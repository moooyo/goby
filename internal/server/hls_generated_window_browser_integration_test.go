//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type generatedWindowBrowserJob struct {
	ID                   string
	State                string
	StartTicks, EndTicks int64
	StartNumber          int
	RequireInputEvidence bool
	Renditions           int
}

type generatedWindowBrowserObservation struct {
	Marker, PlayState                                        string
	PositionTicks, PlaybackRevision                          int64
	Paused, Closed, Pending, GraphPublished                  bool
	RuntimeRevision                                          int64
	RedirectPins, RedirectPinOwners, GlobalRedirectPinOwners int
	SourceDurationTicks, NativeDurationTicks                 int64
	Jobs                                                     []generatedWindowBrowserJob
}

type generatedWindowBrowserHTTP struct {
	Kind            string
	Variant, Number int
	Status          int
}

type generatedWindowBrowserObserver struct {
	h        *hlsHTTPFixture
	graph    hlsGeneratedWindowHTTPGraph
	mu       sync.Mutex
	trace    []generatedWindowBrowserHTTP
	overflow bool
}

// The observer stores only request roles and integer identities. In particular,
// playlist query strings, credentials and redirect locations never enter evidence.
func (observer *generatedWindowBrowserObserver) record(request *http.Request, status int) {
	base := filepath.Base(request.URL.Path)
	fact := generatedWindowBrowserHTTP{Variant: -1, Number: -1, Status: status}
	for _, pattern := range []struct {
		prefix, kind string
	}{{"window-segment-", "logical"}, {"segment-", "exact"}} {
		for variant := range observer.graph.mainURLs {
			prefix := fmt.Sprintf("v%d-%s", variant, pattern.prefix)
			if strings.HasPrefix(base, prefix) && strings.HasSuffix(base, ".ts") {
				number, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".ts"))
				if err == nil && number >= 0 && number < 17 {
					fact.Kind, fact.Variant, fact.Number = pattern.kind, variant, number
				}
			}
		}
	}
	if strings.HasSuffix(base, ".m3u8") {
		fact.Kind = "playlist"
	}
	if strings.HasPrefix(request.URL.Path, "/emby/Sessions/Playing") {
		fact.Kind = "report"
	}
	if fact.Kind == "" {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.trace) == 1024 {
		observer.overflow = true
		return
	}
	observer.trace = append(observer.trace, fact)
}

func (observer *generatedWindowBrowserObserver) snapshot(ctx context.Context) (generatedWindowBrowserObservation, error) {
	value := generatedWindowBrowserObservation{Marker: "goby-generated-window-browser-observation-v1",
		SourceDurationTicks: observer.h.item.Media.DurationTicks, NativeDurationTicks: 100 * media.TicksPerSecond,
		Jobs: make([]generatedWindowBrowserJob, 0)}
	if err := observer.h.f.pool.QueryRow(ctx, "SELECT state,position_ticks,playback_revision FROM play_sessions WHERE id=$1 AND auth_session_id=$2",
		observer.graph.playID, observer.graph.owner.id).Scan(&value.PlayState, &value.PositionTicks, &value.PlaybackRevision); err != nil {
		return value, errors.New("read generated browser playback state")
	}
	rows, err := observer.h.f.pool.Query(ctx, "SELECT id,state FROM encoding_jobs WHERE auth_session_id=$1 AND play_session_id=$2 ORDER BY created_at,id",
		observer.graph.owner.id, observer.graph.playID)
	if err != nil {
		return value, errors.New("read generated browser job identities")
	}
	defer rows.Close()
	for rows.Next() {
		var job generatedWindowBrowserJob
		if err := rows.Scan(&job.ID, &job.State); err != nil {
			return value, errors.New("read generated browser job row")
		}
		if len(value.Jobs) == 32 {
			return value, errors.New("generated browser job evidence limit")
		}
		// A stopped completed job can return its record and ErrJobCancelled.
		// The record is still the independently observed immutable actual plan.
		record, snapshotErr := observer.h.f.app.hls.manager.Snapshot(observer.graph.session.key.scope, job.ID)
		if record.ID != job.ID || snapshotErr != nil && record.ID == "" {
			return value, errors.New("generated browser job plan unavailable")
		}
		job.StartTicks, job.EndTicks = record.Spec.Plan.StartTicks, record.Spec.Plan.HLS.Window.EndTicks
		job.StartNumber = record.Spec.Plan.HLS.Window.StartNumber
		job.RequireInputEvidence = record.Spec.Plan.HLS.Window.RequireInputEvidence
		job.Renditions = record.Spec.Plan.HLS.RenditionCount
		value.Jobs = append(value.Jobs, job)
	}
	if rows.Err() != nil {
		return value, errors.New("finish generated browser job evidence")
	}
	observer.graph.session.mu.Lock()
	defer observer.graph.session.mu.Unlock()
	session := observer.graph.session
	value.Paused, value.Closed, value.Pending = session.demand.paused, session.closed, session.admission != nil
	value.RuntimeRevision = session.demand.revision
	if graph := session.windowGraph; graph != nil {
		value.GraphPublished = graph.published && !graph.fallback
		value.NativeDurationTicks = graph.endpoint.DurationTicks
		for _, binding := range graph.slots {
			if binding.redirectPin != nil {
				value.RedirectPins++
			}
			for _, pin := range binding.redirectPins {
				if pin != nil {
					value.RedirectPins++
				}
			}
		}
	}
	observer.h.f.app.hls.generatedWindowPinMu.Lock()
	value.GlobalRedirectPinOwners = len(observer.h.f.app.hls.generatedWindowPinOwners)
	for _, owner := range observer.h.f.app.hls.generatedWindowPinOwners {
		if owner == session {
			value.RedirectPinOwners++
		}
	}
	observer.h.f.app.hls.generatedWindowPinMu.Unlock()
	return value, nil
}

