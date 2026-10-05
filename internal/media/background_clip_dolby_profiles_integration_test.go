//go:build linux

package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These opt-in tests require admitted remote GPU execution. The analytic
// reference is authored by an independent scalar RPU evaluator, then encoded
// as ordinary PQ. Only the final, shared SDR display mapping uses libplacebo.
type backgroundDolbyProfilesManifest struct {
	Width    int                                      `json:"width"`
	Height   int                                      `json:"height"`
	FPS      int                                      `json:"fps"`
	Seconds  int                                      `json:"seconds"`
	Profiles map[string]backgroundDolbyProfileFixture `json:"profiles"`
}

type backgroundDolbyProfileFixture struct {
	Movie          string            `json:"movie"`
	Reference      string            `json:"reference"`
	ReferenceMovie string            `json:"reference_movie"`
	Swatches       string            `json:"swatches"`
	SHA256         map[string]string `json:"sha256"`
	Corruptions    []struct {
		Kind   string `json:"kind"`
		File   string `json:"file"`
		Frame  int    `json:"frame"`
		SHA256 string `json:"sha256"`
	} `json:"corruptions"`
}

type backgroundDolbyProfileSwatch struct {
	Index      int        `json:"index"`
	CenterX    float64    `json:"center_x"`
	CenterY    float64    `json:"center_y"`
	ExpectedPQ [3]float64 `json:"decoded_expected_bt2020_pq_rgb"`
}

func backgroundDolbyProfilesFixture(t *testing.T) (string, backgroundDolbyProfileFixture) {
	t.Helper()
	profile := os.Getenv("GOBY_TEST_BACKGROUND_DOLBY_PROFILE")
	manifestPath := os.Getenv("GOBY_TEST_BACKGROUND_DOLBY_ORACLE_FILE")
	if profile == "" && manifestPath == "" {
		t.Skip("set the new Dolby Vision profile and independent oracle manifest to enable this remote test")
	}
	key := map[string]string{"5": "5", "84": "8.4", "82": "8.2"}[profile]
	if key == "" || manifestPath == "" {
		t.Fatal("the profile must be 5, 84, or 82 and the oracle manifest must be supplied")
	}
	var manifest backgroundDolbyProfilesManifest
	backgroundDolbyReadJSON(t, manifestPath, &manifest)
	fixture, ok := manifest.Profiles[key]
	if !ok || manifest.Width != 320 || manifest.Height != 180 || manifest.FPS != 24 || manifest.Seconds != 28 {
		t.Fatal("the profile requires a complete 28-second, 320x180, 24 fps analytic fixture")
	}
	if os.Getenv("GOBY_TEST_DOLBY_VISION_FILE") != fixture.Movie {
		t.Fatal("the authorized input must match the manifest's selected profile source")
	}
	return profile, fixture
}

