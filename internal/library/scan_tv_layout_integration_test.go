package library

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestStoreTVScanFindsShowsUnderGroupingFolders(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	root := filepath.Join(allowedRoot, "television")
	shows := []struct {
		episode string
		number  int
	}{
		{"All/TV/First Show/Season 1/Disc 1/First.Show.S01E02.mp4", 2},
		{"All/Second Show/Season 1/Second.Show.S01E03.mp4", 3},
		{"All/@eaDir Archive/Season 1/Archive.S01E04.mp4", 4},
	}
	showPaths := make(map[string]bool)
	for _, show := range shows {
		episode := libraryIntegrationFile(t, root, show.episode, "video:"+show.episode)
		season := filepath.Dir(episode)
		if filepath.Base(season) == "Disc 1" {
			showPaths[season] = true
			season = filepath.Dir(season)
		}
		showPaths[episode], showPaths[season], showPaths[filepath.Dir(season)] = true, true, true
	}
	ignoredEpisodes := []string{
		"#recycle/Deleted Show/Season 1/Deleted.Show.S01E01.mp4",
		"All/@eaDir/Indexed Show/Season 1/Indexed.Show.S01E01.mp4",
		"All/TV/$RECYCLE.BIN/Deleted Show/Season 1/Deleted.Show.S01E01.mp4",
	}
	for _, episode := range ignoredEpisodes {
		libraryIntegrationFile(t, root, episode, "video:"+episode)
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Nested television", "tvshows", root)

	scanLayout := func() map[string]string {
		t.Helper()
		libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
		calls := prober.calls()
		for _, episode := range ignoredEpisodes {
			if slices.Contains(calls, "video:"+episode) {
				t.Errorf("probed an episode under an ignored system directory: %s", episode)
			}
		}
		items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true}).Items
		// Ignored paths must not be stored even if their rows would be hidden from browsing.
		var stored int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE library_id=$1 AND id<>$1`, collection.ID).Scan(&stored); err != nil {
			t.Fatalf("count persisted TV catalog rows: %v", err)
		}
		if stored != len(items) {
			t.Errorf("TV catalog has %d persisted rows but %d browsable items", stored, len(items))
		}
		ids := make(map[string]string, len(items))
		for _, item := range items {
			path := filepath.Clean(item.Path)
			if previous, exists := ids[path]; exists {
				t.Errorf("duplicate TV catalog path %q: IDs %q and %q", path, previous, item.ID)
			}
			ids[path] = item.ID
			switch path {
			case filepath.Join(root, "All"), filepath.Join(root, "All", "TV"):
				if item.Type != "Folder" || !item.IsFolder {
					t.Errorf("grouping directory %q is %s (IsFolder=%t), want a browsing folder or no item", path, item.Type, item.IsFolder)
				}
			default:
				if !showPaths[path] {
					t.Errorf("unexpected TV catalog item %q (%s)", item.Path, item.Type)
				}
			}
		}
		for _, show := range shows {
			assertScannedPhysicalTVHierarchy(t, items, filepath.Join(root, filepath.FromSlash(show.episode)), 1, show.number)
		}
		return ids
	}

	before := scanLayout()
	calls := prober.calls()
	for _, show := range shows {
		if !slices.Contains(calls, "video:"+show.episode) {
			t.Errorf("did not probe legitimate episode %s", show.episode)
		}
	}
	if after := scanLayout(); !maps.Equal(before, after) {
		t.Errorf("unchanged rescan changed catalog IDs: before=%v, after=%v", before, after)
	}
}

func TestStoreTVScanRecognizesShowNamedAllAndPhysicalSeasonZero(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "television")
	episode := libraryIntegrationFile(t, root, "All/Season 0/All.S00E02.mp4", "video:all-special")
	collection := libraryIntegrationCreate(t, ctx, store, "Television specials", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true})
	if result.TotalRecordCount != 3 {
		t.Errorf("show named All has %d catalog items, want one series, one season, and one episode", result.TotalRecordCount)
	}
	assertScannedPhysicalTVHierarchy(t, result.Items, episode, 0, 2)
}

func TestStoreTVScanSeparatesFlatShowsAndKeepsWrappedSpecials(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "television")
	first := libraryIntegrationFile(t, root, "Flat/First.S01E01.mp4", "video:flat-first")
	second := libraryIntegrationFile(t, root, "Flat/Second.S01E01.mp4", "video:flat-second")
	special := libraryIntegrationFile(t, root, "Grouped/Third/Specials/Disc 1/Third.S00E02.mp4", "video:special")
	libraryIntegrationFile(t, root, "Grouped/Third/Season 1/Third.S01E01.mp4", "video:third")
	collection := libraryIntegrationCreate(t, ctx, store, "Mixed television layouts", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")

	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true}).Items
	for path, name := range map[string]string{first: "First", second: "Second", special: "Third"} {
		episode := libraryIntegrationItemByPath(t, items, path)
		if episode.Series == nil || episode.Series.Name != name || episode.Season == nil {
			t.Errorf("%q is not an episode of %s under its Season: %+v", path, name, episode)
		}
		if path == special {
			season := libraryIntegrationItemByPath(t, items, filepath.Join(root, "Grouped", "Third", "Specials"))
			if season.IndexNumber != 0 || episode.ParentID != season.ID || episode.ParentIndexNumber != 0 {
				t.Errorf("the disc folder separated a special from Season 0: season=%+v episode=%+v", season, episode)
			}
		}
	}
	if folder := libraryIntegrationItemByPath(t, items, filepath.Join(root, "Flat")); folder.Type != "Folder" {
		t.Errorf("the directory containing several flat shows was indexed as %s", folder.Type)
	}
}

func TestStoreTVScanKeepsSyntheticShowWithSystemDirectoryName(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "television")
	path := libraryIntegrationFile(t, root, "#recycle.S01E01.mp4", "video:flat-episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Flat television", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")

	result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true})
	if result.TotalRecordCount != 3 {
		t.Errorf("catalog has %d items, want the virtual show, season, and physical episode", result.TotalRecordCount)
	}
	episode := libraryIntegrationItemByPath(t, result.Items, path)
	if episode.Series == nil || episode.Series.Name != "#recycle" || episode.Season == nil {
		t.Fatalf("legitimate virtual ancestors were hidden: %+v", episode)
	}
	if show, err := store.GetItem(ctx, userID, episode.Series.ID); err != nil || show.Type != "Series" || show.Path != "" {
		t.Errorf("direct read lost the virtual show: %+v, %v", show, err)
	}
}

func TestStoreTVScanKeepsRootAndNamedFlatSeriesSeparate(t *testing.T) {
	ctx, _, store, approved, user := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(approved, "Television")
	physical := libraryIntegrationFile(t, root, "Season 1/Home.S01E01.mp4", "video:home")
	libraryIntegrationFile(t, root, "tvshow.nfo", `<tvshow><title>Home Series</title></tvshow>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Independent shows", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, user, collection.ID, physical)
	if before.Series == nil || before.Season == nil {
		t.Fatal("the original root series and physical season were not published")
	}
	flat := libraryIntegrationFile(t, root, "Flat/Root.S01E01.mp4", "video:root")
	libraryIntegrationFile(t, root, "Flat/Other.S01E01.mp4", "video:other")
	var flatSeries, flatSeason string
	for range 2 {
		libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
		home := nfoCatalogItem(t, ctx, store, user, collection.ID, physical)
		named := nfoCatalogItem(t, ctx, store, user, collection.ID, flat)
		if home.Series == nil || home.Season == nil || named.Series == nil || named.Season == nil {
			t.Fatal("the fixture did not publish both series and seasons")
		}
		if home.ID != before.ID || home.Series.ID != before.Series.ID || home.Season.ID != before.Season.ID || home.Series.Name != "Home Series" {
			t.Fatal("adding flat shows changed the accepted physical root hierarchy")
		}
		if home.Series.ID == named.Series.ID || home.Season.ID == named.Season.ID || named.Series.Name != "Root" {
			t.Fatal("the physical root and the show named Root share catalog data")
		}
		if flatSeries != "" && (flatSeries != named.Series.ID || flatSeason != named.Season.ID) {
			t.Fatal("the unchanged flat show acquired a different series or season ID")
		}
		flatSeries, flatSeason = named.Series.ID, named.Season.ID
	}
}

