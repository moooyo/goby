package tasks

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

type taskLifecycleAuditEntry struct {
	Action            string
	Severity          string
	Source            string
	ActorKind         string
	ActorID           string
	ActorCredentialID string
	ResourceKind      string
	ResourceID        string
	State             string
	Revision          int64
	AffectedCount     int64
	RequestID         string
}

func taskLifecycleUserEntry(action, source, runID string, actor Actor, revision, count int64) taskLifecycleAuditEntry {
	return taskLifecycleAuditEntry{
		Action: action, Severity: "Info", Source: source, ActorKind: "user", ActorID: actor.Principal.User.ID,
		ActorCredentialID: actor.Principal.SessionID, ResourceKind: "task_run", ResourceID: runID,
		Revision: revision, AffectedCount: count,
	}
}

func taskLifecycleFinishedEntry(runID string, state RunState, count int64) taskLifecycleAuditEntry {
	severity := "Info"
	if state == RunFailed {
		severity = "Error"
	} else if state == RunInterrupted {
		severity = "Warn"
	}
	return taskLifecycleAuditEntry{
		Action: "task.finished", Severity: severity, Source: "system", ActorKind: "system",
		ResourceKind: "task_run", ResourceID: runID, State: string(state), AffectedCount: count,
	}
}

func assertTaskLifecycleAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string, expected ...taskLifecycleAuditEntry) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT action,severity,source,actor_kind,actor_id,actor_credential_id,
		resource_kind,resource_id,state,revision,affected_count,request_id
		FROM activity_entries WHERE resource_kind = 'task_run' AND resource_id = $1 ORDER BY id`, runID)
	if err != nil {
		t.Fatalf("read task lifecycle activity: %v", err)
	}
	defer rows.Close()
	actual := make([]taskLifecycleAuditEntry, 0)
	for rows.Next() {
		var entry taskLifecycleAuditEntry
		if err := rows.Scan(&entry.Action, &entry.Severity, &entry.Source, &entry.ActorKind, &entry.ActorID,
			&entry.ActorCredentialID, &entry.ResourceKind, &entry.ResourceID, &entry.State,
			&entry.Revision, &entry.AffectedCount, &entry.RequestID); err != nil {
			t.Fatalf("decode task lifecycle activity: %v", err)
		}
		actual = append(actual, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish reading task lifecycle activity: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("task lifecycle activity = %+v, want %+v", actual, expected)
	}
}

func TestTaskActivityAdmissionReceiptsAndTerminalRetries(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 2)
	first, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "original-client-request"})
	if err != nil || !first.Admitted || first.Run.State != RunPending {
		t.Fatalf("admit original task run: %+v, %v", first, err)
	}
	admitted := taskLifecycleUserEntry("task.admitted", "native", first.Run.ID, actor, definition.Revision, 2)
	assertTaskLifecycleAudit(t, ctx, pool, first.Run.ID, admitted)
	for _, requestID := range []string{"original-client-request", "coalesced-client-request", ""} {
		retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: requestID})
		if err != nil || retry.Admitted || retry.Run.ID != first.Run.ID {
			t.Fatalf("retry or coalesce request %q: %+v, %v", requestID, retry, err)
		}
	}
	assertTaskLifecycleAudit(t, ctx, pool, first.Run.ID, admitted)
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_run_requests WHERE run_id=$1`, first.Run.ID).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("accepted request receipts = %d, want 2: %v", receipts, err)
	}
	// A durable child result may precede the coordinator's aggregate refresh.
	// Cancellation counts the locked child state, not the stale parent counter.
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		_, err := tx.Exec(`UPDATE task_run_children SET state='completed',finished_at=clock_timestamp()
			WHERE run_id=$1 AND ordinal=0`, first.Run.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.Stop(ctx, actor, first.Run.ID)
	if err != nil || stopped.State != RunCancelled || stopped.TerminalChildren != 2 {
		t.Fatalf("stop pending task run: %+v, %v", stopped, err)
	}
	expected := []taskLifecycleAuditEntry{
		admitted,
		taskLifecycleUserEntry("task.cancel_requested", "native", first.Run.ID, actor, 0, 1),
		taskLifecycleFinishedEntry(first.Run.ID, RunCancelled, 2),
	}
	assertTaskLifecycleAudit(t, ctx, pool, first.Run.ID, expected...)
	for range 2 {
		if terminal, err := store.Stop(ctx, actor, first.Run.ID); err != nil || !reflect.DeepEqual(terminal, stopped) {
			t.Fatalf("repeat terminal stop changed its result: %+v, %v", terminal, err)
		}
		if terminal, err := store.RefreshRun(ctx, first.Run.ID); err != nil || !reflect.DeepEqual(terminal, stopped) {
			t.Fatalf("repeat terminal refresh changed its result: %+v, %v", terminal, err)
		}
		for _, requestID := range []string{"original-client-request", "coalesced-client-request"} {
			retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: requestID})
			if err != nil || retry.Admitted || !reflect.DeepEqual(retry.Run, stopped) {
				t.Fatalf("terminal request %q did not reuse its receipt: %+v, %v", requestID, retry, err)
			}
		}
	}
	assertTaskLifecycleAudit(t, ctx, pool, first.Run.ID, expected...)
}

