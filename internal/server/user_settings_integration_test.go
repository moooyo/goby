//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func userSettingsHTTPRaw(t *testing.T, f *serverFixture, method, target, mediaType string, body []byte, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body)).WithContext(f.ctx)
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if mediaType != "" {
		request.Header.Set("Content-Type", mediaType)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func userSettingsHTTPMap(t *testing.T, response *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") ||
		response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("user preferences lost their JSON representation or private cache policy")
	}
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result == nil {
		t.Fatal("user preferences must be a JSON object containing string values")
	}
	return result
}

func userSettingsHTTPPersisted(t *testing.T, f *serverFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY user_id), '[]'::jsonb)::text
		FROM user_settings p`).Scan(&snapshot); err != nil {
		t.Fatal("read persisted preference rows and timestamps")
	}
	return snapshot
}

func userSettingsHTTPAccountState(t *testing.T, f *serverFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(jsonb_build_object(
		'id', id, 'configuration', configuration, 'policy', policy, 'revision', management_revision)
		ORDER BY id)::text FROM users`).Scan(&snapshot); err != nil {
		t.Fatal("read unrelated account configuration and policy")
	}
	return snapshot
}

func TestHTTPUserSettingsAcceptsCompleteWebClientQuery(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	query := url.Values{
		"X-Emby-Client": {"Session Integration"}, "X-Emby-Device-Name": {"Linux Session Fixture"},
		"X-Emby-Device-Id": {accounts.viewer.deviceID}, "X-Emby-Client-Version": {"1.2.3"},
		"X-Emby-Token": {accounts.viewer.headers.Get("X-Emby-Token")}, "X-Emby-Language": {"en-us"},
	}
	path := "/emby/usersettings/" + accounts.viewer.userID
	if values := userSettingsHTTPMap(t, userSettingsHTTPRaw(t, f, http.MethodGet, path+"?"+query.Encode(), "", nil, nil)); len(values) != 0 {
		t.Fatal("the original client's complete query did not return default preferences")
	}
	query.Set("reqformat", "json")
	response := userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial?"+query.Encode(),
		"text/plain", []byte(`{"genreLimitOnDetails":"2"}`), nil)
	expectStatus(t, response, http.StatusNoContent)
	query.Del("reqformat")
	query.Set("X-Emby-Language", "zh-CN")
	values := userSettingsHTTPMap(t, userSettingsHTTPRaw(t, f, http.MethodGet, path+"?"+query.Encode(), "", nil, nil))
	if !reflect.DeepEqual(values, map[string]string{"genreLimitOnDetails": "2"}) {
		t.Fatal("the client's language must not select another preference partition")
	}
}

