//go:build linux

package server

import (
	"net/http"
	"testing"
)

type suggestionsHTTPFixture struct {
	f                     *serverFixture
	viewerID, otherID     string
	headers, otherHeaders http.Header
}

func seedHTTPSuggestionsCatalog(t *testing.T, f *serverFixture) {
	t.Helper()
	// Accepted catalog facts are sufficient for discovery. These tests do not
	// open media files, invoke a probe, or create a playback session.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('suggest-visible','Visible Suggestions','mixed'),('suggest-hidden','Hidden Suggestions','movies');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,created_at) VALUES
		('suggest-visible','suggest-visible',NULL,'Visible Suggestions','visible suggestions','CollectionFolder',true,'2025-01-01T00:00:00Z'),
		('suggest-favorite-played','suggest-visible','suggest-visible','A Favorite Played','a favorite played','Movie',false,'2025-01-01T00:00:00Z'),
		('suggest-favorite','suggest-visible','suggest-visible','B Favorite','b favorite','Movie',false,'2025-01-02T00:00:00Z'),
		('suggest-liked','suggest-visible','suggest-visible','C Liked','c liked','Audio',false,'2025-01-03T00:00:00Z'),
		('suggest-favorite-disliked','suggest-visible','suggest-visible','D Favorite Disliked','d favorite disliked','MusicVideo',false,'2025-01-04T00:00:00Z'),
		('suggest-disliked','suggest-visible','suggest-visible','E Disliked','e disliked','Video',false,'2025-01-05T00:00:00Z'),
		('suggest-played','suggest-visible','suggest-visible','F Played','f played','Episode',false,'2025-01-06T00:00:00Z'),
		('suggest-unplayed','suggest-visible','suggest-visible','G Unplayed','g unplayed','Movie',false,'2025-01-07T00:00:00Z'),
		('suggest-tie-a','suggest-visible','suggest-visible','H Tie A','h tie a','Video',false,'2025-01-08T00:00:00Z'),
		('suggest-tie-b','suggest-visible','suggest-visible','I Tie B','i tie b','Video',false,'2025-01-08T00:00:00Z'),
		('suggest-hidden','suggest-hidden',NULL,'Hidden Favorite','hidden favorite','Movie',false,'2025-02-01T00:00:00Z'),
		('suggest-folder','suggest-visible','suggest-visible','Folder Control','folder control','Folder',true,'2025-02-02T00:00:00Z'),
		('suggest-unprobed','suggest-visible','suggest-visible','Unprobed Movie','unprobed movie','Movie',false,'2025-02-03T00:00:00Z'),
		('suggest-unsupported','suggest-visible','suggest-visible','Photo Control','photo control','Photo',false,'2025-02-04T00:00:00Z'),
		('suggest-series','suggest-visible','suggest-visible','Roster Series','roster series','Series',true,'2025-02-05T00:00:00Z');
		UPDATE items SET media='{"Container":"mp4","DurationTicks":6000000000,"Streams":[{"Index":0,"CodecType":"video","Codec":"h264","Width":320,"Height":180}]}'::jsonb
		WHERE id NOT IN ('suggest-visible','suggest-unprobed','suggest-liked','suggest-series');
		UPDATE items SET media='{"Container":"mp3","DurationTicks":6000000000,"Streams":[{"Index":0,"CodecType":"audio","Codec":"mp3","Channels":2,"SampleRate":44100}]}'::jsonb
		WHERE id='suggest-liked';
		INSERT INTO series_episode_rosters(series_id,revision,state,source_key,source_label,source_revision,parser_version,payload_sha256,last_edited_by)
		VALUES ('suggest-series',1,'active','suggestions-fixture','Suggestions Fixture','1',1,repeat('0',64),'suggestions-fixture');
		INSERT INTO episode_roster_imports(series_id,revision,action,source_key,source_label,source_revision,parser_version,payload,payload_sha256,actor_id)
		VALUES ('suggest-series',1,'replace','suggestions-fixture','Suggestions Fixture','1',1,'{}'::bytea,repeat('0',64),'suggestions-fixture');
		INSERT INTO expected_episodes(id,series_id,source_key,entry_key,season_number,episode_number,name,premiere_date,active,import_revision)
		VALUES ('missing-00000000000000000000000000000001','suggest-series','suggestions-fixture','missing-episode',1,2,'Missing Suggestion','2025-01-01',true,1)`); err != nil {
		t.Fatalf("seed suggestions catalog: %v", err)
	}
}

func seedHTTPSuggestionsState(t *testing.T, f *serverFixture, userID string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,played,play_count,is_favorite,likes) VALUES
		($1,'suggest-favorite-played',true,3,true,true),
		($1,'suggest-favorite',false,0,true,NULL),
		($1,'suggest-liked',false,0,false,true),
		($1,'suggest-favorite-disliked',false,0,true,false),
		($1,'suggest-disliked',false,0,false,false),
		($1,'suggest-played',true,1,false,NULL),
		($1,'suggest-hidden',false,0,true,true)`, userID); err != nil {
		t.Fatalf("seed personal suggestions state: %v", err)
	}
}

