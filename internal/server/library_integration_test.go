package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type apiMediaProber struct{}

func (apiMediaProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	return media.Info{
		Container: "mov,mp4", DurationTicks: 125 * media.TicksPerSecond, Bitrate: 4_000_000, Size: stat.Size(),
		Streams: []media.Stream{
			{Index: 2, Codec: "h264", CodecType: "video", Width: 1920, Height: 1080, Profile: "High", AverageFrameRate: "24000/1001", IsDefault: true},
			{Index: 5, Codec: "aac", CodecType: "audio", Language: "eng", Channels: 2, SampleRate: 48000, IsDefault: true},
			{Index: 9, Codec: "subrip", CodecType: "subtitle", Language: "eng", IsTextSubtitleStream: true},
		},
		Chapters: []media.Chapter{{StartTicks: 0, EndTicks: 125 * media.TicksPerSecond, Title: "Opening"}},
	}, nil
}

func newLibraryServerFixture(t *testing.T) (*serverFixture, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("media library HTTP integration tests require Linux")
	}
	f := newServerFixture(t)
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close default media workers: %v", err)
	}
	root := t.TempDir()
	catalog, err := library.New(f.pool, apiMediaProber{}, []string{root})
	if err != nil {
		t.Fatalf("create test media catalog: %v", err)
	}
	f.app.library = catalog
	f.app.cfg.MediaRoots = []string{root}
	f.cfg.MediaRoots = []string{root}
	f.handler = f.app.Handler()
	// Stop the fake scanner before TempDir removes its approved media root.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close test media workers: %v", err)
		}
	})
	return f, root
}

func writeAPIMediaFile(t *testing.T, root, relative string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create media fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("local media fixture for "+relative), 0o600); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
	return path
}

func responseItems(t *testing.T, response *httptest.ResponseRecorder) ([]map[string]any, int) {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	object := jsonObject(t, response)
	raw, ok := object["Items"].([]any)
	if !ok {
		t.Fatalf("Items must be a JSON array: %#v", object)
	}
	total, ok := object["TotalRecordCount"].(float64)
	if !ok {
		t.Fatalf("TotalRecordCount must be numeric: %#v", object)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item must be a JSON object: %#v", item)
		}
		items = append(items, object)
	}
	return items, int(total)
}

func responseArray(t *testing.T, response *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("latest response must be JSON: %s", response.Body.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil || items == nil {
		t.Fatalf("latest response must be a bare JSON array: %v; body = %s", err, response.Body.String())
	}
	return items
}

func assertNoItemPaths(t *testing.T, value any, root string) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			switch strings.ToLower(key) {
			case "path", "paths", "location", "locations":
				t.Errorf("public item metadata exposes a filesystem field: %s", key)
			}
			assertNoItemPaths(t, child, root)
		}
	case []any:
		for _, child := range value {
			assertNoItemPaths(t, child, root)
		}
	case string:
		if strings.Contains(value, root) {
			t.Errorf("public item metadata exposes the approved media directory: %q", value)
		}
	}
}