func backgroundDolbyProfilesSetup(t *testing.T) (context.Context, AnalysisExtractor, *os.File, Info, Stream, BackgroundClipOptions, string, backgroundDolbyProfileFixture) {
	t.Helper()
	profile, fixture := backgroundDolbyProfilesFixture(t)
	ctx, extractor, file, info, video, options := backgroundClipActualDolbyVisionSetup(t, fixture.Movie)
	backgroundDolbyCheckFile(t, file, fixture.SHA256[filepath.Base(fixture.Movie)])
	options.DolbyVision.AllowProfile5 = true
	options.DolbyVision.AllowProfile84 = true
	options.DolbyVision.AllowProfile82 = true
	wantProfile, wantCompatibility := 8, 2
	if profile == "5" {
		wantProfile, wantCompatibility = 5, 0
	} else if profile == "84" {
		wantCompatibility = 4
	}
	if !DolbyVisionConversionSupported(video) || video.DolbyVision.Profile != wantProfile || video.DolbyVision.CompatibilityID != wantCompatibility ||
		video.Width != 320 || video.Height != 180 || video.PixelFormat != "yuv420p10le" || video.AverageFrameRate != "24/1" ||
		info.DurationTicks != 28*TicksPerSecond || video.DolbyVision.RPUFrameCount != 672 || video.DolbyVision.ELPresent {
		t.Fatalf("the source does not carry the required complete native profile evidence: %+v, %+v", info, video)
	}
	if profile == "5" {
		// IPTPQc2 has no ordinary YCbCr transfer/matrix tags. In particular, it
		// must not be admitted by rewriting this fixture's pixels as HDR10.
		unknown := func(s string) bool { return s == "" || s == "unknown" || s == "unspecified" }
		if video.ColorRange != "pc" || !unknown(video.ColorTransfer) || !unknown(video.ColorSpace) || !unknown(video.ColorPrimaries) {
			t.Fatalf("P5 lost its native full-range IPTPQc2 declaration: %+v", video)
		}
	} else {
		primaries, matrix, transfer := "bt709", "bt709", "bt709"
		if profile == "84" {
			primaries, matrix, transfer = "bt2020", "bt2020nc", "arib-std-b67"
		}
		if video.ColorRange != "tv" || video.ColorPrimaries != primaries || video.ColorSpace != matrix || video.ColorTransfer != transfer {
			t.Fatalf("the native compatibility transfer or range was relabeled: %+v", video)
		}
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return ctx, extractor, file, info, video, options, profile, fixture
}

func TestBackgroundClipActualDolbyProfilesDefaultWindow(t *testing.T) {
	ctx, extractor, file, info, video, options, profile, _ := backgroundDolbyProfilesSetup(t)
	var encoded bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, TicksPerSecond, 25*TicksPerSecond, options, &encoded)
	if err != nil {
		t.Fatal(err)
	}
	backgroundDolbyCheckSummary(t, summary, encoded.Len(), TicksPerSecond, 25*TicksPerSecond)
	backgroundDolbyCheckOffset(t, file)
	backgroundDolbyCheckOutput(t, ctx, extractor, encoded.Bytes(), 600)
	// The fixture alternates its bottom 12 rows every second. At the requested
	// one-second seek they equal swatch 13, while the unseeked source equals
	// swatch 0. Check actual pixels instead of trusting only summary.StartTicks.
	pixels := backgroundClipActualFirstRGB(t, ctx, extractor, encoded.Bytes())
	bar := backgroundDolbySwatchMean(pixels, backgroundDolbyProfileSwatch{CenterX: 160, CenterY: 174})
	bright := backgroundDolbySwatchMean(pixels, backgroundDolbyProfileSwatch{CenterX: 120, CenterY: 157.5})
	dark := backgroundDolbySwatchMean(pixels, backgroundDolbyProfileSwatch{CenterX: 40, CenterY: 22.5})
	for channel := range bar {
		if math.Abs(bar[channel]-bright[channel]) > 8 || bar[channel]-dark[channel] < 30 {
			t.Fatalf("the encoded first picture did not preserve the nonzero source seek: bar=%v, bright=%v, dark=%v", bar, bright, dark)
		}
	}
	backgroundDolbySaveEvidence(t, profile, "default-window.mp4", encoded.Bytes())
}

