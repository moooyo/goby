//go:build linux

package server

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Keep the catalog fixture's duration and ordinary chapters while supplying the
// real file snapshot required by intro administration and original playback.
type introHTTPProber struct{}

func (introHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }

func (introHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (apiMediaProber{}).ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info.ProbeVersion = media.CurrentProbeVersion
	info.FileChangeTimeNs = media.FileChangeTime(stat)
	return info, nil
}

func newAdminIntroHTTPFixture(t *testing.T) *adminMetadataHTTPFixture {
	t.Helper()
	f := newAdminMetadataHTTPFixture(t)
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, introHTTPProber{}, []string{f.root})
	if err != nil {
		t.Fatalf("create current-source intro catalog: %v", err)
	}
	installFixtureCatalog(t, f.serverFixture, catalog)
	f.handler = f.app.Handler()
	// The versioned prober refreshes the old catalog-only media snapshots.
	// Replacement helpers retain ownership and cleanup for both generations.
	f.rescan(t, adminMetadataAutomaticNFO)
	return f
}

func TestAdminIntroMarkersLifecycle(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	path := "/admin/v1/items/" + f.itemID + "/intro"
	response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	if detail["Revision"] != "0" || detail["Effective"] != nil || detail["Override"] != nil {
		t.Fatalf("ordinary chapters inferred intro: %#v", detail)
	}
	body := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": int64(30_000_000), "EndTicks": int64(80_000_000), "Provenance": "Manual"}
	counts := f.playbackCounts(t)
	for _, bounds := range [][2]int64{{-1, 80_000_000}, {30_000_000, 30_000_000}, {80_000_000, 30_000_000}, {0, int64(detail["DurationTicks"].(float64)) + 1}} {
		invalid := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": bounds[0], "EndTicks": bounds[1], "Provenance": "Import"}
		expectStatus(t, f.request(t, http.MethodPut, path, invalid, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodPut, path, body, nil, f.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPut, path, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusOK)
	edited := jsonObject(t, response)
	if edited["Revision"] != "1" || objectValue(t, edited, "Effective")["Provenance"] != "Manual" {
		t.Fatalf("manual intro: %#v", edited)
	}
	expectStatus(t, f.request(t, http.MethodPut, path, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie), http.StatusConflict)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	item := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+f.itemID, nil, headers))
	markers := map[string]float64{}
	for _, raw := range item["Chapters"].([]any) {
		chapter := raw.(map[string]any)
		if chapter["MarkerType"] == "IntroStart" || chapter["MarkerType"] == "IntroEnd" {
			markers[chapter["MarkerType"].(string)] = chapter["StartPositionTicks"].(float64)
		}
	}
	if markers["IntroStart"] != 30_000_000 || markers["IntroEnd"] != 80_000_000 {
		t.Fatalf("client markers: %#v", markers)
	}
	response = f.request(t, http.MethodDelete, path, map[string]any{"Revision": edited["Revision"], "SourceRevision": edited["SourceRevision"]}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusOK)
	reset := jsonObject(t, response)
	if reset["Revision"] != "2" || reset["Effective"] != nil || reset["Override"] != nil {
		t.Fatalf("reset retained manual marker: %#v", reset)
	}
	if f.playbackCounts(t) != counts {
		t.Fatal("intro administration changed playback state")
	}
	expectStatus(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, f.viewerID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+f.itemID, nil, headers), http.StatusNotFound)
}
