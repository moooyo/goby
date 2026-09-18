//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoFiltersActualHDRPixelsMetadataAndCadence(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, transfer := range []string{"smpte2084", "arib-std-b67"} {
		t.Run(transfer, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "hdr.mkv")
			// FFmpeg initializes encoder color primaries and transfer from the
			// filtered frame. Output codec flags alone leave these lavfi frame
			// properties unknown, so the Matroska source would not actually be
			// tagged HDR. Set real frame metadata before encoding the fixture.
			sourceFilter := "format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=" + transfer + ":colorspace=bt2020nc"
			progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x96:rate=16:duration=2", "-vf", sourceFilter,
				"-c:v", "ffv1", "-threads:v", "1", "-color_primaries", "bt2020", "-color_trc", transfer,
				"-colorspace", "bt2020nc", "-color_range", "tv", source)
			raw := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
				"stream=color_transfer,color_primaries,color_space,color_range", "-of", "json", source)
			var sourceFacts struct {
				Streams []struct {
					Transfer  string `json:"color_transfer"`
					Primaries string `json:"color_primaries"`
					Matrix    string `json:"color_space"`
					Range     string `json:"color_range"`
				} `json:"streams"`
			}
			if err := json.Unmarshal(raw, &sourceFacts); err != nil || len(sourceFacts.Streams) != 1 {
				t.Fatalf("cannot independently inspect HDR source colorimetry: %s, %v", raw, err)
			}
			declared := sourceFacts.Streams[0]
			if declared.Transfer != transfer || declared.Primaries != "bt2020" || declared.Matrix != "bt2020nc" || declared.Range != "tv" {
				t.Fatalf("encoded fixture does not contain the required HDR colorimetry: %+v", declared)
			}
			info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
			if err != nil || len(info.Streams) != 1 || info.Streams[0].ColorTransfer != transfer || info.Streams[0].BitDepth != 10 ||
				info.Streams[0].ColorPrimaries != declared.Primaries || info.Streams[0].ColorSpace != declared.Matrix || info.Streams[0].ColorRange != declared.Range {
				t.Fatalf("HDR fixture lacks its declared source facts: %+v, %v", info, err)
			}
			plan := progressiveVideoFixturePlan(info, "h264", "", 0)
			baseline := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			plan.VideoFilters = VideoFilters{ToneMap: "hdr10", SourceTransfer: transfer, SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
			if transfer == "arib-std-b67" {
				plan.VideoFilters.ToneMap = "hlg"
			}
			output := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			actual, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, output)
			if err != nil || len(actual.Streams) != 1 {
				t.Fatalf("cannot inspect transformed output: %+v, %v", actual, err)
			}
			video := actual.Streams[0]
			if video.ColorPrimaries != "bt709" || video.ColorTransfer != "bt709" || video.ColorSpace != "bt709" || video.ColorRange != "tv" || video.PixelFormat != "yuv420p" {
				t.Fatalf("encoded color tags differ from the SDR transform: %+v", video)
			}
			assertVideoProcessingPixelChange(t, decodeProgressiveVideoPixels(t, ctx, ffmpeg, baseline), decodeProgressiveVideoPixels(t, ctx, ffmpeg, output), 5)
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
			if len(facts.video) != 32 || math.Abs(facts.video[31].time(t)-31.0/16) > .002 {
				t.Fatalf("tone mapping changed frame count or source cadence: %+v", facts.video)
			}
		})
	}
}

func TestVideoFiltersActualDeinterlaceChangesPixelsAndMarksProgressive(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "interlaced.mkv")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x96:rate=32:duration=2", "-vf", "tinterlace=mode=interleave_top",
		"-c:v", "ffv1", "-threads:v", "1", "-field_order", "tt", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 1 || !info.Streams[0].IsInterlaced {
		t.Fatalf("interlaced fixture lacks field-order facts: %+v, %v", info, err)
	}
	plan := progressiveVideoFixturePlan(info, "h264", "", 0)
	baseline := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
	plan.VideoFilters = VideoFilters{Deinterlace: "tff"}
	output := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
	assertVideoProcessingPixelChange(t, decodeProgressiveVideoPixels(t, ctx, ffmpeg, baseline), decodeProgressiveVideoPixels(t, ctx, ffmpeg, output), .2)
	data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_frames",
		"-show_entries", "frame=interlaced_frame", "-of", "json", output)
	var document struct {
		Frames []struct {
			Interlaced int `json:"interlaced_frame"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Frames) != 32 {
		t.Fatalf("send_frame deinterlacing changed the frame cadence: %d", len(document.Frames))
	}
	for _, frame := range document.Frames {
		if frame.Interlaced != 0 {
			t.Fatal("a transformed frame is still marked interlaced")
		}
	}
}

func assertVideoProcessingPixelChange(t *testing.T, before, after []byte, minimum float64) {
	t.Helper()
	if len(before) == 0 || len(before) != len(after) {
		t.Fatalf("processing changed the presentation length: before=%d after=%d", len(before), len(after))
	}
	var difference float64
	for i := range before {
		difference += math.Abs(float64(before[i]) - float64(after[i]))
	}
	if difference/float64(len(before)) < minimum {
		t.Fatalf("metadata changed without a measurable pixel transform: mean difference=%g", difference/float64(len(before)))
	}
}
