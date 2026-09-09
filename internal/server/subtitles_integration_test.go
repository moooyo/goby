//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

const subtitleHTTPSRT = "7\r\n00:00:05,000 --> 00:00:12,000\r\nBefore start\r\n\r\n" +
	"9\r\n00:00:10,000 --> 00:00:25,000\r\nAt start with long end\r\n\r\n" +
	"12\r\n00:00:19,000 --> 00:00:21,000\r\nBefore absolute end\r\n\r\n" +
	"15\r\n00:00:20,000 --> 00:00:24,000\r\nAt absolute end\r\n\r\n" +
	"21\r\n00:00:29,000 --> 00:00:35,000\r\nBefore shifted end\r\n\r\n" +
	"28\r\n00:00:30,000 --> 00:00:31,000\r\nAt shifted end\r\n\r\n"

const subtitleHTTPNativeVTT = "WEBVTT\n\n00:01.000 --> 00:02.000\nNative WebVTT\n"

const subtitleHTTPConvertedVTT = "WEBVTT\n\n" +
	"00:05.000 --> 00:12.000\nBefore start\n\n" +
	"00:10.000 --> 00:25.000\nAt start with long end\n\n" +
	"00:19.000 --> 00:21.000\nBefore absolute end\n\n" +
	"00:20.000 --> 00:24.000\nAt absolute end\n\n" +
	"00:29.000 --> 00:35.000\nBefore shifted end\n\n" +
	"00:30.000 --> 00:31.000\nAt shifted end\n"

type subtitleHTTPFixture struct {
	p       *playbackHTTPFixture
	srtPath string
}

func writeSubtitleHTTPFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write subtitle HTTP fixture failed (%T)", err)
	}
}

func newSubtitleHTTPFixture(t *testing.T) *subtitleHTTPFixture {
	t.Helper()
	p := newPlaybackHTTPFixture(t)
	path := strings.TrimSuffix(p.s.video.path, filepath.Ext(p.s.video.path)) + ".en.default.srt"
	writeSubtitleHTTPFile(t, path, subtitleHTTPSRT)
	p.s.rescan(t, p.s.video.libraryID)
	fixture := &subtitleHTTPFixture{p: p, srtPath: path}
	track := subtitleHTTPTrack(t, fixture.detail(t), 6)
	if track["Codec"] != "srt" || track["Language"] != "en" || track["IsDefault"] != true {
		t.Fatal("first external subtitle must follow embedded indexes 2 and 5 with its indexed metadata")
	}
	return fixture
}

func subtitleHTTPObject(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	// DTOs contain filesystem paths and credential URLs; never print their bodies.
	if response.Code != http.StatusOK {
		t.Fatalf("subtitle DTO status = %d, want 200", response.Code)
	}
	var object map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil || object == nil {
		t.Fatal("subtitle DTO must be a JSON object")
	}
	return object
}

func (s *subtitleHTTPFixture) detail(t *testing.T) map[string]any {
	t.Helper()
	p := s.p
	return subtitleHTTPObject(t, p.s.f.request(t, http.MethodGet,
		"/emby/Users/"+p.s.viewerID+"/Items/"+p.s.video.id, nil, p.headers))
}

func subtitleHTTPTrack(t *testing.T, object map[string]any, index int) map[string]any {
	t.Helper()
	streams, ok := object["MediaStreams"].([]any)
	if !ok {
		t.Fatal("media descriptor must include a MediaStreams array")
	}
	for _, value := range streams {
		stream, ok := value.(map[string]any)
		if ok && stream["Index"] == float64(index) {
			return stream
		}
	}
	t.Fatalf("external subtitle index %d is missing", index)
	return nil
}

func subtitleHTTPSource(t *testing.T, object map[string]any) map[string]any {
	t.Helper()
	sources, ok := object["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("item must expose exactly one original MediaSources descriptor")
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatal("media source descriptor must be an object")
	}
	return source
}

