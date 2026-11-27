package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/systemevents"
)

const TaskVideoSeekIngestKey = "media.index_video_seek"

// WithDeferredVideoSeek enables the private, background seek evidence cache.
func WithDeferredVideoSeek() Option {
	return func(settings *storeOptions) error {
		settings.deferVideoSeek = true
		return nil
	}
}

func (state *scanState) defersVideoSeek(itemType string, info *media.Info) bool {
	return state.store.deferVideoSeek && !state.task.job.ForceProbe && scanIngestVideoLibrary(state.library) &&
		(itemType == "Movie" || itemType == "Episode") && info != nil && len(info.VideoSeekIndexes) == 0 &&
		media.ProgressiveSeekIngestStream(*info) != nil
}

func (state *scanState) queueVideoSeekIngest(tx pgx.Tx, itemID string) error {
	// Never discard ready evidence or a retry deadline when a scan reaccepts
	// exactly the same media. An incomplete scan cannot claim a pending row.
	_, err := tx.Exec(state.task.ctx, `INSERT INTO video_seek_ingest(item_id,scan_job_id,source_revision,index_version)
		SELECT i.id,$2,`+MediaOperationSourceRevisionSQL+`,$3 FROM items i WHERE i.id=$1
		ON CONFLICT(item_id) DO UPDATE SET scan_job_id=EXCLUDED.scan_job_id,source_revision=EXCLUDED.source_revision,
		index_version=EXCLUDED.index_version,indexes=NULL,retry_at=CASE WHEN
			(video_seek_ingest.source_revision,video_seek_ingest.index_version)=(EXCLUDED.source_revision,EXCLUDED.index_version)
			THEN video_seek_ingest.retry_at ELSE '-infinity' END
		WHERE video_seek_ingest.indexes IS NULL OR (video_seek_ingest.source_revision,video_seek_ingest.index_version)
			IS DISTINCT FROM (EXCLUDED.source_revision,EXCLUDED.index_version)`,
		itemID, state.task.job.ID, media.VideoSeekIndexVersion)
	return err
}

const videoSeekIngestEligibleSQL = `q.indexes IS NULL AND q.index_version=$2 AND j.status='Completed'
	AND j.finished_at<=run.created_at AND (q.retry_at<=clock_timestamp() OR run.source IN ('manual','compatibility'))
	AND q.source_revision=` + MediaOperationSourceRevisionSQL + `
	AND i.type IN ('Movie','Episode') AND NOT i.is_folder AND i.media IS NOT NULL
	AND COALESCE(i.media->'VideoSeekIndexes','null'::jsonb) IN ('null'::jsonb,'[]'::jsonb)`

