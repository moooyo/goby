package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"testing"
)

func requireVisualDiagnosticEquivalence(t *testing.T, cohort Cohort) (Result, Diagnostics) {
	t.Helper()
	plain, plainErr := Analyze(context.Background(), cohort, DefaultOptions())
	observed, diagnostics, err := AnalyzeWithDiagnostics(context.Background(), cohort, DefaultOptions())
	if plainErr != nil || err != nil || !reflect.DeepEqual(plain, observed) {
		t.Fatalf("visual observations changed result or comparisons: plain=%v observed=%v equal=%v", plainErr, err, reflect.DeepEqual(plain, observed))
	}
	x, _ := json.Marshal(plain)
	y, _ := json.Marshal(observed)
	if string(x) != string(y) || !diagnostics.Completed || !diagnostics.Visual.Considered || !diagnostics.Visual.Completed {
		t.Fatalf("visual trace changed serialized output or did not finish: %+v", diagnostics.Visual)
	}
	return observed, diagnostics
}

func TestVisualDiagnosticsObserveNonemptyCalibratedAndCoarseProductionBranches(t *testing.T) {
	for _, policy := range []string{VisualMeasurementCalibrated, VisualMeasurementCoarse} {
		t.Run(policy, func(t *testing.T) {
			cohort := visualSequenceFixture()
			if policy == VisualMeasurementCalibrated {
				cohort, _ = calibratedSequenceFixture()
			}
			result, diagnostics := requireVisualDiagnosticEquivalence(t, cohort)
			v := diagnostics.Visual
			branch := v.Coarse
			if policy == VisualMeasurementCalibrated {
				branch = v.Calibrated
				if v.Counts.CalibrationChecks != 2 || v.Counts.GuardChecks == 0 || !v.Coarse.Completed || v.Coarse.HasQuorum {
					t.Fatalf("calibrated stages or coarse exclusion were not observed: %+v", v)
				}
			} else if v.Calibrated.Attempted {
				t.Fatal("coarse-only input fabricated a calibrated attempt")
			}
			if !v.Attempted || !branch.Attempted || !branch.Completed || !branch.HasQuorum || branch.IndependentSources != 3 || branch.EligibleSources != 3 || branch.Groups == 0 ||
				v.Counts.PairLookups != 3 || v.Counts.GroupSearches == 0 || v.Counts.DiscoveryGroups == 0 || v.Counts.PublishedGroups != len(result.Groups) || len(result.Groups) == 0 || v.TraceEntries != len(v.Entries) || v.TraceTruncated {
				t.Fatalf("nonempty branch evidence was lost: %+v", v)
			}
			before, _ := json.Marshal(diagnostics)
			if _, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions()); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(diagnostics)
			if string(before) != string(after) {
				t.Fatal("separate experimental discovery contaminated production diagnostics")
			}
			cohort.Episodes[0], cohort.Episodes[2] = cohort.Episodes[2], cohort.Episodes[0]
			_, reversed, err := AnalyzeWithDiagnostics(context.Background(), cohort, DefaultOptions())
			if err != nil || !reflect.DeepEqual(diagnostics, reversed) {
				t.Fatalf("input order changed the visual trace: %v", err)
			}
		})
	}
}

