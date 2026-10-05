package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

// The independent control argument list below mirrors the pinned upstream
// FFmpegService.FingerprintAsync recipe, not creditsSkipperArgs. This verifies
// extraction/clock parity on generated audio, not ending-credit accuracy.
func TestCreditsSkipperActualNativeRecipeParity(t *testing.T) {
	ffprobe, fixtureFFmpeg := audioProbeIntegrationTools(t)
	ffmpeg := os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG")
	if ffmpeg == "" {
		ffmpeg = fixtureFFmpeg
	}
	for _, test := range []struct {
		name          string
		durationTicks int64
	}{{"short", 25*TicksPerSecond + 1_250_000}, {"tail", 475*TicksPerSecond + 1_250_000}} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credits.wav")
			duration := float64(test.durationTicks) / float64(TicksPerSecond)
			// The chirp changes over time, so selecting the first 450 seconds
			// cannot accidentally pass as the last 450 seconds of the source.
			audioProbeRunFFmpeg(t, fixtureFFmpeg, "-f", "lavfi", "-i", "aevalsrc=0.3*sin(2*PI*(180*t+0.75*t*t)):s=16000:d="+strconv.FormatFloat(duration, 'f', -1, 64), "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "wav", path)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			if info.DurationTicks != test.durationTicks {
				t.Fatalf("fixture duration changed: %d != %d", info.DurationTicks, test.durationTicks)
			}
			index, ok := SelectIntroSkipperAudioStream(info, "", true)
			if !ok {
				t.Fatal("fixture audio absent")
			}
			if _, err := file.Seek(19, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			result, err := (AnalysisExtractor{IntroFFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: time.Minute}}).ExtractCreditsSkipper(context.Background(), file, info, IntroSkipperAnalysisRequest{AudioStreamIndex: index, Options: introskipper.DefaultOptions()})
			if err != nil {
				t.Fatal(err)
			}
			start := math.Max(0, duration-450)
			control := func(seek, end float64) []uint32 {
				raw, err := runLimitedFiles(context.Background(), time.Minute, introskipper.MaxFingerprintPoints*4, ffmpeg, []*os.File{file}, "-v", "error", "-nostdin", "-ss", strconv.FormatFloat(seek, 'f', -1, 64), "-i", "/proc/self/fd/3", "-to", strconv.FormatFloat(end-seek, 'f', -1, 64), "-map", "0:"+strconv.Itoa(index)+"?", "-ac", "2", "-f", "chromaprint", "-fp_format", "raw", "-")
				if err != nil {
					t.Fatal(err)
				}
				points, err := parseIntroSkipperFingerprint(raw)
				if err != nil {
					t.Fatal(err)
				}
				return points
			}
			expected := control(start, duration)
			if !reflect.DeepEqual(result.RawFingerprint, expected) || math.Float64bits(result.FingerprintStartSeconds) != math.Float64bits(start) || math.Float64bits(result.FingerprintEndSeconds) != math.Float64bits(duration) {
				t.Fatalf("native recipe changed: actual=%d expected=%d window=[%.17g,%.17g]", len(result.RawFingerprint), len(expected), result.FingerprintStartSeconds, result.FingerprintEndSeconds)
			}
			if start > 0 && reflect.DeepEqual(result.RawFingerprint, control(0, 450)) {
				t.Fatal("tail fixture did not distinguish the wrong prefix window")
			}
			if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 19 {
				t.Fatalf("borrowed offset changed: %d %v", position, err)
			}
			encoded := make([]byte, len(result.RawFingerprint)*4)
			for index, word := range result.RawFingerprint {
				binary.LittleEndian.PutUint32(encoded[index*4:], word)
			}
			hash := sha256.Sum256(encoded)
			t.Logf("credits-recipe-parity case=%s words=%d sha256=%x start=%.17g end=%.17g ffmpeg=%s", test.name, len(result.RawFingerprint), hash, result.FingerprintStartSeconds, result.FingerprintEndSeconds, result.FFmpegSHA256)
		})
	}
}
