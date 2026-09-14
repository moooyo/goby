package tasks

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func TestRefreshMediaRegistryPreservesScanStateAndDefaultsToNativeManualUse(t *testing.T) {
	ctx, pool, _, store, actor, scan := taskRepository(t, 0)
	// Recreate the pre-upgrade registry inside this fixture's isolated schema.
	deleted, err := pool.Exec(ctx, `DELETE FROM task_definitions WHERE key=$1`, LibraryRefreshMediaKey)
	if err != nil || deleted.RowsAffected() != 1 {
		t.Fatalf("remove only the fresh definition before registry reconciliation: %v", err)
	}
	interval := int64(12*3600) * ScheduleTicksPerSecond
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: scan.ID,
		Revision: scan.Revision, ScheduleTimezone: "Asia/Shanghai", Triggers: []ScheduleRule{
			{Kind: ScheduleInterval, IntervalTicks: &interval}, {Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	startupAt, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchedules(ctx, startupAt); err != nil {
		t.Fatal(err)
	}
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: scan.ID, RequestID: "retained-scan-history"})
	if err != nil || !admission.Admitted || admission.Run.State != RunCompleted {
		t.Fatalf("create retained scan history: state=%s error=%v", admission.Run.State, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_definitions SET enabled=false, revision=revision+7 WHERE id=$1`, scan.ID); err != nil {
		t.Fatal(err)
	}
	before := refreshRegistrySnapshot(t, ctx, pool, scan.ID)
	var refresh Definition
	for attempt := range 2 {
		if err := store.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		if after := refreshRegistrySnapshot(t, ctx, pool, scan.ID); after != before {
			t.Fatal("registering refresh media rewrote the existing scan definition, schedules, history, receipts, or row versions")
		}
		definitions, err := store.List(ctx, ListOptions{})
		if err != nil || len(definitions) != 2 {
			t.Fatalf("registry must expose exactly the two executable definitions: count=%d error=%v", len(definitions), err)
		}
		current, err := store.GetByKey(ctx, LibraryScanKey)
		if err != nil || current.ID != scan.ID || current.Key != "library.scan" || current.EmbyKey != "RefreshLibrary" ||
			current.Enabled || current.Revision != saved.Revision+7 || current.ScheduleTimezone != "Asia/Shanghai" ||
			len(current.Triggers) != 2 || current.LastRun == nil || current.LastRun.ID != admission.Run.ID {
			t.Fatalf("existing scan registration lost an administrator choice or stable identity: %v", err)
		}
		fresh, err := store.GetByKey(ctx, LibraryRefreshMediaKey)
		if err != nil || len(fresh.ID) != 32 || fresh.ID == scan.ID || fresh.Key != "library.refresh_media" ||
			fresh.EmbyKey != "" || !fresh.Enabled || fresh.IsHidden || fresh.Revision != 1 || fresh.ScheduleTimezone != "UTC" ||
			len(fresh.Triggers) != 0 || fresh.CurrentRun != nil || fresh.LastRun != nil {
			t.Fatalf("refresh media did not retain its native manual-only defaults: %v", err)
		}
		if attempt > 0 && !reflect.DeepEqual(fresh, refresh) {
			t.Fatal("repeated reconciliation changed the fresh definition")
		}
		refresh = fresh
	}
	refreshBefore := refreshRegistrySnapshot(t, ctx, pool, refresh.ID)
	if err := store.InitializeSchedules(ctx, startupAt); err != nil {
		t.Fatal(err)
	}
	if next, err := store.NextDue(ctx); err != nil || next != nil {
		t.Fatalf("an unscheduled refresh or disabled scan became a timer: next=%v error=%v", next, err)
	}
	if changed, err := store.DispatchDue(ctx, 2); err != nil || changed {
		t.Fatalf("the default registry admitted automatic work: changed=%t error=%v", changed, err)
	}
	emby := taskCompatibilityActor(t, ctx, pool, actor)
	if _, err := store.Start(ctx, emby, StartRequest{TaskID: refresh.ID, RequestID: "forbidden-refresh"}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("compatibility audience admitted the native refresh task: %v", err)
	}
	if _, err := store.ReplaceTriggers(ctx, emby, ReplaceTriggersRequest{TaskID: refresh.ID,
		Revision: refresh.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("compatibility audience scheduled the native refresh task: %v", err)
	}
	if refreshRegistrySnapshot(t, ctx, pool, refresh.ID) != refreshBefore || refreshRegistrySnapshot(t, ctx, pool, scan.ID) != before {
		t.Fatal("default scheduler processing or rejected compatibility requests changed task state")
	}
	manual, err := store.Start(ctx, actor, StartRequest{TaskID: refresh.ID, RequestID: "native-refresh"})
	if err != nil || !manual.Admitted || manual.Run.State != RunCompleted || manual.Run.Source != "manual" ||
		manual.Run.TaskID != refresh.ID || manual.Run.TaskKey != LibraryRefreshMediaKey || manual.Run.TaskEmbyKey != "" ||
		manual.Run.TotalChildren != 0 || manual.Run.TriggerID != nil || manual.Run.TriggerRevision != nil || manual.Run.ScheduledFor != nil {
		t.Fatalf("native manual refresh did not complete its empty-library snapshot: state=%s error=%v", manual.Run.State, err)
	}
}

func TestRefreshMediaStartupInitializesBothDefinitionsAtTheirRuleLimit(t *testing.T) {
	ctx, pool, _, store, actor, scan := taskRepository(t, 0)
	refresh, err := store.GetByKey(ctx, LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]ScheduleRule, MaxTriggers)
	for index := range rules {
		rules[index] = ScheduleRule{Kind: ScheduleStartup}
	}
	definitions := []Definition{scan, refresh}
	for index, definition := range definitions {
		definitions[index], err = store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
			Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: rules})
		if err != nil {
			t.Fatal(err)
		}
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
	for _, definition := range definitions {
		current, err := store.Get(ctx, definition.ID)
		if err != nil || len(current.Triggers) != MaxTriggers {
			t.Fatalf("startup lost rules for %s: %v", definition.Key, err)
		}
		triggerIDs := make(map[string]bool, MaxTriggers)
		for _, trigger := range current.Triggers {
			if trigger.TaskID != definition.ID || trigger.ScheduleRevision != definition.Revision ||
				trigger.Kind != string(ScheduleStartup) || trigger.NextFireAt != nil || trigger.LastDueAt == nil ||
				!trigger.LastDueAt.Equal(startupAt) || trigger.CalculationError != "" {
				t.Fatalf("startup did not initialize every rule for %s", definition.Key)
			}
			triggerIDs[trigger.ID] = true
		}
		history, err := store.ListRuns(ctx, definition.ID, Page{Limit: MaxPageLimit})
		if err != nil || history.TotalRecordCount != int64(MaxTriggers) || len(history.Items) != MaxTriggers {
			t.Fatalf("startup skipped or replayed a definition: key=%s count=%d error=%v", definition.Key, history.TotalRecordCount, err)
		}
		for _, run := range history.Items {
			if run.State != RunCompleted || run.Source != "startup" || run.TaskID != definition.ID || run.TaskKey != definition.Key ||
				run.TaskEmbyKey != definition.EmbyKey || run.TriggerID == nil || !triggerIDs[*run.TriggerID] ||
				run.TriggerRevision == nil || *run.TriggerRevision != definition.Revision || run.ScheduledFor == nil ||
				!run.ScheduledFor.Equal(startupAt) || run.StartedAt == nil || run.FinishedAt == nil || run.TotalChildren != 0 ||
				run.ActorKind != "" || run.ActorUserID != "" || run.ActorSessionID != "" {
				t.Fatalf("startup lost the admitted definition or trigger snapshot for %s", definition.Key)
			}
			delete(triggerIDs, *run.TriggerID)
		}
		if len(triggerIDs) != 0 {
			t.Fatal("startup history omitted a trigger")
		}
		var occurrences, triggers, runs, exact int
		if err := pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT trigger_id), count(DISTINCT run_id),
			count(*) FILTER (WHERE disposition='admitted' AND due_at=$2 AND occurrence_count=1 AND last_due_at IS NULL)
			FROM task_occurrences WHERE task_id=$1`, definition.ID, startupAt).Scan(&occurrences, &triggers, &runs, &exact); err != nil {
			t.Fatal(err)
		}
		if occurrences != MaxTriggers || triggers != MaxTriggers || runs != MaxTriggers || exact != MaxTriggers {
			t.Fatalf("startup occurrence receipts are incomplete or duplicated for %s: %d/%d/%d/%d", definition.Key, occurrences, triggers, runs, exact)
		}
	}
	if next, err := store.NextDue(ctx); err != nil || next != nil {
		t.Fatalf("startup rules became timed wakeups: next=%v error=%v", next, err)
	}
	if changed, err := store.DispatchDue(ctx, 1); err != nil || changed {
		t.Fatalf("startup receipts were replayed as timer work: changed=%t error=%v", changed, err)
	}
}

