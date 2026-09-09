//go:build linux

package server

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestHTTPNextUpSeriesCursorPaginationProjectionAndAuthorization(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	for _, name := range []string{
		"nextup/Example Show/Season 01/Example.Show.S01E01.mp4",
		"nextup/Example Show/Season 01/Example.Show.S01E02.mp4",
		"nextup/Example Show/Season 02/Example.Show.S02E01.mp4",
	} {
		writeAPIMediaFile(t, p.s.root, name)
	}
	collection, err := p.s.f.app.library.CreateLibrary(p.s.f.ctx, "Next-up television", "tvshows", []string{filepath.Join(p.s.root, "nextup")})
	if err != nil {
		t.Fatal(err)
	}
	p.s.rescan(t, collection.ID)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID, collection.ID})
	f := p.s.f
	series, count := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+collection.ID+"&Recursive=true&IncludeItemTypes=Series", nil, p.headers))
	if count != 1 || len(series) != 1 {
		t.Fatal("next-up fixture must contain one series")
	}
	seriesID := stringValue(t, series[0], "Id")
	episodes, count := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes", nil, p.headers))
	if count != 3 || len(episodes) != 3 {
		t.Fatal("next-up fixture must contain three ordered episodes")
	}
	first, middle, last := stringValue(t, episodes[0], "Id"), stringValue(t, episodes[1], "Id"), stringValue(t, episodes[2], "Id")
	path := "/sHoWs/nExTuP?UserId=" + p.s.viewerID + "&SeriesId=" + seriesID
	read := func(suffix string, total int, expected ...string) []map[string]any {
		t.Helper()
		items, count := responseItems(t, f.request(t, http.MethodGet, path+suffix, nil, p.headers))
		if count != total || len(items) != len(expected) {
			t.Fatalf("next-up count/page = %d/%d, want %d/%d", count, len(items), total, len(expected))
		}
		for index, id := range expected {
			if items[index]["Id"] != id {
				t.Errorf("next-up item %d is not the expected episode", index)
			}
		}
		return items
	}
	flag := func(method, id string) {
		t.Helper()
		expectStatus(t, f.request(t, method, "/emby/Users/"+p.s.viewerID+"/PlayedItems/"+id, nil, p.headers), http.StatusOK)
	}
	read("", 0)
	flag(http.MethodPost, middle)
	read("", 1, last)
	flag(http.MethodDelete, middle)
	flag(http.MethodPost, first)
	read("", 2, middle, last)
	read("&Limit=1&StartIndex=1", 2, last)
	read("&Limit=0", 2)
	read("&ParentId="+collection.ID, 2, middle, last)
	read("&ParentId="+stringValue(t, episodes[2], "ParentId"), 1, last)
	projected := read("&Fields=MediaSources,Path&EnableUserData=false&EnableImages=false", 2, middle, last)
	for _, item := range projected {
		if _, exists := item["MediaSources"]; !exists {
			t.Error("next-up fields did not include requested media sources")
		}
		if _, exists := item["Path"]; !exists {
			t.Error("next-up fields did not include the authorized source path")
		}
		for _, field := range []string{"UserData", "ImageTags", "BackdropImageTags"} {
			if _, exists := item[field]; exists {
				t.Errorf("disabled next-up field %s remains", field)
			}
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"&Limit=-1", nil, p.headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Shows/NextUp?UserId="+p.s.adminID, nil, p.headers), http.StatusForbidden)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	expectStatus(t, f.request(t, http.MethodGet, path, nil, p.headers), http.StatusNotFound)
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/NextUp", nil, p.headers))
	if total != 0 || len(items) != 0 {
		t.Error("next-up exposed a revoked television library")
	}
}
