//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
)

func TestHTTPLibraryNotifierNativeAuxiliaryScansRespectMovieOwnershipAndCurrentACL(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real Linux auxiliary notification verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	for _, name := range []string{"backdrops", "featurettes"} {
		if err := os.MkdirAll(filepath.Join(root, "Film", name), 0o700); err != nil {
			t.Fatal("create the auxiliary notification media directories")
		}
	}
	mainPath := filepath.Join(root, "Film", "Main.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=green:size=160x90:rate=24:duration=1",
		"-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24",
		"-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p", "-t", "1", mainPath)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := f.app.Close(closeCtx)
	closeCancel()
	if err != nil {
		t.Fatalf("close the bootstrap auxiliary notification server (%T)", err)
	}
	f.cfg.MediaRoots, f.cfg.FFmpegPath, f.cfg.FFprobePath = []string{root}, ffmpeg, ffprobe
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "auxiliary-notification-integration")
	if err != nil {
		t.Fatalf("create the production auxiliary notification server (%T)", err)
	}
	// Capture the complete new owner so its workers close before its media root.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close the production auxiliary notification server (%T)", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	if app.catalogNotifier == nil || app.catalogNotifier.store != app.library {
		t.Fatal("Server.New did not register its notifier on the real-probe catalog")
	}
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Real auxiliary notifications", "CollectionType": "movies", "Paths": []string{root}, "Scan": true,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	creation := jsonObject(t, created)
	libraryID := stringValue(t, objectValue(t, creation, "Library"), "Id")
	initialJob := assertAdminScanJobMode(t, f, objectValue(t, creation, "Job"), libraryID, false)
	waitAuxiliaryNotifierHTTPScan(t, f, cookie, libraryID, initialJob, false, 1, 1, 0)
	waitLibraryScanNotifierIdle(t, f)
	var ownerID, ownerType string
	var ownerFolder bool
	if err := f.pool.QueryRow(f.ctx, "SELECT id,type,is_folder FROM items WHERE library_id=$1 AND path=$2", libraryID, mainPath).
		Scan(&ownerID, &ownerType, &ownerFolder); err != nil || ownerType != "Movie" || ownerFolder {
		t.Fatal("the actual scan did not establish a non-folder Movie semantic owner")
	}
	observer, err := app.eventHub.Subscribe(events.Scope{UserID: accounts.viewer.userID, SessionID: "auxiliary-publication-observer"})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	setLibraryChangedFolders(t, f, accounts.viewer.userID, libraryID)
	setLibraryChangedFolders(t, f, accounts.other.userID, libraryID)
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.second.headers, http.StatusSwitchingProtocols)
	denied := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	for _, sessionID := range []string{accounts.viewer.id, accounts.second.id, accounts.other.id} {
		websocketHTTPWaitCount(t, f, sessionID, 1)
	}
	// Revoke only library access after the connection is established. Delivery
	// must read the current ACL instead of retaining the handshake's access.
	setLibraryChangedFolders(t, f, accounts.other.userID)
	resources := []struct {
		table, path, id string
	}{
		{table: "item_theme_resources", path: filepath.Join(root, "Film", "backdrops", "Theme.mp4")},
		{table: "item_extra_resources", path: filepath.Join(root, "Film", "featurettes", "Clip.mp4")},
	}
	scan := func(force bool, scanned, added, updated int) {
		t.Helper()
		body := `{"ForceProbe":false}`
		if force {
			body = `{"ForceProbe":true}`
		}
		response := adminScanRawRequest(t, f, "/admin/v1/libraries/"+libraryID+"/scan", body,
			http.Header{"X-CSRF-Token": {csrf}, "Content-Type": {"application/json"}}, cookie)
		expectStatus(t, response, http.StatusAccepted)
		jobID := assertAdminScanJobMode(t, f, objectValue(t, jsonObject(t, response), "Job"), libraryID, force)
		waitAuxiliaryNotifierHTTPScan(t, f, cookie, libraryID, jobID, force, scanned, added, updated)
		waitLibraryScanNotifierIdle(t, f)
	}
	assertResources := func(active bool) {
		t.Helper()
		for index := range resources {
			resource := &resources[index]
			var id, parentID, semanticOwner, itemType string
			var storedActive bool
			if err := f.pool.QueryRow(f.ctx, "SELECT i.id,i.parent_id,i.type,r.owner_item_id,r.active FROM items i JOIN "+resource.table+
				" r ON r.resource_item_id=i.id WHERE i.library_id=$1 AND i.path=$2", libraryID, resource.path).
				Scan(&id, &parentID, &itemType, &semanticOwner, &storedActive); err != nil || id == "" ||
				parentID != ownerID || semanticOwner != ownerID || itemType != "Video" || storedActive != active {
				t.Fatal("the committed auxiliary record lost its real media role, Movie owner, or expected visibility")
			}
			if resource.id != "" && resource.id != id {
				t.Fatal("auxiliary refresh or retirement replaced the accepted item identity")
			}
			resource.id = id
			response := f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Items/"+id, nil, accounts.viewer.headers)
			if active {
				expectStatus(t, response, http.StatusOK)
				detail := jsonObject(t, response)
				duration, ok := detail["RunTimeTicks"].(float64)
				if detail["Id"] != id || !ok || duration <= 0 {
					t.Fatal("the authorized item query did not expose the committed real-probed auxiliary resource")
				}
			} else {
				expectStatus(t, response, http.StatusNotFound)
			}
		}
		if resources[0].id == resources[1].id {
			t.Fatal("independent theme and extra files reused one catalog identity")
		}
	}
	messageIDs := make(map[string]bool)
	visible := make(map[string]bool)
	observe := func(wantField string) {
		t.Helper()
		covered := make(map[string]bool)
		publications := 0
		for {
			ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
			published, err := observer.Next(ctx)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				break
			}
			if err != nil || published.MessageType() != "LibraryChanged" || published.MessageID() == "" || messageIDs[published.MessageID()] {
				t.Fatalf("auxiliary publication lost its shared identity or disconnected for resynchronization (%T)", err)
			}
			publications++
			messageIDs[published.MessageID()] = true
			if wantField == "" {
				t.Fatal("a cached auxiliary scan emitted a catalog publication")
			}
			var envelope events.Envelope
			var data libraryChangedData
			if json.Unmarshal(published.Bytes(), &envelope) != nil || json.Unmarshal(envelope.Data, &data) != nil {
				t.Fatal("the auxiliary publication has invalid public JSON")
			}
			fields := map[string][]string{
				"ItemsAdded": data.ItemsAdded, "ItemsUpdated": data.ItemsUpdated, "ItemsRemoved": data.ItemsRemoved,
				"FoldersAddedTo": data.FoldersAddedTo, "FoldersRemovedFrom": data.FoldersRemovedFrom, "CollectionFolders": data.CollectionFolders,
			}
			scoped := make(map[string]bool)
			for _, scope := range published.CatalogScopes() {
				var committed bool
				if scope.LibraryID != libraryID || scoped[scope.ItemID] {
					t.Fatal("the native auxiliary producer attached duplicate or foreign library scopes")
				}
				if err := f.pool.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM items WHERE id=$1 AND library_id=$2)",
					scope.ItemID, libraryID).Scan(&committed); err != nil || !committed {
					t.Fatal("an auxiliary notification did not retain an independently committed item scope")
				}
				scoped[scope.ItemID] = true
			}
			for _, ids := range fields {
				for _, id := range ids {
					if !scoped[id] {
						t.Fatal("a public auxiliary identifier lacks its trusted library scope")
					}
				}
			}
			for _, resource := range resources {
				added := slices.Contains(data.ItemsAdded, resource.id)
				updated := slices.Contains(data.ItemsUpdated, resource.id)
				removed := slices.Contains(data.ItemsRemoved, resource.id)
				if added && (updated || removed) || updated && removed ||
					wantField == "ItemsAdded" && removed || wantField == "ItemsRemoved" && added ||
					wantField == "ItemsUpdated" && (added || removed) {
					t.Fatal("the stable auxiliary scan phase emitted an incompatible visibility transition")
				}
				if added {
					if visible[resource.id] {
						t.Fatal("the native producer added an already visible auxiliary identity")
					}
					visible[resource.id] = true
				} else if updated || removed {
					if !visible[resource.id] {
						t.Fatal("the native producer changed an auxiliary identity before its addition or after its removal")
					}
					if removed {
						visible[resource.id] = false
					}
				}
				if slices.Contains(fields[wantField], resource.id) {
					covered[resource.id] = true
				}
				if added || updated || removed {
					if !slices.Contains(data.ItemsUpdated, ownerID) {
						t.Fatal("an auxiliary change did not invalidate its Movie owner in the same committed publication")
					}
				}
				if slices.Contains(data.FoldersAddedTo, resource.id) || slices.Contains(data.FoldersRemovedFrom, resource.id) {
					t.Fatal("an auxiliary item was published as a browse folder")
				}
			}
			if slices.Contains(data.FoldersAddedTo, ownerID) || slices.Contains(data.FoldersRemovedFrom, ownerID) {
				t.Fatal("the semantic Movie owner was published as a browse folder")
			}
			firstPayload, secondPayload := first.next(t), second.next(t)
			assertLibraryChangedPayload(t, firstPayload, published.MessageID(), fields)
			assertLibraryChangedPayload(t, secondPayload, published.MessageID(), fields)
			if !bytes.Equal(firstPayload, secondPayload) {
				t.Fatal("two authorized sessions did not share the same auxiliary publication")
			}
		}
		if wantField != "" && (publications == 0 || len(covered) != len(resources)) {
			t.Fatalf("the native scan did not publish %s for both auxiliary resources", wantField)
		}
		for _, client := range []*websocketHTTPClient{first, second, denied} {
			client.ping(t)
			client.quiet(t)
		}
	}
	// Encode once, then create independent regular files. Separate inodes avoid
	// hardlink identity effects while both roles retain valid real H.264 media.
	encoded, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal("read the encoded auxiliary fixture media")
	}
	for _, resource := range resources {
		if err := os.WriteFile(resource.path, encoded, 0o600); err != nil {
			t.Fatal("create the independent real auxiliary media file")
		}
	}
	scan(false, 3, 2, 0)
	assertResources(true)
	observe("ItemsAdded")
	scan(false, 3, 0, 0)
	observe("")
	scan(true, 3, 0, 3)
	assertResources(true)
	// Theme inheritance can invalidate an already visible extra before that
	// extra's own forced refresh commits. Coverage must allow those real batches.
	observe("ItemsUpdated")
	for _, resource := range resources {
		if err := os.Remove(resource.path); err != nil {
			t.Fatal("remove the auxiliary source while retaining its registered directories")
		}
	}
	scan(false, 1, 0, 0)
	assertResources(false)
	observe("ItemsRemoved")
}

func waitAuxiliaryNotifierHTTPScan(t *testing.T, f *serverFixture, cookie *http.Cookie,
	libraryID, jobID string, force bool, scanned, added, updated int) {
	t.Helper()
	deadline, tick := time.NewTimer(30*time.Second), time.NewTicker(25*time.Millisecond)
	defer deadline.Stop()
	defer tick.Stop()
	for {
		jobs, _ := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
		for _, job := range jobs {
			if job["Id"] != jobID {
				continue
			}
			assertAdminScanJobMode(t, f, job, libraryID, force)
			switch job["Status"] {
			case "completed":
				if job["Error"] != "" || job["Scanned"] != float64(scanned) || job["Added"] != float64(added) || job["Updated"] != float64(updated) {
					t.Fatalf("the real auxiliary scan did not inspect and commit its expected media population: %#v", job)
				}
				return
			case "failed", "cancelled", "interrupted":
				t.Fatalf("the real auxiliary scan did not complete: %#v", job)
			}
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("the real auxiliary scan did not complete before its media deadline")
		case <-f.ctx.Done():
			t.Fatal("the auxiliary notification fixture context ended")
		}
	}
}
