package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

const maxRegisteredRootBindingList = 4096

type RootBindingStatus string

const (
	RootBindingUnbound     RootBindingStatus = "unbound"
	RootBindingVerified    RootBindingStatus = "verified"
	RootBindingMismatch    RootBindingStatus = "mismatch"
	RootBindingUnavailable RootBindingStatus = "unavailable"
)

// RegisteredRootInfo identifies a catalog registration without opening storage.
// Revision remains an exact decimal string for clients with limited integers.
type RegisteredRootInfo struct {
	RootID       string `json:"root_id"`
	LibraryID    string `json:"library_id"`
	Path         string `json:"path"`
	AllowedPath  string `json:"allowed_path"`
	RelativePath string `json:"relative_path"`
	Revision     string `json:"revision"`
}

// RootBindingIdentityInfo is a display projection, never an opaque file handle.
type RootBindingIdentityInfo struct {
	Profile        string `json:"profile"`
	FilesystemUUID string `json:"filesystem_uuid"`
	Digest         string `json:"digest"`
}

type RootBindingBoundaryInfo struct {
	RelativePath string                  `json:"relative_path"`
	Identity     RootBindingIdentityInfo `json:"identity"`
}

// RootBindingTopologyInfo contains all bounded boundary summaries. It cannot
// reconstruct the storage document or act as a live filesystem witness.
type RootBindingTopologyInfo struct {
	Anchor         RootBindingIdentityInfo   `json:"anchor"`
	RegisteredRoot RootBindingIdentityInfo   `json:"registered_root"`
	Boundaries     []RootBindingBoundaryInfo `json:"boundaries"`
}

// RootBindingInfo reports one observation, not permission to delete media.
// ObservedFingerprint can become stale immediately after this read. A later
// approval must independently observe storage and compare it again.
type RootBindingInfo struct {
	RegisteredRootInfo
	Status              RootBindingStatus        `json:"status"`
	ApprovedFingerprint string                   `json:"approved_fingerprint,omitempty"`
	ObservedFingerprint string                   `json:"observed_fingerprint,omitempty"`
	Approved            *RootBindingTopologyInfo `json:"approved,omitempty"`
	Observed            *RootBindingTopologyInfo `json:"observed,omitempty"`
	BoundAt             *time.Time               `json:"bound_at,omitempty"`
	BoundBy             string                   `json:"bound_by,omitempty"`
}

type rootBindingRow struct {
	root     libraryRoot
	revision int64
	stored   bool
	document []byte
	boundAt  *time.Time
	boundBy  *string
}

// The bounded projections prevent oversized persisted values from entering the
// process. Invalid replacements fail mapping validation rather than being used.
const rootBindingMetadataColumns = `
	CASE WHEN octet_length(r.id) <= 256 THEN r.id ELSE '' END,
	CASE WHEN octet_length(r.library_id) <= 256 THEN r.library_id ELSE '' END,
	CASE WHEN octet_length(r.path) <= 4096 THEN r.path ELSE '' END,
	CASE WHEN octet_length(r.allowed_path) <= 4096 THEN r.allowed_path ELSE '' END,
	CASE WHEN octet_length(r.relative_path) <= 4096 THEN r.relative_path ELSE '..' END,
	r.binding_revision`

// ListRegisteredRoots discovers root IDs using bounded catalog metadata only.
// It neither reads stored topology documents nor opens any media directory.
func (s *Store) ListRegisteredRoots(ctx context.Context, actor identity.Principal, libraryID string) ([]RegisteredRootInfo, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(libraryID) {
		return nil, ErrInvalidInput
	}
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	tx, err := s.beginRootBindingRead(ctx, administrator)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $1)`, libraryID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find root binding library: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT `+rootBindingMetadataColumns+`
		FROM library_roots r WHERE r.library_id = $1 ORDER BY r.path, r.id LIMIT $2`, libraryID, maxRegisteredRootBindingList+1)
	if err != nil {
		return nil, fmt.Errorf("list registered library roots: %w", err)
	}
	defer rows.Close()
	result := make([]RegisteredRootInfo, 0)
	for rows.Next() {
		var row rootBindingRow
		if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path,
			&row.root.allowedPath, &row.root.relativePath, &row.revision); err != nil {
			return nil, fmt.Errorf("read registered library root: %w", err)
		}
		if len(result) == maxRegisteredRootBindingList || row.validateMapping() != nil {
			return nil, ErrUnavailable
		}
		result = append(result, row.info())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish registered library roots: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete registered library roots read: %w", err)
	}
	return result, nil
}

