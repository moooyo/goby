package introdetect

import (
	"math/bits"
	"sort"
)

type pairMatch struct {
	left, right int
	a, b        Interval
	metrics     Metrics
	reasons     []Reason
}

func visualConfirm(a, b Episode, audio audioMatch, offset int64, o Options, budget *workBudget) (*pairMatch, Reason, error) {
	start := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.StartTicks })
	end := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.EndTicks })
	if end-start < o.MinVisualSamples || len(b.Visual) < o.MinVisualSamples {
		return nil, InsufficientVisual, nil
	}
	var usable, good, distance, transitions int
	uniqueA, uniqueB := make(map[uint64]int), make(map[uint64]int)
	var changingTime int64
	firstA, lastA, firstB, lastB := int64(-1), int64(-1), int64(-1), int64(-1)
	previousA, previousB := -1, -1
	previousMatched := false
	maximumGap := int64(0)
	j, lastTarget := 0, -1
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return nil, "", err
		}
		frame := a.Visual[i]
		target := frame.Ticks + offset
		for j+1 < len(b.Visual) && absolute(b.Visual[j+1].Ticks-target) <= absolute(b.Visual[j].Ticks-target) {
			j++
		}
		if j >= len(b.Visual) || j <= lastTarget || b.Visual[j].Ticks < audio.b.StartTicks || b.Visual[j].Ticks >= audio.b.EndTicks ||
			absolute(b.Visual[j].Ticks-target) > o.VisualAlignmentTicks || frame.Contrast < o.MinVisualContrast || b.Visual[j].Contrast < o.MinVisualContrast {
			continue
		}
		lastTarget = j
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
		if firstA < 0 {
			firstA, firstB = frame.Ticks, b.Visual[j].Ticks
		}
		lastA, lastB = frame.Ticks, b.Visual[j].Ticks
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
	if len(uniqueA) < 4 || len(uniqueB) < 4 || transitions < o.MinVisualTransitions || changeCoverage < o.MinVisualChangeCoverage || dominant > o.MaxVisualDominance {
		return nil, LowVisualDiversity, nil
	}
	if firstA-audio.a.StartTicks > o.MaxVisualGapTicks || audio.a.EndTicks-lastA > o.MaxVisualGapTicks ||
		firstB-audio.b.StartTicks > o.MaxVisualGapTicks || audio.b.EndTicks-lastB > o.MaxVisualGapTicks {
		return nil, InsufficientVisual, nil
	}
	metrics := audio.metrics
	metrics.VisualAgreementPermille, metrics.VisualSimilarityPermille = agreement, 1000-distance*1000/(usable*64)
	metrics.VisualCoveragePermille, metrics.VisualSamples, metrics.VisualTransitions = coverage, usable, transitions
	metrics.VisualChangeCoveragePermille, metrics.VisualDominancePermille = changeCoverage, dominant
	metrics.BoundaryUncertaintyTicks = max(metrics.BoundaryUncertaintyTicks, maximumGap, o.AudioAlignmentTicks)
	reasons := append([]Reason(nil), audio.reasons...)
	if agreement < o.MinVisualAgreement || metrics.VisualSimilarityPermille < o.MinVisualSimilarity {
		reasons = addReason(reasons, WeakVisualEvidence)
	}
	// Use only the intersection of acoustic interior and confirmed visual time.
	// No guessed extrapolation extends the skip past the last confirmed frame.
	arange := Interval{max(audio.a.StartTicks, firstA), min(audio.a.EndTicks, lastA)}
	brange := Interval{max(audio.b.StartTicks, firstB), min(audio.b.EndTicks, lastB)}
	if arange.EndTicks-arange.StartTicks < o.MinDurationTicks || brange.EndTicks-brange.StartTicks < o.MinDurationTicks {
		return nil, InsufficientVisual, nil
	}
	if min(arange.EndTicks-arange.StartTicks, brange.EndTicks-brange.StartTicks) < o.AutoMinDurationTicks {
		reasons = addReason(reasons, ShortInterval)
	}
	return &pairMatch{a: arange, b: brange, metrics: metrics, reasons: reasons}, "", nil
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
	}
}
