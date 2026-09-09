//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type applicationKeyCatalogProber struct{}

func (applicationKeyCatalogProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	return media.Info{
		Container: "mp4", DurationTicks: 120 * media.TicksPerSecond, Size: stat.Size(),
		Streams: []media.Stream{{Index: 0, Codec: "h264", CodecType: "video", Width: 320, Height: 180}},
	}, nil
}

type applicationKeyCatalogFixture struct {
	f                                       *serverFixture
	adminID, viewerID, targetID             string
	visibleID, hiddenID, seriesID, seasonID string
	movieID, firstEpisodeID, nextEpisodeID  string
	key                                     identity.ApplicationKey
	headers, viewerHeaders                  http.Header
	adminCookie                             *http.Cookie
	poster                                  []byte
}

func newApplicationKeyCatalogFixture(t *testing.T) *applicationKeyCatalogFixture {
	t.Helper()
	f := newServerFixture(t)
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close default catalog: %v", err)
	}
	root := t.TempDir()
	catalog, err := library.New(f.pool, applicationKeyCatalogProber{}, []string{root})
	if err != nil {
		t.Fatalf("create application catalog: %v", err)
	}
	f.app.library = catalog
	f.app.cfg.MediaRoots = []string{root}
	f.cfg.MediaRoots = []string{root}
	f.users = identity.NewWithApplicationKeyVault(f.pool,
		identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "application-keys.master")))
	f.app.identity = f.users
	f.handler = f.app.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close application catalog: %v", err)
		}
	})
	for _, relative := range []string{
		"visible/Visible.Movie.mp4", "hidden/Hidden.Movie.mp4", "hidden/Other.Movie.mp4",
		"tv/Private Show/Season 01/Private.Show.S01E01.mp4",
		"tv/Private Show/Season 01/Private.Show.S01E02.mp4",
	} {
		writeAPIMediaFile(t, root, relative)
	}
	if err := os.WriteFile(filepath.Join(root, "hidden", "Hidden.Movie.nfo"), []byte(
		`<movie><title>Hidden Movie</title><genre>Private Genre</genre><tag>Private Tag</tag><studio>Private Studio</studio><actor><name>Private Person</name></actor></movie>`), 0o600); err != nil {
		t.Fatalf("write application catalog metadata: %v", err)
	}
	a := &applicationKeyCatalogFixture{f: f, adminID: f.bootstrap(t)}
	a.poster = writeAPIJPEG(t, filepath.Join(root, "hidden", "Hidden.Movie-poster.jpg"), 32, 48, 1)
	var csrf string
	a.adminCookie, csrf = f.adminLogin(t)
	a.visibleID = createAndScanAPILibrary(t, f, a.adminCookie, csrf, filepath.Join(root, "visible"), "movies")
	a.hiddenID = createAndScanAPILibrary(t, f, a.adminCookie, csrf, filepath.Join(root, "hidden"), "movies")
	createAndScanAPILibrary(t, f, a.adminCookie, csrf, filepath.Join(root, "tv"), "tvshows")
	items, err := catalog.QueryItems(f.ctx, library.Query{UserID: a.adminID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatalf("read application catalog fixture: %v", err)
	}
	for _, item := range items.Items {
		switch {
		case item.Type == "Movie" && item.Name == "Hidden Movie":
			a.movieID = item.ID
		case item.Type == "Series":
			a.seriesID = item.ID
		case item.Type == "Season":
			a.seasonID = item.ID
		case item.Type == "Episode" && item.IndexNumber == 1:
			a.firstEpisodeID = item.ID
		case item.Type == "Episode" && item.IndexNumber == 2:
			a.nextEpisodeID = item.ID
		}
	}
	if a.movieID == "" || a.seriesID == "" || a.seasonID == "" || a.firstEpisodeID == "" || a.nextEpisodeID == "" {
		t.Fatal("application catalog fixture is incomplete")
	}
	viewer, err := f.users.CreateUser(f.ctx, "Catalog Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create catalog viewer: %v", err)
	}
	a.viewerID = viewer.ID
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{a.visibleID}})
	if err != nil {
		t.Fatalf("encode viewer policy: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", policy, a.viewerID); err != nil {
		t.Fatalf("restrict catalog viewer: %v", err)
	}
	viewerLogin := f.embyLogin(t, viewer.Name, "viewer-password")
	a.viewerHeaders = http.Header{"X-Emby-Token": {stringValue(t, viewerLogin, "AccessToken")}}
	target, err := f.users.CreateUser(f.ctx, "Disabled Target", "target-password", false)
	if err != nil {
		t.Fatalf("create catalog state target: %v", err)
	}
	a.targetID = target.ID
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled = true,
		policy = '{"EnableAllFolders":true,"EnabledFolders":[],"EnableMediaPlayback":false}' WHERE id = $1`, a.targetID); err != nil {
		t.Fatalf("disable catalog state target: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data (user_id, item_id, played, play_count, is_favorite)
		VALUES ($1, $2, true, 3, true), ($1, $3, true, 1, false)`, a.targetID, a.movieID, a.firstEpisodeID); err != nil {
		t.Fatalf("seed explicit target state: %v", err)
	}
	actor, err := f.users.Resolve(f.ctx, a.adminCookie.Value, "admin")
	if err != nil {
		t.Fatalf("resolve application key creator: %v", err)
	}
	a.key, err = f.users.CreateApplicationKey(f.ctx, actor, "Catalog Integration", "192.0.2.1", identity.Client{DeviceID: "catalog-application", Device: "Integration", Version: "1"})
	if err != nil {
		t.Fatalf("create application catalog key: %v", err)
	}
	a.headers = http.Header{"X-Emby-Token": {a.key.Token}}
	return a
}

