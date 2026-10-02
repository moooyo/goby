//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedWindowHTTPGraph struct {
	playID, masterURL string
	mainURLs          []string
	mediaURLs         [][]string
	manifests         [][]byte
	session           *hlsSession
	owner             clientSessionHTTPLogin
}

type hlsGeneratedWindowHTTPClientOutput struct {
	buffer bytes.Buffer
	limit  int
	cancel context.CancelFunc
}

func (output *hlsGeneratedWindowHTTPClientOutput) Write(data []byte) (int, error) {
	if len(data) > output.limit-output.buffer.Len() {
		output.cancel()
		return 0, errors.New("generated-window client output limit")
	}
	return output.buffer.Write(data)
}

// The fixture uses real catalog scanning, credentials and PlaybackInfo
// negotiation. Its source has exactly one ordinary AVC video track; no test
// override changes a negotiated plan, indexed duration or projected output.
func newHLSGeneratedWindowHTTPFixture(t *testing.T) *hlsHTTPFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for generated-window HTTP verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t, 8*time.Minute)
	root := t.TempDir()
	path := filepath.Join(root, "Generated.Window.Color.Sequence.mp4")
	// Filler keeps the real source bitrate above the adaptive planner's lower
	// bound. Color intervals still independently identify source [90,96).
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=100",
		"-vf", "drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,90)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,93)'",
		"-map", "0:v:0", "-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-bf", "0", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-b:v", "512k", "-minrate", "512k", "-maxrate", "512k",
		"-bufsize", "1024k", "-x264-params", "nal-hrd=cbr:filler=1", "-output_ts_offset", "2", "-movflags", "+faststart", path)
	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}, []string{root})
	if err != nil {
		t.Fatalf("create real generated-window catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() { f.app.notifier.Close() })
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath = ffmpeg, ffprobe
	f.app.cfg.MediaRoots = []string{root}
	f.app.cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	f.cfg = f.app.cfg
	initializeFixtureSettings(t, f)
	runtime, err := newHLSRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatalf("create real generated-window runtime: %v", err)
	}
	if runtime.generatedWindowsEnabled {
		t.Fatal("experimental generated windows were enabled by default")
	}
	runtime.generatedWindowsEnabled = true
	f.app.hls = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close generated-window runtime: %v", err)
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Generated Window Movies", "movies", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == path && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil || len(item.Media.Streams) != 1 || item.Media.Streams[0].CodecType != "video" ||
		item.Media.Streams[0].Codec != "h264" || !item.Media.FormatStartKnown || item.Media.FormatStartTicks != 2*media.TicksPerSecond {
		t.Fatal("real scan did not retain the sole AVC source and its separate format origin")
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	certificate, certificateErr := transcode.MeasureGeneratedMP4SourceEndpoint(f.ctx, input, 0)
	closeErr := input.Close()
	if certificateErr != nil || closeErr != nil || certificate.SampleCount != 2400 || !certificate.DurationTicksExact ||
		certificate.DurationTicks != 100*media.TicksPerSecond || certificate.Origin != (transcode.GeneratedRational{Num: 2, Den: 1}) ||
		certificate.End != (transcode.GeneratedRational{Num: 102, Den: 1}) {
		t.Fatalf("real source did not produce its independent native sample-set endpoint: %+v, %v, %v", certificate, certificateErr, closeErr)
	}
	if item.Media.DurationTicks != certificate.DurationTicks && item.Media.DurationTicks != 102*media.TicksPerSecond {
		t.Fatalf("catalog source duration is neither the native span nor the independently observed absolute end: %d", item.Media.DurationTicks)
	}
	t.Logf("real source facts: catalog_duration_ticks=%d native_duration_ticks=%d format_origin_ticks=%d samples=%d",
		item.Media.DurationTicks, certificate.DurationTicks, item.Media.FormatStartTicks, certificate.SampleCount)
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, path: path, libraryID: collection.ID, item: item}
	h.policy(t, true)
	f.handler = f.app.Handler()
	h.server = httptest.NewServer(f.handler)
	t.Cleanup(h.server.Close)
	return h
}

