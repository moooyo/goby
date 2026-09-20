package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisAdmissionTestIntroExecution() AnalysisExecutionProfile {
	return AnalysisExecutionProfile{Version: AnalysisProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a1", 32), FFprobeSHA256: strings.Repeat("b2", 32), FingerprintSHA256: strings.Repeat("c3", 32),
		DetectorVersion: introdetect.Version, DetectorOptions: introdetect.DefaultOptions(),
		VisualIntervalTicks: media.TicksPerSecond / 2, IntroProfile: "intro-extraction-v1"}
}

func analysisAdmissionTestPreviewExecution() AnalysisExecutionProfile {
	return AnalysisExecutionProfile{Version: AnalysisProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a1", 32), FFprobeSHA256: strings.Repeat("b2", 32),
		PreviewProfile: media.PreviewAnalysisProfile, PreviewWidths: []int{240, 320, 400}}
}

func analysisAdmissionTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAnalysisExecutionProfileRequiresTheClosedAvailableContract(t *testing.T) {
	for _, valid := range []AnalysisExecutionProfile{analysisAdmissionTestIntroExecution(), analysisAdmissionTestPreviewExecution()} {
		before := analysisAdmissionTestJSON(t, valid)
		if err := ValidateAnalysisExecutionProfile(valid); err != nil {
			t.Fatalf("current extraction profile was rejected: %v", err)
		}
		if !bytes.Equal(before, analysisAdmissionTestJSON(t, valid)) {
			t.Fatal("profile validation rewrote the extraction inventory")
		}
	}
	for _, mutate := range []func(*AnalysisExecutionProfile){
		func(p *AnalysisExecutionProfile) { p.Version = 0 },
		func(p *AnalysisExecutionProfile) { p.Version++ },
		func(p *AnalysisExecutionProfile) { p.Available = false },
		func(p *AnalysisExecutionProfile) { p.UnavailableReason = "disabled" },
		func(p *AnalysisExecutionProfile) { p.FFmpegSHA256 = "" },
		func(p *AnalysisExecutionProfile) { p.FFprobeSHA256 = strings.Repeat("A", 64) },
		func(p *AnalysisExecutionProfile) { p.FFmpegSHA256 = strings.Repeat("g", 64) },
		func(p *AnalysisExecutionProfile) { p.FFprobeSHA256 = strings.Repeat("b", 63) },
		func(p *AnalysisExecutionProfile) { p.FingerprintSHA256 = "" },
		func(p *AnalysisExecutionProfile) { p.DetectorVersion = "different-detector" },
		func(p *AnalysisExecutionProfile) { p.DetectorOptions.MaxComparisons-- },
		func(p *AnalysisExecutionProfile) { p.DetectorOptions = introdetect.Options{} },
		func(p *AnalysisExecutionProfile) { p.VisualIntervalTicks = media.TicksPerSecond },
		func(p *AnalysisExecutionProfile) { p.IntroProfile = " leading-profile" },
		func(p *AnalysisExecutionProfile) { p.IntroProfile = "profile\u0085" },
		func(p *AnalysisExecutionProfile) { p.IntroProfile = string([]byte{0xff}) },
		func(p *AnalysisExecutionProfile) { p.IntroProfile = strings.Repeat("p", 513) },
		func(p *AnalysisExecutionProfile) { p.PreviewProfile = media.PreviewAnalysisProfile },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{320} },
	} {
		profile := analysisAdmissionTestIntroExecution()
		mutate(&profile)
		if err := ValidateAnalysisExecutionProfile(profile); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid or mixed intro execution profile was admitted: %+v: %v", profile, err)
		}
	}
	for _, mutate := range []func(*AnalysisExecutionProfile){
		func(p *AnalysisExecutionProfile) { p.FingerprintSHA256 = strings.Repeat("c3", 32) },
		func(p *AnalysisExecutionProfile) { p.DetectorVersion = introdetect.Version },
		func(p *AnalysisExecutionProfile) { p.DetectorOptions = introdetect.DefaultOptions() },
		func(p *AnalysisExecutionProfile) { p.VisualIntervalTicks = media.TicksPerSecond / 2 },
		func(p *AnalysisExecutionProfile) { p.IntroProfile = "intro-extraction-v1" },
		func(p *AnalysisExecutionProfile) { p.PreviewProfile = "" },
		func(p *AnalysisExecutionProfile) { p.PreviewProfile = "different-preview-profile" },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = nil },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{240, 320} },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{400, 320, 240} },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{240, 320, 320} },
		func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{240, 320, 400, 800} },
	} {
		profile := analysisAdmissionTestPreviewExecution()
		mutate(&profile)
		if err := ValidateAnalysisExecutionProfile(profile); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid or mixed preview execution profile was admitted: %+v: %v", profile, err)
		}
	}
}

