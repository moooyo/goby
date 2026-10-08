//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
	"golang.org/x/sys/unix"
)

// Copy this complete test-only file unchanged to both commit stages. The tests
// retain production constructor defaults and the same Data12/Control4 budget.
// Execute fresh pairs serially; neither test is a client, GPU or capacity gate.
const httpGetStopRemeasureRequests = 256

func httpGetStopRemeasureInputs(t *testing.T) map[string]any {
	t.Helper()
	if os.Getenv("GOBY_HTTP_GET_STOP_REMEASURE") != "1" {
		t.Skip("GOBY_HTTP_GET_STOP_REMEASURE=1 admits the matched HTTP measurement")
	}
	variant, runID := os.Getenv("GOBY_HTTP_REMEASURE_VARIANT"), os.Getenv("GOBY_HTTP_REMEASURE_RUN_ID")
	if variant != "baseline" && variant != "candidate" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`).MatchString(runID) {
		t.Fatal("measurement requires a bounded run identity and baseline or candidate variant")
	}
	if os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("enabled measurement requires the actual PostgreSQL fixture")
	}
	for _, name := range []string{"GOBY_FFMPEG", "GOBY_FFPROBE"} {
		path := os.Getenv(name)
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("enabled measurement requires an absolute executable %s", name)
		}
	}
	return map[string]any{"schema_version": 1, "variant": variant, "run_id": runID,
		"go_version": runtime.Version(), "go_max_procs": runtime.GOMAXPROCS(0), "go_num_cpu": runtime.NumCPU(),
		"scope": "bounded real loopback HTTP/1 fixture; no existing player, GPU, physical cold-storage or whole-service capacity claim"}
}

func httpGetStopRemeasureLog(t *testing.T, record map[string]any) {
	t.Helper()
	record["success"] = !t.Failed()
	encoded, err := json.Marshal(record)
	if err != nil || len(encoded) > 128<<10 {
		t.Error("bounded HTTP measurement record could not be encoded")
		return
	}
	t.Logf("http_get_stop_remeasure=%s", encoded)
}

func httpGetStopRemeasureRecord(inputs map[string]any, kind string) map[string]any {
	record := make(map[string]any, len(inputs)+32)
	for key, value := range inputs {
		record[key] = value
	}
	record["record_type"] = kind
	return record
}

func httpGetStopRemeasurePoolDelta(before, after hlsProfilePoolCounters) map[string]any {
	return map[string]any{"acquires": after.Acquires - before.Acquires,
		"empty_acquires": after.EmptyAcquires - before.EmptyAcquires, "canceled_acquires": after.CanceledAcquires - before.CanceledAcquires,
		"new_connections":     after.NewConnections - before.NewConnections,
		"acquire_duration_ns": (after.AcquireDuration - before.AcquireDuration).Nanoseconds(),
		"empty_wait_ns":       (after.EmptyWait - before.EmptyWait).Nanoseconds()}
}

func httpGetStopRemeasureCounts(counts *hlsPriorityProfileCounts) map[string]any {
	return map[string]any{"requests": counts.requests.Load(), "sql": counts.sql.Load(),
		"play_lock_reads": counts.playbackReads.Load(), "source_reads": counts.sourceReads.Load(),
		"final_play_reads": counts.finalPlaybackReads.Load(), "fresh_requests": counts.freshRequests.Load(),
		"entity_projections": counts.entityProjections.Load()}
}

// The existing tracer keeps request facts private in its HTTP context. This
// outer observer associates them with the final HTTP status, so successful
// cancellation-burst responses cannot borrow failed requests' authority facts.
type httpGetStopRemeasureAuthorityKey struct{}

type httpGetStopRemeasureAuthority struct {
	facts atomic.Pointer[hlsPriorityProfileRequestFacts]
}

type httpGetStopRemeasureAuthorityCounts struct {
	successful, fresh, invalid atomic.Int64
	active                     atomic.Int64
}

type httpGetStopRemeasureAuthorityCase struct {
	steady, burst httpGetStopRemeasureAuthorityCounts
}

type httpGetStopRemeasureTracer struct {
	playbackMediaHTTPProfileTrace
	authority  atomic.Pointer[httpGetStopRemeasureAuthorityCase]
	activeHTTP atomic.Int64
}

func httpGetStopRemeasureRememberFacts(ctx context.Context) {
	if observer, _ := ctx.Value(httpGetStopRemeasureAuthorityKey{}).(*httpGetStopRemeasureAuthority); observer != nil {
		if facts, _ := ctx.Value(hlsPriorityProfileRequestContextKey{}).(*hlsPriorityProfileRequestFacts); facts != nil {
			observer.facts.Store(facts)
		}
	}
}

func (trace *httpGetStopRemeasureTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	httpGetStopRemeasureRememberFacts(ctx)
	return trace.playbackMediaHTTPProfileTrace.TraceQueryStart(ctx, connection, data)
}

func (trace *httpGetStopRemeasureTracer) TraceBatchStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	httpGetStopRemeasureRememberFacts(ctx)
	return trace.playbackMediaHTTPProfileTrace.TraceBatchStart(ctx, connection, data)
}

type httpGetStopRemeasureWriter struct {
	http.ResponseWriter
	status int
}

func (writer *httpGetStopRemeasureWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *httpGetStopRemeasureWriter) WriteHeader(status int) {
	if status >= 200 && writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *httpGetStopRemeasureWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *httpGetStopRemeasureWriter) Flush() {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *httpGetStopRemeasureWriter) ReadFrom(reader io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if consumer, ok := writer.ResponseWriter.(io.ReaderFrom); ok {
		return consumer.ReadFrom(reader)
	}
	return io.Copy(writer.ResponseWriter, reader)
}

func (trace *httpGetStopRemeasureTracer) wrap(next http.Handler) http.Handler {
	inner := trace.priority.wrap(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace.activeHTTP.Add(1)
		defer trace.activeHTTP.Add(-1)
		current := trace.authority.Load()
		var counts *httpGetStopRemeasureAuthorityCounts
		if current != nil {
			switch r.Header.Get(hlsPriorityProfileHeader) {
			case "steady":
				counts = &current.steady
			case "stop_get":
				counts = &current.burst
			}
		}
		if counts == nil {
			inner.ServeHTTP(w, r)
			return
		}
		counts.active.Add(1)
		defer counts.active.Add(-1)
		observer := &httpGetStopRemeasureAuthority{}
		writer := &httpGetStopRemeasureWriter{ResponseWriter: w}
		inner.ServeHTTP(writer, r.WithContext(context.WithValue(r.Context(), httpGetStopRemeasureAuthorityKey{}, observer)))
		if writer.status == http.StatusOK {
			counts.successful.Add(1)
			facts := observer.facts.Load()
			if facts != nil && facts.playbackLocks.Load() >= 2 && facts.sourceReads.Load() >= 2 && facts.finalPlaybackReads.Load() >= 2 {
				counts.fresh.Add(1)
			} else {
				counts.invalid.Add(1)
			}
		}
	})
}

func httpGetStopRemeasureWaitAuthority(t *testing.T, counts *httpGetStopRemeasureAuthorityCounts) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for counts.active.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the marked successful-response authority observers did not join")
		}
		time.Sleep(time.Millisecond)
	}
}

func httpGetStopRemeasureJoinRequests(t *testing.T, parent context.Context, hls *hlsRuntime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		hls.requests.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("actual HLS request cleanup did not join within its finite deadline")
	}
}

func httpGetStopRemeasureWarmPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int32) {
	t.Helper()
	held := make([]*pgxpool.Conn, 0, count)
	defer func() {
		for _, connection := range held {
			connection.Release()
		}
	}()
	for range count {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal("warm every usable measurement database connection")
		}
		held = append(held, connection)
	}
}

func httpGetStopRemeasureCachedOutput(t *testing.T, h *hlsHTTPFixture, graph hlsHTTPGraph) []byte {
	t.Helper()
	var expected []byte
	for index, child := range graph.children {
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if index == 0 {
			expected = response.body
		}
	}
	if len(expected) == 0 {
		t.Fatal("cached measurement has no fixed segment representation")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var jobs, completed int
		if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*),count(*) FILTER (WHERE state='completed') FROM encoding_jobs WHERE play_session_id=$1", graph.playID).Scan(&jobs, &completed); err != nil {
			t.Fatal("observe completed cached producers before measurement")
		}
		if jobs > 0 && jobs == completed {
			return expected
		}
		if time.Now().After(deadline) {
			t.Fatal("cached producers did not complete before the measurement window")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func httpGetStopRemeasureWarmHTTP(t *testing.T, h *hlsHTTPFixture, trace *httpGetStopRemeasureTracer, client, controls *http.Client, target string, expected []byte, headers http.Header) {
	t.Helper()
	gate := &hlsPriorityProfileWarmGate{ready: make(chan struct{}), release: make(chan struct{})}
	trace.priority.warm.Store(gate)
	warmed := make(chan []hlsPriorityProfileResponse, 1)
	go func() { warmed <- hlsPriorityProfileWave(h.f.ctx, client, target, "warm", 32, 1) }()
	select {
	case <-gate.ready:
	case <-h.f.ctx.Done():
		close(gate.release)
		t.Fatal("measurement warm-up did not establish 32 live TCP connections")
	}
	close(gate.release)
	hlsPriorityProfileCheck(t, <-warmed, expected)
	httpGetStopRemeasureJoinRequests(t, h.f.ctx, h.f.app.hls)
	trace.priority.warm.Store(nil)
	warmControl := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodGet, h.server.URL+"/emby/System/Info", "", nil, headers)
	if warmControl.err != nil || warmControl.status != http.StatusOK {
		t.Fatal("warm the independent Stop TCP connection")
	}
}

// This state observation follows actual HTTP joins. It does not infer encoder
// retirement: every producer in the cached case completed before sampling.
func httpGetStopRemeasureFence(t *testing.T, h *hlsHTTPFixture, graph hlsHTTPGraph, session *hlsSession, producers []hlsProducer, client *http.Client) transcode.ResourceUsage {
	t.Helper()
	late := hlsPriorityProfileRequest(h.f.ctx, client, http.MethodGet, h.server.URL+graph.children[0], "", nil, nil)
	if late.err != nil || late.status != http.StatusNotFound {
		t.Fatal("a fresh request reused the stopped owner's cached output")
	}
	var state string
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT state FROM play_sessions WHERE id=$1", graph.playID).Scan(&state); err != nil || state != "Stopped" {
		t.Fatal("the real Stop did not persist its exact playback state")
	}
	h.f.app.hls.mu.Lock()
	registered := h.f.app.hls.sessions[graph.hlsID] != nil
	h.f.app.hls.mu.Unlock()
	var activeJobs int
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*) FROM encoding_jobs WHERE auth_session_id=$1 AND play_session_id=$2 AND state IN ('queued','running')", session.key.scope.AuthSessionID, graph.playID).Scan(&activeJobs); err != nil || activeJobs != 0 || registered || session.ctx.Err() == nil {
		t.Fatal("Stop retained its exact HLS registration, lifetime or active encoding owner")
	}
	policy := h.f.app.playbackPolicyGate()
	policy.mu.Lock()
	retained := false
	for _, lease := range policy.leases {
		retained = retained || lease.scope == session.key.scope
	}
	policy.mu.Unlock()
	if retained {
		t.Fatal("Stop retained its exact delivery policy lease")
	}
	for _, producer := range producers {
		handle, err := h.f.app.hls.manager.TryOpen(session.key.scope, producer.id, "segment-000000.ts")
		if handle != nil {
			_ = handle.Close()
		}
		if handle != nil || !errors.Is(err, transcode.ErrJobCancelled) && !errors.Is(err, transcode.ErrJobNotFound) {
			t.Fatal("Stop failed to fence the exact completed producer's output")
		}
	}
	resources, ok := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("cached manager has no read-only exact-scope ownership observation")
	}
	ctx, cancel := context.WithTimeout(h.f.ctx, 5*time.Second)
	defer cancel()
	for {
		usage, err := resources.ResourceUsage(ctx, session.key.scope)
		if err != nil || usage.AccountingUnknownJobs != 0 || usage.CompletionUnknownJobs != 0 {
			t.Fatal("cached ownership completion became unknown")
		}
		intentRetained := false
		if h.f.app.correlatedHLSOwnershipEnabled {
			gate := &h.f.app.playbackStopIntents
			gate.mu.Lock()
			intentRetained = gate.entries[playbackStopIntentKeyFor(session.key.scope)] != nil
			gate.mu.Unlock()
		}
		if !intentRetained && usage.UnfinishedJobs == 0 && usage.CreatingJobs == 0 && usage.QueuedJobs == 0 &&
			usage.ExecutionReservations == 0 && usage.ReaderPins == 0 && usage.ReclaimingJobs == 0 && usage.AccountingPendingJobs == 0 &&
			usage.CompletionReservedJobs == 0 && usage.CompletionQueuedJobs == 0 && usage.CompletionWorkingJobs == 0 {
			// Cancelled completed history and its accounted cache bytes may be
			// retained until fixture Close. They are not live consumers or pins.
			return usage
		}
		select {
		case <-ctx.Done():
			t.Fatal("cached exact-scope owners, output pins or Stop references did not complete")
		case <-time.After(time.Millisecond):
		}
	}
}

func httpGetStopRemeasureBurst(t *testing.T, ctx context.Context, samples []hlsPriorityProfileResponse, expected []byte) map[string]int {
	t.Helper()
	counts := map[string]int{"ok": 0, "not_found": 0, "limited": 0, "interrupted": 0}
	for sampleOrdinal, sample := range samples {
		switch {
		case sample.err != nil:
			var networkError net.Error
			if ctx.Err() != nil || errors.Is(sample.err, context.Canceled) || errors.Is(sample.err, context.DeadlineExceeded) ||
				errors.As(sample.err, &networkError) && networkError.Timeout() ||
				!(errors.Is(sample.err, io.EOF) || errors.Is(sample.err, io.ErrUnexpectedEOF) || errors.Is(sample.err, net.ErrClosed) ||
					errors.Is(sample.err, syscall.ECONNRESET) || errors.Is(sample.err, syscall.EPIPE)) {
				t.Fatalf("unexpected cancellation-burst transport failure: error_type=%T", sample.err)
			}
			counts["interrupted"]++
		case sample.status == http.StatusOK:
			hlsPriorityProfileCheck(t, []hlsPriorityProfileResponse{sample}, expected)
			counts["ok"]++
		case sample.status == http.StatusNotFound:
			counts["not_found"]++
		case sample.status == http.StatusTooManyRequests:
			counts["limited"]++
		default:
			bodyPrefix := sample.data
			if len(bodyPrefix) > 256 {
				bodyPrefix = bodyPrefix[:256]
			}
			bodyTruncated := len(sample.data) > len(bodyPrefix)
			bodyRedacted := regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://|/emby(?:/|\?)|[?&](?:api_key|access_token|token)=)`).Match(bodyPrefix)
			if bodyRedacted {
				bodyPrefix = []byte("<redacted URL-like response>")
			}
			t.Fatalf("unexpected cancellation-burst status=%d sample_ordinal=%d body_prefix=%q body_bytes=%d body_truncated=%t body_redacted=%t",
				sample.status, sampleOrdinal+1, bodyPrefix, len(sample.data), bodyTruncated, bodyRedacted)
		}
	}
	return counts
}

