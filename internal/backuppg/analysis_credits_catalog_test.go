package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreditsAnalysisRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(58, "credits_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(59, "credits_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 59 || !equalJSON(prefix, migrations[:58]) || migrations[58].Name != "0059_credits_analysis.sql" ||
		!equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("credits analysis changed the historical migration prefix or sequence ownership")
	}
	newTables := map[string][]string{
		"analysis_credits_detections":        {"item_id", "revision", "source_revision", "profile_fingerprint", "profile_revision", "publication_epoch", "child_id", "cohort_revision", "status", "result", "start_ticks", "end_ticks", "auto_published", "updated_at"},
		"analysis_credits_detection_sources": {"item_id", "source_item_id", "library_id", "root_id", "source_revision", "hierarchy_revision", "episode_key", "content_sha256"},
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
		keyColumns := columns[:1]
		if table.Name == "analysis_credits_detection_sources" {
			keyColumns = columns[:2]
		}
		if !equalJSON(table.Columns, columns) || !equalJSON(table.PrimaryKey, keyColumns) {
			t.Fatalf("credits recovery omitted durable values or identity from %s", table.Name)
		}
	}
	if added != len(newTables) || !equalJSON(previous.Tables, retained) {
		t.Fatal("credits analysis changed a historical row shape")
	}
	var constraints []ConstraintSpec
	for _, constraint := range current.Constraints {
		if _, exists := newTables[constraint.Table]; !exists {
			constraints = append(constraints, constraint)
		}
	}
	if !equalJSON(previous.Constraints, constraints) {
		t.Fatal("credits analysis changed a historical foreign key")
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
			t.Fatal("decode credits catalog")
		}
		return objects
	}
	before := read("catalogs/schema-58-postgresql-17.json")
	previousObjects := make(map[string]object, len(before))
	for _, item := range before {
		previousObjects[item.Kind+":"+item.Name] = item
	}
	modified := map[string]bool{
		"column:libraries.00007":                                      false,
		"constraint:libraries.libraries_options_check":                false,
		"constraint:task_system_events.task_system_events_name_check": false,
		"constraint:task_triggers.task_triggers_system_event_check":   false,
		"constraint:task_runs.task_runs_analysis_input_check":         false,
		"constraint:task_runs.task_runs_analysis_authority_check":     false,
		"constraint:analysis_work.analysis_work_task_key_check":       false,
		"function:enforce_task_analysis_scope_key()":                  false,
	}
	var objects []object
	for _, item := range read("catalogs/schema-59-postgresql-17.json") {
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
				t.Fatalf("expected credits declaration change in %s", key)
			}
			if item.Kind == "column" {
				var oldValue, newValue map[string]json.RawMessage
				if json.Unmarshal(old.Value, &oldValue) != nil || json.Unmarshal(item.Value, &newValue) != nil {
					t.Fatal("decode library options default")
				}
				var defaultValue string
				if json.Unmarshal(newValue["default"], &defaultValue) != nil || !strings.Contains(defaultValue, `"EnableCreditsDetection": false`) {
					t.Fatal("automatic credits generation is not disabled by default")
				}
				delete(oldValue, "default")
				delete(newValue, "default")
				if !equalJSON(oldValue, newValue) {
					t.Fatal("credits generation changed the library options column beyond its default")
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
			t.Fatalf("credits recovery omitted %s", name)
		}
	}
	if !equalJSON(before, objects) {
		t.Fatal("credits analysis changed an unrelated catalog object")
	}
	if _, err := compiledRecoveryDropPlan(59); err != nil {
		t.Fatal("credits analysis omitted their authenticated recovery object plan", err)
	}
}
