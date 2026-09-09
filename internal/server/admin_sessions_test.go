package server

import (
	"context"
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

func adminSessionRequestForTest(query, body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/sessions/session-id/revoke", strings.NewReader(body))
	r.URL.RawQuery = query
	r.SetPathValue("id", "session-id")
	r.Header.Set("Content-Type", contentType)
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "session-request-id"))
}

func assertAdminSessionInputError(t *testing.T, response *httptest.ResponseRecorder, status int, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("session validation status = %d, want %d", response.Code, status)
	}
	var body struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode session validation envelope: %v", err)
	}
	code := "invalid_input"
	if status == http.StatusUnsupportedMediaType {
		code = "unsupported_media_type"
	}
	if body.Error.Code != code || body.RequestID != "session-request-id" || field != "" && body.Error.Fields[field] == "" {
		t.Fatal("session validation lost its code, field, or request identifier")
	}
}

func TestParseAdminSessionQueryPreservesCanonicalFilters(t *testing.T) {
	for _, test := range []struct {
		query string
		want  identity.ManagedSessionFilter
	}{
		{"", identity.ManagedSessionFilter{Limit: 50}},
		{"UserId=&Kind=&Status=&DeviceId=&SearchTerm=", identity.ManagedSessionFilter{Limit: 50}},
		{"UserId=opaque%3Auser&Kind=emby&Status=all&DeviceId=device+one&SearchTerm=Caf%C3%A9%25_&StartIndex=2147483647&Limit=200", identity.ManagedSessionFilter{UserID: "opaque:user", Kind: "emby", Status: "all", DeviceID: "device one", SearchTerm: "Caf\u00e9%_", StartIndex: 2147483647, Limit: 200}},
		{"Kind=admin&Status=active&StartIndex=0&Limit=1", identity.ManagedSessionFilter{Kind: "admin", Status: "active", Limit: 1}},
	} {
		r := adminSessionRequestForTest(test.query, "", "")
		got, err := parseAdminSessionQuery(r)
		if err != nil || !reflect.DeepEqual(got, test.want) || r.URL.RawQuery != test.query {
			t.Fatalf("canonical session filters changed or failed: %v", err)
		}
	}
}

func TestParseAdminSessionQueryRejectsAmbiguityAndInvalidEncoding(t *testing.T) {
	for _, test := range []struct{ query, field string }{
		{"userid=x", "Query"}, {"UserID=x", "Query"}, {"IsActive=true", "Query"}, {"api_key=secret-marker", "Query"},
		{"UserId=a&UserId=b", "UserId"}, {"Kind=emby&Kind=emby", "Kind"}, {"Status=all&Status=all", "Status"},
		{"DeviceId=a&DeviceId=a", "DeviceId"}, {"SearchTerm=a&SearchTerm=a", "SearchTerm"}, {"Limit=1&Limit=1", "Limit"},
		{"StartIndex=0&StartIndex=0", "StartIndex"}, {"DeviceId=%ff", "DeviceId"}, {"UserId=%ff", "UserId"},
		{"Kind=%ff", "Kind"}, {"Status=%ff", "Status"}, {"SearchTerm=%ff", "SearchTerm"},
		{"SearchTerm=%zz", "Query"}, {"SearchTerm=a;b", "Query"},
		{"StartIndex=-1", "StartIndex"}, {"StartIndex=01", "StartIndex"}, {"StartIndex=%2B1", "StartIndex"},
		{"StartIndex=1.0", "StartIndex"}, {"StartIndex=1e2", "StartIndex"}, {"StartIndex=2147483648", "StartIndex"}, {"StartIndex=", "StartIndex"},
		{"Limit=0", "Limit"}, {"Limit=201", "Limit"}, {"Limit=01", "Limit"}, {"Limit=%201", "Limit"}, {"Limit=", "Limit"},
	} {
		_, err := parseAdminSessionQuery(adminSessionRequestForTest(test.query, "", ""))
		var invalid *identity.ManagedSessionValidationError
		if !errors.As(err, &invalid) || invalid.Fields[test.field] == "" {
			t.Fatalf("invalid session filter lacks its %s error: %v", test.field, err)
		}
	}
}

