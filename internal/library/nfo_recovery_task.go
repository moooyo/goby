package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/systemevents"
)

const nfoRecoveryDueSQL = `w.status IN ('pending','verify_outcome') AND w.retry_at<=clock_timestamp() AND w.confirmed_at<=run.created_at
	AND q.import_nfo AND j.library_id=i.library_id AND j.status='Completed' AND j.finished_at<=run.created_at
	AND i.type IN ('Movie','Episode','Series','Season') AND w.source_key=q.source_key
	AND q.source_key=` + metadataIngestSourceKeySQL + ` AND w.directory_identity=q.directory_identity
	AND w.media_change_ns=COALESCE((i.media->>'FileChangeTimeNs')::bigint,0)
	AND NOT EXISTS(SELECT 1 FROM scan_jobs active WHERE active.library_id=i.library_id AND active.status IN ('Queued','Running'))
	AND ` + nfoRecoveryStableRootSQL + ` AND ` + scanIngestAutomaticGenerationSQL

// A prepared sibling can temporarily disappear and then be rolled back to the
// same NFO basename. Do not claim that an incomplete media journal is absence.
const nfoRecoveryStableRootSQL = `NOT EXISTS(SELECT 1 FROM media_operations p
	WHERE p.source_root_id=r.id AND p.publication_phase IN ('prepared','catalog_committed'))
	AND NOT EXISTS(SELECT 1 FROM media_deletion_operations d
	WHERE d.library_id=r.library_id AND d.root_id=r.id AND d.kind='media' AND d.state='prepared')`

const nfoRecoveryTables = `local_nfo_recovery_work w JOIN local_metadata_ingest q ON q.item_id=w.item_id
	JOIN scan_jobs j ON j.id=q.scan_job_id JOIN items i ON i.id=w.item_id
	JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id`

var nfoRecoveryDueItem = nfoRecoveryDueSQL + ` AND ` + ordinaryItemSQL("i")

