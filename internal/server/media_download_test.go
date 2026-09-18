//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPMediaDownloadsAliasesDispositionAndHEAD(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		for _, base := range []string{"/Items/", "/items/", "/emby/Items/", "/emby/items/"} {
			for _, resource := range []string{"Download", "download", "File", "file"} {
				t.Run(item.route+base+resource, func(t *testing.T) {
					path := base + item.id + "/" + resource
					get := s.request(t, http.MethodGet, path, s.token, nil, nil)
					expectStreamStatus(t, get, http.StatusOK)
					expectDownloadHeaders(t, get, item)
					if !bytes.Equal(get.body, item.data) || get.header.Get("Content-Type") != item.contentType || get.header.Get("Content-Length") != strconv.Itoa(len(item.data)) {
						t.Fatalf("download alias lost original bytes, MIME, or size: %v", get.header)
					}
					disposition, parameters, err := mime.ParseMediaType(get.header.Get("Content-Disposition"))
					wantDisposition := "inline"
					if strings.EqualFold(resource, "Download") {
						wantDisposition = "attachment"
					}
					if err != nil || disposition != wantDisposition {
						t.Fatalf("download disposition = %q, want %q: %v", get.header.Get("Content-Disposition"), wantDisposition, err)
					}
					if wantDisposition == "attachment" && parameters["filename"] != filepath.Base(item.path) {
						t.Errorf("attachment filename = %q, want indexed source basename %q", parameters["filename"], filepath.Base(item.path))
					}
					if strings.Contains(get.header.Get("Content-Disposition"), s.root) {
						t.Error("download disposition exposed its source directory")
					}
					head := s.request(t, http.MethodHead, path, s.token, nil, nil)
					expectStreamStatus(t, head, http.StatusOK)
					for _, name := range []string{"Content-Length", "Content-Type", "Content-Disposition", "ETag", "Last-Modified", "Accept-Ranges"} {
						if head.header.Get(name) != get.header.Get(name) {
							t.Errorf("HEAD %s = %q, want GET metadata %q", name, head.header.Get(name), get.header.Get(name))
						}
					}
					if len(head.body) != 0 {
						t.Error("real download HEAD returned source bytes")
					}
				})
			}
		}
	}
}

func expectDownloadHeaders(t *testing.T, response streamHTTPResponse, item streamHTTPItem) {
	t.Helper()
	if response.header.Get("Cache-Control") != "private, no-cache, no-transform" || response.header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("download did not require private revalidation or expose byte ranges: %v", response.header)
	}
	etag := response.header.Get("ETag")
	if len(etag) < 3 || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("download requires a quoted strong ETag, got %q", etag)
	}
	info, err := os.Stat(item.path)
	if err != nil {
		t.Fatal(err)
	}
	modified := info.ModTime()
	if changed := time.Unix(0, media.FileChangeTime(info)); changed.After(modified) {
		modified = changed
	}
	if response.header.Get("Last-Modified") != modified.UTC().Format(http.TimeFormat) {
		t.Errorf("download Last-Modified does not include the indexed source change time: %q", response.header.Get("Last-Modified"))
	}
}

func TestValidateMediaDownloadRequestRejectsSelectorsBodiesAndUnsafeIDs(t *testing.T) {
	for _, test := range []struct {
		name, id, query string
		contentLength   int64
		transfer        []string
	}{
		{name: "empty-id"},
		{name: "padded-id", id: " item"},
		{name: "overlong-id", id: strings.Repeat("a", 257)},
		{name: "control-id", id: "item\x00"},
		{name: "invalid-utf8-id", id: "item\xff"},
		{name: "slash-id", id: "path/item"},
		{name: "backslash-id", id: `path\item`},
		{name: "unknown-query", id: "item", query: "MediaSourceId="},
		{name: "malformed-query", id: "item", query: "api_key=%"},
		{name: "nonempty-body", id: "item", contentLength: 1},
		{name: "unknown-body-size", id: "item", contentLength: -1},
		{name: "chunked-body", id: "item", transfer: []string{"chunked"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/emby/Items/item/Download", nil)
			request.SetPathValue("Id", test.id)
			request.URL.RawQuery = test.query
			request.ContentLength, request.TransferEncoding = test.contentLength, test.transfer
			if err := validateMediaDownloadRequest(request); !errors.Is(err, library.ErrInvalidInput) {
				t.Fatalf("download request accepted an invalid selector or body: %v", err)
			}
		})
	}
	for _, query := range []string{"", "api_key=token", "x-emby-token=token&x-emby-client=Library%20Browser&x-emby-client-version=1&x-emby-device-id=device&x-emby-device-name=Browser"} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Items/item/Download", nil)
		request.SetPathValue("Id", "item")
		request.URL.RawQuery = query
		if err := validateMediaDownloadRequest(request); err != nil {
			t.Errorf("download rejected ordinary authentication query fields: %v", err)
		}
	}
}

