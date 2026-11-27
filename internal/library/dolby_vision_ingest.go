package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/systemevents"
)

const TaskDolbyVisionVerificationKey = "media.verify_dolby_vision"

const (
	dolbyVisionBatchSize     = 8
	dolbyVisionBatchTime     = 90 * time.Second
	dolbyVisionLookupTimeout = 100 * time.Millisecond
)

type dolbyVisionVerifier struct {
	executable, resolved, identity string
	deferred                       bool
}

// DolbyVisionVerifierIdentity binds evidence to the installed regular executable
// without reading its contents on the scan or the playback path. The media
// runtime and its loader dependencies are deployment-owned immutable inputs.
func DolbyVisionVerifierIdentity(executable string) (resolved, identity string, err error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	id, change := fileIdentity(info), media.FileChangeTime(info)
	if id == "" || change <= 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", "", ErrUnavailable
	}
	return path, fmt.Sprintf("ffmpeg-file-v1:%s:%d:%d:%d", id, info.Size(), info.ModTime().UnixNano(), change), nil
}

// WithDolbyVisionVerifier binds the installed tool for existing ingest work.
// Only opted-in scans may skip verification and enqueue new work.
func WithDolbyVisionVerifier(executable string, deferScans bool) (Option, bool) {
	resolved, identity, err := DolbyVisionVerifierIdentity(executable)
	if err != nil {
		return nil, false
	}
	return func(settings *storeOptions) error {
		settings.dolbyVisionVerifier = dolbyVisionVerifier{executable, resolved, identity, deferScans}
		return nil
	}, true
}

func (s *Store) DolbyVisionVerificationConfigured() bool {
	return s != nil && s.dolbyVisionVerifier.identity != ""
}

func (v dolbyVisionVerifier) current() bool {
	resolved, identity, err := DolbyVisionVerifierIdentity(v.executable)
	return err == nil && resolved == v.resolved && identity == v.identity
}

func (state *scanState) pendingDolbyVision(itemType string, info *media.Info) []int {
	if !state.store.dolbyVisionVerifier.deferred || state.task.job.ForceProbe || !scanIngestVideoLibrary(state.library) ||
		(itemType != "Movie" && itemType != "Episode") || info == nil {
		return nil
	}
	return media.PendingDolbyVisionStreams(*info)
}

// A new successful scan can authorize the same pending work, but a warm scan
// never resets positive/negative results or the retry budget of the same source.
func (state *scanState) queueDolbyVisionVerification(tx pgx.Tx, itemID string, indexes []int) error {
	if len(indexes) == 0 {
		return nil
	}
	_, err := tx.Exec(state.task.ctx, `INSERT INTO dolby_vision_verifications
		(item_id,stream_index,scan_job_id,source_revision,verifier_version,tool_identity)
		SELECT i.id,s.stream_index,$2,`+MediaOperationSourceRevisionSQL+`,$3,$4
		FROM items i CROSS JOIN unnest($5::integer[]) s(stream_index) WHERE i.id=$1
		ON CONFLICT(item_id,stream_index) DO UPDATE SET scan_job_id=EXCLUDED.scan_job_id,
		source_revision=EXCLUDED.source_revision,verifier_version=EXCLUDED.verifier_version,tool_identity=EXCLUDED.tool_identity,
		evidence=NULL,verified_at=NULL,
		attempts=CASE WHEN `+sameQueuedDolbyVisionSource+` THEN dolby_vision_verifications.attempts ELSE 0 END,
		retry_at=CASE WHEN `+sameQueuedDolbyVisionSource+` THEN dolby_vision_verifications.retry_at ELSE '-infinity' END
		WHERE dolby_vision_verifications.evidence IS NULL OR NOT (`+sameQueuedDolbyVisionSource+`)`,
		itemID, state.task.job.ID, media.DolbyVisionVerificationVersion, state.store.dolbyVisionVerifier.identity, indexes)
	return err
}

const sameQueuedDolbyVisionSource = `(dolby_vision_verifications.source_revision,dolby_vision_verifications.verifier_version,dolby_vision_verifications.tool_identity)
	IS NOT DISTINCT FROM (EXCLUDED.source_revision,EXCLUDED.verifier_version,EXCLUDED.tool_identity)`

const dolbyVisionRetryableTerminalSQL = `(q.attempts=3 AND q.evidence->>'Reason' IN ('scan_timeout','scan_failed'))`
const dolbyVisionOrdinarySourceSQL = `i.type IN ('Movie','Episode') AND NOT i.is_folder AND i.media IS NOT NULL
	AND NOT EXISTS(SELECT 1 FROM scan_jobs active WHERE active.library_id=i.library_id AND active.status IN ('Queued','Running'))
	AND NOT EXISTS(SELECT 1 FROM media_operations p WHERE p.source_item_id=i.id AND p.publication_phase IN ('prepared','catalog_committed'))`

