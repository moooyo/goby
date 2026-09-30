package introdetect

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"
)

const calibratedFixtureStep = TicksPerSecond / 10

// The source rasters have independent material around a shared ten-second
// opening. Each raster is an actual observation at 100 ms, not an upsampled
// coarse descriptor, and the episode clocks differ by non-half-second offsets.
func calibratedSequenceFixture() (Cohort, map[string]Interval) {
	rng := rand.New(rand.NewSource(809))
	opening := make([][256]byte, 100)
	for i := range opening {
		if i%5 == 0 {
			opening[i] = calibratedFixtureRaster(rng)
		} else {
			opening[i] = opening[i-1]
		}
	}
	cohort := Cohort{Key: "calibrated-raster-fixture"}
	bounds := make(map[string]Interval)
	for source := 0; source < 3; source++ {
		key := string(rune('a' + source))
		episode := Episode{
			EpisodeKey:       "episode-" + key,
			SourceKey:        "refinement-" + key,
			ContentIdentity:  "content-" + key,
			AlgorithmProfile: "synthetic-refinement-v1",
			DurationTicks:    40 * TicksPerSecond,
		}
		start := 30 + source*7
		for frame := 0; frame < 200; frame++ {
			raster := calibratedFixtureRaster(rng)
			if frame >= start && frame < start+len(opening) {
				raster = opening[frame-start]
			}
			episode.Refinement = append(episode.Refinement, RefinementSample{
				Ticks:  int64(frame) * calibratedFixtureStep,
				Raster: raster,
			})
		}
		bounds[episode.EpisodeKey] = Interval{
			StartTicks: int64(start) * calibratedFixtureStep,
			EndTicks:   int64(start+len(opening)-1) * calibratedFixtureStep,
		}
		cohort.Episodes = append(cohort.Episodes, episode)
	}
	return cohort, bounds
}

func calibratedFixtureRaster(rng *rand.Rand) [256]byte {
	var raster [256]byte
	// Large independent cells retain meaningful contrast through the fixed
	// crop and spatial resampling, while every frame has a different state.
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			value := byte(20 + rng.Intn(216))
			for y := row * 4; y < (row+1)*4; y++ {
				for x := column * 4; x < (column+1)*4; x++ {
					raster[y*16+x] = value
				}
			}
		}
	}
	return raster
}

func requireCalibratedFixtureGroup(t *testing.T, result VisualSequenceResult, bounds map[string]Interval) VisualSequenceGroup {
	t.Helper()
	if !result.Experimental || result.Version != VisualSequenceVersion || result.SearchLimited || len(result.Groups) != 1 {
		t.Fatalf("complete calibrated opening was not discovered: %+v", result)
	}
	group := result.Groups[0]
	if len(group.Members) != 3 || group.Metrics.PairCount != 3 || group.Metrics.CoveragePermille < 850 ||
		group.Metrics.MaxLumaRMSPermille > 550 || group.Metrics.MaxCenterRMSPermille > 650 || group.Metrics.DistinctStates < 4 {
		t.Fatalf("calibrated opening lacks complete independent visual support: %+v", group)
	}
	for _, member := range group.Members {
		want, exists := bounds[member.EpisodeKey]
		if !exists || member.Interval.EndTicks-member.Interval.StartTicks < 8*TicksPerSecond ||
			member.Interval.StartTicks < want.StartTicks || member.Interval.EndTicks > want.EndTicks {
			t.Fatalf("member escaped its source opening or lost its full duration: got %+v, true opening %+v", member, want)
		}
	}
	return group
}

func TestCalibratedVisualSequencesFindIndependentRasterOpening(t *testing.T) {
	cohort, bounds := calibratedSequenceFixture()
	result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireCalibratedFixtureGroup(t, result, bounds)
	if result.Comparisons <= 1 {
		t.Fatal("calibrated discovery did not account for its comparison work")
	}
	// Canonical anchor selection and all measured output must be independent
	// of the order in which the caller supplied the complete cohort.
	cohort.Episodes[0], cohort.Episodes[2] = cohort.Episodes[2], cohort.Episodes[0]
	reversed, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
	if err != nil || !reflect.DeepEqual(result, reversed) {
		t.Fatalf("cohort permutation changed calibrated evidence: original %+v, reversed %+v, error %v", result, reversed, err)
	}
}

