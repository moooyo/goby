package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func taskScheduleAuditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action=$1`, action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestTaskScheduleActivityRecordsOnlyCommittedRevisionFacts(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 1)
	ticks := int64(3600)*ScheduleTicksPerSecond + 1
	request := ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision,
		ScheduleTimezone: "Asia/Shanghai", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks}}}
	saved, err := store.ReplaceTriggers(ctx, actor, request)
	if err != nil {
		t.Fatal(err)
	}
	var source, actorKind, actorID, credentialID, resourceKind, resourceID, requestID, state string
	var revision, affected int64
	var fields []string
	if err := pool.QueryRow(ctx, `SELECT source,actor_kind,actor_id,actor_credential_id,
		resource_kind,resource_id,request_id,state,revision,affected_count,changed_fields
		FROM activity_entries WHERE action='task.schedule_updated'`).Scan(&source, &actorKind,
		&actorID, &credentialID, &resourceKind, &resourceID, &requestID, &state, &revision, &affected, &fields); err != nil {
		t.Fatal(err)
	}
	if source != "native" || actorKind != "user" || actorID != actor.Principal.User.ID ||
		credentialID != actor.Principal.SessionID || resourceKind != "task" || resourceID != definition.ID ||
		requestID != "" || state != "" || revision != saved.Revision || affected != 1 ||
		!reflect.DeepEqual(fields, []string{"ScheduleTimezone", "Triggers"}) {
		t.Fatal("schedule activity lost its source, identity, revision, count, or finite field names")
	}
	if _, err := store.ReplaceTriggers(ctx, actor, request); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale schedule replacement = %v", err)
	}
	if _, err := store.PreviewTriggers(ctx, definition.ID, request.ScheduleTimezone, request.Triggers); err != nil {
		t.Fatal(err)
	}
	if count := taskScheduleAuditCount(t, ctx, pool, "task.schedule_updated"); count != 1 {
		t.Fatalf("stale replacement or preview created activity: %d", count)
	}
	// A fresh replacement retains the established revision and anchor-reset
	// semantics even when its client-configured values are unchanged.
	request.Revision = saved.Revision
	replaced, err := store.ReplaceTriggers(ctx, actor, request)
	if err != nil || replaced.Revision != saved.Revision+1 || replaced.Triggers[0].ID == saved.Triggers[0].ID {
		t.Fatalf("matching replacement did not create its real new rule revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision,changed_fields FROM activity_entries
		WHERE action='task.schedule_updated' ORDER BY id DESC LIMIT 1`).Scan(&revision, &fields); err != nil {
		t.Fatal(err)
	}
	if revision != replaced.Revision || !reflect.DeepEqual(fields, []string{"Triggers"}) ||
		taskScheduleAuditCount(t, ctx, pool, "task.schedule_updated") != 2 {
		t.Fatal("matching replacement lost its revision fact or claimed a timezone change")
	}
	var payload string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM activity_entries a`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, request.ScheduleTimezone) || strings.Contains(payload, "interval_ticks") ||
		strings.Contains(payload, definition.Name) || strings.Contains(payload, "anchor_at") {
		t.Fatal("schedule activity persisted configuration values or display text")
	}
}

func TestTaskScheduledActivityRecordsOneAdmissionForOverlappingOccurrences(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleStartup, ScheduleInterval} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, pool, _, store, actor, definition := taskRepository(t, 1)
			rule := ScheduleRule{Kind: kind}
			ticks := int64(1000) * ScheduleTicksPerSecond
			if kind == ScheduleInterval {
				rule.IntervalTicks = &ticks
			}
			saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule, rule}})
			if err != nil {
				t.Fatal(err)
			}
			now, err := store.ScheduleClock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ScheduleInterval {
				anchor := now.Add(-1001 * time.Second)
				if _, err := pool.Exec(ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3
					WHERE task_id=$1`, definition.ID, anchor, anchor.Add(1000*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if kind == ScheduleStartup {
					if err := store.InitializeSchedules(ctx, now); err != nil {
						t.Fatal(err)
					}
				} else if _, err := store.DispatchDue(ctx, 2); err != nil {
					t.Fatal(err)
				}
			}
			current, err := store.Get(ctx, definition.ID)
			if err != nil || current.CurrentRun == nil {
				t.Fatalf("scheduled run missing: %v", err)
			}
			var source, actorKind, actorID, credentialID, runID, requestID string
			var revision, affected int64
			if err := pool.QueryRow(ctx, `SELECT source,actor_kind,actor_id,actor_credential_id,
				resource_id,request_id,revision,affected_count FROM activity_entries WHERE action='task.admitted'`).
				Scan(&source, &actorKind, &actorID, &credentialID, &runID, &requestID, &revision, &affected); err != nil {
				t.Fatal(err)
			}
			if source != "system" || actorKind != "system" || actorID != "" || credentialID != "" ||
				runID != current.CurrentRun.ID || requestID != "" || revision != saved.Revision || affected != 1 ||
				taskScheduleAuditCount(t, ctx, pool, "task.admitted") != 1 {
				t.Fatal("scheduled overlap or retry duplicated admission or borrowed an administrator actor")
			}
			var admitted, overlap int
			if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE disposition='admitted'),
				count(*) FILTER (WHERE disposition='overlap') FROM task_occurrences`).Scan(&admitted, &overlap); err != nil {
				t.Fatal(err)
			}
			if admitted != 1 || overlap != 1 {
				t.Fatalf("test did not exercise a real coalesced occurrence: admitted=%d overlap=%d", admitted, overlap)
			}
			// A later schedule revision must not become the execution's
			// historical revision when its terminal fact is committed.
			if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: saved.Revision, ScheduleTimezone: "UTC", Triggers: nil}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SystemStopRun(ctx, runID, "shutdown"); err != nil {
				t.Fatal(err)
			}
			var state string
			var finishedRunID string
			if err := pool.QueryRow(ctx, `SELECT source,actor_kind,resource_id,state,revision,affected_count
				FROM activity_entries WHERE action='task.finished'`).Scan(&source, &actorKind, &finishedRunID, &state, &revision, &affected); err != nil {
				t.Fatal(err)
			}
			if source != "system" || actorKind != "system" || finishedRunID != runID || state != "interrupted" ||
				revision != saved.Revision || affected != 1 || taskScheduleAuditCount(t, ctx, pool, "task.finished") != 1 {
				t.Fatal("scheduled completion lost its system authority or immutable revision")
			}
		})
	}
}

func TestTaskActivityRollsBackAtFinalAuthorizationAfterInsertion(t *testing.T) {
	for _, operation := range []string{"admit", "cancel", "schedule"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, scanner, store, actor, definition := taskRepository(t, 1)
			// Keep the post-activity expiry update valid under expires_at >
			// created_at so the failure comes from final authorization.
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 day'
				WHERE id=$1`, actor.Principal.SessionID); err != nil {
				t.Fatal(err)
			}
			var original Run
			var originalChildren ChildPage
			if operation == "cancel" {
				admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
				if err != nil {
					t.Fatal(err)
				}
				original = admission.Run
				originalChildren, err = store.ListChildren(ctx, original.ID, Page{})
				if err != nil {
					t.Fatal(err)
				}
			}
			var before int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			var reached atomic.Bool
			hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(tx library.OwnedTx, statement string) error {
				if strings.HasPrefix(statement, "INSERT INTO activity_entries") && reached.CompareAndSwap(false, true) {
					// The test changes the same transaction's already locked
					// credential after the activity row has been inserted.
					_, err := tx.Exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, actor.Principal.SessionID)
					return err
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "admit":
				_, err = hooked.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "audit-authorization"})
			case "cancel":
				_, err = hooked.Stop(ctx, actor, original.ID)
			case "schedule":
				_, err = hooked.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
			}
			if !reached.Load() || !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("activity did not precede final authorization: reached=%t error=%v", reached.Load(), err)
			}
			var after, runs, children, receipts, triggers int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM activity_entries),
				(SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_children),
				(SELECT count(*) FROM task_run_requests),(SELECT count(*) FROM task_triggers)`).
				Scan(&after, &runs, &children, &receipts, &triggers); err != nil {
				t.Fatal(err)
			}
			if after != before || receipts != 0 || triggers != 0 ||
				(operation != "cancel" && (runs != 0 || children != 0)) {
				t.Fatal("final authorization failure leaked business or activity rows")
			}
			if operation == "cancel" {
				afterRun, err := store.GetRun(ctx, original.ID)
				if err != nil || !reflect.DeepEqual(afterRun, original) {
					t.Fatalf("failed cancellation changed the run: %v", err)
				}
				afterChildren, err := store.ListChildren(ctx, original.ID, Page{})
				if err != nil || !reflect.DeepEqual(afterChildren, originalChildren) {
					t.Fatalf("failed cancellation changed the child snapshots: %v", err)
				}
			}
			current, err := store.Get(ctx, definition.ID)
			if err != nil || current.Revision != definition.Revision {
				t.Fatalf("failed mutation advanced the schedule revision: %v", err)
			}
		})
	}
}

func TestTaskActivityCommitFailureReturnsNoAdmissionAndRetainsNoRows(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 0)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION task_audit_fail_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected task commit failure'; END $$;
		CREATE CONSTRAINT TRIGGER task_audit_deferred_failure AFTER INSERT ON activity_entries
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION task_audit_fail_commit()`); err != nil {
		t.Fatal(err)
	}
	var inserted atomic.Int32
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO activity_entries") {
			inserted.Add(1)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := hooked.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "deferred-commit"})
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "P0001" || inserted.Load() != 2 ||
		result.Admitted || result.Run.ID != "" {
		t.Fatalf("failed commit reported success or did not reach both empty-run facts: inserted=%d error=%v", inserted.Load(), err)
	}
	var activities, runs, receipts int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM activity_entries),
		(SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_requests)`).Scan(&activities, &runs, &receipts); err != nil {
		t.Fatal(err)
	}
	if activities != 0 || runs != 0 || receipts != 0 {
		t.Fatal("failed commit retained an admission, receipt, or activity fact")
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER task_audit_deferred_failure ON activity_entries`); err != nil {
		t.Fatal(err)
	}
	retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "deferred-commit"})
	if err != nil || !retry.Admitted || retry.Run.State != RunCompleted ||
		taskScheduleAuditCount(t, ctx, pool, "task.admitted") != 1 || taskScheduleAuditCount(t, ctx, pool, "task.finished") != 1 {
		t.Fatalf("failed commit prevented one truthful admission on retry: %v", err)
	}
}