func TestBackgroundClipActualDolbyProfilesColors(t *testing.T) {
	ctx, extractor, file, info, video, options, profile, fixture := backgroundDolbyProfilesSetup(t)
	const start, duration = TicksPerSecond / 2, 3 * TicksPerSecond
	var actual bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, start, duration, options, &actual)
	if err != nil {
		t.Fatal(err)
	}
	backgroundDolbyCheckSummary(t, summary, actual.Len(), start, duration)
	backgroundDolbyCheckOffset(t, file)
	reference := backgroundDolbyOpenFixture(t, fixture.ReferenceMovie, fixture.SHA256[filepath.Base(fixture.ReferenceMovie)])
	referenceInfo, err := (Prober{FFprobePath: extractor.FFprobePath, FFmpegPath: extractor.FFmpegPath, Timeout: 30 * time.Second}).ProbeFile(ctx, reference)
	if err != nil || len(referenceInfo.Streams) != 1 {
		t.Fatalf("independent PQ reference probe: %+v, %v", referenceInfo, err)
	}
	referenceVideo := referenceInfo.Streams[0]
	if referenceVideo.DolbyVision != nil || referenceVideo.ColorTransfer != "smpte2084" || referenceVideo.ColorSpace != "bt2020nc" ||
		referenceVideo.ColorPrimaries != "bt2020" || referenceVideo.ColorRange != "tv" || referenceVideo.Width != 320 || referenceVideo.Height != 180 {
		t.Fatalf("the scalar oracle must be encoded as ordinary BT.2020 PQ: %+v", referenceVideo)
	}
	limits := DefaultAnalysisLimits()
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, start, duration, options, limits)
	if err != nil {
		t.Fatal(err)
	}
	// The scalar oracle has already applied the DV reshape and color matrices.
	// Only the common BT.2390 SDR display mapping is shared with production.
	plan.colorFilter = "libplacebo=format=yuv420p:colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv:apply_dolbyvision=0:tonemapping=bt.2390:peak_detect=0,format=yuv420p"
	control, err := backgroundDolbyRunEncode(ctx, extractor, reference, referenceVideo, plan, limits)
	if err != nil {
		t.Fatalf("ordinary PQ display mapping failed: %v", err)
	}
	backgroundDolbyCheckOutput(t, ctx, extractor, actual.Bytes(), 72)
	backgroundDolbyCheckOutput(t, ctx, extractor, control, 72)
	swatchFile := backgroundDolbyOpenFixture(t, fixture.Swatches, fixture.SHA256[filepath.Base(fixture.Swatches)])
	var swatches []backgroundDolbyProfileSwatch
	if err := json.NewDecoder(io.NewSectionReader(swatchFile, 0, 64<<10)).Decode(&swatches); err != nil || len(swatches) != 16 {
		t.Fatalf("the independent oracle must contain 16 swatches: %v", err)
	}
	// Bind each named scalar result to the raw oracle before using its encoded
	// display reference. This catches a stale or differently ordered reference.
	rawOracle := backgroundDolbyOpenFixture(t, fixture.Reference, fixture.SHA256[filepath.Base(fixture.Reference)])
	for i, swatch := range swatches {
		if swatch.Index != i || swatch.CenterX < 4 || swatch.CenterX > 315 || swatch.CenterY < 4 || swatch.CenterY > 175 {
			t.Fatalf("invalid scalar swatch: %+v", swatch)
		}
		for c, plane := range []int{2, 0, 1} {
			var data [4]byte
			offset := int64((plane*320*180 + int(swatch.CenterY)*320 + int(swatch.CenterX)) * 4)
			if _, err := rawOracle.ReadAt(data[:], offset); err != nil || math.Abs(float64(math.Float32frombits(binary.LittleEndian.Uint32(data[:])))-swatch.ExpectedPQ[c]) > .000001 {
				t.Fatalf("raw oracle differs from independent scalar swatch %d channel %d: %v", i, c, err)
			}
		}
	}
	got := backgroundClipActualFirstRGB(t, ctx, extractor, actual.Bytes())
	want := backgroundClipActualFirstRGB(t, ctx, extractor, control)
	type colorEvidence struct {
		Index    int        `json:"index"`
		Actual   [3]float64 `json:"actual_rgb"`
		Oracle   [3]float64 `json:"oracle_rgb"`
		MaxError float64    `json:"max_channel_error"`
	}
	var evidence []colorEvidence
	var sumError, maxError float64
	for _, swatch := range swatches {
		actualRGB, oracleRGB := backgroundDolbySwatchMean(got, swatch), backgroundDolbySwatchMean(want, swatch)
		var swatchError float64
		for channel := range actualRGB {
			delta := math.Abs(actualRGB[channel] - oracleRGB[channel])
			sumError += delta
			swatchError = max(swatchError, delta)
		}
		maxError = max(maxError, swatchError)
		evidence = append(evidence, colorEvidence{swatch.Index, actualRGB, oracleRGB, swatchError})
	}
	meanError := sumError / 48
	t.Logf("independent scalar oracle SDR swatches: mean channel error %.4f, maximum %.4f, values %+v", meanError, maxError, evidence)
	// Fixed limits cover two finite lossy video encodes and 10-bit reference
	// quantization while remaining sensitive to wrong transfer, range or hue.
	if maxError > 8 || meanError > 3 {
		t.Fatalf("native DV colors differ from the independent scalar oracle: mean %.4f, maximum %.4f", meanError, maxError)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	backgroundDolbySaveEvidence(t, profile, "colors.json", data)
	backgroundDolbySaveEvidence(t, profile, "colors.mp4", actual.Bytes())
	backgroundDolbySaveEvidence(t, profile, "oracle-sdr.mp4", control)
}

