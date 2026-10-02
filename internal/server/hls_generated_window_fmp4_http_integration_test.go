//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedWindowFMP4HTTPGraph struct {
	hlsGeneratedWindowHTTPGraph
	initializationURLs []string
}

// The real catalog and PlaybackInfo registration remain untouched. This
// profile deliberately negotiates one output, rather than a one-entry ladder.
func hlsGeneratedWindowFMP4HTTPPrepare(t *testing.T, h *hlsHTTPFixture, start int64) hlsGeneratedWindowFMP4HTTPGraph {
	t.Helper()
	owner := h.accounts.viewer
	catalogDuration, sourceOrigin := h.item.Media.DurationTicks, h.item.Media.FormatStartTicks
	body := map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "AllowVideoStreamCopy": false,
		"StartTimeTicks": start, "MaxStreamingBitrate": 2_000_000,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": "h264",
			"EnableAdaptiveBitrate": false, "MaxWidth": 160, "MaxHeight": 96, "SegmentLength": 6,
		}}},
	}
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, owner.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var negotiation struct {
		PlayID       string           `json:"PlaySessionId"`
		MediaSources []map[string]any `json:"MediaSources"`
		ErrorCode    string           `json:"ErrorCode"`
	}
	if json.Unmarshal(response.body, &negotiation) != nil || negotiation.PlayID == "" || len(negotiation.MediaSources) != 1 || negotiation.ErrorCode != "" {
		t.Fatal("real PlaybackInfo did not negotiate one permitted silent AVC source")
	}
	masterURL, ok := negotiation.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || negotiation.MediaSources[0]["TranscodingContainer"] != "mp4" || negotiation.MediaSources[0]["TranscodingSubProtocol"] != "hls" {
		t.Fatal("the actual negotiated profile did not retain fragmented MP4 HLS")
	}
	parsed := hlsHTTPURL(t, masterURL, owner.headers.Get("X-Emby-Token"))
	h.f.app.hls.mu.Lock()
	session := h.f.app.hls.sessions[parsed.Query().Get("GobyHlsId")]
	h.f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("PlaybackInfo did not establish its immutable HLS registration")
	}
	plan := session.key.plan
	if !transcode.GeneratedHLS(plan) || plan.Container != "mp4" || plan.HLS.SegmentType != "fmp4" || plan.HLS.RenditionCount != 0 ||
		plan.VideoCodec != "h264" || plan.AudioStreamIndex != -1 || plan.AudioCodec != "" || plan.FrameRate != 24 ||
		plan.HLS.Window != (transcode.HLSWindow{}) || plan.StartTicks != 0 || plan.Width != 160 || plan.Height != 96 ||
		plan.DurationTicks != catalogDuration || h.item.Media.DurationTicks != catalogDuration || h.item.Media.FormatStartTicks != sourceOrigin {
		t.Fatalf("real negotiated single fMP4 output is outside the native contract: %+v", plan)
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != 1 {
		t.Fatal("single-output fMP4 negotiation acquired an adaptive ladder")
	}
	child := hlsHTTPURL(t, variants[0], owner.headers.Get("X-Emby-Token"))
	if !strings.HasSuffix(child.Path, "/"+transcode.HLSPlaylistName(0, 0)) || child.Query().Get("GobyHlsId") != session.id {
		t.Fatal("single-output playlist lost its actual negotiated registration")
	}
	return hlsGeneratedWindowFMP4HTTPGraph{hlsGeneratedWindowHTTPGraph: hlsGeneratedWindowHTTPGraph{
		playID: negotiation.PlayID, masterURL: masterURL, mainURLs: variants, mediaURLs: make([][]string, 1),
		manifests: make([][]byte, 1), session: session, owner: owner}}
}

