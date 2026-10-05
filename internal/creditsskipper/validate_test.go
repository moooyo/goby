// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

func TestStoredResultRejectsAlteredDeterministicEvidence(t *testing.T) {
	base, err := Detect(context.Background(), Request{DurationSeconds: 500, Chapters: []Chapter{{"Main", 0}, {"Ending", 350}, {"Preview", 438}}}, &testProbe{keyframes: []float64{441}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Result)
	}{
		{"version", func(r *Result) { r.Version = "other" }},
		{"source window", func(r *Result) { r.Window.Start++ }},
		{"changed options", func(r *Result) { r.Options.BlackFrameThreshold++ }},
		{"unknown source", func(r *Result) { r.Segments[0].Source = "Invented" }},
		{"nan source timestamp", func(r *Result) { r.Evidence.RawCandidates[0].Start = math.NaN() }},
		{"raw candidate changed", func(r *Result) { r.Evidence.RawCandidates[0].Start++ }},
		{"combined candidate changed", func(r *Result) { r.Evidence.CombinedCandidates[0].End++ }},
		{"hard boundary removed", func(r *Result) { r.Evidence.HardBoundaries = nil }},
		{"adjustment original changed", func(r *Result) { r.Evidence.Adjustments[0].Original.Start++ }},
		{"chapter plan changed", func(r *Result) { r.Evidence.Adjustments[0].Plan.Segment.Start++ }},
		{"search range changed", func(r *Result) { r.Evidence.Adjustments[0].SearchRange.End++ }},
		{"keyframe removed", func(r *Result) { r.Evidence.Adjustments[0].Keyframes = nil }},
		{"adjusted output changed", func(r *Result) { r.Evidence.Adjustments[0].Adjusted.End++ }},
		{"extra final segment", func(r *Result) { r.Segments = append(r.Segments, r.Segments[0]) }},
		{"unbounded samples", func(r *Result) { r.Evidence.BlackFrameCount = MaxSummaryFrames + 1 }},
		{"invented visual branch", func(r *Result) { r.Evidence.VisualMethod = "Entropy" }},
		{"unbounded intervals", func(r *Result) { r.Evidence.BlackIntervals = make([]Range, MaxEvidenceRanges+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var value Result
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&value)
			if ValidateStoredResult(value, 500, false) == nil {
				t.Fatal("accepted altered stored evidence")
			}
		})
	}
}

func TestStoredNoMatchIsValidWithoutFabricatedCandidates(t *testing.T) {
	value, err := Detect(context.Background(), Request{DurationSeconds: 500, IsMovie: true}, &testProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoredResult(value, 500, true); err != nil {
		t.Fatal(err)
	}
	if len(value.Segments) != 0 || len(value.Evidence.RawCandidates) != 0 || len(value.Evidence.Adjustments) != 0 {
		t.Fatalf("fabricated candidate: %+v", value)
	}
}
