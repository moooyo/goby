//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type applicationPlaybackBindingWaitTrace struct{ bindings atomic.Int64 }

func (trace *applicationPlaybackBindingWaitTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "FROM play_sessions p JOIN sessions a") {
		trace.bindings.Add(1)
	}
	return ctx
}

func (*applicationPlaybackBindingWaitTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

type applicationPlaybackBindingWaitFixture struct {
	*applicationMediaFixture
	credential, client applicationMediaKey
	playID             string
	trace              *applicationPlaybackBindingWaitTrace
}

func newApplicationPlaybackBindingWaitFixture(t *testing.T) *applicationPlaybackBindingWaitFixture {
	t.Helper()
	media := newApplicationMediaFixture(t)
	fixture := &applicationPlaybackBindingWaitFixture{applicationMediaFixture: media, credential: media.keys[0]}
	fixture.client = applicationMediaClient(t, media.f, fixture.credential, identity.Client{
		Name: "Binding wait client", DeviceID: "binding-wait-device", Device: "Binding wait device", Version: "1",
	})
	media.keys[0] = fixture.client
	fixture.playID, _ = applicationMediaPrepare(t, media, 0, media.stream.video.id, http.MethodGet, "", nil)
	fixture.trace = new(applicationPlaybackBindingWaitTrace)
	configuration := media.f.pool.Config().Copy()
	configuration.ConnConfig.Tracer = fixture.trace
	pool, err := pgxpool.NewWithConfig(media.f.ctx, configuration)
	if err != nil {
		t.Fatal("create traced playback binding identity pool")
	}
	t.Cleanup(pool.Close)
	media.f.app.identity = identity.New(pool)
	return fixture
}

func TestHTTPApplicationKeyPlaybackBindingRechecksAuthorityAfterBodyWait(t *testing.T) {
	for _, change := range []struct {
		name, statement string
		status          int
	}{
		{"credential-revocation", "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", http.StatusUnauthorized},
		{"client-device-change", "UPDATE application_key_clients SET device_id = 'changed-binding-wait-device' WHERE id = $1", http.StatusNotFound},
		{"stopped-play", "UPDATE play_sessions SET state = 'Stopped', stopped_at = clock_timestamp() WHERE id = $1", http.StatusNotFound},
		{"expired-play", "UPDATE play_sessions SET state = 'Expired', expires_at = clock_timestamp() - interval '1 second', stopped_at = clock_timestamp() WHERE id = $1", http.StatusNotFound},
	} {
		t.Run(change.name, func(t *testing.T) {
			fixture := newApplicationPlaybackBindingWaitFixture(t)
			f := fixture.f
			body := &scheduledTaskHTTPGatedBody{
				reader:  strings.NewReader(`{"CurrentPlaySessionId":"` + fixture.playID + `"}`),
				entered: make(chan struct{}), release: make(chan struct{}),
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(body.release) }) }
			ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
			target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo?CurrentPlaySessionId=" + url.QueryEscape(fixture.playID)
			request := httptest.NewRequest(http.MethodPost, target, body).WithContext(ctx)
			request.SetPathValue("Id", fixture.stream.video.id)
			request.RemoteAddr = "192.0.2.76:54321"
			request.Header = fixture.credential.headers.Clone()
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			bound := make(chan bool, 1)
			handler := f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
				principal := r.Context().Value(principalKey).(identity.Principal)
				binding, ok := r.Context().Value(keyPlaybackBindingContextKey{}).(*keyPlaybackBinding)
				bound <- ok && binding != nil && binding.playID == fixture.playID &&
					binding.credentialID == fixture.client.principal.SessionID && binding.keyID == fixture.client.principal.ApplicationKeyID &&
					binding.clientID == fixture.client.principal.ClientSessionID && binding.client == fixture.client.principal.Client &&
					principal.ClientSessionID == fixture.client.principal.ClientSessionID && principal.PeerIP == "192.0.2.76"
				f.app.playbackInfo(w, r)
			})
			done := make(chan struct{})
			go func() {
				defer close(done)
				handler(response, request)
			}()
			// Release and retire the request before its catalog and pools close.
			t.Cleanup(func() {
				release()
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("playback request did not retire after the body gate was released")
				}
			})
			select {
			case <-body.entered:
			case <-done:
				t.Fatalf("playback request returned before the body wait: status %d", response.Code)
			case <-time.After(10 * time.Second):
				t.Fatal("playback request did not reach its body wait")
			}
			if !<-bound || fixture.trace.bindings.Load() != 1 {
				t.Fatal("query hint did not establish one successful binding before the body wait")
			}
			id := fixture.playID
			switch change.name {
			case "credential-revocation":
				id = fixture.client.principal.SessionID
			case "client-device-change":
				id = fixture.client.principal.ClientSessionID
			}
			mutationCtx, mutationCancel := context.WithTimeout(f.ctx, 5*time.Second)
			result, err := f.pool.Exec(mutationCtx, change.statement, id)
			mutationCancel()
			if err != nil || result.RowsAffected() != 1 {
				t.Fatal("authority mutation did not commit while the request body was waiting")
			}
			snapshotSQL := "SELECT to_jsonb(play)::text FROM play_sessions play WHERE id = $1"
			if change.name == "client-device-change" {
				// Existing cleanup may expire the old-device play after fresh
				// identity revalidation, but must not renew its expiry or owner.
				snapshotSQL = "SELECT (to_jsonb(play) - 'state' - 'stopped_at' - 'updated_at')::text FROM play_sessions play WHERE id = $1"
			}
			var before string
			if err := f.pool.QueryRow(f.ctx, snapshotSQL, fixture.playID).Scan(&before); err != nil {
				t.Fatal("snapshot the play after the authority mutation")
			}
			release()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("playback request did not finish after its body wait")
			}
			// Fresh identity authority preserves invalid-token failures, while
			// current play ownership and terminal state remain resource checks.
			applicationMediaStatus(t, response, change.status)
			if change.status == http.StatusUnauthorized {
				expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
			}
			if fixture.trace.bindings.Load() != 1 {
				t.Fatal("post-wait rejection depended on repeating the routing transaction")
			}
			var after string
			if err := f.pool.QueryRow(f.ctx, snapshotSQL, fixture.playID).Scan(&after); err != nil || after != before {
				t.Fatal("rejected continuation changed or refreshed its retained play")
			}
			if change.name == "client-device-change" {
				var state string
				if err := f.pool.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id = $1", fixture.playID).Scan(&state); err != nil || state != "Expired" {
					t.Fatal("a rebound client left the old-device play active")
				}
			}
		})
	}
}

