package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
)

// Every test owns a random schema and only removes the schema it created.
func libraryIntegrationStore(t *testing.T, prober Prober, allowedCloseErrors ...error) (context.Context, *pgxpool.Pool, *Store, string, string) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "goby_library_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create isolated test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated integration database pool")
	}
	libraryIntegrationPoolCleanup(t, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	allowedRoot := t.TempDir()
	store, err := New(pool, prober, []string{allowedRoot})
	if err != nil {
		t.Fatalf("create library store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := store.Close(cleanupCtx); err != nil {
			for _, allowed := range allowedCloseErrors {
				if errors.Is(err, allowed) {
					return
				}
			}
			t.Errorf("close library store: %v", err)
		}
	})
	userID := "unrestricted-viewer"
	libraryIntegrationUser(t, ctx, pool, userID, false, true, nil)
	return ctx, pool, store, allowedRoot, userID
}

func libraryIntegrationPoolCleanup(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	t.Cleanup(func() {
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(15 * time.Second):
			t.Error("closing test database pool timed out; a store may have leaked an acquired connection")
		}
	})
}

func libraryIntegrationUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, disabled, enableAll bool, folders []string) {
	t.Helper()
	if folders == nil {
		folders = []string{}
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": enableAll, "EnabledFolders": folders})
	if err != nil {
		t.Fatalf("encode test user policy: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, has_password, is_disabled, policy)
		VALUES ($1, $1, $1, '', false, $2, $3)`, id, disabled, policy); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
}

type libraryFixtureProber struct {
	mu        sync.Mutex
	contents  []string
	active    int
	maxActive int
	block     bool
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
}

func (p *libraryFixtureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return media.Info{}, err
	}
	p.mu.Lock()
	p.contents = append(p.contents, string(data))
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()
	if p.block {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
			return libraryMediaFixture(data), nil
		case <-ctx.Done():
		}
		select {
		case p.cancelled <- struct{}{}:
		default:
		}
		return media.Info{}, ctx.Err()
	}
	return libraryMediaFixture(data), nil
}

func (p *libraryFixtureProber) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.contents...)
}

func (p *libraryFixtureProber) concurrency() (active, maximum int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active, p.maxActive
}

func libraryMediaFixture(data []byte) media.Info {
	if strings.HasPrefix(string(data), "audio:") {
		return media.Info{
			Container: "flac", DurationTicks: 183 * media.TicksPerSecond, Bitrate: 900_000, Size: int64(len(data)),
			Streams: []media.Stream{{Index: 0, Codec: "flac", CodecType: "audio", Channels: 2, SampleRate: 44_100, IsDefault: true}},
		}
	}
	return media.Info{
		Container: "matroska,webm", DurationTicks: 5_400 * media.TicksPerSecond, Bitrate: 4_192_000, Size: int64(len(data)),
		Streams: []media.Stream{
			{Index: 0, Codec: "h264", CodecType: "video", Width: 1920, Height: 1080, Profile: "High", Level: 41, PixelFormat: "yuv420p", AverageFrameRate: "24000/1001", IsDefault: true},
			{Index: 1, Codec: "aac", CodecType: "audio", Channels: 6, SampleRate: 48_000, Bitrate: 192_000, Language: "eng", IsDefault: true},
			{Index: 2, Codec: "subrip", CodecType: "subtitle", Language: "fra", IsForced: true, IsTextSubtitleStream: true},
		},
		Chapters: []media.Chapter{{StartTicks: 0, EndTicks: 60 * media.TicksPerSecond, Title: "Opening"}},
	}
}

func libraryIntegrationFile(t *testing.T, root, relativePath, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
	return path
}

func libraryIntegrationCreate(t *testing.T, ctx context.Context, store *Store, name, collectionType, path string) Library {
	t.Helper()
	library, err := store.CreateLibrary(ctx, name, collectionType, []string{path})
	if err != nil {
		t.Fatalf("create %s library: %v", collectionType, err)
	}
	return library
}

func libraryIntegrationWaitJob(t *testing.T, ctx context.Context, store *Store, id, wantStatus string) Job {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := store.GetJob(waitCtx, id)
		if err != nil {
			t.Fatalf("read scan job: %v", err)
		}
		switch job.Status {
		case "Completed", "Failed", "Cancelled", "Interrupted":
			if job.Status != wantStatus {
				t.Fatalf("scan status = %s, want %s; error = %q", job.Status, wantStatus, job.Error)
			}
			if job.FinishedAt == nil {
				t.Error("terminal scan job has no finish timestamp")
			}
			return job
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("waiting for job %s to reach %s: %v", id, wantStatus, waitCtx.Err())
		}
	}
}

func libraryIntegrationScan(t *testing.T, ctx context.Context, store *Store, libraryID, wantStatus string) Job {
	t.Helper()
	job, err := store.StartScan(ctx, libraryID)
	if err != nil {
		t.Fatalf("start library scan: %v", err)
	}
	return libraryIntegrationWaitJob(t, ctx, store, job.ID, wantStatus)
}

func libraryIntegrationQuery(t *testing.T, ctx context.Context, store *Store, query Query) ItemResult {
	t.Helper()
	result, err := store.QueryItems(ctx, query)
	if err != nil {
		t.Fatalf("query catalog items: %v", err)
	}
	return result
}

func libraryIntegrationItemByPath(t *testing.T, items []Item, path string) Item {
	t.Helper()
	for _, item := range items {
		if filepath.Clean(item.Path) == filepath.Clean(path) {
			return item
		}
	}
	t.Fatalf("no catalog item for path %q", path)
	return Item{}
}

func TestStoreMovieScanHierarchyStableIDsAndDeletion(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "movies/Adventure/First.mp4", "video:first")
	secondPath := libraryIntegrationFile(t, allowedRoot, "movies/Second.mkv", "video:second")
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Join(allowedRoot, "movies"))
	listed, err := store.ListLibraries(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != library.ID {
		t.Fatalf("library listing = %+v, error = %v", listed, err)
	}
	firstJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if firstJob.Scanned != 2 || firstJob.StartedAt == nil {
		t.Errorf("completed scan metadata = %+v", firstJob)
	}
	storedLibrary, err := store.GetLibrary(ctx, library.ID)
	if err != nil || storedLibrary.LastScanAt == nil || !reflect.DeepEqual(storedLibrary.Paths, library.Paths) {
		t.Errorf("scanned library = %+v, error = %v", storedLibrary, err)
	}
	root, err := store.GetItem(ctx, userID, library.ID)
	if err != nil || root.Type != "CollectionFolder" || !root.IsFolder {
		t.Fatalf("library root item = %+v, error = %v", root, err)
	}
	direct := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID})
	if direct.TotalRecordCount != 2 || len(direct.Items) != 2 {
		t.Fatalf("direct library children = %+v, want one directory and one movie", direct)
	}
	allQuery := Query{UserID: userID, ParentID: library.ID, Recursive: true}
	before := libraryIntegrationQuery(t, ctx, store, allQuery)
	if before.TotalRecordCount != 3 || len(before.Items) != 3 {
		t.Fatalf("recursive movie catalog = %+v, want three items", before)
	}
	folder := libraryIntegrationItemByPath(t, before.Items, filepath.Dir(firstPath))
	first := libraryIntegrationItemByPath(t, before.Items, firstPath)
	second := libraryIntegrationItemByPath(t, before.Items, secondPath)
	if folder.Type != "Folder" || !folder.IsFolder || folder.ParentID != library.ID || first.ParentID != folder.ID || second.ParentID != library.ID {
		t.Errorf("movie hierarchy mismatch: folder = %+v, first = %+v, second = %+v", folder, first, second)
	}
	if first.Type != "Movie" || first.IsFolder || first.Media == nil || !reflect.DeepEqual(*first.Media, libraryMediaFixture([]byte("video:first"))) {
		t.Errorf("movie metadata did not survive persistence: %+v", first)
	}
	probeCount := len(prober.calls())
	secondJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if secondJob.Added != 0 || secondJob.Updated != 0 {
		t.Errorf("unchanged rescan modified catalog records: %+v", secondJob)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("unchanged rescan repeated media probing: calls = %d, want %d", calls, probeCount)
	}
	after := libraryIntegrationQuery(t, ctx, store, allQuery)
	if after.TotalRecordCount != before.TotalRecordCount {
		t.Fatalf("rescan total = %d, want %d", after.TotalRecordCount, before.TotalRecordCount)
	}
	for _, item := range before.Items {
		rescanned := libraryIntegrationItemByPath(t, after.Items, item.Path)
		if rescanned.ID != item.ID || rescanned.ParentID != item.ParentID {
			t.Errorf("rescan changed identity or hierarchy for %q", item.Path)
		}
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Adventure/First.mp4", "video:first-revised")
	updatedJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if updatedJob.Added != 0 || updatedJob.Updated < 1 {
		t.Errorf("changed file was not updated in place: %+v", updatedJob)
	}
	updated, err := store.GetItem(ctx, userID, first.ID)
	if err != nil || updated.Path != firstPath || updated.ParentID != first.ParentID || updated.Media == nil || !reflect.DeepEqual(*updated.Media, libraryMediaFixture([]byte("video:first-revised"))) {
		t.Errorf("changed file lost its identity or metadata: item = %+v, error = %v", updated, err)
	}
	jobs, err := store.ListJobs(ctx)
	if err != nil || len(jobs) != 3 {
		t.Fatalf("scan history = %+v, error = %v", jobs, err)
	}
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("delete library: %v", err)
	}
	if _, err := store.GetLibrary(ctx, library.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get deleted library: got %v, want ErrNotFound", err)
	}
	if _, err := store.GetItem(ctx, userID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get deleted catalog item: got %v, want ErrNotFound", err)
	}
	if content, err := os.ReadFile(firstPath); err != nil || string(content) != "video:first-revised" {
		t.Errorf("deleting a library changed its media file: contents = %q, error = %v", content, err)
	}
}

func TestStoreTelevisionAndMusicHierarchy(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	episodePath := libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.mp4", "video:episode")
	audioPath := libraryIntegrationFile(t, allowedRoot, "music/Artist/Album/01 Title.flac", "audio:track")
	television := libraryIntegrationCreate(t, ctx, store, "Television", "tvshows", filepath.Join(allowedRoot, "television"))
	music := libraryIntegrationCreate(t, ctx, store, "Music", "music", filepath.Join(allowedRoot, "music"))
	libraryIntegrationScan(t, ctx, store, television.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, music.ID, "Completed")
	televisionItems := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: television.ID, Recursive: true})
	series := libraryIntegrationItemByPath(t, televisionItems.Items, filepath.Join(allowedRoot, "television", "Show"))
	season := libraryIntegrationItemByPath(t, televisionItems.Items, filepath.Dir(episodePath))
	episode := libraryIntegrationItemByPath(t, televisionItems.Items, episodePath)
	if series.Type != "Series" || series.ParentID != television.ID || !series.IsFolder || season.Type != "Season" || season.ParentID != series.ID || !season.IsFolder {
		t.Errorf("television folders: series = %+v, season = %+v", series, season)
	}
	if season.IndexNumber != 1 || episode.Type != "Episode" || episode.IsFolder || episode.ParentID != season.ID || episode.IndexNumber != 2 || episode.ParentIndexNumber != 1 {
		t.Errorf("episode hierarchy or indexes: season = %+v, episode = %+v", season, episode)
	}
	musicItems := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: music.ID, Recursive: true})
	album := libraryIntegrationItemByPath(t, musicItems.Items, filepath.Dir(audioPath))
	track := libraryIntegrationItemByPath(t, musicItems.Items, audioPath)
	if album.Type != "MusicAlbum" || !album.IsFolder || track.Type != "Audio" || track.IsFolder || track.ParentID != album.ID {
		t.Errorf("music hierarchy: album = %+v, track = %+v", album, track)
	}
	if track.Media == nil || !reflect.DeepEqual(*track.Media, libraryMediaFixture([]byte("audio:track"))) {
		t.Errorf("audio metadata did not survive persistence: %+v", track)
	}
}

func TestStoreAppliesLibraryPolicyBeforeCountingAndPagination(t *testing.T) {
	ctx, pool, store, allowedRoot, unrestrictedID := libraryIntegrationStore(t, &libraryFixtureProber{})
	hiddenPath := libraryIntegrationFile(t, allowedRoot, "hidden/A Hidden.mp4", "video:hidden")
	libraryIntegrationFile(t, allowedRoot, "visible/B Allowed.mp4", "video:allowed-b")
	lastPath := libraryIntegrationFile(t, allowedRoot, "visible/C Allowed.mp4", "video:allowed-c")
	hidden := libraryIntegrationCreate(t, ctx, store, "Hidden", "movies", filepath.Join(allowedRoot, "hidden"))
	visible := libraryIntegrationCreate(t, ctx, store, "Visible", "movies", filepath.Join(allowedRoot, "visible"))
	libraryIntegrationScan(t, ctx, store, hidden.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, visible.ID, "Completed")
	viewerID, emptyID, disabledID := "limited-viewer", "empty-viewer", "disabled-viewer"
	libraryIntegrationUser(t, ctx, pool, viewerID, false, false, []string{visible.ID})
	libraryIntegrationUser(t, ctx, pool, emptyID, false, false, nil)
	libraryIntegrationUser(t, ctx, pool, disabledID, true, true, nil)
	visibleLibraries, err := store.ListUserLibraries(ctx, viewerID)
	if err != nil || len(visibleLibraries) != 1 || visibleLibraries[0].ID != visible.ID {
		t.Fatalf("authorized libraries = %+v, error = %v", visibleLibraries, err)
	}
	query := Query{UserID: viewerID, Recursive: true, IncludeItemTypes: []string{"Movie"}, SortBy: "SortName", SortOrder: "Ascending", StartIndex: 1, Limit: 1}
	page := libraryIntegrationQuery(t, ctx, store, query)
	if page.TotalRecordCount != 2 || len(page.Items) != 1 || page.Items[0].Path != lastPath || page.Items[0].LibraryID != visible.ID {
		t.Fatalf("policy must precede counting and pagination: %+v", page)
	}
	if item, err := store.GetItem(ctx, viewerID, page.Items[0].ID); err != nil || item.ID != page.Items[0].ID {
		t.Errorf("authorized item detail = %+v, error = %v", item, err)
	}
	hiddenItems := libraryIntegrationQuery(t, ctx, store, Query{UserID: unrestrictedID, ParentID: hidden.ID, Recursive: true})
	hiddenItem := libraryIntegrationItemByPath(t, hiddenItems.Items, hiddenPath)
	for _, id := range []string{hidden.ID, hiddenItem.ID} {
		if _, err := store.GetItem(ctx, viewerID, id); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
			t.Errorf("cross-library detail %s: got %v, want access denial", id, err)
		}
	}
	byIDs := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewerID, Recursive: true, Ids: []string{hiddenItem.ID, page.Items[0].ID}})
	if byIDs.TotalRecordCount != 1 || len(byIDs.Items) != 1 || byIDs.Items[0].ID != page.Items[0].ID {
		t.Errorf("explicit item IDs bypassed library policy: %+v", byIDs)
	}
	emptyLibraries, err := store.ListUserLibraries(ctx, emptyID)
	if err != nil || len(emptyLibraries) != 0 {
		t.Errorf("empty folder policy libraries = %+v, error = %v", emptyLibraries, err)
	}
	emptyItems := libraryIntegrationQuery(t, ctx, store, Query{UserID: emptyID, Recursive: true})
	if emptyItems.TotalRecordCount != 0 || len(emptyItems.Items) != 0 {
		t.Errorf("empty folder policy exposed items: %+v", emptyItems)
	}
	if _, err := store.ListUserLibraries(ctx, disabledID); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled user's libraries: got %v, want ErrForbidden", err)
	}
	if _, err := store.QueryItems(ctx, Query{UserID: disabledID, Recursive: true}); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled user's items: got %v, want ErrForbidden", err)
	}
	if _, err := store.GetItem(ctx, disabledID, page.Items[0].ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled user's item detail: got %v, want ErrForbidden", err)
	}
}

func TestStoreCancelsActiveProbeAndRejectsDuplicateScan(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1)}
	ctx, _, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
	libraryIntegrationFile(t, allowedRoot, "movies/Blocked.mp4", "video:blocked")
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Join(allowedRoot, "movies"))
	job, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start blocking scan: %v", err)
	}
	select {
	case <-prober.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("scan never entered the blocking media prober")
	}
	if _, err := store.StartScan(ctx, library.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("duplicate active scan: got %v, want ErrBusy", err)
	}
	if err := store.DeleteLibrary(ctx, library.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("delete actively scanning library: got %v, want ErrBusy", err)
	}
	if err := store.CancelJob(ctx, job.ID); err != nil {
		t.Fatalf("cancel active scan: %v", err)
	}
	select {
	case <-prober.cancelled:
	case <-time.After(15 * time.Second):
		t.Fatal("cancelling the scan did not cancel the active prober context")
	}
	libraryIntegrationWaitJob(t, ctx, store, job.ID, "Cancelled")
	jobs, err := store.ListJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Errorf("duplicate scan created another persistent job: jobs = %+v, error = %v", jobs, err)
	}
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Errorf("cancelled scan left its library busy: %v", err)
	}
}

func TestStoreRejectsTraversalAndDoesNotProbeEscapingSymlinks(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	safePath := libraryIntegrationFile(t, allowedRoot, "movies/Safe.mp4", "video:safe")
	outsideRoot := t.TempDir()
	escapedPath := libraryIntegrationFile(t, outsideRoot, "Private.mp4", "video:outside-secret")
	separator := string(os.PathSeparator)
	for _, test := range []struct {
		path string
		want error
	}{
		{path: outsideRoot, want: ErrForbidden},
		{path: allowedRoot + separator + ".." + separator + filepath.Base(outsideRoot), want: ErrInvalidInput},
		{path: filepath.Dir(safePath) + separator + ".." + separator + "movies", want: ErrInvalidInput},
	} {
		if _, err := store.CreateLibrary(ctx, "Rejected", "movies", []string{test.path}); !errors.Is(err, test.want) {
			t.Errorf("register invalid media path %q: got %v, want %v", test.path, err, test.want)
		}
	}
	t.Run("SymlinkEscape", func(t *testing.T) {
		linkPath := filepath.Join(allowedRoot, "movies", "Escape.mp4")
		if err := os.Symlink(escapedPath, linkPath); err != nil {
			t.Skipf("filesystem cannot create file symlinks: %v", err)
		}
		if err := os.Symlink(outsideRoot, filepath.Join(allowedRoot, "movies", "EscapingDirectory")); err != nil {
			t.Skipf("filesystem cannot create directory symlinks: %v", err)
		}
		library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(safePath))
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
		if items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].Path != safePath {
			t.Errorf("escaping symlink entered the catalog: %+v", items)
		}
		if calls := prober.calls(); len(calls) != 1 || calls[0] != "video:safe" {
			t.Errorf("prober received media beyond the approved root: %q", calls)
		}
	})
}

func TestStoreUnavailableRootPreservesCatalogAndRecovers(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Retained.mp4", "video:retained")
	rootPath := filepath.Dir(mediaPath)
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", rootPath)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	query := Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}
	before := libraryIntegrationQuery(t, ctx, store, query)
	if len(before.Items) != 1 {
		t.Fatalf("initial movie count = %d, want 1", len(before.Items))
	}
	missingPath := rootPath + "-temporarily-unavailable"
	if err := os.Rename(rootPath, missingPath); err != nil {
		t.Fatalf("temporarily remove library root: %v", err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(missingPath); err == nil {
			if err := os.Rename(missingPath, rootPath); err != nil {
				t.Errorf("restore media fixture root: %v", err)
			}
		}
	})
	failed := libraryIntegrationScan(t, ctx, store, library.ID, "Failed")
	if failed.Error == "" {
		t.Error("unavailable root scan did not record a failure reason")
	}
	retained := libraryIntegrationQuery(t, ctx, store, query)
	if retained.TotalRecordCount != 1 || len(retained.Items) != 1 || retained.Items[0].ID != before.Items[0].ID {
		t.Fatalf("unavailable root removed existing catalog records: %+v", retained)
	}
	if err := os.Rename(missingPath, rootPath); err != nil {
		t.Fatalf("restore library root: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	recovered := libraryIntegrationQuery(t, ctx, store, query)
	if recovered.TotalRecordCount != 1 || len(recovered.Items) != 1 || recovered.Items[0].ID != before.Items[0].ID {
		t.Errorf("root recovery lost stable catalog identity: %+v", recovered)
	}
}

func TestStoreLinuxRenameAcrossLibraryRootsPreservesIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable filesystem identity is supported on Linux")
	}
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "first/Original.mp4", "video:renamed")
	secondRoot := filepath.Join(allowedRoot, "second")
	if err := os.MkdirAll(secondRoot, 0700); err != nil {
		t.Fatalf("create destination media root: %v", err)
	}
	library, err := store.CreateLibrary(ctx, "Multi-root movies", "movies", []string{filepath.Dir(firstPath), secondRoot})
	if err != nil {
		t.Fatalf("create library with two roots: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	query := Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}
	before := libraryIntegrationQuery(t, ctx, store, query)
	if len(before.Items) != 1 {
		t.Fatalf("initial movie count = %d, want 1", len(before.Items))
	}
	original := before.Items[0]
	var originalIdentity, originalRootID string
	if err := pool.QueryRow(ctx, "SELECT file_identity, root_id FROM items WHERE id = $1", original.ID).Scan(&originalIdentity, &originalRootID); err != nil {
		t.Fatalf("read original filesystem identity: %v", err)
	}
	if originalIdentity == "" {
		t.Fatal("Linux scan did not persist a filesystem identity")
	}
	probeCount := len(prober.calls())
	destination := filepath.Join(secondRoot, "Renamed.mp4")
	if err := os.Rename(firstPath, destination); err != nil {
		t.Fatalf("move fixture between library roots: %v", err)
	}
	renamedJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if renamedJob.Added != 0 || renamedJob.Updated != 1 {
		t.Errorf("rename did not update the existing item: %+v", renamedJob)
	}
	after := libraryIntegrationQuery(t, ctx, store, query)
	if after.TotalRecordCount != 1 || len(after.Items) != 1 || after.Items[0].ID != original.ID || after.Items[0].Path != destination {
		t.Fatalf("cross-root rename changed catalog identity: %+v", after)
	}
	var renamedIdentity, renamedRootID string
	if err := pool.QueryRow(ctx, "SELECT file_identity, root_id FROM items WHERE id = $1", original.ID).Scan(&renamedIdentity, &renamedRootID); err != nil {
		t.Fatalf("read renamed filesystem identity: %v", err)
	}
	if renamedIdentity != originalIdentity || renamedRootID == originalRootID {
		t.Errorf("rename failed to retain filesystem identity or change its root: identity = %q, root = %q", renamedIdentity, renamedRootID)
	}
	unchanged := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if unchanged.Added != 0 || unchanged.Updated != 0 {
		t.Errorf("unchanged renamed item was modified again: %+v", unchanged)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("rename or unchanged rescan repeated probing: calls = %d, want %d", calls, probeCount)
	}
}

func TestStoreBoundsWorkersAndCloseWaitsForCancellation(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 3), cancelled: make(chan struct{}, 3)}
	ctx, _, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
	jobs := make([]Job, 0, 3)
	for _, name := range []string{"first", "second", "third"} {
		path := libraryIntegrationFile(t, allowedRoot, name+"/Blocked.mp4", "video:"+name)
		library := libraryIntegrationCreate(t, ctx, store, name, "movies", filepath.Dir(path))
		job, err := store.StartScan(ctx, library.ID)
		if err != nil {
			t.Fatalf("queue %s scan: %v", name, err)
		}
		jobs = append(jobs, job)
		if len(jobs) <= 2 {
			select {
			case <-prober.entered:
			case <-time.After(15 * time.Second):
				t.Fatalf("%s scan did not enter a worker", name)
			}
		}
	}
	select {
	case <-prober.entered:
		t.Fatal("more than two workers entered the blocking media prober")
	case <-time.After(150 * time.Millisecond):
	}
	queued, err := store.GetJob(ctx, jobs[2].ID)
	if err != nil || queued.Status != "Queued" || queued.StartedAt != nil {
		t.Fatalf("third job should wait for a worker: job = %+v, error = %v", queued, err)
	}
	if calls := len(prober.calls()); calls != 2 {
		t.Fatalf("active media probe count = %d, want 2", calls)
	}
	if err := store.CancelJob(ctx, jobs[0].ID); err != nil {
		t.Fatalf("cancel first worker: %v", err)
	}
	libraryIntegrationWaitJob(t, ctx, store, jobs[0].ID, "Cancelled")
	select {
	case <-prober.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("queued third job did not start after a worker became available")
	}
	if calls := len(prober.calls()); calls != 3 {
		t.Errorf("probe count after releasing a worker = %d, want 3", calls)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("close store with active probes: %v", err)
	}
	if active, maximum := prober.concurrency(); active != 0 || maximum != 2 {
		t.Errorf("probe concurrency after Close: active = %d, maximum = %d; want 0 and 2", active, maximum)
	}
	if cancellations := len(prober.cancelled); cancellations != 3 {
		t.Errorf("cancelled media probes = %d, want 3", cancellations)
	}
	for _, started := range jobs {
		job, err := store.GetJob(ctx, started.ID)
		if err != nil || job.Status != "Cancelled" || job.FinishedAt == nil {
			t.Errorf("Close returned before a scan became terminal: job = %+v, error = %v", job, err)
		}
	}
	if _, err := store.StartScan(ctx, jobs[0].LibraryID); !errors.Is(err, ErrUnavailable) {
		t.Errorf("closed store accepted a new scan: got %v, want ErrUnavailable", err)
	}
}

func TestStoreRecoversInterruptedJobsWithUnavailableConfiguredRoot(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraries := make([]Library, 0, 2)
	for _, name := range []string{"queued", "running"} {
		path := libraryIntegrationFile(t, allowedRoot, name+"/Unscanned.mp4", "video:"+name)
		libraries = append(libraries, libraryIntegrationCreate(t, ctx, store, name, "movies", filepath.Dir(path)))
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer closeCancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("stop original store before recovery fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, started_at)
		VALUES ('recover-queued', $1, 'Queued', NULL), ('recover-running', $2, 'Running', now())`, libraries[0].ID, libraries[1].ID); err != nil {
		t.Fatalf("insert interrupted scan fixtures: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, finished_at)
		VALUES ('keep-completed', $1, 'Completed', now())`, libraries[0].ID); err != nil {
		t.Fatalf("insert terminal scan fixture: %v", err)
	}
	missingRoot := filepath.Join(allowedRoot, "unavailable-at-startup")
	reopened, err := New(pool, &libraryFixtureProber{}, []string{missingRoot})
	if err != nil {
		t.Fatalf("restart with unavailable configured media root: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close recovered store: %v", err)
		}
	})
	for _, id := range []string{"recover-queued", "recover-running"} {
		job, err := reopened.GetJob(ctx, id)
		if err != nil || job.Status != "Interrupted" || job.FinishedAt == nil || job.Error == "" {
			t.Errorf("unfinished scan was not recovered: job = %+v, error = %v", job, err)
		}
	}
	completed, err := reopened.GetJob(ctx, "keep-completed")
	if err != nil || completed.Status != "Completed" || completed.Error != "" {
		t.Errorf("restart modified a terminal scan: job = %+v, error = %v", completed, err)
	}
}

func TestStoreOwnershipLockProtectsRunningJobsAndTransfersAfterClose(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1)}
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Blocked.mp4", "video:owned-job")
	library := libraryIntegrationCreate(t, ctx, store, "Owned movies", "movies", filepath.Dir(path))
	started, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start owned scan: %v", err)
	}
	select {
	case <-prober.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("owned scan did not enter the blocking prober")
	}
	running, err := store.GetJob(ctx, started.ID)
	if err != nil || running.Status != "Running" || running.StartedAt == nil || running.FinishedAt != nil {
		t.Fatalf("owned scan was not running: job = %+v, error = %v", running, err)
	}
	independentPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("create independent pool for the owned schema")
	}
	libraryIntegrationPoolCleanup(t, independentPool)
	for _, contenderPool := range []*pgxpool.Pool{pool, independentPool} {
		contender, err := New(contenderPool, &libraryFixtureProber{}, []string{allowedRoot})
		if contender != nil {
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cleanupCancel()
				if err := contender.Close(cleanupCtx); err != nil {
					t.Errorf("close unexpected competing store: %v", err)
				}
			})
		}
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("second store in the same schema: got %v, want ErrBusy", err)
		}
		protected, err := store.GetJob(ctx, started.ID)
		if err != nil || protected.Status != "Running" || protected.FinishedAt != nil || protected.StartedAt == nil || !protected.StartedAt.Equal(*running.StartedAt) {
			t.Fatalf("rejected store changed the active owner's job: job = %+v, error = %v", protected, err)
		}
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer closeCancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("close the original owning store: %v", err)
	}
	closedJob, err := store.GetJob(ctx, started.ID)
	if err != nil || closedJob.Status != "Cancelled" || closedJob.FinishedAt == nil {
		t.Fatalf("original store did not finish its scan before releasing ownership: job = %+v, error = %v", closedJob, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, started_at)
		VALUES ('orphaned-owner-job', $1, 'Running', now())`, library.ID); err != nil {
		t.Fatalf("insert abandoned scan after ownership release: %v", err)
	}
	successor, err := New(independentPool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("acquire schema ownership after the original store closed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := successor.Close(cleanupCtx); err != nil {
			t.Errorf("close successor store: %v", err)
		}
	})
	recovered, err := successor.GetJob(ctx, "orphaned-owner-job")
	if err != nil || recovered.Status != "Interrupted" || recovered.FinishedAt == nil || recovered.Error == "" {
		t.Errorf("new owner did not recover the abandoned scan: job = %+v, error = %v", recovered, err)
	}
}

func TestStoreInitializationFailureReleasesOwnershipLock(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer closeCancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("close original store before initialization failure fixture: %v", err)
	}
	independentPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("create independent pool for initialization recovery")
	}
	libraryIntegrationPoolCleanup(t, independentPool)
	if _, err := pool.Exec(ctx, "ALTER TABLE scan_jobs RENAME TO scan_jobs_initialization_fixture"); err != nil {
		t.Fatalf("temporarily rename owned scan table: %v", err)
	}
	tableRenamed := true
	t.Cleanup(func() {
		if !tableRenamed {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "ALTER TABLE scan_jobs_initialization_fixture RENAME TO scan_jobs"); err != nil {
			t.Errorf("restore owned scan table: %v", err)
		}
	})
	unexpected, err := New(pool, &libraryFixtureProber{}, []string{allowedRoot})
	if unexpected != nil {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := unexpected.Close(cleanupCtx); err != nil {
				t.Errorf("close unexpected initialized store: %v", err)
			}
		})
	}
	var pgError *pgconn.PgError
	if err == nil || !strings.Contains(err.Error(), "recover interrupted scans") || !errors.As(err, &pgError) || pgError.Code != "42P01" {
		t.Fatalf("initialization with missing scan table: got %v, want a recovery error", err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE scan_jobs_initialization_fixture RENAME TO scan_jobs"); err != nil {
		t.Fatalf("restore scan table before retrying initialization: %v", err)
	}
	tableRenamed = false
	// A separate connection pool cannot reuse a leaked session-level lock.
	recovered, err := New(independentPool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("initialization failure retained schema ownership: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := recovered.Close(cleanupCtx); err != nil {
			t.Errorf("close store after successful initialization retry: %v", err)
		}
	})
}