func subtitleHTTPDeliveryURL(t *testing.T, s *subtitleHTTPFixture, track map[string]any, index int, format string) string {
	t.Helper()
	for key, want := range map[string]any{
		"Type": "Subtitle", "Index": float64(index), "IsExternal": true,
		"IsTextSubtitleStream": true, "SupportsExternalStream": true, "DeliveryMethod": "External",
	} {
		if track[key] != want {
			t.Errorf("external subtitle field %s does not match its delivery contract", key)
		}
	}
	raw, ok := track["DeliveryUrl"].(string)
	if !ok || raw == "" {
		t.Fatal("external subtitle has no delivery URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		t.Fatal("subtitle delivery URL must be a relative catalog route")
	}
	wantPath := "/Videos/" + s.p.s.video.id + "/" + media.SourceID(s.p.s.video.id) +
		"/Subtitles/" + strconv.Itoa(index) + "/0/Stream." + format
	if parsed.Path != wantPath || parsed.Query().Get("api_key") != s.p.s.token ||
		parsed.Query().Get("Path") != "" || strings.Contains(raw, s.p.s.root) {
		t.Fatal("subtitle delivery URL does not carry the catalog selectors and authenticated token")
	}
	return raw
}

func subtitleHTTPRoute(s *subtitleHTTPFixture, index int, start, format string) string {
	base := "/emby/Videos/" + s.p.s.video.id + "/" + media.SourceID(s.p.s.video.id) + "/Subtitles/" + strconv.Itoa(index) + "/"
	if start != "" {
		base += start + "/"
	}
	return base + "Stream." + format
}

func expectSubtitleHTTPStatus(t *testing.T, response streamHTTPResponse, status int) {
	t.Helper()
	if response.status != status {
		t.Fatalf("subtitle HTTP status = %d, want %d", response.status, status)
	}
}

func expectSubtitleHTTPBody(t *testing.T, response streamHTTPResponse, contentType, body string) {
	t.Helper()
	expectSubtitleHTTPStatus(t, response, http.StatusOK)
	if response.header.Get("Content-Type") != contentType || response.header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Error("subtitle MIME type or byte length does not match its representation")
	}
	if !bytes.Equal(response.body, []byte(body)) {
		t.Error("subtitle response bytes differ from the expected representation")
	}
}

func TestHTTPSubtitleDescriptorsNegotiateAndDeliverCredentialedTracks(t *testing.T) {
	s := newSubtitleHTTPFixture(t)
	p := s.p
	vttPath := strings.TrimSuffix(p.s.video.path, filepath.Ext(p.s.video.path)) + ".fr.vtt"
	writeSubtitleHTTPFile(t, vttPath, subtitleHTTPNativeVTT)
	p.s.rescan(t, p.s.video.libraryID)
	detail := s.detail(t)
	list := subtitleHTTPObject(t, p.s.f.request(t, http.MethodGet,
		"/emby/Items?Ids="+p.s.video.id+"&Fields=MediaStreams,MediaSources", nil, p.headers))
	items, ok := list["Items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal("Fields query must return the requested item")
	}
	listItem, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("Fields query item must be an object")
	}
	for _, item := range []map[string]any{detail, listItem} {
		for _, descriptor := range []map[string]any{item, subtitleHTTPSource(t, item)} {
			for _, test := range []struct {
				index              int
				codec, contentType string
				body               string
			}{{6, "srt", "text/plain", subtitleHTTPSRT}, {7, "vtt", "text/vtt", subtitleHTTPNativeVTT}} {
				track := subtitleHTTPTrack(t, descriptor, test.index)
				if track["Codec"] != test.codec {
					t.Error("subtitle descriptor changed the indexed native codec")
				}
				deliveryURL := subtitleHTTPDeliveryURL(t, s, track, test.index, test.codec)
				get := p.s.request(t, http.MethodGet, deliveryURL, "", nil, nil)
				expectSubtitleHTTPBody(t, get, test.contentType, test.body)
				head := p.s.request(t, http.MethodHead, deliveryURL, "", nil, nil)
				expectSubtitleHTTPStatus(t, head, http.StatusOK)
				if len(head.body) != 0 || head.header.Get("Content-Type") != get.header.Get("Content-Type") ||
					head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("ETag") != get.header.Get("ETag") {
					t.Error("real subtitle HEAD did not retain GET metadata without a body")
				}
			}
		}
	}
	request := matchingPlaybackHTTPBody()
	request["SubtitleStreamIndex"] = 6
	profile := request["DeviceProfile"].(map[string]any)
	profile["SubtitleProfiles"] = []map[string]any{{"Format": "vtt", "Method": "External"}}
	info, source := p.prepare(t, request)
	if source["SupportsDirectPlay"] != true || source["SupportsDirectStream"] != true || source["SupportsTranscoding"] != false {
		t.Error("external subtitle conversion incorrectly disabled original media delivery")
	}
	if _, exists := info["ErrorCode"]; exists {
		t.Error("compatible external subtitle selection returned an error")
	}
	if _, exists := source["DefaultSubtitleStreamIndex"]; exists {
		t.Error("external delivery must not advertise an embedded default subtitle index")
	}
	selected := subtitleHTTPTrack(t, source, 6)
	if selected["Codec"] != "srt" {
		t.Error("negotiated output format overwrote the native SRT codec")
	}
	deliveryURL := subtitleHTTPDeliveryURL(t, s, selected, 6, "vtt")
	converted := p.s.request(t, http.MethodGet, deliveryURL, "", nil, nil)
	expectSubtitleHTTPBody(t, converted, "text/vtt", subtitleHTTPConvertedVTT)
	head := p.s.request(t, http.MethodHead, deliveryURL, "", nil, nil)
	expectSubtitleHTTPStatus(t, head, http.StatusOK)
	if len(head.body) != 0 || head.header.Get("Content-Type") != "text/vtt" || head.header.Get("Content-Length") != strconv.Itoa(len(subtitleHTTPConvertedVTT)) {
		t.Error("converted subtitle HEAD did not expose the converted byte length and MIME type")
	}
}