func TestHTTPApplicationKeyPlaybackBindingRejectsRevocationAfterActivityWait(t *testing.T) {
	fixture := newApplicationPlaybackBindingWaitFixture(t)
	f := fixture.f
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	blocker, err := f.pool.Begin(ctx)
	if err != nil {
		cancel()
		t.Fatal("begin the application activity blocker")
	}
	var blockerPID int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		_ = blocker.Rollback(ctx)
		cancel()
		t.Fatal("read the application activity blocker PID")
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", fixture.client.principal.SessionID); err != nil {
		_ = blocker.Rollback(ctx)
		cancel()
		t.Fatal("lock the application credential before its activity wait")
	}
	target := "/emby/Items/" + fixture.stream.video.id + "/PlaybackInfo?CurrentPlaySessionId=" + url.QueryEscape(fixture.playID)
	request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	request.Header = fixture.credential.headers.Clone()
	request.RemoteAddr = "192.0.2.77:54321"
	response := httptest.NewRecorder()
	var entered atomic.Bool
	handler := f.app.requireEmby(func(w http.ResponseWriter, _ *http.Request) {
		entered.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(response, request)
	}()
	t.Cleanup(func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = blocker.Rollback(cleanupCtx)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("application activity request did not retire after releasing its blocker")
		}
	})
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := f.pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query = $1 AND $2::int = ANY(pg_blocking_pids(pid)))`,
			"SELECT id FROM sessions WHERE id = $1 FOR SHARE", blockerPID).Scan(&waiting)
		if err != nil {
			t.Fatal("observe the application's credential activity wait")
		}
		if waiting {
			break
		}
		select {
		case <-done:
			t.Fatalf("application request returned before its activity wait: status %d", response.Code)
		case <-waitCtx.Done():
			t.Fatal("application request did not wait on the locked activity credential")
		case <-ticker.C:
		}
	}
	if fixture.trace.bindings.Load() != 1 || entered.Load() {
		t.Fatal("activity wait did not follow one successful playback binding before handler dispatch")
	}
	if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", fixture.client.principal.SessionID); err != nil {
		t.Fatal("revoke the credential in the activity blocker transaction")
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal("commit revocation before releasing the activity wait")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("application request did not finish after the activity blocker committed")
	}
	expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
	if entered.Load() || fixture.trace.bindings.Load() != 1 {
		t.Fatal("activity wait reused stale authority or repeated its successful playback binding")
	}
}
