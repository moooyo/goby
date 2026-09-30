package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestAnalysisRefinementBoundsPrefixAndFloorsCompleteSlots(t *testing.T) {
	for _, fixture := range []struct {
		duration, end int64
		frames        int
	}{
		{1000 * TicksPerSecond, 120 * TicksPerSecond, 1200},
		{120 * TicksPerSecond, 120 * TicksPerSecond, 1200},
		{120*TicksPerSecond - 1, 120*TicksPerSecond - TicksPerSecond/10, 1199},
		{137*TicksPerSecond/10 + 1, 137 * TicksPerSecond / 10, 137},
		{TicksPerSecond / 10, TicksPerSecond / 10, 1},
	} {
		for _, origin := range []int64{-3 * TicksPerSecond, 0, 17*TicksPerSecond + 123} {
			info := Info{DurationTicks: fixture.duration, FormatStartTicks: origin, FormatStartKnown: true}
			plan, err := analysisIntroRefinementPlan(info, DefaultAnalysisLimits())
			if err != nil || plan.start != 0 || plan.end != fixture.end || plan.frames != fixture.frames ||
				plan.interval != TicksPerSecond/10 || plan.width != 16 || plan.height != 16 || plan.pixelFormat != "gray" || plan.channels != 1 {
				t.Fatalf("refinement plan changed its bounded actual-source raster policy: %+v, %v", plan, err)
			}
			stream := Stream{Index: 0, TimeBase: "1/1000"}
			if err := analysisRefinementPTSSelection(info, stream, &plan); err != nil {
				t.Fatal(err)
			}
			args := strings.Join(buildAnalysisVisualArgs(info, stream, plan, DefaultAnalysisLimits()), " ")
			if !strings.Contains(args, "select='gte(pts*") || strings.Contains(args, "selected_n*0.1000000") || !strings.Contains(args, "scale=16:16:flags=area") ||
				!strings.Contains(args, "-copyts") || strings.Contains(args, " -ss ") || strings.Contains(args, " -t ") {
				t.Fatalf("refinement stopped decoding directly on the source clock: %s", args)
			}
		}
	}
	for _, duration := range []int64{-1, 0, TicksPerSecond/10 - 1} {
		if _, err := analysisIntroRefinementPlan(Info{DurationTicks: duration}, DefaultAnalysisLimits()); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("an incomplete refinement slot was admitted: duration=%d error=%v", duration, err)
		}
	}
}

func TestAnalysisRefinementRetainsExplicitResourceBudgets(t *testing.T) {
	info := Info{DurationTicks: 600 * TicksPerSecond}
	limits := DefaultAnalysisLimits()
	limits.MaxVisualSamples, limits.MaxRawBytes, limits.MaxFramePixels = 1200, 1200*256, 256
	if _, err := analysisIntroRefinementPlan(info, limits); err != nil {
		t.Fatalf("exact refinement budget was rejected: %v", err)
	}
	for _, mutate := range []func(*AnalysisLimits){
		func(v *AnalysisLimits) { v.MaxVisualSamples-- },
		func(v *AnalysisLimits) { v.MaxRawBytes-- },
		func(v *AnalysisLimits) { v.MaxFramePixels-- },
	} {
		limited := limits
		mutate(&limited)
		if _, err := analysisIntroRefinementPlan(info, limited); !errors.Is(err, ErrAnalysisBudget) {
			t.Fatalf("refinement silently reduced its horizon to evade a resource limit: %v", err)
		}
	}
	if _, err := analysisVisualRasterOptions(info, VisualAnalysisOptions{}, limits, 1); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatalf("an arbitrary raster size entered the shared decoder: %v", err)
	}
	if samples, err := (AnalysisExtractor{}).ExtractRefinement(nil, nil, info, 0); !errors.Is(err, ErrAnalysisUnavailable) || samples != nil {
		t.Fatalf("refinement accepted an absent context: %+v, %v", samples, err)
	}
}

