//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
	"golang.org/x/sys/unix"
)

type playbackStopAlias struct {
	name, method, path string
}

var playbackStopAliases = []playbackStopAlias{
	{name: "delete", method: http.MethodDelete, path: "/emby/Videos/ActiveEncodings"},
	{name: "post-delete", method: http.MethodPost, path: "/emby/Videos/ActiveEncodings/Delete"},
}

func playbackStopAliasRequest(ctx context.Context, handler http.Handler, alias playbackStopAlias, reference, device string, headers http.Header) *httptest.ResponseRecorder {
	query := url.Values{"PlaySessionId": {reference}, "DeviceId": {device}}
	request := httptest.NewRequest(alias.method, alias.path+"?"+query.Encode(), nil).WithContext(ctx)
	request.Header = headers.Clone()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func playbackStopAliasAssertControl(t *testing.T, counts *playbackControlAcquireCounts) {
	t.Helper()
	if counts.dataAttempts.Load() != 0 || counts.dataSuccesses.Load() != 0 || counts.unexpectedAttempts.Load() != 0 ||
		counts.controlAttempts.Load() == 0 || counts.controlSuccesses.Load() == 0 {
		t.Fatalf("stop alias routing mismatch: data_attempts=%d data_successes=%d control_attempts=%d control_successes=%d unexpected_attempts=%d",
			counts.dataAttempts.Load(), counts.dataSuccesses.Load(), counts.controlAttempts.Load(), counts.controlSuccesses.Load(), counts.unexpectedAttempts.Load())
	}
}

// The catalog's reserved ownership session occupies one data connection. Hold
// every other connection ourselves instead of mistaking a transient background
// acquisition for stable exhaustion. These leases survive the response and reap.
func playbackStopAliasHoldData(t *testing.T, f *playbackControlFixture) func() {
	t.Helper()
	if f.pool.Config().MaxConns != database.DataMaxConns || f.control.Config().MaxConns != database.PlaybackControlMaxConns {
		t.Fatal("stop alias fixture changed the production data/control budgets")
	}
	held := make([]*pgxpool.Conn, 0, database.DataMaxConns-1)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		for _, connection := range held {
			connection.Release()
		}
	}
	t.Cleanup(release)
	for range database.DataMaxConns - 1 {
		connection, err := f.pool.Acquire(f.ctx)
		if err != nil {
			release()
			t.Fatal("hold every usable data connection before stopping the encoder")
		}
		held = append(held, connection)
	}
	if f.pool.Stat().AcquiredConns() != database.DataMaxConns {
		t.Fatal("the actual data pool is not fully borrowed")
	}
	return release
}

type playbackStopAliasRealFixture struct {
	control *playbackControlFixture
	hls     *hlsHTTPFixture
	pidFile string
	encoder string
	source  os.FileInfo
}

