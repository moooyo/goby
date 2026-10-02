//go:build linux

package transcode_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
	"golang.org/x/sys/unix"
)

// Run the identical opt-in workload on both revisions. Resource samples are
// observations of this paced software workload, not general throughput claims.
func TestPlaybackPerformanceConcurrentRealTranscodes(t *testing.T) {
	if os.Getenv("GOBY_TEST_PLAYBACK_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_PLAYBACK_PERFORMANCE=1 is required for resource profiling")
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Linux media profiling")
	}
	fixtureCtx, pool, repository, first, second := encodingRepositoryFixture(t)
	ctx, cancel := context.WithTimeout(fixtureCtx, 60*time.Second)
	defer cancel()
	resolved, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	realExecutable, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "concurrent-source.mkv")
	generate := exec.CommandContext(ctx, realExecutable, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=12:duration=20", "-c:v", "ffv1", "-threads:v", "1", source)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate concurrent media fixture: %v: %s", err, output)
	}
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 1 || info.Streams[0].CodecType != "video" ||
		info.Streams[0].Width != 320 || info.Streams[0].Height != 180 {
		t.Fatalf("probe concurrent media fixture: %+v: %v", info, err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	plan := transcode.Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		DurationTicks: info.DurationTicks, Width: 320, Height: 180, FrameRate: 12, VideoBitrate: 500_000, SegmentSeconds: 2}
	for _, scope := range []transcode.Scope{first, second} {
		if _, err := pool.Exec(ctx, "UPDATE play_sessions SET duration_ticks = $2 WHERE id = $1", scope.PlaySessionID, info.DurationTicks); err != nil {
			t.Fatal(err)
		}
	}
	controlDirectory, root := t.TempDir(), t.TempDir()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(controlDirectory, "paced-ffmpeg")
	// exec preserves the recorded process identity; input pacing keeps each
	// actual encoder alive after publishing its first playable HLS segment.
	program := "#!/bin/sh\nset -eu\ndirectory=\"$(pwd -P)\"\njob=\"${directory##*/}\"\nprintf '%s\\n' \"$$\" > " +
		quote(controlDirectory) + "/\"$job.pid\"\nexec " + quote(realExecutable) + " -readrate 0.5 \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := transcode.NewManager(ctx, transcode.Options{
		Root: root, FFmpegPath: wrapper, Repository: repository, Threads: 1,
		MaxJobs: 2, MaxUserJobs: 1, MaxSessionJobs: 1, MaxBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		StartupTimeout: 25 * time.Second, NoProgressTimeout: 20 * time.Second, IdleTimeout: time.Minute, MaxRuntime: time.Minute,
	})
	if err != nil {
		t.Fatalf("create concurrent conversion manager: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Errorf("close concurrent conversion manager: %v", err)
		}
	})
	specs := []transcode.Spec{
		{Scope: first, SourceStamp: hex.EncodeToString(digest[:]), Plan: plan},
		{Scope: second, SourceStamp: hex.EncodeToString(digest[:]), Plan: plan},
	}
	type admission struct {
		index  int
		record transcode.Record
		err    error
	}
	results := make(chan admission, 2)
	started := time.Now()
	for index, spec := range specs {
		go func() {
			input, err := os.Open(source)
			if err != nil {
				results <- admission{index: index, err: err}
				return
			}
			record, err := manager.Ensure(ctx, spec, input)
			if err == nil {
				_, err = manager.WaitReady(ctx, spec.Scope, record.ID)
			}
			results <- admission{index: index, record: record, err: err}
		}()
	}
	records := make([]transcode.Record, 2)
	for range records {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("prepare concurrent playable job %d: %v", result.index, result.err)
			}
			records[result.index] = result.record
		case <-ctx.Done():
			t.Fatal("concurrent media preparation exceeded its test budget")
		}
	}
	producers := make([]performanceProducer, 2)
	for index, record := range records {
		performanceAssertPlayable(t, ctx, manager, record)
		producers[index] = performanceObserveProducer(t, controlDirectory, root, realExecutable, record.ID)
		defer unix.Close(producers[index].pidfd)
	}
	if producers[0].pid == producers[1].pid {
		t.Fatal("two independent jobs shared one producer process")
	}
	metrics := manager.Metrics()
	if metrics.Running != 2 || metrics.Software != 2 || metrics.Hardware != 0 || metrics.MaxJobs != 2 {
		t.Fatalf("two real software conversions did not occupy both execution slots: %+v", metrics)
	}
	performanceLogSample(t, "ready", started, manager, producers)

	const duplicateRequests = 16
	duplicateResults := make(chan admission, duplicateRequests)
	for index := range duplicateRequests {
		go func() {
			specIndex := index % len(specs)
			input, err := os.Open(source)
			if err != nil {
				duplicateResults <- admission{index: specIndex, err: err}
				return
			}
			record, err := manager.Ensure(ctx, specs[specIndex], input)
			if _, statErr := input.Stat(); statErr == nil {
				_ = input.Close()
				err = errors.New("duplicate input descriptor remained open")
			}
			duplicateResults <- admission{index: specIndex, record: record, err: err}
		}()
	}
	for range duplicateRequests {
		select {
		case result := <-duplicateResults:
			if result.err != nil || result.record.ID != records[result.index].ID {
				t.Fatalf("concurrent identical request created another job: %+v", result)
			}
		case <-ctx.Done():
			t.Fatal("concurrent duplicate requests exceeded their test budget")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate requests persisted %d jobs, want 2: %v", count, err)
	}
	for sample := 1; sample <= 2; sample++ {
		select {
		case <-time.After(time.Second):
			performanceLogSample(t, "concurrent_"+strconv.Itoa(sample), started, manager, producers)
		case <-ctx.Done():
			t.Fatal("resource sampling exceeded its test budget")
		}
	}
	firstCancelled := time.Now()
	if err := manager.CancelJob(records[0].ID, first); err != nil {
		t.Fatalf("cancel the first real producer: %v", err)
	}
	performanceWaitCancelled(t, ctx, manager, firstCancelled, producers[0], &producers[1], filepath.Join(root, records[0].ID), 1)
	firstCancelMS := float64(time.Since(firstCancelled).Microseconds()) / 1000
	if stored := encodingStoredRecord(t, ctx, pool, records[0].ID); stored.State != "cancelled" || stored.ErrorCode != "cancelled" {
		t.Fatalf("cancelled producer lacks its durable terminal state: %+v", stored)
	}
	performanceAssertPlayable(t, ctx, manager, records[1])
	performanceLogSample(t, "first_cancelled", started, manager, producers[1:])
	secondCancelled := time.Now()
	if err := manager.CancelJob(records[1].ID, second); err != nil {
		t.Fatalf("cancel the peer real producer: %v", err)
	}
	performanceWaitCancelled(t, ctx, manager, secondCancelled, producers[1], nil, filepath.Join(root, records[1].ID), 0)
	secondCancelMS := float64(time.Since(secondCancelled).Microseconds()) / 1000
	performanceLogSample(t, "all_cancelled", started, manager, nil)
	performanceLogJSON(t, map[string]any{"phase": "cancellation", "first_cancel_ms": firstCancelMS,
		"second_cancel_ms": secondCancelMS, "cancel_budget_ms": 6000, "duplicate_requests": duplicateRequests, "durable_jobs": count})
}

