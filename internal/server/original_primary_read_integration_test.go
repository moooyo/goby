//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
)

// These requests enter the registered video/audio handlers, including planning
// and the fresh original descriptor open. Crossing chunk boundaries makes the
// HTTP contract exercise multiple admitted reads rather than a small sniff.
func TestHTTPOriginalPrimaryReadDeliveryAcrossChunks(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	for _, item := range []*streamHTTPItem{&fixture.video, &fixture.audio} {
		item.data = make([]byte, 2*primaryio.MaxReadChunkBytes+193)
		for index := range item.data {
			item.data[index] = byte((index*17 + index/251) % 256)
		}
		if err := os.WriteFile(item.path, item.data, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.rescan(t, item.libraryID)
	}
	principal, err := fixture.f.users.ResolveWithPeer(fixture.f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		t.Run(item.route, func(t *testing.T) {
			path := "/emby/" + item.route + "/" + item.id + "/original." + item.container
			resetPolicy := func() { fixture.f.app.cancelMediaPolicy(principal.SessionID, "") }
			resetPolicy()
			fullPath := path
			if item.route == "Videos" {
				fullPath = "/emby/Videos/" + item.id + "/stream?Static=true"
			}
			full := fixture.request(t, http.MethodGet, fullPath, fixture.token, nil, nil)
			expectStreamStatus(t, full, http.StatusOK)
			expectStreamHeaders(t, full, item)
			if !bytes.Equal(full.body, item.data) || full.header.Get("Content-Length") != strconv.Itoa(len(item.data)) || full.header.Get("Content-Type") != item.contentType {
				t.Fatal("actual original handler lost source bytes or full representation metadata")
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)

			start, end := primaryio.MaxReadChunkBytes-7, primaryio.MaxReadChunkBytes+31
			for _, test := range []struct {
				name, value string
				start, end  int
			}{
				{"cross-chunk", fmt.Sprintf("bytes=%d-%d", start, end), start, end},
				{"suffix", "bytes=-41", len(item.data) - 41, len(item.data) - 1},
			} {
				t.Run(test.name, func(t *testing.T) {
					resetPolicy()
					response := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {test.value}}, nil)
					expectStreamStatus(t, response, http.StatusPartialContent)
					if response.header.Get("ETag") != full.header.Get("ETag") || response.header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", test.start, test.end, len(item.data)) || !bytes.Equal(response.body, item.data[test.start:test.end+1]) {
						t.Fatal("actual original range changed its snapshot or returned the wrong bytes")
					}
					originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
					originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 1, true)
				})
			}

			resetPolicy()
			multi := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {fmt.Sprintf("bytes=0-4,%d-%d", start, end)}}, nil)
			expectStreamStatus(t, multi, http.StatusPartialContent)
			if multi.header.Get("ETag") != full.header.Get("ETag") || multi.header.Get("Content-Length") != strconv.Itoa(len(multi.body)) {
				t.Fatal("multipart original response lost its exact snapshot or aggregate length")
			}
			kind, parameters, err := mime.ParseMediaType(multi.header.Get("Content-Type"))
			if err != nil || kind != "multipart/byteranges" || parameters["boundary"] == "" {
				t.Fatalf("original multipart response is malformed: %v", err)
			}
			parts := multipart.NewReader(bytes.NewReader(multi.body), parameters["boundary"])
			for _, span := range [][2]int{{0, 4}, {start, end}} {
				part, err := parts.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(part)
				_ = part.Close()
				if readErr != nil || !bytes.Equal(body, item.data[span[0]:span[1]+1]) || part.Header.Get("Content-Type") != item.contentType || part.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", span[0], span[1], len(item.data)) {
					t.Fatal("multipart original response returned incorrect source bytes or part metadata")
				}
			}
			if _, err := parts.NextPart(); !errors.Is(err, io.EOF) {
				t.Fatalf("multipart original response has an extra part: %v", err)
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 1, true)

			for _, test := range []struct {
				name, method string
				headers      http.Header
				status       int
			}{
				{"head", http.MethodHead, nil, http.StatusOK},
				{"range-head", http.MethodHead, http.Header{"Range": {"bytes=7-19"}}, http.StatusPartialContent},
				{"etag-conditional", http.MethodGet, http.Header{"If-None-Match": {full.header.Get("ETag")}}, http.StatusNotModified},
				{"date-conditional", http.MethodGet, http.Header{"If-Modified-Since": {full.header.Get("Last-Modified")}}, http.StatusNotModified},
			} {
				t.Run(test.name, func(t *testing.T) {
					resetPolicy()
					response := fixture.request(t, test.method, path, fixture.token, test.headers, nil)
					expectStreamStatus(t, response, test.status)
					if len(response.body) != 0 || response.header.Get("ETag") != full.header.Get("ETag") {
						t.Fatal("header-only original response returned a body or changed its snapshot")
					}
					if test.name == "head" && response.header.Get("Content-Length") != strconv.Itoa(len(item.data)) || test.name == "range-head" && (response.header.Get("Content-Length") != "13" || response.header.Get("Content-Range") != fmt.Sprintf("bytes 7-19/%d", len(item.data))) {
						t.Fatal("HEAD did not preserve its original representation length and range")
					}
					originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
					originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
				})
			}

			for _, test := range []struct {
				name, validator string
				status          int
			}{
				{"matching-if-range", full.header.Get("ETag"), http.StatusPartialContent},
				{"stale-if-range", `"stale-original"`, http.StatusOK},
			} {
				t.Run(test.name, func(t *testing.T) {
					resetPolicy()
					response := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {"bytes=7-19"}, "If-Range": {test.validator}}, nil)
					expectStreamStatus(t, response, test.status)
					want := item.data
					if test.status == http.StatusPartialContent {
						want = want[7:20]
					}
					if !bytes.Equal(response.body, want) {
						t.Fatal("actual original If-Range returned the wrong source representation")
					}
					originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
					// A Range request retains its delivery identity even when an
					// unmatched validator causes ServeContent to send the full file.
					originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 1, true)
				})
			}
			resetPolicy()
		})
	}
}

