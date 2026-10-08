//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

func TestScanReconciliationFallbackRootUsesActualAdmissionAndStartupGrant(t *testing.T) {
	for _, mode := range []string{"individual_unbound", "missing_capture", "retained_unverified"} {
		for _, scenario := range []string{"read", "approval_edit", "cancel"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				fixture := newRootBindingScanFixture(t)
				ctx, store, task, root := fixture.ctx, fixture.store, fixture.task, fixture.scanRoot
				const payload = "video:legacy-unbound-readable"
				libraryIntegrationFile(t, root.path, "Legacy.mkv", payload)
				scanReconciliationCommitInsertItem(t, fixture, "fallback-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
				if _, err := fixture.pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE id=$1`, root.id); err != nil {
					t.Fatal(err)
				}
				if err := store.prepareScanOperationAuthority(ctx, task, []libraryRoot{root}); err != nil {
					t.Fatal(err)
				}
				row, err := store.readScanOperationAuthority(ctx, task, root)
				if err != nil || row.stored {
					t.Fatalf("fixture did not retain a legal unbound startup grant: row=%+v error=%v", row, err)
				}
				pass := &scanReconciliationPass{task: task}
				if mode == "individual_unbound" {
					pass, err = store.prepareScanReconciliation(task, []libraryRoot{root})
					if err != nil || !pass.individual {
						t.Fatalf("unbound root did not select ordinary individual scanning: pass=%+v error=%v", pass, err)
					}
				} else if mode == "retained_unverified" {
					pass.primaryIO, err = store.prepareScanOperationRootIO(ctx, task,
						[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
					if err != nil {
						t.Fatal(err)
					}
					pass.byRoot = map[string]*rootBindingScanCapture{root.id: {row: row, status: RootBindingUnbound}}
				}
				t.Cleanup(func() {
					if err := pass.Close(); err != nil {
						t.Errorf("close fallback reconciliation pass: %v", err)
					}
				})
				beforeIO := originalMediaReadGovernor.Stats()
				beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
				if beforeIO.Active != 0 || beforeIO.Background != 0 || beforeIO.Queued != 0 {
					t.Fatalf("root admission fixture requires idle actual capacity: %+v", beforeIO)
				}
				release := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				var blockers []*primarySidecarRetryBlocker
				for range 2 {
					operation, err := store.prepareScanOperationRootIO(ctx, task,
						[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
					if err != nil {
						t.Fatal(err)
					}
					blocker := &primarySidecarRetryBlocker{operation: operation, rootID: root.id,
						entered: make(chan struct{}), done: make(chan struct{})}
					blockers = append(blockers, blocker)
					t.Cleanup(func() {
						unblock()
						if blocker.started.Load() {
							mediaSourceAdmissionTestWait(t, blocker.done, "fallback root blocker cleanup")
						}
						if err := operation.Close(); err != nil {
							t.Errorf("close fallback root blocker: %v", err)
						}
					})
					blocker.start(ctx, release)
					mediaSourceAdmissionTestWait(t, blocker.entered, "fallback root actual background admission")
				}
				assertUnread := primaryScanRoutingWatchSource(t, root.path)
				done := make(chan struct{})
				var opened *os.Root
				var openErr error
				t.Cleanup(func() {
					task.cancel()
					unblock()
					mediaSourceAdmissionTestWait(t, done, "fallback root opener cleanup")
					if opened != nil {
						_ = opened.Close()
					}
				})
				go func() {
					opened, openErr = pass.openRoot(store, root)
					close(done)
				}()
				wait, cancelWait := context.WithTimeout(ctx, 5*time.Second)
				defer cancelWait()
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for originalMediaReadGovernor.Stats().Queued != beforeIO.Queued+1 {
					select {
					case <-done:
						t.Fatalf("fallback root bypassed occupied background admission: opened=%v error=%v", opened, openErr)
					case <-wait.Done():
						t.Fatal("fallback root did not queue for actual background capacity")
					case <-ticker.C:
					}
				}
				assertUnread()
				if scenario == "approval_edit" {
					if _, err := fixture.pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, root.id); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "cancel" {
					task.cancel()
				} else {
					unblock()
				}
				mediaSourceAdmissionTestWait(t, done, "fallback root admission result")
				if scenario == "cancel" {
					if opened != nil || !errors.Is(openErr, context.Canceled) {
						t.Fatalf("canceled root opening returned a usable root: opened=%v error=%v", opened, openErr)
					}
					assertUnread()
				} else {
					if openErr != nil || opened == nil {
						t.Fatalf("admitted legacy root was unreadable: opened=%v error=%v", opened, openErr)
					}
					file, err := opened.Open("Legacy.mkv")
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(file)
					closeErr := file.Close()
					if readErr != nil || closeErr != nil || string(data) != payload {
						t.Fatalf("legacy root changed its readable source: bytes=%q read=%v close=%v", data, readErr, closeErr)
					}
					if err := opened.Close(); err != nil {
						t.Fatal(err)
					}
					opened = nil
					if message, err := pass.finish(store, task, fixture.library, true, map[string]bool{}); err != nil || message != "" {
						t.Fatalf("unbound pass attempted destructive reconciliation: message=%q error=%v", message, err)
					}
				}
				if pass.eligible || pass.collector() != nil {
					t.Fatal("ordinary fallback opening granted destructive evidence")
				}
				scanReconciliationCommitAssertItem(t, fixture, "fallback-missing", true)
				unblock()
				for _, blocker := range blockers {
					mediaSourceAdmissionTestWait(t, blocker.done, "fallback root blocker retirement")
					if err := blocker.operation.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
					t.Fatalf("fallback opening retained admission: before=%+v after=%+v owners=%d/%d", beforeIO, stats,
						beforeOwners, originalMediaReadOwners.Stats().RegisteredOwners)
				}
			})
		}
	}
}

func TestScanReconciliationRootHandoffClosesCloneWhenAdmissionReturnsCanceled(t *testing.T) {
	for _, mode := range []string{"retained_pass", "individual_capture", "temporary_fallback"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
			source, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			var retire func() error
			if mode == "individual_capture" {
				capture := &rootBindingScanCapture{primaryIO: operation, status: RootBindingVerified, opened: source}
				retire = capture.Close
			} else if mode == "temporary_fallback" {
				retire = operation.Close
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var cloned *os.Root
			opened, err := openScanReconciliationRoot(ctx, operation, "root-0", func(context.Context) (*os.Root, error) {
				if stats := governor.Stats(); stats.Active != 1 || stats.Background != 1 {
					return nil, errors.New("root clone did not hold exactly one actual phase")
				}
				var err error
				cloned, err = source.OpenRoot(".")
				if err == nil {
					// The clone succeeded. Run appends the cancellation only after
					// this callback returns, reproducing the failed handoff window.
					cancel()
				}
				return cloned, err
			}, retire)
			if !errors.Is(err, context.Canceled) || opened != nil || cloned == nil {
				t.Fatalf("canceled admission handed off its root: opened=%v cloned=%v error=%v", opened, cloned, err)
			}
			if _, err := cloned.Stat("."); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("failed handoff leaked the actual cloned root: %v", err)
			}
			if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
				t.Fatalf("canceled handoff retained actual admission: %+v", stats)
			}
			wantOwners := 0
			if mode == "retained_pass" {
				wantOwners = 1
			}
			if got := owners.Stats().RegisteredOwners; got != wantOwners {
				t.Fatalf("handoff retired the wrong owner: got=%d want=%d", got, wantOwners)
			}
			if err := operation.Close(); err != nil {
				t.Fatal(err)
			}
			primaryRootIOTestWait(t, finished, "canceled root handoff retirement")
		})
	}
}

type rootHandoffFailedCapture struct {
	rootBindingScanTestCapture
	err error
}

func (capture *rootHandoffFailedCapture) Close() error { return capture.err }

func TestScanReconciliationRootHandoffPropagatesCaptureRetirementFailure(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	source, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	closeFailure := errors.New("capture descriptor retirement failed")
	inner := &rootHandoffFailedCapture{err: closeFailure}
	capture := &rootBindingScanCapture{primaryIO: operation, status: RootBindingVerified, opened: source, capture: inner}
	var cloned *os.Root
	opened, err := openScanReconciliationRoot(context.Background(), operation, "root-0", func(context.Context) (*os.Root, error) {
		var err error
		cloned, err = source.OpenRoot(".")
		return cloned, err
	}, capture.Close)
	t.Cleanup(func() {
		inner.err = nil
		if operation.handle.state.unknown {
			if err := operation.ConfirmRetired(inner.Close); err != nil {
				t.Errorf("retire the independently joined fixture capture: %v", err)
			}
		}
	})
	if !errors.Is(err, closeFailure) || !errors.Is(err, errSidecarRetirementUnknown) || opened != nil || cloned == nil {
		t.Fatalf("capture cleanup failure handed off a successful root: opened=%v cloned=%v error=%v", opened, cloned, err)
	}
	if _, err := cloned.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("failed capture retirement leaked its cloned root: %v", err)
	}
	if stats := governor.Stats(); stats.Active != 0 || owners.Stats().RegisteredOwners != 1 || !operation.handle.state.unknown {
		t.Fatalf("failed capture retirement released its unknown owner: IO=%+v owners=%+v", stats, owners.Stats())
	}
	inner.err = nil
	if err := operation.ConfirmRetired(inner.Close); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "independent failed-capture retirement")
}

func TestScanReconciliationFirstOpenRetainsOriginalAnchor(t *testing.T) {
	for _, mode := range []string{"lazy_unbound", "individual_verified", "retained_verified"} {
		t.Run(mode, func(t *testing.T) {
			prober := &libraryFixtureProber{}
			ctx, pool, initial, allowed, _ := scanReconciliationScanStore(t, prober)
			path := libraryIntegrationFile(t, allowed, "bridge/registered/Film.mp4", "video:first-root-source")
			library := libraryIntegrationCreate(t, ctx, initial, "First root source witness", "movies", filepath.Dir(path))
			var root libraryRoot
			if err := pool.QueryRow(ctx, `SELECT id,library_id,path,allowed_path,relative_path FROM library_roots WHERE library_id=$1`, library.ID).
				Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
				t.Fatal(err)
			}
			if mode == "lazy_unbound" {
				if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE id=$1`, root.id); err != nil {
					t.Fatal(err)
				}
			}
			if err := initial.Close(ctx); err != nil {
				t.Fatal(err)
			}
			store, err := New(pool, prober, []string{allowed})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := store.Close(cleanup); err != nil {
					t.Errorf("close first-root witness Store: %v", err)
				}
			})
			if len(store.roots) != 1 || store.roots[0].root != nil {
				t.Fatal("reopened Store did not leave its configured anchor lazy")
			}
			task := rootBindingScanOwnedTask(t, ctx, pool, store, library)
			if err := store.prepareScanOperationAuthority(ctx, task, []libraryRoot{root}); err != nil {
				t.Fatal(err)
			}
			pass := &scanReconciliationPass{task: task, individual: true}
			if mode == "individual_verified" {
				capture, err := store.prepareRootBindingScan(task, root)
				if err != nil || capture == nil || capture.status != RootBindingVerified {
					if capture != nil {
						_ = capture.Close()
					}
					t.Fatalf("individual fixture did not verify its real stored topology: capture=%v error=%v", capture, err)
				}
				if err := capture.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "retained_verified" {
				pass, err = store.prepareScanReconciliation(task, []libraryRoot{root})
				if err != nil || !pass.eligible || pass.individual {
					t.Fatalf("fixture did not retain a complete verified capture: pass=%+v error=%v", pass, err)
				}
			}
			t.Cleanup(func() {
				if err := pass.Close(); err != nil {
					t.Errorf("close first-root reconciliation pass: %v", err)
				}
			})
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			opened, source, err := pass.openSourceRoot(store, root)
			if err != nil || opened == nil || source == nil {
				t.Fatalf("first admitted open did not transfer its original witness: root=%v source=%v error=%v", opened, source, err)
			}
			t.Cleanup(func() { _ = opened.Close(); _ = source.Close() })
			if source.anchor == nil || source.root == nil || source.row.root != root {
				t.Fatal("root handoff omitted original source identities")
			}
			if mode == "retained_verified" {
				if source.capture != pass.byRoot[root.id] || source.ownedAnchor || source.ownedRoot {
					t.Fatal("normal verified handoff duplicated an already-retained capture")
				}
			} else if source.capture != nil || !source.ownedAnchor || !source.ownedRoot {
				t.Fatal("individual handoff retained full topology instead of lightweight source handles")
			}
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("source witness allocated or retained another governor owner: before=%+v after=%+v owners=%d/%d",
					beforeIO, stats, beforeOwners, originalMediaReadOwners.Stats().RegisteredOwners)
			}
			borrowed, err := source.borrow()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = borrowed.Close() })
			if err := errors.Join(opened.Close(), source.Close()); err != nil {
				t.Fatal(err)
			}
			// A group can retain its own handle after the walk and base witness
			// close, without reopening names or preserving a whole capture.
			nested, err := borrowed.borrow()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = nested.Close() })
			if err := borrowed.Close(); err != nil {
				t.Fatal(err)
			}
			operation, err := store.prepareScanOperationRootIO(ctx, task,
				[]mediaSourceRootHint{{root: source.row.root, bindingRevision: source.row.revision}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = operation.Close() })
			if err := operation.Run(ctx, root.id, primaryio.Background, nested.Check); err != nil {
				t.Fatalf("borrowed source did not survive its walk's retirement: %v", err)
			}
			change := newScanNamedRootReplacement(t, allowed, root.path, path)
			if err := change.apply(); err != nil {
				t.Fatal(err)
			}
			if err := operation.Run(ctx, root.id, primaryio.Background, nested.Check); !errors.Is(err, ErrRootBindingConflict) {
				t.Fatalf("first-open proof accepted a replaced anchor with unchanged child identities: %v", err)
			}
			if err := nested.Close(); err != nil {
				t.Fatal(err)
			}
			if mode != "retained_verified" {
				for name, held := range map[string]*os.Root{"original anchor": source.anchor, "registered root": source.root} {
					if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
						t.Fatalf("last group reference retained its %s: %v", name, err)
					}
				}
			}
		})
	}
}

