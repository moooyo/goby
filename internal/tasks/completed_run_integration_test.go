package tasks

import (
	"strings"
	"testing"
)

func TestTaskRepositoryCompletedRunOrderingAndEmptyHistory(t *testing.T) {
	ctx, pool, _, store, _, definition := taskRepository(t, 0)
	assertProjection := func(lastID, activeID string) {
		t.Helper()
		byID, err := store.Get(ctx, definition.ID)
		if err != nil {
			t.Fatal(err)
		}
		byKey, err := store.GetByKey(ctx, definition.Key)
		if err != nil {
			t.Fatal(err)
		}
		listed, err := store.List(ctx, ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		projections := []Definition{byID, byKey}
		for _, entry := range listed {
			if entry.ID == definition.ID {
				projections = append(projections, entry)
			}
		}
		if len(projections) != 3 {
			t.Fatal("task list omitted or duplicated the selected definition")
		}
		for _, entry := range projections {
			if lastID == "" {
				if entry.LastRun != nil {
					t.Fatal("empty completed history fabricated a last result")
				}
			} else if entry.LastRun == nil || entry.LastRun.ID != lastID {
				t.Fatalf("last completed run lost finished-time ordering or ID tie-break: %+v", entry.LastRun)
			}
			if activeID == "" {
				if entry.CurrentRun != nil {
					t.Fatal("empty active history fabricated a current run")
				}
			} else if entry.CurrentRun == nil || entry.CurrentRun.ID != activeID {
				t.Fatalf("completed-run lookup changed the independent active run: %+v", entry.CurrentRun)
			}
		}
	}
	assertProjection("", "")
	active := strings.Repeat("d", 32)
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at)
		VALUES($1,$2,'pending','manual',$3,$4,'2026-01-06T00:00:00Z')`, active, definition.ID, definition.Key, definition.Name); err != nil {
		t.Fatal(err)
	}
	assertProjection("", active)
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at,finished_at)
		SELECT repeat(letter,32),$1,state,'manual',$2,$3,created::timestamptz,finished::timestamptz
		FROM (VALUES('a','completed','2026-01-01T00:00:00Z','2026-01-05T00:00:00Z'),
		('b','failed','2025-12-31T00:00:00Z','2026-01-05T00:00:00Z'),
		('c','cancelled','2026-01-04T00:00:00Z','2026-01-04T12:00:00Z')) fixture(letter,state,created,finished)`,
		definition.ID, definition.Key, definition.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name)
		VALUES(repeat('e',32),'completed-index-other','Other task');
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,finished_at)
		VALUES(repeat('f',32),repeat('e',32),'completed','manual','completed-index-other','Other task','2026-02-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertProjection(strings.Repeat("b", 32), active)
	history, err := store.ListRuns(ctx, definition.ID, Page{Limit: 10})
	if err != nil || history.TotalRecordCount != 4 || len(history.Items) != 4 {
		t.Fatalf("task history changed after completed-result lookup: %+v, %v", history, err)
	}
	for index, letter := range []string{"d", "c", "a", "b"} {
		if history.Items[index].ID != strings.Repeat(letter, 32) {
			t.Fatal("task history must retain its independent created-time ordering")
		}
	}
}
