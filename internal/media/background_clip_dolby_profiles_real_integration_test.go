//go:build linux

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"testing"
)

type backgroundDolbyRealManifest struct {
	Sources map[string]backgroundDolbyRealSource `json:"real_sources"`
}

type backgroundDolbyRealSource struct {
	Movie          string `json:"movie"`
	SHA256         string `json:"sha256"`
	StartTicks     int64  `json:"start_ticks"`
	ColorRange     string `json:"color_range"`
	ColorSpace     string `json:"color_space"`
	ColorTransfer  string `json:"color_transfer"`
	ColorPrimaries string `json:"color_primaries"`
}

func backgroundDolbyRealSources(t *testing.T) (string, map[string]backgroundDolbyRealSource) {
	t.Helper()
	profile, path := os.Getenv("GOBY_TEST_BACKGROUND_DOLBY_PROFILE"), os.Getenv("GOBY_TEST_BACKGROUND_DOLBY_ORACLE_FILE")
	if profile == "" && path == "" {
		t.Skip("set the admitted real-source manifest to enable official-source acceptance")
	}
	key := map[string]string{"5": "5", "84": "8.4"}[profile]
	if key == "" {
		t.Fatal("official-source acceptance requires profile 5 or 84")
	}
	var manifest backgroundDolbyRealManifest
	backgroundDolbyReadJSON(t, path, &manifest)
	for _, name := range []string{"5", "8.4"} {
		source, ok := manifest.Sources[name]
		if !ok || !analysisValidSHA256(source.SHA256) || source.StartTicks < 25*TicksPerSecond || source.StartTicks > 180*TicksPerSecond {
			t.Fatal("both independently hashed official versions and an explicit non-intro source window are required")
		}
	}
	if manifest.Sources["5"].SHA256 == manifest.Sources["8.4"].SHA256 || manifest.Sources["5"].Movie == manifest.Sources["8.4"].Movie ||
		manifest.Sources["5"].StartTicks != manifest.Sources["8.4"].StartTicks || os.Getenv("GOBY_TEST_DOLBY_VISION_FILE") != manifest.Sources[key].Movie {
		t.Fatal("official versions must be distinct, use the same scene, and match the authorized source")
	}
	return profile, manifest.Sources
}

func backgroundDolbyRealSetup(t *testing.T, profile string, source backgroundDolbyRealSource) (context.Context, AnalysisExtractor, *os.File, Info, Stream, BackgroundClipOptions) {
	t.Helper()
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, source.Movie)
	backgroundDolbyCheckFile(t, file, source.SHA256)
	options.DolbyVision.AllowProfile5 = true
	options.DolbyVision.AllowProfile84 = true
	options.DolbyVision.AllowProfile82 = true
	wantProfile, wantCompatibility := 5, 0
	if profile == "84" {
		wantProfile, wantCompatibility = 8, 4
	}
	normalize := func(s string) string {
		if s == "unknown" || s == "unspecified" {
			return ""
		}
		return s
	}
	if !DolbyVisionConversionSupported(video) || video.DolbyVision.Profile != wantProfile || video.DolbyVision.CompatibilityID != wantCompatibility ||
		video.Width != 1920 || video.Height != 1080 || info.DurationTicks < source.StartTicks+25*TicksPerSecond ||
		video.ColorRange != source.ColorRange || normalize(video.ColorSpace) != normalize(source.ColorSpace) ||
		normalize(video.ColorTransfer) != normalize(source.ColorTransfer) || normalize(video.ColorPrimaries) != normalize(source.ColorPrimaries) {
		t.Fatalf("official source differs from its independently recorded native profile: duration_ticks=%d, video=%+v, dolby_vision=%+v", info.DurationTicks, video, video.DolbyVision)
	}
	if profile == "84" && (video.ColorTransfer != "arib-std-b67" || video.ColorRange != "tv" || video.ColorPrimaries != "bt2020" || video.ColorSpace != "bt2020nc") {
		t.Fatalf("the official P8.4 source is not native HLG: %+v", video)
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return ctx, extractor, file, info, video, options
}

