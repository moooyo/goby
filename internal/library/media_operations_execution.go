package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// RecoverMediaOperations is an owner-startup operation. Call it once, before
// starting any media workers. It never replays a publication or accesses files.
func (s *Store) RecoverMediaOperations(ctx context.Context) error {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	_, err = normalizeMediaOperationExecutionState(protected, tx, false)
	if err != nil {
		return err
	}
	return tx.Commit(protected)
}

// NormalizeMediaOperationRestoreState runs only in a trusted restore finalizer,
// after archive fingerprints were authenticated. It preserves review evidence
// and owned subtitle bytes while removing every runnable restored grant.
func NormalizeMediaOperationRestoreState(ctx context.Context, tx pgx.Tx) (int64, error) {
	if tx == nil {
		return 0, ErrInvalidInput
	}
	var present bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('media_operations') IS NOT NULL`).Scan(&present); err != nil {
		return 0, err
	}
	if !present {
		return 0, nil
	}
	return normalizeMediaOperationExecutionState(ctx, tx, true)
}

func normalizeMediaOperationExecutionState(ctx context.Context, tx pgx.Tx, restored bool) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE media_operations SET
		state=CASE
			WHEN publication_phase IN ('prepared','catalog_committed') THEN 'recovery_required'
			WHEN state='applying' AND kind='remove_embedded_subtitle' THEN 'recovery_required'
			WHEN state='applying' AND kind='subtitle_ocr' AND publication_phase='none' THEN 'ready'
			WHEN state='applying' THEN 'recovery_required'
			WHEN state IN ('queued','running') THEN 'interrupted'
			ELSE state END,
		worker_token='',cancel_requested_at=NULL,apply_actor_id='',apply_credential_id='',
		apply_request_id=CASE WHEN $1 THEN '' ELSE apply_request_id END,
		apply_fingerprint=CASE WHEN $1 THEN NULL ELSE apply_fingerprint END,
		apply_revision=CASE WHEN $1 THEN NULL ELSE apply_revision END,
		error_code=CASE
			WHEN publication_phase IN ('prepared','catalog_committed') OR state='applying' AND kind='remove_embedded_subtitle' THEN 'publication_recovery_required'
			WHEN state='applying' AND kind='subtitle_ocr' AND publication_phase='none' THEN ''
			WHEN state IN ('queued','running','applying') THEN 'interrupted'
			ELSE error_code END,
		error_message=CASE
			WHEN publication_phase IN ('prepared','catalog_committed') OR state='applying' AND kind='remove_embedded_subtitle' THEN 'The publication requires an administrator to review its recovery state.'
			WHEN state='applying' AND kind='subtitle_ocr' AND publication_phase='none' THEN ''
			WHEN state IN ('queued','running','applying') THEN 'The service stopped before the operation finished.'
			ELSE error_message END,
		finished_at=CASE
			WHEN state='applying' AND kind='subtitle_ocr' AND publication_phase='none' THEN NULL
			WHEN state IN ('queued','running','applying') OR publication_phase IN ('prepared','catalog_committed') THEN clock_timestamp()
			ELSE finished_at END,
		revision=revision+1,updated_at=clock_timestamp()
		WHERE worker_token<>'' OR state IN ('queued','running','applying')
			OR publication_phase IN ('prepared','catalog_committed') AND state<>'recovery_required'
			OR cancel_requested_at IS NOT NULL AND state IN ('ready','interrupted')
			OR $1::boolean AND state IN ('ready','interrupted','recovery_required','stale','failed')
				AND (apply_actor_id<>'' OR apply_credential_id<>'' OR apply_request_id<>'' OR apply_fingerprint IS NOT NULL OR apply_revision IS NOT NULL)`, restored)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClaimMediaOperation rechecks the persisted execution grant before issuing a