func hlsGeneratedWindowFMP4HTTPLoad(t *testing.T, h *hlsHTTPFixture, graph *hlsGeneratedWindowFMP4HTTPGraph) {
	t.Helper()
	registeredPlan := graph.session.key.plan
	response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	matches := generatedWindowFMP4ManifestSlots.FindAllStringSubmatch(string(response.body), -1)
	if len(matches) != 17 || !bytes.HasPrefix(response.body, []byte("#EXTM3U\n#EXT-X-VERSION:7\n")) ||
		!bytes.Contains(response.body, []byte("#EXT-X-PLAYLIST-TYPE:VOD\n")) || !bytes.Contains(response.body, []byte("#EXT-X-MEDIA-SEQUENCE:0\n")) ||
		!bytes.Contains(response.body, []byte("#EXT-X-TARGETDURATION:6\n")) ||
		!bytes.Contains(response.body, []byte("#EXT-X-START:TIME-OFFSET=90.0000000,PRECISE=YES\n")) ||
		!bytes.HasSuffix(response.body, []byte("#EXT-X-ENDLIST\n")) || bytes.Count(response.body, []byte("#EXT-X-MAP:URI=")) != 17 ||
		bytes.Count(response.body, []byte("#EXT-X-DISCONTINUITY\n")) != 17 || bytes.Contains(response.body, []byte("#EXT-X-GAP")) {
		t.Fatal("real single fMP4 output did not advertise the unchanged complete native VOD timeline")
	}
	graph.initializationURLs = make([]string, len(matches))
	graph.mediaURLs[0] = make([]string, len(matches))
	for number, match := range matches {
		duration := "6.0000000"
		if number == 16 {
			duration = "4.0000000"
		}
		if match[2] != duration {
			t.Fatalf("slot %d replaced its exact source tail with metadata or a nominal segment", number)
		}
		for index, raw := range []string{match[1], match[3]} {
			parsed := hlsHTTPURL(t, raw, graph.owner.headers.Get("X-Emby-Token"))
			expected := fmt.Sprintf("window-segment-%06d.m4s", number)
			if index == 0 {
				expected = fmt.Sprintf("window-init-%06d.mp4", number)
			}
			if filepath.Base(parsed.Path) != expected || parsed.Query().Get(hlsGeneratedWindowGraphQuery) != graph.session.id ||
				parsed.Query().Get(hlsProducerQuery) != "" {
				t.Fatalf("slot %d lost its distinct logical map/media admission role", number)
			}
		}
		graph.initializationURLs[number], graph.mediaURLs[0][number] = match[1], match[3]
	}
	graph.manifests[0] = bytes.Clone(response.body)
	ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph)
	if len(ids) != 1 {
		t.Fatalf("initial high seek produced %d jobs instead of one closed single-output window", len(ids))
	}
	record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, ids[0])
	if err != nil || record.State != "completed" || record.Spec.Plan.Container != "mp4" || record.Spec.Plan.HLS.SegmentType != "fmp4" ||
		record.Spec.Plan.StartTicks != 90*media.TicksPerSecond || record.Spec.Plan.DurationTicks != 100*media.TicksPerSecond ||
		record.Spec.Plan.HLS.Window.EndTicks != 96*media.TicksPerSecond || record.Spec.Plan.HLS.Window.StartNumber != 15 ||
		!record.Spec.Plan.HLS.Window.RequireInputEvidence || record.Spec.Plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		record.Spec.Plan.HLS.RenditionCount != 0 || graph.session.key.plan != registeredPlan {
		t.Fatalf("real high-seek producer changed the negotiated output or source span: %+v, %v", record, err)
	}
	graph.session.mu.Lock()
	windowGraph := graph.session.windowGraph
	proved := windowGraph != nil && !windowGraph.fallback && windowGraph.published && windowGraph.endpoint.DurationTicks == 100*media.TicksPerSecond
	if proved {
		binding := windowGraph.slots[15]
		proved = binding.producer.id == ids[0] && binding.closure.NativeClockVersion == transcode.GeneratedWindowNativeClockV2 &&
			binding.nativeEmission.StartTicks == 90*media.TicksPerSecond && binding.nativeEmission.EndTicks == 96*media.TicksPerSecond &&
			binding.nativeEmission.RenditionCount == 1 && binding.nativeEmission.InitializationSHA256[0] != ([32]byte{}) &&
			binding.nativeEmission.SegmentSHA256[0] != ([32]byte{})
	}
	graph.session.mu.Unlock()
	if !proved {
		t.Fatal("the published fMP4 graph lacks independent native initialization and packet closure")
	}
}