func TestHTTPUserSettingsPersistAcrossDevicesAndObservedRequestVariants(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/emby/UserSettings/" + accounts.viewer.userID
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET configuration='{"AudioLanguagePreference":"eng","Retained":"configuration"}'::jsonb,
		policy=policy || '{"RetainedUserSettingsSentinel":true}'::jsonb WHERE id=$1`, accounts.viewer.userID); err != nil {
		t.Fatal("prepare independent user configuration and policy")
	}
	accountState := userSettingsHTTPAccountState(t, f)
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)); len(values) != 0 {
		t.Fatal("a user without stored preferences did not receive an empty object")
	}
	if userSettingsHTTPPersisted(t, f) != "[]" {
		t.Fatal("reading the default created a preference row")
	}
	for _, test := range []struct {
		path, mediaType, body string
	}{
		{path + "/Partial?reqformat=json", "text/plain", `{"genreLimitOnDetails":"2"}`},
		{"/uSeRsEtTiNgS/" + accounts.viewer.userID + "/pArTiAl", "application/json; charset=UTF-8", `{"theme":"dark"}`},
		{path + "/Partial?reqformat=JSON", "application/octet-stream", `{"sort":"Name"}`},
	} {
		response := userSettingsHTTPRaw(t, f, http.MethodPost, test.path, test.mediaType, []byte(test.body), accounts.viewer.headers)
		expectStatus(t, response, http.StatusNoContent)
		if response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("a successful Partial update returned a body or became cacheable")
		}
	}
	want := map[string]string{"genreLimitOnDetails": "2", "theme": "dark", "sort": "Name"}
	for _, client := range []string{"", "emby", "embyweb", "another-client"} {
		query := url.Values{"Client": {client}, "api_key": {accounts.second.headers.Get("X-Emby-Token")}}
		response := f.request(t, http.MethodGet, "/UsErSeTtInGs/"+accounts.viewer.userID+"?"+query.Encode(), nil, nil)
		if values := userSettingsHTTPMap(t, response); !reflect.DeepEqual(values, want) {
			t.Fatal("client metadata, query authentication, or a second device selected different preferences")
		}
	}
	// Replacing the identity store proves the HTTP response is backed by the database.
	f.app.identity = identity.New(f.pool)
	response := f.request(t, http.MethodGet, path, nil, accounts.second.headers)
	if values := userSettingsHTTPMap(t, response); !reflect.DeepEqual(values, want) {
		t.Fatal("a new identity store lost committed preferences")
	}
	head := f.request(t, http.MethodHead, path+"?Client=emby", nil, accounts.second.headers)
	expectStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(response.Body.Len()) ||
		head.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HEAD did not retain the GET representation length without its body")
	}
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, "/emby/UserSettings/"+accounts.other.userID, nil, accounts.other.headers)); len(values) != 0 {
		t.Fatal("a different user inherited another user's preferences")
	}
	if userSettingsHTTPAccountState(t, f) != accountState {
		t.Fatal("preference requests changed UserConfiguration, policy, or account management revisions")
	}
}

func TestHTTPUserSettingsPartialPreservesObservedScalarAndDuplicateSemantics(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/emby/UserSettings/" + accounts.viewer.userID
	expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"unseen":""}`), accounts.viewer.headers), http.StatusNoContent)
	if userSettingsHTTPPersisted(t, f) != "[]" {
		t.Fatal("an empty string for a missing preference created a stored row")
	}
	body := []byte(`{"CaseKey":"first","CaseKey":"second","CASEKEY":"last","integer":12,"enabled":true,"disabled":false,"retained":"yes","object":{"nested":"value"},"array":["value"]}`)
	expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial?reqformat=json", "text/plain", body, accounts.viewer.headers), http.StatusNoContent)
	want := map[string]string{
		"CaseKey": "last", "integer": "12", "enabled": "true", "disabled": "false", "retained": "yes",
		"object": "{nested:value}", "array": "[value]",
	}
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, path, nil, accounts.second.headers)); !reflect.DeepEqual(values, want) {
		t.Fatal("Partial lost first key spelling, last duplicate value, or observed value-to-string conversion")
	}
	expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"CaseKey":null,"retained":"","absent":"","added":"new"}`), accounts.second.headers), http.StatusNoContent)
	delete(want, "CaseKey")
	delete(want, "retained")
	want["added"] = "new"
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)); !reflect.DeepEqual(values, want) {
		t.Fatal("Partial replaced unrelated keys or failed to treat null and empty strings as deletion")
	}
	before := userSettingsHTTPPersisted(t, f)
	for _, body := range []string{`{}`, `{"integer":"12","absent":null,"unseen":""}`} {
		expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(body), accounts.viewer.headers), http.StatusNoContent)
	}
	if userSettingsHTTPPersisted(t, f) != before {
		t.Fatal("unchanged Partial updates altered persisted values or timestamps")
	}
}

func TestHTTPUserSettingsRequireCurrentIdentityAndWriteAuthority(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/emby/UserSettings/" + accounts.viewer.userID
	expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"dark"}`), accounts.admin.headers), http.StatusNoContent)
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, path, nil, accounts.admin.headers)); values["theme"] != "dark" {
		t.Fatal("the administrator could not read the target user's committed preferences")
	}
	before := userSettingsHTTPPersisted(t, f)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		target := path
		if method == http.MethodPost {
			target += "/Partial"
		}
		other := userSettingsHTTPRaw(t, f, method, target, "text/plain", []byte(`{"theme":"foreign"}`), accounts.other.headers)
		if method == http.MethodGet {
			if values := userSettingsHTTPMap(t, other); len(values) != 0 {
				t.Fatal("another user's path changed the authenticated reader's own preference map")
			}
		} else {
			expectStatus(t, other, http.StatusForbidden)
		}
		expectStatus(t, userSettingsHTTPRaw(t, f, method, target, "text/plain", []byte(`{"theme":"cookie"}`), nil, accounts.cookie), http.StatusUnauthorized)
		expectStatus(t, userSettingsHTTPRaw(t, f, method, target, "text/plain", []byte(`{"theme":"anonymous"}`), nil), http.StatusUnauthorized)
	}
	if values := userSettingsHTTPMap(t, f.request(t, http.MethodGet, "/emby/UserSettings/"+accounts.other.userID, nil, accounts.viewer.headers)); !reflect.DeepEqual(values, map[string]string{"theme": "dark"}) {
		t.Fatal("a different path user selected preferences instead of the authenticated reader")
	}
	for _, test := range []struct {
		name, invalidate, restore, id string
	}{
		{"revoked", "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", "UPDATE sessions SET revoked_at=NULL WHERE id=$1", accounts.viewer.id},
		{"disabled", "UPDATE users SET is_disabled=true WHERE id=$1", "UPDATE users SET is_disabled=false WHERE id=$1", accounts.viewer.userID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, test.invalidate, test.id); err != nil {
				t.Fatal("invalidate the owned preference identity")
			}
			expectStatus(t, f.request(t, http.MethodGet, path, nil, accounts.viewer.headers), http.StatusUnauthorized)
			expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"inactive"}`), accounts.viewer.headers), http.StatusUnauthorized)
			if _, err := f.pool.Exec(f.ctx, test.restore, test.id); err != nil {
				t.Fatal("restore the owned preference identity")
			}
		})
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=false WHERE id=$1", accounts.admin.userID); err != nil {
		t.Fatal("demote the owned administrator")
	}
	expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"demoted"}`), accounts.admin.headers), http.StatusForbidden)
	if userSettingsHTTPPersisted(t, f) != before {
		t.Fatal("a forbidden, inactive, or wrong-audience request changed preferences")
	}
}

