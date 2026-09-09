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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const timelineProbeExample = "packet|stream_index=0|pts_time=1.483333|flags=K__|\n" +
	"packet|stream_index=0|pts_time=1.608333|flags=___|\n" +
	"packet|stream_index=0|pts_time=1.525000|flags=___|\n" +
	"packet|stream_index=0|pts_time=3.483333|flags=K__|\n" +
	"program|stream|index=0|codec_type=video|time_base=1/90000|start_time=1.483333\n\n" +
	"stream|index=0|codec_type=video|time_base=1/90000|start_time=1.483333\n" +
	"format|start_time=1.462000|duration=4.021333\n"

func TestTimelineProbeStreamingPreservesFormatOriginAndReorderedPTS(t *testing.T) {
	writer := &keyframeWriter{duration: 40_213_330}
	for _, character := range []byte(timelineProbeExample) {
		if _, err := writer.Write([]byte{character}); err != nil {
			t.Fatal(err)
		}
	}
	writer.finish()
	keys, err := writer.keyframes()
	if err != nil || !reflect.DeepEqual(keys, []int64{213_330, 20_213_330}) || writer.packets != 4 || writer.length != 0 {
		t.Fatalf("unexpected source-time keys: %v, packets=%d, err=%v", keys, writer.packets, err)
	}
}