func TestHTTPOriginalPrimaryReadRechecksAuthorityBeforeConditionalResponse(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		path := "/emby/" + item.route + "/" + item.id + "/original." + item.container
		full := fixture.request(t, http.MethodGet, path, fixture.token, nil, nil)
		expectStreamStatus(t, full, http.StatusOK)
		conditional := http.Header{"If-None-Match": {full.header.Get("ETag")}}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			denied := fixture.request(t, method, path, "", conditional, nil)
			expectStreamStatus(t, denied, http.StatusUnauthorized)
			if denied.header.Get("ETag") != "" || denied.header.Get("Content-Range") != "" || bytes.Equal(denied.body, item.data) {
				t.Fatal("unauthenticated original conditional request disclosed the media snapshot")
			}
		}
		fixture.setPolicy(t, fixture.viewerID, false, []string{fixture.video.libraryID, fixture.audio.libraryID})
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			denied := fixture.request(t, method, path, fixture.token, conditional, nil)
			expectStreamStatus(t, denied, http.StatusForbidden)
			if denied.header.Get("ETag") != "" || denied.header.Get("Content-Range") != "" || bytes.Equal(denied.body, item.data) {
				t.Fatal("current playback denial was bypassed by an original cache validator")
			}
		}
		fixture.setPolicy(t, fixture.viewerID, true, []string{fixture.video.libraryID, fixture.audio.libraryID})
		expectStreamStatus(t, fixture.request(t, http.MethodGet, path, fixture.token, conditional, nil), http.StatusNotModified)
		originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
		originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
	}
}

func TestHTTPOriginalPrimaryReadStoreCloseJoinsRealDescriptorAndClaims(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	// The catalog is last in production shutdown: its operation managers must
	// stop before this test deliberately fences new catalog work.
	if err := fixture.f.app.mediaOperations.Close(fixture.f.ctx); err != nil {
		t.Fatalf("stop media operations before deliberate Store shutdown: %v", err)
	}
	if err := fixture.f.app.taskManager.Close(fixture.f.ctx); err != nil {
		t.Fatalf("stop task manager before deliberate Store shutdown: %v", err)
	}
	if err := fixture.f.app.mediaAnalysis.Close(fixture.f.ctx); err != nil {
		t.Fatalf("stop media analysis before deliberate Store shutdown: %v", err)
	}
	target := originalRevocationSparseSource(t, fixture)
	opened := originalRevocationOpen(t, fixture, target, fixture.token)
	if len(fixture.f.app.streamSlots) != 1 {
		t.Fatal("original production response did not remain active before Store cancellation")
	}
	// Store.Close can only finish after its retained response registration has
	// joined actual reads and descriptor cleanup. Keeping the client connected
	// also requires cancellation to reach the blocked HTTP write independently.
	closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fixture.f.app.library.Close(closeContext); err != nil {
		t.Fatalf("Store shutdown retained the actual original response descriptor or claim: %v", err)
	}
	originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
	originalRevocationAssertAborted(t, opened)
	originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
}

