package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"testing"
)

func visualSequenceFixture() Cohort {
	rng := rand.New(rand.NewSource(104))
	sequence := make([]VisualSample, 33)
	for i := range sequence {
		sequence[i] = VisualSample{Hash: rng.Uint64(), Contrast: 200, LumaKnown: true}
		for j := range sequence[i].Luma {
			sequence[i].Luma[j] = int8(rng.Intn(111) - 55)
		}
	}
	cohort := Cohort{Key: "visual-fixture"}
	for n := 0; n < 3; n++ {
		e := Episode{EpisodeKey: string(rune('a' + n)), SourceKey: string(rune('A' + n)), ContentIdentity: string(rune('0' + n)), AlgorithmProfile: "luma-v2", DurationTicks: 300 * TicksPerSecond}
		for i := 0; i < 240; i++ {
			s := VisualSample{Ticks: int64(i) * TicksPerSecond / 2, Hash: rng.Uint64(), Contrast: 200, LumaKnown: true}
			for j := range s.Luma {
				s.Luma[j] = int8(rng.Intn(111) - 55)
			}
			start := 10 + n*4
			if i >= start && i < start+len(sequence) {
				s = sequence[i-start]
				s.Ticks = int64(i) * TicksPerSecond / 2
			}
			e.Visual = append(e.Visual, s)
		}
		cohort.Episodes = append(cohort.Episodes, e)
	}
	return cohort
}

func TestVisualSequencesFindIndependentShortVariantWithoutInventingAudio(t *testing.T) {
	c := visualSequenceFixture()
	r, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil || !r.Experimental || r.Version != VisualSequenceVersion || len(r.Groups) != 1 || r.SearchLimited {
		t.Fatalf("result=%+v error=%v", r, err)
	}
	g := r.Groups[0]
	if len(g.Members) != 3 || g.Metrics.PairCount != 3 || g.Metrics.CoveragePermille != 1000 {
		t.Fatalf("group=%+v", g)
	}
	for i, m := range g.Members {
		expected := Interval{int64(5+i*2) * TicksPerSecond, int64(21+i*2) * TicksPerSecond}
		if m.Interval != expected {
			t.Fatalf("member=%+v expected=%+v", m, expected)
		}
	}
	ordinary, err := Analyze(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ordinary.Episodes {
		if e.Status != Qualified || len(e.Candidates) != 1 || e.Candidates[0].VisualEvidence == nil || e.Candidates[0].Metrics != (Metrics{}) {
			t.Fatal("visual fallback must retain separate evidence")
		}
	}
	c.Episodes[0], c.Episodes[2] = c.Episodes[2], c.Episodes[0]
	reversed, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(r)
	y, _ := json.Marshal(reversed)
	if string(x) != string(y) {
		t.Fatal("input order changed results")
	}
}

func TestVisualSequencesRejectAliasesMissingDescriptorsAndStaticLogos(t *testing.T) {
	for _, mode := range []string{"alias", "missing", "static", "foreground"} {
		t.Run(mode, func(t *testing.T) {
			c := visualSequenceFixture()
			switch mode {
			case "alias":
				c.Episodes[2].ContentIdentity = c.Episodes[0].ContentIdentity
			case "missing":
				for i := range c.Episodes[2].Visual {
					c.Episodes[2].Visual[i].LumaKnown = false
					c.Episodes[2].Visual[i].Luma = [64]int8{}
				}
			case "static":
				for k := range c.Episodes {
					for i := range c.Episodes[k].Visual {
						c.Episodes[k].Visual[i].Hash = 1
					}
				}
			case "foreground":
				for i := range c.Episodes[2].Visual {
					for row := 2; row < 6; row++ {
						for col := 2; col < 6; col++ {
							c.Episodes[2].Visual[i].Luma[row*8+col] = 127
						}
					}
				}
			}
			r, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
			if err != nil || len(r.Groups) != 0 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestVisualSequencesCancelAndExhaustWithoutPartialGroups(t *testing.T) {
	c := visualSequenceFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := DiscoverVisualSequences(ctx, c, DefaultOptions())
	if !errors.Is(err, context.Canceled) || len(r.Groups) != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	o := DefaultOptions()
	o.MaxComparisons = 10
	r, err = DiscoverVisualSequences(context.Background(), c, o)
	if !errors.Is(err, ErrLimit) || len(r.Groups) != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestVisualSequencesDoNotExtendAcrossDifferentEpisodeMaterial(t *testing.T) {
	c := visualSequenceFixture()
	// The repeated prefix ends before unrelated episode-specific material.
	// Equal background hashes after the opening cannot override the descriptor.
	for k := range c.Episodes {
		for i := 43; i < 65; i++ {
			c.Episodes[k].Visual[i].Hash = 0x1234
		}
	}
	r, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil || len(r.Groups) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	for i, m := range r.Groups[0].Members {
		if m.Interval.EndTicks > int64(21+i*2)*TicksPerSecond {
			t.Fatal("candidate extended into protected material")
		}
	}
}
