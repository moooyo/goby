package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/providers"
)

func providerPreviewTestHandler(fixture *imageAPIFixture, download func(context.Context, providers.RemoteImage) ([]byte, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/v1/items/{id}/providers/image-preview", fixture.f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		fixture.f.app.serveProviderImagePreview(w, r, download)
	}))
	return fixture.f.app.middleware(mux)
}

func providerPreviewTestRequest(fixture *imageAPIFixture, ctx context.Context, method string) *http.Request {
	path := "/admin/v1/items/" + fixture.itemID + "/providers/image-preview?Provider=tmdb&Id=42&ImageId=%2FFixture.jpg&ImageType=Primary"
	request := httptest.NewRequest(method, path, nil).WithContext(ctx)
	request.AddCookie(fixture.cookie)
	return request
}

func providerPreviewTestBudget(cache *imageCache) (processing, transfers, retained int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.slots), cache.activeTransfers, cache.activeBytes
}

func assertProviderPreviewTestIdle(t *testing.T, cache *imageCache) {
	t.Helper()
	if processing, transfers, retained := providerPreviewTestBudget(cache); processing != 0 || transfers != 0 || retained != 0 {
		t.Fatalf("preview ownership leaked: processing=%d transfers=%d retained=%d", processing, transfers, retained)
	}
}

type providerPreviewTestResponse struct {
	*httptest.ResponseRecorder
	observationMu sync.Mutex
	observe       func()
	deadlineError error
	writeError    error
}

func (w *providerPreviewTestResponse) observeOwnership() {
	w.observationMu.Lock()
	defer w.observationMu.Unlock()
	if w.observe != nil {
		w.observe()
	}
}

func (w *providerPreviewTestResponse) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		w.observeOwnership()
	}
	return w.deadlineError
}

func (w *providerPreviewTestResponse) Write(data []byte) (int, error) {
	w.observeOwnership()
	if w.writeError != nil {
		return 0, w.writeError
	}
	return w.ResponseRecorder.Write(data)
}

func TestHTTPProviderImagePreviewTransfersGETAndHEADRetainedCapacity(t *testing.T) {
	fixture := newImageAPIFixture(t)
	cache := fixture.f.app.images
	data := providerPreviewTestLargeJPEG(fixture.poster)
	expected, err := artwork.Render(fixture.f.ctx, bytes.NewReader(data), artwork.Options{Format: "jpeg", MaxWidth: 320, MaxHeight: 480})
	if err != nil {
		t.Fatal(err)
	}
	expectedCapacity := cap(expected.Bytes)
	expected.Bytes = nil
	if expectedCapacity <= len(data) {
		t.Fatal("the retained-capacity fixture must distinguish slice capacity from length")
	}
	handler := providerPreviewTestHandler(fixture, func(context.Context, providers.RemoteImage) ([]byte, error) {
		if processing, transfers, retained := providerPreviewTestBudget(cache); processing != 1 || transfers != 0 || retained != 0 {
			t.Errorf("download left processing ownership: processing=%d transfers=%d retained=%d", processing, transfers, retained)
		}
		return bytes.Clone(data), nil
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			observations := 0
			writer := &providerPreviewTestResponse{ResponseRecorder: httptest.NewRecorder(), observe: func() {
				observations++
				if processing, transfers, retained := providerPreviewTestBudget(cache); processing != 0 || transfers != 1 || retained != expectedCapacity {
					t.Errorf("output lacks retained-capacity ownership: processing=%d transfers=%d retained=%d", processing, transfers, retained)
				}
			}}
			handler.ServeHTTP(writer, providerPreviewTestRequest(fixture, fixture.f.ctx, method))
			expectStatus(t, writer.ResponseRecorder, http.StatusOK)
			if observations == 0 || writer.Header().Get("Content-Length") != strconv.Itoa(len(data)) ||
				writer.Header().Get("Content-Type") != "image/jpeg" || writer.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("preview response contract changed: observations=%d headers=%v", observations, writer.Header())
			}
			if method == http.MethodHead && writer.Body.Len() != 0 || method == http.MethodGet && !bytes.Equal(writer.Body.Bytes(), data) {
				t.Fatal("preview GET or HEAD body changed")
			}
			assertProviderPreviewTestIdle(t, cache)
		})
	}
}

func TestHTTPProviderImagePreviewRejectsTransferCountAndBytePressure(t *testing.T) {
	fixture := newImageAPIFixture(t)
	cache := fixture.f.app.images
	handler := providerPreviewTestHandler(fixture, func(context.Context, providers.RemoteImage) ([]byte, error) {
		return bytes.Clone(fixture.poster), nil
	})
	for _, limit := range []string{"count", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			count, size := imageTransferCount, 0
			if limit == "bytes" {
				count, size = 1, imageTransferBytes
			}
			for range count {
				release, admitted := cache.beginTransfer(size)
				if !admitted {
					t.Fatal("could not reserve the transfer-pressure fixture")
				}
				t.Cleanup(release)
			}
			observations := 0
			response := &providerPreviewTestResponse{ResponseRecorder: httptest.NewRecorder(), observe: func() {
				observations++
				if processing, transfers, retained := providerPreviewTestBudget(cache); processing != 0 || transfers != count || retained != size {
					t.Errorf("admission-error output retained preview ownership: processing=%d transfers=%d retained=%d", processing, transfers, retained)
				}
			}}
			handler.ServeHTTP(response, providerPreviewTestRequest(fixture, fixture.f.ctx, http.MethodGet))
			expectAPIError(t, response.ResponseRecorder, http.StatusTooManyRequests, "image_transfer_limit", false)
			if observations == 0 {
				t.Fatal("the admission error never reached response output")
			}
			if response.Header().Get("Retry-After") != "2" {
				t.Fatal("transfer admission did not advertise retry")
			}
			if processing, transfers, retained := providerPreviewTestBudget(cache); processing != 0 || transfers != count || retained != size {
				t.Fatalf("failed admission changed transfer ownership: processing=%d transfers=%d retained=%d", processing, transfers, retained)
			}
		})
	}
	assertProviderPreviewTestIdle(t, cache)
}

