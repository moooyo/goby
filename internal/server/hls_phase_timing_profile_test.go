//go:build linux

package server

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const hlsPhaseTimingEventLimit = 128
const hlsPhaseTimingSlowLimit = 16

type hlsPhaseTimingContextKey struct{}
type hlsPhaseTimingBatchContextKey struct{}
type hlsPhaseTimingAcquireContextKey struct{}

type hlsPhaseTimingEvent struct {
	Kind       string `json:"kind"`
	Category   string `json:"category"`
	StartNS    int64  `json:"start_offset_ns"`
	ElapsedNS  int64  `json:"elapsed_ns"`
	BackendPID uint32 `json:"backend_pid,omitempty"`
	PoolMax    int32  `json:"pool_max,omitempty"`
	HadError   bool   `json:"had_error,omitempty"`
}

type hlsPhaseTimingStatistic struct {
	Count     int64 `json:"count"`
	ElapsedNS int64 `json:"elapsed_ns"`
	MaximumNS int64 `json:"maximum_ns"`
	Errors    int64 `json:"errors"`
}

type hlsPhaseTimingRequestSummary struct {
	Ordinal       int64                              `json:"request_ordinal"`
	StartedAt     time.Time                          `json:"handler_started_at"`
	HandlerNS     int64                              `json:"handler_elapsed_ns"`
	DroppedEvents int64                              `json:"dropped_detail_events"`
	Statistics    map[string]hlsPhaseTimingStatistic `json:"statistics"`
	Events        []hlsPhaseTimingEvent              `json:"events"`
}

type hlsPhaseTimingRequest struct {
	mu      sync.Mutex
	started time.Time
	group   string
	closed  bool
	summary hlsPhaseTimingRequestSummary
}

type hlsPhaseTimingCase struct {
	mu         sync.Mutex
	requests   int64
	statistics map[string]hlsPhaseTimingStatistic
	slowest    []hlsPhaseTimingRequestSummary
}

type hlsPhaseTimingSpan struct {
	ended    sync.Once
	request  *hlsPhaseTimingRequest
	started  time.Time
	previous time.Time
	category string
	pid      uint32
	poolMax  int32
}

func hlsPhaseTimingEnabled() bool { return os.Getenv("GOBY_HLS_PHASE_TIMING") == "1" }

func hlsPhaseTimingFor(ctx context.Context) *hlsPhaseTimingRequest {
	facts, _ := ctx.Value(hlsPriorityProfileRequestContextKey{}).(*hlsPriorityProfileRequestFacts)
	if facts == nil {
		return nil
	}
	return facts.phaseTiming
}

func (request *hlsPhaseTimingRequest) record(kind, category string, started time.Time, pid uint32, poolMax int32, failed bool) {
	now := time.Now()
	event := hlsPhaseTimingEvent{Kind: kind, Category: category, StartNS: started.Sub(request.started).Nanoseconds(),
		ElapsedNS: now.Sub(started).Nanoseconds(), BackendPID: pid, PoolMax: poolMax, HadError: failed}
	key := kind + ":" + category
	request.mu.Lock()
	defer request.mu.Unlock()
	if request.closed {
		return
	}
	statistic := request.summary.Statistics[key]
	statistic.Count++
	statistic.ElapsedNS += event.ElapsedNS
	if event.ElapsedNS > statistic.MaximumNS {
		statistic.MaximumNS = event.ElapsedNS
	}
	if failed {
		statistic.Errors++
	}
	request.summary.Statistics[key] = statistic
	if len(request.summary.Events) < hlsPhaseTimingEventLimit {
		request.summary.Events = append(request.summary.Events, event)
	} else {
		request.summary.DroppedEvents++
	}
}