func TestBackgroundClipActualDolbyProfilesRuntimeRejection(t *testing.T) {
	ctx, extractor, file, info, video, options, _, fixture := backgroundDolbyProfilesSetup(t)
	limits := DefaultAnalysisLimits()
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, corruption := range fixture.Corruptions {
		if corruption.Kind != "missing-rpu" && corruption.Kind != "bad-crc" || seen[corruption.Kind] || corruption.Frame != 36 {
			t.Fatalf("unexpected runtime corruption fixture: %+v", corruption)
		}
		seen[corruption.Kind] = true
		t.Run(corruption.Kind, func(t *testing.T) {
			bad := backgroundDolbyOpenFixture(t, corruption.File, corruption.SHA256)
			stat, err := bad.Stat()
			if err != nil {
				t.Fatal(err)
			}
			// Preserve genuinely verified RPU evidence from the admitted source
			// while injecting a later bad frame. This must reach the renderer,
			// rather than merely exercise the scan-time unsupported gate.
			admitted := info
			admitted.Size, admitted.FileChangeTimeNs = stat.Size(), FileChangeTime(stat)
			backgroundDolbyRequireRuntimeRejection(t, ctx, extractor, bad, admitted, video, geometry, options, limits)
		})
	}
	if len(seen) != 2 {
		t.Fatal("both a missing per-frame RPU and a bad CRC are required")
	}
	t.Run("profile-mismatch", func(t *testing.T) {
		mismatch := video
		dv := *video.DolbyVision
		if dv.Profile == 5 {
			dv.Profile, dv.RPUProfile, dv.CompatibilityID = 8, 8, 4
		} else {
			dv.Profile, dv.RPUProfile, dv.CompatibilityID = 5, 5, 0
		}
		mismatch.DolbyVision = &dv
		admitted := info
		admitted.Streams = slices.Clone(info.Streams)
		for i := range admitted.Streams {
			if admitted.Streams[i].Index == video.Index {
				admitted.Streams[i] = mismatch
			}
		}
		backgroundDolbyRequireRuntimeRejection(t, ctx, extractor, file, admitted, mismatch, geometry, options, limits)
	})
}

func backgroundDolbyRequireRuntimeRejection(t *testing.T, ctx context.Context, extractor AnalysisExtractor, file *os.File, info Info, video Stream, geometry analysisDisplayGeometry, options BackgroundClipOptions, limits AnalysisLimits) {
	t.Helper()
	if !DolbyVisionConversionSupported(video) {
		t.Fatal("runtime negative control was incorrectly rejected at the metadata gate")
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, TicksPerSecond/2, 3*TicksPerSecond, options, limits)
	if err != nil {
		t.Fatalf("runtime negative control failed before the renderer: %v", err)
	}
	_, err = backgroundDolbyRunEncode(ctx, extractor, file, video, plan, limits)
	var exited *exec.ExitError
	if !errors.As(err, &exited) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrProcessRetirementUnknown) {
		t.Fatalf("the real decoder/strict renderer did not reject the negative control: %v", err)
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var published bytes.Buffer
	summary, err := extractor.GenerateBackgroundClipWithOptions(ctx, file, info, video.Index, TicksPerSecond/2, 3*TicksPerSecond, options, &published)
	exited = nil
	if !errors.As(err, &exited) || summary != (BackgroundClipSummary{}) || published.Len() != 0 || errors.Is(err, ErrBackgroundClipUnsupported) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrProcessRetirementUnknown) {
		t.Fatalf("runtime rejection published output or only used the policy gate: %+v, %d bytes, %v", summary, published.Len(), err)
	}
	backgroundDolbyCheckOffset(t, file)
}