func TestHTTPUserSettingsApplicationKeyUsesExplicitUserAndRevocation(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	viewer, err := a.users.CreateUser(a.ctx, "Application Preference Viewer", "preference-viewer-password", false)
	if err != nil {
		t.Fatal("create the owned application preference target")
	}
	key := a.create(t, "User preference application")
	headers := http.Header{
		"X-Emby-Token": {key.token}, "X-Emby-Client": {"UserSettings Integration"},
		"X-Emby-Device-Id": {"user-settings-application"}, "X-Emby-Device-Name": {"Linux Fixture"},
	}
	path := "/emby/UserSettings/" + viewer.ID
	accountState := userSettingsHTTPAccountState(t, a.serverFixture)
	expectStatus(t, userSettingsHTTPRaw(t, a.serverFixture, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"application"}`), headers), http.StatusNoContent)
	if values := userSettingsHTTPMap(t, a.request(t, http.MethodGet, path, nil, headers)); !reflect.DeepEqual(values, map[string]string{"theme": "application"}) {
		t.Fatal("application authority did not read the explicitly selected user's preferences")
	}
	var count int
	var storedUser string
	if a.pool.QueryRow(a.ctx, "SELECT count(*), min(user_id) FROM user_settings").Scan(&count, &storedUser) != nil || count != 1 || storedUser != viewer.ID {
		t.Fatal("an application preference write fabricated a user or a credential-owned preference row")
	}
	before := userSettingsHTTPPersisted(t, a.serverFixture)
	expectStatus(t, a.request(t, http.MethodGet, "/emby/UserSettings/missing-preference-target", nil, headers), http.StatusNotFound)
	principal, err := a.users.ResolveEmby(a.ctx, key.token)
	if err != nil || !principal.IsApplicationKey() {
		t.Fatal("resolve the owned userless application credential")
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal("revoke the owned application credential")
	}
	expectStatus(t, a.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized)
	expectStatus(t, userSettingsHTTPRaw(t, a.serverFixture, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"revoked"}`), headers), http.StatusUnauthorized)
	if userSettingsHTTPPersisted(t, a.serverFixture) != before || userSettingsHTTPAccountState(t, a.serverFixture) != accountState {
		t.Fatal("application preference access changed unrelated account state or retained authority after revocation")
	}
}

func TestHTTPUserSettingsFullWritePreservesExactRejectionWithoutMutation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/emby/UserSettings/" + accounts.viewer.userID
	accountState := userSettingsHTTPAccountState(t, f)
	for _, populated := range []bool{false, true} {
		if populated {
			expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path+"/Partial", "text/plain", []byte(`{"theme":"retained"}`), accounts.viewer.headers), http.StatusNoContent)
		}
		before := userSettingsHTTPPersisted(t, f)
		for _, mediaType := range []string{"text/plain", "application/json", "application/octet-stream"} {
			response := userSettingsHTTPRaw(t, f, http.MethodPost, path+"?reqformat=json", mediaType, []byte(`{"theme":"replacement"}`), accounts.viewer.headers)
			expectEmbyTextError(t, response, http.StatusBadRequest, "Expected configuration type is UserSettings")
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("the full-write rejection became cacheable")
			}
		}
		if userSettingsHTTPPersisted(t, f) != before {
			t.Fatal("a rejected full write created a row or changed persisted preferences")
		}
	}
	if userSettingsHTTPAccountState(t, f) != accountState {
		t.Fatal("rejected full writes changed independent configuration or policy")
	}
}

