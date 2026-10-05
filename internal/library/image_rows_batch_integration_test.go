//go:build linux

package library

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanImageRowsBatchWritesAllTypesOnceAndRetainsNoopVersions(t *testing.T) {
	ctx, pool, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Feature.mp4", "video:image-rowset")
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	wantPaths := map[string]string{
		"Primary:0": "Feature-poster.png", "Thumb:0": "Feature-thumb.png", "Banner:0": "Feature-banner.png",
		"Logo:0": "Feature-logo.png", "Art:0": "Feature-clearart.png",
	}
	for index := 0; index < 32; index++ {
		wantPaths[fmt.Sprintf("Backdrop:%d", index)] = fmt.Sprintf("backdrop%02d.png", index)
	}
	for _, name := range wantPaths {
		imageScanTestWrite(t, filepath.Join(directory, name), color.White)
	}
	notifications := catalogChangesTestListener(t, store)
	scan := func() {
		t.Helper()
		state := imageScanTestState(t, ctx, pool, store, library, ".")
		trace.reset()
		if err := state.scanImages(itemID, "Movie", "Feature.mp4", false); err != nil || state.warnings != 0 {
			t.Fatalf("scan complete image rowset: warnings=%d error=%v", state.warnings, err)
		}
	}
	assertOneStatement := func() {
		t.Helper()
		if trace.inserts.Load() != 1 || trace.deletes.Load() != 1 {
			t.Fatalf("one item did not use one image rowset statement: inserts=%d deletes=%d", trace.inserts.Load(), trace.deletes.Load())
		}
	}
	wantChange := []CatalogChange{{Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: library.ID}}
	scan()
	assertOneStatement()
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), wantChange)
	assertNoCatalogTestNotification(t, notifications)
	rows, err := pool.Query(ctx, `SELECT image_type,image_index,relative_path FROM item_images WHERE item_id=$1`, itemID)
	if err != nil {
		t.Fatal(err)
	}
	gotPaths := make(map[string]string)
	for rows.Next() {
		var kind, path string
		var index int
		if err := rows.Scan(&kind, &index, &path); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		gotPaths[fmt.Sprintf("%s:%d", kind, index)] = path
	}
	rows.Close()
	if err := rows.Err(); err != nil || !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("batched type/index/path association changed: got=%v want=%v error=%v", gotPaths, wantPaths, err)
	}
	before := scanImageDiffVersions(t, ctx, pool, itemID)
	scan()
	assertOneStatement()
	if after := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(after, before) || trace.snapshots.Load() != 1 {
		t.Fatalf("no-op rowset rewrote versions or read a changed projection: before=%v after=%v snapshots=%d", before, after, trace.snapshots.Load())
	}
	assertNoCatalogTestNotification(t, notifications)
	// A storage-only correction must update its exact row without notifying a
	// content change or rewriting another member of the same statement.
	if _, err := pool.Exec(ctx, "UPDATE item_images SET file_identity='stale-rowset-identity' WHERE item_id=$1 AND image_type='Thumb'", itemID); err != nil {
		t.Fatal(err)
	}
	stale := scanImageDiffVersions(t, ctx, pool, itemID)
	scan()
	assertOneStatement()
	after := scanImageDiffVersions(t, ctx, pool, itemID)
	if after["Thumb:0"] == stale["Thumb:0"] {
		t.Fatal("rowset omitted a private source-identity correction")
	}
	for key, version := range stale {
		if key != "Thumb:0" && after[key] != version {
			t.Fatalf("private correction rewrote unchanged image %s", key)
		}
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := os.Remove(filepath.Join(directory, "backdrop31.png")); err != nil {
		t.Fatal(err)
	}
	scan()
	assertOneStatement()
	trimmed := scanImageDiffVersions(t, ctx, pool, itemID)
	if len(trimmed) != 36 || trimmed["Backdrop:31"] != "" {
		t.Fatalf("rowset retained the missing highest index: %v", trimmed)
	}
	for key, version := range trimmed {
		if after[key] != version {
			t.Fatalf("deleting the last backdrop rewrote surviving image %s", key)
		}
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), wantChange)
	for _, name := range wantPaths {
		if name != "backdrop31.png" {
			if err := os.Remove(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	scan()
	if trace.inserts.Load() != 0 || trace.deletes.Load() != 1 || len(scanImageDiffVersions(t, ctx, pool, itemID)) != 0 {
		t.Fatalf("empty replacement issued an INSERT or retained rows: inserts=%d deletes=%d", trace.inserts.Load(), trace.deletes.Load())
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), wantChange)
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanImageRowsBatchPreservesInvalidTypesAndRollsBackReplacement(t *testing.T) {
	ctx, pool, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Feature.mp4", "video:image-rowset-rollback")
	poster, backdrop, thumb := filepath.Join(directory, "Feature-poster.png"), filepath.Join(directory, "backdrop.png"), filepath.Join(directory, "Feature-thumb.png")
	for _, path := range []string{poster, backdrop, thumb} {
		imageScanTestWrite(t, path, color.White)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	before := scanImageDiffVersions(t, ctx, pool, itemID)
	notifications := catalogChangesTestListener(t, store)
	if err := os.WriteFile(poster, []byte("invalid rowset poster"), 0600); err != nil {
		t.Fatal(err)
	}
	imageScanTestWrite(t, backdrop, color.Black)
	if err := os.Remove(thumb); err != nil {
		t.Fatal(err)
	}
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	trace.reset()
	if err := state.scanImages(itemID, "Movie", "Feature.mp4", false); err != nil || state.warnings != 1 {
		t.Fatalf("mixed-validity rowset did not preserve its invalid type: warnings=%d error=%v", state.warnings, err)
	}
	after := scanImageDiffVersions(t, ctx, pool, itemID)
	if len(after) != 2 || after["Primary:0"] != before["Primary:0"] || after["Backdrop:0"] == before["Backdrop:0"] || trace.inserts.Load() != 1 {
		t.Fatalf("rowset changed its invalid type or lost an independent replacement: before=%v after=%v inserts=%d", before, after, trace.inserts.Load())
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: library.ID}})
	assertNoCatalogTestNotification(t, notifications)
	imageScanTestWrite(t, poster, color.Black)
	imageScanTestWrite(t, thumb, color.Black)
	if err := os.Remove(backdrop); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_image_rowset_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'reject complete image rowset'; RETURN NEW; END $$;
		CREATE CONSTRAINT TRIGGER reject_image_rowset_commit AFTER INSERT OR UPDATE ON item_images
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_image_rowset_commit()`); err != nil {
		t.Fatal(err)
	}
	state = imageScanTestState(t, ctx, pool, store, library, ".")
	trace.reset()
	err := state.scanImages(itemID, "Movie", "Feature.mp4", false)
	if err == nil || trace.inserts.Load() != 1 || trace.deletes.Load() != 1 || state.warnings != 0 {
		t.Fatalf("deferred image rejection did not reject the whole rowset: inserts=%d deletes=%d warnings=%d error=%v", trace.inserts.Load(), trace.deletes.Load(), state.warnings, err)
	}
	if retained := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(retained, after) {
		t.Fatalf("failed rowset committed a deletion or partial replacement: before=%v after=%v", after, retained)
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := store.CheckOwnership(ctx); err != nil {
		t.Fatalf("failed rowset lost the catalog owner: %v", err)
	}
}

func TestScanImageRowsBatchKeepsSharedFilenameAcrossTypes(t *testing.T) {
	ctx, pool, store, _, directory, trace := scanNewItemImageFixture(t)
	episodes := filepath.Join(filepath.Dir(directory), "episodes")
	libraryIntegrationFile(t, episodes, "Show.S01E01.mp4", "video:shared-image-rowset")
	library := libraryIntegrationCreate(t, ctx, store, "Shared image rowset", "tvshows", episodes)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Show.S01E01.mp4' AND type='Episode'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	imageScanTestWrite(t, filepath.Join(episodes, "Show.S01E01-thumb.png"), color.White)
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	trace.reset()
	if err := state.scanImages(itemID, "Episode", "Show.S01E01.mp4", false); err != nil || state.warnings != 0 {
		t.Fatalf("shared-filename rowset failed: warnings=%d error=%v", state.warnings, err)
	}
	var count int
	var pathsMatch bool
	if err := pool.QueryRow(ctx, `SELECT count(*),bool_and(image_type IN ('Primary','Thumb') AND image_index=0
		AND relative_path='Show.S01E01-thumb.png') FROM item_images WHERE item_id=$1`, itemID).Scan(&count, &pathsMatch); err != nil {
		t.Fatal(err)
	}
	if count != 2 || !pathsMatch || trace.inserts.Load() != 1 {
		t.Fatalf("same source collapsed distinct image keys: count=%d paths_match=%t inserts=%d", count, pathsMatch, trace.inserts.Load())
	}
}
