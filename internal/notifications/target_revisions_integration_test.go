package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationDeliveryRevisionChecksPreserveAuthority(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	if _, err := fixture.observer.Exec(fixture.ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
		VALUES('other-notification-user','Other notification user','other notification user','fixture')`); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{store: fixture.store}
	for _, test := range []struct {
		name             string
		statement        string
		revisionChanged  bool
		sessionAuthority bool
	}{
		{name: "registration disabled without revision", statement: `UPDATE notification_registrations SET enabled=false`},
		{name: "transport disabled without revision", statement: `UPDATE notification_transport SET enabled=false`},
		{name: "account disabled", statement: `UPDATE users SET is_disabled=true WHERE id='notification-user'`},
		{name: "credential revoked", statement: `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='notification-session'`},
		{name: "credential expired", statement: `UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id='notification-session'`},
		{name: "device binding changed", statement: `UPDATE sessions SET device_id='changed-device' WHERE id='notification-session'`},
		{name: "user binding changed", statement: `UPDATE sessions SET user_id='other-notification-user' WHERE id='notification-session'`},
		{name: "session kind changed", statement: `UPDATE sessions SET kind='admin' WHERE id='notification-session'`},
		{name: "registration revision changed", statement: `UPDATE notification_registrations SET revision=revision+1`, revisionChanged: true},
		{name: "transport revision changed", statement: `UPDATE notification_transport SET revision=revision+1`, revisionChanged: true},
		{name: "local credential at remote peer", statement: `UPDATE sessions SET local_auth=true WHERE id='notification-session'`, sessionAuthority: true},
		{name: "remote access revoked", statement: `UPDATE users SET policy='{"EnableRemoteAccess":false}' WHERE id='notification-user'`, sessionAuthority: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := fixture.observer.Exec(fixture.ctx, `UPDATE users SET is_disabled=false,policy='{}' WHERE id='notification-user';
				UPDATE sessions SET user_id='notification-user',device_id='notification-device',kind='emby',
					revoked_at=NULL,expires_at=clock_timestamp()+interval '1 hour',local_auth=false WHERE id='notification-session';
				UPDATE notification_registrations SET enabled=true,revision=1;
				UPDATE notification_transport SET enabled=true,revision=1`); err != nil {
				t.Fatal(err)
			}
			target, err := fixture.store.currentTarget(fixture.ctx, fanoutTransactionRegistration)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.checkDelivery(fixture.ctx, target, nil); err != nil {
				t.Fatalf("valid delivery was rejected: %v", err)
			}
			if _, err := fixture.observer.Exec(fixture.ctx, test.statement); err != nil {
				t.Fatal(err)
			}
			var registrationRevision, transportRevision int64
			if err := fixture.observer.QueryRow(fixture.ctx, `SELECT r.revision,c.revision
				FROM notification_registrations r CROSS JOIN notification_transport c WHERE r.id=$1 AND c.id=1`,
				fanoutTransactionRegistration).Scan(&registrationRevision, &transportRevision); err != nil {
				t.Fatal(err)
			}
			if !test.revisionChanged && (registrationRevision != target.regRevision || transportRevision != target.configRevision) {
				t.Fatal("fixture changed revisions while testing an independent authority condition")
			}
			_, err = fixture.store.currentTargetRevisions(fixture.ctx, target.id)
			if test.revisionChanged || test.sessionAuthority {
				if err != nil {
					t.Fatalf("target lookup unexpectedly replaced the later revision or session check: %v", err)
				}
			} else if !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("target authority change was not rejected by the narrow lookup: %v", err)
			}
			if err := runtime.checkDelivery(fixture.ctx, target, nil); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("changed delivery authority was accepted: %v", err)
			}
		})
	}
}

func TestNotificationDeliveryRevisionLookupMapsFailures(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	runtime := &Runtime{store: fixture.store}
	target, err := fixture.store.currentTarget(fixture.ctx, fanoutTransactionRegistration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.currentTargetRevisions(fixture.ctx, "missing-registration"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("missing registration error = %v", err)
	}
	ctx, cancel := context.WithCancel(fixture.ctx)
	cancel()
	if err := runtime.checkDelivery(ctx, target, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancelled revision query error = %v", err)
	}
	if _, err := fixture.observer.Exec(fixture.ctx, `ALTER TABLE notification_transport RENAME TO unavailable_notification_transport`); err != nil {
		t.Fatal(err)
	}
	if err := runtime.checkDelivery(fixture.ctx, target, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed revision query error = %v", err)
	}
}
