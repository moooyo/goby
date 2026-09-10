package identity_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type deviceSessionAuditEntry struct {
	Action            string
	Severity          string
	Source            string
	ActorKind         string
	ActorID           string
	ActorCredentialID string
	ResourceKind      string
	ResourceID        string
	RequestID         string
	Revision          int64
	AffectedCount     int64
	State             string
	ChangedFields     []string
}

type deviceSessionAuditMutationResult struct {
	device     identity.ManagedDevice
	deletion   identity.DeviceDeletion
	revocation identity.ManagedSessionRevocation
	err        error
}

func (result deviceSessionAuditMutationResult) hasBusinessResult() bool {
	return result.device != (identity.ManagedDevice{}) || result.revocation != (identity.ManagedSessionRevocation{}) ||
		result.deletion.ID != 0 || !result.deletion.DeletedAt.IsZero() || result.deletion.RevokedLoginCount != 0 ||
		result.deletion.RevokedSessionIDs != nil
}

func readDeviceSessionAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action, resourceID string) []deviceSessionAuditEntry {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT action, severity, source, actor_kind, actor_id,
		actor_credential_id, resource_kind, resource_id, request_id, revision,
		affected_count, state, changed_fields FROM activity_entries
		WHERE action = $1 AND resource_id = $2 ORDER BY id`, action, resourceID)
	if err != nil {
		t.Fatalf("read device or session audit: %v", err)
	}
	defer rows.Close()
	var entries []deviceSessionAuditEntry
	for rows.Next() {
		var entry deviceSessionAuditEntry
		if err := rows.Scan(&entry.Action, &entry.Severity, &entry.Source, &entry.ActorKind,
			&entry.ActorID, &entry.ActorCredentialID, &entry.ResourceKind, &entry.ResourceID,
			&entry.RequestID, &entry.Revision, &entry.AffectedCount, &entry.State, &entry.ChangedFields); err != nil {
			t.Fatalf("scan device or session audit: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish device or session audit: %v", err)
	}
	return entries
}

func assertDeviceSessionAudit(t *testing.T, got, want deviceSessionAuditEntry) {
	t.Helper()
	if got.Action != want.Action || got.Severity != "Info" || got.Source != want.Source ||
		got.ActorKind != want.ActorKind || got.ActorID != want.ActorID ||
		got.ActorCredentialID != want.ActorCredentialID || got.ResourceKind != want.ResourceKind ||
		got.ResourceID != want.ResourceID || got.RequestID != "" || got.Revision != want.Revision ||
		got.AffectedCount != want.AffectedCount || got.State != "" || !slices.Equal(got.ChangedFields, want.ChangedFields) {
		t.Fatal("audit did not retain the committed action, source, identities, revision, count, and safe field names")
	}
}

func deviceSessionAuditSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id), '[]'::jsonb)::text FROM "+pgx.Identifier{table}.Sanitize()+" a").Scan(&snapshot); err != nil {
		t.Fatalf("snapshot device and session audit: %v", err)
	}
	return snapshot
}

func TestStoreDeviceAuditRecordsTransitionsAndPersistedActors(t *testing.T) {
	for _, actorKind := range []string{"native", "emby", "application_key"} {
		t.Run(actorKind, func(t *testing.T) {
			var ctx context.Context
			var pool *pgxpool.Pool
			var store *identity.Store
			var native, actor identity.Principal
			if actorKind == "application_key" {
				ctx, pool, store, native, _ = applicationKeyTestStore(t)
				key := issueApplicationKey(t, ctx, store, native, "Private Audit Key Name")
				actor = applicationKeyPrincipal(t, ctx, store, key)
			} else {
				ctx, pool, store = identityTestStore(t)
				admin := bootstrapTestAdmin(t, ctx, store)
				_, native = managedLogin(t, ctx, store, admin, "administrator-password", "admin")
				actor = native
				if actorKind == "emby" {
					_, actor = managedLogin(t, ctx, store, admin, "administrator-password", "emby")
				}
			}
			client := identity.Client{Name: "Private Audit Client", DeviceID: "audit-reported-device", Device: "Private Reported Device", Version: "9.7.6"}
			first, _ := deviceLogin(t, ctx, store, native.User, "administrator-password", client, "emby", "192.0.2.175")
			deviceLogin(t, ctx, store, native.User, "administrator-password", client, "emby", "192.0.2.176")
			expired, _ := deviceLogin(t, ctx, store, native.User, "administrator-password", client, "emby", "192.0.2.177")
			previouslyRevoked, _ := deviceLogin(t, ctx, store, native.User, "administrator-password", client, "emby", "192.0.2.178")
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours',
				expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`, expired.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := store.Revoke(ctx, previouslyRevoked.Token); err != nil {
				t.Fatal(err)
			}
			initial := onlyDevice(t, ctx, store, native)
			if initial.ActiveLoginCount != 2 {
				t.Fatal("audit fixture did not distinguish active logins from unrevoked historical logins")
			}
			resourceID := strconv.FormatInt(initial.ID, 10)
			update := func(revision int64, name string) (identity.ManagedDevice, error) {
				if actorKind == "native" {
					return store.UpdateManagedDeviceOptions(ctx, actor, initial.ID, revision, name)
				}
				return store.UpdateEmbyDeviceOptions(ctx, actor, client.DeviceID, name)
			}
			unchanged, err := update(initial.Revision, "   ")
			if err != nil || unchanged.Revision != initial.Revision {
				t.Fatalf("empty device override no-op failed: %v", err)
			}
			if len(readDeviceSessionAudit(t, ctx, pool, "device.updated", resourceID)) != 0 {
				t.Fatal("unchanged device options generated an audit entry")
			}
			renamed, err := update(initial.Revision, " Private Custom Device ")
			if err != nil || renamed.Revision != initial.Revision+1 {
				t.Fatalf("audited device rename failed: %v", err)
			}
			unchanged, err = update(renamed.Revision, "Private Custom Device")
			if err != nil || unchanged.Revision != renamed.Revision {
				t.Fatalf("equivalent trimmed device override failed: %v", err)
			}
			cleared, err := update(renamed.Revision, "")
			if err != nil || cleared.Revision != renamed.Revision+1 || cleared.CustomName != nil {
				t.Fatalf("audited device override removal failed: %v", err)
			}
			want := deviceSessionAuditEntry{Action: "device.updated", Source: "emby", ActorKind: "user",
				ActorID: actor.User.ID, ActorCredentialID: actor.SessionID, ResourceKind: "device", ResourceID: resourceID,
				AffectedCount: 1, ChangedFields: []string{"CustomName"}}
			if actorKind == "native" {
				want.Source = "native"
			}
			if actorKind == "application_key" {
				want.ActorKind = "application_key"
				want.ActorID = strconv.FormatInt(actor.ApplicationKeyID, 10)
			}
			entries := readDeviceSessionAudit(t, ctx, pool, "device.updated", resourceID)
			if len(entries) != 2 {
				t.Fatalf("device update audit count = %d, want two actual transitions", len(entries))
			}
			for index, revision := range []int64{renamed.Revision, cleared.Revision} {
				want.Revision = revision
				assertDeviceSessionAudit(t, entries[index], want)
			}
			var deletion identity.DeviceDeletion
			if actorKind == "native" {
				deletion, err = store.DeleteManagedDevice(ctx, actor, initial.ID, cleared.Revision)
			} else {
				deletion, err = store.DeleteEmbyDevice(ctx, actor, client.DeviceID)
			}
			if err != nil || deletion.RevokedLoginCount != 3 || deletion.DeletedAt.IsZero() {
				t.Fatalf("audited device removal failed to count all newly revoked logins: %v", err)
			}
			var repeated identity.DeviceDeletion
			if actorKind == "native" {
				repeated, err = store.DeleteManagedDevice(ctx, actor, initial.ID, initial.Revision)
			} else {
				repeated, err = store.DeleteEmbyDevice(ctx, actor, resourceID)
			}
			if err != nil || repeated.RevokedLoginCount != 0 || !repeated.DeletedAt.Equal(deletion.DeletedAt) {
				t.Fatalf("audited device removal retry was not idempotent: %v", err)
			}
			entries = readDeviceSessionAudit(t, ctx, pool, "device.removed", resourceID)
			if len(entries) != 1 {
				t.Fatalf("device removal audit count = %d, want one logical transition", len(entries))
			}
			want.Action, want.Revision, want.AffectedCount, want.ChangedFields = "device.removed", cleared.Revision+1, 3, nil
			assertDeviceSessionAudit(t, entries[0], want)
			if len(readDeviceSessionAudit(t, ctx, pool, "device.updated", resourceID)) != 2 {
				t.Fatal("logical device removal discarded committed update audit history")
			}
			var payload string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text
				FROM activity_entries a WHERE action IN ('device.updated', 'device.removed') AND resource_id = $1`, resourceID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			for _, privateValue := range []string{client.Name, client.DeviceID, client.Device, client.Version,
				"Private Custom Device", "Private Audit Key Name", "192.0.2.175", first.Token, native.User.Name} {
				if strings.Contains(payload, privateValue) {
					t.Fatal("device audit retained a private value instead of safe identifiers and field names")
				}
			}
		})
	}
}

func TestStoreManagedSessionAuditRetainsSelfRevocationHistory(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := deviceLogin(t, ctx, store, admin, "administrator-password",
		identity.Client{Name: "Self Revocation Client", DeviceID: "self-revocation-audit", Device: "Private Self Device"}, "admin", "192.0.2.179")
	_, observer := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	seedDevicePlaybackHistory(t, ctx, pool, actor)
	before := deviceRetainedHistory(t, ctx, pool, []string{actor.SessionID})
	result, err := store.RevokeManagedSession(ctx, actor, actor.SessionID)
	if err != nil || !result.CurrentSessionRevoked || result.SessionID != actor.SessionID || result.RevokedAt.IsZero() {
		t.Fatalf("audited managed self-revocation failed: %v", err)
	}
	if after := deviceRetainedHistory(t, ctx, pool, []string{actor.SessionID}); after != before {
		t.Fatal("audited self-revocation changed retained authentication or playback history")
	}
	repeated, err := store.RevokeManagedSession(ctx, observer, actor.SessionID)
	if err != nil || repeated.CurrentSessionRevoked || !repeated.RevokedAt.Equal(result.RevokedAt) {
		t.Fatalf("managed session revocation retry changed its committed timestamp: %v", err)
	}
	if _, err := store.RevokeManagedSession(ctx, actor, observer.SessionID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked audit actor retained authorization: %v", err)
	}
	entries := readDeviceSessionAudit(t, ctx, pool, "session.revoked", actor.SessionID)
	if len(entries) != 1 {
		t.Fatalf("managed session revocation audit count = %d, want one", len(entries))
	}
	assertDeviceSessionAudit(t, entries[0], deviceSessionAuditEntry{Action: "session.revoked", Source: "native",
		ActorKind: "user", ActorID: admin.ID, ActorCredentialID: actor.SessionID, ResourceKind: "session", ResourceID: actor.SessionID,
		AffectedCount: 1})
	if len(readDeviceSessionAudit(t, ctx, pool, "session.revoked", observer.SessionID)) != 0 {
		t.Fatal("unauthorized session revocation left an audit entry")
	}
	assertManagedTokenRevoked(t, ctx, store, credentials, "admin")
}

func TestStoreDeviceAuditAllowsSelfRemovalAndRetainsHistory(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, observer := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	client := identity.Client{Name: "Self Removal Client", DeviceID: "self-removal-audit", Device: "Private Removal Device"}
	credentials, actor := deviceLogin(t, ctx, store, admin, "administrator-password", client, "emby", "192.0.2.180")
	sibling, _ := deviceLogin(t, ctx, store, admin, "administrator-password", client, "emby", "192.0.2.181")
	device := onlyDevice(t, ctx, store, observer)
	seedDevicePlaybackHistory(t, ctx, pool, actor)
	revokedIDs := []string{actor.SessionID, sibling.SessionID}
	before := deviceRetainedHistory(t, ctx, pool, revokedIDs)
	result, err := store.DeleteEmbyDevice(ctx, actor, client.DeviceID)
	if err != nil || result.RevokedLoginCount != 2 || result.DeletedAt.IsZero() {
		t.Fatalf("audited device self-removal failed: %v", err)
	}
	if after := deviceRetainedHistory(t, ctx, pool, revokedIDs); after != before {
		t.Fatal("audited device self-removal changed retained authentication or playback history")
	}
	repeated, err := store.DeleteManagedDevice(ctx, observer, device.ID, device.Revision)
	if err != nil || repeated.RevokedLoginCount != 0 || !repeated.DeletedAt.Equal(result.DeletedAt) {
		t.Fatalf("device self-removal retry changed its committed result: %v", err)
	}
	resourceID := strconv.FormatInt(device.ID, 10)
	entries := readDeviceSessionAudit(t, ctx, pool, "device.removed", resourceID)
	if len(entries) != 1 {
		t.Fatalf("device self-removal audit count = %d, want one", len(entries))
	}
	assertDeviceSessionAudit(t, entries[0], deviceSessionAuditEntry{Action: "device.removed", Source: "emby",
		ActorKind: "user", ActorID: admin.ID, ActorCredentialID: actor.SessionID, ResourceKind: "device", ResourceID: resourceID,
		Revision: device.Revision + 1, AffectedCount: 2})
	assertManagedTokenRevoked(t, ctx, store, credentials, "emby")
	assertManagedTokenRevoked(t, ctx, store, sibling, "emby")
}

func installDeviceSessionAuditFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, failure string) (string, string) {
	t.Helper()
	if failure == "missing_table" {
		if _, err := pool.Exec(ctx, "ALTER TABLE activity_entries RENAME TO unavailable_device_session_activity"); err != nil {
			t.Fatal(err)
		}
		return "unavailable_device_session_activity", "42P01"
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_device_session_activity() RETURNS trigger LANGUAGE plpgsql AS $function$
	BEGIN
		RAISE EXCEPTION 'activity insertion rejected by test' USING ERRCODE = 'P0001';
	END;
	$function$;
	CREATE TRIGGER reject_device_session_activity_insert BEFORE INSERT ON activity_entries
		FOR EACH ROW EXECUTE FUNCTION reject_device_session_activity()`); err != nil {
		t.Fatalf("install activity rejection trigger: %v", err)
	}
	return "activity_entries", "P0001"
}

