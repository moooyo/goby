package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestHTTPItemsObservedPlaylistAndBoxSetFiltersUseAuthorizedCatalog(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Collection Query Viewer", "collection-query-password", false)
	if err != nil {
		t.Fatal("create collection query viewer")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('collection-visible','Visible Catalog','mixed'),('collection-hidden','Hidden Catalog','mixed');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('collection-visible','collection-visible',NULL,'Visible Catalog','Visible Catalog','CollectionFolder',true),
		('collection-hidden','collection-hidden',NULL,'Hidden Catalog','Hidden Catalog','CollectionFolder',true),
		('movie-visible','collection-visible','collection-visible','A Visible Movie','A Visible Movie','Movie',false),
		('movie-hidden','collection-hidden','collection-hidden','0 Hidden Movie','0 Hidden Movie','Movie',false)`); err != nil {
		t.Fatalf("seed authorized collection query catalog: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["collection-visible"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal("restrict collection query viewer library access")
	}
	login := f.embyLogin(t, viewer.Name, "collection-query-password")
	token := stringValue(t, login, "AccessToken")
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)
	client := server.Client()
	send := func(types string, extra url.Values) *httptest.ResponseRecorder {
		t.Helper()
		// These are the actual failing client parameters. X-Emby-Token is its
		// verified query credential carrier; no fixture result is substituted.
		query := url.Values{"Fields": {"PrimaryImageAspectRatio"}, "Recursive": {"true"},
			"IncludeItemTypes": {types}, "X-Emby-Token": {token}}
		for name, values := range extra {
			query[name] = append([]string(nil), values...)
		}
		request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, server.URL+"/emby/Users/"+viewer.ID+"/Items?"+query.Encode(), nil)
		if err != nil {
			t.Fatal("construct observed collection filter request")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("perform observed collection filter request: %T", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatalf("read observed collection filter response: %T", err)
		}
		result := httptest.NewRecorder()
		for name, values := range response.Header {
			result.Header()[name] = append([]string(nil), values...)
		}
		result.WriteHeader(response.StatusCode)
		_, _ = result.Write(data)
		return result
	}
	for _, kind := range []string{"Playlist", "BoxSet"} {
		items, total := responseItems(t, send(kind, nil))
		if len(items) != 0 || total != 0 {
			t.Fatal("an absent client-requested kind returned unrelated catalog objects")
		}
	}
	// Seed catalog representations directly to prove real nonempty filtering.
	// This change does not introduce playlist/collection creation or editing.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('playlist-visible','collection-visible','collection-visible','B Visible Playlist','B Visible Playlist','Playlist',true),
		('folder-visible','collection-visible','collection-visible','Nested Folder','Nested Folder','Folder',true),
		('playlist-nested','collection-visible','folder-visible','C Nested Playlist','C Nested Playlist','Playlist',true),
		('boxset-visible','collection-visible','collection-visible','D Visible Collection','D Visible Collection','BoxSet',true),
		('playlist-hidden','collection-hidden','collection-hidden','0 Hidden Playlist','0 Hidden Playlist','Playlist',true),
		('boxset-hidden','collection-hidden','collection-hidden','0 Hidden Collection','0 Hidden Collection','BoxSet',true)`); err != nil {
		t.Fatalf("seed real collection-kind item representations: %v", err)
	}
	for _, test := range []struct {
		name, types string
		query       url.Values
		ids         []string
		total       int
	}{
		{"playlist", "Playlist", nil, []string{"playlist-visible", "playlist-nested"}, 2},
		{"boxset", "BoxSet", nil, []string{"boxset-visible"}, 1},
		{"mixed", "Movie,Playlist", nil, []string{"movie-visible", "playlist-visible", "playlist-nested"}, 3},
		{"page", "Movie,Playlist", url.Values{"StartIndex": {"1"}, "Limit": {"1"}}, []string{"playlist-visible"}, 3},
		{"zero_limit", "Movie,Playlist", url.Values{"Limit": {"0"}}, []string{}, 3},
		{"nested_scope", "Playlist", url.Values{"ParentId": {"folder-visible"}}, []string{"playlist-nested"}, 1},
		{"direct_scope", "Playlist", url.Values{"ParentId": {"collection-visible"}, "Recursive": {"false"}}, []string{"playlist-visible"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			items, total := responseItems(t, send(test.types, test.query))
			ids := make([]string, 0, len(items))
			for _, item := range items {
				ids = append(ids, stringValue(t, item, "Id"))
			}
			if !reflect.DeepEqual(ids, test.ids) || total != test.total {
				t.Fatalf("filtered catalog IDs/total = %v/%d, want %v/%d", ids, total, test.ids, test.total)
			}
		})
	}
	expectAPIError(t, send("Playlist,Unknown", nil), http.StatusBadRequest, "invalid_input", true)
	expectAPIError(t, send("Playlist", url.Values{"ParentId": {"collection-hidden"}}), http.StatusNotFound, "not_found", true)
}
