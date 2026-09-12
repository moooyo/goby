//go:build linux

package library

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func scanReconciliationScanStore(t *testing.T, prober Prober) (context.Context, *pgxpool.Pool, *Store, string, string) {
	t.Helper()
	ctx, pool, store, _, userID := libraryIntegrationStore(t, prober)
	root := filepath.Join(rootStorageTestDirectory(t), "approved")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: root})
	store.mu.Unlock()
	return ctx, pool, store, root, userID
}

func scanReconciliationCreateRoots(t *testing.T, ctx context.Context, store *Store, parent string, names ...string) Library {
	t.Helper()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(parent, name))
	}
	library, err := store.CreateLibrary(ctx, "Reconciliation roots", "movies", paths)
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func scanReconciliationAssertBound(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	t.Helper()
	var complete bool
	if err := pool.QueryRow(ctx, `SELECT count(*)>0 AND bool_and(storage_binding IS NOT NULL)
		FROM library_roots WHERE library_id=$1`, libraryID).Scan(&complete); err != nil || !complete {
		t.Fatalf("scan fixture lacks real initial storage approvals: complete=%v error=%v", complete, err)
	}
}

func scanReconciliationAssertItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, present bool, ids ...string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE id=ANY($1::text[])`, ids).Scan(&count); err != nil {
		t.Fatal(err)
	}
	want := 0
	if present {
		want = len(ids)
	}
	if count != want {
		t.Fatalf("retained item count=%d, want=%d for %v", count, want, ids)
	}
}

func scanReconciliationRemoved(t *testing.T, notifications <-chan CatalogNotification, libraryID string) []string {
	t.Helper()
	var ids []string
	for {
		select {
		case notification := <-notifications:
			if notification.Resync {
				t.Fatal("small scan reconciliation unexpectedly exceeded notification bounds")
			}
			for _, change := range notification.Changes {
				if change.LibraryID != libraryID {
					t.Fatal("scan reconciliation published a foreign library")
				}
				if change.Kind == CatalogRemoved {
					ids = append(ids, change.ItemID)
				}
			}
		default:
			sort.Strings(ids)
			return ids
		}
	}
}

func TestScanReconciliationScanRemovesMissingFilesAndDirectoriesPreservesCache(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := scanReconciliationScanStore(t, prober)
	missing := libraryIntegrationFile(t, root, "movies/Gone/Lost.mp4", "video:missing")
	kept := libraryIntegrationFile(t, root, "movies/Kept.mp4", "video:kept")
	library := libraryIntegrationCreate(t, ctx, store, "Reconciled movies", "movies", filepath.Join(root, "movies"))
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	lost := nfoCatalogItem(t, ctx, store, userID, library.ID, missing)
	folder := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(missing))
	keep := nfoCatalogItem(t, ctx, store, userID, library.ID, kept)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: keep.ID, IsFavorite: true, PlayCount: 4, PlaybackPositionTicks: 80})
	before := forceProbeUserDataSnapshot(t, ctx, pool, keep.ID)
	notifications := catalogChangesTestListener(t, store)
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(missing)); err != nil {
		t.Fatal(err)
	}
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" || job.Added != 0 || job.Updated != 0 {
		t.Fatalf("stable complete reconciliation did not complete quietly: %+v", job)
	}
	scanReconciliationAssertItems(t, ctx, pool, false, lost.ID, folder.ID)
	scanReconciliationAssertItems(t, ctx, pool, true, keep.ID, library.ID)
	want := []string{lost.ID, folder.ID}
	sort.Strings(want)
	if got := scanReconciliationRemoved(t, notifications, library.ID); !reflect.DeepEqual(got, want) {
		t.Fatalf("committed Removed facts=%v, want=%v", got, want)
	}
	if forceProbeUserDataSnapshot(t, ctx, pool, keep.ID) != before || len(prober.calls()) != 2 {
		t.Fatal("missing reconciliation changed or reprobed the cached survivor")
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationScanUnboundOrUnavailableRootPreservesWholeLibrary(t *testing.T) {
	for _, scenario := range []string{"unbound", "unavailable second root"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, store, root, userID := scanReconciliationScanStore(t, &libraryFixtureProber{})
			missing := libraryIntegrationFile(t, root, "a-root/Lost.mp4", "video:missing")
			libraryIntegrationFile(t, root, "z-root/Other.mp4", "video:other")
			library := scanReconciliationCreateRoots(t, ctx, store, root, "a-root", "z-root")
			scanReconciliationAssertBound(t, ctx, pool, library.ID)
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, userID, library.ID, missing)
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			if scenario == "unbound" {
				if tag, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL
					WHERE library_id=$1 AND path=$2`, library.ID, filepath.Join(root, "z-root")); err != nil || tag.RowsAffected() != 1 {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(filepath.Join(root, "z-root"), filepath.Join(root, "unavailable")); err != nil {
					t.Fatal(err)
				}
			}
			notifications := catalogChangesTestListener(t, store)
			status := "Completed"
			if scenario != "unbound" {
				status = "Failed"
			}
			libraryIntegrationScan(t, ctx, store, library.ID, status)
			scanReconciliationAssertItems(t, ctx, pool, true, item.ID)
			if got := scanReconciliationRemoved(t, notifications, library.ID); len(got) != 0 {
				t.Fatalf("incomplete all-root proof removed catalog items: %v", got)
			}
		})
	}
}

