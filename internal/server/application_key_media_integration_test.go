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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Transport fixtures provide current source snapshots without requiring a
// decoder. Real encoder output is covered separately by the HLS integration.
type applicationMediaProber struct{}

func (applicationMediaProber) CacheVersion() int { return media.CurrentProbeVersion }

func (applicationMediaProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: media.FileChangeTime(stat),
		Container: "mov,mp4", Size: stat.Size(), DurationTicks: 12 * media.TicksPerSecond, Bitrate: 500_000, FormatStartKnown: true,
		Streams: []media.Stream{
			{Index: 0, Codec: "h264", CodecType: "video", Width: 160, Height: 90, BitDepth: 8, Profile: "High", Level: 41,
				AverageFrameRate: "24/1", InterlaceKnown: true, VideoRangeKnown: true, VideoRange: "SDR"},
			{Index: 1, Codec: "aac", CodecType: "audio", Channels: 2, SampleRate: 48000, Bitrate: 96000, IsDefault: true},
		}}
	if strings.EqualFold(filepath.Ext(file.Name()), ".wav") {
		info.Container, info.Bitrate, info.AudioDurationExact = "wav", 1_536_000, true
		info.Streams = []media.Stream{{Index: 0, Codec: "pcm_s16le", CodecType: "audio", Channels: 2, SampleRate: 48000,
			Bitrate: info.Bitrate, BitDepth: 16, IsDefault: true, AudioTiming: &media.AudioTiming{Exact: true,
				SampleCount: 576000, PacketCount: 563, EndTicks: info.DurationTicks,
				LastPacketStartTicks: info.DurationTicks - 200000, MaxPacketDurationTicks: 220000}}}
	}
	return info, nil
}

type applicationMediaKey struct {
	key       identity.ApplicationKey
	principal identity.Principal
	headers   http.Header
}

func applicationMediaIssueKeys(t *testing.T, f *serverFixture, adminToken string) [2]applicationMediaKey {
	t.Helper()
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key")))
	f.app.identity = f.users
	actor, err := f.users.ResolveEmby(f.ctx, adminToken)
	if err != nil {
		t.Fatalf("resolve key creator (%T)", err)
	}
	var result [2]applicationMediaKey
	for index := range result {
		key, err := f.users.CreateApplicationKey(f.ctx, actor, "Media integration "+strconv.Itoa(index), "127.0.0.1",
			identity.Client{DeviceID: f.app.serverID, Device: "Integration Server", Version: "integration-version"})
		if err != nil {
			t.Fatalf("create media application key (%T)", err)
		}
		principal, err := f.users.ResolveEmby(f.ctx, key.Token)
		if err != nil || !principal.IsApplicationKey() || principal.User.ID != "" || principal.SessionID != key.CredentialID || principal.ClientSessionID == "" {
			t.Fatal("media application key did not resolve as an independent credential")
		}
		result[index] = applicationMediaKey{key: key, principal: principal, headers: http.Header{"X-Emby-Token": {key.Token}}}
	}
	return result
}

func applicationMediaClient(t *testing.T, f *serverFixture, key applicationMediaKey, client identity.Client) applicationMediaKey {
	t.Helper()
	principal, err := f.users.ResolveEmbyForClient(f.ctx, key.key.Token, client)
	if err != nil || principal.SessionID != key.principal.SessionID || principal.ClientSessionID == "" {
		t.Fatal("application client metadata did not bind to the original credential")
	}
	headers := key.headers.Clone()
	headers.Set("Authorization", `Emby Client="`+client.Name+`", DeviceId="`+client.DeviceID+`", Device="`+client.Device+`", Version="`+client.Version+`"`)
	return applicationMediaKey{key: key.key, principal: principal, headers: headers}
}

type applicationMediaFixture struct {
	f                    *serverFixture
	stream               *streamHTTPFixture
	keys                 [2]applicationMediaKey
	deniedID, disabledID string
	adminID              string
}

