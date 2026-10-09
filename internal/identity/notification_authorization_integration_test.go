//go:build linux

package identity_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/notifications"
)

type notificationAuthorizationProber struct{}

func (notificationAuthorizationProber) ProbeFile(context.Context, *os.File) (media.Info, error) {
	return media.Info{}, errors.New("notification authorization fixture must not probe media")
}

func notificationAuthorizationFixture(t *testing.T) (context.Context, *pgxpool.Pool, *identity.Store, *notifications.Store, identity.Principal, notifications.Registration) {
	t.Helper()
	ctx, pool, users, admin, _ := applicationKeyTestStore(t)
	viewer, err := users.CreateUser(ctx, "Notification authority viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := clientSessionPrincipal(t, ctx, users, viewer, "viewer-password",
		identity.Client{Name: "Notification player", DeviceID: "notification-device"}, "emby")
	store := notifications.NewStore(pool, users, nil)
	receiver, target := "notification-receiver-secret", "notification-target-secret"
	if _, err := store.UpdateConfig(ctx, admin, notifications.ConfigUpdate{
		Revision: "1", Enabled: true, Endpoint: "https://receiver.invalid/events",
		AllowedNetworks: []string{"192.0.2.0/24"}, ReceiverCredential: &receiver,
	}); err != nil {
		t.Fatal(err)
	}
	registration, err := store.PutRegistration(ctx, actor, notifications.RegistrationUpdate{
		Revision: "0", Transport: notifications.Transport, TargetToken: &target,
		EventIds: []string{"CatalogInvalidated"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, pool, users, store, actor, registration
}

func TestNotificationResourceWaitDoesNotBlockSameSessionMediaAuthority(t *testing.T) {
	for _, operation := range []string{"put", "delete", "test"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, _, store, actor, registration := notificationAuthorizationFixture(t)
			catalog, err := library.New(pool, notificationAuthorizationProber{}, []string{t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := catalog.Close(closeCtx); err != nil {
					t.Error(err)
				}
			})
			blocker, _ := managedSessionBlocker(t, ctx, pool)
			defer blocker.Rollback(context.Background())
			if operation == "delete" {
				_, err = blocker.Exec(ctx, "SELECT id FROM notification_registrations WHERE id=$1 FOR UPDATE", registration.Id)
			} else {
				_, err = blocker.Exec(ctx, "SELECT id FROM notification_journal_state WHERE id=1 FOR UPDATE")
			}
			if err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				switch operation {
				case "put":
					_, err := store.PutRegistration(operationCtx, actor, notifications.RegistrationUpdate{
						Revision: registration.Revision, Transport: notifications.Transport, EventIds: []string{"UserDataInvalidated"},
					})
					finished <- err
				case "delete":
					_, err := store.DeleteRegistration(operationCtx, actor, registration.Revision)
					finished <- err
				case "test":
					finished <- store.EnqueueTest(operationCtx, actor)
				}
			}()
			waitForManagedAccountLock(t, ctx, pool, blocker.Conn().PgConn().PID())
			readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
			defer readCancel()
			// Even a missing source must pass the real media authority guard.
			// It must reach that result while the notification write still waits.
			file, _, err := catalog.AuthorizePlaybackMediaFor(readCtx, actor, "missing-play", "1", "", false)
			if file != nil {
				file.Close()
				t.Fatal("missing source unexpectedly opened media")
			}
			if !errors.Is(err, library.ErrNotFound) {
				t.Fatalf("notification resource wait blocked same-session media authority: %v", err)
			}
			select {
			case err := <-finished:
				t.Fatalf("notification mutation escaped its held resource lock: %v", err)
			default:
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatalf("notification mutation failed after its resource was released: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("notification mutation did not finish after its resource was released")
			}
		})
	}
}

func TestNotificationSharedAuthorityStillExcludesCredentialRevocation(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	user := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := managedLogin(t, ctx, store, user, "administrator-password", "emby")
	owner, _ := managedSessionBlocker(t, ctx, pool)
	defer owner.Rollback(context.Background())
	if err := identity.CheckNotificationSession(ctx, owner, actor); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- store.Revoke(operationCtx, credentials.Token) }()
	waitForManagedAccountLock(t, ctx, pool, owner.Conn().PgConn().PID())
	select {
	case err := <-finished:
		t.Fatalf("credential revocation passed a retained notification authority lock: %v", err)
	default:
	}
	if err := owner.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-operationCtx.Done():
		t.Fatal("credential revocation did not finish after authority was released")
	}
	fresh, _ := managedSessionBlocker(t, ctx, pool)
	if err := identity.CheckNotificationSession(ctx, fresh, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("notification authority admitted the revoked login: %v", err)
	}
}

func notificationMutationSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'registrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM notification_registrations r),
		'deliveries',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM notification_deliveries d),
		'journal',(SELECT to_jsonb(j) FROM notification_journal_state j WHERE id=1))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestNotificationMutationRechecksExpiryAfterResourceWait(t *testing.T) {
	ctx, pool, _, store, actor, registration := notificationAuthorizationFixture(t)
	before := notificationMutationSnapshot(t, ctx, pool)
	if _, err := pool.Exec(ctx, "UPDATE sessions SET expires_at=clock_timestamp()+interval '5 seconds' WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	blocker, _ := managedSessionBlocker(t, ctx, pool)
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT id FROM notification_registrations WHERE id=$1 FOR UPDATE", registration.Id); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := store.PutRegistration(operationCtx, actor, notifications.RegistrationUpdate{
			Revision: registration.Revision, Transport: notifications.Transport, EventIds: []string{"UserDataInvalidated"},
		})
		finished <- err
	}()
	waitForManagedAccountLock(t, ctx, pool, blocker.Conn().PgConn().PID())
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		if err := pool.QueryRow(operationCtx, "SELECT expires_at<=clock_timestamp() FROM sessions WHERE id=$1", actor.SessionID).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			break
		}
		select {
		case <-ticker.C:
		case <-operationCtx.Done():
			t.Fatal("notification expiry fixture did not reach its database deadline")
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("notification mutation committed after expiry during its resource wait: %v", err)
		}
	case <-operationCtx.Done():
		t.Fatal("expired notification mutation did not finish")
	}
	if after := notificationMutationSnapshot(t, ctx, pool); after != before {
		t.Fatal("expired notification mutation changed its registration, queue, or journal")
	}
}

func TestNotificationConcurrentMutationsPreserveVersionAndQueue(t *testing.T) {
	ctx, pool, _, store, actor, registration := notificationAuthorizationFixture(t)
	type result struct {
		operation string
		err       error
	}
	start := make(chan struct{})
	results := make(chan result, 3)
	for _, operation := range []string{"put", "delete", "test"} {
		go func() {
			<-start
			var err error
			switch operation {
			case "put":
				_, err = store.PutRegistration(ctx, actor, notifications.RegistrationUpdate{
					Revision: registration.Revision, Transport: notifications.Transport, EventIds: []string{"UserDataInvalidated"},
				})
			case "delete":
				_, err = store.DeleteRegistration(ctx, actor, registration.Revision)
			case "test":
				err = store.EnqueueTest(ctx, actor)
			}
			results <- result{operation: operation, err: err}
		}()
	}
	close(start)
	outcomes := make(map[string]error)
	for range 3 {
		select {
		case result := <-results:
			outcomes[result.operation] = result.err
		case <-ctx.Done():
			t.Fatal("concurrent notification mutations did not finish")
		}
	}
	putWon := outcomes["put"] == nil && errors.Is(outcomes["delete"], notifications.ErrConflict)
	deleteWon := outcomes["delete"] == nil && errors.Is(outcomes["put"], notifications.ErrConflict)
	if !putWon && !deleteWon {
		t.Fatalf("concurrent registration revisions did not select one writer: put=%v delete=%v", outcomes["put"], outcomes["delete"])
	}
	if err := outcomes["test"]; err != nil && !(deleteWon && errors.Is(err, notifications.ErrUnavailable)) {
		t.Fatalf("concurrent test delivery failed outside its registration state: %v", err)
	}
	current, err := store.Registration(ctx, actor)
	if err != nil || current.Revision != "2" || current.Enabled != putWon {
		t.Fatalf("concurrent registration result lost its committed version: %v", err)
	}
	var stale, total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE d.state IN ('pending','sending')
		AND (NOT r.enabled OR d.registration_revision<>r.revision OR d.transport_revision<>c.revision)),count(*)
		FROM notification_deliveries d JOIN notification_registrations r ON r.id=d.registration_id
		CROSS JOIN notification_transport c WHERE c.id=1 AND r.id=$1`, registration.Id).Scan(&stale, &total); err != nil {
		t.Fatal(err)
	}
	if stale != 0 || total > 1 || outcomes["test"] == nil && total != 1 {
		t.Fatalf("concurrent notification mutations left invalid deliveries: stale=%d total=%d", stale, total)
	}
}
