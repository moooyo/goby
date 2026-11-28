//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"github.com/moooyo/goby/internal/subtitle"
)

type hlsSubtitleExtractionHTTPFixture struct {
	h         *hlsHTTPFixture
	playID    string
	children  [][]string
	indexes   []int
	directory string
}

func newHLSSubtitleExtractionHTTPFixture(t *testing.T) hlsSubtitleExtractionHTTPFixture {
	t.Helper()
	h := newHLSHTTPFixture(t)
	directory := t.TempDir()
	english, french := filepath.Join(directory, "english.srt"), filepath.Join(directory, "french.srt")
	for path, text := range map[string]string{english: "English", french: "French"} {
		if err := os.WriteFile(path, []byte("1\n00:00:02,500 --> 00:00:03,500\n"+text+" cross segment\n\n2\n00:00:07,000 --> 00:00:08,000\n"+text+" later\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	embedded := filepath.Join(directory, "embedded.mp4")
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", h.path, "-i", english, "-i", french,
		"-map", "0:v:0", "-map", "0:a:0", "-map", "1:0", "-map", "2:0", "-c:v", "copy", "-c:a", "copy", "-c:s", "mov_text",
		"-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=fra", embedded)
	data, err := os.ReadFile(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	h.item, err = h.f.app.library.GetItem(h.f.ctx, h.accounts.viewer.userID, h.item.ID)
	if err != nil || h.item.Media == nil {
		t.Fatal("the embedded subtitle source was not indexed")
	}
	var indexes []int
	for _, stream := range h.item.Media.Streams {
		if stream.CodecType == "subtitle" && !stream.IsExternal {
			indexes = append(indexes, stream.Index)
		}
	}
	if len(indexes) != 2 {
		t.Fatal("the fixture requires two embedded text tracks")
	}
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	var negotiation struct {
		PlayID string `json:"PlaySessionId"`
	}
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlayID == "" {
		t.Fatal("embedded subtitle playback was not prepared")
	}
	query := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")}, "PlaySessionId": {negotiation.PlayID},
		"MediaSourceId": {media.SourceID(h.item.ID)}, "DeviceId": {h.accounts.viewer.deviceID},
		"SegmentContainer": {"mp4"}, "VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "SegmentLength": {"3"},
		"AllowVideoStreamCopy": {"false"}, "AllowAudioStreamCopy": {"false"}, "SubtitleStreamIndex": {strconv.Itoa(indexes[0])}, "ManifestSubtitles": {"vtt"}}
	master := h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants, tracks := hlsHTTPManifestChildren(master.body), hlsSubtitleRenditionURLs(t, master.body)
	if len(variants) != 1 || len(tracks) != 2 {
		t.Fatal("the fixture did not bind one media producer and two subtitle tracks")
	}
	graph := hlsGeneratedIdentityHTTPGraph{playID: negotiation.PlayID, mainURL: variants[0], owner: h.accounts.viewer}
	hlsGeneratedIdentityHTTPMedia(t, h, &graph)
	sessions := videoHTTPSessions(h, negotiation.PlayID)
	if len(sessions) != 1 {
		t.Fatal("the fixture created multiple HLS registrations")
	}
	fixture := hlsSubtitleExtractionHTTPFixture{h: h, playID: negotiation.PlayID, indexes: indexes, directory: directory}
	for _, track := range tracks {
		playlist := h.request(t, http.MethodGet, track, nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		children := hlsHTTPManifestChildren(playlist.body)
		if len(children) != 4 {
			t.Fatal("embedded subtitles did not expose the four actual media windows")
		}
		fixture.children = append(fixture.children, children)
	}
	countHLSSubtitleExtractions(t, h, directory)
	return fixture
}

func countHLSSubtitleExtractions(t *testing.T, h *hlsHTTPFixture, directory string) {
	t.Helper()
	// The A/V manager retains its original executable. Count only the separate
	// subtitle extraction process used by the HTTP handler.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(directory, "count-extractions")
	program := "#!/bin/sh\nset -eu\ndirectory=${0%/*}\nprintf x >> \"$directory/calls\"\n" +
		"if [ -e \"$directory/block\" ]; then\n  printf started > \"$directory/started\"\n  while [ -e \"$directory/block\" ]; do sleep 0.02; done\nfi\nexec " + quote(h.ffmpeg) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	h.f.app.cfg.FFmpegPath = wrapper
}

func (fixture hlsSubtitleExtractionHTTPFixture) calls(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixture.directory, "calls"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return len(data)
}

func (fixture hlsSubtitleExtractionHTTPFixture) retained() int {
	cache := fixture.h.f.app.subtitleExtractions
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.bytes
}

func (fixture hlsSubtitleExtractionHTTPFixture) running(t *testing.T) <-chan struct{} {
	t.Helper()
	cache := fixture.h.f.app.subtitleExtractions
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fixture.calls(t) != 0 {
			cache.mu.Lock()
			for _, entry := range cache.entries {
				if entry.running && entry.key.item == fixture.h.item.ID {
					cache.mu.Unlock()
					return entry.ready
				}
			}
			cache.mu.Unlock()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the controlled extraction did not start")
	return nil
}

func (fixture hlsSubtitleExtractionHTTPFixture) standalone(index int) string {
	item := fixture.h.item.ID
	return "/emby/Videos/" + item + "/" + media.SourceID(item) + "/Subtitles/" + strconv.Itoa(index) + "/Stream.vtt"
}

func asyncHLSSubtitleHeaders() http.Header {
	return http.Header{"Prefer": {"respond-async"}}
}

func expectSubtitlePreparing(t *testing.T, response hlsHTTPResponse) {
	t.Helper()
	expectHLSHTTPStatus(t, response, http.StatusServiceUnavailable)
	if response.header.Get("X-Goby-Subtitle-Preparing") != "1" || response.header.Get("Retry-After") != "2" || response.header.Get("Cache-Control") != "no-store" || response.header.Get("ETag") != "" {
		t.Fatal("pending subtitle lacked its bounded retry contract or carried a validator")
	}
}

func readReadySubtitle(t *testing.T, h *hlsHTTPFixture, path string, headers http.Header) hlsHTTPResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := h.request(t, http.MethodGet, path, nil, headers)
		if response.header.Get("X-Goby-Subtitle-Preparing") != "1" {
			expectHLSHTTPStatus(t, response, http.StatusOK)
			return response
		}
		expectSubtitlePreparing(t, response)
		if time.Now().After(deadline) {
			t.Fatal("subtitle did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// This listener joins only the controlled HTTP handler, not the shared worker.
func hlsSubtitleExtractionObservedServer(t *testing.T, h *hlsHTTPFixture) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	next := h.server.Config.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		next.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server, done
}

func TestHTTPPlaybackInfoKeepsEmbeddedASSExtractionOffTheHLSStartupPath(t *testing.T) {
	h := newHLSHTTPFixture(t)
	directory := t.TempDir()
	text := filepath.Join(directory, "captions.srt")
	if err := os.WriteFile(text, []byte("1\n00:00:00,500 --> 00:00:02,000\nGoby subtitle fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mkv := filepath.Join(filepath.Dir(h.path), "Embedded.ASS.mkv")
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", h.path, "-i", text,
		"-map", "0:v:0", "-map", "0:a:0", "-map", "1:0", "-c:v", "copy", "-c:a", "copy", "-c:s", "ass", mkv)
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	items, err := h.f.app.library.QueryItems(h.f.ctx, library.Query{UserID: h.accounts.viewer.userID, ParentID: h.libraryID, Recursive: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for _, item := range items.Items {
		if item.Path != mkv || item.Media == nil {
			continue
		}
		h.item = item
		for _, stream := range item.Media.Streams {
			if stream.CodecType == "subtitle" && stream.Codec == "ass" && !stream.IsExternal {
				index = stream.Index
			}
		}
	}
	if index < 0 {
		t.Fatal("the fixture did not index an embedded ASS track")
	}
	countHLSSubtitleExtractions(t, h, directory)
	fixture := hlsSubtitleExtractionHTTPFixture{h: h, directory: directory}
	block := filepath.Join(directory, "block")
	if err := os.WriteFile(block, nil, 0600); err != nil {
		t.Fatal(err)
	}
	direct := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{
		"IsPlayback": true, "EnableDirectPlay": true, "EnableTranscoding": false, "SubtitleStreamIndex": index,
		"DeviceProfile": map[string]any{
			"SupportedMediaTypes": "Video",
			"DirectPlayProfiles":  []map[string]any{{"Type": "Video", "Container": "mkv", "VideoCodec": "h264", "AudioCodec": "aac"}},
			"SubtitleProfiles":    []map[string]any{{"Format": "vtt", "Method": "External"}},
		},
	}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, direct, http.StatusOK)
	if !bytes.Contains(direct.body, []byte(`"SupportsDirectPlay":true`)) || !bytes.Contains(direct.body, []byte(`"DeliveryMethod":"External"`)) || fixture.calls(t) != 0 {
		t.Fatal("original playback did not select embedded external delivery independently of extraction")
	}
	profile := map[string]any{
		"SupportedMediaTypes": "Video",
		"DirectPlayProfiles":  []map[string]any{{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}},
		"TranscodingProfiles": []map[string]any{{"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "Context": "Streaming", "ManifestSubtitles": "vtt", "SegmentLength": 3}},
		"SubtitleProfiles":    []map[string]any{{"Format": "vtt", "Method": "Hls", "Container": "ts"}, {"Format": "vtt", "Method": "External"}},
	}
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{
		"IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "SubtitleStreamIndex": index, "DeviceProfile": profile,
	}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	var negotiation struct {
		ErrorCode    string
		MediaSources []struct {
			TranscodingURL string `json:"TranscodingUrl"`
		}
	}
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.ErrorCode != "" || len(negotiation.MediaSources) != 1 || negotiation.MediaSources[0].TranscodingURL == "" {
		t.Fatal("forced HLS with embedded ASS did not return a usable master URL")
	}
	if fixture.calls(t) != 0 {
		t.Fatal("HLS negotiation extracted the full ASS track before any subtitle was requested")
	}
	master := h.request(t, http.MethodGet, negotiation.MediaSources[0].TranscodingURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	tracks := hlsSubtitleRenditionURLs(t, master.body)
	if len(tracks) != 1 {
		t.Fatal("the negotiated HLS master must retain its selected subtitle rendition")
	}
	list := h.request(t, http.MethodGet, tracks[0], nil, nil)
	expectHLSHTTPStatus(t, list, http.StatusOK)
	segments := hlsHTTPManifestChildren(list.body)
	if len(segments) == 0 || fixture.calls(t) != 0 {
		t.Fatal("subtitle playlist creation read captions or exposed no measured window")
	}
	standalone := fixture.standalone(index)
	expectSubtitlePreparing(t, h.request(t, http.MethodHead, standalone, nil, h.accounts.viewer.headers))
	if fixture.calls(t) != 0 {
		t.Fatal("standalone HEAD started an extraction")
	}
	conditional := asyncHLSSubtitleHeaders()
	conditional.Set("If-None-Match", "*")
	expectSubtitlePreparing(t, h.request(t, http.MethodGet, segments[0], nil, conditional))
	ready := fixture.running(t)
	expectSubtitlePreparing(t, h.request(t, http.MethodGet, standalone, nil, h.accounts.viewer.headers))
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != 1 {
		t.Fatal("missing video rendition")
	}
	mediaList := h.request(t, http.MethodGet, variants[0], nil, nil)
	expectHLSHTTPStatus(t, mediaList, http.StatusOK)
	mediaSegments := hlsHTTPManifestChildren(mediaList.body)
	if len(mediaSegments) == 0 {
		t.Fatal("video ceased to deliver while subtitle extraction was pending")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, mediaSegments[0], nil, nil), http.StatusOK)
	// Keep the real child alive across the previous per-extraction deadline.
	// Merely detaching the request context would still fail at twenty seconds.
	select {
	case <-ready:
		t.Fatal("background ASS extraction stopped before the old foreground deadline")
	case <-time.After(21 * time.Second):
	case <-h.f.ctx.Done():
		t.Fatal("ASS fixture deadline expired")
	}
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	waitSubtitleCache(t, ready)
	vtt := readReadySubtitle(t, h, segments[0], nil)
	download := readReadySubtitle(t, h, standalone, h.accounts.viewer.headers)
	for _, response := range []hlsHTTPResponse{vtt, download} {
		if !bytes.HasPrefix(response.body, []byte("WEBVTT")) || !bytes.Contains(response.body, []byte("Goby subtitle fixture")) {
			t.Fatal("the completed ASS extraction did not render the expected cue")
		}
	}
	if fixture.calls(t) != 1 {
		t.Fatal("standalone and HLS delivery repeated the same whole-track extraction")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodHead, standalone, nil, h.accounts.viewer.headers), http.StatusOK)
}

func TestHTTPHLSEmbeddedSubtitleExtractionReusesSegmentsAndRechecksAuthority(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	for _, child := range fixture.children[0] {
		expectHLSHTTPStatus(t, h.request(t, http.MethodHead, child, nil, nil), http.StatusOK)
	}
	if fixture.calls(t) != 0 || fixture.retained() != 0 {
		t.Fatal("HEAD extracted or retained subtitle content")
	}
	var first hlsHTTPResponse
	for index, child := range fixture.children[0] {
		response := readReadySubtitle(t, h, child, nil)
		if index == 0 {
			first = response
		}
	}
	if fixture.calls(t) != 1 || fixture.retained() == 0 {
		t.Fatal("four segment windows did not share one successful full-track extraction")
	}
	shared := readReadySubtitle(t, h, fixture.standalone(fixture.indexes[0]), h.accounts.second.headers)
	if !bytes.Contains(shared.body, []byte("English cross segment")) || fixture.calls(t) != 1 {
		t.Fatal("an independently authorized client could not reuse the embedded text")
	}
	second := readReadySubtitle(t, h, fixture.children[1][0], nil)
	if fixture.calls(t) != 2 || bytes.Equal(first.body, second.body) {
		t.Fatal("the second embedded track did not receive its own extraction")
	}
	conditional := http.Header{"If-None-Match": {first.header.Get("ETag")}}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, conditional), http.StatusNotModified)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, http.Header{"Range": {"bytes=0-6"}}), http.StatusPartialContent)
	shifted, err := url.Parse(fixture.children[0][0])
	if err != nil {
		t.Fatal(err)
	}
	values := shifted.Query()
	values.Set("SubtitleOffsetTicks", "2500000")
	shifted.RawQuery = values.Encode()
	shiftedResponse := h.request(t, http.MethodGet, shifted.String(), nil, nil)
	expectHLSHTTPStatus(t, shiftedResponse, http.StatusOK)
	originalDocument, originalErr := subtitle.Parse(first.body, subtitle.FormatWebVTT)
	shiftedDocument, shiftedErr := subtitle.Parse(shiftedResponse.body, subtitle.FormatWebVTT)
	if originalErr != nil || shiftedErr != nil || len(originalDocument.Cues) != 1 || len(shiftedDocument.Cues) != 1 ||
		shiftedDocument.Cues[0].StartTicks-originalDocument.Cues[0].StartTicks != 2500000 {
		t.Fatal("cached extraction reused a previous view's subtitle timing")
	}
	if fixture.calls(t) != 2 {
		t.Fatal("conditional, range or offset changes repeated extraction")
	}
	h.policy(t, false)
	if response := h.request(t, http.MethodGet, fixture.children[0][0], nil, conditional); response.status < 400 {
		t.Fatal("a cached extraction bypassed current playback authority")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, fixture.standalone(fixture.indexes[0]), nil, h.accounts.second.headers), http.StatusForbidden)
}

func TestHTTPHLSEmbeddedSubtitleNativeRequestWaitsForTheSharedResult(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	child := fixture.children[0][0]
	block := filepath.Join(fixture.directory, "block")
	if err := os.WriteFile(block, nil, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(block)
	ctx, cancel := context.WithTimeout(h.f.ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+child, nil)
	if err != nil {
		t.Fatal("construct the native-style subtitle request")
	}
	result := make(chan hlsHTTPResponse, 1)
	go func() {
		var received hlsHTTPResponse
		defer func() { result <- received }()
		response, err := h.server.Client().Do(request)
		if err != nil {
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err == nil {
			received = hlsHTTPResponse{status: response.StatusCode, header: response.Header, body: data}
		}
	}()
	ready := fixture.running(t)
	pending := h.request(t, http.MethodGet, child, nil, asyncHLSSubtitleHeaders())
	expectSubtitlePreparing(t, pending)
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if !bytes.Contains(response.body, []byte("X-TIMESTAMP-MAP")) || !bytes.Contains(response.body, []byte("English cross segment")) {
			t.Fatal("the native-style request did not receive the measured HLS caption")
		}
	case <-ctx.Done():
		t.Fatal("the native-style subtitle request did not return after preparation")
	}
	waitSubtitleCache(t, ready)
	if fixture.calls(t) != 1 {
		t.Fatal("native and async requests did not share the same background extraction")
	}
}

func TestHTTPHLSEmbeddedSubtitleExtractionSurvivesCanceledRequestAndPlaybackStop(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	block := filepath.Join(fixture.directory, "block")
	if err := os.WriteFile(block, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.f.ctx)
	defer cancel()
	observed, handlerDone := hlsSubtitleExtractionObservedServer(t, h)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, observed.URL+fixture.children[0][0], nil)
	if err != nil {
		t.Fatal("failed to construct the cancellable subtitle request")
	}
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		response, err := observed.Client().Do(request)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	ready := fixture.running(t)
	cancel()
	waitSubtitleCache(t, handlerDone)
	waitSubtitleCache(t, completed)
	if fixture.retained() != 0 || len(h.f.app.subtitleSlots) != 0 {
		t.Fatal("the canceled client retained a request slot or received incomplete bytes")
	}
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {fixture.playID}, "DeviceId": {h.accounts.viewer.deviceID}}.Encode()
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stop, nil, h.accounts.viewer.headers), http.StatusNoContent)
	select {
	case <-ready:
		t.Fatal("canceling a client or play canceled the source-owned worker")
	default:
	}
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	waitSubtitleCache(t, ready)
	if response := h.request(t, http.MethodGet, fixture.children[0][0], nil, nil); response.status < 400 {
		t.Fatal("a stopped HLS URL was revived by cache readiness")
	}
	readReadySubtitle(t, h, fixture.standalone(fixture.indexes[0]), h.accounts.second.headers)
	if fixture.calls(t) != 1 || fixture.retained() == 0 {
		t.Fatal("another authorized client did not reuse the completed extraction")
	}
}

