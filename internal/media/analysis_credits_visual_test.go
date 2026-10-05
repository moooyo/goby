package media

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/creditsskipper"
)

func TestCreditsVisualArgumentsKeepIndependentGraphsAndUpstreamSeekSemantics(t *testing.T) {
	window := creditsskipper.Range{Start: 1000.125, End: 1450.125}
	args := creditsKeyframeArgs(3, window, 28, true)
	var filters []string
	for index, arg := range args {
		if arg == "-vf" {
			filters = append(filters, args[index+1])
		}
	}
	if !reflect.DeepEqual(filters, []string{"blackframe=amount=0:threshold=28", "format=yuv420p,entropy,signalstats,metadata=print"}) {
		t.Fatalf("gray blackframe graph was merged: %v", filters)
	}
	joined := " " + strings.Join(args, " ") + " "
	if strings.Contains(joined, " -to ") || strings.Count(joined, " -map 0:3 ") != 2 || !strings.Contains(joined, " -skip_frame nokey -ss 1000.125 -i /proc/self/fd/3 ") {
		t.Fatal("whole-tail input or outputs changed")
	}
	args = creditsKeyframeArgs(3, window, 28, false)
	if strings.Contains(strings.Join(args, " "), "entropy") {
		t.Fatal("unavailable optional visuals started")
	}
	window = creditsskipper.Range{Start: 12.25, End: 15.375}
	joined = " " + strings.Join(creditsBlackIntervalArgs(3, window, 28, 85), " ") + " "
	for _, fragment := range []string{" -ss 12.25 -skip_frame noref -i /proc/self/fd/3 -to 3.125 ", "blackdetect=d=0.1:pix_th=0.0548:pic_th=0.85"} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("native blackdetect recipe changed: %s", joined)
		}
	}
	if creditsVisualFourDecimals(0) != "0" || creditsVisualFourDecimals(1) != "1" {
		t.Fatal("native ratio formatting changed")
	}
	joined = " " + strings.Join(creditsBoundaryArgs(3, window, 28), " ") + " "
	if strings.Contains(joined, "skip_frame") || !strings.Contains(joined, "blackframe=amount=50:threshold=28") {
		t.Fatal("boundary probe lost full-frame sampling")
	}
	joined = " " + strings.Join(creditsSilenceArgs(5, window), " ") + " "
	if !strings.Contains(joined, " -vn -sn -dn -ss 12.25 ") || !strings.Contains(joined, " -map 0:5 -af silencedetect=noise=-50dB:duration=0.1 ") {
		t.Fatal("silence recipe or selected stream changed")
	}
	joined = " " + strings.Join(creditsBoundaryKeyframeArgs(3, window), " ") + " "
	if !strings.Contains(joined, " -skip_frame nokey -ss 12.25 ") || !strings.Contains(joined, " -to 3.125 ") || !strings.Contains(joined, " -vf showinfo ") {
		t.Fatal("keyframe adjustment recipe changed")
	}
}

func TestCreditsVisualParsersKeepRawRelativeAndAbsoluteClocks(t *testing.T) {
	black := `[Parsed_blackframe_4 @ 0x123] frame:2 pblack:99 pts:43 t:0.043000 type:B last_keyframe:0
[Parsed_blackframe_4 @ 0x123] frame:3 pblack:85 pts:1250 t:1.250000 type:I last_keyframe:3
`
	frames, err := parseCreditsBlackFrames(black)
	if err != nil || !reflect.DeepEqual(frames, []creditsskipper.BlackFrame{{Frame: 2, Percentage: 99, Time: .043}, {Frame: 3, Percentage: 85, Time: 1.25}}) {
		t.Fatalf("black frames %+v %v", frames, err)
	}
	intervals, err := parseCreditsBlackIntervals("[blackdetect @ x] black_start:-0.02 black_end:1.25 black_duration:1.27\n[blackdetect @ x] black_start:2 black_end:2 black_duration:0\n")
	if err != nil || !reflect.DeepEqual(intervals, []creditsskipper.Range{{Start: -.02, End: 1.25}}) {
		t.Fatalf("interval clocks %+v %v", intervals, err)
	}
	silence, err := parseCreditsSilence("silence_start: 0.1\nsilence_end: 0.3 | silence_duration: 0.2\nsilence_start: 1.25\n", 10)
	if err != nil || !reflect.DeepEqual(silence, []creditsskipper.Range{{Start: 10.1, End: 10.3}}) {
		t.Fatalf("silence offset or prematurely filtered .2s evidence %+v %v", silence, err)
	}
	keys, err := parseCreditsBoundaryKeyframes("[Parsed_showinfo_0 @ x] n:0 pts:0 pts_time:0.25 pos:1\n[Parsed_showinfo_0 @ x] n:1 pts:0 pts_time:1.75 pos:2\n", 100)
	if err != nil || !reflect.DeepEqual(keys, []float64{100.25, 101.75}) {
		t.Fatalf("keyframe offset %+v %v", keys, err)
	}
}