func TestCalibratedVisualSequencesFindCliqueWithoutFirstEpisode(t *testing.T) {
	cohort, bounds := calibratedSequenceFixture()
	unrelated := cohort.Episodes[0]
	unrelated.EpisodeKey, unrelated.SourceKey, unrelated.ContentIdentity = "episode-0-unrelated", "unrelated-source", "unrelated-content"
	unrelated.Refinement = append([]RefinementSample(nil), unrelated.Refinement...)
	rng := rand.New(rand.NewSource(910))
	for i := range unrelated.Refinement {
		unrelated.Refinement[i].Raster = calibratedFixtureRaster(rng)
	}
	cohort.Episodes = append([]Episode{unrelated}, cohort.Episodes...)
	result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireCalibratedFixtureGroup(t, result, bounds)
}

func TestCalibratedVisualSequencesPreferUsableRefinementAlias(t *testing.T) {
	for _, kind := range []string{"missing", "flat"} {
		t.Run(kind, func(t *testing.T) {
			cohort, bounds := calibratedSequenceFixture()
			alias := cohort.Episodes[0]
			alias.SourceKey = "legacy-a"
			alias.Refinement = nil
			// A coarse-ready alias sorts first and has more legacy evidence.
			// Neither that evidence nor flat rasters can displace usable
			// refinement belonging to the same independent episode.
			alias.Visual = visualSequenceFixture().Episodes[0].Visual[:80]
			if kind == "flat" {
				alias.Visual = nil
				var flat [256]byte
				for i := range flat {
					flat[i] = 128
				}
				for _, sample := range cohort.Episodes[0].Refinement {
					alias.Refinement = append(alias.Refinement, RefinementSample{Ticks: sample.Ticks, Raster: flat})
				}
			}
			cohort.Episodes = append(cohort.Episodes, alias)
			result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			group := requireCalibratedFixtureGroup(t, result, bounds)
			for _, member := range group.Members {
				if member.SourceKey == alias.SourceKey {
					t.Fatalf("unusable refinement alias supplied support: %+v", group)
				}
			}
		})
	}
}

func TestCalibratedVisualWitnessRejectsPairwiseOnlyGeometrySupport(t *testing.T) {
	cohort := visualSequenceFixture()
	shift := func(episode Episode, amount int) Episode {
		view := episode
		view.Visual = append([]VisualSample(nil), episode.Visual...)
		for i := range view.Visual {
			for cell, value := range view.Visual[i].Luma {
				sign := 1
				if (cell/8+cell%8)%2 != 0 {
					sign = -1
				}
				view.Visual[i].Luma[cell] = int8(int(value) + sign*amount)
			}
		}
		return view
	}
	// These are pre-rendered calibration views. Each individual pair has a
	// valid witness, but B's and C's transforms cannot both be interpreted
	// relative to A. A 12-level difference passes both RMS gates; a 24-level
	// difference fails. No clock or interval mismatch causes the rejection.
	anchor := cohort.Episodes[0]
	bAtAnchor := shift(cohort.Episodes[1], 12)
	cAtAnchor := shift(cohort.Episodes[2], -12)
	bNeutral := shift(cohort.Episodes[1], 48)
	cAtB := shift(cohort.Episodes[2], 48)
	for _, pair := range []struct {
		left, right Episode
		offset      int64
		leftStart   int64
	}{
		{anchor, bAtAnchor, 2 * TicksPerSecond, 5 * TicksPerSecond},
		{anchor, cAtAnchor, 4 * TicksPerSecond, 5 * TicksPerSecond},
		{bNeutral, cAtB, 2 * TicksPerSecond, 7 * TicksPerSecond},
	} {
		left := Interval{pair.leftStart, pair.leftStart + 16*TicksPerSecond}
		right := Interval{left.StartTicks + pair.offset, left.EndTicks + pair.offset}
		metrics, ok, err := sequenceMeasure(pair.left, pair.right, pair.offset, left, right, v2TestBudget())
		if err != nil || !ok || metrics.PairCount != 1 {
			t.Fatalf("individually calibrated pair lost its complete witness: %+v, %v, %v", metrics, ok, err)
		}
	}
	views := []Episode{anchor, bAtAnchor, cAtAnchor}
	clocks := map[int]int64{0: 0, 1: 2 * TicksPerSecond, 2: 4 * TicksPerSecond}
	window := Interval{5 * TicksPerSecond, 21 * TicksPerSecond}
	group, ok, err := sequenceGroupWitness(views, []int{0, 1, 2}, clocks, window, v2TestBudget())
	if err != nil || ok || len(group.Members) != 0 {
		t.Fatalf("pair-specific geometries were spliced into one calibrated clique: %+v, %v, %v", group, ok, err)
	}
	// A coherent replacement passes the identical duration, clock, state,
	// coverage, and complete-clique checks, excluding a vacuous negative.
	views[2] = shift(cohort.Episodes[2], 12)
	group, ok, err = sequenceGroupWitness(views, []int{0, 1, 2}, clocks, window, v2TestBudget())
	if err != nil || !ok || len(group.Members) != 3 || group.Metrics.PairCount != 3 {
		t.Fatalf("coherent fixed geometry failed the complete witness: %+v, %v, %v", group, ok, err)
	}
}

