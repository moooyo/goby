package identity_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreLoginPolicyAppliesBeforeIssuanceAndToExistingSessions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Restricted Login Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{Name: "Policy Client", DeviceID: "allowed-device"}
	credentials, err := store.AuthenticateWithPeer(ctx, viewer.Name, "viewer-password", client, "emby", "192.168.1.2")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := store.ResolveWithPeer(ctx, credentials.Token, "emby", "192.168.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":["allowed-device"],"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "viewer-password", identity.Client{DeviceID: "denied-device"}, "emby", "192.168.1.2"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("unlisted device issued a login: %v", err)
	}
	if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "viewer-password", client, "emby", "8.8.8.8"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("remote peer issued a login: %v", err)
	}
	if _, err := store.ResolveWithPeer(ctx, credentials.Token, "emby", "8.8.8.8"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("current remote restriction ignored: %v", err)
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("missing trusted peer bypassed restriction: %v", err)
	}
	if _, err := store.RevalidateSession(ctx, previous); err != nil {
		t.Fatalf("known local connection was rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnabledDevices":[]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveWithPeer(ctx, credentials.Token, "emby", "192.168.1.2"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("existing session ignored revoked device access: %v", err)
	}
	if _, err := store.RevalidateSession(ctx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("continuous session ignored revoked device access: %v", err)
	}
	if err := store.TouchClientSession(ctx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("locked mutation used stale policy: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllDevices":false,"EnableRemoteAccess":false,"AccessSchedules":null}'::jsonb WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "admin"); err != nil {
		t.Fatalf("Emby restrictions locked out native recovery: %v", err)
	}
	var deniedDevices int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM devices WHERE reported_device_id='denied-device'").Scan(&deniedDevices); err != nil || deniedDevices != 0 {
		t.Fatalf("rejected login registered a device: count=%d error=%v", deniedDevices, err)
	}
}

func TestStoreUserPreferencePolicyUsesCurrentDatabaseState(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Preference Policy Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUserSettings(ctx, actor, viewer.ID); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("preference read used stale permission: %v", err)
	}
	value := "eng"
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{"language": &value}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("preference write used stale permission: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_settings WHERE user_id=$1", viewer.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rejected preference write changed storage: count=%d error=%v", rows, err)
	}
}

func TestStoreSelfPasswordChangeRequiresCurrentPasswordAndRevokesAllSessions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Password Owner", "old-password", false)
	if err != nil {
		t.Fatal(err)
	}
	first, actor := managedLogin(t, ctx, store, viewer, "old-password", "emby")
	second, _ := managedLogin(t, ctx, store, viewer, "old-password", "emby")
	before := managedSnapshot(t, ctx, pool, viewer.ID)
	if _, err := store.ChangeUserPassword(ctx, actor, viewer.ID, "incorrect-password", "new-password"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("incorrect old password accepted: %v", err)
	}
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	result, err := store.ChangeUserPassword(ctx, actor, viewer.ID, "old-password", "new-password")
	if err != nil {
		t.Fatal(err)
	}
	if !result.CurrentSessionRevoked || result.User.Revision != 2 || !slices.Contains(result.RevokedSessionIDs, first.SessionID) || !slices.Contains(result.RevokedSessionIDs, second.SessionID) {
		t.Fatal("password change did not report every retired credential")
	}
	assertManagedTokenRevoked(t, ctx, store, first, "emby")
	assertManagedTokenRevoked(t, ctx, store, second, "emby")
	if _, err := store.Authenticate(ctx, viewer.Name, "old-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("old password still authenticated: %v", err)
	}
	managedLogin(t, ctx, store, viewer, "new-password", "emby")
	if _, err := store.ChangeUserPassword(ctx, actor, viewer.ID, "new-password", "another-password"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked owner session changed password: %v", err)
	}
}

func TestStoreEmbyAdministratorUsesSharedManagementAndLastAdminGuard(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	viewer, err := store.CreateManagedUser(ctx, actor, "Emby Managed Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	input := managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
	input.Policy.EnableUserPreferenceAccess = false
	result, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input)
	if err != nil || result.User.Policy.EnableUserPreferenceAccess {
		t.Fatalf("Emby administrator could not update policy: %v", err)
	}
	if _, err := store.DeleteManagedUser(ctx, actor, admin.ID, 1); !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("Emby deletion bypassed final administrator guard: %v", err)
	}
	if _, err := store.ChangeUserPassword(ctx, actor, admin.ID, "administrator-password", ""); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("self password change removed last administrator password: %v", err)
	}
	if _, err := store.DeleteManagedUser(ctx, actor, viewer.ID, result.User.Revision); err != nil {
		t.Fatalf("Emby administrator could not delete managed user: %v", err)
	}
}
