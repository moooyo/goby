package tasks

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestTaskAggregateRefreshSkipsUnchangedParentVersions(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 2)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	rowVersion := func() string {
		t.Helper()
		var version string
		if err := pool.QueryRow(ctx, `SELECT xmin::text FROM task_runs WHERE id=$1`, admission.Run.ID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return version
	}
	assertUnchanged := func(want Run) {
		t.Helper()
		before := rowVersion()
		for range 3 {
			got, err := store.RefreshRun(ctx, want.ID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("unchanged aggregate changed its result: got=%+v want=%+v error=%v", got, want, err)
			}
			if after := rowVersion(); after != before {
				t.Fatalf("unchanged aggregate rewrote its parent: before=%s after=%s", before, after)
			}
		}
	}
	assertUnchanged(admission.Run)
	running, err := store.BeginRun(ctx, admission.Run.ID)
	if err != nil || running.State != RunRunning {
		t.Fatalf("begin task run: %+v %v", running, err)
	}
	assertUnchanged(running)
	beforeProgress := rowVersion()
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		_, err := tx.Exec(`UPDATE task_run_children SET scanned=5,added=2,updated=1 WHERE run_id=$1 AND ordinal=0`, running.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := store.RefreshRun(ctx, running.ID)
	if err != nil || progress.State != RunRunning || progress.TerminalChildren != 0 ||
		progress.Scanned != 5 || progress.Added != 2 || progress.Updated != 1 ||
		!reflect.DeepEqual(progress.StartedAt, running.StartedAt) {
		t.Fatalf("progress did not update the aggregate while preserving its runtime origin: %+v %v", progress, err)
	}
	if rowVersion() == beforeProgress {
		t.Fatal("changed progress did not persist a new parent version")
	}
	assertUnchanged(progress)
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		_, err := tx.Exec(`UPDATE task_run_children SET state='completed',finished_at=clock_timestamp(),
			error_message='A library scan completed with warnings.' WHERE run_id=$1 AND ordinal=0`, running.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	partial, err := store.RefreshRun(ctx, running.ID)
	if err != nil || partial.State != RunRunning || partial.TerminalChildren != 1 || partial.CompletedChildren != 1 {
		t.Fatalf("durable child completion was not aggregated: %+v %v", partial, err)
	}
	assertUnchanged(partial)
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		_, err := tx.Exec(`UPDATE task_run_children SET state='completed',finished_at=clock_timestamp(),
			scanned=7,added=3,updated=2 WHERE run_id=$1 AND ordinal=1`, running.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.RefreshRun(ctx, running.ID)
	if err != nil || terminal.State != RunCompleted || terminal.TerminalChildren != 2 || terminal.CompletedChildren != 2 ||
		terminal.Scanned != 12 || terminal.Added != 5 || terminal.Updated != 3 || terminal.FinishedAt == nil ||
		terminal.ErrorCode != "scan_warnings" || !reflect.DeepEqual(terminal.StartedAt, running.StartedAt) {
		t.Fatalf("terminal aggregate lost its durable child results: %+v %v", terminal, err)
	}
	assertUnchanged(terminal)
	assertTaskLifecycleAudit(t, ctx, pool, running.ID,
		taskLifecycleUserEntry("task.admitted", "native", running.ID, actor, definition.Revision, 2),
		taskLifecycleFinishedEntry(running.ID, RunCompleted, 2))
}

func TestTaskAggregateRefreshSkipsUnchangedStoppingRun(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.BeginRun(ctx, admission.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE task_run_children SET state='running',started_at=clock_timestamp(),
			scan_job_id='aggregate-owned-scan' WHERE run_id=$1`, running.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at)
			SELECT 'aggregate-owned-scan',library_id,'Running',id,clock_timestamp()
			FROM task_run_children WHERE run_id=$1`, running.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stopping, err := store.SystemStopRun(ctx, running.ID, "max_runtime")
	if err != nil || stopping.State != RunStopping || stopping.StopReason != "max_runtime" {
		t.Fatalf("request runtime stop: %+v %v", stopping, err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM task_runs WHERE id=$1`, running.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		refreshed, err := store.RefreshRun(ctx, running.ID)
		if err != nil || !reflect.DeepEqual(refreshed, stopping) {
			t.Fatalf("idle stopping refresh changed the runtime stop: %+v %v", refreshed, err)
		}
		var after string
		if err := pool.QueryRow(ctx, `SELECT xmin::text FROM task_runs WHERE id=$1`, running.ID).Scan(&after); err != nil || after != before {
			t.Fatalf("idle stopping refresh rewrote its parent: before=%s after=%s error=%v", before, after, err)
		}
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status='Cancelled',finished_at=clock_timestamp() WHERE id='aggregate-owned-scan'`); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE task_run_children SET state='cancelled',finished_at=clock_timestamp() WHERE run_id=$1`, running.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.RefreshRun(ctx, running.ID)
	if err != nil || terminal.State != RunCancelled || terminal.ErrorCode != "max_runtime" || terminal.CancelledChildren != 1 {
		t.Fatalf("runtime stop did not finish after the durable worker result: %+v %v", terminal, err)
	}
}
