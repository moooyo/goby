//go:build linux

package library

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func metadataIngestTestWorker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, collection Library, key string) (string, AnalysisFence) {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO task_definitions(id,key,emby_key,name,description,category)
		VALUES($1,$2,$2,'Ingest fixture','','Metadata') ON CONFLICT(key) DO UPDATE SET key=EXCLUDED.key RETURNING id`, id, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	run, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	child, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind) VALUES($1,$2,1,0,'startup')
		ON CONFLICT (task_id,position) WHERE retired_at IS NULL DO UPDATE SET position=EXCLUDED.position RETURNING id`, trigger, id).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,task_key,task_emby_key,task_name,state,source,actor_kind,
		trigger_id,trigger_revision,scheduled_for,started_at,total_children)
		VALUES($1,$2,$3,$3,'Ingest fixture','running','startup','system',$4,1,clock_timestamp(),clock_timestamp(),1)`, run, id, key, trigger); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,state,executor_token,started_at)
		VALUES($1,$2,$3,$4,0,'running',$1,clock_timestamp())`, child, run, collection.ID, collection.Name); err != nil {
		t.Fatal(err)
	}
	fence := func(tx OwnedTx) error {
		var state string
		if err := tx.QueryRow(`SELECT run.state FROM task_runs run JOIN task_run_children child ON child.run_id=run.id
			WHERE run.id=$1 AND child.id=$2 AND child.executor_token=$2 AND child.state='running' FOR UPDATE OF run,child`, run, child).Scan(&state); err != nil {
			return err
		}
		if state != "running" {
			return context.Canceled
		}
		return nil
	}
	return child, fence
}

func TestLocalMetadataIngestRechecksAcceptedSourceAndSelectedSidecar(t *testing.T) {
	for _, scenario := range []string{"nfo", "image", "media", "root", "newer_scan", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, store, approved, userID := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				return forceProbeTestInfo(file)
			}))
			mediaPath := libraryIntegrationFile(t, approved, "movies/Film.mp4", "old media")
			nfoPath := libraryIntegrationFile(t, approved, "movies/Film.nfo", `<movie><title>Old Auto</title></movie>`)
			imagePath := filepath.Join(approved, "movies", "Film-poster.png")
			imageBytes := imageScanTestWrite(t, imagePath, color.NRGBA{R: 80, A: 255})
			collection := libraryIntegrationCreate(t, ctx, store, "Metadata ingest proof", "movies", filepath.Dir(mediaPath))
			store.deferLocalMetadata = true
			oldJob := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, userID, collection.ID, mediaPath)
			if scenario == "image" {
				// Independently exercise the image phase rather than accepting an NFO
				// before the blocked image publication reaches its final proof.
				if _, err := pool.Exec(ctx, `UPDATE local_metadata_ingest SET import_nfo=false WHERE item_id=$1`, item.ID); err != nil {
					t.Fatal(err)
				}
			}
			child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
			work, cancel := context.WithCancel(ctx)
			defer cancel()
			fenceCalls := 0
			var mutationErr error
			mutated := false
			guard := func(tx OwnedTx) error {
				if err := fence(tx); err != nil {
					return err
				}
				fenceCalls++
				// BeginTaskSourceOperation verifies before and after the first root
				// capture. The fifth check occurs after sidecar inspection, at the
				// start of the actual item writer, before its final source proof.
				if fenceCalls != 5 {
					return nil
				}
				mutated = true
				switch scenario {
				case "nfo":
					mutationErr = rewriteIngestFixture(nfoPath, []byte(`<movie><title>New Auto</title></movie>`))
				case "image":
					imageBytes[len(imageBytes)-1] = 'b'
					mutationErr = rewriteIngestFixture(imagePath, imageBytes)
				case "media":
					mutationErr = rewriteIngestFixture(mediaPath, []byte("new media"))
				case "root":
					_, mutationErr = tx.Exec(`UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, collection.ID)
				case "newer_scan":
					_, mutationErr = tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('newer-scan',$1,'Completed',clock_timestamp())`, collection.ID)
					if mutationErr == nil {
						_, mutationErr = tx.Exec(`UPDATE local_metadata_ingest SET scan_job_id='newer-scan' WHERE item_id=$1`, item.ID)
					}
				case "cancel":
					cancel()
				}
				return mutationErr
			}
			err := store.ImportLocalScanMetadata(work, child, collection.ID, guard, func(ProviderProgress) error { return nil })
			if !mutated || mutationErr != nil || err == nil || scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("source mutation was not rejected: injected=%v mutation=%v import=%v", mutated, mutationErr, err)
			}
			var jobID, title string
			var images int
			if err := pool.QueryRow(ctx, `SELECT q.scan_job_id,i.name,(SELECT count(*) FROM item_images WHERE item_id=i.id)
				FROM local_metadata_ingest q JOIN items i ON i.id=q.item_id WHERE i.id=$1`, item.ID).Scan(&jobID, &title, &images); err != nil ||
				jobID != oldJob.ID || title != "Film" || images != 0 {
				t.Fatalf("rejected import published or cleared work: title=%q images=%d err=%v", title, images, err)
			}
		})
	}
}

func rewriteIngestFixture(path string, data []byte) error {
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	if int64(len(data)) != before.Size() {
		return errors.New("fixture rewrite must preserve size")
	}
	// Distinguish change time even on filesystems with coarse timestamp ticks.
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return os.Chtimes(path, before.ModTime(), before.ModTime())
}
