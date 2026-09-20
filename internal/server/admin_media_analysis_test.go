package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

const analysisConfigurationBodyForTest = `{"Revision":"1","Profile":{"AutoPublishIntros":true,"PreviewIntervalSeconds":10,"PreviewQuality":80,"MaxSourceBytes":137438953472,"MaxItemRuntimeSeconds":1200,"FeatureCacheMaxBytes":134217728}}`
const analysisRunBodyForTest = `{"Kind":"intro","RequestId":"retry-1","LibraryIds":["library-b","library-a"],"ItemIds":["item-b","item-a"],"Force":true}`
const analysisDecisionBodyForTest = `{"Revision":"0","SourceRevision":"intro-source-v1-current","ManualRevision":"0","Action":"reset"}`

func analysisInputRequestForTest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/media-analysis/runs", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestAdminMediaAnalysisStrictBodiesPreserveScopeAndRevisionPrecision(t *testing.T) {
	w := httptest.NewRecorder()
	configuration, ok := decodeAdminMediaAnalysisConfiguration(w, analysisInputRequestForTest(analysisConfigurationBodyForTest))
	if !ok || configuration.Revision != "1" || configuration.Profile != library.DefaultAnalysisProfile() {
		t.Fatal("complete configuration did not retain its typed profile")
	}
	run, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(analysisRunBodyForTest))
	if !ok || run.TaskKey != library.TaskIntroAnalysisKey || run.RequestID != "retry-1" || !run.Selection.Force ||
		!reflect.DeepEqual(run.Selection.LibraryIDs, []string{"library-a", "library-b"}) || !reflect.DeepEqual(run.Selection.ItemIDs, []string{"item-a", "item-b"}) {
		t.Fatal("combined library/item selection was lost or not canonical")
	}
	all := `{"Kind":"previews","RequestId":"all","LibraryIds":[],"ItemIds":[],"Force":false}`
	if result, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(all)); !ok || result.TaskKey != library.TaskPreviewGenerationKey || result.Selection.LibraryIDs == nil || result.Selection.ItemIDs == nil {
		t.Fatal("explicit all-library selection was not retained")
	}
	decision, ok := decodeAdminMediaAnalysisDecision(httptest.NewRecorder(), analysisInputRequestForTest(analysisDecisionBodyForTest))
	if !ok || decision.Revision != "0" || decision.ManualRevision != "0" || decision.Action != "reset" {
		t.Fatal("initial decision tombstone revisions were rejected")
	}
	revision := "9007199254740993"
	if value, ok := decodeAdminMediaAnalysisPrune(httptest.NewRecorder(), analysisInputRequestForTest(`{"Revision":"`+revision+`"}`)); !ok || value != revision {
		t.Fatal("configuration revision lost precision at the HTTP boundary")
	}
}

