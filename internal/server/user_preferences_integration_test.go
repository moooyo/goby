//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func updateConfigurationHTTP(t *testing.T, p *playbackHTTPFixture, patch map[string]any) {
	t.Helper()
	response := p.s.f.request(t, http.MethodPost, "/emby/Users/"+p.s.viewerID+"/Configuration", patch, p.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() != 0 {
		t.Fatal("compatibility configuration writes must return an empty body")
	}
}

func TestHTTPUserPreferencesPersistWithIndependentCASAndCurrentAuthority(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/admin/v1/users/" + accounts.viewer.userID + "/preferences"
	snapshot := f.request(t, http.MethodGet, path, nil, nil, accounts.cookie)
	expectStatus(t, snapshot, http.StatusOK)
	current := jsonObject(t, snapshot)
	if current["Revision"] != "1" || current["UserId"] != accounts.viewer.userID {
		t.Fatal("native preference scope or decimal-string revision is incorrect")
	}
	var managementBefore int64
	if err := f.pool.QueryRow(f.ctx, "SELECT management_revision FROM users WHERE id=$1", accounts.viewer.userID).Scan(&managementBefore); err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	patch := map[string]any{"Revision": "1", "Configuration": map[string]any{"AudioLanguagePreference": "eng", "SubtitleMode": "Always", "ResumeRewindSeconds": 10,
		"IntroSkipMode": "AutoSkip", "EnableNextEpisodeAutoPlay": false, "DisplayMissingEpisodes": true, "HidePlayedInSuggestions": true}}
	expectStatus(t, f.request(t, http.MethodPut, path, patch, nil, accounts.cookie), http.StatusForbidden)
	written := f.request(t, http.MethodPut, path, patch, headers, accounts.cookie)
	expectStatus(t, written, http.StatusOK)
	if jsonObject(t, written)["Revision"] != "2" {
		t.Fatal("native preference revision did not advance exactly once")
	}
	expectStatus(t, f.request(t, http.MethodPut, path, patch, headers, accounts.cookie), http.StatusConflict)
	patch["Revision"] = float64(2)
	expectStatus(t, f.request(t, http.MethodPut, path, patch, headers, accounts.cookie), http.StatusBadRequest)
	compat := "/Users/" + accounts.viewer.userID + "/configuration"
	read := f.request(t, http.MethodGet, compat, nil, accounts.second.headers)
	expectStatus(t, read, http.StatusOK)
	if got := jsonObject(t, read); got["AudioLanguagePreference"] != "eng" || got["SubtitleMode"] != "Always" || got["ResumeRewindSeconds"] != float64(10) ||
		got["IntroSkipMode"] != "AutoSkip" || got["EnableNextEpisodeAutoPlay"] != false ||
		got["DisplayMissingEpisodes"] != true || got["HidePlayedInSuggestions"] != true {
		t.Fatal("another authenticated device did not read the persisted account configuration")
	}
	other := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.other.userID+"/Configuration", nil, accounts.other.headers))
	if other["IntroSkipMode"] != "None" || other["EnableNextEpisodeAutoPlay"] != true ||
		other["DisplayMissingEpisodes"] != false || other["HidePlayedInSuggestions"] != false {
		t.Fatal("another user inherited playback or discovery preferences")
	}
	response := f.request(t, http.MethodPost, compat, map[string]any{"SubtitleLanguagePreference": "fra"}, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() != 0 {
		t.Fatal("compatibility preference write body is not empty")
	}
	user := f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID, nil, accounts.viewer.headers)
	expectStatus(t, user, http.StatusOK)
	configuration := objectValue(t, jsonObject(t, user), "Configuration")
	if configuration["AudioLanguagePreference"] != "eng" || configuration["SubtitleLanguagePreference"] != "fra" {
		t.Fatal("ordinary UserDto did not consume the persisted configuration")
	}
	for _, body := range []map[string]any{{"UnknownPreference": true}, {"SubtitleMode": "Unsupported"}, {"ResumeRewindSeconds": -1}, {"HidePlayedInSuggestions": "true"}, {"DisplayMissingEpisodes": nil}} {
		expectStatus(t, f.request(t, http.MethodPost, compat, body, accounts.viewer.headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodPost, compat, map[string]any{"AudioLanguagePreference": "deu"}, accounts.other.headers), http.StatusForbidden)
	var managementAfter, revision, settingsCount int64
	if err := f.pool.QueryRow(f.ctx, `SELECT management_revision,configuration_revision,(SELECT count(*) FROM user_settings) FROM users WHERE id=$1`, accounts.viewer.userID).
		Scan(&managementAfter, &revision, &settingsCount); err != nil {
		t.Fatal(err)
	}
	if managementAfter != managementBefore || revision != 3 || settingsCount != 0 {
		t.Fatal("preference writes mutated management or UserSettings state, or invalid writes advanced CAS")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, accounts.viewer.userID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, compat, nil, accounts.viewer.headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, compat, map[string]any{"SubtitleMode": "None"}, accounts.viewer.headers), http.StatusForbidden)
	// Native administration remains independent of the user's self-service gate.
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil, accounts.cookie), http.StatusOK)
}

