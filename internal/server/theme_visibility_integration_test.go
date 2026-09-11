//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func seedHTTPThemeVisibilityCatalog(t *testing.T, f *serverFixture) {
	t.Helper()
	// These catalog facts exercise HTTP projections and the real playback state
	// machine. No stream or encoder is needed to observe NowPlaying metadata.
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO libraries (id, name, collection_type)
		VALUES ('http-theme-library', 'HTTP Theme Visibility', 'mixed');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('http-theme-root', 'http-theme-library', '/media/http-theme', '/media', 'http-theme');
		INSERT INTO items (id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, relative_path) VALUES
		('http-theme-library', 'http-theme-library', NULL, NULL, 'HTTP Theme Visibility', 'HTTP Theme Visibility', 'CollectionFolder', true, '', ''),
		('http-theme-owner', 'http-theme-library', 'http-theme-root', 'http-theme-library', 'Ordinary Movie Owner', 'Ordinary Movie Owner', 'Movie', false,
		 '/media/http-theme/Owner/feature.mp4', 'Owner/feature.mp4'),
		('http-theme-ordinary', 'http-theme-library', 'http-theme-root', 'http-theme-library', 'Ordinary Audio Control', 'Ordinary Audio Control', 'Audio', false,
		 '/media/http-theme/ordinary.mp3', 'ordinary.mp3'),
		('http-theme-song', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Active Theme Song', 'Active Theme Song', 'Audio', false,
		 '/media/http-theme/Owner/theme.mp3', 'Owner/theme.mp3'),
		('http-theme-video', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Active Theme Video', 'Active Theme Video', 'Video', false,
		 '/media/http-theme/Owner/backdrops/clip.mp4', 'Owner/backdrops/clip.mp4'),
		('http-theme-inactive', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Inactive Theme Song', 'Inactive Theme Song', 'Audio', false,
		 '/media/http-theme/Owner/theme-music/inactive.mp3', 'Owner/theme-music/inactive.mp3'),
		('http-theme-retained-role', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Permanent Former Theme', 'Permanent Former Theme', 'Audio', false,
		 '/media/http-theme/Owner/former-theme.mp3', 'Owner/former-theme.mp3'),
		('http-theme-legacy-file', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Reserved Legacy File', 'Reserved Legacy File', 'Audio', false,
		 '/media/http-theme/Owner/legacy.mp3', 'Owner/legacy.mp3'),
		('http-theme-legacy-directory', 'http-theme-library', 'http-theme-root', 'http-theme-owner', 'Reserved Legacy Directory', 'Reserved Legacy Directory', 'Folder', true,
		 '/media/http-theme/Owner/theme-music', 'Owner/theme-music'),
		('http-theme-legacy-descendant', 'http-theme-library', 'http-theme-root', 'http-theme-legacy-directory', 'Reserved Legacy Descendant', 'Reserved Legacy Descendant', 'Audio', false,
		 '/media/http-theme/Owner/theme-music/nested/legacy.mp3', 'Owner/theme-music/nested/legacy.mp3');
		UPDATE items SET media = '{"DurationTicks":6000000000,"Container":"mp3","Streams":[{"Index":0,"CodecType":"audio","Codec":"mp3","Channels":2,"SampleRate":44100}]}'::jsonb
		WHERE type = 'Audio' AND library_id = 'http-theme-library';
		UPDATE items SET media = '{"DurationTicks":6000000000,"Container":"mp4","Streams":[{"Index":0,"CodecType":"video","Codec":"h264","Width":160,"Height":90},{"Index":1,"CodecType":"audio","Codec":"aac","Channels":2,"SampleRate":48000}]}'::jsonb
		WHERE id IN ('http-theme-owner', 'http-theme-video');
		INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory) VALUES
		('http-theme-root', 'Owner/theme.mp3', false),
		('http-theme-root', 'Owner/backdrops', true),
		('http-theme-root', 'Owner/theme-music', true),
		('http-theme-root', 'Owner/legacy.mp3', false);
		INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active) VALUES
		('http-theme-song', 'http-theme-owner', 'song', true),
		('http-theme-video', 'http-theme-owner', 'video', true),
		('http-theme-inactive', 'http-theme-owner', 'song', false),
		('http-theme-retained-role', 'http-theme-owner', 'song', false)`); err != nil {
		t.Fatalf("seed HTTP theme visibility catalog: %v", err)
	}
}

func TestHTTPThemeVisibilityOverviewCountsOnlyOrdinaryItems(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	seedHTTPThemeVisibilityCatalog(t, f)
	assertCount := func(want int) {
		t.Helper()
		response := f.request(t, http.MethodGet, "/admin/v1/overview", nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		counts := objectValue(t, jsonObject(t, response), "Counts")
		if counts["Items"] != float64(want) || counts["Libraries"] != float64(1) {
			t.Fatalf("overview ordinary item count = %v, library count = %v, want %d / 1", counts["Items"], counts["Libraries"], want)
		}
	}
	var leaves int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM items WHERE NOT is_folder").Scan(&leaves); err != nil || leaves != 8 {
		t.Fatalf("HTTP fixture retained %d catalog leaves, want 8: %v", leaves, err)
	}
	assertCount(2)
	if _, err := f.pool.Exec(f.ctx, "UPDATE item_theme_resources SET active = false WHERE active"); err != nil {
		t.Fatal(err)
	}
	assertCount(2)
	// A detached legacy file remains hidden by its permanent reservation.
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM item_theme_resources WHERE resource_item_id = 'http-theme-song'"); err != nil {
		t.Fatal(err)
	}
	assertCount(2)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items (id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, relative_path)
		VALUES ('http-theme-new-ordinary', 'http-theme-library', 'http-theme-root', 'http-theme-library',
		'New Ordinary Movie', 'New Ordinary Movie', 'Movie', false, '/media/http-theme/new.mp4', 'new.mp4')`); err != nil {
		t.Fatal(err)
	}
	assertCount(3)
}

