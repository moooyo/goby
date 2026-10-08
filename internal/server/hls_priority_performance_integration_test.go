//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const hlsPriorityProfileHeader = "X-Goby-HLS-Profile"
const hlsPriorityProfileOrdinalHeader = "X-Goby-HLS-Profile-Ordinal"

type hlsPriorityProfileContextKey struct{}
type hlsPriorityProfileRequestContextKey struct{}

type hlsPriorityProfileRequestFacts struct {
	playbackLocks, finalPlaybackReads, sourceReads atomic.Int64
	phaseTiming                                    *hlsPhaseTimingRequest
}

// Candidate-only test files may opt in to the production capacity partition.
// These nil baseline hooks preserve the original sixteen-connection pool and
// never name an optimized production API in this shared profile.
var hlsProfileDataConfigurationHook func(*pgxpool.Config)
var hlsProfileApplicationOptionsHook func(*testing.T, context.Context, *pgxpool.Pool) []Option
var hlsProfileControlStatHook func(*Server) *pgxpool.Stat

func hlsProfileSourceAdmissionCounters(app *Server) map[string]uint64 {
	if profiler, ok := any(app.library).(interface{ MediaSourceAdmissionProfile() map[string]uint64 }); ok {
		return profiler.MediaSourceAdmissionProfile()
	}
	return nil
}

func hlsProfileSourceAdmissionDelta(before, after map[string]uint64) map[string]uint64 {
	if after == nil {
		return nil
	}
	delta := make(map[string]uint64, len(after))
	for name, value := range after {
		if name == "io_measurement_version" {
			// This labels the span contract, rather than an accumulating counter.
			delta[name] = value
			continue
		}
		delta[name] = value - before[name]
	}
	return delta
}

func TestHLSProfileSourceAdmissionDeltaPreservesMeasurementVersion(t *testing.T) {
	before := map[string]uint64{"io_measurement_version": 2, "io_held_ns": 100, "try_misses": 4}
	after := map[string]uint64{"io_measurement_version": 2, "io_held_ns": 160, "try_misses": 7}
	delta := hlsProfileSourceAdmissionDelta(before, after)
	if delta["io_measurement_version"] != 2 || delta["io_held_ns"] != 60 || delta["try_misses"] != 3 {
		t.Fatalf("admission profile lost its span version or counter deltas: %v", delta)
	}
	if baseline := hlsProfileSourceAdmissionDelta(nil, nil); baseline != nil {
		t.Fatalf("a baseline without admission profiling produced metadata: %v", baseline)
	}
}

func hlsProfileConfigureData(configuration *pgxpool.Config) {
	if hlsProfileDataConfigurationHook != nil {
		hlsProfileDataConfigurationHook(configuration)
	}
}

func hlsProfileOptions(t *testing.T, ctx context.Context, data *pgxpool.Pool) []Option {
	if hlsProfileApplicationOptionsHook == nil {
		return nil
	}
	return hlsProfileApplicationOptionsHook(t, ctx, data)
}

type hlsProfileDatabaseCapacity struct {
	DataMax, DataUsable, ControlMax, ApplicationMax int32
}

func hlsProfileDatabaseCapacities(data *pgxpool.Pool, app *Server) hlsProfileDatabaseCapacity {
	capacity := hlsProfileDatabaseCapacity{DataMax: data.Config().MaxConns, DataUsable: data.Config().MaxConns - 1,
		ApplicationMax: data.Config().MaxConns}
	if hlsProfileControlStatHook != nil {
		if control := hlsProfileControlStatHook(app); control != nil {
			capacity.ControlMax = control.MaxConns()
			capacity.ApplicationMax += control.MaxConns()
		}
	}
	return capacity
}

type hlsProfilePoolCounters struct {
	Acquires, EmptyAcquires, CanceledAcquires, NewConnections int64
	AcquireDuration, EmptyWait                                time.Duration
}

func hlsProfileCounters(snapshot *pgxpool.Stat) hlsProfilePoolCounters {
	if snapshot == nil {
		return hlsProfilePoolCounters{}
	}
	return hlsProfilePoolCounters{Acquires: snapshot.AcquireCount(), EmptyAcquires: snapshot.EmptyAcquireCount(),
		CanceledAcquires: snapshot.CanceledAcquireCount(), NewConnections: snapshot.NewConnsCount(),
		AcquireDuration: snapshot.AcquireDuration(), EmptyWait: snapshot.EmptyAcquireWaitTime()}
}

