package library

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Observe the actual owner-mutex wait, not a goroutine-start signal or an
// unrelated admission mutex. This keeps the barrier entirely in test code.
func ownedAdmissionWaitForOwner(t *testing.T, ctx context.Context, finished <-chan error, caller string) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	const storeFrame = "github.com/moooyo/goby/internal/library.(*Store)."
	var observed string
	for {
		buffer := make([]byte, 1<<20)
		size := runtime.Stack(buffer, true)
		for size == len(buffer) && len(buffer) < 8<<20 {
			buffer = make([]byte, 2*len(buffer))
			size = runtime.Stack(buffer, true)
		}
		var relevant []string
		for _, stack := range strings.Split(string(buffer[:size]), "\n\n") {
			if !strings.Contains(stack, storeFrame+caller+"(") {
				continue
			}
			relevant = append(relevant, stack)
			owner := strings.Index(stack, storeFrame+"lockOwnedSession(")
			admission := strings.Index(stack, storeFrame+"lockOwnedAdmission(")
			mutex := strings.Index(stack, "sync.(*Mutex).Lock(")
			if mutex < 0 {
				mutex = strings.Index(stack, "internal/sync.(*Mutex).lockSlow(")
			}
			if mutex >= 0 && owner > mutex && admission > owner {
				return
			}
		}
		observed = strings.Join(relevant, "\n\n")
		select {
		case err := <-finished:
			t.Fatalf("%s ended before waiting for the held owner: %v\n%s", caller, err, observed)
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("%s did not reach the owner-mutex barrier: %v\n%s", caller, waitCtx.Err(), observed)
		}
	}
}

func ownedAdmissionHoldOwner(t *testing.T, ctx context.Context, store *Store) (func(), <-chan error) {
	t.Helper()
	entered, held := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		finished <- store.WithOwnedTx(ctx, func(OwnedTx) error {
			close(entered)
			select {
			case <-held:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("hold the catalog owner: %v", err)
	case <-ctx.Done():
		t.Fatal("the owner callback did not enter its barrier")
	}
	return release, finished
}

func ownedAdmissionJoin(t *testing.T, finished <-chan error, operation string) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Errorf("%s did not stop after releasing every test barrier", operation)
	}
}

func ownedAdmissionIndependentWork(t *testing.T, ctx context.Context, store *Store, root libraryRoot, userID, itemID string) {
	t.Helper()
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"root open", func(context.Context) error {
			opened, err := store.openLibraryRoot(root)
			if opened != nil {
				defer opened.Close()
			}
			if err != nil {
				return err
			}
			_, err = opened.Stat(".")
			return err
		}},
		{"catalog read", func(work context.Context) error {
			library, err := store.GetLibrary(work, root.libraryID)
			if err == nil && library.ID != root.libraryID {
				return fmt.Errorf("read a different library: %q", library.ID)
			}
			return err
		}},
		{"independent user state", func(work context.Context) error {
			data, err := store.SetFavorite(work, userID, itemID, true)
			if err == nil && (data.ItemID != itemID || !data.IsFavorite) {
				return fmt.Errorf("favorite did not commit: %+v", data)
			}
			return err
		}},
	} {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		finished := make(chan error, 1)
		go func() { finished <- operation.run(work) }()
		select {
		case err := <-finished:
			cancel()
			if err != nil {
				t.Fatalf("%s failed behind an unrelated owner waiter: %v", operation.name, err)
			}
		case <-work.Done():
			cancel()
			t.Fatalf("%s was blocked by an unrelated owner waiter", operation.name)
		}
	}
}

