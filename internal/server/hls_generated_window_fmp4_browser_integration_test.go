//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type generatedFMP4BrowserHTTP struct {
	Kind, Resource, ProducerRef string
	Number, Status              int
}

type generatedFMP4BrowserJob struct {
	generatedWindowBrowserJob
	Container, SegmentType string
	NativeClockVersion     uint8
}

type generatedFMP4BrowserObservation struct {
	generatedWindowBrowserObservation
	Jobs                                                              []generatedFMP4BrowserJob
	InitializationPins, ReservedMetadataPages, AllocatedMetadataBytes int
}

type generatedFMP4BrowserObserver struct {
	base     generatedWindowBrowserObserver
	mu       sync.Mutex
	trace    []generatedFMP4BrowserHTTP
	overflow bool
	sources  []*os.File
}

// Embedding the real manager preserves every optional production observation
// interface. Ensure records the actual consumed descriptor without duplicating,
// moving, closing or replacing it, then delegates the unchanged spec and input.
type generatedFMP4BrowserJobs struct {
	*transcode.Manager
	observer *generatedFMP4BrowserObserver
}

func (jobs *generatedFMP4BrowserJobs) Ensure(ctx context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	if input != nil {
		jobs.observer.mu.Lock()
		if len(jobs.observer.sources) >= 128 {
			jobs.observer.overflow = true
		} else {
			jobs.observer.sources = append(jobs.observer.sources, input)
		}
		jobs.observer.mu.Unlock()
	}
	return jobs.Manager.Ensure(ctx, spec, input)
}

func generatedFMP4BrowserProducerRef(id string) string {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(id) {
		return ""
	}
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}

// Only bounded resource roles, source-slot numbers and opaque hashes enter the
// independent trace. Neither request queries nor redirect locations are stored.
func (observer *generatedFMP4BrowserObserver) record(request *http.Request, status int, headers http.Header) {
	base := filepath.Base(request.URL.Path)
	fact := generatedFMP4BrowserHTTP{Status: status, Number: -1}
	for _, role := range []struct{ prefix, extension, resource string }{
		{"window-init-", ".mp4", "init"}, {"window-segment-", ".m4s", "media"}, {"segment-", ".m4s", "media"},
	} {
		if strings.HasPrefix(base, role.prefix) && strings.HasSuffix(base, role.extension) {
			number, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(base, role.prefix), role.extension))
			if err == nil && number >= 0 && number < 17 {
				fact.Number, fact.Resource, fact.Kind = number, role.resource, "logical"
				if role.prefix == "segment-" {
					fact.Kind = "exact"
				}
			}
		}
	}
	id := request.URL.Query().Get(hlsProducerQuery)
	if base == "init.mp4" && id != "" {
		fact.Kind, fact.Resource = "exact", "init"
		if record, _ := observer.base.h.f.app.hls.manager.Snapshot(observer.base.graph.session.key.scope, id); record.ID == id {
			fact.Number = record.Spec.Plan.HLS.Window.StartNumber
		}
	}
	if fact.Kind == "logical" && status == http.StatusTemporaryRedirect {
		if target, err := url.Parse(headers.Get("Location")); err == nil {
			id = target.Query().Get(hlsProducerQuery)
		}
	}
	fact.ProducerRef = generatedFMP4BrowserProducerRef(id)
	if strings.HasSuffix(base, ".m3u8") {
		fact.Kind, fact.Resource = "playlist", "none"
	}
	if strings.HasPrefix(request.URL.Path, "/emby/Sessions/Playing") {
		fact.Kind, fact.Resource = "report", "none"
	}
	if fact.Kind == "" {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.trace) >= 1024 {
		observer.overflow = true
		return
	}
	observer.trace = append(observer.trace, fact)
}

