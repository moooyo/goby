//go:build linux

package recoverydb

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
)

func assertRecoverySelectedPhase1ResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	f.exec(t, f.source, `INSERT INTO libraries(id,name,collection_type)
		VALUES('reset-intro-library','Intro reset witness','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES('reset-intro-item','reset-intro-library','Intro reset witness','intro reset witness','Movie',false)`)
	observe := func(name, table, mutation, undo string, rowDelta int64) {
		t.Helper()
		if !t.Run(name, func(t *testing.T) {
			before := f.capture(t, f.source)
			f.exec(t, f.source, mutation)
			after := f.capture(t, f.source)
			changed := false
			if before.RawMarker != after.RawMarker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("a selected phase1 state write changed the generation or catalog")
			}
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if previous.Name != table || current.Name != table || current.Rows != previous.Rows+rowDelta || changed {
					t.Fatal("a selected phase1 witness changed an unexpected durable table")
				}
				changed = true
			}
			if !changed {
				t.Fatal("retained proof omitted a selected phase1 durable mutation")
			}
			if err := f.source.store.ResetOwnedTarget(f.ctx,
				recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), before); !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale retained proof authorized reset after a phase1 mutation: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
			f.exec(t, f.source, undo)
			f.assertFacts(t, f.source, before.Facts)
		}) {
			t.Fatal("stop the exclusive recovery fixture after an inexact phase1 preservation result")
		}
	}
	observe("encrypted_profile_pin", "users",
		`UPDATE users SET profile_pin_ciphertext=decode('47505001'||repeat('ab',32),'hex') WHERE id='recovery-admin'`,
		`UPDATE users SET profile_pin_ciphertext=NULL WHERE id='recovery-admin'`, 0)
	observe("source_bound_intro", "item_intro_state",
		`INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance)
		 VALUES('reset-intro-item',9007199254740993,'retained-source',30000000,80000000,'Manual')`,
		`DELETE FROM item_intro_state WHERE item_id='reset-intro-item'`, 1)
	f.exec(t, f.source, `DELETE FROM libraries WHERE id='reset-intro-library'`)
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("phase1 reset protection changed the preserved recovery claim")
	}
}
