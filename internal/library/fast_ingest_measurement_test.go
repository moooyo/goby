//go:build linux

package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// TestFastIngestSyntheticPublicationTiming compares the same catalog write
// path with and without a deterministic optional per-file probe delay. It
// measures publication and queue drain, not NAS or decoder performance.
func TestFastIngestSyntheticPublicationTiming(t *testing.T) {
	if os.Getenv("GOBY_TEST_FAST_INGEST_MEASUREMENT") != "1" {
		t.Skip("select the synthetic fast-ingest timing fixture explicitly")
	}
	const itemCount = 40
	const optionalCost = 40 * time.Millisecond
	probe := forceProbeVersionedFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		timer := time.NewTimer(optionalCost)
		defer timer.Stop()
		select {
		case <-timer.C:
			return forceProbeTestInfo(file)
		case <-ctx.Done():
			return media.Info{}, ctx.Err()
		}
	})
	basic := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) { return forceProbeTestInfo(file) })
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprintf("fast=%v", fast), func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, probe)
			if fast {
				store.basicVideoScanProbe, store.deferLocalMetadata = basic, true
			}
			for i := range itemCount {
				base := fmt.Sprintf("movies/Film.%03d", i)
				libraryIntegrationFile(t, root, base+".mp4", "synthetic payload")
				libraryIntegrationFile(t, root, base+".nfo", fmt.Sprintf("<movie><title>Fixture %d</title></movie>", i))
			}
			collection := libraryIntegrationCreate(t, ctx, store, "Synthetic fast ingest", "movies", filepath.Join(root, "movies"))
			start := time.Now()
			job, err := store.StartScan(ctx, collection.ID)
			if err != nil {
				t.Fatal(err)
			}
			var first, ninety time.Duration
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for ninety == 0 {
				var visible int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE library_id=$1 AND type='Movie'`, collection.ID).Scan(&visible); err != nil {
					t.Fatal(err)
				}
				observed := time.Since(start)
				if visible >= 1 && first == 0 {
					first = observed
				}
				if visible >= itemCount*9/10 {
					ninety = observed
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			finished := libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
			if finished.Added != itemCount {
				t.Fatalf("expected %d published movies, got %+v", itemCount, finished)
			}
			scanDone := time.Since(start)
			if fast {
				child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
				if err := store.ImportLocalScanMetadata(ctx, child, collection.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
					t.Fatal(err)
				}
				var pending int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM local_metadata_ingest`).Scan(&pending); err != nil || pending != 0 {
					t.Fatalf("local stage did not drain: %d %v", pending, err)
				}
			}
			t.Logf("movies=%d artificial_optional_probe_per_item=%s first_visible=%s ninety_percent_visible=%s scan_finished=%s local_work_finished=%s",
				itemCount, optionalCost, first.Round(time.Millisecond), ninety.Round(time.Millisecond), scanDone.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
		})
	}
}