func ownedAdmissionRoot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) libraryRoot {
	t.Helper()
	var root libraryRoot
	if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id=$1 ORDER BY id LIMIT 1`, libraryID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatalf("read the independent root: %v", err)
	}
	return root
}

func ownedAdmissionCheckWait(t *testing.T, ctx context.Context, store *Store, root libraryRoot, userID, itemID, caller string, operation func(context.Context) error) {
	t.Helper()
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	release, ownerDone := ownedAdmissionHoldOwner(t, ctx, store)
	defer release()
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		finished <- operation(request)
	}()
	defer func() {
		cancel()
		release()
		ownedAdmissionJoin(t, ownerDone, "the owner callback")
		ownedAdmissionJoin(t, finished, "the queued admission")
	}()
	ownedAdmissionWaitForOwner(t, ctx, finished, caller)
	ownedAdmissionIndependentWork(t, ctx, store, root, userID, itemID)
	cancel()
	release()
	if err := ownedTransactionsAwait(t, ctx, ownerDone); err != nil {
		t.Fatalf("release the unrelated owner: %v", err)
	}
	if err := ownedTransactionsAwait(t, ctx, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel admission while waiting for ownership: %v", err)
	}
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestOwnedAdmissionWaitDoesNotBlockIndependentWork(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library, items := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "owner-wait")
	root := ownedAdmissionRoot(t, ctx, pool, library.ID)
	var callbackCalled atomic.Bool
	childID := strings.Repeat("a", 32)
	for _, operation := range []struct {
		name, caller string
		run          func(context.Context) error
	}{
		{"callback", "WithOwnedTx", func(ctx context.Context) error {
			return store.WithOwnedTx(ctx, func(OwnedTx) error { callbackCalled.Store(true); return nil })
		}},
		{"collection", "beginCollectionWriteWithScope", func(ctx context.Context) error {
			tx, _, err := store.beginCollectionWrite(ctx, Subject{UserID: userID}, "", nil)
			if tx != nil {
				rollback(tx)
			}
			return err
		}},
		{"staging", "withScanStagingTx", func(ctx context.Context) error {
			return store.withScanStagingTx(ctx, false, func(*scanStagingTx) (func(), error) {
				callbackCalled.Store(true)
				return nil, nil
			})
		}},
		{"manual scan", "startScan", func(ctx context.Context) error {
			_, err := store.StartScan(ctx, library.ID)
			return err
		}},
		{"manual cancellation", "cancelJob", func(ctx context.Context) error {
			return store.CancelJob(ctx, "not-admitted")
		}},
		{"task scan", "AdmitTaskScan", func(ctx context.Context) error {
			_, err := store.AdmitTaskScan(ctx, childID)
			return err
		}},
		{"task cancellation", "CancelTaskScan", func(ctx context.Context) error {
			return store.CancelTaskScan(ctx, childID)
		}},
		{"library creation", "createLibraryWithCapture", func(ctx context.Context) error {
			_, err := store.CreateLibrary(ctx, "Waiting registration", "movies", []string{allowedRoot})
			return err
		}},
		{"library deletion", "deleteLibrary", func(ctx context.Context) error {
			return store.DeleteLibrary(ctx, library.ID)
		}},
		{"library editing", "updateLibrary", func(ctx context.Context) error {
			_, err := store.updateLibrary(ctx, nil, library.ID, LibraryUpdate{Revision: library.Revision})
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ownedAdmissionCheckWait(t, ctx, store, root, userID, items["Movie"], operation.caller, operation.run)
		})
	}
	if callbackCalled.Load() {
		t.Fatal("a cancelled owner waiter entered its transaction callback")
	}
}

func TestOwnedAdmissionCloseRejectsWaiterAndTransfersOwnership(t *testing.T) {
	for _, operation := range []string{"callback", "staging", "scan", "task scan"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			library, items := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "closing-owner-wait")
			root := ownedAdmissionRoot(t, ctx, pool, library.ID)
			release, ownerDone := ownedAdmissionHoldOwner(t, ctx, store)
			defer release()
			request, cancelRequest := context.WithCancel(ctx)
			defer cancelRequest()
			var called atomic.Bool
			caller := "WithOwnedTx"
			var run func() error
			switch operation {
			case "callback":
				run = func() error {
					return store.WithOwnedTx(request, func(tx OwnedTx) error {
						called.Store(true)
						_, err := tx.Exec(`INSERT INTO server_settings (key,value) VALUES ('closed-admission','unexpected')`)
						return err
					})
				}
			case "staging":
				caller = "withScanStagingTx"
				run = func() error {
					return store.withScanStagingTx(request, false, func(*scanStagingTx) (func(), error) {
						called.Store(true)
						return nil, nil
					})
				}
			case "scan":
				caller = "startScan"
				run = func() error { _, err := store.StartScan(request, library.ID); return err }
			case "task scan":
				caller = "AdmitTaskScan"
				run = func() error { _, err := store.AdmitTaskScan(request, strings.Repeat("b", 32)); return err }
			}
			finished := make(chan error, 1)
			go func() {
				defer close(finished)
				finished <- run()
			}()
			defer func() {
				cancelRequest()
				release()
				ownedAdmissionJoin(t, ownerDone, "the owner callback")
				ownedAdmissionJoin(t, finished, "the closing admission")
			}()
			ownedAdmissionWaitForOwner(t, ctx, finished, caller)
			ownedAdmissionIndependentWork(t, ctx, store, root, userID, items["Movie"])
			deadline, cancelDeadline := context.WithTimeout(ctx, 25*time.Millisecond)
			defer cancelDeadline()
			if err := store.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Close abandoned its deadline or released the active owner: %v", err)
			}
			select {
			case <-store.done:
				t.Fatal("Close completed before the admitted transaction ended")
			default:
			}
			if store.Available() {
				t.Fatal("Close did not fence new admission")
			}
			release()
			if err := ownedTransactionsAwait(t, ctx, ownerDone); err != nil {
				t.Fatalf("finish the previously admitted transaction: %v", err)
			}
			if err := ownedTransactionsAwait(t, ctx, finished); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("a waiter crossed the shutdown fence: %v", err)
			}
			if called.Load() {
				t.Fatal("a queued callback ran after shutdown began")
			}
			if err := store.Close(ctx); err != nil {
				t.Fatalf("join background shutdown: %v", err)
			}
			ownedTransactionsExpectAbsent(t, ctx, pool, "closed-admission")
			var jobs int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_jobs WHERE library_id=$1`, library.ID).Scan(&jobs); err != nil || jobs != 0 {
				t.Fatalf("shutdown committed an unqueued scan: jobs=%d, error=%v", jobs, err)
			}
			ownedTransactionsAssertTakeover(t, ctx, pool, store, allowedRoot)
		})
	}
}
