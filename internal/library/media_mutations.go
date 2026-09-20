package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

var ErrMediaDeletionRecovery = errors.New("media deletion requires an authorized recovery retry")

// fileDeletionTarget contains only server-owned catalog facts. Filesystem paths
// never come from an HTTP request. An external subtitle can bind its primary
// source as a second read-only precondition without deleting that primary file.
type fileDeletionTarget struct {
	Kind, ItemID, LibraryID, ParentID string
	SubtitleIndex                     int
	File                              fileDeletionSpec
	PrimaryFile                       *fileDeletionSpec
	BindingRevision                   int64
	BindingFingerprint                string
	SourceTag, SourceHash             string
}

type deletionRootDocument struct{ ID, LibraryID, Path, AllowedPath, RelativePath string }
type deletionTargetDocument struct {
	Target fileDeletionTarget
	Root   deletionRootDocument
}

func encodeDeletionTarget(target fileDeletionTarget) ([]byte, error) {
	root := target.File.Root
	return json.Marshal(deletionTargetDocument{Target: target, Root: deletionRootDocument{root.id, root.libraryID, root.path, root.allowedPath, root.relativePath}})
}

func decodeDeletionTarget(raw []byte) (fileDeletionTarget, error) {
	var document deletionTargetDocument
	if len(raw) > 256*1024 || json.Unmarshal(raw, &document) != nil {
		return fileDeletionTarget{}, ErrUnavailable
	}
	result := document.Target
	root := document.Root
	result.File.Root = libraryRoot{root.ID, root.LibraryID, root.Path, root.AllowedPath, root.RelativePath}
	if result.PrimaryFile != nil {
		result.PrimaryFile.Root = result.File.Root
	}
	if !metadataIdentifier(result.ItemID) || !metadataIdentifier(result.LibraryID) || root.ID == "" || root.LibraryID != result.LibraryID || result.BindingRevision < 1 ||
		(result.Kind != "media" && result.Kind != "subtitle") {
		return fileDeletionTarget{}, ErrUnavailable
	}
	return result, nil
}

