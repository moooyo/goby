//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func TestHTTPNavigationContractsUseCurrentLibraryScope(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	for _, name := range []string{"Feature - part1.mp4", "Feature - part2.mp4", "Feature - part3.mp4"} {
		writeAPIMediaFile(t, p.s.root, "navigation-parts/"+name)
	}
	collection, err := f.app.library.CreateLibrary(f.ctx, "Navigation parts", "movies", []string{filepath.Join(p.s.root, "navigation-parts")})
	if err != nil {
		t.Fatal(err)
	}
	p.s.rescan(t, collection.ID)
	p.s.setPolicy(t, p.s.viewerID, true, []string{collection.ID})
	ids := make([]string, 3)
	for index, name := range []string{"Feature - part1.mp4", "Feature - part2.mp4", "Feature - part3.mp4"} {
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path=$2", collection.ID, name).Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	counts := playbackTextRequest(t, p, http.MethodGet, "/items/counts", "", p.headers, nil)
	expectStatus(t, counts, http.StatusOK)
	if data := jsonObject(t, counts); len(data) != 14 || data["MovieCount"] != float64(3) || data["ItemCount"] != float64(3) || data["ArtistCount"] != float64(0) {
		t.Fatalf("count projection or scope = %v", data)
	}
	ancestors := playbackTextRequest(t, p, http.MethodGet, "/items/"+ids[0]+"/ancestors", "", p.headers, nil)
	expectStatus(t, ancestors, http.StatusOK)
	var chain []map[string]any
	if err := json.Unmarshal(ancestors.Body.Bytes(), &chain); err != nil || len(chain) != 1 || chain[0]["Id"] != collection.ID || chain[0]["Type"] != "CollectionFolder" || chain[0]["CollectionType"] != "movies" {
		t.Fatal("ancestors must be a nearest-first JSON array of real visible parents")
	}
	parts, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/videos/"+ids[0]+"/additionalparts?EnableImages=false&EnableUserData=false", "", p.headers, nil))
	if total != 2 || len(parts) != 2 || parts[0]["Id"] != ids[1] || parts[1]["Id"] != ids[2] {
		t.Fatal("additional parts lost their numeric sequence or result envelope")
	}
	for _, part := range parts {
		if _, exists := part["UserData"]; exists {
			t.Fatal("additional parts ignored the user-data projection switch")
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET local_metadata='{"ProductionYear":2025,"CommunityRating":9.0}' WHERE id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	selected, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Items?Recursive=true&Years=2025&MinCommunityRating=8&IsHD=false&SortBy=CommunityRating,Runtime&SortOrder=Descending&Limit=1", "", p.headers, nil))
	if total != 1 || len(selected) != 1 || selected[0]["Id"] != ids[1] {
		t.Fatal("HTTP filters were accepted without being applied before count and paging")
	}
	for _, path := range []string{"/emby/Items/Counts?Years=2025", "/emby/Items/Counts?IsFavorite=true&IsFavorite=false", "/emby/Items/" + ids[0] + "/Ancestors?Recursive=true", "/emby/Videos/" + ids[0] + "/AdditionalParts?StartIndex=1", "/emby/Items?SortBy=InventedRank", "/emby/Items?Years=0", "/emby/Shows/NextUp?Years=2025", "/emby/Shows/NextUp?SortBy=Name"} {
		expectStatus(t, playbackTextRequest(t, p, http.MethodGet, path, "", p.headers, nil), http.StatusBadRequest)
	}
	expectStatus(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Items/Counts?UserId="+p.s.adminID, "", p.headers, nil), http.StatusForbidden)
	expectStatus(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Items/Counts", "", nil, nil), http.StatusUnauthorized)
	p.s.setPolicy(t, p.s.viewerID, true, []string{})
	for _, path := range []string{"/emby/Items/" + ids[0] + "/Ancestors", "/emby/Videos/" + ids[0] + "/AdditionalParts"} {
		expectStatus(t, playbackTextRequest(t, p, http.MethodGet, path, "", p.headers, nil), http.StatusNotFound)
	}
	counts = playbackTextRequest(t, p, http.MethodGet, "/emby/Items/Counts", "", p.headers, nil)
	expectStatus(t, counts, http.StatusOK)
	if jsonObject(t, counts)["ItemCount"] != float64(0) {
		t.Fatal("counts retained a revoked library")
	}
}
