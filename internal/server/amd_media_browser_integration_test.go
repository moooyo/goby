//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const amdMediaBrowserTitle = "native browser plays Goby global copy timestamps and AV1 output"

// This private manifest is the only artifact containing an authentication token.
// The page is an explicitly identified native-media harness, not Emby Web.
type amdMediaBrowserFixture struct {
	Marker        string
	RunID         string `json:"RunId"`
	BaseURL       string
	Token         string
	UserID        string `json:"UserId"`
	SessionID     string `json:"SessionId"`
	DeviceID      string `json:"DeviceId"`
	ItemID        string `json:"ItemId"`
	SourceID      string `json:"SourceId"`
	DurationTicks int64
	ArtifactsDir  string
	ResultPath    string
}

type amdMediaBrowserCase struct {
	Name           string
	PlayID         string `json:"PlayId"`
	RequestedTicks int64
	AlignedTicks   int64
	CopyTimestamps bool
	VideoCodec     string
	FirstFrameTime float64
	FirstClockTime float64
	StoppedTicks   int64
	Startup        amdMediaBrowserStartup
	Checks         map[string]bool
}

type amdMediaBrowserClockFrame struct {
	FrameNumber    int
	AtMilliseconds float64
	FrameTime      float64
	MediaTime      float64
	ReadyState     int
	Paused         bool
	Seeking        bool
}

type amdMediaBrowserStartupReport struct {
	MediaTime          float64
	PresentedFrameTime float64
	SourceTime         float64
	DisplayedTime      float64
	PositionTicks      int64
}

type amdMediaBrowserStartup struct {
	Frames           []amdMediaBrowserClockFrame
	Events           json.RawMessage
	StableFrames     []amdMediaBrowserClockFrame
	ClockReady       bool
	WaitMilliseconds float64
	Report           *amdMediaBrowserStartupReport
	Observation      *amdMediaBrowserObservation
}

type amdMediaBrowserResult struct {
	Marker           string
	RunID            string `json:"RunId"`
	HarnessComplete  bool
	PlayerKind       string
	OriginalEmbyWeb  bool
	PageErrors       int
	ExternalRequests int
	Cases            []amdMediaBrowserCase
	AV1              struct {
		Status string
		Case   *amdMediaBrowserCase
	}
	Capabilities json.RawMessage
}

type amdMediaBrowserObservation struct {
	State             string
	PositionTicks     int64
	UserPositionTicks int64
	Sessions          int
	ActiveJobs        int
	StreamSlots       int
	Plans             []amdMediaBrowserPlan
}

type amdMediaBrowserPlan struct {
	StartTicks       int64
	CopyTimestamps   bool
	VideoCodec       string
	AudioCodec       string
	VideoCopyCodec   string
	HasCopySeekProof bool
	ProducerStates   []string
}

type amdMediaBrowserObserver struct {
	fixture *hlsHTTPFixture
	mu      sync.Mutex
	seen    map[string][]*hlsSession
}

func amdMediaBrowserReadPlay(ctx context.Context, fixture *hlsHTTPFixture, id string) (library.PlaySession, error) {
	play := library.PlaySession{ID: id}
	// The public preparation accessor intentionally rejects terminal plays.
	// This scoped, read-only observer must also verify the persisted stop state.
	err := fixture.f.pool.QueryRow(ctx, `SELECT state,position_ticks,item_id,media_source_id FROM play_sessions
		WHERE id=$1 AND user_id=$2 AND auth_session_id=$3 AND device_id=$4`, id,
		fixture.accounts.viewer.userID, fixture.accounts.viewer.id, fixture.accounts.viewer.deviceID).
		Scan(&play.State, &play.PositionTicks, &play.ItemID, &play.MediaSourceID)
	return play, err
}