func TestHTTPMediaDownloadsRangesAndConditionals(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, resource := range []string{"Download", "File"} {
		t.Run(resource, func(t *testing.T) {
			path := "/emby/Items/" + s.video.id + "/" + resource
			full := s.request(t, http.MethodGet, path, s.token, nil, nil)
			expectStreamStatus(t, full, http.StatusOK)
			for _, test := range []struct {
				rangeValue string
				start, end int
			}{
				{"bytes=7-22", 7, 22},
				{"bytes=240-", 240, 255},
				{"bytes=-16", 240, 255},
			} {
				response := s.request(t, http.MethodGet, path, s.token, http.Header{"Range": {test.rangeValue}}, nil)
				expectStreamStatus(t, response, http.StatusPartialContent)
				if !bytes.Equal(response.body, s.video.data[test.start:test.end+1]) || response.header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", test.start, test.end, len(s.video.data)) || response.header.Get("Content-Length") != strconv.Itoa(test.end-test.start+1) {
					t.Errorf("download range %q returned incorrect bytes or metadata: %v", test.rangeValue, response.header)
				}
			}
			head := s.request(t, http.MethodHead, path, s.token, http.Header{"Range": {"bytes=7-22"}}, nil)
			expectStreamStatus(t, head, http.StatusPartialContent)
			if len(head.body) != 0 || head.header.Get("Content-Length") != "16" || head.header.Get("Content-Range") != "bytes 7-22/256" {
				t.Errorf("range HEAD returned bytes or lost partial metadata: %v", head.header)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				invalid := s.request(t, method, path, s.token, http.Header{"Range": {"bytes=9999-"}}, nil)
				expectStreamStatus(t, invalid, http.StatusRequestedRangeNotSatisfiable)
				if invalid.header.Get("Content-Range") != "bytes */256" || method == http.MethodHead && len(invalid.body) != 0 {
					t.Error("unsatisfiable download range lost HTTP semantics")
				}
				for _, validators := range []http.Header{{"If-None-Match": {full.header.Get("ETag")}}, {"If-Modified-Since": {full.header.Get("Last-Modified")}}} {
					cached := s.request(t, method, path, s.token, validators, nil)
					expectStreamStatus(t, cached, http.StatusNotModified)
					if len(cached.body) != 0 || cached.header.Get("ETag") != full.header.Get("ETag") {
						t.Error("conditional download returned a body or changed its validator")
					}
				}
			}
			for _, test := range []struct {
				validator string
				status    int
				body      []byte
			}{
				{full.header.Get("ETag"), http.StatusPartialContent, s.video.data[7:23]},
				{`"stale-download"`, http.StatusOK, s.video.data},
			} {
				response := s.request(t, http.MethodGet, path, s.token, http.Header{"Range": {"bytes=7-22"}, "If-Range": {test.validator}}, nil)
				expectStreamStatus(t, response, test.status)
				if !bytes.Equal(response.body, test.body) {
					t.Error("If-Range did not select the expected full or partial source")
				}
			}
		})
	}
}

