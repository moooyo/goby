package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/media"
)

type storedCreditsProbe struct{ black bool }

func (p storedCreditsProbe) ScanKeyframes(_ context.Context, window creditsskipper.Range, _ int) (creditsskipper.KeyframeEvidence, error) {
	result := creditsskipper.KeyframeEvidence{}
	if p.black {
		for second := 0; float64(second) < window.Duration(); second++ {
			percentage := 10
			if float64(second) >= window.Duration()-30 {
				percentage = 95
			}
			result.BlackFrames = append(result.BlackFrames, creditsskipper.BlackFrame{Frame: second, Time: float64(second), Percentage: percentage})
		}
	}
	return result, nil
}
func (storedCreditsProbe) ScanBlackIntervals(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (storedCreditsProbe) ScanBoundary(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.BlackFrame, error) {
	return nil, nil
}
func (storedCreditsProbe) ScanSilence(context.Context, creditsskipper.Range) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (storedCreditsProbe) ScanKeyframesAtBoundary(context.Context, creditsskipper.Range) ([]float64, error) {
	return nil, nil
}

func storedCreditsExecution() AnalysisExecutionProfile {
	value := analysisAdmissionTestIntroExecution()
	value.DetectorVersion = introskipper.CreditsVersion
	value.FFprobeSHA256 = strings.Repeat("b2", 32)
	value.IntroProfile = "credits-extraction-fixture-v1"
	return value
}

func storedCreditsValue(t *testing.T, source AnalysisSource, audio *introskipper.EpisodeResult, twoSegments bool) AnalysisStoredCreditsResult {
	t.Helper()
	duration := float64(source.DurationTicks) / float64(media.TicksPerSecond)
	request := creditsskipper.Request{DurationSeconds: duration, IsMovie: source.ItemType == "Movie"}
	if twoSegments {
		request.Chapters = []creditsskipper.Chapter{{Name: "Main", StartSeconds: 0}, {Name: "Credits", StartSeconds: duration - 100}, {Name: "Content", StartSeconds: duration - 60}}
	} else if audio == nil {
		request.Chapters = []creditsskipper.Chapter{{Name: "Main", StartSeconds: 0}, {Name: "Credits", StartSeconds: duration - 30}}
	}
	if audio != nil {
		request.AudioSegments = []creditsskipper.Segment{{Start: float64(audio.Candidate.Interval.StartTicks) / float64(media.TicksPerSecond), End: float64(audio.Candidate.Interval.EndTicks) / float64(media.TicksPerSecond), Source: creditsskipper.ChromaprintSource}}
	}
	visual, err := creditsskipper.Detect(context.Background(), request, storedCreditsProbe{black: twoSegments})
	if err != nil {
		t.Fatal(err)
	}
	value := AnalysisStoredCreditsResult{Version: AnalysisCreditsResultVersion, ItemID: source.ItemID, ItemType: source.ItemType, SourceRevision: source.SourceRevision, DurationTicks: source.DurationTicks, Segments: []CreditsInterval{}, Audio: audio, Visual: &visual}
	for _, segment := range visual.Segments {
		start, ok := creditsTicks(segment.Start)
		if !ok {
			t.Fatal("invalid fixture start")
		}
		end, ok := creditsTicks(segment.End)
		if !ok {
			t.Fatal("invalid fixture end")
		}
		value.Segments = append(value.Segments, CreditsInterval{start, end, string(segment.Source)})
	}
	return value
}

func TestStoredCreditsRetainsMultipleSegmentsWithoutMarkingTheGap(t *testing.T) {
	source := AnalysisSource{ItemID: "episode-one", ItemType: "Episode", SourceRevision: "source-one", DurationTicks: 600 * media.TicksPerSecond}
	value := storedCreditsValue(t, source, nil, true)
	if len(value.Segments) != 2 || value.Segments[0].EndTicks >= value.Segments[1].StartTicks {
		t.Fatalf("fixture did not retain the content gap: %+v", value.Segments)
	}
	raw, _ := json.Marshal(value)
	first := value.Segments[0]
	got, err := ReadStoredCreditsAnalysisResult(raw, "qualified", &first.StartTicks, &first.EndTicks)
	if err != nil || !reflect.DeepEqual(got, value) {
		t.Fatalf("multi-segment facts changed: %+v %v", got, err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"Version":`), []byte(`"version":`), 1),
		bytes.Replace(raw, []byte(`"ItemType":"Episode"`), []byte(`"ItemType":"Video"`), 1),
		bytes.Replace(raw, []byte(`"Reason":""`), []byte(`"Reason":"","Reason":""`), 1),
		bytes.Replace(raw, []byte(`"Source":"Chapter"`), []byte(`"Source":"Guess"`), 1),
		append(append([]byte{}, raw...), []byte(` {}`)...),
	} {
		if bytes.Equal(raw, invalid) || !errors.Is(ValidateStoredCreditsAnalysisResult(invalid, "qualified", &first.StartTicks, &first.EndTicks), ErrInvalidInput) {
			t.Fatal("malformed evidence was accepted")
		}
	}
	lastEnd := value.Segments[len(value.Segments)-1].EndTicks
	if ValidateStoredCreditsAnalysisResult(raw, "qualified", &first.StartTicks, &lastEnd) == nil {
		t.Fatal("index column flattened the content gap")
	}
}

func TestStoredCreditsAudioRequiresIndependentFullSourcePair(t *testing.T) {
	for _, duration := range []int64{120 * media.TicksPerSecond, 600 * media.TicksPerSecond} {
		work := AnalysisWork{TaskKey: TaskCreditsAnalysisKey, Profile: DefaultAnalysisProfile(), Execution: storedCreditsExecution()}
		pair := []introskipper.Support{}
		hashes := map[string]string{}
		for index, name := range []string{"episode-one", "episode-two"} {
			source := AnalysisSource{ItemID: name, ItemType: "Episode", EpisodeKey: name, SourceRevision: "source-" + name, DurationTicks: duration, Target: true}
			work.Sources = append(work.Sources, source)
			hashes[name] = strings.Repeat(string(rune('a'+index)), 64)
			pair = append(pair, introskipper.Support{EpisodeKey: name, SourceKey: source.SourceRevision, ContentIdentity: hashes[name], AlgorithmProfile: work.Execution.IntroProfile, Interval: introskipper.Interval{StartTicks: duration - 40*media.TicksPerSecond, EndTicks: duration - 5*media.TicksPerSecond}})
		}
		values := map[string]AnalysisStoredCreditsResult{}
		for index, source := range work.Sources {
			audio := introskipper.EpisodeResult{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hashes[source.ItemID], AlgorithmProfile: work.Execution.IntroProfile, DurationTicks: duration, Status: introskipper.Qualified, Reasons: []string{}, Candidate: &introskipper.Candidate{Interval: pair[index].Interval, UpstreamCommit: introskipper.UpstreamCommit, Support: append([]introskipper.Support{}, pair...)}}
			values[source.ItemID] = storedCreditsValue(t, source, &audio, false)
		}
		prepared, err := prepareCreditsAnalysisResults(work, values, hashes)
		if err != nil {
			t.Fatalf("valid pair rejected at duration %d: %v", duration, err)
		}
		first := work.Sources[0].ItemID
		if len(prepared[first].AudioSources) != 2 {
			t.Fatal("winning pair was lost")
		}
		values[first].Audio.Candidate.Support[1].ContentIdentity = strings.Repeat("f", 64)
		if prepared[first].Audio.Candidate.Support[1].ContentIdentity != hashes[work.Sources[1].ItemID] {
			t.Fatal("stored snapshot retained mutable caller slices")
		}
		if _, err := prepareCreditsAnalysisResults(work, values, hashes); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("unsupported full-source hash accepted", err)
		}
	}
}

func TestStoredCreditsAbstentionNeverInventsAudioOrQualification(t *testing.T) {
	value := AnalysisStoredCreditsResult{Version: AnalysisCreditsResultVersion, ItemID: "movie", ItemType: "Movie", SourceRevision: "source", DurationTicks: 600 * media.TicksPerSecond, Segments: []CreditsInterval{}, Reason: "source_unavailable"}
	raw, _ := json.Marshal(value)
	if ValidateStoredCreditsAnalysisResult(raw, "no_result", nil, nil) != nil {
		t.Fatal("valid unavailable outcome rejected")
	}
	value.Reason = "no_credits_detected"
	raw, _ = json.Marshal(value)
	if ValidateStoredCreditsAnalysisResult(raw, "no_result", nil, nil) == nil {
		t.Fatal("completed outcome omitted actual pass evidence")
	}
}

func storedCreditsNativeMatcherResult(t *testing.T, work AnalysisWork, hashes map[string]string) introskipper.Result {
	t.Helper()
	cohort := introskipper.Cohort{Key: "native-credits-publication", Episodes: []introskipper.Episode{}}
	for _, source := range work.Sources {
		fingerprint := make([]uint32, 200)
		for index := range fingerprint {
			fingerprint[index] = 17
		}
		cohort.Episodes = append(cohort.Episodes, introskipper.Episode{
			EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hashes[source.ItemID],
			AlgorithmProfile: work.Execution.IntroProfile, DurationTicks: source.DurationTicks, Fingerprint: fingerprint,
		})
	}
	result, err := introskipper.AnalyzeCredits(context.Background(), cohort, work.Execution.IntroSkipperOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, episode := range result.Episodes {
		if episode.Status != introskipper.Qualified || episode.Candidate == nil || episode.Reasons != nil {
			t.Fatalf("fixture must exercise the native qualified nil-reasons result: %+v", episode)
		}
	}
	return result
}

func TestStoredCreditsNativeMatcherPublishesCanonicalReasons(t *testing.T) {
	work := AnalysisWork{TaskKey: TaskCreditsAnalysisKey, Profile: DefaultAnalysisProfile(), Execution: storedCreditsExecution()}
	hashes := map[string]string{}
	for index, name := range []string{"episode-one", "episode-two"} {
		work.Sources = append(work.Sources, AnalysisSource{ItemID: name, ItemType: "Episode", EpisodeKey: name,
			SourceRevision: "source-" + name, DurationTicks: 600 * media.TicksPerSecond, Target: true})
		hashes[name] = strings.Repeat(string(rune('a'+index)), 64)
	}
	native := storedCreditsNativeMatcherResult(t, work, hashes)
	values := map[string]AnalysisStoredCreditsResult{}
	for index, source := range work.Sources {
		episode := native.Episodes[index]
		values[source.ItemID] = storedCreditsValue(t, source, &episode, false)
	}
	prepared, err := prepareCreditsAnalysisResults(work, values, hashes)
	if err != nil {
		t.Fatalf("native matcher output failed publication preparation: %v", err)
	}
	for _, source := range work.Sources {
		value := prepared[source.ItemID]
		if value.Audio == nil || value.Audio.Reasons == nil || len(value.Audio.Reasons) != 0 || values[source.ItemID].Audio.Reasons != nil {
			t.Fatal("publication failed to canonicalize its snapshot or mutated native evidence")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		first := value.Segments[0]
		if err := ValidateStoredCreditsAnalysisResult(raw, "qualified", &first.StartTicks, &first.EndTicks); err != nil {
			t.Fatal("canonical matcher evidence failed the strict stored codec", err)
		}
		malformed := bytes.Replace(raw, []byte(`"Reasons":[]`), []byte(`"Reasons":null`), 1)
		if bytes.Equal(raw, malformed) || ValidateStoredCreditsAnalysisResult(malformed, "qualified", &first.StartTicks, &first.EndTicks) == nil {
			t.Fatal("normalization weakened the strict persisted result contract")
		}
	}
}

func TestCreditsProjectionPreservesExplicitPrecedenceAndStaleEvidence(t *testing.T) {
	detection := creditsDetectionRecord{Revision: "7", Status: "qualified", Active: true, Result: AnalysisStoredCreditsResult{Segments: []CreditsInterval{{StartTicks: 90, EndTicks: 100, Source: "BlackFrame"}, {StartTicks: 110, EndTicks: 120, Source: "Chapter"}}}}
	for _, provenance := range []string{"Manual", "Import", "Chapter"} {
		detail := CreditsDetail{Automatic: &CreditsPoint{80, "Chapter"}, Effective: &CreditsPoint{70, provenance}}
		applyCreditsDetection(&detail, detection)
		if detail.Effective.Provenance != provenance || detail.Automatic.StartTicks != 80 || len(detail.Detected) != 2 {
			t.Fatalf("explicit marker was replaced: %+v", detail)
		}
	}
	detail := CreditsDetail{}
	applyCreditsDetection(&detail, detection)
	if detail.Effective == nil || detail.Effective.StartTicks != 90 || detail.Effective.Provenance != "Detected" {
		t.Fatalf("valid detection missing: %+v", detail)
	}
	detection.Stale = true
	detail = CreditsDetail{}
	applyCreditsDetection(&detail, detection)
	if detail.Effective != nil || !detail.DetectedStale || len(detail.Detected) != 2 {
		t.Fatal("stale evidence became effective or disappeared")
	}
}
