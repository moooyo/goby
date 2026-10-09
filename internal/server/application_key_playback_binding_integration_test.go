//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type keyPlaybackContextQueryTrace struct {
	applicationAuthQueryCounts
	selects atomic.Int64
	routing atomic.Int64
}

func (trace *keyPlaybackContextQueryTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.applicationAuthQueryCounts.TraceQueryStart(ctx, connection, data)
	if trace.enabled.Load() {
		sql := strings.ToLower(strings.TrimSpace(data.SQL))
		if strings.HasPrefix(sql, "select ") {
			trace.selects.Add(1)
		}
		if strings.Contains(sql, "from play_sessions p join sessions a on a.id = p.auth_session_id") {
			trace.routing.Add(1)
		}
	}
	return ctx
}

func (trace *keyPlaybackContextQueryTrace) reset() {
	trace.applicationAuthQueryCounts.reset()
	trace.selects.Store(0)
	trace.routing.Store(0)
}

func traceKeyPlaybackContext(t *testing.T, f *serverFixture) *keyPlaybackContextQueryTrace {
	t.Helper()
	trace := new(keyPlaybackContextQueryTrace)
	configuration := f.pool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// Only identity queries are traced. Resource authority continues through
	// the original library pool and is exercised by each real HTTP handler.
	f.app.identity = identity.New(pool)
	trace.enabled.Store(true)
	return trace
}

