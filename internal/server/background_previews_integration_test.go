//go:build linux

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func backgroundHTTPCounts(t *testing.T, f *serverFixture) [4]int64 {
	t.Helper()
	var counts [4]int64
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM background_preview_queue),
		(SELECT count(*) FROM background_preview_requests),(SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM item_background_preview_settings)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3]); err != nil {
		t.Fatal(err)
	}
	return counts
}

func backgroundHTTPConfiguration(t *testing.T, f *serverFixture, cookie *http.Cookie) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/background-previews/configuration", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	return jsonObject(t, response)
}

func TestHTTPBackgroundPreviewConfigurationAuthenticationStrictCASAndNoGeneration(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := "/admin/v1/background-previews/configuration"
	for _, target := range []string{path, "/admin/v1/background-previews/items"} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil), http.StatusUnauthorized)
		token := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
		expectStatus(t, f.request(t, http.MethodGet, target, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized)
	}
	initial := backgroundHTTPConfiguration(t, f, cookie)
	if initial["Revision"] != "1" || !reflect.DeepEqual(initial["Profile"], initial["Defaults"]) || objectValue(t, initial, "Profile")["DurationSeconds"] != float64(25) {
		t.Fatalf("default clip configuration changed: %#v", initial)
	}
	counts := backgroundHTTPCounts(t, f)
	body := map[string]any{"Revision": initial["Revision"], "Profile": initial["Profile"]}
	expectStatus(t, f.request(t, http.MethodPut, path, body, nil, cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPut, path, body, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://outside.example"}}, cookie), http.StatusForbidden)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	response := f.request(t, http.MethodPut, path, body, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, response), initial) {
		t.Fatal("no-op configuration changed its revision or update time")
	}
	const valid = `{"Revision":"1","Profile":{"DurationSeconds":25,"MaxWidth":1280,"VideoBitrate":1500000,"MaxItemRuntimeSeconds":1200}}`
	for _, raw := range []string{
		`null`, `{}`, valid + `{}`,
		strings.Replace(valid, `"Revision":"1"`, `"Revision":null`, 1),
		strings.Replace(valid, `"Revision":"1"`, `"Revision":"1","Revision":"2"`, 1),
		strings.Replace(valid, `"Revision":"1"`, `"Revision":"01"`, 1),
		strings.Replace(valid, `"Profile":{`, `"Extra":true,"Profile":{`, 1),
		strings.Replace(valid, `"DurationSeconds":25`, `"DurationSeconds":null`, 1),
		strings.Replace(valid, `"DurationSeconds":25`, `"DurationSeconds":25.0`, 1),
		strings.Replace(valid, `"DurationSeconds":25`, `"DurationSeconds":4`, 1),
		strings.Replace(valid, `"DurationSeconds":25`, `"DurationSeconds":61`, 1),
		strings.Replace(valid, `"MaxWidth":1280`, `"MaxWidth":1280,"Max\u0057idth":640`, 1),
		strings.Replace(valid, `"MaxWidth":1280`, `"maxWidth":1280`, 1),
		strings.Replace(valid, `"MaxWidth":1280`, `"MaxWidth":720`, 1),
		strings.Replace(valid, `"VideoBitrate":1500000`, `"VideoBitrate":"1500000"`, 1),
		strings.Replace(valid, `"MaxItemRuntimeSeconds":1200`, `"MaxItemRuntimeSeconds":null`, 1),
	} {
		expectAPIError(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPut, path, raw, "application/json"), http.StatusBadRequest, "invalid_input", false)
	}
	for _, suffix := range []string{"?", "?Force=true"} {
		expectStatus(t, f.request(t, http.MethodGet, path+suffix, nil, nil, cookie), http.StatusBadRequest)
		expectStatus(t, f.request(t, http.MethodPut, path+suffix, body, headers, cookie), http.StatusBadRequest)
	}
	expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPut, path, valid, "text/plain"), http.StatusUnsupportedMediaType)
	profile := objectValue(t, initial, "Profile")
	profile["VideoBitrate"] = float64(2000000)
	body["Profile"] = profile
	response = f.request(t, http.MethodPut, path, body, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	saved := jsonObject(t, response)
	if saved["Revision"] != "2" || objectValue(t, saved, "Profile")["VideoBitrate"] != float64(2000000) {
		t.Fatalf("configuration did not persist: %#v", saved)
	}
	expectAPIError(t, f.request(t, http.MethodPut, path, body, headers, cookie), http.StatusConflict, "background_preview_conflict", false)
	if !reflect.DeepEqual(backgroundHTTPConfiguration(t, f, cookie), saved) || backgroundHTTPCounts(t, f) != counts {
		t.Fatal("configuration update queued generation or changed unrelated state")
	}
	page := f.request(t, http.MethodGet, "/admin/v1/background-previews/items?Limit=1&StartIndex=0", nil, nil, cookie)
	expectStatus(t, page, http.StatusOK)
	object := jsonObject(t, page)
	if !reflect.DeepEqual(object["Items"], []any{}) || object["TotalRecordCount"] != float64(0) || object["Limit"] != float64(1) {
		t.Fatalf("empty page contract changed: %#v", object)
	}
}

