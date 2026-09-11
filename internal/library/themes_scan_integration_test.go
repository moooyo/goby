package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type themeScanTestResource struct {
	id, ownerID, kind, itemType, path string
	active                            bool
	probe                             media.Info
}

func themeScanTestResources(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) []themeScanTestResource {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT i.id, r.owner_item_id, r.kind, r.active, i.type, i.path, i.media
		FROM item_theme_resources r JOIN items i ON i.id = r.resource_item_id
		WHERE i.library_id = $1 ORDER BY i.path, i.id`, libraryID)
	if err != nil {
		t.Fatalf("read scanned theme resources: %v", err)
	}
	defer rows.Close()
	resources := make([]themeScanTestResource, 0)
	for rows.Next() {
		var resource themeScanTestResource
		var raw []byte
		if err := rows.Scan(&resource.id, &resource.ownerID, &resource.kind, &resource.active, &resource.itemType, &resource.path, &raw); err != nil {
			t.Fatalf("scan theme resource snapshot: %v", err)
		}
		if err := json.Unmarshal(raw, &resource.probe); err != nil {
			t.Fatalf("decode accepted theme media: %v", err)
		}
		resources = append(resources, resource)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish theme resource snapshot: %v", err)
	}
	return resources
}

func themeScanTestSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'relationship', to_jsonb(r), 'item', to_jsonb(i), 'metadata', to_jsonb(ms))
		ORDER BY i.id), '[]'::jsonb)::text
		FROM item_theme_resources r JOIN items i ON i.id = r.resource_item_id
		LEFT JOIN item_metadata_state ms ON ms.item_id = i.id
		WHERE i.library_id = $1`, libraryID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot accepted theme population: %v", err)
	}
	return snapshot
}

func themeScanTestQuery(t *testing.T, ctx context.Context, store *Store, userID, ownerID string) ThemeMediaResult {
	t.Helper()
	result, err := store.QueryThemeMedia(ctx, ownerID, ThemeQuery{
		Subject: Subject{UserID: userID}, EnableThemeSongs: true, EnableThemeVideos: true,
	})
	if err != nil {
		t.Fatalf("query scanned themes: %v", err)
	}
	return result
}

func themeScanTestForce(t *testing.T, ctx context.Context, store *Store, libraryID string) Job {
	t.Helper()
	job, err := store.StartScanWithOptions(ctx, libraryID, ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatalf("start forced theme scan: %v", err)
	}
	return job
}

func themeScanTestWaitStopped(t *testing.T, ctx context.Context, store *Store, jobID string) Job {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := store.GetJob(waitCtx, jobID)
		if err != nil {
			t.Fatalf("read theme scan job: %v", err)
		}
		if job.Status == "Completed" || job.Status == "Failed" || job.Status == "Cancelled" || job.Status == "Interrupted" {
			return job
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("theme scan did not stop: %v", waitCtx.Err())
		}
	}
}

