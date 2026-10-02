//go:build linux

package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/storagebinding"
)

type scanPublicationIOQueryKey struct{}

type scanPublicationIOQuery struct {
	trace *scanPublicationIOTrace
	kind  string
}

// Only the catalog owner's exact root query can start the final-proof gate.
// Pooled pre-admission authority transactions cannot satisfy this barrier.
type scanPublicationIOTrace struct {
	mu            sync.Mutex
	expectedOwner *pgx.Conn
	libraryID     string
	rootID        string
	itemID        string
	armed         bool
	injected      bool
	proof         bool
	primary       bool
	rootReads     int
	itemWrites    int
	commits       int
	rollbacks     int
	err           error
	inject        func() error
	rolledBack    chan struct{}
	rollbackOnce  sync.Once
}

func (trace *scanPublicationIOTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := scanPerformanceNormalizeSQL(data.SQL)
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if !trace.armed || conn != trace.expectedOwner {
		return ctx
	}
	kind := ""
	if statement == scanPerformanceAuthorityStatements[5] && len(data.Args) == 3 &&
		data.Args[0] == trace.libraryID && data.Args[1] == trace.rootID && data.Args[2] == storagebinding.MaxDocumentBytes {
		kind = "root"
	} else if strings.HasPrefix(statement, "insert into items ") && len(data.Args) != 0 && data.Args[0] == trace.itemID {
		trace.itemWrites++
		trace.primary = true
	} else if statement == "rollback" && trace.proof {
		kind = "rollback"
	} else if statement == "commit" && trace.primary {
		kind = "commit"
	}
	if kind != "" {
		return context.WithValue(ctx, scanPublicationIOQueryKey{}, scanPublicationIOQuery{trace: trace, kind: kind})
	}
	return ctx
}

func (trace *scanPublicationIOTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	query, ok := ctx.Value(scanPublicationIOQueryKey{}).(scanPublicationIOQuery)
	if !ok || query.trace != trace {
		return
	}
	trace.mu.Lock()
	trace.err = errors.Join(trace.err, data.Err)
	if data.Err != nil {
		trace.mu.Unlock()
		return
	}
	var inject func() error
	switch query.kind {
	case "root":
		if conn != trace.expectedOwner || conn.PgConn().TxStatus() != 'T' || data.CommandTag.String() != "SELECT 1" {
			trace.err = errors.Join(trace.err, errors.New("final root proof did not read one row in the active owned transaction"))
			trace.mu.Unlock()
			return
		}
		trace.rootReads++
		trace.proof = true
		if !trace.injected {
			trace.injected = true
			inject = trace.inject
		}
	case "rollback":
		if data.CommandTag.String() != "ROLLBACK" || conn.PgConn().TxStatus() != 'I' {
			trace.err = errors.Join(trace.err, errors.New("publication rollback did not retire its SQL transaction"))
		} else {
			trace.rollbacks++
			trace.proof, trace.primary = false, false
			trace.rollbackOnce.Do(func() { close(trace.rolledBack) })
		}
	case "commit":
		if data.CommandTag.String() != "COMMIT" || conn.PgConn().TxStatus() != 'I' {
			trace.err = errors.Join(trace.err, errors.New("primary publication did not commit its SQL transaction"))
		} else {
			trace.commits++
			trace.armed = false
			trace.proof, trace.primary = false, false
		}
	}
	trace.mu.Unlock()
	if inject != nil {
		if err := inject(); err != nil {
			trace.mu.Lock()
			trace.err = errors.Join(trace.err, err)
			trace.mu.Unlock()
		}
	}
}

func (trace *scanPublicationIOTrace) snapshot() (roots, writes, commits, rollbacks int, err error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.rootReads, trace.itemWrites, trace.commits, trace.rollbacks, trace.err
}

func (trace *scanPublicationIOTrace) arm(inject func() error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.inject, trace.armed = inject, true
}

type scanPublicationIOFixture struct {
	primaryScanReadFixture
	trace           *scanPublicationIOTrace
	pass            *scanReconciliationPass
	window          *scanProbeWindow
	itemID          string
	retirementError error
}

