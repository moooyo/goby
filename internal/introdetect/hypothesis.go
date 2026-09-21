package introdetect

import (
	"fmt"
	"sort"
)

func equivalentPair(a, b pairMatch, o Options) bool {
	return compatibleInterval(a.a, b.a, o) && compatibleInterval(a.b, b.b, o) &&
		hypothesisTicksWithin(a.offset, b.offset, o.AudioAlignmentTicks)
}

func pairHypothesisClass(anchor pairMatch) string {
	return fmt.Sprintf("%d:%d:%d:%d:%d", anchor.a.StartTicks, anchor.a.EndTicks, anchor.b.StartTicks, anchor.b.EndTicks, anchor.offset)
}

// distinctPairHypotheses clusters a canonical ordering against immutable first
// observations. A stronger replacement is only the winner, never a new anchor
// that could bridge two otherwise distinct phase hypotheses.
func distinctPairHypotheses(values []pairMatch, o Options, budget *workBudget) ([]pairMatch, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	ordered := make([]pairMatch, len(values))
	for index, value := range values {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		ordered[index] = value
	}
	if err := sortPairHypotheses(ordered, make([]pairMatch, len(ordered)), 0, len(ordered), o, budget); err != nil {
		return nil, err
	}
	var anchors, winners []pairMatch
	for _, value := range ordered {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		cluster := -1
		for index, anchor := range anchors {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			if equivalentPair(anchor, value, o) {
				cluster = index
				break
			}
		}
		if cluster < 0 {
			anchors = append(anchors, value)
			winners = append(winners, value)
			if len(winners) > o.MaxCandidatesPerPair {
				return nil, fmt.Errorf("%w: distinct pair hypotheses", ErrLimit)
			}
			continue
		}
		if err := budget.spend(); err != nil {
			return nil, err
		}
		if preferPair(value, winners[cluster], o) {
			winners[cluster] = value
		}
	}
	result := make([]pairMatch, len(winners))
	for index, winner := range winners {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		winner.phaseAnchorOffset, winner.phaseGrouped = anchors[index].offset, true
		winner.phaseClass = pairHypothesisClass(anchors[index])
		if winner.reasons != nil {
			winner.reasons = append([]Reason{}, winner.reasons...)
		}
		if winner.audio.reasons != nil {
			winner.audio.reasons = append([]Reason{}, winner.audio.reasons...)
		}
		result[index] = winner
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// An error-aware merge sort can stop immediately on budget exhaustion without
// changing a comparison function's ordering halfway through a library sort.
func sortPairHypotheses(values, scratch []pairMatch, start, end int, o Options, budget *workBudget) error {
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := sortPairHypotheses(values, scratch, start, middle, o, budget); err != nil {
		return err
	}
	if err := sortPairHypotheses(values, scratch, middle, end, o, budget); err != nil {
		return err
	}
	left, right := start, middle
	for index := start; index < end; index++ {
		if err := budget.spend(); err != nil {
			return err
		}
		if right == end || left < middle && !canonicalPairHypothesisLess(values[right], values[left], o) {
			scratch[index] = values[left]
			left++
		} else {
			scratch[index] = values[right]
			right++
		}
	}
	copy(values[start:end], scratch[start:end])
	return nil
}

func canonicalPairHypothesisLess(a, b pairMatch, o Options) bool {
	for _, values := range [][2]int64{
		{a.offset, b.offset}, {a.a.StartTicks, b.a.StartTicks}, {a.a.EndTicks, b.a.EndTicks},
		{a.b.StartTicks, b.b.StartTicks}, {a.b.EndTicks, b.b.EndTicks},
	} {
		if values[0] != values[1] {
			return values[0] < values[1]
		}
	}
	if preferPair(a, b, o) {
		return true
	}
	if preferPair(b, a, o) {
		return false
	}
	// The preserved original audio observation is part of the complete witness,
	// even when its final visual interval and summarized metrics are identical.
	for _, values := range [][2]int64{
		{a.audio.a.StartTicks, b.audio.a.StartTicks}, {a.audio.a.EndTicks, b.audio.a.EndTicks},
		{a.audio.b.StartTicks, b.audio.b.StartTicks}, {a.audio.b.EndTicks, b.audio.b.EndTicks},
	} {
		if values[0] != values[1] {
			return values[0] < values[1]
		}
	}
	if order := hypothesisStrength(a.audio.metrics, b.audio.metrics); order != 0 {
		return order > 0
	}
	if order := hypothesisMetricTie(a.audio.metrics, b.audio.metrics); order != 0 {
		return order < 0
	}
	if order := hypothesisReasons(a.audio.reasons, b.audio.reasons); order != 0 {
		return order < 0
	}
	if order := hypothesisRawReasons(a.reasons, b.reasons); order != 0 {
		return order < 0
	}
	return hypothesisRawReasons(a.audio.reasons, b.audio.reasons) < 0
}

func hypothesisRawReasons(a, b []Reason) int {
	for index := 0; index < min(len(a), len(b)); index++ {
		if a[index] < b[index] {
			return -1
		}
		if a[index] > b[index] {
			return 1
		}
	}
	if len(a) < len(b) || len(a) == len(b) && a == nil && b != nil {
		return -1
	}
	if len(a) > len(b) || len(a) == len(b) && a != nil && b == nil {
		return 1
	}
	return 0
}

// preferPair selects one complete observation. Strong individual metrics from
// an ineligible observation never repair another observation's missing proof.
func preferPair(candidate, prior pairMatch, o Options) bool {
	candidateQualified := len(candidate.reasons) == 0 && hypothesisQualifiedInterval(candidate.a, o) &&
		hypothesisQualifiedInterval(candidate.b, o) && qualifiedEvidence(candidate.metrics, o)
	priorQualified := len(prior.reasons) == 0 && hypothesisQualifiedInterval(prior.a, o) &&
		hypothesisQualifiedInterval(prior.b, o) && qualifiedEvidence(prior.metrics, o)
	if candidateQualified != priorQualified {
		return candidateQualified
	}
	if order := hypothesisStrength(candidate.metrics, prior.metrics); order != 0 {
		return order > 0
	}
	for _, values := range [][2]int64{
		{candidate.a.StartTicks, prior.a.StartTicks}, {candidate.a.EndTicks, prior.a.EndTicks},
		{candidate.b.StartTicks, prior.b.StartTicks}, {candidate.b.EndTicks, prior.b.EndTicks},
		{candidate.offset, prior.offset}, {int64(candidate.left), int64(prior.left)}, {int64(candidate.right), int64(prior.right)},
	} {
		if values[0] != values[1] {
			return values[0] < values[1]
		}
	}
	if order := hypothesisReasons(candidate.reasons, prior.reasons); order != 0 {
		return order < 0
	}
	return hypothesisMetricTie(candidate.metrics, prior.metrics) < 0
}

func preferGroup(candidate, prior Group, o Options) bool {
	candidateQualified, priorQualified := hypothesisQualifiedGroup(candidate, o), hypothesisQualifiedGroup(prior, o)
	if candidateQualified != priorQualified {
		return candidateQualified
	}
	if order := hypothesisStrength(candidate.Metrics, prior.Metrics); order != 0 {
		return order > 0
	}
	// Extra independent witnesses break an otherwise equal evidence tie; their
	// observations remain together instead of being combined with another group.
	if len(candidate.Members) != len(prior.Members) {
		return len(candidate.Members) > len(prior.Members)
	}
	candidateMembers, priorMembers := hypothesisMembers(candidate.Members), hypothesisMembers(prior.Members)
	for index := range candidateMembers {
		if order := hypothesisCompareMember(candidateMembers[index], priorMembers[index]); order != 0 {
			return order < 0
		}
	}
	if order := hypothesisReasons(candidate.Reasons, prior.Reasons); order != 0 {
		return order < 0
	}
	if order := hypothesisMetricTie(candidate.Metrics, prior.Metrics); order != 0 {
		return order < 0
	}
	for _, values := range [][2]string{
		{candidate.AlgorithmProfile, prior.AlgorithmProfile}, {string(candidate.Status), string(prior.Status)}, {candidate.ID, prior.ID},
	} {
		if values[0] != values[1] {
			return values[0] < values[1]
		}
	}
	return false
}

func hypothesisQualifiedInterval(interval Interval, o Options) bool {
	return interval.StartTicks >= 0 && interval.EndTicks >= interval.StartTicks &&
		interval.EndTicks-interval.StartTicks >= o.AutoMinDurationTicks
}

func hypothesisQualifiedGroup(group Group, o Options) bool {
	if len(group.Reasons) != 0 || len(group.Members) < o.MinSupport || !qualifiedEvidence(group.Metrics, o) {
		return false
	}
	for _, member := range group.Members {
		if !hypothesisQualifiedInterval(member.Interval, o) {
			return false
		}
	}
	return true
}

// Positive order means stronger joint evidence. Later fields cannot outweigh
// an earlier field, and this ordering never synthesizes a new metric tuple.
func hypothesisStrength(a, b Metrics) int {
	for _, values := range [][2]int{
		{a.AudioAgreementPermille, b.AudioAgreementPermille},
		{a.AudioSimilarityPermille, b.AudioSimilarityPermille},
		{a.AudioInformativePermille, b.AudioInformativePermille},
		{a.VisualMinBandMatchedPermille, b.VisualMinBandMatchedPermille},
		{a.VisualMatchedTimePermille, b.VisualMatchedTimePermille},
		{b.VisualContradictedTimePermille, a.VisualContradictedTimePermille},
		{b.VisualUnobservableTimePermille, a.VisualUnobservableTimePermille},
	} {
		if values[0] > values[1] {
			return 1
		}
		if values[0] < values[1] {
			return -1
		}
	}
	if a.BoundaryUncertaintyTicks < b.BoundaryUncertaintyTicks {
		return 1
	}
	if a.BoundaryUncertaintyTicks > b.BoundaryUncertaintyTicks {
		return -1
	}
	return 0
}

// Remaining diagnostics resolve exact strength and timeline ties only. Their
// numeric order is a stable witness key, not an additional eligibility policy.
func hypothesisMetricTie(a, b Metrics) int {
	for _, values := range [][2]int64{
		{int64(a.AudioSamples), int64(b.AudioSamples)}, {int64(a.AudioDistinct), int64(b.AudioDistinct)},
		{int64(a.VisualAgreementPermille), int64(b.VisualAgreementPermille)}, {int64(a.VisualSimilarityPermille), int64(b.VisualSimilarityPermille)},
		{int64(a.VisualCoveragePermille), int64(b.VisualCoveragePermille)}, {int64(a.VisualSamples), int64(b.VisualSamples)},
		{int64(a.VisualTransitions), int64(b.VisualTransitions)}, {int64(a.VisualChangeCoveragePermille), int64(b.VisualChangeCoveragePermille)},
		{int64(a.VisualDominancePermille), int64(b.VisualDominancePermille)}, {int64(a.PairCount), int64(b.PairCount)},
		{int64(a.VisualAnchorCount), int64(b.VisualAnchorCount)}, {a.VisualMaxUnconfirmedGapTicks, b.VisualMaxUnconfirmedGapTicks},
		{a.VisualStartAnchorGapTicks, b.VisualStartAnchorGapTicks}, {a.VisualEndAnchorGapTicks, b.VisualEndAnchorGapTicks},
		{int64(a.VisualDistinctStates), int64(b.VisualDistinctStates)}, {int64(a.VisualDominantStatePermille), int64(b.VisualDominantStatePermille)},
	} {
		if values[0] < values[1] {
			return -1
		}
		if values[0] > values[1] {
			return 1
		}
	}
	return 0
}

func hypothesisReasons(a, b []Reason) int {
	left, right := append([]Reason(nil), a...), append([]Reason(nil), b...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for index := 0; index < min(len(left), len(right)); index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func hypothesisMembers(values []Support) []Support {
	result := append([]Support(nil), values...)
	sort.Slice(result, func(i, j int) bool { return hypothesisCompareMember(result[i], result[j]) < 0 })
	return result
}

func hypothesisCompareMember(a, b Support) int {
	for _, values := range [][2]string{{a.SourceKey, b.SourceKey}, {a.EpisodeKey, b.EpisodeKey}, {a.ContentIdentity, b.ContentIdentity}} {
		if values[0] < values[1] {
			return -1
		}
		if values[0] > values[1] {
			return 1
		}
	}
	for _, values := range [][2]int64{{a.Interval.StartTicks, b.Interval.StartTicks}, {a.Interval.EndTicks, b.Interval.EndTicks}} {
		if values[0] < values[1] {
			return -1
		}
		if values[0] > values[1] {
			return 1
		}
	}
	return 0
}

// consistentOffsets fixes the smallest source index at zero. Each other clock
// is its actual reference edge's right-minus-left offset; no fitting or warping
// can conceal a contradictory edge elsewhere in the complete clique.
func consistentOffsets(selected []int, edges map[[2]int]pairMatch, o Options) (map[int]int64, bool) {
	if len(selected) == 0 || len(selected) > o.MaxEpisodes || o.AudioAlignmentTicks < 0 {
		return nil, false
	}
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	for index, node := range ordered {
		if node < 0 || index > 0 && node == ordered[index-1] {
			return nil, false
		}
	}
	reference := ordered[0]
	clocks := make(map[int]int64, len(ordered))
	clocks[reference] = 0
	for _, node := range ordered[1:] {
		offset, ok := hypothesisEdgeOffset(edges, reference, node)
		if !ok {
			return nil, false
		}
		clocks[node] = offset
	}
	for i, left := range ordered {
		for _, right := range ordered[i+1:] {
			observed, ok := hypothesisEdgeOffset(edges, left, right)
			if !ok {
				return nil, false
			}
			expected, ok := hypothesisSubtractTicks(clocks[right], clocks[left])
			if !ok || !hypothesisTicksWithin(observed, expected, o.AudioAlignmentTicks) {
				return nil, false
			}
		}
	}
	return clocks, true
}

func hypothesisEdgeOffset(edges map[[2]int]pairMatch, left, right int) (int64, bool) {
	edge, exists := edges[[2]int{min(left, right), max(left, right)}]
	if !exists {
		return 0, false
	}
	if edge.left == left && edge.right == right {
		return edge.offset, true
	}
	if edge.left == right && edge.right == left {
		return hypothesisSubtractTicks(0, edge.offset)
	}
	return 0, false
}

func hypothesisSubtractTicks(a, b int64) (int64, bool) {
	if b > 0 && a < (-1<<63)+b || b < 0 && a > (1<<63-1)+b {
		return 0, false
	}
	return a - b, true
}

func hypothesisTicksWithin(a, b, tolerance int64) bool {
	if tolerance < 0 {
		return false
	}
	if a >= b {
		return uint64(a)-uint64(b) <= uint64(tolerance)
	}
	return uint64(b)-uint64(a) <= uint64(tolerance)
}