func TestScanReconciliationSourceHandoffClosesProofAfterCloneCancellation(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	inner := &rootBindingScanTestCapture{snapshot: RootTopologySnapshot{
		Mapping: RootTopologyMapping{ApprovedPath: path, RegisteredPath: path},
	}}
	capture := &rootBindingScanCapture{primaryIO: operation, status: RootBindingVerified, opened: root, capture: inner,
		row: rootBindingRow{root: libraryRoot{id: "root-0", path: path, allowedPath: path, relativePath: "."}, revision: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cloned *os.Root
	var original *scanSourceRootWitness
	opened, proof, err := openScanReconciliationSource(ctx, operation, "root-0", func(work context.Context) (*os.Root, *scanSourceRootWitness, error) {
		var err error
		cloned, original, err = cloneScanReconciliationSource(work, capture, false, true)
		if err == nil {
			cancel()
		}
		return cloned, original, err
	}, capture.Close)
	if !errors.Is(err, context.Canceled) || opened != nil || proof != nil || original == nil || cloned == nil {
		t.Fatalf("canceled source handoff transferred its resources: root=%v proof=%v error=%v", opened, proof, err)
	}
	for name, held := range map[string]*os.Root{"walk root": cloned, "original anchor": original.anchor, "registered proof": original.root} {
		if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("canceled source handoff leaked %s: %v", name, err)
		}
	}
	if !capture.closed || inner.closes != 1 || governor.Stats().Active != 0 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("source handoff retained its full capture or owner: closed=%v closes=%d IO=%+v owners=%+v",
			capture.closed, inner.closes, governor.Stats(), owners.Stats())
	}
	primaryRootIOTestWait(t, finished, "canceled source witness retirement")
}
