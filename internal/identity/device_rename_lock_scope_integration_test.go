package identity_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

type deviceRenameUserMutation struct {
	mutation identity.ManagedUserMutation
	err      error
}

func startDeviceRenameUserMutation(ctx context.Context, store *identity.Store, actor identity.Principal, user identity.ManagedUser, name string) (<-chan deviceRenameUserMutation, <-chan struct{}) {
	input := managedUpdate(user)
	input.Name = name
	results := make(chan deviceRenameUserMutation, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		mutation, err := store.UpdateManagedUser(ctx, actor, user.User.ID, input)
		results <- deviceRenameUserMutation{mutation: mutation, err: err}
	}()
	return results, done
}

// Register every worker before observing a wait. Cancellation and rollback must
// release failed assertions before the pool and its schema are cleaned up.
func trackDeviceRenameWorkers(t *testing.T, cancel context.CancelFunc, blocker pgx.Tx) func(<-chan struct{}) {
	t.Helper()
	var workers []<-chan struct{}
	t.Cleanup(func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := blocker.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("release device rename blocker during cleanup: %v", err)
		}
		for _, done := range workers {
			select {
			case <-done:
			case <-cleanupCtx.Done():
				t.Error("device rename concurrency worker did not exit before cleanup")
				return
			}
		}
	})
	return func(done <-chan struct{}) {
		workers = append(workers, done)
	}
}

func awaitDeviceRenameWorker[T any](t *testing.T, ctx context.Context, results <-chan T, done <-chan struct{}) T {
	t.Helper()
	result := awaitDeviceOperation(t, ctx, results)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("device rename concurrency worker returned without exiting before its deadline")
	}
	return result
}

func assertDeviceRenameCommitted(t *testing.T, ctx context.Context, store *identity.Store, actor identity.Principal, before identity.ManagedDevice, name string, result concurrentDeviceRename) {
	t.Helper()
	if result.err != nil {
		t.Fatalf("rename an independent device: %v", result.err)
	}
	want := before
	want.Name, want.CustomName = name, &name
	want.Revision++
	if !reflect.DeepEqual(result.device, want) {
		t.Fatal("device rename did not change only its custom name and revision")
	}
	if current := readDevice(t, ctx, store, actor, before.ID); !reflect.DeepEqual(current, result.device) {
		t.Fatal("device rename result differs from its committed device")
	}
}