func TestOriginalPrimaryReadSnapshotClosesPlanningDescriptorAndRejectsChangedETag(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	principal, err := fixture.f.users.ResolveWithPeer(fixture.f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		t.Run(item.route, func(t *testing.T) {
			planning, source, err := fixture.f.app.library.OpenMediaFor(fixture.f.ctx, librarySubject(principal, principal.User.ID), item.id, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = planning.Close() })
			changed := append(bytes.Clone(item.data), []byte("new indexed snapshot")...)
			if err := os.WriteFile(item.path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			fixture.rescan(t, item.libraryID)
			request := originalPrimaryIntegrationRequest(fixture, principal, item)
			response := httptest.NewRecorder()
			fixture.f.app.serveOriginalMediaSnapshot(response, request, planning, source)
			if _, err := planning.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("planning descriptor was not consumed before reopening: %v", err)
			}
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("ETag") != "" || bytes.Equal(response.Body.Bytes(), changed) {
				t.Fatal("original reopen accepted a different publication than the planning ETag")
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
			fresh, current, err := fixture.f.app.library.OpenMediaFor(fixture.f.ctx, librarySubject(principal, principal.User.ID), item.id, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fresh.Close() })
			retry := httptest.NewRecorder()
			fixture.f.app.serveOriginalMediaSnapshot(retry, request, fresh, current)
			if retry.Code != http.StatusOK || !bytes.Equal(retry.Body.Bytes(), changed) || retry.Header().Get("ETag") == source.ETag || retry.Header().Get("ETag") != current.ETag {
				t.Fatal("failed reopen poisoned later delivery of the current exact snapshot")
			}
			if _, err := fresh.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("successful original retry retained its planning descriptor: %v", err)
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
		})
	}
}

// The authorized catalog descriptor remains real; only its body reader is
// controlled. This isolates failure after HTTP headers from source-open errors.
type originalPrimaryIntegrationSource struct {
	reader  *io.SectionReader
	file    *os.File
	readErr error
}

func (s *originalPrimaryIntegrationSource) Read(data []byte) (int, error) {
	if s.readErr != nil {
		n, _ := s.reader.Read(data[:min(len(data), 37)])
		return n, s.readErr
	}
	return s.reader.Read(data)
}

func (s *originalPrimaryIntegrationSource) Seek(offset int64, whence int) (int64, error) {
	return s.reader.Seek(offset, whence)
}