func TestHTTPBackgroundPreviewItemStartIsIndependentOfPersistentArtifact(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	path := "/admin/v1/items/" + f.itemID + "/background-preview"
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {f.viewerToken}}, {"X-Emby-Token": {f.adminToken}}} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized)
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"?Force=true", nil, nil, f.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/items/missing-item/background-preview", nil, nil, f.cookie), http.StatusNotFound)
	response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	if detail["Revision"] != "0" || detail["StartTicks"] != nil || detail["State"] != "missing" || objectValue(t, detail, "Artifact")["Available"] != false {
		t.Fatalf("unscheduled detail changed: %#v", detail)
	}
	counts, playback := backgroundHTTPCounts(t, f.serverFixture), f.playbackCounts(t)
	data, manifestPath := writeBackgroundHTTPArtifact(t, f)
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": int64(0)}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	expectStatus(t, f.request(t, http.MethodPut, path, body, nil, f.cookie), http.StatusForbidden)
	for _, ticks := range []int64{-1, int64(detail["DurationTicks"].(float64))} {
		invalid := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": ticks}
		expectStatus(t, f.request(t, http.MethodPut, path, invalid, headers, f.cookie), http.StatusBadRequest)
	}
	valid := fmt.Sprintf(`{"Revision":"0","SourceRevision":%q,"StartTicks":0}`, detail["SourceRevision"])
	for _, raw := range []string{
		strings.Replace(valid, `"Revision":"0"`, `"Revision":null`, 1),
		strings.Replace(valid, `"Revision":"0"`, `"Revision":"00"`, 1),
		strings.Replace(valid, `"Revision":"0"`, `"Revision":"0","Revision":"1"`, 1),
		strings.Replace(valid, `"StartTicks":0`, `"StartTicks":0.5`, 1),
		strings.Replace(valid, `"StartTicks":0`, `"StartTicks":"0"`, 1),
		strings.Replace(valid, `"StartTicks":0`, `"StartTicks":0,"StartTicks":1`, 1),
		strings.Replace(valid, `"StartTicks":0`, `"StartTicks":0,"Force":true`, 1),
		strings.Replace(valid, `,"StartTicks":0`, ``, 1),
	} {
		expectStatus(t, adminTaskHTTPRaw(f.serverFixture, f.cookie, f.csrf, http.MethodPut, path, raw, "application/json"), http.StatusBadRequest)
	}
	response = f.request(t, http.MethodPut, path, body, headers, f.cookie)
	expectStatus(t, response, http.StatusOK)
	saved := jsonObject(t, response)
	if saved["Revision"] != "1" || saved["StartTicks"] != float64(0) || objectValue(t, saved, "Artifact")["Available"] != true || objectValue(t, saved, "Artifact")["StartPositionTicks"] != float64(300000000) {
		t.Fatalf("saved edit replaced artifact projection: %#v", saved)
	}
	expectAPIError(t, f.request(t, http.MethodPut, path, body, headers, f.cookie), http.StatusConflict, "background_preview_conflict", false)
	wrongSource := map[string]any{"Revision": saved["Revision"], "SourceRevision": "obsolete-source", "StartTicks": nil}
	expectAPIError(t, f.request(t, http.MethodPut, path, wrongSource, headers, f.cookie), http.StatusConflict, "background_preview_conflict", false)
	reset := map[string]any{"Revision": saved["Revision"], "SourceRevision": saved["SourceRevision"], "StartTicks": nil}
	response = f.request(t, http.MethodPut, path, reset, headers, f.cookie)
	expectStatus(t, response, http.StatusOK)
	cleared := jsonObject(t, response)
	if cleared["Revision"] != "2" || cleared["StartTicks"] != nil || objectValue(t, cleared, "Artifact")["Available"] != true {
		t.Fatalf("null start did not reset only the future selection: %#v", cleared)
	}
	configuration := backgroundHTTPConfiguration(t, f.serverFixture, f.cookie)
	profile := objectValue(t, configuration, "Profile")
	profile["VideoBitrate"] = float64(2000000)
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/background-previews/configuration",
		map[string]any{"Revision": configuration["Revision"], "Profile": profile}, headers, f.cookie), http.StatusOK)
	counts[3]++
	if backgroundHTTPCounts(t, f.serverFixture) != counts || f.playbackCounts(t) != playback {
		t.Fatal("start administration queued generation or wrote playback history")
	}
	manifestAfter, err := os.ReadFile(manifestPath)
	if err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("saving an editorial start changed the existing manifest")
	}
	stream := f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/BackgroundPreview/stream.mp4", nil, http.Header{"X-Emby-Token": {f.viewerToken}})
	expectStatus(t, stream, http.StatusOK)
	if !bytes.Equal(stream.Body.Bytes(), data) {
		t.Fatal("saving an editorial start replaced existing clip bytes")
	}
}

