//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type deletedUserEncoderProcess struct {
	pid   int
	pidfd int
}

func deletedUserObserveEncoder(t *testing.T, a *audioHTTPFixture, producerID string, count int) deletedUserEncoderProcess {
	t.Helper()
	pids := a.encoderPIDs(t)
	if len(pids) != count {
		t.Fatalf("recorded encoder count = %d, want %d", len(pids), count)
	}
	pid := pids[len(pids)-1]
	descriptor, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatalf("open the owned encoder process handle (%T)", err)
	}
	t.Cleanup(func() { _ = unix.Close(descriptor) })
	process := deletedUserEncoderProcess{pid: pid, pidfd: descriptor}
	// The fixture logs before exec. Actual output has already been read; bind
	// this live PID to FFmpeg and this exact producer's cache directory as well.
	resolved, resolveErr := exec.LookPath(a.ffmpeg)
	executable, executableErr := filepath.EvalSymlinks(resolved)
	actualExecutable, processErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	directory, directoryErr := filepath.EvalSymlinks(filepath.Join(a.f.app.cfg.Transcoding.CacheDirectory, producerID))
	actualDirectory, cwdErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if resolveErr != nil || executableErr != nil || processErr != nil || directoryErr != nil || cwdErr != nil ||
		actualExecutable != executable || actualDirectory != directory || process.exited(t) {
		t.Fatal("the observed live process is not the selected real FFmpeg producer")
	}
	return process
}

func (process deletedUserEncoderProcess) exited(t *testing.T) bool {
	t.Helper()
	// Poll the retained process handle, not a potentially reused numeric PID.
	// This helper never sends a signal; cancellation must come from the server.
	for {
		files := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(files, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			t.Fatalf("inspect the owned encoder process handle: %v", err)
		}
		if files[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			t.Fatalf("the owned encoder process handle returned an invalid event: ready=%d revents=%#x", ready, files[0].Revents)
		}
		if ready == 0 {
			return false
		}
		if files[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
			t.Fatalf("the owned encoder process handle returned an unknown event: ready=%d revents=%#x", ready, files[0].Revents)
		}
		return true
	}
}

