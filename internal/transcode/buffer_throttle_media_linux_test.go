//go:build linux

package transcode

import (
	"context"
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

// This opt-in test exercises Goby's process control with actual software media.
// Its paced fixture is independent of native Emby's stdin throttle mechanism.
func TestActualHLSPlaybackBufferThrottleMedia(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Linux media verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	filter := "nullsrc=size=160x90:rate=8:duration=120,geq=lum='if(lt(T,60),48,208)+32*sin(X*17+Y*13+T*11)':cb=128:cr=128,format=yuv420p"
	generate := exec.CommandContext(ctx, executable, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", filter, "-c:v", "ffv1", "-threads:v", "1", fixture)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate distinct source regions: %v: %s", err, output)
	}
	origin, duration := sealMediaSourceTimeline(t, ctx, ffprobe, fixture)
	if math.Abs(origin) > .000001 || math.Abs(duration-120) > .13 {
		t.Fatalf("actual fixture has an unexpected presentation clock: origin=%f duration=%f", origin, duration)
	}
	durationTicks := int64(math.Round(duration * float64(ticksPerSecond)))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(control, "paced-buffer-ffmpeg")
	program := "#!/bin/sh\nset -eu\ndirectory=\"$(pwd -P)\"\njob=\"${directory##*/}\"\nprintf '%s\\n' \"$$\" > " +
		quote(control) + "/\"$job.pid\"\nexec " + quote(executable) + " -readrate 4 \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}

	for index, scenario := range []struct {
		name         string
		startSeconds int
		readers      int
		retainPrefix bool
		completeTail bool
	}{
		{name: "cancel_zero_start_single_reader", readers: 1},
		{name: "deliver_tail_nonzero_start_retained_reader", startSeconds: 60, readers: 8, retainPrefix: true, completeTail: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			options := managerTestOptions(t, nil)
			options.FFmpegPath, options.Threads = wrapper, 1
			options.MaxBytes, options.MaxJobBytes = 64<<20, 16<<20
			options.MaxReaders, options.MaxJobReaders = scenario.readers, scenario.readers
			options.IdleTimeout, options.StartupTimeout = 2*time.Minute, 20*time.Second
			options.NoProgressTimeout, options.MaxRuntime = 1500*time.Millisecond, 90*time.Second
			options.pollInterval = 25 * time.Millisecond
			manager := newTestManager(t, options)
			start := int64(scenario.startSeconds) * ticksPerSecond
			plan := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
				DurationTicks: durationTicks, StartTicks: start, EndTicks: durationTicks,
				Width: 160, Height: 90, FrameRate: 8, VideoBitrate: 250_000,
				SegmentSeconds: 6, SegmentMode: "vod", SegmentStartNumber: scenario.startSeconds / 6}
			var cuts []string
			for second := scenario.startSeconds + 6; second < 120; second += 6 {
				cuts = append(cuts, strconv.FormatInt(int64(second)*ticksPerSecond, 10))
			}
			plan.SegmentTimes = strings.Join(cuts, ",")
			plan, err := CaptureExecution(plan, DefaultExecutionOptions(1))
			if err != nil {
				t.Fatal(err)
			}
			spec := managerTestSpec(100 + index)
			spec.Plan = plan
			admission := WithPlaybackBufferThrottling(ctx, true, PlaybackBufferDemand{Known: true, Revision: 1, PositionTicks: start})
			input := sealMediaInput(t, fixture)
			record, err := manager.Ensure(admission, spec, input)
			if err != nil {
				t.Fatal(err)
			}
			firstName := fmt.Sprintf("segment-%06d.ts", plan.SegmentStartNumber)
			prefix, err := manager.Open(ctx, spec.Scope, record.ID, firstName)
			if err != nil {
				t.Fatalf("initial artifact did not receive one startup demand lease: %v", err)
			}
			defer prefix.Close()
			prefixData, err := io.ReadAll(prefix)
			if err != nil || len(prefixData) == 0 {
				t.Fatalf("read actual initial segment: %v", err)
			}
			prefixPath := filepath.Join(t.TempDir(), "prefix.ts")
			if err := os.WriteFile(prefixPath, prefixData, 0600); err != nil {
				t.Fatal(err)
			}
			if !scenario.retainPrefix {
				if err := prefix.Close(); err != nil {
					t.Fatal(err)
				}
			}
			prefixFacts := sealMediaDecode(t, ctx, executable, ffprobe, prefixPath)
			if math.Abs(prefixFacts.FirstPTS-float64(scenario.startSeconds+1)) > .13 || prefixFacts.Frames != 48 ||
				scenario.startSeconds == 0 && prefixFacts.FirstLumaMean >= 100 ||
				scenario.startSeconds != 0 && prefixFacts.FirstLumaMean <= 150 {
				t.Fatalf("initial segment does not represent the requested source epoch: %+v", prefixFacts)
			}
			process := playbackBufferMediaProcess(t, control, manager, record.ID)
			defer unix.Close(process.pidfd)
			first := playbackBufferMediaWaitPaused(t, ctx, manager, spec.Scope, record.ID, process, 0)
			if first.outputTicks < 18*ticksPerSecond || first.outputTicks > 45*ticksPerSecond || first.waiters != 0 {
				t.Fatalf("first stop did not bound a real source-relative buffer: output=%d waiters=%d", first.outputTicks, first.waiters)
			}
			playlist := sealMediaPlaylist(t, manager, spec.Scope, record.ID)
			var publishedTicks int64
			for _, segment := range playlist.Segments {
				publishedTicks += segment.DurationTicks
			}
			if playlist.Ended || publishedTicks < 18*ticksPerSecond {
				t.Fatalf("initial offset caused a stop before enough real source media: %+v", playlist)
			}
			playbackBufferMediaAssertStablePause(t, ctx, manager, spec.Scope, record.ID, process, first, 2100*time.Millisecond)
			playbackBufferMediaRejectUnrelatedDemand(t, ctx, manager, spec, record.ID, process, start)
			duplicateInput := sealMediaInput(t, fixture)
			replacementContext := WithPlaybackBufferThrottling(ctx, false, PlaybackBufferDemand{Known: true, Revision: 99, PositionTicks: durationTicks})
			duplicate, err := manager.Ensure(replacementContext, spec, duplicateInput)
			if err != nil || duplicate.ID != record.ID || duplicate.Spec != record.Spec {
				t.Fatalf("later policy changed an admitted output or process: %+v/%v", duplicate, err)
			}
			assertManagerInputClosed(t, duplicateInput)
			retained := playbackBufferMediaObserve(t, manager, spec.Scope, record.ID, process)
			if !retained.paused || retained.state != "T" || retained.demand.Revision != 1 || retained.demand.PositionTicks != start {
				t.Fatal("an existing job adopted a later caller's scheduling policy")
			}
			if err := manager.UpdatePlaybackBufferDemand(spec.Scope, PlaybackBufferDemand{Known: true, Revision: 2, PositionTicks: start + 20*ticksPerSecond}); err != nil {
				t.Fatal(err)
			}
			second := playbackBufferMediaWaitPaused(t, ctx, manager, spec.Scope, record.ID, process, first.outputTicks+5*ticksPerSecond)
			if second.demand.Revision != 2 || second.demand.PositionTicks != start+20*ticksPerSecond || second.waiters != 0 {
				t.Fatalf("new committed progress did not resume the same scoped producer: %+v", second)
			}
			if scenario.retainPrefix {
				if _, err := prefix.Stat(); err != nil {
					t.Fatalf("buffer control lost a retained cache pin: %v", err)
				}
			}
			if !scenario.completeTail {
				// Pin existing bytes without production credit so maintenance
				// cannot reclaim the cancelled job before its terminal snapshot.
				cancelPrefix, err := manager.TryOpen(spec.Scope, record.ID, firstName)
				if err != nil {
					t.Fatalf("retain cache ownership before cancellation: %v", err)
				}
				defer cancelPrefix.Close()
				pinned := playbackBufferMediaObserve(t, manager, spec.Scope, record.ID, process)
				if pinned.state != "T" || !pinned.paused || pinned.waiters != 0 {
					t.Fatal("a cached pin granted production demand before cancellation")
				}
				stop, cancelStop := context.WithTimeout(ctx, 6*time.Second)
				err = manager.Cancel(stop, spec.Scope)
				cancelStop()
				if err != nil {
					t.Fatalf("cancel stopped process group: %v", err)
				}
				sealMediaWaitDone(t, ctx, manager, record.ID)
				final, snapshotErr := manager.Snapshot(spec.Scope, record.ID)
				if !errors.Is(snapshotErr, ErrJobCancelled) {
					t.Fatalf("cancelled producer lost its terminal ownership projection: %v", snapshotErr)
				}
				if final.State != "cancelled" || final.ErrorCode != "cancelled" || final.ProductionSealed || manager.Metrics().Running != 0 {
					t.Fatalf("stop retained production or corrupted its cancellation state: %+v/%+v", final, manager.Metrics())
				}
				assertManagerInputClosed(t, input)
				sealMediaAssertReaped(t, process, filepath.Join(manager.options.Root, record.ID))
				return
			}
			// The last segment remains private until the process finishes. Its
			// pending delivery lease must run through the tail publication boundary.
			lastName := "segment-000019.ts"
			tail, err := manager.Open(ctx, spec.Scope, record.ID, lastName)
			if err != nil {
				t.Fatalf("pending tail did not resume and complete the same encoder: %v", err)
			}
			defer tail.Close()
			tailData, err := io.ReadAll(tail)
			if err != nil || len(tailData) == 0 {
				t.Fatalf("read completed tail: %v", err)
			}
			tailPath := filepath.Join(t.TempDir(), "tail.ts")
			if err := os.WriteFile(tailPath, tailData, 0600); err != nil {
				t.Fatal(err)
			}
			sealMediaWaitDone(t, ctx, manager, record.ID)
			final, snapshotErr := manager.Snapshot(spec.Scope, record.ID)
			if snapshotErr != nil {
				t.Fatalf("completed producer lost its terminal ownership projection: %v", snapshotErr)
			}
			if final.State != "completed" || final.ErrorCode != "" || final.ProductionSealed || manager.Metrics().Running != 0 {
				t.Fatalf("resumed finite producer did not complete normally: %+v/%+v", final, manager.Metrics())
			}
			assertManagerInputClosed(t, input)
			sealMediaAssertReaped(t, process, filepath.Join(manager.options.Root, record.ID))
			tailFacts := sealMediaDecode(t, ctx, executable, ffprobe, tailPath)
			if math.Abs(tailFacts.FirstPTS-115) > .13 || tailFacts.Frames != 48 || tailFacts.FirstLumaMean <= 150 {
				t.Fatalf("tail source frames or presentation epoch changed across resume: %+v", tailFacts)
			}
			complete := sealMediaPlaylist(t, manager, spec.Scope, record.ID)
			if !complete.Ended || complete.Type != "VOD" || complete.Sequence != int64(plan.SegmentStartNumber) || len(complete.Segments) != len(cuts)+1 {
				t.Fatalf("pending tail did not retain complete VOD publication: %+v", complete)
			}
			fullFacts := sealMediaDecode(t, ctx, executable, ffprobe, filepath.Join(manager.options.Root, record.ID, "main.m3u8"))
			if fullFacts.Frames != (120-scenario.startSeconds)*8 || math.Abs(fullFacts.FirstPTS-float64(scenario.startSeconds+1)) > .13 {
				t.Fatalf("resuming one PID lost or duplicated source frames: %+v", fullFacts)
			}
			_, total := sealMediaCountFiles(t, filepath.Join(manager.options.Root, record.ID))
			if total != final.OutputBytes {
				t.Fatalf("completed files lost their actual byte charge: files=%d record=%d", total, final.OutputBytes)
			}
			t.Logf("buffer_throttle_media start_seconds=%d pid=%d start_tick=%s first_pause_ticks=%d second_pause_ticks=%d decoded_frames=%d",
				scenario.startSeconds, process.pid, process.startTick, first.outputTicks, second.outputTicks, fullFacts.Frames)
		})
	}
}

