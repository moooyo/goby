package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanVirtualFolderCacheRetainsLatestInputAndSkipsRepeatedWrites(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "shows/Film.mp4", "video:virtual-folder-cache")
	library := libraryIntegrationCreate(t, ctx, store, "Virtual folders", "tvshows", filepath.Dir(path))
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	const relative = "//series/show"
	publish := func(name string) string {
		t.Helper()
		id, err := state.folder(relative, "", name, "Series", library.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		var accepted string
		if err := pool.QueryRow(ctx, "SELECT automatic->>'Name' FROM item_metadata_state WHERE item_id=$1", id).Scan(&accepted); err != nil || accepted != name {
			t.Fatalf("virtual folder did not publish the latest automatic input: name=%q error=%v", accepted, err)
		}
		return id
	}
	id := publish("Show")
	version := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT i.xmin::text || ':' || ms.xmin::text FROM items i
			JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id=$1`, id).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	before := version()
	if publish("Show") != id || version() != before {
		t.Fatal("identical virtual folder input rewrote the accepted catalog")
	}
	// The normalized relative path may receive differently cased source titles.
	// A previous title must not survive after another input has overwritten it.
	if publish("SHOW") != id || publish("Show") != id {
		t.Fatal("changed virtual input replaced the stable folder identity")
	}
	before = version()
	if publish("Show") != id || version() != before {
		t.Fatal("latest matching input did not use the scan-local cache")
	}
	info, err := state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	state.directoryIdentities = map[string]os.FileInfo{".": info}
	libraryIntegrationFile(t, root, "shows/tvshow.nfo", "<tvshow><title>NFO Show</title></tvshow>")
	if sourcedID, err := state.folder(relative, "", "Show", "Series", library.ID, 0, "."); err != nil || sourcedID != id {
		t.Fatalf("source-backed virtual folder visit failed: id=%q error=%v", sourcedID, err)
	}
	var sourcedName string
	if err := pool.QueryRow(ctx, "SELECT automatic->>'Name' FROM item_metadata_state WHERE item_id=$1", id).Scan(&sourcedName); err != nil || sourcedName != "NFO Show" {
		t.Fatalf("source-backed metadata was not accepted: name=%q error=%v", sourcedName, err)
	}
	if publish("Show") != id {
		t.Fatal("a source-backed visit left a stale virtual publication cached")
	}
	if _, err := state.folder(relative, "", "Show", "Series", library.ID, 7); err != nil {
		t.Fatal(err)
	}
	var index int
	if err := pool.QueryRow(ctx, "SELECT index_number FROM items WHERE id=$1", id).Scan(&index); err != nil || index != 7 {
		t.Fatalf("changed hierarchy input was hidden by the cache: index=%d error=%v", index, err)
	}
	if publish("Show") != id {
		t.Fatal("restoring hierarchy input changed identity")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	state.task.ctx = cancelCtx
	if _, err := state.folder(relative, "", "Show", "Series", library.ID, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached folder ignored cancellation: %v", err)
	}
	state.task.ctx = ctx
	// A cache belongs to one walk, so the next scan publishes its own inputs.
	before = version()
	next := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	if idAgain, err := next.folder(relative, "", "Show", "Series", library.ID, 0); err != nil || idAgain != id || version() == before {
		t.Fatalf("new scan failed to revisit its virtual folder: id=%q error=%v", idAgain, err)
	}
}

func TestScanVirtualFolderCacheBoundsPathsAndOverwritesLatestInput(t *testing.T) {
	state := &scanState{}
	for index := 0; index < maxScannedVirtualFolders+100; index++ {
		state.cacheVirtualFolder(fmt.Sprintf("//series/%d", index), scannedVirtualFolder{name: "Show"}, fmt.Sprint(index))
	}
	if len(state.virtualFolders) != maxScannedVirtualFolders {
		t.Fatal("virtual folder cache exceeded its path budget")
	}
	state.cacheVirtualFolder("//series/0", scannedVirtualFolder{name: "Changed"}, "0")
	if state.virtualFolders["//series/0"].name != "Changed" || len(state.virtualFolders) != maxScannedVirtualFolders {
		t.Fatal("a full cache retained an older input for an existing path")
	}
}
