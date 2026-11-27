package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/systemevents"
)

const TaskOnlineMetadataIngestKey = "metadata.enrich_ingested"

type OnlineMetadataIngestProvider interface {
	Status() []providers.Status
	Search(context.Context, string, providers.Query) ([]providers.Match, error)
	Lookup(context.Context, providers.Selection) (providers.Metadata, error)
}

const onlineMetadataIngestEligibleSQL = `q.enrich_online AND NOT q.import_nfo AND NOT q.import_images
	AND j.status='Completed' AND j.finished_at<=run.created_at AND j.library_id=i.library_id
	AND q.source_key=` + metadataIngestSourceKeySQL + ` AND i.type IN ('Movie','Series','Season','Episode')`

// SnapshotOnlineMetadataIngestChildren admits libraries with local work already
// accepted. The worker rechecks ancestry and matching facts before publication.
func SnapshotOnlineMetadataIngestChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER (ORDER BY l.id)-1)::integer
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND l.collection_type IN ('movies','tvshows') AND EXISTS(
		SELECT 1 FROM scan_jobs j JOIN local_metadata_ingest q ON q.scan_job_id=j.id JOIN items i ON i.id=q.item_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.library_id=l.id AND `+onlineMetadataIngestEligibleSQL+` AND `+ordinaryItemSQL("i")+`
		AND `+scanIngestAutomaticGenerationSQL+`)`, runID)
	return tag.RowsAffected(), err
}

type onlineMetadataIngestTarget struct{ id, jobID, source string }

type onlineMetadataIngestParent struct {
	id, parentID, kind, providerID string
	pending                        bool
}

type onlineMetadataIngestSnapshot struct {
	libraryRevision string
	onlineImages    bool
	parents         [2]onlineMetadataIngestParent
	metadataRev     int64
	blocked         bool
	knownID         string
	query           providers.Query
}

// EnrichIngestedOnlineMetadata uses a separately admitted task. The remote
// response is never authority to change a newer scan generation or a rematched
// series. Library media directories are not read or modified.
func (s *Store) EnrichIngestedOnlineMetadata(ctx context.Context, client OnlineMetadataIngestProvider, childID, libraryID string,
	fence AnalysisFence, report func(ProviderProgress) error) error {
	if ctx == nil || fence == nil || report == nil || !metadataIdentifier(libraryID) || !validTaskChildID(childID) {
		return ErrInvalidInput
	}
	if client == nil {
		return providers.ErrNotConfigured
	}
	configured := false
	for _, status := range client.Status() {
		configured = configured || status.ID == "tmdb" && status.Configured
	}
	if !configured {
		return providers.ErrNotConfigured
	}
	ctx = systemevents.WithDerived(ctx)
	var runID string
	if err := s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT run.id FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
			WHERE child.id=$1 AND child.library_id=$2 AND child.state='running' AND child.executor_token IS NOT NULL
			AND run.state='running' AND run.task_key=$3`, childID, libraryID, TaskOnlineMetadataIngestKey).Scan(&runID)
	}); err != nil {
		return err
	}
	progress := ProviderProgress{}
	const pageSize = 32
	for _, kinds := range [][]string{{"Movie", "Series"}, {"Season"}, {"Episode"}} {
		for after := ""; ; {
			rows, err := s.pool.Query(ctx, `SELECT i.id,q.scan_job_id,q.source_key
				FROM task_runs run CROSS JOIN scan_jobs j JOIN local_metadata_ingest q ON q.scan_job_id=j.id
				JOIN items i ON i.id=q.item_id JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
				WHERE run.id=$1 AND j.library_id=$2 AND i.library_id=$2 AND i.id>$3 AND i.type=ANY($4::text[])
				AND `+onlineMetadataIngestEligibleSQL+` AND `+ordinaryItemSQL("i")+`
				AND `+scanIngestAutomaticGenerationSQL+` ORDER BY i.id LIMIT $5`, runID, libraryID, after, kinds, pageSize)
			if err != nil {
				return err
			}
			page, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (target onlineMetadataIngestTarget, err error) {
				err = row.Scan(&target.id, &target.jobID, &target.source)
				return
			})
			if err != nil {
				return err
			}
			for _, target := range page {
				updated, err := s.enrichOnlineIngestTarget(ctx, client, libraryID, target, fence)
				if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrRevisionConflict) {
					return err
				}
				progress.Processed++
				if updated {
					progress.Updated++
				}
				after = target.id
				if err := report(progress); err != nil {
					return err
				}
			}
			if len(page) < pageSize {
				break
			}
		}
	}
	return ctx.Err()
}

