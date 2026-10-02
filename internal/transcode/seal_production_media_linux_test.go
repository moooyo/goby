//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This opt-in test measures a paced, small software workload. Controlled cache
// ages exercise retention ownership without pretending that wall-clock pauses,
// a consumer application's complete lifecycle, or hardware throughput were run.
func TestActualLegacyHLSSealProductionMedia(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Linux media verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	executable, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	fixture := filepath.Join(control, "source.mkv")
	// The two presentation regions have different luma means, so independently
	// decoding a late window detects a duplicated source prefix as well as PTS.
	filter := "nullsrc=size=160x90:rate=8:duration=120,geq=lum='if(lt(T,60),48,208)+32*sin(X*17+Y*13+T*11)':cb=128:cr=128,format=yuv420p"
	generate := exec.CommandContext(ctx, executable, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", filter, "-c:v", "ffv1", "-threads:v", "1", fixture)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate time-distinguishable source fixture: %v: %s", err, output)
	}
	sourceOrigin, sourceDuration := sealMediaSourceTimeline(t, ctx, ffprobe, fixture)
	if math.Abs(sourceOrigin) > 0.000001 || math.Abs(sourceDuration-120) > 0.13 {
		t.Fatalf("actual fixture has an unexpected source clock: start=%f duration=%f", sourceOrigin, sourceDuration)
	}
	durationTicks := int64(math.Round(sourceDuration * float64(ticksPerSecond)))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(control, "paced-ffmpeg")
	program := "#!/bin/sh\nset -eu\ndirectory=\"$(pwd -P)\"\njob=\"${directory##*/}\"\nprintf '%s\\n' \"$$\" > " +
		quote(control) + "/\"$job.pid\"\nexec " + quote(executable) + " -readrate 4 \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		DurationTicks: durationTicks, Width: 160, Height: 90, FrameRate: 8, VideoBitrate: 250_000,
		SegmentSeconds: 6, SegmentMode: "vod", EndTicks: durationTicks}
	var cuts []string
	for second := 6; second < 120; second += 6 {
		cuts = append(cuts, strconv.FormatInt(int64(second)*ticksPerSecond, 10))
	}
	plan.SegmentTimes = strings.Join(cuts, ",")

	t.Run("RetainsAndResumes", func(t *testing.T) {
		manager := sealMediaManager(t, wrapper)
		spec := managerTestSpec(1)
		spec.Plan = plan
		input := sealMediaInput(t, fixture)
		record, err := manager.Ensure(ctx, spec, input)
		if err != nil {
			t.Fatal(err)
		}
		initial, privateTail := sealMediaWaitPartial(t, ctx, manager, spec.Scope, record.ID)
		process := sealMediaObserveProcess(t, control, manager.options.Root, executable, record.ID)
		defer unix.Close(process.pidfd)
		pin, err := manager.TryOpen(spec.Scope, record.ID, initial.Segments[0].Name)
		if err != nil {
			t.Fatal(err)
		}
		defer pin.Close()
		pinnedBytes, err := io.ReadAll(pin)
		if err != nil || len(pinnedBytes) == 0 {
			t.Fatalf("read pinned actual media: %v", err)
		}
		pausedAt := time.Now()
		if err := manager.SealProduction(record.ID, spec.Scope); err != nil {
			t.Fatal(err)
		}
		// A missing artifact has no producer to wait for once sealing fences it.
		missingStarted := time.Now()
		missingCtx, missingCancel := context.WithTimeout(ctx, time.Second)
		handle, missingErr := manager.Open(missingCtx, spec.Scope, record.ID, "segment-000019.ts")
		missingCancel()
		if handle != nil {
			_ = handle.Close()
		}
		if handle != nil || !errors.Is(missingErr, ErrOutputUnavailable) || time.Since(missingStarted) >= time.Second {
			t.Fatalf("sealed cache miss waited or became available: %v", missingErr)
		}
		final := sealMediaWaitDone(t, ctx, manager, record.ID)
		retirementMS := float64(time.Since(pausedAt).Microseconds()) / 1000
		if retirementMS > 6000 {
			t.Fatalf("seal exceeded its pause-to-finalization budget: %.3f ms", retirementMS)
		}
		sealMediaAssertReaped(t, process, filepath.Join(manager.options.Root, record.ID))
		if final.State != "cancelled" || final.ErrorCode != "production_sealed" || final.OutputBytes <= 0 || manager.Metrics().Running != 0 {
			t.Fatalf("sealed actual producer lost its terminal accounting or execution slot: %+v/%+v", final, manager.Metrics())
		}
		assertManagerInputClosed(t, input)
		list := sealMediaPlaylist(t, manager, spec.Scope, record.ID)
		if list.Ended || list.Type != "EVENT" || len(list.Segments) < len(initial.Segments) || len(list.Segments) >= 20 {
			t.Fatalf("cancelled partial producer claimed a complete VOD: %+v", list)
		}
		jobDirectory := filepath.Join(manager.options.Root, record.ID)
		privateBytes, directoryBytes := sealMediaCountFiles(t, jobDirectory)
		if privateBytes == 0 || directoryBytes != final.OutputBytes {
			t.Fatalf("private partial bytes escaped final accounting: private=%d total=%d record=%d", privateBytes, directoryBytes, final.OutputBytes)
		}
		var finalTail string
		for _, entry := range sealMediaEntries(t, jobDirectory) {
			if strings.HasPrefix(entry.Name(), "segment-") && strings.HasSuffix(entry.Name(), ".ts.tmp") && entry.Name() > finalTail {
				finalTail = entry.Name()
			}
		}
		// The final writer tail stays private even if SIGTERM flushes a final
		// private list. A segment may legitimately close between observation and
		// cancellation, so the earlier snapshot is not an immutable tail fence.
		for _, segment := range list.Segments {
			if segment.Name == strings.TrimSuffix(finalTail, ".tmp") {
				t.Fatalf("cancellation published its unfinished tail: %s", finalTail)
			}
		}
		if finalTail == "" {
			t.Fatal("cancelled producer has no uncredited private tail")
		}
		if info, err := os.Stat(filepath.Join(jobDirectory, finalTail)); err != nil || info.Size() == 0 {
			t.Fatalf("final private tail disappeared or lost its byte charge: %v", err)
		}
		sealedCopy := filepath.Join(t.TempDir(), "sealed-first.ts")
		if err := os.WriteFile(sealedCopy, pinnedBytes, 0600); err != nil {
			t.Fatal(err)
		}
		firstFacts := sealMediaDecode(t, ctx, executable, ffprobe, sealedCopy)
		if firstFacts.FirstPTS < 0.87 || firstFacts.FirstPTS > 1.13 || firstFacts.FirstLumaMean >= 100 || firstFacts.Frames != 48 {
			t.Fatalf("sealed prefix is not independently decodable source media: %+v", firstFacts)
		}
		for _, age := range []time.Duration{2 * time.Minute, 10 * time.Minute} {
			// An existing reader owns the files across an expired idle lease.
			// Only LastAccessAt is controlled; no clock or process time is faked.
			manager.mu.Lock()
			manager.jobs[record.ID].record.LastAccessAt = time.Now().UTC().Add(-age)
			manager.mu.Unlock()
			manager.maintain()
			if _, err := os.Stat(jobDirectory); err != nil {
				t.Fatalf("controlled cache age %s reclaimed a pinned sealed window: %v", age, err)
			}
			reopened, err := manager.TryOpen(spec.Scope, record.ID, initial.Segments[0].Name)
			if err != nil {
				t.Fatalf("controlled cache age %s lost published cache access: %v", age, err)
			}
			data, readErr := io.ReadAll(reopened)
			closeErr := reopened.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(data, pinnedBytes) {
				t.Fatalf("retained cache hit changed media bytes: %v", errors.Join(readErr, closeErr))
			}
		}
		// An identical immutable plan must receive a new producer after sealing.
		replacementInput := sealMediaInput(t, fixture)
		replacement, err := manager.Ensure(ctx, spec, replacementInput)
		if err != nil || replacement.ID == record.ID {
			t.Fatalf("sealed plan deduplication reused a retired producer: %+v/%v", replacement, err)
		}
		if err := manager.CancelJob(replacement.ID, spec.Scope); err != nil {
			t.Fatal(err)
		}
		sealMediaWaitDone(t, ctx, manager, replacement.ID)
		assertManagerInputClosed(t, replacementInput)

		resume := spec
		resume.Plan.StartTicks, resume.Plan.EndTicks = 90*ticksPerSecond, 102*ticksPerSecond
		resume.Plan.SegmentStartNumber, resume.Plan.SegmentTimes = 15, strconv.FormatInt(96*ticksPerSecond, 10)
		resumeInput := sealMediaInput(t, fixture)
		resumed, err := manager.Ensure(ctx, resume, resumeInput)
		if err != nil || resumed.ID == record.ID || resumed.ID == replacement.ID {
			t.Fatalf("resume failed to create a distinct bounded producer: %+v/%v", resumed, err)
		}
		resumeFinal := sealMediaWaitDone(t, ctx, manager, resumed.ID)
		if resumeFinal.State != "completed" || resumeFinal.ErrorCode != "" {
			t.Fatalf("late bounded producer failed: %+v", resumeFinal)
		}
		assertManagerInputClosed(t, resumeInput)
		resumeList := sealMediaPlaylist(t, manager, resume.Scope, resumed.ID)
		if !resumeList.Ended || resumeList.Type != "VOD" || resumeList.Sequence != 15 || len(resumeList.Segments) != 2 {
			t.Fatalf("resume lost source-global numbering or its finite window: %+v", resumeList)
		}
		resumeFacts := make([]sealMediaDecodedFacts, 0, 2)
		for index, segment := range resumeList.Segments {
			if segment.Number != int64(15+index) || segment.Name != fmt.Sprintf("segment-%06d.ts", 15+index) || segment.DurationTicks != 6*ticksPerSecond {
				t.Fatalf("unexpected bounded resume segment: %+v", segment)
			}
			facts := sealMediaDecode(t, ctx, executable, ffprobe, filepath.Join(manager.options.Root, resumed.ID, segment.Name))
			wantStart := 91 + float64(6*index)
			if math.Abs(facts.FirstPTS-wantStart) > 0.13 || facts.LastPTS >= 103.13 || facts.FirstLumaMean <= 160 || facts.Frames != 48 {
				t.Fatalf("resume duplicated a source prefix or escaped its window: %+v", facts)
			}
			resumeFacts = append(resumeFacts, facts)
		}
		for _, entry := range sealMediaEntries(t, filepath.Join(manager.options.Root, resumed.ID)) {
			if strings.HasPrefix(entry.Name(), "segment-") && strings.HasSuffix(entry.Name(), ".ts") &&
				entry.Name() != "segment-000015.ts" && entry.Name() != "segment-000016.ts" {
				t.Fatalf("resume produced an unrequested prefix segment: %s", entry.Name())
			}
		}
		if err := manager.CancelJob(record.ID, spec.Scope); err != nil {
			t.Fatal(err)
		}
		if handle, err := manager.TryOpen(spec.Scope, record.ID, initial.Segments[0].Name); handle != nil || !errors.Is(err, ErrJobCancelled) {
			if handle != nil {
				_ = handle.Close()
			}
			t.Fatalf("full stop retained access to sealed cache: %v", err)
		}
		manager.maintain()
		if _, err := os.Stat(jobDirectory); err != nil {
			t.Fatalf("full stop reclaimed files with a pinned reader: %v", err)
		}
		if err := pin.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := pin.File.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("last reader did not close its descriptor: %v", err)
		}
		sealMediaWaitRemoved(t, ctx, manager, jobDirectory)
		if err := manager.CancelJob(resumed.ID, resume.Scope); err != nil {
			t.Fatal(err)
		}
		sealMediaWaitRemoved(t, ctx, manager, filepath.Join(manager.options.Root, resumed.ID))
		sealMediaLog(t, map[string]any{"scenario": "seal_and_source_global_resume", "software_pacing": 4,
			"source_origin_seconds": sourceOrigin, "source_duration_seconds": sourceDuration,
			"pause_to_finalization_ms": retirementMS, "retirement_budget_ms": 6000, "sealed_charged_bytes": final.OutputBytes,
			"sealed_private_bytes": privateBytes, "sealed_published_segments": len(list.Segments), "source_prefix": firstFacts,
			"observed_private_tail": privateTail, "final_uncredited_private_tail": finalTail,
			"resume_start_seconds": 90, "resume_end_seconds": 102, "resume_global_segment_number": 15,
			"resume_segments": resumeFacts, "controlled_cache_ages_seconds": []int{120, 600}, "actual_wall_clock_pause": false})
	})

	t.Run("RejectsChangedSource", func(t *testing.T) {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(t.TempDir(), "mutated-source.mkv")
		if err := os.WriteFile(source, data, 0600); err != nil {
			t.Fatal(err)
		}
		manager := sealMediaManager(t, wrapper)
		spec := managerTestSpec(2)
		spec.Plan = plan
		input := sealMediaInput(t, source)
		record, err := manager.Ensure(ctx, spec, input)
		if err != nil {
			t.Fatal(err)
		}
		initial, _ := sealMediaWaitPartial(t, ctx, manager, spec.Scope, record.ID)
		process := sealMediaObserveProcess(t, control, manager.options.Root, executable, record.ID)
		defer unix.Close(process.pidfd)
		pin, err := manager.TryOpen(spec.Scope, record.ID, initial.Segments[0].Name)
		if err != nil {
			t.Fatal(err)
		}
		defer pin.Close()
		pinInfo, err := pin.File.Stat()
		if err != nil || pinInfo.Size() <= 0 {
			t.Fatalf("inspect pinned media before source mutation: %v", err)
		}
		writer, err := os.OpenFile(source, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := writer.Write([]byte{0})
		closeErr := writer.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(errors.Join(writeErr, closeErr))
		}
		if err := manager.SealProduction(record.ID, spec.Scope); err != nil {
			t.Fatal(err)
		}
		final := sealMediaWaitDone(t, ctx, manager, record.ID)
		sealMediaAssertReaped(t, process, filepath.Join(manager.options.Root, record.ID))
		assertManagerInputClosed(t, input)
		if final.ErrorCode == "production_sealed" || final.State == "completed" {
			t.Fatalf("cancelled changed source acquired retained-output proof: %+v", final)
		}
		if handle, err := manager.TryOpen(spec.Scope, record.ID, initial.Segments[0].Name); handle != nil || err == nil {
			if handle != nil {
				_ = handle.Close()
			}
			t.Fatalf("failed seal allowed a new cache reader: %v", err)
		}
		pinnedData, readErr := io.ReadAll(pin)
		if readErr != nil || int64(len(pinnedData)) != pinInfo.Size() || len(pinnedData) == 0 || pinnedData[0] != 0x47 {
			t.Fatalf("existing reader could not finish its already pinned segment: bytes=%d want=%d: %v", len(pinnedData), pinInfo.Size(), readErr)
		}
		if err := pin.Close(); err != nil {
			t.Fatal(err)
		}
		sealMediaWaitRemoved(t, ctx, manager, filepath.Join(manager.options.Root, record.ID))
		sealMediaLog(t, map[string]any{"scenario": "changed_source_cannot_retain_partial_output", "state": final.State,
			"error_code": final.ErrorCode, "source_descriptor_closed": true, "producer_reaped": true,
			"pinned_bytes_drained": len(pinnedData), "cache_removed_after_last_reader": true})
	})
}