func hlsProfileControlCounters(app *Server) hlsProfilePoolCounters {
	if hlsProfileControlStatHook == nil {
		return hlsProfilePoolCounters{}
	}
	return hlsProfileCounters(hlsProfileControlStatHook(app))
}

func hlsProfileLogDatabaseDelta(t *testing.T, phase string, application bool, callers int, capacity hlsProfileDatabaseCapacity,
	dataBefore, dataAfter, controlBefore, controlAfter hlsProfilePoolCounters) {
	t.Helper()
	t.Logf("hls_profile_database phase=%s application_key=%t callers=%d data_max=%d data_usable=%d control_max=%d application_max=%d data_acquires=%d data_empty_acquires=%d data_canceled_acquires=%d data_acquire_duration=%s data_empty_wait=%s data_new_connections=%d control_acquires=%d control_empty_acquires=%d control_canceled_acquires=%d control_acquire_duration=%s control_empty_wait=%s control_new_connections=%d",
		phase, application, callers, capacity.DataMax, capacity.DataUsable, capacity.ControlMax, capacity.ApplicationMax,
		dataAfter.Acquires-dataBefore.Acquires, dataAfter.EmptyAcquires-dataBefore.EmptyAcquires, dataAfter.CanceledAcquires-dataBefore.CanceledAcquires,
		dataAfter.AcquireDuration-dataBefore.AcquireDuration, dataAfter.EmptyWait-dataBefore.EmptyWait, dataAfter.NewConnections-dataBefore.NewConnections,
		controlAfter.Acquires-controlBefore.Acquires, controlAfter.EmptyAcquires-controlBefore.EmptyAcquires, controlAfter.CanceledAcquires-controlBefore.CanceledAcquires,
		controlAfter.AcquireDuration-controlBefore.AcquireDuration, controlAfter.EmptyWait-controlBefore.EmptyWait, controlAfter.NewConnections-controlBefore.NewConnections)
}

type hlsPriorityProfileCounts struct {
	sql, playbackReads, sourceReads, entityProjections atomic.Int64
	finalPlaybackReads, freshRequests                  atomic.Int64
	requests, active                                   atomic.Int64
	entered                                            chan struct{}
	enteredOnce                                        sync.Once
	stopDiagnosticGate                                 atomic.Pointer[hlsStopDiagnosticGateEvidence]
	phaseTiming                                        atomic.Pointer[hlsPhaseTimingCase]
}

type hlsPriorityProfileCase struct {
	steady, stoppingGET, stopped hlsPriorityProfileCounts
	stopOverlap                  atomic.Int64
}

type hlsPriorityProfileTracer struct {
	current    atomic.Pointer[hlsPriorityProfileCase]
	warm       atomic.Pointer[hlsPriorityProfileWarmGate]
	pgObserver atomic.Pointer[hlsPGPhaseObserver]
}

type hlsPriorityProfileWarmGate struct {
	arrivals       atomic.Int64
	ready, release chan struct{}
}

func (trace *hlsPriorityProfileTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	recordHLSProfileSQL(ctx, data.SQL, "query_start")
	ctx = hlsPhaseTimingQueryStart(ctx, connection, data.SQL)
	if observer := trace.pgObserver.Load(); observer != nil {
		ctx = observer.queryStart(ctx, connection, data.SQL)
	}
	return ctx
}

func recordHLSProfileSQL(ctx context.Context, sql, callback string) {
	counts, _ := ctx.Value(hlsPriorityProfileContextKey{}).(*hlsPriorityProfileCounts)
	if counts == nil {
		return
	}
	request, _ := ctx.Value(hlsPriorityProfileRequestContextKey{}).(*hlsPriorityProfileRequestFacts)
	counts.sql.Add(1)
	statement := strings.ToLower(strings.TrimSpace(sql))
	if strings.Contains(statement, "from play_sessions") && (strings.HasSuffix(statement, "for update") || strings.HasSuffix(statement, "for share")) {
		counts.playbackReads.Add(1)
		if request != nil {
			request.playbackLocks.Add(1)
		}
		if counts.entered != nil {
			// Observe natural database work without holding a lock or delaying
			// a request to manufacture contention with the stop report.
			counts.enteredOnce.Do(func() {
				hlsStopDiagnosticMarkGate(counts, callback)
				close(counts.entered)
			})
		}
	}
	if strings.Contains(statement, "from play_sessions") && strings.Contains(statement, "clock_timestamp()") &&
		!strings.HasSuffix(statement, "for share") && !strings.HasSuffix(statement, "for update") {
		counts.finalPlaybackReads.Add(1)
		if request != nil {
			request.finalPlaybackReads.Add(1)
		}
	}
	if strings.Contains(statement, "and i.type in ('movie', 'episode', 'video', 'audio')") {
		counts.sourceReads.Add(1)
		if request != nil {
			request.sourceReads.Add(1)
		}
		if strings.Contains(statement, "jsonb_agg") && strings.Contains(statement, "catalog_entities") {
			counts.entityProjections.Add(1)
		}
	}
}