func TestCalibratedVisualSequencesCancellationAndLateBudgetReturnNoPartialGroups(t *testing.T) {
	cohort, bounds := calibratedSequenceFixture()
	options := DefaultOptions()
	complete, err := DiscoverVisualSequences(context.Background(), cohort, options)
	if err != nil {
		t.Fatal(err)
	}
	requireCalibratedFixtureGroup(t, complete, bounds)
	options.MaxComparisons = complete.Comparisons - 1
	result, err := DiscoverVisualSequences(context.Background(), cohort, options)
	if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(result, VisualSequenceResult{}) {
		t.Fatalf("late exhaustion returned partial calibrated evidence: %+v, %v", result, err)
	}
	base, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Input validation consults this context before the search. The delayed
	// cancellation reaches actual raster processing rather than entry checks.
	during := &cancelDuringContext{Context: base, cancel: cancel, after: 30}
	result, err = DiscoverVisualSequences(during, cohort, DefaultOptions())
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, VisualSequenceResult{}) {
		t.Fatalf("mid-search cancellation returned partial calibrated evidence: %+v, %v", result, err)
	}
}

func calibratedCompetingModeFixture() (Cohort, map[string]Interval) {
	cohort, bounds := calibratedSequenceFixture()
	coarse := visualSequenceFixture()
	for i := range cohort.Episodes {
		cohort.Episodes[i].DurationTicks = 300 * TicksPerSecond
		cohort.Episodes[i].Visual = append([]VisualSample(nil), coarse.Episodes[i].Visual...)
		for j := range cohort.Episodes[i].Visual {
			// This independently valid coarse opening is disjoint from the
			// retained refinement opening on every source timeline.
			cohort.Episodes[i].Visual[j].Ticks += 30 * TicksPerSecond
		}
	}
	return cohort, bounds
}

