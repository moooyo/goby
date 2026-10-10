package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type previewBuildTestCost struct {
	at             time.Time
	self, children syscall.Rusage
}

func previewBuildTestMeasure(t *testing.T) previewBuildTestCost {
	t.Helper()
	value := previewBuildTestCost{at: time.Now()}
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &value.self); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &value.children); err != nil {
		t.Fatal(err)
	}
	return value
}

func previewBuildTestReport(t *testing.T, mode string, before, after previewBuildTestCost) {
	t.Helper()
	cpu := func(value syscall.Rusage) time.Duration {
		return time.Duration(value.Utime.Nano() + value.Stime.Nano())
	}
	t.Logf("%s elapsed=%s self_cpu=%s child_cpu=%s child_inblocks=%d child_outblocks=%d", mode, after.at.Sub(before.at),
		cpu(after.self)-cpu(before.self), cpu(after.children)-cpu(before.children),
		after.children.Inblock-before.children.Inblock, after.children.Oublock-before.children.Oublock)
}

func previewBuildTestProbe(t *testing.T, ffprobe, path string) (*os.File, Info, int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			return file, info, stream.Index
		}
	}
	t.Fatal("fixture has no timed video")
	return nil, Info{}, -1
}

// Each comparison runs both modes on the same descriptor and compares every
// JPEG byte and timeline fact. Costs exclude fixture creation and initial
// probing, but include the current executable/geometry checks in both modes.
// Block I/O is the kernel's physical block count, not logical bytes read.
func TestAnalysisPreviewBuildActualMatchesIndependentPasses(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, fixture := range []struct {
		name, video, audio string
		frames             int
		start, rotate      bool
	}{
		{name: "short_cfr", video: "testsrc2=size=160x90:rate=10,trim=end_frame=30", frames: 30},
		{name: "long_cfr", video: "testsrc2=size=1280x720:rate=25,trim=end_frame=1500", frames: 1500},
		{name: "vfr_first_and_last_hold", frames: 3,
			video: "testsrc2=size=96x64:rate=25,trim=end_frame=3,settb=expr=1/1000,setpts='if(eq(N,0),1000,if(eq(N,1),4000,8000))'",
			audio: "anullsrc=r=48000:cl=mono,atrim=end_sample=436800"},
		{name: "nonzero_start", video: "testsrc2=size=160x90:rate=10,trim=end_frame=30", frames: 30, start: true},
		{name: "rotation_sar", video: "testsrc2=size=96x64:rate=10,trim=end_frame=30,setsar=16/15", frames: 30, rotate: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.mp4")
			args := []string{"-f", "lavfi", "-i", fixture.video}
			if fixture.audio != "" {
				path = strings.TrimSuffix(path, ".mp4") + ".mkv"
				args = append(args, "-f", "lavfi", "-i", fixture.audio, "-map", "0:v:0", "-map", "1:a:0", "-c:a", "pcm_s16le")
			}
			args = append(args, "-copyts", "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
				"-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "25", "-pix_fmt", "yuv420p")
			if fixture.start {
				args = append(args, "-output_ts_offset", "1.25")
			}
			audioProbeRunFFmpeg(t, ffmpeg, append(args, path)...)
			if fixture.rotate {
				rotated := filepath.Join(filepath.Dir(path), "rotated.mp4")
				audioProbeRunFFmpeg(t, ffmpeg, "-display_rotation:v:0", "-90", "-noautorotate", "-i", path, "-map", "0:v:0", "-c", "copy", rotated)
				path = rotated
			}
			file, info, streamIndex := previewBuildTestProbe(t, ffprobe, path)
			if fixture.start && info.FormatStartTicks != 12_500_000 {
				t.Fatalf("fixture lost its nonzero origin: %+v", info)
			}
			if _, err := file.Seek(17, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			// A shared build must admit sources exactly at the original
			// two-pass frame limit, even though it performs four real passes.
			extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Limits: AnalysisLimits{MaxSourceFrames: 2 * fixture.frames}}
			widths := []int{240, 320, 400}
			frames := make([][]PreviewFrame, len(widths))
			summaries := make([]PreviewAnalysisSummary, len(widths))
			independentStart := previewBuildTestMeasure(t)
			for index, width := range widths {
				var err error
				summaries[index], err = extractor.ExtractPreviews(context.Background(), file, info, streamIndex,
					PreviewAnalysisOptions{Width: width, IntervalTicks: TicksPerSecond}, func(frame PreviewFrame) error {
						frame.JPEG = bytes.Clone(frame.JPEG)
						frames[index] = append(frames[index], frame)
						return nil
					})
				if err != nil {
					t.Fatal(err)
				}
			}
			independentEnd := previewBuildTestMeasure(t)
			state := &analysisPreviewBuild{}
			sharedStart := previewBuildTestMeasure(t)
			err := state.run(context.Background(), extractor, file, info, streamIndex, func(extract PreviewAnalysisExtract) error {
				for index, width := range widths {
					count := 0
					summary, err := extract(PreviewAnalysisOptions{Width: width, IntervalTicks: TicksPerSecond}, func(frame PreviewFrame) error {
						if count >= len(frames[index]) || !reflect.DeepEqual(frame, frames[index][count]) {
							return fmt.Errorf("width %d frame %d differs from its independent extraction", width, count)
						}
						count++
						return nil
					})
					if err != nil {
						return err
					}
					if summary != summaries[index] || count != len(frames[index]) {
						return fmt.Errorf("width %d summary or frame count changed", width)
					}
					// This is where the server assembles a BIF and deletes JPEG
					// scratch. Retaining only source proof must not hold a slot.
					if len(analysisSlots) != 0 {
						return fmt.Errorf("preview build retained admission between widths")
					}
				}
				return nil
			})
			sharedEnd := previewBuildTestMeasure(t)
			if err != nil {
				t.Fatal(err)
			}
			if state.source.frames != fixture.frames || state.actual.frames != 4*fixture.frames || state.actual.packets != 4*state.source.packets || state.proof != nil {
				t.Fatalf("source proof lifetime or actual pass accounting changed: source=%+v actual=%+v proof=%v", state.source, state.actual, state.proof != nil)
			}
			if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
				t.Fatalf("build changed the source descriptor offset: %d, %v", offset, err)
			}
			t.Logf("audited source frames: independent=%d shared=%d; full preview decodes: 6 -> 4", 6*fixture.frames, state.actual.frames)
			previewBuildTestReport(t, "independent", independentStart, independentEnd)
			previewBuildTestReport(t, "shared", sharedStart, sharedEnd)
		})
	}
}

