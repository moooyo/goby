package server

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func TestHTTPCollectionLifecyclePreservesPlaylistEntriesAndBoxSetMembership(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	writeAPIMediaFile(t, root, "movies/Alpha.mp4")
	writeAPIMediaFile(t, root, "movies/Beta.mp4")
	ownerID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "movies"), "movies")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	adminHeaders := http.Header{"X-CSRF-Token": {csrf}}
	media, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+libraryID+"&Recursive=true&IncludeItemTypes=Movie&SortBy=SortName&EnableImages=false", nil, headers))
	if total != 2 || len(media) != 2 {
		t.Fatalf("expected two media items, got %#v", media)
	}
	firstID, secondID := stringValue(t, media[0], "Id"), stringValue(t, media[1], "Id")
	create := map[string]any{"Name": "Favorites", "MediaType": "Video", "Ids": []string{firstID, firstID}}
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/playlists", create, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	created := f.request(t, http.MethodPost, "/admin/v1/playlists", create, adminHeaders, cookie)
	expectStatus(t, created, http.StatusOK)
	playlist := jsonObject(t, created)
	playlistID := stringValue(t, playlist, "Id")
	if playlist["Name"] != "Favorites" || playlist["ItemAddedCount"] != float64(2) {
		t.Fatalf("playlist creation wire contract changed: %#v", playlist)
	}
	playlistPath := "/emby/Playlists/" + playlistID
	nativePlaylistPath := "/admin/v1/playlists/" + playlistID
	detail := jsonObject(t, f.request(t, http.MethodGet, nativePlaylistPath, nil, nil, cookie))
	if detail["OwnerId"] != ownerID || detail["Type"] != "Playlist" || detail["ChildCount"] != float64(2) {
		t.Fatalf("playlist detail lost ownership or membership: %#v", detail)
	}
	listed, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/playlists?Limit=0", nil, nil, cookie))
	if len(listed) != 0 || total != 1 {
		t.Fatal("zero-limit administrator collection listing lost its authorized total")
	}
	entries, total := responseItems(t, f.request(t, http.MethodGet, playlistPath+"/Items?EnableImages=false", nil, headers))
	if total != 2 || len(entries) != 2 || entries[0]["Id"] != firstID || entries[1]["Id"] != firstID {
		t.Fatalf("playlist deduplicated physical entries: %#v", entries)
	}
	firstEntry := stringValue(t, entries[0], "PlaylistItemId")
	secondEntry := stringValue(t, entries[1], "PlaylistItemId")
	if firstEntry == secondEntry {
		t.Fatal("duplicate playlist items share their mutable entry identity")
	}
	zeroEntries, zeroTotal := responseItems(t, f.request(t, http.MethodGet, playlistPath+"/Items?Limit=0&EnableImages=false", nil, headers))
	if len(zeroEntries) != 0 || zeroTotal != 2 {
		t.Fatal("zero-limit playlist page did not preserve its authorized entry count")
	}
	preview := f.request(t, http.MethodGet, playlistPath+"/AddToPlaylistInfo?Ids="+firstID, nil, headers)
	expectStatus(t, preview, http.StatusOK)
	if jsonObject(t, preview)["ContainsDuplicates"] != true {
		t.Fatal("playlist duplicate preview ignored existing membership")
	}
	added := f.request(t, http.MethodPost, playlistPath+"/Items?Ids="+secondID, nil, headers)
	expectStatus(t, added, http.StatusOK)
	if jsonObject(t, added)["ItemAddedCount"] != float64(1) {
		t.Fatal("playlist add response omitted the inserted entry count")
	}
	entries, _ = responseItems(t, f.request(t, http.MethodGet, playlistPath+"/Items?EnableImages=false", nil, headers))
	if len(entries) != 3 {
		t.Fatal("playlist append did not retain prior duplicate entries")
	}
	lastEntry := stringValue(t, entries[2], "PlaylistItemId")
	expectStatus(t, f.request(t, http.MethodPost, playlistPath+"/Items/"+url.PathEscape(lastEntry)+"/Move/0", nil, headers), http.StatusOK)
	entries, _ = responseItems(t, f.request(t, http.MethodGet, playlistPath+"/Items?EnableImages=false", nil, headers))
	if len(entries) != 3 || entries[0]["Id"] != secondID || entries[0]["PlaylistItemId"] != lastEntry ||
		entries[1]["PlaylistItemId"] != firstEntry || entries[2]["PlaylistItemId"] != secondEntry {
		t.Fatalf("playlist move changed stable entry identities or order: %#v", entries)
	}
	expectStatus(t, f.request(t, http.MethodDelete, playlistPath+"/Items?EntryIds="+url.QueryEscape(firstEntry), nil, headers), http.StatusOK)
	entries, total = responseItems(t, f.request(t, http.MethodGet, playlistPath+"/Items?EnableImages=false", nil, headers))
	if total != 2 || len(entries) != 2 || entries[1]["PlaylistItemId"] != secondEntry {
		t.Fatalf("removing one entry removed another duplicate: %#v", entries)
	}
	expectStatus(t, f.request(t, http.MethodPost, playlistPath+"/Items/Delete?EntryIds="+url.QueryEscape(secondEntry), nil, headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPatch, nativePlaylistPath, map[string]any{"IsLocked": true}, adminHeaders, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPost, playlistPath+"/Items?Ids="+firstID, nil, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, nativePlaylistPath, map[string]any{"Name": "Renamed", "IsLocked": false}, adminHeaders, cookie), http.StatusOK)

	boxCreated := f.request(t, http.MethodPost, "/emby/Collections?Name=Double+Feature&Ids="+firstID+","+firstID+","+secondID, nil, headers)
	expectStatus(t, boxCreated, http.StatusOK)
	boxID := stringValue(t, jsonObject(t, boxCreated), "Id")
	boxPath := "/emby/Collections/" + boxID
	boxItems, total := responseItems(t, f.request(t, http.MethodGet, boxPath+"/Items?EnableImages=false", nil, headers))
	if total != 2 || len(boxItems) != 2 {
		t.Fatalf("BoxSet did not deduplicate its media IDs: %#v", boxItems)
	}
	for _, item := range boxItems {
		if _, exists := item["PlaylistItemId"]; exists {
			t.Fatal("BoxSet membership acquired playlist entry identities")
		}
	}
	expectStatus(t, f.request(t, http.MethodPost, boxPath+"/Items?Ids="+firstID, nil, headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodDelete, boxPath+"/Items?Ids="+firstID, nil, headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPost, boxPath+"/Items/Delete?Ids="+secondID, nil, headers), http.StatusOK)
	boxItems, total = responseItems(t, f.request(t, http.MethodGet, boxPath+"/Items?EnableImages=false", nil, headers))
	if total != 0 || len(boxItems) != 0 {
		t.Fatal("BoxSet removal aliases did not remove the requested memberships")
	}
	expectStatus(t, f.request(t, http.MethodDelete, "/admin/v1/collections/"+boxID, nil, adminHeaders, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, boxPath, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodDelete, nativePlaylistPath, nil, adminHeaders, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, playlistPath, nil, headers), http.StatusNotFound)
	media, total = responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+libraryID+"&Recursive=true&IncludeItemTypes=Movie&EnableImages=false", nil, headers))
	if total != 2 || len(media) != 2 {
		t.Fatal("deleting collections affected their media items")
	}
}