func TestStoreDeviceAndSessionAuditFailureRollsBackBusinessState(t *testing.T) {
	for _, operation := range []string{"update", "remove", "revoke"} {
		for _, failure := range []string{"missing_table", "reject_trigger"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				ctx, pool, store := identityTestStore(t)
				admin := bootstrapTestAdmin(t, ctx, store)
				_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
				client := identity.Client{Name: "Rollback Client", DeviceID: "audit-rollback-device", Device: "Rollback Device"}
				target, principal := deviceLogin(t, ctx, store, admin, "administrator-password", client, "emby", "192.0.2.182")
				deviceLogin(t, ctx, store, admin, "administrator-password", client, "emby", "192.0.2.183")
				device := onlyDevice(t, ctx, store, actor)
				seedDevicePlaybackHistory(t, ctx, pool, principal)
				businessBefore := deviceManagementSnapshot(t, ctx, pool)
				historyBefore := deviceRetainedHistory(t, ctx, pool, nil)
				table, code := installDeviceSessionAuditFailure(t, ctx, pool, failure)
				auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, table)
				var result deviceSessionAuditMutationResult
				switch operation {
				case "update":
					result.device, result.err = store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, "Rejected Audit Name")
				case "remove":
					result.deletion, result.err = store.DeleteManagedDevice(ctx, actor, device.ID, device.Revision)
				case "revoke":
					result.revocation, result.err = store.RevokeManagedSession(ctx, actor, target.SessionID)
				}
				var databaseError *pgconn.PgError
				if !errors.As(result.err, &databaseError) || databaseError.Code != code || result.hasBusinessResult() {
					t.Fatalf("mutation did not propagate its audit insertion failure with an empty result: %v", result.err)
				}
				if deviceManagementSnapshot(t, ctx, pool) != businessBefore || deviceRetainedHistory(t, ctx, pool, nil) != historyBefore {
					t.Fatal("audit insertion failure committed device, authentication, or retained history changes")
				}
				if deviceSessionAuditSnapshot(t, ctx, pool, table) != auditBefore {
					t.Fatal("failed mutation changed retained audit history")
				}
			})
		}
	}
}