func sealMediaManager(t *testing.T, executable string) *Manager {
	t.Helper()
	options := managerTestOptions(t, nil)
	options.FFmpegPath, options.Threads = executable, 1
	options.MaxBytes, options.MaxJobBytes = 64<<20, 16<<20
	options.IdleTimeout, options.StartupTimeout = 2*time.Minute, 20*time.Second
	options.NoProgressTimeout, options.MaxRuntime = 20*time.Second, time.Minute
	options.pollInterval = 25 * time.Millisecond
	return newTestManager(t, options)
}

func sealMediaInput(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func sealMediaWaitPartial(t *testing.T, ctx context.Context, manager *Manager, scope Scope, id string) (MediaPlaylist, string) {
	t.Helper()
	if _, err := manager.WaitReady(ctx, scope, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		list := sealMediaPlaylist(t, manager, scope, id)
		var tail string
		for _, entry := range sealMediaEntries(t, filepath.Join(manager.options.Root, id)) {
			if strings.HasPrefix(entry.Name(), "segment-") && strings.HasSuffix(entry.Name(), ".ts.tmp") && entry.Name() > tail {
				info, err := entry.Info()
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					t.Fatal(err)
				}
				if info.Size() > 0 {
					tail = entry.Name()
				}
			}
		}
		if len(list.Segments) > 0 && tail != "" && !list.Ended && manager.Metrics().Running == 1 {
			return list, tail
		}
		if !time.Now().Before(deadline) {
			t.Fatal("actual producer did not retain a nonempty private successor while publishing media")
		}
		sealMediaTick(t, ctx)
	}
}

