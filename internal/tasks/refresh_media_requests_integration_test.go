package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRefreshMediaKeepsLiteralLegacyRequestReceiptsAndDefinitionScope(t *testing.T) {
	for _, legacySource := range []string{"manual", "compatibility"} {
		t.Run(legacySource, func(t *testing.T) {
			ctx, pool, _, store, actor, ordinary := taskRepository(t, 0)
			retryActor := actor
			if legacySource == "compatibility" {
				retryActor = taskCompatibilityActor(t, ctx, pool, actor)
			}
			refresh, err := store.GetByKey(ctx, LibraryRefreshMediaKey)
			if err != nil {
				t.Fatal(err)
			}
			const legacyRunID = "11111111111111111111111111111111"
			const requestID = "retained-client-request"
			// These are the old protocol's literal JSON bytes, including field order.
			// Do not create this compatibility fixture through the new Start encoder.
			legacyBytes := []byte(fmt.Sprintf(`{"TaskID":"%s","Executor":"library.scan","Source":"%s"}`, ordinary.ID, legacySource))
			legacyFingerprint := sha256.Sum256(legacyBytes)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, `INSERT INTO task_runs
		(id,task_id,state,source,request_id,request_fingerprint,actor_user_id,actor_session_id,
		actor_kind,task_key,task_emby_key,task_name,created_at,started_at,finished_at)
		VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8,'library.scan','RefreshLibrary',
		'Scan media library',now()-interval '2 minutes',now()-interval '1 minute',now())`, legacyRunID, ordinary.ID, legacySource, requestID,
				legacyFingerprint[:], retryActor.Principal.User.ID, retryActor.Principal.SessionID, retryActor.Principal.Kind); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO task_run_requests(task_id,request_id,run_id,fingerprint)
		VALUES ($1,$2,$3,$4)`, ordinary.ID, requestID, legacyRunID, legacyFingerprint[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE task_definitions SET enabled=false WHERE id=$1`, ordinary.ID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			legacyRun, err := store.GetRun(ctx, legacyRunID)
			if err != nil {
				t.Fatal(err)
			}
			before := refreshMediaTaskSnapshot(t, ctx, pool, ordinary.ID)
			if err := store.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				repeated, err := store.Start(ctx, retryActor, StartRequest{TaskID: ordinary.ID, RequestID: requestID})
				if err != nil || repeated.Admitted || !reflect.DeepEqual(repeated.Run, legacyRun) {
					t.Fatalf("literal legacy receipt did not replay its immutable run: admitted=%t error=%v", repeated.Admitted, err)
				}
			}
			if refreshMediaTaskSnapshot(t, ctx, pool, ordinary.ID) != before {
				t.Fatal("reconciliation or legacy replay rewrote the retained definition, run or receipt")
			}
			newRun, err := store.Start(ctx, actor, StartRequest{TaskID: refresh.ID, RequestID: requestID})
			if err != nil || !newRun.Admitted || newRun.Run.ID == legacyRunID || newRun.Run.State != RunCompleted ||
				newRun.Run.TaskKey != LibraryRefreshMediaKey || newRun.Run.TaskEmbyKey != CompatibilityKey(LibraryRefreshMediaKey) {
				t.Fatalf("the same request ID crossed task definitions: admission=%+v error=%v", newRun, err)
			}
			wantRefresh := sha256.Sum256([]byte(fmt.Sprintf(`{"TaskID":"%s","Executor":"library.refresh_media","Source":"manual"}`, refresh.ID)))
			var fingerprint []byte
			if err := pool.QueryRow(ctx, `SELECT fingerprint FROM task_run_requests WHERE task_id=$1 AND request_id=$2`,
				refresh.ID, requestID).Scan(&fingerprint); err != nil || !reflect.DeepEqual(fingerprint, wantRefresh[:]) {
				t.Fatalf("refresh receipt did not bind its actual executor: %v", err)
			}
			repeated, err := store.Start(ctx, actor, StartRequest{TaskID: refresh.ID, RequestID: requestID})
			if err != nil || repeated.Admitted || !reflect.DeepEqual(repeated.Run, newRun.Run) {
				t.Fatalf("terminal refresh retry started another run: %v", err)
			}
			if refreshMediaTaskSnapshot(t, ctx, pool, ordinary.ID) != before {
				t.Fatal("refresh admission changed retained ordinary-scan history")
			}
			var runs, requests, scans int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM task_run_requests),(SELECT count(*) FROM scan_jobs)`).Scan(&runs, &requests, &scans); err != nil ||
				runs != 2 || requests != 2 || scans != 0 {
				t.Fatalf("receipt replay changed the bounded execution history: runs=%d requests=%d scans=%d error=%v", runs, requests, scans, err)
			}
		})
	}
}

func TestRefreshMediaConcurrentRequestsCoalescePerDefinitionAndStopOnlyTheirRun(t *testing.T) {
	ctx, pool, _, store, actor, ordinary := taskRepository(t, 2)
	refresh, err := store.GetByKey(ctx, LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal(err)
	}
	definitions := []Definition{ordinary, refresh}
	type outcome struct {
		taskID    string
		admission Admission
		err       error
	}
	const callers = 8
	results := make(chan outcome, callers*len(definitions))
	start := make(chan struct{})
	var pending sync.WaitGroup
	for _, definition := range definitions {
		for range callers {
			pending.Add(1)
			go func(taskID string) {
				defer pending.Done()
				<-start
				admission, err := store.Start(ctx, actor, StartRequest{TaskID: taskID, RequestID: "same-concurrent-request"})
				results <- outcome{taskID: taskID, admission: admission, err: err}
			}(definition.ID)
		}
	}
	close(start)
	pending.Wait()
	close(results)
	runs := make(map[string]Run)
	admitted := make(map[string]int)
	for result := range results {
		if result.err != nil || result.admission.Run.TaskID != result.taskID || result.admission.Run.TotalChildren != 2 {
			t.Fatalf("concurrent task admission failed or crossed a definition: %v", result.err)
		}
		if previous, exists := runs[result.taskID]; exists && previous.ID != result.admission.Run.ID {
			t.Fatal("one definition admitted multiple runs for one request ID")
		}
		runs[result.taskID] = result.admission.Run
		if result.admission.Admitted {
			admitted[result.taskID]++
		}
	}
	if len(runs) != 2 || runs[ordinary.ID].ID == runs[refresh.ID].ID || admitted[ordinary.ID] != 1 || admitted[refresh.ID] != 1 {
		t.Fatal("concurrency did not retain one distinct admission for each definition")
	}
	beforeOrdinary := refreshMediaTaskSnapshot(t, ctx, pool, ordinary.ID)
	stoppedRefresh, err := store.Stop(ctx, actor, runs[refresh.ID].ID)
	if err != nil || stoppedRefresh.State != RunCancelled || stoppedRefresh.CancelledChildren != 2 {
		t.Fatalf("refresh stop did not cancel its waiting children: %v", err)
	}
	if refreshMediaTaskSnapshot(t, ctx, pool, ordinary.ID) != beforeOrdinary {
		t.Fatal("stopping refresh changed the other definition's pending run or children")
	}
	stoppedOrdinary, err := store.Stop(ctx, actor, runs[ordinary.ID].ID)
	if err != nil || stoppedOrdinary.State != RunCancelled || stoppedOrdinary.CancelledChildren != 2 {
		t.Fatalf("ordinary stop failed: %v", err)
	}
	for _, expected := range []Run{stoppedOrdinary, stoppedRefresh} {
		repeated, err := store.Start(ctx, actor, StartRequest{TaskID: expected.TaskID, RequestID: "same-concurrent-request"})
		if err != nil || repeated.Admitted || !reflect.DeepEqual(repeated.Run, expected) {
			t.Fatalf("completed concurrent receipt did not retain its own result: %v", err)
		}
	}
}

func TestRefreshMediaBulkStopAndRecoveryRejectMismatchedTerminalScanMode(t *testing.T) {
	for _, test := range []struct {
		name, key string
		force     bool
		unknown   bool
	}{
		{"ordinary-forced-snapshot", LibraryScanKey, true, false},
		{"refresh-normal-snapshot", LibraryRefreshMediaKey, false, false},
		{"unknown-executor-snapshot", LibraryScanKey, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool, _, store, actor, _ := taskRepository(t, 1)
			definition, err := store.GetByKey(ctx, test.key)
			if err != nil {
				t.Fatal(err)
			}
			admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "inconsistent-terminal-link"})
			if err != nil {
				t.Fatal(err)
			}
			children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
			if err != nil || len(children.Items) != 1 {
				t.Fatalf("read task child: %v", err)
			}
			child := children.Items[0]
			jobID, err := randomID()
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			// The scan is terminal, so an old active-linked recovery guard cannot
			// make this mode-validation test pass by rejecting unfinished work.
			if _, err := tx.Exec(ctx, `INSERT INTO scan_jobs
				(id,library_id,status,force_probe,task_child_id,scanned,added,started_at,finished_at)
				VALUES ($1,$2,'Completed',$3,$4,1,1,now(),now())`, jobID, child.LibraryID, test.force, child.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE task_run_children SET state='completed',scan_job_id=$2,
				scanned=1,added=1,started_at=now(),finished_at=now() WHERE id=$1`, child.ID, jobID); err != nil {
				t.Fatal(err)
			}
			if test.unknown {
				if _, err := tx.Exec(ctx, `UPDATE task_runs SET task_key='library.unknown_executor' WHERE id=$1`, admission.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			before := refreshMediaTaskSnapshot(t, ctx, pool, definition.ID)
			operations := []struct {
				name  string
				apply func() error
			}{
				{"administrator stop", func() error { _, err := store.Stop(ctx, actor, admission.Run.ID); return err }},
				{"system stop", func() error { _, err := store.SystemStopRun(ctx, admission.Run.ID, "shutdown"); return err }},
				{"startup recovery", func() error { return store.RecoverRuns(ctx) }},
			}
			for _, operation := range operations {
				if err := operation.apply(); !errors.Is(err, ErrInconsistent) {
					t.Fatalf("%s accepted an inconsistent executor snapshot: %v", operation.name, err)
				}
				if refreshMediaTaskSnapshot(t, ctx, pool, definition.ID) != before {
					t.Fatalf("%s rewrote mismatched scan history or cancellation flags", operation.name)
				}
			}
		})
	}
}

func refreshMediaTaskSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'definition',(SELECT to_jsonb(d)||jsonb_build_object('row_version',d.xmin::text) FROM task_definitions d WHERE d.id=$1),
		'runs',(SELECT COALESCE(jsonb_agg(to_jsonb(r)||jsonb_build_object('row_version',r.xmin::text) ORDER BY r.id),'[]'::jsonb) FROM task_runs r WHERE r.task_id=$1),
		'requests',(SELECT COALESCE(jsonb_agg(to_jsonb(q)||jsonb_build_object('row_version',q.xmin::text) ORDER BY q.request_id),'[]'::jsonb) FROM task_run_requests q WHERE q.task_id=$1),
		'children',(SELECT COALESCE(jsonb_agg(to_jsonb(c)||jsonb_build_object('row_version',c.xmin::text) ORDER BY c.id),'[]'::jsonb) FROM task_run_children c JOIN task_runs r ON r.id=c.run_id WHERE r.task_id=$1),
		'scans',(SELECT COALESCE(jsonb_agg(to_jsonb(j)||jsonb_build_object('row_version',j.xmin::text) ORDER BY j.id),'[]'::jsonb) FROM scan_jobs j JOIN task_run_children c ON c.id=j.task_child_id JOIN task_runs r ON r.id=c.run_id WHERE r.task_id=$1)
	)::text`, taskID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot task definition and owned history: %v", err)
	}
	return snapshot
}
