//go:build linux

package library

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisHistoricalVisualConsensusSurvivesCacheAndRestore(t *testing.T) {
	f := newAnalysisWorkFixture(t, 5)
	f.setIntroDetection(t, true)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	if len(children) != 1 {
		t.Fatalf("five independent episodes need one admitted cohort, got %d", len(children))
	}
	child := children[0]
	f.claim(t, run, child)
	fence := f.fence(child)
	work, err := f.store.GetAnalysisWork(f.ctx, child, fence)
	if err != nil || len(work.Sources) != 5 {
		t.Fatalf("read the admitted independent sources: %v", err)
	}
	work = analysisHistoricalVisualWork(t, work)
	sort.Slice(work.Sources, func(i, j int) bool { return work.Sources[i].EpisodeKey < work.Sources[j].EpisodeKey })

	// A complete opening shares its first thirteen seconds with two sources
	// and its last thirteen seconds with two others. The complete measured
	// cliques therefore cross on the first source, despite sharing a safe
	// ten-second interior. Surrounding rasters and source identities differ.
	rng := rand.New(rand.NewSource(809))
	raster := func() [256]byte {
		var result [256]byte
		for row := 0; row < 4; row++ {
			for column := 0; column < 4; column++ {
				value := byte(20 + rng.Intn(216))
				for y := row * 4; y < (row+1)*4; y++ {
					for x := column * 4; x < (column+1)*4; x++ {
						result[y*16+x] = value
					}
				}
			}
		}
		return result
	}
	opening := make([][256]byte, 160)
	for index := range opening {
		if index%5 == 0 {
			opening[index] = raster()
		} else {
			opening[index] = opening[index-1]
		}
	}
	cohort := introdetect.Cohort{Key: work.ScopeKey, Episodes: []introdetect.Episode{}}
	for index, source := range work.Sources {
		var path string
		if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, source.ItemID).Scan(&path); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		features := AnalysisFeatures{ContentSHA256: hex.EncodeToString(digest[:]), AlgorithmProfile: work.Execution.IntroProfile,
			Audio: []introdetect.AudioSample{}, Visual: []introdetect.VisualSample{}, Refinement: []introdetect.RefinementSample{}}
		start := 30 + index*7
		for frame := 0; frame < 230; frame++ {
			observation := raster()
			position := frame - start
			shared := position >= 0 && position < len(opening)
			if index == 1 || index == 2 {
				shared = shared && position < 130
			} else if index == 3 || index == 4 {
				shared = shared && position >= 30
			}
			if shared {
				observation = opening[position]
			}
			features.Refinement = append(features.Refinement, introdetect.RefinementSample{
				Ticks: int64(frame) * media.TicksPerSecond / 10, Raster: observation})
		}
		cached := analysisHistoricalVisualCache(t, f, work, source, features)
		cohort.Episodes = append(cohort.Episodes, introdetect.Episode{
			EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: cached.ContentSHA256,
			AlgorithmProfile: cached.AlgorithmProfile, DurationTicks: source.DurationTicks,
			Audio: cached.Audio, Visual: cached.Visual, Refinement: cached.Refinement})
	}

	// Establish the actual conflicting complete-clique observations through
	// production discovery, rather than assigning candidate metrics by hand.
	discovered, err := introdetect.DiscoverVisualSequences(f.ctx, cohort, work.Execution.DetectorOptions)
	if err != nil || discovered.SearchLimited {
		t.Fatalf("discover complete cached refinement witnesses: %v", err)
	}
	target := work.Sources[0].SourceRevision
	windows := []introdetect.Interval{}
	for _, group := range discovered.Groups {
		for _, member := range group.Members {
			if member.SourceKey == target {
				windows = append(windows, member.Interval)
			}
		}
	}
	if len(windows) < 2 {
		t.Fatalf("the consensus control did not discover competing witnesses: %+v", discovered)
	}
	common := windows[0]
	crossing := false
	contains := func(outer, inner introdetect.Interval) bool {
		const residual = 3 * media.TicksPerSecond / 10
		return outer.StartTicks <= inner.StartTicks+residual && outer.EndTicks >= inner.EndTicks-residual
	}
	for i, window := range windows {
		common.StartTicks = max(common.StartTicks, window.StartTicks)
		common.EndTicks = min(common.EndTicks, window.EndTicks)
		for _, other := range windows[i+1:] {
			crossing = crossing || (!contains(window, other) && !contains(other, window))
		}
	}
	if !crossing || common.EndTicks-common.StartTicks < 8*media.TicksPerSecond {
		t.Fatalf("the fixture did not require recovery of a safe common interior: windows=%+v common=%+v", windows, common)
	}

	result, err := introdetect.Analyze(f.ctx, cohort, work.Execution.DetectorOptions)
	if err != nil || len(result.Episodes) != 5 {
		t.Fatalf("analyze complete cached crossing witnesses: %+v %v", result, err)
	}
	candidates := make(map[string]introdetect.Candidate, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.Status != introdetect.Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("the safe common opening was not recovered: %+v", episode)
		}
		candidate := episode.Candidates[0]
		if candidate.VisualEvidence == nil || candidate.VisualEvidence.MeasurementPolicy != introdetect.VisualMeasurementCalibrated ||
			len(candidate.VisualEvidence.CalibrationDigest) != 64 ||
			candidate.VisualEvidence.PairCount != len(candidate.Support)*(len(candidate.Support)-1)/2 ||
			!introdetect.ValidateCandidateEvidence(candidate, work.Execution.DetectorOptions) {
			t.Fatalf("recovered publication lost its complete measured refinement evidence: %+v", candidate)
		}
		if episode.SourceKey == target && (candidate.Interval.StartTicks < common.StartTicks || candidate.Interval.EndTicks > common.EndTicks) {
			t.Fatalf("recovered publication escaped the original exact common interior: %+v, common=%+v", candidate, common)
		}
		candidates[episode.SourceKey] = candidate
	}
	seedAnalysisHistoricalVisualResult(t, f, child, fence, work, result)
	for _, source := range work.Sources {
		candidate := candidates[source.SourceRevision]
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		if err != nil || !reflect.DeepEqual(item.Detection.Candidate, &candidate) {
			t.Fatalf("persisted recovered candidate differs from actual matcher evidence: %v", err)
		}
		intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || intro == nil || intro.Provenance != "Detected" ||
			intro.StartTicks != candidate.Interval.StartTicks || intro.EndTicks != candidate.Interval.EndTicks {
			t.Fatalf("recovered refinement interval was not delivered to playback: %+v %v", intro, err)
		}
	}
}