type generatedWindowBrowserResponse struct {
	http.ResponseWriter
	status int
}

func (writer *generatedWindowBrowserResponse) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
		writer.ResponseWriter.WriteHeader(status)
	}
}

func (writer *generatedWindowBrowserResponse) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *generatedWindowBrowserResponse) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

type generatedWindowBrowserFrame struct {
	Stage                           string
	MediaTime, At, PresentationTime float64
	PresentedFrames                 int64
	RGB                             []int
	Level, Variant, Width, Height   int
}

type generatedWindowBrowserLevel struct {
	Level, Variant, Width, Height int
	Bandwidth                     int64
}

type generatedWindowBrowserTransition struct {
	Stage                                                                             string
	StartedAt, Target, FragChangedAt                                                  float64
	QualitySettledAt, FragmentBarrierAt                                               float64
	QualityPausedAt, QualityPauseReportAt                                             float64
	QualityBufferedAt, QualitySeekAssignedAt, QualitySeekedAt, QualityUnpauseReportAt float64
	QualityBufferedNumber                                                             int
	BaselinePresentedFrames                                                           int64
	Level, Variant                                                                    int
}

type generatedWindowBrowserResult struct {
	Marker   string
	RunId    string
	Complete bool
	Checks   map[string]bool
	Levels   []generatedWindowBrowserLevel
	Frames   []generatedWindowBrowserFrame
	HTTP     []generatedWindowBrowserHTTP
	Reports  []struct {
		Stage         string
		Event         string
		PositionTicks int64
		Status        int
		At            float64
	}
	Observations []struct {
		Stage string
		generatedWindowBrowserObservation
	}
	Transitions                                   []generatedWindowBrowserTransition
	PageErrors, FatalErrors, ForeignRequests      int
	ClientStopLoadUsed                            bool
	ClientStopLoadPurpose                         string
	QualitySeekFlow                               string
	NoSeekQualityVerified, OriginalEmbyClientUsed bool
	SimultaneousQualitySeekVerified               bool
}

