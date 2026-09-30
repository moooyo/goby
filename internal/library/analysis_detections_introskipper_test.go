package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/media"
)

func analysisIntroSkipperTestWork() (AnalysisWork, introskipper.Result) {
	work := AnalysisWork{TaskKey: TaskIntroAnalysisKey, ScopeKey: "native-scope", Profile: DefaultAnalysisProfile(),
		Execution: analysisAdmissionTestIntroExecution()}
	pair := []introskipper.Support{
		{EpisodeKey: "episode-1", SourceKey: "source-1", ContentIdentity: strings.Repeat("1", 64), AlgorithmProfile: work.Execution.IntroProfile,
			Interval: introskipper.Interval{StartTicks: 0, EndTicks: 40 * media.TicksPerSecond}},
		{EpisodeKey: "episode-2", SourceKey: "source-2", ContentIdentity: strings.Repeat("2", 64), AlgorithmProfile: work.Execution.IntroProfile,
			Interval: introskipper.Interval{StartTicks: 20 * media.TicksPerSecond, EndTicks: 60 * media.TicksPerSecond}},
	}
	result := introskipper.Result{Version: introskipper.Version, CohortKey: work.ScopeKey, Options: work.Profile.IntroSkipper}
	for _, support := range pair {
		work.Sources = append(work.Sources, AnalysisSource{ItemID: support.EpisodeKey, EpisodeKey: support.EpisodeKey, SourceRevision: support.SourceKey,
			DurationTicks: 600 * media.TicksPerSecond, Target: true})
		result.Episodes = append(result.Episodes, introskipper.EpisodeResult{EpisodeKey: support.EpisodeKey, SourceKey: support.SourceKey,
			ContentIdentity: support.ContentIdentity, AlgorithmProfile: support.AlgorithmProfile, DurationTicks: 600 * media.TicksPerSecond,
			Status: introskipper.Qualified, Reasons: []string{}, Candidate: &introskipper.Candidate{Interval: support.Interval,
				UpstreamCommit: introskipper.UpstreamCommit, Support: append([]introskipper.Support{}, pair...)}})
	}
	return work, result
}

func TestStoredIntroSkipperResultKeepsPairFactsWithoutLegacyMetrics(t *testing.T) {
	work, result := analysisIntroSkipperTestWork()
	values, err := validateAnalysisIntroSkipperResultForWork(work, result)
	if err != nil || len(values) != 2 {
		t.Fatalf("valid two-source result rejected: %v", err)
	}
	value := values[work.Sources[0].ItemID]
	raw := analysisDetectionTestJSON(t, value)
	interval := value.Episode.Candidate.Interval
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current != nil || facts.Version != introskipper.Version || len(facts.Episode.Candidates) != 1 ||
		len(facts.Episode.Candidates[0].Support) != 2 || facts.Episode.Candidates[0].GroupID != "" {
		t.Fatalf("native pair facts were lost or projected into legacy metrics: %+v %v", facts, err)
	}
	if ValidateStoredAnalysisResult(raw, introskipper.Qualified, &interval.StartTicks, &interval.EndTicks) != nil {
		t.Fatal("native interval could not be read")
	}
	audit := analysisDetectionTestJSON(t, struct {
		Result   AnalysisStoredIntroSkipperResult
		Decision *AnalysisDecision
	}{Result: value})
	if ValidateStoredAnalysisAudit(audit, introskipper.Qualified) != nil {
		t.Fatal("native audit facts could not be read")
	}
	if bytes.Contains(raw, []byte("Metrics")) || bytes.Contains(raw, []byte("VisualEvidence")) {
		t.Fatal("native evidence fabricated legacy metrics")
	}
	result.Episodes[0].Candidate.Support[0].ContentIdentity = strings.Repeat("f", 64)
	if values[work.Sources[0].ItemID].Episode.Candidate.Support[0].ContentIdentity != strings.Repeat("1", 64) {
		t.Fatal("publication snapshot retained caller-owned support")
	}
}

