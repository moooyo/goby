package server

import (
	"net/http"
	"testing"
)

func TestHTTPEmbyUserManagementUsesSDKResponsesAndSharedMutations(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	created := f.request(t, http.MethodPost, "/emby/Users/New", map[string]any{"Name": "SDK Viewer"}, headers)
	expectStatus(t, created, http.StatusOK)
	user := jsonObject(t, created)
	id := stringValue(t, user, "Id")
	if user["Name"] != "SDK Viewer" || user["HasPassword"] != false || objectValue(t, user, "Policy")["IsAdministrator"] != false {
		t.Fatalf("create did not return a passwordless member UserDto: %#v", user)
	}
	password := f.request(t, http.MethodPost, "/emby/Users/"+id+"/Password", map[string]any{"Id": id, "NewPw": "sdk-viewer-password", "ResetPassword": false}, headers)
	expectStatus(t, password, http.StatusOK)
	if password.Body.Len() != 0 {
		t.Fatal("SDK password write must return an empty success response")
	}
	viewer := f.embyLogin(t, "SDK Viewer", "sdk-viewer-password")
	viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, viewer, "AccessToken")}}
	user["Name"] = "Renamed SDK Viewer"
	user["Policy"] = map[string]any{"IsAdministrator": true}
	renamed := f.request(t, http.MethodPost, "/emby/Users/"+id, user, headers)
	expectStatus(t, renamed, http.StatusOK)
	if renamed.Body.Len() != 0 {
		t.Fatal("SDK account write must return an empty success response")
	}
	current := f.request(t, http.MethodGet, "/emby/Users/"+id, nil, headers)
	expectStatus(t, current, http.StatusOK)
	if jsonObject(t, current)["Name"] != "Renamed SDK Viewer" || objectValue(t, jsonObject(t, current), "Policy")["IsAdministrator"] != false {
		t.Fatal("name update consumed role claims from its read-only UserDto envelope")
	}
	policy := f.request(t, http.MethodPost, "/emby/Users/"+id+"/Policy", map[string]any{"IsHidden": true, "RemoteClientBitrateLimit": 4000000, "SimultaneousStreamLimit": 2}, headers)
	expectStatus(t, policy, http.StatusOK)
	if policy.Body.Len() != 0 {
		t.Fatal("SDK policy write must return an empty success response")
	}
	current = f.request(t, http.MethodGet, "/emby/Users/"+id, nil, headers)
	currentPolicy := objectValue(t, jsonObject(t, current), "Policy")
	if currentPolicy["IsHidden"] != true || currentPolicy["EnableAllFolders"] != true || currentPolicy["RemoteClientBitrateLimit"] != float64(4000000) {
		t.Fatalf("partial policy update lost unrelated persisted fields: %#v", currentPolicy)
	}
	for _, path := range []string{"/emby/Users/New", "/emby/Users/" + id, "/emby/Users/" + id + "/Policy"} {
		expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"Name": "Forbidden"}, viewerHeaders), http.StatusForbidden)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+adminID+"/Policy", map[string]any{"IsAdministrator": false}, headers), http.StatusConflict)
	expectStatus(t, f.request(t, http.MethodDelete, "/emby/Users/"+adminID, nil, headers), http.StatusConflict)
	deleted := f.request(t, http.MethodPost, "/emby/Users/"+id+"/Delete", nil, headers)
	expectStatus(t, deleted, http.StatusOK)
	if deleted.Body.Len() != 0 {
		t.Fatal("SDK delete alias must return an empty success response")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+id, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+id, nil, viewerHeaders), http.StatusUnauthorized)
}

