package introdetect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// These generated features exercise matcher mechanics only. They are not real
// media and are not evidence of semantic intro accuracy or calibrated recall.
type testRandom uint64

func (r *testRandom) next() uint64 {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return uint64(*r)
}

type testOpening struct {
	start, duration int64
	variant         uint64
}

func testEpisode(number int, duration int64, openings ...testOpening) Episode {
	window := min(duration, 600*TicksPerSecond)
	e := Episode{EpisodeKey: fmt.Sprintf("episode-%02d", number), SourceKey: fmt.Sprintf("source-%02d", number),
		ContentIdentity: fmt.Sprintf("whole-content-%02d", number), AlgorithmProfile: "synthetic-mechanics-v1",
		DurationTicks: duration, AudioBoundaryUncertaintyTicks: 2 * TicksPerSecond}
	step := TicksPerSecond / 4
	random := testRandom(123456789 + uint64(number)*23456789)
	for tick := int64(0); tick < window; {
		stop := min(tick+step, window)
		for _, opening := range openings {
			if tick < opening.start && stop > opening.start {
				stop = opening.start
			}
		}
		e.Audio = append(e.Audio, AudioSample{tick, stop, uint32(random.next())})
		tick = stop
	}
	for tick := int64(0); tick < window; tick += TicksPerSecond {
		e.Visual = append(e.Visual, VisualSample{tick, random.next(), 200})
	}
	for _, opening := range openings {
		end := min(window, opening.start+opening.duration)
		// Replace a real time interval, not a number of frames. Fractional
		// cold-open offsets intentionally change the sampling grid.
		audio := make([]AudioSample, 0, len(e.Audio))
		for _, sample := range e.Audio {
			if sample.EndTicks <= opening.start || sample.StartTicks >= end {
				audio = append(audio, sample)
			}
		}
		shared := testRandom(opening.variant)
		for tick := opening.start; tick < end; tick += step {
			audio = append(audio, AudioSample{tick, min(end, tick+step), uint32(shared.next())})
		}
		sort.Slice(audio, func(i, j int) bool { return audio[i].StartTicks < audio[j].StartTicks })
		e.Audio = audio
		visual := make([]VisualSample, 0, len(e.Visual))
		for _, sample := range e.Visual {
			if sample.Ticks < opening.start || sample.Ticks >= end {
				visual = append(visual, sample)
			}
		}
		shared = testRandom(opening.variant + 987654321)
		var sharedState uint64
		for tick := opening.start; tick < end; tick += TicksPerSecond {
			// Each shared state has two actual observations a second apart.
			// This proves the v2 minimum state support instead of treating one
			// instantaneous random hash as a second of observed state duration.
			if (tick-opening.start)/TicksPerSecond%2 == 0 {
				sharedState = shared.next()
			}
			visual = append(visual, VisualSample{tick, sharedState, 200})
		}
		sort.Slice(visual, func(i, j int) bool { return visual[i].Ticks < visual[j].Ticks })
		e.Visual = visual
	}
	return e
}

func testCohort() Cohort {
	return Cohort{Key: "series-one-season-one-window-one", Episodes: []Episode{
		testEpisode(1, 150*TicksPerSecond, testOpening{10*TicksPerSecond + 1200000, 50 * TicksPerSecond, 778899}),
		testEpisode(2, 150*TicksPerSecond, testOpening{35*TicksPerSecond + 3700000, 50 * TicksPerSecond, 778899}),
		testEpisode(3, 150*TicksPerSecond, testOpening{63*TicksPerSecond + 8100000, 50 * TicksPerSecond, 778899}),
	}}
}

func analyzeTest(t *testing.T, cohort Cohort) Result {
	t.Helper()
	result, err := Analyze(context.Background(), cohort, Options{})
	if err != nil {
		t.Fatalf("analyze feature fixture: %v", err)
	}
	return result
}

func requireNoAutomatic(t *testing.T, result Result) {
	t.Helper()
	for _, episode := range result.Episodes {
		if episode.Status == Qualified {
			t.Fatalf("unsafe fixture qualified: %#v", episode)
		}
		for _, candidate := range episode.Candidates {
			if candidate.Status == Qualified {
				t.Fatalf("unsafe candidate qualified: %#v", candidate)
			}
		}
	}
}

