package identity_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

func rejectIdentityActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries
		ADD CONSTRAINT identity_activity_rejection CHECK (false) NOT VALID`); err != nil {
		t.Fatalf("reject new identity activity: %v", err)
	}
}

func acceptIdentityActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "ALTER TABLE activity_entries DROP CONSTRAINT identity_activity_rejection"); err != nil {
		t.Fatalf("restore identity activity writes: %v", err)
	}
}

// Snapshots contain secret material only in test memory, never assertion output.
func identityAuditSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'sessions', (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s),
		'devices', (SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM devices d),
		'settings', (SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM server_settings s),
		'activity', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM activity_entries a))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot identity and activity state: %v", err)
	}
	return snapshot
}

func identityActivityCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, resourceID string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries
		WHERE action = $1 AND ($2 = '' OR resource_id = $2)`, string(action), resourceID).Scan(&count); err != nil {
		t.Fatalf("count identity activity: %v", err)
	}
	return count
}

func readIdentityActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, resourceID string) activity.Event {
	t.Helper()
	var event activity.Event
	var fields []string
	if err := pool.QueryRow(ctx, `SELECT action, source, actor_kind, actor_id, actor_credential_id,
		resource_kind, resource_id, revision, affected_count, changed_fields
		FROM activity_entries WHERE action = $1 AND resource_id = $2`, string(action), resourceID).
		Scan(&event.Action, &event.Source, &event.Actor.Kind, &event.Actor.ID, &event.Actor.CredentialID,
			&event.Resource.Kind, &event.Resource.ID, &event.Revision, &event.Count, &fields); err != nil {
		t.Fatalf("read identity activity: %v", err)
	}
	for _, field := range fields {
		event.ChangedFields = append(event.ChangedFields, activity.Field(field))
	}
	return event
}

func TestStoreIdentityBootstrapAuditIsAtomic(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	rejectIdentityActivity(t, ctx, pool)
	before := identityAuditSnapshot(t, ctx, pool)
	if _, err := store.Bootstrap(ctx, "Bootstrap Audit Needle", "bootstrap-audit-password"); err == nil {
		t.Fatal("bootstrap succeeded after its audit was rejected")
	}
	if identityAuditSnapshot(t, ctx, pool) != before {
		t.Fatal("rejected bootstrap audit committed an account or setup state")
	}
	acceptIdentityActivity(t, ctx, pool)
	admin, err := store.Bootstrap(ctx, "Bootstrap Audit Needle", "bootstrap-audit-password")
	if err != nil {
		t.Fatalf("bootstrap with audit storage restored: %v", err)
	}
	event := readIdentityActivity(t, ctx, pool, activity.ActionUserCreated, admin.ID)
	if event.Actor.Kind != activity.ActorSystem || event.Actor.ID != "" || event.Actor.CredentialID != "" ||
		event.Source != activity.SourceNative || event.Resource.Kind != activity.ResourceUser || event.Revision != 1 {
		t.Fatal("bootstrap activity did not retain its explicit system actor and native source")
	}
	if _, err := store.Bootstrap(ctx, "Second Bootstrap", "second-bootstrap-password"); !errors.Is(err, identity.ErrAlreadyInitialized) {
		t.Fatalf("repeat bootstrap error = %v", err)
	}
	if identityActivityCount(t, ctx, pool, activity.ActionUserCreated, "") != 1 {
		t.Fatal("repeat bootstrap wrote an extra activity")
	}
}

func TestStoreIdentityLoginAuditUsesIssuedSessionAndRollsBackDevice(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	for _, kind := range []string{"admin", "emby"} {
		credentials, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, kind)
		if err != nil {
			t.Fatalf("authenticate audited %s login: %v", kind, err)
		}
		event := readIdentityActivity(t, ctx, pool, activity.ActionSessionLogin, credentials.SessionID)
		source := activity.SourceNative
		if kind == "emby" {
			source = activity.SourceEmby
		}
		if event.Actor.Kind != activity.ActorUser || event.Actor.ID != admin.ID ||
			event.Actor.CredentialID != credentials.SessionID || event.Resource.Kind != activity.ResourceSession || event.Source != source {
			t.Fatal("login audit lost its issued credential, account, or audience")
		}
	}
	before := identityAuditSnapshot(t, ctx, pool)
	if _, err := store.Authenticate(ctx, admin.Name, "invalid-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("invalid login error = %v", err)
	}
	if identityAuditSnapshot(t, ctx, pool) != before {
		t.Fatal("failed authentication changed identity or audit state")
	}
	rejectIdentityActivity(t, ctx, pool)
	credentials, err := store.AuthenticateWithPeer(ctx, admin.Name, "administrator-password", identity.Client{
		Name: "Client Audit Needle", DeviceID: "new-audit-device", Device: "Device Audit Needle", Version: "Version Audit Needle",
	}, "emby", "192.0.2.71")
	if err == nil || credentials.Token != "" || credentials.SessionID != "" {
		t.Fatal("login returned a credential after its audit failed")
	}
	if identityAuditSnapshot(t, ctx, pool) != before {
		t.Fatal("rejected login audit committed a session or device registration")
	}
}

