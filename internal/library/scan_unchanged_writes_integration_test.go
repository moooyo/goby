package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/providers"
)

// This fixture links a running scan without scheduling a worker. Tests can
// change persisted authority between equal-counter checkpoints deterministically.
func scanUnchangedProgressFixture(t *testing.T, allowedCloseErrors ...error) (context.Context, *pgxpool.Pool, *Store, *scanTask, string) {
	t.Helper()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, allowedCloseErrors...)
	library := taskScanCreateLibrary(t, ctx, store, root, "unchanged-progress")
	task, runID := scanUnchangedProgressTask(t, ctx, pool, library)
	return ctx, pool, store, task, runID
}

func scanUnchangedProgressTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, library Library) (*scanTask, string) {
	t.Helper()
	runID, children := taskScanFixture(t, ctx, pool, library)
	jobID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	job, err := scanJob(pool.QueryRow(ctx, `INSERT INTO scan_jobs
		(id, library_id, task_child_id, status, started_at, scanned, added, updated)
		VALUES ($1,$2,$3,'Running',clock_timestamp(),1,1,0) RETURNING `+jobColumns,
		jobID, library.ID, children[0]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_run_children SET scan_job_id=$2, state='running',
		scanned=$3, added=$4, updated=$5, started_at=$6 WHERE id=$1`,
		children[0], job.ID, job.Scanned, job.Added, job.Updated, job.StartedAt); err != nil {
		t.Fatal(err)
	}
	scanCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	return &scanTask{ctx: scanCtx, cancel: cancel, job: job}, runID
}

func scanUnchangedProgressSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, task *scanTask) string {
	t.Helper()
	return taskRefreshRowSnapshot(t, ctx, pool, "scan_jobs", task.job.ID) + ":" +
		taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID)
}

func TestScanUnchangedProgressRetainsJobAndChildRowVersions(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	for checkpoint := 0; checkpoint < 2; checkpoint++ {
		if err := store.persistProgress(task); err != nil {
			t.Fatal(err)
		}
		if after := scanUnchangedProgressSnapshot(t, ctx, pool, task); after != before {
			t.Fatal("equal-counter checkpoint rewrote its job or child snapshot")
		}
	}
	task.job.Scanned++
	task.job.Updated++
	if err := store.persistProgress(task); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil || job.Scanned != task.job.Scanned || job.Added != task.job.Added || job.Updated != task.job.Updated {
		t.Fatalf("changed checkpoint lost counters: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
	after := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	if after == before {
		t.Fatal("changed counters did not publish a new atomic snapshot")
	}
	if err := store.persistProgress(task); err != nil || scanUnchangedProgressSnapshot(t, ctx, pool, task) != after {
		t.Fatalf("repeated changed checkpoint rewrote the accepted snapshot: %v", err)
	}
}

func TestScanUnchangedProgressStillChecksCurrentTaskAuthority(t *testing.T) {
	for _, scenario := range []struct {
		name, statement string
		parent          bool
		want            error
	}{
		{"stopping_parent", `UPDATE task_runs SET state='stopping',stop_reason='administrator',stop_requested_at=clock_timestamp() WHERE id=$1`, true, context.Canceled},
		{"cancelled_scan", `UPDATE scan_jobs SET cancel_requested=true WHERE id=$1`, false, context.Canceled},
		{"unknown_parent_mode", `UPDATE task_runs SET task_key='unsupported.scan' WHERE id=$1`, true, ErrUnavailable},
		{"changed_scan_link", `UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1`, false, ErrUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, store, task, runID := scanUnchangedProgressFixture(t)
			id := task.job.ID
			if scenario.parent {
				id = runID
			}
			if _, err := pool.Exec(ctx, scenario.statement, id); err != nil {
				t.Fatal(err)
			}
			before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
			if err := store.persistProgress(task); !errors.Is(err, scenario.want) {
				t.Fatalf("equal-counter checkpoint missed current authority: got=%v want=%v", err, scenario.want)
			}
			if errors.Is(scenario.want, context.Canceled) {
				job, err := store.GetJob(ctx, task.job.ID)
				if err != nil || !job.CancelRequested || !errors.Is(task.ctx.Err(), context.Canceled) {
					t.Fatalf("equal-counter cancellation was not retained: job=%+v error=%v context=%v", job, err, task.ctx.Err())
				}
				taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
			} else if scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
				t.Fatal("rejected equal-counter checkpoint changed its job or child")
			}
		})
	}
}

func TestScanUnchangedProgressRejectsInconsistentTerminalChild(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	if _, err := pool.Exec(ctx, `UPDATE task_run_children SET state='completed',finished_at=clock_timestamp()
		WHERE id=$1`, task.job.TaskChildID); err != nil {
		t.Fatal(err)
	}
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	if err := store.persistProgress(task); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("equal-counter checkpoint accepted a conflicting terminal child: %v", err)
	}
	if scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatal("rejected terminal association rewrote retained history")
	}
}

func TestScanUnchangedProgressRetainsTerminalAndDeletedSnapshots(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "terminal"
		if deleted {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
			finished, err := scanJob(pool.QueryRow(ctx, `UPDATE scan_jobs SET status='Completed',
				finished_at=clock_timestamp() WHERE id=$1 RETURNING `+jobColumns, task.job.ID))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE task_run_children SET state='completed',finished_at=$2 WHERE id=$1`,
				task.job.TaskChildID, finished.FinishedAt); err != nil {
				t.Fatal(err)
			}
			if deleted {
				if _, err := pool.Exec(ctx, `DELETE FROM scan_jobs WHERE id=$1`, task.job.ID); err != nil {
					t.Fatal(err)
				}
			}
			before := taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID)
			if err := store.persistProgress(task); !errors.Is(err, context.Canceled) {
				t.Fatalf("checkpoint did not honor retained terminal progress: %v", err)
			}
			if taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID) != before {
				t.Fatal("terminal checkpoint rewrote retained child history")
			}
		})
	}
}

func scanUnchangedItemSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(i)::text || ':' || i.xmin::text FROM items i WHERE id=$1`, id).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot + ":" + metadataMusicScanStateSnapshot(t, ctx, pool, id)
}

func TestStoreCachedPhysicalFoldersRetainSourcesOverlaysAndRowVersions(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=ARRAY['The']`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := libraryIntegrationFile(t, root, "unchanged-folders/The Source Show/Season 01/Show.S01E01.mp4", "video:unchanged-folder")
	nfoPath := libraryIntegrationFile(t, root, "unchanged-folders/The Source Show/tvshow.nfo",
		`<tvshow><title>The Source Show</title><plot>Local overview.</plot><genre>Local Genre</genre></tvshow>`)
	library := libraryIntegrationCreate(t, ctx, store, "Unchanged folders", "tvshows", filepath.Join(root, "unchanged-folders"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	series := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(nfoPath))
	season := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(path))
	actor := metadataEditTestActor(t, ctx, pool, "unchanged-folder-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	if detail.Automatic.SortName != "source show" {
		t.Fatalf("folder source did not use configured sorting: %+v", detail.Automatic)
	}
	plainSeries, plainSeason := scanUnchangedItemSnapshot(t, ctx, pool, series.ID), scanUnchangedItemSnapshot(t, ctx, pool, season.ID)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if scanUnchangedItemSnapshot(t, ctx, pool, series.ID) != plainSeries ||
		scanUnchangedItemSnapshot(t, ctx, pool, season.ID) != plainSeason {
		t.Fatal("cached physical folder rewrote unchanged generated sorting or NFO metadata")
	}
	detail, err := store.ApplyOnlineMetadata(ctx, actor, series.ID, detail.Revision, providers.Metadata{
		Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Series"},
		Fields:    map[string]json.RawMessage{"Name": json.RawMessage(`"The Online Show"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Provider publication captures the already sorted base; the scanner stores
	// its pre-online, pre-sorting base. Establish that existing canonical scan
	// representation before comparing repeated unchanged folder visits.
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	detail = metadataEditTestDetail(t, ctx, store, actor, series.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual Show"`), "Overview": json.RawMessage(`"Pinned overview."`),
	}, []string{"Overview"})
	notifications := catalogChangesTestListener(t, store)
	beforeSeries, beforeSeason := scanUnchangedItemSnapshot(t, ctx, pool, series.ID), scanUnchangedItemSnapshot(t, ctx, pool, season.ID)
	for visit := 0; visit < 2; visit++ {
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		if job.Error != "" || job.Added != 0 || job.Updated != 0 {
			t.Fatalf("unchanged folder scan changed file counters: %+v", job)
		}
		if scanUnchangedItemSnapshot(t, ctx, pool, series.ID) != beforeSeries ||
			scanUnchangedItemSnapshot(t, ctx, pool, season.ID) != beforeSeason {
			t.Fatal("cached physical folder changed an item, source, overlay or entity row version")
		}
		assertNoCatalogTestNotification(t, notifications)
	}
	// A fresh accepted NFO source still advances revision even when its changed
	// overview is hidden by a retained manual value and lock.
	libraryIntegrationFile(t, root, "unchanged-folders/The Source Show/tvshow.nfo",
		`<tvshow><title>The Source Show</title><plot>Changed local overview.</plot><genre>Local Genre</genre></tvshow>`)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	changed := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	if metadataEditTestRevision(t, changed.Revision) <= metadataEditTestRevision(t, detail.Revision) ||
		changed.Automatic.Overview != "Changed local overview." || changed.Effective.Name != "Manual Show" ||
		changed.Effective.SortName != "online show" || changed.Effective.Overview != "Pinned overview." {
		t.Fatalf("folder source refresh lost independent controls: %+v", changed)
	}
	if err := os.Remove(nfoPath); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	removed := metadataEditTestDetail(t, ctx, store, actor, series.ID)
	if metadataEditTestRevision(t, removed.Revision) <= metadataEditTestRevision(t, changed.Revision) ||
		removed.Automatic.Overview != "" || removed.Effective.Name != "Manual Show" ||
		removed.Effective.Overview != "Pinned overview." {
		t.Fatalf("folder NFO absence lost automatic source or retained controls: %+v", removed)
	}
}

func TestScannedMetadataRepairsEntitiesWithoutRewritingUnchangedItem(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "unchanged-entity-repair/Film.mp4", "video:unchanged-entity-repair")
	libraryIntegrationFile(t, root, "unchanged-entity-repair/Film.nfo", `<movie><title>Film</title><genre>Restored Genre</genre></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Unchanged repair", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	var before string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(i)::text || ':' || i.xmin::text FROM items i WHERE id=$1`, item.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM item_entities WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := store.beginOwnedTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if err := syncScannedMetadata(ctx, tx, item.ID, scannedMetadataOptions{ForceEntities: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(i)::text || ':' || i.xmin::text FROM items i WHERE id=$1`, item.ID).Scan(&after); err != nil || after != before {
		t.Fatalf("entity repair rewrote unchanged effective item fields: %v", err)
	}
	var associations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_entities WHERE item_id=$1`, item.ID).Scan(&associations); err != nil || associations != 1 {
		t.Fatalf("unchanged item did not regain its accepted entity association: count=%d error=%v", associations, err)
	}
}
