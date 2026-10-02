package transcode

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func generatedSourceRangeTestPlan() Plan {
	return Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		Width: 160, Height: 96, FrameRate: 25, VideoBitrate: 256000, DurationTicks: 120 * ticksPerSecond,
		StartTicks: 90 * ticksPerSecond, SegmentSeconds: 6,
		HLS: HLSPlan{SegmentType: "mpegts", Window: HLSWindow{EndTicks: 96 * ticksPerSecond, StartNumber: 15, RequireInputEvidence: true}}}
}

func generatedSourceRangeFrameJSON(pts, duration int64) string {
	return fmt.Sprintf(`{"media_type":"video","stream_index":0,"pts":%d,"duration":%d}`, pts, duration)
}

func generatedSourceRangeDocumentJSON(frames, base, origin string) string {
	return `{"frames":[` + frames + `],"streams":[{"index":0,"codec_type":"video","time_base":` +
		fmt.Sprintf("%q", base) + `}],"format":{"start_time":` + fmt.Sprintf("%q", origin) + `}}`
}

func generatedSourceRangeFrames(first, step int64, count int) string {
	var data strings.Builder
	for index := 0; index < count; index++ {
		if index != 0 {
			data.WriteByte(',')
		}
		data.WriteString(generatedSourceRangeFrameJSON(first+int64(index)*step, step))
	}
	return data.String()
}

func TestGeneratedSourceRangeUsesSourceGlobalEpochAndCompleteFrames(t *testing.T) {
	p := generatedSourceRangeTestPlan()
	// The independent source origin is two seconds. Preroll and postroll are
	// outside the requested interval and do not inflate its coverage count.
	frames := generatedSourceRangeFrames(91920, 40, 153)
	got, err := parseGeneratedSourceRange([]byte(generatedSourceRangeDocumentJSON(frames, "1/1000", "2.000000")), p, 2*ticksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	want := GeneratedSourceRange{StartTicks: 90 * ticksPerSecond, EndTicks: 96 * ticksPerSecond, FrameCount: 150,
		First: GeneratedRational{Num: 90, Den: 1}, Last: GeneratedRational{Num: 2399, Den: 25},
		End: GeneratedRational{Num: 96, Den: 1}, FrameDuration: GeneratedRational{Num: 1, Den: 25}}
	if got != want || got.EndTicks == p.DurationTicks {
		t.Fatalf("source origin, requested coverage or frame count changed: %+v", got)
	}
}

func TestGeneratedSourceRangeSupportsNegativeOriginWithoutDoubleOffset(t *testing.T) {
	p := generatedSourceRangeTestPlan()
	data := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(88500, 40, 150), "1/1000", "-1.500000")
	got, err := parseGeneratedSourceRange([]byte(data), p, -15_000_000)
	if err != nil || got.First != (GeneratedRational{Num: 90, Den: 1}) || got.End != (GeneratedRational{Num: 96, Den: 1}) {
		t.Fatalf("negative source origin changed the source-relative range: %+v, %v", got, err)
	}
}

