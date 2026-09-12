//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

type rootBindingRegistrationFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *Store
	parent   string
	approved string
}

func newRootBindingRegistrationFixture(t *testing.T) rootBindingRegistrationFixture {
	t.Helper()
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	parent := rootStorageTestDirectory(t)
	approved := filepath.Join(parent, "approved")
	if err := os.Mkdir(approved, 0o700); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: approved})
	store.mu.Unlock()
	return rootBindingRegistrationFixture{ctx: ctx, pool: pool, store: store, parent: parent, approved: approved}
}

func (fixture rootBindingRegistrationFixture) directory(t *testing.T, name, content string) string {
	t.Helper()
	return filepath.Dir(libraryIntegrationFile(t, fixture.approved, name+"/marker.txt", content))
}

func rootBindingRegistrationRows(t *testing.T, fixture rootBindingRegistrationFixture, libraryID string) []rootBindingRow {
	t.Helper()
	rows, err := fixture.pool.Query(fixture.ctx, `SELECT id, library_id, path, allowed_path, relative_path,
		binding_revision, storage_binding IS NOT NULL, storage_binding::text, bound_at, bound_by
		FROM library_roots WHERE library_id = $1 ORDER BY path`, libraryID)
	if err != nil {
		t.Fatalf("read registered binding rows: %v", err)
	}
	defer rows.Close()
	var result []rootBindingRow
	for rows.Next() {
		var row rootBindingRow
		if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath,
			&row.root.relativePath, &row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
			t.Fatalf("decode registered binding row: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish registered binding rows: %v", err)
	}
	return result
}

func rootBindingRegistrationOnlyRow(t *testing.T, fixture rootBindingRegistrationFixture, libraryID string) rootBindingRow {
	t.Helper()
	rows := rootBindingRegistrationRows(t, fixture, libraryID)
	if len(rows) != 1 {
		t.Fatalf("registered root count = %d, want one", len(rows))
	}
	return rows[0]
}

func rootBindingRegistrationAssertApproved(t *testing.T, fixture rootBindingRegistrationFixture, row rootBindingRow, libraryID, boundBy string) {
	t.Helper()
	if row.root.libraryID != libraryID || row.root.id == "" || row.revision != 1 || !row.stored ||
		row.boundAt == nil || row.boundAt.IsZero() || row.boundBy == nil || *row.boundBy != boundBy {
		t.Fatalf("registration did not persist its initial complete binding: %+v", row)
	}
	persisted, err := storagebinding.DecodeSnapshot(row.document)
	if err != nil {
		t.Fatalf("decode initial registered binding: %v", err)
	}
	observed, err := fixture.store.observeRootBinding(fixture.ctx, row.root)
	if err != nil || !reflect.DeepEqual(persisted, observed) {
		t.Fatalf("registration document differs from the current complete topology: %v", err)
	}
	fixture.store.mu.Lock()
	anchor, ok := fixture.store.rootBindingAnchors[row.root.id]
	fixture.store.mu.Unlock()
	if !ok || anchor.root != row.root || anchor.approved == nil {
		t.Fatalf("registration did not publish its complete root mapping: %+v", anchor.root)
	}
	opened, err := fixture.store.openLibraryRoot(row.root)
	if err != nil {
		t.Fatalf("registration anchor is unavailable immediately after commit: %v", err)
	}
	defer opened.Close()
	file, err := opened.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	identity, err := ObserveRootStorageIdentity(file)
	if err != nil || !identity.Identity.Equal(persisted.RegisteredRoot) {
		t.Fatalf("published registration anchor differs from the committed identity: %v", err)
	}
}

