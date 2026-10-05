//go:build linux

package transcode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// A 320x192 caption canvas must retain its normalized position and glyph size
// on a 640x384 source, including a smaller adaptive rendition and an authored
// event after seeking. Merely proving that some pixels changed is insufficient.
func TestExternalBitmapActualCanvasScalingAndSeek(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "canvas.mkv")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=black:size=640x384:rate=16:duration=4.5", "-c:v", "ffv1", "-threads:v", "1", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || !info.FormatStartKnown || len(info.Streams) != 1 || info.Streams[0].Width != 640 || info.Streams[0].Height != 384 {
		t.Fatalf("source canvas facts are not established: %+v, %v", info, err)
	}
	spec, subtitle, _ := externalBitmapAssetFixture(t)
	base := Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		DurationTicks: info.DurationTicks, StartTicks: 2 * ticksPerSecond, Width: 640, Height: 384, FrameRate: 16, VideoBitrate: 768000,
		SourceFormatStartKnown: true, SourceFormatStartTicks: info.FormatStartTicks, Subtitle: spec.Plan.Subtitle}
	base.Subtitle.OffsetTicks = 0
	for _, mode := range []string{"progressive", "ladder"} {
		t.Run(mode, func(t *testing.T) {
			plan := base
			if mode == "ladder" {
				plan.OutputMode, plan.SourceFormatStartKnown, plan.SourceFormatStartTicks = "", false, 0
				plan.SegmentSeconds = 1
				plan.HLS = HLSPlan{SegmentType: "fmp4", RenditionCount: 2,
					Renditions: [MaxHLSRenditions]HLSRendition{{Width: 640, Height: 384, VideoBitrate: 768000}, {Width: 320, Height: 192, VideoBitrate: 256000}}}
			}
			spec.Plan = plan
			work := withBitmapSubtitleSource(ctx, spec, func(ctx context.Context, _ Spec, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
				return consume(ctx, subtitle)
			})
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			directory := t.TempDir()
			result, err := Run(work, ffmpeg, directory, input, plan, 1, nil)
			if err != nil {
				t.Fatalf("external canvas conversion failed: %v: %s", err, result.StderrTail)
			}
			for rendition := 0; rendition < max(1, plan.HLS.RenditionCount); rendition++ {
				path, width, height := filepath.Join(directory, "stream.bin"), 640, 384
				if mode == "ladder" {
					width, height = plan.HLS.Renditions[rendition].Width, plan.HLS.Renditions[rendition].Height
					data, err := os.ReadFile(filepath.Join(directory, HLSPlaylistName(rendition, 2)))
					if err != nil {
						t.Fatal(err)
					}
					playlist, err := ParseMediaPlaylist(data)
					if err != nil || !playlist.Ended || len(playlist.Segments) != 3 {
						t.Fatalf("caption ladder is not a complete 2.5-second window: %+v, %v", playlist, err)
					}
					joined, err := os.ReadFile(filepath.Join(directory, playlist.InitName))
					if err != nil {
						t.Fatal(err)
					}
					for _, segment := range playlist.Segments {
						data, err := os.ReadFile(filepath.Join(directory, segment.Name))
						if err != nil {
							t.Fatal(err)
						}
						joined = append(joined, data...)
					}
					path = filepath.Join(t.TempDir(), fmt.Sprintf("rendition-%d.mp4", rendition))
					if err := os.WriteFile(path, joined, 0600); err != nil {
						t.Fatal(err)
					}
				}
				pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, path)
				if len(pixels) != 40*width*height {
					t.Fatalf("subtitle scaling changed the output frame count: %d", len(pixels)/(width*height))
				}
				average := func(frame, left, top, right, bottom int) float64 {
					var sum, count int
					for y := top * height / 192; y < bottom*height/192; y++ {
						for x := left * width / 320; x < right*width/320; x++ {
							sum += int(pixels[frame*width*height+y*width+x])
							count++
						}
					}
					return float64(sum) / float64(count)
				}
				for frame := 0; frame < 40; frame++ {
					white := average(frame, 116, 148, 204, 164)
					visible := frame >= 8 && frame < 24
					if visible && white < 200 || !visible && white > 24 || average(frame, 55, 70, 105, 85) > 24 {
						t.Fatalf("caption position/size/clock changed at %s rendition %d frame %d: white=%g visible=%v", mode, rendition, frame, white, visible)
					}
				}
			}
		})
	}
}
