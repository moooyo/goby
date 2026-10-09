//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

type hlsSubtitleExtractionHTTPFixture struct {
	h         *hlsHTTPFixture
	playID    string
	session   *hlsSession
	children  [][]string
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
	fixture := hlsSubtitleExtractionHTTPFixture{h: h, playID: negotiation.PlayID, session: sessions[0], directory: directory}
	for _, track := range tracks {
		playlist := h.request(t, http.MethodGet, track, nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		children := hlsHTTPManifestChildren(playlist.body)
		if len(children) != 4 {
			t.Fatal("embedded subtitles did not expose the four actual media windows")
		}
		fixture.children = append(fixture.children, children)
	}
	// The A/V manager retains its original executable. Only subsequent text
	// extraction uses this real-tool wrapper, giving an independent call count.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(directory, "count-extractions")
	program := "#!/bin/sh\nset -eu\ndirectory=${0%/*}\nprintf x >> \"$directory/calls\"\n" +
		"if [ -e \"$directory/block\" ]; then\n  printf started > \"$directory/started\"\n  while [ -e \"$directory/block\" ]; do sleep 0.02; done\nfi\nexec " + quote(h.ffmpeg) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	h.f.app.cfg.FFmpegPath = wrapper
	return fixture
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
	fixture.h.f.app.hls.subtitleExtractionMu.Lock()
	defer fixture.h.f.app.hls.subtitleExtractionMu.Unlock()
	return fixture.h.f.app.hls.subtitleExtractionBytes
}

// This listener receives only the controlled request. Its completion joins the
// real handler and its defers, including synchronous extraction/process cleanup,
// without waiting on a live runtime's WaitGroup while other requests can enter.
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
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if index == 0 {
			first = response
		}
	}
	if fixture.calls(t) != 1 || fixture.retained() == 0 {
		t.Fatal("four segment windows did not share one successful full-track extraction")
	}
	second := h.request(t, http.MethodGet, fixture.children[1][0], nil, nil)
	expectHLSHTTPStatus(t, second, http.StatusOK)
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
	if fixture.retained() != 0 {
		t.Fatal("authority retirement retained the session's subtitle bytes")
	}
}

func TestHTTPHLSEmbeddedSubtitleCancelledExtractionCannotPopulateCacheAndStopReleasesBytes(t *testing.T) {
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
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(fixture.directory, "started")); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("the controlled extraction did not start")
		}
	}
	cancel()
	select {
	case <-handlerDone:
	case <-deadline.C:
		t.Fatal("the cancelled subtitle handler did not join actual extraction retirement")
	}
	select {
	case <-completed:
	case <-deadline.C:
		t.Fatal("the cancelled HTTP client did not complete")
	}
	if fixture.retained() != 0 || len(h.f.app.subtitleSlots) != 0 {
		t.Fatal("cancelled extraction retained reusable bytes or a request slot")
	}
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, fixture.children[0][0], nil, nil), http.StatusOK)
	if fixture.calls(t) != 2 || fixture.retained() == 0 {
		t.Fatal("a cancelled result was reused or a later successful extraction was not retained")
	}
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {fixture.playID}, "DeviceId": {h.accounts.viewer.deviceID}}.Encode()
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stop, nil, h.accounts.viewer.headers), http.StatusNoContent)
	if fixture.retained() != 0 {
		t.Fatal("Stop did not release retained subtitle bytes")
	}
}

func TestHTTPHLSEmbeddedSubtitleCacheRejectsChangedSource(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	first := h.request(t, http.MethodGet, fixture.children[0][0], nil, nil)
	expectHLSHTTPStatus(t, first, http.StatusOK)
	changed := time.Now().Add(time.Second)
	if err := os.Chtimes(h.path, changed, changed); err != nil {
		t.Fatal(err)
	}
	response := h.request(t, http.MethodGet, fixture.children[0][0], nil, http.Header{"If-None-Match": {first.header.Get("ETag")}})
	if response.status < 400 || fixture.retained() != 0 || fixture.calls(t) != 1 {
		t.Fatal("changed source facts reused cached bytes or retained the obsolete session")
	}
}

func TestHTTPHLSEmbeddedSubtitleRevocationDuringExtractionCannotPublish(t *testing.T) {
	fixture := newHLSSubtitleExtractionHTTPFixture(t)
	h := fixture.h
	block := filepath.Join(fixture.directory, "block")
	if err := os.WriteFile(block, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(h.f.ctx, 10*time.Second)
	defer cancel()
	observed, handlerDone := hlsSubtitleExtractionObservedServer(t, h)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, observed.URL+fixture.children[0][0], nil)
	if err != nil {
		t.Fatal("failed to construct the controlled subtitle request")
	}
	completed := make(chan []byte, 1)
	go func() {
		var data []byte
		response, err := observed.Client().Do(request)
		if err == nil {
			data, _ = io.ReadAll(io.LimitReader(response.Body, 16<<10))
			_ = response.Body.Close()
		}
		completed <- data
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(fixture.directory, "started")); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("the controlled extraction did not start")
		}
	}
	h.policy(t, false)
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-completed:
		if bytes.Contains(body, []byte("English cross segment")) {
			t.Fatal("an extraction completed after revocation reached the response")
		}
	case <-ctx.Done():
		t.Fatal("revoked extraction did not complete its response or cancellation")
	}
	select {
	case <-handlerDone:
	case <-ctx.Done():
		t.Fatal("the revoked subtitle handler did not retire")
	}
	if fixture.retained() != 0 || fixture.calls(t) != 1 {
		t.Fatal("failed final authority retained the completed extraction")
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
