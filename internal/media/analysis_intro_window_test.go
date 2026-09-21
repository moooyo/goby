package media

import (
	"errors"
	"math"
	"testing"
)

func TestAnalysisIntroVisualWindowAlignsCompleteRelativeSlots(t *testing.T) {
	const second = TicksPerSecond
	for _, fixture := range []struct {
		name                    string
		duration, interval, end int64
		normalizedInterval      int64
		frames                  int
	}{
		{"partial_default_tail", 137*second + second/200, 0, 137 * second, second / 2, 274},
		{"exact_default_boundary", 137 * second, 0, 137 * second, second / 2, 274},
		{"one_tick_after_boundary", 137*second + 1, 0, 137 * second, second / 2, 274},
		{"explicit_default_interval", 137*second + second/200, second / 2, 137 * second, second / 2, 274},
		{"seven_second_interval_at_cap", 600 * second, 7 * second, 595 * second, 7 * second, 85},
		{"seven_second_interval_beyond_cap", 1000 * second, 7 * second, 595 * second, 7 * second, 85},
		{"default_interval_beyond_cap", 1000 * second, 0, 600 * second, second / 2, 1200},
		{"minimum_interval", 13*second + 1, second / 10, 13 * second, second / 10, 130},
		{"maximum_interval", 137*second + second/200, 10 * second, 130 * second, 10 * second, 13},
		{"one_complete_default_slot", second / 2, 0, second / 2, second / 2, 1},
		{"one_complete_minimum_slot", second / 10, second / 10, second / 10, second / 10, 1},
	} {
		for _, origin := range []struct {
			name  string
			ticks int64
		}{
			{"zero_origin", 0},
			{"positive_origin", 17*second + 123},
			{"negative_origin", -3*second - 1},
		} {
			t.Run(fixture.name+"/"+origin.name, func(t *testing.T) {
				info := Info{DurationTicks: fixture.duration, FormatStartKnown: true, FormatStartTicks: origin.ticks}
				options, err := analysisIntroVisualOptions(info, fixture.interval)
				want := VisualAnalysisOptions{EndTicks: fixture.end, IntervalTicks: fixture.normalizedInterval}
				if err != nil || options != want {
					t.Fatalf("relative intro window = %+v, want %+v, error = %v", options, want, err)
				}
				plan, err := analysisVisualOptions(info, options, DefaultAnalysisLimits())
				if err != nil || plan.start != 0 || plan.end != fixture.end ||
					plan.interval != fixture.normalizedInterval || plan.frames != fixture.frames {
					t.Fatalf("intro visual plan = %+v, want %d complete slots, error = %v", plan, fixture.frames, err)
				}
			})
		}
	}
}

func TestAnalysisIntroVisualWindowRejectsInvalidIntervalsAndEmptyHorizons(t *testing.T) {
	const second = TicksPerSecond
	for _, fixture := range []struct {
		name               string
		duration, interval int64
	}{
		{"negative_interval", 137 * second, -1},
		{"below_minimum_interval", 137 * second, second/10 - 1},
		{"above_maximum_interval", 137 * second, 10*second + 1},
		{"maximum_integer_interval", 137 * second, math.MaxInt64},
		{"zero_duration_default", 0, 0},
		{"zero_duration_custom", 0, second},
		{"negative_duration_default", -1, 0},
		{"negative_duration_custom", -1, second},
		{"short_default_window", second/2 - 1, 0},
		{"short_minimum_interval_window", second/10 - 1, second / 10},
		{"short_maximum_interval_window", 10*second - 1, 10 * second},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if options, err := analysisIntroVisualOptions(Info{DurationTicks: fixture.duration}, fixture.interval); !errors.Is(err, ErrAnalysisUnproven) {
				t.Fatalf("invalid intro window returned %+v, %v", options, err)
			}
		})
	}
}

func TestAnalysisIntroVisualWindowDoesNotChangeGenericCeilPlanning(t *testing.T) {
	info := Info{DurationTicks: 137*TicksPerSecond + TicksPerSecond/200}
	for _, fixture := range []struct {
		name    string
		options VisualAnalysisOptions
	}{
		{"generic_default", VisualAnalysisOptions{}},
		{"explicit_end", VisualAnalysisOptions{EndTicks: info.DurationTicks}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			plan, err := analysisVisualOptions(info, fixture.options, DefaultAnalysisLimits())
			if err != nil || plan.end != info.DurationTicks || plan.frames != 275 || plan.interval != TicksPerSecond/2 {
				t.Fatalf("generic partial-slot contract changed: %+v, %v", plan, err)
			}
		})
	}
	options, err := analysisIntroVisualOptions(info, 0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := analysisVisualOptions(info, options, DefaultAnalysisLimits())
	if err != nil || plan.end != 137*TicksPerSecond || plan.frames != 274 {
		t.Fatalf("intro-specific complete-slot plan = %+v, %v", plan, err)
	}
}

func TestAnalysisIntroVisualWindowRetainsVisualSampleBudget(t *testing.T) {
	info := Info{DurationTicks: 600 * TicksPerSecond}
	options, err := analysisIntroVisualOptions(info, TicksPerSecond/10)
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultAnalysisLimits()
	if _, err := analysisVisualOptions(info, options, limits); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("the default sample budget admitted 6000 visual slots: %v", err)
	}
	limits.MaxVisualSamples = 6000
	if plan, err := analysisVisualOptions(info, options, limits); err != nil || plan.frames != 6000 {
		t.Fatalf("exact admitted sample budget = %+v, %v", plan, err)
	}
	limits.MaxVisualSamples--
	if _, err := analysisVisualOptions(info, options, limits); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("intro planning silently shortened the window to fit the sample budget: %v", err)
	}
}
