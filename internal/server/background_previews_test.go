package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func TestBackgroundPreviewRunInputPreservesExplicitRegeneration(t *testing.T) {
	const body = `{"Kind":"background","RequestId":"background-retry","LibraryIds":["library-b","library-a"],"ItemIds":["item-b","item-a"],"Force":true}`
	input, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(body))
	if !ok || input.TaskKey != library.TaskBackgroundPreviewGenerationKey || input.RequestID != "background-retry" || !input.Selection.Force ||
		!reflect.DeepEqual(input.Selection.LibraryIDs, []string{"library-a", "library-b"}) || !reflect.DeepEqual(input.Selection.ItemIDs, []string{"item-a", "item-b"}) {
		t.Fatalf("background selection changed: %+v", input)
	}
	for _, raw := range []string{
		strings.Replace(body, `"background"`, `"Background"`, 1),
		strings.Replace(body, `"Force":true`, `"Force":null`, 1),
		strings.Replace(body, `"Force":true`, `"Force":true,"Force":false`, 1),
		strings.Replace(body, `"Force":true`, `"Regenerate":true`, 1),
		strings.Replace(body, `"background-retry"`, `null`, 1),
		strings.Replace(body, `"background-retry"`, `""`, 1),
		strings.Replace(body, `["item-b","item-a"]`, `null`, 1),
	} {
		w := httptest.NewRecorder()
		if _, accepted := decodeAdminMediaAnalysisRun(w, analysisInputRequestForTest(raw)); accepted || w.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous background selection accepted: %s", raw)
		}
	}
}

func TestBackgroundPreviewConflictsAndWorkerFailuresRemainActionable(t *testing.T) {
	app := &Server{}
	for _, cause := range []error{library.ErrBackgroundPreviewConflict, library.ErrBackgroundClipConflict} {
		w := httptest.NewRecorder()
		app.backgroundPreviewError(w, httptest.NewRequest(http.MethodPut, "/admin/v1/background-previews/configuration", nil), fmt.Errorf("wrapped: %w", cause))
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"background_preview_conflict"`) {
			t.Fatalf("persistent material conflict lost its HTTP contract: %d %s", w.Code, w.Body.String())
		}
	}
	for _, test := range []struct {
		cause error
		code  string
	}{
		{nil, ""}, {context.Canceled, "cancelled"}, {context.DeadlineExceeded, "processing_limit"},
		{media.ErrAnalysisBudget, "processing_limit"}, {library.ErrBackgroundClipConflict, "material_conflict"},
		{library.ErrBackgroundPreviewConflict, "request_changed"}, {media.ErrBackgroundClipUnsupported, "unsupported_source"},
		{media.ErrBackgroundClipUnusable, "unusable_pictures"}, {library.ErrAnalysisSourceChanged, "source_changed"},
		{library.ErrSourceChanged, "source_changed"}, {media.ErrAnalysisUnavailable, "dependencies_unavailable"},
		{os.ErrPermission, "media_directory_not_writable"}, {errors.New("private host pathname"), "generation_failed"},
	} {
		cause := test.cause
		if cause != nil {
			cause = fmt.Errorf("worker detail: %w", cause)
		}
		if got := backgroundPreviewErrorCode(cause); got != test.code {
			t.Fatalf("worker failure code = %q, want %q", got, test.code)
		}
	}
}

func TestBackgroundPreviewExecutorRequiresIndependentRuntimeAndCatalog(t *testing.T) {
	for _, app := range []*Server{nil, {}, {backgroundPreviews: &backgroundPreviewRuntime{}}, {backgroundPreviews: &backgroundPreviewRuntime{available: true}}} {
		executor := backgroundPreviewTaskExecutor{server: app}
		if executor.Available() {
			t.Fatal("background generation claimed availability without both runtime and catalog")
		}
		if err := executor.Execute(context.Background(), tasks.Work{TaskKey: library.TaskBackgroundPreviewGenerationKey}, func(tasks.Progress) error { return nil }); !errors.Is(err, tasks.ErrUnavailable) {
			t.Fatalf("unavailable executor attempted work: %v", err)
		}
	}
}