func TestStoreOwnershipConnectionLossStopsOldWriterAfterTakeover(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 2), cancelled: make(chan struct{}, 2), release: make(chan struct{}, 1)}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober, ErrUnavailable)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Interrupted.mp4", "video:ownership-loss")
	library := libraryIntegrationCreate(t, ctx, store, "Recoverable movies", "movies", filepath.Dir(path))
	started, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start scan before terminating ownership session: %v", err)
	}
	otherPath := libraryIntegrationFile(t, allowedRoot, "other/Interrupted.mp4", "video:ownership-loss-other")
	otherLibrary := libraryIntegrationCreate(t, ctx, store, "Other recoverable movies", "movies", filepath.Dir(otherPath))
	otherStarted, err := store.StartScan(ctx, otherLibrary.ID)
	if err != nil {
		t.Fatalf("start second scan before terminating ownership session: %v", err)
	}
	oldJobs := []Job{started, otherStarted}
	for range oldJobs {
		select {
		case <-prober.entered:
		case <-time.After(15 * time.Second):
			t.Fatal("both scans did not enter blocking probes before ownership loss")
		}
	}
	for _, job := range oldJobs {
		running, err := store.GetJob(ctx, job.ID)
		if err != nil || running.Status != "Running" {
			t.Fatalf("scan before ownership loss: job = %+v, error = %v", running, err)
		}
	}
	backendPID := store.ownership.conn.Conn().PgConn().PID()
	var terminated bool
	if err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", int32(backendPID)).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate owned test database session: terminated = %v, error = %v", terminated, err)
	}
	successorPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("create pool for ownership takeover")
	}
	libraryIntegrationPoolCleanup(t, successorPool)
	takeoverCtx, takeoverCancel := context.WithTimeout(ctx, 15*time.Second)
	defer takeoverCancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var successor *Store
	for {
		candidate, err := New(successorPool, &libraryFixtureProber{}, []string{allowedRoot})
		if candidate != nil {
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cleanupCancel()
				if err := candidate.Close(cleanupCtx); err != nil {
					t.Errorf("close ownership takeover store: %v", err)
				}
			})
		}
		if err == nil {
			successor = candidate
			break
		}
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("take over terminated ownership session: %v", err)
		}
		select {
		case <-ticker.C:
		case <-takeoverCtx.Done():
			t.Fatalf("ownership lock was not released after backend termination: %v", takeoverCtx.Err())
		}
	}
	interruptedJobs := make([]Job, 0, len(oldJobs))
	for _, job := range oldJobs {
		interrupted, err := successor.GetJob(ctx, job.ID)
		if err != nil || interrupted.Status != "Interrupted" || interrupted.FinishedAt == nil {
			t.Fatalf("successor did not recover an old running scan: job = %+v, error = %v", interrupted, err)
		}
		interruptedJobs = append(interruptedJobs, interrupted)
	}
	// Let one old worker attempt persistence; that write must cancel its peer.
	prober.release <- struct{}{}
	select {
	case <-prober.cancelled:
	case <-time.After(15 * time.Second):
		t.Fatal("old worker persistence did not detect ownership loss and cancel its peer")
	}
	if store.Available() {
		t.Error("store remained available after losing its ownership session")
	}
	if _, err := store.StartScan(ctx, library.ID); !errors.Is(err, ErrUnavailable) {
		t.Errorf("old owner accepted a write after losing its database session: got %v, want ErrUnavailable", err)
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer closeCancel()
	if err := store.Close(closeCtx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("close store after ownership loss: got %v, want ErrUnavailable", err)
	}
	for _, interrupted := range interruptedJobs {
		retained, err := successor.GetJob(ctx, interrupted.ID)
		if err != nil || !reflect.DeepEqual(retained, interrupted) {
			t.Errorf("old worker changed the successor's interrupted job: job = %+v, error = %v", retained, err)
		}
	}
	beforeScan := libraryIntegrationQuery(t, ctx, successor, Query{UserID: userID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if beforeScan.TotalRecordCount != 0 || len(beforeScan.Items) != 0 {
		t.Errorf("old worker persisted media after ownership takeover: %+v", beforeScan)
	}
	completed := libraryIntegrationScan(t, ctx, successor, library.ID, "Completed")
	if completed.Scanned != 1 {
		t.Errorf("successor scan did not inspect the media fixture: %+v", completed)
	}
	items := libraryIntegrationQuery(t, ctx, successor, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].Path != path {
		t.Errorf("successor did not persist the recovered media item: %+v", items)
	}
}

func TestStoreRejectsSearchPathWithAnEmptySchemaPrefix(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1)}
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Blocked.mp4", "video:search-path")
	library := libraryIntegrationCreate(t, ctx, store, "Schema-scoped movies", "movies", filepath.Dir(path))
	started, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start scan before invalid search path attempt: %v", err)
	}
	select {
	case <-prober.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("scan did not enter the blocking prober before invalid search path attempt")
	}
	before, err := store.GetJob(ctx, started.ID)
	if err != nil || before.Status != "Running" {
		t.Fatalf("scan before invalid search path attempt: job = %+v, error = %v", before, err)
	}
	var actualSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&actualSchema); err != nil {
		t.Fatalf("read owned test schema: %v", err)
	}
	prefixSchema := actualSchema + "_prefix"
	quotedPrefix := pgx.Identifier{prefixSchema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedPrefix); err != nil {
		t.Fatalf("create owned empty prefix schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA "+quotedPrefix+" CASCADE"); err != nil {
			t.Errorf("remove owned empty prefix schema: %v", err)
		}
	})
	config := pool.Config()
	config.ConnConfig.RuntimeParams["search_path"] = quotedPrefix + "," + pgx.Identifier{actualSchema}.Sanitize()
	prefixedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create pool with an empty schema prefix")
	}
	libraryIntegrationPoolCleanup(t, prefixedPool)
	unexpected, err := New(prefixedPool, &libraryFixtureProber{}, []string{allowedRoot})
	if unexpected != nil {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := unexpected.Close(cleanupCtx); err != nil {
				t.Errorf("close unexpected store with invalid search path: %v", err)
			}
		})
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("initialize store with prefix-schema fallback: got %v, want ErrInvalidInput", err)
	}
	jobs, err := store.ListJobs(ctx)
	if err != nil || len(jobs) != 1 || !reflect.DeepEqual(jobs[0], before) {
		t.Errorf("invalid search path attempt changed the actual catalog's jobs: jobs = %+v, error = %v", jobs, err)
	}
}