func TestHTTPDisplayPreferencesApplyScopedDefaultsWithoutBlockingBrowsing(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	path := "/emby/DisplayPreferences/" + p.s.video.libraryID + "?UserId=" + p.s.viewerID + "&Client=web"
	response := p.s.f.request(t, http.MethodPost, path, map[string]any{"Revision": "0", "SortBy": "SortName", "SortOrder": "Descending", "CustomPrefs": map[string]string{"layout": "poster"}}, p.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() != 0 {
		t.Fatal("display preference write body is not empty")
	}
	read := p.s.f.request(t, http.MethodGet, path, nil, p.headers)
	expectStatus(t, read, http.StatusOK)
	if got := jsonObject(t, read); got["Revision"] != "1" || got["SortOrder"] != "Descending" || objectValue(t, got, "CustomPrefs")["layout"] != "poster" {
		t.Fatal("display scope did not retain its persisted fields")
	}
	list := "/emby/Users/" + p.s.viewerID + "/Items?ParentId=" + p.s.video.libraryID + "&Recursive=true&IsFolder=false"
	assertOrder := func(path string, want ...string) {
		t.Helper()
		items, total := responseItems(t, p.s.f.request(t, http.MethodGet, path, nil, p.headers))
		var ids []string
		for _, item := range items {
			ids = append(ids, stringValue(t, item, "Id"))
		}
		if total != len(want) || !reflect.DeepEqual(ids, want) {
			t.Fatalf("scoped item order = %v, want %v", ids, want)
		}
	}
	assertOrder(list+"&Client=web", p.secondItemID, p.s.video.id)
	assertOrder(list+"&Client=tv", p.s.video.id, p.secondItemID)
	assertOrder(list+"&Client=web&SortBy=SortName&SortOrder=Ascending", p.s.video.id, p.secondItemID)
	assertOrder(list+"&Client=web&DisplayPreferencesId="+p.s.video.libraryID, p.secondItemID, p.s.video.id)
	expectStatus(t, p.s.f.request(t, http.MethodPost, path, map[string]any{"SortBy": "Unsupported"}, p.headers), http.StatusBadRequest)
	expectStatus(t, p.s.f.request(t, http.MethodGet, path+"&client=tv", nil, p.headers), http.StatusBadRequest)
	expectStatus(t, p.s.f.request(t, http.MethodGet, list+"&Client=web&client=tv", nil, p.headers), http.StatusBadRequest)
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, p.s.viewerID); err != nil {
		t.Fatal(err)
	}
	// Denied implicit preference access skips defaults; it must not deny a
	// separately authorized catalog read or disclose saved settings.
	assertOrder(list+"&Client=web", p.s.video.id, p.secondItemID)
	expectStatus(t, p.s.f.request(t, http.MethodGet, path, nil, p.headers), http.StatusForbidden)
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, p.authSessionID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, p.s.f.request(t, http.MethodGet, list+"&Client=web", nil, p.headers), http.StatusUnauthorized)
}

func TestHTTPConfigurationChangesViewsAndLatestWithoutGrantingLibraryAccess(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	other := p.s.addItem(t, "Another.Feature.mp4", "mixed", "Videos", "mp4", "video/mp4", p.s.video.data)
	hidden := p.s.addItem(t, "Hidden.S01E01.mp4", "tvshows", "Videos", "mp4", "video/mp4", p.s.video.data)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID, other.libraryID})
	updateConfigurationHTTP(t, p, map[string]any{"OrderedViews": []string{hidden.libraryID, p.s.video.libraryID, other.libraryID}})
	views := "/emby/Users/" + p.s.viewerID + "/Views"
	items, total := responseItems(t, p.s.f.request(t, http.MethodGet, views, nil, p.headers))
	if total != 2 || items[0]["Id"] != p.s.video.libraryID || items[1]["Id"] != other.libraryID {
		t.Fatal("ordered views were not filtered through the current library ACL")
	}
	updateConfigurationHTTP(t, p, map[string]any{"MyMediaExcludes": []string{p.s.video.libraryID}})
	items, total = responseItems(t, p.s.f.request(t, http.MethodGet, views, nil, p.headers))
	if total != 1 || items[0]["Id"] != other.libraryID {
		t.Fatal("MyMediaExcludes did not change the visible view list")
	}
	updateConfigurationHTTP(t, p, map[string]any{"MyMediaExcludes": []string{}, "LatestItemsExcludes": []string{other.libraryID}})
	state := "/emby/Users/" + p.s.viewerID + "/Items/" + p.s.video.id + "/UserData"
	expectStatus(t, p.s.f.request(t, http.MethodPost, state, map[string]any{"Played": true}, p.headers), http.StatusOK)
	latest := "/emby/Users/" + p.s.viewerID + "/Items/Latest?GroupItems=false"
	latestIDs := func(path string) []string {
		t.Helper()
		response := p.s.f.request(t, http.MethodGet, path, nil, p.headers)
		expectStatus(t, response, http.StatusOK)
		var entries []map[string]any
		if json.Unmarshal(response.Body.Bytes(), &entries) != nil {
			t.Fatal("latest items response is not an array")
		}
		ids := make([]string, 0, len(entries))
		for _, item := range entries {
			ids = append(ids, stringValue(t, item, "Id"))
		}
		return ids
	}
	if got := latestIDs(latest); !reflect.DeepEqual(got, []string{p.secondItemID}) {
		t.Fatalf("latest preference defaults = %v", got)
	}
	if got := latestIDs(latest + "&ParentId=" + url.QueryEscape(other.libraryID)); !reflect.DeepEqual(got, []string{other.id}) {
		t.Fatal("an explicit latest parent did not override global exclusions")
	}
	if got := latestIDs(latest + "&IsPlayed=true"); !reflect.DeepEqual(got, []string{p.s.video.id}) {
		t.Fatal("an explicit latest played filter did not override the preference")
	}
	updateConfigurationHTTP(t, p, map[string]any{"HidePlayedInLatest": false})
	if got := latestIDs(latest); len(got) != 2 {
		t.Fatal("latest played preference was stored without changing query results")
	}
}
