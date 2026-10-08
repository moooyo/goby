//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const generatedSourceRangeHelperPrefix = "goby-generated-source-range-helper-"

func generatedSourceRangeHelper(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), generatedSourceRangeHelperPrefix+mode)
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func init() {
	if mode, ok := strings.CutPrefix(filepath.Base(os.Args[0]), generatedSourceRangeHelperPrefix); ok {
		os.Exit(runGeneratedSourceRangeHelper(mode))
	}
}

func runGeneratedSourceRangeHelper(mode string) int {
	if mode == "child" {
		if len(os.Args) != 2 || os.WriteFile(os.Args[1], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 70
		}
		for {
			time.Sleep(time.Second)
		}
	}
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "GOBY_") || strings.HasPrefix(value, "AWS_") || strings.HasPrefix(value, "FFREPORT=") {
			return 71
		}
	}
	if !hasArgumentPair(os.Args[1:], "-select_streams", "0") || !hasArgumentPair(os.Args[1:], "-read_intervals", "92.0000000%98.0000000") ||
		!hasArgumentPair(os.Args[1:], "-fflags", "+nofillin-genpts") || !hasArgumentPair(os.Args[1:], "-i", "/proc/self/fd/3") {
		return 72
	}
	data, err := os.ReadFile("/proc/self/fd/3")
	if err != nil {
		return 73
	}
	document := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92000, 40, 150), "1/1000", "2.000000")
	switch mode {
	case "valid":
		fmt.Fprint(os.Stdout, document)
	case "failure":
		fmt.Fprint(os.Stdout, document)
		return 74
	case "stderr":
		fmt.Fprint(os.Stdout, document)
		fmt.Fprint(os.Stderr, "decoder diagnostic")
	case "bytes":
		fmt.Fprint(os.Stdout, strings.Repeat(" ", maxGeneratedSourceRangeBytes+1))
	case "frames":
		fmt.Fprint(os.Stdout, `{"frames":[`+generatedSourceRangeFrames(0, 40, maxGeneratedSourceRangeFrames+1))
	case "mutation":
		before, err := os.Stat(string(data))
		if err != nil || len(data) == 0 {
			return 75
		}
		beforeStat, ok := before.Sys().(*syscall.Stat_t)
		if !ok {
			return 75
		}
		file, err := os.OpenFile(string(data), os.O_WRONLY, 0)
		if err != nil {
			return 75
		}
		defer file.Close()
		// Keep size and restore mtime so only the independent ctime fence
		// distinguishes this changed source from its borrowed snapshot.
		changed := append([]byte(nil), data...)
		changed[0] ^= 1
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := file.WriteAt(changed, 0); err != nil {
				return 76
			}
			if err := os.Chtimes(string(data), before.ModTime(), before.ModTime()); err != nil {
				return 76
			}
			after, err := os.Stat(string(data))
			if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				return 76
			}
			afterStat, ok := after.Sys().(*syscall.Stat_t)
			if !ok {
				return 76
			}
			if afterStat.Ctim != beforeStat.Ctim {
				break
			}
			if time.Now().After(deadline) {
				// Exit 80 identifies a fixture that could not produce the ctime
				// transition required by this test, rather than valid evidence.
				return 80
			}
			// Filesystems can expose coarse clock quanta. Retry only until this
			// controlled same-size mutation has an observable ctime transition.
			time.Sleep(time.Millisecond)
		}
		if err := file.Close(); err != nil {
			return 76
		}
		fmt.Fprint(os.Stdout, document)
	case "parent", "successful-parent":
		child := exec.Command("/proc/self/exe")
		child.Args = []string{generatedSourceRangeHelperPrefix + "child", string(data)}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 77
		}
		if mode == "successful-parent" {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if pidData, err := os.ReadFile(string(data)); err == nil {
					if pid, err := strconv.Atoi(string(pidData)); err == nil && pid > 0 {
						fmt.Fprint(os.Stdout, document)
						return 0
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			return 78
		}
		_ = child.Wait()
	case "stall":
		for {
			time.Sleep(time.Second)
		}
	default:
		return 79
	}
	return 0
}