func amdMediaBrowserVerifyStartup(t *testing.T, tested amdMediaBrowserCase) {
	t.Helper()
	startup := tested.Startup
	if !startup.ClockReady || startup.WaitMilliseconds < 0 || startup.WaitMilliseconds > 3000 ||
		len(startup.Frames) == 0 || len(startup.Frames) > 32 || len(startup.StableFrames) != 3 ||
		startup.Frames[0].FrameTime != tested.FirstFrameTime || startup.Frames[0].MediaTime != tested.FirstClockTime ||
		tested.FirstClockTime < 0 || startup.Report == nil || startup.Observation == nil {
		t.Fatal("native browser startup lacks its preserved first callback and bounded clock convergence")
	}
	for index, frame := range startup.StableFrames {
		if frame.FrameNumber <= 0 || frame.ReadyState < 2 || frame.Paused || frame.Seeking || frame.MediaTime < 0 || frame.FrameTime < 0 ||
			math.Abs(frame.MediaTime-frame.FrameTime) > .1 {
			t.Fatal("native browser accepted an uninitialized or divergent startup clock")
		}
		if index > 0 {
			previous := startup.StableFrames[index-1]
			if frame.FrameNumber != previous.FrameNumber+1 || frame.FrameTime <= previous.FrameTime || frame.MediaTime <= previous.MediaTime ||
				frame.AtMilliseconds <= previous.AtMilliseconds {
				t.Fatal("native browser startup did not observe three advancing frame and element clocks")
			}
		}
	}
	report, observed := startup.Report, startup.Observation
	offset := float64(tested.RequestedTicks) / 1e7
	if tested.CopyTimestamps {
		offset = 0
	}
	if report.MediaTime < startup.StableFrames[2].MediaTime || math.Abs(report.MediaTime-report.PresentedFrameTime) > .15 ||
		report.MediaTime < tested.FirstFrameTime-.1 ||
		math.Abs(report.SourceTime-report.MediaTime-offset) > 1e-6 || math.Abs(report.DisplayedTime-report.SourceTime) > .003 ||
		report.PositionTicks <= 0 || report.PositionTicks != int64(math.Round(report.SourceTime*1e7)) ||
		observed.State != "Playing" || observed.PositionTicks != report.PositionTicks || observed.UserPositionTicks != report.PositionTicks {
		t.Fatal("Started did not persist the same settled clock used by the browser and displayed progress")
	}
}

// Observation cannot prepare, seek, stop, or mutate playback. Browser reports
// go through the actual Goby routes; this endpoint independently reads their
// persisted state and the actual conversion plans before resource retirement.
func (o *amdMediaBrowserObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("X-Emby-Token") != o.fixture.accounts.viewer.headers.Get("X-Emby-Token") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id := r.URL.Query().Get("PlaySessionId")
	play, err := amdMediaBrowserReadPlay(r.Context(), o.fixture, id)
	if err != nil || play.ItemID != o.fixture.item.ID {
		http.Error(w, "unknown owned playback", http.StatusNotFound)
		return
	}
	data, err := o.fixture.f.app.library.GetUserData(r.Context(), o.fixture.accounts.viewer.userID, play.ItemID)
	if err != nil {
		http.Error(w, "state observation failed", http.StatusInternalServerError)
		return
	}
	sessions := videoHTTPSessions(o.fixture, id)
	result := amdMediaBrowserObservation{State: play.State, PositionTicks: play.PositionTicks,
		UserPositionTicks: data.PlaybackPositionTicks, Sessions: len(sessions), StreamSlots: len(o.fixture.f.app.streamSlots),
		Plans: []amdMediaBrowserPlan{}}
	if err := o.fixture.f.pool.QueryRow(r.Context(), `SELECT count(*) FROM encoding_jobs
		WHERE auth_session_id=$1 AND play_session_id=$2 AND state IN ('queued','running')`,
		o.fixture.accounts.viewer.id, id).Scan(&result.ActiveJobs); err != nil {
		http.Error(w, "job observation failed", http.StatusInternalServerError)
		return
	}
	for _, session := range sessions {
		plan := session.key.plan
		fact := amdMediaBrowserPlan{StartTicks: plan.StartTicks, CopyTimestamps: plan.CopyTimestamps,
			VideoCodec: plan.VideoCodec, AudioCodec: plan.AudioCodec, VideoCopyCodec: plan.VideoCopyCodec,
			HasCopySeekProof: plan.VideoCopySeekCandidate != "", ProducerStates: []string{}}
		session.mu.Lock()
		producers := append([]hlsProducer(nil), session.producers...)
		session.mu.Unlock()
		for _, producer := range producers {
			record, err := o.fixture.f.app.hls.manager.Snapshot(session.key.scope, producer.id)
			if err != nil {
				http.Error(w, "producer observation failed", http.StatusInternalServerError)
				return
			}
			fact.ProducerStates = append(fact.ProducerStates, record.State)
		}
		result.Plans = append(result.Plans, fact)
	}
	o.mu.Lock()
	if len(sessions) > 0 {
		o.seen[id] = append([]*hlsSession(nil), sessions...)
	}
	o.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}

