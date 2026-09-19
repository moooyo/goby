//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

func dynamicHTTPSubtitleRenditions(t *testing.T, body []byte) []string {
	t.Helper()
	var result []string
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "#EXT-X-MEDIA:TYPE=SUBTITLES,") {
			continue
		}
		_, value, ok := strings.Cut(line, ",URI=\"")
		if !ok || !strings.HasSuffix(value, "\"") {
			t.Fatal("dynamic subtitle rendition lacks a bounded quoted URI")
		}
		result = append(result, strings.TrimSuffix(value, "\""))
	}
	return result
}

func dynamicHTTPSubtitleDocument(t *testing.T, response hlsHTTPResponse, marker string) subtitle.Document {
	t.Helper()
	expectHLSHTTPStatus(t, response, http.StatusOK)
	if !strings.Contains(response.header.Get("Content-Type"), "text/vtt") ||
		!bytes.Contains(response.body, []byte("X-TIMESTAMP-MAP=")) || !bytes.Contains(response.body, []byte(marker)) {
		t.Fatal("dynamic subtitle segment lost its bound clock or selected caption")
	}
	document, err := subtitle.Parse(response.body, subtitle.FormatWebVTT)
	if err != nil || len(document.Cues) != 1 {
		t.Fatal("dynamic subtitle segment is not one complete cross-segment cue", err)
	}
	return document
}