func hlsGeneratedWindowFMP4HTTPAdmission(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowFMP4HTTPGraph, number int, initialization bool) (string, string) {
	t.Helper()
	target := graph.mediaURLs[0][number]
	name := fmt.Sprintf("segment-%06d.m4s", number)
	if initialization {
		target, name = graph.initializationURLs[number], "init.mp4"
	}
	admission := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, target, nil, nil)
	expectHLSHTTPStatus(t, admission, http.StatusTemporaryRedirect)
	location := admission.header.Get("Location")
	parsed := hlsHTTPURL(t, location, graph.owner.headers.Get("X-Emby-Token"))
	id := parsed.Query().Get(hlsProducerQuery)
	if id == "" || parsed.Query().Get(hlsGeneratedWindowGraphQuery) != "" || filepath.Base(parsed.Path) != name {
		t.Fatalf("source slot %d did not redirect to its exact private map/media role", number)
	}
	return id, location
}

func hlsGeneratedWindowFMP4HTTPBytes(t *testing.T, h *hlsHTTPFixture, location string) []byte {
	t.Helper()
	response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, location, nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	if len(response.body) == 0 || !strings.HasPrefix(response.header.Get("Content-Type"), "video/mp4") {
		t.Fatal("bound private fMP4 role did not return complete bytes")
	}
	return response.body
}

func hlsGeneratedWindowFMP4HTTPHasNoPins(session *hlsSession) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.closed {
		return false
	}
	if session.windowGraph == nil {
		return true
	}
	for _, binding := range session.windowGraph.slots {
		for index := 0; index < transcode.MaxHLSRenditions; index++ {
			if binding.redirectPins[index] != nil || binding.initializationPins[index] != nil {
				return false
			}
		}
	}
	return true
}

func TestHTTPGeneratedWindowFMP4NegotiatesSingleOutputAndHEADDoesNotProduce(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
	for _, target := range []string{graph.masterURL, graph.mainURLs[0]} {
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodHead, target, nil, nil), http.StatusOK)
	}
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph)) != 0 {
		t.Fatal("negotiation, master GET or initial playlist HEAD started a media producer")
	}
	hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
	for _, number := range []int{0, 15, 16} {
		for _, target := range []string{graph.initializationURLs[number], graph.mediaURLs[0][number]} {
			response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodHead, target, nil, nil)
			expectHLSHTTPStatus(t, response, http.StatusOK)
			if len(response.body) != 0 || !strings.HasPrefix(response.header.Get("Content-Type"), "video/mp4") {
				t.Fatal("logical fMP4 HEAD served media bytes or another role's content type")
			}
		}
	}
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph)) != 1 {
		t.Fatal("logical map/media HEAD admitted a cold source slot")
	}
}

