//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type browseRouteProjectionTrace struct {
	mu         sync.Mutex
	statements []string
}

func (trace *browseRouteProjectionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "i.created_at,") &&
		(strings.Contains(data.SQL, "FROM media_collection_entries e JOIN items i") || strings.Contains(data.SQL, "FROM episode_queue queue")) {
		trace.mu.Lock()
		trace.statements = append(trace.statements, data.SQL)
		trace.mu.Unlock()
	}
	return ctx
}

func (*browseRouteProjectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *browseRouteProjectionTrace) reset() {
	trace.mu.Lock()
	trace.statements = nil
	trace.mu.Unlock()
}

func (trace *browseRouteProjectionTrace) assertPrivateMediaOmitted(t *testing.T) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if len(trace.statements) != 1 || !strings.Contains(trace.statements[0], "i.media - 'VideoSeekIndexes'") {
		t.Fatalf("the route did not select exactly one browse media projection: statements=%d", len(trace.statements))
	}
}

func TestHTTPCollectionAndEpisodeQueueRoutesUseBrowseMediaProjection(t *testing.T) {
	f := newServerFixture(t)
	f.app.notifier.Close()
	f.app.catalogNotifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	trace := &browseRouteProjectionTrace{}
	config := f.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	catalog, err := library.New(reader, playbackHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	for _, name := range []string{"movies/Alpha.mp4", "movies/Beta.mp4",
		"series/Example Show/Season 01/Example.Show.S01E01.mp4", "series/Example Show/Season 01/Example.Show.S01E02.mp4"} {
		writeAPIMediaFile(t, root, name)
	}
	movies := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "movies"), "movies")
	television := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "series"), "tvshows")
	var movieIDs, mediaIDs []string
	var seriesID string
	if err := f.pool.QueryRow(f.ctx, `SELECT array_agg(id ORDER BY sort_name,id) FROM items WHERE library_id=$1 AND type='Movie'`, movies).Scan(&movieIDs); err != nil || len(movieIDs) != 2 {
		t.Fatalf("movie fixture: ids=%v err=%v", movieIDs, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series'`, television).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT array_agg(id ORDER BY id) FROM items WHERE library_id=ANY($1::text[]) AND media IS NOT NULL`, []string{movies, television}).Scan(&mediaIDs); err != nil {
		t.Fatal(err)
	}
	subject := library.Subject{UserID: userID}
	playlist, err := catalog.CreateCollection(f.ctx, subject, library.PlaylistKind, library.CollectionInput{
		Name: "Browse projection playlist", MediaType: "Video", ItemIDs: []string{movieIDs[0], movieIDs[0], movieIDs[1]}})
	if err != nil {
		t.Fatal(err)
	}
	box, err := catalog.CreateCollection(f.ctx, subject, library.BoxSetKind, library.CollectionInput{Name: "Browse projection box", ItemIDs: movieIDs})
	if err != nil {
		t.Fatal(err)
	}
	const fields = "EnableImages=false&Fields=MediaSources,MediaStreams,Path,Chapters"
	routes := []struct {
		name, path string
		native     bool
		count      int
		before     []map[string]any
	}{
		{name: "playlist", path: "/emby/Playlists/" + playlist.ID + "/Items?" + fields, count: 3},
		{name: "box set", path: "/emby/Collections/" + box.ID + "/Items?" + fields, count: 2},
		{name: "native playlist", path: "/admin/v1/playlists/" + playlist.ID + "/items?" + fields, native: true, count: 3},
		{name: "native box set", path: "/admin/v1/collections/" + box.ID + "/items?" + fields, native: true, count: 2},
		{name: "episode queue", path: "/emby/Shows/" + seriesID + "/Episodes?IsMissing=false&IsVirtualUnaired=false&" + fields, count: 2},
	}
	read := func(path string, native bool) ([]map[string]any, int) {
		t.Helper()
		if native {
			return responseItems(t, f.request(t, http.MethodGet, path, nil, nil, cookie))
		}
		return responseItems(t, f.request(t, http.MethodGet, path, nil, headers))
	}
	for index := range routes {
		items, count := read(routes[index].path, routes[index].native)
		if count != routes[index].count || len(items) != count {
			t.Fatalf("%s baseline count=%d items=%d", routes[index].name, count, len(items))
		}
		routes[index].before = items
	}
	indexes := []media.VideoSeekIndex{{Version: media.VideoSeekIndexVersion, Entries: make([]media.VideoSeekPoint, 128)}}
	encoded, err := json.Marshal(indexes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=media || jsonb_build_object('VideoSeekIndexes',$1::jsonb) WHERE id=ANY($2::text[])`, encoded, mediaIDs); err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			trace.reset()
			items, count := read(route.path, route.native)
			trace.assertPrivateMediaOmitted(t)
			if count != route.count || !reflect.DeepEqual(items, route.before) {
				t.Fatalf("%s changed public fields, order, or duplicate entry identity", route.name)
			}
			for _, item := range items {
				if item["MediaSources"] == nil || item["MediaStreams"] == nil || item["Path"] == nil || item["Chapters"] == nil {
					t.Fatalf("%s lost explicitly selected public media fields", route.name)
				}
			}
		})
	}
	for _, readDomain := range []func() (library.ItemResult, error){
		func() (library.ItemResult, error) {
			return catalog.CollectionItems(f.ctx, subject, playlist.ID, library.PlaylistKind, 0, 100)
		},
		func() (library.ItemResult, error) {
			return catalog.CollectionItems(f.ctx, subject, box.ID, library.BoxSetKind, 0, 100)
		},
		func() (library.ItemResult, error) { return catalog.EpisodePlaybackQueue(f.ctx, subject, seriesID) },
	} {
		result, err := readDomain()
		if err != nil || len(result.Items) == 0 {
			t.Fatalf("complete default domain read: count=%d err=%v", len(result.Items), err)
		}
		for _, item := range result.Items {
			if item.Media == nil || len(item.Media.VideoSeekIndexes) != 1 || len(item.Media.VideoSeekIndexes[0].Entries) != 128 {
				t.Fatal("an omitted projection narrowed the complete domain item contract")
			}
		}
	}
}
