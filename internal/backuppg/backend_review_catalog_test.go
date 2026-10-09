package backuppg

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type backendReviewCatalogObject struct {
	Kind  string          `json:"kind"`
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

func backendReviewCatalogObjects(t *testing.T, version int64) []backendReviewCatalogObject {
	t.Helper()
	raw, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
	if err != nil {
		t.Fatal(err)
	}
	var baseline catalogBaseline
	var objects []backendReviewCatalogObject
	if json.Unmarshal(raw, &baseline) != nil || json.Unmarshal(baseline.Objects, &objects) != nil {
		t.Fatal("decode observed backend review catalog")
	}
	return objects
}

func TestNotificationSourceRecoveryCatalogChangesOnlyRecordingFunction(t *testing.T) {
	previous, prefix, err := loadCatalog(61, "notification_source_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(62, "notification_source_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 62 || !equalJSON(prefix, migrations[:61]) || migrations[61].Name != "0062_notification_disabled_transport.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("notification optimization changed the historical migration prefix or durable row shapes")
	}
	before := backendReviewCatalogObjects(t, 61)
	after := backendReviewCatalogObjects(t, 62)
	if len(before) != len(after) {
		t.Fatal("notification optimization changed the schema object inventory")
	}
	changed := 0
	for index, object := range after {
		if equalJSON(object, before[index]) {
			continue
		}
		if object.Kind != "function" || object.Kind != before[index].Kind || object.Name != before[index].Name ||
			!strings.HasPrefix(object.Name, "goby_record_notification_source(") {
			t.Fatalf("notification optimization changed an unrelated object: %s %s", object.Kind, object.Name)
		}
		changed++
	}
	if changed != 1 {
		t.Fatal("notification optimization did not publish its observed recording function")
	}
	if _, err := compiledRecoveryDropPlan(62); err != nil {
		t.Fatal("notification catalog omitted its authenticated recovery object plan", err)
	}
}

func TestMediaOperationSourceRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(62, "media_operation_source_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(63, "media_operation_source_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 63 || !equalJSON(prefix, migrations[:62]) || migrations[62].Name != "0063_media_operation_source_revision.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("source cache changed the historical migration prefix or object ownership")
	}
	items := 0
	for index, table := range current.Tables {
		if table.Name != "items" {
			continue
		}
		items++
		columns := table.Columns
		if len(columns) < 2 || !equalJSON(columns[len(columns)-2:], []string{"media_operation_source_revision", "media_operation_source_binding_revision"}) {
			t.Fatal("source cache omitted or reordered its durable copy columns")
		}
		current.Tables[index].Columns = columns[:len(columns)-2]
	}
	if items != 1 || !equalJSON(previous.Tables, current.Tables) {
		t.Fatal("source cache changed an unrelated row shape")
	}
	var retained []backendReviewCatalogObject
	columns, functions, triggers := 0, 0, 0
	for _, object := range backendReviewCatalogObjects(t, 63) {
		if object.Kind == "column" && strings.HasPrefix(object.Name, "items.") {
			var column struct {
				Name, Type string
				NotNull    bool `json:"not_null"`
				Default    *string
			}
			if json.Unmarshal(object.Value, &column) != nil {
				t.Fatal("decode source cache column")
			}
			if column.Name == "media_operation_source_revision" || column.Name == "media_operation_source_binding_revision" {
				wantType := "text"
				if column.Name == "media_operation_source_binding_revision" {
					wantType = "bigint"
				}
				if column.Type != wantType || column.NotNull || column.Default != nil {
					t.Fatal("source cache omitted nullable fallback semantics")
				}
				columns++
				continue
			}
		}
		if object.Kind == "function" && object.Name == "refresh_item_media_operation_source_revision()" {
			functions++
			continue
		}
		if object.Kind == "trigger" && object.Name == "items.items_media_operation_source_revision" {
			triggers++
			continue
		}
		retained = append(retained, object)
	}
	if columns != 2 || functions != 1 || triggers != 1 || !equalJSON(backendReviewCatalogObjects(t, 62), retained) {
		t.Fatal("source cache changed objects beyond its columns and refresh trigger")
	}
	plan, err := compiledRecoveryDropPlan(63)
	if err != nil {
		t.Fatal(err)
	}
	foundFunction, foundTrigger := false, false
	for _, function := range plan.functions {
		foundFunction = foundFunction || function.name == "refresh_item_media_operation_source_revision" && function.arguments == ""
	}
	for _, trigger := range plan.triggers {
		foundTrigger = foundTrigger || trigger == [2]string{"items", "items_media_operation_source_revision"}
	}
	if !foundFunction || !foundTrigger {
		t.Fatal("source cache recovery plan omitted its trusted trigger or function")
	}
}

func TestItemReferenceIndexesRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(63, "item_reference_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(64, "item_reference_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 64 || !equalJSON(prefix, migrations[:63]) || migrations[63].Name != "0064_item_reference_indexes.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("item-reference indexes changed the historical migration prefix, row shapes, or constraints")
	}
	wanted := map[string]string{
		"user_item_data_item_idx":   "user_item_data",
		"play_sessions_item_idx":    "play_sessions",
		"media_operations_item_idx": "media_operations",
	}
	var retained []backendReviewCatalogObject
	added := make(map[string]int)
	for _, object := range backendReviewCatalogObjects(t, 64) {
		name := strings.TrimSuffix(object.Name, ".00001")
		table, expected := wanted[name]
		if !expected {
			retained = append(retained, object)
			continue
		}
		switch object.Kind {
		case "index":
			var index struct {
				Table, Method      string
				Unique, Primary    bool
				Valid, Ready, Live bool
				KeyCount           int `json:"key_count"`
				Columns            []string
				Predicate          *string
			}
			if json.Unmarshal(object.Value, &index) != nil || index.Table != table || index.Method != "btree" ||
				index.Unique || index.Primary || !index.Valid || !index.Ready || !index.Live || index.KeyCount != 1 ||
				!equalJSON(index.Columns, []string{"item_id"}) {
				t.Fatalf("item-reference catalog changed the index definition: %s", name)
			}
			if table == "media_operations" {
				if index.Predicate == nil || *index.Predicate != "item_id IS NOT NULL" {
					t.Fatal("media-operation item index must exclude only detached history")
				}
			} else if index.Predicate != nil {
				t.Fatal("playback and user-state item indexes must retain all referencing rows")
			}
		case "column", "relation":
		default:
			t.Fatalf("unexpected new index object kind: %s", object.Kind)
		}
		added[name+":"+object.Kind]++
	}
	if len(added) != 9 || !equalJSON(backendReviewCatalogObjects(t, 63), retained) {
		t.Fatal("item-reference migration changed objects beyond the three selected indexes")
	}
	for name := range wanted {
		for _, kind := range []string{"index", "column", "relation"} {
			if added[name+":"+kind] != 1 {
				t.Fatalf("item-reference catalog omitted or duplicated %s %s", kind, name)
			}
		}
	}
	plan, err := compiledRecoveryDropPlan(64)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range plan.indexes {
		delete(wanted, index)
	}
	if len(wanted) != 0 {
		t.Fatal("item-reference catalog omitted indexes from the authenticated recovery plan")
	}
}