func generatedWindowBrowserReadResult(path string) (generatedWindowBrowserResult, error) {
	var result generatedWindowBrowserResult
	file, err := os.Open(path)
	if err != nil {
		return result, errors.New("open generated browser result")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > 1<<20 {
		return result, errors.New("generated browser result identity or budget")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return result, errors.New("read bounded generated browser result")
	}
	if json.Unmarshal(data, &result) != nil {
		return result, errors.New("decode generated browser result")
	}
	return result, nil
}

func generatedWindowBrowserAssertLevels(t *testing.T, result generatedWindowBrowserResult, plan transcode.Plan) []generatedWindowBrowserLevel {
	t.Helper()
	count := plan.HLS.RenditionCount
	if count < 2 || count > transcode.MaxHLSRenditions || len(result.Levels) != count || plan.AudioStreamIndex != -1 || plan.AudioBitrate != 0 {
		t.Fatal("generated browser level evidence differs from its silent negotiated ladder")
	}
	levels := make([]generatedWindowBrowserLevel, count)
	var seenLevels, seenVariants [transcode.MaxHLSRenditions]bool
	for _, level := range result.Levels {
		if level.Level < 0 || level.Level >= count || level.Variant < 0 || level.Variant >= count ||
			seenLevels[level.Level] || seenVariants[level.Variant] {
			t.Fatal("generated browser levels do not bijectively identify the negotiated renditions")
		}
		rendition := plan.HLS.Renditions[level.Variant]
		if level.Width != rendition.Width || level.Height != rendition.Height || level.Bandwidth != rendition.VideoBitrate*10/9 {
			t.Fatal("generated browser parsed level differs from its actual negotiated rendition")
		}
		seenLevels[level.Level], seenVariants[level.Variant] = true, true
		levels[level.Level] = level
	}
	return levels
}

func generatedWindowBrowserAssertFrames(t *testing.T, result generatedWindowBrowserResult, plan transcode.Plan) []generatedWindowBrowserLevel {
	t.Helper()
	levels := generatedWindowBrowserAssertLevels(t, result, plan)
	if len(result.Frames) > 256 {
		t.Fatal("generated browser frame evidence exceeded its budget")
	}
	for _, frame := range result.Frames {
		if frame.Level < 0 || frame.Level >= len(levels) || frame.Variant < 0 || frame.Variant >= plan.HLS.RenditionCount ||
			levels[frame.Level].Variant != frame.Variant || frame.Width != plan.HLS.Renditions[frame.Variant].Width ||
			frame.Height != plan.HLS.Renditions[frame.Variant].Height || math.IsNaN(frame.MediaTime) || math.IsInf(frame.MediaTime, 0) ||
			math.IsNaN(frame.At) || math.IsInf(frame.At, 0) || frame.At <= 0 || math.IsNaN(frame.PresentationTime) ||
			math.IsInf(frame.PresentationTime, 0) || frame.PresentationTime <= 0 || frame.PresentedFrames <= 0 || len(frame.RGB) != 3 {
			t.Fatal("generated browser presented dimensions outside its actual negotiated rendition")
		}
		for _, channel := range frame.RGB {
			if channel < 0 || channel > 255 {
				t.Fatal("generated browser frame contains an invalid observed channel")
			}
		}
	}
	for _, scenario := range []struct {
		stage            string
		start, end       float64
		channel, variant int
	}{{"initial90", 90, 90.8, 1, 0}, {"seek91", 91, 91.8, 1, 0}, {"quality91", 91, 91.8, 1, 1}, {"seek94", 94, 94.8, 2, 1}, {"backseek30", 30, 30.8, 0, 1}} {
		var frames []generatedWindowBrowserFrame
		for _, frame := range result.Frames {
			if frame.Stage == scenario.stage {
				frames = append(frames, frame)
			}
		}
		if len(frames) < 3 {
			t.Fatalf("generated browser did not preserve three fresh presented frames: stage=%s", scenario.stage)
		}
		for index, frame := range frames {
			if math.IsNaN(frame.MediaTime) || math.IsInf(frame.MediaTime, 0) || frame.MediaTime < scenario.start || frame.MediaTime > scenario.end ||
				scenario.stage == "quality91" && frame.MediaTime >= scenario.end ||
				math.IsNaN(frame.At) || math.IsInf(frame.At, 0) || frame.At <= 0 || frame.PresentedFrames <= 0 ||
				frame.Width <= 0 || frame.Height <= 0 || len(frame.RGB) != 3 || frame.Variant != scenario.variant {
				t.Fatalf("generated browser frame is outside the native source interval: stage=%s", scenario.stage)
			}
			for channel, color := range frame.RGB {
				if color < 0 || color > 255 || channel == scenario.channel && color < 170 || channel != scenario.channel && color > 60 {
					t.Fatalf("generated browser presented the wrong source color: stage=%s", scenario.stage)
				}
			}
			if index > 0 && (frame.MediaTime <= frames[index-1].MediaTime || frame.PresentedFrames <= frames[index-1].PresentedFrames || frame.At <= frames[index-1].At) {
				t.Fatalf("generated browser reused a presented frame: stage=%s", scenario.stage)
			}
		}
	}
	var before, after bool
	var previous *generatedWindowBrowserFrame
	for index := range result.Frames {
		frame := &result.Frames[index]
		if frame.Stage != "continuous" {
			continue
		}
		if frame.Variant != 1 || frame.MediaTime < 94 || frame.MediaTime > 97.8 || len(frame.RGB) != 3 || frame.RGB[2] < 170 || frame.RGB[0] > 60 || frame.RGB[1] > 60 ||
			previous != nil && (frame.MediaTime <= previous.MediaTime || frame.PresentedFrames <= previous.PresentedFrames || frame.At <= previous.At) {
			t.Fatal("generated browser continuous decode evidence is invalid")
		}
		before = before || frame.MediaTime < 96
		after = after || frame.MediaTime >= 96.5
		previous = frame
	}
	if !before || !after {
		t.Fatal("generated browser did not present fresh frames across the source window boundary")
	}
	return levels
}

func generatedWindowBrowserAssertPublicQualityResume(t *testing.T, result generatedWindowBrowserResult) {
	t.Helper()
	var quality, seek *generatedWindowBrowserTransition
	for index := range result.Transitions {
		transition := &result.Transitions[index]
		switch transition.Stage {
		case "quality91":
			if quality != nil {
				t.Fatal("generated browser duplicated its settled quality transition")
			}
			quality = transition
		case "seek94":
			if seek != nil {
				t.Fatal("generated browser duplicated its explicit seek transition")
			}
			seek = transition
		}
	}
	if quality == nil || seek == nil {
		t.Fatal("generated browser omitted its sequential quality and seek evidence")
	}
	for _, value := range []float64{quality.StartedAt, quality.FragChangedAt, quality.QualitySettledAt, quality.FragmentBarrierAt,
		quality.QualityPausedAt, quality.QualityPauseReportAt, quality.QualityBufferedAt, quality.QualitySeekAssignedAt,
		quality.QualitySeekedAt, quality.QualityUnpauseReportAt,
		seek.StartedAt, seek.FragChangedAt, seek.QualitySettledAt, seek.FragmentBarrierAt} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			t.Fatal("generated browser quality and seek barriers contain an invalid timestamp")
		}
	}
	if quality.Target != 91 || seek.Target != 94 || quality.Variant != 1 || seek.Variant != 1 || quality.Level != seek.Level ||
		quality.FragmentBarrierAt != quality.StartedAt || quality.FragChangedAt < quality.StartedAt || quality.QualitySettledAt < quality.FragChangedAt ||
		quality.QualityPausedAt < quality.StartedAt || quality.QualityPauseReportAt < quality.QualityPausedAt ||
		quality.QualityBufferedAt < quality.QualityPauseReportAt || quality.QualityBufferedNumber != 15 ||
		quality.QualitySeekAssignedAt < quality.QualityBufferedAt || quality.QualitySeekedAt < quality.QualitySeekAssignedAt ||
		quality.QualityUnpauseReportAt < quality.QualitySeekedAt || quality.QualitySettledAt < quality.QualityUnpauseReportAt ||
		seek.QualitySettledAt != quality.QualitySettledAt || seek.StartedAt < quality.QualitySettledAt ||
		seek.FragmentBarrierAt != quality.FragChangedAt || seek.FragChangedAt < seek.FragmentBarrierAt {
		t.Fatal("generated browser explicit seek did not follow the proved quality transition")
	}
	for _, frame := range result.Frames {
		switch frame.Stage {
		case "quality91":
			if frame.Level != quality.Level || frame.Variant != quality.Variant || quality.QualitySettledAt < frame.At ||
				quality.QualitySettledAt < frame.PresentationTime || frame.PresentationTime < quality.QualityUnpauseReportAt ||
				seek.BaselinePresentedFrames < frame.PresentedFrames {
				t.Fatal("generated browser quality settled before its fresh presented frames")
			}
		case "seek94":
			if frame.Level != seek.Level || frame.Variant != seek.Variant || frame.PresentedFrames <= seek.BaselinePresentedFrames ||
				frame.PresentationTime < seek.StartedAt || frame.PresentationTime < seek.QualitySettledAt || frame.PresentationTime < seek.FragChangedAt {
				t.Fatal("generated browser explicit seek reused a frame predating its quality or seek barrier")
			}
		}
	}
	var pauseReport, unpauseReport bool
	var pausePosition int64
	for _, report := range result.Reports {
		if report.Stage != "quality91" {
			continue
		}
		switch report.Event {
		case "Pause":
			if pauseReport || report.At != quality.QualityPauseReportAt || report.Status != http.StatusNoContent ||
				report.PositionTicks < 91*media.TicksPerSecond || report.PositionTicks >= 100*media.TicksPerSecond {
				t.Fatal("generated browser quality pause does not bind its standard playback report")
			}
			pauseReport = true
			pausePosition = report.PositionTicks
		case "Unpause":
			if !pauseReport || unpauseReport || report.At != quality.QualityUnpauseReportAt || report.Status != http.StatusNoContent ||
				report.PositionTicks != 91*media.TicksPerSecond {
				t.Fatal("generated browser quality resume does not follow its explicit native seek and standard unpause")
			}
			unpauseReport = true
		}
	}
	if !pauseReport || !unpauseReport {
		t.Fatal("generated browser public quality flow omitted standard pause or unpause")
	}
	var paused *generatedWindowBrowserObservation
	var resumed bool
	for index := range result.Observations {
		observation := &result.Observations[index]
		if observation.Stage == "quality-pause" {
			if paused != nil || observation.PlayState != "Paused" || !observation.Paused || observation.Closed || observation.Pending ||
				!observation.GraphPublished || observation.RuntimeRevision != observation.PlaybackRevision || observation.PositionTicks != pausePosition {
				t.Fatal("generated browser public quality pause lacks committed server demand")
			}
			paused = &observation.generatedWindowBrowserObservation
		}
		if observation.Stage == "quality-resume" {
			if paused == nil || resumed || observation.PlayState != "Playing" || observation.Paused || observation.Closed ||
				observation.RuntimeRevision != observation.PlaybackRevision || observation.PlaybackRevision <= paused.PlaybackRevision ||
				observation.PositionTicks != 91*media.TicksPerSecond {
				t.Fatal("generated browser public quality unpause lacks committed seek position")
			}
			resumed = true
		}
	}
	if paused == nil || !resumed {
		t.Fatal("generated browser public quality flow omitted independent demand observations")
	}
}