func (request *hlsPhaseTimingRequest) finish(group *hlsPhaseTimingCase) {
	request.mu.Lock()
	request.closed = true
	request.summary.HandlerNS = time.Since(request.started).Nanoseconds()
	summary := request.summary
	request.mu.Unlock()
	group.mu.Lock()
	defer group.mu.Unlock()
	group.requests++
	for key, value := range summary.Statistics {
		statistic := group.statistics[key]
		statistic.Count += value.Count
		statistic.ElapsedNS += value.ElapsedNS
		statistic.Errors += value.Errors
		if value.MaximumNS > statistic.MaximumNS {
			statistic.MaximumNS = value.MaximumNS
		}
		group.statistics[key] = statistic
	}
	if len(group.slowest) < hlsPhaseTimingSlowLimit {
		group.slowest = append(group.slowest, summary)
	} else if summary.HandlerNS > group.slowest[len(group.slowest)-1].HandlerNS {
		// Replacing the tail avoids retaining a seventeenth summary in an
		// otherwise truncated backing array.
		group.slowest[len(group.slowest)-1] = summary
	} else {
		return
	}
	sort.Slice(group.slowest, func(i, j int) bool { return group.slowest[i].HandlerNS > group.slowest[j].HandlerNS })
}

// Categories are a closed vocabulary. SQL text, arguments, credentials, item
// identities, filesystem paths and transport error messages are never retained.
func hlsPhaseTimingCategory(sql string) string {
	statement := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if strings.HasPrefix(statement, "begin") {
		return "transaction_begin"
	}
	if statement == "commit" || statement == "rollback" {
		return "transaction_" + statement
	}
	if strings.Contains(statement, "token_hash") {
		return "token_lookup"
	}
	for _, table := range []struct{ name, category string }{
		{"application_key_devices", "device"}, {"application_key_clients", "client"},
		{"application_keys", "key"}, {"sessions", "credential"},
		{"users", "account"}, {"items", "item"}, {"play_sessions", "play"},
	} {
		if strings.HasPrefix(statement, "update "+table.name+" ") {
			return table.category + "_write"
		}
		if strings.Contains(statement, "from "+table.name+" ") {
			if strings.Contains(statement, "for update") {
				return table.category + "_exclusive_lock"
			}
			if strings.Contains(statement, "for share") {
				return table.category + "_shared_lock"
			}
		}
	}
	if strings.Contains(statement, "from play_sessions p join sessions") {
		return "playback_context_restore"
	}
	if strings.Contains(statement, "from play_sessions") && strings.Contains(statement, "clock_timestamp()") {
		return "play_final_clock"
	}
	if strings.Contains(statement, "c.last_seen_at <= clock_timestamp()") {
		return "client_activity_predicates"
	}
	if strings.Contains(statement, "from application_key_clients c join sessions") {
		return "client_defaults"
	}
	if strings.Contains(statement, "select exists") && strings.Contains(statement, "from sessions") {
		return "credential_validation"
	}
	if strings.Contains(statement, "i.file_identity") && strings.Contains(statement, "from items i") {
		return "source_projection"
	}
	if strings.Contains(statement, "binding_revision") && strings.Contains(statement, "from items") {
		return "source_binding_projection"
	}
	if strings.Contains(statement, "from media_operations") || strings.Contains(statement, "publication_phase") {
		return "source_publication_check"
	}
	if strings.Contains(statement, "from library_roots") {
		return "root_binding_check"
	}
	if strings.Contains(statement, "from sessions") || strings.Contains(statement, "join sessions") {
		return "principal_revalidation"
	}
	return "other"
}

func hlsPhaseTimingQueryStart(ctx context.Context, connection *pgx.Conn, sql string) context.Context {
	request := hlsPhaseTimingFor(ctx)
	if request == nil {
		return ctx
	}
	return context.WithValue(ctx, hlsPhaseTimingContextKey{}, &hlsPhaseTimingSpan{
		request: request, started: time.Now(), category: hlsPhaseTimingCategory(sql), pid: connection.PgConn().PID()})
}

func hlsPhaseTimingQueryEnd(ctx context.Context, failed bool) {
	if span, _ := ctx.Value(hlsPhaseTimingContextKey{}).(*hlsPhaseTimingSpan); span != nil {
		span.request.record("query", span.category, span.started, span.pid, 0, failed)
	}
}