func TestHTTPMediaDownloadsAcceptOnlyPathIDAndAuthenticationQuery(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, resource := range []string{"Download", "File"} {
		base := "/emby/Items/" + s.video.id + "/" + resource
		for _, query := range []string{
			"Id=" + s.video.id,
			"MediaSourceId=" + media.SourceID(s.video.id),
			"MediaSourceId=",
			"UserId=" + s.viewerID,
			"userid=",
			"Static=true",
			"Container=mp4",
			"FileName=spoofed.mp4",
			"Path=" + url.QueryEscape(s.video.path),
			"DeviceId=integration-device",
			"PlaySessionId=download-play",
			"StartTimeTicks=0",
			"UnknownOption=true",
		} {
			response := s.request(t, http.MethodGet, base+"?"+query, s.token, nil, nil)
			expectStreamStatus(t, response, http.StatusBadRequest)
			if response.header.Get("ETag") != "" || response.header.Get("Content-Disposition") != "" || bytes.Contains(response.body, []byte(s.root)) {
				t.Errorf("rejected download parameter exposed source metadata: %s", strings.SplitN(query, "=", 2)[0])
			}
		}
		for _, query := range []string{
			"api_key=" + url.QueryEscape(s.token),
			"X-Emby-Token=" + url.QueryEscape(s.token) + "&X-Emby-Device-Id=integration-device",
		} {
			response := s.request(t, http.MethodGet, base+"?"+query, "", nil, nil)
			expectStreamStatus(t, response, http.StatusOK)
			if !bytes.Equal(response.body, s.video.data) {
				t.Error("authentication query changed the selected original source")
			}
		}
	}
}

func TestHTTPMediaDownloadsAuthorizeBeforeCacheValidators(t *testing.T) {
	s := newStreamHTTPFixture(t)
	path := "/emby/Items/" + s.video.id + "/Download"
	baseline := s.request(t, http.MethodGet, path, s.token, nil, nil)
	expectStreamStatus(t, baseline, http.StatusOK)
	validators := http.Header{"If-None-Match": {baseline.header.Get("ETag")}, "If-Modified-Since": {baseline.header.Get("Last-Modified")}}
	denied := func(path, token string, status int, cookie *http.Cookie) {
		t.Helper()
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := s.request(t, method, path, token, validators, cookie)
			expectStreamStatus(t, response, status)
			if response.header.Get("ETag") != "" || response.header.Get("Content-Disposition") != "" || bytes.Equal(response.body, s.video.data) || bytes.Contains(response.body, []byte(s.root)) {
				t.Error("denied download exposed its validator, disposition, body, or source directory")
			}
			if method == http.MethodHead && len(response.body) != 0 {
				t.Error("denied download HEAD returned a body")
			}
		}
	}
	denied(path, "", http.StatusUnauthorized, nil)
	denied(path, "invalid-download-token", http.StatusUnauthorized, nil)
	denied(path, "", http.StatusUnauthorized, s.cookie)
	s.setPolicy(t, s.viewerID, true, []string{s.audio.libraryID})
	denied(path, s.token, http.StatusNotFound, nil)
	denied("/emby/Items/missing-download-item/Download", s.token, http.StatusNotFound, nil)
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false}`)
	denied(path, s.token, http.StatusForbidden, nil)
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":true}`)
	expectStreamStatus(t, s.request(t, http.MethodGet, path, s.token, validators, nil), http.StatusNotModified)
	adminToken := stringValue(t, s.f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	setHTTPUserPolicy(t, s.f, s.adminID, `{"EnableContentDownloading":false}`)
	denied(path, adminToken, http.StatusForbidden, nil)
	if err := s.f.users.Revoke(s.f.ctx, s.token); err != nil {
		t.Fatalf("revoke download credential: %v", err)
	}
	denied(path, s.token, http.StatusUnauthorized, nil)
}