func newApplicationMediaFixture(t *testing.T) *applicationMediaFixture {
	t.Helper()
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	adminToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	fixture := &applicationMediaFixture{f: f, adminID: adminID, keys: applicationMediaIssueKeys(t, f, adminToken)}
	f.app.notifier.Close()
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close initial application media catalog (%T)", err)
	}
	root := t.TempDir()
	catalog, err := library.New(f.pool, applicationMediaProber{}, []string{root})
	if err != nil {
		t.Fatalf("create application media catalog (%T)", err)
	}
	f.app.library, f.app.cfg.MediaRoots = catalog, []string{root}
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	fixture.stream = &streamHTTPFixture{f: f, root: root, adminID: adminID}
	fixture.stream.video = fixture.stream.addItem(t, "Application.Feature.mp4", "movies", "Videos", "mp4", "video/mp4", bytes.Repeat([]byte("original-video-data\n"), 64))
	fixture.stream.audio = fixture.stream.addItem(t, "Application.Track.wav", "music", "Audio", "wav", "audio/wav", bytes.Repeat([]byte("original-audio-data\n"), 64))
	subtitlePath := strings.TrimSuffix(fixture.stream.video.path, ".mp4") + ".en.default.srt"
	if err := os.WriteFile(subtitlePath, []byte("1\n00:00:01,000 --> 00:00:02,000\nApplication subtitle\n"), 0o600); err != nil {
		t.Fatalf("write application subtitle (%T)", err)
	}
	fixture.stream.rescan(t, fixture.stream.video.libraryID)
	for index, name := range []string{"Denied media context", "Disabled media context"} {
		user, err := f.users.CreateUser(f.ctx, name, "media-context-password", false)
		if err != nil {
			t.Fatalf("create media context user (%T)", err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled = $2,
			policy = jsonb_set('{"EnableMediaPlayback":false,"EnableAllFolders":false,"EnabledFolders":[],
			"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false}'::jsonb,
			'{EnableMediaPlayback}', to_jsonb($2::boolean)) WHERE id = $1`, user.ID, index == 1); err != nil {
			t.Fatalf("restrict media context user (%T)", err)
		}
		if index == 0 {
			fixture.deniedID = user.ID
		} else {
			fixture.disabledID = user.ID
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id, item_id, playback_position_ticks, play_count, is_favorite)
			VALUES ($1, $2, 10000000, 3, true)`, user.ID, fixture.stream.video.id); err != nil {
			t.Fatalf("seed user playback data (%T)", err)
		}
	}
	f.handler = f.app.Handler()
	return fixture
}

