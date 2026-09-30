package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestAnalyzeWithDiagnosticsPreservesResultsAndComparisonBudget(t *testing.T) {
	for _, kind := range []string{"qualified", "visual_rejected", "unrelated"} {
		t.Run(kind, func(t *testing.T) {
			cohort := testCohort()
			for i := range cohort.Episodes {
				if kind == "visual_rejected" {
					for j := range cohort.Episodes[i].Visual {
						cohort.Episodes[i].Visual[j].Contrast = 0
					}
				} else if kind == "unrelated" {
					cohort.Episodes[i] = testEpisode(i+1, 150*TicksPerSecond)
				}
			}
			plain, plainErr := Analyze(context.Background(), cohort, Options{})
			observed, diagnostics, err := AnalyzeWithDiagnostics(context.Background(), cohort, Options{})
			if plainErr != nil || err != nil || !reflect.DeepEqual(plain, observed) {
				t.Fatalf("diagnostics changed matching output or budget: plain=%v observed=%v equal=%v", plainErr, err, reflect.DeepEqual(plain, observed))
			}
			plainJSON, err := json.Marshal(plain)
			if err != nil {
				t.Fatal(err)
			}
			observedJSON, err := json.Marshal(observed)
			if err != nil || string(plainJSON) != string(observedJSON) {
				t.Fatal("diagnostics changed serialized result bytes", err)
			}
			if !diagnostics.Completed || diagnostics.Version != Version || diagnostics.TraceTruncated ||
				diagnostics.Counts.PairsConsidered != 3 || diagnostics.Counts.Groups != len(observed.Groups) {
				t.Fatalf("incorrect complete trace: %+v", diagnostics)
			}
			if kind == "qualified" && (diagnostics.Counts.AcceptedAudioRuns == 0 || diagnostics.Counts.VisualAccepted == 0 || diagnostics.Counts.PairHypotheses == 0 || diagnostics.Counts.Groups == 0) {
				t.Fatalf("accepted stages were not recorded: %+v", diagnostics.Counts)
			}
			if kind == "visual_rejected" && (diagnostics.Counts.VisualRejected == 0 || diagnostics.Counts.VisualAccepted != 0 || diagnostics.Counts.Groups != 0) {
				t.Fatalf("visual rejection stages were not recorded: %+v", diagnostics.Counts)
			}
			for left, right := 0, len(cohort.Episodes)-1; left < right; left, right = left+1, right-1 {
				cohort.Episodes[left], cohort.Episodes[right] = cohort.Episodes[right], cohort.Episodes[left]
			}
			_, permuted, err := AnalyzeWithDiagnostics(context.Background(), cohort, Options{})
			if err != nil || !reflect.DeepEqual(diagnostics, permuted) {
				t.Fatalf("input ordering changed diagnostics: %v", err)
			}
		})
	}
}

func TestAudioDiagnosticsDistinguishRawAndGuardedShortRuns(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		duration                         int64
		rawShort, guardedShort, accepted int
	}{
		{"raw_short", 10 * TicksPerSecond, 1, 0, 0},
		{"guarded_short", 18 * TicksPerSecond, 0, 1, 0},
		{"accepted", 30 * TicksPerSecond, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testEpisode(1, tc.duration)
			b := a
			b.EpisodeKey = "independent-second-episode"
			matched := make([]int, len(a.Audio))
			for i := range matched {
				matched[i] = i
			}
			value := Diagnostics{TraceLimit: MaxDiagnosticTraceEntries}
			collector := &diagnosticsCollector{value: &value}
			collector.beginPair(a, b)
			collector.nominatedOffsets(1, false)
			collector.beginOffset(0)
			o := DefaultOptions()
			budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
			observed, reason, err := measureAudioRunWithDiagnostics(a, b, matched, 0, len(matched)-1, o, budget, collector)
			plainBudget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
			plain, plainReason, plainErr := measureAudioRun(a, b, matched, 0, len(matched)-1, o, plainBudget)
			if err != nil || plainErr != nil || reason != plainReason || !reflect.DeepEqual(observed, plain) || budget.used != plainBudget.used {
				t.Fatalf("audio diagnostics changed discovery: %v %v", err, plainErr)
			}
			counts := value.Counts
			if counts.RawAudioRuns != 1 || counts.RawShortAudioRuns != tc.rawShort || counts.GuardedShortAudioRuns != tc.guardedShort || counts.AcceptedAudioRuns != tc.accepted ||
				counts.RawRejectedAudioRuns != 0 || counts.GuardedRejectedAudioRuns != 0 {
				t.Fatalf("short run was assigned to the wrong stage: %+v", counts)
			}
			offset := value.Pairs[0].Offsets[0].Counts
			if offset.RawAudioRuns != counts.RawAudioRuns || offset.RawShortAudioRuns != counts.RawShortAudioRuns || offset.GuardedShortAudioRuns != counts.GuardedShortAudioRuns || offset.AcceptedAudioRuns != counts.AcceptedAudioRuns {
				t.Fatalf("per-offset counters differ from aggregate counters: %+v %+v", offset, counts)
			}
		})
	}
}

func TestDiagnosticTraceIsBoundedWithoutTruncatingCounts(t *testing.T) {
	collect := func() Diagnostics {
		value := Diagnostics{TraceLimit: MaxDiagnosticTraceEntries}
		collector := &diagnosticsCollector{value: &value}
		for pair := 0; pair < 50; pair++ {
			collector.beginPair(Episode{EpisodeKey: "left"}, Episode{EpisodeKey: "right"})
			collector.nominatedOffsets(12, true)
			for offset := int64(0); offset < 12; offset++ {
				collector.beginOffset(offset)
				collector.audioStage(diagnosticAudioRaw, "")
				collector.audioStage(diagnosticAudioRawRejected, LowAudioEntropy)
				collector.visualResult(nil, InsufficientVisual)
			}
			collector.pairHypotheses(2)
		}
		collector.groups(3)
		return value
	}
	value := collect()
	entries := len(value.Pairs)
	for _, pair := range value.Pairs {
		entries += len(pair.Offsets)
	}
	if !value.TraceTruncated || value.TraceEntries != MaxDiagnosticTraceEntries || entries != MaxDiagnosticTraceEntries ||
		value.Counts.PairsConsidered != 50 || value.Counts.OffsetCandidates != 600 || value.Counts.RawAudioRuns != 600 ||
		value.Counts.RawRejectedAudioRuns != 600 || value.Counts.VisualRejected != 600 || value.Counts.PairHypotheses != 100 || value.Counts.Groups != 3 {
		t.Fatalf("trace truncation changed totals or exceeded its bound: %+v entries=%d", value.Counts, entries)
	}
	if !reflect.DeepEqual(value, collect()) {
		t.Fatal("trace truncation is not deterministic")
	}
}

func TestAnalyzeWithDiagnosticsPreservesErrorAndEmptyResult(t *testing.T) {
	for _, kind := range []string{"cancelled", "budget"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			o := DefaultOptions()
			want := ErrLimit
			if kind == "cancelled" {
				cancel()
				want = context.Canceled
			} else {
				o.MaxComparisons = 1
			}
			plain, plainErr := Analyze(ctx, testCohort(), o)
			result, diagnostics, err := AnalyzeWithDiagnostics(ctx, testCohort(), o)
			if !errors.Is(err, want) || !errors.Is(plainErr, want) || diagnostics.Completed ||
				!reflect.DeepEqual(plain, Result{}) || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("diagnostics changed failure behavior: plain=%v observed=%v completed=%v", plainErr, err, diagnostics.Completed)
			}
		})
	}
}
