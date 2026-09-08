//go:build linux

package library

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestScanDoesNotBlockOnMediaNamedPipe(t *testing.T) {
	fixture := &libraryFixtureProber{}
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, fixture)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Regular.mp4", "video:regular")
	if err := syscall.Mkfifo(filepath.Join(filepath.Dir(path), "Pipe.mp4"), 0600); err != nil {
		t.Fatalf("create named pipe fixture: %v", err)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Movies", "movies", filepath.Dir(path))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" || job.Scanned != 1 || job.Added != 1 || len(fixture.calls()) != 1 {
		t.Errorf("named pipe reached probing or was not reported: %+v", job)
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if len(items.Items) != 1 || items.Items[0].Path != path {
		t.Errorf("nonregular input entered the catalog: %+v", items.Items)
	}
}
