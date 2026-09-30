package library

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisVisualDetectionTestValue() AnalysisStoredResult {
	value := analysisDetectionTestValue(introdetect.Qualified)
	candidate := &value.Episode.Candidates[0]
	candidate.Metrics = introdetect.Metrics{}
	candidate.VisualEvidence = &introdetect.VisualSequenceMetrics{
		MeasurementPolicy: "coarse-v1",
		Samples:           16, CoveragePermille: 850, Transitions: 3, DistinctStates: 4,
		DominantStatePermille: 650, MaxLumaRMSPermille: 550, MaxCenterRMSPermille: 650,
		MaxGapTicks: 21 * media.TicksPerSecond / 10, PairCount: 3,
	}
	for index := range candidate.Support {
		candidate.Support[index].Interval.EndTicks = candidate.Support[index].Interval.StartTicks + 8*media.TicksPerSecond
	}
	candidate.Interval = candidate.Support[0].Interval
	return value
}

func TestStoredAnalysisVisualEvidenceRoundTripsWithoutAudioClaims(t *testing.T) {
	value := analysisVisualDetectionTestValue()
	raw := analysisDetectionTestJSON(t, value)
	start, end := analysisDetectionTestInterval(value)
	if err := ValidateStoredAnalysisResult(raw, "qualified", start, end); err != nil {
		t.Fatalf("qualified eight-second visual evidence rejected: %v", err)
	}
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current == nil || !reflect.DeepEqual(*current, value) || facts.Version != introdetect.Version {
		t.Fatalf("visual evidence was lost or reinterpreted: %+v %v", current, err)
	}
	if current.Episode.Candidates[0].Metrics != (introdetect.Metrics{}) {
		t.Fatal("visual result fabricated joint audio metrics")
	}
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value}), "qualified"); err != nil {
		t.Fatalf("visual audit rejected: %v", err)
	}
	joint := analysisDetectionTestValue(introdetect.Qualified)
	jointRaw := analysisDetectionTestJSON(t, joint)
	if bytes.Contains(jointRaw, []byte(`"VisualEvidence"`)) {
		t.Fatal("joint evidence acquired an optional visual branch")
	}
	jointStart, jointEnd := analysisDetectionTestInterval(joint)
	if err := ValidateStoredAnalysisResult(jointRaw, "qualified", jointStart, jointEnd); err != nil {
		t.Fatalf("omitted optional branch invalidated joint evidence: %v", err)
	}
}

func TestStoredAnalysisVisualEvidenceRejectsWeakOrIncompleteWitnesses(t *testing.T) {
	for _, example := range []struct {
		name   string
		mutate func(*introdetect.Candidate)
	}{
		{"missing visual evidence", func(c *introdetect.Candidate) { c.VisualEvidence = nil }},
		{"fabricated audio", func(c *introdetect.Candidate) { c.Metrics.AudioDistinct = 12; c.Metrics.AudioSamples = 12 }},
		{"legacy visual metrics", func(c *introdetect.Candidate) { c.Metrics.VisualSamples = 16 }},
		{"incomplete clique", func(c *introdetect.Candidate) { c.VisualEvidence.PairCount = 4 }},
		{"two sources", func(c *introdetect.Candidate) { c.Support = c.Support[:2] }},
		{"duplicate source", func(c *introdetect.Candidate) { c.Support[1].SourceKey = c.Support[0].SourceKey }},
		{"duplicate episode", func(c *introdetect.Candidate) { c.Support[1].EpisodeKey = c.Support[0].EpisodeKey }},
		{"duplicate content", func(c *introdetect.Candidate) { c.Support[1].ContentIdentity = c.Support[0].ContentIdentity }},
		{"different support span", func(c *introdetect.Candidate) { c.Support[1].Interval.EndTicks++ }},
		{"support beyond prefix", func(c *introdetect.Candidate) {
			c.Support[1].Interval = introdetect.Interval{StartTicks: 113 * media.TicksPerSecond, EndTicks: 121 * media.TicksPerSecond}
		}},
		{"too short", func(c *introdetect.Candidate) { c.Interval.EndTicks--; c.Support[0].Interval = c.Interval }},
		{"too long", func(c *introdetect.Candidate) {
			c.Interval.EndTicks = c.Interval.StartTicks + 90*media.TicksPerSecond + 1
			c.Support[0].Interval = c.Interval
		}},
		{"missing samples", func(c *introdetect.Candidate) { c.VisualEvidence.Samples = 15 }},
		{"low coverage", func(c *introdetect.Candidate) { c.VisualEvidence.CoveragePermille = 849 }},
		{"few transitions", func(c *introdetect.Candidate) { c.VisualEvidence.Transitions = 2 }},
		{"few states", func(c *introdetect.Candidate) { c.VisualEvidence.DistinctStates = 3 }},
		{"dominant state", func(c *introdetect.Candidate) { c.VisualEvidence.DominantStatePermille = 651 }},
		{"luma mismatch", func(c *introdetect.Candidate) { c.VisualEvidence.MaxLumaRMSPermille = 551 }},
		{"center mismatch", func(c *introdetect.Candidate) { c.VisualEvidence.MaxCenterRMSPermille = 651 }},
		{"long gap", func(c *introdetect.Candidate) { c.VisualEvidence.MaxGapTicks++ }},
		{"negative metric", func(c *introdetect.Candidate) { c.VisualEvidence.MaxLumaRMSPermille = -1 }},
		{"review observation", func(c *introdetect.Candidate) { c.Status = introdetect.Review }},
	} {
		t.Run(example.name, func(t *testing.T) {
			value := analysisVisualDetectionTestValue()
			example.mutate(&value.Episode.Candidates[0])
			start, end := analysisDetectionTestInterval(value)
			if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid visual evidence admitted: %v", err)
			}
		})
	}
}

