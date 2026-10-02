//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type generatedWholeBrowserJobs struct {
	*transcode.Manager
	mu       sync.Mutex
	inputs   []*os.File
	overflow bool
}

func (jobs *generatedWholeBrowserJobs) Ensure(ctx context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	if input != nil {
		jobs.mu.Lock()
		if len(jobs.inputs) >= 128 {
			jobs.overflow = true
		} else {
			jobs.inputs = append(jobs.inputs, input)
		}
		jobs.mu.Unlock()
	}
	return jobs.Manager.Ensure(ctx, spec, input)
}

type generatedWholeBrowserOutput struct {
	Variant                int
	PlaylistSegments       int
	PlaylistDurationTicks  int64
	PacketCount            int64
	NativeFirst, NativeEnd transcode.GeneratedRational
	Segments               []transcode.GeneratedSegmentBounds
}

type generatedWholeBrowserResult struct {
	generatedWindowBrowserResult
	FiniteGraphUsed, AVQualified, DefaultEnabled bool
	FailureStage, FailureCode                    string
}

func generatedWholeBrowserLoad(t *testing.T, h *hlsHTTPFixture, graph *hlsGeneratedWindowHTTPGraph,
	endpoint transcode.GeneratedSourceEndpointCertificate) []generatedWholeBrowserOutput {
	t.Helper()
	response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	ids := hlsGeneratedWindowHTTPJobIDs(t, h, *graph)
	if len(ids) != 1 {
		t.Fatal("natural whole output did not establish one genuine adaptive producer")
	}
	id := ids[0]
	hlsGeneratedWindowHTTPWait(t, h, 2*time.Minute, func() bool {
		record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
		return err == nil && record.State == "completed"
	}, "natural whole producer did not complete normally")
	record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
	plan := record.Spec.Plan
	if err != nil || record.State != "completed" || plan.StartTicks != 0 || plan.HLS.Window != (transcode.HLSWindow{}) ||
		plan.DurationTicks != h.item.Media.DurationTicks || plan.HLS.RenditionCount != 4 || plan.HLS.SegmentType != "mpegts" || plan.FrameRate != 24 || plan.SegmentSeconds != 6 ||
		plan.VideoCodec != "h264" || plan.AudioStreamIndex != -1 || plan.AudioCodec != "" {
		t.Fatal("baseline changed the naturally negotiated whole-production plan")
	}
	graph.session.mu.Lock()
	finite := graph.session.windowGraph != nil && !graph.session.windowGraph.fallback
	graph.session.mu.Unlock()
	if finite {
		t.Fatal("whole baseline acquired a finite generated-window graph")
	}
	outputs := make([]generatedWholeBrowserOutput, 0, 4)
	var referenceFirst, referenceEnd *big.Rat
	for variant := 0; variant < 4; variant++ {
		response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[variant], nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if !bytes.Contains(response.body, []byte("#EXT-X-PLAYLIST-TYPE:EVENT\n")) || !bytes.Contains(response.body, []byte("#EXT-X-ENDLIST\n")) ||
			bytes.Contains(response.body, []byte("window-segment-")) || bytes.Contains(response.body, []byte("#EXT-X-DISCONTINUITY\n")) || bytes.Contains(response.body, []byte("#EXT-X-MAP")) {
			t.Fatal("whole baseline did not retain its actual direct exact EVENT protocol")
		}
		children := hlsHTTPManifestChildren(response.body)
		if len(children) != 17 {
			t.Fatal("natural whole producer does not have the actual complete 100-second output")
		}
		graph.mediaURLs[variant], graph.manifests[variant] = children, bytes.Clone(response.body)
		listHandle, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, id, transcode.HLSPlaylistName(variant, 4))
		if err != nil {
			t.Fatal("open actual completed whole producer playlist")
		}
		listBytes, readErr := io.ReadAll(io.LimitReader(listHandle, transcode.MaxPlaylistBytes+1))
		closeErr := listHandle.Close()
		list, parseErr := transcode.ParseMediaPlaylist(listBytes)
		if readErr != nil || closeErr != nil || parseErr != nil || !list.Ended || list.Type != "EVENT" || list.Sequence != 0 || len(list.Segments) != 17 || list.InitName != "" {
			t.Fatal("actual whole private playlist is not a normally closed complete EVENT")
		}
		output := generatedWholeBrowserOutput{Variant: variant, PlaylistSegments: len(list.Segments), Segments: make([]transcode.GeneratedSegmentBounds, 0, 17)}
		var first, end *big.Rat
		for number, segment := range list.Segments {
			expected := fmt.Sprintf("v%d-segment-%06d.ts", variant, number)
			parsed := hlsHTTPURL(t, children[number], graph.owner.headers.Get("X-Emby-Token"))
			if segment.Name != expected || filepath.Base(parsed.Path) != expected || parsed.Query().Get(hlsProducerQuery) != id ||
				parsed.Query().Get(hlsGeneratedWindowGraphQuery) != "" || segment.Number != int64(number) || segment.Discontinuity {
				t.Fatal("whole baseline fabricated a logical admission or segment namespace")
			}
			handle, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, id, segment.Name)
			if err != nil {
				t.Fatal("actual completed whole segment unavailable")
			}
			before, beforeErr := handle.Stat()
			var beforeIdentity string
			if beforeErr == nil {
				beforeIdentity, beforeErr = media.VideoSeekSourceIdentity(before)
			}
			beforeOffset, offsetErr := handle.Seek(0, io.SeekCurrent)
			framingErr := transcode.ValidateGeneratedWindowFraming(plan, nil, handle.File)
			bounds, boundsErr := transcode.MeasureGeneratedSegmentBounds(h.f.ctx, h.f.app.cfg.FFprobePath, nil, handle.File, true)
			after, afterErr := handle.Stat()
			var afterIdentity string
			if afterErr == nil {
				afterIdentity, afterErr = media.VideoSeekSourceIdentity(after)
			}
			afterOffset, afterOffsetErr := handle.Seek(0, io.SeekCurrent)
			closeErr := handle.Close()
			video := bounds.Video
			if beforeErr != nil || afterErr != nil || offsetErr != nil || afterOffsetErr != nil || beforeIdentity != afterIdentity || beforeOffset != afterOffset ||
				framingErr != nil || boundsErr != nil || closeErr != nil || !video.Present || bounds.Audio.Present || video.Codec != "h264" ||
				!video.FirstKey || video.HasCorrupt || video.HasDiscard || !video.PresentationDecodeAligned || video.MinPacketDuration <= 0 || video.MinPacketDuration != video.MaxPacketDuration ||
				video.FirstPTS != video.FirstDTS || video.EndPTS != video.EndDTS || video.TotalPresentationGapTicks != 0 || video.TotalDecodeGapTicks != 0 || bounds.SegmentSHA256 == ([32]byte{}) {
				t.Fatal("whole baseline lacks complete actual framing and every-packet coverage")
			}
			packetFirst := new(big.Rat).SetFrac64(video.FirstPTS*video.TimeBase.Num, video.TimeBase.Den)
			packetEnd := new(big.Rat).SetFrac64(video.EndPTS*video.TimeBase.Num, video.TimeBase.Den)
			period := new(big.Rat).SetFrac64(video.MinPacketDuration*video.TimeBase.Num, video.TimeBase.Den)
			span := new(big.Rat).Sub(packetEnd, packetFirst)
			if period.Cmp(big.NewRat(1, 24)) != 0 || span.Cmp(new(big.Rat).SetFrac64(segment.DurationTicks, media.TicksPerSecond)) != 0 ||
				span.Cmp(new(big.Rat).SetFrac64(video.PacketCount, 24)) != 0 || end != nil && packetFirst.Cmp(end) != 0 {
				t.Fatal("whole baseline actual packets are incomplete or discontinuous")
			}
			if first == nil {
				first = packetFirst
			}
			end = packetEnd
			output.PacketCount += video.PacketCount
			output.PlaylistDurationTicks += segment.DurationTicks
			output.Segments = append(output.Segments, bounds)
		}
		if output.PacketCount != endpoint.SampleCount || output.PacketCount != 2400 || output.PlaylistDurationTicks != endpoint.DurationTicks ||
			new(big.Rat).Sub(end, first).Cmp(new(big.Rat).SetFrac64(endpoint.DurationTicks, media.TicksPerSecond)) != 0 {
			t.Fatal("whole output did not actually produce all 2400 frames and the complete native 100-second packet interval")
		}
		if variant == 0 {
			referenceFirst, referenceEnd = first, end
		} else if first.Cmp(referenceFirst) != 0 || end.Cmp(referenceEnd) != 0 {
			t.Fatal("whole adaptive siblings have different actual native packet intervals")
		}
		output.NativeFirst = transcode.GeneratedRational{Num: first.Num().Int64(), Den: first.Denom().Int64()}
		output.NativeEnd = transcode.GeneratedRational{Num: end.Num().Int64(), Den: end.Denom().Int64()}
		outputs = append(outputs, output)
	}
	return outputs
}