func TestHTTPGeneratedWindowChromiumHLSJSClient(t *testing.T) {
	if os.Getenv("GOBY_GENERATED_WINDOW_BROWSER_RUN_ID") == "" {
		t.Skip("GOBY_GENERATED_WINDOW_BROWSER_RUN_ID explicitly admits the owned browser acceptance")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" {
		t.Fatal("generated browser acceptance requires root, an owned integration database and actual media tools")
	}
	runID := os.Getenv("GOBY_GENERATED_WINDOW_BROWSER_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("generated browser acceptance requires a bounded run identity")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("generated browser evidence parent must be private")
	}
	cache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	bundlePath := refreshBrowserPath(t, "GOBY_PHASE2_HLS_BUNDLE", false)
	bundleInfo, err := os.Stat(bundlePath)
	if err != nil || bundleInfo.Size() <= 0 || bundleInfo.Size() > 8<<20 {
		t.Fatal("generated browser HLS engine exceeds its admission budget")
	}
	bundle, err := os.ReadFile(bundlePath)
	digest := sha256.Sum256(bundle)
	if err != nil || hex.EncodeToString(digest[:]) != phase2BrowserBundleSHA256 {
		t.Fatal("generated browser HLS engine differs from the pinned phase 2 bundle")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read generated browser source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate generated browser source root")
	}
	output, err := os.MkdirTemp(artifacts, "generated-window-browser-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create generated browser private evidence directory")
	}
	driver := map[string]any{"Marker": "goby-generated-window-browser-driver-v1", "RunId": runID, "Complete": false,
		"DefaultEnabled": false, "OriginalEmbyClientUsed": false, "RealChromiumAndHLSJS": true,
		"QualitySeekFlow": "public_pause_buffered_quality_seek_resume", "NoSeekQualityVerified": false, "SimultaneousQualitySeekVerified": false,
		"CredentialsWrittenToSummary": false, "HlsBundleSHA256": phase2BrowserBundleSHA256}
	defer func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve generated browser driver evidence")
		}
	}()
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	observer := &generatedWindowBrowserObserver{h: h, graph: graph}
	initial, err := observer.snapshot(h.f.ctx)
	if err != nil || len(initial.Jobs) != 1 || initial.Jobs[0].StartNumber != 15 || !initial.GraphPublished {
		t.Fatal("generated browser initial high seek did not preserve the independently closed window")
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "initial-server-evidence.json"), initial) != nil {
		t.Fatal("preserve generated browser initial source and plan evidence")
	}
	private := h.f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__generated-window-consumer":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>Owned generated window acceptance</title></head><body><video id="generated-video" muted playsinline width="160" height="96"></video><canvas id="generated-pixel" width="1" height="1"></canvas></body></html>`)
		case "/__generated-window-hls.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(bundle)
		case "/__generated-window-observe":
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			value, err := observer.snapshot(ctx)
			if err != nil {
				http.Error(w, "generated browser observation unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(value)
		default:
			http.NotFound(w, r)
		}
	})
	product := h.f.handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__generated-window-") {
			if r.Method != http.MethodGet {
				http.Error(w, "read-only browser fixture", http.StatusMethodNotAllowed)
				return
			}
			private(w, r)
			return
		}
		writer := &generatedWindowBrowserResponse{ResponseWriter: w}
		product.ServeHTTP(writer, r)
		if writer.status == 0 {
			writer.status = http.StatusOK
		}
		observer.record(r, writer.status)
	}))
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	h.server.Close()
	h.server = server
	headers := make(map[string]string)
	for name, values := range graph.owner.headers {
		if len(values) == 1 {
			headers[name] = values[0]
		}
	}
	contextPath := filepath.Join(output, "private-context.json")
	resultPath := filepath.Join(output, "browser-result.json")
	fixture := map[string]any{"Marker": "goby-generated-window-browser-fixture-v1", "RunId": runID,
		"BaseURL": server.URL, "Headers": headers, "MasterURL": graph.masterURL, "MainURLs": graph.mainURLs,
		"MediaURLs": graph.mediaURLs, "ItemId": h.item.ID, "PlaySessionId": graph.playID,
		"ExpectedRenditions": len(graph.mainURLs), "HlsBundleSHA256": phase2BrowserBundleSHA256,
		"Renditions":   graph.session.key.plan.HLS.Renditions[:len(graph.mainURLs)],
		"ArtifactsDir": output, "ResultPath": resultPath, "ObservePath": "/__generated-window-observe",
		"PagePath": "/__generated-window-consumer", "BundlePath": "/__generated-window-hls.js"}
	defer func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove generated browser private credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	}()
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write generated browser private context")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create generated browser private process directories")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + cache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright,
		"GOBY_GENERATED_WINDOW_BROWSER_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(h.f.ctx, 3*time.Minute)
	defer cancel()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{"--max-old-space-size=128", filepath.Join(sourceRoot, "scripts", "test-env", "generated-window-browser.mjs")}, environment, sourceRoot, output)
	driver["BrowserCommand"] = command
	result, resultErr := generatedWindowBrowserReadResult(resultPath)
	final, finalErr := observer.snapshot(h.f.ctx)
	if finalErr == nil {
		if refreshBrowserWriteJSON(filepath.Join(output, "final-server-evidence.json"), final) != nil {
			t.Error("preserve generated browser final server evidence")
		}
	}
	observer.mu.Lock()
	trace, overflow := append([]generatedWindowBrowserHTTP(nil), observer.trace...), observer.overflow
	observer.mu.Unlock()
	if refreshBrowserWriteJSON(filepath.Join(output, "server-http-evidence.json"), map[string]any{"Requests": trace, "Overflow": overflow}) != nil {
		t.Error("preserve generated browser independent HTTP evidence")
	}
	// Close the real manager before the fixture's registered cleanup, so browser
	// evidence includes resource retirement while the database is still available.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	closeErr := h.f.app.hls.Close(closeCtx)
	closeCancel()
	driver["RuntimeClosedBeforeFixtureCleanup"] = closeErr == nil
	h.f.app.hls.generatedWindowPinMu.Lock()
	remainingPinOwners := len(h.f.app.hls.generatedWindowPinOwners)
	h.f.app.hls.generatedWindowPinMu.Unlock()
	driver["RedirectPinOwnersAfterClose"] = remainingPinOwners
	if commandErr != nil || resultErr != nil || finalErr != nil || closeErr != nil {
		t.Fatalf("generated browser execution failed: command_error_type=%T result_error_type=%T observation_error_type=%T close_error_type=%T; inspect private artifacts",
			commandErr, resultErr, finalErr, closeErr)
	}
	checks := []string{"Initial90", "Seek91", "QualitySettled91", "Seek94", "ContinuousAcrossWindows", "ABRSwitch", "PauseColdAdmission", "PingRemainsPaused", "CachedPause", "BackSeek30", "StandardReports", "StoppedResources"}
	if result.Marker != "goby-generated-window-browser-result-v1" || result.RunId != runID || !result.Complete || len(result.Checks) != len(checks) ||
		result.PageErrors != 0 || result.FatalErrors != 0 || result.ForeignRequests != 0 || !result.ClientStopLoadUsed || result.ClientStopLoadPurpose != "client_request_reduction" ||
		result.QualitySeekFlow != "public_pause_buffered_quality_seek_resume" || result.NoSeekQualityVerified ||
		result.SimultaneousQualitySeekVerified || result.OriginalEmbyClientUsed || overflow {
		t.Fatal("generated browser result does not bind a complete real client scenario")
	}
	for _, check := range checks {
		if !result.Checks[check] {
			t.Fatalf("generated browser acceptance check incomplete: %s", check)
		}
	}
	levels := generatedWindowBrowserAssertFrames(t, result, graph.session.key.plan)
	generatedWindowBrowserAssertPublicQualityResume(t, result)
	seen := make(map[int]bool)
	for _, job := range final.Jobs {
		if !job.RequireInputEvidence || job.Renditions != len(graph.mainURLs) || job.StartTicks != int64(job.StartNumber)*6*media.TicksPerSecond ||
			job.EndTicks <= job.StartTicks || job.EndTicks > 100*media.TicksPerSecond || job.EndTicks-job.StartTicks > 6*media.TicksPerSecond ||
			job.StartNumber != 5 && job.StartNumber != 6 && job.StartNumber != 15 && job.StartNumber != 16 {
			t.Fatal("generated browser caused a full source producer or an unproved private window")
		}
		seen[job.StartNumber] = true
	}
	if !seen[15] || !seen[16] || !seen[5] || final.PlayState != "Stopped" || !final.Closed || final.Pending || final.RedirectPins != 0 ||
		final.RedirectPinOwners != 0 || final.GlobalRedirectPinOwners != 0 || remainingPinOwners != 0 {
		t.Fatal("generated browser did not independently establish both windows, back seek and stopped resource retirement")
	}
	var logical15, logical16, logical5, switchedLogical, switchedExact, coldPaused bool
	backSeekRequested := false
	for _, request := range trace {
		if request.Kind == "logical" && request.Status == http.StatusTemporaryRedirect {
			if request.Number == 5 {
				backSeekRequested = true
			}
			if !backSeekRequested && request.Number < 15 {
				t.Fatal("generated browser produced a prefix before its back seek")
			}
			logical15 = logical15 || request.Number == 15
			logical16 = logical16 || request.Number == 16
			logical5 = logical5 || request.Number == 5
			switchedLogical = switchedLogical || request.Variant == 1
		}
		switchedExact = switchedExact || request.Kind == "exact" && request.Variant == 1 && request.Status == http.StatusOK
		coldPaused = coldPaused || request.Kind == "logical" && request.Number == 0 && request.Status == http.StatusServiceUnavailable
	}
	if !logical15 || !logical16 || !logical5 || !switchedLogical || !switchedExact || !coldPaused {
		t.Fatal("generated browser presentation evidence lacks independent logical, exact, ABR or pause HTTP operations")
	}
	reports := make(map[string]bool)
	for _, report := range result.Reports {
		if report.Status != http.StatusNoContent || report.PositionTicks < 0 || report.PositionTicks >= 100*media.TicksPerSecond {
			t.Fatal("generated browser standard playback report evidence is invalid")
		}
		reports[report.Event] = true
	}
	for _, event := range []string{"Started", "TimeUpdate", "Pause", "Unpause", "Ping", "Stopped"} {
		if !reports[event] {
			t.Fatal("generated browser omitted a standard Emby playback report")
		}
	}
	var paused *generatedWindowBrowserObservation
	observedPauseStages := make(map[string]bool)
	for index := range result.Observations {
		observation := &result.Observations[index]
		switch observation.Stage {
		case "pause-before", "pause-after-cache", "pause-after-cold", "ping-after-cold":
			if observation.Marker != "goby-generated-window-browser-observation-v1" || observation.PlayState != "Paused" ||
				!observation.Paused || observation.Pending || observation.Closed || !observation.GraphPublished ||
				observation.NativeDurationTicks != 100*media.TicksPerSecond || observation.RuntimeRevision != observation.PlaybackRevision {
				t.Fatal("generated browser pause is not bound to committed server demand")
			}
			if observation.Stage == "pause-before" {
				paused = &observation.generatedWindowBrowserObservation
			}
			if paused == nil || len(observation.Jobs) != len(paused.Jobs) || observation.PlaybackRevision != paused.PlaybackRevision {
				t.Fatal("generated browser paused requests or Ping changed production or playback revision")
			}
			for jobIndex, job := range observation.Jobs {
				if job.ID != paused.Jobs[jobIndex].ID || job.StartNumber != paused.Jobs[jobIndex].StartNumber ||
					job.StartTicks != paused.Jobs[jobIndex].StartTicks || job.EndTicks != paused.Jobs[jobIndex].EndTicks {
					t.Fatal("generated browser paused request started another real job")
				}
			}
			observedPauseStages[observation.Stage] = true
		}
	}
	if len(observedPauseStages) != 4 {
		t.Fatal("generated browser omitted independently observed paused requests or Ping")
	}
	for _, transition := range result.Transitions {
		if transition.Level < 0 || transition.Level >= len(levels) || transition.Variant < 0 || transition.Variant >= len(levels) ||
			levels[transition.Level].Variant != transition.Variant || transition.Stage == "continuous" && transition.Variant != 1 {
			t.Fatal("generated browser transition does not identify its actual parsed level and rendition")
		}
	}
	for _, stage := range []string{"initial90", "seek91", "quality91", "seek94", "backseek30"} {
		expectedVariant := 1
		if stage == "initial90" || stage == "seek91" {
			expectedVariant = 0
		}
		var transitionFound bool
		for _, transition := range result.Transitions {
			if transition.Stage != stage {
				continue
			}
			transitionFound = true
			if transition.StartedAt <= 0 || transition.BaselinePresentedFrames < 0 || transition.Variant != expectedVariant ||
				stage == "quality91" && transition.FragChangedAt < transition.StartedAt {
				t.Fatal("generated browser omitted a fresh presentation or ABR transition barrier")
			}
			for _, frame := range result.Frames {
				if frame.Stage == stage && (frame.Level != transition.Level || frame.Variant != transition.Variant ||
					frame.PresentedFrames <= transition.BaselinePresentedFrames || frame.PresentationTime < transition.StartedAt ||
					(stage == "quality91" || stage == "seek94") && frame.PresentationTime < transition.FragChangedAt) {
					t.Fatal("generated browser presented a frame predating its seek or rendition change")
				}
			}
		}
		if !transitionFound {
			t.Fatal("generated browser omitted a presentation transition baseline")
		}
	}
	driver["Complete"], driver["Checks"], driver["NativeDurationTicks"], driver["SourceDurationTicks"] = true, result.Checks, final.NativeDurationTicks, final.SourceDurationTicks
	driver["QualitySeekFlow"], driver["SimultaneousQualitySeekVerified"] = result.QualitySeekFlow, result.SimultaneousQualitySeekVerified
	driver["NoSeekQualityVerified"], driver["OriginalEmbyClientUsed"] = result.NoSeekQualityVerified, result.OriginalEmbyClientUsed
	driver["ObservedJobCount"], driver["ObservedPresentedFrames"], driver["ObservedHTTPRequests"] = len(final.Jobs), len(result.Frames), len(trace)
	t.Log("generated_window_chromium_hlsjs_verified=true real_ts_abr=true quality_seek_flow=public_pause_buffered_quality_seek_resume no_seek_quality_verified=false simultaneous_quality_seek_verified=false original_emby_client_used=false global_seek_seconds=90,91,94,30 continuous_windows=15,16 standard_emby_reports=true default_enabled=false")
}
