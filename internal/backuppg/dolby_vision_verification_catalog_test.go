package backuppg

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestDolbyVisionVerificationRecoveryCatalogPreservesSchema79(t *testing.T) {
	const (
		tableName = "dolby_vision_verifications"
		scanIndex = tableName + "_scan_idx"
		dueIndex  = tableName + "_due_idx"
	)
	previous, prefix, err := loadCatalog(79, "dolby_vision_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(80, "dolby_vision_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 80 || !slices.Equal(prefix, migrations[:79]) || migrations[79].Name != "0080_dolby_vision_verification.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("verification changed the published migration prefix or sequence inventory")
	}
	key := []string{"item_id", "stream_index"}
	expected := TableSpec{
		Name: tableName, PrimaryKey: key, SortKey: key,
		Columns: []string{"item_id", "stream_index", "scan_job_id", "source_revision", "verifier_version", "tool_identity", "evidence", "attempts", "retry_at", "verified_at"},
	}
	found := false
	var retainedTables []TableSpec
	for _, table := range current.Tables {
		if table.Name == tableName {
			if found || !equalJSON(table, expected) {
				t.Fatal("verification recovery changed its exact COPY columns or stable stream identity")
			}
			found = true
		} else {
			retainedTables = append(retainedTables, table)
		}
	}
	if !found || !equalJSON(previous.Tables, retainedTables) {
		t.Fatal("verification changed a historical row shape or omitted its table")
	}
	var retainedKeys, addedKeys []ConstraintSpec
	for _, key := range current.Constraints {
		if key.Table == tableName {
			addedKeys = append(addedKeys, key)
		} else {
			retainedKeys = append(retainedKeys, key)
		}
	}
	if !slices.Equal(previous.Constraints, retainedKeys) || !slices.Equal(addedKeys, []ConstraintSpec{
		{Table: tableName, Name: tableName + "_item_id_fkey", Definition: "FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE"},
		{Table: tableName, Name: tableName + "_scan_job_id_fkey", Definition: "FOREIGN KEY (scan_job_id) REFERENCES scan_jobs(id) ON DELETE CASCADE"},
	}) {
		t.Fatal("verification changed foreign-key ownership or cascade actions")
	}

	before := backendReviewCatalogObjects(t, 79)
	var retainedObjects []backendReviewCatalogObject
	var indexes, eventChecks int
	for _, object := range backendReviewCatalogObjects(t, 80) {
		switch object.Name {
		case "task_system_events.task_system_events_name_check", "task_triggers.task_triggers_system_event_check":
			if object.Kind != "constraint" {
				t.Fatal("verification changed the event constraint kind")
			}
			// Undo exactly the appended name before comparing the published objects.
			const appended = `, 'DolbyVisionVerificationRequested'::text`
			if strings.Count(string(object.Value), appended) != 1 {
				t.Fatalf("verification did not extend %s", object.Name)
			}
			object.Value = json.RawMessage(strings.Replace(string(object.Value), appended, "", 1))
			eventChecks++
		case scanIndex, dueIndex:
			if object.Kind == "index" {
				var index struct {
					Table, Method      string
					Unique, Primary    bool
					Valid, Ready, Live bool
					Columns            []string
					Predicate          *string
				}
				leading := "scan_job_id"
				if object.Name == dueIndex {
					leading = "retry_at"
				}
				if json.Unmarshal(object.Value, &index) != nil || index.Table != tableName || index.Method != "btree" ||
					index.Unique || index.Primary || !index.Valid || !index.Ready || !index.Live ||
					!slices.Equal(index.Columns, []string{leading, "item_id", "stream_index"}) {
					t.Fatalf("verification changed the index definition: %s", object.Name)
				}
				if object.Name == scanIndex && index.Predicate != nil ||
					object.Name == dueIndex && (index.Predicate == nil || *index.Predicate != "evidence IS NULL") {
					t.Fatalf("verification changed the index predicate: %s", object.Name)
				}
				indexes++
			}
			continue
		}
		if object.Name == tableName || strings.HasPrefix(object.Name, tableName+".") || strings.HasPrefix(object.Name, tableName+"_") {
			continue
		}
		retainedObjects = append(retainedObjects, object)
	}
	if indexes != 2 || eventChecks != 2 || !equalJSON(before, retainedObjects) {
		t.Fatal("verification did not preserve the historical catalog except for the two event checks")
	}
	plan, err := compiledRecoveryDropPlan(80)
	if err != nil {
		t.Fatalf("verification recovery drop plan: %v", err)
	}
	if !slices.Contains(plan.indexes, scanIndex) || !slices.Contains(plan.indexes, dueIndex) {
		t.Fatal("verification recovery drop plan omitted its indexes")
	}
	for _, key := range addedKeys {
		if !slices.Contains(plan.constraints, recoveryConstraint{table: key.Table, name: key.Name, foreign: true}) {
			t.Fatalf("verification recovery drop plan omitted %s", key.Name)
		}
	}
}
