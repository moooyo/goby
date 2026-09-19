package tasks

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestPhase3SystemEventNewOwnerConsumesPendingRangeAndTaskScansDoNotRecurse(t *testing.T) {
	f := newManagerFixture(t)
	first := f.addLibraries(t, 1)[0]
	saved := managerReplaceTriggers(t, f, ScheduleRule{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged})
	trigger := saved.Triggers[0]
	oldManager, _ := f.manager(t, false)
	managerWait(t, f.ctx, "old event manager readiness", func(context.Context) bool { return oldManager.Available() })
	if err := oldManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	startupSequence := phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.ServerStarted)

	// Commit a real source mutation after the old coordinator has stopped, while
	// its catalog owner can still commit. No dispatcher can consume this range.
	second := f.addLibraries(t, 1)[0]
	pendingSequence := phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.LibraryChanged)
	if pendingSequence != trigger.LastEventSequence+1 {
		t.Fatal("fixture did not retain one newly committed source event")
	}
	var cursor int64
	var runs, receipts int
	if err := f.pool.QueryRow(f.ctx, `SELECT last_event_sequence,(SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM task_system_event_receipts) FROM task_triggers WHERE id=$1`, trigger.ID).Scan(&cursor, &runs, &receipts); err != nil {
		t.Fatal(err)
	}
	if cursor != trigger.LastEventSequence || runs != 0 || receipts != 0 {
		t.Fatal("closed coordinator consumed the pending event")
	}
	if err := f.catalog.Close(f.ctx); err != nil {
		t.Fatal(err)
	}

	// Acquire a genuinely new catalog ownership connection and perform the same
	// recovery ordering as server startup. Recreating only Store is insufficient.
	successorProber := newManagerTestProber()
	successor, err := library.New(f.pool, successorProber, []string{f.root})
	if err != nil {
		t.Fatalf("new catalog owner could not acquire released ownership: %v", err)
	}
	f.catalogs = append(f.catalogs, successor)
	f.probers = append(f.probers, successorProber)
	recovered, err := New(f.pool, successor)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := recovered.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.catalog, f.store, f.prober = successor, recovered, successorProber

	var observedMu sync.Mutex
	observedItems := make(map[string]bool)
	successor.SetCatalogChangeListener(func(notification library.CatalogNotification) {
		observedMu.Lock()
		defer observedMu.Unlock()
		for _, change := range notification.Changes {
			if !change.IsFolder && !change.IsCollectionFolder {
				observedItems[change.ItemID] = true
			}
		}
	})
	manager, observed := f.manager(t, false)
	var admitted Run
	managerWait(t, f.ctx, "pending event admission by the new owner", func(ctx context.Context) bool {
		page, err := f.store.ListRuns(ctx, f.definition.ID, Page{Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalRecordCount > 1 {
			t.Fatal("pending range admitted more than one task run")
		}
		if len(page.Items) == 0 {
			return false
		}
		admitted = page.Items[0]
		return true
	})
	completed := managerWaitRun(t, f, admitted.ID, RunCompleted)
	if completed.Source != "system_event" || completed.TriggerID == nil || *completed.TriggerID != trigger.ID ||
		completed.TotalChildren != 2 || completed.CompletedChildren != 2 || completed.Scanned != 2 {
		t.Fatalf("recovered range did not execute the two real catalog scans: %+v", completed)
	}
	children, err := f.store.ListChildren(f.ctx, completed.ID, Page{Limit: 5})
	if err != nil || children.TotalRecordCount != 2 || len(children.Items) != 2 {
		t.Fatalf("read actual event scan children: %v", err)
	}
	for _, child := range children.Items {
		if child.State != ChildCompleted || child.ScanJobID == nil {
			t.Fatalf("event child did not own a completed scan: %+v", child)
		}
		job, err := successor.GetJob(f.ctx, *child.ScanJobID)
		if err != nil || job.Status != "Completed" || job.TaskChildID != child.ID || job.LibraryID != child.LibraryID {
			t.Fatalf("event work bypassed durable task-scan ownership: %+v, %v", job, err)
		}
	}
	for _, collection := range []managerTestLibrary{first, second} {
		var itemID string
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie' AND NOT is_folder", collection.id).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		observedMu.Lock()
		notified := observedItems[itemID]
		observedMu.Unlock()
		if !notified {
			t.Fatal("task-derived scan suppressed the client's committed LibraryChanged notification")
		}
		successorProber.mu.Lock()
		calls := successorProber.entered[collection.payload]
		successorProber.mu.Unlock()
		if calls != 1 {
			t.Fatalf("event recovery reprobed the same source %d times", calls)
		}
	}
	if observed.count(library.ScanAdmitted) != 2 {
		t.Fatal("new owner did not admit exactly its two task-owned scans")
	}
	if phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.LibraryChanged) != pendingSequence {
		t.Fatal("real task worker output recursively advanced the source event counter")
	}
	if changed, err := recovered.DispatchSystemEvents(f.ctx, 10); err != nil || changed {
		t.Fatalf("completed task output created another event range: %v", err)
	}
	var firstSequence, lastSequence int64
	var receiptRun, disposition string
	if err := f.pool.QueryRow(f.ctx, `SELECT first_sequence,last_sequence,run_id,disposition,
		(SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_system_event_receipts)
		FROM task_system_event_receipts WHERE trigger_id=$1`, trigger.ID).
		Scan(&firstSequence, &lastSequence, &receiptRun, &disposition, &runs, &receipts); err != nil {
		t.Fatal(err)
	}
	if firstSequence != trigger.LastEventSequence+1 || lastSequence != pendingSequence || receiptRun != completed.ID || disposition != "admitted" || runs != 1 || receipts != 1 {
		t.Fatal("new-owner recovery lost or repeated the exact pending event range")
	}
	if phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.ServerStarted) != startupSequence+1 {
		t.Fatal("new manager did not establish a distinct startup lifecycle")
	}
	if err := manager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}

	// Suppression must belong to task ownership, not the catalog lifetime. An
	// ordinary scan on this same successor still emits a committed task signal.
	if err := os.WriteFile(filepath.Join(first.path, "Independent.mp4"), []byte("independent-event-scan"), 0600); err != nil {
		t.Fatal(err)
	}
	independent, err := successor.StartScan(f.ctx, first.id)
	if err != nil {
		t.Fatal(err)
	}
	finished := refreshManagerWaitScan(t, f, independent.ID, "Completed")
	if finished.TaskChildID != "" || phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.LibraryChanged) <= pendingSequence {
		t.Fatal("task-derived marker leaked into a later independent scan")
	}
}
