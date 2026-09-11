//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type applicationKeyHTTPFixture struct {
	*serverFixture
	cookie                *http.Cookie
	csrf, adminID, master string
	logs                  bytes.Buffer
}

type applicationKeyHTTPSecret struct{ id, token string }

func (key applicationKeyHTTPSecret) numericID(t *testing.T) int64 {
	t.Helper()
	id, err := strconv.ParseInt(key.id, 10, 64)
	if err != nil {
		t.Fatalf("parse fixture application key ID: %v", err)
	}
	return id
}

func newApplicationKeyHTTPFixture(t *testing.T) *applicationKeyHTTPFixture {
	t.Helper()
	a := &applicationKeyHTTPFixture{serverFixture: newServerFixture(t)}
	a.master = filepath.Join(t.TempDir(), "application-key-master.key")
	a.users = identity.NewWithApplicationKeyVault(a.pool, identity.NewApplicationKeyVault(a.master))
	a.app.identity = a.users
	a.log = slog.New(slog.NewTextHandler(&a.logs, nil))
	a.app.log = a.log
	a.handler = a.app.Handler()
	a.adminID = a.bootstrap(t)
	a.cookie, a.csrf = a.adminLogin(t)
	return a
}

func (a *applicationKeyHTTPFixture) native(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return a.request(t, method, path, body, http.Header{"X-CSRF-Token": {a.csrf}}, a.cookie)
}

func (a *applicationKeyHTTPFixture) create(t *testing.T, name string) applicationKeyHTTPSecret {
	t.Helper()
	response := a.native(t, http.MethodPost, "/admin/v1/api-keys", map[string]any{"AppName": name})
	expectStatus(t, response, http.StatusCreated)
	expectApplicationKeyHTTPNoCache(t, response)
	object := jsonObject(t, response)
	if len(object) != 2 {
		t.Fatalf("create response must contain only Key and AccessToken: %#v", object)
	}
	key := objectValue(t, object, "Key")
	expectApplicationKeyHTTPMetadata(t, key)
	return applicationKeyHTTPSecret{id: stringValue(t, key, "Id"), token: stringValue(t, object, "AccessToken")}
}

func applicationKeyHTTPCounts(t *testing.T, f *serverFixture) [4]int {
	t.Helper()
	var counts [4]int
	err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM sessions),
		(SELECT count(*) FROM application_keys), (SELECT count(*) FROM application_key_clients),
		(SELECT count(*) FROM users)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3])
	if err != nil {
		t.Fatalf("read application key row counts: %v", err)
	}
	return counts
}

func expectApplicationKeyHTTPNoCache(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Errorf("sensitive key response is cacheable: %v", response.Header())
	}
}

func expectApplicationKeyHTTPMetadata(t *testing.T, key map[string]any) {
	t.Helper()
	fields := []string{"Id", "AppName", "CreatedAt", "LastUsedAt", "RevokedAt", "CreatedBy", "IPAddress", "Status"}
	if len(key) != len(fields) {
		t.Fatalf("native key must contain exactly eight safe metadata fields: %#v", key)
	}
	for _, field := range fields {
		if _, exists := key[field]; !exists {
			t.Errorf("native key is missing %s", field)
		}
	}
	id := stringValue(t, key, "Id")
	if parsed, err := strconv.ParseInt(id, 10, 64); err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != id {
		t.Errorf("native key ID is not a canonical decimal string: %q", id)
	}
	expectApplicationKeyHTTPUTC(t, stringValue(t, key, "CreatedAt"))
}

func expectApplicationKeyHTTPUTC(t *testing.T, value string) {
	t.Helper()
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil || !strings.HasSuffix(value, "Z") {
		t.Errorf("key timestamp must be RFC3339 UTC: %q, error = %v", value, err)
	}
}

