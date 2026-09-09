//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// This fixture verifies transport and state persistence with deterministic
// original bytes. The current probe version and ctime are real file snapshots;
// the ten-minute duration and stream facts do not require real-time playback.
type playbackHTTPProber struct{}

func (playbackHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }

func (playbackHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	return media.Info{
		ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: media.FileChangeTime(stat),
		Container: "mov,mp4", Size: stat.Size(), DurationTicks: 600 * media.TicksPerSecond, Bitrate: 4_000_000,
		Streams: []media.Stream{
			{Index: 2, Codec: "h264", CodecType: "video", Width: 160, Height: 90, BitDepth: 8, Profile: "High", Level: 41, AverageFrameRate: "30/1", InterlaceKnown: true},
			{Index: 5, Codec: "aac", CodecType: "audio", Channels: 2, SampleRate: 48000, Bitrate: 192000, IsDefault: true},
		},
	}, nil
}

type playbackHTTPFixture struct {
	s             *streamHTTPFixture
	authSessionID string
	secondItemID  string
	headers       http.Header
}

func newPlaybackHTTPFixture(t *testing.T) *playbackHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close default playback catalog: %v", err)
	}
	root := t.TempDir()
	catalog, err := library.New(f.pool, playbackHTTPProber{}, []string{root})
	if err != nil {
		t.Fatalf("create current playback catalog: %v", err)
	}
	f.app.library = catalog
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close playback catalog: %v", err)
		}
	})
	s := &streamHTTPFixture{f: f, root: root, adminID: f.bootstrap(t)}
	s.cookie, _ = f.adminLogin(t)
	data := bytes.Repeat([]byte("Goby original playback fixture bytes\n"), 32)
	s.video = s.addItem(t, "Playback.Feature.mp4", "movies", "Videos", "mp4", "video/mp4", data)
	secondPath := writeAPIMediaFile(t, root, "movies/Second.Feature.mp4")
	s.rescan(t, s.video.libraryID)
	items, err := catalog.QueryItems(f.ctx, library.Query{UserID: s.adminID, ParentID: s.video.libraryID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("query playback fixture items: %v", err)
	}
	fixture := &playbackHTTPFixture{s: s}
	for _, item := range items.Items {
		if item.Path == secondPath && !item.IsFolder {
			fixture.secondItemID = item.ID
		}
	}
	if fixture.secondItemID == "" {
		t.Fatal("second playback fixture item was not scanned")
	}
	user, err := f.users.CreateUser(f.ctx, "Playback Viewer", "playback-viewer-password", false)
	if err != nil {
		t.Fatalf("create playback viewer: %v", err)
	}
	s.viewerID = user.ID
	s.setPolicy(t, user.ID, true, []string{s.video.libraryID})
	login := f.embyLogin(t, user.Name, "playback-viewer-password")
	s.token = stringValue(t, login, "AccessToken")
	fixture.authSessionID = stringValue(t, objectValue(t, login, "SessionInfo"), "Id")
	fixture.headers = http.Header{"X-Emby-Token": {s.token}}
	s.server = httptest.NewServer(f.handler)
	t.Cleanup(s.server.Close)
	s.client = s.server.Client()
	s.client.Timeout = 15 * time.Second
	return fixture
}

func matchingPlaybackHTTPBody() map[string]any {
	return map[string]any{"DeviceProfile": map[string]any{
		"DirectPlayProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}},
	}}
}

func playbackHTTPSource(t *testing.T, response *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()
	// Negotiation responses contain credential-bearing URLs. Report only status
	// and structure on failure, never a whole response body or generated URL.
	if response.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo status = %d, want 200", response.Code)
	}
	var object map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
		t.Fatalf("decode PlaybackInfo JSON: %T", err)
	}
	sources, ok := object["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("PlaybackInfo must contain exactly one original media source")
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatal("PlaybackInfo media source must be an object")
	}
	return object, source
}

