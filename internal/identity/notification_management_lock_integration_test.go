package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationManagementLockPreservesDatabaseFailure(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, nativeActor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Notification Lock Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, embyActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	blocker, _ := managedSessionBlocker(t, ctx, pool)
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
		t.Fatal(err)
	}
	before := managedDeletionSnapshot(t, ctx, pool)
	for _, fixture := range []struct {
		name   string
		actor  identity.Principal
		native bool
	}{
		{"native administrator", nativeActor, true},
		{"personal Emby login", embyActor, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='100ms'"); err != nil {
				t.Fatal(err)
			}
			err = identity.LockNotificationMutation(ctx, tx, fixture.actor, fixture.native)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "57014" ||
				errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrClientSessionForbidden) ||
				!strings.Contains(err.Error(), "lock notification management") {
				t.Fatalf("management statement timeout was not preserved as a database failure: %v", err)
			}
		})
	}
	t.Run("request cancellation", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout=0"); err != nil {
			t.Fatal(err)
		}
		callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		err = identity.LockNotificationMutation(callCtx, tx, nativeActor, true)
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("management lock cancellation became an authentication failure: %v", err)
		}
	})
	if managedDeletionSnapshot(t, ctx, pool) != before {
		t.Fatal("failed management lock admission changed account, session or audit state")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		actor  identity.Principal
		native bool
	}{
		{nativeActor, true},
		{embyActor, false},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := identity.LockNotificationMutation(ctx, tx, fixture.actor, fixture.native); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("released management lock rejected valid credentials: %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=ANY($1::text[])",
		[]string{nativeActor.SessionID, embyActor.SessionID}); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		actor  identity.Principal
		native bool
	}{
		{nativeActor, true},
		{embyActor, false},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = identity.LockNotificationMutation(ctx, tx, fixture.actor, fixture.native)
		_ = tx.Rollback(context.Background())
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("revoked credentials lost their authentication error: %v", err)
		}
	}
}