func onlineIngestCatalogSnapshot(ctx context.Context, tx pgx.Tx, libraryID string, target onlineMetadataIngestTarget, lock bool) (onlineMetadataIngestSnapshot, metadataRecord, error) {
	var snapshot onlineMetadataIngestSnapshot
	statement := `SELECT revision::text,COALESCE(options->'EnableOnlineImages'='true'::jsonb,false)
		FROM libraries WHERE id=$1 AND collection_type IN ('movies','tvshows')`
	if lock {
		statement += ` FOR SHARE`
	}
	if err := tx.QueryRow(ctx, statement, libraryID).Scan(&snapshot.libraryRevision, &snapshot.onlineImages); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return snapshot, metadataRecord{}, err
	}
	statement = `SELECT COALESCE((SELECT provider_id FROM item_provider_sources p WHERE p.item_id=i.id AND p.provider='tmdb'
		AND EXISTS(SELECT 1 FROM item_metadata_state ms WHERE ms.item_id=i.id AND ms.online_type=i.type)),'')
		FROM local_metadata_ingest q JOIN items i ON i.id=q.item_id JOIN scan_jobs j ON j.id=q.scan_job_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND i.library_id=$2 AND j.library_id=$2 AND j.id=$3 AND q.source_key=$4
		AND q.enrich_online AND NOT q.import_nfo AND NOT q.import_images AND j.status='Completed'
		AND q.source_key=` + metadataIngestSourceKeySQL + ` AND ` + ordinaryItemSQL("i") + `
		AND NOT EXISTS(SELECT 1 FROM scan_jobs scan WHERE scan.library_id=$2 AND scan.status IN ('Queued','Running'))
		AND NOT EXISTS(SELECT 1 FROM media_operations p WHERE p.source_item_id=i.id AND p.publication_phase IN ('prepared','catalog_committed'))`
	if lock {
		statement += ` FOR UPDATE OF i,q FOR SHARE OF r`
	}
	if err := tx.QueryRow(ctx, statement, target.id, libraryID, target.jobID, target.source).Scan(&snapshot.knownID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return snapshot, metadataRecord{}, err
	}
	record, err := readMetadataRecord(ctx, tx, target.id, lock)
	if err != nil {
		return snapshot, record, err
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return snapshot, record, err
	}
	snapshot.metadataRev = record.revision
	snapshot.query = providerQuery(detail)
	snapshot.knownID = providers.CanonicalTMDBMetadataID(record.itemType, snapshot.knownID)
	switch record.itemType {
	case "Movie", "Series":
		if id := providers.MetadataID("tmdb", record.itemType, snapshot.query.ProviderIDs); id != "" {
			snapshot.query.ProviderIDs = nil
			if canonical := providers.CanonicalTMDBMetadataID(record.itemType, id); canonical != "" {
				snapshot.query.ProviderIDs = map[string]string{"Tmdb": canonical}
			} else {
				// An explicitly selected but invalid identifier is not permission
				// to replace that selection with a title match.
				snapshot.query.Name = ""
			}
		}
	case "Season", "Episode":
		if err := onlineIngestDependentQuery(ctx, tx, record, &snapshot, lock); err != nil {
			return snapshot, record, err
		}
	default:
		return snapshot, record, ErrNotFound
	}
	return snapshot, record, nil
}

func readOnlineIngestParent(ctx context.Context, tx pgx.Tx, libraryID, id string, lock bool) (onlineMetadataIngestParent, error) {
	var parent onlineMetadataIngestParent
	var projection []byte
	statement := `SELECT i.id,COALESCE(i.parent_id,''),i.type,ms.effective,
		EXISTS(SELECT 1 FROM local_metadata_ingest q WHERE q.item_id=i.id AND (q.import_nfo OR q.enrich_online))
		FROM items i LEFT JOIN item_metadata_state ms ON ms.item_id=i.id
		WHERE i.id=$1 AND i.library_id=$2 AND i.is_folder AND i.type IN ('Series','Season') AND ` + ordinaryItemSQL("i")
	if lock {
		// The reserved writer serializes metadata changes. Lock the physical
		// parent even if its metadata projection is currently absent.
		statement += ` FOR SHARE OF i`
	}
	err := tx.QueryRow(ctx, statement, id, libraryID).Scan(&parent.id, &parent.parentID, &parent.kind, &projection, &parent.pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return parent, nil
	}
	if err != nil {
		return parent, err
	}
	values, err := decodeMetadataValues(projection)
	if err == nil {
		parent.providerID = providers.CanonicalTMDBMetadataID(parent.kind, providers.MetadataID("tmdb", parent.kind, values.ProviderIDs))
	}
	return parent, err
}

