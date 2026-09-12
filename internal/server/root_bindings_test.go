package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func rootBindingRequestForTest(query, body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/libraries/library/roots/root/binding", strings.NewReader(body))
	r.URL.RawQuery = query
	r.Header.Set("Content-Type", contentType)
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "root-binding-request-id"))
}

func assertRootBindingHTTPError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("root binding response status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string
			Message string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode root binding error: %v", err)
	}
	if body.Error.Code != code || body.Error.Message == "" || body.RequestID != "root-binding-request-id" {
		t.Fatalf("root binding error lost its code, message, or request ID: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-marker") {
		t.Fatal("root binding error exposed rejected input or an internal error detail")
	}
}

func rootBindingJSONForTest(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode root binding projection: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode root binding projection: %v", err)
	}
	return fields
}

func TestNativeRegisteredRootProjectsOnlyPublicFieldsWithExactRevision(t *testing.T) {
	root := library.RegisteredRootInfo{
		RootID: "registered-root", LibraryID: "library", Path: "/media/movies",
		AllowedPath: "/media", RelativePath: "movies", Revision: "9007199254740993",
	}
	want := map[string]any{
		"Id": "registered-root", "LibraryId": "library", "Path": "/media/movies",
		"AllowedPath": "/media", "RelativePath": "movies", "Revision": "9007199254740993",
	}
	if got := rootBindingJSONForTest(t, nativeRegisteredRoot(root)); !reflect.DeepEqual(got, want) {
		t.Fatalf("registered root projection changed its public fields or exact revision string: %#v", got)
	}
}

func TestNativeRootBindingOmitsAbsentOptionalFieldsAndKeepsStatus(t *testing.T) {
	for _, status := range []library.RootBindingStatus{
		library.RootBindingUnbound, library.RootBindingVerified, library.RootBindingMismatch, library.RootBindingUnavailable,
	} {
		t.Run(string(status), func(t *testing.T) {
			binding := library.RootBindingInfo{
				RegisteredRootInfo: library.RegisteredRootInfo{
					RootID: "root", LibraryID: "library", Path: "/media", AllowedPath: "/media",
					RelativePath: ".", Revision: "9007199254740993",
				},
				Status: status,
			}
			want := map[string]any{
				"Id": "root", "LibraryId": "library", "Path": "/media", "AllowedPath": "/media",
				"RelativePath": ".", "Revision": "9007199254740993", "Status": string(status),
			}
			if got := rootBindingJSONForTest(t, nativeRootBinding(binding)); !reflect.DeepEqual(got, want) {
				t.Fatalf("root binding projection changed its required fields or exposed absent optional fields: %#v", got)
			}
		})
	}
}