// checkFileMutationActor locks account before credential and reads the current
// policy on every phase. An application key retains its independent authority.
func checkFileMutationActor(ctx context.Context, tx pgx.Tx, actor identity.Principal, lock bool) (libraryAccess, error) {
	if actor.IsApplicationKey() {
		if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorEmby, lock); err != nil {
			return libraryAccess{}, errors.Join(ErrForbidden, err)
		}
		access := unrestrictedLibraryAccess()
		access.policy = identity.DefaultManagedPolicy()
		access.policy.EnableContentDeletion = true
		access.policy.EnableSubtitleManagement = true
		return access, nil
	}
	if (actor.Kind != "admin" && actor.Kind != "emby") || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" || !metadataIdentifier(actor.User.ID) || !metadataIdentifier(actor.SessionID) {
		return libraryAccess{}, ErrForbidden
	}
	query := `SELECT is_disabled,is_administrator,CASE WHEN octet_length(policy::text)<=131072 THEN policy END FROM users WHERE id=$1`
	if lock {
		query += " FOR SHARE"
	}
	var disabled, administrator bool
	var policyJSON []byte
	if err := tx.QueryRow(ctx, query, actor.User.ID).Scan(&disabled, &administrator, &policyJSON); errors.Is(err, pgx.ErrNoRows) {
		return libraryAccess{}, ErrForbidden
	} else if err != nil {
		return libraryAccess{}, err
	}
	if disabled || actor.Kind == "admin" && !administrator {
		return libraryAccess{}, ErrForbidden
	}
	query = `SELECT id FROM sessions WHERE id=$1 AND user_id=$2 AND kind=$3`
	if lock {
		query += " FOR SHARE"
	}
	var id string
	if err := tx.QueryRow(ctx, query, actor.SessionID, actor.User.ID, actor.Kind).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return libraryAccess{}, ErrForbidden
	} else if err != nil {
		return libraryAccess{}, err
	}
	var live bool
	var deviceID string
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp(),device_id,clock_timestamp() FROM sessions
		WHERE id=$1 AND user_id=$2 AND kind=$3`, actor.SessionID, actor.User.ID, actor.Kind).Scan(&live, &deviceID, &now); errors.Is(err, pgx.ErrNoRows) {
		return libraryAccess{}, ErrForbidden
	} else if err != nil {
		return libraryAccess{}, err
	}
	access, err := parseLibraryPolicy(policyJSON)
	if err != nil {
		return libraryAccess{}, err
	}
	var loginState struct{ LockedOutDate *int64 }
	if json.Unmarshal(policyJSON, &loginState) != nil || !live || loginState.LockedOutDate != nil && *loginState.LockedOutDate != 0 {
		return libraryAccess{}, ErrForbidden
	}
	if actor.Kind == "emby" && (!access.policy.AllowsDevice(deviceID) || !access.policy.AllowsAccessAt(now) || !access.policy.EnableRemoteAccess && !identity.IsLocalPeer(actor.PeerIP)) {
		return libraryAccess{}, ErrForbidden
	}
	access.userID, access.administrator = actor.User.ID, administrator
	if administrator {
		access.all = true
	}
	return access, nil
}

func deletionActorID(actor identity.Principal) string {
	if actor.IsApplicationKey() {
		return "application:" + strconv.FormatInt(actor.ApplicationKeyID, 10)
	}
	return actor.User.ID
}

func deletionActivity(actor identity.Principal, target fileDeletionTarget) activity.Event {
	event := activity.Event{Action: activity.ActionItemDeleted, Source: activity.SourceEmby, Actor: activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID}, Resource: activity.Resource{Kind: activity.ResourceItem, ID: target.ItemID}, Count: 1}
	if target.Kind == "subtitle" {
		event.Action = activity.ActionSubtitleDeleted
	}
	if actor.Kind == "admin" {
		event.Source = activity.SourceNative
	}
	if actor.IsApplicationKey() {
		event.Actor.Kind = activity.ActorApplicationKey
		event.Actor.ID = strconv.FormatInt(actor.ApplicationKeyID, 10)
	}
	return event
}

func readFileDeletionTarget(ctx context.Context, tx pgx.Tx, actor identity.Principal, id, kind string, index int, lock bool) (fileDeletionTarget, error) {
	var target fileDeletionTarget
	var err error
	if kind == "subtitle" {
		target, err = readSubtitleDeletionTarget(ctx, tx, actor, id, index, lock)
	} else {
		target, err = readMediaDeletionTarget(ctx, tx, actor, id, lock)
	}
	if err != nil {
		return target, err
	}
	target.BindingFingerprint, err = readDeletionBindingFingerprint(ctx, tx, target)
	return target, err
}

func readDeletionBindingFingerprint(ctx context.Context, tx pgx.Tx, target fileDeletionTarget) (string, error) {
	var row rootBindingRow
	err := tx.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`,r.storage_binding IS NOT NULL,
		CASE WHEN octet_length(r.storage_binding::text)<=$2 THEN r.storage_binding::text END,r.bound_at,r.bound_by
		FROM library_roots r WHERE r.id=$1`, target.File.Root.id, storagebinding.MaxDocumentBytes).
		Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath, &row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy)
	if err != nil {
		return "", err
	}
	if row.root != target.File.Root || row.revision != target.BindingRevision {
		return "", ErrSourceChanged
	}
	approved, err := row.validate()
	if err != nil {
		return "", err
	}
	if approved == nil {
		return "", fmt.Errorf("%w: safe deletion requires a verified persistent root binding", ErrUnavailable)
	}
	return approved.Fingerprint()
}

func (s *Store) captureDeletionTopology(ctx context.Context, target fileDeletionTarget) (rootBindingWriteCapture, error) {
	if target.BindingFingerprint == "" {
		return nil, ErrUnavailable
	}
	capture, err := s.captureRootBindingWrite(ctx, target.File.Root)
	if err != nil {
		return nil, err
	}
	snapshot, err := capture.Snapshot()
	if err != nil {
		capture.Close()
		return nil, err
	}
	fingerprint, err := snapshot.Fingerprint()
	if err != nil || fingerprint != target.BindingFingerprint {
		capture.Close()
		return nil, ErrSourceChanged
	}
	if err := capture.Revalidate(ctx); err != nil {
		capture.Close()
		return nil, err
	}
	return capture, nil
}

func sameFileDeletionTarget(first, second fileDeletionTarget) bool {
	first.File.StageName, first.File.StageIdentity, first.File.StagedChangeTimeNs = "", "", 0
	second.File.StageName, second.File.StageIdentity, second.File.StagedChangeTimeNs = "", "", 0
	a, _ := encodeDeletionTarget(first)
	b, _ := encodeDeletionTarget(second)
	return string(a) == string(b)
}

type mediaDeletionOperation struct {
	ID, State, ActorID, CredentialID, OriginHost string
	Target                                       fileDeletionTarget
}

func readMediaDeletionOperation(ctx context.Context, tx pgx.Tx, id, kind string, index int, lock bool) (mediaDeletionOperation, error) {
	query := `SELECT id,state,actor_id,credential_id,origin_host,library_id,root_id,source_snapshot FROM media_deletion_operations WHERE item_id=$1 AND kind=$2 AND subtitle_index=$3`
	if lock {
		query += " FOR UPDATE"
	}
	var op mediaDeletionOperation
	var raw []byte
	var libraryID, rootID string
	if err := tx.QueryRow(ctx, query, id, kind, index).Scan(&op.ID, &op.State, &op.ActorID, &op.CredentialID, &op.OriginHost, &libraryID, &rootID, &raw); err != nil {
		return op, err
	}
	target, err := decodeDeletionTarget(raw)
	op.Target = target
	if err == nil && (target.ItemID != id || target.Kind != kind || target.SubtitleIndex != index || target.LibraryID != libraryID || target.File.Root.id != rootID || target.File.StageName != ".goby-delete-"+op.ID) {
		return mediaDeletionOperation{}, ErrUnavailable
	}
	return op, err
}

// DeleteMediaFor deletes a single indexed media file. Directories and media
// owners with dependent files require an explicit separate workflow.
func (s *Store) DeleteMediaFor(ctx context.Context, actor identity.Principal, itemID string) error {
	return s.deleteManagedFile(ctx, actor, itemID, "media", -1)
}
func (s *Store) DeleteSubtitleAsUser(ctx context.Context, actor identity.Principal, itemID string, index int) error {
	if index < 0 {
		return ErrInvalidInput
	}
	if handled, err := s.deleteOwnedSubtitleAsUser(ctx, actor, itemID, index); handled {
		return err
	}
	return s.deleteManagedFile(ctx, actor, itemID, "subtitle", index)
}

func deletionRecoveryError(err error) error {
	return fmt.Errorf("%w: %w", ErrMediaDeletionRecovery, err)
}

// Imported journals are never executed automatically. Even an explicit retry
// must originate on the machine that prepared the filesystem operation.
func mediaDeletionHostIdentity() (string, error) {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", fmt.Errorf("%w: a stable Linux machine identity is required for recoverable deletion", ErrUnavailable)
	}
	value := strings.TrimSpace(string(data))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 16 {
		return "", ErrUnavailable
	}
	digest := sha256.Sum256(decoded)
	return hex.EncodeToString(digest[:]), nil
}

var fileDeletionWorkers = make(chan struct{}, 4)
var fileDeletionInFlight sync.Map

type fileDeletionKey struct {
	store        *Store
	itemID, kind string
	index        int
}

func (s *Store) deleteManagedFile(ctx context.Context, actor identity.Principal, itemID, kind string, index int) error {
	if ctx == nil || !metadataIdentifier(itemID) || s == nil || s.pool == nil {
		return ErrInvalidInput
	}
	key := fileDeletionKey{s, itemID, kind, index}
	return s.runFileDeletion(ctx, key, func(work context.Context) error {
		return s.performManagedFileDeletion(work, actor, itemID, kind, index)
	})
}

// Admission and Close share Store.mu so Wait never races a new zero-to-one
// worker registration. The private action boundary also lets tests represent a
// storage syscall that remains blocked after its context has been canceled.
func (s *Store) runFileDeletion(ctx context.Context, key fileDeletionKey, action func(context.Context) error) error {
	if _, busy := fileDeletionInFlight.LoadOrStore(key, struct{}{}); busy {
		return ErrBusy
	}
	select {
	case fileDeletionWorkers <- struct{}{}:
	case <-ctx.Done():
		fileDeletionInFlight.Delete(key)
		return ctx.Err()
	}
	s.mu.Lock()
	if s.closed || s.closing.Load() || s.ctx == nil {
		s.mu.Unlock()
		<-fileDeletionWorkers
		fileDeletionInFlight.Delete(key)
		return ErrUnavailable
	}
	s.fileDeletions.Add(1)
	s.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		defer s.fileDeletions.Done()
		defer func() { <-fileDeletionWorkers; fileDeletionInFlight.Delete(key) }()
		work, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(s.ctx, cancel)
		defer stop()
		result <- action(work)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return deletionRecoveryError(ctx.Err())
	}
}

func (s *Store) performManagedFileDeletion(ctx context.Context, actor identity.Principal, itemID, kind string, index int) error {
	return s.performManagedFileDeletionWithHook(ctx, actor, itemID, kind, index, nil)
}

func (s *Store) performManagedFileDeletionWithHook(ctx context.Context, actor identity.Principal, itemID, kind string, index int, afterStage func()) error {
	host, err := mediaDeletionHostIdentity()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	access, err := checkFileMutationActor(ctx, tx, actor, false)
	if err != nil {
		rollback(tx)
		return err
	}
	op, err := readMediaDeletionOperation(ctx, tx, itemID, kind, index, false)
	existing := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		rollback(tx)
		return err
	}
	if existing {
		if op.OriginHost != host || op.ActorID != deletionActorID(actor) && !access.administrator {
			rollback(tx)
			return deletionRecoveryError(ErrForbidden)
		}
		if err := verifyDeletionRoot(ctx, tx, op.Target, false); err != nil {
			rollback(tx)
			return deletionRecoveryError(err)
		}
		if op.State == "catalog_removed" && !access.administrator {
			if op.Target.Kind == "subtitle" {
				if !access.policy.EnableSubtitleManagement || !access.policy.AllowsFeature(identity.FeatureSubtitleManagement) {
					rollback(tx)
					return deletionRecoveryError(ErrForbidden)
				}
				if _, err := readQueryParent(ctx, tx, itemID, access); err != nil {
					rollback(tx)
					return deletionRecoveryError(err)
				}
			} else {
				if _, err := readQueryParent(ctx, tx, op.Target.ParentID, access); err != nil {
					rollback(tx)
					return deletionRecoveryError(err)
				}
				allowed, err := contentDeletionAllowed(ctx, tx, access, op.Target.ParentID)
				if err != nil || !allowed {
					rollback(tx)
					return deletionRecoveryError(ErrForbidden)
				}
			}
		}
		if op.State == "prepared" {
			if _, err := readFileDeletionTarget(ctx, tx, actor, itemID, kind, index, false); err != nil {
				rollback(tx)
				return deletionRecoveryError(err)
			}
		}
	} else {
		op.Target, err = readFileDeletionTarget(ctx, tx, actor, itemID, kind, index, false)
		if err != nil {
			rollback(tx)
			return err
		}
		op.ID, err = randomID()
		if err != nil {
			rollback(tx)
			return err
		}
		op.State, op.ActorID, op.CredentialID, op.OriginHost = "prepared", deletionActorID(actor), actor.SessionID, host
		op.Target.File.StageName = ".goby-delete-" + op.ID
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	topology, err := s.captureDeletionTopology(ctx, op.Target)
	if err != nil {
		if existing {
			return deletionRecoveryError(err)
		}
		return err
	}
	defer topology.Close()
	if op.Target.PrimaryFile != nil && !existing {
		primary := *op.Target.PrimaryFile
		primary.StageName = op.Target.File.StageName
		capture, err := s.prepareFileDeletion(ctx, primary)
		if err != nil {
			return err
		}
		capture.Close()
	}
	capture, err := s.prepareFileDeletion(ctx, op.Target.File)
	if err != nil {
		if existing && op.State == "prepared" {
			empty, inspectionErr := s.deletionStageIsEmpty(ctx, op.Target.File)
			if inspectionErr == nil && empty {
				if cleanupErr := s.removeDeletionJournal(ctx, actor, op, false); cleanupErr != nil {
					return deletionRecoveryError(cleanupErr)
				}
				return fmt.Errorf("%w: interrupted deletion retained no staged file; rescan before retrying", ErrSourceChanged)
			}
		}
		if existing {
			return deletionRecoveryError(err)
		}
		return err
	}
	defer capture.Close()
	if existing {
		if op.State == "prepared" {
			if err := topology.Revalidate(ctx); err != nil {
				return deletionRecoveryError(err)
			}
			if err := capture.Restore(ctx); err != nil {
				return deletionRecoveryError(err)
			}
			if err := s.removeDeletionJournal(ctx, actor, op, false); err != nil {
				return deletionRecoveryError(err)
			}
			return fmt.Errorf("%w: interrupted deletion was restored; rescan the source before retrying", ErrSourceChanged)
		}
		if err := topology.Revalidate(ctx); err != nil {
			return deletionRecoveryError(err)
		}
		if err := capture.Purge(ctx); err != nil {
			return deletionRecoveryError(err)
		}
		return s.removeDeletionJournal(ctx, actor, op, true)
	}
	if err := s.prepareDeletionJournal(ctx, actor, op); err != nil {
		return err
	}
	if err := topology.Revalidate(ctx); err != nil {
		return s.restoreFailedDeletion(actor, op, capture, err)
	}
	stageIdentity, stagedChange, err := capture.Stage(ctx)
	if err != nil {
		return s.restoreFailedDeletion(actor, op, capture, err)
	}
	op.Target.File.StageIdentity, op.Target.File.StagedChangeTimeNs = stageIdentity, stagedChange
	if afterStage != nil {
		afterStage()
	}
	if err := topology.Revalidate(ctx); err != nil {
		return s.restoreFailedDeletion(actor, op, capture, err)
	}
	committed, err := s.commitFileDeletion(ctx, actor, op)
	if err != nil {
		if committed {
			return deletionRecoveryError(err)
		}
		return s.restoreFailedDeletion(actor, op, capture, err)
	}
	capture.Close()
	op.State = "catalog_removed"
	// Purge accepts only the durable post-rename snapshot, never the prepared
	// source. A commit error above leaves that snapshot for an explicit retry.
	cleanup, err := s.prepareFileDeletion(ctx, op.Target.File)
	if err != nil {
		return deletionRecoveryError(err)
	}
	defer cleanup.Close()
	if err := topology.Revalidate(ctx); err != nil {
		return deletionRecoveryError(err)
	}
	if err := cleanup.Purge(ctx); err != nil {
		return deletionRecoveryError(err)
	}
	return s.removeDeletionJournal(ctx, actor, op, true)
}

func verifyDeletionRoot(ctx context.Context, tx pgx.Tx, target fileDeletionTarget, lock bool) error {
	query := `SELECT id,library_id,path,allowed_path,relative_path,binding_revision FROM library_roots WHERE id=$1`
	if lock {
		query += " FOR SHARE"
	}
	var root libraryRoot
	var revision int64
	if err := tx.QueryRow(ctx, query, target.File.Root.id).Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath, &revision); err != nil {
		return err
	}
	if root != target.File.Root || revision != target.BindingRevision {
		return ErrSourceChanged
	}
	fingerprint, err := readDeletionBindingFingerprint(ctx, tx, target)
	if err != nil {
		return err
	}
	if fingerprint != target.BindingFingerprint {
		return ErrSourceChanged
	}
	return nil
}

func lockDeletionLibrary(ctx context.Context, tx pgx.Tx, target fileDeletionTarget) error {
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM libraries WHERE id=$1 FOR UPDATE`, target.LibraryID).Scan(&id); err != nil {
		return err
	}
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))`, target.LibraryID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	if err := tx.QueryRow(ctx, mediaPublicationLibraryActiveSQL, target.LibraryID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	return verifyDeletionRoot(ctx, tx, target, true)
}

func (s *Store) prepareDeletionJournal(ctx context.Context, actor identity.Principal, op mediaDeletionOperation) error {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if _, err := checkFileMutationActor(protected, tx, actor, true); err != nil {
		return err
	}
	if err := lockDeletionLibrary(protected, tx, op.Target); err != nil {
		return err
	}
	current, err := readFileDeletionTarget(protected, tx, actor, op.Target.ItemID, op.Target.Kind, op.Target.SubtitleIndex, true)
	if err != nil {
		return err
	}
	if !sameFileDeletionTarget(current, op.Target) {
		return ErrSourceChanged
	}
	encoded, err := encodeDeletionTarget(op.Target)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(protected, `INSERT INTO media_deletion_operations(id,item_id,library_id,root_id,actor_id,credential_id,origin_host,kind,subtitle_index,state,source_snapshot)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'prepared',$10)`, op.ID, op.Target.ItemID, op.Target.LibraryID, op.Target.File.Root.id, op.ActorID, op.CredentialID, op.OriginHost, op.Target.Kind, op.Target.SubtitleIndex, encoded); err != nil {
		return err
	}
	if _, err := checkFileMutationActor(protected, tx, actor, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

// The bool distinguishes a commit that was attempted from a transaction that
// certainly cannot commit. An ambiguous outcome must never restore the file.
func (s *Store) commitFileDeletion(ctx context.Context, actor identity.Principal, op mediaDeletionOperation) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if _, err := checkFileMutationActor(protected, tx, actor, true); err != nil {
		return false, err
	}
	if err := lockDeletionLibrary(protected, tx, op.Target); err != nil {
		return false, err
	}
	current, err := readFileDeletionTarget(protected, tx, actor, op.Target.ItemID, op.Target.Kind, op.Target.SubtitleIndex, true)
	if err != nil {
		return false, err
	}
	if !sameFileDeletionTarget(current, op.Target) {
		return false, ErrSourceChanged
	}
	stored, err := readMediaDeletionOperation(protected, tx, op.Target.ItemID, op.Target.Kind, op.Target.SubtitleIndex, true)
	if err != nil {
		return false, err
	}
	if stored.ID != op.ID || stored.State != "prepared" {
		return false, ErrBusy
	}
	if op.Target.Kind == "subtitle" {
		err = applySubtitleDeletion(protected, tx, op.Target)
	} else {
		err = applyMediaDeletion(protected, tx, op.Target)
	}
	if err != nil {
		return false, err
	}
	encoded, err := encodeDeletionTarget(op.Target)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(protected, `UPDATE media_deletion_operations SET state='catalog_removed',source_snapshot=$2,updated_at=clock_timestamp() WHERE id=$1`, op.ID, encoded); err != nil {
		return false, err
	}
	if err := activity.RecordOwned(catalogActivityTx{tx: tx}, deletionActivity(actor, op.Target)); err != nil {
		return false, err
	}
	if _, err := checkFileMutationActor(protected, tx, actor, false); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, tx.Commit(protected)
}

func (s *Store) restoreFailedDeletion(actor identity.Principal, op mediaDeletionOperation, capture *fileDeletionCapture, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if !capture.HasStagedPayload() {
		if err := s.removeDeletionJournal(ctx, actor, op, false); err != nil {
			return deletionRecoveryError(errors.Join(cause, err))
		}
		return cause
	}
	if err := capture.Restore(ctx); err != nil {
		return deletionRecoveryError(errors.Join(cause, err))
	}
	// Restoration is compensation for this server's own prepared move, not a
	// new deletion grant. Clearing its prepared journal permits a normal rescan.
	if err := s.removeDeletionJournal(ctx, actor, op, false); err != nil {
		return deletionRecoveryError(errors.Join(cause, err))
	}
	return cause
}

func (s *Store) removeDeletionJournal(ctx context.Context, actor identity.Principal, op mediaDeletionOperation, committed bool) error {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	state := "prepared"
	if committed {
		state = "catalog_removed"
	}
	if _, err := tx.Exec(ctx, `DELETE FROM media_deletion_operations WHERE id=$1 AND state=$2`, op.ID, state); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
