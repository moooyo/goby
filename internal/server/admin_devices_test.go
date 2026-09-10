package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func adminDeviceRequestForTest(query, body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/devices/2/options", strings.NewReader(body))
	r.URL.RawQuery = query
	r.SetPathValue("id", "2")
	r.Header.Set("Content-Type", contentType)
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "device-request-id"))
}

func assertAdminDeviceInputError(t *testing.T, response *httptest.ResponseRecorder, status int, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("device validation status = %d, want %d", response.Code, status)
	}
	var body struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode native device error: %v", err)
	}
	code := "invalid_input"
	if status == http.StatusUnsupportedMediaType {
		code = "unsupported_media_type"
	}
	if body.Error.Code != code || body.RequestID != "device-request-id" || field != "" && body.Error.Fields[field] == "" {
		t.Fatal("native device error lost its code, request ID, or field details")
	}
}

func TestParseAdminDeviceQueryCanonicalBoundsAndLiteralSearch(t *testing.T) {
	for _, test := range []struct {
		query, search string
		start, limit  int
	}{
		{"", "", 0, 50}, {"SearchTerm=", "", 0, 50},
		{"SearchTerm=Caf%C3%A9%25_&StartIndex=0&Limit=200", "Caf\u00e9%_", 0, 200},
		{"StartIndex=2147483647&Limit=1", "", 2147483647, 1},
		{"SearchTerm=" + url.QueryEscape(strings.Repeat("\u00e9", 128)), strings.Repeat("\u00e9", 128), 0, 50},
	} {
		r := adminDeviceRequestForTest(test.query, "", "")
		filter, err := parseAdminDeviceQuery(r)
		if err != nil || filter.SearchTerm != test.search || filter.StartIndex != test.start || filter.Limit != test.limit || r.URL.RawQuery != test.query {
			t.Fatalf("canonical native device query changed or failed: %v", err)
		}
	}
	for _, query := range []string{
		"searchterm=a", "UserId=other", "Kind=emby", "SortOrder=Ascending", "api_key=private-marker",
		"SearchTerm=a&SearchTerm=a", "Limit=1&Limit=1", "StartIndex=0&StartIndex=0",
		"SearchTerm=%zz", "SearchTerm=%ff", "SearchTerm=a;b", "SearchTerm=a%00b", "SearchTerm=a%0Ab", "SearchTerm=a%C2%85b",
		"SearchTerm=" + url.QueryEscape(strings.Repeat("\u00e9", 129)),
		"StartIndex=-1", "StartIndex=01", "StartIndex=%2B1", "StartIndex=2147483648", "StartIndex=", "StartIndex=1.0", "StartIndex=1e2",
		"Limit=0", "Limit=201", "Limit=01", "Limit=%201", "Limit=", "Limit=1.0",
	} {
		_, err := parseAdminDeviceQuery(adminDeviceRequestForTest(query, "", ""))
		if !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatalf("invalid native device query was accepted: %s", query)
		}
	}
}

func TestAdminDeviceIDPreservesDecimalPrecisionAndRejectsReportedIdentifiers(t *testing.T) {
	for _, id := range []string{"1", "2", "9007199254740993", "9223372036854775807"} {
		r := adminDeviceRequestForTest("", "", "")
		r.SetPathValue("id", id)
		response := httptest.NewRecorder()
		got, ok := adminDeviceID(response, r)
		if !ok || strconv.FormatInt(got, 10) != id || response.Body.Len() != 0 {
			t.Fatal("canonical native device identifier lost decimal precision")
		}
	}
	for _, id := range []string{"", "0", "-1", "+1", "01", "1.0", "1e3", " 1", "1 ", "device-reported-id", "9223372036854775808", "1\x00", "1\xff"} {
		r := adminDeviceRequestForTest("", "", "")
		r.SetPathValue("id", id)
		response := httptest.NewRecorder()
		if _, ok := adminDeviceID(response, r); ok {
			t.Fatal("native device path accepted a noncanonical registry identifier")
		}
		assertAdminDeviceInputError(t, response, http.StatusBadRequest, "Id")
	}
}

func TestDecodeAdminDeviceOptionsAndDeleteExactContract(t *testing.T) {
	for _, revision := range []string{"1", "9007199254740993", "9223372036854775807"} {
		for _, name := range []string{"", "  Living Room  ", strings.Repeat("\u00e9", 128)} {
			encoded, err := json.Marshal(map[string]string{"Revision": revision, "CustomName": name})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			gotRevision, gotName, ok := decodeAdminDeviceOptions(response, adminDeviceRequestForTest("", string(encoded), "application/json; charset=utf-8"))
			if !ok || strconv.FormatInt(gotRevision, 10) != revision || gotName != strings.TrimSpace(name) || response.Body.Len() != 0 {
				t.Fatal("native device options changed revision precision or name normalization")
			}
		}
		response := httptest.NewRecorder()
		got, ok := decodeAdminDeviceDelete(response, adminDeviceRequestForTest("", `{"Revision":"`+revision+`"}`, "application/json"))
		if !ok || strconv.FormatInt(got, 10) != revision || response.Body.Len() != 0 {
			t.Fatal("native device delete did not preserve its revision string")
		}
	}
	for _, revision := range []string{`null`, `1`, `true`, `"0"`, `"01"`, `"+1"`, `"-1"`, `"1.0"`, `"1e3"`, `"9223372036854775808"`} {
		for _, options := range []bool{true, false} {
			body := `{"Revision":` + revision
			if options {
				body += `,"CustomName":"Living Room"`
			}
			body += `}`
			response := httptest.NewRecorder()
			if options {
				if _, _, ok := decodeAdminDeviceOptions(response, adminDeviceRequestForTest("", body, "application/json")); ok {
					t.Fatal("native device rename accepted an invalid revision")
				}
			} else if _, ok := decodeAdminDeviceDelete(response, adminDeviceRequestForTest("", body, "application/json")); ok {
				t.Fatal("native device delete accepted an invalid revision")
			}
			assertAdminDeviceInputError(t, response, http.StatusBadRequest, "Revision")
		}
	}
	for _, raw := range []string{`null`, `1`, `false`, `"a\u0000b"`, `"a\nb"`, `"a\u0085b"`, strconv.Quote(strings.Repeat("\u00e9", 129))} {
		response := httptest.NewRecorder()
		if _, _, ok := decodeAdminDeviceOptions(response, adminDeviceRequestForTest("", `{"Revision":"1","CustomName":`+raw+`}`, "application/json")); ok {
			t.Fatal("native device rename accepted an invalid custom name")
		}
		assertAdminDeviceInputError(t, response, http.StatusBadRequest, "CustomName")
	}
}