func TestStoreDeviceRenameWaitKeepsOtherDevicesIndependent(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	var devices [2]identity.ManagedDevice
	for index, reportedID := range []string{"blocked-rename-device", "independent-rename-device"} {
		login, _ := deviceLogin(t, testCtx, store, admin, "administrator-password",
			identity.Client{Name: "Independent Rename Player", DeviceID: reportedID, Device: "Reported Screen"}, "emby", "192.0.2.201")
		devices[index] = readDevice(t, testCtx, store, actor, registeredDeviceForSession(t, testCtx, pool, login.SessionID))
	}
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	if _, err := blocker.Exec(ctx, "SELECT id FROM devices WHERE id = $1 FOR UPDATE", devices[0].ID); err != nil {
		t.Fatal(err)
	}
	firstResults, firstDone := startDeviceRename(ctx, store, actor, devices[0], "Blocked Rename")
	track(firstDone)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT revision, custom_name, deleted_at FROM devices", firstDone)

	// The same actor can share its account and credential locks across edits.
	// The second result must commit while the first target remains locked.
	independentCtx, independentCancel := context.WithTimeout(ctx, 3*time.Second)
	defer independentCancel()
	secondResults, secondDone := startDeviceRename(independentCtx, store, actor, devices[1], "Independent Rename")
	track(secondDone)
	second := awaitDeviceRenameWorker(t, independentCtx, secondResults, secondDone)
	assertDeviceRenameCommitted(t, ctx, store, actor, devices[1], "Independent Rename", second)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT revision, custom_name, deleted_at FROM devices", firstDone)
	if current := readDevice(t, ctx, store, actor, devices[0].ID); !reflect.DeepEqual(current, devices[0]) {
		t.Fatal("blocked device changed before its row lock was released")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	first := awaitDeviceRenameWorker(t, ctx, firstResults, firstDone)
	assertDeviceRenameCommitted(t, ctx, store, actor, devices[0], "Blocked Rename", first)
}

func TestStoreDeviceRenameWaitAllowsUnrelatedUserManagement(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	otherAdmin, err := store.CreateUser(testCtx, "Independent User Administrator", "other-password", true)
	if err != nil {
		t.Fatal(err)
	}
	_, otherActor := managedLogin(t, testCtx, store, otherAdmin, "other-password", "admin")
	target, err := store.CreateUser(testCtx, "Unrelated Managed Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	beforeUser := readManagedUser(t, testCtx, store, target.ID)
	deviceLogin(t, testCtx, store, admin, "administrator-password",
		identity.Client{Name: "Blocked Rename Player", DeviceID: "rename-user-management", Device: "Reported Screen"}, "emby", "192.0.2.202")
	device := onlyDevice(t, testCtx, store, actor)
	actorBefore := managedSnapshot(t, testCtx, pool, actor.User.ID)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	if _, err := blocker.Exec(ctx, "SELECT id FROM devices WHERE id = $1 FOR UPDATE", device.ID); err != nil {
		t.Fatal(err)
	}
	renameResults, renameDone := startDeviceRename(ctx, store, actor, device, "Renamed After User Management")
	track(renameDone)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT revision, custom_name, deleted_at FROM devices", renameDone)

	// A different administrator and target avoid legitimate account-lock
	// conflicts, so only an unnecessary management lock could serialize them.
	managementCtx, managementCancel := context.WithTimeout(ctx, 3*time.Second)
	defer managementCancel()
	userResults, userDone := startDeviceRenameUserMutation(managementCtx, store, otherActor, beforeUser, "Independently Updated Viewer")
	track(userDone)
	updated := awaitDeviceRenameWorker(t, managementCtx, userResults, userDone)
	if updated.err != nil || updated.mutation.User.Revision != beforeUser.Revision+1 ||
		updated.mutation.User.User.Name != "Independently Updated Viewer" || updated.mutation.CurrentSessionRevoked {
		t.Fatalf("unrelated user management waited for a device rename or returned an invalid result: %v", updated.err)
	}
	if current := readManagedUser(t, ctx, store, target.ID); !reflect.DeepEqual(current, updated.mutation.User) {
		t.Fatal("unrelated user management returned without committing its account update")
	}
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT revision, custom_name, deleted_at FROM devices", renameDone)
	assertManagedUnchanged(t, ctx, pool, actor.User.ID, actorBefore)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rename := awaitDeviceRenameWorker(t, ctx, renameResults, renameDone)
	assertDeviceRenameCommitted(t, ctx, store, actor, device, "Renamed After User Management", rename)
}

func TestStoreNativeDeviceRenameSkipsHeldManagementLock(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	target, err := store.CreateUser(testCtx, "Queued Management Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	beforeUser := readManagedUser(t, testCtx, store, target.ID)
	deviceLogin(t, testCtx, store, admin, "administrator-password",
		identity.Client{Name: "Direct Rename Player", DeviceID: "rename-held-management-lock", Device: "Reported Screen"}, "emby", "192.0.2.203")
	device := onlyDevice(t, testCtx, store, actor)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
		t.Fatal(err)
	}
	userResults, userDone := startDeviceRenameUserMutation(ctx, store, actor, beforeUser, "Updated After Management Release")
	track(userDone)
	// A real managed mutation establishes that this lock still protects user
	// management before the native rename takes its direct ordinary entry.
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "pg_advisory_xact_lock", userDone)
	renameCtx, renameCancel := context.WithTimeout(ctx, 3*time.Second)
	defer renameCancel()
	renameResults, renameDone := startDeviceRename(renameCtx, store, actor, device, "Renamed With Management Locked")
	track(renameDone)
	rename := awaitDeviceRenameWorker(t, renameCtx, renameResults, renameDone)
	assertDeviceRenameCommitted(t, ctx, store, actor, device, "Renamed With Management Locked", rename)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "pg_advisory_xact_lock", userDone)
	if current := readManagedUser(t, ctx, store, target.ID); !reflect.DeepEqual(current, beforeUser) {
		t.Fatal("user management escaped the held management lock")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	updated := awaitDeviceRenameWorker(t, ctx, userResults, userDone)
	if updated.err != nil || updated.mutation.User.Revision != beforeUser.Revision+1 ||
		updated.mutation.User.User.Name != "Updated After Management Release" {
		t.Fatalf("managed mutation did not resume after releasing its management lock: %v", updated.err)
	}
}

func TestStoreDeviceRenameRevalidatesExpiryAfterDeviceWait(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	deviceLogin(t, testCtx, store, admin, "administrator-password",
		identity.Client{Name: "Expiring Rename Player", DeviceID: "rename-device-wait-expiry", Device: "Reported Screen"}, "emby", "192.0.2.204")
	device := onlyDevice(t, testCtx, store, actor)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	if _, err := blocker.Exec(ctx, "SELECT id FROM devices WHERE id = $1 FOR UPDATE", device.ID); err != nil {
		t.Fatal(err)
	}
	// Keep the principal's original expiry so the database must reauthorize
	// after the target wait instead of trusting the request's snapshot.
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, "activity_entries")
	results, done := startDeviceRename(ctx, store, actor, device, "Expired Rename")
	track(done)
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT revision, custom_name, deleted_at FROM devices", done)
	waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitDeviceRenameWorker(t, ctx, results, done)
	if !errors.Is(result.err, identity.ErrUnauthorized) || result.device != (identity.ManagedDevice{}) {
		t.Fatalf("device rename accepted an actor expired during its device wait: %v", result.err)
	}
	if deviceManagementSnapshot(t, testCtx, pool) != before ||
		deviceSessionAuditSnapshot(t, testCtx, pool, "activity_entries") != auditBefore {
		t.Fatal("expired device rename committed device, credential, or audit changes")
	}
}