func TestBackgroundClipActualDolbyProfilesCancellation(t *testing.T) {
	ctx, extractor, file, info, video, options, _, _ := backgroundDolbyProfilesSetup(t)
	limits := DefaultAnalysisLimits()
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, TicksPerSecond, 10*TicksPerSecond, options, limits)
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
		t.Fatal("missing production input seek")
	}
	args = slices.Insert(args, input, "-readrate", "1")
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	started := false
	var cancellationTime time.Time
	err = runBackgroundClipProcess(work, "/proc/self/fd/4", file, args, 30*time.Second, MaxBackgroundClipBytes, &analysisDiscardStderr{}, func(reader io.Reader) error {
		// An initialization header alone does not prove Vulkan rendered a
		// picture. Wait for an encoded media payload before cancellation.
		for boxes := 0; boxes < 16; boxes++ {
			var header [8]byte
			if _, err := io.ReadFull(reader, header[:]); err != nil {
				return err
			}
			size := int64(binary.BigEndian.Uint32(header[:4]))
			if size < 8 || size > MaxBackgroundClipBytes {
				return fmt.Errorf("unexpected test MP4 box size %d", size)
			}
			if string(header[4:]) == "mdat" && size >= 40 {
				if _, err := io.CopyN(io.Discard, reader, 32); err != nil {
					return err
				}
				started = true
				cancellationTime = time.Now()
				cancel()
				_, err := io.Copy(io.Discard, reader)
				return err
			}
			if _, err := io.CopyN(io.Discard, reader, size-8); err != nil {
				return err
			}
		}
		return errors.New("no rendered media fragment appeared before the cancellation bound")
	}, true, tool.file)
	if !started || !errors.Is(err, context.Canceled) || errors.Is(err, ErrProcessRetirementUnknown) ||
		cancellationTime.IsZero() || time.Since(cancellationTime) > 3*time.Second {
		t.Fatalf("active GPU cancellation did not retire through the owner within three seconds: rendered=%t, elapsed=%v, %v", started, time.Since(cancellationTime), err)
	}
	backgroundDolbyCheckOffset(t, file)
}

func backgroundDolbyRunEncode(ctx context.Context, extractor AnalysisExtractor, file *os.File, video Stream, plan backgroundClipPlan, limits AnalysisLimits) ([]byte, error) {
	tool, err := analysisOpenToolExpected(ctx, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return nil, err
	}
	defer tool.file.Close()
	var encoded bytes.Buffer
	sink := &analysisDiscardStderr{}
	err = runBackgroundClipProcess(ctx, "/proc/self/fd/4", file, backgroundClipEncodeArgs(video, plan, limits), 2*time.Minute, MaxBackgroundClipBytes, sink,
		func(reader io.Reader) error { _, err := io.Copy(&encoded, reader); return err }, true, tool.file)
	return encoded.Bytes(), errors.Join(err, sink.failure(), tool.check())
}

func backgroundDolbyCheckSummary(t *testing.T, summary BackgroundClipSummary, size int, start, duration int64) {
	t.Helper()
	wantProfile := BackgroundClipProfile + ";max_width=1280;bitrate=1500000;dolby_vision=strict-sdr-v1"
	if summary.Profile != wantProfile || summary.StartTicks != start || summary.DurationTicks != duration || summary.Width != 1280 || summary.Height != 720 ||
		summary.Bytes != int64(size) || size <= 0 || int64(size) > MaxBackgroundClipBytes || !analysisValidSHA256(summary.FFmpegSHA256) || !analysisValidSHA256(summary.FFprobeSHA256) {
		t.Fatalf("the strict finite background output contract changed: %+v", summary)
	}
}

func backgroundDolbyCheckOffset(t *testing.T, file *os.File) {
	t.Helper()
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
		t.Fatalf("the borrowed source descriptor offset changed: %d, %v", offset, err)
	}
}