func TestHTTPCachedHLSGetStopRemeasurePerformance(t *testing.T) {
	inputs := httpGetStopRemeasureInputs(t)
	if os.Getenv("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY") != "1" || !hlsPhaseTimingEnabled() {
		t.Fatal("cached measurement requires production database capacity and matched phase timing")
	}
	h := newHLSHTTPFixture(t, 6*time.Minute)
	key := applicationMediaIssueKeys(t, h.f, h.accounts.admin.headers.Get("X-Emby-Token"))[0]
	h.server.Close()
	if err := h.f.app.Close(h.f.ctx); err != nil {
		t.Fatal("join the initial application before replacing the traced generation")
	}
	trace := &httpGetStopRemeasureTracer{}
	configuration := h.f.pool.Config().Copy()
	configuration.MaxConns, configuration.ConnConfig.Tracer = database.DataMaxConns, trace
	pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
	if err != nil {
		t.Fatal("create the traced measurement Data pool")
	}
	t.Cleanup(pool.Close)
	options := hlsProfileOptions(t, h.f.ctx, pool)
	users := identity.New(pool)
	app, err := New(h.f.ctx, h.f.cfg, pool, users, h.f.log, "http-get-stop-remeasure", options...)
	if err != nil {
		t.Fatal("create the measurement application with production defaults")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error("measurement application cleanup retained ownership")
		}
	})
	h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
	h.server = httptest.NewServer(trace.wrap(h.f.handler))
	t.Cleanup(h.server.Close)
	capacity := hlsProfileDatabaseCapacities(pool, app)
	if capacity.DataMax != 12 || capacity.DataUsable != 11 || capacity.ControlMax != 4 || capacity.ApplicationMax != 16 || app.hls.generatedWindowsEnabled {
		t.Fatal("measurement changed the production capacity or legacy file-HLS runtime")
	}
	httpGetStopRemeasureWarmPool(t, h.f.ctx, pool, capacity.DataUsable)
	client, controls := hlsPriorityProfileClient(t), hlsPriorityProfileClient(t)
	var measuredScopes []transcode.Scope
	for _, application := range []bool{false, true} {
		for _, callers := range []int{1, 8, 32} {
			t.Run(fmt.Sprintf("application-%t/callers-%d", application, callers), func(t *testing.T) {
				record := httpGetStopRemeasureRecord(inputs, "cached_get_stop")
				defer httpGetStopRemeasureLog(t, record)
				record["application_key"], record["callers"], record["requests"] = application, callers, httpGetStopRemeasureRequests
				record["data_pool_max"], record["data_pool_usable"], record["control_pool_max"], record["application_pool_max"] = capacity.DataMax, capacity.DataUsable, capacity.ControlMax, capacity.ApplicationMax
				record["observer_pool_max"] = h.f.pool.Config().MaxConns
				record["correlated_hls_default"], record["early_stop_default"] = app.correlatedHLSOwnershipEnabled, app.correlatedHLSEarlyStopEnabled
				graph, headers := hlsHTTPGraph{}, h.accounts.viewer.headers
				if application {
					graph, headers = applicationMediaRealGraph(t, h, key, h.accounts.viewer.userID), key.headers
				} else {
					graph = h.graph(t, h.accounts.viewer, 0)
				}
				expected := httpGetStopRemeasureCachedOutput(t, h, graph)
				target := h.server.URL + graph.children[0]
				httpGetStopRemeasureWarmHTTP(t, h, trace, client, controls, target, expected, headers)
				current, authority := &hlsPriorityProfileCase{}, &httpGetStopRemeasureAuthorityCase{}
				current.stoppingGET.entered = make(chan struct{})
				stopDiagnostic := hlsStopDiagnosticBegin(t)
				trace.priority.current.Store(current)
				trace.authority.Store(authority)
				defer trace.priority.current.Store(nil)
				defer trace.authority.Store(nil)
				trace.begins.Store(0)
				trace.commits.Store(0)
				dataBefore, controlBefore := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				admissionBefore := hlsProfileSourceAdmissionCounters(app)
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				started := time.Now()
				samples := hlsPriorityProfileWave(h.f.ctx, client, target, "steady", callers, httpGetStopRemeasureRequests/callers)
				elapsed := time.Since(started)
				hlsPriorityProfileWaitIdle(t, &current.steady)
				httpGetStopRemeasureJoinRequests(t, h.f.ctx, app.hls)
				httpGetStopRemeasureWaitAuthority(t, &authority.steady)
				runtime.ReadMemStats(&after)
				latencies := hlsPriorityProfileCheck(t, samples, expected)
				raw := make([]int64, len(samples))
				var ordinals []int64
				var seenOrdinals [httpGetStopRemeasureRequests + 1]bool
				if hlsStopDiagnosticEnabled() && hlsPhaseTimingEnabled() {
					ordinals = make([]int64, len(samples))
				}
				for index, sample := range samples {
					raw[index] = sample.duration.Nanoseconds()
					if ordinals != nil {
						ordinal := sample.requestOrdinal
						if ordinal < 1 || ordinal > httpGetStopRemeasureRequests || seenOrdinals[ordinal] {
							t.Fatal("steady GET response ordinals are missing, duplicated or outside the selected wave")
						}
						seenOrdinals[ordinal] = true
						ordinals[index] = ordinal
					}
				}
				if ordinals != nil {
					record["get_raw_request_ordinals"] = ordinals
				}
				median := (latencies[len(latencies)/2-1] + latencies[len(latencies)/2]) / 2
				record["segment_bytes"], record["get_elapsed_ns"], record["get_raw_duration_ns"] = len(expected), elapsed.Nanoseconds(), raw
				record["get_p50_ns"], record["get_p95_ns"], record["get_p99_ns"] = median.Nanoseconds(), hlsPriorityProfilePercentile(latencies, 95).Nanoseconds(), hlsPriorityProfilePercentile(latencies, 99).Nanoseconds()
				record["get_counts"], record["get_begins"], record["get_commits"] = httpGetStopRemeasureCounts(&current.steady), trace.begins.Load(), trace.commits.Load()
				record["get_mallocs"], record["get_allocated_bytes"] = after.Mallocs-before.Mallocs, after.TotalAlloc-before.TotalAlloc
				record["get_data_pool"], record["get_control_pool"] = httpGetStopRemeasurePoolDelta(dataBefore, hlsProfileCounters(pool.Stat())), httpGetStopRemeasurePoolDelta(controlBefore, hlsProfileControlCounters(app))
				record["get_source_admission"] = hlsProfileSourceAdmissionDelta(admissionBefore, hlsProfileSourceAdmissionCounters(app))
				record["get_successful_fresh"] = authority.steady.fresh.Load()
				if current.steady.requests.Load() != httpGetStopRemeasureRequests || current.steady.freshRequests.Load() != httpGetStopRemeasureRequests ||
					current.steady.playbackReads.Load() < 2*httpGetStopRemeasureRequests || current.steady.sourceReads.Load() < 2*httpGetStopRemeasureRequests ||
					current.steady.finalPlaybackReads.Load() < 2*httpGetStopRemeasureRequests || current.steady.entityProjections.Load() != 0 ||
					authority.steady.successful.Load() != httpGetStopRemeasureRequests || authority.steady.fresh.Load() != httpGetStopRemeasureRequests || authority.steady.invalid.Load() != 0 {
					t.Fatal("a successful cached GET omitted one of its two fresh authority stages")
				}
				app.hls.mu.Lock()
				session := app.hls.sessions[graph.hlsID]
				app.hls.mu.Unlock()
				if session == nil {
					t.Fatal("cached measurement lost its exact HLS owner before Stop")
				}
				measuredScopes = append(measuredScopes, session.key.scope)
				session.mu.Lock()
				producers := append([]hlsProducer(nil), session.producers...)
				session.mu.Unlock()
				body, err := json.Marshal(map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0})
				if err != nil {
					t.Fatal("encode the bounded standard Stop report")
				}
				stopData, stopControl := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				stopAdmission := hlsProfileSourceAdmissionCounters(app)
				trace.begins.Store(0)
				trace.commits.Store(0)
				burstDone := make(chan []hlsPriorityProfileResponse, 1)
				go func() { burstDone <- hlsPriorityProfileWave(h.f.ctx, client, target, "stop_get", callers, 1) }()
				select {
				case <-current.stoppingGET.entered:
					hlsStopDiagnosticGateReceived(stopDiagnostic)
				case <-h.f.ctx.Done():
					t.Fatal("cached Stop burst did not reach real playback validation")
				}
				stopStarted := time.Now()
				stopped := hlsPriorityProfileRequest(hlsStopDiagnosticClientContext(h.f.ctx, stopDiagnostic), controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Stopped", "stopped", body, headers)
				record["stop_response_ns"], record["stop_status"], record["stop_overlap_handlers"] = stopped.duration.Nanoseconds(), stopped.status, current.stopOverlap.Load()
				if stopped.err != nil || stopped.status != http.StatusNoContent || current.stopOverlap.Load() == 0 {
					t.Fatal("standard Stop failed or did not enter during a live same-scope cached GET")
				}
				burst := <-burstDone
				hlsPriorityProfileWaitIdle(t, &current.stoppingGET, &current.stopped)
				httpGetStopRemeasureJoinRequests(t, h.f.ctx, app.hls)
				httpGetStopRemeasureWaitAuthority(t, &authority.burst)
				record["stop_handler_drain_observed_upper_bound_ns"] = time.Since(stopStarted).Nanoseconds()
				record["stop_burst_outcomes"] = httpGetStopRemeasureBurst(t, h.f.ctx, burst, expected)
				if len(burst) != callers || authority.burst.invalid.Load() != 0 || authority.burst.successful.Load() != authority.burst.fresh.Load() {
					t.Fatal("a successful cancellation-burst GET omitted a fresh authority stage")
				}
				record["stop_burst_successful_fresh"] = authority.burst.fresh.Load()
				record["stop_counts"], record["stop_burst_counts"] = httpGetStopRemeasureCounts(&current.stopped), httpGetStopRemeasureCounts(&current.stoppingGET)
				record["stop_and_burst_begins"], record["stop_and_burst_commits"] = trace.begins.Load(), trace.commits.Load()
				record["stop_and_burst_data_pool"], record["stop_and_burst_control_pool"] = httpGetStopRemeasurePoolDelta(stopData, hlsProfileCounters(pool.Stat())), httpGetStopRemeasurePoolDelta(stopControl, hlsProfileControlCounters(app))
				record["stop_and_burst_source_admission"] = hlsProfileSourceAdmissionDelta(stopAdmission, hlsProfileSourceAdmissionCounters(app))
				record["stop_exact_scope_resource_usage"] = httpGetStopRemeasureFence(t, h, graph, session, producers, client)
				record["stop_lifetime_output_lease_completion_observed_upper_bound_ns"] = time.Since(stopStarted).Nanoseconds()
				record["stop_lifetime_output_lease_complete"], record["encoder_reap_measured"] = true, false
				record["timing_contract"] = "GET and Stop client spans include Do, bounded complete body consumption and Body.Close over TCP; p50 averages the middle two, p95/p99 use nearest rank; drain/owner-completion values are observation upper bounds including joins/assertions; cancelled completed history and accounted cache bytes may remain until fixture Close; allocation/pool/admission deltas include natural process work and recorder cost, and overlapping stage sums are not additive or isolated filesystem/SQL execution time"
				hlsStopDiagnosticExport(t, record, current, stopDiagnostic)
				hlsProfileLogPhaseTiming(t, current, map[string]any{"workload": "matched_cached_get_stop", "application_key": application, "callers": callers})
			})
			if t.Failed() {
				return
			}
		}
	}
	closed := httpGetStopRemeasureRecord(inputs, "cached_fixture_close")
	defer httpGetStopRemeasureLog(t, closed)
	closeStarted := time.Now()
	if err := app.Close(h.f.ctx); err != nil {
		t.Fatal("cached measurement Store and dependency owners did not join")
	}
	closed["store_and_dependency_join_ns"] = time.Since(closeStarted).Nanoseconds()
	resources, ok := app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("closed cached manager has no read-only exact-scope observation")
	}
	finalResources := make([]transcode.ResourceUsage, 0, len(measuredScopes))
	for _, scope := range measuredScopes {
		usage, err := resources.ResourceUsage(h.f.ctx, scope)
		if err != nil || usage != (transcode.ResourceUsage{}) {
			t.Fatal("cached Store Close retained exact-scope resources or accounting")
		}
		finalResources = append(finalResources, usage)
	}
	closed["final_scope_resource_usage"] = finalResources
	closed["guard_observed_at"] = time.Now().UTC()
	info, err := os.Stat(h.path)
	// Retain each guard's first observation, including on failure. These are
	// sequential post-Close snapshots, not a simultaneous owner inventory;
	// neither waiting nor reacquiring may replace an initially failing value.
	dataState, controlState := pool.Stat(), app.playbackControlDB.Stat()
	streamSlots, hlsSlots := len(app.streamSlots), len(app.hls.slots)
	closed["outer_http_handlers"] = trace.activeHTTP.Load()
	closed["source_stat_error"] = ""
	if err != nil {
		closed["source_stat_error"] = err.Error()
	}
	closed["data_pool_state"] = map[string]any{
		"acquired": dataState.AcquiredConns(), "idle": dataState.IdleConns(),
		"constructing": dataState.ConstructingConns(), "total": dataState.TotalConns(), "max": dataState.MaxConns(),
	}
	closed["control_pool_state"] = map[string]any{
		"acquired": controlState.AcquiredConns(), "idle": controlState.IdleConns(),
		"constructing": controlState.ConstructingConns(), "total": controlState.TotalConns(), "max": controlState.MaxConns(),
	}
	closed["stream_slots"], closed["hls_slots"] = streamSlots, hlsSlots
	closed["pool_loans"], closed["http_slots"] = dataState.AcquiredConns()+controlState.AcquiredConns(), streamSlots+hlsSlots
	closed["library_available"] = app.library.Available()
	closed["hls_done"] = false
	select {
	case <-app.hls.done:
		closed["hls_done"] = true
	default:
	}
	// A failed source stat cannot supply the identity needed for the FD check.
	// Keep that count unavailable instead of reporting an unobserved zero.
	sourceFDs := -1
	closed["source_device_inode_fds"] = nil
	if err == nil {
		sourceFDs = playbackStopAliasSourceFDs(t, info)
		closed["source_device_inode_fds"] = sourceFDs
	}
	if err != nil || dataState.AcquiredConns() != 0 || controlState.AcquiredConns() != 0 || sourceFDs != 0 || streamSlots != 0 || hlsSlots != 0 {
		t.Fatal("closed cached fixture retained a pool loan, HTTP slot or source descriptor")
	}
}

