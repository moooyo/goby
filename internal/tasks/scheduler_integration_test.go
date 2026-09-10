package tasks

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestTaskSchedulerRecoverySummarizesDowntimeAndDeduplicatesStartup(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 0)
	ticks := ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
			{Kind: ScheduleInterval, IntervalTicks: &ticks}, {Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	anchor := scheduleTestTime(t, "2000-01-01T00:00:00Z")
	first := anchor.Add(time.Second)
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`, saved.Triggers[0].ID, anchor, first); err != nil {
		t.Fatal(err)
	}
	startupAt, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.InitializeSchedules(ctx, startupAt); err != nil {
			t.Fatal(err)
		}
	}
	var occurrences, runs, children int
	var count int64
	var missedFirst, missedLast time.Time
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_occurrences),
        (SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_children),
        due_at,last_due_at,occurrence_count FROM task_occurrences WHERE disposition='missed'`).
		Scan(&occurrences, &runs, &children, &missedFirst, &missedLast, &count); err != nil {
		t.Fatal(err)
	}
	if occurrences != 2 || runs != 1 || children != 0 || count != startupAt.Unix()-anchor.Unix() ||
		!missedFirst.Equal(first) || !missedLast.Equal(startupAt.Truncate(time.Second)) {
		t.Fatalf("restart replayed work or lost exact downtime: occurrences=%d runs=%d children=%d count=%d", occurrences, runs, children, count)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentRun != nil || current.LastRun == nil || current.LastRun.State != RunCompleted ||
		current.LastRun.Source != "startup" || current.LastRun.ScheduledFor == nil ||
		!current.LastRun.ScheduledFor.Equal(startupAt) || current.LastRun.ActorUserID != "" ||
		current.LastRun.ActorSessionID != "" || current.LastRun.ActorKind != "" {
		t.Fatal("startup did not keep an event receipt after its empty-library run completed")
	}
	if current.Triggers[0].NextFireAt == nil || !current.Triggers[0].NextFireAt.Equal(startupAt.Truncate(time.Second).Add(time.Second)) {
		t.Fatal("restart did not move the timed rule strictly beyond startup")
	}
	if current.Triggers[1].NextFireAt != nil || current.Triggers[1].LastDueAt == nil || !current.Triggers[1].LastDueAt.Equal(startupAt) {
		t.Fatal("startup event became a timer or lost its event instant")
	}
	// A replacement saved after the captured startup does not become another
	// startup event merely because initialization is retried.
	newRules, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: current.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	if !newRules.Triggers[0].CreatedAt.After(startupAt) {
		t.Fatal("test replacement did not follow its startup cutoff")
	}
	if err := store.InitializeSchedules(ctx, startupAt); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&occurrences); err != nil || occurrences != 2 {
		t.Fatalf("initialization retry fired a later replacement: count=%d error=%v", occurrences, err)
	}
}

func TestTaskSchedulerConcurrentDispatchCoalescesAndPreservesRuntimeSnapshot(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 3)
	ticks := int64(1000)*ScheduleTicksPerSecond + 1
	runtime := ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
			{Kind: ScheduleInterval, IntervalTicks: &ticks, MaxRuntimeTicks: &runtime},
			{Kind: ScheduleInterval, IntervalTicks: &ticks}}})
	if err != nil {
		t.Fatal(err)
	}
	now, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anchor := now.Add(-3001 * time.Second)
	period := 1000*time.Second + 100*time.Nanosecond
	first, _ := ceilScheduleTime(anchor.Add(period))
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE task_id=$1`, definition.ID, anchor, first); err != nil {
		t.Fatal(err)
	}
	const clients = 8
	failures := make(chan error, clients)
	var workers sync.WaitGroup
	for range clients {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := store.DispatchDue(ctx, 2)
			failures <- err
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var admitted, overlap, missed, runs, children int
	var missedCount int64
	if err := pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM task_occurrences WHERE disposition='admitted'),
        (SELECT count(*) FROM task_occurrences WHERE disposition='overlap'),
        (SELECT count(*) FROM task_occurrences WHERE disposition='missed'),
        (SELECT sum(occurrence_count) FROM task_occurrences WHERE disposition='missed'),
        (SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_children)`).
		Scan(&admitted, &overlap, &missed, &missedCount, &runs, &children); err != nil {
		t.Fatal(err)
	}
	if admitted != 1 || overlap != 1 || missed != 2 || missedCount != 4 || runs != 1 || children != 3 {
		t.Fatalf("concurrent dispatch duplicated or replayed work: admit=%d overlap=%d missed=%d count=%d runs=%d children=%d",
			admitted, overlap, missed, missedCount, runs, children)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil || current.CurrentRun == nil {
		t.Fatalf("scheduled run missing: %v", err)
	}
	run := *current.CurrentRun
	last, _ := ceilScheduleTime(anchor.Add(3 * period))
	next, _ := ceilScheduleTime(anchor.Add(4 * period))
	if run.Source != "schedule" || run.TriggerID == nil || *run.TriggerID != saved.Triggers[0].ID ||
		run.TriggerRevision == nil || *run.TriggerRevision != saved.Revision || run.ScheduledFor == nil ||
		!run.ScheduledFor.Equal(last) || run.MaxRuntimeTicks == nil || *run.MaxRuntimeTicks != runtime ||
		run.ActorKind != "" || run.ActorUserID != "" || run.ActorSessionID != "" {
		t.Fatal("scheduled run did not retain its exact trigger and runtime snapshot")
	}
	for _, trigger := range current.Triggers {
		if trigger.NextFireAt == nil || !trigger.NextFireAt.Equal(next) || trigger.LastDueAt == nil || !trigger.LastDueAt.Equal(last) {
			t.Fatal("dispatch drifted from the immutable fractional-tick anchor")
		}
	}
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: current.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{}}); err != nil {
		t.Fatal(err)
	}
	preserved, err := store.GetRun(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(preserved, run) {
		t.Fatalf("replacement changed an admitted run: %v", err)
	}
	started, err := store.BeginRun(ctx, run.ID)
	if err != nil || started.StartedAt == nil || started.DeadlineAt == nil ||
		!started.DeadlineAt.Equal(started.StartedAt.Add(time.Second)) {
		t.Fatalf("retired rule runtime was not applied when its run started: %v", err)
	}
}

