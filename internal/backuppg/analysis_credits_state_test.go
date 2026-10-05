package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisCreditsArchiveGatePreservesContextAndHistoricalSchemas(t *testing.T) {
	for _, version := range []int64{50, 58, 59} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := validateAnalysisCreditsState(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatalf("schema %d lost cancellation: %v", version, err)
		}
		want := error(nil)
		if version >= 59 {
			want = ErrDatabase
		}
		if err := validateAnalysisCreditsState(context.Background(), nil, version); !errors.Is(err, want) {
			t.Fatalf("schema %d crossed the credits migration boundary: %v", version, err)
		}
	}
}

func creditsArchiveAdmission(t *testing.T, execution library.AnalysisExecutionProfile) ([]byte, []byte, string) {
	t.Helper()
	profile := library.DefaultAnalysisProfile()
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	executionRaw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         library.AnalysisProfile
		Execution       library.AnalysisExecutionProfile
	}{6, 1, 1, profile, execution})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	return profileRaw, executionRaw, hex.EncodeToString(digest[:])
}

func creditsArchiveExecution() library.AnalysisExecutionProfile {
	return library.AnalysisExecutionProfile{Version: 6, Available: true,
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64),
		DetectorVersion: introskipper.CreditsVersion, IntroProfile: "archive-credits-tools-v1", IntroSkipperOptions: library.DefaultAnalysisProfile().IntroSkipper}
}

func TestAnalysisCreditsArchiveKeepsV6TaskAndSchemaBoundaries(t *testing.T) {
	credits := creditsArchiveExecution()
	intro := credits
	intro.FFprobeSHA256, intro.FingerprintSHA256, intro.DetectorVersion = "", intro.FFmpegSHA256, introskipper.Version
	unavailable := library.AnalysisExecutionProfile{Version: 6, UnavailableReason: "dependencies_unavailable"}
	for _, schema := range []int64{50, 53, 54, 55, 56, 57, 58, 59} {
		for _, value := range []struct {
			name, key string
			execution library.AnalysisExecutionProfile
			valid     bool
		}{
			{"credits", library.TaskCreditsAnalysisKey, credits, schema >= 59},
			{"intro", library.TaskIntroAnalysisKey, intro, schema >= 54},
			{"credits_as_intro", library.TaskIntroAnalysisKey, credits, false},
			{"intro_as_credits", library.TaskCreditsAnalysisKey, intro, false},
			{"credits_as_preview", library.TaskPreviewGenerationKey, credits, false},
			{"unavailable_credits", library.TaskCreditsAnalysisKey, unavailable, schema >= 59},
			{"unavailable_intro", library.TaskIntroAnalysisKey, unavailable, schema >= 54},
		} {
			profile, execution, fingerprint := creditsArchiveAdmission(t, value.execution)
			if validAnalysisStateAdmission(value.key, profile, execution, 1, 1, fingerprint, schema) != value.valid {
				t.Fatalf("schema %d reinterpreted %s admission", schema, value.name)
			}
		}
		for _, resultVersion := range []string{introskipper.CreditsVersion, library.AnalysisCreditsResultVersion} {
			if validAnalysisStateResultVersion(resultVersion, schema) != (schema >= 59) {
				t.Fatalf("schema %d accepted a future credits result format", schema)
			}
		}
	}
	if analysisStateRelationsForVersion(58) != analysisStateRelationsSQL || strings.Contains(analysisStateRelationsForVersion(58), "credits") {
		t.Fatal("schema 59 rewrote the historical SQL admission contract")
	}
}

func TestAnalysisCreditsArchiveRequiresTheEntireExplicitV6WireShape(t *testing.T) {
	for _, execution := range []library.AnalysisExecutionProfile{creditsArchiveExecution(), {Version: 6, UnavailableReason: "dependencies_unavailable"}} {
		profile, raw, fingerprint := creditsArchiveAdmission(t, execution)
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("decode v6 execution fixture")
		}
		for field, original := range fields {
			delete(fields, field)
			missing, _ := json.Marshal(fields)
			if validAnalysisStateAdmission(library.TaskCreditsAnalysisKey, profile, missing, 1, 1, fingerprint, 59) {
				t.Fatalf("credits archive silently defaulted missing v6 field %s", field)
			}
			fields[field] = original
		}
	}
}
