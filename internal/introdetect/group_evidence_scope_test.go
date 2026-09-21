package introdetect

import (
	"slices"
	"testing"
)

var groupScopeStates = []uint64{
	0x0000ffff0000ffff, 0xffff0000ffff0000, 0xff00ff00ff00ff00, 0x00ff00ff00ff00ff, 0xaaaaaaaaaaaaaaaa,
}

// Measure both the discovery edges and the control edges from the same actual
// features. A control interval is measured before visual confirmation; neither
// its metrics nor its qualification status is fabricated by these fixtures.
func groupScopeEdges(t *testing.T, episodes []Episode, interval *Interval) map[[2]int]pairMatch {
	t.Helper()
	edges := make(map[[2]int]pairMatch)
	o, budget := DefaultOptions(), audioEvidenceBudget()
	for left := 0; left < len(episodes); left++ {
		for right := left + 1; right < len(episodes); right++ {
			runs, _, err := alignedAudio(episodes[left], episodes[right], 0, o, budget)
			if err != nil || len(runs) != 1 {
				t.Fatalf("expected one actual discovery run for %d/%d: %#v, %v", left, right, runs, err)
			}
			audio := runs[0]
			if interval != nil {
				if intersect(audio.a, *interval) != *interval || intersect(audio.b, *interval) != *interval {
					t.Fatalf("control interval escaped the actual acoustic witness: %#v, %#v", audio, interval)
				}
				projected, err := projectAudioEvidence(episodes[left], episodes[right], *interval, *interval, 0, audio, o, budget)
				if err != nil || projected == nil {
					t.Fatalf("control interval had no actual audio evidence: %#v, %v", projected, err)
				}
				audio = *projected
			}
			match, reason, err := visualConfirm(episodes[left], episodes[right], audio, 0, o, budget)
			if err != nil || match == nil || reason != "" {
				t.Fatalf("expected an actual visual witness for %d/%d: %#v, %s, %v", left, right, match, reason, err)
			}
			match.left, match.right = left, right
			edges[[2]int{left, right}] = *match
		}
	}
	return edges
}

func groupScopeProject(t *testing.T, episodes []Episode, edges map[[2]int]pairMatch, interval Interval) *Group {
	t.Helper()
	for _, edge := range edges {
		if intersect(edge.a, interval) != interval || intersect(edge.b, interval) != interval {
			t.Fatalf("final interval escaped a real supporting edge: %#v, %#v", edge, interval)
		}
	}
	group, err := projectGroup(episodes, []int{0, 1, 2}, map[int]Interval{0: interval, 1: interval, 2: interval}, edges, DefaultOptions(), audioEvidenceBudget())
	if err != nil || group == nil || len(group.Members) != 3 || group.Metrics.PairCount != 3 {
		t.Fatalf("complete witness did not project: %#v, %v", group, err)
	}
	for _, member := range group.Members {
		if member.Interval != interval {
			t.Fatalf("projection changed the exact observed control interval: %#v, want %#v", member, interval)
		}
	}
	return group
}

func requireGroupScopeQualified(t *testing.T, group *Group) {
	t.Helper()
	if group.Status != Qualified || len(group.Reasons) != 0 || !qualifiedEvidence(group.Metrics, DefaultOptions()) {
		t.Fatalf("actual final evidence did not qualify: %#v", group)
	}
}

func groupScopeDominantWings(episodes []Episode) {
	for source := range episodes {
		for index := range episodes[source].Visual {
			frame := &episodes[source].Visual[index]
			if frame.Ticks >= 50*TicksPerSecond {
				continue
			}
			state := int64(0)
			if frame.Ticks >= 25*TicksPerSecond && frame.Ticks < 40*TicksPerSecond {
				state = 1 + (frame.Ticks-25*TicksPerSecond)/(5*TicksPerSecond)
			}
			frame.Hash = groupScopeStates[state]
		}
	}
}

