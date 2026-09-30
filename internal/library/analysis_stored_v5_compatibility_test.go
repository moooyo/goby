package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
)

// These canonical examples retain the pre-v6 field order and defaults. They
// are wire-format fixtures, not observations from a corpus evaluation.
func TestStoredAnalysisV5AdmissionPreservesCanonicalFingerprints(t *testing.T) {
	for _, fixture := range []struct{ name, fingerprint string }{
		{"intro", "140d9e4870cac2ba07b8bc5be301150f08086ed7c0a8978dd4139243d3ca21aa"},
		{"preview", "e91dba633e7d05e6730d4df3636f5938f587434f1f456974b7eda6bff9b463ed"},
		{"unavailable", "ac15479830c3fe0a7108f206309f135eb0461976d3e196afb8ef32e0f58c2a89"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/analysis-admission-v5-" + fixture.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != fixture.fingerprint {
				t.Fatal("the frozen v5 canonical fixture changed")
			}
			var envelope struct {
				Version         int
				Revision, Epoch int64
				Profile         json.RawMessage
				Execution       json.RawMessage
			}
			if json.Unmarshal(raw, &envelope) != nil || ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 7, 3, fixture.fingerprint) != nil {
				t.Fatal("the pre-v6 canonical admission was rejected")
			}
			var reordered map[string]json.RawMessage
			if json.Unmarshal(envelope.Execution, &reordered) != nil || ValidateStoredAnalysisAdmission(envelope.Profile, analysisAdmissionTestJSON(t, reordered), 7, 3, fixture.fingerprint) != nil {
				t.Fatal("JSONB ordering changed the pre-v6 canonical fingerprint")
			}
			for _, invalid := range [][]byte{
				bytes.Replace(envelope.Execution, []byte(`"Version":5`), []byte(`"Version":6`), 1),
				bytes.Replace(envelope.Execution, []byte(`"Version":5`), []byte(`"Version":5,"Version":5`), 1),
				bytes.Replace(envelope.Execution, []byte(`"DetectorOptions":{`), []byte(`"DetectorOptions":{"Unknown":1,`), 1),
				append([]byte(`{"IntroSkipperOptions":{},`), envelope.Execution[1:]...),
			} {
				if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, invalid, 7, 3, fixture.fingerprint), ErrInvalidInput) {
					t.Fatal("mutated v5 execution retained its frozen hash")
				}
			}
			var active AnalysisExecutionProfile
			if json.Unmarshal(envelope.Execution, &active) != nil || !errors.Is(ValidateAnalysisExecutionProfile(active), ErrInvalidInput) {
				t.Fatal("retired v5 admission acquired current execution authority")
			}
		})
	}
}

func TestStoredAnalysisV5QualificationRetainsLegacyReaderAndGate(t *testing.T) {
	value := analysisDetectionTestValue(introdetect.Qualified)
	raw := analysisDetectionTestJSON(t, value)
	facts, legacy, err := decodeAnalysisStoredResult(raw)
	if err != nil || legacy == nil || facts.Version != "introdetect-v5" || facts.Episode.Status != "qualified" {
		t.Fatalf("v5 qualification was retired or promoted to another evidence type: %v", err)
	}
	value.Episode.Candidates[0].Support = value.Episode.Candidates[0].Support[:2]
	value.Episode.Candidates[0].Metrics.PairCount = 1
	start, end := analysisDetectionTestInterval(value)
	if !errors.Is(ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end), ErrInvalidInput) {
		t.Fatal("the native pair rule relaxed the old v5 support gate")
	}
}