// This uses the existing real-control fixture's paced exec wrapper and ordinary
// production HLS runtime. It does not enable the private generated-window gate.
func newPlaybackStopAliasRealFixture(t *testing.T) *playbackStopAliasRealFixture {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real stop-alias verification")
	}
	encoder, err := exec.LookPath(ffmpeg)
	if err == nil {
		encoder, err = filepath.Abs(encoder)
	}
	if err == nil {
		encoder, err = filepath.EvalSymlinks(encoder)
	}
	if err != nil {
		t.Fatal("resolve the actual paced encoder executable")
	}
	f := newPlaybackControlFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "Stop.Alias.HLS.mp4")
	hlsHTTPMediaCommand(t, encoder, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=12",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", source)
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal("capture the independent source inode")
	}
	wrapperRoot := t.TempDir()
	wrapper, pidFile := filepath.Join(wrapperRoot, "ffmpeg-stop-alias"), filepath.Join(wrapperRoot, "encoder.pid")
	script := "#!/bin/sh\ncase \" $* \" in *\" -progress \"*) printf '%s\\n' \"$$\" > " + audioHTTPShellQuote(pidFile) +
		"; exec " + audioHTTPShellQuote(encoder) + " -readrate 0.5 \"$@\";; *) exec " + audioHTTPShellQuote(encoder) + " \"$@\";; esac\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal("write the owned real-encoder pacing wrapper")
	}
	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}, []string{root}, library.WithPlaybackControlPool(f.control))
	if err != nil {
		t.Fatal("open the real stop-alias catalog")
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
		t.Fatal("open the real stop-alias conversion runtime")
	}
	f.app.hls = runtime
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Close(cleanup); err != nil {
			t.Error("real stop-alias runtime cleanup retained ownership")
		}
	})
	collection, err := catalog.CreateLibrary(f.ctx, "Real Stop Alias Media", "movies", []string{root})
	if err != nil {
		t.Fatal("create the real stop-alias library")
	}
	(&streamHTTPFixture{f: f.serverFixture}).rescan(t, collection.ID)
	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: f.actor.User.ID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal("find the indexed stop-alias source")
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == source && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" {
		t.Fatal("the real stop-alias source was not indexed")
	}
	f.handler = f.app.Handler()
	h := &hlsHTTPFixture{f: f.serverFixture, ffmpeg: encoder, ffprobe: ffprobe, item: item, path: source, libraryID: collection.ID,
		server: httptest.NewServer(f.handler)}
	t.Cleanup(h.server.Close)
	return &playbackStopAliasRealFixture{control: f, hls: h, pidFile: pidFile, encoder: encoder, source: info}
}

type playbackStopAliasEncoder struct {
	pid       int
	startTick uint64
	pidfd     int
	jobID     string
	first     hlsWallPauseProcessCounters
	last      hlsWallPauseProcessCounters
}

