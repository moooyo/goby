//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGeneratedHLSHTTPGraphIncludesDistinctAuthenticatedMapsAndSegments(t *testing.T) {
	h := newHLSHTTPFixture(t)
	login := h.accounts.viewer
	request := map[string]any{"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "EnableAdaptiveBitrate": true, "SegmentLength": 3}}}}
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", request, login.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var negotiation struct {
		PlaySessionID string           `json:"PlaySessionId"`
		MediaSources  []map[string]any `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.body, &negotiation); err != nil || len(negotiation.MediaSources) != 1 {
		t.Fatal("invalid negotiation")
	}
	masterURL, ok := negotiation.MediaSources[0]["TranscodingUrl"].(string)
	if !ok || negotiation.MediaSources[0]["TranscodingContainer"] != "mp4" {
		t.Fatal("missing negotiated fMP4 output")
	}
	master := h.request(t, http.MethodGet, masterURL, nil, nil)
	expectHLSHTTPStatus(t, master, http.StatusOK)
	variants := hlsHTTPManifestChildren(master.body)
	if len(variants) < 2 || variants[0] == variants[1] {
		t.Fatal("master does not advertise separate variants")
	}
	token := login.headers.Get("X-Emby-Token")
	var artifactURLs []string
	for _, variant := range variants {
		hlsHTTPURL(t, variant, token)
		playlist := h.request(t, http.MethodGet, variant, nil, nil)
		expectHLSHTTPStatus(t, playlist, http.StatusOK)
		segments := hlsHTTPManifestChildren(playlist.body)
		if len(segments) == 0 {
			t.Fatal("empty generated media playlist")
		}
		mapURI := ""
		for _, line := range strings.Split(string(playlist.body), "\n") {
			if strings.HasPrefix(line, "#EXT-X-MAP:URI=\"") {
				mapURI = strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
			}
		}
		if mapURI == "" {
			t.Fatal("missing fMP4 initialization map")
		}
		artifactURLs = append(artifactURLs, variant, mapURI, segments[0])
		for _, child := range []string{mapURI, segments[0]} {
			parsed := hlsHTTPURL(t, child, token)
			media := h.request(t, http.MethodGet, child, nil, nil)
			expectHLSHTTPStatus(t, media, http.StatusOK)
			if len(media.body) == 0 {
				t.Fatal("empty HLS artifact")
			}
			query := parsed.Query()
			query.Del("api_key")
			parsed.RawQuery = query.Encode()
			// The unmodified URL names the first player's device and is rejected
			// by the existing device binding before revision ownership lookup.
			expectHLSHTTPStatus(t, h.request(t, http.MethodGet, parsed.String(), nil, h.accounts.second.headers), http.StatusForbidden)
			query.Set("DeviceId", h.accounts.second.deviceID)
			parsed.RawQuery = query.Encode()
			expectHLSHTTPStatus(t, h.request(t, http.MethodGet, parsed.String(), nil, h.accounts.second.headers), http.StatusNotFound)
		}
	}
	stop := "/emby/Videos/ActiveEncodings?" + url.Values{"PlaySessionId": {negotiation.PlaySessionID}, "DeviceId": {login.deviceID}}.Encode()
	expectHLSHTTPStatus(t, h.request(t, http.MethodDelete, stop, nil, login.headers), http.StatusNoContent)
	for _, artifact := range artifactURLs {
		expectHLSHTTPStatus(t, h.request(t, http.MethodGet, artifact, nil, nil), http.StatusNotFound)
	}
}
