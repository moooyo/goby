package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func managedHTTPCreate(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf, name string, administrator bool) string {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/users", map[string]any{
		"Name": name, "Password": "managed-user-password", "IsAdministrator": administrator,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusCreated)
	user := objectValue(t, jsonObject(t, response), "User")
	if len(user) != 6 {
		t.Fatalf("create response changed its six-field user contract: %#v", user)
	}
	return stringValue(t, user, "Id")
}

func managedHTTPDetail(t *testing.T, f *serverFixture, cookie *http.Cookie, id string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/users/"+id, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	return objectValue(t, jsonObject(t, response), "User")
}

func managedHTTPUpdateBody(user map[string]any) map[string]any {
	policy := make(map[string]any)
	for key, value := range user["Policy"].(map[string]any) {
		policy[key] = value
	}
	return map[string]any{
		"Revision": user["Revision"], "Name": user["Name"], "IsAdministrator": user["IsAdministrator"],
		"IsDisabled": user["IsDisabled"], "Policy": policy,
	}
}

func managedHTTPLogin(t *testing.T, f *serverFixture, name, password string) (*http.Cookie, string) {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": name, "Password": password,
	}, nil)
	expectStatus(t, response, http.StatusOK)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie {
			return cookie, stringValue(t, jsonObject(t, response), "CSRFToken")
		}
	}
	t.Fatal("managed administrator login did not return a session cookie")
	return nil, ""
}

func expectManagedFieldError(t *testing.T, response *httptest.ResponseRecorder, field string) {
	t.Helper()
	expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
	object := jsonObject(t, response)
	stringValue(t, objectValue(t, objectValue(t, object, "Error"), "Fields"), field)
	if stringValue(t, object, "RequestId") != response.Header().Get("X-Request-Id") {
		t.Error("managed user error lost its request identifier")
	}
}

func expectManagedCookieCleared(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if jsonObject(t, response)["CurrentSessionRevoked"] != true {
		t.Fatal("self-revoking mutation did not report CurrentSessionRevoked")
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie {
			if cookie.Value != "" || cookie.MaxAge != -1 || cookie.Path != "/admin" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("revoked cookie has incorrect clearing attributes: %+v", cookie)
			}
			return
		}
	}
	t.Fatal("self-revoking mutation did not clear the administrator cookie")
}

