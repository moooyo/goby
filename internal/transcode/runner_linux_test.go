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
	case "progressive-slow":
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			return 96
		}
		args := append([]string{ffmpeg, "-readrate", "1"}, os.Args[1:]...)
		if syscall.Exec(ffmpeg, args, os.Environ()) != nil {
			return 97
		}
		return 98
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

func TestVODPublicationWaitsForClosedSegments(t *testing.T) {
	directory := t.TempDir()
	p := commandPlan()
	p.SegmentMode = "vod"
	p.DurationTicks = 6 * ticksPerSecond
	p.SegmentStartNumber = 2
	p.SegmentTimes = "30000000"
	publisher := &vodPublisher{directory: directory, plan: p}
	packet := make([]byte, 188)
	packet[0], packet[3] = 0x47, 0x10
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("segment-000002.ts.tmp", packet)
	private := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-ALLOW-CACHE:YES\n#EXT-X-TARGETDURATION:3\n#EXTINF:3.000000,\nsegment-000002.ts.tmp\n"
	write("segment-list.m3u8", []byte(private))
	if err := publisher.publish(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "main.m3u8")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("open segment was published")
	}
	write("segment-000003.ts.tmp", packet)
	if err := publisher.publish(false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseMediaPlaylist(data)
	if err != nil || list.Sequence != 2 || len(list.Segments) != 1 || list.Ended {
		t.Fatalf("partial publication: %+v, %v", list, err)
	}
	write("segment-list.m3u8", []byte(private+"#EXTINF:3.000000,\nsegment-000003.ts.tmp\n#EXT-X-ENDLIST\n"))
	if err := publisher.publish(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "segment-000003.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("final segment was published before exit")
	}
	if err := publisher.publish(true); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(directory, "main.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	list, err = ParseMediaPlaylist(data)
	if err != nil || !list.Ended || list.Type != "VOD" || list.Sequence != 2 || len(list.Segments) != 2 || list.Segments[0].Discontinuity || !list.Segments[1].Discontinuity {
		t.Fatalf("final publication: %+v, %v", list, err)
	}
}

func TestVODPublicationRejectsTruncatedTransportAndRetainsIOFailure(t *testing.T) {
	directory := t.TempDir()
	p := commandPlan()
	p.SegmentMode = "vod"
	if err := os.WriteFile(filepath.Join(directory, "segment-000000.ts.tmp"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	private := "#EXTM3U\n#EXT-X-TARGETDURATION:12\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:12.000000,\nsegment-000000.ts.tmp\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(directory, "segment-list.m3u8"), []byte(private), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (&vodPublisher{directory: directory, plan: p}).publish(true); err == nil {
		t.Fatal("truncated segment was published")
	}
	canceled := false
	tail := &stderrTail{cancel: func() { canceled = true }}
	_, _ = tail.Write([]byte("[error] Error writing trai"))
	_, _ = tail.Write([]byte("ler: No space left on device\n"))
	_, _ = tail.Write(bytes.Repeat([]byte("x"), maxStderrTail*2))
	if !tail.failed || !canceled {
		t.Fatal("I/O failure was lost with the rolling diagnostic tail")
	}
}

func TestRunActualVODSeekPreservesGlobalTimeline(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual VOD verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	mp4, ts := filepath.Join(dir, "source.mp4"), filepath.Join(dir, "source.ts")
	run := func(args ...string) []byte {
		t.Helper()
		data, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("media command: %v: %s", err, data)
		}
		return data
	}
	run(ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-filter_threads", "1", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=9", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=9", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-pix_fmt", "yuv420p", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-bf", "2", "-c:a", "aac", "-threads:a", "1", "-t", "9", mp4)
	run(ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-i", mp4, "-map", "0", "-c", "copy", ts)
	for _, source := range []string{mp4, ts} {
		for _, mode := range []string{"copy", "encode", "audio-aac", "audio-mp3"} {
			t.Run(filepath.Ext(source)+"/"+mode, func(t *testing.T) {
				var facts struct {
					Format struct {
						Duration string `json:"duration"`
					} `json:"format"`
				}
				if err := json.Unmarshal(run(ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "json", source), &facts); err != nil {
					t.Fatal(err)
				}
				seconds, err := strconv.ParseFloat(facts.Format.Duration, 64)
				if err != nil {
					t.Fatal(err)
				}
				duration := int64(math.Round(seconds * float64(ticksPerSecond)))
				input, err := os.Open(source)
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				keys, err := Keyframes(ctx, ffprobe, input, 0, duration)
				if err != nil {
					t.Fatal(err)
				}
				timeline, err := BuildTimeline(duration, 3, keys, mode == "copy")
				if err != nil {
					t.Fatal(err)
				}
				last := len(timeline.Segments) - 1
				results := map[int]map[int64][]vodStreamFact{}
				paths := map[int]map[int64]string{}
				var fullPlaylist MediaPlaylist
				var fullManifestPath string
				for _, first := range []int{0, 1} {
					p := Plan{Container: "ts", VideoCodec: "copy", AudioCodec: "copy", VideoStreamIndex: 0, AudioStreamIndex: 1, DurationTicks: duration, SegmentSeconds: 3, SegmentMode: "vod", SegmentStartNumber: first, StartTicks: timeline.Segments[first].StartTicks, EndTicks: timeline.Segments[last].StartTicks + timeline.Segments[last].DurationTicks}
					cuts, err := timeline.BoundaryTicks(first, last)
					if err != nil {
						t.Fatal(err)
					}
					parts := make([]string, len(cuts))
					for i, v := range cuts {
						parts[i] = strconv.FormatInt(v, 10)
					}
					p.SegmentTimes = strings.Join(parts, ",")
					if mode == "copy" && first == 0 {
						p.ReferenceStartTicks = keys[0]
					}
					if mode == "encode" {
						p.VideoCodec, p.AudioCodec, p.FrameRate, p.Width, p.Height, p.VideoBitrate, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = "h264", "aac", 24, 128, 72, 256000, 96000, 2, 48000
					}
					if strings.HasPrefix(mode, "audio-") {
						p.VideoCodec, p.VideoStreamIndex = "", -1
						p.AudioCodec, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = strings.TrimPrefix(mode, "audio-"), 96000, 2, 48000
					}
					rootPath := t.TempDir()
					if err := os.Mkdir(filepath.Join(rootPath, "job"), 0700); err != nil {
						t.Fatal(err)
					}
					rootFD, err := os.Open(rootPath)
					if err != nil {
						t.Fatal(err)
					}
					output := fmt.Sprintf("/proc/%d/fd/%d/job", os.Getpid(), rootFD.Fd())
					result, err := Run(ctx, ffmpeg, output, input, p, 1, nil)
					rootFD.Close()
					if err != nil {
						t.Fatalf("VOD %s/%d: %v: %s", mode, first, err, result.StderrTail)
					}
					playlistBytes, err := os.ReadFile(filepath.Join(rootPath, "job", "main.m3u8"))
					if err != nil {
						t.Fatal(err)
					}
					playlist, err := ParseMediaPlaylist(playlistBytes)
					if err != nil || !playlist.Ended || playlist.Type != "VOD" || playlist.Sequence != int64(first) || len(playlist.Segments) != last-first+1 {
						t.Fatalf("VOD manifest: %+v: %v", playlist, err)
					}
					results[first] = map[int64][]vodStreamFact{}
					paths[first] = map[int64]string{}
					if first == 0 {
						fullPlaylist = playlist
						fullManifestPath = filepath.Join(rootPath, "job", "main.m3u8")
					}
					for i, segment := range playlist.Segments {
						if segment.Discontinuity != (i > 0) {
							t.Fatal("generated playlist does not declare its local transport boundaries")
						}
						path := filepath.Join(rootPath, "job", segment.Name)
						paths[first][segment.Number] = path
						assertTransportResetDeclared(t, path)
						var probe struct {
							Streams []vodStreamFact `json:"streams"`
						}
						if err := json.Unmarshal(run(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,start_time,duration", "-of", "json", path), &probe); err != nil {
							t.Fatal(err)
						}
						results[first][segment.Number] = probe.Streams
						decode := []string{ffmpeg, "-v", "error", "-nostdin", "-threads", "1", "-i", path, "-map", "0:a:0"}
						if p.VideoStreamIndex >= 0 {
							decode = append(decode, "-map", "0:v:0")
						}
						decode = append(decode, "-threads", "1", "-f", "null", "-")
						if data := run(decode...); len(data) != 0 {
							t.Fatalf("standalone decoding: %s", data)
						}
					}
					if first == 1 {
						bounded := p
						bounded.SegmentTimes = ""
						bounded.EndTicks = timeline.Segments[first].StartTicks + timeline.Segments[first].DurationTicks
						boundedOutput := t.TempDir()
						result, err := Run(ctx, ffmpeg, boundedOutput, input, bounded, 1, nil)
						if err != nil {
							t.Fatalf("bounded VOD: %v: %s", err, result.StderrTail)
						}
						data, err := os.ReadFile(filepath.Join(boundedOutput, "main.m3u8"))
						if err != nil {
							t.Fatal(err)
						}
						one, err := ParseMediaPlaylist(data)
						if err != nil || len(one.Segments) != 1 || one.Sequence != int64(first) || !one.Ended {
							t.Fatalf("bounded VOD playlist: %+v: %v", one, err)
						}
						if math.Abs(float64(one.Segments[0].DurationTicks-timeline.Segments[first].DurationTicks)/float64(ticksPerSecond)) > .15 {
							t.Fatalf("bounded duration: %+v", one)
						}
					}
				}
				for number, seek := range results[1] {
					zero := results[0][number]
					wantStreams := 2
					if strings.HasPrefix(mode, "audio-") {
						wantStreams = 1
					}
					if len(zero) != wantStreams || len(seek) != wantStreams {
						t.Fatalf("missing AV streams: %v/%v", zero, seek)
					}
					for i := range zero {
						zs, _ := strconv.ParseFloat(zero[i].Start, 64)
						ss, _ := strconv.ParseFloat(seek[i].Start, 64)
						zd, _ := strconv.ParseFloat(zero[i].Duration, 64)
						sd, _ := strconv.ParseFloat(seek[i].Duration, 64)
						if zero[i].Codec != seek[i].Codec || math.Abs(zs-ss) > .15 || math.Abs(zd-sd) > .15 {
							t.Fatalf("segment %d %s timeline differs: zero=%+v seek=%+v", number, zero[i].Type, zero[i], seek[i])
						}
						if zero[i].Type == "video" {
							want := 1 + float64(timeline.Segments[number].StartTicks)/float64(ticksPerSecond)
							if math.Abs(ss-want) > 1.0/24+.002 {
								t.Fatalf("segment %d video PTS=%f want source position %f", number, ss, want)
							}
						}
					}
					t.Logf("%s/%s segment %d zero=%+v seek=%+v", filepath.Ext(source), mode, number, zero, seek)
				}
				var wholeFrames int
				for _, layout := range []string{"whole", "mixed"} {
					var manifest strings.Builder
					fmt.Fprintf(&manifest, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n", fullPlaylist.TargetDuration)
					for i, segment := range fullPlaylist.Segments {
						// Every segment was independently muxed. This is the same
						// transport discontinuity declared by the public VOD API;
						// timestamps continue to use the original source timeline.
						if i > 0 {
							manifest.WriteString("#EXT-X-DISCONTINUITY\n")
						}
						producer := 0
						if layout == "mixed" && i > 0 && i < len(fullPlaylist.Segments)-1 {
							producer = 1
						}
						fmt.Fprintf(&manifest, "#EXTINF:%s,\n%s\n", tickSeconds(segment.DurationTicks), paths[producer][segment.Number])
					}
					manifest.WriteString("#EXT-X-ENDLIST\n")
					manifestPath := fullManifestPath
					if layout == "mixed" {
						manifestPath = filepath.Join(t.TempDir(), "main.m3u8")
						if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0600); err != nil {
							t.Fatal(err)
						}
					}
					frames, elapsed := decodeContinuousVOD(t, ctx, ffmpeg, manifestPath, !strings.HasPrefix(mode, "audio-"))
					if elapsed < duration-ticksPerSecond/4 || elapsed > duration+ticksPerSecond/4 {
						t.Fatalf("%s HLS ended at %d ticks, source duration %d", layout, elapsed, duration)
					}
					if layout == "whole" {
						wholeFrames = frames
					} else if frames != wholeFrames {
						t.Fatalf("mixed producer HLS decoded %d frames, whole decoded %d", frames, wholeFrames)
					}
					t.Logf("%s/%s continuous %s HLS: frames=%d, ticks=%d", filepath.Ext(source), mode, layout, frames, elapsed)
				}
			})
		}
	}
}

type vodStreamFact struct {
	Type     string `json:"codec_type"`
	Codec    string `json:"codec_name"`
	Start    string `json:"start_time"`
	Duration string `json:"duration"`
}

func assertTransportResetDeclared(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || len(data)%188 != 0 {
		t.Fatal("invalid transport packet framing")
	}
	seen := map[int]bool{}
	for offset := 0; offset < len(data); offset += 188 {
		packet := data[offset : offset+188]
		pid := int(packet[1]&31)<<8 | int(packet[2])
		if pid == 8191 || seen[pid] {
			continue
		}
		seen[pid] = true
		if packet[0] != 0x47 || packet[3]&0x20 == 0 || packet[4] == 0 || packet[5]&0x80 == 0 {
			t.Fatalf("PID %d resets continuity without a transport discontinuity indicator", pid)
		}
	}
}

func decodeContinuousVOD(t *testing.T, ctx context.Context, ffmpeg, manifest string, video bool) (int, int64) {
	t.Helper()
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", "-protocol_whitelist", "file,pipe", "-allowed_extensions", "m3u8,ts", "-prefer_x_start", "0", "-f", "hls", "-i", manifest, "-map", "0:a:0"}
	if video {
		args = append(args, "-map", "0:v:0")
	}
	args = append(args, "-threads", "1", "-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var output, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	if err := cmd.Run(); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("strict continuous HLS decode: %v: %s", err, diagnostic.String())
	}
	var frames int
	var elapsed int64
	ended := false
	for _, line := range strings.Split(output.String(), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "frame":
			frames, _ = strconv.Atoi(value)
		case "out_time_us":
			microseconds, _ := strconv.ParseInt(value, 10, 64)
			elapsed = microseconds * 10
		case "progress":
			ended = value == "end"
		}
	}
	if !ended || video && frames == 0 || elapsed <= 0 {
		t.Fatalf("incomplete continuous HLS progress: %s", output.String())
	}
	return frames, elapsed
}
