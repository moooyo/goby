//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func playbackProfileWireFixture(t *testing.T) ([]byte, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "emby-web-4.9.5.0-playback-profile.json"))
	if err != nil {
		t.Fatal("read the captured public Web Client device profile")
	}
	var profile map[string]any
	if json.Unmarshal(raw, &profile) != nil || profile == nil || profile["MaxStaticBitrate"] != float64(200000000) {
		t.Fatal("the captured profile lost its complete object or static bitrate declaration")
	}
	return raw, profile
}

func playbackProfileWireBody(t *testing.T, profile any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"DeviceProfile": profile, "IsPlayback": true})
	if err != nil {
		t.Fatal("encode a playback profile request")
	}
	return string(body)
}

func playbackProfileWireNative(t *testing.T, profile map[string]any) {
	t.Helper()
	segments, conditions := 0, 0
	for _, raw := range profile["TranscodingProfiles"].([]any) {
		entry := raw.(map[string]any)
		if value, ok := entry["MinSegments"].(string); ok {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed != 1 {
				t.Fatal("the captured minimum-segment spelling changed")
			}
			entry["MinSegments"] = parsed
			segments++
		}
	}
	for _, raw := range profile["CodecProfiles"].([]any) {
		for _, condition := range raw.(map[string]any)["Conditions"].([]any) {
			entry := condition.(map[string]any)
			if value, ok := entry["IsRequired"].(string); ok {
				parsed, err := strconv.ParseBool(value)
				if err != nil || value != "false" {
					t.Fatal("the captured optional-condition spelling changed")
				}
				entry["IsRequired"] = parsed
				conditions++
			}
		}
	}
	if segments != 2 || conditions != 4 {
		t.Fatal("the full Web Client fixture no longer covers both observed scalar adapters")
	}
}

func playbackProfileWireStableSource(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		if key != "DirectStreamUrl" && key != "TranscodingUrl" {
			result[key] = value
		}
	}
	return result
}