// SnapshotVideoSeekIngestChildren sees only current, due work from completed
// movie and TV scans. The task manager shares one expensive analysis slot.
func SnapshotVideoSeekIngestChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER (ORDER BY l.id)-1)::integer
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND l.collection_type IN ('movies','tvshows') AND EXISTS(
		SELECT 1 FROM scan_jobs j JOIN video_seek_ingest q ON q.scan_job_id=j.id JOIN items i ON i.id=q.item_id
		WHERE i.library_id=l.id AND j.library_id=l.id AND `+videoSeekIngestEligibleSQL+` AND `+ordinaryItemSQL("i")+`
		AND `+scanIngestAutomaticGenerationSQL+`)`, runID, media.VideoSeekIndexVersion)
	return tag.RowsAffected(), err
}

type videoSeekIngestTarget struct{ itemID, jobID, revision string }

type videoSeekIngestAnalyzer func(context.Context, *os.File, media.Info) ([]media.VideoSeekIndex, error)

// BuildIngestedVideoSeekIndexes reads and indexes the already accepted media
// under a new task grant. It never writes media files or changes items.media.
func (s *Store) BuildIngestedVideoSeekIndexes(ctx context.Context, childID, libraryID, executable string, fence AnalysisFence, report func(ProviderProgress) error) error {
	if executable == "" {
		return ErrInvalidInput
	}
	return s.buildIngestedVideoSeekIndexes(ctx, childID, libraryID, fence, report, func(work context.Context, file *os.File, info media.Info) ([]media.VideoSeekIndex, error) {
		return media.AnalyzeProgressiveSeekIngest(work, executable, file, info)
	})
}

func (s *Store) buildIngestedVideoSeekIndexes(ctx context.Context, childID, libraryID string, fence AnalysisFence,
	report func(ProviderProgress) error, analyze videoSeekIngestAnalyzer) (resultErr error) {
	if ctx == nil || fence == nil || report == nil || analyze == nil || !metadataIdentifier(libraryID) {
		return ErrInvalidInput
	}
	var runID string
	check := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		if runID != "" {
			return nil
		}
		return tx.QueryRow(`SELECT run.id FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
			WHERE child.id=$1 AND child.library_id=$2 AND run.task_key=$3`, childID, libraryID, TaskVideoSeekIngestKey).Scan(&runID)
	}
	ctx, release, err := s.BeginTaskSourceOperation(media.WithBackgroundProcess(systemevents.WithDerived(ctx)), childID, check)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	grant := taskSourceGrant(ctx)
	if grant.libraryID != libraryID {
		return ErrInvalidInput
	}
	progress := ProviderProgress{}
	const pageSize = 32
	for after := ""; ; {
		rows, err := s.pool.Query(ctx, `SELECT q.item_id,q.scan_job_id,q.source_revision
			FROM task_runs run CROSS JOIN scan_jobs j JOIN video_seek_ingest q ON q.scan_job_id=j.id JOIN items i ON i.id=q.item_id
			WHERE run.id=$1 AND j.library_id=$3 AND i.library_id=$3 AND q.item_id>$4 AND `+videoSeekIngestEligibleSQL+`
			AND `+ordinaryItemSQL("i")+` AND `+scanIngestAutomaticGenerationSQL+`
			ORDER BY q.item_id LIMIT $5`, runID, media.VideoSeekIndexVersion, libraryID, after, pageSize)
		if err != nil {
			return err
		}
		page, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (target videoSeekIngestTarget, err error) {
			err = row.Scan(&target.itemID, &target.jobID, &target.revision)
			return
		})
		if err != nil {
			return err
		}
		for _, target := range page {
			written, err := s.indexIngestedVideoSeek(ctx, grant, target, analyze)
			if err != nil && !videoSeekIngestDeferred(err) {
				return err
			}
			if written {
				progress.Updated++
			}
			progress.Processed++
			after = target.itemID
			if err := report(progress); err != nil {
				return err
			}
		}
		if len(page) < pageSize {
			return ctx.Err()
		}
	}
}

func videoSeekIngestDeferred(err error) bool {
	if errors.Is(err, errSidecarRetirementUnknown) || errors.Is(err, errSidecarRollbackUnknown) ||
		errors.Is(err, media.ErrProcessRetirementUnknown) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrSourceChanged) || errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, ErrRootBindingConflict) || sidecarAdmissionRetryable(err)
}

func (s *Store) readVideoSeekIngestSource(ctx context.Context, grant *internalTaskSourceGrant, target videoSeekIngestTarget) (indexedMediaSource, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer rollback(tx)
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), target.itemID, "")
	if err != nil {
		return indexedMediaSource{}, err
	}
	row := grant.roots[snapshot.root.id]
	if row.root != snapshot.root || row.revision != snapshot.rootBindingRevision || row.root.libraryID != grant.libraryID {
		return indexedMediaSource{}, ErrSourceChanged
	}
	if err := checkVideoSeekIngestTarget(ctx, tx, grant, target, snapshot, false); err != nil {
		return indexedMediaSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, err
	}
	return snapshot, nil
}

func checkVideoSeekIngestTarget(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant,
	target videoSeekIngestTarget, snapshot indexedMediaSource, lock bool) error {
	if lock {
		if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
			return err
		}
		var kind string
		if err := tx.QueryRow(ctx, `SELECT collection_type FROM libraries WHERE id=$1 FOR SHARE`, grant.libraryID).Scan(&kind); err != nil {
			return err
		}
		if kind != "movies" && kind != "tvshows" {
			return ErrSourceChanged
		}
		if err := checkScanOperationRootMappingTx(ctx, tx, snapshot.root); err != nil {
			return err
		}
	}
	statement := `SELECT q.source_revision FROM video_seek_ingest q JOIN items i ON i.id=q.item_id
		JOIN scan_jobs j ON j.id=q.scan_job_id JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE q.item_id=$1 AND q.scan_job_id=$2 AND q.source_revision=$3 AND q.index_version=$4
		AND q.indexes IS NULL AND j.status='Completed' AND j.library_id=$5 AND i.library_id=$5 AND i.root_id=$6
		AND r.binding_revision=$7 AND q.source_revision=` + MediaOperationSourceRevisionSQL + `
		AND ` + ordinaryItemSQL("i") + ` AND NOT i.is_folder AND i.type IN ('Movie','Episode')
		AND COALESCE(i.media->'VideoSeekIndexes','null'::jsonb) IN ('null'::jsonb,'[]'::jsonb)
		AND NOT EXISTS(SELECT 1 FROM scan_jobs active WHERE active.library_id=$5 AND active.status IN ('Queued','Running'))
		AND NOT EXISTS(SELECT 1 FROM media_operations p WHERE p.source_item_id=i.id AND p.publication_phase IN ('prepared','catalog_committed'))`
	if lock {
		statement += ` FOR UPDATE OF i,q`
	}
	var revision string
	err := tx.QueryRow(ctx, statement, target.itemID, target.jobID, target.revision, media.VideoSeekIndexVersion,
		grant.libraryID, snapshot.root.id, snapshot.rootBindingRevision).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSourceChanged
	}
	return err
}

func (s *Store) indexIngestedVideoSeek(ctx context.Context, grant *internalTaskSourceGrant, target videoSeekIngestTarget,
	analyze videoSeekIngestAnalyzer) (_ bool, resultErr error) {
	snapshot, err := s.readVideoSeekIngestSource(ctx, grant, target)
	if err != nil {
		return false, err
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: snapshot.root, bindingRevision: snapshot.rootBindingRevision}})
	if err != nil {
		return false, err
	}
	var lease *libraryRootLease
	var file *os.File
	var before os.FileInfo
	var proofWorkers sync.WaitGroup
	defer func() {
		proofWorkers.Wait()
		cleanup := operation.Context(ctx)
		if file != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanup, file))
		}
		if lease != nil {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(cleanup, lease.Close))
		}
		resultErr = errors.Join(resultErr, operation.Close())
	}()
	path := filepath.FromSlash(snapshot.relativePath)
	verify := func(work context.Context) error {
		return verifyVideoSeekIngestSource(work, snapshot, lease, path, file, before)
	}
	var indexes []media.VideoSeekIndex
	if err := operation.Run(ctx, snapshot.root.id, primaryio.Background, func(work context.Context) (readErr error) {
		var err error
		lease, err = s.leaseLibraryRootContext(work, snapshot.root)
		if err != nil {
			return err
		}
		root, err := lease.Open()
		if err != nil {
			return err
		}
		defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, root)) }()
		parent, err := openRegisteredRoot(root, filepath.Dir(path))
		if err != nil {
			return errors.Join(ErrSourceChanged, err)
		}
		defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, parent)) }()
		name := filepath.Base(path)
		before, err = parent.Lstat(name)
		if err != nil || !snapshot.matches(before) {
			return errors.Join(ErrSourceChanged, err)
		}
		file, err = openScanFile(parent, name)
		if err != nil {
			current, statErr := parent.Lstat(name)
			if statErr != nil || !sameMediaSourceFile(before, current) {
				return errors.Join(ErrSourceChanged, err, statErr)
			}
			return err
		}
		if err := verify(work); err != nil {
			return err
		}
		indexes, err = analyze(work, file, *snapshot.mediaFile.Item.Media)
		if errors.Is(err, media.ErrProcessRetirementUnknown) {
			// The actual child and its inherited descriptor may still be alive.
			retained := media.SourceReadRetirementError(err, file)
			file = nil
			return operation.MarkUnknown(retained)
		}
		if err != nil {
			return errors.Join(err, verify(work))
		}
		return verify(work)
	}); err != nil {
		return false, err
	}
	var encoded any
	if len(indexes) != 0 {
		identity, err := media.VideoSeekSourceIdentity(before)
		if err != nil || len(indexes) != 1 || indexes[0].SourceIdentity != identity || media.ValidateVideoSeekIndex(indexes[0]) != nil {
			return false, ErrSourceChanged
		}
		data, err := json.Marshal(indexes)
		// PostgreSQL's canonical JSON includes spaces. Leave room for them without
		// accepting a larger payload than the planner's existing byte budget.
		if err == nil && len(data) <= media.MaxVideoSeekIndexBytes*3/4 {
			encoded = data
		}
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := checkVideoSeekIngestTarget(ctx, tx, grant, target, snapshot, true); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE video_seek_ingest SET indexes=$2,retry_at=CASE WHEN $2::jsonb IS NULL
		THEN clock_timestamp()+interval '24 hours' ELSE 'infinity' END WHERE item_id=$1`, target.itemID, encoded); err != nil {
		return false, err
	}
	deadline, _ := tx.(*ownedTx).ctx.Deadline()
	bounded, cancel := context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	if err := operation.RunImmediate(bounded, snapshot.root.id, primaryio.Background, func(work context.Context) error {
		return runIngestSourceProof(work, &proofWorkers, verify)
	}); err != nil {
		return false, err
	}
	if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
		return false, err
	}
	return encoded != nil, tx.Commit(ctx)
}

