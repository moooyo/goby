//go:build linux

package library

import (
	"context"
	"errors"
	"image/color"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type primarySidecarQueuedAuthorityTrace struct {
	authority    scanPerformanceSQLTracer
	startup      scanOperationAuthoritySQLTracer
	owner        atomic.Pointer[pgx.Conn]
	begins       atomic.Int64
	ownedBegins  atomic.Int64
	ownedCommits atomic.Int64
	imageWrites  atomic.Int64
}

func (trace *primarySidecarQueuedAuthorityTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.authority.TraceQueryStart(ctx, conn, data)
	ctx = trace.startup.TraceQueryStart(ctx, conn, data)
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if statement == "begin" {
		trace.begins.Add(1)
		if conn == trace.owner.Load() {
			trace.ownedBegins.Add(1)
		}
	}
	if conn == trace.owner.Load() && statement == "commit" {
		trace.ownedCommits.Add(1)
	}
	if strings.HasPrefix(statement, "delete from item_images") || strings.HasPrefix(statement, "insert into item_images") {
		trace.imageWrites.Add(1)
	}
	return ctx
}

func (trace *primarySidecarQueuedAuthorityTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.authority.TraceQueryEnd(ctx, conn, data)
	trace.startup.TraceQueryEnd(ctx, conn, data)
}

func (trace *primarySidecarQueuedAuthorityTrace) reset() {
	trace.authority.reset()
	trace.startup.reset()
	trace.begins.Store(0)
	trace.ownedBegins.Store(0)
	trace.ownedCommits.Store(0)
	trace.imageWrites.Store(0)
}

func TestPrimarySidecarQueuedWalkRetainsOperationAuthority(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		statement string
		job       bool
		want      error
	}{
		{"persisted_task_cancel", "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", true, context.Canceled},
		{"binding_revision", "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", false, nil},
		{"binding_document", `UPDATE library_roots SET storage_binding=jsonb_set(storage_binding, '{registered_root,handle}', '"c3dhcHBlZC1oYW5kbGU="'::jsonb) WHERE id=$1`, false, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prober := &libraryFixtureProber{}
			ctx, pool, fixtureStore, approved, userID := libraryIntegrationStore(t, prober)
			mediaPath := libraryIntegrationFile(t, approved, "movies/Film.mp4", "video:queued-sidecar-authority")
			poster := filepath.Join(filepath.Dir(mediaPath), "Film-poster.png")
			imageScanTestWrite(t, poster, color.NRGBA{R: 230, A: 255})
			library := libraryIntegrationCreate(t, ctx, fixtureStore, "Queued sidecar authority", "movies", filepath.Dir(mediaPath))
			libraryIntegrationScan(t, ctx, fixtureStore, library.ID, "Completed")
			item := nfoCatalogItem(t, ctx, fixtureStore, userID, library.ID, mediaPath)
			before := imageScanTestList(t, ctx, fixtureStore, userID, item.ID, "Primary")
			if len(before) != 1 {
				t.Fatalf("queued authority fixture needs one stored image: %+v", before)
			}
			if err := fixtureStore.Close(ctx); err != nil {
				t.Fatalf("release fixture catalog ownership: %v", err)
			}
			trace := &primarySidecarQueuedAuthorityTrace{}
			configuration := pool.Config()
			configuration.ConnConfig.Tracer = trace
			tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatalf("create traced sidecar authority pool: %v", err)
			}
			libraryIntegrationPoolCleanup(t, tracedPool)
			store, err := New(tracedPool, prober, []string{approved})
			if err != nil {
				t.Fatalf("reopen traced sidecar Store: %v", err)
			}
			trace.owner.Store(store.ownership.conn.Conn())
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := store.Close(cleanup); err != nil {
					t.Errorf("close sidecar authority Store: %v", err)
				}
			})
			state := imageScanTestState(t, ctx, tracedPool, store, library, ".")
			notifications := catalogChangesTestListener(t, store)
			baselineIO := originalMediaReadGovernor.Stats()
			baselineOwners := originalMediaReadOwners.Stats().RegisteredOwners
			if baselineIO.Active != 0 || baselineIO.Background != 0 || baselineIO.Queued != 0 {
				t.Fatalf("queued sidecar fixture needs idle common admission: %+v", baselineIO)
			}
			row, err := state.readPrimaryScanAuthority(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.name == "binding_document" && (!row.stored || row.document == nil) {
				t.Fatal("queued document fixture needs a stored root binding")
			}
			if trace.startup.startups.Load() != 1 {
				t.Fatalf("sidecar fixture did not establish one operation grant: startups=%d", trace.startup.startups.Load())
			}
			walkIO, err := store.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
			if err != nil {
				t.Fatal(err)
			}
			state.walkIO, state.walkRow = walkIO, row
			t.Cleanup(func() {
				if err := walkIO.Close(); err != nil {
					t.Errorf("close committed walk routing: %v", err)
				}
			})
			prepared, preparedRow, err := state.prepareSidecarScanIO()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := prepared.Close(); err != nil {
					t.Errorf("close prepared sidecar routing fork: %v", err)
				}
			})
			if prepared.handle.state != walkIO.handle.state || !preparedRow.same(row) {
				t.Fatal("sidecar preparation did not borrow the exact committed walk routing")
			}
			if err := prepared.Close(); err != nil {
				t.Fatal(err)
			}

			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			blockers := make([]*primarySidecarRetryBlocker, 0, 2)
			for range 2 {
				operation, err := store.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
				if err != nil {
					t.Fatal(err)
				}
				blocker := &primarySidecarRetryBlocker{operation: operation, rootID: row.root.id,
					entered: make(chan struct{}), done: make(chan struct{})}
				blockers = append(blockers, blocker)
				t.Cleanup(func() {
					unblock()
					if blocker.started.Load() {
						mediaSourceAdmissionTestWait(t, blocker.done, "queued sidecar BG blocker cleanup")
					}
					if err := blocker.operation.Close(); err != nil {
						t.Errorf("close queued sidecar BG blocker: %v", err)
					}
				})
				blocker.start(ctx, release)
				select {
				case <-blocker.entered:
				case <-blocker.done:
					t.Fatalf("queued sidecar BG blocker did not acquire its actual lease: %v", blocker.err)
				case <-time.After(5 * time.Second):
					t.Fatal("queued sidecar BG blocker did not enter its actual lease")
				}
			}
			trace.reset()
			scanDone := make(chan struct{})
			var scanErr error
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, scanDone, "queued sidecar scan cleanup")
			})
			go func() {
				scanErr = state.scanImages(item.ID, item.Type, "Film.mp4", false)
				close(scanDone)
			}()
			wait, cancelWait := context.WithTimeout(ctx, 5*time.Second)
			defer cancelWait()
			primaryReadTestWaitQueued(t, wait, baselineIO.Queued+1)
			stats := originalMediaReadGovernor.Stats()
			if stats.Active != baselineIO.Active+2 || stats.Background != baselineIO.Background+2 ||
				originalMediaReadOwners.Stats().RegisteredOwners != baselineOwners+3 || trace.begins.Load() != 0 || trace.authority.authorityImplicitAttempts.Load() != 0 {
				t.Fatalf("queued sidecar repeated preparation authority or changed actual admission: IO=%+v owners=%+v begins=%d implicit_attempts=%d",
					stats, originalMediaReadOwners.Stats(), trace.begins.Load(), trace.authority.authorityImplicitAttempts.Load())
			}
			if count := mediaSourceWarmPipelineTestFileCount(t, poster); count != 0 {
				t.Fatalf("queued sidecar opened %d payload descriptors before its grant", count)
			}
			select {
			case <-scanDone:
				t.Fatalf("sidecar completed before the actual BG grant: %v", scanErr)
			default:
			}
			id := state.root.id
			if scenario.job {
				id = state.task.job.ID
			}
			changed, err := pool.Exec(ctx, scenario.statement, id)
			if err != nil {
				t.Fatal(err)
			}
			if changed.RowsAffected() != 1 {
				t.Fatalf("queued authority mutation affected %d rows, want one", changed.RowsAffected())
			}
			if scenario.job {
				state.task.cancel()
			}
			unblock()
			mediaSourceAdmissionTestWait(t, scanDone, "queued sidecar completion after actual grant")
			if !errors.Is(scanErr, scenario.want) {
				t.Fatalf("queued sidecar did not retain operation authority: got=%v want=%v", scanErr, scenario.want)
			}
			primaryScanRoutingAssertNoRepeatedAuthority(t, &trace.authority)
			if trace.startup.startups.Load() != 0 {
				t.Fatalf("queued sidecar repeated startup authorization: startups=%d", trace.startup.startups.Load())
			}
			if trace.begins.Load() != 0 || trace.ownedBegins.Load() != 0 || trace.ownedCommits.Load() != 0 || trace.imageWrites.Load() != 0 ||
				(state.imageDirectories != nil) != (scenario.want == nil) || state.warnings != 0 {
				t.Fatalf("sidecar operation changed its publication boundary: begins=%d owned_begin=%d owned_commit=%d writes=%d directories=%v warnings=%d",
					trace.begins.Load(), trace.ownedBegins.Load(), trace.ownedCommits.Load(), trace.imageWrites.Load(), state.imageDirectories, state.warnings)
			}
			if current := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); !reflect.DeepEqual(current, before) {
				t.Fatalf("queued operation changed image values: before=%+v after=%+v", before, current)
			}
			assertNoCatalogTestNotification(t, notifications)
			if count := mediaSourceWarmPipelineTestFileCount(t, poster); count != 0 {
				t.Fatalf("sidecar retained %d actual payload descriptors", count)
			}
			for _, blocker := range blockers {
				mediaSourceAdmissionTestWait(t, blocker.done, "queued sidecar BG blocker retirement")
				if blocker.err != nil {
					t.Fatalf("queued sidecar BG blocker failed: %v", blocker.err)
				}
				if err := blocker.operation.Close(); err != nil {
					t.Fatal(err)
				}
			}
			walkIO.handle.state.mu.Lock()
			active, phase, handles := walkIO.handle.state.active, walkIO.handle.state.phase, walkIO.handle.state.handles
			walkIO.handle.state.mu.Unlock()
			if active != 0 || phase != nil || handles != 1 {
				t.Fatalf("sidecar retained its actual phase or completion fork: active=%d phase=%v handles=%d", active, phase, handles)
			}
			if err := walkIO.Close(); err != nil {
				t.Fatal(err)
			}
			drain, cancelDrain := context.WithTimeout(ctx, 5*time.Second)
			defer cancelDrain()
			primaryReadTestWaitOwners(t, drain, baselineOwners)
			if stats := originalMediaReadGovernor.Stats(); stats.Active != baselineIO.Active || stats.Background != baselineIO.Background || stats.Queued != baselineIO.Queued {
				t.Fatalf("queued sidecar phases did not drain: before=%+v after=%+v", baselineIO, stats)
			}
		})
	}
}
