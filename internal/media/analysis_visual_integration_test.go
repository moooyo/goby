package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in runtime test is mechanics evidence only. A generated VFR raster
// cannot establish intro-detection accuracy on a labeled real episode corpus.
func TestAnalysisVisualActualVFRAndStreamingPreviews(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "source-vfr.mp4")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25",
		"-frames:v", "96", "-vf", "setpts=(N+floor(N/5))/(25*TB)", "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "1", "-pix_fmt", "yuv420p", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Replace the pathname before probing the held object. Both extractions
	// must continue through the authorized descriptor and preserve its offset.
	if err := os.Rename(path, path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement is not media"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	streamIndex := -1
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			streamIndex = stream.Index
			break
		}
	}
	extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe}
	samples, err := extractor.ExtractVisual(context.Background(), file, info, streamIndex, VisualAnalysisOptions{EndTicks: 3 * TicksPerSecond})
	if err != nil {
		t.Fatal(err)
	}
	var actual []int64
	for _, sample := range samples {
		actual = append(actual, sample.Ticks)
	}
	if fmt.Sprint(actual) != "[0 5200000 10000000 15200000 20000000 25200000]" {
		t.Fatalf("actual VFR samples used an invented fixed cadence: %v", actual)
	}
	var count int
	var total int64
	summary, err := extractor.ExtractPreviews(context.Background(), file, info, streamIndex, PreviewAnalysisOptions{Width: 240, IntervalTicks: TicksPerSecond, Quality: 95}, func(frame PreviewFrame) error {
		if frame.NominalTicks != int64(count)*TicksPerSecond || frame.ActualTicks < 0 || frame.ActualTicks > frame.NominalTicks {
			return fmt.Errorf("invalid preview timestamps: nominal=%d actual=%d", frame.NominalTicks, frame.ActualTicks)
		}
		if sha256.Sum256(frame.JPEG) != frame.SHA256 {
			return fmt.Errorf("preview content digest mismatch")
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(frame.JPEG))
		if err != nil || config.Width != 240 || config.Height != 240 || config.Width != frame.Width || config.Height != frame.Height {
			return fmt.Errorf("invalid preview image dimensions: %+v, %v", config, err)
		}
		total += int64(len(frame.JPEG))
		count++
		return nil
	})
	if err != nil || count < 4 || summary.FrameCount != count || summary.JPEGBytes != total || summary.Quality != 95 {
		t.Fatalf("streaming previews = %+v, callbacks=%d, bytes=%d, error=%v", summary, count, total, err)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
		t.Fatalf("extraction changed the held descriptor position: %d, %v", offset, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbacks := 0
	canceledSummary, err := extractor.ExtractPreviews(ctx, file, info, streamIndex, PreviewAnalysisOptions{Width: 240, IntervalTicks: TicksPerSecond}, func(PreviewFrame) error {
		callbacks++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || callbacks != 1 || canceledSummary.FrameCount != 0 {
		t.Fatalf("canceled preview publication = %+v, callbacks=%d, error=%v", canceledSummary, callbacks, err)
	}
}
