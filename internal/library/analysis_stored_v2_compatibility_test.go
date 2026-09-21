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

	"github.com/moooyo/goby/internal/introdetect"
)

func analysisV2Literal(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The literal fixtures retain the exact 00047ac field order and values. They
// are wire-format examples, not observations from a media or accuracy run.
func TestStoredAnalysisV2AdmissionPreservesCanonicalFingerprints(t *testing.T) {
	for _, fixture := range []struct{ name, fingerprint string }{
		{"intro", "2c255299bb4919df394fb980855fef0698d89c97f9418daac694aeb4cc08a4a4"},
		{"preview", "d98d3cffc35bdce942491b1c10f322c86b1bfe0689d5bcd566df2beecd5c0eda"},
		{"unavailable", "7a17d7533f15bb16083eee6c1eb61f67a68ba039502a9ba694d7bf8592482f95"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := analysisV2Literal(t, "analysis-admission-v2-"+fixture.name)
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != fixture.fingerprint {
				t.Fatal("the frozen v2 canonical fixture changed")
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
				t.Fatalf("known v2 admission rejected: %v", err)
			}
			var reordered map[string]json.RawMessage
			if err := json.Unmarshal(envelope.Execution, &reordered); err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(reordered)
			if err != nil || ValidateStoredAnalysisAdmission(envelope.Profile, wire, 7, 3, fixture.fingerprint) != nil {
				t.Fatal("JSONB order changed the frozen v2 fingerprint")
			}
			for _, invalid := range [][]byte{
				bytes.Replace(envelope.Execution, []byte(`"Version":2`), []byte(`"Version":3`), 1),
				bytes.Replace(envelope.Execution, []byte(`"Version":2`), []byte(`"Version":2,"Version":2`), 1),
				bytes.Replace(envelope.Execution, []byte(`"MinVisualStateAnchorTicks"`), []byte(`"MinVisualStateSupportTicks"`), 1),
				bytes.Replace(envelope.Execution, []byte(`"VisualBandTicks":`), []byte(`"FutureOption":`), 1),
			} {
				if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, invalid, 7, 3, fixture.fingerprint), ErrInvalidInput) {
					t.Fatal("mutated v2 wire acquired the old fingerprint")
				}
			}
			if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 8, 3, fixture.fingerprint), ErrInvalidInput) {
				t.Fatal("a changed revision retained v2 authority")
			}
			if fixture.name == "intro" {
				changed := bytes.Replace(raw, []byte(`"MaxVisualUnconfirmedGapTicks":50000000`), []byte(`"MaxVisualUnconfirmedGapTicks":40000000`), 1)
				changedEnvelope := envelope
				if bytes.Equal(changed, raw) || json.Unmarshal(changed, &changedEnvelope) != nil {
					t.Fatal("the frozen option mutation did not change the intended input")
				}
				changedDigest := sha256.Sum256(changed)
				if !errors.Is(ValidateStoredAnalysisAdmission(changedEnvelope.Profile, changedEnvelope.Execution, 7, 3, hex.EncodeToString(changedDigest[:])), ErrInvalidInput) {
					t.Fatal("a fresh digest made an unadmitted historical Options profile valid")
				}
			}
			var profile AnalysisProfile
			var execution AnalysisExecutionProfile
			if json.Unmarshal(envelope.Profile, &profile) != nil || json.Unmarshal(envelope.Execution, &execution) != nil ||
				!errors.Is(ValidateAnalysisExecutionProfile(execution), ErrInvalidInput) {
				t.Fatal("historical execution acquired current worker authority")
			}
			// New admission uses a new identity even with the same managed
			// settings, tools and extraction profile; old feature keys miss.
			execution.Version = AnalysisExecutionProfileVersion
			if execution.IntroProfile != "" {
				execution.DetectorVersion = introdetect.Version
			}
			if err := ValidateAnalysisExecutionProfile(execution); err != nil {
				t.Fatal(err)
			}
			current := analysisAdmissionFingerprint(profile, execution, 7, 3)
			source := AnalysisSource{ItemID: "same-item", SourceRevision: "same-source"}
			if current == fixture.fingerprint || analysisFeatureCacheKey(source, current) == analysisFeatureCacheKey(source, fixture.fingerprint) {
				t.Fatal("current admission reused a v2 feature-cache identity")
			}
		})
	}
}