func hlsGeneratedWindowHTTPPrepare(t *testing.T, h *hlsHTTPFixture, start int64) hlsGeneratedWindowHTTPGraph {
	t.Helper()
	owner := h.accounts.viewer
	body := map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "AllowVideoStreamCopy": false,
		"StartTimeTicks": start, "MaxStreamingBitrate": 2_000_000,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264",
			"EnableAdaptiveBitrate": true, "MaxWidth": 160, "MaxHeight": 96, "SegmentLength": 6,
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
		t.Fatal("real PlaybackInfo did not negotiate one permitted adaptive source")
	}
	masterURL, ok := negotiation.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || negotiation.MediaSources[0]["TranscodingContainer"] != "ts" || negotiation.MediaSources[0]["TranscodingSubProtocol"] != "hls" {
		t.Fatal("the adaptive request changed the actual negotiated segment container")
	}
	parsed := hlsHTTPURL(t, masterURL, owner.headers.Get("X-Emby-Token"))
	h.f.app.hls.mu.Lock()
	session := h.f.app.hls.sessions[parsed.Query().Get("GobyHlsId")]
	h.f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("PlaybackInfo did not establish its immutable HLS registration")
	}
	plan := session.key.plan
	if !transcode.GeneratedHLS(plan) || plan.HLS.SegmentType != "mpegts" || plan.HLS.RenditionCount < 2 || plan.HLS.RenditionCount > transcode.MaxHLSRenditions ||
		plan.VideoCodec != "h264" || plan.AudioStreamIndex != -1 || plan.FrameRate != 24 || plan.HLS.Window != (transcode.HLSWindow{}) || plan.StartTicks != 0 {
		t.Fatalf("real negotiated TS profile did not produce an eligible encoded ladder: %+v", plan)
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != plan.HLS.RenditionCount {
		t.Fatal("real generated master did not advertise every negotiated TS rendition")
	}
	for index, variant := range variants {
		child := hlsHTTPURL(t, variant, owner.headers.Get("X-Emby-Token"))
		if !strings.HasSuffix(child.Path, "/v"+strconv.Itoa(index)+".m3u8") || child.Query().Get("GobyHlsId") != session.id {
			t.Fatal("negotiated variants lost their distinct output identities")
		}
	}
	return hlsGeneratedWindowHTTPGraph{playID: negotiation.PlayID, masterURL: masterURL, mainURLs: variants,
		mediaURLs: make([][]string, len(variants)), manifests: make([][]byte, len(variants)), session: session, owner: owner}
}

// Disable redirect following so admission and exact immutable bytes are
// independently observable HTTP operations, including their status and scope.
func hlsGeneratedWindowHTTPDo(ctx context.Context, h *hlsHTTPFixture, method, target string, body any, headers http.Header) (hlsHTTPResponse, error) {
	var input io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return hlsHTTPResponse{}, err
		}
		input = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, h.server.URL+target, input)
	if err != nil {
		return hlsHTTPResponse{}, errors.New("create generated-window HTTP request")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := *h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		// Transport errors can contain token-bearing URLs.
		return hlsHTTPResponse{}, fmt.Errorf("generated-window HTTP transport failed (%T)", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 17<<20))
	if err != nil || len(data) >= 17<<20 {
		return hlsHTTPResponse{}, errors.New("read bounded generated-window HTTP response")
	}
	return hlsHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: data}, nil
}