// Stop acceptance needs an admitted owner and a live encoder, not a completed
// movie graph. Keep the same explicit application client on every setup request.
// Metadata-free URL context restoration is a separate production contract.
func playbackStopAliasPrepareKeyGraph(t *testing.T, fixture *playbackStopAliasRealFixture, principal identity.Principal, headers http.Header) hlsHTTPGraph {
	t.Helper()
	f, h := fixture.control, fixture.hls
	if !principal.IsApplicationKey() || principal.ClientSessionID == "" || headers.Get("Authorization") == "" {
		t.Fatal("key stop setup requires the exact explicit authenticated client")
	}
	metadataRequest := httptest.NewRequest(http.MethodGet, "/emby/Items/"+h.item.ID+"/PlaybackInfo", nil)
	metadataRequest.Header = headers.Clone()
	token, client, err := parseEmbyCredentials(metadataRequest)
	if err != nil || token != headers.Get("X-Emby-Token") || client != principal.Client || f.app.hls.generatedWindowsEnabled {
		t.Fatal("key stop setup changed its explicit client metadata or the default HLS runtime")
	}
	target, err := f.users.GetUser(f.ctx, f.actor.User.ID)
	if err != nil || target.ID != f.actor.User.ID {
		t.Fatal("key stop setup has no existing target-user capability projection")
	}
	body := map[string]any{"UserId": target.ID, "IsPlayback": true, "EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false,
		"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Container": "ts", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac",
			"MaxWidth": 96, "MaxHeight": 54, "SegmentLength": 3,
		}}}}
	prepared := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, headers)
	if prepared.status != http.StatusOK {
		t.Fatalf("key stop setup stage=playback_info status=%d want=%d", prepared.status, http.StatusOK)
	}
	var object struct {
		PlaySessionID string           `json:"PlaySessionId"`
		MediaSources  []map[string]any `json:"MediaSources"`
		ErrorCode     string           `json:"ErrorCode"`
	}
	if err := json.Unmarshal(prepared.body, &object); err != nil || object.PlaySessionID == "" || len(object.MediaSources) != 1 || object.ErrorCode != "" {
		t.Fatal("key stop setup did not prepare exactly one supported source")
	}
	source := object.MediaSources[0]
	masterURL, ok := source["TranscodingUrl"].(string)
	if !ok || source["SupportsTranscoding"] != true || source["TranscodingContainer"] != "ts" || source["TranscodingSubProtocol"] != "hls" {
		t.Fatal("key stop setup did not advertise the actual requested HLS output")
	}
	parsed := hlsHTTPURL(t, masterURL, headers.Get("X-Emby-Token"))
	if parsed.Query().Get("PlaySessionId") != object.PlaySessionID || parsed.Query().Get("GobyHlsId") == "" ||
		parsed.Query().Get("DeviceId") != principal.Client.DeviceID || parsed.Query().Get("MediaSourceId") != media.SourceID(h.item.ID) {
		t.Fatal("key stop setup URL changed the admitted playback or client identity")
	}
	f.app.hls.mu.Lock()
	session := f.app.hls.sessions[parsed.Query().Get("GobyHlsId")]
	f.app.hls.mu.Unlock()
	if session == nil || !session.key.scope.ApplicationKey || session.key.scope.AuthSessionID != principal.SessionID ||
		session.key.scope.ApplicationClientID != principal.ClientSessionID || session.key.scope.DeviceID != principal.Client.DeviceID ||
		session.key.scope.UserID != principal.User.ID || session.key.scope.PlaySessionID != object.PlaySessionID || session.key.scope.ItemID != h.item.ID ||
		session.key.scope.SourceID != media.SourceID(h.item.ID) {
		t.Fatal("key stop setup registered a different authenticated producer owner")
	}
	master := h.request(t, http.MethodGet, masterURL, nil, headers)
	if master.status != http.StatusOK {
		t.Fatalf("key stop setup stage=master status=%d want=%d", master.status, http.StatusOK)
	}
	children := hlsHTTPManifestChildren(master.body)
	if len(children) != 1 {
		t.Fatal("key stop setup master did not advertise one media playlist")
	}
	main := hlsHTTPURL(t, children[0], headers.Get("X-Emby-Token"))
	if main.Query().Get("PlaySessionId") != object.PlaySessionID || main.Query().Get("GobyHlsId") != session.id ||
		main.Query().Get("DeviceId") != principal.Client.DeviceID || main.Query().Get("MediaSourceId") != session.key.scope.SourceID {
		t.Fatal("key stop setup media URL changed the admitted playback owner")
	}
	t.Log("key_stop_alias_setup playback_info=200 master=200 explicit_client_metadata=true target_user_verified=true scope_client_verified=true")
	return hlsHTTPGraph{playID: object.PlaySessionID, hlsID: session.id, masterURL: masterURL, mainURL: children[0]}
}

// The natural consumer follows at most one media playlist before consuming its
// first media resource. It keeps the explicit client rather than changing its
// authenticated identity between negotiation and producer startup.
func playbackStopAliasConsumeKey(ctx context.Context, h *hlsHTTPFixture, mainURL string, headers http.Header) {
	target := mainURL
	for stage := 0; stage < 2; stage++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+target, nil)
		if err != nil {
			return
		}
		request.Header = headers.Clone()
		response, err := h.server.Client().Do(request)
		if err != nil {
			return
		}
		if stage == 0 && response.StatusCode == http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 17<<20))
			_ = response.Body.Close()
			children := hlsHTTPManifestChildren(body)
			if readErr != nil || len(body) >= 17<<20 || len(children) == 0 {
				return
			}
			parsed, err := url.Parse(children[0])
			if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/emby/") ||
				parsed.Query().Get("api_key") != headers.Get("X-Emby-Token") {
				return
			}
			target = children[0]
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return
	}
}

