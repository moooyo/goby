package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/providers"
)

// The remote measurement overlay replaces only DownloadImage in the original
// handler and sets this marker. No provider credential or network is involved.
var providerPreviewTestOverlayActive bool

type providerPreviewTestDownloadKey struct{}

func providerPreviewTestDownload(ctx context.Context, selected providers.RemoteImage) ([]byte, error) {
	data, ok := ctx.Value(providerPreviewTestDownloadKey{}).([]byte)
	if !ok || selected.Provider != "tmdb" || selected.ID != "42" || selected.ImageID != "/Fixture.jpg" {
		return nil, providers.ErrInvalidInput
	}
	return bytes.Clone(data), ctx.Err()
}

// An unread net.Pipe supplies actual transport backpressure without relying on
// kernel socket buffers, external networking, or a scheduler sleep to fill them.
type providerPreviewTestWriter struct {
	header  http.Header
	conn    net.Conn
	peer    net.Conn
	entered chan struct{}
	once    sync.Once
	status  int
}

func newProviderPreviewTestWriter(t *testing.T) *providerPreviewTestWriter {
	t.Helper()
	conn, peer := net.Pipe()
	w := &providerPreviewTestWriter{header: make(http.Header), conn: conn, peer: peer, entered: make(chan struct{})}
	t.Cleanup(func() {
		_ = conn.Close()
		_ = peer.Close()
	})
	return w
}

func (w *providerPreviewTestWriter) Header() http.Header    { return w.header }
func (w *providerPreviewTestWriter) WriteHeader(status int) { w.status = status }
func (w *providerPreviewTestWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.conn.Write(data)
}
func (w *providerPreviewTestWriter) SetWriteDeadline(deadline time.Time) error {
	return w.conn.SetWriteDeadline(deadline)
}