func TestThemeScanAttachesSingleMovieResourcesAndKeepsCachedRoles(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	moviePath := libraryIntegrationFile(t, allowedRoot, "mixed/Film/Feature.mp4", "video:feature")
	songPath := libraryIntegrationFile(t, allowedRoot, "mixed/Film/theme.mp3", "audio:theme-song")
	videoPath := libraryIntegrationFile(t, allowedRoot, "mixed/Film/backdrops/video.mp4", "video:theme-video")
	library := libraryIntegrationCreate(t, ctx, store, "Movie themes", "mixed", filepath.Join(allowedRoot, "mixed"))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Fatalf("valid movie themes produced a warning: %+v", job)
	}
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	for _, item := range ordinary.Items {
		if item.Path == songPath || item.Path == videoPath || item.Type == "Audio" || item.Type == "Video" || item.Type == "MusicAlbum" {
			t.Errorf("reserved movie themes entered the ordinary hierarchy or album evidence: %+v", item)
		}
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 2 {
		t.Fatalf("scanned %d resources, want the complete song and video pair", len(resources))
	}
	for _, resource := range resources {
		wantType, wantKind := "Audio", "song"
		if resource.path == videoPath {
			wantType, wantKind = "Video", "video"
		} else if resource.path != songPath {
			t.Fatalf("published an unexpected theme path: %+v", resource)
		}
		if resource.ownerID != movie.ID || !resource.active || resource.kind != wantKind || resource.itemType != wantType || len(resource.probe.Streams) == 0 || resource.probe.Size <= 0 {
			t.Errorf("theme resource lost its semantic movie owner, role, or accepted media: %+v", resource)
		}
	}
	result := themeScanTestQuery(t, ctx, store, userID, movie.ID)
	if result.ThemeSongsResult.TotalRecordCount != 1 || result.ThemeVideosResult.TotalRecordCount != 1 {
		t.Fatalf("movie owner did not expose both complete theme groups: %+v", result)
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	calls := len(prober.calls())
	cached := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if cached.Error != "" || cached.Added != 0 || cached.Updated != 0 || len(prober.calls()) != calls {
		t.Errorf("cached theme scan probed or rewrote accepted files: %+v", cached)
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("cached theme scan changed resource identity, active role, or accepted metadata")
	}
}

func TestThemeScanPromotionPreservesUserDataAndLeavingCreatesOrdinaryIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable filesystem rename identity is supported on Linux")
	}
	prober := &metadataMusicScanProber{}
	movingFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Moving song", Album: "Moving album", Artist: "Moving artist"}
	remainingFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Remaining song", Album: "Remaining album", Artist: "Remaining artist"}
	for _, name := range []string{"01 Moving.mp3", "theme.mp3", "Recovered.mp3"} {
		prober.set(name, movingFacts, false)
	}
	prober.set("02 Remaining.flac", remainingFacts, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	movingPath := libraryIntegrationFile(t, allowedRoot, "mixed/Album/01 Moving.mp3", "audio:moving-song")
	libraryIntegrationFile(t, allowedRoot, "mixed/Album/02 Remaining.flac", "audio:remaining-song")
	moviePath := libraryIntegrationFile(t, allowedRoot, "mixed/Film/Feature.mp4", "video:feature")
	library := libraryIntegrationCreate(t, ctx, store, "Theme identity", "mixed", filepath.Join(allowedRoot, "mixed"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	moving := libraryIntegrationItemByPath(t, ordinary.Items, movingPath)
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, last_played_at, updated_at)
		VALUES ($1,$2,9007199254740993,7,true,'2025-01-01T00:00:00Z','2025-01-02T00:00:00Z')`, userID, moving.ID); err != nil {
		t.Fatalf("seed ordinary audio user data: %v", err)
	}
	var userDataBefore string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, userID, moving.ID).Scan(&userDataBefore); err != nil {
		t.Fatalf("snapshot original user data: %v", err)
	}
	themePath := filepath.Join(filepath.Dir(moviePath), "theme.mp3")
	if err := os.Rename(movingPath, themePath); err != nil {
		t.Fatalf("move ordinary audio into the theme layout: %v", err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("theme promotion failed: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || resources[0].id != moving.ID || resources[0].ownerID != movie.ID || !resources[0].active {
		t.Fatalf("ordinary audio promotion lost its original identity or semantic owner: %+v", resources)
	}
	var albumArtistsRaw []byte
	if err := pool.QueryRow(ctx, `SELECT music_source->'Artists' FROM item_metadata_state WHERE item_id=$1`, moving.ParentID).Scan(&albumArtistsRaw); err != nil {
		t.Fatalf("read the previous album after theme promotion: %v", err)
	}
	var albumArtists []string
	if err := json.Unmarshal(albumArtistsRaw, &albumArtists); err != nil || !reflect.DeepEqual(albumArtists, []string{"Remaining artist"}) {
		t.Errorf("the previous album retained its hidden theme member: artists=%q, error=%v", albumArtistsRaw, err)
	}
	recoveredPath := filepath.Join(filepath.Dir(movingPath), "Recovered.mp3")
	if err := os.Rename(themePath, recoveredPath); err != nil {
		t.Fatalf("move a theme resource back to an ordinary path: %v", err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("scan the resource after it leaves its theme role: %+v", job)
	}
	ordinary = libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	recovered := libraryIntegrationItemByPath(t, ordinary.Items, recoveredPath)
	if recovered.ID == moving.ID || recovered.Type != "Audio" {
		t.Errorf("ordinary identity lookup revived the permanently hidden theme identity: %+v", recovered)
	}
	for _, item := range ordinary.Items {
		if item.ID == moving.ID {
			t.Error("the retired theme identity appeared in ordinary results")
		}
	}
	resources = themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || resources[0].id != moving.ID || resources[0].active {
		t.Errorf("the old theme role was deleted or remained active after complete absence: %+v", resources)
	}
	if _, err := store.GetItem(ctx, userID, moving.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a retired theme remained directly readable: %v", err)
	}
	// Both the retired theme and the new ordinary row now share the inode.
	// A second move forces identity lookup to reject inactive roles as well.
	movedAgainPath := filepath.Join(filepath.Dir(recoveredPath), "MovedAgain.mp3")
	prober.set("MovedAgain.mp3", movingFacts, false)
	if err := os.Rename(recoveredPath, movedAgainPath); err != nil {
		t.Fatalf("move the ordinary file after its historical theme role retired: %v", err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("scan the ordinary file beside its inactive identity candidate: %+v", job)
	}
	ordinary = libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	movedAgain := libraryIntegrationItemByPath(t, ordinary.Items, movedAgainPath)
	if movedAgain.ID != recovered.ID {
		t.Errorf("ordinary rename lookup selected a historical theme identity: got %s, want %s", movedAgain.ID, recovered.ID)
	}
	var userDataAfter string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, userID, moving.ID).Scan(&userDataAfter); err != nil || userDataAfter != userDataBefore {
		t.Errorf("theme transitions changed or removed historical user data: %v", err)
	}
	var transferred int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id=$1 AND item_id=$2`, userID, recovered.ID).Scan(&transferred); err != nil || transferred != 0 {
		t.Errorf("the new ordinary identity inherited theme user data: count=%d, error=%v", transferred, err)
	}
}

