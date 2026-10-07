//go:build linux

package library

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type imageScanReadonlyFixture struct {
	ctx           context.Context
	pool          *pgxpool.Pool
	store         *Store
	state         *scanState
	trace         *imageScanNoopProofTrace
	itemID        string
	poster        string
	notifications chan CatalogNotification
}

func imageScanReadonlyPrepare(t *testing.T, populated bool, allowedCloseErrors ...error) imageScanReadonlyFixture {
	t.Helper()
	return imageScanReadonlyPrepareWithConfig(t, populated, nil, allowedCloseErrors...)
}

func imageScanReadonlyPrepareWithConfig(t *testing.T, populated bool, configure func(*pgxpool.Config), allowedCloseErrors ...error) imageScanReadonlyFixture {
	t.Helper()
	ctx, pool, original, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	media := libraryIntegrationFile(t, approved, "movies/Film.mp4", "video:image-readonly-noop")
	poster := filepath.Join(filepath.Dir(media), "Film-poster.png")
	if populated {
		imageScanTestWrite(t, poster, color.White)
	}
	library := libraryIntegrationCreate(t, ctx, original, "Read-only image no-op", "movies", filepath.Dir(media))
	libraryIntegrationScan(t, ctx, original, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Film.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &imageScanNoopProofTrace{}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	if configure != nil {
		configure(configuration)
	}
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, &libraryFixtureProber{}, []string{approved})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			for _, allowed := range allowedCloseErrors {
				if errors.Is(err, allowed) {
					return
				}
			}
			t.Errorf("close read-only image store: %v", err)
		}
	})
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	primaryScanRoutingRetainWalk(t, state)
	trace.authority.owner.Store(store.ownership.conn.Conn())
	trace.authority.reset()
	trace.arm(itemID, nil)
	return imageScanReadonlyFixture{ctx: ctx, pool: pool, store: store, state: state, trace: trace,
		itemID: itemID, poster: poster, notifications: catalogChangesTestListener(t, store)}
}

func imageScanReadonlyAssertNoPublication(t *testing.T, fixture imageScanReadonlyFixture) {
	t.Helper()
	reads, err := fixture.trace.snapshot()
	trace := &fixture.trace.authority
	if err != nil || reads != 1 || trace.authority.queries.Load() != 1 || trace.begins.Load() != 0 ||
		trace.ownedCommits.Load() != 0 || trace.authority.rollbacks.Load() != 0 || trace.imageWrites.Load() != 0 || trace.startup.startups.Load() != 0 {
		t.Fatalf("read-only image visit changed its SQL boundary: reads=%d queries=%d begins=%d commits=%d rollbacks=%d writes=%d startups=%d error=%v",
			reads, trace.authority.queries.Load(), trace.begins.Load(), trace.ownedCommits.Load(), trace.authority.rollbacks.Load(),
			trace.imageWrites.Load(), trace.startup.startups.Load(), err)
	}
	assertNoCatalogTestNotification(t, fixture.notifications)
}

func TestScanImageReadonlyNoopDefersDatabaseCancellationToCheckpoint(t *testing.T) {
	fixture := imageScanReadonlyPrepare(t, true)
	before := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)
	scanProgressBatchHoldWindow(fixture.state.task)
	fixture.state.task.job.Scanned += scanProgressCheckpointItems - 1
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", fixture.state.task.job.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false); err != nil || fixture.state.warnings != 0 {
		t.Fatalf("image no-op changed the cooperative cancellation boundary: warnings=%d error=%v", fixture.state.warnings, err)
	}
	if err := fixture.store.maybePersistProgress(fixture.state.task); err != nil {
		t.Fatalf("image visit forced an early database checkpoint: %v", err)
	}
	imageScanReadonlyAssertNoPublication(t, fixture)
	if after := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID); !reflect.DeepEqual(after, before) {
		t.Fatalf("read-only image visit changed tuple versions: before=%v after=%v", before, after)
	}
	fixture.state.task.job.Scanned++
	if err := fixture.store.maybePersistProgress(fixture.state.task); !errors.Is(err, context.Canceled) {
		t.Fatalf("the ordinary entry checkpoint missed database cancellation: %v", err)
	}
}

