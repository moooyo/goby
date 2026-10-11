package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

func TestStoreCreateManagedUserAcceptsSharedAuthorityReaders(t *testing.T) {
	for _, kind := range []string{"admin", "emby"} {
		t.Run(kind, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", kind)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			reader, _ := managedSessionBlocker(t, ctx, pool)
			var lockedID string
			if err := reader.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID).Scan(&lockedID); err != nil {
				t.Fatal(err)
			}
			if err := reader.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", actor.SessionID).Scan(&lockedID); err != nil {
				t.Fatal(err)
			}
			before := managedSnapshot(t, ctx, pool, actor.User.ID)
			actor.User.IsAdministrator = false
			created, err := store.CreateManagedUser(ctx, actor, "Shared Authority Created", "created-password", false)
			if err != nil {
				t.Fatalf("create while authorization readers retain shared actor locks: %v", err)
			}
			assertManagedUnchanged(t, ctx, pool, actor.User.ID, before)
			if identityActivityCount(t, ctx, pool, activity.ActionUserCreated, created.ID) != 1 {
				t.Fatal("successful shared-lock creation did not record exactly one activity")
			}
			_, err = store.CreateManagedUser(ctx, actor, " SHARED AUTHORITY CREATED ", "duplicate-password", false)
			assertManagedFieldError(t, err, "Name")
			if identityActivityCount(t, ctx, pool, activity.ActionUserCreated, created.ID) != 1 {
				t.Fatal("duplicate creation changed the original account activity")
			}
		})
	}
}

func TestStoreCreateManagedUserRechecksEmbyAfterManagementWait(t *testing.T) {
	for _, change := range []string{"demoted", "deleted", "revoked", "expired"} {
		t.Run(change, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			admin := bootstrapTestAdmin(t, ctx, store)
			if _, err := store.CreateUser(ctx, "Creation Survivor", "survivor-password", true); err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
				t.Fatal(err)
			}
			done, result := make(chan struct{}), make(chan error, 1)
			go func() {
				defer close(done)
				_, err := store.CreateManagedUser(ctx, actor, "Queued Creation", "created-password", false)
				result <- err
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "pg_advisory_xact_lock", done)
			statement, id := "UPDATE users SET is_administrator = false WHERE id = $1", actor.User.ID
			switch change {
			case "deleted":
				statement = "DELETE FROM users WHERE id = $1"
			case "revoked":
				statement, id = "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID
			case "expired":
				statement, id = `UPDATE sessions SET created_at = clock_timestamp() - interval '2 days',
					expires_at = clock_timestamp() - interval '1 day' WHERE id = $1`, actor.SessionID
			}
			if _, err := pool.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			before := identityAuditSnapshot(t, ctx, pool)
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("creation with authority invalidated during its management wait: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("queued creation did not finish after the management lock was released")
			}
			if identityAuditSnapshot(t, ctx, pool) != before {
				t.Fatal("rejected queued creation changed identity or activity state")
			}
		})
	}
}

func TestStoreManagedEmbyAuthorityUsesStoredCredentialAndPolicy(t *testing.T) {
	for _, change := range []string{"wrong-kind", "missing-session", "revoked", "expired", "device", "peer", "malformed-policy", "application-marker"} {
		t.Run(change, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			target, err := store.CreateUser(ctx, "Emby Authority Target", "target-password", false)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
			actor.Client.DeviceID = "allowed-device"
			actor.PeerIP = "192.0.2.42"
			statement, id := "", actor.SessionID
			switch change {
			case "wrong-kind":
				statement = "UPDATE sessions SET kind = 'admin' WHERE id = $1"
			case "missing-session":
				statement = "DELETE FROM sessions WHERE id = $1"
			case "revoked":
				statement = "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1"
			case "expired":
				statement = `UPDATE sessions SET created_at = clock_timestamp() - interval '2 days',
					expires_at = clock_timestamp() - interval '1 day' WHERE id = $1`
			case "device":
				statement, id = `UPDATE users SET policy = '{"EnableAllDevices":false,"EnabledDevices":["allowed-device"]}'::jsonb WHERE id = $1`, actor.User.ID
			case "peer":
				statement, id = `UPDATE users SET policy = '{"EnableRemoteAccess":false}'::jsonb WHERE id = $1`, actor.User.ID
			case "malformed-policy":
				statement, id = `UPDATE users SET policy = '{"EnableRemoteAccess":"invalid"}'::jsonb WHERE id = $1`, actor.User.ID
			case "application-marker":
				actor.ApplicationKeyID = 1
			}
			if statement != "" {
				if _, err := pool.Exec(ctx, statement, id); err != nil {
					t.Fatal(err)
				}
			}
			current := readManagedUser(t, ctx, store, target.ID)
			before := identityAuditSnapshot(t, ctx, pool)
			if _, err := store.CreateManagedUser(ctx, actor, "Forbidden Emby Creation", "created-password", false); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("create with rejected Emby authority: %v", err)
			}
			if _, err := store.UpdateManagedUser(ctx, actor, target.ID, managedUpdate(current)); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("update with rejected Emby authority: %v", err)
			}
			if _, err := store.DeleteManagedUser(ctx, actor, target.ID, current.Revision); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("delete with rejected Emby authority: %v", err)
			}
			if identityAuditSnapshot(t, ctx, pool) != before {
				t.Fatal("rejected Emby authority changed identity or activity state")
			}
		})
	}
}

func TestStoreManagedCreationAndEmbyDeletionRecheckClockAfterAudit(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   string
		remove bool
		self   bool
	}{
		{name: "native-create", kind: "admin"},
		{name: "emby-create", kind: "emby"},
		{name: "emby-delete-other", kind: "emby", remove: true},
		{name: "emby-delete-self", kind: "emby", remove: true, self: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			admin := bootstrapTestAdmin(t, ctx, store)
			other, err := store.CreateUser(ctx, "Clock Check Survivor", "survivor-password", true)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", test.kind)
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			const gateClass int32 = 1196446327
			if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, $2::integer)", gateClass, blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `CREATE TABLE managed_authority_clock_gate (lock_class integer, lock_key integer);
				CREATE FUNCTION pause_managed_authority_audit() RETURNS trigger LANGUAGE plpgsql AS $$
				DECLARE gate managed_authority_clock_gate%ROWTYPE;
				BEGIN SELECT * INTO gate FROM managed_authority_clock_gate;
				PERFORM pg_advisory_xact_lock(gate.lock_class, gate.lock_key); RETURN NEW; END; $$;
				CREATE TRIGGER managed_authority_audit_barrier AFTER INSERT ON activity_entries
				FOR EACH ROW WHEN (NEW.action IN ('user.created', 'user.deleted')) EXECUTE FUNCTION pause_managed_authority_audit()`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO managed_authority_clock_gate VALUES ($1, $2)", gateClass, blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			before := identityAuditSnapshot(t, ctx, pool)
			done, result := make(chan struct{}), make(chan error, 1)
			go func() {
				defer close(done)
				var err error
				if test.remove {
					target := other.ID
					if test.self {
						target = actor.User.ID
					}
					_, err = store.DeleteManagedUser(ctx, actor, target, 1)
				} else {
					_, err = store.CreateManagedUser(ctx, actor, "Expired Audit Creation", "created-password", false)
				}
				result <- err
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("managed mutation retained expired authority after its audit wait: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("managed mutation did not finish after its audit gate was released")
			}
			if identityAuditSnapshot(t, ctx, pool) != before {
				t.Fatal("final authority expiry did not roll back identity and activity changes")
			}
		})
	}
}
