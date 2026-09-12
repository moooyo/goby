//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/storagebinding"
)

func scanReconciliationCommitInsertItem(t *testing.T, fixture rootBindingScanFixture, id, relative, itemType, parentID string, folder bool) {
	t.Helper()
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, relative_path)
		VALUES ($1, $2, $3, NULLIF($4, ''), $1, lower($1), $5, $6, $7, $8)`,
		id, fixture.library.ID, fixture.scanRoot.id, parentID, itemType, folder,
		filepath.Join(fixture.scanRoot.path, filepath.FromSlash(relative)), relative); err != nil {
		t.Fatalf("insert reconciliation item %q: %v", id, err)
	}
}

func scanReconciliationCommitCapture(t *testing.T, fixture rootBindingScanFixture, adapter *rootBindingScanTestCapture) *rootBindingScanCapture {
	t.Helper()
	if adapter == nil {
		adapter = &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	}
	capture, err := fixture.prepare(adapter)
	if capture != nil {
		t.Cleanup(func() { _ = capture.Close() })
	}
	if err != nil || capture == nil || capture.status != RootBindingVerified || capture.opened == nil {
		t.Fatalf("prepare verified reconciliation root: capture = %+v, error = %v", capture, err)
	}
	return capture
}

func scanReconciliationCommitObserveRoot(t *testing.T, evidence *scanReconciliationEvidence, capture *rootBindingScanCapture) {
	t.Helper()
	rootID, root := capture.row.root.id, capture.opened
	if err := evidence.AttachRoot(rootID, root); err != nil {
		t.Fatal(err)
	}
	var walk func(string)
	walk = func(relative string) {
		directory, err := openScanFile(root, filepath.FromSlash(relative))
		if err != nil {
			t.Fatal(err)
		}
		defer directory.Close()
		before, err := directory.Stat()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := directory.ReadDir(-1)
		if err != nil {
			t.Fatal(err)
		}
		if err := evidence.RecordDirectory(rootID, relative, before, raw); err != nil {
			t.Fatalf("record complete raw membership for %q: %v", relative, err)
		}
		for _, entry := range raw {
			if entry.IsDir() {
				walk(filepath.ToSlash(filepath.Join(relative, entry.Name())))
			}
		}
		if err := evidence.CompleteDirectory(rootID, relative); err != nil {
			t.Fatalf("complete reconciliation directory %q: %v", relative, err)
		}
	}
	walk(".")
}

func scanReconciliationCommitEvidence(t *testing.T, captures ...*rootBindingScanCapture) *scanReconciliationEvidence {
	t.Helper()
	evidence := newScanReconciliationEvidence()
	t.Cleanup(func() { _ = evidence.Close() })
	for _, capture := range captures {
		scanReconciliationCommitObserveRoot(t, evidence, capture)
	}
	return evidence
}

func scanReconciliationCommitSnapshot(t *testing.T, fixture rootBindingScanFixture) string {
	t.Helper()
	var related string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'theme_owners', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM theme_owner_ids r),
		'themes', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY resource_item_id), '[]') FROM item_theme_resources r),
		'extras', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY resource_item_id), '[]') FROM item_extra_resources r),
		'theme_reservations', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY root_id, relative_path), '[]') FROM theme_reserved_paths r),
		'extra_reservations', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY root_id, relative_path), '[]') FROM extra_reserved_paths r),
		'user_data', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY user_id, item_id), '[]') FROM user_item_data r))::text`).Scan(&related); err != nil {
		t.Fatalf("snapshot reconciliation dependents: %v", err)
	}
	return catalogAuditSnapshot(t, fixture.ctx, fixture.pool) + related
}

