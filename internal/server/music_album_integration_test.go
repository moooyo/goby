package server

import (
	"net/http"
	"strconv"
	"testing"
)

func TestHTTPMusicAlbumCollectionsAndChildCountUseAuthorizedCatalog(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Music catalog viewer", "music-catalog-viewer-password", false)
	if err != nil {
		t.Fatal("create music catalog viewer")
	}
	other, err := f.users.CreateUser(f.ctx, "Music catalog other", "music-catalog-other-password", false)
	if err != nil {
		t.Fatal("create independent music catalog viewer")
	}
	// These are existing catalog facts in the owned schema. Reading an album
	// does not require generating media files, probing, or running a scan.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('music-visible','Visible Music','music'),('music-hidden','Hidden Music','music');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,path) VALUES
		('music-visible','music-visible',NULL,'Visible Music','visible music','CollectionFolder',true,''),
		('music-hidden','music-hidden',NULL,'Hidden Music','hidden music','CollectionFolder',true,''),
		('album-visible','music-visible','music-visible','Raw Album Name','raw album name','MusicAlbum',true,'/owned/Music/Raw Album Name'),
		('album-empty','music-visible','music-visible','Empty Album','empty album','MusicAlbum',true,''),
		('track-mp3','music-visible','album-visible','Raw Track Name','01 track','Audio',false,'/owned/Music/Raw Track Name.mp3'),
		('track-flac','music-visible','album-visible','Raw Track Name','02 track','Audio',false,'/owned/Music/Raw Track Name.flac'),
		('album-booklet','music-visible','album-visible','Booklet Folder','03 booklet','Folder',true,''),
		('nested-track','music-visible','album-booklet','Nested Track','nested track','Audio',false,''),
		('album-hidden','music-hidden','music-hidden','Hidden Album','hidden album','MusicAlbum',true,''),
		('hidden-track','music-hidden','album-hidden','Hidden Track','hidden track','Audio',false,''),
		('foreign-child','music-hidden','album-visible','Foreign Child','foreign child','Audio',false,'')`); err != nil {
		t.Fatal("seed existing music catalog relationships")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["music-visible"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal("scope the music catalog viewer")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["music-hidden"]}'::jsonb WHERE id=$1`, other.ID); err != nil {
		t.Fatal("scope the independent music catalog viewer")
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "music-catalog-viewer-password"), "AccessToken")}}
	otherHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, other.Name, "music-catalog-other-password"), "AccessToken")}}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, "SELECT jsonb_agg(to_jsonb(i) ORDER BY id)::text FROM items i").Scan(&value); err != nil {
			t.Fatal("snapshot music catalog rows")
		}
		return value
	}
	before := snapshot()
	detail := func(userID, itemID string, requestHeaders http.Header) map[string]any {
		t.Helper()
		response := f.request(t, http.MethodGet, "/emby/Users/"+userID+"/Items/"+itemID, nil, requestHeaders)
		expectStatus(t, response, http.StatusOK)
		return jsonObject(t, response)
	}
	album := detail(viewer.ID, "album-visible", headers)
	assertEmptyMusicCollections(t, album)
	if album["ChildCount"] != float64(3) || album["Name"] != "Raw Album Name" ||
		album["ParentId"] != "music-visible" || album["Path"] != "/owned/Music/Raw Album Name" {
		t.Fatal("album detail changed direct-child count, name, path, or parent")
	}
	empty := detail(viewer.ID, "album-empty", headers)
	assertEmptyMusicCollections(t, empty)
	if empty["ChildCount"] != float64(0) {
		t.Fatal("a known empty album must project its actual zero child count")
	}
	albums, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Users/"+viewer.ID+"/Items?ParentId=music-visible&IncludeItemTypes=MusicAlbum", nil, headers))
	if total != 2 || len(albums) != 2 {
		t.Fatal("music album listing lost the current library scope")
	}
	for _, item := range albums {
		assertEmptyMusicCollections(t, item)
		want, known := map[string]float64{"album-visible": 3, "album-empty": 0}[stringValue(t, item, "Id")]
		if !known || item["ChildCount"] != want {
			t.Error("album listing and detail use different child-count facts")
		}
	}
	childrenPath := "/emby/Users/" + viewer.ID + "/Items?ParentId=album-visible&SortBy=SortName&SortOrder=Ascending"
	expected := []string{"track-mp3", "track-flac", "album-booklet"}
	for offset, id := range expected {
		page, total := responseItems(t, f.request(t, http.MethodGet, childrenPath+"&Limit=1&StartIndex="+strconv.Itoa(offset), nil, headers))
		if total != 3 || len(page) != 1 || page[0]["Id"] != id {
			t.Fatal("album child count differs from authorized direct-child pagination")
		}
		if id != "album-booklet" {
			assertEmptyMusicCollections(t, page[0])
			if page[0]["Name"] != "Raw Track Name" || page[0]["ParentId"] != "album-visible" {
				t.Fatal("music collection defaults changed a track name or parent")
			}
		}
		if _, present := page[0]["ChildCount"]; present {
			t.Fatal("ordinary child items acquired a music-album count")
		}
	}
	for _, suffix := range []string{"&Limit=0", "&Limit=1&StartIndex=3"} {
		page, total := responseItems(t, f.request(t, http.MethodGet, childrenPath+suffix, nil, headers))
		if total != 3 || len(page) != 0 {
			t.Fatal("empty album child pages changed the authoritative total")
		}
	}
	for _, id := range []string{"track-mp3", "track-flac"} {
		track := detail(viewer.ID, id, headers)
		assertEmptyMusicCollections(t, track)
		if track["ParentId"] != "album-visible" || track["Name"] != "Raw Track Name" {
			t.Fatal("audio detail changed catalog identity")
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/album-hidden", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+other.ID+"/Items/album-visible", nil, otherHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/album-visible", nil, otherHeaders), http.StatusForbidden)
	if hidden := detail(other.ID, "album-hidden", otherHeaders); hidden["ChildCount"] != float64(1) {
		t.Fatal("independent library did not expose its own actual child count")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["music-hidden"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal("change current music library permission")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/album-visible", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, childrenPath, nil, headers), http.StatusNotFound)
	if hidden := detail(viewer.ID, "album-hidden", headers); hidden["ChildCount"] != float64(1) {
		t.Fatal("album reads retained a stale library permission snapshot")
	}
	if after := snapshot(); after != before {
		t.Fatal("music album reads changed persisted names, paths, hierarchy, or metadata")
	}
}
