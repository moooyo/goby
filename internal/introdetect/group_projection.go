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
			fmt.Fprintf(hash, "pair:%d:%d:%v:%v:%d:%d:%t:%s:%v:%v:%v;", left, right, edge.a, edge.b, edge.offset,
				edge.phaseAnchorOffset, edge.phaseGrouped, edge.phaseClass, edge.metrics, edge.reasons, edge.audio)
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

// projectGroup retains the selected complete witness and rechecks its final
// intersection under one source-clock mapping. Quality measurements describe
// that intersection; boundary, periodicity and search-limit facts survive it.
func projectGroup(episodes []Episode, selected []int, ranges map[int]Interval, edges map[[2]int]pairMatch, o Options, budget *workBudget) (*Group, error) {
	clocks, valid, err := checkOffsetBudget(selected, edges, o, budget)
	if err != nil || !valid {
		return nil, err
	}
	clocks, err = refineCliqueClocks(selected, edges, clocks, o, budget)
	if err != nil {
		return nil, err
	}
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	for round := 0; round < 2*o.MaxEpisodes+2; round++ {
		next := make(map[int]Interval, len(ranges))
		for source, interval := range ranges {
			if interval.EndTicks-interval.StartTicks < o.MinDurationTicks {
				return nil, nil
			}
			next[source] = interval
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
				observed, _, err := visualConfirm(episodes[left], episodes[right], *audio, offset, o, budget)
				if err != nil || observed == nil {
					return nil, err
				}
				next[left], next[right] = intersect(next[left], observed.a), intersect(next[right], observed.b)
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
		stable := true
		for _, node := range ordered {
			stable = stable && next[node] == ranges[node]
		}
		if !stable {
			ranges = next
			continue
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
		return group, nil
	}
	return nil, fmt.Errorf("%w: final boundary projection", ErrLimit)
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
