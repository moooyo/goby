//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type playbackControlFixture struct {
	*serverFixture
	control *pgxpool.Pool
	actor   identity.Principal
}

type playbackControlAcquireContextKey struct{}

// A request-specific witness records attempted acquisitions, including ones
// that fail before borrowing a connection. Whole-pool statistics also include
// legitimate background activity and cannot prove request routing by itself.
type playbackControlAcquireCounts struct {
	data, control                                          *pgxpool.Pool
	dataAttempts, controlAttempts, unexpectedAttempts      atomic.Int64
	dataSuccesses, controlSuccesses, controlDeadlineErrors atomic.Int64
}

type playbackControlAcquireTracer struct{}

func (playbackControlAcquireTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (playbackControlAcquireTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (playbackControlAcquireTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	counts, _ := ctx.Value(playbackControlAcquireContextKey{}).(*playbackControlAcquireCounts)
	if counts == nil {
		return ctx
	}
	switch pool {
	case counts.data:
		counts.dataAttempts.Add(1)
	case counts.control:
		counts.controlAttempts.Add(1)
	default:
		counts.unexpectedAttempts.Add(1)
	}
	return ctx
}

func (playbackControlAcquireTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	counts, _ := ctx.Value(playbackControlAcquireContextKey{}).(*playbackControlAcquireCounts)
	if counts == nil {
		return
	}
	if data.Err == nil {
		if pool == counts.data {
			counts.dataSuccesses.Add(1)
		} else if pool == counts.control {
			counts.controlSuccesses.Add(1)
		}
	} else if pool == counts.control && errors.Is(data.Err, context.DeadlineExceeded) {
		counts.controlDeadlineErrors.Add(1)
	}
}

func (f *playbackControlFixture) traceAcquisitions(ctx context.Context) (context.Context, *playbackControlAcquireCounts) {
	counts := &playbackControlAcquireCounts{data: f.pool, control: f.control}
	return context.WithValue(ctx, playbackControlAcquireContextKey{}, counts), counts
}

func newPlaybackControlFixture(t *testing.T) *playbackControlFixture {
	t.Helper()
	f := newServerFixture(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("close the initial fixture before reserving control capacity")
	}
	config := f.pool.Config().Copy()
	config.MaxConns = database.DataMaxConns
	config.ConnConfig.Tracer = playbackControlAcquireTracer{}
	f.pool.Close()
	data, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal("open bounded fixture data capacity")
	}
	t.Cleanup(data.Close)
	f.pool = data
	control, err := database.OpenPlaybackControlFor(f.ctx, data)
	if err != nil {
		t.Fatal("open reserved fixture control capacity")
	}
	t.Cleanup(control.Close)
	f.users = identity.NewWithApplicationKeyVault(data, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "control-master.key")))
	app, err := New(f.ctx, f.cfg, data, f.users, f.log, "control-integration", WithPlaybackControlPool(control))
	if err != nil {
		t.Fatal("create the reserved-capacity server")
	}
	t.Cleanup(func() {
		if err := app.Close(context.Background()); err != nil {
			t.Error("close reserved-capacity workers")
		}
	})
	f.app, f.handler = app, app.Handler()
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve fixture administrator")
	}
	if _, err := data.Exec(f.ctx, `INSERT INTO libraries(id, name, collection_type)
		VALUES ('control-library', 'Control Library', 'movies');
		INSERT INTO items(id, library_id, name, sort_name, type, media)
		VALUES ('control-item', 'control-library', 'Control Movie', 'control movie', 'Movie',
		jsonb_build_object('DurationTicks', 6000000000::bigint))`); err != nil {
		t.Fatal("insert playback-control catalog facts")
	}
	return &playbackControlFixture{serverFixture: f, control: control, actor: actor}
}

func (f *playbackControlFixture) principal(t *testing.T, kind string) (identity.Principal, http.Header) {
	t.Helper()
	client := identity.Client{Name: "Control Client", DeviceID: "control-device", Device: "Control Device", Version: "1"}
	var token string
	if kind == "normal" {
		credentials, err := f.users.Authenticate(f.ctx, "Administrator", "administrator-password", client, "emby")
		if err != nil {
			t.Fatal("authenticate the ordinary fixture client")
		}
		token = credentials.Token
	} else {
		key, err := f.users.CreateApplicationKey(f.ctx, f.actor, "Control Key", "", identity.Client{DeviceID: "control-default-device"})
		if err != nil {
			t.Fatal("create application credential")
		}
		token = key.Token
	}
	principal, err := f.users.ResolveEmbyForClient(f.ctx, token, client)
	if err != nil {
		t.Fatal("resolve the fixture playback owner")
	}
	headers := http.Header{"X-Emby-Token": {token}}
	if kind == "key-explicit" {
		headers.Set("Authorization", `Emby Client="Control Client", DeviceId="control-device", Device="Control Device", Version="1"`)
	}
	return principal, headers
}