func applicationKeyHTTPItems(t *testing.T, response *httptest.ResponseRecorder, total, length int) []any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	expectApplicationKeyHTTPNoCache(t, response)
	object := jsonObject(t, response)
	items, ok := object["Items"].([]any)
	if !ok || len(items) != length || object["TotalRecordCount"] != float64(total) {
		t.Fatalf("key list has inconsistent pagination: %#v, want total %d and length %d", object, total, length)
	}
	return items
}

func TestHTTPApplicationKeyNativeLifecycleAndSecretBoundary(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	key := a.create(t, "  Native Application  ")
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != [4]int{before[0] + 1, 1, 1, before[3]} {
		t.Fatalf("creation must add one userless credential, key, and default client: %v", got)
	}
	var credentialID, kind string
	var hash, ciphertext []byte
	var userless, nonexpiring bool
	err := a.pool.QueryRow(a.ctx, `SELECT s.id, s.kind, s.user_id IS NULL, s.expires_at IS NULL,
		s.token_hash, k.secret_ciphertext FROM sessions s JOIN application_keys k ON k.credential_id = s.id
		WHERE k.id = $1`, key.numericID(t)).Scan(&credentialID, &kind, &userless, &nonexpiring, &hash, &ciphertext)
	if err != nil {
		t.Fatalf("read issued application credential: %v", err)
	}
	digest := sha256.Sum256([]byte(key.token))
	if kind != identity.ApplicationKeyKind || !userless || !nonexpiring || !bytes.Equal(hash, digest[:]) {
		t.Error("issued application key lost its userless, non-expiring hashed credential boundary")
	}
	if len(ciphertext) <= len(key.token) || bytes.Contains(ciphertext, []byte(key.token)) || bytes.Equal(ciphertext, hash) {
		t.Error("recoverable application secret was not persisted as distinct ciphertext")
	}
	list := a.native(t, http.MethodGet, "/admin/v1/api-keys", nil)
	items := applicationKeyHTTPItems(t, list, 1, 1)
	metadata := items[0].(map[string]any)
	expectApplicationKeyHTTPMetadata(t, metadata)
	if metadata["AppName"] != "Native Application" || metadata["CreatedBy"] != a.adminID || metadata["IPAddress"] != "192.0.2.1" ||
		metadata["Status"] != "active" || metadata["LastUsedAt"] != nil || metadata["RevokedAt"] != nil {
		t.Errorf("native metadata differs from the persisted creation facts: %#v", metadata)
	}
	if strings.Contains(list.Body.String(), key.token) || strings.Contains(list.Body.String(), credentialID) {
		t.Error("native list exposed the bearer secret or internal credential ID")
	}
	reveal := a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/reveal", map[string]any{})
	expectStatus(t, reveal, http.StatusOK)
	expectApplicationKeyHTTPNoCache(t, reveal)
	if object := jsonObject(t, reveal); len(object) != 2 || object["Id"] != key.id || object["AccessToken"] != key.token {
		t.Errorf("reveal response changed its exact secret contract: %#v", object)
	}
	revoke := a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{})
	expectStatus(t, revoke, http.StatusOK)
	expectApplicationKeyHTTPNoCache(t, revoke)
	object := jsonObject(t, revoke)
	if len(object) != 2 || object["Id"] != key.id {
		t.Errorf("revoke response changed its exact contract: %#v", object)
	}
	revokedAt := stringValue(t, object, "RevokedAt")
	expectApplicationKeyHTTPUTC(t, revokedAt)
	repeated := a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{})
	expectStatus(t, repeated, http.StatusOK)
	if jsonObject(t, repeated)["RevokedAt"] != revokedAt {
		t.Error("repeated revocation changed the original revocation time")
	}
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys", nil), 0, 0)
	history := a.native(t, http.MethodGet, "/admin/v1/api-keys?IncludeRevoked=true", nil)
	historical := applicationKeyHTTPItems(t, history, 1, 1)[0].(map[string]any)
	expectApplicationKeyHTTPMetadata(t, historical)
	if historical["Status"] != "revoked" || historical["RevokedAt"] != revokedAt || strings.Contains(history.Body.String(), key.token) {
		t.Errorf("revoked history did not preserve safe metadata: %#v", historical)
	}
	expectAPIError(t, a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/reveal", map[string]any{}), http.StatusConflict, "key_revoked", false)
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {key.token}}), http.StatusUnauthorized, "invalid_credentials", true)
	for _, secret := range []string{key.token, a.master} {
		if strings.Contains(a.logs.String(), secret) {
			t.Error("application key audit logs exposed a secret or master file path")
		}
	}
}

