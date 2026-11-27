//go:build linux

package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func TestHTTPFastIngestPublishesBeforeLocalMetadataAndReopensPendingWork(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	tools := t.TempDir()
	probe, probeLog, ffmpeg, indexLog := fastIngestFixtureTools(t, tools)
	// The fixture exercises the process and descriptor gateway with
	// deterministic output. Its synthetic bytes do not exercise a decoder.
	cfg := f.cfg
	cfg.MediaRoots, cfg.FFprobePath, cfg.FFmpegPath, cfg.FastLibraryIngest = []string{root}, probe, ffmpeg, true
	fastIngestReopen(t, f, cfg)
	// Stop only the task coordinator, leaving the real scanner and HTTP
	// playback service running. Reopening must discover the durable requests.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	moviePath := filepath.Join(root, "movies", "Film.mp4")
	episodePath := filepath.Join(root, "tv", "Season 1", "Show.S01E02.mp4")
	movieBytes, episodeBytes := []byte("synthetic movie bytes"), []byte("synthetic episode bytes")
	writeFastIngestFile(t, moviePath, movieBytes, 0600)
	writeFastIngestFile(t, episodePath, episodeBytes, 0600)
	writeFastIngestFile(t, filepath.Join(root, "movies", "Film.nfo"), []byte(`<movie><title>Accepted Movie</title><plot>Movie description</plot><mpaa>PG</mpaa></movie>`), 0600)
	writeFastIngestFile(t, filepath.Join(root, "tv", "tvshow.nfo"), []byte(`<tvshow><title>Accepted Show</title><mpaa>R</mpaa></tvshow>`), 0600)
	writeFastIngestFile(t, filepath.Join(root, "tv", "Season 1", "Show.S01E02.nfo"), []byte(`<episodedetails><title>Accepted Episode</title><season>3</season><episode>7</episode></episodedetails>`), 0600)
	var poster bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 6))
	canvas.Set(0, 0, color.RGBA{R: 150, A: 255})
	if err := png.Encode(&poster, canvas); err != nil {
		t.Fatal(err)
	}
	writeFastIngestFile(t, filepath.Join(root, "movies", "Film-poster.png"), poster.Bytes(), 0600)
	movies, err := f.app.library.CreateLibrary(f.ctx, "Fast Movies", "movies", []string{filepath.Dir(moviePath)})
	if err != nil {
		t.Fatal(err)
	}
	television, err := f.app.library.CreateLibrary(f.ctx, "Fast TV", "tvshows", []string{filepath.Join(root, "tv")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, accounts.other.userID); err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, movies.ID)
	(&streamHTTPFixture{f: f}).rescan(t, television.ID)
	movie := fastIngestItem(t, f, accounts.viewer.userID, movies.ID, moviePath)
	episode := fastIngestItem(t, f, accounts.viewer.userID, television.ID, episodePath)
	if movie.Name != "Film" || episode.Name != "Show S01E02" || episode.Series == nil || episode.Season == nil || episode.IndexNumber != 2 {
		t.Fatalf("initial structure was not published before local import: movie=%q episode=%q", movie.Name, episode.Name)
	}
	playlist := func(login clientSessionHTTPLogin, item library.Item) string {
		return "/emby/Users/" + login.userID + "/Items/" + item.ID
	}
	for _, fixture := range []struct {
		item library.Item
		data []byte
	}{{movie, movieBytes}, {episode, episodeBytes}} {
		item := fixture.item
		if item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion || len(item.Media.VideoSeekIndexes) != 0 {
			t.Fatal("fast scan did not publish basic playback facts")
		}
		expectStatus(t, f.request(t, http.MethodGet, playlist(accounts.viewer, item), nil, accounts.viewer.headers), http.StatusOK)
		expectStatus(t, f.request(t, http.MethodGet, playlist(accounts.other, item), nil, accounts.other.headers), http.StatusNotFound)
		original := f.request(t, http.MethodGet, "/emby/Videos/"+item.ID+"/original.mp4", nil, accounts.viewer.headers)
		expectStatus(t, original, http.StatusOK)
		if !bytes.Equal(original.Body.Bytes(), fixture.data) {
			t.Fatal("original bytes did not play while metadata was pending")
		}
	}
	// Blocking only unrated music must not hide pending movie or TV items.
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=(policy-'MaxParentalRating')||'{"BlockUnratedItems":["Music"]}'::jsonb WHERE id=$1`, accounts.other.userID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []library.Item{movie, episode} {
		expectStatus(t, f.request(t, http.MethodGet, playlist(accounts.other, item), nil, accounts.other.headers), http.StatusOK)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=(policy-'BlockUnratedItems')||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, accounts.other.userID); err != nil {
		t.Fatal(err)
	}
	var imageCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_images WHERE item_id=$1`, movie.ID).Scan(&imageCount); err != nil || imageCount != 0 {
		t.Fatalf("image import ran during fast scan: %d %v", imageCount, err)
	}
	if _, err := os.Stat(indexLog); !os.IsNotExist(err) {
		t.Fatalf("scan or original playback attempted full indexing: %v", err)
	}
	metaPath := "/admin/v1/items/" + movie.ID + "/metadata"
	metadata := f.request(t, http.MethodGet, metaPath, nil, nil, accounts.cookie)
	expectStatus(t, metadata, http.StatusOK)
	edit := map[string]any{"Revision": jsonObject(t, metadata)["Revision"], "Overrides": map[string]any{"Name": "Manual Movie"}, "LockedFields": []string{"Overview"}}
	expectStatus(t, f.request(t, http.MethodPut, metaPath, edit, http.Header{"X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}, accounts.cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+accounts.viewer.userID+"/FavoriteItems/"+movie.ID, nil, accounts.viewer.headers), http.StatusOK)
	stamp := fastIngestSourceStamp(t, f, movie.ID)
	probeBefore := fastIngestToolLog(t, probeLog)
	fastIngestReopen(t, f, cfg)
	waitFastIngestDrain(t, f)
	waitFastIngestSeekRows(t, f, 2)
	file, source, err := f.app.library.OpenMedia(f.ctx, accounts.viewer.userID, movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	indexes := f.app.library.IngestedVideoSeekIndexes(f.ctx, source)
	if len(indexes) != 1 || media.ValidateVideoSeekIndex(indexes[0]) != nil || len(source.Item.Media.VideoSeekIndexes) != 0 {
		t.Fatal("accepted background seek evidence was not available independently of the media document")
	}
	indexBefore := fastIngestToolLog(t, indexLog)
	if strings.Count(indexBefore, "index\n") != 2 || fastIngestToolLog(t, probeLog) != probeBefore {
		t.Fatal("the background task did not make exactly one index per source without repeating ffprobe")
	}
	movieAfter := fastIngestItem(t, f, accounts.viewer.userID, movies.ID, moviePath)
	episodeAfter := fastIngestItem(t, f, accounts.viewer.userID, television.ID, episodePath)
	if movieAfter.ID != movie.ID || movieAfter.Name != "Manual Movie" || movieAfter.Overview != "" || movieAfter.UserData == nil || !movieAfter.UserData.IsFavorite ||
		episodeAfter.ID != episode.ID || episodeAfter.ParentID != episode.ParentID || episodeAfter.IndexNumber != 7 || episodeAfter.ParentIndexNumber != 1 ||
		episodeAfter.Name != "Accepted Episode" || episodeAfter.Series == nil || episodeAfter.Series.Name != "Accepted Show" || fastIngestSourceStamp(t, f, movie.ID) != stamp {
		t.Fatalf("local import changed source identity, locked metadata or history: movie=%q episode=%q", movieAfter.Name, episodeAfter.Name)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_images WHERE item_id=$1`, movie.ID).Scan(&imageCount); err != nil || imageCount != 1 {
		t.Fatalf("local poster not imported: %d %v", imageCount, err)
	}
	expectStatus(t, f.request(t, http.MethodGet, playlist(accounts.other, movieAfter), nil, accounts.other.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, playlist(accounts.other, episodeAfter), nil, accounts.other.headers), http.StatusNotFound)
	// Warm scans must discover sidecar deletion without replacing the item or
	// probing its unchanged media again.
	if err := os.Remove(filepath.Join(root, "tv", "Season 1", "Show.S01E02.nfo")); err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, television.ID)
	waitFastIngestDrain(t, f)
	if item := fastIngestItem(t, f, accounts.viewer.userID, television.ID, episodePath); item.ID != episode.ID || item.IndexNumber != 2 || item.Name != "Show S01E02" {
		t.Fatalf("stable sidecar absence did not restore filename facts: %+v", item)
	}
	if fastIngestToolLog(t, probeLog) != probeBefore || fastIngestToolLog(t, indexLog) != indexBefore {
		t.Fatal("an unchanged media source was probed or indexed again after sidecar deletion")
	}
	// A new accepted source with the same item ID cannot retrieve the old
	// evidence. A stale authorization snapshot cannot retrieve it either.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	writeFastIngestFile(t, moviePath, []byte("replacement synthetic movie"), 0600)
	(&streamHTTPFixture{f: f}).rescan(t, movies.ID)
	if fastIngestSourceStamp(t, f, movie.ID) == stamp || len(f.app.library.IngestedVideoSeekIndexes(f.ctx, source)) != 0 {
		t.Fatal("the old indexed evidence remained authorized after a source replacement")
	}
	currentFile, current, err := f.app.library.OpenMedia(f.ctx, accounts.viewer.userID, movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer currentFile.Close()
	if current.Item.ID != movie.ID || current.ETag == source.ETag || len(f.app.library.IngestedVideoSeekIndexes(f.ctx, current)) != 0 {
		t.Fatal("a new media source reused a prior source's seek evidence")
	}
}

func TestHTTPFastIngestAdminStopSurvivesRestartAndManualRetry(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	root, tools := t.TempDir(), t.TempDir()
	probe, _, ffmpeg, _ := fastIngestFixtureTools(t, tools)
	entered, gate := filepath.Join(tools, "entered"), filepath.Join(tools, "allow-index")
	// Keep the actual child alive until the administrator cancels it. Use the
	// same executable to finish without evidence when deliberately retried.
	writeFastIngestFile(t, ffmpeg, []byte(fmt.Sprintf(`#!/bin/sh
case "$*" in
-version) printf 'fixture ffmpeg\n';;
*framehash*) printf 'read\n' >> %q
while [ ! -f %q ]; do /bin/sleep 0.05; done
exit 1;;
esac
`, entered, gate)), 0700)
	cfg := f.cfg
	cfg.MediaRoots, cfg.FFprobePath, cfg.FFmpegPath, cfg.FastLibraryIngest = []string{root}, probe, ffmpeg, true
	fastIngestReopen(t, f, cfg)
	path := filepath.Join(root, "movies", "Film.mp4")
	writeFastIngestFile(t, path, []byte("synthetic source"), 0600)
	collection, err := f.app.library.CreateLibrary(f.ctx, "Stopped index movies", "movies", []string{filepath.Dir(path)})
	if err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	var runID, taskID string
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "background index admission", func() bool {
		def, err := f.app.taskStore.GetByKey(f.ctx, library.TaskVideoSeekIngestKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(entered); err != nil || def.CurrentRun == nil || def.CurrentRun.State != tasks.RunRunning {
			return false
		}
		runID, taskID = def.CurrentRun.ID, def.ID
		return true
	})
	csrf := http.Header{"X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/task-runs/"+runID+"/cancel", map[string]any{}, csrf, accounts.cookie), http.StatusAccepted)
	fastIngestWaitRun(t, f, runID, tasks.RunCancelled)
	fastIngestReopen(t, f, cfg)
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "startup index recovery check", func() bool {
		var emptyRecovery bool
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM task_runs recovered JOIN task_runs stopped ON stopped.id=$2
			WHERE recovered.task_id=$1 AND recovered.source='startup' AND recovered.created_at>stopped.finished_at
			AND recovered.state='completed' AND recovered.total_children=0)`, taskID, runID).Scan(&emptyRecovery); err != nil {
			t.Fatal(err)
		}
		return emptyRecovery
	})
	if fastIngestToolLog(t, entered) != "read\n" {
		t.Fatal("administrator-cancelled work started again at startup")
	}
	writeFastIngestFile(t, gate, []byte("retry allowed"), 0600)
	start := f.request(t, http.MethodPost, "/admin/v1/tasks/"+taskID+"/runs", map[string]any{}, csrf, accounts.cookie)
	expectStatus(t, start, http.StatusAccepted)
	manual := jsonObject(t, start)["Run"].(map[string]any)
	fastIngestWaitRun(t, f, stringValue(t, manual, "Id"), tasks.RunCompleted)
	if fastIngestToolLog(t, entered) != "read\nread\n" {
		t.Fatal("a deliberate manual retry did not reconsider the stopped generation")
	}
}