type performanceProducer struct {
	pid   int
	pidfd int
}

type performanceProcessSample struct {
	PID            int    `json:"pid"`
	UserCPUTicks   uint64 `json:"user_cpu_ticks"`
	SystemCPUTicks uint64 `json:"system_cpu_ticks"`
	RSSBytes       uint64 `json:"rss_bytes"`
	Threads        uint64 `json:"threads"`
	OpenFDs        int    `json:"open_fds"`
}

func performanceObserveProducer(t *testing.T, control, root, executable, jobID string) performanceProducer {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(control, jobID+".pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid owned producer PID: %q", data)
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatalf("retain identity-bound producer handle: %v", err)
	}
	process := performanceProducer{pid: pid, pidfd: pidfd}
	processDirectory := filepath.Join("/proc", strconv.Itoa(pid))
	actualExecutable, executableErr := os.Readlink(filepath.Join(processDirectory, "exe"))
	actualDirectory, directoryErr := os.Readlink(filepath.Join(processDirectory, "cwd"))
	expectedDirectory, expectedErr := filepath.EvalSymlinks(filepath.Join(root, jobID))
	command, commandErr := os.ReadFile(filepath.Join(processDirectory, "cmdline"))
	if executableErr != nil || directoryErr != nil || expectedErr != nil || commandErr != nil ||
		actualExecutable != executable || actualDirectory != expectedDirectory ||
		!bytes.Contains(command, []byte("\x00libx264\x00")) || !bytes.Contains(command, []byte("\x00hls\x00")) ||
		!bytes.Contains(command, []byte("\x00-readrate\x000.5\x00")) ||
		performanceProducerExited(t, process) {
		_ = unix.Close(pidfd)
		t.Fatal("observed process is not this live software HLS conversion")
	}
	return process
}

