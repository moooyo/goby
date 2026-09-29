package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestHTTPProviderRefreshRecognizesTypedMusicBrainzIDsBeforeFirstLookup(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('provider-music','Provider music','music')`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		itemType, key, wantCode string
	}{
		{"MusicAlbum", "MusicBrainzReleaseGroup", "providers_disabled"},
		{"MusicArtist", "musicbrainzartist", "providers_disabled"},
		{"Audio", "MusicBrainzRecording", "providers_disabled"},
		{"Audio", "MusicBrainz", "providers_disabled"},
		{"MusicAlbum", "MusicBrainzRecording", "provider_match_required"},
	} {
		t.Run(test.itemType+"/"+test.key, func(t *testing.T) {
			id := test.itemType + "-" + test.key
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES($1,'provider-music','Provider item','provider item',$2,$3)`, id, test.itemType, test.itemType != "Audio"); err != nil {
				t.Fatal(err)
			}
			ids, err := json.Marshal(map[string]string{test.key: "11111111-2222-3333-4444-555555555555"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE item_metadata_state SET automatic=jsonb_set(automatic,'{ProviderIDs}',$2::jsonb),effective=jsonb_set(effective,'{ProviderIDs}',$2::jsonb) WHERE item_id=$1`, id, ids); err != nil {
				t.Fatal(err)
			}
			path := "/admin/v1/items/" + id
			before := jsonObject(t, f.request(t, http.MethodGet, path+"/metadata", nil, nil, cookie))
			// Disabled admission proves the stored ID reached the provider client
			// without making any network request or requiring a prior match record.
			response := f.request(t, http.MethodPost, path+"/providers/refresh", map[string]any{
				"Provider": "musicbrainz", "Revision": stringValue(t, before, "Revision"),
			}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectAPIError(t, response, http.StatusConflict, test.wantCode, false)
			after := jsonObject(t, f.request(t, http.MethodGet, path+"/metadata", nil, nil, cookie))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refresh admission changed metadata")
			}
			var sources int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_provider_sources WHERE item_id=$1`, id).Scan(&sources); err != nil || sources != 0 {
				t.Fatalf("refresh admission created provenance: count=%d error=%v", sources, err)
			}
		})
	}
}
