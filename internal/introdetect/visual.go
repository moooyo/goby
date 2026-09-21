package introdetect

import (
	"math/bits"
	"sort"
)

type pairMatch struct {
	left, right       int
	a, b              Interval
	offset            int64
	audio             audioMatch
	phaseAnchorOffset int64
	phaseGrouped      bool
	phaseClass        string
	metrics           Metrics
	reasons           []Reason
}

func visualConfirm(a, b Episode, audio audioMatch, offset int64, o Options, budget *workBudget) (*pairMatch, Reason, error) {
	start := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.StartTicks })
	end := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.EndTicks })
	if end-start < o.MinVisualSamples || len(b.Visual) < o.MinVisualSamples {
		return nil, InsufficientVisual, nil
	}
	alignment, err := alignVisual(a, b, audio, offset, o, budget)
	if err != nil {
		return nil, "", err
	}
	var usable, good, distance, transitions int
	uniqueA, uniqueB := make(map[uint64]int), make(map[uint64]int)
	var changingTime int64
	previousA, previousB := -1, -1
	previousMatched := false
	maximumGap := int64(0)
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return nil, "", err
		}
		frame := a.Visual[i]
		j := alignment.targets[i-alignment.start]
		if j < 0 || b.Visual[j].Ticks >= audio.b.EndTicks || frame.Contrast < o.MinVisualContrast || b.Visual[j].Contrast < o.MinVisualContrast {
			continue
		}
		usable++
		difference := bits.OnesCount64(frame.Hash ^ b.Visual[j].Hash)
		matched := difference <= o.MaxVisualHamming
		if previousA >= 0 {
			gapA, gapB := frame.Ticks-a.Visual[previousA].Ticks, b.Visual[j].Ticks-b.Visual[previousB].Ticks
			maximumGap = max(maximumGap, gapA, gapB)
			// Matching changes corroborate temporal structure. A static logo
			// with perfect pairwise hashes still has no scene evidence.
			if matched && previousMatched && i == previousA+1 && j == previousB+1 && gapA <= o.MaxVisualGapTicks && gapB <= o.MaxVisualGapTicks &&
				bits.OnesCount64(frame.Hash^a.Visual[previousA].Hash) >= 8 &&
				bits.OnesCount64(b.Visual[j].Hash^b.Visual[previousB].Hash) >= 8 {
				transitions++
				changingTime += min(gapA, gapB)
			}
		}
		previousA, previousB = i, j
		previousMatched = matched
		distance += difference
		if !matched {
			continue
		}
		good++
		uniqueA[frame.Hash]++
		uniqueB[b.Visual[j].Hash]++
	}
	coverage := usable * 1000 / (end - start)
	if usable < o.MinVisualSamples || coverage < 700 || maximumGap > o.MaxVisualGapTicks {
		return nil, InsufficientVisual, nil
	}
	agreement := good * 1000 / usable
	if good < o.MinVisualSamples || agreement < 650 {
		return nil, AudioWithoutVisual, nil
	}
	dominant := 0
	for _, hashes := range []map[uint64]int{uniqueA, uniqueB} {
		for _, count := range hashes {
			dominant = max(dominant, count*1000/good)
		}
	}
	changeCoverage := min(1000, int(changingTime*1000/min(audio.a.EndTicks-audio.a.StartTicks, audio.b.EndTicks-audio.b.StartTicks)))
	metrics := audio.metrics
	metrics.VisualAgreementPermille, metrics.VisualSimilarityPermille = agreement, 1000-distance*1000/(usable*64)
	metrics.VisualCoveragePermille, metrics.VisualSamples, metrics.VisualTransitions = coverage, usable, transitions
	metrics.VisualChangeCoveragePermille, metrics.VisualDominancePermille = changeCoverage, dominant
	metrics.BoundaryUncertaintyTicks = max(metrics.BoundaryUncertaintyTicks, maximumGap, o.AudioAlignmentTicks, alignment.maximumResidual)
	evidence, err := measureAlignedVisualV2(a, b, audio, alignment, o, budget)
	if err != nil {
		return nil, "", err
	}
	metrics.VisualAnchorCount = evidence.metrics.VisualAnchorCount
	metrics.VisualMinBandMatchedPermille = evidence.metrics.VisualMinBandMatchedPermille
	metrics.VisualMatchedTimePermille = evidence.metrics.VisualMatchedTimePermille
	metrics.VisualContradictedTimePermille = evidence.metrics.VisualContradictedTimePermille
	metrics.VisualUnobservableTimePermille = evidence.metrics.VisualUnobservableTimePermille
	metrics.VisualMaxUnconfirmedGapTicks = evidence.metrics.VisualMaxUnconfirmedGapTicks
	metrics.VisualStartAnchorGapTicks = evidence.metrics.VisualStartAnchorGapTicks
	metrics.VisualEndAnchorGapTicks = evidence.metrics.VisualEndAnchorGapTicks
	metrics.VisualDistinctStates = evidence.metrics.VisualDistinctStates
	metrics.VisualDominantStatePermille = evidence.metrics.VisualDominantStatePermille
	reasons := append([]Reason(nil), audio.reasons...)
	if metrics.VisualDistinctStates < o.MinVisualStates || metrics.VisualDominantStatePermille > o.MaxVisualStateDominancePermille {
		reasons = addReason(reasons, LowVisualDiversity)
	}
	if agreement < o.MinVisualAgreement || metrics.VisualSimilarityPermille < o.MinVisualSimilarity || metrics.VisualMatchedTimePermille < o.MinVisualAgreement {
		reasons = addReason(reasons, WeakVisualEvidence)
	}
	if !evidence.allBandsAnchored || metrics.VisualAnchorCount == 0 || metrics.VisualMinBandMatchedPermille < o.MinVisualBandMatchedPermille ||
		metrics.VisualMaxUnconfirmedGapTicks > o.MaxVisualUnconfirmedGapTicks || metrics.VisualStartAnchorGapTicks > o.MaxVisualAnchorEdgeGapTicks || metrics.VisualEndAnchorGapTicks > o.MaxVisualAnchorEdgeGapTicks {
		reasons = addReason(reasons, InsufficientVisualAnchors)
	}
	if evidence.periodic {
		reasons = addReason(reasons, PeriodicVisualEvidence)
	}
	// Use only the intersection of acoustic interior and confirmed visual time.
	// No guessed extrapolation extends the skip past the last confirmed frame.
	arange := Interval{max(audio.a.StartTicks, evidence.firstA), min(audio.a.EndTicks, evidence.lastA)}
	brange := Interval{max(audio.b.StartTicks, evidence.firstB), min(audio.b.EndTicks, evidence.lastB)}
	if arange.EndTicks-arange.StartTicks < o.MinDurationTicks || brange.EndTicks-brange.StartTicks < o.MinDurationTicks {
		return nil, InsufficientVisual, nil
	}
	if min(arange.EndTicks-arange.StartTicks, brange.EndTicks-brange.StartTicks) < o.AutoMinDurationTicks {
		reasons = addReason(reasons, ShortInterval)
	}
	return &pairMatch{a: arange, b: brange, offset: offset, audio: audio, metrics: metrics, reasons: reasons}, "", nil
}

