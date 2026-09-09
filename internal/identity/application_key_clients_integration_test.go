package identity_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func bindApplicationClient(t *testing.T, ctx context.Context, store *identity.Store, key identity.ApplicationKey, client identity.Client) identity.Principal {
	t.Helper()
	principal, err := store.ResolveEmbyForClient(ctx, key.Token, client)
	if err != nil {
		t.Fatalf("bind application client context: %v", err)
	}
	if !principal.IsApplicationKey() || principal.SessionID != key.CredentialID || principal.ApplicationKeyID != key.ID ||
		principal.ClientSessionID == principal.SessionID || principal.Client != client || principal.User.ID != "" {
		t.Fatal("application client binding changed credential identity or lost supplied metadata")
	}
	return principal
}

func TestStoreApplicationKeyClientsRemainIndependentUnderOneCredential(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Application label")
	defaults := applicationKeyPrincipal(t, ctx, store, key)
	if _, err := pool.Exec(ctx, "UPDATE application_key_clients SET last_seen_at = clock_timestamp() - interval '1 minute' WHERE id = $1", defaults.ClientSessionID); err != nil {
		t.Fatal(err)
	}
	var beforeDefault string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(c)::text FROM application_key_clients c WHERE id = $1", defaults.ClientSessionID).Scan(&beforeDefault); err != nil {
		t.Fatal(err)
	}
	alpha := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Alpha", DeviceID: "shared-device", Device: "Alpha Device", Version: "9.8.7"})
	beta := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Beta", DeviceID: "shared-device", Device: "Beta Device", Version: "1.2.3"})
	if alpha.ClientSessionID == beta.ClientSessionID || alpha.ClientSessionID == defaults.ClientSessionID || beta.ClientSessionID == defaults.ClientSessionID {
		t.Fatal("distinct client contexts collapsed to the same wire session identity")
	}
	var afterDefault string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(c)::text FROM application_key_clients c WHERE id = $1", defaults.ClientSessionID).Scan(&afterDefault); err != nil || beforeDefault != afterDefault {
		t.Fatalf("binding client metadata implicitly touched the default context: %v", err)
	}
	metadata, err := store.GetApplicationKey(ctx, admin, key.ID, true)
	if err != nil || metadata.Token != key.Token || metadata.AppName != key.AppName || metadata.Client.Name != key.AppName ||
		metadata.Client.DeviceID != key.Client.DeviceID || metadata.ReportedDeviceNumericID != 1 ||
		metadata.Client.Device != beta.Client.Device || metadata.Client.Version != beta.Client.Version || metadata.LastUsedAt == nil {
		t.Fatalf("client binding conflated key record metadata with client identity: %v", err)
	}
	if err := store.UpdateClientCapabilities(ctx, alpha, alpha.ClientSessionID, identity.ClientCapabilities{SupportsMediaControl: true}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{beta.ClientSessionID, key.CredentialID} {
		if err := store.UpdateClientCapabilities(ctx, alpha, target, identity.ClientCapabilities{}); !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Fatalf("client capabilities escaped the current context: %v", err)
		}
	}
	zero := 0
	sessions, err := store.ListClientSessions(ctx, beta, identity.ClientSessionFilter{ActiveWithinSeconds: &zero})
	if err != nil || len(sessions) != 3 {
		t.Fatalf("session list lost a default or header client context: count=%d error=%v", len(sessions), err)
	}
	for _, session := range sessions {
		if session.CredentialID != key.CredentialID || session.ApplicationKeyID != key.ID || session.UserID != "" || session.UserName != "" || session.Kind != identity.ApplicationKeyKind {
			t.Fatal("application client DTO became a fake user login")
		}
		if session.Capabilities.SupportsMediaControl != (session.SessionID == alpha.ClientSessionID) {
			t.Fatal("capability snapshots leaked between application clients")
		}
	}
	alphaClient := alpha.Client
	alphaClient.Version, alphaClient.Device = "9.9.0", "Renamed Alpha Device"
	updatedAlpha := bindApplicationClient(t, ctx, store, key, alphaClient)
	if updatedAlpha.ClientSessionID != alpha.ClientSessionID {
		t.Fatal("version or device display-name update created a new context")
	}
	refreshedBeta, err := store.RevalidateSession(ctx, beta)
	if err != nil || refreshedBeta.Client != beta.Client || refreshedBeta.ClientSessionID != beta.ClientSessionID {
		t.Fatalf("one client's update changed another context: %v", err)
	}
	refreshedDefault, err := store.ResolveEmby(ctx, key.Token)
	if err != nil || refreshedDefault.ClientSessionID != defaults.ClientSessionID || refreshedDefault.Client != defaults.Client {
		t.Fatalf("default lookup used the last client's parent metadata: %v", err)
	}
	var credentialCount, contextCount, userCount int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE kind = 'application_key'),
		(SELECT count(*) FROM application_key_clients), (SELECT count(*) FROM users)`).Scan(&credentialCount, &contextCount, &userCount); err != nil {
		t.Fatal(err)
	}
	if credentialCount != 1 || contextCount != 3 || userCount != 1 {
		t.Fatal("client contexts issued extra credentials or fake users")
	}
	_, embyAdmin := managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	if _, err := store.RevokeApplicationKey(ctx, alpha, key.ID); err != nil {
		t.Fatalf("client self-revocation failed: %v", err)
	}
	for _, principal := range []identity.Principal{defaults, alpha, beta} {
		if _, err := store.RevalidateSession(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("revoked credential left a client context authorized: %v", err)
		}
		if err := store.TouchClientSession(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("revoked credential left a client writable: %v", err)
		}
	}
	remaining, err := store.ListClientSessions(ctx, embyAdmin, identity.ClientSessionFilter{ActiveWithinSeconds: &zero})
	if err != nil || len(remaining) != 1 || remaining[0].CredentialID != embyAdmin.SessionID {
		t.Fatalf("credential revocation retained visible application clients: %v", err)
	}
}

func TestStoreApplicationKeyClientQuotaAndNormalLoginIsolation(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Bounded app")
	defaults := applicationKeyPrincipal(t, ctx, store, key)
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		SELECT 'synthetic-context-' || n, $1, 'Client ' || n, 'Device ' || n, 'Synthetic', '1.0'
		FROM generate_series(1, $2::integer) n`, key.CredentialID, identity.MaxApplicationKeyClients-1); err != nil {
		t.Fatal(err)
	}
	before := applicationKeySnapshot(t, ctx, pool)
	if _, err := store.ResolveEmbyForClient(ctx, key.Token, identity.Client{Name: "Over quota", DeviceID: "new-device"}); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("application client limit did not reject a new context: %v", err)
	}
	if after := applicationKeySnapshot(t, ctx, pool); after != before {
		t.Fatal("rejected client binding mutated state")
	}
	updated, err := store.ResolveEmbyForClient(ctx, key.Token, identity.Client{Name: "Client 1", DeviceID: "Device 1", Device: "Updated", Version: "2.0"})
	if err != nil || updated.ClientSessionID != "synthetic-context-1" {
		t.Fatalf("full key quota rejected an existing client: %v", err)
	}
	defaultAgain, err := store.ResolveEmbyForClient(ctx, key.Token, identity.Client{})
	if err != nil || defaultAgain.ClientSessionID != defaults.ClientSessionID {
		t.Fatalf("full key quota rejected its default context: %v", err)
	}
	credentials, login := managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	resolved, err := store.ResolveEmbyForClient(ctx, credentials.Token, identity.Client{Name: "Untrusted app", DeviceID: "other-device", Version: "untrusted"})
	if err != nil || resolved.ClientSessionID != "" || resolved.SessionID != login.SessionID || resolved.Client != login.Client || resolved.User.ID != login.User.ID {
		t.Fatalf("client authorization metadata replaced an ordinary login's identity: %v", err)
	}
	other := issueApplicationKey(t, ctx, store, admin, "Other credential")
	otherPrincipal := applicationKeyPrincipal(t, ctx, store, other)
	forged := defaults
	forged.ClientSessionID = otherPrincipal.ClientSessionID
	if _, err := store.RevalidateSession(ctx, forged); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("foreign application client attached to another credential: %v", err)
	}
	if _, err := store.ListApplicationKeys(ctx, forged, identity.ApplicationKeyFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("foreign client binding authorized key management: %v", err)
	}
}