func TestThemeScanMissingResourcesWaitForCompleteRootAfterFailureAndCancellation(t *testing.T) {
	fixture := &libraryFixtureProber{}
	var failVideo, blockVideo atomic.Bool
	entered := make(chan struct{}, 1)
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "video.mp4" {
			if failVideo.Load() {
				return media.Info{}, errors.New("theme video probe failed")
			}
			if blockVideo.Load() {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-ctx.Done()
				return media.Info{}, ctx.Err()
			}
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	moviePath := libraryIntegrationFile(t, allowedRoot, "movies/Film/Feature.mp4", "video:feature")
	songPath := libraryIntegrationFile(t, allowedRoot, "movies/Film/theme.mp3", "audio:theme-song")
	libraryIntegrationFile(t, allowedRoot, "movies/Film/backdrops/video.mp4", "video:theme-video")
	library := libraryIntegrationCreate(t, ctx, store, "Retained themes", "movies", filepath.Join(allowedRoot, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	if resources := themeScanTestResources(t, ctx, pool, library.ID); len(resources) != 2 {
		t.Fatalf("initial complete theme pair was not accepted: %+v", resources)
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	if err := os.Remove(songPath); err != nil {
		t.Fatalf("remove the accepted theme song: %v", err)
	}
	failVideo.Store(true)
	failed := themeScanTestForce(t, ctx, store, library.ID)
	failed = libraryIntegrationWaitJob(t, ctx, store, failed.ID, "Completed")
	if failed.Error == "" {
		t.Fatal("failed theme probing did not make the root incomplete")
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("a theme probe failure retired a missing sibling or replaced its prior accepted snapshot")
	}
	failVideo.Store(false)
	blockVideo.Store(true)
	cancelled := themeScanTestForce(t, ctx, store, library.ID)
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("forced theme scan did not enter the blocking video probe")
	}
	if err := store.CancelJob(ctx, cancelled.ID); err != nil {
		t.Fatalf("cancel the incomplete theme scan: %v", err)
	}
	libraryIntegrationWaitJob(t, ctx, store, cancelled.ID, "Cancelled")
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("cancellation retired or replaced the previous complete theme population")
	}
	blockVideo.Store(false)
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("complete stable root did not recover: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	activeSongs, activeVideos := 0, 0
	for _, resource := range resources {
		if resource.active && resource.kind == "song" {
			activeSongs++
		}
		if resource.active && resource.kind == "video" {
			activeVideos++
		}
	}
	if len(resources) != 2 || activeSongs != 0 || activeVideos != 1 {
		t.Errorf("complete root did not retire only the missing theme: %+v", resources)
	}
	result := themeScanTestQuery(t, ctx, store, userID, movie.ID)
	if result.ThemeSongsResult.TotalRecordCount != 0 || result.ThemeVideosResult.TotalRecordCount != 1 {
		t.Errorf("theme query did not reflect complete-root retirement: %+v", result)
	}
	var reserved bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM theme_reserved_paths p JOIN library_roots r ON r.id=p.root_id
		WHERE r.library_id=$1 AND p.relative_path='Film/theme.mp3')`, library.ID).Scan(&reserved); err != nil || !reserved {
		t.Errorf("theme retirement removed its permanent path reservation: %v", err)
	}
}

func TestThemeScanDirectoryReplacementRetainsPreviousActivePopulation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fixture replaces a directory while its descriptor is held open")
	}
	fixture := &libraryFixtureProber{}
	var replace, replaced atomic.Bool
	var ownerPath string
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Feature.mp4" && replace.CompareAndSwap(true, false) {
			if err := os.Rename(ownerPath, ownerPath+"-replaced"); err != nil {
				return media.Info{}, err
			}
			if err := os.Mkdir(ownerPath, 0700); err != nil {
				return media.Info{}, err
			}
			if err := os.WriteFile(filepath.Join(ownerPath, "Feature.mp4"), []byte("video:replacement"), 0600); err != nil {
				return media.Info{}, err
			}
			replaced.Store(true)
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
	moviePath := libraryIntegrationFile(t, allowedRoot, "movies/Film/Feature.mp4", "video:original")
	ownerPath = filepath.Dir(moviePath)
	songPath := libraryIntegrationFile(t, allowedRoot, "movies/Film/theme.mp3", "audio:theme-song")
	library := libraryIntegrationCreate(t, ctx, store, "Changing theme directory", "movies", filepath.Join(allowedRoot, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if resources := themeScanTestResources(t, ctx, pool, library.ID); len(resources) != 1 || !resources[0].active {
		t.Fatalf("initial theme population was not accepted: %+v", resources)
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	if err := os.Remove(songPath); err != nil {
		t.Fatalf("remove the theme before the unstable listing: %v", err)
	}
	replace.Store(true)
	job := themeScanTestForce(t, ctx, store, library.ID)
	job = themeScanTestWaitStopped(t, ctx, store, job.ID)
	if !replaced.Load() || job.Error == "" || job.Status == "Cancelled" {
		t.Fatalf("the directory replacement was not observed as an incomplete root: %+v", job)
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("a replaced owner directory authorized retirement of its previous active theme")
	}
}

func TestThemeScanCompleteOwnerPublishesDespiteIndependentOrdinaryProbeWarning(t *testing.T) {
	fixture := &libraryFixtureProber{}
	var rejected atomic.Bool
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Broken.mp4" {
			rejected.Store(true)
			return media.Info{}, errors.New("independent ordinary media probe failed")
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	libraryIntegrationFile(t, allowedRoot, "movies/Incomplete/Broken.mp4", "video:unreadable-feature")
	moviePath := libraryIntegrationFile(t, allowedRoot, "movies/Complete/Feature.mp4", "video:complete-feature")
	songPath := libraryIntegrationFile(t, allowedRoot, "movies/Complete/theme.mp3", "audio:complete-theme")
	library := libraryIntegrationCreate(t, ctx, store, "Independent theme owner", "movies", filepath.Join(allowedRoot, "movies"))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if !rejected.Load() || job.Error == "" {
		t.Fatalf("the independent ordinary probe warning was not exercised: %+v", job)
	}
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || resources[0].ownerID != movie.ID || resources[0].path != songPath || !resources[0].active {
		t.Fatalf("an independent ordinary warning blocked a complete theme owner from publishing: %+v", resources)
	}
	result := themeScanTestQuery(t, ctx, store, userID, movie.ID)
	if result.ThemeSongsResult.TotalRecordCount != 1 || result.ThemeVideosResult.TotalRecordCount != 0 {
		t.Errorf("the complete owner did not expose its accepted theme after an independent warning: %+v", result)
	}
}

func TestThemeScanAmbiguousMovieAndSongLayoutsPreservePreviousCompletePopulation(t *testing.T) {
	for _, conflict := range []string{"multiple movies", "multiple song layouts"} {
		t.Run(conflict, func(t *testing.T) {
			ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			libraryIntegrationFile(t, allowedRoot, "movies/Film/Feature.mp4", "video:feature")
			libraryIntegrationFile(t, allowedRoot, "movies/Film/theme.mp3", "audio:theme-song")
			libraryIntegrationFile(t, allowedRoot, "movies/Film/backdrops/one.mp4", "video:original-theme")
			library := libraryIntegrationCreate(t, ctx, store, "Ambiguous themes", "movies", filepath.Join(allowedRoot, "movies"))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			if resources := themeScanTestResources(t, ctx, pool, library.ID); len(resources) != 2 {
				t.Fatalf("initial theme population was not complete: %+v", resources)
			}
			before := themeScanTestSnapshot(t, ctx, pool, library.ID)
			if conflict == "multiple movies" {
				libraryIntegrationFile(t, allowedRoot, "movies/Film/Another.mp4", "video:another-feature")
			} else {
				libraryIntegrationFile(t, allowedRoot, "movies/Film/theme-music/another.mp3", "audio:conflicting-layout")
			}
			// A new video makes a partial publication observable even when the
			// existing song and video have unchanged accepted media snapshots.
			libraryIntegrationFile(t, allowedRoot, "movies/Film/backdrops/two.mp4", "video:unpublished-theme")
			job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			if job.Error == "" {
				t.Fatal("ambiguous theme ownership or song layout produced no warning")
			}
			if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
				t.Error("ambiguity partially published a new theme or replaced the previous complete population")
			}
		})
	}
}

func TestThemeScanOwnerLimitCountsSongsAndVideosAcrossRootsWithoutPartialPublication(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, allowedRoot, "first/theme-music/original.mp3", "audio:original-theme")
	libraryIntegrationFile(t, allowedRoot, "second/backdrops/original.mp4", "video:original-theme")
	library, err := store.CreateLibrary(ctx, "Shared root theme owner", "mixed", []string{
		filepath.Join(allowedRoot, "first"), filepath.Join(allowedRoot, "second"),
	})
	if err != nil {
		t.Fatalf("create the shared-owner library roots: %v", err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("initial themes from separate roots were rejected: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 2 {
		t.Fatalf("initial cross-root population was not accepted: %+v", resources)
	}
	for _, resource := range resources {
		if resource.ownerID != library.ID || !resource.active {
			t.Fatalf("root-level theme did not use the shared library owner: %+v", resource)
		}
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	// The two roots are individually below the owner limit. Only their
	// combined song/video population is invalid, so per-root truncation or
	// publication before the other root is enumerated cannot pass this case.
	for index := 0; index < MaxThemeResourcesPerOwner-1; index++ {
		if index%2 == 0 {
			libraryIntegrationFile(t, allowedRoot, fmt.Sprintf("first/theme-music/extra-%03d.mp3", index), "audio:extra-theme")
		} else {
			libraryIntegrationFile(t, allowedRoot, fmt.Sprintf("second/backdrops/extra-%03d.mp4", index), "video:extra-theme")
		}
	}
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" {
		t.Fatal("the combined owner population exceeded 256 without a scan warning")
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("cross-root overflow truncated, partially published, or retired the previous complete population")
	}
	result := themeScanTestQuery(t, ctx, store, userID, library.ID)
	if result.ThemeSongsResult.TotalRecordCount != 1 || result.ThemeVideosResult.TotalRecordCount != 1 {
		t.Errorf("overflow replaced the previously complete theme groups: %+v", result)
	}
}

func TestThemeScanPresentHistoricalNestedResourcePreventsDeferredReplacement(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, allowedRoot, "movies/Film/Feature.mp4", "video:feature")
	originalPath := libraryIntegrationFile(t, allowedRoot, "movies/Film/theme-music/original.mp3", "audio:historical-theme")
	library := libraryIntegrationCreate(t, ctx, store, "Historical nested theme", "movies", filepath.Join(allowedRoot, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || !resources[0].active {
		t.Fatalf("initial historical theme was not accepted: %+v", resources)
	}
	nestedPath := filepath.Join(filepath.Dir(originalPath), "nested", "original.mp3")
	if err := os.MkdirAll(filepath.Dir(nestedPath), 0700); err != nil {
		t.Fatalf("create the historical nested source directory: %v", err)
	}
	if err := os.Rename(originalPath, nestedPath); err != nil {
		t.Fatalf("move the historical source into its retained nested layout: %v", err)
	}
	// Represent an older accepted nested layout without asking the current
	// classifier to accept it. Its permanent directory reservation and source
	// item remain valid, and the file really exists throughout the next scan.
	if _, err := pool.Exec(ctx, `UPDATE items SET path=$2,relative_path=$3 WHERE id=$1`,
		resources[0].id, nestedPath, "Film/theme-music/nested/original.mp3"); err != nil {
		t.Fatalf("seed the historical nested resource path: %v", err)
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	for index := 0; index < MaxThemeResourcesPerOwner; index++ {
		libraryIntegrationFile(t, allowedRoot, fmt.Sprintf("movies/Film/theme-music/new-%03d.mp3", index), "audio:new-theme")
	}
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" {
		t.Fatal("a present historical nested theme did not prevent the deferred replacement")
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("deferred publication retired a present nested source or published part of the new population")
	}
	if _, err := os.Stat(nestedPath); err != nil {
		t.Fatalf("the historical nested source was not present during the regression: %v", err)
	}
}

func TestThemeScanIncompleteRootRetainsOldLayoutWithoutPublishingConflictingRoot(t *testing.T) {
	fixture := &libraryFixtureProber{}
	var rejected atomic.Bool
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Broken.mp4" {
			rejected.Store(true)
			return media.Info{}, errors.New("ordinary probe prevents root retirement")
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	originalPath := libraryIntegrationFile(t, allowedRoot, "first/theme.mp3", "audio:original-direct-theme")
	libraryIntegrationFile(t, allowedRoot, "second/placeholder.txt", "ordinary nonmedia file")
	library, err := store.CreateLibrary(ctx, "Retained cross-root layout", "mixed", []string{
		filepath.Join(allowedRoot, "first"), filepath.Join(allowedRoot, "second"),
	})
	if err != nil {
		t.Fatalf("create the cross-root layout fixture: %v", err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("initial direct theme layout was not accepted: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || resources[0].ownerID != library.ID || !resources[0].active {
		t.Fatalf("the original theme did not belong to the shared library owner: %+v", resources)
	}
	before := themeScanTestSnapshot(t, ctx, pool, library.ID)
	if err := os.Remove(originalPath); err != nil {
		t.Fatalf("remove the old direct theme before the incomplete scan: %v", err)
	}
	libraryIntegrationFile(t, allowedRoot, "first/Broken.mp4", "video:unreadable-root-movie")
	libraryIntegrationFile(t, allowedRoot, "second/theme-music/new.mp3", "audio:conflicting-music-layout")
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if !rejected.Load() || job.Error == "" {
		t.Fatalf("the original source root was not made incomplete by its ordinary probe: %+v", job)
	}
	if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
		t.Error("a retained old direct layout was retired or mixed with a newly published cross-root music layout")
	}
	result := themeScanTestQuery(t, ctx, store, userID, library.ID)
	if result.ThemeSongsResult.TotalRecordCount != 1 || len(result.ThemeSongsResult.Items) != 1 || result.ThemeSongsResult.Items[0].ID != resources[0].id {
		t.Errorf("the shared owner did not retain only its original complete population: %+v", result)
	}
}

func TestThemeScanSyntheticSeriesRootOwnsItsRootTheme(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, allowedRoot, "television/Season 01/Show.S01E01.mp4", "video:series-episode")
	themePath := libraryIntegrationFile(t, allowedRoot, "television/theme.mp3", "audio:series-theme")
	library := libraryIntegrationCreate(t, ctx, store, "Synthetic series theme", "tvshows", filepath.Join(allowedRoot, "television"))
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("the synthetic root series theme was not accepted: %+v", job)
	}
	var seriesID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id=$1
		AND relative_path='//series/root' AND type='Series' AND is_folder`, library.ID).Scan(&seriesID); err != nil {
		t.Fatalf("season discovery did not establish the synthetic root series: %v", err)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != 1 || resources[0].ownerID != seriesID || resources[0].ownerID == library.ID || resources[0].path != themePath || !resources[0].active {
		t.Fatalf("root-level theme was attached to the collection instead of its discovered series: %+v", resources)
	}
	result := themeScanTestQuery(t, ctx, store, userID, seriesID)
	if result.ThemeSongsResult.TotalRecordCount != 1 || len(result.ThemeSongsResult.Items) != 1 || result.ThemeSongsResult.Items[0].ID != resources[0].id {
		t.Errorf("the discovered root series did not expose its own theme: %+v", result)
	}
	collection := themeScanTestQuery(t, ctx, store, userID, library.ID)
	if collection.ThemeSongsResult.TotalRecordCount != 0 {
		t.Errorf("the collection retained a duplicate synthetic-series theme association: %+v", collection)
	}
}