// process-local claim. Cancellation cleanup is compensation, not a new grant.
func (s *Store) ClaimMediaOperation(ctx context.Context, id string) (MediaOperationWork, error) {
	var work MediaOperationWork
	if !metadataIdentifier(id) {
		return work, ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return work, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	op, err := readMediaOperation(protected, tx, id, false)
	if err != nil {
		return work, err
	}
	apply, discard, err := mediaOperationClaimMode(op)
	if err != nil {
		return work, err
	}
	actor := mediaOperationExecutionActor(op, apply)
	if !discard {
		if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
			return work, rejectMediaOperationClaim(protected, tx, op, "", err)
		}
		if err = lockMediaOperationTarget(protected, tx, op, op.PublicationPhase != "catalog_committed"); err != nil {
			return work, rejectMediaOperationClaim(protected, tx, op, "", err)
		}
	}
	current, err := readMediaOperation(protected, tx, id, true)
	if err != nil {
		return work, err
	}
	if current.Revision != op.Revision || current.WorkerToken != "" || current.State != op.State {
		return work, ErrMediaOperationConflict
	}
	if !discard {
		if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
			return work, rejectMediaOperationClaim(protected, tx, current, "", err)
		}
	}
	token, err := randomID()
	if err != nil {
		return work, err
	}
	state := current.State
	if state == "queued" {
		state = "running"
	}
	tag, err := tx.Exec(protected, `UPDATE media_operations SET state=$3,worker_token=$2,
		started_at=COALESCE(started_at,clock_timestamp()),finished_at=NULL,
		error_code='',error_message='',revision=revision+1,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token='' AND state=$4 AND revision=$5`, id, token, state, current.State, current.Revision)
	if err != nil {
		return work, err
	}
	if tag.RowsAffected() != 1 {
		return work, ErrMediaOperationConflict
	}
	current, err = readMediaOperation(protected, tx, id, false)
	if err != nil {
		return work, err
	}
	if !discard {
		if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
			return work, rejectMediaOperationClaim(protected, tx, current, token, err)
		}
	}
	if err = tx.Commit(protected); err != nil {
		return work, err
	}
	return MediaOperationWork{Operation: current, Token: token, Apply: apply, Discard: discard}, nil
}

func mediaOperationClaimMode(op MediaOperation) (apply, discard bool, err error) {
	if op.WorkerToken != "" {
		return false, false, ErrMediaOperationConflict
	}
	switch op.State {
	case "queued", "applying":
		apply = op.State == "applying"
	case "ready", "interrupted":
		if op.CancelRequestedAt == nil {
			return false, false, ErrMediaOperationState
		}
	default:
		return false, false, ErrMediaOperationState
	}
	if op.CancelRequestedAt != nil {
		if op.PublicationPhase == "prepared" || op.PublicationPhase == "catalog_committed" {
			return false, false, ErrMediaOperationRecovery
		}
		discard = true
	}
	return apply, discard, nil
}

func mediaOperationExecutionActor(op MediaOperation, apply bool) identity.Principal {
	if apply {
		return op.ApplyActor
	}
	return op.RequestActor
}

// A rejected, expired execution grant must not stay queued forever. This is a
// database-only failure record and deliberately needs no surviving actor grant.
func rejectMediaOperationClaim(ctx context.Context, tx pgx.Tx, expected MediaOperation, token string, cause error) error {
	if !errors.Is(cause, ErrSourceChanged) && !errors.Is(cause, ErrForbidden) && !errors.Is(cause, identity.ErrUnauthorized) {
		return cause
	}
	current, err := readMediaOperation(ctx, tx, expected.ID, true)
	if err != nil {
		return errors.Join(cause, err)
	}
	if current.Revision != expected.Revision || current.WorkerToken != token || current.State != expected.State {
		return errors.Join(cause, ErrMediaOperationConflict)
	}
	state, code, message := mediaOperationFinishedState(current, cause, false)
	_, err = tx.Exec(ctx, `UPDATE media_operations SET state=$3,worker_token='',error_code=$4,error_message=$5,
		apply_actor_id=CASE WHEN $3='ready' THEN '' ELSE apply_actor_id END,
		apply_credential_id=CASE WHEN $3='ready' THEN '' ELSE apply_credential_id END,
		revision=revision+1,finished_at=CASE WHEN $3='ready' THEN NULL ELSE clock_timestamp() END,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state=$6 AND revision=$7`, current.ID, token, state, code, message, current.State, current.Revision)
	if err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(cause, tx.Commit(ctx))
}