func hlsGeneratedWindowHTTPRequest(t *testing.T, h *hlsHTTPFixture, method, target string, body any, headers http.Header) hlsHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.f.ctx, 30*time.Second)
	defer cancel()
	response, err := hlsGeneratedWindowHTTPDo(ctx, h, method, target, body, headers)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func hlsGeneratedWindowHTTPJobIDs(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph) []string {
	t.Helper()
	rows, err := h.f.pool.Query(h.f.ctx, "SELECT id FROM encoding_jobs WHERE auth_session_id=$1 AND play_session_id=$2 ORDER BY created_at,id", graph.owner.id, graph.playID)
	if err != nil {
		t.Fatal("read generated-window encoding identities")
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestHTTPGeneratedWindowNegotiatesRealTSAdaptiveProfile(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != 0 {
		t.Fatal("negotiating or reading the adaptive master started a media producer")
	}
}

func hlsGeneratedWindowHTTPLoad(t *testing.T, h *hlsHTTPFixture, graph *hlsGeneratedWindowHTTPGraph) {
	t.Helper()
	for variant, mainURL := range graph.mainURLs {
		response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, mainURL, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		children := hlsHTTPManifestChildren(response.body)
		if len(children) != 17 || !bytes.Contains(response.body, []byte("#EXT-X-PLAYLIST-TYPE:VOD\n")) ||
			!bytes.Contains(response.body, []byte("#EXT-X-MEDIA-SEQUENCE:0\n")) || !bytes.Contains(response.body, []byte("#EXT-X-TARGETDURATION:6\n")) ||
			!bytes.Contains(response.body, []byte("#EXT-X-START:TIME-OFFSET=90.0000000,PRECISE=YES\n")) ||
			!bytes.HasSuffix(response.body, []byte("#EXT-X-ENDLIST\n")) || bytes.Count(response.body, []byte("#EXT-X-DISCONTINUITY\n")) != 17 ||
			bytes.Contains(response.body, []byte("#EXT-X-GAP")) || bytes.Contains(response.body, []byte("#EXT-X-MAP")) {
			t.Fatalf("rendition %d did not advertise the complete immutable native VOD timeline", variant)
		}
		var total big.Rat
		for _, line := range strings.Split(string(response.body), "\n") {
			if !strings.HasPrefix(line, "#EXTINF:") {
				continue
			}
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, ok := new(big.Rat).SetString(value)
			if !ok || duration.Sign() <= 0 {
				t.Fatal("logical VOD contains an invalid duration")
			}
			total.Add(&total, duration)
		}
		if total.Cmp(new(big.Rat).SetInt64(100)) != 0 {
			t.Fatalf("rendition %d replaced the native span with metadata or window EOF: %s", variant, total.RatString())
		}
		for number, child := range children {
			parsed := hlsHTTPURL(t, child, graph.owner.headers.Get("X-Emby-Token"))
			if !strings.HasSuffix(parsed.Path, fmt.Sprintf("/v%d-window-segment-%06d.ts", variant, number)) ||
				parsed.Query().Get(hlsGeneratedWindowGraphQuery) != graph.session.id || parsed.Query().Get(hlsProducerQuery) != "" {
				t.Fatal("logical VOD slot did not retain its rendition and admission identity")
			}
		}
		graph.mediaURLs[variant], graph.manifests[variant] = children, bytes.Clone(response.body)
	}
	ids := hlsGeneratedWindowHTTPJobIDs(t, h, *graph)
	if len(ids) != 1 {
		t.Fatalf("initial high seek produced %d jobs instead of one closed adaptive window", len(ids))
	}
	record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, ids[0])
	if err != nil || record.State != "completed" || record.Spec.Plan.StartTicks != 90*media.TicksPerSecond ||
		record.Spec.Plan.DurationTicks != 100*media.TicksPerSecond || record.Spec.Plan.HLS.Window.EndTicks != 96*media.TicksPerSecond ||
		record.Spec.Plan.HLS.Window.StartNumber != 15 || !record.Spec.Plan.HLS.Window.RequireInputEvidence ||
		record.Spec.Plan.HLS.RenditionCount != len(graph.mainURLs) {
		t.Fatalf("high-seek producer generated a prefix or claimed the complete source: %+v, %v", record, err)
	}
	entries, err := os.ReadDir(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	segments := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".ts") {
			segments++
			if !strings.HasSuffix(entry.Name(), "-segment-000015.ts") {
				t.Fatal("initial high seek left a source prefix or another output slot in the cache")
			}
		}
	}
	if segments != len(graph.mainURLs) {
		t.Fatal("adaptive private producer did not retain exactly one segment per rendition")
	}
}

