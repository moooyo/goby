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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProgressWriterBoundsIndividualLinesWithoutLifetimeLimit(t *testing.T) {
	count := 0
	var last Progress
	w := &progressWriter{callback: func(p Progress) { count++; last = p }}
	block := []byte("frame=15\nout_time_us=1234567\ntotal_size=2048\nprogress=continue\n")
	for range 100_000 {
		if _, err := w.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("out_time_us=2000000\ntotal_size=N/A\nprogress=end\n")); err != nil {
		t.Fatal(err)
	}
	if count != 100_001 || last.OutputTicks != 20_000_000 || last.Bytes != 2048 || !last.Ended || w.length != 0 {
		t.Fatalf("unexpected final progress: count=%d, %+v", count, last)
	}
	for name, input := range map[string]string{
		"long line":     strings.Repeat("x", maxProgressLine+1),
		"overflow time": "out_time_us=9223372036854775807\n",
		"invalid size":  "total_size=-1\n",
		"invalid state": "progress=unknown\n",
		"invalid line":  "not a key=value line\nno separator\n",
	} {
		t.Run(name, func(t *testing.T) {
			canceled := false
			writer := &progressWriter{cancel: func() { canceled = true }}
			if _, err := writer.Write([]byte(input)); !errors.Is(err, ErrProgress) || !canceled {
				t.Fatalf("got %v, canceled=%t", err, canceled)
			}
		})
	}
}

func TestProgressWriterAcceptsChunkedLinesAndClampsNegativeStart(t *testing.T) {
	var last Progress
	w := &progressWriter{callback: func(p Progress) { last = p }}
	for _, part := range []string{"out_time_", "us=-1234\r", "\nprogress=", "end"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	w.finish()
	if w.err != nil || !last.Ended || last.OutputTicks != 0 {
		t.Fatalf("unexpected progress: %+v, %v", last, w.err)
	}
}

func TestStderrTailKeepsNewestBytes(t *testing.T) {
	w := &stderrTail{}
	for range 100 {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 8192))
	}
	_, _ = w.Write([]byte("final diagnostic"))
	if len(w.String()) != maxStderrTail || !strings.HasSuffix(w.String(), "final diagnostic") {
		t.Fatal("stderr did not retain its bounded tail")
	}
	_, _ = w.Write(bytes.Repeat([]byte("b"), maxStderrTail*2))
	if w.String() != strings.Repeat("b", maxStderrTail) {
		t.Fatal("oversized stderr writes must keep the newest bytes")
	}
}

func TestRunBorrowsPinnedSourceAndKeepsBoundedOutput(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "synthetic-secret-must-not-reach-the-child")
	t.Setenv("GOBY_SETUP_TOKEN", "synthetic-setup-secret")
	t.Setenv("FFREPORT", "file=unexpected-report.txt")
	input := helperInput(t)
	if _, err := input.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var count int
	var last Progress
	result, err := Run(context.Background(), helperExecutable(t, "copy"), t.TempDir(), input, commandPlan(), 1, func(p Progress) { count++; last = p })
	if err != nil {
		t.Fatalf("Run: %v, stderr=%s", err, result.StderrTail)
	}
	if result.ExitCode != 0 || len(result.StderrTail) != maxStderrTail || !strings.HasSuffix(result.StderrTail, "helper tail") || count != 20_001 || !last.Ended {
		t.Fatalf("unexpected result: exit=%d, stderr length=%d, callbacks=%d, last=%+v", result.ExitCode, len(result.StderrTail), count, last)
	}
	position, err := input.Seek(0, io.SeekCurrent)
	if err != nil || position != 4 {
		t.Fatalf("caller descriptor was closed or repositioned: %d, %v", position, err)
	}
}

func TestRunRejectsUnsafeDirectoryAndInvalidInput(t *testing.T) {
	input := helperInput(t)
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "main.m3u8.tmp"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), helperExecutable(t, "copy"), output, input, commandPlan(), 1, nil); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatalf("nonempty directory: %v", err)
	}
	link := filepath.Join(t.TempDir(), "linked-output")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), helperExecutable(t, "copy"), link, input, commandPlan(), 1, nil); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatalf("symlink directory: %v", err)
	}
	if _, err := Run(context.Background(), helperExecutable(t, "copy"), t.TempDir(), nil, commandPlan(), 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil input: %v", err)
	}
}

func TestRunCancelsMalformedProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, helperExecutable(t, "bad-progress"), t.TempDir(), helperInput(t), commandPlan(), 1, nil)
	if !errors.Is(err, ErrProgress) || time.Since(start) > 5*time.Second {
		t.Fatalf("malformed progress: %v after %s", err, time.Since(start))
	}
}

func TestRunCancellationKillsEntireProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	output, executable, input := t.TempDir(), helperExecutable(t, "parent"), helperInput(t)
	pidFile := filepath.Join(output, "child.pid")
	go func() {
		_, err := Run(ctx, executable, output, input, commandPlan(), 1, nil)
		done <- err
	}()
	var pid int
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("helper child did not become ready")
	}
	grandchildBytes, err := os.ReadFile(filepath.Join(output, "grandchild.pid"))
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := strconv.Atoi(string(grandchildBytes))
	if err != nil || grandchild <= 0 {
		t.Fatalf("invalid grandchild PID: %q", grandchildBytes)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancellation did not reap the process")
	}
	if time.Since(start) < terminateGrace-200*time.Millisecond {
		t.Fatal("SIGTERM-resistant processes did not receive the grace period")
	}
	assertProcessStopped(t, pid)
	assertProcessStopped(t, grandchild)
}

func TestRunSuccessfulParentExitRetiresSurvivingChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output := t.TempDir()
	result, err := Run(ctx, helperExecutable(t, "exit-parent"), output, helperInput(t), commandPlan(), 1, nil)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("successful parent exit: %v, %+v", err, result)
	}
	data, err := os.ReadFile(filepath.Join(output, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child PID: %q", data)
	}
	assertProcessStopped(t, pid)
}

func TestProcessEnvironmentContainsOnlyRequiredRuntimeSettings(t *testing.T) {
	for name, value := range map[string]string{
		"GOBY_DATABASE_URL": "synthetic-database-secret", "GOBY_SETUP_TOKEN": "synthetic-setup-secret",
		"DATABASE_URL": "synthetic-other-secret", "AWS_SECRET_ACCESS_KEY": "synthetic-cloud-secret",
		"FFREPORT": "file=report.txt", "LD_PRELOAD": "/not/allowed.so", "UNRELATED_SETTING": "not allowed",
		"LC_FAKE_SECRET": "not a locale category", "LANG": "C", "LC_TIME": "C",
		"LIBVA_DRIVER_NAME": "iHD", "CUDA_VISIBLE_DEVICES": "0", "ONEVPL_SEARCH_PATH": "/usr/lib",
	} {
		t.Setenv(name, value)
	}
	env := strings.Join(processEnvironment(), "\n")
	for _, forbidden := range []string{"GOBY_", "DATABASE_URL=", "AWS_SECRET_ACCESS_KEY=", "FFREPORT=", "LD_PRELOAD=", "UNRELATED_SETTING=", "LC_FAKE_SECRET="} {
		if strings.Contains(env, forbidden) {
			t.Errorf("environment leaked %q", forbidden)
		}
	}
	for _, required := range []string{"LANG=C", "LC_TIME=C", "LIBVA_DRIVER_NAME=iHD", "CUDA_VISIBLE_DEVICES=0", "ONEVPL_SEARCH_PATH=/usr/lib", "AV_LOG_FORCE_NOCOLOR=1"} {
		if !strings.Contains(env, required) {
			t.Errorf("environment lost %q", required)
		}
	}
}

func assertProcessStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err == nil {
			close := strings.LastIndexByte(string(data), ')')
			if close >= 0 && strings.HasPrefix(string(data)[close+1:], " Z ") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d survived process-group retirement", pid)
}

