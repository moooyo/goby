//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/primaryio"
)

type primarySidecarRetryTrace struct {
	mu            sync.Mutex
	itemID        string
	deletes       int
	expectedOwner *pgx.Conn
	firstOwner    *pgx.Conn
	inject        func() error
	err           error
	rollback      chan struct{}
	rollbackOnce  sync.Once
}

type primarySidecarRetryQueryKey struct{}

func (trace *primarySidecarRetryTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.TrimSpace(data.SQL)
	if strings.HasPrefix(statement, "DELETE FROM item_images") && len(data.Args) != 0 && data.Args[0] == trace.itemID {
		trace.mu.Lock()
		trace.deletes++
		first := trace.deletes == 1
		if first {
			trace.firstOwner = conn
			if conn != trace.expectedOwner || conn.PgConn().TxStatus() != 'T' {
				trace.err = errors.Join(trace.err, errors.New("image replacement did not use an active owned transaction"))
			}
		}
		trace.mu.Unlock()
		if first {
			// All root preparation happened before the scan. The SQL tracer
			// starts only memory callbacks on immediately available BG leases.
			if err := trace.inject(); err != nil {
				trace.mu.Lock()
				trace.err = errors.Join(trace.err, err)
				trace.mu.Unlock()
			}
		}
	}
	if strings.EqualFold(statement, "rollback") {
		trace.mu.Lock()
		owned := trace.firstOwner == conn
		trace.mu.Unlock()
		if owned {
			return context.WithValue(ctx, primarySidecarRetryQueryKey{}, trace)
		}
	}
	return ctx
}

func (trace *primarySidecarRetryTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(primarySidecarRetryQueryKey{}) != trace {
		return
	}
	trace.mu.Lock()
	trace.err = errors.Join(trace.err, data.Err)
	trace.mu.Unlock()
	if data.Err == nil {
		trace.rollbackOnce.Do(func() { close(trace.rollback) })
	}
}

func (trace *primarySidecarRetryTrace) snapshot() (int, error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.deletes, trace.err
}

type primarySidecarRetryBlocker struct {
	operation *PrimaryRootIO
	rootID    string
	entered   chan struct{}
	done      chan struct{}
	started   atomic.Bool
	err       error
}

func (blocker *primarySidecarRetryBlocker) start(ctx context.Context, release <-chan struct{}) {
	if !blocker.started.CompareAndSwap(false, true) {
		return
	}
	go func() {
		blocker.err = blocker.operation.RunImmediate(ctx, blocker.rootID, primaryio.Background, func(work context.Context) error {
			close(blocker.entered)
			select {
			case <-release:
				return nil
			case <-work.Done():
				return work.Err()
			}
		})
		close(blocker.done)
	}()
}

