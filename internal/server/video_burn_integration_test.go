//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPProgressiveBurnNegotiationAndHEADRevalidateExternalSubtitle(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	path := strings.TrimSuffix(fixture.path, filepath.Ext(fixture.path)) + ".en.srt"
	if err := os.WriteFile(path, []byte("1\n00:00:01,000 --> 00:00:02,000\nAuthorized caption\n"), 0600); err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: fixture.f}).rescan(t, fixture.libraryID)
	item, err := fixture.f.app.library.GetItem(fixture.f.ctx, fixture.accounts.viewer.userID, fixture.item.ID)
	if err != nil || len(item.Subtitles) != 1 {
		t.Fatal("external burn fixture did not index one subtitle")
	}
	index := item.Subtitles[0].Index
	body := videoHTTPBody(true, videoHTTPProfile("http", true, true))
	body["SubtitleStreamIndex"] = index
	body["DeviceProfile"].(map[string]any)["SubtitleProfiles"] = []map[string]any{{"Format": "srt", "Method": "Encode", "Container": "mp4", "Protocol": "http"}}
	response := fixture.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", body, fixture.accounts.viewer.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var negotiation struct {
		PlayID  string `json:"PlaySessionId"`
		Sources []struct {
			URL string `json:"TranscodingUrl"`
		} `json:"MediaSources"`
	}
	if json.Unmarshal(response.body, &negotiation) != nil || len(negotiation.Sources) != 1 || negotiation.Sources[0].URL == "" {
		t.Fatal("progressive burn negotiation did not advertise its authorized output")
	}
	parsed, err := url.Parse(negotiation.Sources[0].URL)
	if err != nil || parsed.Query().Get("SubtitleStreamIndex") != strconv.Itoa(index) || parsed.Query().Get("SubtitleDeliveryMethod") != "Encode" {
		t.Fatal("progressive burn URL omitted the selected subtitle")
	}
	head := fixture.request(t, http.MethodHead, parsed.String(), nil, nil)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	if videoHTTPJobCount(t, fixture, negotiation.PlayID, false) != 0 {
		t.Fatal("external burn negotiation or HEAD started an encoder")
	}
	// Change the file without rescanning so the indexed fingerprint cannot
	// authorize these new bytes during either negotiation or stream delivery.
	if err := os.WriteFile(path, []byte("1\n00:00:01,000 --> 00:00:02,000\nUnauthorized replacement caption\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changedHead := fixture.request(t, http.MethodHead, parsed.String(), nil, nil)
	if changedHead.status < 400 {
		t.Fatal("progressive HEAD accepted a replaced external subtitle under its old fingerprint")
	}
	changedNegotiation := fixture.request(t, http.MethodPost, "/emby/Items/"+item.ID+"/PlaybackInfo", body, fixture.accounts.viewer.headers)
	if changedNegotiation.status < 400 {
		t.Fatal("PlaybackInfo advertised a burn URL for a replaced external subtitle")
	}
	if videoHTTPJobCount(t, fixture, negotiation.PlayID, false) != 0 {
		t.Fatal("failed subtitle authorization started an encoder")
	}
}
