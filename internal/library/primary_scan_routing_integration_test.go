//go:build linux

package library

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Retain the same committed mapping and opaque operation that walk uses. The
// retained row is only an admission route; it must never grant actual I/O.
func primaryScanRoutingRetainWalk(t *testing.T, state *scanState) *PrimaryRootIO {
	t.Helper()
	row, err := state.readPrimaryScanAuthority(state.task.ctx)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := state.store.preparePrimaryRootIO(state.task.ctx,
		[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		t.Fatal(err)
	}
	state.walkIO, state.walkRow = operation, row
	t.Cleanup(func() {
		if err := operation.Close(); err != nil {
			t.Errorf("retire retained scan routing operation: %v", err)
		}
		state.walkIO = nil
	})
	return operation
}

// An actual kernel watch detects even a source open/read that is closed before
// the descriptor inventory can sample it. Setup completes before queueing.
func primaryScanRoutingWatchSource(t *testing.T, path string) func() {
	t.Helper()
	descriptor, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Close(descriptor); err != nil {
			t.Errorf("close routing source watch: %v", err)
		}
	})
	if _, err := syscall.InotifyAddWatch(descriptor, path, syscall.IN_OPEN|syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		var events [4096]byte
		count, err := syscall.Read(descriptor, events[:])
		if errors.Is(err, syscall.EAGAIN) {
			return
		}
		if err != nil {
			t.Fatalf("observe routing source access: %v", err)
		}
		t.Fatalf("source was opened or read before rejected fresh authority: event_bytes=%d", count)
	}
}

// These are upper workload budgets. Freshness is established by the queued
// mutation and locked-tuple tests, rather than by requiring redundant phases.
func primaryScanRoutingAssertAuthorityBudget(t *testing.T, trace *scanPerformanceSQLTracer, maximum int64) {
	t.Helper()
	singleRows, attempts := trace.authorityImplicitSingleRows.Load(), trace.authorityImplicitAttempts.Load()
	if singleRows == 0 || singleRows > maximum || attempts != singleRows || trace.authorityImplicitCommits.Load() != singleRows ||
		trace.authorityQueries.Load() != attempts || trace.authorityBegins.Load() != 0 || trace.authorityCommits.Load() != 0 ||
		trace.authorityRollbacks.Load() != 0 || trace.authorityImplicitErrors.Load() != 0 || trace.authorityImplicitEmpty.Load() != 0 ||
		trace.authorityImplicitUnexpectedRows.Load() != 0 {
		t.Fatalf("successful scan exceeded its one-request authority budget: single_rows=%d attempts=%d commits=%d SQL=%d explicit=%d/%d/%d errors=%d empty=%d unexpected=%d maximum=%d",
			singleRows, attempts, trace.authorityImplicitCommits.Load(), trace.authorityQueries.Load(), trace.authorityBegins.Load(),
			trace.authorityCommits.Load(), trace.authorityRollbacks.Load(), trace.authorityImplicitErrors.Load(),
			trace.authorityImplicitEmpty.Load(), trace.authorityImplicitUnexpectedRows.Load(), maximum)
	}
}

func TestPrimaryScanRoutingPreparationKeepsFreshAuthority(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "standalone"
		if retained {
			name = "retained_walk"
		}
		t.Run(name, func(t *testing.T) {
			_, _, _, state, trace, _ := scanCachedVisitFixture(t)
			if retained {
				primaryScanRoutingRetainWalk(t, state)
			}
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			trace.reset()
			input, err := state.prepareScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
			if err != nil || input == nil {
				t.Fatalf("prepare cached source under exact routing: input=%v error=%v", input, err)
			}
			if err := input.close(); err != nil {
				t.Fatal(err)
			}
			maximumAuthority := int64(2)
			if retained {
				maximumAuthority = 1
			}
			primaryScanRoutingAssertAuthorityBudget(t, trace, maximumAuthority)
			if trace.begins.Load() != 1 || trace.commits.Load() != 1 ||
				trace.rollbacks.Load() != 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
				t.Fatalf("routing preparation changed entry checkpoint or primary writes: begin=%d commit=%d rollback=%d items=%d metadata=%d",
					trace.begins.Load(), trace.commits.Load(), trace.rollbacks.Load(), trace.itemRows.Load(), trace.metadataRows.Load())
			}
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("source preparation retained admission charges: before=%+v after=%+v owners=%+v", beforeIO, stats, originalMediaReadOwners.Stats())
			}
		})
	}
}

