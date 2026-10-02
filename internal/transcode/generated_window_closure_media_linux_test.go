//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// These actual-media tests combine independent source-frame duration, encoder
// association and complete output evidence. A retained interval proves its
// requested coverage; no interval probe establishes terminal source EOF.
func TestGeneratedWindowActualClosurePrimitiveEvidence(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	source := generatedClosureMediaSource(t, ctx, ffmpeg, 24, 100, 90, 93)
	generatedClosureMediaAssertOrigin(t, ctx, ffprobe, source)
	for _, fixture := range []struct {
		name, format string
		adaptive     bool
	}{
		{name: "high-seek TS", format: "mpegts"},
		{name: "high-seek fMP4", format: "fmp4"},
		{name: "high-seek two fMP4 renditions", format: "fmp4", adaptive: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			plan := generatedClosureMediaPlan(fixture.format, 100, 90, 96, fixture.adaptive)
			sourceFile, before := generatedClosureMediaOpenSource(t, source)
			directory, result, clocks := generatedClosureMediaRun(t, ctx, ffmpeg, sourceFile, plan)
			if result.WindowInputEvidence == nil || result.ExitCode != 0 {
				t.Fatalf("normally completed window omitted input evidence: %+v", result)
			}
			coverage, err := MeasureGeneratedSourceRange(ctx, ffprobe, sourceFile, plan, 2*ticksPerSecond)
			if err != nil || coverage.FrameCount != 144 {
				t.Fatalf("actual source frame durations did not prove the full requested range: %+v, %v", coverage, err)
			}
			var boundsArray [MaxHLSRenditions]GeneratedSegmentBounds
			var listsArray [MaxHLSRenditions]MediaPlaylist
			for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
				input := result.WindowInputEvidence[index]
				if err := ValidateGeneratedInputEvidence(plan, input); err != nil || input.Rendition != index || input.Frames != 144 ||
					!input.SourceSequential || !input.InputCadenceAligned || !input.InputCadenceExact || input.FirstEncoderPTS != 0 {
					t.Fatalf("actual source/encoder association = %+v, %v", input, err)
				}
				clock := clocks[index]
				if clock.Rendition != index || clock.TimeBaseNumerator <= 0 || clock.TimeBaseDenominator <= 0 || clock.PTS != 0 {
					t.Fatalf("actual first pre-container packet clock = %+v", clock)
				}
				list := generatedWindowReadList(t, directory, HLSPlaylistName(index, plan.HLS.RenditionCount), plan.HLS.Window.StartNumber)
				if len(list.Segments) != 1 || list.Segments[0].DurationTicks != 6*ticksPerSecond {
					t.Fatalf("normal EOF did not close the single six-second output slot: %+v", list)
				}
				bounds := generatedClosureMediaMeasure(t, ctx, ffprobe, directory, plan, list)
				boundsArray[index], listsArray[index] = bounds, list
				generatedClosureMediaAssertOutputCadence(t, bounds, 144)
				combined := generatedWindowCombine(t, directory, list, "closed."+plan.Container)
				width, height := plan.Width, plan.Height
				if plan.HLS.RenditionCount != 0 {
					width, height = plan.HLS.Renditions[index].Width, plan.HLS.Renditions[index].Height
				}
				generatedClosureMediaAssertWholeDecode(t, ctx, ffmpeg, combined, width, height)
				t.Logf("actual primitive evidence %s rendition %d: input=%+v mux=%+v output=%+v", fixture.format, index, input, clock, bounds.Video)
			}
			if !transcodeSourceUnchanged(sourceFile, before) {
				t.Fatal("source changed across production, source range and output proof")
			}
			closure, err := ValidateGeneratedWindowClosure(plan, coverage, *result.WindowInputEvidence, clocks, boundsArray, listsArray)
			if err != nil || closure.StartTicks != 90*ticksPerSecond || closure.EndTicks != 96*ticksPerSecond ||
				closure.Number != plan.HLS.Window.StartNumber || closure.RenditionCount != max(1, plan.HLS.RenditionCount) {
				t.Fatalf("actual source and output evidence failed complete interval closure: %+v, %v", closure, err)
			}
		})
	}
}

