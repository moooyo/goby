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
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/primaryio"
)

const originalPrimaryDownloadAllowedPolicy = `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false,"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false,"RemoteClientBitrateLimit":1,"SimultaneousStreamLimit":1}`

func TestHTTPPrimaryDownloadsOwnedProtocols(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
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
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		for _, resource := range []string{"Download", "File"} {
			t.Run(item.route+"/"+resource, func(t *testing.T) {
				path := "/emby/Items/" + item.id + "/" + resource
				full := fixture.request(t, http.MethodGet, path, fixture.token, nil, nil)
				expectStreamStatus(t, full, http.StatusOK)
				expectDownloadHeaders(t, full, item)
				originalPrimaryDownloadAssertDisposition(t, full.header, item, resource)
				if !bytes.Equal(full.body, item.data) || full.header.Get("Content-Type") != item.contentType || full.header.Get("Content-Length") != strconv.Itoa(len(item.data)) {
					t.Fatal("owned download lost source bytes, MIME, or representation length")
				}
				originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
				start, end := primaryio.MaxReadChunkBytes-7, primaryio.MaxReadChunkBytes+31
				partial := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", start, end)}}, nil)
				expectStreamStatus(t, partial, http.StatusPartialContent)
				if !bytes.Equal(partial.body, item.data[start:end+1]) || partial.header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, len(item.data)) || partial.header.Get("Content-Length") != strconv.Itoa(end-start+1) || partial.header.Get("ETag") != full.header.Get("ETag") {
					t.Fatal("owned cross-chunk download range changed its source snapshot or bytes")
				}
				originalPrimaryDownloadAssertDisposition(t, partial.header, item, resource)
				originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
				multi := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {fmt.Sprintf("bytes=0-4,%d-%d", start, end)}}, nil)
				expectStreamStatus(t, multi, http.StatusPartialContent)
				kind, parameters, err := mime.ParseMediaType(multi.header.Get("Content-Type"))
				if err != nil || kind != "multipart/byteranges" || parameters["boundary"] == "" || multi.header.Get("Content-Length") != strconv.Itoa(len(multi.body)) || multi.header.Get("ETag") != full.header.Get("ETag") {
					t.Fatal("owned multipart download lost its framing, length, or snapshot")
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
						t.Fatal("owned multipart download returned incorrect native source bytes")
					}
				}
				if _, err := parts.NextPart(); !errors.Is(err, io.EOF) {
					t.Fatalf("owned multipart download has an extra part: %v", err)
				}
				originalPrimaryDownloadAssertDisposition(t, multi.header, item, resource)
				originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
				for _, test := range []struct {
					name, method string
					headers      http.Header
					status       int
				}{
					{"head", http.MethodHead, nil, http.StatusOK},
					{"range-head", http.MethodHead, http.Header{"Range": {"bytes=7-19"}}, http.StatusPartialContent},
					{"conditional-get", http.MethodGet, http.Header{"If-None-Match": {full.header.Get("ETag")}}, http.StatusNotModified},
					{"conditional-head", http.MethodHead, http.Header{"If-Modified-Since": {full.header.Get("Last-Modified")}}, http.StatusNotModified},
				} {
					t.Run(test.name, func(t *testing.T) {
						response := fixture.request(t, test.method, path, fixture.token, test.headers, nil)
						expectStreamStatus(t, response, test.status)
						if len(response.body) != 0 || response.header.Get("ETag") != full.header.Get("ETag") || response.header.Get("Content-Disposition") != full.header.Get("Content-Disposition") {
							t.Fatal("header-only owned download returned bytes or changed authorized metadata")
						}
						if test.name == "head" && response.header.Get("Content-Length") != strconv.Itoa(len(item.data)) || test.name == "range-head" && (response.header.Get("Content-Length") != "13" || response.header.Get("Content-Range") != fmt.Sprintf("bytes 7-19/%d", len(item.data))) {
							t.Fatal("owned download HEAD lost representation or partial-range length")
						}
						originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
					})
				}
				for _, validator := range []string{full.header.Get("ETag"), `"stale-owned-download"`} {
					response := fixture.request(t, http.MethodGet, path, fixture.token, http.Header{"Range": {"bytes=7-19"}, "If-Range": {validator}}, nil)
					status, body := http.StatusOK, item.data
					if validator == full.header.Get("ETag") {
						status, body = http.StatusPartialContent, item.data[7:20]
					}
					expectStreamStatus(t, response, status)
					if !bytes.Equal(response.body, body) {
						t.Fatal("owned download If-Range selected the wrong source representation")
					}
					originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
				}
				originalPrimaryIntegrationAssertPolicy(t, fixture.f.app, 0, false)
			})
		}
	}
	originalPrimaryFlushCloseStore(t, fixture)
}

