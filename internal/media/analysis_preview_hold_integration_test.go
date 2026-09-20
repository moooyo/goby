package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These opt-in generated fixtures prove presentation mechanics, not the
// accuracy of intro matching. A separate bounded FFprobe frame inventory
// supplies the reference clock; color changes identify the held source image.
func TestAnalysisPreviewActualLongFramesAndAudioTail(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	const second = TicksPerSecond
	for _, fixture := range []struct {
		name, video, audio string
		interval, duration int64
		sourceTicks        []int64
		actualTicks        []int64
		colors             string
	}{
		{
			name:     "video_10s_audio_10_1s",
			video:    "color=c=red:size=64x64:rate=25,drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='eq(n,249)',trim=end_frame=250",
			audio:    "anullsrc=r=48000:cl=mono,atrim=end_sample=484800",
			interval: 10 * second, duration: 101 * second / 10,
			actualTicks: []int64{0, 249 * second / 25}, colors: "RB",
		},
		{
			name: "vfr_long_frames",
			video: "color=c=red:size=64x64:rate=25,drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='eq(n,1)'," +
				"drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='eq(n,2)',trim=end_frame=3," +
				"settb=expr=1/1000,setpts='if(eq(N,0),0,if(eq(N,1),3000,7000))'",
			audio:    "anullsrc=r=48000:cl=mono,atrim=end_sample=388800",
			interval: second, duration: 81 * second / 10,
			sourceTicks: []int64{0, 3 * second, 7 * second},
			actualTicks: []int64{0, 0, 0, 3 * second, 3 * second, 3 * second, 3 * second, 7 * second, 7 * second}, colors: "RRRGGGGBB",
		},
		{
			name: "first_video_frame_after_audio_start",
			video: "color=c=red:size=64x64:rate=25,drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='eq(n,1)'," +
				"drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='eq(n,2)',trim=end_frame=3," +
				"settb=expr=1/1000,setpts='if(eq(N,0),1000,if(eq(N,1),4000,8000))'",
			audio:    "anullsrc=r=48000:cl=mono,atrim=end_sample=436800",
			interval: 2 * second, duration: 91 * second / 10,
			sourceTicks: []int64{second, 4 * second, 8 * second},
			actualTicks: []int64{second, second, 4 * second, 4 * second, 8 * second}, colors: "RRGGB",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".mkv")
			audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", fixture.video, "-f", "lavfi", "-i", fixture.audio,
				"-map", "0:v:0", "-map", "1:a:0", "-copyts", "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
				"-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "1", "-pix_fmt", "yuv420p",
				"-c:a", "pcm_s16le", path)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil || info.DurationTicks != fixture.duration || !info.FormatStartKnown || info.FormatStartTicks != 0 {
				t.Fatalf("fixture clock does not contain the intended video/audio boundary: %+v, %v", info, err)
			}
			var video Stream
			videos := 0
			for _, stream := range info.Streams {
				if stream.CodecType == "video" && !stream.IsAttachedPicture {
					video, videos = stream, videos+1
				}
			}
			if videos != 1 {
				t.Fatalf("expected one timed video stream: %+v", info.Streams)
			}
			actualSource := analysisPreviewReferenceFrameTicks(t, ffprobe, path, info, video)
			wantSource := fixture.sourceTicks
			if wantSource == nil {
				wantSource = make([]int64, 250)
				for index := range wantSource {
					wantSource[index] = int64(index) * second / 25
				}
			}
			if fmt.Sprint(actualSource) != fmt.Sprint(wantSource) {
				t.Fatalf("source did not retain the intended real frame PTS: got %v, want %v", actualSource, wantSource)
			}
			extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe}
			callbacks, preceding := 0, 0
			var previousJPEG [sha256.Size]byte
			summary, err := extractor.ExtractPreviews(context.Background(), file, info, video.Index,
				PreviewAnalysisOptions{Width: 240, IntervalTicks: fixture.interval}, func(frame PreviewFrame) error {
					if callbacks >= len(fixture.actualTicks) {
						return fmt.Errorf("extra nominal slot %d", callbacks)
					}
					nominal := int64(callbacks) * fixture.interval
					for preceding+1 < len(actualSource) && actualSource[preceding+1] <= nominal {
						preceding++
					}
					if frame.NominalTicks != nominal || frame.ActualTicks != fixture.actualTicks[callbacks] || frame.ActualTicks != actualSource[preceding] {
						return fmt.Errorf("slot %d did not retain its original displayed frame PTS: nominal=%d actual=%d reference=%d", callbacks, frame.NominalTicks, frame.ActualTicks, actualSource[preceding])
					}
					if sha256.Sum256(frame.JPEG) != frame.SHA256 {
						return fmt.Errorf("JPEG digest is not bound to its bytes")
					}
					if callbacks > 0 && fixture.actualTicks[callbacks] == fixture.actualTicks[callbacks-1] && frame.SHA256 != previousJPEG {
						return fmt.Errorf("a held source frame changed its JPEG between slots")
					}
					raster, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
					if err != nil {
						return err
					}
					if err := analysisGeometryAssertColor(raster, raster.Bounds().Dx()/2, raster.Bounds().Dy()/2, fixture.colors[callbacks]); err != nil {
						return fmt.Errorf("slot %d selected a different source image: %w", callbacks, err)
					}
					previousJPEG = frame.SHA256
					callbacks++
					return nil
				})
			if err != nil || callbacks != len(fixture.actualTicks) || summary.FrameCount != callbacks || summary.IntervalTicks != fixture.interval || summary.Profile != PreviewAnalysisProfile {
				t.Fatalf("held-frame extraction = %+v, callbacks=%d, error=%v", summary, callbacks, err)
			}
		})
	}
}

func analysisPreviewReferenceFrameTicks(t *testing.T, ffprobe, path string, info Info, stream Stream) []int64 {
	t.Helper()
	encoded, err := runLimited(context.Background(), 30*time.Second, 256<<10, ffprobe,
		"-v", "error", "-select_streams", "v:0", "-show_frames", "-show_entries", "frame=pts", "-of", "json", path)
	if err != nil {
		t.Fatalf("read independent frame PTS inventory: %v", err)
	}
	var inventory struct {
		Frames []struct {
			PTS *int64 `json:"pts"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(encoded, &inventory); err != nil || len(inventory.Frames) < 1 || len(inventory.Frames) > 250 {
		t.Fatalf("invalid reference frame inventory: %v", err)
	}
	base, err := analysisTimeBase(stream.TimeBase)
	if err != nil {
		t.Fatal(err)
	}
	ticks := make([]int64, len(inventory.Frames))
	for index, frame := range inventory.Frames {
		if frame.PTS == nil {
			t.Fatal("the reference frame has no actual PTS")
		}
		ticks[index], err = analysisVisualTicks(*frame.PTS, base, info.FormatStartTicks)
		if err != nil || index > 0 && ticks[index] <= ticks[index-1] {
			t.Fatalf("invalid reference source PTS: %v", err)
		}
	}
	return ticks
}
