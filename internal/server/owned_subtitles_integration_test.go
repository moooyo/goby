//go:build linux

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

// These fixtures represent an already reviewed and published OCR result. The
// operation worker and review transaction have their own persistence tests;
// HTTP readers must serve these owned bytes without inventing a sidecar file.
func insertOwnedSubtitleHTTPTrack(t *testing.T, f *serverFixture, itemID string, index int, codec, contents string) {
	t.Helper()
	digest := sha256.Sum256([]byte(contents))
	result, err := f.pool.Exec(f.ctx, `INSERT INTO item_owned_subtitles
		(item_id,root_id,stream_index,source_revision,codec,language,title,content,content_sha256)
		SELECT i.id,i.root_id,$2,`+library.MediaOperationSourceRevisionSQL+`,$3,'en','Reviewed OCR',$4,$5
		FROM items i WHERE i.id=$1`, itemID, index, codec, []byte(contents), hex.EncodeToString(digest[:]))
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("insert published owned subtitle fixture failed (%T)", err)
	}
}

func newOwnedSubtitleHTTPFixture(t *testing.T) *subtitleHTTPFixture {
	t.Helper()
	p := newPlaybackHTTPFixture(t)
	insertOwnedSubtitleHTTPTrack(t, p.s.f, p.s.video.id, 6, "srt", subtitleHTTPSRT)
	return &subtitleHTTPFixture{p: p}
}

func assertOwnedSubtitleHTTPDescriptor(t *testing.T, track map[string]any, codec string) {
	t.Helper()
	if track["Protocol"] != "Http" || track["Codec"] != codec || track["Language"] != "en" ||
		track["Title"] != "Reviewed OCR" || track["DisplayTitle"] != "Reviewed OCR ("+strings.ToUpper(codec)+")" {
		t.Error("owned subtitle descriptor lost its HTTP protocol or reviewed metadata")
	}
	if _, exists := track["Path"]; exists {
		t.Error("database-backed subtitle must not advertise a filesystem Path")
	}
	if delivery, ok := track["DeliveryUrl"].(string); !ok {
		t.Error("owned subtitle has no delivery URL")
	} else if parsed, err := url.Parse(delivery); err != nil || len(parsed.Query().Get("GobySubtitleTag")) != 64 {
		t.Error("owned delivery URL does not bind the issued track identity")
	}
}

