package server

import (
	"net/http"
	"testing"
)

func TestHTTPCollectionUserDataProjectsPersonalFavoritesAndMemberPlayback(t *testing.T) {
	f := newServerFixture(t)
	ownerID := f.bootstrap(t)
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('collection-state-wire-library','Collection state','music');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES
		('collection-state-wire-a','collection-state-wire-library','Track A','a','Audio'),
		('collection-state-wire-b','collection-state-wire-library','Track B','b','Audio')`); err != nil {
		t.Fatal(err)
	}
	create := func(path string, ids []string) string {
		t.Helper()
		response := f.request(t, http.MethodPost, path, map[string]any{"Name": "State wire fixture", "Ids": ids}, headers)
		expectStatus(t, response, http.StatusOK)
		return stringValue(t, jsonObject(t, response), "Id")
	}
	playlistID := create("/emby/Playlists", []string{"collection-state-wire-a", "collection-state-wire-a"})
	boxID := create("/emby/Collections", []string{"collection-state-wire-a", "collection-state-wire-b"})
	checkData := func(item map[string]any, favorite, played bool, unplayed int) {
		t.Helper()
		data, ok := item["UserData"].(map[string]any)
		if !ok || data["IsFavorite"] != favorite || data["Played"] != played || data["UnplayedItemCount"] != float64(unplayed) {
			t.Fatalf("collection wire user data = %#v", item["UserData"])
		}
	}
	getItem := func(id string) map[string]any {
		t.Helper()
		response := f.request(t, http.MethodGet, "/emby/Users/"+ownerID+"/Items/"+id+"?EnableImages=false", nil, headers)
		expectStatus(t, response, http.StatusOK)
		return jsonObject(t, response)
	}
	checkData(getItem(playlistID), false, false, 1)
	checkData(getItem(boxID), false, false, 2)
	base := "/emby/Users/" + ownerID
	favorite := f.request(t, http.MethodPost, base+"/FavoriteItems/"+playlistID, nil, headers)
	expectStatus(t, favorite, http.StatusOK)
	checkData(map[string]any{"UserData": jsonObject(t, favorite)}, true, false, 1)
	if source := getItem("collection-state-wire-a")["UserData"].(map[string]any); source["IsFavorite"] != false || source["Played"] != false {
		t.Fatalf("container favorite changed media state: %#v", source)
	}
	expectStatus(t, f.request(t, http.MethodPost, base+"/PlayedItems/"+playlistID, nil, headers), http.StatusBadRequest)
	played := f.request(t, http.MethodPost, base+"/PlayedItems/"+boxID, nil, headers)
	expectStatus(t, played, http.StatusOK)
	checkData(map[string]any{"UserData": jsonObject(t, played)}, false, true, 0)
	checkData(getItem(playlistID), true, true, 0)
	checkData(jsonObject(t, f.request(t, http.MethodGet, "/emby/Playlists/"+playlistID, nil, headers)), true, true, 0)
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?Recursive=true&IncludeItemTypes=Playlist,BoxSet&IsPlayed=true&EnableImages=false", nil, headers))
	if total != 2 || len(items) != 2 {
		t.Fatalf("collection played filter did not use real member state: %#v / %d", items, total)
	}
	for _, item := range items {
		checkData(item, item["Id"] == playlistID, true, 0)
	}
	without := jsonObject(t, f.request(t, http.MethodGet, base+"/Items/"+playlistID+"?EnableImages=false&EnableUserData=false", nil, headers))
	if _, exists := without["UserData"]; exists {
		t.Fatal("EnableUserData=false retained container state")
	}
}
