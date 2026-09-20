package media

import (
	"errors"
	"strings"
	"testing"
)

func analysisTestAvailability() AnalysisAvailability {
	return AnalysisAvailability{AudioAvailable: true, VisualAvailable: true, PreviewAvailable: true,
		FFmpegPath: "/tool/ffmpeg", FFprobePath: "/tool/ffprobe", FingerprintPath: "/tool/fingerprint",
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64),
		Fingerprint: analysisTestFingerprint("describe")}
}

func TestAnalysisIntroProfileBindsAdmittedToolsAndSampling(t *testing.T) {
	available := analysisTestAvailability()
	profile, err := IntroAlgorithmProfile(available, 0)
	if err != nil || profile == "" || len(profile) > 512 {
		t.Fatalf("profile: %q %v", profile, err)
	}
	explicit, err := IntroAlgorithmProfile(available, TicksPerSecond/2)
	if err != nil || profile != explicit {
		t.Fatal("default interval is not the explicit 500 ms profile")
	}
	paths := available
	paths.FFmpegPath, paths.FFprobePath, paths.FingerprintPath = "/other/a", "/other/b", "/other/c"
	if moved, err := IntroAlgorithmProfile(paths, 0); err != nil || moved != profile {
		t.Fatal("equivalent installed tool bytes acquired a per-path profile")
	}
	for _, mutate := range []func(*AnalysisAvailability){
		func(v *AnalysisAvailability) { v.FFmpegSHA256 = strings.Repeat("d", 64) },
		func(v *AnalysisAvailability) { v.FFprobeSHA256 = strings.Repeat("d", 64) },
		func(v *AnalysisAvailability) { v.FingerprintSHA256 = strings.Repeat("d", 64) },
		func(v *AnalysisAvailability) { v.Fingerprint.ItemDurationSamples++; v.Fingerprint.FirstItemEndSample++ },
	} {
		changed := available
		mutate(&changed)
		if actual, err := IntroAlgorithmProfile(changed, 0); err != nil || actual == profile {
			t.Fatalf("changed admitted algorithm facts reused a profile: %q %v", actual, err)
		}
	}
	if actual, err := IntroAlgorithmProfile(available, TicksPerSecond); err != nil || actual == profile {
		t.Fatal("sampling interval did not affect profile")
	}
}

func TestAnalysisIntroProfileRejectsIncompleteAdmission(t *testing.T) {
	for _, mutate := range []func(*AnalysisAvailability){
		func(v *AnalysisAvailability) { v.VisualAvailable = false },
		func(v *AnalysisAvailability) { v.AudioAvailable = false },
		func(v *AnalysisAvailability) { v.FFmpegSHA256 = "" },
		func(v *AnalysisAvailability) { v.FFprobeSHA256 = strings.Repeat("A", 64) },
		func(v *AnalysisAvailability) { v.FingerprintSHA256 = strings.Repeat("g", 64) },
	} {
		available := analysisTestAvailability()
		mutate(&available)
		if _, err := IntroAlgorithmProfile(available, 0); !errors.Is(err, ErrAnalysisUnavailable) {
			t.Fatalf("incomplete admission was accepted: %v", err)
		}
	}
}

func TestAnalysisIntroAdmissionPinsBothSerialPhasesWithoutAdoptingNewBytes(t *testing.T) {
	available := analysisTestAvailability()
	original := AnalysisExtractor{FFmpegPath: "/tool/ffmpeg", ExpectedFFmpegSHA256: available.FFmpegSHA256}
	admitted, err := analysisAdmittedExtractor(original, available)
	if err != nil || admitted.ExpectedFFmpegSHA256 != available.FFmpegSHA256 || admitted.ExpectedFFprobeSHA256 != available.FFprobeSHA256 || admitted.ExpectedFingerprintSHA256 != available.FingerprintSHA256 {
		t.Fatalf("serial-phase tool tuple was not frozen: %+v %v", admitted, err)
	}
	if original.ExpectedFFprobeSHA256 != "" || original.ExpectedFingerprintSHA256 != "" {
		t.Fatal("admission mutated the caller configuration")
	}
	changed := available
	changed.FFmpegSHA256 = strings.Repeat("d", 64)
	if _, err := analysisAdmittedExtractor(admitted, changed); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("a later phase adopted different FFmpeg bytes: %v", err)
	}
	changed = available
	changed.FFprobeSHA256 = strings.Repeat("d", 64)
	if _, err := analysisAdmittedExtractor(admitted, changed); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("a later phase adopted different geometry probe bytes: %v", err)
	}
	changed = available
	changed.FingerprintSHA256 = strings.Repeat("d", 64)
	if _, err := analysisAdmittedExtractor(admitted, changed); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("a later phase adopted different fingerprint bytes: %v", err)
	}
}
