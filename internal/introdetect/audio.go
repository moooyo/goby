package introdetect

import (
	"fmt"
	"math/bits"
	"sort"
)

const (
	maxVoteSamples = 192
	minimumVotes   = 4
	maxOffsetBins  = 16384
)

type audioIndex [4]map[byte][]int

func informativeAudio(samples []AudioSample, i int) bool {
	word := samples[i].Fingerprint
	weight := bits.OnesCount32(word)
	if weight < 4 || weight > 28 {
		return false
	}
	return i > 0 && bits.OnesCount32(word^samples[i-1].Fingerprint) >= 2 ||
		i+1 < len(samples) && bits.OnesCount32(word^samples[i+1].Fingerprint) >= 2
}

func indexAudio(samples []AudioSample) audioIndex {
	var index audioIndex
	for band := range index {
		index[band] = make(map[byte][]int)
	}
	for i, sample := range samples {
		if !informativeAudio(samples, i) {
			continue
		}
		for band := range index {
			key := byte(sample.Fingerprint >> (8 * band))
			// Retain every informative posting. Skipping dense buckets could
			// conceal a competing interval while another interval qualifies.
			// The shared comparison budget bounds expensive distributions.
			index[band][key] = append(index[band][key], i)
		}
	}
	return index
}

type offsetVote struct {
	bin      int64
	count    int
	total    int64
	distance int
}

type voteMatch struct {
	delta    int64
	distance int
}

func voteOffsets(a, b Episode, index audioIndex, o Options, budget *workBudget) ([]int64, bool, error) {
	votes := make(map[int64]offsetVote)
	minimumBin := int64(2 * TicksPerSecond)
	for _, samples := range [][]AudioSample{a.Audio, b.Audio} {
		for _, sample := range samples {
			minimumBin = min(minimumBin, sample.EndTicks-sample.StartTicks)
		}
	}
	// Broad votes must not average offsets that align different intervals.
	// Keep the vote and suppression scale below actual nearest-bin cadence.
	binWidth := min(o.OffsetBinTicks, max(int64(1), minimumBin/4))
	seenTarget := make([]int, len(b.Audio))
	stride := max(1, (len(a.Audio)+maxVoteSamples-1)/maxVoteSamples)
	for begin := 0; begin < len(a.Audio); begin += stride {
		if err := budget.ctx.Err(); err != nil {
			return nil, false, err
		}
		// Select an informative anchor within each sampling block rather than
		// repeatedly landing on a silent bin of a periodic input pattern.
		i, stop := begin, min(len(a.Audio), begin+stride)
		for i < stop && !informativeAudio(a.Audio, i) {
			i++
		}
		if i == stop {
			continue
		}
		word := a.Audio[i].Fingerprint
		// One anchor contributes at most one vote to each offset bin, even if
		// several bands or repeated target words lead to that bin.
		local := make(map[int64]voteMatch)
		for band := range index {
			// A word within Hamming distance D has at least one of its
			// four bytes within floor(D/4). Probe that radius, not only
			// exact bytes, so spread-out bit errors still produce votes.
			for _, key := range nearbyBytes(byte(word>>(8*band)), o.MaxAudioHamming/4) {
				positions := index[band][key]
				for _, j := range positions {
					if seenTarget[j] == i+1 {
						continue
					}
					seenTarget[j] = i + 1
					if err := budget.spend(); err != nil {
						return nil, false, err
					}
					distance := bits.OnesCount32(word ^ b.Audio[j].Fingerprint)
					if distance > o.MaxAudioHamming {
						continue
					}
					delta := b.Audio[j].StartTicks - a.Audio[i].StartTicks
					bin := delta / binWidth
					if delta < 0 && delta%binWidth != 0 {
						bin--
					}
					prior, exists := local[bin]
					if !exists || distance < prior.distance || distance == prior.distance && delta < prior.delta {
						local[bin] = voteMatch{delta, distance}
					}
				}
			}
		}
		for bin, match := range local {
			vote := votes[bin]
			vote.bin, vote.count, vote.total, vote.distance = bin, vote.count+1, vote.total+match.delta, vote.distance+match.distance
			votes[bin] = vote
			if len(votes) > maxOffsetBins {
				return nil, false, fmt.Errorf("%w: offset vote bins", ErrLimit)
			}
		}
	}
	ordered := make([]offsetVote, 0, len(votes))
	for _, vote := range votes {
		if vote.count >= minimumVotes {
			ordered = append(ordered, vote)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.count != b.count {
			return a.count > b.count
		}
		if a.distance != b.distance {
			return a.distance < b.distance
		}
		if absolute(a.bin) != absolute(b.bin) {
			return absolute(a.bin) < absolute(b.bin)
		}
		return a.bin < b.bin
	})
	var offsets []int64
	limited := false
	for _, vote := range ordered {
		offset := vote.total / int64(vote.count)
		near := false
		for _, prior := range offsets {
			near = near || absolute(offset-prior) <= binWidth
		}
		if near {
			continue
		}
		if len(offsets) == o.MaxOffsetCandidates {
			limited = true
			break
		}
		// Use the strongest bin's actual timestamp offsets. Neighboring bins
		// are suppressed but not averaged into an unrelated temporal shift.
		offsets = append(offsets, offset)
	}
	return offsets, limited, nil
}

