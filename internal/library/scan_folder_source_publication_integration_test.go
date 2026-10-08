//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scanFolderSourceTraceKey struct{}

type scanFolderSourceTraceQuery struct {
	trace *scanFolderSourceTrace
	kind  string
}

// The final item snapshot follows the real folder upsert and metadata writes.
// Injecting there leaves the commit-time physical witness as the last fence.
type scanFolderSourceTrace struct {
	mu                         sync.Mutex
	owner                      *pgx.Conn
	rootID, itemID             string
	armed, inFolder, injected  bool
	writes, rollbacks, commits int
	err                        error
	inject                     func() error
	rolledBack                 chan struct{}
	rollbackOnce               sync.Once
}

func (trace *scanFolderSourceTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if !trace.armed || conn != trace.owner {
		return ctx
	}
	statement := scanPerformanceNormalizeSQL(data.SQL)
	if strings.HasPrefix(statement, "insert into items ") && len(data.Args) >= 9 && data.Args[2] == trace.rootID && data.Args[8] == "Show" {
		trace.writes++
		trace.inFolder = true
	}
	kind := ""
	if trace.inFolder && !trace.injected && strings.Contains(statement, "where i.id = $1 for update of i") &&
		len(data.Args) == 1 && data.Args[0] == trace.itemID {
		kind = "proof"
	} else if trace.inFolder && (statement == "rollback" || statement == "commit") {
		kind = statement
	}
	if kind != "" {
		return context.WithValue(ctx, scanFolderSourceTraceKey{}, scanFolderSourceTraceQuery{trace: trace, kind: kind})
	}
	return ctx
}

func (trace *scanFolderSourceTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	query, ok := ctx.Value(scanFolderSourceTraceKey{}).(scanFolderSourceTraceQuery)
	if !ok || query.trace != trace {
		return
	}
	trace.mu.Lock()
	if data.Err != nil {
		trace.err = errors.Join(trace.err, data.Err)
		trace.mu.Unlock()
		return
	}
	var inject func() error
	switch query.kind {
	case "proof":
		if conn != trace.owner || conn.PgConn().TxStatus() != 'T' || data.CommandTag.String() != "SELECT 1" {
			trace.err = errors.New("folder final snapshot did not use the active owner transaction")
		} else if !trace.injected {
			trace.injected = true
			inject = trace.inject
		}
	case "rollback":
		if conn.PgConn().TxStatus() != 'I' || data.CommandTag.String() != "ROLLBACK" {
			trace.err = errors.New("folder rollback did not release its actual transaction")
		} else {
			trace.rollbacks++
			trace.inFolder = false
			trace.rollbackOnce.Do(func() { close(trace.rolledBack) })
		}
	case "commit":
		if conn.PgConn().TxStatus() != 'I' || data.CommandTag.String() != "COMMIT" {
			trace.err = errors.New("folder publication did not commit its actual transaction")
		} else {
			trace.commits++
			trace.inFolder = false
		}
	}
	trace.mu.Unlock()
	if inject != nil {
		err := inject()
		trace.mu.Lock()
		trace.err = errors.Join(trace.err, err)
		trace.mu.Unlock()
	}
}

func (trace *scanFolderSourceTrace) snapshot() (bool, int, int, int, error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.injected, trace.writes, trace.rollbacks, trace.commits, trace.err
}

type scanFolderSourceFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *Store
	state   *scanState
	pass    *scanReconciliationPass
	trace   *scanFolderSourceTrace
	allowed string
	nfo     string
	itemID  string
}

func scanFolderSourcePrepare(t *testing.T) *scanFolderSourceFixture {
	t.Helper()
	ctx, pool, previous, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanFolderSourceTrace{rolledBack: make(chan struct{})}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	prober := &libraryFixtureProber{}
	store, err := New(tracedPool, prober, []string{allowed})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close folder publication Store: %v", err)
		}
	})
	nfo := libraryIntegrationFile(t, allowed, "bridge/registered/Show/tvshow.nfo", "<tvshow><title>Accepted folder title</title></tvshow>")
	rootPath := filepath.Dir(filepath.Dir(nfo))
	library := libraryIntegrationCreate(t, ctx, store, "Folder source publication", "tvshows", rootPath)
	options := DefaultLibraryOptions()
	options.EnableLocalImages = false
	library.Options = &options
	task, _ := scanUnchangedProgressTask(t, ctx, tracedPool, library)
	store.mu.Lock()
	store.active[task.job.ID] = task
	store.mu.Unlock()
	t.Cleanup(func() {
		task.cancel()
		store.mu.Lock()
		delete(store.active, task.job.ID)
		store.mu.Unlock()
	})
	root := ownedAdmissionRoot(t, ctx, tracedPool, library.ID)
	pass, err := store.prepareScanReconciliation(task, []libraryRoot{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pass.Close(); err != nil {
			t.Errorf("close folder root capture: %v", err)
		}
	})
	capture := pass.byRoot[root.id]
	if capture == nil || capture.status != RootBindingVerified || capture.capture == nil || capture.primaryIO == nil {
		t.Fatal("folder fixture requires a verified original configured-anchor capture")
	}
	opened, sourceRoot, err := pass.openSourceRoot(store, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(opened.Close(), sourceRoot.Close()); err != nil {
			t.Errorf("close folder registered root: %v", err)
		}
	})
	show, err := opened.Stat("Show")
	if err != nil {
		t.Fatal(err)
	}
	state := &scanState{store: store, task: task, library: library, root: root, opened: opened, sourceRoot: sourceRoot,
		reconciliationPass: pass, walkRow: capture.row, directoryIdentities: map[string]os.FileInfo{"Show": show}}
	scanProgressBatchHoldWindow(task)
	id, err := state.folder("Show", filepath.Join(root.path, "Show"), "Show", "Series", library.ID, 0)
	if err != nil || id == "" {
		t.Fatalf("publish accepted folder fixture: id=%q error=%v", id, err)
	}
	if len(prober.calls()) != 0 {
		t.Fatal("directory-only fixture invoked a primary-media prober")
	}
	if err := os.WriteFile(nfo, []byte("<tvshow><title>Pending folder title</title></tvshow>"), 0600); err != nil {
		t.Fatal(err)
	}
	trace.owner, trace.rootID, trace.itemID = store.ownership.conn.Conn(), root.id, id
	return &scanFolderSourceFixture{ctx: ctx, pool: tracedPool, store: store, state: state, pass: pass,
		trace: trace, allowed: allowed, nfo: nfo, itemID: id}
}

