//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

func hlsSubtitleRenditionURLs(t *testing.T, body []byte) []string {
	t.Helper()
	var result []string
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "#EXT-X-MEDIA:TYPE=SUBTITLES,") {
			continue
		}
		_, value, ok := strings.Cut(line, "URI=\"")
		if !ok || !strings.HasSuffix(value, "\"") {
			t.Fatal("subtitle rendition has no bounded URI attribute")
		}
		result = append(result, strings.TrimSuffix(value, "\""))
	}
	return result
}

func TestHTTPHLSMultipleSubtitleWindowsViewsAndCurrentSourceAuthority(t *testing.T) {
	h := newHLSHTTPFixture(t)
	stem := strings.TrimSuffix(h.path, filepath.Ext(h.path))
	englishPath := stem + ".en.srt"
	for path, contents := range map[string]string{
		englishPath:      "1\n00:00:02,500 --> 00:00:03,500\nEnglish cross segment\n\n2\n00:00:07,000 --> 00:00:08,000\nEnglish later\n",
		stem + ".fr.srt": "1\n00:00:02,500 --> 00:00:03,500\nFrench cross segment\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	item, err := h.f.app.library.GetItem(h.f.ctx, h.accounts.viewer.userID, h.item.ID)
	if err != nil || len(item.Subtitles) != 2 {
		t.Fatal("the two authorized sidecar tracks were not indexed")
	}
	indexes := map[string]int{}
	for _, track := range item.Subtitles {
		indexes[track.Language] = track.Index
	}
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", map[string]any{}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	var negotiation struct {
		PlayID string `json:"PlaySessionId"`
	}
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlayID == "" {
		t.Fatal("the multi-track subtitle fixture did not prepare playback")
	}
	query := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")}, "PlaySessionId": {negotiation.PlayID},
		"MediaSourceId": {media.SourceID(item.ID)}, "DeviceId": {h.accounts.viewer.deviceID},
		"SegmentContainer": {"mp4"}, "VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "SegmentLength": {"3"},
		"AllowVideoStreamCopy": {"false"}, "AllowAudioStreamCopy": {"false"},
		"SubtitleStreamIndex": {strconv.Itoa(indexes["en"])}, "ManifestSubtitles": {"vtt"}, "SubtitleOffsetTicks": {"0"}}
	masterPath := "/emby/Videos/" + item.ID + "/master.m3u8?"
	master := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	tracks := hlsSubtitleRenditionURLs(t, master.body)
	if len(tracks) != 2 || videoHTTPJobCount(t, h, negotiation.PlayID, false) != 0 {
		t.Fatal("master omitted an authorized track or started media production")
	}
	for _, track := range tracks {
		expectHLSHTTPStatus(t, h.request(t, http.MethodHead, track, nil, nil), http.StatusOK)
	}
	if videoHTTPJobCount(t, h, negotiation.PlayID, false) != 0 {
		t.Fatal("subtitle HEAD started an A/V or subtitle producer")
	}
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != 1 {
		t.Fatal("the subtitle fixture should share one A/V rendition")
	}
	var mediaList hlsHTTPResponse
	for attempt := 0; attempt < 200; attempt++ {
		mediaList = h.request(t, http.MethodGet, variants[0], nil, nil)
		expectHLSHTTPStatus(t, mediaList, http.StatusOK)
		if bytes.Contains(mediaList.body, []byte("#EXT-X-ENDLIST")) {
			break
		}
		select {
		case <-h.f.ctx.Done():
			t.Fatal("subtitle media fixture exceeded its owned deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	if !bytes.Contains(mediaList.body, []byte("#EXT-X-ENDLIST")) || len(hlsHTTPManifestChildren(mediaList.body)) != 4 {
		t.Fatal("the actual media did not close four measured segments")
	}
	sessions := videoHTTPSessions(h, negotiation.PlayID)
	if len(sessions) != 1 {
		t.Fatal("multiple subtitle tracks allocated multiple A/V revisions")
	}
	session := sessions[0]
	session.mu.Lock()
	if len(session.producers) != 1 {
		session.mu.Unlock()
		t.Fatal("multiple subtitle tracks allocated multiple A/V producers")
	}
	producer := session.producers[0].id
	session.mu.Unlock()
	englishSlot := 0
	if indexes["en"] > indexes["fr"] {
		englishSlot = 1
	}
	playlist := h.request(t, http.MethodGet, tracks[englishSlot], nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	children := hlsHTTPManifestChildren(playlist.body)
	if len(children) != 4 || !bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("subtitle segments did not use the actual media window")
	}
	var cached hlsHTTPResponse
	for number, child := range children {
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		doc, err := subtitle.Parse(response.body, subtitle.FormatWebVTT)
		if err != nil {
			t.Fatal(err)
		}
		if number < 2 {
			if len(doc.Cues) != 1 || doc.Cues[0].StartTicks != 25000000 || doc.Cues[0].EndTicks != 35000000 || doc.Cues[0].Text != "English cross segment" {
				t.Fatalf("cross-segment cue was clipped or not repeated at media segment %d", number)
			}
		} else if number == 2 && (len(doc.Cues) != 1 || doc.Cues[0].Text != "English later") || number == 3 && len(doc.Cues) != 0 {
			t.Fatal("subtitle window leaked a cue or omitted its valid empty segment")
		}
		if number == 0 {
			cached = response
		}
	}
	for _, selection := range []int{indexes["fr"], -1, indexes["en"]} {
		query.Set("SubtitleStreamIndex", strconv.Itoa(selection))
		query.Set("SubtitleOffsetTicks", "2500000")
		changed := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
		expectHLSHTTPStatus(t, changed, http.StatusOK)
		for _, child := range hlsSubtitleRenditionURLs(t, changed.body) {
			parsed, err := url.Parse(child)
			if err != nil || parsed.Query().Get("GobyHlsId") != session.id || parsed.Query().Get("SubtitleStreamIndex") != strconv.Itoa(selection) || parsed.Query().Get("SubtitleOffsetTicks") != "2500000" {
				t.Fatal("subtitle selection or offset replaced the immutable A/V producer")
			}
		}
		if selection == -1 && (bytes.Contains(changed.body, []byte("DEFAULT=YES")) || bytes.Contains(changed.body, []byte("AUTOSELECT=YES"))) {
			t.Fatal("off view still auto-selects a subtitle")
		}
	}
	session.mu.Lock()
	unchanged := len(session.producers) == 1 && session.producers[0].id == producer
	session.mu.Unlock()
	if !unchanged || len(videoHTTPSessions(h, negotiation.PlayID)) != 1 {
		t.Fatal("view changes restarted the shared producer")
	}
	old := h.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, old, http.StatusOK)
	if !bytes.Equal(old.body, cached.body) {
		t.Fatal("later view changes changed the original subtitle URL")
	}
	for _, name := range []string{"subtitles.m3u8", "live_subtitles.m3u8"} {
		standard, _ := url.Parse(tracks[englishSlot])
		standard.Path = "/emby/Videos/" + item.ID + "/" + name
		expectHLSHTTPStatus(t, h.request(t, http.MethodHead, standard.String(), nil, nil), http.StatusOK)
		response := h.request(t, http.MethodGet, standard.String(), nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.body, playlist.body) {
			t.Fatal("standard subtitle route did not reuse the authorized media window")
		}
	}
	if cached.header.Get("ETag") == "" {
		t.Fatal("subtitle cache validation has no representation identity")
	}
	conditional := http.Header{"If-None-Match": {cached.header.Get("ETag")}}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, children[0], nil, conditional), http.StatusNotModified)
	foreign, _ := url.Parse(children[0])
	foreignQuery := foreign.Query()
	foreignQuery.Set("api_key", h.accounts.second.headers.Get("X-Emby-Token"))
	foreign.RawQuery = foreignQuery.Encode()
	if response := h.request(t, http.MethodGet, foreign.String(), nil, conditional); response.status < 400 {
		t.Fatal("a foreign principal used a cached subtitle representation")
	}
	if err := os.WriteFile(englishPath, []byte("1\n00:00:02,500 --> 00:00:03,500\nChanged bytes without a rescan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if response := h.request(t, http.MethodGet, children[0], nil, conditional); response.status < 400 {
		t.Fatal("a matching ETag bypassed the changed sidecar fingerprint")
	}
	session.mu.Lock()
	closed := session.closed
	session.mu.Unlock()
	if !closed {
		t.Fatal("a confirmed sidecar fingerprint change did not retire its bound revision")
	}
	videoHTTPWaitRetired(t, h, negotiation.PlayID, sessions)
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	current, err := h.f.app.library.GetItem(h.f.ctx, h.accounts.viewer.userID, item.ID)
	if err != nil || len(current.Subtitles) != 2 {
		t.Fatal("rescan did not publish the current subtitle identities")
	}
	for _, track := range current.Subtitles {
		indexes[track.Language] = track.Index
	}
	englishSlot = 0
	if indexes["en"] > indexes["fr"] {
		englishSlot = 1
	}
	query.Set("SubtitleStreamIndex", strconv.Itoa(indexes["fr"]))
	query.Set("SubtitleOffsetTicks", "0")
	replacement := h.request(t, http.MethodGet, masterPath+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, replacement, http.StatusOK)
	replacementTracks := hlsSubtitleRenditionURLs(t, replacement.body)
	if len(replacementTracks) != 2 {
		t.Fatal("fresh subtitle bytes did not receive a new bound revision")
	}
	replacementSessions := videoHTTPSessions(h, negotiation.PlayID)
	if _, err := h.f.pool.Exec(h.f.ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnableSubtitleManagement}', 'true'::jsonb, true) WHERE id=$1`, h.accounts.admin.userID); err != nil {
		t.Fatal(err)
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, "/emby/Videos/"+item.ID+"/Subtitles/"+strconv.Itoa(indexes["en"]), nil, h.accounts.admin.headers), http.StatusNoContent)
	if response := h.request(t, http.MethodHead, replacementTracks[1-englishSlot], nil, nil); response.status < 400 {
		t.Fatal("deleting one bound track retained an old multi-track revision")
	}
	videoHTTPWaitRetired(t, h, negotiation.PlayID, replacementSessions)
}