func TestStoreDeviceRenameObservesConcurrentDeletion(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, deletionActor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	otherAdmin, err := store.CreateUser(testCtx, "Concurrent Rename Administrator", "other-password", true)
	if err != nil {
		t.Fatal(err)
	}
	_, renameActor := managedLogin(t, testCtx, store, otherAdmin, "other-password", "admin")
	owner, err := store.CreateUser(testCtx, "Concurrent Removal Device Owner", "owner-password", false)
	if err != nil {
		t.Fatal(err)
	}
	login, _ := deviceLogin(t, testCtx, store, owner, "owner-password",
		identity.Client{Name: "Removed Rename Player", DeviceID: "rename-concurrent-removal", Device: "Reported Screen"}, "emby", "192.0.2.205")
	device := onlyDevice(t, testCtx, store, renameActor)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, login.SessionID)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	removalResults, removalDone := startDeviceRemoval(ctx, store, deletionActor, device)
	track(removalDone)
	removalPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", removalDone)

	// Deletion has written the tombstone and credential revocation. The rename
	// actor belongs to neither its actor credential nor its target login set.
	renameResults, renameDone := startDeviceRename(ctx, store, renameActor, device, "Name After Removal")
	track(renameDone)
	waitManagedBlockedQuery(t, ctx, pool, removalPID, "SELECT revision, custom_name, deleted_at FROM devices", renameDone)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	removal := awaitDeviceRenameWorker(t, ctx, removalResults, removalDone)
	rename := awaitDeviceRenameWorker(t, ctx, renameResults, renameDone)
	assertConcurrentDeviceRemoval(t, removal, device.ID, login.SessionID)
	if !errors.Is(rename.err, identity.ErrDeviceNotFound) || rename.device != (identity.ManagedDevice{}) {
		t.Fatalf("queued rename did not preserve the committed device tombstone: %v", rename.err)
	}
	var revision int64
	var customName *string
	var deleted bool
	if err := pool.QueryRow(testCtx, "SELECT revision, custom_name, deleted_at IS NOT NULL FROM devices WHERE id = $1", device.ID).
		Scan(&revision, &customName, &deleted); err != nil {
		t.Fatal(err)
	}
	if revision != device.Revision+1 || customName != nil || !deleted {
		t.Fatal("rejected rename changed the removed device generation")
	}
	resourceID := strconv.FormatInt(device.ID, 10)
	if len(readDeviceSessionAudit(t, testCtx, pool, "device.updated", resourceID)) != 0 ||
		len(readDeviceSessionAudit(t, testCtx, pool, "device.removed", resourceID)) != 1 {
		t.Fatal("concurrent device deletion did not retain exactly its committed removal audit")
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 1)
	assertManagedTokenRevoked(t, testCtx, store, login, "emby")
}