func prepareKeyPlaybackContext(t *testing.T, fixture *applicationMediaFixture, key applicationMediaKey) string {
	t.Helper()
	response := fixture.f.request(t, http.MethodGet, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo", nil, key.headers)
	applicationMediaStatus(t, response, http.StatusOK)
	object, _ := playbackHTTPSource(t, response)
	return stringValue(t, object, "PlaySessionId")
}

func cloneKeyPlaybackContext(t *testing.T, f *serverFixture, original, id, state string) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id,
		state, duration_ticks, expires_at, client_correlated)
		SELECT $2, user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id,
		$3, duration_ticks, clock_timestamp() + interval '1 hour', client_correlated
		FROM play_sessions WHERE id = $1`, original, id, state)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPApplicationKeyPlaybackContextBindingQueryCounts(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, credential := fixture.f, fixture.keys[0]
	alpha := applicationMediaClient(t, f, credential, identity.Client{Name: "Binding Alpha", DeviceID: "binding-alpha", Device: "Alpha", Version: "1"})
	beta := applicationMediaClient(t, f, credential, identity.Client{Name: "Binding Beta", DeviceID: "binding-beta", Device: "Beta", Version: "1"})
	playID := prepareKeyPlaybackContext(t, fixture, alpha)
	foreignID := prepareKeyPlaybackContext(t, fixture, fixture.keys[1])
	cloneKeyPlaybackContext(t, f, playID, "binding-stopped-play", "Stopped")
	cloneKeyPlaybackContext(t, f, playID, "binding-expired-play", "Expired")
	trace := traceKeyPlaybackContext(t, f)
	for _, test := range []struct {
		name, method, hint string
		body               map[string]any
		headers            http.Header
		status             int
		routing            int64
	}{
		{"query", http.MethodGet, playID, nil, credential.headers, http.StatusOK, 1},
		{"matching_query_and_body", http.MethodPost, playID, map[string]any{"CurrentPlaySessionId": playID}, credential.headers, http.StatusOK, 1},
		{"body_only", http.MethodPost, "", map[string]any{"CurrentPlaySessionId": playID}, credential.headers, http.StatusOK, 1},
		{"conflicting_query_and_body", http.MethodPost, playID, map[string]any{"CurrentPlaySessionId": foreignID}, credential.headers, http.StatusBadRequest, 1},
		{"explicit_matching_client", http.MethodGet, playID, nil, alpha.headers, http.StatusOK, 0},
		{"explicit_other_client", http.MethodGet, playID, nil, beta.headers, http.StatusNotFound, 0},
		{"missing_play", http.MethodGet, "binding-missing-play", nil, credential.headers, http.StatusNotFound, 2},
		{"foreign_play", http.MethodGet, foreignID, nil, credential.headers, http.StatusNotFound, 2},
		{"stopped_play", http.MethodGet, "binding-stopped-play", nil, credential.headers, http.StatusNotFound, 1},
		{"expired_play", http.MethodGet, "binding-expired-play", nil, credential.headers, http.StatusNotFound, 1},
		{"fresh_request", http.MethodGet, playID, nil, credential.headers, http.StatusOK, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo"
			if test.hint != "" {
				target += "?CurrentPlaySessionId=" + url.QueryEscape(test.hint)
			}
			trace.reset()
			response := f.request(t, test.method, target, test.body, test.headers)
			applicationMediaStatus(t, response, test.status)
			if got := trace.routing.Load(); got != test.routing {
				t.Fatalf("playback context routing SELECTs = %d, want %d", got, test.routing)
			}
			if test.status == http.StatusOK {
				object, _ := playbackHTTPSource(t, response)
				if object["PlaySessionId"] != playID {
					t.Fatal("PlaybackInfo changed the selected client play")
				}
			}
			t.Logf("identity_sql_statements=%d identity_selects=%d routing_selects=%d",
				trace.statements.Load(), trace.selects.Load(), trace.routing.Load())
		})
	}
	for _, itemID := range []string{"binding-missing-item", fixture.stream.audio.id} {
		t.Run("unavailable_item/"+itemID, func(t *testing.T) {
			trace.reset()
			target := "/emby/Items/" + itemID + "/PlaybackInfo?CurrentPlaySessionId=" + url.QueryEscape(playID)
			response := f.request(t, http.MethodGet, target, nil, credential.headers)
			applicationMediaStatus(t, response, http.StatusNotFound)
			if trace.routing.Load() != 1 {
				t.Fatal("resource failure changed successful request-local client routing")
			}
		})
	}
	t.Run("past_play_deadline", func(t *testing.T) {
		if _, err := f.pool.Exec(f.ctx, "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", playID); err != nil {
			t.Fatal(err)
		}
		trace.reset()
		target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo?CurrentPlaySessionId=" + url.QueryEscape(playID)
		applicationMediaStatus(t, f.request(t, http.MethodGet, target, nil, credential.headers), http.StatusNotFound)
		if trace.routing.Load() != 1 {
			t.Fatal("an expired deadline filtered identity routing before the resource check")
		}
	})
}

func TestHTTPApplicationKeyPlaybackContextBindingRequiresMatchingSuccessfulContext(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, credential := fixture.f, fixture.keys[0]
	alpha := applicationMediaClient(t, f, credential, identity.Client{Name: "Matching Alpha", DeviceID: "matching-alpha", Device: "Alpha", Version: "1"})
	beta := applicationMediaClient(t, f, credential, identity.Client{Name: "Matching Beta", DeviceID: "matching-beta", Device: "Beta", Version: "1"})
	alphaID, betaID := prepareKeyPlaybackContext(t, fixture, alpha), prepareKeyPlaybackContext(t, fixture, beta)
	trace := traceKeyPlaybackContext(t, f)
	f.app.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.5/32")}
	for _, test := range []struct {
		name                string
		statements, selects int64
		wantClient          string
		unauthorized        bool
	}{
		{"same_context", 1, 1, alpha.principal.ClientSessionID, false},
		{"different_peer", 1, 1, alpha.principal.ClientSessionID, false},
		{"different_play", 5, 3, beta.principal.ClientSessionID, false},
		{"different_credential", 5, 3, fixture.keys[1].principal.ClientSessionID, false},
		{"different_key", 3, 1, "", true},
		{"different_client", 5, 3, alpha.principal.ClientSessionID, false},
		{"different_metadata", 5, 3, alpha.principal.ClientSessionID, false},
		{"explicit_metadata", 0, 0, alpha.principal.ClientSessionID, false},
		{"invalid_credentials", 0, 0, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
				principal := r.Context().Value(principalKey).(identity.Principal)
				if principal.ClientSessionID != alpha.principal.ClientSessionID || principal.PeerIP != "203.0.113.17" {
					t.Fatal("middleware did not restore the client with its trusted proxy peer")
				}
				playID := alphaID
				switch test.name {
				case "different_peer":
					principal.PeerIP = "198.51.100.20"
				case "different_play":
					playID = betaID
				case "different_credential":
					principal = fixture.keys[1].principal
					principal.PeerIP = "203.0.113.17"
				case "different_key":
					principal.ApplicationKeyID++
				case "different_client":
					principal.ClientSessionID, principal.Client = credential.principal.ClientSessionID, credential.principal.Client
				case "different_metadata":
					principal.Client.Device = "Changed metadata"
				case "explicit_metadata":
					r.Header.Set("X-Emby-Client", "Explicit client")
				case "invalid_credentials":
					r.Header.Set("X-MediaBrowser-Token", "conflicting-token")
				}
				trace.reset()
				bound, err := f.app.bindKeyPlaybackContext(r, principal, playID)
				if test.unauthorized {
					if !errors.Is(err, identity.ErrUnauthorized) {
						t.Fatalf("mismatched authentication was not rejected: %v", err)
					}
				} else if err != nil || bound.ClientSessionID != test.wantClient || bound.PeerIP != principal.PeerIP {
					t.Fatalf("binding changed client routing or the current peer: client=%s error=%v", bound.ClientSessionID, err)
				}
				if trace.statements.Load() != test.statements || trace.selects.Load() != test.selects {
					t.Fatalf("second binding SQL = %d statements/%d SELECTs, want %d/%d",
						trace.statements.Load(), trace.selects.Load(), test.statements, test.selects)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo?CurrentPlaySessionId="+alphaID, nil).WithContext(f.ctx)
			request.Header = credential.headers.Clone()
			request.Header.Set("X-Forwarded-For", "203.0.113.17")
			request.RemoteAddr = "192.0.2.5:54321"
			response := httptest.NewRecorder()
			handler(response, request)
			applicationMediaStatus(t, response, http.StatusNoContent)
		})
	}
	t.Run("missing_hint_becomes_valid", func(t *testing.T) {
		const hint = "binding-created-between-stages"
		handler := f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
			principal := r.Context().Value(principalKey).(identity.Principal)
			cloneKeyPlaybackContext(t, f, alphaID, hint, "Stopped")
			trace.reset()
			bound, err := f.app.bindKeyPlaybackContext(r, principal, hint)
			if err != nil || bound.ClientSessionID != alpha.principal.ClientSessionID {
				t.Fatalf("an earlier missing hint prevented later successful client routing: %v", err)
			}
			if trace.statements.Load() != 5 || trace.selects.Load() != 3 || trace.routing.Load() != 1 {
				t.Fatal("an unsuccessful binding was reused without its current routing transaction")
			}
			w.WriteHeader(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodGet, "/emby/Items/"+fixture.stream.video.id+"/PlaybackInfo?CurrentPlaySessionId="+hint, nil).WithContext(f.ctx)
		request.Header = credential.headers.Clone()
		request.RemoteAddr = "203.0.113.17:54321"
		response := httptest.NewRecorder()
		handler(response, request)
		applicationMediaStatus(t, response, http.StatusNoContent)
	})
}