func TestHTTPSubtitleWindowsUseReferenceOffsetsAndRejectInvalidQueries(t *testing.T) {
	s := newSubtitleHTTPFixture(t)
	p := s.p
	const shiftedSRT = "1\n00:00:00,000 --> 00:00:15,000\nAt start with long end\n\n" +
		"2\n00:00:09,000 --> 00:00:11,000\nBefore absolute end\n\n" +
		"3\n00:00:10,000 --> 00:00:14,000\nAt absolute end\n\n" +
		"4\n00:00:19,000 --> 00:00:25,000\nBefore shifted end\n\n"
	const copiedSRT = "1\n00:00:10,000 --> 00:00:25,000\nAt start with long end\n\n" +
		"2\n00:00:19,000 --> 00:00:21,000\nBefore absolute end\n\n"
	const overriddenSRT = "1\n00:00:05,000 --> 00:00:12,000\nBefore start\n\n" +
		"2\n00:00:10,000 --> 00:00:25,000\nAt start with long end\n\n" +
		"3\n00:00:19,000 --> 00:00:21,000\nBefore absolute end\n\n"
	const shiftedVTT = "WEBVTT\n\n00:00.000 --> 00:15.000\nAt start with long end\n\n" +
		"00:09.000 --> 00:11.000\nBefore absolute end\n\n" +
		"00:10.000 --> 00:14.000\nAt absolute end\n\n" +
		"00:19.000 --> 00:25.000\nBefore shifted end\n"
	const copiedVTT = "WEBVTT\n\n00:10.000 --> 00:25.000\nAt start with long end\n\n" +
		"00:19.000 --> 00:21.000\nBefore absolute end\n"
	for _, test := range []struct {
		name, start, format, query, contentType, body string
	}{
		{"srt-query-false", "", "srt", "?StartPositionTicks=100000000&EndPositionTicks=200000000&CopyTimestamps=false", "text/plain", shiftedSRT},
		{"srt-query-true", "", "srt", "?StartPositionTicks=100000000&EndPositionTicks=200000000&CopyTimestamps=true", "text/plain", copiedSRT},
		{"srt-path-default", "100000000", "srt", "?EndPositionTicks=200000000", "text/plain", shiftedSRT},
		{"srt-query-overrides-path", "100000000", "srt", "?StartPositionTicks=0&EndPositionTicks=200000000&CopyTimestamps=true", "text/plain", overriddenSRT},
		{"vtt-query-false", "", "vtt", "?StartPositionTicks=100000000&EndPositionTicks=200000000&CopyTimestamps=false", "text/vtt", shiftedVTT},
		{"vtt-path-true", "100000000", "vtt", "?EndPositionTicks=200000000&CopyTimestamps=true", "text/vtt", copiedVTT},
	} {
		t.Run(test.name, func(t *testing.T) {
			route := subtitleHTTPRoute(s, 6, test.start, test.format) + test.query
			if test.name == "srt-query-true" {
				route = strings.Replace(route, "/emby/Videos/", "/emby/Items/", 1)
			}
			response := p.s.request(t, http.MethodGet, route, p.s.token, nil, nil)
			expectSubtitleHTTPBody(t, response, test.contentType, test.body)
		})
	}
	for _, test := range []struct {
		name, start, format, query string
		index                      int
		status                     int
	}{
		{"duplicate-query", "", "srt", "?StartPositionTicks=0&StartPositionTicks=1", 6, http.StatusBadRequest},
		{"duplicate-query-case", "", "vtt", "?CopyTimestamps=true&copytimestamps=false", 6, http.StatusBadRequest},
		{"start-overflow", "", "srt", "?StartPositionTicks=9223372036854775808", 6, http.StatusBadRequest},
		{"end-overflow", "", "vtt", "?EndPositionTicks=9223372036854775808", 6, http.StatusBadRequest},
		{"path-overflow", "9223372036854775808", "srt", "", 6, http.StatusBadRequest},
		{"negative-start", "", "srt", "?StartPositionTicks=-1", 6, http.StatusBadRequest},
		{"invalid-copy", "", "srt", "?CopyTimestamps=invalid", 6, http.StatusBadRequest},
		{"negative-index", "", "srt", "", -1, http.StatusBadRequest},
		{"unsupported-format", "", "ass", "", 6, http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := p.s.request(t, http.MethodGet, subtitleHTTPRoute(s, test.index, test.start, test.format)+test.query, p.s.token, nil, nil)
			expectSubtitleHTTPStatus(t, response, test.status)
		})
	}
	overflowIndex := strings.Replace(subtitleHTTPRoute(s, 6, "", "srt"), "/Subtitles/6/", "/Subtitles/2147483648/", 1)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, overflowIndex, p.s.token, nil, nil), http.StatusBadRequest)
}

