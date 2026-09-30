// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

func contractEpisode(id string, points []uint32) Episode {
	return Episode{EpisodeKey: id, SourceKey: "source:" + id, ContentIdentity: "content:" + id, AlgorithmProfile: "test-profile", DurationTicks: 600 * TicksPerSecond, Fingerprint: points}
}

func contractCohort() Cohort {
	return Cohort{Key: "season", Episodes: []Episode{contractEpisode("a", filled(200, 17)), contractEpisode("b", filled(200, 17))}}
}

func TestOptionsDefaultsAndBounds(t *testing.T) {
	defaults := DefaultOptions()
	if defaults != (Options{25, 10, 15, 120, 6, 3.5, 2}) {
		t.Fatalf("defaults changed: %#v", defaults)
	}
	if err := ValidateOptions(defaults); err != nil {
		t.Fatal(err)
	}
	invalid := []Options{{}}
	for _, change := range []func(*Options){
		func(o *Options) { o.AnalysisPercent = 0 }, func(o *Options) { o.AnalysisPercent = 51 },
		func(o *Options) { o.AnalysisLengthLimit = 0 }, func(o *Options) { o.AnalysisLengthLimit = 11 },
		func(o *Options) { o.MinimumIntroDuration = 0 }, func(o *Options) { o.MinimumIntroDuration = 601 },
		func(o *Options) { o.MaximumIntroDuration = 14 }, func(o *Options) { o.MaximumIntroDuration = 601 },
		func(o *Options) { o.MaximumFingerprintPointDifferences = -1 }, func(o *Options) { o.MaximumFingerprintPointDifferences = 33 },
		func(o *Options) { o.MaximumTimeSkip = -1 }, func(o *Options) { o.MaximumTimeSkip = 30.1 },
		func(o *Options) { o.MaximumTimeSkip = math.NaN() }, func(o *Options) { o.MaximumTimeSkip = math.Inf(1) },
		func(o *Options) { o.InvertedIndexShift = -1 }, func(o *Options) { o.InvertedIndexShift = 33 },
	} {
		o := defaults
		change(&o)
		invalid = append(invalid, o)
	}
	for _, o := range invalid {
		if !errors.Is(ValidateOptions(o), ErrInvalidInput) {
			t.Fatalf("accepted invalid options: %#v", o)
		}
	}
	for _, o := range []Options{{1, 1, 1, 1, 0, 0, 0}, {50, 10, 600, 600, 32, 30, 32}} {
		if err := ValidateOptions(o); err != nil {
			t.Fatalf("rejected boundary: %#v: %v", o, err)
		}
	}
}

func TestFingerprintEndPreservesShortEpisodeBranchAndCap(t *testing.T) {
	for _, row := range []struct {
		duration int64
		want     float64
	}{
		{299 * TicksPerSecond, 299}, {300 * TicksPerSecond, 75},
		{2400 * TicksPerSecond, 600}, {4000 * TicksPerSecond, 600},
		{12345678901, 308.641972525},
	} {
		if got := FingerprintEndSeconds(row.duration, DefaultOptions()); got != row.want {
			t.Fatalf("horizon %d: got %.17g want %.17g", row.duration, got, row.want)
		}
	}
	o := DefaultOptions()
	o.AnalysisLengthLimit = 1
	o.AnalysisPercent = 50
	if got := FingerprintEndSeconds(299*TicksPerSecond, o); got != 60 {
		t.Fatalf("short source cap: %v", got)
	}
	o = DefaultOptions()
	o.AnalysisPercent = 29
	// This case distinguishes the upstream cached percentage ratio from
	// multiplying duration by the integer percentage before dividing.
	if bits := math.Float64bits(FingerprintEndSeconds(12345678901, o)); bits != 0x407660651f612a79 {
		t.Fatalf("percentage evaluation order changed: %016x", bits)
	}
}

func TestTickConversionUsesToEvenAndRejectsInvalid(t *testing.T) {
	for _, row := range []struct {
		seconds float64
		ticks   int64
	}{{0, 0}, {0.00000005, 0}, {0.00000015, 2}, {0.00000025, 2}, {0.00000035, 4}} {
		got, err := secondsToTicks(row.seconds)
		if err != nil || got != row.ticks {
			t.Fatalf("ticks %.17g: %d %v", row.seconds, got, err)
		}
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), float64(math.MaxInt64) / float64(TicksPerSecond)} {
		if _, err := secondsToTicks(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted invalid seconds: %.17g", value)
		}
	}
}

