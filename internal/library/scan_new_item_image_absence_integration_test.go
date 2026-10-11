//go:build linux

package library

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scanNewItemImageTrace struct {
	snapshots, deletes, inserts atomic.Int64
	comparisons                 atomic.Int64
}

func (trace *scanNewItemImageTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, imageCatalogSnapshotProjection) {
		trace.snapshots.Add(1)
	}
	if strings.Contains(data.SQL, "/* image_catalog_unchanged */") {
		trace.comparisons.Add(1)
	}
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(statement, "delete from item_images") {
		trace.deletes.Add(1)
	}
	if strings.HasPrefix(statement, "insert into item_images") {
		trace.inserts.Add(1)
	}
	return ctx
}

func (*scanNewItemImageTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *scanNewItemImageTrace) reset() {
	trace.snapshots.Store(0)
	trace.deletes.Store(0)
	trace.inserts.Store(0)
	trace.comparisons.Store(0)
}

func scanNewItemImageFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Store, Library, string, *scanNewItemImageTrace) {
	t.Helper()
	ctx, observer, original, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanNewItemImageTrace{}
	configuration := observer.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, &libraryFixtureProber{}, []string{approved})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close image absence scan store: %v", err)
		}
	})
	directory := filepath.Join(approved, "movies")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	library := libraryIntegrationCreate(t, ctx, store, "New item image absence", "movies", directory)
	return ctx, observer, store, library, directory, trace
}

func TestScanNewItemImageAbsenceAvoidsEmptyImageTransaction(t *testing.T) {
	for _, mode := range []string{"independent", "task_owned"} {
		for _, candidate := range []string{"absent", "poster"} {
			t.Run(mode+"/"+candidate, func(t *testing.T) {
				ctx, observer, store, library, directory, trace := scanNewItemImageFixture(t)
				libraryIntegrationFile(t, directory, "Feature.mp4", "video:new-image-absence")
				if candidate == "poster" {
					imageScanTestWrite(t, filepath.Join(directory, "Feature-poster.png"), color.NRGBA{R: 220, A: 255})
				}
				childID := ""
				if mode == "task_owned" {
					_, children := taskScanFixture(t, ctx, observer, library)
					if len(children) != 1 {
						t.Fatalf("expected one scan child, got %v", children)
					}
					childID = children[0]
				}
				trace.reset()
				var job Job
				if childID == "" {
					var err error
					job, err = store.StartScan(ctx, library.ID)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					admission, err := store.AdmitTaskScan(ctx, childID)
					if err != nil || admission.Kind != ScanAdmitted {
						t.Fatalf("admit image absence scan: admission=%+v error=%v", admission, err)
					}
					job = admission.Job
				}
				job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
				if job.Error != "" || job.Scanned != 1 || job.Added != 1 || job.Updated != 0 || job.TaskChildID != childID {
					t.Fatalf("new item scan lost its accepted result: %+v", job)
				}
				if childID != "" {
					taskScanAssertChild(t, ctx, observer, childID, job)
				}
				var itemID string
				if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Feature.mp4'", library.ID).Scan(&itemID); err != nil {
					t.Fatal(err)
				}
				var images int
				if err := observer.QueryRow(ctx, "SELECT count(*) FROM item_images WHERE item_id=$1", itemID).Scan(&images); err != nil {
					t.Fatal(err)
				}
				if candidate == "absent" {
					if images != 0 || trace.snapshots.Load() != 0 || trace.deletes.Load() != 0 || trace.inserts.Load() != 0 {
						t.Fatalf("new image-free item entered image publication: images=%d snapshots=%d deletes=%d inserts=%d",
							images, trace.snapshots.Load(), trace.deletes.Load(), trace.inserts.Load())
					}
				} else {
					var path string
					if err := observer.QueryRow(ctx, "SELECT relative_path FROM item_images WHERE item_id=$1 AND image_type='Primary'", itemID).Scan(&path); err != nil {
						t.Fatal(err)
					}
					if images != 1 || path != "Feature-poster.png" || trace.snapshots.Load() == 0 || trace.deletes.Load() == 0 || trace.inserts.Load() != 1 {
						t.Fatalf("new image candidate bypassed publication: images=%d path=%q snapshots=%d deletes=%d inserts=%d",
							images, path, trace.snapshots.Load(), trace.deletes.Load(), trace.inserts.Load())
					}
				}
			})
		}
	}
}

func TestScanRenamedItemWithLocalImageRetainsDeletionTransaction(t *testing.T) {
	ctx, observer, store, library, directory, trace := scanNewItemImageFixture(t)
	oldPath := libraryIntegrationFile(t, directory, "Feature.mp4", "video:rename-image-absence")
	poster := filepath.Join(directory, "Feature-poster.png")
	imageScanTestWrite(t, poster, color.NRGBA{G: 180, A: 255})
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var originalID string
	if err := observer.QueryRow(ctx, `SELECT i.id FROM items i JOIN item_images im ON im.item_id=i.id
		WHERE i.library_id=$1 AND i.relative_path='Feature.mp4' AND im.image_type='Primary'`, library.ID).Scan(&originalID); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, filepath.Join(directory, "Renamed.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(poster); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var renamedID string
	if err := observer.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Renamed.mp4'", library.ID).Scan(&renamedID); err != nil {
		t.Fatal(err)
	}
	var images int
	if err := observer.QueryRow(ctx, "SELECT count(*) FROM item_images WHERE item_id=$1", renamedID).Scan(&images); err != nil {
		t.Fatal(err)
	}
	if job.Error != "" || job.Scanned != 1 || job.Added != 0 || job.Updated != 1 || renamedID != originalID || images != 0 ||
		trace.snapshots.Load() == 0 || trace.deletes.Load() == 0 || trace.inserts.Load() != 0 {
		t.Fatalf("rename lost its old-image deletion path: job=%+v original=%s renamed=%s images=%d snapshots=%d deletes=%d inserts=%d",
			job, originalID, renamedID, images, trace.snapshots.Load(), trace.deletes.Load(), trace.inserts.Load())
	}
}
