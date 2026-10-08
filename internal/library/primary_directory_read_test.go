package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/primaryio"
)

// Unit fixtures need a trusted catalog scope for configured-anchor routing.
// The lazy pool is never queried; filesystem observations use real directories
// and the production governor rather than bypassing primary admission.
func primaryDirectoryUnitStore(t *testing.T, roots []approvedRoot) *Store {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Store{pool: pool, ownership: &scanOwnership{key: 1}, roots: roots}
}

func TestDirectoryPrimaryImmediateBusyRetainsRetryWithoutQueueing(t *testing.T) {
	operation, governor, owners, _ := primaryRootIOTestFixture(t, 1)
	route := operation.handle.state.routes["root-0"].route
	var held []*primaryio.PrimaryReadLease
	var blockers []*primaryio.Owner
	for range 3 {
		owner, err := owners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		lease, err := owner.Acquire(route, primaryio.Foreground)
		if err != nil {
			t.Fatal(err)
		}
		blockers, held = append(blockers, owner), append(held, lease)
	}
	t.Cleanup(func() {
		for index, lease := range held {
			_ = lease.Release()
			_ = blockers[index].Complete()
		}
	})
	called := false
	err := runDirectoryPrimaryImmediate(context.Background(), operation, "root-0", primaryio.Background, func(context.Context) error {
		called = true
		return nil
	})
	var retry *primaryDirectoryRetry
	if !errors.As(err, &retry) || called || governor.Stats().Queued != 0 {
		t.Fatalf("immediate admission waited or ran filesystem work: called=%t stats=%+v err=%v", called, governor.Stats(), err)
	}
	if err := held[0].Release(); err != nil {
		t.Fatal(err)
	}
	if err := blockers[0].Complete(); err != nil {
		t.Fatal(err)
	}
	if retry, err := waitDirectoryPrimaryError(context.Background(), rootBindingScanObservationFailure{err: err}); !retry || err != nil {
		t.Fatalf("rollback-complete retry did not retain its prepared route: retry=%t err=%v", retry, err)
	}
	if stats := governor.Stats(); stats.Active != 2 || stats.Queued != 0 {
		t.Fatalf("retry retained an active filesystem phase: %+v", stats)
	}
}

func TestScanReconciliationCatalogCleanupFailureRetiresFilesystemOwner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reconciliation root evidence requires Linux file identity and change time")
	}
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	path := operation.handle.state.routes["root-0"].domain
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	borrowed, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	evidence := newScanReconciliationEvidence()
	if err := evidence.AttachRoot("root-0", borrowed); err != nil {
		t.Fatal(err)
	}
	retained := evidence.roots["root-0"].anchor
	// A completed staging cleanup preserves its catalog error for every Close.
	// That SQL failure does not make an independently joined filesystem unknown.
	staging := &scanReconciliationStaging{closeErr: ErrUnavailable}
	staging.closed.Store(true)
	pass := &scanReconciliationPass{primaryIO: operation, staging: staging, evidence: evidence}
	if err := pass.Close(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("catalog cleanup failure was lost: %v", err)
	}
	if _, err := retained.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("catalog error prevented actual filesystem descriptor retirement: %v", err)
	}
	primaryRootIOTestWait(t, finished, "filesystem owner completion after catalog cleanup failure")
	if owners.Stats().RegisteredOwners != 0 || governor.Stats().Active != 0 || operation.handle.state.unknown {
		t.Fatalf("catalog failure quarantined a joined filesystem owner: owners=%+v phases=%+v", owners.Stats(), governor.Stats())
	}
}

func TestReconciliationRevalidationCovers256SerialRootPhases(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reconciliation root evidence requires Linux file identity and change time")
	}
	operation, governor, owners, _ := primaryRootIOTestFixture(t, 256)
	evidence := newScanReconciliationEvidence()
	t.Cleanup(func() { _ = evidence.Close() })
	captures := make([]*rootBindingScanCapture, 0, 256)
	var observed []string
	for index := range 256 {
		rootID := fmt.Sprintf("root-%d", index)
		path := operation.handle.state.routes[rootID].domain
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		opened, err := os.OpenRoot(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := opened.Stat(".")
		if err != nil {
			t.Fatal(err)
		}
		if err := evidence.AttachRoot(rootID, opened); err != nil {
			t.Fatal(err)
		}
		if err := evidence.RecordDirectory(rootID, ".", info, nil); err != nil {
			t.Fatal(err)
		}
		if err := evidence.CompleteDirectory(rootID, "."); err != nil {
			t.Fatal(err)
		}
		inner := &rootBindingScanTestCapture{revalidate: func(context.Context, int) error {
			stats := governor.Stats()
			if stats.Active != 1 || stats.Background != 1 || stats.ActiveRoots != 1 || stats.ActiveDomains != 1 || owners.Stats().RegisteredOwners != 1 {
				return fmt.Errorf("root %s lacks its exact single-root phase: %+v", rootID, stats)
			}
			observed = append(observed, rootID)
			return nil
		}}
		capture := &rootBindingScanCapture{row: rootBindingRow{root: libraryRoot{id: rootID}}, status: RootBindingVerified, opened: opened, capture: inner}
		captures = append(captures, capture)
		t.Cleanup(func() { _ = capture.Close() })
	}
	ctx := operation.Context(immediateDirectoryPrimaryContext(context.Background()))
	if err := revalidateScanReconciliation(ctx, captures, evidence); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 256 || !sort.StringsAreSorted(observed) {
		t.Fatalf("revalidation lost roots or observed them out of order: %d", len(observed))
	}
	if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
		t.Fatalf("full root proof retained an active phase: %+v", stats)
	}
}

func TestStorageObservationTransfersPhaseThroughRetiredResourceClose(t *testing.T) {
	var retained atomic.Int32
	var lifetime storageObservationLifetime
	entered, finishWork, closeEntered, finishClose, retired := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var workOnce, closeOnce sync.Once
	releaseWork := func() { workOnce.Do(func() { close(finishWork) }) }
	releaseClose := func() { closeOnce.Do(func() { close(finishClose) }) }
	ctx, cancel := context.WithCancel(context.Background())
	ctx = withStorageObservationPhaseRetention(ctx, func() (func() error, error) {
		retained.Add(1)
		return func() error { retained.Add(-1); close(retired); return nil }, nil
	})
	returned := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		releaseWork()
		releaseClose()
		select {
		case <-retired:
		case <-time.After(5 * time.Second):
			t.Error("actual observation did not retire its phase")
		}
	})
	go func() {
		returned <- runStorageObservationWithLimit(ctx, make(chan struct{}, 1), time.Minute, []*storageObservationLifetime{&lifetime}, func(context.Context) error {
			close(entered)
			<-finishWork
			return nil
		})
	}()
	<-entered
	cancel()
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller returned %v", err)
	}
	if retained.Load() != 1 {
		t.Fatal("caller cancellation released the actual worker phase")
	}
	if err := lifetime.retire(func() error {
		close(closeEntered)
		<-finishClose
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	releaseWork()
	<-closeEntered
	if retained.Load() != 1 {
		t.Fatal("worker released its phase before retired resources closed")
	}
	releaseClose()
	select {
	case <-retired:
	case <-time.After(5 * time.Second):
		t.Fatal("finished cleanup retained the phase")
	}
	if retained.Load() != 0 {
		t.Fatal("phase was not released exactly once")
	}
}