type httpGetStopRemeasureRetirement struct {
	ExitNS      int64  `json:"exit_observed_upper_bound_ns"`
	ReapGroupNS int64  `json:"reap_group_observed_upper_bound_ns"`
	StatESRCH   int    `json:"pidfd_ready_stat_esrch_unknown_observations"`
	PinnedExit  bool   `json:"pinned_pidfd_exit"`
	StatENOENT  bool   `json:"actual_stat_enoent"`
	GroupESRCH  bool   `json:"original_group_esrch"`
	Failure     string `json:"failure_code,omitempty"`
}

type httpGetStopRemeasureConsumer struct {
	status                   int
	bytes                    int64
	doErr, readErr, closeErr error
}

func httpGetStopRemeasureConsumerErrorAccepted(err error) bool {
	if err == nil {
		return true
	}
	var networkError net.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

func httpGetStopRemeasureConsumerAccepted(ctx context.Context, result httpGetStopRemeasureConsumer) bool {
	if ctx.Err() != nil || result.bytes >= 17<<20 ||
		result.status != 0 && result.status != http.StatusOK && result.status != http.StatusNotFound && result.status != http.StatusTooManyRequests ||
		result.status == 0 && result.doErr == nil {
		return false
	}
	// A permitted read interruption cannot hide an unknown Do or Close error.
	return httpGetStopRemeasureConsumerErrorAccepted(result.doErr) &&
		httpGetStopRemeasureConsumerErrorAccepted(result.readErr) && httpGetStopRemeasureConsumerErrorAccepted(result.closeErr)
}

// A pidfd pins the observed leader. Reap requires actual stat ENOENT and an
// absent original process group; ESRCH from stat remains unknown and may only
// be observed again under this same finite deadline. No termination signal is sent.
func httpGetStopRemeasureRetire(ctx context.Context, process playbackStopAliasEncoder, started time.Time) httpGetStopRemeasureRetirement {
	result := httpGetStopRemeasureRetirement{}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			result.Failure = "retirement_observation_deadline"
			return result
		}
		fds := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 0)
		if err != nil && !errors.Is(err, unix.EINTR) || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			result.Failure = "pinned_exit_observation_unknown"
			return result
		}
		exited := err == nil && ready > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0
		if exited && !result.PinnedExit {
			result.PinnedExit, result.ExitNS = true, time.Since(started).Nanoseconds()
		}
		_, start, statErr := hlsColdPID(process.pid)
		if statErr == nil && start != process.startTick {
			result.Failure = "process_start_identity_changed"
			return result
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			if exited && errors.Is(statErr, syscall.ESRCH) {
				result.StatESRCH++
			} else {
				result.Failure = "process_stat_observation_unknown"
				return result
			}
		}
		if exited && errors.Is(statErr, os.ErrNotExist) {
			result.StatENOENT = true
			groupErr := syscall.Kill(-process.pid, 0)
			if errors.Is(groupErr, syscall.ESRCH) {
				if ctx.Err() != nil {
					result.Failure = "retirement_observation_deadline"
					return result
				}
				result.GroupESRCH, result.ReapGroupNS = true, time.Since(started).Nanoseconds()
				return result
			}
			if groupErr != nil {
				result.Failure = "process_group_observation_unknown"
				return result
			}
		}
		select {
		case <-ctx.Done():
			result.Failure = "retirement_observation_deadline"
			return result
		case <-ticker.C:
		}
	}
}