func TestTimelineProbeRejectsUnknownOrUnsafeTimestampDomains(t *testing.T) {
	for name, input := range map[string]string{
		"no origin":          strings.Replace(timelineProbeExample, "format|start_time=1.462000|", "format|", 1),
		"unknown origin":     strings.Replace(timelineProbeExample, "start_time=1.462000", "start_time=N/A", 1),
		"duration mismatch":  strings.Replace(timelineProbeExample, "duration=4.021333", "duration=5.021333", 1),
		"missing PTS":        strings.Replace(timelineProbeExample, "pts_time=1.483333", "pts_time=N/A", 1),
		"first non-key":      strings.Replace(timelineProbeExample, "flags=K__", "flags=___", 1),
		"discarded key":      strings.Replace(timelineProbeExample, "flags=K__", "flags=KD_", 1),
		"earlier video":      strings.Replace(timelineProbeExample, "pts_time=1.525000", "pts_time=1.475000", 1),
		"negative timebase":  strings.ReplaceAll(timelineProbeExample, "time_base=1/90000", "time_base=-1/90000"),
		"wrong stream":       strings.Replace(timelineProbeExample, "stream_index=0", "stream_index=1", 1),
		"stream not video":   strings.ReplaceAll(timelineProbeExample, "codec_type=video", "codec_type=audio"),
		"key reset":          strings.Replace(timelineProbeExample, "pts_time=3.483333", "pts_time=1.483333", 1),
		"after source end":   strings.Replace(timelineProbeExample, "pts_time=3.483333", "pts_time=9.483333", 1),
		"PTS before origin":  strings.Replace(timelineProbeExample, "start_time=1.462000", "start_time=1.562000", 1),
		"stream starts late": strings.ReplaceAll(timelineProbeExample, "start_time=1.483333", "start_time=1.583333"),
		"duplicate fields":   strings.Replace(timelineProbeExample, "stream_index=0", "stream_index=0|stream_index=0", 1),
	} {
		t.Run(name, func(t *testing.T) {
			writer := &keyframeWriter{duration: 40_213_330}
			_, err := writer.Write([]byte(input))
			writer.finish()
			if err == nil {
				_, err = writer.keyframes()
			}
			if !errors.Is(err, ErrUnsupportedTimeline) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTimelineProbeLimitsAndTimestampPrecision(t *testing.T) {
	for name, writer := range map[string]*keyframeWriter{
		"bytes":   {bytes: maxTimelineProbeBytes},
		"packets": {packets: maxTimelinePackets, firstPTS: 0},
		"keys":    {packets: 1, keys: make([]int64, maxTimelineKeys)},
	} {
		t.Run(name, func(t *testing.T) {
			canceled := false
			writer.cancel = func() { canceled = true }
			_, err := writer.Write([]byte("packet|stream_index=0|pts_time=1.000000|flags=K__\n"))
			if !errors.Is(err, ErrTimelineLimit) || !canceled {
				t.Fatalf("limit=%v, canceled=%t", err, canceled)
			}
		})
	}
	writer := &keyframeWriter{}
	if _, err := writer.Write([]byte(strings.Repeat("x", maxTimelineProbeLine+1))); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("long line: %v", err)
	}
	for value, want := range map[string]int64{"0": 0, "-1.000001": -10_000_010, "0.0000001": 1, "922337203685.4775807": 9_223_372_036_854_775_807} {
		if got, valid := timelineTimestamp(value); !valid || got != want {
			t.Errorf("timestamp %q: %d, %t", value, got, valid)
		}
	}
	for _, value := range []string{"", "N/A", "+1", "1.", "1.00000001", "1e6", "922337203685.4775808", "-922337203685.4775808"} {
		if _, valid := timelineTimestamp(value); valid {
			t.Errorf("accepted invalid timestamp %q", value)
		}
	}
}

func TestKeyframesActualMP4AndTSWithIrregularGOP(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Linux timeline verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	directory := t.TempDir()
	for _, gop := range []string{"fixed", "irregular"} {
		source := filepath.Join(directory, gop+".mp4")
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-filter_threads", "1",
			"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=9",
			"-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000:duration=9",
			"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1",
			"-g", "48", "-bf", "2", "-sc_threshold", "0", "-c:a", "aac", "-threads:a", "1", "-t", "9"}
		if gop == "irregular" {
			args = append(args, "-g", "999", "-force_key_frames", "0,0.75,2.25,3.5,6.75,8")
		}
		runTimelineMediaCommand(t, ctx, ffmpeg, append(args, source)...)
		ts := filepath.Join(directory, gop+".ts")
		runTimelineMediaCommand(t, ctx, ffmpeg, "-v", "error", "-nostdin", "-i", source, "-map", "0", "-c", "copy", ts)
		for _, container := range []string{"mp4", "ts"} {
			t.Run(gop+"/"+container, func(t *testing.T) {
				file, err := os.Open(filepath.Join(directory, gop+"."+container))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).ProbeFile(ctx, file)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Seek(7, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				keys, err := Keyframes(ctx, ffprobe, file, 0, info.DurationTicks)
				if err != nil {
					t.Fatal(err)
				}
				if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 7 {
					t.Fatalf("borrowed file moved or closed: %d, %v", position, err)
				}
				want := []int64{0, 20_000_000, 40_000_000, 60_000_000, 80_000_000}
				if gop == "irregular" {
					want = []int64{0, 7_500_000, 22_500_000, 35_000_000, 67_500_000, 80_000_000}
				}
				lead := int64(0)
				if container == "ts" {
					lead = 213_330
				}
				for index := range want {
					want[index] += lead
				}
				if !reflect.DeepEqual(keys, want) {
					t.Fatalf("keyframes: %v, want %v", keys, want)
				}
				timeline, err := BuildTimeline(info.DurationTicks, 3, keys, true)
				if err != nil {
					t.Fatal(err)
				}
				var covered int64
				for _, segment := range timeline.Segments {
					if segment.StartTicks != covered {
						t.Fatal("timeline has a gap or overlap")
					}
					covered += segment.DurationTicks
					at, found := timeline.SegmentAt(segment.StartTicks)
					if !found || at.Number != segment.Number {
						t.Fatal("seek index differs at a segment boundary")
					}
					if segment.Number > 0 {
						found := false
						for _, key := range keys {
							found = found || key == segment.StartTicks
						}
						if !found {
							t.Fatal("copied video cut was not a real keyframe")
						}
					}
					// Independently seek/remux each planned segment, then decode
					// every video frame with fatal decoder-error handling.
					output := filepath.Join(directory, fmt.Sprintf("%s-%s-%d.ts", gop, container, segment.Number))
					cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-ss", tickSeconds(segment.StartTicks),
						"-i", "/proc/self/fd/3", "-t", tickSeconds(segment.DurationTicks), "-map", "0:v:0", "-an", "-c:v", "copy", output)
					cmd.ExtraFiles = []*os.File{file}
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("remux segment: %v: %s", err, output)
					}
					runTimelineMediaCommand(t, ctx, ffmpeg, "-v", "error", "-xerror", "-err_detect", "explode", "-threads", "1", "-i", output, "-map", "0:v:0", "-f", "null", "-")
				}
				if covered != info.DurationTicks {
					t.Fatalf("coverage %d differs from duration %d", covered, info.DurationTicks)
				}
				if _, err := Keyframes(ctx, ffprobe, file, 1, info.DurationTicks); !errors.Is(err, ErrUnsupportedTimeline) {
					t.Fatalf("selected audio stream: %v", err)
				}
			})
		}
	}
}