func hlsPhaseTimingBatchStart(ctx context.Context, connection *pgx.Conn, batch *pgx.Batch) context.Context {
	request := hlsPhaseTimingFor(ctx)
	if request == nil {
		return ctx
	}
	category := "other"
	if len(batch.QueuedQueries) > 0 {
		switch hlsPhaseTimingCategory(batch.QueuedQueries[0].SQL) {
		case "account_shared_lock", "credential_shared_lock":
			category = "authority"
		case "item_shared_lock":
			category = "source"
		case "play_shared_lock":
			category = "play_and_binding"
		}
	}
	now := time.Now()
	return context.WithValue(ctx, hlsPhaseTimingBatchContextKey{}, &hlsPhaseTimingSpan{
		request: request, started: now, previous: now, category: category, pid: connection.PgConn().PID()})
}

func hlsPhaseTimingBatchQuery(ctx context.Context, sql string, failed bool) {
	if span, _ := ctx.Value(hlsPhaseTimingBatchContextKey{}).(*hlsPhaseTimingSpan); span != nil {
		started := span.previous
		span.previous = time.Now()
		// This is an ordered result-completion consumption gap. It includes
		// network, row waits and caller consumption, not isolated server SQL time.
		span.request.record("batch_completion_consumption_gap", hlsPhaseTimingCategory(sql), started, span.pid, 0, failed)
	}
}

func hlsPhaseTimingBatchEnd(ctx context.Context, failed bool) {
	if span, _ := ctx.Value(hlsPhaseTimingBatchContextKey{}).(*hlsPhaseTimingSpan); span != nil {
		// pgx can report an early SendBatch failure again while draining Close.
		span.ended.Do(func() { span.request.record("batch_total", span.category, span.started, span.pid, 0, failed) })
	}
}

func (*hlsPriorityProfileTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	request := hlsPhaseTimingFor(ctx)
	if request == nil {
		return ctx
	}
	return context.WithValue(ctx, hlsPhaseTimingAcquireContextKey{}, &hlsPhaseTimingSpan{
		request: request, started: time.Now(), category: "connection", poolMax: pool.Stat().MaxConns()})
}

func (*hlsPriorityProfileTracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	if span, _ := ctx.Value(hlsPhaseTimingAcquireContextKey{}).(*hlsPhaseTimingSpan); span != nil {
		var pid uint32
		if data.Conn != nil {
			pid = data.Conn.PgConn().PID()
		}
		span.request.record("pool_acquire", span.category, span.started, pid, span.poolMax, data.Err != nil)
	}
}

func hlsProfileLogPhaseTiming(t *testing.T, current *hlsPriorityProfileCase, dimensions map[string]any) {
	t.Helper()
	if !hlsPhaseTimingEnabled() {
		return
	}
	for _, group := range []struct {
		name   string
		counts *hlsPriorityProfileCounts
	}{
		{"steady_get", &current.steady}, {"ping_or_stop_get", &current.stoppingGET}, {"stopped", &current.stopped},
	} {
		profile := group.counts.phaseTiming.Load()
		if profile == nil {
			continue
		}
		profile.mu.Lock()
		encoded, err := json.Marshal(map[string]any{
			"schema_version": 1, "request_group": group.name, "dimensions": dimensions,
			"completed_marked_requests": profile.requests, "statistics": profile.statistics, "slowest16": profile.slowest,
			"timing_contract": "Handler spans cover marked wrapper entry through ServeHTTP return, including response writes and backpressure, and differ from client Do/body-consumption timings. Query spans include server/network/row waits/result consumption after connection acquisition. Batch totals overlap their ordered completion consumption gaps. Pool acquisitions include pgx preparation/ping when present. No span is isolated server execution or row-lock wait. Categories and detail retention are bounded; timings include recorder cost.",
		})
		profile.mu.Unlock()
		if err != nil {
			t.Fatal("encode bounded HLS phase timings")
		}
		t.Logf("hls_phase_timing_profile=%s", encoded)
	}
}