func hasResultReason(result Result, reason Reason) bool {
	for _, episode := range result.Episodes {
		if slices.Contains(episode.Reasons, reason) {
			return true
		}
	}
	return false
}

func TestAnalyzeColdOpenOffsetsAndConservativeSourceBoundaries(t *testing.T) {
	cohort := testCohort()
	before, _ := json.Marshal(cohort)
	result := analyzeTest(t, cohort)
	if len(result.Groups) != 1 || len(result.Groups[0].Members) != 3 || result.Groups[0].Metrics.PairCount != 3 {
		t.Fatalf("expected one complete three-episode witness group: %#v", result)
	}
	starts := []int64{10*TicksPerSecond + 1200000, 35*TicksPerSecond + 3700000, 63*TicksPerSecond + 8100000}
	for i, episode := range result.Episodes {
		if episode.Status != Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("independent audiovisual repetition did not qualify: %#v", episode)
		}
		candidate := episode.Candidates[0]
		if candidate.Interval.StartTicks < starts[i]+2*TicksPerSecond || candidate.Interval.EndTicks > starts[i]+48*TicksPerSecond ||
			candidate.Interval.EndTicks-candidate.Interval.StartTicks < 35*TicksPerSecond {
			t.Fatalf("candidate escaped the known safe interior: %#v", candidate)
		}
		if candidate.Metrics.AudioSimilarityPermille < 990 || candidate.Metrics.VisualSimilarityPermille < 990 ||
			candidate.Metrics.BoundaryUncertaintyTicks < 2*TicksPerSecond {
			t.Fatalf("evidence did not preserve actual similarity and extraction uncertainty: %#v", candidate.Metrics)
		}
	}
	after, _ := json.Marshal(cohort)
	if string(before) != string(after) {
		t.Fatal("Analyze mutated caller-owned features")
	}
	cohort.Episodes[0], cohort.Episodes[2] = cohort.Episodes[2], cohort.Episodes[0]
	permuted := analyzeTest(t, cohort)
	if !reflect.DeepEqual(result, permuted) {
		t.Fatal("episode input permutation changed deterministic output")
	}
}

func TestAnalyzeMultipleOpeningVersionsRemainSeparate(t *testing.T) {
	cohort := testCohort()
	for i := 4; i <= 6; i++ {
		cohort.Episodes = append(cohort.Episodes, testEpisode(i, 150*TicksPerSecond,
			testOpening{int64(i*7) * TicksPerSecond, 50 * TicksPerSecond, 222333444}))
	}
	result := analyzeTest(t, cohort)
	if len(result.Groups) != 2 {
		t.Fatalf("opening versions did not form two groups: %#v", result.Groups)
	}
	for _, episode := range result.Episodes {
		if episode.Status != Qualified || len(episode.Candidates) != 1 || len(episode.Candidates[0].Support) != 3 {
			t.Fatalf("different versions inflated or lost independent support: %#v", episode)
		}
	}
}

func TestAnalyzeCannotPublishAudioAloneStaticLogosOrDarkFrames(t *testing.T) {
	for _, kind := range []string{"different_visuals", "static_logo", "low_contrast", "silence", "missing_visual"} {
		t.Run(kind, func(t *testing.T) {
			cohort := testCohort()
			for i := range cohort.Episodes {
				e := &cohort.Episodes[i]
				random := testRandom(uint64(45454545 + 123456*i))
				for j := range e.Visual {
					switch kind {
					case "different_visuals":
						e.Visual[j].Hash = random.next()
					case "static_logo":
						e.Visual[j].Hash = 0xaaaabbbbccccdddd
					case "low_contrast":
						e.Visual[j].Contrast = 1
					}
				}
				if kind == "silence" {
					for j := range e.Audio {
						e.Audio[j].Fingerprint = 0
					}
				}
				if kind == "missing_visual" {
					e.Visual = nil
				}
			}
			result := analyzeTest(t, cohort)
			requireNoAutomatic(t, result)
			if kind == "static_logo" {
				if len(result.Groups) == 0 {
					t.Fatal("complete audio and matched static imagery lost its explicit review evidence")
				}
				for _, group := range result.Groups {
					if group.Status != Review || group.Metrics.VisualDistinctStates != 1 || !slices.Contains(group.Reasons, LowVisualDiversity) {
						t.Fatalf("static imagery was not retained as an honest one-state review: %#v", group)
					}
				}
			} else if len(result.Groups) != 0 {
				t.Fatalf("fixture without useful audiovisual support retained a group: %#v", result.Groups)
			}
			want := map[string]Reason{"different_visuals": AudioWithoutVisual, "static_logo": LowVisualDiversity,
				"low_contrast": InsufficientVisual, "silence": LowAudioEntropy, "missing_visual": MissingVisual}[kind]
			if !hasResultReason(result, want) {
				t.Fatalf("negative reason %s is missing: %#v", want, result.Episodes)
			}
		})
	}
}