// AFTER INSERT proves the activity write has happened before database time
// invalidates the locked actor. Its counter must roll back with both records.
func pauseDeviceSessionAuditInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action, resourceID string) (pgx.Tx, int32) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE device_session_audit_gate (
		action text NOT NULL,
		resource_id text NOT NULL,
		lock_class integer NOT NULL,
		lock_key integer NOT NULL,
		insert_count integer NOT NULL DEFAULT 0,
		PRIMARY KEY (action, resource_id)
	);
	CREATE FUNCTION pause_device_session_audit() RETURNS trigger LANGUAGE plpgsql AS $function$
	DECLARE
		gate device_session_audit_gate%ROWTYPE;
	BEGIN
		UPDATE device_session_audit_gate SET insert_count = insert_count + 1
			WHERE action = NEW.action AND resource_id = NEW.resource_id RETURNING * INTO gate;
		IF FOUND THEN
			PERFORM pg_advisory_xact_lock(gate.lock_class, gate.lock_key);
		END IF;
		RETURN NEW;
	END;
	$function$;
	CREATE TRIGGER device_session_audit_barrier AFTER INSERT ON activity_entries
		FOR EACH ROW EXECUTE FUNCTION pause_device_session_audit()`); err != nil {
		t.Fatalf("install device and session audit insertion barrier: %v", err)
	}
	blocker, pid := managedSessionBlocker(t, ctx, pool)
	const lockClass int32 = 1196446298
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, $2::integer)", lockClass, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO device_session_audit_gate (action, resource_id, lock_class, lock_key)
		VALUES ($1, $2, $3, $4)`, action, resourceID, lockClass, pid); err != nil {
		t.Fatal(err)
	}
	return blocker, pid
}

