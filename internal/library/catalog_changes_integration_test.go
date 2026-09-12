package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/jackc/pgx/v5"
)

func TestCatalogChangesCreateAndDeleteRetainCommittedRootScope(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	notifications := catalogChangesTestListener(t, store)
	created, err := store.CreateLibrary(ctx, "Notification movies", "movies", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	added := nextCatalogTestNotification(t, notifications)
	want := CatalogChange{Kind: CatalogAdded, ItemID: created.ID, LibraryID: created.ID, IsFolder: true, IsCollectionFolder: true}
	assertCatalogTestChanges(t, added, []CatalogChange{want})
	var visible bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM libraries l JOIN items i ON i.id = l.id
		WHERE l.id = $1 AND i.library_id = l.id AND i.type = 'CollectionFolder')`, created.ID).Scan(&visible); err != nil || !visible {
		t.Fatalf("notified creation is not visible to an external reader: %t, %v", visible, err)
	}
	if err := store.DeleteLibrary(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	removed := nextCatalogTestNotification(t, notifications)
	want.Kind = CatalogRemoved
	assertCatalogTestChanges(t, removed, []CatalogChange{want})
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM libraries WHERE id = $1)
		OR EXISTS(SELECT 1 FROM items WHERE library_id = $1)`, created.ID).Scan(&visible); err != nil || visible {
		t.Fatalf("notified deletion is not visible to an external reader: %t, %v", visible, err)
	}
	if err := store.DeleteLibrary(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated deletion returned %v", err)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestCatalogChangesOnlyPublishSuccessfulCommitsIncludingCancelledRequests(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	notifications := catalogChangesTestListener(t, store)
	change := CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library", ParentID: "parent"}
	tx := beginCatalogTestTransaction(t, ctx, store)
	if _, err := tx.Exec(ctx, "INSERT INTO server_settings(key, value) VALUES ('catalog-notification-commit', 'committed')"); err != nil {
		t.Fatal(err)
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, notifications)
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM server_settings WHERE key = 'catalog-notification-commit')").Scan(&exists); err != nil || exists {
		t.Fatalf("uncommitted change escaped its transaction: %t, %v", exists, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{change})
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM server_settings WHERE key = 'catalog-notification-commit')").Scan(&exists); err != nil || !exists {
		t.Fatalf("committed notification has no externally visible write: %t, %v", exists, err)
	}
	if err := recordCatalogChanges(tx, change); !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("recording after commit returned %v", err)
	}

	tx = beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, notifications)
	tx = beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1 / 0"); err == nil {
		t.Fatal("the aborted transaction fixture unexpectedly succeeded")
	}
	if err := tx.Commit(ctx); !errors.Is(err, pgx.ErrTxCommitRollback) {
		t.Fatalf("aborted transaction commit returned %v", err)
	}
	assertNoCatalogTestNotification(t, notifications)

	callerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tx = beginCatalogTestTransaction(t, callerCtx, store)
	cancel()
	if _, err := tx.Exec(callerCtx, "INSERT INTO server_settings(key, value) VALUES ('catalog-notification-cancelled', 'committed')"); err != nil {
		t.Fatal(err)
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(callerCtx); err != nil {
		t.Fatalf("the protected commit inherited request cancellation: %v", err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{change})
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM server_settings WHERE key = 'catalog-notification-cancelled')").Scan(&exists); err != nil || !exists {
		t.Fatalf("cancelled-request notification has no committed row: %t, %v", exists, err)
	}
	if err := recordCatalogChanges(nil, change); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unowned transaction accepted catalog facts: %v", err)
	}
}

