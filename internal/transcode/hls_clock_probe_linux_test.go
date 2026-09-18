//go:build linux

package transcode

import (
	"bytes"
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

const (
	hlsClockProbeHelperPrefix = "goby-hls-clock-test-helper-"
	hlsClockInitData          = "complete initialization bytes|"
	hlsClockSegmentData       = "complete media segment bytes"
	hlsClockValidPacket       = "packet|pts_time=1.483333|flags=K__\n"
)

func TestMeasureHLSMuxClockBorrowsWholeInputsAndFiltersCredentials(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "synthetic-database-secret")
	t.Setenv("GOBY_SETUP_TOKEN", "synthetic-setup-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-cloud-secret")
	t.Setenv("FFREPORT", "file=unexpected-report.txt")
	for _, mode := range []string{"valid-video-init", "valid-video", "valid-audio"} {
		t.Run(mode, func(t *testing.T) {
			segment := hlsClockTestFile(t, []byte(hlsClockSegmentData))
			if _, err := segment.Seek(5, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			var initialization *os.File
			if mode == "valid-video-init" {
				initialization = hlsClockTestFile(t, []byte(hlsClockInitData))
				if _, err := initialization.Seek(3, io.SeekStart); err != nil {
					t.Fatal(err)
				}
			}
			got, err := MeasureHLSMuxClock(context.Background(), hlsClockHelperExecutable(t, mode), initialization, segment, mode != "valid-audio")
			if err != nil || got != 14_833_330 {
				t.Fatalf("clock = %d, %v", got, err)
			}
			hlsClockAssertOffset(t, segment, 5)
			if initialization != nil {
				hlsClockAssertOffset(t, initialization, 3)
			}
		})
	}
}

func TestMeasureHLSMuxClockRetainsFirstPacketClock(t *testing.T) {
	for _, test := range []struct {
		mode  string
		video bool
		want  int64
	}{
		{mode: "negative", video: true, want: -10_000_010},
		{mode: "zero", video: true, want: 0},
		{mode: "precision", video: true, want: 1},
		{mode: "unterminated", video: true, want: 14_833_330},
		{mode: "first-retained", video: true, want: 14_833_330},
		{mode: "missing-flags-audio", video: false, want: 14_833_330},
		{mode: "priming-audio", video: false, want: 14_833_330},
	} {
		t.Run(test.mode, func(t *testing.T) {
			got, err := MeasureHLSMuxClock(context.Background(), hlsClockHelperExecutable(t, test.mode), nil,
				hlsClockTestFile(t, []byte(hlsClockSegmentData)), test.video)
			if err != nil || got != test.want {
				t.Fatalf("clock = %d, %v; want %d", got, err, test.want)
			}
		})
	}
}

func TestMeasureHLSMuxClockRejectsInvalidFirstPackets(t *testing.T) {
	for _, mode := range []string{
		"empty-output", "non-key", "missing-flags", "invalid-flags", "missing-pts", "unknown-pts",
		"exponent-pts", "overflow-pts", "excess-precision-pts", "failure",
	} {
		t.Run(mode, func(t *testing.T) {
			_, err := MeasureHLSMuxClock(context.Background(), hlsClockHelperExecutable(t, mode), nil,
				hlsClockTestFile(t, []byte(hlsClockSegmentData)), true)
			if !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("invalid first packet: %v", err)
			}
		})
	}
}

func TestMeasureHLSMuxClockBoundsAllOutputAfterFirstPacket(t *testing.T) {
	for _, mode := range []string{"stdout-line-limit", "stderr-line-limit", "stdout-total-limit", "combined-output-limit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := MeasureHLSMuxClock(ctx, hlsClockHelperExecutable(t, mode), nil,
				hlsClockTestFile(t, []byte(hlsClockSegmentData)), true)
			if !errors.Is(err, ErrTimelineLimit) {
				t.Fatalf("unbounded probe output: %v", err)
			}
		})
	}
}