func TestAnalyzeContractAndPreservedOrder(t *testing.T) {
	cohort := contractCohort()
	cohort.Episodes[0], cohort.Episodes[1] = cohort.Episodes[1], cohort.Episodes[0]
	result, err := Analyze(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != Version || result.CohortKey != cohort.Key || result.Options != DefaultOptions() || len(result.Episodes) != 2 {
		t.Fatalf("result contract: %#v", result)
	}
	for i, e := range result.Episodes {
		if e.EpisodeKey != cohort.Episodes[i].EpisodeKey || e.Status != Qualified || e.Candidate == nil || len(e.Candidate.Support) != 2 {
			t.Fatalf("episode contract: %#v", e)
		}
		if e.Candidate.Support[0].EpisodeKey != "b" || e.Candidate.Support[1].EpisodeKey != "a" {
			t.Fatalf("sorted input/support: %#v", e)
		}
		if err := ValidateEpisodeResult(e, cohort.Episodes[i], cohort, DefaultOptions()); err != nil {
			t.Fatal(err)
		}
	}
}

func replacementCohort(secondLength int) Cohort {
	a, b, c := filled(900, 0x11111111), filled(900, 0x77777777), filled(900, 0xeeeeeeee)
	for i := 0; i < 200; i++ {
		a[i], b[80+i] = uint32(i+1)*4096, uint32(i+1)*4096
	}
	for i := 0; i < secondLength; i++ {
		b[350+i], c[450+i] = uint32(i+1000)*4096, uint32(i+1000)*4096
	}
	return Cohort{Key: "pair-provenance", Episodes: []Episode{contractEpisode("a", a), contractEpisode("b", b), contractEpisode("c", c)}}
}

func TestWinningSupportRetainsOriginalPairWhenPeerChanges(t *testing.T) {
	cohort := replacementCohort(300)
	o := DefaultOptions()
	o.MaximumFingerprintPointDifferences = 0
	o.InvertedIndexShift = 0
	result, err := Analyze(context.Background(), cohort, o)
	if err != nil {
		t.Fatal(err)
	}
	a, b := result.Episodes[0], result.Episodes[1]
	if a.Candidate == nil || b.Candidate == nil {
		t.Fatalf("missing candidates: %#v", result)
	}
	if a.Candidate.Support[1].EpisodeKey != "b" || a.Candidate.Support[1].Interval == b.Candidate.Interval {
		t.Fatalf("support replaced with peer final interval: %#v %#v", a, b)
	}
	expected, _ := secondsToTicks(80 * sampleDuration)
	if a.Candidate.Support[1].Interval.StartTicks != expected || b.Candidate.Support[0].EpisodeKey != "b" || b.Candidate.Support[1].EpisodeKey != "c" {
		t.Fatalf("incorrect selected pair: %#v %#v", a, b)
	}
}

func TestShorterOrEqualLaterCandidateKeepsOriginalSupport(t *testing.T) {
	for _, count := range []int{150, 200} {
		cohort := replacementCohort(count)
		o := DefaultOptions()
		o.MaximumFingerprintPointDifferences = 0
		o.InvertedIndexShift = 0
		result, err := Analyze(context.Background(), cohort, o)
		if err != nil {
			t.Fatal(err)
		}
		b := result.Episodes[1]
		if b.Candidate == nil || b.Candidate.Support[0].EpisodeKey != "a" || b.Candidate.Support[1].EpisodeKey != "b" {
			t.Fatalf("lost earlier pair for %d: %#v", count, b)
		}
	}
}

func TestAnalyzeRejectsUntrustedOrDuplicateIdentities(t *testing.T) {
	for _, modify := range []func(*Cohort){
		func(c *Cohort) { c.Key = "" }, func(c *Cohort) { c.Episodes = c.Episodes[:1] },
		func(c *Cohort) { c.Episodes[1].EpisodeKey = c.Episodes[0].EpisodeKey },
		func(c *Cohort) { c.Episodes[1].SourceKey = c.Episodes[0].SourceKey },
		func(c *Cohort) { c.Episodes[1].ContentIdentity = c.Episodes[0].ContentIdentity },
		func(c *Cohort) { c.Episodes[1].AlgorithmProfile = "different" },
		func(c *Cohort) { c.Episodes[1].DurationTicks = 0 },
		func(c *Cohort) { c.Episodes[1].Fingerprint = nil },
		func(c *Cohort) { c.Episodes[1].Fingerprint = filled(MaxFingerprintPoints+1, 17) },
		func(c *Cohort) { c.Episodes[1].SourceKey = " " },
		func(c *Cohort) { c.Episodes[1].SourceKey = "source\x00b" },
	} {
		cohort := contractCohort()
		modify(&cohort)
		result, err := Analyze(context.Background(), cohort, DefaultOptions())
		if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("accepted invalid input or returned partial result: %#v %v", result, err)
		}
	}
}

