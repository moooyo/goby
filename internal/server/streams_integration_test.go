//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// These deterministic bytes test HTTP delivery, not codec decoding. This prober
// opts into the current source snapshot contract without changing M2 fixtures.
type streamHTTPProber struct{}

func (streamHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }

func (streamHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	result := media.Info{
		ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: media.FileChangeTime(info),
		Size: info.Size(), DurationTicks: 2 * media.TicksPerSecond, Bitrate: 128000,
	}
	if strings.EqualFold(filepath.Ext(file.Name()), ".wav") {
		result.Container = "wav"
		result.Streams = []media.Stream{{Index: 0, Codec: "pcm_s16le", CodecType: "audio", Channels: 2, SampleRate: 48000}}
	} else {
		result.Container = "mov,mp4"
		result.Streams = []media.Stream{{Index: 0, Codec: "h264", CodecType: "video", Width: 160, Height: 90}}
	}
	return result, nil
}

type streamHTTPItem struct {
	id, libraryID, path, route, container, contentType string
	data                                               []byte
}

type streamHTTPFixture struct {
	f                     *serverFixture
	server                *httptest.Server
	client                *http.Client
	root, viewerID, token string
	adminID               string
	cookie                *http.Cookie
	video, audio          streamHTTPItem
}

func newStreamHTTPFixture(t *testing.T) *streamHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	catalog, err := library.New(f.pool, streamHTTPProber{}, []string{root})
	if err != nil {
		t.Fatalf("create versioned stream catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	s := &streamHTTPFixture{f: f, root: root, adminID: f.bootstrap(t)}
	s.cookie, _ = f.adminLogin(t)
	videoBytes := make([]byte, 256)
	for index := range videoBytes {
		videoBytes[index] = byte(index)
	}
	audioBytes := bytes.Repeat([]byte("RIFF predictable original audio bytes\n"), 5)
	s.video = s.addItem(t, "Actual Source Name.mp4", "movies", "Videos", "mp4", "video/mp4", videoBytes)
	s.audio = s.addItem(t, "Actual Audio Name.wav", "music", "Audio", "wav", "audio/wav", audioBytes)
	viewer, err := f.users.CreateUser(f.ctx, "Stream Viewer", "stream-viewer-password", false)
	if err != nil {
		t.Fatalf("create stream viewer: %v", err)
	}
	s.viewerID = viewer.ID
	s.setPolicy(t, s.viewerID, true, []string{s.video.libraryID, s.audio.libraryID})
	s.token = stringValue(t, f.embyLogin(t, "Stream Viewer", "stream-viewer-password"), "AccessToken")
	s.server = httptest.NewServer(f.handler)
	t.Cleanup(s.server.Close)
	s.client = s.server.Client()
	s.client.Timeout = 15 * time.Second
	return s
}

func (s *streamHTTPFixture) addItem(t *testing.T, name, kind, route, container, contentType string, data []byte) streamHTTPItem {
	t.Helper()
	directory := filepath.Join(s.root, kind)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// An old mtime makes the Linux ctime the Last-Modified source and prevents
	// tests from accidentally equating the HTTP date with mtime alone.
	modified := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	collection, err := s.f.app.library.CreateLibrary(s.f.ctx, "Stream "+kind, kind, []string{directory})
	if err != nil {
		t.Fatalf("create stream library: %v", err)
	}
	s.rescan(t, collection.ID)
	items, err := s.f.app.library.QueryItems(s.f.ctx, library.Query{UserID: s.adminID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("query scanned stream item: %v", err)
	}
	for _, item := range items.Items {
		if item.Path == path && !item.IsFolder {
			if item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 || item.Media.Size != int64(len(data)) {
				t.Fatalf("stream fixture lacks a current indexed probe: %+v", item.Media)
			}
			return streamHTTPItem{id: item.ID, libraryID: collection.ID, path: path, route: route, container: container, contentType: contentType, data: bytes.Clone(data)}
		}
	}
	t.Fatal("scanned stream item was not found")
	return streamHTTPItem{}
}

func (s *streamHTTPFixture) rescan(t *testing.T, libraryID string) {
	t.Helper()
	job, err := s.f.app.library.StartScan(s.f.ctx, libraryID)
	if err != nil {
		t.Fatalf("queue stream fixture scan: %v", err)
	}
	deadline := time.NewTimer(15 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		current, err := s.f.app.library.GetJob(s.f.ctx, job.ID)
		if err != nil {
			t.Fatalf("read stream fixture scan: %v", err)
		}
		switch current.Status {
		case "Completed":
			if current.Error != "" || current.Scanned < 1 {
				t.Fatalf("stream fixture scan completed with a warning or no media: %+v", current)
			}
			return
		case "Failed", "Cancelled", "Interrupted":
			t.Fatalf("stream fixture scan did not complete: %+v", current)
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for stream fixture scan")
		case <-s.f.ctx.Done():
			t.Fatal("stream fixture context ended during scan")
		case <-ticker.C:
		}
	}
}

func (s *streamHTTPFixture) setPolicy(t *testing.T, userID string, playback bool, folders []string) {
	t.Helper()
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": folders, "EnableMediaPlayback": playback})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.f.pool.Exec(s.f.ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", userID, policy); err != nil {
		t.Fatalf("update current stream policy: %v", err)
	}
}

type streamHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

func (s *streamHTTPFixture) request(t *testing.T, method, path, token string, headers http.Header, cookie *http.Cookie) streamHTTPResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(s.f.ctx, method, s.server.URL+path, nil)
	if err != nil {
		t.Fatal("construct stream HTTP request")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if token != "" {
		request.Header.Set("X-Emby-Token", token)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := s.client.Do(request)
	if err != nil {
		// URL errors can include an api_key; avoid logging their credential URL.
		t.Fatalf("%s %s transport failed (%T)", method, request.URL.EscapedPath(), err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read stream HTTP response: %v", err)
	}
	return streamHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}
}

func expectStreamStatus(t *testing.T, response streamHTTPResponse, status int) {
	t.Helper()
	if response.status != status {
		t.Fatalf("stream HTTP status = %d, want %d; body = %q", response.status, status, response.body)
	}
}

func expectStreamHeaders(t *testing.T, response streamHTTPResponse, item streamHTTPItem) {
	t.Helper()
	if response.header.Get("Cache-Control") != "private, no-transform" || response.header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("original stream cache or range headers are incorrect: %v", response.header)
	}
	etag := response.header.Get("ETag")
	if len(etag) < 3 || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("original stream requires a quoted strong ETag, got %q", etag)
	}
	info, err := os.Stat(item.path)
	if err != nil {
		t.Fatal(err)
	}
	modified := info.ModTime()
	if changed := time.Unix(0, media.FileChangeTime(info)); changed.After(modified) {
		modified = changed
	}
	if got := response.header.Get("Last-Modified"); got != modified.UTC().Format(http.TimeFormat) {
		t.Errorf("Last-Modified = %q, want max(mtime, ctime) at HTTP precision %q", got, modified.UTC().Format(http.TimeFormat))
	}
}