func newAMDMediaBrowserFixture(t *testing.T) *hlsHTTPFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Fatal("native browser admission requires actual FFmpeg and ffprobe")
	}
	f := newServerFixtureWithTimeout(t, 6*time.Minute)
	f.bootstrap(t)
	password := featureWavePassword(t)
	viewer, err := f.users.CreateUser(f.ctx, "Native media browser viewer", password, false)
	if err != nil {
		t.Fatal("create private browser viewer")
	}
	login := loginClientSessionHTTP(t, f, viewer.Name, password, "native-media-browser")
	root := t.TempDir()
	source := filepath.Join(root, "Native.Browser.Clock.mp4")
	// The duration exceeds the real resume threshold. No synthetic duration is
	// injected into the catalog; every indexed frame belongs to actual media.
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=132",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=132",
		"-vf", "drawbox=color=green:t=fill:enable='gte(t,3)*lt(t,6)',drawbox=color=blue:t=fill:enable='gte(t,6)*lt(t,9)',drawbox=color=yellow:t=fill:enable='gte(t,9)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "132", source)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("close initial application before actual browser scanner setup")
	}
	cfg := f.cfg
	cfg.FFmpegPath, cfg.FFprobePath, cfg.MediaRoots = ffmpeg, ffprobe, []string{root}
	cfg.Transcoding = config.TranscodingConfig{
		Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2,
	}
	app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "native-media-browser")
	if err != nil {
		t.Fatal("start actual browser media application")
	}
	f.app, f.cfg, f.handler = app, cfg, app.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error("close actual browser media application")
		}
	})
	collection, err := app.library.CreateLibrary(f.ctx, "Native browser media", "movies", []string{root})
	if err != nil {
		t.Fatal("create owned browser catalog")
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := app.library.QueryItems(f.ctx, library.Query{UserID: viewer.ID, ParentID: collection.ID, Recursive: true, Limit: 20})
	if err != nil {
		t.Fatal("read actual browser catalog")
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == source {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion ||
		math.Abs(float64(item.Media.DurationTicks)/float64(media.TicksPerSecond)-132) > .1 ||
		len(item.Media.VideoSeekIndexes) != 1 || media.ValidateVideoSeekIndex(item.Media.VideoSeekIndexes[0]) != nil || len(item.Media.VideoSeekIndexes[0].Entries) < 4 {
		t.Fatal("actual browser source lacks its real duration and trusted restart index")
	}
	fixture := &hlsHTTPFixture{f: f, accounts: clientSessionHTTPAccounts{viewer: login},
		ffmpeg: ffmpeg, ffprobe: ffprobe, path: source, libraryID: collection.ID, item: item}
	fixture.policy(t, true)
	return fixture
}

func TestAMDMediaNativeBrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("native media browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	cli := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_CLI", false)
	work := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_WORK", true)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	runID := os.Getenv("GOBY_AMD_MEDIA_BROWSER_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit native media browser run ID is required")
	}
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("native media browser artifact parent must be private")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("locate native media browser source")
	}
	sourceRoot := filepath.Clean(filepath.Join(cwd, "..", ".."))
	relative, err := filepath.Rel(sourceRoot, work)
	if err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("browser work must be outside the frozen Go source")
	}
	for _, name := range []string{"playwright.config.ts", "e2e/amd-media-playback.spec.ts"} {
		original, readErr := os.ReadFile(filepath.Join(sourceRoot, "web", "admin", name))
		copied, copyErr := os.ReadFile(filepath.Join(work, name))
		if readErr != nil || copyErr != nil || sha256.Sum256(original) != sha256.Sum256(copied) {
			t.Fatal("browser work does not match its current source files")
		}
	}
	output, err := os.MkdirTemp(artifacts, "amd-native-browser-")
	if err != nil || os.Chmod(output, 0700) != nil {
		t.Fatal("create private native media browser artifacts")
	}
	driver := map[string]any{"Marker": "goby-amd-native-browser-driver-v1", "RunId": runID, "Complete": false,
		"PlayerKind": "native-html-media-element-harness", "OriginalEmbyWebExercised": false, "AMDHardwareExercised": false,
		"HEVCBrowserPlaybackVerified": false, "CredentialsWrittenToSummary": false,
		"FullLengthPlaybackVerified": false, "AudioSynchronizationScope": "media-element-clock-only"}
	t.Cleanup(func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve native media browser driver result")
		}
	})
	fixture := newAMDMediaBrowserFixture(t)
	before, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal("read owned browser source digest")
	}
	hash := sha256.Sum256(before)
	driver["SourceSHA256"], driver["SourceBytes"] = hex.EncodeToString(hash[:]), len(before)
	observer := &amdMediaBrowserObserver{fixture: fixture, seen: make(map[string][]*hlsSession)}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve isolated native browser listener")
	}
	actual := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__amd-media-player":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Goby native media acceptance harness</title><h1>Native HTMLMediaElement acceptance</h1><p>This fixture is not Emby Web and does not claim AMD hardware decoding.</p><video id="media" width="640" height="360" playsinline></video><p>Source time: <output id="position">0.000</output></p><button id="pause">Pause</button><button id="resume">Resume</button><label>Seek seconds <input id="seek" type="number" step="0.01"></label><button id="change">Change stream</button><button id="stop">Stop</button><canvas id="pixels" width="1" height="1" hidden></canvas></html>`))
		case "/__amd-media-observe":
			observer.ServeHTTP(w, r)
		default:
			fixture.f.handler.ServeHTTP(w, r)
		}
	}))
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	fixture.server = actual
	t.Cleanup(actual.Close)
	manifest := amdMediaBrowserFixture{Marker: "goby-amd-native-browser-fixture-v1", RunID: runID, BaseURL: actual.URL,
		Token: fixture.accounts.viewer.headers.Get("X-Emby-Token"), UserID: fixture.accounts.viewer.userID,
		SessionID: fixture.accounts.viewer.id, DeviceID: fixture.accounts.viewer.deviceID, ItemID: fixture.item.ID,
		SourceID: media.SourceID(fixture.item.ID), DurationTicks: fixture.item.Media.DurationTicks,
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	manifestPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove native media browser token manifest")
		}
	})
	if refreshBrowserWriteJSON(manifestPath, manifest) != nil {
		t.Fatal("preserve private native media browser context")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0700) != nil {
			t.Fatal("create private native browser runtime directory")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_SMOKE_BASE_URL=" + actual.URL,
		"GOBY_AMD_MEDIA_BROWSER_RUN_ID=" + runID, "GOBY_AMD_MEDIA_BROWSER_CONTEXT=" + manifestPath}
	process, err := refreshBrowserCommand(fixture.f.ctx, node,
		[]string{cli, "test", "e2e/amd-media-playback.spec.ts", "--grep", "(?:^| )" + regexp.QuoteMeta(amdMediaBrowserTitle) + "$", "--project=chromium", "--workers=1", "--retries=0", "--output", filepath.Join(output, "playwright")},
		environment, work, output)
	driver["BrowserProcess"] = process
	if err != nil {
		t.Fatal("native media browser command failed; inspect the private bounded artifacts")
	}
	var result amdMediaBrowserResult
	if featureWavePrivateJSON(manifest.ResultPath, 1<<20, &result) != nil || result.Marker != "goby-amd-native-browser-result-v1" ||
		result.RunID != runID || !result.HarnessComplete || result.PlayerKind != "native-html-media-element-harness" || result.OriginalEmbyWeb ||
		result.PageErrors != 0 || result.ExternalRequests != 0 || len(result.Cases) != 3 {
		t.Fatal("native browser did not complete its explicitly bounded real-player scope")
	}
	observer.mu.Lock()
	seen := make(map[string][]*hlsSession, len(observer.seen))
	for id, sessions := range observer.seen {
		seen[id] = append([]*hlsSession(nil), sessions...)
	}
	observer.mu.Unlock()
	cases := append([]amdMediaBrowserCase(nil), result.Cases...)
	if result.AV1.Status == "played" && result.AV1.Case != nil {
		cases = append(cases, *result.AV1.Case)
	} else if result.AV1.Status != "platform-unsupported" || result.AV1.Case != nil {
		t.Fatal("AV1 must be actually played or explicitly reported as platform unsupported")
	}
	for index, tested := range cases {
		if len(seen[tested.PlayID]) != 1 || tested.StoppedTicks <= 0 {
			t.Fatal("browser case lacks actual conversion and stop observations")
		}
		amdMediaBrowserVerifyStartup(t, tested)
		plan := seen[tested.PlayID][0].key.plan
		if index < 3 {
			requested := []int64{63_700_000, 34_100_000, 72_500_000}[index]
			aligned := []int64{60_000_000, 30_000_000, 60_000_000}[index]
			if tested.RequestedTicks != requested || tested.AlignedTicks != aligned || !tested.CopyTimestamps || tested.VideoCodec != "copy" ||
				plan.VideoCodec != "copy" || plan.VideoCopyCodec != "h264" || plan.StartTicks != aligned || !plan.CopyTimestamps || plan.VideoCopySeekCandidate == "" ||
				math.Abs(tested.FirstFrameTime-float64(aligned)/1e7) > .1 {
				t.Fatal("copied browser seek did not preserve the independent source-global clock")
			}
		} else if plan.VideoCodec != "av1" || plan.CopyTimestamps || tested.VideoCodec != "av1" || tested.CopyTimestamps || plan.StartTicks != 42_500_000 || math.Abs(tested.FirstFrameTime) > .1 {
			t.Fatal("AV1 browser playback did not use the actual requested encoder and rebased clock")
		}
		for _, name := range []string{"DecodedFrames", "SourceColor", "StartupClockSettled", "StartedPositionPersisted", "ClockMapping", "DisplayedPosition", "PersistedProgress", "PauseStable", "ResumeAdvanced", "StopRetired", "ResumePositionPersisted"} {
			if !tested.Checks[name] {
				t.Fatalf("native browser case did not establish %s", name)
			}
		}
		play, err := amdMediaBrowserReadPlay(fixture.f.ctx, fixture, tested.PlayID)
		if err != nil || play.State != "Stopped" || play.PositionTicks != tested.StoppedTicks {
			t.Fatal("actual persisted playback stop disagrees with the browser report")
		}
		videoHTTPWaitRetired(t, fixture, tested.PlayID, seen[tested.PlayID])
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || sha256.Sum256(after) != hash {
		t.Fatal("native browser workflow changed owned source media")
	}
	driver["Complete"] = result.AV1.Status == "played"
	driver["H264BrowserPlaybackVerified"], driver["SourceUnchanged"], driver["StoppedResourcesRetired"] = true, true, true
	driver["AV1BrowserPlaybackVerified"], driver["AV1Status"] = result.AV1.Status == "played", result.AV1.Status
	driver["BrowserCases"], driver["Capabilities"] = cases, result.Capabilities
	t.Logf("native_html_media_verified=true copied_seeks=3 av1_browser_status=%s original_emby_web=false amd_hardware=false", result.AV1.Status)
}