func playbackStopAliasObserveEncoder(t *testing.T, fixture *playbackStopAliasRealFixture, scope transcode.Scope) playbackStopAliasEncoder {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(fixture.pidFile)
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
		if err == nil && parseErr == nil && pid > 0 {
			exe, exeErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
			cwd, cwdErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
			state, start, statErr := hlsColdPID(pid)
			jobID := filepath.Base(cwd)
			owned, ownedErr := filepath.EvalSymlinks(filepath.Join(fixture.control.app.cfg.Transcoding.CacheDirectory, jobID))
			if exeErr == nil && cwdErr == nil && statErr == nil && ownedErr == nil && state != 'Z' && start > 0 && exe == fixture.encoder &&
				hlsWallPauseJobID(jobID) && cwd == owned {
				record, recordErr := fixture.control.app.hls.manager.Snapshot(scope, jobID)
				first, countersErr := hlsWallPauseReadProcess(pid, start)
				if recordErr == nil && record.State == "running" && countersErr == nil {
					pidfd, err := unix.PidfdOpen(pid, 0)
					if err != nil {
						t.Fatal("pin the actual encoder identity before stop contention")
					}
					t.Cleanup(func() { _ = unix.Close(pidfd) })
					process := playbackStopAliasEncoder{pid: pid, startTick: start, pidfd: pidfd, jobID: jobID, first: first}
					growthDeadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(growthDeadline) {
						last, err := hlsWallPauseReadProcess(pid, start)
						if err != nil {
							t.Fatal("the exact encoder disappeared before actual work was observed")
						}
						if last.UserTicks > first.UserTicks || last.SystemTicks > first.SystemTicks || last.ReadChars > first.ReadChars || last.WriteChars > first.WriteChars {
							process.last = last
							return process
						}
						time.Sleep(20 * time.Millisecond)
					}
					t.Fatal("the actual encoder produced no CPU or character-IO growth")
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the exact real encoder never reached its owned running workspace")
	return playbackStopAliasEncoder{}
}

func playbackStopAliasWaitReaped(t *testing.T, f *playbackControlFixture, process playbackStopAliasEncoder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.pool.Stat().AcquiredConns() != database.DataMaxConns {
			t.Fatal("data capacity was returned before the stopped encoder was reaped")
		}
		exited, err := hlsWallPausePIDFDExited(process.pidfd)
		if err != nil {
			t.Fatal("the pinned encoder exit observation failed")
		}
		_, start, statErr := hlsColdPID(process.pid)
		if exited && errors.Is(statErr, os.ErrNotExist) {
			return
		}
		if statErr == nil && start != process.startTick || statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("the actual encoder retirement identity became unknown")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the real encoder did not exit and reap while all data capacity remained held")
}

func playbackStopAliasSourceFDs(t *testing.T, expected os.FileInfo) int {
	t.Helper()
	want, ok := expected.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("source descriptor identity is unavailable")
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal("read the actual source descriptor inventory")
	}
	count := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal("an actual descriptor could not be inspected")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && stat.Dev == want.Dev && stat.Ino == want.Ino {
			count++
		}
	}
	return count
}

func TestHTTPPlaybackStopAliasesStopActualEncoderWithReservedCapacity(t *testing.T) {
	for _, kind := range []string{"normal", "key-explicit"} {
		for _, alias := range playbackStopAliases {
			t.Run(kind+"/"+alias.name, func(t *testing.T) {
				fixture := newPlaybackStopAliasRealFixture(t)
				f, h := fixture.control, fixture.hls
				principal, headers := f.principal(t, kind)
				var graph hlsHTTPGraph
				if principal.IsApplicationKey() {
					graph = playbackStopAliasPrepareKeyGraph(t, fixture, principal, headers)
				} else {
					graph = h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID, deviceID: principal.Client.DeviceID, headers: headers}, 0)
				}
				f.app.hls.mu.Lock()
				session := f.app.hls.sessions[graph.hlsID]
				f.app.hls.mu.Unlock()
				if session == nil {
					t.Fatal("the production graph has no exact HLS owner")
				}
				scope := session.key.scope
				getCtx, cancelGET := context.WithCancel(f.ctx)
				t.Cleanup(cancelGET)
				var request *http.Request
				if !principal.IsApplicationKey() {
					var err error
					request, err = http.NewRequestWithContext(getCtx, http.MethodGet, h.server.URL+graph.children[0], nil)
					if err != nil {
						t.Fatal("create the natural production HLS consumer")
					}
				}
				consumerDone := make(chan struct{})
				go func() {
					defer close(consumerDone)
					if principal.IsApplicationKey() {
						playbackStopAliasConsumeKey(getCtx, h, graph.mainURL, headers)
						return
					}
					response, err := h.server.Client().Do(request)
					if err == nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
				}()
				process := playbackStopAliasObserveEncoder(t, fixture, scope)
				if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1", principal.SessionID); err != nil {
					t.Fatal("make ordinary credential activity due before pool exhaustion")
				}
				if principal.IsApplicationKey() {
					if _, err := f.pool.Exec(f.ctx, "UPDATE application_key_clients SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1", principal.ClientSessionID); err != nil {
						t.Fatal("make bound application-client activity due before pool exhaustion")
					}
					if _, err := f.pool.Exec(f.ctx, "UPDATE application_keys SET last_used_at=clock_timestamp()-interval '1 minute' WHERE credential_id=$1", principal.SessionID); err != nil {
						t.Fatal("make application-key activity due before pool exhaustion")
					}
				}
				releaseData := playbackStopAliasHoldData(t, f)
				defer releaseData()
				stopCtx, cancelStop := context.WithTimeout(f.ctx, 3*time.Second)
				defer cancelStop()
				tagged, counts := f.traceAcquisitions(stopCtx)
				device := principal.Client.DeviceID
				if principal.IsApplicationKey() {
					// This query hint cannot replace the authenticated client owner.
					device = "non-authorizing-query-device"
				}
				started := time.Now()
				expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, graph.playID, device, headers), http.StatusNoContent)
				responseElapsed := time.Since(started)
				playbackStopAliasAssertControl(t, counts)
				if stopCtx.Err() != nil || f.pool.Stat().AcquiredConns() != database.DataMaxConns {
					t.Fatal("the stop alias response exceeded its deadline or released held data capacity")
				}
				playbackStopAliasWaitReaped(t, f, process)
				reapedElapsed := time.Since(started)
				releaseData()
				cancelGET()
				select {
				case <-consumerDone:
				case <-time.After(5 * time.Second):
					t.Fatal("the stopped natural HTTP consumer did not actually join")
				}
				audioHTTPWait(t, "the stop alias left an HLS request slot or control connection borrowed", func() bool {
					return len(f.app.hls.slots) == 0 && len(f.app.streamSlots) == 0 && f.control.Stat().AcquiredConns() == 0
				})
				cleanup, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancelCleanup()
				if err := f.app.hls.Close(cleanup); err != nil {
					t.Fatal("actual stop-alias finalization retained runtime ownership")
				}
				resources, ok := f.app.hls.manager.(interface {
					ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
				})
				if !ok {
					t.Fatal("the real manager has no readonly scoped ownership observation")
				}
				usage, err := resources.ResourceUsage(cleanup, scope)
				if err != nil || usage != (transcode.ResourceUsage{}) || playbackStopAliasSourceFDs(t, fixture.source) != 0 {
					t.Fatal("actual encoder finalization left scoped resources or source descriptors retained")
				}
				if _, err := os.Stat(filepath.Join(f.app.cfg.Transcoding.CacheDirectory, process.jobID)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("the retired encoder cache directory remains or its deletion is unknown")
				}
				if err := f.app.Close(cleanup); err != nil {
					t.Fatal("stop-alias Store close did not actually drain its ownership")
				}
				if f.pool.Stat().AcquiredConns() != 0 || f.control.Stat().AcquiredConns() != 0 || playbackStopAliasSourceFDs(t, fixture.source) != 0 {
					t.Fatal("closed stop-alias owners retained a connection or source descriptor")
				}
				t.Logf("stop_alias=%s owner_kind=%s response_elapsed=%s actual_exit_and_reap_elapsed=%s pid=%d start_tick=%d cpu_ticks_before=%d cpu_ticks_after=%d read_chars_before=%d read_chars_after=%d write_chars_before=%d write_chars_after=%d data_max=%d control_max=%d",
					alias.name, kind, responseElapsed, reapedElapsed, process.pid, process.startTick,
					process.first.UserTicks+process.first.SystemTicks, process.last.UserTicks+process.last.SystemTicks,
					process.first.ReadChars, process.last.ReadChars, process.first.WriteChars, process.last.WriteChars,
					f.pool.Config().MaxConns, f.control.Config().MaxConns)
			})
		}
	}
}

