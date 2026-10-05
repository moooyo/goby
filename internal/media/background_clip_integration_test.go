package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These opt-in tests exercise actual local descriptors and the pinned Linux
// FFmpeg toolchain. Synthetic pictures prove mechanics, not editorial quality.
func TestBackgroundClipActualSDRHDRAndHLG(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	available, err := (AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe}).BackgroundClipAvailability(context.Background())
	if err != nil || !available.Available || !analysisValidSHA256(available.FFmpegSHA256) || !analysisValidSHA256(available.FFprobeSHA256) {
		t.Fatalf("actual background clip capability is unavailable: %+v, %v", available, err)
	}
	for _, fixture := range []struct{ name, codec, pixelFormat, transfer string }{
		{"sdr", "libx264", "yuv420p", ""},
		{"hdr10", "libx265", "yuv420p10le", "smpte2084"},
		{"hlg", "libx265", "yuv420p10le", "arib-std-b67"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".mp4")
			args := []string{"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
				"-t", "4", "-c:v", fixture.codec, "-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", fixture.pixelFormat, "-c:a", "aac"}
			if fixture.transfer != "" {
				// This pinned encoder needs explicit bitstream VUI parameters;
				// muxer color options alone omit transfer and primaries on decode.
				args = append(args, "-x265-params", "pools=none:frame-threads=1:log-level=error:colorprim=bt2020:transfer="+fixture.transfer+":colormatrix=bt2020nc:range=limited",
					"-color_trc", fixture.transfer, "-color_primaries", "bt2020", "-colorspace", "bt2020nc", "-color_range", "tv")
			}
			audioProbeRunFFmpeg(t, ffmpeg, append(args, path)...)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			// A pathname replacement must not redirect the authorized read.
			if err := os.Rename(path, path+".held"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not the authorized media"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := file.Seek(17, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			index := -1
			for _, stream := range info.Streams {
				if stream.CodecType != "video" {
					continue
				}
				index = stream.Index
				if fixture.transfer != "" && (stream.ColorTransfer != fixture.transfer || stream.ColorPrimaries != "bt2020" || stream.ColorSpace != "bt2020nc" || stream.ColorRange != "tv" || EffectiveVideoBitDepth(stream) != 10) {
					t.Fatalf("HDR fixture lacks complete decoded color evidence: %+v", stream)
				}
				break
			}
			extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Limits: AnalysisLimits{Timeout: 2 * time.Minute}}
			var output bytes.Buffer
			summary, err := extractor.GenerateBackgroundClip(context.Background(), file, info, index, TicksPerSecond/2, 3*TicksPerSecond, &output)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Width != 1280 || summary.Height != 720 || summary.DurationTicks != 3*TicksPerSecond || summary.StartTicks != TicksPerSecond/2 ||
				summary.Bytes != int64(output.Len()) || summary.Bytes <= 0 || summary.Bytes > MaxBackgroundClipBytes || !analysisValidSHA256(summary.FFmpegSHA256) || !analysisValidSHA256(summary.FFprobeSHA256) {
				t.Fatalf("invalid generated summary: %+v, bytes=%d", summary, output.Len())
			}
			if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
				t.Fatalf("source offset changed: %d, %v", offset, err)
			}
			if fixture.name != "sdr" {
				return
			}
			output.Reset()
			configured, err := extractor.GenerateBackgroundClipWithOptions(context.Background(), file, info, index, 0, TicksPerSecond,
				BackgroundClipOptions{MaxWidth: 1920, VideoBitrate: 250000}, &output)
			if err != nil || configured.Width != 1920 || configured.Height != 1080 || configured.DurationTicks != TicksPerSecond || configured.Bytes != int64(output.Len()) {
				t.Fatalf("configured 1080p generation: %+v, %v", configured, err)
			}
			limited := extractor
			limited.Limits.MaxOutputBytes = 1024
			output.Reset()
			if result, err := limited.GenerateBackgroundClip(context.Background(), file, info, index, 0, 3*TicksPerSecond, &output); !errors.Is(err, ErrAnalysisBudget) || result.Bytes != 0 || output.Len() != 0 {
				t.Fatalf("output budget published bytes: %+v, %d, %v", result, output.Len(), err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if result, err := extractor.GenerateBackgroundClip(ctx, file, info, index, 0, 3*TicksPerSecond, backgroundClipCancelWriter{cancel}); !errors.Is(err, context.Canceled) || result.Bytes != 0 {
				t.Fatalf("late cancellation published summary: %+v, %v", result, err)
			}
		})
	}
}

type backgroundClipCancelWriter struct{ cancel context.CancelFunc }

func (writer backgroundClipCancelWriter) Write(data []byte) (int, error) {
	writer.cancel()
	return len(data), nil
}