// SnapshotNFORecoveryChildren admits only confirmed, due work from completed
// scans. The new executor gets its own live source grant, not the scan's.
func SnapshotNFORecoveryChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER(ORDER BY l.id)-1)::integer
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND l.collection_type IN ('movies','tvshows')
		AND COALESCE((l.options->>'EnableLocalMetadata')::boolean,true)
		AND EXISTS(SELECT 1 FROM `+nfoRecoveryTables+` WHERE i.library_id=l.id AND `+nfoRecoveryDueItem+`)`, runID)
	return tag.RowsAffected(), err
}

type nfoRecoveryClaim struct {
	nfoRecoveryEvidence
	attempt    int
	verifyOnly bool
	reservedTo time.Time
}

func (s *Store) RegenerateLocalNFO(ctx context.Context, childID, libraryID string, fence AnalysisFence, report func(ProviderProgress) error) (resultErr error) {
	if ctx == nil || fence == nil || report == nil || !metadataIdentifier(libraryID) {
		return ErrInvalidInput
	}
	var runID string
	check := func(tx OwnedTx) error {
		if err := fence(tx); err != nil || runID != "" {
			return err
		}
		return tx.QueryRow(`SELECT run.id FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
			WHERE child.id=$1 AND child.library_id=$2 AND run.task_key=$3`, childID, libraryID, TaskNFORecoveryKey).Scan(&runID)
	}
	ctx, release, err := s.BeginTaskSourceOperation(systemevents.WithDerived(ctx), childID, check)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	grant := taskSourceGrant(ctx)
	if grant.libraryID != libraryID {
		return ErrInvalidInput
	}
	library, err := s.GetLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	if !scanIngestVideoLibrary(library) || !EffectiveLibraryOptions(library).EnableLocalMetadata {
		return nil
	}
	var progress ProviderProgress
	var noSource, writeDisabled, retrying bool
	const pageSize = 32
	for after := ""; ; {
		rows, err := s.pool.Query(ctx, `SELECT i.id,q.scan_job_id FROM task_runs run CROSS JOIN `+nfoRecoveryTables+`
			WHERE run.id=$1 AND i.library_id=$2 AND w.item_id>$3 AND `+nfoRecoveryDueItem+`
			ORDER BY w.item_id LIMIT $4`, runID, libraryID, after, pageSize)
		if err != nil {
			return err
		}
		type key struct{ id, job string }
		page, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (k key, err error) {
			err = row.Scan(&k.id, &k.job)
			return
		})
		if err != nil {
			return err
		}
		for _, k := range page {
			after = k.id
			target, err := readMetadataIngestTarget(s.pool.QueryRow(ctx, metadataIngestTargetReadOne, libraryID, k.id, k.job))
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			outcome, wrote, err := s.regenerateNFOTarget(ctx, grant, library, target, runID)
			if err != nil {
				if localNFOFatalReadError(ctx, err) != nil || errors.Is(err, errSidecarRollbackUnknown) ||
					!metadataIngestUncertainOnly(err) && !sidecarAdmissionRetryable(err) &&
						!errors.Is(err, errStorageObservationUnavailable) && !errors.Is(err, errNFORecoveryPublicationUncertain) {
					return err
				}
				retrying = retrying || outcome == "pending"
			}
			retrying = retrying || outcome == "retry_exhausted"
			noSource = noSource || outcome == "no_source"
			writeDisabled = writeDisabled || outcome == "write_disabled"
			progress.Processed++
			if wrote {
				progress.Updated++
			}
			if err := report(progress); err != nil {
				return err
			}
		}
		if len(page) < pageSize {
			break
		}
	}
	var warnings []error
	if noSource {
		warnings = append(warnings, ErrNFORecoveryNoSource)
	}
	if writeDisabled {
		warnings = append(warnings, ErrNFORecoveryWriteDisabled)
	}
	if retrying {
		warnings = append(warnings, ErrNFORecoveryIncomplete)
	}
	return errors.Join(warnings...)
}

// Reserve before touching storage, so crashes and lost acknowledgments cannot
// reset the attempt budget. Interval/startup triggers pick up due reservations.
func claimNFORecovery(ctx context.Context, tx pgx.Tx, target metadataIngestTarget, runID string) (nfoRecoveryClaim, error) {
	var claim nfoRecoveryClaim
	err := tx.QueryRow(ctx, `WITH due AS (
		SELECT w.item_id,w.status='verify_outcome' AS verify_only
		FROM local_nfo_recovery_work w JOIN local_metadata_ingest q ON q.item_id=w.item_id CROSS JOIN task_runs run
		WHERE w.item_id=$1 AND run.id=$2 AND w.source_key=q.source_key AND w.directory_identity=q.directory_identity
		AND w.media_change_ns=$3 AND q.import_nfo AND w.status IN ('pending','verify_outcome')
		AND w.retry_at<=clock_timestamp() AND w.confirmed_at<=run.created_at FOR UPDATE OF w
	)
	UPDATE local_nfo_recovery_work w SET attempts=LEAST(w.attempts+1,3),
	retry_at=clock_timestamp()+(CASE WHEN due.verify_only THEN 1 ELSE w.attempts+1 END)*INTERVAL '15 minutes',
	status=CASE WHEN w.attempts>=2 THEN 'verify_outcome' ELSE 'pending' END
	FROM due WHERE w.item_id=due.item_id
	RETURNING w.directory_identity,w.root_identity,w.relative_path,w.media_change_ns,w.version,w.reason,w.attempts,due.verify_only,w.retry_at`,
		target.id, runID, target.changeNS).Scan(&claim.directory, &claim.root, &claim.path, &claim.changeNS, &claim.version, &claim.reason,
		&claim.attempt, &claim.verifyOnly, &claim.reservedTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nfoRecoveryClaim{}, errMetadataIngestChanged
	}
	return claim, err
}

const nfoRecoveryReservedSQL = `w.item_id=$1 AND w.version=$2 AND w.attempts=$3 AND w.status=$4
	AND w.directory_identity=$5 AND w.root_identity=$6 AND w.relative_path=$7 AND w.media_change_ns=$8 AND w.retry_at=$9
	AND EXISTS(SELECT 1 FROM local_metadata_ingest q WHERE q.item_id=w.item_id AND q.source_key=w.source_key AND q.import_nfo)`

func (claim nfoRecoveryClaim) reservedArguments(itemID string) []any {
	status := "pending"
	if claim.attempt == 3 {
		status = "verify_outcome"
	}
	return []any{itemID, claim.version, claim.attempt, status, claim.directory, claim.root, claim.path, claim.changeNS, claim.reservedTo}
}

func (claim nfoRecoveryClaim) checkReservation(ctx context.Context, tx pgx.Tx, target metadataIngestTarget) error {
	var locked bool
	err := tx.QueryRow(ctx, `SELECT true FROM local_nfo_recovery_work w WHERE `+nfoRecoveryReservedSQL+` FOR UPDATE`,
		claim.reservedArguments(target.id)...).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return errMetadataIngestChanged
	}
	return err
}

func (claim nfoRecoveryClaim) setOutcome(ctx context.Context, tx pgx.Tx, target metadataIngestTarget, status string) error {
	tag, err := tx.Exec(ctx, `UPDATE local_nfo_recovery_work w SET status=$10,retry_at='infinity' WHERE `+nfoRecoveryReservedSQL,
		append(claim.reservedArguments(target.id), status)...)
	if err == nil && tag.RowsAffected() != 1 {
		return errMetadataIngestChanged
	}
	return err
}

func (s *Store) readNFORecoveryDocument(ctx context.Context, target metadataIngestTarget, claim nfoRecoveryClaim) ([]byte, string, error) {
	var data []byte
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT saved.document,saved.document_hash FROM local_nfo_recovery_sources saved
		JOIN scan_jobs accepted ON accepted.id=saved.scan_job_id JOIN local_metadata_ingest q ON q.item_id=saved.item_id
		JOIN items i ON i.id=saved.item_id JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE saved.item_id=$1 AND q.scan_job_id=$2 AND saved.source_key=`+nfoRecoveryPhysicalSourceKeySQL+`
		AND (saved.directory_identity,saved.root_identity,saved.relative_path,saved.media_change_ns)=($3,$4,$5,$6)
		AND accepted.status='Completed' AND accepted.library_id=i.library_id AND i.local_metadata_hash=saved.document_hash
		AND i.local_metadata_path=saved.relative_path`, target.id, target.jobID,
		claim.directory, claim.root, claim.path, claim.changeNS).Scan(&data, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	return data, hash, err
}