func TestHTTPOriginalStreamsAliasesFullGETAndRealHEAD(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		for _, prefix := range []string{"/emby/" + item.route, "/emby/" + strings.ToLower(item.route), "/" + item.route, "/" + strings.ToLower(item.route)} {
			for _, alias := range []string{"stream?Static=true", "stream." + item.container + "?Static=true", "original." + item.container} {
				t.Run(prefix+"/"+alias, func(t *testing.T) {
					path := prefix + "/" + item.id + "/" + alias
					get := s.request(t, http.MethodGet, path, s.token, nil, nil)
					expectStreamStatus(t, get, http.StatusOK)
					expectStreamHeaders(t, get, item)
					if !bytes.Equal(get.body, item.data) || get.header.Get("Content-Type") != item.contentType || get.header.Get("Content-Length") != strconv.Itoa(len(item.data)) {
						t.Fatalf("original GET lost source bytes, MIME, or full length: %v", get.header)
					}
					head := s.request(t, http.MethodHead, path, s.token, nil, nil)
					expectStreamStatus(t, head, http.StatusOK)
					if len(head.body) != 0 || head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("ETag") != get.header.Get("ETag") || head.header.Get("Content-Type") != get.header.Get("Content-Type") {
						t.Errorf("real HEAD did not preserve GET metadata without a body: %v, bytes=%d", head.header, len(head.body))
					}
				})
			}
		}
		path := "/emby/" + item.route + "/" + item.id + "/stream?Static=true"
		for _, query := range []string{"&MediaSourceId=" + media.SourceID(item.id), "&DeviceId=integration-device", "&StartTimeTicks=10000000&StartPositionTicks=20000000", "&Path=" + url.QueryEscape("/unrelated/private/file") + "&FileName=other.bin"} {
			response := s.request(t, http.MethodGet, path+query, s.token, nil, nil)
			expectStreamStatus(t, response, http.StatusOK)
			if !bytes.Equal(response.body, item.data) {
				t.Error("source selection or ticks altered original container bytes")
			}
		}
		queryToken := s.request(t, http.MethodGet, path+"&api_key="+url.QueryEscape(s.token), "", nil, nil)
		expectStreamStatus(t, queryToken, http.StatusOK)
		if !bytes.Equal(queryToken.body, item.data) {
			t.Error("api_key authentication changed original bytes")
		}
	}
}