func playbackProfileWireState(t *testing.T, p *playbackHTTPFixture) string {
	t.Helper()
	var snapshot string
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT jsonb_build_object(
		'play_sessions', (SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY id), '[]'::jsonb) FROM play_sessions p),
		'user_data', (SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id), '[]'::jsonb) FROM user_item_data d),
		'users', (SELECT jsonb_agg(jsonb_build_object('id', id, 'configuration', configuration,
			'policy', policy, 'revision', management_revision) ORDER BY id) FROM users))::text`).Scan(&snapshot); err != nil {
		t.Fatal("snapshot playback sessions and unrelated user state")
	}
	return snapshot
}

func TestHTTPPlaybackFullWebClientProfileMatchesNativeScalarsAndOriginalRange(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	raw, native := playbackProfileWireFixture(t)
	playbackProfileWireNative(t, native)
	path := "/emby/Items/" + p.s.video.id + "/PlaybackInfo"
	baselineHeaders := p.headers.Clone()
	baselineHeaders.Set("Content-Type", "application/json")
	baseline, baselineSource := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, path,
		playbackProfileWireBody(t, native), baselineHeaders, nil))
	if baseline["ErrorCode"] != nil || baselineSource["SupportsDirectPlay"] != true || baselineSource["SupportsDirectStream"] != true {
		t.Fatal("the captured native profile did not match the H.264 High Level 4.1 and AAC source facts")
	}
	wantSource := playbackProfileWireStableSource(baselineSource)
	wireBody := playbackProfileWireBody(t, json.RawMessage(raw))
	query := url.Values{
		"api_key": {p.s.token}, "X-Emby-Client": {"Integration, Client"},
		"X-Emby-Device-Id": {"integration-device"}, "X-Emby-Device-Name": {"Linux"},
		"X-Emby-Client-Version": {"1.0"}, "UserId": {p.s.viewerID},
	}
	for _, mediaType := range []string{"text/plain; charset=UTF-8", "application/json"} {
		response := playbackTextRequest(t, p, http.MethodPost, "/Items/"+p.s.video.id+"/playbackinfo?"+query.Encode(), wireBody,
			http.Header{"Content-Type": {mediaType}}, nil)
		object, source := playbackHTTPSource(t, response)
		if object["ErrorCode"] != baseline["ErrorCode"] || !reflect.DeepEqual(playbackProfileWireStableSource(source), wantSource) {
			t.Fatal("string profile scalars or query metadata changed the native planner result or indexed source projection")
		}
		if source["Id"] != media.SourceID(p.s.video.id) || source["Container"] != "mp4" ||
			source["DefaultAudioStreamIndex"] != float64(5) || source["Bitrate"] != float64(4000000) ||
			source["RunTimeTicks"] != float64(600*media.TicksPerSecond) {
			t.Fatal("the full profile changed authoritative source facts or selected audio")
		}
		streams, ok := source["MediaStreams"].([]any)
		if !ok || len(streams) != 2 {
			t.Fatal("the source did not retain its two indexed media streams")
		}
		video, videoOK := streams[0].(map[string]any)
		audio, audioOK := streams[1].(map[string]any)
		if !videoOK || !audioOK || video["Codec"] != "h264" || audio["Codec"] != "aac" ||
			video["Index"] != float64(2) || audio["Index"] != float64(5) {
			t.Fatal("the full profile changed H.264/AAC stream types or indices")
		}
		playID := stringValue(t, object, "PlaySessionId")
		var userID, authID, deviceID, itemID, sourceID, state string
		if err := p.s.f.pool.QueryRow(p.s.f.ctx, `SELECT user_id, auth_session_id, device_id, item_id, media_source_id, state
			FROM play_sessions WHERE id=$1`, playID).Scan(&userID, &authID, &deviceID, &itemID, &sourceID, &state); err != nil ||
			userID != p.s.viewerID || authID != p.authSessionID || deviceID != "integration-device" ||
			itemID != p.s.video.id || sourceID != media.SourceID(p.s.video.id) || state != "Prepared" {
			t.Fatal("the Web Client request did not persist the authenticated prepared-playback binding")
		}
		streamURL, err := url.Parse(stringValue(t, source, "DirectStreamUrl"))
		if err != nil || streamURL.IsAbs() || streamURL.Host != "" || streamURL.User != nil || streamURL.Fragment != "" ||
			streamURL.Path != "/videos/"+p.s.video.id+"/original.mp4" || streamURL.Query().Get("PlaySessionId") != playID {
			t.Fatal("the negotiated direct stream is not the bound relative original-file route")
		}
		stream := p.s.request(t, http.MethodGet, streamURL.String(), "", http.Header{"Range": {"bytes=3-18"}}, nil)
		if stream.status != http.StatusPartialContent || !bytes.Equal(stream.body, p.s.video.data[3:19]) ||
			stream.header.Get("Content-Range") != "bytes 3-18/"+strconv.Itoa(len(p.s.video.data)) || stream.header.Get("Content-Type") != "video/mp4" {
			t.Fatal("the full-profile playback URL did not serve the requested original-byte range")
		}
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
}

func TestHTTPPlaybackStringRequiredConditionHasTheNativePlannerEffect(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	_, wire := playbackProfileWireFixture(t)
	_, native := playbackProfileWireFixture(t)
	playbackProfileWireNative(t, native)
	setRequired := func(profile map[string]any, value any) {
		condition := profile["CodecProfiles"].([]any)[0].(map[string]any)["Conditions"].([]any)[0].(map[string]any)
		condition["IsRequired"] = value
	}
	setRequired(wire, "true")
	setRequired(native, true)
	path := "/emby/Items/" + p.s.video.id + "/PlaybackInfo"
	var wantSource map[string]any
	var wantError any
	for index, profile := range []map[string]any{native, wire} {
		headers := p.headers.Clone()
		headers.Set("Content-Type", "text/plain")
		object, source := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, path,
			playbackProfileWireBody(t, profile), headers, nil))
		// The fixture cannot establish IsSecondaryAudio. Making that AAC
		// condition required must decline the original direct-play decision.
		if source["SupportsDirectPlay"] != false || source["SupportsDirectStream"] != false || object["ErrorCode"] != "NoCompatibleStream" {
			t.Fatal("a string or native required condition was silently treated as optional")
		}
		if index == 0 {
			wantSource, wantError = playbackProfileWireStableSource(source), object["ErrorCode"]
		} else if !reflect.DeepEqual(playbackProfileWireStableSource(source), wantSource) || object["ErrorCode"] != wantError {
			t.Fatal("the required-condition string and native boolean produced different planner semantics")
		}
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
}

func TestHTTPPlaybackProfileWireIgnoresBoundedExtensionMetadata(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	raw, extended := playbackProfileWireFixture(t)
	path := "/emby/Items/" + p.s.video.id + "/PlaybackInfo"
	baseline, baselineSource := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, path,
		playbackProfileWireBody(t, json.RawMessage(raw)), p.headers, nil))
	if baseline["ErrorCode"] != nil || baselineSource["SupportsDirectPlay"] != true || baselineSource["SupportsDirectStream"] != true {
		t.Fatal("the unextended reference profile did not establish original playback")
	}
	extended["ClientExtensionMetadata"] = map[string]any{
		"BuildLabel": "reference-extension", "DisplayHints": []string{"compact", "dark"},
	}
	extended["DirectPlayProfiles"].([]any)[0].(map[string]any)["ClientExtensionMetadata"] = map[string]any{
		"Description": "unrecognized client presentation metadata", "Revision": 1,
	}
	body, err := json.Marshal(map[string]any{
		"DeviceProfile": extended, "IsPlayback": true,
		"ClientExtensionMetadata": map[string]any{
			"Name": "fixture extension", "Details": map[string]any{"Color": "blue", "Visible": true},
		},
	})
	if err != nil {
		t.Fatal("encode bounded root, profile, and nested extension metadata")
	}
	for _, mediaType := range []string{"text/plain", "application/json"} {
		headers := p.headers.Clone()
		headers.Set("Content-Type", mediaType)
		object, source := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, path, string(body), headers, nil))
		if object["ErrorCode"] != baseline["ErrorCode"] ||
			!reflect.DeepEqual(playbackProfileWireStableSource(source), playbackProfileWireStableSource(baselineSource)) {
			t.Fatal("bounded unknown metadata changed the established playback decision or source projection")
		}
	}
	assertPlaybackHTTPData(t, p.detailData(t, p.s.video.id), 0, 0, false, false)
}

func TestHTTPPlaybackProfileWireRejectsAmbiguityAndInvalidScalarsWithoutStateChanges(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	raw, _ := playbackProfileWireFixture(t)
	path := "/emby/Items/" + p.s.video.id + "/PlaybackInfo"
	prepared, _ := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, path,
		playbackProfileWireBody(t, json.RawMessage(raw)), p.headers, nil))
	playID := stringValue(t, prepared, "PlaySessionId")
	p.report(t, "Started", playID, 0)
	p.report(t, "Progress", playID, 120*media.TicksPerSecond)
	p.report(t, "Stopped", playID, 120*media.TicksPerSecond)
	before := playbackProfileWireState(t, p)
	validBody := playbackProfileWireBody(t, json.RawMessage(raw))
	tests := []struct{ name, body string }{
		{"duplicate-profile-alias", `{"DeviceProfile":` + string(raw) + `,"deviceprofile":` + string(raw) + `}`},
		{"duplicate-extension-alias", `{"ClientExtensionMetadata":{"Label":"first","LABEL":"second"},` + validBody[1:]},
		{"deep-extension", `{"ClientExtensionMetadata":` + strings.Repeat("[", maxPlaybackInfoJSONDepth+1) + `0` +
			strings.Repeat("]", maxPlaybackInfoJSONDepth+1) + `,` + validBody[1:]},
		{"long-extension-text", `{"ClientExtensionMetadata":{"Notes":"` + strings.Repeat("x", maxPlaybackInfoJSONText+1) + `"},` + validBody[1:]},
	}
	duplicateStatic := strings.Replace(string(raw), `"MaxStaticBitrate": 200000000`, `"MaxStaticBitrate": 200000000, "maxstaticbitrate": 1`, 1)
	if duplicateStatic == string(raw) {
		t.Fatal("the captured static bitrate field needed for the duplicate-alias case changed")
	}
	tests = append(tests, struct{ name, body string }{"duplicate-nested-alias", playbackProfileWireBody(t, json.RawMessage(duplicateStatic))})
	for _, value := range []string{"TRUE", " false", "0", ""} {
		_, profile := playbackProfileWireFixture(t)
		condition := profile["CodecProfiles"].([]any)[0].(map[string]any)["Conditions"].([]any)[0].(map[string]any)
		condition["IsRequired"] = value
		tests = append(tests, struct{ name, body string }{"invalid-required-string", playbackProfileWireBody(t, profile)})
	}
	for _, value := range []string{"-1", "01", "+1", "1.0", "2147483648", " 1", "NaN"} {
		_, profile := playbackProfileWireFixture(t)
		profile["TranscodingProfiles"].([]any)[0].(map[string]any)["MinSegments"] = value
		tests = append(tests, struct{ name, body string }{"invalid-minimum-segment-string", playbackProfileWireBody(t, profile)})
	}
	for _, mediaType := range []string{"text/plain", "application/json"} {
		for _, test := range tests {
			headers := p.headers.Clone()
			headers.Set("Content-Type", mediaType)
			response := playbackTextRequest(t, p, http.MethodPost, path, test.body, headers, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s with %s returned HTTP %d, want 400", test.name, mediaType, response.Code)
			}
			if playbackProfileWireState(t, p) != before {
				t.Fatalf("%s changed play-session rows, personal playback state, or account configuration", test.name)
			}
		}
	}
}