func TestPrimaryDownloadsFreshDownloadAuthorityAndSnapshot(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
	principal := originalPrimaryDownloadPrincipal(t, fixture)
	lease, release := originalPrimaryDownloadOccupyPlayback(t, fixture, principal)
	before := originalPrimaryFlushPolicySnapshot(fixture.f.app, lease)
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		response := fixture.request(t, http.MethodGet, "/emby/Items/"+item.id+"/Download", fixture.token, nil, nil)
		expectStreamStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.body, item.data) {
			t.Fatal("playback denial or occupied playback policy changed an owned download")
		}
		originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
	}
	for _, mutation := range []string{"download-authority", "source-etag"} {
		t.Run(mutation, func(t *testing.T) {
			request := originalPrimaryDownloadRequest(fixture, principal, fixture.video, "Download")
			planning, expected, err := fixture.f.app.library.OpenDownloadFor(request.Context(), librarySubject(principal, principal.User.ID), fixture.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = planning.Close() })
			status := http.StatusForbidden
			if mutation == "download-authority" {
				setHTTPUserPolicy(t, fixture.f, fixture.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":true,"SimultaneousStreamLimit":1}`)
			} else {
				fixture.video.data = append(bytes.Clone(fixture.video.data), []byte("new owned download snapshot")...)
				if err := os.WriteFile(fixture.video.path, fixture.video.data, 0o600); err != nil {
					t.Fatal(err)
				}
				fixture.rescan(t, fixture.video.libraryID)
				status = http.StatusServiceUnavailable
			}
			response := httptest.NewRecorder()
			fixture.f.app.serveOriginalDownloadSnapshot(response, request, planning, expected)
			if _, err := planning.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("download snapshot did not consume the planning descriptor: %v", err)
			}
			if response.Code != status || response.Header().Get("ETag") != "" || response.Header().Get("Content-Disposition") != "" || bytes.Equal(response.Body.Bytes(), fixture.video.data) {
				t.Fatal("owned download reopen reused stale authority or a different planning publication")
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
			setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
			fresh, current, err := fixture.f.app.library.OpenDownloadFor(request.Context(), librarySubject(principal, principal.User.ID), fixture.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fresh.Close() })
			retry := httptest.NewRecorder()
			fixture.f.app.serveOriginalDownloadSnapshot(retry, request, fresh, current)
			if retry.Code != http.StatusOK || !bytes.Equal(retry.Body.Bytes(), fixture.video.data) || retry.Header().Get("ETag") != current.ETag {
				t.Fatal("rejected download reopen poisoned later delivery of the authorized snapshot")
			}
			if mutation == "source-etag" && current.ETag == expected.ETag {
				t.Fatal("download publication mutation did not change its exact ETag")
			}
			if _, err := fresh.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("successful download retained its planning descriptor: %v", err)
			}
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
		})
	}
	// Cached metadata remains subject to current download permission even when
	// the independently authorized playback path is enabled.
	baseline := fixture.request(t, http.MethodGet, "/emby/Items/"+fixture.video.id+"/File", fixture.token, nil, nil)
	expectStreamStatus(t, baseline, http.StatusOK)
	setHTTPUserPolicy(t, fixture.f, fixture.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":true,"SimultaneousStreamLimit":1}`)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		denied := fixture.request(t, method, "/emby/Items/"+fixture.video.id+"/File", fixture.token, http.Header{"If-None-Match": {baseline.header.Get("ETag")}}, nil)
		expectStreamStatus(t, denied, http.StatusForbidden)
		if denied.header.Get("ETag") != "" || denied.header.Get("Content-Disposition") != "" || bytes.Equal(denied.body, fixture.video.data) || method == http.MethodHead && len(denied.body) != 0 {
			t.Fatal("playback permission or a validator bypassed current download denial")
		}
		originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
	}
	release()
	playback := fixture.request(t, http.MethodGet, "/emby/Videos/"+fixture.video.id+"/original.mp4", fixture.token, nil, nil)
	expectStreamStatus(t, playback, http.StatusOK)
	if !bytes.Equal(playback.body, fixture.video.data) {
		t.Fatal("download denial altered independently permitted original playback")
	}
	originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
	originalPrimaryFlushCloseStore(t, fixture)
}

