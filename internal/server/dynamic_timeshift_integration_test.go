//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/timeshift"
)

type dynamicTimeshiftHTTPFixture struct {
	h                *hlsHTTPFixture
	upstream         *httptest.Server
	firstEOF         chan struct{}
	endFirst         sync.Once
	mediaRequests    atomic.Int32
	badAuthorization atomic.Bool
	subtitleRequests [8]atomic.Int32
	clock            atomic.Int64
}

type dynamicHTTPPresentation struct {
	playID, liveID, presentationID, masterURL, mainURL, windowURL string
	source                                                        map[string]any
}

type dynamicHTTPWindow struct {
	PresentationID                                              string
	EarliestTicks, LiveEdgeTicks, LiveStartTicks, BufferedBytes int64
	CanSeek, IsEnded, IsStalled                                 bool
	LiveURL                                                     string
}

// The first response can be ended explicitly while later connections remain
// open. This tests real HTTP source ownership and generation changes without
// restarting the server, replacing FFmpeg, or fabricating published segments.
func newDynamicTimeshiftHTTPFixture(t *testing.T, withSubtitles bool, timeouts ...time.Duration) *dynamicTimeshiftHTTPFixture {
	t.Helper()
	h := newHLSHTTPFixture(t, timeouts...)
	fixture := &dynamicTimeshiftHTTPFixture{h: h, firstEOF: make(chan struct{})}
	fixture.clock.Store(time.Now().UnixNano())
	path := filepath.Join(t.TempDir(), "source.ts")
	// Keep enough continuous media for twelve seconds of closed slices while
	// EOF is withheld. A twelve-second input leaves its final slice open and
	// cannot establish the three-target-duration advertised live window.
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=24",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=24",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "24", "-muxrate", "600000", "-f", "mpegts", path)
	probe := hlsHTTPMediaCommand(t, h.ffprobe, "-v", "error", "-show_frames", "-show_format",
		"-show_entries", "frame=media_type,best_effort_timestamp_time,nb_samples:format=duration", "-of", "json", path)
	var clock struct {
		Frames []struct {
			Type    string "json:\"media_type\""
			PTS     string "json:\"best_effort_timestamp_time\""
			Samples int    "json:\"nb_samples\""
		}
		Format struct{ Duration string }
	}
	if err := json.Unmarshal(probe, &clock); err != nil {
		t.Fatal("cannot establish the continuous dynamic source clock", err)
	}
	duration, err := strconv.ParseFloat(clock.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || math.Abs(duration-24) > .06 {
		t.Fatal("dynamic source does not contain its complete 24-second input")
	}
	videoFrames, audioFrames, priorSamples := 0, 0, 0
	var firstVideo, priorAudio float64
	for _, frame := range clock.Frames {
		pts, err := strconv.ParseFloat(frame.PTS, 64)
		if err != nil || math.IsNaN(pts) || math.IsInf(pts, 0) {
			t.Fatal("dynamic source frame lacks its actual presentation timestamp")
		}
		switch frame.Type {
		case "video":
			if videoFrames == 0 {
				firstVideo = pts
			}
			if math.Abs(pts-firstVideo-float64(videoFrames)/24) > .000025 {
				t.Fatal("dynamic video source has a timestamp reset or cadence gap")
			}
			videoFrames++
		case "audio":
			if frame.Samples <= 0 || audioFrames > 0 && math.Abs(pts-priorAudio-float64(priorSamples)/48000) > .000025 {
				t.Fatal("dynamic audio source has a timestamp reset or sample gap")
			}
			priorAudio, priorSamples = pts, frame.Samples
			audioFrames++
		}
	}
	if videoFrames != 576 || audioFrames == 0 {
		t.Fatal("dynamic source lost complete continuous audio/video")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) <= 128*1024 || len(data) > 4<<20 {
		t.Fatal("dynamic source must have a complete bounded probe prefix", err)
	}
	fixture.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-dynamic-fixture" {
			fixture.badAuthorization.Store(true)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/media" {
			number := fixture.mediaRequests.Add(1)
			w.Header().Set("Content-Type", "video/mp2t")
			if _, err := w.Write(data); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			if number == 1 {
				select {
				case <-fixture.firstEOF:
				case <-r.Context().Done():
				}
			} else {
				<-r.Context().Done()
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/subtitle/") {
			index, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/subtitle/"))
			if err == nil && index >= 0 && index < len(fixture.subtitleRequests) && withSubtitles {
				fixture.subtitleRequests[index].Add(1)
				w.Header().Set("Content-Type", "text/vtt")
				_, _ = fmt.Fprintf(w, "WEBVTT\n\n00:00:02.000 --> 00:00:30.000\ncaption-track-%d\n\n", index)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(fixture.upstream.Close)
	closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := h.f.app.closeDynamicSources(closeCtx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	definition := config.DynamicSourceDefinition{ItemID: h.item.ID, Name: "Authorized dynamic fixture", URL: fixture.upstream.URL + "/media",
		Headers: map[string]string{"Authorization": "Bearer private-dynamic-fixture"}, Infinite: true, MaxReconnects: 2}
	if withSubtitles {
		for index := 0; index < 8; index++ {
			definition.Subtitles = append(definition.Subtitles, dynamicsource.SubtitleDefinition{ID: fmt.Sprintf("track-%d", index), Name: fmt.Sprintf("Caption %d", index), Language: "eng",
				Format: "webvtt", Mode: "document", Clock: "media", URL: fixture.upstream.URL + "/subtitle/" + strconv.Itoa(index),
				Headers: map[string]string{"Authorization": "Bearer private-dynamic-fixture"}, Default: index == 0})
		}
	}
	h.f.app.cfg.DynamicSources = []config.DynamicSourceDefinition{definition}
	h.f.app.cfg.Timeshift = config.DefaultTimeshiftConfig()
	h.f.app.cfg.Timeshift.CacheDirectory = filepath.Join(t.TempDir(), "timeshift")
	h.f.app.cfg.Timeshift.WindowSeconds = 12
	h.f.app.cfg.Timeshift.MaxWindowBytes, h.f.app.cfg.Timeshift.MaxCacheBytes = 8<<20, 16<<20
	if err := h.f.app.initializeDynamicSources(h.f.ctx); err != nil {
		t.Fatal(err)
	}
	// Inject only the retention clock. Playback credentials and source leases
	// keep their real clocks, so TTL checks cannot pass through expired auth.
	if err := h.f.app.dynamicStreams.store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	options := h.f.app.cfg.Timeshift.Options()
	options.Now = func() time.Time { return time.Unix(0, fixture.clock.Load()) }
	options.SweepInterval = 20 * time.Millisecond
	h.f.app.dynamicStreams.store, err = timeshift.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.f.app.closeDynamicSources(ctx); err != nil {
			t.Error(err)
		}
	})
	h.f.cfg = h.f.app.cfg
	return fixture
}

func (d *dynamicTimeshiftHTTPFixture) open(t *testing.T) dynamicHTTPPresentation {
	t.Helper()
	return d.openWithMode(t, true)
}

func (d *dynamicTimeshiftHTTPFixture) openWithMode(t *testing.T, autoOpen bool) dynamicHTTPPresentation {
	t.Helper()
	h := d.h
	body := map[string]any{"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "AutoOpenLiveStream": autoOpen,
		"SubtitleStreamIndex": -1, "DeviceProfile": map[string]any{
			"TranscodingProfiles": []map[string]any{{"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "MaxWidth": 96, "MaxHeight": 54, "SegmentLength": 3}},
			"SubtitleProfiles":    []map[string]any{{"Format": "vtt", "Method": "Hls", "Container": "ts", "Protocol": "hls"}},
		}}
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var info struct {
		PlaySessionID string
		MediaSources  []map[string]any
	}
	if err := json.Unmarshal(response.body, &info); err != nil || info.PlaySessionID == "" || len(info.MediaSources) != 1 {
		t.Fatal("dynamic PlaybackInfo lacks its prepared playback identity")
	}
	source := info.MediaSources[0]
	if !autoOpen {
		openToken, _ := source["OpenToken"].(string)
		if source["RequiresOpening"] != true || openToken == "" || d.mediaRequests.Load() != 0 {
			t.Fatal("opening hint contacted the upstream or omitted its explicit lease contract")
		}
		delete(body, "AutoOpenLiveStream")
		body["OpenToken"], body["ItemId"], body["PlaySessionId"] = openToken, h.item.ID, info.PlaySessionID
		response = h.request(t, http.MethodPost, "/emby/LiveStreams/Open", body, h.accounts.viewer.headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		var opened struct{ MediaSource map[string]any }
		if err := json.Unmarshal(response.body, &opened); err != nil || opened.MediaSource == nil {
			t.Fatal("explicit LiveStreams/Open did not return its media source")
		}
		source = opened.MediaSource
	}
	get := func(key string) string {
		value, _ := source[key].(string)
		if value == "" {
			t.Fatalf("dynamic source lacks %s", key)
		}
		return value
	}
	result := dynamicHTTPPresentation{playID: info.PlaySessionID, liveID: get("LiveStreamId"), masterURL: get("TranscodingUrl"), windowURL: get("GobyWindowUrl"), source: source}
	parsed := hlsHTTPURL(t, result.masterURL, h.accounts.viewer.headers.Get("X-Emby-Token"))
	result.presentationID = parsed.Query().Get("GobyLiveId")
	if result.presentationID == "" || source["SupportsSeeking"] != true || source["SupportsPause"] != true {
		t.Fatal("dynamic source did not advertise bounded seek/pause")
	}
	for _, secret := range []string{d.upstream.URL, "private-dynamic-fixture", "Authorization"} {
		if bytes.Contains(response.body, []byte(secret)) {
			t.Fatal("PlaybackInfo exposed a private source request")
		}
	}
	master := h.request(t, http.MethodGet, result.masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	children := hlsHTTPManifestChildren(master.body)
	if len(children) != 1 {
		t.Fatal("dynamic master did not advertise one media rendition")
	}
	result.mainURL = children[0]
	return result
}

func (d *dynamicTimeshiftHTTPFixture) window(t *testing.T, presentation dynamicHTTPPresentation) dynamicHTTPWindow {
	t.Helper()
	response := d.h.request(t, http.MethodGet, presentation.windowURL, nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var window dynamicHTTPWindow
	if err := json.Unmarshal(response.body, &window); err != nil || window.PresentationID != presentation.presentationID || window.EarliestTicks < 0 || window.LiveEdgeTicks < window.EarliestTicks || window.CanSeek != (window.LiveEdgeTicks > window.EarliestTicks) {
		t.Fatal("dynamic window has inconsistent measured retained bounds")
	}
	return window
}

func (d *dynamicTimeshiftHTTPFixture) waitWindow(t *testing.T, presentation dynamicHTTPPresentation, ready func(dynamicHTTPWindow) bool) dynamicHTTPWindow {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		window := d.window(t, presentation)
		if ready(window) {
			return window
		}
		if time.Now().After(deadline) {
			d.logWindowTimeout(t, presentation, window)
			t.Fatal("dynamic window did not advance within its bounded fixture deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Emit only non-secret status scalars. In particular, never format the public
// window, source definition, principal, plan, request, or manager record: those
// larger objects may carry token-bearing URLs or private source information.
func (d *dynamicTimeshiftHTTPFixture) logWindowTimeout(t *testing.T, presentation dynamicHTTPPresentation, window dynamicHTTPWindow) {
	t.Helper()
	diagnostic := struct {
		EarliestTicks, LiveEdgeTicks, LiveStartTicks, BufferedBytes                                int64
		CanSeek, WindowEnded, WindowStalled                                                        bool
		Generation                                                                                 uint64
		JobID, RecordState, RecordErrorCode                                                        string
		ProducerStarted, ProducerEnded, SessionClosed, SessionMissing, SessionBusy, SnapshotFailed bool
		ManagerAvailable                                                                           bool
		ManagerCode                                                                                string
		SourceRequests                                                                             int32
	}{
		EarliestTicks: window.EarliestTicks, LiveEdgeTicks: window.LiveEdgeTicks, LiveStartTicks: window.LiveStartTicks, BufferedBytes: window.BufferedBytes,
		CanSeek: window.CanSeek, WindowEnded: window.IsEnded, WindowStalled: window.IsStalled, SourceRequests: d.mediaRequests.Load(),
	}
	server := d.h.f.app
	health := server.hls.health()
	diagnostic.ManagerAvailable, diagnostic.ManagerCode = health.Available, health.Code
	server.dynamicStreams.mu.Lock()
	session := server.dynamicStreams.sessions[presentation.presentationID]
	server.dynamicStreams.mu.Unlock()
	if session == nil {
		diagnostic.SessionMissing = true
	} else if !session.mu.TryLock() {
		// A reconnect holds this lock while connecting. Diagnostics must not
		// extend the existing fifteen-second fixture deadline by waiting for it.
		diagnostic.SessionBusy = true
	} else {
		diagnostic.Generation, diagnostic.JobID = session.generation, session.jobID
		diagnostic.ProducerStarted, diagnostic.ProducerEnded, diagnostic.SessionClosed = session.producerStarted, session.producerEnded, session.closed
		scope, jobID := session.scope, session.jobID
		session.mu.Unlock()
		if jobID != "" {
			record, err := server.hls.manager.Snapshot(scope, jobID)
			diagnostic.RecordState, diagnostic.RecordErrorCode, diagnostic.SnapshotFailed = record.State, record.ErrorCode, err != nil
		}
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Log("dynamic_window_timeout_diagnostic_unavailable=true")
		return
	}
	t.Logf("dynamic_window_timeout=%s", encoded)
}

func dynamicHTTPQuery(t *testing.T, raw string, updates map[string]string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid fixture URL")
	}
	query := parsed.Query()
	for key, value := range updates {
		if value == "" {
			query.Del(key)
		} else {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestDynamicTimeshiftHTTPPauseReconnectWindowSeekLiveAndOwnership(t *testing.T) {
	d := newDynamicTimeshiftHTTPFixture(t, false)
	p := d.openWithMode(t, false)
	h := d.h
	expectHLSHTTPStatus(t, h.request(t, http.MethodHead, p.mainURL, nil, nil), http.StatusNotFound)
	first := d.waitWindow(t, p, func(w dynamicHTTPWindow) bool { return w.LiveEdgeTicks >= 9*media.TicksPerSecond })
	report := map[string]any{"PlaySessionId": p.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": first.EarliestTicks}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing", report, h.accounts.viewer.headers), http.StatusNoContent)
	report["IsPaused"] = true
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", report, h.accounts.viewer.headers), http.StatusNoContent)
	d.endFirst.Do(func() { close(d.firstEOF) })
	advanced := d.waitWindow(t, p, func(w dynamicHTTPWindow) bool {
		return w.LiveEdgeTicks >= 20*media.TicksPerSecond && d.mediaRequests.Load() >= 2
	})
	if advanced.PresentationID != first.PresentationID || advanced.EarliestTicks <= 0 || advanced.BufferedBytes > h.f.app.cfg.Timeshift.MaxWindowBytes {
		t.Fatal("paused reconnect replaced its presentation or exceeded retention bounds")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+p.playID, nil, h.accounts.viewer.headers), http.StatusNoContent)
	info := h.request(t, http.MethodPost, "/emby/LiveStreams/MediaInfo?LiveStreamId="+p.liveID, nil, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, info, http.StatusOK)
	var source map[string]any
	if err := json.Unmarshal(info.body, &source); err != nil || source["TranscodingUrl"] != p.masterURL {
		t.Fatal("MediaInfo changed presentation after source reconnection")
	}
	seek := dynamicHTTPQuery(t, p.mainURL, map[string]string{"StartTimeTicks": strconv.FormatInt(advanced.EarliestTicks, 10)})
	mediaPlaylist := h.request(t, http.MethodGet, seek, nil, nil)
	expectHLSHTTPStatus(t, mediaPlaylist, http.StatusOK)
	if !bytes.Contains(mediaPlaylist.body, []byte("#EXT-X-START:TIME-OFFSET=")) || bytes.Contains(mediaPlaylist.body, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("retained seek was not a live playlist view")
	}
	children := hlsHTTPManifestChildren(mediaPlaylist.body)
	if len(children) == 0 {
		t.Fatal("retained window has no media artifacts")
	}
	segment := h.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, segment, http.StatusOK)
	artifact := filepath.Join(t.TempDir(), "retained.ts")
	if err := os.WriteFile(artifact, segment.body, 0600); err != nil {
		t.Fatal(err)
	}
	hlsHTTPMediaCommand(t, h.ffmpeg, "-v", "error", "-xerror", "-threads", "1", "-i", artifact, "-map", "0:v:0", "-map", "0:a:0", "-f", "null", "-")
	for _, target := range []string{p.masterURL, p.windowURL, children[0]} {
		for _, foreign := range []http.Header{h.accounts.second.headers, h.accounts.other.headers} {
			expectHLSHTTPStatus(t, h.request(t, http.MethodGet, hlsHTTPWithoutToken(t, target), nil, foreign), http.StatusNotFound)
		}
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, dynamicHTTPQuery(t, p.mainURL, map[string]string{"StartTimeTicks": "0"}), nil, nil), http.StatusGone)
	live := h.request(t, http.MethodGet, advanced.LiveURL, nil, nil)
	expectHLSHTTPStatus(t, live, http.StatusOK)
	liveChildren := hlsHTTPManifestChildren(live.body)
	if len(liveChildren) != 1 || !strings.Contains(liveChildren[0], "Live=true") {
		t.Fatal("return-to-live lost its safe live-edge view")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, liveChildren[0], nil, nil), http.StatusOK)
	report["IsPaused"] = false
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", report, h.accounts.viewer.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, p.windowURL, nil, nil), http.StatusNotFound)
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/LiveStreams/Close?LiveStreamId="+p.liveID, nil, h.accounts.viewer.headers), http.StatusOK)
	if d.badAuthorization.Load() {
		t.Fatal("an upstream request lost its configured authorization")
	}
}

func TestDynamicTimeshiftHTTPExpiredRetentionCannotBeRevivedByRequests(t *testing.T) {
	d := newDynamicTimeshiftHTTPFixture(t, false)
	p := d.open(t)
	_ = d.window(t, p)
	d.clock.Add(int64(6 * time.Minute))
	for range 2 {
		expectHLSHTTPStatus(t, d.h.request(t, http.MethodGet, p.windowURL, nil, nil), http.StatusNotFound)
	}
}
