package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestSubtitleTimelineRunAcceptsFullKindAndStrictSelection(t *testing.T) {
	const valid = `{"Kind":"subtitle-timeline","RequestId":"timeline-retry","LibraryIds":["library"],"ItemIds":["movie"],"Force":true}`
	input, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(valid))
	if !ok || input.TaskKey != library.TaskSubtitleTimelineGenerationKey || input.RequestID != "timeline-retry" || !input.Selection.Force || !reflect.DeepEqual(input.Selection.ItemIDs, []string{"movie"}) {
		t.Fatalf("full subtitle-timeline kind or selection was lost: %+v", input)
	}
	for _, raw := range []string{
		strings.Replace(valid, `"subtitle-timeline"`, `"Subtitle-Timeline"`, 1),
		strings.Replace(valid, `"Force":true`, `"Force":null`, 1),
		strings.Replace(valid, `"Force":true`, `"Force":true,"Force":false`, 1),
		strings.Replace(valid, `"timeline-retry"`, `null`, 1),
		strings.Replace(valid, `["movie"]`, `["movie","movie"]`, 1),
	} {
		w := httptest.NewRecorder()
		if _, accepted := decodeAdminMediaAnalysisRun(w, analysisInputRequestForTest(raw)); accepted || w.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous timeline admission accepted: %s", raw)
		}
	}
}

func TestSubtitleTimelineRuntimeProjectsExplicitAvailabilityAndReasons(t *testing.T) {
	raw, err := json.Marshal(adminMediaAnalysisRuntimeDTO(adminMediaAnalysisRuntimeStatus{}))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if object["SubtitleTimelineAvailable"] != false || !reflect.DeepEqual(object["SubtitleTimelineReasons"], []any{}) {
		t.Fatalf("timeline capability omitted its stable empty shape: %s", raw)
	}
}