func TestHTTPHLSEmbeddedSubtitleCacheRejectsChangedSource(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	first := readReadySubtitle(t, h, fixture.children[0][0], nil)
	changed := time.Now().Add(time.Second)
	if err := os.Chtimes(h.path, changed, changed); err != nil {
		t.Fatal(err)
	}
	response := h.request(t, http.MethodGet, fixture.children[0][0], nil, http.Header{"If-None-Match": {first.header.Get("ETag")}})
	if response.status < 400 || fixture.calls(t) != 1 {
		t.Fatal("changed source facts reused cached bytes or started another job for an obsolete request")
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	readReadySubtitle(t, h, fixture.standalone(fixture.indexes[0]), h.accounts.viewer.headers)
	if fixture.calls(t) != 2 {
		t.Fatal("the new indexed source version did not extract independently")
	}
}

func TestHTTPHLSEmbeddedSubtitleRevocationDuringFinalSourceOpenCannotPublish(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	block := filepath.Join(fixture.directory, "block")
	if err := os.WriteFile(block, nil, 0600); err != nil {
		t.Fatal(err)
	}
	expectSubtitlePreparing(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, asyncHLSSubtitleHeaders()))
	ready := fixture.running(t)
	ctx, cancel := context.WithTimeout(h.f.ctx, 5*time.Second)
	defer cancel()
	blocker, err := h.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin fixture source fence")
	}
	defer blocker.Rollback(h.f.ctx)
	if _, err := blocker.Exec(ctx, "LOCK TABLE library_roots IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal("hold the final source lookup")
	}
	var pid int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal("identify the fixture source fence")
	}
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	// The worker must pass its first login check before waiting on this root
	// lookup. Revoke the credential without canceling the worker or its I/O.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := h.f.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1 = ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM items i JOIN library_roots r%')`, pid).Scan(&waiting); err != nil {
			t.Fatal("observe the worker's blocked source lookup")
		}
		if waiting {
			break
		}
		select {
		case <-ready:
			t.Fatal("the worker finished before reaching the final source lookup")
		case <-ctx.Done():
			t.Fatal("the worker did not reach the final source lookup")
		case <-ticker.C:
		}
	}
	if err := h.f.users.Revoke(ctx, h.accounts.viewer.headers.Get("X-Emby-Token")); err != nil {
		t.Fatal("revoke the fixture credential while the source lookup is blocked")
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal("release the fixture source fence")
	}
	waitSubtitleCache(t, ready)
	if fixture.retained() != 0 || fixture.calls(t) != 1 {
		t.Fatal("the worker reused its pre-open login check to publish shared bytes")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, nil), http.StatusUnauthorized)
	readReadySubtitle(t, h, fixture.standalone(fixture.indexes[0]), h.accounts.second.headers)
	if fixture.calls(t) != 2 {
		t.Fatal("another authorized login could not replace the denied work")
	}
}

