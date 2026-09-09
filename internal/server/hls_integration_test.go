//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type hlsHTTPFixture struct {
	f                                *serverFixture
	server                           *httptest.Server
	accounts                         clientSessionHTTPAccounts
	ffmpeg, ffprobe, path, libraryID string
	item                             library.Item
}

type hlsHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

type hlsHTTPGraph struct {
	playID, hlsID, masterURL, mainURL string
	children                          []string
	durations                         []float64
	main                              []byte
}

func hlsHTTPMediaCommand(t *testing.T, executable string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, executable, args...)
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("real HLS media command failed (%T): %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("real HLS media command reported a decode error: %s", stderr.String())
	}
	return output
}

func newHLSHTTPFixture(t *testing.T) *hlsHTTPFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real Linux HLS HTTP verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	source := filepath.Join(root, "HLS.Color.Sequence.mp4")
	// Four full-GOP color intervals make a wrong source-global segment visible
	// independently of its timestamp. This is real 24 fps H.264/AAC media.
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=12",
		"-vf", "drawbox=color=green:t=fill:enable='gte(t,3)*lt(t,6)',drawbox=color=blue:t=fill:enable='gte(t,6)*lt(t,9)',drawbox=color=yellow:t=fill:enable='gte(t,9)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", source)
	f.app.notifier.Close()
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close initial HLS fixture catalog (%T)", err)
	}
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}, []string{root})
	if err != nil {
		t.Fatalf("create real-probe HLS catalog (%T)", err)
	}
	f.app.library = catalog
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() {
		f.app.notifier.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close HLS fixture catalog (%T)", err)
		}
	})
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath = ffmpeg, ffprobe
	f.app.cfg.MediaRoots = []string{root}
	f.app.cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	f.cfg = f.app.cfg
	runtime, err := newHLSRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatalf("create real HLS runtime (%T)", err)
	}
	f.app.hls = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close real HLS runtime (%T)", err)
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Real HLS Movies", "movies", []string{root})
	if err != nil {
		t.Fatalf("create real HLS media library (%T)", err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("query real HLS source (%T)", err)
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == source && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil || math.Abs(float64(item.Media.DurationTicks)/float64(media.TicksPerSecond)-12) > .1 {
		t.Fatal("real HLS source was not indexed with its full duration")
	}
	fixture := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe,
		path: source, libraryID: collection.ID, item: item}
	fixture.policy(t, true)
	f.handler = f.app.Handler()
	fixture.server = httptest.NewServer(f.handler)
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (h *hlsHTTPFixture) policy(t *testing.T, allow bool) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{h.libraryID}, "EnableMediaPlayback": allow,
		"EnablePlaybackRemuxing": true, "EnableVideoPlaybackTranscoding": true, "EnableAudioPlaybackTranscoding": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.f.pool.Exec(h.f.ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", h.accounts.viewer.userID, encoded); err != nil {
		t.Fatal("set HLS viewer playback policy")
	}
}

func (h *hlsHTTPFixture) request(t *testing.T, method, target string, body any, headers http.Header) hlsHTTPResponse {
	t.Helper()
	var input io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(h.f.ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, h.server.URL+target, input)
	if err != nil {
		t.Fatalf("create HLS HTTP request (%T)", err)
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.server.Client().Do(request)
	if err != nil {
		// Transport errors may contain token-bearing URLs.
		t.Fatalf("perform HLS HTTP request (%T)", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 17<<20))
	if err != nil || len(data) >= 17<<20 {
		t.Fatal("HLS response could not be read within its fixture limit")
	}
	return hlsHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: data}
}

func expectHLSHTTPStatus(t *testing.T, response hlsHTTPResponse, expected int) {
	t.Helper()
	if response.status != expected {
		t.Fatalf("HLS response status = %d, want %d", response.status, expected)
	}
}

func hlsHTTPURL(t *testing.T, raw, token string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") ||
		strings.HasPrefix(parsed.Path, "//") || parsed.Query().Get("api_key") != token {
		t.Fatal("HLS child must be an absolute catalog path carrying the authenticated token")
	}
	return parsed
}