func TestHTTPPlaybackStopAliasesPreserveFreshOwnerAndIdempotence(t *testing.T) {
	for _, kind := range []string{"normal", "key-explicit"} {
		t.Run(kind, func(t *testing.T) {
			f := newPlaybackControlFixture(t)
			principal, headers := f.principal(t, kind)
			play := f.play(t, principal, false)
			var foreign identity.Principal
			if principal.IsApplicationKey() {
				var err error
				foreign, err = f.users.ResolveEmbyForClient(f.ctx, headers.Get("X-Emby-Token"), identity.Client{Name: "Control Client", DeviceID: "foreign-client", Device: "Foreign Device", Version: "1"})
				if err != nil {
					t.Fatal("bind a different client of the same application credential")
				}
			} else {
				foreign, _ = f.principal(t, "key-explicit")
			}
			foreignPlay := f.play(t, foreign, false)
			terminal, _, err := f.app.library.ReportPlayback(f.ctx, playbackOwner(principal), library.PlaybackReport{Event: "Stopped", PlaySessionID: play.ID})
			if err != nil || terminal.State != "Stopped" {
				t.Fatal("prepare a terminal owned reference for idempotent cleanup")
			}
			releaseData := playbackStopAliasHoldData(t, f)
			defer releaseData()
			requestCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
			defer cancel()
			tagged, counts := f.traceAcquisitions(requestCtx)
			for _, alias := range playbackStopAliases {
				if !principal.IsApplicationKey() {
					expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, play.ID, "wrong-device", headers), http.StatusForbidden)
				}
				expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, foreignPlay.ID, foreign.Client.DeviceID, headers), http.StatusNoContent)
				for range 2 {
					expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, play.ID, principal.Client.DeviceID, headers), http.StatusNoContent)
					expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, "missing-stop-alias", principal.Client.DeviceID, headers), http.StatusNoContent)
				}
			}
			if _, err := f.control.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
				t.Fatal("revoke the exact caller credential")
			}
			for _, alias := range playbackStopAliases {
				expectStatus(t, playbackStopAliasRequest(tagged, f.handler, alias, play.ID, principal.Client.DeviceID, headers), http.StatusUnauthorized)
			}
			playbackStopAliasAssertControl(t, counts)
			var state string
			if err := f.control.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id=$1", foreignPlay.ID).Scan(&state); err != nil || state != "Playing" {
				t.Fatal("foreign stop alias changed a different playback owner")
			}
			var unexpected int
			if err := f.control.QueryRow(f.ctx, "SELECT count(*) FROM client_playback_references WHERE client_nonce='missing-stop-alias'").Scan(&unexpected); err != nil || unexpected != 0 {
				t.Fatal("an idempotent unknown stop created a playback reference")
			}
			if requestCtx.Err() != nil || f.pool.Stat().AcquiredConns() != database.DataMaxConns || f.control.Stat().AcquiredConns() != 0 {
				t.Fatal("negative stop-alias requests did not preserve held data and return reserved connections")
			}
		})
	}
}

