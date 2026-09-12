//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

type rootBindingReadFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *Store
	actor    identity.Principal
	library  Library
	root     RegisteredRootInfo
	snapshot RootTopologySnapshot
}

func newRootBindingReadFixture(t *testing.T) rootBindingReadFixture {
	t.Helper()
	ctx, pool, store, directory, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "root-binding-reader")
	library := libraryIntegrationCreate(t, ctx, store, "Root binding read", "movies", directory)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover registered root: roots = %d, error = %v", len(roots), err)
	}
	storageIdentity := RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: "0102030405060708090a0b0c0d0e0f10", HandleType: 1, Handle: []byte("private-directory-handle")}
	snapshot := RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: RootTopologyMapping{ApprovedPath: roots[0].AllowedPath, RegisteredPath: roots[0].Path},
		Anchor:  storageIdentity, RegisteredRoot: storageIdentity, Boundaries: []RootTopologyBoundary{}}
	return rootBindingReadFixture{ctx, pool, store, actor, library, roots[0], snapshot}
}

func (fixture rootBindingReadFixture) bind(t *testing.T, revision int64) {
	t.Helper()
	raw, err := storagebinding.EncodeSnapshot(fixture.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision = $1,
		storage_binding = $2::jsonb, bound_at = clock_timestamp(), bound_by = $3 WHERE id = $4`,
		revision, string(raw), fixture.actor.User.ID, fixture.root.RootID); err != nil {
		t.Fatalf("store root binding fixture: %v", err)
	}
}

func (fixture rootBindingReadFixture) read(observe rootBindingObserver) (RootBindingInfo, error) {
	return fixture.store.getRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID, observe)
}

func TestRootBindingReadIntegrationProjectsWithoutCatalogOrOwnershipMutations(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 9223372036854775807)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	// Read APIs must remain available while another operation owns the catalog.
	fixture.store.ownership.mu.Lock()
	defer fixture.store.ownership.mu.Unlock()
	ctx, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
	defer cancel()
	result, err := fixture.store.getRootBinding(ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
		func(ctx context.Context, root libraryRoot) (RootTopologySnapshot, error) {
			if !fixture.store.mu.TryLock() {
				t.Fatal("filesystem observation ran while Store.mu was held")
			}
			fixture.store.mu.Unlock()
			return fixture.snapshot, nil
		})
	if err != nil || result.Status != RootBindingVerified || result.Revision != "9223372036854775807" ||
		result.ApprovedFingerprint == "" || result.ApprovedFingerprint != result.ObservedFingerprint ||
		result.BoundAt == nil || result.BoundBy != fixture.actor.User.ID {
		t.Fatalf("read approved binding: result = %+v, error = %v", result, err)
	}
	roots, err := fixture.store.ListRegisteredRoots(ctx, fixture.actor, fixture.library.ID)
	if err != nil || len(roots) != 1 || roots[0].Revision != result.Revision || roots[0].RootID != result.RootID {
		t.Fatalf("list root metadata while catalog is owned: roots = %+v, error = %v", roots, err)
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("root binding reads changed catalog, binding, job, or activity records")
	}
}

func TestRootBindingReadIntegrationUsesCurrentNativeAdministrator(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	for index, test := range []struct {
		name      string
		statement string
		change    func(*identity.Principal)
	}{
		{name: "Emby credential", change: func(actor *identity.Principal) { actor.Kind = "emby" }},
		{name: "application credential", change: func(actor *identity.Principal) {
			*actor = identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: "application-parent", ApplicationKeyID: 1, ClientSessionID: "application-client"}
		}},
		{name: "revoked", statement: `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`},
		{name: "expired", statement: `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`},
		{name: "demoted", statement: `UPDATE users SET is_administrator = false WHERE id = $1`},
		{name: "disabled", statement: `UPDATE users SET is_disabled = true WHERE id = $1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, fmt.Sprintf("root-binding-rejected-%d", index))
			if test.statement != "" {
				id := actor.SessionID
				if test.name == "demoted" || test.name == "disabled" {
					id = actor.User.ID
				}
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, id); err != nil {
					t.Fatal(err)
				}
			}
			if test.change != nil {
				test.change(&actor)
			}
			observed := false
			result, err := fixture.store.getRootBinding(fixture.ctx, actor, fixture.library.ID, fixture.root.RootID,
				func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
					observed = true
					return fixture.snapshot, nil
				})
			if !errors.Is(err, ErrForbidden) || observed || !reflect.DeepEqual(result, RootBindingInfo{}) {
				t.Fatalf("invalid administrator observed root: observed = %v, result = %+v, error = %v", observed, result, err)
			}
			if roots, err := fixture.store.ListRegisteredRoots(fixture.ctx, actor, fixture.library.ID); !errors.Is(err, ErrForbidden) || roots != nil {
				t.Fatalf("invalid administrator discovered roots: roots = %+v, error = %v", roots, err)
			}
		})
	}
	stale := fixture.actor
	stale.User.IsAdministrator, stale.User.IsDisabled, stale.ExpiresAt = false, true, time.Unix(1, 0)
	if _, err := fixture.store.ListRegisteredRoots(fixture.ctx, stale, fixture.library.ID); err != nil {
		t.Fatalf("display snapshot overrode current native authority: %v", err)
	}
}