var dolbyVisionVerificationDueSQL = `j.status='Completed' AND j.library_id=i.library_id AND j.finished_at<=run.created_at
	AND q.verifier_version=$2 AND q.source_revision=` + MediaOperationSourceRevisionSQL + ` AND ` + dolbyVisionOrdinarySourceSQL + `
	AND ((q.evidence IS NULL AND (q.retry_at<=clock_timestamp() OR run.source IN ('manual','compatibility')))
	OR (run.source IN ('manual','compatibility') AND ` + dolbyVisionRetryableTerminalSQL + `))
	AND ` + ordinaryItemSQL("i") + ` AND ` + scanIngestAutomaticGenerationSQL

// SnapshotDolbyVisionVerificationChildren admits the least recently served
// ready library for automatic runs so the shared analysis slot can rotate.
// Explicit manual runs accept every eligible library, with one bounded turn per child.
func SnapshotDolbyVisionVerificationChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`WITH chosen AS (
		SELECT l.id,l.name,(SELECT max(c.created_at) FROM task_run_children c JOIN task_runs previous ON previous.id=c.run_id
			WHERE c.library_id=l.id AND previous.task_key=run.task_key) AS last_served
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND l.collection_type IN ('movies','tvshows') AND EXISTS(
			SELECT 1 FROM dolby_vision_verifications q JOIN scan_jobs j ON j.id=q.scan_job_id JOIN items i ON i.id=q.item_id
			WHERE i.library_id=l.id AND `+dolbyVisionVerificationDueSQL+`)
		ORDER BY last_served NULLS FIRST,l.id
		LIMIT (SELECT CASE WHEN source IN ('manual','compatibility') THEN NULL ELSE 1 END FROM task_runs WHERE id=$1)
	)
	INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
	SELECT md5($1||':'||id),$1,id,name,(row_number() OVER(ORDER BY last_served NULLS FIRST,id)-1)::integer FROM chosen`,
		runID, media.DolbyVisionVerificationVersion)
	return tag.RowsAffected(), err
}

type dolbyVisionTarget struct {
	itemID, jobID, revision, tool string
	stream, attempts              int
}

type dolbyVisionAnalyze func(context.Context, string, *os.File, media.Info, int) (media.DolbyVisionRPUEvidence, error)

// VerifyIngestedDolbyVision opens accepted sources under a new task grant. It
// never invokes ffprobe again, writes media, or takes over the scan's input.
func (s *Store) VerifyIngestedDolbyVision(ctx context.Context, childID, libraryID, executable string, fence AnalysisFence, report func(ProviderProgress) error) error {
	if !s.DolbyVisionVerificationConfigured() || executable != s.dolbyVisionVerifier.executable || !s.dolbyVisionVerifier.current() {
		return ErrUnavailable
	}
	return s.verifyIngestedDolbyVision(ctx, childID, libraryID, fence, report, media.VerifyDolbyVisionIngest)
}

func (s *Store) verifyIngestedDolbyVision(ctx context.Context, childID, libraryID string, fence AnalysisFence,
	report func(ProviderProgress) error, analyze dolbyVisionAnalyze) (resultErr error) {
	if ctx == nil || fence == nil || report == nil || analyze == nil || !metadataIdentifier(libraryID) {
		return ErrInvalidInput
	}
	var runID string
	var manual bool
	check := func(tx OwnedTx) error {
		if err := fence(tx); err != nil || runID != "" {
			return err
		}
		return tx.QueryRow(`SELECT run.id,run.source IN ('manual','compatibility') FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
			WHERE child.id=$1 AND child.library_id=$2 AND run.task_key=$3`, childID, libraryID, TaskDolbyVisionVerificationKey).Scan(&runID, &manual)
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
	started := time.Now()
	rows, err := s.pool.Query(ctx, `SELECT q.item_id,q.scan_job_id,q.source_revision,q.tool_identity,q.stream_index,
		CASE WHEN q.evidence IS NULL THEN q.attempts ELSE 0 END
		FROM task_runs run CROSS JOIN dolby_vision_verifications q JOIN scan_jobs j ON j.id=q.scan_job_id JOIN items i ON i.id=q.item_id
		WHERE run.id=$1 AND i.library_id=$3 AND `+dolbyVisionVerificationDueSQL+`
		ORDER BY q.retry_at,q.item_id,q.stream_index LIMIT $4`, runID, media.DolbyVisionVerificationVersion, libraryID, dolbyVisionBatchSize)
	if err != nil {
		return err
	}
	page, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (target dolbyVisionTarget, err error) {
		err = row.Scan(&target.itemID, &target.jobID, &target.revision, &target.tool, &target.stream, &target.attempts)
		return
	})
	if err != nil {
		return err
	}
	var progress ProviderProgress
	var advanced bool
	for _, target := range page {
		accepted, verified, err := s.verifyDolbyVisionTarget(ctx, grant, target, manual, analyze)
		if err != nil && !videoSeekIngestDeferred(err) {
			return err
		}
		advanced = advanced || accepted
		if verified {
			progress.Updated++
		}
		progress.Processed++
		if err := report(progress); err != nil {
			return err
		}
		// Yield only between streams, without truncating either native pass.
		if time.Since(started) >= dolbyVisionBatchTime {
			break
		}
	}
	return s.continueDolbyVisionVerification(ctx, grant, runID, !manual && advanced)
}