func TestHTTPApplicationKeyCompatibilityLifecycleAndIndependentDuplicates(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	login := a.embyLogin(t, "Administrator", "administrator-password")
	adminToken := stringValue(t, login, "AccessToken")
	headers := http.Header{"X-Emby-Token": {adminToken}}
	for range 2 {
		response := a.request(t, http.MethodPost, "/emby/Auth/Keys?App=Duplicate+Application", nil, headers)
		expectStatus(t, response, http.StatusNoContent)
		expectApplicationKeyHTTPNoCache(t, response)
		if response.Body.Len() != 0 {
			t.Error("compatibility creation must have an empty 204 body")
		}
	}
	listed := a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, headers)
	items := applicationKeyHTTPItems(t, listed, 2, 2)
	if len(jsonObject(t, listed)) != 2 {
		t.Error("compatibility key list must contain only Items and TotalRecordCount")
	}
	keys := make([]string, 0, len(items))
	ids := make(map[float64]bool)
	for _, raw := range items {
		item := raw.(map[string]any)
		id, numeric := item["Id"].(float64)
		if !numeric || id <= 0 || ids[id] || len(item) != 11 || item["UserId"] != float64(0) || item["DeviceId"] != float64(1) ||
			item["AppName"] != "Duplicate Application" || item["ReportedDeviceId"] != a.app.serverID || item["DeviceName"] != a.cfg.ServerName ||
			item["AppVersion"] != "integration-version" || item["IpAddress"] != "192.0.2.1" || item["IsActive"] != true {
			t.Errorf("compatibility key DTO does not preserve its numeric userless contract: %#v", item)
		}
		ids[id] = true
		expectApplicationKeyHTTPUTC(t, stringValue(t, item, "DateCreated"))
		keys = append(keys, stringValue(t, item, "AccessToken"))
	}
	if keys[0] == keys[1] {
		t.Fatal("duplicate application names reused an existing credential")
	}
	applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys?StartIndex=1&Limit=1", nil, headers), 2, 1)
	applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys?StartIndex=9&Limit=1", nil, headers), 2, 0)
	for _, target := range []struct{ method, path string }{
		{http.MethodDelete, "/emby/Auth/Keys/" + keys[0]},
		{http.MethodPost, "/emby/Auth/Keys/" + keys[0] + "/Delete"},
		{http.MethodDelete, "/emby/Auth/Keys/unknown-token"},
		{http.MethodPost, "/emby/Auth/Keys/" + strings.Repeat("A", 43) + "/Delete"},
	} {
		response := a.request(t, target.method, target.path, nil, headers)
		expectStatus(t, response, http.StatusNoContent)
		expectApplicationKeyHTTPNoCache(t, response)
		if response.Body.Len() != 0 {
			t.Error("compatibility deletion must have an empty 204 body")
		}
		if strings.Contains(a.logs.String(), target.path) {
			t.Error("key revocation logs exposed the token-bearing request target")
		}
	}
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {keys[0]}}), http.StatusUnauthorized, "invalid_credentials", true)
	keyHeaders := http.Header{"X-Emby-Token": {keys[1]}}
	remaining := applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, keyHeaders), 1, 1)[0].(map[string]any)
	if remaining["AccessToken"] != keys[1] {
		t.Error("revoking one duplicate application revoked or replaced the other")
	}
	expectApplicationKeyHTTPUTC(t, stringValue(t, remaining, "DateLastActivity"))
	logout := a.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, keyHeaders)
	expectStatus(t, logout, http.StatusNoContent)
	expectApplicationKeyHTTPNoCache(t, logout)
	if logout.Body.Len() != 0 {
		t.Error("application key logout must have an empty 204 body")
	}
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, keyHeaders), http.StatusUnauthorized, "invalid_credentials", true)
	applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, headers), 0, 0)
	expectStatus(t, a.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, headers), http.StatusNoContent)
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, headers), http.StatusUnauthorized, "invalid_credentials", true)
	for _, token := range append(keys, adminToken) {
		if strings.Contains(a.logs.String(), token) {
			t.Error("application key logging exposed a bearer token")
		}
	}
}

