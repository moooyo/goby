//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

type fileMutationAuthorityObservedTx struct {
	pgx.Tx
	statements []string
	failAt     int
}

func (tx *fileMutationAuthorityObservedTx) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	tx.statements = append(tx.statements, statement)
	if len(tx.statements) == tx.failAt {
		return tx.Tx.QueryRow(ctx, "SELECT 1/0")
	}
	return tx.Tx.QueryRow(ctx, statement, args...)
}

func TestFileMutationActorSessionQueriesPreserveAuthority(t *testing.T) {
	ctx, pool, _, _, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := itemCapabilityTestActor(t, ctx, pool, userID)
	libraryIntegrationUser(t, ctx, pool, "mutation-authority-other", false, true, nil)
	for _, lock := range []bool{false, true} {
		for _, test := range []struct {
			name        string
			userSQL     string
			sessionSQL  string
			kind        string
			peer        string
			forbidden   bool
			queries     int
			lockQueries int
		}{
			{name: "live Emby", queries: 2, lockQueries: 3},
			{name: "wrong user", sessionSQL: `UPDATE sessions SET user_id='mutation-authority-other' WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 2},
			{name: "wrong kind", sessionSQL: `UPDATE sessions SET kind='admin' WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 2},
			{name: "missing credential", sessionSQL: `DELETE FROM sessions WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 2},
			{name: "revoked", sessionSQL: `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 3},
			{name: "expired", sessionSQL: `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 3},
			{name: "disabled user", userSQL: `UPDATE users SET is_disabled=true WHERE id=$1`, forbidden: true, queries: 1, lockQueries: 1},
			{name: "native role required", kind: "admin", sessionSQL: `UPDATE sessions SET kind='admin' WHERE id=$1`, forbidden: true, queries: 1, lockQueries: 1},
			{name: "locked out", userSQL: `UPDATE users SET policy=policy||'{"LockedOutDate":1}'::jsonb WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 3},
			{name: "device denied", userSQL: `UPDATE users SET policy=policy||'{"EnableAllDevices":false,"EnabledDevices":["other"]}'::jsonb WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 3},
			{name: "stored device allowed", userSQL: `UPDATE users SET policy=policy||'{"EnableAllDevices":false,"EnabledDevices":["capability-device"]}'::jsonb WHERE id=$1`, queries: 2, lockQueries: 3},
			{name: "remote denied", peer: "203.0.113.10", userSQL: `UPDATE users SET policy=policy||'{"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, forbidden: true, queries: 2, lockQueries: 3},
			{name: "native policy distinction", kind: "admin", peer: "203.0.113.10", sessionSQL: `UPDATE sessions SET kind='admin' WHERE id=$1`, userSQL: `UPDATE users SET is_administrator=true,policy=policy||'{"EnableAllDevices":false,"EnabledDevices":[],"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, queries: 2, lockQueries: 3},
		} {
			t.Run(fmt.Sprintf("%s/lock=%t", test.name, lock), func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(tx)
				if test.userSQL != "" {
					if _, err := tx.Exec(ctx, test.userSQL, userID); err != nil {
						t.Fatal(err)
					}
				}
				if test.sessionSQL != "" {
					if _, err := tx.Exec(ctx, test.sessionSQL, actor.SessionID); err != nil {
						t.Fatal(err)
					}
				}
				principal := actor
				principal.Client.DeviceID = "untrusted-client-device"
				if test.kind != "" {
					principal.Kind = test.kind
				}
				if test.peer != "" {
					principal.PeerIP = test.peer
				}
				observed := &fileMutationAuthorityObservedTx{Tx: tx}
				access, err := checkFileMutationActor(ctx, observed, principal, lock)
				if test.forbidden {
					if !errors.Is(err, ErrForbidden) {
						t.Fatalf("invalid authority was accepted: %v", err)
					}
				} else if err != nil || access.userID != userID || access.administrator != (principal.Kind == "admin") {
					t.Fatalf("valid authority changed: user=%q administrator=%t error=%v", access.userID, access.administrator, err)
				}
				queries := test.queries
				if lock {
					queries = test.lockQueries
				}
				if len(observed.statements) != queries {
					t.Fatalf("authority executed %d queries, want %d: %v", len(observed.statements), queries, observed.statements)
				}
				if strings.Contains(observed.statements[0], "FOR SHARE") != lock {
					t.Fatal("the account lock did not match the requested phase")
				}
				for index, statement := range observed.statements[1:] {
					credentialLock := strings.HasPrefix(statement, "SELECT id FROM sessions")
					if credentialLock {
						if !lock || index != 0 || !strings.HasSuffix(statement, "FOR SHARE") {
							t.Fatal("the credential existence query ran outside its locking phase")
						}
					} else if !strings.Contains(statement, "expires_at>clock_timestamp()") || strings.Contains(statement, "FOR SHARE") {
						t.Fatal("credential liveness lost its separate fresh-clock observation")
					}
				}
			})
		}
	}
}

func TestFileMutationActorRetainsDatabaseErrorPrecedence(t *testing.T) {
	ctx, pool, _, _, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := itemCapabilityTestActor(t, ctx, pool, userID)
	for _, lock := range []bool{false, true} {
		queries := 2
		if lock {
			queries = 3
		}
		for failAt := 1; failAt <= queries; failAt++ {
			t.Run(fmt.Sprintf("lock=%t/query=%d", lock, failAt), func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(tx)
				if _, err := tx.Exec(ctx, `UPDATE users SET policy='{"EnableAllDevices":"invalid"}'::jsonb WHERE id=$1`, userID); err != nil {
					t.Fatal(err)
				}
				observed := &fileMutationAuthorityObservedTx{Tx: tx, failAt: failAt}
				_, err = checkFileMutationActor(ctx, observed, actor, lock)
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "22012" || errors.Is(err, ErrForbidden) || len(observed.statements) != failAt {
					t.Fatalf("database failure was masked or queries continued: queries=%d error=%v", len(observed.statements), err)
				}
			})
		}
	}
}

func TestFileMutationActorRechecksAfterCredentialLockWait(t *testing.T) {
	ctx, pool, _, _, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	for _, change := range []string{"revoked", "expired"} {
		t.Run(change, func(t *testing.T) {
			actor := itemCapabilityTestActor(t, ctx, pool, userID)
			if change == "expired" {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, actor.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(blocker)
			var blockerPID int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			checking, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var checkingPID int32
			var live bool
			if err := checking.QueryRow(ctx, `SELECT pg_backend_pid(),expires_at>clock_timestamp() FROM sessions WHERE id=$1`, actor.SessionID).Scan(&checkingPID, &live); err != nil || !live {
				rollback(checking)
				t.Fatalf("credential was not live before the lock wait: %v", err)
			}
			observed := &fileMutationAuthorityObservedTx{Tx: checking}
			finished := make(chan error, 1)
			go func() {
				defer rollback(checking)
				_, err := checkFileMutationActor(ctx, observed, actor, true)
				finished <- err
			}()
			ownedTransactionsWaitForBlock(t, ctx, pool, checkingPID, blockerPID, finished)
			if change == "revoked" {
				_, err = blocker.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID)
			} else {
				_, err = blocker.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))+0.02) FROM sessions WHERE id=$1`, actor.SessionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, ErrForbidden) || len(observed.statements) != 3 || !strings.Contains(observed.statements[2], "clock_timestamp()") {
					t.Fatalf("authority survived credential %s during lock wait: queries=%v error=%v", change, observed.statements, err)
				}
			case <-ctx.Done():
				t.Fatal("credential authorization did not finish after the lock was released")
			}
		})
	}
}

func TestFileMutationActorPreservesIndependentApplicationKeyChecks(t *testing.T) {
	ctx, pool, _, _, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := catalogAuditApplicationActor(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []bool{false, true} {
		for _, revoked := range []bool{false, true} {
			t.Run(fmt.Sprintf("lock=%t/revoked=%t", lock, revoked), func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(tx)
				if revoked {
					if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
						t.Fatal(err)
					}
				}
				access, err := checkFileMutationActor(ctx, tx, actor, lock)
				if revoked {
					if !errors.Is(err, ErrForbidden) || !errors.Is(err, identity.ErrUnauthorized) {
						t.Fatalf("revoked application credential retained authority: %v", err)
					}
				} else if err != nil || !access.all || !access.policy.EnableContentDeletion || !access.policy.EnableSubtitleManagement {
					t.Fatalf("independent application authority changed: %v", err)
				}
			})
		}
	}
}