func TestVisualDiagnosticsExplainQuorumCalibrationPairGroupAndGuardAbstention(t *testing.T) {
	for _, kind := range []string{"alias", "profile", "calibration", "pair_group", "guard"} {
		t.Run(kind, func(t *testing.T) {
			cohort, _ := calibratedSequenceFixture()
			switch kind {
			case "alias":
				cohort.Episodes[2].ContentIdentity = cohort.Episodes[0].ContentIdentity
			case "profile":
				cohort.Episodes[2].AlgorithmProfile = "different-profile"
			case "calibration":
				rng := rand.New(rand.NewSource(910))
				for i := range cohort.Episodes[1].Refinement {
					cohort.Episodes[1].Refinement[i].Raster = calibratedFixtureRaster(rng)
				}
			case "pair_group":
				cohort = visualSequenceFixture()
				for i := range cohort.Episodes[2].Visual {
					for row := 2; row < 6; row++ {
						for column := 2; column < 6; column++ {
							cohort.Episodes[2].Visual[i].Luma[row*8+column] = 127
						}
					}
				}
			case "guard":
				rng := rand.New(rand.NewSource(1591))
				for source := range cohort.Episodes {
					for frame := 114 + source*7; frame < 130+source*7; frame++ {
						cohort.Episodes[source].Refinement[frame].Raster = calibratedFixtureRaster(rng)
					}
				}
			}
			result, diagnostics := requireVisualDiagnosticEquivalence(t, cohort)
			v := diagnostics.Visual
			if len(result.Groups) != 0 || !v.Attempted {
				t.Fatalf("abstention fixture produced groups or skipped visual discovery: %+v", v)
			}
			switch kind {
			case "alias":
				if v.Calibrated.HasQuorum || v.Calibrated.IndependentSources != 2 || v.Calibrated.EligibleSources != 2 {
					t.Fatalf("identity alias supplied independent quorum: %+v", v)
				}
			case "profile":
				if v.Calibrated.HasQuorum || v.Calibrated.EligibleSources != 3 {
					t.Fatalf("profile quorum was confused with total eligible sources: %+v", v)
				}
			case "calibration":
				if v.Counts.CalibrationRejected == 0 {
					t.Fatalf("failed anchor/source calibration was not observed: %+v", v)
				}
			case "pair_group":
				if v.Counts.PairsWithoutHypotheses == 0 || v.Counts.GroupSearchesWithoutGroup == 0 {
					t.Fatalf("pair and complete-clique rejection were not observed: %+v", v)
				}
			case "guard":
				if v.Counts.GuardRejected == 0 {
					t.Fatalf("short guarded common window was not observed: %+v", v)
				}
			}
		})
	}
}

func visualDiagnosticCrossingFixture() Cohort {
	rng := rand.New(rand.NewSource(809))
	opening := make([][256]byte, 160)
	for i := range opening {
		if i%5 == 0 {
			opening[i] = calibratedFixtureRaster(rng)
		} else {
			opening[i] = opening[i-1]
		}
	}
	cohort := Cohort{Key: "diagnostic-crossing-cliques"}
	for source := 0; source < 5; source++ {
		key := string(rune('a' + source))
		episode := Episode{EpisodeKey: key, SourceKey: "source-" + key, ContentIdentity: "content-" + key, AlgorithmProfile: "diagnostic-crossing", DurationTicks: 60 * TicksPerSecond}
		for frame := 0; frame < 230; frame++ {
			raster := calibratedFixtureRaster(rng)
			position := frame - (30 + source*7)
			shared := position >= 0 && position < len(opening)
			if source == 1 || source == 2 {
				shared = shared && position < 130
			} else if source == 3 || source == 4 {
				shared = shared && position >= 30
			}
			if shared {
				raster = opening[position]
			}
			episode.Refinement = append(episode.Refinement, RefinementSample{Ticks: int64(frame) * calibratedFixtureStep, Raster: raster})
		}
		cohort.Episodes = append(cohort.Episodes, episode)
	}
	return cohort
}

func TestVisualDiagnosticsDistinguishRecoveredAndRetainedAmbiguity(t *testing.T) {
	for _, recoverable := range []bool{false, true} {
		cohort := visualSequenceFixture()
		if recoverable {
			cohort = visualDiagnosticCrossingFixture()
		} else {
			rng := rand.New(rand.NewSource(811))
			for frame := 0; frame < 17; frame++ {
				sample := VisualSample{Hash: rng.Uint64(), Contrast: 200, LumaKnown: true}
				for cell := range sample.Luma {
					sample.Luma[cell] = int8(rng.Intn(111) - 55)
				}
				for source := range cohort.Episodes {
					index := 100 + source*4 + frame
					sample.Ticks = cohort.Episodes[source].Visual[index].Ticks
					cohort.Episodes[source].Visual[index] = sample
				}
			}
		}
		result, diagnostics := requireVisualDiagnosticEquivalence(t, cohort)
		c := diagnostics.Visual.Counts
		if c.OriginalAmbiguousSources == 0 || c.OriginalAmbiguousSources != c.RecoveredSources+c.RetainedAmbiguousSources {
			t.Fatalf("ambiguity accounting lost an original source: %+v", c)
		}
		if recoverable {
			if c.RecoveredSources == 0 || c.RecoveryGroups == 0 || c.RetainedAmbiguousSources != 0 {
				t.Fatalf("remeasured recovery was not observed: %+v", c)
			}
			for _, episode := range result.Episodes {
				if episode.Status != Qualified {
					t.Fatalf("crossing fixture was not actually recovered: %+v", episode)
				}
			}
		} else if c.RecoveredSources != 0 || c.RetainedAmbiguousSources != 3 || len(result.Groups) != 0 {
			t.Fatalf("disjoint evidence was published or its ambiguity disappeared: %+v", c)
		}
	}
}

