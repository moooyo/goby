//go:build linux

package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestTaskSummaryProjectionsMatchCompleteReadsWithMaximumSelection(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	run := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	selection := &library.AnalysisSelection{Force: true, LibraryIDs: []string{}, ItemIDs: []string{}}
	for index := range 64 {
		selection.LibraryIDs = append(selection.LibraryIDs, fmt.Sprintf("%032x", index+1))
	}
	for index := range 256 {
		selection.ItemIDs = append(selection.ItemIDs, fmt.Sprintf("%032x", index+1000))
	}
	input, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	// Persist a large historical input independently of live catalog membership.
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET analysis_input=$2,
		max_runtime_ticks=36000000000,deadline_at=started_at+interval '1 hour' WHERE id=$1`, run.ID, input); err != nil {
		t.Fatal(err)
	}
	lastID := strings.Repeat("a", 32)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_runs
		(id,task_id,state,source,task_key,task_emby_key,task_name,actor_user_id,actor_session_id,actor_kind,
		analysis_input,analysis_config_fingerprint,created_at,started_at,finished_at,error_code,error_message,
		total_children,terminal_children,completed_children,failed_children,cancelled_children,interrupted_children,
		unavailable_children,scanned,added,updated)
		SELECT $2,task_id,'failed',source,task_key,task_emby_key,task_name,actor_user_id,actor_session_id,actor_kind,
		analysis_input,analysis_config_fingerprint,created_at-interval '1 hour',created_at-interval '1 hour',
		created_at-interval '1 minute','projection_failure','retained failure details',27,27,11,7,4,3,2,987,654,321
		FROM task_runs WHERE id=$1`, run.ID, lastID); err != nil {
		t.Fatal(err)
	}
	complete, err := f.store.GetRun(f.ctx, run.ID)
	if err != nil || !reflect.DeepEqual(complete.AnalysisInput, selection) {
		t.Fatalf("complete read lost its maximum analysis selection: %v", err)
	}
	summary, err := f.store.GetRunSummary(f.ctx, run.ID)
	if err != nil || !reflect.DeepEqual(summary, complete.Summary()) {
		t.Fatalf("run summary changed visible fields: got=%+v want=%+v error=%v", summary, complete.Summary(), err)
	}
	var completeJSON, summaryJSON []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(r) - 'request_fingerprint', `+runSummaryProjection+`
		FROM task_runs r WHERE r.id=$1`, run.ID).Scan(&completeJSON, &summaryJSON); err != nil {
		t.Fatal(err)
	}
	// Compare the transferred projections, not just their decoded Go values.
	if len(completeJSON)-len(summaryJSON) < len(input) {
		t.Fatal("display SQL transferred the omitted maximum selection payload")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(summaryJSON, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"analysis_input", "analysis_config_fingerprint", "actor_user_id", "actor_session_id",
		"actor_application_key_id", "actor_client_session_id", "actor_peer_ip", "request_fingerprint"} {
		if _, present := fields[name]; present {
			t.Fatalf("display SQL serialized omitted execution field %q", name)
		}
	}
	definition, err := f.store.Get(f.ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := f.store.GetSummary(f.ctx, run.TaskID)
	if err != nil || !reflect.DeepEqual(projected, definition.Summary()) {
		t.Fatalf("definition summary changed its committed display snapshot: %v", err)
	}
	if projected.CurrentRun == nil || projected.CurrentRun.ID != run.ID || projected.LastRun == nil ||
		projected.LastRun.ID != lastID || len(projected.Triggers) == 0 {
		t.Fatalf("summary omitted current, last, or trigger state: %+v", projected)
	}
	definitions, err := f.store.List(f.ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := f.store.ListSummaries(f.ctx, ListOptions{})
	if err != nil || len(summaries) != len(definitions) {
		t.Fatalf("definition summary list changed membership: %v", err)
	}
	for index := range definitions {
		if !reflect.DeepEqual(summaries[index], definitions[index].Summary()) {
			t.Fatalf("definition summary list changed entry %d", index)
		}
	}
	history, err := f.store.ListRuns(f.ctx, run.TaskID, Page{Limit: MaxPageLimit})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.store.ListRunSummaries(f.ctx, run.TaskID, Page{Limit: MaxPageLimit})
	if err != nil || page.TotalRecordCount != history.TotalRecordCount || len(page.Items) != len(history.Items) {
		t.Fatalf("history summary changed count or membership: %+v %v", page, err)
	}
	for index := range history.Items {
		if !reflect.DeepEqual(page.Items[index], history.Items[index].Summary()) {
			t.Fatalf("history summary changed entry %d", index)
		}
	}
}

func TestTaskSummaryOmitsMalformedAnalysisWithoutWeakeningExecutionReads(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	run, child, work := f.claim(t)
	complete, err := f.store.GetRun(f.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	if err := f.pool.QueryRow(f.ctx, `SELECT executor_token FROM task_run_children WHERE id=$1`, child.ID).Scan(&token); err != nil {
		t.Fatal(err)
	}
	// The schema bounds the arrays but leaves element decoding to complete reads.
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET analysis_input='{"LibraryIds":[17],"ItemIds":[],"Force":false}'::jsonb WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	want := complete.Summary()
	if summary, err := f.store.GetRunSummary(f.ctx, run.ID); err != nil || !reflect.DeepEqual(summary, want) {
		t.Fatalf("omitted analysis payload broke a run summary: %+v %v", summary, err)
	}
	if definition, err := f.store.GetSummary(f.ctx, run.TaskID); err != nil || definition.CurrentRun == nil || !reflect.DeepEqual(*definition.CurrentRun, want) {
		t.Fatalf("omitted analysis payload broke a definition summary: %+v %v", definition, err)
	}
	if page, err := f.store.ListRunSummaries(f.ctx, run.TaskID, Page{}); err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], want) {
		t.Fatalf("omitted analysis payload broke a history summary: %+v %v", page, err)
	}
	listed, err := f.store.ListSummaries(f.ctx, ListOptions{})
	if err != nil {
		t.Fatalf("omitted analysis payload broke definition listing: %v", err)
	}
	found := false
	for _, definition := range listed {
		if definition.ID == run.TaskID {
			found = definition.CurrentRun != nil && reflect.DeepEqual(*definition.CurrentRun, want)
		}
	}
	if !found {
		t.Fatal("definition listing omitted the affected task")
	}
	for _, test := range []struct {
		name string
		read func() error
	}{
		{"run", func() error { _, err := f.store.GetRun(f.ctx, run.ID); return err }},
		{"definition", func() error { _, err := f.store.Get(f.ctx, run.TaskID); return err }},
		{"definition key", func() error { _, err := f.store.GetByKey(f.ctx, run.TaskKey); return err }},
		{"definitions", func() error { _, err := f.store.List(f.ctx, ListOptions{}); return err }},
		{"history", func() error { _, err := f.store.ListRuns(f.ctx, run.TaskID, Page{}); return err }},
		{"active runs", func() error { _, err := f.store.ListActiveRuns(f.ctx, Page{}); return err }},
		{"execution approval", func() error { _, err := f.store.executionWork(f.ctx, run, child, token); return err }},
		{"approved publication fence", func() error {
			return f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var invalid *json.UnmarshalTypeError
			if err := test.read(); !errors.As(err, &invalid) {
				t.Fatalf("complete path did not reject the malformed analysis element: %v", err)
			}
		})
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=false,revision=revision+1,
		emby_key='stored-compatibility-key',schedule_timezone='Asia/Shanghai' WHERE id=$1`, run.TaskID); err != nil {
		t.Fatal(err)
	}
	wantTarget := DefinitionTarget{ID: run.TaskID, Key: run.TaskKey, EmbyKey: "stored-compatibility-key",
		Enabled: false, Revision: f.definitions[run.TaskKey].Revision + 1, ScheduleTimezone: "Asia/Shanghai"}
	assertTarget := func() {
		t.Helper()
		byID, err := f.store.GetTarget(f.ctx, run.TaskID)
		if err != nil || byID != wantTarget {
			t.Fatalf("target lost base fields or decoded an omitted nested value: %+v %v", byID, err)
		}
		byKey, err := f.store.GetTargetByKey(f.ctx, run.TaskKey)
		if err != nil || byKey != wantTarget {
			t.Fatalf("target key lookup changed its stored compatibility key: %+v %v", byKey, err)
		}
	}
	assertTarget()
	changed, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET created_at='infinity'::timestamptz
		WHERE task_id=$1 AND retired_at IS NULL`, run.TaskID)
	if err != nil || changed.RowsAffected() == 0 {
		t.Fatalf("could not prepare an undecodable nested trigger: %v", err)
	}
	if _, err := f.store.GetSummary(f.ctx, run.TaskID); err == nil {
		t.Fatal("definition summary ignored its included malformed trigger timestamp")
	}
	assertTarget()
}

func TestTaskSummaryOrderingFiltersAndEmptyPages(t *testing.T) {
	ctx, pool, _, store, _, definition := taskRepository(t, 0)
	empty, err := store.GetSummary(ctx, definition.ID)
	if err != nil || empty.CurrentRun != nil || empty.LastRun != nil || empty.Triggers == nil {
		t.Fatalf("empty definition fabricated nested state: %+v %v", empty, err)
	}
	page, err := store.ListRunSummaries(ctx, definition.ID, Page{})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.TotalRecordCount != 0 || page.Limit != DefaultPageLimit {
		t.Fatalf("empty history lost its page contract: %+v %v", page, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at,finished_at)
		SELECT repeat(letter,32),$1,state,'manual',$2,$3,created::timestamptz,finished::timestamptz
		FROM (VALUES('a','completed','2026-01-01T00:00:00Z','2026-01-05T00:00:00Z'),
		('b','failed','2025-12-31T00:00:00Z','2026-01-05T00:00:00Z'),
		('c','cancelled','2026-01-04T00:00:00Z','2026-01-04T12:00:00Z'),
		('d','pending','2026-01-06T00:00:00Z',NULL)) fixture(letter,state,created,finished)`,
		definition.ID, definition.Key, definition.Name); err != nil {
		t.Fatal(err)
	}
	projected, err := store.GetSummary(ctx, definition.ID)
	if err != nil || projected.CurrentRun == nil || projected.CurrentRun.ID != strings.Repeat("d", 32) ||
		projected.LastRun == nil || projected.LastRun.ID != strings.Repeat("b", 32) {
		t.Fatalf("definition summary lost independent active and completed ordering: %+v %v", projected, err)
	}
	page, err = store.ListRunSummaries(ctx, definition.ID, Page{StartIndex: 1, Limit: 2})
	if err != nil || page.TotalRecordCount != 4 || len(page.Items) != 2 || page.StartIndex != 1 || page.Limit != 2 ||
		page.Items[0].ID != strings.Repeat("c", 32) || page.Items[1].ID != strings.Repeat("a", 32) {
		t.Fatalf("summary page lost created-time ordering or page metadata: %+v %v", page, err)
	}
	page, err = store.ListRunSummaries(ctx, definition.ID, Page{StartIndex: 99, Limit: 2})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.TotalRecordCount != 4 {
		t.Fatalf("out-of-range summary page lost its total: %+v %v", page, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name,is_hidden,enabled)
		VALUES(repeat('e',32),'projection-enabled','Projection tie',true,true),
		(repeat('f',32),'projection-disabled','Projection tie',true,false)`); err != nil {
		t.Fatal(err)
	}
	visible, hidden, enabled, disabled := false, true, true, false
	for _, test := range []struct {
		options ListOptions
		ids     []string
	}{
		{ListOptions{IsHidden: &hidden}, []string{strings.Repeat("e", 32), strings.Repeat("f", 32)}},
		{ListOptions{IsHidden: &hidden, IsEnabled: &enabled}, []string{strings.Repeat("e", 32)}},
		{ListOptions{IsHidden: &hidden, IsEnabled: &disabled}, []string{strings.Repeat("f", 32)}},
		{ListOptions{IsHidden: &visible, IsEnabled: &disabled}, []string{}},
	} {
		definitions, err := store.ListSummaries(ctx, test.options)
		if err != nil || definitions == nil || len(definitions) != len(test.ids) {
			t.Fatalf("summary filters changed membership: %+v %v", definitions, err)
		}
		for index, id := range test.ids {
			if definitions[index].ID != id {
				t.Fatal("same-name summary ordering lost its ID tie-break")
			}
		}
	}
	missing := strings.Repeat("0", 32)
	for _, read := range []func() error{
		func() error { _, err := store.GetSummary(ctx, missing); return err },
		func() error { _, err := store.GetRunSummary(ctx, missing); return err },
		func() error { _, err := store.GetTarget(ctx, missing); return err },
		func() error { _, err := store.GetTargetByKey(ctx, "missing-projection-task"); return err },
		func() error { _, err := store.ListRunSummaries(ctx, missing, Page{}); return err },
	} {
		if err := read(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing summary resource returned a different error: %v", err)
		}
	}
	for _, invalid := range []Page{{StartIndex: -1}, {Limit: -1}, {Limit: MaxPageLimit + 1}} {
		if _, err := store.ListRunSummaries(ctx, definition.ID, invalid); err == nil {
			t.Fatalf("summary history accepted invalid pagination: %+v", invalid)
		}
	}

	// Commit definition, trigger, and run changes together while both summary
	// entry points read them. Each response must contain one committed epoch.
	if _, err := pool.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind)
		VALUES(repeat('1',32),$1,1,0,'startup')`, definition.ID); err != nil {
		t.Fatal(err)
	}
	writeEpoch := func(writeContext context.Context, epoch int) error {
		_, err := pool.Exec(writeContext, `WITH changed AS (
			UPDATE task_definitions SET description=$2,revision=revision+1 WHERE id=$1 RETURNING id,revision
		), history AS (
			UPDATE task_runs SET error_message=$2 WHERE task_id IN (SELECT id FROM changed)
		)
		UPDATE task_triggers SET calculation_error=$2,schedule_revision=changed.revision
		FROM changed WHERE task_id=changed.id`, definition.ID, fmt.Sprintf("epoch-%d", epoch))
		return err
	}
	if err := writeEpoch(ctx, 0); err != nil {
		t.Fatal(err)
	}
	writerContext, cancelWriter := context.WithCancel(ctx)
	done := make(chan struct{})
	writerErrors := make(chan error, 1)
	go func() {
		defer close(done)
		for epoch := 1; epoch <= 64; epoch++ {
			if err := writeEpoch(writerContext, epoch); err != nil {
				writerErrors <- err
				return
			}
		}
		writerErrors <- nil
	}()
	t.Cleanup(func() { cancelWriter(); <-done })
	assertEpoch := func(value DefinitionSummary) {
		t.Helper()
		if value.CurrentRun == nil || value.LastRun == nil || len(value.Triggers) != 1 ||
			value.Description != value.CurrentRun.ErrorMessage || value.Description != value.LastRun.ErrorMessage ||
			value.Description != value.Triggers[0].CalculationError || value.Revision != value.Triggers[0].ScheduleRevision {
			t.Fatalf("summary mixed committed definition, trigger, or run snapshots: %+v", value)
		}
	}
	for range 32 {
		value, err := store.GetSummary(ctx, definition.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertEpoch(value)
		values, err := store.ListSummaries(ctx, ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value.ID == definition.ID {
				assertEpoch(value)
			}
		}
	}
	if err := <-writerErrors; err != nil {
		t.Fatal(err)
	}
}
