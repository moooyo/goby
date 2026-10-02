package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

type originalPrimaryTestSource struct {
	mu      sync.Mutex
	reads   int
	seeks   int
	closes  int
	readFn  func([]byte) (int, error)
	seekFn  func(int64, int) (int64, error)
	closeFn func() error
}

func (s *originalPrimaryTestSource) Read(buffer []byte) (int, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	if s.readFn != nil {
		return s.readFn(buffer)
	}
	return 0, io.EOF
}

func (s *originalPrimaryTestSource) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	s.seeks++
	s.mu.Unlock()
	if s.seekFn != nil {
		return s.seekFn(offset, whence)
	}
	return offset, nil
}

func (s *originalPrimaryTestSource) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *originalPrimaryTestSource) counts() (reads, seeks, closes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads, s.seeks, s.closes
}

type originalPrimaryTestResponseWriter struct {
	header        http.Header
	status        int
	writeFn       func([]byte) (int, error)
	readFromCalls int
}

func (w *originalPrimaryTestResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *originalPrimaryTestResponseWriter) WriteHeader(status int) { w.status = status }

func (w *originalPrimaryTestResponseWriter) Write(buffer []byte) (int, error) {
	if w.writeFn != nil {
		return w.writeFn(buffer)
	}
	return len(buffer), nil
}

func (w *originalPrimaryTestResponseWriter) ReadFrom(io.Reader) (int64, error) {
	w.readFromCalls++
	return 0, errors.New("unexpected ReaderFrom fast path")
}

func originalPrimaryTestFixture(t *testing.T) (*primaryio.Governor, *primaryio.OwnerRuntime) {
	t.Helper()
	governor, err := primaryio.NewGovernor(primaryio.Limits{
		Owners: 1, RootOwners: 1, DomainOwners: 1,
		Queued: 2, RootQueued: 2, DomainQueued: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := primaryio.NewOwnerRuntime(governor, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Failure cleanup cancels admission without claiming descriptor retirement.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = owners.Close(ctx)
		governor.Close()
	})
	return governor, owners
}

func originalPrimaryTestRoute() primaryio.Route {
	return primaryio.Route{
		Roots:   []primaryio.RootKey{{Catalog: "original-http-test", RootID: "media"}},
		Domains: []string{"original-http-device"},
	}
}

func originalPrimaryTestContent(t *testing.T, owners *primaryio.OwnerRuntime, source primaryio.ReadSeekCloser) *primaryio.ReadSeeker {
	t.Helper()
	owner, err := owners.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content, err := primaryio.NewReadSeeker(owner, originalPrimaryTestRoute(), primaryio.Foreground, source, primaryio.MaxReadChunkBytes, nil)
	if err != nil {
		_ = source.Close()
		_ = owner.Complete()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = content.Close() })
	return content
}

func originalPrimaryTestAwait[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("original response did not reach its completion barrier")
		var zero T
		return zero
	}
}

func originalPrimaryTestGate() (<-chan struct{}, func()) {
	gate := make(chan struct{})
	var once sync.Once
	return gate, func() { once.Do(func() { close(gate) }) }
}

func originalPrimaryTestDrain(t *testing.T, owners *primaryio.OwnerRuntime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := owners.Close(ctx); err != nil {
		t.Fatalf("retired original response did not drain: %v", err)
	}
}

func TestOriginalPrimaryResponseWriterCountsReturnedBytes(t *testing.T) {
	writeFailure := errors.New("network write failed")
	for _, test := range []struct {
		name    string
		n       int
		err     error
		wantN   int
		wantErr error
	}{
		{name: "complete write", n: 4, wantN: 4},
		{name: "short write", n: 2, wantN: 2},
		{name: "partial failed write", n: 2, err: writeFailure, wantN: 2, wantErr: writeFailure},
		{name: "complete failed write", n: 4, err: writeFailure, wantN: 4, wantErr: writeFailure},
		{name: "negative result", n: -1, wantErr: io.ErrShortWrite},
		{name: "oversized result", n: 5, err: writeFailure, wantErr: io.ErrShortWrite},
	} {
		t.Run(test.name, func(t *testing.T) {
			underlying := &originalPrimaryTestResponseWriter{writeFn: func([]byte) (int, error) {
				return test.n, test.err
			}}
			counted := &originalPrimaryResponseWriter{writer: underlying, written: 3}
			counted.Header().Set("X-Original-Test", "forwarded")
			counted.WriteHeader(http.StatusPartialContent)
			n, err := counted.Write([]byte("body"))
			if n != test.wantN || !errors.Is(err, test.wantErr) || counted.written != int64(3+test.wantN) {
				t.Fatalf("counted write: n=%d err=%v bytes=%d; want n=%d err=%v bytes=%d", n, err, counted.written, test.wantN, test.wantErr, 3+test.wantN)
			}
			if underlying.Header().Get("X-Original-Test") != "forwarded" || underlying.status != http.StatusPartialContent {
				t.Fatal("counted writer did not forward the response header and status")
			}
		})
	}
}