func TestScanReconciliationScanCrossRootMoveRetainsIdentityAndUserData(t *testing.T) {
	ctx, pool, store, root, userID := scanReconciliationScanStore(t, &libraryFixtureProber{})
	oldPath := libraryIntegrationFile(t, root, "a-root/Moved.mp4", "video:cross-root")
	if err := os.MkdirAll(filepath.Join(root, "z-root"), 0o700); err != nil {
		t.Fatal(err)
	}
	library := scanReconciliationCreateRoots(t, ctx, store, root, "a-root", "z-root")
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, oldPath)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 3})
	before := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	newPath := filepath.Join(root, "z-root", "Moved.mp4")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" || job.Added != 0 {
		t.Fatalf("cross-root scan did not reconcile the move: %+v", job)
	}
	after := nfoCatalogItem(t, ctx, store, userID, library.ID, newPath)
	if after.ID != item.ID || forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != before {
		t.Fatal("later-root move matching lost item identity or user data")
	}
	if got := scanReconciliationRemoved(t, notifications, library.ID); len(got) != 0 {
		t.Fatalf("a move was emitted as removal: %v", got)
	}
}

func TestScanReconciliationScanReplacementRetainsThenOriginalResumesWithoutRebind(t *testing.T) {
	ctx, pool, store, root, userID := scanReconciliationScanStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/Lost.mp4", "video:original")
	registered := filepath.Dir(path)
	library := libraryIntegrationCreate(t, ctx, store, "Original storage", "movies", registered)
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	var approval string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM library_roots r WHERE library_id=$1`, library.ID).Scan(&approval); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(root, "saved-original")
	if err := os.Rename(registered, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	for attempt := 0; attempt < 2; attempt++ {
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		scanReconciliationAssertItems(t, ctx, pool, true, item.ID)
		assertNoCatalogTestNotification(t, notifications)
	}
	if err := os.Remove(filepath.Join(saved, "Lost.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(registered); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, registered); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatal(job.Error)
	}
	scanReconciliationAssertItems(t, ctx, pool, false, item.ID)
	if got := scanReconciliationRemoved(t, notifications, library.ID); !reflect.DeepEqual(got, []string{item.ID}) {
		t.Fatalf("original storage did not resume missing reconciliation: %v", got)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM library_roots r WHERE library_id=$1`, library.ID).Scan(&after); err != nil || after != approval {
		t.Fatalf("original storage recovery changed its approval: %v", err)
	}
	var rebound int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action='library.root_binding.updated'`).Scan(&rebound); err != nil || rebound != 0 {
		t.Fatalf("scanner manufactured explicit rebind activity: count=%d error=%v", rebound, err)
	}
}