func TestHTTPOriginalStreamsStandardRangesAndConditionals(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		t.Run(item.route, func(t *testing.T) {
			path := "/emby/" + item.route + "/" + item.id + "/stream?Static=true"
			full := s.request(t, http.MethodGet, path, s.token, nil, nil)
			expectStreamStatus(t, full, http.StatusOK)
			for _, test := range []struct {
				name, rangeValue string
				start, end       int
			}{
				{"prefix", "bytes=0-15", 0, 15},
				{"middle", "bytes=17-42", 17, 42},
				{"open-ended", "bytes=100-", 100, len(item.data) - 1},
				{"suffix", "bytes=-33", len(item.data) - 33, len(item.data) - 1},
				{"oversized-suffix", "bytes=-9999", 0, len(item.data) - 1},
			} {
				t.Run(test.name, func(t *testing.T) {
					response := s.request(t, http.MethodGet, path, s.token, http.Header{"Range": {test.rangeValue}}, nil)
					expectStreamStatus(t, response, http.StatusPartialContent)
					expectStreamHeaders(t, response, item)
					wantRange := fmt.Sprintf("bytes %d-%d/%d", test.start, test.end, len(item.data))
					if !bytes.Equal(response.body, item.data[test.start:test.end+1]) || response.header.Get("Content-Range") != wantRange || response.header.Get("Content-Length") != strconv.Itoa(test.end-test.start+1) {
						t.Errorf("standard byte range mismatch: headers=%v body=%x", response.header, response.body)
					}
				})
			}
			multi := s.request(t, http.MethodGet, path, s.token, http.Header{"Range": {"bytes=0-2,10-15"}}, nil)
			expectStreamStatus(t, multi, http.StatusPartialContent)
			expectStreamHeaders(t, multi, item)
			contentType, parameters, err := mime.ParseMediaType(multi.header.Get("Content-Type"))
			if err != nil || contentType != "multipart/byteranges" || parameters["boundary"] == "" {
				t.Fatalf("multi-range response is not multipart/byteranges: %v, %v", multi.header, err)
			}
			reader := multipart.NewReader(bytes.NewReader(multi.body), parameters["boundary"])
			for _, span := range [][2]int{{0, 2}, {10, 15}} {
				part, err := reader.NextPart()
				if err != nil {
					t.Fatalf("read multipart byte range: %v", err)
				}
				body, err := io.ReadAll(part)
				part.Close()
				if err != nil || !bytes.Equal(body, item.data[span[0]:span[1]+1]) || part.Header.Get("Content-Type") != item.contentType || part.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", span[0], span[1], len(item.data)) {
					t.Errorf("multipart byte range mismatch: header=%v body=%x error=%v", part.Header, body, err)
				}
			}
			if _, err := reader.NextPart(); err != io.EOF {
				t.Errorf("multipart response has unexpected extra data: %v", err)
			}
			rangeHead := s.request(t, http.MethodHead, path, s.token, http.Header{"Range": {"bytes=10-19"}}, nil)
			expectStreamStatus(t, rangeHead, http.StatusPartialContent)
			if len(rangeHead.body) != 0 || rangeHead.header.Get("Content-Length") != "10" || rangeHead.header.Get("Content-Range") != fmt.Sprintf("bytes 10-19/%d", len(item.data)) {
				t.Errorf("real range HEAD lost partial metadata or returned bytes: headers=%v body=%q", rangeHead.header, rangeHead.body)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				invalid := s.request(t, method, path, s.token, http.Header{"Range": {"bytes=9999-"}}, nil)
				expectStreamStatus(t, invalid, http.StatusRequestedRangeNotSatisfiable)
				if invalid.header.Get("Content-Range") != fmt.Sprintf("bytes */%d", len(item.data)) || (method == http.MethodHead && len(invalid.body) != 0) {
					t.Errorf("unsatisfiable range does not follow HTTP: headers=%v body=%q", invalid.header, invalid.body)
				}
				for _, validator := range []http.Header{{"If-None-Match": {full.header.Get("ETag")}}, {"If-Modified-Since": {full.header.Get("Last-Modified")}}} {
					conditional := s.request(t, method, path, s.token, validator, nil)
					expectStreamStatus(t, conditional, http.StatusNotModified)
					if len(conditional.body) != 0 || conditional.header.Get("ETag") != full.header.Get("ETag") {
						t.Error("conditional response returned a body or changed the ETag")
					}
				}
			}
			modified, err := http.ParseTime(full.header.Get("Last-Modified"))
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name, validator string
				status          int
			}{
				{"matching-etag", full.header.Get("ETag"), http.StatusPartialContent},
				{"matching-date", full.header.Get("Last-Modified"), http.StatusPartialContent},
				{"stale-etag", `"unrelated-validator"`, http.StatusOK},
				{"stale-date", modified.Add(-time.Second).Format(http.TimeFormat), http.StatusOK},
			} {
				t.Run("if-range-"+test.name, func(t *testing.T) {
					response := s.request(t, http.MethodGet, path, s.token, http.Header{"Range": {"bytes=10-19"}, "If-Range": {test.validator}}, nil)
					expectStreamStatus(t, response, test.status)
					want := item.data
					if test.status == http.StatusPartialContent {
						want = item.data[10:20]
					}
					if !bytes.Equal(response.body, want) {
						t.Error("If-Range returned the wrong full or partial bytes")
					}
				})
			}
			priority := s.request(t, http.MethodGet, path, s.token, http.Header{"If-None-Match": {`"different-validator"`}, "If-Modified-Since": {full.header.Get("Last-Modified")}}, nil)
			expectStreamStatus(t, priority, http.StatusOK)
			if !bytes.Equal(priority.body, item.data) {
				t.Error("If-None-Match did not take precedence over If-Modified-Since")
			}
		})
	}
}