func TestStoreTVScanKeepsUnnumberedDiscEpisodeInPhysicalSeason(t *testing.T) {
	ctx, _, store, approved, user := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(approved, "television")
	path := libraryIntegrationFile(t, root, "All/Show/Specials/Disc 1/Untitled.mp4", "video:unnumbered-special")
	libraryIntegrationFile(t, root, "All/Show/Specials/Disc 1/Untitled.nfo", `<episodedetails><title>A special</title><season>0</season><episode>7</episode></episodedetails>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Unnumbered specials", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: collection.ID, Recursive: true}).Items
	assertScannedPhysicalTVHierarchy(t, items, path, 0, 7)
	episode := libraryIntegrationItemByPath(t, items, path)
	if episode.Series == nil || episode.Season == nil || episode.Name != "A special" {
		t.Errorf("the unnumbered disc episode lost its ancestors or NFO title: %+v", episode)
	}
}

func TestStoreTVScanKeepsRootSeasonWithOnlyDiscEpisodes(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "Root Show")
	path := libraryIntegrationFile(t, root, "Season 1/Disc 1/Root.Show.S01E02.mp4", "video:disc-episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Television root show", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")

	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true}).Items
	season := libraryIntegrationItemByPath(t, items, filepath.Join(root, "Season 1"))
	episode := libraryIntegrationItemByPath(t, items, path)
	if season.Type != "Season" || season.IndexNumber != 1 || episode.Type != "Episode" || episode.ParentID != season.ID ||
		episode.Series == nil || episode.Series.Name != "Root Show" || episode.Series.ID != season.ParentID ||
		episode.ParentIndexNumber != 1 || episode.IndexNumber != 2 {
		t.Errorf("disc fixture lost its physical season or root show: season=%+v episode=%+v", season, episode)
	}
}

func TestStoreTVScanKeepsShowWhenLocalMetadataIsDisabled(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "television")
	nfo := libraryIntegrationFile(t, root, "Group/Show/tvshow.nfo", `<tvshow><title>Local Title</title></tvshow>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Metadata-only show", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(nfo))
	if before.Type != "Series" || before.Name != "Local Title" {
		t.Fatalf("initial scan did not import the metadata-only show: %+v", before)
	}
	detail, err := store.GetLibraryEditing(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := store.updateLibrary(ctx, nil, collection.ID, LibraryUpdate{
		Revision: detail.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableLocalMetadata: &disabled},
	}); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, root, "Group/Show/tvshow.nfo", `<tvshow><title>Do Not Import</title></tvshow>`)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(nfo))
	if after.ID != before.ID || after.Type != "Series" || after.Name != before.Name {
		t.Errorf("disabling metadata import changed the accepted show: before=%+v after=%+v", before, after)
	}
}

