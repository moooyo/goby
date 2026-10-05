package library

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
)

func analysisAdmissionTestCreditsExecution() AnalysisExecutionProfile {
	value := analysisAdmissionTestIntroExecution()
	value.DetectorVersion = introskipper.CreditsVersion
	value.IntroProfile = "credits-pinned-visual-and-audio-profile"
	value.FFmpegSHA256 = strings.Repeat("c", 64)
	value.FFprobeSHA256 = strings.Repeat("b", 64)
	return value
}

func TestCreditsAnalysisV6ExecutionSealsDetectorAndToolIdentities(t *testing.T) {
	credits := analysisAdmissionTestCreditsExecution()
	if err := ValidateAnalysisExecutionForTask(TaskCreditsAnalysisKey, credits); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAnalysisExecutionForTask(TaskIntroAnalysisKey, credits); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("credits profile admitted as intro: %v", err)
	}
	if err := ValidateAnalysisExecutionForTask(TaskCreditsAnalysisKey, analysisAdmissionTestIntroExecution()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("intro profile admitted as credits: %v", err)
	}
	if err := ValidateAnalysisExecutionForTask(TaskPreviewGenerationKey, credits); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("credits profile admitted as preview: %v", err)
	}
	for _, mutate := range []func(*AnalysisExecutionProfile){
		func(value *AnalysisExecutionProfile) { value.FFprobeSHA256 = "" },
		func(value *AnalysisExecutionProfile) { value.FingerprintSHA256 = "" },
		func(value *AnalysisExecutionProfile) { value.FFmpegSHA256 = "" },
		func(value *AnalysisExecutionProfile) { value.PreviewWidths = []int{320} },
	} {
		bad := credits
		mutate(&bad)
		if ValidateAnalysisExecutionForTask(TaskCreditsAnalysisKey, bad) == nil {
			t.Fatal("incomplete credits execution accepted")
		}
	}
	intro := analysisAdmissionTestIntroExecution()
	intro.FFprobeSHA256 = credits.FFprobeSHA256
	if ValidateAnalysisExecutionProfile(intro) == nil {
		t.Fatal("credits support relaxed the historical intro tool contract")
	}
	profile := DefaultAnalysisProfile()
	profileBytes, _ := json.Marshal(profile)
	executionBytes, _ := json.Marshal(credits)
	if err := ValidateStoredAnalysisAdmission(profileBytes, executionBytes, 1, 1, analysisAdmissionFingerprint(profile, credits, 1, 1)); err != nil {
		t.Fatalf("credits v6 admission did not round trip: %v", err)
	}
}

func TestCreditsDetectionPolicyIsIndependentAndDefaultOff(t *testing.T) {
	if DefaultLibraryOptions().EnableCreditsDetection {
		t.Fatal("credits detection must default off")
	}
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableCreditsDetection":true}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableCreditsDetection || options.EnableIntroDetection || options.EnablePreviewGeneration || options.EnableBackgroundPreviewGeneration || options.EnableAudioWaveformGeneration {
		t.Fatalf("coupled credits policy: %+v", options)
	}
	for _, kind := range []string{"movies", "tvshows", "mixed"} {
		if err := validateLibraryOptions(kind, options); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(validateLibraryOptions("music", options), ErrInvalidInput) {
		t.Fatal("unsupported music credits detection was accepted")
	}
}