func TestDecodeAdminDeviceBodiesRejectUnknownMissingDuplicateAndOversizedInput(t *testing.T) {
	for _, options := range []bool{true, false} {
		valid := `{"Revision":"1"}`
		if options {
			valid = `{"Revision":"1","CustomName":"Room"}`
		}
		decode := func(response *httptest.ResponseRecorder, r *http.Request) bool {
			if options {
				_, _, ok := decodeAdminDeviceOptions(response, r)
				return ok
			}
			_, ok := decodeAdminDeviceDelete(response, r)
			return ok
		}
		for _, body := range []string{"", "null", "[]", "{}", valid + "{}", valid + "null", "{", `{,"Revision":"1"}`,
			`{"revision":"1","CustomName":"Room"}`, `{"Revision":"1","Revision":"2","CustomName":"Room"}`,
			`{"Revision":"1","\u0052evision":"2","CustomName":"Room"}`, `{"Revision":"1","Id":"private-marker"}`,
			`{"Revision":"1","CustomName":"One","CustomName":"Two"}`, "{\"Revision\":\"1\",\"\xff\":0}", valid + strings.Repeat(" ", 4097-len(valid))} {
			response := httptest.NewRecorder()
			if decode(response, adminDeviceRequestForTest("", body, "application/json")) {
				t.Fatal("native device mutation accepted an ambiguous, incomplete, or oversized body")
			}
			assertAdminDeviceInputError(t, response, http.StatusBadRequest, "")
			if strings.Contains(response.Body.String(), "private-marker") {
				t.Fatal("native device validation reflected a rejected protected value")
			}
		}
		response := httptest.NewRecorder()
		if !decode(response, adminDeviceRequestForTest("", valid+strings.Repeat(" ", 4096-len(valid)), "application/json")) {
			t.Fatal("native device mutation rejected a valid object at exactly 4 KiB")
		}
		for _, query := range []string{"Id=2", "Revision=1", "api_key=private-marker", "%zz"} {
			response := httptest.NewRecorder()
			if decode(response, adminDeviceRequestForTest(query, valid, "application/json")) {
				t.Fatal("native device mutation accepted an undeclared query")
			}
			assertAdminDeviceInputError(t, response, http.StatusBadRequest, "Query")
		}
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/json; invalid"} {
			response := httptest.NewRecorder()
			if decode(response, adminDeviceRequestForTest("", valid, contentType)) {
				t.Fatal("native device mutation accepted an unsupported content type")
			}
			assertAdminDeviceInputError(t, response, http.StatusUnsupportedMediaType, "")
		}
	}
}

func TestNativeManagedDeviceProjectionPreservesNullableFieldsPrecisionAndUTC(t *testing.T) {
	stamp := time.Date(2026, 9, 10, 12, 13, 14, 123456000, time.FixedZone("fixture", 8*60*60))
	custom, userID, userName := "Custom Device", "device-user", "Device User"
	for _, named := range []bool{false, true} {
		device := identity.ManagedDevice{ID: 9007199254740993, Revision: 9223372036854775807, ReportedDeviceID: "reported-device", Name: "Reported Device", ReportedName: "Reported Device", AppName: "Device App", AppVersion: "1.2.3", CreatedAt: stamp, LastSeenAt: stamp.Add(time.Minute), IPAddress: "192.0.2.7", ActiveLoginCount: 2}
		want := map[string]any{"Id": "9007199254740993", "Revision": "9223372036854775807", "ReportedDeviceId": "reported-device", "Name": "Reported Device", "ReportedName": "Reported Device", "CustomName": nil, "AppName": "Device App", "AppVersion": "1.2.3", "LastUserId": nil, "LastUserName": nil, "CreatedAt": "2026-09-10T04:13:14.123456Z", "LastSeenAt": "2026-09-10T04:14:14.123456Z", "IpAddress": "192.0.2.7", "ActiveLoginCount": float64(2)}
		if named {
			device.CustomName = &custom
			device.LastUserID = &userID
			device.LastUserName = &userName
			device.Name = custom
			want["CustomName"], want["LastUserId"], want["LastUserName"], want["Name"] = custom, userID, userName, custom
		}
		encoded, err := json.Marshal(nativeManagedDevice(device))
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("native device projection changed its exact safe fields, null semantics, decimal identifiers, or UTC timestamps")
		}
		if device.CreatedAt.Location().String() != "fixture" || device.LastSeenAt.Location().String() != "fixture" {
			t.Fatal("native device projection modified identity-store timestamps")
		}
	}
}