func TestAnalyzeDuplicateIdentitiesCannotAddIndependentSupport(t *testing.T) {
	for _, kind := range []string{"whole_content", "source_alias", "alternate_encode", "transitive_aliases"} {
		t.Run(kind, func(t *testing.T) {
			cohort := testCohort()
			switch kind {
			case "whole_content":
				cohort.Episodes[2] = cohort.Episodes[0]
				cohort.Episodes[2].EpisodeKey, cohort.Episodes[2].SourceKey = "episode-03", "source-03"
			case "source_alias":
				cohort.Episodes[2] = cohort.Episodes[0]
				cohort.Episodes[2].EpisodeKey = "episode-03"
			case "alternate_encode":
				cohort.Episodes[2].EpisodeKey = cohort.Episodes[0].EpisodeKey
			case "transitive_aliases":
				cohort.Episodes[1].ContentIdentity = cohort.Episodes[0].ContentIdentity
				cohort.Episodes[2].EpisodeKey = cohort.Episodes[1].EpisodeKey
			}
			result := analyzeTest(t, cohort)
			requireNoAutomatic(t, result)
			if !hasResultReason(result, DuplicateIdentity) || !hasResultReason(result, InsufficientEpisodes) {
				t.Fatalf("duplicates were counted as independent evidence: %#v", result.Episodes)
			}
		})
	}
}

func TestAnalyzeUnavailableAliasDoesNotReplaceComparableFeatures(t *testing.T) {
	for _, kind := range []string{"missing_audio", "other_profile"} {
		cohort := testCohort()
		alias := cohort.Episodes[0]
		alias.SourceKey, alias.ContentIdentity = "source-00-unusable", "other-encode"
		if kind == "missing_audio" {
			alias.Audio = nil
		} else {
			alias.AlgorithmProfile = "isolated-extractor-profile"
		}
		cohort.Episodes = append(cohort.Episodes, alias)
		result := analyzeTest(t, cohort)
		if len(result.Groups) != 1 || len(result.Groups[0].Members) != 3 {
			t.Fatalf("an unavailable alias displaced an independent comparable episode: %#v", result)
		}
		for _, episode := range result.Episodes {
			if episode.SourceKey == alias.SourceKey {
				if episode.Status != NoResult || !slices.Contains(episode.Reasons, DuplicateIdentity) {
					t.Fatalf("unused alias was counted as extra support: %#v", episode)
				}
			} else if episode.Status != Qualified {
				t.Fatalf("comparable representative lost the opening: %#v", episode)
			}
		}
	}
}

func TestAnalyzeRequiresPairwiseConsensusNotATransitiveBridge(t *testing.T) {
	cohort := testCohort()
	for i := range cohort.Episodes[1].Audio {
		cohort.Episodes[1].Audio[i].Fingerprint ^= 0x0f
	}
	for i := range cohort.Episodes[2].Audio {
		cohort.Episodes[2].Audio[i].Fingerprint ^= 0xf0
	}
	result := analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if len(result.Groups) != 0 || !hasResultReason(result, InsufficientConsensus) {
		t.Fatalf("two individually near matches became an unsupported three-way group: %#v", result)
	}
}