func TestTaskActivityStopOnlyRecordsFirstRequestWhileStopping(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 2)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil || len(children.Items) != 2 {
		t.Fatalf("read task children: %+v, %v", children, err)
	}
	owned := children.Items[0]
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE task_run_children SET state='running',scan_job_id='activity-owned-scan',
			started_at=clock_timestamp() WHERE id=$1`, owned.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at)
			VALUES ('activity-owned-scan',$1,'Running',$2,clock_timestamp())`, owned.LibraryID, owned.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	admitted := taskLifecycleUserEntry("task.admitted", "native", admission.Run.ID, actor, definition.Revision, 2)
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted)
	stopping, err := store.Stop(ctx, actor, admission.Run.ID)
	if err != nil || stopping.State != RunStopping || stopping.TerminalChildren != 1 {
		t.Fatalf("stop did not wait for the running owned scan: %+v, %v", stopping, err)
	}
	cancelled := taskLifecycleUserEntry("task.cancel_requested", "native", admission.Run.ID, actor, 0, 2)
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted, cancelled)
	for range 2 {
		if repeated, err := store.Stop(ctx, actor, admission.Run.ID); err != nil || repeated.State != RunStopping || repeated.StopReason != "administrator" {
			t.Fatalf("repeat stopping request changed its first reason: %+v, %v", repeated, err)
		}
		if repeated, err := store.SystemStopRun(ctx, admission.Run.ID, "shutdown"); err != nil || repeated.State != RunStopping || repeated.StopReason != "administrator" {
			t.Fatalf("system stop replaced the first user request: %+v, %v", repeated, err)
		}
		if repeated, err := store.RefreshRun(ctx, admission.Run.ID); err != nil || repeated.State != RunStopping {
			t.Fatalf("refresh prematurely finished the owned scan: %+v, %v", repeated, err)
		}
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted, cancelled)
	var scanCancellationCount int
	var scanSource, scanActor, scanActorID, scanCredential string
	if err := pool.QueryRow(ctx, `SELECT count(*),min(source),min(actor_kind),min(actor_id),min(actor_credential_id)
		FROM activity_entries WHERE action='scan.cancel_requested' AND resource_kind='scan'
		AND resource_id='activity-owned-scan'`).Scan(&scanCancellationCount, &scanSource,
		&scanActor, &scanActorID, &scanCredential); err != nil {
		t.Fatal(err)
	}
	if scanCancellationCount != 1 || scanSource != "system" || scanActor != "system" || scanActorID != "" || scanCredential != "" {
		t.Fatal("task cancellation failed to record exactly one system cancellation for its owned scan")
	}
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status='Cancelled',finished_at=clock_timestamp()
			WHERE id='activity-owned-scan'`); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE task_run_children SET state='cancelled',finished_at=clock_timestamp() WHERE id=$1`, owned.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		terminal, err := store.RefreshRun(ctx, admission.Run.ID)
		if err != nil || terminal.State != RunCancelled || terminal.TerminalChildren != 2 {
			t.Fatalf("refresh did not persist owned completion: %+v, %v", terminal, err)
		}
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted, cancelled,
		taskLifecycleFinishedEntry(admission.Run.ID, RunCancelled, 2))
}

func TestTaskActivityEmbyAdmissionAndCompatibilityCancellation(t *testing.T) {
	ctx, pool, _, store, native, definition := taskRepository(t, 1)
	actor := taskCompatibilityActor(t, ctx, pool, native)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "compatibility-client-request"})
	if err != nil || !admission.Admitted || admission.Run.Source != "compatibility" {
		t.Fatalf("admit compatibility task: %+v, %v", admission, err)
	}
	admitted := taskLifecycleUserEntry("task.admitted", "emby", admission.Run.ID, actor, definition.Revision, 1)
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted)
	if _, err := store.Start(ctx, native, StartRequest{TaskID: definition.ID, RequestID: "compatibility-client-request"}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("source fingerprint mismatch = %v, want request conflict", err)
	}
	stopped, err := store.StopByDefinition(ctx, actor, definition.ID)
	if err != nil || stopped.State != RunCancelled {
		t.Fatalf("cancel compatibility task: %+v, %v", stopped, err)
	}
	if _, err := store.StopByDefinition(ctx, actor, definition.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("repeat compatibility cancellation = %v, want not running", err)
	}
	if _, err := store.Stop(ctx, native, admission.Run.ID); err != nil {
		t.Fatalf("native terminal cancellation: %v", err)
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted,
		taskLifecycleUserEntry("task.cancel_requested", "emby", admission.Run.ID, actor, 0, 1),
		taskLifecycleFinishedEntry(admission.Run.ID, RunCancelled, 1))
}