func (*hlsPriorityProfileTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	hlsPhaseTimingQueryEnd(ctx, hlsPGObserverEndFailed(ctx, data))
	hlsPGObserverQueryEnd(ctx, data)
}

func (trace *hlsPriorityProfileTracer) TraceBatchStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	ctx = hlsPhaseTimingBatchStart(ctx, connection, data.Batch)
	if observer := trace.pgObserver.Load(); observer != nil {
		observer.batchStart(ctx, connection, data.Batch)
	}
	return ctx
}

func (*hlsPriorityProfileTracer) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	recordHLSProfileSQL(ctx, data.SQL, "batch_result_completion")
	hlsPhaseTimingBatchQuery(ctx, data.SQL, data.Err != nil)
}

func (*hlsPriorityProfileTracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	hlsPhaseTimingBatchEnd(ctx, data.Err != nil)
}

func (trace *hlsPriorityProfileTracer) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(hlsPriorityProfileHeader) == "warm" {
			if gate := trace.warm.Load(); gate != nil {
				// The warm-up alone waits for 32 separate live HTTP connections.
				// Releasing them together also exercises the real cached handler.
				if gate.arrivals.Add(1) == 32 {
					close(gate.ready)
				}
				select {
				case <-gate.release:
				case <-r.Context().Done():
					return
				}
			}
		}
		current := trace.current.Load()
		var counts *hlsPriorityProfileCounts
		if current != nil {
			switch r.Header.Get(hlsPriorityProfileHeader) {
			case "steady":
				counts = &current.steady
			case "stop_get":
				counts = &current.stoppingGET
			case "stopped":
				counts = &current.stopped
				current.stopOverlap.Store(current.stoppingGET.active.Load())
			}
		}
		if counts != nil {
			ordinal := counts.requests.Add(1)
			counts.active.Add(1)
			defer counts.active.Add(-1)
			request := &hlsPriorityProfileRequestFacts{}
			if hlsPhaseTimingEnabled() {
				profile := counts.phaseTiming.Load()
				if profile == nil {
					candidate := &hlsPhaseTimingCase{statistics: make(map[string]hlsPhaseTimingStatistic)}
					counts.phaseTiming.CompareAndSwap(nil, candidate)
					profile = counts.phaseTiming.Load()
				}
				started := time.Now()
				if hlsStopDiagnosticEnabled() {
					w.Header().Set(hlsPriorityProfileOrdinalHeader, strconv.FormatInt(ordinal, 10))
				}
				group := "steady_get"
				if counts == &current.stoppingGET {
					group = "ping_or_stop_get"
				} else if counts == &current.stopped {
					group = "stopped"
				}
				request.phaseTiming = &hlsPhaseTimingRequest{started: started, group: group, summary: hlsPhaseTimingRequestSummary{
					Ordinal: ordinal, StartedAt: started.UTC(), Statistics: make(map[string]hlsPhaseTimingStatistic), Events: make([]hlsPhaseTimingEvent, 0, 64)}}
				defer request.phaseTiming.finish(profile)
			}
			defer func() {
				if request.playbackLocks.Load() >= 2 && request.finalPlaybackReads.Load() >= 2 && request.sourceReads.Load() >= 2 {
					counts.freshRequests.Add(1)
				}
			}()
			ctx := context.WithValue(r.Context(), hlsPriorityProfileContextKey{}, counts)
			r = r.WithContext(context.WithValue(ctx, hlsPriorityProfileRequestContextKey{}, request))
		}
		next.ServeHTTP(w, r)
	})
}