func hlsHTTPManifestChildren(body []byte) []string {
	var children []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			children = append(children, line)
		}
	}
	return children
}

func (h *hlsHTTPFixture) graph(t *testing.T, login clientSessionHTTPLogin, start int64) hlsHTTPGraph {
	t.Helper()
	body := map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "StartTimeTicks": start,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac",
			"MaxWidth": 96, "MaxHeight": 54, "SegmentLength": 3,
		}}},
	}
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, login.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var object struct {
		PlaySessionID string           `json:"PlaySessionId"`
		MediaSources  []map[string]any `json:"MediaSources"`
		ErrorCode     string           `json:"ErrorCode"`
	}
	if err := json.Unmarshal(response.body, &object); err != nil || object.PlaySessionID == "" || len(object.MediaSources) != 1 || object.ErrorCode != "" {
		t.Fatal("PlaybackInfo did not return a supported HLS source")
	}
	source := object.MediaSources[0]
	masterURL, ok := source["TranscodingUrl"].(string)
	if !ok || source["SupportsTranscoding"] != true || source["TranscodingContainer"] != "ts" || source["TranscodingSubProtocol"] != "hls" {
		t.Fatal("PlaybackInfo did not advertise the enabled HLS output")
	}
	token := login.headers.Get("X-Emby-Token")
	parsed := hlsHTTPURL(t, masterURL, token)
	if parsed.Query().Get("PlaySessionId") != object.PlaySessionID || parsed.Query().Get("GobyHlsId") == "" {
		t.Fatal("negotiated master URL omitted its immutable HLS and playback identities")
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	mainURLs := hlsHTTPManifestChildren(master.body)
	if len(mainURLs) != 1 || !bytes.Contains(master.body, []byte("RESOLUTION=96x54")) {
		t.Fatal("master playlist did not advertise one resized output")
	}
	hlsHTTPURL(t, mainURLs[0], token)
	main := h.request(t, http.MethodGet, mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	graph := hlsHTTPGraph{playID: object.PlaySessionID, hlsID: parsed.Query().Get("GobyHlsId"), masterURL: masterURL,
		mainURL: mainURLs[0], main: main.body, children: hlsHTTPManifestChildren(main.body)}
	for _, line := range strings.Split(string(main.body), "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			seconds, err := strconv.ParseFloat(value, 64)
			if err != nil || seconds <= 0 {
				t.Fatal("HLS manifest has an invalid segment duration")
			}
			graph.durations = append(graph.durations, seconds)
		}
	}
	if len(graph.children) != 4 || len(graph.durations) != len(graph.children) ||
		!bytes.Contains(main.body, []byte("#EXT-X-MEDIA-SEQUENCE:0\n")) ||
		!bytes.Contains(main.body, []byte("#EXT-X-PLAYLIST-TYPE:VOD\n")) || !bytes.HasSuffix(main.body, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatal("HLS media playlist did not preserve the complete VOD segment table")
	}
	var duration float64
	for index, child := range graph.children {
		parsed := hlsHTTPURL(t, child, token)
		if !strings.HasSuffix(parsed.Path, "/hls1/"+graph.hlsID+"/"+strconv.Itoa(index)+".ts") {
			t.Fatal("HLS child path does not use source-global segment numbers")
		}
		duration += graph.durations[index]
	}
	if math.Abs(duration-float64(h.item.Media.DurationTicks)/float64(media.TicksPerSecond)) > .01 {
		t.Error("HLS media playlist duration differs from the full indexed source")
	}
	if start > 0 {
		want := fmt.Sprintf("#EXT-X-START:TIME-OFFSET=%d.%07d,PRECISE=YES", start/media.TicksPerSecond, start%media.TicksPerSecond)
		if !bytes.Contains(main.body, []byte(want)) {
			t.Error("HLS starting position was not retained as a full-VOD playback hint")
		}
	} else if bytes.Contains(main.body, []byte("#EXT-X-START:")) {
		t.Error("returning to source zero retained a previous nonzero start hint")
	}
	return graph
}

