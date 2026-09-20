package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisDetectionTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func analysisDetectionTestValue(status introdetect.Status) AnalysisStoredResult {
	options := introdetect.DefaultOptions()
	value := AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{
		EpisodeKey: "episode-1", SourceKey: "source-1", ContentIdentity: strings.Repeat("1", 64), Status: status,
		Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{}}}
	if status == introdetect.NoResult {
		value.Episode.Reasons = []introdetect.Reason{introdetect.NoRepeatedInterval}
		return value
	}
	span := options.AutoMinDurationTicks + 10*media.TicksPerSecond
	reasons := []introdetect.Reason{}
	if status == introdetect.Review {
		span = options.MinDurationTicks
		reasons = []introdetect.Reason{introdetect.ShortInterval}
		value.Episode.Reasons = []introdetect.Reason{introdetect.ShortInterval}
	}
	candidate := introdetect.Candidate{GroupID: "intro-group-v1-" + strings.Repeat("a", 64), Status: status, Reasons: reasons,
		Metrics: introdetect.Metrics{AudioAgreementPermille: 1000, AudioInformativePermille: 1000, AudioSimilarityPermille: 1000,
			AudioSamples: 120, AudioDistinct: 120, VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000,
			VisualCoveragePermille: 1000, VisualSamples: 40, VisualTransitions: 39, VisualChangeCoveragePermille: 1000,
			VisualDominancePermille: 25, BoundaryUncertaintyTicks: media.TicksPerSecond,
			PairCount: options.MinSupport * (options.MinSupport - 1) / 2}, Support: []introdetect.Support{}}
	for index := range options.MinSupport {
		start := int64(10+5*index) * media.TicksPerSecond
		candidate.Support = append(candidate.Support, introdetect.Support{EpisodeKey: fmt.Sprintf("episode-%d", index+1),
			SourceKey: fmt.Sprintf("source-%d", index+1), ContentIdentity: strings.Repeat(fmt.Sprintf("%x", index+1), 64),
			Interval: introdetect.Interval{StartTicks: start, EndTicks: start + span}})
	}
	candidate.Interval = candidate.Support[0].Interval
	value.Episode.Candidates = []introdetect.Candidate{candidate}
	return value
}

func analysisDetectionTestInterval(value AnalysisStoredResult) (*int64, *int64) {
	if len(value.Episode.Candidates) == 0 {
		return nil, nil
	}
	interval := value.Episode.Candidates[0].Interval
	return &interval.StartTicks, &interval.EndTicks
}

func TestStoredAnalysisResultsAcceptCompleteMatcherStatesAndLibraryAbstentions(t *testing.T) {
	for _, status := range []introdetect.Status{introdetect.Qualified, introdetect.Review, introdetect.NoResult} {
		value := analysisDetectionTestValue(status)
		start, end := analysisDetectionTestInterval(value)
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), string(status), start, end); err != nil {
			t.Fatalf("valid matcher state %s was rejected: %v", status, err)
		}
		if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value}), string(status)); err != nil {
			t.Fatalf("result audit with a null decision was rejected: %v", err)
		}
	}
	for _, reason := range []string{"insufficient_cohort", "unsupported_item_type", "unsupported_hierarchy", "source_unavailable", "analysis_limit", "comparison_budget_exceeded", "cancelled"} {
		value := AnalysisStoredResult{Version: introdetect.Version, Reason: reason, Episode: introdetect.EpisodeResult{
			SourceKey: "source-without-extracted-identity", Status: introdetect.NoResult,
			Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{}}}
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "no_result", nil, nil); err != nil {
			t.Fatalf("library abstention %s fabricated a required identity: %v", reason, err)
		}
		if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value}), "no_result"); err != nil {
			t.Fatalf("library abstention audit was rejected: %v", err)
		}
	}
	unproven := analysisDetectionTestValue(introdetect.NoResult)
	unproven.Episode.Reasons = []introdetect.Reason{"source_timeline_unproven"}
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, unproven), "no_result", nil, nil); err != nil {
		t.Fatalf("an extracted identity with an unproven timeline was rejected: %v", err)
	}
}

