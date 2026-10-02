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
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedAVSourceCommandBuffer struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
	err    error
}

func (buffer *hlsGeneratedAVSourceCommandBuffer) Len() int { return buffer.buffer.Len() }

func (buffer *hlsGeneratedAVSourceCommandBuffer) Write(data []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(data) > (64<<10)-buffer.Len() {
		buffer.err = transcode.ErrTimelineLimit
		buffer.cancel()
		return 0, buffer.err
	}
	return buffer.buffer.Write(data)
}

// Fixture generation shares the real media-helper governor. The exited leader
// remains unreaped until its owned group signal fence has completed; Wait then
// reaps that leader and joins stdout/stderr copiers before any receipt is used.
func hlsGeneratedAVSourceCommand(t *testing.T, parent context.Context, executable string, arguments ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env, command.Dir = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC"}, "/"
	if loader := os.Getenv("LD_LIBRARY_PATH"); loader != "" {
		command.Env = append(command.Env, "LD_LIBRARY_PATH="+loader)
	}
	stdout, stderr := &hlsGeneratedAVSourceCommandBuffer{cancel: cancel}, &hlsGeneratedAVSourceCommandBuffer{cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(ctx, command, func() error {
		var information [16]uint64
		var waitErr error
		for {
			_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(command.Process.Pid), uintptr(unsafe.Pointer(&information[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				waitErr = errno
			}
			break
		}
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if err := errors.Join(runErr, stdout.err, stderr.err, ctx.Err()); err != nil || stderr.Len() != 0 || stdout.Len() != 0 {
		// Private paths and command diagnostics are not emitted to the test log.
		t.Fatalf("controlled A1 source generation failed: error_type=%T stdout_bytes=%d stderr_bytes=%d", err, stdout.Len(), stderr.Len())
	}
}

// This creates an independent, actually scanned A/V source. It never edits the
// silent fMP4 fixture, source metadata, returned plan or negotiated ladder.
func newHLSGeneratedAVWindowDiagnosticFixture(t *testing.T) *hlsHTTPFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Fatal("actual A/V diagnostic requires the pinned media executables")
	}
	f, accounts := newClientSessionHTTPAccounts(t, 10*time.Minute)
	root := t.TempDir()
	name := filepath.Join(root, "Generated.AV.Window.Markers.mp4")
	hlsGeneratedAVSourceCommand(t, f.ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=48",
		"-f", "lavfi", "-i", "aevalsrc=0.15*sin(2*PI*(173*t+31*t*t))|0.11*sin(2*PI*(271*t+47*t*t)):s=48000:d=48",
		"-vf", "drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,6)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,12)',drawbox=x=0:y=0:w=iw:h=ih:color=yellow:t=fill:enable='gte(t,18)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-bf", "0", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-b:v", "512k", "-minrate", "512k", "-maxrate", "512k",
		"-bufsize", "1024k", "-x264-params", "nal-hrd=cbr:filler=1",
		"-c:a", "aac", "-profile:a", "aac_low", "-threads:a", "1", "-ar", "48000", "-ac", "2", "-b:a", "128k",
		"-video_track_timescale", "24000", "-movie_timescale", "48000", "-movflags", "+faststart", name)
	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}, []string{root})
	if err != nil {
		t.Fatal("create actual A/V diagnostic catalog")
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
		t.Fatal("create actual A/V diagnostic manager")
	}
	if runtime.generatedWindowsEnabled {
		t.Fatal("A/V diagnostic changed the default-disabled finite graph")
	}
	f.app.hls = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error("close actual A/V diagnostic runtime")
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Generated A/V Diagnostic Movies", "movies", []string{root})
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
		if candidate.Path == name && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil || len(item.Media.Streams) != 2 || !item.Media.FormatStartKnown || item.Media.FormatStartTicks != 0 ||
		item.Media.DurationTicks != 48*media.TicksPerSecond {
		t.Fatal("real A/V scan did not retain the exact source metadata and independent zero origin")
	}
	video, audio := item.Media.Streams[0], item.Media.Streams[1]
	if video.CodecType != "video" || video.Codec != "h264" || video.Width != 160 || video.Height != 96 ||
		audio.CodecType != "audio" || audio.Codec != "aac" || audio.SampleRate != 48000 || audio.Channels != 2 {
		t.Fatal("real A/V scan did not retain the selected ordinary AVC and native stereo AAC tracks")
	}
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, path: name, libraryID: collection.ID, item: item}
	h.policy(t, true)
	f.handler = f.app.Handler()
	h.server = httptest.NewServer(f.handler)
	t.Cleanup(h.server.Close)
	return h
}

