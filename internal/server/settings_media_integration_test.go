//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func settingsMediaVideoDimensions(t *testing.T, fixture *hlsHTTPFixture, data []byte) (int, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "received-settings-segment.ts")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("save received settings media segment (%T)", err)
	}
	encoded := hlsHTTPMediaCommand(t, fixture.ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height", "-of", "json", path)
	var probe struct {
		Streams []struct {
			Codec         string `json:"codec_name"`
			Width, Height int
		} `json:"streams"`
	}
	if err := json.Unmarshal(encoded, &probe); err != nil || len(probe.Streams) != 1 || probe.Streams[0].Codec != "h264" {
		t.Fatal("settings media response was not a single H.264 video stream")
	}
	hlsHTTPMediaCommand(t, fixture.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-f", "null", "-")
	return probe.Streams[0].Width, probe.Streams[0].Height
}

func TestHTTPSettingsMediaPreservesRegisteredHLSAndAppliesNewPlanLimits(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	f := fixture.f
	cookie, csrf := f.adminLogin(t)
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	wide := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		map[string]any{"Revision": initial["Revision"], "Overrides": map[string]any{
			"ServerName": nil, "MaxBitrate": 2_000_000, "MaxWidth": 160, "MaxHeight": 90, "MaxAudioChannels": 2,
		}}), http.StatusOK)
	negotiate := func(login clientSessionHTTPLogin) (*hlsSession, string, string) {
		t.Helper()
		body := map[string]any{"UserId": login.userID, "IsPlayback": true, "EnableDirectPlay": false,
			"EnableDirectStream": false, "EnableTranscoding": true, "AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false,
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
			t.Fatal("settings-backed negotiation did not return one supported source")
		}
		masterURL, ok := result.MediaSources[0]["TranscodingUrl"].(string)
		if !ok || result.MediaSources[0]["SupportsTranscoding"] != true {
			t.Fatal("settings-backed negotiation omitted its HLS conversion")
		}
		parsed := hlsHTTPURL(t, masterURL, login.headers.Get("X-Emby-Token"))
		principal, err := f.users.ResolveEmby(f.ctx, login.headers.Get("X-Emby-Token"))
		if err != nil {
			t.Fatalf("resolve settings media fixture client (%T)", err)
		}
		session, err := f.app.hls.find(parsed.Query().Get("GobyHlsId"), principal, fixture.item.ID)
		if err != nil || session.key.scope.PlaySessionID != result.PlaySessionID {
			t.Fatal("negotiation did not register the returned HLS output identity")
		}
		master := fixture.request(t, http.MethodGet, masterURL, nil, nil)
		expectHLSHTTPStatus(t, master, http.StatusOK)
		mainURLs := hlsHTTPManifestChildren(master.body)
		if len(mainURLs) != 1 {
			t.Fatal("settings HLS master omitted its media playlist")
		}
		main := fixture.request(t, http.MethodGet, mainURLs[0], nil, nil)
		expectHLSHTTPStatus(t, main, http.StatusOK)
		segments := hlsHTTPManifestChildren(main.body)
		if len(segments) != 4 {
			t.Fatal("settings HLS playlist lost the complete fixture timeline")
		}
		return session, masterURL, segments[0]
	}
	old, oldMasterURL, oldSegmentURL := negotiate(fixture.accounts.viewer)
	oldKey := old.key
	if oldKey.plan.Width != 160 || oldKey.plan.Height != 90 || oldKey.plan.VideoCodec != "h264" || oldKey.plan.AudioCodec != "aac" {
		t.Fatal("wide settings were not applied to the registered output plan")
	}
	oldOutput := fixture.request(t, http.MethodGet, oldSegmentURL, nil, nil)
	expectHLSHTTPStatus(t, oldOutput, http.StatusOK)
	if width, height := settingsMediaVideoDimensions(t, fixture, oldOutput.body); width != 160 || height != 90 {
		t.Fatal("wide settings did not produce actual 160 by 90 media")
	}
	const narrowBitrate int64 = 240_000
	narrow := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		map[string]any{"Revision": wide["Revision"], "Overrides": map[string]any{
			"ServerName": nil, "MaxBitrate": narrowBitrate, "MaxWidth": 96, "MaxHeight": 54, "MaxAudioChannels": 1,
		}}), http.StatusOK)
	if narrow["Revision"] == wide["Revision"] {
		t.Fatal("narrower settings did not publish a new revision")
	}
	if oldKey.plan.VideoBitrate+oldKey.plan.AudioBitrate <= narrowBitrate*9/10 {
		t.Fatal("fixture did not retain a plan that exceeds the newly reduced payload budget")
	}
	// This proves preservation of an already registered output. Its producer
	// may already have completed; the test does not claim an in-flight update.
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, oldMasterURL, nil, nil), http.StatusOK)
	retained := fixture.request(t, http.MethodGet, oldSegmentURL, nil, nil)
	expectHLSHTTPStatus(t, retained, http.StatusOK)
	if old.key != oldKey || !bytes.Equal(retained.body, oldOutput.body) {
		t.Fatal("settings update changed the old HLS plan, identity, or published media bytes")
	}
	if width, height := settingsMediaVideoDimensions(t, fixture, retained.body); width != 160 || height != 90 {
		t.Fatal("registered HLS output adopted new dimensions after the settings update")
	}
	updated, _, updatedSegmentURL := negotiate(fixture.accounts.second)
	if updated.id == old.id || updated.key.scope == oldKey.scope || updated.key.plan.Width != 96 || updated.key.plan.Height != 54 ||
		updated.key.plan.VideoBitrate+updated.key.plan.AudioBitrate > narrowBitrate*9/10 || updated.key.plan.AudioChannels > 1 {
		t.Fatal("new HLS scope did not use the reduced output limits")
	}
	newOutput := fixture.request(t, http.MethodGet, updatedSegmentURL, nil, nil)
	expectHLSHTTPStatus(t, newOutput, http.StatusOK)
	if width, height := settingsMediaVideoDimensions(t, fixture, newOutput.body); width != 96 || height != 54 {
		t.Fatal("new settings did not produce actual 96 by 54 media")
	}
	if old.key != oldKey {
		t.Fatal("creating a new output rewrote the prior registered plan")
	}
}

