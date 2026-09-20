package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type mediaOperationQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const mediaOperationColumns = `o.id,o.kind,o.state,o.revision,o.source_item_id,o.source_library_id,o.source_root_id,
	o.media_source_id,o.source_revision,o.stream_index,o.parameters,o.progress_stage,o.processed,o.total,
	o.result_summary,o.result_hash,o.publication_phase,o.cancel_requested_at,o.created_at,o.updated_at,o.started_at,o.finished_at,
	o.error_code,o.error_message,(o.item_id=o.source_item_id AND o.library_id=o.source_library_id AND o.root_id=o.source_root_id) IS TRUE,
	o.source_snapshot,o.execution_snapshot,o.journal,o.worker_token,o.request_actor_id,o.request_credential_id,
	o.apply_actor_id,o.apply_credential_id,o.request_id,o.request_fingerprint,o.apply_request_id,o.apply_fingerprint,o.apply_revision`

func scanMediaOperation(row rowScanner) (MediaOperation, error) {
	var op MediaOperation
	var parameters []byte
	err := row.Scan(&op.ID, &op.Kind, &op.State, &op.Revision, &op.ItemID, &op.LibraryID, &op.RootID,
		&op.MediaSourceID, &op.SourceRevision, &op.StreamIndex, &parameters, &op.Progress.Stage, &op.Progress.Processed, &op.Progress.Total,
		&op.ResultSummary, &op.ResultHash, &op.PublicationPhase, &op.CancelRequestedAt, &op.CreatedAt, &op.UpdatedAt, &op.StartedAt, &op.FinishedAt,
		&op.ErrorCode, &op.ErrorMessage, &op.TargetPresent, &op.SourceSnapshot, &op.ExecutionSnapshot, &op.Journal, &op.WorkerToken,
		&op.RequestActor.User.ID, &op.RequestActor.SessionID, &op.ApplyActor.User.ID, &op.ApplyActor.SessionID,
		&op.RequestID, &op.RequestFingerprint, &op.ApplyRequestID, &op.ApplyFingerprint, &op.ApplyRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
	if err != nil {
		return op, err
	}
	if json.Unmarshal(parameters, &op.Parameters) != nil || !mediaOperationStateValid(op.State) {
		return op, ErrUnavailable
	}
	op.RequestActor.Kind, op.ApplyActor.Kind = "admin", "admin"
	projectMediaOperation(&op)
	return op, nil
}

func readMediaOperation(ctx context.Context, tx mediaOperationQuerier, id string, lock bool) (MediaOperation, error) {
	query := "SELECT " + mediaOperationColumns + " FROM media_operations o WHERE o.id=$1"
	if lock {
		query += " FOR UPDATE OF o"
	}
	return scanMediaOperation(tx.QueryRow(ctx, query, id))
}

func checkMediaOperationActor(ctx context.Context, tx pgx.Tx, actor identity.Principal, lock bool) error {
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, lock); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			return errors.Join(ErrForbidden, err)
		}
		return err
	}
	return nil
}

