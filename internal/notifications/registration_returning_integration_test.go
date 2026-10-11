//go:build linux

package notifications

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notificationjournal"
)

type registrationMutationTraceKey struct{}

type registrationMutationTrace struct {
	readbacks     int
	returned      bool
	afterMutation func()
}

func (trace *registrationMutationTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	if strings.HasPrefix(statement, "SELECT id,revision::text,enabled,event_ids,true,last_outcome FROM notification_registrations") {
		trace.readbacks++
	}
	if strings.HasPrefix(statement, "INSERT INTO notification_registrations(") ||
		strings.HasPrefix(statement, "UPDATE notification_registrations SET enabled=false,revision=") {
		return context.WithValue(ctx, registrationMutationTraceKey{}, true)
	}
	return ctx
}

func (trace *registrationMutationTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(registrationMutationTraceKey{}) == true && data.Err == nil {
		trace.returned = true
		if trace.afterMutation != nil {
			trace.afterMutation()
		}
	}
}

func registrationMutationStore(t *testing.T, fixture fanoutTransactionFixture) (*Store, *registrationMutationTrace, identity.Principal) {
	t.Helper()
	trace := new(registrationMutationTrace)
	configuration := fixture.observer.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	actor := identity.Principal{Kind: "emby", SessionID: "notification-session",
		User: identity.User{ID: "notification-user"}, PeerIP: "192.0.2.20"}
	return NewStore(pool, identity.New(pool), nil), trace, actor
}

func registrationMutationSnapshot(t *testing.T, fixture fanoutTransactionFixture) string {
	t.Helper()
	var snapshot string
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'registrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM notification_registrations r),
		'deliveries',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM notification_deliveries d),
		'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func seedRegistrationMutationDelivery(t *testing.T, fixture fanoutTransactionFixture, revision int64) {
	t.Helper()
	if _, err := fixture.observer.Exec(fixture.ctx, `INSERT INTO notification_deliveries
		(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state,lease_id,lease_until)
		VALUES($1,$2,$3,1,$3,'CatalogInvalidated','[]','sending',$4,clock_timestamp()+interval '1 minute')`,
		notificationjournal.NewID(), fanoutTransactionRegistration, revision, notificationjournal.NewID()); err != nil {
		t.Fatal(err)
	}
}

func assertRegistrationMutationResult(t *testing.T, store *Store, trace *registrationMutationTrace, ctx context.Context, actor identity.Principal, result Registration) {
	t.Helper()
	if !trace.returned || trace.readbacks != 0 {
		t.Fatalf("registration mutation returned=%t and performed %d full readbacks", trace.returned, trace.readbacks)
	}
	persisted, err := store.Registration(ctx, actor)
	if err != nil || !reflect.DeepEqual(result, persisted) || result.Transport != Transport || result.EventIds == nil {
		t.Fatalf("mutation projection differs from committed registration: result=%+v persisted=%+v error=%v", result, persisted, err)
	}
	trace.readbacks, trace.returned = 0, false
}

