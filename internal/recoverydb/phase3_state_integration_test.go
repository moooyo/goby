//go:build linux

package recoverydb

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
)

// Reuse the existing exclusive public-schema fixture. These changes must make
// an earlier retained proof stale even when the generation marker is unchanged.
func assertRecoveryPhase3ResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	observe := func(name, table string, rowDelta int64, mutate, undo func(*testing.T)) {
		t.Helper()
		if !t.Run(name, func(t *testing.T) {
			before := f.capture(t, f.source)
			mutate(t)
			after := f.capture(t, f.source)
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("a phase 3 state write changed the generation marker or catalog inventory")
			}
			changed := false
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if previous.Name != table || current.Name != table || current.Rows != previous.Rows+rowDelta || changed {
					t.Fatal("the phase 3 reset witness changed an unexpected table or row count")
				}
				changed = true
			}
			if !changed {
				t.Fatalf("the retained proof omitted a durable phase 3 change in %s", table)
			}
			if err := f.source.store.ResetOwnedTarget(f.ctx,
				recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), before); !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale phase 3 retained proof authorized target reset: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
			undo(t)
			f.assertFacts(t, f.source, before.Facts)
		}) {
			t.Fatal("stop using the exclusive pair after a failed phase 3 reset assertion")
		}
	}
	var revision int64
	if err := f.source.pool.QueryRow(f.ctx, `SELECT configuration_revision FROM users WHERE id='recovery-admin'`).Scan(&revision); err != nil || revision == math.MaxInt64 {
		t.Fatalf("read the phase 3 account revision witness: %v", err)
	}
	observe("configuration_revision", "users", 0,
		func(t *testing.T) {
			f.exec(t, f.source, `UPDATE users SET configuration_revision=$1 WHERE id='recovery-admin'`, revision+1)
		},
		func(t *testing.T) {
			f.exec(t, f.source, `UPDATE users SET configuration_revision=$1 WHERE id='recovery-admin'`, revision)
		})
	observe("client_display_preferences", "display_preferences", 1,
		func(t *testing.T) {
			f.exec(t, f.source, `INSERT INTO display_preferences(user_id,client,preferences_id,preferences,revision)
				VALUES('recovery-admin','phase3-reset-client','phase3-reset-view',
				'{"Id":"phase3-reset-view","Client":"phase3-reset-client","SortBy":"SortName","SortOrder":"Ascending"}'::jsonb,9007199254740993)`)
		},
		func(t *testing.T) {
			f.exec(t, f.source, `DELETE FROM display_preferences WHERE user_id='recovery-admin'
				AND client='phase3-reset-client' AND preferences_id='phase3-reset-view'`)
		})
	var sequence int64
	if err := f.source.pool.QueryRow(f.ctx, `SELECT sequence FROM task_system_events WHERE name='LibraryChanged'`).Scan(&sequence); err != nil || sequence == math.MaxInt64 {
		t.Fatalf("read the phase 3 durable event sequence witness: %v", err)
	}
	observe("system_event_sequence", "task_system_events", 0,
		func(t *testing.T) {
			f.exec(t, f.source, `UPDATE task_system_events SET sequence=$1 WHERE name='LibraryChanged'`, sequence+1)
		},
		func(t *testing.T) {
			f.exec(t, f.source, `UPDATE task_system_events SET sequence=$1 WHERE name='LibraryChanged'`, sequence)
		})
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("phase 3 reset protection changed the preserved recovery claim")
	}
}
