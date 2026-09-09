package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestApplicationKeyHTTPNativeProjectionIsSafeAndUsesUTC(t *testing.T) {
	zone := time.FixedZone("fixture", 8*60*60)
	created := time.Date(2026, time.September, 10, 12, 13, 14, 0, zone)
	used, revoked := created.Add(time.Minute), created.Add(2*time.Minute)
	for _, active := range []bool{true, false} {
		key := identity.ApplicationKey{
			ID: 9223372036854775807, CredentialID: "private-credential", AppName: "Projection Fixture",
			Token: "private-token", CreatedAt: created, CreatedBy: "creator", IPAddress: "192.0.2.7",
			ReportedDeviceNumericID: 1, Client: identity.Client{Name: "private-client", DeviceID: "private-device"},
		}
		want := map[string]any{
			"Id": "9223372036854775807", "AppName": "Projection Fixture", "CreatedAt": "2026-09-10T04:13:14Z",
			"LastUsedAt": nil, "RevokedAt": nil, "CreatedBy": "creator", "IPAddress": "192.0.2.7", "Status": "active",
		}
		if !active {
			key.CreatedBy, key.LastUsedAt, key.RevokedAt = "", &used, &revoked
			want["CreatedBy"], want["LastUsedAt"], want["RevokedAt"], want["Status"] = nil, "2026-09-10T04:14:14Z", "2026-09-10T04:15:14Z", "revoked"
		}
		encoded, err := json.Marshal(nativeApplicationKey(key))
		if err != nil {
			t.Fatalf("marshal application key projection: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("decode application key projection: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("native key projection = %#v, want %#v", got, want)
		}
		if strings.Contains(string(encoded), "private-") {
			t.Error("native key projection exposed credential or client details")
		}
	}
}

func TestApplicationKeyHTTPQueryContract(t *testing.T) {
	for _, test := range []struct {
		name, query string
		native      bool
		want        identity.ApplicationKeyFilter
		invalid     bool
	}{
		{name: "native_default", native: true, want: identity.ApplicationKeyFilter{Limit: 50}},
		{name: "compatibility_default", want: identity.ApplicationKeyFilter{Limit: 200, RevealTokens: true}},
		{name: "native_full_filter", native: true, query: "StartIndex=2&Limit=1&SearchTerm=Media+Server&IncludeRevoked=true", want: identity.ApplicationKeyFilter{StartIndex: 2, Limit: 1, SearchTerm: "Media Server", IncludeRevoked: true}},
		{name: "native_upper_limit", native: true, query: "Limit=200&IncludeRevoked=false", want: identity.ApplicationKeyFilter{Limit: 200}},
		{name: "compatibility_lower_limit", query: "STARTINDEX=2&limit=1&api_key=secret", want: identity.ApplicationKeyFilter{StartIndex: 2, Limit: 1, RevealTokens: true}},
		{name: "compatibility_upper_limit", query: "Limit=200", want: identity.ApplicationKeyFilter{Limit: 200, RevealTokens: true}},
		{name: "native_zero_limit", native: true, query: "Limit=0", invalid: true},
		{name: "compatibility_zero_limit", query: "Limit=0", invalid: true},
		{name: "native_negative_limit", native: true, query: "Limit=-1", invalid: true},
		{name: "compatibility_negative_limit", query: "Limit=-1", invalid: true},
		{name: "native_oversized_limit", native: true, query: "Limit=201", invalid: true},
		{name: "compatibility_oversized_limit", query: "Limit=201", invalid: true},
		{name: "native_malformed_limit", native: true, query: "Limit=one", invalid: true},
		{name: "compatibility_malformed_limit", query: "Limit=one", invalid: true},
		{name: "native_negative_start", native: true, query: "StartIndex=-1", invalid: true},
		{name: "compatibility_negative_start", query: "StartIndex=-1", invalid: true},
		{name: "native_overflow", native: true, query: "StartIndex=2147483648", invalid: true},
		{name: "compatibility_overflow", query: "StartIndex=2147483648", invalid: true},
		{name: "native_leading_zero", native: true, query: "Limit=01", invalid: true},
		{name: "native_duplicate", native: true, query: "Limit=1&Limit=1", invalid: true},
		{name: "compatibility_duplicate_case", query: "Limit=1&limit=1", invalid: true},
		{name: "native_wrong_case", native: true, query: "limit=1", invalid: true},
		{name: "native_token_query", native: true, query: "api_key=secret", invalid: true},
		{name: "native_null_boolean", native: true, query: "IncludeRevoked=null", invalid: true},
		{name: "native_numeric_boolean", native: true, query: "IncludeRevoked=1", invalid: true},
		{name: "native_case_boolean", native: true, query: "IncludeRevoked=True", invalid: true},
		{name: "compatibility_unsupported_filter", query: "IncludeRevoked=true", invalid: true},
		{name: "invalid_escape", native: true, query: "SearchTerm=%zz", invalid: true},
		{name: "invalid_utf8", native: true, query: "SearchTerm=%ff", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin/v1/api-keys", nil)
			request.URL.RawQuery = test.query
			got, err := parseApplicationKeyQuery(request, test.native)
			if test.invalid {
				if !errors.Is(err, identity.ErrInvalidInput) {
					t.Fatalf("query error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Errorf("query filter = %+v, error = %v, want %+v", got, err, test.want)
			}
		})
	}
}

func TestApplicationKeyHTTPBodyRequiresOneExactBoundedObject(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType, query string
		fields                         []string
		status                         int
		omitContentType                bool
	}{
		{name: "create", body: `{"AppName":"Fixture"}`, fields: []string{"AppName"}},
		{name: "empty_action", body: `{}`},
		{name: "json_parameters", body: `{}`, contentType: "application/json; charset=utf-8"},
		{name: "exact_limit", body: `{}` + strings.Repeat(" ", 4094)},
		{name: "over_limit", body: `{}` + strings.Repeat(" ", 4095), status: http.StatusBadRequest},
		{name: "missing_field", body: `{}`, fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "unknown_field", body: `{"Token":"private"}`, status: http.StatusBadRequest},
		{name: "wrong_case", body: `{"appname":"Fixture"}`, fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "duplicate", body: `{"AppName":"One","AppName":"Two"}`, fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "escaped_duplicate", body: `{"AppName":"One","App\u004eame":"Two"}`, fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "case_variant_duplicate", body: `{"AppName":"One","appName":"Two"}`, fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "null_object", body: `null`, status: http.StatusBadRequest},
		{name: "array", body: `[]`, status: http.StatusBadRequest},
		{name: "boolean", body: `false`, status: http.StatusBadRequest},
		{name: "empty_body", status: http.StatusBadRequest},
		{name: "second_object", body: `{} {}`, status: http.StatusBadRequest},
		{name: "trailing_null", body: `{} null`, status: http.StatusBadRequest},
		{name: "invalid_utf8", body: "{\"AppName\":\"\xff\"}", fields: []string{"AppName"}, status: http.StatusBadRequest},
		{name: "query_not_allowed", body: `{}`, query: "ignored=1", status: http.StatusBadRequest},
		{name: "form_media_type", body: `{}`, contentType: "application/x-www-form-urlencoded", status: http.StatusUnsupportedMediaType},
		{name: "invalid_media_type", body: `{}`, contentType: "application/json; charset", status: http.StatusUnsupportedMediaType},
		{name: "missing_media_type", body: `{}`, omitContentType: true, status: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/admin/v1/api-keys", strings.NewReader(test.body))
			request.URL.RawQuery = test.query
			request.ContentLength = -1
			contentType := test.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			if !test.omitContentType {
				request.Header.Set("Content-Type", contentType)
			}
			response := httptest.NewRecorder()
			values, ok := applicationKeyBody(response, request, test.fields)
			if test.status == 0 {
				if !ok || len(values) != len(test.fields) {
					t.Fatalf("valid body rejected: values = %#v, response = %s", values, response.Body.String())
				}
				return
			}
			if ok {
				t.Fatal("invalid body accepted")
			}
			code := "invalid_input"
			if test.status == http.StatusUnsupportedMediaType {
				code = "unsupported_media_type"
			}
			expectAPIError(t, response, test.status, code, false)
		})
	}
}

func TestApplicationKeyHTTPIDRequiresCanonicalPositiveDecimal(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "+1", "01", "1.0", " 1", "9223372036854775808", "key"} {
		t.Run(value, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/admin/v1/api-keys/id/revoke", nil)
			request.SetPathValue("id", value)
			response := httptest.NewRecorder()
			if _, ok := applicationKeyID(response, request); ok {
				t.Fatal("invalid application key ID accepted")
			}
			expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/api-keys/id/revoke", nil)
	request.SetPathValue("id", "9223372036854775807")
	if id, ok := applicationKeyID(httptest.NewRecorder(), request); !ok || id != 9223372036854775807 {
		t.Errorf("largest canonical key ID = %d, valid = %v", id, ok)
	}
}
