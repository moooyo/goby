//go:build linux

package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func backgroundClipActualDolbyVisionSetup(t *testing.T, source string) (context.Context, AnalysisExtractor, *os.File, Info, Stream, BackgroundClipOptions) {
	t.Helper()
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if source == "" || device == "" {
		t.Skip("a complete Dolby Vision fixture and an admitted remote Vulkan device are required")
	}
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Limits: AnalysisLimits{Timeout: 2 * time.Minute}}
	capability, err := extractor.BackgroundClipDolbyVisionAvailability(ctx, device)
	if err != nil || !capability.Available || !analysisValidSHA256(capability.FFmpegSHA256) || !analysisValidSHA256(capability.FFprobeSHA256) {
		t.Fatalf("actual DV execution dependencies unavailable: %+v, %v", capability, err)
	}
	extractor.ExpectedFFmpegSHA256, extractor.ExpectedFFprobeSHA256 = capability.FFmpegSHA256, capability.FFprobeSHA256
	file, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	var video Stream
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			video = stream
			break
		}
	}
	return ctx, extractor, file, info, video, BackgroundClipOptions{DolbyVision: &BackgroundClipDolbyVisionOptions{Device: device}}
}

func TestBackgroundClipActualDolbyVision(t *testing.T) {
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, os.Getenv("GOBY_TEST_DOLBY_VISION_FILE"))
	if !DolbyVisionConversionSupported(video) || info.DurationTicks < 4*TicksPerSecond || info.DurationTicks > 10*TicksPerSecond || video.DolbyVision.Profile == 5 {
		t.Fatalf("a complete four-to-ten-second profile 8.1 or profile 7 MEL fixture is required: %+v", video)
	}
	const start, duration = TicksPerSecond / 2, 3 * TicksPerSecond
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, start, duration, options, &encoded)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Width != 1280 || summary.Height != 720 || summary.DurationTicks != duration || summary.StartTicks != start ||
		summary.Bytes != int64(encoded.Len()) || !strings.Contains(summary.Profile, "dolby_vision=strict-sdr-v1") {
		t.Fatalf("DV output did not retain the finite background contract: %+v", summary)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
		t.Fatalf("generation moved the authorized source descriptor: %d, %v", offset, err)
	}
	limits := DefaultAnalysisLimits()
	limits.Timeout = 2 * time.Minute
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, start, duration, options, limits)
	if err != nil {
		t.Fatal(err)
	}
	// This control is test-only. Production has no switch that silently drops
	// the RPU. It uses identical source, device, timing and output encoding.
	plan.colorFilter = "setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc," +
		"libplacebo=format=yuv420p:colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv:apply_dolbyvision=0:tonemapping=bt.2390:peak_detect=0,format=yuv420p"
	tool, err := analysisOpenToolExpected(ctx, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	var control bytes.Buffer
	sink := &analysisDiscardStderr{}
	err = runBackgroundClipProcess(ctx, "/proc/self/fd/4", file, backgroundClipEncodeArgs(video, plan, limits), limits.Timeout, MaxBackgroundClipBytes, sink,
		func(reader io.Reader) error { _, err := io.Copy(&control, reader); return err }, true, tool.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil {
		t.Fatalf("RPU-disabled negative control did not execute: %v", err)
	}
	before := backgroundClipActualFirstRGB(t, ctx, extractor, control.Bytes())
	after := backgroundClipActualFirstRGB(t, ctx, extractor, encoded.Bytes())
	difference := 0.0
	for index := range after {
		difference += math.Abs(float64(after[index]) - float64(before[index]))
	}
	if difference/float64(len(after)) < .1 {
		t.Fatal("non-identity RPU did not change actual encoded background pixels")
	}
	if os.Getenv("GOBY_TEST_DOLBY_VISION_CHART") == "1" {
		backgroundClipActualNeutralChart(t, before, after)
	}
	backgroundClipSaveActualOutput(t, video.DolbyVision.Profile, encoded.Bytes())
}

func TestBackgroundClipActualDolbyVisionDefaultWindow(t *testing.T) {
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, os.Getenv("GOBY_TEST_BACKGROUND_DOLBY_VISION_25S_FILE"))
	if !DolbyVisionConversionSupported(video) || info.DurationTicks < 26*TicksPerSecond || video.DolbyVision.Profile == 5 {
		t.Fatalf("a complete at-least-26-second accepted DV fixture is required: %+v", video)
	}
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, TicksPerSecond, 25*TicksPerSecond, options, &encoded)
	if err != nil || summary.DurationTicks != 25*TicksPerSecond || summary.Width != 1280 || summary.Height != 720 || summary.Bytes != int64(encoded.Len()) {
		t.Fatalf("default 25-second DV background failed: %+v, %v", summary, err)
	}
	backgroundClipSaveActualOutput(t, video.DolbyVision.Profile, encoded.Bytes())
}

