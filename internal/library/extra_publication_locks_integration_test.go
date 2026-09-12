//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const extraPublicationChildSchema = "GOBY_TEST_EXTRA_PUBLICATION_CHILD_SCHEMA"

func TestExtraPublicationConcurrentLibraryDeletion(t *testing.T) {
	if schema := os.Getenv(extraPublicationChildSchema); schema != "" {
		runExtraPublicationDeletionChild(t, schema)
		return
	}
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, root, "scanned/Film/Main.mp4", "video:main")
	libraryIntegrationFile(t, root, "scanned/Film/featurettes/Clip.mp4", "video:extra")
	if err := os.Mkdir(filepath.Join(root, "deleted"), 0o700); err != nil {
		t.Fatal(err)
	}
	scanned := libraryIntegrationCreate(t, ctx, store, "Publication owner", "movies", filepath.Join(root, "scanned"))
	deleted := libraryIntegrationCreate(t, ctx, store, "Independent deletion", "movies", filepath.Join(root, "deleted"))
	if job := libraryIntegrationScan(t, ctx, store, scanned.ID, "Completed"); job.Error != "" {
		t.Fatalf("prepare accepted extra publication: %+v", job)
	}
	var schema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal("read the parent-owned publication schema")
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal("release the parent catalog owner before its isolated child")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate the current integration test executable")
	}
	// A regressed pure Go mutex cycle cannot be cancelled or rolled back by the
	// test. Isolate it while the parent retains ownership of schema/root cleanup.
	childCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	command := exec.CommandContext(childCtx, executable, "-test.run=^TestExtraPublicationConcurrentLibraryDeletion$", "-test.timeout=25s")
	command.Env = append(os.Environ(), extraPublicationChildSchema+"="+schema,
		"GOBY_TEST_EXTRA_PUBLICATION_ROOT="+root, "GOBY_TEST_EXTRA_PUBLICATION_SCANNED="+scanned.ID,
		"GOBY_TEST_EXTRA_PUBLICATION_DELETED="+deleted.ID)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated extra publication/deletion did not finish without a lock inversion (%T):\n%s", err, output)
	}
	var removed bool
	if err := pool.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM libraries WHERE id=$1)", deleted.ID).Scan(&removed); err != nil || !removed {
		t.Fatal("the independent library deletion was not committed")
	}
	resources := extraScanTestResources(t, ctx, pool, scanned.ID)
	if len(resources) != 1 || !resources[0].active {
		t.Fatal("concurrent deletion lost the scanned library's accepted extra")
	}
}

func runExtraPublicationDeletionChild(t *testing.T, schema string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("GOBY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("parse the inherited publication database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open the parent-owned publication schema")
	}
	defer pool.Close()
	store, err := New(pool, &libraryFixtureProber{}, []string{os.Getenv("GOBY_TEST_EXTRA_PUBLICATION_ROOT")})
	if err != nil {
		t.Fatalf("open the isolated publication owner (%T)", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := store.Close(closeCtx); err != nil {
			t.Errorf("close the isolated publication owner (%T)", err)
		}
	}()
	gate := holdExtraPublicationGate(t, ctx, pool)
	defer rollback(gate)
	job := themeScanTestForce(t, ctx, store, os.Getenv("GOBY_TEST_EXTRA_PUBLICATION_SCANNED"))
	taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
	deleted := make(chan error, 1)
	go func() {
		deleted <- store.DeleteLibrary(ctx, os.Getenv("GOBY_TEST_EXTRA_PUBLICATION_DELETED"))
	}()
	// The scanner is fixed at the database gate and the other worker is idle.
	// Only DeleteLibrary can now acquire Store.mu and wait on ownership.mu.
	waitExtraDeletionStoreLock(t, ctx, store, deleted)
	if err := gate.Commit(ctx); err != nil {
		t.Fatal("release the exact extra publication transaction gate")
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("delete an independent library during extra commit (%T)", err)
		}
	case <-ctx.Done():
		t.Fatal("extra publication and independent deletion retained opposite locks")
	}
	finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
	if finished.Status != "Completed" || finished.Error != "" {
		t.Fatalf("extra publication did not finish after independent deletion: %+v", finished)
	}
}

func holdExtraPublicationGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION extra_publication_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(hashtextextended(current_schema() || ':extra-publication-gate', 0));
			RETURN NEW;
		END; $$;
		CREATE TRIGGER extra_publication_gate BEFORE INSERT ON item_extra_resources
		FOR EACH ROW EXECUTE FUNCTION extra_publication_gate()`); err != nil {
		t.Fatalf("install the schema-local extra publication gate (%T)", err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin the extra publication gate")
	}
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':extra-publication-gate', 0))"); err != nil {
		rollback(gate)
		t.Fatal("hold the extra publication gate")
	}
	return gate
}

func waitExtraDeletionStoreLock(t *testing.T, ctx context.Context, store *Store, deleted <-chan error) {
	t.Helper()
	deadline, tick := time.NewTimer(5*time.Second), time.NewTicker(5*time.Millisecond)
	defer deadline.Stop()
	defer tick.Stop()
	for {
		if !store.mu.TryLock() {
			return
		}
		store.mu.Unlock()
		select {
		case err := <-deleted:
			t.Fatalf("independent deletion returned before owning its management lock (%T)", err)
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("independent deletion did not acquire Store.mu at the publication gate")
		case <-ctx.Done():
			t.Fatal("publication/deletion fixture ended before its lock barrier")
		}
	}
}

func TestExtraPublicationRejectsSourceChangesAfterTransactionStarts(t *testing.T) {
	for _, mutation := range []string{"registered root", "file pathname", "file contents"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			registered := filepath.Join(root, "registered")
			libraryIntegrationFile(t, root, "registered/Film/Main.mp4", "video:main")
			clip := libraryIntegrationFile(t, root, "registered/Film/featurettes/Clip.mp4", "video:accepted-extra")
			lib := libraryIntegrationCreate(t, ctx, store, "Publication source witnesses", "movies", registered)
			if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
				t.Fatalf("prepare the accepted source population: %+v", job)
			}
			before := extraScanTestSnapshot(t, ctx, pool, lib.ID)
			gate := holdExtraPublicationGate(t, ctx, pool)
			defer rollback(gate)
			job := themeScanTestForce(t, ctx, store, lib.ID)
			taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
			switch mutation {
			case "registered root":
				if err := os.Rename(registered, registered+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(registered, 0o700); err != nil {
					t.Fatal(err)
				}
			case "file pathname":
				if err := os.Rename(clip, clip+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(clip, []byte("video:replacement-extra"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "file contents":
				// An in-place write preserves every parent directory's snapshot,
				// so the prepared file check must still reject its changed bytes.
				if err := os.WriteFile(clip, []byte("video:changed-extra-with-a-new-size"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := gate.Commit(ctx); err != nil {
				t.Fatal("release the source replacement publication gate")
			}
			finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
			if finished.Error == "" || finished.Status == "Cancelled" {
				t.Fatalf("the transaction did not reject its changed source: %+v", finished)
			}
			if after := extraScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
				t.Fatal("a source change after transaction admission replaced the accepted extra population")
			}
		})
	}
}

func TestLibraryRootLeaseReopensTheRegisteredNameChain(t *testing.T) {
	root := t.TempDir()
	registered := filepath.Join(root, "parent", "registered")
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &Store{roots: []approvedRoot{{path: root}}}
	t.Cleanup(func() {
		for _, approved := range store.roots {
			if approved.root != nil {
				_ = approved.root.Close()
			}
		}
	})
	record := libraryRoot{allowedPath: root, relativePath: filepath.Join("parent", "registered")}
	lease, err := store.leaseLibraryRoot(record)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	opened, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	before, err := opened.Stat(".")
	_ = opened.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "parent"), filepath.Join(root, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	opened, err = lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	after, err := opened.Stat(".")
	_ = opened.Close()
	if err != nil || os.SameFile(before, after) {
		t.Fatal("the lease reused a held registered directory instead of reopening its current name")
	}
	if err := os.Rename(filepath.Join(root, "parent"), filepath.Join(root, "replacement")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("replacement", filepath.Join(root, "parent")); err != nil {
		t.Fatal(err)
	}
	if opened, err := lease.Open(); !errors.Is(err, ErrUnavailable) {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("the lease followed a rebound registered ancestor symlink")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	// Closing the independent clone cannot close the Store's approved anchor.
	record.relativePath = filepath.Join("original", "registered")
	opened, err = store.openLibraryRoot(record)
	if err != nil {
		t.Fatal("closing a publication lease closed the Store's approved anchor")
	}
	_ = opened.Close()
}