func TestStoredAnalysisVisualEvidenceKeepsClosedJSONShape(t *testing.T) {
	value := analysisVisualDetectionTestValue()
	raw := analysisDetectionTestJSON(t, value)
	start, end := analysisDetectionTestInterval(value)
	for _, changed := range [][]byte{
		bytes.Replace(raw, []byte(`"VisualEvidence":`), []byte(`"visualEvidence":`), 1),
		bytes.Replace(raw, []byte(`"VisualEvidence":`), []byte(`"UnknownEvidence":`), 1),
		bytes.Replace(raw, []byte(`"VisualEvidence":`), []byte(`"VisualEvidence":null,"VisualEvidence":`), 1),
		bytes.Replace(raw, []byte(`"Samples":16,`), nil, 1),
		bytes.Replace(raw, []byte(`"Samples":16`), []byte(`"samples":16`), 1),
		bytes.Replace(raw, []byte(`"Samples":16`), []byte(`"Samples":16,"Samples":16`), 1),
		bytes.Replace(raw, []byte(`"Samples":16`), []byte(`"Samples":16,"Future":0`), 1),
	} {
		if bytes.Equal(raw, changed) || !errors.Is(ValidateStoredAnalysisResult(changed, "qualified", start, end), ErrInvalidInput) {
			t.Fatal("unknown, missing, duplicate or case-varied visual evidence accepted")
		}
	}
}

func analysisVisualDetectionTestWork() (AnalysisWork, introdetect.Result) {
	value := analysisVisualDetectionTestValue()
	candidate := value.Episode.Candidates[0]
	execution := analysisLegacyTestIntroExecution()
	work := AnalysisWork{TaskKey: TaskIntroAnalysisKey, ScopeKey: "visual-scope", Execution: execution}
	groupMetrics := *candidate.VisualEvidence
	result := introdetect.Result{Version: introdetect.Version, CohortKey: work.ScopeKey, Options: execution.DetectorOptions,
		Groups: []introdetect.Group{{ID: candidate.GroupID, AlgorithmProfile: execution.IntroProfile,
			Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Members: candidate.Support, VisualEvidence: &groupMetrics}}}
	for _, support := range candidate.Support {
		work.Sources = append(work.Sources, AnalysisSource{ItemID: support.EpisodeKey, SourceRevision: support.SourceKey,
			EpisodeKey: support.EpisodeKey, DurationTicks: 600 * media.TicksPerSecond, Target: true})
		episode := normalizeAnalysisEpisode(value.Episode)
		episode.EpisodeKey, episode.SourceKey, episode.ContentIdentity = support.EpisodeKey, support.SourceKey, support.ContentIdentity
		episode.Candidates[0].Interval = support.Interval
		result.Episodes = append(result.Episodes, episode)
	}
	return work, result
}

func TestAnalysisVisualPublicationBindsAdmissionGroupAndSourceWindow(t *testing.T) {
	work, result := analysisVisualDetectionTestWork()
	values, err := validateAnalysisResultForWork(work, result)
	if err != nil || len(values) != 3 {
		t.Fatalf("complete visual publication rejected: %v", err)
	}
	for _, mutate := range []func(*AnalysisWork, *introdetect.Result){
		func(w *AnalysisWork, r *introdetect.Result) { w.Execution.Version = 3 },
		func(w *AnalysisWork, r *introdetect.Result) { w.Execution.DetectorVersion = "introdetect-v3" },
		func(w *AnalysisWork, r *introdetect.Result) { w.Execution.Available = false },
		func(w *AnalysisWork, r *introdetect.Result) { w.TaskKey = TaskPreviewGenerationKey },
		func(w *AnalysisWork, r *introdetect.Result) { r.CohortKey = "other-scope" },
		func(w *AnalysisWork, r *introdetect.Result) { r.Groups[0].VisualEvidence = nil },
		func(w *AnalysisWork, r *introdetect.Result) { r.Groups[0].VisualEvidence.Samples++ },
		func(w *AnalysisWork, r *introdetect.Result) { r.Groups[0].AlgorithmProfile = "other-extraction" },
		func(w *AnalysisWork, r *introdetect.Result) { w.Sources[2].DurationTicks = 50 * media.TicksPerSecond },
	} {
		work, result := analysisVisualDetectionTestWork()
		mutate(&work, &result)
		if _, err := validateAnalysisResultForWork(work, result); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("detached visual publication admitted: %v", err)
		}
	}
	work, result = analysisVisualDetectionTestWork()
	values, err = validateAnalysisResultForWork(work, result)
	if err != nil {
		t.Fatal(err)
	}
	result.Episodes[0].Candidates[0].VisualEvidence.Samples++
	if values[work.Sources[0].ItemID].Episode.Candidates[0].VisualEvidence.Samples != 16 {
		t.Fatal("publication snapshot retained mutable caller evidence")
	}
}
