//go:build linux

package library

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestScanPrimaryNoopForceRefreshKeepsVersionRepairAndNotification(t *testing.T) {
	var changed atomic.Bool
	var calls atomic.Int64
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		info, err := forceProbeTestInfo(file)
		if err == nil && changed.Load() {
			info.DurationTicks += media.TicksPerSecond
		}
		return info, err
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "primary-noop/Film.mp4", "video:primary-noop")
	libraryIntegrationFile(t, root, "primary-noop/Film.nfo", `<movie><title>Film</title><genre>Restored Genre</genre></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Primary no-op", "movies", filepath.Dir(path))
	initial := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if initial.Error != "" || initial.Scanned != 1 || initial.Added != 1 || initial.Updated != 0 || calls.Load() != 1 {
		t.Fatalf("initial source was not accepted: job=%+v calls=%d", initial, calls.Load())
	}
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if item.Media == nil {
		t.Fatal("initial item omitted its media facts")
	}
	version := func(t *testing.T) (string, time.Time) {
		t.Helper()
		var xmin string
		var updated time.Time
		if err := pool.QueryRow(ctx, "SELECT xmin::text,updated_at FROM items WHERE id=$1", item.ID).Scan(&xmin, &updated); err != nil {
			t.Fatal(err)
		}
		return xmin, updated
	}
	beforeVersion, beforeUpdated := version(t)
	notifications := catalogChangesTestListener(t, store)
	for index, scenario := range []string{"identical", "entity_repair"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "entity_repair" {
				deleted, err := pool.Exec(ctx, "DELETE FROM item_entities WHERE item_id=$1", item.ID)
				if err != nil || deleted.RowsAffected() != 1 {
					t.Fatalf("entity repair fixture omitted its original association: rows=%d error=%v", deleted.RowsAffected(), err)
				}
			}
			job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
			if job.Error != "" || job.Scanned != 1 || job.Added != 0 || job.Updated != 1 || calls.Load() != int64(index+2) {
				t.Fatalf("identical force refresh lost accepted progress: job=%+v calls=%d", job, calls.Load())
			}
			assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID}})
			afterVersion, afterUpdated := version(t)
			if afterVersion != beforeVersion || !afterUpdated.Equal(beforeUpdated) {
				t.Fatalf("identical force refresh rewrote the primary item: xmin=%s/%s updated_at=%s/%s", beforeVersion, afterVersion, beforeUpdated, afterUpdated)
			}
			var associations int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_entities WHERE item_id=$1", item.ID).Scan(&associations); err != nil || associations != 1 {
				t.Fatalf("force refresh skipped entity repair: associations=%d error=%v", associations, err)
			}
		})
	}
	changed.Store(true)
	job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" || job.Scanned != 1 || job.Added != 0 || job.Updated != 1 || calls.Load() != 4 {
		t.Fatalf("changed force refresh lost accepted progress: job=%+v calls=%d", job, calls.Load())
	}
	assertScanCatalogChanges(t, notifications, []CatalogChange{{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: library.ID, ParentID: item.ParentID}})
	afterVersion, afterUpdated := version(t)
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if afterVersion == beforeVersion || afterUpdated.Equal(beforeUpdated) || updated.Media == nil ||
		updated.Media.DurationTicks != item.Media.DurationTicks+media.TicksPerSecond {
		t.Fatalf("changed probe facts were suppressed as a no-op: xmin=%s/%s updated_at=%s/%s media=%+v", beforeVersion, afterVersion, beforeUpdated, afterUpdated, updated.Media)
	}
}
