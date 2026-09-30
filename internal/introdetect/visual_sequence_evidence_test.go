package introdetect

import (
	"context"
	"testing"
)

func TestVisualCandidateEvidenceDoesNotBorrowAcousticAuthority(t *testing.T) {
	c := visualSequenceFixture()
	r, err := Analyze(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || len(r.Episodes[0].Candidates) != 1 {
		t.Fatalf("missing visual fallback: %+v", r)
	}
	candidate := r.Episodes[0].Candidates[0]
	if !ValidateCandidateEvidence(candidate, DefaultOptions()) {
		t.Fatal("complete visual witness rejected")
	}
	for _, mutate := range []func(*Candidate){
		func(c *Candidate) { c.VisualEvidence = nil },
		func(c *Candidate) { c.Metrics.AudioDistinct = 12; c.Metrics.AudioSamples = 12 },
		func(c *Candidate) { c.VisualEvidence.MaxCenterRMSPermille = 651 },
		func(c *Candidate) { c.VisualEvidence.MaxLumaRMSPermille = 551 },
		func(c *Candidate) { c.VisualEvidence.MaxGapTicks = visualSequenceGap + 1 },
		func(c *Candidate) { c.VisualEvidence.CoveragePermille = 849 },
		func(c *Candidate) { c.VisualEvidence.Transitions = 2 },
		func(c *Candidate) { c.Interval.EndTicks = c.Interval.StartTicks + visualSequenceMinimum - 1 },
		func(c *Candidate) { c.Interval = Interval{121 * TicksPerSecond, 137 * TicksPerSecond} },
		func(c *Candidate) { c.Reasons = []Reason{CandidateSearchLimited} },
	} {
		copyCandidate := candidate
		metrics := *candidate.VisualEvidence
		copyCandidate.VisualEvidence = &metrics
		mutate(&copyCandidate)
		if ValidateCandidateEvidence(copyCandidate, DefaultOptions()) {
			t.Fatalf("invalid evidence accepted: %+v", copyCandidate)
		}
	}
	// Group and episode evidence are distinct values; mutating one audit view
	// cannot change another publication candidate's evidence in memory.
	r.Groups[0].VisualEvidence.Samples = 0
	if r.Episodes[0].Candidates[0].VisualEvidence.Samples == 0 {
		t.Fatal("group aliases candidate evidence")
	}
	r.Episodes[0].Candidates[0].VisualEvidence.Samples = 0
	if r.Episodes[1].Candidates[0].VisualEvidence.Samples == 0 {
		t.Fatal("episode evidence is shared")
	}
}

func TestVisualFallbackKeepsLegacyInputsOnAcousticPath(t *testing.T) {
	c := visualSequenceFixture()
	for i := range c.Episodes {
		for j := range c.Episodes[i].Visual {
			c.Episodes[i].Visual[j].LumaKnown = false
			c.Episodes[i].Visual[j].Luma = [64]int8{}
		}
	}
	r, err := Analyze(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 0 {
		t.Fatal("old hash-only features acquired new visual authority")
	}
}
