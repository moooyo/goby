//go:build linux

package transcode

import (
	"bytes"
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
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"golang.org/x/sys/unix"
)

// This is deliberately one H.264 geometry and one bitmap canvas. Both formats
// exercise actual VAAPI decode/encode and Vulkan composition through the live
// runner and publication callback, rather than a finite-file command surrogate.
func TestLiveAMDActualBitmapPublicationClockAndCancellation(t *testing.T) {
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" {
		t.Skip("an explicitly selected AMD VAAPI device is required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	setup, cancelSetup := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelSetup()
	chunks, info := liveAMDSource(t, setup, ffmpeg, ffprobe, true)
	empty, emptyInfo := liveAMDSource(t, setup, ffmpeg, ffprobe, false)
	for _, format := range []string{"mpegts", "fmp4"} {
		t.Run(format, func(t *testing.T) {
			p := liveAMDPlan(info, device, format)
			run := startLiveAMD(t, ffmpeg, ffprobe, p)
			// Default input probing can inspect five seconds. No displayed
			// PGS event exists in these supplied bytes, and EOF stays withheld.
			run.feedUntil(t, chunks, 5.5)
			run.waitPublished(t, func(count int, end int64) bool { return count >= 2 })
			pidfd := run.assertGPUProcess(t, device)
			defer unix.Close(pidfd)
			run.feedUntil(t, chunks, 9.5)
			run.waitPublished(t, func(_ int, end int64) bool { return end >= LiveSourceClockBiasTicks(p)+8*ticksPerSecond })
			run.feedUntil(t, chunks, math.Inf(1))
			for _, writer := range run.writers {
				_ = writer.Close()
			}
			completed := run.wait(t)
			if completed.err != nil || completed.result.ExitCode != 0 {
				t.Fatalf("actual AMD live publication failed: %v: %s", completed.err, completed.result.StderrTail)
			}
			if !gpuProducerExited(t, pidfd) {
				t.Fatal("completed live GPU producer retained its process")
			}
			run.verifyPublication(t, ffmpeg, ffprobe, true)
			entries, err := os.ReadDir(run.directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("completed live GPU scratch was retained: %v %v", entries, err)
			}
		})
	}
	t.Run("cancel_empty_subtitle_stream", func(t *testing.T) {
		p := liveAMDPlan(emptyInfo, device, "fmp4")
		run := startLiveAMD(t, ffmpeg, ffprobe, p)
		run.feedUntil(t, empty, 5.5)
		run.waitPublished(t, func(count int, _ int64) bool { return count >= 2 })
		pidfd := run.assertGPUProcess(t, device)
		defer unix.Close(pidfd)
		prefix, _, _, _, before, _ := run.published.snapshot()
		start := time.Now()
		run.cancel()
		completed := run.wait(t)
		if !errors.Is(completed.err, context.Canceled) || !gpuProducerExited(t, pidfd) || time.Since(start) > 6*time.Second {
			t.Fatalf("cancellation did not retire the actual GPU producer: %v: %s", completed.err, completed.result.StderrTail)
		}
		final, _, _, _, after, handles := run.published.snapshot()
		if before < 2 || after < before || !bytes.HasPrefix(final, prefix) {
			t.Fatal("cancellation rewrote already published bitmap-free media")
		}
		for _, handle := range handles {
			if _, err := handle.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("cancelled publisher retained a borrowed media descriptor")
			}
		}
		run.inputs.close()
		for _, writer := range run.writers {
			if _, err := writer.Write([]byte{0}); err == nil {
				t.Fatal("cancelled input retained an owned pipe reader")
			}
		}
		output := filepath.Join(t.TempDir(), "published-prefix.mp4")
		if err := os.WriteFile(output, final, 0600); err != nil {
			t.Fatal(err)
		}
		verifyContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		strictDecodeProgressiveVideo(t, verifyContext, ffmpeg, output)
		pixels := decodeProgressiveVideoPixels(t, verifyContext, ffmpeg, output)
		if len(pixels) < 32*320*192 || len(pixels)%(320*192) != 0 {
			t.Fatal("cancelled live prefix lacks complete published video frames")
		}
		for _, pixel := range pixels {
			if pixel > 180 {
				t.Fatal("empty live subtitle stream burned a visible caption")
			}
		}
		entries, err := os.ReadDir(run.directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				assertProgressiveFileClosed(t, filepath.Join(run.directory, entry.Name()))
			}
		}
	})
}

