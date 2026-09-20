//go:build linux

package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPOriginalClientEpisodeQueueIsCompleteAndDoesNotOwnAutoplay(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	for episode := 1; episode <= 105; episode++ {
		writeAPIMediaFile(t, p.s.root, fmt.Sprintf("client-queue/Example Show/Season 01/Example.Show.S01E%03d.mp4", episode))
	}
	collection, err := p.s.f.app.library.CreateLibrary(p.s.f.ctx, "Client episode queue", "tvshows", []string{filepath.Join(p.s.root, "client-queue")})
	if err != nil {
		t.Fatal(err)
	}
	p.s.rescan(t, collection.ID)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID, collection.ID})
	f := p.s.f
	series, count := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+collection.ID+"&Recursive=true&IncludeItemTypes=Series", nil, p.headers))
	if count != 1 {
		t.Fatal("queue fixture must contain one series")
	}
	path := "/emby/Shows/" + stringValue(t, series[0], "Id") + "/Episodes?IsVirtualUnaired=false&IsMissing=false&UserId=" + p.s.viewerID + "&Fields=ProductionYear,PremiereDate,Container,PresentationUniqueKey"
	items, count := responseItems(t, f.request(t, http.MethodGet, path, nil, p.headers))
	if count != 105 || len(items) != 105 || items[100]["IndexNumber"] != float64(101) {
		t.Fatal("the original client's unpaged queue cannot locate episodes after the first hundred")
	}
	first := stringValue(t, items[0], "Id")
	clientPath := strings.Replace(path, "&Fields=", "&fields=", 1) + "&ExcludeFields=MediaStreams&X-Emby-Language=en-US"
	clientItems, clientCount := responseItems(t, f.request(t, http.MethodGet, clientPath, nil, p.headers))
	if clientCount != 105 || len(clientItems) != 105 || clientItems[100]["Id"] != items[100]["Id"] {
		t.Fatal("ordinary client projection or UI language changed complete episode queue selection")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+p.s.viewerID+"/PlayedItems/"+first, nil, p.headers), http.StatusOK)
	for _, autoplay := range []bool{false, true} {
		response := f.request(t, http.MethodPost, "/users/"+p.s.viewerID+"/configuration/partial", map[string]any{"EnableNextEpisodeAutoPlay": autoplay, "IntroSkipMode": "AutoSkip"}, p.headers)
		expectStatus(t, response, http.StatusOK)
		if response.Body.Len() != 0 {
			t.Fatal("client partial configuration write must return an empty success body")
		}
		items, count = responseItems(t, f.request(t, http.MethodGet, path, nil, p.headers))
		if count != 105 || len(items) != 105 || items[0]["Id"] != first {
			t.Fatal("autoplay preference or played history defeated an explicit queue/replay action")
		}
		user := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+p.s.viewerID, nil, p.headers))
		configuration := objectValue(t, user, "Configuration")
		if configuration["EnableNextEpisodeAutoPlay"] != autoplay || configuration["IntroSkipMode"] != "AutoSkip" {
			t.Fatal("the actual client's user projection did not consume playback behavior preferences")
		}
	}
	page, count := responseItems(t, f.request(t, http.MethodGet, path+"&Limit=1&StartIndex=100", nil, p.headers))
	if count != 105 || len(page) != 1 || page[0]["Id"] != items[100]["Id"] {
		t.Fatal("the complete queue adapter ignored explicit pagination")
	}
	var sessions int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM play_sessions WHERE user_id=$1`, p.s.viewerID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("reading or changing client autoplay created a playback session")
	}
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	expectStatus(t, f.request(t, http.MethodGet, path, nil, p.headers), http.StatusNotFound)
}