func TestPrimaryScanRoutingRetainedWalkCachedVisitKeepsAuthorityBudget(t *testing.T) {
	ctx, pool, store, state, trace, _ := scanCachedVisitFixture(t)
	primaryScanRoutingRetainWalk(t, state)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	trace.reset()
	if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
		t.Fatal(err)
	}
	primaryScanRoutingAssertAuthorityBudget(t, trace, 4)
	if trace.begins.Load() != 1 || trace.commits.Load() != 1 || trace.cachedCompletionChecks.Load() != 1 {
		t.Fatalf("cached retained walk changed checkpoint boundaries: total=%d/%d completion=%d",
			trace.begins.Load(), trace.commits.Load(), trace.cachedCompletionChecks.Load())
	}
	if trace.rollbacks.Load() != 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
		t.Fatalf("cached routing visit rewrote primary facts or rolled back: rollback=%d items=%d metadata=%d",
			trace.rollbacks.Load(), trace.itemRows.Load(), trace.metadataRows.Load())
	}
	job, err := store.GetJob(ctx, state.task.job.ID)
	if err != nil || job.Scanned != state.task.job.Scanned || job.Added != state.task.job.Added || job.Updated != state.task.job.Updated {
		t.Fatalf("cached routing visit changed persisted counters: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, state.task.job.TaskChildID, job)
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
		t.Fatalf("cached routing visit retained admission charges: before=%+v after=%+v owners=%+v", beforeIO, stats, originalMediaReadOwners.Stats())
	}
}

