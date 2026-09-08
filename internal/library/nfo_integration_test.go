package library

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

func nfoExpectedMetadata(t *testing.T, document string) *metadata.Metadata {
	t.Helper()
	value, err := metadata.ParseNFO(strings.NewReader(document))
	if err != nil {
		t.Fatalf("parse valid NFO fixture: %v", err)
	}
	return &value
}

func nfoCatalogItem(t *testing.T, ctx context.Context, store *Store, userID, libraryID, path string) Item {
	t.Helper()
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: libraryID, Recursive: true})
	item := libraryIntegrationItemByPath(t, items.Items, path)
	detail, err := store.GetItem(ctx, userID, item.ID)
	if err != nil {
		t.Fatalf("read persisted NFO item: %v", err)
	}
	return detail
}

func nfoAssertMetadata(t *testing.T, item Item, expected *metadata.Metadata) {
	t.Helper()
	if item.Metadata == nil || !reflect.DeepEqual(item.Metadata, expected) {
		t.Fatalf("persisted local metadata = %+v, want %+v", item.Metadata, expected)
	}
	sortName := expected.SortName
	if sortName == "" {
		sortName = expected.Name
	}
	if item.Name != expected.Name || item.SortName != strings.ToLower(sortName) || item.Overview != expected.Overview {
		t.Errorf("NFO descriptive fields were not applied: item = %+v, metadata = %+v", item, expected)
	}
}

func nfoAssertRetained(t *testing.T, item, expected Item) {
	t.Helper()
	if !reflect.DeepEqual(item, expected) {
		t.Errorf("invalid sidecar changed a previously valid catalog item: got %+v, want %+v", item, expected)
	}
}

func nfoInitialProbeCount(t *testing.T, prober *libraryFixtureProber, expected int) int {
	t.Helper()
	count := len(prober.calls())
	if count != expected {
		t.Fatalf("initial media probe count = %d, want %d", count, expected)
	}
	return count
}