func TestHTTPGeneratedWindowFMP4RolePinsPauseResumeAndStop(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
	firstID, initLocation := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, true)
	graph.session.mu.Lock()
	mapBinding := graph.session.windowGraph.slots[15]
	mapOnly := mapBinding.initializationPins[0] != nil && mapBinding.redirectPins[0] == nil
	graph.session.mu.Unlock()
	if !mapOnly {
		t.Fatal("MAP admission borrowed or retained the media handoff pin")
	}
	mediaID, mediaLocation := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, false)
	graph.session.mu.Lock()
	paired := graph.session.windowGraph.slots[15]
	distinct := paired.initializationPins[0] != nil && paired.redirectPins[0] != nil && paired.initializationPins[0] != paired.redirectPins[0]
	graph.session.mu.Unlock()
	if mediaID != firstID || !distinct {
		t.Fatal("same-slot MAP and media did not retain independent pins from the same completed producer")
	}
	firstInit, firstMedia := hlsGeneratedWindowFMP4HTTPBytes(t, h, initLocation), hlsGeneratedWindowFMP4HTTPBytes(t, h, mediaLocation)
	started := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 90 * media.TicksPerSecond}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing", started, graph.owner.headers), http.StatusNoContent)
	hlsGeneratedWindowHTTPProgress(t, h, graph.hlsGeneratedWindowHTTPGraph, "Pause", false)
	for _, initialization := range []bool{true, false} {
		id, location := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, initialization)
		expectedLocation, expectedBytes := mediaLocation, firstMedia
		if initialization {
			expectedLocation, expectedBytes = initLocation, firstInit
		}
		if id != firstID || location != expectedLocation || !bytes.Equal(hlsGeneratedWindowFMP4HTTPBytes(t, h, location), expectedBytes) {
			t.Fatal("committed pause replaced a cached initialization or media role")
		}
	}
	for _, target := range []string{graph.initializationURLs[0], graph.mediaURLs[0][0]} {
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, target, nil, nil), http.StatusServiceUnavailable)
	}
	ping := "/emby/Sessions/Playing/Ping?" + url.Values{"PlaySessionId": {graph.playID}}.Encode()
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, ping, nil, graph.owner.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.initializationURLs[0], nil, nil), http.StatusServiceUnavailable)
	if ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph); len(ids) != 1 || ids[0] != firstID {
		t.Fatal("paused MAP/media cache misses or heartbeat resumed production")
	}
	hlsGeneratedWindowHTTPProgress(t, h, graph.hlsGeneratedWindowHTTPGraph, "Unpause", true)
	resumedID, resumedInit := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 0, true)
	resumedMediaID, resumedMedia := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 0, false)
	if resumedID == firstID || resumedMediaID != resumedID || len(hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph)) != 2 {
		t.Fatal("committed Unpause failed to admit one new complete same-slot MAP/media pair")
	}
	current := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, current, http.StatusOK)
	if !bytes.Equal(current.body, graph.manifests[0]) {
		t.Fatal("pause or cold-slot admission rewrote the immutable full VOD manifest")
	}
	initReader, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, resumedID, "init.mp4")
	if err != nil {
		t.Fatal(err)
	}
	mediaReader, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, resumedID, "segment-000000.m4s")
	if err != nil {
		_ = initReader.Close()
		t.Fatal(err)
	}
	var initOnce, mediaOnce sync.Once
	closeInit := func() { initOnce.Do(func() { _ = initReader.Close() }) }
	closeMedia := func() { mediaOnce.Do(func() { _ = mediaReader.Close() }) }
	t.Cleanup(closeInit)
	t.Cleanup(closeMedia)
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {graph.playID}, "DeviceId": {graph.owner.deviceID}}.Encode()
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodDelete, stop, nil, graph.owner.headers), http.StatusNoContent)
	for _, target := range []string{initLocation, mediaLocation, resumedInit, resumedMedia} {
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, target, nil, nil), http.StatusNotFound)
	}
	hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool { return hlsGeneratedWindowFMP4HTTPHasNoPins(graph.session) },
		"Stop retained a registration or a map/media redirect handoff pin")
	cachePath := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, resumedID)
	var first [16]byte
	if _, err := initReader.ReadAt(first[:], 0); err != nil || !bytes.Equal(first[4:8], []byte("ftyp")) {
		t.Fatal("Stop invalidated the independently owned initialization reader")
	}
	if _, err := mediaReader.ReadAt(first[:], 0); err != nil {
		t.Fatal("Stop invalidated the independently owned media reader")
	}
	closeInit()
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatal("releasing only initialization removed media still owned by its separate reader")
	}
	closeMedia()
	hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool {
		_, err := os.Stat(cachePath)
		return errors.Is(err, os.ErrNotExist)
	}, "Stop retained the private cache after both owned role readers drained")
}

func TestHTTPGeneratedWindowFMP4ObsoleteExactRolesDoNotEnsure(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
	id, initialization := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, true)
	mediaID, segment := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, false)
	if id != mediaID {
		t.Fatal("obsolete exact-role setup borrowed another producer")
	}
	if err := h.f.app.hls.manager.CancelJob(id, graph.session.key.scope); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{initialization, segment} {
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, target, nil, nil), http.StatusNotFound)
	}
	if ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph); len(ids) != 1 || ids[0] != id {
		t.Fatal("obsolete exact MAP or media cache miss admitted replacement production")
	}
}

