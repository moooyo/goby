//go:build linux

package server

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPUserDataRatingAndHideFromResumePreserveIndependentState(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	base := "/emby/Users/" + p.s.viewerID + "/Items/" + p.s.video.id
	position := int64(120 * media.TicksPerSecond)
	response := p.s.f.request(t, http.MethodPost, base+"/UserData", map[string]any{
		"PlaybackPositionTicks": position, "PlayCount": 3, "Played": false, "IsFavorite": true,
		"LastPlayedDate": "2025-01-02T03:04:05Z", "Rating": 8.5,
	}, p.headers)
	expectStatus(t, response, http.StatusOK)
	data := jsonObject(t, response)
	assertPlaybackHTTPData(t, data, position, 3, false, true)
	if data["ItemId"] != p.s.video.id || data["Rating"] != 8.5 || data["LastPlayedDate"] != "2025-01-02T03:04:05Z" {
		t.Fatal("writable state fields did not round-trip")
	}
	resume := "/emby/Users/" + p.s.viewerID + "/Items/Resume"
	assertResume := func(want int) {
		t.Helper()
		items, total := responseItems(t, p.s.f.request(t, http.MethodGet, resume, nil, p.headers))
		if total != want || len(items) != want {
			t.Fatalf("resume item count = %d/%d, want %d", total, len(items), want)
		}
	}
	assertResume(1)
	hidden := p.s.f.request(t, http.MethodPost, base+"/HideFromResume?Hide=true", nil, p.headers)
	expectStatus(t, hidden, http.StatusOK)
	assertPlaybackHTTPData(t, jsonObject(t, hidden), position, 3, false, true)
	if jsonObject(t, hidden)["HideFromResume"] != true {
		t.Fatal("resume hiding was not stored")
	}
	assertResume(0)
	listed, total := responseItems(t, p.s.f.request(t, http.MethodGet, "/emby/Items?Ids="+p.s.video.id, nil, p.headers))
	if total != 1 || objectValue(t, listed[0], "UserData")["PlaybackPositionTicks"] != float64(position) {
		t.Fatal("hide from resume removed the item or erased progress")
	}
	expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/HideFromResume?Hide=false", nil, p.headers), http.StatusOK)
	assertResume(1)
	rated := p.s.f.request(t, http.MethodPost, base+"/Rating?Likes=false", nil, p.headers)
	expectStatus(t, rated, http.StatusOK)
	if got := jsonObject(t, rated); got["Likes"] != false || got["Rating"] != float64(0) {
		t.Fatal("dislike was confused with an absent rating")
	}
	rated = p.s.f.request(t, http.MethodPost, base+"/UserData", map[string]any{"Rating": 6.5}, p.headers)
	expectStatus(t, rated, http.StatusOK)
	if got := jsonObject(t, rated); got["Rating"] != 6.5 || got["Likes"] != nil {
		t.Fatal("numeric rating retained a stale thumbs-down value")
	}
	expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/Rating?Likes=true", nil, p.headers), http.StatusOK)
	expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/UserData", map[string]any{"IsFavorite": false}, p.headers), http.StatusOK)
	liked, total := responseItems(t, p.s.f.request(t, http.MethodGet, "/emby/Items?Recursive=true&IsFolder=false&Filters=IsFavoriteOrLikes", nil, p.headers))
	if total != 1 || liked[0]["Id"] != p.s.video.id {
		t.Fatal("rating-like state was not consumed by the combined favorite filter")
	}
	cleared := p.s.f.request(t, http.MethodDelete, base+"/Rating", nil, p.headers)
	expectStatus(t, cleared, http.StatusOK)
	if got := jsonObject(t, cleared); got["Rating"] != nil || got["Likes"] != nil {
		t.Fatal("rating deletion retained a nullable rating or like")
	}
	for _, body := range []map[string]any{
		{"Rating": 11, "IsFavorite": true}, {"PlaybackPositionTicks": 601 * media.TicksPerSecond},
		{"PlayCount": -1}, {"PlayedPercentage": 10}, {"ItemId": p.secondItemID}, {"Unknown": true},
	} {
		expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/UserData", body, p.headers), http.StatusBadRequest)
	}
	read := p.s.f.request(t, http.MethodGet, base+"/UserData", nil, p.headers)
	expectStatus(t, read, http.StatusOK)
	assertPlaybackHTTPData(t, jsonObject(t, read), position, 3, false, false)
	expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/HideFromResume", nil, p.headers), http.StatusBadRequest)
	expectStatus(t, p.s.f.request(t, http.MethodPost, "/emby/Users/"+p.s.adminID+"/Items/"+p.s.video.id+"/UserData", map[string]any{"Played": true}, p.headers), http.StatusForbidden)
	marked := p.s.f.request(t, http.MethodPost, base+"/UserData", map[string]any{"Played": true, "LastPlayedDate": nil}, p.headers)
	expectStatus(t, marked, http.StatusOK)
	assertPlaybackHTTPData(t, jsonObject(t, marked), 0, 3, true, false)
	if jsonObject(t, marked)["LastPlayedDate"] != nil {
		t.Fatal("an explicit nullable date clear was lost")
	}
	assertResume(0)
}

