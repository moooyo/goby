//go:build linux

package library

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func scanImageDiffVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT image_type, image_index, xmin::text FROM item_images WHERE item_id=$1`, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	versions := make(map[string]string)
	for rows.Next() {
		var imageType, version string
		var index int
		if err := rows.Scan(&imageType, &index, &version); err != nil {
			t.Fatal(err)
		}
		versions[fmt.Sprintf("%s:%d", imageType, index)] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return versions
}

func TestScanImageDiffRetainsUnchangedRowsAndUpdatesOnlyChangedImages(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:image-row-diff")
	directory := filepath.Dir(mediaPath)
	poster := filepath.Join(directory, "Film-poster.png")
	imageScanTestWrite(t, poster, color.White)
	for _, name := range []string{"backdrop.png", "backdrop2.png", "backdrop10.png"} {
		imageScanTestWrite(t, filepath.Join(directory, name), color.White)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Image row differences", "movies", directory)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	notifications := catalogChangesTestListener(t, store)
	scan := func() {
		t.Helper()
		state := imageScanTestState(t, ctx, pool, store, library, ".")
		if err := state.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
			t.Fatal(err)
		}
		if state.warnings != 0 {
			t.Fatalf("valid image scan produced %d warnings", state.warnings)
		}
	}
	before := scanImageDiffVersions(t, ctx, pool, item.ID)
	if len(before) != 4 {
		t.Fatalf("image row fixture is incomplete: %v", before)
	}
	for range 2 {
		scan()
		if after := scanImageDiffVersions(t, ctx, pool, item.ID); !reflect.DeepEqual(after, before) {
			t.Fatalf("unchanged images acquired new tuple versions: before=%v after=%v", before, after)
		}
		assertNoCatalogTestNotification(t, notifications)
	}
	imageScanTestWrite(t, poster, color.Black)
	scan()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID,
	}})
	assertNoCatalogTestNotification(t, notifications)
	after := scanImageDiffVersions(t, ctx, pool, item.ID)
	if len(after) != len(before) || after["Primary:0"] == before["Primary:0"] {
		t.Fatalf("changed primary image did not receive a new tuple: before=%v after=%v", before, after)
	}
	for key, version := range before {
		if key != "Primary:0" && after[key] != version {
			t.Fatalf("changing the primary image rewrote %s: before=%v after=%v", key, before, after)
		}
	}
	primary := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(primary) != 1 {
		t.Fatalf("changed primary image is missing: %+v", primary)
	}
	stamp, err := os.Stat(poster)
	if err != nil {
		t.Fatal(err)
	}
	modified := stamp.ModTime().Add(time.Second)
	if err := os.Chtimes(poster, stamp.ModTime(), modified); err != nil {
		t.Fatal(err)
	}
	scan()
	assertNoCatalogTestNotification(t, notifications)
	retimed := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(retimed) != 1 || retimed[0].Tag != primary[0].Tag || !retimed[0].ModifiedAt.Equal(modified.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("metadata-only update lost content or mtime: before=%+v after=%+v", primary, retimed)
	}
	versions := scanImageDiffVersions(t, ctx, pool, item.ID)
	if versions["Primary:0"] == after["Primary:0"] {
		t.Fatal("metadata-only image change did not update the stored tuple")
	}
	for key, version := range after {
		if key != "Primary:0" && versions[key] != version {
			t.Fatalf("metadata-only primary change rewrote %s", key)
		}
	}
}

func TestScanImageDiffRepairsOldRootKeysAndDeletesMissingIndexes(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:old-root-image-diff")
	directory := filepath.Dir(mediaPath)
	imageScanTestWrite(t, filepath.Join(directory, "Film-poster.png"), color.White)
	imageScanTestWrite(t, filepath.Join(directory, "backdrop.png"), color.Black)
	library := libraryIntegrationCreate(t, ctx, store, "Old root image differences", "movies", directory)
	oldRoot := imageStoreTestRoot(t, ctx, pool, store, root, "retired-image-root")
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	if _, err := pool.Exec(ctx, `UPDATE item_images SET root_id=$2 WHERE item_id=$1`, item.ID, oldRoot.id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_images
		(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash,
		file_size, modified_at, width, height, mime_type)
		SELECT item_id, root_id, image_type, 31, relative_path, file_identity, source_hash,
		file_size, modified_at, width, height, mime_type
		FROM item_images WHERE item_id=$1 AND image_type='Backdrop' AND image_index=0`, item.ID); err != nil {
		t.Fatal(err)
	}
	if versions := scanImageDiffVersions(t, ctx, pool, item.ID); len(versions) != 3 {
		t.Fatalf("old root fixture is incomplete: %v", versions)
	}
	notifications := catalogChangesTestListener(t, store)
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	if err := state.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
		t.Fatal(err)
	}
	var count int
	var current bool
	if err := pool.QueryRow(ctx, `SELECT count(*), bool_and(im.root_id=i.root_id AND im.image_index=0)
		FROM item_images im JOIN items i ON i.id=im.item_id WHERE im.item_id=$1`, item.ID).Scan(&count, &current); err != nil || count != 2 || !current {
		t.Fatalf("image diff retained an old root or absent index: count=%d current=%v err=%v", count, current, err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID,
	}})
	assertNoCatalogTestNotification(t, notifications)
}
