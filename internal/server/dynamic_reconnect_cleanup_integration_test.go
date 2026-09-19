//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func waitDynamicCleanupPath(t *testing.T, path string, exists bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(path)
		if exists && err == nil && info.IsDir() || !exists && errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cannot inspect the owned dynamic job directory (%T)", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("dynamic job directory did not reach exists=%t before its bounded cleanup deadline", exists)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestDynamicTimeshiftHTTPReconnectReclaimsOldScratchAndStopClosesCurrent(t *testing.T) {
	d := newDynamicTimeshiftHTTPFixture(t, false)
	p := d.open(t)
	h := d.h
	// The fixture's default engine idle retention is two minutes. A five-second
	// cleanup bound must therefore be satisfied by explicit epoch retirement.
	if options := h.f.app.cfg.Transcoding.ManagerOptions(h.ffmpeg, nil); options.IdleTimeout != 0 {
		t.Fatal("fixture must retain the engine's default idle timeout rather than shorten cache retention")
	}
	playlist := h.request(t, http.MethodGet, p.mainURL, nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	children := hlsHTTPManifestChildren(playlist.body)
	if len(children) == 0 {
		t.Fatal("the first advertised epoch contains no readable media")
	}
	retainedURL := children[0]
	before := h.request(t, http.MethodGet, retainedURL, nil, nil)
	expectHLSHTTPStatus(t, before, http.StatusOK)
	if len(before.body) == 0 {
		t.Fatal("the first retained artifact contains no media bytes")
	}
	report := map[string]any{"PlaySessionId": p.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing", report, h.accounts.viewer.headers), http.StatusNoContent)
	server := h.f.app
	server.dynamicStreams.mu.Lock()
	session := server.dynamicStreams.sessions[p.presentationID]
	server.dynamicStreams.mu.Unlock()
	if session == nil {
		t.Fatal("the advertised dynamic presentation was not registered")
	}
	session.mu.Lock()
	oldJob, windowID := session.jobID, session.windowID
	scope := session.scope
	session.mu.Unlock()
	if oldJob == "" {
		t.Fatal("the first real producer has no job identity")
	}
	cache := h.f.app.cfg.Transcoding.CacheDirectory
	oldDirectory := filepath.Join(cache, oldJob)
	waitDynamicCleanupPath(t, oldDirectory, true)
	d.endFirst.Do(func() { close(d.firstEOF) })
	deadline := time.Now().Add(15 * time.Second)
	var currentJob string
	for {
		_ = d.window(t, p)
		snapshot, err := server.dynamicStreams.store.Snapshot(context.Background(), timeshiftScope(scope), windowID)
		if err != nil {
			t.Fatal("reconnection discarded the owned timeshift presentation")
		}
		session.mu.Lock()
		generation, job, currentWindow, closed := session.generation, session.jobID, session.windowID, session.closed
		session.mu.Unlock()
		if closed || currentWindow != windowID {
			t.Fatal("reconnection replaced or revoked the retained presentation")
		}
		if generation >= 2 && job != "" && job != oldJob && len(snapshot.Segments) > 0 && snapshot.Segments[len(snapshot.Segments)-1].Generation == generation {
			currentJob = job
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the next connection did not publish an independent retained epoch")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if d.mediaRequests.Load() < 2 {
		t.Fatal("the cleanup scenario did not reconnect the real HTTP source")
	}
	currentDirectory := filepath.Join(cache, currentJob)
	waitDynamicCleanupPath(t, currentDirectory, true)
	waitDynamicCleanupPath(t, oldDirectory, false)
	// This URL was advertised before reconnect. Its independently copied Store
	// media and grace must outlive deletion of the old encoder's scratch.
	after := h.request(t, http.MethodGet, retainedURL, nil, nil)
	expectHLSHTTPStatus(t, after, http.StatusOK)
	if !bytes.Equal(before.body, after.body) {
		t.Fatal("old epoch scratch cleanup changed or removed the retained media")
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", report, h.accounts.viewer.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, p.windowURL, nil, nil), http.StatusNotFound)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, retainedURL, nil, nil), http.StatusNotFound)
	waitDynamicCleanupPath(t, currentDirectory, false)
	waitDynamicCleanupPath(t, oldDirectory, false)
	deadline = time.Now().Add(5 * time.Second)
	for {
		entries, err := os.ReadDir(cache)
		if err != nil {
			t.Fatal("cannot inspect the stopped presentation's dedicated transcode cache")
		}
		remaining := 0
		for _, entry := range entries {
			if entry.IsDir() {
				remaining++
			}
		}
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop left an old or current dynamic job directory behind")
		}
		time.Sleep(25 * time.Millisecond)
	}
	usage := server.dynamicStreams.store.Usage()
	if usage.Windows != 0 || usage.Bytes != 0 || usage.PendingBytes != 0 || usage.Readers != 0 || usage.Publishing != 0 {
		t.Fatal("stop retained timeshift files, readers, or publication reservations")
	}
}
