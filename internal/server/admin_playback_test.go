package server

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNativePlaybackCommandRejectsAmbiguousAuthorityAndTickValues(t *testing.T) {
	for _, body := range []string{
		`{"Command":"Stop","ControllingUserId":"someone-else"}`,
		`{"Command":"Stop","command":"Pause"}`,
		`{"Command":"Seek","SeekPositionTicks":10}`,
		`{"Command":"Seek","SeekPositionTicks":"01"}`,
		`{"Command":"Seek","SeekPositionTicks":"-1"}`,
		`{"Command":"Seek","SeekPositionTicks":"1e7"}`,
		`{"Command":"Seek","SeekPositionTicks":"9223372036854775808"}`,
		`{"Command":"Seek","SeekPositionTicks":null}`,
		`{"Command":"DisplayMessage","Text":""}`,
		`{"Command":"DisplayMessage","Text":"Hello","TimeoutMs":null}`,
		`{"Command":"DisplayMessage","Text":"Hello","TimeoutMs":60001}`,
		`{"Command":"DisplayMessage","Text":"Hello","Arguments":{"Text":"other"}}`,
		`{"Command":"PlayNow","ItemIds":[]}`,
		`{"Command":"PlayNow","ItemIds":["movie-one","movie-two"]}`,
		`{"Command":"PlayNext","ItemIds":["movie"],"StartPositionTicks":"0"}`,
		`{"Command":"Stop","PlaySessionId":" play-old"}`,
		`{"Command":"DisplayMessage","Text":"Hello","PlaySessionId":"play-old"}`,
	} {
		request := httptest.NewRequest("POST", "/admin/v1/playback/sessions/client/command", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if _, err := parseNativePlaybackCommand(request); err == nil {
			t.Errorf("native command accepted an ambiguous or out-of-range body: %s", body)
		}
	}
	request := httptest.NewRequest("POST", "/admin/v1/playback/sessions/client/command", strings.NewReader(`{"Command":"Seek","SeekPositionTicks":"9007199254740993","PlaySessionId":"play-current"}`))
	request.Header.Set("Content-Type", "application/json")
	command, err := parseNativePlaybackCommand(request)
	if err != nil || command.data["SeekPositionTicks"] != int64(9007199254740993) || command.playSessionID != "play-current" {
		t.Fatal("native seek lost exact decimal tick or playback identity")
	}
}

func TestNativePlaybackNowRequiresOneItemWithoutChangingCompatibleQueueSelections(t *testing.T) {
	for _, test := range []struct {
		body      string
		wantError bool
	}{
		{`{"Command":"PlayNow","ItemIds":[]}`, true},
		{`{"Command":"PlayNow","ItemIds":["movie-one","movie-two"]}`, true},
		{`{"Command":"PlayNow","ItemIds":["movie-one"]}`, false},
		{`{"Command":"PlayNext","ItemIds":["movie-one","movie-two"]}`, false},
		{`{"Command":"PlayLast","ItemIds":["movie-one","movie-two"]}`, false},
	} {
		request := httptest.NewRequest("POST", "/admin/v1/playback/sessions/client/command", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		_, err := parseNativePlaybackCommand(request)
		if test.wantError && !errors.Is(err, errAdminPlaybackSingleItem) || !test.wantError && err != nil {
			t.Fatalf("native playback selection error = %v, want single-item denial = %t", err, test.wantError)
		}
	}
	request := httptest.NewRequest("POST", "/emby/Sessions/client/Playing", strings.NewReader(`{"PlayCommand":"PlayNow","ItemIds":["movie-one","movie-two"]}`))
	request.Header.Set("Content-Type", "application/json")
	data, err := parseRemotePlay(request)
	if err != nil || len(data["ItemIds"].([]string)) != 2 {
		t.Fatal("native single-item validation changed the Emby-compatible multi-item PlayNow contract")
	}
}

func TestNativePlaybackControlsRequireCurrentPolicyTransportAndCapabilities(t *testing.T) {
	actor := identity.Principal{Kind: "admin", SessionID: "admin-login", User: identity.User{
		ID: "administrator", IsAdministrator: true, Policy: json.RawMessage(`{}`),
	}}
	target := identity.ClientSession{Kind: "emby", UserID: "viewer", SessionID: "viewer-login", Capabilities: identity.ClientCapabilities{
		SupportsMediaControl: true, PlayableMediaTypes: []string{"Video"}, SupportedCommands: []string{"DisplayMessage"},
	}}
	dto := map[string]any{"NowPlayingItem": map[string]any{"Id": "movie"}, "PlayState": map[string]any{"CanSeek": true}}
	if controls := nativePlaybackControls(actor, target, dto, true); !controls["Stop"] || !controls["Seek"] || !controls["Message"] || !controls["Play"] {
		t.Fatal("declared, connected native controls were not offered")
	}
	for _, connected := range []bool{false, true} {
		denied := actor
		if connected {
			denied.User.Policy = json.RawMessage(`{"RestrictedFeatures":["goby_remote_control"]}`)
		}
		for name, enabled := range nativePlaybackControls(denied, target, dto, connected) {
			if enabled {
				t.Errorf("native %s control bypassed connection or feature authorization", name)
			}
		}
	}
	target.Capabilities.SupportedCommands = nil
	delete(dto, "NowPlayingItem")
	controls := nativePlaybackControls(actor, target, dto, true)
	if controls["Stop"] || controls["Seek"] || controls["Message"] || !controls["Play"] {
		t.Fatal("idle or message-incapable client was presented as supporting active playback controls")
	}
}

func TestNativePlaybackArtworkUsesOnlyAuthenticatedLocalPaths(t *testing.T) {
	item := map[string]any{"Id": "item/with?parts", "ImageTags": map[string]string{"Primary": "tag&token=unexpected"}}
	preview := nativePlaybackArtworkURL(item)
	if preview != "/admin/v1/items/item%2Fwith%3Fparts/images/Primary/0?Tag=tag%26token%3Dunexpected" || strings.Contains(preview, "api_key") {
		t.Fatal("native artwork URL failed to escape identifiers or introduced a token carrier")
	}
	item["BackdropImageTags"] = []string{"backdrop"}
	if !strings.Contains(nativePlaybackArtworkURL(item), "/images/Backdrop/0?Tag=backdrop") {
		t.Fatal("native session artwork did not prefer a real indexed backdrop")
	}
	if nativePlaybackArtworkURL(map[string]any{"Id": "movie"}) != "" {
		t.Fatal("missing indexed artwork produced a fabricated preview URL")
	}
}
