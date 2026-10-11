package tasks

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskSummaryDecodeRetainsNullableUTCTimestamps(t *testing.T) {
	const timestamp = "2026-10-11T09:08:07+08:00"
	want, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"run","created_at":"` + timestamp + `","scheduled_for":"` + timestamp +
		`","started_at":"` + timestamp + `","deadline_at":"` + timestamp + `","stop_requested_at":"` + timestamp +
		`","finished_at":"` + timestamp + `","max_runtime_ticks":100}`
	var run RunSummary
	if err := decodeRow(taskSelectionJSONRow(raw), &run); err != nil {
		t.Fatal(err)
	}
	assertRunTimes := func(value RunSummary) {
		t.Helper()
		for _, value := range []*time.Time{&value.CreatedAt, value.ScheduledFor, value.StartedAt, value.DeadlineAt, value.StopRequestedAt, value.FinishedAt} {
			if value == nil || value.Location() != time.UTC || !value.Equal(want) {
				t.Fatalf("summary timestamp was not preserved in UTC: %v", value)
			}
		}
		if value.MaxRuntimeTicks == nil || *value.MaxRuntimeTicks != 100 {
			t.Fatal("summary lost its runtime limit")
		}
	}
	assertRunTimes(run)
	var definition DefinitionSummary
	if err := decodeRow(taskSelectionJSONRow(`{"created_at":"`+timestamp+`","updated_at":"`+timestamp+
		`","triggers":[{"created_at":"`+timestamp+`","updated_at":"`+timestamp+`","anchor_at":"`+timestamp+
		`","next_fire_at":"`+timestamp+`","last_due_at":"`+timestamp+`","retired_at":"`+timestamp+
		`"}],"current_run":`+raw+`,"last_run":`+raw+`}`), &definition); err != nil {
		t.Fatal(err)
	}
	if definition.CurrentRun == nil || definition.LastRun == nil || len(definition.Triggers) != 1 {
		t.Fatal("definition summary lost nested state")
	}
	assertRunTimes(*definition.CurrentRun)
	assertRunTimes(*definition.LastRun)
	trigger := definition.Triggers[0]
	for _, value := range []*time.Time{&definition.CreatedAt, &definition.UpdatedAt, &trigger.CreatedAt, &trigger.UpdatedAt,
		trigger.AnchorAt, trigger.NextFireAt, trigger.LastDueAt, trigger.RetiredAt} {
		if value == nil || value.Location() != time.UTC || !value.Equal(want) {
			t.Fatalf("definition or trigger timestamp was not preserved in UTC: %v", value)
		}
	}
	var empty RunSummary
	if err := decodeRow(taskSelectionJSONRow(`{"scheduled_for":null,"max_runtime_ticks":null,"started_at":null,
		"deadline_at":null,"stop_requested_at":null,"finished_at":null}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.ScheduledFor != nil || empty.MaxRuntimeTicks != nil || empty.StartedAt != nil || empty.DeadlineAt != nil ||
		empty.StopRequestedAt != nil || empty.FinishedAt != nil {
		t.Fatal("nullable summary fields became present zero values")
	}
	var noRuns DefinitionSummary
	if err := decodeRow(taskSelectionJSONRow(`{"current_run":null,"last_run":null,"triggers":null}`), &noRuns); err != nil ||
		noRuns.CurrentRun != nil || noRuns.LastRun != nil || noRuns.Triggers == nil || len(noRuns.Triggers) != 0 {
		t.Fatalf("empty definition summary changed its wire shape: %+v %v", noRuns, err)
	}
}

func TestTaskSummaryDecodeRejectsMalformedIncludedFields(t *testing.T) {
	for _, field := range []string{
		`"task_name":false`, `"request_id":17`, `"total_children":"1"`, `"max_runtime_ticks":"100"`,
		`"created_at":false`, `"scheduled_for":"not-a-timestamp"`,
	} {
		t.Run(field, func(t *testing.T) {
			raw := `{` + field + `}`
			var summary RunSummary
			if err := decodeRow(taskSelectionJSONRow(raw), &summary); err == nil {
				t.Fatal("run summary accepted an invalid included field")
			}
			for _, nested := range []string{"current_run", "last_run"} {
				var definition DefinitionSummary
				if err := decodeRow(taskSelectionJSONRow(`{"`+nested+`":`+raw+`}`), &definition); err == nil {
					t.Fatalf("definition summary accepted an invalid %s field", nested)
				}
			}
		})
	}
	for _, raw := range []string{`{"revision":{}}`, `{"triggers":{}}`, `{"triggers":[{"position":"1"}]}`} {
		var definition DefinitionSummary
		if err := decodeRow(taskSelectionJSONRow(raw), &definition); err == nil {
			t.Fatalf("definition summary accepted invalid included data: %s", raw)
		}
	}
}

func TestTaskSummariesNeverSerializeExecutionIdentityOrPayload(t *testing.T) {
	var run Run
	if err := decodeRow(taskSelectionJSONRow(`{"id":"visible-run","task_name":"Visible task","state":"running",
		"task_key":"private-key","task_emby_key":"private-compatibility-key","actor_kind":"application_key",
		"actor_user_id":"private-user","actor_session_id":"private-session","actor_application_key_id":13,
		"actor_client_session_id":"private-client","actor_peer_ip":"198.51.100.8","trigger_id":"private-trigger",
		"trigger_revision":7,"analysis_input":{"LibraryIds":["private-library"],"ItemIds":["private-item"]},
		"analysis_config_fingerprint":"private-fingerprint"}`), &run); err != nil {
		t.Fatal(err)
	}
	if run.authority.ApplicationKeyID != 13 || run.AnalysisInput == nil {
		t.Fatal("complete fixture did not retain its private execution facts")
	}
	assertPublic := func(value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["id"]) != `"visible-run"` || string(fields["task_name"]) != `"Visible task"` {
			t.Fatal("summary lost public display identity")
		}
		for _, omitted := range []string{"analysis_input", "analysis_config_fingerprint", "request_fingerprint",
			"task_key", "task_emby_key", "actor_kind", "actor_user_id", "actor_session_id", "actor_application_key_id",
			"actor_client_session_id", "actor_peer_ip", "trigger_id", "trigger_revision", "authority"} {
			if _, present := fields[omitted]; present {
				t.Fatalf("display summary exposed execution field %q", omitted)
			}
		}
	}
	assertPublic(run.Summary())
	definition := Definition{CurrentRun: &run, LastRun: &run}.Summary()
	assertPublic(definition.CurrentRun)
	assertPublic(definition.LastRun)
}
