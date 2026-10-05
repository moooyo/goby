package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

func TestCreditsSkipperDescriptorExtractionPreservesRawAndWindow(t *testing.T) {
	tool := introSkipperTestTool(t, "cat /proc/self/fd/3")
	path := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(path, []byte{0x78, 0x56, 0x34, 0x12, 0xff, 0xff, 0xff, 0xff}, 0600); err != nil {
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
	if err := os.WriteFile(path, []byte("unrelated replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	options := introskipper.DefaultOptions()
	options.AnalysisPercent = 1
	options.AnalysisLengthLimit = 1
	info := Info{DurationTicks: 12345678901, Streams: []Stream{{Index: 2, CodecType: "audio"}}}
	result, err := (AnalysisExtractor{IntroFFmpegPath: tool, FFmpegPath: "/absent/main", FFprobePath: "/absent/probe", FingerprintPath: "/absent/helper"}).ExtractCreditsSkipper(context.Background(), file, info, IntroSkipperAnalysisRequest{AudioStreamIndex: 2, Options: options})
	if err != nil || !reflect.DeepEqual(result.RawFingerprint, []uint32{0x12345678, 0xffffffff}) || result.SourceIdentity == "" || result.FFmpegSHA256 == "" {
		t.Fatalf("descriptor extraction: %+v %v", result, err)
	}
	if math.Float64bits(result.FingerprintStartSeconds) != math.Float64bits(introskipper.CreditsFingerprintStartSeconds(info.DurationTicks)) || math.Float64bits(result.FingerprintEndSeconds) != math.Float64bits(float64(info.DurationTicks)/float64(TicksPerSecond)) {
		t.Fatal("intro settings shortened credits or changed the native clock")
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 3 {
		t.Fatalf("borrowed descriptor offset changed: %d %v", position, err)
	}
}

func TestCreditsSkipperExtractionNeverReturnsPartialOrChangedToolOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info := Info{DurationTicks: 500 * TicksPerSecond, Streams: []Stream{{Index: 0, CodecType: "audio"}}}
	request := IntroSkipperAnalysisRequest{Options: introskipper.DefaultOptions()}
	for _, test := range []struct {
		name, script string
		expected     error
	}{
		{"empty", "true", ErrIntroSkipperFingerprintUnavailable},
		{"partial-word", "printf '\\001'", ErrAnalysisUnproven},
		{"over-budget", "head -c 20004 /dev/zero", ErrAnalysisBudget},
		{"child-failure", "printf '\\001\\000\\000\\000'; exit 1", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := introSkipperTestTool(t, test.script)
			result, err := (AnalysisExtractor{IntroFFmpegPath: tool}).ExtractCreditsSkipper(context.Background(), file, info, request)
			if err == nil || result.RawFingerprint != nil || test.expected != nil && !errors.Is(err, test.expected) {
				t.Fatalf("failed extraction retained output: %+v %v", result, err)
			}
		})
	}
	tool := introSkipperTestTool(t, "printf '\\001\\000\\000\\000'; sleep 60")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if result, err := (AnalysisExtractor{IntroFFmpegPath: tool}).ExtractCreditsSkipper(ctx, file, info, request); !errors.Is(err, context.DeadlineExceeded) || result.RawFingerprint != nil {
		t.Fatalf("timeout retained partial output: %+v %v", result, err)
	}
	tool = introSkipperTestTool(t, "printf '\\001\\000\\000\\000'")
	contents, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	if err := os.WriteFile(tool, append(contents, []byte("\n# changed\n")...), 0700); err != nil {
		t.Fatal(err)
	}
	if result, err := (AnalysisExtractor{IntroFFmpegPath: tool, ExpectedIntroFFmpegSHA256: hex.EncodeToString(hash[:])}).ExtractCreditsSkipper(context.Background(), file, info, request); !errors.Is(err, ErrAnalysisUnavailable) || result.RawFingerprint != nil {
		t.Fatalf("changed executable accepted: %+v %v", result, err)
	}
}