type hlsPriorityProfileResponse struct {
	duration       time.Duration
	status         int
	data           []byte
	err            error
	requestOrdinal int64
}

// Build requests before timing. Each sample includes Do, the complete body read
// and Body.Close, so connection reuse and all HTTP authorization work are real.
// Never print a transport error string: it can contain the credential URL.
func hlsPriorityProfileRequest(ctx context.Context, client *http.Client, method, target, phase string, body []byte, headers http.Header) hlsPriorityProfileResponse {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return hlsPriorityProfileResponse{err: err}
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if phase != "" {
		request.Header.Set(hlsPriorityProfileHeader, phase)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	started := time.Now()
	hlsStopDiagnosticClientStart(ctx, phase, started)
	response, err := client.Do(request)
	if err != nil {
		elapsed := time.Since(started)
		hlsStopDiagnosticClientEnd(ctx, phase, started.Add(elapsed))
		return hlsPriorityProfileResponse{duration: elapsed, err: err}
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 17<<20))
	closeErr := response.Body.Close()
	elapsed := time.Since(started)
	hlsStopDiagnosticClientEnd(ctx, phase, started.Add(elapsed))
	if readErr == nil {
		readErr = closeErr
	}
	if len(data) >= 17<<20 && readErr == nil {
		readErr = fmt.Errorf("HLS profile response exceeded the fixture limit")
	}
	// Extract the test-only association after the original client timing window.
	// A retained server detail can now be matched without guessing arrival order.
	var ordinal int64
	if value := response.Header.Get(hlsPriorityProfileOrdinalHeader); value != "" {
		var ordinalErr error
		ordinal, ordinalErr = strconv.ParseInt(value, 10, 64)
		if ordinalErr != nil && readErr == nil {
			readErr = errors.New("invalid HLS profile request ordinal")
		}
	}
	return hlsPriorityProfileResponse{duration: elapsed, status: response.StatusCode, data: data, err: readErr, requestOrdinal: ordinal}
}

func hlsPriorityProfileClient(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{MaxIdleConns: 64, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64,
		IdleConnTimeout: 5 * time.Minute, DisableCompression: true}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func hlsPriorityProfileWave(ctx context.Context, client *http.Client, target, phase string, callers, rounds int) []hlsPriorityProfileResponse {
	start := make(chan struct{})
	results := make(chan []hlsPriorityProfileResponse, callers)
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			local := make([]hlsPriorityProfileResponse, 0, rounds)
			<-start
			for range rounds {
				local = append(local, hlsPriorityProfileRequest(ctx, client, http.MethodGet, target, phase, nil, nil))
			}
			results <- local
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var samples []hlsPriorityProfileResponse
	for local := range results {
		samples = append(samples, local...)
	}
	return samples
}

// Separate wire URLs keep each persisted credential/playback context intact.
// This test-only matrix spreads credential and playback rows together; its
// relative latency alone cannot isolate either lock's individual contribution.
func hlsPriorityProfileManyTargetWave(ctx context.Context, client *http.Client, targets []string, phase string, callers, rounds int) []hlsPriorityProfileResponse {
	start := make(chan struct{})
	results := make(chan []hlsPriorityProfileResponse, callers)
	var workers sync.WaitGroup
	for worker := range callers {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			local := make([]hlsPriorityProfileResponse, 0, rounds)
			<-start
			for range rounds {
				local = append(local, hlsPriorityProfileRequest(ctx, client, http.MethodGet, targets[worker%len(targets)], phase, nil, nil))
			}
			results <- local
		}(worker)
	}
	close(start)
	workers.Wait()
	close(results)
	var samples []hlsPriorityProfileResponse
	for local := range results {
		samples = append(samples, local...)
	}
	return samples
}

func hlsPriorityProfileCheck(t *testing.T, samples []hlsPriorityProfileResponse, expected []byte) []time.Duration {
	t.Helper()
	want := sha256.Sum256(expected)
	durations := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample.err != nil || sample.status != http.StatusOK || len(sample.data) != len(expected) || sha256.Sum256(sample.data) != want {
			t.Fatalf("cached HLS response changed: status=%d bytes=%d error_type=%T", sample.status, len(sample.data), sample.err)
		}
		durations = append(durations, sample.duration)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations
}

func hlsPriorityProfilePercentile(samples []time.Duration, percentile int) time.Duration {
	return samples[(len(samples)*percentile+99)/100-1]
}

