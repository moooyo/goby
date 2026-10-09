//go:build linux

package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestUserCopyLockCountsRetainSourceAccountWriteBarrier(t *testing.T) {
	fixture := newUserCopyLockCountFixture(t, 1, 1)
	catalog, err := library.New(fixture.pool, notificationAuthorizationProber{}, []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
	gate := newUserCopyInsertGate()
	fixture.trace.beforeInsert = gate
	results, copyDone := startUserCopyCountOperation(ctx, fixture)
	cleanupUserCopyCountWorkers(t, cancel, gate, copyDone)
	copyPID := waitUserCopyInsertGate(t, ctx, gate, copyDone)
	writes := make(chan error, 1)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		// The subject names only the source account, so its wait cannot be
		// explained by the copy's administrator account or session locks.
		_, err := catalog.SetFavoriteFor(ctx, library.Subject{UserID: fixture.source.ID}, "count-copy-item-000001", true)
		writes <- err
	}()
	cleanupUserCopyCountWorkers(t, cancel, gate, copyDone, writeDone)
	waitManagedBlockedQuery(t, ctx, fixture.pool, copyPID, "SELECT id FROM users WHERE id = ANY", writeDone)
	gate.open()
	copied := awaitUserCopyCountOperation(t, ctx, results)
	select {
	case err := <-writes:
		if err != nil {
			t.Fatalf("source-state write failed after copy commit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("source-state writer did not finish after copy commit")
	}
	var sourceFavorite, copiedFavorite bool
	if err := fixture.pool.QueryRow(ctx, `SELECT
		(SELECT is_favorite FROM user_item_data WHERE user_id=$1 AND item_id='count-copy-item-000001'),
		(SELECT is_favorite FROM user_item_data WHERE user_id=$2 AND item_id='count-copy-item-000001')`, fixture.source.ID, copied.ID).
		Scan(&sourceFavorite, &copiedFavorite); err != nil {
		t.Fatal(err)
	}
	if !sourceFavorite || copiedFavorite {
		t.Fatalf("source-state write crossed the copy barrier: source=%v copy=%v", sourceFavorite, copiedFavorite)
	}
	assertUserCopyLockCountQueries(t, fixture.trace, fixture.source.ID, 100001, 100000)
}
