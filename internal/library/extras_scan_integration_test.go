package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func extraScanTestResources(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) []themeScanTestResource {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT i.id,r.owner_item_id,r.kind,r.active,i.type,i.path,i.media
		FROM item_extra_resources r JOIN items i ON i.id=r.resource_item_id WHERE i.library_id=$1 ORDER BY i.path,i.id`, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	resources := []themeScanTestResource{}
	for rows.Next() {
		var resource themeScanTestResource
		var raw []byte
		if err := rows.Scan(&resource.id, &resource.ownerID, &resource.kind, &resource.active, &resource.itemType, &resource.path, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &resource.probe); err != nil {
			t.Fatal(err)
		}
		resources = append(resources, resource)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return resources
}

func extraScanTestSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'relationship',to_jsonb(r),'item',to_jsonb(i),'metadata',to_jsonb(ms)) ORDER BY i.id),'[]'::jsonb)::text
		FROM item_extra_resources r JOIN items i ON i.id=r.resource_item_id
		LEFT JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.library_id=$1`, libraryID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestExtraScanPublishesRealMovieResourcesAndReservesNestedContents(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("original source delivery requires the Linux file identity and change-time contract")
	}
	prober := &libraryFixtureProber{}
	// Byte delivery requires the current probe snapshot contract. Keep the
	// legacy fixture's content/call tracking, and bind its facts to the exact
	// descriptor inspected by the scanner rather than seeding catalog fields.
	sourceProber := forceProbeVersionedFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		info, err := prober.ProbeFile(ctx, file)
		if err != nil {
			return media.Info{}, err
		}
		stat, err := file.Stat()
		if err != nil {
			return media.Info{}, err
		}
		info.ProbeVersion = media.CurrentProbeVersion
		info.FileChangeTimeNs = media.FileChangeTime(stat)
		return info, nil
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, sourceProber)
	moviePath := libraryIntegrationFile(t, root, "movies/Positive/Main.mp4", "video:main")
	emptyPath := libraryIntegrationFile(t, root, "movies/Empty/Main.mp4", "video:empty")
	expected := map[string]string{}
	for _, spec := range []struct{ path, kind string }{
		{"featurettes/Zeta.mp4", ExtraKindClip}, {"featurettes/Alpha.mp4", ExtraKindClip},
		{"deleted scenes/Middle.mp4", ExtraKindDeletedScene}, {"trailers/Delta.mp4", ExtraKindTrailer},
	} {
		path := libraryIntegrationFile(t, root, "movies/Positive/"+spec.path, "video:"+spec.path)
		expected[path] = spec.kind
	}
	for _, path := range []string{"featurettes/nested/Hidden.mp4", "featurettes/notes.txt", "featurettes/backdrops/Hidden.mp4", "featurettes/theme.mp3", "backdrops/featurettes/Hidden.mp4"} {
		libraryIntegrationFile(t, root, "movies/Positive/"+path, "video:excluded")
	}
	lib := libraryIntegrationCreate(t, ctx, store, "Movie extras", "movies", filepath.Join(root, "movies"))
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("complete extras scan warned: %+v", job)
	}
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: lib.ID, Recursive: true})
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	empty := libraryIntegrationItemByPath(t, ordinary.Items, emptyPath)
	for _, item := range ordinary.Items {
		if !item.IsFolder && item.ID != movie.ID && item.ID != empty.ID {
			t.Errorf("an auxiliary source escaped ordinary filtering: %+v", item)
		}
	}
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 4 {
		t.Fatalf("published %d extras, want 4", len(resources))
	}
	for _, resource := range resources {
		if !resource.active || resource.ownerID != movie.ID || resource.kind != expected[resource.path] || resource.itemType != "Video" || resource.probe.Size <= 0 ||
			resource.probe.ProbeVersion < media.CurrentProbeVersion || resource.probe.FileChangeTimeNs <= 0 {
			t.Fatalf("extra lost its own source or owner: %+v", resource)
		}
		item, err := store.GetItem(ctx, userID, resource.id)
		if err != nil || item.ParentID != movie.ID || item.ExtraKind != resource.kind {
			t.Fatalf("direct extra lost association: %+v, %v", item, err)
		}
		file, source, err := store.OpenMedia(ctx, userID, item.ID, media.SourceID(item.ID))
		if err != nil {
			t.Fatalf("open real extra source: %v", err)
		}
		body, readErr := io.ReadAll(file)
		_ = file.Close()
		want, err := os.ReadFile(resource.path)
		if err != nil || readErr != nil || string(body) != string(want) || source.Item.ID != item.ID || source.Item.ID == movie.ID {
			t.Fatalf("extra delivery substituted the owner or another source: %v, %v", err, readErr)
		}
	}
	features, err := store.QuerySpecialFeatures(ctx, movie.ID, Subject{UserID: userID})
	if err != nil || len(features) != 3 {
		t.Fatalf("SpecialFeatures did not expose all three real extras: %+v, %v", features, err)
	}
	trailers, err := store.QueryLocalTrailers(ctx, movie.ID, Subject{UserID: userID})
	if err != nil || len(trailers) != 1 || trailers[0].Name != "Delta" || trailers[0].ExtraOwnerName != movie.Name {
		t.Fatalf("trailer filename and Movie owner projection diverged: %+v, %v", trailers, err)
	}
	if items, err := store.QuerySpecialFeatures(ctx, empty.ID, Subject{UserID: userID}); err != nil || len(items) != 0 {
		t.Fatalf("empty Movie extras = %+v, %v", items, err)
	}
	if themes := themeScanTestResources(t, ctx, pool, lib.ID); len(themes) != 0 {
		t.Fatalf("nested themes escaped an outer auxiliary boundary: %+v", themes)
	}
	before, calls := extraScanTestSnapshot(t, ctx, pool, lib.ID), len(prober.calls())
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != calls {
		t.Fatalf("cached extra scan rewrote or reprobed accepted sources: %+v", job)
	}
	if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
		t.Fatal("cached scan changed extra identity, metadata, or roles")
	}
}

