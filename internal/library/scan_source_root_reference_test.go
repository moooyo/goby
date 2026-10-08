package library

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestScanSourceRootBorrowCloseDoesNotAllocateRetirementWorker(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := &scanSourceRootWitness{root: root, ownedRoot: true}
	t.Cleanup(func() {
		if err := origin.Close(); err != nil {
			t.Error(err)
		}
	})
	const samples = 64
	// AllocsPerRun performs one warm-up call in addition to measured calls.
	borrowed := make([]*scanSourceRootWitness, samples+1)
	for index := range borrowed {
		borrowed[index], err = origin.borrow()
		if err != nil {
			t.Fatal(err)
		}
	}
	index := 0
	var closeErr error
	allocations := testing.AllocsPerRun(samples, func() {
		if err := borrowed[index].Close(); err != nil {
			closeErr = err
		}
		index++
	})
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if allocations != 0 {
		t.Fatalf("closing a borrowed reference allocated a retirement worker: allocations=%g", allocations)
	}
	if _, err := root.Stat("."); err != nil {
		t.Fatalf("borrowed handle retirement closed the live origin: %v", err)
	}
	if err := origin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("origin retirement left its descriptor open: %v", err)
	}
}

func TestScanSourceRootBorrowRetainsLateWorkerAfterOriginClose(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	origin := &scanSourceRootWitness{root: root, ownedRoot: true}
	borrowed, err := origin.borrow()
	if err != nil {
		t.Fatal(err)
	}
	worker, err := borrowed.Retain()
	if err != nil {
		t.Fatal(err)
	}
	if err := origin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); err != nil {
		t.Fatalf("closing the origin and borrowed handle retired the actual worker: %v", err)
	}
	if late, err := borrowed.borrow(); late != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a retired handle admitted a new borrower: witness=%v error=%v", late, err)
	}
	if err := worker(); err != nil {
		t.Fatal(err)
	}
	if err := worker(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("the final worker did not retire its original descriptor: %v", err)
	}
}

func TestStorageObservationReferenceRetirementIsolatesOriginFailure(t *testing.T) {
	failure := errors.New("origin cleanup failed")
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			var origin, borrowed storageObservationLifetime
			if !origin.retain() || !borrowed.retain() {
				t.Fatal("retain the borrowed origin and its worker")
			}
			calls := 0
			if err := origin.retire(func() error {
				calls++
				switch mode {
				case "panic":
					panic("origin cleanup panic")
				case "goexit":
					runtime.Goexit()
				}
				return failure
			}); err != nil {
				t.Fatal(err)
			}
			if err := borrowed.retireReference(&origin); err != nil || calls != 0 {
				t.Fatalf("retirement did not retain the active worker: calls=%d error=%v", calls, err)
			}
			finished := make(chan error, 1)
			go func() { finished <- borrowed.release() }()
			want := failure
			if mode != "error" {
				want = ErrUnavailable
			}
			select {
			case err := <-finished:
				if !errors.Is(err, want) {
					t.Fatalf("final reference lost origin cleanup failure: got=%v want=%v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("abnormal origin cleanup abandoned the final reference worker")
			}
			if err := borrowed.retireReference(&origin); !errors.Is(err, want) || calls != 1 {
				t.Fatalf("repeated retirement changed the receipt or repeated cleanup: calls=%d error=%v", calls, err)
			}
		})
	}
}
