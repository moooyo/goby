//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"golang.org/x/sys/unix"
)

func TestGPUProducerCancellationRetiresActualFFmpegAndClosesOutput(t *testing.T) {
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" {
		t.Skip("an explicitly selected AMD VAAPI device is required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	setupContext, setupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer setupCancel()
	source := filepath.Join(t.TempDir(), "hdr-source.mkv")
	progressiveVideoCommand(t, setupContext, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x192:rate=16:duration=20", "-vf",
		"format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc",
		"-c:v", "ffv1", "-threads:v", "1", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(setupContext, source)
	// Allow one Matroska clock tick without replacing the actual source clock
	// used by the conversion plan or weakening cancellation requirements.
	durationDelta := info.DurationTicks - 20*ticksPerSecond
	if err != nil || len(info.Streams) != 1 || info.Streams[0].ColorTransfer != "smpte2084" ||
		durationDelta < -ticksPerSecond/1000 || durationDelta > ticksPerSecond/1000 {
		t.Fatalf("cancellation fixture lacks its bounded HDR video: %+v, %v", info, err)
	}
	plan := progressiveVideoFixturePlan(info, "h264", "", 0)
	plan.Width, plan.Height = 320, 192
	plan.Hardware = Hardware{Decode: "software", Encode: "vaapi", Device: device}
	plan.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "hdr10", SourceBitDepth: 10,
		SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
	resolved, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	realExecutable, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		t.Fatal(err)
	}
	controlDirectory := t.TempDir()
	pidPath := filepath.Join(controlDirectory, "producer.pid")
	wrapper := filepath.Join(controlDirectory, "paced-ffmpeg")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	// exec retains the recorded PID. Input pacing keeps the real GPU producer
	// live after its first verified fragment; it is not a simulated encoder.
	program := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$$\" > " + quote(pidPath) + "\nexec " + quote(realExecutable) + " -re \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	outputDirectory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ready := make(chan Progress, 1)
	type completion struct {
		result RunResult
		err    error
	}
	done := make(chan completion, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, err := Run(ctx, wrapper, outputDirectory, input, plan, 1, func(progress Progress) {
			if progress.Ready && progress.Bytes > 0 {
				select {
				case ready <- progress:
				default:
				}
			}
		})
		done <- completion{result: result, err: err}
	}()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(6 * time.Second):
			t.Error("GPU producer cleanup exceeded its process grace bound")
		}
	}()
	select {
	case <-ready:
	case completed := <-done:
		t.Fatalf("GPU producer ended before actual media was ready: %v: %s", completed.err, completed.result.StderrTail)
	case <-ctx.Done():
		t.Fatalf("GPU producer did not become ready: %v", ctx.Err())
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid owned FFmpeg process record: %q", pidBytes)
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatalf("retain an identity-bound handle to the live producer: %v", err)
	}
	defer unix.Close(pidfd)
	processDirectory := filepath.Join("/proc", strconv.Itoa(pid))
	actualExecutable, executableErr := os.Readlink(filepath.Join(processDirectory, "exe"))
	actualDirectory, directoryErr := os.Readlink(filepath.Join(processDirectory, "cwd"))
	command, commandErr := os.ReadFile(filepath.Join(processDirectory, "cmdline"))
	expectedDirectory, resolvedErr := filepath.EvalSymlinks(outputDirectory)
	if executableErr != nil || directoryErr != nil || commandErr != nil || resolvedErr != nil || actualExecutable != realExecutable || actualDirectory != expectedDirectory ||
		!bytes.Contains(command, []byte("\x00h264_vaapi\x00")) || !bytes.Contains(command, []byte("libplacebo=")) || gpuProducerExited(t, pidfd) {
		t.Fatal("the observed live process is not this real GPU conversion")
	}
	files, err := os.ReadDir(filepath.Join(processDirectory, "fd"))
	if err != nil {
		t.Fatal(err)
	}
	deviceOpen := false
	for _, file := range files {
		target, err := os.Readlink(filepath.Join(processDirectory, "fd", file.Name()))
		deviceOpen = deviceOpen || err == nil && target == device
	}
	if !deviceOpen {
		t.Fatal("the ready producer does not own the selected GPU render device")
	}
	output := filepath.Join(outputDirectory, "stream.bin")
	prefix, err := os.ReadFile(output)
	if err != nil || len(prefix) == 0 {
		t.Fatalf("ready output has no actual media prefix: %v", err)
	}
	start := time.Now()
	cancel()
	select {
	case completed := <-done:
		if !errors.Is(completed.err, context.Canceled) {
			t.Fatalf("actual GPU producer did not stop because of cancellation: %+v", completed)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancellation did not retire the GPU producer within its process grace bound")
	}
	if !gpuProducerExited(t, pidfd) || time.Since(start) > 6*time.Second {
		t.Fatal("the cancelled GPU process handle is still live")
	}
	final, err := os.ReadFile(output)
	if err != nil || !bytes.HasPrefix(final, prefix) {
		t.Fatalf("cancellation rewrote the already published media prefix: %v", err)
	}
	assertProgressiveFileClosed(t, output)
}

func gpuProducerExited(t *testing.T, pidfd int) bool {
	t.Helper()
	files := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	ready, err := unix.Poll(files, 0)
	if err != nil || files[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
		t.Fatalf("inspect the retained GPU process handle: %v", err)
	}
	if ready == 0 {
		return false
	}
	if files[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
		t.Fatal("GPU process handle returned an unknown event")
	}
	return true
}
