//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auxiliaryPublicationRetryTrace struct {
	mu                sync.Mutex
	table             string
	expectedOwner     *pgx.Conn
	rolePending       bool
	injected          bool
	rolledBack        bool
	roleWrites        int
	publicationWrites int
	err               error
	inject            func() error
	capacity          chan struct{}
	rollback          chan struct{}
}

type auxiliaryPublicationRetryTraceKey struct{}

type auxiliaryPublicationRetryTraceEvent struct {
	trace    *auxiliaryPublicationRetryTrace
	inject   bool
	rollback bool
}

func (trace *auxiliaryPublicationRetryTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if conn != trace.expectedOwner {
		return ctx
	}
	if strings.HasPrefix(statement, "insert into "+trace.table+"(") {
		trace.rolePending = true
		trace.roleWrites++
		if conn.PgConn().TxStatus() != 'T' {
			trace.err = errors.Join(trace.err, errors.New("auxiliary publication did not own an active transaction"))
		}
	}
	if trace.rolePending && strings.HasPrefix(statement, "update scan_jobs set scanned =") {
		trace.rolePending = false
		trace.publicationWrites++
		if !trace.injected {
			trace.injected = true
			return context.WithValue(ctx, auxiliaryPublicationRetryTraceKey{}, auxiliaryPublicationRetryTraceEvent{trace: trace, inject: true})
		}
	}
	if statement == "rollback" && trace.injected && !trace.rolledBack {
		return context.WithValue(ctx, auxiliaryPublicationRetryTraceKey{}, auxiliaryPublicationRetryTraceEvent{trace: trace, rollback: true})
	}
	return ctx
}

func (trace *auxiliaryPublicationRetryTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	event, ok := ctx.Value(auxiliaryPublicationRetryTraceKey{}).(auxiliaryPublicationRetryTraceEvent)
	if !ok || event.trace != trace {
		return
	}
	err := data.Err
	if event.inject && err == nil {
		// Only memory admission starts here. The progress statement has finished,
		// but its owned transaction has not reached the final filesystem proof.
		err = trace.inject()
	}
	trace.mu.Lock()
	trace.err = errors.Join(trace.err, err)
	if event.rollback && err == nil && !trace.rolledBack {
		trace.rolledBack = true
		trace.rolePending = false
		close(trace.rollback)
	}
	trace.mu.Unlock()
	if event.inject {
		close(trace.capacity)
	}
}

func (trace *auxiliaryPublicationRetryTrace) snapshot() (int, int, error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.roleWrites, trace.publicationWrites, trace.err
}