func (s *originalPrimaryIntegrationSource) Close() error {
	err := s.file.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

type originalPrimaryIntegrationPolicyWriter struct {
	*httptest.ResponseRecorder
	app   *Server
	lease *mediaPolicyLease
}

func (w *originalPrimaryIntegrationPolicyWriter) WriteHeader(status int) {
	gate := w.app.playbackPolicyGate()
	gate.mu.Lock()
	for _, lease := range gate.leases {
		w.lease = lease
	}
	gate.mu.Unlock()
	w.ResponseRecorder.WriteHeader(status)
}

func TestOriginalPrimaryReadBodyFailureAbortsAndDoesNotCompleteDelivery(t *testing.T) {
	for _, failure := range []string{"read-error", "truncated-eof", "admission-closed"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newStreamHTTPFixture(t)
			principal, err := fixture.f.users.ResolveWithPeer(fixture.f.ctx, fixture.token, "emby", "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			file, source, err := fixture.f.app.library.OpenMediaFor(fixture.f.ctx, librarySubject(principal, principal.User.ID), fixture.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			body := fixture.video.data
			if failure == "truncated-eof" {
				body = body[:len(body)/2]
			}
			input := &originalPrimaryIntegrationSource{reader: io.NewSectionReader(bytes.NewReader(body), 0, source.Size), file: file}
			if failure == "read-error" {
				input.readErr = errors.New("controlled original source read failure")
			}
			request := originalPrimaryIntegrationRequest(fixture, principal, fixture.video)
			governor, owners, content := originalPrimaryIntegrationReader(t, request.Context(), input)
			if failure == "admission-closed" {
				governor.Close()
			}
			writer := &originalPrimaryIntegrationPolicyWriter{ResponseRecorder: httptest.NewRecorder(), app: fixture.f.app}
			aborted := func() (aborted bool) {
				defer func() {
					if result := recover(); result != nil {
						if result != http.ErrAbortHandler {
							panic(result)
						}
						aborted = true
					}
				}()
				fixture.f.app.serveOriginalMedia(writer, request, file, source, content)
				return false
			}()
			if !aborted || writer.Code != http.StatusOK || writer.Header().Get("Content-Length") != strconv.Itoa(len(fixture.video.data)) || writer.Body.Len() >= len(fixture.video.data) {
				t.Fatal("failed original body was reported as a normally completed representation")
			}
			if failure == "admission-closed" && writer.Body.Len() != 0 {
				t.Fatal("failed read admission allowed original source bytes to escape")
			}
			gate := fixture.f.app.playbackPolicyGate()
			gate.mu.Lock()
			lease := writer.lease
			failed := lease != nil && lease.failed && !lease.delivered && lease.refs == 0
			leases := len(gate.leases)
			gate.mu.Unlock()
			if !failed || leases != 0 {
				t.Fatal("read/admission failure or short EOF marked successful media delivery or retained its policy claim")
			}
			if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 || stats.ActiveRoots != 0 || stats.ActiveDomains != 0 {
				t.Fatalf("failed original response retained actual read charges: %+v", stats)
			}
			if stats := owners.Stats(); stats.RegisteredOwners != 0 {
				t.Fatalf("failed original response retained descriptor ownership: %+v", stats)
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("failed original response retained its source descriptor: %v", err)
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
		})
	}
}

func originalPrimaryIntegrationReader(t *testing.T, ctx context.Context, source primaryio.ReadSeekCloser) (*primaryio.Governor, *primaryio.OwnerRuntime, *primaryio.ReadSeeker) {
	t.Helper()
	governor, err := primaryio.NewGovernor(primaryio.Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 1, RootQueued: 1, DomainQueued: 1})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := primaryio.NewOwnerRuntime(governor, 1)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := owners.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	content, err := primaryio.NewReadSeeker(owner, primaryio.Route{Roots: []primaryio.RootKey{{Catalog: "original-http-test", RootID: "root"}}, Domains: []string{"configured-domain"}}, primaryio.Foreground, source, primaryio.MaxReadChunkBytes, nil)
	if err != nil {
		_ = source.Close()
		_ = owner.Complete()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := content.Close(); err != nil {
			t.Errorf("retire controlled original reader: %v", err)
		}
		closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := owners.Close(closeContext); err != nil {
			t.Errorf("drain controlled original ownership: %v", err)
		}
		governor.Close()
	})
	return governor, owners, content
}

func originalPrimaryIntegrationRequest(fixture *streamHTTPFixture, principal identity.Principal, item streamHTTPItem) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/emby/"+item.route+"/"+item.id+"/original."+item.container, nil)
	return request.WithContext(context.WithValue(fixture.f.ctx, principalKey, principal))
}

func originalPrimaryIntegrationAwaitHTTPDrain(t *testing.T, fixture *streamHTTPFixture) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		fixture.f.app.originals.mu.Lock()
		owners, sources := len(fixture.f.app.originals.owners), len(fixture.f.app.originals.sources)
		fixture.f.app.originals.mu.Unlock()
		if owners == 0 && sources == 0 && len(fixture.f.app.streamSlots) == 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("completed original handler retained resources: owners=%d sources=%d slots=%d", owners, sources, len(fixture.f.app.streamSlots))
		}
	}
}

func originalPrimaryIntegrationAssertPolicy(t *testing.T, app *Server, count int, delivered bool) {
	t.Helper()
	gate := app.playbackPolicyGate()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if len(gate.leases) != count {
		t.Fatalf("original media policy leases=%d, want %d", len(gate.leases), count)
	}
	for _, lease := range gate.leases {
		if lease.refs != 0 || lease.delivered != delivered || lease.complete || lease.failed {
			t.Fatalf("original media policy retained incorrect delivery state: refs=%d delivered=%v complete=%v failed=%v", lease.refs, lease.delivered, lease.complete, lease.failed)
		}
	}
}
