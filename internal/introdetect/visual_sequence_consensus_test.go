package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"math/bits"
	"math/rand"
	"reflect"
	"testing"
)

type visualConsensusTestFixture struct {
	episodes   []Episode
	views      []Episode
	clocks     map[int]int64
	geometries map[int]calibratedGeometry
}

func visualConsensusFixture(t *testing.T, episodes []Episode) visualConsensusTestFixture {
	t.Helper()
	fixture := visualConsensusTestFixture{
		episodes:   episodes,
		views:      make([]Episode, len(episodes)),
		clocks:     map[int]int64{},
		geometries: map[int]calibratedGeometry{},
	}
	for index, episode := range episodes {
		geometry := calibratedGeometry{ScaleYPermille: 1000}
		view, err := calibratedView(episode, geometry, v2TestBudget())
		if err != nil {
			t.Fatal(err)
		}
		fixture.views[index] = view
		fixture.clocks[index] = int64(index) * 7 * TicksPerSecond / 10
		fixture.geometries[index] = geometry
	}
	return fixture
}

func (fixture visualConsensusTestFixture) witness(t *testing.T, window Interval, clocks map[int]int64) VisualSequenceGroup {
	t.Helper()
	if clocks == nil {
		clocks = fixture.clocks
	}
	rawWindow := Interval{window.StartTicks - visualSequenceResidual, window.EndTicks + visualSequenceResidual}
	raw, ok, err := sequenceGroupWitness(fixture.views, []int{0, 1, 2}, clocks, rawWindow, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("raw consensus control has no complete witness: %+v, %v, %v", raw, ok, err)
	}
	guarded, ok, err := calibratedGuardedWitness(raw, fixture.views, v2TestBudget())
	if err != nil || !ok || guarded.Members[0].Interval != window {
		t.Fatalf("guarded consensus control changed its intended window: %+v, %v, %v", guarded, ok, err)
	}
	guarded.Calibration = calibratedAuditForGroup(guarded, 0, fixture.views, fixture.geometries)
	guarded.Metrics.MeasurementPolicy = VisualMeasurementCalibrated
	guarded.Metrics.CalibrationDigest = visualCalibrationDigest(guarded)
	return guarded
}

func (fixture visualConsensusTestFixture) crossing(t *testing.T) []VisualSequenceGroup {
	t.Helper()
	return []VisualSequenceGroup{
		fixture.witness(t, Interval{33 * TicksPerSecond / 10, 121 * TicksPerSecond / 10}, nil),
		fixture.witness(t, Interval{38 * TicksPerSecond / 10, 126 * TicksPerSecond / 10}, nil),
	}
}

func cloneVisualConsensusGroups(groups []VisualSequenceGroup) []VisualSequenceGroup {
	result := append([]VisualSequenceGroup(nil), groups...)
	for index := range result {
		result[index].Members = append([]Support(nil), groups[index].Members...)
		if groups[index].Calibration != nil {
			audit := *groups[index].Calibration
			audit.Members = append([]calibratedMemberAudit(nil), audit.Members...)
			audit.ScaleYGrid = append([]int(nil), audit.ScaleYGrid...)
			result[index].Calibration = &audit
		}
	}
	return result
}