func TestNativeRootBindingProjectsSafeTopologyFieldsAndUTCTime(t *testing.T) {
	stamp := time.Date(2026, 9, 12, 12, 13, 14, 123456000, time.FixedZone("fixture", 8*60*60))
	approvedFingerprint := strings.Repeat("a", 64)
	observedFingerprint := strings.Repeat("b", 64)
	binding := library.RootBindingInfo{
		RegisteredRootInfo: library.RegisteredRootInfo{
			RootID: "root", LibraryID: "library", Path: "/media/movies", AllowedPath: "/media",
			RelativePath: "movies", Revision: "9007199254740993",
		},
		Status: library.RootBindingMismatch, ApprovedFingerprint: approvedFingerprint, ObservedFingerprint: observedFingerprint,
		Approved: &library.RootBindingTopologyInfo{
			Anchor:         library.RootBindingIdentityInfo{Profile: "anchor-profile", FilesystemUUID: "anchor-uuid", Digest: "anchor-digest"},
			RegisteredRoot: library.RootBindingIdentityInfo{Profile: "root-profile", FilesystemUUID: "root-uuid", Digest: "root-digest"},
			Boundaries: []library.RootBindingBoundaryInfo{{
				RelativePath: "series", Identity: library.RootBindingIdentityInfo{Profile: "boundary-profile", FilesystemUUID: "boundary-uuid", Digest: "boundary-digest"},
			}},
		},
		Observed: &library.RootBindingTopologyInfo{
			Anchor:         library.RootBindingIdentityInfo{Profile: "observed-anchor", FilesystemUUID: "observed-anchor-uuid", Digest: "observed-anchor-digest"},
			RegisteredRoot: library.RootBindingIdentityInfo{Profile: "observed-root", FilesystemUUID: "observed-root-uuid", Digest: "observed-root-digest"},
		},
		BoundAt: &stamp, BoundBy: "administrator",
	}
	want := map[string]any{
		"Id": "root", "LibraryId": "library", "Path": "/media/movies", "AllowedPath": "/media",
		"RelativePath": "movies", "Revision": "9007199254740993", "Status": "mismatch",
		"ApprovedFingerprint": approvedFingerprint, "ObservedFingerprint": observedFingerprint,
		"Approved": map[string]any{
			"Anchor":         map[string]any{"Profile": "anchor-profile", "FilesystemUUID": "anchor-uuid", "Digest": "anchor-digest"},
			"RegisteredRoot": map[string]any{"Profile": "root-profile", "FilesystemUUID": "root-uuid", "Digest": "root-digest"},
			"Boundaries": []any{map[string]any{
				"RelativePath": "series", "Identity": map[string]any{"Profile": "boundary-profile", "FilesystemUUID": "boundary-uuid", "Digest": "boundary-digest"},
			}},
		},
		"Observed": map[string]any{
			"Anchor":         map[string]any{"Profile": "observed-anchor", "FilesystemUUID": "observed-anchor-uuid", "Digest": "observed-anchor-digest"},
			"RegisteredRoot": map[string]any{"Profile": "observed-root", "FilesystemUUID": "observed-root-uuid", "Digest": "observed-root-digest"},
			"Boundaries":     []any{},
		},
		"BoundAt": "2026-09-12T04:13:14.123456Z", "BoundBy": "administrator",
	}
	if got := rootBindingJSONForTest(t, nativeRootBinding(binding)); !reflect.DeepEqual(got, want) {
		t.Fatalf("root binding projection changed its public topology whitelist, array semantics, or UTC timestamp: %#v", got)
	}
	if stamp.Location().String() != "fixture" || binding.BoundAt.Location().String() != "fixture" || binding.Observed.Boundaries != nil {
		t.Fatal("root binding projection mutated the source timestamp or topology")
	}
}

func TestDecodeRootBindingUpdatePreservesStringsForBackendValidation(t *testing.T) {
	for _, test := range []struct {
		name, revision, fingerprint string
	}{
		{"exact large revision", "9007199254740993", strings.Repeat("a", 64)},
		{"maximum stored revision", "9223372036854775807", strings.Repeat("b", 64)},
		{"noncanonical revision delegated", "01", strings.Repeat("a", 64)},
		{"revision range delegated", "9223372036854775808", strings.Repeat("a", 64)},
		{"fingerprint format delegated", "1", "not-a-canonical-fingerprint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(map[string]any{
				"Revision": test.revision, "ObservedFingerprint": test.fingerprint, "AcknowledgeMissingRemoval": true,
			})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			got, ok := decodeRootBindingUpdate(response, rootBindingRequestForTest("", string(encoded), "application/json; charset=utf-8"))
			if !ok || got.Revision != test.revision || got.ObservedFingerprint != test.fingerprint || !got.AcknowledgeMissingRemoval || response.Body.Len() != 0 {
				t.Fatalf("root binding decoder changed exact input strings or bypassed acknowledgement: %+v, accepted = %v", got, ok)
			}
		})
	}
}