func runTimelineMediaCommand(t *testing.T, ctx context.Context, executable string, args ...string) {
	t.Helper()
	if output, err := exec.CommandContext(ctx, executable, args...).CombinedOutput(); err != nil {
		t.Fatalf("media command %s: %v: %s", strconv.Quote(args[0]), err, output)
	}
}

func TestKeyframesProcessBorrowsFileAndFiltersCredentials(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "synthetic-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-cloud-secret")
	t.Setenv("FFREPORT", "file=unexpected-report.txt")
	input := helperInput(t)
	if _, err := input.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	keys, err := Keyframes(context.Background(), timelineHelperExecutable(t, "valid"), input, 0, 40_213_330)
	if err != nil || !reflect.DeepEqual(keys, []int64{213_330, 20_213_330}) {
		t.Fatalf("helper probe: %v, %v", keys, err)
	}
	if position, err := input.Seek(0, io.SeekCurrent); err != nil || position != 5 {
		t.Fatalf("file moved or closed: %d, %v", position, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Keyframes(ctx, timelineHelperExecutable(t, "long-line"), input, 0, 40_213_330); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("unbounded child output: %v", err)
	}
	if _, err := Keyframes(ctx, timelineHelperExecutable(t, "failure"), input, 0, 40_213_330); !errors.Is(err, ErrTimelineProbe) {
		t.Fatalf("nonzero child exit: %v", err)
	}
	if _, err := Keyframes(ctx, "", input, 0, 40_213_330); !errors.Is(err, ErrStart) {
		t.Fatalf("empty executable: %v", err)
	}
	if _, err := Keyframes(ctx, "unused", nil, 0, 40_213_330); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil input: %v", err)
	}
	cancel()
	if _, err := Keyframes(ctx, "unused", input, 0, 40_213_330); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled context: %v", err)
	}
}

func TestKeyframesCancellationRetiresDescendantProcesses(t *testing.T) {
	input := helperInput(t)
	pidFile := filepath.Join(filepath.Dir(input.Name()), "timeline-child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	executable := timelineHelperExecutable(t, "parent")
	done := make(chan error, 1)
	go func() {
		_, err := Keyframes(ctx, executable, input, 0, 40_213_330)
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
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result: %v", err)
	}
	if childPID == 0 {
		t.Fatal("probe descendant did not start")
	}
	assertProcessStopped(t, childPID)
}

const timelineHelperPrefix = "goby-timeline-test-helper-"

func timelineHelperExecutable(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), timelineHelperPrefix+mode)
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func init() {
	if mode, ok := strings.CutPrefix(filepath.Base(os.Args[0]), timelineHelperPrefix); ok {
		os.Exit(runTimelineHelper(mode))
	}
}

func runTimelineHelper(mode string) int {
	switch mode {
	case "valid":
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "GOBY_") || strings.HasPrefix(entry, "AWS_") || strings.HasPrefix(entry, "FFREPORT=") {
				return 81
			}
		}
		if !hasArgumentPair(os.Args, "-i", "/proc/self/fd/3") || !hasArgumentPair(os.Args, "-select_streams", "0") ||
			!hasArgumentPair(os.Args, "-protocol_whitelist", "file,pipe") || !hasArgumentPair(os.Args, "-format_whitelist", inputFormats) {
			return 82
		}
		file, err := os.Open("/proc/self/fd/3")
		if err != nil {
			return 83
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil || string(data) != "borrowed input descriptor" {
			return 84
		}
		fmt.Fprint(os.Stdout, timelineProbeExample)
		return 0
	case "long-line":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxTimelineProbeLine+1))
		for {
			time.Sleep(time.Second)
		}
	case "failure":
		fmt.Fprint(os.Stdout, timelineProbeExample)
		return 85
	case "parent":
		child := exec.Command("/proc/self/exe")
		child.Args[0] = timelineHelperPrefix + "child"
		child.ExtraFiles = []*os.File{os.NewFile(3, "borrowed source")}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 86
		}
		for {
			time.Sleep(time.Second)
		}
	case "child":
		path, err := os.Readlink("/proc/self/fd/3")
		if err != nil || os.WriteFile(filepath.Join(filepath.Dir(path), "timeline-child.pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 87
		}
		for {
			time.Sleep(time.Second)
		}
	default:
		return 88
	}
}