func TestMeasureGeneratedSourceRangeBorrowsSeekableDescriptor(t *testing.T) {
	t.Setenv("GOBY_SOURCE_RANGE_PRIVATE", "must not reach a parser")
	t.Setenv("AWS_SOURCE_RANGE_PRIVATE", "must not reach a parser")
	t.Setenv("FFREPORT", "must not reach a parser")
	input := hlsClockTestFile(t, []byte("authorized source bytes"))
	if _, err := input.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := MeasureGeneratedSourceRange(context.Background(), generatedSourceRangeHelper(t, "valid"), input, generatedSourceRangeTestPlan(), 2*ticksPerSecond)
	if err != nil || got.FrameCount != 150 || got.First != (GeneratedRational{Num: 90, Den: 1}) || got.End != (GeneratedRational{Num: 96, Den: 1}) {
		t.Fatalf("source range proof lost source-global clocks: %+v, %v", got, err)
	}
	hlsClockAssertOffset(t, input, 5)
}

func TestMeasureGeneratedSourceRangeRejectsFailedEvidenceAndChangedInput(t *testing.T) {
	for mode, want := range map[string]error{"failure": ErrTimelineProbe, "stderr": ErrTimelineProbe, "bytes": ErrTimelineLimit, "frames": ErrTimelineLimit, "mutation": ErrInvalidInput} {
		t.Run(mode, func(t *testing.T) {
			input := hlsClockTestFile(t, []byte("authorized source"))
			var mutationBefore, mutationAfter os.FileInfo
			if mode == "mutation" {
				if err := os.WriteFile(input.Name(), []byte(input.Name()), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				mutationBefore, err = input.Stat()
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := MeasureGeneratedSourceRange(context.Background(), generatedSourceRangeHelper(t, mode), input, generatedSourceRangeTestPlan(), 2*ticksPerSecond)
			if mode == "mutation" {
				var statErr error
				mutationAfter, statErr = input.Stat()
				if statErr != nil {
					t.Logf("mutation fixture before: size=%d mtime=%v stat=%+v", mutationBefore.Size(), mutationBefore.ModTime(), mutationBefore.Sys())
					t.Fatalf("mutation fixture final stat failed: %v; evidence=%+v probe_error=%v", statErr, got, err)
				}
				beforeStat, beforeOK := mutationBefore.Sys().(*syscall.Stat_t)
				afterStat, afterOK := mutationAfter.Sys().(*syscall.Stat_t)
				if !beforeOK || !afterOK || !os.SameFile(mutationBefore, mutationAfter) || mutationBefore.Size() != mutationAfter.Size() ||
					!mutationBefore.ModTime().Equal(mutationAfter.ModTime()) || beforeStat.Ctim == afterStat.Ctim {
					t.Logf("mutation fixture before: size=%d mtime=%v stat=%+v", mutationBefore.Size(), mutationBefore.ModTime(), mutationBefore.Sys())
					t.Logf("mutation fixture after: size=%d mtime=%v stat=%+v", mutationAfter.Size(), mutationAfter.ModTime(), mutationAfter.Sys())
					t.Fatalf("mutation fixture did not establish an isolated ctime change: evidence=%+v probe_error=%v", got, err)
				}
			}
			if got != (GeneratedSourceRange{}) || !errors.Is(err, want) {
				if mode == "mutation" {
					t.Logf("mutation fixture before: size=%d mtime=%v stat=%+v", mutationBefore.Size(), mutationBefore.ModTime(), mutationBefore.Sys())
					t.Logf("mutation fixture after: size=%d mtime=%v stat=%+v", mutationAfter.Size(), mutationAfter.ModTime(), mutationAfter.Sys())
				}
				t.Fatalf("failed source evidence escaped its fence: %+v, %v", got, err)
			}
			hlsClockAssertOffset(t, input, 0)
		})
	}
}

func TestMeasureGeneratedSourceRangeRetiresInheritedPipeHolders(t *testing.T) {
	for _, mode := range []string{"parent", "successful-parent"} {
		t.Run(mode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "descendant.pid")
			input := hlsClockTestFile(t, []byte(pidFile))
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			helper := generatedSourceRangeHelper(t, mode)
			go func() {
				_, err := MeasureGeneratedSourceRange(ctx, helper, input, generatedSourceRangeTestPlan(), 2*ticksPerSecond)
				done <- err
			}()
			var childPID int
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				if data, err := os.ReadFile(pidFile); err == nil {
					childPID, _ = strconv.Atoi(string(data))
					if childPID > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "parent" {
				cancel()
			}
			select {
			case err := <-done:
				if mode == "parent" && !errors.Is(err, context.Canceled) || mode == "successful-parent" && err != nil {
					t.Fatalf("group retirement did not preserve the evidence result: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("probe did not join its inherited output pipe holders")
			}
			if childPID == 0 {
				t.Fatal("descendant did not start")
			}
			assertProcessStopped(t, childPID)
			hlsClockAssertOffset(t, input, 0)
		})
	}
}

func TestMeasureGeneratedSourceRangeHonorsDeadlineAndInvalidBorrowers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := MeasureGeneratedSourceRange(ctx, generatedSourceRangeHelper(t, "stall"), hlsClockTestFile(t, []byte("source")), generatedSourceRangeTestPlan(), 2*ticksPerSecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline was not retained: %v", err)
	}
	for _, kind := range []string{"nil", "closed", "directory", "pipe", "empty"} {
		t.Run(kind, func(t *testing.T) {
			if _, err := MeasureGeneratedSourceRange(context.Background(), "unused", hlsClockInvalidFile(t, kind), generatedSourceRangeTestPlan(), 0); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid borrower reached source proof: %v", err)
			}
		})
	}
	if _, err := MeasureGeneratedSourceRange(nil, "unused", nil, generatedSourceRangeTestPlan(), 0); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil context was not rejected: %v", err)
	}
}

