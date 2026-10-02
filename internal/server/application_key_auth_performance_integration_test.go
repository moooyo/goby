//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPApplicationKeyURLActivityTouchesOnlyRecoveredContext(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "Recovered media context")
	defaults, err := a.users.ResolveEmby(a.ctx, key.token)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{Name: "Media client", DeviceID: "media-device", Device: "Media Device", Version: "1"}
	bound, err := a.users.ResolveEmbyForClient(a.ctx, key.token, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, `INSERT INTO libraries (id, name, collection_type) VALUES ('activity-library', 'Activity Library', 'movies');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('activity-root', 'activity-library', '/synthetic/activity', '/synthetic', 'activity');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path)
		VALUES ('activity-item', 'activity-library', 'activity-root', 'Activity Movie', 'activity movie', 'Movie', '/synthetic/activity/movie.mp4', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at, client_correlated)
		VALUES ('activity-play', NULL, $1, $2, $3, 'activity-item', 'source_activity-item', 'Prepared', 90000000,
		clock_timestamp() + interval '1 hour', true)`, bound.SessionID, bound.ClientSessionID, bound.Client.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE application_key_clients SET last_seen_at = clock_timestamp() - interval '1 minute' WHERE credential_id = $1", bound.SessionID); err != nil {
		t.Fatal(err)
	}
	var defaultBefore string
	if err := a.pool.QueryRow(a.ctx, "SELECT to_jsonb(c)::text FROM application_key_clients c WHERE id = $1", defaults.ClientSessionID).Scan(&defaultBefore); err != nil {
		t.Fatal(err)
	}
	var observed identity.Principal
	handler := a.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Context().Value(principalKey).(identity.Principal)
		w.WriteHeader(http.StatusNoContent)
	})
	target := "/emby/Videos/activity-item/stream?api_key=" + url.QueryEscape(key.token) + "&PlaySessionId=activity-play"
	for range 2 {
		r := httptest.NewRequest(http.MethodGet, target, nil).WithContext(a.ctx)
		r.RemoteAddr = "192.0.2.32:54321"
		response := httptest.NewRecorder()
		handler(response, r)
		if response.Code != http.StatusNoContent || observed.ClientSessionID != bound.ClientSessionID || observed.Client != bound.Client {
			t.Fatalf("media URL did not authenticate its recovered client: status=%d", response.Code)
		}
	}
	var defaultAfter string
	var boundActive bool
	if err := a.pool.QueryRow(a.ctx, "SELECT to_jsonb(c)::text FROM application_key_clients c WHERE id = $1", defaults.ClientSessionID).Scan(&defaultAfter); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(a.ctx, "SELECT last_seen_at > clock_timestamp() - interval '10 seconds' FROM application_key_clients WHERE id = $1", bound.ClientSessionID).Scan(&boundActive); err != nil || !boundActive || defaultAfter != defaultBefore {
		t.Fatalf("media URL activity touched the wrong client or skipped its due activity: active=%t error=%v", boundActive, err)
	}
}

type applicationAuthQueryCounts struct {
	enabled      atomic.Bool
	statements   atomic.Int64
	transactions atomic.Int64
	updates      atomic.Int64
	exclusive    atomic.Int64
	shared       atomic.Int64
}

func (counts *applicationAuthQueryCounts) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !counts.enabled.Load() {
		return ctx
	}
	counts.statements.Add(1)
	sql := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(sql, "begin") {
		counts.transactions.Add(1)
	}
	if strings.HasPrefix(sql, "update ") {
		counts.updates.Add(1)
	}
	if strings.Contains(sql, "for update") {
		counts.exclusive.Add(1)
	}
	if strings.Contains(sql, "for share") {
		counts.shared.Add(1)
	}
	return ctx
}

func (*applicationAuthQueryCounts) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (counts *applicationAuthQueryCounts) reset() {
	counts.statements.Store(0)
	counts.transactions.Store(0)
	counts.updates.Store(0)
	counts.exclusive.Store(0)
	counts.shared.Store(0)
}

// This opt-in fixture is identical on baseline and candidate revisions. It
// measures the real authentication middleware without catalog or FFmpeg work.
// Distinct clients share public metadata, so the workload remains steady even
// when the hidden server-device generation is shared across multiple keys.
func TestHTTPApplicationKeyAuthenticationPerformance(t *testing.T) {
	runApplicationKeyAuthenticationPerformance(t, false)
}

// Clients with distinct public metadata repeatedly replace the credential and
// shared-device display. This fixture exposes the cost of leaving the steady
// read path and restarting a write transaction on each metadata transition.
func TestHTTPApplicationKeyAuthenticationMetadataChurnPerformance(t *testing.T) {
	runApplicationKeyAuthenticationPerformance(t, true)
}

