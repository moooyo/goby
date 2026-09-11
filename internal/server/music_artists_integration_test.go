package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func TestHTTPMusicArtistRelationsUseStableIDsAndCurrentSourcePermissions(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Indexed music viewer", "indexed-music-viewer-password", false)
	if err != nil {
		t.Fatal("create indexed music viewer")
	}
	other, err := f.users.CreateUser(f.ctx, "Indexed music other", "indexed-music-other-password", false)
	if err != nil {
		t.Fatal("create independent indexed music viewer")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('artist-visible','Visible Music','music'),('artist-hidden','Hidden Music','music');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('artist-album','artist-visible',NULL,'Effective Album','effective album','MusicAlbum',true),
		('artist-disc','artist-visible','artist-album','Disc','disc','Folder',true),
		('artist-track','artist-visible','artist-disc','Accepted Track','accepted track','Audio',false),
		('artist-other-track','artist-hidden',NULL,'Other Track','other track','Audio',false),
		('artist-private-track','artist-hidden',NULL,'Private Track','private track','Audio',false),
		('artist-orphan-track','artist-visible',NULL,'Orphan Track','orphan track','Audio',false)`); err != nil {
		t.Fatal("seed indexed music catalog")
	}
	for id, libraryID := range map[string]string{viewer.ID: "artist-visible", other.ID: "artist-hidden"} {
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{libraryID}})
		if err != nil {
			t.Fatal("encode indexed music test policy")
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", id, policy); err != nil {
			t.Fatal("apply indexed music test policy")
		}
	}
	setSource := func(itemID string, artists, albumArtists []string) {
		t.Helper()
		source, err := json.Marshal(map[string]any{"Version": 1, "Artists": artists, "AlbumArtists": albumArtists})
		if err != nil {
			t.Fatal("encode accepted music source")
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE item_metadata_state SET music_source=$2::jsonb,effective=$2::jsonb WHERE item_id=$1", itemID, source); err != nil {
			t.Fatal("persist accepted indexed music source")
		}
		if _, err := f.pool.Exec(f.ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", itemID, source); err != nil {
			t.Fatal("synchronize indexed music relationships")
		}
	}
	setSource("artist-album", []string{"Shared Artist"}, []string{"Shared Artist"})
	setSource("artist-track", []string{"Shared Artist"}, []string{})
	setSource("artist-other-track", []string{"Shared Artist"}, []string{})
	setSource("artist-private-track", []string{"Private Artist"}, []string{})
	setSource("artist-orphan-track", []string{"Orphan Artist"}, []string{})
	entityID := func(name string) string {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name=$1", name).Scan(&id); err != nil {
			t.Fatal("read persisted music artist identity")
		}
		return strconv.FormatInt(id, 10)
	}
	sharedID, privateID, orphanID := entityID("Shared Artist"), entityID("Private Artist"), entityID("Orphan Artist")
	setSource("artist-album", []string{"Shared Artist"}, []string{"Shared Artist"})
	if entityID("Shared Artist") != sharedID {
		t.Fatal("repeated synchronization rotated a persistent artist identity")
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "indexed-music-viewer-password"), "AccessToken")}}
	otherHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, other.Name, "indexed-music-other-password"), "AccessToken")}}
	detail := func(userID, itemID string, requestHeaders http.Header) map[string]any {
		t.Helper()
		response := f.request(t, http.MethodGet, "/emby/Users/"+userID+"/Items/"+itemID, nil, requestHeaders)
		expectStatus(t, response, http.StatusOK)
		return jsonObject(t, response)
	}
	assertRelation := func(dto map[string]any, field string) {
		t.Helper()
		values, ok := dto[field].([]any)
		if !ok || len(values) != 1 {
			t.Fatalf("%s must expose one real music relationship", field)
		}
		pair, ok := values[0].(map[string]any)
		if !ok || pair["Name"] != "Shared Artist" || pair["Id"] != sharedID {
			t.Fatalf("%s lost the persisted name or string identity", field)
		}
	}
	for _, id := range []string{"artist-album", "artist-track"} {
		dto := detail(viewer.ID, id, headers)
		assertRelation(dto, "ArtistItems")
		assertRelation(dto, "AlbumArtists")
		if dto["AlbumArtist"] != "Shared Artist" {
			t.Fatal("uniform album relationship was not projected")
		}
		if id == "artist-track" && (dto["AlbumId"] != "artist-album" || dto["Album"] != "Effective Album" || dto["ParentId"] != "artist-disc") {
			t.Fatal("track album reference changed its physical parent or ignored the nearest album")
		}
	}
	artist := detail(viewer.ID, sharedID, headers)
	if artist["Id"] != sharedID || artist["Name"] != "Shared Artist" || artist["Type"] != "MusicArtist" {
		t.Fatal("numeric artist detail did not resolve the real catalog entity")
	}
	for _, filter := range []url.Values{
		{"ArtistIds": {sharedID}}, {"AlbumArtistIds": {sharedID}}, {"AlbumIds": {"artist-album"}},
	} {
		filter.Set("IncludeItemTypes", "Audio")
		filter.Set("Recursive", "true")
		path := "/emby/Users/" + viewer.ID + "/Items?" + filter.Encode()
		items, total := responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
		if total != 1 || len(items) != 1 || items[0]["Id"] != "artist-track" {
			t.Fatal("music HTTP filter ignored the actual relationship or current user scope")
		}
		page, total := responseItems(t, f.request(t, http.MethodGet, path+"&Limit=0", nil, headers))
		if total != 1 || len(page) != 0 {
			t.Fatal("music HTTP filter count was derived from the requested empty page")
		}
		items, total = responseItems(t, f.request(t, http.MethodGet, path+"&ExcludeItemIds=artist-track", nil, headers))
		if total != 0 || len(items) != 0 {
			t.Fatal("music item exclusion was silently dropped")
		}
	}
	// These ordered values were captured from the original Reference album UI.
	// AlbumIds was requested with MusicVideo, not with the album's Audio rows.
	observed := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"MusicAlbum"}, "AlbumArtistIds": {sharedID},
		"ExcludeItemIds": {"artist-album"}, "SortBy": {"ProductionYear,PremiereDate,SortName"}, "SortOrder": {"Descending,Descending,Ascending"}}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items?"+observed.Encode(), nil, headers))
	if total != 0 || len(items) != 0 {
		t.Fatal("reference related-album exclusions did not use the real catalog")
	}
	observed.Del("AlbumArtistIds")
	observed.Del("ExcludeItemIds")
	observed.Set("AlbumIds", "artist-album")
	observed.Set("IncludeItemTypes", "MusicVideo")
	path := "/emby/Users/" + viewer.ID + "/Items?" + observed.Encode()
	items, total = responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
	if total != 0 || len(items) != 0 {
		t.Fatal("absent music videos did not produce the actual empty catalog result")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES('indexed-music-video','artist-visible','artist-album','Catalog Music Video','catalog music video','MusicVideo',false)`); err != nil {
		t.Fatal("seed a real catalog music-video relationship")
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
	if total != 1 || len(items) != 1 || items[0]["Id"] != "indexed-music-video" || items[0]["Type"] != "MusicVideo" {
		t.Fatal("music-video support bypassed real type or album filtering")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+privateID, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+sharedID, nil, otherHeaders), http.StatusForbidden)
	if private := detail(other.ID, privateID, otherHeaders); private["Type"] != "MusicArtist" {
		t.Fatal("an independently authorized artist did not remain accessible")
	}
	setSource("artist-orphan-track", []string{}, []string{})
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+orphanID, nil, headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal("revoke the current music source permission")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+sharedID, nil, headers), http.StatusNotFound)
	if shared := detail(other.ID, sharedID, otherHeaders); shared["Name"] != "Shared Artist" {
		t.Fatal("revoking one user's sources changed another user's artist identity")
	}
	var historyRows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data").Scan(&historyRows); err != nil || historyRows != 0 {
		t.Fatal("music entity and album reads wrote playback or favorite state")
	}
}
