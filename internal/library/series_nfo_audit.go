package library

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

const (
	maxSeriesNFOAuditItems = 512
	maxSeriesNFOAuditRoots = 32
	maxSeriesNFOAuditBytes = 32 << 20
	seriesNFOAuditTimeout  = 20 * time.Second
)

// SeriesNFOAuditReport is a point-in-time diagnostic. ObservedCount counts
// stable, still-accepted sidecars, not independently verified show identities.
type SeriesNFOAuditReport struct {
	LibraryID     string                  `json:"LibraryId"`
	Status        string                  `json:"Status"`
	SeriesCount   int                     `json:"SeriesCount"`
	ObservedCount int                     `json:"ObservedCount"`
	Warnings      []SeriesNFOAuditWarning `json:"Warnings"`
}

const (
	seriesNFOAuditObserved  = "observed"
	seriesNFOAuditUnsettled = "scan_unsettled"
	seriesNFOAuditDisabled  = "local_metadata_disabled"
	seriesNFOAuditLimited   = "limit_exceeded"
	seriesNFOAuditChanged   = "source_changed"
)

var (
	errSeriesNFOAuditBytes = errors.New("series NFO audit read budget exhausted")
	errSeriesNFOAuditRoot  = errors.New("series NFO audit storage binding did not match")
)

type seriesNFOAuditTarget struct {
	id, rootID, relative, path, nfoPath, digest string
	bindingRevision                             int64
	pending                                     bool
}

func (target seriesNFOAuditTarget) sourceRelative() string {
	if target.relative == tvRootSeriesPath && target.path == "" {
		return "."
	}
	return filepath.FromSlash(target.relative)
}

func (target seriesNFOAuditTarget) inRoot(root libraryRoot) bool {
	if target.rootID != root.id {
		return false
	}
	relative := target.sourceRelative()
	if relative == "." {
		return target.relative == tvRootSeriesPath && target.path == ""
	}
	return validImageScanPath(relative) && filepath.IsLocal(relative) &&
		filepath.ToSlash(filepath.Clean(relative)) == target.relative && filepath.Join(root.path, relative) == target.path
}

func (target seriesNFOAuditTarget) accepted() bool {
	if target.pending || len(target.digest) != 64 || !metadataIdentifier(target.id) || target.bindingRevision < 1 {
		return false
	}
	candidates, _ := folderNFOCandidate(target.sourceRelative(), "Series")
	return target.nfoPath == filepath.ToSlash(candidates[0])
}

type seriesNFOAuditSnapshot struct {
	status, jobID string
	revision      int64
	finishedAt    time.Time
	targets       []seriesNFOAuditTarget
}