func liveAMDSource(t *testing.T, ctx context.Context, ffmpeg, ffprobe string, cue bool) ([]liveBitmapChunk, media.Info) {
	t.Helper()
	directory := t.TempDir()
	sup, source := filepath.Join(directory, "authored.sup"), filepath.Join(directory, "source.mkv")
	if err := os.WriteFile(sup, liveBitmapPGSFixture(cue), 0600); err != nil {
		t.Fatal(err)
	}
	// copyts is essential when the first SUP packet is at six seconds:
	// independently normalizing that input would move the cue to source zero.
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-copyts", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=black:s=320x192:r=16:d=12", "-f", "sup", "-i", sup,
		"-map", "0:v:0", "-map", "1:s:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast",
		"-pix_fmt", "yuv420p", "-bf", "0", "-g", "16", "-keyint_min", "16", "-sc_threshold", "0",
		"-c:s", "copy", "-cluster_time_limit", "250", "-t", "12", source)
	assertLiveBitmapSourceEventClock(t, ctx, ffprobe, source, cue)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 2 || info.Streams[0].Codec != "h264" || info.Streams[0].Width != 320 ||
		info.Streams[0].Height != 192 || info.Streams[0].PixelFormat != "yuv420p" || info.Streams[1].Codec != "hdmv_pgs_subtitle" {
		t.Fatal("AMD live fixture does not contain the selected real H.264/PGS streams", err)
	}
	data, err := os.ReadFile(source)
	if err != nil || len(data) > 4<<20 {
		t.Fatal("AMD live fixture exceeded its source bound", err)
	}
	return liveBitmapClusters(t, data), info
}

func liveAMDPlan(info media.Info, device, format string) Plan {
	p := Plan{SourceMode: "stream", Container: "ts", VideoCodec: "h264", VideoStreamIndex: info.Streams[0].Index, AudioStreamIndex: -1,
		Width: 320, Height: 192, FrameRate: 16, VideoBitrate: 768000, SegmentSeconds: 1,
		SourceFormatStartKnown: info.FormatStartKnown, SourceFormatStartTicks: info.FormatStartTicks,
		Hardware: Hardware{Decode: "vaapi", Encode: "vaapi", Device: device}, VideoFilters: VideoFilters{Backend: "vulkan"},
		Subtitle: SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: info.Streams[1].Index}, HLS: HLSPlan{SegmentType: format}}
	if format == "fmp4" {
		p.Container = "mp4"
	}
	return p
}

type liveAMDCompletion struct {
	result RunResult
	err    error
}
type liveAMDRun struct {
	ctx                            context.Context
	cancel                         context.CancelFunc
	inputs                         StreamInputs
	writers                        []*os.File
	plan                           Plan
	directory, pidPath, executable string
	position                       int
	published                      *liveAMDPublication
	done                           chan liveAMDCompletion
	finished                       chan struct{}
}

func startLiveAMD(t *testing.T, ffmpeg, ffprobe string, p Plan) *liveAMDRun {
	t.Helper()
	resolved, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	realExecutable, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	pidPath, wrapper := filepath.Join(control, "producer.pid"), filepath.Join(control, "owned-ffmpeg")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	program := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$$\" > " + quote(pidPath) + "\nexec " + quote(realExecutable) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	run := &liveAMDRun{ctx: ctx, cancel: cancel, plan: p, directory: t.TempDir(), pidPath: pidPath, executable: realExecutable,
		published: &liveAMDPublication{format: p.HLS.SegmentType, ffprobe: ffprobe, changed: make(chan struct{})},
		done:      make(chan liveAMDCompletion, 1), finished: make(chan struct{})}
	for index := 0; index < 2; index++ {
		reader, writer, err := os.Pipe()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if index == 0 {
			run.inputs.Media = reader
		} else {
			run.inputs.Bitmap = reader
		}
		run.writers = append(run.writers, writer)
	}
	stop := context.AfterFunc(ctx, func() {
		for _, writer := range run.writers {
			_ = writer.Close()
		}
	})
	t.Cleanup(func() {
		cancel()
		run.inputs.close()
		for _, writer := range run.writers {
			_ = writer.Close()
		}
		select {
		case <-run.finished:
		case <-time.After(6 * time.Second):
			t.Error("AMD live producer exceeded its cleanup grace")
		}
		stop()
	})
	live := liveRuntime{inputs: run.inputs, maxBytes: 16 << 20, timeout: 15 * time.Second, publish: run.published.publish}
	go func() {
		defer close(run.finished)
		result, err := Run(withLiveRuntime(ctx, live), wrapper, run.directory, run.inputs.Media, p, 1, nil)
		run.done <- liveAMDCompletion{result, err}
	}()
	return run
}