func hlsGeneratedAVWindowDiagnosticPrepare(t *testing.T, h *hlsHTTPFixture) hlsGeneratedWindowHTTPGraph {
	t.Helper()
	body := map[string]any{
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "SubtitleStreamIndex": -1,
		"StartTimeTicks": 0, "MaxStreamingBitrate": 2_000_000, "AudioSampleRate": 48000, "AudioChannels": 2, "AudioBitrate": 128000,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac",
			"EnableAdaptiveBitrate": true, "MaxWidth": 160, "MaxHeight": 96, "SegmentLength": 6,
		}}},
	}
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var negotiation struct {
		PlayID       string           `json:"PlaySessionId"`
		MediaSources []map[string]any `json:"MediaSources"`
		ErrorCode    string           `json:"ErrorCode"`
	}
	if json.Unmarshal(response.body, &negotiation) != nil || negotiation.PlayID == "" || len(negotiation.MediaSources) != 1 || negotiation.ErrorCode != "" {
		t.Fatal("actual PlaybackInfo did not negotiate its permitted A/V output")
	}
	master, ok := negotiation.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || negotiation.MediaSources[0]["TranscodingContainer"] != "ts" || negotiation.MediaSources[0]["TranscodingSubProtocol"] != "hls" {
		t.Fatal("actual A/V negotiation did not retain the requested TS HLS protocol")
	}
	parsed := hlsHTTPURL(t, master, h.accounts.viewer.headers.Get("X-Emby-Token"))
	h.f.app.hls.mu.Lock()
	session := h.f.app.hls.sessions[parsed.Query().Get("GobyHlsId")]
	h.f.app.hls.mu.Unlock()
	if session == nil {
		t.Fatal("actual A/V negotiation lacks its immutable playback registration")
	}
	plan := session.key.plan
	if plan.Container != "ts" || plan.HLS.SegmentType != "mpegts" || plan.HLS.RenditionCount < 2 || plan.HLS.RenditionCount > transcode.MaxHLSRenditions ||
		plan.VideoCodec != "h264" || plan.VideoStreamIndex != 0 || plan.AudioCodec != "aac" || plan.AudioStreamIndex != 1 || plan.AudioSampleRate != 48000 || plan.AudioChannels != 2 ||
		plan.FrameRate != 24 || plan.SegmentSeconds != 6 || plan.DurationTicks != h.item.Media.DurationTicks || plan.StartTicks != 0 ||
		plan.HLS.Window != (transcode.HLSWindow{}) || plan.AudioSampleSeek || transcode.HasHLSSubtitles(plan) {
		t.Fatal("actual negotiated A/V plan is outside the first diagnostic observation domain")
	}
	masterResponse := h.request(t, http.MethodGet, master, nil, nil)
	expectHLSHTTPStatus(t, masterResponse, http.StatusOK)
	variants := hlsHTTPManifestChildren(masterResponse.body)
	if len(variants) != plan.HLS.RenditionCount {
		t.Fatal("A/V diagnostic removed a naturally negotiated rendition")
	}
	graph := hlsGeneratedWindowHTTPGraph{playID: negotiation.PlayID, masterURL: master, mainURLs: variants, session: session, owner: h.accounts.viewer}
	if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != 0 {
		t.Fatal("A/V negotiation or master observation created the baseline before the diagnostic")
	}
	return graph
}