func TestGeneratedWindowActualInputEvidenceDetectsSourceReplication(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	source := generatedClosureMediaSource(t, ctx, ffmpeg, 12, 8, 0, 3)
	for _, format := range []string{"mpegts", "fmp4"} {
		t.Run(format, func(t *testing.T) {
			plan := generatedClosureMediaPlan(format, 8, 0, 6, false)
			sourceFile, before := generatedClosureMediaOpenSource(t, source)
			directory, result, clocks := generatedClosureMediaRun(t, ctx, ffmpeg, sourceFile, plan)
			if result.WindowInputEvidence == nil {
				t.Fatal("normal replicated output omitted the actual input facts")
			}
			input := result.WindowInputEvidence[0]
			if err := ValidateGeneratedInputEvidence(plan, input); err != nil || input.SourceSequential || input.InputCadenceAligned || input.InputCadenceExact || input.Frames != 144 {
				t.Fatalf("12-to-24 fps replication was hidden by valid output endpoints: %+v, %v", input, err)
			}
			list := generatedWindowReadList(t, directory, "main.m3u8", plan.HLS.Window.StartNumber)
			bounds := generatedClosureMediaMeasure(t, ctx, ffprobe, directory, plan, list)
			generatedClosureMediaAssertOutputCadence(t, bounds, 144)
			coverage, sourceErr := MeasureGeneratedSourceRange(ctx, ffprobe, sourceFile, plan, 2*ticksPerSecond)
			if sourceErr == nil {
				t.Fatalf("12-fps source duration was incorrectly certified as 24-fps coverage: %+v", coverage)
			}
			if !transcodeSourceUnchanged(sourceFile, before) {
				t.Fatal("replication fixture source changed across the proof sequence")
			}
			var boundsArray [MaxHLSRenditions]GeneratedSegmentBounds
			var listsArray [MaxHLSRenditions]MediaPlaylist
			boundsArray[0], listsArray[0] = bounds, list
			if _, err := ValidateGeneratedWindowClosure(plan, coverage, *result.WindowInputEvidence, clocks, boundsArray, listsArray); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("normal replicated output incorrectly retained a source interval: %v", err)
			}
			// A complete 24-fps output cannot turn duplicated decoded source
			// frames into a sequential input-coverage fact.
			t.Logf("complete output retained the nonsequential source fact: input=%+v output=%+v", input, bounds.Video)
		})
	}
}

func generatedClosureMediaSource(t *testing.T, ctx context.Context, ffmpeg string, fps, seconds, firstMarker, secondMarker int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mp4")
	filter := fmt.Sprintf("color=c=red:s=160x96:r=%d:d=%d,drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,%d)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,%d)'", fps, seconds, firstMarker, secondMarker)
	generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", filter,
		"-map", "0:v:0", "-an", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", fmt.Sprint(fps),
		"-preset", "ultrafast", "-crf", "0", "-pix_fmt", "yuv420p", "-output_ts_offset", "2", "-movflags", "+faststart", path)
	return path
}

func generatedClosureMediaAssertOrigin(t *testing.T, ctx context.Context, ffprobe, source string) {
	t.Helper()
	data := generatedWindowMediaCommand(t, ctx, ffprobe, "-v", "error", "-show_entries", "format=start_time", "-of", "json", source)
	var facts map[string]map[string]string
	if err := json.Unmarshal(data, &facts); err != nil {
		t.Fatal(err)
	}
	origin, valid := new(big.Rat).SetString(facts["format"]["start_time"])
	if !valid || origin.Cmp(new(big.Rat).SetInt64(2)) != 0 {
		t.Fatalf("source fixture lost its separate two-second format origin: %s", data)
	}
}

func generatedClosureMediaPlan(format string, duration, start, end int64, adaptive bool) Plan {
	plan := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		Width: 160, Height: 96, FrameRate: 24, VideoBitrate: 256000,
		DurationTicks: duration * ticksPerSecond, StartTicks: start * ticksPerSecond, SegmentSeconds: 6,
		HLS: HLSPlan{SegmentType: format, Window: HLSWindow{EndTicks: end * ticksPerSecond, StartNumber: 12000, RequireInputEvidence: true}}}
	if format == "fmp4" {
		plan.Container = "mp4"
	}
	if adaptive {
		plan.HLS.RenditionCount = 2
		plan.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 96, VideoBitrate: 256000}
		plan.HLS.Renditions[1] = HLSRendition{Width: 80, Height: 48, VideoBitrate: 96000}
	}
	return plan
}

func generatedClosureMediaOpenSource(t *testing.T, path string) (*os.File, os.FileInfo) {
	t.Helper()
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return source, before
}

func generatedClosureMediaRun(t *testing.T, ctx context.Context, ffmpeg string, source *os.File, plan Plan) (string, RunResult, [MaxHLSRenditions]HLSMuxClock) {
	t.Helper()
	var clocks [MaxHLSRenditions]HLSMuxClock
	var seen [MaxHLSRenditions]bool
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("actual generated window fixture has an invalid plan: %v", err)
	}
	directory := t.TempDir()
	var mu sync.Mutex
	result, err := Run(ctx, ffmpeg, directory, source, plan, 1, func(progress Progress) {
		if progress.HLSClock != nil {
			index := progress.HLSClock.Rendition
			if index >= 0 && index < len(clocks) {
				mu.Lock()
				clocks[index], seen[index] = *progress.HLSClock, true
				mu.Unlock()
			}
		}
	})
	if err != nil {
		t.Fatalf("actual finite producer did not finish normally: %v: %s", err, result.StderrTail)
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("production moved or closed the shared source descriptor: %d, %v", offset, err)
	}
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		if !seen[index] {
			t.Fatalf("normal output omitted rendition %d mux-clock observation", index)
		}
	}
	return directory, result, clocks
}

