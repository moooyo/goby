package introdetect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func calibratedBoundaryFixture(t *testing.T) ([]Episode, map[int]int64, Interval, map[string]int64) {
	t.Helper()
	cohort, openings := calibratedSequenceFixture()
	views := make([]Episode, len(cohort.Episodes))
	clocks := map[int]int64{0: 0, 1: 7 * TicksPerSecond / 10, 2: 14 * TicksPerSecond / 10}
	protectedStarts := make(map[string]int64)
	for source, episode := range cohort.Episodes {
		view, err := calibratedView(episode, calibratedGeometry{ScaleYPermille: 1000}, v2TestBudget())
		if err != nil {
			t.Fatal(err)
		}
		opening := openings[episode.EpisodeKey]
		// The frozen pair clocks leave 20 ms and 40 ms sampling residuals.
		// They do not change at the guard, and stay below half a dense slot.
		phase := int64(source) * TicksPerSecond / 50
		for i, sample := range view.Visual {
			if sample.Ticks < opening.StartTicks || sample.Ticks > opening.EndTicks {
				// Adjacent protected content is explicitly unmatched, not a
				// random descriptor that might accidentally extend the run.
				view.Visual[i] = VisualSample{Ticks: sample.Ticks}
			}
			view.Visual[i].Ticks += phase
		}
		views[source] = view
		protectedStarts[episode.SourceKey] = opening.EndTicks + phase + calibratedFixtureStep/2
	}
	anchor := openings[cohort.Episodes[0].EpisodeKey]
	// The admitted raw boundary includes 100 ms of temporal uncertainty. On
	// each source it passes the fixed-witness gates but crosses the next
	// protected-content boundary. No label is used by the guard itself.
	window := Interval{anchor.StartTicks - calibratedFixtureStep, anchor.EndTicks + calibratedFixtureStep}
	return views, clocks, window, protectedStarts
}

