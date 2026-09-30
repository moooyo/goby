package main

import (
	"context"
	"errors"
	"math/bits"
	"testing"
	"unsafe"
)

func oracleViews(count int) [3]view {
	var result [3]view
	seed := uint64(0x192c7691a731b881)
	next := func() uint64 { seed ^= seed << 13; seed ^= seed >> 7; seed ^= seed << 17; return seed }
	for source := range result {
		result[source] = view{ID: string(rune('A' + source)), Geometry: neutral(), Frames: make([]frame, count)}
		for index := 0; index < count; index++ {
			f := frame{PTS: float64(index) / 10, Previous: index - 5}
			for patch := range f.Patches {
				d := descriptor{Bits: [2]uint64{next(), next()}, Reliable: [2]uint64{^uint64(0), ^uint64(0)}, Usable: true}
				if (index+patch+source)%7 == 0 {
					d.Usable = false
				}
				if (index*3+patch)%11 == 0 {
					d.Reliable[1] = 0
				}
				if (index+patch)%13 == 0 {
					d.Reliable[0] = 0x1ffff
					d.Reliable[1] = ^uint64(0)
				}
				f.Patches[patch] = d
			}
			result[source].Frames[index] = f
		}
	}
	return result
}

func requireSameEvidence(t *testing.T, views [3]view, cache *posteriorEvidenceCache, source, left, right int, support uint32, oracleWork, cachedWork *budget) {
	t.Helper()
	a, b, c, err := selfEvidence(views[source].Frames[left], views[source].Frames[right], support, oracleWork)
	x, y, z, cachedErr := posteriorSelfEvidence(cache, views, source, left, right, support, cachedWork)
	if err != nil || cachedErr != nil || [3]bool{a, b, c} != [3]bool{x, y, z} {
		t.Fatalf("source=%d pair=%d/%d support=%x oracle=%v/%v cached=%v/%v", source, left, right, support, [3]bool{a, b, c}, err, [3]bool{x, y, z}, cachedErr)
	}
}

func TestPosteriorCacheMatchesUncachedAcrossMasksAndPairOrder(t *testing.T) {
	views := oracleViews(16)
	cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{}, nil, nil)
	oracleWork, cachedWork := &budget{}, &budget{}
	masks := []uint32{0, 1, (1 << 25) - 1, 0xff, 0x555555, 0x1249249, (1 << 24) | 3, 1 << 31, ((1 << 25) - 1) | (1 << 31)}
	for source := range views {
		for left := 0; left < 16; left++ {
			for right := 0; right < 16; right++ {
				for _, support := range masks {
					requireSameEvidence(t, views, cache, source, left, right, support, oracleWork, cachedWork)
					requireSameEvidence(t, views, cache, source, right, left, support, oracleWork, cachedWork)
				}
			}
		}
	}
	if cachedWork.PatchComparisons >= oracleWork.PatchComparisons || cache.stats.FullHits == 0 || cache.stats.PartialHits == 0 {
		t.Fatal("oracle did not exercise reusable and partial evidence")
	}
	if cachedWork.ClockLookups != cache.stats.Queries {
		t.Fatal("cache query work is not charged")
	}
}

func TestPosteriorCacheComputesOnlyRequestedPatchBits(t *testing.T) {
	views := oracleViews(4)
	cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{}, nil, nil)
	work := &budget{}
	for _, mask := range []uint32{0x3, 0xf, 0x7, 0x3f} {
		if _, _, _, err := cache.evidence(0, 0, 1, mask, work); err != nil {
			t.Fatal(err)
		}
	}
	if work.PatchComparisons != 6 || cache.stats.ComputedPatchComparisons != 6 {
		t.Fatal("cache eagerly computed unrequested patches")
	}
	computed := uint32(0)
	for _, entry := range cache.entries {
		computed |= entry.Computed
	}
	if computed != 0x3f {
		t.Fatal("unexpected published computed mask")
	}
}

func TestPosteriorCacheCapacityFallbackMatchesUncached(t *testing.T) {
	views := oracleViews(4)
	for _, capacity := range []int{0, 1} {
		cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{}, nil, nil)
		cache.entries = make([]posteriorCacheEntry, capacity)
		for _, pair := range [][2]int{{0, 1}, {2, 3}, {3, 2}, {0, 1}} {
			requireSameEvidence(t, views, cache, 0, pair[0], pair[1], (1<<25)-1, &budget{}, &budget{})
		}
		if cache.stats.FallbackQueries == 0 {
			t.Fatal("capacity fallback was not exercised")
		}
	}
}

type countedCancellation struct {
	context.Context
	cancel        context.CancelFunc
	checks, after int
}