func TestPrimaryScanRoutingMetadataRechecksQueuedAuthority(t *testing.T) {
	for _, phase := range []string{"prepare_source", "metadata"} {
		for _, retained := range []bool{false, true} {
			for _, change := range []string{"task_cancel", "binding_revision", "binding_document"} {
				route := "standalone"
				if retained {
					route = "retained_walk"
				}
				t.Run(phase+"/"+route+"/"+change, func(t *testing.T) {
					initialIO, initialOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
					if initialIO.Active != 0 || initialIO.Background != 0 || initialIO.Queued != 0 {
						t.Fatalf("metadata queue fixture needs idle admission: %+v", initialIO)
					}
					firstGate, secondGate := primaryScanReadTestGate(), primaryScanReadTestGate()
					first := primaryScanReadFixtureAt(t, firstGate, "")
					second := primaryScanReadFixtureAt(t, secondGate, first.state.root.allowedPath)
					candidate := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, first.state.root.allowedPath)
					if change == "binding_document" {
						if _, err := candidate.pool.Exec(candidate.ctx, `UPDATE library_roots
							SET storage_binding='{"routing_witness":"before"}',bound_at=clock_timestamp(),bound_by='routing-test' WHERE id=$1`, candidate.state.root.id); err != nil {
							t.Fatal(err)
						}
					}
					var walk *PrimaryRootIO
					if retained {
						walk = primaryScanRoutingRetainWalk(t, candidate.state)
					}
					assertUnread := primaryScanRoutingWatchSource(t, candidate.path)
					first.prepare(t)
					second.prepare(t)
					firstResult := primaryScanReadStartProbe(t, first, firstGate)
					secondResult := primaryScanReadStartProbe(t, second, secondGate)
					primaryScanReadSignal(t, first.ctx, firstGate.entered, "first actual background source read")
					primaryScanReadSignal(t, first.ctx, secondGate.entered, "second actual background source read")
					baseline := originalMediaReadGovernor.Stats()
					if baseline.Active != 2 || baseline.Background != 2 {
						t.Fatalf("actual readers did not saturate BG2: %+v", baseline)
					}
					beforeJob := candidate.state.task.job
					var called atomic.Bool
					result := make(chan primaryScanReadProbeResult, 1)
					finished := make(chan struct{})
					t.Cleanup(func() {
						candidate.state.task.cancel()
						firstGate.openGate()
						secondGate.openGate()
						cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						primaryScanReadSignal(t, cleanup, finished, "queued metadata cleanup")
					})
					go func() {
						defer close(finished)
						var err error
						if phase == "prepare_source" {
							var input *scannedMediaInput
							input, err = candidate.state.prepareScannedMedia("Feature.mp4", "video", scannedRoleOrdinary)
							if input != nil {
								called.Store(true)
								err = errors.Join(err, input.close())
							}
						} else {
							err = candidate.state.runPrimaryScanMetadata(candidate.state.task.ctx, func(context.Context) error {
								called.Store(true)
								file, err := openScanFile(candidate.state.opened, "Feature.mp4")
								if err != nil {
									return err
								}
								return file.Close()
							})
						}
						result <- primaryScanReadProbeResult{err: err}
					}()
					primaryScanReadQueued(t, candidate.ctx, baseline, result)
					assertUnread()
					if called.Load() || scanProbeOpenDescriptors(t, []string{candidate.path})[0] != 0 {
						t.Fatal("queued metadata delivered a source descriptor before fresh authority")
					}
					want := error(ErrRootBindingConflict)
					var err error
					switch change {
					case "task_cancel":
						want = context.Canceled
						_, err = candidate.pool.Exec(candidate.ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", beforeJob.ID)
					case "binding_revision":
						_, err = candidate.pool.Exec(candidate.ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", candidate.state.root.id)
					case "binding_document":
						_, err = candidate.pool.Exec(candidate.ctx, `UPDATE library_roots SET storage_binding='{"routing_witness":"after"}' WHERE id=$1`, candidate.state.root.id)
					}
					if err != nil {
						t.Fatal(err)
					}
					firstGate.openGate()
					if got := primaryScanReadReceive(t, first.ctx, firstResult); got.err != nil {
						t.Fatal(got.err)
					}
					got := primaryScanReadReceive(t, candidate.ctx, result)
					assertUnread()
					if !errors.Is(got.err, want) || called.Load() || scanProbeOpenDescriptors(t, []string{candidate.path})[0] != 0 ||
						candidate.state.task.job.Scanned != beforeJob.Scanned || candidate.state.warnings != 0 {
						t.Fatalf("metadata grant accepted stale authority: called=%v scanned=%d warnings=%d error=%v want=%v",
							called.Load(), candidate.state.task.job.Scanned, candidate.state.warnings, got.err, want)
					}
					secondGate.openGate()
					if got := primaryScanReadReceive(t, second.ctx, secondResult); got.err != nil {
						t.Fatal(got.err)
					}
					for _, holder := range []*primaryScanReadFixture{&first, &second} {
						if err := primaryScanReadClose(t, holder.input, holder.file.Close); err != nil {
							t.Fatal(err)
						}
					}
					if walk != nil {
						if err := walk.Close(); err != nil {
							t.Fatal(err)
						}
						candidate.state.walkIO = nil
					}
					if stats := originalMediaReadGovernor.Stats(); stats != initialIO || originalMediaReadOwners.Stats().RegisteredOwners != initialOwners {
						t.Fatalf("rejected metadata retained owners or phases: before=%+v after=%+v owners=%+v", initialIO, stats, originalMediaReadOwners.Stats())
					}
				})
			}
		}
	}
}

func TestPrimaryScanRoutingRetainsDetachedWalkBindingFence(t *testing.T) {
	f := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
	operation := primaryScanRoutingRetainWalk(t, f.state)
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	f.state.walkIO = nil
	if _, err := f.pool.Exec(f.ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", f.state.root.id); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := f.state.runPrimaryScanMetadata(f.state.task.ctx, func(context.Context) error { called = true; return nil }); !errors.Is(err, ErrRootBindingConflict) || called || scanProbeOpenDescriptors(t, []string{f.path})[0] != 0 {
		t.Fatalf("detached walk mapping lost its existing binding fence: called=%v error=%v", called, err)
	}
}

func TestPrimaryScanRoutingRejectsInvalidRetainedMapping(t *testing.T) {
	f := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
	primaryScanRoutingRetainWalk(t, f.state)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	f.state.walkRow.revision = 0
	if input, err := f.state.prepareScannedMedia("Feature.mp4", "video", scannedRoleOrdinary); !errors.Is(err, ErrRootBindingConflict) || input != nil || scanProbeOpenDescriptors(t, []string{f.path})[0] != 0 {
		t.Fatalf("invalid retained mapping reached source preparation: input=%v error=%v", input, err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
		t.Fatalf("invalid mapping retained admission resources: before=%+v after=%+v owners=%+v", beforeIO, stats, originalMediaReadOwners.Stats())
	}
}