func TestAnalyzeSpreadOutBitErrorsStillProduceOffsetCandidates(t *testing.T) {
	cohort := testCohort()
	for i := range cohort.Episodes {
		for j := range cohort.Episodes[i].Audio {
			word := cohort.Episodes[i].Audio[j].Fingerprint &^ uint32(0x01010101)
			if i == 1 {
				word ^= 0x01010101
			}
			cohort.Episodes[i].Audio[j].Fingerprint = word
		}
	}
	result := analyzeTest(t, cohort)
	for _, episode := range result.Episodes {
		if episode.Status != Qualified || len(episode.Candidates) != 1 || episode.Candidates[0].Metrics.AudioSimilarityPermille < 850 {
			t.Fatalf("one bit of noise in every byte evaded offset voting: %#v", result)
		}
	}
}

func TestAnalyzeSparseNoiseCannotMakeSilenceInformative(t *testing.T) {
	cohort := Cohort{Key: "mostly-silence"}
	for i := 1; i <= 3; i++ {
		e := testEpisode(i, 90*TicksPerSecond, testOpening{10 * TicksPerSecond, 50 * TicksPerSecond, 778899})
		for j := range e.Audio {
			sample := &e.Audio[j]
			if sample.StartTicks >= 10*TicksPerSecond && sample.StartTicks < 60*TicksPerSecond {
				position := int((sample.StartTicks - 10*TicksPerSecond) / (TicksPerSecond / 4))
				sample.Fingerprint = 0
				if position%14 == 0 && position/14 < 13 {
					sample.Fingerprint = uint32(0xffff) << uint(position/14)
				}
			}
		}
		cohort.Episodes = append(cohort.Episodes, e)
	}
	result := analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if len(result.Groups) != 0 || !hasResultReason(result, LowAudioEntropy) {
		t.Fatalf("13 isolated noisy bins legitimized 187 silent bins: %#v", result)
	}
}

func TestAnalyzeNearbyOffsetsDoNotHideCompetingIntervals(t *testing.T) {
	for _, offsetStep := range []int64{TicksPerSecond, TicksPerSecond / 4} {
		cohort := Cohort{Key: fmt.Sprintf("nearby-offset-%d", offsetStep)}
		for i := 0; i < 3; i++ {
			cohort.Episodes = append(cohort.Episodes, testEpisode(i+1, 230*TicksPerSecond,
				testOpening{10 * TicksPerSecond, 60 * TicksPerSecond, 778899},
				testOpening{150*TicksPerSecond + int64(i)*offsetStep, 50 * TicksPerSecond, 333444555}))
		}
		result := analyzeTest(t, cohort)
		requireNoAutomatic(t, result)
		if len(result.Groups) < 2 || !hasResultReason(result, CompetingIntervals) {
			t.Fatalf("offset suppression concealed another supported interval: %#v", result)
		}
	}
}

func TestAnalyzeLimitedOtherPairCannotLeaveAnyQualifiedProjection(t *testing.T) {
	cohort := Cohort{Key: "limited-competing-group"}
	cohort.Episodes = append(cohort.Episodes, testEpisode(1, 590*TicksPerSecond,
		testOpening{10 * TicksPerSecond, 60 * TicksPerSecond, 778899},
		testOpening{170 * TicksPerSecond, 200 * TicksPerSecond, 66554433},
		testOpening{420 * TicksPerSecond, 50 * TicksPerSecond, 333444555}))
	for i := 2; i <= 3; i++ {
		cohort.Episodes = append(cohort.Episodes, testEpisode(i, 590*TicksPerSecond,
			testOpening{10 * TicksPerSecond, 60 * TicksPerSecond, 778899}))
	}
	for i := 4; i <= 5; i++ {
		cohort.Episodes = append(cohort.Episodes, testEpisode(i, 590*TicksPerSecond,
			testOpening{int64(190+(i-4)*20) * TicksPerSecond, 200 * TicksPerSecond, 66554433},
			testOpening{int64(460+(i-4)*30) * TicksPerSecond, 50 * TicksPerSecond, 333444555}))
	}
	o := DefaultOptions()
	o.MaxOffsetCandidates = 1
	result, err := Analyze(context.Background(), cohort, o)
	if err != nil {
		t.Fatal(err)
	}
	requireNoAutomatic(t, result)
	if len(result.Groups) == 0 || !hasResultReason(result, CandidateSearchLimited) {
		t.Fatalf("limited search lost its review evidence: %#v", result)
	}
	for _, group := range result.Groups {
		if group.Status != Review || !slices.Contains(group.Reasons, CandidateSearchLimited) {
			t.Fatalf("group exposed a qualified publication bypass: %#v", group)
		}
	}
}

