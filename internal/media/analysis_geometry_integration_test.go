package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This opt-in test checks actual display orientation and anamorphic scaling.
// It supplies no evidence about automatic intro detection on real episodes.
func TestAnalysisGeometryActualOrthogonalRotationAndSAR(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	colorGrid := "color=c=black:size=96x64:rate=5," +
		"drawbox=x=0:y=0:w=48:h=32:color=red:t=fill," +
		"drawbox=x=48:y=0:w=48:h=32:color=lime:t=fill," +
		"drawbox=x=0:y=32:w=48:h=32:color=blue:t=fill," +
		"drawbox=x=48:y=32:w=48:h=32:color=white:t=fill"
	basePaths := make(map[string]string)
	for _, sar := range []string{"1/1", "16/15", "64/45"} {
		path := filepath.Join(directory, fmt.Sprintf("grid-%d.mp4", len(basePaths)))
		audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", colorGrid, "-t", "2", "-vf", "setsar="+sar,
			"-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "1", "-pix_fmt", "yuv420p", path)
		basePaths[sar] = path
	}
	for _, fixture := range []struct {
		name      string
		clockwise int
		sar       string
		height    int
		corners   [4]byte
	}{
		{"identity", 0, "1/1", 160, [4]byte{'R', 'G', 'B', 'W'}},
		{"clockwise90", 90, "1/1", 360, [4]byte{'B', 'R', 'W', 'G'}},
		{"clockwise180", 180, "1/1", 160, [4]byte{'W', 'B', 'G', 'R'}},
		{"clockwise270", 270, "1/1", 360, [4]byte{'G', 'W', 'R', 'B'}},
		{"sar16_15", 0, "16/15", 150, [4]byte{'R', 'G', 'B', 'W'}},
		{"sar64_45", 0, "64/45", 113, [4]byte{'R', 'G', 'B', 'W'}},
		{"clockwise90_sar16_15", 90, "16/15", 384, [4]byte{'B', 'R', 'W', 'G'}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := basePaths[fixture.sar]
			if fixture.clockwise != 0 {
				path = filepath.Join(directory, fixture.name+".mp4")
				// The input option is explicitly counterclockwise in FFmpeg.
				audioProbeRunFFmpeg(t, ffmpeg, "-display_rotation:v:0", strconv.Itoa(-fixture.clockwise), "-noautorotate",
					"-i", basePaths[fixture.sar], "-map", "0:v:0", "-c", "copy", path)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
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
			callbacks := 0
			summary, err := extractor.ExtractPreviews(context.Background(), file, info, streamIndex,
				PreviewAnalysisOptions{Width: 240, IntervalTicks: TicksPerSecond}, func(frame PreviewFrame) error {
					decoded, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
					if err != nil {
						return err
					}
					if decoded.Bounds().Dx() != 240 || decoded.Bounds().Dy() != fixture.height || frame.Width != 240 || frame.Height != fixture.height {
						return fmt.Errorf("incorrect displayed raster: %v, metadata=%dx%d", decoded.Bounds(), frame.Width, frame.Height)
					}
					for index, expected := range fixture.corners {
						x, y := (1+2*(index%2))*240/4, (1+2*(index/2))*fixture.height/4
						if err := analysisGeometryAssertColor(decoded, x, y, expected); err != nil {
							return err
						}
					}
					callbacks++
					return nil
				})
			if err != nil || callbacks != 2 || summary.FrameCount != 2 || summary.FFmpegSHA256 == "" || summary.FFprobeSHA256 == "" {
				t.Fatalf("display normalization = %+v, callbacks=%d, error=%v", summary, callbacks, err)
			}
			extractor.ExpectedFFmpegSHA256, extractor.ExpectedFFprobeSHA256 = summary.FFmpegSHA256, summary.FFprobeSHA256
			visual, err := extractor.ExtractVisual(context.Background(), file, info, streamIndex, VisualAnalysisOptions{EndTicks: 2 * TicksPerSecond})
			if err != nil || len(visual) != 4 {
				t.Fatalf("visual geometry normalization = %+v, %v", visual, err)
			}
			for _, sample := range visual {
				if sample.Contrast == 0 {
					t.Fatal("normalized color grid lost its contrast")
				}
			}
		})
	}
}

func analysisGeometryAssertColor(raster image.Image, x, y int, expected byte) error {
	rawR, rawG, rawB, _ := raster.At(x, y).RGBA()
	r, g, b := rawR>>8, rawG>>8, rawB>>8
	matched := false
	switch expected {
	case 'R':
		matched = r > 180 && g < 90 && b < 90
	case 'G':
		matched = g > 170 && r < 90 && b < 90
	case 'B':
		matched = b > 170 && r < 90 && g < 90
	case 'W':
		matched = r > 180 && g > 180 && b > 180
	}
	if !matched {
		return fmt.Errorf("displayed quadrant at %d,%d = %d,%d,%d, expected %c", x, y, r, g, b, expected)
	}
	return nil
}
