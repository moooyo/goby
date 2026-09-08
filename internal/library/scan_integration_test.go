package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type scanProberFunc func(context.Context, *os.File) (media.Info, error)

func (prober scanProberFunc) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	return prober(ctx, file)
}

func TestScanProbeFailureRetainsPreviousMetadataAndOmitsUnknownFiles(t *testing.T) {
	var fail atomic.Bool
	fixture := &libraryFixtureProber{}
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if fail.Load() {
			return media.Info{}, errors.New("probe fixture failure")
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "movies/Existing.mp4", "video:original")
	libraryIntegrationFile(t, allowedRoot, "movies/.Hidden.mp4", "video:hidden")
	libraryIntegrationFile(t, allowedRoot, "movies/Download.mp4.part", "video:temporary")
	libraryIntegrationFile(t, allowedRoot, "movies/Notes.txt", "unrelated document")
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(firstPath))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	query := Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}
	before := libraryIntegrationQuery(t, ctx, store, query)
	if len(before.Items) != 1 || len(fixture.calls()) != 1 {
		t.Fatalf("hidden, temporary, or unrelated files were probed: items=%+v calls=%q", before.Items, fixture.calls())
	}
	fail.Store(true)
	libraryIntegrationFile(t, allowedRoot, "movies/Existing.mp4", "video:modified-and-no-longer-probeable")
	libraryIntegrationFile(t, allowedRoot, "movies/Unknown.mkv", "video:unprobeable-new-file")
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" || job.Scanned != 2 || job.Added != 0 || job.Updated != 0 {
		t.Errorf("probe failures were not recorded truthfully: %+v", job)
	}
	after := libraryIntegrationQuery(t, ctx, store, query)
	if len(after.Items) != 1 || after.Items[0].ID != before.Items[0].ID || after.Items[0].Media == nil || after.Items[0].Media.Size != before.Items[0].Media.Size {
		t.Fatalf("failed probing changed or fabricated catalog metadata: %+v", after.Items)
	}
}

func TestScanRejectsRegisteredRootReplacedBySiblingSymlink(t *testing.T) {
	fixture := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, fixture)
	mediaPath := libraryIntegrationFile(t, allowedRoot, "movies/Owned.mp4", "video:owned")
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(mediaPath))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	outside := filepath.Join(allowedRoot, "private-library")
	libraryIntegrationFile(t, outside, "Private.mp4", "video:private")
	original := filepath.Dir(mediaPath)
	backup := original + "-owned-backup"
	if err := os.Rename(original, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(original)
		_ = os.Rename(backup, original)
	})
	if err := os.Symlink(outside, original); err != nil {
		t.Skipf("filesystem cannot create symlinks: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Failed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if len(items.Items) != 1 || items.Items[0].Path != mediaPath || len(fixture.calls()) != 1 {
		t.Errorf("replaced root escaped its approved directory: items=%+v calls=%q", items.Items, fixture.calls())
	}
}

func TestScanDoesNotCacheMetadataFromAFileModifiedDuringProbe(t *testing.T) {
	var mediaPath string
	prober := scanProberFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		before, err := file.Stat()
		if err != nil {
			return media.Info{}, err
		}
		// The scanner's descriptor is read-only, so the fixture simulates another
		// writer by changing the source timestamp through its original filename.
		if err := os.Chtimes(mediaPath, before.ModTime().Add(time.Second), before.ModTime().Add(time.Second)); err != nil {
			return media.Info{}, err
		}
		return libraryMediaFixture([]byte("video:unstable")), nil
	})
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	mediaPath = libraryIntegrationFile(t, allowedRoot, "movies/Changing.mp4", "video:unstable")
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(mediaPath))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" || job.Added != 0 {
		t.Errorf("a changing file was accepted without a warning: %+v", job)
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if len(items.Items) != 0 {
		t.Errorf("unstable file metadata entered the catalog: %+v", items.Items)
	}
}

func TestScanHandlesAPathChangingBetweenDirectoryAndMediaFile(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := filepath.Join(allowedRoot, "movies", "Replacement.mp4")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(path))
	query := Query{UserID: userID, ParentID: library.ID, Recursive: true}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	before := libraryIntegrationQuery(t, ctx, store, query)
	if len(before.Items) != 1 || !before.Items[0].IsFolder {
		t.Fatalf("initial directory was not cataloged correctly: %+v", before.Items)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video:replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	asFile := libraryIntegrationQuery(t, ctx, store, query)
	if len(asFile.Items) != 1 || asFile.Items[0].ID != before.Items[0].ID || asFile.Items[0].IsFolder || asFile.Items[0].Type != "Movie" || asFile.Items[0].Media == nil {
		t.Fatalf("directory replacement did not become a media file: %+v", asFile.Items)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	asDirectory := libraryIntegrationQuery(t, ctx, store, query)
	if len(asDirectory.Items) != 1 || asDirectory.Items[0].ID != before.Items[0].ID || !asDirectory.Items[0].IsFolder || asDirectory.Items[0].Media != nil {
		t.Errorf("file replacement retained stale media metadata: %+v", asDirectory.Items)
	}
}