func (observer *generatedFMP4BrowserObserver) snapshot(ctx context.Context) (generatedFMP4BrowserObservation, error) {
	base, err := observer.base.snapshot(ctx)
	if err != nil {
		return generatedFMP4BrowserObservation{}, err
	}
	value := generatedFMP4BrowserObservation{generatedWindowBrowserObservation: base, Jobs: make([]generatedFMP4BrowserJob, 0, len(base.Jobs))}
	value.Marker = "goby-generated-fmp4-browser-observation-v1"
	for _, job := range base.Jobs {
		record, _ := observer.base.h.f.app.hls.manager.Snapshot(observer.base.graph.session.key.scope, job.ID)
		if record.ID != job.ID {
			return value, errors.New("fMP4 actual job plan unavailable")
		}
		value.Jobs = append(value.Jobs, generatedFMP4BrowserJob{generatedWindowBrowserJob: job,
			Container: record.Spec.Plan.Container, SegmentType: record.Spec.Plan.HLS.SegmentType, NativeClockVersion: record.Spec.Plan.HLS.Window.NativeClockVersion})
	}
	session := observer.base.graph.session
	session.mu.Lock()
	if session.windowGraph != nil {
		for _, binding := range session.windowGraph.slots {
			for _, pin := range binding.initializationPins {
				if pin != nil {
					value.InitializationPins++
				}
			}
		}
	}
	budget := &observer.base.h.f.app.hls.initializationBudget
	budget.mu.Lock()
	value.ReservedMetadataPages = budget.reserved
	for _, page := range budget.pages {
		value.AllocatedMetadataBytes += len(page)
	}
	budget.mu.Unlock()
	session.mu.Unlock()
	value.RedirectPins += value.InitializationPins
	return value, nil
}

type generatedFMP4BrowserResult struct {
	Marker, RunId, Container, SegmentType                                        string
	Complete, SilentAVCOnly, AVQualified, OriginalEmbyClientUsed, DefaultEnabled bool
	RenditionCount, NativeClockVersion                                           int
	Checks                                                                       map[string]bool
	Frames                                                                       []generatedWindowBrowserFrame
	Transitions                                                                  []struct {
		generatedWindowBrowserTransition
		BeforeWallMilliseconds, AfterWallMilliseconds float64
	}
	HTTP []struct {
		Stage string
		generatedFMP4BrowserHTTP
	}
	Reports []struct {
		Stage, Event  string
		PositionTicks int64
		Status        int
	}
	Observations []struct {
		Stage string
		generatedFMP4BrowserObservation
	}
	PageErrors, FatalErrors, ForeignRequests int
	ClientStopLoadUsed                       bool
	ClientStopLoadPurpose                    string
	Output                                   struct {
		OK                   bool
		Level, Width, Height int
		Bandwidth            int64
		HlsVersion           string
	}
}

