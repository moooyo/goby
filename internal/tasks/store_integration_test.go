package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type repositoryProber struct{}

func (repositoryProber) ProbeFile(context.Context, *os.File) (media.Info, error) {
	return media.Info{}, errors.New("repository test must not execute media probes")
}

func taskRepository(t *testing.T, libraryCount int) (context.Context, *pgxpool.Pool, *library.Store, *Store, Actor, Definition) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL task integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create task integration database connection")
	}
	t.Cleanup(admin.Close)
	suffix, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"goby_tasks_test_" + suffix}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
		defer finish()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("remove owned task schema: %v", err)
		}
	})
	options, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse task integration database connection")
	}
	options.ConnConfig.RuntimeParams["search_path"] = strings.Trim(schema, `"`)
	options.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	options.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, options)
	if err != nil {
		t.Fatal("create isolated task pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	scanner, err := library.New(pool, repositoryProber{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
		defer finish()
		if err := scanner.Close(cleanup); err != nil {
			t.Errorf("close task-test catalog: %v", err)
		}
	})
	store, err := New(pool, scanner)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	definition, err := store.GetByKey(ctx, LibraryScanKey)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := randomID()
	sessionID, _ := randomID()
	digest := sha256.Sum256([]byte(sessionID))
	if _, err := pool.Exec(ctx, `INSERT INTO users
        (id,name,normalized_name,password_hash,is_administrator) VALUES ($1,'Task Administrator','task administrator','',true)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id,user_id,token_hash,kind,expires_at)
        VALUES ($1,$2,$3,'admin',clock_timestamp()+interval '1 hour')`, sessionID, userID, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id,name,collection_type)
        SELECT 'library-' || number, 'Library ' || number, 'movies' FROM generate_series(1,$1::integer) number`, libraryCount); err != nil {
		t.Fatal(err)
	}
	actor := Actor{Principal: identity.Principal{SessionID: sessionID, Kind: "admin",
		User: identity.User{ID: userID, IsAdministrator: true}}, Audience: identity.AdministratorNative}
	return ctx, pool, scanner, store, actor, definition
}

func TestTaskRepositoryConcurrentRequestsRetainEveryReceiptAfterCompletion(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 137)
	const clients = 12
	results := make(chan Admission, clients)
	failures := make(chan error, clients)
	var workers sync.WaitGroup
	for index := range clients {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			result, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: fmt.Sprintf("request-%d", index)})
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		}(index)
	}
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent task admission: %v", err)
	}
	runID, admitted := "", 0
	for result := range results {
		if runID != "" && result.Run.ID != runID {
			t.Fatal("concurrent requests created different active runs")
		}
		runID = result.Run.ID
		if result.Admitted {
			admitted++
		}
		if result.Run.TotalChildren != 137 || result.Run.State != RunPending || result.Run.CreatedAt.Location() != time.UTC {
			t.Fatal("admission lost its snapshot, pending state, or normalized timestamp")
		}
	}
	if admitted != 1 {
		t.Fatalf("new run count = %d, want one", admitted)
	}
	children, err := store.ListChildren(ctx, runID, Page{StartIndex: 130, Limit: 10})
	if err != nil || children.TotalRecordCount != 137 || len(children.Items) != 7 {
		t.Fatalf("child page lost the tail beyond queue capacity: count=%d length=%d error=%v", children.TotalRecordCount, len(children.Items), err)
	}
	var scans, receipts int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_jobs), (SELECT count(*) FROM task_run_requests)`).Scan(&scans, &receipts); err != nil {
		t.Fatal(err)
	}
	if scans != 0 || receipts != clients {
		t.Fatalf("manual admission executed scans or lost receipts: scans=%d receipts=%d", scans, receipts)
	}
	stopped, err := store.Stop(ctx, actor, runID)
	if err != nil || stopped.State != RunCancelled || stopped.CancelledChildren != 137 || stopped.TerminalChildren != 137 {
		t.Fatalf("stop did not finish unadmitted children: state=%s count=%d error=%v", stopped.State, stopped.CancelledChildren, err)
	}
	for index := range clients {
		retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: fmt.Sprintf("request-%d", index)})
		if err != nil || retry.Admitted || retry.Run.ID != runID || !reflect.DeepEqual(retry.Run, stopped) {
			t.Fatalf("terminal retry did not return its original receipt: admitted=%t error=%v", retry.Admitted, err)
		}
	}
	page, err := store.ListRuns(ctx, definition.ID, Page{Limit: 1, StartIndex: 1})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 0 {
		t.Fatalf("terminal retries changed history count: total=%d error=%v", page.TotalRecordCount, err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetByKey(ctx, LibraryScanKey)
	if err != nil || after.ID != definition.ID || after.LastRun == nil || !reflect.DeepEqual(*after.LastRun, stopped) {
		t.Fatalf("definition reconciliation changed identity or last result: %v", err)
	}
}

func TestTaskRepositoryEmptyLibraryRunCompletesAndUnknownRequestsDoNotCreateHistory(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 0)
	result, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "empty"})
	if err != nil || !result.Admitted || result.Run.State != RunCompleted || result.Run.TotalChildren != 0 || result.Run.StartedAt == nil || result.Run.FinishedAt == nil {
		t.Fatalf("empty full-library task did not finish truthfully: state=%s error=%v", result.Run.State, err)
	}
	if _, err := store.Start(ctx, actor, StartRequest{TaskID: "unknown-task", RequestID: "unknown"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task admission = %v", err)
	}
	if _, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "line\nfeed"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid request ID admission = %v", err)
	}
	if _, err := store.Start(ctx, Actor{}, StartRequest{TaskID: definition.ID}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("zero actor became system authority: %v", err)
	}
	if _, err := store.SystemStopRun(ctx, result.Run.ID, "administrator"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("system stop accepted user reason: %v", err)
	}
	after, err := store.Stop(ctx, actor, result.Run.ID)
	if err != nil || !reflect.DeepEqual(after, result.Run) {
		t.Fatalf("terminal stop modified an immutable result: %v", err)
	}
	triggerID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,interval_ticks,anchor_at,next_fire_at,last_due_at)
		VALUES ($1,$2,1,0,'interval',432000000000,clock_timestamp(),
			clock_timestamp()+interval '12 hours',clock_timestamp())`, triggerID, definition.ID); err != nil {
		t.Fatal(err)
	}
	projection, err := store.Get(ctx, definition.ID)
	if err != nil || len(projection.Triggers) != 1 || projection.LastRun == nil {
		t.Fatalf("task projection lost its trigger or result: %v", err)
	}
	trigger := projection.Triggers[0]
	for _, value := range []*time.Time{&projection.CreatedAt, &projection.UpdatedAt, &trigger.CreatedAt,
		&trigger.UpdatedAt, trigger.AnchorAt, trigger.NextFireAt, trigger.LastDueAt,
		&projection.LastRun.CreatedAt, projection.LastRun.StartedAt, projection.LastRun.FinishedAt} {
		if value == nil || value.Location() != time.UTC {
			t.Fatal("non-UTC pool timezone leaked through a nested task projection")
		}
	}
}

func TestTaskRepositoryReceiptRejectsDifferentSourceFingerprint(t *testing.T) {
	ctx, pool, _, store, actor, definition := taskRepository(t, 1)
	first, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "same-request"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(id))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id,user_id,token_hash,kind,expires_at)
		VALUES ($1,$2,$3,'emby',clock_timestamp()+interval '1 hour')`, id, actor.Principal.User.ID, digest[:]); err != nil {
		t.Fatal(err)
	}
	embyActor := actor
	embyActor.Audience = identity.AdministratorEmby
	embyActor.Principal.Kind, embyActor.Principal.SessionID = "emby", id
	if _, err := store.Start(ctx, embyActor, StartRequest{TaskID: definition.ID, RequestID: "same-request"}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("receipt accepted a different normalized source: %v", err)
	}
	runs, err := store.ListRuns(ctx, definition.ID, Page{})
	if err != nil || runs.TotalRecordCount != 1 || len(runs.Items) != 1 || runs.Items[0].ID != first.Run.ID {
		t.Fatalf("receipt conflict created another run: %v", err)
	}
}

