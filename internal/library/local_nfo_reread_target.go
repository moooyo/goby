package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
)

type localNFORereadTarget struct {
	id, kind, libraryID, rootID, parentID, relative, path string
	folder                                                bool
	index, parentIndex                                    int
	metadataRevision                                      int64
	localHash, localPath                                  string
	witness                                               localNFORereadMedia
}

type localNFORereadMedia struct {
	id, relative, path, identity, publicationRevision string
	size, changeNS                                    int64
	modified                                          *time.Time
}

func (w localNFORereadMedia) same(other localNFORereadMedia) bool {
	if w.modified == nil || other.modified == nil || !w.modified.Equal(*other.modified) {
		return false
	}
	w.modified, other.modified = nil, nil
	return w == other
}

func (w localNFORereadMedia) matches(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && w.modified != nil && w.changeNS > 0 &&
		w.identity == fileIdentity(info) && w.size == info.Size() && w.modified.Equal(catalogModifiedTime(info)) &&
		w.changeNS == media.FileChangeTime(info)
}

func (target localNFORereadTarget) same(other localNFORereadTarget) bool {
	if !target.witness.same(other.witness) {
		return false
	}
	target.witness, other.witness = localNFORereadMedia{}, localNFORereadMedia{}
	return target == other
}

type localNFORereadFence struct {
	libraryRevision int64
	jobID           string
	finishedAt      time.Time
}

func (f localNFORereadFence) same(other localNFORereadFence) bool {
	return f.libraryRevision == other.libraryRevision && f.jobID == other.jobID && f.finishedAt.Equal(other.finishedAt)
}

func localNFORereadPath(relative string) bool {
	return len(relative) <= 4096 && relative != "." && validImageScanPath(relative) && filepath.Clean(relative) == relative
}

func (target localNFORereadTarget) sourcePath(root libraryRoot) string {
	if target.relative == tvRootSeriesPath && target.kind == "Series" && target.folder && target.parentID == root.libraryID && target.path == "" {
		return "."
	}
	if !localNFORereadPath(target.relative) || target.path != filepath.Join(root.path, target.relative) {
		return ""
	}
	return target.relative
}

func (target localNFORereadTarget) directory(root libraryRoot) string {
	path := target.sourcePath(root)
	if target.folder {
		return path
	}
	return filepath.Dir(path)
}

func readLocalNFOReread(ctx context.Context, tx pgx.Tx, id string, expected *localNFORereadTarget) (localNFORereadTarget, localNFORereadFence, rootBindingRow, error) {
	var target localNFORereadTarget
	var fence localNFORereadFence
	var root rootBindingRow
	if expected != nil {
		// The publisher already holds the library row. Take the expected root
		// before the item, matching the physical media publication lock order.
		var err error
		root, err = readRootBindingForUpdate(ctx, tx, expected.libraryID, expected.rootID)
		if err != nil {
			return target, fence, root, err
		}
	}
	var busy bool
	query := `SELECT i.id,i.type,i.library_id,i.root_id,COALESCE(i.parent_id,''),i.relative_path,i.path,i.is_folder,
		COALESCE(((CASE WHEN ms.online_type=i.type THEN COALESCE(ms.online_base,ms.automatic) ELSE ms.automatic END)->>'IndexNumber')::integer,i.index_number),
		i.parent_index_number,ms.revision,
		i.local_metadata_hash,i.local_metadata_path,
		EXISTS(SELECT 1 FROM local_metadata_ingest q WHERE q.item_id=i.id AND (q.import_nfo OR q.enrich_online))
		OR EXISTS(SELECT 1 FROM local_nfo_recovery_work w WHERE w.item_id=i.id)
		FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id
		WHERE i.id=$1 AND i.root_id IS NOT NULL AND i.library_id<>$2 AND octet_length(i.path)<=4096 AND octet_length(i.relative_path)<=4096
		AND ((i.type IN ('Movie','Episode') AND NOT i.is_folder) OR (i.type IN ('Series','Season') AND i.is_folder))
		AND ` + ordinaryItemSQL("i") + ` AND ` + database.CatalogPathVisibleSQL("i")
	args := []any{id, collectionLibraryID}
	if expected != nil {
		query += ` AND i.library_id=$3 AND i.root_id=$4 FOR UPDATE OF i,ms`
		args = append(args, expected.libraryID, expected.rootID)
	}
	err := tx.QueryRow(ctx, query, args...).Scan(&target.id, &target.kind, &target.libraryID, &target.rootID,
		&target.parentID, &target.relative, &target.path, &target.folder, &target.index, &target.parentIndex, &target.metadataRevision,
		&target.localHash, &target.localPath, &busy)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return target, fence, root, err
	}
	fence, err = readLocalNFORereadFence(ctx, tx, target.libraryID)
	if err != nil {
		return target, fence, root, err
	}
	if busy {
		return target, fence, root, ErrBusy
	}
	if expected == nil {
		root, err = readRootBindingRow(ctx, tx, target.libraryID, target.rootID, false)
		if err != nil {
			return target, fence, root, err
		}
	}
	if target.sourcePath(root.root) == "" {
		return target, fence, root, ErrNotFound
	}
	witness, err := target.readMediaWitness(ctx, tx, root.root)
	target.witness = witness
	return target, fence, root, err
}