func hlsHTTPWithoutToken(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal("parse HLS fixture URL")
	}
	query := parsed.Query()
	query.Del("api_key")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type hlsHTTPStreamFact struct {
	Codec, Type, Start, Duration string
	Width, Height                int
}

func (h *hlsHTTPFixture) verifySegment(t *testing.T, data []byte, number int, duration float64) map[string]hlsHTTPStreamFact {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("received-segment-%d.ts", number))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Type     string `json:"codec_type"`
			Start    string `json:"start_time"`
			Duration string `json:"duration"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		} `json:"streams"`
	}
	encoded := hlsHTTPMediaCommand(t, h.ffprobe, "-v", "error", "-show_entries", "stream=codec_name,codec_type,start_time,duration,width,height", "-of", "json", path)
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal("decode received HLS segment facts")
	}
	facts := map[string]hlsHTTPStreamFact{}
	for _, stream := range probe.Streams {
		facts[stream.Type] = hlsHTTPStreamFact{stream.Codec, stream.Type, stream.Start, stream.Duration, stream.Width, stream.Height}
		start, startErr := strconv.ParseFloat(stream.Start, 64)
		span, spanErr := strconv.ParseFloat(stream.Duration, 64)
		if startErr != nil || spanErr != nil || math.Abs(start-(1+float64(number)*3)) > .15 || math.Abs(span-duration) > .15 {
			t.Errorf("segment %d %s does not retain its source-global PTS and span", number, stream.Type)
		}
	}
	if len(facts) != 2 || facts["video"].Codec != "h264" || facts["audio"].Codec != "aac" || facts["video"].Width != 96 || facts["video"].Height != 54 {
		t.Fatal("received HLS segment is not the negotiated resized H.264/AAC output")
	}
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-f", "null", "-")
	pixel := hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
		"-i", path, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=1:1", "-pix_fmt", "rgb24", "-threads", "1", "-f", "rawvideo", "-")
	want := [][3]int{{255, 0, 0}, {0, 128, 0}, {0, 0, 255}, {255, 255, 0}}[number]
	if len(pixel) != 3 {
		t.Fatal("decoded HLS first frame did not yield one RGB pixel")
	}
	for channel, expected := range want {
		if math.Abs(float64(int(pixel[channel])-expected)) > 20 {
			t.Errorf("segment %d contains the wrong source color interval", number)
			break
		}
	}
	return facts
}

func (h *hlsHTTPFixture) userDataSnapshot(t *testing.T) string {
	t.Helper()
	var data string
	if err := h.f.pool.QueryRow(h.f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY item_id), '[]'::jsonb)::text
		FROM user_item_data d WHERE user_id = $1`, h.accounts.viewer.userID).Scan(&data); err != nil {
		t.Fatal("read HLS user-data snapshot")
	}
	return data
}

