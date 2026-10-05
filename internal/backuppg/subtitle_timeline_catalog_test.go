package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubtitleTimelineRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(59, "subtitle_timeline_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(60, "subtitle_timeline_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 60 || !equalJSON(prefix, migrations[:59]) || migrations[59].Name != "0060_subtitle_timelines.sql" ||
		!equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("subtitle timelines changed the historical migration prefix or sequence ownership")
	}
	newTables := map[string][]string{
		"subtitle_timeline_queue":    {"item_id", "operation_id", "requested_revision", "completed_revision", "claimed_revision", "force", "manual", "actor_user_id", "actor_session_id", "state", "run_id", "child_id", "source_revision", "reused", "error_code", "requested_at", "started_at", "finished_at"},
		"subtitle_timeline_requests": {"request_id", "fingerprint", "queued", "created_at"},
	}
	var retained []TableSpec
	added := 0
	for _, table := range current.Tables {
		columns, exists := newTables[table.Name]
		if !exists {
			retained = append(retained, table)
			continue
		}
		added++
		if !equalJSON(table.Columns, columns) || !equalJSON(table.PrimaryKey, columns[:1]) {
			t.Fatalf("subtitle timeline recovery omitted durable values or identity from %s", table.Name)
		}
	}
	if added != len(newTables) || !equalJSON(previous.Tables, retained) {
		t.Fatal("subtitle timelines changed a historical row shape")
	}
	var constraints []ConstraintSpec
	for _, constraint := range current.Constraints {
		if _, exists := newTables[constraint.Table]; !exists {
			constraints = append(constraints, constraint)
		}
	}
	if !equalJSON(previous.Constraints, constraints) {
		t.Fatal("subtitle timelines changed a historical foreign key")
	}

	type object struct {
		Kind  string          `json:"kind"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	read := func(path string) []object {
		t.Helper()
		data, err := catalogFiles.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var baseline catalogBaseline
		var objects []object
		if json.Unmarshal(data, &baseline) != nil || json.Unmarshal(baseline.Objects, &objects) != nil {
			t.Fatal("decode subtitle timeline catalog")
		}
		return objects
	}
	before := read("catalogs/schema-59-postgresql-17.json")
	previousObjects := make(map[string]object, len(before))
	for _, item := range before {
		previousObjects[item.Kind+":"+item.Name] = item
	}
	modified := map[string]bool{
		"column:libraries.00007":                                      false,
		"constraint:libraries.libraries_options_check":                false,
		"constraint:task_system_events.task_system_events_name_check": false,
		"constraint:task_triggers.task_triggers_system_event_check":   false,
	}
	var objects []object
	for _, item := range read("catalogs/schema-60-postgresql-17.json") {
		owned := false
		for table := range newTables {
			if item.Name == table || strings.HasPrefix(item.Name, table+".") || strings.HasPrefix(item.Name, table+"_") {
				owned = true
				break
			}
		}
		if owned {
			continue
		}
		key := item.Kind + ":" + item.Name
		if _, allowed := modified[key]; allowed {
			old, exists := previousObjects[key]
			if !exists || equalJSON(old.Value, item.Value) {
				t.Fatalf("expected subtitle timeline declaration change in %s", key)
			}
			if item.Kind == "column" {
				var oldValue, newValue map[string]json.RawMessage
				if json.Unmarshal(old.Value, &oldValue) != nil || json.Unmarshal(item.Value, &newValue) != nil {
					t.Fatal("decode library options default")
				}
				var defaultValue string
				if json.Unmarshal(newValue["default"], &defaultValue) != nil || !strings.Contains(defaultValue, `"EnableSubtitleTimelineGeneration": false`) {
					t.Fatal("automatic subtitle timeline generation is not disabled by default")
				}
				delete(oldValue, "default")
				delete(newValue, "default")
				if !equalJSON(oldValue, newValue) {
					t.Fatal("subtitle timeline generation changed the library options column beyond its default")
				}
			}
			modified[key] = true
			objects = append(objects, old)
			continue
		}
		objects = append(objects, item)
	}
	for name, seen := range modified {
		if !seen {
			t.Fatalf("subtitle timeline recovery omitted %s", name)
		}
	}
	if !equalJSON(before, objects) {
		t.Fatal("subtitle timelines changed an unrelated catalog object")
	}
	if _, err := compiledRecoveryDropPlan(60); err != nil {
		t.Fatal("subtitle timelines omitted their authenticated recovery object plan", err)
	}
}
