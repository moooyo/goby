//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedIdentityHTTPGraph struct {
	playID, mainURL, producerID string
	mapURL, subtitleURL         string
	mediaURLs, subtitleURLs     []string
	session                     *hlsSession
	owner                       clientSessionHTTPLogin
}

func hlsGeneratedIdentityHTTPPrepare(t *testing.T, h *hlsHTTPFixture, owners ...clientSessionHTTPLogin) hlsGeneratedIdentityHTTPGraph {
	t.Helper()
	owner := h.accounts.viewer
	if len(owners) > 1 {
		t.Fatal("at most one generated graph owner is supported")
	}
	if len(owners) == 1 {
		owner = owners[0]
	}
	if len(h.item.Subtitles) == 0 {
		stem := strings.TrimSuffix(h.path, filepath.Ext(h.path))
		if err := os.WriteFile(stem+".en.srt", []byte("1\n00:00:01,000 --> 00:00:02,000\nBound English\n\n2\n00:00:07,000 --> 00:00:08,000\nLater English\n"), 0600); err != nil {
			t.Fatal(err)
		}
		(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
		item, err := h.f.app.library.GetItem(h.f.ctx, owner.userID, h.item.ID)
		if err != nil || len(item.Subtitles) != 1 {
			t.Fatal("the producer identity fixture did not index its authorized subtitle")
		}
		h.item = item
	}
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{}, owner.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	var negotiation struct {
		PlayID string `json:"PlaySessionId"`
	}
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlayID == "" {
		t.Fatal("the producer identity fixture did not prepare playback")
	}
	query := url.Values{"api_key": {owner.headers.Get("X-Emby-Token")}, "PlaySessionId": {negotiation.PlayID},
		"MediaSourceId": {media.SourceID(h.item.ID)}, "DeviceId": {owner.deviceID},
		"SegmentContainer": {"mp4"}, "VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "SegmentLength": {"3"},
		"AllowVideoStreamCopy": {"false"}, "AllowAudioStreamCopy": {"false"},
		"SubtitleStreamIndex": {strconv.Itoa(h.item.Subtitles[0].Index)}, "ManifestSubtitles": {"vtt"}}
	master := h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	tracks := hlsSubtitleRenditionURLs(t, master.body)
	if len(variants) != 1 || len(tracks) != 1 {
		t.Fatal("the producer identity fixture did not advertise one media and subtitle rendition")
	}
	graph := hlsGeneratedIdentityHTTPGraph{playID: negotiation.PlayID, mainURL: variants[0], subtitleURL: tracks[0], owner: owner}
	hlsGeneratedIdentityHTTPMedia(t, h, &graph)
	h.f.app.hls.mu.Lock()
	var sessions []*hlsSession
	for _, session := range h.f.app.hls.sessions {
		if session.key.scope.AuthSessionID == owner.id && session.key.scope.PlaySessionID == graph.playID {
			sessions = append(sessions, session)
		}
	}
	h.f.app.hls.mu.Unlock()
	if len(sessions) != 1 {
		t.Fatal("a generated graph allocated more than one immutable HLS registration")
	}
	graph.session = sessions[0]
	graph.session.mu.Lock()
	producers := append([]hlsProducer(nil), graph.session.producers...)
	graph.session.mu.Unlock()
	if len(producers) != 1 || producers[0].id != graph.producerID || producers[0].first != -1 {
		t.Fatal("the published graph did not bind its attached full-source producer")
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, graph.producerID)
		if err != nil {
			t.Fatalf("the published generated producer became unavailable: %v", err)
		}
		if record.State == "completed" {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("the generated producer did not finish its finite real media input")
		case <-h.f.ctx.Done():
			t.Fatal("the producer identity fixture exceeded its owned deadline")
		}
	}
	playlist := h.request(t, http.MethodGet, graph.subtitleURL, nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	graph.subtitleURLs = hlsHTTPManifestChildren(playlist.body)
	if len(graph.subtitleURLs) != len(graph.mediaURLs) || !bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("subtitle artifacts did not retain the complete measured media window")
	}
	for _, child := range graph.subtitleURLs {
		hlsGeneratedIdentityHTTPBoundID(t, child, graph.producerID, owner.headers.Get("X-Emby-Token"))
	}
	return graph
}

