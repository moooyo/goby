//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveRestartHelperPrefix = "goby-live-restart-test-helper-"

func liveRestartHelperExecutable(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), liveRestartHelperPrefix+mode)
	if err := os.Symlink(executable, name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestValidateLiveVideoRestartBorrowsInputAndBoundsProbe(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "synthetic-secret")
	t.Setenv("FFREPORT", "file=unexpected-report.txt")
	for _, test := range []struct {
		mode string
		want error
	}{
		{"valid", nil}, {"non-idr", ErrTimelineProbe}, {"diagnostic", ErrTimelineProbe}, {"overflow", ErrTimelineLimit},
	} {
		t.Run(test.mode, func(t *testing.T) {
			input := hlsClockTestFile(t, []byte("owned transport bytes"))
			if _, err := input.Seek(7, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := ValidateLiveVideoRestart(ctx, liveRestartHelperExecutable(t, test.mode), nil, input, "h264")
			if !errors.Is(err, test.want) {
				t.Fatalf("restart probe = %v, want %v", err, test.want)
			}
			hlsClockAssertOffset(t, input, 7)
		})
	}
}

func TestValidateLiveVideoRestartCancellationAndInvalidInputs(t *testing.T) {
	input := hlsClockTestFile(t, []byte("owned transport bytes"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := ValidateLiveVideoRestart(ctx, liveRestartHelperExecutable(t, "wait"), nil, input, "h264"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe cancellation = %v", err)
	}
	for _, kind := range []string{"nil", "closed", "directory", "pipe", "empty"} {
		if err := ValidateLiveVideoRestart(context.Background(), "unused", nil, hlsClockInvalidFile(t, kind), "h264"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid %s input = %v", kind, err)
		}
	}
	if err := ValidateLiveVideoRestart(context.Background(), "unused", nil, input, "vp9"); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("unknown codec reached the probe")
	}
}

func TestValidateLiveVideoRestartActualClosedSegments(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12:duration=2",
		"-an", "-c:v", "libx264", "-threads:v", "1", "-pix_fmt", "yuv420p", source)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		for _, fragmented := range []bool{false, true} {
			if codec == "av1" && !fragmented {
				continue
			}
			t.Run(fmt.Sprintf("%s/fmp4=%t", codec, fragmented), func(t *testing.T) {
				plan := commandPlan()
				plan.VideoCodec, plan.AudioCodec, plan.AudioStreamIndex = codec, "", -1
				plan.AudioBitrate, plan.AudioChannels, plan.AudioSampleRate = 0, 0, 0
				plan.DurationTicks, plan.SegmentSeconds, plan.FrameRate = 2*ticksPerSecond, 1, 12
				plan.HLS.SegmentType = "mpegts"
				if fragmented {
					plan.Container, plan.HLS.SegmentType = "mp4", "fmp4"
				}
				input, err := os.Open(source)
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				directory := t.TempDir()
				result, err := Run(ctx, ffmpeg, directory, input, plan, 1, nil)
				if err != nil {
					t.Fatalf("generate independent segments: %v: %s", err, result.StderrTail)
				}
				data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
				if err != nil {
					t.Fatal(err)
				}
				list, err := ParseMediaPlaylist(data)
				if err != nil || len(list.Segments) != 2 {
					t.Fatalf("segment fixture: %v", err)
				}
				var init *os.File
				if fragmented {
					init, err = os.Open(filepath.Join(directory, list.InitName))
					if err != nil {
						t.Fatal(err)
					}
					defer init.Close()
				}
				for _, item := range list.Segments {
					segment, err := os.Open(filepath.Join(directory, item.Name))
					if err != nil {
						t.Fatal(err)
					}
					if _, err := segment.Seek(5, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					if init != nil {
						if _, err := init.Seek(3, io.SeekStart); err != nil {
							t.Fatal(err)
						}
					}
					err = ValidateLiveVideoRestart(ctx, ffprobe, init, segment, codec)
					hlsClockAssertOffset(t, segment, 5)
					if init != nil {
						hlsClockAssertOffset(t, init, 3)
					}
					_ = segment.Close()
					if err != nil {
						t.Fatalf("actual independent %s segment rejected: %v", codec, err)
					}
				}
			})
		}
	}
}

func init() {
	if mode, ok := strings.CutPrefix(filepath.Base(os.Args[0]), liveRestartHelperPrefix); ok {
		os.Exit(runLiveRestartProbeHelper(mode))
	}
}

func runLiveRestartProbeHelper(mode string) int {
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOBY_") || strings.HasPrefix(entry, "FFREPORT=") {
			return 80
		}
	}
	for _, pair := range [][2]string{{"-protocol_whitelist", "pipe"}, {"-format_whitelist", "mpegts"}, {"-read_intervals", "%+#1"}, {"-select_streams", "v:0"}, {"-i", "pipe:0"}} {
		if !hasArgumentPair(os.Args[1:], pair[0], pair[1]) {
			return 81
		}
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil || string(input) != "owned transport bytes" {
		return 82
	}
	if mode == "wait" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "overflow" {
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", maxLiveRestartProbeBytes+1))
		return 0
	}
	if mode == "diagnostic" {
		_, _ = io.WriteString(os.Stderr, "decoder reported corrupt data\n")
	}
	packet := liveRestartH264Packet()
	if mode == "non-idr" {
		packet = liveRestartAnnexBFixture(videoReadySPS(), videoReadyPPS(), []byte{0x61, 0x88, 0x84, 0x80})
	}
	data, _ := json.Marshal(liveRestartProbeFixture(packet))
	_, _ = os.Stdout.Write(data)
	return 0
}
