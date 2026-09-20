package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func mediaOperationInputRequest(method, body string) (*httptest.ResponseRecorder, *http.Request) {
	r := httptest.NewRequest(method, "/admin/v1/media-operations/"+strings.Repeat("a", 32), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return httptest.NewRecorder(), r
}

func TestAdminMediaOperationStartRejectsAmbiguousAndExecutableInput(t *testing.T) {
	id := strings.Repeat("a", 32)
	valid := `{"RequestId":"prepare-one","Kind":"remove_embedded_subtitle","MediaSourceId":"` + media.SourceID(id) + `","SourceRevision":"source-one","StreamIndex":3,"Parameters":{"Profile":"matroska-v1"}}`
	w, r := mediaOperationInputRequest(http.MethodPost, valid)
	input, ok := decodeAdminMediaOperationStart(w, r, id)
	if !ok || input.ItemID != id || input.StreamIndex != 3 || input.Parameters.Profile != "matroska-v1" || input.MaxQueued != 0 || len(input.ExecutionSnapshot) != 0 {
		t.Fatal("strict admission lost its explicit selection or accepted a private runtime value")
	}
	for name, body := range map[string]string{
		"duplicate":           strings.Replace(valid, `"StreamIndex":3`, `"StreamIndex":3,"StreamIndex":4`, 1),
		"wrong-case":          strings.Replace(valid, `"StreamIndex"`, `"streamIndex"`, 1),
		"missing-selection":   strings.Replace(valid, `"StreamIndex":3,`, ``, 1),
		"null-selection":      strings.Replace(valid, `"StreamIndex":3`, `"StreamIndex":null`, 1),
		"fraction":            strings.Replace(valid, `"StreamIndex":3`, `"StreamIndex":3.5`, 1),
		"negative":            strings.Replace(valid, `"StreamIndex":3`, `"StreamIndex":-1`, 1),
		"source-mismatch":     strings.Replace(valid, media.SourceID(id), "other-source", 1),
		"unknown-kind":        strings.Replace(valid, "remove_embedded_subtitle", "run_command", 1),
		"private-snapshot":    strings.TrimSuffix(valid, "}") + `,"ExecutionSnapshot":{"Path":"/bin/sh"}}`,
		"path":                strings.Replace(valid, `"Profile":"matroska-v1"`, `"Profile":"matroska-v1","Path":"/private/input"`, 1),
		"url":                 strings.Replace(valid, `"Profile":"matroska-v1"`, `"Profile":"matroska-v1","URL":"https://example.invalid/"`, 1),
		"duplicate-parameter": strings.Replace(valid, `"Profile":"matroska-v1"`, `"Profile":"matroska-v1","Profile":"mp4-movtext-v1"`, 1),
		"null-parameter":      strings.Replace(valid, `{"Profile":"matroska-v1"}`, `null`, 1),
		"lossy-surrogate":     strings.Replace(valid, `"prepare-one"`, `"\ud800"`, 1),
		"trailing":            valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			w, r := mediaOperationInputRequest(http.MethodPost, body)
			if _, ok := decodeAdminMediaOperationStart(w, r, id); ok || w.Code != http.StatusBadRequest {
				t.Fatalf("unsafe admission was accepted: status %d", w.Code)
			}
		})
	}
	ocr := `{"RequestId":"ocr-one","Kind":"subtitle_ocr","MediaSourceId":"` + media.SourceID(id) + `","SourceRevision":"source-one","StreamIndex":3,"Parameters":{"ModelIds":["eng","chi_sim"],"OutputFormat":"vtt","Language":"zh-CN","Title":"Reviewed subtitles","IsDefault":false,"IsForced":true,"IsHearingImpaired":false}}`
	w, r = mediaOperationInputRequest(http.MethodPost, ocr)
	parsed, ok := decodeAdminMediaOperationStart(w, r, id)
	if !ok || len(parsed.Parameters.ModelIDs) != 2 || !parsed.Parameters.IsForced {
		t.Fatal("configured model identifiers or subtitle output attributes were rejected")
	}
	for _, body := range []string{
		strings.Replace(ocr, `["eng","chi_sim"]`, `null`, 1),
		strings.Replace(ocr, `["eng","chi_sim"]`, `["eng","eng"]`, 1),
		strings.Replace(ocr, `["eng","chi_sim"]`, `["../eng"]`, 1),
		strings.Replace(ocr, `["eng","chi_sim"]`, `["english-v1"]`, 1),
		strings.Replace(ocr, `"Language":"zh-CN"`, `"Language":"../eng"`, 1),
		strings.Replace(ocr, `"OutputFormat":"vtt"`, `"OutputFormat":"ass"`, 1),
		strings.Replace(ocr, `"IsDefault":false`, `"IsDefault":null`, 1),
	} {
		w, r := mediaOperationInputRequest(http.MethodPost, body)
		if _, ok := decodeAdminMediaOperationStart(w, r, id); ok || w.Code != http.StatusBadRequest {
			t.Fatal("OCR admitted ambiguous or unsupported model/output input")
		}
	}
}

