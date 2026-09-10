package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type managedSessionRevocationOutcome struct {
	revocation identity.ManagedSessionRevocation
	err        error
}

func revokeManagedSessionAsync(ctx context.Context, store *identity.Store, actor identity.Principal, sessionID string) (<-chan managedSessionRevocationOutcome, <-chan struct{}) {
	results := make(chan managedSessionRevocationOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		revocation, err := store.RevokeManagedSession(ctx, actor, sessionID)
		results <- managedSessionRevocationOutcome{revocation: revocation, err: err}
	}()
	return results, done
}

func awaitManagedSessionRevocation(t *testing.T, ctx context.Context, results <-chan managedSessionRevocationOutcome) managedSessionRevocationOutcome {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("managed session revocation did not finish before its deadline")
		return managedSessionRevocationOutcome{}
	}
}

func managedSessionBlocker(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, int32) {
	t.Helper()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = blocker.Rollback(cleanupCtx)
	})
	var pid int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return blocker, pid
}

// Ignore only the permitted timestamp change; account revisions, credentials,
// sibling sessions, and all other authentication metadata remain in the snapshot.
func managedSessionStableSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, sessionID string) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'user', (SELECT to_jsonb(u) FROM users u WHERE id = $1),
		'sessions', COALESCE((SELECT jsonb_agg(
			CASE WHEN s.id = $2 THEN to_jsonb(s) - 'revoked_at' ELSE to_jsonb(s) END ORDER BY s.id)
			FROM sessions s WHERE s.user_id = $1), '[]'::jsonb))::text`, userID, sessionID).Scan(&snapshot)
	if err != nil {
		t.Fatalf("snapshot managed session metadata: %v", err)
	}
	return snapshot
}

// AFTER UPDATE guarantees that the real target write has already happened when
// pg_blocking_pids observes this gate. The counter is a transactional side effect
// that must commit with an authorized write or disappear with a rejected one.
func pauseManagedSessionUpdate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID string) (pgx.Tx, int32) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE managed_session_update_gate (
		session_id text PRIMARY KEY,
		lock_class integer NOT NULL,
		lock_key integer NOT NULL,
		update_count integer NOT NULL DEFAULT 0
	);
	CREATE FUNCTION pause_managed_session_update() RETURNS trigger LANGUAGE plpgsql AS $function$
	DECLARE
		gate managed_session_update_gate%ROWTYPE;
	BEGIN
		UPDATE managed_session_update_gate SET update_count = update_count + 1
			WHERE session_id = NEW.id RETURNING * INTO gate;
		IF FOUND THEN
			PERFORM pg_advisory_xact_lock(gate.lock_class, gate.lock_key);
		END IF;
		RETURN NEW;
	END;
	$function$;
	CREATE TRIGGER managed_session_update_barrier AFTER UPDATE OF revoked_at ON sessions
		FOR EACH ROW WHEN (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
		EXECUTE FUNCTION pause_managed_session_update()`); err != nil {
		t.Fatalf("install managed session update barrier: %v", err)
	}
	blocker, pid := managedSessionBlocker(t, ctx, pool)
	const lockClass int32 = 1196446297
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, $2::integer)", lockClass, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO managed_session_update_gate (session_id, lock_class, lock_key)
		VALUES ($1, $2, $3)`, sessionID, lockClass, pid); err != nil {
		t.Fatal(err)
	}
	return blocker, pid
}

func assertManagedSessionUpdateCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT update_count FROM managed_session_update_gate").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("committed target update side effects = %d, want %d", count, want)
	}
}

func waitManagedSessionDatabaseExpiry(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID string, done <-chan struct{}) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		if err := pool.QueryRow(ctx, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", sessionID).Scan(&expired); err != nil {
			t.Fatalf("observe actor expiration using database time: %v", err)
		}
		if expired {
			return
		}
		select {
		case <-done:
			t.Fatal("revocation completed before the update barrier was released")
		case <-ctx.Done():
			t.Fatal("actor did not expire before the database observation deadline")
		case <-ticker.C:
		}
	}
}

func TestStoreManagedSessionReciprocalRevocationsSerialize(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	first := bootstrapTestAdmin(t, testCtx, store)
	second, err := store.CreateUser(testCtx, "Reciprocal Administrator", "second-password", true)
	if err != nil {
		t.Fatal(err)
	}
	firstCredentials, firstActor := managedLogin(t, testCtx, store, first, "administrator-password", "admin")
	secondCredentials, secondActor := managedLogin(t, testCtx, store, second, "second-password", "admin")
	managedLogin(t, testCtx, store, first, "administrator-password", "emby")
	managedLogin(t, testCtx, store, second, "second-password", "admin")
	managedLogin(t, testCtx, store, second, "second-password", "emby")
	firstBefore := managedSnapshot(t, testCtx, pool, first.ID)
	secondBefore := managedSessionStableSnapshot(t, testCtx, pool, second.ID, secondActor.SessionID)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", firstActor.SessionID); err != nil {
		t.Fatal(err)
	}
	firstResults, firstDone := revokeManagedSessionAsync(ctx, store, firstActor, secondActor.SessionID)
	firstPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id, user_id, kind, revoked_at FROM sessions", firstDone)
	secondResults, secondDone := revokeManagedSessionAsync(ctx, store, secondActor, firstActor.SessionID)
	// The first mutation retains its locks while the reciprocal mutation queues.
	// Releasing one barrier must produce one success and one authorization error,
	// without a deadlock or a write from the administrator who has lost access.
	waitManagedBlockedQuery(t, ctx, pool, firstPID, "pg_advisory_xact_lock", secondDone)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	winner := awaitManagedSessionRevocation(t, ctx, firstResults)
	loser := awaitManagedSessionRevocation(t, ctx, secondResults)
	if winner.err != nil || winner.revocation.SessionID != secondActor.SessionID || winner.revocation.UserID != second.ID ||
		winner.revocation.Kind != "admin" || winner.revocation.CurrentSessionRevoked || winner.revocation.RevokedAt.IsZero() {
		t.Fatalf("authorized reciprocal revocation failed: %v", winner.err)
	}
	if !errors.Is(loser.err, identity.ErrUnauthorized) || loser.revocation != (identity.ManagedSessionRevocation{}) {
		t.Errorf("revoked reciprocal actor returned a result or unexpected error: %v", loser.err)
	}
	assertManagedUnchanged(t, testCtx, pool, first.ID, firstBefore)
	if after := managedSessionStableSnapshot(t, testCtx, pool, second.ID, secondActor.SessionID); after != secondBefore {
		t.Error("reciprocal revocation changed account revisions or unrelated session metadata")
	}
	var revokedAt time.Time
	if err := pool.QueryRow(testCtx, "SELECT revoked_at FROM sessions WHERE id = $1", secondActor.SessionID).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if !revokedAt.Equal(winner.revocation.RevokedAt) {
		t.Error("reciprocal revocation did not return its committed timestamp")
	}
	assertManagedTokenRevoked(t, testCtx, store, secondCredentials, "admin")
	if _, err := store.Resolve(testCtx, firstCredentials.Token, "admin"); err != nil {
		t.Errorf("winning reciprocal administrator lost access: %v", err)
	}
}

func TestStoreManagedSessionExpiryAfterTargetUpdateRollsBack(t *testing.T) {
	for _, targetKind := range []string{"other", "self"} {
		t.Run(targetKind, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			managedLogin(t, testCtx, store, admin, "administrator-password", "emby")
			viewer, err := store.CreateUser(testCtx, "Expiration Barrier Target", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			targetCredentials, _ := managedLogin(t, testCtx, store, viewer, "viewer-password", "emby")
			managedLogin(t, testCtx, store, viewer, "viewer-password", "emby")
			targetID := targetCredentials.SessionID
			if targetKind == "self" {
				targetID = actor.SessionID
			}
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, targetID)
			// Preserve the persisted lifetime constraint while keeping the trusted
			// principal's original, longer expiration snapshot deliberately stale.
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			adminBefore := managedSnapshot(t, ctx, pool, admin.ID)
			viewerBefore := managedSnapshot(t, ctx, pool, viewer.ID)
			results, done := revokeManagedSessionAsync(ctx, store, actor, targetID)
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", done)
			// Reaching AFTER UPDATE proves the initial authorization succeeded.
			// Only database time establishes that it is now too late to commit.
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := awaitManagedSessionRevocation(t, ctx, results)
			if !errors.Is(result.err, identity.ErrUnauthorized) || result.revocation != (identity.ManagedSessionRevocation{}) {
				t.Errorf("actor expired after target update returned a result or unexpected error: %v", result.err)
			}
			assertManagedUnchanged(t, testCtx, pool, admin.ID, adminBefore)
			assertManagedUnchanged(t, testCtx, pool, viewer.ID, viewerBefore)
			assertManagedSessionUpdateCount(t, testCtx, pool, 0)
		})
	}
}

func TestStoreManagedSessionSelfRevocationCommitsAfterFinalAuthorization(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	credentials, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	sibling, _ := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	managedLogin(t, testCtx, store, admin, "administrator-password", "emby")
	before := managedSessionStableSnapshot(t, testCtx, pool, admin.ID, actor.SessionID)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, actor.SessionID)
	results, done := revokeManagedSessionAsync(ctx, store, actor, actor.SessionID)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitManagedSessionRevocation(t, ctx, results)
	if result.err != nil || !result.revocation.CurrentSessionRevoked || result.revocation.SessionID != actor.SessionID ||
		result.revocation.UserID != admin.ID || result.revocation.Kind != "admin" || result.revocation.RevokedAt.IsZero() {
		t.Fatalf("self-revocation failed its final authorization check: %v", result.err)
	}
	var revokedAt time.Time
	if err := pool.QueryRow(testCtx, "SELECT revoked_at FROM sessions WHERE id = $1", actor.SessionID).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if !revokedAt.Equal(result.revocation.RevokedAt) {
		t.Error("self-revocation did not preserve its exact committed timestamp")
	}
	if after := managedSessionStableSnapshot(t, testCtx, pool, admin.ID, actor.SessionID); after != before {
		t.Error("self-revocation changed account revisions or sibling authentication sessions")
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 1)
	committed := managedSnapshot(t, testCtx, pool, admin.ID)
	for _, targetID := range []string{actor.SessionID, sibling.SessionID} {
		repeated, err := store.RevokeManagedSession(ctx, actor, targetID)
		if !errors.Is(err, identity.ErrUnauthorized) || repeated != (identity.ManagedSessionRevocation{}) {
			t.Errorf("self-revoked actor reused the authorization exception: %v", err)
		}
		assertManagedUnchanged(t, testCtx, pool, admin.ID, committed)
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 1)
	assertManagedTokenRevoked(t, testCtx, store, credentials, "admin")
	if _, err := store.Resolve(testCtx, sibling.Token, "admin"); err != nil {
		t.Errorf("self-revocation invalidated a sibling login: %v", err)
	}
}