func onlineIngestDependentQuery(ctx context.Context, tx pgx.Tx, record metadataRecord, snapshot *onlineMetadataIngestSnapshot, lock bool) error {
	parent, err := readOnlineIngestParent(ctx, tx, record.libraryID, record.parentID, lock)
	if err != nil {
		return err
	}
	snapshot.parents[0] = parent
	series := parent
	if record.itemType == "Episode" && parent.kind == "Season" {
		series, err = readOnlineIngestParent(ctx, tx, record.libraryID, parent.parentID, lock)
		if err != nil {
			return err
		}
		snapshot.parents[1] = series
	}
	snapshot.blocked = parent.pending || series.pending
	query := &snapshot.query
	// Never search using an episode or season title as a series name.
	query.Name = ""
	if series.kind != "Series" {
		query.ProviderIDs = nil
		return nil
	}
	localID := providers.MetadataID("tmdb", query.Type, query.ProviderIDs)
	id := providers.CanonicalTMDBMetadataID(query.Type, localID)
	if id != "" && id != snapshot.knownID {
		// A distinct, usable child address wins. Bare child IDs in NFOs
		// cannot be looked up by TMDB's season/episode detail endpoint.
		query.ProviderIDs = map[string]string{"Tmdb": id}
		return nil
	}
	query.ProviderIDs = nil
	if localID != "" && id == "" && providers.CanonicalTMDBMetadataID("Series", localID) == "" {
		// A bare positive child ID is not addressable, but still identifies
		// an entity. Malformed selections must not trigger a guessed lookup.
		return nil
	}
	if series.providerID == "" || query.Season < 0 || record.itemType == "Episode" && (query.Episode <= 0 || parent.kind == "Series" && query.Season == 0) {
		return nil
	}
	id = series.providerID + ":" + strconv.Itoa(query.Season)
	if record.itemType == "Episode" {
		id += ":" + strconv.Itoa(query.Episode)
	}
	query.ProviderIDs = map[string]string{"Tmdb": id}
	return nil
}

func (s *Store) enrichOnlineIngestTarget(ctx context.Context, client OnlineMetadataIngestProvider, libraryID string,
	target onlineMetadataIngestTarget, fence AnalysisFence) (bool, error) {
	read, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, err
	}
	defer rollback(read)
	before, _, err := onlineIngestCatalogSnapshot(ctx, read, libraryID, target, false)
	if err != nil {
		return false, err
	}
	if err := read.Commit(ctx); err != nil || before.blocked {
		return false, err
	}
	selection, match, err := selectProviderTaskMetadata(ctx, client, "tmdb", before.query)
	if errors.Is(err, providers.ErrNotFound) {
		match, err = false, nil
	}
	if err != nil {
		return false, err
	}
	if match {
		selection.ID = providers.CanonicalTMDBMetadataID(selection.Type, selection.ID)
		if selection.ID == "" {
			return false, providers.ErrInvalidInput
		}
	}
	var result providers.Metadata
	var normalized []byte
	if match && selection.ID != before.knownID {
		result, err = client.Lookup(ctx, selection)
		if err != nil && !errors.Is(err, providers.ErrNotFound) {
			return false, err
		}
		if err == nil {
			if result.Provider != selection.Provider || result.Type != selection.Type || result.ID != selection.ID {
				return false, providers.ErrInvalidInput
			}
			normalized, err = normalizeOnlineMetadata(result)
			if err != nil {
				return false, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	err = s.WithOwnedTx(ctx, func(view OwnedTx) error {
		if err := fence(view); err != nil {
			return err
		}
		tx := view.(*ownedCallbackTx)
		current, record, err := onlineIngestCatalogSnapshot(tx.ctx, tx.catalog, libraryID, target, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, current) {
			return ErrRevisionConflict
		}
		if normalized != nil {
			if _, err := writeOnlineMetadata(tx.ctx, tx.catalog, record, nil, result, normalized); err != nil {
				return err
			}
		}
		if _, err := view.Exec(`DELETE FROM local_metadata_ingest WHERE item_id=$1 AND scan_job_id=$2`, target.id, target.jobID); err != nil {
			return err
		}
		if before.onlineImages && (normalized != nil || before.knownID != "" || providers.MetadataID("tmdb", before.query.Type, before.query.ProviderIDs) != "") {
			if err := systemevents.Record(view.Exec, systemevents.OnlineImagesRequested); err != nil {
				return err
			}
		}
		return fence(view)
	})
	return err == nil && normalized != nil, err
}