func sealMediaPlaylist(t *testing.T, manager *Manager, scope Scope, id string) MediaPlaylist {
	t.Helper()
	handle, err := manager.TryOpen(scope, id, "main.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(io.LimitReader(handle, MaxPlaylistBytes+1))
	closeErr := handle.Close()
	list, parseErr := ParseMediaPlaylist(data)
	if readErr != nil || closeErr != nil || parseErr != nil {
		t.Fatalf("read actual published playlist: %v", errors.Join(readErr, closeErr, parseErr))
	}
	return list
}

func sealMediaWaitDone(t *testing.T, ctx context.Context, manager *Manager, id string) Record {
	t.Helper()
	manager.mu.Lock()
	job := manager.jobs[id]
	manager.mu.Unlock()
	if job == nil {
		t.Fatal("admitted actual media job is missing")
	}
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-job.done:
	case <-timer.C:
		t.Fatal("actual producer exceeded its 6 second finalization budget")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return job.record
}

type sealMediaProcess struct {
	pid       int
	pidfd     int
	startTick string
}

func sealMediaObserveProcess(t *testing.T, control, root, executable, id string) sealMediaProcess {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(control, id+".pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid owned encoder PID: %q", data)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	process := sealMediaProcess{pid: pid, pidfd: fd}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	fields := sealMediaStatFields(t, stat)
	process.startTick = fields[19]
	actualExecutable, executableErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	actualDirectory, directoryErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	expectedDirectory, expectedErr := filepath.EvalSymlinks(filepath.Join(root, id))
	command, commandErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if executableErr != nil || directoryErr != nil || expectedErr != nil || commandErr != nil ||
		actualExecutable != executable || actualDirectory != expectedDirectory || fields[2] != strconv.Itoa(pid) ||
		!bytes.Contains(command, []byte("\x00libx264\x00")) || !bytes.Contains(command, []byte("\x00-readrate\x004\x00")) ||
		!bytes.Contains(command, []byte("\x00segment\x00")) || sealMediaExited(t, process) {
		_ = unix.Close(fd)
		t.Fatal("observed process is not this live paced software VOD encoder")
	}
	return process
}

func sealMediaStatFields(t *testing.T, stat []byte) []string {
	t.Helper()
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		t.Fatal("process stat lacks its executable delimiter")
	}
	fields := strings.Fields(string(stat[end+2:]))
	if len(fields) < 20 {
		t.Fatal("process stat lacks its identity fields")
	}
	return fields
}