func verifyVideoSeekIngestSource(ctx context.Context, snapshot indexedMediaSource, lease *libraryRootLease,
	path string, file *os.File, expected os.FileInfo) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := lease.Open()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	parent, err := openRegisteredRoot(root, filepath.Dir(path))
	if err != nil {
		return errors.Join(ErrSourceChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, parent)) }()
	named, namedErr := parent.Lstat(filepath.Base(path))
	held, heldErr := file.Stat()
	if namedErr != nil || heldErr != nil || !snapshot.matches(named) || !snapshot.matches(held) ||
		!sameMediaSourceFile(expected, named) || !sameMediaSourceFile(expected, held) {
		return errors.Join(ErrSourceChanged, namedErr, heldErr)
	}
	return ctx.Err()
}

// IngestedVideoSeekIndexes reads only precomputed private evidence for an
// already-authorized source. A cache miss or error leaves normal playback
// planning intact, without opening a different source or running a tool.
func (s *Store) IngestedVideoSeekIndexes(ctx context.Context, source MediaFile) []media.VideoSeekIndex {
	proof := source.primaryReadProof
	if s == nil || !s.deferVideoSeek || proof == nil || proof.store != s || proof.itemID != source.Item.ID ||
		proof.libraryID != source.Item.LibraryID || proof.sourceID != source.SourceID || proof.etag != source.ETag || proof.publicationRevision == "" ||
		(source.Item.Type != "Movie" && source.Item.Type != "Episode") {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	var raw []byte
	if err := s.pool.QueryRow(bounded, `SELECT q.indexes FROM video_seek_ingest q JOIN items i ON i.id=q.item_id
		WHERE q.item_id=$1 AND q.source_revision=$2 AND q.index_version=$3 AND q.indexes IS NOT NULL
		AND q.source_revision=`+MediaOperationSourceRevisionSQL+` AND `+ordinaryItemSQL("i"), proof.itemID, proof.publicationRevision,
		media.VideoSeekIndexVersion).Scan(&raw); err != nil || len(raw) > media.MaxVideoSeekIndexBytes {
		return nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || len(entries) != 1 {
		return nil
	}
	index, err := media.ParseVideoSeekIndex(entries[0])
	if err != nil {
		return nil
	}
	return []media.VideoSeekIndex{index}
}