func TestScanImageReadonlyNoopChecksSourceAndLifetimeAfterSQLWait(t *testing.T) {
	for _, scenario := range []string{"replaced_source", "cancelled_context", "store_closed", "owner_lost"} {
		t.Run(scenario, func(t *testing.T) {
			var allowedCloseErrors []error
			if scenario == "owner_lost" {
				allowedCloseErrors = []error{ErrUnavailable}
			}
			fixture := imageScanReadonlyPrepare(t, true, allowedCloseErrors...)
			before := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)
			beforeRow := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary")
			replacement := filepath.Join(filepath.Dir(fixture.poster), ".replacement")
			if scenario == "replaced_source" {
				contents, err := os.ReadFile(fixture.poster)
				if err != nil {
					t.Fatal(err)
				}
				stamp, err := os.Stat(fixture.poster)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(replacement, contents, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(replacement, stamp.ModTime(), stamp.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			gate, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(gate)
			if _, err := gate.Exec(fixture.ctx, "LOCK TABLE item_images IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			finished, joined := make(chan error, 1), make(chan struct{})
			go func() {
				finished <- fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false)
				close(joined)
			}()
			t.Cleanup(func() {
				fixture.state.task.cancel()
				rollback(gate)
				mediaSourceAdmissionTestWait(t, joined, "read-only image SQL wait cleanup")
			})
			ownerPID := int32(fixture.store.ownership.conn.Conn().PgConn().PID())
			ownedTransactionsWaitForBlock(t, fixture.ctx, fixture.pool, ownerPID, int32(gate.Conn().PgConn().PID()), finished)
			var query string
			if err := fixture.pool.QueryRow(fixture.ctx, "SELECT query FROM pg_stat_activity WHERE pid=$1", ownerPID).Scan(&query); err != nil ||
				!strings.Contains(query, "/* image_catalog_unchanged */") {
				t.Fatalf("the SQL barrier did not hold the read-only image snapshot: query=%q error=%v", query, err)
			}
			switch scenario {
			case "replaced_source":
				if err := os.Rename(replacement, fixture.poster); err != nil {
					t.Fatal(err)
				}
			case "cancelled_context":
				fixture.state.task.cancel()
			case "store_closed":
				closeCtx, cancel := context.WithCancel(fixture.ctx)
				cancel()
				if err := fixture.store.Close(closeCtx); !errors.Is(err, context.Canceled) {
					t.Fatalf("Close did not retain the active image operation: %v", err)
				}
			case "owner_lost":
				ownedTransactionsTerminateBackend(t, fixture.ctx, fixture.pool, ownerPID)
			}
			if err := gate.Rollback(fixture.ctx); err != nil {
				t.Fatal(err)
			}
			mediaSourceAdmissionTestWait(t, joined, "read-only image SQL wait completion")
			err = <-finished
			if scenario == "replaced_source" {
				if err != nil || fixture.state.warnings != 1 {
					t.Fatalf("post-query proof accepted a replaced image: warnings=%d error=%v", fixture.state.warnings, err)
				}
			} else if err == nil || scenario == "cancelled_context" && !errors.Is(err, context.Canceled) {
				t.Fatalf("read-only image visit accepted its invalidated lifetime: %v", err)
			}
			if scenario != "owner_lost" {
				imageScanReadonlyAssertNoPublication(t, fixture)
			} else if fixture.store.Available() || !fixture.store.ownership.lost.Load() || fixture.trace.authority.begins.Load() != 0 || fixture.trace.authority.imageWrites.Load() != 0 {
				t.Fatalf("failed image SELECT retained ownership or entered a writer: available=%t lost=%t error=%v",
					fixture.store.Available(), fixture.store.ownership.lost.Load(), err)
			}
			if after := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID); !reflect.DeepEqual(after, before) ||
				imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary") != beforeRow {
				t.Fatalf("rejected image observation changed the retained rowset: before=%v after=%v", before, after)
			}
			assertNoCatalogTestNotification(t, fixture.notifications)
			if count := mediaSourceWarmPipelineTestFileCount(t, fixture.poster); count != 0 {
				t.Fatalf("read-only image rejection retained %d source descriptors", count)
			}
		})
	}
}