func TestAnalysisRefinementUsesAuditedActualPTSAndRasterBoundaries(t *testing.T) {
	info, stream := visualAnalysisTestInfo()
	info.DurationTicks = 3 * TicksPerSecond / 10
	limits := DefaultAnalysisLimits()
	plan, err := analysisIntroRefinementPlan(info, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := analysisRefinementPTSSelection(info, stream, &plan); err != nil {
		t.Fatal(err)
	}
	log, err := newAnalysisVisualLog(info, stream, plan, limits)
	if err != nil {
		t.Fatal(err)
	}
	transcript := visualAnalysisConfig("source", "1/1000")
	for index, pts := range []string{"1000", "1130", "1210"} {
		transcript += visualAnalysisPacket(stream.Index, pts) + visualAnalysisFrame("source", index, pts, stream.Width, stream.Height, "yuv420p") + visualAnalysisOutput(pts, "1/1000")
	}
	if _, err := log.Write([]byte(transcript)); err != nil {
		t.Fatal(err)
	}
	log.Close(nil)
	raw := append(bytes.Repeat([]byte{17}, 256), bytes.Repeat([]byte{55}, 256)...)
	raw = append(raw, bytes.Repeat([]byte{90}, 256)...)
	var ticks []int64
	var rasters [][]byte
	err = readAnalysisVisualFrames(context.Background(), bytes.NewReader(raw), log, func(frame analysisVisualFrame, gray []byte) error {
		ticks = append(ticks, frame.actual)
		rasters = append(rasters, bytes.Clone(gray))
		return nil
	})
	if err != nil || log.result() != nil || fmt.Sprint(ticks) != "[0 1300000 2100000]" || len(rasters) != 3 {
		t.Fatalf("refinement did not retain the actual 16x16 frame sequence: ticks=%v error=%v metadata=%v", ticks, err, log.result())
	}
	for i, value := range []byte{17, 55, 90} {
		if !bytes.Equal(rasters[i], bytes.Repeat([]byte{value}, 256)) {
			t.Fatalf("refinement raster %d crossed a frame boundary", i)
		}
	}
}

func TestAnalysisRefinementSelectorUsesExactBoundedPTSProducts(t *testing.T) {
	for _, fixture := range []struct {
		base   string
		origin int64
		want   string
	}{
		{"1/1000", TicksPerSecond, "gte(pts*1,1000+selected_n*100)"},
		{"1/19184", 0, "gte(pts*5,0+selected_n*9592)"},
		{"1/11988", 0, "gte(pts*5,0+selected_n*5994)"},
		{"1/1000", -TicksPerSecond, "gte(pts*1,-1000+selected_n*100)"},
	} {
		info := Info{DurationTicks: 120 * TicksPerSecond, FormatStartTicks: fixture.origin}
		plan, err := analysisIntroRefinementPlan(info, DefaultAnalysisLimits())
		if err != nil {
			t.Fatal(err)
		}
		if err := analysisRefinementPTSSelection(info, Stream{TimeBase: fixture.base}, &plan); err != nil || plan.selectExpression != fixture.want {
			t.Fatalf("timestamp selector lost exact decimal-slot arithmetic: %q, %v", plan.selectExpression, err)
		}
	}
	info := Info{DurationTicks: 120 * TicksPerSecond, FormatStartTicks: math.MaxInt64}
	plan, err := analysisIntroRefinementPlan(info, DefaultAnalysisLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := analysisRefinementPTSSelection(info, Stream{TimeBase: "1/19184"}, &plan); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatalf("an inexact floating-point selector range was admitted: %v", err)
	}
	for _, sourceBase := range []string{"1/1000", "1/2000"} {
		info, stream := visualAnalysisTestInfo()
		info.DurationTicks = 3 * TicksPerSecond / 10
		plan, err := analysisIntroRefinementPlan(info, DefaultAnalysisLimits())
		if err != nil {
			t.Fatal(err)
		}
		if err := analysisRefinementPTSSelection(info, stream, &plan); err != nil {
			t.Fatal(err)
		}
		log, err := newAnalysisVisualLog(info, stream, plan, DefaultAnalysisLimits())
		if err != nil {
			t.Fatal(err)
		}
		_, err = log.Write([]byte(visualAnalysisConfig("source", sourceBase) + visualAnalysisPacket(stream.Index, "1000") + visualAnalysisFrame("source", 0, "1000", stream.Width, stream.Height, "yuv420p")))
		if (err == nil) != (sourceBase == stream.TimeBase) {
			t.Fatalf("selector source time-base proof differed from its declared clock: base=%s, error=%v", sourceBase, err)
		}
	}
}

func sparseRefinementTestLog(t *testing.T) *analysisVisualLog {
	t.Helper()
	info, stream := visualAnalysisTestInfo()
	info.DurationTicks = TicksPerSecond / 2
	plan, err := analysisIntroRefinementPlan(info, DefaultAnalysisLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := analysisRefinementPTSSelection(info, stream, &plan); err != nil {
		t.Fatal(err)
	}
	log, err := newAnalysisVisualLog(info, stream, plan, DefaultAnalysisLimits())
	if err != nil {
		t.Fatal(err)
	}
	transcript := visualAnalysisConfig("source", "1/1000")
	for index, pts := range []string{"1000", "1200", "1400"} {
		transcript += visualAnalysisPacket(stream.Index, pts) + visualAnalysisFrame("source", index, pts, stream.Width, stream.Height, "yuv420p") + visualAnalysisOutput(pts, "1/1000")
	}
	if _, err := log.Write([]byte(transcript)); err != nil {
		t.Fatalf("a proven dense-slot gap stopped the remaining audit: %v", err)
	}
	return log
}

func TestAnalysisRefinementCadenceAbstentionRequiresCompleteAuditedOutput(t *testing.T) {
	log := sparseRefinementTestLog(t)
	if err := log.result(); err == ErrRefinementCadenceUnsupported {
		t.Fatal("a partial decode authorized cadence abstention")
	}
	log.Close(nil)
	if err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, 3*256)), log, func(analysisVisualFrame, []byte) error { return nil }); err != nil {
		t.Fatalf("complete sparse rasters did not finish their audit: %v", err)
	}
	if err := log.result(); err != ErrRefinementCadenceUnsupported {
		t.Fatalf("the final proven sparse sequence did not retain its specific reason: %v", err)
	}
	for _, size := range []int{2 * 256, 3*256 - 1, 4 * 256} {
		log := sparseRefinementTestLog(t)
		log.Close(nil)
		err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, size)), log, func(analysisVisualFrame, []byte) error { return nil })
		if err == nil || err == ErrRefinementCadenceUnsupported {
			t.Fatalf("malformed raw output was reclassified as cadence: bytes=%d error=%v", size, err)
		}
	}
}

func TestAnalysisRefinementCadenceCannotMaskOtherAuditFailures(t *testing.T) {
	for _, failure := range []error{
		ErrAnalysisBudget, context.Canceled, context.DeadlineExceeded,
		errors.New("source ownership changed"), errors.New("tool descriptor changed"),
		errors.Join(ErrRefinementCadenceUnsupported, errors.New("decoder process failed")),
	} {
		log := sparseRefinementTestLog(t)
		log.Close(failure)
		if err := log.result(); err == ErrRefinementCadenceUnsupported || !errors.Is(err, failure) {
			t.Fatalf("cadence erased a concurrent audit/process failure: failure=%v result=%v", failure, err)
		}
	}
	log := sparseRefinementTestLog(t)
	_, err := log.Write([]byte(visualAnalysisPacket(2, "NOPTS")))
	if err == nil {
		t.Fatal("unknown original packet PTS was accepted after a cadence gap")
	}
	log.Close(nil)
	if result := log.result(); result == ErrRefinementCadenceUnsupported || !errors.Is(result, ErrAnalysisUnproven) {
		t.Fatalf("unknown PTS became an optional cadence omission: %v", result)
	}
}