func TestHTTPHLSEmbeddedSubtitlePolicySnapshotCannotOutlivePermission(t *testing.T) {
	h := newHLSSubtitleExtractionHTTPFixture(t).h
	principal, err := h.f.users.Resolve(h.f.ctx, h.accounts.viewer.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal("resolve the fixture login")
	}
	current, err := h.f.users.RevalidateSessionAuthority(h.f.ctx, principal)
	if err != nil {
		t.Fatal("capture the fixture's original authority")
	}
	if err := h.f.app.recheckSubtitleExtractionAuthority(h.f.ctx, current); err != nil {
		t.Fatal("unchanged authority should still allow subtitle delivery")
	}
	h.policy(t, false)
	if err := h.f.app.recheckSubtitleExtractionAuthority(h.f.ctx, current); !errors.Is(err, library.ErrForbidden) {
		t.Fatal("a login that can still sign in retained stale media authority")
	}
}

func TestHTTPHLSEmbeddedSubtitleShutdownCancelsAndJoinsTheWorker(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	if err := os.WriteFile(filepath.Join(fixture.directory, "block"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	expectSubtitlePreparing(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, asyncHLSSubtitleHeaders()))
	ready := fixture.running(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := h.f.app.Close(ctx); err != nil {
		t.Fatalf("shutdown did not retire the actual subtitle extractor: %v", err)
	}
	select {
	case <-ready:
	default:
		t.Fatal("server closed before the subtitle worker reached its terminal state")
	}
	if fixture.retained() != 0 {
		t.Fatal("a canceled subtitle worker published data on shutdown")
	}
}

func TestHTTPHLSEmbeddedSubtitleSymlinkWrapperPreservesInvocationResources(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	targetDirectory, aliasDirectory := t.TempDir(), t.TempDir()
	target := filepath.Join(targetDirectory, "extractor")
	alias := filepath.Join(aliasDirectory, "ffmpeg")
	for directory, label := range map[string]string{targetDirectory: "Target resource", aliasDirectory: "Alias resource"} {
		data := "WEBVTT\n\n00:02.500 --> 00:03.500\n" + label + "\n"
		if err := os.WriteFile(filepath.Join(directory, "caption.vtt"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	program := "#!/bin/sh\nset -eu\ndirectory=${0%/*}\nprintf x >> \"$directory/calls\"\n" +
		"if [ -e \"$directory/fail\" ]; then exit 7; fi\ncat \"$directory/caption.vtt\"\n"
	if err := os.WriteFile(target, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	h.f.app.cfg.FFmpegPath = alias
	failure := filepath.Join(aliasDirectory, "fail")
	if err := os.WriteFile(failure, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if response := h.request(t, http.MethodGet, fixture.children[0][0], nil, nil); response.status < 400 || fixture.retained() != 0 {
		t.Fatal("the configured alias's extraction failure was bypassed or cached")
	}
	if err := os.Remove(failure); err != nil {
		t.Fatal(err)
	}
	for _, child := range fixture.children[0][:2] {
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if !bytes.Contains(response.body, []byte("Alias resource")) || bytes.Contains(response.body, []byte("Target resource")) {
			t.Fatal("subtitle extraction changed the wrapper's configured $0-relative resource directory")
		}
	}
	calls, err := os.ReadFile(filepath.Join(aliasDirectory, "calls"))
	if err != nil || len(calls) != 3 || fixture.retained() != 0 {
		t.Fatal("alias extraction did not preserve invocation or bypass the cache")
	}
	if _, err := os.Stat(filepath.Join(targetDirectory, "calls")); !os.IsNotExist(err) {
		t.Fatal("subtitle extraction invoked the resolved target instead of the configured alias")
	}
}