// UpdateMediaOperationProgress does not change the user-visible CAS revision.
func (s *Store) UpdateMediaOperationProgress(ctx context.Context, work MediaOperationWork, progress MediaOperationProgress) error {
	if !validMediaOperationWork(work) || work.Discard || !validMediaOperationProgress(progress) {
		return ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	op, err := readMediaOperation(protected, tx, work.Operation.ID, true)
	if err != nil {
		return err
	}
	if err = checkMediaOperationWork(op, work, false); err != nil {
		return err
	}
	tag, err := tx.Exec(protected, `UPDATE media_operations SET progress_stage=$3,processed=$4,total=$5,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state=$6 AND cancel_requested_at IS NULL`, op.ID, work.Token, progress.Stage, progress.Processed, progress.Total, op.State)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	return tx.Commit(protected)
}

func validMediaOperationProgress(progress MediaOperationProgress) bool {
	if len(progress.Stage) > 64 || progress.Processed < 0 || progress.Total < 0 || progress.Total > 0 && progress.Processed > progress.Total {
		return false
	}
	for _, character := range progress.Stage {
		if character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// ReadyMediaOperation saves immutable observations and initial review values in
// the same transaction as the ready transition. No partial review is visible.
func (s *Store) ReadyMediaOperation(ctx context.Context, work MediaOperationWork, result MediaOperationResult) error {
	if work.Apply || work.Discard || !validMediaOperationWork(work) {
		return ErrInvalidInput
	}
	result, err := normalizeMediaOperationResult(work.Operation.Kind, result)
	if err != nil {
		return err
	}
	tx, protected, op, err := s.mediaOperationClaimedTransaction(ctx, work, true, false)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if op.PublicationPhase != "none" {
		return ErrMediaOperationState
	}
	if op.Kind == MediaOperationOCR {
		op.ResultSummary = result.Summary
		result.ResultHash, err = mediaOperationReviewHash(op, result.Cues)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(protected, `DELETE FROM media_operation_cues WHERE operation_id=$1`, op.ID); err != nil {
		return err
	}
	if len(result.Cues) > 0 {
		columns := []string{"operation_id", "ordinal", "original_start_ticks", "original_end_ticks", "original_text",
			"start_ticks", "end_ticks", "text", "included", "confidence", "warnings", "image_sha256", "image_png", "is_forced", "is_hearing_impaired"}
		_, err = tx.CopyFrom(protected, pgx.Identifier{"media_operation_cues"}, columns, pgx.CopyFromSlice(len(result.Cues), func(index int) ([]any, error) {
			cue := result.Cues[index]
			warnings, encodeErr := json.Marshal(cue.Warnings)
			if encodeErr != nil {
				return nil, encodeErr
			}
			return []any{op.ID, cue.Ordinal, cue.OriginalStartTicks, cue.OriginalEndTicks, cue.OriginalText,
				cue.StartTicks, cue.EndTicks, cue.Text, cue.Included, cue.Confidence, string(warnings), cue.ImageSHA256, cue.ImagePNG,
				cue.IsForced, cue.IsHearingImpaired}, nil
		}))
		if err != nil {
			return s.ownershipErrorLocked(err)
		}
	}
	tag, err := tx.Exec(protected, `UPDATE media_operations SET state='ready',worker_token='',result_summary=$3,result_hash=$4,journal=$5,
		error_code='',error_message='',revision=revision+1,finished_at=NULL,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state='running' AND cancel_requested_at IS NULL`, op.ID, work.Token, result.Summary, result.ResultHash, result.Journal)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	if err = checkMediaOperationActor(protected, tx, op.RequestActor, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

func normalizeMediaOperationResult(kind string, result MediaOperationResult) (MediaOperationResult, error) {
	var err error
	result.Summary, err = mediaOperationJSON(result.Summary, MaxMediaOperationDocumentBytes, false)
	if err != nil {
		return MediaOperationResult{}, err
	}
	result.Journal, err = mediaOperationJSON(result.Journal, MaxMediaOperationDocumentBytes, kind == MediaOperationOCR)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if kind == MediaOperationRemoveSubtitle {
		if len(result.Cues) != 0 || !mediaOperationHashValid(result.ResultHash, false) {
			return MediaOperationResult{}, ErrInvalidInput
		}
		return result, nil
	}
	if kind != MediaOperationOCR || len(result.Cues) == 0 || len(result.Cues) > MaxMediaOperationCues {
		return MediaOperationResult{}, ErrInvalidInput
	}
	cues := make([]MediaOperationCue, len(result.Cues))
	textBytes, imageBytes, warningBytes := 0, 0, 0
	for index, source := range result.Cues {
		if source.StartTicks < 0 || source.EndTicks <= source.StartTicks || len(source.Text) > MaxMediaOperationDocumentBytes ||
			!utf8.ValidString(source.Text) || strings.ContainsRune(source.Text, '\x00') || len(source.ImagePNG) > 1<<20 ||
			!mediaOperationHashValid(source.ImageSHA256, true) || source.Confidence != nil && (math.IsNaN(*source.Confidence) || math.IsInf(*source.Confidence, 0) || *source.Confidence < 0 || *source.Confidence > 100) {
			return MediaOperationResult{}, ErrInvalidInput
		}
		textBytes += len(source.Text)
		imageBytes += len(source.ImagePNG)
		if textBytes > MaxMediaOperationTextBytes || imageBytes > MaxMediaOperationImageBytes {
			return MediaOperationResult{}, ErrInvalidInput
		}
		if len(source.ImagePNG) > 0 {
			digest := sha256.Sum256(source.ImagePNG)
			if source.ImageSHA256 != hex.EncodeToString(digest[:]) {
				return MediaOperationResult{}, ErrInvalidInput
			}
		} else if source.ImageSHA256 != "" {
			return MediaOperationResult{}, ErrInvalidInput
		}
		cue := source
		cue.Ordinal = index
		cue.OriginalStartTicks, cue.OriginalEndTicks, cue.OriginalText = source.StartTicks, source.EndTicks, source.Text
		cue.Warnings = append([]string{}, source.Warnings...)
		cue.ImagePNG = append([]byte{}, source.ImagePNG...)
		if source.Confidence != nil {
			confidence := *source.Confidence
			cue.Confidence = &confidence
		}
		for _, warning := range cue.Warnings {
			if !mediaOperationText(warning, 2048, false) {
				return MediaOperationResult{}, ErrInvalidInput
			}
		}
		warnings, err := json.Marshal(cue.Warnings)
		if err != nil || len(warnings) > 16384 {
			return MediaOperationResult{}, ErrInvalidInput
		}
		warningBytes += len(warnings)
		if warningBytes > MaxMediaOperationTextBytes {
			return MediaOperationResult{}, ErrInvalidInput
		}
		cues[index] = cue
	}
	result.Cues = cues
	result.ResultHash = ""
	return result, nil
}

// FinishMediaOperationWork is called only after every executor and cleanup
// worker has stopped. Uncertain cleanup must return ErrMediaOperationRecovery.
// A nil cause never implicitly publishes a callback's unfinished result.
func (s *Store) FinishMediaOperationWork(ctx context.Context, work MediaOperationWork, cause error, shutdown bool) error {
	if !validMediaOperationWork(work) {
		return ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	op, err := readMediaOperation(protected, tx, work.Operation.ID, true)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if op.WorkerToken != work.Token || op.State == "completed" || op.State == "ready" && !work.Discard {
		return nil
	}
	if op.Kind != work.Operation.Kind || op.PublicationPhase == "done" {
		return ErrMediaOperationState
	}
	state, code, message := mediaOperationFinishedState(op, cause, shutdown)
	tag, err := tx.Exec(protected, `UPDATE media_operations SET state=$3,worker_token='',error_code=$4,error_message=$5,
		apply_actor_id=CASE WHEN $3='ready' THEN '' ELSE apply_actor_id END,
		apply_credential_id=CASE WHEN $3='ready' THEN '' ELSE apply_credential_id END,
		revision=revision+1,finished_at=CASE WHEN $3='ready' THEN NULL ELSE clock_timestamp() END,updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state=$6`, op.ID, work.Token, state, code, message, op.State)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	return tx.Commit(protected)
}

func mediaOperationFinishedState(op MediaOperation, cause error, shutdown bool) (state, code, message string) {
	if op.PublicationPhase == "prepared" || op.PublicationPhase == "catalog_committed" || errors.Is(cause, ErrMediaOperationRecovery) {
		return "recovery_required", "publication_recovery_required", "The publication requires an administrator to review its recovery state."
	}
	if op.CancelRequestedAt != nil && (cause == nil || errors.Is(cause, context.Canceled)) {
		return "cancelled", "cancelled", "The operation was cancelled."
	}
	// An OCR apply is a single SQL transaction. An unapplied review remains
	// editable after a failed grant, rendering error or shutdown, but a changed
	// source still invalidates the draft. A new explicit Apply is required.
	if op.Kind == MediaOperationOCR && op.State == "applying" && !errors.Is(cause, ErrSourceChanged) {
		if shutdown {
			return "ready", "interrupted", "The service stopped before the reviewed subtitle was published."
		}
		if cause == nil {
			return "ready", "incomplete_result", "The executor stopped without publishing the reviewed subtitle."
		}
		code, message = mediaOperationFailure(cause)
		return "ready", code, message
	}
	if op.Kind == MediaOperationRemoveSubtitle && op.State == "applying" && op.PublicationPhase == "none" && op.ResultHash != "" && !errors.Is(cause, ErrSourceChanged) {
		if shutdown {
			return "ready", "interrupted", "The service stopped before the validated candidate was published."
		}
		if cause == nil {
			return "ready", "incomplete_result", "The executor stopped without publishing its validated candidate."
		}
		code, message = mediaOperationFailure(cause)
		return "ready", code, message
	}
	if shutdown {
		return "interrupted", "interrupted", "The service stopped before the operation finished."
	}
	if cause == nil {
		return "failed", "incomplete_result", "The executor stopped without finalizing its result."
	}
	code, message = mediaOperationFailure(cause)
	if errors.Is(cause, ErrSourceChanged) {
		return "stale", code, message
	}
	return "failed", code, message
}

// PrepareMediaOperationPublication persists the write-ahead reservation before
// the executor can exchange any media entry. Slow storage work remains outside
// this transaction and its locks.
func (s *Store) PrepareMediaOperationPublication(ctx context.Context, work MediaOperationWork, journal json.RawMessage) error {
	if !work.Apply || work.Discard || !validMediaOperationWork(work) || work.Operation.Kind != MediaOperationRemoveSubtitle {
		return ErrInvalidInput
	}
	encoded, err := mediaOperationJSON(journal, MaxMediaOperationDocumentBytes, false)
	if err != nil {
		return err
	}
	tx, protected, op, err := s.mediaOperationClaimedTransaction(ctx, work, true, false)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if op.PublicationPhase != "none" && op.PublicationPhase != "prepared" {
		return ErrMediaOperationState
	}
	var busy bool
	if err = tx.QueryRow(protected, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))
		OR EXISTS(SELECT 1 FROM media_deletion_operations WHERE library_id=$1)`, op.LibraryID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	if op.PublicationPhase == "prepared" {
		var same bool
		if err = tx.QueryRow(protected, `SELECT journal=$2::jsonb FROM media_operations WHERE id=$1`, op.ID, encoded).Scan(&same); err != nil {
			return err
		}
		if !same {
			return ErrMediaOperationConflict
		}
	} else {
		tag, updateErr := tx.Exec(protected, `UPDATE media_operations SET publication_phase='prepared',journal=$3,
			revision=revision+1,updated_at=clock_timestamp()
			WHERE id=$1 AND worker_token=$2 AND state='applying' AND publication_phase='none' AND cancel_requested_at IS NULL`, op.ID, work.Token, encoded)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return ErrMediaOperationConflict
		}
	}
	if err = checkMediaOperationActor(protected, tx, op.ApplyActor, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

// CommitMediaOperationApply owns the transaction, authority checks and durable
// publication state. A commit error is propagated without assuming rollback.
func (s *Store) CommitMediaOperationApply(ctx context.Context, work MediaOperationWork, apply MediaOperationApplyFunc) error {
	if !work.Apply || work.Discard || !validMediaOperationWork(work) || apply == nil {
		return ErrInvalidInput
	}
	tx, protected, op, err := s.mediaOperationClaimedTransaction(ctx, work, true, false)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if op.Kind == MediaOperationOCR && op.PublicationPhase != "none" ||
		op.Kind == MediaOperationRemoveSubtitle && op.PublicationPhase != "prepared" {
		return ErrMediaOperationState
	}
	if err = apply(protected, tx, op); err != nil {
		return err
	}
	state, phase, token := "completed", "done", ""
	if op.Kind == MediaOperationRemoveSubtitle {
		state, phase, token = "applying", "catalog_committed", work.Token
	}
	tag, err := tx.Exec(protected, `UPDATE media_operations SET state=$3,publication_phase=$4,worker_token=$5,
		error_code='',error_message='',revision=revision+1,updated_at=clock_timestamp(),
		finished_at=CASE WHEN $3='completed' THEN clock_timestamp() ELSE NULL END
		WHERE id=$1 AND worker_token=$2 AND state='applying' AND publication_phase=$6 AND cancel_requested_at IS NULL`, op.ID, work.Token, state, phase, token, op.PublicationPhase)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	if err = checkMediaOperationActor(protected, tx, op.ApplyActor, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

// CompleteMediaOperationPublication confirms cleanup already performed by the
// executor. Cancellation cannot undo a catalog commit. A fresh administrator
// grant is still required; expired authority retains the publication journal.
func (s *Store) CompleteMediaOperationPublication(ctx context.Context, work MediaOperationWork, journal json.RawMessage) error {
	if !work.Apply || work.Discard || !validMediaOperationWork(work) || work.Operation.Kind != MediaOperationRemoveSubtitle {
		return ErrInvalidInput
	}
	encoded, err := mediaOperationJSON(journal, MaxMediaOperationDocumentBytes, false)
	if err != nil {
		return err
	}
	tx, protected, op, err := s.mediaOperationClaimedTransaction(ctx, work, false, true)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if op.PublicationPhase != "catalog_committed" {
		return ErrMediaOperationState
	}
	tag, err := tx.Exec(protected, `UPDATE media_operations SET state='completed',publication_phase='done',journal=$3,worker_token='',
		error_code='',error_message='',revision=revision+1,finished_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE id=$1 AND worker_token=$2 AND state='applying' AND publication_phase='catalog_committed'`, op.ID, work.Token, encoded)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationConflict
	}
	if err = checkMediaOperationActor(protected, tx, op.ApplyActor, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

func validMediaOperationWork(work MediaOperationWork) bool {
	if !metadataIdentifier(work.Operation.ID) || len(work.Token) != 32 || strings.ToLower(work.Token) != work.Token ||
		work.Operation.Kind != MediaOperationOCR && work.Operation.Kind != MediaOperationRemoveSubtitle {
		return false
	}
	_, err := hex.DecodeString(work.Token)
	return err == nil
}

func checkMediaOperationWork(op MediaOperation, work MediaOperationWork, allowCancellation bool) error {
	if op.ID != work.Operation.ID || op.Kind != work.Operation.Kind || op.WorkerToken != work.Token {
		return ErrMediaOperationConflict
	}
	if work.Discard || work.Apply && op.State != "applying" || !work.Apply && op.State != "running" {
		return ErrMediaOperationState
	}
	if !allowCancellation && op.CancelRequestedAt != nil {
		return context.Canceled
	}
	return nil
}

// Media mutations use actor, library/root/item, then operation row lock order.
// Reading the operation before these locks supplies immutable request evidence;
// rereading it afterward fences changed apply grants and worker claims.
func (s *Store) mediaOperationClaimedTransaction(ctx context.Context, work MediaOperationWork, checkSource, allowCancellation bool) (pgx.Tx, context.Context, MediaOperation, error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return nil, nil, MediaOperation{}, err
	}
	fail := func(cause error) (pgx.Tx, context.Context, MediaOperation, error) {
		rollback(tx)
		return nil, nil, MediaOperation{}, cause
	}
	protected := tx.(*ownedTx).ctx
	op, err := readMediaOperation(protected, tx, work.Operation.ID, false)
	if err != nil {
		return fail(err)
	}
	if err = checkMediaOperationWork(op, work, allowCancellation); err != nil {
		return fail(err)
	}
	actor := mediaOperationExecutionActor(op, work.Apply)
	if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
		return fail(err)
	}
	if err = lockMediaOperationTarget(protected, tx, op, checkSource); err != nil {
		return fail(err)
	}
	current, err := readMediaOperation(protected, tx, op.ID, true)
	if err != nil {
		return fail(err)
	}
	if err = checkMediaOperationWork(current, work, allowCancellation); err != nil {
		return fail(err)
	}
	currentActor := mediaOperationExecutionActor(current, work.Apply)
	if currentActor.User.ID != actor.User.ID || currentActor.SessionID != actor.SessionID || current.Revision != op.Revision {
		return fail(ErrMediaOperationConflict)
	}
	if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
		return fail(err)
	}
	return tx, protected, current, nil
}