func TestRegistrationReturningPreservesUpdatesReenableRevokeAndDefaults(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	store, trace, actor := registrationMutationStore(t, fixture)
	if _, err := fixture.observer.Exec(fixture.ctx, "UPDATE notification_registrations SET last_outcome='delivered'"); err != nil {
		t.Fatal(err)
	}
	seedRegistrationMutationDelivery(t, fixture, 1)
	updated, err := store.PutRegistration(fixture.ctx, actor, RegistrationUpdate{
		Revision: "1", Transport: Transport, EventIds: []string{"UserDataInvalidated", "CatalogInvalidated"}})
	if err != nil || updated.Id != fanoutTransactionRegistration || updated.Revision != "2" || !updated.Enabled ||
		!updated.HasTargetToken || updated.LastOutcome != "" || !reflect.DeepEqual(updated.EventIds, []string{"CatalogInvalidated", "UserDataInvalidated"}) {
		t.Fatalf("registration update lost its response fields: %+v, %v", updated, err)
	}
	assertRegistrationMutationResult(t, store, trace, fixture.ctx, actor, updated)
	seedRegistrationMutationDelivery(t, fixture, 2)
	revoked, err := store.DeleteRegistration(fixture.ctx, actor, "2")
	if err != nil || revoked.Id != updated.Id || revoked.Revision != "3" || revoked.Enabled || !revoked.HasTargetToken ||
		revoked.LastOutcome != "revoked" || !reflect.DeepEqual(revoked.EventIds, updated.EventIds) {
		t.Fatalf("registration revocation lost its response fields: %+v, %v", revoked, err)
	}
	assertRegistrationMutationResult(t, store, trace, fixture.ctx, actor, revoked)
	var retired bool
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT bool_and(state='cancelled' AND lease_id='' AND lease_until IS NULL
		AND refs='[]'::jsonb AND outcome=CASE registration_revision WHEN 1 THEN 'registration_changed' ELSE 'revoked' END)
		FROM notification_deliveries`).Scan(&retired); err != nil || !retired {
		t.Fatalf("registration mutation skipped delivery retirement: %t, %v", retired, err)
	}
	reenabled, err := store.PutRegistration(fixture.ctx, actor, RegistrationUpdate{
		Revision: "3", Transport: Transport, EventIds: []string{"UserDataInvalidated"}})
	if err != nil || reenabled.Id != updated.Id || reenabled.Revision != "4" || !reenabled.Enabled || !reenabled.HasTargetToken || reenabled.LastOutcome != "" {
		t.Fatalf("registration reenable lost its response fields: %+v, %v", reenabled, err)
	}
	assertRegistrationMutationResult(t, store, trace, fixture.ctx, actor, reenabled)
	var generation int64
	if err := fixture.observer.QueryRow(fixture.ctx, "SELECT token_generation FROM notification_registrations WHERE id=$1", updated.Id).Scan(&generation); err != nil || generation != 1 {
		t.Fatalf("registration metadata mutation replaced its token generation: %d, %v", generation, err)
	}
	before := registrationMutationSnapshot(t, fixture)
	if result, err := store.PutRegistration(fixture.ctx, actor, RegistrationUpdate{
		Revision: "3", Transport: Transport, EventIds: []string{"CatalogInvalidated"}}); !errors.Is(err, ErrConflict) || !reflect.DeepEqual(result, Registration{}) {
		t.Fatalf("stale registration update published a candidate: %+v, %v", result, err)
	}
	if result, err := store.DeleteRegistration(fixture.ctx, actor, "3"); !errors.Is(err, ErrConflict) || !reflect.DeepEqual(result, Registration{}) {
		t.Fatalf("stale registration revoke published a candidate: %+v, %v", result, err)
	}
	if after := registrationMutationSnapshot(t, fixture); after != before {
		t.Fatal("stale registration mutations changed persisted state")
	}
	if _, err := fixture.observer.Exec(fixture.ctx, "DELETE FROM notification_registrations"); err != nil {
		t.Fatal(err)
	}
	if result, err := store.DeleteRegistration(fixture.ctx, actor, "4"); !errors.Is(err, ErrConflict) || !reflect.DeepEqual(result, Registration{}) {
		t.Fatalf("absent revoke adopted GET missing-row semantics: %+v, %v", result, err)
	}
	missing, err := store.Registration(fixture.ctx, actor)
	if err != nil || !reflect.DeepEqual(missing, Registration{Revision: "0", Transport: Transport, EventIds: []string{}}) {
		t.Fatalf("missing registration lost its GET defaults: %+v, %v", missing, err)
	}
}

func TestRegistrationReturningPreservesCreateProjection(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	store, trace, actor := registrationMutationStore(t, fixture)
	store.users = identity.NewWithApplicationKeyVault(store.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "registration-master")))
	if _, err := fixture.observer.Exec(fixture.ctx, `DELETE FROM notification_registrations;
		UPDATE notification_transport SET enabled=false,credential_ciphertext=NULL`); err != nil {
		t.Fatal(err)
	}
	tx, err := fixture.observer.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(fixture.ctx)
	if err := identity.LockNotificationMutation(fixture.ctx, tx, actor, false); err != nil {
		t.Fatal(err)
	}
	sealed, err := store.users.SealNotificationSecret(fixture.ctx, tx, identity.NotificationReceiverPurpose,
		identity.NotificationSecretBinding("receiver", "1"), "registration-receiver-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(fixture.ctx, "UPDATE notification_transport SET enabled=true,credential_ciphertext=$1", sealed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	token := "registration-target-secret"
	result, err := store.PutRegistration(fixture.ctx, actor, RegistrationUpdate{Revision: "0", Transport: Transport,
		TargetToken: &token, EventIds: []string{"UserDataInvalidated", "CatalogInvalidated"}})
	if err != nil || len(result.Id) != 32 || result.Revision != "1" || !result.Enabled || !result.HasTargetToken || result.LastOutcome != "" ||
		!reflect.DeepEqual(result.EventIds, []string{"CatalogInvalidated", "UserDataInvalidated"}) {
		t.Fatalf("registration insert lost its defaults or event ordering: %+v, %v", result, err)
	}
	assertRegistrationMutationResult(t, store, trace, fixture.ctx, actor, result)
}

func TestRegistrationReturningDoesNotPublishBeforeTransactionCompletion(t *testing.T) {
	for _, operation := range []string{"update", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newFanoutTransactionFixture(t)
			store, trace, actor := registrationMutationStore(t, fixture)
			seedRegistrationMutationDelivery(t, fixture, 1)
			for _, failure := range []string{"delivery_cancellation", "final_authority", "commit", "context_cancellation"} {
				t.Run(failure, func(t *testing.T) {
					var setup, cleanup string
					want := ErrUnavailable
					switch failure {
					case "delivery_cancellation":
						setup = "ALTER TABLE notification_deliveries ADD CONSTRAINT reject_registration_cancellation CHECK(state<>'cancelled')"
						cleanup = "ALTER TABLE notification_deliveries DROP CONSTRAINT reject_registration_cancellation"
					case "final_authority":
						setup = `CREATE FUNCTION expire_registration_actor() RETURNS trigger LANGUAGE plpgsql AS $$
						BEGIN UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=NEW.session_id; RETURN NEW; END $$;
						CREATE TRIGGER expire_registration_actor AFTER INSERT OR UPDATE ON notification_registrations
						FOR EACH ROW EXECUTE FUNCTION expire_registration_actor()`
						cleanup = "DROP TRIGGER expire_registration_actor ON notification_registrations; DROP FUNCTION expire_registration_actor()"
						want = identity.ErrUnauthorized
					case "commit":
						setup = `CREATE FUNCTION reject_registration_commit() RETURNS trigger LANGUAGE plpgsql AS $$
						BEGIN RAISE EXCEPTION 'registration commit rejected'; RETURN NEW; END $$;
						CREATE CONSTRAINT TRIGGER reject_registration_commit AFTER INSERT OR UPDATE ON notification_registrations
						DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_registration_commit()`
						cleanup = "DROP TRIGGER reject_registration_commit ON notification_registrations; DROP FUNCTION reject_registration_commit()"
					}
					if setup != "" {
						if _, err := fixture.observer.Exec(fixture.ctx, setup); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := fixture.observer.Exec(fixture.ctx, cleanup); err != nil {
								t.Error(err)
							}
						})
					}
					ctx, cancel := context.WithCancel(fixture.ctx)
					defer cancel()
					trace.readbacks, trace.returned = 0, false
					if failure == "context_cancellation" {
						trace.afterMutation = cancel
					}
					t.Cleanup(func() { trace.afterMutation = nil })
					before := registrationMutationSnapshot(t, fixture)
					var result Registration
					var err error
					if operation == "update" {
						result, err = store.PutRegistration(ctx, actor, RegistrationUpdate{Revision: "1", Transport: Transport, EventIds: []string{"UserDataInvalidated"}})
					} else {
						result, err = store.DeleteRegistration(ctx, actor, "1")
					}
					if !errors.Is(err, want) || !reflect.DeepEqual(result, Registration{}) || !trace.returned || trace.readbacks != 0 {
						t.Fatalf("incomplete mutation published its candidate: result=%+v error=%v returned=%t readbacks=%d", result, err, trace.returned, trace.readbacks)
					}
					if after := registrationMutationSnapshot(t, fixture); after != before {
						t.Fatal("failed registration mutation did not roll back authority, registration and delivery state")
					}
				})
			}
		})
	}
}
