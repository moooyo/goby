package settings

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestSortingLibraryEventWaitPrecedesFinalAdministratorLifetimeCheck(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('sorting-authority','Authority','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('sorting-authority','sorting-authority','Authority','authority','CollectionFolder',true),('sorting-authority-film','sorting-authority','The Film','the film','Movie',false)`); err != nil {
		t.Fatal(err)
	}
	var ownerPID int
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error { return tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&ownerPID) }); err != nil {
		t.Fatal(err)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT name FROM task_system_events WHERE name='LibraryChanged' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	sorting := Sorting{SortRemoveWords: []string{"The"}}
	done := make(chan error, 1)
	go func() {
		_, err := store.Update(ctx, actor, UpdateRequest{Revision: before.Revision, Overrides: before.Overrides, Sorting: &sorting})
		done <- err
	}()
	deadline := time.NewTimer(4 * time.Second)
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
			t.Fatalf("sorting finished before its LibraryChanged write waited: %v", err)
		case <-deadline.C:
			t.Fatal("sorting did not reach the actual event row lock")
		case <-ticker.C:
		}
	}
	// Wait against the database's actual credential deadline only after its
	// backend reports the lock. No sleep guesses whether the write was reached.
	if _, err := pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))+0.05) FROM sessions WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("expired credential committed after a delayed event write: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var key string
	var events int64
	if err := pool.QueryRow(ctx, `SELECT sort_name,(SELECT sum(sequence) FROM task_system_events) FROM items WHERE id='sorting-authority-film'`).Scan(&key, &events); err != nil || key != "the film" || events != 0 || !reflect.DeepEqual(store.Snapshot(), before) {
		t.Fatalf("expired authority partially committed sorting or events: %v", err)
	}
}