func TestHTTPGeneratedWindowFMP4CachedMapRechecksCurrentAuthorization(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		logical        bool
		initialization bool
	}{
		{name: "logical_map_first", logical: true, initialization: true},
		{name: "exact_map_first", initialization: true},
		{name: "exact_media_first"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			h := newHLSGeneratedWindowHTTPFixture(t)
			graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
			hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
			initID, initialization := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, true)
			mediaID, segment := hlsGeneratedWindowFMP4HTTPAdmission(t, h, graph, 15, false)
			if initID != mediaID {
				t.Fatal("current authorization setup borrowed another producer's cached role")
			}
			_ = hlsGeneratedWindowFMP4HTTPBytes(t, h, initialization)
			_ = hlsGeneratedWindowFMP4HTTPBytes(t, h, segment)
			h.policy(t, false)
			target := segment
			if testCase.initialization {
				target = initialization
			}
			if testCase.logical {
				target = graph.initializationURLs[15]
			}
			// Each role is independently the first current-policy check. No
			// earlier denial has retired this subcase's real registration.
			expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, target, nil, nil), http.StatusForbidden)
			for _, obsolete := range []string{initialization, segment} {
				expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, obsolete, nil, nil), http.StatusNotFound)
			}
			if ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph); len(ids) != 1 || ids[0] != initID {
				t.Fatal("current authorization denial admitted another private producer")
			}
			hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool { return hlsGeneratedWindowFMP4HTTPHasNoPins(graph.session) },
				"fresh role authorization denial retained its registration or either role's handoff pins")
		})
	}
}

func hlsGeneratedWindowFMP4HTTPAssertFrames(t *testing.T, plan transcode.Plan, decoded []byte) {
	t.Helper()
	frameSize := plan.Width * plan.Height * 3
	if plan.HLS.RenditionCount != 0 || frameSize <= 0 || len(decoded) != 144*frameSize {
		t.Fatalf("actual single-output HLS client omitted complete source frames: bytes=%d frame_size=%d", len(decoded), frameSize)
	}
	for frame := 0; frame < 144; frame++ {
		pixels := decoded[frame*frameSize : (frame+1)*frameSize]
		var channels [3]int64
		for pixel := 0; pixel < len(pixels); pixel += 3 {
			for channel := 0; channel < 3; channel++ {
				channels[channel] += int64(pixels[pixel+channel])
			}
		}
		count := int64(plan.Width * plan.Height)
		red, green, blue := channels[0]/count, channels[1]/count, channels[2]/count
		if frame < 72 && (green < 170 || red > 60 || blue > 60) || frame >= 72 && (blue < 170 || red > 60 || green > 60) {
			t.Fatalf("actual full-VOD client frame %d selected content outside source [90,96): RGB=%d,%d,%d", frame, red, green, blue)
		}
	}
}