func TestHTTPOriginalStreamsAuthorizationPrecedesCachedResponses(t *testing.T) {
	s := newStreamHTTPFixture(t)
	path := "/emby/Videos/" + s.video.id + "/stream?Static=true"
	cached := s.request(t, http.MethodGet, path, s.token, nil, nil)
	expectStreamStatus(t, cached, http.StatusOK)
	validators := http.Header{"If-None-Match": {cached.header.Get("ETag")}, "If-Modified-Since": {cached.header.Get("Last-Modified")}}
	assertDenied := func(t *testing.T, target, token string, status int, cookie *http.Cookie) streamHTTPResponse {
		t.Helper()
		response := s.request(t, http.MethodGet, target, token, validators, cookie)
		expectStreamStatus(t, response, status)
		if response.header.Get("ETag") != "" || bytes.Equal(response.body, s.video.data) || bytes.Contains(response.body, []byte(s.root)) {
			t.Error("denied stream leaked a validator, media body, or source path")
		}
		return response
	}
	assertDenied(t, path, "", http.StatusUnauthorized, nil)
	assertDenied(t, path, "not-a-session-token", http.StatusUnauthorized, nil)
	assertDenied(t, path, "", http.StatusUnauthorized, s.cookie)
	assertDenied(t, "/emby/Videos/"+s.video.id+"/master.m3u8", "", http.StatusUnauthorized, nil)
	assertDenied(t, path+"&DeviceId=another-device", s.token, http.StatusForbidden, nil)
	s.setPolicy(t, s.viewerID, true, []string{s.audio.libraryID})
	hidden := assertDenied(t, path, s.token, http.StatusNotFound, nil)
	missing := assertDenied(t, "/emby/Videos/missing-media-item/stream?Static=true", s.token, http.StatusNotFound, nil)
	var hiddenBody, missingBody map[string]any
	if json.Unmarshal(hidden.body, &hiddenBody) != nil || json.Unmarshal(missing.body, &missingBody) != nil {
		t.Fatal("hidden and missing media errors must be JSON")
	}
	for _, value := range []map[string]any{hiddenBody, missingBody} {
		status, ok := value["ResponseStatus"].(map[string]any)
		if !ok || status["ErrorCode"] != "not_found" {
			t.Errorf("hidden and missing media must share not_found semantics: %#v", value)
		}
	}
	s.setPolicy(t, s.viewerID, false, []string{s.video.libraryID, s.audio.libraryID})
	assertDenied(t, path, s.token, http.StatusForbidden, nil)
	s.setPolicy(t, s.viewerID, true, []string{s.video.libraryID, s.audio.libraryID})
	restored := s.request(t, http.MethodGet, path, s.token, validators, nil)
	expectStreamStatus(t, restored, http.StatusNotModified)
	if err := s.f.users.Revoke(s.f.ctx, s.token); err != nil {
		t.Fatalf("revoke cached stream token: %v", err)
	}
	assertDenied(t, path, s.token, http.StatusUnauthorized, nil)
	adminToken := stringValue(t, s.f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	s.setPolicy(t, s.adminID, false, []string{s.video.libraryID, s.audio.libraryID})
	assertDenied(t, path, adminToken, http.StatusForbidden, nil)
}

func TestHTTPOriginalStreamsRejectConversionAndInvalidSelectors(t *testing.T) {
	s := newStreamHTTPFixture(t)
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		base := "/emby/" + item.route + "/" + item.id + "/"
		// Both media routes have conversion adapters. This fixture disables
		// their runtime; unsupported outputs are declined, while an original
		// alias combined with Static=false is a contradictory request.
		conversionStatus, originalConversionStatus := http.StatusUnsupportedMediaType, http.StatusBadRequest
		for _, test := range []struct {
			name, suffix string
			status       int
		}{
			{"conversion", "stream?Static=false", conversionStatus},
			{"original-conversion", "original." + item.container + "?Static=false", originalConversionStatus},
			{"codec-request", "stream?VideoCodec=hevc", conversionStatus},
			// HLS has a separate handler; this fixture deliberately disables it.
			{"disabled-hls-service", "master.m3u8?Static=true", http.StatusServiceUnavailable},
			{"real-filename-is-not-an-alias", url.PathEscape(filepath.Base(item.path)) + "?Static=true", http.StatusNotFound},
			{"container-conversion", "stream.avi?Static=true", http.StatusUnsupportedMediaType},
			{"conflicting-container", "stream." + item.container + "?Static=true&Container=avi", http.StatusBadRequest},
			{"wrong-source-id", "stream?Static=true&MediaSourceId=" + item.id, http.StatusNotFound},
			{"another-prefixed-source-id", "stream?Static=true&MediaSourceId=" + media.SourceID("another-media-item"), http.StatusNotFound},
			{"source-is-not-a-path", "stream?Static=true&MediaSourceId=" + url.QueryEscape(item.path), http.StatusNotFound},
			{"source-is-not-a-url", "stream?Static=true&MediaSourceId=" + url.QueryEscape("https://example.invalid/media"), http.StatusNotFound},
			{"bad-static", "stream?Static=invalid", http.StatusBadRequest},
			{"conflicting-query-case", "stream?Static=true&static=FALSE", http.StatusBadRequest},
			{"negative-position", "stream?Static=true&StartTimeTicks=-1", http.StatusBadRequest},
		} {
			t.Run(item.route+"/"+test.name, func(t *testing.T) {
				response := s.request(t, http.MethodGet, base+test.suffix, s.token, nil, nil)
				expectStreamStatus(t, response, test.status)
				if response.header.Get("ETag") != "" || bytes.Contains(response.body, []byte(s.root)) {
					t.Error("invalid selector leaked an indexed validator or filesystem path")
				}
			})
		}
	}
	for _, path := range []string{"/emby/Audio/" + s.video.id + "/original.mp4", "/emby/Videos/" + s.audio.id + "/original.wav"} {
		expectStreamStatus(t, s.request(t, http.MethodGet, path, s.token, nil, nil), http.StatusNotFound)
	}
}