func hlsPriorityProfileWaitIdle(t *testing.T, counts ...*hlsPriorityProfileCounts) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		idle := true
		for _, count := range counts {
			idle = idle && count.active.Load() == 0
		}
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("profile HTTP handlers did not finish their cleanup")
		}
		time.Sleep(time.Millisecond)
	}
}

// This file can be copied unchanged to the baseline checkout. It uses existing
// real HTTP/catalog/encoder fixtures and never names the optimized source API.
// A fixed 12-second source and one fixed cached segment test authorization and
// cancellation latency; byte counts must not be presented as playback capacity.
// Run paired profiles sequentially, excluding all other tests and workloads.
func TestHTTPCachedHLSPriorityPerformance(t *testing.T) {
	if os.Getenv("GOBY_HLS_PRIORITY_PERFORMANCE") != "1" {
		t.Skip("GOBY_HLS_PRIORITY_PERFORMANCE=1 enables the cached HLS priority profile")
	}
	h := newHLSHTTPFixture(t, 3*time.Minute)
	key := applicationMediaIssueKeys(t, h.f, h.accounts.admin.headers.Get("X-Emby-Token"))[0]
	// Replace the complete application only after joining its workers. Changing
	// identity/catalog pointers under live background workers would race. The
	// persisted schema, source, clients and runtime limits survive this restart.
	h.server.Close()
	if err := h.f.app.Close(h.f.ctx); err != nil {
		t.Fatal("close the initial HLS profile application")
	}
	trace := &hlsPriorityProfileTracer{}
	configuration := h.f.pool.Config()
	configuration.MaxConns = 16
	configuration.ConnConfig.Tracer = trace
	hlsProfileConfigureData(configuration)
	pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
	if err != nil {
		t.Fatal("create the traced HLS profile pool")
	}
	t.Cleanup(pool.Close)
	users := identity.New(pool)
	profileOptions := hlsProfileOptions(t, h.f.ctx, pool)
	app, err := New(h.f.ctx, h.f.cfg, pool, users, h.f.log, "cached-hls-priority-profile", profileOptions...)
	if err != nil {
		t.Fatal("restart the real HLS application with its traced request pool")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error("close the traced HLS profile application")
		}
	})
	h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
	server := httptest.NewServer(trace.wrap(h.f.handler))
	h.server = server
	t.Cleanup(server.Close)
	client, stopClient := hlsPriorityProfileClient(t), hlsPriorityProfileClient(t)
	capacity := hlsProfileDatabaseCapacities(pool, app)
	// Library ownership already reserves one physical session for this app.
	// Establish the remaining request sessions without waiting for that owner.
	// The original fixture pool remains only for unmarked assertions.
	requestConnections := configuration.MaxConns - 1
	held := make([]*pgxpool.Conn, 0, requestConnections)
	for range requestConnections {
		connection, err := pool.Acquire(h.f.ctx)
		if err != nil {
			for _, acquired := range held {
				acquired.Release()
			}
			t.Fatal("warm the shared HLS request database pool")
		}
		held = append(held, connection)
	}
	for _, connection := range held {
		connection.Release()
	}
	for _, application := range []bool{false, true} {
		for _, callers := range []int{1, 8, 32} {
			t.Run(fmt.Sprintf("application-%t/callers-%d", application, callers), func(t *testing.T) {
				var graph hlsHTTPGraph
				headers := h.accounts.viewer.headers
				if application {
					graph = applicationMediaRealGraph(t, h, key, h.accounts.viewer.userID)
					headers = key.headers
				} else {
					graph = h.graph(t, h.accounts.viewer, 0)
				}
				var expected []byte
				for index, child := range graph.children {
					response := h.request(t, http.MethodGet, child, nil, nil)
					expectHLSHTTPStatus(t, response, http.StatusOK)
					if index == 0 {
						expected = response.body
					}
				}
				if len(expected) == 0 {
					t.Fatal("the cached segment has no fixed representation")
				}
				// The last published segment can precede process/repository cleanup.
				// Require terminal completed jobs before either warm-up or sampling.
				deadline := time.Now().Add(10 * time.Second)
				for {
					var jobs, complete int
					if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*),count(*) FILTER (WHERE state='completed') FROM encoding_jobs WHERE play_session_id=$1`, graph.playID).Scan(&jobs, &complete); err != nil {
						t.Fatal("read the cached HLS encoding completion")
					}
					if jobs > 0 && jobs == complete {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("HLS encoding was not completed before hot-cache measurement")
					}
					time.Sleep(5 * time.Millisecond)
				}
				target := h.server.URL + graph.children[0]
				// A full 32-client warm-up excludes HTTP connection creation even in
				// the smaller measured cases. Stable metadata follows captured URLs.
				gate := &hlsPriorityProfileWarmGate{ready: make(chan struct{}), release: make(chan struct{})}
				trace.warm.Store(gate)
				warmed := make(chan []hlsPriorityProfileResponse, 1)
				go func() {
					warmed <- hlsPriorityProfileWave(h.f.ctx, client, target, "warm", 32, 1)
				}()
				select {
				case <-gate.ready:
				case <-h.f.ctx.Done():
					close(gate.release)
					t.Fatal("the cached HLS warm-up did not establish 32 HTTP connections")
				}
				close(gate.release)
				hlsPriorityProfileCheck(t, <-warmed, expected)
				app.hls.requests.Wait()
				trace.warm.Store(nil)
				warmStop := hlsPriorityProfileRequest(h.f.ctx, stopClient, http.MethodGet, h.server.URL+"/emby/System/Info", "", nil, headers)
				if warmStop.err != nil || warmStop.status != http.StatusOK {
					t.Fatal("warm the independent stop HTTP connection")
				}
				current := &hlsPriorityProfileCase{}
				current.stoppingGET.entered = make(chan struct{})
				trace.current.Store(current)
				const requests = 256
				dataBefore, controlBefore := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				started := time.Now()
				steady := hlsPriorityProfileWave(h.f.ctx, client, target, "steady", callers, requests/callers)
				elapsed := time.Since(started)
				latencies := hlsPriorityProfileCheck(t, steady, expected)
				hlsPriorityProfileWaitIdle(t, &current.steady)
				dataAfter, controlAfter := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				hlsProfileLogDatabaseDelta(t, "cached-steady", application, callers, capacity, dataBefore, dataAfter, controlBefore, controlAfter)
				if current.steady.requests.Load() != requests || current.steady.sourceReads.Load() != 2*requests || current.steady.playbackReads.Load() != 2*requests {
					t.Fatalf("the HTTP samples bypassed full cached-HLS authorization: requests=%d source_reads=%d play_reads=%d", current.steady.requests.Load(), current.steady.sourceReads.Load(), current.steady.playbackReads.Load())
				}
				app.hls.mu.Lock()
				session := app.hls.sessions[graph.hlsID]
				app.hls.mu.Unlock()
				if session == nil {
					t.Fatal("the cached HLS registration disappeared before Stopped")
				}
				session.mu.Lock()
				producers := append([]hlsProducer(nil), session.producers...)
				session.mu.Unlock()
				stopBody, err := json.Marshal(map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0})
				if err != nil {
					t.Fatal("encode the bounded stop report")
				}
				loaded := make(chan []hlsPriorityProfileResponse, 1)
				stopDataBefore, stopControlBefore := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				go func() {
					loaded <- hlsPriorityProfileWave(h.f.ctx, client, target, "stop_get", callers, 1)
				}()
				select {
				case <-current.stoppingGET.entered:
				case <-h.f.ctx.Done():
					t.Fatal("the stop burst did not reach real play-session validation")
				}
				stopStarted := time.Now()
				stopped := hlsPriorityProfileRequest(h.f.ctx, stopClient, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Stopped", "stopped", stopBody, headers)
				if stopped.err != nil || stopped.status != http.StatusNoContent {
					t.Fatalf("concurrent Stopped report failed: status=%d error_type=%T", stopped.status, stopped.err)
				}
				if current.stopOverlap.Load() == 0 {
					t.Fatal("Stopped did not overlap a live cached GET; this sample cannot measure cancellation under load")
				}
				burst := <-loaded
				hlsPriorityProfileWaitIdle(t, &current.stoppingGET, &current.stopped)
				drained := time.Since(stopStarted)
				hlsProfileLogDatabaseDelta(t, "cached-stop", application, callers, capacity, stopDataBefore, hlsProfileCounters(pool.Stat()), stopControlBefore, hlsProfileControlCounters(app))
				ok, missing, limited, interrupted := 0, 0, 0, 0
				for _, sample := range burst {
					switch {
					case sample.err != nil:
						var networkError net.Error
						if h.f.ctx.Err() != nil || errors.Is(sample.err, context.Canceled) || errors.Is(sample.err, context.DeadlineExceeded) ||
							errors.As(sample.err, &networkError) && networkError.Timeout() ||
							!(errors.Is(sample.err, io.EOF) || errors.Is(sample.err, io.ErrUnexpectedEOF) || errors.Is(sample.err, net.ErrClosed) ||
								errors.Is(sample.err, syscall.ECONNRESET) || errors.Is(sample.err, syscall.EPIPE)) {
							t.Fatalf("unexpected stop-burst transport failure: error_type=%T", sample.err)
						}
						interrupted++
					case sample.status == http.StatusOK:
						hlsPriorityProfileCheck(t, []hlsPriorityProfileResponse{sample}, expected)
						ok++
					case sample.status == http.StatusNotFound:
						missing++
					case sample.status == http.StatusTooManyRequests:
						limited++
					default:
						t.Fatalf("unexpected in-flight cancellation response: status=%d", sample.status)
					}
				}
				late := hlsPriorityProfileRequest(h.f.ctx, client, http.MethodGet, target, "", nil, nil)
				if late.err != nil || late.status != http.StatusNotFound {
					t.Fatal("a fresh request reused cached HLS bytes after Stopped completed")
				}
				var state string
				if err := h.f.pool.QueryRow(h.f.ctx, `SELECT state FROM play_sessions WHERE id=$1`, graph.playID).Scan(&state); err != nil || state != "Stopped" {
					t.Fatal("the concurrent HTTP report did not persist Stopped")
				}
				app.hls.mu.Lock()
				registered := app.hls.sessions[graph.hlsID] != nil
				app.hls.mu.Unlock()
				var activeJobs int
				if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*) FROM encoding_jobs WHERE auth_session_id=$1 AND play_session_id=$2 AND state IN ('queued','running')`, session.key.scope.AuthSessionID, graph.playID).Scan(&activeJobs); err != nil || registered || activeJobs != 0 {
					t.Fatal("Stopped retained its exact owner's HLS registration or active encoding job")
				}
				if session.ctx.Err() == nil {
					t.Fatal("Stopped retained the cached HLS session lifetime")
				}
				policy := app.playbackPolicyGate()
				policy.mu.Lock()
				retainedPolicy := false
				for _, lease := range policy.leases {
					retainedPolicy = retainedPolicy || lease.scope == session.key.scope
				}
				policy.mu.Unlock()
				if retainedPolicy {
					t.Fatal("Stopped retained the cached HLS delivery quota lease")
				}
				for _, producer := range producers {
					handle, err := app.hls.manager.TryOpen(session.key.scope, producer.id, "segment-000000.ts")
					if handle != nil {
						_ = handle.Close()
					}
					if handle != nil || !errors.Is(err, transcode.ErrJobCancelled) && !errors.Is(err, transcode.ErrJobNotFound) {
						t.Fatal("Stopped failed to fence the completed producer's cached output")
					}
				}
				trace.current.Store(nil)
				t.Logf("cached_hls_priority application_key=%t callers=%d requests=%d segment_bytes=%d request_pool_max=%d request_pool_usable=%d assertion_pool_max=%d elapsed=%s p95=%s p99=%s get_sql=%d get_play_reads=%d get_source_reads=%d get_entity_projections=%d stop_response=%s stop_handler_drain=%s stop_sql=%d stop_get_sql=%d stop_get_source_reads=%d stop_overlap=%d burst_ok=%d burst_not_found=%d burst_limited=%d burst_interrupted=%d",
					application, callers, requests, len(expected), configuration.MaxConns, requestConnections, h.f.pool.Config().MaxConns, elapsed,
					hlsPriorityProfilePercentile(latencies, 95), hlsPriorityProfilePercentile(latencies, 99), current.steady.sql.Load(),
					current.steady.playbackReads.Load(), current.steady.sourceReads.Load(), current.steady.entityProjections.Load(),
					stopped.duration, drained, current.stopped.sql.Load(), current.stoppingGET.sql.Load(), current.stoppingGET.sourceReads.Load(),
					current.stopOverlap.Load(), ok, missing, limited, interrupted)
			})
		}
	}
}