type playbackBufferMediaObservation struct {
	state       string
	cpuTicks    uint64
	bytes       int64
	outputTicks int64
	waiters     int
	paused      bool
	record      Record
	demand      PlaybackBufferDemand
}

func playbackBufferMediaProcess(t *testing.T, control string, manager *Manager, id string) sealMediaProcess {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(control, id+".pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("paced wrapper did not identify its owned encoder: %q", data)
	}
	// The selected executable can be an exec-only deployment wrapper. Bind
	// the observation to its live leaf, PIDfd, group, start tick, cwd and argv.
	leaf, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		t.Fatal(err)
	}
	return sealMediaObserveProcess(t, control, manager.options.Root, leaf, id)
}

func playbackBufferMediaObserve(t *testing.T, manager *Manager, scope Scope, id string, process sealMediaProcess) playbackBufferMediaObservation {
	t.Helper()
	if sealMediaExited(t, process) {
		t.Fatal("owned encoder exited before its requested pause or resume")
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process.pid), "stat"))
	if err != nil {
		t.Fatal(err)
	}
	fields := sealMediaStatFields(t, stat)
	if fields[19] != process.startTick || fields[2] != strconv.Itoa(process.pid) {
		t.Fatal("pause/resume observation changed the original process identity")
	}
	user, userErr := strconv.ParseUint(fields[11], 10, 64)
	system, systemErr := strconv.ParseUint(fields[12], 10, 64)
	if userErr != nil || systemErr != nil {
		t.Fatalf("owned encoder has an invalid CPU clock: %v", errors.Join(userErr, systemErr))
	}
	total := playbackBufferMediaBytes(t, filepath.Join(manager.options.Root, id))
	manager.mu.Lock()
	job := manager.jobs[id]
	if job == nil || job.record.Spec.Scope != scope || job.bufferThrottle == nil {
		manager.mu.Unlock()
		t.Fatal("actual encoder lost its scoped scheduling controller")
	}
	record := job.record
	record.ProductionSealed = job.productionSealed
	observation := playbackBufferMediaObservation{state: fields[0], cpuTicks: user + system, bytes: total,
		outputTicks: job.outputTicks, waiters: job.bufferWaiters, paused: job.bufferThrottle.isPaused(), record: record}
	job.bufferThrottle.mu.Lock()
	observation.demand = job.bufferThrottle.demand
	job.bufferThrottle.mu.Unlock()
	manager.mu.Unlock()
	return observation
}