func TestVisualDiagnosticsTruncationLeavesActualResultsAndAggregateCountsComplete(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	complete, full := requireVisualDiagnosticEquivalence(t, cohort)
	value := Diagnostics{TraceLimit: 0, Visual: VisualFallbackDiagnostics{TraceLimit: 1, Entries: []VisualDiagnosticEntry{}}}
	observed, err := analyze(context.Background(), cohort, DefaultOptions(), &diagnosticsCollector{value: &value})
	if err != nil || !reflect.DeepEqual(complete, observed) || value.Visual.Counts != full.Visual.Counts || !value.Visual.TraceTruncated || value.Visual.TraceEntries != 1 || len(value.Visual.Entries) != 1 ||
		value.TraceEntries != 0 || !reflect.DeepEqual(value.Visual.Entries[0], full.Visual.Entries[0]) || full.Visual.TraceLimit != MaxVisualDiagnosticTraceEntries || MaxVisualDiagnosticTraceEntries != 128 {
		t.Fatalf("bounded visual trace changed measured work or aggregate counts: %+v, %v", value, err)
	}
}

func TestVisualDiagnosticsReportCacheReuseWithoutRecomputingMeasurements(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	geometry := calibratedGeometry{ScaleYPermille: 1000}
	a, err := calibratedView(cohort.Episodes[0], geometry, v2TestBudget())
	if err != nil {
		t.Fatal(err)
	}
	b, err := calibratedView(cohort.Episodes[1], geometry, v2TestBudget())
	if err != nil {
		t.Fatal(err)
	}
	plainCache, observedCache := newCalibratedPairCache(), newCalibratedPairCache()
	value := Diagnostics{Visual: VisualFallbackDiagnostics{TraceLimit: MaxVisualDiagnosticTraceEntries}}
	collector := &diagnosticsCollector{value: &value}
	for attempt := 0; attempt < 2; attempt++ {
		plainBudget, observedBudget := v2TestBudget(), v2TestBudget()
		plain, plainErr := plainCache.get(a, b, geometry, geometry, DefaultOptions(), plainBudget)
		observed, err := observedCache.getObserved(a, b, geometry, geometry, DefaultOptions(), observedBudget, a.SourceKey, collector)
		if plainErr != nil || err != nil || !reflect.DeepEqual(plain, observed) || plainBudget.used != observedBudget.used || len(observed) == 0 || value.Visual.Entries[attempt].CacheHit != (attempt == 1) {
			t.Fatalf("cache observation changed measured work or mislabeled reuse: %v, %v", plainErr, err)
		}
	}
	if value.Visual.Counts.PairLookups != 2 || value.Visual.Counts.PairCacheHits != 1 {
		t.Fatalf("cache lookup counts were presented as unique scans: %+v", value.Visual.Counts)
	}
	empty := b
	empty.SourceKey = "empty-cached-source"
	empty.Visual = append([]VisualSample(nil), b.Visual...)
	for index := range empty.Visual {
		empty.Visual[index].Hash = 1
	}
	for attempt := 0; attempt < 2; attempt++ {
		plainBudget, observedBudget := v2TestBudget(), v2TestBudget()
		plain, plainErr := plainCache.get(a, empty, geometry, geometry, DefaultOptions(), plainBudget)
		observed, err := observedCache.getObserved(a, empty, geometry, geometry, DefaultOptions(), observedBudget, a.SourceKey, collector)
		if plainErr != nil || err != nil || !reflect.DeepEqual(plain, observed) || plainBudget.used != observedBudget.used || len(observed) != 0 || value.Visual.Entries[attempt+2].CacheHit != (attempt == 1) {
			t.Fatalf("an empty cached scan was repeated or misreported: %v, %v", plainErr, err)
		}
	}
	if value.Visual.Counts.PairLookups != 4 || value.Visual.Counts.PairCacheHits != 2 || value.Visual.Counts.PairsWithoutHypotheses != 2 {
		t.Fatalf("empty cache reuse lost its actual lookup outcome: %+v", value.Visual.Counts)
	}
}