func TestCalibratedVisualModesPreserveJointAndExclusiveRefinementAuthority(t *testing.T) {
	t.Run("joint", func(t *testing.T) {
		cohort := testCohort()
		before := analyzeTest(t, cohort)
		if len(before.Groups) == 0 || before.Groups[0].VisualEvidence != nil || before.Groups[0].Status != Qualified {
			t.Fatalf("joint fixture has no original qualified audiovisual witness: %+v", before)
		}
		refinement, _ := calibratedSequenceFixture()
		for i := range cohort.Episodes {
			cohort.Episodes[i].Refinement = refinement.Episodes[i].Refinement
		}
		after := analyzeTest(t, cohort)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("available refinement changed an already qualified joint result")
		}
	})
	for _, kind := range []string{"calibrated_match", "no_calibrated_match"} {
		t.Run(kind, func(t *testing.T) {
			cohort, bounds := calibratedCompetingModeFixture()
			legacy := cohort
			legacy.Episodes = append([]Episode(nil), cohort.Episodes...)
			for i := range legacy.Episodes {
				legacy.Episodes[i].Refinement = nil
			}
			coarse, err := DiscoverVisualSequences(context.Background(), legacy, DefaultOptions())
			if err != nil || len(coarse.Groups) != 1 || coarse.Groups[0].Metrics.MeasurementPolicy != VisualMeasurementCoarse {
				t.Fatalf("competing coarse fixture has no valid original witness: %+v, %v", coarse, err)
			}
			for _, member := range coarse.Groups[0].Members {
				if member.Interval.StartTicks <= bounds[member.EpisodeKey].EndTicks {
					t.Fatalf("coarse fixture does not compete with the refinement interval: %+v", member)
				}
			}
			if kind == "no_calibrated_match" {
				// The third source still has usable, profile-compatible raster
				// observations, but no shared opening. Mode choice must precede
				// successful discovery rather than rewarding a coarse result.
				rng := rand.New(rand.NewSource(1011))
				for i := range cohort.Episodes[2].Refinement {
					cohort.Episodes[2].Refinement[i].Raster = calibratedFixtureRaster(rng)
				}
			}
			result := analyzeTest(t, cohort)
			if kind == "no_calibrated_match" {
				if len(result.Groups) != 0 {
					t.Fatalf("coarse success replaced an eligible but unmatched refinement population: %+v", result.Groups)
				}
				return
			}
			if len(result.Groups) != 1 || result.Groups[0].VisualEvidence == nil ||
				result.Groups[0].VisualEvidence.MeasurementPolicy != VisualMeasurementCalibrated || len(result.Groups[0].VisualEvidence.CalibrationDigest) != 64 {
				t.Fatalf("same-source coarse evidence competed with calibrated publication: %+v", result.Groups)
			}
			for _, episode := range result.Episodes {
				want := bounds[episode.EpisodeKey]
				if episode.Status != Qualified || len(episode.Candidates) != 1 ||
					episode.Candidates[0].Interval.StartTicks < want.StartTicks || episode.Candidates[0].Interval.EndTicks > want.EndTicks {
					t.Fatalf("a disjoint coarse opening hid the calibrated candidate: %+v", episode)
				}
			}
		})
	}
}

func TestCalibratedVisualModesAdmitCoarseOnlyForEligibleSourcePopulation(t *testing.T) {
	for _, count := range []int{2, 3} {
		name := "two_unrefined"
		if count == 3 {
			name = "three_unrefined"
		}
		t.Run(name, func(t *testing.T) {
			cohort, _ := calibratedSequenceFixture()
			coarse := visualSequenceFixture()
			cohort.Episodes = append(cohort.Episodes, coarse.Episodes[:count]...)
			result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			policies := map[string]int{}
			for _, group := range result.Groups {
				policies[group.Metrics.MeasurementPolicy]++
				if group.Metrics.MeasurementPolicy == VisualMeasurementCoarse {
					if len(group.Members) != 3 {
						t.Fatalf("coarse subset lacks three independent sources: %+v", group)
					}
					for _, member := range group.Members {
						if member.SourceKey != "A" && member.SourceKey != "B" && member.SourceKey != "C" {
							t.Fatalf("coarse subset borrowed a refined source: %+v", group)
						}
					}
				}
			}
			if policies[VisualMeasurementCalibrated] != 1 || policies[VisualMeasurementCoarse] != count-2 || len(result.Groups) != count-1 {
				t.Fatalf("mixed population selected the wrong discovery modes: %+v", result)
			}
		})
	}
	for _, kind := range []string{"missing", "flat", "incompatible_profile"} {
		t.Run(kind, func(t *testing.T) {
			cohort := visualSequenceFixture()
			refinement, _ := calibratedSequenceFixture()
			for i := 0; i < 2; i++ {
				cohort.Episodes[i].Refinement = refinement.Episodes[i].Refinement
			}
			switch kind {
			case "flat":
				for _, sample := range refinement.Episodes[2].Refinement {
					cohort.Episodes[2].Refinement = append(cohort.Episodes[2].Refinement, RefinementSample{Ticks: sample.Ticks})
				}
			case "incompatible_profile":
				// Three usable raster sources in total are insufficient when
				// only two share a profile. The unrefined third source keeps
				// the original coarse profile's positive control complete.
				other := refinement.Episodes[2]
				other.AlgorithmProfile = "other-refinement-profile"
				cohort.Episodes = append(cohort.Episodes, other)
			}
			result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
			if err != nil || len(result.Groups) != 1 || result.Groups[0].Metrics.MeasurementPolicy != VisualMeasurementCoarse {
				t.Fatalf("insufficient compatible refinement prevented complete coarse discovery: %+v, %v", result, err)
			}
			members := result.Groups[0].Members
			if len(members) != 3 || members[0].SourceKey != "A" || members[1].SourceKey != "B" || members[2].SourceKey != "C" {
				t.Fatalf("coarse fallback used a different or incomplete population: %+v", members)
			}
		})
	}
}