func TestAuxiliaryPublicationAdmissionBusyRetriesAfterRollback(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		for _, changed := range []bool{false, true} {
			name := "same_source"
			if changed {
				name = "changed_source"
			}
			t.Run(role+"/"+name, func(t *testing.T) {
				prober := &libraryFixtureProber{}
				ctx, pool, initial, approved, userID := libraryIntegrationStoreWithTimeout(t, prober, 2*time.Minute)
				mainPath := libraryIntegrationFile(t, approved, "movies/Film/Main.mp4", "video:main")
				directory, extension, payload := "theme-music", "mp3", "audio:auxiliary"
				table := "item_theme_resources"
				resources := themeScanTestResources
				population := themeScanTestSnapshot
				if role == "extra" {
					directory, extension, payload = "featurettes", "mp4", "video:auxiliary"
					table, resources, population = "item_extra_resources", extraScanTestResources, extraScanTestSnapshot
				}
				var paths []string
				for index := range 2 {
					paths = append(paths, libraryIntegrationFile(t, approved,
						fmt.Sprintf("movies/Film/%s/%d.%s", directory, index, extension), payload))
				}
				library := libraryIntegrationCreate(t, ctx, initial, "Auxiliary publication admission retry", "movies", filepath.Join(approved, "movies"))
				if job := libraryIntegrationScan(t, ctx, initial, library.ID, "Completed"); job.Error != "" {
					t.Fatalf("prepare complete auxiliary population: %+v", job)
				}
				beforeResources := resources(t, ctx, pool, library.ID)
				before := population(t, ctx, pool, library.ID)
				if len(beforeResources) != len(paths) {
					t.Fatalf("fixture has %d resources, want %d", len(beforeResources), len(paths))
				}
				main := nfoCatalogItem(t, ctx, initial, userID, library.ID, mainPath)
				if err := initial.Close(ctx); err != nil {
					t.Fatalf("release initial catalog owner: %v", err)
				}
				trace := &auxiliaryPublicationRetryTrace{table: table, capacity: make(chan struct{}), rollback: make(chan struct{})}
				configuration := pool.Config()
				configuration.ConnConfig.Tracer = trace
				tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
				if err != nil {
					t.Fatal(err)
				}
				libraryIntegrationPoolCleanup(t, tracedPool)
				store, err := New(tracedPool, prober, []string{approved})
				if err != nil {
					t.Fatal(err)
				}
				trace.mu.Lock()
				trace.expectedOwner = store.ownership.conn.Conn()
				trace.mu.Unlock()
				release := make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				var blockers []*primarySidecarRetryBlocker
				t.Cleanup(func() {
					unblock()
					for _, blocker := range blockers {
						if blocker.started.Load() {
							select {
							case <-blocker.done:
								if blocker.err != nil && !errors.Is(blocker.err, context.Canceled) {
									t.Errorf("background admission blocker: %v", blocker.err)
								}
							case <-time.After(5 * time.Second):
								t.Error("background admission blocker did not retire")
							}
						}
						if err := blocker.operation.Close(); err != nil {
							t.Errorf("close background blocker owner: %v", err)
						}
					}
					cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					if err := store.Close(cleanup); err != nil {
						t.Errorf("close traced auxiliary store: %v", err)
					}
				})
				hint, err := store.readMediaSourceRootHint(ctx, main.ID)
				if err != nil {
					t.Fatal(err)
				}
				baseline := originalMediaReadGovernor.Stats()
				if baseline.Active != 0 || baseline.Background != 0 || baseline.Queued != 0 {
					t.Fatalf("fixture requires idle actual I/O admission: %+v", baseline)
				}
				for range 2 {
					operation, err := store.preparePrimaryRootIO(ctx, []mediaSourceRootHint{hint})
					if err != nil {
						t.Fatal(err)
					}
					blockers = append(blockers, &primarySidecarRetryBlocker{operation: operation,
						rootID: hint.root.id, entered: make(chan struct{}), done: make(chan struct{})})
				}
				trace.inject = func() error {
					for _, blocker := range blockers {
						blocker.start(ctx, release)
					}
					for _, blocker := range blockers {
						select {
						case <-blocker.entered:
						case <-blocker.done:
							return fmt.Errorf("occupy final publication capacity: %w", blocker.err)
						case <-time.After(5 * time.Second):
							return errors.New("background leases did not enter before final publication proof")
						}
					}
					return nil
				}
				job := themeScanTestForce(t, ctx, store, library.ID)
				select {
				case <-trace.capacity:
					if _, _, err := trace.snapshot(); err != nil {
						t.Fatalf("inject final publication contention: %v", err)
					}
				case <-time.After(15 * time.Second):
					current, err := store.GetJob(ctx, job.ID)
					t.Fatalf("scan did not reach its final publication progress: job=%+v error=%v", current, err)
				}
				select {
				case <-trace.rollback:
				case <-time.After(5 * time.Second):
					t.Fatal("capacity-only publication failure did not roll back")
				}
				wait, cancelWait := context.WithTimeout(ctx, 5*time.Second)
				defer cancelWait()
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for originalMediaReadGovernor.Stats().Queued != baseline.Queued+1 {
					current, err := store.GetJob(wait, job.ID)
					if err != nil || current.Status != "Running" {
						t.Fatalf("capacity-only publication ended instead of retrying: job=%+v error=%v", current, err)
					}
					select {
					case <-tick.C:
					case <-wait.Done():
						t.Fatal("publication retry did not queue for background admission outside SQL")
					}
				}
				if stats := originalMediaReadGovernor.Stats(); stats.Active != 2 || stats.Background != 2 {
					t.Fatalf("retry retained an actual phase beyond rollback: %+v", stats)
				}
				primarySidecarRetryAssertOwnedTransactionFree(t, ctx, store, unblock)
				if current := population(t, ctx, tracedPool, library.ID); current != before {
					t.Fatal("rolled-back capacity failure changed the accepted auxiliary population")
				}
				for _, path := range paths {
					if count := mediaSourceWarmPipelineTestFileCount(t, path); count != 0 {
						t.Fatalf("retired attempt retained %d source descriptors while awaiting admission", count)
					}
				}
				if changed {
					if err := os.WriteFile(paths[0], []byte(payload+":changed-during-capacity-wait"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				unblock()
				finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
				writes, publications, traceErr := trace.snapshot()
				if traceErr != nil {
					t.Fatal(traceErr)
				}
				if changed {
					if finished.Error == "" || publications != 1 || writes != len(paths) {
						t.Fatalf("changed source was republished after capacity waiting: job=%+v publications=%d writes=%d", finished, publications, writes)
					}
					if current := population(t, ctx, tracedPool, library.ID); current != before {
						t.Fatal("retry replaced the accepted population using changed source facts")
					}
					return
				}
				if finished.Status != "Completed" || finished.Error != "" || publications != 2 || writes != 2*len(paths) {
					t.Fatalf("fresh publication retry did not complete once: job=%+v publications=%d writes=%d", finished, publications, writes)
				}
				after := resources(t, ctx, tracedPool, library.ID)
				if len(after) != len(beforeResources) {
					t.Fatalf("retry changed complete group size: before=%d after=%d", len(beforeResources), len(after))
				}
				for index, resource := range after {
					if !resource.active || resource.id != beforeResources[index].id || resource.ownerID != beforeResources[index].ownerID {
						t.Fatalf("retry duplicated or lost an auxiliary role: %+v", resource)
					}
				}
			})
		}
	}
}
