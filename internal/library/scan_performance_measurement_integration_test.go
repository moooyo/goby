//go:build linux

package library

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These observations are intentionally process-wide and pool-wide. They include
// the terminal observer, Go background work, and tracer when enabled.
// Pool counters include observer acquires and exclude waits on ownership.mu or
// operations on the already reserved owner connection. They are not per-item
// allocation measurements or a complete measure of catalog contention.
type scanPerformanceMeasurement struct {
	memory runtime.MemStats
	pool   *pgxpool.Stat
}

type scanPerformanceObservation struct {
	MeasurementVersion           int                            `json:"measurement_version"`
	Profile                      string                         `json:"profile"`
	Phase                        string                         `json:"phase"`
	Library                      string                         `json:"library"`
	ForceProbe                   bool                           `json:"force_probe"`
	TaskOwned                    bool                           `json:"task_owned"`
	JobElapsedNS                 int64                          `json:"job_elapsed_ns"`
	ObservedTerminalElapsedNS    int64                          `json:"observed_terminal_elapsed_ns"`
	WorkerRetirementWaitNS       int64                          `json:"worker_retirement_wait_ns"`
	ProbeCalls                   int64                          `json:"probe_calls"`
	RawSQL                       *int64                         `json:"raw_sql"`
	RawBegin                     *int64                         `json:"raw_begin"`
	RawCommit                    *int64                         `json:"raw_commit"`
	RawRollback                  *int64                         `json:"raw_rollback"`
	AttributedAuthoritySQL       *int64                         `json:"attributed_authority_sql"`
	AttributedAuthorityBegin     *int64                         `json:"attributed_authority_begin"`
	AttributedAuthorityCommit    *int64                         `json:"attributed_authority_commit"`
	AttributedAuthorityRollback  *int64                         `json:"attributed_authority_rollback"`
	AuthorityManualTransactions  *int64                         `json:"authority_manual_transactions"`
	AuthorityTaskTransactions    *int64                         `json:"authority_task_transactions"`
	AuthorityImplicitAttempts    *int64                         `json:"authority_implicit_attempts"`
	AuthorityImplicitCommits     *int64                         `json:"authority_implicit_commits"`
	AuthorityImplicitSingleRows  *int64                         `json:"authority_implicit_single_rows"`
	AuthorityImplicitEmpty       *int64                         `json:"authority_implicit_empty"`
	AuthorityImplicitErrors      *int64                         `json:"authority_implicit_errors"`
	AuthorityImplicitUnexpected  *int64                         `json:"authority_implicit_unexpected_rows"`
	AuthorityImplicitUnconfirmed *int64                         `json:"authority_implicit_unconfirmed"`
	SQLTraceEnabled              bool                           `json:"sql_trace_enabled"`
	SQLCounterScope              string                         `json:"sql_counter_scope"`
	SQLTiming                    bool                           `json:"sql_timing"`
	AllocatedBytes               uint64                         `json:"process_allocated_bytes"`
	Mallocs                      uint64                         `json:"process_mallocs"`
	Frees                        uint64                         `json:"process_frees"`
	GCCount                      uint32                         `json:"process_gc_count"`
	GCPauseNS                    uint64                         `json:"process_gc_pause_ns"`
	HeapAllocBefore              uint64                         `json:"process_heap_alloc_before"`
	HeapAllocAfter               uint64                         `json:"process_heap_alloc_after"`
	PoolAcquireCount             int64                          `json:"pool_acquire_count"`
	PoolAcquireDurationNS        int64                          `json:"pool_acquire_duration_ns"`
	PoolEmptyAcquireCount        int64                          `json:"pool_empty_acquire_count"`
	PoolEmptyAcquireWaitNS       int64                          `json:"pool_empty_acquire_wait_ns"`
	PoolCanceledAcquireCount     int64                          `json:"pool_canceled_acquire_count"`
	PoolNewConnections           int64                          `json:"pool_new_connections"`
	PoolMaxConnections           int32                          `json:"pool_max_connections"`
	PoolAcquiredBefore           int32                          `json:"pool_acquired_before"`
	PoolAcquiredAfter            int32                          `json:"pool_acquired_after"`
	PoolTotalBefore              int32                          `json:"pool_total_before"`
	PoolTotalAfter               int32                          `json:"pool_total_after"`
	AllocationScope              string                         `json:"allocation_scope"`
	PoolScope                    string                         `json:"pool_scope"`
	RetirementScope              string                         `json:"retirement_scope"`
	ProbeResources               *scanPerformanceProbeResources `json:"probe_resources,omitempty"`
}

