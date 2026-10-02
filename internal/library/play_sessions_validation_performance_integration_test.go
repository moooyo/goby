package library

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type playbackValidationPerformanceContextKey struct{}
type playbackValidationPerformanceLockKey struct{}

type playbackValidationPerformanceTrace struct {
	delay                        time.Duration
	queries, active, peak, locks atomic.Int64
}

func (trace *playbackValidationPerformanceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if enabled, _ := ctx.Value(playbackValidationPerformanceContextKey{}).(bool); !enabled {
		return ctx
	}
	trace.queries.Add(1)
	statement := strings.TrimSpace(data.SQL)
	if strings.HasPrefix(statement, "SELECT ") && strings.Contains(statement, " FROM play_sessions") &&
		(strings.HasSuffix(statement, " FOR UPDATE") || strings.HasSuffix(statement, " FOR SHARE")) {
		return context.WithValue(ctx, playbackValidationPerformanceLockKey{}, true)
	}
	return ctx
}

func (trace *playbackValidationPerformanceTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if locked, _ := ctx.Value(playbackValidationPerformanceLockKey{}).(bool); !locked || data.Err != nil || data.CommandTag.RowsAffected() == 0 {
		return
	}
	trace.locks.Add(1)
	active := trace.active.Add(1)
	defer trace.active.Add(-1)
	for previous := trace.peak.Load(); active > previous; previous = trace.peak.Load() {
		if trace.peak.CompareAndSwap(previous, active) {
			break
		}
	}
	if trace.delay > 0 {
		timer := time.NewTimer(trace.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
}

// This opt-in profile calls the complete public validation API on the same play
// with one or eight callers. It can be copied unchanged to the earlier checkout
// for paired measurements. The 10 ms tracing delay holds the final database row
// lock to expose contention reproducibly; it is not real production latency.
// The zero-delay case measures the ordinary API. Neither timing is a threshold.
func TestPlaybackValidationPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_PLAYBACK_VALIDATION_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_PLAYBACK_VALIDATION_PERFORMANCE=1 enables the playback validation performance profile")
	}
	ctx, fixturePool, store, normal, ids := playSessionFixture(t, 4)
	trace := &playbackValidationPerformanceTrace{}
	configuration := fixturePool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create traced playback validation pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store.pool = tracedPool
	t.Cleanup(func() { store.pool = fixturePool })
	// Establish the full caller pool before collecting timings, so connection
	// creation is excluded from the paired steady-state measurements.
	connections := make([]*pgxpool.Conn, 0, 8)
	for index := 0; index < cap(connections); index++ {
		connection, err := tracedPool.Acquire(ctx)
		if err != nil {
			for _, held := range connections {
				held.Release()
			}
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	for _, connection := range connections {
		connection.Release()
	}
	for ownerIndex, owner := range []PlaybackOwner{normal, applicationPlaybackOwnerFixture(t, ctx, fixturePool, "validation-performance")} {
		for selectorIndex, selector := range []string{"canonical", "client reference"} {
			itemID := ids[ownerIndex*2+selectorIndex]
			var prepared PlaySession
			var reference string
			if selector == "client reference" {
				reference = "validation-performance-client-reference"
				prepared = playReferencePrepare(t, ctx, store, owner, itemID, reference)
			} else {
				prepared = playSessionPrepare(t, ctx, store, owner, itemID, "")
				reference = prepared.ID
			}
			trace.delay = 0
			warmupStart := make(chan struct{})
			warmupResults := make(chan error, 8)
			for index := 0; index < cap(warmupResults); index++ {
				go func() {
					<-warmupStart
					session, err := store.GetPlaybackSession(ctx, owner, reference)
					if err == nil && session.ID != prepared.ID {
						err = fmt.Errorf("warmup resolved unexpected playback %s", session.ID)
					}
					warmupResults <- err
				}()
			}
			close(warmupStart)
			for index := 0; index < cap(warmupResults); index++ {
				if err := <-warmupResults; err != nil {
					t.Fatalf("warm the playback API statement cache: %v", err)
				}
			}
			for _, delay := range []time.Duration{0, 10 * time.Millisecond} {
				for _, callers := range []int{1, 8} {
					trace.delay = delay
					trace.queries.Store(0)
					trace.locks.Store(0)
					trace.peak.Store(0)
					callCtx := context.WithValue(ctx, playbackValidationPerformanceContextKey{}, true)
					rounds := 12
					if delay == 0 {
						rounds = 96
					}
					start := make(chan struct{})
					failures := make(chan error, callers)
					var workers sync.WaitGroup
					for index := 0; index < callers; index++ {
						workers.Add(1)
						go func() {
							defer workers.Done()
							<-start
							for round := 0; round < rounds; round++ {
								session, err := store.GetPlaybackSession(callCtx, owner, reference)
								if err != nil || session.ID != prepared.ID || session.State != "Prepared" {
									failures <- fmt.Errorf("session=%s state=%s error=%v", session.ID, session.State, err)
									return
								}
							}
						}()
					}
					started := time.Now()
					close(start)
					workers.Wait()
					elapsed := time.Since(started)
					close(failures)
					for err := range failures {
						t.Errorf("profile validation failed: %v", err)
					}
					if t.Failed() {
						t.FailNow()
					}
					operations := callers * rounds
					if trace.locks.Load() != int64(operations) {
						t.Fatalf("validation profile did not observe one final row lock per request: locks=%d operations=%d", trace.locks.Load(), operations)
					}
					t.Logf("playback_validation_performance application_key=%t selector=%q callers=%d operations=%d held_delay=%s elapsed=%s ns_per_operation=%d sql=%d max_locked=%d",
						owner.ApplicationKey, selector, callers, operations, delay, elapsed, elapsed.Nanoseconds()/int64(operations), trace.queries.Load(), trace.peak.Load())
				}
			}
		}
	}
}