func TestHTTPSettingsServerNameUpdatesPublicViewsAndNewKeySnapshots(t *testing.T) {
	fixture := newApplicationKeyHTTPFixture(t)
	f := fixture.serverFixture
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, fixture.cookie), http.StatusOK)
	oldName := stringValue(t, objectValue(t, initial, "Effective"), "ServerName")
	startupName := f.app.cfg.ServerName
	oldKey := fixture.create(t, "Before server rename")
	readKey := func(key applicationKeyHTTPSecret) (string, string, string) {
		t.Helper()
		var credentialName, clientName, snapshot string
		if err := f.pool.QueryRow(f.ctx, `SELECT a.device_name, c.device_name,
			jsonb_build_object('credential', to_jsonb(a), 'default_client', to_jsonb(c))::text
			FROM application_keys k JOIN sessions a ON a.id = k.credential_id
			JOIN application_key_clients c ON c.credential_id = a.id AND c.client_name = a.client_name AND c.device_id = a.device_id
			WHERE k.id = $1`, key.numericID(t)).Scan(&credentialName, &clientName, &snapshot); err != nil {
			t.Fatalf("read key name snapshots (%T)", err)
		}
		return credentialName, clientName, snapshot
	}
	credentialName, clientName, oldSnapshot := readKey(oldKey)
	if credentialName != oldName || clientName != oldName {
		t.Fatal("initial key did not snapshot the effective server name")
	}
	const newName = "Renamed settings integration server"
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, fixture.cookie, fixture.csrf, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate(stringValue(t, initial, "Revision"), newName, nil)), http.StatusOK)
	public := f.request(t, http.MethodGet, "/emby/System/Info/Public", nil, nil)
	expectStatus(t, public, http.StatusOK)
	if jsonObject(t, public)["ServerName"] != newName {
		t.Fatal("public system information did not observe the committed server name")
	}
	overview := f.request(t, http.MethodGet, "/admin/v1/overview", nil, nil, fixture.cookie)
	expectStatus(t, overview, http.StatusOK)
	if objectValue(t, jsonObject(t, overview), "Server")["Name"] != newName {
		t.Fatal("administrator overview did not use the same effective server name")
	}
	credentialName, clientName, unchanged := readKey(oldKey)
	if credentialName != oldName || clientName != oldName || unchanged != oldSnapshot {
		t.Fatal("renaming the server rewrote an existing key or default client snapshot")
	}
	newKey := fixture.create(t, "After server rename")
	credentialName, clientName, _ = readKey(newKey)
	if credentialName != newName || clientName != newName {
		t.Fatal("a subsequently created key did not capture the new effective server name")
	}
	_, _, unchanged = readKey(oldKey)
	if unchanged != oldSnapshot || f.app.cfg.ServerName != startupName {
		t.Fatal("new key creation changed an old client snapshot or mutated startup configuration")
	}
}
