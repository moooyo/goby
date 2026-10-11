package tasks

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestTaskStartRechecksActorBeforeDecodingLockedActiveRun(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 day' WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	actor.Principal.PeerIP = "127.0.0.1"
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	if admission.Run.authority.PeerIP != actor.Principal.PeerIP {
		t.Fatal("new admission lost its private authority in the returned Run")
	}
	const activeQuery = "SELECT id, to_jsonb(r) - 'request_fingerprint' FROM task_runs r"
	reached, corrupted := false, false
	observer := &executionRefreshObserver{owner: hookedTransactions{owner: owner,
		hook: func(tx library.OwnedTx, statement string) error {
			if strings.HasPrefix(statement, activeQuery) {
				reached = true
				_, err := tx.Exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, actor.Principal.SessionID)
				return err
			}
			return nil
		}}}
	observer.afterScan = func(statement string, dest []any) error {
		if strings.HasPrefix(statement, activeQuery) {
			corrupted = true
			*dest[1].(*[]byte) = []byte(`{"analysis_input":{"LibraryIds":[17]}}`)
		}
		return nil
	}
	observed, err := New(pool, observer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observed.Start(ctx, actor, StartRequest{TaskID: definition.ID}); !reached || !corrupted || !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("active-run decoding preceded the current actor check: reached=%t corrupted=%t error=%v", reached, corrupted, err)
	}
	current, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || !reflect.DeepEqual(current, admission.Run) {
		t.Fatalf("rejected active admission changed the durable run: %+v %v", current, err)
	}
	coalesced, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil || coalesced.Admitted || !reflect.DeepEqual(coalesced.Run, admission.Run) {
		t.Fatalf("rejected admission failed to roll back its injected credential expiry: %+v %v", coalesced, err)
	}
}

func TestTaskRecoveryReusesLockedFactsAndRetainsEmptyObservation(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("unexpected recovery fact reread")
	observer := &executionRefreshObserver{owner: owner, fail: func(statement string) error {
		if strings.HasPrefix(statement, "SELECT task_key FROM task_runs") || strings.HasPrefix(statement, "SELECT COALESCE(bool_or(task_key=") {
			return injected
		}
		return nil
	}}
	observed, err := New(pool, observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := observed.RecoverRuns(ctx); err != nil {
		t.Fatalf("nonempty recovery did not reuse its locked facts: %v", err)
	}
	current, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || current.State != RunInterrupted || current.InterruptedChildren != 1 {
		t.Fatalf("recovery did not retain durable interruption: %+v %v", current, err)
	}
	reached := false
	observer.fail = func(statement string) error {
		if strings.HasPrefix(statement, "SELECT COALESCE(bool_or(task_key=") {
			reached = true
			return injected
		}
		return nil
	}
	if err := observed.RecoverRuns(ctx); !reached || !errors.Is(err, injected) {
		t.Fatalf("empty recovery omitted its final observation: reached=%t error=%v", reached, err)
	}
	observer.fail = nil
	if err := observed.RecoverRuns(ctx); err != nil {
		t.Fatalf("empty recovery could not retry after its observation failed: %v", err)
	}
	assertTaskLifecycleAudit(t, ctx, pool, admission.Run.ID,
		taskLifecycleUserEntry("task.admitted", "native", admission.Run.ID, actor, definition.Revision, 1),
		taskLifecycleFinishedEntry(admission.Run.ID, RunInterrupted, 1))
}

func TestTaskStopAuditsOnlyChangedScanFlagsInStableOrder(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 4)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil || len(page.Items) != 4 {
		t.Fatalf("read cancellation fixture children: %+v %v", page, err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		for index, id := range []string{"scan-z", "scan-a", "scan-m"} {
			child := page.Items[index]
			if _, err := tx.Exec(`UPDATE task_run_children SET state='running',scan_job_id=$2,
				started_at=clock_timestamp() WHERE id=$1`, child.ID, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at,cancel_requested)
				VALUES($1,$2,'Running',$3,clock_timestamp(),$4)`, id, child.LibraryID, child.ID, id == "scan-m"); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,started_at)
			VALUES('independent-scan',$1,'Running',clock_timestamp())`, page.Items[3].LibraryID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var alreadyCancelledVersion string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM scan_jobs WHERE id='scan-m'`).Scan(&alreadyCancelledVersion); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.Stop(ctx, actor, admission.Run.ID)
	if err != nil || stopped.State != RunStopping || stopped.CancelledChildren != 1 {
		t.Fatalf("scan cancellation did not retain the active workers: %+v %v", stopped, err)
	}
	var cancellationIDs []string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(array_agg(resource_id ORDER BY id),'{}'::text[])
		FROM activity_entries WHERE action='scan.cancel_requested'`).Scan(&cancellationIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cancellationIDs, []string{"scan-a", "scan-z"}) {
		t.Fatalf("cancellation activity did not follow actual changes in stable order: %v", cancellationIDs)
	}
	var retainedVersion string
	var changed, independent bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT xmin::text FROM scan_jobs WHERE id='scan-m'),
		(SELECT bool_and(cancel_requested) FROM scan_jobs WHERE id=ANY($1::text[])),
		(SELECT cancel_requested FROM scan_jobs WHERE id='independent-scan')`, []string{"scan-a", "scan-z"}).
		Scan(&retainedVersion, &changed, &independent); err != nil {
		t.Fatal(err)
	}
	if retainedVersion != alreadyCancelledVersion || !changed || independent {
		t.Fatal("cancellation rewrote an existing flag or failed to preserve scan ownership")
	}
	for index := range 2 {
		if _, err := store.SystemStopRun(ctx, admission.Run.ID, "shutdown"); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action='scan.cancel_requested'`).Scan(&count); err != nil || count != 2 {
			t.Fatalf("cancellation retry %d duplicated audit entries: count=%d error=%v", index, count, err)
		}
	}
}