func TestOriginalPrimaryResponseWriterDoesNotExposeReaderFrom(t *testing.T) {
	underlying := &originalPrimaryTestResponseWriter{}
	counted := &originalPrimaryResponseWriter{writer: underlying}
	if _, exposed := any(counted).(io.ReaderFrom); exposed {
		t.Fatal("counted writer exposed the underlying ReaderFrom fast path")
	}
	// Hide the source's WriterTo as well, so Copy must use the destination's Write.
	source := struct{ io.Reader }{Reader: strings.NewReader("count every body byte")}
	written, err := io.Copy(counted, source)
	if err != nil || written != int64(len("count every body byte")) || counted.written != written || underlying.readFromCalls != 0 {
		t.Fatalf("bounded copy bypassed byte accounting: bytes=%d counted=%d ReaderFrom=%d err=%v", written, counted.written, underlying.readFromCalls, err)
	}
}

func TestOriginalPrimaryBodyErrorChecksDeclaredBodyLength(t *testing.T) {
	for _, test := range []struct {
		name    string
		method  string
		status  int
		length  string
		written int64
		wantErr error
	}{
		{name: "complete body", method: http.MethodGet, status: http.StatusOK, length: "5", written: 5},
		{name: "complete range", method: http.MethodGet, status: http.StatusPartialContent, length: "5", written: 5},
		{name: "short body", method: http.MethodGet, status: http.StatusOK, length: "5", written: 3, wantErr: io.ErrUnexpectedEOF},
		{name: "short range", method: http.MethodGet, status: http.StatusPartialContent, length: "5", written: 3, wantErr: io.ErrUnexpectedEOF},
		{name: "oversized body", method: http.MethodGet, status: http.StatusOK, length: "5", written: 6, wantErr: io.ErrUnexpectedEOF},
		{name: "missing length", method: http.MethodGet, status: http.StatusOK, wantErr: io.ErrUnexpectedEOF},
		{name: "malformed length", method: http.MethodGet, status: http.StatusOK, length: "invalid", wantErr: io.ErrUnexpectedEOF},
		{name: "negative length", method: http.MethodGet, status: http.StatusOK, length: "-1", wantErr: io.ErrUnexpectedEOF},
		{name: "head", method: http.MethodHead, status: http.StatusOK, length: "invalid"},
		{name: "no content", method: http.MethodGet, status: http.StatusNoContent},
		{name: "not modified", method: http.MethodGet, status: http.StatusNotModified},
		{name: "uncommitted response", method: http.MethodGet},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, owners := originalPrimaryTestFixture(t)
			content := originalPrimaryTestContent(t, owners, &originalPrimaryTestSource{})
			response := httptest.NewRecorder()
			response.Header().Set("Content-Length", test.length)
			writer := &idleResponseWriter{ResponseWriter: response, status: test.status}
			counted := &originalPrimaryResponseWriter{writer: writer, written: test.written}
			request := httptest.NewRequest(test.method, "/original", nil)
			if err := originalPrimaryBodyError(request, content, writer, counted); !errors.Is(err, test.wantErr) {
				t.Fatalf("body completion error = %v; want %v", err, test.wantErr)
			}
		})
	}
}