// This fixture models the immutable sidecar wire format only. Its synthetic
// MP4 header exercises authenticated HTTP transport; decoding is covered by the
// real FFmpeg acceptance suite rather than inferred from these bytes.
func writeBackgroundHTTPArtifact(t *testing.T, f *adminMetadataHTTPFixture) ([]byte, string) {
	t.Helper()
	return writeBackgroundHTTPArtifactVersion(t, f, 'a')
}

func writeBackgroundHTTPArtifactVersion(t *testing.T, f *adminMetadataHTTPFixture, version byte) ([]byte, string) {
	t.Helper()
	name := filepath.Base(f.mediaPath)
	key := sha256.Sum256([]byte(name))
	directory := filepath.Join(filepath.Dir(f.mediaPath), "backdrops", "goby", hex.EncodeToString(key[:]))
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 128)
	copy(data, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	for index := 12; index < len(data); index++ {
		data[index] = byte(index)
	}
	data[len(data)-1] = version
	file := "gen-" + strings.Repeat(string(version), 32) + ".mp4"
	if err := os.WriteFile(filepath.Join(directory, file), data, 0644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	manifest := map[string]any{"Format": "goby-background-clip-v1", "SourceName": name, "SourceRevision": "transport-fixture-source",
		"SourceSnapshot": `"earlier-indexed-snapshot"`, "Generation": file, "Profile": "transport-fixture-v1", "StartTicks": int64(300000000),
		"DurationTicks": int64(250000000), "Width": 1280, "Height": 720, "Size": len(data), "SHA256": hex.EncodeToString(digest[:]),
		"FFmpegSHA256": strings.Repeat("b", 64), "FFprobeSHA256": strings.Repeat("c", 64), "CreatedAt": time.Now().UTC()}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		t.Fatal(err)
	}
	return data, path
}