func hlsGeneratedWindowHTTPExact(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph, variant, number int) (string, string, hlsHTTPResponse) {
	t.Helper()
	admission := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[variant][number], nil, nil)
	expectHLSHTTPStatus(t, admission, http.StatusTemporaryRedirect)
	location := admission.header.Get("Location")
	parsed := hlsHTTPURL(t, location, graph.owner.headers.Get("X-Emby-Token"))
	id := parsed.Query().Get(hlsProducerQuery)
	if id == "" || parsed.Query().Get(hlsGeneratedWindowGraphQuery) != "" ||
		!strings.HasSuffix(parsed.Path, fmt.Sprintf("/v%d-segment-%06d.ts", variant, number)) {
		t.Fatal("logical admission did not redirect to its exact private producer resource")
	}
	mediaResponse := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, location, nil, nil)
	expectHLSHTTPStatus(t, mediaResponse, http.StatusOK)
	if len(mediaResponse.body) == 0 || !strings.HasPrefix(mediaResponse.header.Get("Content-Type"), "video/mp2t") {
		t.Fatal("bound private media did not return complete TS bytes")
	}
	return id, location, mediaResponse
}

func hlsGeneratedWindowHTTPDecode(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph, variant int, data []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("received-window-v%d.ts", variant))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(h.f.ctx, 20*time.Second)
	defer cancel()
	rendition := graph.session.key.plan.HLS.Renditions[variant]
	output := &hlsGeneratedWindowHTTPClientOutput{limit: 168 * rendition.Width * rendition.Height * 3, cancel: cancel}
	diagnostics := &hlsGeneratedWindowHTTPClientOutput{limit: 64 << 10, cancel: cancel}
	command := exec.CommandContext(ctx, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
		"-i", path, "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	command.Stdout, command.Stderr = output, diagnostics
	if err := command.Run(); err != nil || diagnostics.buffer.Len() != 0 {
		t.Fatalf("independent exact-artifact decode failed: error_type=%T diagnostic_bytes=%d", err, diagnostics.buffer.Len())
	}
	hlsGeneratedWindowHTTPAssertFrames(t, graph, variant, output.buffer.Bytes())
}

func hlsGeneratedWindowHTTPAssertFrames(t *testing.T, graph hlsGeneratedWindowHTTPGraph, variant int, decoded []byte) {
	t.Helper()
	rendition := graph.session.key.plan.HLS.Renditions[variant]
	frameSize := rendition.Width * rendition.Height * 3
	if frameSize <= 0 || len(decoded) != 144*frameSize {
		t.Fatalf("independent exact-artifact decode omitted complete frames: rendition=%d bytes=%d frame_size=%d", variant, len(decoded), frameSize)
	}
	for frame := 0; frame < 144; frame++ {
		pixels := decoded[frame*frameSize : (frame+1)*frameSize]
		var channels [3]int64
		for pixel := 0; pixel < len(pixels); pixel += 3 {
			for channel := 0; channel < 3; channel++ {
				channels[channel] += int64(pixels[pixel+channel])
			}
		}
		count := int64(rendition.Width * rendition.Height)
		red, green, blue := channels[0]/count, channels[1]/count, channels[2]/count
		if frame < 72 && (green < 170 || red > 60 || blue > 60) || frame >= 72 && (blue < 170 || red > 60 || green > 60) {
			t.Fatalf("exact rendition %d frame %d selected content outside source [90,96): RGB=%d,%d,%d", variant, frame, red, green, blue)
		}
	}
}

func hlsGeneratedWindowHTTPProgress(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph, event string, paused bool) {
	t.Helper()
	body := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 91 * media.TicksPerSecond,
		"EventName": event, "IsPaused": paused}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing/Progress", body, graph.owner.headers), http.StatusNoContent)
}

