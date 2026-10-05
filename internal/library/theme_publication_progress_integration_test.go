package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func themePublicationProgressFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Store, Library, string, string) {
	t.Helper()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	theme := libraryIntegrationFile(t, root, "registered/theme-music/First.mp3", "audio:first-theme")
	libraryIntegrationFile(t, root, "registered/theme-music/Second.mp3", "audio:second-theme")
	library := libraryIntegrationCreate(t, ctx, store, "Atomic theme progress", "movies", filepath.Join(root, "registered"))
	_, children := taskScanFixture(t, ctx, pool, library)
	return ctx, pool, store, library, children[0], theme
}

func themePublicationAssertUnpublished(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	t.Helper()
	var roles, media int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id WHERE i.library_id=$1),
		(SELECT count(*) FROM items WHERE library_id=$1 AND type IN ('Audio','Video'))`, libraryID).Scan(&roles, &media); err != nil {
		t.Fatal(err)
	}
	if roles != 0 || media != 0 {
		t.Fatalf("uncommitted theme roles/media survived: %d/%d", roles, media)
	}
}

func TestThemePublicationCommitsResourcesAndTaskProgressTogether(t *testing.T) {
	ctx, pool, store, library, childID, _ := themePublicationProgressFixture(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE theme_progress_commit_audit (
		kind text NOT NULL, xact bigint NOT NULL, added integer NOT NULL);
		CREATE FUNCTION theme_progress_resource_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			INSERT INTO theme_progress_commit_audit VALUES ('resource', txid_current(), 0);
			RETURN NEW;
		END; $$;
		CREATE TRIGGER theme_progress_resource_audit AFTER INSERT ON item_theme_resources
		FOR EACH ROW EXECUTE FUNCTION theme_progress_resource_audit();
		CREATE FUNCTION theme_progress_counter_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			INSERT INTO theme_progress_commit_audit VALUES (TG_TABLE_NAME, txid_current(), NEW.added);
			RETURN NEW;
		END; $$;
		CREATE TRIGGER theme_progress_job_audit AFTER UPDATE ON scan_jobs
		FOR EACH ROW WHEN (NEW.added > OLD.added OR NEW.updated > OLD.updated)
		EXECUTE FUNCTION theme_progress_counter_audit();
		CREATE TRIGGER theme_progress_child_audit AFTER UPDATE ON task_run_children
		FOR EACH ROW WHEN (NEW.added > OLD.added OR NEW.updated > OLD.updated)
		EXECUTE FUNCTION theme_progress_counter_audit()`); err != nil {
		t.Fatalf("install atomic theme progress evidence: %v", err)
	}
	admission, err := store.AdmitTaskScan(ctx, childID)
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit collection theme scan: admission=%+v error=%v", admission, err)
	}
	job := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
	if job.Error != "" || job.Scanned != 2 || job.Added != 2 || job.Updated != 0 {
		t.Fatalf("complete theme counters=%+v", job)
	}
	taskScanAssertChild(t, ctx, pool, childID, job)
	var resources, jobs, children, transactions, acceptedCounters int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind='resource'),
		count(*) FILTER (WHERE kind='scan_jobs'), count(*) FILTER (WHERE kind='task_run_children'),
		count(DISTINCT xact), count(*) FILTER (WHERE kind<>'resource' AND added=2)
		FROM theme_progress_commit_audit`).Scan(&resources, &jobs, &children, &transactions, &acceptedCounters); err != nil {
		t.Fatal(err)
	}
	if resources != 2 || jobs != 1 || children != 1 || transactions != 1 || acceptedCounters != 2 {
		t.Fatalf("resource/job/child commit evidence=%d/%d/%d transactions=%d accepted=%d",
			resources, jobs, children, transactions, acceptedCounters)
	}
	if resources := themeScanTestResources(t, ctx, pool, library.ID); len(resources) != 2 {
		t.Fatalf("complete owner resource count=%d, want 2", len(resources))
	}
}

func TestThemePublicationChildProgressFailureRollsBackResources(t *testing.T) {
	ctx, pool, store, library, childID, _ := themePublicationProgressFixture(t)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_theme_child_progress() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected theme child progress failure';
		END; $$;
		CREATE TRIGGER reject_theme_child_progress BEFORE UPDATE ON task_run_children
		FOR EACH ROW WHEN (NEW.state='running' AND NEW.added > OLD.added)
		EXECUTE FUNCTION reject_theme_child_progress()`); err != nil {
		t.Fatalf("install child progress failure: %v", err)
	}
	admission, err := store.AdmitTaskScan(ctx, childID)
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit collection theme scan: admission=%+v error=%v", admission, err)
	}
	job := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Failed")
	if job.Error == "" || job.Scanned != 2 || job.Added != 0 || job.Updated != 0 {
		t.Fatalf("failed theme publication retained uncommitted counters: %+v", job)
	}
	taskScanAssertChild(t, ctx, pool, childID, job)
	themePublicationAssertUnpublished(t, ctx, pool, library.ID)
}

func TestThemePublicationFinalSourceRejectionRollsBackProgress(t *testing.T) {
	ctx, pool, store, library, childID, theme := themePublicationProgressFixture(t)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION theme_progress_proof_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(hashtextextended(current_schema() || ':theme-progress-proof-gate', 0));
			RETURN NEW;
		END; $$;
		CREATE TRIGGER theme_progress_proof_gate AFTER UPDATE ON task_run_children
		FOR EACH ROW WHEN (NEW.state='running' AND NEW.added > OLD.added)
		EXECUTE FUNCTION theme_progress_proof_gate()`); err != nil {
		t.Fatalf("install the final source proof gate: %v", err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(gate)
	if _, err := gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':theme-progress-proof-gate', 0))`); err != nil {
		t.Fatal(err)
	}
	admission, err := store.AdmitTaskScan(ctx, childID)
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit collection theme scan: admission=%+v error=%v", admission, err)
	}
	taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
	visible, err := store.GetJob(ctx, admission.Job.ID)
	if err != nil || visible.Status != "Running" || visible.Scanned < 0 || visible.Scanned > 2 || visible.Added != 0 || visible.Updated != 0 {
		t.Fatalf("uncommitted theme progress became visible: job=%+v error=%v", visible, err)
	}
	// The pending entry batch may still be invisible, while accepted resource
	// counters must remain zero until their catalog transaction commits.
	taskScanAssertChild(t, ctx, pool, childID, visible)
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM item_theme_resources", 0)
	if err := os.WriteFile(theme, []byte("audio:changed-after-the-progress-write"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	job := themeScanTestWaitStopped(t, ctx, store, admission.Job.ID)
	if job.Error == "" || job.Status == "Cancelled" || job.Scanned != 2 || job.Added != 0 || job.Updated != 0 {
		t.Fatalf("rejected final source retained publication progress: %+v", job)
	}
	taskScanAssertChild(t, ctx, pool, childID, job)
	themePublicationAssertUnpublished(t, ctx, pool, library.ID)
}
