//go:build linux

package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestHTTPAdminRereadLocalBackdrop(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, rereadLocalNFOHTTPProber{}, []string{root}, library.WithDeferredLocalMetadata())
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.handler = f.app.Handler()
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	headers := http.Header{"Origin": {f.cfg.PublicURL}, "X-Csrf-Token": {csrf}}
	dir := filepath.Join(root, "Fixture Series")
	video := writeAPIMediaFile(t, root, "Fixture Series/Season 1/Show.S01E01.mp4")
	good := writeAPIJPEG(t, filepath.Join(dir, "fanart.jpg"), 80, 45, 4)
	bad := filepath.Join(dir, "backdrop.jpg")
	if err := os.WriteFile(bad, nil, 0600); err != nil {
		t.Fatal(err)
	}
	collection, err := catalog.CreateLibrary(f.ctx, "Backdrop TV", "tvshows", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	var itemID, episodeID string
	if err := f.pool.QueryRow(f.ctx, `SELECT series.id,episode.id FROM items series JOIN items episode ON episode.library_id=series.library_id
		WHERE series.library_id=$1 AND series.path=$2 AND series.type='Series' AND episode.path=$3`, collection.ID, dir, video).Scan(&itemID, &episodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE local_metadata_ingest SET import_nfo=false WHERE item_id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	const prefix, suffix = "/admin/v1/items/", "/images/Backdrop/reread-local"
	target := prefix + itemID + suffix
	loggingServer, log := requestLoggingServer()
	loggingServer.serverPolicy = f.app.serverPolicy
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/v1/items/{id}/images/Backdrop/reread-local", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	loggingServer.middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, target, nil))
	completion := requestLoggingCompletion(t, log)
	if completion["route"] != "POST /admin/v1/items/{id}/images/Backdrop/reread-local" || completion["outcome"] != "completed" || strings.Contains(log.String(), itemID) {
		t.Fatalf("the reread route was not safely logged: %v", completion)
	}
	post := func(path string, body any) *httptest.ResponseRecorder {
		return f.request(t, http.MethodPost, path, body, headers, cookie)
	}
	emby := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, emby), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	crossSite := headers.Clone()
	crossSite.Set("Origin", "https://other.example.test")
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, crossSite, cookie), http.StatusForbidden, "origin_denied", false)
	for _, id := range []string{"missing", collection.ID, episodeID} {
		expectAPIError(t, post(prefix+id+suffix, nil), http.StatusNotFound, "not_found", false)
	}
	expectAPIError(t, post(prefix+"bad%00id"+suffix, nil), http.StatusBadRequest, "invalid_input", false)
	for _, query := range []string{"?", "?Path=fanart.jpg"} {
		expectAPIError(t, post(target+query, nil), http.StatusBadRequest, "invalid_input", false)
	}
	expectAPIError(t, post(target, map[string]string{"URL": "https://artwork.example.test/bg.jpg"}), http.StatusBadRequest, "invalid_input", false)
	stream := httptest.NewRequest(http.MethodPost, target, io.LimitReader(strings.NewReader("{}"), 2)).WithContext(f.ctx)
	stream.Header = headers.Clone()
	stream.AddCookie(cookie)
	streamResponse := httptest.NewRecorder()
	f.handler.ServeHTTP(streamResponse, stream)
	expectAPIError(t, streamResponse, http.StatusBadRequest, "invalid_input", false)

	rejected := post(target, nil)
	expectAPIError(t, rejected, http.StatusUnprocessableEntity, "local_backdrop_rejected", false)
	if strings.Contains(rejected.Body.String(), dir) || strings.Contains(rejected.Body.String(), "backdrop.jpg") {
		t.Fatal("the failure exposed the physical directory or candidate name")
	}
	if err := os.Rename(bad, bad+".quarantine"); err != nil {
		t.Fatal(err)
	}
	var lastSeen time.Time
	if err := f.pool.QueryRow(f.ctx, `UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 hour' WHERE user_id=$1 AND kind='admin' RETURNING last_seen_at`, adminID).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	response := post(target, nil)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" || !reflect.DeepEqual(jsonObject(t, response), map[string]any{"ItemId": itemID, "Changed": true}) {
		t.Fatalf("wrong local backdrop response: %s", response.Body)
	}
	expectAPIError(t, post(target, nil), http.StatusConflict, "local_backdrop_not_pending", false)
	var seen time.Time
	var scans int
	if err := f.pool.QueryRow(f.ctx, `SELECT s.last_seen_at,(SELECT count(*) FROM scan_jobs WHERE library_id=$2) FROM sessions s WHERE s.user_id=$1 AND s.kind='admin'`, adminID, collection.ID).
		Scan(&seen, &scans); err != nil || !seen.Equal(lastSeen) || scans != 1 {
		t.Fatalf("reread touched native session activity or started a scan: lastSeen=%v scans=%d (%v)", seen, scans, err)
	}
	preview := f.request(t, http.MethodGet, "/emby/Items/"+itemID+"/Images/Backdrop/0", nil, emby)
	assertAPIImage(t, preview, 80, 45, "jpeg")
	if !bytes.Equal(preview.Body.Bytes(), good) {
		t.Fatal("the client did not receive the original local backdrop bytes")
	}
}