// Optional evidence is copied from the exact output of the production Go
// generator after all test assertions pass. It never invokes another encoder.
func backgroundClipSaveActualOutput(t *testing.T, profile int, encoded []byte) {
	t.Helper()
	directory := os.Getenv("GOBY_TEST_BACKGROUND_OUTPUT_DIR")
	if directory == "" {
		return
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("background evidence directory must be absolute")
	}
	directory = filepath.Clean(directory)
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("background evidence directory must already exist without a symlink: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		t.Fatalf("background evidence directory must not traverse a symlink: %v", err)
	}
	var basename string
	switch t.Name() {
	case "TestBackgroundClipActualDolbyVision", "TestBackgroundClipActualDolbyVisionDefaultWindow":
		basename = t.Name() + "-profile" + strconv.Itoa(profile) + ".mp4"
	default:
		t.Fatal("this test has no background evidence filename")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		t.Fatalf("background evidence directory changed while opening: %v", err)
	}
	output, err := root.OpenFile(basename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatalf("background evidence file must not already exist: %v", err)
	}
	_, writeErr := io.Copy(output, bytes.NewReader(encoded))
	syncErr := output.Sync()
	closeErr := output.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		t.Fatalf("could not persist the actual generated background: %v", err)
	}
	t.Logf("saved actual generated background: %s (%d bytes)", filepath.Join(directory, basename), len(encoded))
}

func TestBackgroundClipActualDolbyVisionRejectsFELBeforePublication(t *testing.T) {
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, os.Getenv("GOBY_TEST_DOLBY_VISION_FEL_FILE"))
	if video.DolbyVision == nil || video.DolbyVision.Profile != 7 || info.DurationTicks < TicksPerSecond {
		t.Fatalf("a complete profile 7 nonzero-residual fixture is required: %+v", video)
	}
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, 0, TicksPerSecond, options, &encoded)
	if err == nil || summary.Bytes != 0 || encoded.Len() != 0 {
		t.Fatalf("unsupported residual published a background: %+v, %d, %v", summary, encoded.Len(), err)
	}
}

func TestBackgroundClipActualDolbyVisionRejectsUnverifiedRPU(t *testing.T) {
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, os.Getenv("GOBY_TEST_DOLBY_VISION_BAD_SYNTAX_FILE"))
	if video.DolbyVision == nil || video.DolbyVision.RPUVerified {
		t.Fatalf("a configured but unverified RPU fixture is required: %+v", video)
	}
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, 0, TicksPerSecond, options, &encoded)
	if !errors.Is(err, ErrBackgroundClipUnsupported) || summary.Bytes != 0 || encoded.Len() != 0 {
		t.Fatalf("unverified RPU published a background: %+v, %d, %v", summary, encoded.Len(), err)
	}
}