func TestHTTPGeneratedWindowHighSeekPauseAndExactBytes(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	var firstID string
	var firstLocation string
	var firstBytes []byte
	for variant := range graph.mainURLs {
		id, location, received := hlsGeneratedWindowHTTPExact(t, h, graph, variant, 15)
		if variant == 0 {
			firstID, firstLocation, firstBytes = id, location, bytes.Clone(received.body)
		} else if id != firstID {
			t.Fatal("one adaptive source window borrowed another producer's rendition")
		}
		hlsGeneratedWindowHTTPDecode(t, h, graph, variant, received.body)
	}
	started := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 90 * media.TicksPerSecond}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing", started, graph.owner.headers), http.StatusNoContent)
	hlsGeneratedWindowHTTPProgress(t, h, graph, "Pause", false)
	_, pausedLocation, paused := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
	if pausedLocation != firstLocation || !bytes.Equal(paused.body, firstBytes) {
		t.Fatal("committed pause replaced its complete cached private bytes")
	}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil), http.StatusServiceUnavailable)
	ping := "/emby/Sessions/Playing/Ping?" + url.Values{"PlaySessionId": {graph.playID}}.Encode()
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, ping, nil, graph.owner.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil), http.StatusServiceUnavailable)
	if ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph); len(ids) != 1 || ids[0] != firstID {
		t.Fatal("paused cold GET or heartbeat resumed production")
	}
	hlsGeneratedWindowHTTPProgress(t, h, graph, "Unpause", true)
	admission := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil)
	expectHLSHTTPStatus(t, admission, http.StatusTemporaryRedirect)
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != 2 {
		t.Fatal("committed Unpause did not admit exactly one independent source window")
	}
	for variant, mainURL := range graph.mainURLs {
		current := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, mainURL, nil, nil)
		expectHLSHTTPStatus(t, current, http.StatusOK)
		if !bytes.Equal(current.body, graph.manifests[variant]) {
			t.Fatal("cached or new source windows rewrote the client-loaded VOD presentation")
		}
	}
}

func hlsGeneratedWindowHTTPWait(t *testing.T, h *hlsHTTPFixture, timeout time.Duration, condition func() bool, failure string) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal(failure)
		case <-h.f.ctx.Done():
			t.Fatal("generated-window fixture exceeded its owned lifetime")
		}
	}
}

func TestHTTPGeneratedWindowLateGETCannotReplacePendingSourceWindow(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	oldID, oldLocation, _ := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
	probes := h.f.app.hls.probes
	reserved := 0
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			for range reserved {
				<-probes
			}
		})
	}
	t.Cleanup(release)
	for reserved < cap(probes) {
		select {
		case probes <- struct{}{}:
			reserved++
		default:
			t.Fatal("initial source-window proofs did not release their probe reservations")
		}
	}
	ctx, cancel := context.WithTimeout(h.f.ctx, 30*time.Second)
	type outcome struct {
		response hlsHTTPResponse
		err      error
	}
	result := make(chan outcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		response, err := hlsGeneratedWindowHTTPDo(ctx, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil)
		result <- outcome{response, err}
	}()
	// Register after the fixture so failure cleanup releases the gate and
	// joins its owned request before runtime shutdown waits for that request.
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("controlled HTTP admission did not join during failure cleanup")
		}
	})
	var pending *hlsAdmission
	hlsGeneratedWindowHTTPWait(t, h, 10*time.Second, func() bool {
		graph.session.mu.Lock()
		defer graph.session.mu.Unlock()
		pending = graph.session.admission
		return pending != nil && pending.first == 0 && pending.spec.Plan.HLS.Window.RequireInputEvidence && pending.ctx.Err() == nil
	}, "cold logical GET did not enter its independently owned admission")
	var newID string
	hlsGeneratedWindowHTTPWait(t, h, 10*time.Second, func() bool {
		ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
		if len(ids) != 2 {
			return false
		}
		for _, id := range ids {
			if id != oldID {
				newID = id
			}
		}
		state, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, newID)
		if err != nil || state.State == "failed" || state.State == "cancelled" {
			t.Fatalf("real private producer failed before its controlled proof gate: %+v, %v", state, err)
		}
		return state.State == "completed"
	}, "real producer did not complete while its publication proof was held")
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][16], nil, nil), http.StatusTooManyRequests)
	if err := h.f.app.hls.manager.CancelJob(oldID, graph.session.key.scope); err != nil {
		t.Fatal(err)
	}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, oldLocation, nil, nil), http.StatusNotFound)
	graph.session.mu.Lock()
	stillOwned := graph.session.admission == pending && pending.ctx.Err() == nil && !graph.session.closed
	graph.session.mu.Unlock()
	if !stillOwned || len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != 2 {
		t.Fatal("an unrelated late GET or obsolete exact cache miss replaced current private work")
	}
	release()
	select {
	case completed := <-result:
		if completed.err != nil {
			t.Fatal(completed.err)
		}
		expectHLSHTTPStatus(t, completed.response, http.StatusTemporaryRedirect)
		location := hlsHTTPURL(t, completed.response.header.Get("Location"), graph.owner.headers.Get("X-Emby-Token"))
		if location.Query().Get(hlsProducerQuery) != newID {
			t.Fatal("controlled admission redirected to another producer's output")
		}
	case <-ctx.Done():
		t.Fatal("controlled source-window request did not publish after proof admission resumed")
	}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, oldLocation, nil, nil), http.StatusNotFound)
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != 2 {
		t.Fatal("obsolete exact URL created a replacement after a new window was published")
	}
}