func TestExtraScanRetainsTheWholeOwnerOnProbeFailureCancellationAndUncertainty(t *testing.T) {
	fixture := &libraryFixtureProber{}
	var fail, block atomic.Bool
	entered := make(chan struct{}, 1)
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Keep.mp4" {
			if fail.Load() {
				return media.Info{}, errors.New("extra probe failed")
			}
			if block.Load() {
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
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
	removed := libraryIntegrationFile(t, root, "movies/Film/featurettes/Remove.mp4", "video:remove")
	libraryIntegrationFile(t, root, "movies/Film/featurettes/Keep.mp4", "video:keep")
	lib := libraryIntegrationCreate(t, ctx, store, "Retained extras", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 2 {
		t.Fatalf("initial extras incomplete: %+v", resources)
	}
	before := extraScanTestSnapshot(t, ctx, pool, lib.ID)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	job := themeScanTestForce(t, ctx, store, lib.ID)
	job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if job.Error == "" {
		t.Fatal("failed probe did not mark the extra owner incomplete")
	}
	if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
		t.Fatal("probe failure retired or partly replaced an accepted extra set")
	}
	fail.Store(false)
	block.Store(true)
	job = themeScanTestForce(t, ctx, store, lib.ID)
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("extra probe did not block")
	}
	if err := store.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationWaitJob(t, ctx, store, job.ID, "Cancelled")
	if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
		t.Fatal("cancellation changed the accepted extra set")
	}
	block.Store(false)
	// A symlink is uncertainty, even when its target does not exist.
	if runtime.GOOS == "linux" {
		if err := os.Symlink("missing-target.mp4", removed); err != nil {
			t.Fatal(err)
		}
		if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error == "" {
			t.Fatal("symlink uncertainty produced no warning")
		}
		if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
			t.Fatal("a symlink authorized missing-source retirement")
		}
		if err := os.Remove(removed); err != nil {
			t.Fatal(err)
		}
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("stable complete owner did not recover: %+v", job)
	}
	resources = extraScanTestResources(t, ctx, pool, lib.ID)
	active := 0
	for _, resource := range resources {
		if resource.active {
			active++
		}
		if resource.path == removed {
			if resource.active {
				t.Fatal("proved missing extra remained active")
			}
			if _, err := store.GetItem(ctx, userID, resource.id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("inactive extra remained directly visible: %v", err)
			}
		}
	}
	if len(resources) != 2 || active != 1 {
		t.Fatalf("retirement deleted history or changed present siblings: %+v", resources)
	}
}

