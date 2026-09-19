package server

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func TestHTTPLibraryEditingCASDirectoryBrowserAndSelectedOptions(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := filepath.Dir(writeAPIMediaFile(t, root, "first/Film.mp4"))
	second := filepath.Dir(writeAPIMediaFile(t, root, "second/Film.mp4"))
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, path, "movies")
	endpoint := "/admin/v1/libraries/" + libraryID
	response := f.request(t, http.MethodGet, endpoint, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	detail := objectValue(t, jsonObject(t, response), "Library")
	revision := stringValue(t, detail, "Revision")
	if len(detail["RegisteredPaths"].([]any)) != 1 {
		t.Fatal("editing detail omitted root identity/count metadata")
	}
	headers := http.Header{"X-CSRF-Token": {csrf}}
	update := map[string]any{"Revision": revision, "Name": "Edited library", "Paths": []string{path, second}, "LibraryOptions": map[string]bool{"EnableLocalMetadata": false}}
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, update, nil, cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPatch, endpoint, update, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	updated := objectValue(t, jsonObject(t, response), "Library")
	if updated["Id"] != libraryID || updated["Name"] != "Edited library" || stringValue(t, updated, "Revision") == revision || len(updated["Paths"].([]any)) != 2 {
		t.Fatalf("edit did not persist: %#v", updated)
	}
	options := objectValue(t, updated, "LibraryOptions")
	if options["EnableLocalMetadata"] != false || options["EnableLocalImages"] != true {
		t.Fatalf("partial options reset unrelated fields: %#v", options)
	}
	if _, exists := jsonObject(t, response)["Job"]; exists {
		t.Fatal("editing unexpectedly started a scan")
	}
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, update, headers, cookie), http.StatusConflict)
	unknown := map[string]any{"Revision": updated["Revision"], "LibraryOptions": map[string]any{"EnableRealtimeMonitor": true}}
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, unknown, headers, cookie), http.StatusBadRequest)
	remove := map[string]any{"Revision": updated["Revision"], "Paths": []string{path}}
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, remove, headers, cookie), http.StatusBadRequest)
	remove["AcknowledgePathRemoval"] = true
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, remove, headers, cookie), http.StatusOK)
	browser := f.request(t, http.MethodGet, "/admin/v1/storage/directories?Path="+url.QueryEscape(root), nil, nil, cookie)
	items, total := responseItems(t, browser)
	if total != 2 || len(items) != 2 {
		t.Fatalf("directory browser: %#v", jsonObject(t, browser))
	}
	validated := f.request(t, http.MethodPost, "/admin/v1/storage/directories/validate", map[string]string{"Path": second}, headers, cookie)
	expectStatus(t, validated, http.StatusOK)
	if jsonObject(t, validated)["Available"] != true {
		t.Fatal("registered path removal changed physical directory availability")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/storage/directories/validate", map[string]string{"Path": t.TempDir()}, headers, cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/storage/directories", nil, nil), http.StatusUnauthorized)
}

func TestHTTPLibraryEditingEmbyAdaptersUseSameCatalogAndAuthority(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := filepath.Dir(writeAPIMediaFile(t, root, "first/Film.mp4"))
	second := filepath.Dir(writeAPIMediaFile(t, root, "second/Film.mp4"))
	id := createAndScanAPILibrary(t, f, cookie, csrf, path, "movies")
	login := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, login, "AccessToken")
	headers := http.Header{"X-Emby-Token": {token}}
	for _, request := range []struct {
		endpoint string
		body     any
	}{
		{"/emby/Library/VirtualFolders/Name", map[string]any{"Id": id, "NewName": "Compatibility edit"}},
		{"/Library/VirtualFolders/Paths", map[string]any{"Id": id, "PathInfo": map[string]string{"Path": second}}},
		{"/emby/library/virtualfolders/libraryoptions", map[string]any{"Id": id, "LibraryOptions": map[string]any{"DisabledLocalMetadataReaders": []string{"Nfo"}}}},
		{"/emby/Library/VirtualFolders/Paths/Delete", map[string]any{"Id": id, "Path": second}},
	} {
		expectStatus(t, f.request(t, http.MethodPost, request.endpoint, request.body, headers), http.StatusNoContent)
	}
	native := f.request(t, http.MethodGet, "/admin/v1/libraries/"+id, nil, nil, cookie)
	expectStatus(t, native, http.StatusOK)
	detail := objectValue(t, jsonObject(t, native), "Library")
	if detail["Name"] != "Compatibility edit" || len(detail["Paths"].([]any)) != 1 || objectValue(t, detail, "LibraryOptions")["EnableLocalMetadata"] != false {
		t.Fatalf("compatibility edits did not reach native catalog: %#v", detail)
	}
	directories := responseArray(t, f.request(t, http.MethodGet, "/environment/directorycontents?Path="+url.QueryEscape(root)+"&IncludeDirectories=true&IncludeFiles=false", nil, headers))
	if len(directories) != 2 || directories[0]["Type"] != "Directory" {
		t.Fatalf("compatibility directory projection: %#v", directories)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Environment/ValidatePath?Path="+url.QueryEscape(second), map[string]any{}, headers), http.StatusNoContent)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Library/VirtualFolders/LibraryOptions", map[string]any{"Id": id, "LibraryOptions": map[string]bool{"EnableRealtimeMonitor": true}}, headers), http.StatusBadRequest)
}