func TestHTTPApplicationKeyCatalogIndependentScopeAndExplicitState(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	path := "/emby/Items?Recursive=true&IncludeItemTypes=Movie"
	items, total := responseItems(t, f.request(t, http.MethodGet, path+"&Limit=1", nil, a.headers))
	if total != 3 || len(items) != 1 {
		t.Fatalf("application catalog count/page = %d/%d, want 3/1", total, len(items))
	}
	if _, exists := items[0]["UserData"]; exists {
		t.Error("userless application catalog invented user data")
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, path+"&Limit=0", nil, a.headers))
	if total != 3 || len(items) != 0 {
		t.Fatalf("application zero-limit count/page = %d/%d, want 3/0", total, len(items))
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, path, nil, a.viewerHeaders, a.adminCookie))
	if total != 1 || len(items) != 1 || items[0]["Name"] != "Visible Movie" {
		t.Fatalf("ordinary viewer escaped its library scope through an administrator cookie: %#v", items)
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, path+"&UserId="+a.targetID+"&IsFavorite=true", nil, a.headers))
	if total != 1 || len(items) != 1 || items[0]["Id"] != a.movieID {
		t.Fatalf("application explicit target filter returned another scope: %#v", items)
	}
	data := objectValue(t, items[0], "UserData")
	if data["IsFavorite"] != true || data["Played"] != true || data["PlayCount"] != float64(3) {
		t.Errorf("application target projection lost persisted state: %#v", data)
	}
	detail := f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items/"+a.movieID, nil, a.headers)
	expectStatus(t, detail, http.StatusOK)
	if objectValue(t, jsonObject(t, detail), "ImageTags")["Primary"] != imageSourceTag(a.poster) {
		t.Error("application detail did not preserve its image authorization")
	}
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Views", nil, a.headers))
	if total != 3 || len(views) != 3 {
		t.Fatalf("application views rejected an all-folders disabled target: %#v", views)
	}
	root := f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items/Root", nil, a.headers)
	expectStatus(t, root, http.StatusOK)
	if jsonObject(t, root)["ChildCount"] != float64(3) {
		t.Error("application root count rejected an all-folders disabled target")
	}
	latest := responseArray(t, f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items/Latest?IncludeItemTypes=Movie&GroupItems=false", nil, a.headers))
	if len(latest) != 3 {
		t.Fatalf("application latest did not include all movies: %#v", latest)
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"&UserId="+a.targetID, nil, a.viewerHeaders), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, path+"&UserId=missing-target", nil, a.headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items?UserId="+a.viewerID, nil, a.headers), http.StatusBadRequest)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", a.adminID); err != nil {
		t.Fatalf("disable application key creator: %v", err)
	}
	_, total = responseItems(t, f.request(t, http.MethodGet, path, nil, a.headers))
	if total != 3 {
		t.Error("application catalog depended on its disabled creator")
	}
}

