package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

// ErrRootBindingConflict requires a new observation before another approval.
// A fingerprint is only a precondition; it never supplies an identity to save.
var ErrRootBindingConflict = errors.New("root binding revision or observation changed")

type RootBindingUpdate struct {
	Revision                  string
	ObservedFingerprint       string
	AcknowledgeMissingRemoval bool
}

// UpdateRootBinding approves an independently observed current named directory
// and its complete storage topology. It only changes binding and audit records;
// a subsequent complete scan must independently establish missing-file absence.
func (s *Store) UpdateRootBinding(ctx context.Context, actor identity.Principal, libraryID, rootID string, input RootBindingUpdate) (RootBindingInfo, error) {
	return s.updateRootBinding(ctx, actor, libraryID, rootID, input, s.captureRootBindingWrite)
}

// A factory is private so tests can arrange changes at filesystem observation
// boundaries without allowing callers to provide an approved identity document.
type rootBindingCaptureFactory func(context.Context, libraryRoot) (rootBindingWriteCapture, error)

type rootBindingWriteCapture interface {
	Snapshot() (RootTopologySnapshot, error)
	CloneApprovedAnchor() (*os.Root, error)
	Revalidate(context.Context) error
	Close() error
}

func (s *Store) updateRootBinding(ctx context.Context, actor identity.Principal, libraryID, rootID string, input RootBindingUpdate, captureRoot rootBindingCaptureFactory) (RootBindingInfo, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(libraryID) || !validCatalogLibraryIdentifier(rootID) || captureRoot == nil {
		return RootBindingInfo{}, ErrInvalidInput
	}
	revision, err := rootBindingUpdateRevision(input)
	if err != nil {
		return RootBindingInfo{}, err
	}
	if s == nil {
		return RootBindingInfo{}, ErrUnavailable
	}
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	// This completes a fresh credential check before any filesystem operation.
	previous, err := s.readRootBinding(ctx, administrator, libraryID, rootID)
	if err != nil {
		return RootBindingInfo{}, err
	}
	if _, err := previous.validate(); err != nil {
		return RootBindingInfo{}, err
	}
	if previous.revision != revision || revision == math.MaxInt64 {
		return RootBindingInfo{}, ErrRootBindingConflict
	}
	capture, err := captureRoot(ctx, previous.root)
	if err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	if capture == nil {
		return RootBindingInfo{}, ErrUnavailable
	}
	defer capture.Close()
	observed, err := capture.Snapshot()
	if err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	if observed.Mapping.ApprovedPath != previous.root.allowedPath || observed.Mapping.RegisteredPath != previous.root.path {
		return RootBindingInfo{}, ErrRootBindingConflict
	}
	// Freeze a private copy before encoding and projecting the held observation.
	observed = observed.Clone()
	document, err := storagebinding.EncodeSnapshot(observed)
	if err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	fingerprint, err := observed.Fingerprint()
	if err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	if fingerprint != input.ObservedFingerprint {
		return RootBindingInfo{}, ErrRootBindingConflict
	}
	anchor, err := capture.CloneApprovedAnchor()
	if err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	if anchor == nil {
		return RootBindingInfo{}, ErrUnavailable
	}
	defer func() {
		if anchor != nil {
			_ = anchor.Close()
		}
	}()
	if err := capture.Revalidate(ctx); err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}

	// Admission, scan exclusion, and shutdown always take Store.mu before the
	// ownership mutex. Capture revalidation never consults the Store or its cache.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.rootBindingPathConfiguredLocked(previous.root.allowedPath) {
		return RootBindingInfo{}, ErrUnavailable
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return RootBindingInfo{}, fmt.Errorf("begin root binding update: %w", err)
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err := administrator.check(protected, tx, true); err != nil {
		return RootBindingInfo{}, err
	}
	var library string
	if err := tx.QueryRow(protected, `SELECT id FROM libraries WHERE id = $1 FOR UPDATE`, libraryID).Scan(&library); errors.Is(err, pgx.ErrNoRows) {
		return RootBindingInfo{}, ErrNotFound
	} else if err != nil {
		return RootBindingInfo{}, fmt.Errorf("lock root binding library: %w", err)
	}
	current, err := readRootBindingForUpdate(protected, tx, libraryID, rootID)
	if err != nil {
		return RootBindingInfo{}, err
	}
	if err := administrator.check(protected, tx, false); err != nil {
		return RootBindingInfo{}, err
	}
	if !previous.same(current) {
		return RootBindingInfo{}, ErrRootBindingConflict
	}
	var active bool
	if err := tx.QueryRow(protected, `SELECT EXISTS (SELECT 1 FROM scan_jobs
		WHERE library_id = $1 AND status IN ('Queued', 'Running'))`, libraryID).Scan(&active); err != nil {
		return RootBindingInfo{}, fmt.Errorf("read active root binding scan: %w", err)
	}
	if active {
		return RootBindingInfo{}, ErrScanAlreadyActive
	}
	successor := current
	successor.revision++
	successor.stored, successor.document = true, document
	boundBy := strings.Clone(actor.User.ID)
	successor.boundBy = &boundBy
	if err := tx.QueryRow(protected, `UPDATE library_roots SET binding_revision = $3,
		storage_binding = $4::jsonb, bound_at = clock_timestamp(), bound_by = $5
		WHERE library_id = $1 AND id = $2 AND binding_revision = $6 RETURNING bound_at`,
		libraryID, rootID, successor.revision, string(document), boundBy, current.revision).Scan(&successor.boundAt); errors.Is(err, pgx.ErrNoRows) {
		return RootBindingInfo{}, ErrRootBindingConflict
	} else if err != nil {
		return RootBindingInfo{}, fmt.Errorf("save root binding: %w", err)
	}
	boundAt := successor.boundAt.UTC()
	successor.boundAt = &boundAt
	result, err := rootBindingProjection(successor, &observed, observed, nil)
	if err != nil {
		return RootBindingInfo{}, err
	}
	event := administrator.event(activity.ActionLibraryRootBindingUpdated,
		activity.Resource{Kind: activity.ResourceLibraryRoot, ID: rootID})
	event.PreviousRevision, event.Revision = current.revision, successor.revision
	event.ObservationFingerprint = fingerprint
	if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
		return RootBindingInfo{}, err
	}
	// The held named paths, mount namespace, and every boundary must still match
	// before commit. Request cancellation cannot cancel the owned session here.
	if err := capture.Revalidate(protected); err != nil {
		return RootBindingInfo{}, rootBindingObservationError(err)
	}
	if err := administrator.check(protected, tx, false); err != nil {
		return RootBindingInfo{}, err
	}
	if err := tx.Commit(protected); err != nil {
		return RootBindingInfo{}, fmt.Errorf("commit root binding update: %w", err)
	}
	// Publish the already retained approved anchor while scan/media admission is
	// still excluded. No postcommit pathname open can substitute newer storage.
	s.installRootBindingAnchorLocked(previous.root, anchor)
	anchor = nil
	// The result is the committed successor and the retained observation. A
	// cancellable postcommit read must not turn a successful approval into error.
	return result, nil
}