func (p *playbackHTTPFixture) prepare(t *testing.T, body map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	return playbackHTTPSource(t, p.s.f.request(t, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo", body, p.headers))
}

func playbackHTTPFlags(t *testing.T, source map[string]any, directPlay, directStream bool) {
	t.Helper()
	for key, want := range map[string]bool{"SupportsDirectPlay": directPlay, "SupportsDirectStream": directStream, "SupportsTranscoding": false} {
		if source[key] != want {
			t.Errorf("%s = %v, want %v", key, source[key], want)
		}
	}
	if _, exists := source["TranscodingUrl"]; exists {
		t.Error("unimplemented transcoding must not advertise a URL")
	}
}

func (p *playbackHTTPFixture) detailData(t *testing.T, itemID string) map[string]any {
	t.Helper()
	response := p.s.f.request(t, http.MethodGet, "/emby/Users/"+p.s.viewerID+"/Items/"+itemID, nil, p.headers)
	expectStatus(t, response, http.StatusOK)
	return objectValue(t, jsonObject(t, response), "UserData")
}

func assertPlaybackHTTPData(t *testing.T, data map[string]any, position int64, count int, played, favorite bool) {
	t.Helper()
	if data["PlaybackPositionTicks"] != float64(position) || data["PlayCount"] != float64(count) || data["Played"] != played || data["IsFavorite"] != favorite {
		t.Errorf("user state = %#v, want position=%d count=%d played=%t favorite=%t", data, position, count, played, favorite)
	}
}

func (p *playbackHTTPFixture) report(t *testing.T, event, playSessionID string, position int64) {
	t.Helper()
	body := map[string]any{
		"PlaySessionId": playSessionID, "ItemId": p.s.video.id, "MediaSourceId": media.SourceID(p.s.video.id),
		"SessionId": p.authSessionID, "PositionTicks": position,
		// These client hints are deliberately false: only indexed duration may
		// determine resume and completion, and extra report fields are accepted.
		"RunTimeTicks": 1, "PlaybackStartTimeTicks": 999, "PlayMethod": "DirectStream", "EventName": "TimeUpdate",
	}
	path := "/emby/Sessions/Playing"
	if event != "Started" {
		path += "/" + event
	}
	response := p.s.f.request(t, http.MethodPost, path, body, p.headers)
	expectStatus(t, response, http.StatusNoContent)
	if response.Body.Len() != 0 {
		t.Error("successful playback report must have an empty response")
	}
}

func TestHTTPPlaybackNegotiationOriginalDeliveryAndResumeLifecycle(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	object, source := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, object, "PlaySessionId")
	if playID == p.authSessionID || !strings.HasPrefix(playID, "play_") || source["Id"] != media.SourceID(p.s.video.id) {
		t.Fatal("playback and original-source identifiers are not independent of authentication")
	}
	playbackHTTPFlags(t, source, true, true)
	if _, exists := object["ErrorCode"]; exists {
		t.Error("matching playback profile returned an ErrorCode")
	}
	if source["RunTimeTicks"] != float64(600*media.TicksPerSecond) || source["Size"] != float64(len(p.s.video.data)) ||
		source["DefaultAudioStreamIndex"] != float64(5) || source["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Error("PlaybackInfo changed authoritative media facts or actual track indices")
	}
	streamURL, err := url.Parse(stringValue(t, source, "DirectStreamUrl"))
	if err != nil {
		t.Fatalf("parse original playback URL: %T", err)
	}
	if streamURL.IsAbs() || streamURL.Host != "" || streamURL.User != nil || streamURL.Fragment != "" ||
		streamURL.Path != "/videos/"+p.s.video.id+"/original.mp4" || strings.Contains(streamURL.String(), p.s.root) {
		t.Fatal("original playback URL is not a relative item route")
	}
	query := streamURL.Query()
	if query.Get("DeviceId") != "integration-device" || query.Get("MediaSourceId") != media.SourceID(p.s.video.id) ||
		query.Get("PlaySessionId") != playID || query.Get("api_key") != p.s.token || query.Get("Path") != "" {
		t.Fatal("original playback URL is missing its authenticated device/source/play-session binding")
	}
	stream := p.s.request(t, http.MethodGet, streamURL.String(), "", nil, nil)
	expectStreamStatus(t, stream, http.StatusOK)
	if !bytes.Equal(stream.body, p.s.video.data) || stream.header.Get("Content-Type") != "video/mp4" {
		t.Error("negotiated URL did not serve the original file bytes")
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
	current := matchingPlaybackHTTPBody()
	current["CurrentPlaySessionId"] = playID
	reused, _ := p.prepare(t, current)
	if stringValue(t, reused, "PlaySessionId") != playID {
		t.Error("explicit live CurrentPlaySessionId was not reused")
	}
	p.report(t, "Started", playID, 0)
	p.report(t, "Started", playID, 400*media.TicksPerSecond)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 1, false, false)
	position := int64(120 * media.TicksPerSecond)
	p.report(t, "Progress", playID, position)
	data := p.detailData(t, p.s.video.id)
	assertPlaybackHTTPData(t, data, position, 1, false, false)
	if _, err := time.Parse(time.RFC3339Nano, stringValue(t, data, "LastPlayedDate")); err != nil {
		t.Errorf("playback did not persist a valid last-played date: %v", err)
	}
	resumePath := "/emby/Users/" + p.s.viewerID + "/Items/Resume"
	resumed, total := responseItems(t, p.s.f.request(t, http.MethodGet, resumePath, nil, p.headers))
	if total != 1 || len(resumed) != 1 || resumed[0]["Id"] != p.s.video.id {
		t.Fatalf("120 seconds of a server-side 600-second item was not resumable: %#v", resumed)
	}
	assertPlaybackHTTPData(t, objectValue(t, resumed[0], "UserData"), position, 1, false, false)
	expectStatus(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+playID, nil, p.headers), http.StatusNoContent)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), position, 1, false, false)
	p.report(t, "Stopped", playID, position)
	p.report(t, "Stopped", playID, 590*media.TicksPerSecond)
	p.report(t, "Progress", playID, 599*media.TicksPerSecond)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), position, 1, false, false)
	beforeRead := readPlaybackHTTPState(t, p, playID)
	if beforeRead.State != "Stopped" {
		t.Fatalf("completed stop report left session state %q", beforeRead.State)
	}
	// PlaySessionId correlates original-file reads with playback activity; it
	// does not replace token/source authorization or revive terminal state.
	afterStop := p.s.request(t, http.MethodGet, streamURL.String(), "", nil, nil)
	expectStreamStatus(t, afterStop, http.StatusOK)
	if !bytes.Equal(afterStop.body, p.s.video.data) {
		t.Error("valid original URL stopped serving its source bytes after a stop report")
	}
	if afterRead := readPlaybackHTTPState(t, p, playID); !reflect.DeepEqual(beforeRead, afterRead) {
		t.Errorf("original-file read changed terminal playback or user state: before=%+v after=%+v", beforeRead, afterRead)
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), position, 1, false, false)
	expectAPIError(t, p.s.f.request(t, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo", current, p.headers), http.StatusNotFound, "not_found", true)
	restarted, _ := p.prepare(t, matchingPlaybackHTTPBody())
	nextID := stringValue(t, restarted, "PlaySessionId")
	if nextID == playID {
		t.Error("preparing after stop revived the terminal play session")
	}
	var preparedPosition, duration int64
	var state string
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, "SELECT state, position_ticks, duration_ticks FROM play_sessions WHERE id = $1", nextID).Scan(&state, &preparedPosition, &duration); err != nil {
		t.Fatalf("read prepared resume state: %v", err)
	}
	if state != "Prepared" || preparedPosition != position || duration != 600*media.TicksPerSecond {
		t.Errorf("resume preparation used client duration or lost persisted position: state=%s position=%d duration=%d", state, preparedPosition, duration)
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), position, 1, false, false)
}

