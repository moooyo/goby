package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func waitForManagedAccountLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerPID uint32) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(deadline, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock'
		)`, ownerPID).Scan(&blocked); err != nil {
			t.Fatalf("observe account lock wait: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.Done():
			t.Fatal("client operation did not wait for the locked account")
		}
	}
}

func TestClientSessionOperationsLockAccountBeforeAuthentication(t *testing.T) {
	for _, mutation := range []bool{false, true} {
		name := "list"
		if mutation {
			name = "touch"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			user := bootstrapTestAdmin(t, ctx, store)
			credentials, principal := clientSessionPrincipal(t, ctx, store, user,
				"administrator-password", identity.Client{Name: "Lock order player"}, "emby")
			owner, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Rollback(context.Background())
			var id string
			if err := owner.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", user.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				if mutation {
					finished <- store.TouchClientSession(operationCtx, principal)
				} else {
					_, err := store.ListClientSessions(operationCtx, principal, identity.ClientSessionFilter{})
					finished <- err
				}
			}()
			waitForManagedAccountLock(t, ctx, pool, owner.Conn().PgConn().PID())
			// A session-first caller would retain this row while waiting for the
			// account, making an administrator's next lock form a deadlock cycle.
			if err := owner.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE NOWAIT", credentials.SessionID).Scan(&id); err != nil {
				t.Fatalf("waiting client retained authentication before its account: %v", err)
			}
			if _, err := owner.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", credentials.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := owner.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("client operation after committed revocation = %v, want unauthorized", err)
				}
			case <-operationCtx.Done():
				t.Fatal("client operation did not finish after releasing its account")
			}
		})
	}
}