func playbackBufferMediaWaitPaused(t *testing.T, ctx context.Context, manager *Manager, scope Scope, id string, process sealMediaProcess, minimumTicks int64) playbackBufferMediaObservation {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		observation := playbackBufferMediaObserve(t, manager, scope, id, process)
		if observation.record.State != "queued" && observation.record.State != "running" {
			t.Fatalf("actual producer stopped before an automatic pause: %+v", observation.record)
		}
		if observation.state == "T" && observation.paused && observation.outputTicks >= minimumTicks {
			// Allow the separate VOD publication goroutine to finish its last
			// closed successor before measuring stable bytes and process ticks.
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			settled := playbackBufferMediaObserve(t, manager, scope, id, process)
			if settled.state == "T" && settled.paused && settled.outputTicks >= minimumTicks && settled.outputTicks == observation.outputTicks &&
				settled.cpuTicks == observation.cpuTicks && settled.bytes == observation.bytes {
				return settled
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("same encoder did not pause after actual output advanced: %+v", observation)
		}
		sealMediaTick(t, ctx)
	}
}

func playbackBufferMediaBytes(t *testing.T, directory string) int64 {
	t.Helper()
	var total int64
	for _, entry := range sealMediaEntries(t, directory) {
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			// The independent VOD publisher can rename a closed file while
			// the running process is being observed. Stable stopped samples
			// below must still agree in bytes and CPU/output clocks.
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("inspect actual output size: %v", err)
		}
		total += info.Size()
	}
	return total
}

