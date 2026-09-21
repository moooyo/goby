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

func analysisV1Literal(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// These digests name the exact pre-v2 canonical bytes, not current Go structs.
// In particular preview/unavailable admissions contain every old zero option.
func TestStoredAnalysisV1AdmissionPreservesCanonicalFingerprints(t *testing.T) {
	for _, fixture := range []struct{ name, fingerprint string }{
		{"intro", "8ab3da6bbad0a0fd91898ef3f5898a50b2510f99bba226e862d4d58c4d5056cc"},
		{"preview", "bdede74f1655a1bf5234028d47767da32b9509beaa45ecdc98bb0191e005b918"},
		{"preview-original", "83e1e9882ac314b2854219be79eabe7cc68d0f7d3720cfa7d48a8d163d203390"},
		{"unavailable", "b393913e5c16f3161c7af19db51896e78ca90030cd7aec1b0418df0e45f1b579"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := analysisV1Literal(t, "analysis-admission-v1-"+fixture.name)
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != fixture.fingerprint {
				t.Fatal("the literal v1 canonical fixture changed")
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
				t.Fatalf("known historical admission rejected: %v", err)
			}
			var reordered map[string]json.RawMessage
			if err := json.Unmarshal(envelope.Execution, &reordered); err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(reordered)
			if err != nil || ValidateStoredAnalysisAdmission(envelope.Profile, wire, 7, 3, fixture.fingerprint) != nil {
				t.Fatal("JSONB field reordering changed the historical canonical fingerprint")
			}
			var live AnalysisExecutionProfile
			if json.Unmarshal(envelope.Execution, &live) != nil || !errors.Is(ValidateAnalysisExecutionProfile(live), ErrInvalidInput) {
				t.Fatal("historical execution acquired current admission authority")
			}
			for _, invalid := range [][]byte{
				bytes.Replace(envelope.Execution, []byte(`"Version":1`), []byte(`"Version":2`), 1),
				bytes.Replace(envelope.Execution, []byte(`"Version":1`), []byte(`"Version":1,"Version":1`), 1),
				bytes.Replace(envelope.Execution, []byte(`"DetectorOptions":{`), []byte(`"DetectorOptions":{"FutureOption":0,`), 1),
				bytes.Replace(envelope.Execution, []byte(`"DetectorOptions":{`), []byte(`"detectorOptions":{`), 1),
			} {
				if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, invalid, 7, 3, fixture.fingerprint), ErrInvalidInput) {
					t.Fatal("mutated historical shape or execution version retained its old fingerprint")
				}
			}
			if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 8, 3, fixture.fingerprint), ErrInvalidInput) {
				t.Fatal("a changed revision retained the historical fingerprint")
			}
			if strings.HasPrefix(fixture.name, "preview") {
				unknown := bytes.Replace(raw, []byte("source-pts-display-preceding-hold-jpeg-v3"), []byte("unknown-preview-profile"), 1)
				digest := sha256.Sum256(unknown)
				if err := json.Unmarshal(unknown, &envelope); err != nil {
					t.Fatal(err)
				}
				if !errors.Is(ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 7, 3, hex.EncodeToString(digest[:])), ErrInvalidInput) {
					t.Fatal("a matching canonical digest made an unknown historical preview profile valid")
				}
			}
		})
	}
}

func TestStoredAnalysisV1ResultExposesFactsWithoutCurrentMeasurements(t *testing.T) {
	raw := analysisV1Literal(t, "analysis-result-v1-qualified")
	start, end := int64(100000000), int64(400000000)
	facts, current, err := decodeAnalysisStoredResult(raw)
	if err != nil || current != nil || facts.Version != "introdetect-v1" || facts.Episode.Status != "qualified" ||
		len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 ||
		facts.Episode.Candidates[0].Interval != (AnalysisStoredIntervalFacts{start, end}) {
		t.Fatalf("legacy facts were lost or promoted into current measurements: %+v %v", facts, err)
	}
	if _, err := ReadStoredAnalysisResult(raw, "qualified", &start, &end); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"introdetect-v1"`), []byte(`"introdetect-unknown"`), 1),
		bytes.Replace(raw, []byte(`"Metrics":{`), []byte(`"Metrics":{"VisualAnchorCount":100,`), 1),
		bytes.Replace(raw, []byte(`,"PairCount":3`), nil, 1),
		bytes.Replace(raw, []byte(`"VisualCoveragePermille":1000`), []byte(`"VisualCoveragePermille":699`), 1),
		bytes.Replace(raw, []byte(`"source-2"`), []byte(`"source-1"`), 1),
	} {
		if !errors.Is(ValidateStoredAnalysisResult(invalid, "qualified", &start, &end), ErrInvalidInput) {
			t.Fatal("invalid legacy evidence passed a permissive compatibility path")
		}
	}
	wrongEnd := end + 1
	if !errors.Is(ValidateStoredAnalysisResult(raw, "qualified", &start, &wrongEnd), ErrInvalidInput) {
		t.Fatal("historical evidence ignored its relational interval")
	}
	audit := append(append([]byte(`{"Result":`), raw...), []byte(`,"Decision":null}`)...)
	if value, err := ReadStoredAnalysisAudit(audit, "qualified"); err != nil || value.Result == nil || value.Result.Episode.SourceKey != "source-1" {
		t.Fatalf("legacy nested audit result rejected: %+v %v", value, err)
	}
	for _, invalid := range []string{
		strings.Replace(string(audit), `"Decision":null`, `"Decision":null,"Decision":null`, 1),
		strings.Replace(string(audit), `"Result":`, `"result":`, 1),
		strings.TrimSuffix(string(audit), `,"Decision":null}`) + `}`,
	} {
		if !errors.Is(ValidateStoredAnalysisAudit([]byte(invalid), "qualified"), ErrInvalidInput) {
			t.Fatal("the versioned audit reader weakened its exact outer shape")
		}
	}
}

func TestStoredCurrentAnalysisRequiresExplicitNewZeroValuedObservations(t *testing.T) {
	value := analysisDetectionTestValue("qualified")
	raw := analysisDetectionTestJSON(t, value)
	missing := bytes.Replace(raw, []byte(`"VisualStartAnchorGapTicks":0,`), nil, 1)
	if bytes.Equal(missing, raw) {
		t.Fatal("current fixture did not contain the explicit zero-valued metric")
	}
	start, end := analysisDetectionTestInterval(value)
	if !errors.Is(ValidateStoredAnalysisResult(missing, "qualified", start, end), ErrInvalidInput) {
		t.Fatal("an absent new observation was silently synthesized as a measured zero")
	}
	profile := DefaultAnalysisProfile()
	execution := AnalysisExecutionProfile{Version: AnalysisExecutionProfileVersion, UnavailableReason: "disabled"}
	profileRaw := analysisAdmissionTestJSON(t, profile)
	executionRaw := analysisAdmissionTestJSON(t, execution)
	missing = bytes.Replace(executionRaw, []byte(`"VisualBandTicks":0,`), nil, 1)
	if bytes.Equal(missing, executionRaw) {
		t.Fatal("current unavailable fixture did not contain the new zero-valued option")
	}
	if !errors.Is(ValidateStoredAnalysisAdmission(profileRaw, missing, 1, 1, analysisAdmissionFingerprint(profile, execution, 1, 1)), ErrInvalidInput) {
		t.Fatal("a current unavailable envelope accepted the old incomplete option shape")
	}
}