func TestHTTPProviderImagePreviewReleasesAfterDownloadRenderAndWriterFailure(t *testing.T) {
	fixture := newImageAPIFixture(t)
	cache := fixture.f.app.images
	for _, phase := range []string{"download", "render", "writer setup", "write"} {
		t.Run(phase, func(t *testing.T) {
			handler := providerPreviewTestHandler(fixture, func(context.Context, providers.RemoteImage) ([]byte, error) {
				if phase == "download" {
					return nil, providers.ErrUnavailable
				}
				if phase == "render" {
					return []byte("invalid image"), nil
				}
				return bytes.Clone(fixture.poster), nil
			})
			writer := &providerPreviewTestResponse{ResponseRecorder: httptest.NewRecorder()}
			if phase == "writer setup" {
				writer.deadlineError = io.ErrClosedPipe
			}
			if phase == "write" {
				writer.writeError = io.ErrClosedPipe
			}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				handler.ServeHTTP(writer, providerPreviewTestRequest(fixture, fixture.f.ctx, http.MethodGet))
			}()
			if phase == "write" && recovered != http.ErrAbortHandler || phase != "write" && recovered != nil {
				t.Fatalf("unexpected writer retirement: phase=%s panic=%v", phase, recovered)
			}
			if phase == "download" {
				expectStatus(t, writer.ResponseRecorder, http.StatusBadGateway)
			}
			if phase == "render" {
				expectStatus(t, writer.ResponseRecorder, http.StatusUnsupportedMediaType)
			}
			assertProviderPreviewTestIdle(t, cache)
		})
	}
}

func TestHTTPProviderImagePreviewCancellationRetiresDownloadAndTransfer(t *testing.T) {
	fixture := newImageAPIFixture(t)
	cache := fixture.f.app.images
	for _, phase := range []string{"download", "transfer"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(fixture.f.ctx)
			writer := newProviderPreviewTestWriter(t)
			entered := writer.entered
			if phase == "download" {
				entered = make(chan struct{})
			}
			handler := providerPreviewTestHandler(fixture, func(ctx context.Context, _ providers.RemoteImage) ([]byte, error) {
				if phase == "download" {
					close(entered)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return bytes.Clone(fixture.poster), nil
			})
			done := make(chan struct{})
			var recovered any
			go func() {
				defer close(done)
				defer func() { recovered = recover() }()
				handler.ServeHTTP(writer, providerPreviewTestRequest(fixture, ctx, http.MethodGet))
			}()
			t.Cleanup(func() {
				cancel()
				_ = writer.conn.Close()
				_ = writer.peer.Close()
				providerPreviewTestWait(t, done, "canceled preview cleanup")
			})
			providerPreviewTestWait(t, entered, "preview cancellation phase")
			processing, transfers, retained := providerPreviewTestBudget(cache)
			if phase == "download" && (processing != 1 || transfers != 0 || retained != 0) ||
				phase == "transfer" && (processing != 0 || transfers != 1 || retained < len(fixture.poster)) {
				t.Fatalf("wrong cancellation owner: phase=%s processing=%d transfers=%d retained=%d", phase, processing, transfers, retained)
			}
			cancel()
			providerPreviewTestWait(t, done, "preview cancellation retirement")
			if phase == "download" && recovered != nil || phase == "transfer" && recovered != http.ErrAbortHandler {
				t.Fatalf("unexpected cancellation result: phase=%s panic=%v", phase, recovered)
			}
			assertProviderPreviewTestIdle(t, cache)
		})
	}
}

func TestHTTPProviderImagePreviewRechecksAuthorityBeforeTransfer(t *testing.T) {
	fixture := newImageAPIFixture(t)
	cache := fixture.f.app.images
	handler := providerPreviewTestHandler(fixture, func(ctx context.Context, _ providers.RemoteImage) ([]byte, error) {
		if _, err := fixture.f.pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", fixture.adminID); err != nil {
			t.Fatal(err)
		}
		return bytes.Clone(fixture.poster), nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, providerPreviewTestRequest(fixture, fixture.f.ctx, http.MethodGet))
	if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
		t.Fatalf("authority revoked during download reached image output: status=%d", response.Code)
	}
	if response.Header().Get("Content-Type") == "image/jpeg" || bytes.Equal(response.Body.Bytes(), fixture.poster) {
		t.Fatal("revoked authority received preview image bytes")
	}
	assertProviderPreviewTestIdle(t, cache)
}