func TestHTTPEmbyPasswordOwnerRequiresCurrentSecretAndRevokesOldCredentials(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	user, err := f.users.CreateUser(f.ctx, "Password Owner", "old-owner-password", false)
	if err != nil {
		t.Fatal(err)
	}
	first := f.embyLogin(t, user.Name, "old-owner-password")
	second := f.embyLogin(t, user.Name, "old-owner-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, first, "AccessToken")}}
	path := "/emby/Users/" + user.ID + "/Password"
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"NewPw": "new-owner-password"}, headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"NewPw": "new-owner-password", "CurrentPw": "incorrect"}, headers), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"ResetPassword": true}, headers), http.StatusForbidden)
	changed := f.request(t, http.MethodPost, path, map[string]any{"Id": user.ID, "NewPw": "new-owner-password", "CurrentPw": "old-owner-password"}, headers)
	expectStatus(t, changed, http.StatusOK)
	if changed.Body.Len() != 0 {
		t.Fatal("SDK owner password change must have an empty success response")
	}
	for _, token := range []string{stringValue(t, first, "AccessToken"), stringValue(t, second, "AccessToken")} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+user.ID, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized)
	}
	f.embyLogin(t, user.Name, "new-owner-password")
}

func TestHTTPEmbyUserWritesRejectAmbiguousTargetsAndUnsupportedCapabilities(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	for _, body := range []map[string]any{
		{"Name": "Copied", "CopyFromUserId": adminID, "UserCopyOptions": []string{"Unknown"}},
		{"Name": "Copied", "UserCopyOptions": []string{"UserPolicy"}},
		{"Name": "Copied", "CopyFromUserId": adminID, "UserCopyOptions": []string{"UserPolicy", "UserPolicy"}},
		{"Name": "Password Shortcut", "Password": "unrecognized"},
	} {
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/New", body, headers), http.StatusBadRequest)
	}
	for _, body := range []map[string]any{
		{"EnableLiveTvAccess": true}, {"EnableSyncTranscoding": true}, {"EnablePublicSharing": true},
		{"LockedOutDate": 1}, {"InvalidLoginAttemptCount": 1}, {"EnabledDevices": []any{nil}},
	} {
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+adminID+"/Policy", body, headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+adminID, map[string]any{"Id": "different-user", "Name": "Ambiguous"}, headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+adminID+"/Password", map[string]any{"Id": "different-user", "NewPw": "new-password"}, headers), http.StatusBadRequest)
}

func TestHTTPEmbyNewUserCopiesOnlyExplicitSDKFacets(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	source, err := f.users.CreateUser(f.ctx, "SDK Copy Source", "source-password", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"IsHidden":true,"RemoteClientBitrateLimit":1234567}'::jsonb,
		configuration='{"SubtitleMode":"Always","Pin":"private-copy-pin"}'::jsonb WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	created := f.request(t, http.MethodPost, "/emby/Users/New", map[string]any{
		"Name": "SDK Copied User", "CopyFromUserId": source.ID, "UserCopyOptions": []string{"UserPolicy", "UserConfiguration", "UserData"},
	}, headers)
	expectStatus(t, created, http.StatusOK)
	user := jsonObject(t, created)
	policy := objectValue(t, user, "Policy")
	configuration := objectValue(t, user, "Configuration")
	if user["Name"] != "SDK Copied User" || user["HasPassword"] != false || policy["IsAdministrator"] != false ||
		policy["IsHidden"] != true || policy["RemoteClientBitrateLimit"] != float64(1234567) || configuration["SubtitleMode"] != "Always" {
		t.Fatalf("copy returned incorrect UserDto facts: %#v", user)
	}
	if _, found := configuration["Pin"]; found {
		t.Fatal("copy exposed a source PIN")
	}
	defaults := f.request(t, http.MethodPost, "/emby/Users/New", map[string]any{
		"Name": "SDK Copy Without Facets", "CopyFromUserId": source.ID,
	}, headers)
	expectStatus(t, defaults, http.StatusOK)
	defaultUser := jsonObject(t, defaults)
	if objectValue(t, defaultUser, "Policy")["IsHidden"] != false || objectValue(t, defaultUser, "Configuration")["SubtitleMode"] != "Smart" {
		t.Fatal("omitted copy options selected facets implicitly")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/New", map[string]any{
		"Name": "Missing SDK Copy", "CopyFromUserId": "missing-source", "UserCopyOptions": []string{"UserData"},
	}, headers), http.StatusNotFound)
}
