//go:build linux

package server

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPNextUpPartialSeriesCursorUsesDurablePlaybackAndCurrentScope(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		partialIndex int
	}{
		{name: "FirstEpisodePartial", partialIndex: 0},
		{name: "SecondEpisodePartial", partialIndex: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p := newPlaybackHTTPFixture(t)
			f := p.s.f
			for _, name := range []string{
				"nextup-partial/Partial Show/Season 01/Partial.Show.S01E01.mp4",
				"nextup-partial/Partial Show/Season 01/Partial.Show.S01E02.mp4",
				"nextup-partial/Partial Show/Season 02/Partial.Show.S02E01.mp4",
			} {
				writeAPIMediaFile(t, p.s.root, name)
			}
			collection, err := f.app.library.CreateLibrary(f.ctx, "Partial next-up television", "tvshows", []string{filepath.Join(p.s.root, "nextup-partial")})
			if err != nil {
				t.Fatal("create partial next-up television library")
			}
			p.s.rescan(t, collection.ID)
			p.s.setPolicy(t, p.s.viewerID, true, []string{collection.ID})
			series, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet,
				"/emby/Items?ParentId="+collection.ID+"&Recursive=true&IncludeItemTypes=Series", "", p.headers, nil))
			if total != 1 || len(series) != 1 {
				t.Fatal("partial next-up fixture must contain one series")
			}
			seriesID := stringValue(t, series[0], "Id")
			episodes, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes", "", p.headers, nil))
			if total != 3 || len(episodes) != 3 {
				t.Fatal("partial next-up fixture must contain three ordered episodes")
			}
			ids := []string{stringValue(t, episodes[0], "Id"), stringValue(t, episodes[1], "Id"), stringValue(t, episodes[2], "Id")}
			read := func(userID, suffix string, headers http.Header, count int, expected ...string) []map[string]any {
				t.Helper()
				items, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet,
					"/emby/Shows/NextUp?UserId="+userID+"&SeriesId="+seriesID+suffix, "", headers, nil))
				if total != count || len(items) != len(expected) {
					t.Fatalf("partial next-up total/page = %d/%d, want %d/%d", total, len(items), count, len(expected))
				}
				for index, id := range expected {
					if items[index]["Id"] != id || items[index]["Type"] != "Episode" {
						t.Error("partial next-up changed the ordered episode sequence")
					}
				}
				return items
			}
			read(p.s.viewerID, "", p.headers, 0)
			partialID := ids[scenario.partialIndex]
			playTo := func(position int64) {
				t.Helper()
				prepared, source := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost,
					"/emby/Items/"+partialID+"/PlaybackInfo", playbackTextJSON(t, matchingPlaybackHTTPBody()), p.headers, nil))
				if source["RunTimeTicks"] != float64(600*media.TicksPerSecond) {
					t.Fatal("partial next-up requires the authoritative 600-second episode duration")
				}
				playID := stringValue(t, prepared, "PlaySessionId")
				for _, report := range []struct {
					path     string
					position int64
				}{
					{path: "/emby/Sessions/Playing", position: 0},
					{path: "/emby/Sessions/Playing/Progress", position: position},
					{path: "/emby/Sessions/Playing/Stopped", position: position},
				} {
					body := playbackTextJSON(t, map[string]any{
						"ItemId": partialID, "MediaSourceId": media.SourceID(partialID), "PlaySessionId": playID,
						"SessionId": p.authSessionID, "PositionTicks": report.position, "CanSeek": true,
						"PlayMethod": "DirectStream", "EventName": "TimeUpdate",
					})
					expectStatus(t, playbackTextRequest(t, p, http.MethodPost, report.path, body, p.headers, nil), http.StatusNoContent)
				}
			}
			playTo(120 * media.TicksPerSecond)
			for _, id := range ids {
				response := playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"/Items/"+id, "", p.headers, nil)
				expectStatus(t, response, http.StatusOK)
				position, count := int64(0), 0
				if id == partialID {
					position, count = 120*media.TicksPerSecond, 1
				}
				assertPlaybackHTTPData(t, objectValue(t, jsonObject(t, response), "UserData"), position, count, false, false)
			}
			expected := ids[scenario.partialIndex:]
			items := read(p.s.viewerID, "", p.headers, len(expected), expected...)
			assertPlaybackHTTPData(t, objectValue(t, items[0], "UserData"), 120*media.TicksPerSecond, 1, false, false)
			for index, id := range expected {
				read(p.s.viewerID, "&Limit=1&StartIndex="+strconv.Itoa(index), p.headers, len(expected), id)
			}
			read(p.s.viewerID, "&Limit=1&StartIndex="+strconv.Itoa(len(expected)), p.headers, len(expected))
			read(p.s.viewerID, "&Limit=0", p.headers, len(expected))
			other, err := f.users.CreateUser(f.ctx, "Partial next-up other", "partial-next-up-other-password", false)
			if err != nil {
				t.Fatal("create an independent partial next-up viewer")
			}
			p.s.setPolicy(t, other.ID, true, []string{collection.ID})
			otherLogin := f.embyLogin(t, other.Name, "partial-next-up-other-password")
			otherHeaders := http.Header{"X-Emby-Token": {stringValue(t, otherLogin, "AccessToken")}}
			read(other.ID, "", otherHeaders, 0)
			expectStatus(t, playbackTextRequest(t, p, http.MethodGet,
				"/emby/Shows/NextUp?UserId="+p.s.viewerID+"&SeriesId="+seriesID, "", otherHeaders, nil), http.StatusForbidden)
			playTo(600 * media.TicksPerSecond)
			afterCompletion := ids[scenario.partialIndex+1:]
			read(p.s.viewerID, "", p.headers, len(afterCompletion), afterCompletion...)
			p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
			expectStatus(t, playbackTextRequest(t, p, http.MethodGet,
				"/emby/Shows/NextUp?UserId="+p.s.viewerID+"&SeriesId="+seriesID, "", p.headers, nil), http.StatusNotFound)
		})
	}
}