func TestHTTPUserSettingsInvalidInputAndLimitsLeaveNoPreferenceChanges(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/emby/UserSettings/" + accounts.viewer.userID + "/Partial"
	accountState := userSettingsHTTPAccountState(t, f)
	entries := make(map[string]string, identity.MaxUserSettingsEntries+1)
	for index := 0; index <= identity.MaxUserSettingsEntries; index++ {
		entries["key"+strconv.Itoa(index)] = "value"
	}
	entryBody, err := json.Marshal(entries)
	if err != nil {
		t.Fatal("encode the entry-count boundary")
	}
	total := make(map[string]string, 9)
	for index := 0; index < 9; index++ {
		total["key"+strconv.Itoa(index)] = strings.Repeat("v", identity.MaxUserSettingsValueBytes)
	}
	totalBody, err := json.Marshal(total)
	if err != nil {
		t.Fatal("encode the total-size boundary")
	}
	for _, populated := range []bool{false, true} {
		if populated {
			expectStatus(t, userSettingsHTTPRaw(t, f, http.MethodPost, path, "text/plain", []byte(`{"theme":"retained"}`), accounts.viewer.headers), http.StatusNoContent)
		}
		before := userSettingsHTTPPersisted(t, f)
		for _, test := range []struct {
			name, suffix, mediaType string
			body                    []byte
			status                  int
		}{
			{"malformed", "", "text/plain", []byte(`{"theme":`), http.StatusBadRequest},
			{"trailing-object", "", "text/plain", []byte(`{"theme":"changed"}{}`), http.StatusBadRequest},
			{"invalid-utf8", "", "text/plain", []byte("{\"theme\":\"\xff\"}"), http.StatusBadRequest},
			{"unpaired-surrogate", "", "text/plain", []byte(`{"theme":"\ud800"}`), http.StatusBadRequest},
			{"control-key", "", "text/plain", []byte(`{"bad\nkey":"changed"}`), http.StatusBadRequest},
			{"nul-value", "", "text/plain", []byte(`{"theme":"bad\u0000value"}`), http.StatusBadRequest},
			{"key-limit", "", "text/plain", []byte(`{"` + strings.Repeat("k", identity.MaxUserSettingsKeyBytes+1) + `":"changed"}`), http.StatusRequestEntityTooLarge},
			{"value-limit", "", "text/plain", []byte(`{"theme":"` + strings.Repeat("v", identity.MaxUserSettingsValueBytes+1) + `"}`), http.StatusRequestEntityTooLarge},
			{"entry-limit", "", "text/plain", entryBody, http.StatusRequestEntityTooLarge},
			{"total-limit", "", "text/plain", totalBody, http.StatusRequestEntityTooLarge},
			{"body-limit", "", "text/plain", bytes.Repeat([]byte(" "), maxUserSettingsBodyBytes+1), http.StatusRequestEntityTooLarge},
			{"unsupported-type", "", "application/xml", []byte(`{"theme":"changed"}`), http.StatusUnsupportedMediaType},
			{"unsupported-charset", "", "text/plain; charset=iso-8859-1", []byte(`{"theme":"changed"}`), http.StatusUnsupportedMediaType},
			{"duplicate-query", "?reqformat=json&REQFORMAT=json", "text/plain", []byte(`{"theme":"changed"}`), http.StatusBadRequest},
			{"unknown-query", "?UserId=" + accounts.other.userID, "text/plain", []byte(`{"theme":"changed"}`), http.StatusBadRequest},
		} {
			response := userSettingsHTTPRaw(t, f, http.MethodPost, path+test.suffix, test.mediaType, test.body, accounts.viewer.headers)
			if response.Code != test.status {
				t.Fatalf("%s populated=%t returned HTTP %d, want %d", test.name, populated, response.Code, test.status)
			}
			if userSettingsHTTPPersisted(t, f) != before {
				t.Fatalf("%s populated=%t changed preference values, timestamps, or row existence", test.name, populated)
			}
		}
	}
	if userSettingsHTTPAccountState(t, f) != accountState {
		t.Fatal("invalid preference requests changed independent account configuration or policy")
	}
}