func TestExtraScanRejectsAmbiguousOwnersAndOverflowWithoutPartialPublication(t *testing.T) {
	for _, ambiguity := range []string{"multiple movies", "population overflow"} {
		t.Run(ambiguity, func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
			libraryIntegrationFile(t, root, "movies/Film/featurettes/Original.mp4", "video:original")
			lib := libraryIntegrationCreate(t, ctx, store, "Bounded extras", "movies", filepath.Join(root, "movies"))
			libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
			before := extraScanTestSnapshot(t, ctx, pool, lib.ID)
			if ambiguity == "multiple movies" {
				libraryIntegrationFile(t, root, "movies/Film/Second.mp4", "video:second")
				libraryIntegrationFile(t, root, "movies/Film/trailers/New.mp4", "video:new")
			} else {
				for index := 0; index < MaxExtraResourcesPerOwner; index++ {
					libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/featurettes/New%03d.mp4", index), "video:new")
				}
			}
			if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error == "" {
				t.Fatal("ambiguous or excessive extras produced no warning")
			}
			if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
				t.Fatal("an unsupported owner partially published new extras")
			}
		})
	}
}

func TestExtraScanPreservesRenameIdentityUserDataAndPermanentRoleBoundaries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable rename and hardlink identity requires Linux")
	}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	movingPath := libraryIntegrationFile(t, root, "movies/Moving/Original.mp4", "video:moving")
	moviePath := libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
	lib := libraryIntegrationCreate(t, ctx, store, "Stable extra roles", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: lib.ID, Recursive: true})
	moving := libraryIntegrationItemByPath(t, ordinary.Items, movingPath)
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite)
		VALUES($1,$2,9007199254740993,7,true)`, userID, moving.ID); err != nil {
		t.Fatal(err)
	}
	var userData string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, userID, moving.ID).Scan(&userData); err != nil {
		t.Fatal(err)
	}
	featurette := filepath.Join(filepath.Dir(moviePath), "featurettes", "Clip.mp4")
	if err := os.MkdirAll(filepath.Dir(featurette), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(movingPath, featurette); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("ordinary promotion failed: %+v", job)
	}
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || resources[0].id != moving.ID || resources[0].ownerID != movie.ID {
		t.Fatalf("promotion lost stable identity or Movie parent: %+v", resources)
	}
	renamed := filepath.Join(filepath.Dir(featurette), "Renamed.mp4")
	if err := os.Rename(featurette, renamed); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("extra rename failed: %+v", job)
	}
	if resources = extraScanTestResources(t, ctx, pool, lib.ID); len(resources) != 1 || resources[0].id != moving.ID {
		t.Fatalf("rename allocated another extra identity: %+v", resources)
	}
	copyPath := filepath.Join(filepath.Dir(renamed), "Copy.mp4")
	if err := os.Link(renamed, copyPath); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("hardlink extra scan failed: %+v", job)
	}
	resources = extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 2 || resources[0].id == resources[1].id {
		t.Fatalf("hardlinks reused one resource identity: %+v", resources)
	}
	themePath := filepath.Join(filepath.Dir(moviePath), "backdrops", "Theme.mp4")
	if err := os.MkdirAll(filepath.Dir(themePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(renamed, themePath); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("permanent role transition scan failed: %+v", job)
	}
	themes := themeScanTestResources(t, ctx, pool, lib.ID)
	if len(themes) != 1 || themes[0].id == moving.ID || !themes[0].active {
		t.Fatalf("theme stole a permanent extra identity: %+v", themes)
	}
	if _, err := store.GetItem(ctx, userID, moving.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired extra remained visible: %v", err)
	}
	returned := filepath.Join(filepath.Dir(copyPath), "Returned.mp4")
	if err := os.Rename(themePath, returned); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("return to extra role failed: %+v", job)
	}
	for _, resource := range extraScanTestResources(t, ctx, pool, lib.ID) {
		if resource.id == themes[0].id {
			t.Fatal("an extra stole an active or inactive permanent theme identity")
		}
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, userID, moving.ID).Scan(&after); err != nil || after != userData {
		t.Fatalf("identity transitions altered historical UserData: %v", err)
	}
	var both int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_extra_resources e JOIN item_theme_resources t USING(resource_item_id)`).Scan(&both); err != nil || both != 0 {
		t.Fatalf("cross-role memberships escaped: %d, %v", both, err)
	}
}