func TestStoreApplicationKeyPlaybackContextRecoveryPreservesCredentialAuthority(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Media app")
	defaults := applicationKeyPrincipal(t, ctx, store, key)
	alpha := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Alpha", DeviceID: "alpha-device", Device: "Alpha", Version: "1"})
	other := issueApplicationKey(t, ctx, store, admin, "Other app")
	otherPrincipal := applicationKeyPrincipal(t, ctx, store, other)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type) VALUES ('context-library', 'Context Library', 'movies');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('context-root', 'context-library', '/synthetic/context', '/synthetic', 'context');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path)
		VALUES ('context-item', 'context-library', 'context-root', 'Context Movie', 'context movie', 'Movie', '/synthetic/context/movie.mp4', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	for index, state := range []string{"Prepared", "Stopped", "Expired"} {
		id := fmt.Sprintf("play_context_%d", index)
		if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
			(id, user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at, client_correlated)
			VALUES ($1, NULL, $2, $3, $4, 'context-item', 'source_context-item', $5, 90000000,
			clock_timestamp() - interval '1 hour', true)`, id, key.CredentialID, alpha.ClientSessionID, alpha.Client.DeviceID, state); err != nil {
			t.Fatal(err)
		}
		recovered, err := store.ResolveApplicationKeyPlaybackContext(ctx, defaults, id)
		if err != nil || recovered.ClientSessionID != alpha.ClientSessionID || recovered.SessionID != key.CredentialID || recovered.Client != alpha.Client || recovered.User.ID != "" {
			t.Fatalf("playback context recovery imposed state policy or changed ownership for %s: %v", state, err)
		}
		if _, err := store.ResolveApplicationKeyPlaybackContext(ctx, otherPrincipal, id); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("play ID crossed application credentials: %v", err)
		}
	}
	if _, err := store.ResolveApplicationKeyPlaybackContext(ctx, defaults, "missing-play"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown play ID was not not-found: %v", err)
	}
	if _, err := store.RevokeApplicationKey(ctx, admin, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveApplicationKeyPlaybackContext(ctx, defaults, "play_context_0"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("play ID authenticated a revoked credential: %v", err)
	}
}

func TestStoreApplicationKeyClientBindingRejectsRevocationWhileWaiting(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Revocation race")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := store.ResolveEmbyForClient(ctx, key.Token, identity.Client{Name: "Queued client", DeviceID: "queued-device"})
		results <- err
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", done)
	if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("queued client binding accepted revoked credential: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("queued client binding did not finish")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM application_key_clients WHERE credential_id = $1", key.CredentialID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("revoked binding partially created a client context: count=%d error=%v", count, err)
	}
}

func TestStoreApplicationKeyConcurrentClientBindingRespectsQuota(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Quota race")
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		SELECT 'quota-context-' || n, $1, 'Client ' || n, 'Device ' || n, 'Synthetic', '1.0'
		FROM generate_series(1, $2::integer) n`, key.CredentialID, identity.MaxApplicationKeyClients-2); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, name := range []string{"First claimant", "Second claimant"} {
		go func(name string) {
			<-start
			_, err := store.ResolveEmbyForClient(ctx, key.Token, identity.Client{Name: name, DeviceID: "last-slot"})
			results <- err
		}(name)
	}
	close(start)
	successes, rejected := 0, 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else if errors.Is(err, identity.ErrInvalidInput) {
				rejected++
			} else {
				t.Fatalf("unexpected concurrent client binding failure: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent application clients did not finish")
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("last client slot admitted %d and rejected %d callers", successes, rejected)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM application_key_clients WHERE credential_id = $1", key.CredentialID).Scan(&count); err != nil || count != identity.MaxApplicationKeyClients {
		t.Fatalf("concurrent application clients exceeded quota: count=%d error=%v", count, err)
	}
}