func TestVisualDiagnosticsPreservePartialFailureAndSkipSemantics(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	contextForRun := func() context.Context {
		base, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		return &cancelDuringContext{Context: base, cancel: cancel, after: 30}
	}
	plain, plainErr := Analyze(contextForRun(), cohort, DefaultOptions())
	observed, diagnostics, err := AnalyzeWithDiagnostics(contextForRun(), cohort, DefaultOptions())
	if !errors.Is(plainErr, context.Canceled) || !errors.Is(err, context.Canceled) || !reflect.DeepEqual(plain, Result{}) || !reflect.DeepEqual(observed, Result{}) || diagnostics.Completed || diagnostics.Visual.Completed || !diagnostics.Visual.Attempted || !diagnostics.Visual.Calibrated.Attempted {
		t.Fatalf("partial visual observations changed cancellation or authorized results: %+v, %v, %v", diagnostics.Visual, plainErr, err)
	}
	for _, collector := range []*diagnosticsCollector{nil, {value: &Diagnostics{Visual: VisualFallbackDiagnostics{TraceLimit: MaxVisualDiagnosticTraceEntries}}}} {
		groups, selection, err := visualFallbackGroups(cohort, cohort.Episodes, DefaultOptions(), nil, &workBudget{ctx: context.Background(), limit: 1}, collector)
		if !errors.Is(err, ErrLimit) || groups != nil || selection != nil || collector != nil && (!collector.value.Visual.Attempted || collector.value.Visual.Completed) {
			t.Fatalf("visual budget failure returned partial publication evidence: %+v, %v", groups, err)
		}
	}
	calibrated, err := discoverCalibratedSequences(context.Background(), cohort.Episodes, DefaultOptions(), nil)
	if err != nil || len(calibrated.Groups) == 0 {
		t.Fatalf("completed-branch failure control has no calibrated witness: %v", err)
	}
	partial := Diagnostics{Visual: VisualFallbackDiagnostics{TraceLimit: MaxVisualDiagnosticTraceEntries}}
	for _, collector := range []*diagnosticsCollector{nil, {value: &partial}} {
		groups, selection, err := visualFallbackGroups(cohort, cohort.Episodes, DefaultOptions(), nil, &workBudget{ctx: context.Background(), limit: calibrated.Comparisons}, collector)
		if !errors.Is(err, ErrLimit) || groups != nil || selection != nil {
			t.Fatalf("failure after a complete calibrated branch leaked witnesses: %+v, %v", groups, err)
		}
	}
	if !partial.Visual.Calibrated.Completed || partial.Visual.Calibrated.Groups == 0 || partial.Visual.Coarse.Attempted || partial.Visual.Completed || partial.Completed {
		t.Fatalf("partial trace promoted branch completion to publication authority: %+v", partial)
	}
	for _, kind := range []string{"audio_groups_available", "non_default_options", "insufficient_ready_sources"} {
		input := cohort
		o := DefaultOptions()
		switch kind {
		case "audio_groups_available":
			input = testCohort()
		case "non_default_options":
			o.MaxComparisons--
		case "insufficient_ready_sources":
			input.Episodes = append([]Episode(nil), cohort.Episodes...)
			input.Episodes[2].Refinement = nil
		}
		plain, plainErr := Analyze(context.Background(), input, o)
		observed, d, err := AnalyzeWithDiagnostics(context.Background(), input, o)
		if plainErr != nil || err != nil || !reflect.DeepEqual(plain, observed) || d.Visual.SkipReason != kind || d.Visual.Attempted || !d.Visual.Completed {
			t.Fatalf("skipped fallback acquired visual measurements: kind=%s %+v, %v, %v", kind, d.Visual, plainErr, err)
		}
	}
}