func generatedClosureMediaMeasure(t *testing.T, ctx context.Context, ffprobe, directory string, plan Plan, list MediaPlaylist) GeneratedSegmentBounds {
	t.Helper()
	if !list.Ended || len(list.Segments) != 1 {
		t.Fatalf("complete primitive proof requires one closed segment: %+v", list)
	}
	var initialization *os.File
	if list.InitName != "" {
		var err error
		initialization, err = os.Open(filepath.Join(directory, list.InitName))
		if err != nil {
			t.Fatal(err)
		}
		defer initialization.Close()
		if _, err := initialization.Seek(3, io.SeekStart); err != nil {
			t.Fatal(err)
		}
	}
	media, err := os.Open(filepath.Join(directory, list.Segments[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	if _, err := media.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	bounds, err := MeasureGeneratedWindowSegment(ctx, ffprobe, plan, initialization, media)
	if err != nil {
		t.Fatalf("complete retained output primitive checks failed: %v", err)
	}
	if offset, err := media.Seek(0, io.SeekCurrent); err != nil || offset != 5 {
		t.Fatalf("combined proof moved or closed borrowed media: %d, %v", offset, err)
	}
	if initialization != nil {
		if offset, err := initialization.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
			t.Fatalf("combined proof moved or closed borrowed initialization: %d, %v", offset, err)
		}
		if bounds.InitializationSHA256 == ([32]byte{}) {
			t.Fatal("fragmented output omitted the initialization-byte binding")
		}
	}
	return bounds
}

func generatedClosureMediaAssertOutputCadence(t *testing.T, bounds GeneratedSegmentBounds, frames int64) {
	t.Helper()
	video := bounds.Video
	if !video.Present || bounds.Audio.Present || video.PacketCount != frames || !video.FirstKey || !video.PresentationDecodeAligned ||
		video.MinPacketDuration <= 0 || video.MinPacketDuration != video.MaxPacketDuration || video.TotalDecodeGapTicks != 0 ||
		video.TotalPresentationGapTicks != 0 || video.TimeBase.Num <= 0 || video.TimeBase.Den <= 0 || bounds.SegmentSHA256 == ([32]byte{}) {
		t.Fatalf("complete output did not retain exact per-packet cadence: %+v", bounds)
	}
	step := generatedClockSeconds(video.MinPacketDuration, video.TimeBase.Num, video.TimeBase.Den)
	span := generatedClockSeconds(video.EndPTS-video.FirstPTS, video.TimeBase.Num, video.TimeBase.Den)
	if step.Cmp(new(big.Rat).SetFrac64(1, 24)) != 0 || span.Cmp(new(big.Rat).SetFrac64(frames, 24)) != 0 {
		t.Fatalf("actual output clock has the wrong native step/span: step=%s span=%s", step.RatString(), span.RatString())
	}
}

func generatedClosureMediaAssertWholeDecode(t *testing.T, ctx context.Context, ffmpeg, path string, width, height int) {
	t.Helper()
	data := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", path,
		"-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	frameSize := width * height * 3
	if frameSize <= 0 || len(data) != 144*frameSize {
		t.Fatalf("independent complete decode did not contain exactly 144 frames: bytes=%d dimensions=%dx%d", len(data), width, height)
	}
	for index := 0; index < 144; index++ {
		frame := data[index*frameSize : (index+1)*frameSize]
		var channels [3]int64
		for pixel := 0; pixel < len(frame); pixel += 3 {
			channels[0] += int64(frame[pixel])
			channels[1] += int64(frame[pixel+1])
			channels[2] += int64(frame[pixel+2])
		}
		pixels := int64(width * height)
		red, green, blue := channels[0]/pixels, channels[1]/pixels, channels[2]/pixels
		if index < 72 {
			if green < 170 || red > 60 || blue > 60 {
				t.Fatalf("frame %d selected content outside source [90,93): RGB=%d,%d,%d", index, red, green, blue)
			}
		} else if blue < 170 || red > 60 || green > 60 {
			t.Fatalf("frame %d selected content outside source [93,96): RGB=%d,%d,%d", index, red, green, blue)
		}
	}
}
