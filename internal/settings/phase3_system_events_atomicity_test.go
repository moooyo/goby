package settings

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPhase3ConfigurationEventFailureRollsBackSettingsAuditAndPublication(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	rowBefore := settingsRowSnapshot(t, ctx, pool)
	var sequenceBefore, activityBefore int64
	if err := pool.QueryRow(ctx, `SELECT sequence,(SELECT count(*) FROM activity_entries)
		FROM task_system_events WHERE name='ConfigurationChanged'`).Scan(&sequenceBefore, &activityBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION phase3_configuration_event_reject() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'phase3 configuration event injected failure'; END $$;
		CREATE TRIGGER phase3_configuration_event_reject BEFORE UPDATE ON task_system_events
		FOR EACH ROW WHEN (NEW.name='ConfigurationChanged') EXECUTE FUNCTION phase3_configuration_event_reject()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, `DROP TRIGGER IF EXISTS phase3_configuration_event_reject ON task_system_events;
			DROP FUNCTION IF EXISTS phase3_configuration_event_reject()`); err != nil {
			t.Errorf("remove configuration event failure fixture: %v", err)
		}
	})
	management := initial.Management
	management.Tasks.MaxConcurrent++
	request := UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides, Management: &management}
	result, err := store.Update(ctx, actor, request)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || databaseError.Message != "phase3 configuration event injected failure" || result.Revision != 0 {
		t.Fatalf("settings mutation did not reach the real event counter failure: revision=%d error=%v", result.Revision, err)
	}
	var sequenceAfter, activityAfter int64
	if err := pool.QueryRow(ctx, `SELECT sequence,(SELECT count(*) FROM activity_entries)
		FROM task_system_events WHERE name='ConfigurationChanged'`).Scan(&sequenceAfter, &activityAfter); err != nil {
		t.Fatal(err)
	}
	if settingsRowSnapshot(t, ctx, pool) != rowBefore || !reflect.DeepEqual(store.Snapshot(), initial) || sequenceAfter != sequenceBefore || activityAfter != activityBefore {
		t.Fatal("failed configuration event partially committed settings, audit, counter or in-memory publication")
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER phase3_configuration_event_reject ON task_system_events"); err != nil {
		t.Fatal(err)
	}
	committed, err := store.Update(ctx, actor, request)
	if err != nil || committed.Revision != initial.Revision+1 || committed.Management.Tasks.MaxConcurrent != management.Tasks.MaxConcurrent {
		t.Fatalf("identical request could not retry the original revision after rollback: %+v, %v", committed, err)
	}
	if err := pool.QueryRow(ctx, `SELECT sequence,(SELECT count(*) FROM activity_entries)
		FROM task_system_events WHERE name='ConfigurationChanged'`).Scan(&sequenceAfter, &activityAfter); err != nil {
		t.Fatal(err)
	}
	if sequenceAfter != sequenceBefore+1 || activityAfter != activityBefore+1 || !reflect.DeepEqual(store.Snapshot(), committed) {
		t.Fatal("successful retry did not publish one settings change with one audit and event")
	}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), committed) {
		t.Fatalf("reloaded configuration differs from the committed retry: %v", err)
	}
}