func scanReconciliationCommitAssertItem(t *testing.T, fixture rootBindingScanFixture, itemID string, want bool) {
	t.Helper()
	var exists bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id = $1)`, itemID).Scan(&exists); err != nil || exists != want {
		t.Fatalf("reconciliation item %q exists = %t, error = %v; want %t", itemID, exists, err, want)
	}
}

func TestScanReconciliationCommitIntegrationRemovesProvenAbsentPhysicalItem(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	missing := libraryIntegrationFile(t, fixture.scanRoot.path, "Missing.mkv", "video:removed-physical-source")
	libraryIntegrationFile(t, fixture.scanRoot.path, "Accepted.mkv", "video:accepted-cache-hit")
	scanReconciliationCommitInsertItem(t, fixture, "removed-physical", "Missing.mkv", "Movie", fixture.library.ID, false)
	scanReconciliationCommitInsertItem(t, fixture, "accepted-cache-hit", "Accepted.mkv", "Movie", fixture.library.ID, false)
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	if err := evidence.MarkSeen("accepted-cache-hit"); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if err != nil || len(albums) != 0 {
		t.Fatalf("remove proven physical absence: affected albums = %v, error = %v", albums, err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "removed-physical", false)
	scanReconciliationCommitAssertItem(t, fixture, "accepted-cache-hit", true)
	scanReconciliationCommitAssertItem(t, fixture, fixture.library.ID, true)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogRemoved, ItemID: "removed-physical", LibraryID: fixture.library.ID, ParentID: fixture.library.ID,
	}})
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationUnprovenDescendantPreservesEntirePass(t *testing.T) {
	for _, kind := range []string{"seen child", "foreign child", "synthetic child", "inactive theme", "inactive extra"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			scanReconciliationCommitInsertItem(t, fixture, "independent-missing", "Independent.mkv", "Movie", fixture.library.ID, false)
			parentPath, parentType, folder := "Gone", "Folder", true
			if kind == "inactive theme" || kind == "inactive extra" {
				parentPath, parentType, folder = "Gone.mkv", "Movie", false
			}
			scanReconciliationCommitInsertItem(t, fixture, "missing-parent", parentPath, parentType, fixture.library.ID, folder)
			childPath, childType, childFolder := "Elsewhere.mkv", "Movie", false
			switch kind {
			case "seen child":
				libraryIntegrationFile(t, fixture.scanRoot.path, childPath, "video:accepted-child")
			case "synthetic child":
				childPath, childType, childFolder = "//series/retained", "Series", true
			case "inactive theme":
				childPath, childType = "Alive/theme.mp3", "Audio"
				libraryIntegrationFile(t, fixture.scanRoot.path, childPath, "audio:retained-theme-history")
			case "inactive extra":
				childPath, childType = "Alive/featurettes/Clip.mp4", "Video"
				libraryIntegrationFile(t, fixture.scanRoot.path, childPath, "video:retained-extra-history")
			}
			scanReconciliationCommitInsertItem(t, fixture, "protected-child", childPath, childType, "missing-parent", childFolder)
			switch kind {
			case "foreign child":
				foreignPath := filepath.Join(fixture.scanRoot.path, "foreign-root")
				if err := os.Mkdir(foreignPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO libraries (id, name, collection_type)
					VALUES ('foreign-reconciliation-library', 'Foreign reconciliation library', 'movies')`); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
					VALUES ('foreign-reconciliation-root', 'foreign-reconciliation-library', $1, $2, 'foreign-root')`,
					foreignPath, fixture.scanRoot.allowedPath); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items
					SET library_id = 'foreign-reconciliation-library', root_id = 'foreign-reconciliation-root', path = $1
					WHERE id = 'protected-child'`, filepath.Join(foreignPath, childPath)); err != nil {
					t.Fatal(err)
				}
			case "inactive theme":
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, $2, false)`, fixture.scanRoot.id, childPath); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('protected-child', 'missing-parent', 'song', false)`); err != nil {
					t.Fatal(err)
				}
			case "inactive extra":
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, 'Alive/featurettes', true)`, fixture.scanRoot.id); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_extra_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('protected-child', 'missing-parent', 'clip', false)`); err != nil {
					t.Fatal(err)
				}
			}
			userDataSeed(t, fixture.ctx, fixture.pool, fixture.actor.User.ID, UserData{
				ItemID: "protected-child", IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 91,
			})
			capture := scanReconciliationCommitCapture(t, fixture, nil)
			evidence := scanReconciliationCommitEvidence(t, capture)
			if kind == "seen child" {
				if err := evidence.MarkSeen("protected-child"); err != nil {
					t.Fatal(err)
				}
			}
			before := scanReconciliationCommitSnapshot(t, fixture)
			notifications := catalogChangesTestListener(t, fixture.store)
			albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
			if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || len(albums) != 0 {
				t.Fatalf("unproven descendant authorized deletion: affected albums = %v, error = %v", albums, err)
			}
			if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
				t.Fatal("one unproven cascade member changed the catalog, auxiliary history, or user data")
			}
			scanReconciliationCommitAssertItem(t, fixture, "independent-missing", true)
			scanReconciliationCommitAssertItem(t, fixture, "missing-parent", true)
			scanReconciliationCommitAssertItem(t, fixture, "protected-child", true)
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func TestScanReconciliationCommitIntegrationCancellationAfterDeleteRollsBackWithoutLosingOwnership(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "cancelled-physical", "Missing.mkv", "Movie", fixture.library.ID, false)
	adapter := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	capture := scanReconciliationCommitCapture(t, fixture, adapter)
	evidence := scanReconciliationCommitEvidence(t, capture)
	// Sequence advancement survives rollback and proves the protected DELETE ran.
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE SEQUENCE scan_reconciliation_delete_hits;
		CREATE FUNCTION scan_reconciliation_count_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.id = 'cancelled-physical' THEN
				PERFORM nextval('scan_reconciliation_delete_hits');
			END IF;
			RETURN OLD;
		END;
		$$;
		CREATE TRIGGER scan_reconciliation_count_delete BEFORE DELETE ON items
		FOR EACH ROW EXECUTE FUNCTION scan_reconciliation_count_delete()`); err != nil {
		t.Fatal(err)
	}
	afterDelete := false
	adapter.revalidate = func(ctx context.Context, _ int) error {
		var deleted bool
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT is_called FROM scan_reconciliation_delete_hits`).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if deleted {
			afterDelete = true
			fixture.task.cancel()
			// A successful adapter cannot hide cancellation of the original task.
			return nil
		}
		return ctx.Err()
	}
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if !afterDelete || !errors.Is(err, context.Canceled) || len(albums) != 0 {
		t.Fatalf("post-DELETE cancellation was not rolled back: reached DELETE = %t, affected albums = %v, error = %v", afterDelete, albums, err)
	}
	if fixture.store.ownership.lost.Load() {
		t.Fatal("caller cancellation destroyed healthy catalog ownership")
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("post-DELETE cancellation committed catalog or dependent changes")
	}
	assertNoCatalogTestNotification(t, notifications)
	tx := beginCatalogTestTransaction(t, fixture.ctx, fixture.store)
	if _, err := tx.Exec(fixture.ctx, `INSERT INTO server_settings (key, value)
		VALUES ('reconciliation-ownership-after-cancellation', 'healthy')`); err != nil {
		t.Fatalf("ownership could not admit a fresh write after cancellation: %v", err)
	}
	if err := tx.Rollback(fixture.ctx); err != nil {
		t.Fatalf("fresh owned transaction could not finish after cancellation: %v", err)
	}
}