func rootBindingUpdateRevision(input RootBindingUpdate) (int64, error) {
	if !input.AcknowledgeMissingRemoval || len(input.Revision) < 1 || len(input.Revision) > 19 ||
		input.Revision[0] < '1' || input.Revision[0] > '9' || len(input.ObservedFingerprint) != 64 {
		return 0, ErrInvalidInput
	}
	for _, digit := range []byte(input.Revision) {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidInput
		}
	}
	for _, digit := range []byte(input.ObservedFingerprint) {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f') {
			return 0, ErrInvalidInput
		}
	}
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil {
		return 0, ErrRootBindingConflict
	}
	return revision, nil
}

func readRootBindingForUpdate(ctx context.Context, tx pgx.Tx, libraryID, rootID string) (rootBindingRow, error) {
	var row rootBindingRow
	err := tx.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`, r.storage_binding IS NOT NULL,
		CASE WHEN octet_length(r.storage_binding::text) <= $3 THEN r.storage_binding::text END,
		r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
		FROM library_roots r WHERE r.library_id = $1 AND r.id = $2 FOR UPDATE OF r`, libraryID, rootID, storagebinding.MaxDocumentBytes).
		Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return rootBindingRow{}, ErrNotFound
	}
	if err != nil {
		return rootBindingRow{}, fmt.Errorf("lock root binding: %w", err)
	}
	return row, nil
}

// The caller holds Store.mu and has not acquired ownership.mu yet.
func (s *Store) rootBindingPathConfiguredLocked(path string) bool {
	for _, approved := range s.roots {
		if approved.path == path {
			return true
		}
	}
	return false
}

func rootBindingObservationError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrRootTopologyChanged) {
		return errors.Join(ErrRootBindingConflict, err)
	}
	return errors.Join(ErrUnavailable, err)
}

// rootBindingNamedCapture retains all original and topology descriptors until
// the transaction finishes. The cached approved anchor is deliberately unused.
type rootBindingNamedCapture struct {
	*RootTopologyCapture
	lease      *libraryRootLease
	registered *os.Root
}

func (capture *rootBindingNamedCapture) CloneApprovedAnchor() (*os.Root, error) {
	return capture.lease.approved.OpenRoot(".")
}

func (capture *rootBindingNamedCapture) Close() error {
	return errors.Join(capture.RootTopologyCapture.Close(), capture.registered.Close(), capture.lease.Close())
}

func (s *Store) captureRootBindingWrite(ctx context.Context, root libraryRoot) (rootBindingWriteCapture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.configuredRootBindingPath(root)
	if err != nil {
		return nil, err
	}
	approved := approvedRoot{path: path}
	if err := openApprovedRoot(&approved); err != nil {
		return nil, err
	}
	lease := &libraryRootLease{approved: approved.root, relativePath: root.relativePath}
	registered, err := lease.Open()
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	capture, err := lease.CaptureTopology(ctx, RootTopologyMapping{ApprovedPath: path, RegisteredPath: root.path}, registered)
	if err != nil {
		_ = registered.Close()
		_ = lease.Close()
		return nil, err
	}
	return &rootBindingNamedCapture{RootTopologyCapture: capture, lease: lease, registered: registered}, nil
}