func TestCalibratedVisualModesDoNotReplaceFailureWithCoarseSuccess(t *testing.T) {
	cohort, _ := calibratedCompetingModeFixture()
	legacy := cohort
	legacy.Episodes = append([]Episode(nil), cohort.Episodes...)
	for i := range legacy.Episodes {
		legacy.Episodes[i].Refinement = nil
	}
	coarse, err := DiscoverVisualSequences(context.Background(), legacy, DefaultOptions())
	if err != nil || len(coarse.Groups) != 1 {
		t.Fatalf("coarse control did not succeed: %+v, %v", coarse, err)
	}
	options := DefaultOptions()
	options.MaxComparisons = coarse.Comparisons + 1
	control, err := DiscoverVisualSequences(context.Background(), legacy, options)
	if err != nil || len(control.Groups) != 1 {
		t.Fatalf("the chosen failure budget cannot preserve its coarse control: %+v, %v", control, err)
	}
	// This budget is sufficient for the old result. Once the calibrated
	// population is selected, its budget failure must remain a failure.
	result, err := DiscoverVisualSequences(context.Background(), cohort, options)
	if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(result, VisualSequenceResult{}) {
		t.Fatalf("refinement failure was replaced by a coarse success: %+v, %v", result, err)
	}
}

func TestCalibratedPairCachePreservesDirectedHypothesesAndCallerIsolation(t *testing.T) {
	cohort := visualSequenceFixture()
	a, b := cohort.Episodes[0], cohort.Episodes[1]
	geometry := calibratedGeometry{ScaleYPermille: 1000}
	options := DefaultOptions()
	baseline, err := calibratedPairHypotheses(a, b, options, v2TestBudget())
	if err != nil || len(baseline) == 0 {
		t.Fatalf("uncached pair has no complete hypothesis: %+v, %v", baseline, err)
	}
	cache := newCalibratedPairCache()
	first, err := cache.get(a, b, geometry, geometry, options, v2TestBudget())
	if err != nil || !reflect.DeepEqual(first, baseline) {
		t.Fatalf("cold cache changed complete hypotheses: %+v, %v", first, err)
	}
	// Group construction assigns source indexes after discovery. It must not
	// change the cached value used by another anchor or a later caller.
	first[0].left, first[0].right = 91, 92
	first[0].a.StartTicks++
	first[0].metrics.Samples = 0
	hitBudget := v2TestBudget()
	reused, err := cache.get(a, b, geometry, geometry, options, hitBudget)
	if err != nil || !reflect.DeepEqual(reused, baseline) {
		t.Fatalf("caller mutation escaped into the cached witness: %+v, %v", reused, err)
	}
	reverse, err := calibratedPairHypotheses(b, a, options, v2TestBudget())
	if err != nil || len(reverse) == 0 {
		t.Fatalf("reverse control has no complete hypothesis: %+v, %v", reverse, err)
	}
	reverseBudget := v2TestBudget()
	reversed, err := cache.get(b, a, geometry, geometry, options, reverseBudget)
	if err != nil || !reflect.DeepEqual(reversed, reverse) || reverseBudget.used <= hitBudget.used {
		t.Fatalf("cache treated directed discovery as a symmetric operation: %+v, %v", reversed, err)
	}
	// A different rendered geometry of the same source needs a separate key.
	// A successful empty scan is reusable and cannot overwrite the first view.
	otherView := b
	otherView.Visual = append([]VisualSample(nil), b.Visual...)
	for i := range otherView.Visual {
		otherView.Visual[i].Hash = 0
	}
	otherGeometry := geometry
	otherGeometry.ShiftXPermille = 10
	emptyBudget := v2TestBudget()
	empty, err := cache.get(a, otherView, geometry, otherGeometry, options, emptyBudget)
	if err != nil || len(empty) != 0 {
		t.Fatalf("a different source geometry borrowed a cached match: %+v, %v", empty, err)
	}
	emptyHitBudget := v2TestBudget()
	empty, err = cache.get(a, otherView, geometry, otherGeometry, options, emptyHitBudget)
	if err != nil || len(empty) != 0 || emptyHitBudget.used >= emptyBudget.used {
		t.Fatalf("complete empty hypotheses were not cached: %+v, %v", empty, err)
	}
	reused, err = cache.get(a, b, geometry, geometry, options, v2TestBudget())
	if err != nil || !reflect.DeepEqual(reused, baseline) {
		t.Fatalf("another geometry changed the original cached witness: %+v, %v", reused, err)
	}
}