type scanPerformanceProbeResources struct {
	PeakProbeCohorts int64  `json:"peak_probe_cohorts"`
	ChildCPUNS       int64  `json:"child_cpu_ns"`
	ChildInBlocks    int64  `json:"child_inblock"`
	ChildOutBlocks   int64  `json:"child_outblock"`
	CohortScope      string `json:"cohort_scope"`
	ChildScope       string `json:"child_scope"`
}

func scanPerformanceBeginMeasurement(pool *pgxpool.Pool) scanPerformanceMeasurement {
	measurement := scanPerformanceMeasurement{pool: pool.Stat()}
	runtime.ReadMemStats(&measurement.memory)
	return measurement
}

func scanPerformanceEndMeasurement(before scanPerformanceMeasurement, pool *pgxpool.Pool) scanPerformanceObservation {
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	stats := pool.Stat()
	return scanPerformanceObservation{
		MeasurementVersion:       2,
		AllocatedBytes:           after.TotalAlloc - before.memory.TotalAlloc,
		Mallocs:                  after.Mallocs - before.memory.Mallocs,
		Frees:                    after.Frees - before.memory.Frees,
		GCCount:                  after.NumGC - before.memory.NumGC,
		GCPauseNS:                after.PauseTotalNs - before.memory.PauseTotalNs,
		HeapAllocBefore:          before.memory.HeapAlloc,
		HeapAllocAfter:           after.HeapAlloc,
		PoolAcquireCount:         stats.AcquireCount() - before.pool.AcquireCount(),
		PoolAcquireDurationNS:    int64(stats.AcquireDuration() - before.pool.AcquireDuration()),
		PoolEmptyAcquireCount:    stats.EmptyAcquireCount() - before.pool.EmptyAcquireCount(),
		PoolEmptyAcquireWaitNS:   int64(stats.EmptyAcquireWaitTime() - before.pool.EmptyAcquireWaitTime()),
		PoolCanceledAcquireCount: stats.CanceledAcquireCount() - before.pool.CanceledAcquireCount(),
		PoolNewConnections:       stats.NewConnsCount() - before.pool.NewConnsCount(),
		PoolMaxConnections:       stats.MaxConns(),
		PoolAcquiredBefore:       before.pool.AcquiredConns(),
		PoolAcquiredAfter:        stats.AcquiredConns(),
		PoolTotalBefore:          before.pool.TotalConns(),
		PoolTotalAfter:           stats.TotalConns(),
		AllocationScope:          "process delta through observed terminal state and scan worker retirement; includes observer, Go background work and tracer when enabled",
		PoolScope:                "data pool delta including observer acquires; empty wait includes resource construction; excludes reserved owner and ownership mutex waits",
		RetirementScope:          "same scan task removed from Store.active; does not establish unrelated physical callback or native process retirement",
	}
}

// The persisted terminal snapshot precedes the worker's final cancellation,
// active-map removal and notification. Wait for the same task to leave the
// active map before sampling its transaction and task-cancellation tail. The
// final nonblocking scan-update notification can still follow this observation.
// This test-only observation is shared unchanged with the accepted baseline.
func scanPerformanceWaitWorkerRetired(t *testing.T, ctx context.Context, store *Store, jobID string) time.Duration {
	t.Helper()
	started := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		store.mu.Lock()
		_, active := store.active[jobID]
		store.mu.Unlock()
		if !active {
			return time.Since(started)
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("scan worker did not retire task %s after its terminal snapshot: %v", jobID, waitCtx.Err())
		}
	}
}