func TestAdminSessionRevokeRejectsInvalidPathQueryAndBodyBeforeStore(t *testing.T) {
	app := &Server{}
	for _, id := range []string{"", " leading", "trailing ", "a\x00b", "a\nb", "a\u0085b", "a\xffb", strings.Repeat("a", 257), strings.Repeat("\u00e9", 129)} {
		r := adminSessionRequestForTest("", "{}", "application/json")
		r.SetPathValue("id", id)
		response := httptest.NewRecorder()
		app.revokeAdminSession(response, r)
		assertAdminSessionInputError(t, response, http.StatusBadRequest, "Id")
	}
	for _, query := range []string{"UserId=other", "Id=other", "api_key=secret-marker", "Limit=", "%zz"} {
		response := httptest.NewRecorder()
		app.revokeAdminSession(response, adminSessionRequestForTest(query, "{}", "application/json"))
		assertAdminSessionInputError(t, response, http.StatusBadRequest, "Query")
	}
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/json; invalid"} {
		response := httptest.NewRecorder()
		app.revokeAdminSession(response, adminSessionRequestForTest("", "{}", contentType))
		assertAdminSessionInputError(t, response, http.StatusUnsupportedMediaType, "")
	}
	for _, body := range []string{"", "null", "[]", "true", "\"value\"", "{}{}", "{}null", "{", "{,}", "{\"Id\":null}", "{\"Id\":\"secret-marker\"}", "{\"Id\":1,\"Id\":2}", "{\"Id\":1,\"\\u0049d\":2}", "{\"\xff\":0}", strings.Repeat(" ", 4095) + "{}"} {
		response := httptest.NewRecorder()
		app.revokeAdminSession(response, adminSessionRequestForTest("", body, "application/json"))
		assertAdminSessionInputError(t, response, http.StatusBadRequest, "Body")
		if strings.Contains(response.Body.String(), "secret-marker") {
			t.Fatal("session input error reflected a rejected value")
		}
	}
}

func TestNativeManagedSessionWhitelistAndUTCTimestamps(t *testing.T) {
	local := time.Date(2026, 9, 9, 12, 34, 56, 123456000, time.FixedZone("fixture", 8*60*60))
	for _, revoked := range []*time.Time{nil, &local} {
		session := identity.ManagedSession{SessionID: "session-id", UserID: "user-id", UserName: "Session User", UserIsAdministrator: true,
			Kind: "admin", Client: identity.Client{Name: "Dashboard", DeviceID: "device-id", Device: "Browser", Version: "1.2.3"},
			CreatedAt: local, LastSeenAt: local, ExpiresAt: local.Add(time.Hour), RevokedAt: revoked, Status: "active", IsCurrent: true}
		result := nativeManagedSession(session)
		want := []string{"Id", "UserId", "UserName", "UserIsAdministrator", "UserIsDisabled", "Kind", "Client", "DeviceId", "DeviceName", "ApplicationVersion", "CreatedAt", "LastSeenAt", "ExpiresAt", "RevokedAt", "Status", "IsCurrent"}
		if len(result) != len(want) {
			t.Fatal("native session projection changed its exact sixteen-field contract")
		}
		for _, key := range want {
			if _, present := result[key]; !present {
				t.Fatalf("native session projection omitted %s", key)
			}
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if err := json.Unmarshal(encoded, &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"CreatedAt", "LastSeenAt", "ExpiresAt", "RevokedAt"} {
			if key == "RevokedAt" && revoked == nil {
				if object[key] != nil {
					t.Fatal("unrevoked session must encode an explicit null revocation timestamp")
				}
				continue
			}
			value, ok := object[key].(string)
			instant, err := time.Parse(time.RFC3339Nano, value)
			want := local
			if key == "ExpiresAt" {
				want = local.Add(time.Hour)
			}
			if !ok || err != nil || !strings.HasSuffix(value, "Z") || !instant.Equal(want) {
				t.Fatalf("native session %s must preserve its instant in UTC", key)
			}
		}
		if revoked != nil && revoked.Location().String() != "fixture" {
			t.Fatal("projection mutated the identity store timestamp")
		}
	}
}