// AuditSeriesNFO checks one settled library without opening media content,
// authorizing a scan, or changing the catalog, sidecars, and task queues.
func (s *Store) AuditSeriesNFO(ctx context.Context, actor identity.Principal, libraryID string) (_ SeriesNFOAuditReport, resultErr error) {
	if ctx == nil || !metadataIdentifier(libraryID) {
		return SeriesNFOAuditReport{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil || !s.Available() {
		return SeriesNFOAuditReport{}, ErrUnavailable
	}
	defer func() {
		var sqlError *pgconn.PgError
		if errors.As(resultErr, &sqlError) && sqlError.Code == "57014" {
			resultErr = errors.Join(ErrUnavailable, resultErr)
		}
	}()
	if !s.seriesNFOAuditRunning.CompareAndSwap(false, true) {
		return SeriesNFOAuditReport{}, ErrBusy
	}
	var retired <-chan struct{}
	defer func() {
		releaseAfterNFOObservationRetires(retired, func() { s.seriesNFOAuditRunning.Store(false) })
	}()
	ctx, cancel := context.WithTimeout(ctx, seriesNFOAuditTimeout)
	defer cancel()

	report, err := s.auditSeriesNFO(ctx, actor, libraryID, &retired)
	if err != nil {
		return SeriesNFOAuditReport{}, err
	}
	if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
		return SeriesNFOAuditReport{}, err
	}
	return report, nil
}

// A timed-out caller must not start another audit while the actual storage
// observation still owns its primary I/O resources. At most one waiter exists.
func releaseAfterNFOObservationRetires(retired <-chan struct{}, release func()) {
	if retired == nil {
		release()
		return
	}
	select {
	case <-retired:
		release()
	default:
		go func() {
			<-retired
			release()
		}()
	}
}

func (s *Store) auditSeriesNFO(ctx context.Context, actor identity.Principal, libraryID string, retired *<-chan struct{}) (SeriesNFOAuditReport, error) {
	snapshot, err := s.readSeriesNFOAuditSnapshot(ctx, actor, libraryID)
	if err != nil {
		return SeriesNFOAuditReport{}, err
	}
	report := SeriesNFOAuditReport{LibraryID: libraryID, Status: snapshot.status, SeriesCount: len(snapshot.targets), Warnings: []SeriesNFOAuditWarning{}}
	if snapshot.status != seriesNFOAuditObserved || len(snapshot.targets) == 0 {
		return report, nil
	}
	claims, err := s.observeSeriesNFOAudit(ctx, actor, libraryID, snapshot, retired)
	switch {
	case errors.Is(err, ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return SeriesNFOAuditReport{}, err
	case errors.Is(err, errSeriesNFOAuditBytes):
		report.Status = seriesNFOAuditLimited
		return report, nil
	case errors.Is(err, errSeriesNFOAuditRoot):
		report.Status = seriesNFOAuditChanged
		return report, nil
	case err != nil:
		return SeriesNFOAuditReport{}, err
	}
	// Release SQL snapshots for storage I/O. A fresh snapshot rejects mixed
	// generations and rechecks accepted hashes, bindings, and pending work.
	current, err := s.readSeriesNFOAuditSnapshot(ctx, actor, libraryID)
	if err != nil {
		return SeriesNFOAuditReport{}, err
	}
	if snapshot.status != current.status || snapshot.jobID != current.jobID || snapshot.revision != current.revision ||
		!snapshot.finishedAt.Equal(current.finishedAt) || !slices.Equal(snapshot.targets, current.targets) {
		report.Status = seriesNFOAuditChanged
		return report, nil
	}
	report.ObservedCount = len(claims)
	report.Warnings = seriesNFOAuditWarnings(claims)
	return report, nil
}

func (s *Store) readSeriesNFOAuditSnapshot(ctx context.Context, actor identity.Principal, libraryID string) (seriesNFOAuditSnapshot, error) {
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return seriesNFOAuditSnapshot{}, err
	}
	defer rollback(tx)
	var enabled bool
	var kind string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT collection_type,revision,COALESCE((options->>'EnableLocalMetadata')::boolean,false)
		FROM libraries WHERE id=$1`, libraryID).Scan(&kind, &revision, &enabled)
	if errors.Is(err, pgx.ErrNoRows) || libraryID == collectionLibraryID {
		return seriesNFOAuditSnapshot{}, ErrNotFound
	}
	if err != nil {
		return seriesNFOAuditSnapshot{}, err
	}
	if kind != "tvshows" && kind != "mixed" {
		return seriesNFOAuditSnapshot{}, ErrInvalidInput
	}
	snapshot := seriesNFOAuditSnapshot{status: seriesNFOAuditDisabled}
	if enabled {
		snapshot, err = listSeriesNFOAuditTargets(ctx, tx, libraryID)
	}
	if err != nil {
		return seriesNFOAuditSnapshot{}, err
	}
	snapshot.revision = revision
	return snapshot, tx.Commit(ctx)
}

func listSeriesNFOAuditTargets(ctx context.Context, tx pgx.Tx, libraryID string) (seriesNFOAuditSnapshot, error) {
	snapshot := seriesNFOAuditSnapshot{status: seriesNFOAuditUnsettled}
	var latest string
	var finishedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT id,status,finished_at FROM scan_jobs WHERE library_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, libraryID).
		Scan(&snapshot.jobID, &latest, &finishedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (latest != "Completed" || finishedAt == nil) {
		return snapshot, nil
	}
	if err != nil {
		return seriesNFOAuditSnapshot{}, err
	}
	var unsettled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))
		OR (`+mediaPublicationLibraryActiveSQL+`)
		OR EXISTS(SELECT 1 FROM media_deletion_operations WHERE library_id=$1 AND kind='media')`, libraryID).
		Scan(&unsettled); err != nil || unsettled {
		return snapshot, err
	}
	snapshot.status, snapshot.finishedAt = seriesNFOAuditObserved, *finishedAt
	rows, err := tx.Query(ctx, `SELECT CASE WHEN octet_length(i.id)<=256 THEN i.id ELSE '' END,
		CASE WHEN octet_length(r.id)<=256 THEN r.id ELSE '' END,
		CASE WHEN octet_length(i.relative_path)<=4096 THEN i.relative_path ELSE '' END,
		CASE WHEN octet_length(i.path)<=4096 THEN i.path ELSE '' END,
		CASE WHEN octet_length(i.local_metadata_path)<=4096 THEN i.local_metadata_path ELSE '' END,
		CASE WHEN octet_length(i.local_metadata_hash)=64 THEN i.local_metadata_hash ELSE '' END,r.binding_revision,
		EXISTS(SELECT 1 FROM local_metadata_ingest q WHERE q.item_id=i.id AND q.import_nfo)
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.library_id=$1 AND i.type='Series' AND i.is_folder
		AND ((i.path<>'' AND i.relative_path NOT LIKE '//%') OR (i.path='' AND i.relative_path=$3))
		AND `+ordinaryItemSQL("i")+` AND `+database.CatalogPathVisibleSQL("i")+`
		ORDER BY r.id COLLATE "C",i.id COLLATE "C" LIMIT $2`, libraryID, maxSeriesNFOAuditItems+1, tvRootSeriesPath)
	if err != nil {
		return seriesNFOAuditSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var target seriesNFOAuditTarget
		if err := rows.Scan(&target.id, &target.rootID, &target.relative, &target.path, &target.nfoPath, &target.digest, &target.bindingRevision, &target.pending); err != nil {
			return seriesNFOAuditSnapshot{}, err
		}
		snapshot.targets = append(snapshot.targets, target)
	}
	roots := make(map[string]struct{})
	for _, target := range snapshot.targets {
		roots[target.rootID] = struct{}{}
	}
	if len(snapshot.targets) > maxSeriesNFOAuditItems || len(roots) > maxSeriesNFOAuditRoots {
		// Do not report an apparently clean subset when a peer may be omitted.
		snapshot.status, snapshot.targets = seriesNFOAuditLimited, nil
	}
	return snapshot, rows.Err()
}
