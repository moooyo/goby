package identity_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type clientSessionListOutcome struct {
	sessions []identity.ClientSession
	err      error
}

func listClientSessionsAsync(ctx context.Context, store *identity.Store, principal identity.Principal) (<-chan clientSessionListOutcome, <-chan struct{}) {
	results := make(chan clientSessionListOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sessions, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{})
		results <- clientSessionListOutcome{sessions: sessions, err: err}
	}()
	return results, done
}

func awaitClientSessionList(t *testing.T, ctx context.Context, results <-chan clientSessionListOutcome) clientSessionListOutcome {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("client session list did not finish before its deadline")
		return clientSessionListOutcome{}
	}
}

func assertClientSessionIDs(t *testing.T, sessions []identity.ClientSession, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(sessions))
	for _, session := range sessions {
		actual = append(actual, session.SessionID)
	}
	slices.Sort(actual)
	expected = slices.Clone(expected)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("client session visibility returned %d sessions with unexpected identities, want %d", len(actual), len(expected))
	}
}

func TestStoreClientSessionListsReuseLockedVisibility(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Session Visibility Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerPrincipal := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "visibility-viewer"}, "emby")
	_, adminPrincipal := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{DeviceID: "visibility-admin"}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Session visibility key")
	keyPrincipal := applicationKeyPrincipal(t, ctx, store, key)
	reader, trace := embyResolutionReader(t, ctx, pool)
	for _, test := range []struct {
		name      string
		remote    bool
		shared    bool
		principal identity.Principal
		queries   int64
		expected  []string
	}{
		{"own", false, false, viewerPrincipal, 9, []string{viewerPrincipal.SessionID}},
		{"other_users", true, false, viewerPrincipal, 9, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID}},
		{"shared_devices", false, true, viewerPrincipal, 9, []string{viewerPrincipal.SessionID, keyPrincipal.ClientSessionID}},
		{"both", true, true, viewerPrincipal, 9, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, keyPrincipal.ClientSessionID}},
		{"administrator", false, false, adminPrincipal, 9, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, keyPrincipal.ClientSessionID}},
		{"application_key", false, false, keyPrincipal, 11, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, keyPrincipal.ClientSessionID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := fmt.Sprintf(`{"EnableRemoteControlOfOtherUsers":%t,"EnableSharedDeviceControl":%t}`, test.remote, test.shared)
			if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
				t.Fatal(err)
			}
			// Retain the original principal so only the locked database policy
			// can grant or withdraw either visibility permission.
			trace.queries.Store(0)
			sessions, err := reader.ListClientSessions(ctx, test.principal, identity.ClientSessionFilter{})
			if err != nil {
				t.Fatal(err)
			}
			assertClientSessionIDs(t, sessions, test.expected)
			// Count BEGIN and COMMIT too: ordinary lists have two complete
			// three-statement authority checks and one list query. Keys retain
			// their two four-statement authority checks.
			if queries := trace.queries.Load(); queries != test.queries {
				t.Fatalf("client session list used %d queries, want %d", queries, test.queries)
			}
		})
	}
}

func TestStoreClientSessionListUsesPolicyAfterAccountWait(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	_, other := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Waiting visibility key")
	keyPrincipal := applicationKeyPrincipal(t, ctx, store, key)
	viewer, err := store.CreateUser(ctx, "Waiting Visibility Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, _ := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{}, "emby")
	const policyUpdate = `UPDATE users SET policy='{"EnableRemoteControlOfOtherUsers":false,"EnableSharedDeviceControl":false}'::jsonb
		|| jsonb_build_object($2::text,$3::boolean) WHERE id=$1`
	for _, permission := range []struct{ field, sessionID string }{
		{"EnableRemoteControlOfOtherUsers", other.SessionID},
		{"EnableSharedDeviceControl", keyPrincipal.ClientSessionID},
	} {
		for _, allowed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s_%t", permission.field, allowed), func(t *testing.T) {
				if _, err := pool.Exec(ctx, policyUpdate, viewer.ID, permission.field, !allowed); err != nil {
					t.Fatal(err)
				}
				principal, err := store.Resolve(ctx, credentials.Token, "emby")
				if err != nil {
					t.Fatal(err)
				}
				blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
				if _, err := blocker.Exec(ctx, policyUpdate, viewer.ID, permission.field, allowed); err != nil {
					t.Fatal(err)
				}
				operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				results, done := listClientSessionsAsync(operationCtx, store, principal)
				waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "SELECT is_administrator, policy FROM users", done)
				if err := blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				result := awaitClientSessionList(t, operationCtx, results)
				if result.err != nil {
					t.Fatal(result.err)
				}
				expected := []string{principal.SessionID}
				if allowed {
					expected = append(expected, permission.sessionID)
				}
				assertClientSessionIDs(t, result.sessions, expected)
			})
		}
	}
}

func TestStoreClientSessionListRejectsExpiryDuringProjectionWait(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, principal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',
		expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, principal.SessionID); err != nil {
		t.Fatal(err)
	}
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	// The initial authority queries do not read devices. This table gate
	// therefore pauses the list only after its account and session are locked.
	if _, err := blocker.Exec(ctx, "LOCK TABLE devices IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, done := listClientSessionsAsync(operationCtx, store, principal)
	waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "WITH clients AS", done)
	waitManagedSessionDatabaseExpiry(t, operationCtx, pool, principal.SessionID, done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitClientSessionList(t, operationCtx, results)
	if !errors.Is(result.err, identity.ErrUnauthorized) || result.sessions != nil {
		t.Fatalf("session list retained expired authority after its projection wait: %v", result.err)
	}
}
