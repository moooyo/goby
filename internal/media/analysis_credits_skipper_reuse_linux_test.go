package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

type creditsSkipperReuseTestFIFO struct {
	path   string
	file   *os.File
	reader *bufio.Reader
}

func newCreditsSkipperReuseTestFIFO(t *testing.T) *creditsSkipperReuseTestFIFO {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe-events")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return &creditsSkipperReuseTestFIFO{path: path, file: file, reader: bufio.NewReader(file)}
}

func (fifo *creditsSkipperReuseTestFIFO) line(t *testing.T) string {
	t.Helper()
	if err := fifo.file.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := fifo.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read controlled probe event: %v", err)
	}
	return strings.TrimSuffix(line, "\n")
}

func creditsSkipperReuseTestQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// Child readiness uses an owned FIFO because analysis intentionally applies
// RLIMIT_FSIZE=0. Only the test process writes ordinary switch/source files.
func creditsSkipperReuseTestTool(t *testing.T, before, raw string) string {
	t.Helper()
	body := `case "$1" in
-version) probe=version;;
-hide_banner)
  case "$2" in
  -h) [ "$3" = 'muxer=chromaprint' ]; probe=muxer;;
  -encoders) probe=encoders;;
  *) probe=raw;;
  esac;;
*) exit 90;;
esac
` + before + `
case "$probe" in
version) printf 'ffmpeg version test\n';;
muxer) cat <<'MUXER'
` + introSkipperTestMuxer + `MUXER
;;
encoders) printf ' A....D pcm_s16le PCM signed 16-bit little-endian\n';;
raw)
` + raw + `
;;
esac`
	return analysisProcessTestTool(t, body)
}

func creditsSkipperReuseTestSource(t *testing.T) (*os.File, Info, IntroSkipperAnalysisRequest) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(path, []byte{0x78, 0x56, 0x34, 0x12, 0xff, 0xff, 0xff, 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info := Info{DurationTicks: 500*TicksPerSecond + 1_250_000, Streams: []Stream{{Index: 2, CodecType: "audio"}}}
	request := IntroSkipperAnalysisRequest{AudioStreamIndex: 2, Options: introskipper.DefaultOptions()}
	return file, info, request
}

func creditsSkipperReuseTestAssertNoToolDescriptor(t *testing.T, toolInfo os.FileInfo) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(toolInfo, info) {
			t.Fatalf("proof retained executable descriptor %s", entry.Name())
		}
	}
}

type creditsSkipperReuseTestResult struct {
	features CreditsSkipperFeatures
	err      error
}

type creditsSkipperReuseTestRun struct {
	cancel   context.CancelFunc
	result   chan creditsSkipperReuseTestResult
	finished chan struct{}
}

func startCreditsSkipperReuseTest(t *testing.T, extractor AnalysisExtractor, file *os.File, info Info, request IntroSkipperAnalysisRequest) creditsSkipperReuseTestRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	run := creditsSkipperReuseTestRun{cancel: cancel, result: make(chan creditsSkipperReuseTestResult, 1), finished: make(chan struct{})}
	go func() {
		features, err := extractor.CheckCreditsSkipper(ctx, file, info, request)
		run.result <- creditsSkipperReuseTestResult{features: features, err: err}
		close(run.finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-run.finished:
		case <-time.After(5 * time.Second):
			t.Error("credits proof did not retire during cleanup")
		}
	})
	return run
}

func (run creditsSkipperReuseTestRun) complete(t *testing.T) creditsSkipperReuseTestResult {
	t.Helper()
	select {
	case result := <-run.result:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("credits proof did not retire after its controlled probe")
	}
	return creditsSkipperReuseTestResult{}
}

