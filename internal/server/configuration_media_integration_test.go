//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func configurationMediaHLS(t *testing.T, fixture *hlsHTTPFixture, login clientSessionHTTPLogin) (*hlsSession, string, string) {
	t.Helper()
	body := map[string]any{"UserId": login.userID, "IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false,
		"EnableTranscoding": true, "AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "SegmentLength": 3,
		}}}}
	response := fixture.request(t, http.MethodPost, "/emby/Items/"+fixture.item.ID+"/PlaybackInfo", body, login.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var result struct {
		PlaySessionID string           `json:"PlaySessionId"`
		MediaSources  []map[string]any `json:"MediaSources"`
		ErrorCode     string           `json:"ErrorCode"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil || result.ErrorCode != "" || len(result.MediaSources) != 1 {
		t.Fatal("configuration media negotiation did not return one supported output")
	}
	masterURL, ok := result.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || result.MediaSources[0]["SupportsTranscoding"] != true {
		t.Fatal("configuration media negotiation omitted HLS conversion")
	}
	parsed := hlsHTTPURL(t, masterURL, login.headers.Get("X-Emby-Token"))
	principal, err := fixture.f.users.ResolveEmby(fixture.f.ctx, login.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatalf("resolve the configuration media client (%T)", err)
	}
	session, err := fixture.f.app.hls.find(parsed.Query().Get("GobyHlsId"), principal, fixture.item.ID)
	if err != nil || session.key.scope.PlaySessionID != result.PlaySessionID {
		t.Fatal("configuration negotiation did not register its HLS identity")
	}
	master := fixture.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	mainURLs := hlsHTTPManifestChildren(master.body)
	if len(mainURLs) != 1 {
		t.Fatal("configuration HLS master omitted its main playlist")
	}
	main := fixture.request(t, http.MethodGet, mainURLs[0], nil, nil)
	expectHLSHTTPStatus(t, main, http.StatusOK)
	segments := hlsHTTPManifestChildren(main.body)
	if len(segments) != 4 {
		t.Fatal("configuration HLS changed the complete source timeline")
	}
	return session, masterURL, segments[0]
}

func TestHTTPConfigurationEncodingCeilingAffectsNewMediaAndPreservesNativeWidth(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	f := fixture.f
	cookie, csrf := f.adminLogin(t)
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		map[string]any{"Revision": initial["Revision"], "Overrides": map[string]any{
			"ServerName": nil, "MaxBitrate": 2_000_000, "MaxWidth": 160, "MaxHeight": 90, "MaxAudioChannels": 2,
		}}), http.StatusOK)
	old, oldMaster, oldSegment := configurationMediaHLS(t, fixture, fixture.accounts.viewer)
	oldKey := old.key
	oldOutput := fixture.request(t, http.MethodGet, oldSegment, nil, nil)
	expectHLSHTTPStatus(t, oldOutput, http.StatusOK)
	if width, height := settingsMediaVideoDimensions(t, fixture, oldOutput.body); width != 160 || height != 90 || oldKey.plan.Width != 160 {
		t.Fatal("native width did not establish an actual 160 by 90 registered output")
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/encoding", map[string]any{"TranscodingMaxWidth": 96}, fixture.accounts.admin.headers), http.StatusNoContent)
	native := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	if objectValue(t, native, "Effective")["MaxWidth"] != float64(160) || objectValue(t, native, "Overrides")["MaxWidth"] != float64(160) ||
		objectValue(t, native, "Encoding")["TranscodingMaxWidth"] != float64(96) {
		t.Fatal("compatibility width replaced the native width instead of adding a ceiling")
	}
	// The registered output may already be complete. This verifies retention,
	// without claiming that a running encoder was held across the update.
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, oldMaster, nil, nil), http.StatusOK)
	retained := fixture.request(t, http.MethodGet, oldSegment, nil, nil)
	expectHLSHTTPStatus(t, retained, http.StatusOK)
	if old.key != oldKey || !bytes.Equal(retained.body, oldOutput.body) {
		t.Fatal("compatibility update rewrote a registered plan or its published bytes")
	}
	narrow, _, narrowSegment := configurationMediaHLS(t, fixture, fixture.accounts.second)
	if narrow.id == old.id || narrow.key.plan.Width != 96 || narrow.key.plan.Height != 54 {
		t.Fatal("new planning did not consume the independent compatibility ceiling")
	}
	narrowOutput := fixture.request(t, http.MethodGet, narrowSegment, nil, nil)
	expectHLSHTTPStatus(t, narrowOutput, http.StatusOK)
	if width, height := settingsMediaVideoDimensions(t, fixture, narrowOutput.body); width != 96 || height != 54 {
		t.Fatal("compatibility width did not produce actual 96 by 54 media")
	}
	configurationHTTPStatus(t, configurationHTTPRaw(f, http.MethodPost, "/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":0}`, "application/octet-stream", fixture.accounts.admin.headers), http.StatusNoContent)
	cleared := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	if objectValue(t, cleared, "Effective")["MaxWidth"] != float64(160) || objectValue(t, cleared, "Encoding")["TranscodingMaxWidth"] != float64(0) {
		t.Fatal("clearing the extra ceiling changed native output policy")
	}
	restored, _, restoredSegment := configurationMediaHLS(t, fixture, fixture.accounts.admin)
	if restored.key.scope == oldKey.scope || restored.id == old.id || restored.key.plan.Width != 160 || restored.key.plan.Height != 90 {
		t.Fatal("a fresh scope did not restore planning under the retained native width")
	}
	restoredOutput := fixture.request(t, http.MethodGet, restoredSegment, nil, nil)
	expectHLSHTTPStatus(t, restoredOutput, http.StatusOK)
	if width, height := settingsMediaVideoDimensions(t, fixture, restoredOutput.body); width != 160 || height != 90 {
		t.Fatal("zero compatibility ceiling did not restore actual native-width output")
	}
	if old.key != oldKey || narrow.key.plan.Width != 96 {
		t.Fatal("a later configuration update changed an older registered plan")
	}
}
