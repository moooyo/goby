package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

func introSkipperTestTool(t *testing.T, output string) string {
	t.Helper()
	body := "case \"$1\" in\n-version) printf 'ffmpeg version test\\n';;\n-hide_banner)\n if [ \"$2\" = '-h' ]; then\ncat <<'MUXER'\n" + introSkipperTestMuxer + "MUXER\nelif [ \"$2\" = '-encoders' ]; then\nprintf ' A....D pcm_s16le PCM signed 16-bit little-endian\\n'\nelse\n" + output + "\nfi;;\nesac"
	return analysisProcessTestTool(t, body)
}

func TestIntroSkipperAvailabilityDoesNotNeedLegacyDependencies(t *testing.T) {
	tool := introSkipperTestTool(t, "exit 1")
	e := AnalysisExtractor{IntroFFmpegPath: tool, FFmpegPath: "/absent/legacy", FFprobePath: "/absent/probe", FingerprintPath: "/absent/helper"}
	available, err := e.Availability(context.Background())
	if err != nil || !available.IntroSkipperAvailable || available.AudioAvailable || available.VisualAvailable || available.IntroFFmpegPath != tool || len(available.IntroFFmpegSHA256) != 64 {
		t.Fatalf("independent intro availability: %+v %v", available, err)
	}
	e.ExpectedIntroFFmpegSHA256 = strings.Repeat("a", 64)
	available, err = e.Availability(context.Background())
	if err != nil || available.IntroSkipperAvailable || available.IntroSkipperReason == "" {
		t.Fatalf("changed binary accepted: %+v %v", available, err)
	}
}

func TestIntroSkipperAvailabilityRejectsMuxerWithoutPCMEncoder(t *testing.T) {
	tool := introSkipperTestTool(t, "exit 1")
	data, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), " A....D pcm_s16le PCM signed 16-bit little-endian", " A....D pcm_f32le PCM floating-point"))
	if err := os.WriteFile(tool, data, 0700); err != nil {
		t.Fatal(err)
	}
	available, err := (AnalysisExtractor{IntroFFmpegPath: tool, FFmpegPath: "/absent/legacy"}).Availability(context.Background())
	if err != nil || available.IntroSkipperAvailable || available.IntroSkipperReason == "" {
		t.Fatalf("muxer without its encoder was admitted: %+v %v", available, err)
	}
}

func TestIntroSkipperDescriptorInputNeedsNoVisualOrSourcePTS(t *testing.T) {
	tool := introSkipperTestTool(t, "cat /proc/self/fd/3")
	path := filepath.Join(t.TempDir(), "source.bin")
	data := []byte{0x78, 0x56, 0x34, 0x12, 0xff, 0xff, 0xff, 0xff}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(path, path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info := Info{DurationTicks: 400 * TicksPerSecond, Streams: []Stream{{Index: 2, CodecType: "audio"}}}
	result, err := (AnalysisExtractor{IntroFFmpegPath: tool}).ExtractIntroSkipper(context.Background(), file, info,
		IntroSkipperAnalysisRequest{AudioStreamIndex: 2, Options: introskipper.DefaultOptions()})
	if err != nil || !reflect.DeepEqual(result.RawFingerprint, []uint32{0x12345678, 0xffffffff}) || result.FingerprintEndSeconds != 100 || result.SourceIdentity == "" {
		t.Fatalf("descriptor extraction: %+v %v", result, err)
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 3 {
		t.Fatalf("borrowed descriptor offset changed: %d %v", position, err)
	}
}

func TestIntroSkipperExtractionRejectsChangedToolAndCanceledContext(t *testing.T) {
	tool := introSkipperTestTool(t, "printf '\\001\\000\\000\\000'")
	raw, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	expected := hex.EncodeToString(digest[:])
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info := Info{DurationTicks: 10 * TicksPerSecond, Streams: []Stream{{Index: 0, CodecType: "audio"}}}
	request := IntroSkipperAnalysisRequest{Options: introskipper.DefaultOptions()}
	extractor := AnalysisExtractor{IntroFFmpegPath: tool, ExpectedIntroFFmpegSHA256: expected, Limits: AnalysisLimits{Timeout: time.Second}}
	if err := os.WriteFile(tool, append(raw, []byte("\n# changed\n")...), 0700); err != nil {
		t.Fatal(err)
	}
	if result, err := extractor.ExtractIntroSkipper(context.Background(), file, info, request); !errors.Is(err, ErrAnalysisUnavailable) || result.RawFingerprint != nil {
		t.Fatalf("changed tool result: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := extractor.ExtractIntroSkipper(ctx, file, info, request); !errors.Is(err, context.Canceled) || result.RawFingerprint != nil {
		t.Fatalf("canceled result: %+v %v", result, err)
	}
}

func TestIntroSkipperExtractionNeverPublishesFailedOrPartialOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info := Info{DurationTicks: 10 * TicksPerSecond, Streams: []Stream{{Index: 0, CodecType: "audio"}}}
	request := IntroSkipperAnalysisRequest{Options: introskipper.DefaultOptions()}
	for _, test := range []struct {
		name, output string
		expected     error
	}{
		{"empty", "true", ErrIntroSkipperFingerprintUnavailable},
		{"partial-word", "printf '\\001'", ErrAnalysisUnproven},
		{"over-budget", "head -c 20004 /dev/zero", ErrAnalysisBudget},
		{"child-failure", "printf '\\001\\000\\000\\000'; exit 1", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := introSkipperTestTool(t, test.output)
			result, err := (AnalysisExtractor{IntroFFmpegPath: tool}).ExtractIntroSkipper(context.Background(), file, info, request)
			if err == nil || result.RawFingerprint != nil || test.expected != nil && !errors.Is(err, test.expected) {
				t.Fatalf("failed extraction returned a candidate input: %+v %v", result, err)
			}
		})
	}
	tool := introSkipperTestTool(t, "printf '\\001\\000\\000\\000'; sleep 60")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := (AnalysisExtractor{IntroFFmpegPath: tool}).ExtractIntroSkipper(ctx, file, info, request)
	if !errors.Is(err, context.DeadlineExceeded) || result.RawFingerprint != nil {
		t.Fatalf("timed-out extraction retained partial output: %+v %v", result, err)
	}
}