func TestHTTPOwnedSubtitleIssuedURLCannotSelectAReprobedEmbeddedIndex(t *testing.T) {
	s := newOwnedSubtitleHTTPFixture(t)
	p := s.p
	route := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, s.detail(t), 6), 6, "srt")
	initial := p.s.request(t, http.MethodGet, route, "", nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/plain", subtitleHTTPSRT)
	parsed, err := url.Parse(route)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("GobySubtitleTag", strings.Repeat("0", 64))
	parsed.RawQuery = query.Encode()
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, parsed.String(), "", nil, nil), http.StatusNotFound)
	query.Set("GobySubtitleTag", "malformed")
	parsed.RawQuery = query.Encode()
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, parsed.String(), "", nil, nil), http.StatusBadRequest)
	// A re-probe changes the complete source revision and expands embedded
	// stream indexes. The old issued OCR URL still names the historical track.
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE items SET media=jsonb_set(media,'{Streams}',
		media->'Streams' || '[{"Index":6,"Codec":"srt","CodecType":"subtitle"}]'::jsonb) WHERE id=$1`, p.s.video.id); err != nil {
		t.Fatal("replace embedded stream probe metadata")
	}
	conditional := http.Header{"If-None-Match": {initial.header.Get("ETag")}}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, "", conditional, nil), http.StatusNotFound)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodHead, route, "", conditional, nil), http.StatusNotFound)
	current := subtitleHTTPTrack(t, s.detail(t), 6)
	if current["IsExternal"] == true {
		t.Fatal("re-probed embedded subtitle inherited an owned descriptor")
	}
	if delivery, ok := current["DeliveryUrl"].(string); ok && strings.Contains(delivery, "GobySubtitleTag") {
		t.Fatal("new embedded stream inherited a historical owned URL tag")
	}
}

func assertOwnedSubtitleHTTPAbsent(t *testing.T, object map[string]any, index int) {
	t.Helper()
	streams, ok := object["MediaStreams"].([]any)
	if !ok {
		t.Fatal("item must expose its remaining media streams")
	}
	for _, value := range streams {
		if stream, ok := value.(map[string]any); ok && stream["Index"] == float64(index) {
			t.Error("retired or source-stale owned subtitle remained in the public descriptor")
		}
	}
}

func TestHTTPOwnedSubtitleDescriptorsDeliveryAndFormatNegotiation(t *testing.T) {
	s := newOwnedSubtitleHTTPFixture(t)
	p := s.p
	insertOwnedSubtitleHTTPTrack(t, p.s.f, p.s.video.id, 7, "vtt", subtitleHTTPNativeVTT)
	var sidecars int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT count(*) FROM item_subtitles WHERE item_id=$1`, p.s.video.id).Scan(&sidecars); err != nil || sidecars != 0 {
		t.Fatal("owned HTTP fixture must not contain indexed sidecar files")
	}
	list := subtitleHTTPObject(t, p.s.f.request(t, http.MethodGet,
		"/emby/Items?Ids="+p.s.video.id+"&Fields=MediaStreams,MediaSources", nil, p.headers))
	items, ok := list["Items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal("owned subtitle list query must return its visible source item")
	}
	listed, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("owned subtitle list item must be an object")
	}
	for _, item := range []map[string]any{s.detail(t), listed} {
		for _, descriptor := range []map[string]any{item, subtitleHTTPSource(t, item)} {
			for _, track := range []struct {
				index                    int
				codec, contentType, body string
			}{{6, "srt", "text/plain", subtitleHTTPSRT}, {7, "vtt", "text/vtt", subtitleHTTPNativeVTT}} {
				projected := subtitleHTTPTrack(t, descriptor, track.index)
				assertOwnedSubtitleHTTPDescriptor(t, projected, track.codec)
				route := subtitleHTTPDeliveryURL(t, s, projected, track.index, track.codec)
				get := p.s.request(t, http.MethodGet, route, "", nil, nil)
				expectSubtitleHTTPBody(t, get, track.contentType, track.body)
				if get.header.Get("ETag") == "" || get.header.Get("Cache-Control") != "private, no-cache, no-transform" {
					t.Error("owned subtitles must expose a private revalidated representation")
				}
				head := p.s.request(t, http.MethodHead, route, "", nil, nil)
				expectSubtitleHTTPStatus(t, head, http.StatusOK)
				if len(head.body) != 0 || head.header.Get("Content-Type") != get.header.Get("Content-Type") ||
					head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("ETag") != get.header.Get("ETag") {
					t.Error("owned subtitle HEAD did not retain GET metadata without its bytes")
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					cached := p.s.request(t, method, route, "", http.Header{"If-None-Match": {get.header.Get("ETag")}}, nil)
					expectSubtitleHTTPStatus(t, cached, http.StatusNotModified)
					if len(cached.body) != 0 {
						t.Error("conditional owned subtitle response contained a body")
					}
				}
			}
		}
	}

	request := matchingPlaybackHTTPBody()
	request["SubtitleStreamIndex"] = 6
	request["DeviceProfile"].(map[string]any)["SubtitleProfiles"] = []map[string]any{{"Format": "vtt", "Method": "External"}}
	info, source := p.prepare(t, request)
	playbackHTTPFlags(t, source, true, true)
	if _, exists := info["ErrorCode"]; exists {
		t.Error("reviewed owned SRT selection failed external WebVTT negotiation")
	}
	if _, exists := source["DefaultSubtitleStreamIndex"]; exists {
		t.Error("owned external delivery must not advertise an embedded default stream index")
	}
	track := subtitleHTTPTrack(t, source, 6)
	assertOwnedSubtitleHTTPDescriptor(t, track, "srt")
	route := subtitleHTTPDeliveryURL(t, s, track, 6, "vtt")
	converted := p.s.request(t, http.MethodGet, route, "", nil, nil)
	expectSubtitleHTTPBody(t, converted, "text/vtt", subtitleHTTPConvertedVTT)
	conditional := http.Header{"If-None-Match": {converted.header.Get("ETag")}}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodHead, route, "", conditional, nil), http.StatusNotModified)
	// Negotiation changes the requested representation, never the catalog codec.
	for _, descriptor := range []map[string]any{s.detail(t), subtitleHTTPSource(t, s.detail(t))} {
		subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, descriptor, 6), 6, "srt")
	}
}

