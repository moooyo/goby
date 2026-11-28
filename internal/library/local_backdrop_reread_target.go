package library

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

type localBackdropItem struct {
	id, libraryID, rootID, parentID, relative, path string
}

func (item localBackdropItem) directory(root libraryRoot) string {
	if item.relative == tvRootSeriesPath && item.parentID == root.libraryID && item.path == "" {
		return "."
	}
	if item.relative == "." || !validImageScanPath(item.relative) || filepath.Clean(item.relative) != item.relative ||
		filepath.Join(root.path, item.relative) != item.path {
		return ""
	}
	return item.relative
}

type localBackdropQueue struct {
	jobID, sourceKey, directoryIdentity string
	index, parentIndex                  int
	parentDefined                       bool
}

type localBackdropGeneration struct {
	jobID, options string
	revision       int64
	finishedAt     time.Time
	onlineImages   bool
}

func (f localBackdropGeneration) same(other localBackdropGeneration) bool {
	return f.jobID == other.jobID && f.revision == other.revision && f.options == other.options &&
		f.finishedAt.Equal(other.finishedAt) && f.onlineImages == other.onlineImages
}

type localBackdropTarget struct {
	item       localBackdropItem
	queue      localBackdropQueue
	generation localBackdropGeneration
	root       rootBindingRow
}

func readLocalBackdropItem(ctx context.Context, tx pgx.Tx, id string, lock bool) (localBackdropItem, error) {
	var item localBackdropItem
	query := `SELECT i.id,i.library_id,i.root_id,COALESCE(i.parent_id,''),i.relative_path,i.path FROM items i
		WHERE i.id=$1 AND i.root_id IS NOT NULL AND i.type='Series' AND i.is_folder
		AND octet_length(i.path)<=4096 AND octet_length(i.relative_path)<=4096
		AND ` + ordinaryItemSQL("i") + ` AND ` + database.CatalogPathVisibleSQL("i")
	if lock {
		query += ` FOR UPDATE OF i`
	}
	err := tx.QueryRow(ctx, query, id).Scan(&item.id, &item.libraryID, &item.rootID, &item.parentID, &item.relative, &item.path)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return item, err
}

func readLocalBackdropGeneration(ctx context.Context, tx pgx.Tx, libraryID string, lock, scopedImporter bool) (localBackdropGeneration, error) {
	var generation localBackdropGeneration
	var kind string
	var enabled bool
	query := `SELECT collection_type,revision,options::text,
		COALESCE(options->'EnableLocalImages'='true'::jsonb,false),
		COALESCE(options->'EnableOnlineImages'='true'::jsonb,false) FROM libraries WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, libraryID).Scan(&kind, &generation.revision, &generation.options, &enabled, &generation.onlineImages)
	if errors.Is(err, pgx.ErrNoRows) {
		return generation, ErrNotFound
	}
	if err != nil {
		return generation, err
	}
	if kind != "tvshows" && kind != "mixed" {
		return generation, ErrNotFound
	}
	if !enabled {
		return generation, ErrLocalBackdropDisabled
	}
	// Lock acquisition and locked rereads fence the local importer by exact
	// source instead of rejecting an unrelated child in this library.
	taskKeys := []string{TaskNFORecoveryKey, TaskOnlineMetadataIngestKey, TaskOnlineMetadataRefreshKey, TaskOnlineImagesKey}
	if !scopedImporter {
		taskKeys = append(taskKeys, TaskLocalArtworkIngestKey)
	}
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))
		OR (`+mediaPublicationLibraryActiveSQL+`) OR EXISTS(SELECT 1 FROM media_deletion_operations WHERE library_id=$1 AND kind='media')
		OR EXISTS(SELECT 1 FROM task_runs run JOIN task_run_children child ON child.run_id=run.id
			WHERE run.task_key=ANY($2::text[]) AND run.state IN ('running','stopping') AND child.state='running' AND child.library_id=$1)`,
		libraryID, taskKeys).Scan(&busy); err != nil {
		return generation, err
	}
	if busy {
		return generation, ErrBusy
	}
	// Never fall back to an older completed scan after a more recent attempt.
	var finished *time.Time
	var status string
	err = tx.QueryRow(ctx, `SELECT id,status,finished_at FROM scan_jobs WHERE library_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, libraryID).
		Scan(&generation.jobID, &status, &finished)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (status != "Completed" || finished == nil) {
		return generation, ErrLocalBackdropNotPending
	}
	if err == nil {
		generation.finishedAt = *finished
	}
	return generation, err
}

func readLocalBackdropQueue(ctx context.Context, tx pgx.Tx, item localBackdropItem, generation localBackdropGeneration, lock bool) (localBackdropQueue, error) {
	var queue localBackdropQueue
	var sourceCurrent bool
	query := `SELECT q.scan_job_id,q.source_key,q.directory_identity,q.index_number,q.parent_index_number,q.parent_number_defined,
		q.source_key=` + metadataIngestSourceKeySQL + ` FROM local_metadata_ingest q JOIN items i ON i.id=q.item_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE q.item_id=$1 AND q.import_images AND NOT q.import_nfo AND NOT q.enrich_online`
	if lock {
		query += ` FOR UPDATE OF q`
	}
	err := tx.QueryRow(ctx, query, item.id).Scan(&queue.jobID, &queue.sourceKey, &queue.directoryIdentity, &queue.index, &queue.parentIndex,
		&queue.parentDefined, &sourceCurrent)
	if errors.Is(err, pgx.ErrNoRows) {
		return queue, ErrLocalBackdropNotPending
	}
	if err == nil && (!sourceCurrent || queue.jobID != generation.jobID) {
		return queue, ErrSourceChanged
	}
	return queue, err
}

func readLocalBackdropTarget(ctx context.Context, tx pgx.Tx, id string, previous *localBackdropTarget, scopedImporter bool) (localBackdropTarget, error) {
	var target localBackdropTarget
	var err error
	if previous == nil {
		target.item, err = readLocalBackdropItem(ctx, tx, id, false)
		if err != nil {
			return target, err
		}
	} else {
		target.item = previous.item
	}
	target.generation, err = readLocalBackdropGeneration(ctx, tx, target.item.libraryID, previous != nil, scopedImporter)
	if err != nil {
		return target, err
	}
	target.root, err = readRootBindingRow(ctx, tx, target.item.libraryID, target.item.rootID, previous != nil)
	if err != nil {
		return target, err
	}
	if target.item.directory(target.root.root) == "" {
		return target, ErrNotFound
	}
	if previous != nil {
		current, err := readLocalBackdropItem(ctx, tx, id, true)
		if err != nil {
			return target, err
		}
		if current != target.item || !target.generation.same(previous.generation) || !target.root.same(previous.root) {
			return target, ErrSourceChanged
		}
	}
	target.queue, err = readLocalBackdropQueue(ctx, tx, target.item, target.generation, previous != nil)
	if err == nil && previous != nil && target.queue != previous.queue {
		err = ErrSourceChanged
	}
	return target, err
}
