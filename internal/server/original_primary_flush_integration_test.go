//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

type originalPrimaryFlushPolicyState struct {
	lease     *mediaPolicyLease
	leases    int
	retained  bool
	refs      int
	delivered bool
	complete  bool
	failed    bool
}

func originalPrimaryFlushPolicySnapshot(app *Server, lease *mediaPolicyLease) originalPrimaryFlushPolicyState {
	gate := app.playbackPolicyGate()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	state := originalPrimaryFlushPolicyState{lease: lease, leases: len(gate.leases)}
	if lease != nil {
		state.retained = gate.leases[lease.key] == lease
		state.refs = lease.refs
		state.delivered, state.complete, state.failed = lease.delivered, lease.complete, lease.failed
	}
	return state
}

// Every Write accepts all bytes. Only the final transport flush can fail, so
// successful body accounting cannot hide the delivery boundary under test.
type originalPrimaryFlushWriter struct {
	*originalPrimaryIntegrationPolicyWriter
	file           *os.File
	flushErr       error
	flushCalls     int
	writes         int
	accepted       int64
	atFlush        originalPrimaryFlushPolicyState
	flushFileErr   error
	flushBodyBytes int
}

func (w *originalPrimaryFlushWriter) Write(data []byte) (int, error) {
	w.writes++
	w.accepted += int64(len(data))
	_, _ = w.Body.Write(data)
	return len(data), nil
}

func (w *originalPrimaryFlushWriter) FlushError() error {
	w.flushCalls++
	w.atFlush = originalPrimaryFlushPolicySnapshot(w.app, w.lease)
	_, w.flushFileErr = w.file.Stat()
	w.flushBodyBytes = w.Body.Len()
	return w.flushErr
}

func originalPrimaryFlushServe(action func()) (aborted bool) {
	defer func() {
		if result := recover(); result != nil {
			if result != http.ErrAbortHandler {
				panic(result)
			}
			aborted = true
		}
	}()
	action()
	return false
}