func TestRunActualFFmpegEncodeRemuxAndAudioOnly(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for actual Linux media verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10:duration=8", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=8",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "10", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-t", "8", source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate source: %v: %s", err, output)
	}
	for _, mode := range []string{"encode", "remux", "audio"} {
		t.Run(mode, func(t *testing.T) {
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			p := commandPlan()
			p.DurationTicks, p.SegmentSeconds = 8*ticksPerSecond, 2
			p.Width, p.Height = 128, 72
			if mode == "remux" {
				p = Plan{Container: "ts", VideoCodec: "copy", AudioCodec: "copy", VideoStreamIndex: 0, AudioStreamIndex: 1,
					DurationTicks: 8 * ticksPerSecond, SegmentSeconds: 2}
			}
			if mode == "audio" {
				p = Plan{Container: "ts", AudioCodec: "mp3", VideoStreamIndex: -1, AudioStreamIndex: 1,
					AudioChannels: 1, AudioBitrate: 96_000, DurationTicks: 8 * ticksPerSecond, SegmentSeconds: 2}
			}
			output := t.TempDir()
			var last Progress
			result, err := Run(ctx, ffmpeg, output, input, p, 1, func(p Progress) { last = p })
			if err != nil {
				t.Fatalf("actual FFmpeg: %v: %s", err, result.StderrTail)
			}
			playlist, err := os.ReadFile(filepath.Join(output, "main.m3u8"))
			if err != nil || !bytes.Contains(playlist, []byte("#EXT-X-ENDLIST")) || !bytes.Contains(playlist, []byte("#EXT-X-PLAYLIST-TYPE:EVENT")) {
				t.Fatalf("incomplete playlist: %v: %s", err, playlist)
			}
			if !last.Ended || last.OutputTicks < 7*ticksPerSecond {
				t.Fatalf("unexpected actual progress: %+v", last)
			}
			entries, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			segments := 0
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".tmp") {
					t.Fatalf("temporary output survived successful completion: %s", entry.Name())
				}
				if !strings.HasSuffix(entry.Name(), ".ts") {
					continue
				}
				segments++
				stream := "0:v:0"
				if mode == "audio" {
					stream = "0:a:0"
				}
				decode := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-threads", "1",
					"-i", filepath.Join(output, entry.Name()), "-map", stream, "-threads", "1", "-f", "null", "-")
				if data, err := decode.CombinedOutput(); err != nil || len(data) != 0 {
					t.Fatalf("decode standalone segment %s: %v: %s", entry.Name(), err, data)
				}
			}
			if segments < 3 {
				t.Fatalf("expected several complete segments, got %d", segments)
			}
		})
	}
}

func helperInput(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte("borrowed input descriptor"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func helperExecutable(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), transcodeHelperPrefix+mode)
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// Helpers run before testing's flag parser, so Run can exercise the identical
// fixed FFmpeg argument vector without introducing a production argument escape.
const transcodeHelperPrefix = "goby-transcode-test-helper-"

func init() {
	name := filepath.Base(os.Args[0])
	if mode, ok := strings.CutPrefix(name, transcodeHelperPrefix); ok {
		os.Exit(runTranscodeTestHelper(mode))
	}
}

func runTranscodeTestHelper(mode string) int {
	switch mode {
	case "copy":
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "GOBY_") || strings.HasPrefix(entry, "FFREPORT=") {
				return 90
			}
		}
		file, err := os.Open("/proc/self/fd/3")
		if err != nil {
			return 91
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil || string(data) != "borrowed input descriptor" || !hasArgumentPair(os.Args, "-i", "/proc/self/fd/3") {
			return 92
		}
		for range 20_000 {
			fmt.Fprint(os.Stdout, "out_time_us=1000000\ntotal_size=2048\nprogress=continue\n")
		}
		fmt.Fprint(os.Stdout, "progress=end\n")
		fmt.Fprint(os.Stderr, strings.Repeat("x", maxStderrTail*2)+"helper tail")
		return 0
	case "bad-progress":
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxProgressLine+1))
		for {
			time.Sleep(time.Second)
		}
	case "parent", "exit-parent", "child-parent":
		signal.Ignore(syscall.SIGTERM)
		child := exec.Command("/proc/self/exe")
		childMode, pidFile := "child", "child.pid"
		if mode == "parent" {
			childMode = "child-parent"
		}
		if mode == "child-parent" {
			childMode, pidFile = "grandchild", "grandchild.pid"
		}
		child.Args[0] = transcodeHelperPrefix + childMode
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 93
		}
		if mode == "exit-parent" || mode == "child-parent" {
			for {
				if _, err := os.Stat(pidFile); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		if mode == "exit-parent" {
			return 0
		}
		if mode == "child-parent" && os.WriteFile("child.pid", []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 94
		}
		for {
			time.Sleep(time.Second)
		}
	case "child", "grandchild":
		signal.Ignore(syscall.SIGTERM)
		if os.WriteFile(mode+".pid", []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 94
		}
		for {
			time.Sleep(time.Second)
		}
	default:
		return 95
	}
}
