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
)

func TestStoreImageInspectionCacheKeepsFreshnessAcrossScans(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, root, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:store-image-cache")
	directory := filepath.Dir(mediaPath)
	posterPath, backdropPath := filepath.Join(directory, "Film-poster.png"), filepath.Join(directory, "backdrop.png")
	posterBytes := imageScanTestWrite(t, posterPath, color.White)
	imageScanTestWrite(t, backdropPath, color.Black)
	library := libraryIntegrationCreate(t, ctx, store, "Store image cache", "movies", directory)
	initial := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	scanPerformanceWaitWorkerRetired(t, ctx, store, initial.ID)
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, mediaPath)
	notifications := catalogChangesTestListener(t, store)
	want := []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID}}
	lastJob := initial.ID
	scan := func(warning bool) {
		t.Helper()
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
		if job.ID == lastJob || (job.Error != "") != warning || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != 1 {
			t.Fatalf("independent image rescan changed media or warning behavior: %+v probes=%d", job, len(prober.calls()))
		}
		lastJob = job.ID
	}
	for range 2 {
		scan(false)
		assertNoCatalogTestNotification(t, notifications)
	}
	before := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	stamp, err := os.Stat(posterPath)
	if err != nil || len(before) != 1 {
		t.Fatalf("stored primary fixture is incomplete: %+v %v", before, err)
	}
	posterBytes[len(posterBytes)-1] = 'b'
	if err := os.WriteFile(posterPath, posterBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(posterPath, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	scan(false)
	after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(after) != 1 || after[0].Tag != fmt.Sprintf("%x", sha256.Sum256(posterBytes)) || after[0].Tag == before[0].Tag ||
		after[0].Size != before[0].Size || !after[0].ModifiedAt.Equal(before[0].ModifiedAt) {
		t.Fatalf("Store cache trusted restored metadata across scans: before=%+v after=%+v", before, after)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	if err := os.WriteFile(posterPath, []byte("invalid PNG replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	backdropBytes := imageScanTestWrite(t, backdropPath, color.White)
	scan(true)
	if retained := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); !reflect.DeepEqual(retained, after) {
		t.Fatalf("a later invalid candidate replaced retained primary facts: %+v", retained)
	}
	backdrops := imageScanTestList(t, ctx, store, userID, item.ID, "Backdrop")
	if len(backdrops) != 1 || backdrops[0].Tag != fmt.Sprintf("%x", sha256.Sum256(backdropBytes)) {
		t.Fatalf("a valid previously cached payload was not published for its current file: %+v", backdrops)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
	if err := os.Remove(posterPath); err != nil {
		t.Fatal(err)
	}
	scan(false)
	if remaining := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); len(remaining) != 0 {
		t.Fatalf("Store cache kept an authoritatively absent source: %+v", remaining)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
	assertNoCatalogTestNotification(t, notifications)
}
