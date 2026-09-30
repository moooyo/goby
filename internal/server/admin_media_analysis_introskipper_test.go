package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
)

func analysisIntroSkipperConfigurationForTest(raw string) string {
	return strings.Replace(analysisConfigurationBodyForTest, analysisIntroSkipperBodyForTest, raw, 1)
}

func TestAdminMediaAnalysisIntroSkipperRetainsCompleteTypedOptions(t *testing.T) {
	for _, options := range []introskipper.Options{
		introskipper.DefaultOptions(),
		{AnalysisPercent: 1, AnalysisLengthLimit: 1, MinimumIntroDuration: 1, MaximumIntroDuration: 1},
		{AnalysisPercent: 50, AnalysisLengthLimit: 10, MinimumIntroDuration: 600, MaximumIntroDuration: 600,
			MaximumFingerprintPointDifferences: 32, MaximumTimeSkip: 30, InvertedIndexShift: 32},
		{AnalysisPercent: 10, AnalysisLengthLimit: 4, MinimumIntroDuration: 8, MaximumIntroDuration: 90,
			MaximumFingerprintPointDifferences: 4, MaximumTimeSkip: 3.125, InvertedIndexShift: 1},
	} {
		raw, err := json.Marshal(options)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		value, ok := decodeAdminMediaAnalysisConfiguration(w, analysisInputRequestForTest(analysisIntroSkipperConfigurationForTest(string(raw))))
		if !ok || value.Profile.IntroSkipper != options || introskipper.ValidateOptions(value.Profile.IntroSkipper) != nil {
			t.Fatalf("complete Intro Skipper options lost their exact values: got=%+v want=%+v response=%s", value.Profile.IntroSkipper, options, w.Body.String())
		}
	}
}

func requireIntroSkipperInputFailure(t *testing.T, body, field string) {
	t.Helper()
	w := httptest.NewRecorder()
	value, ok := decodeAdminMediaAnalysisConfiguration(w, analysisInputRequestForTest(body))
	if ok || w.Code != http.StatusBadRequest || value != (library.AnalysisConfigurationUpdate{}) {
		t.Fatalf("invalid Intro Skipper input returned usable settings: value=%+v response=%s", value, w.Body.String())
	}
	var response struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "invalid_input" || response.Error.Fields[field] == "" {
		t.Fatalf("invalid Intro Skipper input did not identify %s: %s, %v", field, w.Body.String(), err)
	}
}

func TestAdminMediaAnalysisIntroSkipperRequiresEveryFieldWithoutDefaults(t *testing.T) {
	for _, field := range adminMediaAnalysisIntroSkipperFields {
		t.Run(field, func(t *testing.T) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(analysisIntroSkipperBodyForTest), &object); err != nil {
				t.Fatal(err)
			}
			delete(object, field)
			raw, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			requireIntroSkipperInputFailure(t, analysisIntroSkipperConfigurationForTest(string(raw)), "Profile.IntroSkipper."+field)
		})
	}
	body := strings.Replace(analysisConfigurationBodyForTest, `,"IntroSkipper":`+analysisIntroSkipperBodyForTest, "", 1)
	requireIntroSkipperInputFailure(t, body, "Profile.IntroSkipper")
}

func TestAdminMediaAnalysisIntroSkipperRejectsAmbiguousNestedObjects(t *testing.T) {
	for _, raw := range []string{"null", "[]", `"options"`, `{}`, strings.TrimSuffix(analysisIntroSkipperBodyForTest, "}") + `,"Unexpected":true}`,
		strings.Replace(analysisIntroSkipperBodyForTest, `"AnalysisPercent"`, `"analysisPercent"`, 1)} {
		field := "Profile.IntroSkipper"
		if raw == "{}" {
			field += ".AnalysisPercent"
		}
		requireIntroSkipperInputFailure(t, analysisIntroSkipperConfigurationForTest(raw), field)
	}
	for _, name := range []string{`"AnalysisPercent"`, `"\u0041nalysisPercent"`} {
		raw := strings.TrimSuffix(analysisIntroSkipperBodyForTest, "}") + "," + name + `:25}`
		requireIntroSkipperInputFailure(t, analysisIntroSkipperConfigurationForTest(raw), "Profile.IntroSkipper.AnalysisPercent")
	}
	body := strings.Replace(analysisConfigurationBodyForTest, `"IntroSkipper":`+analysisIntroSkipperBodyForTest,
		`"IntroSkipper":`+analysisIntroSkipperBodyForTest+`,"IntroSkipper":`+analysisIntroSkipperBodyForTest, 1)
	requireIntroSkipperInputFailure(t, body, "Profile.IntroSkipper")
}