func TestPrimarySidecarImageFinalProofBusyRetriesOutsideOwnedTransaction(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, fixtureStore, approved, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, approved, "movies/Film.mp4", "video:sidecar-final-proof-retry")
	poster := filepath.Join(filepath.Dir(mediaPath), "Film-poster.png")
	original := imageScanTestWrite(t, poster, color.NRGBA{R: 230, A: 255})
	library := libraryIntegrationCreate(t, ctx, fixtureStore, "Sidecar final proof retry", "movies", filepath.Dir(mediaPath))
	libraryIntegrationScan(t, ctx, fixtureStore, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, fixtureStore, userID, library.ID, mediaPath)
	before := imageScanTestList(t, ctx, fixtureStore, userID, item.ID, "Primary")
	if len(before) != 1 {
		t.Fatalf("old image catalog is incomplete: %+v", before)
	}
	stamp, err := os.Stat(poster)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtureStore.Close(ctx); err != nil {
		t.Fatalf("release fixture catalog ownership: %v", err)
	}
	trace := &primarySidecarRetryTrace{itemID: item.ID, rollback: make(chan struct{})}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create traced sidecar scan pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, prober, []string{approved})
	if err != nil {
		t.Fatalf("reopen traced sidecar Store: %v", err)
	}
	trace.expectedOwner = store.ownership.conn.Conn()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close traced sidecar Store: %v", err)
		}
	})
	state := imageScanTestState(t, ctx, tracedPool, store, library, ".")
	notifications := catalogChangesTestListener(t, store)
	baselineIO := originalMediaReadGovernor.Stats()
	baselineOwners := originalMediaReadOwners.Stats().RegisteredOwners
	if baselineIO.Active != 0 || baselineIO.Background != 0 || baselineIO.Queued != 0 {
		t.Fatalf("sidecar retry fixture needs idle common root admission: %+v", baselineIO)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	blockers := make([]*primarySidecarRetryBlocker, 0, 2)
	for range 2 {
		operation, row, err := state.prepareSidecarScanIO()
		if err != nil {
			t.Fatalf("prepare independent common BG owner: %v", err)
		}
		blocker := &primarySidecarRetryBlocker{operation: operation, rootID: row.root.id,
			entered: make(chan struct{}), done: make(chan struct{})}
		blockers = append(blockers, blocker)
		t.Cleanup(func() {
			unblock()
			if blocker.started.Load() {
				select {
				case <-blocker.done:
					if blocker.err != nil && !errors.Is(blocker.err, context.Canceled) {
						t.Errorf("common BG callback failed: %v", blocker.err)
					}
				case <-time.After(5 * time.Second):
					t.Error("common BG callback did not join its release")
				}
			}
			if err := blocker.operation.Close(); err != nil {
				t.Errorf("close common BG owner: %v", err)
			}
		})
	}
	trace.inject = func() error {
		for _, blocker := range blockers {
			blocker.start(ctx, release)
		}
		for _, blocker := range blockers {
			select {
			case <-blocker.entered:
			case <-blocker.done:
				return fmt.Errorf("occupy common BG lease before final source proof: %w", blocker.err)
			case <-time.After(5 * time.Second):
				return errors.New("common BG lease did not enter before final source proof")
			}
		}
		return nil
	}
	// Different valid PNG bytes keep the inode, names, length and timestamps.
	// The later retry must hash a fresh payload, not reuse this first attempt.
	firstPayload := append([]byte(nil), original...)
	firstPayload[len(firstPayload)-1] = 'b'
	primarySidecarRetryOverwrite(t, poster, firstPayload, stamp)
	var scanErr error
	scanDone := make(chan struct{})
	go func() {
		scanErr = state.scanImages(item.ID, item.Type, "Film.mp4", false)
		close(scanDone)
	}()
	t.Cleanup(func() {
		state.task.cancel()
		unblock()
		select {
		case <-scanDone:
		case <-time.After(5 * time.Second):
			t.Error("sidecar retry scan did not join cleanup")
		}
	})
	select {
	case <-trace.rollback:
	case <-scanDone:
		t.Fatalf("scan exited before a Busy final proof rolled back: %v", scanErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Busy final image source proof did not roll back its owned transaction")
	}
	// TraceQueryEnd precedes the owner mutex release and native cleanup. An
	// actual queued waiter proves the attempt retired before admission waiting.
	waitCtx, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for originalMediaReadGovernor.Stats().Queued != baselineIO.Queued+1 {
		select {
		case <-scanDone:
			t.Fatalf("Busy final image proof ended the scan instead of retrying: %v", scanErr)
		case <-waitCtx.Done():
			t.Fatalf("sidecar retry did not wait outside SQL for real BG capacity: %v", waitCtx.Err())
		case <-ticker.C:
		}
	}
	stats := originalMediaReadGovernor.Stats()
	if stats.Active != baselineIO.Active+2 || stats.Background != baselineIO.Background+2 ||
		originalMediaReadOwners.Stats().RegisteredOwners != baselineOwners+3 {
		t.Fatalf("retry retained its abandoned attempt or bypassed common BG admission: IO=%+v owners=%+v", stats, originalMediaReadOwners.Stats())
	}
	if count := mediaSourceWarmPipelineTestFileCount(t, before[0].Path); count != 0 {
		t.Fatalf("aborted image payload retained %d native descriptors while awaiting capacity", count)
	}
	if current := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); !reflect.DeepEqual(current, before) {
		t.Fatalf("Busy final proof published uncommitted image rows: before=%+v after=%+v", before, current)
	}
	assertNoCatalogTestNotification(t, notifications)
	primarySidecarRetryAssertOwnedTransactionFree(t, ctx, store, unblock)
	select {
	case <-scanDone:
		t.Fatalf("scan completed while both common BG callbacks still owned their leases: %v", scanErr)
	default:
	}
	currentPayload := append([]byte(nil), firstPayload...)
	currentPayload[len(currentPayload)-1] = 'c'
	primarySidecarRetryOverwrite(t, poster, currentPayload, stamp)
	unblock()
	select {
	case <-scanDone:
		if scanErr != nil {
			t.Fatalf("fresh image retry failed after common BG release: %v", scanErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fresh image payload did not commit after common BG release")
	}
	deletes, traceErr := trace.snapshot()
	if traceErr != nil || deletes < 2 {
		t.Fatalf("image retry did not restart its owned catalog attempt: deletes=%d error=%v", deletes, traceErr)
	}
	digest := sha256.Sum256(currentPayload)
	after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(after) != 1 || after[0].Tag != hex.EncodeToString(digest[:]) || after[0].Tag == before[0].Tag ||
		after[0].Size != before[0].Size || !after[0].ModifiedAt.Equal(before[0].ModifiedAt) || state.warnings != 0 {
		t.Fatalf("retry did not commit the fresh stable payload: before=%+v after=%+v warnings=%d", before, after, state.warnings)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID,
	}})
	assertNoCatalogTestNotification(t, notifications)
}

func primarySidecarRetryOverwrite(t *testing.T, path string, data []byte, stamp os.FileInfo) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(stamp, current) || current.Size() != stamp.Size() || !current.ModTime().Equal(stamp.ModTime()) {
		t.Fatalf("retry fixture changed image identity, size or timestamp: %v", err)
	}
}

func primarySidecarRetryAssertOwnedTransactionFree(t *testing.T, ctx context.Context, store *Store, unblock func()) {
	t.Helper()
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	completed := make(chan struct{})
	var resultErr error
	go func() {
		tx, err := store.beginOwnedTx(probeCtx)
		if err == nil {
			_, err = tx.Exec(probeCtx, "SELECT 1")
			err = errors.Join(err, tx.Rollback(probeCtx))
		}
		resultErr = err
		close(completed)
	}()
	select {
	case <-completed:
		if resultErr != nil {
			t.Fatalf("owned transaction remained unavailable during descriptor-free retry waiting: %v", resultErr)
		}
	case <-probeCtx.Done():
		// The owner mutex itself is not context-aware. Release the actual BG
		// callbacks before reporting failure so the probe can retire as well.
		unblock()
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Error("blocked owned transaction probe did not retire after BG release")
		}
		t.Fatal("sidecar retry waited for BG admission while retaining the owned SQL session")
	}
}
