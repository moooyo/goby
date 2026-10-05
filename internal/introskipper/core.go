// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only
//
// Mechanically translated from IntroSkipper 12.0.4.0, commit
// 6e0cb179007ac4c16cd9f358e9a617e791e9bf06. This package supports
// Introduction and Credits raw candidates from admitted fingerprint queues.

package introskipper

import (
	"context"
	"math/bits"
)

// Round each division to binary64, as in the upstream C# constant expression.
var sampleDuration = func() float64 {
	value := float64(4096)
	value /= 11025
	return value / 3
}()

type configuration = Options

func defaultConfiguration() configuration { return DefaultOptions() }

type episode struct {
	ID                      string   `json:"id"`
	Fingerprint             []uint32 `json:"fingerprint"`
	DurationSeconds         *float64 `json:"durationSeconds,omitempty"`
	FingerprintEnd          *float64 `json:"fingerprintEnd,omitempty"`
	CreditsFingerprintStart float64  `json:"creditsFingerprintStart,omitempty"`
}

type timeRange struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (r timeRange) duration() float64 { return r.End - r.Start }

type segment struct {
	ID    string  `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (s segment) valid() bool       { return s.End > 0 && s.End > s.Start }
func (s segment) duration() float64 { return s.End - s.Start }

type pairTrace struct {
	LHS      segment `json:"lhs"`
	RHS      segment `json:"rhs"`
	Accepted bool    `json:"accepted"`
	Reason   string  `json:"reason"`
}

type orderedIndex struct {
	keys      []uint32
	positions map[uint32]int
}

type analyzer struct {
	config  configuration
	credits bool
	cache   map[string]orderedIndex
	pairs   map[string]selectedPair
	ctx     context.Context
	work    int64
	limit   int64
	err     error
}

type selectedPair struct{ lhs, rhs segment }

func newAnalyzer(cfg configuration) *analyzer {
	return &analyzer{config: cfg, cache: make(map[string]orderedIndex), pairs: make(map[string]selectedPair), ctx: context.Background(), limit: MaxComparisons}
}

// charge bounds both index probes and Hamming comparisons. Exhaustion and
// cancellation are execution failures; neither produces a partial result.
func (a *analyzer) charge() bool {
	if a.err != nil {
		return false
	}
	if a.work&1023 == 0 {
		if err := a.ctx.Err(); err != nil {
			a.err = err
			return false
		}
	}
	if a.work >= a.limit {
		a.err = ErrLimit
		return false
	}
	a.work++
	return true
}

// findCandidates preserves the supplied queue order. Upstream FindCandidatesAsync
// does not sort its queue. Every supplied episode is treated as needing analysis,
// with fingerprint extraction already completed by the native plugin.
func (a *analyzer) findCandidates(ctx context.Context, episodes []episode) ([]segment, []pairTrace, error) {
	a.ctx = ctx
	candidates := make([]segment, 0)
	positions := make(map[string]int)
	trace := make([]pairTrace, 0)
	if len(episodes) <= 1 {
		return candidates, trace, nil
	}

	save := func(candidate segment, pair selectedPair) {
		position, found := positions[candidate.ID]
		if !found {
			positions[candidate.ID] = len(candidates)
			candidates = append(candidates, candidate)
			a.pairs[candidate.ID] = pair
		} else if isBetterCandidate(candidate, candidates[position]) {
			candidates[position] = candidate
			a.pairs[candidate.ID] = pair
		}
	}
	for current := 0; current < len(episodes); current++ {
		for remaining := current + 1; remaining < len(episodes); remaining++ {
			if err := ctx.Err(); err != nil {
				return nil, trace, err
			}
			lhs, rhs, err := a.compareEpisodes(ctx, episodes[current], episodes[remaining])
			if err != nil {
				return nil, trace, err
			}
			if !rhs.valid() {
				trace = append(trace, pairTrace{lhs, rhs, false, "invalid-rhs"})
				continue
			}
			if rhs.duration() > float64(a.getMaximumSegmentDuration(episodes[remaining])) {
				trace = append(trace, pairTrace{lhs, rhs, false, "rhs-exceeds-maximum-duration"})
				continue
			}
			// Credits fingerprints use a tail-relative clock. The upstream adds
			// each source's own start only after the relative RHS duration check.
			if a.credits {
				lhs.Start += episodes[current].CreditsFingerprintStart
				lhs.End += episodes[current].CreditsFingerprintStart
				rhs.Start += episodes[remaining].CreditsFingerprintStart
				rhs.End += episodes[remaining].CreditsFingerprintStart
			}
			trace = append(trace, pairTrace{lhs, rhs, true, "first-valid-pair"})
			pair := selectedPair{lhs, rhs}
			save(lhs, pair)
			save(rhs, pair)
			// Introduction and Credits stop after their first valid remaining episode.
			break
		}
	}
	return candidates, trace, nil
}

// Both modes' replacement rule uses post-snap duration, with strict greater
// comparison so an equal-duration later pair cannot replace the saved segment.
func isBetterCandidate(candidate, saved segment) bool {
	return candidate.duration() > saved.duration()
}

func (a *analyzer) getMaximumSegmentDuration(e episode) int {
	if a.credits {
		// Preserve the native floating-point subtraction and truncation order.
		// This is the native guard against a full-window shared match.
		return int(*e.DurationSeconds - e.CreditsFingerprintStart - 1)
	}
	return a.config.MaximumIntroDuration
}

func (a *analyzer) compareEpisodes(ctx context.Context, lhs, rhs episode) (segment, segment, error) {
	lhsRanges, rhsRanges, err := a.searchInvertedIndex(ctx, lhs, rhs)
	if err != nil {
		return segment{}, segment{}, err
	}
	if len(lhsRanges) > 0 {
		left, right := selectSharedRegion(lhs.ID, lhsRanges, rhs.ID, rhsRanges)
		return left, right, nil
	}
	return segment{ID: lhs.ID}, segment{ID: rhs.ID}, nil
}

func selectSharedRegion(lhsID string, lhsRanges []timeRange, rhsID string, rhsRanges []timeRange) (segment, segment) {
	pairCount := min(len(lhsRanges), len(rhsRanges))
	if pairCount == 0 {
		return segment{ID: lhsID}, segment{ID: rhsID}
	}
	selected := 0
	for i := 1; i < pairCount; i++ {
		if lhsRanges[i].duration() > lhsRanges[selected].duration() {
			selected = i
		}
	}
	lhs, rhs := lhsRanges[selected], rhsRanges[selected]
	if lhs.Start <= 5 {
		lhs.Start = 0
	}
	if rhs.Start <= 5 {
		rhs.Start = 0
	}
	return segment{lhsID, lhs.Start, lhs.End}, segment{rhsID, rhs.Start, rhs.End}
}

func (a *analyzer) searchInvertedIndex(ctx context.Context, lhs, rhs episode) ([]timeRange, []timeRange, error) {
	a.ctx = ctx
	lhsIndex, rhsIndex := a.createInvertedIndex(lhs), a.createInvertedIndex(rhs)
	if a.err != nil {
		return nil, nil, a.err
	}
	shifts := make([]int, 0)
	seen := make(map[int]struct{})
	// Explicit order vectors preserve the pinned .NET Dictionary and HashSet
	// first-insertion enumeration order. Updating a duplicate key keeps its slot.
	for _, originalPoint := range lhsIndex.keys {
		lhsPosition := lhsIndex.positions[originalPoint]
		for i := -a.config.InvertedIndexShift; i <= a.config.InvertedIndexShift; i++ {
			if !a.charge() {
				return nil, nil, a.err
			}
			// C# promotes uint + int to long, then its unchecked uint cast wraps.
			point := uint32(int64(originalPoint) + int64(i))
			if rhsPosition, found := rhsIndex.positions[point]; found {
				shift := rhsPosition - lhsPosition
				if _, found := seen[shift]; !found {
					seen[shift] = struct{}{}
					shifts = append(shifts, shift)
				}
			}
		}
	}
	lhsRanges, rhsRanges := make([]timeRange, 0), make([]timeRange, 0)
	for _, shift := range shifts {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		left, right := a.findContiguous(lhs.Fingerprint, rhs.Fingerprint, shift)
		if a.err != nil {
			return nil, nil, a.err
		}
		if left.End > 0 && right.End > 0 {
			lhsRanges = append(lhsRanges, left)
			rhsRanges = append(rhsRanges, right)
		}
	}
	return lhsRanges, rhsRanges, nil
}

func (a *analyzer) findContiguous(lhs, rhs []uint32, shiftAmount int) (timeRange, timeRange) {
	leftOffset, rightOffset := 0, 0
	if shiftAmount < 0 {
		leftOffset = -shiftAmount
	}
	if shiftAmount > 0 {
		rightOffset = shiftAmount
	}
	absShift := shiftAmount
	if absShift < 0 {
		absShift = -absShift
	}
	// Preserve the upstream expression, including its unequal-length behavior.
	upperLimit := min(len(lhs), len(rhs)) - absShift
	bestStart, bestEnd, runStart, runEnd := -1, -1, -1, -1
	for i := 0; i < upperLimit; i++ {
		if !a.charge() {
			return timeRange{}, timeRange{}
		}
		lhsPosition := i + leftOffset
		if bits.OnesCount32(lhs[lhsPosition]^rhs[i+rightOffset]) > a.config.MaximumFingerprintPointDifferences {
			continue
		}
		if runStart >= 0 && float64(lhsPosition-runEnd)*sampleDuration <= a.config.MaximumTimeSkip {
			runEnd = lhsPosition
			continue
		}
		if runStart >= 0 && (bestStart < 0 || runEnd-runStart > bestEnd-bestStart) {
			bestStart, bestEnd = runStart, runEnd
		}
		runStart, runEnd = lhsPosition, lhsPosition
	}
	if runStart >= 0 && (bestStart < 0 || runEnd-runStart > bestEnd-bestStart) {
		bestStart, bestEnd = runStart, runEnd
	}
	if bestStart < 0 || float64(bestEnd-bestStart)*sampleDuration < float64(a.config.MinimumIntroDuration) {
		return timeRange{}, timeRange{}
	}
	positionShift := rightOffset - leftOffset
	return timeRange{float64(bestStart) * sampleDuration, float64(bestEnd) * sampleDuration},
		timeRange{float64(bestStart+positionShift) * sampleDuration, float64(bestEnd+positionShift) * sampleDuration}
}

func (a *analyzer) createInvertedIndex(e episode) orderedIndex {
	if cached, found := a.cache[e.ID]; found {
		return cached
	}
	index := orderedIndex{make([]uint32, 0), make(map[uint32]int)}
	for i, point := range e.Fingerprint {
		if !a.charge() {
			return orderedIndex{}
		}
		if _, found := index.positions[point]; !found {
			index.keys = append(index.keys, point)
		}
		index.positions[point] = i
	}
	a.cache[e.ID] = index
	return index
}