func TestHTTPPlaybackStopAliasesDoNotFallbackWhenControlIsExhausted(t *testing.T) {
	for _, kind := range []string{"normal", "key-explicit"} {
		t.Run(kind, func(t *testing.T) {
			f := newPlaybackControlFixture(t)
			principal, headers := f.principal(t, kind)
			play := f.play(t, principal, false)
			client := identity.Client{}
			if principal.IsApplicationKey() {
				client = identity.Client{Name: "Control Client", DeviceID: "control-device", Device: "Control Device", Version: "1"}
			}
			if _, err := f.users.ResolveEmbyForClientWithPeer(f.ctx, headers.Get("X-Emby-Token"), client, "192.0.2.1"); err != nil {
				t.Fatal("control exhaustion must begin with live request-equivalent authentication")
			}
			releaseControl := holdPlaybackPool(t, f.ctx, f.control)
			defer releaseControl()
			for _, alias := range playbackStopAliases {
				ctx, cancel := context.WithTimeout(f.ctx, 100*time.Millisecond)
				tagged, counts := f.traceAcquisitions(ctx)
				response := playbackStopAliasRequest(tagged, f.handler, alias, play.ID, principal.Client.DeviceID, headers)
				if response.Code == http.StatusNoContent || !errors.Is(ctx.Err(), context.DeadlineExceeded) ||
					counts.dataAttempts.Load() != 0 || counts.dataSuccesses.Load() != 0 || counts.unexpectedAttempts.Load() != 0 ||
					counts.controlAttempts.Load() == 0 || counts.controlSuccesses.Load() != 0 || counts.controlDeadlineErrors.Load() == 0 {
					t.Fatalf("exhausted control alias escaped its lane: alias=%s status=%d data_attempts=%d control_attempts=%d control_successes=%d control_deadlines=%d",
						alias.name, response.Code, counts.dataAttempts.Load(), counts.controlAttempts.Load(), counts.controlSuccesses.Load(), counts.controlDeadlineErrors.Load())
				}
				cancel()
			}
			releaseControl()
			if f.control.Stat().AcquiredConns() != 0 {
				t.Fatal("failed control-alias attempts retained a reserved connection")
			}
		})
	}
}

