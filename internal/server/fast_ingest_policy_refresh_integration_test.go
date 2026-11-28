//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestHTTPFastIngestReleasesVerifiedEpisodesBeforeArtworkAndNotifiesRestrictedViewer(t *testing.T) {
	f := newFastIngestWarmNFOHTTPFixture(t)
	viewer := f.accounts.viewer
	setHTTPUserPolicy(t, f.serverFixture, viewer.userID, `{"MaxParentalRating":5,"BlockedTags":["Blocked Fixture"],"BlockUnratedItems":["Series"]}`)
	artwork, err := f.app.taskStore.GetByKey(f.ctx, library.TaskLocalArtworkIngestKey)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the real artwork worker out of this acceptance window. Leave its
	// durable request in place, rather than deleting the poster or its queue.
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=false WHERE id=$1`, artwork.ID); err != nil {
		t.Fatal(err)
	}
	const data = "synthetic episode source"
	show := f.write(t, "tvshows/Show/tvshow.nfo", `<tvshow><title>Accepted Show</title><mpaa>TV-PG</mpaa></tvshow>`)
	season := filepath.Join(filepath.Dir(show), "Season 1")
	initialPath := f.write(t, "tvshows/Show/Season 1/Show.S01E01.mp4", data)
	f.write(t, "tvshows/Show/Season 1/Show.S01E01.nfo", `<episodedetails><title>First Episode</title></episodedetails>`)
	f.write(t, "tvshows/Show/Season 1/season.nfo", `<season><title>Accepted Season</title><season>1</season></season>`)
	lib := f.createAndScan(t, "tvshows")
	settle := func(t *testing.T, name string) {
		t.Helper()
		scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f.serverFixture}, name, func() bool {
			var pending int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM local_metadata_ingest WHERE import_nfo`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			return pending == 0
		})
	}
	settle(t, "initial NFO verification")
	initial := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, lib, initialPath)
	if initial.Series == nil {
		t.Fatal("missing physical series")
	}
	seriesID := initial.Series.ID
	episodesURL := "/emby/Shows/" + seriesID + "/Episodes?UserId=" + viewer.userID
	f.assertList(t, viewer, episodesURL, initial.ID)
	waitLibraryScanNotifierIdle(t, f.serverFixture)
	server := websocketHTTPServer(t, f.serverFixture, time.Hour)
	socket := websocketHTTPDial(t, server, "/emby/socket", viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f.serverFixture, viewer.id, 1)

	allowedPath := f.write(t, "tvshows/Show/Season 1/Show.S01E02.mp4", data)
	f.write(t, "tvshows/Show/Season 1/Show.S01E02.nfo", `<episodedetails><title>Allowed Episode</title></episodedetails>`)
	maturePath := f.write(t, "tvshows/Show/Season 1/Show.S01E03.mp4", data)
	f.write(t, "tvshows/Show/Season 1/Show.S01E03.nfo", `<episodedetails><title>Restricted Rating</title><mpaa>TV-MA</mpaa></episodedetails>`)
	taggedPath := f.write(t, "tvshows/Show/Season 1/Show.S01E04.mp4", data)
	f.write(t, "tvshows/Show/Season 1/Show.S01E04.nfo", `<episodedetails><title>Restricted Tag</title><tag>Blocked Fixture</tag></episodedetails>`)
	brokenPath := f.write(t, "tvshows/Show/Season 1/Show.S01E05.mp4", data)
	f.write(t, "tvshows/Show/Season 1/Show.S01E05.nfo", `<episodedetails>unreadable metadata`)
	writeAPIJPEG(t, filepath.Join(season, "Show.S01E02-thumb.jpg"), 8, 12, 1)
	// Stop just NFO scheduling for this scan. This proves denial before the
	// verifier runs; the scanner and authorized WebSocket remain live.
	nfo, err := f.app.taskStore.GetByKey(f.ctx, library.TaskLocalMetadataIngestKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=false WHERE id=$1`, nfo.ID); err != nil {
		t.Fatal(err)
	}
	job := f.warmScan(t, lib)
	allowed := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, lib, allowedPath)
	mature := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, lib, maturePath)
	tagged := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, lib, taggedPath)
	broken := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, lib, brokenPath)
	assertPlayback := func(t *testing.T, item library.Item, status int) {
		t.Helper()
		f.assertDetail(t, viewer, item, status)
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+item.ID+"/PlaybackInfo", nil, viewer.headers), status)
		if status == http.StatusOK {
			status = http.StatusPartialContent
		}
		f.originalRange(t, viewer, item.ID, data, "", status)
	}
	f.assertList(t, viewer, episodesURL, initial.ID)
	for _, item := range []library.Item{allowed, mature, tagged, broken} {
		assertPlayback(t, item, http.StatusNotFound)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=true WHERE id=$1`, nfo.ID); err != nil {
		t.Fatal(err)
	}
	f.app.taskManager.Wake()
	// Read until the authorized projection reveals the newly accepted episode.
	// Other frames can invalidate a surviving parent, but none may name denied items.
	for {
		var envelope struct {
			MessageType string
			Data        libraryChangedData
		}
		if err := json.Unmarshal(socket.next(t), &envelope); err != nil || envelope.MessageType != "LibraryChanged" {
			t.Fatalf("unexpected catalog socket frame: %v", err)
		}
		for _, ids := range [][]string{envelope.Data.ItemsAdded, envelope.Data.ItemsUpdated, envelope.Data.ItemsRemoved} {
			if slices.Contains(ids, mature.ID) || slices.Contains(ids, tagged.ID) || slices.Contains(ids, broken.ID) {
				t.Fatal("catalog invalidation disclosed a denied episode")
			}
		}
		if slices.Contains(envelope.Data.ItemsUpdated, allowed.ID) {
			break
		}
	}
	f.assertList(t, viewer, episodesURL, initial.ID, allowed.ID)
	allowed.Name = "Allowed Episode"
	assertPlayback(t, allowed, http.StatusOK)
	for _, item := range []library.Item{mature, tagged, broken} {
		assertPlayback(t, item, http.StatusNotFound)
	}
	var nfoPending, imagesPending bool
	var imageCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT import_nfo,import_images,(SELECT count(*) FROM item_images WHERE item_id=$1)
		FROM local_metadata_ingest WHERE item_id=$1`, allowed.ID).Scan(&nfoPending, &imagesPending, &imageCount); err != nil || nfoPending || !imagesPending || imageCount != 0 {
		t.Fatalf("permitted playback depended on artwork: NFO=%t artwork=%t images=%d, %v", nfoPending, imagesPending, imageCount, err)
	}
	// A broken sidecar is independent of valid ones, but it never becomes a
	// trusted absence or a playable episode for the restricted account.
	f.assertNFOJob(t, broken, job)
}
