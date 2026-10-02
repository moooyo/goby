//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type playbackMediaHTTPProfileTrace struct {
	priority        hlsPriorityProfileTracer
	begins, commits atomic.Int64
}

func (trace *playbackMediaHTTPProfileTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.priority.TraceQueryStart(ctx, connection, data)
	if counts, _ := ctx.Value(hlsPriorityProfileContextKey{}).(*hlsPriorityProfileCounts); counts != nil {
		statement := strings.ToLower(strings.TrimSpace(data.SQL))
		if strings.HasPrefix(statement, "begin") {
			trace.begins.Add(1)
		}
		if statement == "commit" {
			trace.commits.Add(1)
		}
	}
	return ctx
}

func (trace *playbackMediaHTTPProfileTrace) TraceQueryEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.priority.TraceQueryEnd(ctx, connection, data)
}

func (trace *playbackMediaHTTPProfileTrace) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	return trace.priority.TraceAcquireStart(ctx, pool, data)
}

func (trace *playbackMediaHTTPProfileTrace) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	trace.priority.TraceAcquireEnd(ctx, pool, data)
}

func (trace *playbackMediaHTTPProfileTrace) TraceBatchStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	return trace.priority.TraceBatchStart(ctx, connection, data)
}

func (trace *playbackMediaHTTPProfileTrace) TraceBatchQuery(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchQueryData) {
	trace.priority.TraceBatchQuery(ctx, connection, data)
}

func (trace *playbackMediaHTTPProfileTrace) TraceBatchEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchEndData) {
	trace.priority.TraceBatchEnd(ctx, connection, data)
}