func restrictHTTPSuggestionsUser(t *testing.T, f *serverFixture, userID string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["suggest-visible"]}'::jsonb WHERE id=$1`, userID); err != nil {
		t.Fatalf("restrict suggestions library access: %v", err)
	}
}

func newSuggestionsHTTPFixture(t *testing.T) *suggestionsHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	seedHTTPSuggestionsCatalog(t, f)
	viewer, err := f.users.CreateUser(f.ctx, "Suggestions Viewer", "suggestions-viewer-password", false)
	if err != nil {
		t.Fatalf("create suggestions viewer: %v", err)
	}
	other, err := f.users.CreateUser(f.ctx, "Other Suggestions Viewer", "suggestions-other-password", false)
	if err != nil {
		t.Fatalf("create independent suggestions viewer: %v", err)
	}
	restrictHTTPSuggestionsUser(t, f, viewer.ID)
	restrictHTTPSuggestionsUser(t, f, other.ID)
	seedHTTPSuggestionsState(t, f, viewer.ID)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,played,is_favorite,likes) VALUES
		($1,'suggest-unplayed',true,false,false),($1,'suggest-played',false,true,true)`, other.ID); err != nil {
		t.Fatalf("seed independent suggestions state: %v", err)
	}
	return &suggestionsHTTPFixture{
		f: f, viewerID: viewer.ID, otherID: other.ID,
		headers:      http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "suggestions-viewer-password"), "AccessToken")}},
		otherHeaders: http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, other.Name, "suggestions-other-password"), "AccessToken")}},
	}
}

func readHTTPSuggestions(t *testing.T, f *serverFixture, path string, headers http.Header, total int, expected ...string) []map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, path, nil, headers)
	items, count := responseItems(t, response)
	if count != total || len(items) != len(expected) {
		t.Fatalf("suggestions total/page = %d/%d, want %d/%d; path = %s", count, len(items), total, len(expected), path)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personal suggestions response is cacheable")
	}
	for index, id := range expected {
		if items[index]["Id"] != id {
			t.Fatalf("suggestion %d = %v, want %s; path = %s", index, items[index]["Id"], id, path)
		}
		if items[index]["IsFolder"] != false || items[index]["RunTimeTicks"] != float64(6000000000) {
			t.Fatalf("suggestions returned a folder or an entry without accepted media: %#v", items[index])
		}
	}
	return items
}

func setHTTPSuggestionsPreference(t *testing.T, s *suggestionsHTTPFixture, hidePlayed bool) {
	t.Helper()
	response := s.f.request(t, http.MethodPost, "/emby/Users/"+s.viewerID+"/Configuration",
		map[string]any{"HidePlayedInSuggestions": hidePlayed}, s.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() != 0 {
		t.Fatal("suggestions preference write must return an empty compatibility response")
	}
}

func TestHTTPSuggestionsRanksPlayableItemsAndPreservesCountsAcrossAliases(t *testing.T) {
	s := newSuggestionsHTTPFixture(t)
	missing, count := responseItems(t, s.f.request(t, http.MethodGet,
		"/emby/Users/"+s.viewerID+"/Items?Recursive=true&IsMissing=true", nil, s.headers))
	if count != 1 || len(missing) != 1 || missing[0]["Id"] != "missing-00000000000000000000000000000001" || missing[0]["IsMissing"] != true {
		t.Fatal("the suggestions fixture does not expose its virtual episode through ordinary discovery")
	}
	expectStatus(t, s.f.request(t, http.MethodPost, "/emby/Users/"+s.viewerID+"/Configuration",
		map[string]any{"DisplayMissingEpisodes": true}, s.headers), http.StatusOK)
	order := []string{
		"suggest-favorite-disliked", "suggest-liked", "suggest-favorite", "suggest-favorite-played",
		"suggest-tie-a", "suggest-tie-b", "suggest-unplayed", "suggest-disliked", "suggest-played",
	}
	for _, path := range []string{
		"/emby/Users/" + s.viewerID + "/Suggestions",
		"/Users/" + s.viewerID + "/Suggestions",
		"/emby/users/" + s.viewerID + "/suggestions",
		"/users/" + s.viewerID + "/suggestions",
	} {
		readHTTPSuggestions(t, s.f, path, s.headers, len(order), order...)
	}
	path := "/emby/Users/" + s.viewerID + "/Suggestions"
	readHTTPSuggestions(t, s.f, path+"?Limit=2&StartIndex=2", s.headers, 9, "suggest-favorite", "suggest-favorite-played")
	readHTTPSuggestions(t, s.f, path+"?Limit=1&StartIndex=4", s.headers, 9, "suggest-tie-a")
	readHTTPSuggestions(t, s.f, path+"?Limit=1&StartIndex=5", s.headers, 9, "suggest-tie-b")
	readHTTPSuggestions(t, s.f, path+"?Limit=0", s.headers, 9)
	readHTTPSuggestions(t, s.f, path+"?StartIndex=9&Limit=1", s.headers, 9)
	readHTTPSuggestions(t, s.f, path+"?SortBy=SortName&SortOrder=Descending&Limit=2&StartIndex=1", s.headers, 9,
		"suggest-tie-a", "suggest-unplayed")
	readHTTPSuggestions(t, s.f, path+"?IncludeItemTypes=Audio", s.headers, 1, "suggest-liked")
	readHTTPSuggestions(t, s.f, path+"?IsFolder=true", s.headers, 0)
	readHTTPSuggestions(t, s.f, path+"?IsMissing=true", s.headers, 0)
	readHTTPSuggestions(t, s.f, path+"?Ids=missing-00000000000000000000000000000001", s.headers, 0)
	readHTTPSuggestions(t, s.f, path+"?Ids=suggest-folder,suggest-unprobed,suggest-unsupported,suggest-hidden", s.headers, 0)
	items := readHTTPSuggestions(t, s.f, path+"?EnableUserData=false&EnableImages=false&Limit=1", s.headers, 9, order[0])
	if _, present := items[0]["UserData"]; present {
		t.Fatal("suggestions ignored the user-data projection switch")
	}
	if _, present := items[0]["ImageTags"]; present {
		t.Fatal("suggestions ignored the image projection switch")
	}
}

func TestHTTPSuggestionsPreferenceDefaultsAndExplicitFiltersIntersect(t *testing.T) {
	s := newSuggestionsHTTPFixture(t)
	path := "/emby/Users/" + s.viewerID + "/Suggestions"
	setHTTPSuggestionsPreference(t, s, true)
	unplayed := []string{
		"suggest-favorite-disliked", "suggest-liked", "suggest-favorite",
		"suggest-tie-a", "suggest-tie-b", "suggest-unplayed", "suggest-disliked",
	}
	readHTTPSuggestions(t, s.f, path, s.headers, len(unplayed), unplayed...)
	readHTTPSuggestions(t, s.f, path+"?Limit=0", s.headers, len(unplayed))
	readHTTPSuggestions(t, s.f, path+"?IsPlayed=false", s.headers, len(unplayed), unplayed...)
	readHTTPSuggestions(t, s.f, path+"?Filters=IsUnplayed", s.headers, len(unplayed), unplayed...)
	readHTTPSuggestions(t, s.f, path+"?IsPlayed=true", s.headers, 2, "suggest-favorite-played", "suggest-played")
	readHTTPSuggestions(t, s.f, path+"?Filters=IsPlayed", s.headers, 2, "suggest-favorite-played", "suggest-played")
	readHTTPSuggestions(t, s.f, path+"?IsPlayed=true&Filters=IsPlayed&Limit=0", s.headers, 2)
	for _, test := range []struct {
		name, query string
		want        []string
	}{
		{"favorites", "Filters=IsFavorite", []string{"suggest-favorite-disliked", "suggest-favorite"}},
		{"likes", "Filters=Likes", []string{"suggest-liked"}},
		{"dislikes", "Filters=Dislikes", []string{"suggest-favorite-disliked", "suggest-disliked"}},
		{"played_liked_favorite", "Filters=IsFavorite,Likes,IsPlayed", []string{"suggest-favorite-played"}},
		{"disliked_favorite", "Filters=IsFavorite,Dislikes", []string{"suggest-favorite-disliked"}},
		{"disliked_favorite_or_liked", "Filters=IsFavoriteOrLikes,Dislikes", []string{"suggest-favorite-disliked"}},
		{"liked_nonfavorite", "IsFavorite=false&Filters=Likes", []string{"suggest-liked"}},
		{"disliked_nonfavorite", "IsFavorite=false&Filters=Dislikes", []string{"suggest-disliked"}},
		{"favorite_and_unplayed", "Filters=IsFavorite,IsUnplayed", []string{"suggest-favorite-disliked", "suggest-favorite"}},
		{"disjoint_combined_preference", "IsFavoriteOrLikes=false&Filters=Likes", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			readHTTPSuggestions(t, s.f, path+"?"+test.query, s.headers, len(test.want), test.want...)
			readHTTPSuggestions(t, s.f, path+"?"+test.query+"&Limit=0", s.headers, len(test.want))
		})
	}
	for _, query := range []string{
		"IsPlayed=false&Filters=IsPlayed", "Filters=IsPlayed,IsUnplayed", "Filters=Likes,Dislikes",
		"IsFavorite=false&Filters=IsFavorite", "IsPlayed=", "IsPlayed=true&IsPlayed=false", "Filters=Unknown",
	} {
		expectStatus(t, s.f.request(t, http.MethodGet, path+"?"+query, nil, s.headers), http.StatusBadRequest)
	}
	setHTTPSuggestionsPreference(t, s, false)
	readHTTPSuggestions(t, s.f, path+"?Limit=1&StartIndex=3", s.headers, 9, "suggest-favorite-played")
}

func TestHTTPSuggestionsUsesIndependentUserStateAndCurrentLibraryAuthority(t *testing.T) {
	s := newSuggestionsHTTPFixture(t)
	path := "/emby/Users/" + s.viewerID + "/Suggestions"
	otherPath := "/emby/Users/" + s.otherID + "/Suggestions"
	viewerItems := readHTTPSuggestions(t, s.f, path+"?IsPlayed=true", s.headers, 2, "suggest-favorite-played", "suggest-played")
	data := objectValue(t, viewerItems[0], "UserData")
	if data["Played"] != true || data["PlayCount"] != float64(3) || data["IsFavorite"] != true || data["Likes"] != true {
		t.Fatalf("suggestions lost the requesting user's persisted state: %#v", data)
	}
	readHTTPSuggestions(t, s.f, otherPath+"?IsPlayed=true", s.otherHeaders, 1, "suggest-unplayed")
	otherItems := readHTTPSuggestions(t, s.f, otherPath+"?Filters=IsFavorite,Likes", s.otherHeaders, 1, "suggest-played")
	data = objectValue(t, otherItems[0], "UserData")
	if data["Played"] != false || data["PlayCount"] != float64(0) || data["IsFavorite"] != true || data["Likes"] != true {
		t.Fatalf("suggestions borrowed another user's playback or preference state: %#v", data)
	}
	setHTTPSuggestionsPreference(t, s, true)
	readHTTPSuggestions(t, s.f, otherPath+"?Limit=0", s.otherHeaders, 9)
	expectStatus(t, s.f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	expectStatus(t, s.f.request(t, http.MethodGet, otherPath, nil, s.headers), http.StatusForbidden)
	expectStatus(t, s.f.request(t, http.MethodGet, path+"?UserId="+s.otherID, nil, s.headers), http.StatusBadRequest)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, s.viewerID); err != nil {
		t.Fatalf("revoke suggestions library access: %v", err)
	}
	readHTTPSuggestions(t, s.f, path, s.headers, 0)
	readHTTPSuggestions(t, s.f, path+"?Filters=IsFavoriteOrLikes&Limit=0", s.headers, 0)
	expectStatus(t, s.f.request(t, http.MethodGet, path+"?ParentId=suggest-visible", nil, s.headers), http.StatusNotFound)
	readHTTPSuggestions(t, s.f, otherPath+"?Filters=IsFavorite,Likes", s.otherHeaders, 1, "suggest-played")
}

func TestHTTPApplicationKeySuggestionsKeepExplicitStateWithoutTargetPreferences(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	f := a.serverFixture
	seedHTTPSuggestionsCatalog(t, f)
	target, err := f.users.CreateUser(f.ctx, "Suggestions Application Target", "suggestions-target-password", false)
	if err != nil {
		t.Fatalf("create suggestions application target: %v", err)
	}
	restrictHTTPSuggestionsUser(t, f, target.ID)
	seedHTTPSuggestionsState(t, f, target.ID)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET configuration='{"HidePlayedInSuggestions":true}'::jsonb WHERE id IN ($1,$2)`, target.ID, a.adminID); err != nil {
		t.Fatalf("set target and creator suggestions preferences: %v", err)
	}
	key := a.create(t, "Suggestions Integration")
	headers := http.Header{"X-Emby-Token": {key.token}}
	path := "/emby/Users/" + target.ID + "/Suggestions"
	readHTTPSuggestions(t, f, path+"?Limit=0", headers, 9)
	items := readHTTPSuggestions(t, f, path+"?Limit=1&StartIndex=3", headers, 9, "suggest-favorite-played")
	data := objectValue(t, items[0], "UserData")
	if data["Played"] != true || data["PlayCount"] != float64(3) || data["IsFavorite"] != true || data["Likes"] != true {
		t.Fatalf("application suggestions lost the explicit target's state projection: %#v", data)
	}
	readHTTPSuggestions(t, f, path+"?IsPlayed=true", headers, 2, "suggest-favorite-played", "suggest-played")
	readHTTPSuggestions(t, f, path+"?Filters=IsPlayed,Likes", headers, 1, "suggest-favorite-played")
	readHTTPSuggestions(t, f, path+"?IsPlayed=false&Limit=0", headers, 7)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/missing-suggestions-target/Suggestions?Limit=0", nil, headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, target.ID); err != nil {
		t.Fatalf("revoke application target suggestions access: %v", err)
	}
	readHTTPSuggestions(t, f, path+"?Limit=0", headers, 0)
}