func (s *Store) regenerateNFOTarget(ctx context.Context, grant *internalTaskSourceGrant, library Library,
	target metadataIngestTarget, runID string) (_ string, _ bool, resultErr error) {
	row := grant.roots[target.rootID]
	path, valid := target.sourcePath(row.root)
	if !valid || !target.request.nfo {
		return "", false, errMetadataIngestChanged
	}
	candidates, kind := target.nfoCandidates(path)
	if kind == "" {
		return "", false, errMetadataIngestChanged
	}
	check := func(work context.Context, tx pgx.Tx) error {
		if err := s.checkMetadataIngestTarget(work, tx, grant, library, row, target, false); err != nil {
			return err
		}
		var revision int64
		var stable bool
		if err := tx.QueryRow(work, `SELECT binding_revision,`+nfoRecoveryStableRootSQL+` FROM library_roots r WHERE r.id=$1`, row.root.id).
			Scan(&revision, &stable); err != nil {
			return err
		}
		if revision != row.revision {
			return ErrRootBindingConflict
		}
		if !stable {
			return errMetadataIngestChanged
		}
		return nil
	}
	directory := target.directoryPath()
	writeConfigured := s.nfoRecoveryWritable(row.root)
	var exclusiveDirectory bool
	var claim nfoRecoveryClaim
	if err := s.finishMetadataIngest(ctx, check, func(work context.Context, tx pgx.Tx) error {
		var err error
		claim, err = claimNFORecovery(work, tx, target, runID)
		if err != nil || claim.verifyOnly || !writeConfigured {
			return err
		}
		exclusiveDirectory, err = ownsNFORecoveryDirectory(work, tx, row.root, directory)
		return err
	}); err != nil {
		return "", false, err
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		return "pending", false, err
	}
	var lease *libraryRootLease
	var dir *os.Root
	var stage *nfoRecoveryStage
	var proofWorkers sync.WaitGroup
	defer func() {
		// A slow filesystem can outlive the catalog proof deadline. Rollback
		// completes first, but the live worker retains this task and its files.
		proofWorkers.Wait()
		cleanup := operation.Context(ctx)
		if stage != nil {
			resultErr = errors.Join(resultErr, stage.close(cleanup))
		}
		if dir != nil {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(cleanup, dir))
		}
		if lease != nil {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(cleanup, lease.Close))
		}
		resultErr = errors.Join(resultErr, operation.Close())
	}()
	var observation localNFOObservation
	var proof nfoIngestProof
	verify := func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		held, err := dir.Stat(".")
		if err != nil || !held.IsDir() || fileIdentity(held) != claim.directory {
			return errors.Join(errMetadataIngestChanged, err)
		}
		return verifyNFORecoveryLocation(work, lease, directory, path, target, claim)
	}
	if err := operation.Run(ctx, target.rootID, primaryio.Background, func(work context.Context) (readErr error) {
		var err error
		lease, err = s.leaseLibraryRootContext(work, row.root)
		if err != nil {
			return err
		}
		root, err := lease.Open()
		if err != nil {
			return err
		}
		defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, root)) }()
		dir, err = openRegisteredRoot(root, directory)
		if err != nil {
			return errors.Join(errMetadataIngestChanged, err)
		}
		if err := verify(work); err != nil {
			return err
		}
		observation, proof, err = observeMetadataIngestNFO(work, dir, directory, candidates)
		return err
	}); err != nil {
		return "pending", false, err
	}
	if observation.invalid {
		return "pending", false, errMetadataIngestChanged
	}
	version := nfoRecoveryVersion(observation, proof)
	if observation.path != "" && version == "" {
		return "pending", false, errMetadataIngestChanged
	}
	status := "awaiting_import"
	var pairSource episodePairSource
	var binding *episodePairBinding
	var sharedDirectory, sharedLeaf bool
	if version == claim.version && observation.path == claim.path {
		switch {
		case claim.verifyOnly:
			status = "retry_exhausted"
		case !writeConfigured || !primaryNFORecoveryCandidate(path, target.folder, target.kind, claim.path):
			status = "write_disabled"
		case !exclusiveDirectory:
			status, sharedDirectory = "write_disabled", true
		default:
			data, hash, err := s.readNFORecoveryDocument(ctx, target, claim)
			if err != nil {
				return "pending", false, err
			}
			if target.kind == "Episode" {
				file := episodePairFile{target.fileIdentity, target.fileSize, target.modified.UnixNano(), target.changeNS}
				pairSource = newEpisodePairSource(row, path, target.parentID, claim.root, target.request, file)
				binding, err = pairSource.readBinding(ctx, s.pool, false)
				if err != nil {
					return "pending", false, err
				}
			}
			local, err := parseObservedNFO(localNFOObservation{path: claim.path, data: data}, kind, binding)
			if err != nil || local.hash != hash || !trustedNFORecoveryDocument(path, target.folder, target.kind, target.request, local, data) {
				status = "no_source"
			} else if err := operation.Run(ctx, target.rootID, primaryio.Background, func(work context.Context) error {
				if err := verify(work); err != nil {
					return err
				}
				unique, err := uniqueNFORecoveryCandidate(work, dir, path, claim.path, target.folder)
				if err != nil {
					return err
				}
				if !unique {
					status, sharedLeaf = "write_disabled", true
					return nil
				}
				stage, err = stageNFORecovery(work, dir, data)
				return err
			}); err != nil {
				return "pending", false, err
			}
		}
	}
	leaf := filepath.Base(claim.path)
	recordOutcome := func(work context.Context, tx pgx.Tx) error {
		if err := claim.setOutcome(work, tx, target, status); err != nil {
			return err
		}
		if status == "awaiting_import" {
			if err := systemevents.Record(taskSourcePublicationTx{ctx: work, tx: tx}.Exec, systemevents.LocalMetadataIngestRequested); err != nil {
				return err
			}
		}
		return checkTaskIngestFence(work, tx, grant)
	}
	prove := func(work context.Context) error {
		if err := verify(work); err != nil {
			return err
		}
		if err := proof.verifyAt(work, dir); err != nil {
			return err
		}
		if stage != nil || sharedLeaf {
			unique, err := uniqueNFORecoveryCandidate(work, dir, path, claim.path, target.folder)
			if err != nil || unique == sharedLeaf {
				return errors.Join(errMetadataIngestChanged, err)
			}
		}
		if stage != nil {
			current, _, err := observeMetadataIngestNFO(work, dir, directory, candidates)
			if err != nil || current.invalid || current.path != observation.path || !bytes.Equal(current.data, observation.data) {
				return errors.Join(errMetadataIngestChanged, err)
			}
			if err := proof.verifyAt(work, dir); err != nil {
				return err
			}
		}
		return verify(work)
	}
	err = s.finishMetadataIngest(ctx, check, func(work context.Context, tx pgx.Tx) error {
		if stage != nil || sharedDirectory {
			owned, err := ownsNFORecoveryDirectory(work, tx, row.root, directory)
			if err != nil || owned == sharedDirectory {
				return errors.Join(errMetadataIngestChanged, err)
			}
		}
		if stage != nil {
			if err := pairSource.checkBinding(work, tx, binding, errMetadataIngestChanged); err != nil {
				return err
			}
			if err := claim.checkReservation(work, tx, target); err != nil {
				return err
			}
			if err := checkTaskIngestFence(work, tx, grant); err != nil {
				return err
			}
		} else if err := recordOutcome(work, tx); err != nil {
			return err
		}
		deadline, _ := tx.(*ownedTx).ctx.Deadline()
		bounded, cancel := context.WithDeadline(work, deadline.Add(-scanReconciliationRollbackSpace))
		defer cancel()
		return operation.RunImmediate(bounded, target.rootID, primaryio.Background, func(admitted context.Context) error {
			err := runIngestSourceProof(admitted, &proofWorkers, func(proofCtx context.Context) error {
				if stage == nil {
					return prove(proofCtx)
				}
				return stage.checkReplacement(proofCtx, leaf, func() error { return prove(proofCtx) })
			})
			if err != nil || stage == nil {
				return err
			}
			// A started rename must retire before releasing the source fence.
			// Only its nonmutating preflight can detach on a deadline.
			return stage.replace(admitted, leaf)
		})
	})
	if err != nil {
		// A failed rename or DB commit is not permission to redo it blindly.
		return "pending", false, err
	}
	if stage == nil {
		return status, false, nil
	}
	// File/parent synchronization and readback do not retain catalog ownership.
	// Even the final attempt leaves durable observation work until acknowledged.
	if err := operation.Run(ctx, target.rootID, primaryio.Background, func(work context.Context) error {
		if err := stage.verifyPublished(work, leaf); err != nil {
			return err
		}
		if err := verify(work); err != nil {
			return errors.Join(errNFORecoveryPublicationUncertain, err)
		}
		return nil
	}); err != nil {
		return "pending", false, err
	}
	if err := s.finishMetadataIngest(ctx, check, recordOutcome); err != nil {
		return "pending", false, err
	}
	return status, true, nil
}

