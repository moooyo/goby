//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsCachedContentBarrier struct {
	armed                   atomic.Bool
	entered, resume         chan struct{}
	enteredOnce, resumeOnce sync.Once
}

func (barrier *hlsCachedContentBarrier) release() {
	barrier.resumeOnce.Do(func() { close(barrier.resume) })
}

type hlsCachedContentBarrierWriter struct {
	http.ResponseWriter
	barrier *hlsCachedContentBarrier
}

func (writer *hlsCachedContentBarrierWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *hlsCachedContentBarrierWriter) SetWriteDeadline(deadline time.Time) error {
	err := http.NewResponseController(writer.ResponseWriter).SetWriteDeadline(deadline)
	remaining := time.Until(deadline)
	// Ignore beginHLS's two-minute deadline, cancellation's immediate deadline,
	// and deadline clearing. The first content deadline follows both fresh
	// authorization stages and Stat; its next Header call is in ServeContent.
	if err == nil && remaining > time.Second && remaining <= mediaWriteIdle+time.Second {
		writer.barrier.armed.Store(true)
	}
	return err
}

func (writer *hlsCachedContentBarrierWriter) Header() http.Header {
	if writer.barrier.armed.Load() {
		writer.barrier.enteredOnce.Do(func() {
			close(writer.barrier.entered)
			<-writer.barrier.resume
		})
	}
	return writer.ResponseWriter.Header()
}

type hlsCachedContentResult struct {
	status   int
	body     []byte
	readSize int
	err      error
}