func TestCatalogChangesMetadataNoopAndFailuresStayQuiet(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "catalog-metadata-editor")
	path := libraryIntegrationFile(t, root, "movies/Group/Film.mp4", "video:catalog-notification")
	created := libraryIntegrationCreate(t, ctx, store, "Metadata notifications", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, created.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, created.ID, path)
	before := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	notifications := catalogChangesTestListener(t, store)
	noop := MetadataEdit{Revision: before.Revision, Overrides: map[string]json.RawMessage{}, LockedFields: []string{}}
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, noop); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, notifications)
	edit := MetadataEdit{Revision: before.Revision, Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Published title"`)}, LockedFields: []string{}}
	after, err := store.UpdateItemMetadata(ctx, actor, item.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated,
		ItemID: item.ID, LibraryID: created.ID, ParentID: item.ParentID, IsFolder: item.IsFolder}})
	var name string
	if err := pool.QueryRow(ctx, "SELECT name FROM items WHERE id = $1", item.ID).Scan(&name); err != nil || name != "Published title" {
		t.Fatalf("metadata notification preceded its public value: %q, %v", name, err)
	}
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, edit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale metadata edit returned %v", err)
	}
	edit.Revision = after.Revision
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, edit); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, notifications)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION catalog_notification_reject_metadata() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.name = 'Rejected title' THEN RAISE EXCEPTION 'metadata notification test rejection'; END IF; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER catalog_notification_metadata_failure AFTER UPDATE ON items
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION catalog_notification_reject_metadata()`); err != nil {
		t.Fatal(err)
	}
	edit.Overrides = map[string]json.RawMessage{"Name": json.RawMessage(`"Rejected title"`)}
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, edit); err == nil {
		t.Fatal("the deferred metadata commit failure was ignored")
	}
	assertNoCatalogTestNotification(t, notifications)
	retained := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if retained.Revision != after.Revision || retained.Effective.Name != "Published title" {
		t.Fatalf("the rejected metadata write changed persisted values: %+v", retained)
	}
}

func TestCatalogChangesOwnPayloadStorageAndSupportListenerReplacement(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	first := catalogChangesTestListener(t, store)
	backing := strings.Repeat("x", 1<<20) + "itemlibraryparentprevious"
	change := CatalogChange{Kind: CatalogUpdated, ItemID: backing[1<<20 : (1<<20)+4],
		LibraryID: backing[(1<<20)+4 : (1<<20)+11], ParentID: backing[(1<<20)+11 : (1<<20)+17],
		PreviousParentID: backing[(1<<20)+17:]}
	input := []CatalogChange{change}
	tx := beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, input...); err != nil {
		t.Fatal(err)
	}
	input[0] = CatalogChange{Kind: CatalogRemoved, ItemID: "changed", LibraryID: "changed"}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	notification := nextCatalogTestNotification(t, first)
	assertCatalogTestChanges(t, notification, []CatalogChange{change})
	if unsafe.StringData(notification.Changes[0].ItemID) == unsafe.StringData(change.ItemID) ||
		unsafe.StringData(notification.Changes[0].LibraryID) == unsafe.StringData(change.LibraryID) ||
		unsafe.StringData(notification.Changes[0].ParentID) == unsafe.StringData(change.ParentID) ||
		unsafe.StringData(notification.Changes[0].PreviousParentID) == unsafe.StringData(change.PreviousParentID) {
		t.Fatal("retained identifiers kept the producer's large backing allocation")
	}
	notification.Changes[0].ItemID = "listener-reused-payload"
	if len(tx.(*ownedTx).catalogChanges.changes) != 0 {
		t.Fatal("a completed transaction retained its listener payload")
	}
	tx = beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	store.SetCatalogChangeListener(nil)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, first)
	second := catalogChangesTestListener(t, store)
	tx = beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertNoCatalogTestNotification(t, first)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, second), []CatalogChange{change})
}

func TestCatalogChangesOverflowReplacesAllFactsWithResync(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	notifications := catalogChangesTestListener(t, store)
	change := CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library"}
	maximum := make([]CatalogChange, maxCatalogChanges)
	for index := range maximum {
		maximum[index] = change
	}
	tx := beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, maximum...); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), maximum)
	large := CatalogChange{Kind: CatalogUpdated, ItemID: strings.Repeat("i", 256),
		LibraryID: strings.Repeat("l", 256), ParentID: strings.Repeat("p", 256), PreviousParentID: strings.Repeat("o", 256)}
	for _, overflow := range []string{"count", "bytes", "invalid"} {
		t.Run(overflow, func(t *testing.T) {
			tx := beginCatalogTestTransaction(t, ctx, store)
			if overflow == "count" {
				if err := recordCatalogChanges(tx, maximum...); err != nil {
					t.Fatal(err)
				}
			} else if overflow == "bytes" {
				for index := 0; index < maxCatalogChangeBytes/(catalogChangeOverhead+1024); index++ {
					if err := recordCatalogChanges(tx, large); err != nil {
						t.Fatal(err)
					}
				}
				if tx.(*ownedTx).catalogChanges.resync {
					t.Fatal("facts within the byte budget were discarded")
				}
				if err := recordCatalogChanges(tx, large); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := recordCatalogChanges(tx, change, CatalogChange{Kind: CatalogAdded, ItemID: "bad\x00id", LibraryID: "library"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := recordCatalogChanges(tx, change); err != nil {
				t.Fatal(err)
			}
			batch := tx.(*ownedTx).catalogChanges
			if !batch.resync || batch.changes != nil || batch.bytes != 0 {
				t.Fatalf("overflow retained partial facts: %+v", batch)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			got := nextCatalogTestNotification(t, notifications)
			if !got.Resync || got.Changes != nil {
				t.Fatalf("overflow did not produce only a resync marker: %+v", got)
			}
		})
	}
}

func TestCatalogChangesListenerPanicCannotFailCommitOrRetainOwnership(t *testing.T) {
	ctx, _, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	var failures []CatalogNotification
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		failures = append(failures, notification)
		panic("catalog notification listener test failure")
	})
	created, err := store.CreateLibrary(ctx, "Committed despite listener failure", "movies", []string{root})
	if err != nil {
		t.Fatalf("listener failure changed the committed result: %v", err)
	}
	if len(failures) != 2 || failures[0].Resync || !failures[1].Resync || failures[1].Changes != nil {
		t.Fatalf("listener failure did not get one bounded resync attempt: %+v", failures)
	}
	notifications := catalogChangesTestListener(t, store)
	if err := store.DeleteLibrary(ctx, created.ID); err != nil {
		t.Fatalf("listener failure retained the ownership mutex: %v", err)
	}
	_ = nextCatalogTestNotification(t, notifications)
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		select {
		case notifications <- notification:
		default:
			panic("closed catalog notification test queue exceeded its bound")
		}
	})
	if store.catalogListener.Load() != nil || !store.catalogChangesClosed.Load() {
		t.Fatal("a final closed store accepted a new listener")
	}
	assertNoCatalogTestNotification(t, notifications)
}

func beginCatalogTestTransaction(t *testing.T, ctx context.Context, store *Store) pgx.Tx {
	t.Helper()
	tx, err := store.beginOwnedTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollback(tx) })
	return tx
}

func catalogChangesTestListener(t *testing.T, store *Store) chan CatalogNotification {
	t.Helper()
	notifications := make(chan CatalogNotification, 8)
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		select {
		case notifications <- notification:
		default:
			panic("catalog notification test queue exceeded its bound")
		}
	})
	t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
	return notifications
}

func nextCatalogTestNotification(t *testing.T, notifications <-chan CatalogNotification) CatalogNotification {
	t.Helper()
	select {
	case notification := <-notifications:
		return notification
	case <-time.After(5 * time.Second):
		t.Fatal("the committed catalog notification did not arrive")
		return CatalogNotification{}
	}
}

func assertNoCatalogTestNotification(t *testing.T, notifications <-chan CatalogNotification) {
	t.Helper()
	select {
	case notification := <-notifications:
		t.Fatalf("an uncommitted or unobserved operation published %+v", notification)
	default:
	}
}

func assertCatalogTestChanges(t *testing.T, notification CatalogNotification, want []CatalogChange) {
	t.Helper()
	if notification.Resync || !slices.Equal(notification.Changes, want) {
		t.Fatalf("catalog notification = %+v, want changes %+v", notification, want)
	}
}
