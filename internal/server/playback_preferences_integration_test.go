//go:build linux

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func TestHTTPPlaybackRemembersServableSidecarsAndInvalidatesReplacedTrackIdentity(t *testing.T) {
	s := newSubtitleHTTPFixture(t)
	p := s.p
	var playID string
	negotiate := func(selection *int) map[string]any {
		t.Helper()
		request := matchingPlaybackHTTPBody()
		request["DeviceProfile"].(map[string]any)["SubtitleProfiles"] = []map[string]any{{"Format": "vtt", "Method": "External"}}
		if playID != "" {
			request["CurrentPlaySessionId"] = playID
		}
		if selection != nil {
			request["SubtitleStreamIndex"] = *selection
		}
		object, source := p.prepare(t, request)
		playID = stringValue(t, object, "PlaySessionId")
		return source
	}
	reportSelection := func(index int) {
		t.Helper()
		response := p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing", map[string]any{
			"ItemId": p.s.video.id, "MediaSourceId": media.SourceID(p.s.video.id), "PlaySessionId": playID,
			"SessionId": p.authSessionID, "PositionTicks": 0, "AudioStreamIndex": 5, "SubtitleStreamIndex": index,
		}, p.headers)
		expectStatus(t, response, http.StatusNoContent)
		p.report(t, "Stopped", playID, 120*media.TicksPerSecond)
		playID = ""
	}
	remembered := func() (int, string) {
		t.Helper()
		var index int
		var stamp string
		if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT remembered_subtitle_stream_index,remembered_media_stamp FROM user_item_data WHERE user_id=$1 AND item_id=$2`, p.s.viewerID, p.s.video.id).Scan(&index, &stamp); err != nil {
			t.Fatal(err)
		}
		return index, stamp
	}
	if negotiate(nil)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Fatal("unset Smart mode did not retain the default off selection")
	}
	reportSelection(6)
	index, firstStamp := remembered()
	if index != 6 || len(firstStamp) != 32 {
		t.Fatal("a servable external subtitle was not remembered with its media identity")
	}
	source := negotiate(nil)
	if _, exists := source["DefaultSubtitleStreamIndex"]; exists {
		t.Fatal("remembered sidecar was not negotiated as selected external delivery")
	}
	selected := subtitleHTTPTrack(t, source, 6)
	result := p.s.request(t, http.MethodGet, subtitleHTTPDeliveryURL(t, s, selected, 6, "vtt"), "", nil, nil)
	expectSubtitleHTTPBody(t, result, "text/vtt", subtitleHTTPConvertedVTT)
	off := -1
	if negotiate(&off)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Fatal("remembered captions defeated explicit Off")
	}
	if index, _ := remembered(); index != 6 {
		t.Fatal("negotiation alone wrote selection preferences")
	}
	reportSelection(-1)
	if negotiate(nil)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Fatal("remembered Off became an unset selection")
	}
	updateConfigurationHTTP(t, p, map[string]any{"RememberSubtitleSelections": false})
	reportSelection(6)
	if index, _ := remembered(); index != -1 {
		t.Fatal("disabled remember setting still changed its stored subtitle index")
	}
	updateConfigurationHTTP(t, p, map[string]any{"RememberSubtitleSelections": true})
	negotiate(nil)
	reportSelection(6)
	if index, _ := remembered(); index != 6 {
		t.Fatal("reenabled remember setting did not persist a current sidecar")
	}
	// Replace the bytes in place: the stream index remains allocated, but the
	// indexed sidecar identity must invalidate the previous selection stamp.
	writeSubtitleHTTPFile(t, s.srtPath, "1\n00:00:01,000 --> 00:00:02,000\nReplacement caption asset\n")
	p.s.rescan(t, p.s.video.libraryID)
	if subtitleHTTPTrack(t, s.detail(t), 6)["Index"] != float64(6) {
		t.Fatal("replacement fixture did not preserve its external index")
	}
	if negotiate(nil)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Fatal("reused sidecar index inherited a previous asset's remembered selection")
	}
	if index, stamp := remembered(); index != 6 || stamp != firstStamp {
		t.Fatal("invalidation was implemented by rewriting old user state during negotiation")
	}
	p.report(t, "Stopped", playID, 120*media.TicksPerSecond)
}

func TestPersistedResumePreferenceOnlyChangesImplicitPlaybackStarts(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	updateConfigurationHTTP(t, p, map[string]any{"ResumeRewindSeconds": 10})
	path := "/emby/Users/" + p.s.viewerID + "/Items/" + p.s.video.id + "/UserData"
	expectStatus(t, p.s.f.request(t, http.MethodPost, path, map[string]any{"PlaybackPositionTicks": 120 * media.TicksPerSecond}, p.headers), http.StatusOK)
	principal, err := p.s.f.users.Resolve(p.s.f.ctx, p.s.token, "emby")
	if err != nil {
		t.Fatal("resolve playback preference fixture authority")
	}
	file, source, err := p.s.f.app.library.OpenMediaFor(p.s.f.ctx, librarySubject(principal, principal.User.ID), p.s.video.id, media.SourceID(p.s.video.id))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	isPlayback := true
	for _, test := range []struct {
		name  string
		start *int64
		play  *bool
		want  *int64
	}{
		{"implicit playback", nil, &isPlayback, preferenceTicks(110 * media.TicksPerSecond)},
		{"explicit start", preferenceTicks(40 * media.TicksPerSecond), &isPlayback, preferenceTicks(40 * media.TicksPerSecond)},
		{"capability lookup", nil, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := playback.Request{StartTimeTicks: test.start, IsPlayback: test.play}
			if err := p.s.f.app.applyStaticPlaybackPreferences(p.s.f.ctx, httptest.NewRequest(http.MethodPost, "/", nil), principal, source, &request); err != nil {
				t.Fatal(err)
			}
			if (request.StartTimeTicks == nil) != (test.want == nil) || test.want != nil && *request.StartTimeTicks != *test.want {
				t.Fatal("resume rewind overrode an explicit start or did not reach the playback request")
			}
		})
	}
}

func preferenceTicks(value int64) *int64 { return &value }