func generatedFMP4BrowserReadResult(filename string) (generatedFMP4BrowserResult, error) {
	var value generatedFMP4BrowserResult
	file, err := os.Open(filename)
	if err != nil {
		return value, errors.New("open private fMP4 browser result")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 || info.Mode().Perm() != 0600 {
		return value, errors.New("private fMP4 browser result exceeds admission")
	}
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	if decoder.Decode(&value) != nil {
		return value, errors.New("decode private fMP4 browser result")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return value, errors.New("extra fMP4 browser result document")
	}
	return value, nil
}

func generatedFMP4BrowserAssertFrames(t *testing.T, value generatedFMP4BrowserResult) {
	t.Helper()
	if !value.Output.OK || value.Output.Level != 0 || value.Output.Width != 160 || value.Output.Height != 96 || value.Output.HlsVersion != "1.6.0-beta.2" {
		t.Fatal("actual fMP4 consumer did not parse the genuinely negotiated single output")
	}
	for _, scenario := range []struct {
		stage   string
		target  float64
		channel int
	}{{"initial90", 90, 1}, {"seek94", 94, 2}, {"backseek30", 30, 0}} {
		var transition *generatedWindowBrowserTransition
		for index := range value.Transitions {
			if value.Transitions[index].Stage == scenario.stage {
				if transition != nil {
					t.Fatal("duplicate fMP4 presentation transition")
				}
				transition = &value.Transitions[index].generatedWindowBrowserTransition
			}
		}
		if transition == nil || transition.StartedAt <= 0 || transition.Target != scenario.target || transition.BaselinePresentedFrames < 0 {
			t.Fatal("fMP4 presentation omitted its actual seek and frame baseline")
		}
		count := 0
		var previous *generatedWindowBrowserFrame
		for index := range value.Frames {
			frame := &value.Frames[index]
			if frame.Stage != scenario.stage {
				continue
			}
			if frame.Level != 0 || frame.Width != 160 || frame.Height != 96 || frame.MediaTime < scenario.target || frame.MediaTime >= scenario.target+.8 ||
				frame.PresentedFrames <= transition.BaselinePresentedFrames || frame.PresentationTime < transition.StartedAt || len(frame.RGB) != 3 ||
				previous != nil && (frame.MediaTime <= previous.MediaTime || frame.PresentedFrames <= previous.PresentedFrames || frame.At <= previous.At) {
				t.Fatal("fMP4 actual presented frame lost its source interval, dimensions or freshness")
			}
			for channel, sample := range frame.RGB {
				if sample < 0 || sample > 255 || channel == scenario.channel && sample < 170 || channel != scenario.channel && sample > 60 {
					t.Fatal("fMP4 actual canvas source content disagrees")
				}
			}
			for _, number := range []float64{frame.MediaTime, frame.At, frame.PresentationTime} {
				if math.IsNaN(number) || math.IsInf(number, 0) {
					t.Fatal("fMP4 presented clocks are not finite")
				}
			}
			previous = frame
			count++
		}
		if count != 3 {
			t.Fatal("fMP4 consumer did not present three fresh actual source frames")
		}
	}
	var previous *generatedWindowBrowserFrame
	before, after := false, false
	for index := range value.Frames {
		frame := &value.Frames[index]
		if frame.Stage != "continuous" {
			continue
		}
		if frame.Level != 0 || frame.Width != 160 || frame.Height != 96 || frame.MediaTime < 94 || frame.MediaTime > 97.8 || len(frame.RGB) != 3 ||
			frame.RGB[2] < 170 || frame.RGB[0] > 60 || frame.RGB[1] > 60 || previous != nil &&
			(frame.MediaTime <= previous.MediaTime || frame.PresentedFrames <= previous.PresentedFrames || frame.At <= previous.At) {
			t.Fatal("fMP4 continuity used an invalid presented source frame")
		}
		before = before || frame.MediaTime < 96
		after = after || frame.MediaTime >= 97
		previous = frame
	}
	continuous := 0
	for _, transition := range value.Transitions {
		if transition.Stage == "continuous" {
			continuous++
			if transition.BeforeWallMilliseconds < 500 || transition.AfterWallMilliseconds < 500 {
				t.Fatal("fMP4 cross-window presentation lacks actual wall-clock samples on both sides")
			}
		}
	}
	if !before || !after || continuous != 1 {
		t.Fatal("fMP4 continuity did not cross the actual 96-second source boundary")
	}
}

func TestHTTPGeneratedWindowFMP4ChromiumHLSJSClient(t *testing.T) {
	runID := os.Getenv("GOBY_GENERATED_FMP4_BROWSER_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_GENERATED_FMP4_BROWSER_RUN_ID explicitly admits this owned actual client")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" ||
		!regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("fMP4 browser requires root, pinned actual tools and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	cache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	bundlePath := refreshBrowserPath(t, "GOBY_PHASE2_HLS_BUNDLE", false)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("fMP4 browser artifacts parent is not private")
	}
	bundleInfo, err := os.Stat(bundlePath)
	if err != nil || bundleInfo.Size() < 1 || bundleInfo.Size() > 8<<20 {
		t.Fatal("fMP4 browser pinned bundle exceeds admission")
	}
	bundle, err := os.ReadFile(bundlePath)
	digest := sha256.Sum256(bundle)
	if err != nil || hex.EncodeToString(digest[:]) != phase2BrowserBundleSHA256 {
		t.Fatal("fMP4 browser bundle differs from the pinned engine")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read fMP4 browser source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate fMP4 browser source root")
	}
	output, err := os.MkdirTemp(artifacts, "generated-fmp4-browser-")
	if err != nil || os.Chmod(output, 0700) != nil {
		t.Fatal("create private fMP4 browser evidence directory")
	}
	driver := map[string]any{"Marker": "goby-generated-fmp4-browser-driver-v1", "RunId": runID, "Complete": false,
		"DefaultEnabled": false, "OriginalEmbyClientUsed": false, "AVQualified": false, "SilentAVCOnly": true, "RenditionCount": 0,
		"RealChromiumAndHLSJS": true, "HlsBundleSHA256": phase2BrowserBundleSHA256, "CredentialsWrittenToSummary": false}
	processBefore := media.GetProcessCapacityStats()
	defer func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve private fMP4 browser driver evidence")
		}
	}()
	h := newHLSGeneratedWindowHTTPFixture(t)
	observer := &generatedFMP4BrowserObserver{}
	manager, ok := h.f.app.hls.manager.(*transcode.Manager)
	if !ok {
		t.Fatal("fMP4 browser requires the actual manager")
	}
	h.f.app.hls.manager = &generatedFMP4BrowserJobs{Manager: manager, observer: observer}
	graph := hlsGeneratedWindowFMP4HTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowFMP4HTTPLoad(t, h, &graph)
	observer.base.h, observer.base.graph = h, graph.hlsGeneratedWindowHTTPGraph
	initial, err := observer.snapshot(h.f.ctx)
	if err != nil || len(initial.Jobs) != 1 || initial.Jobs[0].StartNumber != 15 || !initial.GraphPublished || initial.ReservedMetadataPages < 1 {
		t.Fatal("fMP4 initial high seek lacks its genuine native closed graph and metadata reservation")
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "initial-server-evidence.json"), initial) != nil {
		t.Fatal("preserve fMP4 initial actual graph evidence")
	}
	private := h.f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/__generated-fmp4-consumer":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>Owned single fMP4 acceptance</title></head><body><video id="generated-video" muted playsinline width="160" height="96"></video><canvas id="generated-pixel" width="1" height="1"></canvas></body></html>`)
		case "/__generated-fmp4-hls.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bundle)
		case "/__generated-fmp4-observe":
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			value, err := observer.snapshot(ctx)
			if err != nil {
				http.Error(w, "fMP4 observation unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(value)
		default:
			http.NotFound(w, r)
		}
	})
	product := h.f.handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__generated-fmp4-") {
			if r.Method != http.MethodGet {
				http.Error(w, "read-only fixture", http.StatusMethodNotAllowed)
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
		observer.record(r, writer.status, writer.Header())
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
	contextPath, resultPath := filepath.Join(output, "private-context.json"), filepath.Join(output, "browser-result.json")
	fixture := map[string]any{"Marker": "goby-generated-fmp4-browser-fixture-v1", "RunId": runID, "BaseURL": server.URL, "Headers": headers,
		"MasterURL": graph.masterURL, "MainURL": graph.mainURLs[0], "InitializationURLs": graph.initializationURLs, "MediaURLs": graph.mediaURLs[0],
		"ItemId": h.item.ID, "PlaySessionId": graph.playID, "ProducerQuery": hlsProducerQuery, "RenditionCount": 0, "NativeClockVersion": 2,
		"Width": graph.session.key.plan.Width, "Height": graph.session.key.plan.Height, "Bandwidth": graph.session.output.Info.Bitrate,
		"HlsBundleSHA256": phase2BrowserBundleSHA256, "ArtifactsDir": output, "ResultPath": resultPath,
		"PagePath": "/__generated-fmp4-consumer", "BundlePath": "/__generated-fmp4-hls.js", "ObservePath": "/__generated-fmp4-observe"}
	defer func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove private fMP4 credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	}()
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write private fMP4 browser context")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0700) != nil {
			t.Fatal("create private browser process directories")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1", "HOME=" + filepath.Join(output, "home"),
		"TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"), "PLAYWRIGHT_BROWSERS_PATH=" + cache,
		"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright, "GOBY_GENERATED_FMP4_BROWSER_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(h.f.ctx, 3*time.Minute)
	defer cancel()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{"--max-old-space-size=128", filepath.Join(sourceRoot, "scripts", "test-env", "generated-window-fmp4-browser.mjs")}, environment, sourceRoot, output)
	driver["BrowserCommand"] = command
	value, resultErr := generatedFMP4BrowserReadResult(resultPath)
	final, finalErr := observer.snapshot(h.f.ctx)
	if finalErr == nil {
		if refreshBrowserWriteJSON(filepath.Join(output, "final-server-evidence.json"), final) != nil {
			t.Error("preserve fMP4 final server evidence")
		}
	}
	observer.mu.Lock()
	trace, overflow, sources := append([]generatedFMP4BrowserHTTP(nil), observer.trace...), observer.overflow, append([]*os.File(nil), observer.sources...)
	observer.mu.Unlock()
	if refreshBrowserWriteJSON(filepath.Join(output, "server-http-evidence.json"), map[string]any{"Requests": trace, "Overflow": overflow}) != nil {
		t.Error("preserve independent fMP4 HTTP evidence")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	closeErr := h.f.app.hls.Close(closeCtx)
	closeCancel()
	driver["RuntimeClosedBeforeFixtureCleanup"] = closeErr == nil
	budget := &h.f.app.hls.initializationBudget
	budget.mu.Lock()
	reserved, allocated, arenaClosed := budget.reserved, 0, budget.closed
	for _, page := range budget.pages {
		allocated += len(page)
	}
	budget.mu.Unlock()
	h.f.app.hls.generatedWindowPinMu.Lock()
	globalPins := len(h.f.app.hls.generatedWindowPinOwners)
	h.f.app.hls.generatedWindowPinMu.Unlock()
	fdClosed := len(sources) > 0
	for _, file := range sources {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			fdClosed = false
		}
	}
	driver["CapturedSourceDescriptors"], driver["CapturedSourceDescriptorsClosed"] = len(sources), fdClosed
	driver["ReservedMetadataPagesAfterClose"], driver["AllocatedMetadataBytesAfterClose"], driver["MetadataArenaClosed"] = reserved, allocated, arenaClosed
	driver["GlobalRedirectPinOwnersAfterClose"] = globalPins
	resources, supported := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	var usage transcode.ResourceUsage
	var usageErr error
	if supported {
		usage, usageErr = resources.ResourceUsage(h.f.ctx, graph.session.key.scope)
	} else {
		usageErr = errors.New("scoped resource observer unavailable")
	}
	driver["ManagerScopeAfterClose"] = usage
	processAfter := media.GetProcessCapacityStats()
	driver["ProcessCapacityBefore"], driver["ProcessCapacityAfterClose"] = processBefore, processAfter
	cacheRemoved := true
	for _, job := range final.Jobs {
		if filepath.Base(job.ID) != job.ID {
			cacheRemoved = false
			continue
		}
		if _, err := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, job.ID)); !errors.Is(err, os.ErrNotExist) {
			cacheRemoved = false
		}
	}
	driver["ObservedPrivateJobCachesRemoved"] = cacheRemoved
	if commandErr != nil || resultErr != nil || finalErr != nil || closeErr != nil || usageErr != nil {
		t.Fatalf("fMP4 browser failed: command=%T result=%T observation=%T close=%T resources=%T; inspect private evidence", commandErr, resultErr, finalErr, closeErr, usageErr)
	}
	if overflow || !fdClosed || !arenaClosed || reserved != 0 || allocated != 0 || globalPins != 0 || usage != (transcode.ResourceUsage{}) || !cacheRemoved ||
		processAfter.Active != 0 || processAfter.Background != 0 || processAfter.Queued != 0 || processAfter.RetirementUnknown != processBefore.RetirementUnknown {
		t.Fatal("fMP4 owned runtime did not retire its actual source descriptors, role pins, metadata, readers or caches")
	}
	if value.Marker != "goby-generated-fmp4-browser-result-v1" || value.RunId != runID || !value.Complete || value.Container != "mp4" || value.SegmentType != "fmp4" ||
		value.RenditionCount != 0 || value.NativeClockVersion != 2 || !value.SilentAVCOnly || value.AVQualified || value.OriginalEmbyClientUsed || value.DefaultEnabled ||
		value.PageErrors != 0 || value.FatalErrors != 0 || value.ForeignRequests != 0 || !value.ClientStopLoadUsed || value.ClientStopLoadPurpose != "client_request_reduction" {
		t.Fatal("fMP4 result does not bind the narrow genuine actual-client scenario")
	}
	checks := []string{"Initial90", "Seek94", "ContinuousAcrossWindows", "CachedMapAndMedia", "PauseColdAdmission", "PingRemainsPaused", "BackSeek30", "StandardReports", "StoppedResources"}
	if len(value.Checks) != len(checks) {
		t.Fatal("fMP4 client result changed its required checks")
	}
	for _, check := range checks {
		if !value.Checks[check] {
			t.Fatalf("fMP4 client check incomplete: %s", check)
		}
	}
	generatedFMP4BrowserAssertFrames(t, value)
	if len(value.Frames) > 256 || len(value.Transitions) != 4 || len(value.HTTP) > 256 || len(value.Observations) > 24 {
		t.Fatal("fMP4 client evidence exceeded its fixed scenario bounds")
	}
	seen := make(map[int]bool)
	for _, job := range final.Jobs {
		if job.Container != "mp4" || job.SegmentType != "fmp4" || job.Renditions != 0 || job.NativeClockVersion != 2 || !job.RequireInputEvidence ||
			job.StartNumber != 5 && job.StartNumber != 6 && job.StartNumber != 15 && job.StartNumber != 16 || job.StartTicks != int64(job.StartNumber)*6*media.TicksPerSecond ||
			job.EndTicks <= job.StartTicks || job.EndTicks > 100*media.TicksPerSecond || job.EndTicks-job.StartTicks > 6*media.TicksPerSecond {
			t.Fatal("fMP4 browser produced an unproved or unnegotiated private job")
		}
		seen[job.StartNumber] = true
	}
	if !seen[5] || !seen[15] || !seen[16] || final.PlayState != "Stopped" || !final.Closed || final.Pending || final.RedirectPins != 0 || final.InitializationPins != 0 || final.GlobalRedirectPinOwners != 0 || final.ReservedMetadataPages != 0 {
		t.Fatal("fMP4 client omitted required source windows or Stop retirement")
	}
	for _, number := range []int{5, 15, 16} {
		for _, resource := range []string{"init", "media"} {
			var ref string
			var exact bool
			for _, request := range trace {
				if request.Number == number && request.Resource == resource && request.Kind == "logical" && request.Status == http.StatusTemporaryRedirect {
					if ref != "" && ref != request.ProducerRef {
						t.Fatal("fMP4 logical resource changed its actual closed producer")
					}
					ref = request.ProducerRef
				}
			}
			for _, request := range trace {
				exact = exact || request.Number == number && request.Resource == resource && request.Kind == "exact" && request.Status == http.StatusOK && request.ProducerRef == ref
			}
			if ref == "" || !exact {
				t.Fatal("fMP4 actual presented window lacks its independent exact resource handoff")
			}
			var sibling string
			for _, request := range trace {
				if request.Number == number && request.Kind == "logical" && request.Status == http.StatusTemporaryRedirect && request.Resource != resource {
					sibling = request.ProducerRef
				}
			}
			if ref != sibling {
				t.Fatal("fMP4 init/media borrowed different actual private producers")
			}
		}
	}
	for _, resource := range []string{"init", "media"} {
		cold := false
		for _, request := range trace {
			cold = cold || request.Kind == "logical" && request.Resource == resource && request.Number == 0 && request.Status == http.StatusServiceUnavailable
		}
		if !cold {
			t.Fatal("fMP4 actual paused admission lacks independent cold MAP and media denials")
		}
	}
	var paused *generatedFMP4BrowserObservation
	pauseStages := make(map[string]bool)
	for index := range value.Observations {
		observation := &value.Observations[index]
		if !strings.HasPrefix(observation.Stage, "pause-") && observation.Stage != "ping-after-cold" {
			continue
		}
		if pauseStages[observation.Stage] {
			t.Fatal("duplicate fMP4 paused demand observation")
		}
		pauseStages[observation.Stage] = true
		if observation.PlayState != "Paused" || !observation.Paused || observation.Closed || observation.Pending || observation.PlaybackRevision != observation.RuntimeRevision || observation.ReservedMetadataPages < 1 {
			t.Fatal("fMP4 paused role requests are not bound to committed demand")
		}
		if observation.Stage == "pause-before" {
			paused = &observation.generatedFMP4BrowserObservation
		}
		if paused == nil || len(paused.Jobs) != len(observation.Jobs) || paused.PlaybackRevision != observation.PlaybackRevision || paused.PositionTicks != observation.PositionTicks || paused.ReservedMetadataPages != observation.ReservedMetadataPages {
			t.Fatal("fMP4 paused requests changed production, demand or initialization history")
		}
		for index, job := range paused.Jobs {
			if job != observation.Jobs[index] {
				t.Fatal("fMP4 paused requests admitted or replaced an actual job")
			}
		}
	}
	if paused == nil || len(pauseStages) != 4 {
		t.Fatal("fMP4 omitted committed pause evidence")
	}
	for _, event := range []string{"Started", "TimeUpdate", "Pause", "Ping", "Unpause", "Stopped"} {
		found := false
		for _, report := range value.Reports {
			if report.Status != 204 || report.PositionTicks < 0 || report.PositionTicks >= 100*media.TicksPerSecond {
				t.Fatal("fMP4 standard report invalid")
			}
			found = found || report.Event == event
		}
		if !found {
			t.Fatal("fMP4 actual client omitted a standard playback event")
		}
	}
	driver["Complete"], driver["Checks"], driver["ObservedJobCount"] = true, value.Checks, len(final.Jobs)
	t.Log("generated_window_fmp4_chromium_verified=true genuine_single_output=true native_clock_version=2 global_seek_seconds=90,94,30 continuous_windows=15,16 original_emby_client_used=false av_qualified=false default_enabled=false")
}
