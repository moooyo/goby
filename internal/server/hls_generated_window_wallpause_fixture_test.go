//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	"github.com/moooyo/goby/internal/transcode"
)

const (
	hlsGeneratedWindowWallPauseFixtureLifetime = 20 * time.Minute
	hlsGeneratedWindowWallPauseRetainedTTL     = 20 * time.Minute
)

// This fixture retains the real catalog, authentication and negotiated source
// facts of the ordinary generated-window HTTP fixture. Only its manager uses a
// paced FFmpeg executable, so source generation and independent decoding use
// the original GOBY_FFMPEG executable.
func newHLSGeneratedWindowWallPauseFixture(t *testing.T, mode string) (*hlsHTTPFixture, string) {
	t.Helper()
	maxRetainedJobs := 32
	switch mode {
	case "retained", "natural-ttl":
	case "retained-job-quota":
		maxRetainedJobs = 2
	default:
		t.Fatalf("unknown generated-window wall-pause fixture mode: %q", mode)
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for generated-window wall-pause verification")
	}
	wrapper, controlDirectory := hlsGeneratedWindowWallPauseFFmpegWrapper(t, ffmpeg)
	f, accounts := newClientSessionHTTPAccounts(t, hlsGeneratedWindowWallPauseFixtureLifetime)
	root := t.TempDir()
	path := filepath.Join(root, "Generated.Window.Color.Sequence.mp4")
	// Filler preserves the adaptive planner's real bitrate requirement. The
	// late green and blue intervals independently identify source [90,96).
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
		t.Fatalf("create real generated-window wall-pause catalog: error_type=%T", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() { f.app.notifier.Close() })
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath = ffmpeg, ffprobe
	f.app.cfg.MediaRoots = []string{root}
	f.app.cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: maxRetainedJobs,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	f.cfg = f.app.cfg
	initializeFixtureSettings(t, f)
	runtime, err := newHLSGeneratedWindowWallPauseRuntime(f.ctx, f.app, wrapper, mode)
	if err != nil {
		t.Fatalf("create real generated-window wall-pause runtime: error_type=%T", err)
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
			t.Errorf("close generated-window wall-pause runtime: error_type=%T", err)
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Generated Window Movies", "movies", []string{root})
	if err != nil {
		t.Fatalf("create wall-pause movie library: error_type=%T", err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("query wall-pause source library: error_type=%T", err)
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
		t.Fatalf("open wall-pause source endpoint input: error_type=%T", err)
	}
	certificate, certificateErr := transcode.MeasureGeneratedMP4SourceEndpoint(f.ctx, input, 0)
	closeErr := input.Close()
	if certificateErr != nil || closeErr != nil || certificate.SampleCount != 2400 || !certificate.DurationTicksExact ||
		certificate.DurationTicks != 100*media.TicksPerSecond || certificate.Origin != (transcode.GeneratedRational{Num: 2, Den: 1}) ||
		certificate.End != (transcode.GeneratedRational{Num: 102, Den: 1}) {
		t.Fatalf("real source did not produce its independent native sample-set endpoint: samples=%d duration_ticks=%d exact_duration=%t certificate_error_type=%T close_error_type=%T",
			certificate.SampleCount, certificate.DurationTicks, certificate.DurationTicksExact, certificateErr, closeErr)
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
	return h, controlDirectory
}

// A distinct authenticated owner negotiates a distinct scope through the real
// PlaybackInfo endpoint. Quota pressure must not reuse the paused viewer's
// existing play session by repeating negotiation with that same login.
func hlsWallPausePrepareOwner(t *testing.T, h *hlsHTTPFixture, start int64, owner clientSessionHTTPLogin) hlsGeneratedWindowHTTPGraph {
	t.Helper()
	if owner.id == "" || owner.userID == "" || owner.deviceID == "" || owner.headers.Get("X-Emby-Token") == "" {
		t.Fatal("wall-pause negotiation requires a complete authenticated owner")
	}
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
		t.Fatal("real PlaybackInfo did not negotiate one permitted wall-pause adaptive source")
	}
	masterURL, ok := negotiation.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || negotiation.MediaSources[0]["TranscodingContainer"] != "ts" || negotiation.MediaSources[0]["TranscodingSubProtocol"] != "hls" {
		t.Fatal("the wall-pause adaptive request changed the negotiated segment container")
	}
	parsed := hlsHTTPURL(t, masterURL, owner.headers.Get("X-Emby-Token"))
	h.f.app.hls.mu.Lock()
	session := h.f.app.hls.sessions[parsed.Query().Get("GobyHlsId")]
	h.f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("wall-pause PlaybackInfo did not establish its immutable HLS registration")
	}
	scope, plan := session.key.scope, session.key.plan
	if scope.UserID != owner.userID || scope.AuthSessionID != owner.id || scope.DeviceID != owner.deviceID ||
		scope.PlaySessionID != negotiation.PlayID || scope.ItemID != h.item.ID || scope.SourceID != media.SourceID(h.item.ID) ||
		scope.ApplicationKey || scope.ApplicationClientID != "" {
		t.Fatal("wall-pause PlaybackInfo did not bind every scope dimension to its authenticated owner")
	}
	if !transcode.GeneratedHLS(plan) || !transcode.GeneratedWindowClosureEligible(plan) || plan.HLS.SegmentType != "mpegts" ||
		plan.HLS.RenditionCount < 2 || plan.HLS.RenditionCount > transcode.MaxHLSRenditions || plan.VideoCodec != "h264" ||
		plan.AudioStreamIndex != -1 || plan.FrameRate != 24 || plan.HLS.Window != (transcode.HLSWindow{}) || plan.StartTicks != 0 ||
		(plan.Hardware.Decode != "" && plan.Hardware.Decode != "software") ||
		(plan.Hardware.Encode != "" && plan.Hardware.Encode != "software") || plan.Hardware.Device != "" {
		t.Fatal("wall-pause negotiated TS profile did not produce an eligible software encoded ladder")
	}
	// Eligibility is checked on a private candidate, without changing the
	// negotiated plan or claiming that an emitted window has been proved.
	candidate := plan
	candidate.HLS.Window = transcode.HLSWindow{EndTicks: min(int64(plan.SegmentSeconds)*media.TicksPerSecond, plan.DurationTicks),
		RequireInputEvidence: true, NativeClockVersion: transcode.GeneratedWindowNativeClockV1}
	if !transcode.GeneratedWindowNativeClockEligible(candidate) {
		t.Fatal("wall-pause negotiated software TS plan is not eligible for the native clock contract")
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != plan.HLS.RenditionCount {
		t.Fatal("real wall-pause generated master did not advertise every negotiated TS rendition")
	}
	for index, variant := range variants {
		child := hlsHTTPURL(t, variant, owner.headers.Get("X-Emby-Token"))
		if !strings.HasSuffix(child.Path, "/v"+strconv.Itoa(index)+".m3u8") || child.Query().Get("GobyHlsId") != session.id {
			t.Fatal("wall-pause negotiated variants lost their distinct output identities")
		}
	}
	return hlsGeneratedWindowHTTPGraph{playID: negotiation.PlayID, masterURL: masterURL, mainURLs: variants,
		mediaURLs: make([][]string, len(variants)), manifests: make([][]byte, len(variants)), session: session, owner: owner}
}

// The PID file names the manager job through its cache-directory basename.
// exec keeps that PID attached to the real FFmpeg process; -readrate paces real
// input without introducing a test process that waits on a release barrier.
func hlsGeneratedWindowWallPauseFFmpegWrapper(t *testing.T, ffmpeg string) (string, string) {
	t.Helper()
	executable, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal("resolve the real wall-pause FFmpeg executable")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal("resolve the absolute wall-pause FFmpeg executable")
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal("resolve wall-pause FFmpeg executable symlinks")
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	controlDirectory := t.TempDir()
	wrapper := filepath.Join(controlDirectory, "paced-ffmpeg")
	program := "#!/bin/sh\nset -eu\ndirectory=\"$(pwd -P)\"\njob=\"${directory##*/}\"\nprintf '%s\\n' \"$$\" > " +
		quote(controlDirectory) + "/\"$job.pid\"\nexec " + quote(executable) + " -readrate 1 \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0o700); err != nil {
		t.Fatal("write the paced wall-pause FFmpeg executable")
	}
	return wrapper, controlDirectory
}

// The runtime has its production authorization and lifecycle callbacks. Its
// test-only manager options extend retention only in the explicit retained
// cases; natural-ttl leaves IdleTimeout zero for the production default.
func newHLSGeneratedWindowWallPauseRuntime(ctx context.Context, server *Server, wrapper, mode string) (*hlsRuntime, error) {
	if !server.cfg.Transcoding.Enabled {
		return nil, errors.New("wall-pause fixture requires transcoding")
	}
	if _, err := exec.LookPath(server.cfg.FFmpegPath); err != nil {
		return nil, errors.New("the configured FFmpeg executable is unavailable")
	}
	if _, err := exec.LookPath(server.cfg.FFprobePath); err != nil {
		return nil, errors.New("the configured FFprobe executable is unavailable")
	}
	if _, err := exec.LookPath(wrapper); err != nil {
		return nil, errors.New("the wall-pause FFmpeg wrapper is unavailable")
	}
	managerOptions := server.cfg.Transcoding.ManagerOptions(wrapper, transcode.NewRepository(server.db))
	switch mode {
	case "retained", "retained-job-quota":
		managerOptions.IdleTimeout = hlsGeneratedWindowWallPauseRetainedTTL
	case "natural-ttl":
	default:
		return nil, errors.New("unknown generated-window wall-pause retention mode")
	}
	managerOptions.ValidateHardware = func(ctx context.Context, plan transcode.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !server.plannedHardwareAvailable(plan) {
			return errHLSRequestUnsupported
		}
		return ctx.Err()
	}
	managerOptions.SubtitleSource = server.readBurnSubtitleAsset
	managerOptions.LivePublish = server.publishDynamicSegment
	managerOptions.LiveSubtitle = server.receiveDynamicSubtitles
	managerOptions.LiveCaption = server.receiveDynamicCaption
	manager, err := transcode.NewManager(ctx, managerOptions)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	runtime := &hlsRuntime{server: server, manager: manager, ctx: lifetime, cancel: cancel, sessions: make(map[string]*hlsSession),
		byKey: make(map[hlsKey]*hlsSession), byScope: make(map[transcode.Scope]map[string]*hlsSession),
		done: make(chan struct{}), probes: make(chan struct{}, 2), slots: make(chan struct{}, 32)}
	runtime.verify = server.authorizeHLS
	runtime.workers.Add(1)
	go runtime.maintain()
	return runtime, nil
}