func TestHTTPApplicationKeyAuthorizationIsolationAndUserProjection(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "Userless Access")
	viewer, err := a.users.CreateUser(a.ctx, "Key Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create ordinary viewer: %v", err)
	}
	viewerToken := stringValue(t, a.embyLogin(t, viewer.Name, "viewer-password"), "AccessToken")
	viewerHeaders := http.Header{"X-Emby-Token": {viewerToken}}
	keyHeaders := http.Header{"X-Emby-Token": {key.token}}
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/emby/Auth/Keys"},
		{http.MethodPost, "/emby/Auth/Keys?App=Denied"},
		{http.MethodDelete, "/emby/Auth/Keys/" + key.token},
		{http.MethodPost, "/emby/Auth/Keys/" + key.token + "/Delete"},
	} {
		expectEmbyTextError(t, a.request(t, route.method, route.path, nil, viewerHeaders), http.StatusForbidden,
			"User Key Viewer does not have access to ManageServer feature.")
	}
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/api-keys", nil},
		{http.MethodPost, "/admin/v1/api-keys", map[string]any{"AppName": "Denied"}},
		{http.MethodPost, "/admin/v1/api-keys/" + key.id + "/reveal", map[string]any{}},
		{http.MethodPost, "/admin/v1/api-keys/" + key.id + "/revoke", map[string]any{}},
	} {
		expectAPIError(t, a.request(t, route.method, route.path, route.body, keyHeaders), http.StatusUnauthorized, "authentication_required", false)
		keyCookie := &http.Cookie{Name: sessionCookie, Value: key.token}
		expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {csrfToken(key.token)}}, keyCookie), http.StatusUnauthorized, "invalid_credentials", false)
		if route.method == http.MethodPost {
			expectAPIError(t, a.request(t, route.method, route.path, route.body, nil, a.cookie), http.StatusForbidden, "csrf_invalid", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {a.csrf}, "Origin": {"https://other.example.test"}}, a.cookie), http.StatusForbidden, "origin_denied", false)
		}
	}
	expectAPIError(t, a.request(t, http.MethodGet, "/admin/v1/api-keys?api_key="+url.QueryEscape(key.token), nil, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, nil, a.cookie), http.StatusUnauthorized, "authentication_required", true)
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != before {
		t.Errorf("rejected authorization inserted or removed identity rows: before = %v, after = %v", before, got)
	}
	users := a.request(t, http.MethodGet, "/emby/Users", nil, keyHeaders)
	expectStatus(t, users, http.StatusOK)
	var listed []map[string]any
	if err := json.Unmarshal(users.Body.Bytes(), &listed); err != nil || len(listed) != 2 {
		t.Fatalf("application key Users response must be a bare array of real users: %s, error = %v", users.Body.String(), err)
	}
	seen := map[string]bool{}
	for _, user := range listed {
		seen[stringValue(t, user, "Id")] = true
	}
	if !seen[a.adminID] || !seen[viewer.ID] {
		t.Errorf("application key user list substituted a synthetic owner: %#v", listed)
	}
	for _, id := range []string{a.adminID, viewer.ID} {
		response := a.request(t, http.MethodGet, "/emby/Users/"+id, nil, keyHeaders)
		expectStatus(t, response, http.StatusOK)
		if jsonObject(t, response)["Id"] != id {
			t.Error("application key user detail substituted the creator")
		}
	}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, viewerHeaders), http.StatusOK)
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/Users/"+a.adminID, nil, viewerHeaders), http.StatusForbidden, "access_denied", true)
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/Users", nil, viewerHeaders), http.StatusForbidden, "administrator_required", true)
	createdByKey := a.request(t, http.MethodPost, "/emby/Auth/Keys?App=Userless+Creator", nil, keyHeaders)
	expectStatus(t, createdByKey, http.StatusNoContent)
	expectApplicationKeyHTTPNoCache(t, createdByKey)
	child := applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?SearchTerm=Userless+Creator", nil), 1, 1)[0].(map[string]any)
	expectApplicationKeyHTTPMetadata(t, child)
	if child["CreatedBy"] != nil {
		t.Error("application key creation invented a user owner")
	}
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != [4]int{before[0] + 1, before[1] + 1, before[2] + 1, before[3]} {
		t.Errorf("key-created credential must remain independent and userless: %v", got)
	}
}

