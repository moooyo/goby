//go:build linux

package backuppg

import (
	"errors"
	"testing"
)

func TestPostgreSQLSelectedPhase4StateValidatesNotificationLinkageAndHistoricalEvidence(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `UPDATE notification_transport SET revision=9007199254740993,enabled=true,
		endpoint='https://receiver.example.invalid/goby',allowed_networks=ARRAY['192.0.2.0/24'],credential_ciphertext=decode('474e5401'||repeat('ab',44),'hex');
		UPDATE notification_journal_state SET sequence=9007199254740993 WHERE id=1;
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		VALUES('phase4-source-session','backup-admin',decode(repeat('7d',32),'hex'),'emby','phase4-device','2099-01-01T00:00:00Z');
		INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,revision,event_ids,token_ciphertext,source_cursor)
		VALUES(repeat('9',32),'phase4-source-session','backup-admin','phase4-device','192.0.2.7',9007199254740993,
		ARRAY['CatalogInvalidated','UserDataInvalidated'],decode('474e5401'||repeat('cd',44),'hex'),9007199254740993);
		INSERT INTO notification_source_events(id,sequence,kind,refs)
		VALUES(repeat('8',32),9007199254740993,'CatalogInvalidated','[{"Kind":"Library","Id":"retained-library","LibraryId":"retained-library"}]');
		INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state)
		VALUES(repeat('a',32),repeat('9',32),9007199254740993,9007199254740993,9007199254740993,'CatalogInvalidated',
		'[{"Kind":"Library","Id":"retained-library","LibraryId":"retained-library"}]','pending'),
		(repeat('b',32),repeat('9',32),1,1,1,'CatalogInvalidated','[]','delivered')`); err != nil {
		t.Fatalf("seed bounded raw notification storage witness: %v", err)
	}
	// These opaque bytes prove low-level row semantics only. The encrypted
	// recovery integration separately authenticates real purpose-bound secrets.
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	for _, fixture := range []struct {
		name, mutation string
		valid          bool
	}{
		{"revoked_history", `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='phase4-source-session'`, true},
		{"disabled_history", `UPDATE notification_registrations SET enabled=false WHERE id=repeat('9',32)`, true},
		{"runtime_noncanonical", `UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{Network}','{"BindHost":"999.0.0.1","HttpPort":10096}')`, false},
		{"endpoint_credentials", `UPDATE notification_transport SET endpoint='https://user:secret@example.invalid/goby'`, false},
		{"duplicate_network", `UPDATE notification_transport SET allowed_networks=ARRAY['192.0.2.0/24','192.0.2.0/24']`, false},
		{"session_kind", `UPDATE sessions SET kind='admin' WHERE id='phase4-source-session'`, false},
		{"device_binding", `UPDATE notification_registrations SET device_id='other-device' WHERE id=repeat('9',32)`, false},
		{"duplicate_events", `UPDATE notification_registrations SET event_ids=ARRAY['CatalogInvalidated','CatalogInvalidated'] WHERE id=repeat('9',32)`, false},
		{"future_cursor", `UPDATE notification_registrations SET source_cursor=9007199254740994 WHERE id=repeat('9',32)`, false},
		{"future_source", `UPDATE notification_source_events SET sequence=9007199254740994`, false},
		{"stale_pending_generation", `UPDATE notification_deliveries SET registration_revision=1 WHERE id=repeat('a',32)`, false},
		{"unscoped_source", `UPDATE notification_source_events SET refs='[{"Kind":"Item","Id":"opaque-item"}]'`, false},
		{"unexpected_reference_field", `UPDATE notification_source_events SET refs='[{"Kind":"Library","Id":"retained-library","LibraryId":"retained-library","Token":"secret"}]'`, false},
		{"missing_control", `DELETE FROM notification_journal_state`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated notification state mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if tag, err := tx.Exec(ctx, fixture.mutation); err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("mutate exactly one SQL-valid notification row: %v", err)
			}
			err = validateSelectedPhase4State(ctx, tx, 48)
			if fixture.valid {
				if err != nil {
					t.Fatalf("rejected retained notification history: %v", err)
				}
			} else if !errors.Is(err, ErrSchema) {
				t.Fatalf("accepted invalid notification state: %v", err)
			}
		})
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