func TestHTTPBackgroundPreviewPublicLookupAndRangeDoNotGenerateOrReportPlayback(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	path := "/emby/Items/" + f.itemID + "/BackgroundPreview"
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	counts, playback := backgroundHTTPCounts(t, f.serverFixture), f.playbackCounts(t)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	for _, suffix := range []string{"", "?Force=true&Generate=true"} {
		response := f.request(t, http.MethodGet, path+suffix, nil, headers)
		expectStatus(t, response, http.StatusOK)
		if !reflect.DeepEqual(jsonObject(t, response), map[string]any{"Available": false}) || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("absent generated material was not an ordinary private fallback: %s", response.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.mediaPath), "backdrops")); !os.IsNotExist(err) {
		t.Fatal("read-only lookup created a media-side directory")
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"/stream.mp4", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/missing-item/BackgroundPreview", nil, headers), http.StatusNotFound)
	data, _ := writeBackgroundHTTPArtifact(t, f)
	response := f.request(t, http.MethodGet, path, nil, headers)
	expectStatus(t, response, http.StatusOK)
	artifact := jsonObject(t, response)
	streamURL, err := url.Parse(stringValue(t, artifact, "StreamUrl"))
	if err != nil || streamURL.Path != path+"/stream.mp4" || streamURL.Query().Get("tag") == "" || artifact["Available"] != true || artifact["SourceChanged"] != true || artifact["Size"] != float64(len(data)) {
		t.Fatalf("persistent artifact projection changed: %#v", artifact)
	}
	for _, private := range []string{f.root, "SourceSnapshot", "SourceRevision", "FFmpegSHA256", "OperationID", "Generation"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("public artifact exposed internal storage detail %q", private)
		}
	}
	rangeHeaders := headers.Clone()
	rangeHeaders.Set("Range", "bytes=4-11")
	ranged := f.request(t, http.MethodGet, path+"/stream.mp4", nil, rangeHeaders)
	expectStatus(t, ranged, http.StatusPartialContent)
	if !bytes.Equal(ranged.Body.Bytes(), data[4:12]) || ranged.Header().Get("Content-Range") != "bytes 4-11/128" || ranged.Header().Get("Content-Type") != "video/mp4" || ranged.Header().Get("X-Content-Type-Options") != "nosniff" || ranged.Header().Get("ETag") == "" {
		t.Fatalf("clip range response changed: %#v %q", ranged.Header(), ranged.Body.String())
	}
	head := f.request(t, http.MethodHead, path+"/stream.mp4", nil, headers)
	expectStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != "128" {
		t.Fatal("HEAD returned bytes or omitted the persistent size")
	}
	conditional := headers.Clone()
	conditional.Set("If-None-Match", ranged.Header().Get("ETag"))
	expectStatus(t, f.request(t, http.MethodGet, path+"/stream.mp4", nil, conditional), http.StatusNotModified)
	if backgroundHTTPCounts(t, f.serverFixture) != counts || f.playbackCounts(t) != playback {
		t.Fatal("background lookup or byte ranges queued work or wrote playback state")
	}
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, path+"/stream.mp4", nil, rangeHeaders), http.StatusForbidden)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":true}`)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, path+"/stream.mp4", nil, rangeHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, path+"?UserId="+f.adminID, nil, headers), http.StatusForbidden)
}

func TestHTTPBackgroundPreviewVersionTagCannotReadDifferentGeneration(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	path := "/emby/Items/" + f.itemID + "/BackgroundPreview"
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	counts, playback := backgroundHTTPCounts(t, f.serverFixture), f.playbackCounts(t)
	currentURL := func() string {
		response := f.request(t, http.MethodGet, path, nil, headers)
		expectStatus(t, response, http.StatusOK)
		return stringValue(t, jsonObject(t, response), "StreamUrl")
	}
	oldBytes, _ := writeBackgroundHTTPArtifactVersion(t, f, 'a')
	oldURL := currentURL()
	old := f.request(t, http.MethodGet, oldURL, nil, headers)
	expectStatus(t, old, http.StatusOK)
	parsed, err := url.Parse(oldURL)
	if err != nil || len(parsed.Query()["tag"]) != 1 || parsed.Query().Get("tag") != old.Header().Get("ETag") || !bytes.Equal(old.Body.Bytes(), oldBytes) {
		t.Fatalf("descriptor did not bind its current generation: %s", oldURL)
	}
	// Publish a different immutable generation, retaining the old file. A URL
	// obtained before publication must not silently return the newer bytes.
	newBytes, _ := writeBackgroundHTTPArtifactVersion(t, f, 'b')
	newURL := currentURL()
	if oldURL == newURL || bytes.Equal(oldBytes, newBytes) {
		t.Fatal("generation replacement did not change its descriptor identity")
	}
	rangeHeaders := headers.Clone()
	rangeHeaders.Set("Range", "bytes=120-127")
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		stale := f.request(t, method, oldURL, nil, rangeHeaders)
		expectStatus(t, stale, http.StatusNotFound)
		if stale.Header().Get("Content-Type") == "video/mp4" || bytes.Equal(stale.Body.Bytes(), oldBytes[120:]) || bytes.Equal(stale.Body.Bytes(), newBytes[120:]) {
			t.Fatal("stale descriptor leaked bytes from an old or replacement generation")
		}
	}
	current := f.request(t, http.MethodGet, newURL, nil, rangeHeaders)
	expectStatus(t, current, http.StatusPartialContent)
	if !bytes.Equal(current.Body.Bytes(), newBytes[120:]) || current.Header().Get("ETag") == old.Header().Get("ETag") {
		t.Fatal("new descriptor did not read the replacement generation")
	}
	expectStatus(t, f.request(t, http.MethodGet, newURL+"&tag="+url.QueryEscape(current.Header().Get("ETag")), nil, headers), http.StatusNotFound)
	if backgroundHTTPCounts(t, f.serverFixture) != counts || f.playbackCounts(t) != playback {
		t.Fatal("versioned clip requests changed generation or playback state")
	}
}