func TestHTTPOwnedSubtitleAuthorityPrecedesConditionalResponses(t *testing.T) {
	s := newOwnedSubtitleHTTPFixture(t)
	p := s.p
	route := subtitleHTTPRoute(s, 6, "0", "vtt")
	initial := p.s.request(t, http.MethodGet, route, p.s.token, nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/vtt", subtitleHTTPConvertedVTT)
	conditional := http.Header{"If-None-Match": {initial.header.Get("ETag")}}
	for _, token := range []string{"", "invalid-owned-subtitle-token"} {
		expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, token, conditional, nil), http.StatusUnauthorized)
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, "", conditional, p.s.cookie), http.StatusUnauthorized)
	wrongSource := strings.Replace(route, "/"+media.SourceID(p.s.video.id)+"/", "/unrelated-source/", 1)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, wrongSource, p.s.token, conditional, nil), http.StatusNotFound)
	p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusForbidden)
	p.s.setPolicy(t, p.s.viewerID, true, []string{})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotFound)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotModified)
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, p.authSessionID); err != nil {
		t.Fatal("revoke owned subtitle fixture authentication session")
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodHead, route, p.s.token, conditional, nil), http.StatusUnauthorized)
}

func TestHTTPOwnedSubtitleCacheRechecksContentAndOriginalSource(t *testing.T) {
	s := newOwnedSubtitleHTTPFixture(t)
	p := s.p
	route := subtitleHTTPRoute(s, 6, "0", "srt")
	initial := p.s.request(t, http.MethodGet, route, p.s.token, nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/plain", subtitleHTTPSRT)
	conditional := http.Header{"If-None-Match": {initial.header.Get("ETag")}}
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE item_owned_subtitles SET content=$2 WHERE item_id=$1 AND stream_index=6`,
		p.s.video.id, []byte(strings.Replace(subtitleHTTPSRT, "Before start", "Tampered OCR", 1))); err != nil {
		t.Fatal("change owned subtitle fixture bytes")
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusServiceUnavailable)
	if _, err := p.s.f.pool.Exec(p.s.f.ctx, `UPDATE item_owned_subtitles SET content=$2 WHERE item_id=$1 AND stream_index=6`, p.s.video.id, []byte(subtitleHTTPSRT)); err != nil {
		t.Fatal("restore owned subtitle fixture bytes")
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotModified)
	if err := os.WriteFile(p.s.video.path, append(bytes.Clone(p.s.video.data), '\n'), 0o600); err != nil {
		t.Fatalf("replace original media fixture bytes failed (%T)", err)
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusServiceUnavailable)
	p.s.rescan(t, p.s.video.libraryID)
	// OCR text is bound to the original source revision. A successful rescan
	// cannot apply that old derivative to newly indexed media with the same ID.
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotFound)
	detail := s.detail(t)
	assertOwnedSubtitleHTTPAbsent(t, detail, 6)
	assertOwnedSubtitleHTTPAbsent(t, subtitleHTTPSource(t, detail), 6)
	insertOwnedSubtitleHTTPTrack(t, p.s.f, p.s.video.id, 7, "srt", subtitleHTTPSRT)
	newRoute := subtitleHTTPDeliveryURL(t, s, subtitleHTTPTrack(t, s.detail(t), 7), 7, "srt")
	expectSubtitleHTTPBody(t, p.s.request(t, http.MethodGet, newRoute, "", nil, nil), "text/plain", subtitleHTTPSRT)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodHead, route, p.s.token, conditional, nil), http.StatusNotFound)
}

func TestHTTPOwnedSubtitleDeletionRetiresURLsAndRememberedSelection(t *testing.T) {
	s := newOwnedSubtitleHTTPFixture(t)
	p := s.p
	var playID string
	negotiate := func(selected *int) map[string]any {
		t.Helper()
		request := matchingPlaybackHTTPBody()
		request["DeviceProfile"].(map[string]any)["SubtitleProfiles"] = []map[string]any{{"Format": "vtt", "Method": "External"}}
		if selected != nil {
			request["SubtitleStreamIndex"] = *selected
		}
		if playID != "" {
			request["CurrentPlaySessionId"] = playID
		}
		info, source := p.prepare(t, request)
		playID = stringValue(t, info, "PlaySessionId")
		return source
	}
	selected := 6
	negotiate(&selected)
	expectStatus(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing", map[string]any{
		"ItemId": p.s.video.id, "MediaSourceId": media.SourceID(p.s.video.id), "PlaySessionId": playID,
		"SessionId": p.authSessionID, "PositionTicks": 0, "AudioStreamIndex": 5, "SubtitleStreamIndex": 6,
	}, p.headers), http.StatusNoContent)
	p.report(t, "Stopped", playID, 120*media.TicksPerSecond)
	playID = ""
	var remembered int
	var stamp string
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT remembered_subtitle_stream_index,remembered_media_stamp
		FROM user_item_data WHERE user_id=$1 AND item_id=$2`, p.s.viewerID, p.s.video.id).Scan(&remembered, &stamp); err != nil || remembered != 6 || len(stamp) != 32 {
		t.Fatal("owned subtitle selection was not remembered with its source identity")
	}
	if _, exists := negotiate(nil)["DefaultSubtitleStreamIndex"]; exists {
		t.Fatal("remembered owned subtitle was not selected for external delivery")
	}
	route := subtitleHTTPRoute(s, 6, "0", "srt")
	initial := p.s.request(t, http.MethodGet, route, p.s.token, nil, nil)
	expectSubtitleHTTPBody(t, initial, "text/plain", subtitleHTTPSRT)
	conditional := http.Header{"If-None-Match": {initial.header.Get("ETag")}}
	management := "/emby/Videos/" + p.s.video.id + "/Subtitles/6"
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodDelete, management, p.s.token, nil, nil), http.StatusForbidden)
	setSubtitleManagementPolicy(t, s, true)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodDelete, management, p.s.token, nil, nil), http.StatusNoContent)
	var active, retired bool
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT active,retired_at IS NOT NULL FROM item_owned_subtitles
		WHERE item_id=$1 AND stream_index=6`, p.s.video.id).Scan(&active, &retired); err != nil || active || !retired {
		t.Fatal("owned deletion did not retain an inactive timestamped identity")
	}
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, route, p.s.token, conditional, nil), http.StatusNotFound)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodDelete, management, p.s.token, nil, nil), http.StatusNotFound)
	if negotiate(nil)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Error("deleting an owned subtitle retained its remembered playback selection")
	}
	detail := s.detail(t)
	assertOwnedSubtitleHTTPAbsent(t, detail, 6)
	assertOwnedSubtitleHTTPAbsent(t, subtitleHTTPSource(t, detail), 6)
	// The second management alias must use the same owned-track retirement path.
	insertOwnedSubtitleHTTPTrack(t, p.s.f, p.s.video.id, 7, "vtt", subtitleHTTPNativeVTT)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodPost, "/emby/Items/"+p.s.video.id+"/Subtitles/7/Delete", p.s.token, nil, nil), http.StatusNoContent)
	expectSubtitleHTTPStatus(t, p.s.request(t, http.MethodGet, subtitleHTTPRoute(s, 7, "0", "vtt"), p.s.token, nil, nil), http.StatusNotFound)
	original, err := os.ReadFile(p.s.video.path)
	if err != nil || !bytes.Equal(original, p.s.video.data) {
		t.Fatal("owned subtitle deletion changed the original media bytes")
	}
	var sidecars int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT count(*) FROM item_subtitles WHERE item_id=$1`, p.s.video.id).Scan(&sidecars); err != nil || sidecars != 0 {
		t.Fatal("owned subtitle deletion created a synthetic sidecar identity")
	}
	p.report(t, "Stopped", playID, 120*media.TicksPerSecond)
}

