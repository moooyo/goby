package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestHTTPUserConfigurationReadsPersistedPreferencesWithoutChangingAuthority(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "User Configuration Viewer", "configuration-viewer-password", false)
	if err != nil {
		t.Fatal("create user configuration viewer")
	}
	login := f.embyLogin(t, viewer.Name, "configuration-viewer-password")
	if got := objectValue(t, objectValue(t, login, "User"), "Configuration"); !reflect.DeepEqual(got, observedUserConfigurationObject(t)) {
		t.Fatal("fresh login did not project the reference viewer configuration defaults")
	}
	token := stringValue(t, login, "AccessToken")
	headers := http.Header{"X-Emby-Token": {token}}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('configuration-visible','Visible Configuration Library','mixed'),('configuration-hidden','Hidden Configuration Library','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('configuration-visible','configuration-visible','Visible Configuration Library','Visible Configuration Library','CollectionFolder',true),
		('configuration-hidden','configuration-hidden','Hidden Configuration Library','Hidden Configuration Library','CollectionFolder',true)`); err != nil {
		t.Fatal("seed user configuration library scopes")
	}
	stored := `{"OrderedViews":["configuration-hidden","configuration-visible","configuration-hidden"],
		"LatestItemsExcludes":["configuration-visible"],"MyMediaExcludes":["configuration-hidden"],
		"HidePlayedInLatest":false,"ResumeRewindSeconds":10,"SubtitleMode":"Always",
		"EnableAllFolders":true,"IsAdministrator":true,"ProfilePin":"retained-private-pin"}`
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET configuration=$2::jsonb,
		policy='{"EnableAllFolders":false,"EnabledFolders":["configuration-visible"]}'::jsonb WHERE id=$1`, viewer.ID, stored); err != nil {
		t.Fatal("persist independent configuration and restrictive policy")
	}
	var before string
	if err := f.pool.QueryRow(f.ctx, "SELECT to_jsonb(u)::text FROM users u WHERE id=$1", viewer.ID).Scan(&before); err != nil {
		t.Fatal("snapshot user configuration before reads")
	}
	expected := userConfigurationObject(t, projectUserConfiguration(json.RawMessage(stored)))
	for _, response := range []map[string]any{
		jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, headers)),
		objectValue(t, f.embyLogin(t, viewer.Name, "configuration-viewer-password"), "User"),
	} {
		if !reflect.DeepEqual(objectValue(t, response, "Configuration"), expected) {
			t.Fatal("current user or new login response lost persisted configuration")
		}
		policy := objectValue(t, response, "Policy")
		if policy["IsAdministrator"] != false || policy["EnableAllFolders"] != false {
			t.Fatal("display configuration changed authorization policy")
		}
	}
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Views", nil, headers))
	if len(views) != 1 || total != 1 || views[0]["Id"] != "configuration-visible" {
		t.Fatal("ordered/excluded view preferences granted a hidden library")
	}
	var after string
	if err := f.pool.QueryRow(f.ctx, "SELECT to_jsonb(u)::text FROM users u WHERE id=$1", viewer.ID).Scan(&after); err != nil || before != after {
		t.Fatal("configuration projection or authentication changed persisted account fields")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET configuration='{"OrderedViews":null,"LatestItemsExcludes":"bad","MyMediaExcludes":[null],"HidePlayedInLatest":"false"}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal("seed wrong-typed but valid JSON configuration")
	}
	response := f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, headers)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(objectValue(t, jsonObject(t, response), "Configuration"), observedUserConfigurationObject(t)) {
		t.Fatal("wrong-typed stored preferences produced unsafe client configuration")
	}
}