func (run *liveAMDRun) feedUntil(t *testing.T, chunks []liveBitmapChunk, end float64) {
	t.Helper()
	var data []byte
	for run.position < len(chunks) && chunks[run.position].time <= end {
		data = append(data, chunks[run.position].data...)
		run.position++
	}
	var pumps sync.WaitGroup
	failures := make(chan error, 2)
	for _, writer := range run.writers {
		pumps.Add(1)
		go func() { defer pumps.Done(); _, err := writer.Write(data); failures <- err }()
	}
	pumps.Wait()
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal("authorized AMD live input stopped accepting its next bounded source interval", err)
		}
	}
}

func (run *liveAMDRun) waitPublished(t *testing.T, ready func(int, int64) bool) {
	t.Helper()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	for {
		run.published.mu.Lock()
		count, end, changed := run.published.count, run.published.end, run.published.changed
		run.published.mu.Unlock()
		if ready(count, end) {
			return
		}
		select {
		case <-changed:
		case <-run.finished:
			completed := <-run.done
			t.Fatalf("AMD live producer ended before the gated interval was published: %v: %s", completed.err, completed.result.StderrTail)
		case <-deadline.C:
			t.Fatal("AMD live output stalled while future PGS events or EOF were unavailable")
		}
	}
}

func (run *liveAMDRun) wait(t *testing.T) liveAMDCompletion {
	t.Helper()
	select {
	case result := <-run.done:
		return result
	case <-time.After(6 * time.Second):
		t.Fatal("AMD live producer did not finish within its bounded process grace")
		return liveAMDCompletion{}
	}
}

