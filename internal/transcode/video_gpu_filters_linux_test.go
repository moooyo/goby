//go:build linux

package transcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestProgressiveSubtitleBurnActualSeekAndOffsetPixels(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	directory := t.TempDir()
	asset := filepath.Join(directory, "source.ass")
	text := "[Script Info]\nScriptType: v4.00+\nPlayResX: 320\nPlayResY: 192\n[V4+ Styles]\n" +
		"Format: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n" +
		"Style: Default,DejaVu Sans,24,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n" +
		"[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\nDialogue: 0,0:00:02.50,0:00:03.50,Default,,0,0,0,,CAPTION\n"
	if err := os.WriteFile(asset, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "source.mkv")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=black:s=320x192:r=16:d=4.5", "-i", asset, "-map", "0:v", "-map", "1:s",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-c:s", "ass", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, gpu := range []bool{false, true} {
		name := "software"
		if gpu {
			name = "vulkan-vaapi"
		}
		t.Run(name, func(t *testing.T) {
			plan := progressiveVideoFixturePlan(info, "h264", "", 2*ticksPerSecond)
			plan.Width, plan.Height = 320, 192
			plan.Subtitle = SubtitlePlan{Mode: "burn", Codec: "ass", StreamIndex: 1, OffsetTicks: ticksPerSecond / 2}
			if gpu {
				device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
				if device == "" {
					t.Skip("an explicitly selected AMD VAAPI device is required")
				}
				plan.Hardware = Hardware{Decode: "vaapi", Encode: "vaapi", Device: device}
				plan.VideoFilters.Backend = "vulkan"
			}
			output := runSubtitleProgressiveVideo(t, ctx, ffmpeg, source, plan)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, output)
			frameBytes := 320 * 192
			if len(pixels) != 40*frameBytes {
				t.Fatalf("subtitle composition changed the source cadence: %d frames", len(pixels)/frameBytes)
			}
			for frame := 0; frame < 40; frame++ {
				bright := 0
				for _, pixel := range pixels[frame*frameBytes : (frame+1)*frameBytes] {
					if pixel > 160 {
						bright++
					}
				}
				// The original 2.5--3.5 s cue moves to 3--4 s after offset,
				// then appears at 1--2 s in the output trimmed at source 2 s.
				visible := frame >= 16 && frame < 32
				if visible && bright < 40 || !visible && bright > 4 {
					t.Fatalf("caption pixels disagree with the source/seek/offset clock at frame %d: %d bright pixels", frame, bright)
				}
			}
		})
	}
}

func TestAMDVideoProcessingActualHDRAndDeinterlace(t *testing.T) {
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" {
		t.Skip("an explicitly selected AMD VAAPI device is required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, hdr := range []bool{false, true} {
		name, sourceFilter := "interlaced", "tinterlace=mode=interleave_top"
		if hdr {
			name, sourceFilter = "hdr", "format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc"
		}
		t.Run(name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source.mkv")
			progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=320x192:rate=32:duration=2", "-vf", sourceFilter, "-c:v", "ffv1", "-threads:v", "1", source)
			info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			plan := progressiveVideoFixturePlan(info, "h264", "", 0)
			plan.Width, plan.Height = 320, 192
			baseline := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			plan.Hardware = Hardware{Decode: "software", Encode: "vaapi", Device: device}
			plan.VideoFilters = VideoFilters{Backend: "vulkan", Deinterlace: "tff"}
			if hdr {
				plan.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "hdr10", SourceBitDepth: 10, SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
			}
			output := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			assertVideoProcessingPixelChange(t, decodeProgressiveVideoPixels(t, ctx, ffmpeg, baseline), decodeProgressiveVideoPixels(t, ctx, ffmpeg, output), .2)
			actual, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, output)
			if err != nil || len(actual.Streams) != 1 || actual.Streams[0].IsInterlaced || !actual.Streams[0].InterlaceKnown {
				t.Fatalf("GPU processing did not produce progressive output: %+v, %v", actual, err)
			}
			if hdr && (actual.Streams[0].ColorTransfer != "bt709" || actual.Streams[0].ColorPrimaries != "bt709" || actual.Streams[0].ColorSpace != "bt709") {
				t.Fatalf("GPU tone mapping retained source HDR tags: %+v", actual.Streams[0])
			}
		})
	}
}

func runSubtitleProgressiveVideo(t *testing.T, ctx context.Context, ffmpeg, source string, plan Plan) string {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	directory := t.TempDir()
	var ready, ended bool
	result, err := Run(ctx, ffmpeg, directory, input, plan, 1, func(progress Progress) {
		ready = ready || progress.Ready && progress.Bytes > 0
		ended = ended || progress.Ended
	})
	if err != nil || !ready || !ended {
		t.Fatalf("progressive subtitle burn failed: %v ready=%t ended=%t: %s", err, ready, ended, result.StderrTail)
	}
	return filepath.Join(directory, "stream.bin")
}