func TestStoredAnalysisCandidatesRejectUnsupportedBoundariesAndWitnesses(t *testing.T) {
	options := introdetect.DefaultOptions()
	for _, example := range []struct {
		name   string
		status introdetect.Status
		mutate func(*introdetect.Candidate)
	}{
		{"overlong candidate", introdetect.Qualified, func(c *introdetect.Candidate) {
			c.Interval.EndTicks = c.Interval.StartTicks + options.MaxDurationTicks + 1
			c.Support[0].Interval = c.Interval
		}},
		{"short candidate", introdetect.Review, func(c *introdetect.Candidate) {
			c.Interval.EndTicks = c.Interval.StartTicks + options.MinDurationTicks - 1
			c.Support[0].Interval = c.Interval
		}},
		{"short qualified candidate", introdetect.Qualified, func(c *introdetect.Candidate) {
			c.Interval.EndTicks = c.Interval.StartTicks + options.AutoMinDurationTicks - 1
			c.Support[0].Interval = c.Interval
		}},
		{"overlong support", introdetect.Qualified, func(c *introdetect.Candidate) {
			c.Support[1].Interval.EndTicks = c.Support[1].Interval.StartTicks + options.MaxDurationTicks + 1
		}},
		{"short support", introdetect.Review, func(c *introdetect.Candidate) {
			c.Support[1].Interval.EndTicks = c.Support[1].Interval.StartTicks + options.MinDurationTicks - 1
		}},
		{"short qualified support", introdetect.Qualified, func(c *introdetect.Candidate) {
			c.Support[1].Interval.EndTicks = c.Support[1].Interval.StartTicks + options.AutoMinDurationTicks - 1
		}},
		{"support outside window", introdetect.Qualified, func(c *introdetect.Candidate) {
			c.Support[1].Interval = introdetect.Interval{StartTicks: options.WindowTicks - options.AutoMinDurationTicks + 1, EndTicks: options.WindowTicks + 1}
		}},
		{"negative support", introdetect.Review, func(c *introdetect.Candidate) { c.Support[1].Interval.StartTicks = -1 }},
		{"duplicate episode", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support[1].EpisodeKey = c.Support[0].EpisodeKey }},
		{"duplicate source", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support[1].SourceKey = c.Support[0].SourceKey }},
		{"duplicate content", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support[1].ContentIdentity = c.Support[0].ContentIdentity }},
		{"unproven content identity", introdetect.Review, func(c *introdetect.Candidate) { c.Support[1].ContentIdentity = "not-a-whole-content-hash" }},
		{"too few witnesses", introdetect.Review, func(c *introdetect.Candidate) {
			c.Support = c.Support[:options.MinSupport-1]
			c.Metrics.PairCount = len(c.Support) * (len(c.Support) - 1) / 2
		}},
		{"missing self witness", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support[0].SourceKey = "another-source" }},
		{"different self interval", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support[0].Interval.StartTicks++ }},
		{"null support", introdetect.Qualified, func(c *introdetect.Candidate) { c.Support = nil }},
		{"null reasons", introdetect.Qualified, func(c *introdetect.Candidate) { c.Reasons = nil }},
		{"qualified reason", introdetect.Qualified, func(c *introdetect.Candidate) { c.Reasons = []introdetect.Reason{introdetect.WeakAudioEvidence} }},
		{"unproven candidate timeline", introdetect.Review, func(c *introdetect.Candidate) { c.Reasons = []introdetect.Reason{"source_timeline_unproven"} }},
	} {
		t.Run(example.name, func(t *testing.T) {
			value := analysisDetectionTestValue(example.status)
			example.mutate(&value.Episode.Candidates[0])
			start, end := analysisDetectionTestInterval(value)
			if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), string(value.Episode.Status), start, end); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("invalid candidate boundary or witness was admitted")
			}
		})
	}
}

