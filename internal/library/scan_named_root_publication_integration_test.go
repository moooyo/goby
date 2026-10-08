//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

type scanNamedRootPublicationTraceKey struct{}

// The mutation runs only after primary facts and candidate progress have been
// written in the actual owner transaction, before its final storage proof.
type scanNamedRootPublicationTrace struct {
	mu       sync.Mutex
	owner    *pgx.Conn
	itemID   string
	jobID    string
	armed    bool
	primary  bool
	injected bool
	inject   func() error
	err      error
}

func TestScanNamedRootLatePublicationRetainsBorrowedAnchor(t *testing.T) {
	ctx, _, store, state, _, _ := scanCachedVisitFixture(t)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	pass, err := store.prepareScanReconciliation(state.task, []libraryRoot{state.root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pass.Close() })
	state.reconciliationPass = pass
	input, err := state.inspectScannedMedia("Film.mp4", "video", scannedRoleOrdinary)
	if err != nil || input == nil {
		t.Fatalf("prepare retained publication source: input=%v error=%v", input, err)
	}
	t.Cleanup(func() {
		if err := input.close(); err != nil {
			t.Errorf("retire retained publication source: %v", err)
		}
	})
	capture := pass.byRoot[state.root.id]
	if capture == nil || input.primary.anchorCapture != capture || input.primary.ownedAnchor {
		t.Fatal("verified input opened another anchor instead of borrowing its capture")
	}
	anchor := input.primary.namedAnchor
	operation, err := input.primary.preparePublicationIO()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = operation.Close() })
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	observed := make(chan error, 1)
	finished := make(chan error, 1)
	proof, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	go func() {
		finished <- operation.RunImmediate(proof, state.root.id, primaryio.Background, func(work context.Context) error {
			return input.primary.observePublicationSource(work, func(observation context.Context) error {
				close(entered)
				<-release
				// Model a native read already in progress when its caller expired.
				err := input.primary.checkPhysicalSource(context.WithoutCancel(observation))
				observed <- err
				return err
			})
		})
	}()
	mediaSourceAdmissionTestWait(t, entered, "late primary physical source proof")
	if err := <-finished; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("physical proof caller did not return its deadline: %v", err)
	}
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pass.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Stat("."); err != nil || capture.closed {
		t.Fatalf("capture retired its borrowed anchor before the actual proof: closed=%v error=%v", capture.closed, err)
	}
	closed := make(chan error, 1)
	closing := make(chan struct{})
	go func() {
		close(closing)
		closed <- input.close()
	}()
	mediaSourceAdmissionTestWait(t, closing, "source close waiting for the physical proof")
	select {
	case err := <-closed:
		t.Fatalf("source close abandoned its actual proof worker: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if _, err := input.file.Stat(); err != nil {
		t.Fatalf("late physical worker lost its original file: %v", err)
	}
	unblock()
	if err := <-observed; err != nil {
		t.Fatalf("late physical proof lost retained source witnesses: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := input.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("joined physical proof retained its file: %v", err)
	}
	if _, err := anchor.Stat("."); !errors.Is(err, os.ErrClosed) || !capture.closed {
		t.Fatalf("joined physical proof retained its capture: closed=%v error=%v", capture.closed, err)
	}
	if originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners || originalMediaReadGovernor.Stats() != beforeIO {
		t.Fatalf("late physical proof retained admission: owners=%+v IO=%+v", originalMediaReadOwners.Stats(), originalMediaReadGovernor.Stats())
	}
}

func (trace *scanNamedRootPublicationTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if !trace.armed || conn != trace.owner {
		return ctx
	}
	statement := scanPerformanceNormalizeSQL(data.SQL)
	if strings.HasPrefix(statement, "insert into items ") && len(data.Args) != 0 && data.Args[0] == trace.itemID {
		trace.primary = true
	}
	if trace.primary && !trace.injected && strings.HasPrefix(statement, "update scan_jobs set scanned =") &&
		len(data.Args) != 0 && data.Args[0] == trace.jobID {
		return context.WithValue(ctx, scanNamedRootPublicationTraceKey{}, trace)
	}
	return ctx
}

func (trace *scanNamedRootPublicationTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(scanNamedRootPublicationTraceKey{}) != trace {
		return
	}
	trace.mu.Lock()
	if trace.injected {
		trace.mu.Unlock()
		return
	}
	trace.injected = true
	if data.Err != nil || conn != trace.owner || conn.PgConn().TxStatus() != 'T' || data.CommandTag.RowsAffected() != 1 {
		trace.err = errors.Join(data.Err, errors.New("named-root mutation did not reach accepted progress in the owner transaction"))
		trace.mu.Unlock()
		return
	}
	inject := trace.inject
	trace.mu.Unlock()
	err := inject()
	trace.mu.Lock()
	trace.err = err
	trace.mu.Unlock()
}

type scanNamedRootReplacement struct {
	allowed, retained, registered, mediaPath string
	anchor, root, file                       os.FileInfo
	renamed, created, moved                  bool
}

func newScanNamedRootReplacement(t *testing.T, allowed, registered, mediaPath string) *scanNamedRootReplacement {
	t.Helper()
	change := &scanNamedRootReplacement{allowed: allowed, retained: allowed + "-retained", registered: registered, mediaPath: mediaPath}
	var err error
	if change.anchor, err = os.Stat(allowed); err != nil {
		t.Fatal(err)
	}
	if change.root, err = os.Stat(registered); err != nil {
		t.Fatal(err)
	}
	if change.file, err = os.Stat(mediaPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if change.moved {
			if err := os.Rename(filepath.Join(change.allowed, "bridge"), filepath.Join(change.retained, "bridge")); err != nil {
				t.Errorf("restore original named-root bridge: %v", err)
				return
			}
		}
		if change.created {
			if err := os.Remove(change.allowed); err != nil {
				t.Errorf("remove empty replacement anchor: %v", err)
				return
			}
		}
		if change.renamed {
			if err := os.Rename(change.retained, change.allowed); err != nil {
				t.Errorf("restore configured anchor: %v", err)
			}
		}
	})
	return change
}