func TestMeasureHLSMuxClockRejectsInvalidBorrowedInputs(t *testing.T) {
	for _, slot := range []string{"init", "segment"} {
		for _, kind := range []string{"nil", "closed", "directory", "pipe", "empty"} {
			if slot == "init" && kind == "nil" {
				continue
			}
			t.Run(slot+"/"+kind, func(t *testing.T) {
				invalid := hlsClockInvalidFile(t, kind)
				var initialization *os.File
				segment := hlsClockTestFile(t, []byte(hlsClockSegmentData))
				if slot == "init" {
					initialization = invalid
				} else {
					segment = invalid
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := MeasureHLSMuxClock(ctx, hlsClockHelperExecutable(t, "valid-video"), initialization, segment, true)
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("invalid borrowed input: %v", err)
				}
			})
		}
	}
}

func TestMeasureHLSMuxClockLimitsCombinedSparseInputSize(t *testing.T) {
	for _, withInit := range []bool{false, true} {
		t.Run(fmt.Sprintf("with-init-%t", withInit), func(t *testing.T) {
			segment := hlsClockTestFile(t, []byte(hlsClockSegmentData))
			segmentSize := int64((64 << 20) + 1)
			var initialization *os.File
			if withInit {
				initialization = hlsClockTestFile(t, []byte(hlsClockInitData))
				if err := os.Truncate(initialization.Name(), 32<<20); err != nil {
					t.Fatal(err)
				}
				segmentSize -= 32 << 20
			}
			if err := os.Truncate(segment.Name(), segmentSize); err != nil {
				t.Fatal(err)
			}
			_, err := MeasureHLSMuxClock(context.Background(), hlsClockHelperExecutable(t, "valid-video"), initialization, segment, true)
			if !errors.Is(err, ErrTimelineLimit) {
				t.Fatalf("oversized combined input: %v", err)
			}
		})
	}
}

func TestMeasureHLSMuxClockChecksCancellationAndExecutable(t *testing.T) {
	segment := hlsClockTestFile(t, []byte(hlsClockSegmentData))
	if _, err := segment.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureHLSMuxClock(ctx, "unused", nil, segment, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled context: %v", err)
	}
	for _, executable := range []string{"", "invalid\nexecutable", filepath.Join(t.TempDir(), "missing-ffprobe")} {
		if _, err := MeasureHLSMuxClock(context.Background(), executable, nil, segment, true); !errors.Is(err, ErrStart) {
			t.Errorf("invalid executable %q: %v", executable, err)
		}
	}
	hlsClockAssertOffset(t, segment, 5)
}

func TestMeasureHLSMuxClockRejectsChangedSourceFiles(t *testing.T) {
	for _, slot := range []string{"init", "segment"} {
		t.Run(slot, func(t *testing.T) {
			target := hlsClockTestFile(t, []byte("placeholder"))
			if err := os.WriteFile(target.Name(), []byte(target.Name()+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var initialization *os.File
			segment := target
			if slot == "init" {
				initialization, segment = target, hlsClockTestFile(t, []byte(hlsClockSegmentData))
			}
			_, err := MeasureHLSMuxClock(context.Background(), hlsClockHelperExecutable(t, "mutate-source"), initialization, segment, true)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("changed borrowed source: %v", err)
			}
			hlsClockAssertOffset(t, target, 0)
		})
	}
}

func TestMeasureHLSMuxClockCancellationRetiresDescendantProcesses(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "clock-child.pid")
	segment := hlsClockTestFile(t, []byte(pidFile))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	executable := hlsClockHelperExecutable(t, "parent")
	go func() {
		_, err := MeasureHLSMuxClock(ctx, executable, nil, segment, true)
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
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled probe: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not wait for the probe to exit")
	}
	if childPID == 0 {
		t.Fatal("probe descendant did not start")
	}
	assertProcessStopped(t, childPID)
	hlsClockAssertOffset(t, segment, 0)
}

func TestMeasureHLSMuxClockHonorsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := MeasureHLSMuxClock(ctx, hlsClockHelperExecutable(t, "stall"), nil,
		hlsClockTestFile(t, []byte(hlsClockSegmentData)), true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline: %v", err)
	}
}

func TestDuplicateInputBorrowsRegularFileAndSetsCloseOnExec(t *testing.T) {
	input := hlsClockTestFile(t, []byte(hlsClockSegmentData))
	if _, err := input.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	duplicate, err := DuplicateInput(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = duplicate.Close() })
	if duplicate.Fd() == input.Fd() {
		t.Fatal("duplicate reused the caller's descriptor")
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, duplicate.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("duplicate lacks FD_CLOEXEC: flags=%d, errno=%v", flags, errno)
	}
	hlsClockAssertOffset(t, duplicate, 5)
	data := make([]byte, len(hlsClockSegmentData))
	if _, err := duplicate.ReadAt(data, 0); err != nil || string(data) != hlsClockSegmentData {
		t.Fatalf("duplicate lost original input: %q, %v", data, err)
	}
	if err := duplicate.Close(); err != nil {
		t.Fatal(err)
	}
	hlsClockAssertOffset(t, input, 5)
	if _, err := input.ReadAt(data, 0); err != nil || string(data) != hlsClockSegmentData {
		t.Fatalf("closing duplicate affected caller: %q, %v", data, err)
	}
}

func TestDuplicateInputSurvivesCallerClose(t *testing.T) {
	input := hlsClockTestFile(t, []byte(hlsClockSegmentData))
	if _, err := input.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	duplicate, err := DuplicateInput(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = duplicate.Close() })
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(hlsClockSegmentData))
	if _, err := duplicate.ReadAt(data, 0); err != nil || string(data) != hlsClockSegmentData {
		t.Fatalf("closing caller affected duplicate: %q, %v", data, err)
	}
	hlsClockAssertOffset(t, duplicate, 3)
}

func TestDuplicateInputRejectsInvalidFiles(t *testing.T) {
	for _, kind := range []string{"nil", "closed", "directory", "pipe"} {
		t.Run(kind, func(t *testing.T) {
			duplicate, err := DuplicateInput(hlsClockInvalidFile(t, kind))
			if duplicate != nil {
				_ = duplicate.Close()
				t.Error("invalid input produced an owned descriptor")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid duplicate input: %v", err)
			}
		})
	}
}