func TestStoreOwnedTransactionSurvivesCallerCancellation(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	callerCtx, callerCancel := context.WithCancel(ctx)
	defer callerCancel()
	tx, err := store.beginOwnedTx(callerCtx)
	if err != nil {
		t.Fatalf("begin owned transaction before caller cancellation: %v", err)
	}
	defer rollback(tx)
	callerCancel()
	if _, err := tx.Exec(callerCtx, `INSERT INTO server_settings (key, value)
		VALUES ('owned-transaction-caller-cancellation', 'committed')`); err != nil {
		t.Fatalf("execute owned transaction after caller cancellation: %v", err)
	}
	if err := tx.Commit(callerCtx); err != nil {
		t.Fatalf("commit owned transaction after caller cancellation: %v", err)
	}
	var value string
	if err := pool.QueryRow(ctx, "SELECT value FROM server_settings WHERE key = 'owned-transaction-caller-cancellation'").Scan(&value); err != nil || value != "committed" {
		t.Fatalf("owned transaction did not persist after caller cancellation: value = %q, error = %v", value, err)
	}
	if !store.Available() {
		t.Error("caller cancellation made the library store unavailable")
	}
	if err := store.CheckOwnership(ctx); err != nil {
		t.Fatalf("caller cancellation damaged the ownership connection: %v", err)
	}
	path := libraryIntegrationFile(t, allowedRoot, "movies/AfterCancellation.mp4", "video:after-caller-cancellation")
	library := libraryIntegrationCreate(t, ctx, store, "Movies after caller cancellation", "movies", filepath.Dir(path))
	completed := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if completed.Scanned != 1 || completed.Added != 1 {
		t.Errorf("scan after caller cancellation did not persist its media: %+v", completed)
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].Path != path {
		t.Errorf("catalog after caller cancellation = %+v", items)
	}
}
