package identity_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"golang.org/x/crypto/bcrypt"
)

func TestStoreLocalPasswordAuthenticationIsolationThrottleAndRevocation(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Local password owner", "normal-password", false)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := managedLogin(t, ctx, store, viewer, "normal-password", "emby")
	local := "separate-local-password"
	result, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{
		Revision: 1, EnableLocalPassword: true, LocalPassword: &local,
	})
	if err != nil || !result.Credentials.HasLocalPassword || !result.Credentials.EnableLocalPassword || result.Credentials.Revision != 2 {
		t.Fatalf("configure separate local password: %v", err)
	}
	if _, err := store.Resolve(ctx, old.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("local credential mutation retained an existing login")
	}
	var stored, main string
	if err := pool.QueryRow(ctx, "SELECT local_password_hash,password_hash FROM users WHERE id=$1", viewer.ID).Scan(&stored, &main); err != nil {
		t.Fatal(err)
	}
	if stored == local || stored == main || bcrypt.CompareHashAndPassword([]byte(stored), []byte(local)) != nil || bcrypt.CompareHashAndPassword([]byte(main), []byte(local)) == nil {
		t.Fatal("local and main credentials are not independent protected hashes")
	}
	client := identity.Client{Name: "Local credential test", DeviceID: "local-device"}
	for _, peer := range []string{"", "192.0.2.20", "8.8.8.8"} {
		if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", peer); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("nonlocal password login accepted for %q: %v", peer, err)
		}
	}
	for _, password := range []string{"", "wrong"} {
		if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, password, client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatal("empty or incorrect local password was accepted")
		}
	}
	issued, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", "192.168.10.20")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := store.ResolveWithPeer(ctx, issued.Token, "emby", "192.168.10.20")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveWithPeer(ctx, issued.Token, "emby", "192.0.2.20"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("local-auth token was usable remotely")
	}
	principal.PeerIP = "192.0.2.20"
	if _, err := store.RevalidateSession(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("remote revalidation admitted a local-auth token")
	}
	if _, err := store.AuthenticateWithPeer(ctx, admin.Name, local, client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("another account accepted the local password")
	}
	var attempts sync.WaitGroup
	for index := 0; index < 8; index++ {
		attempts.Add(1)
		go func() {
			defer attempts.Done()
			if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "wrong-again", client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Errorf("failed local attempt returned %v", err)
			}
		}()
	}
	attempts.Wait()
	var failures int
	var blocked bool
	if err := pool.QueryRow(ctx, "SELECT local_password_failures,local_password_blocked_until>clock_timestamp() FROM users WHERE id=$1", viewer.ID).Scan(&failures, &blocked); err != nil {
		t.Fatal(err)
	}
	if failures != 5 || !blocked {
		t.Fatal("concurrent attempts failed to enforce the durable five-attempt limit")
	}
	store = identity.New(pool)
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("a restart bypassed the local-password cooldown")
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "normal-password", client, "emby", "192.0.2.20"); err != nil {
		t.Fatal("local-password throttling blocked normal-password recovery")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET local_password_blocked_until=clock_timestamp()-interval '1 second' WHERE id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", "192.168.10.20"); err != nil {
		t.Fatal(err)
	}
	current := readManagedUser(t, ctx, store, viewer.ID)
	if _, err := store.ResetManagedUserPassword(ctx, actor, viewer.ID, current.Revision, "replacement-main-password"); err != nil {
		t.Fatal(err)
	}
	status, err := store.GetLocalCredentials(ctx, actor, viewer.ID)
	if err != nil || status.HasLocalPassword || status.HasProfilePin || status.EnableLocalPassword {
		t.Fatal("main-password reset retained shortcut credentials")
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("old local credential survived main-password reset")
	}
	if _, err := store.ResolveWithPeer(ctx, issued.Token, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("reset retained the local-auth login")
	}
}

func TestStoreLocalCredentialCASAuthorityPolicyAndClear(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Local credential controls", "normal-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Other credential controls", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, outsider := managedLogin(t, ctx, store, other, "other-password", "emby")
	local := "local-control-password"
	input := identity.LocalCredentialsUpdate{Revision: 1, EnableLocalPassword: true, LocalPassword: &local}
	if _, err := store.UpdateLocalCredentials(ctx, outsider, viewer.ID, input); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatal("another member changed credentials")
	}
	configured, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, input); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Fatal("stale credential revision was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableAllDevices":false,"EnabledDevices":["allowed"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, identity.Client{DeviceID: "denied"}, "emby", "10.1.2.3"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("local credential bypassed the current device policy")
	}
	empty := ""
	cleared, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{Revision: configured.Credentials.Revision, LocalPassword: &empty})
	if err != nil || cleared.Credentials.HasLocalPassword || cleared.Credentials.EnableLocalPassword {
		t.Fatal("explicit empty local password did not clear it")
	}
	if _, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{Revision: cleared.Credentials.Revision, EnableLocalPassword: true}); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatal("enabled local authentication without a credential")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{Revision: cleared.Credentials.Revision, LocalPassword: &local}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("stale administrator authority changed credentials")
	}
}

func TestStoreLocalCredentialMutationRechecksRevocationAfterCredentialWait(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Waiting local credential target", "normal-password", false)
	if err != nil {
		t.Fatal(err)
	}
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id=$1 FOR UPDATE", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var resultErr error
	go func() {
		defer close(done)
		local := "waiting-local-password"
		_, resultErr = store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{Revision: 1, EnableLocalPassword: true, LocalPassword: &local})
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions", done)
	if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("credential mutation did not finish after revocation")
	}
	if !errors.Is(resultErr, identity.ErrUnauthorized) {
		t.Fatal("a writer authorized before its lock wait retained revoked authority")
	}
	var present bool
	var revision int64
	if err := pool.QueryRow(ctx, "SELECT local_password_hash IS NOT NULL,local_credentials_revision FROM users WHERE id=$1", viewer.ID).Scan(&present, &revision); err != nil {
		t.Fatal(err)
	}
	if present || revision != 1 {
		t.Fatal("rejected credential mutation left persisted state")
	}
}

func TestStoreLocalPasswordIssuanceRejectsCredentialClearedDuringAccountWait(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Waiting local login", "normal-password", false)
	if err != nil {
		t.Fatal(err)
	}
	local := "old-local-password"
	if _, err := store.UpdateLocalCredentials(ctx, actor, viewer.ID, identity.LocalCredentialsUpdate{Revision: 1, EnableLocalPassword: true, LocalPassword: &local}); err != nil {
		t.Fatal(err)
	}
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", viewer.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var loginErr error
	go func() {
		defer close(done)
		_, loginErr = store.AuthenticateWithPeer(ctx, viewer.Name, local, identity.Client{DeviceID: "waiting-local-device"}, "emby", "192.168.10.20")
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "local_password_hash=$2", done)
	if _, err := blocker.Exec(ctx, `UPDATE users SET local_password_hash=NULL,configuration=configuration || '{"EnableLocalPassword":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("local login did not finish after the account change")
	}
	if !errors.Is(loginErr, identity.ErrInvalidCredentials) {
		t.Fatal("a stale local hash issued a login after the credential was cleared")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL", viewer.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("stale local login left an active session")
	}
}