func TestHTTPSubtitleAuthorizationAndSelectorsPrecedeConditionalResponses(t *testing.T) {
	s := newSubtitleHTTPFixture(t)
	p := s.p
	route := subtitleHTTPRoute(s, 6, "0", "srt")
	initial := p.s.request(t, http.MethodGet, route, p.s.token, nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/plain", subtitleHTTPSRT)
	conditional := http.Header{"If-None-Match": {initial.header.Get("ETag")}}
	cached := p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil)
	expectSubtitleHTTPStatus(t, cached, http.StatusNotModified)
	if len(cached.body) != 0 {
		t.Error("not-modified subtitle response must be empty")
	}
	// Goby intentionally requires a token although the captured reference allowed public subtitle URLs.
	for _, token := range []string{"", "invalid-subtitle-token"} {
		expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, token, conditional, nil), http.StatusUnauthorized)
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, "", conditional, p.s.cookie), http.StatusUnauthorized)
	// Missing sources and track indexes return 404 instead of copying the reference's empty 200.
	wrongSource := strings.Replace(route, "/"+media.SourceID(p.s.video.id)+"/", "/unknown-source/", 1)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, wrongSource, p.s.token, conditional, nil), http.StatusNotFound)
	for _, index := range []int{5, 999} {
		expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, subtitleHTTPRoute(s, index, "0", "srt"), p.s.token, conditional, nil), http.StatusNotFound)
	}
	p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusForbidden)
	p.s.setPolicy(t, p.s.viewerID, true, []string{})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotFound)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodHead, route, p.s.token, conditional, nil), http.StatusNotModified)
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, "UPDATE sessions SET revoked_at = now() WHERE id = $1", p.authSessionID); err != nil {
		t.Fatal("revoke subtitle reader authentication session")
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusUnauthorized)
}