func fastIngestWaitRun(t *testing.T, f *serverFixture, id string, state tasks.RunState) {
	t.Helper()
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "ingest run "+string(state), func() bool {
		run, err := f.app.taskStore.GetRun(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.State.Active() && run.State != state {
			t.Fatalf("unexpected ingest terminal state: %s (%s)", run.State, run.ErrorCode)
		}
		return run.State == state
	})
}

func fastIngestReopen(t *testing.T, f *serverFixture, cfg config.Config) {
	t.Helper()
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "fast-ingest-integration")
	if err != nil {
		t.Fatal(err)
	}
	f.app, f.handler = app, app.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error(err)
		}
	})
}

func fastIngestFixtureTools(t *testing.T, directory string) (probe, probeLog, ffmpeg, indexLog string) {
	t.Helper()
	probe, probeLog = filepath.Join(directory, "probe"), filepath.Join(directory, "probe-invoked")
	ffmpeg, indexLog = filepath.Join(directory, "ffmpeg"), filepath.Join(directory, "index-invoked")
	writeFastIngestFile(t, probe, []byte(fmt.Sprintf(`#!/bin/sh
printf 'probe\n' >> %q
printf '%%s\n' '{"format":{"format_name":"mov,mp4","duration":"4","start_time":"0"},"streams":[{"index":0,"codec_name":"h264","codec_type":"video","width":64,"height":64,"pix_fmt":"yuv420p","bits_per_raw_sample":"8","time_base":"1/1000"}]}'
`, probeLog)), 0700)
	var framehash strings.Builder
	framehash.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n#software: Fast ingest fixture\n")
	for stream := 0; stream < 5; stream++ {
		codec := "h264"
		if stream == 1 {
			codec = "rawvideo"
		}
		fmt.Fprintf(&framehash, "#tb %d: 1/1000\n#media_type %d: video\n#codec_id %d: %s\n#dimensions %d: 64x64\n#sar %d: 1/1\n", stream, stream, stream, codec, stream, stream)
	}
	framehash.WriteString("#stream#, dts, pts, duration, size, hash\n")
	for _, pts := range []int{0, 1000, 2000, 3000} {
		fmt.Fprintf(&framehash, "0, %d, %d, 1, 120, %s\n", pts-100, pts, strings.Repeat("c", 64))
		fmt.Fprintf(&framehash, "2, %d, %d, 1, 40, %s\n", pts-100, pts, strings.Repeat("e", 64))
		fmt.Fprintf(&framehash, "3, %d, %d, 1, 120, %s\n", pts-100, pts, strings.Repeat("f", 64))
		fmt.Fprintf(&framehash, "1, %d, %d, 1, 6144, %s\n", pts, pts, strings.Repeat("d", 64))
	}
	writeFastIngestFile(t, ffmpeg, []byte(fmt.Sprintf(`#!/bin/sh
case "$*" in
-version) printf 'fixture ffmpeg\n';;
*framehash*) printf 'index\n' >> %q
while IFS= read -r line; do printf '%%s\n' "$line"; done <<'FRAMEHASH'
%sFRAMEHASH
;;
esac
`, indexLog, framehash.String())), 0700)
	return
}

func fastIngestToolLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitFastIngestSeekRows(t *testing.T, f *serverFixture, want int) {
	t.Helper()
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "background seek index publication", func() bool {
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_seek_ingest WHERE indexes IS NOT NULL`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count == want
	})
}

func writeFastIngestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func fastIngestItem(t *testing.T, f *serverFixture, userID, libraryID, path string) library.Item {
	t.Helper()
	result, err := f.app.library.QueryItems(f.ctx, library.Query{UserID: userID, ParentID: libraryID, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("missing catalog item %q", path)
	return library.Item{}
}

func fastIngestSourceStamp(t *testing.T, f *serverFixture, itemID string) string {
	t.Helper()
	var stamp string
	if err := f.pool.QueryRow(f.ctx, `SELECT `+library.MediaOperationSourceRevisionSQL+` FROM items i WHERE id=$1`, itemID).Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	return stamp
}

func waitFastIngestDrain(t *testing.T, f *serverFixture) {
	t.Helper()
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "local metadata task completion", func() bool {
		var pending int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM local_metadata_ingest`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		definition, err := f.app.taskStore.GetByKey(f.ctx, library.TaskLocalMetadataIngestKey)
		if err != nil {
			t.Fatal(err)
		}
		if definition.CurrentRun == nil && definition.LastRun != nil && definition.LastRun.State == tasks.RunFailed {
			t.Fatalf("local metadata task failed with %d pending: %s (%s)", pending, definition.LastRun.ErrorMessage, definition.LastRun.ErrorCode)
		}
		return pending == 0 && definition.CurrentRun == nil && definition.LastRun != nil && definition.LastRun.State == tasks.RunCompleted
	})
}
