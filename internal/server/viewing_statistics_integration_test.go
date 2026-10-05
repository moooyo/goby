//go:build linux

package server

import (
	"net/http"
	"reflect"
	"testing"
)

func assertHTTPViewingStatistics(t *testing.T, f *serverFixture, userID string, headers http.Header, hours int64, ticks string) {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Users/"+userID+"/ViewingStatistics", nil, headers)
	expectStatus(t, response, http.StatusOK)
	want := map[string]any{"EstimatedContentHours": float64(hours), "EstimatedContentTicks": ticks, "IsEstimate": true}
	if got := jsonObject(t, response); !reflect.DeepEqual(got, want) {
		t.Fatalf("viewing statistics = %#v, want %#v", got, want)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("personal viewing statistics are cacheable")
	}
}

func TestHTTPViewingStatisticsEstimatesVisibleContentOnce(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Statistics Viewer", "statistics-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('statistics-visible','Visible Statistics','mixed'),('statistics-hidden','Hidden Statistics','movies');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('statistics-visible','statistics-visible',NULL,'Visible Statistics','visible','CollectionFolder',true),
		('statistics-hidden','statistics-hidden',NULL,'Hidden Statistics','hidden','CollectionFolder',true),
		('statistics-played','statistics-visible','statistics-visible','Played Movie','played','Movie',false),
		('statistics-partial','statistics-visible','statistics-visible','Partial Episode','partial','Episode',false),
		('statistics-capped','statistics-visible','statistics-visible','Capped Episode','capped','Episode',false),
		('statistics-unplayed','statistics-visible','statistics-visible','Unplayed Movie','unplayed','Movie',false),
		('statistics-unknown','statistics-visible','statistics-visible','Unknown Runtime','unknown','Movie',false),
		('statistics-zero','statistics-visible','statistics-visible','Zero Runtime','zero','Movie',false),
		('statistics-negative','statistics-visible','statistics-visible','Negative Runtime','negative','Movie',false),
		('statistics-string','statistics-visible','statistics-visible','String Runtime','string','Movie',false),
		('statistics-audio','statistics-visible','statistics-visible','Audio Control','audio','Audio',false),
		('statistics-folder','statistics-visible','statistics-visible','Folder Control','folder','Movie',true),
		('statistics-theme','statistics-visible','statistics-visible','Theme Control','theme','Movie',false),
		('statistics-extra','statistics-visible','statistics-visible','Extra Control','extra','Movie',false),
		('statistics-excluded','statistics-visible','statistics-visible','Excluded Folder','excluded','Folder',true),
		('statistics-excluded-movie','statistics-visible','statistics-excluded','Excluded Movie','excluded movie','Movie',false),
		('statistics-hidden-movie','statistics-hidden','statistics-hidden','Hidden Movie','hidden movie','Movie',false);
		UPDATE items SET media='{"DurationTicks":72000000000}'::jsonb WHERE NOT is_folder;
		UPDATE items SET media='{"DurationTicks":36000000000}'::jsonb WHERE id='statistics-partial';
		UPDATE items SET media='{"DurationTicks":54000000000}'::jsonb WHERE id='statistics-capped';
		UPDATE items SET media=NULL WHERE id='statistics-unknown';
		UPDATE items SET media='{"DurationTicks":0}'::jsonb WHERE id='statistics-zero';
		UPDATE items SET media='{"DurationTicks":-100}'::jsonb WHERE id='statistics-negative';
		UPDATE items SET media='{"DurationTicks":"72000000000"}'::jsonb WHERE id='statistics-string';
		UPDATE items SET media='{"DurationTicks":72000000000}'::jsonb WHERE id='statistics-folder';
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
		VALUES('statistics-theme','statistics-played','video',true);
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES('statistics-extra','statistics-played','trailer',true)`); err != nil {
		t.Fatalf("seed viewing statistics catalog: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `WITH actor AS (UPDATE users SET policy=
		'{"EnableAllFolders":false,"EnabledFolders":["statistics-visible"],"ExcludedSubFolders":["statistics-excluded"]}'::jsonb
		WHERE id=$1 RETURNING id)
		INSERT INTO user_item_data(user_id,item_id,played,play_count,playback_position_ticks)
		SELECT actor.id,i.id,i.id NOT IN ('statistics-partial','statistics-capped','statistics-unplayed'),
			CASE WHEN i.id IN ('statistics-partial','statistics-capped') THEN 0
				WHEN i.id='statistics-unplayed' THEN 3 ELSE 8 END,
			CASE i.id WHEN 'statistics-partial' THEN 18000000000 WHEN 'statistics-capped' THEN 90000000000
				WHEN 'statistics-unplayed' THEN 0 ELSE 9000000000 END
		FROM actor CROSS JOIN items i WHERE i.id NOT IN ('statistics-visible','statistics-hidden')`, viewer.ID); err != nil {
		t.Fatalf("seed viewing statistics state: %v", err)
	}
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "statistics-viewer-password"), "AccessToken")}}
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	path := "/emby/Users/" + viewer.ID + "/ViewingStatistics"
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/ViewingStatistics", nil, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, path+"?UserId="+adminID, nil, adminHeaders), http.StatusBadRequest)
	// Login-based catalog reads deliberately treat an absent target as forbidden;
	// application-key projections retain their separate missing-target contract.
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/missing-statistics-user/ViewingStatistics", nil, adminHeaders), http.StatusForbidden, "access_denied", true)

	// A finished two-hour movie contributes once, plus thirty minutes of an
	// unfinished episode and ninety minutes from a position beyond its runtime.
	assertHTTPViewingStatistics(t, f, viewer.ID, headers, 4, "144000000000")
	assertHTTPViewingStatistics(t, f, viewer.ID, adminHeaders, 4, "144000000000")
	assertHTTPViewingStatistics(t, f, adminID, adminHeaders, 0, "0")
	if _, err := f.pool.Exec(f.ctx, `UPDATE user_item_data SET playback_position_ticks=0
		WHERE user_id=$1 AND item_id='statistics-partial'`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	// The handoff rounds the displayed number of hours, while the exact tick
	// string remains available and never rounds or multiplies by play count.
	assertHTTPViewingStatistics(t, f, viewer.ID, headers, 4, "126000000000")
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	assertHTTPViewingStatistics(t, f, viewer.ID, headers, 0, "0")
	assertHTTPViewingStatistics(t, f, viewer.ID, adminHeaders, 0, "0")
	var played bool
	var count int
	var position int64
	if err := f.pool.QueryRow(f.ctx, `SELECT played,play_count,playback_position_ticks FROM user_item_data
		WHERE user_id=$1 AND item_id='statistics-played'`, viewer.ID).Scan(&played, &count, &position); err != nil {
		t.Fatal(err)
	}
	if !played || count != 8 || position != 9000000000 {
		t.Fatal("reading statistics changed persisted playback state")
	}
}

func TestHTTPViewingStatisticsApplicationKeyExplicitTargetRespectsCurrentACL(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	// This target is disabled, but application credentials may still project its
	// persisted state. Its two completed two-minute items each contribute once.
	assertHTTPViewingStatistics(t, f, a.targetID, a.headers, 0, "2400000000")
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/ViewingStatistics", nil, a.viewerHeaders), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/missing-statistics-user/ViewingStatistics", nil, a.headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=jsonb_build_object('EnableAllFolders',false,'EnabledFolders',jsonb_build_array($2::text))
		WHERE id=$1`, a.targetID, a.visibleID); err != nil {
		t.Fatal(err)
	}
	assertHTTPViewingStatistics(t, f, a.targetID, a.headers, 0, "0")
}