func TestStoredAnalysisV2ResultsKeepHistoricalSemanticsAndAuditFacts(t *testing.T) {
	raw := analysisV2Literal(t, "analysis-result-v2-qualified")
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "6d0ae6ab835adc32e6eaa7f8fa8765820e393a57458a5ee4191f5c6dc1af4faf" {
		t.Fatal("the frozen v2 result fixture changed")
	}
	start, end := int64(100000000), int64(400000000)
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current != nil || facts.Version != "introdetect-v2" || facts.Episode.Status != "qualified" ||
		len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 ||
		facts.Episode.Candidates[0].Interval != (AnalysisStoredIntervalFacts{start, end}) {
		t.Fatalf("v2 observations were lost or promoted: %+v %v", facts, err)
	}
	audit := append(append([]byte(`{"Result":`), raw...), []byte(`,"Decision":null}`)...)
	if stored, err := ReadStoredAnalysisAudit(audit, "qualified"); err != nil || stored.Result == nil || stored.Result.Version != "introdetect-v2" {
		t.Fatalf("v2 nested audit rejected: %+v %v", stored, err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"introdetect-v2"`), []byte(`"introdetect-unknown"`), 1),
		bytes.Replace(raw, []byte(`"VisualAnchorCount":30,`), nil, 1),
		bytes.Replace(raw, []byte(`"VisualMinBandMatchedPermille":400`), []byte(`"VisualMinBandMatchedPermille":399`), 1),
		bytes.Replace(raw, []byte(`"VisualMaxUnconfirmedGapTicks":50000000`), []byte(`"VisualMaxUnconfirmedGapTicks":50000001`), 1),
		bytes.Replace(raw, []byte(`"VisualAnchorCount":30`), []byte(`"VisualAnchorCount":null`), 1),
		bytes.Replace(raw, []byte(`"source-2"`), []byte(`"source-1"`), 1),
	} {
		if bytes.Equal(invalid, raw) || !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatal("invalid v2 evidence passed the frozen decoder")
		}
	}
	wrongEnd := end + 1
	if !errors.Is(ValidateStoredAnalysisResult(raw, "qualified", &start, &wrongEnd), ErrInvalidInput) {
		t.Fatal("historical result ignored its stored interval")
	}
	// v2 keeps legacy motion counts as diagnostics, not qualification gates.
	legacyDiagnostics := bytes.Replace(raw, []byte(`"VisualTransitions":39`), []byte(`"VisualTransitions":0`), 1)
	legacyDiagnostics = bytes.Replace(legacyDiagnostics, []byte(`"VisualChangeCoveragePermille":1000`), []byte(`"VisualChangeCoveragePermille":0`), 1)
	legacyDiagnostics = bytes.Replace(legacyDiagnostics, []byte(`"VisualDominancePermille":25`), []byte(`"VisualDominancePermille":1000`), 1)
	if err := ValidateStoredAnalysisResult(legacyDiagnostics, "qualified", &start, &end); err != nil {
		t.Fatal("historical v2 qualification was reinterpreted using legacy motion gates")
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

func TestStoredAnalysisV2ReviewAndAbstentionDoNotBecomeCurrentCandidates(t *testing.T) {
	var qualified analysisStoredResultV2
	if err := json.Unmarshal(analysisV2Literal(t, "analysis-result-v2-qualified"), &qualified); err != nil {
		t.Fatal(err)
	}
	review := qualified
	review.Episode.Status = "review"
	review.Episode.Reasons = []string{"weak_audio_evidence", "insufficient_visual_anchors"}
	review.Episode.Candidates[0].Status = "review"
	review.Episode.Candidates[0].Reasons = review.Episode.Reasons
	review.Episode.Candidates[0].Metrics.AudioAgreementPermille = 857
	review.Episode.Candidates[0].Metrics.VisualMinBandMatchedPermille = 294
	noResult := analysisStoredResultV2{Version: "introdetect-v2", Episode: analysisStoredEpisodeV2{
		EpisodeKey: "episode-1", SourceKey: "source-1", ContentIdentity: strings.Repeat("1", 64), Status: "no_result",
		Reasons: []string{"no_repeated_interval"}, Candidates: []analysisStoredCandidateV2{}}}
	unavailable := analysisStoredResultV2{Version: "introdetect-v2", Reason: "source_unavailable", Episode: analysisStoredEpisodeV2{
		SourceKey: "source-1", Status: "no_result", Reasons: []string{}, Candidates: []analysisStoredCandidateV2{}}}
	for _, value := range []analysisStoredResultV2{review, noResult, unavailable} {
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
		t.Fatal("unavailable v2 history fabricated a matcher content identity")
	}
}