func (run *liveAMDRun) assertGPUProcess(t *testing.T, device string) int {
	t.Helper()
	data, err := os.ReadFile(run.pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatal("invalid owned live GPU process identity")
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	valid := false
	defer func() {
		if !valid {
			_ = unix.Close(pidfd)
		}
	}()
	process := filepath.Join("/proc", strconv.Itoa(pid))
	executable, err := os.Readlink(filepath.Join(process, "exe"))
	if err != nil || executable != run.executable {
		t.Fatal("observed GPU producer is not the actual configured FFmpeg")
	}
	directory, err := os.Readlink(filepath.Join(process, "cwd"))
	expected, expectedErr := filepath.EvalSymlinks(run.directory)
	if err != nil || expectedErr != nil || directory != expected {
		t.Fatal("observed GPU process does not belong to this live job")
	}
	command, err := os.ReadFile(filepath.Join(process, "cmdline"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"\x00-hwaccel\x00vaapi\x00", "\x00-hwaccel_output_format\x00vaapi\x00", "\x00h264_vaapi\x00", "libplacebo=inputs=2", "\x00-i\x00pipe:3\x00", "\x00-i\x00pipe:6\x00"} {
		if !bytes.Contains(command, []byte(required)) {
			t.Fatalf("live GPU process omitted required hardware/input stage %q", required)
		}
	}
	files, err := os.ReadDir(filepath.Join(process, "fd"))
	if err != nil {
		t.Fatal(err)
	}
	deviceOpen := false
	for _, file := range files {
		target, err := os.Readlink(filepath.Join(process, "fd", file.Name()))
		deviceOpen = deviceOpen || err == nil && target == device
	}
	if !deviceOpen || gpuProducerExited(t, pidfd) {
		t.Fatal("published live producer does not own the selected active GPU device")
	}
	valid = true
	return pidfd
}

type liveAMDPublication struct {
	mu                              sync.Mutex
	format, ffprobe                 string
	media                           bytes.Buffer
	init                            []byte
	firstPre, firstPost, delta, end int64
	count                           int
	handles                         []*os.File
	changed                         chan struct{}
}

func (published *liveAMDPublication) publish(ctx context.Context, _ Spec, _ string, bundle LiveSegment) error {
	published.mu.Lock()
	defer published.mu.Unlock()
	if bundle.RenditionCount != 1 || bundle.Sequence != int64(published.count) || bundle.DurationTicks <= 0 || bundle.DurationTicks > 2*ticksPerSecond ||
		published.count > 0 && !liveTicksClose(bundle.StartTicks, published.end) {
		return ErrInvalidTimeline
	}
	actual := bundle.Renditions[0]
	if actual.Media == nil || actual.Size <= 0 || actual.Size > 4<<20 || int64(published.media.Len())+actual.Size > 16<<20 {
		return ErrQuota
	}
	post, err := MeasureHLSMuxClock(ctx, published.ffprobe, actual.Init, actual.Media, true)
	if err != nil {
		return err
	}
	delta := post - actual.PreMuxClockTicks
	if published.count == 0 {
		published.firstPre, published.firstPost, published.delta = actual.PreMuxClockTicks, post, delta
	} else if math.Abs(float64(delta-published.delta)) > 112 {
		return fmt.Errorf("live GPU container reset its source clock")
	}
	if published.format == "fmp4" {
		if actual.Init == nil {
			return ErrInvalidTimeline
		}
		stat, err := actual.Init.Stat()
		if err != nil || stat.Size() <= 0 || stat.Size() > MaxProgressivePrefixBytes {
			return ErrInvalidTimeline
		}
		initialization := make([]byte, stat.Size())
		if _, err := actual.Init.ReadAt(initialization, 0); err != nil {
			return err
		}
		if published.count == 0 {
			published.init = initialization
			_, _ = published.media.Write(initialization)
		} else if !bytes.Equal(initialization, published.init) {
			return fmt.Errorf("live GPU initialization changed within its epoch")
		}
		published.handles = append(published.handles, actual.Init)
	} else if actual.Init != nil {
		return ErrInvalidTimeline
	}
	if _, err := io.Copy(&published.media, io.NewSectionReader(actual.Media, 0, actual.Size)); err != nil {
		return err
	}
	published.handles = append(published.handles, actual.Media)
	published.end = bundle.StartTicks + bundle.DurationTicks
	published.count++
	close(published.changed)
	published.changed = make(chan struct{})
	return nil
}

func (published *liveAMDPublication) snapshot() ([]byte, int64, int64, int64, int, []*os.File) {
	published.mu.Lock()
	defer published.mu.Unlock()
	return append([]byte(nil), published.media.Bytes()...), published.firstPre, published.firstPost, published.end, published.count, append([]*os.File(nil), published.handles...)
}

func (run *liveAMDRun) verifyPublication(t *testing.T, ffmpeg, ffprobe string, cue bool) {
	t.Helper()
	data, pre, post, end, count, handles := run.published.snapshot()
	// The fixed one-second transport headroom can introduce a short leading
	// cut. Frame count and the complete measured interval remain exact.
	if count < 11 || count > 13 || !liveTicksClose(pre, LiveSourceClockBiasTicks(run.plan)) || !liveTicksClose(end-pre, 12*ticksPerSecond) {
		t.Fatalf("AMD live publication changed the complete source interval: count=%d pre=%d end=%d", count, pre, end)
	}
	for _, handle := range handles {
		if _, err := handle.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("completed live publication retained a borrowed descriptor")
		}
	}
	output := filepath.Join(t.TempDir(), "published.bin")
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
	facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
	pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, output)
	if len(facts.video) != 192 || len(facts.audio) != 0 || len(pixels) != 192*320*192 {
		t.Fatal("AMD live publication dropped or repeated decoded media")
	}
	for index, frame := range facts.video {
		want := float64(post)/float64(ticksPerSecond) + float64(index)/16
		if math.Abs(frame.time(t)-want) > .002 {
			t.Fatalf("live GPU bitmap changed frame %d PTS: got=%g want=%g", index, frame.time(t), want)
		}
		inside, outside := 0, 0
		for position, pixel := range pixels[index*320*192 : (index+1)*320*192] {
			if pixel <= 180 {
				continue
			}
			x, y := position%320, position/320
			if x >= 112 && x < 208 && y >= 144 && y < 168 {
				inside++
			} else if x < 108 || x >= 212 || y < 140 || y >= 172 {
				outside++
			}
		}
		visible := cue && index >= 96 && index < 112
		if visible && inside < 2000 || !visible && inside > 4 || outside > 4 {
			t.Fatalf("live GPU bitmap display/clear is wrong at frame %d: inside=%d outside=%d", index, inside, outside)
		}
	}
	first, last := facts.video[0], facts.video[len(facts.video)-1]
	if math.Abs(last.time(t)+last.duration(t)-first.time(t)-12) > .002 {
		t.Fatal("live GPU output changed its first-to-last decoded frame span")
	}
}