func TestScanImageReadonlyNoopDoesNotWaitForUncommittedImageWriter(t *testing.T) {
	fixture := imageScanReadonlyPrepare(t, true)
	before := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary")
	writer, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(writer)
	if _, err := writer.Exec(fixture.ctx, "UPDATE item_images SET source_hash=repeat('f',64) WHERE item_id=$1", fixture.itemID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		rollback(writer)
		<-finished
		t.Fatal("read-only image snapshot waited for an uncommitted row writer")
	}
	imageScanReadonlyAssertNoPublication(t, fixture)
	if visible := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary"); visible != before {
		t.Fatalf("an uncommitted image writer became visible to the no-op: before=%s after=%s", before, visible)
	}
	if err := writer.Commit(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	var tag string
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT source_hash FROM item_images WHERE item_id=$1", fixture.itemID).Scan(&tag); err != nil || tag != strings.Repeat("f", 64) {
		t.Fatalf("the completed no-op overwrote the later image commit: tag=%q error=%v", tag, err)
	}
}

func TestScanImageReadonlyNoopReleasesOwnerBeforeProofAndRetainsLaterWriter(t *testing.T) {
	fixture := imageScanReadonlyPrepare(t, true)
	observed := make(chan struct{})
	phase := fixture.state.walkIO.handle.state
	var gateMu sync.Mutex
	var held, released bool
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			gateMu.Lock()
			defer gateMu.Unlock()
			released = true
			if held {
				phase.mu.Unlock()
				held = false
			}
		})
	}
	fixture.trace.arm(fixture.itemID, func() error {
		gateMu.Lock()
		defer gateMu.Unlock()
		if !released {
			phase.mu.Lock()
			held = true
		}
		close(observed)
		return nil
	})
	finished, joined := make(chan error, 1), make(chan struct{})
	t.Cleanup(func() {
		release()
		fixture.state.task.cancel()
		mediaSourceAdmissionTestWait(t, joined, "image proof gate cleanup")
	})
	go func() {
		finished <- fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false)
		close(joined)
	}()
	mediaSourceAdmissionTestWait(t, observed, "completed read-only image snapshot")
	imageScanReadonlyAssertNoPublication(t, fixture)
	change := CatalogChange{Kind: CatalogUpdated, ItemID: fixture.itemID, LibraryID: fixture.state.library.ID, ParentID: fixture.state.library.ID}
	written := make(chan error, 1)
	go func() {
		tx, err := fixture.store.beginOwnedTx(fixture.ctx)
		if err == nil {
			defer rollback(tx)
			_, err = tx.Exec(fixture.ctx, "UPDATE item_images SET source_hash=repeat('f',64) WHERE item_id=$1", fixture.itemID)
			if err == nil {
				err = recordCatalogChanges(tx, change)
			}
			if err == nil {
				err = tx.Commit(fixture.ctx)
			}
		}
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		release()
		<-written
		t.Fatal("final image source proof retained the owner mutex")
	}
	afterWriter := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary")
	versions := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, fixture.notifications), []CatalogChange{change})
	release()
	mediaSourceAdmissionTestWait(t, joined, "image proof after the later writer")
	if err := <-finished; err != nil || fixture.state.warnings != 0 {
		t.Fatalf("stable image proof failed after a later database commit: warnings=%d error=%v", fixture.state.warnings, err)
	}
	if current := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary"); current != afterWriter ||
		!reflect.DeepEqual(scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID), versions) {
		t.Fatalf("no-op overwrote the writer accepted after its snapshot: before=%s after=%s", afterWriter, current)
	}
	if fixture.trace.authority.ownedBegins.Load() != 1 || fixture.trace.authority.ownedCommits.Load() != 1 || fixture.trace.authority.imageWrites.Load() != 0 {
		t.Fatal("the no-op added a transaction or image replacement around the later writer")
	}
	assertNoCatalogTestNotification(t, fixture.notifications)
}

