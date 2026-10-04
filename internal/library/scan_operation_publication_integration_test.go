//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

const scanOperationPublicationSource = "video:operation-publication"

type scanOperationPublicationFixture struct {
	ctx                context.Context
	pool               *pgxpool.Pool
	store              *Store
	state              *scanState
	alternateLibraryID string
}

func scanOperationPublicationFixtureAt(t *testing.T, prober Prober, collectionType string) scanOperationPublicationFixture {
	t.Helper()
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, approved, "catalog/Feature.mp4", scanOperationPublicationSource)
	libraryIntegrationFile(t, approved, "catalog/Show/tvshow.nfo", `<tvshow><title>Approved series</title></tvshow>`)
	library := libraryIntegrationCreate(t, ctx, store, "Operation publication", collectionType, filepath.Dir(path))
	alternatePath := filepath.Join(approved, "alternate")
	if err := os.Mkdir(alternatePath, 0o700); err != nil {
		t.Fatal(err)
	}
	alternate := libraryIntegrationCreate(t, ctx, store, "Alternate operation identity", collectionType, alternatePath)
	task := rootBindingScanOwnedTask(t, ctx, pool, store, library)
	if task.job.TaskChildID != "" {
		t.Fatal("publication fixture must exercise an independent manual scan")
	}
	var root libraryRoot
	if err := pool.QueryRow(ctx, "SELECT id,library_id,path,allowed_path,relative_path FROM library_roots WHERE library_id=$1", library.ID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatal(err)
	}
	opened, err := store.openLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	state := &scanState{store: store, task: task, library: library, root: root, opened: opened,
		directoryIdentities: make(map[string]os.FileInfo)}
	for _, directory := range []string{".", "Show"} {
		info, err := opened.Lstat(directory)
		if err != nil {
			t.Fatal(err)
		}
		state.directoryIdentities[directory] = info
	}
	if err := store.prepareScanOperationAuthority(ctx, task, []libraryRoot{root}); err != nil {
		t.Fatal(err)
	}
	return scanOperationPublicationFixture{ctx: ctx, pool: pool, store: store, state: state, alternateLibraryID: alternate.ID}
}

func (fixture scanOperationPublicationFixture) mutate(change string) error {
	statement, id := "", fixture.state.root.id
	arguments := []any{id}
	switch change {
	case "job_force_probe":
		statement = "UPDATE scan_jobs SET force_probe=NOT force_probe WHERE id=$1"
		arguments = []any{fixture.state.task.job.ID}
	case "job_library":
		statement = "UPDATE scan_jobs SET library_id=$2 WHERE id=$1"
		arguments = []any{fixture.state.task.job.ID, fixture.alternateLibraryID}
	case "root_mapping":
		statement = "UPDATE library_roots SET relative_path=relative_path || '/moved' WHERE id=$1"
	case "approval_revision":
		statement = "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1"
	case "approval_document":
		statement = `UPDATE library_roots SET storage_binding='{"operation_witness":"changed"}',
			bound_at=clock_timestamp(),bound_by='operation-publication-test' WHERE id=$1`
	default:
		return fmt.Errorf("unknown operation publication mutation %q", change)
	}
	changed, err := fixture.pool.Exec(fixture.ctx, statement, arguments...)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != 1 {
		return fmt.Errorf("operation publication mutation affected %d rows, want one", changed.RowsAffected())
	}
	return nil
}

func scanOperationPublicationCases() []struct {
	name string
	want error
} {
	return []struct {
		name string
		want error
	}{
		{"job_force_probe", ErrUnavailable},
		{"job_library", ErrUnavailable},
		{"root_mapping", ErrRootBindingConflict},
		{"approval_revision", nil},
		{"approval_document", nil},
	}
}

