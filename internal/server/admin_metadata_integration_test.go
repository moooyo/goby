//go:build linux

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const adminMetadataAutomaticNFO = `<movie><title>Automatic Alpha</title><sorttitle>alpha</sorttitle><originaltitle>Automatic Original</originaltitle><plot>Automatic overview.</plot><year>2020</year><premiered>2020-02-03</premiered><rating>7.5</rating><mpaa>PG</mpaa><uniqueid type="imdb">tt1234567</uniqueid><genre>Automatic Genre</genre><tag>automatic-tag</tag><studio>Automatic Studio</studio><actor><name>Automatic Actor</name><role>Lead</role></actor></movie>`

type adminMetadataHTTPFixture struct {
	*serverFixture
	root, libraryID, otherLibraryID string
	adminID, viewerID               string
	itemID, secondID, thirdID       string
	mediaPath, nfoPath              string
	cookie                          *http.Cookie
	csrf                            string
	adminToken, viewerToken         string
}

func newAdminMetadataHTTPFixture(t *testing.T) *adminMetadataHTTPFixture {
	t.Helper()
	f, root := newLibraryServerFixture(t)
	fixture := &adminMetadataHTTPFixture{serverFixture: f, root: root}
	fixture.adminID = f.bootstrap(t)
	fixture.cookie, fixture.csrf = f.adminLogin(t)
	fixture.mediaPath = writeAPIMediaFile(t, root, "movies/Alpha.mp4")
	fixture.nfoPath = strings.TrimSuffix(fixture.mediaPath, filepath.Ext(fixture.mediaPath)) + ".nfo"
	if err := os.WriteFile(fixture.nfoPath, []byte(adminMetadataAutomaticNFO), 0o600); err != nil {
		t.Fatalf("write administrator metadata fixture: %v", err)
	}
	secondPath := writeAPIMediaFile(t, root, "movies/Beta (2021).mp4")
	thirdPath := writeAPIMediaFile(t, root, "movies/Gamma (2022).mp4")
	writeAPIMediaFile(t, root, "other/Outside.mp4")
	fixture.libraryID = createAndScanAPILibrary(t, f, fixture.cookie, fixture.csrf, filepath.Join(root, "movies"), "movies")
	fixture.otherLibraryID = createAndScanAPILibrary(t, f, fixture.cookie, fixture.csrf, filepath.Join(root, "other"), "movies")
	for _, item := range []struct {
		path string
		id   *string
	}{{fixture.mediaPath, &fixture.itemID}, {secondPath, &fixture.secondID}, {thirdPath, &fixture.thirdID}} {
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id = $1 AND path = $2", fixture.libraryID, item.path).Scan(item.id); err != nil {
			t.Fatalf("find scanned metadata fixture: %v", err)
		}
	}
	viewer, err := f.users.CreateUser(f.ctx, "Metadata Viewer", "metadata-viewer-password", false)
	if err != nil {
		t.Fatalf("create metadata viewer: %v", err)
	}
	fixture.viewerID = viewer.ID
	fixture.adminToken = stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	fixture.viewerToken = stringValue(t, f.embyLogin(t, viewer.Name, "metadata-viewer-password"), "AccessToken")
	return fixture
}

func (f *adminMetadataHTTPFixture) detail(t *testing.T, itemID string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/items/"+itemID+"/metadata", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	return jsonObject(t, response)
}

func (f *adminMetadataHTTPFixture) update(t *testing.T, itemID, revision string, overrides map[string]any, locked []string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPut, "/admin/v1/items/"+itemID+"/metadata", map[string]any{
		"Revision": revision, "Overrides": overrides, "LockedFields": locked,
	}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
}