func TestStoreIdentityTokenRevocationAuditIsAtomicAndIdempotent(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, _ := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	rejectIdentityActivity(t, ctx, pool)
	before := identityAuditSnapshot(t, ctx, pool)
	if err := store.Revoke(ctx, credentials.Token); err == nil {
		t.Fatal("token revocation succeeded after its audit failed")
	}
	if identityAuditSnapshot(t, ctx, pool) != before {
		t.Fatal("rejected token revocation audit changed the credential")
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatalf("audit failure revoked the original credential: %v", err)
	}
	acceptIdentityActivity(t, ctx, pool)
	const attempts = 4
	results := make(chan error, attempts)
	for range attempts {
		go func() { results <- store.Revoke(ctx, credentials.Token) }()
	}
	for range attempts {
		if err := <-results; err != nil {
			t.Fatalf("concurrent token revocation: %v", err)
		}
	}
	for _, token := range []string{credentials.Token, "invalid-token", strings.Repeat("A", 43)} {
		if err := store.Revoke(ctx, token); err != nil {
			t.Fatalf("idempotent token revocation: %v", err)
		}
	}
	if identityActivityCount(t, ctx, pool, activity.ActionSessionRevoked, credentials.SessionID) != 1 {
		t.Fatal("concurrent or repeated token revocation duplicated activity")
	}
	event := readIdentityActivity(t, ctx, pool, activity.ActionSessionRevoked, credentials.SessionID)
	if event.Actor.Kind != activity.ActorUser || event.Actor.ID != admin.ID ||
		event.Actor.CredentialID != credentials.SessionID || event.Source != activity.SourceEmby {
		t.Fatal("token revocation did not identify its persisted owner and credential")
	}
	expired, _ := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = now() - interval '2 days',
		expires_at = now() - interval '1 day' WHERE id = $1`, expired.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, expired.Token); err != nil {
		t.Fatalf("retire expired credential: %v", err)
	}
	event = readIdentityActivity(t, ctx, pool, activity.ActionSessionRevoked, expired.SessionID)
	if event.Source != activity.SourceNative || event.Actor.ID != admin.ID {
		t.Fatal("expired token retirement lost its original native identity")
	}
}

func TestStoreIdentityTokenRevocationRecordsUserlessKeyIdentity(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Token Revocation Audit Needle")
	if err := store.Revoke(ctx, key.Token); err != nil {
		t.Fatalf("revoke application credential by token: %v", err)
	}
	if err := store.Revoke(ctx, key.Token); err != nil {
		t.Fatalf("repeat application credential retirement: %v", err)
	}
	id := strconv.FormatInt(key.ID, 10)
	if identityActivityCount(t, ctx, pool, activity.ActionApplicationKeyRevoked, id) != 1 {
		t.Fatal("application token retirement did not record exactly its first transition")
	}
	event := readIdentityActivity(t, ctx, pool, activity.ActionApplicationKeyRevoked, id)
	if event.Actor.Kind != activity.ActorApplicationKey || event.Actor.ID != id ||
		event.Actor.CredentialID != key.CredentialID || event.Resource.Kind != activity.ResourceApplicationKey ||
		event.Source != activity.SourceEmby {
		t.Fatal("application token retirement fabricated a user or system actor")
	}
}

func TestStoreManagedUserAuditFailureRollsBackEveryMutation(t *testing.T) {
	for _, action := range []string{"create", "update", "reset"} {
		t.Run(action, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			if _, err := store.CreateUser(ctx, "Remaining Audit Administrator", "remaining-password", true); err != nil {
				t.Fatal(err)
			}
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			managedLogin(t, ctx, store, admin, "administrator-password", "emby")
			current := readManagedUser(t, ctx, store, admin.ID)
			rejectIdentityActivity(t, ctx, pool)
			before := identityAuditSnapshot(t, ctx, pool)
			var err error
			switch action {
			case "create":
				_, err = store.CreateManagedUser(ctx, actor, "Rejected Creation Needle", "creation-password", false)
			case "update":
				input := managedUpdate(current)
				input.IsDisabled = true
				_, err = store.UpdateManagedUser(ctx, actor, admin.ID, input)
			case "reset":
				_, err = store.ResetManagedUserPassword(ctx, actor, admin.ID, current.Revision, "rejected-reset-password")
			}
			if err == nil {
				t.Fatal("managed mutation succeeded after its audit failed")
			}
			if identityAuditSnapshot(t, ctx, pool) != before {
				t.Fatal("rejected audit changed accounts, passwords, or revoked sessions")
			}
		})
	}
}

func TestStoreManagedUserMissingAuthorityDoesNotWriteActivity(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	current := readManagedUser(t, ctx, store, admin.ID)
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	before := identityAuditSnapshot(t, ctx, pool)
	if _, err := store.CreateManagedUser(ctx, actor, "Missing Authority Needle", "creation-password", false); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("unauthorized account creation error = %v", err)
	}
	if _, err := store.UpdateManagedUser(ctx, actor, admin.ID, managedUpdate(current)); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("unauthorized account update error = %v", err)
	}
	if _, err := store.ResetManagedUserPassword(ctx, actor, admin.ID, current.Revision, "unauthorized-password"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("unauthorized password reset error = %v", err)
	}
	if identityAuditSnapshot(t, ctx, pool) != before {
		t.Fatal("missing authority changed identity or activity history")
	}
}

func TestStoreManagedUserActivityStoresOnlyChangedFieldNames(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	user, err := store.CreateManagedUser(ctx, actor, "Original User Audit Needle", "original-user-audit-password", false)
	if err != nil {
		t.Fatal(err)
	}
	input := managedUpdate(readManagedUser(t, ctx, store, user.ID))
	input.Name = "Renamed User Audit Needle"
	input.Policy.EnableMediaPlayback = false
	updated, err := store.UpdateManagedUser(ctx, actor, user.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResetManagedUserPassword(ctx, actor, user.ID, updated.User.Revision, "replacement-user-audit-password"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []activity.Action{activity.ActionUserCreated, activity.ActionUserUpdated, activity.ActionUserPasswordReset} {
		event := readIdentityActivity(t, ctx, pool, action, user.ID)
		if event.Actor.Kind != activity.ActorUser || event.Actor.ID != admin.ID ||
			event.Actor.CredentialID != credentials.SessionID || event.Source != activity.SourceNative {
			t.Fatal("managed user activity lost the authorized account and credential")
		}
		if action == activity.ActionUserUpdated && (event.Revision != updated.User.Revision ||
			!slices.Equal(event.ChangedFields, []activity.Field{activity.FieldEnableMediaPlayback, activity.FieldName})) {
			t.Fatal("user update activity did not record exactly the changed field names and revision")
		}
	}
	var history string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(a))::text FROM activity_entries a").Scan(&history); err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{admin.Name, user.Name, input.Name, "administrator-password",
		"original-user-audit-password", "replacement-user-audit-password", credentials.Token} {
		if strings.Contains(history, strconv.Quote(sensitive)) {
			t.Fatal("identity activity persisted a display name, password, or token")
		}
	}
}

func TestStoreManagedUserAuditPreservesLegitimateSelfRevocation(t *testing.T) {
	for _, action := range []string{"demote", "disable", "reset"} {
		t.Run(action, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			if _, err := store.CreateUser(ctx, "Remaining Self Audit Administrator", "remaining-password", true); err != nil {
				t.Fatal(err)
			}
			credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			current := readManagedUser(t, ctx, store, admin.ID)
			var result identity.ManagedUserMutation
			var err error
			eventAction := activity.ActionUserUpdated
			if action == "reset" {
				result, err = store.ResetManagedUserPassword(ctx, actor, admin.ID, current.Revision, "self-audit-password")
				eventAction = activity.ActionUserPasswordReset
			} else {
				input := managedUpdate(current)
				input.IsDisabled = action == "disable"
				input.IsAdministrator = action != "demote"
				result, err = store.UpdateManagedUser(ctx, actor, admin.ID, input)
			}
			if err != nil || !result.CurrentSessionRevoked {
				t.Fatalf("audited self mutation did not commit its self revocation: %v", err)
			}
			assertManagedTokenRevoked(t, ctx, store, credentials, "admin")
			if identityActivityCount(t, ctx, pool, eventAction, admin.ID) != 1 {
				t.Fatal("self mutation failed to commit exactly one account activity")
			}
			event := readIdentityActivity(t, ctx, pool, eventAction, admin.ID)
			if event.Actor.ID != admin.ID || event.Actor.CredentialID != credentials.SessionID {
				t.Fatal("self revocation lost the original authorized actor")
			}
		})
	}
}