func TestHTTPApplicationKeyInvalidRequestsLeaveNoCredentialOrMasterFile(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	for _, test := range []struct {
		name, path, body string
		status           int
	}{
		{name: "null_name", path: "/admin/v1/api-keys", body: `{"AppName":null}`},
		{name: "boolean_name", path: "/admin/v1/api-keys", body: `{"AppName":true}`},
		{name: "numeric_name", path: "/admin/v1/api-keys", body: `{"AppName":1}`},
		{name: "empty_name", path: "/admin/v1/api-keys", body: `{"AppName":" "}`},
		{name: "control_name", path: "/admin/v1/api-keys", body: `{"AppName":"Bad\nName"}`},
		{name: "long_name", path: "/admin/v1/api-keys", body: `{"AppName":"` + strings.Repeat("a", 257) + `"}`},
		{name: "duplicate_name", path: "/admin/v1/api-keys", body: `{"AppName":"A","AppName":"B"}`},
		{name: "unknown_field", path: "/admin/v1/api-keys", body: `{"AppName":"A","UserId":"owner"}`},
		{name: "create_query", path: "/admin/v1/api-keys?AppName=A", body: `{"AppName":"A"}`},
		{name: "reveal_query", path: "/admin/v1/api-keys/1/reveal?Token=secret", body: `{}`},
		{name: "revoke_query", path: "/admin/v1/api-keys/1/revoke?Force=true", body: `{}`},
		{name: "reveal_nonempty_body", path: "/admin/v1/api-keys/1/reveal", body: `{"Confirm":true}`},
		{name: "revoke_nonempty_body", path: "/admin/v1/api-keys/1/revoke", body: `{"Confirm":true}`},
		{name: "oversized_body", path: "/admin/v1/api-keys", body: `{"AppName":"A"}` + strings.Repeat(" ", 4096)},
		{name: "unknown_id", path: "/admin/v1/api-keys/1/reveal", body: `{}`, status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)).WithContext(a.ctx)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", a.csrf)
			request.AddCookie(a.cookie)
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			status, code := test.status, "invalid_input"
			if status == 0 {
				status = http.StatusBadRequest
			} else if status == http.StatusNotFound {
				code = "not_found"
			}
			expectAPIError(t, response, status, code, false)
			expectApplicationKeyHTTPNoCache(t, response)
			if got := applicationKeyHTTPCounts(t, a.serverFixture); got != before {
				t.Fatalf("invalid request changed identity rows: before = %v, after = %v", before, got)
			}
		})
	}
	for _, query := range []string{"Limit=0", "Limit=-1", "Limit=bad", "StartIndex=-1", "IncludeRevoked=null"} {
		expectAPIError(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?"+query, nil), http.StatusBadRequest, "invalid_input", false)
	}
	entries, err := os.ReadDir(filepath.Dir(a.master))
	if err != nil || len(entries) != 0 {
		t.Errorf("rejected native operations created vault files or sidecars: %v, error = %v", entries, err)
	}
}

