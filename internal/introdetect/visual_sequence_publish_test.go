package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"testing"
)

func publicationWitness(id string, sources []string, start, end int64, coverage int) Group {
	m := VisualSequenceMetrics{Samples: 41, CoveragePermille: coverage, Transitions: 6, DistinctStates: 8, DominantStatePermille: 250, MaxLumaRMSPermille: 100, MaxCenterRMSPermille: 100, MaxGapTicks: TicksPerSecond, PairCount: len(sources) * (len(sources) - 1) / 2}
	g := Group{ID: id, Status: Qualified, VisualEvidence: &m}
	for _, source := range sources {
		g.Members = append(g.Members, Support{EpisodeKey: source, SourceKey: source, ContentIdentity: source, Interval: Interval{start, end}})
	}
	return g
}

func TestVisualPublicationSelectsExistingNestedWitnessPerSource(t *testing.T) {
	groups := []Group{
		publicationWitness("outer", []string{"A", "B", "C"}, 10*TicksPerSecond, 30*TicksPerSecond, 900),
		publicationWitness("inner", []string{"A", "B", "D", "E"}, 12*TicksPerSecond, 28*TicksPerSecond, 950),
	}
	before, _ := json.Marshal(groups)
	selection, err := selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "inner", "B": "inner", "C": "outer", "D": "inner", "E": "inner"}
	if !reflect.DeepEqual(selection, want) {
		t.Fatalf("selection=%v, want %v", selection, want)
	}
	after, _ := json.Marshal(groups)
	if string(before) != string(after) {
		t.Fatal("selection modified a complete witness or its evidence")
	}
	groups[0], groups[1] = groups[1], groups[0]
	reversed, err := selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1000})
	if err != nil || !reflect.DeepEqual(selection, reversed) {
		t.Fatalf("input order changed selection: %v, %v", reversed, err)
	}
}

func TestVisualPublicationRejectsCrossingAndDisjointWindowsForAffectedSources(t *testing.T) {
	for _, interval := range []Interval{{15 * TicksPerSecond, 35 * TicksPerSecond}, {40 * TicksPerSecond, 60 * TicksPerSecond}} {
		groups := []Group{
			publicationWitness("first", []string{"A", "B", "C"}, 10*TicksPerSecond, 30*TicksPerSecond, 900),
			publicationWitness("competing", []string{"A", "D", "E"}, interval.StartTicks, interval.EndTicks, 950),
		}
		selection, err := selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1000})
		if err != nil {
			t.Fatal(err)
		}
		if selected, exists := selection["A"]; !exists || selected != "" {
			t.Fatalf("competing source was selected or lost its ambiguity: %v", selection)
		}
		if selection["B"] != "first" || selection["D"] != "competing" {
			t.Fatalf("unambiguous source lost its full witness: %v", selection)
		}
	}
}

func TestVisualPublicationHandlesSamplingPhaseWithoutMergingClocks(t *testing.T) {
	groups := []Group{
		publicationWitness("one", []string{"A", "B", "C"}, 10*TicksPerSecond, 30*TicksPerSecond, 900),
		publicationWitness("two", []string{"A", "D", "E"}, 10*TicksPerSecond+visualSequenceResidual, 30*TicksPerSecond+visualSequenceResidual, 950),
	}
	selection, err := selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1000})
	if err != nil || selection["A"] != "two" {
		t.Fatalf("sampling-phase witness rejected: %v, %v", selection, err)
	}
	groups[1].Members[0].Interval.StartTicks++
	groups[1].Members[0].Interval.EndTicks++
	selection, err = selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if selection["A"] != "" {
		t.Fatal("crossing window outside the existing residual was accepted")
	}
}

func TestVisualPublicationSelectionHonorsComparisonBudget(t *testing.T) {
	groups := []Group{publicationWitness("one", []string{"A", "B", "C"}, 10*TicksPerSecond, 30*TicksPerSecond, 900)}
	selection, err := selectVisualWitnesses(groups, &workBudget{ctx: context.Background(), limit: 1})
	if !errors.Is(err, ErrLimit) || selection != nil {
		t.Fatalf("partial selection escaped budget: %v, %v", selection, err)
	}
}

func TestAnalyzeAttachesOneCompleteNestedVisualWitnessPerEpisode(t *testing.T) {
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
	result, err := Analyze(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, episode := range result.Episodes {
		if episode.Status != Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("episode=%+v", episode)
		}
		candidate := episode.Candidates[0]
		found := false
		for _, group := range result.Groups {
			if group.ID == candidate.GroupID {
				found = true
				if !reflect.DeepEqual(group.Members, candidate.Support) || !reflect.DeepEqual(group.VisualEvidence, candidate.VisualEvidence) {
					t.Fatal("candidate lost its complete supporting witness")
				}
			}
		}
		if !found || candidate.Metrics != (Metrics{}) {
			t.Fatal("candidate lost its group or invented acoustic evidence")
		}
	}
}

func TestAnalyzeReportsCompetingVisualIntervalsWithoutPublishing(t *testing.T) {
	c := visualSequenceFixture()
	rng := rand.New(rand.NewSource(811))
	sequence := make([]VisualSample, 17)
	for i := range sequence {
		sequence[i] = VisualSample{Hash: rng.Uint64(), Contrast: 200, LumaKnown: true}
		for j := range sequence[i].Luma {
			sequence[i].Luma[j] = int8(rng.Intn(111) - 55)
		}
	}
	for k := range c.Episodes {
		for i, frame := range sequence {
			index := 100 + k*4 + i
			frame.Ticks = c.Episodes[k].Visual[index].Ticks
			c.Episodes[k].Visual[index] = frame
		}
	}
	result, err := Analyze(context.Background(), c, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, episode := range result.Episodes {
		competing := false
		for _, reason := range episode.Reasons {
			competing = competing || reason == CompetingIntervals
		}
		if episode.Status != NoResult || len(episode.Candidates) != 0 || !competing {
			t.Fatalf("episode=%+v", episode)
		}
	}
}
