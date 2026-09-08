package server

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPLibraryMutationStatusesMatchReference(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	writeAPIMediaFile(t, root, "movies/Reference Contract.mp4")
	f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	created := f.request(t, http.MethodPost, "/emby/Library/VirtualFolders", map[string]any{"Name": "Reference contract", "CollectionType": "movies", "Paths": []string{filepath.Join(root, "movies")}, "RefreshLibrary": false}, headers)
	expectStatus(t, created, http.StatusNoContent)
	if created.Body.Len() != 0 || created.Header().Get("Content-Type") != "" {
		t.Error("library creation must return an empty 204 response")
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Library/VirtualFolders/Query", nil, headers))
	if total != 1 || len(items) != 1 {
		t.Fatalf("created library not visible: %#v", items)
	}
	id := stringValue(t, items[0], "ItemId")
	refreshed := f.request(t, http.MethodPost, "/emby/Library/Refresh", nil, headers)
	expectStatus(t, refreshed, http.StatusNoContent)
	if refreshed.Body.Len() != 0 {
		t.Error("library refresh must return an empty 204 response")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := f.app.library.ListJobs(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range jobs {
			if job.LibraryID == id && job.Status == "Completed" && job.Added == 1 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the 204 refresh response did not result in a completed scan")
}
