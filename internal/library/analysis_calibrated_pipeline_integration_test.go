//go:build linux

package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisCalibratedPipelineRaster(random *rand.Rand) [256]byte {
	var raster [256]byte
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			value := byte(20 + random.Intn(216))
			for y := row * 4; y < (row+1)*4; y++ {
				for x := column * 4; x < (column+1)*4; x++ {
					raster[y*16+x] = value
				}
			}
		}
	}
	return raster
}

func TestAnalysisHistoricalCalibratedMatcherEvidenceSurvivesCacheAndRestore(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	f.setIntroDetection(t, true)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	if len(children) != 1 {
		t.Fatalf("three independent episodes need one admitted cohort, got %d", len(children))
	}
	child := children[0]
	f.claim(t, run, child)
	fence := f.fence(child)
	work, err := f.store.GetAnalysisWork(f.ctx, child, fence)
	if err != nil || len(work.Sources) != 3 {
		t.Fatalf("read admitted refinement sources: %v", err)
	}
	work = analysisHistoricalVisualWork(t, work)

	// These 16x16 observations repeat a nonperiodic ten-second action sequence
	// at distinct 100 ms source-clock offsets. Surrounding rasters are independent.
	// Audio and coarse descriptors stay empty so only measured refinement can
	// produce a qualified candidate; the test never assigns candidate metrics.
	const step = media.TicksPerSecond / 10
	random := rand.New(rand.NewSource(809))
	opening := make([][256]byte, 100)
	for index := range opening {
		if index%5 == 0 {
			opening[index] = analysisCalibratedPipelineRaster(random)
		} else {
			opening[index] = opening[index-1]
		}
	}
	cohort := introdetect.Cohort{Key: work.ScopeKey, Episodes: []introdetect.Episode{}}
	bounds := make(map[string]introdetect.Interval, len(work.Sources))
	for index, source := range work.Sources {
		if source.DurationTicks/2 <= 20*media.TicksPerSecond {
			t.Fatal("the source duration does not admit the complete refinement window")
		}
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
		for frame := 0; frame < 200; frame++ {
			raster := analysisCalibratedPipelineRaster(random)
			if frame >= start && frame < start+len(opening) {
				raster = opening[frame-start]
			}
			features.Refinement = append(features.Refinement, introdetect.RefinementSample{Ticks: int64(frame) * step, Raster: raster})
		}
		bounds[source.SourceRevision] = introdetect.Interval{StartTicks: int64(start) * step, EndTicks: int64(start+len(opening)-1) * step}
		cached := analysisHistoricalVisualCache(t, f, work, source, features)
		cohort.Episodes = append(cohort.Episodes, introdetect.Episode{
			EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: cached.ContentSHA256,
			AlgorithmProfile: cached.AlgorithmProfile, DurationTicks: source.DurationTicks,
			AudioBoundaryUncertaintyTicks: cached.AudioBoundaryUncertaintyTicks, Audio: cached.Audio, Visual: cached.Visual, Refinement: cached.Refinement})
	}

	observed, err := introdetect.DiscoverVisualSequences(f.ctx, cohort, work.Execution.DetectorOptions)
	if err != nil || observed.SearchLimited || len(observed.Groups) != 1 || observed.Groups[0].Calibration == nil {
		t.Fatalf("the cached rasters lack a complete calibrated witness: groups=%d error=%v", len(observed.Groups), err)
	}
	audit, err := json.Marshal(observed.Groups[0].Calibration)
	if err != nil {
		t.Fatal(err)
	}
	auditDigest := sha256.Sum256(audit)
	expectedDigest := hex.EncodeToString(auditDigest[:])
	result, err := introdetect.Analyze(f.ctx, cohort, work.Execution.DetectorOptions)
	if err != nil || len(result.Groups) != 1 || len(result.Episodes) != 3 {
		t.Fatalf("match the complete cached refinement cohort: groups=%d episodes=%d error=%v", len(result.Groups), len(result.Episodes), err)
	}
	group := result.Groups[0]
	if group.Status != introdetect.Qualified || len(group.Reasons) != 0 || group.Metrics != (introdetect.Metrics{}) ||
		group.VisualEvidence == nil || group.VisualEvidence.MeasurementPolicy != "calibrated-r16-v1" ||
		group.VisualEvidence.CalibrationDigest != expectedDigest || group.VisualEvidence.PairCount != 3 ||
		!reflect.DeepEqual(*group.VisualEvidence, observed.Groups[0].Metrics) || !reflect.DeepEqual(group.Members, observed.Groups[0].Members) {
		t.Fatalf("publication lost the actual calibrated witness or acquired acoustic evidence: %+v", group)
	}
	candidates := make(map[string]introdetect.Candidate, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.Status != introdetect.Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("cached refinement did not qualify independently: %+v", episode)
		}
		candidate := episode.Candidates[0]
		openingBounds, exists := bounds[episode.SourceKey]
		if !exists || candidate.Interval.StartTicks < openingBounds.StartTicks || candidate.Interval.EndTicks > openingBounds.EndTicks ||
			candidate.Interval.EndTicks-candidate.Interval.StartTicks < 8*media.TicksPerSecond ||
			candidate.GroupID != group.ID || candidate.Metrics != (introdetect.Metrics{}) ||
			!reflect.DeepEqual(candidate.VisualEvidence, group.VisualEvidence) || !reflect.DeepEqual(candidate.Support, group.Members) {
			t.Fatalf("calibrated candidate escaped its safe opening or lost group evidence: %+v", candidate)
		}
		candidates[episode.SourceKey] = candidate
	}
	seedAnalysisHistoricalVisualResult(t, f, child, fence, work, result)
	for _, source := range work.Sources {
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		candidate := candidates[source.SourceRevision]
		if err != nil || !reflect.DeepEqual(item.Detection.Candidate, &candidate) {
			t.Fatalf("stored calibrated candidate differs from actual matcher evidence: %v", err)
		}
		intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || intro == nil || intro.Provenance != "Detected" ||
			intro.StartTicks != candidate.Interval.StartTicks || intro.EndTicks != candidate.Interval.EndTicks {
			t.Fatalf("calibrated cache-to-publication output was not delivered to playback: %+v %v", intro, err)
		}
	}
}
