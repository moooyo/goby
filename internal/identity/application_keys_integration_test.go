package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func applicationKeyTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *identity.Store, identity.Principal, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("application key vault integration requires Linux")
	}
	ctx, pool, _ := identityTestStore(t)
	path := filepath.Join(t.TempDir(), "application-key-master")
	store := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(path))
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	return ctx, pool, store, actor, path
}

func issueApplicationKey(t *testing.T, ctx context.Context, store *identity.Store, actor identity.Principal, name string) identity.ApplicationKey {
	t.Helper()
	key, err := store.CreateApplicationKey(ctx, actor, name, "192.0.2.14", identity.Client{
		Name: "ignored client claim", DeviceID: "persistent-server-id", Device: "Test Server", Version: "1.0"})
	if err != nil {
		t.Fatalf("create application key: %v", err)
	}
	return key
}

func applicationKeyPrincipal(t *testing.T, ctx context.Context, store *identity.Store, key identity.ApplicationKey) identity.Principal {
	t.Helper()
	principal, err := store.ResolveEmby(ctx, key.Token)
	if err != nil {
		t.Fatalf("resolve application key: %v", err)
	}
	if !principal.IsApplicationKey() || !principal.CanManageServer() || principal.ApplicationKeyID != key.ID ||
		principal.User.ID != "" || !principal.ExpiresAt.IsZero() || principal.SessionID != key.CredentialID ||
		principal.ClientSessionID == "" || principal.ClientSessionID == principal.SessionID {
		t.Fatal("application key resolution fabricated a login or lost its immutable identity")
	}
	return principal
}

func applicationKeySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'credentials', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM sessions a),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c),
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u))::text`).Scan(&result); err != nil {
		t.Fatalf("snapshot application key state: %v", err)
	}
	return result
}

func TestStoreApplicationKeysAreIndependentUserlessCredentials(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	first := issueApplicationKey(t, ctx, store, admin, "Automation")
	second := issueApplicationKey(t, ctx, store, admin, "Automation")
	if first.ID < 1 || second.ID <= first.ID || first.CredentialID == second.CredentialID || first.Token == second.Token || len(first.Token) != 43 {
		t.Fatal("duplicate app names did not issue independent opaque credentials")
	}
	if first.AppName != "Automation" || first.Client.Name != first.AppName || first.Client.DeviceID != "persistent-server-id" ||
		first.Client.Device != "Test Server" || first.Client.Version != "1.0" || first.CreatedBy != admin.User.ID ||
		first.ReportedDeviceNumericID != 1 || first.LastUsedAt != nil || first.RevokedAt != nil {
		t.Fatal("application key metadata does not match the server identity")
	}
	var userless, indefinite bool
	var hash, ciphertext []byte
	if err := pool.QueryRow(ctx, `SELECT a.user_id IS NULL, a.expires_at IS NULL, a.token_hash, k.secret_ciphertext
		FROM sessions a JOIN application_keys k ON k.credential_id = a.id WHERE a.id = $1`, first.CredentialID).
		Scan(&userless, &indefinite, &hash, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if !userless || !indefinite || len(hash) != 32 || len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte(first.Token)) {
		t.Fatal("application key storage contains a fake login or an unprotected token")
	}
	principal := applicationKeyPrincipal(t, ctx, store, first)
	for _, kind := range []string{"admin", "emby", identity.ApplicationKeyKind} {
		if _, err := store.Resolve(ctx, first.Token, kind); !errors.Is(err, identity.ErrUnauthorized) {
			t.Errorf("application key crossed exact Resolve kind %s: %v", kind, err)
		}
	}
	if refreshed, err := store.RevalidateSession(ctx, principal); err != nil || refreshed.ApplicationKeyID != first.ID || refreshed.User.ID != "" {
		t.Fatalf("revalidate userless principal: %v", err)
	}
	third := issueApplicationKey(t, ctx, store, principal, "Key-created app")
	if third.CreatedBy != "" {
		t.Fatal("key-created credential inherited a user owner")
	}
	metadata, err := store.GetApplicationKey(ctx, admin, first.ID, false)
	if err != nil || metadata.Token != "" {
		t.Fatalf("safe application key read exposed a token: %v", err)
	}
	encoded, err := json.Marshal(first)
	if err != nil || bytes.Contains(encoded, []byte(first.Token)) || bytes.Contains(encoded, []byte(`"Token"`)) {
		t.Fatal("application key JSON exposed the recoverable bearer token")
	}
	page, err := store.ListApplicationKeys(ctx, principal, identity.ApplicationKeyFilter{Limit: 2})
	if err != nil || page.TotalRecordCount != 3 || len(page.Items) != 2 || page.Items[0].ID != third.ID || page.Items[1].ID != second.ID {
		t.Fatalf("application key creation pagination failed: %v", err)
	}
	for _, key := range page.Items {
		if key.Token != "" {
			t.Fatal("metadata list exposed a token")
		}
	}
	empty, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{StartIndex: 9, Limit: 2})
	if err != nil || empty.TotalRecordCount != 3 || len(empty.Items) != 0 {
		t.Fatalf("empty application key page lost count: %v", err)
	}
	literal, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{SearchTerm: "%"})
	if err != nil || literal.TotalRecordCount != 0 {
		t.Fatalf("search treated a literal as a wildcard: %v", err)
	}
	if err := store.TouchClientSession(ctx, principal); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateClientCapabilities(ctx, principal, principal.ClientSessionID, identity.ClientCapabilities{SupportsMediaControl: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateClientCapabilities(ctx, principal, second.CredentialID, identity.ClientCapabilities{}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("key updated another credential's capabilities: %v", err)
	}
	metadata, err = store.GetApplicationKey(ctx, admin, first.ID, false)
	if err != nil || metadata.LastUsedAt == nil {
		t.Fatalf("first authenticated key activity was not persisted: %v", err)
	}
	zero := 0
	sessions, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{SessionID: principal.ClientSessionID, ActiveWithinSeconds: &zero})
	if err != nil || len(sessions) != 1 || sessions[0].Kind != identity.ApplicationKeyKind || sessions[0].UserID != "" ||
		sessions[0].UserName != "" || sessions[0].ApplicationKeyID != first.ID || sessions[0].CredentialID != first.CredentialID || !sessions[0].ExpiresAt.IsZero() ||
		sessions[0].LastUsedAt == nil || !sessions[0].Capabilities.SupportsMediaControl {
		t.Fatalf("client session projection lost typed key metadata: %v", err)
	}
	managed, err := store.ListManagedSessions(ctx, admin, identity.ManagedSessionFilter{})
	if err != nil || managed.TotalRecordCount != 1 {
		t.Fatalf("native login list included application credentials: %v", err)
	}
	if _, err := store.RevokeManagedSession(ctx, admin, first.CredentialID); !errors.Is(err, identity.ErrManagedSessionNotFound) {
		t.Fatalf("native login revocation accepted application credential: %v", err)
	}
}

func TestStoreApplicationKeyManagementRevalidatesPrivileges(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Managed")
	viewer, err := store.CreateUser(ctx, "Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	before := applicationKeySnapshot(t, ctx, pool)
	if _, err := store.CreateApplicationKey(ctx, viewerActor, "Denied", "", identity.Client{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("viewer created key: %v", err)
	}
	if _, err := store.ListApplicationKeys(ctx, viewerActor, identity.ApplicationKeyFilter{RevealTokens: true}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("viewer listed key secrets: %v", err)
	}
	if _, err := store.GetApplicationKey(ctx, viewerActor, key.ID, true); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("viewer read key secret: %v", err)
	}
	if _, err := store.RevokeApplicationKeyToken(ctx, viewerActor, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("viewer revoked key: %v", err)
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("unauthorized key management changed persisted state")
	}
	_, embyAdmin := managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	issueApplicationKey(t, ctx, store, embyAdmin, "Administrator Emby app")
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListApplicationKeys(ctx, embyAdmin, identity.ApplicationKeyFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("demoted Emby snapshot retained key management: %v", err)
	}
	if _, err := store.RevokeApplicationKey(ctx, admin, key.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("demoted cookie snapshot retained key management: %v", err)
	}
	keyActor := applicationKeyPrincipal(t, ctx, store, key)
	if _, err := store.ListApplicationKeys(ctx, keyActor, identity.ApplicationKeyFilter{}); err != nil {
		t.Fatalf("creator demotion changed independent key authority: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", admin.User.ID); err != nil {
		t.Fatal(err)
	}
	keyActor = applicationKeyPrincipal(t, ctx, store, key)
	retained, err := store.GetApplicationKey(ctx, keyActor, key.ID, false)
	if err != nil || retained.CreatedBy != "" {
		t.Fatalf("creator deletion removed a key or retained dangling audit owner: %v", err)
	}
}

func TestStoreApplicationKeyVaultRestartAndFailureIsolation(t *testing.T) {
	ctx, pool, store, admin, path := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Durable")
	restarted := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(path))
	recovered, err := restarted.GetApplicationKey(ctx, admin, key.ID, true)
	if err != nil || recovered.Token != key.Token {
		t.Fatalf("restart could not recover the same key secret: %v", err)
	}
	page, err := restarted.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{RevealTokens: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].Token != key.Token {
		t.Fatalf("compatibility list could not recover key: %v", err)
	}
	master, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read owned test master file")
	}
	defer clear(master)
	if err := os.Remove(path); err != nil {
		t.Fatal("remove owned test master file")
	}
	missing := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(path))
	before := applicationKeySnapshot(t, ctx, pool)
	if _, err := missing.CreateApplicationKey(ctx, admin, "Must fail", "", identity.Client{}); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatalf("missing master silently replaced: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("creation regenerated a missing historical master")
	}
	if _, err := missing.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{RevealTokens: true}); !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatalf("missing master reveal did not fail closed: %v", err)
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("vault failure partially created credentials")
	}
	if _, err := missing.ResolveEmby(ctx, key.Token); err != nil {
		t.Fatalf("vault loss disabled hashed key authentication: %v", err)
	}
	if _, err := missing.Authenticate(ctx, admin.User.Name, "administrator-password", identity.Client{}, "admin"); err != nil {
		t.Fatalf("vault loss disabled ordinary login: %v", err)
	}
	if _, err := missing.GetApplicationKey(ctx, admin, key.ID, false); err != nil {
		t.Fatalf("safe metadata unnecessarily opened missing vault: %v", err)
	}
	wrong := bytes.Repeat([]byte{0x7b}, 32)
	if bytes.Equal(wrong, master) {
		wrong[0] ^= 0xff
	}
	if err := os.WriteFile(path, wrong, 0600); err != nil {
		t.Fatal("write owned replacement master fixture")
	}
	wrongStore := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(path))
	if _, err := wrongStore.CreateApplicationKey(ctx, admin, "Must fail after restart", "", identity.Client{}); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatalf("restart with a wrong master issued a key: %v", err)
	}
	if _, err := wrongStore.RevokeApplicationKeyToken(ctx, admin, "unknown"); err != nil {
		t.Fatalf("unknown revocation accessed failed vault: %v", err)
	}
	if _, err := wrongStore.RevokeApplicationKeyToken(ctx, admin, key.Token); err != nil {
		t.Fatalf("revocation accessed failed vault: %v", err)
	}
	if _, err := wrongStore.CreateApplicationKey(ctx, admin, "Revoked witness", "", identity.Client{}); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatalf("revoked history failed to protect master continuity: %v", err)
	}
}

func TestStoreApplicationKeyRevocationIsDurableScopedAndSecretFree(t *testing.T) {
	ctx, _, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "First")
	sibling := issueApplicationKey(t, ctx, store, admin, "Second")
	actor := applicationKeyPrincipal(t, ctx, store, key)
	revocation, err := store.RevokeApplicationKey(ctx, actor, key.ID)
	if err != nil || !revocation.CurrentCredentialRevoked || revocation.ID != key.ID || revocation.CredentialID != key.CredentialID || revocation.RevokedAt.IsZero() {
		t.Fatalf("self-revocation failed to commit exact credential: %v", err)
	}
	if _, err := store.ResolveEmby(ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked token remained valid: %v", err)
	}
	if _, err := store.RevalidateSession(ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked connection revalidated: %v", err)
	}
	if err := store.TouchClientSession(ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked key recorded activity: %v", err)
	}
	if _, err := store.RevokeApplicationKey(ctx, actor, sibling.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked actor reused exception: %v", err)
	}
	again, err := store.RevokeApplicationKey(ctx, admin, key.ID)
	if err != nil || !again.RevokedAt.Equal(revocation.RevokedAt) || again.CurrentCredentialRevoked {
		t.Fatalf("repeated revocation changed history: %v", err)
	}
	history, err := store.GetApplicationKey(ctx, admin, key.ID, true)
	if err != nil || history.RevokedAt == nil || history.Token != "" {
		t.Fatalf("revoked history revealed a secret: %v", err)
	}
	page, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{IncludeRevoked: true, RevealTokens: true})
	if err != nil || page.TotalRecordCount != 2 {
		t.Fatalf("history list failed: %v", err)
	}
	for _, row := range page.Items {
		if row.ID == key.ID && row.Token != "" {
			t.Fatal("revoked list row revealed a secret")
		}
	}
	if _, err := store.GetApplicationKey(ctx, admin, sibling.ID+999, false); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown native key read was not not-found: %v", err)
	}
	if _, err := store.RevokeApplicationKey(ctx, admin, sibling.ID+999); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown native key revoke was not not-found: %v", err)
	}
	if _, err := store.RevokeApplicationKeyToken(ctx, admin, strings.Repeat("A", 43)); err != nil {
		t.Fatalf("unknown valid token revocation was not idempotent: %v", err)
	}
	if _, err := store.ResolveEmby(ctx, sibling.Token); err != nil {
		t.Fatalf("revocation invalidated sibling application key: %v", err)
	}
	if err := store.Revoke(ctx, sibling.Token); err != nil {
		t.Fatalf("application key logout failed: %v", err)
	}
	if _, err := store.ResolveEmby(ctx, sibling.Token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("application key logout did not invalidate credential: %v", err)
	}
}

type applicationKeyRevokeOutcome struct {
	value identity.ApplicationKeyRevocation
	err   error
}

func revokeApplicationKeyAsync(ctx context.Context, store *identity.Store, actor identity.Principal, id int64) (<-chan applicationKeyRevokeOutcome, <-chan struct{}) {
	results := make(chan applicationKeyRevokeOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		value, err := store.RevokeApplicationKey(ctx, actor, id)
		results <- applicationKeyRevokeOutcome{value, err}
	}()
	return results, done
}

func awaitApplicationKeyRevoke(t *testing.T, ctx context.Context, results <-chan applicationKeyRevokeOutcome) applicationKeyRevokeOutcome {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("application key revocation did not finish")
		return applicationKeyRevokeOutcome{}
	}
}

func TestStoreApplicationKeyReciprocalRevocationsDoNotDeadlock(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	first := issueApplicationKey(t, ctx, store, admin, "First")
	second := issueApplicationKey(t, ctx, store, admin, "Second")
	firstActor := applicationKeyPrincipal(t, ctx, store, first)
	secondActor := applicationKeyPrincipal(t, ctx, store, second)
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", second.CredentialID); err != nil {
		t.Fatal(err)
	}
	firstResults, firstDone := revokeApplicationKeyAsync(ctx, store, firstActor, second.ID)
	firstPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions WHERE id = ANY", firstDone)
	secondResults, secondDone := revokeApplicationKeyAsync(ctx, store, secondActor, first.ID)
	waitManagedBlockedQuery(t, ctx, pool, firstPID, "pg_advisory_xact_lock", secondDone)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	firstResult := awaitApplicationKeyRevoke(t, ctx, firstResults)
	secondResult := awaitApplicationKeyRevoke(t, ctx, secondResults)
	if firstResult.err != nil || firstResult.value.ID != second.ID || firstResult.value.CurrentCredentialRevoked {
		t.Fatalf("first reciprocal key revocation failed: %v", firstResult.err)
	}
	if !errors.Is(secondResult.err, identity.ErrUnauthorized) || secondResult.value != (identity.ApplicationKeyRevocation{}) {
		t.Fatalf("revoked key committed reciprocal mutation: %v", secondResult.err)
	}
	if _, err := store.ResolveEmby(ctx, first.Token); err != nil {
		t.Fatalf("losing reciprocal mutation changed winning actor: %v", err)
	}
}

func TestStoreApplicationKeyExpiryAfterWriteRollsBack(t *testing.T) {
	testCtx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, testCtx, store, admin, "Rollback target")
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, key.CredentialID)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, admin.SessionID); err != nil {
		t.Fatal(err)
	}
	before := applicationKeySnapshot(t, ctx, pool)
	results, done := revokeApplicationKeyAsync(ctx, store, admin, key.ID)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", done)
	waitManagedSessionDatabaseExpiry(t, ctx, pool, admin.SessionID, done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitApplicationKeyRevoke(t, ctx, results)
	if !errors.Is(result.err, identity.ErrUnauthorized) || result.value != (identity.ApplicationKeyRevocation{}) {
		t.Fatalf("expired actor committed key revocation: %v", result.err)
	}
	if after := applicationKeySnapshot(t, testCtx, pool); after != before {
		t.Fatal("rejected key revocation did not roll back persisted state")
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 0)
}
