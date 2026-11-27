//go:build linux

package server

import (
	"context"
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
	"github.com/moooyo/goby/internal/media"
)

type rereadLocalNFOHTTPProber struct{}

func (rereadLocalNFOHTTPProber) CacheVersion() int             { return media.CurrentProbeVersion }
func (rereadLocalNFOHTTPProber) ProbeFileJoinedContract() bool { return true }

func (rereadLocalNFOHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (apiMediaProber{}).ProbeFile(ctx, file)
	if err != nil {
		return info, err
	}
	stat, err := file.Stat()
	if err == nil {
		info.ProbeVersion, info.FileChangeTimeNs = media.CurrentProbeVersion, media.FileChangeTime(stat)
	}
	return info, err
}

func TestHTTPAdminRereadLocalNFO(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, rereadLocalNFOHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.handler = f.app.Handler()
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	headers := make(http.Header)
	headers.Set("Origin", f.cfg.PublicURL)
	headers.Set("X-CSRF-Token", csrf)
	const leaf = "Series/Season 01/Show.S01E01"
	video := writeAPIMediaFile(t, root, leaf+".mp4")
	const original = `<episodedetails><title>Before</title><season>1</season><episode>1</episode></episodedetails>`
	writeNFOReferenceSidecar(t, root, leaf+".nfo", []byte(original))
	id := createAndScanAPILibrary(t, f, cookie, csrf, root, "tvshows")
	var itemID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Episode' AND path=$2`, id, video).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	const prefix, suffix = "/admin/v1/items/", "/metadata/reread-local-nfo"
	target := prefix + itemID + suffix
	loggingServer, log := requestLoggingServer()
	loggingServer.serverPolicy = f.app.serverPolicy
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/v1/items/{id}/metadata/reread-local-nfo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	loggingServer.middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, target, nil))
	completion := requestLoggingCompletion(t, log)
	if completion["route"] != "POST /admin/v1/items/{id}/metadata/reread-local-nfo" || completion["outcome"] != "completed" || strings.Contains(log.String(), itemID) {
		t.Fatalf("policy dispatch did not safely log the reread route: %v", completion)
	}
	withAdmin := func(route string, body any) *httptest.ResponseRecorder {
		return f.request(t, http.MethodPost, route, body, headers, cookie)
	}
	assertTitle := func(want string) {
		t.Helper()
		var name string
		if err := f.pool.QueryRow(f.ctx, `SELECT name FROM items WHERE id=$1`, itemID).Scan(&name); err != nil || name != want {
			t.Fatalf("catalog name = %q, want %q (%v)", name, want, err)
		}
	}
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	emby := http.Header{"X-Emby-Token": {embyToken}}
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, emby), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	crossSite := headers.Clone()
	crossSite.Set("Origin", "https://other.example.test")
	expectAPIError(t, f.request(t, http.MethodPost, target, nil, crossSite, cookie), http.StatusForbidden, "origin_denied", false)
	expectAPIError(t, f.request(t, http.MethodPost, "/emby/Items/"+itemID+"/Refresh", nil, emby), http.StatusNotFound, "not_implemented", true)
	expectAPIError(t, withAdmin(prefix+"missing"+suffix, nil), http.StatusNotFound, "not_found", false)
	expectAPIError(t, withAdmin(prefix+id+suffix, nil), http.StatusNotFound, "not_found", false)
	expectAPIError(t, withAdmin(prefix+"bad%00id"+suffix, nil), http.StatusBadRequest, "invalid_input", false)
	expectAPIError(t, withAdmin(target+"?", nil), http.StatusBadRequest, "invalid_input", false)
	expectAPIError(t, withAdmin(target+"?Path=other", nil), http.StatusBadRequest, "invalid_input", false)
	expectAPIError(t, withAdmin(target, map[string]any{"Path": "not allowed"}), http.StatusBadRequest, "invalid_input", false)
	stream := httptest.NewRequest(http.MethodPost, target, io.LimitReader(strings.NewReader("{}"), 2)).WithContext(f.ctx)
	stream.Header = headers.Clone()
	stream.AddCookie(cookie)
	streamResponse := httptest.NewRecorder()
	f.handler.ServeHTTP(streamResponse, stream)
	expectAPIError(t, streamResponse, http.StatusBadRequest, "invalid_input", false)
	assertTitle("Before")

	nfo := filepath.Join(root, filepath.FromSlash(leaf+".nfo"))
	if err := os.Remove(nfo); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, withAdmin(target, nil), http.StatusNotFound, "local_nfo_missing", false)
	assertTitle("Before")
	writeNFOReferenceSidecar(t, root, leaf+".nfo", []byte(`<episodedetails><episode>99</episode><title>Wrong episode</title></episodedetails>`))
	rejected := withAdmin(target, nil)
	expectAPIError(t, rejected, http.StatusUnprocessableEntity, "local_nfo_rejected", false)
	if strings.Contains(rejected.Body.String(), "Wrong episode") || strings.Contains(rejected.Body.String(), root) {
		t.Fatal("rejected reread exposed sidecar contents or a path")
	}
	assertTitle("Before")
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableLocalMetadata":false}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, withAdmin(target, nil), http.StatusConflict, "local_nfo_disabled", false)
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableLocalMetadata":true}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}

	var lastSeen time.Time
	if err := f.pool.QueryRow(f.ctx, `UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 hour'
		WHERE user_id=$1 AND kind='admin' RETURNING last_seen_at`, adminID).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	const replacement = `<episodedetails><title>After</title><season>1</season><episode>1</episode><tmdbid>987654</tmdbid></episodedetails>`
	writeNFOReferenceSidecar(t, root, leaf+".nfo", []byte(replacement))
	for _, changed := range []bool{true, false} {
		reply := withAdmin(target, nil)
		expectStatus(t, reply, http.StatusOK)
		if reply.Header().Get("Cache-Control") != "no-store" || !reflect.DeepEqual(jsonObject(t, reply), map[string]any{"ItemId": itemID, "Changed": changed}) {
			t.Fatalf("wrong reread response: %s", reply.Body)
		}
		assertTitle("After")
	}
	var seen time.Time
	var scans int
	if err := f.pool.QueryRow(f.ctx, `SELECT s.last_seen_at,(SELECT count(*) FROM scan_jobs WHERE library_id=$2) FROM sessions s
		WHERE s.user_id=$1 AND s.kind='admin'`, adminID, id).Scan(&seen, &scans); err != nil || !seen.Equal(lastSeen) || scans != 1 {
		t.Fatalf("reread touched authentication activity or admitted a scan: lastSeen=%v scans=%d (%v)", seen, scans, err)
	}
	if data, err := os.ReadFile(nfo); err != nil || string(data) != replacement {
		t.Fatalf("reread modified the NFO on disk: %v", err)
	}
}
