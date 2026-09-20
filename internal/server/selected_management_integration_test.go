//go:build linux

package server

import (
	"bytes"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPSelectedManagementFolderPickerAndPolicyWritesUseOneMeaning(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := writeAPIMediaFile(t, root, "media/Nested/Movie.mp4")
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "media"), "movies")
	var folderID, leafID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id,parent_id FROM items WHERE path=$1`, path).Scan(&leafID, &folderID); err != nil {
		t.Fatal(err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Folder Policy Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "viewer-password"), "AccessToken")}}
	endpoint := "/admin/v1/policy/deletion-folders?LibraryId=" + url.QueryEscape(libraryID) + "&Limit=50"
	expectStatus(t, f.request(t, http.MethodGet, endpoint, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, endpoint, nil, adminHeaders), http.StatusUnauthorized)
	choices := f.request(t, http.MethodGet, endpoint, nil, nil, cookie)
	items, total := responseItems(t, choices)
	if total != 2 || len(items) != 2 || choices.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("folder picker lost bounded current scope")
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[stringValue(t, item, "Id")] = true
		for _, name := range []string{"Name", "Type", "LibraryId", "LibraryName", "ParentId", "Path"} {
			if _, ok := item[name].(string); !ok {
				t.Fatalf("missing picker field %s", name)
			}
		}
	}
	if !seen[libraryID] || !seen[folderID] || seen[leafID] {
		t.Fatal("picker included a file or omitted a live folder")
	}
	managed := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/users/"+viewer.ID, nil, nil, cookie)), "User")
	policy := objectValue(t, managed, "Policy")
	policy["EnableContentDeletionFromFolders"] = []string{folderID}
	body := map[string]any{"Revision": managed["Revision"], "Name": managed["Name"], "IsAdministrator": managed["IsAdministrator"], "IsDisabled": managed["IsDisabled"], "Policy": policy}
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/users/"+viewer.ID, body, http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusOK)
	native := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/users/"+viewer.ID, nil, nil, cookie)), "User")
	saved := objectValue(t, native, "Policy")["EnableContentDeletionFromFolders"].([]any)
	if len(saved) != 1 || saved[0] != folderID {
		t.Fatal("native write did not persist folder identity")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+viewer.ID+"/Policy", map[string]any{"EnableContentDeletionFromFolders": []string{libraryID}}, adminHeaders), http.StatusOK)
	compat := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, viewerHeaders))
	saved = objectValue(t, compat, "Policy")["EnableContentDeletionFromFolders"].([]any)
	if len(saved) != 1 || saved[0] != libraryID {
		t.Fatal("compatibility policy projection diverged from the saved grant")
	}
	for _, value := range []string{leafID, "/new/unchecked/path"} {
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+viewer.ID+"/Policy", map[string]any{"EnableContentDeletionFromFolders": []string{value}}, adminHeaders), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/policy/deletion-folders?Limit=1&Limit=1", nil, nil, cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/policy/deletion-folders", map[string]any{}, nil, cookie), http.StatusBadRequest)
}

func TestHTTPSelectedManagementAvailableOptionsAndNativeCreateShareDefaults(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	viewer, err := f.users.CreateUser(f.ctx, "Options Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "viewer-password"), "AccessToken")}}
	for _, path := range []string{"/emby/Libraries/AvailableOptions", "/libraries/availableoptions", "/EmBy/LIBRARIES/AVAILABLEOPTIONS"} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		response := f.request(t, http.MethodGet, path, nil, headers)
		expectStatus(t, response, http.StatusOK)
		value := jsonObject(t, response)
		if len(value) != 6 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("option inventory grew an undeclared surface")
		}
		readers, ok := value["MetadataReaders"].([]any)
		if !ok || len(readers) != 1 {
			t.Fatal("missing sole Nfo reader")
		}
		reader, ok := readers[0].(map[string]any)
		if !ok || reader["Name"] != "Nfo" || reader["DefaultEnabled"] != true {
			t.Fatal("Nfo default diverged")
		}
		for _, name := range []string{"MetadataSavers", "SubtitleFetchers", "LyricsFetchers", "TypeOptions"} {
			if entries, ok := value[name].([]any); !ok || len(entries) != 0 {
				t.Fatalf("unselected capability advertised: %s", name)
			}
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Libraries/AvailableOptions?EnableRealtimeMonitor=true", nil, headers), http.StatusBadRequest)
	for index, options := range []map[string]bool{nil, {"EnableLocalMetadata": false, "EnableLocalImages": false}} {
		path := filepath.Dir(writeAPIMediaFile(t, root, []string{"defaults/Movie.mp4", "disabled/Movie.mp4"}[index]))
		body := map[string]any{"Name": []string{"Defaults", "Disabled"}[index], "CollectionType": "movies", "Paths": []string{path}, "Scan": false}
		if options != nil {
			body["LibraryOptions"] = options
		}
		created := f.request(t, http.MethodPost, "/admin/v1/libraries", body, http.Header{"X-CSRF-Token": {csrf}}, cookie)
		expectStatus(t, created, http.StatusCreated)
		library := objectValue(t, jsonObject(t, created), "Library")
		saved := objectValue(t, library, "LibraryOptions")
		if saved["EnableLocalMetadata"] != (index == 0) || saved["EnableLocalImages"] != (index == 0) {
			t.Fatal("native create did not preserve explicit scanner switches/defaults")
		}
		id := stringValue(t, library, "Id")
		adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Library/VirtualFolders/LibraryOptions", map[string]any{"Id": id, "LibraryOptions": map[string]any{"DisabledLocalMetadataReaders": []string{}}}, adminHeaders), http.StatusNoContent)
		detail := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/libraries/"+id, nil, nil, cookie)), "Library")
		current := objectValue(t, detail, "LibraryOptions")
		if current["EnableLocalMetadata"] != true || current["EnableLocalImages"] != (index == 0) {
			t.Fatal("compatibility Nfo reset changed the native-only image switch")
		}
	}
}

func TestHTTPSelectedManagementObservabilityLiteralAliasesKeepExistingContracts(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	for _, route := range []struct{ canonical, alias string }{
		{"/emby/System/ActivityLog/Entries", "/sYsTeM/aCtIvItYlOg/eNtRiEs"},
		{"/emby/System/Logs/Query", "/EMBY/sYsTeM/lOgS/qUeRy"},
		{"/emby/System/Logs/" + f.name, "/SyStEm/LoGs/" + f.name},
		{"/emby/System/Logs/" + f.name + "/Lines", "/eMbY/SyStEm/LoGs/" + f.name + "/lInEs"},
	} {
		for _, headers := range []http.Header{f.adminHeaders, f.keyHeaders} {
			canonical := f.request(t, http.MethodGet, route.canonical, nil, headers)
			alias := f.request(t, http.MethodGet, route.alias, nil, headers)
			expectStatus(t, canonical, http.StatusOK)
			expectStatus(t, alias, http.StatusOK)
			if canonical.Header().Get("Content-Type") != alias.Header().Get("Content-Type") || !bytes.Equal(canonical.Body.Bytes(), alias.Body.Bytes()) {
				t.Fatal("literal alias changed the observed response contract")
			}
		}
		expectStatus(t, f.request(t, http.MethodGet, route.alias, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, route.alias, nil, f.viewerHeaders), http.StatusForbidden)
		expectStatus(t, f.request(t, http.MethodHead, route.alias, nil, f.adminHeaders), http.StatusNotFound)
	}
	if upper := strings.ToUpper(f.name); upper != f.name {
		expectStatus(t, f.request(t, http.MethodGet, "/SyStEm/LoGs/"+upper, nil, f.adminHeaders), http.StatusNotFound)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/SystemActivity/Entries", nil, f.adminHeaders), http.StatusNotFound)
}

func TestHTTPSelectedManagementPlaylistPreviewLiteralAliasReusesMembershipAuthority(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := writeAPIMediaFile(t, root, "preview/Movie.mp4")
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Dir(path), "movies")
	var itemID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND path=$2`, libraryID, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	created := f.request(t, http.MethodPost, "/admin/v1/playlists", map[string]any{"Name": "Preview", "MediaType": "Video", "Ids": []string{itemID}}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusOK)
	playlistID := stringValue(t, jsonObject(t, created), "Id")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	canonical := "/emby/Playlists/" + playlistID + "/AddToPlaylistInfo?Ids=" + itemID
	baseline := f.request(t, http.MethodGet, canonical, nil, headers)
	expectStatus(t, baseline, http.StatusOK)
	for _, prefix := range []string{"/playlists/", "/EmBy/PlAyLiStS/"} {
		path := prefix + playlistID + "/aDdToPlAyLiStInFo?Ids=" + itemID
		response := f.request(t, http.MethodGet, path, nil, headers)
		expectStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.Body.Bytes(), baseline.Body.Bytes()) || jsonObject(t, response)["ContainsDuplicates"] != true {
			t.Fatal("playlist alias lost actual duplicate membership")
		}
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	}
	if upper := strings.ToUpper(playlistID); upper != playlistID {
		expectStatus(t, f.request(t, http.MethodGet, "/playlists/"+upper+"/addtoplaylistinfo?Ids="+itemID, nil, headers), http.StatusNotFound)
	}
}
