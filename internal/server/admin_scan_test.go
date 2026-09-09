package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func adminScanRequestForTest(query, body, contentType string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/libraries/library-id/scan", strings.NewReader(body))
	request.URL.RawQuery = query
	request.SetPathValue("id", "library-id")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request.WithContext(context.WithValue(request.Context(), requestIDKey, "scan-request-id"))
}

func expectAdminScanInputError(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	code := "invalid_input"
	if status == http.StatusUnsupportedMediaType {
		code = "unsupported_media_type"
	}
	expectAPIError(t, response, status, code, false)
	if jsonObject(t, response)["RequestId"] != "scan-request-id" {
		t.Fatal("scan input error lost its native request identifier")
	}
}

func TestDecodeScanOptionsAcceptsLegacyEmptyBodiesAndExplicitModes(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType string
		forceProbe              bool
	}{
		{name: "legacy empty"},
		{name: "empty JSON", contentType: "application/json"},
		{name: "empty text", contentType: "text/plain"},
		{name: "empty malformed MIME", contentType: "application/json; broken"},
		{name: "empty object", body: `{}`, contentType: "application/json"},
		{name: "explicit cached mode", body: `{"ForceProbe":false}`, contentType: "application/json"},
		{name: "explicit forced mode", body: `{"ForceProbe":true}`, contentType: "application/json", forceProbe: true},
		{name: "JSON whitespace", body: " \r\n{\"ForceProbe\": true}\t\n", contentType: "application/json; charset=utf-8", forceProbe: true},
		{name: "escaped canonical key", body: `{"\u0046orceProbe":true}`, contentType: "application/json", forceProbe: true},
		{name: "exact byte limit", body: `{}` + strings.Repeat(" ", 4094), contentType: "application/json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			options, ok := decodeScanOptions(response, adminScanRequestForTest("", test.body, test.contentType))
			if !ok || options.ForceProbe != test.forceProbe || response.Body.Len() != 0 {
				t.Fatalf("valid scan options changed or were rejected: options = %+v, accepted = %v, response = %s", options, ok, response.Body.String())
			}
		})
	}
}

func TestDecodeScanOptionsRejectsMalformedAndAmbiguousJSON(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"whitespace only", " \r\n\t"},
		{"null root", `null`},
		{"array root", `[]`},
		{"boolean root", `true`},
		{"string root", `"ForceProbe"`},
		{"unknown field", `{"Mode":"secret-rejected-value"}`},
		{"wrong case", `{"forceProbe":true}`},
		{"all lower case", `{"forceprobe":true}`},
		{"null boolean", `{"ForceProbe":null}`},
		{"string boolean", `{"ForceProbe":"true"}`},
		{"numeric boolean", `{"ForceProbe":1}`},
		{"object boolean", `{"ForceProbe":{}}`},
		{"array boolean", `{"ForceProbe":[]}`},
		{"duplicate boolean", `{"ForceProbe":true,"ForceProbe":false}`},
		{"duplicate escaped key", `{"ForceProbe":true,"\u0046orceProbe":true}`},
		{"additional field", `{"ForceProbe":true,"Other":false}`},
		{"trailing object", `{"ForceProbe":true}{}`},
		{"trailing null", `{} null`},
		{"trailing text", `{} trailing`},
		{"missing value", `{"ForceProbe":}`},
		{"missing closing brace", `{"ForceProbe":true`},
		{"trailing comma", `{"ForceProbe":true,}`},
		{"invalid UTF-8", "{\"ForceProbe\":true,\"\xff\":false}"},
		{"UTF-8 BOM", "\xef\xbb\xbf{}"},
		{"over byte limit", `{}` + strings.Repeat(" ", 4095)},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			options, ok := decodeScanOptions(response, adminScanRequestForTest("", test.body, "application/json"))
			if ok || options.ForceProbe {
				t.Fatalf("invalid scan body was accepted or retained forced mode: %+v", options)
			}
			expectAdminScanInputError(t, response, http.StatusBadRequest)
			if strings.Contains(response.Body.String(), "secret-rejected-value") {
				t.Fatal("scan error reflected a rejected input value")
			}
		})
	}
}

