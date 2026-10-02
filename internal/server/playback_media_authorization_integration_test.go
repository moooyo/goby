//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type playbackMediaResponseFenceTrace struct {
	observer      *pgxpool.Pool
	active        atomic.Bool
	once          sync.Once
	statement, id string
	err           error
}

type playbackMediaResponseFenceContextKey struct{}

func (trace *playbackMediaResponseFenceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// This exact query is the fresh publication observation after opening the
	// rooted source. The transaction that authorized it must already be gone.
	if trace.active.Load() && strings.Contains(data.SQL, "WHERE i.id=$1 AND i.root_id=$2 AND i.library_id=$3") && strings.Contains(data.SQL, "publication.source_item_id=i.id") {
		return context.WithValue(ctx, playbackMediaResponseFenceContextKey{}, true)
	}
	return ctx
}

func (trace *playbackMediaResponseFenceTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(playbackMediaResponseFenceContextKey{}).(bool); matched && data.Err == nil {
		trace.once.Do(func() {
			bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, trace.err = trace.observer.Exec(bounded, trace.statement, trace.id)
		})
	}
}

func TestHTTPCachedHLSConsolidatedAuthorizationKeepsFreshResponseFence(t *testing.T) {
	for _, change := range []string{"stop", "permission", "login-revoke", "source-facts", "key-client-device"} {
		t.Run(change, func(t *testing.T) {
			h := newHLSHTTPFixture(t, 90*time.Second)
			key := applicationMediaIssueKeys(t, h.f, h.accounts.admin.headers.Get("X-Emby-Token"))[0]
			h.server.Close()
			if err := h.f.app.Close(h.f.ctx); err != nil {
				t.Fatal(err)
			}
			trace := &playbackMediaResponseFenceTrace{observer: h.f.pool}
			configuration := h.f.pool.Config()
			configuration.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pool.Close)
			users := identity.New(pool)
			app, err := New(h.f.ctx, h.f.cfg, pool, users, h.f.log, "playback-response-fence")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := app.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
			h.server = httptest.NewServer(h.f.handler)
			t.Cleanup(h.server.Close)
			var graph hlsHTTPGraph
			if change == "key-client-device" {
				graph = applicationMediaRealGraph(t, h, key, h.accounts.viewer.userID)
			} else {
				graph = h.graph(t, h.accounts.viewer, 0)
			}
			warm := h.request(t, http.MethodGet, graph.children[0], nil, nil)
			expectHLSHTTPStatus(t, warm, http.StatusOK)
			app.hls.mu.Lock()
			session := app.hls.sessions[graph.hlsID]
			app.hls.mu.Unlock()
			if session == nil {
				t.Fatal("the warmed HLS registration is missing")
			}
			want := http.StatusNotFound
			switch change {
			case "stop":
				trace.statement, trace.id = "UPDATE play_sessions SET state='Stopped',stopped_at=clock_timestamp() WHERE id=$1", graph.playID
			case "permission":
				trace.statement, trace.id, want = `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, h.accounts.viewer.userID, http.StatusForbidden
			case "login-revoke":
				trace.statement, trace.id, want = "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", session.key.scope.AuthSessionID, http.StatusUnauthorized
			case "source-facts":
				trace.statement, trace.id, want = "UPDATE items SET media=jsonb_set(media,'{FileChangeTimeNs}',to_jsonb((media->>'FileChangeTimeNs')::bigint+1)) WHERE id=$1", h.item.ID, http.StatusServiceUnavailable
			case "key-client-device":
				trace.statement, trace.id, want = "UPDATE application_key_clients SET device_id='rebound-client-device' WHERE id=$1", session.key.scope.ApplicationClientID, http.StatusUnauthorized
			}
			trace.active.Store(true)
			response := h.request(t, http.MethodGet, graph.children[0], nil, nil)
			trace.active.Store(false)
			if trace.err != nil {
				t.Fatal("authority mutation could not commit after the first stage; a database lock may have crossed I/O")
			}
			expectHLSHTTPStatus(t, response, want)
			if session.ctx.Err() == nil {
				t.Fatal("a permanent response fence failure retained its HLS registration")
			}
		})
	}
}