func generatedWholeBrowserAssertFrames(t *testing.T, value generatedWholeBrowserResult, plan transcode.Plan) {
	t.Helper()
	if len(value.Levels) != 4 || len(value.Transitions) != 3 || len(value.Frames) != 9 {
		t.Fatal("whole quality baseline changed its actual action and frame set")
	}
	seen := make(map[int]bool)
	for _, level := range value.Levels {
		if level.Level < 0 || level.Level >= 4 || level.Variant < 0 || level.Variant >= 4 || seen[level.Variant] {
			t.Fatal("whole baseline actual level mapping is not unique")
		}
		rendition := plan.HLS.Renditions[level.Variant]
		if level.Width != rendition.Width || level.Height != rendition.Height || level.Bandwidth != rendition.VideoBitrate*10/9 {
			t.Fatal("whole baseline did not retain its real negotiated ladder")
		}
		seen[level.Variant] = true
	}
	for _, scenario := range []struct {
		stage   string
		target  float64
		variant int
	}{{"initial90", 90, 0}, {"seek91", 91, 0}, {"quality91", 91, 1}} {
		var transition *generatedWindowBrowserTransition
		for index := range value.Transitions {
			if value.Transitions[index].Stage == scenario.stage {
				if transition != nil {
					t.Fatal("duplicate whole baseline action")
				}
				transition = &value.Transitions[index]
			}
		}
		if transition == nil || transition.Target != scenario.target || transition.Variant != scenario.variant || transition.StartedAt <= 0 || transition.BaselinePresentedFrames < 0 ||
			scenario.stage == "quality91" && (transition.FragmentBarrierAt != transition.StartedAt || transition.FragChangedAt < transition.StartedAt) {
			t.Fatal("whole baseline lost its original strict frame/fragment action barrier")
		}
		count := 0
		var previous *generatedWindowBrowserFrame
		for index := range value.Frames {
			frame := &value.Frames[index]
			if frame.Stage != scenario.stage {
				continue
			}
			rendition := plan.HLS.Renditions[scenario.variant]
			if frame.Variant != scenario.variant || frame.Level != transition.Level || frame.Width != rendition.Width || frame.Height != rendition.Height ||
				frame.MediaTime < scenario.target || frame.MediaTime >= scenario.target+.8 || frame.PresentationTime < transition.StartedAt || frame.PresentedFrames <= transition.BaselinePresentedFrames ||
				scenario.stage == "quality91" && frame.PresentationTime < transition.FragChangedAt || len(frame.RGB) != 3 || frame.RGB[1] < 170 || frame.RGB[0] > 60 || frame.RGB[2] > 60 ||
				previous != nil && (frame.MediaTime <= previous.MediaTime || frame.At <= previous.At || frame.PresentedFrames <= previous.PresentedFrames) {
				t.Fatal("whole no-seek quality used a stale, incorrectly sized or wrong-source frame")
			}
			count++
			previous = frame
		}
		if count != 3 {
			t.Fatal("whole baseline omitted its original three actual advancing frames")
		}
	}
}