func TestGroupProjectionUsesActualFinalQualityInsteadOfDiscoveryQuality(t *testing.T) {
	for _, test := range []struct {
		name     string
		reason   Reason
		interval Interval
		mutate   func([]Episode)
	}{
		{
			name: "weak_audio", reason: WeakAudioEvidence, interval: Interval{10 * TicksPerSecond, 45 * TicksPerSecond},
			mutate: func(episodes []Episode) {
				for index := range episodes[2].Audio {
					sample := &episodes[2].Audio[index]
					outside := sample.StartTicks >= 2*TicksPerSecond && sample.StartTicks < 10*TicksPerSecond ||
						sample.StartTicks >= 45*TicksPerSecond && sample.StartTicks < 48*TicksPerSecond
					if outside && index%2 == 1 {
						sample.Fingerprint = ^sample.Fingerprint
					}
				}
			},
		},
		{
			name: "weak_visual", reason: WeakVisualEvidence, interval: Interval{15 * TicksPerSecond, 45 * TicksPerSecond},
			mutate: func(episodes []Episode) {
				for _, second := range []int{3, 4, 8, 9, 13, 14, 47} {
					episodes[2].Visual[second].Hash = ^episodes[2].Visual[second].Hash
				}
			},
		},
		{
			name: "insufficient_visual_anchors", reason: InsufficientVisualAnchors, interval: Interval{13 * TicksPerSecond, 45 * TicksPerSecond},
			mutate: func(episodes []Episode) {
				for second := 8; second <= 12; second++ {
					episodes[2].Visual[second].Hash = ^episodes[2].Visual[second].Hash
				}
			},
		},
		{
			name: "low_visual_diversity", reason: LowVisualDiversity, interval: Interval{10 * TicksPerSecond, 40 * TicksPerSecond},
			mutate: groupScopeDominantWings,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			episodes, _ := v2ProjectionFixture(t)
			test.mutate(episodes)
			discovery := groupScopeEdges(t, episodes, nil)
			found := false
			for _, edge := range discovery {
				if slices.Contains(edge.reasons, test.reason) {
					found = true
					if qualifiedEvidence(edge.metrics, DefaultOptions()) {
						t.Fatalf("fixture claimed weak discovery without a measured gate failure: %#v", edge)
					}
				}
			}
			if !found {
				t.Fatalf("actual discovery did not observe %s: %#v", test.reason, discovery)
			}
			control := groupScopeProject(t, episodes, groupScopeEdges(t, episodes, &test.interval), test.interval)
			requireGroupScopeQualified(t, control)
			group := groupScopeProject(t, episodes, discovery, test.interval)
			requireGroupScopeQualified(t, group)
			if group.Metrics != control.Metrics {
				t.Fatalf("earlier interval metrics polluted the same actual final witness: %#v, want %#v", group.Metrics, control.Metrics)
			}
		})
	}
}

func TestGroupProjectionRecomputesShortIntervalsWithoutGrowingThem(t *testing.T) {
	episodes, _ := v2ProjectionFixture(t)
	groupScopeDominantWings(episodes)
	short := Interval{10 * TicksPerSecond, 39 * TicksPerSecond}
	edges := groupScopeEdges(t, episodes, &short)
	for _, edge := range edges {
		if !slices.Contains(edge.reasons, ShortInterval) || !qualifiedEvidence(edge.metrics, DefaultOptions()) {
			t.Fatalf("short fixture was not otherwise strong actual evidence: %#v", edge)
		}
	}
	// A true short interval cannot become long enough by intersection. Its new
	// ShortInterval reason must survive even after the earlier one is discarded.
	group := groupScopeProject(t, episodes, edges, Interval{11 * TicksPerSecond, 39 * TicksPerSecond})
	if group.Status != Review || len(group.Reasons) != 1 || group.Reasons[0] != ShortInterval || !qualifiedEvidence(group.Metrics, DefaultOptions()) {
		t.Fatalf("shrinking a real short interval erased its current duration gate: %#v", group)
	}
	long := Interval{10 * TicksPerSecond, 40 * TicksPerSecond}
	control := groupScopeProject(t, episodes, groupScopeEdges(t, episodes, &long), long)
	requireGroupScopeQualified(t, control)
}

func TestGroupProjectionDoesNotTrustAStaleShortIntervalAnnotation(t *testing.T) {
	episodes, edges := v2ProjectionFixture(t)
	interval := Interval{10 * TicksPerSecond, 45 * TicksPerSecond}
	control := groupScopeProject(t, episodes, edges, interval)
	requireGroupScopeQualified(t, control)
	// This deliberately inconsistent annotation tests stale metadata handling;
	// it does not claim that a genuinely short discovery interval can grow.
	edge := edges[[2]int{0, 1}]
	edge.reasons = addReason(edge.reasons, ShortInterval)
	edges[[2]int{0, 1}] = edge
	group := groupScopeProject(t, episodes, edges, interval)
	requireGroupScopeQualified(t, group)
	if group.Metrics != control.Metrics {
		t.Fatalf("a stale annotation changed actual final metrics: %#v, want %#v", group.Metrics, control.Metrics)
	}
}

