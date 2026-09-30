//go:build linux

package library

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/rand"
	"os"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisVisualMatcherOutputSurvivesFeatureCacheAndPublication(t *testing.T) {
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
		t.Fatalf("read the admitted independent sources: %v", err)
	}

	// A nonperiodic 16-second visual sequence repeats at distinct source offsets.
	// Each episode's surrounding frames and audio remain independent. These are
	// synthetic extracted features, not manually assigned candidate metrics.
	visualRandom := rand.New(rand.NewSource(104))
	frame := func() introdetect.VisualSample {
		sample := introdetect.VisualSample{Hash: visualRandom.Uint64(), Contrast: 200, LumaKnown: true}
		for index := range sample.Luma {
			sample.Luma[index] = int8(visualRandom.Intn(111) - 55)
		}
		return sample
	}
	sequence := make([]introdetect.VisualSample, 33)
	for index := range sequence {
		sequence[index] = frame()
	}
	cohort := introdetect.Cohort{Key: work.ScopeKey, Episodes: []introdetect.Episode{}}
	expected := make(map[string]introdetect.Interval, len(work.Sources))
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
		features := AnalysisFeatures{ContentSHA256: hex.EncodeToString(digest[:]),
			AlgorithmProfile: work.Execution.IntroProfile, Audio: []introdetect.AudioSample{}, Visual: []introdetect.VisualSample{}}
		audioRandom := rand.New(rand.NewSource(int64(500 + index)))
		for sample := 0; sample < 480; sample++ {
			features.Audio = append(features.Audio, introdetect.AudioSample{
				StartTicks: int64(sample) * media.TicksPerSecond / 4,
				EndTicks:   int64(sample+1) * media.TicksPerSecond / 4, Fingerprint: audioRandom.Uint32()})
		}
		first := 10 + index*4
		for sample := 0; sample < 240; sample++ {
			visual := frame()
			if sample >= first && sample < first+len(sequence) {
				visual = sequence[sample-first]
			}
			visual.Ticks = int64(sample) * media.TicksPerSecond / 2
			features.Visual = append(features.Visual, visual)
		}
		expected[source.SourceRevision] = introdetect.Interval{
			StartTicks: int64(5+index*2) * media.TicksPerSecond,
			EndTicks:   int64(21+index*2) * media.TicksPerSecond}
		if err := f.store.PutAnalysisFeatures(f.ctx, child, source.ItemID, fence, features); err != nil {
			t.Fatalf("store source-bound extracted features: %v", err)
		}
		var payload []byte
		if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM analysis_feature_cache
			WHERE item_id=$1 AND profile_fingerprint=$2`, source.ItemID, work.ConfigurationFingerprint).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) < 6 || string(payload[:4]) != "GAFB" || binary.LittleEndian.Uint16(payload[4:6]) != 2 {
			t.Fatal("the production feature cache did not persist GAFB v2")
		}
		cached, found, err := f.store.GetAnalysisFeatures(f.ctx, child, source.ItemID, fence)
		if err != nil || !found || !reflect.DeepEqual(cached, features) {
			t.Fatalf("the durable cache changed measured audio or luminance evidence: found=%v error=%v", found, err)
		}
		cohort.Episodes = append(cohort.Episodes, introdetect.Episode{
			EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: cached.ContentSHA256,
			AlgorithmProfile: cached.AlgorithmProfile, DurationTicks: source.DurationTicks,
			AudioBoundaryUncertaintyTicks: cached.AudioBoundaryUncertaintyTicks, Audio: cached.Audio, Visual: cached.Visual})
	}

	result, err := introdetect.Analyze(f.ctx, cohort, work.Execution.DetectorOptions)
	if err != nil || len(result.Groups) != 1 || len(result.Episodes) != 3 {
		t.Fatalf("match the complete cached cohort: groups=%d episodes=%d error=%v", len(result.Groups), len(result.Episodes), err)
	}
	candidates := make(map[string]introdetect.Candidate, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.Status != introdetect.Qualified || len(episode.Candidates) != 1 {
			t.Fatalf("the matcher did not qualify the repeated short visual sequence: %+v", episode)
		}
		candidate := episode.Candidates[0]
		span := candidate.Interval.EndTicks - candidate.Interval.StartTicks
		if candidate.VisualEvidence == nil || candidate.VisualEvidence.PairCount != 3 || candidate.Metrics != (introdetect.Metrics{}) ||
			span < 8*media.TicksPerSecond || span >= 30*media.TicksPerSecond || candidate.Interval != expected[episode.SourceKey] {
			t.Fatalf("the matcher lost its independent short visual evidence: %+v", candidate)
		}
		candidates[episode.SourceKey] = candidate
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, child, fence, result); err != nil {
		t.Fatalf("publish actual matcher output after a durable feature-cache round trip: %v", err)
	}
	for _, source := range work.Sources {
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		candidate := candidates[source.SourceRevision]
		if err != nil || !reflect.DeepEqual(item.Detection.Candidate, &candidate) {
			t.Fatalf("published candidate differs from actual matcher evidence: %v", err)
		}
		intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || intro == nil || intro.Provenance != "Detected" ||
			intro.StartTicks != candidate.Interval.StartTicks || intro.EndTicks != candidate.Interval.EndTicks {
			t.Fatalf("actual visual matcher output was not delivered to playback: %+v %v", intro, err)
		}
	}
}