func (s *Store) mediaOperationRead(ctx context.Context, actor identity.Principal) (pgx.Tx, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func mediaOperationText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && strings.TrimSpace(value) == value && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func mediaOperationHashValid(value string, empty bool) bool {
	if empty && value == "" {
		return true
	}
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func mediaOperationJSON(raw []byte, maximum int, emptyOK bool) ([]byte, error) {
	if len(raw) == 0 && emptyOK {
		return []byte("{}"), nil
	}
	if len(raw) == 0 || len(raw) > maximum || !utf8.Valid(raw) {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidInput
	}
	encoded, err := json.Marshal(object)
	if err != nil || len(encoded) > maximum {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

func validateMediaOperationRequest(request MediaOperationRequest) error {
	if !metadataIdentifier(request.ItemID) || !mediaOperationText(request.RequestID, 128, false) || request.MediaSourceID != media.SourceID(request.ItemID) ||
		!mediaOperationText(request.SourceRevision, 256, false) || request.StreamIndex < 0 || request.StreamIndex > 4095 || request.MaxQueued < 1 || request.MaxQueued > 128 {
		return ErrInvalidInput
	}
	p := request.Parameters
	if !mediaOperationText(p.Language, 128, true) || !mediaOperationText(p.Title, 1024, true) {
		return ErrInvalidInput
	}
	switch request.Kind {
	case MediaOperationRemoveSubtitle:
		if len(p.ModelIDs) != 0 || p.OutputFormat != "" || p.Language != "" || p.Title != "" || p.IsDefault || p.IsForced || p.IsHearingImpaired ||
			p.Profile != "" && p.Profile != "matroska-v1" && p.Profile != "mp4-movtext-v1" {
			return ErrInvalidInput
		}
	case MediaOperationOCR:
		if len(p.ModelIDs) < 1 || len(p.ModelIDs) > 3 || p.OutputFormat != "srt" && p.OutputFormat != "vtt" || p.Profile != "" || !validOwnedSubtitleMetadata(p.Language, p.Title) {
			return ErrInvalidInput
		}
		seen := map[string]bool{}
		for _, model := range p.ModelIDs {
			if (model != "eng" && model != "chi_sim" && model != "chi_tra") || seen[model] {
				return ErrInvalidInput
			}
			seen[model] = true
		}
	default:
		return ErrInvalidInput
	}
	_, err := mediaOperationJSON(request.ExecutionSnapshot, MaxMediaOperationDocumentBytes, false)
	return err
}

func mediaOperationRequestFingerprint(request MediaOperationRequest) ([]byte, error) {
	encoded, err := json.Marshal(struct {
		ItemID, Kind, MediaSourceID, SourceRevision string
		StreamIndex                                 int
		Parameters                                  MediaOperationParameters
	}{
		request.ItemID, request.Kind, request.MediaSourceID, request.SourceRevision, request.StreamIndex, request.Parameters})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func (s *Store) captureMediaOperationTarget(ctx context.Context, actor identity.Principal, itemID string) (MediaOperationTarget, indexedMediaSource, []byte, error) {
	var target MediaOperationTarget
	if !metadataIdentifier(itemID) {
		return target, indexedMediaSource{}, nil, ErrInvalidInput
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return target, indexedMediaSource{}, nil, err
	}
	defer rollback(tx)
	source, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, "")
	if err != nil {
		return target, source, nil, err
	}
	var revision int64
	err = tx.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+`,r.binding_revision FROM items i JOIN library_roots r ON r.id=i.root_id WHERE i.id=$1`, itemID).Scan(&target.SourceRevision, &revision)
	if err != nil {
		return target, source, nil, err
	}
	target.ItemID, target.MediaSourceID, target.Container = itemID, source.mediaFile.SourceID, source.mediaFile.Container
	target.Streams = append([]media.Stream(nil), source.mediaFile.Item.Media.Streams...)
	encoded, err := json.Marshal(struct {
		ItemID, LibraryID, RootID, RelativePath, FileIdentity, ETag string
		Size                                                        int64
		BindingRevision                                             int64
		Media                                                       *media.Info
	}{
		itemID, source.root.libraryID, source.root.id, source.relativePath, source.identity, source.mediaFile.ETag, source.mediaFile.Size, revision, source.mediaFile.Item.Media})
	if err != nil || len(encoded) > MaxMediaOperationDocumentBytes {
		return target, source, nil, ErrInvalidInput
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return target, source, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return target, source, nil, err
	}
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		f, e := s.openMediaSource(ctx, source)
		return f, source.mediaFile, e
	})
	if err != nil {
		return target, source, nil, err
	}
	if err = file.Close(); err != nil {
		return target, source, nil, err
	}
	return target, source, encoded, nil
}

func (s *Store) GetMediaOperationTarget(ctx context.Context, actor identity.Principal, itemID string) (MediaOperationTarget, error) {
	target, _, _, err := s.captureMediaOperationTarget(ctx, actor, itemID)
	return target, err
}

// ReplayMediaOperationRequest checks only current administrator authority and
// the original request receipt. Disabled tools or replaced media do not erase
// an already admitted request's exactly-once identity.
func (s *Store) ReplayMediaOperationRequest(ctx context.Context, actor identity.Principal, request MediaOperationRequest) (MediaOperationAdmission, bool, error) {
	check := request
	check.MaxQueued = 1
	check.ExecutionSnapshot = json.RawMessage(`{}`)
	if err := validateMediaOperationRequest(check); err != nil {
		return MediaOperationAdmission{}, false, err
	}
	fingerprint, err := mediaOperationRequestFingerprint(request)
	if err != nil {
		return MediaOperationAdmission{}, false, err
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return MediaOperationAdmission{}, false, err
	}
	defer rollback(tx)
	op, err := scanMediaOperation(tx.QueryRow(ctx, "SELECT "+mediaOperationColumns+` FROM media_operations o WHERE request_actor_id=$1 AND request_id=$2`, actor.User.ID, request.RequestID))
	if errors.Is(err, ErrNotFound) {
		return MediaOperationAdmission{}, false, nil
	}
	if err != nil {
		return MediaOperationAdmission{}, false, err
	}
	if !bytes.Equal(op.RequestFingerprint, fingerprint) {
		return MediaOperationAdmission{}, false, ErrMediaOperationConflict
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return MediaOperationAdmission{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MediaOperationAdmission{}, false, err
	}
	return MediaOperationAdmission{Operation: op}, true, nil
}

// lockMediaOperationTarget uses the deletion workflow's library/root/item lock
// order. It performs no storage I/O; callers must verify descriptors separately.
func lockMediaOperationTarget(ctx context.Context, tx pgx.Tx, op MediaOperation, checkSource bool) error {
	if !op.TargetPresent {
		return ErrSourceChanged
	}
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM libraries WHERE id=$1 FOR UPDATE`, op.LibraryID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSourceChanged
		}
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM library_roots WHERE id=$1 AND library_id=$2 FOR SHARE`, op.RootID, op.LibraryID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSourceChanged
		}
		return err
	}
	var revision string
	err := tx.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1 AND i.library_id=$2 AND i.root_id=$3 AND NOT i.is_folder AND i.media IS NOT NULL FOR UPDATE OF i`, op.ItemID, op.LibraryID, op.RootID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSourceChanged
	}
	if err != nil {
		return err
	}
	if checkSource && revision != op.SourceRevision {
		return ErrSourceChanged
	}
	return nil
}

func (s *Store) StartMediaOperation(ctx context.Context, actor identity.Principal, request MediaOperationRequest) (MediaOperationAdmission, error) {
	var result MediaOperationAdmission
	if err := validateMediaOperationRequest(request); err != nil {
		return result, err
	}
	fingerprint, err := mediaOperationRequestFingerprint(request)
	if err != nil {
		return result, err
	}
	// Receipt replay precedes source inspection: the original source may already
	// have been edited by the successfully completed first request.
	read, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return result, err
	}
	prior, priorErr := scanMediaOperation(read.QueryRow(ctx, "SELECT "+mediaOperationColumns+` FROM media_operations o WHERE request_actor_id=$1 AND request_id=$2`, actor.User.ID, request.RequestID))
	rollback(read)
	if priorErr == nil {
		if !bytes.Equal(prior.RequestFingerprint, fingerprint) {
			return result, ErrMediaOperationConflict
		}
		return MediaOperationAdmission{Operation: prior}, nil
	}
	if !errors.Is(priorErr, ErrNotFound) {
		return result, priorErr
	}
	target, source, snapshot, err := s.captureMediaOperationTarget(ctx, actor, request.ItemID)
	if err != nil || target.SourceRevision != request.SourceRevision {
		if replay, found, replayErr := s.ReplayMediaOperationRequest(ctx, actor, request); found || replayErr != nil {
			return replay, replayErr
		}
		if err != nil {
			return result, err
		}
		return result, ErrSourceChanged
	}
	found := false
	for _, stream := range target.Streams {
		if stream.Index == request.StreamIndex && stream.CodecType == "subtitle" && !stream.IsExternal {
			found = true
			if request.Kind == MediaOperationOCR && stream.Codec != "hdmv_pgs_subtitle" && stream.Codec != "pgssub" && stream.Codec != "dvd_subtitle" && stream.Codec != "dvdsub" {
				return result, ErrInvalidInput
			}
		}
	}
	if !found {
		return result, ErrNotFound
	}
	id, err := randomID()
	if err != nil {
		return result, err
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
		return result, err
	}
	op := MediaOperation{ItemID: request.ItemID, LibraryID: source.root.libraryID, RootID: source.root.id, SourceRevision: target.SourceRevision, TargetPresent: true}
	prior, priorErr = scanMediaOperation(tx.QueryRow(protected, "SELECT "+mediaOperationColumns+` FROM media_operations o WHERE request_actor_id=$1 AND request_id=$2`, actor.User.ID, request.RequestID))
	if priorErr == nil {
		if !bytes.Equal(prior.RequestFingerprint, fingerprint) {
			return result, ErrMediaOperationConflict
		}
		result.Operation = prior
	} else if !errors.Is(priorErr, ErrNotFound) {
		return result, priorErr
	} else {
		if err = lockMediaOperationTarget(protected, tx, op, true); err != nil {
			return result, err
		}
		var queued int
		var busy bool
		if err = tx.QueryRow(protected, `SELECT count(*) FROM media_operations WHERE state='queued'`).Scan(&queued); err != nil {
			return result, err
		}
		if queued >= request.MaxQueued {
			return result, ErrBusy
		}
		if err = tx.QueryRow(protected, `SELECT EXISTS(SELECT 1 FROM media_operations WHERE source_item_id=$1 AND (state IN ('queued','running','applying') OR publication_phase IN ('prepared','catalog_committed')))`, request.ItemID).Scan(&busy); err != nil {
			return result, err
		}
		if busy {
			return result, ErrBusy
		}
		parameters, _ := json.Marshal(request.Parameters)
		execution, _ := mediaOperationJSON(request.ExecutionSnapshot, MaxMediaOperationDocumentBytes, false)
		_, err = tx.Exec(protected, `INSERT INTO media_operations(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,parameters,source_snapshot,execution_snapshot,state)
		VALUES($1,$2,$3,$4,$5,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'queued')`, id, request.Kind, request.ItemID, source.root.libraryID, source.root.id, actor.User.ID, actor.SessionID, request.RequestID, fingerprint, request.MediaSourceID, request.SourceRevision, request.StreamIndex, parameters, snapshot, execution)
		if err != nil {
			return result, err
		}
		result.Operation, err = readMediaOperation(protected, tx, id, false)
		if err != nil {
			return result, err
		}
		result.Admitted = true
	}
	if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
		return result, err
	}
	if err = tx.Commit(protected); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) GetMediaOperation(ctx context.Context, actor identity.Principal, id string) (MediaOperation, error) {
	if !metadataIdentifier(id) {
		return MediaOperation{}, ErrInvalidInput
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return MediaOperation{}, err
	}
	defer rollback(tx)
	op, err := readMediaOperation(ctx, tx, id, false)
	if err != nil {
		return op, err
	}
	if op.CanApply {
		var current string
		err = tx.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1`, op.ItemID).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			op.CanApply = false
		} else if err != nil {
			return op, err
		} else {
			op.CanApply = current == op.SourceRevision
		}
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return op, err
	}
	return op, tx.Commit(ctx)
}

