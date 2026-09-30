package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func analysisV4Literal(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The literal fixtures retain the exact 7fb7caa field order and values. They
// are wire-format examples, not observations from a media or accuracy run.
func TestStoredAnalysisV4AdmissionPreservesCanonicalFingerprints(t *testing.T) {
	for _, fixture := range []struct{ name, fingerprint string }{
		{"intro", "bab3d978992a00ef4144ed27066bfaa98f212bb9f7ca2f5de084f70b605aca14"},
		{"preview", "5757ffc3c2c10a9ecb0bd07a1c95e88937b45f5a812e1fc6d82d1b41ca9c9df7"},
		{"unavailable", "02faf32c1033367a33e7e2ba9fce44074e9a15c0798597dded5b182e5a4f808b"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := analysisV4Literal(t, "analysis-admission-v4-"+fixture.name)
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != fixture.fingerprint {
				t.Fatal("the frozen v4 canonical fixture changed")
			}
			var envelope struct {
				Version         int
				Revision, Epoch int64
				Profile         json.RawMessage
				Execution       json.RawMessage
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			if err := ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, envelope.Revision, envelope.Epoch, fixture.fingerprint); err != nil {
				t.Fatalf("known v4 admission rejected: %v", err)
			}
			var reordered map[string]json.RawMessage
			if err := json.Unmarshal(envelope.Execution, &reordered); err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(reordered)
			if err != nil || ValidateStoredAnalysisAdmission(envelope.Profile, wire, 7, 3, fixture.fingerprint) != nil {
				t.Fatal("JSONB order changed the frozen v4 fingerprint")
			}
			for _, invalid := range [][]byte{
				bytes.Replace(envelope.Execution, []byte(`"Version":4`), []byte(`"Version":5`), 1),
				bytes.Replace(envelope.Execution, []byte(`"Version":4`), []byte(`"Version":4,"Version":4`), 1),
				bytes.Replace(envelope.Execution, []byte(`"MinVisualStateAnchorTicks"`), []byte(`"MinVisualStateSupportTicks"`), 1),
				bytes.Replace(envelope.Execution, []byte(`"VisualBandTicks":`), []byte(`"FutureOption":`), 1),
			} {
				if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, invalid, 7, 3, fixture.fingerprint), ErrInvalidInput) {
					t.Fatal("mutated v4 wire acquired the old fingerprint")
				}
			}
			if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 8, 3, fixture.fingerprint), ErrInvalidInput) {
				t.Fatal("a changed revision retained v4 authority")
			}
			if fixture.name == "intro" {
				for _, change := range [][2]string{
					{`"MaxFeatureBytes":262144`, `"MaxFeatureBytes":786432`},
					{`"MaxComparisons":100000000`, `"MaxComparisons":200000000`},
					{`"MaxVisualUnconfirmedGapTicks":50000000`, `"MaxVisualUnconfirmedGapTicks":40000000`},
				} {
					changed := bytes.Replace(raw, []byte(change[0]), []byte(change[1]), 1)
					changedEnvelope := envelope
					// Detach both RawMessages before decoding the mutation.
					changedEnvelope.Profile, changedEnvelope.Execution = nil, nil
					if bytes.Equal(changed, raw) || json.Unmarshal(changed, &changedEnvelope) != nil {
						t.Fatal("the frozen option mutation did not change the intended input")
					}
					changedDigest := sha256.Sum256(changed)
					if !errors.Is(ValidateStoredAnalysisAdmission(changedEnvelope.Profile, changedEnvelope.Execution, 7, 3, hex.EncodeToString(changedDigest[:])), ErrInvalidInput) {
						t.Fatal("a fresh digest made an unadmitted historical Options profile valid")
					}
				}
			}
			var profile AnalysisProfile
			var execution AnalysisExecutionProfile
			if json.Unmarshal(envelope.Profile, &profile) != nil || json.Unmarshal(envelope.Execution, &execution) != nil ||
				!errors.Is(ValidateAnalysisExecutionProfile(execution), ErrInvalidInput) {
				t.Fatal("historical execution acquired current worker authority")
			}
			// Current admission has its own native execution and settings
			// identity; historical feature keys cannot acquire new semantics.
			execution.Version = AnalysisExecutionProfileVersion
			if execution.IntroProfile != "" {
				execution.DetectorVersion = analysisAdmissionTestIntroExecution().DetectorVersion
				if !errors.Is(ValidateAnalysisExecutionProfile(execution), ErrInvalidInput) {
					t.Fatal("changing version labels made frozen v4 Options executable")
				}
				execution = analysisAdmissionTestIntroExecution()
			}
			if err := ValidateAnalysisExecutionProfile(execution); err != nil {
				t.Fatal(err)
			}
			profile.IntroSkipper = DefaultAnalysisProfile().IntroSkipper
			current := analysisAdmissionFingerprint(profile, execution, 7, 3)
			source := AnalysisSource{ItemID: "same-item", SourceRevision: "same-source"}
			if current == fixture.fingerprint || analysisFeatureCacheKey(source, current) == analysisFeatureCacheKey(source, fixture.fingerprint) {
				t.Fatal("current admission reused a v4 feature-cache identity")
			}
		})
	}
}