func TestStoreTVScanRepairsExistingGroupingWithoutLosingUserData(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(allowedRoot, "television")
	episodePath := libraryIntegrationFile(t, root, "All/Show/Season 1/Show.S01E02.mp4", "video:kept")
	recycledPath := libraryIntegrationFile(t, root, "#recycle/Removed/Season 1/Removed.S01E01.mp4", "video:recycled")
	collection := libraryIntegrationCreate(t, ctx, store, "Existing television", "tvshows", root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	group := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Join(root, "All"))
	series := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Join(root, "All", "Show"))
	season := nfoCatalogItem(t, ctx, store, userID, collection.ID, filepath.Dir(episodePath))
	episode := nfoCatalogItem(t, ctx, store, userID, collection.ID, episodePath)
	position, favorite := int64(42), true
	if _, err := store.UpdateUserDataFor(ctx, Subject{UserID: userID}, episode.ID, UserDataPatch{
		PlaybackPositionTicks: &position, IsFavorite: &favorite,
	}); err != nil {
		t.Fatal(err)
	}

	// Recreate the old classification without changing the registered root or
	// any legitimate file. Seed historical recycle rows directly, because the
	// repaired walker must never enter that directory.
	const recycleID, recycledEpisodeID = "old-recycle", "old-recycled-episode"
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`UPDATE items SET type='Series' WHERE id=$1`, group.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE items SET type='Folder' WHERE id=$1`, series.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE items SET parent_id=$1 WHERE id=$2`, group.ID, season.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO items (id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path, is_folder)
			SELECT $2,$1,r.id,$1,'#recycle','#recycle','Series',$4,'#recycle',true FROM library_roots r WHERE r.library_id=$1
			UNION ALL SELECT $3,$1,r.id,$2,'Removed','removed','Episode',$5,'#recycle/Removed/Season 1/Removed.S01E01.mp4',false
			FROM library_roots r WHERE r.library_id=$1`, collection.ID, recycleID, recycledEpisodeID, filepath.Join(root, "#recycle"), recycledPath)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed"); job.Error != "" {
		t.Fatalf("correcting the layout caused scan warnings: %s", job.Error)
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true}).Items
	assertScannedPhysicalTVHierarchy(t, items, episodePath, 1, 2)
	for _, old := range []Item{group, series, season, episode} {
		if item := libraryIntegrationItemByPath(t, items, old.Path); item.ID != old.ID {
			t.Errorf("layout correction changed the ID of %s: %s -> %s", old.Path, old.ID, item.ID)
		}
	}
	seriesList := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: collection.ID, Recursive: true, IncludeItemTypes: []string{"Series"}})
	if seriesList.TotalRecordCount != 1 || len(seriesList.Items) != 1 || seriesList.Items[0].ID != series.ID {
		t.Errorf("series list still contains a grouping or recycle directory: %+v", seriesList)
	}
	data, err := store.GetUserData(ctx, userID, episode.ID)
	if err != nil || !data.IsFavorite || data.PlaybackPositionTicks != position {
		t.Errorf("layout correction lost the episode's user data: %+v, %v", data, err)
	}
	for _, id := range []string{recycleID, recycledEpisodeID} {
		if _, err := store.GetItem(ctx, userID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("historical system item %s remains visible: %v", id, err)
		}
	}
	var retained int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE id IN ($1,$2)`, recycleID, recycledEpisodeID).Scan(&retained); err != nil || retained != 2 {
		t.Errorf("excluded system rows should be retained, got %d: %v", retained, err)
	}
	if contents, err := os.ReadFile(recycledPath); err != nil || string(contents) != "video:recycled" {
		t.Errorf("excluded system media was changed: %q, %v", contents, err)
	}
}