func TestBackgroundClipActualDolbyVisionCancellation(t *testing.T) {
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, os.Getenv("GOBY_TEST_DOLBY_VISION_FILE"))
	if !DolbyVisionConversionSupported(video) || video.DolbyVision.Profile == 5 || info.DurationTicks < 4*TicksPerSecond {
		t.Fatalf("a complete accepted DV cancellation fixture is required: %+v", video)
	}
	limits := DefaultAnalysisLimits()
	limits.Timeout = 30 * time.Second
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, 0, 3*TicksPerSecond, options, limits)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := analysisOpenToolExpected(ctx, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	args := backgroundClipEncodeArgs(video, plan, limits)
	input := slices.Index(args, "-ss")
	if input < 0 {
		t.Fatal("production DV input arguments are missing")
	}
	// Pace only the test input so cancellation happens while the real Vulkan
	// renderer is active; the production graph and process owner stay intact.
	args = slices.Insert(args, input, "-readrate", "1")
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	started := false
	sink := &analysisDiscardStderr{}
	err = runBackgroundClipProcess(work, "/proc/self/fd/4", file, args, limits.Timeout, MaxBackgroundClipBytes, sink,
		func(reader io.Reader) error {
			var prefix [32]byte
			if _, err := io.ReadFull(reader, prefix[:]); err != nil {
				return err
			}
			started = true
			cancel()
			_, err := io.Copy(io.Discard, reader)
			return err
		}, true, tool.file)
	if !started || !errors.Is(err, context.Canceled) {
		t.Fatalf("real DV cancellation did not retire through its process owner: started=%t, %v", started, err)
	}
}

func backgroundClipActualFirstRGB(t *testing.T, ctx context.Context, extractor AnalysisExtractor, encoded []byte) []byte {
	t.Helper()
	tool, err := analysisOpenToolExpected(ctx, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	const expected = 320 * 180 * 3
	var pixels bytes.Buffer
	sink := &analysisDiscardStderr{}
	args := []string{"-hide_banner", "-loglevel", "error", "-xerror", "-nostdin", "-threads", "1", "-filter_threads", "1",
		"-f", "mp4", "-i", "pipe:0", "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", "scale=320:180:flags=area,format=rgb24",
		"-frames:v", "1", "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"}
	err = runAnalysisProcess(ctx, "/proc/self/fd/3", nil, bytes.NewReader(encoded), args, 30*time.Second, expected, sink,
		func(reader io.Reader) error { _, err := io.Copy(&pixels, reader); return err }, tool.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil || pixels.Len() != expected {
		t.Fatalf("background RGB decode failed: %d bytes, %v", pixels.Len(), err)
	}
	return pixels.Bytes()
}

func backgroundClipActualNeutralChart(t *testing.T, before, after []byte) {
	t.Helper()
	means := func(pixels []byte) [8][3]float64 {
		var result [8][3]float64
		for stripe := range result {
			for y := 28; y < 36; y++ {
				for x := stripe*40 + 16; x < stripe*40+24; x++ {
					for channel := range result[stripe] {
						result[stripe][channel] += float64(pixels[(y*320+x)*3+channel]) / 64
					}
				}
			}
		}
		return result
	}
	control, converted := means(before), means(after)
	intensity := func(value [3]float64) float64 { return (value[0] + value[1] + value[2]) / 3 }
	for stripe, value := range converted {
		if math.Max(value[0], math.Max(value[1], value[2]))-math.Min(value[0], math.Min(value[1], value[2])) > 12 ||
			stripe > 0 && intensity(value)+3 < intensity(converted[stripe-1]) {
			t.Fatalf("DV background neutral color or brightness order is incorrect: %v", converted)
		}
	}
	if intensity(converted[7])-intensity(converted[0]) < 20 || intensity(converted[0]) <= intensity(control[0])+.5 {
		t.Fatalf("DV background did not retain the authored RPU luma curve: control=%v converted=%v", control, converted)
	}
	t.Logf("background neutral chart RGB: control=%v converted=%v", control, converted)
}