func TestGroupProjectionRetainsAnActualExtractionBoundary(t *testing.T) {
	var episodes []Episode
	for number := 1; number <= 3; number++ {
		episode := testEpisode(number, 50*TicksPerSecond, testOpening{0, 50 * TicksPerSecond, 778899})
		for index := range episode.Visual {
			episode.Visual[index].Hash = groupScopeStates[episode.Visual[index].Ticks/(10*TicksPerSecond)]
		}
		episodes = append(episodes, episode)
	}
	edges := groupScopeEdges(t, episodes, nil)
	for _, edge := range edges {
		if !slices.Contains(edge.audio.reasons, AnalysisBoundary) || !slices.Contains(edge.reasons, AnalysisBoundary) {
			t.Fatalf("fixture did not discover the actual extraction suffix: %#v", edge)
		}
	}
	group := groupScopeProject(t, episodes, edges, Interval{10 * TicksPerSecond, 45 * TicksPerSecond})
	if group.Status != Review || !slices.Contains(group.Reasons, AnalysisBoundary) || !qualifiedEvidence(group.Metrics, DefaultOptions()) {
		t.Fatalf("a strong interior erased its real extraction boundary: %#v", group)
	}
}

func TestGroupProjectionRetainsObservedPeriodicityOutsideTheFinalInterval(t *testing.T) {
	var episodes []Episode
	for number := 1; number <= 3; number++ {
		episode := testEpisode(number, 100*TicksPerSecond, testOpening{0, 70 * TicksPerSecond, 778899})
		for index := range episode.Audio {
			if episode.Audio[index].StartTicks >= 70*TicksPerSecond {
				episode.Audio[index].Fingerprint = []uint32{0, 0xffffffff, 0xaaaaaaaa}[number-1]
			}
		}
		for index := range episode.Visual {
			frame := &episode.Visual[index]
			if frame.Ticks < 70*TicksPerSecond {
				frame.Hash = groupScopeStates[(frame.Ticks/(8*TicksPerSecond))%4]
			}
		}
		episodes = append(episodes, episode)
	}
	edges := groupScopeEdges(t, episodes, nil)
	for _, edge := range edges {
		if !slices.Contains(edge.reasons, PeriodicVisualEvidence) {
			t.Fatalf("fixture did not observe its repeated 32-second sequence: %#v", edge)
		}
	}
	interval := Interval{2 * TicksPerSecond, 32 * TicksPerSecond}
	control := groupScopeProject(t, episodes, groupScopeEdges(t, episodes, &interval), interval)
	requireGroupScopeQualified(t, control)
	group := groupScopeProject(t, episodes, edges, interval)
	if group.Status != Review || !slices.Contains(group.Reasons, PeriodicVisualEvidence) || group.Metrics != control.Metrics {
		t.Fatalf("a single strong cycle erased observed periodicity: %#v, control %#v", group, control)
	}
}

func TestGroupProjectionRetainsSuppliedSearchAndUnknownSafetyFacts(t *testing.T) {
	for _, reason := range []Reason{CandidateSearchLimited, CompetingIntervals, InconsistentTimeAlignment, Reason("future_independent_safety_fact")} {
		t.Run(string(reason), func(t *testing.T) {
			episodes, edges := v2ProjectionFixture(t)
			interval := Interval{10 * TicksPerSecond, 45 * TicksPerSecond}
			control := groupScopeProject(t, episodes, edges, interval)
			requireGroupScopeQualified(t, control)
			// These facts belong to the upstream search or future safety policy;
			// projection cannot regenerate them by looking only at this interval.
			edge := edges[[2]int{0, 1}]
			edge.reasons = addReason(edge.reasons, reason)
			edges[[2]int{0, 1}] = edge
			group := groupScopeProject(t, episodes, edges, interval)
			if group.Status != Review || len(group.Reasons) != 1 || group.Reasons[0] != reason || group.Metrics != control.Metrics {
				t.Fatalf("projection lost a search or unknown safety fact: %#v, control %#v", group, control)
			}
		})
	}
}