func TestRootBindingReadIntegrationRechecksAuthorityAndRowsAfterObservation(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
		want      error
	}{
		{"revoked", `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, ErrForbidden},
		{"expired", `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`, ErrForbidden},
		{"demoted", `UPDATE users SET is_administrator = false WHERE id = $1`, ErrForbidden},
		{"revision", `UPDATE library_roots SET binding_revision = binding_revision + 1 WHERE id = $1`, ErrUnavailable},
		{"mapping", `UPDATE library_roots SET path = path || '/changed', relative_path = 'changed' WHERE id = $1`, ErrUnavailable},
		{"document", `UPDATE library_roots SET storage_binding = jsonb_set(storage_binding, '{registered_root,handle}', '"c3dhcHBlZC1oYW5kbGU="'::jsonb) WHERE id = $1`, ErrUnavailable},
		{"binding time", `UPDATE library_roots SET bound_at = bound_at + interval '1 second' WHERE id = $1`, ErrUnavailable},
		{"binding actor", `UPDATE library_roots SET bound_by = 'historical-other-administrator' WHERE id = $1`, ErrUnavailable},
		{"unbound", `UPDATE library_roots SET storage_binding = NULL, bound_at = NULL, bound_by = NULL WHERE id = $1`, ErrUnavailable},
		{"removed", `DELETE FROM library_roots WHERE id = $1`, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 9)
			result, err := fixture.read(func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
				id := fixture.root.RootID
				if test.name == "revoked" || test.name == "expired" {
					id = fixture.actor.SessionID
				} else if test.name == "demoted" {
					id = fixture.actor.User.ID
				}
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, id); err != nil {
					t.Fatalf("change state during observation: %v", err)
				}
				return fixture.snapshot, nil
			})
			if !errors.Is(err, test.want) || !reflect.DeepEqual(result, RootBindingInfo{}) {
				t.Fatalf("returned stale observation: result = %+v, error = %v, want = %v", result, err, test.want)
			}
		})
	}
}

func TestRootBindingReadIntegrationUnavailablePreservesApprovalAndStillReauthorizes(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 11)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	result, err := fixture.read(func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
		return RootTopologySnapshot{}, errors.New("private filesystem detail must not leave the domain")
	})
	if err != nil || result.Status != RootBindingUnavailable || result.Approved == nil || result.BoundAt == nil ||
		result.ApprovedFingerprint == "" || result.Observed != nil || result.ObservedFingerprint != "" {
		t.Fatalf("unavailable read lost stored approval: result = %+v, error = %v", result, err)
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("unavailable observation changed the approved binding")
	}
	result, err = fixture.read(func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
		if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, fixture.actor.SessionID); err != nil {
			t.Fatal(err)
		}
		return RootTopologySnapshot{}, ErrUnavailable
	})
	if !errors.Is(err, ErrForbidden) || !reflect.DeepEqual(result, RootBindingInfo{}) {
		t.Fatalf("unavailable observation bypassed final authority: result = %+v, error = %v", result, err)
	}
}

func TestRootBindingReadIntegrationListsMetadataWithoutDecodingDocuments(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 15)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET storage_binding = '{"unknown":true}'::jsonb WHERE id = $1`, fixture.root.RootID); err != nil {
		t.Fatal(err)
	}
	if roots, err := fixture.store.ListRegisteredRoots(fixture.ctx, fixture.actor, fixture.library.ID); err != nil || len(roots) != 1 || roots[0].Revision != "15" {
		t.Fatalf("root discovery decoded a topology document: roots = %+v, error = %v", roots, err)
	}
	observed := false
	if _, err := fixture.read(func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
		observed = true
		return fixture.snapshot, nil
	}); !errors.Is(err, ErrUnavailable) || observed {
		t.Fatalf("invalid stored document reached filesystem observation: observed = %v, error = %v", observed, err)
	}
	if _, err := fixture.store.GetRootBinding(fixture.ctx, fixture.actor, "another-library", fixture.root.RootID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("root ID escaped its library: %v", err)
	}
	if _, err := fixture.store.ListRegisteredRoots(fixture.ctx, fixture.actor, "another-library"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing library was returned as a registered root collection: %v", err)
	}
}

