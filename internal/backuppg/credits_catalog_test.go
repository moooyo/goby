package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreditsRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(55, "credits_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(56, "credits_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 56 || !equalJSON(prefix, migrations[:55]) || migrations[55].Name != "0056_credits_markers.sql" || !equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("credits changed the historical migration prefix or sequence ownership")
	}
	var retained []TableSpec
	found := false
	for _, table := range current.Tables {
		if table.Name != "item_credits_state" {
			retained = append(retained, table)
			continue
		}
		found = true
		if !equalJSON(table.Columns, []string{"item_id", "revision", "source_revision", "start_ticks", "provenance", "last_edited_by", "last_edited_at"}) || !equalJSON(table.PrimaryKey, []string{"item_id"}) {
			t.Fatal("credits recovery omitted durable values or item identity")
		}
	}
	if !found || !equalJSON(previous.Tables, retained) {
		t.Fatal("credits changed historical row shapes")
	}
	var constraints []ConstraintSpec
	for _, constraint := range current.Constraints {
		if constraint.Table != "item_credits_state" {
			constraints = append(constraints, constraint)
		}
	}
	if !equalJSON(previous.Constraints, constraints) {
		t.Fatal("credits changed historical foreign keys")
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
			t.Fatal("decode catalog")
		}
		return objects
	}
	var objects []object
	for _, item := range read("catalogs/schema-56-postgresql-17.json") {
		if item.Name != "item_credits_state" && !strings.HasPrefix(item.Name, "item_credits_state.") && item.Name != "item_credits_state_pkey" && !strings.HasPrefix(item.Name, "item_credits_state_pkey.") {
			objects = append(objects, item)
		}
	}
	if !equalJSON(read("catalogs/schema-55-postgresql-17.json"), objects) {
		t.Fatal("credits changed an unrelated catalog object")
	}
	if _, err := compiledRecoveryDropPlan(56); err != nil {
		t.Fatal(err)
	}
}
