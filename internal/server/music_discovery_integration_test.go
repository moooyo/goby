package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPMusicDiscoveryAliasesKeepTypedSeedsCountsAndCurrentACL(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Music discovery viewer", "music-discovery-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('mix-visible','Music','music'),('mix-private','Private','music');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('discovery-album','mix-visible','Album','album','MusicAlbum',true),
		('discovery-other-album','mix-visible','Other album','other album','MusicAlbum',true),
		('discovery-hidden-album','mix-private','Hidden album','hidden album','MusicAlbum',true);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('discovery-track','mix-visible','discovery-album','Alpha track','alpha track','Audio',false),
		('discovery-other','mix-visible','discovery-other-album','Other track','other track','Audio',false),
		('discovery-hidden','mix-private','discovery-hidden-album','Hidden track','hidden track','Audio',false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["mix-visible"]}' WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('discovery-visible-root','mix-visible','/music/visible','/music','visible'),('discovery-private-root','mix-private','/music/private','/music','private')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items i SET media=jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1,'DurationTicks',100000000,'Streams',jsonb_build_array(jsonb_build_object('Index',0,'CodecType','audio','Codec','flac'))),root_id=r.id,path=r.path || '/' || i.id || '.flac',relative_path=i.id || '.flac',file_identity='fixture-' || i.id,file_size=100,modified_at='2026-09-20T00:00:00Z' FROM library_roots r WHERE i.type='Audio' AND r.library_id=i.library_id`, media.CurrentProbeVersion); err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ id, artist string }{
		{"discovery-album", "Alpha artist"}, {"discovery-track", "Alpha artist"},
		{"discovery-other-album", "Other artist"}, {"discovery-other", "Other artist"},
		{"discovery-hidden-album", "Hidden artist"}, {"discovery-hidden", "Hidden artist"},
	} {
		raw, err := json.Marshal(map[string]any{"Artists": []string{value.artist}, "AlbumArtists": []string{value.artist}, "Genres": []string{"Rock / Pop"}, "Tags": []string{"Shared tag"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE items SET local_metadata=$2::jsonb WHERE id=$1`, value.id, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `SELECT sync_catalog_item_entities($1,$2::jsonb)`, value.id, raw); err != nil {
			t.Fatal(err)
		}
	}
	idOf := func(kind, name string) string {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM catalog_entities WHERE kind=$1 AND name=$2`, kind, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(id, 10)
	}
	artist, genre, hiddenArtist := idOf("MusicArtist", "Alpha artist"), idOf("Genre", "Rock / Pop"), idOf("MusicArtist", "Hidden artist")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "music-discovery-password"), "AccessToken")}}
	for _, path := range []string{
		"/Items/discovery-track/InstantMix", "/Songs/discovery-track/InstantMix", "/Albums/discovery-album/InstantMix",
		"/Artists/InstantMix?Id=" + artist, "/MusicGenres/InstantMix?Id=" + genre,
		"/MusicGenres/" + url.PathEscape("Rock / Pop") + "/InstantMix", "/Items/" + artist + "/InstantMix", "/Items/" + genre + "/InstantMix",
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
		if total != 2 || len(items) != 2 {
			t.Fatalf("mix alias %s returned %#v total=%d", path, items, total)
		}
		for _, item := range items {
			if item["Type"] != "Audio" || item["Id"] == "discovery-hidden" {
				t.Fatalf("mix alias %s leaked a hidden or non-Audio result: %#v", path, item)
			}
		}
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Songs/discovery-track/InstantMix?Limit=0", nil, headers))
	if total != 2 || len(items) != 0 {
		t.Fatal("explicit zero instant mix page lost complete count")
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Artists/"+artist+"/Similar", nil, headers))
	if total != 1 || len(items) != 1 || items[0]["Id"] != idOf("MusicArtist", "Other artist") {
		t.Fatalf("artist Similar alias lost authorized metadata score: %#v %d", items, total)
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Albums/discovery-album/Similar", nil, headers))
	if total != 1 || len(items) != 1 || items[0]["Id"] != "discovery-other-album" {
		t.Fatalf("album Similar alias changed existing score: %#v %d", items, total)
	}
	response := f.request(t, http.MethodGet, "/emby/Artists/Prefixes?ArtistType=Artist,AlbumArtist&Limit=1&StartIndex=99", nil, headers)
	expectStatus(t, response, http.StatusOK)
	var prefixes []map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &prefixes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prefixes, []map[string]string{{"Name": "A", "Value": "1"}, {"Name": "O", "Value": "1"}}) {
		t.Fatalf("artist prefixes escaped authority/role dedup: %#v", prefixes)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Artists/InstantMix?Id="+hiddenArtist, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Albums/discovery-track/Similar", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Songs/discovery-hidden/InstantMix", nil, headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `SELECT sync_catalog_item_entities('discovery-album','{"AlbumArtists":["Alpha artist","similar"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	legacy := jsonObject(t, f.request(t, http.MethodGet, "/artists/albumartists/similar", nil, headers))
	if legacy["Id"] != idOf("MusicArtist", "similar") || legacy["Name"] != "similar" {
		t.Fatalf("legacy album-artist name was consumed as a Similar operation: %#v", legacy)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Artists/InstantMix?Id="+artist, nil, headers), http.StatusNotFound)
}