func TestRootBindingReadIntegrationRejectsOversizedLegacyRelativePath(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	if fixture.root.Path != fixture.root.AllowedPath {
		t.Fatal("the fixture must register the configured anchor itself")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET relative_path = $1 WHERE id = $2`,
		strings.Repeat("x", storagebinding.MaxPathBytes+1), fixture.root.RootID); err != nil {
		t.Fatal(err)
	}
	if roots, err := fixture.store.ListRegisteredRoots(fixture.ctx, fixture.actor, fixture.library.ID); !errors.Is(err, ErrUnavailable) || roots != nil {
		t.Fatalf("oversized relative path became the legacy empty path: roots = %+v, error = %v", roots, err)
	}
	observed := false
	if _, err := fixture.read(func(context.Context, libraryRoot) (RootTopologySnapshot, error) {
		observed = true
		return fixture.snapshot, nil
	}); !errors.Is(err, ErrUnavailable) || observed {
		t.Fatalf("oversized relative path reached observation: observed = %v, error = %v", observed, err)
	}
}

func TestRootBindingReadIntegrationCurrentFilesystemStates(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	parent := rootStorageTestDirectory(t)
	directory := parent + "/approved"
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: directory})
	store.mu.Unlock()
	actor := metadataEditTestActor(t, ctx, pool, "root-binding-live-reader")
	library := libraryIntegrationCreate(t, ctx, store, "Live root binding", "movies", directory)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover live root: roots = %+v, error = %v", roots, err)
	}
	root := libraryRoot{id: roots[0].RootID, libraryID: library.ID, path: directory, allowedPath: directory, relativePath: "."}
	snapshot := rootBindingReadFilesystemSnapshot(t, store, root)
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding = NULL, bound_at = NULL, bound_by = NULL
		WHERE id = $1`, root.id); err != nil {
		t.Fatalf("seed a legacy unbound root: %v", err)
	}
	result, err := store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || result.Status != RootBindingUnbound || result.Observed == nil || result.Approved != nil {
		t.Fatalf("legacy root binding observation: result = %+v, error = %v", result, err)
	}
	fixture := rootBindingReadFixture{ctx: ctx, pool: pool, store: store, actor: actor, library: library, root: roots[0], snapshot: snapshot}
	fixture.bind(t, 2)
	result, err = store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || result.Status != RootBindingVerified || result.ApprovedFingerprint != result.ObservedFingerprint {
		t.Fatalf("approved live root did not verify: result = %+v, error = %v", result, err)
	}
	before := catalogAuditSnapshot(t, ctx, pool)
	if err := os.Rename(directory, parent+"/retained-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || result.Status != RootBindingMismatch || result.ApprovedFingerprint == result.ObservedFingerprint || result.BoundBy != actor.User.ID {
		t.Fatalf("replacement root reused cached approval: result = %+v, error = %v", result, err)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	result, err = store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || result.Status != RootBindingUnavailable || result.Approved == nil || result.Observed != nil {
		t.Fatalf("missing root rewrote stored approval: result = %+v, error = %v", result, err)
	}
	if after := catalogAuditSnapshot(t, ctx, pool); after != before {
		t.Fatal("live storage status reads modified the binding or catalog")
	}
}