func runApplicationKeyAuthenticationPerformance(t *testing.T, metadataChurn bool) {
	t.Helper()
	if os.Getenv("GOBY_AUTH_PERFORMANCE") != "1" {
		t.Skip("GOBY_AUTH_PERFORMANCE=1 is required for the authentication performance fixture")
	}
	a := newApplicationKeyHTTPFixture(t)
	const keyCount = 8
	keys := make([]applicationKeyHTTPSecret, keyCount)
	headers := make([]http.Header, keyCount)
	sharedHeaders := make([]http.Header, keyCount)
	for index := range keyCount {
		keys[index] = a.create(t, "Performance application")
		client := identity.Client{Name: fmt.Sprintf("Client %d", index), DeviceID: fmt.Sprintf("device-%d", index), Device: "Performance Device", Version: "1.0"}
		if metadataChurn {
			client.Device, client.Version = fmt.Sprintf("Performance Device %d", index), fmt.Sprintf("%d.0", index+1)
		}
		if _, err := a.users.ResolveEmbyForClient(a.ctx, keys[index].token, client); err != nil {
			t.Fatal(err)
		}
		headers[index] = http.Header{"X-Emby-Token": {keys[index].token}, "Authorization": {
			`Emby Client="` + client.Name + `", DeviceId="` + client.DeviceID + `", Device="` + client.Device + `", Version="` + client.Version + `"`}}
	}
	for index := range keyCount {
		client := identity.Client{Name: fmt.Sprintf("Shared Client %d", index), DeviceID: fmt.Sprintf("shared-device-%d", index), Device: "Performance Device", Version: "1.0"}
		if metadataChurn {
			client.Device, client.Version = fmt.Sprintf("Performance Device %d", index), fmt.Sprintf("%d.0", index+1)
		}
		if _, err := a.users.ResolveEmbyForClient(a.ctx, keys[0].token, client); err != nil {
			t.Fatal(err)
		}
		sharedHeaders[index] = http.Header{"X-Emby-Token": {keys[0].token}, "Authorization": {
			`Emby Client="` + client.Name + `", DeviceId="` + client.DeviceID + `", Device="` + client.Device + `", Version="` + client.Version + `"`}}
	}
	counts := new(applicationAuthQueryCounts)
	poolConfig := a.pool.Config()
	poolConfig.MaxConns = 16
	poolConfig.ConnConfig.Tracer = counts
	tracedPool, err := pgxpool.NewWithConfig(a.ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tracedPool.Close)
	a.app.identity = identity.New(tracedPool)
	handler := a.app.requireEmby(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	const requestsPerWorker = 32
	for _, keyMode := range []string{"same_key", "multiple_keys"} {
		requestHeaders := headers
		if keyMode == "same_key" {
			requestHeaders = sharedHeaders
		}
		for _, concurrency := range []int{1, 8, 32, 64} {
			t.Run(fmt.Sprintf("%s/concurrency_%d", keyMode, concurrency), func(t *testing.T) {
				// Warm every context and connection before starting the counters.
				for _, metadata := range requestHeaders {
					r := httptest.NewRequest(http.MethodGet, "/emby/System/Info", nil).WithContext(a.ctx)
					r.Header, r.RemoteAddr = metadata.Clone(), "192.0.2.33:54321"
					response := httptest.NewRecorder()
					handler(response, r)
					if response.Code != http.StatusNoContent {
						t.Fatalf("warm authentication: status=%d", response.Code)
					}
				}
				counts.reset()
				counts.enabled.Store(true)
				start := make(chan struct{})
				samples := make(chan []time.Duration, concurrency)
				errors := make(chan int, concurrency)
				var workers sync.WaitGroup
				for worker := range concurrency {
					workers.Add(1)
					go func(worker int) {
						defer workers.Done()
						index := worker % keyCount
						local := make([]time.Duration, 0, requestsPerWorker)
						<-start
						for request := range requestsPerWorker {
							if metadataChurn {
								// Rotate even with one worker so serialized requests
								// continually change the shared public projection.
								index = (worker + request) % keyCount
							}
							r := httptest.NewRequest(http.MethodGet, "/emby/System/Info", nil).WithContext(a.ctx)
							r.Header, r.RemoteAddr = requestHeaders[index].Clone(), "192.0.2.33:54321"
							response := httptest.NewRecorder()
							began := time.Now()
							handler(response, r)
							local = append(local, time.Since(began))
							if response.Code != http.StatusNoContent {
								errors <- response.Code
								return
							}
						}
						samples <- local
					}(worker)
				}
				began := time.Now()
				close(start)
				workers.Wait()
				elapsed := time.Since(began)
				counts.enabled.Store(false)
				close(errors)
				close(samples)
				for status := range errors {
					t.Fatalf("measured authentication failed: status=%d", status)
				}
				latencies := make([]time.Duration, 0, concurrency*requestsPerWorker)
				for local := range samples {
					latencies = append(latencies, local...)
				}
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				requests := len(latencies)
				result := map[string]any{
					"key_mode": keyMode, "concurrency": concurrency, "requests": requests,
					"metadata_churn":                        metadataChurn,
					"elapsed_ms":                            float64(elapsed.Nanoseconds()) / 1e6,
					"p95_ms":                                float64(latencies[(requests-1)*95/100].Nanoseconds()) / 1e6,
					"p99_ms":                                float64(latencies[(requests-1)*99/100].Nanoseconds()) / 1e6,
					"statements_per_request":                float64(counts.statements.Load()) / float64(requests),
					"transactions_per_request":              float64(counts.transactions.Load()) / float64(requests),
					"update_statements_per_request":         float64(counts.updates.Load()) / float64(requests),
					"exclusive_lock_statements_per_request": float64(counts.exclusive.Load()) / float64(requests),
					"shared_lock_statements_per_request":    float64(counts.shared.Load()) / float64(requests),
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("application_key_auth_performance=%s", encoded)
			})
		}
	}
}