func TestOriginalPrimaryFinalFlushPrecedesPolicyDeliveryAndOwnershipRetirement(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	principal, err := fixture.f.users.ResolveWithPeer(fixture.f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	flushFailure := errors.New("controlled final original response flush failure")
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		t.Run(item.route, func(t *testing.T) {
			for _, test := range []struct {
				name       string
				method     string
				ranged     bool
				flushFails bool
			}{
				{name: "full flush failure", method: http.MethodGet, flushFails: true},
				{name: "range flush failure", method: http.MethodGet, ranged: true, flushFails: true},
				{name: "full flush success", method: http.MethodGet},
				{name: "range flush success", method: http.MethodGet, ranged: true},
				{name: "head flush success", method: http.MethodHead},
			} {
				t.Run(test.name, func(t *testing.T) {
					// Range delivery retains an idle lease. Reset it before opening
					// each case so the flush observes a fresh, undelivered policy lease.
					fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
					originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
					request := originalPrimaryIntegrationRequest(fixture, principal, item)
					request.Method = test.method
					if test.ranged {
						request.Header.Set("Range", "bytes=7-19")
					}
					subject := librarySubject(principal, principal.User.ID)
					planning, expected, err := fixture.f.app.library.OpenMediaFor(request.Context(), subject, item.id, "")
					if err != nil {
						t.Fatal(err)
					}
					if err := planning.Close(); err != nil {
						t.Fatalf("close original planning descriptor: %v", err)
					}
					// Preserve the principal on the delivery lifetime before the
					// reader takes ownership and the handler adopts that context.
					file, source, content, err := fixture.f.app.library.OpenOriginalMediaFor(request.Context(), subject, expected.Item.ID, expected.SourceID, expected.ETag)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = content.Close() })
					writer := &originalPrimaryFlushWriter{
						originalPrimaryIntegrationPolicyWriter: &originalPrimaryIntegrationPolicyWriter{
							ResponseRecorder: httptest.NewRecorder(), app: fixture.f.app,
						},
						file: file,
					}
					if test.flushFails {
						writer.flushErr = flushFailure
					}
					aborted := originalPrimaryFlushServe(func() {
						fixture.f.app.serveOriginalMedia(writer, request, file, source, content)
					})
					wantStatus := http.StatusOK
					wantBody := item.data
					wantLength := len(wantBody)
					wantRange := ""
					if test.ranged {
						wantStatus, wantBody, wantLength = http.StatusPartialContent, item.data[7:20], 13
						wantRange = fmt.Sprintf("bytes 7-19/%d", len(item.data))
					}
					if test.method == http.MethodHead {
						wantBody = nil
					}
					if aborted != test.flushFails || writer.flushCalls != 1 {
						t.Fatalf("final flush outcome: aborted=%v flushes=%d; want aborted=%v and one flush", aborted, writer.flushCalls, test.flushFails)
					}
					if writer.Code != wantStatus || !bytes.Equal(writer.Body.Bytes(), wantBody) || writer.accepted != int64(len(wantBody)) || writer.flushBodyBytes != len(wantBody) {
						t.Fatalf("final flush changed accepted response bytes: status=%d body=%d accepted=%d at-flush=%d; want status=%d bytes=%d", writer.Code, writer.Body.Len(), writer.accepted, writer.flushBodyBytes, wantStatus, len(wantBody))
					}
					if writer.Header().Get("Content-Length") != strconv.Itoa(wantLength) || writer.Header().Get("Content-Range") != wantRange || writer.Header().Get("Content-Type") != source.MIMEType || writer.Header().Get("ETag") != expected.ETag || source.ETag != expected.ETag || writer.Header().Get("Accept-Ranges") != "bytes" || writer.Header().Get("Last-Modified") == "" {
						t.Fatal("final flush response lost the authorized original snapshot or range metadata")
					}
					if writer.flushFileErr != nil {
						t.Fatalf("original descriptor retired before final flush: %v", writer.flushFileErr)
					}
					atFlush := writer.atFlush
					if test.method == http.MethodHead {
						if atFlush.lease != nil || atFlush.leases != 0 || writer.writes != 0 {
							t.Fatalf("HEAD acquired delivery policy or wrote source bytes: policy=%+v writes=%d", atFlush, writer.writes)
						}
					} else if atFlush.lease == nil || !atFlush.retained || atFlush.leases != 1 || atFlush.refs != 1 || atFlush.delivered || atFlush.complete || atFlush.failed {
						t.Fatalf("final flush occurred after policy delivery or retirement: %+v", atFlush)
					}
					finished := originalPrimaryFlushPolicySnapshot(fixture.f.app, writer.lease)
					switch {
					case test.method == http.MethodHead:
						if finished.lease != nil || finished.leases != 0 {
							t.Fatalf("HEAD retained delivery policy: %+v", finished)
						}
					case test.flushFails:
						if finished.lease == nil || finished.delivered || !finished.failed || finished.refs != 0 || finished.retained || finished.leases != 0 {
							t.Fatalf("failed final flush marked delivery or retained its policy lease: %+v", finished)
						}
					case test.ranged:
						if finished.lease == nil || !finished.delivered || finished.complete || finished.failed || finished.refs != 0 || !finished.retained || finished.leases != 1 {
							t.Fatalf("successful range flush lost its idle delivery identity: %+v", finished)
						}
					default:
						if finished.lease == nil || !finished.delivered || !finished.complete || finished.failed || finished.refs != 0 || finished.retained || finished.leases != 0 {
							t.Fatalf("successful full flush did not complete and retire delivery: %+v", finished)
						}
					}
					if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
						t.Fatalf("final flush outcome retained the actual original descriptor: %v", err)
					}
					if n, err := content.Read(make([]byte, 1)); n != 0 || !errors.Is(err, primaryio.ErrClosed) {
						t.Fatalf("retired original reader accepted a late read: n=%d err=%v", n, err)
					}
					originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
					fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
					originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
				})
			}
		})
	}
	// Descriptor closure and the reader fence above prove consumer retirement;
	// Store shutdown also joins its retained lifetime and domain-claim callbacks.
	originalPrimaryFlushCloseStore(t, fixture)
}

func originalPrimaryFlushCloseStore(t *testing.T, fixture *streamHTTPFixture) {
	t.Helper()
	for _, shutdown := range []struct {
		name  string
		close func(context.Context) error
	}{
		{name: "media operations", close: fixture.f.app.mediaOperations.Close},
		{name: "task manager", close: fixture.f.app.taskManager.Close},
		{name: "media analysis", close: fixture.f.app.mediaAnalysis.Close},
		{name: "original media Store", close: fixture.f.app.library.Close},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := shutdown.close(ctx)
		cancel()
		if err != nil {
			t.Fatalf("close %s after final original flush cases: %v", shutdown.name, err)
		}
	}
}