func TestTaskActivityEmptyLibraryAdmissionPrecedesCompletion(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 0)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "empty-client-request"})
	if err != nil || !admission.Admitted || admission.Run.State != RunCompleted || admission.Run.StartedAt == nil || admission.Run.FinishedAt == nil {
		t.Fatalf("empty-library task did not complete in admission: %+v, %v", admission, err)
	}
	expected := []taskLifecycleAuditEntry{
		taskLifecycleUserEntry("task.admitted", "native", admission.Run.ID, actor, definition.Revision, 0),
		taskLifecycleFinishedEntry(admission.Run.ID, RunCompleted, 0),
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, expected...)
	retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "empty-client-request"})
	if err != nil || retry.Admitted || !reflect.DeepEqual(retry.Run, admission.Run) {
		t.Fatalf("empty-library retry changed the original result: %+v, %v", retry, err)
	}
	if _, err := store.Stop(ctx, actor, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RefreshRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, expected...)
}

func TestTaskActivityRecoveryRecordsOneSystemTerminalTransition(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 2)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recovery-client-request"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	admitted := taskLifecycleUserEntry("task.admitted", "native", admission.Run.ID, actor, definition.Revision, 2)
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted)
	for range 2 {
		if err := store.RecoverRuns(ctx); err != nil {
			t.Fatalf("recover abandoned task: %v", err)
		}
		terminal, err := store.GetRun(ctx, admission.Run.ID)
		if err != nil || terminal.State != RunInterrupted || terminal.InterruptedChildren != 2 || terminal.TerminalChildren != 2 {
			t.Fatalf("recovery did not preserve an interrupted terminal result: %+v, %v", terminal, err)
		}
		assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted,
			taskLifecycleFinishedEntry(admission.Run.ID, RunInterrupted, 2))
	}
	retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recovery-client-request"})
	if err != nil || retry.Admitted || retry.Run.ID != admission.Run.ID || retry.Run.State != RunInterrupted {
		t.Fatalf("recovery retry did not reuse its receipt: %+v, %v", retry, err)
	}
	if _, err := store.RefreshRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted,
		taskLifecycleFinishedEntry(admission.Run.ID, RunInterrupted, 2))
}

func TestTaskActivityRefreshRecordsCompletionAndFailureExactlyOnce(t *testing.T) {
	for _, test := range []struct {
		name       string
		childState ChildState
		runState   RunState
	}{
		{name: "completed", childState: ChildCompleted, runState: RunCompleted},
		{name: "failed", childState: ChildFailed, runState: RunFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool, scanner, store, actor, definition := taskRepository(t, 2)
			admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
				t.Fatal(err)
			}
			admitted := taskLifecycleUserEntry("task.admitted", "native", admission.Run.ID, actor, definition.Revision, 2)
			if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				_, err := tx.Exec(`UPDATE task_run_children SET state='completed',
					finished_at=clock_timestamp(),scanned=3,added=1 WHERE run_id=$1 AND ordinal=0`, admission.Run.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			partial, err := store.RefreshRun(ctx, admission.Run.ID)
			if err != nil || partial.State != RunRunning || partial.TerminalChildren != 1 {
				t.Fatalf("partial child results ended the task early: %+v, %v", partial, err)
			}
			assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted)
			if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				_, err := tx.Exec(`UPDATE task_run_children SET state=$2,finished_at=clock_timestamp(),
					scanned=4,updated=2 WHERE run_id=$1 AND ordinal=1`, admission.Run.ID, string(test.childState))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				terminal, err := store.RefreshRun(ctx, admission.Run.ID)
				if err != nil || terminal.State != test.runState || terminal.TerminalChildren != 2 || terminal.Scanned != 7 {
					t.Fatalf("refresh did not preserve the durable terminal aggregate: %+v, %v", terminal, err)
				}
			}
			if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
				t.Fatal(err)
			}
			assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID, admitted,
				taskLifecycleFinishedEntry(admission.Run.ID, test.runState, 2))
		})
	}
}