func TestMeasureGeneratedSourceRangeActualHighSeekAndTruncatedTail(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	for _, fixture := range []struct {
		name, codec, extension string
		fps, seconds           int
		origin                 int64
		missingOrigin          bool
		shortCoverage          bool
	}{
		{"mkv_25fps", "libx264", "mkv", 25, 102, 2 * ticksPerSecond, true, false},
		{"short_mkv", "libx264", "mkv", 25, 95, 2 * ticksPerSecond, true, false},
		{"mp4_24fps", "libx264", "mp4", 24, 102, 2 * ticksPerSecond, false, false},
		{"short_mp4", "libx264", "mp4", 24, 95, 2 * ticksPerSecond, false, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source."+fixture.extension)
			args := []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i",
				fmt.Sprintf("testsrc2=s=64x64:r=%d:d=%d", fixture.fps, fixture.seconds), "-c:v", fixture.codec, "-threads:v", "1"}
			if fixture.codec == "libx264" {
				args = append(args, "-bf", "0", "-g", strconv.Itoa(fixture.fps))
			}
			if fixture.origin != 0 {
				args = append(args, "-output_ts_offset", signedTickSeconds(fixture.origin))
			}
			generatedWindowMediaCommand(t, ctx, ffmpeg, append(args, path)...)
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			p := generatedSourceRangeTestPlan()
			p.FrameRate = float64(fixture.fps)
			got, err := MeasureGeneratedSourceRange(ctx, ffprobe, input, p, fixture.origin)
			if fixture.missingOrigin {
				// Strict nofillin probing leaves the MKV format origin absent.
				// A caller-provided origin must not manufacture that missing fact.
				if got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineProbe) {
					t.Fatalf("missing independent format origin became usable source coverage: %+v, %v", got, err)
				}
				return
			}
			if fixture.shortCoverage {
				if got != (GeneratedSourceRange{}) || !errors.Is(err, ErrInvalidTimeline) {
					t.Fatalf("truncated fixture did not provide the expected explicit source-range coverage rejection: %+v, %v", got, err)
				}
				return
			}
			if err != nil || got.FrameCount != int64(6*fixture.fps) || got.First != (GeneratedRational{Num: 90, Den: 1}) || got.End != (GeneratedRational{Num: 96, Den: 1}) {
				t.Fatalf("actual high-seek source range was not established: %+v, %v", got, err)
			}
			hlsClockAssertOffset(t, input, 0)
			if _, err := MeasureGeneratedSourceRange(ctx, ffprobe, input, p, fixture.origin+ticksPerSecond); err == nil {
				t.Fatal("a wrong format origin produced usable source coverage")
			}
		})
	}
}