func TestCalibratedGuardShrinksCommonWindowOnceAndRemeasuresEveryPair(t *testing.T) {
	views, clocks, window, protectedStarts := calibratedBoundaryFixture(t)
	raw, ok, err := sequenceGroupWitness(views, []int{0, 1, 2}, clocks, window, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("raw fixed-clock witness is not valid: %+v, %v, %v", raw, ok, err)
	}
	before := raw
	before.Members = append([]Support(nil), raw.Members...)
	for _, member := range raw.Members {
		if member.Interval.EndTicks <= protectedStarts[member.SourceKey] {
			t.Fatalf("raw fixture does not cross its adjacent protected boundary: %+v", member)
		}
	}
	inner := Interval{window.StartTicks + visualSequenceResidual, window.EndTicks - visualSequenceResidual}
	want, ok, err := sequenceGroupWitness(views, []int{0, 1, 2}, clocks, inner, v2TestBudget())
	if err != nil || !ok || want.Metrics == raw.Metrics {
		t.Fatalf("independent inner measurement does not change valid evidence: raw %+v, inner %+v, %v, %v", raw.Metrics, want.Metrics, ok, err)
	}
	guarded, ok, err := calibratedGuardedWitness(raw, views, v2TestBudget())
	if err != nil || !ok || !reflect.DeepEqual(guarded.Members, want.Members) || guarded.Metrics != want.Metrics || guarded.Metrics.PairCount != 3 {
		t.Fatalf("guard reused old metrics or changed fixed source clocks: got %+v, want %+v, %v, %v", guarded, want, ok, err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatal("guard mutated the original admitted witness")
	}
	for source, member := range guarded.Members {
		prior := raw.Members[source]
		if member.Interval.StartTicks-prior.Interval.StartTicks != visualSequenceResidual ||
			prior.Interval.EndTicks-member.Interval.EndTicks != visualSequenceResidual || member.Interval.EndTicks > protectedStarts[member.SourceKey] {
			t.Fatalf("guard was multiplied per pair or entered protected content: raw %+v, guarded %+v", prior, member)
		}
	}
	again, ok, err := calibratedGuardedWitness(guarded, views, v2TestBudget())
	if err != nil || !ok || !reflect.DeepEqual(again, guarded) {
		t.Fatalf("guard shrank an already guarded common witness again: %+v, %v, %v", again, ok, err)
	}
}

func TestCalibratedGuardKeepsEightSecondMinimumAndRejectsShorterInterior(t *testing.T) {
	views, clocks, window, _ := calibratedBoundaryFixture(t)
	window.EndTicks = window.StartTicks + visualSequenceMinimum + 2*visualSequenceResidual
	raw, ok, err := sequenceGroupWitness(views, []int{0, 1, 2}, clocks, window, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("minimum-duration raw control has no valid witness: %+v, %v, %v", raw, ok, err)
	}
	guarded, ok, err := calibratedGuardedWitness(raw, views, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("an exactly eight-second guarded interior was rejected: %+v, %v, %v", guarded, ok, err)
	}
	for _, member := range guarded.Members {
		if member.Interval.EndTicks-member.Interval.StartTicks != visualSequenceMinimum {
			t.Fatalf("minimum boundary changed during common projection: %+v", member)
		}
	}
	short := raw
	short.Members = append([]Support(nil), raw.Members...)
	for source := range short.Members {
		short.Members[source].Interval.EndTicks--
	}
	rejected, ok, err := calibratedGuardedWitness(short, views, v2TestBudget())
	if err != nil || ok || !reflect.DeepEqual(rejected, VisualSequenceGroup{}) {
		t.Fatalf("guard admitted an interior one tick below the minimum: %+v, %v, %v", rejected, ok, err)
	}
}

func TestCalibratedGuardCannotReuseEvidenceAfterRemeasurementFails(t *testing.T) {
	views, clocks, window, _ := calibratedBoundaryFixture(t)
	raw, ok, err := sequenceGroupWitness(views, []int{0, 1, 2}, clocks, window, v2TestBudget())
	if err != nil || !ok {
		t.Fatalf("raw failure control has no valid witness: %+v, %v, %v", raw, ok, err)
	}
	corrupted := append([]Episode(nil), views...)
	corrupted[2].Visual = append([]VisualSample(nil), views[2].Visual...)
	for i := range corrupted[2].Visual {
		corrupted[2].Visual[i] = VisualSample{Ticks: corrupted[2].Visual[i].Ticks}
	}
	group, ok, err := calibratedGuardedWitness(raw, corrupted, v2TestBudget())
	if err != nil || ok || !reflect.DeepEqual(group, VisualSequenceGroup{}) {
		t.Fatalf("old metrics survived a failed complete-clique remeasurement: %+v, %v, %v", group, ok, err)
	}
	group, ok, err = calibratedGuardedWitness(raw, views, &workBudget{ctx: context.Background(), limit: 1})
	if !errors.Is(err, ErrLimit) || ok || !reflect.DeepEqual(group, VisualSequenceGroup{}) {
		t.Fatalf("guard returned partial evidence after budget exhaustion: %+v, %v, %v", group, ok, err)
	}
}

func TestCalibratedBoundaryGuardIsBoundIntoAuditDigest(t *testing.T) {
	cohort, bounds := calibratedSequenceFixture()
	result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	group := requireCalibratedFixtureGroup(t, result, bounds)
	if group.Calibration == nil || group.Calibration.BoundaryGuardTicks != visualSequenceResidual ||
		group.Metrics.CalibrationDigest == "" || group.Metrics.CalibrationDigest != visualCalibrationDigest(group) {
		t.Fatalf("guarded group omitted its boundary policy identity: %+v", group)
	}
	changed := group
	audit := *group.Calibration
	audit.BoundaryGuardTicks = 0
	changed.Calibration = &audit
	if visualCalibrationDigest(changed) == group.Metrics.CalibrationDigest {
		t.Fatal("removing the boundary guard preserved the calibrated audit digest")
	}
}
