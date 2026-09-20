package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
)

// An incomplete generation witness grants no source publication. It preserves
// the private directory and original-source provenance when an unproven entry
// must be left for administrator inspection instead of being unlinked.
func incompleteMediaEditResult(operation MediaOperation, target fileDeletionTarget, host string, capture *mediaEditCapture) (MediaOperationResult, error) {
	if capture == nil || capture.base == nil || capture.base.stageIdentity == "" {
		return MediaOperationResult{}, ErrMediaOperationRecovery
	}
	r := target.File.Root
	witness := struct {
		Version                                                                        string
		OperationID, Host, SourceRevision, StageName, StageIdentity, CandidateIdentity string
		Target                                                                         deletionTargetDocument
	}{Version: "media-edit-incomplete-staging-v1", OperationID: operation.ID, Host: host, SourceRevision: operation.SourceRevision, StageName: capture.base.spec.StageName, StageIdentity: capture.base.stageIdentity,
		Target: deletionTargetDocument{Target: target, Root: deletionRootDocument{r.id, r.libraryID, r.path, r.allowedPath, r.relativePath}}}
	if capture.candidateInfo != nil {
		witness.CandidateIdentity = fileIdentity(capture.candidateInfo)
	}
	encoded, err := json.Marshal(witness)
	if err != nil || len(encoded) > MaxMediaOperationDocumentBytes {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	hash := sha256.Sum256(encoded)
	return MediaOperationResult{Journal: encoded, ResultHash: hex.EncodeToString(hash[:]), Summary: json.RawMessage(`{"UnpublishedCandidateMayRemain":true,"RequiresManualInspection":true,"GenerationComplete":false}`)}, nil
}

func (s *Store) retainIncompleteMediaEdit(ctx context.Context, work MediaOperationWork, result MediaOperationResult) error {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	tag, err := tx.Exec(protected, `UPDATE media_operations SET journal=$3,result_hash=$4,result_summary=$5,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state='running' AND publication_phase='none'`, work.Operation.ID, work.Token, result.Journal, result.ResultHash, result.Summary)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	return tx.Commit(protected)
}

// Only explicit apply/recovery work reaches this observation. A prepared
// journal can describe either side of an interrupted exchange. Both identities
// must be proven; missing, replaced or mixed objects never trigger compensation.
func (s *Store) openMediaEditPublication(ctx context.Context, journal mediaEditJournal, phase string) (*mediaEditCapture, bool, error) {
	target := journal.source()
	if phase == "none" || phase == "" || phase == "prepared" {
		capture, err := s.openMediaEditCapture(ctx, target.File, journal.StageName, journal.StageIdentity, &journal.Candidate)
		if err == nil {
			return capture, false, nil
		}
		if phase != "prepared" {
			return nil, false, err
		}
	}
	if phase != "prepared" && phase != "catalog_committed" {
		return nil, false, ErrMediaOperationState
	}
	capture, err := s.openPublishedMediaEditCapture(ctx, journal)
	if err != nil {
		return nil, false, errors.Join(ErrMediaOperationRecovery, err)
	}
	return capture, true, nil
}

func (s *Store) openPublishedMediaEditCapture(ctx context.Context, journal mediaEditJournal) (_ *mediaEditCapture, resultErr error) {
	target := journal.source()
	spec := target.File
	spec.StageName = journal.StageName
	base := &fileDeletionCapture{store: s, spec: spec}
	capture := &mediaEditCapture{base: base, attempted: true}
	defer func() {
		if resultErr != nil {
			_ = capture.Close()
		}
	}()
	var err error
	base.lease, err = s.leaseLibraryRoot(spec.Root)
	if err != nil {
		return nil, err
	}
	base.root, err = base.lease.Open()
	if err != nil {
		return nil, err
	}
	base.parent, err = openRegisteredRoot(base.root, filepath.Dir(filepath.FromSlash(spec.RelativePath)))
	if err != nil {
		return nil, err
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	stage, info, err := openFileDeletionStage(base.parent, spec.StageName)
	if err != nil || stage == nil {
		return nil, errors.Join(ErrSourceChanged, err)
	}
	base.stage, base.stageIdentity, base.stageExists = stage, fileIdentity(info), true
	if base.stageIdentity != journal.StageIdentity {
		return nil, ErrSourceChanged
	}
	// Exchange changes only ctime. Size, mtime, inode and the complete digest
	// must still match; the caller verifies both digests before catalog writes.
	base.source, base.sourceInfo, err = openFileDeletionPayload(stage, "payload", spec, 0)
	if err != nil || base.source == nil {
		return nil, errors.Join(ErrSourceChanged, err)
	}
	capture.candidate, capture.candidateInfo, err = openMediaEditFile(base.parent, filepath.Base(filepath.FromSlash(spec.RelativePath)), journal.Candidate, true)
	if err != nil {
		return nil, err
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	return capture, nil
}

// DiscardEmbeddedSubtitleCandidate removes only a proven unpublished candidate.
// Prepared/committed journals are retained even if a caller cancels the task.
// An interrupted generation without a ready witness is intentionally left for
// an administrator to inspect rather than trusting a predictable pathname.
func (s *Store) DiscardEmbeddedSubtitleCandidate(ctx context.Context, work MediaOperationWork) error {
	op := work.Operation
	if ctx == nil || s == nil || s.pool == nil || !validMediaOperationWork(work) || !work.Discard || op.Kind != MediaOperationRemoveSubtitle || op.PublicationPhase != "none" {
		return ErrMediaOperationState
	}
	// Cancellation is cleanup of Goby's own unpublished bytes, not a fresh
	// media-mutation grant. The durable cancellation and live claim fence it.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer rollback(tx)
	current, err := readMediaOperation(ctx, tx, op.ID, false)
	if err != nil {
		return err
	}
	if current.WorkerToken != work.Token || current.Kind != op.Kind || current.CancelRequestedAt == nil || current.PublicationPhase != "none" {
		return ErrMediaOperationConflict
	}
	op = current
	if op.ResultHash == "" {
		// A hard interruption may predate a durable witness. Never guess which
		// filesystem object a deterministic stage pathname now represents.
		_, err := tx.Exec(ctx, `UPDATE media_operations SET result_summary=result_summary||'{"UnpublishedCandidateMayRemain":true}'::jsonb WHERE id=$1 AND worker_token=$2 AND publication_phase='none'`, op.ID, work.Token)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	journal, err := decodeMediaEditJournal(op)
	if err != nil {
		return err
	}
	host, err := mediaDeletionHostIdentity()
	if err != nil || host != journal.Host {
		return errors.Join(ErrMediaOperationRecovery, ErrForbidden, err)
	}
	if err := verifyDeletionRoot(ctx, tx, journal.source(), false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	capture, err := s.openMediaEditCandidateForDiscard(ctx, journal)
	if err != nil {
		return err
	}
	defer capture.Close()
	if capture.candidate == nil {
		return nil
	}
	if err := capture.validateCandidate(ctx, journal.Candidate, false); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// This private directory has never participated in an exchange. Once a
	// prepared intent exists the manager must never issue a discard claim.
	if err := fileDeletionUnlink(capture.base.stage, "payload"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fileDeletionSyncDirectory(capture.base.stage)
}

func (s *Store) openMediaEditCandidateForDiscard(ctx context.Context, journal mediaEditJournal) (_ *mediaEditCapture, resultErr error) {
	spec := journal.source().File
	spec.StageName = journal.StageName
	base := &fileDeletionCapture{store: s, spec: spec}
	capture := &mediaEditCapture{base: base}
	defer func() {
		if resultErr != nil {
			_ = capture.Close()
		}
	}()
	var err error
	base.lease, err = s.leaseLibraryRoot(spec.Root)
	if err != nil {
		return nil, err
	}
	base.root, err = base.lease.Open()
	if err != nil {
		return nil, err
	}
	base.parent, err = openRegisteredRoot(base.root, filepath.Dir(filepath.FromSlash(spec.RelativePath)))
	if err != nil {
		return nil, err
	}
	stage, info, err := openFileDeletionStage(base.parent, spec.StageName)
	if err != nil {
		return nil, err
	}
	if stage == nil {
		if err := base.verifyNamedDirectories(ctx); err != nil {
			return nil, err
		}
		return capture, nil
	}
	base.stage, base.stageIdentity, base.stageExists = stage, fileIdentity(info), true
	if base.stageIdentity != journal.StageIdentity {
		return nil, ErrSourceChanged
	}
	if _, err := stage.Lstat("payload"); errors.Is(err, os.ErrNotExist) {
		if err := base.verifyNamedDirectories(ctx); err != nil {
			return nil, err
		}
		return capture, nil
	}
	capture.candidate, capture.candidateInfo, err = openMediaEditFile(stage, "payload", journal.Candidate, false)
	if err != nil {
		return nil, err
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	return capture, nil
}

// AbandonEmbeddedSubtitleCandidate closes the generation-to-ready gap. A ready
// commit with uncertain outcome may have cleared the worker token; that case
// refuses cleanup so a successfully published review result remains usable.
func (s *Store) AbandonEmbeddedSubtitleCandidate(ctx context.Context, work MediaOperationWork, result MediaOperationResult) error {
	if ctx == nil || s == nil || s.pool == nil || !validMediaOperationWork(work) || work.Apply || work.Discard || work.Operation.Kind != MediaOperationRemoveSubtitle {
		return ErrInvalidInput
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer rollback(tx)
	op, err := readMediaOperation(ctx, tx, work.Operation.ID, false)
	if err != nil {
		return err
	}
	if op.WorkerToken != work.Token || op.State != "running" || op.PublicationPhase != "none" {
		return ErrMediaOperationConflict
	}
	op.Journal, op.ResultHash = result.Journal, result.ResultHash
	journal, err := decodeMediaEditJournal(op)
	if err != nil {
		return err
	}
	host, err := mediaDeletionHostIdentity()
	if err != nil || host != journal.Host {
		return errors.Join(ErrMediaOperationRecovery, ErrForbidden, err)
	}
	if err := verifyDeletionRoot(ctx, tx, journal.source(), false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	capture, err := s.openMediaEditCandidateForDiscard(ctx, journal)
	if err != nil {
		return err
	}
	defer capture.Close()
	if capture.candidate == nil {
		return nil
	}
	if err := capture.validateCandidate(ctx, journal.Candidate, false); err != nil {
		return err
	}
	return capture.discardUnpublished(ctx)
}