func scanPublicationIOPrepare(t *testing.T) *scanPublicationIOFixture {
	t.Helper()
	prober := &primaryScanReadTestProber{joined: true}
	ctx, pool, previous, allowed, userID := libraryIntegrationStore(t, prober)
	if err := previous.Close(ctx); err != nil {
		t.Fatalf("release untraced catalog ownership: %v", err)
	}
	trace := &scanPublicationIOTrace{rolledBack: make(chan struct{})}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create publication trace pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, prober, []string{allowed})
	if err != nil {
		t.Fatalf("create publication trace Store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close publication trace Store: %v", err)
		}
	})
	path := libraryIntegrationFile(t, allowed, "publication/Feature.mp4", string(primaryScanReadSourceBytes))
	library := libraryIntegrationCreate(t, ctx, store, "Publication RootIO", "movies", filepath.Dir(path))
	options := DefaultLibraryOptions()
	options.EnableLocalImages = false
	library.Options = &options
	task, _ := scanUnchangedProgressTask(t, ctx, tracedPool, library)
	store.mu.Lock()
	store.active[task.job.ID] = task
	store.mu.Unlock()
	root := ownedAdmissionRoot(t, ctx, tracedPool, library.ID)
	fixture := &scanPublicationIOFixture{primaryScanReadFixture: primaryScanReadFixture{
		ctx: ctx, pool: tracedPool, store: store, userID: userID, path: path,
		state: &scanState{store: store, task: task, library: library, root: root},
	}, trace: trace}
	t.Cleanup(func() {
		task.cancel()
		fixture.retire(t)
		store.mu.Lock()
		delete(store.active, task.job.ID)
		store.mu.Unlock()
	})
	fixture.itemID, _ = primaryScanReadIndexForeground(t, fixture.primaryScanReadFixture)
	stamp, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A changed source timestamp makes this a cold refresh of an existing row.
	// Its prepared input still reads the actual original 32 KiB descriptor.
	changed := stamp.ModTime().Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, allowed, "publication/Feature.nfo", "<movie><title>Accepted publication</title></movie>")
	fixture.pass, err = store.prepareScanReconciliation(task, []libraryRoot{root})
	if err != nil {
		t.Fatalf("prepare independently verified publication roots: %v", err)
	}
	capture := fixture.pass.byRoot[root.id]
	if capture == nil || capture.status != RootBindingVerified || capture.capture == nil || capture.primaryIO == nil {
		t.Fatal("publication fixture has no complete independent named-storage witness")
	}
	fixture.state.opened, err = fixture.pass.openRoot(store, root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := fixture.state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	fixture.state.directoryIdentities = map[string]os.FileInfo{".": info}
	fixture.state.reconciliationPass, fixture.state.reconciliation = fixture.pass, fixture.pass.collector()
	fixture.window = fixture.state.newScanProbeWindow()
	if fixture.window == nil {
		t.Fatal("publication fixture did not select the bounded primary scan window")
	}
	if err := fixture.window.submit("Feature.mp4", "video", hierarchy{parentID: library.ID}); err != nil {
		t.Fatal(err)
	}
	if len(fixture.window.pending) != 1 || fixture.window.pending[0].input.unchanged || fixture.window.pending[0].input.authority == nil {
		t.Fatal("publication fixture did not retain one cold authoritative input")
	}
	select {
	case result := <-fixture.window.pending[0].result:
		fixture.window.pending[0].result <- result
		if result.err != nil || result.info.Size != originalMediaReadChunk {
			t.Fatalf("actual publication input did not finish its joined probe: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("publication probe did not return")
	}
	trace.mu.Lock()
	trace.expectedOwner, trace.libraryID, trace.rootID, trace.itemID = store.ownership.conn.Conn(), library.ID, root.id, fixture.itemID
	trace.mu.Unlock()
	return fixture
}

func (fixture *scanPublicationIOFixture) retire(t *testing.T) {
	t.Helper()
	if fixture.window != nil {
		if err := fixture.window.close(); err != nil {
			t.Errorf("retire bounded publication window: %v", err)
		}
		fixture.window = nil
	}
	if fixture.state.opened != nil {
		if err := fixture.state.opened.Close(); err != nil {
			t.Errorf("retire publication scan root: %v", err)
		}
		fixture.state.opened = nil
	}
	if fixture.pass != nil {
		if err := fixture.pass.Close(); err != nil && (fixture.retirementError == nil || !errors.Is(err, fixture.retirementError)) {
			t.Errorf("retire independent publication witnesses: %v", err)
		}
		fixture.pass = nil
	}
}

type scanPublicationIOBlocker struct {
	fixture primaryScanReadFixture
	gate    *primaryScanReadTestProber
	started atomic.Bool
	result  chan primaryScanReadProbeResult
}

func scanPublicationIOPrepareBlocker(t *testing.T, allowed string) *scanPublicationIOBlocker {
	t.Helper()
	gate := primaryScanReadTestGate()
	fixture := primaryScanReadFixtureAt(t, gate, allowed)
	fixture.prepare(t)
	blocker := &scanPublicationIOBlocker{fixture: fixture, gate: gate, result: make(chan primaryScanReadProbeResult, 1)}
	t.Cleanup(func() {
		gate.openGate()
		if blocker.started.Load() {
			select {
			case <-blocker.result:
			case <-time.After(10 * time.Second):
				t.Error("actual BG read did not return during publication cleanup")
			}
		}
	})
	return blocker
}

func (blocker *scanPublicationIOBlocker) start() {
	if blocker.started.CompareAndSwap(false, true) {
		go func() {
			info, err := blocker.fixture.input.probe(blocker.gate, blocker.fixture.file)
			blocker.result <- primaryScanReadProbeResult{info, err}
		}()
	}
}

func scanPublicationIOWaitQueued(t *testing.T, ctx context.Context, finished <-chan struct{}, result *error, baseline primaryio.Stats) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		stats := originalMediaReadGovernor.Stats()
		if stats.Queued == baseline.Queued+1 {
			if stats.Active != baseline.Active+2 || stats.Background != baseline.Background+2 {
				t.Fatalf("publication retry damaged actual BG ownership: before=%+v after=%+v", baseline, stats)
			}
			return
		}
		select {
		case <-finished:
			t.Fatalf("publication returned instead of queueing after final-proof rollback: %v", *result)
		case <-waitCtx.Done():
			t.Fatalf("publication did not queue outside the rolled-back transaction: %+v", stats)
		case <-ticker.C:
		}
	}
}

