package library

import (
	"crypto/sha256"
	"encoding/hex"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestImageCatalogChangesTrackCachedMediaArtworkAndIgnoreInspectionTime(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, root, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:image-notification-cache")
	directory := filepath.Dir(mediaPath)
	library := libraryIntegrationCreate(t, ctx, store, "Image notifications", "movies", directory)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	notifications := catalogChangesTestListener(t, store)
	want := []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID}}
	scan := func() {
		t.Helper()
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		if job.Error != "" || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != 1 {
			t.Fatalf("artwork inspection changed or reprobed cached media: job=%+v calls=%d", job, len(prober.calls()))
		}
	}
	poster := filepath.Join(directory, "Film-poster.png")
	contents := imageScanTestWrite(t, poster, color.NRGBA{R: 255, A: 255})
	scan()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	before := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(before) != 1 || before[0].Width != 4 || before[0].Height != 2 || before[0].Filename != "Film-poster.png" {
		t.Fatalf("the added public image projection is incomplete: %+v", before)
	}
	info, err := os.Stat(poster)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve decoded pixels, file size, and mtime while changing the content
	// tag. The media probe must remain cached throughout this independent write.
	contents[len(contents)-1] = 'b'
	if err := os.WriteFile(poster, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(poster, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	scan()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	digest := sha256.Sum256(contents)
	if len(after) != 1 || after[0].Tag == before[0].Tag || after[0].Tag != hex.EncodeToString(digest[:]) ||
		after[0].Size != before[0].Size || !after[0].ModifiedAt.Equal(before[0].ModifiedAt) {
		t.Fatalf("the notified hash-only change did not preserve its controlled stat values: before=%+v after=%+v", before, after)
	}
	for visit := 0; visit < 2; visit++ {
		scan()
		assertNoCatalogTestNotification(t, notifications)
	}
	if err := os.Chtimes(poster, info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	scan()
	assertNoCatalogTestNotification(t, notifications)
	retimed := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(retimed) != 1 || retimed[0].Tag != after[0].Tag || retimed[0].ModifiedAt.Equal(after[0].ModifiedAt) {
		t.Fatalf("mtime-only inspection did not retain the same public content tag: %+v", retimed)
	}
	cover := filepath.Join(directory, "Film-cover.png")
	if err := os.Rename(poster, cover); err != nil {
		t.Fatal(err)
	}
	scan()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	renamed := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(renamed) != 1 || renamed[0].Filename != "Film-cover.png" || renamed[0].Path != cover || renamed[0].Tag != after[0].Tag {
		t.Fatalf("the image pathname update lost its content identity: %+v", renamed)
	}
	if err := os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	scan()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	if remaining := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); len(remaining) != 0 {
		t.Fatalf("the notified image removal retained a public image: %+v", remaining)
	}
	scan()
	assertNoCatalogTestNotification(t, notifications)
}

func TestImageCatalogChangesRetainInvalidAndUnstableImageSnapshots(t *testing.T) {
	for _, scenario := range []string{"invalid image", "changed listing", "directory replacement"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "directory replacement" && runtime.GOOS != "linux" {
				t.Skip("the fixture renames a directory while its descriptor is held")
			}
			ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			mediaPath := libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:retained-image-notification")
			directory := filepath.Dir(mediaPath)
			poster := filepath.Join(directory, "Film-poster.png")
			imageScanTestWrite(t, poster, color.White)
			library := libraryIntegrationCreate(t, ctx, store, "Retained image notifications", "movies", filepath.Join(root, "movies"))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
			before := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
			if len(before) != 1 {
				t.Fatalf("the retained image fixture is incomplete: %+v", before)
			}
			notifications := catalogChangesTestListener(t, store)
			state := imageScanTestState(t, ctx, pool, store, library, "Group")
			if err := state.scanImages(item.ID, item.Type, "Group/Film.mp4", false); err != nil {
				t.Fatal(err)
			}
			assertNoCatalogTestNotification(t, notifications)
			switch scenario {
			case "invalid image":
				if err := os.WriteFile(poster, []byte("invalid replacement image"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed listing":
				if err := os.Remove(poster); err != nil {
					t.Fatal(err)
				}
			case "directory replacement":
				held, err := state.opened.OpenRoot("Group")
				if err != nil {
					t.Fatal(err)
				}
				defer held.Close()
				if err := os.Rename(directory, directory+"-moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := state.scanImages(item.ID, item.Type, "Group/Film.mp4", false); err != nil {
				t.Fatal(err)
			}
			if state.warnings == 0 {
				t.Fatal("an invalid or unstable image source produced no warning")
			}
			assertNoCatalogTestNotification(t, notifications)
			if after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); !reflect.DeepEqual(after, before) {
				t.Fatalf("an unverified image change replaced the retained public projection: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestImageCatalogChangesFailedCommitRetainsProjectionAndCanRecover(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:rollback-image-notification")
	directory := filepath.Dir(mediaPath)
	poster := filepath.Join(directory, "Film-poster.png")
	imageScanTestWrite(t, poster, color.White)
	library := libraryIntegrationCreate(t, ctx, store, "Image rollback notifications", "movies", directory)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	before := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(before) != 1 {
		t.Fatalf("the rollback image fixture is incomplete: %+v", before)
	}
	notifications := catalogChangesTestListener(t, store)
	imageScanTestWrite(t, poster, color.Black)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_image_notification_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'image notification commit rejection'; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER reject_image_notification_commit AFTER INSERT ON item_images
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_image_notification_commit()`); err != nil {
		t.Fatal(err)
	}
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	if err := state.scanImages(item.ID, item.Type, "Film.mp4", false); err == nil {
		t.Fatal("the deferred image commit failure was ignored")
	}
	assertNoCatalogTestNotification(t, notifications)
	if after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed image commit changed the accepted projection: before=%+v after=%+v", before, after)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_image_notification_commit ON item_images;
		DROP FUNCTION reject_image_notification_commit()`); err != nil {
		t.Fatal(err)
	}
	if err := state.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
		t.Fatalf("a failed image notification retained the transaction or ownership mutex: %v", err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated,
		ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID}})
	assertNoCatalogTestNotification(t, notifications)
	after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(after) != 1 || after[0].Tag == before[0].Tag {
		t.Fatalf("successful image recovery did not expose the committed replacement: %+v", after)
	}
}

func TestImageCatalogChangesUseTheCurrentFolderOwnerScope(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:folder-image-notification")
	library := libraryIntegrationCreate(t, ctx, store, "Folder image notifications", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	owner := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(mediaPath))
	notifications := catalogChangesTestListener(t, store)
	imageScanTestWrite(t, filepath.Join(filepath.Dir(mediaPath), "poster.png"), color.White)
	state := imageScanTestState(t, ctx, pool, store, library, "Group")
	if err := state.scanImages(owner.ID, owner.Type, "Group", true); err != nil {
		t.Fatal(err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated,
		ItemID: owner.ID, LibraryID: library.ID, ParentID: owner.ParentID, IsFolder: true}})
	assertNoCatalogTestNotification(t, notifications)
	if images := imageScanTestList(t, ctx, store, userID, owner.ID, "Primary"); len(images) != 1 {
		t.Fatalf("the notified folder has no committed primary image: %+v", images)
	}
}