func applicationMediaSnapshot(t *testing.T, f *serverFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id), '[]'::jsonb)::text FROM user_item_data d`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot application media user data (%T)", err)
	}
	return snapshot
}

func applicationMediaStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		// Media negotiation and playlist bodies can contain bearer credentials.
		t.Fatalf("application media HTTP status = %d, want %d", response.Code, expected)
	}
}

func applicationMediaAssertPublic(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for name, nested := range value {
			switch strings.ToLower(name) {
			case "videoseekindexes", "videoseekcandidate", "audiotiming", "audiodurationexact", "audiodurationreason",
				"presentationoriginticks", "sourceformatstartknown", "sourceformatstartticks", "formatstartknown", "formatstartticks",
				"probeversion", "filechangetimens", "hardware", "sourcestamp", "applicationcredentialid", "applicationclientid", "clientsessionid", "authsessionid":
				t.Errorf("application media response exposes private field %s", name)
			}
			applicationMediaAssertPublic(t, nested)
		}
	case []any:
		for _, nested := range value {
			applicationMediaAssertPublic(t, nested)
		}
	}
}

func applicationMediaPrepare(t *testing.T, fixture *applicationMediaFixture, key int, itemID, method, userID string, body map[string]any) (string, map[string]any) {
	t.Helper()
	target := "/emby/Items/" + itemID + "/PlaybackInfo"
	if userID != "" {
		target += "?UserId=" + url.QueryEscape(userID)
	}
	response := fixture.f.request(t, method, target, body, fixture.keys[key].headers)
	applicationMediaStatus(t, response, http.StatusOK)
	object, source := playbackHTTPSource(t, response)
	applicationMediaAssertPublic(t, object)
	for _, field := range []string{"UserId", "UserName", "UserData", "Password", "PasswordHash", "Policy", "Configuration"} {
		if _, exists := object[field]; exists {
			t.Errorf("PlaybackInfo exposes unexpected user field %s", field)
		}
		if _, exists := source[field]; exists {
			t.Errorf("media source exposes unexpected user field %s", field)
		}
	}
	playID := stringValue(t, object, "PlaySessionId")
	var userless bool
	var credentialID, deviceID, clientID string
	if err := fixture.f.pool.QueryRow(fixture.f.ctx, `SELECT user_id IS NULL, auth_session_id, device_id, application_client_id FROM play_sessions WHERE id = $1`, playID).
		Scan(&userless, &credentialID, &deviceID, &clientID); err != nil {
		t.Fatalf("read application playback ownership (%T)", err)
	}
	if !userless || credentialID != fixture.keys[key].principal.SessionID || deviceID != fixture.keys[key].principal.Client.DeviceID || clientID != fixture.keys[key].principal.ClientSessionID {
		t.Fatal("application playback borrowed the request user or another credential")
	}
	return playID, source
}

func TestHTTPApplicationKeyMediaIgnoresUserPolicyContextAndPreservesUserData(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, key := fixture.f, fixture.keys[0]
	before := applicationMediaSnapshot(t, f)
	var playID string
	baselineSupports := map[string]any{}
	for _, userID := range []string{"", fixture.deniedID, fixture.disabledID} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			var body map[string]any
			if method == http.MethodPost {
				body = map[string]any{"UserId": userID}
			}
			var source map[string]any
			playID, source = applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, method, userID, body)
			index, hasDefaultAudio := source["DefaultAudioStreamIndex"]
			if userID == "" && hasDefaultAudio || userID != "" && (!hasDefaultAudio || index != float64(1)) {
				t.Fatal("minimal application PlaybackInfo did not distinguish user context for the default audio track")
			}
			for _, field := range []string{"SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding"} {
				if userID == "" {
					baselineSupports[field] = source[field]
				} else if source[field] != baselineSupports[field] {
					t.Errorf("minimal target context changed %s", field)
				}
			}
		}
		for _, item := range []streamHTTPItem{fixture.stream.video, fixture.stream.audio} {
			target := "/emby/" + item.route + "/" + item.id + "/original." + item.container + "?UserId=" + url.QueryEscape(userID)
			headers := key.headers.Clone()
			headers.Set("Range", "bytes=7-22")
			response := f.request(t, http.MethodGet, target, nil, headers)
			applicationMediaStatus(t, response, http.StatusPartialContent)
			if !bytes.Equal(response.Body.Bytes(), item.data[7:23]) || response.Header().Get("Content-Range") != "bytes 7-22/"+strconv.Itoa(len(item.data)) {
				t.Fatal("application key range delivery changed source bytes or range metadata")
			}
			head := f.request(t, http.MethodHead, target, nil, key.headers)
			applicationMediaStatus(t, head, http.StatusOK)
			if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(item.data)) {
				t.Fatal("application key HEAD did not preserve original delivery semantics")
			}
		}
	}
	for _, body := range []map[string]any{{"AudioStreamIndex": 1}, matchingPlaybackHTTPBody()} {
		_, source := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodPost, "", body)
		if source["DefaultAudioStreamIndex"] != float64(1) {
			t.Fatal("explicit audio selection or profile lost its negotiated default audio track")
		}
	}
	missingUserTarget := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo"
	applicationMediaStatus(t, f.request(t, http.MethodGet, missingUserTarget+"?UserId=missing-media-user", nil, key.headers), http.StatusNotFound)
	applicationMediaStatus(t, f.request(t, http.MethodPost, missingUserTarget, map[string]any{"UserId": "missing-media-user"}, key.headers), http.StatusNotFound)
	subtitleTarget := "/emby/Videos/" + fixture.stream.video.id + "/" + media.SourceID(fixture.stream.video.id) + "/Subtitles/2/stream.vtt"
	subtitles := f.request(t, http.MethodGet, subtitleTarget, nil, key.headers)
	applicationMediaStatus(t, subtitles, http.StatusOK)
	if !strings.Contains(subtitles.Body.String(), "Application subtitle") || !strings.HasPrefix(subtitles.Body.String(), "WEBVTT") {
		t.Fatal("application subtitle was not rendered from the indexed source")
	}
	for index, route := range []string{"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"} {
		body := map[string]any{"PlaySessionId": playID, "ItemId": fixture.stream.video.id, "MediaSourceId": media.SourceID(fixture.stream.video.id),
			"SessionId": key.principal.ClientSessionID, "UserId": fixture.disabledID, "PositionTicks": int64(index+1) * media.TicksPerSecond}
		applicationMediaStatus(t, f.request(t, http.MethodPost, route, body, key.headers), http.StatusNoContent)
	}
	var state string
	if err := f.pool.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id = $1", playID).Scan(&state); err != nil || state != "Stopped" {
		t.Fatal("application playback lifecycle did not persist its stopped state")
	}
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("application media reads or reports changed personal playback data")
	}
}

func TestHTTPApplicationKeyMediaPingRefreshesOnlyRecoveredClientPlayback(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, credential, foreignKey := fixture.f, fixture.keys[0], fixture.keys[1]
	alpha := applicationMediaClient(t, f, credential, identity.Client{Name: "Ping Alpha", DeviceID: "ping-device", Device: "Ping Alpha Device", Version: "1.0"})
	beta := applicationMediaClient(t, f, credential, identity.Client{Name: "Ping Beta", DeviceID: "ping-device", Device: "Ping Beta Device", Version: "1.0"})
	fixture.keys = [2]applicationMediaKey{alpha, beta}
	alphaPlay, _ := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodGet, "", nil)
	betaPlay, _ := applicationMediaPrepare(t, fixture, 1, fixture.stream.video.id, http.MethodGet, "", nil)
	if _, err := f.pool.Exec(f.ctx, `UPDATE play_sessions SET expires_at = clock_timestamp() + interval '2 minutes'
		WHERE id = ANY($1::text[])`, []string{alphaPlay, betaPlay}); err != nil {
		t.Fatalf("set near-expiry application playback (%T)", err)
	}
	readPlayback := func(id string) (time.Time, string) {
		t.Helper()
		var expires time.Time
		var snapshot string
		if err := f.pool.QueryRow(f.ctx, "SELECT expires_at, to_jsonb(play)::text FROM play_sessions play WHERE id = $1", id).Scan(&expires, &snapshot); err != nil {
			t.Fatalf("read application ping playback (%T)", err)
		}
		return expires, snapshot
	}
	alphaExpires, alphaBefore := readPlayback(alphaPlay)
	_, betaBefore := readPlayback(betaPlay)
	userBefore := applicationMediaSnapshot(t, f)
	target := "/emby/Sessions/Playing/Ping?PlaySessionId=" + alphaPlay
	for _, headers := range []http.Header{foreignKey.headers, beta.headers} {
		applicationMediaStatus(t, f.request(t, http.MethodPost, target, nil, headers), http.StatusNoContent)
		_, alphaAfter := readPlayback(alphaPlay)
		_, betaAfter := readPlayback(betaPlay)
		if alphaAfter != alphaBefore || betaAfter != betaBefore {
			t.Fatal("foreign credential or client ping changed application playback")
		}
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, target, nil, credential.headers), http.StatusNoContent)
	refreshedExpires, _ := readPlayback(alphaPlay)
	_, betaAfter := readPlayback(betaPlay)
	if !refreshedExpires.After(alphaExpires.Add(20 * time.Minute)) {
		t.Fatal("token-only application ping did not refresh the recovered playback lifetime")
	}
	if betaAfter != betaBefore {
		t.Fatal("token-only application ping refreshed a sibling client playback")
	}
	var state, clientID string
	var position int64
	if err := f.pool.QueryRow(f.ctx, `SELECT state, position_ticks, application_client_id FROM play_sessions WHERE id = $1`, alphaPlay).
		Scan(&state, &position, &clientID); err != nil || state != "Prepared" || position != 0 || clientID != alpha.principal.ClientSessionID {
		t.Fatal("application ping changed playback identity, position, or player state")
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId=play_missing", nil, credential.headers), http.StatusNoContent)
	if after := applicationMediaSnapshot(t, f); after != userBefore {
		t.Fatal("application pings changed personal playback data")
	}
}

func TestHTTPApplicationKeyMediaSeparatesKeysAndSurvivesCreatorDisable(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, key, other := fixture.f, fixture.keys[0], fixture.keys[1]
	before := applicationMediaSnapshot(t, f)
	playID, _ := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodGet, "", nil)
	otherPlay, _ := applicationMediaPrepare(t, fixture, 1, fixture.stream.video.id, http.MethodGet, "", nil)
	if playID == otherPlay {
		t.Fatal("independent application credentials shared a playback identity")
	}
	body := map[string]any{"PlaySessionId": playID, "ItemId": fixture.stream.video.id, "PositionTicks": media.TicksPerSecond}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", body, other.headers), http.StatusNotFound)
	foreignPrepare := map[string]any{"CurrentPlaySessionId": playID}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo", foreignPrepare, other.headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", fixture.adminID); err != nil {
		t.Fatalf("disable application credential creator (%T)", err)
	}
	applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodGet, fixture.adminID, nil)
	target := "/emby/Videos/" + fixture.stream.video.id + "/original.mp4"
	applicationMediaStatus(t, f.request(t, http.MethodGet, target, nil, key.headers), http.StatusOK)
	if _, err := f.users.RevokeApplicationKey(f.ctx, other.principal, key.key.ID); err != nil {
		t.Fatalf("revoke independent media key (%T)", err)
	}
	applicationMediaStatus(t, f.request(t, http.MethodGet, target, nil, key.headers), http.StatusUnauthorized)
	applicationMediaStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo", nil, key.headers), http.StatusUnauthorized)
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", body, key.headers), http.StatusUnauthorized)
	applicationMediaStatus(t, f.request(t, http.MethodGet, target, nil, other.headers), http.StatusOK)
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("cross-key attempts or key revocation changed personal playback data")
	}
}

func TestHTTPApplicationKeyMediaConversionUsesGlobalLimits(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f := fixture.f
	runtime, jobs := hlsRuntimeTestFixture(t)
	runtime.server, runtime.verify = f.app, f.app.authorizeHLS
	runtime.done, runtime.slots, runtime.probes = make(chan struct{}), make(chan struct{}, 32), make(chan struct{}, 2)
	f.app.hls = runtime
	f.app.cfg.Transcoding = config.TranscodingConfig{Enabled: true, MaxBitrate: 800_000, MaxWidth: 96, MaxHeight: 54, MaxAudioChannels: 2}
	before := applicationMediaSnapshot(t, f)
	body := map[string]any{"IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "SubtitleStreamIndex": -1, "DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "SegmentLength": 3,
		}}}}
	playID, source := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodPost, fixture.adminID, body)
	masterURL, ok := source["TranscodingUrl"].(string)
	if !ok || source["SupportsTranscoding"] != true {
		t.Fatal("application key did not advertise globally enabled conversion")
	}
	for _, userID := range []string{fixture.deniedID, fixture.disabledID} {
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = policy ||
			'{"EnablePlaybackRemuxing":true,"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":true}'::jsonb
			WHERE id = $1`, userID); err != nil {
			t.Fatalf("enable independent target conversion permissions (%T)", err)
		}
		_, independent := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodPost, userID, body)
		if independent["SupportsDirectPlay"] != false || independent["SupportsDirectStream"] != false || independent["SupportsTranscoding"] != true {
			t.Fatal("account or media-playback restrictions independently disabled application conversion")
		}
		if targetURL, ok := independent["TranscodingUrl"].(string); !ok || targetURL == "" {
			t.Fatal("independently restricted target lost its negotiated conversion URL")
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = policy ||
			'{"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false}'::jsonb
			WHERE id = $1`, userID); err != nil {
			t.Fatalf("disable target conversion permissions (%T)", err)
		}
	}
	for _, userID := range []string{fixture.deniedID, fixture.disabledID} {
		target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo?UserId=" + userID
		response := f.request(t, http.MethodPost, target, body, fixture.keys[0].headers)
		applicationMediaStatus(t, response, http.StatusOK)
		object, restricted := playbackHTTPSource(t, response)
		for _, field := range []string{"SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding"} {
			if restricted[field] != false {
				t.Errorf("restricted target left %s enabled", field)
			}
		}
		for _, field := range []string{"TranscodingUrl", "TranscodingSubProtocol", "TranscodingContainer", "DirectStreamUrl"} {
			if _, exists := restricted[field]; exists {
				t.Errorf("restricted target retained %s", field)
			}
		}
		if _, exists := object["ErrorCode"]; exists {
			t.Fatal("target policy restrictions were reported as a stream compatibility error")
		}
		static := "/emby/Videos/" + fixture.stream.video.id + "/stream?Static=true&UserId=" + userID
		headers := fixture.keys[0].headers.Clone()
		headers.Set("Range", "bytes=0-31")
		applicationMediaStatus(t, f.request(t, http.MethodGet, static, nil, headers), http.StatusPartialContent)
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo", body, fixture.keys[0].headers), http.StatusBadRequest)
	master := f.request(t, http.MethodGet, masterURL, nil, nil)
	applicationMediaStatus(t, master, http.StatusOK)
	children := hlsHTTPManifestChildren(master.Body.Bytes())
	if len(children) != 1 || !strings.Contains(master.Body.String(), "RESOLUTION=96x54") {
		t.Fatal("application HLS manifest ignored configured output dimensions")
	}
	main := f.request(t, http.MethodGet, children[0], nil, nil)
	applicationMediaStatus(t, main, http.StatusOK)
	if !strings.Contains(main.Body.String(), "#EXT-X-ENDLIST") || len(hlsHTTPManifestChildren(main.Body.Bytes())) != 4 {
		t.Fatal("application HLS did not preserve the complete VOD timeline")
	}
	parsed, err := url.Parse(masterURL)
	if err != nil {
		t.Fatal("parse negotiated application HLS path")
	}
	session, err := runtime.find(parsed.Query().Get("GobyHlsId"), fixture.keys[0].principal, fixture.stream.video.id)
	if err != nil || !session.key.scope.ApplicationKey || session.key.scope.UserID != "" || session.key.scope.PlaySessionID != playID ||
		session.key.scope.ApplicationClientID != fixture.keys[0].principal.ClientSessionID ||
		session.key.plan.Width > 96 || session.key.plan.Height > 54 || session.key.plan.VideoBitrate+session.key.plan.AudioBitrate > 800_000 {
		t.Fatal("application HLS scope or plan escaped the server configuration")
	}
	foreignURL := hlsHTTPWithoutToken(t, masterURL)
	applicationMediaStatus(t, f.request(t, http.MethodGet, foreignURL, nil, fixture.keys[1].headers), http.StatusNotFound)
	for _, target := range []string{
		"/emby/Videos/" + fixture.stream.video.id + "/stream.mp4?Static=false&AllowVideoStreamCopy=false&UserId=" + fixture.disabledID,
		"/emby/Audio/" + fixture.stream.audio.id + "/stream.mp3?Static=false&AudioCodec=mp3&UserId=" + fixture.deniedID,
	} {
		response := f.request(t, http.MethodHead, target, nil, fixture.keys[0].headers)
		applicationMediaStatus(t, response, http.StatusOK)
		if response.Body.Len() != 0 || response.Header().Get("Content-Length") != "" || response.Header().Get("Accept-Ranges") != "none" {
			t.Fatal("application progressive HEAD did not retain header-only conversion semantics")
		}
	}
	jobs.mu.Lock()
	started := len(jobs.ensured)
	jobs.mu.Unlock()
	if started != 0 {
		t.Fatal("manifest and progressive HEAD requests started an encoder")
	}
	f.app.cfg.Transcoding.Enabled = false
	applicationMediaStatus(t, f.request(t, http.MethodGet, masterURL, nil, nil), http.StatusForbidden)
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("application conversion negotiation changed personal playback data")
	}
}

func TestHTTPApplicationKeyMediaSeparatesClientContextsAndRestoresURLContext(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, credential := fixture.f, fixture.keys[0]
	alpha := applicationMediaClient(t, f, credential, identity.Client{Name: "Media Alpha", DeviceID: "shared-media-device", Device: "Alpha Device", Version: "1.0"})
	beta := applicationMediaClient(t, f, credential, identity.Client{Name: "Media Beta", DeviceID: "shared-media-device", Device: "Beta Device", Version: "2.0"})
	fixture.keys = [2]applicationMediaKey{alpha, beta}
	if alpha.principal.ClientSessionID == beta.principal.ClientSessionID {
		t.Fatal("different client names on one device shared an application context")
	}
	runtime, _ := hlsRuntimeTestFixture(t)
	runtime.server, runtime.verify = f.app, f.app.authorizeHLS
	runtime.done, runtime.slots, runtime.probes = make(chan struct{}), make(chan struct{}, 32), make(chan struct{}, 2)
	f.app.hls = runtime
	f.app.cfg.Transcoding = config.TranscodingConfig{Enabled: true, MaxBitrate: 800_000, MaxWidth: 96, MaxHeight: 54, MaxAudioChannels: 2}
	before := applicationMediaSnapshot(t, f)
	for index, key := range fixture.keys {
		method := http.MethodGet
		var body map[string]any
		if index == 1 {
			method, body = http.MethodPost, map[string]any{}
		}
		target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo?DeviceId=query-hint-" + strconv.Itoa(index)
		applicationMediaStatus(t, f.request(t, method, target, body, key.headers), http.StatusOK)
		headers := key.headers.Clone()
		headers.Set("Range", "bytes=0-31")
		target = "/emby/Videos/" + fixture.stream.video.id + "/stream?Static=true&DeviceId=query-hint-" + strconv.Itoa(index)
		applicationMediaStatus(t, f.request(t, http.MethodGet, target, nil, headers), http.StatusPartialContent)
	}
	body := map[string]any{"UserId": fixture.adminID, "IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "SubtitleStreamIndex": -1,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "SegmentLength": 3,
		}}}}
	alphaPlay, alphaSource := applicationMediaPrepare(t, fixture, 0, fixture.stream.video.id, http.MethodPost, "", body)
	betaPlay, betaSource := applicationMediaPrepare(t, fixture, 1, fixture.stream.video.id, http.MethodPost, "", body)
	alphaURL, alphaOK := alphaSource["TranscodingUrl"].(string)
	betaURL, betaOK := betaSource["TranscodingUrl"].(string)
	if !alphaOK || !betaOK || alphaURL == betaURL || alphaPlay == betaPlay {
		t.Fatal("different application client contexts shared a negotiated output")
	}
	applicationMediaStatus(t, f.request(t, http.MethodGet, alphaURL, nil, nil), http.StatusOK)
	applicationMediaStatus(t, f.request(t, http.MethodGet, alphaURL, nil, beta.headers), http.StatusNotFound)
	applicationMediaStatus(t, f.request(t, http.MethodGet, betaURL, nil, nil), http.StatusOK)
	report := map[string]any{"PlaySessionId": alphaPlay, "ItemId": fixture.stream.video.id, "PositionTicks": media.TicksPerSecond}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", report, beta.headers), http.StatusNotFound)
	report["SessionId"] = alpha.principal.ClientSessionID
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing", report, credential.headers), http.StatusNoContent)
	var alphaState, betaState string
	if err := f.pool.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id = $1", alphaPlay).Scan(&alphaState); err != nil {
		t.Fatalf("read first application client playback (%T)", err)
	}
	if err := f.pool.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id = $1", betaPlay).Scan(&betaState); err != nil || alphaState != "Playing" || betaState != "Prepared" {
		t.Fatal("token-only report failed to restore the correct client playback context")
	}
	continued := map[string]any{"CurrentPlaySessionId": alphaPlay}
	response := f.request(t, http.MethodPost, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo", continued, credential.headers)
	applicationMediaStatus(t, response, http.StatusOK)
	continuedObject, _ := playbackHTTPSource(t, response)
	if continuedObject["PlaySessionId"] != alphaPlay {
		t.Fatal("token-only PlaybackInfo lost its existing client context")
	}
	nonce := "shared-client-playback-reference"
	universal := "/emby/Audio/" + fixture.stream.audio.id + "/universal?Container=wav&PlaySessionId=" + nonce
	for _, key := range fixture.keys {
		applicationMediaStatus(t, f.request(t, http.MethodHead, universal, nil, key.headers), http.StatusOK)
	}
	alphaAudio, err := f.app.library.ResolvePlaybackReference(f.ctx, playbackOwner(alpha.principal), nonce)
	if err != nil {
		t.Fatalf("resolve first application client nonce (%T)", err)
	}
	betaAudio, err := f.app.library.ResolvePlaybackReference(f.ctx, playbackOwner(beta.principal), nonce)
	if err != nil || alphaAudio == betaAudio {
		t.Fatal("application clients shared a client playback nonce")
	}
	stop := "/emby/Videos/ActiveEncodings?PlaySessionId=" + alphaPlay + "&DeviceId=ignored-query-hint"
	applicationMediaStatus(t, f.request(t, http.MethodDelete, stop, nil, beta.headers), http.StatusNoContent)
	applicationMediaStatus(t, f.request(t, http.MethodGet, alphaURL, nil, alpha.headers), http.StatusOK)
	applicationMediaStatus(t, f.request(t, http.MethodDelete, stop, nil, alpha.headers), http.StatusNoContent)
	applicationMediaStatus(t, f.request(t, http.MethodGet, alphaURL, nil, alpha.headers), http.StatusNotFound)
	applicationMediaStatus(t, f.request(t, http.MethodGet, betaURL, nil, beta.headers), http.StatusOK)
	runtime.cancelMatching(credential.principal.SessionID, "")
	applicationMediaStatus(t, f.request(t, http.MethodGet, betaURL, nil, beta.headers), http.StatusNotFound)
	var unexpected int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM application_key_clients WHERE credential_id = $1 AND device_id LIKE 'query-hint-%'`, credential.principal.SessionID).Scan(&unexpected); err != nil || unexpected != 0 {
		t.Fatal("query DeviceId created an authentication client context")
	}
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("application client context activity changed personal playback data")
	}
}