func TestHTTPHLSOwnedSubtitleWindowsRecheckAuthorityAndRetirement(t *testing.T) {
	h := newHLSHTTPFixture(t)
	const index = 6
	insertOwnedSubtitleHTTPTrack(t, h.f, h.item.ID, index, "srt",
		"1\n00:00:02,500 --> 00:00:03,500\nReviewed cross segment\n\n2\n00:00:07,000 --> 00:00:08,000\nReviewed later\n")
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	var negotiation struct {
		PlayID string `json:"PlaySessionId"`
	}
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlayID == "" {
		t.Fatal("owned subtitle HLS fixture did not prepare playback")
	}
	query := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")}, "PlaySessionId": {negotiation.PlayID},
		"MediaSourceId": {media.SourceID(h.item.ID)}, "DeviceId": {h.accounts.viewer.deviceID},
		"SegmentContainer": {"mp4"}, "VideoCodec": {"h264"}, "AudioCodec": {"aac"}, "SegmentLength": {"3"},
		"AllowVideoStreamCopy": {"false"}, "AllowAudioStreamCopy": {"false"},
		"SubtitleStreamIndex": {strconv.Itoa(index)}, "ManifestSubtitles": {"vtt"}, "SubtitleOffsetTicks": {"0"}}
	master := h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	tracks := hlsSubtitleRenditionURLs(t, master.body)
	if len(tracks) != 1 {
		t.Fatal("HLS master did not expose the single owned subtitle rendition")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodHead, tracks[0], nil, nil), http.StatusOK)
	if videoHTTPJobCount(t, h, negotiation.PlayID, false) != 0 {
		t.Fatal("owned subtitle playlist HEAD started a media producer")
	}
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) != 1 {
		t.Fatal("owned subtitle fixture must share one A/V rendition")
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
			t.Fatal("owned subtitle HLS media exceeded its fixture deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	if !bytes.Contains(mediaList.body, []byte("#EXT-X-ENDLIST")) || len(hlsHTTPManifestChildren(mediaList.body)) != 4 {
		t.Fatal("owned subtitle HLS fixture did not close four measured media segments")
	}
	playlist := h.request(t, http.MethodGet, tracks[0], nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	children := hlsHTTPManifestChildren(playlist.body)
	if len(children) != 4 || !bytes.Contains(playlist.body, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("owned subtitle playlist did not use the completed media segment window")
	}
	var cached hlsHTTPResponse
	for number, child := range children {
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		document, err := subtitle.Parse(response.body, subtitle.FormatWebVTT)
		if err != nil {
			t.Fatal("owned HLS subtitle segment was not parseable WebVTT")
		}
		if number < 2 {
			if len(document.Cues) != 1 || document.Cues[0].StartTicks != 25_000_000 || document.Cues[0].EndTicks != 35_000_000 || document.Cues[0].Text != "Reviewed cross segment" {
				t.Errorf("owned cue was clipped or not repeated across measured media segment %d", number)
			}
		} else if number == 2 && (len(document.Cues) != 1 || document.Cues[0].Text != "Reviewed later") || number == 3 && len(document.Cues) != 0 {
			t.Error("owned subtitle segment lost its valid cue or empty interval")
		}
		if number == 0 {
			cached = response
		}
	}
	if cached.header.Get("ETag") == "" {
		t.Fatal("owned HLS subtitle representation has no cache identity")
	}
	conditional := http.Header{"If-None-Match": {cached.header.Get("ETag")}}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, children[0], nil, conditional), http.StatusNotModified)
	h.policy(t, false)
	if response := h.request(t, http.MethodGet, children[0], nil, conditional); response.status < 400 {
		t.Fatal("matching owned HLS ETag bypassed revoked playback authority")
	}
	h.policy(t, true)
	// Policy denial may retire the playback session. A fresh negotiation gives
	// deletion its own live revision, independent of the revoked cached graph.
	prepared = h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", map[string]any{}, h.accounts.viewer.headers)
	expectHLSHTTPStatus(t, prepared, http.StatusOK)
	negotiation.PlayID = ""
	if json.Unmarshal(prepared.body, &negotiation) != nil || negotiation.PlayID == "" {
		t.Fatal("owned subtitle HLS fixture did not prepare replacement playback")
	}
	query.Set("PlaySessionId", negotiation.PlayID)
	master = h.request(t, http.MethodGet, "/emby/Videos/"+h.item.ID+"/master.m3u8?"+query.Encode(), nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	tracks = hlsSubtitleRenditionURLs(t, master.body)
	if len(tracks) != 1 {
		t.Fatal("replacement HLS graph lost the current owned subtitle")
	}
	sessions := videoHTTPSessions(h, negotiation.PlayID)
	if len(sessions) != 1 {
		t.Fatal("owned subtitle deletion fixture has no single bound HLS revision")
	}
	if _, err := h.f.pool.Exec(h.f.ctx, `UPDATE users SET policy=jsonb_set(policy,'{EnableSubtitleManagement}','true'::jsonb,true) WHERE id=$1`, h.accounts.admin.userID); err != nil {
		t.Fatal("enable owned subtitle fixture management")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, "/emby/Videos/"+h.item.ID+"/Subtitles/"+strconv.Itoa(index), nil, h.accounts.admin.headers), http.StatusNoContent)
	if response := h.request(t, http.MethodHead, tracks[0], nil, conditional); response.status < 400 {
		t.Fatal("owned subtitle deletion retained a cached bound HLS graph")
	}
	videoHTTPWaitRetired(t, h, negotiation.PlayID, sessions)
}