func verifyNFORecoveryLocation(ctx context.Context, lease *libraryRootLease, directory, path string, target metadataIngestTarget,
	claim nfoRecoveryClaim) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := lease.Open()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	rootInfo, err := root.Stat(".")
	if err != nil || fileIdentity(rootInfo) != claim.root {
		return errors.Join(errMetadataIngestChanged, err)
	}
	dir, err := openRegisteredRoot(root, directory)
	if err != nil {
		return errors.Join(errMetadataIngestChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	info, err := dir.Stat(".")
	if err != nil || fileIdentity(info) != claim.directory {
		return errors.Join(errMetadataIngestChanged, err)
	}
	if !target.folder {
		file, err := dir.Lstat(filepath.Base(path))
		if err != nil || !target.matchesFile(file) {
			return errors.Join(errMetadataIngestChanged, err)
		}
	}
	return ctx.Err()
}

// Two registered roots may otherwise authorize different logical items to
// rewrite the same physical name, even when only one root opted in.
func ownsNFORecoveryDirectory(ctx context.Context, tx pgx.Tx, root libraryRoot, relative string) (bool, error) {
	var exclusive bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM library_roots other
		WHERE other.id<>$1 AND ($2=other.path OR starts_with($2,rtrim(other.path,'/')||'/')))`,
		root.id, filepath.Join(root.path, relative)).Scan(&exclusive)
	return exclusive, err
}

// Scan the exact target directory, including unindexed siblings, so multiple
// video versions or a case collision cannot share write authorization.
func uniqueNFORecoveryCandidate(ctx context.Context, dir *os.Root, path, candidate string, folder bool) (_ bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	listing, err := dir.Open(".")
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, listing.Close)) }()
	const maxDirectoryEntries = 4096
	entries, err := listing.ReadDir(maxDirectoryEntries + 1)
	if err != nil && err != io.EOF {
		return false, err
	}
	if len(entries) > maxDirectoryEntries {
		return false, nil
	}
	leaf := filepath.Base(candidate)
	ownVideo := filepath.Base(path)
	for _, entry := range entries {
		name := entry.Name()
		if name != leaf && strings.EqualFold(name, leaf) || (folder || name != ownVideo) && extensionKind(name) == "video" &&
			strings.EqualFold(strings.TrimSuffix(name, filepath.Ext(name))+".nfo", leaf) {
			return false, nil
		}
	}
	return true, ctx.Err()
}
