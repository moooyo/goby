package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

func TestHTTPSimilarUsesScopedMetadataAndReturnedPageCounts(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Similarity viewer", "similarity-viewer-password", false)
	if err != nil {
		t.Fatal("create similarity viewer")
	}
	other, err := f.users.CreateUser(f.ctx, "Other similarity viewer", "other-similarity-password", false)
	if err != nil {
		t.Fatal("create independent similarity viewer")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('similar-visible','Visible Similarity','movies'),('similar-hidden','Hidden Similarity','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('similar-seed','similar-visible','Seed','seed','Movie',false),
		('similar-all','similar-visible','AllMatches','allmatches','Movie',false),
		('similar-genres','similar-visible','GenreBoth','genreboth','Movie',false),
		('similar-tags','similar-visible','TagBoth','tagboth','Movie',false),
		('similar-one','similar-visible','OneGenre','onegenre','Movie',false),
		('similar-private','similar-hidden','Private Match','private match','Movie',false),
		('similar-wrong-type','similar-visible','Wrong Type','wrong type','Audio',false)`); err != nil {
		t.Fatal("seed similarity catalog")
	}
	for userID, libraryID := range map[string]string{viewer.ID: "similar-visible", other.ID: "similar-hidden"} {
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{libraryID}})
		if err != nil {
			t.Fatal("encode similarity access policy")
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", userID, policy); err != nil {
			t.Fatal("apply similarity access policy")
		}
	}
	metadata := map[string]map[string]any{
		"similar-seed":       {"Genres": []string{"Drama", "Adventure", "Third Genre"}, "Tags": []string{"One", "Two", "Third Tag"}},
		"similar-all":        {"Genres": []string{"Drama", "Adventure"}, "Tags": []string{"One", "Two"}},
		"similar-genres":     {"Genres": []string{"Drama", "Adventure"}},
		"similar-tags":       {"Tags": []string{"One", "Two"}},
		"similar-one":        {"Genres": []string{"Drama"}},
		"similar-private":    {"Genres": []string{"Drama", "Adventure", "Third Genre"}, "Tags": []string{"One", "Two", "Third Tag"}},
		"similar-wrong-type": {"Genres": []string{"Drama", "Adventure", "Third Genre"}, "Tags": []string{"One", "Two", "Third Tag"}},
	}
	for itemID, values := range metadata {
		encoded, err := json.Marshal(values)
		if err != nil {
			t.Fatal("encode accepted similarity metadata")
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE item_metadata_state SET effective=$2::jsonb WHERE item_id=$1", itemID, encoded); err != nil {
			t.Fatal("persist effective similarity metadata")
		}
		if _, err := f.pool.Exec(f.ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", itemID, encoded); err != nil {
			t.Fatal("index effective similarity metadata")
		}
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "similarity-viewer-password"), "AccessToken")}}
	path := "/emby/Items/similar-seed/Similar?UserId=" + viewer.ID
	get := func(suffix string, want []string, ordered bool) {
		t.Helper()
		items, count := responseItems(t, f.request(t, http.MethodGet, path+suffix, nil, headers))
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, stringValue(t, item, "Id"))
			if _, found := item["UserData"]; found && suffix == "&EnableUserData=false&EnableImages=false" {
				t.Fatal("similar results ignored the user-data projection switch")
			}
		}
		if count != len(ids) {
			t.Fatal("similar result count exposed a pre-pagination population")
		}
		if !ordered {
			sort.Strings(ids)
			want = append([]string{}, want...)
			sort.Strings(want)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("similar result identities differ: got %v, want %v", ids, want)
		}
	}
	get("", []string{"similar-all", "similar-genres", "similar-tags"}, false)
	get("&Limit=1", []string{"similar-all"}, true)
	get("&Limit=0", []string{"similar-all"}, true)
	get("&Limit=1&EnableTotalRecordCount=false", []string{"similar-all"}, true)
	get("&Limit=1&StartIndex=50", []string{}, true)
	get("&SortBy=SortName&SortOrder=Descending&Limit=1&StartIndex=1", []string{"similar-genres"}, true)
	get("&ExcludeItemIds=similar-all", []string{"similar-genres", "similar-tags"}, false)
	get("&EnableUserData=false&EnableImages=false", []string{"similar-all", "similar-genres", "similar-tags"}, false)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/similar-private/Similar?Limit=0", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/missing-similar-seed/Similar?Limit=0", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/similar-private/Similar?UserId="+other.ID, nil, headers), http.StatusForbidden)
	var userDataRows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data WHERE user_id=$1", viewer.ID).Scan(&userDataRows); err != nil || userDataRows != 0 {
		t.Fatal("similarity browsing created or changed personal playback state")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, `{"EnableAllFolders":false,"EnabledFolders":[]}`); err != nil {
		t.Fatal("revoke similarity catalog access")
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"&Limit=0", nil, headers), http.StatusNotFound)
}

func TestHTTPMusicSimilarityKeepsArtistDirectionAndAlbumIdentity(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Music similarity viewer", "music-similarity-password", false)
	if err != nil {
		t.Fatal("create music similarity viewer")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('music-similar','Music Similarity','music');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('sim-album-seed','music-similar',NULL,'Seed Album','seed album','MusicAlbum',true),
		('sim-album-same','music-similar',NULL,'Same Album','same album','MusicAlbum',true),
		('sim-album-different','music-similar',NULL,'Different Album','different album','MusicAlbum',true),
		('sim-album-mixed','music-similar',NULL,'Mixed Album','mixed album','MusicAlbum',true),
		('sim-audio-seed','music-similar','sim-album-seed','Seed Audio','seed audio','Audio',false),
		('sim-audio-same','music-similar','sim-album-same','Same Audio','same audio','Audio',false),
		('sim-audio-different','music-similar','sim-album-different','Different Audio','different audio','Audio',false),
		('sim-audio-mixed-a','music-similar','sim-album-mixed','Mixed A','mixed a','Audio',false),
		('sim-audio-mixed-b','music-similar','sim-album-mixed','Mixed B','mixed b','Audio',false)`); err != nil {
		t.Fatal("seed physical music similarity albums")
	}
	for itemID, artists := range map[string][]string{
		"sim-album-seed": {"ArtistA"}, "sim-album-same": {"ArtistA"}, "sim-album-different": {"ArtistB"}, "sim-album-mixed": {"ArtistA", "ArtistB"},
		"sim-audio-seed": {"ArtistA"}, "sim-audio-same": {"ArtistA"}, "sim-audio-different": {"ArtistB"}, "sim-audio-mixed-a": {"ArtistA"}, "sim-audio-mixed-b": {"ArtistB"},
	} {
		albumArtist := "ArtistA"
		if itemID == "sim-album-different" || itemID == "sim-audio-different" {
			albumArtist = "ArtistB"
		}
		source, err := json.Marshal(map[string]any{"Version": 1, "Artists": artists, "AlbumArtists": []string{albumArtist}})
		if err != nil {
			t.Fatal("encode music similarity facts")
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE item_metadata_state SET music_source=$2::jsonb,effective=$2::jsonb WHERE item_id=$1", itemID, source); err != nil {
			t.Fatal("persist accepted music similarity facts")
		}
		if _, err := f.pool.Exec(f.ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", itemID, source); err != nil {
			t.Fatal("index accepted music similarity relationships")
		}
	}
	artistID := func(name string) string {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name=$1", name).Scan(&id); err != nil {
			t.Fatal("read indexed similarity artist")
		}
		return strconv.FormatInt(id, 10)
	}
	a, b := artistID("ArtistA"), artistID("ArtistB")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "music-similarity-password"), "AccessToken")}}
	check := func(seed string, values url.Values, want ...string) {
		t.Helper()
		items, count := responseItems(t, f.request(t, http.MethodGet, "/emby/Items/"+seed+"/Similar?"+values.Encode(), nil, headers))
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, stringValue(t, item, "Id"))
		}
		sort.Strings(ids)
		want = append([]string{}, want...)
		sort.Strings(want)
		if count != len(ids) || !reflect.DeepEqual(ids, want) {
			t.Fatalf("music similarity lost its directed artist or album boundary: got %v/%d, want %v", ids, count, want)
		}
	}
	check("sim-audio-seed", nil, "sim-audio-same", "sim-audio-mixed-a")
	check("sim-audio-different", nil)
	check("sim-audio-mixed-a", nil, "sim-audio-seed", "sim-audio-same", "sim-audio-mixed-b")
	check("sim-audio-mixed-b", nil, "sim-audio-seed", "sim-audio-same", "sim-audio-different", "sim-audio-mixed-a")
	check("sim-album-seed", nil, "sim-album-same", "sim-album-mixed")
	check("sim-album-different", nil)
	check("sim-album-mixed", nil, "sim-album-seed", "sim-album-same", "sim-album-different")
	check("sim-album-seed", url.Values{"ExcludeArtistIds": {b}}, "sim-album-same")
	check("sim-audio-seed", url.Values{"ExcludeArtistIds": {b}}, "sim-audio-same", "sim-audio-mixed-a")
	check("sim-audio-mixed-a", url.Values{"ExcludeArtistIds": {a}})
	check("sim-audio-mixed-b", url.Values{"ExcludeArtistIds": {a}}, "sim-audio-different")
	check("sim-audio-mixed-b", url.Values{"ExcludeArtistIds": {b}}, "sim-audio-seed", "sim-audio-same", "sim-audio-mixed-a")
	check("sim-album-mixed", url.Values{"ExcludeArtistIds": {a}}, "sim-album-different")
	check("sim-album-seed", url.Values{"Limit": {"0"}})
	check("sim-audio-seed", url.Values{"Limit": {"0"}})
	check("sim-album-seed", url.Values{"Limit": {"1"}, "StartIndex": {"1"}, "SortBy": {"SortName"}, "SortOrder": {"Ascending"}}, "sim-album-same")
}
