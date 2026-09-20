//go:build linux

package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPExpectedEpisodeRosterUsesNativeAuthorityAndExcludesPlayback(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	writeAPIMediaFile(t, root, "tv/Declared Show/Season 01/Declared Show S01E01.mkv")
	collection := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "tv"), "tvshows")
	var seriesID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series'`, collection).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	token := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	base := "/admin/v1/series/" + seriesID + "/episode-roster"
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {token}}} {
		expectStatus(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusUnauthorized)
	}
	initial := f.request(t, http.MethodGet, base, nil, nil, cookie)
	expectStatus(t, initial, http.StatusOK)
	if body := jsonObject(t, initial); body["Revision"] != "0" || body["State"] != "absent" || body["Source"] != nil || initial.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native absent source contract changed")
	}
	headers := http.Header{"Content-Type": {"application/json"}, "X-CSRF-Token": {csrf}}
	expectStatus(t, adminMetadataHTTPRaw(t, f, http.MethodPut, base, episodeRosterRequestJSON, http.Header{"Content-Type": {"application/json"}}, cookie), http.StatusForbidden)
	accepted := adminMetadataHTTPRaw(t, f, http.MethodPut, base, episodeRosterRequestJSON, headers, cookie)
	expectStatus(t, accepted, http.StatusOK)
	body := jsonObject(t, accepted)
	entries, ok := body["Entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatal("missing native entry")
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatal("invalid native entry")
	}
	missingID := stringValue(t, entry, "Id")
	if entry["Availability"] != "missing" || entry["Airing"] != "aired" || body["Revision"] != "1" {
		t.Fatal("explicit fact classification changed")
	}
	expectAPIError(t, adminMetadataHTTPRaw(t, f, http.MethodPut, base, episodeRosterRequestJSON, headers, cookie), http.StatusConflict, "revision_conflict", false)
	consumer := http.Header{"X-Emby-Token": {token}}
	detail := f.request(t, http.MethodGet, "/Users/"+adminID+"/Items/"+missingID+"?Fields=Path,MediaSources,MediaStreams", nil, consumer)
	expectStatus(t, detail, http.StatusOK)
	dto := jsonObject(t, detail)
	if dto["IsMissing"] != true || dto["LocationType"] != "Virtual" {
		t.Fatal("missing consumer DTO lost virtual markers")
	}
	for _, field := range []string{"Path", "MediaSources", "MediaStreams", "UserData", "Source", "SourceKey", "EntryKey"} {
		if _, exists := dto[field]; exists {
			t.Fatalf("missing DTO exposed %s", field)
		}
	}
	for _, path := range []string{"/Users/" + adminID + "/FavoriteItems/" + missingID, "/Users/" + adminID + "/PlayedItems/" + missingID} {
		expectStatus(t, f.request(t, http.MethodPost, path, nil, consumer), http.StatusNotFound)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/Videos/"+missingID+"/stream", nil, consumer), http.StatusNotFound)
	playback := f.request(t, http.MethodPost, "/Items/"+missingID+"/PlaybackInfo", map[string]any{"UserId": adminID}, consumer)
	expectAPIError(t, playback, http.StatusNotFound, "not_found", true)
	changed := strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":"1"`, 1)
	changed = strings.Replace(changed, "Second episode", "Different payload", 1)
	expectAPIError(t, adminMetadataHTTPRaw(t, f, http.MethodPut, base, changed, headers, cookie), http.StatusConflict, "source_revision_conflict", false)
	// request supplies Content-Type for a structured body. Adding the raw-helper
	// header again must be rejected without consuming the roster revision.
	duplicateContentType := f.request(t, http.MethodDelete, base, map[string]any{"Revision": "1"}, headers, cookie)
	expectAPIError(t, duplicateContentType, http.StatusUnsupportedMediaType, "unsupported_media_type", false)
	withdraw := f.request(t, http.MethodDelete, base, map[string]any{"Revision": "1"}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, withdraw, http.StatusOK)
	if body := jsonObject(t, withdraw); body["Revision"] != "2" || body["State"] != "withdrawn" || body["RetiredCount"] != float64(1) {
		t.Fatal("withdrawal tombstone lost")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/Users/"+adminID+"/Items/"+missingID, nil, consumer), http.StatusNotFound)
}