func TestHTTPManagedUserReadContractAndAuthorization(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	viewerID := managedHTTPCreate(t, f, cookie, csrf, "Managed Reader", false)
	setHTTPUserPolicy(t, f, adminID, `{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":false,"EnablePlaybackRemuxing":true,"SensitiveMarker":"`+userPolicySecretMarker+`"}`)
	user := managedHTTPDetail(t, f, cookie, adminID)
	if len(user) != 8 || user["Id"] != adminID || user["IsAdministrator"] != true {
		t.Fatalf("managed detail must retain the six user fields plus revision and policy: %#v", user)
	}
	if revision, err := strconv.ParseInt(stringValue(t, user, "Revision"), 10, 64); err != nil || revision < 1 {
		t.Fatalf("managed revision must be a positive decimal string: %#v", user["Revision"])
	}
	policy := objectValue(t, user, "Policy")
	if len(policy) != 6 || policy["EnableAllFolders"] != false || policy["EnableMediaPlayback"] != false || policy["EnablePlaybackRemuxing"] != true {
		t.Fatalf("native policy must show configured facts without administrator or runtime overrides: %#v", policy)
	}
	if folders, ok := policy["EnabledFolders"].([]any); !ok || len(folders) != 0 {
		t.Fatalf("configured empty folders must be a non-null array: %#v", policy["EnabledFolders"])
	}
	assertNoStoredPolicyExposure(t, user, "Password", "PasswordHash", "Token", "TokenHash", "AccessToken", "SensitiveMarker")
	list := f.request(t, http.MethodGet, "/admin/v1/users", nil, nil, cookie)
	expectStatus(t, list, http.StatusOK)
	for _, entry := range jsonObject(t, list)["Items"].([]any) {
		if len(entry.(map[string]any)) != 6 {
			t.Fatal("existing user list must retain its six-field projection")
		}
	}
	path := "/admin/v1/users/" + viewerID
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized, "authentication_required", false)
	viewer := f.embyLogin(t, "Managed Reader", "managed-user-password")
	token := stringValue(t, viewer, "AccessToken")
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil, &http.Cookie{Name: sessionCookie, Value: token}), http.StatusUnauthorized, "invalid_credentials", false)
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{"Name": "Managed Reader", "Password": "managed-user-password"}, nil), http.StatusUnauthorized, "invalid_credentials", false)
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/users/missing-user", nil, nil, cookie), http.StatusNotFound, "not_found", false)
	input := managedHTTPUpdateBody(managedHTTPDetail(t, f, cookie, viewerID))
	expectAPIError(t, f.request(t, http.MethodPut, path, input, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodPut, path, input, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://foreign.example.test"}}, cookie), http.StatusForbidden, "origin_denied", false)
	expectAPIError(t, f.request(t, http.MethodPost, path+"/password", map[string]any{"Revision": input["Revision"], "Password": "replacement-password"}, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
}

func TestHTTPManagedUserAtomicUpdateConflictAndEmbyProjection(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := managedHTTPCreate(t, f, cookie, csrf, "Managed Viewer", false)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries (id, name, collection_type) VALUES
		('managed-library-a', 'Library A', 'movies'), ('managed-library-b', 'Library B', 'music')`); err != nil {
		t.Fatalf("create isolated managed policy libraries: %v", err)
	}
	login := f.embyLogin(t, "Managed Viewer", "managed-user-password")
	token := stringValue(t, login, "AccessToken")
	before := managedHTTPDetail(t, f, cookie, id)
	input := managedHTTPUpdateBody(before)
	input["Name"] = "Renamed Viewer"
	policy := input["Policy"].(map[string]any)
	policy["EnableAllFolders"] = false
	policy["EnabledFolders"] = []string{"managed-library-b", "managed-library-a", "managed-library-b"}
	policy["EnableMediaPlayback"] = false
	policy["EnablePlaybackRemuxing"] = true
	policy["EnableAudioPlaybackTranscoding"] = true
	policy["EnableVideoPlaybackTranscoding"] = true
	response := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusOK)
	result := jsonObject(t, response)
	if result["CurrentSessionRevoked"] != false || len(response.Result().Cookies()) != 0 {
		t.Fatal("editing another user's policy must not revoke the caller's administrator session")
	}
	updated := objectValue(t, result, "User")
	oldRevision, _ := strconv.ParseInt(stringValue(t, before, "Revision"), 10, 64)
	if updated["Revision"] != strconv.FormatInt(oldRevision+1, 10) || updated["Name"] != "Renamed Viewer" || updated["IsAdministrator"] != false || updated["IsDisabled"] != false {
		t.Fatalf("managed update did not atomically replace account fields: %#v", updated)
	}
	updatedPolicy := objectValue(t, updated, "Policy")
	if updatedPolicy["EnableAllFolders"] != false || updatedPolicy["EnableMediaPlayback"] != false || updatedPolicy["EnablePlaybackRemuxing"] != true || !reflect.DeepEqual(updatedPolicy["EnabledFolders"], []any{"managed-library-a", "managed-library-b"}) {
		t.Fatalf("managed policy update was not complete or canonical: %#v", updatedPolicy)
	}
	if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), updated) {
		t.Fatal("managed mutation response differs from its persisted detail")
	}
	emby := f.request(t, http.MethodGet, "/emby/Users/"+id, nil, http.Header{"X-Emby-Token": {token}})
	expectStatus(t, emby, http.StatusOK)
	projected := jsonObject(t, emby)
	embyPolicy := objectValue(t, projected, "Policy")
	assertUserPolicyWhitelist(t, embyPolicy)
	if projected["Name"] != "Renamed Viewer" || embyPolicy["EnableMediaPlayback"] != false || embyPolicy["EnableAllFolders"] != false || !reflect.DeepEqual(embyPolicy["EnabledFolders"], updatedPolicy["EnabledFolders"]) {
		t.Fatalf("existing Emby session did not observe the committed native policy: %#v", projected)
	}
	input["Name"] = "Stale Overwrite"
	stale := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectAPIError(t, stale, http.StatusConflict, "revision_conflict", false)
	if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), updated) {
		t.Fatal("stale revision changed the persisted user")
	}
}

func TestHTTPManagedUserValidationIsAtomic(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := managedHTTPCreate(t, f, cookie, csrf, "Validated Viewer", false)
	before := managedHTTPDetail(t, f, cookie, id)
	for _, test := range []struct {
		name, field string
		change      func(map[string]any)
	}{
		{"empty_name", "Name", func(input map[string]any) { input["Name"] = "" }},
		{"duplicate_name", "Name", func(input map[string]any) { input["Name"] = "Administrator" }},
		{"unknown_library", "Policy.EnabledFolders", func(input map[string]any) {
			input["Name"] = "Must Not Persist"
			input["Policy"].(map[string]any)["EnabledFolders"] = []string{"missing-library"}
		}},
		{"unknown_field", "Body", func(input map[string]any) { input["ActorId"] = "untrusted-actor" }},
		{"missing_policy_flag", "Policy.EnableMediaPlayback", func(input map[string]any) { delete(input["Policy"].(map[string]any), "EnableMediaPlayback") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := managedHTTPUpdateBody(before)
			test.change(input)
			response := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectManagedFieldError(t, response, test.field)
			if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), before) {
				t.Fatal("invalid managed update partially changed the user")
			}
		})
	}
	for _, password := range []string{"", strings.Repeat("x", 73)} {
		response := f.request(t, http.MethodPost, "/admin/v1/users/"+id+"/password", map[string]any{
			"Revision": before["Revision"], "Password": password,
		}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
		expectManagedFieldError(t, response, "Password")
	}
	if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), before) {
		t.Fatal("invalid password mutation changed the revision or user")
	}
	missing := f.request(t, http.MethodPut, "/admin/v1/users/missing-user", managedHTTPUpdateBody(before), http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectAPIError(t, missing, http.StatusNotFound, "not_found", false)
}

func TestHTTPManagedUserIdentifiersRejectMalformedAndPreserveOpaqueIDs(t *testing.T) {
	f := newServerFixture(t)
	id := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	before := managedHTTPDetail(t, f, cookie, id)
	for _, route := range []struct {
		method, suffix string
		body           any
	}{
		{method: http.MethodGet},
		{method: http.MethodPut, body: managedHTTPUpdateBody(before)},
		{method: http.MethodPost, suffix: "/password", body: map[string]any{"Revision": before["Revision"], "Password": "unused-password"}},
	} {
		for _, malformed := range []string{"bad\x00id", "bad\xffid", " leading", "trailing ", "bad\nid", "bad\u0085id", strings.Repeat("a", 257)} {
			response := f.request(t, route.method, "/admin/v1/users/"+url.PathEscape(malformed)+route.suffix, route.body, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectManagedFieldError(t, response, "Id")
		}
		for _, absent := range []string{"missing-user", "opaque.user_id:42", "\u7528\u6237", strings.Repeat("a", 256)} {
			response := f.request(t, route.method, "/admin/v1/users/"+url.PathEscape(absent)+route.suffix, route.body, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectAPIError(t, response, http.StatusNotFound, "not_found", false)
		}
	}
	if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), before) {
		t.Fatal("identifier validation or absent targets changed the administrator")
	}
}

func TestHTTPManagedUserRejectsInvalidUTF8BeforeMutation(t *testing.T) {
	f := newServerFixture(t)
	id := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	before := managedHTTPDetail(t, f, cookie, id)
	update := managedHTTPUpdateBody(before)
	update["Name"] = "invalid-byte-marker"
	for _, route := range []struct {
		method, suffix string
		body           map[string]any
	}{
		{method: http.MethodPut, body: update},
		{method: http.MethodPost, suffix: "/password", body: map[string]any{"Revision": before["Revision"], "Password": "invalid-byte-marker"}},
	} {
		encoded, err := json.Marshal(route.body)
		if err != nil {
			t.Fatalf("encode invalid UTF-8 request fixture: %v", err)
		}
		raw := bytes.Replace(encoded, []byte("invalid-byte-marker"), []byte("invalid\xffvalue"), 1)
		request := httptest.NewRequest(route.method, "/admin/v1/users/"+id+route.suffix, bytes.NewReader(raw)).WithContext(f.ctx)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		expectManagedFieldError(t, response, "Body")
		if !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), before) {
			t.Fatal("invalid UTF-8 input mutated the user")
		}
	}
}

func TestHTTPManagedUserDisableEnableDoesNotReviveCredentials(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := managedHTTPCreate(t, f, cookie, csrf, "Managed Operator", true)
	operatorCookie, _ := managedHTTPLogin(t, f, "Managed Operator", "managed-user-password")
	login := f.embyLogin(t, "Managed Operator", "managed-user-password")
	token := stringValue(t, login, "AccessToken")
	input := managedHTTPUpdateBody(managedHTTPDetail(t, f, cookie, id))
	input["IsDisabled"] = true
	disabled := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, disabled, http.StatusOK)
	if jsonObject(t, disabled)["CurrentSessionRevoked"] != false {
		t.Fatal("disabling another administrator revoked the caller")
	}
	for _, phase := range []string{"disabled", "enabled_again"} {
		if phase == "enabled_again" {
			input = managedHTTPUpdateBody(objectValue(t, jsonObject(t, disabled), "User"))
			input["IsDisabled"] = false
			enabled := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectStatus(t, enabled, http.StatusOK)
		}
		expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, operatorCookie), http.StatusUnauthorized, "invalid_credentials", false)
		expectEmbyTextError(t, f.request(t, http.MethodGet, "/emby/Users/"+id, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized, embyInvalidTokenMessage)
	}
	managedHTTPLogin(t, f, "Managed Operator", "managed-user-password")
	f.embyLogin(t, "Managed Operator", "managed-user-password")
}

func TestHTTPManagedUserPasswordResetRevokesTokensAndRedactsLogs(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := managedHTTPCreate(t, f, cookie, csrf, "Password Operator", true)
	operatorCookie, _ := managedHTTPLogin(t, f, "Password Operator", "managed-user-password")
	login := f.embyLogin(t, "Password Operator", "managed-user-password")
	token := stringValue(t, login, "AccessToken")
	before := managedHTTPDetail(t, f, cookie, id)
	var logs bytes.Buffer
	f.app.log = slog.New(slog.NewJSONHandler(&logs, nil))
	newPassword := "new-managed-password-marker"
	response := f.request(t, http.MethodPost, "/admin/v1/users/"+id+"/password", map[string]any{
		"Revision": before["Revision"], "Password": newPassword,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["CurrentSessionRevoked"] != false || len(response.Result().Cookies()) != 0 {
		t.Fatal("resetting another user's password cleared the caller session")
	}
	for _, secret := range []string{newPassword, "managed-user-password", token, cookie.Value, operatorCookie.Value} {
		if strings.Contains(response.Body.String(), secret) || strings.Contains(logs.String(), secret) {
			t.Error("password mutation exposed a password or token in its response or log")
		}
	}
	if !strings.Contains(logs.String(), "reset_user_password") || !strings.Contains(logs.String(), id) {
		t.Error("password mutation did not log its safe action and target identifiers")
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, operatorCookie), http.StatusUnauthorized, "invalid_credentials", false)
	expectEmbyTextError(t, f.request(t, http.MethodGet, "/emby/Users/"+id, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized, embyInvalidTokenMessage)
	oldPassword := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{"Name": "Password Operator", "Password": "managed-user-password"}, nil)
	expectAPIError(t, oldPassword, http.StatusUnauthorized, "invalid_credentials", false)
	managedHTTPLogin(t, f, "Password Operator", newPassword)
	f.embyLogin(t, "Password Operator", newPassword)
	stale := f.request(t, http.MethodPost, "/admin/v1/users/"+id+"/password", map[string]any{
		"Revision": before["Revision"], "Password": "stale-password-replacement",
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectAPIError(t, stale, http.StatusConflict, "revision_conflict", false)
	managedHTTPLogin(t, f, "Password Operator", newPassword)
}

func TestHTTPManagedUserSelfRevocationAndLastAdministrator(t *testing.T) {
	for _, action := range []string{"last_administrator", "password", "demotion"} {
		t.Run(action, func(t *testing.T) {
			f := newServerFixture(t)
			id := f.bootstrap(t)
			cookie, csrf := f.adminLogin(t)
			before := managedHTTPDetail(t, f, cookie, id)
			input := managedHTTPUpdateBody(before)
			input["IsAdministrator"] = false
			if action == "last_administrator" {
				for _, disable := range []bool{false, true} {
					input["IsAdministrator"] = disable
					input["IsDisabled"] = disable
					response := f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
					expectAPIError(t, response, http.StatusConflict, "last_administrator", false)
					if len(response.Result().Cookies()) != 0 || !reflect.DeepEqual(managedHTTPDetail(t, f, cookie, id), before) {
						t.Fatal("last-administrator rejection changed the user or cookie")
					}
				}
				return
			}
			var response *httptest.ResponseRecorder
			if action == "password" {
				response = f.request(t, http.MethodPost, "/admin/v1/users/"+id+"/password", map[string]any{
					"Revision": before["Revision"], "Password": "new-self-password",
				}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			} else {
				managedHTTPCreate(t, f, cookie, csrf, "Remaining Administrator", true)
				response = f.request(t, http.MethodPut, "/admin/v1/users/"+id, input, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			}
			expectStatus(t, response, http.StatusOK)
			expectManagedCookieCleared(t, response)
			expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie), http.StatusUnauthorized, "invalid_credentials", false)
			if action == "password" {
				managedHTTPLogin(t, f, "Administrator", "new-self-password")
			} else {
				login := f.embyLogin(t, "Administrator", "administrator-password")
				if objectValue(t, objectValue(t, login, "User"), "Policy")["IsAdministrator"] != false {
					t.Fatal("Emby login retained the administrator role after native demotion")
				}
			}
		})
	}
}

func TestHTTPManagedUserMutationsRevalidateActorAfterMiddleware(t *testing.T) {
	for _, action := range []string{"update", "password", "create"} {
		t.Run(action, func(t *testing.T) {
			f := newServerFixture(t)
			f.bootstrap(t)
			cookie, csrf := f.adminLogin(t)
			id := managedHTTPCreate(t, f, cookie, csrf, "Race Viewer", false)
			before := managedHTTPDetail(t, f, cookie, id)
			method, target := http.MethodPut, "/admin/v1/users/"+id
			body := managedHTTPUpdateBody(before)
			body["Name"] = "Unauthorized Race Update"
			handler := f.app.updateManagedUser
			pattern := "PUT /admin/v1/users/{id}"
			if action == "password" {
				method, target, pattern = http.MethodPost, target+"/password", "POST /admin/v1/users/{id}/password"
				body = map[string]any{"Revision": before["Revision"], "Password": "unauthorized-race-password"}
				handler = f.app.resetManagedUserPassword
			} else if action == "create" {
				method, target, pattern = http.MethodPost, "/admin/v1/users", "POST /admin/v1/users"
				body = map[string]any{"Name": "Unauthorized Race Create", "Password": "unauthorized-race-password", "IsAdministrator": true}
				handler = f.app.createUser
			}
			mux := http.NewServeMux()
			mux.HandleFunc(pattern, f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
				// Model a credential revocation after middleware resolved its
				// trusted principal but before the application transaction begins.
				if err := f.users.Revoke(f.ctx, cookie.Value); err != nil {
					t.Fatalf("revoke racing actor fixture: %v", err)
				}
				handler(w, r)
			}))
			f.handler = f.app.middleware(mux)
			response := f.request(t, method, target, body, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", false)
			user, err := f.users.GetManagedUser(f.ctx, id)
			if err != nil || user.User.Name != before["Name"] || strconv.FormatInt(user.Revision, 10) != before["Revision"] {
				t.Fatalf("stale actor changed the target user: %+v, %v", user, err)
			}
			users, err := f.users.ListUsers(f.ctx)
			if err != nil || len(users) != 2 {
				t.Fatalf("stale actor created an account: count=%d, error=%v", len(users), err)
			}
		})
	}
}