func TestStoreMovieNFOPriorityUpdatesAndRemoval(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Raw Movie.mp4", "video:nfo-movie")
	preferredDocument := `<movie>
		<title>Preferred Movie</title><sorttitle>Preferred Sort Title</sorttitle>
		<originaltitle>Original Movie</originaltitle><plot>A local movie overview.</plot>
		<year>2021</year><premiered>2021-04-05</premiered><rating>8.5</rating><mpaa>PG-13</mpaa>
		<uniqueid type="imdb">tt1234567</uniqueid><tmdbid>12345</tmdbid>
		<genre>Adventure</genre><tag>Family favorite</tag><studio>Local Studio</studio>
		<actor><name>Example Actor</name><role>Lead</role><order>0</order></actor>
		<director>Example Director</director>
	</movie>`
	fallbackDocument := `<movie><title>Directory Fallback</title><sorttitle>Fallback Sort</sorttitle><plot>Fallback overview.</plot><year>1999</year></movie>`
	preferredPath := libraryIntegrationFile(t, allowedRoot, "movies/Raw Movie.nfo", preferredDocument)
	fallbackPath := libraryIntegrationFile(t, allowedRoot, "movies/movie.nfo", fallbackDocument)
	library := libraryIntegrationCreate(t, ctx, store, "Local movies", "movies", filepath.Dir(mediaPath))
	firstJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if firstJob.Error != "" {
		t.Errorf("valid movie NFO produced a warning: %q", firstJob.Error)
	}
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	nfoAssertMetadata(t, first, nfoExpectedMetadata(t, preferredDocument))
	if first.Metadata.ProductionYear == nil || *first.Metadata.ProductionYear != 2021 || first.Metadata.ProviderIDs["Imdb"] != "tt1234567" || first.Metadata.ProviderIDs["Tmdb"] != "12345" || len(first.Metadata.People) != 2 {
		t.Errorf("movie year, provider IDs, or credits were not persisted: %+v", first.Metadata)
	}
	var firstHash, firstSource string
	if err := pool.QueryRow(ctx, "SELECT local_metadata_hash, local_metadata_path FROM items WHERE id = $1", first.ID).Scan(&firstHash, &firstSource); err != nil {
		t.Fatalf("read persisted NFO source: %v", err)
	}
	if firstHash == "" || firstSource != "Raw Movie.nfo" {
		t.Errorf("preferred NFO source was not recorded: hash = %q, path = %q", firstHash, firstSource)
	}
	probeCount := nfoInitialProbeCount(t, prober, 1)
	if first.Media == nil {
		t.Fatal("initial movie scan did not persist media probe metadata")
	}
	updatedDocument := `<movie><title>Revised Movie</title><sorttitle>Revised Sort</sorttitle><plot>Revised local overview.</plot><year>2022</year><imdbid>tt7654321</imdbid></movie>`
	libraryIntegrationFile(t, allowedRoot, "movies/Raw Movie.nfo", updatedDocument)
	updatedJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if updatedJob.Added != 0 || updatedJob.Updated != 1 || updatedJob.Error != "" {
		t.Errorf("NFO-only change did not update the cached media item: %+v", updatedJob)
	}
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	nfoAssertMetadata(t, updated, nfoExpectedMetadata(t, updatedDocument))
	if updated.ID != first.ID || !reflect.DeepEqual(updated.Media, first.Media) {
		t.Error("NFO-only update changed the media identity or probe metadata")
	}
	var updatedHash string
	if err := pool.QueryRow(ctx, "SELECT local_metadata_hash FROM items WHERE id = $1", first.ID).Scan(&updatedHash); err != nil || updatedHash == "" || updatedHash == firstHash {
		t.Errorf("NFO-only update did not change its content hash: hash = %q, error = %v", updatedHash, err)
	}
	if err := os.Remove(preferredPath); err != nil {
		t.Fatalf("remove preferred NFO fixture: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	fallback := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	nfoAssertMetadata(t, fallback, nfoExpectedMetadata(t, fallbackDocument))
	if fallback.ID != first.ID {
		t.Error("switching to directory NFO fallback changed the item ID")
	}
	var fallbackSource string
	if err := pool.QueryRow(ctx, "SELECT local_metadata_path FROM items WHERE id = $1", first.ID).Scan(&fallbackSource); err != nil || fallbackSource != "movie.nfo" {
		t.Errorf("fallback source path = %q, error = %v", fallbackSource, err)
	}
	if err := os.Remove(fallbackPath); err != nil {
		t.Fatalf("remove fallback NFO fixture: %v", err)
	}
	removedJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	removed := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	if removedJob.Error != "" || removed.ID != first.ID || removed.Name != "Raw Movie" || removed.SortName != "raw movie" || removed.Overview != "" || removed.Metadata != nil || !reflect.DeepEqual(removed.Media, first.Media) {
		t.Errorf("removing all sidecars did not restore filename metadata: item = %+v, job = %+v", removed, removedJob)
	}
	var clearedHash, clearedSource string
	var metadataIsNull bool
	if err := pool.QueryRow(ctx, "SELECT local_metadata IS NULL, local_metadata_hash, local_metadata_path FROM items WHERE id = $1", first.ID).Scan(&metadataIsNull, &clearedHash, &clearedSource); err != nil || !metadataIsNull || clearedHash != "" || clearedSource != "" {
		t.Errorf("deleted sidecar left persisted source data: null = %v, hash = %q, source = %q, error = %v", metadataIsNull, clearedHash, clearedSource, err)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("NFO changes repeated media probing: calls = %d, want %d", calls, probeCount)
	}
}

func TestStoreInvalidNFOPreservesValidMetadataWithoutReprobing(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Safe.mp4", "video:safe-nfo")
	validDocument := `<movie><title>Retained Movie</title><sorttitle>Retained Sort</sorttitle><plot>Retained overview.</plot><year>2008</year><imdbid>tt1111111</imdbid></movie>`
	sidecarPath := libraryIntegrationFile(t, allowedRoot, "movies/Safe.nfo", validDocument)
	libraryIntegrationFile(t, allowedRoot, "movies/movie.nfo", `<movie><title>Lower Priority Fallback</title><plot>Must not replace valid metadata when the preferred sidecar is invalid.</plot></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Safe local movies", "movies", filepath.Dir(mediaPath))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	valid := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	nfoAssertMetadata(t, valid, nfoExpectedMetadata(t, validDocument))
	probeCount := nfoInitialProbeCount(t, prober, 1)
	if valid.Media == nil {
		t.Fatal("initial movie scan did not persist media probe metadata")
	}
	externalPath := libraryIntegrationFile(t, t.TempDir(), "external-secret.txt", "External entity metadata must not be applied")
	externalURL := url.URL{Scheme: "file", Path: filepath.ToSlash(externalPath)}
	if !strings.HasPrefix(externalURL.Path, "/") {
		externalURL.Path = "/" + externalURL.Path
	}
	for _, test := range []struct {
		name, document string
	}{
		{name: "MalformedXML", document: `<movie><title>Broken</movie>`},
		{name: "Oversized", document: `<movie><plot>` + strings.Repeat("x", 2*1024*1024+1) + `</plot></movie>`},
		{name: "ExternalEntity", document: `<!DOCTYPE movie [<!ENTITY secret SYSTEM "` + externalURL.String() + `">]><movie><title>&secret;</title></movie>`},
		{name: "WrongKind", document: `<tvshow><title>Wrong item kind</title></tvshow>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			libraryIntegrationFile(t, allowedRoot, "movies/Safe.nfo", test.document)
			job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			if job.Error == "" {
				t.Error("invalid sidecar did not record a scan warning")
			}
			item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
			nfoAssertRetained(t, item, valid)
			if calls := len(prober.calls()); calls != probeCount {
				t.Errorf("invalid sidecar repeated media probing: calls = %d, want %d", calls, probeCount)
			}
		})
	}
	t.Run("EscapingSymlink", func(t *testing.T) {
		outsideRoot := t.TempDir()
		outsidePath := libraryIntegrationFile(t, outsideRoot, "Outside.nfo", `<movie><title>Escaped Metadata</title><plot>Must not be read.</plot></movie>`)
		if err := os.Remove(sidecarPath); err != nil {
			t.Fatalf("remove invalid sidecar before creating symlink: %v", err)
		}
		if err := os.Symlink(outsidePath, sidecarPath); err != nil {
			t.Skipf("filesystem cannot create symlinks: %v", err)
		}
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		if job.Error == "" {
			t.Error("unsafe NFO symlink did not record a scan warning")
		}
		nfoAssertRetained(t, nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath), valid)
		if calls := len(prober.calls()); calls != probeCount {
			t.Errorf("unsafe NFO symlink repeated media probing: calls = %d, want %d", calls, probeCount)
		}
	})
}

