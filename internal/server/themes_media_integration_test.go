//go:build linux

package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// This is a real scanner/probe and authorized HTTP byte-delivery regression.
// It does not establish original-client behavior or rendered audio/video output.
func TestHTTPThemeMediaRealScanAndAuthorizedDelivery(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real theme HTTP verification")
	}
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	film := filepath.Join(root, "Film")
	backdrops := filepath.Join(film, "backdrops")
	if err := os.MkdirAll(backdrops, 0o700); err != nil {
		t.Fatal("create owned theme fixture directories")
	}
	ownerPath := filepath.Join(film, "Feature.mp4")
	songPath := filepath.Join(film, "theme.mp3")
	videoPath := filepath.Join(backdrops, "video.mp4")
	for _, source := range []struct{ path, color, frequency string }{
		{ownerPath, "red", "440"}, {videoPath, "blue", "880"},
	} {
		hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", "color=c="+source.color+":size=160x90:rate=12:duration=2",
			"-f", "lavfi", "-i", "sine=frequency="+source.frequency+":sample_rate=48000:duration=2",
			"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1",
			"-g", "24", "-bf", "0", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1",
			"-b:a", "64000", "-t", "2", "-movflags", "+faststart", source.path)
	}
	const songTitle = "Owned Theme Song Title"
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=2", "-ac", "2", "-ar", "48000",
		"-c:a", "libmp3lame", "-b:a", "96000", "-threads:a", "1", "-id3v2_version", "3",
		"-metadata", "title="+songTitle, songPath)

	f.app.notifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}, []string{root})
	if err != nil {
		t.Fatalf("create real theme probe catalog (%T)", err)
	}
	f.app.cfg.FFmpegPath, f.app.cfg.FFprobePath = ffmpeg, ffprobe
	f.app.cfg.MediaRoots = []string{root}
	f.cfg = f.app.cfg
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() { f.app.notifier.Close() })
	collection, err := catalog.CreateLibrary(f.ctx, "Real Theme Media", "movies", []string{root})
	if err != nil {
		t.Fatalf("create the owned real theme library (%T)", err)
	}
	stream := &streamHTTPFixture{f: f}
	stream.rescan(t, collection.ID)
	stream.setPolicy(t, accounts.viewer.userID, true, []string{collection.ID})
	f.handler = f.app.Handler()
	transport := &hlsHTTPFixture{f: f, accounts: accounts, libraryID: collection.ID, server: httptest.NewServer(f.handler)}
	t.Cleanup(transport.server.Close)

	listed, err := catalog.QueryItems(f.ctx, library.Query{UserID: accounts.admin.userID,
		ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("read the scanned ordinary owner (%T)", err)
	}
	var owner library.Item
	for _, item := range listed.Items {
		if item.Path == ownerPath {
			owner = item
		}
		if item.Path == songPath || item.Path == videoPath || item.ThemeKind != "" {
			t.Fatal("real scanner exposed a theme through ordinary enumeration")
		}
	}
	if owner.ID == "" || owner.Type != "Movie" || owner.Media == nil || owner.Media.ProbeVersion != media.CurrentProbeVersion {
		t.Fatal("the real movie owner was not indexed with current probe facts")
	}
	var ownerNumber int64
	if err := f.pool.QueryRow(f.ctx, "SELECT id FROM theme_owner_ids WHERE item_id=$1", owner.ID).Scan(&ownerNumber); err != nil || ownerNumber <= 0 {
		t.Fatal("the scanned owner has no durable theme identity")
	}
	userDataSnapshot := func() string {
		t.Helper()
		var snapshot string
		if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id),'[]'::jsonb)::text
			FROM user_item_data d`).Scan(&snapshot); err != nil {
			t.Fatal("snapshot all owned theme fixture user data")
		}
		return snapshot
	}
	emptyHistory := userDataSnapshot()
	themeURL := "/emby/Items/" + owner.ID + "/ThemeMedia?" + url.Values{
		"UserId": {accounts.viewer.userID}, "Fields": {"Path,MediaSources,MediaStreams"},
	}.Encode()
	groups := themeHTTPGroups(t, f.request(t, http.MethodGet, themeURL, nil, accounts.viewer.headers))
	if groups["ThemeSongsResult"].owner != ownerNumber || groups["ThemeVideosResult"].owner != ownerNumber ||
		len(groups["ThemeSongsResult"].items) != 1 || len(groups["ThemeVideosResult"].items) != 1 {
		t.Fatal("real scan did not attach one song and one video to the actual movie owner")
	}
	type sourceEvidence struct {
		id, route, mime string
		data            []byte
	}
	sources := make([]sourceEvidence, 0, 2)
	for _, expected := range []struct{ group, path, kind, extra, codec, route, mime string }{
		{"ThemeSongsResult", songPath, "Audio", "ThemeSong", "mp3", "Audio", "audio/mpeg"},
		{"ThemeVideosResult", videoPath, "Video", "ThemeVideo", "h264", "Videos", "video/mp4"},
	} {
		item := groups[expected.group].items[0]
		id := stringValue(t, item, "Id")
		if item["Type"] != expected.kind || item["MediaType"] != expected.kind || item["ExtraType"] != expected.extra ||
			item["ParentId"] != owner.ID || item["Path"] != expected.path || id == owner.ID {
			t.Fatal("scanned theme DTO replaced the resource identity, type, path, or semantic parent")
		}
		if expected.kind == "Audio" && item["Name"] != songTitle {
			t.Fatal("the theme scanner discarded the actual MP3 title tag")
		}
		indexed, err := catalog.GetItem(f.ctx, accounts.viewer.userID, id)
		if err != nil || indexed.Media == nil || indexed.Media.ProbeVersion != media.CurrentProbeVersion ||
			indexed.Media.FileChangeTimeNs <= 0 || indexed.Media.DurationTicks < media.TicksPerSecond ||
			indexed.Media.DurationTicks > 3*media.TicksPerSecond || len(indexed.Media.Streams) == 0 || indexed.Media.Streams[0].Codec != expected.codec {
			t.Fatalf("theme resource lacks current real probe facts (%T)", err)
		}
		if expected.kind == "Audio" && (indexed.Media.EmbeddedMusic == nil || indexed.Media.EmbeddedMusic.Title != songTitle) {
			t.Fatal("the real MP3 probe did not retain its own embedded title")
		}
		original, err := os.ReadFile(expected.path)
		if err != nil || len(original) < 64 || len(original) > 1<<20 || indexed.Media.Size != int64(len(original)) {
			t.Fatal("owned generated theme bytes disagree with the indexed size")
		}
		mediaSources, ok := item["MediaSources"].([]any)
		if !ok || len(mediaSources) != 1 {
			t.Fatal("real theme DTO lost its media source")
		}
		source, ok := mediaSources[0].(map[string]any)
		streams, streamsOK := item["MediaStreams"].([]any)
		if !ok || !streamsOK || len(streams) != len(indexed.Media.Streams) || source["Id"] != media.SourceID(id) ||
			source["Path"] != expected.path || source["Size"] != float64(len(original)) ||
			source["RunTimeTicks"] != float64(indexed.Media.DurationTicks) || source["SupportsDirectPlay"] != true ||
			source["Container"] != media.CanonicalContainer(*indexed.Media, expected.path) {
			t.Fatal("ThemeMedia projection does not reflect the real indexed source")
		}
		firstStream, ok := streams[0].(map[string]any)
		if !ok || firstStream["Codec"] != expected.codec || firstStream["Type"] != expected.kind {
			t.Fatal("ThemeMedia stream fields do not reflect the actual audio/video probe")
		}
		sources = append(sources, sourceEvidence{id: id, route: expected.route, mime: expected.mime, data: original})
	}
	if userDataSnapshot() != emptyHistory {
		t.Fatal("ThemeMedia browsing created user data before any playback")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data
		(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at,updated_at)
		SELECT $1,id,1000000,7,true,false,'2025-01-02T03:04:05Z','2025-01-02T03:04:05Z'
		FROM unnest($2::text[]) item(id)`, accounts.viewer.userID, []string{owner.ID, sources[0].id, sources[1].id}); err != nil {
		t.Fatal("seed preexisting owned user data for the byte-delivery regression")
	}
	history := userDataSnapshot()
	streamURL := func(source sourceEvidence) string {
		return "/emby/" + source.route + "/" + source.id + "/stream?" + url.Values{
			"Static": {"true"}, "MediaSourceId": {media.SourceID(source.id)}, "UserId": {accounts.viewer.userID},
		}.Encode()
	}
	for _, source := range sources {
		target := streamURL(source)
		expectHLSHTTPStatus(t, transport.request(t, http.MethodGet, target, nil, nil), http.StatusUnauthorized)
		full := transport.request(t, http.MethodGet, target, nil, accounts.viewer.headers)
		expectHLSHTTPStatus(t, full, http.StatusOK)
		if !bytes.Equal(full.body, source.data) || full.header.Get("Content-Type") != source.mime ||
			full.header.Get("Accept-Ranges") != "bytes" || full.header.Get("Content-Length") != strconv.Itoa(len(source.data)) || full.header.Get("ETag") == "" {
			t.Fatal("authorized theme delivery did not return the exact generated resource bytes")
		}
		headers := accounts.viewer.headers.Clone()
		headers.Set("Range", "bytes=7-38")
		partial := transport.request(t, http.MethodGet, target, nil, headers)
		expectHLSHTTPStatus(t, partial, http.StatusPartialContent)
		if !bytes.Equal(partial.body, source.data[7:39]) || partial.header.Get("Content-Type") != source.mime ||
			partial.header.Get("Content-Range") != fmt.Sprintf("bytes 7-38/%d", len(source.data)) {
			t.Fatal("authorized theme range returned owner bytes or another resource representation")
		}
	}
	ordinary, _ := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+
		"/Items?Recursive=true&Fields=Path&Limit=100", nil, accounts.viewer.headers))
	for _, item := range ordinary {
		if item["Id"] == sources[0].id || item["Id"] == sources[1].id || item["Path"] == songPath || item["Path"] == videoPath {
			t.Fatal("HTTP ordinary enumeration exposed a scanned theme resource")
		}
	}
	// Remove only the generated song and let a complete real scan retire its
	// relationship. The historical item/UserData survives, but direct access stops.
	if err := os.Remove(songPath); err != nil {
		t.Fatal("remove the owned temporary theme song")
	}
	stream.rescan(t, collection.ID)
	var active bool
	if err := f.pool.QueryRow(f.ctx, "SELECT active FROM item_theme_resources WHERE resource_item_id=$1", sources[0].id).Scan(&active); err != nil || active {
		t.Fatal("complete real scan did not retain and retire the absent song relationship")
	}
	remaining := themeHTTPGroups(t, f.request(t, http.MethodGet, themeURL, nil, accounts.viewer.headers))
	if len(remaining["ThemeSongsResult"].items) != 0 || len(remaining["ThemeVideosResult"].items) != 1 {
		t.Fatal("retiring one missing theme changed the active sibling population")
	}
	expectHLSHTTPStatus(t, transport.request(t, http.MethodGet, streamURL(sources[0]), nil, accounts.viewer.headers), http.StatusNotFound)
	deniedRange := accounts.viewer.headers.Clone()
	deniedRange.Set("Range", "bytes=0-31")
	expectHLSHTTPStatus(t, transport.request(t, http.MethodGet, streamURL(sources[0]), nil, deniedRange), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Items/"+sources[0].id, nil, accounts.viewer.headers), http.StatusNotFound)

	expectHLSHTTPStatus(t, transport.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, accounts.viewer.headers), http.StatusNoContent)
	expectHLSHTTPStatus(t, transport.request(t, http.MethodGet, streamURL(sources[1]), nil, deniedRange), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, themeURL, nil, accounts.viewer.headers), http.StatusUnauthorized)
	siblingRange := accounts.second.headers.Clone()
	siblingRange.Set("Range", "bytes=0-31")
	sibling := transport.request(t, http.MethodGet, streamURL(sources[1]), nil, siblingRange)
	expectHLSHTTPStatus(t, sibling, http.StatusPartialContent)
	if !bytes.Equal(sibling.body, sources[1].data[:32]) || userDataSnapshot() != history {
		t.Fatal("inactive/revoked theme reads changed old user data or interrupted an authorized sibling session")
	}
}
