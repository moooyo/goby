package introdetect

import (
	"context"
	"errors"
	"math/bits"
	"reflect"
	"testing"
)

// This small-clock oracle exhausts every integer phase and scans every target
// independently. It shares neither the event sweep nor its bucket bookkeeping.
func bruteVisualAlignment(a, b Episode, audio audioMatch, offset int64, o Options) visualAlignment {
	start := 0
	for start < len(a.Visual) && a.Visual[start].Ticks < audio.a.StartTicks {
		start++
	}
	end := start
	for end < len(a.Visual) && a.Visual[end].Ticks <= audio.a.EndTicks {
		end++
	}
	bestScore := -1
	var best visualAlignment
	for phase := -o.VisualAlignmentTicks; phase <= o.VisualAlignmentTicks; phase++ {
		value := visualAlignment{start: start, targets: make([]int, end-start), phase: phase}
		score, lastTarget := 0, -1
		for i := start; i < end; i++ {
			value.targets[i-start] = -1
			target := a.Visual[i].Ticks + offset + phase
			nearest, distance := -1, int64(1<<62)
			for j, sample := range b.Visual {
				if current := absolute(sample.Ticks - target); current <= distance {
					nearest, distance = j, current
				}
			}
			if nearest < 0 || nearest <= lastTarget || b.Visual[nearest].Ticks < audio.b.StartTicks || b.Visual[nearest].Ticks > audio.b.EndTicks ||
				absolute(b.Visual[nearest].Ticks-a.Visual[i].Ticks-offset) > o.VisualAlignmentTicks {
				continue
			}
			lastTarget, value.targets[i-start] = nearest, nearest
			value.maximumResidual = max(value.maximumResidual, absolute(b.Visual[nearest].Ticks-a.Visual[i].Ticks-offset))
			if a.Visual[i].Contrast >= o.MinVisualContrast && b.Visual[nearest].Contrast >= o.MinVisualContrast {
				score += 64 - bits.OnesCount64(a.Visual[i].Hash^b.Visual[nearest].Hash)
			}
		}
		if score > bestScore || score == bestScore && (absolute(phase) < absolute(best.phase) || absolute(phase) == absolute(best.phase) && phase < best.phase) {
			bestScore, best = score, value
		}
	}
	return best
}

func TestVisualPhaseSweepMatchesExhaustiveIntegerClockOracle(t *testing.T) {
	random := testRandom(289137)
	for fixture := 0; fixture < 1000; fixture++ {
		var episodes [2]Episode
		for side := range episodes {
			tick := int64(random.next() % 4)
			count := 1 + int(random.next()%9)
			for i := 0; i < count; i++ {
				tick += 1 + int64(random.next()%4)
				hash, contrast := random.next(), uint16(200)
				if random.next()%4 == 0 {
					contrast = 0
				}
				if side == 1 && random.next()%2 == 0 {
					hash = episodes[0].Visual[int(random.next()%uint64(len(episodes[0].Visual)))].Hash
				}
				episodes[side].Visual = append(episodes[side].Visual, VisualSample{tick, hash, contrast})
			}
		}
		var windows [2]Interval
		for side, episode := range episodes {
			first, last := int(random.next()%uint64(len(episode.Visual))), int(random.next()%uint64(len(episode.Visual)))
			if first > last {
				first, last = last, first
			}
			windows[side] = Interval{episode.Visual[first].Ticks, episode.Visual[last].Ticks}
		}
		audio := audioMatch{a: windows[0], b: windows[1]}
		o := DefaultOptions()
		o.VisualAlignmentTicks = 1 + int64(random.next()%9)
		offset := int64(random.next()%9) - 4
		got, err := alignVisual(episodes[0], episodes[1], audio, offset, o, v2TestBudget())
		want := bruteVisualAlignment(episodes[0], episodes[1], audio, offset, o)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("fixture %d: got %#v, want %#v, error %v; A=%#v B=%#v audio=%#v offset=%d tolerance=%d", fixture, got, want, err,
				episodes[0].Visual, episodes[1].Visual, audio, offset, o.VisualAlignmentTicks)
		}
	}
}