func TestDecodeRootBindingUpdateRequiresExactNonNullFieldTypes(t *testing.T) {
	for _, field := range []string{"Revision", "ObservedFingerprint", "AcknowledgeMissingRemoval"} {
		t.Run(field, func(t *testing.T) {
			invalidValues := []any{nil, float64(1), []any{}, map[string]any{}}
			if field == "AcknowledgeMissingRemoval" {
				invalidValues = append(invalidValues, false, "true")
			} else {
				invalidValues = append(invalidValues, true, "")
			}
			for index, value := range invalidValues {
				body := map[string]any{"Revision": "1", "ObservedFingerprint": strings.Repeat("a", 64), "AcknowledgeMissingRemoval": true}
				body[field] = value
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				got, ok := decodeRootBindingUpdate(response, rootBindingRequestForTest("", string(encoded), "application/json"))
				if ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
					t.Fatalf("invalid %s value %d was accepted or retained a partial update: %+v", field, index, got)
				}
				assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
			}
			body := map[string]any{"Revision": "1", "ObservedFingerprint": strings.Repeat("a", 64), "AcknowledgeMissingRemoval": true}
			delete(body, field)
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			if _, ok := decodeRootBindingUpdate(response, rootBindingRequestForTest("", string(encoded), "application/json")); ok {
				t.Fatalf("root binding update accepted a missing %s field", field)
			}
			assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
		})
	}
}