func TestHTTPThemeVisibilitySessionsKeepActiveThemesAndHideInvalidResources(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	seedHTTPThemeVisibilityCatalog(t, f)
	plays := make(map[string]string)
	for _, entry := range []struct {
		login clientSessionHTTPLogin
		item  string
	}{
		{accounts.viewer, "http-theme-song"},
		{accounts.second, "http-theme-video"},
		{accounts.other, "http-theme-ordinary"},
	} {
		owner := library.PlaybackOwner{UserID: entry.login.userID, SessionID: entry.login.id, DeviceID: entry.login.deviceID}
		prepared, err := f.app.library.PreparePlayback(f.ctx, owner, entry.item, media.SourceID(entry.item), "")
		if err != nil {
			t.Fatalf("prepare an owned HTTP theme projection session: %v", err)
		}
		position := int64(2 * media.TicksPerSecond)
		started, _, err := f.app.library.ReportPlayback(f.ctx, owner, library.PlaybackReport{
			Event: "Started", PlaySessionID: prepared.ID, ItemID: entry.item,
			MediaSourceID: media.SourceID(entry.item), PositionTicks: &position,
		})
		if err != nil || started.State != "Playing" || started.AuthSessionID != entry.login.id {
			t.Fatalf("start the exact authenticated theme playback: state=%s, error=%v", started.State, err)
		}
		plays[entry.login.id] = started.ID
	}
	// The two resources are intentionally absent from ordinary enumeration.
	browse, err := f.app.library.QueryItems(f.ctx, library.Query{UserID: accounts.viewer.userID,
		Ids: []string{"http-theme-song", "http-theme-video"}})
	if err != nil || browse.TotalRecordCount != 0 || len(browse.Items) != 0 {
		t.Fatal("the NowPlaying fixture accidentally made theme resources ordinary catalog items")
	}
	assertProjection := func(songActive, videoActive bool) {
		t.Helper()
		for _, observer := range []clientSessionHTTPLogin{accounts.admin, accounts.viewer} {
			sessions := clientSessionHTTPGet(t, f, observer, "")
			byID := make(map[string]map[string]any, len(sessions))
			for _, session := range sessions {
				assertClientSessionHTTPPrivate(t, session)
				byID[stringValue(t, session, "Id")] = session
			}
			for _, expected := range []struct {
				login  clientSessionHTTPLogin
				item   string
				kind   string
				active bool
			}{
				{accounts.viewer, "http-theme-song", "Audio", songActive},
				{accounts.second, "http-theme-video", "Video", videoActive},
			} {
				session, found := byID[expected.login.id]
				if !found {
					t.Fatal("theme visibility removed an otherwise live authentication session")
				}
				if !expected.active {
					assertClientSessionHTTPIdle(t, session)
					continue
				}
				item := objectValue(t, session, "NowPlayingItem")
				state := objectValue(t, session, "PlayState")
				if item["Id"] != expected.item || item["Type"] != expected.kind || item["RunTimeTicks"] != float64(600*media.TicksPerSecond) ||
					state["MediaSourceId"] != media.SourceID(expected.item) || state["PositionTicks"] != float64(2*media.TicksPerSecond) {
					t.Fatal("an authorized active theme lost its indexed NowPlaying item, source, or persisted position")
				}
			}
			if observer.id == accounts.admin.id {
				ordinary := objectValue(t, byID[accounts.other.id], "NowPlayingItem")
				if ordinary["Id"] != "http-theme-ordinary" {
					t.Fatal("filtering theme resources removed another session's ordinary NowPlaying item")
				}
			} else if _, exists := byID[accounts.other.id]; exists {
				t.Fatal("the ordinary viewer learned another user's authentication session")
			}
		}
	}
	assertProjection(true, true)
	if _, err := f.pool.Exec(f.ctx, "UPDATE item_theme_resources SET active = false WHERE resource_item_id = 'http-theme-song'"); err != nil {
		t.Fatal(err)
	}
	assertProjection(false, true)
	if _, err := f.pool.Exec(f.ctx, "UPDATE items SET parent_id = 'http-theme-library' WHERE id = 'http-theme-video'"); err != nil {
		t.Fatal(err)
	}
	assertProjection(false, false)
	// A read suppresses an invalid projection; it does not rewrite the owned
	// playback history or remove its authentication session.
	for authID, playID := range plays {
		var state, owner string
		if err := f.pool.QueryRow(f.ctx, "SELECT state, auth_session_id FROM play_sessions WHERE id = $1", playID).Scan(&state, &owner); err != nil ||
			state != "Playing" || owner != authID {
			t.Fatalf("session projection rewrote retained playback: state=%s, error=%v", state, err)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE item_theme_resources SET active = true WHERE resource_item_id = 'http-theme-song';
		UPDATE items SET parent_id = 'http-theme-owner' WHERE id = 'http-theme-video'`); err != nil {
		t.Fatal(err)
	}
	assertProjection(true, true)
}