func readLocalNFORereadFence(ctx context.Context, tx pgx.Tx, libraryID string) (localNFORereadFence, error) {
	var fence localNFORereadFence
	var enabled, busy bool
	var kind string
	err := tx.QueryRow(ctx, `SELECT l.collection_type,l.revision,COALESCE((l.options->>'EnableLocalMetadata')::boolean,false),
		EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=l.id AND status IN ('Queued','Running'))
		OR (`+mediaPublicationLibraryActiveSQL+`) OR EXISTS(SELECT 1 FROM media_deletion_operations WHERE library_id=l.id AND kind='media')
		OR EXISTS(SELECT 1 FROM task_runs run JOIN task_run_children child ON child.run_id=run.id
			WHERE run.task_key=ANY($2::text[]) AND run.state IN ('running','stopping') AND child.state='running' AND child.library_id=l.id)
		FROM libraries l WHERE l.id=$1`, libraryID,
		[]string{TaskOnlineMetadataRefreshKey, TaskOnlineMetadataIngestKey, TaskOnlineImagesKey}).Scan(&kind, &fence.libraryRevision, &enabled, &busy)
	if err != nil {
		return fence, err
	}
	if kind != "movies" && kind != "tvshows" && kind != "mixed" {
		return fence, ErrNotFound
	}
	if !enabled {
		return fence, ErrLocalNFODisabled
	}
	if busy {
		return fence, ErrBusy
	}
	var status string
	var finished *time.Time
	err = tx.QueryRow(ctx, `SELECT id,status,finished_at FROM scan_jobs WHERE library_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, libraryID).
		Scan(&fence.jobID, &status, &finished)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (status != "Completed" || finished == nil) {
		return fence, ErrBusy
	}
	if err == nil {
		fence.finishedAt = *finished
	}
	return fence, err
}

// A folder catalog row has no durable inode. Require a still-indexed media
// witness under the exact directory (including on unbound mounts), rather than
// trusting a directory that might have been replaced before the reread began.
// This reads one stored source and later stats it; it never enumerates a directory.
func (target localNFORereadTarget) readMediaWitness(ctx context.Context, tx pgx.Tx, root libraryRoot) (localNFORereadMedia, error) {
	columns := `SELECT w.id,w.relative_path,w.path,w.file_identity,w.file_size,w.modified_at,
		COALESCE((w.media->>'FileChangeTimeNs')::bigint,0),COALESCE(w.media_operation_source_revision,'') FROM items w
		WHERE w.library_id=$2 AND w.root_id=$3 AND NOT w.is_folder AND octet_length(w.path)<=4096 AND octet_length(w.relative_path)<=4096
		AND w.file_identity<>'' AND w.modified_at IS NOT NULL AND COALESCE((w.media->>'FileChangeTimeNs')::bigint,0)>0
		AND ` + ordinaryItemSQL("w") + ` AND ` + database.CatalogPathVisibleSQL("w")
	var query string
	args := []any{target.id, target.libraryID, target.rootID}
	if !target.folder {
		query = columns + ` AND w.id=$1`
	} else {
		query = `WITH parents AS (
			SELECT $1::text AS id UNION ALL SELECT p.id FROM items p
			WHERE p.parent_id=$1 AND p.library_id=$2 AND p.root_id=$3 AND p.is_folder AND p.type='Season'
		) ` + columns + ` AND w.parent_id IN (SELECT id FROM parents) AND w.type='Episode'
			AND ($4='' OR left(w.relative_path,length($4))=$4) ORDER BY w.id LIMIT 1`
		prefix := target.sourcePath(root)
		if prefix == "." {
			prefix = ""
		} else {
			prefix += "/"
		}
		args = append(args, prefix)
	}
	var w localNFORereadMedia
	err := tx.QueryRow(ctx, query, args...).Scan(&w.id, &w.relative, &w.path, &w.identity, &w.size, &w.modified, &w.changeNS, &w.publicationRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrSourceChanged
	}
	if err == nil && (!localNFORereadPath(w.relative) || w.path != filepath.Join(root.path, w.relative)) {
		return w, ErrSourceChanged
	}
	return w, err
}

func (target localNFORereadTarget) pairSource(row rootBindingRow, observation localNFORereadObservation) episodePairSource {
	if target.kind != "Episode" {
		return episodePairSource{}
	}
	path := target.sourcePath(row.root)
	season, physical := tvSeasonNumber(filepath.Base(filepath.Dir(path)))
	request := scanMetadataImport{index: target.index, parentIndex: season, parentDefined: physical, directoryIdentity: fileIdentity(observation.directory)}
	return newEpisodePairSource(row, path, target.parentID, fileIdentity(observation.root.registered), request, episodePairFile{
		Identity: target.witness.identity, Size: target.witness.size, ModifiedNS: target.witness.modified.UnixNano(), ChangeNS: target.witness.changeNS,
	})
}

func (target localNFORereadTarget) candidates() ([]string, string) {
	if target.folder {
		return folderNFOCandidate(".", target.kind)
	}
	return fileNFOCandidates(filepath.Base(target.relative), target.kind)
}

func (target localNFORereadTarget) parse(observed localNFOObservation, binding *episodePairBinding) (localMetadata, error) {
	_, kind := target.candidates()
	local, err := parseObservedNFO(observed, kind, binding)
	if err != nil || local.value == nil {
		return localMetadata{}, ErrLocalNFORejected
	}
	v := local.value
	if v.IndexNumber != nil && *v.IndexNumber != target.index || v.ParentIndexNumber != nil && *v.ParentIndexNumber != target.parentIndex {
		return localMetadata{}, ErrLocalNFORejected
	}
	// Empty/unknown-only documents are not a destructive metadata reset command.
	if strings.TrimSpace(v.Name+v.SortName+v.OriginalTitle+v.Overview+v.OfficialRating) == "" &&
		v.ProductionYear == nil && v.PremiereDate == nil && v.EndDate == nil && v.Status == nil && v.CommunityRating == nil &&
		v.AirsBeforeSeasonNumber == nil && v.AirsAfterSeasonNumber == nil && v.AirsBeforeEpisodeNumber == nil &&
		len(v.ProviderIDs)+len(v.Genres)+len(v.Tags)+len(v.Studios)+len(v.People) == 0 {
		return localMetadata{}, ErrLocalNFORejected
	}
	return local, nil
}
