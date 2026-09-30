package main

import (
	"math"
	"testing"
)

func observedCommonBoundary(views [3]view, mappings [3][]int, clocks [2]clockHypothesis, appearance []uint32, start, end int, support uint32, original [3][2]float64, work *budget) (boundaryAudit, error) {
	components, err := observedCommonComponents(views, mappings, clocks, appearance, start, end, support, original, work)
	if err != nil {
		return boundaryAudit{}, err
	}
	if len(components) != 1 {
		return boundaryAudit{Reason: "no-unique-complete-component"}, nil
	}
	return components[0], nil
}

func boundaryDescriptor(seed uint32) descriptor {
	d := descriptor{Reliable: [2]uint64{^uint64(0), ^uint64(0)}, Usable: true}
	for word := range d.Bits {
		for b := 0; b < 8; b++ {
			d.Bits[word] = d.Bits[word]<<8 | uint64(syntheticRandom(&seed))
		}
	}
	return d
}

func boundaryFixture(kind string, drift bool) ([3]view, [3][]int, [2]clockHypothesis, [3][2]float64) {
	clocks := [2]clockHypothesis{{Scale: 1, Offset: 7}, {Scale: 1, Offset: 14}}
	if drift {
		clocks[0].Scale, clocks[1].Scale = .98, 1.02
	}
	var views [3]view
	var mappings [3][]int
	var original [3][2]float64
	for source := range views {
		scale, offset := 1.0, 0.0
		if source > 0 {
			scale, offset = clocks[source-1].Scale, clocks[source-1].Offset
		}
		views[source] = view{ID: []string{"A", "B", "C"}[source], Geometry: neutral(), Frames: make([]frame, 160)}
		mappings[source] = make([]int, 160)
		original[source] = [2]float64{18*scale + offset, 34*scale + offset}
		for i := range views[source].Frames {
			pts := (18+float64(i)/10)*scale + offset
			if drift && source > 0 {
				pts += float64((i+source)%3) * .007
			}
			f := frame{PTS: pts, Previous: i - 5}
			if f.Previous < 0 {
				f.Previous = -1
			}
			for patch := range f.Patches {
				seed := uint32(0x51fa11 + int64(i)*8917 + int64(patch)*1237)
				if i < 20 || i >= 140 || kind == "unrelated" {
					seed ^= uint32(source+1) * 0x1135137
				}
				if kind == "static" && i >= 20 && i < 140 {
					seed = uint32(0x888888 + patch*1237)
				}
				f.Patches[patch] = boundaryDescriptor(seed)
			}
			views[source].Frames[i] = f
			mappings[source][i] = i
		}
	}
	return views, mappings, clocks, original
}

func boundaryAppearance(t *testing.T, views [3]view, mappings [3][]int) []uint32 {
	t.Helper()
	appearance := make([]uint32, len(views[0].Frames))
	for ai := range appearance {
		if mappings[0][ai] < 0 || mappings[1][ai] < 0 || mappings[2][ai] < 0 {
			continue
		}
		mask := uint32((1 << patchCount) - 1)
		for left := 0; left < 3; left++ {
			for right := left + 1; right < 3; right++ {
				m, err := compareFrames(views[left], mappings[left][ai], views[right], mappings[right][ai], &budget{})
				if err != nil {
					t.Fatal(err)
				}
				mask &= m.Appearance
			}
		}
		appearance[ai] = mask
	}
	return appearance
}