func normalizeMediaOperationPage(options MediaOperationPageOptions) (MediaOperationPageOptions, error) {
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 200 || options.StartIndex < 0 || options.StartIndex > 2147483647 || options.ItemID != "" && !metadataIdentifier(options.ItemID) || options.Kind != "" && options.Kind != MediaOperationOCR && options.Kind != MediaOperationRemoveSubtitle || options.State != "" && !mediaOperationStateValid(options.State) {
		return options, ErrInvalidInput
	}
	return options, nil
}

func (s *Store) ListMediaOperations(ctx context.Context, actor identity.Principal, options MediaOperationPageOptions) (MediaOperationPage, error) {
	options, err := normalizeMediaOperationPage(options)
	result := MediaOperationPage{Items: []MediaOperation{}, StartIndex: options.StartIndex, Limit: options.Limit}
	if err != nil {
		return result, err
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	predicate := `($1='' OR source_item_id=$1) AND ($2='' OR kind=$2) AND ($3='' OR state=$3)`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM media_operations WHERE `+predicate, options.ItemID, options.Kind, options.State).Scan(&result.TotalRecordCount); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, "SELECT "+mediaOperationColumns+` FROM media_operations o WHERE `+predicate+` ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, options.ItemID, options.Kind, options.State, options.Limit, options.StartIndex)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		op, e := scanMediaOperation(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, op)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// PendingMediaOperations is private coordinator work, not an HTTP authority
// boundary. It never resumes ready work unless cancellation was requested.
func (s *Store) PendingMediaOperations(ctx context.Context, limit int) ([]MediaOperation, error) {
	return s.pendingMediaOperations(ctx, limit, false)
}

// PendingMediaOperationCancellations keeps dormant execution inventory from
// hiding an explicitly requested cleanup behind unrelated queued operations.
func (s *Store) PendingMediaOperationCancellations(ctx context.Context, limit int) ([]MediaOperation, error) {
	return s.pendingMediaOperations(ctx, limit, true)
}

func (s *Store) pendingMediaOperations(ctx context.Context, limit int, cancellationOnly bool) ([]MediaOperation, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if limit < 1 || limit > 128 {
		return nil, ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, "SELECT "+mediaOperationColumns+` FROM media_operations o WHERE worker_token='' AND (state IN ('queued','applying') OR (state IN ('ready','interrupted') AND cancel_requested_at IS NOT NULL))
		AND (NOT $2::boolean OR (cancel_requested_at IS NOT NULL AND publication_phase='none'))
		ORDER BY (cancel_requested_at IS NOT NULL) DESC,created_at,id LIMIT $1`, limit, cancellationOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MediaOperation{}
	for rows.Next() {
		op, e := scanMediaOperation(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, op)
	}
	return result, rows.Err()
}

func (s *Store) CancelMediaOperation(ctx context.Context, actor identity.Principal, id string, revision int64) (MediaOperation, error) {
	var result MediaOperation
	if !metadataIdentifier(id) || revision < 1 {
		return result, ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
		return result, err
	}
	op, err := readMediaOperation(protected, tx, id, true)
	if err != nil {
		return result, err
	}
	cleanupRetry := op.Kind == MediaOperationRemoveSubtitle && op.PublicationPhase == "none" && op.WorkerToken == "" && (op.State == "interrupted" || op.State == "failed" || op.State == "stale" || op.State == "recovery_required")
	if op.State != "cancelled" && (op.CancelRequestedAt == nil || cleanupRetry) {
		if op.Revision != revision {
			return result, ErrMediaOperationConflict
		}
		if !op.CanCancel && op.State != "interrupted" {
			return result, ErrMediaOperationState
		}
		state := op.State
		if state == "queued" {
			state = "cancelled"
		} else if op.WorkerToken == "" && op.PublicationPhase == "prepared" {
			// An unclaimed recovery request has not resumed its filesystem work.
			// Retain the barrier and permit a later fresh recovery grant.
			state = "recovery_required"
		} else if op.Kind == MediaOperationOCR && op.WorkerToken == "" && op.PublicationPhase == "none" && (state == "ready" || state == "interrupted" || state == "applying") {
			state = "cancelled"
		} else if cleanupRetry {
			state = "interrupted"
		}
		_, err = tx.Exec(protected, `UPDATE media_operations SET cancel_requested_at=clock_timestamp(),state=$2,revision=revision+1,updated_at=clock_timestamp(),apply_actor_id=CASE WHEN $2 IN ('ready','interrupted') THEN $3 ELSE apply_actor_id END,apply_credential_id=CASE WHEN $2 IN ('ready','interrupted') THEN $4 ELSE apply_credential_id END,finished_at=CASE WHEN $2 IN ('cancelled','recovery_required') THEN clock_timestamp() ELSE finished_at END WHERE id=$1`, id, state, actor.User.ID, actor.SessionID)
		if err != nil {
			return result, err
		}
	}
	result, err = readMediaOperation(protected, tx, id, false)
	if err != nil {
		return result, err
	}
	if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
		return result, err
	}
	return result, tx.Commit(protected)
}

func (s *Store) ApplyMediaOperation(ctx context.Context, actor identity.Principal, id string, request MediaOperationApplyRequest) (MediaOperationAdmission, error) {
	return s.admitMediaOperationApply(ctx, actor, id, request, false)
}
func (s *Store) RecoverMediaOperation(ctx context.Context, actor identity.Principal, id string, request MediaOperationApplyRequest) (MediaOperationAdmission, error) {
	return s.admitMediaOperationApply(ctx, actor, id, request, true)
}

func (s *Store) admitMediaOperationApply(ctx context.Context, actor identity.Principal, id string, request MediaOperationApplyRequest, recovery bool) (MediaOperationAdmission, error) {
	var result MediaOperationAdmission
	if !metadataIdentifier(id) || request.Revision < 1 || !mediaOperationText(request.RequestID, 128, false) || !mediaOperationText(request.SourceRevision, 256, false) || !mediaOperationHashValid(request.ResultHash, false) {
		return result, ErrInvalidInput
	}
	encoded, _ := json.Marshal(struct {
		Request  MediaOperationApplyRequest
		Recovery bool
	}{request, recovery})
	digest := sha256.Sum256(encoded)
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
		return result, err
	}
	op, err := readMediaOperation(protected, tx, id, false)
	if err != nil {
		return result, err
	}
	if op.ApplyRequestID == request.RequestID {
		if !bytes.Equal(op.ApplyFingerprint, digest[:]) {
			return result, ErrMediaOperationConflict
		}
		result.Operation = op
	} else {
		if op.Revision != request.Revision || op.SourceRevision != request.SourceRevision || op.ResultHash != request.ResultHash {
			return result, ErrMediaOperationConflict
		}
		if recovery {
			if op.State != "recovery_required" || op.Kind != MediaOperationRemoveSubtitle {
				return result, ErrMediaOperationState
			}
		} else if op.State != "ready" || op.CancelRequestedAt != nil {
			return result, ErrMediaOperationState
		}
		if err = lockMediaOperationTarget(protected, tx, op, !recovery); err != nil {
			return result, err
		}
		current, e := readMediaOperation(protected, tx, id, true)
		if e != nil {
			return result, e
		}
		if current.Revision != op.Revision {
			return result, ErrMediaOperationConflict
		}
		var busy bool
		if err = tx.QueryRow(protected, `SELECT EXISTS(SELECT 1 FROM media_operations WHERE source_item_id=$1 AND id<>$2 AND (state IN ('queued','running','applying') OR publication_phase IN ('prepared','catalog_committed')))`, op.ItemID, op.ID).Scan(&busy); err != nil {
			return result, err
		}
		if busy {
			return result, ErrBusy
		}
		if op.Kind == MediaOperationOCR {
			cues, e := readMediaOperationCues(protected, tx, id)
			if e != nil {
				return result, e
			}
			if e = validateMediaOperationReview(op, cues, false); e != nil {
				return result, e
			}
		}
		_, err = tx.Exec(protected, `UPDATE media_operations SET state='applying',worker_token='',revision=revision+1,apply_actor_id=$2,apply_credential_id=$3,apply_request_id=$4,apply_fingerprint=$5,apply_revision=$6,cancel_requested_at=NULL,error_code='',error_message='',finished_at=NULL,updated_at=clock_timestamp() WHERE id=$1`, id, actor.User.ID, actor.SessionID, request.RequestID, digest[:], op.Revision)
		if err != nil {
			return result, err
		}
		result.Operation, err = readMediaOperation(protected, tx, id, false)
		if err != nil {
			return result, err
		}
		result.Admitted = true
	}
	if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
		return result, err
	}
	return result, tx.Commit(protected)
}

