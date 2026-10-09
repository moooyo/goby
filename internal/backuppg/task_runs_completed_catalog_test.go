package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskRunsCompletedIndexRecoveryCatalogPreservesSchema64(t *testing.T) {
	previous, prefix, err := loadCatalog(64, "completed_run_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(65, "completed_run_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 65 || !equalJSON(prefix, migrations[:64]) || migrations[64].Name != "0065_task_runs_completed_index.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("completed-run index changed the historical prefix, row shapes, or constraints")
	}
	var retained []backendReviewCatalogObject
	added := make(map[string]int)
	for _, object := range backendReviewCatalogObjects(t, 65) {
		name, _, _ := strings.Cut(object.Name, ".")
		if name != "task_runs_completed_idx" {
			retained = append(retained, object)
			continue
		}
		switch object.Kind {
		case "index":
			var index struct {
				Table, Method, Options string
				Unique, Primary        bool
				Valid, Ready, Live     bool
				KeyCount               int `json:"key_count"`
				Columns                []string
				Predicate              *string
			}
			if json.Unmarshal(object.Value, &index) != nil || index.Table != "task_runs" || index.Method != "btree" ||
				index.Unique || index.Primary || !index.Valid || !index.Ready || !index.Live || index.KeyCount != 3 ||
				index.Options != "0 3 3" || !equalJSON(index.Columns, []string{"task_id", "finished_at", "id"}) ||
				index.Predicate == nil || *index.Predicate != "finished_at IS NOT NULL" {
				t.Fatal("completed-run catalog lost the exact partial index ordering")
			}
		case "column", "relation":
		default:
			t.Fatalf("unexpected completed-run index object kind: %s", object.Kind)
		}
		added[object.Kind]++
	}
	if added["index"] != 1 || added["relation"] != 1 || added["column"] != 3 || len(added) != 3 ||
		!equalJSON(backendReviewCatalogObjects(t, 64), retained) {
		t.Fatal("completed-run migration changed objects beyond its one selected index")
	}
	plan, err := compiledRecoveryDropPlan(65)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range plan.indexes {
		if index == "task_runs_completed_idx" {
			return
		}
	}
	t.Fatal("authenticated recovery plan omitted the completed-run index")
}