type hookedTransactions struct {
	owner library.OwnedTransactions
	hook  func(library.OwnedTx, string) error
}

func (value hookedTransactions) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	return value.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return callback(hookedTransaction{OwnedTx: tx, hook: value.hook})
	})
}

type hookedTransaction struct {
	library.OwnedTx
	hook func(library.OwnedTx, string) error
}

func (value hookedTransaction) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	tag, err := value.OwnedTx.Exec(statement, args...)
	if err == nil {
		err = value.hook(value.OwnedTx, strings.TrimSpace(statement))
	}
	return tag, err
}

func TestTaskRepositoryStopPersistsOnlyOwnedFlagsAfterCallerCancellation(t *testing.T) {
	ctx, pool, scanner, store, actor, definition := taskRepository(t, 3)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	owned := children.Items[0]
	if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE task_run_children SET state='running',scan_job_id='owned-scan',
            started_at=clock_timestamp(),scanned=5,added=2,updated=1 WHERE id=$1`, owned.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at,scanned,added,updated)
            VALUES ('owned-scan',$1,'Running',$2,clock_timestamp(),5,2,1),
                ('independent-scan',$3,'Running',NULL,clock_timestamp(),0,0,0)`, owned.LibraryID, owned.ID, children.Items[1].LibraryID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	var interrupted atomic.Bool
	hooked, err := New(pool, hookedTransactions{owner: scanner, hook: func(_ library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "UPDATE scan_jobs j SET cancel_requested") && interrupted.CompareAndSwap(false, true) {
			cancel()
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := hooked.Stop(caller, actor, admission.Run.ID)
	if err != nil || stopped.State != RunStopping || !interrupted.Load() || !errors.Is(caller.Err(), context.Canceled) {
		t.Fatalf("stop did not outlive its HTTP caller: state=%s error=%v", stopped.State, err)
	}
	var ownFlag, independentFlag bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT cancel_requested FROM scan_jobs WHERE id='owned-scan'),
        (SELECT cancel_requested FROM scan_jobs WHERE id='independent-scan')`).Scan(&ownFlag, &independentFlag); err != nil {
		t.Fatal(err)
	}
	if !ownFlag || independentFlag || stopped.CancelledChildren != 2 || stopped.Scanned != 5 {
		t.Fatal("stop lost an owned flag, touched an independent scan, or discarded counters")
	}
	shutdown, err := store.SystemStopRun(ctx, admission.Run.ID, "shutdown")
	if err != nil || shutdown.StopReason != "administrator" || shutdown.State != RunStopping ||
		shutdown.CancelledChildren != 2 || shutdown.InterruptedChildren != 0 {
		t.Fatalf("shutdown replaced an earlier administrator stop: reason=%s error=%v", shutdown.StopReason, err)
	}
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status='Cancelled',finished_at=clock_timestamp() WHERE id='owned-scan'`); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE task_run_children SET state='cancelled',finished_at=clock_timestamp() WHERE id=$1`, owned.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	final, err := store.RefreshRun(ctx, admission.Run.ID)
	if err != nil || final.State != RunCancelled || final.TerminalChildren != 3 || final.Scanned != 5 {
		t.Fatalf("aggregate did not await owned terminal state: state=%s error=%v", final.State, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, owned.LibraryID); err != nil {
		t.Fatal(err)
	}
	preserved, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil || preserved.TotalRecordCount != 3 || preserved.Items[0].Scanned != 5 || preserved.Items[0].FinishedAt == nil || preserved.Items[0].FinishedAt.Location() != time.UTC {
		t.Fatalf("library deletion erased terminal task history: %v", err)
	}
}

func TestTaskRepositoryExpiredActorRollsBackRunChildrenAndReceiptAtCommit(t *testing.T) {
	ctx, pool, scanner, _, actor, definition := taskRepository(t, 2)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	var reached atomic.Bool
	store, err := New(pool, hookedTransactions{owner: scanner, hook: func(tx library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "INSERT INTO task_run_requests") && reached.CompareAndSwap(false, true) {
			_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
                (expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
			return err
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "expires-before-commit"}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("expired actor committed task admission: %v", err)
	}
	if !reached.Load() {
		t.Fatal("actor expired before the test reached its post-write barrier")
	}
	var runs, children, receipts int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_runs),
        (SELECT count(*) FROM task_run_children),(SELECT count(*) FROM task_run_requests)`).Scan(&runs, &children, &receipts); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || children != 0 || receipts != 0 {
		t.Fatalf("final authorization failure leaked writes: runs=%d children=%d receipts=%d", runs, children, receipts)
	}
}