func TestAnalyzeFlickersCannotMakeAStaticLogoDynamic(t *testing.T) {
	cohort := testCohort()
	starts := []int64{10*TicksPerSecond + 1200000, 35*TicksPerSecond + 3700000, 63*TicksPerSecond + 8100000}
	for i := range cohort.Episodes {
		for j := range cohort.Episodes[i].Visual {
			frame := &cohort.Episodes[i].Visual[j]
			if frame.Ticks >= starts[i] && frame.Ticks < starts[i]+50*TicksPerSecond {
				position := (frame.Ticks - starts[i]) / TicksPerSecond
				frame.Hash = 0xaaaaaaaaaaaaaaaa
				switch position {
				case 10:
					frame.Hash ^= 0xffff
				case 25:
					frame.Hash ^= 0xffff0000
				case 40:
					frame.Hash ^= 0xffff00000000
				}
			}
		}
	}
	result := analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if len(result.Groups) == 0 || !hasResultReason(result, LowVisualDiversity) {
		t.Fatalf("three brief flickers legitimized a predominantly static logo: %#v", result)
	}
	for _, group := range result.Groups {
		if group.Status != Review || group.Metrics.VisualDominantStatePermille <= DefaultOptions().MaxVisualStateDominancePermille {
			t.Fatalf("flickers hid full-window state dominance: %#v", group)
		}
	}
}

func TestAnalyzeUnmatchedVisualPulsesCannotSupplySceneEvidence(t *testing.T) {
	cohort := Cohort{Key: "unmatched-visual-pulses"}
	for i := 1; i <= 3; i++ {
		e := testEpisode(i, 90*TicksPerSecond, testOpening{10 * TicksPerSecond, 50 * TicksPerSecond, 778899})
		e.AudioBoundaryUncertaintyTicks = 0
		e.Visual = nil
		random := testRandom(uint64(34343434 + i*121212))
		for tick := int64(0); tick < e.DurationTicks; tick += TicksPerSecond / 4 {
			hash := random.next()
			if tick >= 10*TicksPerSecond && tick < 60*TicksPerSecond {
				position := int((tick - 10*TicksPerSecond) / (TicksPerSecond / 4))
				hash = 0xaaaaaaaaaaaaaaaa ^ (uint64(1) << uint(position%16))
				if position%20 == 5 || position%20 == 10 || position%20 == 15 {
					hash = 0xaaaaaaaaaaaaaaaa ^ (uint64(0xffff) << uint((i-1)*16))
				}
			}
			e.Visual = append(e.Visual, VisualSample{tick, hash, 200})
		}
		cohort.Episodes = append(cohort.Episodes, e)
	}
	result := analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if len(result.Groups) == 0 || !hasResultReason(result, LowVisualDiversity) {
		t.Fatalf("30 unmatched pulses supplied the missing temporal confirmation: %#v", result)
	}
	for _, group := range result.Groups {
		if group.Status != Review || group.Metrics.VisualDistinctStates != 1 {
			t.Fatalf("unmatched pulse states borrowed the matched base-image anchors: %#v", group)
		}
	}
}