func TestDynamicSubtitleHTTPDocumentEightTracksSwitchOffOffsetAndRetainedEpochs(t *testing.T) {
	d := newDynamicTimeshiftHTTPFixture(t, true)
	// The fixed target is four seconds. Leave room for a leading three-second
	// subtitle interval to expire under a positive offset while the remaining
	// live playlist still meets the three-target-duration minimum.
	d.h.f.app.cfg.Timeshift.WindowSeconds = 18
	p := d.open(t)
	h := d.h
	first := d.waitWindow(t, p, func(w dynamicHTTPWindow) bool { return w.LiveEdgeTicks >= 9*media.TicksPerSecond })
	master := h.request(t, http.MethodGet, p.masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	tracks := dynamicHTTPSubtitleRenditions(t, master.body)
	if len(tracks) != 8 || bytes.Contains(master.body, []byte("DEFAULT=YES")) {
		t.Fatal("off view did not preserve all eight optional subtitle tracks")
	}
	var firstChild string
	var original subtitle.Document
	for index, track := range tracks {
		playlist := h.request(t, http.MethodGet, track, nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		children := hlsHTTPManifestChildren(playlist.body)
		if len(children) < 2 {
			t.Fatal("rolling subtitle rendition did not cover the buffered media interval")
		}
		// The same full cue crosses multiple media segments. Its endpoints
		// must remain intact rather than being clipped to each segment.
		one := dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, children[0], nil, nil), "caption-track-"+strconv.Itoa(index))
		two := dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, children[1], nil, nil), "caption-track-"+strconv.Itoa(index))
		if one.Cues[0].StartTicks != two.Cues[0].StartTicks || one.Cues[0].EndTicks != two.Cues[0].EndTicks || one.Cues[0].EndTicks-one.Cues[0].StartTicks != 28*media.TicksPerSecond {
			t.Fatal("a cue crossing media segments lost its complete original timestamps")
		}
		if d.subtitleRequests[index].Load() != 1 {
			t.Fatal("subtitle rendition selection reopened its finite sidecar")
		}
		if index == 0 {
			firstChild, original = children[0], one
		}
	}
	selected := dynamicHTTPQuery(t, p.masterURL, map[string]string{"SubtitleStreamIndex": strconv.Itoa(dynamicsource.ExternalSubtitleIndexBase + 7)})
	chosen := h.request(t, http.MethodGet, selected, nil, nil)
	expectHLSHTTPStatus(t, chosen, http.StatusOK)
	if bytes.Count(chosen.body, []byte("DEFAULT=YES")) != 1 || !bytes.Contains(chosen.body, []byte("Caption 7")) {
		t.Fatal("track selection did not remain an independent master view")
	}
	chosenChildren := hlsHTTPManifestChildren(chosen.body)
	if len(chosenChildren) != 1 || !strings.Contains(chosenChildren[0], "SubtitleStreamIndex="+strconv.Itoa(dynamicsource.ExternalSubtitleIndexBase+7)) {
		t.Fatal("selected view was not propagated to media children")
	}
	off := h.request(t, http.MethodGet, p.masterURL, nil, nil)
	expectHLSHTTPStatus(t, off, http.StatusOK)
	if bytes.Contains(off.body, []byte("DEFAULT=YES")) {
		t.Fatal("another subtitle view changed the original off selection")
	}
	// Advertised AV grace preserves this unshifted caption resource. It does
	// not restore subtitle completeness before the journal's retained start,
	// even when a surviving long cue crosses that boundary.
	retained := dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, firstChild, nil, nil), "caption-track-0")
	if retained.Cues[0].StartTicks != original.Cues[0].StartTicks || retained.Cues[0].EndTicks != original.Cues[0].EndTicks {
		t.Fatal("advertised subtitle grace changed an existing unshifted cue")
	}
	shifted := dynamicHTTPQuery(t, firstChild, map[string]string{"SubtitleOffsetTicks": strconv.FormatInt(media.TicksPerSecond, 10)})
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, shifted, nil, nil), http.StatusGone)
	// Re-negotiate only the subtitle view. Its current playlist identifies
	// usable intervals instead of promising the unavailable leading prefix.
	shiftedPlaylistURL := dynamicHTTPQuery(t, tracks[0], map[string]string{"SubtitleOffsetTicks": strconv.FormatInt(media.TicksPerSecond, 10)})
	shiftedPlaylist := h.request(t, http.MethodGet, shiftedPlaylistURL, nil, nil)
	expectHLSHTTPStatus(t, shiftedPlaylist, http.StatusOK)
	shiftedChildren := hlsHTTPManifestChildren(shiftedPlaylist.body)
	if len(shiftedChildren) == 0 {
		t.Fatal("offset subtitle view did not advertise a complete retained interval")
	}
	for _, child := range shiftedChildren {
		if child == shifted {
			t.Fatal("offset subtitle playlist advertised its unavailable leading interval")
		}
	}
	offset := dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, shiftedChildren[0], nil, nil), "caption-track-0")
	if offset.Cues[0].StartTicks-original.Cues[0].StartTicks != media.TicksPerSecond || offset.Cues[0].EndTicks-original.Cues[0].EndTicks != media.TicksPerSecond {
		t.Fatal("viewer subtitle offset did not shift the complete cue exactly once")
	}
	if d.mediaRequests.Load() != 1 || d.subtitleRequests[0].Load() != 1 {
		t.Fatal("an offset subtitle view reopened its producer or finite sidecar")
	}
	query, err := url.Parse(selected)
	if err != nil {
		t.Fatal("invalid selected subtitle URL")
	}
	values := query.Query()
	values.Set("LiveStreamId", p.liveID)
	values.Set("SubtitleSegmentLength", "3")
	standard := "/emby/Videos/" + h.item.ID + "/live_subtitles.m3u8?" + values.Encode()
	standardPlaylist := h.request(t, http.MethodGet, standard, nil, nil)
	expectHLSHTTPStatus(t, standardPlaylist, http.StatusOK)
	standardChildren := hlsHTTPManifestChildren(standardPlaylist.body)
	if len(standardChildren) == 0 {
		t.Fatal("standard subtitle playlist entry did not resolve the selected dynamic track")
	}
	dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, standardChildren[0], nil, nil), "caption-track-7")
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, hlsHTTPWithoutToken(t, firstChild), nil, h.accounts.second.headers), http.StatusNotFound)
	d.endFirst.Do(func() { close(d.firstEOF) })
	second := d.waitWindow(t, p, func(w dynamicHTTPWindow) bool {
		return w.LiveEdgeTicks >= 20*media.TicksPerSecond && d.mediaRequests.Load() >= 2
	})
	if second.PresentationID != first.PresentationID {
		t.Fatal("subtitle reconnect replaced the buffered presentation")
	}
	again := h.request(t, http.MethodGet, p.masterURL, nil, nil)
	expectHLSHTTPStatus(t, again, http.StatusOK)
	reconnectedTracks := dynamicHTTPSubtitleRenditions(t, again.body)
	if len(reconnectedTracks) != len(tracks) {
		t.Fatal("subtitle reconnect changed the fixed track set")
	}
	for index := range tracks {
		if reconnectedTracks[index] != tracks[index] {
			t.Fatal("a reconnect changed the configured subtitle track identity")
		}
	}
	reconnected := h.request(t, http.MethodGet, tracks[7], nil, nil)
	expectHLSHTTPStatus(t, reconnected, http.StatusOK)
	children := hlsHTTPManifestChildren(reconnected.body)
	if len(children) == 0 {
		t.Fatal("new epoch has no retained subtitle segments")
	}
	dynamicHTTPSubtitleDocument(t, h.request(t, http.MethodGet, children[len(children)-1], nil, nil), "caption-track-7")
	for index := range d.subtitleRequests {
		if d.subtitleRequests[index].Load() != 2 {
			t.Fatal("new source generation did not independently reauthorize its external sidecar")
		}
	}
	if d.mediaRequests.Load() != 2 || d.badAuthorization.Load() {
		t.Fatal("view changes restarted media or bypassed configured source authentication")
	}
	// Current catalog policy applies to already retained media and captions.
	h.policy(t, false)
	denied := h.request(t, http.MethodGet, children[len(children)-1], nil, nil)
	if denied.status != http.StatusForbidden && denied.status != http.StatusNotFound {
		t.Fatal("revoked catalog access retained caption delivery")
	}
	var object map[string]any
	if json.Unmarshal(denied.body, &object) != nil {
		t.Fatal("revoked caption access did not return a structured error")
	}
}