func TestAnalysisExecutionUnavailableProfilesCarryOnlyAReason(t *testing.T) {
	for _, reason := range []string{"disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform"} {
		profile := AnalysisExecutionProfile{Version: AnalysisProfileVersion, UnavailableReason: reason}
		if err := ValidateAnalysisExecutionProfile(profile); err != nil {
			t.Fatalf("closed unavailable reason %q was rejected: %v", reason, err)
		}
		for _, mutate := range []func(*AnalysisExecutionProfile){
			func(p *AnalysisExecutionProfile) { p.Available = true },
			func(p *AnalysisExecutionProfile) { p.FFmpegSHA256 = strings.Repeat("a", 64) },
			func(p *AnalysisExecutionProfile) { p.FFprobeSHA256 = strings.Repeat("b", 64) },
			func(p *AnalysisExecutionProfile) { p.FingerprintSHA256 = strings.Repeat("c", 64) },
			func(p *AnalysisExecutionProfile) { p.IntroProfile = "fabricated-profile" },
			func(p *AnalysisExecutionProfile) { p.PreviewProfile = media.PreviewAnalysisProfile },
			func(p *AnalysisExecutionProfile) { p.PreviewWidths = []int{} },
			func(p *AnalysisExecutionProfile) { p.DetectorVersion = introdetect.Version },
			func(p *AnalysisExecutionProfile) { p.DetectorOptions.MinSupport = 3 },
			func(p *AnalysisExecutionProfile) { p.VisualIntervalTicks = 1 },
		} {
			invalid := profile
			mutate(&invalid)
			if err := ValidateAnalysisExecutionProfile(invalid); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("unavailable profile retained active execution facts: %+v", invalid)
			}
		}
	}
	for _, reason := range []string{"", "Disabled", "unknown", " disabled", "disabled\n"} {
		if err := ValidateAnalysisExecutionProfile(AnalysisExecutionProfile{Version: AnalysisProfileVersion, UnavailableReason: reason}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unknown unavailable reason %q was admitted", reason)
		}
	}
}

