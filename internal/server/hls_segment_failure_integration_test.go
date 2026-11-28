//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsSegmentFailureLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *hlsSegmentFailureLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}

func (l *hlsSegmentFailureLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.String()
}

// Fail one documented boundary on a real HTTP playback. The other layers use
// the existing fixture's source, account, PostgreSQL and HLS ownership.
type hlsSegmentFailureJobs struct {
	hlsJobs
	attempts atomic.Int32
	fail     diagnostics.PlaybackFailure
	err      error
}

func (jobs *hlsSegmentFailureJobs) Ensure(ctx context.Context, spec transcode.Spec, source *os.File) (transcode.Record, error) {
	if jobs.attempts.Add(1) == 1 {
		if err := source.Close(); err != nil {
			return transcode.Record{}, err
		}
		diagnostics.CapturePlaybackFailure(ctx, jobs.fail)
		return transcode.Record{ID: jobs.fail.JobID}, jobs.err
	}
	return jobs.hlsJobs.Ensure(ctx, spec, source)
}

func (jobs *hlsSegmentFailureJobs) Health() transcode.Health {
	return jobs.hlsJobs.(interface{ Health() transcode.Health }).Health()
}

func (jobs *hlsSegmentFailureJobs) CancelPlayback(authID, playID string) {
	jobs.hlsJobs.(interface{ CancelPlayback(string, string) }).CancelPlayback(authID, playID)
}

func TestHTTPHLSSegmentFailureDiagnostics(t *testing.T) {
	h := newHLSHTTPFixture(t)
	app, runtime := h.f.app, h.f.app.hls
	var output hlsSegmentFailureLog
	app.log = slog.New(diagnostics.NewHandler(nil, slog.NewJSONHandler(&output, nil)))

	const route = "GET /emby/Videos/{Id}/hls1/{PlaylistId}/{SegmentFile}"
	manager := runtime.manager
	jobs := &hlsSegmentFailureJobs{hlsJobs: manager, err: transcode.ErrOutputUnavailable,
		fail: diagnostics.PlaybackFailure{Stage: "hls_cache_accounting", ErrorClass: "accounting_pending"}}
	runtime.mu.Lock()
	runtime.manager = jobs
	runtime.mu.Unlock()
	graph := h.graph(t, h.accounts.viewer, 0)
	response := h.request(t, http.MethodGet, graph.children[0], nil, nil)
	assertHLSSegmentFailure(t, &output, response, route, "hls_cache_accounting", "accounting_pending", "hls_unavailable", "")
	retry := h.request(t, http.MethodGet, graph.children[0], nil, nil)
	expectHLSHTTPStatus(t, retry, http.StatusNotFound)
	assertNoHLSSegmentFailure(t, &output, retry.header.Get("X-Request-Id"))
	if jobs.attempts.Load() != 1 {
		t.Fatal("retired segment unexpectedly retried producer admission")
	}
	fresh := h.graph(t, h.accounts.viewer, 0)
	if fresh.hlsID == graph.hlsID {
		t.Fatal("renegotiation reused a retired HLS registration")
	}
	ok := h.request(t, http.MethodGet, fresh.children[0], nil, nil)
	expectHLSHTTPStatus(t, ok, http.StatusOK)
	assertNoHLSSegmentFailure(t, &output, ok.header.Get("X-Request-Id"))
	if jobs.attempts.Load() != 2 || len(ok.body) == 0 {
		t.Fatal("fresh negotiation did not deliver the first segment")
	}

	const jobID = "0123456789abcdef0123456789abcdef"
	jobs = &hlsSegmentFailureJobs{hlsJobs: manager, err: transcode.ErrPersistence,
		fail: diagnostics.PlaybackFailure{Stage: "hls_job_insert", ErrorClass: "database_conflict", JobID: jobID}}
	runtime.mu.Lock()
	runtime.manager = jobs
	runtime.mu.Unlock()
	graph = h.graph(t, h.accounts.second, 0)
	response = h.request(t, http.MethodGet, graph.children[0], nil, nil)
	assertHLSSegmentFailure(t, &output, response, route, "hls_job_insert", "database_conflict", "hls_unavailable", jobID)

	graph = h.graph(t, h.accounts.second, 0)
	// Replace only the test's indexed file, keeping the path and bytes. The
	// source check must reject the new physical identity before admission.
	mediaBytes, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(h.path), "replacement.tmp")
	if err := os.WriteFile(replacement, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, h.path); err != nil {
		t.Fatal(err)
	}
	response = h.request(t, http.MethodGet, graph.children[0], nil, nil)
	assertHLSSegmentFailure(t, &output, response, route, "hls_segment_source", "source_changed", "hls_unavailable", "")
	if jobs.attempts.Load() != 1 {
		t.Fatal("changed source reached the producer manager")
	}
	if text := output.String(); strings.Contains(text, "api_key") || strings.Contains(text, ".mp4") ||
		strings.Contains(text, h.accounts.viewer.headers.Get("X-Emby-Token")) || strings.Contains(text, h.accounts.second.headers.Get("X-Emby-Token")) {
		t.Fatal("HLS diagnostics retained private request, source or credential data")
	}
}

func hlsSegmentFailureRecords(t *testing.T, output *hlsSegmentFailureLog, requestID string) (failure, completion map[string]any) {
	t.Helper()
	if requestID == "" {
		t.Fatal("response did not carry a request ID")
	}
	dec := json.NewDecoder(strings.NewReader(output.String()))
	for {
		var entry map[string]any
		if err := dec.Decode(&entry); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if entry["request_id"] != requestID {
			continue
		}
		switch entry["event"] {
		case "playback.operation.failed":
			if failure != nil {
				t.Fatal("duplicate HLS failure event")
			}
			failure = entry
		case "request.completed":
			if completion != nil {
				t.Fatal("duplicate request completion")
			}
			completion = entry
		}
	}
	if completion == nil {
		t.Fatal("no matching request completion")
	}
	return failure, completion
}

func assertHLSSegmentFailure(t *testing.T, output *hlsSegmentFailureLog, response hlsHTTPResponse, route, stage, class, code, jobID string) {
	t.Helper()
	expectHLSHTTPStatus(t, response, http.StatusServiceUnavailable)
	failure, completed := hlsSegmentFailureRecords(t, output, response.header.Get("X-Request-Id"))
	if failure["playback_stage"] != stage || failure["error_class"] != class || failure["hls_error_code"] != code ||
		failure["status"] != float64(503) || completed["status"] != float64(503) || completed["route"] != route {
		t.Fatalf("incorrect HLS failure or completion: failure=%v completion=%v", failure, completed)
	}
	if id, _ := failure["job_id"].(string); id != jobID {
		t.Fatal("incorrect HLS job correlation")
	}
	var body struct {
		ResponseStatus struct{ ErrorCode string }
	}
	if err := json.Unmarshal(response.body, &body); err != nil || body.ResponseStatus.ErrorCode != code {
		t.Fatalf("unexpected response error envelope: %v", err)
	}
}

func assertNoHLSSegmentFailure(t *testing.T, output *hlsSegmentFailureLog, requestID string) {
	t.Helper()
	failure, _ := hlsSegmentFailureRecords(t, output, requestID)
	if failure != nil {
		t.Fatal("non-503 request emitted a failure event")
	}
}