func (f *playbackControlFixture) play(t *testing.T, principal identity.Principal, dynamic bool) library.PlaySession {
	t.Helper()
	var play library.PlaySession
	var err error
	if dynamic {
		play, err = f.app.library.PrepareDynamicPlayback(f.ctx, playbackOwner(principal), "control-item", media.SourceID("control-item"), "")
	} else {
		play, err = f.app.library.PreparePlayback(f.ctx, playbackOwner(principal), "control-item", media.SourceID("control-item"), "")
	}
	if err != nil {
		t.Fatal("prepare the fixture playback")
	}
	play, _, err = f.app.library.ReportPlayback(f.ctx, playbackOwner(principal), library.PlaybackReport{Event: "Started", PlaySessionID: play.ID})
	if err != nil {
		t.Fatal("start the fixture playback")
	}
	return play
}

func holdPlaybackPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool) func() {
	t.Helper()
	var held []*pgxpool.Conn
	for pool.Stat().AcquiredConns() < pool.Config().MaxConns {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			for _, value := range held {
				value.Release()
			}
			t.Fatal("reserve all remaining fixture pool capacity")
		}
		held = append(held, connection)
	}
	var released bool
	release := func() {
		if !released {
			released = true
			for _, connection := range held {
				connection.Release()
			}
		}
	}
	t.Cleanup(release)
	return release
}

func playbackControlRequest(ctx context.Context, handler http.Handler, target string, body any, headers http.Header) *httptest.ResponseRecorder {
	var content bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&content).Encode(body)
	}
	request := httptest.NewRequest(http.MethodPost, target, &content).WithContext(ctx)
	request.Header = headers.Clone()
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHTTPPlaybackControlUsesReservedCapacityWhenDataIsExhausted(t *testing.T) {
	for _, kind := range []string{"normal", "key-explicit", "key-metadata-free"} {
		t.Run(kind, func(t *testing.T) {
			f := newPlaybackControlFixture(t)
			principal, headers := f.principal(t, kind)
			play := f.play(t, principal, false)
			// Force due authentication activity to exercise its write transaction.
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1", principal.SessionID); err != nil {
				t.Fatal(err)
			}
			runtime, jobs := hlsRuntimeTestFixture(t)
			runtime.server = f.app
			f.app.hls = runtime
			defer jobs.releaseAll()
			release := holdPlaybackPool(t, f.ctx, f.pool)
			defer release()
			before := f.pool.Stat().AcquireCount()
			requestCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
			defer cancel()
			ping := "/emby/Sessions/Playing/Ping?PlaySessionId=" + url.QueryEscape(play.ID)
			expectStatus(t, playbackControlRequest(requestCtx, f.handler, ping, nil, headers), http.StatusNoContent)
			body := map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": play.MediaSourceID}
			expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Stopped", body, headers), http.StatusNoContent)
			if f.pool.Stat().AcquireCount() != before || requestCtx.Err() != nil {
				t.Fatal("playback control borrowed data capacity or exceeded its deadline")
			}
			var state string
			if err := f.control.QueryRow(requestCtx, "SELECT state FROM play_sessions WHERE id=$1", play.ID).Scan(&state); err != nil || state != "Stopped" {
				t.Fatal("the reserved transaction did not commit playback retirement")
			}
			jobs.mu.Lock()
			cancelled := append([]hlsRuntimeOpenCall(nil), jobs.playbackCancels...)
			jobs.mu.Unlock()
			if len(cancelled) != 1 || cancelled[0].scope.AuthSessionID != principal.SessionID || cancelled[0].scope.PlaySessionID != play.ID {
				t.Fatal("stopped control did not promptly retire the exact producer owner")
			}
			total, lanes := f.app.databasePoolResourceLanes()
			if total.MaxConns != database.ApplicationMaxConns || lanes.Data.MaxConns != database.DataMaxConns || lanes.PlaybackControl == nil || lanes.PlaybackControl.MaxConns != database.PlaybackControlMaxConns {
				t.Fatal("runtime statistics do not expose the actual application pool budget")
			}
		})
	}
}