func TestAdminMediaAnalysisIntroSkipperReportsTypedRangeAndDurationErrors(t *testing.T) {
	for _, fixture := range []struct {
		field, original string
		invalid         []string
	}{
		{"AnalysisPercent", "25", []string{"0", "51", "25.0", "2.5e1", `"25"`, "null", "true", "9223372036854775808"}},
		{"AnalysisLengthLimit", "10", []string{"0", "11", "10.5", `"10"`, "null"}},
		{"MinimumIntroDuration", "15", []string{"0", "601", "15.5", `"15"`, "null"}},
		{"MaximumIntroDuration", "120", []string{"0", "14", "601", "120.5", `"120"`, "null"}},
		{"MaximumFingerprintPointDifferences", "6", []string{"-1", "33", "6.5", `"6"`, "null"}},
		{"MaximumTimeSkip", "3.5", []string{"-0.1", "30.0001", "1e309", `"3.5"`, "null", "true", "{}"}},
		{"InvertedIndexShift", "2", []string{"-1", "33", "2.5", `"2"`, "null"}},
	} {
		for _, invalid := range fixture.invalid {
			t.Run(fixture.field+"/"+invalid, func(t *testing.T) {
				raw := strings.Replace(analysisIntroSkipperBodyForTest, `"`+fixture.field+`":`+fixture.original, `"`+fixture.field+`":`+invalid, 1)
				requireIntroSkipperInputFailure(t, analysisIntroSkipperConfigurationForTest(raw), "Profile.IntroSkipper."+fixture.field)
			})
		}
	}
}

func TestAdminMediaAnalysisIntroSkipperDTOClonesSupportAndRetainsNullAlternative(t *testing.T) {
	original := library.AnalysisDetection{IntroSkipperCandidate: &introskipper.Candidate{
		UpstreamCommit: introskipper.UpstreamCommit,
		Support:        []introskipper.Support{{EpisodeKey: "episode-a", AlgorithmProfile: "profile-a"}},
	}}
	projected := adminMediaAnalysisDetectionDTO(original)
	projected.IntroSkipperCandidate.Support[0].EpisodeKey = "changed"
	projected.IntroSkipperCandidate.UpstreamCommit = "changed"
	if original.IntroSkipperCandidate.Support[0].EpisodeKey != "episode-a" || original.IntroSkipperCandidate.UpstreamCommit != introskipper.UpstreamCommit {
		t.Fatal("the response candidate retained mutable storage owned by the source detection")
	}
	empty := library.AnalysisDetection{IntroSkipperCandidate: &introskipper.Candidate{UpstreamCommit: introskipper.UpstreamCommit}}
	data, err := json.Marshal(adminMediaAnalysisDetectionDTO(empty))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	candidate := object["IntroSkipperCandidate"].(map[string]any)
	if !reflect.DeepEqual(candidate["Support"], []any{}) || candidate["UpstreamCommit"] != introskipper.UpstreamCommit {
		t.Fatalf("the new candidate omitted its safe empty support array: %s", data)
	}
	if value, present := object["Candidate"]; !present || value != nil {
		t.Fatal("the unused legacy candidate must remain an explicit null field")
	}
	data, err = json.Marshal(adminMediaAnalysisDetectionDTO(library.AnalysisDetection{}))
	if err != nil || !strings.Contains(string(data), `"IntroSkipperCandidate":null`) {
		t.Fatalf("an absent Intro Skipper candidate was not explicitly null: %s, %v", data, err)
	}
}

func TestAdminMediaAnalysisConfigurationDTOPreservesIntroSkipperPolicy(t *testing.T) {
	stamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("fixture", 8*60*60))
	value := library.AnalysisConfiguration{Profile: library.DefaultAnalysisProfile(), Defaults: library.DefaultAnalysisProfile(), UpdatedAt: stamp}
	value.Profile.IntroSkipper.MaximumTimeSkip = 1.25
	projected := adminMediaAnalysisConfigurationDTO(value)
	if projected.Profile != value.Profile || projected.Defaults != value.Defaults || projected.UpdatedAt.Location() != time.UTC {
		t.Fatal("configuration projection changed the independent Intro Skipper policy")
	}
}