func TestStoredAnalysisV4ResultsKeepHistoricalSemanticsAndAuditFacts(t *testing.T) {
	raw := analysisV4Literal(t, "analysis-result-v4-qualified")
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "5d04a45a6983a4958144d9aa2d3e67fd59b957a029b48b198c6995e8f95f5805" {
		t.Fatal("the frozen v4 result fixture changed")
	}
	start, end := int64(100000000), int64(400000000)
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current != nil || facts.Version != "introdetect-v4" || facts.Episode.Status != "qualified" ||
		len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 ||
		facts.Episode.Candidates[0].Interval != (AnalysisStoredIntervalFacts{start, end}) {
		t.Fatalf("v4 observations were lost or promoted: %+v %v", facts, err)
	}
	audit := append(append([]byte(`{"Result":`), raw...), []byte(`,"Decision":null}`)...)
	if stored, err := ReadStoredAnalysisAudit(audit, "qualified"); err != nil || stored.Result == nil || stored.Result.Version != "introdetect-v4" {
		t.Fatalf("v4 nested audit rejected: %+v %v", stored, err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"introdetect-v4"`), []byte(`"introdetect-unknown"`), 1),
		bytes.Replace(raw, []byte(`"Metrics":`), []byte(`"visualEvidence":null,"Metrics":`), 1),
		bytes.Replace(raw, []byte(`"Metrics":`), []byte(`"MeasurementPolicy":"future","Metrics":`), 1),
		bytes.Replace(raw, []byte(`"Metrics":`), []byte(`"CalibrationDigest":"future","Metrics":`), 1),
		bytes.Replace(raw, []byte(`"VisualAnchorCount":30,`), nil, 1),
		bytes.Replace(raw, []byte(`"VisualMinBandMatchedPermille":400`), []byte(`"VisualMinBandMatchedPermille":399`), 1),
		bytes.Replace(raw, []byte(`"VisualMaxUnconfirmedGapTicks":50000000`), []byte(`"VisualMaxUnconfirmedGapTicks":50000001`), 1),
		bytes.Replace(raw, []byte(`"VisualAnchorCount":30`), []byte(`"VisualAnchorCount":null`), 1),
		bytes.Replace(raw, []byte(`"source-2"`), []byte(`"source-1"`), 1),
	} {
		if bytes.Equal(invalid, raw) || !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatal("invalid v4 evidence passed the frozen decoder")
		}
	}
	wrongEnd := end + 1
	if !errors.Is(ValidateStoredAnalysisResult(raw, "qualified", &start, &wrongEnd), ErrInvalidInput) {
		t.Fatal("historical result ignored its stored interval")
	}
	// v4 already allowed an omitted or explicitly null visual pointer for
	// joint evidence. The canonical fixture retains the omitted field.
	if bytes.Contains(raw, []byte(`"VisualEvidence"`)) {
		t.Fatal("the frozen joint fixture lost its historical omission")
	}
	withNullVisual := bytes.Replace(raw, []byte(`"Metrics":`), []byte(`"VisualEvidence":null,"Metrics":`), 1)
	if err := ValidateStoredAnalysisResult(withNullVisual, "qualified", &start, &end); err != nil {
		t.Fatal("an optional v4 visual pointer became required")
	}
	// v4 keeps legacy motion counts as diagnostics, not qualification gates.
	legacyDiagnostics := bytes.Replace(raw, []byte(`"VisualTransitions":39`), []byte(`"VisualTransitions":0`), 1)
	legacyDiagnostics = bytes.Replace(legacyDiagnostics, []byte(`"VisualChangeCoveragePermille":1000`), []byte(`"VisualChangeCoveragePermille":0`), 1)
	legacyDiagnostics = bytes.Replace(legacyDiagnostics, []byte(`"VisualDominancePermille":25`), []byte(`"VisualDominancePermille":1000`), 1)
	if err := ValidateStoredAnalysisResult(legacyDiagnostics, "qualified", &start, &end); err != nil {
		t.Fatal("historical v4 qualification was reinterpreted using legacy motion gates")
	}
	for _, invalidAudit := range []string{
		strings.Replace(string(audit), `"Decision":null`, `"Decision":null,"Decision":null`, 1),
		strings.Replace(string(audit), `"Result":`, `"result":`, 1),
	} {
		if !errors.Is(ValidateStoredAnalysisAudit([]byte(invalidAudit), "qualified"), ErrInvalidInput) {
			t.Fatal("historical audit dispatch weakened the closed outer wire")
		}
	}
}

func TestStoredAnalysisV4ReviewAndAbstentionDoNotBecomeCurrentCandidates(t *testing.T) {
	var qualified analysisStoredResultV4
	if err := json.Unmarshal(analysisV4Literal(t, "analysis-result-v4-qualified"), &qualified); err != nil {
		t.Fatal(err)
	}
	review := qualified
	review.Episode.Status = "review"
	review.Episode.Reasons = []string{"weak_audio_evidence", "insufficient_visual_anchors"}
	review.Episode.Candidates[0].Status = "review"
	review.Episode.Candidates[0].Reasons = review.Episode.Reasons
	review.Episode.Candidates[0].Metrics.AudioAgreementPermille = 857
	review.Episode.Candidates[0].Metrics.VisualMinBandMatchedPermille = 294
	noResult := analysisStoredResultV4{Version: "introdetect-v4", Episode: analysisStoredEpisodeV4{
		EpisodeKey: "episode-1", SourceKey: "source-1", ContentIdentity: strings.Repeat("1", 64), Status: "no_result",
		Reasons: []string{"no_repeated_interval"}, Candidates: []analysisStoredCandidateV4{}}}
	unavailable := analysisStoredResultV4{Version: "introdetect-v4", Reason: "source_unavailable", Episode: analysisStoredEpisodeV4{
		SourceKey: "source-1", Status: "no_result", Reasons: []string{}, Candidates: []analysisStoredCandidateV4{}}}
	for _, value := range []analysisStoredResultV4{review, noResult, unavailable} {
		raw := analysisDetectionTestJSON(t, value)
		facts, current, err := decodeAnalysisStoredResult(raw)
		if err != nil || current != nil || facts.Episode.Status != value.Episode.Status || facts.Reason != value.Reason {
			t.Fatalf("valid historical outcome was rejected or upgraded: %+v %v", facts, err)
		}
		audit := append(append([]byte(`{"Result":`), raw...), []byte(`,"Decision":null}`)...)
		if err := ValidateStoredAnalysisAudit(audit, value.Episode.Status); err != nil {
			t.Fatal(err)
		}
	}
	invalid := unavailable
	invalid.Episode.ContentIdentity = strings.Repeat("1", 64)
	if _, _, err := decodeAnalysisStoredResult(analysisDetectionTestJSON(t, invalid)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unavailable v4 history fabricated a matcher content identity")
	}
}

func TestStoredAnalysisV4VisualFactsKeepTheHistoricalWire(t *testing.T) {
	raw := analysisV4Literal(t, "analysis-result-v4-visual")
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "272b6d7ad7393bdb9b36488601d757bec489494d28d9226a64afa483635263c0" {
		t.Fatal("the frozen v4 visual result fixture changed")
	}
	start, end := int64(100000000), int64(180000000)
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current != nil || facts.Version != "introdetect-v4" || facts.Episode.Status != "qualified" ||
		len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 ||
		facts.Episode.Candidates[0].Interval != (AnalysisStoredIntervalFacts{start, end}) {
		t.Fatalf("v4 visual observations were lost or promoted: %+v %v", facts, err)
	}
	if err := ValidateStoredAnalysisResult(raw, "qualified", &start, &end); err != nil {
		t.Fatal(err)
	}
	var frozen analysisStoredResultV4
	if err := analysisStrictJSON(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(frozen)
	if err != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("the frozen v4 visual field order changed")
	}
	metricsJSON, err := json.Marshal(frozen.Episode.Candidates[0].Metrics)
	if err != nil {
		t.Fatal(err)
	}
	metricsField := `"Metrics":` + string(metricsJSON)
	for _, replacement := range []string{`"Metrics":null,`, `"Metrics":{},`, ``} {
		invalid := bytes.Replace(raw, []byte(metricsField+","), []byte(replacement), 1)
		if bytes.Equal(invalid, raw) || !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatal("visual evidence lost the required complete zero joint metrics")
		}
	}
	for _, metric := range [][2]string{
		{"Samples", "16"}, {"CoveragePermille", "850"}, {"Transitions", "3"}, {"DistinctStates", "4"},
		{"DominantStatePermille", "650"}, {"MaxLumaRMSPermille", "550"}, {"MaxCenterRMSPermille", "650"},
		{"MaxGapTicks", "21000000"}, {"PairCount", "3"},
	} {
		field := `"` + metric[0] + `":`
		invalid := bytes.Replace(raw, []byte(field+metric[1]), []byte(field+"null"), 1)
		if bytes.Equal(invalid, raw) || !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatalf("a null historical visual metric was accepted: %s", metric[0])
		}
	}
	audit := append(append([]byte(`{"Result":`), raw...), []byte(`,"Decision":null}`)...)
	if stored, err := ReadStoredAnalysisAudit(audit, "qualified"); err != nil || stored.Result == nil || stored.Result.Version != "introdetect-v4" {
		t.Fatalf("v4 visual audit was rejected: %+v %v", stored, err)
	}
	for _, change := range [][2]string{
		{`"VisualEvidence":{`, `"VisualEvidence":{"MeasurementPolicy":"future",`},
		{`"VisualEvidence":{`, `"VisualEvidence":{"CalibrationDigest":"future",`},
		{`"VisualEvidence":{`, `"visualEvidence":{`},
		{`"VisualEvidence":{`, `"VisualEvidence":null,"VisualEvidence":{`},
		{`"Samples":16`, `"Samples":16,"Samples":16`},
		{`"Samples":16`, `"samples":16`},
		{`"Samples":16,`, ``},
		{`"Samples":16`, `"Samples":null`},
		{`"AudioAgreementPermille":0,`, ``},
		{`"AudioAgreementPermille":0`, `"AudioAgreementPermille":null`},
		{`"Metrics":{`, `"Metrics":null,"Metrics":{`},
	} {
		invalid := bytes.Replace(raw, []byte(change[0]), []byte(change[1]), 1)
		if bytes.Equal(invalid, raw) || !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatalf("invalid v4 visual wire passed the frozen decoder: %s", change[1])
		}
	}
	// A historical episode could be under review while retaining a qualified
	// visual candidate. The reader preserves this existing status relationship.
	frozen.Episode.Status = "review"
	facts, current, err = decodeAnalysisStoredResult(analysisDetectionTestJSON(t, frozen))
	if err != nil || current != nil || facts.Episode.Status != "review" || facts.Episode.Candidates[0].Status != "qualified" {
		t.Fatalf("a historical episode review changed its visual observation: %+v %v", facts, err)
	}
}

func TestStoredAnalysisV4SupportIntervalsKeepTheirHistoricalMeaning(t *testing.T) {
	for _, name := range []string{"qualified", "visual"} {
		t.Run(name, func(t *testing.T) {
			var value analysisStoredResultV4
			if err := json.Unmarshal(analysisV4Literal(t, "analysis-result-v4-"+name), &value); err != nil {
				t.Fatal(err)
			}
			candidate := &value.Episode.Candidates[0]
			// Equal visual spans may be shifted on each source clock. Joint
			// evidence did not require exactly equal supporting spans at all.
			candidate.Support[1].Interval.EndTicks++
			if name == "visual" {
				candidate.Support[1].Interval.StartTicks++
			}
			if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", &candidate.Interval.StartTicks, &candidate.Interval.EndTicks); err != nil {
				t.Fatal("historical supporting intervals inherited a stricter current rule")
			}
		})
	}
}

func TestStoredAnalysisV4VisualGateRetainsFrozenBoundaries(t *testing.T) {
	raw := analysisV4Literal(t, "analysis-result-v4-visual")
	for _, test := range []struct {
		name   string
		mutate func(*analysisStoredCandidateV4)
	}{
		{"samples_below_minimum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.Samples = 15 }},
		{"samples_above_maximum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.Samples = 4097 }},
		{"coverage_below_minimum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.CoveragePermille = 849 }},
		{"coverage_above_maximum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.CoveragePermille = 1001 }},
		{"transitions_below_minimum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.Transitions = 2 }},
		{"transitions_reach_samples", func(c *analysisStoredCandidateV4) { c.VisualEvidence.Transitions = c.VisualEvidence.Samples }},
		{"states_below_minimum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.DistinctStates = 3 }},
		{"states_exceed_samples", func(c *analysisStoredCandidateV4) { c.VisualEvidence.DistinctStates = c.VisualEvidence.Samples + 1 }},
		{"negative_dominance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.DominantStatePermille = -1 }},
		{"excessive_dominance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.DominantStatePermille = 651 }},
		{"negative_luma_distance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxLumaRMSPermille = -1 }},
		{"excessive_luma_distance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxLumaRMSPermille = 551 }},
		{"negative_center_distance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxCenterRMSPermille = -1 }},
		{"excessive_center_distance", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxCenterRMSPermille = 651 }},
		{"zero_gap", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxGapTicks = 0 }},
		{"excessive_gap", func(c *analysisStoredCandidateV4) { c.VisualEvidence.MaxGapTicks = 21000001 }},
		{"pairs_below_minimum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.PairCount = 2 }},
		{"pairs_above_maximum", func(c *analysisStoredCandidateV4) { c.VisualEvidence.PairCount = 497 }},
		{"incomplete_clique", func(c *analysisStoredCandidateV4) { c.VisualEvidence.PairCount = 4 }},
		{"invented_joint_metrics", func(c *analysisStoredCandidateV4) { c.Metrics.AudioSamples = 1 }},
		{"visual_review_candidate", func(c *analysisStoredCandidateV4) { c.Status = "review" }},
		{"visual_candidate_reason", func(c *analysisStoredCandidateV4) { c.Reasons = []string{"weak_visual_evidence"} }},
		{"unproven_timeline", func(c *analysisStoredCandidateV4) { c.Reasons = []string{"source_timeline_unproven"} }},
		{"duration_below_minimum", func(c *analysisStoredCandidateV4) { c.Interval.EndTicks-- }},
		{"duration_above_maximum", func(c *analysisStoredCandidateV4) { c.Interval.EndTicks = c.Interval.StartTicks + 900000001 }},
		{"negative_start", func(c *analysisStoredCandidateV4) { c.Interval.StartTicks = -1 }},
		{"beyond_prefix", func(c *analysisStoredCandidateV4) { c.Interval = analysisStoredIntervalV4{1120000001, 1200000001} }},
		{"missing_support", func(c *analysisStoredCandidateV4) { c.Support = c.Support[:2] }},
		{"duplicate_episode", func(c *analysisStoredCandidateV4) { c.Support[1].EpisodeKey = c.Support[0].EpisodeKey }},
		{"duplicate_source", func(c *analysisStoredCandidateV4) { c.Support[1].SourceKey = c.Support[0].SourceKey }},
		{"duplicate_content", func(c *analysisStoredCandidateV4) { c.Support[1].ContentIdentity = c.Support[0].ContentIdentity }},
		{"unequal_support_span", func(c *analysisStoredCandidateV4) { c.Support[1].Interval.EndTicks++ }},
		{"support_beyond_prefix", func(c *analysisStoredCandidateV4) {
			c.Support[1].Interval = analysisStoredIntervalV4{1120000001, 1200000001}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var value analysisStoredResultV4
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			candidate := &value.Episode.Candidates[0]
			test.mutate(candidate)
			if validateAnalysisCandidateV4(*candidate) {
				t.Fatal("invalid evidence passed the independent frozen v4 gate")
			}
			if !errors.Is(ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", &candidate.Interval.StartTicks, &candidate.Interval.EndTicks), ErrInvalidInput) {
				t.Fatal("invalid evidence passed the historical result dispatcher")
			}
		})
	}
	// The largest admitted interval ends exactly at the old prefix boundary.
	// The stored format has no source duration with which to add a half-runtime gate.
	var upper analysisStoredResultV4
	if err := json.Unmarshal(raw, &upper); err != nil {
		t.Fatal(err)
	}
	candidate := &upper.Episode.Candidates[0]
	candidate.Interval = analysisStoredIntervalV4{300000000, 1200000000}
	for index := range candidate.Support {
		candidate.Support[index].Interval = candidate.Interval
	}
	candidate.VisualEvidence.Samples = 4096
	candidate.VisualEvidence.CoveragePermille = 1000
	candidate.VisualEvidence.Transitions = 4095
	candidate.VisualEvidence.DistinctStates = 4096
	candidate.VisualEvidence.DominantStatePermille = 0
	candidate.VisualEvidence.MaxLumaRMSPermille = 0
	candidate.VisualEvidence.MaxCenterRMSPermille = 0
	candidate.VisualEvidence.MaxGapTicks = 1
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, upper), "qualified", &candidate.Interval.StartTicks, &candidate.Interval.EndTicks); err != nil {
		t.Fatal("an admitted v4 boundary inherited current calibration requirements")
	}
}