func TestHTTPBackgroundPreviewInventoryIsBoundedAndFolderFallbackIsSafe(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	base := "/admin/v1/background-previews/items"
	response := f.request(t, http.MethodGet, base+"?LibraryId="+f.libraryID+"&State=missing&StartIndex=1&Limit=1", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	items, ok := page["Items"].([]any)
	if !ok || len(items) != 1 || page["TotalRecordCount"] != float64(3) || items[0].(map[string]any)["ItemId"] != f.secondID {
		t.Fatalf("inventory count or library scope changed: %#v", page)
	}
	for _, query := range []string{"Unknown=1", "Limit=0", "Limit=201", "Limit=01", "StartIndex=-1", "StartIndex=1000001", "Limit=1&Limit=2", "State=unknown", "SearchTerm=%00", "SearchTerm=%ff", "SearchTerm=" + strings.Repeat("x", 257)} {
		expectStatus(t, f.request(t, http.MethodGet, base+"?"+query, nil, nil, f.cookie), http.StatusBadRequest)
	}
	writeAPIMediaFile(t, f.root, "shows/Example/Season 01/Example S01E01.mp4")
	television := createAndScanAPILibrary(t, f.serverFixture, f.cookie, f.csrf, filepath.Join(f.root, "shows"), "tvshows")
	var series string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series' LIMIT 1`, television).Scan(&series); err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	response = f.request(t, http.MethodGet, "/emby/Items/"+series+"/BackgroundPreview", nil, headers)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, response), map[string]any{"Available": false}) {
		t.Fatal("folder requested a source-bound clip")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=NULL WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	response = f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/BackgroundPreview", nil, headers)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, response), map[string]any{"Available": false}) {
		t.Fatal("an unprobed source was not an ordinary fallback")
	}
}

func TestHTTPBackgroundPreviewUnavailableRunDoesNotQueueAndRevokedEditCannotCommit(t *testing.T) {
	f := newAdminIntroHTTPFixture(t)
	// This tests unavailable admission, not a simulated healthy encoder. The
	// independent runtime is deliberately removed before any request can start.
	f.app.backgroundPreviews = nil
	counts := backgroundHTTPCounts(t, f.serverFixture)
	body := map[string]any{"Kind": "background", "RequestId": "unavailable-background", "LibraryIds": []string{}, "ItemIds": []string{f.itemID}, "Force": true}
	path := "/admin/v1/media-analysis/runs"
	expectStatus(t, f.request(t, http.MethodPost, path, body, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, path, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie), http.StatusServiceUnavailable)
	if backgroundHTTPCounts(t, f.serverFixture) != counts {
		t.Fatal("unavailable encoder admitted a durable generation request")
	}
	initial := backgroundHTTPConfiguration(t, f.serverFixture, f.cookie)
	encoded, err := json.Marshal(map[string]any{"Revision": initial["Revision"], "Profile": map[string]any{"DurationSeconds": 25, "MaxWidth": 1280, "VideoBitrate": 2000000, "MaxItemRuntimeSeconds": 1200}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &analysisRevokeBody{reader: bytes.NewReader(encoded), revoke: func() {
		if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id=$1 AND kind='admin'`, f.adminID); err != nil {
			t.Fatal(err)
		}
	}}
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/background-previews/configuration", reader).WithContext(f.ctx)
	r.AddCookie(f.cookie)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	expectStatus(t, w, http.StatusUnauthorized)
	var revision, bitrate int
	if err := f.pool.QueryRow(f.ctx, `SELECT revision,video_bitrate FROM background_preview_settings WHERE id=1`).Scan(&revision, &bitrate); err != nil || revision != 1 || bitrate != 1500000 {
		t.Fatal("revoked native session changed the independent generation profile")
	}
}