func scanReconciliationCommitLaterRoot(t *testing.T, fixture rootBindingScanFixture) rootBindingScanFixture {
	t.Helper()
	root := libraryRoot{id: "later-reconciliation-root", libraryID: fixture.library.ID,
		path: filepath.Join(fixture.scanRoot.path, "later-root"), allowedPath: fixture.scanRoot.allowedPath}
	if err := os.Mkdir(root.path, 0o700); err != nil {
		t.Fatal(err)
	}
	var err error
	root.relativePath, err = filepath.Rel(root.allowedPath, root.path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot.Clone()
	snapshot.Mapping.RegisteredPath = root.path
	raw, err := storagebinding.EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO library_roots
		(id, library_id, path, allowed_path, relative_path, binding_revision, storage_binding, bound_at, bound_by)
		VALUES ($1, $2, $3, $4, $5, 13, $6::jsonb, clock_timestamp(), $7)`,
		root.id, root.libraryID, root.path, root.allowedPath, root.relativePath, string(raw), fixture.actor.User.ID); err != nil {
		t.Fatal(err)
	}
	fixture.scanRoot, fixture.snapshot = root, snapshot
	return fixture
}

func TestScanReconciliationCommitIntegrationLaterRootMoveRetainsSharedSeenIdentity(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	later := scanReconciliationCommitLaterRoot(t, fixture)
	libraryIntegrationFile(t, later.scanRoot.path, "Current.mkv", "video:identity-moved-to-later-root")
	scanReconciliationCommitInsertItem(t, fixture, "moved-between-roots", "Original.mkv", "Movie", fixture.library.ID, false)
	scanReconciliationCommitInsertItem(t, fixture, "independent-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	firstCapture := scanReconciliationCommitCapture(t, fixture, nil)
	laterCapture := scanReconciliationCommitCapture(t, later, nil)
	evidence := scanReconciliationCommitEvidence(t, firstCapture)
	// The later root accepts the old identity before library-wide reconciliation.
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET root_id = $1, relative_path = 'Current.mkv', path = $2
		WHERE id = 'moved-between-roots'`, later.scanRoot.id, filepath.Join(later.scanRoot.path, "Current.mkv")); err != nil {
		t.Fatal(err)
	}
	scanReconciliationCommitObserveRoot(t, evidence, laterCapture)
	if err := evidence.MarkSeen("moved-between-roots"); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{firstCapture, laterCapture}, evidence, nil)
	if err != nil || len(albums) != 0 {
		t.Fatalf("later-root identity was reconciled before its final observation: affected albums = %v, error = %v", albums, err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "independent-missing", false)
	var rootID, relative string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT root_id, relative_path FROM items WHERE id = 'moved-between-roots'`).Scan(&rootID, &relative); err != nil || rootID != later.scanRoot.id || relative != "Current.mkv" {
		t.Fatalf("later-root move lost its accepted row: root = %q, relative = %q, error = %v", rootID, relative, err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogRemoved, ItemID: "independent-missing", LibraryID: fixture.library.ID, ParentID: fixture.library.ID,
	}})
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationEvidenceBudgetExhaustionPreservesEntirePass(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "budget-protected-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := newScanReconciliationEvidenceWithLimits(scanReconciliationEvidenceLimits{
		directories: 8, entries: 8, seenIDs: 0, bytes: 1 << 20,
	})
	t.Cleanup(func() { _ = evidence.Close() })
	scanReconciliationCommitObserveRoot(t, evidence, capture)
	if err := evidence.MarkSeen("accepted-cache-hit"); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatalf("exhaust the scan-wide accepted identity budget: %v", err)
	}
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if !errors.Is(err, errScanReconciliationEvidenceBudget) || len(albums) != 0 {
		t.Fatalf("exhausted evidence authorized deletion: affected albums = %v, error = %v", albums, err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("exhausted evidence changed catalog or dependent rows")
	}
	if fixture.store.ownership.lost.Load() {
		t.Fatal("evidence exhaustion destroyed healthy catalog ownership")
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationFindsSurvivingAlbumAboveRemovedFolder(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE libraries SET collection_type = 'music' WHERE id = $1`, fixture.library.ID); err != nil {
		t.Fatal(err)
	}
	fixture.library.CollectionType = "music"
	track := libraryIntegrationFile(t, fixture.scanRoot.path, "Album/Disc/01.flac", "audio:removed-album-track")
	scanReconciliationCommitInsertItem(t, fixture, "surviving-album", "Album", "MusicAlbum", fixture.library.ID, true)
	scanReconciliationCommitInsertItem(t, fixture, "removed-disc", "Album/Disc", "Folder", "surviving-album", true)
	scanReconciliationCommitInsertItem(t, fixture, "removed-track", "Album/Disc/01.flac", "Audio", "removed-disc", false)
	if err := os.Remove(track); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(track)); err != nil {
		t.Fatal(err)
	}
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	if err := evidence.MarkSeen("surviving-album"); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if err != nil || len(albums) != 1 || albums[0] != "surviving-album" {
		t.Fatalf("removed nested Audio did not retain its surviving MusicAlbum: affected albums = %v, error = %v", albums, err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "surviving-album", true)
	scanReconciliationCommitAssertItem(t, fixture, "removed-disc", false)
	scanReconciliationCommitAssertItem(t, fixture, "removed-track", false)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{
		{Kind: CatalogRemoved, ItemID: "removed-disc", LibraryID: fixture.library.ID, ParentID: "surviving-album", IsFolder: true},
		{Kind: CatalogRemoved, ItemID: "removed-track", LibraryID: fixture.library.ID, ParentID: "removed-disc"},
	})
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationChangedApprovalPreservesEntirePass(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "approval-protected-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision = binding_revision + 1 WHERE id = $1`, fixture.scanRoot.id); err != nil {
		t.Fatal(err)
	}
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if !errors.Is(err, ErrRootBindingConflict) || len(albums) != 0 {
		t.Fatalf("changed storage approval authorized deletion: affected albums = %v, error = %v", albums, err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("changed storage approval mutated catalog or dependent rows")
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationRejectsEvidenceFromAnotherDirectory(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	libraryIntegrationFile(t, fixture.scanRoot.path, "Alive.mkv", "video:present-in-captured-root")
	scanReconciliationCommitInsertItem(t, fixture, "present-physical", "Alive.mkv", "Movie", fixture.library.ID, false)
	scanReconciliationCommitInsertItem(t, fixture, "independent-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	emptyRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = emptyRoot.Close() })
	// The root ID matches, but the complete listing belongs to another directory.
	observation := *capture
	observation.opened = emptyRoot
	evidence := scanReconciliationCommitEvidence(t, &observation)
	absent, err := evidence.PathAbsent(fixture.ctx, fixture.scanRoot.id, "Alive.mkv")
	if err != nil || !absent {
		t.Fatalf("prepare complete but unrelated absence evidence: absent = %t, error = %v", absent, err)
	}
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || len(albums) != 0 {
		t.Fatalf("unrelated directory evidence authorized deletion: affected albums = %v, error = %v", albums, err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("evidence from another directory changed catalog or dependent rows")
	}
	scanReconciliationCommitAssertItem(t, fixture, "present-physical", true)
	scanReconciliationCommitAssertItem(t, fixture, "independent-missing", true)
	if _, err := capture.opened.Stat("Alive.mkv"); err != nil {
		t.Fatalf("the verified root no longer retains its physical source: %v", err)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationInactiveOwnerEdgePreservesHistoryOutsideParentCascade(t *testing.T) {
	for _, kind := range []string{"theme", "extra"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			resourcePath, resourceType, contents := "Surviving/theme.mp3", "Audio", "audio:retained-owner-edge"
			if kind == "extra" {
				resourcePath, resourceType, contents = "Surviving/featurettes/Clip.mp4", "Video", "video:retained-owner-edge"
			}
			libraryIntegrationFile(t, fixture.scanRoot.path, resourcePath, contents)
			scanReconciliationCommitInsertItem(t, fixture, "independent-missing", "Independent.mkv", "Movie", fixture.library.ID, false)
			scanReconciliationCommitInsertItem(t, fixture, "missing-parent", "Gone.mkv", "Movie", fixture.library.ID, false)
			scanReconciliationCommitInsertItem(t, fixture, "surviving-parent", "Surviving", "Folder", fixture.library.ID, true)
			scanReconciliationCommitInsertItem(t, fixture, "protected-resource", resourcePath, resourceType, "surviving-parent", false)
			// A corrupt parent cannot hide the separate cascading owner foreign key.
			if kind == "theme" {
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, $2, false)`, fixture.scanRoot.id, resourcePath); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('protected-resource', 'missing-parent', 'song', false)`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, 'Surviving/featurettes', true)`, fixture.scanRoot.id); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_extra_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('protected-resource', 'missing-parent', 'clip', false)`); err != nil {
					t.Fatal(err)
				}
			}
			userDataSeed(t, fixture.ctx, fixture.pool, fixture.actor.User.ID, UserData{
				ItemID: "protected-resource", IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 91,
			})
			capture := scanReconciliationCommitCapture(t, fixture, nil)
			evidence := scanReconciliationCommitEvidence(t, capture)
			if err := evidence.MarkSeen("surviving-parent"); err != nil {
				t.Fatal(err)
			}
			before := scanReconciliationCommitSnapshot(t, fixture)
			notifications := catalogChangesTestListener(t, fixture.store)
			albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
			if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || len(albums) != 0 {
				t.Fatalf("inactive owner edge outside the parent cascade authorized deletion: affected albums = %v, error = %v", albums, err)
			}
			if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
				t.Fatal("an inactive owner edge lost catalog rows, association history, or user data")
			}
			for _, itemID := range []string{"independent-missing", "missing-parent", "surviving-parent", "protected-resource"} {
				scanReconciliationCommitAssertItem(t, fixture, itemID, true)
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func TestScanReconciliationCommitIntegrationOversizedSQLCandidatePreservesEntirePass(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "independent-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	// The persisted path exceeds the transfer bound without requiring a real file.
	longRelative := strings.Repeat("segment/", 600) + "Movie.mkv"
	scanReconciliationCommitInsertItem(t, fixture, "oversized-candidate", longRelative, "Movie", fixture.library.ID, false)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
	if !errors.Is(err, errScanReconciliationEvidenceBudget) || len(albums) != 0 {
		t.Fatalf("oversized SQL candidate was accepted as a truncated identity: affected albums = %v, error = %v", albums, err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("one oversized SQL candidate changed catalog or dependent rows")
	}
	scanReconciliationCommitAssertItem(t, fixture, "independent-missing", true)
	scanReconciliationCommitAssertItem(t, fixture, "oversized-candidate", true)
	var path, relative string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT path, relative_path FROM items WHERE id = 'oversized-candidate'`).Scan(&path, &relative); err != nil || relative != longRelative || path != filepath.Join(fixture.scanRoot.path, filepath.FromSlash(longRelative)) {
		t.Fatalf("oversized candidate lost its full persisted identity: error = %v", err)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestScanReconciliationCommitIntegrationRemovesProvenAbsentActiveAuxiliaryWithOwner(t *testing.T) {
	for _, kind := range []string{"theme", "extra"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			resourcePath, resourceType := "Gone/theme.mp3", "Audio"
			if kind == "extra" {
				resourcePath, resourceType = "Gone/featurettes/Clip.mp4", "Video"
			}
			scanReconciliationCommitInsertItem(t, fixture, "missing-owner", "Gone/Main.mkv", "Movie", fixture.library.ID, false)
			scanReconciliationCommitInsertItem(t, fixture, "missing-resource", resourcePath, resourceType, "missing-owner", false)
			if kind == "theme" {
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, $2, false)`, fixture.scanRoot.id, resourcePath); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('missing-resource', 'missing-owner', 'song', true)`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
					VALUES ($1, 'Gone/featurettes', true)`, fixture.scanRoot.id); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_extra_resources (resource_item_id, owner_item_id, kind, active)
					VALUES ('missing-resource', 'missing-owner', 'clip', true)`); err != nil {
					t.Fatal(err)
				}
			}
			capture := scanReconciliationCommitCapture(t, fixture, nil)
			evidence := scanReconciliationCommitEvidence(t, capture)
			notifications := catalogChangesTestListener(t, fixture.store)
			albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library, []*rootBindingScanCapture{capture}, evidence, nil)
			if err != nil || len(albums) != 0 {
				t.Fatalf("remove proven absent owner and active auxiliary: affected albums = %v, error = %v", albums, err)
			}
			scanReconciliationCommitAssertItem(t, fixture, "missing-owner", false)
			scanReconciliationCommitAssertItem(t, fixture, "missing-resource", false)
			scanReconciliationCommitAssertItem(t, fixture, fixture.library.ID, true)
			var reserved bool
			if kind == "theme" {
				err = fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM theme_reserved_paths
					WHERE root_id = $1 AND relative_path = $2 AND NOT is_directory)`, fixture.scanRoot.id, resourcePath).Scan(&reserved)
			} else {
				err = fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM extra_reserved_paths
					WHERE root_id = $1 AND relative_path = 'Gone/featurettes' AND is_directory)`, fixture.scanRoot.id).Scan(&reserved)
			}
			if err != nil || !reserved {
				t.Fatalf("proven auxiliary deletion discarded its root reservation: reserved = %t, error = %v", reserved, err)
			}
			var association bool
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM item_theme_resources WHERE resource_item_id = 'missing-resource')
				OR EXISTS(SELECT 1 FROM item_extra_resources WHERE resource_item_id = 'missing-resource')`).Scan(&association); err != nil || association {
				t.Fatalf("proven auxiliary deletion retained its association: retained = %t, error = %v", association, err)
			}
			assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{
				{Kind: CatalogRemoved, ItemID: "missing-owner", LibraryID: fixture.library.ID, ParentID: fixture.library.ID},
				{Kind: CatalogRemoved, ItemID: "missing-resource", LibraryID: fixture.library.ID},
			})
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}
