package tasks

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func countScheduledStatements(observer *executionRefreshObserver, prefix string) int {
	count := 0
	for _, statement := range observer.statements {
		if strings.HasPrefix(statement, prefix) {
			count++
		}
	}
	return count
}

func TestTaskScheduledMissedReceiptRetainsIdempotency(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleStartup, ScheduleInterval} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
			ticks := int64(1000) * ScheduleTicksPerSecond
			rule := ScheduleRule{Kind: kind}
			if kind == ScheduleInterval {
				rule.IntervalTicks = &ticks
			}
			saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
			if err != nil {
				t.Fatal(err)
			}
			due, err := store.ScheduleClock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			trigger := saved.Triggers[0]
			if kind == ScheduleInterval {
				if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`,
					trigger.ID, due.Add(-1000*time.Second), due); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `INSERT INTO task_occurrences
				(id,task_id,trigger_id,schedule_revision,due_at,disposition)
				VALUES(md5($1),$2,$1,$3,$4,'missed')`, trigger.ID, definition.ID, trigger.ScheduleRevision, due); err != nil {
				t.Fatal(err)
			}
			observer := &executionRefreshObserver{owner: owner}
			observed, err := New(pool, observer)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ScheduleStartup {
				err = observed.InitializeSchedules(ctx, due)
			} else {
				var changed bool
				changed, err = observed.DispatchDue(ctx, 1)
				if !changed {
					t.Fatalf("existing missed receipt did not advance its timed rule: %v", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var runID *string
			var disposition string
			var occurrences, runs int
			var lastDue time.Time
			if err := pool.QueryRow(ctx, `SELECT o.run_id,o.disposition,t.last_due_at,
				(SELECT count(*) FROM task_occurrences),(SELECT count(*) FROM task_runs)
				FROM task_occurrences o JOIN task_triggers t ON t.id=o.trigger_id WHERE t.id=$1`, trigger.ID).
				Scan(&runID, &disposition, &lastDue, &occurrences, &runs); err != nil {
				t.Fatal(err)
			}
			if runID != nil || disposition != "missed" || !lastDue.Equal(due) || occurrences != 1 || runs != 0 {
				t.Fatal("existing missed receipt was converted into a new admission or lost its cursor")
			}
			if countScheduledStatements(observer, "SELECT run_id,disposition FROM task_occurrences") != 1 ||
				countScheduledStatements(observer, "SELECT id FROM task_runs") != 0 {
				t.Fatalf("existing receipt entered run admission: %v", observer.statements)
			}
		})
	}
}

func TestSystemEventReusesNewAndExistingOccurrenceReceipts(t *testing.T) {
	for _, scenario := range []string{"new", "admitted", "overlap", "missed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
			saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
					{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged}}})
			if err != nil {
				t.Fatal(err)
			}
			trigger := saved.Triggers[0]
			var existingRunID *string
			if scenario == "admitted" || scenario == "overlap" {
				admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
				if err != nil {
					t.Fatal(err)
				}
				existingRunID = &admission.Run.ID
				if scenario == "admitted" {
					if _, err := store.Stop(ctx, actor, admission.Run.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				return systemevents.Record(tx.Exec, systemevents.LibraryChanged)
			}); err != nil {
				t.Fatal(err)
			}
			var sequence int64
			var due time.Time
			if err := pool.QueryRow(ctx, `SELECT sequence,occurred_at FROM task_system_events WHERE name=$1`,
				string(systemevents.LibraryChanged)).Scan(&sequence, &due); err != nil {
				t.Fatal(err)
			}
			if scenario != "new" {
				if _, err := pool.Exec(ctx, `INSERT INTO task_occurrences
					(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id)
					VALUES(md5($1),$2,$1,$3,$4,$5,$6)`, trigger.ID, definition.ID, trigger.ScheduleRevision,
					due, scenario, existingRunID); err != nil {
					t.Fatal(err)
				}
			}
			before := phase3TaskEventSnapshot(t, ctx, pool)
			observer := &executionRefreshObserver{owner: owner}
			observed, err := New(pool, observer)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := observed.DispatchSystemEvents(ctx, 1)
			if countScheduledStatements(observer, "SELECT run_id,disposition FROM task_occurrences") != 1 ||
				countScheduledStatements(observer, executionRefreshRunRead) != 0 {
				t.Fatalf("system-event admission reread its occurrence or run: %v", observer.statements)
			}
			if scenario == "missed" {
				if changed || !errors.Is(err, ErrInconsistent) || phase3TaskEventSnapshot(t, ctx, pool) != before {
					t.Fatalf("missed receipt was consumed as an admitted event: changed=%t error=%v", changed, err)
				}
				return
			}
			if err != nil || !changed {
				t.Fatalf("valid occurrence receipt did not consume its event: changed=%t error=%v", changed, err)
			}
			var runID, disposition string
			var first, last, cursor int64
			var runs, receipts int
			if err := pool.QueryRow(ctx, `SELECT e.run_id,e.disposition,e.first_sequence,e.last_sequence,t.last_event_sequence,
				(SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_system_event_receipts)
				FROM task_system_event_receipts e JOIN task_triggers t ON t.id=e.trigger_id WHERE e.trigger_id=$1`, trigger.ID).
				Scan(&runID, &disposition, &first, &last, &cursor, &runs, &receipts); err != nil {
				t.Fatal(err)
			}
			wantDisposition := scenario
			if scenario == "new" {
				wantDisposition = "admitted"
			}
			if runID == "" || disposition != wantDisposition || first != trigger.LastEventSequence+1 || last != sequence ||
				cursor != sequence || runs != 1 || receipts != 1 || (existingRunID != nil && runID != *existingRunID) {
				t.Fatal("receipt reuse changed the durable event range or admitted a second run")
			}
			if changed, err := observed.DispatchSystemEvents(ctx, 1); err != nil || changed {
				t.Fatalf("consumed event was admitted again: changed=%t error=%v", changed, err)
			}
		})
	}
}

func TestTaskScheduledAdmissionQueryFailuresRollback(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	startup, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observer := &executionRefreshObserver{owner: owner}
	observed, err := New(pool, observer)
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"SELECT run_id,disposition FROM task_occurrences", "SELECT id FROM task_runs WHERE task_id",
		"UPDATE task_runs SET total_children", "SELECT count(*),"} {
		t.Run(prefix, func(t *testing.T) {
			before := phase3TaskEventSnapshot(t, ctx, pool)
			injected := errors.New("injected scheduled admission query failure")
			reached := false
			observer.fail = func(statement string) error {
				if strings.HasPrefix(statement, prefix) {
					reached = true
					return injected
				}
				return nil
			}
			err := observed.InitializeSchedules(ctx, startup)
			observer.fail = nil
			if !reached || !errors.Is(err, injected) || phase3TaskEventSnapshot(t, ctx, pool) != before {
				t.Fatalf("scheduled query failure leaked an admission or lost its error: reached=%t error=%v", reached, err)
			}
		})
	}
	if err := observed.InitializeSchedules(ctx, startup); err != nil {
		t.Fatalf("rolled-back scheduled admission could not retry: %v", err)
	}
}

func TestTaskScheduledAdmissionReturningDecodeFailureRollsBack(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	startup, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION scheduler_invalid_returning_time() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN NEW.created_at='infinity'::timestamptz; RETURN NEW; END $$;
		CREATE TRIGGER scheduler_invalid_returning_time BEFORE UPDATE OF total_children ON task_runs
		FOR EACH ROW EXECUTE FUNCTION scheduler_invalid_returning_time()`); err != nil {
		t.Fatal(err)
	}
	before := phase3TaskEventSnapshot(t, ctx, pool)
	observer := &executionRefreshObserver{owner: owner}
	observed, err := New(pool, observer)
	if err != nil {
		t.Fatal(err)
	}
	err = observed.InitializeSchedules(ctx, startup)
	var invalidTime *time.ParseError
	if !errors.As(err, &invalidTime) || countScheduledStatements(observer, "UPDATE task_runs SET total_children") != 1 ||
		countScheduledStatements(observer, executionRefreshRunRead) != 0 || phase3TaskEventSnapshot(t, ctx, pool) != before {
		t.Fatalf("invalid returned Run did not roll back its real child-count update: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER scheduler_invalid_returning_time ON task_runs`); err != nil {
		t.Fatal(err)
	}
	if err := observed.InitializeSchedules(ctx, startup); err != nil {
		t.Fatalf("returning decode failure prevented a healthy retry: %v", err)
	}
}