func TestCreditsVisualMetadataKeepsNormalizedLumaAndClipsOnlyVisualWindow(t *testing.T) {
	raw := `[Parsed_metadata_3 @ x] frame:0 pts:0 pts_time:-1
[Parsed_metadata_3 @ x] lavfi.entropy.normalized_entropy.normal.Y=0.15
[Parsed_metadata_3 @ x] lavfi.signalstats.SATAVG=10
[Parsed_metadata_3 @ x] frame:1 pts:0 pts_time:0
[Parsed_metadata_3 @ x] lavfi.entropy.entropy.normal.Y=8
[Parsed_metadata_3 @ x] lavfi.entropy.normalized_entropy.normal.Y=1.25e-1
[Parsed_metadata_3 @ x] lavfi.signalstats.SATAVG=20
[Parsed_metadata_3 @ x] frame:2 pts:0 pts_time:1
[Parsed_metadata_3 @ x] lavfi.entropy.normalized_entropy.normal.Y=0.5
[Parsed_metadata_3 @ x] frame:3 pts:0 pts_time:2
[Parsed_metadata_3 @ x] lavfi.entropy.normalized_entropy.normal.Y=0.1
[Parsed_metadata_3 @ x] lavfi.signalstats.SATAVG=3
[Parsed_metadata_3 @ x] frame:4 pts:0 pts_time:3
[Parsed_metadata_3 @ x] lavfi.entropy.normalized_entropy.normal.Y=0.2
[Parsed_metadata_3 @ x] lavfi.signalstats.SATAVG=4
`
	values, err := parseCreditsKeyframeVisuals(raw, 2)
	if err != nil || !reflect.DeepEqual(values, []creditsskipper.KeyframeVisual{{Time: 0, Entropy: .125, Saturation: 20}, {Time: 2, Entropy: .1, Saturation: 3}}) {
		t.Fatalf("normalized or bounded metadata %+v %v", values, err)
	}
}

func TestCreditsVisualBlackFrameBodySurvivesInterleavedOutputHeader(t *testing.T) {
	// Retained shape of a real FFmpeg 9.0.1 two-output gray10 decode. The
	// frame at one second loses its av_log prefix inside the output header,
	// while every source-evidence field remains present and unchanged.
	raw := `[Parsed_blackframe_0 @ 0x1] frame:0 pblack:100 pts:0 t:0.000000 type:I last_keyframe:0
    encoder         : frame:1 pblack:100 pts:1000 t:1.000000 type:I last_keyframe:0
[Parsed_metadata_3 @ 0x2] frame:1    pts:1000    pts_time:1
[Parsed_metadata_3 @ 0x2] lavfi.entropy.normalized_entropy.normal.Y=0.034121
[Parsed_blackframe_0 @ 0x1] frame:2 pblack:100 pts:2000 t:2.000000 type:I last_keyframe:0
`
	actual, err := parseCreditsBlackFrames(raw)
	wanted := []creditsskipper.BlackFrame{{Frame: 0, Percentage: 100, Time: 0}, {Frame: 1, Percentage: 100, Time: 1}, {Frame: 2, Percentage: 100, Time: 2}}
	if err != nil || !reflect.DeepEqual(actual, wanted) {
		t.Fatalf("complete blackframe evidence was lost in an interleaved header: %+v %v", actual, err)
	}
	if partial, err := parseCreditsBlackFrames("frame:1 pblack:100 pts:1000 t:1.000000\n"); err != nil || len(partial) != 0 {
		t.Fatal("partial text became complete blackframe evidence")
	}
}

func TestCreditsVisualEvidenceAndCaptureHaveFiniteBounds(t *testing.T) {
	sink := &creditsVisualLog{limit: 4}
	if _, err := sink.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("5")); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatal("unbounded diagnostics")
	}
	sink.Close(nil)
	if _, err := sink.result(); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatal("budget failure became usable output")
	}
	if _, err := parseCreditsBlackFrames(strings.Repeat("x", (64<<10)+1)); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatal("unbounded diagnostic line")
	}
	capabilities := CreditsVisualCapabilities{Available: true, VisualsAvailable: true, FFmpegSHA256: strings.Repeat("a", 64)}
	profile, err := CreditsVisualAlgorithmProfile(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	capabilities.VisualsAvailable = false
	without, err := CreditsVisualAlgorithmProfile(capabilities)
	if err != nil || profile == without || !strings.Contains(profile, creditsskipper.UpstreamCommit) || len(profile) > 512 {
		t.Fatal("optional branch not bound to path-free profile")
	}
}