func TestHTTPSubtitleSnapshotsStableIndexesAndRetiredURLs(t *testing.T) {
	s := newSubtitleHTTPFixture(t)
	p := s.p
	originalURL := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, s.detail(t), 6), 6, "srt")
	initial := p.s.request(t, http.MethodGet, originalURL, "", nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/plain", subtitleHTTPSRT)
	oldETag := initial.header.Get("ETag")
	if oldETag == "" {
		t.Fatal("subtitle representation must provide an ETag")
	}
	changed := strings.Replace(subtitleHTTPSRT, "Before start", "Changed caption before start", 1)
	writeSubtitleHTTPFile(t, s.srtPath, changed)
	oldConditional := http.Header{"If-None-Match": {oldETag}}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, originalURL, "", oldConditional, nil), http.StatusServiceUnavailable)
	p.s.rescan(t, p.s.video.libraryID)
	currentURL := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, s.detail(t), 6), 6, "srt")
	if currentURL != originalURL {
		t.Error("changed subtitle content unnecessarily changed its stable stream index")
	}
	current := p.s.request(t, http.MethodGet, currentURL, "", oldConditional, nil)
	expectSubtitleHTTPBody(t, current, "text/plain", changed)
	currentETag := current.header.Get("ETag")
	if currentETag == "" || currentETag == oldETag {
		t.Error("rescanned subtitle content did not change the representation ETag")
	}
	currentConditional := http.Header{"If-None-Match": {currentETag}}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, currentURL, "", currentConditional, nil), http.StatusNotModified)
	// Even unchanged subtitle bytes cannot satisfy a cache validator after the primary media snapshot changes.
	if err := os.WriteFile(p.s.video.path, append(bytes.Clone(p.s.video.data), byte('\n')), 0o600); err != nil {
		t.Fatalf("change primary media fixture failed (%T)", err)
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, currentURL, "", currentConditional, nil), http.StatusServiceUnavailable)
	p.s.rescan(t, p.s.video.libraryID)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, currentURL, "", currentConditional, nil), http.StatusNotModified)
	vttPath := strings.TrimSuffix(p.s.video.path, filepath.Ext(p.s.video.path)) + ".fr.vtt"
	writeSubtitleHTTPFile(t, vttPath, subtitleHTTPNativeVTT)
	p.s.rescan(t, p.s.video.libraryID)
	withVTT := s.detail(t)
	if subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, withVTT, 6), 6, "srt") != currentURL {
		t.Error("adding a VTT sidecar changed the existing SRT URL")
	}
	nativeURL := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, withVTT, 7), 7, "vtt")
	expectSubtitleHTTPBody(t, p.s.request(t, http.MethodGet, nativeURL, "", nil, nil), "text/vtt", subtitleHTTPNativeVTT)
	if err := os.Remove(s.srtPath); err != nil {
		t.Fatalf("remove owned subtitle fixture failed (%T)", err)
	}
	p.s.rescan(t, p.s.video.libraryID)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, originalURL, "", currentConditional, nil), http.StatusNotFound)
	writeSubtitleHTTPFile(t, s.srtPath, changed)
	p.s.rescan(t, p.s.video.libraryID)
	reappeared := s.detail(t)
	newURL := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, reappeared, 8), 8, "srt")
	if newURL == originalURL {
		t.Error("a reappearing subtitle reused a retired delivery URL")
	}
	expectSubtitleHTTPBody(t, p.s.request(t, http.MethodGet, newURL, "", nil, nil), "text/plain", changed)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, originalURL, "", currentConditional, nil), http.StatusNotFound)
	if subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, reappeared, 7), 7, "vtt") != nativeURL {
		t.Error("retiring and restoring SRT changed the unrelated VTT index")
	}
}