func nearbyBytes(value byte, radius int) []byte {
	keys := make([]byte, 0, 37)
	keys = append(keys, value)
	if radius >= 1 {
		for bit := 0; bit < 8; bit++ {
			keys = append(keys, value^(1<<bit))
		}
	}
	if radius >= 2 {
		for first := 0; first < 8; first++ {
			for second := first + 1; second < 8; second++ {
				keys = append(keys, value^(1<<first)^(1<<second))
			}
		}
	}
	return keys
}

type audioMatch struct {
	a, b    Interval
	metrics Metrics
	reasons []Reason
}

func alignedAudio(a, b Episode, offset int64, o Options, budget *workBudget) ([]audioMatch, []Reason, error) {
	matched := make([]int, len(a.Audio))
	for i := range matched {
		matched[i] = -1
	}
	j, lastTarget := 0, -1
	for i, sample := range a.Audio {
		if err := budget.spend(); err != nil {
			return nil, nil, err
		}
		target := sample.StartTicks + offset
		for j+1 < len(b.Audio) && absolute(b.Audio[j+1].StartTicks-target) <= absolute(b.Audio[j].StartTicks-target) {
			if err := budget.spend(); err != nil {
				return nil, nil, err
			}
			j++
		}
		if j < len(b.Audio) && j > lastTarget && absolute(b.Audio[j].StartTicks-target) <= o.AudioAlignmentTicks &&
			bits.OnesCount32(sample.Fingerprint^b.Audio[j].Fingerprint) <= o.MaxAudioHamming {
			matched[i], lastTarget = j, j
		}
	}
	var results []audioMatch
	var reasons []Reason
	first, last := -1, -1
	flush := func() error {
		if first < 0 {
			return nil
		}
		match, reason, err := measureAudioRun(a, b, matched, first, last, o, budget)
		if err != nil {
			return err
		}
		if reason != "" {
			reasons = addReason(reasons, reason)
		} else if match != nil {
			results = append(results, *match)
		}
		first, last = -1, -1
		return nil
	}
	for i, target := range matched {
		if first >= 0 && (a.Audio[i].StartTicks-a.Audio[last].EndTicks > o.MaxAudioGapTicks ||
			target >= 0 && b.Audio[target].StartTicks-b.Audio[matched[last]].EndTicks > o.MaxAudioGapTicks) {
			if err := flush(); err != nil {
				return nil, nil, err
			}
		}
		if target >= 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if err := flush(); err != nil {
		return nil, nil, err
	}
	return results, reasons, nil
}

func measureAudioRun(a, b Episode, matched []int, first, last int, o Options, budget *workBudget) (*audioMatch, Reason, error) {
	arange := Interval{a.Audio[first].StartTicks, a.Audio[last].EndTicks}
	brange := Interval{b.Audio[matched[first]].StartTicks, b.Audio[matched[last]].EndTicks}
	duration := arange.EndTicks - arange.StartTicks
	if duration < o.MinDurationTicks {
		return nil, "", nil
	}
	if duration > o.MaxDurationTicks || brange.EndTicks-brange.StartTicks > o.MaxDurationTicks {
		return nil, OverlongRepeat, nil
	}
	// A safe interior cannot erase failed discovery gates on the full run.
	if _, reason, err := measureMatchedAudioEvidence(a, b, matched, first, last, arange, brange, o, budget); err != nil || reason != "" {
		return nil, reason, err
	}
	guard := max(a.AudioBoundaryUncertaintyTicks, b.AudioBoundaryUncertaintyTicks)
	arange.StartTicks += guard
	arange.EndTicks -= guard
	brange.StartTicks += guard
	brange.EndTicks -= guard
	if arange.EndTicks-arange.StartTicks < o.MinDurationTicks || brange.EndTicks-brange.StartTicks < o.MinDurationTicks {
		return nil, "", nil
	}
	metrics, reason, err := measureMatchedAudioEvidence(a, b, matched, first, last, arange, brange, o, budget)
	if err != nil || reason != "" {
		return nil, reason, err
	}
	metrics.BoundaryUncertaintyTicks = guard
	reasons := audioReasonsForInterval(nil, metrics, o)
	// A repeat reaching either extracted suffix has no demonstrated ending.
	if last == len(a.Audio)-1 || matched[last] == len(b.Audio)-1 ||
		a.Audio[last].EndTicks >= min(a.DurationTicks, o.WindowTicks)-o.AudioAlignmentTicks ||
		b.Audio[matched[last]].EndTicks >= min(b.DurationTicks, o.WindowTicks)-o.AudioAlignmentTicks {
		reasons = addReason(reasons, AnalysisBoundary)
	}
	return &audioMatch{arange, brange, metrics, reasons}, "", nil
}