func TestTaskScheduledActivityRollsBackBeforeOccurrenceReceipt(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 0)
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	startupAt, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected occurrence persistence failure")
	var activityWrites atomic.Int32
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO activity_entries") {
			activityWrites.Add(1)
		}
		if strings.HasPrefix(statement, "INSERT INTO task_occurrences") {
			return injected
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := hooked.InitializeSchedules(ctx, startupAt); !errors.Is(err, injected) || activityWrites.Load() != 2 {
		t.Fatalf("scheduled failure did not follow empty-run admission and completion: %v", err)
	}
	var runs, receipts int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM task_occurrences)`).Scan(&runs, &receipts); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || receipts != 0 || taskScheduleAuditCount(t, ctx, pool, "task.admitted") != 0 ||
		taskScheduleAuditCount(t, ctx, pool, "task.finished") != 0 {
		t.Fatal("failed occurrence receipt retained scheduled activity or business state")
	}
	for range 2 {
		if err := store.InitializeSchedules(ctx, startupAt); err != nil {
			t.Fatal(err)
		}
	}
	if taskScheduleAuditCount(t, ctx, pool, "task.admitted") != 1 || taskScheduleAuditCount(t, ctx, pool, "task.finished") != 1 {
		t.Fatal("startup retry duplicated or omitted the committed empty-run facts")
	}
}