func TestScanPublicationRootIOBusyRetriesOutsideOwnedTransaction(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "stable_source"
		if changed {
			name = "changed_source"
		}
		t.Run(name, func(t *testing.T) {
			initialIO, initialOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			if initialIO.Active != 0 || initialIO.Background != 0 || initialIO.Queued != 0 {
				t.Fatalf("publication fixture needs idle common admission: %+v", initialIO)
			}
			fixture := scanPublicationIOPrepare(t)
			first := scanPublicationIOPrepareBlocker(t, fixture.state.root.allowedPath)
			second := scanPublicationIOPrepareBlocker(t, fixture.state.root.allowedPath)
			if first.fixture.input.route.Roots[0].Catalog == second.fixture.input.route.Roots[0].Catalog ||
				first.fixture.input.route.Domains[0] != second.fixture.input.route.Domains[0] ||
				first.fixture.input.route.Domains[0] != fixture.window.pending[0].input.primary.route.Domains[0] {
				t.Fatal("actual BG holders do not use separate catalog identities on the publication domain")
			}
			foreground := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, fixture.state.root.allowedPath)
			foregroundID, foregroundSnapshot := primaryScanReadIndexForeground(t, foreground)
			beforeCatalog := taskRefreshRowSnapshot(t, fixture.ctx, fixture.pool, "items", fixture.itemID)
			beforeProgress := scanUnchangedProgressSnapshot(t, fixture.ctx, fixture.pool, fixture.state.task)
			beforeJob := fixture.state.task.job
			notifications := catalogChangesTestListener(t, fixture.store)
			baselineIO, baselineOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			unblock := func() { first.gate.openGate(); second.gate.openGate() }
			inject := func() error {
				first.start()
				second.start()
				for _, blocker := range []*scanPublicationIOBlocker{first, second} {
					select {
					case <-blocker.gate.entered:
					case result := <-blocker.result:
						blocker.result <- result
						return fmt.Errorf("actual BG read ended before final publication proof: %v", result.err)
					case <-time.After(5 * time.Second):
						return errors.New("actual BG read did not occupy the common phase before final publication proof")
					}
				}
				stats := originalMediaReadGovernor.Stats()
				if stats.Active != baselineIO.Active+2 || stats.Background != baselineIO.Background+2 {
					return fmt.Errorf("actual publication contention did not fill BG2: %+v", stats)
				}
				return nil
			}
			fixture.trace.arm(inject)
			var scanErr error
			finished := make(chan struct{})
			go func() { scanErr = fixture.window.flush(); close(finished) }()
			t.Cleanup(func() {
				fixture.state.task.cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("publication did not join its actual cleanup barriers")
				}
			})
			select {
			case <-fixture.trace.rolledBack:
			case <-finished:
				t.Fatalf("publication did not roll back its Busy final proof: %v", scanErr)
			case <-time.After(10 * time.Second):
				t.Fatal("publication final proof did not return through real rollback")
			}
			scanPublicationIOWaitQueued(t, fixture.ctx, finished, &scanErr, baselineIO)
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != baselineOwners+1 {
				t.Fatalf("retry retained an abandoned publication owner: before=%d waiting=%d", baselineOwners, owners)
			}
			if counts := scanProbeOpenDescriptors(t, []string{fixture.path, first.fixture.path, second.fixture.path}); counts[0] != 1 || counts[1] != 1 || counts[2] != 1 {
				t.Fatalf("retry changed bounded original descriptor ownership: %v", counts)
			}
			if taskRefreshRowSnapshot(t, fixture.ctx, fixture.pool, "items", fixture.itemID) != beforeCatalog ||
				scanUnchangedProgressSnapshot(t, fixture.ctx, fixture.pool, fixture.state.task) != beforeProgress {
				t.Fatal("Busy final proof exposed catalog or accepted progress before a successful retry")
			}
			assertNoCatalogTestNotification(t, notifications)
			primarySidecarRetryAssertOwnedTransactionFree(t, fixture.ctx, fixture.store, unblock)
			file, _, reader, err := foreground.store.OpenOriginalMediaFor(foreground.ctx, Subject{UserID: foreground.userID},
				foregroundID, foregroundSnapshot.SourceID, foregroundSnapshot.ETag)
			if err != nil || file == nil || reader == nil {
				t.Fatalf("reserved foreground original did not open during publication backpressure: %v", err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			data := make([]byte, originalMediaReadChunk)
			if n, err := reader.Read(data); err != nil || n != len(data) || !bytes.Equal(data, primaryScanReadSourceBytes) {
				t.Fatalf("reserved foreground did not read actual original 32 KiB: bytes=%d error=%v", n, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if stats := originalMediaReadGovernor.Stats(); stats.Active != baselineIO.Active+2 || stats.Background != baselineIO.Background+2 || stats.Queued != baselineIO.Queued+1 {
				t.Fatalf("foreground read changed actual publication BG owners: %+v", stats)
			}
			if changed {
				if err := os.WriteFile(fixture.path, append(append([]byte(nil), primaryScanReadSourceBytes...), 'x'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			select {
			case <-finished:
				if scanErr != nil {
					t.Fatalf("publication window did not resolve its fresh retry: %v", scanErr)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("publication did not finish after actual BG release")
			}
			roots, writes, commits, rollbacks, traceErr := fixture.trace.snapshot()
			if traceErr != nil || rollbacks != 1 {
				t.Fatalf("publication trace did not record one clean final-proof rollback: roots=%d writes=%d commits=%d rollbacks=%d error=%v", roots, writes, commits, rollbacks, traceErr)
			}
			if changed {
				if roots != 1 || writes != 0 || commits != 0 || fixture.state.warnings != 1 ||
					taskRefreshRowSnapshot(t, fixture.ctx, fixture.pool, "items", fixture.itemID) != beforeCatalog ||
					fixture.state.task.job.Added != beforeJob.Added || fixture.state.task.job.Updated != beforeJob.Updated {
					t.Fatalf("a source change during admission was accepted or retried after rejection: roots=%d writes=%d commits=%d warnings=%d job=%+v", roots, writes, commits, fixture.state.warnings, fixture.state.task.job)
				}
				assertNoCatalogTestNotification(t, notifications)
			} else {
				var title string
				if err := fixture.pool.QueryRow(fixture.ctx, "SELECT name FROM items WHERE id=$1", fixture.itemID).Scan(&title); err != nil || title != "Accepted publication" ||
					roots != 2 || writes != 1 || commits != 1 || fixture.state.warnings != 0 ||
					fixture.state.task.job.Added != beforeJob.Added || fixture.state.task.job.Updated != beforeJob.Updated+1 {
					t.Fatalf("stable retry did not commit exactly one fresh primary projection: title=%q roots=%d writes=%d commits=%d warnings=%d job=%+v error=%v", title, roots, writes, commits, fixture.state.warnings, fixture.state.task.job, err)
				}
				assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
					Kind: CatalogUpdated, ItemID: fixture.itemID, LibraryID: fixture.state.library.ID, ParentID: fixture.state.library.ID,
				}})
				assertNoCatalogTestNotification(t, notifications)
			}
			job, err := fixture.store.GetJob(fixture.ctx, fixture.state.task.job.ID)
			if err != nil {
				t.Fatal(err)
			}
			taskScanAssertChild(t, fixture.ctx, fixture.pool, job.TaskChildID, job)
			for _, blocker := range []*scanPublicationIOBlocker{first, second} {
				select {
				case result := <-blocker.result:
					blocker.result <- result
					if result.err != nil || result.info.Size != originalMediaReadChunk {
						t.Fatalf("actual BG source did not join before retirement: %+v", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("actual BG source did not return")
				}
				if err := primaryScanReadClose(t, blocker.fixture.input, blocker.fixture.file.Close); err != nil {
					t.Fatal(err)
				}
			}
			fixture.retire(t)
			if counts := scanProbeOpenDescriptors(t, []string{fixture.path, first.fixture.path, second.fixture.path}); counts[0] != 0 || counts[1] != 0 || counts[2] != 0 {
				t.Fatalf("publication inputs survived actual retirement: %v", counts)
			}
			if stats := originalMediaReadGovernor.Stats(); stats != initialIO || originalMediaReadOwners.Stats().RegisteredOwners != initialOwners {
				t.Fatalf("publication retry retained common admission charges: before=%+v after=%+v owners=%+v", initialIO, stats, originalMediaReadOwners.Stats())
			}
		})
	}
}

type scanPublicationIOUnknownCapture struct {
	rootBindingWriteCapture
	observation *storageObservationLifetime
	calls       atomic.Int64
}

func (capture *scanPublicationIOUnknownCapture) Revalidate(ctx context.Context) error {
	if err := capture.rootBindingWriteCapture.Revalidate(ctx); err != nil {
		return err
	}
	capture.observation.mu.Lock()
	observing := capture.observation.active != 0
	capture.observation.mu.Unlock()
	if observing {
		capture.calls.Add(1)
		return errors.Join(ErrBusy, media.ErrProcessRetirementUnknown)
	}
	return nil
}

func TestScanPublicationRootIOUnknownJoinedBusyDoesNotRetry(t *testing.T) {
	initialIO, initialOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	fixture := scanPublicationIOPrepare(t)
	beforeCatalog := taskRefreshRowSnapshot(t, fixture.ctx, fixture.pool, "items", fixture.itemID)
	beforeProgress := scanUnchangedProgressSnapshot(t, fixture.ctx, fixture.pool, fixture.state.task)
	capture := fixture.pass.byRoot[fixture.state.root.id]
	operation := capture.primaryIO
	input := fixture.window.pending[0].input
	fixture.retirementError = media.ErrProcessRetirementUnknown
	unknown := &scanPublicationIOUnknownCapture{rootBindingWriteCapture: capture.capture, observation: &capture.observation}
	capture.capture = unknown
	var recoveryOnce sync.Once
	recoverActual := func() {
		recoveryOnce.Do(func() {
			// Close every actual descriptor and join the window before proving
			// retirement. Historical uncertainty remains in Close's result.
			fixture.retire(t)
			if err := operation.ConfirmRetired(func() error {
				select {
				case <-input.authority.closed:
				default:
					return errors.New("the uncertain source observation has not joined its descriptor close")
				}
				if _, err := input.file.Stat(); !errors.Is(err, os.ErrClosed) {
					return fmt.Errorf("the uncertain actual input descriptor remains open: %v", err)
				}
				capture.observation.mu.Lock()
				active := capture.observation.active
				capture.observation.mu.Unlock()
				if active != 0 || !capture.closed || capture.capture != nil || capture.opened != nil {
					return errors.New("the uncertain named-storage capture has not joined and closed its actual witnesses")
				}
				return nil
			}); err != nil {
				t.Errorf("confirm independently joined publication retirement: %v", err)
			}
		})
	}
	t.Cleanup(recoverActual)
	notifications := catalogChangesTestListener(t, fixture.store)
	fixture.trace.arm(nil)
	err := fixture.window.flush()
	if !errors.Is(err, media.ErrProcessRetirementUnknown) || !errors.Is(err, ErrBusy) {
		t.Fatalf("actual mixed final-proof error lost its uncertainty or capacity cause: %v", err)
	}
	roots, writes, commits, rollbacks, traceErr := fixture.trace.snapshot()
	if traceErr != nil || roots != 1 || writes != 0 || commits != 0 || rollbacks != 1 || unknown.calls.Load() != 1 || fixture.state.warnings != 0 {
		t.Fatalf("uncertain final proof retried or became a recoverable warning: roots=%d writes=%d commits=%d rollbacks=%d calls=%d warnings=%d trace_error=%v", roots, writes, commits, rollbacks, unknown.calls.Load(), fixture.state.warnings, traceErr)
	}
	if taskRefreshRowSnapshot(t, fixture.ctx, fixture.pool, "items", fixture.itemID) != beforeCatalog ||
		scanUnchangedProgressSnapshot(t, fixture.ctx, fixture.pool, fixture.state.task) != beforeProgress {
		t.Fatal("uncertain final proof changed retained catalog or accepted task progress")
	}
	assertNoCatalogTestNotification(t, notifications)
	primarySidecarRetryAssertOwnedTransactionFree(t, fixture.ctx, fixture.store, func() {})
	if stats := originalMediaReadGovernor.Stats(); stats.Active != initialIO.Active+1 || stats.Background != initialIO.Background+1 || stats.Queued != initialIO.Queued ||
		originalMediaReadOwners.Stats().RegisteredOwners != initialOwners+1 {
		t.Fatalf("unknown publication proof released its quarantined common owner: IO=%+v owners=%+v", stats, originalMediaReadOwners.Stats())
	}
	recoverActual()
	if count := scanProbeOpenDescriptors(t, []string{fixture.path})[0]; count != 0 {
		t.Fatalf("the returned mixed-error observation retained %d actual source descriptors", count)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != initialIO || originalMediaReadOwners.Stats().RegisteredOwners != initialOwners {
		t.Fatalf("the returned mixed-error observation retained common admission charges: before=%+v after=%+v owners=%+v", initialIO, stats, originalMediaReadOwners.Stats())
	}
}