func TestAnalyzeSparseAudioCannotBorrowTheOtherSourcesTimeCoverage(t *testing.T) {
	for _, sparseFirst := range []bool{false, true} {
		for _, gap := range []int64{TicksPerSecond, 2 * TicksPerSecond} {
			cohort := Cohort{Key: "bilateral-audio-coverage"}
			for i := 1; i <= 3; i++ {
				e := testEpisode(i, 100*TicksPerSecond, testOpening{10 * TicksPerSecond, 50 * TicksPerSecond, 778899})
				samples := make([]AudioSample, 0, len(e.Audio)/8)
				for j := 0; j < len(e.Audio); j += 8 {
					sample := e.Audio[j]
					sample.EndTicks = min(sample.StartTicks+2*TicksPerSecond, e.DurationTicks)
					if i == 3 {
						sample.EndTicks = sample.StartTicks + TicksPerSecond/100
					}
					samples = append(samples, sample)
				}
				e.Audio = samples
				if i == 3 && sparseFirst {
					e.EpisodeKey = "episode-00-sparse"
				}
				cohort.Episodes = append(cohort.Episodes, e)
			}
			o := DefaultOptions()
			o.MaxAudioGapTicks = gap
			result, err := Analyze(context.Background(), cohort, o)
			if err != nil {
				t.Fatal(err)
			}
			requireNoAutomatic(t, result)
			if len(result.Groups) != 0 {
				t.Fatalf("sparse source inherited the dense source's agreement: %#v", result)
			}
			if gap == 2*TicksPerSecond && !hasResultReason(result, InsufficientAudio) {
				t.Fatalf("bilateral time coverage was not measured: %#v", result)
			}
		}
	}
}

func TestAnalyzeCompetingIntervalsAndShortOpeningsRequireReview(t *testing.T) {
	for _, kind := range []string{"competing", "short", "window_end", "weak_visual"} {
		t.Run(kind, func(t *testing.T) {
			cohort := Cohort{Key: kind}
			for i := 1; i <= 3; i++ {
				var e Episode
				switch kind {
				case "competing":
					e = testEpisode(i, 230*TicksPerSecond, testOpening{10 * TicksPerSecond, 50 * TicksPerSecond, 778899}, testOpening{120 * TicksPerSecond, 50 * TicksPerSecond, 333444555})
				case "short":
					e = testEpisode(i, 90*TicksPerSecond, testOpening{10 * TicksPerSecond, 26 * TicksPerSecond, 778899})
				case "window_end":
					e = testEpisode(i, 900*TicksPerSecond, testOpening{510 * TicksPerSecond, 90 * TicksPerSecond, 778899})
				case "weak_visual":
					e = testEpisode(i, 100*TicksPerSecond, testOpening{10 * TicksPerSecond, 50 * TicksPerSecond, 778899})
					if i == 2 {
						for j := range e.Visual {
							if j%4 == 0 {
								// Twenty-eight bits remain a disagreement under the
								// second fixed v2 candidate's radius of twenty-four.
								e.Visual[j].Hash ^= 0xfffffff
							}
						}
					}
				}
				cohort.Episodes = append(cohort.Episodes, e)
			}
			result := analyzeTest(t, cohort)
			requireNoAutomatic(t, result)
			want := map[string]Reason{"competing": CompetingIntervals, "short": ShortInterval, "window_end": AnalysisBoundary, "weak_visual": WeakVisualEvidence}[kind]
			if len(result.Groups) == 0 || !hasResultReason(result, want) {
				t.Fatalf("uncertain candidate and review reason were not retained: %#v", result)
			}
		})
	}
}

func TestAnalyzeUsesIrregularVisualTimestamps(t *testing.T) {
	cohort := testCohort()
	starts := []int64{10*TicksPerSecond + 1200000, 35*TicksPerSecond + 3700000, 63*TicksPerSecond + 8100000}
	for i := range cohort.Episodes {
		var observations []VisualSample
		for _, frame := range cohort.Episodes[i].Visual {
			if frame.Ticks < starts[i] || frame.Ticks >= starts[i]+50*TicksPerSecond {
				observations = append(observations, frame)
			}
		}
		shared := testRandom(778899 + 987654321)
		states := make([]uint64, 25)
		for state := range states {
			states[state] = shared.next()
		}
		for position := int64(0); position < 100; position++ {
			actual := position*(TicksPerSecond/2) + position%3*(TicksPerSecond/7)
			// The frame's state is sampled at its actual relative presentation
			// time, including when jitter crosses a two-second scene boundary.
			observations = append(observations, VisualSample{starts[i] + actual, states[actual/(2*TicksPerSecond)], 200})
		}
		sort.Slice(observations, func(a, b int) bool { return observations[a].Ticks < observations[b].Ticks })
		cohort.Episodes[i].Visual = observations
	}
	result := analyzeTest(t, cohort)
	for _, episode := range result.Episodes {
		if episode.Status != Qualified {
			t.Fatalf("irregular presentation times were treated as frame indices: %#v", episode)
		}
	}
}