func TestTaskSchedulerCalendarErrorRetainsRecoveryPositionAndOtherRulesProceed(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 1)
	ticks := int64(1000) * ScheduleTicksPerSecond
	noon := int64(12*3600) * ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
			{Kind: ScheduleDaily, TimeOfDayTicks: &noon}, {Kind: ScheduleInterval, IntervalTicks: &ticks}}})
	if err != nil {
		t.Fatal(err)
	}
	first := scheduleTestTime(t, "2000-01-01T12:00:00Z")
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET next_fire_at=$2 WHERE id=$1`, saved.Triggers[0].ID, first); err != nil {
		t.Fatal(err)
	}
	now, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anchor := now.Add(-2001 * time.Second)
	intervalDue := anchor.Add(1000 * time.Second)
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`, saved.Triggers[1].ID, anchor, intervalDue); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.DispatchDue(ctx, 1); err != nil || !changed {
		t.Fatalf("calendar calculation did not become an explicit paused rule: %t, %v", changed, err)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	paused := current.Triggers[0]
	if paused.CalculationError != "calendar_misfire_limit" || paused.NextFireAt == nil ||
		!paused.NextFireAt.Equal(first) || paused.LastDueAt != nil || current.CurrentRun != nil {
		t.Fatal("bounded calendar calculation advanced or discarded an uncounted range")
	}
	if next, err := store.NextDue(ctx); err != nil || next == nil || !next.Equal(intervalDue) {
		t.Fatalf("paused calendar masked another due rule: %v, %v", next, err)
	}
	if changed, err := store.DispatchDue(ctx, 4); err != nil || !changed {
		t.Fatalf("another rule could not proceed after a calculation error: %t, %v", changed, err)
	}
	if changed, err := store.DispatchDue(ctx, 4); err != nil || changed {
		t.Fatalf("paused calendar became a dispatch hot loop: %t, %v", changed, err)
	}
	var calendarOccurrences int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_occurrences WHERE trigger_id=$1`, paused.ID).Scan(&calendarOccurrences); err != nil || calendarOccurrences != 0 {
		t.Fatalf("calendar overflow persisted an estimated count: count=%d error=%v", calendarOccurrences, err)
	}
	replaced, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: current.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleDaily, TimeOfDayTicks: &noon}}})
	if err != nil || len(replaced.Triggers) != 1 || replaced.Triggers[0].CalculationError != "" ||
		replaced.Triggers[0].NextFireAt == nil || !replaced.Triggers[0].NextFireAt.After(now) {
		t.Fatalf("replacement did not reset paused calculation explicitly: %v", err)
	}
}

func TestTaskSchedulerFailedOccurrenceWriteRollsBackAdmissionAndAdvancement(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 2)
	ticks := int64(1000) * ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks}}})
	if err != nil {
		t.Fatal(err)
	}
	now, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anchor := now.Add(-1001 * time.Second)
	first := anchor.Add(1000 * time.Second)
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`, saved.Triggers[0].ID, anchor, first); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected occurrence persistence failure")
	var reached atomic.Bool
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO task_occurrences") && reached.CompareAndSwap(false, true) {
			return injected
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := hooked.DispatchDue(ctx, 1); changed || !errors.Is(err, injected) || !reached.Load() {
		t.Fatalf("injected post-admission failure was not returned: changed=%t error=%v", changed, err)
	}
	after, err := store.Get(ctx, definition.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed occurrence persistence leaked schedule or run state: %v", err)
	}
	var occurrences, runs, children int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_occurrences),
        (SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_children)`).Scan(&occurrences, &runs, &children); err != nil {
		t.Fatal(err)
	}
	if occurrences != 0 || runs != 0 || children != 0 {
		t.Fatal("failed occurrence persistence left partial admission rows")
	}
	if changed, err := store.DispatchDue(ctx, 1); err != nil || !changed {
		t.Fatalf("retry could not admit the rolled-back occurrence: changed=%t error=%v", changed, err)
	}
}

func TestTaskSchedulerExcludesDisabledUnknownAndRetiredRules(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 0)
	ticks := int64(1000) * ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{
			{Kind: ScheduleInterval, IntervalTicks: &ticks}, {Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	now, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anchor := now.Add(-1001 * time.Second)
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`, saved.Triggers[0].ID, anchor, anchor.Add(1000*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"disabled", "unknown", "retired"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE task_definitions SET enabled=$2,key=$3 WHERE id=$1`, definition.ID, mode != "disabled",
				map[bool]string{true: "unsupported.executor", false: LibraryScanKey}[mode == "unknown"]); err != nil {
				t.Fatal(err)
			}
			if mode == "retired" {
				if _, err := pool.Exec(ctx, `UPDATE task_triggers SET retired_at=clock_timestamp() WHERE task_id=$1`, definition.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.InitializeSchedules(ctx, now); err != nil {
				t.Fatal(err)
			}
			if changed, err := store.DispatchDue(ctx, 2); err != nil || changed {
				t.Fatalf("excluded rules entered dispatch: changed=%t error=%v", changed, err)
			}
			if next, err := store.NextDue(ctx); err != nil || next != nil {
				t.Fatalf("excluded rule became the next wakeup: %v, %v", next, err)
			}
		})
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("excluded rules created occurrences: count=%d error=%v", count, err)
	}
}

func TestTaskSchedulerInitializationRetryUsesItsFixedStartupInstant(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 0)
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}, {Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	startupAt, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected second startup transaction failure")
	var occurrences atomic.Int32
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO task_occurrences") && occurrences.Add(1) == 2 {
			return injected
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := hooked.InitializeSchedules(ctx, startupAt); !errors.Is(err, injected) {
		t.Fatalf("initialization did not expose a partial-pass failure: %v", err)
	}
	if err := store.InitializeSchedules(ctx, startupAt); err != nil {
		t.Fatal(err)
	}
	var runs, count int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_runs),count(*)
        FROM task_occurrences WHERE due_at=$1 AND trigger_id=ANY($2::text[])`, startupAt,
		[]string{saved.Triggers[0].ID, saved.Triggers[1].ID}).Scan(&runs, &count); err != nil {
		t.Fatal(err)
	}
	if runs != 2 || count != 2 {
		t.Fatalf("startup retry duplicated a committed empty-library event: runs=%d occurrences=%d", runs, count)
	}
}

