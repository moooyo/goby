package introdetect

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"sort"
)

// Different seed edges often construct the same complete witness. Reusing its
// exact projection avoids charging an identical dense clique once per seed.
func groupProjectionKey(selected []int, ranges map[int]Interval, edges map[[2]int]pairMatch, budget *workBudget) (string, error) {
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	hash := sha256.New()
	for index, left := range ordered {
		interval := ranges[left]
		fmt.Fprintf(hash, "node:%d:%d:%d;", left, interval.StartTicks, interval.EndTicks)
		for _, right := range ordered[index+1:] {
			if err := budget.spend(); err != nil {
				return "", err
			}
			edge := edges[[2]int{left, right}]
			fmt.Fprintf(hash, "pair:%d:%d:%v:%v:%d:%d:%d:%t:%s:%v:%v:%v;", left, right, edge.a, edge.b, edge.offset,
				edge.visualPhase, edge.phaseAnchorOffset, edge.phaseGrouped, edge.phaseClass, edge.metrics, edge.reasons, edge.audio)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func detachedGroup(value *Group) *Group {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.Members = append([]Support{}, value.Members...)
	copyValue.Reasons = append([]Reason{}, value.Reasons...)
	copyValue.alignmentOffsets = append([]int64{}, value.alignmentOffsets...)
	copyValue.phaseAnchors = make(map[[2]string]int64, len(value.phaseAnchors))
	for key, offset := range value.phaseAnchors {
		copyValue.phaseAnchors[key] = offset
	}
	copyValue.phaseClasses = make(map[[2]string]string, len(value.phaseClasses))
	for key, class := range value.phaseClasses {
		copyValue.phaseClasses[key] = class
	}
	return &copyValue
}

// projectAudioEvidence measures the actual final interval without applying the
// extraction guard a second time. Partial edge bins receive no invented credit.
func projectAudioEvidence(a, b Episode, ar, br Interval, offset int64, original audioMatch, o Options, budget *workBudget) (*audioMatch, error) {
	var count, distance, adjacent, changesA, changesB int
	var coveredA, coveredB, informativeA, informativeB int64
	uniqueA, uniqueB := map[uint32]bool{}, map[uint32]bool{}
	start := sort.Search(len(a.Audio), func(i int) bool { return a.Audio[i].StartTicks >= ar.StartTicks })
	j, previousI, previousJ := 0, -1, -1
	lastTarget := -1
	for i := start; i < len(a.Audio) && a.Audio[i].EndTicks <= ar.EndTicks; i++ {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		sample := a.Audio[i]
		target := sample.StartTicks + offset
		for j+1 < len(b.Audio) && absolute(b.Audio[j+1].StartTicks-target) <= absolute(b.Audio[j].StartTicks-target) {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			j++
		}
		if j >= len(b.Audio) || j <= lastTarget || b.Audio[j].StartTicks < br.StartTicks || b.Audio[j].EndTicks > br.EndTicks ||
			absolute(b.Audio[j].StartTicks-target) > o.AudioAlignmentTicks || bits.OnesCount32(sample.Fingerprint^b.Audio[j].Fingerprint) > o.MaxAudioHamming {
			continue
		}
		if previousI >= 0 && (sample.StartTicks-a.Audio[previousI].EndTicks > o.MaxAudioGapTicks || b.Audio[j].StartTicks-b.Audio[previousJ].EndTicks > o.MaxAudioGapTicks) {
			return nil, nil
		}
		spanA, spanB := sample.EndTicks-sample.StartTicks, b.Audio[j].EndTicks-b.Audio[j].StartTicks
		coveredA, coveredB = coveredA+spanA, coveredB+spanB
		if informativeAudio(a.Audio, i) && informativeAudio(b.Audio, j) {
			informativeA, informativeB = informativeA+spanA, informativeB+spanB
		}
		if i == previousI+1 && j == previousJ+1 && previousI >= 0 {
			adjacent++
			changesA += bits.OnesCount32(sample.Fingerprint ^ a.Audio[previousI].Fingerprint)
			changesB += bits.OnesCount32(b.Audio[j].Fingerprint ^ b.Audio[previousJ].Fingerprint)
		}
		uniqueA[sample.Fingerprint], uniqueB[b.Audio[j].Fingerprint] = true, true
		count++
		distance += bits.OnesCount32(sample.Fingerprint ^ b.Audio[j].Fingerprint)
		previousI, previousJ, lastTarget = i, j, j
	}
	if count == 0 || min(len(uniqueA), len(uniqueB)) < 12 || adjacent == 0 || min(changesA, changesB) < 2*adjacent {
		return nil, nil
	}
	information := min(int(informativeA*1000/max(int64(1), coveredA)), int(informativeB*1000/max(int64(1), coveredB)))
	agreement := min(int(coveredA*1000/(ar.EndTicks-ar.StartTicks)), int(coveredB*1000/(br.EndTicks-br.StartTicks)))
	if information < o.MinAudioInformation || agreement < 800 {
		return nil, nil
	}
	metrics := Metrics{AudioAgreementPermille: min(1000, agreement), AudioInformativePermille: information,
		AudioSimilarityPermille: 1000 - distance*1000/(count*32), AudioSamples: count,
		AudioDistinct: min(len(uniqueA), len(uniqueB)), BoundaryUncertaintyTicks: original.metrics.BoundaryUncertaintyTicks, PairCount: 1}
	reasons := audioReasonsForInterval(original.reasons, metrics, o)
	return &audioMatch{a: ar, b: br, metrics: metrics, reasons: reasons}, nil
}

func checkOffsetBudget(selected []int, edges map[[2]int]pairMatch, o Options, budget *workBudget) (map[int]int64, bool, error) {
	for i := 0; i < len(selected)*(len(selected)-1)/2; i++ {
		if err := budget.spend(); err != nil {
			return nil, false, err
		}
	}
	clocks, ok := consistentOffsets(selected, edges, o)
	return clocks, ok, nil
}

// projectGroup preserves a qualified admitted mapping. The one geometry-only
// refinement is an alternative complete witness, never a source of individual
// replacement metrics, intervals, or reasons.
func projectGroup(episodes []Episode, selected []int, ranges map[int]Interval, edges map[[2]int]pairMatch, o Options, budget *workBudget) (*Group, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	clocks, valid, err := checkOffsetBudget(selected, edges, o, budget)
	if err != nil || !valid {
		return nil, err
	}
	baseline, err := projectGroupWithClocks(episodes, selected, ranges, edges, clocks, o, budget)
	if err != nil {
		return nil, err
	}
	if baseline != nil {
		for range baseline.Members {
			if err := budget.spend(); err != nil {
				return nil, err
			}
		}
		if hypothesisQualifiedGroup(*baseline, o) {
			if err := budget.ctx.Err(); err != nil {
				return nil, err
			}
			return baseline, nil
		}
	}
	refinedClocks, err := refineCliqueClocks(selected, edges, clocks, o, budget)
	if err != nil {
		return nil, err
	}
	same := true
	for _, node := range selected {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		same = same && clocks[node] == refinedClocks[node]
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if same {
		return baseline, nil
	}
	refined, err := projectGroupWithClocks(episodes, selected, ranges, edges, refinedClocks, o, budget)
	if err != nil {
		return nil, err
	}
	chosen := baseline
	if baseline == nil {
		chosen = refined
	} else if refined != nil {
		// Charge bounded containment/alignment comparisons and the existing
		// whole-group ordering, including its member sorts, before using them.
		members := max(len(baseline.Members), len(refined.Members))
		checks := len(refined.phaseAnchors) + len(refined.phaseClasses) + 4*members*members + 2*members
		for i := 0; i < checks; i++ {
			if err := budget.spend(); err != nil {
				return nil, err
			}
		}
		if len(baseline.Members) == len(refined.Members) && groupContained(*refined, *baseline, o) &&
			compatibleGroupAlignment(*refined, *baseline, o) && preferGroup(*refined, *baseline, o) {
			chosen = refined
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	return chosen, nil
}

// intersectClockRanges chooses the unique common reference-time interval of
// one complete clock witness. It never searches metrics or band positions.
// Each mapped interval must stay inside its immutable source cap.
func intersectClockRanges(selected []int, caps map[int]Interval, clocks map[int]int64, o Options, budget *workBudget) (map[int]Interval, error) {
	if budget == nil || budget.ctx == nil {
		return nil, fmt.Errorf("%w: clock intersection budget", ErrInvalidInput)
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if len(selected) == 0 || len(selected) > o.MaxEpisodes || len(caps) != len(selected) || len(clocks) != len(selected) || o.MinDurationTicks <= 0 {
		return nil, fmt.Errorf("%w: clock intersection witness", ErrInvalidInput)
	}
	seen := make(map[int]bool, len(selected))
	var common Interval
	for index, node := range selected {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		cap, capExists := caps[node]
		clock, clockExists := clocks[node]
		if node < 0 || seen[node] || !capExists || !clockExists || cap.StartTicks < 0 || cap.EndTicks <= cap.StartTicks {
			return nil, fmt.Errorf("%w: clock intersection member", ErrInvalidInput)
		}
		seen[node] = true
		start, startOK := hypothesisSubtractTicks(cap.StartTicks, clock)
		end, endOK := hypothesisSubtractTicks(cap.EndTicks, clock)
		if !startOK || !endOK {
			return nil, fmt.Errorf("%w: clock intersection reference arithmetic", ErrInvalidInput)
		}
		if index == 0 {
			common = Interval{start, end}
		} else {
			common.StartTicks, common.EndTicks = max(common.StartTicks, start), min(common.EndTicks, end)
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if common.EndTicks <= common.StartTicks {
		return nil, nil
	}
	duration, valid := hypothesisSubtractTicks(common.EndTicks, common.StartTicks)
	if !valid {
		return nil, fmt.Errorf("%w: clock intersection duration arithmetic", ErrInvalidInput)
	}
	if duration < o.MinDurationTicks {
		return nil, nil
	}
	ranges := make(map[int]Interval, len(selected))
	for _, node := range selected {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		start, startOK := addConsensusTicks(common.StartTicks, clocks[node])
		end, endOK := addConsensusTicks(common.EndTicks, clocks[node])
		cap := caps[node]
		if !startOK || !endOK || start < cap.StartTicks || end > cap.EndTicks || end <= start {
			return nil, fmt.Errorf("%w: clock intersection escaped source cap", ErrInvalidInput)
		}
		ranges[node] = Interval{start, end}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	return ranges, nil
}

// projectGroupWithClocks rechecks every edge once on the common reference-time
// intersection under one fixed mapping. Each mapping starts with the same
// immutable caps and cannot inherit measurements from another witness.
func projectGroupWithClocks(episodes []Episode, selected []int, initial map[int]Interval, edges map[[2]int]pairMatch, clocks map[int]int64, o Options, budget *workBudget) (*Group, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	ranges, err := intersectClockRanges(ordered, initial, clocks, o, budget)
	if err != nil || ranges == nil {
		return nil, err
	}
	metrics, reasons := Metrics{}, []Reason{}
	for index, left := range ordered {
		for _, right := range ordered[index+1:] {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			original := edges[[2]int{left, right}]
			offset := clocks[right] - clocks[left]
			audio, err := projectAudioEvidence(episodes[left], episodes[right], ranges[left], ranges[right], offset, original.audio, o, budget)
			if err != nil || audio == nil {
				return nil, err
			}
			observed, _, err := projectVisualEvidence(episodes[left], episodes[right], *audio, offset, original, o, budget)
			if err != nil || observed == nil {
				return nil, err
			}
			pairMetrics := observed.metrics
			pairMetrics.PairCount = 1
			metrics = conservativeMetrics(metrics, pairMetrics)
			for _, reason := range original.reasons {
				if !intervalQualityReason(reason) {
					reasons = addReason(reasons, reason)
				}
			}
			for _, reason := range observed.reasons {
				reasons = addReason(reasons, reason)
			}
		}
	}
	group := &Group{AlgorithmProfile: episodes[ordered[0]].AlgorithmProfile, Status: Qualified,
		Metrics: metrics, Reasons: reasons, Members: make([]Support, 0, len(ordered)), alignmentOffsets: make([]int64, 0, len(ordered)),
		phaseAnchors: make(map[[2]string]int64, len(edges)), phaseClasses: make(map[[2]string]string, len(edges))}
	for key, edge := range edges {
		anchor := edge.offset
		if edge.phaseGrouped {
			anchor = edge.phaseAnchorOffset
		}
		pair := [2]string{episodes[key[0]].SourceKey, episodes[key[1]].SourceKey}
		group.phaseAnchors[pair] = anchor
		class := edge.phaseClass
		if class == "" {
			class = pairHypothesisClass(edge)
		}
		group.phaseClasses[pair] = class
	}
	for _, node := range ordered {
		interval := ranges[node]
		if interval.EndTicks-interval.StartTicks < o.MinDurationTicks {
			return nil, nil
		}
		if interval.EndTicks-interval.StartTicks < o.AutoMinDurationTicks {
			group.Reasons = addReason(group.Reasons, ShortInterval)
		}
		group.Members = append(group.Members, Support{episodes[node].EpisodeKey, episodes[node].SourceKey, episodes[node].ContentIdentity, interval})
		group.alignmentOffsets = append(group.alignmentOffsets, clocks[node])
	}
	if len(group.Reasons) != 0 || !qualifiedEvidence(group.Metrics, o) {
		group.Status = Review
		if len(group.Reasons) == 0 {
			group.Reasons = addReason(group.Reasons, WeakVisualEvidence)
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	return group, nil
}

func compatibleGroupAlignment(a, b Group, o Options) bool {
	if len(a.alignmentOffsets) != len(a.Members) || len(b.alignmentOffsets) != len(b.Members) {
		return false
	}
	for pair, anchor := range a.phaseAnchors {
		if other, shared := b.phaseAnchors[pair]; shared && !hypothesisTicksWithin(anchor, other, o.AudioAlignmentTicks) {
			return false
		}
	}
	for pair, class := range a.phaseClasses {
		if other, shared := b.phaseClasses[pair]; shared && class != other {
			return false
		}
	}
	var translation int64
	found := false
	for i, member := range a.Members {
		for j, other := range b.Members {
			if member.SourceKey != other.SourceKey {
				continue
			}
			difference := a.alignmentOffsets[i] - b.alignmentOffsets[j]
			if !found {
				translation, found = difference, true
			} else if !hypothesisTicksWithin(difference, translation, o.AudioAlignmentTicks) {
				return false
			}
		}
	}
	return found
}