func backgroundDolbyCheckOutput(t *testing.T, ctx context.Context, extractor AnalysisExtractor, encoded []byte, frames int) {
	t.Helper()
	tool, err := analysisOpenToolExpected(ctx, extractor.FFprobePath, extractor.ExpectedFFprobeSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	var data bytes.Buffer
	sink := &analysisDiscardStderr{}
	args := []string{"-v", "error", "-threads", "1", "-f", "mp4", "-count_frames", "-show_frames", "-show_streams", "-show_format",
		"-show_entries", "stream=index,codec_name,codec_type,pix_fmt,width,height,time_base,avg_frame_rate,r_frame_rate,color_range,color_space,color_transfer,color_primaries,nb_read_frames:stream_side_data=side_data_type:frame=media_type:frame_side_data=side_data_type:format=format_name", "-of", "json", "-i", "pipe:0"}
	err = runAnalysisProcess(ctx, "/proc/self/fd/3", nil, bytes.NewReader(encoded), args, time.Minute, 1<<20, sink,
		func(reader io.Reader) error { _, err := io.Copy(&data, reader); return err }, tool.file)
	if err = errors.Join(err, sink.failure()); err != nil {
		t.Fatalf("full output frame inspection failed: %v", err)
	}
	var doc struct {
		Streams []struct {
			Codec     string `json:"codec_name"`
			Type      string `json:"codec_type"`
			Pixel     string `json:"pix_fmt"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Frames    string `json:"nb_read_frames"`
			Rate      string `json:"avg_frame_rate"`
			Range     string `json:"color_range"`
			Matrix    string `json:"color_space"`
			Transfer  string `json:"color_transfer"`
			Primaries string `json:"color_primaries"`
		} `json:"streams"`
		Frames []json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(data.Bytes(), &doc); err != nil || len(doc.Streams) != 1 || len(doc.Frames) != frames {
		t.Fatalf("unexpected decoded output stream/frame count: %d/%d, %v", len(doc.Streams), len(doc.Frames), err)
	}
	s := doc.Streams[0]
	if s.Type != "video" || s.Codec != "h264" || s.Pixel != "yuv420p" || s.Width != 1280 || s.Height != 720 || s.Frames != strconv.Itoa(frames) ||
		s.Rate != "24/1" || s.Range != "tv" || s.Matrix != "bt709" || s.Transfer != "bt709" || s.Primaries != "bt709" {
		t.Fatalf("decoded output is not silent 8-bit limited BT.709 H.264: %+v", s)
	}
	for _, forbidden := range []string{"DOVI", "Dolby Vision", "Mastering display metadata", "Content light level metadata", "SMPTE2094", "HDR Dynamic"} {
		if bytes.Contains(data.Bytes(), []byte(forbidden)) {
			t.Fatalf("encoded output retained HDR side data: %s", forbidden)
		}
	}
}

func backgroundDolbySwatchMean(pixels []byte, swatch backgroundDolbyProfileSwatch) [3]float64 {
	var mean [3]float64
	for y := int(swatch.CenterY) - 4; y < int(swatch.CenterY)+4; y++ {
		for x := int(swatch.CenterX) - 4; x < int(swatch.CenterX)+4; x++ {
			for c := range mean {
				mean[c] += float64(pixels[(y*320+x)*3+c]) / 64
			}
		}
	}
	return mean
}

func backgroundDolbyReadJSON(t *testing.T, path string, output any) {
	t.Helper()
	if !filepath.IsAbs(path) {
		t.Fatal("the fixture manifest path must be absolute")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 256<<10 {
		t.Fatalf("fixture JSON could not be read within its bound: %v", err)
	}
	if _, err := mediaEditDecodeJSON(data); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatal(err)
	}
}

func backgroundDolbyOpenFixture(t *testing.T, path, expectedSHA string) *os.File {
	t.Helper()
	if !filepath.IsAbs(path) {
		t.Fatal("fixture paths must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	backgroundDolbyCheckFile(t, file, expectedSHA)
	return file
}

func backgroundDolbyCheckFile(t *testing.T, file *os.File, expectedSHA string) {
	t.Helper()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 2<<30 || !analysisValidSHA256(expectedSHA) {
		t.Fatalf("fixture identity or file size is invalid: %v", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(file, 0, stat.Size())); err != nil || hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		t.Fatalf("fixture SHA-256 differs from its independently recorded provenance: %s, %v", file.Name(), err)
	}
}

func backgroundDolbySaveEvidence(t *testing.T, profile, suffix string, data []byte) {
	t.Helper()
	directory := os.Getenv("GOBY_TEST_BACKGROUND_OUTPUT_DIR")
	if directory == "" {
		return
	}
	if !filepath.IsAbs(directory) || !slices.Contains([]string{"5", "84", "82"}, profile) || strings.ContainsAny(suffix, "/\\") {
		t.Fatal("invalid background evidence location")
	}
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("evidence directory must already exist without a symlink: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != filepath.Clean(directory) {
		t.Fatalf("evidence directory traverses a symlink: %v", err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		t.Fatal("evidence directory changed while opening")
	}
	output, err := root.OpenFile("profile"+profile+"-"+suffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := output.Write(data)
	if err := errors.Join(writeErr, output.Sync(), output.Close()); err != nil {
		t.Fatal(err)
	}
}
