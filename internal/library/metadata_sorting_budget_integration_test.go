package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/notificationjournal"
)

func TestSortRebuildRefreshesRemainingBudgetBeforeJournalLock(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('sorting-budget','Budget','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('sorting-budget','sorting-budget','Budget','budget','CollectionFolder',true),('sorting-budget-film','sorting-budget','The Film','the film','Movie',false);
		CREATE FUNCTION sorting_budget_work() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(2.6); RETURN NEW; END $$;
		CREATE TRIGGER sorting_budget_work BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION sorting_budget_work()`); err != nil {
		t.Fatal(err)
	}
	var ownerPID int
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { return tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&ownerPID) }); err != nil {
		t.Fatal(err)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT id FROM notification_journal_state WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			view := tx.(*ownedCallbackTx)
			// Scale the real protected deadline to five seconds while exercising
			// the actual owner connection, PostgreSQL work and subsequent row lock.
			originalView, originalCatalog := view.ctx, view.catalog.ctx
			short, cancel := context.WithTimeout(view.ctx, 5*time.Second)
			defer cancel()
			view.ctx, view.catalog.ctx = short, short
			defer func() { view.ctx, view.catalog.ctx = originalView, originalCatalog }()
			if _, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=ARRAY['The']`); err != nil {
				return err
			}
			return RebuildGeneratedSortNames(tx)
		})
	}()
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for waiting := false; !waiting; {
		if err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, ownerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("rebuild did not reach its later journal lock: %v", err)
		case <-deadline.C:
			t.Fatal("no actual PostgreSQL journal lock wait observed")
		case <-ticker.C:
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, notificationjournal.ErrJournal) {
			t.Fatalf("later statement did not use its remaining server-side budget: %v", err)
		}
	case <-deadline.C:
		t.Fatal("journal wait exceeded bounded transaction budget")
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { var alive int; return tx.QueryRow(`SELECT 1`).Scan(&alive) }); err != nil {
		t.Fatalf("cumulative waits closed the catalog owner connection: %v", err)
	}
	var key string
	var words []string
	if err := pool.QueryRow(ctx, `SELECT sort_name,(SELECT sort_remove_words FROM managed_settings WHERE id=1) FROM items WHERE id='sorting-budget-film'`).Scan(&key, &words); err != nil || key != "the film" || len(words) != 0 {
		t.Fatalf("budget failure partially committed settings or keys: %v", err)
	}
}