func (change *scanNamedRootReplacement) apply() error {
	if err := os.Rename(change.allowed, change.retained); err != nil {
		return err
	}
	change.renamed = true
	if err := os.Mkdir(change.allowed, change.anchor.Mode().Perm()); err != nil {
		return err
	}
	change.created = true
	// Move an intermediate ancestor, not the registered directory itself. Its
	// inode, mtime, ctime and the complete media stamp must remain unchanged.
	if err := os.Rename(filepath.Join(change.retained, "bridge"), filepath.Join(change.allowed, "bridge")); err != nil {
		return err
	}
	change.moved = true
	anchor, anchorErr := os.Stat(change.allowed)
	root, rootErr := os.Stat(change.registered)
	file, fileErr := os.Stat(change.mediaPath)
	if err := errors.Join(anchorErr, rootErr, fileErr); err != nil {
		return err
	}
	if os.SameFile(change.anchor, anchor) || !sameMediaSourceFile(change.root, root) || !sameMediaSourceFile(change.file, file) {
		return errors.New("ancestor replacement did not isolate the configured anchor from unchanged registered-root and media facts")
	}
	return nil
}

func scanNamedRootPublicationFixture(t *testing.T, trace *scanNamedRootPublicationTrace) (context.Context, *pgxpool.Pool, *Store, *scanState, string) {
	t.Helper()
	prober := &libraryFixtureProber{}
	ctx, pool, previous, allowed, _ := libraryIntegrationStore(t, prober)
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, prober, []string{allowed})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close named-root publication Store: %v", err)
		}
	})
	path := libraryIntegrationFile(t, allowed, "bridge/registered/Film.mp4", "video:named-root-publication")
	library := libraryIntegrationCreate(t, ctx, store, "Named root publication", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
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
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	state.task, state.themes = task, nil
	cachedObservationRefreshDirectory(t, state)
	if err := store.prepareScanOperationAuthority(ctx, task, []libraryRoot{state.root}); err != nil {
		t.Fatal(err)
	}
	scanProgressBatchHoldWindow(task)
	return ctx, tracedPool, store, state, allowed
}

func scanNamedRootCatalogSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM items i WHERE i.library_id=$1), '[]'::jsonb),
		'metadata', COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.item_id) FROM item_metadata_state m
			JOIN items i ON i.id=m.item_id WHERE i.library_id=$1), '[]'::jsonb),
		'images', COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.item_id,m.image_type,m.image_index) FROM item_images m
			JOIN items i ON i.id=m.item_id WHERE i.library_id=$1), '[]'::jsonb),
		'subtitles', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.item_id,s.stream_index) FROM item_subtitles s
			JOIN items i ON i.id=s.item_id WHERE i.library_id=$1), '[]'::jsonb))::text`, libraryID).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestScanNamedRootPublicationRejectsReplacedAllowedAncestor(t *testing.T) {
	for _, cached := range []bool{true, false} {
		mode := "cold_sync"
		if cached {
			mode = "cached_payload"
		}
		for _, boundary := range []string{"metadata_wait", "owned_final_proof"} {
			t.Run(mode+"/"+boundary, func(t *testing.T) {
				trace := &scanNamedRootPublicationTrace{}
				ctx, pool, store, state, allowed := scanNamedRootPublicationFixture(t, trace)
				relative := "Film.mp4"
				if boundary == "metadata_wait" {
					state.library.CollectionType = "mixed"
					relative = "Unpublished.S02E01.mp4"
					if err := os.Rename(filepath.Join(state.root.path, "Film.mp4"), filepath.Join(state.root.path, relative)); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(state.root.path, relative)
				base := strings.TrimSuffix(relative, filepath.Ext(relative))
				nfo := "<movie><title>Unpublished anchor change</title></movie>"
				if boundary == "metadata_wait" {
					nfo = "<episodedetails><title>Unpublished anchor change</title></episodedetails>"
				}
				if err := os.WriteFile(filepath.Join(state.root.path, base+".nfo"), []byte(nfo), 0600); err != nil {
					t.Fatal(err)
				}
				// A real subtitle candidate prevents the cached absence shortcut
				// from supplying an unrelated complete named-root observation.
				if err := os.WriteFile(filepath.Join(state.root.path, base+".en.srt"), []byte(subtitleTestSRT), 0600); err != nil {
					t.Fatal(err)
				}
				if !cached {
					stamp, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					changed := stamp.ModTime().Add(time.Second)
					if err := os.Chtimes(path, changed, changed); err != nil {
						t.Fatal(err)
					}
				}
				cachedObservationRefreshDirectory(t, state)
				baselineIO, baselineOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
				walk := primaryScanRoutingRetainWalk(t, state)
				prober := store.prober.(*libraryFixtureProber)
				beforeProbes := len(prober.calls())
				input, err := state.inspectScannedMedia(relative, "video", scannedRoleOrdinary)
				if input != nil {
					t.Cleanup(func() {
						if err := input.close(); err != nil {
							t.Errorf("close named-root input: %v", err)
						}
					})
				}
				if err != nil || input == nil || input.authority != nil || input.unchanged != cached {
					t.Fatalf("prepare nil-authority publication input: cached=%v input=%+v error=%v", cached, input, err)
				}
				// Rename reuse deliberately requires the original size and mtime.
				// The cold renamed source changed its mtime, so it plans a new item;
				// both cached renames and unchanged paths retain the existing ID.
				wantStored := cached || boundary == "owned_final_proof"
				if (input.stored.id != "") != wantStored {
					t.Fatalf("unexpected named-root fixture identity: cached=%v boundary=%s stored=%q want_existing=%v", cached, boundary, input.stored.id, wantStored)
				}
				wantProbes := beforeProbes
				if !cached {
					wantProbes++
					if input.primary.receipt == nil || !input.primary.receipt.RetirementComplete() || input.primary.receipt.UnknownObserved() {
						t.Fatal("cold fallback did not complete its real joined probe")
					}
				}
				if len(prober.calls()) != wantProbes {
					t.Fatalf("fixture used the wrong probe path: calls=%d want=%d", len(prober.calls()), wantProbes)
				}
				beforeCatalog := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID)
				beforeJob := state.task.job
				notifications := catalogChangesTestListener(t, store)
				change := newScanNamedRootReplacement(t, allowed, state.root.path, path)
				var result error
				if boundary == "metadata_wait" {
					unblock, retire := cachedObservationBlockBackground(t, ctx, state)
					done := make(chan struct{})
					t.Cleanup(func() {
						state.task.cancel()
						unblock()
						mediaSourceAdmissionTestWait(t, done, "named-root queued publication cleanup")
					})
					go func() {
						result = state.publishScannedMedia(relative, "video", hierarchy{parentID: state.library.ID}, input)
						close(done)
					}()
					wait, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					primaryReadTestWaitQueued(t, wait, baselineIO.Queued+1)
					if current := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID); current != beforeCatalog {
						t.Fatal("queued metadata created virtual hierarchy or primary facts before admission")
					}
					if err := change.apply(); err != nil {
						t.Fatal(err)
					}
					unblock()
					mediaSourceAdmissionTestWait(t, done, "named-root queued publication")
					retire()
				} else {
					trace.mu.Lock()
					trace.owner, trace.itemID, trace.jobID = store.ownership.conn.Conn(), input.stored.id, state.task.job.ID
					trace.inject, trace.armed = change.apply, true
					trace.mu.Unlock()
					result = state.publishScannedMedia(relative, "video", hierarchy{parentID: state.library.ID}, input)
					trace.mu.Lock()
					injected, injectErr := trace.injected, trace.err
					trace.armed = false
					trace.mu.Unlock()
					if !injected || injectErr != nil {
						t.Fatalf("owned final-proof mutation did not run: injected=%v error=%v", injected, injectErr)
					}
				}
				if result == nil && state.warnings == 0 {
					t.Fatal("changed configured anchor produced neither rejection nor warning")
				}
				if errors.Is(result, media.ErrProcessRetirementUnknown) || errors.Is(result, errScanPublicationRetirementUnknown) {
					t.Fatalf("ordinary anchor replacement introduced unknown retirement: %v", result)
				}
				if current := scanNamedRootCatalogSnapshot(t, ctx, pool, state.library.ID); current != beforeCatalog {
					t.Fatalf("changed configured anchor published catalog or virtual hierarchy: boundary=%s error=%v", boundary, result)
				}
				job, err := store.GetJob(ctx, state.task.job.ID)
				if err != nil || job.Added != beforeJob.Added || job.Updated != beforeJob.Updated ||
					state.task.job.Added != beforeJob.Added || state.task.job.Updated != beforeJob.Updated {
					t.Fatalf("rejected publication advanced accepted counters: before=%+v memory=%+v stored=%+v error=%v", beforeJob, state.task.job, job, err)
				}
				assertNoCatalogTestNotification(t, notifications)
				if granted, err := state.readPrimaryScanAuthority(ctx); err != nil || !granted.same(input.primary.row) {
					t.Fatalf("physical-anchor fixture unexpectedly changed startup permission: row=%+v error=%v", granted, err)
				}
				if err := input.close(); err != nil {
					t.Fatal(err)
				}
				if err := walk.Close(); err != nil {
					t.Fatal(err)
				}
				if stats := originalMediaReadGovernor.Stats(); stats != baselineIO || originalMediaReadOwners.Stats().RegisteredOwners != baselineOwners {
					t.Fatalf("rejected publication retained actual resources: before=%+v after=%+v owners=%+v", baselineIO, stats, originalMediaReadOwners.Stats())
				}
			})
		}
	}
}