func TestTaskRepositoryRecoveryRequiresScannerSnapshotsAndPreservesReceipts(t *testing.T) {
	ctx, _, scanner, store, actor, definition := taskRepository(t, 2)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recover"})
	if err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	child := children.Items[0]
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE task_run_children SET state='running',scan_job_id='recover-scan',
            started_at=clock_timestamp() WHERE id=$1`, child.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at)
            VALUES ('recover-scan',$1,'Running',$2,clock_timestamp())`, child.LibraryID, child.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverRuns(ctx); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("recovery accepted missing scanner snapshots: %v", err)
	}
	if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status='Interrupted',finished_at=clock_timestamp(),scanned=7
            WHERE id='recover-scan'`); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE task_run_children SET state='interrupted',finished_at=clock_timestamp(),scanned=7
            WHERE id=$1`, child.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	retry, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recover"})
	if err != nil || retry.Admitted || retry.Run.ID != admission.Run.ID || retry.Run.State != RunInterrupted || retry.Run.InterruptedChildren != 2 || retry.Run.Scanned != 7 {
		t.Fatalf("recovery lost its receipt or child snapshot: state=%s error=%v", retry.Run.State, err)
	}
	before := retry.Run
	if err := store.RecoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("repeated recovery rewrote terminal history: %v", err)
	}
}

func TestTaskRepositoryShutdownInterruptsUnadmittedChildren(t *testing.T) {
	ctx, _, _, store, actor, definition := taskRepository(t, 2)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := store.SystemStopRun(ctx, admission.Run.ID, "shutdown")
	if err != nil || stopped.State != RunInterrupted || stopped.StopReason != "shutdown" ||
		stopped.InterruptedChildren != 2 || stopped.CancelledChildren != 0 {
		t.Fatalf("shutdown did not interrupt pending work: state=%s error=%v", stopped.State, err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children.Items {
		if child.State != ChildInterrupted || child.ErrorCode != "server_interrupted" || child.FinishedAt == nil {
			t.Fatal("shutdown left a pending child cancelled or active")
		}
	}
}
