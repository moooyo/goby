//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestScanReconciliationBlockedObservationRollsBackAndKeepsOwnership(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "blocked-observation-item", "Missing.mkv", "Movie", fixture.library.ID, false)
	adapter := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	capture := scanReconciliationCommitCapture(t, fixture, adapter)
	evidence := scanReconciliationCommitEvidence(t, capture)
	opened := capture.opened
	entered, unblock, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce, closedOnce sync.Once
	adapter.closeHook = func() { closedOnce.Do(func() { close(released) }) }
	blockAt := adapter.checks + 2
	adapter.revalidate = func(ctx context.Context, count int) error {
		if count == blockAt {
			close(entered)
			<-unblock
			if _, err := opened.Stat("."); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(unblock) })
		_ = capture.Close()
		_ = evidence.Close()
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			t.Error("retired observation did not release its capture")
		}
	})
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	result := make(chan error, 1)
	go func() {
		_, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
			[]*rootBindingScanCapture{capture}, evidence, nil)
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reconciliation did not enter its controlled in-transaction observation")
	}
	fixture.task.cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled blocked observation did not abort deletion: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked filesystem observation retained the catalog transaction")
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("aborted observation changed catalog or dependent records")
	}
	assertNoCatalogTestNotification(t, notifications)
	check, cancelCheck := context.WithTimeout(fixture.ctx, time.Second)
	defer cancelCheck()
	if err := fixture.store.CheckOwnership(check); err != nil {
		t.Fatalf("blocked retired worker retained or destroyed the healthy owner: %v", err)
	}
	if err := fixture.store.WithOwnedTx(check, func(tx OwnedTx) error {
		_, err := tx.Exec(`INSERT INTO server_settings (key, value)
			VALUES ('observation-rollback-owner', 'available')`)
		return err
	}); err != nil {
		t.Fatalf("unrelated catalog work remained blocked: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
		t.Fatal("rollback closed a root still retained by filesystem work")
	default:
	}
	releaseOnce.Do(func() { close(unblock) })
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("completed filesystem work did not release its retired root")
	}
	if _, err := opened.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("completed retired observation kept the root open: %v", err)
	}
}