func TestOriginalPrimaryBodyErrorPreservesSourceAndWriterFailures(t *testing.T) {
	readFailure := errors.New("original descriptor read failed")
	writeFailure := errors.New("original network write failed")
	for _, mode := range []string{"read failure", "admission failure", "truncated EOF", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			governor, owners := originalPrimaryTestFixture(t)
			data := bytes.NewReader([]byte("short"))
			source := &originalPrimaryTestSource{
				readFn: data.Read,
				seekFn: func(offset int64, whence int) (int64, error) {
					if offset == 0 && whence == io.SeekEnd {
						return 8, nil
					}
					return data.Seek(offset, whence)
				},
			}
			wantErr := error(io.ErrUnexpectedEOF)
			if mode == "read failure" {
				source.readFn = func([]byte) (int, error) { return 0, readFailure }
				wantErr = readFailure
			}
			content := originalPrimaryTestContent(t, owners, source)
			if mode == "admission failure" {
				governor.Close()
				wantErr = primaryio.ErrClosed
			}
			response := &originalPrimaryTestResponseWriter{}
			if mode == "write failure" {
				response.writeFn = func([]byte) (int, error) { return 0, writeFailure }
				wantErr = writeFailure
			}
			writer, err := newIdleResponseWriter(response, context.Background(), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if !writer.stop() {
					<-writer.callback
				}
			})
			writer.Header().Set("Content-Type", "application/octet-stream")
			counted := &originalPrimaryResponseWriter{writer: writer}
			request := httptest.NewRequest(http.MethodGet, "/original", nil)
			http.ServeContent(counted, request, "original.bin", time.Time{}, content)
			if mode == "read failure" || mode == "admission failure" {
				// The source/admission failure takes precedence over a write failure.
				writer.err = writeFailure
			}
			if err := originalPrimaryBodyError(request, content, writer, counted); !errors.Is(err, wantErr) {
				t.Fatalf("ServeContent hid %s: body error=%v; want %v", mode, err, wantErr)
			}
			if mode == "truncated EOF" && (content.Err() != nil || counted.written != 5 || counted.Header().Get("Content-Length") != "8") {
				t.Fatalf("normal EOF did not retain the declared-length check: content=%v bytes=%d length=%q", content.Err(), counted.written, counted.Header().Get("Content-Length"))
			}
			if reads, _, _ := source.counts(); mode == "admission failure" && reads != 0 {
				t.Fatalf("failed admission performed %d source reads", reads)
			}
		})
	}
}

func TestOriginalPrimaryServeContentReleasesIOQuotaBeforeBlockedWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		governor, owners := originalPrimaryTestFixture(t)
		payload := bytes.Repeat([]byte("x"), 2*primaryio.MaxReadChunkBytes+9)
		data := bytes.NewReader(payload)
		source := &originalPrimaryTestSource{
			readFn: func(buffer []byte) (int, error) {
				if stats := governor.Stats(); stats.Active != 1 {
					t.Errorf("source read did not hold its actual-I/O quota: %+v", stats)
				}
				return data.Read(buffer)
			},
			seekFn: data.Seek,
		}
		content := originalPrimaryTestContent(t, owners, source)
		writeEntered := make(chan struct{})
		writeMayReturn, releaseWrite := originalPrimaryTestGate()
		t.Cleanup(releaseWrite)
		var firstWrite sync.Once
		var body bytes.Buffer
		response := &originalPrimaryTestResponseWriter{writeFn: func(buffer []byte) (int, error) {
			firstWrite.Do(func() {
				close(writeEntered)
				<-writeMayReturn
			})
			return body.Write(buffer)
		}}
		writer, err := newIdleResponseWriter(response, context.Background(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(writer.finish)
		writer.Header().Set("Content-Type", "application/octet-stream")
		counted := &originalPrimaryResponseWriter{writer: writer}
		request := httptest.NewRequest(http.MethodGet, "/original", nil)
		served := make(chan struct{})
		bodyError := make(chan error, 1)
		go func() {
			defer close(served)
			http.ServeContent(counted, request, "original.bin", time.Time{}, content)
			bodyError <- originalPrimaryBodyError(request, content, writer, counted)
		}()
		t.Cleanup(func() {
			releaseWrite()
			originalPrimaryTestAwait(t, served)
		})
		originalPrimaryTestAwait(t, writeEntered)
		if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
			t.Fatalf("blocked network write retained actual-I/O quota: %+v", stats)
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 1 {
			t.Fatalf("blocked network write lost retained descriptor ownership: %+v", stats)
		}
		if reads, _, closes := source.counts(); reads != 1 || closes != 0 || counted.written != 0 {
			t.Fatalf("blocked write crossed a source or byte-accounting boundary: reads=%d closes=%d bytes=%d", reads, closes, counted.written)
		}
		releaseWrite()
		originalPrimaryTestAwait(t, served)
		if err := originalPrimaryTestAwait(t, bodyError); err != nil || counted.written != int64(len(payload)) || !bytes.Equal(body.Bytes(), payload) || response.readFromCalls != 0 {
			t.Fatalf("completed response: err=%v bytes=%d body=%d ReaderFrom=%d", err, counted.written, body.Len(), response.readFromCalls)
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 1 {
			t.Fatalf("ServeContent completion retired the descriptor before Close: %+v", stats)
		}
		if err := content.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, closes := source.counts(); closes != 1 || owners.Stats().RegisteredOwners != 0 || governor.Stats().Active != 0 {
			t.Fatalf("descriptor Close did not retire response ownership: closes=%d owners=%+v governor=%+v", closes, owners.Stats(), governor.Stats())
		}
		originalPrimaryTestDrain(t, owners)
	})
}