func TestVisualConsensusRemeasuresCrossingOpeningWithoutShrinkingGuardAgain(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	fixture := visualConsensusFixture(t, cohort.Episodes)
	groups := fixture.crossing(t)
	before, _ := json.Marshal(groups)
	selection := map[string]string{fixture.episodes[0].SourceKey: ""}
	recovered, err := recoverVisualConsensus(groups, fixture.episodes, selection, v2TestBudget())
	if err != nil || len(recovered) == 0 {
		t.Fatalf("crossing observations of one opening were not recovered: %+v, %v", recovered, err)
	}
	window := Interval{38 * TicksPerSecond / 10, 121 * TicksPerSecond / 10}
	want, ok, err := sequenceGroupWitness(fixture.views, []int{0, 1, 2}, fixture.clocks, window, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("independent consensus control failed: %+v, %v, %v", want, ok, err)
	}
	want.Metrics.MeasurementPolicy = VisualMeasurementCalibrated
	want.Metrics.CalibrationDigest = groups[0].Metrics.CalibrationDigest
	for _, group := range recovered {
		if !reflect.DeepEqual(group.Members, want.Members) || group.Metrics != want.Metrics || !group.boundaryGuarded ||
			!reflect.DeepEqual(group.Calibration, groups[0].Calibration) || group.Metrics.PairCount != 3 {
			t.Fatalf("recovery changed clocks, reused old metrics, or applied the guard again: got %+v, want %+v", group, want)
		}
		if group.Metrics.Samples >= groups[0].Metrics.Samples || group.Metrics.CalibrationDigest != visualCalibrationDigest(group) {
			t.Fatal("cropped evidence lost its independent measurement or fixed calibration identity")
		}
		prior, priorOK := visualPublicationGroup(groups[0], cohort.Key, fixture.episodes, DefaultOptions())
		published, publishedOK := visualPublicationGroup(group, cohort.Key, fixture.episodes, DefaultOptions())
		if !priorOK || !publishedOK || published.ID == prior.ID {
			t.Fatal("new measured intervals did not receive a distinct publication identity")
		}
	}
	after, _ := json.Marshal(groups)
	if string(before) != string(after) {
		t.Fatal("consensus recovery mutated an original witness")
	}
	groups[0], groups[1] = groups[1], groups[0]
	reversed, err := recoverVisualConsensus(groups, fixture.episodes, selection, v2TestBudget())
	if err != nil || !reflect.DeepEqual(recovered, reversed) {
		t.Fatalf("original candidate order changed the recovered evidence: %+v, %v", reversed, err)
	}
	unchanged, err := recoverVisualConsensus(groups, fixture.episodes, map[string]string{fixture.episodes[0].SourceKey: "existing-group"}, v2TestBudget())
	if err != nil || len(unchanged) != 0 {
		t.Fatalf("an unambiguous source was needlessly recovered: %+v, %v", unchanged, err)
	}
}

func TestVisualConsensusKeepsExactEightSecondMinimum(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	fixture := visualConsensusFixture(t, cohort.Episodes)
	for _, shorter := range []int64{0, 1} {
		groups := []VisualSequenceGroup{
			fixture.witness(t, Interval{33 * TicksPerSecond / 10, 118*TicksPerSecond/10 - shorter}, nil),
			fixture.witness(t, Interval{38 * TicksPerSecond / 10, 126 * TicksPerSecond / 10}, nil),
		}
		recovered, err := recoverVisualConsensus(groups, fixture.episodes, map[string]string{fixture.episodes[0].SourceKey: ""}, v2TestBudget())
		if err != nil || (len(recovered) > 0) != (shorter == 0) {
			t.Fatalf("minimum-duration consensus changed at %d ticks below eight seconds: %+v, %v", shorter, recovered, err)
		}
		for _, group := range recovered {
			for _, member := range group.Members {
				if member.Interval.EndTicks-member.Interval.StartTicks != visualSequenceMinimum {
					t.Fatalf("exact minimum was widened or guarded twice: %+v", member)
				}
			}
		}
	}
}

func TestVisualConsensusRetainsEveryDisjointCandidateAsCounterevidence(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	rng := rand.New(rand.NewSource(4901))
	for source := range cohort.Episodes {
		episode := &cohort.Episodes[source]
		episode.DurationTicks = 80 * TicksPerSecond
		for frame := len(episode.Refinement); frame < 400; frame++ {
			episode.Refinement = append(episode.Refinement, RefinementSample{Ticks: int64(frame) * calibratedFixtureStep, Raster: calibratedFixtureRaster(rng)})
		}
		for frame := 0; frame < 100; frame++ {
			episode.Refinement[230+source*7+frame].Raster = episode.Refinement[30+source*7+frame].Raster
		}
	}
	fixture := visualConsensusFixture(t, cohort.Episodes)
	groups := append(fixture.crossing(t), fixture.witness(t, Interval{233 * TicksPerSecond / 10, 326 * TicksPerSecond / 10}, nil))
	for _, invalidAudit := range []bool{false, true} {
		input := cloneVisualConsensusGroups(groups)
		if invalidAudit {
			// A failed later remeasurement must not remove its interval from
			// the original source intersection and rescue an earlier result.
			input[2].Calibration = nil
		}
		recovered, err := recoverVisualConsensus(input, fixture.episodes, map[string]string{fixture.episodes[0].SourceKey: ""}, v2TestBudget())
		if err != nil || len(recovered) != 0 {
			t.Fatalf("disjoint counterevidence disappeared when audit invalid=%v: %+v, %v", invalidAudit, recovered, err)
		}
	}
}

