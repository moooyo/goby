package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

type dispatchObservedTransactions struct {
	owner  library.OwnedTransactions
	calls  int
	before func(context.Context) error
}

func (value *dispatchObservedTransactions) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	value.calls++
	if value.before != nil {
		if err := value.before(ctx); err != nil {
			return err
		}
	}
	return value.owner.WithOwnedTx(ctx, callback)
}

func TestTaskDispatchIdleHintsAvoidOwnedTransactionsAndDefinitionLocks(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 0)
	ticks := int64(3600) * ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
			{Kind: ScheduleInterval, IntervalTicks: &ticks},
			{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged}}})
	if err != nil {
		t.Fatal(err)
	}
	observed := &dispatchObservedTransactions{owner: owner, before: func(context.Context) error {
		return errors.New("idle dispatch entered an owned transaction")
	}}
	reader, err := New(pool, observed)
	if err != nil {
		t.Fatal(err)
	}
	assertIdle := func(name string, excluded ...string) {
		t.Helper()
		blocker, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback(ctx)
		if _, err := blocker.Exec(ctx, `SELECT id FROM task_definitions FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		// A pool query that accidentally takes row locks must also fail, even
		// if it bypasses the transaction-owner observer entirely.
		idleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		before := observed.calls
		if changed, err := reader.dispatchOneSchedule(idleCtx, excluded...); err != nil || changed {
			t.Fatalf("%s schedule was not idle: changed=%t error=%v", name, changed, err)
		}
		if changed, err := reader.dispatchSystemEvent(idleCtx, excluded...); err != nil || changed {
			t.Fatalf("%s system event was not idle: changed=%t error=%v", name, changed, err)
		}
		if observed.calls != before {
			t.Fatalf("%s idle dispatch acquired the catalog owner", name)
		}
	}
	assertIdle("future schedule and unchanged event")
	now, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`,
		saved.Triggers[0].ID, now.Add(-3601*time.Second), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return systemevents.Record(tx.Exec, systemevents.LibraryChanged)
	}); err != nil {
		t.Fatal(err)
	}
	assertIdle("excluded task", definition.ID)
	for _, state := range []string{"disabled", "unsupported", "paused", "retired"} {
		enabled, key, calculation := state != "disabled", definition.Key, ""
		if state == "unsupported" {
			key = "unsupported.dispatch"
		}
		if state == "paused" {
			calculation = "invalid_schedule"
		}
		if _, err := pool.Exec(ctx, `UPDATE task_definitions SET enabled=$2,key=$3 WHERE id=$1`, definition.ID, enabled, key); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE task_triggers SET calculation_error=$2,
			retired_at=CASE WHEN $3 THEN clock_timestamp() ELSE NULL END WHERE task_id=$1`, definition.ID, calculation, state == "retired"); err != nil {
			t.Fatal(err)
		}
		assertIdle(state)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET retired_at=NULL WHERE task_id=$1`, definition.ID); err != nil {
		t.Fatal(err)
	}
	observed.before = nil
	if changed, err := reader.DispatchDue(ctx, 2); err != nil || !changed {
		t.Fatalf("due work was skipped: changed=%t error=%v", changed, err)
	}
	if changed, err := reader.DispatchSystemEvents(ctx, 2); err != nil || !changed {
		t.Fatalf("new event was skipped: changed=%t error=%v", changed, err)
	}
	if observed.calls != 2 {
		t.Fatalf("dispatch used %d owned transactions for two work items", observed.calls)
	}
	var occurrences int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&occurrences); err != nil || occurrences != 2 {
		t.Fatalf("dispatch lost work: occurrences=%d error=%v", occurrences, err)
	}
	assertIdle("consumed work")
}

func TestTaskDispatchRechecksRuleReplacementAfterIdleHint(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleInterval, ScheduleSystemEvent} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, pool, owner, store, actor, definition := taskRepository(t, 0)
			ticks := int64(3600) * ScheduleTicksPerSecond
			rule := ScheduleRule{Kind: kind}
			if kind == ScheduleInterval {
				rule.IntervalTicks = &ticks
			} else {
				rule.SystemEvent = systemevents.LibraryChanged
			}
			saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
			if err != nil {
				t.Fatal(err)
			}
			makeReady := func(triggerID string) {
				t.Helper()
				if kind == ScheduleSystemEvent {
					if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
						return systemevents.Record(tx.Exec, systemevents.LibraryChanged)
					}); err != nil {
						t.Fatal(err)
					}
					return
				}
				now, err := store.ScheduleClock(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`,
					triggerID, now.Add(-3601*time.Second), now.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			makeReady(saved.Triggers[0].ID)
			observed := &dispatchObservedTransactions{owner: owner}
			var replacement Definition
			observed.before = func(ctx context.Context) error {
				observed.before = nil
				var err error
				// Replace after the pool hint but before the authoritative locks.
				// The replacement starts in the future or at the current cursor.
				replacement, err = store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: saved.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
				return err
			}
			reader, err := New(pool, observed)
			if err != nil {
				t.Fatal(err)
			}
			dispatch := reader.DispatchDue
			if kind == ScheduleSystemEvent {
				dispatch = reader.DispatchSystemEvents
			}
			if changed, err := dispatch(ctx, 1); err != nil || changed || observed.calls != 1 {
				t.Fatalf("stale hint bypassed replacement: changed=%t calls=%d error=%v", changed, observed.calls, err)
			}
			var runs, occurrences int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_runs),
				(SELECT count(*) FROM task_occurrences)`).Scan(&runs, &occurrences); err != nil || runs != 0 || occurrences != 0 {
				t.Fatalf("retired rule admitted work: runs=%d occurrences=%d error=%v", runs, occurrences, err)
			}
			makeReady(replacement.Triggers[0].ID)
			if changed, err := dispatch(ctx, 1); err != nil || !changed {
				t.Fatalf("replacement lost later work: changed=%t error=%v", changed, err)
			}
			var triggerID string
			var revision int64
			if err := pool.QueryRow(ctx, `SELECT trigger_id,schedule_revision FROM task_occurrences`).Scan(&triggerID, &revision); err != nil ||
				triggerID != replacement.Triggers[0].ID || revision != replacement.Revision {
				t.Fatalf("dispatch retained stale occurrence identity: trigger=%s revision=%d error=%v", triggerID, revision, err)
			}
		})
	}
}