func TestPrimaryDownloadsBodyAndFinalFlushFailureRetiresOwnedSource(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
	principal := originalPrimaryDownloadPrincipal(t, fixture)
	lease, _ := originalPrimaryDownloadOccupyPlayback(t, fixture, principal)
	before := originalPrimaryFlushPolicySnapshot(fixture.f.app, lease)
	for _, item := range []streamHTTPItem{fixture.video, fixture.audio} {
		for _, test := range []struct {
			name, method string
			ranged, fail bool
		}{
			{"full flush failure", http.MethodGet, false, true},
			{"range flush failure", http.MethodGet, true, true},
			{"full flush success", http.MethodGet, false, false},
			{"range flush success", http.MethodGet, true, false},
			{"head flush success", http.MethodHead, false, false},
		} {
			t.Run(item.route+"/"+test.name, func(t *testing.T) {
				request := originalPrimaryDownloadRequest(fixture, principal, item, "Download")
				request.Method = test.method
				if test.ranged {
					request.Header.Set("Range", "bytes=7-19")
				}
				file, source, content := originalPrimaryDownloadOpenOwned(t, fixture, principal, item, request)
				writer := &originalPrimaryFlushWriter{originalPrimaryIntegrationPolicyWriter: &originalPrimaryIntegrationPolicyWriter{ResponseRecorder: httptest.NewRecorder(), app: fixture.f.app}, file: file}
				if test.fail {
					writer.flushErr = errors.New("controlled final owned download flush failure")
				}
				aborted := originalPrimaryFlushServe(func() { fixture.f.app.serveOriginalDownload(writer, request, file, source, content) })
				status, body, length, contentRange := http.StatusOK, item.data, len(item.data), ""
				if test.ranged {
					status, body, length = http.StatusPartialContent, item.data[7:20], 13
					contentRange = fmt.Sprintf("bytes 7-19/%d", len(item.data))
				}
				if test.method == http.MethodHead {
					body = nil
				}
				if aborted != test.fail || writer.flushCalls != 1 || writer.Code != status || !bytes.Equal(writer.Body.Bytes(), body) || writer.accepted != int64(len(body)) || writer.flushBodyBytes != len(body) {
					t.Fatal("owned download final flush did not preserve accepted bytes and exactly one flush outcome")
				}
				if writer.Header().Get("Content-Length") != strconv.Itoa(length) || writer.Header().Get("Content-Range") != contentRange || writer.Header().Get("ETag") != source.ETag || writer.Header().Get("Content-Type") != source.MIMEType || writer.Header().Get("Cache-Control") != "private, no-cache, no-transform" {
					t.Fatal("owned download final flush lost source metadata")
				}
				originalPrimaryDownloadAssertDisposition(t, writer.Header(), item, "Download")
				if writer.flushFileErr != nil || writer.atFlush != before {
					t.Fatalf("download flush retired its descriptor or changed playback policy: file=%v policy=%+v", writer.flushFileErr, writer.atFlush)
				}
				originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
				originalPrimaryDownloadAssertClosed(t, file, content)
				originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
			})
		}
	}
	for _, failure := range []string{"read-error", "truncated-eof", "admission-closed"} {
		t.Run(failure, func(t *testing.T) {
			request := originalPrimaryDownloadRequest(fixture, principal, fixture.video, "File")
			file, source, err := fixture.f.app.library.OpenDownloadFor(request.Context(), librarySubject(principal, principal.User.ID), fixture.video.id, "")
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
				input.readErr = errors.New("controlled owned download read failure")
			}
			governor, owners, content := originalPrimaryIntegrationReader(t, request.Context(), input)
			if failure == "admission-closed" {
				governor.Close()
			}
			writer := httptest.NewRecorder()
			if !originalPrimaryFlushServe(func() { fixture.f.app.serveOriginalDownload(writer, request, file, source, content) }) || writer.Code != http.StatusOK || writer.Header().Get("Content-Length") != strconv.Itoa(len(fixture.video.data)) || writer.Body.Len() >= len(fixture.video.data) {
				t.Fatal("failed owned download body was reported as a normally completed response")
			}
			if failure == "admission-closed" && writer.Body.Len() != 0 {
				t.Fatal("failed download admission published source bytes")
			}
			if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 || stats.ActiveRoots != 0 || stats.ActiveDomains != 0 {
				t.Fatalf("failed owned download retained actual read charges: %+v", stats)
			}
			if stats := owners.Stats(); stats.RegisteredOwners != 0 {
				t.Fatalf("failed owned download retained descriptor ownership: %+v", stats)
			}
			originalPrimaryDownloadAssertClosed(t, file, content)
			originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
			originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
		})
	}
	originalPrimaryFlushCloseStore(t, fixture)
}