func (observation scanPerformanceObservation) log(t *testing.T, profile, phase, library string, job Job, elapsed, workerRetirementWait time.Duration, probes int64, trace *scanPerformanceSQLTracer) {
	t.Helper()
	observation.Profile = profile
	observation.Phase, observation.Library = phase, library
	observation.ForceProbe, observation.TaskOwned = job.ForceProbe, job.TaskChildID != ""
	observation.JobElapsedNS = int64(job.FinishedAt.Sub(*job.StartedAt))
	observation.ObservedTerminalElapsedNS = int64(elapsed)
	observation.WorkerRetirementWaitNS, observation.ProbeCalls = int64(workerRetirementWait), probes
	observation.SQLTraceEnabled = !trace.disabled
	observation.SQLCounterScope = "not collected; the pgx SQL tracer was not installed"
	if !trace.disabled {
		observation.RawSQL, observation.RawBegin = scanPerformanceCounter(trace.queries.Load()), scanPerformanceCounter(trace.begins.Load())
		observation.RawCommit, observation.RawRollback = scanPerformanceCounter(trace.commits.Load()), scanPerformanceCounter(trace.rollbacks.Load())
		observation.AttributedAuthoritySQL = scanPerformanceCounter(trace.authorityQueries.Load())
		observation.AttributedAuthorityBegin = scanPerformanceCounter(trace.authorityBegins.Load())
		observation.AttributedAuthorityCommit = scanPerformanceCounter(trace.authorityCommits.Load())
		observation.AttributedAuthorityRollback = scanPerformanceCounter(trace.authorityRollbacks.Load())
		observation.AuthorityManualTransactions = scanPerformanceCounter(trace.authorityManual.Load())
		observation.AuthorityTaskTransactions = scanPerformanceCounter(trace.authorityTask.Load())
		observation.AuthorityImplicitAttempts = scanPerformanceCounter(trace.authorityImplicitAttempts.Load())
		observation.AuthorityImplicitCommits = scanPerformanceCounter(trace.authorityImplicitCommits.Load())
		observation.AuthorityImplicitSingleRows = scanPerformanceCounter(trace.authorityImplicitSingleRows.Load())
		observation.AuthorityImplicitEmpty = scanPerformanceCounter(trace.authorityImplicitEmpty.Load())
		observation.AuthorityImplicitErrors = scanPerformanceCounter(trace.authorityImplicitErrors.Load())
		observation.AuthorityImplicitUnexpected = scanPerformanceCounter(trace.authorityImplicitUnexpectedRows.Load())
		observation.AuthorityImplicitUnconfirmed = scanPerformanceCounter(trace.authorityImplicitUnconfirmed.Load())
		observation.SQLCounterScope = "client commands excluding observer queries; AUTH SQL is a subset of raw SQL; BEGIN/COMMIT count explicit commands only; implicit commits include empty results; manual/task transactions require a single-row authority result"
	}
	observation.SQLTiming = trace.timingEnabled
	data, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("encode scan performance observation: %v", err)
	}
	t.Logf("scan_performance_observation=%s", data)
}

func scanPerformanceCounter(value int64) *int64 {
	return &value
}

// Close is outside every workload interval and must finish before a successful
// profile returns. Its receipt reports the common Store contract only; no native
// process or arbitrary callback retirement certificate is inferred from it.
func scanPerformanceCloseMeasurementStore(t *testing.T, store *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("join scan profile store cleanup: %v", err)
	}
	t.Logf("scan_performance_store_close elapsed_ns=%d result=joined scope=store_close_contract", time.Since(started).Nanoseconds())
}
