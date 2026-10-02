//go:build linux && primary_io_measure

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// This cohort constructs a complete production Server after fixture media and
// committed catalog facts exist. It deliberately does not install a manager
// hook, a fake Start callback, or a manually constructed HLS runtime.
func newHLSPrimarySourceIOProductionFixture(t *testing.T) *playbackStopAliasRealFixture {
	t.Helper()
	real := newPlaybackStopAliasRealFixture(t)
	f, h := real.control, real.hls
	h.server.Close()
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := f.app.Close(cleanup)
	cancel()
	if err != nil {
		t.Fatal("join the replaced fixture application before production reconstruction")
	}
	users := identity.New(f.pool)
	app, err := New(f.ctx, f.cfg, f.pool, users, f.log, "primary-source-io-production", WithPlaybackControlPool(f.control))
	if err != nil {
		t.Fatal("construct the complete production server over the owned data and control pools")
	}
	f.app, f.users, f.handler = app, users, app.Handler()
	server := httptest.NewServer(f.handler)
	h.server = server
	// The old fixture cleanups capture their old owners. This cleanup captures
	// the newly constructed application and does not close either borrowed pool.
	t.Cleanup(func() {
		server.CloseClientConnections()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Close(cleanup); err != nil {
			t.Error("production source-I/O cleanup did not join its workers and Store")
		}
		closed := make(chan struct{})
		go func() {
			server.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Error("production source-I/O HTTP cleanup did not join its handlers")
		}
	})
	if !app.correlatedHLSOwnershipEnabled || !app.correlatedHLSEarlyStopEnabled || app.hls.generatedWindowsEnabled {
		t.Fatal("Server.New did not retain the default qualified file-HLS ownership chain")
	}
	return real
}

func hlsPrimarySourceIOAssertBaseline(t *testing.T, before library.PrimaryReadMeasurementSnapshot, stage string) {
	t.Helper()
	after := library.PrimaryReadMeasurementStats()
	if after != before {
		t.Fatalf("%s retained primary source-I/O state: before=%+v after=%+v", stage, before, after)
	}
}

// Copy every public field from a newly authorized snapshot. The private proof
// belongs to the current Store and cannot be reconstructed by this literal.
func hlsPrimarySourceIORejectPublicSnapshot(t *testing.T, real *playbackStopAliasRealFixture, principal identity.Principal) {
	t.Helper()
	f, h := real.control, real.hls
	file, source, err := f.app.library.OpenMediaFor(f.ctx, librarySubject(principal, principal.User.ID), h.item.ID, media.SourceID(h.item.ID))
	if err != nil || file == nil {
		t.Fatal("open a fresh authorized production source snapshot")
	}
	read, err := f.app.library.PrepareMediaSourceIO(f.ctx, source)
	if err != nil || read == nil {
		_ = file.Close()
		t.Fatal("the fresh committed snapshot did not carry its production source-I/O proof")
	}
	publicOnly := library.MediaFile{
		Item: source.Item, SourceID: source.SourceID, Container: source.Container,
		MIMEType: source.MIMEType, ETag: source.ETag, Size: source.Size, ModifiedAt: source.ModifiedAt,
	}
	forged, forgeErr := f.app.library.PrepareMediaSourceIO(f.ctx, publicOnly)
	if forged != nil {
		_ = forged.Close()
	}
	closeErr := closePrimaryMediaSource(file, read)
	if forged != nil || !errors.Is(forgeErr, library.ErrUnavailable) {
		t.Fatal("public source fields manufactured a committed primary source-I/O capability")
	}
	if closeErr != nil {
		t.Fatal("join the independently opened proof-check descriptor and read capability")
	}
}

type hlsPrimarySourceIOConsumerResult struct {
	status int
	bytes  int64
	err    error
	joined bool
}

// This consumer starts the ordinary authenticated media HTTP route. Its joined
// result is kept separate from the encoder's independently pinned retirement.
func hlsPrimarySourceIOConsume(t *testing.T, real *playbackStopAliasRealFixture, target string, headers http.Header) func() hlsPrimarySourceIOConsumerResult {
	t.Helper()
	ctx, cancel := context.WithCancel(real.control.ctx)
	joined := make(chan hlsPrimarySourceIOConsumerResult, 1)
	go func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, real.hls.server.URL+target, nil)
		if err != nil {
			joined <- hlsPrimarySourceIOConsumerResult{err: err}
			return
		}
		request.Header = headers.Clone()
		response, err := real.hls.server.Client().Do(request)
		if err != nil {
			joined <- hlsPrimarySourceIOConsumerResult{err: err}
			return
		}
		bytes, readErr := io.Copy(io.Discard, response.Body)
		joined <- hlsPrimarySourceIOConsumerResult{status: response.StatusCode, bytes: bytes, err: errors.Join(readErr, response.Body.Close())}
	}()
	var result hlsPrimarySourceIOConsumerResult
	done := false
	join := func() hlsPrimarySourceIOConsumerResult {
		cancel()
		if done {
			return result
		}
		select {
		case result = <-joined:
			result.joined = true
			done = true
		case <-time.After(5 * time.Second):
			t.Error("the actual production HLS HTTP consumer did not join after cancellation")
		}
		return result
	}
	t.Cleanup(func() { _ = join() })
	return join
}

func hlsPrimarySourceIOEncoderSourceFDs(t *testing.T, real *playbackStopAliasRealFixture, process playbackStopAliasEncoder) int {
	t.Helper()
	_, start, err := hlsColdPID(process.pid)
	if err != nil || start != process.startTick {
		t.Fatal("the pinned encoder identity changed during source descriptor observation")
	}
	group, err := syscall.Getpgid(process.pid)
	if err != nil || group != process.pid {
		t.Fatal("the pinned production encoder did not own the process group used for retirement proof")
	}
	root := filepath.Join("/proc", strconv.Itoa(process.pid), "fd")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal("read the actual pinned encoder descriptor inventory")
	}
	count := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(root, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal("the actual encoder source descriptor could not be inspected")
		}
		if os.SameFile(real.source, info) {
			count++
		}
	}
	return count
}

