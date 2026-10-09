package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMediaOperationsPendingIndexRecoveryCatalogPreservesSchema66(t *testing.T) {
	previous, prefix, err := loadCatalog(66, "media_pending_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(67, "media_pending_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 67 || !equalJSON(prefix, migrations[:66]) || migrations[66].Name != "0067_media_operations_pending_index.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("pending index changed the published prefix, row shapes, sequences or constraints")
	}
	var retained []backendReviewCatalogObject
	added := make(map[string]int)
	for _, object := range backendReviewCatalogObjects(t, 67) {
		name, _, _ := strings.Cut(object.Name, ".")
		if name != "media_operations_pending_order_idx" {
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
			if json.Unmarshal(object.Value, &index) != nil || index.Table != "media_operations" || index.Method != "btree" ||
				index.Unique || index.Primary || !index.Valid || !index.Ready || !index.Live || index.KeyCount != 3 ||
				index.Options != "3 0 0" || len(index.Columns) != 3 || !strings.Contains(index.Columns[0], "cancel_requested_at IS NOT NULL") ||
				index.Columns[1] != "created_at" || index.Columns[2] != "id" || index.Predicate == nil {
				t.Fatal("pending index catalog lost its priority ordering or partial predicate")
			}
		case "column", "relation":
		default:
			t.Fatalf("unexpected pending index object kind: %s", object.Kind)
		}
		added[object.Kind]++
	}
	if added["index"] != 1 || added["relation"] != 1 || added["column"] != 3 || len(added) != 3 ||
		!equalJSON(backendReviewCatalogObjects(t, 66), retained) {
		t.Fatal("pending index migration changed objects beyond its one new index")
	}
	plan, err := compiledRecoveryDropPlan(67)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range plan.indexes {
		if index == "media_operations_pending_order_idx" {
			return
		}
	}
	t.Fatal("authenticated recovery plan omitted the pending-operation index")
}