func TestHTTPDeleteManagedUserStopsRealEncoderAndSocketsWithoutInterruptingOtherUser(t *testing.T) {
	a := newAudioHTTPFixture(t, true)
	if a.accounts.viewer.userID == a.accounts.other.userID || a.accounts.second.userID != a.accounts.viewer.userID {
		t.Fatal("deletion fixture does not separate the target account from the surviving account")
	}
	// Give the independently owned other account the same fixture-library access.
	if changed, err := a.f.pool.Exec(a.f.ctx, `UPDATE users SET policy =
		(SELECT policy FROM users WHERE id = $1) WHERE id = $2`, a.accounts.viewer.userID, a.accounts.other.userID); err != nil || changed.RowsAffected() != 1 {
		t.Fatal("grant the other fixture user access to the real audio source")
	}
	csrf := adminSessionHTTPCSRF(t, a.f, a.accounts.cookie)
	user := managedHTTPDetail(t, a.f, a.accounts.cookie, a.accounts.viewer.userID)
	// Periodic authorization must not stand in for DELETE's consumer retirement.
	a.f.app.sockets.revalidateEvery, a.f.app.sockets.pingEvery = time.Hour, time.Hour
	firstSocket := websocketHTTPDial(t, a.server, "/emby/socket", a.accounts.viewer.headers, http.StatusSwitchingProtocols)
	secondSocket := websocketHTTPDial(t, a.server, "/emby/socket", a.accounts.second.headers, http.StatusSwitchingProtocols)
	peerSocket := websocketHTTPDial(t, a.server, "/emby/socket", a.accounts.other.headers, http.StatusSwitchingProtocols)
	for _, login := range []clientSessionHTTPLogin{a.accounts.viewer, a.accounts.second, a.accounts.other} {
		websocketHTTPWaitCount(t, a.f, login.id, 1)
	}
	if len(a.encoderPIDs(t)) != 0 {
		t.Fatal("the deletion fixture already has an unrelated encoder")
	}
	// Start the peer first, so the target is the newest slow producer when deleted.
	const peerReference = "delete-user-surviving-audio"
	peer := a.openLiveFor(t, a.accounts.other, peerReference)
	peerPlayID := a.canonical(t, a.accounts.other, peerReference, "flac")
	peerSession, peerRecord := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
	peerProcess := deletedUserObserveEncoder(t, a, peerRecord.ID, 1)
	const targetReference = "delete-user-target-audio"
	target := a.openLive(t, targetReference)
	targetPlayID := a.canonical(t, a.accounts.viewer, targetReference, "flac")
	targetSession, targetRecord := a.liveSession(t, targetPlayID, 1)
	targetProcess := deletedUserObserveEncoder(t, a, targetRecord.ID, 2)
	_, peerBefore := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
	if peerProcess.pid == targetProcess.pid || peerRecord.ID == targetRecord.ID || peerBefore.ID != peerRecord.ID ||
		peerProcess.exited(t) || targetProcess.exited(t) {
		t.Fatal("deletion did not start with two distinct live real producers")
	}
	nativeHeaders := http.Header{"X-CSRF-Token": {csrf}}
	(&http.Request{Header: nativeHeaders}).AddCookie(a.accounts.cookie)
	response := a.request(t, http.MethodDelete, "/admin/v1/users/"+url.PathEscape(a.accounts.viewer.userID),
		map[string]any{"Revision": user["Revision"]}, nativeHeaders)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var outcome map[string]any
	if err := json.Unmarshal(response.body, &outcome); err != nil || len(outcome) != 1 || outcome["CurrentSessionRevoked"] != false || response.header.Get("Set-Cookie") != "" {
		t.Fatal("deleting another user changed the administrator response or cookie")
	}
	// Do not cancel either client request or close its body before these facts.
	// FK deletion and manager counters cannot establish that FFmpeg has exited.
	audioHTTPWait(t, "user deletion did not stop its real producer and release its owned readers and cache", func() bool {
		if !targetProcess.exited(t) || adminSessionHTTPHasHLS(a.f, targetSession.id) {
			return false
		}
		targetSession.mu.Lock()
		readers := targetSession.progressiveReaders
		targetSession.mu.Unlock()
		_, cacheErr := os.Stat(filepath.Join(a.f.app.cfg.Transcoding.CacheDirectory, targetRecord.ID))
		return readers == 0 && len(a.f.app.streamSlots) == 1 && errors.Is(cacheErr, os.ErrNotExist)
	})
	firstSocket.closed(t)
	secondSocket.closed(t)
	websocketHTTPWaitCount(t, a.f, a.accounts.viewer.id, 0)
	websocketHTTPWaitCount(t, a.f, a.accounts.second.id, 0)
	peerSocket.ping(t)
	websocketHTTPWaitCount(t, a.f, a.accounts.other.id, 1)
	_, peerAfter := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
	if peerAfter.ID != peerRecord.ID || peerProcess.exited(t) {
		t.Fatal("deletion stopped or replaced the other user's live producer")
	}
	_, interrupted := io.ReadAll(io.LimitReader(target.response.Body, 1<<20))
	if interrupted == nil || errors.Is(interrupted, context.DeadlineExceeded) || errors.Is(interrupted, context.Canceled) {
		t.Fatalf("deleted-user transport did not abort from the server (%T)", interrupted)
	}
	_ = target.response.Body.Close()
	target.cancel()
	remaining, err := io.ReadAll(io.LimitReader(peer.response.Body, 1<<20))
	if err != nil || len(remaining) == 0 || len(remaining) >= 1<<20 {
		t.Fatalf("user deletion interrupted the other user's complete audio stream (%T)", err)
	}
	_ = peer.response.Body.Close()
	peer.cancel()
	complete := append(append([]byte(nil), peer.prefix...), remaining...)
	a.verifyMP3(t, complete, 6)
	audioHTTPWait(t, "the surviving stream did not complete normally on its original producer", func() bool {
		peerSession.mu.Lock()
		readers, closed := peerSession.progressiveReaders, peerSession.closed
		peerSession.mu.Unlock()
		record, snapshotErr := a.f.app.hls.manager.Snapshot(peerSession.key.scope, peerRecord.ID)
		return readers == 0 && !closed && snapshotErr == nil && record.State == "completed" && peerProcess.exited(t) && len(a.f.app.streamSlots) == 0
	})
	peerSocket.ping(t)
	cached := a.request(t, http.MethodGet, a.universal("flac", audioHTTPMP3Query(peerReference, 0)), nil, a.accounts.other.headers)
	expectHLSHTTPStatus(t, cached, http.StatusOK)
	if !bytes.Equal(cached.body, complete) || len(a.encoderPIDs(t)) != 2 {
		t.Fatal("the surviving user lost its cached output or started a replacement producer")
	}
	for _, login := range []clientSessionHTTPLogin{a.accounts.viewer, a.accounts.second} {
		expectHLSHTTPStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized)
	}
	expectHLSHTTPStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, a.accounts.other.headers), http.StatusOK)
	expectHLSHTTPStatus(t, a.request(t, http.MethodGet, "/admin/v1/session", nil, nativeHeaders), http.StatusOK)
}