func (h *hlsHTTPFixture) retired(t *testing.T, graph hlsHTTPGraph) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		h.f.app.hls.mu.Lock()
		_, exists := h.f.app.hls.sessions[graph.hlsID]
		h.f.app.hls.mu.Unlock()
		var active int
		if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*) FROM encoding_jobs
			WHERE auth_session_id = $1 AND play_session_id = $2 AND state IN ('queued','running')`,
			h.accounts.viewer.id, graph.playID).Scan(&active); err != nil {
			t.Fatal("read retired HLS producer count")
		}
		if !exists && active == 0 {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("HLS cleanup did not retire its registry and active producers")
		}
	}
}

func TestHTTPHLSRealVODGraphSupportsRandomAccessAndChecksCachedAuthorization(t *testing.T) {
	h := newHLSHTTPFixture(t)
	graph := h.graph(t, h.accounts.viewer, 6*media.TicksPerSecond)
	facts := map[int]map[string]hlsHTTPStreamFact{}
	var cached hlsHTTPResponse
	// Start in the middle, move to its neighbor, then seek backward to source zero.
	for _, number := range []int{2, 3, 0} {
		segment := h.request(t, http.MethodGet, graph.children[number], nil, nil)
		expectHLSHTTPStatus(t, segment, http.StatusOK)
		if segment.header.Get("Content-Type") != "video/mp2t" || len(segment.body) == 0 {
			t.Fatal("HLS segment has no MPEG-TS representation")
		}
		facts[number] = h.verifySegment(t, segment.body, number, graph.durations[number])
		if number == 2 {
			cached = segment
		}
	}
	backToMiddle := h.request(t, http.MethodGet, graph.children[2], nil, nil)
	expectHLSHTTPStatus(t, backToMiddle, http.StatusOK)
	for kind, current := range h.verifySegment(t, backToMiddle.body, 2, graph.durations[2]) {
		oldStart, _ := strconv.ParseFloat(facts[2][kind].Start, 64)
		newStart, _ := strconv.ParseFloat(current.Start, 64)
		if math.Abs(oldStart-newStart) > .05 {
			t.Errorf("revisiting segment 2 changed its %s timeline", kind)
		}
	}
	head := h.request(t, http.MethodHead, graph.children[2], nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	length, lengthErr := strconv.Atoi(head.header.Get("Content-Length"))
	if len(head.body) != 0 || lengthErr != nil || length <= 0 || head.header.Get("Content-Type") != "video/mp2t" || head.header.Get("ETag") == "" {
		t.Error("HLS HEAD did not preserve representation metadata without a body")
	}
	conditional := http.Header{"If-None-Match": {head.header.Get("ETag")}}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[2], nil, conditional), http.StatusNotModified)
	withoutToken := hlsHTTPWithoutToken(t, graph.children[2])
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, withoutToken, nil, conditional), http.StatusUnauthorized)
	bad := conditional.Clone()
	bad.Set("X-Emby-Token", "invalid-hls-token")
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, withoutToken, nil, bad), http.StatusUnauthorized)
	for _, account := range []clientSessionHTTPLogin{h.accounts.second, h.accounts.other, h.accounts.admin} {
		headers := account.headers.Clone()
		headers.Set("If-None-Match", cached.header.Get("ETag"))
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, withoutToken, nil, headers), http.StatusNotFound)
	}
	zero := h.graph(t, h.accounts.viewer, 0)
	if len(zero.children) != len(graph.children) {
		t.Error("clearing the initial seek hint changed the VOD timeline")
	}
	for _, query := range []string{"AudioChannels=2", "MaxStreamingBitrate=64000", "CopyTimestamps=true", "Container=mp4"} {
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, zero.masterURL+"&"+query, nil, nil), http.StatusBadRequest)
	}
	// A manually constructed master without GobyHlsId is also bound to the
	// authenticated playback session and goes through the query planner.
	manualQuery := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")},
		"PlaySessionId": {graph.playID}, "MediaSourceId": {media.SourceID(h.item.ID)}, "DeviceId": {h.accounts.viewer.deviceID},
		"VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "MaxWidth": {"96"}, "MaxHeight": {"54"}, "SegmentLength": {"3"},
		"AllowVideoStreamCopy": {"false"}, "StartTimeTicks": {"60000000"}}
	manual := h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+manualQuery.Encode(), nil, nil)
	expectHLSHTTPStatus(t, manual, http.StatusOK)
	children := hlsHTTPManifestChildren(manual.body)
	if len(children) != 1 {
		t.Fatal("manual HLS query did not produce a media playlist")
	}
	manualMain := hlsHTTPURL(t, children[0], h.accounts.viewer.headers.Get("X-Emby-Token"))
	if manualMain.Query().Get("StartTimeTicks") != "60000000" {
		t.Fatal("manual HLS request did not retain its explicit start hint")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, children[0], nil, nil), http.StatusOK)
	manualQuery.Del("StartTimeTicks")
	reset := h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+manualQuery.Encode(), nil, nil)
	expectHLSHTTPStatus(t, reset, http.StatusOK)
	resetChildren := hlsHTTPManifestChildren(reset.body)
	if len(resetChildren) != 1 {
		t.Fatal("manual HLS reset did not produce a media playlist")
	}
	resetMain := hlsHTTPURL(t, resetChildren[0], h.accounts.viewer.headers.Get("X-Emby-Token"))
	if resetMain.Query().Get("GobyHlsId") != manualMain.Query().Get("GobyHlsId") || resetMain.Query().Get("StartTimeTicks") != "0" {
		t.Fatal("omitting a manual start did not reset the reused revision to zero")
	}
	resetPlaylist := h.request(t, http.MethodGet, resetChildren[0], nil, nil)
	expectHLSHTTPStatus(t, resetPlaylist, http.StatusOK)
	if bytes.Contains(resetPlaylist.body, []byte("#EXT-X-START:")) {
		t.Error("a manual default-start request retained a prior seek hint")
	}
}

func TestHTTPHLSCancellationPolicySnapshotsStopAndLogoutInvalidateCachedChildren(t *testing.T) {
	h := newHLSHTTPFixture(t)
	graph := h.graph(t, h.accounts.viewer, 0)
	cached := h.request(t, http.MethodGet, graph.children[0], nil, nil)
	expectHLSHTTPStatus(t, cached, http.StatusOK)
	before := h.userDataSnapshot(t)
	stopPath := func(account clientSessionHTTPLogin, playID string, post bool) string {
		path := "/emby/Videos/ActiveEncodings"
		if post {
			path += "/Delete"
		}
		return path + "?" + url.Values{"DeviceId": {account.deviceID}, "PlaySessionId": {playID}}.Encode()
	}
	for _, account := range []clientSessionHTTPLogin{h.accounts.second, h.accounts.other} {
		expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stopPath(account, graph.playID, false), nil, account.headers), http.StatusNoContent)
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stopPath(h.accounts.viewer, "unknown-play-session", false), nil, h.accounts.viewer.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusOK)
	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		expectHLSHTTPStatus(t, h.request(t, method, stopPath(h.accounts.viewer, graph.playID, method == http.MethodPost), nil, h.accounts.viewer.headers), http.StatusNoContent)
		h.retired(t, graph)
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusNotFound)
		if h.userDataSnapshot(t) != before {
			t.Error("stopping HLS encoders changed the user's playback history")
		}
		graph = h.graph(t, h.accounts.viewer, 0)
		cached = h.request(t, http.MethodGet, graph.children[0], nil, nil)
		expectHLSHTTPStatus(t, cached, http.StatusOK)
	}
	h.policy(t, false)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil,
		http.Header{"If-None-Match": {cached.header.Get("ETag")}}), http.StatusForbidden)
	h.retired(t, graph)
	h.policy(t, true)
	graph = h.graph(t, h.accounts.viewer, 0)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusOK)
	start := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 0}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing", start, h.accounts.viewer.headers), http.StatusNoContent)
	stop := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 2 * media.TicksPerSecond}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", stop, h.accounts.viewer.headers), http.StatusNoContent)
	h.retired(t, graph)
	stoppedData := h.userDataSnapshot(t)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusNotFound)
	if h.userDataSnapshot(t) != stoppedData {
		t.Error("a stopped HLS child request changed user data after the stop report")
	}
	graph = h.graph(t, h.accounts.viewer, 0)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusOK)
	beforeLogout := h.userDataSnapshot(t)
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, h.accounts.viewer.headers), http.StatusOK)
	h.retired(t, graph)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusUnauthorized)
	if h.userDataSnapshot(t) != beforeLogout {
		t.Error("HLS logout cleanup changed user data")
	}
	h.accounts.viewer = loginClientSessionHTTP(t, h.f, "Session Viewer", "session-viewer-password", "hls-reopened-viewer")
	graph = h.graph(t, h.accounts.viewer, 0)
	cached = h.request(t, http.MethodGet, graph.children[0], nil, nil)
	expectHLSHTTPStatus(t, cached, http.StatusOK)
	// Even a conditional cache hit must recheck the indexed original snapshot.
	modified := time.Now().Add(-time.Hour)
	if err := os.Chtimes(h.path, modified, modified); err != nil {
		t.Fatal("change owned original HLS source snapshot")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, graph.children[0], nil,
		http.Header{"If-None-Match": {cached.header.Get("ETag")}}), http.StatusServiceUnavailable)
	h.retired(t, graph)
}