func hlsGeneratedIdentityHTTPMedia(t *testing.T, h *hlsHTTPFixture, graph *hlsGeneratedIdentityHTTPGraph) {
	t.Helper()
	var playlist hlsHTTPResponse
	for attempt := 0; attempt < 200; attempt++ {
		playlist = h.request(t, http.MethodGet, graph.mainURL, nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		if bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) {
			break
		}
		select {
		case <-h.f.ctx.Done():
			t.Fatal("the generated media identity fixture exceeded its owned deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	graph.mediaURLs = hlsHTTPManifestChildren(playlist.body)
	graph.mapURL = ""
	for _, line := range strings.Split(string(playlist.body), "\n") {
		if strings.HasPrefix(line, "#EXT-X-MAP:URI=\"") {
			graph.mapURL = strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
		}
	}
	if !bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) || graph.mapURL == "" || len(graph.mediaURLs) != 4 {
		t.Fatal("the generated identity fixture did not publish its real map and four finite fragments")
	}
	parsed := hlsHTTPURL(t, graph.mapURL, graph.owner.headers.Get("X-Emby-Token"))
	graph.producerID = parsed.Query().Get(hlsProducerQuery)
	if graph.producerID == "" {
		t.Fatal("the published initialization map omitted its exact producer identity")
	}
	for _, child := range graph.mediaURLs {
		hlsGeneratedIdentityHTTPBoundID(t, child, graph.producerID, graph.owner.headers.Get("X-Emby-Token"))
	}
}

func hlsGeneratedIdentityHTTPBoundID(t *testing.T, raw, producerID, token string) {
	t.Helper()
	parsed := hlsHTTPURL(t, raw, token)
	if values := parsed.Query()[hlsProducerQuery]; len(values) != 1 || values[0] != producerID {
		t.Fatal("a published generated artifact does not retain exactly one shared producer identity")
	}
}

func hlsGeneratedIdentityHTTPJobCount(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedIdentityHTTPGraph) int {
	t.Helper()
	var count int
	if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*) FROM encoding_jobs
		WHERE auth_session_id = $1 AND play_session_id = $2`, graph.owner.id, graph.playID).Scan(&count); err != nil {
		t.Fatal("read the generated graph owner's encoding count")
	}
	return count
}

func hlsGeneratedIdentityHTTPSelector(t *testing.T, raw, producerID string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if producerID == "" {
		query.Del(hlsProducerQuery)
	} else {
		query.Set(hlsProducerQuery, producerID)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func hlsGeneratedIdentityHTTPRootMain(t *testing.T, graph hlsGeneratedIdentityHTTPGraph) string {
	t.Helper()
	parsed, err := url.Parse(graph.mainURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = parsed.Path[:strings.Index(parsed.Path, "/hls2/")] + "/main.m3u8"
	return hlsGeneratedIdentityHTTPSelector(t, parsed.String(), graph.producerID)
}

func hlsGeneratedIdentityHTTPArtifacts(t *testing.T, graph hlsGeneratedIdentityHTTPGraph) []string {
	t.Helper()
	return []string{hlsGeneratedIdentityHTTPSelector(t, graph.mainURL, graph.producerID),
		hlsGeneratedIdentityHTTPRootMain(t, graph), graph.mapURL, graph.mediaURLs[0], graph.subtitleURLs[0]}
}

func TestHTTPGeneratedHLSProducerURLsKeepExactArtifactAndSubtitleOwnership(t *testing.T) {
	h := newHLSHTTPFixture(t)
	graph := hlsGeneratedIdentityHTTPPrepare(t, h)
	// Preparing the same source again under one credential reuses its active
	// play session. A separate authenticated device establishes another scope.
	other := hlsGeneratedIdentityHTTPPrepare(t, h, h.accounts.second)
	if graph.producerID == other.producerID || graph.playID == other.playID {
		t.Fatal("independently owned playback graphs shared their manager identity")
	}
	artifacts := hlsGeneratedIdentityHTTPArtifacts(t, graph)
	for _, artifact := range artifacts {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			response := h.request(t, method, artifact, nil, nil)
			expectHLSHTTPStatus(t, response, http.StatusOK)
			if method == http.MethodGet && len(response.body) == 0 || method == http.MethodHead && len(response.body) != 0 {
				t.Fatal("an attached generated artifact lost its GET or HEAD representation")
			}
			foreign := hlsGeneratedIdentityHTTPSelector(t, artifact, other.producerID)
			expectHLSHTTPStatus(t, h.request(t, method, foreign, nil, nil), http.StatusNotFound)
		}
	}
	missing := strings.Replace(graph.mediaURLs[0], "/segment-000000.m4s?", "/segment-000999.m4s?", 1)
	if missing == graph.mediaURLs[0] {
		t.Fatal("the generated identity fixture omitted its canonical first fragment")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, missing, nil, nil), http.StatusServiceUnavailable)
	expectHLSHTTPStatus(t, h.request(t, http.MethodHead, missing, nil, nil), http.StatusOK)
	graph.session.mu.Lock()
	for _, producer := range graph.session.producers {
		h.f.app.hls.releaseProducer(graph.session.key.scope, producer)
	}
	graph.session.mu.Unlock()
	for _, artifact := range artifacts {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			expectHLSHTTPStatus(t, h.request(t, method, artifact, nil, nil), http.StatusNotFound)
		}
	}
	if hlsGeneratedIdentityHTTPJobCount(t, h, graph) != 1 || hlsGeneratedIdentityHTTPJobCount(t, h, other) != 1 {
		t.Fatal("foreign or released producer requests created replacement jobs")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, other.mediaURLs[0], nil, nil), http.StatusOK)
}

func TestHTTPGeneratedHLSOldProducerCannotBootstrapOrBorrowReplacement(t *testing.T) {
	h := newHLSHTTPFixture(t)
	old := hlsGeneratedIdentityHTTPPrepare(t, h)
	if err := h.f.app.hls.manager.CancelJob(old.producerID, old.session.key.scope); err != nil {
		t.Fatal(err)
	}
	replacement := old
	hlsGeneratedIdentityHTTPMedia(t, h, &replacement)
	if replacement.producerID == old.producerID || hlsGeneratedIdentityHTTPJobCount(t, h, old) != 2 {
		t.Fatal("an unbound media request did not create exactly one distinct replacement")
	}
	for _, artifact := range hlsGeneratedIdentityHTTPArtifacts(t, old) {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			expectHLSHTTPStatus(t, h.request(t, method, artifact, nil, nil), http.StatusNotFound)
		}
	}
	for _, artifact := range []string{replacement.mapURL, replacement.mediaURLs[0]} {
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, artifact, nil, nil), http.StatusOK)
	}
	if hlsGeneratedIdentityHTTPJobCount(t, h, old) != 2 {
		t.Fatal("obsolete bound media, initialization or subtitle URLs admitted another producer")
	}
}

func TestHTTPGeneratedHLSPauseCacheMissBlocksBootstrapUntilCommittedUnpause(t *testing.T) {
	h := newHLSHTTPFixture(t)
	old := hlsGeneratedIdentityHTTPPrepare(t, h)
	started := map[string]any{"PlaySessionId": old.playID, "ItemId": h.item.ID, "PositionTicks": 0}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing", started, h.accounts.viewer.headers), http.StatusNoContent)
	paused := map[string]any{"PlaySessionId": old.playID, "ItemId": h.item.ID,
		"PositionTicks": media.TicksPerSecond, "EventName": "Pause", "IsPaused": false}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", paused, h.accounts.viewer.headers), http.StatusNoContent)
	// Attached complete bytes remain readable. This stage fences new generated
	// admission without applying the separate legacy production-seal contract.
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, old.mediaURLs[0], nil, nil), http.StatusOK)
	if err := h.f.app.hls.manager.CancelJob(old.producerID, old.session.key.scope); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{old.mainURL, hlsGeneratedIdentityHTTPSelector(t, old.mapURL, "")} {
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, raw, nil, nil), http.StatusServiceUnavailable)
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodHead, old.mainURL, nil, nil), http.StatusOK)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, old.mediaURLs[0], nil, nil), http.StatusNotFound)
	if hlsGeneratedIdentityHTTPJobCount(t, h, old) != 1 {
		t.Fatal("paused cache misses restarted full-source generation")
	}
	unpaused := map[string]any{"PlaySessionId": old.playID, "ItemId": h.item.ID,
		"PositionTicks": media.TicksPerSecond, "EventName": "Unpause", "IsPaused": true}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", unpaused, h.accounts.viewer.headers), http.StatusNoContent)
	replacement := old
	hlsGeneratedIdentityHTTPMedia(t, h, &replacement)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, replacement.mediaURLs[0], nil, nil), http.StatusOK)
	if replacement.producerID == old.producerID || hlsGeneratedIdentityHTTPJobCount(t, h, old) != 2 {
		t.Fatal("committed Unpause did not permit exactly one fresh generated producer")
	}
}

func TestHTTPGeneratedHLSSubtitleClockEvictionCannotRecreateOldState(t *testing.T) {
	h := newHLSHTTPFixture(t)
	graph := hlsGeneratedIdentityHTTPPrepare(t, h)
	input, err := os.Open(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx, cancel := context.WithTimeout(h.f.ctx, 5*time.Second)
	defer cancel()
	observed := hlsAdmissionObserve(ctx)
	pending := make(chan struct{})
	var pendingOnce sync.Once
	releasePending := func() { pendingOnce.Do(func() { close(pending) }) }
	state := &hlsProducerSubtitleClock{pending: pending}
	graph.session.mu.Lock()
	graph.session.subtitleProducerClocks[graph.producerID] = state
	graph.session.mu.Unlock()
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := h.f.app.hls.producerSubtitleClock(observed, graph.session, input, graph.producerID)
		result <- err
	}()
	defer func() {
		cancel()
		releasePending()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("the subtitle clock waiter did not drain during failure cleanup")
		}
	}()
	// The producer has completed, so the first playlist Open cannot wait for
	// future bytes. The observed Done call therefore enters the clock's pending
	// select after its ReadHandle was acquired and its metadata was checked.
	hlsAdmissionJoined(t, observed)
	cachePath := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, graph.producerID)
	graph.session.mu.Lock()
	if graph.session.subtitleProducerClocks[graph.producerID] != state {
		graph.session.mu.Unlock()
		t.Fatal("the pending clock waiter replaced its established metadata")
	}
	for _, producer := range graph.session.producers {
		h.f.app.hls.releaseProducer(graph.session.key.scope, producer)
	}
	graph.session.producers = nil
	delete(graph.session.subtitleProducerClocks, graph.producerID)
	graph.session.mu.Unlock()
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatal("retained clock reader did not pin its cancelled producer's cache")
	}
	releasePending()
	select {
	case err := <-result:
		if !errors.Is(err, transcode.ErrJobNotFound) {
			t.Fatalf("an evicted subtitle clock waiter recreated or borrowed producer state: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the evicted subtitle clock waiter did not release its playlist reader")
	}
	graph.session.mu.Lock()
	_, resurrected := graph.session.subtitleProducerClocks[graph.producerID]
	clockCount := len(graph.session.subtitleProducerClocks)
	graph.session.mu.Unlock()
	if resurrected || clockCount != 0 {
		t.Fatal("subtitle metadata retained an evicted producer after its waiter resumed")
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		_, err := os.Stat(cachePath)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("the completed clock waiter leaked a cache reader reservation")
		case <-h.f.ctx.Done():
			t.Fatal("the subtitle eviction fixture exceeded its owned deadline")
		}
	}
	if hlsGeneratedIdentityHTTPJobCount(t, h, graph) != 1 {
		t.Fatal("a subtitle clock eviction created a replacement media producer")
	}
}
