package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestScanCatalogChangesAddAndCachePreserveManualState(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:catalog-cache")
	created := libraryIntegrationCreate(t, ctx, store, "Scan notifications", "movies", filepath.Join(root, "movies"))
	notifications := catalogChangesTestListener(t, store)
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	folder := nfoCatalogItem(t, ctx, store, userID, created.ID, filepath.Dir(path))
	item := nfoCatalogItem(t, ctx, store, userID, created.ID, path)
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogAdded, ItemID: folder.ID, LibraryID: created.ID, ParentID: created.ID, IsFolder: true},
		{Kind: CatalogAdded, ItemID: item.ID, LibraryID: created.ID, ParentID: folder.ID},
	})
	actor := metadataEditTestActor(t, ctx, pool, "scan-catalog-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual film"`), "Genres": json.RawMessage(`["Manual genre"]`)}, []string{"Overview"})
	_ = nextCatalogTestNotification(t, notifications)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 3, PlaybackPositionTicks: 70})
	before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	for visit := 0; visit < 2; visit++ {
		job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
		if job.Error != "" || job.Added != 0 || job.Updated != 0 {
			t.Fatalf("cached scan did not remain unchanged: %+v", job)
		}
		assertNoCatalogTestNotification(t, notifications)
		if metadataEditTestSnapshot(t, ctx, pool, item.ID) != before || forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
			t.Fatal("cached notifications changed manual metadata or user data")
		}
	}
	if len(prober.calls()) != 1 || metadataEditTestDetail(t, ctx, store, actor, item.ID).Revision != detail.Revision {
		t.Fatal("cached scanning reprobed media or changed the administrator revision")
	}
	libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:catalog-source-with-new-facts")
	job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	if job.Error != "" || job.Updated != 1 {
		t.Fatalf("changed media did not update once: %+v", job)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: created.ID, ParentID: folder.ID}})
	after := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if after.Effective.Name != "Manual film" || !reflect.DeepEqual(after.Overrides, detail.Overrides) ||
		!reflect.DeepEqual(after.LockedValues, detail.LockedValues) || forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
		t.Fatal("a notified media update lost the existing administrator or user state")
	}
}

func TestScanCatalogChangesFolderComparesEffectiveMetadata(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "television/Show/Season 01/Show.S01E01.mp4", "video:folder-notifications")
	document := `<tvshow><title>Series title</title><plot>Original overview.</plot><year>2020</year><genre>Original genre</genre></tvshow>`
	libraryIntegrationFile(t, root, "television/Show/tvshow.nfo", document)
	created := libraryIntegrationCreate(t, ctx, store, "Folder notifications", "tvshows", filepath.Join(root, "television"))
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	series := nfoCatalogItem(t, ctx, store, userID, created.ID, filepath.Dir(filepath.Dir(path)))
	notifications := catalogChangesTestListener(t, store)
	document = `<tvshow><title>Series title</title><plot>Original overview.</plot><year>2021</year><genre>Changed genre</genre></tvshow>`
	libraryIntegrationFile(t, root, "television/Show/tvshow.nfo", document)
	if job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: series.ID, LibraryID: created.ID, ParentID: created.ID, IsFolder: true}})
	actor := metadataEditTestActor(t, ctx, pool, "scan-folder-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, nil, []string{"Overview"})
	_ = nextCatalogTestNotification(t, notifications)
	libraryIntegrationFile(t, root, "television/Show/tvshow.nfo",
		`<tvshow><title>Series title</title><plot>Hidden source overview.</plot><year>2021</year><genre>Changed genre</genre></tvshow>`)
	if job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	assertNoCatalogTestNotification(t, notifications)
	after := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	if after.Automatic.Overview != "Hidden source overview." || after.Effective.Overview != "Original overview." ||
		!reflect.DeepEqual(after.LockedValues, detail.LockedValues) {
		t.Fatalf("the quiet scan did not preserve the effective directory lock: %+v", after)
	}
	if job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanCatalogChangesSuccessfulForceProbeInvalidatesIdenticalMedia(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		if fail.Load() {
			return media.Info{}, errors.New("forced catalog notification fixture failure")
		}
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:identical-force")
	created := libraryIntegrationCreate(t, ctx, store, "Forced notifications", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, created.ID, path)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 2})
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	notifications := catalogChangesTestListener(t, store)
	for iteration := 0; iteration < 2; iteration++ {
		job := forceProbeTestScan(t, ctx, store, created.ID, "Completed")
		if job.Error != "" || job.Updated != 1 || job.Added != 0 {
			t.Fatalf("successful explicit refresh was not accepted: %+v", job)
		}
		assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: created.ID, ParentID: item.ParentID}})
		after := nfoCatalogItem(t, ctx, store, userID, created.ID, path)
		if !reflect.DeepEqual(after.Media, item.Media) || forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
			t.Fatal("identical explicit refresh changed accepted media or user data")
		}
	}
	fail.Store(true)
	if job := forceProbeTestScan(t, ctx, store, created.ID, "Completed"); job.Error == "" || job.Updated != 0 {
		t.Fatalf("failed explicit probe was reported as an accepted refresh: %+v", job)
	}
	assertNoCatalogTestNotification(t, notifications)
	if calls.Load() != 4 {
		t.Fatalf("unexpected force probe call count: %d", calls.Load())
	}
}

func TestScanCatalogChangesMoveRetainsIdentityStateAndBothParents(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cross-root stable file identity requires Linux")
	}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	original := libraryIntegrationFile(t, root, "first/Old/Film.mp4", "video:move-notification")
	second := filepath.Join(root, "second")
	newParentPath := filepath.Join(second, "New")
	if err := os.MkdirAll(newParentPath, 0700); err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateLibrary(ctx, "Moved notifications", "movies", []string{filepath.Join(root, "first"), second})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, created.ID, original)
	newParent := nfoCatalogItem(t, ctx, store, userID, created.ID, newParentPath)
	actor := metadataEditTestActor(t, ctx, pool, "scan-move-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual moved title"`)}, []string{"Overview"})
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 4, PlaybackPositionTicks: 120})
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	notifications := catalogChangesTestListener(t, store)
	destination := filepath.Join(newParentPath, "Renamed.mp4")
	if err := os.Rename(original, destination); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, created.ID, "Completed"); job.Error != "" || job.Added != 0 || job.Updated != 1 {
		t.Fatalf("same-library move changed its identity or failed: %+v", job)
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: created.ID,
		ParentID: newParent.ID, PreviousParentID: item.ParentID}})
	after := nfoCatalogItem(t, ctx, store, userID, created.ID, destination)
	current := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if after.ID != item.ID || after.ParentID != newParent.ID || current.Effective.Name != "Manual moved title" ||
		!reflect.DeepEqual(current.Overrides, detail.Overrides) || !reflect.DeepEqual(current.LockedValues, detail.LockedValues) ||
		forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
		t.Fatal("move notification did not preserve the catalog identity and saved state")
	}
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanCatalogChangesCommittedFileSurvivesLaterFailureOrCancellation(t *testing.T) {
	for _, ending := range []string{"Failed", "Cancelled"} {
		t.Run(ending, func(t *testing.T) {
			var block atomic.Bool
			entered, release := make(chan struct{}, 1), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			fixture := &libraryFixtureProber{}
			prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
				if filepath.Base(file.Name()) == "Second.mp4" && block.Load() {
					select {
					case entered <- struct{}{}:
					default:
					}
					select {
					case <-release:
					case <-ctx.Done():
						return media.Info{}, ctx.Err()
					}
				}
				return fixture.ProbeFile(ctx, file)
			})
			ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
			firstPath := libraryIntegrationFile(t, root, "first/First.mp4", "video:first-before")
			secondPath := libraryIntegrationFile(t, root, "second/Second.mp4", "video:second-retained")
			created, err := store.CreateLibrary(ctx, "Partial scan notifications", "movies", []string{filepath.Dir(firstPath), filepath.Dir(secondPath)})
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
			first := nfoCatalogItem(t, ctx, store, userID, created.ID, firstPath)
			second := nfoCatalogItem(t, ctx, store, userID, created.ID, secondPath)
			beforeSecond := metadataEditTestSnapshot(t, ctx, pool, second.ID)
			if ending == "Failed" {
				if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_second_scan_commit() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN IF NEW.relative_path = 'Second.mp4' THEN RAISE EXCEPTION 'second scan commit rejected'; END IF; RETURN NEW; END; $$;
					CREATE CONSTRAINT TRIGGER reject_second_scan_commit AFTER UPDATE ON items
					DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_second_scan_commit()`); err != nil {
					t.Fatal(err)
				}
			}
			notifications := catalogChangesTestListener(t, store)
			libraryIntegrationFile(t, root, "first/First.mp4", "video:first-committed-with-new-facts")
			block.Store(true)
			job, err := store.StartScanWithOptions(ctx, created.ID, ScanOptions{ForceProbe: true})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(15 * time.Second):
				t.Fatal("the second root did not reach its controlled probe")
			}
			assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: first.ID, LibraryID: created.ID, ParentID: created.ID}})
			visible := nfoCatalogItem(t, ctx, store, userID, created.ID, firstPath)
			if visible.Media == nil || first.Media == nil || visible.Media.Size == first.Media.Size {
				t.Fatal("notification did not expose the first committed file while the second root remained blocked")
			}
			if ending == "Cancelled" {
				if err := store.CancelJob(ctx, job.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				releaseOnce.Do(func() { close(release) })
			}
			finished := libraryIntegrationWaitJob(t, ctx, store, job.ID, ending)
			if finished.Updated != 1 || finished.Added != 0 {
				t.Fatalf("partial scan lost its committed result: %+v", finished)
			}
			assertNoCatalogTestNotification(t, notifications)
			if metadataEditTestSnapshot(t, ctx, pool, second.ID) != beforeSecond {
				t.Fatal("a failed or cancelled second file changed its previous catalog snapshot")
			}
		})
	}
}

func TestScanCatalogChangesFileDirectoryReclassificationRetainsID(t *testing.T) {
	ctx, _, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := filepath.Join(root, "movies", "Replacement.mp4")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	created := libraryIntegrationCreate(t, ctx, store, "Reclassified notifications", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, userID, created.ID, path)
	notifications := catalogChangesTestListener(t, store)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video:replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: before.ID, LibraryID: created.ID, ParentID: created.ID}})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: before.ID, LibraryID: created.ID, ParentID: created.ID, IsFolder: true}})
	if after := nfoCatalogItem(t, ctx, store, userID, created.ID, path); after.ID != before.ID || !after.IsFolder {
		t.Fatal("file/directory notification changed the retained identity")
	}
}

func TestScanCatalogChangesCachedMusicAlbumUsesAcceptedEffectiveSource(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("Track.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Embedded track", Album: "Embedded album", Artist: "Track artist", AlbumArtist: "Album artist"}, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Physical Album/Track.flac", "audio:cached-album")
	created := libraryIntegrationCreate(t, ctx, store, "Music notifications", "music", filepath.Join(root, "music"))
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	album := nfoCatalogItem(t, ctx, store, userID, created.ID, filepath.Dir(path))
	if album.Name != "Embedded album" {
		t.Fatalf("the accepted album source was not established: %q", album.Name)
	}
	actor := metadataEditTestActor(t, ctx, pool, "scan-album-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual album"`)}, []string{"Name"})
	beforeSource := metadataMusicScanSource(t, ctx, pool, album.ID)
	notifications := catalogChangesTestListener(t, store)
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
	after := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	if after.Effective.Name != "Manual album" || !reflect.DeepEqual(after.LockedValues, detail.LockedValues) ||
		!reflect.DeepEqual(metadataMusicScanSource(t, ctx, pool, album.ID), beforeSource) || len(prober.calls()) != 1 {
		t.Fatal("cached directory notification comparison changed the accepted album source or controls")
	}
}

func assertScanCatalogChanges(t *testing.T, notifications <-chan CatalogNotification, want []CatalogChange) {
	t.Helper()
	var got []CatalogChange
	for {
		select {
		case notification := <-notifications:
			if notification.Resync {
				t.Fatal("a bounded ordinary scan required an unexpected resynchronization")
			}
			got = append(got, notification.Changes...)
		default:
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("scanned catalog changes = %+v, want %+v", got, want)
			}
			return
		}
	}
}