func TestVisualConsensusRejectsWeakCommonInteriorDespiteQualifiedParents(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	states := make([][256]byte, 0, 5)
	descriptors := []VisualSample{}
	stateRNG := rand.New(rand.NewSource(1931))
	for attempt := 0; attempt < 1000 && len(states) < 5; attempt++ {
		raster := calibratedFixtureRaster(stateRNG)
		sample := calibratedRender(RefinementSample{Raster: raster}, calibratedGeometry{ScaleYPermille: 1000})
		distinct := sequenceUsable(sample)
		for _, prior := range descriptors {
			distinct = distinct && bits.OnesCount64(sample.Hash^prior.Hash) > 8
		}
		if distinct {
			states = append(states, raster)
			descriptors = append(descriptors, sample)
		}
	}
	if len(states) != 5 {
		t.Fatal("weak-interior fixture could not form five distinct observed states")
	}
	rng := rand.New(rand.NewSource(7019))
	opening := [100][256]byte{}
	for frame := range opening {
		state := rng.Intn(3)
		if frame < 6 {
			state = 3
		} else if frame >= 94 {
			state = 4
		}
		opening[frame] = states[state]
	}
	for source := range cohort.Episodes {
		for frame, raster := range opening {
			cohort.Episodes[source].Refinement[30+source*7+frame].Raster = raster
		}
	}
	fixture := visualConsensusFixture(t, cohort.Episodes)
	groups := []VisualSequenceGroup{
		fixture.witness(t, Interval{33 * TicksPerSecond / 10, 12 * TicksPerSecond}, nil),
		fixture.witness(t, Interval{39 * TicksPerSecond / 10, 126 * TicksPerSecond / 10}, nil),
	}
	window := Interval{39 * TicksPerSecond / 10, 12 * TicksPerSecond}
	right := Interval{window.StartTicks + fixture.clocks[1], window.EndTicks + fixture.clocks[1]}
	metrics, ok, err := sequenceMeasure(fixture.views[0], fixture.views[1], fixture.clocks[1], window, right, v2TestBudget())
	if err != nil || ok || metrics.DistinctStates != 3 || metrics.CoveragePermille < 850 {
		t.Fatalf("weak-interior control did not isolate the state gate: %+v, %v, %v", metrics, ok, err)
	}
	recovered, err := recoverVisualConsensus(groups, fixture.episodes, map[string]string{fixture.episodes[0].SourceKey: ""}, v2TestBudget())
	if err != nil || len(recovered) != 0 {
		t.Fatalf("qualified parent metrics authorized an unqualified common interior: %+v, %v", recovered, err)
	}
}

func TestVisualConsensusProjectsEveryMemberUsingEachOriginalClock(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	fixture := visualConsensusFixture(t, cohort.Episodes)
	phase := TicksPerSecond / 25
	later := map[int]int64{0: 0, 1: fixture.clocks[1] + phase, 2: fixture.clocks[2] + phase}
	earlier := map[int]int64{0: 0, 1: fixture.clocks[1] - phase, 2: fixture.clocks[2] - phase}
	groups := []VisualSequenceGroup{
		fixture.witness(t, Interval{33 * TicksPerSecond / 10, 118*TicksPerSecond/10 + phase}, nil),
		fixture.witness(t, Interval{38 * TicksPerSecond / 10, 126 * TicksPerSecond / 10}, later),
		fixture.witness(t, Interval{33 * TicksPerSecond / 10, 126 * TicksPerSecond / 10}, earlier),
	}
	// All three sources have at least eight seconds in their own clocks.
	// Only the first two complete clocks can project a shared eight seconds;
	// the third clock projects just 7.96 seconds despite its valid parent.
	recovered, err := recoverVisualConsensus(groups, fixture.episodes, map[string]string{fixture.episodes[0].SourceKey: ""}, v2TestBudget())
	if err != nil || len(recovered) != 2 {
		t.Fatalf("independent source intersections bypassed complete-clock projection: %+v, %v", recovered, err)
	}
	windows := []Interval{
		{38*TicksPerSecond/10 + phase, 118*TicksPerSecond/10 + phase},
		{38 * TicksPerSecond / 10, 118 * TicksPerSecond / 10},
	}
	for index, group := range recovered {
		if group.Members[0].Interval != windows[index] || !reflect.DeepEqual(group.Calibration, groups[index].Calibration) || group.Metrics.PairCount != 3 {
			t.Fatalf("consensus changed a fixed clock or ignored a member projection: %+v", group)
		}
		for position, member := range group.Members {
			if member.Interval.StartTicks-group.Members[0].Interval.StartTicks != groups[index].Calibration.Members[position].ClockOffsetTicks ||
				member.Interval.EndTicks-member.Interval.StartTicks != visualSequenceMinimum {
				t.Fatalf("member projection changed its original clock: %+v", member)
			}
		}
	}
}

