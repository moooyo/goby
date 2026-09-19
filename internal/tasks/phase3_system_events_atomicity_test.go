package tasks

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// The trigger runs inside the actual PostgreSQL transaction. It does not
// replace the store, transaction owner, dispatcher, or source mutation.
func phase3SystemEventFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, clause string) func() {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION phase3_system_event_reject() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'phase3 system event injected failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "CREATE TRIGGER phase3_system_event_reject "+clause+" EXECUTE FUNCTION phase3_system_event_reject()"); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	remove := func() {
		t.Helper()
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := pool.Exec(cleanup, "DROP TRIGGER phase3_system_event_reject ON "+pgx.Identifier{table}.Sanitize()); err != nil {
				t.Errorf("remove event failure fixture: %v", err)
			}
			if _, err := pool.Exec(cleanup, "DROP FUNCTION phase3_system_event_reject()"); err != nil {
				t.Errorf("remove event failure function: %v", err)
			}
		})
	}
	t.Cleanup(remove)
	return remove
}

func phase3RequireEventFailure(t *testing.T, err error) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || databaseError.Message != "phase3 system event injected failure" {
		t.Fatalf("operation did not reach the real event failure trigger: %v", err)
	}
}

func phase3SystemEventSequence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, event systemevents.Event) int64 {
	t.Helper()
	var sequence int64
	if err := pool.QueryRow(ctx, "SELECT sequence FROM task_system_events WHERE name=$1", string(event)).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func phase3TaskEventSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e),
		'triggers',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM task_triggers t),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
		'children',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM task_run_children c),
		'occurrences',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM task_occurrences o),
		'receipts',(SELECT jsonb_agg(to_jsonb(e) ORDER BY trigger_id,schedule_revision,last_sequence) FROM task_system_event_receipts e),
		'activity',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM activity_entries a))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPhase3SystemEventCounterFailureRollsBackAdministratorLibraryMutation(t *testing.T) {
	f := newManagerFixture(t)
	var notifications atomic.Int64
	f.catalog.SetCatalogChangeListener(func(value library.CatalogNotification) {
		if value.Resync || len(value.Changes) != 0 {
			notifications.Add(1)
		}
	})
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'libraries',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
			'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
			'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
			'activity',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM activity_entries a),
			'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	sequence := phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.LibraryChanged)
	remove := phase3SystemEventFailure(t, f.ctx, f.pool, "task_system_events",
		"BEFORE UPDATE ON task_system_events FOR EACH ROW WHEN (NEW.name='LibraryChanged')")
	created, err := f.catalog.CreateLibraryAsAdministrator(f.ctx, f.actor.Principal, f.actor.Audience, "Atomic event source", "movies", []string{f.root})
	phase3RequireEventFailure(t, err)
	if created.ID != "" || snapshot() != before || notifications.Load() != 0 {
		t.Fatal("failed event counter exposed a partial registration, audit, signal or client notification")
	}
	remove()
	created, err = f.catalog.CreateLibraryAsAdministrator(f.ctx, f.actor.Principal, f.actor.Audience, "Atomic event source", "movies", []string{f.root})
	if err != nil || created.ID == "" {
		t.Fatalf("clean retry of source mutation failed: %v", err)
	}
	if phase3SystemEventSequence(t, f.ctx, f.pool, systemevents.LibraryChanged) != sequence+1 || notifications.Load() != 1 {
		t.Fatal("successful source retry did not publish exactly one committed signal and notification")
	}
	var libraries, roots, items int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM libraries WHERE id=$1),
		(SELECT count(*) FROM library_roots WHERE library_id=$1),(SELECT count(*) FROM items WHERE library_id=$1)`, created.ID).Scan(&libraries, &roots, &items); err != nil || libraries != 1 || roots != 1 || items != 1 {
		t.Fatalf("retry did not persist one complete registration: libraries=%d roots=%d items=%d error=%v", libraries, roots, items, err)
	}
}

func TestPhase3SystemEventReceiptAndCursorFailuresRollbackAdmissionAndRetry(t *testing.T) {
	for _, failure := range []struct{ name, table, clause string }{
		{"receipt", "task_system_event_receipts", "BEFORE INSERT ON task_system_event_receipts FOR EACH ROW"},
		{"cursor", "task_triggers", "BEFORE UPDATE OF last_event_sequence ON task_triggers FOR EACH ROW WHEN (NEW.kind='system_event' AND NEW.last_event_sequence>OLD.last_event_sequence)"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
			saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision,
				ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged}}})
			if err != nil {
				t.Fatal(err)
			}
			cursor := saved.Triggers[0].LastEventSequence
			if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				for range 3 {
					if err := systemevents.Record(tx.Exec, systemevents.LibraryChanged); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := phase3TaskEventSnapshot(t, ctx, pool)
			remove := phase3SystemEventFailure(t, ctx, pool, failure.table, failure.clause)
			changed, err := store.DispatchSystemEvents(ctx, 1)
			phase3RequireEventFailure(t, err)
			if changed || phase3TaskEventSnapshot(t, ctx, pool) != before {
				t.Fatal("failed event admission retained a run, child, occurrence, receipt, activity or advanced cursor")
			}
			remove()
			if changed, err := store.DispatchSystemEvents(ctx, 10); err != nil || !changed {
				t.Fatalf("event range did not retry after removing the SQL failure: %v", err)
			}
			if changed, err := store.DispatchSystemEvents(ctx, 10); err != nil || changed {
				t.Fatalf("consumed event range retried again: %v", err)
			}
			var first, last, persistedCursor int64
			var runID, disposition string
			var runs, children, occurrences, receipts int
			if err := pool.QueryRow(ctx, `SELECT e.first_sequence,e.last_sequence,e.run_id,e.disposition,t.last_event_sequence,
				(SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_run_children),
				(SELECT count(*) FROM task_occurrences),(SELECT count(*) FROM task_system_event_receipts)
				FROM task_system_event_receipts e JOIN task_triggers t ON t.id=e.trigger_id WHERE e.trigger_id=$1`, saved.Triggers[0].ID).
				Scan(&first, &last, &runID, &disposition, &persistedCursor, &runs, &children, &occurrences, &receipts); err != nil {
				t.Fatal(err)
			}
			if first != cursor+1 || last != cursor+3 || persistedCursor != last || disposition != "admitted" || runs != 1 || children != 1 || occurrences != 1 || receipts != 1 {
				t.Fatal("retry did not atomically consume the original exact range into one admission")
			}
			run, err := store.GetRun(ctx, runID)
			if err != nil || run.State != RunPending || run.Source != "system_event" || run.TotalChildren != 1 {
				t.Fatalf("retry lost its actual execution snapshot: %+v, %v", run, err)
			}
		})
	}
}
