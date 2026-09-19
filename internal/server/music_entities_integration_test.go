package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestHTTPMusicEntityFamiliesKeepIDsRolesPermissionsAndIndependentFavorites(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Music entity viewer", "music-entity-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('music-public','Music','music'),('music-private','Private','music');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('music-family-album','music-public',NULL,'Album','album','MusicAlbum',true),
		('music-family-track','music-public','music-family-album','Track','track','Audio',false),
		('music-family-film','music-public',NULL,'Film','film','Movie',false),
		('music-family-hidden','music-private',NULL,'Hidden','hidden','Audio',false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["music-public"]}' WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	set := func(id string, source any) {
		t.Helper()
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE item_metadata_state SET effective=$2::jsonb WHERE item_id=$1`, id, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `SELECT sync_catalog_item_entities($1,$2::jsonb)`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
	set("music-family-album", map[string]any{"AlbumArtists": []string{"Album Ensemble"}})
	set("music-family-track", map[string]any{"Artists": []string{"Track & Solo / Duo"}, "Genres": []string{"Music Genre"}})
	set("music-family-film", map[string]any{"Artists": []string{"Film-only artist"}, "Genres": []string{"Film-only genre"}})
	set("music-family-hidden", map[string]any{"Artists": []string{"Private Artist"}, "Genres": []string{"Private Genre"}})
	idOf := func(kind, name string) string {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM catalog_entities WHERE kind=$1 AND name=$2`, kind, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(id, 10)
	}
	artistID, albumID := idOf("MusicArtist", "Track & Solo / Duo"), idOf("MusicArtist", "Album Ensemble")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "music-entity-viewer-password"), "AccessToken")}}
	for _, test := range []struct{ path, id, kind string }{
		{"/emby/Artists", artistID, "MusicArtist"}, {"/emby/Artists/AlbumArtists", albumID, "MusicArtist"},
		{"/emby/AlbumArtists", albumID, "MusicArtist"}, {"/emby/MusicGenres", idOf("Genre", "Music Genre"), "MusicGenre"},
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, test.path, nil, headers))
		if total != 1 || len(items) != 1 || items[0]["Id"] != test.id || items[0]["Type"] != test.kind {
			t.Fatalf("music family escaped its role/scope: %s %#v total=%d", test.path, items, total)
		}
		items, total = responseItems(t, f.request(t, http.MethodGet, test.path+"?Limit=0", nil, headers))
		if total != 1 || len(items) != 0 {
			t.Fatal("music family count depended on page size")
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Artists/Album%20Ensemble", nil, headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Artists/Private%20Artist", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/MusicGenres/Film-only%20genre", nil, headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,is_favorite) VALUES($1,'music-family-track',true)`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Artists?IsFavorite=true", nil, headers))
	if total != 0 || len(items) != 0 {
		t.Fatal("track favorite became an artist favorite")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO entity_user_data(user_id,entity_id,is_favorite) VALUES($1,$2::bigint,true)`, viewer.ID, artistID); err != nil {
		t.Fatal(err)
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Artists?IsFavorite=true", nil, headers))
	if total != 1 || len(items) != 1 || items[0]["Id"] != artistID {
		t.Fatal("independent artist favorite was not consumed")
	}
	cookie, _ := f.adminLogin(t)
	response := f.request(t, http.MethodGet, "/admin/v1/music/artists?Role=AlbumArtist&Limit=25", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	native := jsonObject(t, response)
	if native["Limit"] != float64(25) || native["StartIndex"] != float64(0) {
		t.Fatal("native music page lost its explicit bounds")
	}
	set("music-family-track", map[string]any{"Artists": []string{"Track & Solo / Duo"}, "Genres": []string{"Music Genre"}})
	if idOf("MusicArtist", "Track & Solo / Duo") != artistID {
		t.Fatal("music entity resynchronization rotated identity")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Artists?IsFavorite=true", nil, headers))
	if total != 0 || len(items) != 0 {
		t.Fatal("favorite artist bypassed revoked source permission")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE kind='admin'`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/music/artists", nil, nil, cookie), http.StatusUnauthorized)
}