func TestOriginalPrimaryReadCancellationBridgeStopsWithoutRetiringContent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		governor, owners := originalPrimaryTestFixture(t)
		source := &originalPrimaryTestSource{}
		content := originalPrimaryTestContent(t, owners, source)
		work, cancel := context.WithCancel(content.Context())
		defer cancel()
		stopBridge := bridgeOriginalReadCancellation(work, content)
		stopBridge()
		cancel()
		synctest.Wait()
		if content.Context().Err() != nil || owners.Stats().RegisteredOwners != 1 || governor.Stats().Active != 0 {
			t.Fatalf("stopped bridge changed retained content ownership: context=%v owners=%+v governor=%+v", content.Context().Err(), owners.Stats(), governor.Stats())
		}
		if _, _, closes := source.counts(); closes != 0 {
			t.Fatalf("stopped bridge closed the descriptor %d times", closes)
		}
		if err := content.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, closes := source.counts(); closes != 1 || owners.Stats().RegisteredOwners != 0 {
			t.Fatalf("explicit Close did not retire stopped-bridge content: closes=%d owners=%+v", closes, owners.Stats())
		}
		originalPrimaryTestDrain(t, owners)
	})
}

func TestOriginalPrimaryReadCancellationBridgeJoinsQueuedReadAndDescriptorCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		governor, owners := originalPrimaryTestFixture(t)
		holder, err := owners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		lease, err := holder.Acquire(originalPrimaryTestRoute(), primaryio.Foreground)
		if err != nil {
			_ = holder.Complete()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = lease.Release()
			_ = holder.Complete()
		})
		closeEntered := make(chan struct{})
		closeMayReturn, releaseClose := originalPrimaryTestGate()
		source := &originalPrimaryTestSource{closeFn: func() error {
			close(closeEntered)
			<-closeMayReturn
			return nil
		}}
		content := originalPrimaryTestContent(t, owners, source)
		t.Cleanup(releaseClose)
		work, cancel := context.WithCancel(content.Context())
		stopBridge := bridgeOriginalReadCancellation(work, content)
		bridgeStopped := false
		t.Cleanup(func() {
			cancel()
			if !bridgeStopped {
				stopBridge()
			}
		})
		type readResult struct {
			n   int
			err error
		}
		readDone := make(chan readResult, 1)
		go func() {
			n, err := content.Read(make([]byte, 8))
			readDone <- readResult{n, err}
		}()
		synctest.Wait()
		if stats := governor.Stats(); stats.Active != 1 || stats.Queued != 1 {
			t.Fatalf("content read did not queue behind the unrelated actual-I/O owner: %+v", stats)
		}
		cancel()
		stopBridge()
		bridgeStopped = true
		// The bridge joins its cancellation callback, while descriptor cleanup
		// remains the content owner's obligation until Close joins it below.
		if !errors.Is(content.Context().Err(), context.Canceled) {
			t.Fatal("bridge returned before its content cancellation callback completed")
		}
		originalPrimaryTestAwait(t, closeEntered)
		read := originalPrimaryTestAwait(t, readDone)
		if read.n != 0 || !errors.Is(read.err, context.Canceled) {
			t.Fatalf("bridge did not cancel queued admission: n=%d err=%v", read.n, read.err)
		}
		if stats := governor.Stats(); stats.Active != 1 || stats.Queued != 0 || owners.Stats().RegisteredOwners != 2 {
			t.Fatalf("cancellation released unrelated I/O or retained content ownership: governor=%+v owners=%+v", stats, owners.Stats())
		}
		if reads, _, closes := source.counts(); reads != 0 || closes != 1 {
			t.Fatalf("queued cancellation touched source data or duplicated cleanup: reads=%d closes=%d", reads, closes)
		}
		closed := make(chan error, 1)
		go func() { closed <- content.Close() }()
		synctest.Wait()
		select {
		case err := <-closed:
			t.Fatalf("content Close returned before descriptor cleanup callback: %v", err)
		default:
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 2 {
			t.Fatalf("unfinished descriptor cleanup retired content ownership: %+v", stats)
		}
		releaseClose()
		if err := originalPrimaryTestAwait(t, closed); err != nil {
			t.Fatal(err)
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 1 {
			t.Fatalf("joined descriptor cleanup did not retire exactly its content owner: %+v", stats)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		if err := holder.Complete(); err != nil {
			t.Fatal(err)
		}
		if owners.Stats().RegisteredOwners != 0 || governor.Stats().Active != 0 {
			t.Fatalf("final actual-owner retirement did not drain: owners=%+v governor=%+v", owners.Stats(), governor.Stats())
		}
		originalPrimaryTestDrain(t, owners)
	})
}