func (fixture *scanFolderSourceFixture) publish() (string, error) {
	state := fixture.state
	return state.folder("Show", filepath.Join(state.root.path, "Show"), "Show", "Series", state.library.ID, 0)
}

func TestScanFolderSourceRejectsReplacedAllowedAncestor(t *testing.T) {
	for _, boundary := range []string{"metadata_wait", "owned_final_proof"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := scanFolderSourcePrepare(t)
			ctx, pool, store, state := fixture.ctx, fixture.pool, fixture.store, fixture.state
			baselineIO, baselineOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			beforeCatalog := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID)
			beforeProgress := scanUnchangedProgressSnapshot(t, ctx, pool, state.task)
			notifications := catalogChangesTestListener(t, store)
			change := newScanNamedRootReplacement(t, fixture.allowed, state.root.path, fixture.nfo)
			show, err := os.Stat(filepath.Join(state.root.path, "Show"))
			if err != nil {
				t.Fatal(err)
			}
			inject := func() error {
				if err := change.apply(); err != nil {
					return err
				}
				current, err := os.Stat(filepath.Join(state.root.path, "Show"))
				if err != nil || !sameMediaSourceFile(show, current) {
					return errors.Join(err, errors.New("configured-anchor replacement changed the retained source directory"))
				}
				return nil
			}
			var result error
			if boundary == "metadata_wait" {
				unblock, retire := cachedObservationBlockBackground(t, ctx, state)
				done := make(chan struct{})
				t.Cleanup(func() {
					state.task.cancel()
					unblock()
					mediaSourceAdmissionTestWait(t, done, "queued source-folder cleanup")
				})
				go func() { _, result = fixture.publish(); close(done) }()
				wait, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				primaryReadTestWaitQueued(t, wait, 1)
				if current := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID); current != beforeCatalog {
					t.Fatal("queued folder metadata changed accepted catalog before admission")
				}
				if err := inject(); err != nil {
					t.Fatal(err)
				}
				unblock()
				mediaSourceAdmissionTestWait(t, done, "queued source-folder completion")
				retire()
			} else {
				fixture.trace.mu.Lock()
				fixture.trace.inject, fixture.trace.armed = inject, true
				fixture.trace.mu.Unlock()
				_, result = fixture.publish()
				injected, writes, rollbacks, commits, traceErr := fixture.trace.snapshot()
				if !injected || traceErr != nil || writes != 1 || rollbacks != 1 || commits != 0 {
					t.Fatalf("folder final proof did not reject and roll back its writes: injected=%v writes=%d rollback=%d commits=%d error=%v", injected, writes, rollbacks, commits, traceErr)
				}
			}
			if result == nil && state.warnings == 0 {
				t.Fatal("source-backed folder accepted a changed configured anchor")
			}
			if result != nil && !errors.Is(result, ErrRootBindingConflict) && !errors.Is(result, errScanProbeSourceChanged) && !errors.Is(result, ErrSourceChanged) {
				t.Fatalf("folder failed for an unrelated reason instead of rejecting its changed source: %v", result)
			}
			if current := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID); current != beforeCatalog ||
				scanUnchangedProgressSnapshot(t, ctx, pool, state.task) != beforeProgress {
				t.Fatalf("changed configured anchor published folder metadata or accepted progress: %v", result)
			}
			assertNoCatalogTestNotification(t, notifications)
			if stats := originalMediaReadGovernor.Stats(); stats != baselineIO || originalMediaReadOwners.Stats().RegisteredOwners != baselineOwners {
				t.Fatalf("rejected source folder retained temporary resources: before=%+v after=%+v owners=%+v", baselineIO, stats, originalMediaReadOwners.Stats())
			}
		})
	}
}