func (f *adminMetadataHTTPFixture) playbackCounts(t *testing.T) [3]int64 {
	t.Helper()
	var counts [3]int64
	if err := f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM user_item_data),
		(SELECT count(*) FROM play_sessions),
		(SELECT count(*) FROM encoding_jobs)`).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatalf("read playback side-effect counts: %v", err)
	}
	return counts
}

func (f *adminMetadataHTTPFixture) rescan(t *testing.T, nfo string) {
	t.Helper()
	if err := os.WriteFile(f.nfoPath, []byte(nfo), 0o600); err != nil {
		t.Fatalf("replace owned metadata sidecar: %v", err)
	}
	response := f.request(t, http.MethodPost, "/admin/v1/libraries/"+f.libraryID+"/scan", nil,
		http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	id := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	timer, ticker := time.NewTimer(15*time.Second), time.NewTicker(25*time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		jobs, _ := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, f.cookie))
		for _, job := range jobs {
			if job["Id"] != id {
				continue
			}
			switch job["Status"] {
			case "completed":
				if job["Error"] != "" {
					t.Fatal("metadata fixture rescan completed with an unexpected warning")
				}
				return
			case "failed", "cancelled", "interrupted":
				t.Fatal("metadata fixture rescan failed to complete")
			}
		}
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for metadata fixture rescan")
		case <-f.ctx.Done():
			t.Fatalf("metadata fixture context ended: %v", f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func adminMetadataHTTPRaw(t *testing.T, f *serverFixture, method, target, body string, headers http.Header, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(f.ctx)
	for key, values := range headers {
		for _, value := range values {
			r.Header.Add(key, value)
		}
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func adminMetadataHTTPRevision(t *testing.T, detail map[string]any) string {
	t.Helper()
	value := stringValue(t, detail, "Revision")
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != value {
		t.Fatal("metadata revision is not a canonical positive decimal string")
	}
	return value
}

func (f *adminMetadataHTTPFixture) embyDetail(t *testing.T, itemID string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+itemID, nil,
		http.Header{"X-Emby-Token": {f.viewerToken}})
	expectStatus(t, response, http.StatusOK)
	return jsonObject(t, response)
}

func TestHTTPAdminMetadataReadContractsAuthorizationAndQueries(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	beforeState := f.playbackCounts(t)
	listPath := "/admin/v1/libraries/" + f.libraryID + "/items"
	detailPath := "/admin/v1/items/" + f.itemID + "/metadata"
	for _, target := range []string{listPath, detailPath} {
		for _, headers := range []http.Header{nil, {"X-Emby-Token": {f.adminToken}}, {"X-Emby-Token": {f.viewerToken}}} {
			expectStatus(t, f.request(t, http.MethodGet, target, nil, headers), http.StatusUnauthorized)
		}
		embyCookie := &http.Cookie{Name: sessionCookie, Value: f.adminToken}
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil, embyCookie), http.StatusUnauthorized)
	}
	response := f.request(t, http.MethodGet, listPath+"?Types=Movie,Episode&StartIndex=1&Limit=1", nil, nil, f.cookie)
	items, total := responseItems(t, response)
	listing := jsonObject(t, response)
	if total != 3 || len(items) != 1 || items[0]["Id"] != f.secondID || listing["StartIndex"] != float64(1) || listing["Limit"] != float64(1) {
		t.Fatal("native metadata pagination did not retain the library-scoped total and stable order")
	}
	library := objectValue(t, listing, "Library")
	if len(library) != 3 || library["Id"] != f.libraryID || library["CollectionType"] != "movies" || len(items[0]) != 13 {
		t.Fatal("native metadata list did not use its bounded library/item summaries")
	}
	if items[0]["IndexNumber"] != nil || items[0]["ParentIndexNumber"] != nil || items[0]["HasOverrides"] != false || items[0]["LockedFieldCount"] != float64(0) {
		t.Fatal("unmodified movie summary invented numbering or manual metadata")
	}
	for _, test := range []struct {
		query string
		total int
		id    string
	}{
		{"SearchTerm=Automatic+Alpha&Types=Movie", 1, f.itemID},
		{"SearchTerm=%25&Types=Movie", 0, ""},
		{"Types=Episode", 0, ""},
		{"StartIndex=999&Limit=50&Types=Movie", 3, ""},
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, listPath+"?"+test.query, nil, nil, f.cookie))
		if total != test.total || test.id != "" && (len(items) != 1 || items[0]["Id"] != test.id) || test.id == "" && len(items) != 0 {
			t.Fatal("native metadata search/types/page bounds changed the expected item set")
		}
	}
	for _, query := range []string{"limit=1", "Limit=1&Limit=1", "SearchTerm=%ff", "Types=Movie,Movie", "Types=movie", "Limit=", "UserId=" + f.viewerID} {
		expectAPIError(t, f.request(t, http.MethodGet, listPath+"?"+query, nil, nil, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	for _, target := range []string{detailPath + "?Revision=1", detailPath + "?UserId=" + f.viewerID} {
		expectAPIError(t, f.request(t, http.MethodGet, target, nil, nil, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	for _, target := range []string{"/admin/v1/items/missing-item/metadata", "/admin/v1/items/1/metadata", "/admin/v1/libraries/missing-library/items"} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil, f.cookie), http.StatusNotFound)
	}
	detail := f.detail(t, f.itemID)
	adminMetadataHTTPRevision(t, detail)
	item := objectValue(t, detail, "Item")
	if len(detail) != 11 || len(item) != 8 || item["Id"] != f.itemID || item["LibraryId"] != f.libraryID || item["Path"] != f.mediaPath ||
		item["Name"] != "Automatic Alpha" || detail["LastEditedBy"] != "" || detail["LastEditedAt"] != nil {
		t.Fatal("initial metadata editor envelope changed its source or audit facts")
	}
	if inactive, ok := detail["InactiveFields"].([]any); !ok || len(inactive) != 0 {
		t.Fatal("initial metadata must expose a non-null empty inactive-field array")
	}
	for _, layer := range []string{"Automatic", "Effective"} {
		values := objectValue(t, detail, layer)
		if len(values) != 15 || values["Name"] != "Automatic Alpha" || values["ProductionYear"] != float64(2020) || values["IndexNumber"] != nil {
			t.Fatal("initial complete metadata values do not describe the automatic source")
		}
	}
	if len(objectValue(t, detail, "Overrides")) != 0 || len(objectValue(t, detail, "LockedValues")) != 0 || len(detail["LockedFields"].([]any)) != 0 {
		t.Fatal("an editor read created manual overrides or locks")
	}
	if after := f.playbackCounts(t); after != beforeState {
		t.Fatal("native metadata browsing created playback or user-state records")
	}
}

func TestHTTPAdminMetadataEditProjectsEntitiesAndPreservesManualLocksAcrossScans(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	stateBefore := f.playbackCounts(t)
	mediaBefore, err := os.ReadFile(f.mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	nfoBefore, err := os.ReadFile(f.nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	before := f.detail(t, f.itemID)
	overrides := map[string]any{
		"Name": "Manual Alpha", "SortName": "manual-alpha", "Overview": "Manual overview.", "OriginalTitle": "Manual Original",
		"ProductionYear": nil, "PremiereDate": nil, "CommunityRating": 0, "OfficialRating": "PG-13",
		"ProviderIds": map[string]string{"Imdb": "tt7654321"}, "Genres": []string{"Manual Genre"}, "Tags": []string{"manual-tag"},
		"Studios": []string{"Manual Studio"}, "People": []map[string]any{{"Name": "Manual Actor", "Type": "Actor", "Role": "Guide", "SortOrder": 0}},
	}
	response := f.update(t, f.itemID, adminMetadataHTTPRevision(t, before), overrides, []string{"Overview", "Genres"})
	expectStatus(t, response, http.StatusOK)
	edited := jsonObject(t, response)
	if edited["Revision"] == before["Revision"] || edited["LastEditedBy"] != f.adminID || edited["LastEditedAt"] == nil ||
		objectValue(t, edited, "Effective")["ProductionYear"] != nil || objectValue(t, edited, "Effective")["CommunityRating"] != float64(0) {
		t.Fatal("metadata edit lost revision/audit or explicit nullable/zero semantics")
	}
	if objectValue(t, edited, "LockedValues")["Overview"] != "Manual overview." || len(objectValue(t, edited, "LockedValues")) != 2 {
		t.Fatal("new locks did not capture the values produced by this edit")
	}
	for _, object := range []map[string]any{
		f.embyDetail(t, f.itemID),
		func() map[string]any {
			query := url.Values{"Ids": {f.itemID}, "Fields": {"Overview,OriginalTitle,ProductionYear,PremiereDate,CommunityRating,ProviderIds,Genres,Tags,Studios,People"}}
			items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+query.Encode(), nil, http.Header{"X-Emby-Token": {f.viewerToken}}))
			if total != 1 || len(items) != 1 {
				t.Fatal("edited item disappeared from Emby list projection")
			}
			return items[0]
		}(),
	} {
		if object["Name"] != "Manual Alpha" || object["Overview"] != "Manual overview." || object["OriginalTitle"] != "Manual Original" || object["CommunityRating"] != float64(0) {
			t.Fatal("native metadata changes did not reach Emby item projection")
		}
		if _, exists := object["ProductionYear"]; exists {
			t.Fatal("an explicitly cleared year was refilled from automatic metadata")
		}
		if _, exists := object["PremiereDate"]; exists {
			t.Fatal("an explicitly cleared premiere date was refilled from automatic metadata")
		}
		if objectValue(t, object, "ProviderIds")["Imdb"] != "tt7654321" {
			t.Fatal("native provider identifiers did not reach Emby output")
		}
	}
	for _, filter := range []struct{ field, value string }{{"Genres", "Manual Genre"}, {"Tags", "manual-tag"}, {"Studios", "Manual Studio"}, {"Person", "Manual Actor"}} {
		query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie"}, filter.field: {filter.value}}
		items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+query.Encode(), nil, http.Header{"X-Emby-Token": {f.viewerToken}}))
		if total != 1 || len(items) != 1 || items[0]["Id"] != f.itemID {
			t.Fatalf("metadata entity associations were not rebuilt for %s", filter.field)
		}
	}
	oldGenre := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie"}, "Genres": {"Automatic Genre"}}
	if _, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+oldGenre.Encode(), nil, http.Header{"X-Emby-Token": {f.viewerToken}})); total != 0 {
		t.Fatal("the old effective genre association survived a whole override replacement")
	}
	if data, err := os.ReadFile(f.nfoPath); err != nil || !bytes.Equal(data, nfoBefore) {
		t.Fatal("native metadata editing rewrote the automatic NFO source")
	}
	refreshedNFO := strings.NewReplacer("Automatic Alpha", "Refreshed Alpha", "Automatic overview.", "Refreshed overview.", "Automatic Genre", "Refreshed Genre").Replace(adminMetadataAutomaticNFO)
	f.rescan(t, refreshedNFO)
	refreshed := f.detail(t, f.itemID)
	if objectValue(t, refreshed, "Automatic")["Name"] != "Refreshed Alpha" || objectValue(t, refreshed, "Effective")["Name"] != "Manual Alpha" ||
		objectValue(t, refreshed, "Effective")["Overview"] != "Manual overview." {
		t.Fatal("valid NFO rescan replaced manual metadata or failed to refresh the automatic layer")
	}
	response = f.update(t, f.itemID, adminMetadataHTTPRevision(t, refreshed), map[string]any{"Name": "Manual Alpha"}, []string{"Overview", "Genres"})
	expectStatus(t, response, http.StatusOK)
	locked := jsonObject(t, response)
	if len(objectValue(t, locked, "Overrides")) != 1 || objectValue(t, locked, "Effective")["Overview"] != "Manual overview." || len(objectValue(t, locked, "LockedValues")) != 2 {
		t.Fatal("removing an override discarded its existing locked value")
	}
	latestNFO := strings.NewReplacer("Refreshed Alpha", "Latest Alpha", "Refreshed overview.", "Latest overview.", "Refreshed Genre", "Latest Genre").Replace(refreshedNFO)
	f.rescan(t, latestNFO)
	locked = f.detail(t, f.itemID)
	if objectValue(t, locked, "Automatic")["Overview"] != "Latest overview." || objectValue(t, locked, "Effective")["Overview"] != "Manual overview." {
		t.Fatal("NFO rescan overwrote a retained lock without an override")
	}
	response = f.update(t, f.itemID, adminMetadataHTTPRevision(t, locked), map[string]any{}, []string{})
	expectStatus(t, response, http.StatusOK)
	restored := jsonObject(t, response)
	if len(objectValue(t, restored, "Overrides")) != 0 || len(objectValue(t, restored, "LockedValues")) != 0 ||
		!reflect.DeepEqual(restored["Automatic"], restored["Effective"]) || objectValue(t, restored, "Effective")["Name"] != "Latest Alpha" {
		t.Fatal("clearing both manual layers did not restore the latest automatic values")
	}
	if f.embyDetail(t, f.itemID)["Name"] != "Latest Alpha" || objectValue(t, restored, "Item")["Path"] != f.mediaPath {
		t.Fatal("automatic restoration changed physical identity or failed to update Emby projection")
	}
	for _, filter := range []struct {
		genre string
		total int
	}{{"Latest Genre", 1}, {"Manual Genre", 0}} {
		query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie"}, "Genres": {filter.genre}}
		items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+query.Encode(), nil, http.Header{"X-Emby-Token": {f.viewerToken}}))
		if total != filter.total || total == 1 && (len(items) != 1 || items[0]["Id"] != f.itemID) {
			t.Fatal("clearing manual metadata left stale effective entity associations")
		}
	}
	if data, err := os.ReadFile(f.mediaPath); err != nil || !bytes.Equal(data, mediaBefore) {
		t.Fatal("metadata writes or rescans changed the media file")
	}
	if f.playbackCounts(t) != stateBefore {
		t.Fatal("metadata editing or browsing changed user playback state")
	}
}

func TestHTTPAdminMetadataRejectsStaleAndProtectedWritesWithoutSideEffects(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	path := "/admin/v1/items/" + f.itemID + "/metadata"
	initial := f.detail(t, f.itemID)
	response := f.update(t, f.itemID, adminMetadataHTTPRevision(t, initial), map[string]any{"Name": "Saved title"}, []string{})
	expectStatus(t, response, http.StatusOK)
	saved := jsonObject(t, response)
	expectAPIError(t, f.update(t, f.itemID, adminMetadataHTTPRevision(t, initial), map[string]any{"Name": "Stale title"}, []string{}), http.StatusConflict, "revision_conflict", false)
	if !reflect.DeepEqual(f.detail(t, f.itemID), saved) {
		t.Fatal("stale edit changed metadata, revision, or audit values")
	}
	revision := adminMetadataHTTPRevision(t, saved)
	valid := map[string]any{"Revision": revision, "Overrides": map[string]any{"Name": "Unauthorized title"}, "LockedFields": []string{}}
	expectStatus(t, f.request(t, http.MethodPut, path, valid, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPut, path, valid, http.Header{"X-Emby-Token": {f.adminToken}}), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPut, path, valid, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPut, path, valid, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example"}}, f.cookie), http.StatusForbidden)
	expectAPIError(t, f.request(t, http.MethodPut, path+"?Revision="+revision, valid, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie), http.StatusBadRequest, "invalid_input", false)
	for _, overrides := range []map[string]any{
		{"Path": "/outside/changed.mp4"}, {"Id": "different-item"}, {"LibraryId": f.otherLibraryID}, {"Type": "Audio"}, {"IsFolder": true},
		{"Kind": "movie"}, {"Media": map[string]any{}}, {"IndexNumber": 1}, {"ParentIndexNumber": 1}, {"Name": ""}, {"SortName": ""}, {"ProductionYear": "2020"},
		{"ProductionYear": 10000}, {"CommunityRating": 11}, {"Genres": nil}, {"ProviderIds": nil},
		{"People": []map[string]any{{"Name": "Actor", "Type": "Actor", "Path": "/private/credit.jpg"}}},
	} {
		expectAPIError(t, f.update(t, f.itemID, revision, overrides, []string{}), http.StatusBadRequest, "invalid_input", false)
	}
	for _, locks := range [][]string{{"Path"}, {"Overview", "Overview"}} {
		expectAPIError(t, f.update(t, f.itemID, revision, map[string]any{}, locks), http.StatusBadRequest, "invalid_input", false)
	}
	for _, body := range []string{
		`{"Revision":"` + revision + `","Overrides":{},"LockedFields":[],"Path":"protected"}`,
		`{"Revision":"` + revision + `","Overrides":{"Name":"one","Name":"two"},"LockedFields":[]}`,
		`{"Revision":"` + revision + `","Overrides":{"ProviderIds":{"Imdb":"one","Imdb":"two"}},"LockedFields":[]}`,
		`{"Revision":"` + revision + `","Overrides":{},"LockedFields":null}`,
	} {
		response := adminMetadataHTTPRaw(t, f.serverFixture, http.MethodPut, path, body, http.Header{"Content-Type": {"application/json"}, "X-CSRF-Token": {f.csrf}}, f.cookie)
		expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
	}
	if !reflect.DeepEqual(f.detail(t, f.itemID), saved) {
		t.Fatal("rejected metadata writes changed the saved state")
	}
	f.rescan(t, strings.Replace(adminMetadataAutomaticNFO, "Automatic overview.", "New automatic source.", 1))
	afterScan := f.detail(t, f.itemID)
	if afterScan["Revision"] == revision {
		t.Fatal("automatic source change failed to invalidate the editor revision")
	}
	expectAPIError(t, f.update(t, f.itemID, revision, map[string]any{}, []string{}), http.StatusConflict, "revision_conflict", false)
	if !reflect.DeepEqual(f.detail(t, f.itemID), afterScan) {
		t.Fatal("stale edit after a rescan changed metadata")
	}
}

func TestHTTPAdminMetadataEmptyEditPreservesNFOAbsentEmbyProjections(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	writeAPIMediaFile(t, f.root, "series/Example Show/Season 01/Example Show S01E02.mp4")
	writeAPIMediaFile(t, f.root, "music/Example Artist/Example Album/01 - Example Track.mp3")
	seriesID := createAndScanAPILibrary(t, f.serverFixture, f.cookie, f.csrf, filepath.Join(f.root, "series"), "tvshows")
	musicID := createAndScanAPILibrary(t, f.serverFixture, f.cookie, f.csrf, filepath.Join(f.root, "music"), "music")
	stateBefore := f.playbackCounts(t)
	for _, test := range []struct{ libraryID, kind string }{{f.libraryID, "Movie"}, {seriesID, "Season"}, {seriesID, "Episode"}, {musicID, "Audio"}} {
		t.Run(test.kind, func(t *testing.T) {
			id := f.secondID
			if test.kind != "Movie" {
				if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id = $1 AND type = $2 ORDER BY id LIMIT 1", test.libraryID, test.kind).Scan(&id); err != nil {
					t.Fatalf("find unannotated typed metadata fixture: %v", err)
				}
			}
			before := f.embyDetail(t, id)
			detail := f.detail(t, id)
			editable := map[string]bool{}
			for _, field := range detail["EditableFields"].([]any) {
				editable[field.(string)] = true
			}
			if editable["ParentIndexNumber"] || editable["IndexNumber"] != (test.kind == "Episode") {
				t.Fatal("editable metadata fields exposed a structural season or audio-track number")
			}
			if test.kind == "Episode" {
				rowState := func() string {
					var snapshot string
					if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('Item', to_jsonb(i), 'Metadata', to_jsonb(ms))::text
						FROM items i JOIN item_metadata_state ms ON ms.item_id = i.id WHERE i.id = $1`, id).Scan(&snapshot); err != nil {
						t.Fatalf("read episode metadata row snapshot: %v", err)
					}
					return snapshot
				}
				beforeRow := rowState()
				expectAPIError(t, f.update(t, id, adminMetadataHTTPRevision(t, detail), map[string]any{"IndexNumber": nil}, []string{}), http.StatusBadRequest, "invalid_input", false)
				if rowState() != beforeRow || !reflect.DeepEqual(f.detail(t, id), detail) || !reflect.DeepEqual(f.embyDetail(t, id), before) {
					t.Fatal("rejected null episode number changed a catalog row, revision, or projection")
				}
			}
			readOnly := "IndexNumber"
			if test.kind == "Episode" {
				readOnly = "ParentIndexNumber"
			}
			// A structural field remains read-only even when its supplied
			// value is unchanged or the request only tries to lock it.
			expectAPIError(t, f.update(t, id, adminMetadataHTTPRevision(t, detail), map[string]any{readOnly: objectValue(t, detail, "Automatic")[readOnly]}, []string{}), http.StatusBadRequest, "invalid_input", false)
			expectAPIError(t, f.update(t, id, adminMetadataHTTPRevision(t, detail), map[string]any{}, []string{readOnly}), http.StatusBadRequest, "invalid_input", false)
			response := f.update(t, id, adminMetadataHTTPRevision(t, detail), map[string]any{}, []string{})
			expectStatus(t, response, http.StatusOK)
			after := f.embyDetail(t, id)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("an empty administrator edit fabricated NFO fields or changed physical numbering")
			}
			if len(objectValue(t, jsonObject(t, response), "Overrides")) != 0 || len(objectValue(t, jsonObject(t, response), "LockedValues")) != 0 {
				t.Fatal("an empty edit fabricated a manual metadata layer")
			}
		})
	}
	if f.playbackCounts(t) != stateBefore {
		t.Fatal("reading metadata compatibility projections changed playback state")
	}
}

