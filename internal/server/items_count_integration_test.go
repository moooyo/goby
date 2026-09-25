package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestHTTPItemsZeroLimitPreservesCountsAndAuthorization(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Count Viewer", "count-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('count-visible','Visible Count Library','movies'),('count-hidden','Hidden Count Library','movies');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('count-visible','count-visible',NULL,'Visible Count Library','Visible Count Library','CollectionFolder',true),
		('count-hidden','count-hidden',NULL,'Hidden Count Library','Hidden Count Library','CollectionFolder',true),
		('count-movie','count-visible','count-visible','Visible 100%_Movie','Visible 100%_Movie','Movie',false),
		('count-folder','count-visible','count-visible','Visible Folder','Visible Folder','Folder',true),
		('count-nested','count-visible','count-folder','Nested Movie','Nested Movie','Movie',false),
		('count-excluded','count-visible','count-visible','Excluded Folder','Excluded Folder','Folder',true),
		('count-excluded-movie','count-visible','count-excluded','Excluded Movie','Excluded Movie','Movie',false),
		('count-hidden-movie','count-hidden','count-hidden','Hidden Movie','Hidden Movie','Movie',false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=
		'{"EnableAllFolders":false,"EnabledFolders":["count-visible"],"ExcludedSubFolders":["count-excluded"]}'::jsonb
		WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,is_favorite) VALUES ($1,'count-movie',true)`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, viewer.Name, "count-viewer-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	path := "/emby/Users/" + viewer.ID + "/Items?"
	for _, test := range []struct {
		name  string
		query string
		total int
	}{
		{name: "global movies", query: "Recursive=true&IncludeItemTypes=Movie", total: 2},
		{name: "parent descendants", query: "ParentId=count-visible&Recursive=true&IncludeItemTypes=Movie", total: 2},
		{name: "direct parent", query: "ParentId=count-visible&Recursive=false&IncludeItemTypes=Movie", total: 1},
		{name: "literal search", query: "Recursive=true&IncludeItemTypes=Movie&SearchTerm=100%25_", total: 1},
		{name: "favorite", query: "Recursive=true&IncludeItemTypes=Movie&IsFavorite=true", total: 1},
		{name: "empty population", query: "Recursive=true&SearchTerm=missing-count-result", total: 0},
		{name: "exhausted offset", query: "Recursive=true&IncludeItemTypes=Movie&StartIndex=1000", total: 2},
		{name: "root libraries", query: "Recursive=false", total: 1},
		{name: "virtual root", query: "ParentId=" + url.QueryEscape(virtualRootID()) + "&Recursive=false", total: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, baseline := responseItems(t, f.request(t, http.MethodGet, path+test.query+"&Limit=1", nil, headers))
			items, total := responseItems(t, f.request(t, http.MethodGet, path+test.query+"&Limit=0", nil, headers))
			if total != test.total || total != baseline || len(items) != 0 {
				t.Fatalf("zero-limit Items/count = %v/%d, baseline=%d; want empty Items and %d", items, total, baseline, test.total)
			}
		})
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, path+"Recursive=true&IncludeItemTypes=Movie", nil, headers))
	if total != 2 || len(items) != 2 {
		t.Fatal("omitting Limit stopped returning the default page")
	}
	for _, parent := range []string{"count-hidden", "count-excluded", "missing-count-parent"} {
		expectAPIError(t, f.request(t, http.MethodGet, path+"ParentId="+parent+"&Recursive=true&Limit=0", nil, headers),
			http.StatusNotFound, "not_found", true)
	}
	expectAPIError(t, f.request(t, http.MethodGet, path+"Recursive=true&Limit=0&SortBy=Random", nil, headers),
		http.StatusBadRequest, "invalid_input", true)
	expectStatus(t, f.request(t, http.MethodGet, path+"Recursive=true&Limit=0", nil, nil), http.StatusUnauthorized)
}