func TestObservedBoundaryContainsColdOpenAndTrailingContent(t *testing.T) {
	for _, kind := range []string{"moving", "few-edge-patches", "isolated-full-frame", "edge-mapping-gap", "internal-mapping-gap", "drift"} {
		t.Run(kind, func(t *testing.T) {
			views, mappings, clocks, bounds := boundaryFixture("moving", kind == "drift")
			if kind == "few-edge-patches" || kind == "isolated-full-frame" {
				patches := 7
				if kind == "isolated-full-frame" {
					patches = patchCount
				}
				for _, edge := range []int{8, 150} {
					for source := range views {
						for patch := 0; patch < patches; patch++ {
							views[source].Frames[edge].Patches[patch] = views[0].Frames[50].Patches[patch]
						}
					}
				}
			}
			if kind == "edge-mapping-gap" {
				for i := 20; i < 28; i++ {
					mappings[1][i] = -1
				}
			}
			if kind == "internal-mapping-gap" {
				mappings[2][60] = -1
			}
			work := &budget{}
			if err := validateBoundaryViews(views, work); err != nil {
				t.Fatal(err)
			}
			if err := validateBoundaryMappings(views, mappings, clocks, work); err != nil {
				t.Fatal(err)
			}
			boundary, err := observedCommonBoundary(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 160, (1<<patchCount)-1, bounds, work)
			if kind == "internal-mapping-gap" {
				if err != nil || boundary.Reason == "" {
					t.Fatalf("a missing interior slot was bridged: %+v, %v", boundary, err)
				}
				return
			}
			if err != nil || boundary.Reason != "" {
				t.Fatalf("contained positive did not reach audit: %+v, %v", boundary, err)
			}
			boundary, err = auditObservedBoundary(views, mappings, boundary, newPosteriorEvidenceCache(views, clocks, nil, nil), work)
			if err != nil || boundary.Reason != "" {
				t.Fatalf("contained positive failed complete remeasurement: %+v, %v", boundary, err)
			}
			for source := range views {
				if boundary.Bounds[source][0] < views[source].Frames[20].PTS || boundary.Bounds[source][1] > views[source].Frames[139].PTS || boundary.Bounds[source][1]-boundary.Bounds[source][0] < 8 {
					t.Fatalf("source %d escaped observed shared content: %+v", source, boundary)
				}
			}
			if boundary.Support != (1<<patchCount)-1 || boundary.MinimumCoverage < 850 {
				t.Fatal("remeasurement changed support or diluted coverage")
			}
		})
	}
}

func TestObservedBoundaryRemeasuresFixedSupportAndMotion(t *testing.T) {
	for _, kind := range []string{"appearance", "motion"} {
		views, mappings, clocks, bounds := boundaryFixture("moving", false)
		boundary, err := observedCommonBoundary(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 160, (1<<patchCount)-1, bounds, &budget{})
		if err != nil || boundary.Reason != "" {
			t.Fatal("valid baseline missing")
		}
		if kind == "appearance" {
			for i := 65; i < 100; i++ {
				for patch := range views[2].Frames[i].Patches {
					views[2].Frames[i].Patches[patch] = boundaryDescriptor(uint32(0x171 + i*511 + patch*993))
				}
			}
		} else {
			for source := range views {
				for i := 20; i < 140; i++ {
					views[source].Frames[i].Patches = views[0].Frames[20].Patches
				}
			}
		}
		audited, err := auditObservedBoundary(views, mappings, boundary, newPosteriorEvidenceCache(views, clocks, nil, nil), &budget{})
		if err != nil || audited.Reason != kind || audited.Support != boundary.Support {
			t.Fatalf("%s was not remeasured with immutable support: %+v, %v", kind, audited, err)
		}
	}
}

func TestObservedBoundaryRealCadenceDriftAbstainsAtOwnershipBreaks(t *testing.T) {
	for _, scale := range []float64{.98, 1.02} {
		var views [3]view
		for source := range views {
			views[source] = view{ID: []string{"A", "B", "C"}[source], Geometry: neutral(), Frames: make([]frame, 300)}
			for i := range views[source].Frames {
				views[source].Frames[i] = frame{PTS: float64(i) / 10, Previous: i - 5}
				if i < 5 {
					views[source].Frames[i].Previous = -1
				}
			}
		}
		clocks := [2]clockHypothesis{{Scale: scale}, {Scale: 1}}
		var mappings [3][]int
		for source := range views {
			s := 1.0
			if source == 1 {
				s = scale
			}
			var err error
			mappings[source], err = affineMapping(views[0], views[source], s, 0, &budget{})
			if err != nil {
				t.Fatal(err)
			}
		}
		for ai := range views[0].Frames {
			for source := range views {
				if mappings[source][ai] < 0 {
					continue
				}
				for patch := 0; patch < patchCount; patch++ {
					views[source].Frames[mappings[source][ai]].Patches[patch] = boundaryDescriptor(uint32(0xaaa + ai*811 + patch*337))
				}
			}
		}
		bounds := [3][2]float64{{0, 30}, {0, 30 * scale}, {0, 30}}
		components, err := observedCommonComponents(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 300, (1<<patchCount)-1, bounds, &budget{})
		if err != nil || len(components) != 0 {
			t.Fatalf("scale %v bridged ownership discontinuities: %+v, %v", scale, components, err)
		}
	}
}