func (s *Store) continueDolbyVisionVerification(ctx context.Context, grant *internalTaskSourceGrant, runID string, advanced bool) error {
	if !advanced {
		return ctx.Err()
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := grant.fence(tx); err != nil {
			return err
		}
		var pending bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_runs run CROSS JOIN dolby_vision_verifications q
			JOIN items i ON i.id=q.item_id JOIN libraries l ON l.id=i.library_id JOIN scan_jobs j ON j.id=q.scan_job_id
			WHERE run.id=$1 AND l.collection_type IN ('movies','tvshows') AND `+dolbyVisionVerificationDueSQL+`)`,
			runID, media.DolbyVisionVerificationVersion).Scan(&pending); err != nil {
			return err
		}
		if pending {
			if err := systemevents.Record(tx.Exec, systemevents.DolbyVisionVerificationRequested); err != nil {
				return err
			}
		}
		return grant.fence(tx)
	})
}

func checkDolbyVisionTarget(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant,
	target dolbyVisionTarget, snapshot indexedMediaSource, manual, lock bool) error {
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
	statement := `SELECT q.source_revision FROM dolby_vision_verifications q JOIN items i ON i.id=q.item_id
		JOIN scan_jobs j ON j.id=q.scan_job_id JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE q.item_id=$1 AND q.stream_index=$2 AND q.scan_job_id=$3 AND q.source_revision=$4 AND q.tool_identity=$5 AND q.verifier_version=$6
		AND j.status='Completed' AND j.library_id=$7 AND i.library_id=$7 AND i.root_id=$8 AND r.binding_revision=$9
		AND ((q.evidence IS NULL AND q.attempts=$10 AND (q.retry_at<=clock_timestamp() OR $11)) OR ($11 AND ` + dolbyVisionRetryableTerminalSQL + `))
		AND q.source_revision=` + MediaOperationSourceRevisionSQL + ` AND ` + dolbyVisionOrdinarySourceSQL + ` AND ` + ordinaryItemSQL("i")
	if lock {
		statement += ` FOR UPDATE OF i,q`
	}
	var revision string
	err := tx.QueryRow(ctx, statement, target.itemID, target.stream, target.jobID, target.revision, target.tool,
		media.DolbyVisionVerificationVersion, grant.libraryID, snapshot.root.id, snapshot.rootBindingRevision, target.attempts, manual).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSourceChanged
	}
	return err
}

func (s *Store) readDolbyVisionTarget(ctx context.Context, grant *internalTaskSourceGrant, target dolbyVisionTarget, manual bool) (indexedMediaSource, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer rollback(tx)
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), target.itemID, "")
	if err != nil {
		return indexedMediaSource{}, err
	}
	root := grant.roots[snapshot.root.id]
	if root.root != snapshot.root || root.revision != snapshot.rootBindingRevision || root.root.libraryID != grant.libraryID {
		return indexedMediaSource{}, ErrSourceChanged
	}
	if err := checkDolbyVisionTarget(ctx, tx, grant, target, snapshot, manual, false); err != nil {
		return indexedMediaSource{}, err
	}
	return snapshot, tx.Commit(ctx)
}

func (s *Store) verifyDolbyVisionTarget(ctx context.Context, grant *internalTaskSourceGrant, target dolbyVisionTarget,
	manual bool, analyze dolbyVisionAnalyze) (accepted, verified bool, resultErr error) {
	snapshot, err := s.readDolbyVisionTarget(ctx, grant, target, manual)
	if err != nil {
		return false, false, err
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: snapshot.root, bindingRevision: snapshot.rootBindingRevision}})
	if err != nil {
		return false, false, err
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
	var evidence media.DolbyVisionRPUEvidence
	verifier := s.dolbyVisionVerifier
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
			return err
		}
		if err := verify(work); err != nil {
			return err
		}
		if !verifier.current() {
			return ErrUnavailable
		}
		evidence, err = analyze(work, verifier.resolved, file, *snapshot.mediaFile.Item.Media, target.stream)
		if errors.Is(err, media.ErrProcessRetirementUnknown) {
			retained := media.SourceReadRetirementError(err, file)
			file = nil
			return operation.MarkUnknown(retained)
		}
		if !verifier.current() {
			err = errors.Join(err, ErrUnavailable)
		}
		return errors.Join(err, verify(work))
	}); err != nil {
		return false, false, err
	}
	if evidence.StreamIndex != target.stream || !evidence.Valid() {
		return false, false, fmt.Errorf("%w: invalid Dolby Vision verification result", ErrUnavailable)
	}

	attempt := target.attempts + 1
	if target.tool != verifier.identity {
		attempt = 1
	}
	var value any
	if !evidence.Retryable() || attempt >= 3 {
		value = evidence
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := checkDolbyVisionTarget(ctx, tx, grant, target, snapshot, manual, true); err != nil {
		return false, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE dolby_vision_verifications SET evidence=$3,attempts=$4,tool_identity=$5,
		retry_at=CASE WHEN $3::jsonb IS NOT NULL THEN 'infinity' ELSE clock_timestamp()+($4::smallint * interval '15 minutes') END,
		verified_at=CASE WHEN $6 THEN clock_timestamp() END WHERE item_id=$1 AND stream_index=$2`,
		target.itemID, target.stream, value, attempt, verifier.identity, evidence.Verified); err != nil {
		return false, false, err
	}
	if value != nil {
		if err := signalDolbyVisionPreview(ctx, tx, target.itemID, target.stream, evidence.Verified); err != nil {
			return false, false, err
		}
	}
	deadline, _ := tx.(*ownedTx).ctx.Deadline()
	bounded, cancel := context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	if err := operation.RunImmediate(bounded, snapshot.root.id, primaryio.Background, func(work context.Context) error {
		return runIngestSourceProof(work, &proofWorkers, verify)
	}); err != nil {
		return false, false, err
	}
	if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
		return false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, evidence.Verified, nil
}