func TestStoreDirectoryNFOMetadataKeepsFilesystemKinds(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	episodePath := libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.mp4", "video:directory-nfo")
	audioPath := libraryIntegrationFile(t, allowedRoot, "music/Artist/Album/01 Title.flac", "audio:directory-nfo")
	seriesDocument := `<tvshow><title>Local Series</title><plot>Series overview.</plot><year>2020</year><tvdbid>54321</tvdbid></tvshow>`
	seasonDocument := `<season><title>Local First Season</title><plot>Season overview.</plot><season>1</season></season>`
	albumDocument := `<album><title>Local Album</title><sorttitle>Album Sort</sorttitle><plot>Album overview.</plot><year>2003</year><genre>Jazz</genre></album>`
	libraryIntegrationFile(t, allowedRoot, "television/Show/tvshow.nfo", seriesDocument)
	libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/season.nfo", seasonDocument)
	libraryIntegrationFile(t, allowedRoot, "music/Artist/Album/album.nfo", albumDocument)
	television := libraryIntegrationCreate(t, ctx, store, "Local television", "tvshows", filepath.Join(allowedRoot, "television"))
	music := libraryIntegrationCreate(t, ctx, store, "Local music", "music", filepath.Join(allowedRoot, "music"))
	libraryIntegrationScan(t, ctx, store, television.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, music.ID, "Completed")
	seriesPath := filepath.Join(allowedRoot, "television", "Show")
	series := nfoCatalogItem(t, ctx, store, userID, television.ID, seriesPath)
	season := nfoCatalogItem(t, ctx, store, userID, television.ID, filepath.Dir(episodePath))
	album := nfoCatalogItem(t, ctx, store, userID, music.ID, filepath.Dir(audioPath))
	nfoAssertMetadata(t, series, nfoExpectedMetadata(t, seriesDocument))
	nfoAssertMetadata(t, season, nfoExpectedMetadata(t, seasonDocument))
	nfoAssertMetadata(t, album, nfoExpectedMetadata(t, albumDocument))
	if series.Type != "Series" || !series.IsFolder || season.Type != "Season" || !season.IsFolder || season.ParentID != series.ID || season.IndexNumber != 1 || album.Type != "MusicAlbum" || !album.IsFolder {
		t.Fatalf("NFO directory metadata changed filesystem kinds or hierarchy: series = %+v, season = %+v, album = %+v", series, season, album)
	}
	probeCount := nfoInitialProbeCount(t, prober, 2)
	libraryIntegrationFile(t, allowedRoot, "television/Show/tvshow.nfo", `<movie><title>Not a Series</title></movie>`)
	libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/season.nfo", `<tvshow><title>Not a Season</title></tvshow>`)
	libraryIntegrationFile(t, allowedRoot, "music/Artist/Album/album.nfo", `<artist><name>Not an Album</name></artist>`)
	for _, libraryID := range []string{television.ID, music.ID} {
		if job := libraryIntegrationScan(t, ctx, store, libraryID, "Completed"); job.Error == "" {
			t.Errorf("wrong directory NFO kind produced no warning for library %s", libraryID)
		}
	}
	nfoAssertRetained(t, nfoCatalogItem(t, ctx, store, userID, television.ID, seriesPath), series)
	nfoAssertRetained(t, nfoCatalogItem(t, ctx, store, userID, television.ID, filepath.Dir(episodePath)), season)
	nfoAssertRetained(t, nfoCatalogItem(t, ctx, store, userID, music.ID, filepath.Dir(audioPath)), album)
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("directory NFO changes repeated media probing: calls = %d, want %d", calls, probeCount)
	}
}