func createAndScanAPILibrary(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf, path, collectionType string) string {
	t.Helper()
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Test " + collectionType, "CollectionType": collectionType, "Paths": []string{path}, "Scan": true,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	object := jsonObject(t, created)
	id := stringValue(t, objectValue(t, object, "Library"), "Id")
	jobID := stringValue(t, objectValue(t, object, "Job"), "Id")
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		jobs, _ := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
		for _, job := range jobs {
			if job["Id"] != jobID {
				continue
			}
			switch job["Status"] {
			case "completed":
				scanned, scannedOK := job["Scanned"].(float64)
				added, addedOK := job["Added"].(float64)
				if !scannedOK || !addedOK || scanned < 1 || added < 1 || job["Error"] != "" {
					t.Fatalf("completed scan did not catalog its media files: %#v", job)
				}
				return id
			case "failed", "cancelled", "interrupted":
				t.Fatalf("media fixture scan did not complete: %#v", job)
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for the HTTP scan job to complete")
		case <-f.ctx.Done():
			t.Fatalf("scan fixture context ended: %v", f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestHTTPLibraryScanBrowseFieldsAndDeletePreservesMedia(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	firstFile := writeAPIMediaFile(t, root, "movies/Alpha.Movie.mp4")
	secondFile := writeAPIMediaFile(t, root, "movies/Nested/Beta.Movie.mp4")
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "movies"), "movies")
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/Views", nil, headers))
	if total != 1 || len(views) != 1 || views[0]["Id"] != libraryID || views[0]["Type"] != "CollectionFolder" || views[0]["CollectionType"] != "movies" {
		t.Fatalf("views do not expose the scanned movie collection: %#v, total = %d", views, total)
	}
	assertNoItemPaths(t, views[0], root)
	base := "/emby/Users/" + adminID + "/Items?ParentId=" + libraryID + "&IncludeItemTypes=Movie"
	direct, total := responseItems(t, f.request(t, http.MethodGet, base, nil, headers))
	if total != 1 || len(direct) != 1 || direct[0]["Name"] != "Alpha Movie" {
		t.Fatalf("non-recursive movie browsing crossed folder boundaries: %#v, total = %d", direct, total)
	}
	if _, exists := direct[0]["MediaStreams"]; exists {
		t.Error("listing returned MediaStreams without requesting the field")
	}
	if _, exists := direct[0]["MediaSources"]; exists {
		t.Error("listing returned MediaSources without requesting the field")
	}
	assertNoItemPaths(t, direct[0], root)
	recursive, total := responseItems(t, f.request(t, http.MethodGet, base+"&Recursive=true&MediaTypes=Video&SortBy=SortName", nil, headers))
	if total != 2 || len(recursive) != 2 || recursive[0]["Name"] != "Alpha Movie" || recursive[1]["Name"] != "Beta Movie" {
		t.Fatalf("recursive movie filtering or sorting is incorrect: %#v, total = %d", recursive, total)
	}
	searched, total := responseItems(t, f.request(t, http.MethodGet, base+"&Recursive=true&SearchTerm=beta&Fields=MediaStreams,MediaSources,Chapters,Overview,Path", nil, headers))
	if total != 1 || len(searched) != 1 || searched[0]["Name"] != "Beta Movie" {
		t.Fatalf("search did not isolate the nested movie: %#v, total = %d", searched, total)
	}
	mediaItem := searched[0]
	for _, field := range []string{"MediaStreams", "MediaSources", "Chapters", "Overview"} {
		if _, exists := mediaItem[field]; !exists {
			t.Errorf("requested field %s is missing", field)
		}
	}
	if mediaItem["Path"] != secondFile {
		t.Errorf("explicit Path projection = %v, want authorized file %q", mediaItem["Path"], secondFile)
	}
	itemID := stringValue(t, mediaItem, "Id")
	detailResponse := f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/Items/"+itemID+"?Fields=Path", nil, headers)
	expectStatus(t, detailResponse, http.StatusOK)
	detail := jsonObject(t, detailResponse)
	if detail["Path"] != secondFile {
		t.Errorf("item detail Path = %v, want %q", detail["Path"], secondFile)
	}
	streams, ok := detail["MediaStreams"].([]any)
	if !ok || len(streams) != 3 {
		t.Fatalf("detail media streams are incomplete: %#v", detail)
	}
	for index, expected := range []int{2, 5, 9} {
		stream, ok := streams[index].(map[string]any)
		if !ok || stream["Index"] != float64(expected) {
			t.Errorf("stream position %d lost the actual source index %d: %#v", index, expected, streams[index])
		}
	}
	sources, ok := detail["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("detail media source is missing: %#v", detail)
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatalf("media source must be an object: %#v", sources[0])
	}
	if source["Path"] != secondFile || source["Protocol"] != "File" {
		t.Errorf("authorized media source path/protocol mismatch: %#v", source)
	}
	noImages, total := responseItems(t, f.request(t, http.MethodGet, base+"&EnableImages=false&EnableUserData=false", nil, headers))
	if total != 1 || len(noImages) != 1 {
		t.Fatalf("projection switches changed query membership")
	}
	for _, name := range []string{"ImageTags", "BackdropImageTags", "UserData"} {
		if _, exists := noImages[0][name]; exists {
			t.Errorf("disabled field %s remains", name)
		}
	}
	stat, err := os.Stat(secondFile)
	if err != nil || source["Size"] != float64(stat.Size()) || source["Container"] != "mov" || detail["RunTimeTicks"] != float64(125*media.TicksPerSecond) {
		t.Fatalf("media source size, container, or duration changed: %#v, stat error = %v", source, err)
	}
	overview := jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/overview", nil, nil, cookie))
	counts := objectValue(t, overview, "Counts")
	if counts["Libraries"] != float64(1) || counts["Items"] != float64(2) {
		t.Errorf("overview does not reflect scanned catalog totals: %#v", counts)
	}
	deleted := f.request(t, http.MethodDelete, "/admin/v1/libraries/"+libraryID, nil, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, deleted, http.StatusNoContent)
	for _, path := range []string{firstFile, secondFile} {
		content, err := os.ReadFile(path)
		if err != nil || !strings.HasPrefix(string(content), "local media fixture for ") {
			t.Errorf("deleting the library modified its media file %s: %v", path, err)
		}
	}
	for _, table := range []string{"libraries", "library_roots", "items", "scan_jobs"} {
		var count int
		if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("deleted library retains %s metadata: count = %d, error = %v", table, count, err)
		}
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/Items/"+itemID, nil, headers), http.StatusNotFound, "not_found", true)
	remaining, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/libraries", nil, nil, cookie))
	if total != 0 || len(remaining) != 0 {
		t.Errorf("deleted library remains in administrator list: %#v", remaining)
	}
}

func TestHTTPLibraryPermissionsCSRFAndLivePolicyRevocation(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	writeAPIMediaFile(t, root, "movies/Private.Movie.mp4")
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	body := map[string]any{"Name": "Denied", "CollectionType": "movies", "Paths": []string{filepath.Join(root, "movies")}}
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/libraries", body, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "movies"), "movies")
	expectAPIError(t, f.request(t, http.MethodDelete, "/admin/v1/libraries/"+libraryID, nil, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	viewer, err := f.users.CreateUser(f.ctx, "Library Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create library viewer: %v", err)
	}
	login := f.embyLogin(t, viewer.Name, "viewer-password")
	token := stringValue(t, login, "AccessToken")
	headers := http.Header{"X-Emby-Token": {token}}
	for _, path := range []string{"/admin/v1/libraries", "/admin/v1/jobs", "/admin/v1/storage/roots"} {
		expectAPIError(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized, "authentication_required", false)
		expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil, &http.Cookie{Name: "goby_session", Value: token}), http.StatusUnauthorized, "invalid_credentials", false)
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Library/VirtualFolders/Query", nil, headers), http.StatusForbidden, "administrator_required", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Items?UserId="+adminID+"&Recursive=true", nil, headers), http.StatusForbidden, "access_denied", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items?UserId="+adminID, nil, headers), http.StatusBadRequest, "invalid_input", true)
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?Recursive=true&IncludeItemTypes=Movie", nil, headers))
	if total != 1 || len(items) != 1 {
		t.Fatalf("default user policy did not expose its movie: %#v", items)
	}
	itemID := stringValue(t, items[0], "Id")
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = $1`, viewer.ID); err != nil {
		t.Fatalf("apply empty library access policy: %v", err)
	}
	for _, path := range []string{
		"/emby/Users/" + viewer.ID + "/Views",
		"/emby/Items?Recursive=true&IncludeItemTypes=Movie",
		"/emby/Items?Recursive=true&Ids=" + itemID,
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
		if total != 0 || len(items) != 0 {
			t.Errorf("revoked library policy leaked list contents or counts at %s: %#v, total = %d", path, items, total)
		}
	}
	rootResponse := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/Root", nil, headers))
	if rootResponse["ChildCount"] != float64(0) {
		t.Errorf("virtual root leaked a revoked library count: %#v", rootResponse)
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+itemID, nil, headers), http.StatusNotFound, "not_found", true)
	if latest := responseArray(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/Latest", nil, headers)); len(latest) != 0 {
		t.Errorf("latest items leaked a revoked library: %#v", latest)
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{libraryID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", policy, viewer.ID); err != nil {
		t.Fatalf("grant explicit library access: %v", err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Items/"+itemID, nil, headers), http.StatusOK)
}

func TestHTTPLibraryRejectsTraversalOutsideAndUnavailableDirectories(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	outside := t.TempDir()
	regularFile := writeAPIMediaFile(t, root, "regular-file.mp4")
	outsideLink := filepath.Join(root, "outside-link")
	brokenLink := filepath.Join(root, "broken-link")
	if err := os.Symlink(outside, outsideLink); err != nil {
		t.Fatalf("create outside symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), brokenLink); err != nil {
		t.Fatalf("create broken symlink: %v", err)
	}
	for _, test := range []struct {
		name, path, code string
		status           int
	}{
		{name: "relative", path: "relative/movies", status: 400, code: "invalid_input"},
		{name: "traversal", path: root + "/../outside", status: 400, code: "invalid_input"},
		{name: "backslash traversal", path: root + `/..\outside`, status: 400, code: "invalid_input"},
		{name: "outside", path: outside, status: 403, code: "access_denied"},
		{name: "outside symlink", path: outsideLink, status: 403, code: "access_denied"},
		{name: "missing", path: filepath.Join(root, "missing"), status: 503, code: "library_unavailable"},
		{name: "broken symlink", path: brokenLink, status: 503, code: "library_unavailable"},
		{name: "regular file", path: regularFile, status: 503, code: "library_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
				"Name": "Rejected library", "CollectionType": "movies", "Paths": []string{test.path},
			}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
			expectAPIError(t, response, test.status, test.code, false)
		})
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/libraries", nil, nil, cookie))
	if total != 0 || len(items) != 0 {
		t.Errorf("failed directory validation persisted a partial library: %#v", items)
	}
	roots := jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie))
	entries, ok := roots["Items"].([]any)
	if !ok || len(entries) != 1 || roots["Configured"] != true {
		t.Fatalf("configured storage roots are missing: %#v", roots)
	}
	entry, ok := entries[0].(map[string]any)
	if !ok || entry["Path"] != root || entry["Available"] != true {
		t.Errorf("storage root availability is incorrect: %#v", entries[0])
	}
}

func TestHTTPLatestGroupsSeriesAndEpisodesFilterBySeasonNumber(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	for _, relative := range []string{
		"tv/Example Show/Season 01/Example.Show.S01E01.mp4",
		"tv/Example Show/Season 01/Example.Show.S01E02.mp4",
		"tv/Example Show/Season 02/Example.Show.S02E01.mp4",
	} {
		writeAPIMediaFile(t, root, relative)
	}
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	libraryID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "tv"), "tvshows")
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	series, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+libraryID+"&IncludeItemTypes=Series", nil, headers))
	if total != 1 || len(series) != 1 || series[0]["Type"] != "Series" {
		t.Fatalf("scanner did not produce one television series: %#v", series)
	}
	seriesID := stringValue(t, series[0], "Id")
	seasons, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Seasons", nil, headers))
	if total != 2 || len(seasons) != 2 || seasons[0]["IndexNumber"] != float64(1) || seasons[1]["IndexNumber"] != float64(2) {
		t.Fatalf("television seasons are not ordered by their numeric indexes: %#v", seasons)
	}
	for _, season := range []struct{ number, expected int }{{1, 2}, {2, 1}} {
		values := url.Values{"Season": {strconv.Itoa(season.number)}}
		episodes, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes?"+values.Encode(), nil, headers))
		if total != season.expected || len(episodes) != season.expected {
			t.Fatalf("season %d episode count = %d/%d, want %d", season.number, len(episodes), total, season.expected)
		}
		for _, episode := range episodes {
			if episode["Type"] != "Episode" || episode["ParentIndexNumber"] != float64(season.number) {
				t.Errorf("numeric season filter returned another season: %#v", episode)
			}
		}
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes?Season=invalid", nil, headers), http.StatusBadRequest, "invalid_input", true)
	latestPath := "/emby/Users/" + adminID + "/Items/Latest?ParentId=" + libraryID + "&IncludeItemTypes=Episode"
	for _, suffix := range []string{"", "&GroupItems=true"} {
		latest := responseArray(t, f.request(t, http.MethodGet, latestPath+suffix, nil, headers))
		if len(latest) != 1 || latest[0]["Id"] != seriesID || latest[0]["Type"] != "Series" || latest[0]["ChildCount"] != float64(3) {
			t.Fatalf("latest items did not group episodes into their series: %#v", latest)
		}
		assertNoItemPaths(t, latest[0], root)
	}
	ungrouped := responseArray(t, f.request(t, http.MethodGet, latestPath+"&GroupItems=false", nil, headers))
	if len(ungrouped) != 3 {
		t.Fatalf("ungrouped latest items must return all three episodes: %#v", ungrouped)
	}
	for _, episode := range ungrouped {
		if episode["Type"] != "Episode" {
			t.Errorf("ungrouped latest returned a container: %#v", episode)
		}
	}
}
