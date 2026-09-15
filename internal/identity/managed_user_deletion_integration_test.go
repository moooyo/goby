package identity_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

// Snapshot values remain private to the test, including credentials and keys.
func managedDeletionSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
		'sessions', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM sessions t),
		'plays', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM play_sessions t),
		'userdata', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id,item_id) FROM user_item_data t),
		'preferences', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM user_settings t),
		'jobs', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM encoding_jobs t),
		'references', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id,auth_session_id,device_id,client_nonce) FROM client_playback_references t),
		'keys', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM application_keys t),
		'devices', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM devices t),
		'activity', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM activity_entries t))::text`).Scan(&result); err != nil {
		t.Fatal("snapshot managed deletion state")
	}
	return result
}

func TestStoreDeleteManagedUserCascadesOwnedStateAndRetainsServerResources(t *testing.T) {
	ctx, pool, store, actor, _ := applicationKeyTestStore(t)
	target, err := store.CreateManagedUser(ctx, actor, "Delete Target", "target-password", true)
	if err != nil {
		t.Fatal(err)
	}
	admin, targetActor := managedLogin(t, ctx, store, target, "target-password", "admin")
	emby, err := store.Authenticate(ctx, target.Name, "target-password", identity.Client{DeviceID: "retained-device", Device: "Shared Device"}, "emby")
	if err != nil {
		t.Fatal(err)
	}
	key := issueApplicationKey(t, ctx, store, targetActor, "Retained Automation")
	path := filepath.Join(t.TempDir(), "retained-media.bin")
	if err := os.WriteFile(path, []byte("retained media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id,name,collection_type) VALUES ('delete-library','Retained Library','movies');
		INSERT INTO items (id,library_id,name,sort_name,type) VALUES ('delete-item','delete-library','Retained Movie','Retained Movie','Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET path=$1 WHERE id='delete-item'", path); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE item_metadata_state SET last_edited_by=$1 WHERE item_id='delete-item'",
		"INSERT INTO user_settings (user_id) VALUES ($1)",
		"INSERT INTO user_item_data (user_id,item_id,play_count) VALUES ($1,'delete-item',2)",
	} {
		if _, err := pool.Exec(ctx, statement, target.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
		(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
		VALUES ('delete-play',$1,$2,'retained-device','delete-item','delete-source','Playing',100000000,clock_timestamp()+interval '1 hour')`, target.ID, emby.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO encoding_jobs
		(id,user_id,auth_session_id,device_id,play_session_id,item_id,media_source_id,source_stamp,plan,state,created_at,updated_at,last_access_at)
		VALUES ('dddddddddddddddddddddddddddddddd',$1,$2,'retained-device','delete-play','delete-item','delete-source','source-stamp','{}','running',now(),now(),now())`, target.ID, emby.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO client_playback_references (user_id,auth_session_id,device_id,client_nonce,play_session_id)
		VALUES ($1,$2,'retained-device','delete-reference','delete-play')`, target.ID, emby.SessionID); err != nil {
		t.Fatal(err)
	}
	current := readManagedUser(t, ctx, store, target.ID)
	result, err := store.DeleteManagedUser(ctx, actor, target.ID, current.Revision)
	if err != nil || result.CurrentSessionRevoked || !slices.Equal(result.RevokedSessionIDs, sortedDeletionSessions(admin.SessionID, emby.SessionID)) {
		t.Fatalf("delete account result did not contain exactly its committed credential cleanup: %v", err)
	}
	for _, table := range []string{"sessions", "user_settings", "user_item_data", "play_sessions", "encoding_jobs", "client_playback_references"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", target.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("target state remained in %s: %v", table, err)
		}
	}
	if _, err := store.GetManagedUser(ctx, target.ID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("deleted account remained readable: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, admin, "admin")
	assertManagedTokenRevoked(t, ctx, store, emby, "emby")
	applicationKeyPrincipal(t, ctx, store, key)
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM application_keys WHERE id=$1 AND created_by IS NULL)
		AND EXISTS (SELECT 1 FROM devices WHERE reported_device_id='retained-device' AND last_user_id IS NULL AND deleted_at IS NULL)
		AND EXISTS (SELECT 1 FROM item_metadata_state WHERE item_id='delete-item' AND last_edited_by IS NULL)
		AND EXISTS (SELECT 1 FROM items WHERE id='delete-item')
		AND EXISTS (SELECT 1 FROM libraries WHERE id='delete-library')`, key.ID).Scan(&retained); err != nil || !retained {
		t.Fatalf("deletion removed a server-owned resource instead of clearing its user reference: %v", err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "retained media" {
		t.Fatal("account deletion changed the media file")
	}
	event := readIdentityActivity(t, ctx, pool, activity.ActionUserDeleted, target.ID)
	if event.Actor.ID != actor.User.ID || event.Actor.CredentialID != actor.SessionID || event.Resource.Kind != activity.ResourceUser ||
		event.Source != activity.SourceNative || event.Revision != current.Revision || event.Count != 1 || len(event.ChangedFields) != 0 {
		t.Fatal("deletion audit lost its real actor, target, revision or count")
	}
	if identityActivityCount(t, ctx, pool, activity.ActionUserCreated, target.ID) != 1 {
		t.Fatal("account deletion removed existing audit history")
	}
}

func sortedDeletionSessions(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func TestStoreDeleteManagedUserRejectsRevisionMissingTargetAndLastAdministrator(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	for _, test := range []struct {
		id       string
		revision int64
		want     error
	}{
		{admin.ID, 0, identity.ErrInvalidInput},
		{admin.ID, 2, identity.ErrRevisionConflict},
		{"missing-user", 1, identity.ErrNotFound},
		{admin.ID, 1, identity.ErrLastAdministrator},
	} {
		before := managedDeletionSnapshot(t, ctx, pool)
		if _, err := store.DeleteManagedUser(ctx, actor, test.id, test.revision); !errors.Is(err, test.want) {
			t.Errorf("delete guard returned %v, want %v", err, test.want)
		}
		if managedDeletionSnapshot(t, ctx, pool) != before {
			t.Fatal("rejected deletion changed durable user or audit state")
		}
	}
	for _, changed := range []identity.Principal{
		{},
		{Kind: "emby", User: actor.User, SessionID: actor.SessionID},
		{Kind: "admin", User: actor.User, SessionID: actor.SessionID, ApplicationKeyID: 1},
	} {
		before := managedDeletionSnapshot(t, ctx, pool)
		if _, err := store.DeleteManagedUser(ctx, changed, admin.ID, 1); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("deletion accepted missing, wrong-audience or mixed authority: %v", err)
		}
		if managedDeletionSnapshot(t, ctx, pool) != before {
			t.Fatal("invalid principal changed deletion state")
		}
	}
	disabled, err := store.CreateUser(ctx, "Disabled Administrator", "disabled-password", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=true, management_revision=9223372036854775807 WHERE id=$1", disabled.ID); err != nil {
		t.Fatal(err)
	}
	if result, err := store.DeleteManagedUser(ctx, actor, disabled.ID, 9223372036854775807); err != nil || result.CurrentSessionRevoked {
		t.Fatalf("a disabled administrator was counted as the last enabled administrator or its final revision overflowed: %v", err)
	}
}

func TestStoreDeleteManagedUserConcurrentSelfDeletionPreservesOneAdministrator(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	first := bootstrapTestAdmin(t, ctx, store)
	second, err := store.CreateUser(ctx, "Remaining Administrator", "second-password", true)
	if err != nil {
		t.Fatal(err)
	}
	_, firstActor := managedLogin(t, ctx, store, first, "administrator-password", "admin")
	_, secondActor := managedLogin(t, ctx, store, second, "second-password", "admin")
	start := make(chan struct{})
	type outcome struct {
		result identity.ManagedUserDeletion
		err    error
	}
	results := make(chan outcome, 2)
	for _, actor := range []identity.Principal{firstActor, secondActor} {
		go func(actor identity.Principal) {
			<-start
			result, err := store.DeleteManagedUser(ctx, actor, actor.User.ID, 1)
			results <- outcome{result, err}
		}(actor)
	}
	close(start)
	succeeded, rejected := 0, 0
	for range 2 {
		select {
		case result := <-results:
			if result.err == nil && result.result.CurrentSessionRevoked {
				succeeded++
			} else if errors.Is(result.err, identity.ErrLastAdministrator) {
				rejected++
			} else {
				t.Fatalf("unexpected concurrent deletion result: %v", result.err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent account deletion did not close")
		}
	}
	var enabled int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_administrator AND NOT is_disabled").Scan(&enabled); err != nil || enabled != 1 || succeeded != 1 || rejected != 1 {
		t.Fatalf("competing deletions did not preserve exactly one administrator: %v", err)
	}
	if identityActivityCount(t, ctx, pool, activity.ActionUserDeleted, "") != 1 {
		t.Fatal("concurrent self-deletion did not commit exactly one truthful event")
	}
}

func TestStoreDeleteManagedUserAuditFailureRollsBackSelfAndTarget(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "other", true: "self"}[self], func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			other, err := store.CreateUser(ctx, "Audit Survivor", "other-password", true)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			target := other.ID
			if self {
				target = admin.ID
			}
			rejectIdentityActivity(t, ctx, pool)
			before := managedDeletionSnapshot(t, ctx, pool)
			if _, err := store.DeleteManagedUser(ctx, actor, target, 1); err == nil {
				t.Fatal("deletion committed despite an audit failure")
			}
			if managedDeletionSnapshot(t, ctx, pool) != before {
				t.Fatal("audit rejection failed to restore deleted rows")
			}
		})
	}
}

func TestStoreDeleteManagedUserDatabaseCommitRejectionRollsBackSelfDeletion(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	if _, err := store.CreateUser(ctx, "Commit Survivor", "survivor-password", true); err != nil {
		t.Fatal(err)
	}
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_user_deletion_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.action='user.deleted' THEN RAISE EXCEPTION 'user deletion commit rejected'; END IF; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER user_deletion_commit_rejection AFTER INSERT ON activity_entries
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_user_deletion_commit()`); err != nil {
		t.Fatal(err)
	}
	before := managedDeletionSnapshot(t, ctx, pool)
	var databaseError *pgconn.PgError
	if _, err := store.DeleteManagedUser(ctx, actor, admin.ID, 1); !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("expected a database-confirmed commit rejection: %v", err)
	}
	if managedDeletionSnapshot(t, ctx, pool) != before {
		t.Fatal("database-confirmed commit rejection retained user deletion or audit changes")
	}
}