// Each fresh run contributes one standard Stopped sample. Three matched pairs
// yield three observations per variant, suitable for min/median/max only.
func TestHTTPRunningEncoderStoppedRemeasurePerformance(t *testing.T) {
	inputs := httpGetStopRemeasureInputs(t)
	record := httpGetStopRemeasureRecord(inputs, "running_encoder_stopped")
	defer httpGetStopRemeasureLog(t, record)
	processBefore := media.GetProcessCapacityStats()
	if processBefore.Active != 0 || processBefore.Background != 0 || processBefore.Queued != 0 {
		t.Fatal("fresh encoder measurement began with unrelated charged media processes")
	}
	fixture := newPlaybackStopAliasRealFixture(t)
	f, h := fixture.control, fixture.hls
	if f.pool.Config().MaxConns != 12 || f.control.Config().MaxConns != 4 || f.app.hls.generatedWindowsEnabled {
		t.Fatal("running encoder measurement changed production capacity or legacy HLS")
	}
	principal, headers := f.principal(t, "normal")
	controls := hlsPriorityProfileClient(t)
	warm := hlsPriorityProfileRequest(f.ctx, controls, http.MethodGet, h.server.URL+"/emby/System/Info", "", nil, headers)
	if warm.err != nil || warm.status != http.StatusOK {
		t.Fatal("warm the running encoder's independent Stop TCP connection")
	}
	graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID, deviceID: principal.Client.DeviceID, headers: headers}, 0)
	f.app.hls.mu.Lock()
	session := f.app.hls.sessions[graph.hlsID]
	f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("running encoder measurement has no exact admitted HLS owner")
	}
	getCtx, cancelGET := context.WithCancel(f.ctx)
	defer cancelGET()
	request, err := http.NewRequestWithContext(getCtx, http.MethodGet, h.server.URL+graph.children[0], nil)
	if err != nil {
		t.Fatal("create the natural running encoder HTTP consumer")
	}
	consumerDone := make(chan struct{})
	consumerResult := make(chan httpGetStopRemeasureConsumer, 1)
	go func() {
		defer close(consumerDone)
		result := httpGetStopRemeasureConsumer{}
		response, err := h.server.Client().Do(request)
		result.doErr = err
		if response != nil {
			result.status = response.StatusCode
			result.bytes, result.readErr = io.Copy(io.Discard, io.LimitReader(response.Body, 17<<20))
			result.closeErr = response.Body.Close()
		}
		consumerResult <- result
	}()
	process := playbackStopAliasObserveEncoder(t, fixture, session.key.scope)
	workDeadline := time.Now().Add(3 * time.Second)
	for process.last.UserTicks+process.last.SystemTicks <= process.first.UserTicks+process.first.SystemTicks ||
		process.last.ReadChars <= process.first.ReadChars && process.last.WriteChars <= process.first.WriteChars {
		if time.Now().After(workDeadline) {
			t.Fatal("the pinned running encoder did not show both CPU and character-IO growth")
		}
		last, err := hlsWallPauseReadProcess(process.pid, process.startTick)
		if err != nil {
			t.Fatal("running encoder identity became unknown before its work witness")
		}
		process.last = last
		time.Sleep(10 * time.Millisecond)
	}
	state, startTick, statErr := hlsColdPID(process.pid)
	exited, exitErr := hlsWallPausePIDFDExited(process.pidfd)
	group, groupErr := syscall.Getpgid(process.pid)
	if statErr != nil || startTick != process.startTick || state == 'Z' || state == 'X' || exitErr != nil || exited || groupErr != nil || group != process.pid {
		t.Fatal("the exact witnessed encoder was not live in its original group immediately before Stop")
	}
	record["pid"], record["start_tick"], record["original_process_group"] = process.pid, process.startTick, group
	record["cpu_ticks_before"], record["cpu_ticks_after"] = process.first.UserTicks+process.first.SystemTicks, process.last.UserTicks+process.last.SystemTicks
	record["read_chars_before"], record["read_chars_after"] = process.first.ReadChars, process.last.ReadChars
	record["write_chars_before"], record["write_chars_after"] = process.first.WriteChars, process.last.WriteChars
	record["correlated_hls_default"], record["early_stop_default"] = f.app.correlatedHLSOwnershipEnabled, f.app.correlatedHLSEarlyStopEnabled
	record["data_pool_max"], record["control_pool_max"], record["software_encoder_readrate"] = 12, 4, 0.5
	body, err := json.Marshal(map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0})
	if err != nil {
		t.Fatal("encode the running encoder's standard Stop report")
	}
	dataBefore, controlBefore := hlsProfileCounters(f.pool.Stat()), hlsProfileCounters(f.control.Stat())
	select {
	case <-consumerDone:
		t.Fatal("the natural encoder consumer ended before standard Stop dispatch")
	default:
	}
	record["consumer_pending_before_stop"] = true
	stopStarted := time.Now()
	observeCtx, cancelObserve := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancelObserve()
	retired := make(chan httpGetStopRemeasureRetirement, 1)
	go func() { retired <- httpGetStopRemeasureRetire(observeCtx, process, stopStarted) }()
	stopped := hlsPriorityProfileRequest(f.ctx, controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Stopped", "", body, headers)
	record["stop_response_ns"], record["stop_status"] = stopped.duration.Nanoseconds(), stopped.status
	record["stop_response_observed_upper_bound_ns"] = time.Since(stopStarted).Nanoseconds()
	retirement := <-retired
	record["retirement"] = retirement
	if stopped.err != nil || stopped.status != http.StatusNoContent || retirement.Failure != "" || !retirement.PinnedExit || !retirement.StatENOENT || !retirement.GroupESRCH {
		t.Fatal("standard Stop response or exact pinned exit/reap/group retirement failed")
	}
	select {
	case <-consumerDone:
	case <-observeCtx.Done():
		t.Fatal("the stopped natural HTTP consumer did not join without client cancellation")
	}
	record["consumer_join_observed_upper_bound_ns"] = time.Since(stopStarted).Nanoseconds()
	consumer := <-consumerResult
	record["consumer_status"], record["consumer_bytes"] = consumer.status, consumer.bytes
	record["consumer_error_types"] = map[string]string{"do": fmt.Sprintf("%T", consumer.doErr), "read": fmt.Sprintf("%T", consumer.readErr), "close": fmt.Sprintf("%T", consumer.closeErr)}
	if !httpGetStopRemeasureConsumerAccepted(getCtx, consumer) {
		t.Fatal("the stopped natural HTTP consumer ended with an unexpected status, error or body size")
	}
	httpGetStopRemeasureJoinRequests(t, f.ctx, f.app.hls)
	var playState string
	if err := f.pool.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id=$1", graph.playID).Scan(&playState); err != nil || playState != "Stopped" || session.ctx.Err() == nil {
		t.Fatal("running encoder Stop did not persist and cancel its exact playback lifetime")
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCleanup()
	closeStarted := time.Now()
	if err := f.app.hls.Close(cleanup); err != nil {
		t.Fatal("running encoder HLS Close retained an actual owner")
	}
	resources, ok := f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("running encoder manager has no read-only exact-scope observation")
	}
	usage, err := resources.ResourceUsage(cleanup, session.key.scope)
	if err != nil || usage != (transcode.ResourceUsage{}) || playbackStopAliasSourceFDs(t, fixture.source) != 0 {
		t.Fatal("joined running encoder retained a scoped resource or native source descriptor")
	}
	if _, err := os.Stat(filepath.Join(f.app.cfg.Transcoding.CacheDirectory, process.jobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the retired encoder's exact workspace was not removed")
	}
	if err := f.app.Close(cleanup); err != nil {
		t.Fatal("running encoder Store and dependency Close did not actually join")
	}
	record["store_and_dependency_join_ns"] = time.Since(closeStarted).Nanoseconds()
	record["stop_to_final_close_observed_upper_bound_ns"] = time.Since(stopStarted).Nanoseconds()
	processAfter := media.GetProcessCapacityStats()
	if f.pool.Stat().AcquiredConns() != 0 || f.control.Stat().AcquiredConns() != 0 || len(f.app.streamSlots) != 0 || len(f.app.hls.slots) != 0 ||
		playbackStopAliasSourceFDs(t, fixture.source) != 0 || processAfter.Active != 0 || processAfter.Background != 0 || processAfter.Queued != 0 || processAfter.RetirementUnknown != processBefore.RetirementUnknown {
		t.Fatal("closed running encoder fixture retained a loan, slot, descriptor or media-process charge")
	}
	record["data_pool"], record["control_pool"] = httpGetStopRemeasurePoolDelta(dataBefore, hlsProfileCounters(f.pool.Stat())), httpGetStopRemeasurePoolDelta(controlBefore, hlsProfileCounters(f.control.Stat()))
	record["process_capacity_before"], record["process_capacity_after"] = processBefore, processAfter
	record["source_device_inode_fds"], record["exact_scope_resource_usage"] = 0, usage
	record["timing_contract"] = "one live paced software encoder per fresh run; response is TCP Do/body-close time; exit and reap/group observations run concurrently from Stop dispatch, with 1ms polling and recorder cost included as upper bounds; consumer joins without client cancellation; HLS/Store Close is separately timed after retirement; three samples per variant support min/median/max, not stable p99"
}
