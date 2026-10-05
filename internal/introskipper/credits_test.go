// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCreditsFingerprintWindowPreservesNativeTailCalculation(t *testing.T) {
	for _, row := range []struct {
		duration int64
		want     float64
	}{
		{20 * TicksPerSecond, 0},
		{449 * TicksPerSecond, 0},
		{450 * TicksPerSecond, 0},
		{451 * TicksPerSecond, 1},
		{1200 * TicksPerSecond, 750},
		// The binary64 division rounds before subtraction; this is one ULP
		// above the decimal tail start rounded only once as a Go constant.
		{12345678901, math.Nextafter(784.5678901, math.Inf(1))},
	} {
		if got := CreditsFingerprintStartSeconds(row.duration); math.Float64bits(got) != math.Float64bits(row.want) {
			t.Fatalf("credits tail %d: got %.17g want %.17g", row.duration, got, row.want)
		}
	}
}

func TestAnalyzeCreditsUsesEachAbsoluteTailAndPreservesIntro(t *testing.T) {
	cohort := contractCohort()
	cohort.Episodes[0].DurationTicks = 10001234567
	cohort.Episodes[1].DurationTicks = 12009876543
	before := append([]uint32(nil), cohort.Episodes[0].Fingerprint...)
	options := DefaultOptions()
	// Intro's maximum and extraction prefix controls do not restrict credits.
	options.MaximumIntroDuration = 15
	options.AnalysisPercent = 1
	options.AnalysisLengthLimit = 1
	result, err := AnalyzeCredits(context.Background(), cohort, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != CreditsVersion || result.CohortKey != cohort.Key || result.Options != options || len(result.Episodes) != 2 {
		t.Fatalf("unexpected credits result contract: %#v", result)
	}
	for i, e := range result.Episodes {
		if e.Status != Qualified || e.Candidate == nil || len(e.Candidate.Support) != 2 {
			t.Fatalf("missing candidate: %#v", e)
		}
		offset := CreditsFingerprintStartSeconds(cohort.Episodes[i].DurationTicks)
		want, err := segmentInterval(segment{Start: offset, End: offset + 199*sampleDuration})
		if err != nil || e.Candidate.Interval != want {
			t.Fatalf("wrong absolute interval: got %#v want %#v: %v", e.Candidate.Interval, want, err)
		}
		for j, support := range e.Candidate.Support {
			peerOffset := CreditsFingerprintStartSeconds(cohort.Episodes[j].DurationTicks)
			peerWant, err := segmentInterval(segment{Start: peerOffset, End: peerOffset + 199*sampleDuration})
			if err != nil || support.Interval != peerWant {
				t.Fatalf("support does not use its own source clock: %#v want %#v: %v", support, peerWant, err)
			}
		}
		if err := ValidateCreditsEpisodeResult(e, cohort.Episodes[i], cohort, options); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(cohort.Episodes[0].Fingerprint, before) {
		t.Fatal("credits matcher mutated original fingerprints")
	}
	intro, err := Analyze(context.Background(), cohort, options)
	if err != nil || intro.Version != Version || intro.Episodes[0].Status != NoResult || intro.Episodes[1].Status != NoResult {
		t.Fatalf("credits changed the intro maximum or version: %#v %v", intro, err)
	}
}

func TestCreditsMaximumUsesTruncatedRelativeWindowMinusOne(t *testing.T) {
	for _, row := range []struct {
		points int
		status string
	}{{123, Qualified}, {155, NoResult}} {
		cohort := contractCohort()
		for i := range cohort.Episodes {
			cohort.Episodes[i].DurationTicks = 20 * TicksPerSecond
			cohort.Episodes[i].Fingerprint = filled(row.points, 17)
		}
		result, err := AnalyzeCredits(context.Background(), cohort, DefaultOptions())
		if err != nil || result.Episodes[0].Status != row.status || result.Episodes[1].Status != row.status {
			t.Fatalf("relative window cap points=%d: %#v %v", row.points, result, err)
		}
	}
	a := newAnalyzer(DefaultOptions())
	a.credits = true
	duration := 20.9
	if got := a.getMaximumSegmentDuration(episode{DurationSeconds: &duration}); got != 19 {
		t.Fatalf("native truncation was lost: %d", got)
	}
}

func TestCreditsKeepsTailRelativeStartSnapAndFirstValidPair(t *testing.T) {
	points := filled(200, 17)
	cohort := Cohort{Key: "season", Episodes: []Episode{
		contractEpisode("first", points), contractEpisode("second", points), contractEpisode("third", points),
	}}
	result, err := AnalyzeCredits(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Episodes[0].Candidate.Interval.StartTicks != 150*TicksPerSecond ||
		result.Episodes[0].Candidate.Support[1].EpisodeKey != "second" ||
		result.Episodes[2].Candidate.Support[0].EpisodeKey != "second" {
		t.Fatalf("start snap or first-pair order changed: %#v", result)
	}
	// The core's <=5-second snap still applies before adding the file offset.
	left, right := filled(240, 0), filled(240, ^uint32(0))
	for i := 0; i < 180; i++ {
		point := uint32(i+1)*4096 + 0x555
		left[30+i], right[50+i] = point, point
	}
	cohort.Episodes = cohort.Episodes[:2]
	cohort.Episodes[0].Fingerprint, cohort.Episodes[1].Fingerprint = left, right
	result, err = AnalyzeCredits(context.Background(), cohort, DefaultOptions())
	if err != nil || result.Episodes[0].Candidate == nil || result.Episodes[1].Candidate == nil {
		t.Fatalf("missing shifted tail candidate: %#v %v", result, err)
	}
	wantRHS, err := secondsToTicks(150 + 50*sampleDuration)
	if err != nil || result.Episodes[0].Candidate.Interval.StartTicks != 150*TicksPerSecond || result.Episodes[1].Candidate.Interval.StartTicks != wantRHS {
		t.Fatalf("tail-relative snap differs: %#v %v", result, err)
	}
}

func TestCreditsValidationRejectsRelativeAndForeignTailEvidence(t *testing.T) {
	cohort := contractCohort()
	cohort.Episodes[1].DurationTicks = 1000 * TicksPerSecond
	result, err := AnalyzeCredits(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Candidate){
		func(c *Candidate) { c.Interval.StartTicks = 0; c.Support[0].Interval = c.Interval },
		func(c *Candidate) { c.Support[1].Interval = c.Support[0].Interval },
		func(c *Candidate) { c.Support[1].Interval.EndTicks = 1001 * TicksPerSecond },
		func(c *Candidate) { c.UpstreamCommit = "foreign" },
	} {
		candidate := *result.Episodes[0].Candidate
		candidate.Support = append([]Support(nil), candidate.Support...)
		mutate(&candidate)
		if err := ValidateCreditsCandidate(candidate, cohort.Episodes[0], cohort, DefaultOptions()); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted invalid credits evidence: %#v %v", candidate, err)
		}
	}
	// Stored validation does not require retaining the complete fingerprints.
	for i := range cohort.Episodes {
		cohort.Episodes[i].Fingerprint = nil
	}
	if err := ValidateCreditsEpisodeResult(result.Episodes[0], cohort.Episodes[0], cohort, DefaultOptions()); err != nil {
		t.Fatal(err)
	}
}

func TestCreditsAdmissionAndCancellationFailWithoutPartialResults(t *testing.T) {
	cohort := contractCohort()
	for _, candidate := range []Cohort{
		{Key: "single", Episodes: cohort.Episodes[:1]},
		{Key: "duplicate", Episodes: []Episode{cohort.Episodes[0], cohort.Episodes[0]}},
	} {
		if result, err := AnalyzeCredits(context.Background(), candidate, DefaultOptions()); !errors.Is(err, ErrInvalidInput) || result.Episodes != nil {
			t.Fatalf("accepted invalid credits cohort: %#v %v", result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := AnalyzeCredits(ctx, cohort, DefaultOptions()); !errors.Is(err, context.Canceled) || result.Episodes != nil {
		t.Fatalf("returned a result after cancellation: %#v %v", result, err)
	}
	if result, err := AnalyzeCredits(nil, cohort, DefaultOptions()); !errors.Is(err, ErrInvalidInput) || result.Episodes != nil {
		t.Fatalf("accepted a missing context: %#v %v", result, err)
	}
}