func TestAdminMediaAnalysisRejectsAmbiguousOrLossyWrites(t *testing.T) {
	decoders := []struct {
		name, valid string
		decode      func(http.ResponseWriter, *http.Request) bool
	}{
		{"configuration", analysisConfigurationBodyForTest, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminMediaAnalysisConfiguration(w, r)
			return ok
		}},
		{"run", analysisRunBodyForTest, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminMediaAnalysisRun(w, r)
			return ok
		}},
		{"decision", analysisDecisionBodyForTest, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminMediaAnalysisDecision(w, r)
			return ok
		}},
		{"prune", `{"Revision":"1"}`, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminMediaAnalysisPrune(w, r)
			return ok
		}},
	}
	for _, decoder := range decoders {
		t.Run(decoder.name, func(t *testing.T) {
			for _, body := range []string{"", "null", "[]", "{}", decoder.valid + " {}", strings.TrimSuffix(decoder.valid, "}") + `,"Unexpected":true}`, decoder.valid + strings.Repeat(" ", maxAdminMediaAnalysisBodyBytes)} {
				w := httptest.NewRecorder()
				if decoder.decode(w, analysisInputRequestForTest(body)) || w.Code != http.StatusBadRequest {
					t.Fatal("ambiguous, incomplete or oversized JSON was accepted")
				}
			}
			for _, mediaType := range []string{"", "text/plain", "application/json;charset=latin1", "application/json;other=utf-8", "application/json;charset*=UTF-8''utf-8", "application/json;charset=utf-8;charset=utf-8"} {
				r := analysisInputRequestForTest(decoder.valid)
				r.Header.Set("Content-Type", mediaType)
				w := httptest.NewRecorder()
				if decoder.decode(w, r) || w.Code != http.StatusUnsupportedMediaType {
					t.Fatal("unsupported JSON transport was accepted")
				}
			}
			for _, query := range []string{"?", "?Force=true"} {
				r := analysisInputRequestForTest(decoder.valid)
				r.URL.RawQuery, r.URL.ForceQuery = strings.TrimPrefix(query, "?"), true
				w := httptest.NewRecorder()
				if decoder.decode(w, r) || w.Code != http.StatusBadRequest {
					t.Fatal("write query was accepted")
				}
			}
		})
	}
	for _, body := range []string{
		strings.Replace(analysisConfigurationBodyForTest, `"Revision":"1"`, `"Revision":1`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"Revision":"1"`, `"Revision":"01"`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"Revision":"1"`, `"Revision":"0"`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"PreviewQuality":80`, `"PreviewQuality":80,"PreviewQuality":81`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"PreviewQuality":80`, `"previewQuality":80`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"AutoPublishIntros":true`, `"AutoPublishIntros":null`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"MaxSourceBytes":137438953472`, `"MaxSourceBytes":1099511627777`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"PreviewQuality":80`, `"PreviewQuality":80.0`, 1),
	} {
		if _, ok := decodeAdminMediaAnalysisConfiguration(httptest.NewRecorder(), analysisInputRequestForTest(body)); ok {
			t.Fatal("invalid complete profile was accepted")
		}
	}
	for _, body := range []string{
		strings.Replace(analysisRunBodyForTest, `"intro"`, `"Intro"`, 1),
		strings.Replace(analysisRunBodyForTest, `"retry-1"`, `null`, 1),
		strings.Replace(analysisRunBodyForTest, `"retry-1"`, `""`, 1),
		strings.Replace(analysisRunBodyForTest, `"retry-1"`, `"\ud800"`, 1),
		strings.Replace(analysisRunBodyForTest, `["item-b","item-a"]`, `null`, 1),
		strings.Replace(analysisRunBodyForTest, `["item-b","item-a"]`, `["item-a","item-a"]`, 1),
		strings.Replace(analysisRunBodyForTest, `"Force":true`, `"Force":true,"Force":false`, 1),
	} {
		if _, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(body)); ok {
			t.Fatal("ambiguous typed task selection was accepted")
		}
	}
	for _, value := range []string{`null`, `0`, `"00"`, `"-1"`, `"9223372036854775808"`} {
		body := strings.Replace(analysisDecisionBodyForTest, `"Revision":"0"`, `"Revision":`+value, 1)
		if _, ok := decodeAdminMediaAnalysisDecision(httptest.NewRecorder(), analysisInputRequestForTest(body)); ok {
			t.Fatal("noncanonical decision revision was accepted")
		}
	}
}

func TestAdminMediaAnalysisQueriesAreScopedBoundedAndUnambiguous(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/media-analysis/items?LibraryId=library-a&SearchTerm=Intro%20%25&StartIndex=25&Limit=25", nil)
	query, ok := adminMediaAnalysisQuery(httptest.NewRecorder(), r)
	if !ok || query.LibraryID != "library-a" || query.SearchTerm != "Intro %" || query.StartIndex != 25 || query.Limit != 25 {
		t.Fatal("query scope was changed")
	}
	for _, query := range []string{"LibraryId=", "libraryId=a", "LibraryId=a&LibraryId=b", "SearchTerm=%ff", "SearchTerm=%00", "Limit=0", "Limit=201", "Limit=01", "StartIndex=-1", "StartIndex=2147483648", "Unknown=1", "SearchTerm=" + strings.Repeat("x", 257)} {
		w := httptest.NewRecorder()
		if _, ok := adminMediaAnalysisQuery(w, httptest.NewRequest(http.MethodGet, "/admin/v1/media-analysis/items?"+query, nil)); ok || w.Code != http.StatusBadRequest {
			t.Fatal("invalid query was accepted")
		}
	}
}

func TestAdminMediaAnalysisDTOsUseClosedSafeShapesAndEmptyArrays(t *testing.T) {
	stamp := time.Date(2026, 9, 21, 12, 0, 0, 0, time.FixedZone("fixture", 8*60*60))
	item := library.AnalysisItem{ID: "item", Name: "Episode", Type: "Episode", LibraryID: "library", MediaSourceID: "source", SourceRevision: "revision",
		Detection: library.AnalysisDetection{ItemID: "item", Revision: "0", ManualRevision: "0", SourceRevision: "revision", Status: "none", UpdatedAt: stamp,
			Candidate: &introdetect.Candidate{GroupID: "group"}}}
	encoded, err := json.Marshal(adminMediaAnalysisItemDTO(item))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	expected := []string{"Id", "Name", "Type", "LibraryId", "MediaSourceId", "SourceRevision", "Detection", "Previews"}
	if len(object) != len(expected) {
		t.Fatal("item exposed an undocumented field")
	}
	for _, name := range expected {
		if _, exists := object[name]; !exists {
			t.Fatal("item omitted a public field")
		}
	}
	detection := object["Detection"].(map[string]any)
	for _, value := range []any{object["Previews"], detection["Reasons"], detection["Candidate"].(map[string]any)["Reasons"], detection["Candidate"].(map[string]any)["Support"]} {
		if array, ok := value.([]any); !ok || len(array) != 0 {
			t.Fatal("empty array was serialized as null")
		}
	}
	if detection["UpdatedAt"] != stamp.UTC().Format(time.RFC3339Nano) {
		t.Fatal("timestamp was not UTC")
	}
	encoded, err = json.Marshal(adminMediaAnalysisRuntimeDTO(adminMediaAnalysisRuntimeStatus{}))
	if err != nil {
		t.Fatal(err)
	}
	object = nil
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != 5 || object["Cache"] != nil || !reflect.DeepEqual(object["Reasons"], []any{}) {
		t.Fatal("runtime safe empty projection changed")
	}
}

func TestAdminMediaAnalysisConflictMapsToActionableHTTPStatus(t *testing.T) {
	app := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/media-analysis/runs", nil)
	w := httptest.NewRecorder()
	app.taskError(w, r, tasks.ErrActiveRunConflict)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"active_run_conflict"`) {
		t.Fatal("active scope conflict was not mapped to HTTP 409")
	}
	for _, cause := range []error{library.ErrAnalysisConflict, library.ErrAnalysisSourceChanged, library.ErrAnalysisSuppressed} {
		w := httptest.NewRecorder()
		app.mediaAnalysisError(w, r, cause)
		if w.Code != http.StatusConflict {
			t.Fatal("analysis revision conflict was not mapped to HTTP 409")
		}
	}
}