func TestStoredQualifiedAnalysisRequiresObservedAudioPairsAndVisualCoverage(t *testing.T) {
	options := introdetect.DefaultOptions()
	for _, mutate := range []func(*introdetect.Metrics){
		func(m *introdetect.Metrics) { m.AudioSamples = 0 },
		func(m *introdetect.Metrics) { m.AudioDistinct = 0 },
		func(m *introdetect.Metrics) { m.AudioDistinct = 11 },
		func(m *introdetect.Metrics) { m.AudioDistinct = m.AudioSamples + 1 },
		func(m *introdetect.Metrics) { m.PairCount = 0 },
		func(m *introdetect.Metrics) { m.PairCount++ },
		func(m *introdetect.Metrics) { m.AudioAgreementPermille = options.MinAudioAgreement - 1 },
		func(m *introdetect.Metrics) { m.AudioInformativePermille = options.MinAudioInformation - 1 },
		func(m *introdetect.Metrics) { m.AudioSimilarityPermille = options.MinAudioSimilarity - 1 },
		func(m *introdetect.Metrics) { m.VisualCoveragePermille = 699 },
		func(m *introdetect.Metrics) { m.VisualAgreementPermille = options.MinVisualAgreement - 1 },
		func(m *introdetect.Metrics) { m.VisualSimilarityPermille = options.MinVisualSimilarity - 1 },
		func(m *introdetect.Metrics) { m.VisualChangeCoveragePermille = options.MinVisualChangeCoverage - 1 },
		func(m *introdetect.Metrics) { m.VisualDominancePermille = options.MaxVisualDominance + 1 },
		func(m *introdetect.Metrics) { m.VisualSamples = options.MinVisualSamples - 1 },
		func(m *introdetect.Metrics) { m.VisualTransitions = options.MinVisualTransitions - 1 },
		func(m *introdetect.Metrics) { m.VisualSamples = 4097 },
		func(m *introdetect.Metrics) { m.VisualTransitions = 4097 },
		func(m *introdetect.Metrics) { m.AudioSamples = 8193 },
		func(m *introdetect.Metrics) { m.AudioAgreementPermille = 1001 },
		func(m *introdetect.Metrics) { m.AudioSimilarityPermille = -1 },
		func(m *introdetect.Metrics) { m.BoundaryUncertaintyTicks = -1 },
		func(m *introdetect.Metrics) { m.BoundaryUncertaintyTicks = 30*media.TicksPerSecond + 1 },
	} {
		value := analysisDetectionTestValue(introdetect.Qualified)
		mutate(&value.Episode.Candidates[0].Metrics)
		start, end := analysisDetectionTestInterval(value)
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("impossible qualified evidence was admitted: %+v", value.Episode.Candidates[0].Metrics)
		}
	}
	value := analysisDetectionTestValue(introdetect.Qualified)
	value.Episode.Candidates[0].Metrics.VisualSamples = 4096
	value.Episode.Candidates[0].Metrics.VisualTransitions = 4095
	start, end := analysisDetectionTestInterval(value)
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); err != nil {
		t.Fatalf("inclusive detector visual sample bound was rejected: %v", err)
	}
}

func TestStoredAnalysisResultStatusAndNullableArraysAreStrict(t *testing.T) {
	for _, mutate := range []func(*AnalysisStoredResult){
		func(v *AnalysisStoredResult) { v.Version = "future-detector" },
		func(v *AnalysisStoredResult) { v.Episode.Reasons = nil },
		func(v *AnalysisStoredResult) { v.Episode.Candidates = nil },
		func(v *AnalysisStoredResult) { v.Episode.Reasons = []introdetect.Reason{"unknown-reason"} },
		func(v *AnalysisStoredResult) {
			v.Episode.Reasons = []introdetect.Reason{introdetect.NoRepeatedInterval, introdetect.NoRepeatedInterval}
		},
		func(v *AnalysisStoredResult) { v.Episode.Status = "unknown-status" },
		func(v *AnalysisStoredResult) { v.Episode.ContentIdentity = "" },
		func(v *AnalysisStoredResult) { v.Episode.EpisodeKey = "" },
		func(v *AnalysisStoredResult) { v.Reason = "unknown-abstention" },
	} {
		value := analysisDetectionTestValue(introdetect.NoResult)
		mutate(&value)
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), string(value.Episode.Status), nil, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid no-result state or null array was admitted")
		}
	}
	qualified := analysisDetectionTestValue(introdetect.Qualified)
	start, end := analysisDetectionTestInterval(qualified)
	for _, pair := range [][2]*int64{{nil, end}, {start, nil}, {nil, nil}} {
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, qualified), "qualified", pair[0], pair[1]); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("candidate bounds were detached from the indexed interval")
		}
	}
	wrong := *end + 1
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, qualified), "qualified", start, &wrong); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("different indexed end ticks were accepted")
	}
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, qualified), "review", start, end); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("indexed status differs from its evidence")
	}
	review := analysisDetectionTestValue(introdetect.Review)
	review.Episode.Candidates = append(review.Episode.Candidates, review.Episode.Candidates[0])
	start, end = analysisDetectionTestInterval(review)
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, review), "review", start, end); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("duplicate candidate groups were accepted")
	}
	abstention := analysisDetectionTestValue(introdetect.NoResult)
	abstention.Reason = "source_unavailable"
	abstention.Episode.Reasons = []introdetect.Reason{}
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, abstention), "no_result", nil, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("an abstention invented an extracted content identity")
	}
}