func TestHTTPPlaybackInfoFlagsFallbackAndBindingValidation(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	path := "/emby/Items/" + p.s.video.id + "/PlaybackInfo"
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var body any
		if method == http.MethodPost {
			body = map[string]any{}
		}
		object, source := playbackHTTPSource(t, p.s.f.request(t, method, path, body, p.headers))
		playbackHTTPFlags(t, source, true, true)
		if _, exists := source["DirectStreamUrl"]; exists {
			t.Error("minimal source-fact response unexpectedly included a playback URL")
		}
		if _, exists := object["ErrorCode"]; exists {
			t.Error("minimal source-fact response returned an ErrorCode")
		}
	}
	for _, test := range []struct {
		name                          string
		transcode, allDisabled, limit bool
		wantPlayable                  bool
	}{
		{"explicit original fallback", false, false, false, true},
		{"transcoding unavailable", true, false, false, false},
		{"fallback cannot bypass bitrate limit", false, false, true, false},
		{"all delivery explicitly disabled", false, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := map[string]any{
				"EnableTranscoding": test.transcode, "IsPlayback": true,
				"DeviceProfile": map[string]any{"DirectPlayProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "VideoCodec": "hevc", "AudioCodec": "aac"}}},
			}
			if test.limit {
				body["MaxStreamingBitrate"] = 1
			}
			if test.allDisabled {
				body["EnableDirectPlay"], body["EnableDirectStream"] = false, false
			}
			object, source := p.prepare(t, body)
			playbackHTTPFlags(t, source, test.wantPlayable, test.wantPlayable)
			_, hasURL := source["DirectStreamUrl"]
			if hasURL != test.wantPlayable {
				t.Error("negotiation advertised a URL inconsistent with its delivery flags")
			}
			if test.wantPlayable || test.allDisabled {
				if _, exists := object["ErrorCode"]; exists {
					t.Error("explicit fallback or all-disabled request should not return ErrorCode")
				}
			} else if object["ErrorCode"] != "NoCompatibleStream" {
				t.Errorf("unavailable playback ErrorCode = %v, want NoCompatibleStream", object["ErrorCode"])
			}
		})
	}
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {"invalid-token"}}} {
		expectEmbyTextError(t, p.s.f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized, "Access token is invalid or expired.")
	}
	for _, test := range []struct {
		name, query, code string
		body              map[string]any
		status            int
	}{
		{"other user", "", "access_denied", map[string]any{"UserId": p.s.adminID}, 403},
		{"other user query", "?UserId=" + p.s.adminID, "access_denied", map[string]any{}, 403},
		{"path body item conflict", "", "invalid_playback_request", map[string]any{"Id": p.secondItemID}, 400},
		{"body query item conflict", "?Id=" + p.secondItemID, "invalid_playback_request", map[string]any{"Id": p.s.video.id}, 400},
		{"body query source conflict", "?MediaSourceId=" + media.SourceID(p.secondItemID), "invalid_playback_request", map[string]any{"MediaSourceId": media.SourceID(p.s.video.id)}, 400},
		{"other device", "?DeviceId=another-device", "device_mismatch", map[string]any{}, 403},
		{"query int64 overflow", "?StartTimeTicks=9223372036854775808", "invalid_playback_request", map[string]any{}, 400},
		{"body int64 overflow", "", "invalid_json", map[string]any{"StartTimeTicks": json.Number("9223372036854775808")}, 400},
		{"stream index not array position", "", "invalid_playback_request", map[string]any{"AudioStreamIndex": 1}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectAPIError(t, p.s.f.request(t, http.MethodPost, path+test.query, test.body, p.headers), test.status, test.code, true)
		})
	}
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	otherLogin := p.s.f.embyLogin(t, "Playback Viewer", "playback-viewer-password")
	otherHeaders := http.Header{"X-Emby-Token": {stringValue(t, otherLogin, "AccessToken")}}
	expectAPIError(t, p.s.f.request(t, http.MethodPost, path, map[string]any{"CurrentPlaySessionId": playID}, otherHeaders), http.StatusNotFound, "not_found", true)
	foreign, err := p.s.f.users.CreateUser(p.s.f.ctx, "Foreign Playback Viewer", "foreign-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	foreignLogin := p.s.f.embyLogin(t, foreign.Name, "foreign-viewer-password")
	foreignHeaders := http.Header{"X-Emby-Token": {stringValue(t, foreignLogin, "AccessToken")}}
	foreignPrepared, _ := playbackHTTPSource(t, p.s.f.request(t, http.MethodPost, path, matchingPlaybackHTTPBody(), foreignHeaders))
	expectAPIError(t, p.s.f.request(t, http.MethodPost, path, map[string]any{"CurrentPlaySessionId": stringValue(t, foreignPrepared, "PlaySessionId")}, p.headers), http.StatusNotFound, "not_found", true)
	expectAPIError(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing", map[string]any{"PlaySessionId": playID, "SessionId": playID, "PositionTicks": 0}, p.headers), http.StatusForbidden, "session_mismatch", true)
	expectAPIError(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", map[string]any{"PlaySessionId": playID, "PositionTicks": json.Number("9223372036854775808")}, p.headers), http.StatusBadRequest, "invalid_json", true)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
}

func TestHTTPPlaybackUserFlagsFiltersAndLegacyReports(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	itemPath := "/emby/Users/" + p.s.viewerID + "/Items/" + p.s.video.id
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
	for _, path := range []string{itemPath + "?EnableUserData=false", "/emby/Items?Ids=" + p.s.video.id + "&EnableUserData=false"} {
		response := p.s.f.request(t, http.MethodGet, path, nil, p.headers)
		var item map[string]any
		if strings.HasPrefix(path, "/emby/Items?") {
			items, total := responseItems(t, response)
			if total != 1 || len(items) != 1 {
				t.Fatal("user-data projection switch changed item membership")
			}
			item = items[0]
		} else {
			expectStatus(t, response, http.StatusOK)
			item = jsonObject(t, response)
		}
		if _, exists := item["UserData"]; exists {
			t.Error("EnableUserData=false retained inline state")
		}
	}
	flags := "/emby/Users/" + p.s.viewerID
	favoritePath, playedPath := flags+"/FavoriteItems/"+p.s.video.id, flags+"/PlayedItems/"+p.s.video.id
	applyFlag := func(method, path string) map[string]any {
		response := p.s.f.request(t, method, path, nil, p.headers)
		expectStatus(t, response, http.StatusOK)
		return jsonObject(t, response)
	}
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, favoritePath), 0, 0, false, true)
	queryItems := func(query string, wantID string) {
		t.Helper()
		items, total := responseItems(t, p.s.f.request(t, http.MethodGet,
			"/emby/Items?ParentId="+p.s.video.libraryID+"&Recursive=true&IncludeItemTypes=Movie&"+query, nil, p.headers))
		if total != 1 || len(items) != 1 || items[0]["Id"] != wantID {
			t.Errorf("user state filter %s returned wrong membership: total=%d items=%d", query, total, len(items))
		}
	}
	queryItems("IsFavorite=true", p.s.video.id)
	queryItems("IsFavorite=false", p.secondItemID)
	queryItems("Filters=IsFavorite,IsUnplayed", p.s.video.id)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, playedPath), 0, 1, true, true)
	queryItems("Filters=IsFavorite,IsPlayed", p.s.video.id)
	queryItems("IsPlayed=false", p.secondItemID)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, playedPath), 0, 1, true, true)
	assertPlaybackHTTPData(t, applyFlag(http.MethodDelete, playedPath), 0, 0, false, true)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, playedPath), 0, 1, true, true)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, playedPath+"/Delete"), 0, 0, false, true)
	assertPlaybackHTTPData(t, applyFlag(http.MethodDelete, favoritePath), 0, 0, false, false)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, favoritePath), 0, 0, false, true)
	assertPlaybackHTTPData(t, applyFlag(http.MethodPost, favoritePath+"/Delete"), 0, 0, false, false)
	expectAPIError(t, p.s.f.request(t, http.MethodPost, "/emby/Users/"+p.s.adminID+"/FavoriteItems/"+p.s.video.id, nil, p.headers), http.StatusForbidden, "access_denied", true)
	// Legacy reporting omits the playback ID but remains bound to the same
	// authenticated user, authentication session, device, item, and source.
	expectAPIError(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", map[string]any{"ItemId": p.s.video.id, "PositionTicks": 1}, p.headers), http.StatusNotFound, "not_found", true)
	p.report(t, "Started", "", 0)
	p.report(t, "Progress", "", 120*media.TicksPerSecond)
	p.report(t, "Stopped", "", 120*media.TicksPerSecond)
	p.report(t, "Stopped", "", 590*media.TicksPerSecond)
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 120*media.TicksPerSecond, 1, false, false)
}