func TestHTTPMediaDownloadsIgnorePlaybackBitrateAndStreamPolicies(t *testing.T) {
	s := newStreamHTTPFixture(t)
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":true,
		"EnableMediaPlayback":false,"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,
		"EnableVideoPlaybackTranscoding":false,"RemoteClientBitrateLimit":1,"SimultaneousStreamLimit":1}`)
	principal, err := s.f.users.ResolveEmby(s.f.ctx, s.token)
	if err != nil {
		t.Fatal(err)
	}
	scope := transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		ItemID: "occupied-playback-item", SourceID: "occupied-playback-source"}
	_, release, err := s.f.app.acquireMediaPolicy(s.f.ctx, principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defer s.f.app.releaseMediaPolicy(scope)
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		for _, resource := range []string{"Download", "File"} {
			response := s.request(t, http.MethodGet, "/emby/Items/"+item.id+"/"+resource, s.token, nil, nil)
			expectStreamStatus(t, response, http.StatusOK)
			if !bytes.Equal(response.body, item.data) {
				t.Error("playback denial or occupied stream quota altered download bytes")
			}
		}
	}
	file, source, err := s.f.app.library.OpenDownload(s.f.ctx, s.viewerID, s.video.id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	principal.PeerIP = "198.51.100.23"
	if err := s.f.app.authorizeDownload(s.f.ctx, principal, source); err != nil {
		t.Fatalf("remote playback bitrate policy denied authorized downloading: %v", err)
	}
	if err := s.f.app.checkMediaPolicy(principal, scope); err != nil {
		t.Fatalf("download consumed or completed an unrelated playback lease: %v", err)
	}
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":true}`)
	s.f.app.releaseMediaPolicy(scope)
	expectStreamStatus(t, s.request(t, http.MethodGet, "/emby/Videos/"+s.video.id+"/original.mp4", s.token, nil, nil), http.StatusOK)
	expectStreamStatus(t, s.request(t, http.MethodGet, "/emby/Items/"+s.video.id+"/Download", s.token, nil, nil), http.StatusForbidden)
}

func TestHTTPMediaDownloadsApplicationKeyAuthorityIsIndependent(t *testing.T) {
	s := newStreamHTTPFixture(t)
	adminToken := stringValue(t, s.f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	keys := applicationMediaIssueKeys(t, s.f, adminToken)
	before := applicationMediaSnapshot(t, s.f)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET is_disabled = true,
		policy = '{"EnableAllFolders":false,"EnabledFolders":[],"EnableContentDownloading":false,"EnableMediaPlayback":false}'::jsonb
		WHERE id = ANY($1::text[])`, []string{s.adminID, s.viewerID}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"Download", "File"} {
		path := "/emby/Items/" + s.video.id + "/" + resource
		response := s.request(t, http.MethodGet, path, keys[0].key.Token, http.Header{"Range": {"bytes=7-22"}}, nil)
		expectStreamStatus(t, response, http.StatusPartialContent)
		if !bytes.Equal(response.body, s.video.data[7:23]) {
			t.Error("application download inherited its disabled creator's policy")
		}
		expectStreamStatus(t, s.request(t, http.MethodGet, path+"?UserId="+s.viewerID, keys[0].key.Token, nil, nil), http.StatusBadRequest)
	}
	if _, err := s.f.users.RevokeApplicationKey(s.f.ctx, keys[1].principal, keys[0].key.ID); err != nil {
		t.Fatalf("revoke download application key: %v", err)
	}
	path := "/emby/Items/" + s.video.id + "/Download"
	expectStreamStatus(t, s.request(t, http.MethodGet, path, keys[0].key.Token, nil, nil), http.StatusUnauthorized)
	expectStreamStatus(t, s.request(t, http.MethodGet, path, keys[1].key.Token, nil, nil), http.StatusOK)
	if after := applicationMediaSnapshot(t, s.f); after != before {
		t.Error("application download or revocation modified personal playback data")
	}
}

func TestDownloadAuthorizationRefreshesIdentityPolicyAndSource(t *testing.T) {
	for _, mutation := range []string{"policy", "folder", "disabled", "revoked", "expired", "device", "source"} {
		t.Run(mutation, func(t *testing.T) {
			s := newStreamHTTPFixture(t)
			principal, err := s.f.users.ResolveEmby(s.f.ctx, s.token)
			if err != nil {
				t.Fatal(err)
			}
			file, source, err := s.f.app.library.OpenDownload(s.f.ctx, s.viewerID, s.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := s.f.app.authorizeDownload(s.f.ctx, principal, source); err != nil {
				t.Fatalf("initial download authorization failed: %v", err)
			}
			var want error
			switch mutation {
			case "policy":
				setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableContentDownloading":false}`)
				want = library.ErrForbidden
			case "folder":
				s.setPolicy(t, s.viewerID, true, []string{s.audio.libraryID})
				want = library.ErrNotFound
			case "disabled":
				_, err = s.f.pool.Exec(s.f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", s.viewerID)
				want = identity.ErrUnauthorized
			case "revoked":
				err = s.f.users.Revoke(s.f.ctx, s.token)
				want = identity.ErrUnauthorized
			case "expired":
				_, err = s.f.pool.Exec(s.f.ctx, "UPDATE sessions SET created_at = clock_timestamp() - interval '1 day', expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", principal.SessionID)
				want = identity.ErrUnauthorized
			case "device":
				_, err = s.f.pool.Exec(s.f.ctx, "UPDATE sessions SET device_id = 'rebound-download-device' WHERE id = $1", principal.SessionID)
				want = library.ErrNotFound
			case "source":
				changed := source.ModifiedAt.Add(time.Minute)
				err = os.Chtimes(s.video.path, changed, changed)
				if err == nil {
					s.rescan(t, s.video.libraryID)
				}
				want = library.ErrSourceChanged
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.f.app.authorizeDownload(s.f.ctx, principal, source); !errors.Is(err, want) {
				t.Fatalf("download authorization retained stale %s authority: %v, want %v", mutation, err, want)
			}
		})
	}
}

