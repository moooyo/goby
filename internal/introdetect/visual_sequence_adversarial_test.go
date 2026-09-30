package introdetect

import (
	"context"
	"errors"
	"math/rand"
	"testing"
)

func TestVisualSequencesPreferVisualEvidenceAcrossEpisodeAliases(t *testing.T) {
	c := visualSequenceFixture()
	alias := c.Episodes[0]
	alias.SourceKey = "A-legacy"
	alias.AlgorithmProfile = "legacy-audio-profile"
	alias.Visual = append([]VisualSample(nil), alias.Visual...)
	for i := range alias.Visual {
		alias.Visual[i].LumaKnown = false
		alias.Visual[i].Luma = [64]int8{}
	}
	for i := 0; i < 12; i++ {
		word := uint32(0x55555555)
		if i%2 != 0 {
			word = 0xaaaaaaaa
		}
		alias.Audio = append(alias.Audio, AudioSample{
			StartTicks:  int64(i) * TicksPerSecond / 2,
			EndTicks:    int64(i+1) * TicksPerSecond / 2,
			Fingerprint: word,
		})
	}
	c.Episodes = append(c.Episodes, alias)
	result, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Members) != 3 {
		t.Fatalf("an audio-ready alias displaced usable visual evidence: %+v", result)
	}
}

func TestVisualSequencesRejectRepeatedFourStateLoop(t *testing.T) {
	c := visualSequenceFixture()
	states := append([]VisualSample(nil), c.Episodes[0].Visual[10:14]...)
	for k := range c.Episodes {
		start := 10 + k*4
		for i := start; i < start+33; i++ {
			ticks := c.Episodes[k].Visual[i].Ticks
			c.Episodes[k].Visual[i] = states[(i-start)%len(states)]
			c.Episodes[k].Visual[i].Ticks = ticks
		}
	}
	result, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil || len(result.Groups) != 0 {
		t.Fatalf("periodic loop accepted: result=%+v error=%v", result, err)
	}
}

func TestVisualSequencesRejectMalformedDescriptors(t *testing.T) {
	for _, mode := range []string{"range", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			c := visualSequenceFixture()
			if mode == "range" {
				c.Episodes[0].Visual[0].Luma[0] = -128
			} else {
				c.Episodes[0].Visual[0].LumaKnown = false
			}
			_, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid descriptor error = %v", err)
			}
		})
	}
}

func TestVisualSequenceIndependentIdentityUnionRemainsTransitive(t *testing.T) {
	c := visualSequenceFixture()
	bridge := c.Episodes[0]
	bridge.SourceKey = "bridge"
	bridge.ContentIdentity = c.Episodes[1].ContentIdentity
	c.Episodes = append(c.Episodes, bridge)
	result, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil || len(result.Groups) != 0 {
		t.Fatalf("transitive aliases supplied independent support: result=%+v error=%v", result, err)
	}
}

func TestVisualSequencesPreserveLongerWitnessWhenAnotherEpisodeRepeatsItsPrefix(t *testing.T) {
	c := visualSequenceFixture()
	extra := c.Episodes[0]
	extra.EpisodeKey, extra.SourceKey, extra.ContentIdentity = "0-short", "D", "3"
	extra.Visual = append([]VisualSample(nil), extra.Visual...)
	rng := rand.New(rand.NewSource(206))
	for i := range extra.Visual {
		if i >= 10 && i <= 26 {
			continue
		}
		extra.Visual[i].Hash = rng.Uint64()
		for j := range extra.Visual[i].Luma {
			extra.Visual[i].Luma[j] = int8(rng.Intn(111) - 55)
		}
	}
	c.Episodes = append(c.Episodes, extra)
	result, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range result.Groups {
		if len(group.Members) >= 3 && group.Members[0].Interval.EndTicks-group.Members[0].Interval.StartTicks == 16*TicksPerSecond {
			return
		}
	}
	t.Fatalf("a shorter fourth witness erased the complete three-episode interval: %+v", result)
}

func TestVisualSequencesRejectUncorrelatedDetailOverOneCoarseState(t *testing.T) {
	c := visualSequenceFixture()
	coarse := c.Episodes[0].Visual[10].Luma
	rng := rand.New(rand.NewSource(307))
	for k := range c.Episodes {
		start := 10 + k*4
		for i := start; i < start+33; i++ {
			// The coarse image stays static. Independent fine detail must not
			// count as a shared sequence of changing visual states.
			c.Episodes[k].Visual[i].Luma = coarse
			c.Episodes[k].Visual[i].Hash = rng.Uint64()
		}
	}
	result, err := DiscoverVisualSequences(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("independent hash changes over one coarse state supplied false diversity: %+v", result)
	}
}

func TestVisualSequenceRMSCeilingDoesNotTruncateBeforeSquareRoot(t *testing.T) {
	// The exact squared RMS is 19,000,000 / 65,536, strictly above 17^2.
	if got := sequenceRMS(19, 64); got != 18 {
		t.Fatalf("RMS ceiling = %d, want 18", got)
	}
}
