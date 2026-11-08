//go:build linux

package transcode

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestSoftwareToneMapAlgorithmsProduceDistinctPixelsWithStableClocks(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "hdr.mkv")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x96:rate=16:duration=2",
		"-vf", "format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc",
		"-c:v", "ffv1", "-threads:v", "1", "-color_primaries", "bt2020", "-color_trc", "smpte2084",
		"-colorspace", "bt2020nc", "-color_range", "tv", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 1 || info.Streams[0].ColorTransfer != "smpte2084" || info.Streams[0].BitDepth != 10 {
		t.Fatalf("cannot establish the source HDR facts: %+v: %v", info, err)
	}
	plan := progressiveVideoFixturePlan(info, "h264", "", 0)
	plan.Width, plan.Height = info.Streams[0].Width, info.Streams[0].Height
	plan.VideoFilters = VideoFilters{ToneMap: "hdr10", SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
	legacyOutput := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
	legacyPixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, legacyOutput)
	var hablePixels []byte
	for _, algorithm := range []string{"Hable", "Reinhard", "Mobius"} {
		t.Run(algorithm, func(t *testing.T) {
			options := DefaultExecutionOptions(1)
			options.SoftwareToneMapOptions.SoftwareToneMapAlgorithm = algorithm
			captured, err := CaptureExecution(plan, options)
			if err != nil {
				t.Fatal(err)
			}
			output := runProgressiveVideo(t, ctx, ffmpeg, source, captured)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			actual, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, output)
			if err != nil || len(actual.Streams) != 1 {
				t.Fatalf("cannot inspect the selected transform: %+v: %v", actual, err)
			}
			video := actual.Streams[0]
			if video.Width != plan.Width || video.Height != plan.Height || video.ColorPrimaries != "bt709" ||
				video.ColorTransfer != "bt709" || video.ColorSpace != "bt709" || video.ColorRange != "tv" {
				t.Fatalf("output dimensions or color metadata disagree with the pixel transform: %+v", video)
			}
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
			if len(facts.video) != 32 || math.Abs(facts.video[0].time(t)) > .002 ||
				math.Abs(facts.video[31].time(t)-31.0/16) > .002 {
				t.Fatalf("algorithm selection changed the presentation clock: %+v", facts.video)
			}
			pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, output)
			if algorithm == "Hable" {
				hablePixels = pixels
				if len(legacyPixels) != len(pixels) {
					t.Fatal("current Hable output changed the legacy presentation length")
				}
				var difference float64
				for index := range pixels {
					difference += math.Abs(float64(pixels[index]) - float64(legacyPixels[index]))
				}
				if len(pixels) == 0 || difference/float64(len(pixels)) > .5 {
					t.Fatal("current Hable output differs from the historical pixel transform")
				}
			} else {
				assertVideoProcessingPixelChange(t, hablePixels, pixels, .2)
			}
		})
	}
}