func TestHTTPGeneratedWindowFMP4FFmpegSeeksNegotiatedFullVOD(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
	registeredPlan := graph.session.key.plan
	type requestFact struct {
		number         int
		initialization bool
		producerID     string
	}
	var traceMu sync.Mutex
	var logical, exact []requestFact
	var traceOverflow bool
	const maxRoleRequests = 256
	// The caller holds traceMu. Overflow records one bounded failure fact;
	// the real HTTP handler still receives every unchanged client request.
	recordRequest := func(logicalRole bool, fact requestFact) {
		if len(logical)+len(exact) >= maxRoleRequests {
			traceOverflow = true
			return
		}
		if logicalRole {
			logical = append(logical, fact)
		} else {
			exact = append(exact, fact)
		}
	}
	clientServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		traceMu.Lock()
		if resource, found := hlsGeneratedWindowResourceFromName(registeredPlan, name); found {
			recordRequest(true, requestFact{number: resource.number, initialization: resource.initialization})
		} else if id := r.URL.Query().Get(hlsProducerQuery); id != "" {
			switch {
			case name == "init.mp4":
				recordRequest(false, requestFact{number: -1, initialization: true, producerID: id})
			case strings.HasSuffix(name, ".m4s"):
				recordRequest(false, requestFact{number: -1, producerID: id})
			}
		}
		traceMu.Unlock()
		// The proxy observes safe roles and IDs only. Every original request
		// reaches the real handler without rewriting a manifest or its URLs.
		h.f.handler.ServeHTTP(w, r)
	}))
	defer clientServer.Close()
	ctx, cancel := context.WithTimeout(h.f.ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto", "-ss", "90", "-i", clientServer.URL+graph.masterURL,
		"-t", "6", "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	output := &hlsGeneratedWindowHTTPClientOutput{limit: 168 * registeredPlan.Width * registeredPlan.Height * 3, cancel: cancel}
	diagnostics := &hlsGeneratedWindowHTTPClientOutput{limit: 64 << 10, cancel: cancel}
	command.Stdout, command.Stderr = output, diagnostics
	err := command.Run()
	if err != nil || diagnostics.buffer.Len() != 0 {
		// FFmpeg diagnostics and process errors may contain token-bearing URLs.
		t.Fatalf("actual FFmpeg negotiated full-VOD seek failed: error_type=%T diagnostic_bytes=%d", err, diagnostics.buffer.Len())
	}
	hlsGeneratedWindowFMP4HTTPAssertFrames(t, registeredPlan, output.buffer.Bytes())
	traceMu.Lock()
	logicalRequests, exactRequests := append([]requestFact(nil), logical...), append([]requestFact(nil), exact...)
	overflow := traceOverflow
	traceMu.Unlock()
	if overflow {
		t.Fatalf("actual full-VOD client exceeded its bounded safe role trace: limit=%d", maxRoleRequests)
	}
	allowed := map[int]bool{0: true, 1: true, 15: true, 16: true}
	seenTargetMap, seenTargetMedia := false, false
	for _, request := range logicalRequests {
		if !allowed[request.number] {
			t.Fatalf("actual full-VOD client encoded beyond bounded bootstrap and target: slot=%d initialization=%t", request.number, request.initialization)
		}
		seenTargetMap = seenTargetMap || request.number == 15 && request.initialization
		seenTargetMedia = seenTargetMedia || request.number == 15 && !request.initialization
	}
	roles := make(map[string][2]bool)
	for _, request := range exactRequests {
		pair := roles[request.producerID]
		index := 0
		if request.initialization {
			index = 1
		}
		pair[index] = true
		roles[request.producerID] = pair
	}
	ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph.hlsGeneratedWindowHTTPGraph)
	if !seenTargetMap || !seenTargetMedia || len(exactRequests) == 0 || len(ids) > len(allowed) {
		t.Fatal("actual FFmpeg client did not traverse both target roles through bounded logical and exact HTTP admission")
	}
	seenTargetProducer := false
	for _, id := range ids {
		state, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
		if err != nil && !(errors.Is(err, transcode.ErrJobCancelled) && state.ID == id) ||
			!allowed[state.Spec.Plan.HLS.Window.StartNumber] || !state.Spec.Plan.HLS.Window.RequireInputEvidence ||
			state.Spec.Plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 || state.Spec.Plan.HLS.RenditionCount != 0 ||
			state.Spec.Plan.StartTicks != int64(state.Spec.Plan.HLS.Window.StartNumber)*6*media.TicksPerSecond ||
			state.Spec.Plan.DurationTicks != 100*media.TicksPerSecond ||
			state.Spec.Plan.HLS.Window.EndTicks != min(state.Spec.Plan.StartTicks+6*media.TicksPerSecond, 100*media.TicksPerSecond) {
			t.Fatalf("actual HLS client caused a generic, rebased or complete-source producer: %+v, %v", state, err)
		}
		if state.Spec.Plan.HLS.Window.StartNumber == 15 {
			seenTargetProducer = roles[id] == ([2]bool{true, true})
		}
	}
	if !seenTargetProducer || graph.session.key.plan != registeredPlan {
		t.Fatal("actual full-VOD seek did not fetch its target MAP and media from the same negotiated producer")
	}
	current := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, current, http.StatusOK)
	if !bytes.Equal(current.body, graph.manifests[0]) {
		t.Fatal("actual client requests rewrote the original complete VOD presentation")
	}
	t.Logf("actual FFmpeg single fMP4 full-VOD seek: logical_role_requests=%d exact_role_requests=%d decoded_frames=144 bounded_bootstrap_seconds_at_most=12",
		len(logicalRequests), len(exactRequests))
}