func previewBuildTestTool(t *testing.T, executable, marker, action string) string {
	t.Helper()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	path := filepath.Join(t.TempDir(), "ffmpeg")
	content := "#!/bin/sh\nif [ -e " + quote(marker) + " ]; then " + action + "; fi\nexec " + quote(executable) + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAnalysisPreviewBuildActualRejectsLaterFailures(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fixture := filepath.Join(t.TempDir(), "source.mp4")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=96x64:rate=5", "-t", "2", "-an",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "1", "-pix_fmt", "yuv420p", fixture)
	fixtureBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"source_changed", "tool_changed", "capability_failed", "later_callback", "later_cancel", "variant_timeout", "build_deadline", "duplicate_width", "changed_interval"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.mp4")
			if err := os.WriteFile(path, fixtureBytes, 0600); err != nil {
				t.Fatal(err)
			}
			file, info, streamIndex := previewBuildTestProbe(t, ffprobe, path)
			marker := filepath.Join(t.TempDir(), "changed")
			action := "exit 73"
			if failure == "variant_timeout" || failure == "build_deadline" {
				action = "sleep 10"
			}
			tool := previewBuildTestTool(t, ffmpeg, marker, action)
			extractor := AnalysisExtractor{FFmpegPath: tool, FFprobePath: ffprobe, Limits: AnalysisLimits{Timeout: 3 * time.Second}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "build_deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 2*time.Second)
				defer stop()
			}
			state := &analysisPreviewBuild{}
			callbackFailure := errors.New("staging failed")
			var escaped PreviewAnalysisExtract
			var laterErr error
			err := state.run(ctx, extractor, file, info, streamIndex, func(extract PreviewAnalysisExtract) error {
				escaped = extract
				if _, err := extract(PreviewAnalysisOptions{Width: 240, IntervalTicks: TicksPerSecond}, func(PreviewFrame) error { return nil }); err != nil {
					return fmt.Errorf("initial extraction: %w", err)
				}
				options := PreviewAnalysisOptions{Width: 320, IntervalTicks: TicksPerSecond}
				switch failure {
				case "source_changed":
					if err := os.WriteFile(path, fixtureBytes[:len(fixtureBytes)/2], 0600); err != nil {
						return err
					}
				case "tool_changed":
					if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
						return err
					}
				case "capability_failed", "variant_timeout", "build_deadline":
					if err := os.WriteFile(marker, nil, 0600); err != nil {
						return err
					}
				case "duplicate_width":
					options.Width = 240
				case "changed_interval":
					options.IntervalTicks = 2 * TicksPerSecond
				}
				callbacks := 0
				summary, err := extract(options, func(PreviewFrame) error {
					callbacks++
					if failure == "later_cancel" {
						cancel()
						return nil
					}
					if failure == "later_callback" {
						return callbackFailure
					}
					return nil
				})
				laterErr = err
				if err == nil || summary != (PreviewAnalysisSummary{}) {
					return fmt.Errorf("failed later variant produced a successful summary: %+v, %v", summary, err)
				}
				if failure != "later_cancel" && failure != "later_callback" && callbacks != 0 {
					return fmt.Errorf("changed source/tool/options emitted %d frames", callbacks)
				}
				if _, nextErr := extract(PreviewAnalysisOptions{Width: 400}, func(PreviewFrame) error { return nil }); nextErr == nil {
					return fmt.Errorf("failed build allowed another variant")
				}
				// A caller cannot hide a failed extraction by returning nil.
				return nil
			})
			if err == nil || laterErr == nil || state.proof != nil || len(analysisSlots) != 0 {
				t.Fatalf("failed build retained proof/admission or hid its failure: %v, later=%v", err, laterErr)
			}
			if failure == "later_callback" && !errors.Is(err, callbackFailure) || failure == "later_cancel" && !errors.Is(err, context.Canceled) ||
				(failure == "variant_timeout" || failure == "build_deadline") && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failure lost its cause: %v", err)
			}
			if _, err := escaped(PreviewAnalysisOptions{Width: 400}, func(PreviewFrame) error { return nil }); err == nil {
				t.Fatal("escaped extraction retained its build proof")
			}
		})
	}
}