// The manager retains its actual production admission and lifecycle contracts.
// Completed is reported as a terminal manager fact, never as A/V closure or as
// a receipt from the separate media-helper process governor.
func hlsGeneratedAVDiagnosticBaseline(h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph) func(context.Context, *os.File, transcode.Plan) (transcode.GeneratedAVDiagnosticBaseline, error) {
	return func(ctx context.Context, source *os.File, plan transcode.Plan) (baseline transcode.GeneratedAVDiagnosticBaseline, failure error) {
		defer func() {
			if failure != nil {
				for variant := range baseline.PlaylistHandles {
					if baseline.PlaylistHandles[variant] != nil {
						_ = baseline.PlaylistHandles[variant].Close()
					}
					for _, handle := range baseline.MediaHandles[variant] {
						if handle != nil {
							_ = handle.Close()
						}
					}
				}
				if baseline.Record.ID != "" {
					_ = h.f.app.hls.manager.CancelJob(baseline.Record.ID, graph.session.key.scope)
				}
			}
		}()
		if plan.HLS.Window.NativeClockVersion != 0 || plan.HLS.Window.RequireInputEvidence || plan.StartTicks != 0 ||
			plan.HLS.Window.EndTicks != 24*media.TicksPerSecond || plan.HLS.Window.StartNumber != 0 {
			return baseline, transcode.ErrInvalidPlan
		}
		original := plan
		original.StartTicks, original.HLS.Window = 0, transcode.HLSWindow{}
		if original != graph.session.key.plan {
			return baseline, transcode.ErrInvalidPlan
		}
		owned, err := transcode.DuplicateInput(source)
		if err != nil {
			return baseline, err
		}
		record, err := h.f.app.hls.manager.Ensure(ctx, transcode.Spec{Scope: graph.session.key.scope, SourceStamp: graph.session.key.stamp, Plan: plan}, owned)
		if err != nil {
			return baseline, err
		}
		baseline.Record = record
		for {
			terminal, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, record.ID)
			if err != nil {
				return baseline, err
			}
			if terminal.State == "completed" {
				baseline.Record = terminal
				break
			}
			if terminal.State != "queued" && terminal.State != "running" {
				return baseline, transcode.ErrJobFailed
			}
			select {
			case <-ctx.Done():
				return baseline, ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		clocks, ok := h.f.app.hls.manager.(hlsClockJobs)
		if !ok {
			return baseline, transcode.ErrUnsupportedTimeline
		}
		count := max(1, plan.HLS.RenditionCount)
		var mediaBytes int64
		for variant := 0; variant < count; variant++ {
			clock, err := clocks.HLSClock(ctx, graph.session.key.scope, record.ID, variant)
			if err != nil {
				return baseline, err
			}
			baseline.MuxClocks[variant] = clock
			playlist, err := h.f.app.hls.manager.Open(ctx, graph.session.key.scope, record.ID, transcode.HLSPlaylistName(variant, plan.HLS.RenditionCount))
			if err != nil {
				return baseline, err
			}
			baseline.PlaylistHandles[variant] = playlist
			data, err := io.ReadAll(io.LimitReader(playlist, transcode.MaxPlaylistBytes+1))
			if err != nil {
				return baseline, err
			}
			if len(data) > transcode.MaxPlaylistBytes {
				return baseline, transcode.ErrTimelineLimit
			}
			list, err := transcode.ParseMediaPlaylist(data)
			if err != nil {
				return baseline, err
			}
			if !list.Ended || list.Sequence != 0 || list.InitName != "" || len(list.Segments) == 0 || len(list.Segments) > 32 {
				return baseline, transcode.ErrInvalidTimeline
			}
			baseline.Playlists[variant] = list
			for _, segment := range list.Segments {
				handle, err := h.f.app.hls.manager.Open(ctx, graph.session.key.scope, record.ID, segment.Name)
				if err != nil {
					return baseline, err
				}
				baseline.MediaHandles[variant] = append(baseline.MediaHandles[variant], handle)
				stat, err := handle.Stat()
				if err != nil {
					return baseline, err
				}
				if stat.Size() <= 0 || stat.Size() > (64<<20)-mediaBytes {
					return baseline, transcode.ErrTimelineLimit
				}
				mediaBytes += stat.Size()
			}
		}
		if ctx.Err() != nil {
			return baseline, ctx.Err()
		}
		return baseline, nil
	}
}

func hlsGeneratedAVDiagnosticAcquireProbe(h *hlsRuntime) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) {
		select {
		case h.probes <- struct{}{}:
			var once sync.Once
			return func() {
				once.Do(func() { <-h.probes })
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.ctx.Done():
			return nil, transcode.ErrManagerClosed
		}
	}
}
