package server

import (
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestHTTPExtraTrailerNativeNameControlsRemainIndependentAcrossAllReads(t *testing.T) {
	f := newExtraHTTPFixture(t)
	cookie, csrf := f.adminLogin(t)
	const trailerID = "ex-trailer"
	const filename = "Delta Local Trailer"
	const initialAutomatic = "Theme Movie - Trailer"
	base := "/emby/Users/" + f.viewerID + "/Items"
	edit := func(id string, overrides map[string]any, locks []string) {
		t.Helper()
		path := "/admin/v1/items/" + id + "/metadata"
		response := f.request(t, http.MethodGet, path, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		revision := stringValue(t, jsonObject(t, response), "Revision")
		response = f.request(t, http.MethodPut, path, map[string]any{
			"Revision": revision, "Overrides": overrides, "LockedFields": locks,
		}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
		expectStatus(t, response, http.StatusOK)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,play_count,is_favorite)
		VALUES($1,$3,3,true),($2,$3,7,false)`, f.viewerID, f.otherID, trailerID); err != nil {
		t.Fatal(err)
	}
	userState := func() string {
		t.Helper()
		var state string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(data) ORDER BY user_id,item_id)::text
			FROM user_item_data data`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	originalUserState := userState()
	assertProjection := func(name, sortName string, nameControlled, sortControlled bool) {
		t.Helper()
		listed := extraHTTPItems(t, f.request(t, http.MethodGet, base+"/th-movie/LocalTrailers?Fields=SortName,MediaSources", nil, f.headers))
		if len(listed) != 1 {
			t.Fatal("native metadata edit changed the local trailer membership")
		}
		directResponse := f.request(t, http.MethodGet, base+"/"+trailerID, nil, f.headers)
		expectStatus(t, directResponse, http.StatusOK)
		direct := jsonObject(t, directResponse)
		selected, count := responseItems(t, f.request(t, http.MethodGet, base+"?Ids="+trailerID+
			"&IncludeItemTypes=Trailer&Fields=SortName,MediaSources", nil, f.headers))
		if count != 1 || len(selected) != 1 {
			t.Fatal("native metadata edit changed explicit Trailer type selection")
		}
		for _, item := range []map[string]any{listed[0], direct, selected[0]} {
			if item["Id"] != trailerID || item["Name"] != name || item["SortName"] != sortName ||
				item["Type"] != "Trailer" || item["ExtraType"] != "Trailer" {
				t.Fatalf("trailer projection hid independent native controls: name=%v sort=%v", item["Name"], item["SortName"])
			}
			sources, ok := item["MediaSources"].([]any)
			if !ok || len(sources) != 1 {
				t.Fatal("native name control removed the resource source")
			}
			source, ok := sources[0].(map[string]any)
			if !ok || source["Name"] != filename || source["Id"] != media.SourceID(trailerID) {
				t.Fatal("a display override changed the source filename or stable ID")
			}
			data, ok := item["UserData"].(map[string]any)
			if !ok || data["PlayCount"] != float64(3) || data["IsFavorite"] != true {
				t.Fatal("native metadata edit changed the viewer's trailer state")
			}
		}
		indexed, err := f.app.library.GetItemFor(f.ctx, library.Subject{UserID: f.viewerID}, trailerID)
		if err != nil || indexed.ExtraNameControlled != nameControlled || indexed.ExtraSortNameControlled != sortControlled {
			t.Fatalf("control provenance was not read in the authorized item snapshot: %v", err)
		}
		if userState() != originalUserState {
			t.Fatal("native metadata edits or projections changed independent user histories")
		}
	}

	assertProjection(initialAutomatic, initialAutomatic, false, false)
	edit(trailerID, map[string]any{"Name": "Edited title"}, []string{})
	assertProjection("Edited title", initialAutomatic, true, false)
	edit(trailerID, map[string]any{"SortName": "Edited sort"}, []string{})
	assertProjection(initialAutomatic, "Edited sort", false, true)
	edit(trailerID, map[string]any{"Name": "Pinned title", "SortName": "Pinned sort"}, []string{"Name", "SortName"})
	assertProjection("Pinned title", "Pinned sort", true, true)
	edit(trailerID, map[string]any{}, []string{"Name", "SortName"})
	assertProjection("Pinned title", "Pinned sort", true, true)
	edit(trailerID, map[string]any{}, []string{"Name"})
	assertProjection("Pinned title", initialAutomatic, true, false)
	edit(trailerID, map[string]any{"SortName": "Independent sort lock"}, []string{"SortName"})
	assertProjection(initialAutomatic, "Independent sort lock", false, true)
	edit(trailerID, map[string]any{}, []string{"SortName"})
	assertProjection(initialAutomatic, "Independent sort lock", false, true)
	edit(trailerID, map[string]any{}, []string{})
	assertProjection(initialAutomatic, initialAutomatic, false, false)

	// Equal values are still explicit controls. Neither a filename comparison
	// nor a comparison with today's owner-derived text may erase that intent.
	edit(trailerID, map[string]any{"Name": filename, "SortName": filename}, []string{})
	assertProjection(filename, filename, true, true)
	edit(trailerID, map[string]any{"Name": initialAutomatic, "SortName": initialAutomatic}, []string{})
	edit("th-movie", map[string]any{"Name": "Renamed parent"}, []string{})
	assertProjection(initialAutomatic, initialAutomatic, true, true)
	edit(trailerID, map[string]any{}, []string{})
	assertProjection("Renamed parent - Trailer", "Renamed parent - Trailer", false, false)
	edit("th-movie", map[string]any{"Name": "Renamed again"}, []string{})
	assertProjection("Renamed again - Trailer", "Renamed again - Trailer", false, false)
}
