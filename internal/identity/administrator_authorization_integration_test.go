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

// Only QueryRow is exposed to the identity helper. Production owner-transaction
// adapters can preserve their own context and row wrappers in the same way.
type administratorQueryAdapter struct {
	tx pgx.Tx
}

func (q administratorQueryAdapter) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	return q.tx.QueryRow(ctx, statement, args...)
}

var _ identity.AuthorizationTx = administratorQueryAdapter{}

func administratorTestTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	})
	return tx
}

func TestCheckAdministratorRealTransactionsRespectCredentialAudiences(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, native, "Administrator Authorization")
	application := applicationKeyPrincipal(t, ctx, store, key)
	viewer, err := store.CreateUser(ctx, "Authorization Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerLogin := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	wrongNativeOwner, wrongEmbyOwner := native, emby
	wrongNativeOwner.User.ID, wrongEmbyOwner.User.ID = viewer.ID, viewer.ID
	for _, test := range []struct {
		name     string
		actor    identity.Principal
		audience identity.AdministratorAudience
		want     error
	}{
		{"native administrator", native, identity.AdministratorNative, nil},
		{"Emby administrator", emby, identity.AdministratorEmby, nil},
		{"application key", application, identity.AdministratorEmby, nil},
		{"native credential at Emby boundary", native, identity.AdministratorEmby, identity.ErrUnauthorized},
		{"Emby credential at native boundary", emby, identity.AdministratorNative, identity.ErrUnauthorized},
		{"application key at native boundary", application, identity.AdministratorNative, identity.ErrUnauthorized},
		{"ordinary viewer", viewerLogin, identity.AdministratorEmby, identity.ErrClientSessionForbidden},
		{"native credential with another account", wrongNativeOwner, identity.AdministratorNative, identity.ErrUnauthorized},
		{"Emby credential with another account", wrongEmbyOwner, identity.AdministratorEmby, identity.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, lock := range []bool{false, true} {
				tx := administratorTestTransaction(t, ctx, pool)
				err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, test.actor, test.audience, lock)
				if !errors.Is(err, test.want) {
					t.Fatalf("administrator audience returned %v, want %v", err, test.want)
				}
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	// A stale display snapshot cannot remove or invent persisted administrator
	// authority. Current database account state remains the deciding value.
	emby.User.IsAdministrator, emby.User.IsDisabled = false, true
	emby.ExpiresAt = time.Unix(1, 0)
	tx := administratorTestTransaction(t, ctx, pool)
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, emby, identity.AdministratorEmby, true); err != nil {
		t.Fatalf("administrator authority depended on stale principal fields: %v", err)
	}
}

func TestCheckAdministratorUsesCurrentAccountAndCredentialState(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	for _, test := range []struct {
		name      string
		actor     identity.Principal
		audience  identity.AdministratorAudience
		statement string
		id        string
		want      error
	}{
		{"native revoked", native, identity.AdministratorNative, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", native.SessionID, identity.ErrUnauthorized},
		{"Emby revoked", emby, identity.AdministratorEmby, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", emby.SessionID, identity.ErrUnauthorized},
		{"native disabled", native, identity.AdministratorNative, "UPDATE users SET is_disabled = true WHERE id = $1", native.User.ID, identity.ErrUnauthorized},
		{"Emby disabled", emby, identity.AdministratorEmby, "UPDATE users SET is_disabled = true WHERE id = $1", emby.User.ID, identity.ErrUnauthorized},
		{"native demoted", native, identity.AdministratorNative, "UPDATE users SET is_administrator = false WHERE id = $1", native.User.ID, identity.ErrUnauthorized},
		{"Emby demoted", emby, identity.AdministratorEmby, "UPDATE users SET is_administrator = false WHERE id = $1", emby.User.ID, identity.ErrClientSessionForbidden},
		{"native expired", native, identity.AdministratorNative, "UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1", native.SessionID, identity.ErrUnauthorized},
		{"Emby expired", emby, identity.AdministratorEmby, "UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1", emby.SessionID, identity.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := administratorTestTransaction(t, ctx, pool)
			if _, err := tx.Exec(ctx, test.statement, test.id); err != nil {
				t.Fatal(err)
			}
			for _, lock := range []bool{false, true} {
				if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, test.actor, test.audience, lock); !errors.Is(err, test.want) {
					t.Fatalf("changed administrator state returned %v, want %v", err, test.want)
				}
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckAdministratorRequiresOwnedApplicationClientAndHasNoSelfRevocationException(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	first := issueApplicationKey(t, ctx, store, native, "Authorization Alpha")
	second := issueApplicationKey(t, ctx, store, native, "Authorization Beta")
	alpha := applicationKeyPrincipal(t, ctx, store, first)
	beta := applicationKeyPrincipal(t, ctx, store, second)
	for _, field := range []string{"client", "parent", "key"} {
		actor := alpha
		switch field {
		case "client":
			actor.ClientSessionID = beta.ClientSessionID
		case "parent":
			actor.SessionID = beta.SessionID
		case "key":
			actor.ApplicationKeyID = beta.ApplicationKeyID
		}
		tx := administratorTestTransaction(t, ctx, pool)
		if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, true); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("application authority accepted a foreign %s: %v", field, err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutation := range []struct{ statement, id string }{
		{"DELETE FROM application_key_clients WHERE id = $1", alpha.ClientSessionID},
		{"DELETE FROM application_keys WHERE credential_id = $1", alpha.SessionID},
		{"UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", alpha.SessionID},
	} {
		tx := administratorTestTransaction(t, ctx, pool)
		if _, err := tx.Exec(ctx, mutation.statement, mutation.id); err != nil {
			t.Fatal(err)
		}
		if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, alpha, identity.AdministratorEmby, true); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalidated application identity retained authority: %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tx := administratorTestTransaction(t, ctx, pool)
	if _, err := tx.Exec(ctx, "UPDATE users SET is_administrator = false, is_disabled = true WHERE id = $1", native.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, alpha, identity.AdministratorEmby, true); err != nil {
		t.Fatalf("application authority depended on its creator account: %v", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", alpha.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, alpha, identity.AdministratorEmby, false); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("final administrator check accepted self-revocation: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	applicationKeyPrincipal(t, ctx, store, first)
}

func TestCheckAdministratorRechecksRevocationAfterActorLockWait(t *testing.T) {
	for _, actorKind := range []string{"emby", "application_key"} {
		t.Run(actorKind, func(t *testing.T) {
			testCtx, pool, store, native, _ := applicationKeyTestStore(t)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			_, actor := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
			if actorKind == "application_key" {
				actor = applicationKeyPrincipal(t, ctx, store, issueApplicationKey(t, ctx, store, native, "Waiting Authorization"))
			}
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			statement, id := "SELECT id FROM users WHERE id = $1 FOR UPDATE", actor.User.ID
			queryFragment := "SELECT id FROM users WHERE id = $1 FOR SHARE"
			if actorKind == "application_key" {
				statement, id = "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", actor.SessionID
				queryFragment = "SELECT id FROM sessions WHERE id = $1"
			}
			if _, err := blocker.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			tx := administratorTestTransaction(t, ctx, pool)
			results, done := make(chan error, 1), make(chan struct{})
			go func() {
				defer close(done)
				results <- identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, true)
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, queryFragment, done)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-results:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("administrator lock wait retained revoked authority: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("administrator lock wait did not complete")
			}
		})
	}
}

func TestCheckAdministratorLocksCredentialsUntilTheCallingTransactionEnds(t *testing.T) {
	testCtx, pool, store, native, _ := applicationKeyTestStore(t)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	credentials, actor := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	tx := administratorTestTransaction(t, ctx, pool)
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, true); err != nil {
		t.Fatal(err)
	}
	var transactionPID int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&transactionPID); err != nil {
		t.Fatal(err)
	}
	results, done := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(done)
		results <- store.Revoke(ctx, credentials.Token)
	}()
	waitManagedBlockedQuery(t, ctx, pool, transactionPID, "UPDATE sessions SET revoked_at", done)
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, false); err != nil {
		t.Fatalf("pending revocation changed the locked transaction's authority: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatalf("revocation did not finish after transaction commit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("revocation remained blocked after the administrator transaction ended")
	}
	tx = administratorTestTransaction(t, ctx, pool)
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, false); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("a new transaction retained revoked administrator authority: %v", err)
	}
}

func TestCheckAdministratorFinalClockCheckRollsBackBusinessWrites(t *testing.T) {
	testCtx, pool, store, native, _ := applicationKeyTestStore(t)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	_, actor := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	if _, err := pool.Exec(ctx, `CREATE TABLE administrator_authorization_effects (id integer PRIMARY KEY, value integer NOT NULL);
		INSERT INTO administrator_authorization_effects (id, value) VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	tx := administratorTestTransaction(t, ctx, pool)
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE administrator_authorization_effects SET value = 1 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, make(chan struct{}))
	if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, actor, identity.AdministratorEmby, false); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("final administrator check used the transaction's old clock: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var value int
	if err := pool.QueryRow(ctx, "SELECT value FROM administrator_authorization_effects WHERE id = 1").Scan(&value); err != nil || value != 0 {
		t.Fatalf("expired administrator left business writes committed: value=%d error=%v", value, err)
	}
}