func TestHTTPGeneratedWindowRedirectGraceStopAndOwnedReaderDrain(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	id, location, _ := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
	graph.session.mu.Lock()
	binding := graph.session.windowGraph.slots[15]
	graph.session.mu.Unlock()
	if binding.redirectedUntil.IsZero() || binding.redirectPins[0] == nil {
		t.Fatal("HTTP redirect did not retain its bounded exact-artifact handoff pin")
	}
	// Grace is wall-clock behavior. The maintenance cadence can add one full
	// tick, so the observation deadline covers that cadence without aging state.
	hlsGeneratedWindowHTTPWait(t, h, 16*time.Second, func() bool {
		graph.session.mu.Lock()
		defer graph.session.mu.Unlock()
		return graph.session.windowGraph.slots[15].redirectPins[0] == nil
	}, "redirect handoff pin survived its five-second grace and maintenance tick")
	if time.Now().Before(binding.redirectedUntil) {
		t.Fatal("redirect handoff pin was removed before its promised grace")
	}
	reader, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, id, fmt.Sprintf("v0-segment-%06d.ts", 15))
	if err != nil {
		t.Fatal(err)
	}
	var readerOnce sync.Once
	closeReader := func() { readerOnce.Do(func() { _ = reader.Close() }) }
	t.Cleanup(closeReader)
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {graph.playID}, "DeviceId": {graph.owner.deviceID}}.Encode()
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodDelete, stop, nil, graph.owner.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, location, nil, nil), http.StatusNotFound)
	cachePath := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatal("Stop removed a real manager artifact still pinned by its owned reader")
	}
	var first [188]byte
	if _, err := reader.ReadAt(first[:], 0); err != nil || first[0] != 0x47 {
		t.Fatal("Stop invalidated bytes owned by an already-open complete reader")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- h.f.app.hls.Close(closeCtx) }()
	hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool {
		reporter, supported := h.f.app.hls.manager.(interface{ Health() transcode.Health })
		return supported && reporter.Health().Code == "manager_closed"
	}, "backend Close did not enter shutdown while its owned reader remained open")
	select {
	case err := <-closed:
		t.Fatalf("runtime Close bypassed its outstanding artifact reader: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	closeReader()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("runtime did not drain after the final owned reader closed: %v", err)
		}
	case <-closeCtx.Done():
		t.Fatal("runtime Close did not complete after owned-reader release")
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reader-drained runtime retained its closed private cache: %v", err)
	}
}

func TestHTTPGeneratedWindowCachedRedirectRechecksCurrentPolicy(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	_, location, _ := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
	before := len(hlsGeneratedWindowHTTPJobIDs(t, h, graph))
	h.policy(t, false)
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][15], nil, nil), http.StatusForbidden)
	// The first current-policy denial retires the immutable registration.
	// Its former exact URI is therefore hidden by the subsequent registry
	// lookup, before another source or artifact authorization can run.
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, location, nil, nil), http.StatusNotFound)
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != before {
		t.Fatal("authorization rejection admitted another private producer")
	}
	hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool {
		graph.session.mu.Lock()
		defer graph.session.mu.Unlock()
		if !graph.session.closed {
			return false
		}
		if graph.session.windowGraph == nil {
			return true
		}
		for _, binding := range graph.session.windowGraph.slots {
			for _, pin := range binding.redirectPins {
				if pin != nil {
					return false
				}
			}
		}
		return true
	}, "fresh authorization denial retained the registration or redirect handoff pins")
}

