package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/tasks"
)

func TestAdminTaskDTOsPreserveDecimalPrecisionAndExcludePrivateIdentity(t *testing.T) {
	wireObject := func(value any) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if err := json.Unmarshal(encoded, &object); err != nil {
			t.Fatal(err)
		}
		return object
	}
	ticks := int64(9007199254740993)
	first, second := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)
	local := first.In(time.FixedZone("fixture-offset", 8*60*60))
	blocked := first.Add(-time.Hour)
	secret := "private-actor-marker"
	run := tasks.Run{ID: strings.Repeat("a", 32), TaskID: strings.Repeat("b", 32), State: tasks.RunRunning,
		TaskName: "Scan media library", Source: "native", ActorSessionID: secret, ActorUserID: secret, ActorKind: secret,
		TaskKey: secret, TaskEmbyKey: secret, TriggerID: &secret, TriggerRevision: &ticks, MaxRuntimeTicks: &ticks,
		CreatedAt: first, ScheduledFor: &local, StartedAt: &local, DeadlineAt: &local, StopRequestedAt: &local, FinishedAt: &local,
		TotalChildren: 2, CompletedChildren: 1}
	definition := tasks.Definition{ID: run.TaskID, Key: tasks.LibraryScanKey, Revision: ticks, Name: run.TaskName, Enabled: true,
		Triggers: []tasks.Trigger{{ID: "later", Kind: "interval", IntervalTicks: &ticks, NextFireAt: &second},
			{ID: "earlier", Kind: "daily", TimeOfDayTicks: &ticks, NextFireAt: &local},
			{ID: "blocked", Kind: "weekly", NextFireAt: &blocked, CalculationError: "schedule_out_of_range"}}, CurrentRun: &run}
	value := wireObject(taskDefinitionDTO(definition))
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "actor_") || strings.Contains(string(encoded), "TriggerRevision") {
		t.Fatal("task DTO exposed private actor or trigger provenance")
	}
	if value["Revision"] != "9007199254740993" || value["NextRunAt"] != first.Format(time.RFC3339Nano) {
		t.Fatal("task DTO lost exact revision or earliest occurrence")
	}
	if wireObject(taskRunDTO(run))["MaxRuntimeTicks"] != "9007199254740993" || wireObject(taskTriggerDTO(definition.Triggers[0]))["IntervalTicks"] != "9007199254740993" {
		t.Fatal("task DTO rounded tick values")
	}
	empty := wireObject(taskDefinitionDTO(tasks.Definition{}))
	for _, field := range []string{"CurrentRun", "LastRun", "NextRunAt"} {
		if value, exists := empty[field]; !exists || value != nil {
			t.Errorf("empty task DTO omitted the explicit null field %s", field)
		}
	}
	if triggers, ok := empty["Triggers"].([]any); !ok || len(triggers) != 0 {
		t.Fatal("empty task DTO omitted its empty trigger array")
	}
	definition.Enabled = false
	disabled := wireObject(taskDefinitionDTO(definition))
	if next, exists := disabled["NextRunAt"]; !exists || next != nil {
		t.Fatal("disabled task advertised a next scheduled run")
	}
	if triggers, ok := disabled["Triggers"].([]any); !ok || len(triggers) != 3 {
		t.Fatal("disabled task lost its retained trigger configuration")
	}
	for _, test := range []struct {
		populated, empty map[string]any
		fields           []string
	}{
		{wireObject(taskRunDTO(run)), wireObject(taskRunDTO(tasks.Run{})), []string{"ScheduledFor", "StartedAt", "DeadlineAt", "StopRequestedAt", "FinishedAt"}},
		{wireObject(taskChildDTO(tasks.Child{StartedAt: &local, FinishedAt: &local})), wireObject(taskChildDTO(tasks.Child{})), []string{"StartedAt", "FinishedAt"}},
		{wireObject(taskTriggerDTO(definition.Triggers[1])), wireObject(taskTriggerDTO(tasks.Trigger{})), []string{"NextFireAt"}},
	} {
		for _, field := range test.fields {
			if test.populated[field] != first.Format(time.RFC3339Nano) {
				t.Errorf("optional task time %s did not serialize in UTC", field)
			}
			if value, exists := test.empty[field]; !exists || value != nil {
				t.Errorf("absent optional task time %s did not serialize as explicit null", field)
			}
		}
	}
}

func TestAdminTaskStrictBodiesRejectAmbiguousWrites(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType, query string
		status                         int
	}{
		{"missing-json", `{}`, "", "", http.StatusUnsupportedMediaType},
		{"wrong-mime", `{}`, "text/plain", "", http.StatusUnsupportedMediaType},
		{"missing-object", ``, "application/json", "", http.StatusBadRequest},
		{"null-object", `null`, "application/json", "", http.StatusBadRequest},
		{"array", `[]`, "application/json", "", http.StatusBadRequest},
		{"duplicate", `{"RequestId":"one","RequestId":"two"}`, "application/json", "", http.StatusBadRequest},
		{"wrong-case", `{"requestId":"one"}`, "application/json", "", http.StatusBadRequest},
		{"unknown", `{"ForceProbe":true}`, "application/json", "", http.StatusBadRequest},
		{"trailing-object", `{} {}`, "application/json", "", http.StatusBadRequest},
		{"oversized", `{"RequestId":"` + strings.Repeat("a", maxAdminTaskBodyBytes) + `"}`, "application/json", "", http.StatusBadRequest},
		{"query", `{}`, "application/json", "?ForceProbe=true", http.StatusBadRequest},
		{"empty-query", `{}`, "application/json", "?", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/admin/v1/tasks/"+strings.Repeat("a", 32)+"/runs"+test.query, strings.NewReader(test.body))
			if test.contentType != "" {
				r.Header.Set("Content-Type", test.contentType)
			}
			w := httptest.NewRecorder()
			if _, ok := adminTaskBody(w, r, []string{"RequestId"}, nil); ok || w.Code != test.status {
				t.Fatalf("strict task body accepted ambiguous input or returned %d, want %d", w.Code, test.status)
			}
		})
	}
}

func TestAdminTaskTriggerInputRejectsDuplicateAndLossyFields(t *testing.T) {
	for _, body := range []string{
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":[{"Kind":"daily","Kind":"weekly"}]}`,
		`{"Revision":1,"ScheduleTimezone":"UTC","Triggers":[]}`,
		`{"Revision":"01","ScheduleTimezone":"UTC","Triggers":[]}`,
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":null}`,
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":[{"Kind":"interval","IntervalTicks":10000000}]}`,
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":[{"Kind":"interval","IntervalTicks":"01"}]}`,
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":[{"Kind":"interval","AnchorAt":"2026-09-10T00:00:00Z"}]}`,
		`{"Revision":"1","ScheduleTimezone":"UTC","Triggers":[{"Kind":"weekly","DayOfWeek":"1"}]}`,
	} {
		r := httptest.NewRequest(http.MethodPut, "/admin/v1/tasks/"+strings.Repeat("a", 32)+"/triggers", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := decodeAdminTaskTriggers(w, r, true); ok || w.Code != http.StatusBadRequest {
			t.Fatal("native trigger input accepted duplicate, unsupported, or imprecise fields")
		}
	}
}