func TestCalibratedPairCacheFailsWithoutPartialOrCachedErrors(t *testing.T) {
	cohort := visualSequenceFixture()
	a, b := cohort.Episodes[0], cohort.Episodes[1]
	geometry := calibratedGeometry{ScaleYPermille: 1000}
	options := DefaultOptions()
	baseline, err := calibratedPairHypotheses(a, b, options, v2TestBudget())
	if err != nil || len(baseline) == 0 {
		t.Fatalf("uncached failure control has no complete hypothesis: %+v, %v", baseline, err)
	}
	cache := newCalibratedPairCache()
	limited := &workBudget{ctx: context.Background(), limit: 1}
	partial, err := cache.get(a, b, geometry, geometry, options, limited)
	if !errors.Is(err, ErrLimit) || partial != nil {
		t.Fatalf("cache miss returned partial evidence after exhaustion: %+v, %v", partial, err)
	}
	complete, err := cache.get(a, b, geometry, geometry, options, v2TestBudget())
	if err != nil || !reflect.DeepEqual(complete, baseline) {
		t.Fatalf("an earlier failed scan was cached: %+v, %v", complete, err)
	}
	// One unit admits the lookup but cannot finish copying its first result.
	limited = &workBudget{ctx: context.Background(), limit: 1}
	partial, err = cache.get(a, b, geometry, geometry, options, limited)
	if !errors.Is(err, ErrLimit) || partial != nil {
		t.Fatalf("cache hit returned a partial copy after exhaustion: %+v, %v", partial, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	partial, err = cache.get(a, b, geometry, geometry, options, &workBudget{ctx: ctx, limit: options.MaxComparisons})
	if !errors.Is(err, context.Canceled) || partial != nil {
		t.Fatalf("short cache hit ignored cancellation: %+v, %v", partial, err)
	}
	complete, err = cache.get(a, b, geometry, geometry, options, v2TestBudget())
	if err != nil || !reflect.DeepEqual(complete, baseline) {
		t.Fatalf("a canceled or exhausted hit damaged complete cached evidence: %+v, %v", complete, err)
	}
}

func TestCalibratedStateMemoRequiresBothHashesAndRetainsExactCounts(t *testing.T) {
	cohort := visualSequenceFixture()
	a, b := cohort.Episodes[0], cohort.Episodes[1]
	for i := 0; i < 33; i++ {
		// All left hashes are equal, but five right hashes are mutually
		// farther than the state radius. Every cross-source pair remains
		// within the match gate and retains identical luminance evidence.
		a.Visual[10+i].Hash = 0
		b.Visual[14+i].Hash = ((uint64(1) << 12) - 1) << uint(12*(i%5))
	}
	want := VisualSequenceMetrics{
		MeasurementPolicy:     VisualMeasurementCoarse,
		Samples:               33,
		CoveragePermille:      1000,
		DistinctStates:        5,
		DominantStatePermille: 7 * 1000 / 33,
		MaxGapTicks:           TicksPerSecond / 2,
		PairCount:             1,
	}
	for _, reversed := range []bool{false, true} {
		left, right := a, b
		offset := 2 * TicksPerSecond
		ar, br := Interval{5 * TicksPerSecond, 21 * TicksPerSecond}, Interval{7 * TicksPerSecond, 23 * TicksPerSecond}
		if reversed {
			left, right, ar, br, offset = right, left, br, ar, -offset
		}
		metrics, ok, err := sequenceMeasure(left, right, offset, ar, br, v2TestBudget())
		// One side never moves, so diversity alone must not qualify the
		// interval. The measured five-state distribution is still exact.
		if err != nil || ok || metrics != want {
			t.Fatalf("exact-pair memo changed state counts or another metric: got %+v, want %+v, accepted %v, error %v", metrics, want, ok, err)
		}
	}
}

func TestCalibratedPeriodicBoundsPreserveExactNinetyPercentBoundary(t *testing.T) {
	const count = 32
	hashes := []uint64{0x00000000ffffffff, 0xffffffff00000000, 0xaaaaaaaaaaaaaaaa, 0x5555555555555555, 0x3333333333333333}
	fixture := func(extraFailure bool) (Episode, Episode, [][2]int) {
		a := Episode{}
		matches := make([][2]int, count)
		for i := 0; i < count; i++ {
			state := i % 2
			if i == 0 {
				state = 2
			}
			if i == 10 {
				state = 3
			}
			if extraFailure && i == count-1 {
				state = 4
			}
			sample := VisualSample{Ticks: int64(i) * TicksPerSecond / 2, Hash: hashes[state], Contrast: 200, LumaKnown: true}
			for cell := range sample.Luma {
				if cell%2 == 0 {
					sample.Luma[cell] = 32
				} else {
					sample.Luma[cell] = -32
				}
			}
			a.Visual = append(a.Visual, sample)
			matches[i] = [2]int{i, i}
		}
		b := a
		b.Visual = append([]VisualSample(nil), a.Visual...)
		return a, b, matches
	}
	// Lag two has exactly 27 matches among 30 comparisons. Moving one more
	// endpoint to a distinct state drops it below 90%; every longer allowed
	// lag is also below the same threshold for this finite sequence.
	a, b, matches := fixture(false)
	periodic, err := sequencePeriodic(a, b, matches, v2TestBudget())
	if err != nil || !periodic {
		t.Fatalf("the exact 90-percent recurrence was excluded: %v, %v", periodic, err)
	}
	a, b, matches = fixture(true)
	budget := v2TestBudget()
	periodic, err = sequencePeriodic(a, b, matches, budget)
	if err != nil || periodic {
		t.Fatalf("a sub-threshold recurrence was promoted by early termination: %v, %v", periodic, err)
	}
	var exhaustiveComparisons int64
	for lag := 2; lag <= min(16, len(matches)/3); lag++ {
		exhaustiveComparisons += int64(len(matches) - lag)
	}
	if budget.used >= exhaustiveComparisons {
		t.Fatalf("impossible lags still consumed their complete comparison budget: %d >= %d", budget.used, exhaustiveComparisons)
	}
	periodic, err = sequencePeriodic(a, b, matches, &workBudget{ctx: context.Background(), limit: 1})
	if !errors.Is(err, ErrLimit) || periodic {
		t.Fatalf("periodic bound swallowed budget exhaustion: %v, %v", periodic, err)
	}
}