func TestStoreDeviceAndSessionAuditPrecedesFinalFreshAuthorization(t *testing.T) {
	for _, operation := range []string{"update", "remove", "remove_self", "revoke", "revoke_self"} {
		t.Run(operation, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, native := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			target, deviceActor := deviceLogin(t, testCtx, store, admin, "administrator-password",
				identity.Client{Name: "Expiry Client", DeviceID: "audit-expiry-device", Device: "Expiry Device"}, "emby", "192.0.2.184")
			device := onlyDevice(t, testCtx, store, native)
			actor, action, resourceID := native, "device.updated", strconv.FormatInt(device.ID, 10)
			if strings.HasPrefix(operation, "remove") {
				action = "device.removed"
			}
			if operation == "remove_self" {
				actor = deviceActor
			}
			if strings.HasPrefix(operation, "revoke") {
				action, resourceID = "session.revoked", target.SessionID
			}
			if operation == "revoke_self" {
				resourceID = actor.SessionID
			}
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseDeviceSessionAuditInsert(t, ctx, pool, action, resourceID)
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			businessBefore := deviceManagementSnapshot(t, ctx, pool)
			auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, "activity_entries")
			results := make(chan deviceSessionAuditMutationResult, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				var result deviceSessionAuditMutationResult
				switch operation {
				case "update":
					result.device, result.err = store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, "Expired Audit Rename")
				case "remove":
					result.deletion, result.err = store.DeleteManagedDevice(ctx, actor, device.ID, device.Revision)
				case "remove_self":
					result.deletion, result.err = store.DeleteEmbyDevice(ctx, actor, resourceID)
				case "revoke", "revoke_self":
					result.revocation, result.err = store.RevokeManagedSession(ctx, actor, resourceID)
				}
				results <- result
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-results:
				if !errors.Is(result.err, identity.ErrUnauthorized) || result.hasBusinessResult() {
					t.Fatalf("actor expired after the activity insertion returned a result or unexpected error: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("mutation did not finish after releasing its audit insertion barrier")
			}
			if deviceManagementSnapshot(t, testCtx, pool) != businessBefore {
				t.Fatal("actor expiry after the activity insertion committed a device or authentication change")
			}
			if deviceSessionAuditSnapshot(t, testCtx, pool, "activity_entries") != auditBefore {
				t.Fatal("actor expiry left audit history for a rolled-back mutation")
			}
			var insertCount int
			if err := pool.QueryRow(testCtx, "SELECT insert_count FROM device_session_audit_gate").Scan(&insertCount); err != nil {
				t.Fatal(err)
			}
			if insertCount != 0 {
				t.Fatal("actor expiry committed the audit insertion trigger's transactional side effect")
			}
		})
	}
}
