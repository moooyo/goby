//go:build linux

package library

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestStorePreservesNFOWhenSidecarBecomesNamedPipe(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Film.mp4", "video:film")
	nfoPath := libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo",
		`<movie><title>Saved NFO Title</title><plot>Preserved local synopsis.</plot></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(mediaPath))
	initialJob := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if initialJob.Error != "" {
		t.Fatalf("valid NFO produced a scan warning: %+v", initialJob)
	}
	query := Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}
	initialItems := libraryIntegrationQuery(t, ctx, store, query)
	if initialItems.TotalRecordCount != 1 || len(initialItems.Items) != 1 {
		t.Fatalf("initial movie catalog = %+v, want one movie", initialItems)
	}
	listed := libraryIntegrationItemByPath(t, initialItems.Items, mediaPath)
	original, err := store.GetItem(ctx, userID, listed.ID)
	if err != nil {
		t.Fatalf("read movie with valid NFO: %v", err)
	}
	if original.Name != "Saved NFO Title" || original.Overview != "Preserved local synopsis." ||
		original.Metadata == nil || original.Metadata.Kind != "movie" ||
		original.Metadata.Name != original.Name || original.Metadata.Overview != original.Overview {
		t.Fatalf("valid NFO metadata was not persisted: %+v", original)
	}
	probeCount := len(prober.calls())
	if probeCount != 1 {
		t.Fatalf("initial media probe count = %d, want 1", probeCount)
	}
	if err := os.Remove(nfoPath); err != nil {
		t.Fatalf("remove regular NFO fixture: %v", err)
	}
	if err := syscall.Mkfifo(nfoPath, 0600); err != nil {
		t.Fatalf("replace NFO fixture with a named pipe: %v", err)
	}
	t.Cleanup(func() {
		// Unblock an accidental blocking reader before the store cleanup runs.
		if descriptor, err := syscall.Open(nfoPath, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = syscall.Close(descriptor)
		}
	})
	rescanned := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if rescanned.Error == "" {
		t.Error("named pipe NFO did not produce a scan warning")
	}
	retainedItems := libraryIntegrationQuery(t, ctx, store, query)
	if retainedItems.TotalRecordCount != 1 || len(retainedItems.Items) != 1 || retainedItems.Items[0].ID != original.ID {
		t.Fatalf("named pipe NFO changed catalog identity: %+v", retainedItems)
	}
	retained, err := store.GetItem(ctx, userID, original.ID)
	if err != nil {
		t.Fatalf("read movie after named pipe NFO rescan: %v", err)
	}
	if !reflect.DeepEqual(retained, original) {
		t.Errorf("named pipe NFO replaced valid catalog metadata: before = %+v, after = %+v", original, retained)
	}
	if calls := len(prober.calls()); calls != probeCount {
		t.Errorf("named pipe NFO repeated media probing: calls = %d, want %d", calls, probeCount)
	}
}