func TestStoredAnalysisAdmissionRequiresExactCompleteJSON(t *testing.T) {
	profile, execution := DefaultAnalysisProfile(), analysisAdmissionTestIntroExecution()
	profileRaw, executionRaw := analysisAdmissionTestJSON(t, profile), analysisAdmissionTestJSON(t, execution)
	fingerprint := analysisAdmissionFingerprint(profile, execution, 3, 7)
	if err := ValidateStoredAnalysisAdmission(profileRaw, executionRaw, 3, 7, fingerprint); err != nil {
		t.Fatal(err)
	}
	var reordered map[string]json.RawMessage
	if err := json.Unmarshal(executionRaw, &reordered); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoredAnalysisAdmission(profileRaw, analysisAdmissionTestJSON(t, reordered), 3, 7, fingerprint); err != nil {
		t.Fatalf("JSONB field ordering changed the admission fingerprint: %v", err)
	}
	for _, malformed := range []struct {
		name               string
		profile, execution []byte
	}{
		{"profile case", bytes.Replace(profileRaw, []byte(`"AutoPublishIntros"`), []byte(`"autoPublishIntros"`), 1), executionRaw},
		{"profile duplicate", bytes.Replace(profileRaw, []byte(`"AutoPublishIntros":true`), []byte(`"AutoPublishIntros":true,"AutoPublishIntros":true`), 1), executionRaw},
		{"profile missing", bytes.Replace(profileRaw, []byte(`"AutoPublishIntros":true,`), nil, 1), executionRaw},
		{"profile unknown", append([]byte(`{"Unknown":0,`), profileRaw[1:]...), executionRaw},
		{"profile nested instead of scalar", bytes.Replace(profileRaw, []byte(`"AutoPublishIntros":true`), []byte(`"AutoPublishIntros":{}`), 1), executionRaw},
		{"profile scalar type", bytes.Replace(profileRaw, []byte(`"PreviewQuality":80`), []byte(`"PreviewQuality":"80"`), 1), executionRaw},
		{"execution case", profileRaw, bytes.Replace(executionRaw, []byte(`"Available"`), []byte(`"available"`), 1)},
		{"execution duplicate", profileRaw, bytes.Replace(executionRaw, []byte(`"Available":true`), []byte(`"Available":true,"Available":true`), 1)},
		{"execution missing", profileRaw, bytes.Replace(executionRaw, []byte(`"UnavailableReason":"",`), nil, 1)},
		{"execution unknown", profileRaw, append([]byte(`{"Unknown":0,`), executionRaw[1:]...)},
		{"nested detector case", profileRaw, bytes.Replace(executionRaw, []byte(`"MaxEpisodes"`), []byte(`"maxEpisodes"`), 1)},
		{"nested detector duplicate", profileRaw, bytes.Replace(executionRaw, []byte(`"MaxEpisodes":32`), []byte(`"MaxEpisodes":32,"MaxEpisodes":32`), 1)},
		{"nested detector missing", profileRaw, bytes.Replace(executionRaw, []byte(`"MaxEpisodes":32,`), nil, 1)},
		{"nested detector unknown", profileRaw, bytes.Replace(executionRaw, []byte(`"DetectorOptions":{`), []byte(`"DetectorOptions":{"Unknown":0,`), 1)},
		{"nested detector null", profileRaw, bytes.Replace(executionRaw, []byte(`"MaxEpisodes":32`), []byte(`"MaxEpisodes":null`), 1)},
		{"null profile", []byte(`null`), executionRaw},
		{"null execution", profileRaw, []byte(`null`)},
		{"trailing JSON", profileRaw, append(append([]byte(nil), executionRaw...), []byte(` {}`)...)},
		{"oversized profile", bytes.Repeat([]byte(" "), 32769), executionRaw},
		{"oversized execution", profileRaw, bytes.Repeat([]byte(" "), 32769)},
	} {
		t.Run(malformed.name, func(t *testing.T) {
			if err := ValidateStoredAnalysisAdmission(malformed.profile, malformed.execution, 3, 7, fingerprint); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("ambiguous or incomplete admitted JSON was accepted")
			}
		})
	}
	// Null booleans must fail even when typed decoding would preserve the zero
	// value and therefore agree with a recomputed fingerprint.
	profile.AutoPublishIntros = false
	profileRaw = analysisAdmissionTestJSON(t, profile)
	profileRaw = bytes.Replace(profileRaw, []byte(`"AutoPublishIntros":false`), []byte(`"AutoPublishIntros":null`), 1)
	if err := ValidateStoredAnalysisAdmission(profileRaw, executionRaw, 3, 7, analysisAdmissionFingerprint(profile, execution, 3, 7)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a null zero-valued boolean bypassed exact admission decoding")
	}
	for _, validExecution := range []AnalysisExecutionProfile{analysisAdmissionTestPreviewExecution(), {Version: AnalysisProfileVersion, UnavailableReason: "dependencies_unavailable"}} {
		profileRaw := analysisAdmissionTestJSON(t, DefaultAnalysisProfile())
		executionRaw := analysisAdmissionTestJSON(t, validExecution)
		if err := ValidateStoredAnalysisAdmission(profileRaw, executionRaw, 3, 7, analysisAdmissionFingerprint(DefaultAnalysisProfile(), validExecution, 3, 7)); err != nil {
			t.Fatalf("valid closed admission JSON did not round trip: %v", err)
		}
	}
}