func hlsPrimarySourceIORequireLiveCharge(t *testing.T, real *playbackStopAliasRealFixture, process playbackStopAliasEncoder, before library.PrimaryReadMeasurementSnapshot) library.PrimaryReadMeasurementSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := hlsWallPauseReadProcess(process.pid, process.startTick)
		if err != nil {
			t.Fatal("the exact production encoder retired before a live source-I/O observation")
		}
		observed := library.PrimaryReadMeasurementStats()
		cpuGrowth := current.UserTicks > process.first.UserTicks || current.SystemTicks > process.first.SystemTicks
		characterIOGrowth := current.ReadChars > process.first.ReadChars || current.WriteChars > process.first.WriteChars
		if cpuGrowth && characterIOGrowth && observed.IO.Active > before.IO.Active &&
			observed.IO.ActiveRoots > before.IO.ActiveRoots && observed.IO.ActiveDomains > before.IO.ActiveDomains &&
			observed.Owners.RegisteredOwners > before.Owners.RegisteredOwners && observed.DomainClaims > before.DomainClaims {
			if playbackStopAliasSourceFDs(t, real.source) == 0 || hlsPrimarySourceIOEncoderSourceFDs(t, real, process) == 0 {
				t.Fatal("live primary source-I/O charged an encoder without the exact indexed source inode")
			}
			return observed
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the actual production encoder never retained an active source charge, owner and domain claim during CPU and character-I/O growth")
	return library.PrimaryReadMeasurementSnapshot{}
}

func hlsPrimarySourceIORequireRetired(t *testing.T, process playbackStopAliasEncoder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exited, reaped, groupClosed, known, facts := stoppedEncoderRetirementObservation(process)
		if !known {
			t.Fatalf("the exact production encoder retirement identity became unknown: %+v", facts)
		}
		if exited && reaped && groupClosed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the pinned production encoder did not exit, reap and retire its owned process group")
}

func TestHTTPHLSPrimarySourceIOProductionConstructorOwnsActualEncoder(t *testing.T) {
	runID := os.Getenv("GOBY_HLS_PRIMARY_SOURCE_IO_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_HLS_PRIMARY_SOURCE_IO_RUN_ID explicitly admits the actual production HLS source-I/O cohort")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("production HLS source-I/O requires a bounded run identity")
	}
	if testing.Short() {
		t.Skip("production HLS source-I/O requires real database and media fixtures")
	}
	before := library.PrimaryReadMeasurementStats()
	if before.IO.Active != 0 || before.IO.Background != 0 || before.IO.Queued != 0 || before.IO.ActiveRoots != 0 ||
		before.IO.ActiveDomains != 0 || before.Owners.RegisteredOwners != 0 || before.DomainClaims != 0 {
		t.Fatal("production HLS source-I/O requires an idle process-wide admission baseline")
	}
	real := newHLSPrimarySourceIOProductionFixture(t)
	f, h := real.control, real.hls
	principal, headers := f.principal(t, "normal")
	hlsPrimarySourceIORejectPublicSnapshot(t, real, principal)
	hlsPrimarySourceIOAssertBaseline(t, before, "joined authorized snapshot proof check")
	graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID,
		deviceID: principal.Client.DeviceID, headers: headers}, 0)
	f.app.hls.mu.Lock()
	session := f.app.hls.sessions[graph.hlsID]
	f.app.hls.mu.Unlock()
	if session == nil || session.playbackReference == nil {
		t.Fatal("natural HLS negotiation did not retain the production playback owner")
	}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", map[string]any{
		"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": int64(0)}, headers), http.StatusNoContent)
	joinConsumer := hlsPrimarySourceIOConsume(t, real, graph.children[0], headers)
	process := playbackStopAliasObserveEncoder(t, real, session.key.scope)
	live := hlsPrimarySourceIORequireLiveCharge(t, real, process, before)
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
		"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, headers), http.StatusNoContent)
	consumer := joinConsumer()
	if !consumer.joined {
		t.Fatal("join the actual production HLS HTTP consumer before native retirement and idle assertions")
	}
	if consumer.err != nil && !errors.Is(consumer.err, context.Canceled) {
		t.Fatal("the actual production HLS HTTP consumer returned an unexpected delivery or cleanup error")
	}
	hlsPrimarySourceIORequireRetired(t, process)
	if descriptors := playbackStopAliasSourceFDs(t, real.source); descriptors != 0 {
		t.Fatal("the exact source descriptors survived consumer join and pinned encoder retirement")
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := f.app.Close(cleanup)
	cancel()
	if err != nil {
		t.Fatal("join production application and Store cleanup before asserting idle primary state")
	}
	// Scalar idle observations are meaningful here only after the independent
	// consumer, native process/group, descriptor and Store lifetime joins above.
	hlsPrimarySourceIOAssertBaseline(t, before, "joined actual production HLS retirement")
	t.Logf("hls_primary_source_io run_id=%s production_constructor=true committed_public_snapshot_forgery_rejected=true actual_encoder=true live_active=%d live_owners=%d live_domain_claims=%d consumer_status=%d consumer_bytes=%d consumer_joined=true pinned_retirement=true source_fds=0 idle_after_store_join=true",
		runID, live.IO.Active, live.Owners.RegisteredOwners, live.DomainClaims, consumer.status, consumer.bytes)
}