func TestVisualPhaseSweepRetainsExactClockEdgesAndEmptyInputs(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		a, b      []VisualSample
		audio     audioMatch
		tolerance int64
	}{
		{"simultaneous_events", []VisualSample{{0, 1, 200}, {4, 2, 200}, {8, 3, 200}}, []VisualSample{{0, 2, 200}, {4, 3, 200}, {8, 1, 200}}, audioMatch{a: Interval{0, 8}, b: Interval{0, 8}}, 5},
		{"odd_midpoints", []VisualSample{{0, 1, 200}, {3, 2, 200}, {7, 3, 200}}, []VisualSample{{1, 2, 200}, {4, 3, 200}, {8, 1, 200}}, audioMatch{a: Interval{0, 7}, b: Interval{1, 8}}, 5},
		{"outside_window_nearest", []VisualSample{{3, 1, 200}, {7, 2, 200}}, []VisualSample{{1, 1, 200}, {5, 2, 200}, {9, 1, 200}}, audioMatch{a: Interval{3, 7}, b: Interval{5, 5}}, 4},
		{"empty_source", nil, []VisualSample{{0, 1, 200}}, audioMatch{a: Interval{0, 5}, b: Interval{0, 5}}, 5},
		{"empty_target", []VisualSample{{0, 1, 200}, {3, 2, 200}}, nil, audioMatch{a: Interval{0, 5}, b: Interval{0, 5}}, 5},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			a, b := Episode{Visual: fixture.a}, Episode{Visual: fixture.b}
			o := DefaultOptions()
			o.VisualAlignmentTicks = fixture.tolerance
			got, err := alignVisual(a, b, fixture.audio, 0, o, v2TestBudget())
			want := bruteVisualAlignment(a, b, fixture.audio, 0, o)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v, error %v", got, want, err)
			}
		})
	}
}

func TestVisualPhaseCannotExpandTheOriginalAudioCorridor(t *testing.T) {
	h0, h1 := stateAnchorTestHash(1), stateAnchorTestHash(2)
	a := Episode{Visual: []VisualSample{{0, h0, 200}, {30, h1, 200}}}
	b := Episode{Visual: []VisualSample{{3, ^h0, 200}, {14, h0, 200}, {38, ^h1, 200}, {40, h1, 200}}}
	o := DefaultOptions()
	o.VisualAlignmentTicks = 10
	value, err := alignVisual(a, b, audioMatch{a: Interval{0, 30}, b: Interval{0, 40}}, 0, o, v2TestBudget())
	if err != nil || value.phase != 9 || !reflect.DeepEqual(value.targets, []int{-1, 3}) || value.maximumResidual != 10 {
		t.Fatalf("a refined phase admitted a target outside the original corridor: %#v, %v", value, err)
	}
}

func TestVisualPhaseTiesUseTheClosestStablePhaseThenSignedOrder(t *testing.T) {
	hash := stateAnchorTestHash(1)
	a := Episode{Visual: []VisualSample{{10, hash, 200}}}
	for _, fixture := range []struct {
		name            string
		b               []VisualSample
		tolerance, want int64
	}{
		{"negative_interval_endpoint", []VisualSample{{0, hash, 200}, {8, ^hash, 200}}, 10, -7},
		{"equal_absolute_phases", []VisualSample{{1, hash, 200}, {10, ^hash, 200}, {19, hash, 200}}, 9, -5},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			o := DefaultOptions()
			o.VisualAlignmentTicks = fixture.tolerance
			value, err := alignVisual(a, Episode{Visual: fixture.b}, audioMatch{a: Interval{10, 10}, b: Interval{0, 20}}, 0, o, v2TestBudget())
			if err != nil || value.phase != fixture.want {
				t.Fatalf("phase tie chose %#v, want %d, error %v", value, fixture.want, err)
			}
		})
	}
}

func TestVisualPhaseCannotStitchDifferentLocalOffsets(t *testing.T) {
	var a, b Episode
	for i := 0; i < 10; i++ {
		a.Visual = append(a.Visual, VisualSample{int64(i * 4), stateAnchorTestHash(i), 200})
		b.Visual = append(b.Visual, VisualSample{int64(i * 4), stateAnchorTestHash(i + 32), 200})
	}
	for i := 1; i <= 3; i++ {
		b.Visual[i+1].Hash = a.Visual[i].Hash
		b.Visual[i+4].Hash = a.Visual[i+5].Hash
	}
	o := DefaultOptions()
	o.VisualAlignmentTicks = 5
	value, err := alignVisual(a, b, audioMatch{a: Interval{0, 36}, b: Interval{0, 36}}, 0, o, v2TestBudget())
	if err != nil {
		t.Fatal(err)
	}
	matched := 0
	for i, j := range value.targets {
		if j >= 0 && bits.OnesCount64(a.Visual[i].Hash^b.Visual[j].Hash) <= o.MaxVisualHamming {
			matched++
		}
	}
	if matched != 3 {
		t.Fatalf("independently favorable offsets supplied more than one global phase: %#v, matched=%d", value, matched)
	}
}