func assertScannedPhysicalTVHierarchy(t *testing.T, items []Item, episodePath string, seasonNumber, episodeNumber int) {
	t.Helper()
	seasonPath := filepath.Dir(episodePath)
	if filepath.Base(seasonPath) == "Disc 1" {
		seasonPath = filepath.Dir(seasonPath)
	}
	series := libraryIntegrationItemByPath(t, items, filepath.Dir(seasonPath))
	season := libraryIntegrationItemByPath(t, items, seasonPath)
	episode := libraryIntegrationItemByPath(t, items, episodePath)
	if series.Type != "Series" || !series.IsFolder {
		t.Errorf("show directory %q is %s (IsFolder=%t), want Series folder", series.Path, series.Type, series.IsFolder)
	}
	if season.Type != "Season" || !season.IsFolder || season.ParentID != series.ID || season.IndexNumber != seasonNumber {
		t.Errorf("season %q = %s (IsFolder=%t), parent=%q, index=%d; want Season folder under %q with index %d",
			season.Path, season.Type, season.IsFolder, season.ParentID, season.IndexNumber, series.ID, seasonNumber)
	}
	if episode.Type != "Episode" || episode.IsFolder || episode.ParentID != season.ID ||
		episode.ParentIndexNumber != seasonNumber || episode.IndexNumber != episodeNumber {
		t.Errorf("episode %q = %s (IsFolder=%t), parent=%q, season=%d, index=%d; want Episode under %q with season %d and index %d",
			episode.Path, episode.Type, episode.IsFolder, episode.ParentID, episode.ParentIndexNumber, episode.IndexNumber, season.ID, seasonNumber, episodeNumber)
	}
}