func TestGeneratedSourceRangeRejectsMissingOrPartialSourceCoverage(t *testing.T) {
	p := generatedSourceRangeTestPlan()
	valid := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92000, 40, 150), "1/1000", "2.000000")
	for name, data := range map[string]string{
		"missing final frame":       generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92000, 40, 149), "1/1000", "2.000000"),
		"short final duration":      strings.Replace(valid, `"pts":97960,"duration":40`, `"pts":97960,"duration":20`, 1),
		"middle gap":                strings.Replace(valid, generatedSourceRangeFrameJSON(92080, 40)+",", "", 1),
		"middle duplicate":          strings.Replace(valid, generatedSourceRangeFrameJSON(92080, 40), generatedSourceRangeFrameJSON(92040, 40), 1),
		"middle irregular duration": strings.Replace(valid, generatedSourceRangeFrameJSON(92080, 40), generatedSourceRangeFrameJSON(92080, 39), 1),
		"first frame is late":       strings.Replace(valid, generatedSourceRangeFrameJSON(92000, 40), generatedSourceRangeFrameJSON(92001, 40), 1),
		"preroll crosses start":     generatedSourceRangeDocumentJSON(generatedSourceRangeFrameJSON(91980, 40)+","+generatedSourceRangeFrames(92000, 40, 150), "1/1000", "2.000000"),
		"unknown origin":            strings.Replace(valid, "2.000000", "N/A", 1),
		"wrong origin":              strings.Replace(valid, "2.000000", "0.000000", 1),
		"coarse 24 fps duration":    generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92000, 41, 150), "1/1000", "2.000000"),
		"overflowing pts clock":     strings.Replace(valid, `"pts":92000`, fmt.Sprintf(`"pts":%d`, int64(math.MaxInt64)), 1),
	} {
		t.Run(name, func(t *testing.T) {
			plan := p
			if name == "coarse 24 fps duration" {
				plan.FrameRate = 24
			}
			got, err := parseGeneratedSourceRange([]byte(data), plan, 2*ticksPerSecond)
			if got != (GeneratedSourceRange{}) || !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("unsupported source interval was accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestGeneratedSourceRangeRejectsAmbiguousOrSynthesizedClocks(t *testing.T) {
	valid := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92000, 40, 150), "1/1000", "2.000000")
	for name, data := range map[string]string{
		"empty output":           "",
		"missing pts":            strings.Replace(valid, `"pts":92000,`, "", 1),
		"null pts":               strings.Replace(valid, `"pts":92000`, `"pts":null`, 1),
		"best effort substitute": strings.Replace(valid, `"pts":92000`, `"best_effort_timestamp":92000`, 1),
		"exponent pts":           strings.Replace(valid, `"pts":92000`, `"pts":9.2e4`, 1),
		"string pts":             strings.Replace(valid, `"pts":92000`, `"pts":"92000"`, 1),
		"missing duration":       strings.Replace(valid, `,"duration":40`, "", 1),
		"null duration":          strings.Replace(valid, `"duration":40`, `"duration":null`, 1),
		"negative duration":      strings.Replace(valid, `"duration":40`, `"duration":-40`, 1),
		"duplicate clock":        strings.Replace(valid, `"pts":92000`, `"pts":91960,"pts":92000`, 1),
		"case folded key":        strings.Replace(valid, `"pts":92000`, `"PTS":92000`, 1),
		"foreign frame":          strings.Replace(valid, `"stream_index":0`, `"stream_index":1`, 1),
		"foreign stream":         strings.Replace(valid, `"index":0`, `"index":1`, 1),
		"invalid timebase":       strings.Replace(valid, `"1/1000"`, `"1/0"`, 1),
		"missing format":         strings.Replace(valid, `,"format":{"start_time":"2.000000"}`, "", 1),
		"null format":            strings.Replace(valid, `"format":{"start_time":"2.000000"}`, `"format":null`, 1),
		"duplicate origin":       strings.Replace(valid, `"start_time":"2.000000"`, `"start_time":"0.000000","start_time":"2.000000"`, 1),
		"unknown root":           strings.TrimSuffix(valid, "}") + `,"metadata_eof":true}`,
		"truncated document":     strings.TrimSuffix(valid, "}"),
		"concatenated document":  valid + valid,
		"multiple streams":       strings.Replace(valid, `"streams":[`, `"streams":[{"index":1,"codec_type":"video","time_base":"1/1000"},`, 1),
		"nonempty programs":      strings.TrimSuffix(valid, "}") + `,"programs":[{}]}`,
		"nonempty stream groups": strings.TrimSuffix(valid, "}") + `,"stream_groups":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseGeneratedSourceRange([]byte(data), generatedSourceRangeTestPlan(), 2*ticksPerSecond)
			if got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("ambiguous or unavailable clock was accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestGeneratedSourceRangeStopsFrameOutputBeforeCompleteDocument(t *testing.T) {
	for _, key := range []string{`"frames"`, `"\u0066rames"`} {
		ctx, cancel := context.WithCancel(context.Background())
		budget := &generatedSourceRangeBudget{cancel: cancel}
		output := &generatedSourceRangeOutput{budget: budget}
		if _, err := output.Write([]byte("{" + key + ":[")); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < maxGeneratedSourceRangeFrames; index++ {
			prefix := ""
			if index != 0 {
				prefix = ","
			}
			// Split keys, quotes and nesting across copier chunks. This is
			// deliberately not a complete JSON document or coverage proof.
			data := prefix + generatedSourceRangeFrameJSON(int64(index)*40, 40)
			for offset := range len(data) {
				if _, err := output.Write([]byte(data[offset : offset+1])); err != nil {
					t.Fatalf("frame %d prematurely exhausted its budget: %v", index, err)
				}
			}
		}
		if _, err := output.Write([]byte(",{")); !errors.Is(err, ErrTimelineLimit) || ctx.Err() == nil {
			t.Fatalf("decoder frame limit did not cancel incomplete output: %v", err)
		}
		cancel()
	}
}

func TestGeneratedSourceRangeOriginRequiresExactTickRepresentation(t *testing.T) {
	for value, want := range map[string]int64{"0": 0, "2.000000": 20_000_000, "-0.0000001": -1, "17.1234567": 171_234_567} {
		if got, ok := generatedSourceRangeOrigin(value); !ok || got != want {
			t.Fatalf("exact origin %q changed: got=%d, valid=%t", value, got, ok)
		}
	}
	for _, value := range []string{"", "N/A", "2e0", "+2", "2.", ".2", "0.00000001", "2/1", "922337203685.4775808"} {
		if got, ok := generatedSourceRangeOrigin(value); ok {
			t.Fatalf("unrepresentable origin %q accepted: %d", value, got)
		}
	}
}

func TestGeneratedSourceRangeBoundsAllPrerollAndOutputBytes(t *testing.T) {
	p := generatedSourceRangeTestPlan()
	data := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(0, 40, maxGeneratedSourceRangeFrames+1), "1/1000", "2.000000")
	if got, err := parseGeneratedSourceRange([]byte(data), p, 2*ticksPerSecond); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("excess preroll was not counted: %+v, %v", got, err)
	}
	if _, err := parseGeneratedSourceRange([]byte(strings.Repeat(" ", maxGeneratedSourceRangeBytes+1)), p, 2*ticksPerSecond); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("oversized output accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget := &generatedSourceRangeBudget{cancel: cancel}
	output := &generatedSourceRangeOutput{budget: budget}
	if _, err := output.Write([]byte(strings.Repeat(" ", maxGeneratedSourceRangeBytes))); err != nil {
		t.Fatal(err)
	}
	if n, err := output.Write([]byte("x")); n != 0 || !errors.Is(err, ErrTimelineLimit) || ctx.Err() == nil || output.buffer.Len() != maxGeneratedSourceRangeBytes {
		t.Fatalf("stdout budget failed to cancel without growing: n=%d, err=%v", n, err)
	}
}

func TestGeneratedSourceRangeRequiresExplicitSupportedEvidencePlan(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"no input evidence": func(p *Plan) { p.HLS.Window.RequireInputEvidence = false },
		"audio":             func(p *Plan) { p.AudioStreamIndex, p.AudioCodec = 1, "aac" },
		"fractional fps":    func(p *Plan) { p.FrameRate = 24_000.0 / 1001 },
		"copied video":      func(p *Plan) { p.VideoCodec, p.VideoCopyCodec = "copy", "h264" },
	} {
		t.Run(name, func(t *testing.T) {
			p := generatedSourceRangeTestPlan()
			mutate(&p)
			if err := generatedSourceRangePlan(p, 0); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unsupported plan accepted: %v", err)
			}
		})
	}
	p := generatedSourceRangeTestPlan()
	p.StartTicks, p.HLS.Window.EndTicks = 0, p.DurationTicks
	if err := generatedSourceRangePlan(p, 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("unbounded source range accepted: %v", err)
	}
	if err := generatedSourceRangePlan(generatedSourceRangeTestPlan(), maxDurationTicks+1); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("unbounded origin accepted: %v", err)
	}
}