func (fixture scanOperationPublicationFixture) snapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'item',to_jsonb(i),'item_version',i.xmin::text,
		'metadata',to_jsonb(ms),'metadata_version',ms.xmin::text) ORDER BY i.id),'[]'::jsonb)::text
		FROM items i LEFT JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.library_id=$1`, fixture.state.library.ID).
		Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestScanOperationAuthorityManualPrimaryPublicationFencesIdentity(t *testing.T) {
	const durationTicks = 53 * media.TicksPerSecond
	for _, test := range scanOperationPublicationCases() {
		t.Run(test.name, func(t *testing.T) {
			var fixture scanOperationPublicationFixture
			var mutationErr error
			calls := 0
			prober := scanProberFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				calls++
				data := make([]byte, len(scanOperationPublicationSource))
				if n, err := file.ReadAt(data, 0); err != nil || n != len(data) || string(data) != scanOperationPublicationSource {
					return media.Info{}, fmt.Errorf("actual publication source read: bytes=%d error=%v", n, err)
				}
				info, err := scanProbeFixtureInfo(file)
				if err != nil {
					return media.Info{}, err
				}
				// Preparation has persisted its entry checkpoint. The synchronous
				// writer must check identity again after this actual source read.
				mutationErr = fixture.mutate(test.name)
				if mutationErr != nil {
					return media.Info{}, mutationErr
				}
				info.DurationTicks = durationTicks
				return info, nil
			})
			fixture = scanOperationPublicationFixtureAt(t, prober, "movies")
			before := fixture.snapshot(t)
			notifications := catalogChangesTestListener(t, fixture.store)
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			err := fixture.state.scanFile("Feature.mp4", "video", hierarchy{parentID: fixture.state.library.ID})
			if mutationErr != nil || calls != 1 || !errors.Is(err, test.want) {
				t.Fatalf("manual primary publication lost its identity fence: calls=%d mutation=%v error=%v want=%v", calls, mutationErr, err, test.want)
			}
			wantAdded := 0
			if test.want == nil {
				wantAdded = 1
				var name string
				var acceptedDuration int64
				if err := fixture.pool.QueryRow(fixture.ctx, `SELECT name,(media->>'DurationTicks')::bigint FROM items
					WHERE root_id=$1 AND relative_path='Feature.mp4' AND NOT is_folder`, fixture.state.root.id).
					Scan(&name, &acceptedDuration); err != nil || name != "Feature" || acceptedDuration != durationTicks {
					t.Fatalf("manual primary did not publish its actual probe: name=%q duration=%d error=%v", name, acceptedDuration, err)
				}
			} else {
				if after := fixture.snapshot(t); after != before {
					t.Fatalf("rejected manual primary wrote item or metadata rows: before=%s after=%s", before, after)
				}
				assertNoCatalogTestNotification(t, notifications)
			}
			job, jobErr := fixture.store.GetJob(fixture.ctx, fixture.state.task.job.ID)
			if jobErr != nil || job.Scanned != 1 || job.Added != wantAdded || job.Updated != 0 ||
				fixture.state.task.job.Scanned != 1 || fixture.state.task.job.Added != wantAdded || fixture.state.task.job.Updated != 0 || fixture.state.warnings != 0 {
				t.Fatalf("manual primary changed its accepted progress: persisted=%+v memory=%+v warnings=%d error=%v", job, fixture.state.task.job, fixture.state.warnings, jobErr)
			}
			if count := scanProbeOpenDescriptors(t, []string{filepath.Join(fixture.state.root.path, "Feature.mp4")})[0]; count != 0 {
				t.Fatalf("manual primary retained %d source descriptors", count)
			}
			if after := originalMediaReadGovernor.Stats(); after != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("manual primary retained admission: before=%+v after=%+v owners=%+v", beforeIO, after, originalMediaReadOwners.Stats())
			}
		})
	}
}

func TestScanOperationAuthorityManualFolderPublicationFencesIdentity(t *testing.T) {
	for _, test := range scanOperationPublicationCases() {
		t.Run(test.name, func(t *testing.T) {
			fixture := scanOperationPublicationFixtureAt(t, &libraryFixtureProber{}, "tvshows")
			before := fixture.snapshot(t)
			notifications := catalogChangesTestListener(t, fixture.store)
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			if err := fixture.mutate(test.name); err != nil {
				t.Fatal(err)
			}
			id, err := fixture.state.folder("Show", filepath.Join(fixture.state.root.path, "Show"), "Fallback title", "Series", fixture.state.library.ID, 0)
			if !errors.Is(err, test.want) || (id != "") != (test.want == nil) {
				t.Fatalf("manual folder publication lost its identity fence: id=%q error=%v want=%v", id, err, test.want)
			}
			if test.want == nil {
				var name, automatic, metadataPath string
				var folder bool
				if err := fixture.pool.QueryRow(fixture.ctx, `SELECT i.name,ms.automatic->>'Name',i.is_folder,i.local_metadata_path
					FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id=$1`, id).
					Scan(&name, &automatic, &folder, &metadataPath); err != nil || name != "Approved series" || automatic != name || !folder || metadataPath != "Show/tvshow.nfo" {
					t.Fatalf("manual folder did not publish actual NFO metadata: name=%q automatic=%q folder=%v path=%q error=%v", name, automatic, folder, metadataPath, err)
				}
			} else {
				if after := fixture.snapshot(t); after != before {
					t.Fatalf("rejected manual folder wrote item or metadata rows: before=%s after=%s", before, after)
				}
				assertNoCatalogTestNotification(t, notifications)
			}
			job, jobErr := fixture.store.GetJob(fixture.ctx, fixture.state.task.job.ID)
			if jobErr != nil || job.Scanned != 0 || job.Added != 0 || job.Updated != 0 || fixture.state.warnings != 0 {
				t.Fatalf("folder publication changed primary progress or warnings: job=%+v warnings=%d error=%v", job, fixture.state.warnings, jobErr)
			}
			if count := scanProbeOpenDescriptors(t, []string{filepath.Join(fixture.state.root.path, "Show", "tvshow.nfo")})[0]; count != 0 {
				t.Fatalf("manual folder retained %d NFO descriptors", count)
			}
			if after := originalMediaReadGovernor.Stats(); after != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("manual folder retained admission: before=%+v after=%+v owners=%+v", beforeIO, after, originalMediaReadOwners.Stats())
			}
		})
	}
}