func TestIntroSkipperPublicationBindsOptionsIdentityOrderAndSourceBounds(t *testing.T) {
	for _, example := range []struct {
		name   string
		mutate func(*AnalysisWork, *introskipper.Result)
	}{
		{"old execution", func(w *AnalysisWork, _ *introskipper.Result) { w.Execution = analysisLegacyTestIntroExecution() }},
		{"unavailable", func(w *AnalysisWork, _ *introskipper.Result) { w.Execution.Available = false }},
		{"wrong task", func(w *AnalysisWork, _ *introskipper.Result) { w.TaskKey = TaskPreviewGenerationKey }},
		{"wrong scope", func(_ *AnalysisWork, r *introskipper.Result) { r.CohortKey += "-other" }},
		{"different options", func(_ *AnalysisWork, r *introskipper.Result) { r.Options.MaximumTimeSkip++ }},
		{"different profile options", func(w *AnalysisWork, _ *introskipper.Result) { w.Profile.IntroSkipper.MaximumTimeSkip++ }},
		{"missing episode", func(_ *AnalysisWork, r *introskipper.Result) { r.Episodes = r.Episodes[:1] }},
		{"duplicate source", func(_ *AnalysisWork, r *introskipper.Result) { r.Episodes[1].SourceKey = r.Episodes[0].SourceKey }},
		{"different extraction", func(_ *AnalysisWork, r *introskipper.Result) { r.Episodes[0].AlgorithmProfile += "-other" }},
		{"different duration", func(w *AnalysisWork, _ *introskipper.Result) { w.Sources[0].DurationTicks-- }},
		{"reversed pair", func(_ *AnalysisWork, r *introskipper.Result) {
			p := r.Episodes[0].Candidate.Support
			p[0], p[1] = p[1], p[0]
		}},
		{"outside peer source", func(w *AnalysisWork, r *introskipper.Result) {
			w.Sources[1].DurationTicks = 55 * media.TicksPerSecond
			r.Episodes[1].DurationTicks = w.Sources[1].DurationTicks
		}},
	} {
		t.Run(example.name, func(t *testing.T) {
			work, result := analysisIntroSkipperTestWork()
			example.mutate(&work, &result)
			if _, err := validateAnalysisIntroSkipperResultForWork(work, result); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("unbound native result accepted: %v", err)
			}
		})
	}
}

func TestIntroSkipperPairIsNotReplacedByPeersLaterCandidate(t *testing.T) {
	work, result := analysisIntroSkipperTestWork()
	result.Episodes[1].Candidate.Interval.EndTicks += media.TicksPerSecond
	result.Episodes[1].Candidate.Support[1].Interval = result.Episodes[1].Candidate.Interval
	if _, err := validateAnalysisIntroSkipperResultForWork(work, result); err != nil {
		t.Fatalf("an accepted pair was incorrectly forced to equal the peer's final candidate: %v", err)
	}
}

func TestStoredIntroSkipperResultRejectsCorruptNativeEvidence(t *testing.T) {
	work, result := analysisIntroSkipperTestWork()
	values, _ := validateAnalysisIntroSkipperResultForWork(work, result)
	value := values[work.Sources[0].ItemID]
	raw := analysisDetectionTestJSON(t, value)
	interval := value.Episode.Candidate.Interval
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte(`"Version":`), []byte(`"version":`), 1),
		bytes.Replace(raw, []byte(`"Options":{`), []byte(`"Options":{"Unknown":1,`), 1),
		bytes.Replace(raw, []byte(`"Reasons":[]`), []byte(`"Reasons":null`), 1),
		bytes.Replace(raw, []byte(`"UpstreamCommit":"`+introskipper.UpstreamCommit+`"`), []byte(`"UpstreamCommit":"`+strings.Repeat("a", 40)+`"`), 1),
		bytes.Replace(raw, []byte(`"MaximumTimeSkip":3.5`), []byte(`"MaximumTimeSkip":3.5,"MaximumTimeSkip":3.5`), 1),
		bytes.Replace(raw, []byte(`"ContentIdentity":"`+strings.Repeat("2", 64)+`"`), []byte(`"ContentIdentity":"`+strings.Repeat("1", 64)+`"`), 1),
		append(raw, []byte(` {}`)...),
	} {
		if bytes.Equal(malformed, raw) || !errors.Is(ValidateStoredAnalysisResult(malformed, introskipper.Qualified, &interval.StartTicks, &interval.EndTicks), ErrInvalidInput) {
			t.Fatal("invalid native wire or pair acquired stored-result validity")
		}
	}
	var legacy map[string]json.RawMessage
	if json.Unmarshal(analysisDetectionTestJSON(t, analysisDetectionTestValue("qualified")), &legacy) != nil {
		t.Fatal("legacy fixture could not be decoded")
	}
	legacy["Version"] = json.RawMessage(`"` + introskipper.Version + `"`)
	if !errors.Is(ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, legacy), introskipper.Qualified, &interval.StartTicks, &interval.EndTicks), ErrInvalidInput) {
		t.Fatal("changing only a legacy version granted native result semantics")
	}
}

func TestStoredIntroSkipperNoResultAndLibraryAbstentionRemainDistinct(t *testing.T) {
	work, result := analysisIntroSkipperTestWork()
	episode := result.Episodes[0]
	episode.Candidate, episode.Status, episode.Reasons = nil, introskipper.NoResult, []string{introskipper.NoRepeatedInterval}
	value := AnalysisStoredIntroSkipperResult{Version: introskipper.Version, Options: work.Profile.IntroSkipper, Episode: episode}
	if ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), introskipper.NoResult, nil, nil) != nil {
		t.Fatal("completed native no-result was rejected")
	}
	value.Reason = "source_unavailable"
	if validateAnalysisStoredIntroSkipperResult(value) == nil {
		t.Fatal("failed extraction retained completed matcher evidence")
	}
	value.Episode.ContentIdentity, value.Episode.AlgorithmProfile, value.Episode.Reasons = "", "", []string{}
	if ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), introskipper.NoResult, nil, nil) != nil {
		t.Fatal("library abstention required an invented fingerprint identity")
	}
}
