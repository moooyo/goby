package server

import (
	"net/http"
	"testing"
)

func TestHTTPListMembershipGuardOnlyReturnsProvableEmptyResults(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "List guard viewer", "list-guard-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('list-visible','Visible','mixed'),('list-hidden','Hidden','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('list-hidden-playlist','list-hidden','Hidden Playlist','hidden playlist','Playlist',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["list-visible"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "list-guard-viewer-password"), "AccessToken")}}
	path := "/emby/Users/" + viewer.ID + "/Items?Recursive=true&IncludeItemTypes=Playlist,BoxSet&SortBy=SortName&ListItemIds=album-id"
	assertEmpty := func(path string) {
		t.Helper()
		items, total := responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
		if total != 0 || len(items) != 0 {
			t.Fatal("an empty current-ACL candidate set did not prove the empty intersection")
		}
	}
	assertEmpty(path)
	for _, kind := range []string{"Playlist", "BoxSet"} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
			VALUES($1,'list-visible',$1,$1,$2,true)`, "visible-"+kind, kind); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", "&Limit=0", "&StartIndex=1000&Limit=1"} {
			response := f.request(t, http.MethodGet, path+suffix, nil, headers)
			expectAPIError(t, response, http.StatusNotImplemented, "unsupported_filter", true)
			if _, exists := jsonObject(t, response)["TotalRecordCount"]; exists {
				t.Fatal("an unknown membership result claimed a total count")
			}
		}
	}
	assertEmpty(path + "&ExcludeItemIds=visible-Playlist,visible-BoxSet")
	assertEmpty(path + "&SearchTerm=NoSuchVisibleList")
	for _, route := range []string{
		"/emby/Users/" + viewer.ID + "/Items/Latest", "/emby/Users/" + viewer.ID + "/Items/Resume",
		"/emby/Shows/NextUp", "/emby/Genres",
	} {
		expectAPIError(t, f.request(t, http.MethodGet, route+"?ListItemIds=album-id", nil, headers), http.StatusNotImplemented, "unsupported_filter", true)
	}
	for _, suffix := range []string{"ListItemIds=", "ListItemIds=album,"} {
		expectAPIError(t, f.request(t, http.MethodGet, "/emby/Items?"+suffix, nil, headers), http.StatusBadRequest, "invalid_input", true)
	}
}