// GetRootBinding revalidates a native administrator before and after observing
// the registered root's current configured pathname and complete mount topology.
func (s *Store) GetRootBinding(ctx context.Context, actor identity.Principal, libraryID, rootID string) (RootBindingInfo, error) {
	return s.getRootBinding(ctx, actor, libraryID, rootID, s.observeRootBinding)
}

type rootBindingObserver func(context.Context, libraryRoot) (RootTopologySnapshot, error)

func (s *Store) getRootBinding(ctx context.Context, actor identity.Principal, libraryID, rootID string, observe rootBindingObserver) (RootBindingInfo, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(libraryID) || !validCatalogLibraryIdentifier(rootID) {
		return RootBindingInfo{}, ErrInvalidInput
	}
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	row, err := s.readRootBinding(ctx, administrator, libraryID, rootID)
	if err != nil {
		return RootBindingInfo{}, err
	}
	approved, err := row.validate()
	if err != nil {
		return RootBindingInfo{}, err
	}
	observed, observationErr := observe(ctx, row.root)
	if err := ctx.Err(); err != nil {
		return RootBindingInfo{}, err
	}
	// A new Read Committed transaction observes revocation and row changes made
	// while filesystem calls were in progress. No transaction spans those calls.
	current, err := s.readRootBinding(ctx, administrator, libraryID, rootID)
	if err != nil {
		return RootBindingInfo{}, err
	}
	if !row.same(current) {
		return RootBindingInfo{}, fmt.Errorf("%w: root binding changed during observation", ErrUnavailable)
	}
	return rootBindingProjection(row, approved, observed, observationErr)
}

func (s *Store) beginRootBindingRead(ctx context.Context, administrator *catalogAdministrator) (pgx.Tx, error) {
	// These reads never acquire catalog ownership or hold Store.mu during I/O.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin root binding read: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func (s *Store) readRootBinding(ctx context.Context, administrator *catalogAdministrator, libraryID, rootID string) (rootBindingRow, error) {
	tx, err := s.beginRootBindingRead(ctx, administrator)
	if err != nil {
		return rootBindingRow{}, err
	}
	defer rollback(tx)
	var row rootBindingRow
	err = tx.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`, r.storage_binding IS NOT NULL,
		CASE WHEN octet_length(r.storage_binding::text) <= $3 THEN r.storage_binding::text END,
		r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
		FROM library_roots r WHERE r.library_id = $1 AND r.id = $2`, libraryID, rootID, storagebinding.MaxDocumentBytes).
		Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return rootBindingRow{}, ErrNotFound
	}
	if err != nil {
		return rootBindingRow{}, fmt.Errorf("read root binding: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return rootBindingRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return rootBindingRow{}, fmt.Errorf("complete root binding read: %w", err)
	}
	return row, nil
}

func (row rootBindingRow) validateMapping() error {
	if row.revision < 1 || !validCatalogLibraryIdentifier(row.root.id) || !validCatalogLibraryIdentifier(row.root.libraryID) {
		return ErrUnavailable
	}
	relative, err := (RootTopologyMapping{ApprovedPath: row.root.allowedPath, RegisteredPath: row.root.path}).Relative()
	if err != nil || row.root.relativePath != relative && !(relative == "." && row.root.relativePath == "") {
		return ErrUnavailable
	}
	return nil
}

func (row rootBindingRow) validate() (*RootTopologySnapshot, error) {
	if err := row.validateMapping(); err != nil {
		return nil, err
	}
	if !row.stored {
		if row.document != nil || row.boundAt != nil || row.boundBy != nil {
			return nil, ErrUnavailable
		}
		return nil, nil
	}
	if row.boundAt == nil || row.boundAt.Year() < 0 || row.boundAt.Year() > 9999 ||
		row.boundBy == nil || !validRootBindingActorID(*row.boundBy) {
		return nil, ErrUnavailable
	}
	snapshot, err := storagebinding.DecodeSnapshot(row.document)
	if err != nil || snapshot.Mapping.ApprovedPath != row.root.allowedPath || snapshot.Mapping.RegisteredPath != row.root.path {
		return nil, ErrUnavailable
	}
	return &snapshot, nil
}

func validRootBindingActorID(value string) bool {
	if len(value) < 1 || len(value) > 256 {
		return false
	}
	for index, char := range []byte(value) {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		if index == 0 || char != '.' && char != '_' && char != ':' && char != '-' {
			return false
		}
	}
	return true
}

func (row rootBindingRow) info() RegisteredRootInfo {
	relative := row.root.relativePath
	if relative == "" {
		relative = "."
	}
	return RegisteredRootInfo{RootID: row.root.id, LibraryID: row.root.libraryID, Path: row.root.path,
		AllowedPath: row.root.allowedPath, RelativePath: relative, Revision: strconv.FormatInt(row.revision, 10)}
}