func TestHTTPPrimaryDownloadStoreCloseJoinsActiveResponse(t *testing.T) {
	t.Run("native Store close", func(t *testing.T) {
		fixture := newStreamHTTPFixture(t)
		setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
		principal := originalPrimaryDownloadPrincipal(t, fixture)
		lease, _ := originalPrimaryDownloadOccupyPlayback(t, fixture, principal)
		before := originalPrimaryFlushPolicySnapshot(fixture.f.app, lease)
		originalPrimaryDownloadStopManagers(t, fixture)
		originalRevocationSparseSource(t, fixture)
		opened := originalRevocationOpen(t, fixture, "/emby/Items/"+fixture.video.id+"/Download", fixture.token)
		if len(fixture.f.app.streamSlots) != 1 {
			t.Fatal("actual owned download was not active at Store shutdown")
		}
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fixture.f.app.library.Close(closeContext); err != nil {
			t.Fatalf("Store shutdown did not join the connected download descriptor and retained claim: %v", err)
		}
		originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
		originalRevocationAssertAborted(t, opened)
		originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, lease, before)
	})
	t.Run("queued download revocation", func(t *testing.T) {
		fixture := newStreamHTTPFixture(t)
		setHTTPUserPolicy(t, fixture.f, fixture.viewerID, originalPrimaryDownloadAllowedPolicy)
		principal := originalPrimaryDownloadPrincipal(t, fixture)
		policyLease, _ := originalPrimaryDownloadOccupyPlayback(t, fixture, principal)
		before := originalPrimaryFlushPolicySnapshot(fixture.f.app, policyLease)
		request := originalPrimaryDownloadRequest(fixture, principal, fixture.video, "Download")
		requestContext, cancelRequest := context.WithCancel(request.Context())
		defer cancelRequest()
		request = request.WithContext(requestContext)
		file, source, err := fixture.f.app.library.OpenDownloadFor(request.Context(), librarySubject(principal, principal.User.ID), fixture.video.id, "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		governor, owners := originalPrimaryTestFixture(t)
		holder, err := owners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held, err := holder.Acquire(originalPrimaryTestRoute(), primaryio.Foreground)
		if err != nil {
			_ = holder.Complete()
			t.Fatal(err)
		}
		releaseHolder := sync.OnceFunc(func() {
			if err := held.Release(); err != nil {
				t.Errorf("release queued download admission holder: %v", err)
			}
			if err := holder.Complete(); err != nil {
				t.Errorf("retire queued download admission holder: %v", err)
			}
		})
		t.Cleanup(releaseHolder)
		readerOwner, err := owners.Register(request.Context())
		if err != nil {
			t.Fatal(err)
		}
		input := &originalPrimaryDownloadNativeSource{reader: io.NewSectionReader(file, 0, source.Size), file: file}
		content, err := primaryio.NewReadSeeker(readerOwner, originalPrimaryTestRoute(), primaryio.Foreground, input, primaryio.MaxReadChunkBytes, nil)
		if err != nil {
			_ = input.Close()
			_ = readerOwner.Complete()
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = content.Close() })
		writer := &originalPrimaryDownloadQueuedWriter{ResponseRecorder: httptest.NewRecorder(), headers: make(chan struct{})}
		done := make(chan struct{})
		var aborted bool
		go func() {
			defer close(done)
			aborted = originalPrimaryFlushServe(func() { fixture.f.app.serveOriginalDownload(writer, request, file, source, content) })
		}()
		t.Cleanup(func() {
			cancelRequest()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("join cancelled queued download handler")
			}
		})
		originalPrimaryTestAwait(t, writer.headers)
		originalPrimaryDownloadAwaitQueue(t, governor)
		setHTTPUserPolicy(t, fixture.f, fixture.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":false,"SimultaneousStreamLimit":1}`)
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Fatal("download permission revocation did not cancel queued read admission")
		}
		if !aborted || writer.Code != http.StatusOK || writer.Body.Len() != 0 || input.reads.Load() != 0 {
			t.Fatal("revoked queued download read source bytes or reported normal completion")
		}
		if stats := governor.Stats(); stats.Active != 1 || stats.Queued != 0 || stats.ActiveRoots != 1 || stats.ActiveDomains != 1 {
			t.Fatalf("download cancellation released the unrelated holder or retained its queue: %+v", stats)
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 1 {
			t.Fatalf("queued download cancellation retained its consumer or retired the holder: %+v", stats)
		}
		originalPrimaryDownloadAssertClosed(t, file, content)
		originalPrimaryDownloadAssertPolicyUnchanged(t, fixture, policyLease, before)
		originalPrimaryIntegrationAwaitHTTPDrain(t, fixture)
		releaseHolder()
		if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 || stats.ActiveRoots != 0 || stats.ActiveDomains != 0 {
			t.Fatalf("queued download fixture retained an actual read charge: %+v", stats)
		}
		if stats := owners.Stats(); stats.RegisteredOwners != 0 {
			t.Fatalf("queued download fixture retained ownership: %+v", stats)
		}
		originalPrimaryTestDrain(t, owners)
		originalPrimaryFlushCloseStore(t, fixture)
	})
}

