package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/creditsskipper"
)

func creditsVisualActualProbe(t *testing.T, ffmpeg string, file *os.File, info Info, videoIndex, audioIndex int) *creditsVisualProbe {
	t.Helper()
	tool, err := analysisOpenTool(context.Background(), ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tool.file.Close() })
	capabilities, err := inspectCreditsVisualTool(context.Background(), tool)
	if err != nil || !capabilities.Available || !capabilities.VisualsAvailable {
		t.Fatalf("visual inventory %+v %v", capabilities, err)
	}
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return &creditsVisualProbe{source: file, tool: tool, before: before, duration: float64(info.DurationTicks) / float64(TicksPerSecond), videoIndex: videoIndex, audioIndex: audioIndex, visuals: true, timeout: 30 * time.Second, stderrLimit: 8 << 20}
}

func TestCreditsVisualActualIndependentGrayGraphsAndProbeClocks(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "gray10.mkv")
	// This is the pinned upstream regression fixture. Converting to limited
	// yuv420p before blackframe would turn this luma into a non-black value.
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "color=c=#141414:s=64x64:r=1:d=3", "-pix_fmt", "gray10le", "-c:v", "ffv1", "-threads:v", "1", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	probe := creditsVisualActualProbe(t, ffmpeg, file, info, 0, -1)
	if _, err := file.Seek(19, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	frames, err := probe.ScanKeyframes(context.Background(), creditsskipper.Range{Start: 0, End: 3}, 28)
	if err != nil || len(frames.BlackFrames) != 3 || len(frames.Visuals) != 3 {
		t.Fatalf("gray evidence %+v %v", frames, err)
	}
	for _, frame := range frames.BlackFrames {
		if frame.Percentage != 100 {
			t.Fatalf("gray luma negotiated through visuals graph: %+v", frame)
		}
	}
	// An independent pinned-upstream blackframe-only command establishes
	// both the exact source times and the raw gray-luma result. It does not
	// call creditsKeyframeArgs or add the entropy graph under examination.
	control, err := runLimitedFilesOutput(context.Background(), 30*time.Second, 1, ffmpeg, []*os.File{file},
		"-hide_banner", "-nostdin", "-skip_frame", "nokey", "-ss", "0", "-i", "/proc/self/fd/3",
		"-an", "-dn", "-sn", "-vf", "blackframe=amount=0:threshold=28", "-f", "null", "-")
	if err != nil {
		t.Fatal(err)
	}
	nativeFrames, err := parseCreditsBlackFrames(string(control.stderr))
	if err != nil || len(nativeFrames) != 3 || !reflect.DeepEqual(frames.BlackFrames, nativeFrames) {
		t.Fatalf("dual graphs differ from native gray blackframe control: actual=%+v native=%+v error=%v", frames.BlackFrames, nativeFrames, err)
	}
	probe.visuals = false
	alone, err := probe.ScanKeyframes(context.Background(), creditsskipper.Range{Start: 0, End: 3}, 28)
	if err != nil || !reflect.DeepEqual(frames.BlackFrames, alone.BlackFrames) || len(alone.Visuals) != 0 {
		t.Fatalf("optional visuals changed blackframe: %+v %v", alone, err)
	}
	boundary, err := probe.ScanBoundary(context.Background(), creditsskipper.Range{Start: 1, End: 2}, 28, 85)
	if err != nil || len(boundary) == 0 || boundary[0].Time != 0 {
		t.Fatalf("boundary times are not relative: %+v %v", boundary, err)
	}
	keys, err := probe.ScanKeyframesAtBoundary(context.Background(), creditsskipper.Range{Start: 1, End: 2})
	if err != nil || len(keys) == 0 || keys[0] != 1 {
		t.Fatalf("adjustment keyframes are not absolute: %v %v", keys, err)
	}
	if silence, err := probe.ScanSilence(context.Background(), creditsskipper.Range{Start: 1, End: 2}); err != nil || len(silence) != 0 {
		t.Fatalf("known absence of audio became failure: %v %v", silence, err)
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 19 {
		t.Fatalf("borrowed offset changed: %d %v", position, err)
	}
}

func TestCreditsVisualActualIsolatedMovieAndEpisodePass(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "visual-ending.mkv")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=s=64x64:r=2:d=10", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=2:d=25", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-c:v", "ffv1", "-threads:v", "1", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	for _, isMovie := range []bool{true, false} {
		result, err := (AnalysisExtractor{FFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: time.Minute}}).ExtractCreditsVisual(context.Background(), file, info, CreditsVisualRequest{VideoStreamIndex: 0, AudioStreamIndex: -1, IsMovie: isMovie})
		if err != nil {
			t.Fatal(err)
		}
		if result.SourceIdentity == "" || result.FFmpegSHA256 == "" || !result.VisualsAvailable || len(result.Result.Segments) == 0 || result.Result.Evidence.BlackFrameCount == 0 {
			t.Fatalf("isolated visual pass missing evidence: %+v", result)
		}
		for _, segment := range result.Result.Segments {
			if segment.Start < 8 || segment.Start > 12 || segment.End != 35 {
				t.Fatalf("unexpected generated ending interval: %+v", segment)
			}
		}
	}
}

func TestCreditsVisualActualSilenceUsesAbsoluteRangeClock(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "silence.wav")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=mono", "-t", "3", "-c:a", "pcm_s16le", "-threads:a", "1", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	probe := creditsVisualActualProbe(t, ffmpeg, file, info, 0, 0)
	ranges, err := probe.ScanSilence(context.Background(), creditsskipper.Range{Start: 1, End: 2})
	if err != nil {
		t.Fatal(err)
	}
	// This independent command is the pinned FFmpegService silence recipe.
	// Its output -to does not trim audio already consumed by silencedetect;
	// the ending event can extend beyond the requested range by decoded frames.
	// Upstream preserves that event and adds range.Start without clipping it.
	control, err := runLimitedFilesOutput(context.Background(), 30*time.Second, 1, ffmpeg, []*os.File{file},
		"-hide_banner", "-nostdin", "-vn", "-sn", "-dn", "-ss", "1", "-i", "/proc/self/fd/3", "-to", "1",
		"-af", "silencedetect=noise=-50dB:duration=0.1", "-f", "null", "-")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := parseCreditsSilence(string(control.stderr), 1)
	if err != nil || len(expected) != 1 || expected[0].Start != 1 || expected[0].End < 2 || expected[0].End > 3 {
		t.Fatalf("invalid native silence control: %+v %v", expected, err)
	}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("silence differs from native file clock: actual=%+v native=%+v", ranges, expected)
	}
	t.Logf("credits-silence-parity requested=[1,2] native=[%.17g,%.17g]", ranges[0].Start, ranges[0].End)
}
