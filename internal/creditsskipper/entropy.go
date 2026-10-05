// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "sort"

func IsCreditCardKeyframe(visual KeyframeVisual) bool {
	return visual.Entropy < 0.35 && visual.Saturation < 96
}

// FindEntropyRange preserves the latest qualifying run, the non-card gap
// requirement, lower-quartile cadence trimming, and 50% card-density gate.
func FindEntropyRange(visuals []KeyframeVisual, minimumDuration int) *Range {
	var best *Range
	var cards []KeyframeVisual
	nonCard := false
	for _, v := range visuals {
		if !IsCreditCardKeyframe(v) {
			nonCard = true
			continue
		}
		if len(cards) > 0 && v.Time-cards[len(cards)-1].Time > MaximumSceneMergeGapSeconds && nonCard {
			best = selectEntropyRun(best, cards, visuals, minimumDuration)
			cards = nil
		}
		cards = append(cards, v)
		nonCard = false
	}
	return selectEntropyRun(best, cards, visuals, minimumDuration)
}
func selectEntropyRun(best *Range, cards, visuals []KeyframeVisual, minimumDuration int) *Range {
	if len(cards) == 0 {
		return best
	}
	start, end := 0, len(cards)-1
	if end >= 1 {
		gaps := make([]float64, 0, len(cards)-1)
		for i := 1; i < len(cards); i++ {
			gaps = append(gaps, cards[i].Time-cards[i-1].Time)
		}
		sort.Float64s(gaps)
		trimGap := gaps[len(gaps)/4] * 2.5
		for start < end && cards[start+1].Time-cards[start].Time > trimGap {
			start++
		}
		for end > start && cards[end].Time-cards[end-1].Time > trimGap {
			end--
		}
		if cards[end].Time-cards[start].Time < float64(minimumDuration) {
			start, end = 0, len(cards)-1
		}
	}
	r := Range{cards[start].Time, cards[end].Time}
	if r.Duration() < float64(minimumDuration) {
		return best
	}
	total, count := 0, 0
	for _, v := range visuals {
		if v.Time >= r.Start && v.Time <= r.End {
			total++
			if IsCreditCardKeyframe(v) {
				count++
			}
		}
	}
	if total == 0 || float64(count)/float64(total) < 0.5 {
		return best
	}
	return &r
}