func (row rootBindingRow) same(other rootBindingRow) bool {
	if row.root != other.root || row.revision != other.revision || row.stored != other.stored || !bytes.Equal(row.document, other.document) ||
		(row.boundAt == nil) != (other.boundAt == nil) || (row.boundBy == nil) != (other.boundBy == nil) {
		return false
	}
	return (row.boundAt == nil || row.boundAt.Equal(*other.boundAt)) && (row.boundBy == nil || *row.boundBy == *other.boundBy)
}

// configuredRootBindingPath copies configuration only; it never opens or clones
// the cached descriptor, which may still name a replaced approved directory.
func (s *Store) configuredRootBindingPath(root libraryRoot) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ErrUnavailable
	}
	for _, approved := range s.roots {
		if approved.path == root.allowedPath {
			return strings.Clone(approved.path), nil
		}
	}
	return "", ErrUnavailable
}

func (s *Store) observeRootBinding(ctx context.Context, root libraryRoot) (RootTopologySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return RootTopologySnapshot{}, err
	}
	path, err := s.configuredRootBindingPath(root)
	if err != nil {
		return RootTopologySnapshot{}, err
	}
	approved := approvedRoot{path: path}
	if err := openApprovedRoot(&approved); err != nil {
		return RootTopologySnapshot{}, err
	}
	lease := &libraryRootLease{approved: approved.root, relativePath: root.relativePath}
	defer lease.Close()
	registered, err := lease.Open()
	if err != nil {
		return RootTopologySnapshot{}, err
	}
	defer registered.Close()
	capture, err := lease.CaptureTopology(ctx, RootTopologyMapping{ApprovedPath: path, RegisteredPath: root.path}, registered)
	if err != nil {
		return RootTopologySnapshot{}, err
	}
	defer capture.Close()
	return capture.Snapshot()
}

func rootBindingProjection(row rootBindingRow, approved *RootTopologySnapshot, observed RootTopologySnapshot, observationErr error) (RootBindingInfo, error) {
	result := RootBindingInfo{RegisteredRootInfo: row.info(), Status: RootBindingUnavailable}
	if approved != nil {
		var err error
		result.Approved, result.ApprovedFingerprint, err = rootBindingTopologyInfo(*approved)
		if err != nil {
			return RootBindingInfo{}, ErrUnavailable
		}
		boundAt := *row.boundAt
		result.BoundAt, result.BoundBy = &boundAt, *row.boundBy
	}
	if observationErr != nil {
		return result, nil
	}
	if observed.Mapping.ApprovedPath != row.root.allowedPath || observed.Mapping.RegisteredPath != row.root.path {
		return result, nil
	}
	var err error
	result.Observed, result.ObservedFingerprint, err = rootBindingTopologyInfo(observed)
	if err != nil {
		return result, nil
	}
	switch {
	case approved == nil:
		result.Status = RootBindingUnbound
	case result.ApprovedFingerprint == result.ObservedFingerprint:
		result.Status = RootBindingVerified
	default:
		result.Status = RootBindingMismatch
	}
	return result, nil
}

func rootBindingTopologyInfo(snapshot RootTopologySnapshot) (*RootBindingTopologyInfo, string, error) {
	fingerprint, err := snapshot.Fingerprint()
	if err != nil {
		return nil, "", err
	}
	result := &RootBindingTopologyInfo{Anchor: rootBindingIdentityInfo(snapshot.Anchor),
		RegisteredRoot: rootBindingIdentityInfo(snapshot.RegisteredRoot), Boundaries: make([]RootBindingBoundaryInfo, 0, len(snapshot.Boundaries))}
	for _, boundary := range snapshot.Boundaries {
		result.Boundaries = append(result.Boundaries, RootBindingBoundaryInfo{
			RelativePath: boundary.RelativePath, Identity: rootBindingIdentityInfo(boundary.Identity)})
	}
	sort.Slice(result.Boundaries, func(i, j int) bool { return result.Boundaries[i].RelativePath < result.Boundaries[j].RelativePath })
	return result, fingerprint, nil
}

func rootBindingIdentityInfo(value RootStorageIdentity) RootBindingIdentityInfo {
	// Callers validate the enclosing snapshot first. Encoding this fixed struct
	// cannot fail, and only its digest is retained in the public projection.
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return RootBindingIdentityInfo{Profile: value.Profile, FilesystemUUID: value.FilesystemUUID, Digest: hex.EncodeToString(digest[:])}
}
