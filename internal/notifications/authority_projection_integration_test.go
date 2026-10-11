package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationAuthorityProjectionOmitsDisplayFailuresAndKeepsFreshRevocation(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	fixture.attachCatalog(t)
	target, err := fixture.store.currentTarget(fixture.ctx, fanoutTransactionRegistration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.observer.Exec(fixture.ctx, `UPDATE users SET created_at='infinity' WHERE id='notification-user'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.users.RevalidateSession(fixture.ctx, target.principal()); err == nil {
		t.Fatal("complete principal read accepted an unrepresentable display timestamp")
	}
	if refs, err := fixture.store.authorizedRefs(fixture.ctx, target, nil); err != nil || len(refs) != 0 {
		t.Fatalf("notification reference authority consumed an unused display field: %v", err)
	}
	if err := fixture.store.fanoutRegistration(fixture.ctx, fanoutTransactionRegistration); err != nil || !fixture.enabled(t) {
		t.Fatalf("fanout authority consumed an unused display field: %v", err)
	}
	fixture.trace.fired = false
	fixture.trace.afterTarget = func(ctx context.Context) error {
		_, err := fixture.observer.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='notification-session'`)
		return err
	}
	if err := fixture.store.fanoutRegistration(fixture.ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	if !fixture.trace.fired || fixture.trace.err != nil || fixture.enabled(t) {
		t.Fatal("authority projection lost the same-transaction fresh revocation update")
	}
	if _, err := fixture.store.authorizedRefs(fixture.ctx, target, nil); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("notification references reused authority after revocation: %v", err)
	}
}