func applicationMediaRealGraph(t *testing.T, fixture *hlsHTTPFixture, key applicationMediaKey, userID string) hlsHTTPGraph {
	t.Helper()
	body := map[string]any{"UserId": userID, "IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac",
			"MaxWidth": 96, "MaxHeight": 54, "SegmentLength": 3,
		}}}}
	response := fixture.request(t, http.MethodPost, "/emby/Items/"+fixture.item.ID+"/PlaybackInfo", body, key.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var object struct {
		PlaySessionID string           `json:"PlaySessionId"`
		MediaSources  []map[string]any `json:"MediaSources"`
		ErrorCode     string           `json:"ErrorCode"`
	}
	if err := json.Unmarshal(response.body, &object); err != nil || object.PlaySessionID == "" || len(object.MediaSources) != 1 || object.ErrorCode != "" {
		t.Fatal("application profile did not return one compatible HLS source")
	}
	masterURL, ok := object.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || object.MediaSources[0]["SupportsTranscoding"] != true {
		t.Fatal("application profile did not advertise conversion")
	}
	parsed := hlsHTTPURL(t, masterURL, key.key.Token)
	master := fixture.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	children := hlsHTTPManifestChildren(master.body)
	if len(children) != 1 {
		t.Fatal("application master playlist omitted its media playlist")
	}
	main := fixture.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	graph := hlsHTTPGraph{playID: object.PlaySessionID, hlsID: parsed.Query().Get("GobyHlsId"), masterURL: masterURL,
		mainURL: children[0], main: main.body, children: hlsHTTPManifestChildren(main.body)}
	for _, line := range strings.Split(string(main.body), "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			raw, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, err := strconv.ParseFloat(raw, 64)
			if err != nil || duration <= 0 {
				t.Fatal("application media playlist has invalid segment duration")
			}
			graph.durations = append(graph.durations, duration)
		}
	}
	if len(graph.children) != 4 || len(graph.durations) != 4 {
		t.Fatal("application HLS lost the complete source timeline")
	}
	return graph
}

