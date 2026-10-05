package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBitmapSubtitleRecoveryCatalogPreservesHistoricalObjects(t *testing.T) {
	previous, prefix, err := loadCatalog(60, "bitmap_subtitle_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(61, "bitmap_subtitle_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 61 || !equalJSON(prefix, migrations[:60]) || migrations[60].Name != "0061_external_bitmap_subtitles.sql" ||
		!equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("bitmap subtitles changed the historical migration prefix or sequence ownership")
	}
	expected := TableSpec{Name: "item_bitmap_subtitles", Columns: []string{
		"item_id", "root_id", "stream_index", "active", "relative_path", "source_stream_index", "format", "codec", "source_hash",
		"language", "title", "is_default", "is_forced", "is_hearing_impaired", "components",
	}, PrimaryKey: []string{"item_id", "stream_index"}, SortKey: []string{"item_id", "stream_index"}}
	var retained []TableSpec
	added := false
	for _, table := range current.Tables {
		if table.Name != expected.Name {
			retained = append(retained, table)
			continue
		}
		if added || !equalJSON(table, expected) {
			t.Fatal("bitmap subtitle recovery omitted durable values or stable stream identity")
		}
		added = true
	}
	if !added || !equalJSON(previous.Tables, retained) {
		t.Fatal("bitmap subtitles changed a historical row shape")
	}
	var constraints []ConstraintSpec
	for _, constraint := range current.Constraints {
		if constraint.Table != expected.Name {
			constraints = append(constraints, constraint)
		}
	}
	if !equalJSON(previous.Constraints, constraints) {
		t.Fatal("bitmap subtitles changed a historical foreign key")
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
			t.Fatal("decode bitmap subtitle catalog")
		}
		return objects
	}
	before := read("catalogs/schema-60-postgresql-17.json")
	var retainedObjects []object
	for _, item := range read("catalogs/schema-61-postgresql-17.json") {
		if item.Name == expected.Name || strings.HasPrefix(item.Name, expected.Name+".") || strings.HasPrefix(item.Name, expected.Name+"_") {
			continue
		}
		retainedObjects = append(retainedObjects, item)
	}
	if !equalJSON(before, retainedObjects) {
		t.Fatal("bitmap subtitles changed an unrelated catalog object")
	}
	if _, err := compiledRecoveryDropPlan(61); err != nil {
		t.Fatal("bitmap subtitles omitted their authenticated recovery object plan", err)
	}
}