func TestHTTPApplicationKeyCatalogExplicitTargetLibraryACL(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	genreItems, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Genres", nil, a.headers))
	if total != 1 || len(genreItems) != 1 {
		t.Fatalf("application genre fixture = %#v, total %d", genreItems, total)
	}
	genreID := stringValue(t, genreItems[0], "Id")
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET
		policy = '{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":false}' WHERE id = $1`, a.targetID); err != nil {
		t.Fatalf("remove explicit target library access: %v", err)
	}
	for _, route := range []string{
		"/emby/Users/" + a.targetID + "/Views",
		"/emby/Users/" + a.targetID + "/Items?Recursive=true&Limit=2",
		"/emby/Items?Recursive=true&Limit=2&UserId=" + a.targetID,
		"/emby/Genres?UserId=" + a.targetID,
		"/emby/Tags?UserId=" + a.targetID,
		"/emby/Studios?UserId=" + a.targetID,
		"/emby/Persons?UserId=" + a.targetID,
		"/emby/Shows/NextUp?UserId=" + a.targetID,
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, route, nil, a.headers))
		if total != 0 || len(items) != 0 {
			t.Errorf("application explicit target route %s escaped empty library access: %#v, total %d", route, items, total)
		}
	}
	root := f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items/Root", nil, a.headers)
	expectStatus(t, root, http.StatusOK)
	if jsonObject(t, root)["ChildCount"] != float64(0) {
		t.Error("application explicit target root retained inaccessible libraries")
	}
	latest := responseArray(t, f.request(t, http.MethodGet,
		"/emby/Users/"+a.targetID+"/Items/Latest?IncludeItemTypes=Movie&GroupItems=false", nil, a.headers))
	if len(latest) != 0 {
		t.Errorf("application explicit target latest retained inaccessible movies: %#v", latest)
	}
	// Detail and nested reads must preserve the same target folder scope as
	// the captured empty Views and Items contract.
	for _, route := range []string{
		"/emby/Users/" + a.targetID + "/Items/" + a.movieID,
		"/emby/Users/" + a.targetID + "/Items/" + genreID,
		"/emby/Items/" + a.movieID + "/Images?UserId=" + a.targetID,
		"/emby/Genres/Private%20Genre?UserId=" + a.targetID,
		"/emby/Studios/Private%20Studio?UserId=" + a.targetID,
		"/emby/Persons/Private%20Person?UserId=" + a.targetID,
		"/emby/Shows/" + a.seriesID + "/Seasons?UserId=" + a.targetID,
		"/emby/Shows/" + a.seriesID + "/Episodes?SeasonId=" + a.seasonID + "&UserId=" + a.targetID,
		"/emby/Shows/NextUp?SeriesId=" + a.seriesID + "&UserId=" + a.targetID,
	} {
		expectStatus(t, f.request(t, http.MethodGet, route, nil, a.headers), http.StatusNotFound)
	}
	items, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?Recursive=true&IncludeItemTypes=Movie", nil, a.headers))
	if total != 3 || len(items) != 3 {
		t.Fatalf("target library changes leaked into the userless key catalog: %#v, total %d", items, total)
	}
	for _, item := range items {
		if _, exists := item["UserData"]; exists {
			t.Error("userless key catalog retained the preceding target's user data")
		}
	}
	for _, endpoint := range []string{"Genres", "Tags", "Studios", "Persons"} {
		items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/"+endpoint, nil, a.headers))
		if total != 1 || len(items) != 1 {
			t.Errorf("target library changes leaked into userless %s: %#v, total %d", endpoint, items, total)
		}
	}
	images := responseArray(t, f.request(t, http.MethodGet, "/emby/Items/"+a.movieID+"/Images", nil, a.headers))
	if len(images) != 1 || images[0]["ImageType"] != "Primary" {
		t.Errorf("target library changes leaked into userless image metadata: %#v", images)
	}
}

func TestHTTPApplicationKeyCatalogNestedEntitiesImagesAndNextUp(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	for _, endpoint := range []struct{ path, name string }{
		{path: "Genres", name: "Private Genre"}, {path: "Tags", name: "Private Tag"},
		{path: "Studios", name: "Private Studio"}, {path: "Persons", name: "Private Person"},
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/"+endpoint.path, nil, a.headers))
		if total != 1 || len(items) != 1 || items[0]["Name"] != endpoint.name {
			t.Fatalf("application %s catalog = %#v, total %d", endpoint.path, items, total)
		}
		id := stringValue(t, items[0], "Id")
		detail := f.request(t, http.MethodGet, "/emby/Users/"+a.targetID+"/Items/"+id, nil, a.headers)
		expectStatus(t, detail, http.StatusOK)
		if jsonObject(t, detail)["Name"] != endpoint.name {
			t.Errorf("application entity fallback lost %s", endpoint.path)
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Genres/Private%20Genre", nil, a.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Genres/Private%20Genre", nil, a.viewerHeaders), http.StatusNotFound)
	images := responseArray(t, f.request(t, http.MethodGet, "/emby/Items/"+a.movieID+"/Images", nil, a.headers))
	if len(images) != 1 || images[0]["ImageType"] != "Primary" || images[0]["Width"] != float64(32) {
		t.Fatalf("application image metadata did not retain catalog authority: %#v", images)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+a.movieID+"/Images", nil, a.viewerHeaders), http.StatusNotFound)
	seasons, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+a.seriesID+"/Seasons", nil, a.headers))
	if total != 1 || len(seasons) != 1 || seasons[0]["Id"] != a.seasonID {
		t.Fatalf("application series detail lost catalog scope: %#v", seasons)
	}
	episodes, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+a.seriesID+"/Episodes?SeasonId="+a.seasonID, nil, a.headers))
	if total != 2 || len(episodes) != 2 || episodes[0]["Id"] != a.firstEpisodeID || episodes[1]["Id"] != a.nextEpisodeID {
		t.Fatalf("application season lookup lost catalog scope: %#v", episodes)
	}
	for _, episode := range episodes {
		if _, exists := episode["UserData"]; exists {
			t.Error("userless application episode invented user data")
		}
	}
	upNext, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/NextUp?SeriesId="+a.seriesID+"&UserId="+a.targetID, nil, a.headers))
	if total != 1 || len(upNext) != 1 || upNext[0]["Id"] != a.nextEpisodeID {
		t.Fatalf("application next-up did not use explicit target history: %#v", upNext)
	}
}

func TestHTTPApplicationKeyCatalogCredentialKindAndRevocationBoundaries(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	path := "/emby/Items?Recursive=true&IncludeItemTypes=Movie"
	keyCookie := &http.Cookie{Name: sessionCookie, Value: a.key.Token}
	for _, request := range []struct {
		path    string
		headers http.Header
		cookie  *http.Cookie
	}{
		{path: "/admin/v1/libraries", cookie: keyCookie},
		{path: path, cookie: keyCookie},
		{path: path, headers: http.Header{"X-Emby-Token": {a.adminCookie.Value}}},
	} {
		var cookies []*http.Cookie
		if request.cookie != nil {
			cookies = append(cookies, request.cookie)
		}
		expectStatus(t, f.request(t, http.MethodGet, request.path, nil, request.headers, cookies...), http.StatusUnauthorized)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", a.key.CredentialID); err != nil {
		t.Fatalf("revoke catalog key: %v", err)
	}
	for _, route := range []string{path, "/emby/Items/" + a.movieID + "/Images", "/emby/Genres"} {
		expectStatus(t, f.request(t, http.MethodGet, route, nil, a.headers, a.adminCookie), http.StatusUnauthorized)
	}
	_, total := responseItems(t, f.request(t, http.MethodGet, path, nil, a.viewerHeaders))
	if total != 1 {
		t.Error("revoking an application key changed ordinary user access")
	}
}