func TestVisualPhaseScoreDoesNotImproveByDroppingWeakObservations(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := Episode{Visual: []VisualSample{{0, stateAnchorTestHash(0), 200}, {10, stateAnchorTestHash(1), 200}, {20, stateAnchorTestHash(2), 200}}}
		b := Episode{Visual: []VisualSample{{0, a.Visual[0].Hash ^ 255, 200}, {10, a.Visual[1].Hash ^ 255, 200}, {20, a.Visual[0].Hash, 200}}}
		if dark {
			b.Visual[1].Contrast = 0
		}
		o := DefaultOptions()
		o.VisualAlignmentTicks = 20
		value, err := alignVisual(a, b, audioMatch{a: Interval{0, 20}, b: Interval{0, 20}}, 0, o, v2TestBudget())
		if err != nil || value.phase != 0 || !reflect.DeepEqual(value.targets, []int{0, 1, 2}) {
			t.Fatalf("one perfect selected pair displaced stronger full-window evidence: dark=%v alignment=%#v error=%v", dark, value, err)
		}
	}
	a := Episode{Visual: []VisualSample{{0, 1, 0}, {1, 1, 200}}}
	b := Episode{Visual: []VisualSample{{0, 1, 200}}}
	o := DefaultOptions()
	o.VisualAlignmentTicks = 1
	value, err := alignVisual(a, b, audioMatch{a: Interval{0, 1}, b: Interval{0, 1}}, 0, o, v2TestBudget())
	if err != nil || value.phase != 0 || !reflect.DeepEqual(value.targets, []int{0, -1}) {
		t.Fatalf("a dark raw correspondence freed its target for a later source: %#v, %v", value, err)
	}
}

func TestVisualPhaseSharedMapPreservesEndpointEvidenceAndResidualUncertainty(t *testing.T) {
	var a, b Episode
	for i := 0; i <= 60; i++ {
		tick, hash := int64(i)*TicksPerSecond/2, stateAnchorTestHash(i/8)
		a.Visual = append(a.Visual, VisualSample{tick, hash, 200})
		b.Visual = append(b.Visual, VisualSample{tick + 8*TicksPerSecond/10, hash, 200})
	}
	audio := audioMatch{a: Interval{0, 30 * TicksPerSecond}, b: Interval{0, 31 * TicksPerSecond}}
	o := DefaultOptions()
	alignment, err := alignVisual(a, b, audio, 0, o, v2TestBudget())
	if err != nil || alignment.phase != 55*TicksPerSecond/100 || alignment.maximumResidual != 8*TicksPerSecond/10 {
		t.Fatalf("the complete repeated sequence lost its constant sampling phase: %#v, %v", alignment, err)
	}
	match, reason, err := visualConfirm(a, b, audio, 0, o, v2TestBudget())
	if err != nil || match == nil || reason != "" {
		t.Fatalf("confirm shared visual map: %#v, %s, %v", match, reason, err)
	}
	if match.metrics.VisualSamples != 60 || match.metrics.VisualMatchedTimePermille != 967 || match.metrics.BoundaryUncertaintyTicks != 8*TicksPerSecond/10 ||
		match.a != (Interval{0, 30 * TicksPerSecond}) || match.b != (Interval{8 * TicksPerSecond / 10, 308 * TicksPerSecond / 10}) {
		t.Fatalf("diagnostic half-open samples, closed timeline endpoints or actual residual uncertainty changed: %#v", match)
	}
}

func TestVisualPhaseBudgetAndCancellationNeverReturnPartialWinners(t *testing.T) {
	var a, b Episode
	for i := 0; i < 20; i++ {
		a.Visual = append(a.Visual, VisualSample{int64(i * 3), stateAnchorTestHash(i), 200})
		b.Visual = append(b.Visual, VisualSample{int64(i*3 + 2), stateAnchorTestHash(i), 200})
	}
	o := DefaultOptions()
	o.VisualAlignmentTicks = 8
	audio := audioMatch{a: Interval{0, 60}, b: Interval{0, 60}}
	complete := v2TestBudget()
	if _, err := alignVisual(a, b, audio, 0, o, complete); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{1, 32, complete.used - 1} {
		budget := &workBudget{ctx: context.Background(), limit: limit}
		value, err := alignVisual(a, b, audio, 0, o, budget)
		if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(value, visualAlignment{}) {
			t.Fatalf("budget %d returned a partial winning phase: %#v, %v", limit, value, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := alignVisual(a, b, audio, 0, o, &workBudget{ctx: ctx, limit: o.MaxComparisons})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(value, visualAlignment{}) {
		t.Fatalf("cancellation returned visual evidence: %#v, %v", value, err)
	}
	base, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	late := &cancelDuringContext{Context: base, cancel: stop, after: int(complete.used/256) + 2}
	value, err = alignVisual(a, b, audio, 0, o, &workBudget{ctx: late, limit: o.MaxComparisons})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(value, visualAlignment{}) || late.after != 0 {
		t.Fatalf("cancellation before delivery returned a completed map: %#v, %v", value, err)
	}
}
