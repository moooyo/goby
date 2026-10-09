package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func TestStoreDeviceDeletionAllowsSiblingAuthorizationWhileWaiting(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Device Shared Account", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := deviceLogin(t, ctx, store, viewer, "viewer-password",
		identity.Client{DeviceID: "deleted-device"}, "emby", "192.0.2.80")
	device := onlyDevice(t, ctx, store, actor)
	siblingCredentials, targetSibling := deviceLogin(t, ctx, store, viewer, "viewer-password",
		identity.Client{DeviceID: "retained-device"}, "emby", "192.0.2.81")
	_, actorSibling := deviceLogin(t, ctx, store, admin, "administrator-password",
		identity.Client{DeviceID: "administrator-other-device"}, "emby", "192.0.2.82")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	var locked string
	if err := blocker.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", target.SessionID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results, done := deleteDeviceForAuthorizationAsync(operationCtx, store, actor, device, true)
	waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "SELECT id, kind, device_registry_id FROM sessions", done)
	for _, sibling := range []identity.Principal{actorSibling, targetSibling} {
		read, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer read.Rollback(context.Background())
		// Playback takes these same account and credential locks before its
		// current authority read. Neither sibling belongs to the deleted device.
		if err := read.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE NOWAIT", sibling.User.ID).Scan(&locked); err != nil {
			t.Fatalf("device deletion blocked sibling account authority: %v", err)
		}
		if err := read.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE NOWAIT", sibling.SessionID).Scan(&locked); err != nil {
			t.Fatalf("device deletion blocked sibling credential authority: %v", err)
		}
		if _, err := identity.RevalidateSessionInTransaction(ctx, read, sibling); err != nil {
			t.Fatalf("sibling authorization failed during device deletion: %v", err)
		}
		if err := read.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		mutation, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer mutation.Rollback(context.Background())
		err = mutation.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE NOWAIT", sibling.User.ID).Scan(&locked)
		var conflict *pgconn.PgError
		if !errors.As(err, &conflict) || conflict.Code != "55P03" {
			t.Fatalf("device deletion did not exclude an account mutation: %v", err)
		}
		if err := mutation.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitDeviceDeletionAuthorization(t, operationCtx, results)
	if result.err != nil || result.deletion.ID != device.ID || result.deletion.RevokedLoginCount != 1 ||
		len(result.deletion.RevokedSessionIDs) != 1 || result.deletion.RevokedSessionIDs[0] != target.SessionID {
		t.Fatalf("device deletion did not retain its exact credential scope: %v", result.err)
	}
	assertManagedTokenRevoked(t, ctx, store, target, "emby")
	if _, err := store.Resolve(ctx, siblingCredentials.Token, "emby"); err != nil {
		t.Fatalf("device deletion revoked an unrelated device: %v", err)
	}
}