func TestHTTPLegacyPlayingItemsSharesPlaybackLifecycleAndOwnership(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	base := "/emby/Users/" + p.s.viewerID + "/PlayingItems/" + p.s.video.id
	query := url.Values{"PlaySessionId": {playID}, "MediaSourceId": {media.SourceID(p.s.video.id)}}
	copyQuery := func() url.Values {
		copied := make(url.Values, len(query))
		for key, values := range query {
			copied[key] = append([]string(nil), values...)
		}
		return copied
	}
	report := func(method, suffix string, position int64, paused bool) {
		t.Helper()
		values := copyQuery()
		values.Set("PositionTicks", strconv.FormatInt(position, 10))
		values.Set("IsPaused", strconv.FormatBool(paused))
		response := p.s.f.request(t, method, base+suffix+"?"+values.Encode(), nil, p.headers)
		expectStatus(t, response, http.StatusOK)
		if response.Body.Len() != 0 {
			t.Fatal("legacy playback success did not use its empty 200 response")
		}
	}
	state := func(want string, position int64, stopped bool) {
		t.Helper()
		var current string
		var ticks int64
		var isStopped bool
		if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT state,position_ticks,stopped_at IS NOT NULL FROM play_sessions WHERE id=$1`, playID).Scan(&current, &ticks, &isStopped); err != nil {
			t.Fatal(err)
		}
		if current != want || ticks != position || isStopped != stopped {
			t.Fatalf("legacy state = %s/%d/%t, want %s/%d/%t", current, ticks, isStopped, want, position, stopped)
		}
	}
	report(http.MethodPost, "", 0, false)
	report(http.MethodPost, "", 300*media.TicksPerSecond, false)
	state("Playing", 0, false)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 1, false, false)
	report(http.MethodPost, "/Progress", 120*media.TicksPerSecond, true)
	state("Paused", 120*media.TicksPerSecond, false)
	// The modern route can continue the same legacy-started session.
	p.report(t, "Progress", playID, 125*media.TicksPerSecond)
	state("Playing", 125*media.TicksPerSecond, false)
	conflict := base + "/Progress?" + query.Encode() + "&PositionTicks=1"
	expectStatus(t, p.s.f.request(t, http.MethodPost, conflict, map[string]any{"PositionTicks": 2}, p.headers), http.StatusBadRequest)
	foreign := "/emby/Users/" + p.s.adminID + "/PlayingItems/" + p.s.video.id + "/Progress?" + query.Encode()
	expectStatus(t, p.s.f.request(t, http.MethodPost, foreign, nil, p.headers), http.StatusForbidden)
	wrongSource := copyQuery()
	wrongSource.Set("MediaSourceId", media.SourceID(p.secondItemID))
	expectStatus(t, p.s.f.request(t, http.MethodPost, base+"/Progress?"+wrongSource.Encode(), nil, p.headers), http.StatusNotFound)
	state("Playing", 125*media.TicksPerSecond, false)
	report(http.MethodDelete, "", 125*media.TicksPerSecond, false)
	state("Stopped", 125*media.TicksPerSecond, true)
	report(http.MethodPost, "/Delete", 570*media.TicksPerSecond, false)
	state("Stopped", 125*media.TicksPerSecond, true)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 125*media.TicksPerSecond, 1, false, false)
	var sessions int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT count(*) FROM play_sessions WHERE auth_session_id=$1 AND item_id=$2`, p.authSessionID, p.s.video.id).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatal("legacy reports created a second playback state machine")
	}
}