func TestObservedBoundaryRejectsStaticAndUnrelatedControls(t *testing.T) {
	for _, kind := range []string{"static", "unrelated"} {
		views, mappings, clocks, bounds := boundaryFixture(kind, false)
		boundary, err := observedCommonBoundary(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 160, (1<<patchCount)-1, bounds, &budget{})
		if err != nil || boundary.Reason == "" {
			t.Fatalf("%s supplied certified edges: %+v, %v", kind, boundary, err)
		}
	}
}

func TestObservedBoundaryHonorsOriginalWindowAndHalfOpenEnd(t *testing.T) {
	views, mappings, clocks, bounds := boundaryFixture("moving", false)
	for source := range bounds {
		bounds[source][0] = views[source].Frames[30].PTS
		bounds[source][1] = views[source].Frames[130].PTS
	}
	boundary, err := observedCommonBoundary(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 160, (1<<patchCount)-1, bounds, &budget{})
	if err != nil || boundary.Reason != "" {
		t.Fatalf("valid inner window rejected: %+v, %v", boundary, err)
	}
	for source := range bounds {
		if boundary.Bounds[source][0] < bounds[source][0] || boundary.Bounds[source][1] > views[source].Frames[129].PTS {
			t.Fatal("outside-window or exclusive-end frame supplied an edge")
		}
	}
	if strictlyInsideBoundary(views, mappings, 129, boundary.Bounds) {
		t.Fatal("exclusive endpoint counted as an interior sample")
	}
}

func TestObservedBoundaryRejectsUnknownPTSAndReusedOwnership(t *testing.T) {
	for _, invalid := range []float64{math.NaN(), math.Inf(1), -1, 18} {
		views, _, _, _ := boundaryFixture("moving", false)
		views[1].Frames[40].PTS = invalid
		if validateBoundaryViews(views, &budget{}) == nil {
			t.Fatal("unknown or nonmonotonic PTS supplied boundary evidence")
		}
	}
	views, mappings, clocks, _ := boundaryFixture("moving", false)
	mappings[1][40] = mappings[1][39]
	if validateBoundaryMappings(views, mappings, clocks, &budget{}) == nil {
		t.Fatal("reused source frame supplied boundary evidence")
	}
}

func TestObservedBoundaryBudgetFailureReturnsNoEvidence(t *testing.T) {
	views, mappings, clocks, bounds := boundaryFixture("moving", false)
	appearance := boundaryAppearance(t, views, mappings)
	for _, work := range []*budget{{ClockLookups: maxClockLookups}, {PatchComparisons: maxPatchComparisons}} {
		result, err := observedCommonBoundary(views, mappings, clocks, appearance, 0, 160, (1<<patchCount)-1, bounds, work)
		if err == nil || result != (boundaryAudit{}) {
			t.Fatal("budget failure returned partial edge evidence")
		}
	}
}

func TestObservedBoundaryRejectsDisconnectedMovingIsland(t *testing.T) {
	views, mappings, clocks, bounds := boundaryFixture("moving", false)
	// A 0.7-second copied moving island precedes a 0.7-second unmatched
	// protected gap. The known twelve-second shared core still starts at 20.
	for source := range views {
		for i := 5; i <= 12; i++ {
			views[source].Frames[i].Patches = views[0].Frames[70+i].Patches
		}
	}
	work := &budget{}
	boundary, err := observedCommonBoundary(views, mappings, clocks, boundaryAppearance(t, views, mappings), 0, 160, (1<<patchCount)-1, bounds, work)
	if err != nil || boundary.Reason != "" {
		t.Fatalf("required twelve-second core was lost: %+v, %v", boundary, err)
	}
	boundary, err = auditObservedBoundary(views, mappings, boundary, newPosteriorEvidenceCache(views, clocks, nil, nil), work)
	if err != nil || boundary.Reason != "" {
		t.Fatalf("required twelve-second core did not pass audit: %+v, %v", boundary, err)
	}
	for source := range views {
		if boundary.Bounds[source][0] < views[source].Frames[20].PTS || boundary.Bounds[source][1] > views[source].Frames[139].PTS {
			t.Fatalf("disconnected moving island crossed the protected gap: %+v", boundary)
		}
	}
}