func TestTaskSchedulerFractionalTicksSurviveConsecutiveDatabaseAdvancement(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 0)
	ticks := ScheduleTicksPerSecond + 1
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks}}})
	if err != nil {
		t.Fatal(err)
	}
	anchor := *saved.Triggers[0].AnchorAt
	stored := *saved.Triggers[0].NextFireAt
	period := time.Second + 100*time.Nanosecond
	// Synthetic startup cutoffs avoid sleeps while exercising real PostgreSQL
	// encoding/decoding and atomic advancement at each fractional occurrence.
	for index := 1; index <= 20; index++ {
		if err := store.InitializeSchedules(ctx, stored); err != nil {
			t.Fatal(err)
		}
		current, err := store.Get(ctx, definition.ID)
		if err != nil {
			t.Fatal(err)
		}
		trigger := current.Triggers[0]
		wanted, err := ceilScheduleTime(anchor.Add(time.Duration(index+1) * period))
		if err != nil || trigger.NextFireAt == nil || !trigger.NextFireAt.Equal(wanted) ||
			trigger.LastDueAt == nil || !trigger.LastDueAt.Equal(stored) || trigger.IntervalTicks == nil ||
			*trigger.IntervalTicks != ticks || !trigger.AnchorAt.Equal(anchor) || trigger.CalculationError != "" {
			t.Fatalf("database occurrence %d drifted or lost fractional ticks: %+v, %v", index, trigger, err)
		}
		stored = *trigger.NextFireAt
	}
	var records, total, runs int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(occurrence_count),(SELECT count(*) FROM task_runs)
        FROM task_occurrences WHERE disposition='missed'`).Scan(&records, &total, &runs); err != nil {
		t.Fatal(err)
	}
	if records != 20 || total != 20 || runs != 0 {
		t.Fatalf("fractional advancement merged, duplicated, or executed skipped occurrences: records=%d total=%d runs=%d", records, total, runs)
	}
}