func TestValidateStoredResultBindsPairAndSourceScope(t *testing.T) {
	cohort := contractCohort()
	result, err := Analyze(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	cohort.Episodes[0].Fingerprint, cohort.Episodes[1].Fingerprint = nil, nil
	if err := ValidateEpisodeResult(result.Episodes[0], cohort.Episodes[0], cohort, DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*EpisodeResult){
		func(r *EpisodeResult) { r.SourceKey = "different" },
		func(r *EpisodeResult) { r.Candidate.UpstreamCommit = "different" },
		func(r *EpisodeResult) { r.Candidate.Support[1].SourceKey = "different" },
		func(r *EpisodeResult) { r.Candidate.Support[1].ContentIdentity = "different" },
		func(r *EpisodeResult) { r.Candidate.Support[1].AlgorithmProfile = "different" },
		func(r *EpisodeResult) { r.Candidate.Support[1] = r.Candidate.Support[0] },
		func(r *EpisodeResult) { r.Candidate.Support = r.Candidate.Support[:1] },
		func(r *EpisodeResult) { r.Candidate.Interval.StartTicks++ },
		func(r *EpisodeResult) {
			r.Candidate.Support[1].Interval.EndTicks = cohort.Episodes[1].DurationTicks + 1
		},
		func(r *EpisodeResult) { r.Status = NoResult },
		func(r *EpisodeResult) { r.Reasons = []string{NoRepeatedInterval} },
	} {
		copyResult := result.Episodes[0]
		candidate := *copyResult.Candidate
		candidate.Support = append([]Support(nil), candidate.Support...)
		copyResult.Candidate = &candidate
		change(&copyResult)
		if !errors.Is(ValidateEpisodeResult(copyResult, cohort.Episodes[0], cohort, DefaultOptions()), ErrInvalidInput) {
			t.Fatalf("accepted corrupt result: %#v", copyResult)
		}
	}
}

func TestAnalyzeReturnsNoResultForEmptyAdmittedFingerprints(t *testing.T) {
	cohort := contractCohort()
	cohort.Episodes[0].Fingerprint, cohort.Episodes[1].Fingerprint = []uint32{}, []uint32{}
	result, err := Analyze(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range result.Episodes {
		if e.Status != NoResult || e.Candidate != nil || !reflect.DeepEqual(e.Reasons, []string{NoRepeatedInterval}) {
			t.Fatalf("incorrect no-result: %#v", e)
		}
		if err := ValidateEpisodeResult(e, cohort.Episodes[i], cohort, DefaultOptions()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionFailureDoesNotBecomeNoResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Analyze(ctx, contractCohort(), DefaultOptions())
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("cancellation became output: %#v %v", result, err)
	}
	a := newAnalyzer(DefaultOptions())
	a.limit = 2
	candidates, _, err := a.findCandidates(context.Background(), []episode{{ID: "a", Fingerprint: filled(200, 17)}, {ID: "b", Fingerprint: filled(200, 17)}})
	if !errors.Is(err, ErrLimit) || candidates != nil {
		t.Fatalf("exhaustion became candidates: %#v %v", candidates, err)
	}
}

func TestAnalyzeAllowsUpstreamOversizedSnappedLeft(t *testing.T) {
	left, right := filled(1100, 0), filled(1100, ^uint32(0))
	for i := 0; i < 965; i++ {
		point := uint32(i+1)*4096 + 0x555
		left[30+i], right[60+i] = point, point
	}
	cohort := Cohort{Key: "rhs-only", Episodes: []Episode{contractEpisode("a", left), contractEpisode("b", right)}}
	result, err := Analyze(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Episodes[0].Candidate == nil || result.Episodes[0].Candidate.Interval.EndTicks <= 120*TicksPerSecond {
		t.Fatalf("added left maximum restriction: %#v", result)
	}
}
