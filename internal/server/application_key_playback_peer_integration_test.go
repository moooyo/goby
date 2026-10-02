//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPApplicationKeyExplicitClientMetadataFreeHLS(t *testing.T) {
	fixture := newHLSHTTPFixture(t)
	keys := applicationMediaIssueKeys(t, fixture.f, fixture.accounts.admin.headers.Get("X-Emby-Token"))
	client := applicationMediaClient(t, fixture.f, keys[0], identity.Client{
		Name: "Metadata-free HLS", DeviceID: "metadata-free-hls-device", Device: "Playback device", Version: "1",
	})
	// PlaybackInfo carries explicit metadata. Its advertised master, media
	// playlist and segment URLs authenticate from the application token alone.
	graph := applicationMediaRealGraph(t, fixture, client, fixture.accounts.viewer.userID)
	if len(graph.children) != 4 {
		t.Fatal("metadata-free media URLs lost the complete source timeline")
	}
	segment := fixture.request(t, http.MethodGet, graph.children[0], nil, nil)
	expectHLSHTTPStatus(t, segment, http.StatusOK)
	if len(segment.body) == 0 {
		t.Fatal("metadata-free HLS did not deliver actual encoder output")
	}
	stop := "/emby/Videos/ActiveEncodings?PlaySessionId=" + graph.playID
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, stop, nil, client.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodGet, graph.children[0], nil, nil), http.StatusNotFound)
}
