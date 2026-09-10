package tasks

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestTaskTriggersReplacementPreviewAndEmptySchedule(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 1)
	ticks := int64(3600)*ScheduleTicksPerSecond + 1
	timeOfDay := int64(2*3600+30*60) * ScheduleTicksPerSecond
	zero := int64(0)
	input := []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks},
		{Kind: ScheduleDaily, TimeOfDayTicks: &timeOfDay}, {Kind: ScheduleStartup, MaxRuntimeTicks: &zero}}
	replaced, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "America/New_York", Triggers: input})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Revision != definition.Revision+1 || replaced.ScheduleTimezone != "America/New_York" || len(replaced.Triggers) != 3 {
		t.Fatalf("replacement did not produce a complete new revision: %+v", replaced)
	}
	interval := replaced.Triggers[0]
	if interval.AnchorAt == nil || interval.NextFireAt == nil || interval.AnchorAt.Location() != time.UTC ||
		interval.NextFireAt.Location() != time.UTC || interval.AnchorAt.Nanosecond()%1000 != 0 ||
		interval.NextFireAt.Nanosecond()%1000 != 0 || !interval.AnchorAt.Equal(interval.CreatedAt) {
		t.Fatal("interval anchor or due did not use normalized database microseconds")
	}
	exact := interval.AnchorAt.Add(time.Hour + 100*time.Nanosecond)
	if interval.NextFireAt.Before(exact) || interval.NextFireAt.Sub(exact) >= time.Microsecond {
		t.Fatal("new interval first due was early or not one period after the database anchor")
	}
	if input[0].AnchorAt != nil || input[1].Timezone != "" || input[2].MaxRuntimeTicks == nil ||
		*input[2].MaxRuntimeTicks != 0 || replaced.Triggers[2].MaxRuntimeTicks != nil || replaced.Triggers[2].NextFireAt != nil {
		t.Fatal("replacement mutated input, persisted unlimited runtime as zero, or assigned startup a due time")
	}
	for index, trigger := range replaced.Triggers {
		if trigger.Position != index || trigger.ScheduleRevision != replaced.Revision || trigger.CalculationError != "" {
			t.Fatal("new trigger position, revision, or calculation status is inconsistent")
		}
	}
	preview, err := store.PreviewTriggers(ctx, definition.ID, "America/New_York", input)
	if err != nil || len(preview.Items) != 3 || preview.ServerTime.Location() != time.UTC {
		t.Fatalf("preview failed: %+v, %v", preview, err)
	}
	for index, item := range preview.Items {
		if item.Index != index || item.Occurrences == nil {
			t.Fatal("preview lost ordering or its empty-array contract")
		}
		if index == 2 {
			if item.Event == nil || *item.Event != "startup" || len(item.Occurrences) != 0 {
				t.Fatal("startup preview was represented as a time")
			}
		} else if item.Event != nil || len(item.Occurrences) != 3 || !item.Occurrences[0].After(preview.ServerTime) {
			t.Fatal("timed preview was not three strict future instants")
		}
	}
	unchanged, err := store.Get(ctx, definition.ID)
	if err != nil || !reflect.DeepEqual(unchanged, replaced) {
		t.Fatalf("preview mutated persisted state: %v", err)
	}
	empty, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: replaced.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{}})
	if err != nil || len(empty.Triggers) != 0 || empty.Revision != replaced.Revision+1 {
		t.Fatalf("empty schedule replacement failed: %+v, %v", empty, err)
	}
	var total, retired, runs int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_triggers),
        (SELECT count(*) FROM task_triggers WHERE retired_at IS NOT NULL),(SELECT count(*) FROM task_runs)`).Scan(&total, &retired, &runs); err != nil {
		t.Fatal(err)
	}
	if total != 3 || retired != 3 || runs != 0 {
		t.Fatal("replacement deleted history or started work")
	}
	if next, err := store.NextDue(ctx); err != nil || next != nil {
		t.Fatalf("retired schedule remained due: %v, %v", next, err)
	}
	if admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID}); err != nil || !admission.Admitted {
		t.Fatalf("empty automatic schedule disabled manual execution: %v", err)
	}
}

func TestTaskTriggersInvalidAndStaleReplacementDoNotChangeRows(t *testing.T) {
	ctx, _, _, store, actor, definition := taskRepository(t, 0)
	ticks := int64(3600) * ScheduleTicksPerSecond
	base := ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision,
		ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks}}}
	saved, err := store.ReplaceTriggers(ctx, actor, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceTriggers(ctx, actor, base); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision did not conflict: %v", err)
	}
	anchor := time.Now().UTC()
	for _, test := range []struct {
		name string
		edit func(*ReplaceTriggersRequest)
	}{
		{"late-invalid-rule", func(r *ReplaceTriggersRequest) { r.Triggers = append(r.Triggers, ScheduleRule{Kind: ScheduleWeekly}) }},
		{"caller-anchor", func(r *ReplaceTriggersRequest) { r.Triggers[0].AnchorAt = &anchor }},
		{"caller-zone", func(r *ReplaceTriggersRequest) { r.Triggers[0].Timezone = "UTC" }},
		{"implicit-zone", func(r *ReplaceTriggersRequest) { r.ScheduleTimezone = "Local" }},
		{"path-zone", func(r *ReplaceTriggersRequest) { r.ScheduleTimezone = "../UTC" }},
		{"too-many", func(r *ReplaceTriggersRequest) { r.Triggers = make([]ScheduleRule, MaxTriggers+1) }},
		{"revision-overflow", func(r *ReplaceTriggersRequest) { r.Revision = math.MaxInt64 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.Revision = saved.Revision
			request.Triggers = append([]ScheduleRule(nil), base.Triggers...)
			test.edit(&request)
			if _, err := store.ReplaceTriggers(ctx, actor, request); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid replacement was not rejected: %v", err)
			}
			current, err := store.Get(ctx, definition.ID)
			if err != nil || !reflect.DeepEqual(current, saved) {
				t.Fatalf("invalid replacement partially changed rows: %v", err)
			}
		})
	}
	if _, err := store.PreviewTriggers(ctx, "missing-task", "UTC", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("preview fabricated a missing task: %v", err)
	}
}

func TestTaskTriggersFinalAuthorizationRollsBackWholeReplacement(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 0)
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	var reached atomic.Bool
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(tx library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO task_triggers") && reached.CompareAndSwap(false, true) {
			_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
                (expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
			return err
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = hooked.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: saved.Revision, ScheduleTimezone: "Asia/Shanghai", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
	if !reached.Load() || !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("final authorization barrier was not enforced: reached=%t error=%v", reached.Load(), err)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil || !reflect.DeepEqual(current, saved) {
		t.Fatalf("failed final authorization leaked replacement changes: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_triggers`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed authorization left retired or inserted history: count=%d error=%v", count, err)
	}
}

func TestTaskTriggersReplacementOutlivesCallerCancellation(t *testing.T) {
	ctx, pool, scanner, _, actor, definition := taskRepository(t, 0)
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	var reached atomic.Bool
	store, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "UPDATE task_definitions SET revision") && reached.CompareAndSwap(false, true) {
			cancel()
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ReplaceTriggers(caller, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
	if err != nil || !reached.Load() || !errors.Is(caller.Err(), context.Canceled) || len(result.Triggers) != 1 {
		t.Fatalf("caller cancellation left an incomplete revision: %v", err)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil || !reflect.DeepEqual(current, result) {
		t.Fatalf("replacement result did not reflect its committed revision: %v", err)
	}
}
