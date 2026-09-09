package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func setHTTPUserPolicy(t *testing.T, f *serverFixture, userID, raw string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", raw, userID); err != nil {
		t.Fatalf("update user policy fixture: %v", err)
	}
}

func TestHTTPUserPolicyDefaultsAndImmediatePlaybackRevocation(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/users", map[string]any{
		"Name": "Policy Viewer", "Password": "viewer-password", "IsAdministrator": false,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	createdUser := objectValue(t, jsonObject(t, created), "User")
	userID := stringValue(t, createdUser, "Id")
	assertNoStoredPolicyExposure(t, createdUser, "Policy", "EnabledFolders", "SensitiveMarker")
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, adminLogin, "AccessToken")}}
	login := f.embyLogin(t, "Policy Viewer", "viewer-password")
	viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	policy := objectValue(t, objectValue(t, login, "User"), "Policy")
	assertUserPolicyWhitelist(t, policy)
	if policy["EnableMediaPlayback"] != true || policy["EnableAllFolders"] != true {
		t.Fatalf("new user's default policy must permit implemented playback and library access: %#v", policy)
	}
	for _, test := range []struct{ name, raw string }{
		{name: "explicit_false", raw: `{"EnableMediaPlayback":false}`},
		{name: "explicit_null", raw: `{"EnableMediaPlayback":null}`},
		{name: "wrong_typed_string", raw: `{"EnableMediaPlayback":"true"}`},
		{name: "wrong_typed_array", raw: `{"EnableMediaPlayback":[]}`},
		{name: "whole_policy_false", raw: `false`},
		{name: "whole_policy_null", raw: `null`},
		{name: "whole_policy_true", raw: `true`},
		{name: "whole_policy_array", raw: `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			setHTTPUserPolicy(t, f, userID, test.raw)
			response := f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, adminHeaders)
			expectStatus(t, response, http.StatusOK)
			current := objectValue(t, jsonObject(t, response), "Policy")
			assertUserPolicyWhitelist(t, current)
			if current["EnableMediaPlayback"] != false {
				t.Errorf("current persisted playback denial was not reflected immediately: %#v", current)
			}
		})
	}
	setHTTPUserPolicy(t, f, userID, `{"EnableMediaPlayback":true,"IsDisabled":false}`)
	restored := f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, viewerHeaders)
	expectStatus(t, restored, http.StatusOK)
	if objectValue(t, jsonObject(t, restored), "Policy")["EnableMediaPlayback"] != true {
		t.Error("restored playback permission was not visible to the existing session")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", userID); err != nil {
		t.Fatalf("disable policy viewer: %v", err)
	}
	disabled := f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, adminHeaders)
	expectStatus(t, disabled, http.StatusOK)
	disabledPolicy := objectValue(t, jsonObject(t, disabled), "Policy")
	assertUserPolicyWhitelist(t, disabledPolicy)
	if disabledPolicy["IsDisabled"] != true || disabledPolicy["EnableMediaPlayback"] != false || disabledPolicy["EnableAllFolders"] != false {
		t.Errorf("disabled account policy must override the stored true playback switch: %#v", disabledPolicy)
	}
	if folders, ok := disabledPolicy["EnabledFolders"].([]any); !ok || len(folders) != 0 {
		t.Errorf("disabled user must not expose folder access: %#v", disabledPolicy["EnabledFolders"])
	}
	denied := f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, viewerHeaders)
	expectEmbyTextError(t, denied, http.StatusUnauthorized, "Access token is invalid or expired.")
}

func TestHTTPRawPolicyCannotGrantRolesOrLeakThroughUserEndpoints(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Private Policy Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create private policy viewer: %v", err)
	}
	setHTTPUserPolicy(t, f, adminID, `{"SensitiveMarker":"`+userPolicySecretMarker+`","IsAdministrator":false,"IsDisabled":true,`+
		`"EnableMediaPlayback":true,"EnableAllFolders":false,"EnabledFolders":["internal-admin-folder"]}`)
	setHTTPUserPolicy(t, f, viewer.ID, `{"SensitiveMarker":"`+userPolicySecretMarker+`","IsAdministrator":true,"IsDisabled":true,`+
		`"EnableMediaPlayback":true,"EnablePlaybackRemuxing":true,"EnableAudioPlaybackTranscoding":true,`+
		`"EnableVideoPlaybackTranscoding":true,"EnableContentDeletion":true,"EnableAllFolders":false,"EnabledFolders":["private-viewer-folder"]}`)
	cookie, _ := f.adminLogin(t)
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, adminLogin, "AccessToken")}}
	adminPolicy := objectValue(t, objectValue(t, adminLogin, "User"), "Policy")
	assertUserPolicyWhitelist(t, adminPolicy)
	assertNoStoredPolicyExposure(t, adminPolicy, "SensitiveMarker")
	if adminPolicy["IsAdministrator"] != true || adminPolicy["IsDisabled"] != false || adminPolicy["EnableAllFolders"] != true {
		t.Errorf("administrator column authority was replaced by raw policy claims: %#v", adminPolicy)
	}
	login := f.embyLogin(t, viewer.Name, "viewer-password")
	viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	viewerPolicy := objectValue(t, objectValue(t, login, "User"), "Policy")
	assertUserPolicyWhitelist(t, viewerPolicy)
	assertNoStoredPolicyExposure(t, viewerPolicy, "SensitiveMarker")
	if viewerPolicy["IsAdministrator"] != false || viewerPolicy["IsDisabled"] != false || viewerPolicy["EnableMediaPlayback"] != true || viewerPolicy["EnableAllFolders"] != false {
		t.Errorf("ordinary user's trusted role or supported permissions are incorrect: %#v", viewerPolicy)
	}
	if folders, ok := viewerPolicy["EnabledFolders"].([]any); !ok || len(folders) != 1 || folders[0] != "private-viewer-folder" {
		t.Errorf("valid explicitly restricted folder policy was not projected: %#v", viewerPolicy["EnabledFolders"])
	}
	adminAttempt := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": viewer.Name, "Password": "viewer-password",
	}, nil)
	expectAPIError(t, adminAttempt, http.StatusUnauthorized, "invalid_credentials", false)
	queryAttempt := f.request(t, http.MethodGet, "/emby/Users/Query", nil, viewerHeaders)
	expectAPIError(t, queryAttempt, http.StatusForbidden, "administrator_required", true)
	for _, target := range []string{"/admin/v1/users", "/admin/v1/session"} {
		response := f.request(t, http.MethodGet, target, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		assertNoStoredPolicyExposure(t, jsonObject(t, response), "Policy", "EnabledFolders", "SensitiveMarker")
	}
	public := f.request(t, http.MethodGet, "/emby/Users/Public", nil, nil)
	expectStatus(t, public, http.StatusOK)
	var publicUsers []any
	if err := json.Unmarshal(public.Body.Bytes(), &publicUsers); err != nil || publicUsers == nil {
		t.Fatalf("public users must be an array: %v", err)
	}
	if len(publicUsers) != 1 {
		t.Fatalf("public list must contain the enabled ordinary user: %#v", publicUsers)
	}
	publicViewer, ok := publicUsers[0].(map[string]any)
	if !ok || publicViewer["Id"] != viewer.ID {
		t.Fatalf("public list changed user identity: %#v", publicUsers)
	}
	assertNoStoredPolicyExposure(t, publicUsers, "Policy", "EnabledFolders", "SensitiveMarker")
	query := f.request(t, http.MethodGet, "/emby/Users/Query", nil, adminHeaders)
	expectStatus(t, query, http.StatusOK)
	listed := jsonObject(t, query)
	assertNoStoredPolicyExposure(t, listed, "SensitiveMarker")
	items, ok := listed["Items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("administrator user query must contain both accounts: %#v", listed)
	}
	for _, entry := range items {
		user, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("user query entry must be an object: %#v", entry)
		}
		policy := objectValue(t, user, "Policy")
		assertUserPolicyWhitelist(t, policy)
		if policy["IsAdministrator"] != (user["Id"] == adminID) || policy["IsDisabled"] != false {
			t.Errorf("listed policy trusted stored role claims: %#v", user)
		}
	}
}