func TestHTTPAdminMetadataWritesRevalidateActorAfterMiddleware(t *testing.T) {
	for _, authority := range []string{"revoked", "demoted", "disabled", "emby-kind"} {
		t.Run(authority, func(t *testing.T) {
			f := newAdminMetadataHTTPFixture(t)
			observer, err := f.users.CreateUser(f.ctx, "Metadata Observer", "metadata-observer-password", true)
			if err != nil {
				t.Fatal(err)
			}
			observerCookie, _ := managedHTTPLogin(t, f.serverFixture, observer.Name, "metadata-observer-password")
			before := f.detail(t, f.itemID)
			path := "/admin/v1/items/" + f.itemID + "/metadata"
			originalHandler := f.handler
			mux := http.NewServeMux()
			if authority == "emby-kind" {
				// A weaker middleware must not let an administrator's Emby
				// token become authority for a native management mutation.
				mux.HandleFunc("PUT /admin/v1/items/{id}/metadata", f.app.requireEmby(f.app.updateAdminItemMetadata))
			} else {
				mux.HandleFunc("PUT /admin/v1/items/{id}/metadata", f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
					// Change stored authority after middleware resolved a trusted
					// principal, without relying on timing or scheduler delays.
					switch authority {
					case "revoked":
						if err := f.users.Revoke(f.ctx, f.cookie.Value); err != nil {
							t.Fatal(err)
						}
					case "demoted":
						if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", f.adminID); err != nil {
							t.Fatal(err)
						}
					case "disabled":
						if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", f.adminID); err != nil {
							t.Fatal(err)
						}
					}
					f.app.updateAdminItemMetadata(w, r)
				}))
			}
			f.handler = f.app.middleware(mux)
			body := map[string]any{"Revision": adminMetadataHTTPRevision(t, before), "Overrides": map[string]any{"Name": "Unauthorized race value"}, "LockedFields": []string{}}
			var response *httptest.ResponseRecorder
			if authority == "emby-kind" {
				response = f.request(t, http.MethodPut, path, body, http.Header{"X-Emby-Token": {f.adminToken}})
			} else {
				response = f.request(t, http.MethodPut, path, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
			}
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("stale or wrong-kind actor reached metadata mutation: status %d", response.Code)
			}
			f.handler = originalHandler
			after := f.request(t, http.MethodGet, path, nil, nil, observerCookie)
			expectStatus(t, after, http.StatusOK)
			if !reflect.DeepEqual(before, jsonObject(t, after)) {
				t.Fatal("an unauthorized actor changed metadata, revision, or audit state")
			}
		})
	}
}

func TestHTTPAdminMetadataReadsRevalidateRevokedActorAfterMiddleware(t *testing.T) {
	for _, operation := range []string{"list", "detail"} {
		t.Run(operation, func(t *testing.T) {
			f := newAdminMetadataHTTPFixture(t)
			pattern, path := "GET /admin/v1/items/{id}/metadata", "/admin/v1/items/"+f.itemID+"/metadata"
			handler := f.app.adminItemMetadata
			if operation == "list" {
				pattern, path = "GET /admin/v1/libraries/{id}/items", "/admin/v1/libraries/"+f.libraryID+"/items"
				handler = f.app.adminMetadataItems
			}
			mux := http.NewServeMux()
			mux.HandleFunc(pattern, f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
				if err := f.users.Revoke(f.ctx, f.cookie.Value); err != nil {
					t.Fatal(err)
				}
				handler(w, r)
			}))
			f.handler = f.app.middleware(mux)
			response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("revoked metadata reader retained administrative access: status %d", response.Code)
			}
			if strings.Contains(response.Body.String(), f.mediaPath) || strings.Contains(response.Body.String(), "Automatic overview.") {
				t.Fatal("a revoked metadata reader received item path or editable values")
			}
		})
	}
}
