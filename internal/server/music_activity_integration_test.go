package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPMusicMetadataActivityRecordsFieldsForSetResetAndLocksWithoutValues(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('music-activity-library','Music','music');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES('music-activity-track','music-activity-library','Track','track','Audio',false)`); err != nil {
		t.Fatal(err)
	}
	const route = "/admin/v1/items/music-activity-track/metadata"
	response := f.request(t, http.MethodGet, route, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	initial := stringValue(t, jsonObject(t, response), "Revision")
	private := []string{"private-album-value", "private-artist-value", "private-ensemble-value"}
	overrides := map[string]any{"Album": private[0], "Artists": []string{private[1]}, "AlbumArtists": []string{private[2]}}
	update := func(revision string, values map[string]any, locks []string) *httptest.ResponseRecorder {
		return f.request(t, http.MethodPut, route, map[string]any{"Revision": revision, "Overrides": values, "LockedFields": locks},
			http.Header{"X-CSRF-Token": {csrf}}, cookie)
	}
	response = update(initial, overrides, []string{"AlbumArtists"})
	expectStatus(t, response, http.StatusOK)
	changed := stringValue(t, jsonObject(t, response), "Revision")
	// The same controls are a no-op; stale controls fail without an audit row.
	expectStatus(t, update(changed, overrides, []string{"AlbumArtists"}), http.StatusOK)
	expectStatus(t, update(initial, overrides, []string{"AlbumArtists"}), http.StatusConflict)
	response = update(changed, map[string]any{}, []string{})
	expectStatus(t, response, http.StatusOK)
	reset := stringValue(t, jsonObject(t, response), "Revision")
	response = f.request(t, http.MethodGet, "/admin/v1/activity?Action=metadata.updated&Limit=20", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	for _, value := range private {
		if bytes.Contains(response.Body.Bytes(), []byte(value)) {
			t.Fatal("activity exposed a music metadata value")
		}
	}
	page := jsonObject(t, response)
	items := page["Items"].([]any)
	if page["TotalRecordCount"] != float64(2) || len(items) != 2 {
		t.Fatal("music audit lost committed changes or recorded rejected/no-op edits")
	}
	for index, raw := range items {
		entry := raw.(map[string]any)
		if entry["Action"] != "metadata.updated" || entry["Name"] != "Metadata updated" || entry["Overview"] != "Item metadata controls were updated." ||
			entry["Revision"] != []string{reset, changed}[index] || objectValue(t, entry, "Resource")["Id"] != "music-activity-track" ||
			!reflect.DeepEqual(entry["ChangedFields"], []any{"Album", "AlbumArtists", "Artists", "LockedFields", "Overrides"}) {
			t.Fatalf("music activity lost fixed descriptions or exact changed-field facts: %#v", entry)
		}
	}
}
