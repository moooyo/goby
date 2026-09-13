package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPTVParentMetadataPreservesAuthorizedHierarchyAcrossRoutes(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	for _, relative := range []string{
		"visible/Visible Series/Season 02/Visible.Series.S02E01.mp4",
		"hidden/Private Series/Season 01/Private.Series.S01E01.mp4",
	} {
		writeAPIMediaFile(t, root, relative)
	}
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	visibleID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "visible"), "tvshows")
	hiddenID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "hidden"), "tvshows")
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, adminLogin, "AccessToken")}}
	visible := readScannedTVFilterSeries(t, f, adminHeaders, visibleID, "Visible Series", []int{2}, [][2]int{{2, 1}})
	hidden := readScannedTVFilterSeries(t, f, adminHeaders, hiddenID, "Private Series", []int{1}, [][2]int{{1, 1}})
	viewer, err := f.users.CreateUser(f.ctx, "TV Parent Viewer", "tv-parent-password", false)
	if err != nil {
		t.Fatalf("create TV parent viewer: %v", err)
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{visibleID}})
	if err != nil {
		t.Fatal(err)
	}
	setHTTPUserPolicy(t, f, viewer.ID, string(policy))
	login := f.embyLogin(t, viewer.Name, "tv-parent-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	episodeID, seasonID := visible.episodes[[2]int{2, 1}], visible.seasons[2]
	want := map[string]string{"SeriesId": visible.id, "SeriesName": "Visible Series", "SeasonId": seasonID, "SeasonName": "Season 02"}
	for _, route := range []string{"/emby/Users/" + viewer.ID + "/Items/" + episodeID, "/Users/" + viewer.ID + "/Items/" + episodeID} {
		response := f.request(t, http.MethodGet, route, nil, headers)
		expectStatus(t, response, http.StatusOK)
		assertTVParentFields(t, jsonObject(t, response), want)
	}
	for _, route := range []string{
		"/emby/Shows/" + visible.id + "/Episodes?UserId=" + viewer.ID,
		"/emby/Shows/" + visible.id + "/Episodes?Fields=UserData,ParentId",
		"/emby/Items?Ids=" + episodeID,
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, route, nil, headers))
		if total != 1 || len(items) != 1 || items[0]["Id"] != episodeID {
			t.Fatalf("TV relationship route %s changed item membership", route)
		}
		assertTVParentFields(t, items[0], want)
	}
	seasons, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+visible.id+"/Seasons", nil, headers))
	if total != 1 || len(seasons) != 1 {
		t.Fatal("season relationship projection changed membership")
	}
	assertTVParentFields(t, seasons[0], map[string]string{"SeriesId": visible.id, "SeriesName": "Visible Series"})
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+hidden.episodes[[2]int{1, 1}], nil, headers), http.StatusNotFound)

	// An authorized leaf must not inherit a private name through a malformed edge.
	if _, err := f.pool.Exec(f.ctx, "UPDATE items SET parent_id = $1 WHERE id = $2", hidden.id, seasonID); err != nil {
		t.Fatalf("set cross-library grandparent: %v", err)
	}
	readers := []struct {
		id      string
		headers http.Header
	}{{viewer.ID, headers}, {adminID, adminHeaders}}
	for _, reader := range readers {
		response := f.request(t, http.MethodGet, "/emby/Users/"+reader.id+"/Items/"+episodeID, nil, reader.headers)
		expectStatus(t, response, http.StatusOK)
		assertTVParentFields(t, jsonObject(t, response), map[string]string{"SeasonId": seasonID, "SeasonName": "Season 02"})
		if strings.Contains(response.Body.String(), "Private Series") {
			t.Fatal("cross-library grandparent leaked its name")
		}
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE items SET parent_id = $1 WHERE id = $2", hidden.seasons[1], episodeID); err != nil {
		t.Fatalf("set cross-library parent: %v", err)
	}
	for _, reader := range readers {
		response := f.request(t, http.MethodGet, "/emby/Users/"+reader.id+"/Items/"+episodeID, nil, reader.headers)
		expectStatus(t, response, http.StatusOK)
		assertTVParentFields(t, jsonObject(t, response), nil)
	}
}