func TestDecodeScanOptionsRejectsNonJSONMediaTypesForNonemptyBodies(t *testing.T) {
	for _, contentType := range []string{"", "text/plain", "application/octet-stream", "application/problem+json", "application/json; broken"} {
		t.Run(contentType, func(t *testing.T) {
			response := httptest.NewRecorder()
			if _, ok := decodeScanOptions(response, adminScanRequestForTest("", `{"ForceProbe":true}`, contentType)); ok {
				t.Fatal("nonempty scan body bypassed the JSON media type requirement")
			}
			expectAdminScanInputError(t, response, http.StatusUnsupportedMediaType)
		})
	}
}

func TestDecodeScanOptionsRejectsAllQueryParameters(t *testing.T) {
	for _, query := range []string{"ForceProbe=true", "ForceProbe=false", "forceprobe=true", "Unused=", "api_key=secret-rejected-value", "ForceProbe=true&ForceProbe=false", "%zz", "a;b", "&"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			if _, ok := decodeScanOptions(response, adminScanRequestForTest(query, "", "")); ok {
				t.Fatal("scan endpoint accepted an undeclared query parameter")
			}
			expectAdminScanInputError(t, response, http.StatusBadRequest)
			if strings.Contains(response.Body.String(), "secret-rejected-value") {
				t.Fatal("scan error reflected query credentials")
			}
		})
	}
}

func TestDecodeScanOptionsReadsActualBodyRegardlessOfContentLength(t *testing.T) {
	for _, test := range []struct {
		name          string
		body          string
		contentLength int64
		wantStatus    int
		forceProbe    bool
	}{
		{name: "unknown length empty", contentLength: -1},
		{name: "claimed nonempty but actual empty", contentLength: 100},
		{name: "unknown length forced mode", body: `{"ForceProbe":true}`, contentLength: -1, forceProbe: true},
		{name: "zero length hint with body", body: `{"ForceProbe":true}`, forceProbe: true},
		{name: "unknown length over limit", body: `{}` + strings.Repeat(" ", 4095), contentLength: -1, wantStatus: http.StatusBadRequest},
		{name: "short length hint over limit", body: `{}` + strings.Repeat(" ", 4095), contentLength: 2, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := adminScanRequestForTest("", test.body, "application/json")
			request.ContentLength = test.contentLength
			response := httptest.NewRecorder()
			options, ok := decodeScanOptions(response, request)
			if test.wantStatus != 0 {
				if ok {
					t.Fatal("oversized actual scan body was accepted")
				}
				expectAdminScanInputError(t, response, test.wantStatus)
				return
			}
			if !ok || options.ForceProbe != test.forceProbe {
				t.Fatalf("scan parser trusted the length hint instead of the actual body: %+v, accepted = %v", options, ok)
			}
		})
	}
}

type adminScanErrorReader struct{}

func (adminScanErrorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestDecodeScanOptionsRejectsBodyReadFailure(t *testing.T) {
	request := adminScanRequestForTest("", "", "application/json")
	request.Body = io.NopCloser(adminScanErrorReader{})
	response := httptest.NewRecorder()
	if _, ok := decodeScanOptions(response, request); ok {
		t.Fatal("failed body read was treated as a legacy empty request")
	}
	expectAdminScanInputError(t, response, http.StatusBadRequest)
}

func TestJobDTOAlwaysExposesBooleanForceProbe(t *testing.T) {
	for _, forceProbe := range []bool{false, true} {
		response := httptest.NewRecorder()
		jsonResponse(response, http.StatusAccepted, map[string]any{"Job": jobDTO(library.Job{ID: "scan-job", Status: "Queued", ForceProbe: forceProbe})})
		job := objectValue(t, jsonObject(t, response), "Job")
		got, ok := job["ForceProbe"].(bool)
		if !ok || got != forceProbe || job["Status"] != "pending" {
			t.Fatalf("job response lost its boolean scan mode or pending status: %#v", job)
		}
	}
}