func performanceProducerExited(t *testing.T, process performanceProducer) bool {
	t.Helper()
	// The retained handle identifies the original process even if its numeric
	// PID is reused. Signal interruption is retryable and is not a job failure.
	for {
		files := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(files, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			t.Fatalf("inspect the retained producer handle: %v", err)
		}
		if files[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			t.Fatalf("producer handle returned an invalid event: ready=%d revents=%#x", ready, files[0].Revents)
		}
		if ready == 0 {
			return false
		}
		if files[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
			t.Fatalf("producer handle returned an unknown event: ready=%d revents=%#x", ready, files[0].Revents)
		}
		return true
	}
}

func performanceReadProcess(t *testing.T, pid int) performanceProcessSample {
	t.Helper()
	directory := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(directory, "stat"))
	if err != nil {
		t.Fatal(err)
	}
	// The executable name can contain spaces and parentheses. Numeric fields
	// begin after its final closing parenthesis; field 3 is at index zero.
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		t.Fatal("producer stat record has no executable delimiter")
	}
	fields := strings.Fields(string(stat[end+2:]))
	if len(fields) < 22 {
		t.Fatal("producer stat record lacks resource counters")
	}
	read := func(index int) uint64 {
		value, err := strconv.ParseUint(fields[index], 10, 64)
		if err != nil {
			t.Fatalf("parse process resource field %d: %v", index+3, err)
		}
		return value
	}
	fds, err := os.ReadDir(filepath.Join(directory, "fd"))
	if err != nil {
		t.Fatal(err)
	}
	return performanceProcessSample{PID: pid, UserCPUTicks: read(11), SystemCPUTicks: read(12),
		RSSBytes: read(21) * uint64(os.Getpagesize()), Threads: read(17), OpenFDs: len(fds)}
}

func performanceLogSample(t *testing.T, phase string, started time.Time, manager *transcode.Manager, producers []performanceProducer) {
	t.Helper()
	samples := make([]performanceProcessSample, 0, len(producers))
	for _, producer := range producers {
		if performanceProducerExited(t, producer) {
			t.Fatal("paced real producer exited before resource sampling completed")
		}
		samples = append(samples, performanceReadProcess(t, producer.pid))
		if performanceProducerExited(t, producer) {
			t.Fatal("paced real producer exited during resource sampling")
		}
	}
	performanceLogJSON(t, map[string]any{"phase": phase, "elapsed_ms": float64(time.Since(started).Microseconds()) / 1000,
		"producers": samples, "manager_process": performanceReadProcess(t, os.Getpid()), "slots": manager.Metrics()})
}

func performanceLogJSON(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("playback_performance %s", data)
}

func performanceAssertPlayable(t *testing.T, ctx context.Context, manager *transcode.Manager, record transcode.Record) {
	t.Helper()
	manifest, err := manager.Open(ctx, record.Spec.Scope, record.ID, "main.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(io.LimitReader(manifest, transcode.MaxPlaylistBytes+1))
	closeErr := manifest.Close()
	playlist, parseErr := transcode.ParseMediaPlaylist(data)
	if readErr != nil || closeErr != nil || parseErr != nil || len(playlist.Segments) == 0 {
		t.Fatalf("read actual playable HLS manifest: %v", errors.Join(readErr, closeErr, parseErr))
	}
	segment, err := manager.Open(ctx, record.Spec.Scope, record.ID, playlist.Segments[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	var packet [188]byte
	_, readErr = io.ReadFull(segment, packet[:])
	closeErr = segment.Close()
	if readErr != nil || closeErr != nil || packet[0] != 0x47 {
		t.Fatalf("read actual HLS transport packet: %v", errors.Join(readErr, closeErr))
	}
}

func performanceWaitCancelled(t *testing.T, ctx context.Context, manager *transcode.Manager, cancelledAt time.Time, cancelled performanceProducer,
	peer *performanceProducer, directory string, running int) {
	t.Helper()
	deadline := cancelledAt.Add(6 * time.Second)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if waitCtx.Err() != nil {
			t.Fatalf("cancelled producer, cache, or execution slot survived its 6 second budget: %v", waitCtx.Err())
		}
		if peer != nil && performanceProducerExited(t, *peer) {
			t.Fatal("cancelling one producer stopped its independent peer")
		}
		_, directoryErr := os.Stat(directory)
		if directoryErr != nil && !errors.Is(directoryErr, os.ErrNotExist) {
			t.Fatalf("inspect cancelled cache directory: %v", directoryErr)
		}
		if performanceProducerExited(t, cancelled) && errors.Is(directoryErr, os.ErrNotExist) && manager.Metrics().Running == running {
			if !time.Now().Before(deadline) {
				t.Fatal("observed cancellation after its 6 second budget")
			}
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("cancelled producer, cache, or execution slot survived its 6 second budget: %v", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