func TestHTTPPlaybackControlDoesNotFallbackOrAuthorizeForeignPlayback(t *testing.T) {
	f := newPlaybackControlFixture(t)
	principal, headers := f.principal(t, "normal")
	play := f.play(t, principal, false)
	foreign, _ := f.principal(t, "key-explicit")
	foreignPlay := f.play(t, foreign, false)
	releaseData := holdPlaybackPool(t, f.ctx, f.pool)
	defer releaseData()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	requestCtx, requestCounts := f.traceAcquisitions(ctx)
	before := f.pool.Stat().AcquireCount()
	expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId=unknown-play", nil, headers), http.StatusNoContent)
	expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+foreignPlay.ID, nil, headers), http.StatusNoContent)
	var foreignState string
	if err := f.control.QueryRow(ctx, "SELECT state FROM play_sessions WHERE id=$1", foreignPlay.ID).Scan(&foreignState); err != nil || foreignState != "Playing" {
		t.Fatal("foreign Ping changed another playback owner")
	}
	if _, err := f.control.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", principal.User.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, headers), http.StatusUnauthorized)
	if _, err := f.control.Exec(ctx, "UPDATE users SET is_disabled=false WHERE id=$1", principal.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.control.Exec(ctx, "UPDATE sessions SET created_at=clock_timestamp()-interval '2 days', expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, headers), http.StatusUnauthorized)
	if _, err := f.control.Exec(ctx, "UPDATE sessions SET expires_at=clock_timestamp()+interval '1 day' WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.control.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, playbackControlRequest(requestCtx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, headers), http.StatusUnauthorized)
	if requestCounts.dataAttempts.Load() != 0 || requestCounts.unexpectedAttempts.Load() != 0 || requestCounts.controlAttempts.Load() == 0 {
		t.Fatalf("failed or unknown control routing mismatch: request_data_attempts=%d request_data_successes=%d request_control_attempts=%d request_unexpected_attempts=%d whole_data_acquires_before=%d whole_data_acquires_after=%d",
			requestCounts.dataAttempts.Load(), requestCounts.dataSuccesses.Load(), requestCounts.controlAttempts.Load(), requestCounts.unexpectedAttempts.Load(), before, f.pool.Stat().AcquireCount())
	}
	releaseData()
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=NULL WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	// The later deadline must come from reserved-pool exhaustion, not a stale
	// expiry, disablement, revocation, device, or access-policy fast denial.
	if _, err := f.users.ResolveWithPeer(f.ctx, headers.Get("X-Emby-Token"), "emby", "192.0.2.1"); err != nil {
		t.Fatalf("restored fixture authentication is not live: error_type=%T", err)
	}
	releaseControl := holdPlaybackPool(t, f.ctx, f.control)
	defer releaseControl()
	before = f.pool.Stat().AcquireCount()
	deadline, stop := context.WithTimeout(f.ctx, 100*time.Millisecond)
	defer stop()
	taggedDeadline, exhaustedCounts := f.traceAcquisitions(deadline)
	response := playbackControlRequest(taggedDeadline, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, headers)
	if response.Code == http.StatusNoContent || !errors.Is(deadline.Err(), context.DeadlineExceeded) ||
		exhaustedCounts.dataAttempts.Load() != 0 || exhaustedCounts.dataSuccesses.Load() != 0 || exhaustedCounts.unexpectedAttempts.Load() != 0 ||
		exhaustedCounts.controlAttempts.Load() == 0 || exhaustedCounts.controlSuccesses.Load() != 0 || exhaustedCounts.controlDeadlineErrors.Load() == 0 {
		t.Fatalf("exhausted control routing mismatch: response_status=%d context_error=%v request_data_attempts=%d request_data_successes=%d request_control_attempts=%d request_control_successes=%d request_control_deadline_errors=%d request_unexpected_attempts=%d whole_data_acquires_before=%d whole_data_acquires_after=%d",
			response.Code, deadline.Err(), exhaustedCounts.dataAttempts.Load(), exhaustedCounts.dataSuccesses.Load(), exhaustedCounts.controlAttempts.Load(),
			exhaustedCounts.controlSuccesses.Load(), exhaustedCounts.controlDeadlineErrors.Load(), exhaustedCounts.unexpectedAttempts.Load(), before, f.pool.Stat().AcquireCount())
	}
}

func TestHTTPPlaybackControlStopsRealFFmpegWhenDataIsExhausted(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real playback-control process verification")
	}
	f := newPlaybackControlFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "Control.HLS.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=12",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", source)
	wrapperRoot := t.TempDir()
	wrapper, pidFile := filepath.Join(wrapperRoot, "ffmpeg-control"), filepath.Join(wrapperRoot, "encoder.pid")
	script := "#!/bin/sh\ncase \" $* \" in *\" -progress \"*) printf '%s\\n' \"$$\" > " + audioHTTPShellQuote(pidFile) +
		"; exec " + audioHTTPShellQuote(ffmpeg) + " -readrate 0.5 \"$@\";; *) exec " + audioHTTPShellQuote(ffmpeg) + " \"$@\";; esac\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal("write the owned real-encoder rate wrapper")
	}
	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}, []string{root}, library.WithPlaybackControlPool(f.control))
	if err != nil {
		t.Fatal("open the real-media reserved-capacity catalog")
	}
	installFixtureCatalog(t, f.serverFixture, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(f.app.notifier.Close)
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath, f.app.cfg.MediaRoots = wrapper, ffprobe, []string{root}
	f.app.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	f.cfg = f.app.cfg
	initializeFixtureSettings(t, f.serverFixture)
	if err := f.app.hls.Close(f.ctx); err != nil {
		t.Fatal("close the initial disabled conversion runtime")
	}
	runtime, err := newHLSRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatal("open the real reserved-capacity conversion runtime")
	}
	f.app.hls = runtime
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	collection, err := catalog.CreateLibrary(f.ctx, "Real Control Media", "movies", []string{root})
	if err != nil {
		t.Fatal("create the real-media library")
	}
	(&streamHTTPFixture{f: f.serverFixture}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: f.actor.User.ID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal("find the indexed real source")
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == source && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" {
		t.Fatal("the real source was not indexed")
	}
	principal, headers := f.principal(t, "normal")
	f.handler = f.app.Handler()
	h := &hlsHTTPFixture{f: f.serverFixture, ffmpeg: ffmpeg, ffprobe: ffprobe, item: item, path: source, libraryID: collection.ID, server: httptest.NewServer(f.handler)}
	t.Cleanup(h.server.Close)
	graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID, deviceID: principal.Client.DeviceID, headers: headers}, 0)
	getCtx, cancelGET := context.WithCancel(f.ctx)
	defer cancelGET()
	request, err := http.NewRequestWithContext(getCtx, http.MethodGet, h.server.URL+graph.children[0], nil)
	if err != nil {
		t.Fatal("create the independent real-encoder consumer")
	}
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		response, err := h.server.Client().Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	var pid int
	verifiedEncoder := false
	startDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(startDeadline) {
		if content, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(content)))
			if executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); pid > 0 && err == nil && strings.Contains(filepath.Base(executable), "ffmpeg") {
				verifiedEncoder = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 || !verifiedEncoder {
		t.Fatal("the real encoder never reached process execution")
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err != nil {
		t.Fatal("the encoder exited before control-capacity contention")
	}
	release := holdPlaybackPool(t, f.ctx, f.pool)
	defer release()
	before := f.pool.Stat().AcquireCount()
	stopCtx, cancelStop := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancelStop()
	started := time.Now()
	body := map[string]any{"PlaySessionId": graph.playID, "ItemId": item.ID, "MediaSourceId": media.SourceID(item.ID)}
	expectStatus(t, playbackControlRequest(stopCtx, f.handler, "/emby/Sessions/Playing/Stopped", body, headers), http.StatusNoContent)
	controlLatency := time.Since(started)
	if f.pool.Stat().AcquireCount() != before || stopCtx.Err() != nil {
		t.Fatal("the real encoder stop acquired exhausted Data capacity")
	}
	reapDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(reapDeadline) {
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); errors.Is(err, os.ErrNotExist) {
			t.Logf("stopped_latency=%s encoder_reaped_after=%s data_max=%d control_max=%d", controlLatency, time.Since(started), f.pool.Config().MaxConns, f.control.Config().MaxConns)
			cancelGET()
			release()
			select {
			case <-consumerDone:
			case <-time.After(5 * time.Second):
				t.Fatal("the stopped real-media consumer did not drain")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the stopped encoder was not actually reaped while Data remained exhausted")
}

func TestHTTPNonControlRoutesCannotSelectReservedCapacityFromMetadata(t *testing.T) {
	f := newPlaybackControlFixture(t)
	principal, headers := f.principal(t, "normal")
	play := f.play(t, principal, false)
	release := holdPlaybackPool(t, f.ctx, f.pool)
	defer release()
	before := f.control.Stat().AcquireCount()
	headers.Set("X-Goby-Database-Lane", "playback-control")
	deadline, cancel := context.WithTimeout(f.ctx, 100*time.Millisecond)
	defer cancel()
	response := playbackControlRequest(deadline, f.handler, "/emby/Sessions/Playing/Progress?database_lane=playback-control", map[string]any{"PlaySessionId": play.ID, "Event": "Stopped"}, headers)
	if response.Code == http.StatusNoContent || !errors.Is(deadline.Err(), context.DeadlineExceeded) || f.control.Stat().AcquireCount() != before {
		t.Fatal("untrusted non-control metadata selected reserved capacity")
	}
}

func TestHTTPDynamicPlaybackPingReadbacksUseReservedCapacity(t *testing.T) {
	f := newPlaybackControlFixture(t)
	principal, headers := f.principal(t, "key-metadata-free")
	play := f.play(t, principal, true)
	var authorizationMu sync.Mutex
	var controlAuthorizations int
	var lastControlAuthorizationErr error
	manager, err := dynamicsource.New(f.ctx, []dynamicsource.Definition{{ItemID: play.ItemID, URL: "https://configured.invalid/control", Infinite: true}}, dynamicsource.Options{Connector: dynamicTestConnector{}, Authorize: func(ctx context.Context, owner dynamicsource.Owner, itemID, playID string) error {
		err := f.app.authorizeDynamicSource(ctx, owner, itemID, playID)
		if database.IsPlaybackControl(ctx) {
			authorizationMu.Lock()
			controlAuthorizations++
			lastControlAuthorizationErr = err
			authorizationMu.Unlock()
		}
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	owner := dynamicSourceOwner(principal)
	description, err := manager.Describe(f.ctx, owner, play.ItemID, play.MediaSourceID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Open(f.ctx, owner, dynamicsource.OpenRequest{ItemID: play.ItemID, PlaySessionID: play.ID, OpenToken: description.OpenToken})
	if err != nil {
		t.Fatal(err)
	}
	input, err := manager.Acquire(f.ctx, owner, lease.ID)
	if err != nil {
		t.Fatal("acquire the authorized fixture input before heartbeat delivery")
	}
	t.Cleanup(func() { _ = input.Close() })
	var delivered [1]byte
	if _, err := io.ReadFull(input, delivered[:]); err != nil {
		t.Fatal("the dynamic fixture delivered no authorized bytes")
	}
	if err := f.app.dynamicSources.Close(context.Background()); err != nil {
		t.Fatal("close the initial dynamic fixture manager")
	}
	f.app.dynamicSources = manager
	lifetime, cancelLifetime := context.WithCancel(f.ctx)
	defer cancelLifetime()
	scope := transcode.Scope{AuthSessionID: principal.SessionID, ApplicationKey: true, ApplicationClientID: principal.ClientSessionID, DeviceID: principal.Client.DeviceID,
		PlaySessionID: play.ID, ItemID: play.ItemID, SourceID: play.MediaSourceID}
	session := &dynamicStreamSession{id: "control-dynamic", key: dynamicStreamKey{owner: owner.Identity(), liveID: lease.ID}, scope: scope,
		principal: principal, ctx: lifetime, cancel: cancelLifetime, changed: make(chan struct{}), accessed: time.Now().Add(-time.Minute)}
	f.app.dynamicStreams.sessions[session.id] = session
	f.app.dynamicStreams.byKey[session.key] = session
	gate := f.app.playbackPolicyGate()
	policyContext, releasePolicy, err := gate.acquire(f.ctx, principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer releasePolicy()
	// Heartbeats renew previously delivered media; they cannot turn a mere
	// admission into delivery. Register the byte observed from the owned input.
	f.app.touchMediaPolicy(policyContext, principal, scope)
	release := holdPlaybackPool(t, f.ctx, f.pool)
	defer release()
	before := f.pool.Stat().AcquireCount()
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	expectStatus(t, playbackControlRequest(ctx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, headers), http.StatusNoContent)
	session.mu.Lock()
	closed, accessed := session.closed, session.accessed
	session.mu.Unlock()
	after := f.pool.Stat().AcquireCount()
	authorizationMu.Lock()
	infoAuthorizations, infoErr := controlAuthorizations, lastControlAuthorizationErr
	authorizationMu.Unlock()
	if closed || !accessed.After(time.Now().Add(-10*time.Second)) || after != before || ctx.Err() != nil || infoAuthorizations != 1 || infoErr != nil {
		t.Fatalf("dynamic heartbeat mismatch: closed=%t accessed_recently=%t data_acquires_before=%d data_acquires_after=%d context_error=%v control_info_authorizations=%d info_error_type=%T",
			closed, accessed.After(time.Now().Add(-10*time.Second)), before, after, ctx.Err(), infoAuthorizations, infoErr)
	}
}
