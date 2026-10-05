//go:build linux

package library

import (
	"crypto/sha256"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

func TestScanInspectionCacheRefreshesChangedBytesAndRetainsInvalidTypes(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, root, "movies/First.mp4", "video:cached-artwork-first")
	secondPath := libraryIntegrationFile(t, root, "movies/Second.mp4", "video:cached-artwork-second")
	directory := filepath.Dir(firstPath)
	posterPath, backdropPath := filepath.Join(directory, "First-poster.png"), filepath.Join(directory, "backdrop.png")
	posterBytes := imageScanTestWrite(t, posterPath, color.White)
	imageScanTestWrite(t, filepath.Join(directory, "Second-poster.png"), color.Black)
	imageScanTestWrite(t, backdropPath, color.White)
	library := libraryIntegrationCreate(t, ctx, store, "Cached artwork content", "movies", directory)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	state.imageInspection = &artwork.InspectionCache{}
	notifications := catalogChangesTestListener(t, store)
	scan := func(item Item, relative string) {
		t.Helper()
		if err := state.scanImages(item.ID, item.Type, relative, false); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		scan(first, "First.mp4")
		scan(second, "Second.mp4")
		assertNoCatalogTestNotification(t, notifications)
	}
	before := imageScanTestList(t, ctx, store, userID, first.ID, "Primary")
	stamp, err := os.Stat(posterPath)
	if err != nil || len(before) != 1 {
		t.Fatalf("cached image fixture is incomplete: %+v %v", before, err)
	}
	posterBytes[len(posterBytes)-1] = 'b'
	if err := os.WriteFile(posterPath, posterBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(posterPath, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	scan(first, "First.mp4")
	after := imageScanTestList(t, ctx, store, userID, first.ID, "Primary")
	if len(after) != 1 || after[0].Tag != fmt.Sprintf("%x", sha256.Sum256(posterBytes)) || after[0].Tag == before[0].Tag ||
		after[0].Size != before[0].Size || !after[0].ModifiedAt.Equal(before[0].ModifiedAt) {
		t.Fatalf("cache reused a source with changed bytes and restored metadata: before=%+v after=%+v", before, after)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: first.ID, LibraryID: library.ID, ParentID: first.ParentID,
	}})
	assertNoCatalogTestNotification(t, notifications)
	if err := os.WriteFile(posterPath, []byte("invalid replacement PNG"), 0600); err != nil {
		t.Fatal(err)
	}
	backdropBytes := imageScanTestWrite(t, backdropPath, color.Black)
	scan(first, "First.mp4")
	if state.warnings == 0 || !reflect.DeepEqual(imageScanTestList(t, ctx, store, userID, first.ID, "Primary"), after) {
		t.Fatal("invalid replacement did not retain the complete previous image type")
	}
	backdrop := imageScanTestList(t, ctx, store, userID, first.ID, "Backdrop")
	if len(backdrop) != 1 || backdrop[0].Tag != fmt.Sprintf("%x", sha256.Sum256(backdropBytes)) {
		t.Fatalf("a retained invalid primary prevented valid cached-content publication: %+v", backdrop)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: first.ID, LibraryID: library.ID, ParentID: first.ParentID,
	}})
	assertNoCatalogTestNotification(t, notifications)
	if len(prober.calls()) != 2 {
		t.Fatal("image cache validation unexpectedly reprobed media")
	}
}