func TestAdminMediaOperationReviewPreservesExactTicksAndRejectsAmbiguity(t *testing.T) {
	valid := `{"Revision":"9007199254740993","Edits":[{"Ordinal":0,"StartTicks":"0","EndTicks":"9007199254740993","Text":"Hello\n\u4e2d\u6587","Included":true}]}`
	w, r := mediaOperationInputRequest(http.MethodPut, valid)
	revision, edits, ok := decodeAdminMediaOperationReview(w, r)
	if !ok || revision != 9007199254740993 || len(edits) != 1 || edits[0].EndTicks != revision || edits[0].Text != "Hello\n\u4e2d\u6587" {
		t.Fatal("review lost exact revision, ticks, or multiline text")
	}
	for _, body := range []string{
		strings.Replace(valid, `"Revision":"9007199254740993"`, `"Revision":9007199254740993`, 1),
		strings.Replace(valid, `"Revision":"9007199254740993"`, `"Revision":"01"`, 1),
		strings.Replace(valid, `"StartTicks":"0"`, `"StartTicks":0`, 1),
		strings.Replace(valid, `"StartTicks":"0"`, `"StartTicks":"-0"`, 1),
		strings.Replace(valid, `"EndTicks":"9007199254740993"`, `"EndTicks":"9223372036854775808"`, 1),
		strings.Replace(valid, `"EndTicks":"9007199254740993"`, `"EndTicks":"0"`, 1),
		strings.Replace(valid, `"Ordinal":0`, `"Ordinal":0,"Ordinal":1`, 1),
		strings.Replace(valid, `"Included":true`, `"Included":null`, 1),
		strings.Replace(valid, `"Hello\n\u4e2d\u6587"`, `"\udc00"`, 1),
		strings.Replace(valid, `"Hello\n\u4e2d\u6587"`, `"\t"`, 1),
		strings.Replace(valid, `"Hello\n\u4e2d\u6587"`, `""`, 1),
		`{"Revision":"1","Edits":[]}`,
		`{"Revision":"1","Edits":null}`,
		`{"Revision":"1","Edits":[{"Ordinal":0,"StartTicks":"0","EndTicks":"1","Text":"a","Included":true},{"Ordinal":0,"StartTicks":"0","EndTicks":"1","Text":"b","Included":true}]}`,
	} {
		w, r := mediaOperationInputRequest(http.MethodPut, body)
		if _, _, ok := decodeAdminMediaOperationReview(w, r); ok || w.Code != http.StatusBadRequest {
			t.Fatal("review accepted ambiguous or lossy cue edits")
		}
	}
}

func TestAdminMediaOperationApplyRequiresExactConfirmationReceipt(t *testing.T) {
	valid := `{"Revision":"9007199254740993","SourceRevision":"source-one","ResultHash":"` + strings.Repeat("a", 64) + `","RequestId":"apply-one"}`
	w, r := mediaOperationInputRequest(http.MethodPost, valid)
	parsed, ok := decodeAdminMediaOperationApply(w, r)
	if !ok || parsed.Revision != 9007199254740993 || parsed.RequestID != "apply-one" {
		t.Fatal("apply confirmation lost the exact reviewed result")
	}
	for _, body := range []string{
		strings.Replace(valid, `"Revision":"9007199254740993"`, `"Revision":1`, 1),
		strings.Replace(valid, `"SourceRevision":"source-one",`, ``, 1),
		strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		strings.Replace(valid, `"apply-one"`, `""`, 1),
		strings.TrimSuffix(valid, "}") + `,"Force":true}`,
	} {
		w, r := mediaOperationInputRequest(http.MethodPost, body)
		if _, ok := decodeAdminMediaOperationApply(w, r); ok || w.Code != http.StatusBadRequest {
			t.Fatal("apply admitted incomplete or bypass confirmation")
		}
	}
}