func TestHTTPApplicationKeyMediaUsesRealHLSAndProgressiveEngine(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	f := fixture.f
	keys := applicationMediaIssueKeys(t, f, fixture.accounts.admin.headers.Get("X-Emby-Token"))
	before := applicationMediaSnapshot(t, f)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled = true,
		policy = '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, fixture.accounts.admin.userID); err != nil {
		t.Fatalf("disable real media key creator (%T)", err)
	}
	graph := applicationMediaRealGraph(t, fixture, keys[0], fixture.accounts.viewer.userID)
	segment := fixture.request(t, http.MethodGet, graph.children[0], nil, nil)
	expectHLSHTTPStatus(t, segment, http.StatusOK)
	fixture.verifySegment(t, segment.body, 0, graph.durations[0])
	var keyOwned bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) > 0 AND bool_and(user_id IS NULL AND auth_session_id = $2 AND application_client_id = $3)
		FROM encoding_jobs WHERE play_session_id = $1`, graph.playID, keys[0].principal.SessionID, keys[0].principal.ClientSessionID).Scan(&keyOwned); err != nil || !keyOwned {
		t.Fatal("real HLS encoder job did not retain the userless credential owner")
	}
	foreign := fixture.request(t, http.MethodGet, hlsHTTPWithoutToken(t, graph.children[0]), nil, keys[1].headers)
	expectHLSHTTPStatus(t, foreign, http.StatusNotFound)
	target := "/emby/Videos/" + fixture.item.ID + "/stream.mp4?Static=false&AllowVideoStreamCopy=false&VideoCodec=h264&AudioCodec=aac&MaxWidth=96&MaxHeight=54"
	progressive := fixture.request(t, http.MethodGet, target, nil, keys[0].headers)
	expectHLSHTTPStatus(t, progressive, http.StatusOK)
	if len(progressive.body) < 64 || progressive.header.Get("Content-Type") != "video/mp4" || progressive.header.Get("Accept-Ranges") != "none" {
		t.Fatal("application progressive conversion did not return MP4 output")
	}
	path := filepath.Join(t.TempDir(), "application-output.mp4")
	if err := os.WriteFile(path, progressive.body, 0o600); err != nil {
		t.Fatalf("save application progressive output (%T)", err)
	}
	encoded := hlsHTTPMediaCommand(t, fixture.ffprobe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,width,height", "-of", "json", path)
	var probe struct {
		Streams []struct {
			Type          string `json:"codec_type"`
			Codec         string `json:"codec_name"`
			Width, Height int
		} `json:"streams"`
	}
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal("decode application progressive output facts")
	}
	video, audio := false, false
	for _, stream := range probe.Streams {
		video = video || stream.Type == "video" && stream.Codec == "h264" && stream.Width == 96 && stream.Height == 54
		audio = audio || stream.Type == "audio" && stream.Codec == "aac"
	}
	if !video || !audio {
		t.Fatal("application progressive output did not use the requested H.264/AAC conversion")
	}
	hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-f", "null", "-")
	if _, err := f.users.RevokeApplicationKey(f.ctx, keys[1].principal, keys[0].key.ID); err != nil {
		t.Fatalf("revoke real media application key (%T)", err)
	}
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, graph.masterURL, nil, nil), http.StatusUnauthorized)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusUnauthorized)
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("real application conversion changed personal playback data")
	}
}
