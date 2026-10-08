//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPApplicationKeyDynamicNegotiationPreservesTrustedPeerWithoutProfile(t *testing.T) {
	for _, mode := range []struct {
		name     string
		autoOpen bool
	}{
		{name: "automatic", autoOpen: true},
		{name: "explicit"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			fixture := newApplicationMediaFixture(t)
			f, key := fixture.f, fixture.keys[0]
			if err := f.app.dynamicSources.Close(f.ctx); err != nil {
				t.Fatal("close the initial dynamic source manager")
			}
			manager, err := dynamicsource.New(f.ctx, []dynamicsource.Definition{{
				ItemID: fixture.stream.video.id, URL: "https://configured.invalid/private-dynamic-media", Infinite: true,
			}}, dynamicsource.Options{Connector: dynamicTestConnector{}, Authorize: f.app.authorizeDynamicSource})
			if err != nil {
				t.Fatal("create the authorized dynamic source manager")
			}
			f.app.dynamicSources = manager

			const trustedPeer = "198.51.100.27"
			var previouslyAuthenticated identity.Principal
			// Observe the real revalidation input so an empty transport peer cannot
			// accidentally turn this into a passing owner comparison.
			f.app.dynamicStreams.revalidate = func(ctx context.Context, principal identity.Principal) (identity.Principal, error) {
				previouslyAuthenticated = principal
				return f.users.RevalidateSession(ctx, principal)
			}
			request := func(target string, body map[string]any) *httptest.ResponseRecorder {
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal("encode dynamic negotiation request")
				}
				r := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(encoded)).WithContext(f.ctx)
				r.RemoteAddr = trustedPeer + ":43128"
				r.Header = key.headers.Clone()
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-Forwarded-For", "203.0.113.99")
				response := httptest.NewRecorder()
				f.handler.ServeHTTP(response, r)
				return response
			}

			// No DeviceProfile or user context is needed to inspect an opened
			// source. Both HTTP entry points still revalidate the key's identity.
			playbackTarget := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo"
			response := request(playbackTarget, map[string]any{"AutoOpenLiveStream": mode.autoOpen})
			applicationMediaStatus(t, response, http.StatusOK)
			object, source := playbackHTTPSource(t, response)
			playID := stringValue(t, object, "PlaySessionId")
			if !mode.autoOpen {
				if source["RequiresOpening"] != true {
					t.Fatal("deferred dynamic negotiation did not require explicit opening")
				}
				response = request("/emby/LiveStreams/Open", map[string]any{
					"OpenToken": stringValue(t, source, "OpenToken"), "ItemId": fixture.stream.video.id, "PlaySessionId": playID,
				})
				applicationMediaStatus(t, response, http.StatusOK)
				source = objectValue(t, jsonObject(t, response), "MediaSource")
			}
			if previouslyAuthenticated.PeerIP != trustedPeer || !previouslyAuthenticated.IsApplicationKey() ||
				previouslyAuthenticated.SessionID != key.principal.SessionID || previouslyAuthenticated.ClientSessionID != key.principal.ClientSessionID {
				t.Fatal("dynamic negotiation did not revalidate the authenticated key and transport peer")
			}
			liveID := stringValue(t, source, "LiveStreamId")
			if source["RequiresOpening"] != false || source["RequiresClosing"] != true || source["IsInfiniteStream"] != true {
				t.Fatal("metadata-only negotiation did not retain its opened dynamic source lease")
			}
			streams, ok := source["MediaStreams"].([]any)
			if !ok || len(streams) != len(dynamicTestInfo().Streams) {
				t.Fatal("metadata-only negotiation lost observed dynamic source streams")
			}
			if _, exists := source["TranscodingUrl"]; exists {
				t.Fatal("negotiation without a device profile invented an output capability")
			}
			applicationMediaAssertPublic(t, source)
			mediaInfoTarget := "/emby/LiveStreams/MediaInfo?LiveStreamId=" + url.QueryEscape(liveID)
			info := request(mediaInfoTarget, map[string]any{})
			applicationMediaStatus(t, info, http.StatusOK)
			if stringValue(t, jsonObject(t, info), "LiveStreamId") != liveID {
				t.Fatal("dynamic source metadata did not preserve its opened lease")
			}

			if _, err := f.users.RevokeApplicationKey(f.ctx, fixture.keys[1].principal, key.key.ID); err != nil {
				t.Fatal("revoke the dynamic playback key")
			}
			if _, err := f.app.freshDynamicPrincipal(f.ctx, previouslyAuthenticated); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("dynamic revalidation retained a revoked application credential")
			}
			applicationMediaStatus(t, request(mediaInfoTarget, map[string]any{}), http.StatusUnauthorized)
			applicationMediaStatus(t, request(playbackTarget, map[string]any{"AutoOpenLiveStream": true}), http.StatusUnauthorized)
		})
	}
}