func TestAdminMediaOperationTransportRejectsUnboundedOrUnknownInputs(t *testing.T) {
	for _, test := range []struct {
		query, mime, body string
		status            int
	}{
		{"?", "application/json", `{"Revision":"1"}`, 400},
		{"?Revision=2", "application/json", `{"Revision":"1"}`, 400},
		{"", "text/plain", `{"Revision":"1"}`, 415},
		{"", "application/json", `{"Revision":"1","Path":"/private"}`, 400},
		{"", "application/json", `{"Revision":"` + strings.Repeat("a", maxAdminMediaOperationBodyBytes) + `"}`, 400},
	} {
		w, r := mediaOperationInputRequest(http.MethodPost, test.body)
		r.URL.RawQuery = strings.TrimPrefix(test.query, "?")
		r.URL.ForceQuery = test.query == "?"
		r.Header.Set("Content-Type", test.mime)
		if _, ok := decodeAdminMediaOperationCancel(w, r); ok || w.Code != test.status {
			t.Fatalf("unexpected transport validation status %d, want %d", w.Code, test.status)
		}
	}
	for _, query := range []string{"?", "?Limit=0", "?Limit=101", "?Limit=01", "?Limit=1&Limit=2", "?StartIndex=2147483648", "?Path=%2fprivate", "?State=unknown", "?ItemId=../private", "?Limit=%zz"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/admin/v1/media-operations"+query, nil)
		if _, ok := adminMediaOperationPage(w, r, true); ok || w.Code != http.StatusBadRequest {
			t.Fatal("operation pagination accepted unknown, ambiguous or unbounded query")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/media-operations?Kind=subtitle_ocr", nil)
	if _, ok := adminMediaOperationPage(w, r, false); ok {
		t.Fatal("cue pagination accepted operation filters")
	}
}

func TestAdminMediaOperationDTOExcludesPrivateExecutionAndPreservesExactValues(t *testing.T) {
	secret := "private-marker"
	operation := library.MediaOperation{ID: strings.Repeat("a", 32), Kind: library.MediaOperationRemoveSubtitle,
		Revision: 9007199254740993, RootID: secret, SourceSnapshot: json.RawMessage(`{"Path":"private-marker"}`),
		ExecutionSnapshot: json.RawMessage(`{"Executable":"private-marker"}`), Journal: json.RawMessage(`{"Credential":"private-marker"}`),
		WorkerToken: secret, RequestID: secret, RequestFingerprint: []byte(secret),
		ResultSummary: json.RawMessage(`{"RemovedStreamIndex":0,"PreservedStreamCount":2,"OriginalBytes":9007199254740993,"CandidateBytes":9007199254740992,"BackupRetained":true,"Path":"private-marker","Journal":{"WorkerToken":"private-marker"},"Warnings":["private-marker"]}`)}
	encoded, err := json.Marshal(mediaOperationDTO(operation))
	if err != nil || strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), `"Revision":"9007199254740993"`) || !strings.Contains(string(encoded), `"OriginalBytes":"9007199254740993"`) || !strings.Contains(string(encoded), `"RemovedStreamIndex":0`) {
		t.Fatal("operation DTO exposed private evidence or rounded confirmation values")
	}
	target := mediaProcessingTargetDTO(library.MediaOperationTarget{Streams: []media.Stream{{Index: 3, Filename: secret, SubtitleTag: secret, Codec: "hdmv_pgs_subtitle"}}}, adminMediaOperationCapabilities{})
	encoded, err = json.Marshal(target)
	if err != nil || strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), `"Codec":"hdmv_pgs_subtitle"`) {
		t.Fatal("target projection leaked private fields or lost stream selection")
	}
	cue := library.MediaOperationCue{StartTicks: 9007199254740993, EndTicks: 9007199254740994, ImagePNG: []byte(secret)}
	encoded, err = json.Marshal(cue)
	if err != nil || strings.Contains(string(encoded), "ImagePNG") || !strings.Contains(string(encoded), `"StartTicks":"9007199254740993"`) {
		t.Fatal("cue projection lost tick precision or embedded image bytes")
	}
}

func TestAdminMediaOperationErrorsAndResponseBudgetAreSafe(t *testing.T) {
	s := &Server{}
	for _, test := range []struct {
		cause  error
		status int
		code   string
	}{
		{library.ErrMediaOperationConflict, 409, "media_operation_conflict"},
		{library.ErrMediaOperationState, 409, "media_operation_state"},
		{library.ErrSourceChanged, 409, "source_changed"},
		{library.ErrMediaOperationRecovery, 409, "recovery_required"},
		{library.ErrBusy, 409, "media_operation_busy"},
		{library.ErrUnavailable, 503, "media_operation_unavailable"},
		{errors.New("private-path-and-secret"), 500, "internal_error"},
	} {
		w, r := mediaOperationInputRequest(http.MethodGet, "")
		s.mediaOperationError(w, r, test.cause)
		if w.Code != test.status || !strings.Contains(w.Body.String(), `"Code":"`+test.code+`"`) || strings.Contains(w.Body.String(), "private-path-and-secret") {
			t.Fatalf("unsafe or incorrect error response %d", w.Code)
		}
	}
	w, r := mediaOperationInputRequest(http.MethodGet, "")
	adminMediaOperationResponse(w, r, http.StatusOK, map[string]string{"Text": strings.Repeat("a", maxAdminMediaOperationResponseBytes)})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"Code":"response_limit"`) || w.Body.Len() > 1024 {
		t.Fatal("oversized response escaped its output budget")
	}
}