func TestDownloadWatcherClosesRevokedExpiredAndChangedSources(t *testing.T) {
	for _, mutation := range []string{"policy", "revoked", "expired", "source", "missing-source"} {
		t.Run(mutation, func(t *testing.T) {
			s := newStreamHTTPFixture(t)
			setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}`)
			principal, err := s.f.users.ResolveEmby(s.f.ctx, s.token)
			if err != nil {
				t.Fatal(err)
			}
			file, source, err := s.f.app.library.OpenDownload(s.f.ctx, s.viewerID, s.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			request := httptest.NewRequest(http.MethodGet, "/emby/Items/"+s.video.id+"/Download", nil)
			request = request.WithContext(context.WithValue(s.f.ctx, principalKey, principal))
			work, finish, err := s.f.app.guardDownloadMedia(httptest.NewRecorder(), request, file, source)
			if err != nil {
				t.Fatalf("download watcher inherited playback denial: %v", err)
			}
			defer finish()
			switch mutation {
			case "policy":
				setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":false}`)
			case "revoked":
				err = s.f.users.Revoke(s.f.ctx, s.token)
			case "expired":
				_, err = s.f.pool.Exec(s.f.ctx, "UPDATE sessions SET created_at = clock_timestamp() - interval '1 day', expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", principal.SessionID)
			case "source":
				changed := source.ModifiedAt.Add(time.Minute)
				err = os.Chtimes(s.video.path, changed, changed)
			case "missing-source":
				err = os.Remove(s.video.path)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-work.Done():
			case <-time.After(8 * time.Second):
				t.Fatalf("download watcher left a %s response authorized", mutation)
			}
			// Cancellation and descriptor closure belong to the same watcher;
			// joining it avoids inspecting between those two operations.
			finish()
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("download watcher left the %s source descriptor open: %v", mutation, err)
			}
		})
	}
}

func TestHTTPDownloadRevocationInterruptsBlockedResponse(t *testing.T) {
	s := newStreamHTTPFixture(t)
	originalRevocationSparseSource(t, s)
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}`)
	path := "/emby/Items/" + s.video.id + "/Download"
	opened := originalRevocationOpen(t, s, path, s.token)
	setHTTPUserPolicy(t, s.f, s.viewerID, `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":false}`)
	originalRevocationWaitSlots(t, s, 0)
	originalRevocationAssertAborted(t, opened)
	expectStreamStatus(t, s.request(t, http.MethodHead, path, s.token, nil, nil), http.StatusForbidden)
}