func TestHTTPApplicationKeyPaginationUsesActualFilteredCounts(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	actor, err := a.users.Resolve(a.ctx, a.cookie.Value, "admin")
	if err != nil {
		t.Fatalf("resolve pagination actor: %v", err)
	}
	for index := range 52 {
		name := "Common Application"
		if index == 51 {
			name = "Unique Application"
		}
		if _, err := a.users.CreateApplicationKey(a.ctx, actor, name, "192.0.2.1", identity.Client{
			DeviceID: a.app.serverID, Device: a.cfg.ServerName, Version: "integration-version",
		}); err != nil {
			t.Fatalf("create pagination key %d: %v", index, err)
		}
	}
	native := a.native(t, http.MethodGet, "/admin/v1/api-keys", nil)
	applicationKeyHTTPItems(t, native, 52, 50)
	if object := jsonObject(t, native); len(object) != 4 || object["StartIndex"] != float64(0) || object["Limit"] != float64(50) {
		t.Errorf("native list lost its default pagination envelope: %#v", object)
	}
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?Limit=200", nil), 52, 52)
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?StartIndex=52&Limit=1", nil), 52, 0)
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?SearchTerm=unique&Limit=1", nil), 1, 1)
	adminToken := stringValue(t, a.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	headers := http.Header{"X-Emby-Token": {adminToken}}
	applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, headers), 52, 52)
	applicationKeyHTTPItems(t, a.request(t, http.MethodGet, "/emby/Auth/Keys?STARTINDEX=51&limit=1", nil, headers), 52, 1)
	for _, query := range []string{"Limit=0", "Limit=-1", "Limit=bad", "Limit=201", "StartIndex=-1", "StartIndex=bad", "Limit=1&limit=1"} {
		expectAPIError(t, a.request(t, http.MethodGet, "/emby/Auth/Keys?"+query, nil, headers), http.StatusBadRequest, "invalid_input", true)
	}
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	for _, query := range []string{"", "App=", "App=One&App=Two", "App=One&app=Two", "App=One&UserId=owner", "App=Bad%0AName", "App=" + strings.Repeat("a", 257)} {
		expectAPIError(t, a.request(t, http.MethodPost, "/emby/Auth/Keys?"+query, nil, headers), http.StatusBadRequest, "invalid_input", true)
	}
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != before {
		t.Errorf("rejected compatibility requests changed credential rows: before = %v, after = %v", before, got)
	}
}

func TestHTTPApplicationKeyMissingVaultKeepsMetadataAuthenticationAndRevocation(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "Persistent Secret")
	adminToken := stringValue(t, a.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	headers := http.Header{"X-Emby-Token": {adminToken}}
	if err := os.Remove(a.master); err != nil {
		t.Fatalf("remove owned master file fixture: %v", err)
	}
	// A fresh store simulates a restart: persisted ciphertext is the witness
	// that forbids silently replacing a missing master key.
	a.users = identity.NewWithApplicationKeyVault(a.pool, identity.NewApplicationKeyVault(a.master))
	a.app.identity = a.users
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys", nil), 1, 1)
	expectStatus(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {key.token}}), http.StatusOK)
	for _, response := range []*httptest.ResponseRecorder{
		a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/reveal", map[string]any{}),
		a.native(t, http.MethodPost, "/admin/v1/api-keys", map[string]any{"AppName": "Must Not Exist"}),
	} {
		expectAPIError(t, response, http.StatusServiceUnavailable, "key_vault_unavailable", false)
		expectApplicationKeyHTTPNoCache(t, response)
		if strings.Contains(response.Body.String(), a.master) || strings.Contains(response.Body.String(), key.token) {
			t.Error("vault failure response exposed its master path or bearer secret")
		}
	}
	expectAPIError(t, a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, headers), http.StatusServiceUnavailable, "key_vault_unavailable", true)
	expectAPIError(t, a.request(t, http.MethodPost, "/emby/Auth/Keys?App=Must+Not+Exist", nil, headers), http.StatusServiceUnavailable, "key_vault_unavailable", true)
	expectStatus(t, a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{}), http.StatusOK)
	applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?IncludeRevoked=true", nil), 1, 1)
	expectAPIError(t, a.native(t, http.MethodPost, "/admin/v1/api-keys", map[string]any{"AppName": "Still Must Not Exist"}), http.StatusServiceUnavailable, "key_vault_unavailable", false)
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != before {
		t.Errorf("vault failures inserted or removed identity rows: before = %v, after = %v", before, got)
	}
	entries, err := os.ReadDir(filepath.Dir(a.master))
	if err != nil || len(entries) != 0 {
		t.Errorf("missing vault was silently replaced or created sidecars: %v, error = %v", entries, err)
	}
	if strings.Contains(a.logs.String(), key.token) || strings.Contains(a.logs.String(), a.master) {
		t.Error("vault failure logs exposed the secret or master path")
	}
}