func hlsClockTestFile(t *testing.T, content []byte) *os.File {
	t.Helper()
	name := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(name, content, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func hlsClockInvalidFile(t *testing.T, kind string) *os.File {
	t.Helper()
	switch kind {
	case "nil":
		return nil
	case "closed":
		file := hlsClockTestFile(t, []byte(hlsClockSegmentData))
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		return file
	case "directory":
		file, err := os.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		return file
	case "pipe":
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
		return reader
	case "empty":
		return hlsClockTestFile(t, nil)
	default:
		t.Fatalf("unknown invalid input kind %q", kind)
		return nil
	}
}

func hlsClockAssertOffset(t *testing.T, file *os.File, want int64) {
	t.Helper()
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != want {
		t.Fatalf("borrowed descriptor moved or closed: offset=%d, want=%d, err=%v", position, want, err)
	}
}

func hlsClockHelperExecutable(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), hlsClockProbeHelperPrefix+mode)
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func init() {
	if mode, ok := strings.CutPrefix(filepath.Base(os.Args[0]), hlsClockProbeHelperPrefix); ok {
		os.Exit(runHLSClockProbeHelper(mode))
	}
}

func runHLSClockProbeHelper(mode string) int {
	if mode == "child" {
		if len(os.Args) != 2 || os.WriteFile(os.Args[1], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 80
		}
		for {
			time.Sleep(time.Second)
		}
	}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOBY_") || strings.HasPrefix(entry, "AWS_") || strings.HasPrefix(entry, "FFREPORT=") {
			return 81
		}
	}
	if !hlsClockHelperArguments(os.Args[1:], strings.HasSuffix(mode, "audio")) {
		return 82
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 83
	}
	if mode != "parent" && mode != "mutate-source" {
		want := hlsClockSegmentData
		if mode == "valid-video-init" {
			want = hlsClockInitData + want
		}
		if string(input) != want {
			return 84
		}
	}
	switch mode {
	case "valid-video", "valid-video-init":
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
	case "valid-audio":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.483333|flags=___\n")
	case "priming-audio":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.483333|flags=KD_\n")
	case "negative":
		fmt.Fprint(os.Stdout, "packet|pts_time=-1.000001|flags=K__\n")
	case "zero":
		fmt.Fprint(os.Stdout, "packet|pts_time=0|flags=K__\n")
	case "precision":
		fmt.Fprint(os.Stdout, "packet|pts_time=0.0000001|flags=K__\n")
	case "unterminated":
		fmt.Fprint(os.Stdout, strings.TrimSuffix(hlsClockValidPacket, "\n"))
	case "first-retained":
		fmt.Fprint(os.Stdout, hlsClockValidPacket+"packet|pts_time=N/A|flags=___\npacket|pts_time=-9.000000|flags=___\n")
	case "missing-flags-audio", "missing-flags":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.483333\n")
	case "empty-output":
	case "non-key":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.483333|flags=___\n")
	case "invalid-flags":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.483333|flags=KD_\n")
	case "missing-pts":
		fmt.Fprint(os.Stdout, "packet|flags=K__\n")
	case "unknown-pts":
		fmt.Fprint(os.Stdout, "packet|pts_time=N/A|flags=K__\n")
	case "exponent-pts":
		fmt.Fprint(os.Stdout, "packet|pts_time=1e6|flags=K__\n")
	case "overflow-pts":
		fmt.Fprint(os.Stdout, "packet|pts_time=922337203685.4775808|flags=K__\n")
	case "excess-precision-pts":
		fmt.Fprint(os.Stdout, "packet|pts_time=1.00000001|flags=K__\n")
	case "failure":
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
		return 85
	case "stdout-line-limit", "stderr-line-limit":
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
		output := os.Stdout
		if mode == "stderr-line-limit" {
			output = os.Stderr
		}
		fmt.Fprintln(output, strings.Repeat("x", 4097))
	case "stdout-total-limit":
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("\n"), 1<<20))
	case "combined-output-limit":
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("\n"), 1<<19))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("\n"), 1<<19))
	case "mutate-source":
		path, _, found := strings.Cut(string(input), "\n")
		if !found {
			return 86
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return 87
		}
		_, writeErr := file.Write([]byte("changed"))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return 88
		}
		fmt.Fprint(os.Stdout, hlsClockValidPacket)
	case "parent":
		child := exec.Command("/proc/self/exe")
		child.Args = []string{hlsClockProbeHelperPrefix + "child", string(input)}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 89
		}
		_ = child.Wait()
	case "stall":
		for {
			time.Sleep(time.Second)
		}
	default:
		return 90
	}
	return 0
}

func hlsClockHelperArguments(args []string, audio bool) bool {
	stream := "v:0"
	if audio {
		stream = "a:0"
	}
	required := map[string]string{
		"-protocol_whitelist": "pipe", "-format_whitelist": inputFormats, "-select_streams": stream,
		"-read_intervals": "%+#8192", "-show_entries": "packet=pts_time,flags", "-of": "compact=p=1:nk=0", "-i": "pipe:0",
	}
	optional := map[string]string{"-v": "error", "-threads": "1"}
	seen := make(map[string]bool)
	if len(args)%2 != 0 {
		return false
	}
	for index := 0; index < len(args); index += 2 {
		flag, value := args[index], args[index+1]
		if seen[flag] {
			return false
		}
		seen[flag] = true
		want, found := required[flag]
		if !found {
			want, found = optional[flag]
		}
		if !found || value != want {
			return false
		}
	}
	for flag := range required {
		if !seen[flag] {
			return false
		}
	}
	return true
}
