//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestScanOperationAuthorityRetainsLaterConfiguredRootUntilTheNextTask(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	firstApproved, laterApproved := filepath.Join(parent, "a-approved"), filepath.Join(parent, "z-approved")
	firstRoot, laterRoot := filepath.Join(firstApproved, "movies"), filepath.Join(laterApproved, "movies")
	const firstContents, laterContents = "video:first-operation-root", "video:later-operation-root"
	firstPath := libraryIntegrationFile(t, firstRoot, "First.mp4", firstContents)
	laterPath := libraryIntegrationFile(t, laterRoot, "Later.mp4", laterContents)
	var store *Store
	var probeMu sync.Mutex
	var calls []string
	var opened []*os.File
	var removed approvedRoot
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		data, err := io.ReadAll(file)
		if err != nil {
			return media.Info{}, err
		}
		probeMu.Lock()
		calls = append(calls, string(data))
		opened = append(opened, file)
		probeMu.Unlock()
		if string(data) == firstContents {
			// This is the first root's actual probe, after the complete task grant
			// was captured and before the later root begins its walk.
			store.mu.Lock()
			configured := make([]approvedRoot, 0, len(store.roots))
			for _, root := range store.roots {
				if root.path == laterApproved {
					removed = root
					continue
				}
				configured = append(configured, root)
			}
			store.roots = configured
			store.mu.Unlock()
			if removed.path == "" {
				return media.Info{}, errors.New("the later approved root was absent before the first probe")
			}
		}
		return libraryMediaFixture(data), ctx.Err()
	})
	ctx, pool, created, _, userID := libraryIntegrationStore(t, prober)
	store = created
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: firstApproved}, approvedRoot{path: laterApproved})
	store.mu.Unlock()
	t.Cleanup(func() {
		// Return the removed descriptor to Store cleanup without retiring it in
		// the middle of the operation whose permission is being exercised.
		store.mu.Lock()
		defer store.mu.Unlock()
		if removed.path != "" {
			store.roots = append(store.roots, removed)
		}
	})
	library, err := store.CreateLibrary(ctx, "Operation root permissions", "movies", []string{firstRoot, laterRoot})
	if err != nil {
		t.Fatal(err)
	}
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	var distinctApprovals int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT allowed_path) FROM library_roots WHERE library_id = $1`, library.ID).Scan(&distinctApprovals); err != nil || distinctApprovals != 2 {
		t.Fatalf("the library did not register two independent approved paths: count = %d, error = %v", distinctApprovals, err)
	}
	rootSnapshot := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]')::text
			FROM library_roots r WHERE library_id = $1`, library.ID).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	approved := rootSnapshot()
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" || job.Scanned != 2 || job.Added != 2 || job.Updated != 0 {
		t.Fatalf("the ongoing scan lost its later startup root: %+v", job)
	}
	probeMu.Lock()
	observedCalls, observedFiles := append([]string(nil), calls...), append([]*os.File(nil), opened...)
	probeMu.Unlock()
	if !reflect.DeepEqual(observedCalls, []string{firstContents, laterContents}) {
		t.Fatalf("the scan did not probe both physical sources in root order: %q", observedCalls)
	}
	for _, file := range observedFiles {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("the completed scan retained an actual probe descriptor: %v", err)
		}
	}
	store.mu.Lock()
	laterStillConfigured := store.rootBindingPathConfiguredLocked(laterApproved)
	store.mu.Unlock()
	if laterStillConfigured {
		t.Fatal("the first root's probe did not remove the later root configuration")
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if len(items.Items) != 2 {
		t.Fatalf("the ongoing scan did not publish both roots: %+v", items.Items)
	}
	first := libraryIntegrationItemByPath(t, items.Items, firstPath)
	later := libraryIntegrationItemByPath(t, items.Items, laterPath)
	var distinctItemRoots int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT root_id) FROM items WHERE id = ANY($1::text[])`, []string{first.ID, later.ID}).Scan(&distinctItemRoots); err != nil {
		t.Fatal(err)
	}
	if distinctItemRoots != 2 || first.Media == nil || later.Media == nil {
		t.Fatalf("the two physical sources lost their separate catalog roots: first = %+v, later = %+v", first, later)
	}
	if rootSnapshot() != approved {
		t.Fatal("the ongoing operation changed persisted root mappings or approval metadata")
	}
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: later.ID, IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 91})
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, later.ID)
	var beforeItems string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY id), '[]')::text
		FROM items i WHERE library_id = $1`, library.ID).Scan(&beforeItems); err != nil {
		t.Fatal(err)
	}
	// Force probing so unchanged media cannot conceal accidental grant reuse by
	// a new task. The removed configuration must reject its complete root set.
	next, err := store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatal(err)
	}
	failed := libraryIntegrationWaitJob(t, ctx, store, next.ID, "Failed")
	if failed.Error == "" || failed.Scanned != 0 || failed.Added != 0 || failed.Updated != 0 {
		t.Fatalf("the next task did not reject its removed root before scanning: %+v", failed)
	}
	probeMu.Lock()
	afterCalls := append([]string(nil), calls...)
	probeMu.Unlock()
	if !reflect.DeepEqual(afterCalls, observedCalls) {
		t.Fatalf("the next task reused previous root permissions for physical probing: before = %q, after = %q", observedCalls, afterCalls)
	}
	var afterItems string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY id), '[]')::text
		FROM items i WHERE library_id = $1`, library.ID).Scan(&afterItems); err != nil || afterItems != beforeItems {
		t.Fatalf("the rejected task changed previously accepted catalog rows: error = %v", err)
	}
	if rootSnapshot() != approved || forceProbeUserDataSnapshot(t, ctx, pool, later.ID) != beforeUserData {
		t.Fatal("the rejected task changed persisted root authority or retained user data")
	}
}