func mediaOperationReviewHash(op MediaOperation, cues []MediaOperationCue) (string, error) {
	summary, err := mediaOperationJSON(op.ResultSummary, MaxMediaOperationDocumentBytes, true)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Version    string
		Parameters MediaOperationParameters
		Summary    json.RawMessage
		Cues       []MediaOperationCue
	}{"goby.media-ocr.review.v1", op.Parameters, summary, cues})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Keep errors bounded and free of executable paths, SQL and source filenames.
func mediaOperationFailure(cause error) (string, string) {
	switch {
	case errors.Is(cause, ErrSourceChanged):
		return "source_changed", "The indexed media source changed."
	case errors.Is(cause, ErrForbidden), errors.Is(cause, identity.ErrUnauthorized):
		return "authority_changed", "The administrator credential is no longer authorized."
	case errors.Is(cause, context.DeadlineExceeded):
		return "runtime_limit", "The operation exceeded its execution time limit."
	case errors.Is(cause, context.Canceled):
		return "cancelled", "The operation was cancelled."
	default:
		return "execution_failed", "The media operation could not finish."
	}
}

// MediaOperationWorkStatus lets the owner coordinator observe cancellation and
// current authority without exporting private execution witnesses over HTTP.
func (s *Store) MediaOperationWorkStatus(ctx context.Context, work MediaOperationWork) (MediaOperation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return MediaOperation{}, err
	}
	defer rollback(tx)
	op, err := readMediaOperation(ctx, tx, work.Operation.ID, false)
	if err != nil {
		return op, err
	}
	if op.WorkerToken != work.Token || work.Token == "" {
		return op, ErrMediaOperationState
	}
	if !work.Discard {
		actor := op.RequestActor
		if work.Apply {
			actor = op.ApplyActor
		}
		if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
			return op, err
		}
	}
	return op, tx.Commit(ctx)
}

// RetainMediaOperationJournal records only ownership evidence after a producer
// returned a candidate that could not become ready. It grants no publication.
func (s *Store) RetainMediaOperationJournal(ctx context.Context, work MediaOperationWork, journal json.RawMessage, resultHash string) error {
	encoded, err := mediaOperationJSON(journal, MaxMediaOperationDocumentBytes, false)
	if err != nil || !mediaOperationHashValid(resultHash, false) {
		return ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	tag, err := tx.Exec(protected, `UPDATE media_operations SET journal=$3,result_hash=$4,updated_at=clock_timestamp() WHERE id=$1 AND worker_token=$2 AND state='running' AND publication_phase='none'`, work.Operation.ID, work.Token, encoded, resultHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMediaOperationState
	}
	return tx.Commit(protected)
}