func TestStoredAnalysisAdmissionFingerprintBindsProfileToolsRevisionAndEpoch(t *testing.T) {
	profile, execution := DefaultAnalysisProfile(), analysisAdmissionTestIntroExecution()
	fingerprint := analysisAdmissionFingerprint(profile, execution, 3, 7)
	profileRaw, executionRaw := analysisAdmissionTestJSON(t, profile), analysisAdmissionTestJSON(t, execution)
	for _, pair := range [][2]int64{{0, 7}, {-1, 7}, {3, 0}, {3, -1}, {4, 7}, {3, 8}} {
		if err := ValidateStoredAnalysisAdmission(profileRaw, executionRaw, pair[0], pair[1], fingerprint); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unbound admission revision/epoch was accepted: %v", pair)
		}
	}
	for _, altered := range []string{"", strings.Repeat("0", 64), "A" + fingerprint[1:], fingerprint + "0"} {
		if err := ValidateStoredAnalysisAdmission(profileRaw, executionRaw, 3, 7, altered); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("an invalid admission fingerprint was accepted")
		}
	}
	profile.PreviewQuality++
	if err := ValidateStoredAnalysisAdmission(analysisAdmissionTestJSON(t, profile), executionRaw, 3, 7, fingerprint); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a changed profile retained an old admission fingerprint")
	}
	execution.FFprobeSHA256 = strings.Repeat("d4", 32)
	if err := ValidateStoredAnalysisAdmission(profileRaw, analysisAdmissionTestJSON(t, execution), 3, 7, fingerprint); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("changed tool bytes retained an old admission fingerprint")
	}
	invalidProfile := DefaultAnalysisProfile()
	invalidProfile.PreviewQuality = 0
	if err := ValidateStoredAnalysisAdmission(analysisAdmissionTestJSON(t, invalidProfile), executionRaw, 3, 7,
		analysisAdmissionFingerprint(invalidProfile, analysisAdmissionTestIntroExecution(), 3, 7)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a correct hash made an invalid profile admissible")
	}
	invalidExecution := analysisAdmissionTestIntroExecution()
	invalidExecution.PreviewProfile = media.PreviewAnalysisProfile
	if err := ValidateStoredAnalysisAdmission(profileRaw, analysisAdmissionTestJSON(t, invalidExecution), 3, 7,
		analysisAdmissionFingerprint(DefaultAnalysisProfile(), invalidExecution, 3, 7)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a correct hash made a mixed execution profile admissible")
	}
}

func TestAnalysisCohortHashIsOrderStableAndTracksSourceHierarchyAndMembership(t *testing.T) {
	sources := []AnalysisSource{
		{ItemID: "item-b", SourceRevision: "source-b", HierarchyRevision: "hierarchy-b", EpisodeKey: "episode-2", Target: true, Position: 1},
		{ItemID: "item-a", SourceRevision: "source-a", HierarchyRevision: "hierarchy-a", EpisodeKey: "episode-1", Position: 0},
		{ItemID: "item-c", SourceRevision: "source-c", HierarchyRevision: "hierarchy-c", EpisodeKey: "episode-3", Position: 2},
	}
	before := append([]AnalysisSource(nil), sources...)
	fingerprint := analysisCohortHash(sources)
	if !analysisSHA(fingerprint) || !reflect.DeepEqual(before, sources) {
		t.Fatal("cohort hashing rewrote its input or omitted a full digest")
	}
	reversed := []AnalysisSource{sources[2], sources[1], sources[0]}
	if analysisCohortHash(reversed) != fingerprint {
		t.Fatal("the same complete population has an order-dependent cohort revision")
	}
	for _, mutate := range []func(*AnalysisSource){
		func(s *AnalysisSource) { s.SourceRevision += "-changed" },
		func(s *AnalysisSource) { s.HierarchyRevision += "-changed" },
		func(s *AnalysisSource) { s.EpisodeKey += "-changed" },
		func(s *AnalysisSource) { s.ItemID += "-changed" },
	} {
		changed := append([]AnalysisSource(nil), sources...)
		mutate(&changed[0])
		if analysisCohortHash(changed) == fingerprint {
			t.Fatal("changed cohort evidence retained its admission stamp")
		}
	}
	if analysisCohortHash(sources[:2]) == fingerprint || analysisCohortHash(append(append([]AnalysisSource(nil), sources...), AnalysisSource{ItemID: "item-d"})) == fingerprint {
		t.Fatal("cohort membership changed without invalidating the admission stamp")
	}
	selected := append([]AnalysisSource(nil), sources...)
	selected[0].Target, selected[0].Position = false, 17
	if analysisCohortHash(selected) != fingerprint {
		t.Fatal("publication selection was confused with complete cohort evidence")
	}
}

func TestAnalysisCohortKeyDoesNotConfuseDelimitedIdentifierTuples(t *testing.T) {
	first := AnalysisSource{ItemID: "first", LibraryID: "library", SeriesID: "series:part", SeasonID: "season", EpisodeKey: "first-episode"}
	second := AnalysisSource{ItemID: "second", LibraryID: "library", SeriesID: "series", SeasonID: "part:season", EpisodeKey: "second-episode"}
	if analysisCohortKey(first) == analysisCohortKey(second) {
		t.Fatal("different series/season identifier tuples share a cohort key")
	}
}