func compatibleInterval(a, b Interval, o Options) bool {
	intersection := min(a.EndTicks, b.EndTicks) - max(a.StartTicks, b.StartTicks)
	if intersection <= 0 {
		return false
	}
	union := max(a.EndTicks, b.EndTicks) - min(a.StartTicks, b.StartTicks)
	return intersection*100 >= union*80 && absolute(a.StartTicks-b.StartTicks) <= o.BoundaryToleranceTicks &&
		absolute(a.EndTicks-b.EndTicks) <= o.BoundaryToleranceTicks
}

func intersect(a, b Interval) Interval {
	return Interval{max(a.StartTicks, b.StartTicks), min(a.EndTicks, b.EndTicks)}
}

func conservativeMetrics(a, b Metrics) Metrics {
	if a.PairCount == 0 {
		return b
	}
	return Metrics{
		AudioAgreementPermille:   min(a.AudioAgreementPermille, b.AudioAgreementPermille),
		AudioInformativePermille: min(a.AudioInformativePermille, b.AudioInformativePermille),
		AudioSimilarityPermille:  min(a.AudioSimilarityPermille, b.AudioSimilarityPermille),
		AudioSamples:             min(a.AudioSamples, b.AudioSamples), AudioDistinct: min(a.AudioDistinct, b.AudioDistinct),
		VisualAgreementPermille:  min(a.VisualAgreementPermille, b.VisualAgreementPermille),
		VisualSimilarityPermille: min(a.VisualSimilarityPermille, b.VisualSimilarityPermille),
		VisualCoveragePermille:   min(a.VisualCoveragePermille, b.VisualCoveragePermille),
		VisualSamples:            min(a.VisualSamples, b.VisualSamples), VisualTransitions: min(a.VisualTransitions, b.VisualTransitions),
		VisualChangeCoveragePermille: min(a.VisualChangeCoveragePermille, b.VisualChangeCoveragePermille),
		VisualDominancePermille:      max(a.VisualDominancePermille, b.VisualDominancePermille),
		BoundaryUncertaintyTicks:     max(a.BoundaryUncertaintyTicks, b.BoundaryUncertaintyTicks), PairCount: a.PairCount + b.PairCount,
		VisualAnchorCount:              min(a.VisualAnchorCount, b.VisualAnchorCount),
		VisualMinBandMatchedPermille:   min(a.VisualMinBandMatchedPermille, b.VisualMinBandMatchedPermille),
		VisualMatchedTimePermille:      min(a.VisualMatchedTimePermille, b.VisualMatchedTimePermille),
		VisualContradictedTimePermille: max(a.VisualContradictedTimePermille, b.VisualContradictedTimePermille),
		VisualUnobservableTimePermille: max(a.VisualUnobservableTimePermille, b.VisualUnobservableTimePermille),
		VisualMaxUnconfirmedGapTicks:   max(a.VisualMaxUnconfirmedGapTicks, b.VisualMaxUnconfirmedGapTicks),
		VisualStartAnchorGapTicks:      max(a.VisualStartAnchorGapTicks, b.VisualStartAnchorGapTicks),
		VisualEndAnchorGapTicks:        max(a.VisualEndAnchorGapTicks, b.VisualEndAnchorGapTicks),
		VisualDistinctStates:           min(a.VisualDistinctStates, b.VisualDistinctStates),
		VisualDominantStatePermille:    max(a.VisualDominantStatePermille, b.VisualDominantStatePermille),
	}
}
