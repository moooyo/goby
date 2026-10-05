//go:build linux

package server

import (
	"fmt"
	"net/http"
	"testing"
)

func creditsHTTPMarker(t *testing.T, object map[string]any) *int64 {
	t.Helper()
	chapters, ok := object["Chapters"].([]any)
	if !ok {
		t.Fatal("chapters must be an array")
	}
	var result *int64
	for _, raw := range chapters {
		chapter, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("chapter must be an object")
		}
		if chapter["MarkerType"] != "CreditsStart" {
			continue
		}
		if result != nil {
			t.Fatal("public chapters contain duplicate credits markers")
		}
		value, ok := chapter["StartPositionTicks"].(float64)
		if !ok {
			t.Fatal("credits marker lacks a numeric position")
		}
		ticks := int64(value)
		result = &ticks
	}
	return result
}

func TestAdminCreditsMarkersLifecycleAndProjection(t *testing.T) {
	// The existing intro fixture provides actual file snapshots and ordinary
	// chapters without automatic markers of either kind.
	f := newAdminIntroHTTPFixture(t)
	path := "/admin/v1/items/" + f.itemID + "/credits"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}, "Content-Type": {"application/json"}}
	viewer := http.Header{"X-Emby-Token": {f.viewerToken}}
	for _, headers := range []http.Header{nil, viewer, {"X-Emby-Token": {f.adminToken}}} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized)
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"?UserId="+f.viewerID, nil, nil, f.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/items/missing-item/credits", nil, nil, f.cookie), http.StatusNotFound)
	response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	if detail["Revision"] != "0" || detail["Effective"] != nil || detail["Override"] != nil || detail["Automatic"] != nil || detail["OverrideStale"] != false {
		t.Fatalf("ordinary chapters inferred credits: %#v", detail)
	}
	start := int64(100_000_000)
	body := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": start, "Provenance": "Manual"}
	counts := f.playbackCounts(t)
	for _, ticks := range []int64{-1, int64(detail["DurationTicks"].(float64)), int64(detail["DurationTicks"].(float64)) + 1} {
		invalid := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": ticks, "Provenance": "Import"}
		expectAPIError(t, f.request(t, http.MethodPut, path, invalid, csrf, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	for _, revision := range []string{"", "-1", "00", "+0", "0.0", "9223372036854775808"} {
		invalid := map[string]any{"Revision": revision, "SourceRevision": detail["SourceRevision"], "StartTicks": start, "Provenance": "Manual"}
		expectAPIError(t, f.request(t, http.MethodPut, path, invalid, csrf, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	for _, provenance := range []string{"Chapter", "Detected", "manual", ""} {
		invalid := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": start, "Provenance": provenance}
		expectAPIError(t, f.request(t, http.MethodPut, path, invalid, csrf, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	for _, raw := range []string{
		fmt.Sprintf(`{"Revision":"0","Revision":"0","SourceRevision":%q,"StartTicks":0,"Provenance":"Manual"}`, detail["SourceRevision"]),
		fmt.Sprintf(`{"Revision":"0","SourceRevision":%q,"StartTicks":null,"Provenance":"Manual"}`, detail["SourceRevision"]),
		fmt.Sprintf(`{"Revision":"0","SourceRevision":%q,"StartTicks":0,"Provenance":"Manual","EndTicks":1}`, detail["SourceRevision"]),
	} {
		expectAPIError(t, adminMetadataHTTPRaw(t, f.serverFixture, http.MethodPut, path, raw, csrf, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	expectStatus(t, f.request(t, http.MethodPut, path, body, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPut, path, body, http.Header{"X-CSRF-Token": {"wrong-token"}}, f.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPut, path, body, csrf, f.cookie)
	expectStatus(t, response, http.StatusOK)
	edited := jsonObject(t, response)
	if edited["Revision"] != "1" || edited["LastEditedBy"] != f.adminID || edited["LastEditedAt"] == nil || objectValue(t, edited, "Effective")["Provenance"] != "Manual" {
		t.Fatalf("manual credits: %#v", edited)
	}
	expectAPIError(t, f.request(t, http.MethodPut, path, body, csrf, f.cookie), http.StatusConflict, "credits_revision_conflict", false)
	wrongSource := map[string]any{"Revision": edited["Revision"], "SourceRevision": "old-source", "StartTicks": start, "Provenance": "Import"}
	expectAPIError(t, f.request(t, http.MethodPut, path, wrongSource, csrf, f.cookie), http.StatusConflict, "credits_revision_conflict", false)
	if marker := creditsHTTPMarker(t, f.embyDetail(t, f.itemID)); marker == nil || *marker != start {
		t.Fatalf("item credits marker: %v", marker)
	}
	if f.playbackCounts(t) != counts {
		t.Fatal("credits administration or item projection changed playback state")
	}
	response = f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/PlaybackInfo", nil, viewer)
	expectStatus(t, response, http.StatusOK)
	sources, ok := jsonObject(t, response)["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("playback must describe one source")
	}
	if marker := creditsHTTPMarker(t, sources[0].(map[string]any)); marker == nil || *marker != start {
		t.Fatalf("playback credits marker: %v", marker)
	}
	// PlaybackInfo owns its prepared session. Subsequent administrator actions
	// must preserve it without adding reports, encoders or user history.
	counts = f.playbackCounts(t)
	resetBody := map[string]any{"Revision": edited["Revision"], "SourceRevision": edited["SourceRevision"]}
	expectStatus(t, f.request(t, http.MethodDelete, path, resetBody, nil, f.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodDelete, path, resetBody, csrf, f.cookie)
	expectStatus(t, response, http.StatusOK)
	reset := jsonObject(t, response)
	if reset["Revision"] != "2" || reset["Effective"] != nil || reset["Override"] != nil || reset["OverrideStale"] != false {
		t.Fatalf("reset retained manual credits: %#v", reset)
	}
	expectAPIError(t, f.request(t, http.MethodDelete, path, resetBody, csrf, f.cookie), http.StatusConflict, "credits_revision_conflict", false)
	if marker := creditsHTTPMarker(t, f.embyDetail(t, f.itemID)); marker != nil {
		t.Fatal("reset credits remained in item chapters")
	}
	if f.playbackCounts(t) != counts {
		t.Fatal("credits reset changed playback state")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, f.viewerID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+f.itemID, nil, viewer), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/PlaybackInfo", nil, viewer), http.StatusNotFound)
}
