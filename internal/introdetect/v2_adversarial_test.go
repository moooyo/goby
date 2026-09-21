package introdetect

import "testing"

// These fixtures describe feature-level mechanisms, not real-media accuracy.
// All variants retain the same complete audio repetition and source timestamps.
var v2OpeningStarts = [...]int64{
	10*TicksPerSecond + 1200000,
	35*TicksPerSecond + 3700000,
	63*TicksPerSecond + 8100000,
}

// Every pair of base states is 32 bits apart. A scene's low-bit drift remains
// within one eight-bit neighborhood without conflating different scenes.
var v2VisualStates = [...]uint64{
	0xaaaaaaaaaaaaaaaa,
	0xcccccccccccccccc,
	0xf0f0f0f0f0f0f0f0,
	0xff00ff00ff00ff00,
	0xffff0000ffff0000,
	0xffffffff00000000,
}

func v2RewriteOpeningVisuals(t *testing.T, cohort *Cohort, change func(int, int64) (uint64, uint16)) {
	t.Helper()
	if len(cohort.Episodes) != len(v2OpeningStarts) {
		t.Fatal("visual fixture requires the three original cold-open timelines")
	}
	for episodeIndex := range cohort.Episodes {
		episode := &cohort.Episodes[episodeIndex]
		count := int64(0)
		for frameIndex := range episode.Visual {
			frame := &episode.Visual[frameIndex]
			relative := frame.Ticks - v2OpeningStarts[episodeIndex]
			if relative < 0 || relative >= 50*TicksPerSecond {
				continue
			}
			if relative != count*TicksPerSecond {
				t.Fatalf("visual fixture lost its actual one-second sampling grid: episode=%d relative=%d", episodeIndex, relative)
			}
			frame.Hash, frame.Contrast = change(episodeIndex, relative)
			count++
		}
		if count != 50 {
			t.Fatalf("visual fixture has %d opening samples, want 50", count)
		}
	}
}

func v2SlowSceneHash(relative int64) uint64 {
	second := relative / TicksPerSecond
	scene, phase := second/8, uint(second%8)
	return v2VisualStates[scene%int64(len(v2VisualStates))] ^ (uint64(1)<<phase - 1)
}

func v2RequireNoAutomatic(t *testing.T, result Result) {
	t.Helper()
	requireNoAutomatic(t, result)
	for _, group := range result.Groups {
		if group.Status == Qualified {
			t.Fatalf("unsafe visual fixture retained an automatic group: %#v", group)
		}
	}
}

func TestAnalyzeV2SlowSceneChangesRetainQualifiedEvidence(t *testing.T) {
	cohort := testCohort()
	v2RewriteOpeningVisuals(t, &cohort, func(_ int, relative int64) (uint64, uint16) {
		return v2SlowSceneHash(relative), 200
	})
	result := analyzeTest(t, cohort)
	if len(result.Groups) != 1 || len(result.Groups[0].Members) != 3 || result.Groups[0].Metrics.PairCount != 3 {
		t.Fatalf("slow scenes lost their complete independent witness group: %#v", result)
	}
	for index, episode := range result.Episodes {
		if episode.Status != Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("fully repeated slow scenes did not qualify: %#v", episode)
		}
		candidate := episode.Candidates[0]
		// Six sustained scenes have real one-bit changes between samples, with
		// only the cuts contributing to the retained eight-bit motion metric.
		if candidate.Metrics.VisualChangeCoveragePermille <= 0 || candidate.Metrics.VisualChangeCoveragePermille >= 300 || candidate.Metrics.VisualTransitions == 0 {
			t.Fatalf("slow-scene motion was erased or inflated: %#v", candidate.Metrics)
		}
		if candidate.Interval.StartTicks < v2OpeningStarts[index]+2*TicksPerSecond ||
			candidate.Interval.EndTicks > v2OpeningStarts[index]+48*TicksPerSecond ||
			candidate.Interval.EndTicks-candidate.Interval.StartTicks < 35*TicksPerSecond {
			t.Fatalf("visual evidence changed the safe audio extent: %#v", candidate)
		}
	}
}

func TestAnalyzeV2SparseSharedTitlesCannotHideDifferentStory(t *testing.T) {
	cohort := testCohort()
	v2RewriteOpeningVisuals(t, &cohort, func(episodeIndex int, relative int64) (uint64, uint16) {
		second := relative / TicksPerSecond
		phase := second % 5
		if phase == 2 || phase == 3 {
			// Each five-second cell contains a shared one-second anchor run.
			// The first and last safe audio boundaries also have shared titles.
			return v2VisualStates[(second/5)%int64(len(v2VisualStates))] ^ uint64(phase-2), 200
		}
		// The remaining story samples are pairwise 32 bits apart at the
		// same timestamp, regardless of their shared low-bit variation.
		return v2VisualStates[episodeIndex+3] ^ uint64(second%16), 200
	})
	v2RequireNoAutomatic(t, analyzeTest(t, cohort))
}

func TestAnalyzeV2UnobservableFramesRemainInTimeCoverage(t *testing.T) {
	for _, kind := range []string{"all_sources_black", "one_source_black"} {
		t.Run(kind, func(t *testing.T) {
			cohort := testCohort()
			v2RewriteOpeningVisuals(t, &cohort, func(episodeIndex int, relative int64) (uint64, uint16) {
				hash := v2SlowSceneHash(relative)
				contrast := uint16(200)
				if relative/TicksPerSecond%4 == 0 && (kind == "all_sources_black" || episodeIndex == 2) {
					hash, contrast = 0, 0
				}
				// Keep explicit PTS samples for unobservable time. Dropping them
				// from the denominator would turn 75% visibility into 100%.
				return hash, contrast
			})
			v2RequireNoAutomatic(t, analyzeTest(t, cohort))
		})
	}
}

func TestAnalyzeV2NearStateLogoNoiseDoesNotCreateSceneDiversity(t *testing.T) {
	for _, kind := range []string{"gradual_logo", "one_bit_flicker"} {
		t.Run(kind, func(t *testing.T) {
			cohort := testCohort()
			v2RewriteOpeningVisuals(t, &cohort, func(_ int, relative int64) (uint64, uint16) {
				second := relative / TicksPerSecond
				var variation uint64
				if kind == "gradual_logo" {
					variation = uint64(1)<<uint(second%8) - 1
				} else {
					variation = uint64(1) << uint(second%32)
				}
				// Many exact hashes still describe one bounded visual state.
				return v2VisualStates[0] ^ variation, 200
			})
			v2RequireNoAutomatic(t, analyzeTest(t, cohort))
		})
	}
}

func TestAnalyzeV2RepeatedVisualStateCycleDoesNotBecomeAutomatic(t *testing.T) {
	cohort := testCohort()
	v2RewriteOpeningVisuals(t, &cohort, func(_ int, relative int64) (uint64, uint16) {
		second := relative / TicksPerSecond
		// Four distinct two-second states repeat every eight seconds. Each
		// state has a full second of local support, and none is dominant.
		// The audio remains the original nonperiodic shared opening.
		return v2VisualStates[(second/2)%4] ^ uint64(second%2), 200
	})
	v2RequireNoAutomatic(t, analyzeTest(t, cohort))
}