func TestScanFolderSourceFinalBusyRetriesOutsideOwnedTransaction(t *testing.T) {
	fixture := scanFolderSourcePrepare(t)
	ctx, pool, store, state := fixture.ctx, fixture.pool, fixture.store, fixture.state
	beforeCatalog := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID)
	beforeProgress := scanUnchangedProgressSnapshot(t, ctx, pool, state.task)
	notifications := catalogChangesTestListener(t, store)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	blockers := make([]*primarySidecarRetryBlocker, 0, 2)
	for range 2 {
		operation, err := store.prepareScanOperationRootIO(ctx, state.task,
			[]mediaSourceRootHint{{root: state.walkRow.root, bindingRevision: state.walkRow.revision}})
		if err != nil {
			t.Fatal(err)
		}
		blocker := &primarySidecarRetryBlocker{operation: operation, rootID: state.root.id,
			entered: make(chan struct{}), done: make(chan struct{})}
		blockers = append(blockers, blocker)
		t.Cleanup(func() {
			unblock()
			if blocker.started.Load() {
				mediaSourceAdmissionTestWait(t, blocker.done, "folder final-proof blocker cleanup")
			}
			if err := operation.Close(); err != nil {
				t.Errorf("close folder final-proof blocker: %v", err)
			}
		})
	}
	baselineIO, baselineOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	fixture.trace.mu.Lock()
	fixture.trace.armed = true
	fixture.trace.inject = func() error {
		if stats := originalMediaReadGovernor.Stats(); stats.Active != baselineIO.Active {
			return fmt.Errorf("folder retained I/O admission during owned SQL: %+v", stats)
		}
		for _, blocker := range blockers {
			blocker.start(ctx, release)
			select {
			case <-blocker.entered:
			case <-blocker.done:
				return fmt.Errorf("folder final-proof blocker failed before admission: %v", blocker.err)
			case <-time.After(5 * time.Second):
				return errors.New("folder final-proof blocker did not acquire actual admission")
			}
		}
		return nil
	}
	fixture.trace.mu.Unlock()
	done := make(chan struct{})
	var result error
	var publishedID string
	t.Cleanup(func() {
		state.task.cancel()
		unblock()
		mediaSourceAdmissionTestWait(t, done, "folder Busy retry cleanup")
	})
	go func() { publishedID, result = fixture.publish(); close(done) }()
	select {
	case <-fixture.trace.rolledBack:
	case <-done:
		t.Fatalf("folder final proof did not roll back its Busy attempt: %v", result)
	case <-time.After(10 * time.Second):
		t.Fatal("folder Busy final proof did not release its transaction")
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	primaryReadTestWaitQueued(t, wait, baselineIO.Queued+1)
	if stats := originalMediaReadGovernor.Stats(); stats.Active != baselineIO.Active+2 || stats.Background != baselineIO.Background+2 {
		t.Fatalf("folder retry did not wait behind the actual two background leases: %+v", stats)
	}
	if current := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID); current != beforeCatalog ||
		scanUnchangedProgressSnapshot(t, ctx, pool, state.task) != beforeProgress {
		t.Fatal("Busy folder proof exposed uncommitted catalog or accepted progress")
	}
	assertNoCatalogTestNotification(t, notifications)
	primarySidecarRetryAssertOwnedTransactionFree(t, ctx, store, unblock)
	unblock()
	mediaSourceAdmissionTestWait(t, done, "folder retry after actual background release")
	if result != nil || publishedID != fixture.itemID {
		t.Fatalf("folder retry did not publish its stable source and identity: id=%q error=%v", publishedID, result)
	}
	injected, writes, rollbacks, commits, traceErr := fixture.trace.snapshot()
	if !injected || traceErr != nil || writes != 2 || rollbacks != 1 || commits != 1 || state.warnings != 0 {
		t.Fatalf("folder retry did not complete exactly one rebuilt publication: injected=%v writes=%d rollback=%d commits=%d warnings=%d error=%v", injected, writes, rollbacks, commits, state.warnings, traceErr)
	}
	var title string
	if err := pool.QueryRow(ctx, "SELECT name FROM items WHERE id=$1", fixture.itemID).Scan(&title); err != nil || title != "Pending folder title" {
		t.Fatalf("folder retry lost refreshed metadata: title=%q error=%v", title, err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: fixture.itemID, LibraryID: state.library.ID, ParentID: state.library.ID, IsFolder: true,
	}})
	assertNoCatalogTestNotification(t, notifications)
	for _, blocker := range blockers {
		mediaSourceAdmissionTestWait(t, blocker.done, "folder final-proof blocker retirement")
		if blocker.err != nil {
			t.Fatal(blocker.err)
		}
	}
	if stats := originalMediaReadGovernor.Stats(); stats != baselineIO || originalMediaReadOwners.Stats().RegisteredOwners != baselineOwners {
		t.Fatalf("folder retry retained temporary admission: before=%+v after=%+v owners=%+v", baselineIO, stats, originalMediaReadOwners.Stats())
	}
}