func TestHTTPPlaybackFolderStateUsesRecursivePlayableDescendants(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	for _, name := range []string{
		"tv/Example Show/Season 01/Example.Show.S01E01.mp4",
		"tv/Example Show/Season 01/Example.Show.S01E02.mp4",
		"tv/Example Show/Season 02/Example.Show.S02E01.mp4",
	} {
		writeAPIMediaFile(t, p.s.root, name)
	}
	collection, err := p.s.f.app.library.CreateLibrary(p.s.f.ctx, "Playback television", "tvshows", []string{filepath.Join(p.s.root, "tv")})
	if err != nil {
		t.Fatalf("create playback television library: %v", err)
	}
	p.s.rescan(t, collection.ID)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID, collection.ID})
	series, total := responseItems(t, p.s.f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+collection.ID+"&Recursive=true&IncludeItemTypes=Series", nil, p.headers))
	if total != 1 || len(series) != 1 {
		t.Fatal("television playback fixture did not contain exactly one series")
	}
	seriesID := stringValue(t, series[0], "Id")
	assertFolder := func(itemID string, unplayed int, played, favorite bool) {
		t.Helper()
		data := p.detailData(t, itemID)
		assertPlaybackHTTPData(t, data, 0, 0, played, favorite)
		if data["UnplayedItemCount"] != float64(unplayed) {
			t.Errorf("folder recursive unplayed count=%v, want %d", data["UnplayedItemCount"], unplayed)
		}
		if _, exists := data["LastPlayedDate"]; exists {
			t.Error("derived folder state must not invent a last-played date")
		}
	}
	assertFolder(seriesID, 3, false, false)
	seasons, total := responseItems(t, p.s.f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Seasons", nil, p.headers))
	if total != 2 || len(seasons) != 2 {
		t.Fatal("television playback fixture did not contain both seasons")
	}
	assertFolder(stringValue(t, seasons[0], "Id"), 2, false, false)
	assertFolder(stringValue(t, seasons[1], "Id"), 1, false, false)
	base := "/emby/Users/" + p.s.viewerID
	marked := p.s.f.request(t, http.MethodPost, base+"/PlayedItems/"+seriesID, nil, p.headers)
	expectStatus(t, marked, http.StatusOK)
	assertFolder(seriesID, 0, true, false)
	for _, season := range seasons {
		assertFolder(stringValue(t, season, "Id"), 0, true, false)
	}
	favorite := p.s.f.request(t, http.MethodPost, base+"/FavoriteItems/"+seriesID, nil, p.headers)
	expectStatus(t, favorite, http.StatusOK)
	assertFolder(seriesID, 0, true, true)
	episodeQuery := "/emby/Items?ParentId=" + seriesID + "&Recursive=true&IncludeItemTypes=Episode"
	episodes, total := responseItems(t, p.s.f.request(t, http.MethodGet, episodeQuery+"&IsPlayed=true&IsFavorite=false", nil, p.headers))
	if total != 3 || len(episodes) != 3 {
		t.Error("recursive played mutation or nonrecursive favorite mutation affected the wrong descendants")
	}
	for _, episode := range episodes {
		assertPlaybackHTTPData(t, objectValue(t, episode, "UserData"), 0, 1, true, false)
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
	cleared := p.s.f.request(t, http.MethodPost, base+"/PlayedItems/"+seriesID+"/Delete", nil, p.headers)
	expectStatus(t, cleared, http.StatusOK)
	assertFolder(seriesID, 3, false, true)
	assertFolder(stringValue(t, seasons[0], "Id"), 2, false, false)
	assertFolder(stringValue(t, seasons[1], "Id"), 1, false, false)
	episodes, total = responseItems(t, p.s.f.request(t, http.MethodGet, episodeQuery+"&Filters=IsUnplayed", nil, p.headers))
	if total != 3 || len(episodes) != 3 {
		t.Error("clearing recursive played state left played descendants")
	}
	for _, episode := range episodes {
		assertPlaybackHTTPData(t, objectValue(t, episode, "UserData"), 0, 0, false, false)
	}
}

type playbackHTTPPersistedState struct {
	Position, Duration, SessionPosition int64
	Count                               int
	Favorite, Played                    bool
	State                               string
	Updated, DataUpdated                time.Time
}

func readPlaybackHTTPState(t *testing.T, p *playbackHTTPFixture, playID string) playbackHTTPPersistedState {
	t.Helper()
	var value playbackHTTPPersistedState
	err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT data.playback_position_ticks, data.play_count, data.is_favorite, data.played,
		play.state, play.position_ticks, play.duration_ticks, play.updated_at, data.updated_at
		FROM user_item_data data JOIN play_sessions play ON play.user_id = data.user_id AND play.item_id = data.item_id
		WHERE data.user_id = $1 AND data.item_id = $2 AND play.id = $3`, p.s.viewerID, p.s.video.id, playID).
		Scan(&value.Position, &value.Count, &value.Favorite, &value.Played, &value.State, &value.SessionPosition, &value.Duration, &value.Updated, &value.DataUpdated)
	if err != nil {
		t.Fatalf("read persisted playback state: %v", err)
	}
	return value
}

func TestHTTPPlaybackRevocationPreventsNewStateWrites(t *testing.T) {
	for _, kind := range []string{"account disabled", "token revoked", "playback disabled", "library access revoked"} {
		t.Run(kind, func(t *testing.T) {
			p := newPlaybackHTTPFixture(t)
			prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
			playID := stringValue(t, prepared, "PlaySessionId")
			p.report(t, "Started", playID, 0)
			p.report(t, "Progress", playID, 120*media.TicksPerSecond)
			before := readPlaybackHTTPState(t, p, playID)
			status := http.StatusUnauthorized
			switch kind {
			case "account disabled":
				if _, err := p.s.f.pool.Exec(p.s.f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", p.s.viewerID); err != nil {
					t.Fatal(err)
				}
			case "token revoked":
				expectStatus(t, p.s.f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, p.headers), http.StatusOK)
			case "playback disabled":
				p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID})
				status = http.StatusForbidden
			case "library access revoked":
				p.s.setPolicy(t, p.s.viewerID, true, []string{})
				status = http.StatusNotFound
			}
			for _, path := range []string{"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"} {
				response := p.s.f.request(t, http.MethodPost, path, map[string]any{"PlaySessionId": playID, "PositionTicks": 590 * media.TicksPerSecond}, p.headers)
				expectStatus(t, response, status)
			}
			response := p.s.f.request(t, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo", matchingPlaybackHTTPBody(), p.headers)
			if response.Code != status {
				t.Errorf("revoked negotiation status=%d, want %d", response.Code, status)
			}
			if status == http.StatusUnauthorized {
				for _, action := range []string{"FavoriteItems", "PlayedItems"} {
					expectStatus(t, p.s.f.request(t, http.MethodPost, "/emby/Users/"+p.s.viewerID+"/"+action+"/"+p.s.video.id, nil, p.headers), status)
				}
			}
			after := readPlaybackHTTPState(t, p, playID)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("revoked request changed persistent playback state: before=%+v after=%+v", before, after)
			}
		})
	}
}