// Copy this complete file unchanged to the baseline checkout. Measurements use
// the real HTTP handler, token middleware, both authorization stages, rooted
// opens and completed transcode outputs. Process allocation deltas include the
// HTTP client/server and any natural maintenance during the bounded interval.
func TestHTTPCachedHLSAuthorizationPerformance(t *testing.T) {
	if os.Getenv("GOBY_HLS_AUTHORIZATION_PERFORMANCE") != "1" {
		t.Skip("GOBY_HLS_AUTHORIZATION_PERFORMANCE=1 enables cached HLS authorization measurements")
	}
	h := newHLSHTTPFixture(t, 3*time.Minute)
	issued := applicationMediaIssueKeys(t, h.f, h.accounts.admin.headers.Get("X-Emby-Token"))
	key := issued[0]
	keyMode := os.Getenv("GOBY_HLS_PHASE_KEY_MODE")
	if keyMode == "" {
		keyMode = "same_key"
	}
	if keyMode != "same_key" && (keyMode != "many_keys" || !hlsPhaseTimingEnabled()) {
		t.Fatal("GOBY_HLS_PHASE_KEY_MODE must be same_key, or many_keys with GOBY_HLS_PHASE_TIMING=1")
	}
	profileKeys := []applicationMediaKey{key}
	if keyMode == "many_keys" {
		// Shared-device display metadata is fixed across all credentials. The
		// diagnostic matrix must not manufacture metadata churn as another cause.
		ids := []string{issued[0].principal.SessionID, issued[1].principal.SessionID}
		for _, statement := range []string{"UPDATE sessions SET client_name=$2 WHERE id=ANY($1::text[])", "UPDATE application_key_clients SET client_name=$2 WHERE credential_id=ANY($1::text[])"} {
			if _, err := h.f.pool.Exec(h.f.ctx, statement, ids, key.key.AppName); err != nil {
				t.Fatal("normalize fixed diagnostic application metadata")
			}
		}
		actor, err := h.f.users.ResolveEmby(h.f.ctx, h.accounts.admin.headers.Get("X-Emby-Token"))
		if err != nil {
			t.Fatal("resolve the diagnostic fixture's key issuer")
		}
		profileKeys = append(profileKeys, issued[1])
		for len(profileKeys) < 8 {
			created, err := h.f.users.CreateApplicationKey(h.f.ctx, actor, key.key.AppName, "127.0.0.1", key.key.Client)
			if err != nil {
				t.Fatal("create a distinct fixed-metadata diagnostic credential")
			}
			principal, err := h.f.users.ResolveEmby(h.f.ctx, created.Token)
			if err != nil {
				t.Fatal("resolve a distinct diagnostic credential")
			}
			profileKeys = append(profileKeys, applicationMediaKey{key: created, principal: principal, headers: http.Header{"X-Emby-Token": {created.Token}}})
		}
	}
	h.server.Close()
	if err := h.f.app.Close(h.f.ctx); err != nil {
		t.Fatal("close the initial authorization fixture")
	}
	trace := &playbackMediaHTTPProfileTrace{}
	configuration := h.f.pool.Config()
	configuration.MaxConns = 16
	configuration.ConnConfig.Tracer = trace
	hlsProfileConfigureData(configuration)
	pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	profileOptions := hlsProfileOptions(t, h.f.ctx, pool)
	users := identity.New(pool)
	app, err := New(h.f.ctx, h.f.cfg, pool, users, h.f.log, "cached-hls-authorization-profile", profileOptions...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
	h.server = httptest.NewServer(trace.priority.wrap(h.f.handler))
	t.Cleanup(h.server.Close)
	client := hlsPriorityProfileClient(t)
	capacity := hlsProfileDatabaseCapacities(pool, app)
	for _, application := range []bool{false, true} {
		for _, callers := range []int{1, 8, 32} {
			t.Run(fmt.Sprintf("application-%t/callers-%d", application, callers), func(t *testing.T) {
				graph := hlsHTTPGraph{}
				headers := h.accounts.viewer.headers
				if application {
					graph, headers = applicationMediaRealGraph(t, h, key, h.accounts.viewer.userID), key.headers
				} else {
					graph = h.graph(t, h.accounts.viewer, 0)
				}
				graphs := []hlsHTTPGraph{graph}
				if application && keyMode == "many_keys" {
					for _, sibling := range profileKeys[1:] {
						graphs = append(graphs, applicationMediaRealGraph(t, h, sibling, h.accounts.viewer.userID))
					}
				}
				var expected []byte
				for graphIndex, cachedGraph := range graphs {
					for index, child := range cachedGraph.children {
						response := h.request(t, http.MethodGet, child, nil, nil)
						expectHLSHTTPStatus(t, response, http.StatusOK)
						if index == 0 {
							if graphIndex == 0 {
								expected = response.body
							} else {
								hlsPriorityProfileCheck(t, []hlsPriorityProfileResponse{{status: response.status, data: response.body}}, expected)
							}
						}
					}
					deadline := time.Now().Add(10 * time.Second)
					for {
						var jobs, completed int
						if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*),count(*) FILTER (WHERE state='completed') FROM encoding_jobs WHERE play_session_id=$1", cachedGraph.playID).Scan(&jobs, &completed); err != nil {
							t.Fatal(err)
						}
						if jobs > 0 && jobs == completed {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("the authorization profile did not obtain completed cache output")
						}
						time.Sleep(5 * time.Millisecond)
					}
				}
				target := h.server.URL + graph.children[0]
				targets := make([]string, 0, len(graphs))
				for _, cachedGraph := range graphs {
					targets = append(targets, h.server.URL+cachedGraph.children[0])
				}
				gate := &hlsPriorityProfileWarmGate{ready: make(chan struct{}), release: make(chan struct{})}
				trace.priority.warm.Store(gate)
				warmed := make(chan []hlsPriorityProfileResponse, 1)
				go func() {
					if len(targets) > 1 {
						warmed <- hlsPriorityProfileManyTargetWave(h.f.ctx, client, targets, "warm", 32, 1)
					} else {
						warmed <- hlsPriorityProfileWave(h.f.ctx, client, target, "warm", 32, 1)
					}
				}()
				select {
				case <-gate.ready:
				case <-h.f.ctx.Done():
					close(gate.release)
					t.Fatal("the authorization warm-up did not establish 32 live HTTP connections")
				}
				close(gate.release)
				hlsPriorityProfileCheck(t, <-warmed, expected)
				app.hls.requests.Wait()
				trace.priority.warm.Store(nil)
				current := &hlsPriorityProfileCase{}
				pgObserver := hlsStartPGPhaseObserver(t, h.f.ctx, pool, &trace.priority, map[string]any{"workload": "cached_hls", "application_key": application, "callers": callers, "key_mode": keyMode})
				trace.begins.Store(0)
				trace.commits.Store(0)
				trace.priority.current.Store(current)
				const requests = 256
				var before, after runtime.MemStats
				dataBefore, controlBefore := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				admissionBefore := hlsProfileSourceAdmissionCounters(app)
				runtime.ReadMemStats(&before)
				start := time.Now()
				var samples []hlsPriorityProfileResponse
				if len(targets) > 1 {
					samples = hlsPriorityProfileManyTargetWave(h.f.ctx, client, targets, "steady", callers, requests/callers)
				} else {
					samples = hlsPriorityProfileWave(h.f.ctx, client, target, "steady", callers, requests/callers)
				}
				elapsed := time.Since(start)
				hlsPriorityProfileWaitIdle(t, &current.steady)
				app.hls.requests.Wait()
				runtime.ReadMemStats(&after)
				pgObserver.finish(t)
				dataAfter, controlAfter := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
				admission := hlsProfileSourceAdmissionDelta(admissionBefore, hlsProfileSourceAdmissionCounters(app))
				hlsProfileLogDatabaseDelta(t, "authorization-steady", application, callers, capacity, dataBefore, dataAfter, controlBefore, controlAfter)
				latencies := hlsPriorityProfileCheck(t, samples, expected)
				trace.priority.current.Store(nil)
				// Global admission measurements describe workload cost, not proof
				// of request authority. Every successful tagged GET must separately
				// observe two source, play-lock and final-clock reads. Extra complete
				// authorization attempts remain counted instead of failing parity.
				if current.steady.requests.Load() != requests || current.steady.freshRequests.Load() != requests ||
					current.steady.playbackReads.Load() < 2*requests || current.steady.sourceReads.Load() < 2*requests ||
					current.steady.finalPlaybackReads.Load() < 2*requests || current.steady.entityProjections.Load() != 0 {
					t.Fatalf("the real HTTP profile omitted a fresh authorization stage: requests=%d plays=%d sources=%d entities=%d", current.steady.requests.Load(), current.steady.playbackReads.Load(), current.steady.sourceReads.Load(), current.steady.entityProjections.Load())
				}
				encoded, err := json.Marshal(map[string]any{
					"application_key": application, "callers": callers, "requests": requests, "segment_bytes": len(expected),
					"request_pool_max": configuration.MaxConns, "data_pool_max": capacity.DataMax, "data_pool_usable": capacity.DataUsable,
					"control_pool_max": capacity.ControlMax, "application_pool_max": capacity.ApplicationMax, "elapsed_ns": elapsed.Nanoseconds(),
					"p95_ns": hlsPriorityProfilePercentile(latencies, 95).Nanoseconds(), "p99_ns": hlsPriorityProfilePercentile(latencies, 99).Nanoseconds(),
					"sql": current.steady.sql.Load(), "begins": trace.begins.Load(), "commits": trace.commits.Load(),
					"play_lock_reads": current.steady.playbackReads.Load(), "source_reads": current.steady.sourceReads.Load(),
					"final_play_reads": current.steady.finalPlaybackReads.Load(), "fresh_requests": current.steady.freshRequests.Load(),
					"mallocs": after.Mallocs - before.Mallocs, "allocated_bytes": after.TotalAlloc - before.TotalAlloc,
					"source_admission": admission,
					"phase_key_mode":   keyMode, "phase_configured_keys": len(graphs), "phase_active_keys": min(callers, len(graphs)),
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("cached_hls_authorization_performance=%s", encoded)
				hlsProfileLogPhaseTiming(t, current, map[string]any{"workload": "cached_hls", "application_key": application, "callers": callers, "key_mode": keyMode, "configured_keys": len(graphs), "active_keys": min(callers, len(graphs))})
				for index, cachedGraph := range graphs {
					stopHeaders := headers
					if application {
						stopHeaders = profileKeys[index].headers
					}
					response := h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", map[string]any{"PlaySessionId": cachedGraph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, stopHeaders)
					expectHLSHTTPStatus(t, response, http.StatusNoContent)
				}
			})
		}
	}
}