func TestAnalyzeRejectsOverlongSharedContentAndUnrelatedEpisodes(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		cohort := Cohort{Key: "long-or-unrelated"}
		for i := 1; i <= 3; i++ {
			var openings []testOpening
			if repeated {
				openings = []testOpening{{10 * TicksPerSecond, 220 * TicksPerSecond, 778899}}
			}
			cohort.Episodes = append(cohort.Episodes, testEpisode(i, 250*TicksPerSecond, openings...))
		}
		result := analyzeTest(t, cohort)
		requireNoAutomatic(t, result)
		if len(result.Groups) != 0 {
			t.Fatal("overlong or absent repetition produced a group")
		}
		if repeated && !hasResultReason(result, OverlongRepeat) {
			t.Fatalf("overlong repetition was silently sliced into an intro: %#v", result)
		}
	}
}

type cancelDuringContext struct {
	context.Context
	cancel context.CancelFunc
	after  int
}

func (c *cancelDuringContext) Err() error {
	c.after--
	if c.after == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestAnalyzeCancellationAndLimitsReturnNoPartialResults(t *testing.T) {
	cohort := testCohort()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{ctx, func() context.Context {
		base, stop := context.WithCancel(context.Background())
		t.Cleanup(stop)
		return &cancelDuringContext{Context: base, cancel: stop, after: 30}
	}()} {
		result, err := Analyze(ctx, cohort, Options{})
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("cancellation returned partial publication evidence: %#v, %v", result, err)
		}
	}
	o := DefaultOptions()
	o.MaxComparisons = 1
	result, err := Analyze(context.Background(), cohort, o)
	if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("exhausted work budget returned partial publication evidence: %#v, %v", result, err)
	}
	tooMany := cohort
	tooMany.Episodes = make([]Episode, 33)
	for i := range tooMany.Episodes {
		tooMany.Episodes[i] = cohort.Episodes[i%len(cohort.Episodes)]
	}
	if _, err := Analyze(context.Background(), tooMany, Options{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized cohort was silently truncated: %v", err)
	}
	limited := DefaultOptions()
	limited.MaxAudioSamples = 10
	if _, err := Analyze(context.Background(), cohort, limited); !errors.Is(err, ErrLimit) {
		t.Fatalf("feature bound was ignored: %v", err)
	}
}

func TestAnalyzeRejectsInvalidFeatureContractsAndKeepsUnknowns(t *testing.T) {
	for _, mutate := range []func(*Cohort){
		func(c *Cohort) { c.Key = "" },
		func(c *Cohort) { c.Episodes[0].ContentIdentity = "" },
		func(c *Cohort) { c.Episodes[0].AudioBoundaryUncertaintyTicks = -1 },
		func(c *Cohort) { c.Episodes[0].Audio[1].StartTicks = 0 },
		func(c *Cohort) { c.Episodes[0].Visual[1].Ticks = c.Episodes[0].Visual[0].Ticks },
		func(c *Cohort) { c.Episodes[0].Visual[0].Contrast = 1001 },
		func(c *Cohort) { c.Episodes[1].SourceKey = c.Episodes[0].SourceKey },
	} {
		cohort := testCohort()
		mutate(&cohort)
		if _, err := Analyze(context.Background(), cohort, Options{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid feature contract was admitted: %v", err)
		}
	}
	cohort := testCohort()
	cohort.Episodes[0].AlgorithmProfile = "different-extractor"
	result := analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if !hasResultReason(result, IncompatibleProfile) {
		t.Fatal("incompatible extraction profiles were compared")
	}
	cohort = testCohort()
	cohort.Episodes[0].Audio = nil
	result = analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if !hasResultReason(result, MissingAudio) {
		t.Fatal("missing audio was represented as a positive detection")
	}
	cohort = testCohort()
	cohort.Episodes = cohort.Episodes[:2]
	result = analyzeTest(t, cohort)
	requireNoAutomatic(t, result)
	if !hasResultReason(result, InsufficientEpisodes) {
		t.Fatal("two episodes supplied a three-episode decision")
	}
}