func TestHTTPHLSCachedSegmentStopBeforeSeekNeverReturnsInternalError(t *testing.T) {
	h := newHLSHTTPFixture(t)
	graph := h.graph(t, h.accounts.viewer, 0)
	if h.f.app.hls.generatedWindowsEnabled {
		t.Fatal("the regression requires the legacy hls1 segment route")
	}
	expected := httpGetStopRemeasureCachedOutput(t, h, graph)
	if len(expected) < 32 {
		t.Fatal("the cached fixture has no complete range prefix")
	}
	rangeHeader := http.Header{"Range": {"bytes=0-31"}}
	contentRange := "bytes 0-31/" + strconv.Itoa(len(expected))
	partial := h.request(t, http.MethodGet, graph.children[0], nil, rangeHeader)
	if partial.status != http.StatusPartialContent || partial.header.Get("Content-Range") != contentRange ||
		partial.header.Get("Content-Length") != "32" || !bytes.Equal(partial.body, expected[:32]) {
		t.Fatal("cached legacy GET did not preserve exact range bytes and metadata")
	}
	head := h.request(t, http.MethodHead, graph.children[0], nil, rangeHeader)
	if head.status != http.StatusPartialContent || head.header.Get("Content-Range") != contentRange ||
		head.header.Get("Content-Length") != "32" || len(head.body) != 0 {
		t.Fatal("cached legacy HEAD did not preserve range metadata without a body")
	}
	httpGetStopRemeasureJoinRequests(t, h.f.ctx, h.f.app.hls)
	h.f.app.hls.mu.Lock()
	session := h.f.app.hls.sessions[graph.hlsID]
	h.f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("the cached graph has no exact playback owner")
	}
	session.mu.Lock()
	producers := append([]hlsProducer(nil), session.producers...)
	session.mu.Unlock()
	resources, ok := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("the cached manager has no exact-scope reader observation")
	}
	sourceInfo, err := os.Stat(h.path)
	if err != nil {
		t.Fatal("read the synthetic source's physical identity")
	}
	target, err := url.Parse(graph.children[0])
	if err != nil {
		t.Fatal("parse the fixture's cached child path")
	}
	barrier := &hlsCachedContentBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == target.Path {
			w = &hlsCachedContentBarrierWriter{ResponseWriter: w, barrier: barrier}
		}
		h.f.handler.ServeHTTP(w, r)
	}))
	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	getCtx, cancelGET := context.WithTimeout(h.f.ctx, 15*time.Second)
	results := make(chan hlsCachedContentResult, 1)
	done := make(chan struct{})
	// Cleanup releases the barrier before joining either client or server. It
	// also runs on the expected red failure, so no blocked handler survives Fatal.
	t.Cleanup(func() {
		barrier.release()
		cancelGET()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the controlled cached GET did not join")
		}
		transport.CloseIdleConnections()
		server.CloseClientConnections()
		server.Close()
		h.server.CloseClientConnections()
		h.server.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.f.app.Close(cleanup); err != nil {
			t.Errorf("cached cancellation fixture Close failed: error_type=%T", err)
			return
		}
		usage, err := resources.ResourceUsage(cleanup, session.key.scope)
		if err != nil || usage != (transcode.ResourceUsage{}) || playbackStopAliasSourceFDs(t, sourceInfo) != 0 ||
			h.f.pool.Stat().AcquiredConns() != 0 || len(h.f.app.hls.slots) != 0 {
			t.Error("closed cached cancellation fixture retained a scoped owner, source FD, pool loan or request slot")
		}
	})
	go func() {
		defer close(done)
		request, err := http.NewRequestWithContext(getCtx, http.MethodGet, server.URL+graph.children[0], nil)
		if err != nil {
			results <- hlsCachedContentResult{err: err}
			return
		}
		response, err := client.Do(request)
		if err != nil {
			results <- hlsCachedContentResult{err: err}
			return
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(expected)+1)))
		closeErr := response.Body.Close()
		if readErr == nil {
			readErr = closeErr
		}
		results <- hlsCachedContentResult{status: response.StatusCode, body: bytes.Clone(data[:min(len(data), 256)]), readSize: len(data), err: readErr}
	}()
	select {
	case <-barrier.entered:
	case <-getCtx.Done():
		t.Fatal("cached GET did not reach ServeContent after its content deadline")
	}
	usage, err := resources.ResourceUsage(h.f.ctx, session.key.scope)
	if err != nil || usage.ReaderPins != 1 || usage.UnfinishedJobs != 0 {
		t.Fatal("the controlled cached GET did not retain exactly one completed output reader")
	}
	stopped := h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", map[string]any{
		"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0,
	}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, stopped, http.StatusNoContent)
	// Keep Header blocked until the same-scope callback has actually released
	// its output pin. Cancellation alone cannot prove that Close has completed.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		usage, err = resources.ResourceUsage(h.f.ctx, session.key.scope)
		if err != nil || usage.AccountingUnknownJobs != 0 || usage.CompletionUnknownJobs != 0 {
			t.Fatal("same-scope cancellation ownership became unknown")
		}
		if usage.ReaderPins == 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("Stopped did not close the held output reader before Header resumed")
		}
	}
	barrier.release()
	var result hlsCachedContentResult
	select {
	case result = <-results:
	case <-getCtx.Done():
		t.Fatal("cancelled cached content did not finish after Header resumed")
	}
	<-done
	httpGetStopRemeasureJoinRequests(t, h.f.ctx, h.f.app.hls)
	class := "other"
	if bytes.Contains(result.body, []byte("internal_error")) {
		class = "json_internal_error"
	} else if bytes.Equal(bytes.TrimSpace(result.body), []byte("seeker can't seek")) {
		class = "plain_seeker_error"
	}
	// The synthetic fixture body is bounded; transport errors can contain the
	// token-bearing URL and are intentionally reported by type only.
	seeker500 := result.status == http.StatusInternalServerError && result.err == nil && class == "plain_seeker_error"
	t.Logf("cancelled_cached_segment status=%d body_class=%s legacy_seeker_500_reproduced=%t bounded_body=%q read_bytes=%d transport_error_type=%T", result.status, class, seeker500, result.body, result.readSize, result.err)
	if result.status == http.StatusInternalServerError {
		if seeker500 {
			// Only this exact failure is the expected red reproduction. Another
			// failure or a JSON internal_error cannot qualify the red product.
			t.Fatalf("legacy_cancelled_segment_seeker_500_reproduced: status=500 bounded_body=%q", result.body)
		}
		t.Fatalf("cancelled cached segment returned 500: body_class=%s bounded_body=%q", class, result.body)
	}
	if result.err == nil {
		if result.status != http.StatusNotFound {
			t.Fatalf("cancelled cached segment reported a complete response: status=%d bounded_body=%q", result.status, result.body)
		}
	} else {
		aborted := errors.Is(result.err, io.EOF) || errors.Is(result.err, io.ErrUnexpectedEOF) || errors.Is(result.err, net.ErrClosed) ||
			errors.Is(result.err, syscall.ECONNRESET) || errors.Is(result.err, syscall.EPIPE)
		if !aborted || result.status == http.StatusOK && result.readSize >= len(expected) {
			t.Fatalf("cancelled cached segment did not abort its transport: status=%d read_bytes=%d error_type=%T", result.status, result.readSize, result.err)
		}
	}
	_ = httpGetStopRemeasureFence(t, h, graph, session, producers, h.server.Client())
}