func sealMediaExited(t *testing.T, process sealMediaProcess) bool {
	t.Helper()
	for {
		fds := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			t.Fatalf("inspect identity-bound encoder handle: %v/%#x", err, fds[0].Revents)
		}
		return ready > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0
	}
}

func sealMediaAssertReaped(t *testing.T, process sealMediaProcess, jobDirectory string) {
	t.Helper()
	if !sealMediaExited(t, process) {
		t.Fatal("producer remains alive after job finalization")
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process.pid), "stat"))
	if err == nil && sealMediaStatFields(t, stat)[19] == process.startTick {
		t.Fatal("original encoder remained waitable after job finalization")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		if directory, err := os.Readlink(filepath.Join("/proc", entry.Name(), "cwd")); err == nil && directory == jobDirectory {
			t.Fatalf("a producer descendant still references the owned job directory: PID %s", entry.Name())
		}
	}
}

type sealMediaDecodedFacts struct {
	FirstPTS      float64 `json:"first_pts_seconds"`
	LastPTS       float64 `json:"last_pts_seconds"`
	Frames        int     `json:"decoded_frames"`
	FirstLumaMean float64 `json:"first_frame_luma_mean"`
}

func sealMediaSourceTimeline(t *testing.T, ctx context.Context, ffprobe, path string) (origin, duration float64) {
	t.Helper()
	probe := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=start_time,duration", "-of", "json", path)
	output, err := probe.Output()
	if err != nil {
		t.Fatalf("independently probe generated source clock: %v", err)
	}
	var document struct {
		Format struct {
			Start    string `json:"start_time"`
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	origin, err = strconv.ParseFloat(document.Format.Start, 64)
	if err != nil {
		t.Fatal(err)
	}
	duration, err = strconv.ParseFloat(document.Format.Duration, 64)
	if err != nil || math.IsNaN(origin) || math.IsInf(origin, 0) || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		t.Fatalf("generated source lacks a finite presentation clock: %v", err)
	}
	return origin, duration
}

func sealMediaDecode(t *testing.T, ctx context.Context, ffmpeg, ffprobe, path string) sealMediaDecodedFacts {
	t.Helper()
	probe := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time", "-of", "json", path)
	output, err := probe.Output()
	if err != nil {
		t.Fatalf("independently probe generated media: %v", err)
	}
	var document struct {
		Packets []struct {
			PTS string `json:"pts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(output, &document); err != nil || len(document.Packets) == 0 {
		t.Fatalf("generated media lacks presentation packets: %v", err)
	}
	first, err := strconv.ParseFloat(document.Packets[0].PTS, 64)
	if err != nil {
		t.Fatal(err)
	}
	last, err := strconv.ParseFloat(document.Packets[len(document.Packets)-1].PTS, 64)
	if err != nil {
		t.Fatal(err)
	}
	decode := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-err_detect", "explode", "-threads", "1",
		"-i", path, "-map", "0:v:0", "-an", "-vf", "format=gray", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	var stderr strings.Builder
	decode.Stderr = &stderr
	decoded, err := decode.Output()
	const frameBytes = 160 * 90
	if err != nil || len(decoded) == 0 || len(decoded)%frameBytes != 0 {
		t.Fatalf("independently decode generated media: %v: %s", err, stderr.String())
	}
	var luma int64
	for _, value := range decoded[:frameBytes] {
		luma += int64(value)
	}
	return sealMediaDecodedFacts{FirstPTS: first, LastPTS: last, Frames: len(decoded) / frameBytes, FirstLumaMean: float64(luma) / frameBytes}
}

func sealMediaEntries(t *testing.T, directory string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func sealMediaCountFiles(t *testing.T, directory string) (privateBytes, total int64) {
	t.Helper()
	for _, entry := range sealMediaEntries(t, directory) {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("final cache contains unreadable or nonregular output: %v", err)
		}
		total += info.Size()
		if strings.HasSuffix(entry.Name(), ".ts.tmp") {
			privateBytes += info.Size()
		}
	}
	return privateBytes, total
}

func sealMediaWaitRemoved(t *testing.T, ctx context.Context, manager *Manager, directory string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		manager.maintain()
		_, err := os.Stat(directory)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatal("invalidated cache survived the last reader's release")
		}
		sealMediaTick(t, ctx)
	}
}

func sealMediaTick(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func sealMediaLog(t *testing.T, observation any) {
	t.Helper()
	data, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("production_seal_media %s", data)
}