func TestStoreDeviceRenameRevalidatesConcurrentActorRevocation(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	credentials, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	_, observer := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	deviceLogin(t, testCtx, store, admin, "administrator-password",
		identity.Client{Name: "Revoked Rename Player", DeviceID: "rename-concurrent-revocation", Device: "Reported Screen"}, "emby", "192.0.2.206")
	device := onlyDevice(t, testCtx, store, observer)
	before := managedSessionStableSnapshot(t, testCtx, pool, admin.ID, actor.SessionID)
	devicesBefore := deviceSessionAuditSnapshot(t, testCtx, pool, "devices")
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, actor.SessionID)
	track := trackDeviceRenameWorkers(t, cancel, blocker)
	revokeResults := make(chan error, 1)
	revokeDone := make(chan struct{})
	go func() {
		defer close(revokeDone)
		revokeResults <- store.Revoke(ctx, credentials.Token)
	}()
	track(revokeDone)
	revokePID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", revokeDone)
	renameResults, renameDone := startDeviceRename(ctx, store, actor, device, "Revoked Actor Rename")
	track(renameDone)
	// Initial authorization sees the still-committed credential. Its shared
	// lock must wait for the real logout and then recheck the revoked state.
	waitManagedBlockedQuery(t, ctx, pool, revokePID, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", renameDone)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := awaitDeviceRenameWorker(t, ctx, revokeResults, revokeDone); err != nil {
		t.Fatalf("concurrent actor revocation failed: %v", err)
	}
	rename := awaitDeviceRenameWorker(t, ctx, renameResults, renameDone)
	if !errors.Is(rename.err, identity.ErrUnauthorized) || rename.device != (identity.ManagedDevice{}) {
		t.Fatalf("device rename accepted an actor revoked during its credential wait: %v", rename.err)
	}
	if managedSessionStableSnapshot(t, testCtx, pool, admin.ID, actor.SessionID) != before ||
		deviceSessionAuditSnapshot(t, testCtx, pool, "devices") != devicesBefore {
		t.Fatal("rejected rename changed device state or unrelated authentication metadata")
	}
	if len(readDeviceSessionAudit(t, testCtx, pool, "device.updated", strconv.FormatInt(device.ID, 10))) != 0 ||
		len(readDeviceSessionAudit(t, testCtx, pool, "session.revoked", actor.SessionID)) != 1 {
		t.Fatal("actor revocation retained an audit for the rejected device rename")
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 1)
	assertManagedTokenRevoked(t, testCtx, store, credentials, "admin")
	if current := readDevice(t, testCtx, store, observer, device.ID); !reflect.DeepEqual(current, device) {
		t.Fatal("rejected rename changed the device visible to an authorized observer")
	}
}