func providerPreviewTestWait(t *testing.T, done <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func providerPreviewTestLargeJPEG(picture []byte) []byte {
	// JPEG comments preserve the original dimensions and are intentionally
	// retained by the no-transform path. Pixel bounds alone do not bound bytes.
	const commentLength = 60 << 10
	data := make([]byte, 0, len(picture)+20*(commentLength+4))
	data = append(data, picture[:2]...)
	for range 20 {
		data = append(data, 0xff, 0xfe, byte((commentLength+2)>>8), byte((commentLength+2)&0xff))
		data = append(data, bytes.Repeat([]byte{'x'}, commentLength)...)
	}
	return append(data, picture[2:]...)
}

func TestHTTPProviderImagePreviewBackpressureMeasurement(t *testing.T) {
	mode := os.Getenv("GOBY_PROVIDER_PREVIEW_MEASUREMENT")
	if mode == "" {
		t.Skip("the controlled remote provider-preview measurement requires its download overlay")
	}
	if mode != "baseline" && mode != "candidate" {
		t.Fatal("GOBY_PROVIDER_PREVIEW_MEASUREMENT must be baseline or candidate")
	}
	if !providerPreviewTestOverlayActive {
		t.Fatal("the fixture-only download overlay is required before any request")
	}
	fixture := newImageAPIFixture(t)
	f := fixture.f
	data := providerPreviewTestLargeJPEG(fixture.poster)
	previewPath := "/admin/v1/items/" + fixture.itemID + "/providers/image-preview?Provider=tmdb&Id=42&ImageId=%2FFixture.jpg&ImageType=Primary"
	ordinaryPath := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	assertAPIImage(t, f.request(t, http.MethodGet, ordinaryPath, nil, fixture.headers), 160, 240, "jpeg")
	started := time.Now()
	unblocked := f.request(t, http.MethodGet, ordinaryPath, nil, fixture.headers)
	unblockedLatency := time.Since(started)
	assertAPIImage(t, unblocked, 160, 240, "jpeg")
	var writers []*providerPreviewTestWriter
	var completed []<-chan struct{}
	panics := make(chan any, cap(f.app.images.slots)+1)
	for range cap(f.app.images.slots) {
		writer := newProviderPreviewTestWriter(t)
		writers = append(writers, writer)
		previewCtx, previewCancel := context.WithCancel(f.ctx)
		request := httptest.NewRequest(http.MethodGet, previewPath, nil).WithContext(context.WithValue(previewCtx, providerPreviewTestDownloadKey{}, data))
		request.AddCookie(fixture.cookie)
		done := make(chan struct{})
		completed = append(completed, done)
		go func() {
			defer close(done)
			defer writer.conn.Close()
			defer func() {
				if recovered := recover(); recovered != nil {
					panics <- recovered
				}
			}()
			f.handler.ServeHTTP(writer, request)
		}()
		t.Cleanup(func() {
			previewCancel()
			_ = writer.conn.Close()
			_ = writer.peer.Close()
			providerPreviewTestWait(t, done, "preview cleanup")
		})
		providerPreviewTestWait(t, writer.entered, "preview output backpressure")
		if writer.status != http.StatusOK || writer.header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("preview did not reach image output: status=%d headers=%v", writer.status, writer.header)
		}
	}
	processing := len(f.app.images.slots)
	f.app.images.mu.Lock()
	transfers, retainedBytes := f.app.images.activeTransfers, f.app.images.activeBytes
	f.app.images.mu.Unlock()
	ordinaryDone := make(chan struct{})
	ordinary := httptest.NewRecorder()
	ordinaryCtx, ordinaryCancel := context.WithCancel(f.ctx)
	request := httptest.NewRequest(http.MethodGet, ordinaryPath, nil).WithContext(ordinaryCtx)
	request.Header = fixture.headers.Clone()
	started = time.Now()
	var ordinaryLatency time.Duration
	go func() {
		defer close(ordinaryDone)
		defer func() {
			ordinaryLatency = time.Since(started)
			if recovered := recover(); recovered != nil {
				panics <- recovered
			}
		}()
		f.handler.ServeHTTP(ordinary, request)
	}()
	t.Cleanup(func() {
		ordinaryCancel()
		providerPreviewTestWait(t, ordinaryDone, "ordinary image cleanup")
	})
	const observationWindow = 250 * time.Millisecond
	finishedWhileBlocked := false
	select {
	case <-ordinaryDone:
		finishedWhileBlocked = true
	case <-time.After(observationWindow):
	}
	blockedLatency := time.Since(started)
	var drained []<-chan struct{}
	for _, writer := range writers {
		done := make(chan struct{})
		drained = append(drained, done)
		go func() {
			defer close(done)
			_, _ = io.Copy(io.Discard, writer.peer)
			_ = writer.peer.Close()
		}()
	}
	for _, done := range completed {
		providerPreviewTestWait(t, done, "preview retirement")
	}
	for _, done := range drained {
		providerPreviewTestWait(t, done, "preview drain retirement")
	}
	providerPreviewTestWait(t, ordinaryDone, "ordinary image completion")
	assertAPIImage(t, ordinary, 160, 240, "jpeg")
	select {
	case recovered := <-panics:
		t.Fatalf("preview handler panicked: %v", recovered)
	default:
	}
	f.app.images.mu.Lock()
	finalTransfers, finalBytes := f.app.images.activeTransfers, f.app.images.activeBytes
	f.app.images.mu.Unlock()
	if len(f.app.images.slots) != 0 || finalTransfers != 0 || finalBytes != 0 {
		t.Fatalf("preview ownership remained after output: processing=%d transfers=%d bytes=%d", len(f.app.images.slots), finalTransfers, finalBytes)
	}
	t.Logf("mode=%s previews=%d source_bytes=%d processing=%d transfers=%d retained_bytes=%d ordinary_unblocked=%s ordinary_latency=%s observation_window=%s ordinary_finished_during_backpressure=%t", mode, len(writers), len(data), processing, transfers, retainedBytes, unblockedLatency, ordinaryLatency, blockedLatency, finishedWhileBlocked)
	if mode == "baseline" {
		if processing != len(writers) || transfers != 0 || retainedBytes != 0 || finishedWhileBlocked {
			t.Fatal("baseline did not reproduce shared processing-slot starvation")
		}
	} else if processing != 0 || transfers != len(writers) || retainedBytes < len(writers)*len(data) || !finishedWhileBlocked {
		t.Fatalf("candidate did not isolate preview output from processing: processing=%d transfers=%d bytes=%d completed=%t", processing, transfers, retainedBytes, finishedWhileBlocked)
	}
}