func TestStoreDeleteManagedUserRechecksAuthorityAfterManagementWait(t *testing.T) {
	for _, change := range []string{"demoted", "disabled", "revoked"} {
		t.Run(change, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			admin := bootstrapTestAdmin(t, ctx, store)
			target, err := store.CreateUser(ctx, "Queued Target", "target-password", false)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
				t.Fatal(err)
			}
			done, result := make(chan struct{}), make(chan error, 1)
			go func() {
				_, err := store.DeleteManagedUser(ctx, actor, target.ID, 1)
				result <- err
				close(done)
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "pg_advisory_xact_lock", done)
			statement, id := "UPDATE users SET is_administrator=false WHERE id=$1", admin.ID
			if change == "disabled" {
				statement = "UPDATE users SET is_disabled=true WHERE id=$1"
			} else if change == "revoked" {
				statement, id = "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID
			}
			if _, err := pool.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			before := managedDeletionSnapshot(t, ctx, pool)
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("stale middleware authority survived the management lock wait: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("queued deletion did not close")
			}
			if managedDeletionSnapshot(t, ctx, pool) != before {
				t.Fatal("unauthorized queued deletion changed durable state")
			}
		})
	}
}

func TestStoreDeleteManagedUserFinalClockCheckAfterAuditWait(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "other", true: "self"}[self], func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			admin := bootstrapTestAdmin(t, ctx, store)
			other, err := store.CreateUser(ctx, "Expiry Survivor", "other-password", true)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			target := other.ID
			if self {
				target = admin.ID
			}
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			const gateClass int32 = 1196446301
			if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer,$2::integer)", gateClass, blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `CREATE TABLE managed_user_delete_gate (lock_class integer,lock_key integer);
				CREATE FUNCTION pause_managed_user_delete_audit() RETURNS trigger LANGUAGE plpgsql AS $$
				DECLARE gate managed_user_delete_gate%ROWTYPE;
				BEGIN SELECT * INTO gate FROM managed_user_delete_gate;
				PERFORM pg_advisory_xact_lock(gate.lock_class,gate.lock_key); RETURN NEW; END; $$;
				CREATE TRIGGER managed_user_delete_audit_barrier AFTER INSERT ON activity_entries
				FOR EACH ROW WHEN (NEW.action='user.deleted') EXECUTE FUNCTION pause_managed_user_delete_audit()`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO managed_user_delete_gate VALUES ($1,$2)", gateClass, blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',
				expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			before := managedDeletionSnapshot(t, ctx, pool)
			done, result := make(chan struct{}), make(chan error, 1)
			go func() {
				_, err := store.DeleteManagedUser(ctx, actor, target, 1)
				result <- err
				close(done)
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("expired deletion authority committed after its audit wait: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("deletion did not close after the audit gate")
			}
			if managedDeletionSnapshot(t, ctx, pool) != before {
				t.Fatal("final expiry rejection did not roll back user and audit deletion")
			}
		})
	}
}
