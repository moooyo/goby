package main

import (
	"context"
	"errors"
	"math/bits"
)

const posteriorCacheSlots = 1 << 16
const posteriorCacheProbes = 8
const posteriorCacheAllocationBound = posteriorCacheSlots*16 + 1024

// All evidence belongs to one immutable source/view and one clock-combination
// context. The table never stores state choices, window outcomes, or motion.
type posteriorCacheEntry struct {
	Key            uint32
	Computed       uint32
	KnownClose     uint32
	KnownDifferent uint32
}

type posteriorCacheStats struct {
	Contexts                 int64 `json:"contexts"`
	Queries                  int64 `json:"queries"`
	FullHits                 int64 `json:"fullHits"`
	PartialHits              int64 `json:"partialHits"`
	FallbackQueries          int64 `json:"fallbackQueries"`
	ComputedPatchComparisons int64 `json:"computedPatchComparisons"`
	AvoidedPatchComparisons  int64 `json:"avoidedPatchComparisons"`
	AllocationBoundBytes     int   `json:"allocationBoundBytes"`
}

type posteriorEvidenceCache struct {
	views   [3]view
	clocks  [2]clockHypothesis
	ctx     context.Context
	entries []posteriorCacheEntry
	stats   *posteriorCacheStats
}

func newPosteriorEvidenceCache(views [3]view, clocks [2]clockHypothesis, ctx context.Context, stats *posteriorCacheStats) *posteriorEvidenceCache {
	if ctx == nil {
		ctx = context.Background()
	}
	if stats == nil {
		stats = &posteriorCacheStats{}
	}
	stats.Contexts++
	stats.AllocationBoundBytes = posteriorCacheAllocationBound
	return &posteriorEvidenceCache{views: views, clocks: clocks, ctx: ctx, entries: make([]posteriorCacheEntry, posteriorCacheSlots), stats: stats}
}

func posteriorHash(key uint32) uint32 {
	key ^= key >> 16
	key *= 0x7feb352d
	key ^= key >> 15
	key *= 0x846ca68b
	key ^= key >> 16
	return key
}

func reducePosteriorEvidence(closeMask, differentMask, support uint32) (bool, bool, bool) {
	matched := bits.OnesCount32(closeMask & support)
	comparable := bits.OnesCount32((closeMask | differentMask) & support)
	total := bits.OnesCount32(support)
	return comparable*1000 >= total*850, matched*1000 >= total*850, (matched+total-comparable)*1000 < total*850
}

func (cache *posteriorEvidenceCache) evidence(source, left, right int, support uint32, work *budget) (bool, bool, bool, error) {
	if err := cache.ctx.Err(); err != nil {
		return false, false, false, err
	}
	cache.stats.Queries++
	// Every query, including a hit or fallback, remains bounded by the same
	// lookup budget as the surrounding finite search.
	if err := work.clock(); err != nil {
		return false, false, false, err
	}
	if work.PatchComparisons > maxPatchComparisons {
		return false, false, false, errors.New("patch comparison budget exceeded")
	}
	if source < 0 || source >= len(cache.views) || left < 0 || right < 0 || left >= len(cache.views[source].Frames) || right >= len(cache.views[source].Frames) {
		return false, false, false, errors.New("posterior cache frame outside fixed view")
	}
	if left > right {
		left, right = right, left
	}
	fallback := func() (bool, bool, bool, error) {
		cache.stats.FallbackQueries++
		before := work.PatchComparisons
		a, b, c, err := selfEvidence(cache.views[source].Frames[left], cache.views[source].Frames[right], support, work)
		computed := work.PatchComparisons - before
		if err != nil && work.PatchComparisons > maxPatchComparisons {
			computed--
		}
		cache.stats.ComputedPatchComparisons += max(0, computed)
		if err != nil {
			return false, false, false, err
		}
		if err := cache.ctx.Err(); err != nil {
			return false, false, false, err
		}
		return a, b, c, nil
	}
	if len(cache.entries) == 0 || left >= 4096 || right >= 4096 {
		return fallback()
	}
	key := uint32(source)<<24 | uint32(left)<<12 | uint32(right)
	key++
	start := int(posteriorHash(key)) & (len(cache.entries) - 1)
	slot := -1
	for probe := 0; probe < min(posteriorCacheProbes, len(cache.entries)); probe++ {
		index := (start + probe) & (len(cache.entries) - 1)
		if cache.entries[index].Key == 0 || cache.entries[index].Key == key {
			slot = index
			break
		}
	}
	if slot < 0 {
		return fallback()
	}
	entry := cache.entries[slot]
	requested := support & uint32((1<<25)-1)
	known := requested & entry.Computed
	missing := requested &^ entry.Computed
	if known != 0 {
		cache.stats.AvoidedPatchComparisons += int64(bits.OnesCount32(known))
	}
	if missing == 0 {
		cache.stats.FullHits++
	} else if known != 0 {
		cache.stats.PartialHits++
	}
	for patch := 0; patch < patchCount; patch++ {
		bit := uint32(1) << uint(patch)
		if missing&bit == 0 {
			continue
		}
		if err := cache.ctx.Err(); err != nil {
			return false, false, false, err
		}
		x, y := cache.views[source].Frames[left].Patches[patch], cache.views[source].Frames[right].Patches[patch]
		close, _, err := closePatch(x, y, work)
		if err != nil {
			return false, false, false, err
		}
		cache.stats.ComputedPatchComparisons++
		active := bits.OnesCount64(x.Reliable[0]&y.Reliable[0]) + bits.OnesCount64(x.Reliable[1]&y.Reliable[1])
		entry.Computed |= bit
		if x.Usable && y.Usable && active >= 80 {
			if close {
				entry.KnownClose |= bit
			} else {
				entry.KnownDifferent |= bit
			}
		}
	}
	if err := cache.ctx.Err(); err != nil {
		return false, false, false, err
	}
	// Publish newly computed bits only after the complete requested evidence
	// succeeds. Budget/cancellation errors never commit a partial update.
	entry.Key = key
	cache.entries[slot] = entry
	a, b, c := reducePosteriorEvidence(entry.KnownClose, entry.KnownDifferent, support)
	return a, b, c, nil
}

func posteriorSelfEvidence(cache *posteriorEvidenceCache, views [3]view, source, left, right int, support uint32, work *budget) (bool, bool, bool, error) {
	if source < 0 || source >= len(views) || left < 0 || right < 0 || left >= len(views[source].Frames) || right >= len(views[source].Frames) {
		return false, false, false, errors.New("posterior cache frame outside requested view")
	}
	if cache == nil {
		return selfEvidence(views[source].Frames[left], views[source].Frames[right], support, work)
	}
	bound, requested := cache.views[source], views[source]
	if bound.ID != requested.ID || bound.Geometry != requested.Geometry || len(bound.Frames) != len(requested.Frames) || len(bound.Frames) == 0 || &bound.Frames[0] != &requested.Frames[0] {
		return false, false, false, errors.New("posterior cache view context mismatch")
	}
	return cache.evidence(source, left, right, support, work)
}
