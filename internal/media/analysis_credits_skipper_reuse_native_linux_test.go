package media

import (
	"context"
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

func TestCreditsSkipperReuseProofActualNativeMetadataParity(t *testing.T) {
	ffprobe, fixtureFFmpeg := audioProbeIntegrationTools(t)
	ffmpeg := os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG")
	if ffmpeg == "" {
		ffmpeg = fixtureFFmpeg
	}
	for _, fixture := range []struct {
		name          string
		durationTicks int64
	}{{"short", 25*TicksPerSecond + 1_250_000}, {"tail", 475*TicksPerSecond + 1_250_000}} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credits-reuse.wav")
			duration := float64(fixture.durationTicks) / float64(TicksPerSecond)
			audioProbeRunFFmpeg(t, fixtureFFmpeg, "-f", "lavfi", "-i", "aevalsrc=0.3*sin(2*PI*(180*t+0.75*t*t)):s=16000:d="+strconv.FormatFloat(duration, 'f', -1, 64), "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "wav", path)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil || info.DurationTicks != fixture.durationTicks {
				t.Fatalf("native fixture duration = %d, want %d: %v", info.DurationTicks, fixture.durationTicks, err)
			}
			index, ok := SelectIntroSkipperAudioStream(info, "", true)
			if !ok {
				t.Fatal("native fixture audio absent")
			}
			if _, err := file.Seek(19, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			extractor := AnalysisExtractor{IntroFFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: time.Minute}}
			request := IntroSkipperAnalysisRequest{AudioStreamIndex: index, Options: introskipper.DefaultOptions()}
			extracted, err := extractor.ExtractCreditsSkipper(context.Background(), file, info, request)
			if err != nil || len(extracted.RawFingerprint) == 0 || extracted.AlgorithmProfile == "" || extracted.SourceIdentity == "" || extracted.FFmpegSHA256 == "" {
				t.Fatalf("native extraction: %+v %v", extracted, err)
			}
			extractor.ExpectedIntroFFmpegSHA256 = extracted.FFmpegSHA256
			proof, err := extractor.CheckCreditsSkipper(context.Background(), file, info, request)
			if err != nil {
				t.Fatal(err)
			}
			words := len(extracted.RawFingerprint)
			extracted.RawFingerprint = nil
			if !reflect.DeepEqual(proof, extracted) || math.Float64bits(proof.FingerprintStartSeconds) != math.Float64bits(math.Max(0, duration-450)) || math.Float64bits(proof.FingerprintEndSeconds) != math.Float64bits(duration) {
				t.Fatalf("native proof did not preserve extraction metadata: proof=%+v extraction=%+v", proof, extracted)
			}
			if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 19 {
				t.Fatalf("native proof changed borrowed descriptor offset: %d %v", position, err)
			}
			t.Logf("credits-reuse-native-proof case=%s words=%d start=%.17g end=%.17g ffmpeg=%s", fixture.name, words, proof.FingerprintStartSeconds, proof.FingerprintEndSeconds, proof.FFmpegSHA256)
		})
	}
}