func TestCreditsSkipperReuseProofRepeatsAllCapabilitiesWithoutDecoding(t *testing.T) {
	file, info, request := creditsSkipperReuseTestSource(t)
	events := newCreditsSkipperReuseTestFIFO(t)
	decodeAllowed := filepath.Join(t.TempDir(), "decode-allowed")
	if err := os.WriteFile(decodeAllowed, nil, 0600); err != nil {
		t.Fatal(err)
	}
	tool := creditsSkipperReuseTestTool(t, "printf '%s\\n' \"$probe\" > "+creditsSkipperReuseTestQuote(events.path),
		"[ -e "+creditsSkipperReuseTestQuote(decodeAllowed)+" ]\ncat /proc/self/fd/3")
	extractor := AnalysisExtractor{IntroFFmpegPath: tool, FFmpegPath: "/absent/main", FFprobePath: "/absent/probe", FingerprintPath: "/absent/helper"}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	extracted, err := extractor.ExtractCreditsSkipper(context.Background(), file, info, request)
	if err != nil || !reflect.DeepEqual(extracted.RawFingerprint, []uint32{0x12345678, 0xffffffff}) || extracted.AlgorithmProfile == "" || extracted.SourceIdentity == "" || extracted.FFmpegSHA256 == "" {
		t.Fatalf("initial extraction: %+v %v", extracted, err)
	}
	for _, expected := range []string{"version", "muxer", "encoders", "raw"} {
		if got := events.line(t); got != expected {
			t.Fatalf("initial extraction probe = %q, want %q", got, expected)
		}
	}
	if err := os.Remove(decodeAllowed); err != nil {
		t.Fatal(err)
	}
	extractor.ExpectedIntroFFmpegSHA256 = extracted.FFmpegSHA256
	extracted.RawFingerprint = nil
	for range 2 {
		proof, err := extractor.CheckCreditsSkipper(context.Background(), file, info, request)
		if err != nil || !reflect.DeepEqual(proof, extracted) {
			t.Fatalf("proof did not preserve extraction metadata without decoding: %+v %v", proof, err)
		}
		for _, expected := range []string{"version", "muxer", "encoders"} {
			if got := events.line(t); got != expected {
				t.Fatalf("current proof probe = %q, want %q", got, expected)
			}
		}
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 3 {
		t.Fatalf("proof changed borrowed descriptor offset: %d %v", position, err)
	}
	toolInfo, err := os.Stat(tool)
	if err != nil {
		t.Fatal(err)
	}
	creditsSkipperReuseTestAssertNoToolDescriptor(t, toolInfo)
}

func TestCreditsSkipperReuseProofRejectsRemovedOrReplacedCurrentTool(t *testing.T) {
	for _, mutation := range []string{"removed", "replaced"} {
		t.Run(mutation, func(t *testing.T) {
			file, info, request := creditsSkipperReuseTestSource(t)
			tool := introSkipperTestTool(t, "printf '\\001\\000\\000\\000'")
			extractor := AnalysisExtractor{IntroFFmpegPath: tool}
			initial, err := extractor.ExtractCreditsSkipper(context.Background(), file, info, request)
			if err != nil || len(initial.RawFingerprint) == 0 {
				t.Fatalf("initial extraction: %+v %v", initial, err)
			}
			extractor.ExpectedIntroFFmpegSHA256 = initial.FFmpegSHA256
			if err := os.Remove(tool); err != nil {
				t.Fatal(err)
			}
			if mutation == "replaced" {
				if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			proof, err := extractor.CheckCreditsSkipper(context.Background(), file, info, request)
			if !errors.Is(err, ErrAnalysisUnavailable) || !reflect.DeepEqual(proof, CreditsSkipperFeatures{}) {
				t.Fatalf("changed current executable retained reusable proof: %+v %v", proof, err)
			}
		})
	}
}

func TestCreditsSkipperReuseProofRejectsCurrentCapabilityFailureWithIdenticalToolBytes(t *testing.T) {
	for _, probe := range []string{"version", "muxer", "encoders"} {
		t.Run(probe, func(t *testing.T) {
			file, info, request := creditsSkipperReuseTestSource(t)
			dependency := filepath.Join(t.TempDir(), "capability-dependency")
			if err := os.WriteFile(dependency, nil, 0600); err != nil {
				t.Fatal(err)
			}
			tool := creditsSkipperReuseTestTool(t, "if [ \"$probe\" = "+creditsSkipperReuseTestQuote(probe)+" ]; then [ -e "+creditsSkipperReuseTestQuote(dependency)+" ]; fi",
				"printf '\\001\\000\\000\\000'")
			original, err := os.ReadFile(tool)
			if err != nil {
				t.Fatal(err)
			}
			extractor := AnalysisExtractor{IntroFFmpegPath: tool}
			initial, err := extractor.ExtractCreditsSkipper(context.Background(), file, info, request)
			if err != nil || len(initial.RawFingerprint) == 0 {
				t.Fatalf("initial extraction: %+v %v", initial, err)
			}
			extractor.ExpectedIntroFFmpegSHA256 = initial.FFmpegSHA256
			if err := os.Remove(dependency); err != nil {
				t.Fatal(err)
			}
			proof, err := extractor.CheckCreditsSkipper(context.Background(), file, info, request)
			if !errors.Is(err, ErrAnalysisUnavailable) || !reflect.DeepEqual(proof, CreditsSkipperFeatures{}) {
				t.Fatalf("failed current capability retained reusable proof: %+v %v", proof, err)
			}
			current, err := os.ReadFile(tool)
			if err != nil || !bytes.Equal(current, original) {
				t.Fatalf("capability fixture changed executable bytes: %v", err)
			}
		})
	}
}

func TestCreditsSkipperReuseProofRejectsChangesDuringFinalCapabilityProbe(t *testing.T) {
	for _, mutation := range []string{"source", "tool-removed", "tool-replaced"} {
		t.Run(mutation, func(t *testing.T) {
			file, info, request := creditsSkipperReuseTestSource(t)
			ready, release := newCreditsSkipperReuseTestFIFO(t), newCreditsSkipperReuseTestFIFO(t)
			tool := creditsSkipperReuseTestTool(t, "if [ \"$probe\" = 'encoders' ]; then\nprintf 'ready\\n' > "+creditsSkipperReuseTestQuote(ready.path)+
				"\nIFS= read -r released < "+creditsSkipperReuseTestQuote(release.path)+"\nfi", "exit 91")
			toolInfo, err := os.Stat(tool)
			if err != nil {
				t.Fatal(err)
			}
			run := startCreditsSkipperReuseTest(t, AnalysisExtractor{IntroFFmpegPath: tool}, file, info, request)
			if got := ready.line(t); got != "ready" {
				t.Fatalf("unexpected readiness record %q", got)
			}
			if mutation == "source" {
				if err := os.WriteFile(file.Name(), []byte("source changed during the final probe"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(tool, tool+".retained"); err != nil {
					t.Fatal(err)
				}
				if mutation == "tool-replaced" {
					original, err := os.ReadFile(tool + ".retained")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(tool, original, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := release.file.WriteString("continue\n"); err != nil {
				t.Fatal(err)
			}
			result := run.complete(t)
			if result.err == nil || mutation != "source" && !errors.Is(result.err, ErrAnalysisUnavailable) || !reflect.DeepEqual(result.features, CreditsSkipperFeatures{}) {
				t.Fatalf("changed final proof retained metadata: %+v %v", result.features, result.err)
			}
			creditsSkipperReuseTestAssertNoToolDescriptor(t, toolInfo)
		})
	}
}

func TestCreditsSkipperReuseProofCancellationRetiresProbeGroupAndSlots(t *testing.T) {
	for _, probe := range []string{"version", "muxer", "encoders"} {
		t.Run(probe, func(t *testing.T) {
			file, info, request := creditsSkipperReuseTestSource(t)
			ready := newCreditsSkipperReuseTestFIFO(t)
			tool := creditsSkipperReuseTestTool(t, "if [ \"$probe\" = "+creditsSkipperReuseTestQuote(probe)+" ]; then\nsleep 60 &\nchild=$!\nprintf 'ready %s %s\\n' \"$$\" \"$child\" > "+
				creditsSkipperReuseTestQuote(ready.path)+"\nwait \"$child\"\nfi", "exit 91")
			toolInfo, err := os.Stat(tool)
			if err != nil {
				t.Fatal(err)
			}
			run := startCreditsSkipperReuseTest(t, AnalysisExtractor{IntroFFmpegPath: tool}, file, info, request)
			var ids analysisProcessTestPIDs
			line := ready.line(t)
			if count, err := fmt.Sscanf(line, "ready %d %d", &ids.leader, &ids.child); err != nil || count != 2 || ids.leader <= 1 || ids.child <= 1 || ids.leader == ids.child {
				t.Fatalf("invalid probe readiness record %q: %v", line, err)
			}
			if len(analysisSlots) != 1 {
				t.Fatal("active capability proof did not retain its analysis slot")
			}
			run.cancel()
			result := run.complete(t)
			if !errors.Is(result.err, context.Canceled) || !reflect.DeepEqual(result.features, CreditsSkipperFeatures{}) {
				t.Fatalf("canceled capability proof retained metadata: %+v %v", result.features, result.err)
			}
			analysisProcessTestAssertRetired(t, ids)
			creditsSkipperReuseTestAssertNoToolDescriptor(t, toolInfo)
			if len(analysisSlots) != 0 {
				t.Fatal("retired capability proof retained its analysis slot")
			}
			mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
		})
	}
}