func TestVisualConsensusRejectsMismatchedProfilesAndCalibration(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	fixture := visualConsensusFixture(t, cohort.Episodes)
	groups := fixture.crossing(t)
	for _, kind := range []string{"profile", "identity", "episode-alias", "content-alias", "missing-audit", "digest", "clock", "member", "anchor", "guard"} {
		t.Run(kind, func(t *testing.T) {
			input := cloneVisualConsensusGroups(groups)
			episodes := append([]Episode(nil), fixture.episodes...)
			if kind == "profile" {
				episodes[1].AlgorithmProfile = "different-profile"
			} else if kind == "identity" {
				episodes[1].ContentIdentity = "different-content"
			} else if kind == "episode-alias" {
				episodes[1].EpisodeKey = episodes[0].EpisodeKey
				for index := range input {
					input[index].Members[1].EpisodeKey = episodes[1].EpisodeKey
				}
			} else if kind == "content-alias" {
				episodes[1].ContentIdentity = episodes[0].ContentIdentity
				for index := range input {
					input[index].Members[1].ContentIdentity = episodes[1].ContentIdentity
				}
			} else {
				for index := range input {
					switch kind {
					case "missing-audit":
						input[index].Calibration = nil
					case "digest":
						input[index].Calibration.Members[1].Geometry.ShiftXPermille += 10
					case "clock":
						input[index].Calibration.Members[1].ClockOffsetTicks++
					case "member":
						input[index].Calibration.Members[1].SourceKey = "different-source"
					case "anchor":
						input[index].Calibration.AnchorSourceKey = input[index].Members[1].SourceKey
					case "guard":
						input[index].Calibration.BoundaryGuardTicks = 0
					}
					if kind != "digest" {
						input[index].Metrics.CalibrationDigest = visualCalibrationDigest(input[index])
					}
				}
			}
			recovered, err := recoverVisualConsensus(input, episodes, map[string]string{fixture.episodes[0].SourceKey: ""}, v2TestBudget())
			if err != nil || len(recovered) != 0 {
				t.Fatalf("mismatched %s authorized consensus evidence: %+v, %v", kind, recovered, err)
			}
		})
	}
}

func TestVisualConsensusBudgetAndCancellationReturnNoPartialEvidence(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	fixture := visualConsensusFixture(t, cohort.Episodes)
	groups := fixture.crossing(t)
	selection := map[string]string{fixture.episodes[0].SourceKey: ""}
	completeBudget := v2TestBudget()
	complete, err := recoverVisualConsensus(groups, fixture.episodes, selection, completeBudget)
	if err != nil || len(complete) == 0 || completeBudget.used <= 1 {
		t.Fatalf("budget control did not complete measured recovery: %+v, %v", complete, err)
	}
	for _, limit := range []int64{1, completeBudget.used - 1} {
		recovered, err := recoverVisualConsensus(groups, fixture.episodes, selection, &workBudget{ctx: context.Background(), limit: limit})
		if !errors.Is(err, ErrLimit) || len(recovered) != 0 {
			t.Fatalf("budget exhaustion returned partial consensus evidence: %+v, %v", recovered, err)
		}
	}
	for _, during := range []bool{false, true} {
		base, cancel := context.WithCancel(context.Background())
		var ctx context.Context = base
		if during {
			ctx = &cancelDuringContext{Context: base, cancel: cancel, after: 3}
		} else {
			cancel()
		}
		recovered, err := recoverVisualConsensus(groups, fixture.episodes, selection, &workBudget{ctx: ctx, limit: DefaultOptions().MaxComparisons})
		cancel()
		if !errors.Is(err, context.Canceled) || len(recovered) != 0 {
			t.Fatalf("cancellation returned partial consensus evidence: %+v, %v", recovered, err)
		}
	}
}
