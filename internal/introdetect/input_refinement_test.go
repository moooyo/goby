package introdetect

import (
	"context"
	"errors"
	"testing"
)

func TestRefinementInputRetainsExactIndependentSourceEvidence(t *testing.T) {
	c := visualSequenceFixture()
	for n := range c.Episodes {
		for i := 0; i < MaxRefinementSamples; i++ {
			c.Episodes[n].Refinement = append(c.Episodes[n].Refinement, RefinementSample{Ticks: int64(i) * TicksPerSecond / 10})
		}
	}
	o := DefaultOptions()
	if _, err := validateInput(context.Background(), c, o); err != nil {
		t.Fatal(err)
	}
	e := c.Episodes[0]
	bytes := len(e.Audio)*24 + len(e.Visual)*88 + len(e.Refinement)*264 + len(e.EpisodeKey) + len(e.SourceKey) + len(e.ContentIdentity) + len(e.AlgorithmProfile)
	single := Cohort{Key: c.Key, Episodes: []Episode{e}}
	o.MaxFeatureBytes = bytes
	if _, err := validateInput(context.Background(), single, o); err != nil {
		t.Fatal("exact retained raster budget rejected", err)
	}
	o.MaxFeatureBytes--
	if _, err := validateInput(context.Background(), single, o); !errors.Is(err, ErrLimit) {
		t.Fatal("refinement was omitted from the byte budget", err)
	}
	for _, mutate := range []func(*Episode){
		func(e *Episode) { e.Refinement[1].Ticks = e.Refinement[0].Ticks },
		func(e *Episode) { e.Refinement[0].Ticks = -1 },
		func(e *Episode) { e.Refinement[len(e.Refinement)-1].Ticks = RefinementPrefixTicks },
		func(e *Episode) { e.DurationTicks = 60 * TicksPerSecond; e.Visual = e.Visual[:120] },
	} {
		copyEpisode := e
		copyEpisode.Refinement = append([]RefinementSample(nil), e.Refinement...)
		mutate(&copyEpisode)
		if _, err := validateInput(context.Background(), Cohort{Key: c.Key, Episodes: []Episode{copyEpisode}}, DefaultOptions()); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid refinement timeline accepted", err)
		}
	}
	alias := e
	alias.Refinement = append([]RefinementSample(nil), e.Refinement...)
	alias.Refinement[0].Raster[0] = 1
	if _, err := validateInput(context.Background(), Cohort{Key: c.Key, Episodes: []Episode{e, alias}}, DefaultOptions()); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("conflicting refinement escaped source identity validation", err)
	}
	e.Refinement = append(e.Refinement, RefinementSample{Ticks: RefinementPrefixTicks - 1})
	if _, err := validateInput(context.Background(), Cohort{Key: c.Key, Episodes: []Episode{e}}, DefaultOptions()); !errors.Is(err, ErrLimit) {
		t.Fatal("refinement count cap was not applied", err)
	}
}

func TestRefinementComparisonBudgetIsAnExplicitCurrentResourceBound(t *testing.T) {
	o := DefaultOptions()
	if o.MaxComparisons != 200_000_000 || o.MaxFeatureBytes != 768<<10 {
		t.Fatal("unexpected current resource policy", o)
	}
	if _, err := normalizeOptions(o); err != nil {
		t.Fatal(err)
	}
	o.MaxComparisons++
	if _, err := normalizeOptions(o); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unbounded calibrated comparison request accepted", err)
	}
	o = DefaultOptions()
	o.MaxFeatureBytes++
	if _, err := normalizeOptions(o); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unbounded refinement input accepted", err)
	}
}