func TestDolbyVisionActualRPUChangesPixelsAndProducesSDRAndHDR10(t *testing.T) {
	source, device := os.Getenv("GOBY_TEST_DOLBY_VISION_FILE"), os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if source == "" || device == "" {
		t.Skip("a licensed residual-free Dolby Vision fixture and AMD device are required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	info, err := (media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 30 * time.Second}).Probe(ctx, source)
	if err != nil || info.DurationTicks <= 0 || info.DurationTicks > 10*ticksPerSecond {
		t.Fatalf("Dolby Vision fixture must be a complete clip of at most ten seconds: %+v, %v", info, err)
	}
	var dv *media.DolbyVisionMetadata
	var sourceVideo media.Stream
	for _, stream := range info.Streams {
		if stream.CodecType == "video" {
			dv = stream.DolbyVision
			sourceVideo = stream
		}
	}
	if dv == nil || !dv.RPUVerified || dv.RPUProfile != dv.Profile || dv.RPUResidualMixed || dv.RPUFrameCount < 2 || dv.Profile != 7 && !dv.ResidualDisabled {
		t.Fatalf("fixture does not contain complete verified RPU evidence: %+v", dv)
	}
	if media.VideoBitDepthConflict(sourceVideo) || media.EffectiveVideoBitDepth(sourceVideo) != 10 ||
		(sourceVideo.PixelFormat != "yuv420p10le" && sourceVideo.PixelFormat != "p010le") {
		t.Fatalf("fixture does not establish decoded 10-bit pixels: %+v", sourceVideo)
	}
	for _, outputRange := range []string{"sdr", "hdr10"} {
		t.Run(outputRange, func(t *testing.T) {
			plan := progressiveVideoFixturePlan(info, "h264", "", 0)
			plan.Width, plan.Height = 320, 192
			plan.Hardware = Hardware{Decode: "software", Encode: "vaapi", Device: device}
			plan.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "dolbyvision", DVProfile: dv.Profile, SourceBitDepth: 10,
				OutputRange: outputRange, SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
			if outputRange == "hdr10" {
				plan.VideoCodec, plan.VideoBitDepth = "hevc", 10
			}
			output := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			actual, err := (media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, output)
			if err != nil || len(actual.Streams) != 1 || actual.Streams[0].DolbyVision != nil {
				t.Fatalf("converted output retained source Dolby Vision metadata: %+v, %v", actual, err)
			}
			wantTransfer, wantPrimaries, wantDepth, wantPixelFormat := "bt709", "bt709", 8, "yuv420p"
			if outputRange == "hdr10" {
				wantTransfer, wantPrimaries, wantDepth, wantPixelFormat = "smpte2084", "bt2020", 10, "yuv420p10le"
			}
			video := actual.Streams[0]
			if video.ColorTransfer != wantTransfer || video.ColorPrimaries != wantPrimaries ||
				media.VideoBitDepthConflict(video) || media.EffectiveVideoBitDepth(video) != wantDepth || video.PixelFormat != wantPixelFormat {
				t.Fatalf("converted pixels and output signal declaration disagree: %+v", video)
			}
			// The same encoded source without RPU reshaping is an independent
			// negative control. A non-identity fixture must change actual pixels;
			// metadata tags alone cannot satisfy this acceptance gate.
			plan.VideoFilters.ToneMap, plan.VideoFilters.DVProfile = "hdr10", 0
			withoutRPU := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			assertVideoProcessingPixelChange(t, decodeProgressiveVideoPixels(t, ctx, ffmpeg, withoutRPU), decodeProgressiveVideoPixels(t, ctx, ffmpeg, output), .1)
			if os.Getenv("GOBY_TEST_DOLBY_VISION_CHART") == "1" {
				verifyDolbyVisionNeutralChart(t, ctx, ffmpeg, withoutRPU, output, outputRange)
			}
		})
	}
}

func TestDolbyVisionStrictRendererRejectsActualNonzeroResidual(t *testing.T) {
	source, device := os.Getenv("GOBY_TEST_DOLBY_VISION_FEL_FILE"), os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if source == "" || device == "" {
		t.Skip("a licensed nonzero-residual Dolby Vision fixture and AMD device are required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	info, err := (media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(ctx, source)
	if err != nil || info.DurationTicks <= 0 || info.DurationTicks > 10*ticksPerSecond {
		t.Fatalf("residual fixture must be a complete clip of at most ten seconds: %+v, %v", info, err)
	}
	plan := progressiveVideoFixturePlan(info, "h264", "", 0)
	plan.Width, plan.Height = 320, 192
	plan.Hardware = Hardware{Decode: "software", Encode: "vaapi", Device: device}
	plan.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "dolbyvision", DVProfile: 7, SourceBitDepth: 10,
		SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ready := false
	_, err = Run(ctx, ffmpeg, t.TempDir(), input, plan, 1, func(progress Progress) { ready = ready || progress.Ready })
	if err == nil || ready {
		t.Fatalf("FEL residual was silently discarded into playable output: err=%v ready=%v", err, ready)
	}
}