func TestBackgroundClipActualDolbyProfilesRealDefaultWindow(t *testing.T) {
	profile, sources := backgroundDolbyRealSources(t)
	key := map[string]string{"5": "5", "84": "8.4"}[profile]
	source := sources[key]
	ctx, extractor, file, info, video, options := backgroundDolbyRealSetup(t, profile, source)
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, source.StartTicks, 25*TicksPerSecond, options, &encoded)
	if err != nil {
		t.Fatal(err)
	}
	backgroundDolbyCheckSummary(t, summary, encoded.Len(), source.StartTicks, 25*TicksPerSecond)
	backgroundDolbyCheckOffset(t, file)
	backgroundDolbyCheckOutput(t, ctx, extractor, encoded.Bytes(), 600)
	backgroundDolbySaveEvidence(t, profile, "official-default-window.mp4", encoded.Bytes())
}

func TestBackgroundClipActualDolbyProfilesRealConsistency(t *testing.T) {
	_, sources := backgroundDolbyRealSources(t)
	var pictures [2][]byte
	for index, profile := range []string{"5", "84"} {
		key := map[string]string{"5": "5", "84": "8.4"}[profile]
		source := sources[key]
		ctx, extractor, file, info, video, options := backgroundDolbyRealSetup(t, profile, source)
		var encoded bytes.Buffer
		summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, source.StartTicks, 3*TicksPerSecond, options, &encoded)
		if err != nil {
			t.Fatal(err)
		}
		backgroundDolbyCheckSummary(t, summary, encoded.Len(), source.StartTicks, 3*TicksPerSecond)
		backgroundDolbyCheckOffset(t, file)
		backgroundDolbyCheckOutput(t, ctx, extractor, encoded.Bytes(), 72)
		pictures[index] = backgroundClipActualFirstRGB(t, ctx, extractor, encoded.Bytes())
		backgroundDolbySaveEvidence(t, profile, "official-same-scene.mp4", encoded.Bytes())
	}
	// These are separately published Dolby versions of the same scene, not an
	// SDR mastering oracle. Compare bounded RGB differences and spatial luma
	// correlation to detect a gross hue, transfer or scene-alignment failure.
	var totalError, sumA, sumB, sumAA, sumBB, sumAB float64
	const pixels = 320 * 180
	for p := 0; p < pixels; p++ {
		var a, b float64
		for c, weight := range []float64{.2126, .7152, .0722} {
			x, y := float64(pictures[0][3*p+c]), float64(pictures[1][3*p+c])
			totalError += math.Abs(x - y)
			a, b = a+x*weight, b+y*weight
		}
		sumA, sumB = sumA+a, sumB+b
		sumAA, sumBB, sumAB = sumAA+a*a, sumBB+b*b, sumAB+a*b
	}
	varianceA, varianceB := sumAA-sumA*sumA/pixels, sumBB-sumB*sumB/pixels
	if varianceA <= pixels*25 || varianceB <= pixels*25 {
		t.Fatal("official-source comparison requires a spatially varied scene")
	}
	correlation := (sumAB - sumA*sumB/pixels) / math.Sqrt(varianceA*varianceB)
	meanError := totalError / (pixels * 3)
	t.Logf("independently published P5/P8.4 same-scene comparison: RGB MAE %.4f/255, spatial luma correlation %.6f", meanError, correlation)
	if meanError > 12 || correlation < .96 {
		t.Fatalf("official P5/P8.4 outputs disagree: RGB MAE %.4f, luma correlation %.6f", meanError, correlation)
	}
	evidence, err := json.MarshalIndent(map[string]any{"p5_sha256": sources["5"].SHA256, "p84_sha256": sources["8.4"].SHA256,
		"start_ticks": sources["5"].StartTicks, "rgb_mean_absolute_error": meanError, "luma_correlation": correlation,
		"scope": "Independent Dolby version consistency; not an SDR mastering oracle"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	backgroundDolbySaveEvidence(t, "5", "official-consistency.json", evidence)
}
