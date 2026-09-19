package library

import (
	"testing"

	"github.com/moooyo/goby/internal/systemevents"
)

func TestCatalogSystemSignalSharesCommitAndDerivedOutputKeepsClientNotification(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	notifications := catalogChangesTestListener(t, store)
	sequence := func() int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='LibraryChanged'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := sequence()
	created, err := store.CreateLibrary(ctx, "System event library", "movies", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	_ = nextCatalogTestNotification(t, notifications)
	if sequence() != before+1 {
		t.Fatal("successful catalog commit omitted its durable signal")
	}
	tx := beginCatalogTestTransaction(t, ctx, store)
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: created.ID, LibraryID: created.ID, IsFolder: true, IsCollectionFolder: true}); err != nil {
		t.Fatal(err)
	}
	// A later query flushes the event before a final authorization statement;
	// the whole signal still rolls back with the surrounding transaction.
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if sequence() != before+1 {
		t.Fatal("rolled back catalog event escaped")
	}
	if err := store.DeleteLibrary(systemevents.WithDerived(ctx), created.ID); err != nil {
		t.Fatal(err)
	}
	removed := nextCatalogTestNotification(t, notifications)
	if len(removed.Changes) == 0 || sequence() != before+1 {
		t.Fatal("task-derived mutation either recursed or suppressed client invalidation")
	}
}