func TestExtraScanNewReservationDeactivatesOnlyThemeChildrenOfHiddenOwners(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mainPath := libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
	legacyPath := libraryIntegrationFile(t, root, "movies/Film/featurettes/Legacy.mp4", "video:legacy")
	libraryIntegrationFile(t, root, "movies/Film/featurettes/theme.mp3", "audio:legacy-theme")
	libraryIntegrationFile(t, root, "movies/Other/Main.mp4", "video:other")
	libraryIntegrationFile(t, root, "movies/Other/theme.mp3", "audio:other-theme")
	lib := libraryIntegrationCreate(t, ctx, store, "Changing classification", "mixed", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: lib.ID, Recursive: true})
	main := libraryIntegrationItemByPath(t, ordinary.Items, mainPath)
	legacy := libraryIntegrationItemByPath(t, ordinary.Items, legacyPath)
	before := themeScanTestResources(t, ctx, pool, lib.ID)
	if len(before) != 2 {
		t.Fatalf("legacy active themes were not established: %+v", before)
	}
	if _, err := pool.Exec(ctx, `UPDATE libraries SET collection_type='movies' WHERE id=$1`, lib.ID); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("new reservation did not reconcile the hidden owner: %+v", job)
	}
	for _, resource := range themeScanTestResources(t, ctx, pool, lib.ID) {
		if resource.ownerID == legacy.ID && resource.active {
			t.Fatal("a new extra marker left a hidden Movie owner's themes active")
		}
		if resource.ownerID != legacy.ID && !resource.active {
			t.Fatal("extra reservation deactivated an unrelated owner's themes")
		}
	}
	extras := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(extras) != 1 || extras[0].id != legacy.ID || extras[0].ownerID != main.ID || !extras[0].active {
		t.Fatalf("legacy ordinary movie did not become a real stable extra: %+v", extras)
	}
}

func TestExtraScanDirectoryReplacementCannotPublishAChangedSource(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fixture replaces a directory while its descriptor is held")
	}
	fixture := &libraryFixtureProber{}
	var replace, replaced atomic.Bool
	var extraDirectory string
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Clip.mp4" && replace.CompareAndSwap(true, false) {
			if err := os.Rename(extraDirectory, extraDirectory+"-replaced"); err != nil {
				return media.Info{}, err
			}
			if err := os.Mkdir(extraDirectory, 0700); err != nil {
				return media.Info{}, err
			}
			if err := os.WriteFile(filepath.Join(extraDirectory, "Clip.mp4"), []byte("video:replacement"), 0600); err != nil {
				return media.Info{}, err
			}
			replaced.Store(true)
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
	clip := libraryIntegrationFile(t, root, "movies/Film/featurettes/Clip.mp4", "video:accepted")
	extraDirectory = filepath.Dir(clip)
	lib := libraryIntegrationCreate(t, ctx, store, "Changing extra source", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	before := extraScanTestSnapshot(t, ctx, pool, lib.ID)
	replace.Store(true)
	job := themeScanTestForce(t, ctx, store, lib.ID)
	job = themeScanTestWaitStopped(t, ctx, store, job.ID)
	if !replaced.Load() || job.Error == "" {
		t.Fatalf("directory replacement was not exercised as uncertainty: %+v", job)
	}
	if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
		t.Fatal("a changed extra source directory replaced the accepted population")
	}
}