func TestHTTPApplicationKeyStaleActorIsRevalidatedBeforeEveryOperation(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "Stale Actor Target")
	actor, err := a.users.Resolve(a.ctx, a.cookie.Value, "admin")
	if err != nil {
		t.Fatalf("resolve actor snapshot: %v", err)
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
		t.Fatalf("revoke actor after authentication snapshot: %v", err)
	}
	before := applicationKeyHTTPCounts(t, a.serverFixture)
	for _, test := range []struct {
		name, method, path, body string
		handler                  http.HandlerFunc
		emby                     bool
	}{
		{name: "native_list", method: http.MethodGet, path: "/admin/v1/api-keys", handler: a.app.adminApplicationKeys},
		{name: "native_create", method: http.MethodPost, path: "/admin/v1/api-keys", body: `{"AppName":"Denied"}`, handler: a.app.createAdminApplicationKey},
		{name: "native_reveal", method: http.MethodPost, path: "/admin/v1/api-keys/" + key.id + "/reveal", body: `{}`, handler: a.app.revealAdminApplicationKey},
		{name: "native_revoke", method: http.MethodPost, path: "/admin/v1/api-keys/" + key.id + "/revoke", body: `{}`, handler: a.app.revokeAdminApplicationKey},
		{name: "compatibility_list", method: http.MethodGet, path: "/emby/Auth/Keys", handler: a.app.embyApplicationKeys, emby: true},
		{name: "compatibility_create", method: http.MethodPost, path: "/emby/Auth/Keys?App=Denied", handler: a.app.createEmbyApplicationKey, emby: true},
		{name: "compatibility_revoke", method: http.MethodDelete, path: "/emby/Auth/Keys/" + key.token, handler: a.app.revokeEmbyApplicationKey, emby: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request = request.WithContext(context.WithValue(a.ctx, principalKey, actor))
			request.Header.Set("Content-Type", "application/json")
			request.SetPathValue("id", key.id)
			request.SetPathValue("Key", key.token)
			response := httptest.NewRecorder()
			test.handler(response, request)
			expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", test.emby)
			if strings.Contains(response.Body.String(), key.token) {
				t.Error("stale actor received the target application secret")
			}
		})
	}
	if got := applicationKeyHTTPCounts(t, a.serverFixture); got != before {
		t.Errorf("stale actor inserted or removed identity rows: before = %v, after = %v", before, got)
	}
	var active bool
	if err := a.pool.QueryRow(a.ctx, `SELECT s.revoked_at IS NULL FROM sessions s
		JOIN application_keys k ON k.credential_id = s.id WHERE k.id = $1`, key.numericID(t)).Scan(&active); err != nil || !active {
		t.Errorf("stale actor revoked the target key: active = %v, error = %v", active, err)
	}
	expectAPIError(t, a.native(t, http.MethodGet, "/admin/v1/api-keys", nil), http.StatusUnauthorized, "invalid_credentials", false)
}