// IngestedDolbyVisionPlanning reads private evidence for this exact authorized
// plan source. A preview must distinguish blocked work from a failed SQL read.
func (s *Store) IngestedDolbyVisionPlanning(ctx context.Context, source MediaFile, streamIndices ...int) (evidence []media.DolbyVisionRPUEvidence, waiting []int, resultErr error) {
	proof := source.primaryReadProof
	if !s.DolbyVisionVerificationConfigured() || proof == nil || proof.store != s || proof.itemID != source.Item.ID ||
		proof.libraryID != source.Item.LibraryID || proof.sourceID != source.SourceID || proof.etag != source.ETag || proof.publicationRevision == "" ||
		(source.Item.Type != "Movie" && source.Item.Type != "Episode") || source.Item.Media == nil {
		return nil, nil, nil
	}
	indices := media.PendingDolbyVisionStreams(*source.Item.Media)
	if len(streamIndices) > 0 {
		indices = slices.DeleteFunc(indices, func(index int) bool { return !slices.Contains(streamIndices, index) })
	}
	if len(indices) == 0 {
		return nil, nil, nil
	}
	bounded, cancel := context.WithTimeout(ctx, dolbyVisionLookupTimeout)
	defer cancel()
	rows, err := s.pool.Query(bounded, `SELECT q.stream_index,q.evidence FROM dolby_vision_verifications q
		JOIN items i ON i.id=q.item_id JOIN scan_jobs j ON j.id=q.scan_job_id
		WHERE i.id=$1 AND i.library_id=$2 AND i.root_id=$3 AND q.source_revision=$4 AND q.verifier_version=$5 AND q.stream_index=ANY($6::integer[])
		AND j.library_id=i.library_id AND (q.evidence IS NULL OR (j.status='Completed' AND q.tool_identity=$7 AND q.verified_at IS NOT NULL))
		AND q.source_revision=`+MediaOperationSourceRevisionSQL+` AND `+ordinaryItemSQL("i"), proof.itemID, proof.libraryID, proof.hint.root.id,
		proof.publicationRevision, media.DolbyVisionVerificationVersion, indices, s.dolbyVisionVerifier.identity)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var index int
		var raw []byte
		if err := rows.Scan(&index, &raw); err != nil {
			return nil, nil, err
		}
		if raw == nil {
			waiting = append(waiting, index)
			continue
		}
		var entry media.DolbyVisionRPUEvidence
		if len(raw) <= 2048 && json.Unmarshal(raw, &entry) == nil && entry.StreamIndex == index && entry.Verified && entry.Valid() {
			evidence = append(evidence, entry)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return evidence, waiting, nil
}

// IngestedDolbyVisionEvidence is an optional bounded playback lookup. No miss
// or error launches analysis, alters the source, or allows unproved encoding.
func (s *Store) IngestedDolbyVisionEvidence(ctx context.Context, source MediaFile) []media.DolbyVisionRPUEvidence {
	evidence, _, _ := s.IngestedDolbyVisionPlanning(ctx, source)
	return evidence
}