func TestStoredAnalysisResultRejectsAmbiguousJSON(t *testing.T) {
	value := analysisDetectionTestValue(introdetect.Qualified)
	raw := analysisDetectionTestJSON(t, value)
	start, end := analysisDetectionTestInterval(value)
	for _, malformed := range [][]byte{
		[]byte(`null`), append([]byte(`{"Unknown":true,`), raw[1:]...),
		bytes.Replace(raw, []byte(`"Version":`), []byte(`"version":`), 1),
		bytes.Replace(raw, []byte(`"Reason":""`), []byte(`"Reason":"","Reason":""`), 1),
		bytes.Replace(raw, []byte(`,"Reason":""`), nil, 1),
		bytes.Replace(raw, []byte(`"Episode":{`), []byte(`"Episode":{"Unknown":true,`), 1),
		bytes.Replace(raw, []byte(`"AudioSamples":120`), []byte(`"AudioSamples":120,"AudioSamples":120`), 1),
		bytes.Replace(raw, []byte(`"Metrics":{`), []byte(`"Metrics":{"Unknown":true,`), 1),
		bytes.Replace(raw, []byte(`"AudioSamples":120`), []byte(`"AudioSamples":null`), 1),
		bytes.Replace(raw, []byte(`"Reasons":[]`), []byte(`"Reasons":null`), 1),
		append(append([]byte(nil), raw...), []byte(` {}`)...), bytes.Repeat([]byte(" "), 131073),
	} {
		if err := ValidateStoredAnalysisResult(malformed, "qualified", start, end); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("ambiguous result JSON was accepted")
		}
	}
}

func TestStoredAnalysisAuditPointerBranchesAndDecisionBounds(t *testing.T) {
	for _, action := range []string{"accept", "reject", "reset"} {
		decision := AnalysisDecision{Revision: "0", ManualRevision: "0", SourceRevision: "source-revision", Action: action}
		evidence := AnalysisAuditEvidence{Decision: &decision}
		raw := analysisDetectionTestJSON(t, evidence)
		if err := ValidateStoredAnalysisAudit(raw, action); err != nil {
			t.Fatalf("decision with null result was rejected: %v", err)
		}
		for _, malformed := range [][]byte{
			[]byte(`{"Result":null,"Decision":null}`), []byte(`{"Decision":null}`), []byte(`{"Result":null}`),
			append([]byte(`{"Unknown":0,`), raw[1:]...),
			bytes.Replace(raw, []byte(`"Result":null`), []byte(`"Result":null,"Result":null`), 1),
			bytes.Replace(raw, []byte(`"Decision":{`), []byte(`"Decision":{"Unknown":0,`), 1),
			bytes.Replace(raw, []byte(`"ManualRevision":"0"`), []byte(`"ManualRevision":"0","ManualRevision":"0"`), 1),
			bytes.Replace(raw, []byte(`"ManualRevision":"0"`), []byte(`"ManualRevision":null`), 1),
			bytes.Replace(raw, []byte(`"SourceRevision":"source-revision"`), []byte(`"SourceRevision":""`), 1),
			bytes.Replace(raw, []byte(`"SourceRevision":"source-revision"`), []byte(`"sourceRevision":"source-revision"`), 1),
		} {
			if err := ValidateStoredAnalysisAudit(malformed, action); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("ambiguous audit branch JSON was admitted")
			}
		}
		for _, revision := range []string{"", "-1", "01", "-0", "9223372036854775808"} {
			invalid := decision
			invalid.Revision = revision
			if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Decision: &invalid}), action); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid decision revision %q was admitted", revision)
			}
			invalid = decision
			invalid.ManualRevision = revision
			if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Decision: &invalid}), action); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid manual CAS revision %q was admitted", revision)
			}
		}
		decision.Action = "different-action"
		if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Decision: &decision}), action); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("audit action differs from its decision")
		}
	}
	value := analysisDetectionTestValue(introdetect.Qualified)
	decision := AnalysisDecision{Revision: "1", ManualRevision: "0", SourceRevision: "source-revision", Action: "accept"}
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value, Decision: &decision}), "qualified"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("result audit contains an unrelated decision branch")
	}
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{}), "qualified"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("result audit omitted its result evidence")
	}
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value}), "review"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("result audit action differs from its stored state")
	}
	value.Episode.Reasons = nil
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Result: &value, Decision: &decision}), "accept"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a decision audit bypassed validation of its present result")
	}
	if err := ValidateStoredAnalysisAudit(analysisDetectionTestJSON(t, AnalysisAuditEvidence{Decision: &decision}), "unknown"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown audit action was admitted")
	}
}