func TestScanImageReadonlyMismatchRereadsLockedRowsAndNotificationSnapshot(t *testing.T) {
	for _, scenario := range []string{"concurrent_repair", "later_public_change", "failed_commit"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := imageScanReadonlyPrepare(t, true)
			accepted := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary")
			var identity string
			if err := fixture.pool.QueryRow(fixture.ctx, "SELECT file_identity FROM item_images WHERE item_id=$1", fixture.itemID).Scan(&identity); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE item_images SET file_identity='stale-before-readonly-snapshot' WHERE item_id=$1", fixture.itemID); err != nil {
				t.Fatal(err)
			}
			var laterRow string
			var laterVersions map[string]string
			fixture.trace.arm(fixture.itemID, func() error {
				var err error
				if scenario == "concurrent_repair" {
					_, err = fixture.pool.Exec(fixture.ctx, "UPDATE item_images SET file_identity=$2 WHERE item_id=$1", fixture.itemID, identity)
				} else {
					_, err = fixture.pool.Exec(fixture.ctx, "UPDATE item_images SET source_hash=repeat('f',64) WHERE item_id=$1", fixture.itemID)
				}
				if err != nil {
					return err
				}
				if err := fixture.pool.QueryRow(fixture.ctx, "SELECT to_jsonb(im)::text FROM item_images im WHERE item_id=$1", fixture.itemID).Scan(&laterRow); err != nil {
					return err
				}
				var version string
				if err := fixture.pool.QueryRow(fixture.ctx, "SELECT xmin::text FROM item_images WHERE item_id=$1", fixture.itemID).Scan(&version); err != nil {
					return err
				}
				laterVersions = map[string]string{"Primary:0": version}
				if scenario == "failed_commit" {
					_, err = fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_readonly_fallback_commit() RETURNS trigger LANGUAGE plpgsql AS $$
						BEGIN RAISE EXCEPTION 'reject image fallback commit'; RETURN NEW; END; $$;
						CREATE CONSTRAINT TRIGGER reject_readonly_fallback_commit AFTER INSERT OR UPDATE ON item_images
						DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_readonly_fallback_commit()`)
				}
				return err
			})
			err := fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false)
			reads, traceErr := fixture.trace.snapshot()
			if traceErr != nil || reads != 1 || fixture.trace.authority.ownedBegins.Load() != 1 || fixture.state.warnings != 0 {
				t.Fatalf("image mismatch did not enter its ordinary locked writer: reads=%d begins=%d warnings=%d trace_error=%v",
					reads, fixture.trace.authority.ownedBegins.Load(), fixture.state.warnings, traceErr)
			}
			if scenario == "failed_commit" {
				if err == nil || imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary") != laterRow ||
					!reflect.DeepEqual(scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID), laterVersions) {
					t.Fatalf("failed fallback committed a partial image repair: error=%v", err)
				}
				assertNoCatalogTestNotification(t, fixture.notifications)
				return
			}
			if err != nil || imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary") != accepted {
				t.Fatalf("fallback writer failed to restore its accepted source: %v", err)
			}
			if scenario == "concurrent_repair" {
				if fixture.trace.authority.imageWrites.Load() != 0 || !reflect.DeepEqual(scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID), laterVersions) {
					t.Fatal("fallback writer reused the stale mismatch after a concurrent repair")
				}
			} else {
				if fixture.trace.authority.imageWrites.Load() != 2 {
					t.Fatal("fallback writer skipped the remaining real image repair")
				}
				assertCatalogTestChanges(t, nextCatalogTestNotification(t, fixture.notifications), []CatalogChange{{
					Kind: CatalogUpdated, ItemID: fixture.itemID, LibraryID: fixture.state.library.ID, ParentID: fixture.state.library.ID,
				}})
			}
			assertNoCatalogTestNotification(t, fixture.notifications)
		})
	}
}

func TestScanImageReadonlyKnownAbsenceHintKeepsOriginalWriter(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "new_image"
		if populated {
			name = "stale_absence_hint"
		}
		t.Run(name, func(t *testing.T) {
			fixture := imageScanReadonlyPrepare(t, populated)
			if !populated {
				imageScanTestWrite(t, fixture.poster, color.White)
			}
			if err := fixture.state.scanImagesWithKnownAbsence(fixture.itemID, "Movie", "Film.mp4", false, true); err != nil || fixture.state.warnings != 0 {
				t.Fatalf("known-absence candidate failed its ordinary writer: warnings=%d error=%v", fixture.state.warnings, err)
			}
			reads, err := fixture.trace.snapshot()
			if err != nil || reads != 0 || fixture.trace.authority.ownedBegins.Load() != 1 || fixture.trace.authority.ownedCommits.Load() != 1 {
				t.Fatalf("known absence added a precheck or authorized a no-op: reads=%d begins=%d commits=%d error=%v",
					reads, fixture.trace.authority.ownedBegins.Load(), fixture.trace.authority.ownedCommits.Load(), err)
			}
			if !populated {
				assertCatalogTestChanges(t, nextCatalogTestNotification(t, fixture.notifications), []CatalogChange{{
					Kind: CatalogUpdated, ItemID: fixture.itemID, LibraryID: fixture.state.library.ID, ParentID: fixture.state.library.ID,
				}})
			}
			assertNoCatalogTestNotification(t, fixture.notifications)
		})
	}
}