func TestHTTPGeneratedWholeProducerChromiumNoSeekQualityBaseline(t *testing.T) {
	runID := os.Getenv("GOBY_GENERATED_WHOLE_BROWSER_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_GENERATED_WHOLE_BROWSER_RUN_ID explicitly admits the transparent baseline")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" ||
		!regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("whole baseline requires root, owned database and pinned actual tools")
	}
	node, playwright := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false), refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts, cache := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true), refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	bundlePath := refreshBrowserPath(t, "GOBY_PHASE2_HLS_BUNDLE", false)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("whole baseline artifact parent is not private")
	}
	bundleInfo, err := os.Stat(bundlePath)
	if err != nil || bundleInfo.Size() < 1 || bundleInfo.Size() > 8<<20 {
		t.Fatal("whole baseline bundle exceeds admission")
	}
	bundle, err := os.ReadFile(bundlePath)
	digest := sha256.Sum256(bundle)
	if err != nil || hex.EncodeToString(digest[:]) != phase2BrowserBundleSHA256 {
		t.Fatal("whole baseline requires the unchanged pinned HLS bundle")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read baseline source root")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate baseline source root")
	}
	output, err := os.MkdirTemp(artifacts, "generated-whole-browser-")
	if err != nil || os.Chmod(output, 0700) != nil {
		t.Fatal("create private whole baseline evidence")
	}
	driver := map[string]any{"Marker": "goby-generated-whole-browser-driver-v1", "RunId": runID, "Complete": false,
		"Scope": "transparent_whole_producer_no_seek_quality_baseline", "OriginalEmbyClientUsed": false, "FiniteGraphUsed": false, "AVQualified": false, "DefaultEnabled": false,
		"OriginalActionSourceSHA256": "5fee16ec86fe3596f1cb29ec00bba4e01244b26ef6cded5d875e1c5bfffbc640", "HlsBundleSHA256": phase2BrowserBundleSHA256, "CredentialsWrittenToSummary": false}
	defer func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve whole baseline driver")
		}
	}()
	processBefore := media.GetProcessCapacityStats()
	h := newHLSGeneratedWindowHTTPFixture(t)
	// Selecting the existing disabled/default path changes only this fixture's
	// feature admission. Negotiation, catalog data and producer plans stay real.
	h.f.app.hls.generatedWindowsEnabled = false
	manager, ok := h.f.app.hls.manager.(*transcode.Manager)
	if !ok {
		t.Fatal("whole baseline requires the real manager")
	}
	jobs := &generatedWholeBrowserJobs{Manager: manager}
	h.f.app.hls.manager = jobs
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	if graph.session.key.plan.HLS.RenditionCount != 4 {
		t.Fatal("natural baseline does not match the actual four-rendition client fixture")
	}
	source, _, err := h.f.app.authorizeHLS(h.f.ctx, graph.session.principal, graph.session.key.scope, graph.session.key.stamp, graph.session.key.plan)
	if err != nil {
		t.Fatal("authorize unchanged actual whole source")
	}
	defer source.Close()
	endpoint, err := transcode.MeasureGeneratedMP4SourceEndpoint(h.f.ctx, source, 0)
	if err != nil || !endpoint.DurationTicksExact || endpoint.DurationTicks != 100*media.TicksPerSecond || endpoint.SampleCount != 2400 ||
		endpoint.FrameDuration != (transcode.GeneratedRational{Num: 1, Den: 24}) || endpoint.Origin != (transcode.GeneratedRational{Num: 2, Den: 1}) {
		t.Fatal("baseline source does not have its independent native 100-second endpoint")
	}
	outputs := generatedWholeBrowserLoad(t, h, &graph, endpoint)
	wholeIDs := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
	if len(wholeIDs) != 1 {
		t.Fatal("whole baseline lost its measured actual producer identity")
	}
	if transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint) != nil {
		t.Fatal("whole baseline source changed across actual production and all-packet observation")
	}
	driver["CatalogDurationTicks"], driver["NativeDurationTicks"], driver["SourceOriginTicks"] = h.item.Media.DurationTicks, endpoint.DurationTicks, h.item.Media.FormatStartTicks
	driver["WholeStartTicks"], driver["NativeClockVersion"], driver["WindowZero"], driver["SegmentSeconds"], driver["RenditionCount"] = 0, 0, true, 6, 4
	driver["FullDecodedSourceClosureQualified"] = false
	if refreshBrowserWriteJSON(filepath.Join(output, "whole-packet-source-evidence.json"), map[string]any{"Endpoint": endpoint, "Outputs": outputs, "CatalogDurationTicks": h.item.Media.DurationTicks}) != nil {
		t.Fatal("preserve quantified actual whole-production evidence")
	}
	observer := &generatedWindowBrowserObserver{h: h, graph: graph}
	private := h.f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/__whole-baseline-consumer":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>Owned whole no-seek baseline</title></head><body><video id="generated-video" muted playsinline width="160" height="96"></video><canvas id="generated-pixel" width="1" height="1"></canvas></body></html>`)
		case "/__whole-baseline-hls.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bundle)
		case "/__whole-baseline-observe":
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			value, err := observer.snapshot(ctx)
			if err != nil {
				http.Error(w, "whole baseline observation unavailable", 503)
				return
			}
			value.NativeDurationTicks = endpoint.DurationTicks
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(value)
		default:
			http.NotFound(w, r)
		}
	})
	product := h.f.handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__whole-baseline-") {
			if r.Method != http.MethodGet {
				http.Error(w, "read-only fixture", 405)
				return
			}
			private(w, r)
			return
		}
		writer := &generatedWindowBrowserResponse{ResponseWriter: w}
		product.ServeHTTP(writer, r)
		if writer.status == 0 {
			writer.status = 200
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
	contextPath, resultPath := filepath.Join(output, "private-context.json"), filepath.Join(output, "browser-result.json")
	fixture := map[string]any{"Marker": "goby-generated-window-browser-fixture-v1", "RunId": runID, "BaseURL": server.URL, "Headers": headers,
		"MasterURL": graph.masterURL, "MainURLs": graph.mainURLs, "MediaURLs": graph.mediaURLs, "ItemId": h.item.ID, "PlaySessionId": graph.playID,
		"ExpectedRenditions": 4, "Renditions": graph.session.key.plan.HLS.Renditions[:4], "HlsBundleSHA256": phase2BrowserBundleSHA256,
		"ArtifactsDir": output, "ResultPath": resultPath, "ObservePath": "/__whole-baseline-observe", "PagePath": "/__whole-baseline-consumer", "BundlePath": "/__whole-baseline-hls.js"}
	defer func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove whole baseline private credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	}()
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write private whole baseline context")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0700) != nil {
			t.Fatal("create owned whole baseline process directory")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1", "HOME=" + filepath.Join(output, "home"),
		"TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"), "PLAYWRIGHT_BROWSERS_PATH=" + cache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
		"GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright, "GOBY_GENERATED_WHOLE_BROWSER_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(h.f.ctx, 3*time.Minute)
	defer cancel()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{"--max-old-space-size=128", filepath.Join(sourceRoot, "scripts", "test-env", "generated-window-whole-browser-baseline.mjs")}, environment, sourceRoot, output)
	driver["BrowserCommand"] = command
	var value generatedWholeBrowserResult
	file, readErr := os.Open(resultPath)
	if readErr == nil {
		info, err := file.Stat()
		if err != nil || info.Size() < 1 || info.Size() > 1<<20 || info.Mode().Perm() != 0600 {
			readErr = errors.New("whole result admission")
		} else {
			decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
			readErr = decoder.Decode(&value)
			var extra any
			if readErr == nil && decoder.Decode(&extra) != io.EOF {
				readErr = errors.New("extra whole result document")
			}
		}
		_ = file.Close()
	}
	if !value.Complete {
		// Failure cleanup is labelled separately and cannot turn absent actual
		// quality frames into a passing original-client compatibility result.
		stopped := hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing/Stopped", map[string]any{"ItemId": h.item.ID, "PlaySessionId": graph.playID}, graph.owner.headers)
		driver["FailureCleanupStandardStoppedStatus"], driver["FailureCleanupPerformedByGo"] = stopped.status, true
	}
	final, finalErr := observer.snapshot(h.f.ctx)
	if finalErr == nil {
		final.NativeDurationTicks = endpoint.DurationTicks
		if refreshBrowserWriteJSON(filepath.Join(output, "final-server-evidence.json"), final) != nil {
			t.Error("preserve whole final server evidence")
		}
	}
	observer.mu.Lock()
	trace, traceOverflow := append([]generatedWindowBrowserHTTP(nil), observer.trace...), observer.overflow
	observer.mu.Unlock()
	if refreshBrowserWriteJSON(filepath.Join(output, "server-http-evidence.json"), map[string]any{"Requests": trace, "Overflow": traceOverflow}) != nil {
		t.Error("preserve whole actual HTTP evidence")
	}
	sourceFenceErr := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint)
	sourceCloseErr := source.Close()
	_, closedSourceErr := source.Stat()
	driver["SourceEndpointIdentityPreservedAfterBrowser"] = sourceFenceErr == nil
	driver["IndependentSourceDescriptorClosed"] = sourceCloseErr == nil && errors.Is(closedSourceErr, os.ErrClosed)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	closeErr := h.f.app.hls.Close(closeCtx)
	closeCancel()
	jobs.mu.Lock()
	inputs, overflow := append([]*os.File(nil), jobs.inputs...), jobs.overflow
	jobs.mu.Unlock()
	inputsClosed := len(inputs) > 0
	for _, input := range inputs {
		if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
			inputsClosed = false
		}
	}
	usage, usageErr := jobs.ResourceUsage(h.f.ctx, graph.session.key.scope)
	cacheRemoved := true
	for _, id := range wholeIDs {
		if filepath.Base(id) != id {
			cacheRemoved = false
			continue
		}
		if _, err := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
			cacheRemoved = false
		}
	}
	processAfter := media.GetProcessCapacityStats()
	driver["RuntimeClosedBeforeFixtureCleanup"], driver["ConsumedInputDescriptorsClosed"], driver["ConsumedInputDescriptors"] = closeErr == nil, inputsClosed, len(inputs)
	driver["ManagerScopeAfterClose"], driver["ObservedWholeCacheRemoved"], driver["ProcessCapacityBefore"], driver["ProcessCapacityAfterClose"] = usage, cacheRemoved, processBefore, processAfter
	driver["FailureStage"], driver["FailureCode"], driver["NoSeekQualityVerified"] = value.FailureStage, value.FailureCode, value.NoSeekQualityVerified
	if closeErr != nil || usageErr != nil || sourceFenceErr != nil || sourceCloseErr != nil || !errors.Is(closedSourceErr, os.ErrClosed) ||
		!inputsClosed || overflow || traceOverflow || !cacheRemoved || usage != (transcode.ResourceUsage{}) || processAfter.Active != 0 || processAfter.Queued != 0 || processAfter.Background != 0 || processAfter.RetirementUnknown != processBefore.RetirementUnknown {
		t.Fatal("whole baseline did not retire its owned actual producer, inputs, readers, caches or known process admission")
	}
	if commandErr != nil || readErr != nil || finalErr != nil {
		t.Fatalf("whole no-seek baseline failed: command=%T result=%T server=%T stage=%s code=%s; inspect retained actual frames", commandErr, readErr, finalErr, value.FailureStage, value.FailureCode)
	}
	if value.Marker != "goby-generated-whole-browser-result-v1" || value.RunId != runID || !value.Complete || !value.NoSeekQualityVerified || value.FiniteGraphUsed || value.AVQualified || value.OriginalEmbyClientUsed || value.DefaultEnabled ||
		value.QualitySeekFlow != "whole_producer_no_seek_quality_baseline" || value.PageErrors != 0 || value.FatalErrors != 0 || value.ForeignRequests != 0 || value.ClientStopLoadUsed {
		t.Fatal("whole baseline did not preserve its original no-seek client scope")
	}
	checks := []string{"Initial90", "Seek91", "QualitySettled91", "ABRSwitch", "StandardReports", "StoppedResources"}
	if len(value.Checks) != len(checks) {
		t.Fatal("whole baseline changed its required checks")
	}
	for _, check := range checks {
		if !value.Checks[check] {
			t.Fatalf("whole baseline check incomplete: %s", check)
		}
	}
	generatedWholeBrowserAssertFrames(t, value, graph.session.key.plan)
	for _, variant := range []int{0, 1} {
		found := false
		for _, request := range trace {
			found = found || request.Kind == "exact" && request.Variant == variant && request.Number == 15 && request.Status == 200
		}
		if !found {
			t.Fatal("whole baseline actual quality lacks independent direct exact media")
		}
	}
	if len(final.Jobs) != 1 || final.Jobs[0].StartTicks != 0 || final.Jobs[0].RequireInputEvidence || final.GraphPublished || !final.Closed || final.Pending || final.PlayState != "Stopped" {
		t.Fatal("whole baseline did not retain its one original producer and retire the playback registration")
	}
	driver["Complete"], driver["Checks"] = true, value.Checks
	t.Log("whole_producer_no_seek_quality_baseline_verified=true actual_packet_frames_per_rendition=2400 actual_packet_span_seconds=100 native_clock_version=0 start_seconds=0 renditions=4 gop_seconds=6 original_emby_client_used=false finite_graph_used=false")
}