func (ctx *countedCancellation) Err() error {
	ctx.checks++
	if ctx.checks == ctx.after {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func nonemptyCacheEntries(cache *posteriorEvidenceCache) map[uint32]posteriorCacheEntry {
	result := map[uint32]posteriorCacheEntry{}
	for _, entry := range cache.entries {
		if entry.Key != 0 {
			result[entry.Key] = entry
		}
	}
	return result
}

func TestPosteriorCacheBudgetAndCancellationDoNotCommitPartial(t *testing.T) {
	views := oracleViews(4)
	for _, existing := range []bool{false, true} {
		cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{}, nil, nil)
		if existing {
			if _, _, _, err := cache.evidence(0, 0, 1, 1, &budget{}); err != nil {
				t.Fatal(err)
			}
		}
		before := nonemptyCacheEntries(cache)
		a, b, c, err := cache.evidence(0, 0, 1, 0xff, &budget{PatchComparisons: maxPatchComparisons - 2})
		if err == nil || a || b || c {
			t.Fatal("budget failure returned evidence")
		}
		after := nonemptyCacheEntries(cache)
		if len(before) != len(after) {
			t.Fatal("budget failure published a partial entry")
		}
		for key, entry := range before {
			if after[key] != entry {
				t.Fatal("budget failure published partial bits")
			}
		}
		base, cancel := context.WithCancel(context.Background())
		cache.ctx = &countedCancellation{Context: base, cancel: cancel, after: 4}
		work := &budget{}
		a, b, c, err = cache.evidence(0, 0, 1, 0xff, work)
		if !errors.Is(err, context.Canceled) || a || b || c || work.PatchComparisons != 2 {
			t.Fatal("mid-computation cancellation did not fail closed")
		}
		after = nonemptyCacheEntries(cache)
		if len(before) != len(after) {
			t.Fatal("cancellation published a partial entry")
		}
		for key, entry := range before {
			if after[key] != entry {
				t.Fatal("cancellation published partial bits")
			}
		}
		cache.ctx = context.Background()
		if _, _, _, err = cache.evidence(0, 0, 1, 1, &budget{ClockLookups: maxClockLookups}); err == nil {
			t.Fatal("cached lookup bypassed its budget")
		}
		if _, _, _, err = cache.evidence(0, 0, 1, 1, &budget{PatchComparisons: maxPatchComparisons + 1}); err == nil {
			t.Fatal("cache accepted an already failed work budget")
		}
	}
}

func TestPosteriorCacheBoundAndViewClockIsolation(t *testing.T) {
	views := oracleViews(4)
	cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{{Scale: 1}, {Scale: 1}}, nil, nil)
	if unsafe.Sizeof(posteriorCacheEntry{}) != 16 || int(unsafe.Sizeof(*cache))+len(cache.entries)*int(unsafe.Sizeof(posteriorCacheEntry{})) > posteriorCacheAllocationBound {
		t.Fatal("cache exceeded its fixed allocation bound")
	}
	requireSameEvidence(t, views, cache, 0, 0, 1, (1<<25)-1, &budget{}, &budget{})
	other := views
	other[0].Frames = append([]frame(nil), views[0].Frames...)
	other[0].Geometry.ScaleY = 1.1
	other[0].Frames[1].Patches = other[0].Frames[0].Patches
	if _, _, _, err := posteriorSelfEvidence(cache, other, 0, 0, 1, (1<<25)-1, &budget{}); err == nil {
		t.Fatal("different fixed geometry borrowed cached evidence")
	}
	otherCache := newPosteriorEvidenceCache(other, [2]clockHypothesis{{Scale: 1}, {Scale: 1}}, nil, nil)
	requireSameEvidence(t, other, otherCache, 0, 0, 1, (1<<25)-1, &budget{}, &budget{})
	otherClock := newPosteriorEvidenceCache(views, [2]clockHypothesis{{Scale: 1.02, Offset: 2}, {Scale: .98, Offset: 4}}, nil, nil)
	requireSameEvidence(t, views, otherClock, 0, 0, 1, (1<<25)-1, &budget{}, &budget{})
	if otherClock.stats.AvoidedPatchComparisons != 0 || otherClock.stats.ComputedPatchComparisons != 25 {
		t.Fatal("new clock context borrowed prior evidence")
	}
}

func TestPosteriorStateAndPeriodAuditMatchesUncached(t *testing.T) {
	for _, mode := range []string{"varied", "static", "unknown"} {
		views := oracleViews(64)
		for source := range views {
			for index := range views[source].Frames {
				if mode == "static" {
					views[source].Frames[index].Patches = views[source].Frames[0].Patches
				}
				if mode == "unknown" && index%3 == 0 {
					views[source].Frames[index].Patches = [patchCount]descriptor{}
				}
			}
		}
		cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{}, nil, nil)
		var mappings [3][]int
		for source := range mappings {
			mappings[source] = make([]int, 64)
			for i := range mappings[source] {
				mappings[source][i] = i
			}
			mappings[source][2] = -1
		}
		for _, support := range []uint32{(1 << 25) - 1, 0xff, 0x1249249, 0x555555} {
			for _, window := range [][2]int{{0, 45}, {0, 64}, {3, 54}, {10, 64}} {
				start, end := window[0], window[1]
				bounds := [3][2]float64{{float64(start) / 10, float64(end) / 10}, {float64(start) / 10, float64(end) / 10}, {float64(start) / 10, float64(end) / 10}}
				uncached, err := stateAndPeriodAudit(views, mappings, start, end, support, bounds, nil, &budget{})
				cached, cachedErr := stateAndPeriodAudit(views, mappings, start, end, support, bounds, cache, &budget{})
				if err != nil || cachedErr != nil || uncached != cached {
					t.Fatalf("mode=%s mask=%x window=%v uncached=%q/%v cached=%q/%v", mode, support, window, uncached, err, cached, cachedErr)
				}
			}
		}
		if cache.stats.Queries == 0 {
			t.Fatal("posterior oracle did not execute")
		}
	}
	if bits.OnesCount32((1<<25)-1) != 25 {
		t.Fatal("invalid fixed patch domain")
	}
}