func TestStoreEpisodeNFOOverridesNumbersWithoutChangingSeasonParent(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	episodePath := libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.mp4", "video:episode-nfo")
	validDocument := `<episodedetails><title>Local Episode</title><plot>Episode overview.</plot><season>1</season><episode>7</episode><aired>2024-02-03</aired></episodedetails>`
	libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.nfo", validDocument)
	library := libraryIntegrationCreate(t, ctx, store, "Episode metadata", "tvshows", filepath.Join(allowedRoot, "television"))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Errorf("matching episode season produced a warning: %q", job.Error)
	}
	season := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(episodePath))
	episode := nfoCatalogItem(t, ctx, store, userID, library.ID, episodePath)
	nfoAssertMetadata(t, episode, nfoExpectedMetadata(t, validDocument))
	if episode.Type != "Episode" || episode.IndexNumber != 7 || episode.ParentIndexNumber != 1 || episode.ParentID != season.ID {
		t.Fatalf("valid episode NFO did not preserve its filesystem season: %+v", episode)
	}
	probeCount := nfoInitialProbeCount(t, prober, 1)
	if episode.Media == nil {
		t.Fatal("initial episode scan did not persist media probe metadata")
	}
	conflictingDocument := `<episodedetails><title>Updated Episode</title><plot>Updated episode overview.</plot><season>9</season><episode>8</episode></episodedetails>`
	libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.nfo", conflictingDocument)
	conflictJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if conflictJob.Error == "" {
		t.Error("conflicting episode season did not record a warning")
	}
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, episodePath)
	expected := nfoExpectedMetadata(t, conflictingDocument)
	expected.ParentIndexNumber = nil
	nfoAssertMetadata(t, updated, expected)
	if updated.ID != episode.ID || updated.ParentID != season.ID || updated.ParentIndexNumber != 1 || updated.IndexNumber != 8 {
		t.Errorf("conflicting NFO season changed item identity or hierarchy: %+v", updated)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("episode NFO change repeated media probing: calls = %d, want %d", calls, probeCount)
	}
}

func TestStoreEpisodeNFOCanSupplySeasonWithoutReparenting(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	episodePath := libraryIntegrationFile(t, allowedRoot, "television/Loose/Untitled.mp4", "video:loose-episode")
	library := libraryIntegrationCreate(t, ctx, store, "Unstructured episodes", "tvshows", filepath.Join(allowedRoot, "television"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, userID, library.ID, episodePath)
	series := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(episodePath))
	if before.Type != "Episode" || before.ParentID != series.ID || before.ParentIndexNumber != 0 || series.Type != "Series" {
		t.Fatalf("unstructured episode fixture unexpectedly has a season: episode = %+v, series = %+v", before, series)
	}
	probeCount := nfoInitialProbeCount(t, prober, 1)
	if before.Media == nil {
		t.Fatal("initial unstructured episode scan did not persist media probe metadata")
	}
	document := `<episodedetails><title>Locally Numbered Episode</title><plot>Local numbering only.</plot><season>3</season><episode>4</episode></episodedetails>`
	libraryIntegrationFile(t, allowedRoot, "television/Loose/Untitled.nfo", document)
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Errorf("NFO season without a structural season produced a warning: %q", job.Error)
	}
	after := nfoCatalogItem(t, ctx, store, userID, library.ID, episodePath)
	nfoAssertMetadata(t, after, nfoExpectedMetadata(t, document))
	if after.ID != before.ID || after.ParentID != before.ParentID || after.ParentIndexNumber != 3 || after.IndexNumber != 4 {
		t.Errorf("NFO numbering created an unwanted parent change: before = %+v, after = %+v", before, after)
	}
	seasons := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Season"}})
	if seasons.TotalRecordCount != 0 || len(seasons.Items) != 0 {
		t.Errorf("NFO-only season numbering created structural season items: %+v", seasons)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("NFO numbering repeated media probing: calls = %d, want %d", calls, probeCount)
	}
}