func playbackBufferMediaAssertStablePause(t *testing.T, ctx context.Context, manager *Manager, scope Scope, id string, process sealMediaProcess, baseline playbackBufferMediaObservation, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		manager.maintain()
		current := playbackBufferMediaObserve(t, manager, scope, id, process)
		if current.state != "T" || !current.paused || current.record.State != "running" || current.record.ProductionSealed ||
			current.cpuTicks != baseline.cpuTicks || current.bytes != baseline.bytes || current.outputTicks != baseline.outputTicks || current.waiters != 0 {
			t.Fatalf("actual paused output changed or was killed by the no-progress timeout: before=%+v after=%+v", baseline, current)
		}
		sealMediaTick(t, ctx)
	}
}

func playbackBufferMediaRejectUnrelatedDemand(t *testing.T, ctx context.Context, manager *Manager, spec Spec, id string, process sealMediaProcess, start int64) {
	t.Helper()
	badNumber := spec.Plan.SegmentStartNumber + 8
	for _, name := range []string{
		fmt.Sprintf("segment-%08d.ts", badNumber),
		fmt.Sprintf("segment-%d.ts", badNumber),
		fmt.Sprintf("v1-segment-%06d.ts", badNumber),
		fmt.Sprintf("segment-%06d.m4s", badNumber),
		"segment-000020.ts",
	} {
		request, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		handle, err := manager.Open(request, spec.Scope, id, name)
		cancel()
		if handle != nil {
			_ = handle.Close()
		}
		if handle != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("unconstructible artifact acquired production credit: name=%s err=%v", name, err)
		}
	}
	if err := manager.UpdatePlaybackBufferDemand(spec.Scope, PlaybackBufferDemand{Known: true, Revision: 1, PositionTicks: spec.Plan.DurationTicks}); err != nil {
		t.Fatal(err)
	}
	foreign := spec.Scope
	foreign.AuthSessionID = "different-authentication-owner"
	if err := manager.UpdatePlaybackBufferDemand(foreign, PlaybackBufferDemand{Known: true, Revision: 99, PositionTicks: spec.Plan.DurationTicks}); err != nil {
		t.Fatal(err)
	}
	current := playbackBufferMediaObserve(t, manager, spec.Scope, id, process)
	if current.state != "T" || !current.paused || current.waiters != 0 || current.demand.Revision != 1 || current.demand.PositionTicks != start {
		t.Fatalf("unrelated artifact, stale report or foreign scope resumed this encoder: %+v", current)
	}
}
