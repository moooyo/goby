//go:build linux

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestLocalNFOExecutionGetsASlotWhileArtworkIsStillRunning(t *testing.T) {
	for _, limit := range []int{1, 2} {
		name := "shared worker limit"
		if limit == 1 {
			name = "single worker limit"
		}
		t.Run(name, func(t *testing.T) {
			f := newManagerFixture(t, library.WithDeferredLocalMetadata())
			for _, collection := range f.addLibraries(t, 2) {
				if err := os.WriteFile(filepath.Join(collection.path, "Fixture.nfo"), []byte(`<movie><title>Accepted</title><mpaa>PG</mpaa></movie>`), 0o600); err != nil {
					t.Fatal(err)
				}
				scan, err := f.catalog.StartScan(f.ctx, collection.id)
				if err != nil {
					t.Fatal(err)
				}
				managerWait(t, f.ctx, "accepted source", func(ctx context.Context) bool {
					job, err := f.catalog.GetJob(ctx, scan.ID)
					return err == nil && job.Status == "Completed"
				})
			}
			entered, release := make(chan Work, 2), make(chan struct{})
			registry, err := NewExecutorRegistry(
				ExecutorRegistration{Key: library.TaskLocalArtworkIngestKey, Name: "Local artwork", Executor: heldArtworkExecutor{entered, release}},
				ExecutorRegistration{Key: library.TaskLocalMetadataIngestKey, Name: "Local NFO", Executor: libraryBatchTestExecutor{}},
			)
			if err != nil {
				t.Fatal(err)
			}
			store, err := New(f.pool, f.catalog, registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Reconcile(f.ctx); err != nil {
				t.Fatal(err)
			}
			start := func(key string) Run {
				t.Helper()
				definition, err := store.GetByKey(f.ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				started, err := store.Start(f.ctx, f.actor, StartRequest{TaskID: definition.ID})
				if err != nil || started.Run.TotalChildren != 2 {
					t.Fatalf("%s did not independently admit both libraries: %+v, %v", key, started.Run, err)
				}
				return started.Run
			}
			artwork := start(library.TaskLocalArtworkIngestKey)
			manager := newManagerOwnershipFixture(t, f.ctx, store, f.catalog)
			manager.options.MaxConcurrent = func() int { return limit }
			t.Cleanup(manager.drainExecutions)
			pass := func() {
				t.Helper()
				if _, err := manager.reconcile(f.ctx, false); err != nil {
					t.Fatal(err)
				}
			}
			pass()
			var held Work
			select {
			case held = <-entered:
			case <-f.ctx.Done():
				t.Fatal("the artwork worker did not start")
			}
			if len(manager.executions) != 1 || held.RunID != artwork.ID {
				t.Fatal("artwork occupied more than one generic worker slot")
			}
			nfo := start(library.TaskLocalMetadataIngestKey)
			pass()
			if limit == 1 {
				if len(manager.executions) != 1 {
					t.Fatal("the one-worker setting was exceeded")
				}
				close(release)
				analysisWaitDone(t, manager.executions[held.ChildID].done)
				pass()
			}
			var nfoWorker *workerExecution
			for _, worker := range manager.executions {
				if worker.runID == nfo.ID {
					nfoWorker = worker
				}
			}
			if nfoWorker == nil {
				t.Fatal("the artwork backlog prevented an accepted NFO from being dispatched")
			}
			analysisWaitDone(t, nfoWorker.done)
			if nfoWorker.err != nil {
				t.Fatalf("NFO worker failed: %v", nfoWorker.err)
			}
			if limit > 1 {
				select {
				case <-manager.executions[held.ChildID].done:
					t.Fatal("the NFO check did not run while artwork was held")
				default:
				}
				close(release)
			}
		})
	}
}

type heldArtworkExecutor struct {
	entered chan<- Work
	release <-chan struct{}
}

func (heldArtworkExecutor) Available() bool { return true }

func (e heldArtworkExecutor) Execute(ctx context.Context, work Work, _ func(Progress) error) error {
	e.entered <- work
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
