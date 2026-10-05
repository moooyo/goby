//go:build linux

package library

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestScanFolderImageAbsenceSkipsNewAndExistingImageTransactions(t *testing.T) {
	ctx, observer, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Group/Feature.mp4", "video:folder-image-absence")
	notifications := catalogChangesTestListener(t, store)
	var folderID, itemID string
	for _, phase := range []string{"cold", "cached", "force"} {
		trace.reset()
		job, err := store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: phase == "force"})
		if err != nil {
			t.Fatal(err)
		}
		job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
		wantAdded, wantUpdated := 0, 0
		if phase == "cold" {
			wantAdded = 1
		} else if phase == "force" {
			wantUpdated = 1
		}
		if job.Error != "" || job.Scanned != 1 || job.Added != wantAdded || job.Updated != wantUpdated {
			t.Fatalf("%s folder scan changed media progress: %+v", phase, job)
		}
		var currentFolder, currentItem string
		if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Group' AND is_folder", library.ID).Scan(&currentFolder); err != nil {
			t.Fatal(err)
		}
		if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Group/Feature.mp4'", library.ID).Scan(&currentItem); err != nil {
			t.Fatal(err)
		}
		if phase == "cold" {
			folderID, itemID = currentFolder, currentItem
			assertScanCatalogChanges(t, notifications, []CatalogChange{
				{Kind: CatalogAdded, ItemID: folderID, LibraryID: library.ID, ParentID: library.ID, IsFolder: true},
				{Kind: CatalogAdded, ItemID: itemID, LibraryID: library.ID, ParentID: folderID},
			})
		} else {
			if currentFolder != folderID || currentItem != itemID {
				t.Fatal("a repeated folder scan replaced catalog identities")
			}
			if phase == "force" {
				assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: folderID}})
			} else {
				assertNoCatalogTestNotification(t, notifications)
			}
		}
		var images int
		if err := observer.QueryRow(ctx, "SELECT count(*) FROM item_images WHERE item_id=ANY($1::text[])", []string{folderID, itemID}).Scan(&images); err != nil {
			t.Fatal(err)
		}
		if images != 0 || trace.snapshots.Load() != 0 || trace.deletes.Load() != 0 || trace.inserts.Load() != 0 {
			t.Fatalf("%s empty folder images entered publication: images=%d snapshots=%d deletes=%d inserts=%d",
				phase, images, trace.snapshots.Load(), trace.deletes.Load(), trace.inserts.Load())
		}
	}
}

func TestScanFolderImageCandidatesRetainInvalidReplacementAndRemoveMissingImage(t *testing.T) {
	ctx, observer, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Group/Feature.mp4", "video:folder-image-candidates")
	poster := filepath.Join(directory, "Group", "poster.png")
	imageScanTestWrite(t, poster, color.NRGBA{R: 190, A: 255})
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var folderID, itemID, originalTag string
	if err := observer.QueryRow(ctx, `SELECT i.id,im.source_hash FROM items i JOIN item_images im ON im.item_id=i.id
		WHERE i.library_id=$1 AND i.relative_path='Group' AND im.image_type='Primary'`, library.ID).Scan(&folderID, &originalTag); err != nil {
		t.Fatal(err)
	}
	if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Group/Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if err := os.WriteFile(poster, []byte("invalid folder image replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var retainedTag string
	if err := observer.QueryRow(ctx, "SELECT source_hash FROM item_images WHERE item_id=$1 AND image_type='Primary'", folderID).Scan(&retainedTag); err != nil {
		t.Fatal(err)
	}
	if job.Error == "" || retainedTag != originalTag {
		t.Fatalf("invalid folder candidate lost its warning or old image: job=%+v original=%s retained=%s", job, originalTag, retainedTag)
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := os.Remove(poster); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	job = libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var images int
	if err := observer.QueryRow(ctx, "SELECT count(*) FROM item_images WHERE item_id=$1", folderID).Scan(&images); err != nil {
		t.Fatal(err)
	}
	if job.Error != "" || images != 0 || trace.deletes.Load() == 0 {
		t.Fatalf("complete folder absence did not remove the old image: job=%+v images=%d deletes=%d", job, images, trace.deletes.Load())
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{
		{Kind: CatalogUpdated, ItemID: folderID, LibraryID: library.ID, ParentID: library.ID, IsFolder: true},
		{Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: folderID},
	})
}

func TestScanFolderImageAbsenceIncludesRowsFromOldRoots(t *testing.T) {
	ctx, observer, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Group/Feature.mp4", "video:folder-stale-root-image")
	poster := filepath.Join(directory, "Group", "poster.png")
	imageScanTestWrite(t, poster, color.NRGBA{B: 210, A: 255})
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var folderID, itemID string
	if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Group' AND is_folder", library.ID).Scan(&folderID); err != nil {
		t.Fatal(err)
	}
	if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Group/Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	oldDirectory := filepath.Join(filepath.Dir(directory), "old-root")
	if err := os.Mkdir(oldDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	oldLibrary := libraryIntegrationCreate(t, ctx, store, "Retained old image root", "movies", oldDirectory)
	var oldRootID string
	if err := observer.QueryRow(ctx, "SELECT id FROM library_roots WHERE library_id=$1", oldLibrary.ID).Scan(&oldRootID); err != nil {
		t.Fatal(err)
	}
	if tag, err := observer.Exec(ctx, "UPDATE item_images SET root_id=$2 WHERE item_id=$1", folderID, oldRootID); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("prepare one stale folder image: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if err := os.Remove(poster); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	trace.reset()
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var images int
	if err := observer.QueryRow(ctx, "SELECT count(*) FROM item_images WHERE item_id=$1", folderID).Scan(&images); err != nil {
		t.Fatal(err)
	}
	if job.Error != "" || images != 0 || trace.deletes.Load() == 0 {
		t.Fatalf("an invisible old-root row was mistaken for no image: job=%+v images=%d deletes=%d", job, images, trace.deletes.Load())
	}
	// The stale folder image was already absent from its public projection.
	// Only the child's formerly visible image removal invalidates the catalog.
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: folderID}})
}
