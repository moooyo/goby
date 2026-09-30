package backuppg

import (
	"encoding/json"
	"testing"
)

func TestIntroSkipperRecoveryCatalogAddsOnlySettingsColumnAndConstraint(t *testing.T) {
	previous, prefix, err := loadCatalog(53, "intro_skipper_catalog")
	if err != nil {
		t.Fatal("load the published schema53 catalog", err)
	}
	current, migrations, err := loadCatalog(54, "intro_skipper_catalog")
	if err != nil {
		t.Fatal("load the observed schema54 catalog", err)
	}
	if len(prefix) != 53 || len(migrations) != 54 || !equalJSON(prefix, migrations[:53]) ||
		migrations[53].Name != "0054_intro_skipper_options.sql" || previous.SHA256 == current.SHA256 ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("Intro Skipper options changed the historical prefix or object ownership")
	}
	foundSettings := false
	for index := range current.Tables {
		if current.Tables[index].Name != "analysis_settings" {
			continue
		}
		foundSettings = true
		columns := current.Tables[index].Columns
		if len(columns) != 11 || columns[len(columns)-1] != "intro_skipper_options" {
			t.Fatal("schema54 omitted or reordered its durable settings column")
		}
		current.Tables[index].Columns = columns[:len(columns)-1]
	}
	if !foundSettings || !equalJSON(previous.Tables, current.Tables) {
		t.Fatal("schema54 changed a historical row shape beyond the new settings column")
	}
	type catalogObject struct {
		Kind  string          `json:"kind"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	readObjects := func(name string) []catalogObject {
		t.Helper()
		data, err := catalogFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var baseline catalogBaseline
		var objects []catalogObject
		if json.Unmarshal(data, &baseline) != nil || json.Unmarshal(baseline.Objects, &objects) != nil {
			t.Fatal("decode the observed catalog objects")
		}
		return objects
	}
	before := readObjects("catalogs/schema-53-postgresql-17.json")
	after := readObjects("catalogs/schema-54-postgresql-17.json")
	retained := make([]catalogObject, 0, len(before))
	added := 0
	for _, object := range after {
		if object.Kind == "column" && object.Name == "analysis_settings.00011" ||
			object.Kind == "constraint" && object.Name == "analysis_settings.analysis_settings_intro_skipper_options_check" {
			added++
			continue
		}
		retained = append(retained, object)
	}
	if added != 2 || !equalJSON(before, retained) {
		t.Fatal("schema54 changed an object other than its settings column and constraint")
	}
	if _, err := compiledRecoveryDropPlan(54); err != nil {
		t.Fatal("schema54 omitted its authenticated recovery object plan", err)
	}
}