func originalPrimaryDownloadPrincipal(t *testing.T, fixture *streamHTTPFixture) identity.Principal {
	t.Helper()
	principal, err := fixture.f.users.ResolveWithPeer(fixture.f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func originalPrimaryDownloadRequest(fixture *streamHTTPFixture, principal identity.Principal, item streamHTTPItem, resource string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/emby/Items/"+item.id+"/"+resource, nil)
	request.SetPathValue("Id", item.id)
	return request.WithContext(context.WithValue(fixture.f.ctx, principalKey, principal))
}

func originalPrimaryDownloadOpenOwned(t *testing.T, fixture *streamHTTPFixture, principal identity.Principal, item streamHTTPItem, request *http.Request) (*os.File, library.MediaFile, *primaryio.ReadSeeker) {
	t.Helper()
	subject := librarySubject(principal, principal.User.ID)
	planning, expected, err := fixture.f.app.library.OpenDownloadFor(request.Context(), subject, item.id, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := planning.Close(); err != nil {
		t.Fatal(err)
	}
	file, source, content, err := fixture.f.app.library.OpenOriginalDownloadFor(request.Context(), subject, expected.Item.ID, expected.SourceID, expected.ETag)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = content.Close() })
	if source.ETag != expected.ETag {
		t.Fatal("owned download reopened a different planning ETag")
	}
	return file, source, content
}

func originalPrimaryDownloadOccupyPlayback(t *testing.T, fixture *streamHTTPFixture, principal identity.Principal) (*mediaPolicyLease, func()) {
	t.Helper()
	scope := mediaPolicyTestScope(principal, "occupied-download-playback")
	scope.ItemID, scope.SourceID = "occupied-playback-item", "occupied-playback-source"
	work, finish, err := fixture.f.app.acquireMediaPolicy(fixture.f.ctx, principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := mediaPolicyContextLease(work)
	if lease == nil {
		finish()
		t.Fatal("download fixture did not acquire its independent playback lease")
	}
	release := sync.OnceFunc(func() { finish(); fixture.f.app.releaseMediaPolicy(scope) })
	t.Cleanup(release)
	return lease, release
}

func originalPrimaryDownloadAssertPolicyUnchanged(t *testing.T, fixture *streamHTTPFixture, lease *mediaPolicyLease, before originalPrimaryFlushPolicyState) {
	t.Helper()
	if after := originalPrimaryFlushPolicySnapshot(fixture.f.app, lease); after != before {
		t.Fatalf("download changed an independent playback lease: before=%+v after=%+v", before, after)
	}
}

func originalPrimaryDownloadAssertDisposition(t *testing.T, header http.Header, item streamHTTPItem, resource string) {
	t.Helper()
	disposition, parameters, err := mime.ParseMediaType(header.Get("Content-Disposition"))
	want := "inline"
	if resource == "Download" {
		want = "attachment"
	}
	if err != nil || disposition != want || parameters["filename"] != filepath.Base(item.path) {
		t.Fatalf("owned download disposition=%q, want %s with indexed basename", header.Get("Content-Disposition"), want)
	}
}

func originalPrimaryDownloadAssertClosed(t *testing.T, file *os.File, content *primaryio.ReadSeeker) {
	t.Helper()
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("owned download retained its actual source descriptor: %v", err)
	}
	if n, err := content.Read(make([]byte, 1)); n != 0 || !errors.Is(err, primaryio.ErrClosed) {
		t.Fatalf("retired download reader accepted a late read: n=%d err=%v", n, err)
	}
}

func originalPrimaryDownloadStopManagers(t *testing.T, fixture *streamHTTPFixture) {
	t.Helper()
	for _, shutdown := range []struct {
		name  string
		close func(context.Context) error
	}{
		{"media operations", fixture.f.app.mediaOperations.Close},
		{"task manager", fixture.f.app.taskManager.Close},
		{"media analysis", fixture.f.app.mediaAnalysis.Close},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := shutdown.close(ctx)
		cancel()
		if err != nil {
			t.Fatalf("stop %s before deliberate download Store shutdown: %v", shutdown.name, err)
		}
	}
}

type originalPrimaryDownloadNativeSource struct {
	reader *io.SectionReader
	file   *os.File
	reads  atomic.Int64
}

func (s *originalPrimaryDownloadNativeSource) Read(data []byte) (int, error) {
	s.reads.Add(1)
	return s.reader.Read(data)
}
func (s *originalPrimaryDownloadNativeSource) Seek(offset int64, whence int) (int64, error) {
	return s.reader.Seek(offset, whence)
}
func (s *originalPrimaryDownloadNativeSource) Close() error {
	err := s.file.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

type originalPrimaryDownloadQueuedWriter struct {
	*httptest.ResponseRecorder
	headers chan struct{}
	once    sync.Once
}

func (w *originalPrimaryDownloadQueuedWriter) WriteHeader(status int) {
	w.ResponseRecorder.WriteHeader(status)
	w.once.Do(func() { close(w.headers) })
}

func originalPrimaryDownloadAwaitQueue(t *testing.T, governor *primaryio.Governor) {
	t.Helper()
	timer, ticker := time.NewTimer(2*time.Second), time.NewTicker(time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		stats := governor.Stats()
		if stats.Active == 1 && stats.Queued == 1 {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("owned download did not queue behind actual read admission: %+v", stats)
		}
	}
}