func TestRootBindingRegistrationIntegrationPersistsInitialBindingAndOriginalAuditActors(t *testing.T) {
	for _, test := range []struct {
		name        string
		audience    identity.AdministratorAudience
		source      activity.Source
		application bool
	}{
		{name: "system", source: activity.SourceSystem},
		{name: "native", audience: identity.AdministratorNative, source: activity.SourceNative},
		{name: "emby", audience: identity.AdministratorEmby, source: activity.SourceEmby},
		{name: "application key", audience: identity.AdministratorEmby, source: activity.SourceEmby, application: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingRegistrationFixture(t)
			paths := []string{fixture.directory(t, "private-first-root", "first"), fixture.directory(t, "private-second-root", "second")}
			var actor identity.Principal
			boundBy := "system"
			if test.application {
				actor = catalogAuditApplicationActor(t, fixture.ctx, fixture.pool)
				boundBy = "application_key:" + strconv.FormatInt(actor.ApplicationKeyID, 10)
			} else if test.source != activity.SourceSystem {
				actor = catalogAuditActor(t, fixture.ctx, fixture.pool, test.audience)
				boundBy = actor.User.ID
			}
			var library Library
			var err error
			if test.source == activity.SourceSystem {
				library, err = fixture.store.CreateLibrary(fixture.ctx, "Private registration display name", "movies", paths)
			} else {
				library, err = fixture.store.CreateLibraryAsAdministrator(fixture.ctx, actor, test.audience,
					"Private registration display name", "movies", paths)
			}
			if err != nil {
				t.Fatalf("register initial approved storage: %v", err)
			}
			rows := rootBindingRegistrationRows(t, fixture, library.ID)
			if len(rows) != len(paths) {
				t.Fatalf("registration root count = %d, want %d", len(rows), len(paths))
			}
			for _, row := range rows {
				rootBindingRegistrationAssertApproved(t, fixture, row, library.ID, boundBy)
				if facts := catalogAuditFacts(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, row.root.id); len(facts) != 0 {
					t.Fatal("initial registration emitted an explicit rebind audit event")
				}
			}
			fact := catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryCreated, library.ID)
			catalogAuditAssertActor(t, fact, actor, test.source, activity.ResourceLibrary)
			var affected int64
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT affected_count FROM activity_entries
				WHERE action = 'library.created' AND resource_id = $1`, library.ID).Scan(&affected); err != nil || affected != 2 {
				t.Fatalf("registration audit changed its root count: count = %d, error = %v", affected, err)
			}
			for _, private := range []string{"Private registration display name", "private-first-root", "private-second-root", fixture.approved} {
				if strings.Contains(fact.raw, private) {
					t.Errorf("registration audit retained private input %q", private)
				}
			}
		})
	}
}

func TestRootBindingRegistrationIntegrationStartsAtOneAndExplicitRebindAdvancesToTwo(t *testing.T) {
	fixture := newRootBindingRegistrationFixture(t)
	registered := fixture.directory(t, "registered", "original registration")
	library := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "Initial binding revision", "movies", registered)
	row := rootBindingRegistrationOnlyRow(t, fixture, library.ID)
	rootBindingRegistrationAssertApproved(t, fixture, row, library.ID, "system")
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "registration-rebind-administrator")
	initial, err := fixture.store.GetRootBinding(fixture.ctx, actor, library.ID, row.root.id)
	if err != nil || initial.Status != RootBindingVerified || initial.Revision != "1" || initial.BoundBy != "system" {
		t.Fatalf("read initial registration approval: result = %+v, error = %v", initial, err)
	}
	lease, err := fixture.store.leaseLibraryRoot(row.root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := os.Rename(fixture.approved, filepath.Join(fixture.parent, "retained-original")); err != nil {
		t.Fatal(err)
	}
	fixture.directory(t, "registered", "explicitly rebound storage")
	changed, err := fixture.store.GetRootBinding(fixture.ctx, actor, library.ID, row.root.id)
	if err != nil || changed.Status != RootBindingMismatch || changed.Revision != "1" ||
		changed.ObservedFingerprint == "" || changed.ObservedFingerprint == initial.ObservedFingerprint {
		t.Fatalf("replacement did not preserve the initial approval: result = %+v, error = %v", changed, err)
	}
	rebound, err := fixture.store.UpdateRootBinding(fixture.ctx, actor, library.ID, row.root.id, RootBindingUpdate{
		Revision: "1", ObservedFingerprint: changed.ObservedFingerprint, AcknowledgeMissingRemoval: true})
	if err != nil || rebound.Status != RootBindingVerified || rebound.Revision != "2" || rebound.BoundBy != actor.User.ID ||
		rebound.ApprovedFingerprint != changed.ObservedFingerprint {
		t.Fatalf("explicit rebind did not advance the registration revision: result = %+v, error = %v", rebound, err)
	}
	rootBindingPathsAssertOpen(t, fixture.store, row.root, "explicitly rebound storage")
	retained, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	rootBindingPathsAssertContents(t, retained, "original registration")
	fact := catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, row.root.id)
	if fact.revision != 2 {
		t.Fatalf("rebind audit revision = %d, want two", fact.revision)
	}
	catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryCreated, library.ID)
}

func TestRootBindingRegistrationIntegrationIgnoresStaleSharedAnchorAndRetainsExistingRoots(t *testing.T) {
	fixture := newRootBindingRegistrationFixture(t)
	registered := fixture.directory(t, "registered", "old approved storage")
	oldLibrary := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "Existing approved root", "movies", registered)
	old := rootBindingRegistrationOnlyRow(t, fixture, oldLibrary.ID)
	fixture.store.mu.Lock()
	var shared *os.Root
	for index := range fixture.store.roots {
		if fixture.store.roots[index].path == fixture.approved {
			if err := openApprovedRoot(&fixture.store.roots[index]); err != nil {
				fixture.store.mu.Unlock()
				t.Fatal(err)
			}
			shared = fixture.store.roots[index].root
		}
	}
	oldAnchor := fixture.store.rootBindingAnchors[old.root.id].approved
	fixture.store.mu.Unlock()
	if shared == nil || oldAnchor == nil {
		t.Fatal("existing registration did not retain its shared and root-specific fixture anchors")
	}
	if err := os.Rename(fixture.approved, filepath.Join(fixture.parent, "retained-old")); err != nil {
		t.Fatal(err)
	}
	fixture.directory(t, "registered", "current named storage")
	newLibrary := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "New approved root", "movies", registered)
	current := rootBindingRegistrationOnlyRow(t, fixture, newLibrary.ID)
	rootBindingRegistrationAssertApproved(t, fixture, current, newLibrary.ID, "system")
	rootBindingPathsAssertOpen(t, fixture.store, current.root, "current named storage")
	rootBindingPathsAssertOpen(t, fixture.store, old.root, "old approved storage")
	legacy := old.root
	legacy.id = "legacy-shared-anchor-root"
	rootBindingPathsAssertOpen(t, fixture.store, legacy, "old approved storage")
	fixture.store.mu.Lock()
	retainedOld := fixture.store.rootBindingAnchors[old.root.id].approved
	var retainedShared *os.Root
	for _, approved := range fixture.store.roots {
		if approved.path == fixture.approved {
			retainedShared = approved.root
		}
	}
	fixture.store.mu.Unlock()
	if retainedOld != oldAnchor || retainedShared != shared {
		t.Fatal("new registration replaced an existing root or the shared configured anchor")
	}
	if got := rootBindingRegistrationOnlyRow(t, fixture, oldLibrary.ID); !reflect.DeepEqual(got, old) {
		t.Fatal("new registration mutated the existing root's persisted approval")
	}
}

type rootBindingRegistrationTestCapture struct {
	rootBindingRegistrationTopology
	approved     *os.Root
	registered   *os.Root
	snapshotHook func() error
	inside       func(context.Context) error
	snapshots    int
	checks       int
	closes       int
}

func (capture *rootBindingRegistrationTestCapture) Snapshot() (RootTopologySnapshot, error) {
	capture.snapshots++
	if capture.snapshots == 1 && capture.snapshotHook != nil {
		if err := capture.snapshotHook(); err != nil {
			return RootTopologySnapshot{}, err
		}
	}
	return capture.rootBindingRegistrationTopology.Snapshot()
}

func (capture *rootBindingRegistrationTestCapture) Revalidate(ctx context.Context) error {
	capture.checks++
	if capture.checks == 2 && capture.inside != nil {
		if err := capture.inside(ctx); err != nil {
			return err
		}
	}
	return capture.rootBindingRegistrationTopology.Revalidate(ctx)
}

func (capture *rootBindingRegistrationTestCapture) Close() error {
	capture.closes++
	return capture.rootBindingRegistrationTopology.Close()
}

func rootBindingRegistrationTrackCapture(t *testing.T, store *Store, ctx context.Context, lease *libraryRootLease,
	mapping RootTopologyMapping, registered *os.Root) (*rootBindingRegistrationTestCapture, error) {
	t.Helper()
	if !store.mu.TryLock() {
		return nil, errors.New("registration topology capture ran while Store.mu was held")
	}
	store.mu.Unlock()
	if !store.ownership.mu.TryLock() {
		return nil, errors.New("registration topology capture ran inside an owned transaction")
	}
	store.ownership.mu.Unlock()
	topology, err := captureRootBindingRegistrationTopology(ctx, lease, mapping, registered)
	if err != nil {
		return nil, err
	}
	return &rootBindingRegistrationTestCapture{rootBindingRegistrationTopology: topology,
		approved: lease.approved, registered: registered}, nil
}

func rootBindingRegistrationAssertReleased(t *testing.T, captures []*rootBindingRegistrationTestCapture) {
	t.Helper()
	for index, capture := range captures {
		if capture.closes != 1 {
			t.Errorf("registration capture %d close count = %d, want one", index, capture.closes)
		}
		for _, directory := range []*os.Root{capture.approved, capture.registered} {
			if _, err := directory.Stat("."); !errors.Is(err, os.ErrClosed) {
				t.Errorf("registration capture %d retained a directory after return: %v", index, err)
			}
		}
	}
}

func rootBindingRegistrationAssertRejected(t *testing.T, fixture rootBindingRegistrationFixture, result Library, err error, before string) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(result, Library{}) {
		t.Fatalf("rejected registration returned a library: result = %+v, error = %v", result, err)
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("rejected registration partially committed catalog, binding, scan, or activity records")
	}
	fixture.store.mu.Lock()
	anchors := len(fixture.store.rootBindingAnchors)
	fixture.store.mu.Unlock()
	if anchors != 0 {
		t.Fatalf("rejected registration published %d candidate anchors", anchors)
	}
}

func TestRootBindingRegistrationIntegrationMultiRootFailureRollsBackAndReleasesCandidates(t *testing.T) {
	for _, mode := range []string{"second capture", "final topology", "audit insert", "final authority"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newRootBindingRegistrationFixture(t)
			actor := catalogAuditActor(t, fixture.ctx, fixture.pool, identity.AdministratorNative)
			paths := []string{fixture.directory(t, "first", "first candidate"), fixture.directory(t, "second", "second candidate")}
			if mode == "audit insert" || mode == "final authority" {
				catalogAuditInstallTrigger(t, fixture.ctx, fixture.pool, activity.ActionLibraryCreated, mode == "final authority")
			}
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			var captures []*rootBindingRegistrationTestCapture
			var failedApproved, failedRegistered *os.Root
			result, err := fixture.store.createLibraryWithCapture(fixture.ctx,
				&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, "Uncommitted candidate roots", "movies", paths,
				func(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, registered *os.Root) (rootBindingRegistrationTopology, error) {
					if mode == "second capture" && len(captures) == 1 {
						failedApproved, failedRegistered = lease.approved, registered
						return nil, ErrRootTopologyUnavailable
					}
					capture, err := rootBindingRegistrationTrackCapture(t, fixture.store, ctx, lease, mapping, registered)
					if err != nil {
						return nil, err
					}
					captures = append(captures, capture)
					if mode == "final topology" && len(captures) == 2 {
						capture.inside = func(context.Context) error { return ErrRootTopologyChanged }
					}
					return capture, nil
				})
			rootBindingRegistrationAssertRejected(t, fixture, result, err, before)
			rootBindingRegistrationAssertReleased(t, captures)
			wantCaptures := 2
			if mode == "second capture" {
				wantCaptures = 1
				for _, directory := range []*os.Root{failedApproved, failedRegistered} {
					if directory == nil {
						t.Fatal("second root never reached the injected capture failure")
					}
					if _, err := directory.Stat("."); !errors.Is(err, os.ErrClosed) {
						t.Errorf("failed second capture retained a base directory: %v", err)
					}
				}
			}
			if len(captures) != wantCaptures {
				t.Fatalf("failure reached %d captures, want %d", len(captures), wantCaptures)
			}
			if mode == "final topology" && (!errors.Is(err, ErrRootTopologyChanged) || captures[1].checks != 2) {
				t.Fatalf("registration did not reach its final topology failure: checks = %d, error = %v", captures[1].checks, err)
			}
			if mode == "audit insert" {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
					t.Fatalf("registration did not reach its audit failure: %v", err)
				}
			}
			if mode == "final authority" && !errors.Is(err, ErrForbidden) {
				t.Fatalf("registration ignored its final credential check: %v", err)
			}
			if mode == "audit insert" || mode == "final authority" {
				catalogAuditAssertTrigger(t, fixture.ctx, fixture.pool)
			}
		})
	}
}

func TestRootBindingRegistrationIntegrationUnsupportedTopologyRetainsAnchorWithoutScanLearning(t *testing.T) {
	fixture := newRootBindingRegistrationFixture(t)
	registered := fixture.directory(t, "unsupported", "unbound retained storage")
	libraryIntegrationFile(t, registered, "Movie.mkv", "video:unbound registration scan")
	var approved, held *os.Root
	library, err := fixture.store.createLibraryWithCapture(fixture.ctx, nil, "Unsupported registration topology", "movies", []string{registered},
		func(_ context.Context, lease *libraryRootLease, _ RootTopologyMapping, root *os.Root) (rootBindingRegistrationTopology, error) {
			approved, held = lease.approved, root
			return nil, ErrRootStorageIdentityUnsupported
		})
	if err != nil {
		t.Fatalf("unsupported topology prevented a valid directory registration: %v", err)
	}
	row := rootBindingRegistrationOnlyRow(t, fixture, library.ID)
	if row.root.libraryID != library.ID || row.revision != 1 || row.stored || len(row.document) != 0 || row.boundAt != nil || row.boundBy != nil {
		t.Fatalf("unsupported registration invented a persisted identity: %+v", row)
	}
	fixture.store.mu.Lock()
	anchor, installed := fixture.store.rootBindingAnchors[row.root.id]
	fixture.store.mu.Unlock()
	if !installed || anchor.root != row.root || anchor.approved == nil || anchor.approved == approved {
		t.Fatal("unsupported registration did not publish its independent retained anchor")
	}
	for _, directory := range []*os.Root{approved, held} {
		if directory == nil {
			t.Fatal("unsupported fixture did not receive its base directory handles")
		}
		if _, err := directory.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("successful registration retained a temporary directory handle: %v", err)
		}
	}
	rootBindingPathsAssertOpen(t, fixture.store, row.root, "unbound retained storage")
	libraryIntegrationScan(t, fixture.ctx, fixture.store, library.ID, "Completed")
	if after := rootBindingRegistrationOnlyRow(t, fixture, library.ID); !reflect.DeepEqual(after, row) {
		t.Fatal("scan learned or changed an unsupported registration binding")
	}
	catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryCreated, library.ID)
	if facts := catalogAuditFacts(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, row.root.id); len(facts) != 0 {
		t.Fatal("scan emitted an implicit rebind audit event")
	}
}

func TestRootBindingRegistrationIntegrationChecksCurrentAuthorityBeforeFilesystemCapture(t *testing.T) {
	for _, test := range []struct {
		name        string
		audience    identity.AdministratorAudience
		application bool
	}{
		{name: "native", audience: identity.AdministratorNative},
		{name: "emby", audience: identity.AdministratorEmby},
		{name: "application key", audience: identity.AdministratorEmby, application: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingRegistrationFixture(t)
			var actor identity.Principal
			if test.application {
				actor = catalogAuditApplicationActor(t, fixture.ctx, fixture.pool)
			} else {
				actor = catalogAuditActor(t, fixture.ctx, fixture.pool, test.audience)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			called := false
			result, err := fixture.store.createLibraryWithCapture(fixture.ctx,
				&catalogAdministrator{actor: actor, audience: test.audience}, "Revoked registration", "movies",
				[]string{filepath.Join(fixture.approved, "unavailable-directory")},
				func(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (rootBindingRegistrationTopology, error) {
					called = true
					return nil, ErrRootTopologyUnavailable
				})
			rootBindingRegistrationAssertRejected(t, fixture, result, err, before)
			if !errors.Is(err, ErrForbidden) || called {
				t.Fatalf("stale authority reached directory resolution or topology capture: captured = %v, error = %v", called, err)
			}
		})
	}
}

func TestRootBindingRegistrationIntegrationRejectsRevocationOrCancellationAfterCapture(t *testing.T) {
	for _, mode := range []string{"revocation", "request cancellation"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newRootBindingRegistrationFixture(t)
			actor := catalogAuditActor(t, fixture.ctx, fixture.pool, identity.AdministratorNative)
			registered := fixture.directory(t, "registered", "rejected candidate")
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			callerCtx, cancel := context.WithCancel(fixture.ctx)
			defer cancel()
			var capture *rootBindingRegistrationTestCapture
			result, err := fixture.store.createLibraryWithCapture(callerCtx,
				&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, "Rejected after observation", "movies", []string{registered},
				func(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, registered *os.Root) (rootBindingRegistrationTopology, error) {
					var err error
					capture, err = rootBindingRegistrationTrackCapture(t, fixture.store, ctx, lease, mapping, registered)
					if err != nil {
						return nil, err
					}
					capture.snapshotHook = func() error {
						if mode == "request cancellation" {
							cancel()
							return nil
						}
						_, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, actor.SessionID)
						return err
					}
					return capture, nil
				})
			rootBindingRegistrationAssertRejected(t, fixture, result, err, before)
			want := ErrForbidden
			if mode == "request cancellation" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || capture == nil || capture.snapshots == 0 {
				t.Fatalf("registration did not reject %s after observation: %v", mode, err)
			}
			rootBindingRegistrationAssertReleased(t, []*rootBindingRegistrationTestCapture{capture})
		})
	}
}

func TestRootBindingRegistrationIntegrationCommitsAfterCallerCancellationInsideTransaction(t *testing.T) {
	fixture := newRootBindingRegistrationFixture(t)
	actor := catalogAuditActor(t, fixture.ctx, fixture.pool, identity.AdministratorNative)
	registered := fixture.directory(t, "registered", "protected transaction candidate")
	callerCtx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	var capture *rootBindingRegistrationTestCapture
	library, err := fixture.store.createLibraryWithCapture(callerCtx,
		&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, "Cancelled request registration", "movies", []string{registered},
		func(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, registered *os.Root) (rootBindingRegistrationTopology, error) {
			var err error
			capture, err = rootBindingRegistrationTrackCapture(t, fixture.store, ctx, lease, mapping, registered)
			if err != nil {
				return nil, err
			}
			capture.inside = func(ctx context.Context) error {
				cancel()
				if ctx.Err() != nil {
					return fmt.Errorf("registration transaction inherited request cancellation: %w", ctx.Err())
				}
				if fixture.store.mu.TryLock() {
					fixture.store.mu.Unlock()
					return errors.New("registration released scan admission before its final revalidation")
				}
				if fixture.store.ownership.mu.TryLock() {
					fixture.store.ownership.mu.Unlock()
					return errors.New("registration final revalidation ran outside catalog ownership")
				}
				return nil
			}
			return capture, nil
		})
	if err != nil || library.ID == "" || !errors.Is(callerCtx.Err(), context.Canceled) || capture == nil || capture.checks != 2 {
		t.Fatalf("request cancellation prevented protected registration: library = %+v, error = %v, caller = %v", library, err, callerCtx.Err())
	}
	rootBindingRegistrationAssertReleased(t, []*rootBindingRegistrationTestCapture{capture})
	row := rootBindingRegistrationOnlyRow(t, fixture, library.ID)
	rootBindingRegistrationAssertApproved(t, fixture, row, library.ID, actor.User.ID)
	catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryCreated, library.ID)
}

func TestRootBindingRegistrationIntegrationRechecksAuthorityAfterAdministratorLockWait(t *testing.T) {
	fixture := newRootBindingRegistrationFixture(t)
	actor := catalogAuditActor(t, fixture.ctx, fixture.pool, identity.AdministratorNative)
	registered := fixture.directory(t, "registered", "waiting registration candidate")
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	blocker, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blocker)
	var blockerPID int32
	if err := blocker.QueryRow(fixture.ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(fixture.ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	ownerPID := int32(fixture.store.ownership.conn.Conn().PgConn().PID())
	finished := make(chan error, 1)
	var result Library
	var capture *rootBindingRegistrationTestCapture
	go func() {
		var createErr error
		result, createErr = fixture.store.createLibraryWithCapture(fixture.ctx,
			&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, "Waiting registration", "movies", []string{registered},
			func(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, registered *os.Root) (rootBindingRegistrationTopology, error) {
				var err error
				capture, err = rootBindingRegistrationTrackCapture(t, fixture.store, ctx, lease, mapping, registered)
				if err != nil {
					return nil, err
				}
				return capture, nil
			})
		finished <- createErr
	}()
	ownedTransactionsWaitForBlock(t, fixture.ctx, fixture.pool, ownerPID, blockerPID, finished)
	if _, err := blocker.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		rootBindingRegistrationAssertRejected(t, fixture, result, err, before)
		if !errors.Is(err, ErrForbidden) || capture == nil {
			t.Fatalf("registration accepted authority revoked during its lock wait: %v", err)
		}
		rootBindingRegistrationAssertReleased(t, []*rootBindingRegistrationTestCapture{capture})
	case <-time.After(10 * time.Second):
		t.Fatal("registration did not finish after the administrator lock was released")
	}
}