func TestHTTPPlaybackStopAliasesLegacyConstructorKeepsSinglePool(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	client := identity.Client{Name: "Legacy Stop Client", DeviceID: "legacy-stop-device", Device: "Legacy Device", Version: "1"}
	credentials, err := f.users.Authenticate(f.ctx, "Administrator", "administrator-password", client, "emby")
	if err != nil {
		t.Fatal("authenticate the legacy single-pool stop caller")
	}
	principal, err := f.users.ResolveEmbyForClient(f.ctx, credentials.Token, client)
	if err != nil {
		t.Fatal("resolve the legacy stop owner")
	}
	if f.app.playbackControlDB != nil {
		t.Fatal("the legacy constructor unexpectedly installed a reserved pool")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('legacy-stop-library','Legacy Stop','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,media) VALUES ('legacy-stop-item','legacy-stop-library','Legacy Item','legacy item','Movie',
		jsonb_build_object('DurationTicks',6000000000::bigint))`); err != nil {
		t.Fatal("prepare the legacy single-pool catalog facts")
	}
	play, err := f.app.library.PreparePlayback(f.ctx, playbackOwner(principal), "legacy-stop-item", media.SourceID("legacy-stop-item"), "")
	if err != nil {
		t.Fatal("prepare legacy playback without reserved control capacity")
	}
	for _, alias := range playbackStopAliases {
		expectStatus(t, playbackStopAliasRequest(f.ctx, f.handler, alias, play.ID, client.DeviceID, http.Header{"X-Emby-Token": {credentials.Token}}), http.StatusNoContent)
	}
}