func TestRefreshMediaTimedDispatchDoesNotStarveEitherDefinition(t *testing.T) {
	ctx, pool, _, store, actor, scan := taskRepository(t, 0)
	refresh, err := store.GetByKey(ctx, LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal(err)
	}
	interval := int64(12*3600) * ScheduleTicksPerSecond
	definitions := make(map[string]Definition, 2)
	for _, definition := range []Definition{scan, refresh} {
		saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
			Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &interval}}})
		if err != nil || len(saved.Triggers) != 1 {
			t.Fatalf("install a timed rule for %s: %v", definition.Key, err)
		}
		definitions[definition.Key] = saved
	}
	// Reverse the earliest definition without sleeping or changing the host clock.
	// Each round has exactly one past occurrence per rule and a future successor.
	for round, order := range [][]string{{LibraryScanKey, LibraryRefreshMediaKey}, {LibraryRefreshMediaKey, LibraryScanKey}} {
		now, err := store.ScheduleClock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		due := make(map[string]time.Time, 2)
		for index, key := range order {
			due[key] = now.Add(-time.Duration(4-2*round-index) * time.Hour)
			definition := definitions[key]
			if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`,
				definition.Triggers[0].ID, due[key].Add(-12*time.Hour), due[key]); err != nil {
				t.Fatal(err)
			}
		}
		for index, key := range order {
			if next, err := store.NextDue(ctx); err != nil || next == nil || !next.Equal(due[key]) {
				t.Fatalf("another definition masked the earliest due rule: key=%s next=%v error=%v", key, next, err)
			}
			if changed, err := store.DispatchDue(ctx, 1); err != nil || !changed {
				t.Fatalf("bounded dispatch starved %s: changed=%t error=%v", key, changed, err)
			}
			for otherIndex, otherKey := range order {
				definition := definitions[otherKey]
				wantRuns := round
				if otherIndex <= index {
					wantRuns++
				}
				history, err := store.ListRuns(ctx, definition.ID, Page{Limit: 2})
				if err != nil || history.TotalRecordCount != int64(wantRuns) || len(history.Items) != wantRuns {
					t.Fatalf("single-rule dispatch advanced the wrong definition: key=%s count=%d want=%d error=%v", otherKey, history.TotalRecordCount, wantRuns, err)
				}
			}
		}
		for _, key := range order {
			definition := definitions[key]
			current, err := store.Get(ctx, definition.ID)
			if err != nil || current.CurrentRun != nil || current.LastRun == nil || len(current.Triggers) != 1 {
				t.Fatalf("timed dispatch did not retain a completed run for %s: %v", key, err)
			}
			run, trigger := current.LastRun, current.Triggers[0]
			if run.State != RunCompleted || run.Source != "schedule" || run.TaskID != definition.ID || run.TaskKey != key ||
				run.TaskEmbyKey != definition.EmbyKey || run.TriggerID == nil || *run.TriggerID != trigger.ID ||
				run.TriggerRevision == nil || *run.TriggerRevision != definition.Revision || run.ScheduledFor == nil ||
				!run.ScheduledFor.Equal(due[key]) || run.StartedAt == nil || run.FinishedAt == nil || run.TotalChildren != 0 ||
				trigger.LastDueAt == nil || !trigger.LastDueAt.Equal(due[key]) || trigger.NextFireAt == nil ||
				!trigger.NextFireAt.Equal(due[key].Add(12*time.Hour)) || trigger.CalculationError != "" {
				t.Fatalf("timed dispatch lost or drifted the independent occurrence for %s", key)
			}
			var occurrences, exact int
			if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE disposition='admitted' AND occurrence_count=1)
				FROM task_occurrences WHERE task_id=$1`, definition.ID).Scan(&occurrences, &exact); err != nil || occurrences != round+1 || exact != round+1 {
				t.Fatalf("timed dispatch replayed history or skipped a due definition: key=%s occurrences=%d exact=%d error=%v", key, occurrences, exact, err)
			}
		}
		if next, err := store.NextDue(ctx); err != nil || next == nil || !next.Equal(due[order[0]].Add(12*time.Hour)) {
			t.Fatalf("next wakeup did not advance beyond both due definitions: next=%v error=%v", next, err)
		}
		if changed, err := store.DispatchDue(ctx, 1); err != nil || changed {
			t.Fatalf("completed due rules caused a dispatch hot loop: changed=%t error=%v", changed, err)
		}
	}
}

func refreshRegistrySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'definition', (SELECT to_jsonb(d) || jsonb_build_object('row_version',d.xmin::text) FROM task_definitions d WHERE d.id=$1),
		'triggers', (SELECT COALESCE(jsonb_agg(to_jsonb(t) || jsonb_build_object('row_version',t.xmin::text) ORDER BY t.id),'[]'::jsonb) FROM task_triggers t WHERE t.task_id=$1),
		'runs', (SELECT COALESCE(jsonb_agg(to_jsonb(r) || jsonb_build_object('row_version',r.xmin::text) ORDER BY r.id),'[]'::jsonb) FROM task_runs r WHERE r.task_id=$1),
		'requests', (SELECT COALESCE(jsonb_agg(to_jsonb(q) || jsonb_build_object('row_version',q.xmin::text) ORDER BY q.request_id),'[]'::jsonb) FROM task_run_requests q WHERE q.task_id=$1),
		'occurrences', (SELECT COALESCE(jsonb_agg(to_jsonb(o) || jsonb_build_object('row_version',o.xmin::text) ORDER BY o.id),'[]'::jsonb) FROM task_occurrences o WHERE o.task_id=$1),
		'children', (SELECT COALESCE(jsonb_agg(to_jsonb(c) || jsonb_build_object('row_version',c.xmin::text) ORDER BY c.id),'[]'::jsonb)
			FROM task_run_children c JOIN task_runs r ON r.id=c.run_id WHERE r.task_id=$1))::text`, taskID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