func TestHTTPCollectionSharingAndOwnerAuthority(t *testing.T) {
	f := newServerFixture(t)
	ownerID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	adminHeaders := http.Header{"X-CSRF-Token": {csrf}}
	viewer, err := f.users.CreateUser(f.ctx, "Collection viewer", "collection-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "collection-viewer-password"), "AccessToken")}}
	created := f.request(t, http.MethodPost, "/admin/v1/playlists", map[string]any{"Name": "Private"}, adminHeaders, cookie)
	expectStatus(t, created, http.StatusOK)
	id := stringValue(t, jsonObject(t, created), "Id")
	nativePath, embyPath := "/admin/v1/playlists/"+id, "/emby/Playlists/"+id
	expectStatus(t, f.request(t, http.MethodGet, embyPath, nil, viewerHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodPost, nativePath, map[string]any{
		"Shares": []map[string]any{{"UserId": viewer.ID, "CanEdit": true}},
	}, adminHeaders, cookie), http.StatusOK)
	read := f.request(t, http.MethodGet, embyPath, nil, viewerHeaders)
	expectStatus(t, read, http.StatusOK)
	if jsonObject(t, read)["OwnerId"] != ownerID {
		t.Fatal("sharing changed the collection owner")
	}
	expectStatus(t, f.request(t, http.MethodPost, embyPath, map[string]any{"Name": "Taken over"}, viewerHeaders), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodDelete, embyPath, nil, viewerHeaders), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, embyPath+"?UserId="+ownerID, map[string]any{"Name": "Taken over"}, viewerHeaders), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, nativePath, map[string]any{"Shares": []any{}}, adminHeaders, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, embyPath, nil, viewerHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodPost, nativePath, map[string]any{"IsPublic": true}, adminHeaders, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, embyPath, nil, viewerHeaders), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Playlists?Name=Spoof&UserId="+ownerID, nil, viewerHeaders), http.StatusForbidden)
}