func TestDecodeRootBindingUpdateRejectsAmbiguousAndInvalidJSON(t *testing.T) {
	valid := `{"Revision":"1","ObservedFingerprint":"fingerprint","AcknowledgeMissingRemoval":true}`
	for _, test := range []struct {
		name, body string
	}{
		{"empty", ""}, {"null", "null"}, {"array", "[]"}, {"boolean", "true"}, {"string", `"object"`}, {"missing all fields", "{}"},
		{"trailing object", valid + "{}"}, {"trailing null", valid + " null"}, {"trailing text", valid + " text"},
		{"incomplete object", "{"}, {"missing value", `{"Revision":}`}, {"trailing comma", strings.TrimSuffix(valid, "}") + ",}"},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"FileHandle":"private-marker"}`},
		{"lowercase revision", strings.Replace(valid, `"Revision"`, `"revision"`, 1)},
		{"lowercase fingerprint", strings.Replace(valid, `"ObservedFingerprint"`, `"observedFingerprint"`, 1)},
		{"lowercase acknowledgement", strings.Replace(valid, `"AcknowledgeMissingRemoval"`, `"acknowledgeMissingRemoval"`, 1)},
		{"duplicate revision", strings.TrimSuffix(valid, "}") + `,"Revision":"2"}`},
		{"duplicate fingerprint", strings.TrimSuffix(valid, "}") + `,"ObservedFingerprint":"private-marker"}`},
		{"duplicate acknowledgement", strings.TrimSuffix(valid, "}") + `,"AcknowledgeMissingRemoval":false}`},
		{"escaped duplicate revision", strings.TrimSuffix(valid, "}") + `,"\u0052evision":"2"}`},
		{"invalid UTF-8 key", strings.TrimSuffix(valid, "}") + ",\"\xff\":true}"},
		{"invalid UTF-8 value", strings.Replace(valid, "fingerprint", "\xff", 1)},
		{"UTF-8 BOM", "\xef\xbb\xbf" + valid},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			got, ok := decodeRootBindingUpdate(response, rootBindingRequestForTest("", test.body, "application/json"))
			if ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
				t.Fatalf("invalid root binding body was accepted or retained a partial update: %+v", got)
			}
			assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
		})
	}
}

func TestDecodeRootBindingUpdateEnforcesActualFourKiBBodyLimit(t *testing.T) {
	valid := `{"Revision":"1","ObservedFingerprint":"fingerprint","AcknowledgeMissingRemoval":true}`
	for _, size := range []int{len(valid), 4096, 4097} {
		for _, contentLength := range []int64{-1, 0, 2, int64(size)} {
			t.Run(fmt.Sprintf("size_%d_length_%d", size, contentLength), func(t *testing.T) {
				request := rootBindingRequestForTest("", valid+strings.Repeat(" ", size-len(valid)), "application/json")
				request.ContentLength = contentLength
				response := httptest.NewRecorder()
				got, ok := decodeRootBindingUpdate(response, request)
				if size > 4096 {
					if ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
						t.Fatal("oversized root binding body bypassed the actual byte limit")
					}
					assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
					return
				}
				if !ok || got.Revision != "1" || got.ObservedFingerprint != "fingerprint" || !got.AcknowledgeMissingRemoval || response.Body.Len() != 0 {
					t.Fatal("valid root binding body at or below 4 KiB was rejected based on its length hint")
				}
			})
		}
	}
}

type rootBindingReadFailureForTest struct{}

func (rootBindingReadFailureForTest) Read([]byte) (int, error) {
	return 0, fmt.Errorf("private-marker: %w", io.ErrUnexpectedEOF)
}

func TestDecodeRootBindingUpdateRejectsBodyReadFailure(t *testing.T) {
	request := rootBindingRequestForTest("", "", "application/json")
	request.Body = io.NopCloser(rootBindingReadFailureForTest{})
	response := httptest.NewRecorder()
	if got, ok := decodeRootBindingUpdate(response, request); ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
		t.Fatalf("failed body read produced a root binding update: %+v", got)
	}
	assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
}

func TestDecodeRootBindingUpdateRequiresJSONMediaType(t *testing.T) {
	valid := `{"Revision":"1","ObservedFingerprint":"fingerprint","AcknowledgeMissingRemoval":true}`
	for _, contentType := range []string{
		"", "text/plain", "application/octet-stream", "application/x-www-form-urlencoded", "application/problem+json",
		"application/json; broken", "application/json, text/plain",
	} {
		t.Run(contentType, func(t *testing.T) {
			response := httptest.NewRecorder()
			if got, ok := decodeRootBindingUpdate(response, rootBindingRequestForTest("", valid, contentType)); ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
				t.Fatalf("unsupported content type produced a root binding update: %+v", got)
			}
			assertRootBindingHTTPError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type")
		})
	}
}

func TestRootBindingRejectsAllQueryParameters(t *testing.T) {
	valid := `{"Revision":"1","ObservedFingerprint":"fingerprint","AcknowledgeMissingRemoval":true}`
	response := httptest.NewRecorder()
	if !rootBindingNoQuery(response, rootBindingRequestForTest("", "", "")) || response.Body.Len() != 0 {
		t.Fatal("root binding query guard rejected a request without a query")
	}
	for _, query := range []string{
		"Revision=1", "ObservedFingerprint=private-marker", "AcknowledgeMissingRemoval=true",
		"api_key=private-marker", "unknown=", "Revision=1&Revision=2", "%zz", "a;b", "&",
	} {
		for _, decode := range []bool{false, true} {
			request := rootBindingRequestForTest(query, valid, "application/json")
			response := httptest.NewRecorder()
			if decode {
				if got, ok := decodeRootBindingUpdate(response, request); ok || !reflect.DeepEqual(got, library.RootBindingUpdate{}) {
					t.Fatalf("root binding update accepted an undeclared query: %+v", got)
				}
			} else if rootBindingNoQuery(response, request) {
				t.Fatal("root binding query guard accepted an undeclared query")
			}
			assertRootBindingHTTPError(t, response, http.StatusBadRequest, "invalid_input")
		}
	}
}

func TestRootBindingConflictIsMappedAndSanitizedWhenWrapped(t *testing.T) {
	for _, err := range []error{
		library.ErrRootBindingConflict,
		fmt.Errorf("private-marker storage document and handle: %w", library.ErrRootBindingConflict),
	} {
		response := httptest.NewRecorder()
		(&Server{}).rootBindingError(response, rootBindingRequestForTest("", "", ""), err)
		assertRootBindingHTTPError(t, response, http.StatusConflict, "root_binding_conflict")
	}
}