func TestStoreRootSeriesNFODescribesSyntheticRepresentative(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	episodePath := libraryIntegrationFile(t, allowedRoot, "ShowDir/Season 01/Show.S01E02.mp4", "video:root-series")
	document := `<tvshow><title>Series Root Title</title><plot>Series root overview.</plot><year>2019</year><tvdbid>9988</tvdbid></tvshow>`
	libraryIntegrationFile(t, allowedRoot, "ShowDir/tvshow.nfo", document)
	library := libraryIntegrationCreate(t, ctx, store, "Single show library", "tvshows", filepath.Join(allowedRoot, "ShowDir"))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Errorf("valid root series NFO produced a warning: %q", job.Error)
	}
	seriesItems := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Series"}})
	if seriesItems.TotalRecordCount != 1 || len(seriesItems.Items) != 1 {
		t.Fatalf("root series representative = %+v, want one Series", seriesItems)
	}
	series, err := store.GetItem(ctx, userID, seriesItems.Items[0].ID)
	if err != nil {
		t.Fatalf("read root series representative: %v", err)
	}
	nfoAssertMetadata(t, series, nfoExpectedMetadata(t, document))
	season := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(episodePath))
	episode := nfoCatalogItem(t, ctx, store, userID, library.ID, episodePath)
	if series.Type != "Series" || !series.IsFolder || series.ParentID != library.ID || season.Type != "Season" || season.ParentID != series.ID || season.IndexNumber != 1 || episode.Type != "Episode" || episode.ParentID != season.ID || episode.IndexNumber != 2 || episode.ParentIndexNumber != 1 {
		t.Errorf("root series metadata changed season or episode ancestry: series = %+v, season = %+v, episode = %+v", series, season, episode)
	}
}

type nfoRenameDuringProbe struct {
	fixture             libraryFixtureProber
	source, destination string
}

func (prober *nfoRenameDuringProbe) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := prober.fixture.ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	if len(prober.fixture.calls()) == 2 {
		if err := os.Rename(prober.source, prober.destination); err != nil {
			return media.Info{}, err
		}
	}
	return info, nil
}

func TestStoreParentRenameDuringProbeDoesNotRemoveLocalMetadata(t *testing.T) {
	prober := &nfoRenameDuringProbe{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Original/Film.mp4", "video:before-directory-move")
	document := `<movie><title>Preserved During Move</title><sorttitle>Preserved Sort</sorttitle><plot>Preserved sidecar overview.</plot><year>2017</year></movie>`
	libraryIntegrationFile(t, allowedRoot, "movies/Original/Film.nfo", document)
	prober.source = filepath.Dir(mediaPath)
	prober.destination = filepath.Join(allowedRoot, "MovedDuringProbe")
	library := libraryIntegrationCreate(t, ctx, store, "Moving media", "movies", filepath.Join(allowedRoot, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	original := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	nfoAssertMetadata(t, original, nfoExpectedMetadata(t, document))
	nfoInitialProbeCount(t, &prober.fixture, 1)
	var originalHash string
	if err := pool.QueryRow(ctx, "SELECT local_metadata_hash FROM items WHERE id = $1", original.ID).Scan(&originalHash); err != nil || originalHash == "" {
		t.Fatalf("read valid NFO hash before directory move: hash = %q, error = %v", originalHash, err)
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Original/Film.mp4", "video:changed-before-directory-move")
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" {
		t.Error("parent directory rename during probing did not record a warning")
	}
	if calls := len(prober.fixture.calls()); calls != 2 {
		t.Fatalf("media change did not enter the second probe: calls = %d, want 2", calls)
	}
	if info, err := os.Stat(filepath.Join(prober.destination, "Film.mp4")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("second probe did not move the media parent directory: error = %v", err)
	}
	retained, err := store.GetItem(ctx, userID, original.ID)
	if err != nil {
		t.Fatalf("read catalog item after directory move: %v", err)
	}
	if retained.Name != original.Name || retained.SortName != original.SortName || retained.Overview != original.Overview || !reflect.DeepEqual(retained.Metadata, original.Metadata) {
		t.Errorf("missing old NFO pathname was treated as sidecar deletion: before = %+v, after = %+v", original, retained)
	}
	var retainedHash string
	if err := pool.QueryRow(ctx, "SELECT local_metadata_hash FROM items WHERE id = $1", original.ID).Scan(&retainedHash); err != nil || retainedHash != originalHash {
		t.Errorf("parent rename cleared or changed the valid NFO hash: hash = %q, want %q, error = %v", retainedHash, originalHash, err)
	}
}
