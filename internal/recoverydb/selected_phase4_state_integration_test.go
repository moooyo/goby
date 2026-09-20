//go:build linux

package recoverydb

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
)

func assertRecoverySelectedPhase4ResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	f.exec(t, f.source, `INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		VALUES('phase4-reset-session','recovery-admin',decode(repeat('7e',32),'hex'),'emby','phase4-reset-device','2099-01-01T00:00:00Z');
		UPDATE notification_journal_state SET sequence=9007199254740993 WHERE id=1;
		INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,enabled,event_ids,token_ciphertext,source_cursor)
		VALUES(repeat('9',32),'phase4-reset-session','recovery-admin','phase4-reset-device','',false,ARRAY['CatalogInvalidated'],
		decode('474e5401'||repeat('cd',44),'hex'),9007199254740993);
		INSERT INTO notification_source_events(id,sequence,kind,refs)
		VALUES(repeat('8',32),9007199254740993,'CatalogInvalidated','[{"Kind":"Library","Id":"historical-library","LibraryId":"historical-library"}]');
		INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state,attempts,outcome)
		VALUES(repeat('7',32),repeat('9',32),1,1,9007199254740993,'CatalogInvalidated','[]','delivered',1,'delivered')`)
	observe := func(name, table, mutation, undo string) {
		t.Helper()
		if !t.Run(name, func(t *testing.T) {
			before := f.capture(t, f.source)
			f.exec(t, f.source, mutation)
			after := f.capture(t, f.source)
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("phase4 mutation changed generation or catalog inventory")
			}
			changed := false
			for i, prior := range before.Facts.Tables {
				next := after.Facts.Tables[i]
				if reflect.DeepEqual(prior, next) {
					continue
				}
				if changed || prior.Name != table || next.Name != table || prior.Rows != next.Rows {
					t.Fatal("phase4 witness changed an unintended durable population")
				}
				changed = true
			}
			if !changed {
				t.Fatalf("retained proof omitted exact durable state in %s", table)
			}
			if err := f.source.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), before); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale phase4 state authorized target reset: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
			f.exec(t, f.source, undo)
			f.assertFacts(t, f.source, before.Facts)
		}) {
			t.Fatal("stop the exclusive recovery sequence after an inexact phase4 reset proof")
		}
	}
	observe("host_threads", "managed_settings", `UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{Threads}','7') WHERE id=1`, `UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{Threads}','null') WHERE id=1`)
	observe("transport_destination", "notification_transport", `UPDATE notification_transport SET endpoint='https://receiver.example.invalid/goby' WHERE id=1`, `UPDATE notification_transport SET endpoint='' WHERE id=1`)
	observe("journal_exact_sequence", "notification_journal_state", `UPDATE notification_journal_state SET sequence=sequence+1 WHERE id=1`, `UPDATE notification_journal_state SET sequence=sequence-1 WHERE id=1`)
	observe("registration_microsecond", "notification_registrations", `UPDATE notification_registrations SET updated_at=updated_at+interval '1 microsecond' WHERE id=repeat('9',32)`, `UPDATE notification_registrations SET updated_at=updated_at-interval '1 microsecond' WHERE id=repeat('9',32)`)
	observe("source_microsecond", "notification_source_events", `UPDATE notification_source_events SET created_at=created_at+interval '1 microsecond' WHERE id=repeat('8',32)`, `UPDATE notification_source_events SET created_at=created_at-interval '1 microsecond' WHERE id=repeat('8',32)`)
	observe("terminal_receipt_microsecond", "notification_deliveries", `UPDATE notification_deliveries SET updated_at=updated_at+interval '1 microsecond' WHERE id=repeat('7',32)`, `UPDATE notification_deliveries SET updated_at=updated_at-interval '1 microsecond' WHERE id=repeat('7',32)`)
	f.exec(t, f.source, `DELETE FROM sessions WHERE id='phase4-reset-session';DELETE FROM notification_source_events WHERE id=repeat('8',32);UPDATE notification_journal_state SET sequence=0 WHERE id=1`)
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("phase4 reset checks changed local recovery ownership")
	}
}