func TestHTTPOriginalStreamsRequireRescanAfterSourceChanges(t *testing.T) {
	for _, mutation := range []string{"inode", "size", "mtime", "ctime-only"} {
		t.Run(mutation, func(t *testing.T) {
			s := newStreamHTTPFixture(t)
			path := "/emby/Videos/" + s.video.id + "/original.mp4"
			cached := s.request(t, http.MethodGet, path, s.token, nil, nil)
			expectStreamStatus(t, cached, http.StatusOK)
			before, err := os.Stat(s.video.path)
			if err != nil {
				t.Fatal(err)
			}
			want := bytes.Clone(s.video.data)
			switch mutation {
			case "inode":
				replacement := s.video.path + ".replacement"
				if err := os.WriteFile(replacement, want, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(replacement, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, s.video.path); err != nil {
					t.Fatal(err)
				}
			case "size":
				want = append(want, []byte("additional source bytes")...)
				if err := os.WriteFile(s.video.path, want, 0o600); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				modified := before.ModTime().Add(time.Minute)
				if err := os.Chtimes(s.video.path, modified, modified); err != nil {
					t.Fatal(err)
				}
			case "ctime-only":
				previous, err := http.ParseTime(cached.header.Get("Last-Modified"))
				if err != nil {
					t.Fatal(err)
				}
				want[0] ^= 0x7f
				// Observe the filesystem's actual ctime crossing the HTTP-second
				// boundary. A wall-clock timer alone can outrun coarse kernel time.
				deadline := time.NewTimer(3 * time.Second)
				ticker := time.NewTicker(10 * time.Millisecond)
				defer deadline.Stop()
				defer ticker.Stop()
				for {
					if err := os.WriteFile(s.video.path, want, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(s.video.path, before.ModTime(), before.ModTime()); err != nil {
						t.Fatal(err)
					}
					observed, err := os.Stat(s.video.path)
					if err != nil {
						t.Fatal(err)
					}
					if time.Unix(0, media.FileChangeTime(observed)).Unix() > previous.Unix() {
						break
					}
					select {
					case <-deadline.C:
						t.Fatal("filesystem ctime did not cross the HTTP-second boundary")
					case <-s.f.ctx.Done():
						t.Fatal("fixture context ended while observing filesystem ctime")
					case <-ticker.C:
					}
				}
			}
			after, err := os.Stat(s.video.path)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "inode" && os.SameFile(before, after) {
				t.Fatal("inode replacement fixture did not replace its inode")
			}
			if mutation == "ctime-only" && (!os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) == media.FileChangeTime(after)) {
				t.Fatal("ctime-only fixture did not preserve inode, size, and mtime while changing ctime")
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				stale := s.request(t, method, path, s.token, http.Header{"If-None-Match": {"*"}, "If-Modified-Since": {cached.header.Get("Last-Modified")}}, nil)
				expectStreamStatus(t, stale, http.StatusServiceUnavailable)
				if stale.header.Get("ETag") != "" || (method == http.MethodHead && len(stale.body) != 0) {
					t.Error("stale media returned a cache validator or HEAD body")
				}
			}
			s.rescan(t, s.video.libraryID)
			fresh := s.request(t, http.MethodGet, path, s.token, http.Header{"If-None-Match": {cached.header.Get("ETag")}}, nil)
			expectStreamStatus(t, fresh, http.StatusOK)
			expectStreamHeaders(t, fresh, s.video)
			if !bytes.Equal(fresh.body, want) || fresh.header.Get("ETag") == cached.header.Get("ETag") {
				t.Error("rescan did not publish the new source bytes and validator at the same item URL")
			}
			if mutation == "ctime-only" {
				conditional := s.request(t, http.MethodGet, path, s.token, http.Header{"If-Modified-Since": {cached.header.Get("Last-Modified")}}, nil)
				expectStreamStatus(t, conditional, http.StatusOK)
				if fresh.header.Get("Last-Modified") == cached.header.Get("Last-Modified") {
					t.Error("ctime change across an HTTP-second boundary did not update Last-Modified")
				}
			}
		})
	}
}

func TestHTTPOriginalStreamsRequireCurrentProbeAfterUpgrade(t *testing.T) {
	s := newStreamHTTPFixture(t)
	path := "/emby/Videos/" + s.video.id + "/stream?Static=true"
	baseline := s.request(t, http.MethodGet, path, s.token, nil, nil)
	expectStreamStatus(t, baseline, http.StatusOK)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE items SET media = jsonb_set(media, '{ProbeVersion}', '0'::jsonb) WHERE id = $1`, s.video.id); err != nil {
		t.Fatalf("mark media probe as predating the source snapshot contract: %v", err)
	}
	expectStreamStatus(t, s.request(t, http.MethodGet, path, s.token, http.Header{"If-None-Match": {"*"}}, nil), http.StatusServiceUnavailable)
	s.rescan(t, s.video.libraryID)
	refreshed := s.request(t, http.MethodGet, path, s.token, nil, nil)
	expectStreamStatus(t, refreshed, http.StatusOK)
	if !bytes.Equal(refreshed.body, s.video.data) {
		t.Error("probe cache upgrade changed original source bytes")
	}
}
