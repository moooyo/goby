//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
)

func TestHTTPLibraryNotifierNativeScansPublishCommittedCatalogChanges(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real Linux scan notification verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	for _, name := range []string{"From", "To"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal("create scan notification media directory")
		}
	}
	source := filepath.Join(root, "From", "Committed.Movie.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=blue:size=160x90:rate=24:duration=1",
		"-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24",
		"-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p", "-t", "1", source)
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal("stat scan notification source")
	}
	// Reopen the complete production server so its listener owns the real-probe
	// catalog. Replacing only f.app.library would leave the listener detached.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = f.app.Close(closeCtx)
	closeCancel()
	if err != nil {
		t.Fatalf("close initial scan notification server (%T)", err)
	}
	f.cfg.MediaRoots, f.cfg.FFmpegPath, f.cfg.FFprobePath = []string{root}, ffmpeg, ffprobe
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "scan-notification-integration")
	if err != nil {
		t.Fatalf("create production scan notification server (%T)", err)
	}
	// Capture this owner, and retire it before the media root is removed.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close production scan notification server (%T)", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	if app.catalogNotifier == nil || app.catalogNotifier.store != app.library {
		t.Fatal("Server.New did not register the catalog notifier on its real-probe store")
	}
	observer, err := app.eventHub.Subscribe(events.Scope{UserID: accounts.viewer.userID, SessionID: "native-scan-commit-observer"})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	nextPublication := func() (events.Event, libraryChangedData) {
		t.Helper()
		ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
		defer cancel()
		published, err := observer.Next(ctx)
		if err != nil || published.MessageType() != "LibraryChanged" || published.MessageID() == "" {
			t.Fatalf("native scan did not reach the production catalog notifier (%T)", err)
		}
		var envelope events.Envelope
		var data libraryChangedData
		if json.Unmarshal(published.Bytes(), &envelope) != nil || json.Unmarshal(envelope.Data, &data) != nil {
			t.Fatal("native scan publication has invalid catalog JSON")
		}
		return published, data
	}
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Native scan notifications", "CollectionType": "movies", "Paths": []string{root}, "Scan": false,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	var clients []*websocketHTTPClient
	assertPublication := func(published events.Event, expected map[string][]string) {
		t.Helper()
		assertLibraryChangedPayload(t, published.Bytes(), published.MessageID(), expected)
		scoped := make(map[string]bool)
		for _, scope := range published.CatalogScopes() {
			var committed bool
			if scope.LibraryID != libraryID || scoped[scope.ItemID] {
				t.Fatal("native scan publication has duplicate or foreign catalog scopes")
			}
			if err := f.pool.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM items WHERE id = $1 AND library_id = $2)",
				scope.ItemID, libraryID).Scan(&committed); err != nil || !committed {
				t.Fatal("catalog event arrived before its item was independently visible as committed")
			}
			scoped[scope.ItemID] = true
		}
		for _, ids := range expected {
			for _, id := range ids {
				if !scoped[id] {
					t.Fatal("native scan publication omitted a trusted item or folder scope")
				}
			}
		}
		for _, client := range clients {
			assertLibraryChangedPayload(t, client.next(t), published.MessageID(), expected)
		}
	}
	// Consume library creation before connecting sockets, so only native scan
	// commits can produce the application messages observed below.
	createdPublication, _ := nextPublication()
	assertPublication(createdPublication, map[string][]string{"ItemsAdded": {libraryID}, "CollectionFolders": {libraryID}})
	setLibraryChangedFolders(t, f, accounts.viewer.userID, libraryID)
	setLibraryChangedFolders(t, f, accounts.other.userID)
	server := websocketHTTPServer(t, f, time.Hour)
	for _, account := range []clientSessionHTTPLogin{accounts.viewer, accounts.second} {
		clients = append(clients, websocketHTTPDial(t, server, "/emby/socket", account.headers, http.StatusSwitchingProtocols))
		websocketHTTPWaitCount(t, f, account.id, 1)
	}
	denied := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.other.id, 1)
	startScan := func(forceProbe bool) string {
		t.Helper()
		body := `{"ForceProbe":false}`
		if forceProbe {
			body = `{"ForceProbe":true}`
		}
		response := adminScanRawRequest(t, f, "/admin/v1/libraries/"+libraryID+"/scan", body,
			http.Header{"X-CSRF-Token": {csrf}, "Content-Type": {"application/json"}}, cookie)
		expectStatus(t, response, http.StatusAccepted)
		return assertAdminScanJobMode(t, f, objectValue(t, jsonObject(t, response), "Job"), libraryID, forceProbe)
	}
	finishScan := func(jobID string, forceProbe bool, added, updated float64) {
		t.Helper()
		job := waitAdminScanHTTPJob(t, f, cookie, libraryID, jobID, forceProbe)
		if job["Added"] != added || job["Updated"] != updated {
			t.Fatalf("native scan reported incorrect committed media counts: %#v", job)
		}
	}
	assertQuiet := func() {
		t.Helper()
		waitLibraryScanNotifierIdle(t, f)
		ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
		_, err := observer.Next(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("completed native scan emitted an additional catalog publication (%T)", err)
		}
		for _, client := range clients {
			client.quiet(t)
		}
		denied.ping(t)
		denied.quiet(t)
	}
	initialJob := startScan(false)
	scanned := make(map[string]string)
	// Directories and the movie commit separately. Read the publications before
	// waiting for job completion, and accept the filesystem's traversal order.
	for range 3 {
		published, data := nextPublication()
		if len(data.ItemsAdded) != 1 {
			t.Fatal("initial native scan did not publish one newly committed item")
		}
		id := data.ItemsAdded[0]
		var relative, parentID string
		if err := f.pool.QueryRow(f.ctx, "SELECT relative_path, parent_id FROM items WHERE id = $1 AND library_id = $2",
			id, libraryID).Scan(&relative, &parentID); err != nil {
			t.Fatal("read newly committed scan item from an independent connection")
		}
		if relative != "From" && relative != "To" && relative != "From/Committed.Movie.mp4" || scanned[relative] != "" {
			t.Fatal("initial native scan published an unexpected or duplicate item")
		}
		scanned[relative] = id
		assertPublication(published, map[string][]string{
			"ItemsAdded": {id}, "FoldersAddedTo": {parentID}, "CollectionFolders": {libraryID},
		})
	}
	finishScan(initialJob, false, 1, 0)
	itemID, fromID, toID := scanned["From/Committed.Movie.mp4"], scanned["From"], scanned["To"]
	assertMovie := func(path, parentID string) string {
		t.Helper()
		var storedPath, storedParent, identity string
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT path, parent_id, file_identity,
			(SELECT count(*) FROM items WHERE library_id = $2 AND NOT is_folder)
			FROM items WHERE id = $1 AND library_id = $2 AND type = 'Movie'`, itemID, libraryID).
			Scan(&storedPath, &storedParent, &identity, &count); err != nil || storedPath != path || storedParent != parentID || identity == "" || count != 1 {
			t.Fatal("native scan did not persist exactly one movie with its expected identity, path, and parent")
		}
		detailResponse := f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Items/"+itemID, nil, accounts.viewer.headers)
		expectStatus(t, detailResponse, http.StatusOK)
		detail := jsonObject(t, detailResponse)
		duration, ok := detail["RunTimeTicks"].(float64)
		if detail["Id"] != itemID || !ok || duration <= 0 {
			t.Fatal("authorized catalog lookup did not expose the committed, real-probed movie")
		}
		return identity
	}
	identity := assertMovie(source, fromID)
	assertQuiet()
	finishScan(startScan(false), false, 0, 0)
	assertQuiet()
	forcedJob := startScan(true)
	forced, _ := nextPublication()
	assertPublication(forced, map[string][]string{"ItemsUpdated": {itemID}})
	finishScan(forcedJob, true, 0, 1)
	if assertMovie(source, fromID) != identity {
		t.Fatal("forced scan replaced the unchanged movie identity")
	}
	after, err := os.Stat(source)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("scan notification fixture changed before its forced refresh")
	}
	assertQuiet()
	destination := filepath.Join(root, "To", filepath.Base(source))
	if err := os.Rename(source, destination); err != nil {
		t.Fatal("move scan notification fixture between existing folders")
	}
	movedJob := startScan(false)
	moved, _ := nextPublication()
	assertPublication(moved, map[string][]string{
		"ItemsUpdated": {itemID}, "FoldersRemovedFrom": {fromID}, "FoldersAddedTo": {toID},
	})
	if assertMovie(destination, toID) != identity {
		t.Fatal("within-library move replaced the movie identity")
	}
	finishScan(movedJob, false, 0, 1)
	assertQuiet()
}

func waitLibraryScanNotifierIdle(t *testing.T, f *serverFixture) {
	t.Helper()
	deadline, tick := time.NewTimer(3*time.Second), time.NewTicker(5*time.Millisecond)
	defer deadline.Stop()
	defer tick.Stop()
	for {
		notifier := f.app.catalogNotifier
		notifier.mu.Lock()
		idle := notifier.count == 0 && notifier.active == nil && len(notifier.queue) == 0
		notifier.mu.Unlock()
		if idle {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("catalog notifier did not finish publishing a completed scan")
		case <-f.ctx.Done():
			t.Fatal("scan notification fixture context ended")
		}
	}
}
