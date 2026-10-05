package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisCreditsWireRetainsGapsAndUnionsOnlyOverlaps(t *testing.T) {
	item := library.Item{Type: "Episode", Media: &media.Info{DurationTicks: 200}, DetectedCredits: []library.CreditsInterval{
		{StartTicks: 80, EndTicks: 110, Source: "Chapter"},
		{StartTicks: 100, EndTicks: 120, Source: "BlackFrame"},
		{StartTicks: 150, EndTicks: 190, Source: "Chromaprint"},
	}}
	want := []map[string]any{
		{"StartPositionTicks": int64(80), "EndPositionTicks": int64(120), "Source": "Combined"},
		{"StartPositionTicks": int64(150), "EndPositionTicks": int64(190), "Source": "Chromaprint"},
	}
	if got := itemCreditsIntervalsDTO(item); !reflect.DeepEqual(got, want) {
		t.Fatalf("overlap union erased the intervening content: %#v", got)
	}
	markers := itemChaptersDTO(item)
	if len(markers) != 1 || markers[0]["MarkerType"] != "CreditsStart" || markers[0]["StartPositionTicks"] != int64(80) {
		t.Fatalf("standard marker must describe only the first start: %#v", markers)
	}
	for _, provenance := range []string{"Manual", "Import", "Chapter"} {
		item.Credits = &library.CreditsPoint{StartTicks: 170, Provenance: provenance}
		want := []map[string]any{{"StartPositionTicks": int64(170), "EndPositionTicks": int64(200), "Source": provenance}}
		if !reflect.DeepEqual(itemCreditsIntervalsDTO(item), want) {
			t.Fatal("explicit or reserved chapter marker lost precedence")
		}
		markers := itemChaptersDTO(item)
		if len(markers) != 1 || markers[0]["MarkerType"] != "CreditsStart" || markers[0]["StartPositionTicks"] != int64(170) {
			t.Fatal("explicit marker emitted automatic boundaries")
		}
	}
	item.Credits = nil
	item.DetectedCredits = []library.CreditsInterval{{StartTicks: 0, EndTicks: 20, Source: "Chapter"}, {StartTicks: 30, EndTicks: 40, Source: "BlackFrame"}}
	if got := itemCreditsIntervalsDTO(item); len(got) != 2 || got[0]["StartPositionTicks"] != int64(0) {
		t.Fatalf("zero start or a real gap was discarded: %#v", got)
	}
	for _, invalid := range []library.Item{
		{Type: "Episode", Media: &media.Info{DurationTicks: 200}},
		{Type: "Episode", Media: &media.Info{DurationTicks: 200}, DetectedCredits: []library.CreditsInterval{{StartTicks: 190, EndTicks: 201, Source: "Chapter"}}},
		{Type: "Episode"}, {Type: "Series", IsFolder: true, Media: item.Media, DetectedCredits: item.DetectedCredits},
	} {
		raw, err := json.Marshal(itemCreditsIntervalsDTO(invalid))
		if err != nil || string(raw) != "[]" {
			t.Fatalf("empty authoritative interval set must be [], got %s %v", raw, err)
		}
	}
}

func TestAnalysisCreditsRuntimeDTOHasExplicitAvailabilityAndEmptyReasons(t *testing.T) {
	raw, err := json.Marshal(adminMediaAnalysisRuntimeDTO(adminMediaAnalysisRuntimeStatus{}))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if object["CreditsAvailable"] != false || !reflect.DeepEqual(object["CreditsReasons"], []any{}) {
		t.Fatalf("credits runtime contract omitted explicit unavailable state: %s", raw)
	}
	runtime := &mediaAnalysisRuntime{profiles: make(map[string]library.AnalysisExecutionProfile)}
	runtime.setUnavailableProfiles("not_configured")
	status := runtime.Status()
	if status.CreditsAvailable || !reflect.DeepEqual(status.CreditsReasons, []string{"not_configured"}) {
		t.Fatalf("disabled runtime claimed detection capability: %+v", status)
	}
}

func TestAnalysisCreditsKindIsIndependentAndStrictlyTyped(t *testing.T) {
	const raw = `{"Kind":"credits","RequestId":"credits-retry","LibraryIds":["library"],"ItemIds":["episode"],"Force":true}`
	input, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(raw))
	if !ok || input.TaskKey != library.TaskCreditsAnalysisKey || input.RequestID != "credits-retry" || !input.Selection.Force || !reflect.DeepEqual(input.Selection.ItemIDs, []string{"episode"}) {
		t.Fatalf("credits admitted through another task contract: %+v", input)
	}
	for _, invalid := range []string{strings.Replace(raw, `"credits"`, `"Credits"`, 1), strings.Replace(raw, `"Force":true`, `"Force":null`, 1), strings.Replace(raw, `"credits-retry"`, `null`, 1)} {
		w := httptest.NewRecorder()
		if _, accepted := decodeAdminMediaAnalysisRun(w, analysisInputRequestForTest(invalid)); accepted || w.Code != http.StatusBadRequest {
			t.Fatalf("invalid credits run accepted: %s", invalid)
		}
	}
}