func TestHTTPGeneratedWindowFFmpegClientSeeksThroughLogicalRedirects(t *testing.T) {
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	var traceMu sync.Mutex
	var logicalRequests []int
	var exactRequests int
	clientServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		traceMu.Lock()
		if number, _, logical := hlsGeneratedWindowVariantNumber(name, len(graph.mainURLs)); logical {
			logicalRequests = append(logicalRequests, number)
		} else if strings.HasSuffix(name, ".ts") && r.URL.Query().Get(hlsProducerQuery) != "" {
			exactRequests++
		}
		traceMu.Unlock()
		h.f.handler.ServeHTTP(w, r)
	}))
	defer clientServer.Close()
	ctx, cancel := context.WithTimeout(h.f.ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto", "-ss", "90", "-i", clientServer.URL+graph.mainURLs[0],
		"-t", "6", "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	primary := graph.session.key.plan.HLS.Renditions[0]
	output := &hlsGeneratedWindowHTTPClientOutput{limit: 168 * primary.Width * primary.Height * 3, cancel: cancel}
	diagnostics := &hlsGeneratedWindowHTTPClientOutput{limit: 64 << 10, cancel: cancel}
	command.Stdout, command.Stderr = output, diagnostics
	err := command.Run()
	if err != nil || diagnostics.buffer.Len() != 0 {
		// Neither diagnostics nor command errors may expose token-bearing URLs.
		t.Fatalf("actual FFmpeg HLS seek did not finish cleanly: error_type=%T diagnostic_bytes=%d", err, diagnostics.buffer.Len())
	}
	hlsGeneratedWindowHTTPAssertFrames(t, graph, 0, output.buffer.Bytes())
	traceMu.Lock()
	requests, exact := append([]int(nil), logicalRequests...), exactRequests
	traceMu.Unlock()
	if len(requests) == 0 || exact == 0 {
		t.Fatal("actual FFmpeg client did not traverse both logical admission and exact producer HTTP resources")
	}
	// FFmpeg's complete-VOD header opens the beginning before applying -ss.
	// The actual native-seek diagnostic established slots 0/1 as its bounded
	// bootstrap. Accept that cost without allowing the long source prefix.
	allowed := map[int]bool{0: true, 1: true, 15: true, 16: true}
	seenTarget := false
	for _, number := range requests {
		if !allowed[number] {
			t.Fatalf("actual high-seek client encoded beyond its bounded bootstrap and target: slot=%d", number)
		}
		seenTarget = seenTarget || number == 15
	}
	ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
	if !seenTarget || len(ids) > len(allowed) {
		t.Fatal("actual native seek did not retain a bounded bootstrap and its target window")
	}
	for _, id := range ids {
		state, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
		if err != nil && !(errors.Is(err, transcode.ErrJobCancelled) && state.ID == id) ||
			!allowed[state.Spec.Plan.HLS.Window.StartNumber] || !state.Spec.Plan.HLS.Window.RequireInputEvidence ||
			state.Spec.Plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV1 ||
			state.Spec.Plan.StartTicks != int64(state.Spec.Plan.HLS.Window.StartNumber)*6*media.TicksPerSecond ||
			state.Spec.Plan.HLS.Window.EndTicks != min(state.Spec.Plan.StartTicks+6*media.TicksPerSecond, 100*media.TicksPerSecond) {
			t.Fatalf("actual HLS client caused a nonnative or complete-source producer: %+v, %v", state, err)
		}
	}
	t.Logf("actual FFmpeg native seek client: logical_slots=%v exact_requests=%d decoded_frames=144 bounded_bootstrap_seconds_at_most=12", requests, exact)
}
