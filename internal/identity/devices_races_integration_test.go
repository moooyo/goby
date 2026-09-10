package identity_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type deviceDeletionAuthorizationOutcome struct {
	deletion identity.DeviceDeletion
	err      error
}

func deleteDeviceForAuthorizationAsync(ctx context.Context, store *identity.Store, actor identity.Principal, device identity.ManagedDevice, native bool) (<-chan deviceDeletionAuthorizationOutcome, <-chan struct{}) {
	results := make(chan deviceDeletionAuthorizationOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var deletion identity.DeviceDeletion
		var err error
		if native {
			deletion, err = store.DeleteManagedDevice(ctx, actor, device.ID, device.Revision)
		} else {
			deletion, err = store.DeleteEmbyDevice(ctx, actor, strconv.FormatInt(device.ID, 10))
		}
		results <- deviceDeletionAuthorizationOutcome{deletion: deletion, err: err}
	}()
	return results, done
}

func awaitDeviceDeletionAuthorization(t *testing.T, ctx context.Context, results <-chan deviceDeletionAuthorizationOutcome) deviceDeletionAuthorizationOutcome {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("device deletion did not finish before its authorization deadline")
		return deviceDeletionAuthorizationOutcome{}
	}
}

func TestStoreDeviceDeletionRevalidatesRevokedActorAfterRegistryWait(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	actorCredentials, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	_, observer := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(testCtx, "Device Revocation Target", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{Name: "Registry Barrier Player", DeviceID: "revoked-manager-target", Device: "Target Television"}
	credentials, principal := deviceLogin(t, testCtx, store, viewer, "viewer-password", client, "emby", "192.0.2.110")
	sibling, _ := deviceLogin(t, testCtx, store, viewer, "viewer-password", client, "emby", "192.0.2.111")
	device := onlyDevice(t, testCtx, store, observer)
	seedDevicePlaybackHistory(t, testCtx, pool, principal)
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	const registrationNamespace int32 = 1735352915
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, hashtext($2))", registrationNamespace, client.DeviceID); err != nil {
		t.Fatal(err)
	}
	results, done := deleteDeviceForAuthorizationAsync(ctx, store, actor, device, true)
	// This proves that initial authorization passed while the actor's account
	// and credential remain unlocked, allowing a real logout to commit first.
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "pg_advisory_xact_lock($1::integer, hashtext($2))", done)
	if err := store.Revoke(ctx, actorCredentials.Token); err != nil {
		t.Fatal(err)
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	historyBefore := deviceRetainedHistory(t, ctx, pool, []string{})
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitDeviceDeletionAuthorization(t, ctx, results)
	if !errors.Is(result.err, identity.ErrUnauthorized) || !reflect.DeepEqual(result.deletion, identity.DeviceDeletion{}) {
		t.Errorf("actor revoked during registry wait returned deletion state or an unexpected error: %v", result.err)
	}
	if deviceManagementSnapshot(t, testCtx, pool) != before || deviceRetainedHistory(t, testCtx, pool, []string{}) != historyBefore {
		t.Error("rejected device deletion changed registry, credential, or playback history after the actor's logout")
	}
	assertManagedTokenRevoked(t, testCtx, store, actorCredentials, "admin")
	for _, credential := range []identity.Credentials{credentials, sibling} {
		if _, err := store.Resolve(testCtx, credential.Token, "emby"); err != nil {
			t.Errorf("rejected device deletion invalidated a target login: %v", err)
		}
	}
}

func TestStoreDeviceDeletionExpiryAfterCredentialWriteRollsBack(t *testing.T) {
	for _, targetKind := range []string{"other", "self"} {
		t.Run(targetKind, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, observer := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			viewer, err := store.CreateUser(testCtx, "Device Expiration Target", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			client := identity.Client{Name: "Expiration Barrier Player", DeviceID: "expired-manager-target", Device: "Shared Television"}
			targetCredentials, targetPrincipal := deviceLogin(t, testCtx, store, viewer, "viewer-password", client, "emby", "192.0.2.112")
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			gateSessionID := targetCredentials.SessionID
			if targetKind == "self" {
				_, actor = deviceLogin(t, testCtx, store, admin, "administrator-password", client, "emby", "192.0.2.113")
				gateSessionID = actor.SessionID
			}
			device := onlyDevice(t, testCtx, store, observer)
			seedDevicePlaybackHistory(t, testCtx, pool, targetPrincipal)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, gateSessionID)
			// Only fixture timing changes here. The principal deliberately keeps
			// its original expiration so the database must decide commit authority.
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			before := deviceManagementSnapshot(t, ctx, pool)
			historyBefore := deviceRetainedHistory(t, ctx, pool, []string{})
			results, done := deleteDeviceForAuthorizationAsync(ctx, store, actor, device, targetKind == "other")
			// The AFTER UPDATE gate proves both the device tombstone and a real
			// credential revocation have been written inside the transaction.
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := awaitDeviceDeletionAuthorization(t, ctx, results)
			if !errors.Is(result.err, identity.ErrUnauthorized) || !reflect.DeepEqual(result.deletion, identity.DeviceDeletion{}) {
				t.Errorf("actor expired after credential write returned deletion state or an unexpected error: %v", result.err)
			}
			if deviceManagementSnapshot(t, testCtx, pool) != before || deviceRetainedHistory(t, testCtx, pool, []string{}) != historyBefore {
				t.Error("expired actor committed device removal or altered credential and playback history")
			}
			assertManagedSessionUpdateCount(t, testCtx, pool, 0)
			if _, err := store.Resolve(testCtx, targetCredentials.Token, "emby"); err != nil {
				t.Errorf("rolled-back device removal invalidated the unexpired target login: %v", err)
			}
			if current := readDevice(t, testCtx, store, observer, device.ID); current.Revision != device.Revision {
				t.Error("rolled-back device removal advanced its registry revision")
			}
		})
	}
}
