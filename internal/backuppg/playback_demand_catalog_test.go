package backuppg

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestPlaybackDemandRecoveryCatalogAddsOnlyRevisionColumnAndConstraint(t *testing.T) {
	previous, prefix, err := loadCatalog(54, "playback_demand_catalog")
	if err != nil {
		t.Fatal("load the published schema54 catalog", err)
	}
	current, migrations, err := loadCatalog(55, "playback_demand_catalog")
	if err != nil {
		t.Fatal("load the observed PostgreSQL17 schema55 catalog", err)
	}
	compiled, err := database.EmbeddedMigrations()
	if err != nil || len(compiled) < 55 || len(prefix) != 54 || len(migrations) != 55 ||
		!equalJSON(prefix, migrations[:54]) || migrations[54].Version != 55 || migrations[54].Name != "0055_playback_demand_revision.sql" ||
		migrations[54].SHA256 != compiled[54].SHA256 || migrations[54].SHA256 != "8e2f2b3663e4af9b9c45fbfad7df7177630692a2c49d3f8983b2519b8a1e7809" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("schema55 changed the trusted historical migration prefix, source checksum, or object ownership")
	}
	found := 0
	for index := range current.Tables {
		if current.Tables[index].Name != "play_sessions" {
			continue
		}
		found++
		columns := current.Tables[index].Columns
		if len(columns) != 20 || columns[len(columns)-1] != "playback_revision" {
			t.Fatal("schema55 omitted or reordered the durable playback revision column")
		}
		current.Tables[index].Columns = columns[:len(columns)-1]
	}
	if found != 1 || !equalJSON(previous.Tables, current.Tables) {
		t.Fatal("schema55 changed a historical row shape beyond the playback revision column")
	}
	type object struct {
		Kind  string          `json:"kind"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	read := func(name string) []object {
		t.Helper()
		data, err := catalogFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var baseline catalogBaseline
		var objects []object
		if json.Unmarshal(data, &baseline) != nil || json.Unmarshal(baseline.Objects, &objects) != nil {
			t.Fatal("decode the observed catalog object inventory")
		}
		return objects
	}
	before, after := read("catalogs/schema-54-postgresql-17.json"), read("catalogs/schema-55-postgresql-17.json")
	retained := make([]object, 0, len(before))
	newColumn, newConstraint := 0, 0
	for _, candidate := range after {
		if candidate.Kind == "column" && candidate.Name == "play_sessions.00020" {
			newColumn++
			var column struct {
				Name, Type, Default, Storage string
				NotNull                      bool `json:"not_null"`
				Dropped                      bool
				Generated, Identity          string
			}
			if json.Unmarshal(candidate.Value, &column) != nil || column.Name != "playback_revision" || column.Type != "bigint" || column.Default != "0" ||
				!column.NotNull || column.Dropped || column.Generated != "" || column.Identity != "" || column.Storage != "p" {
				t.Fatal("schema55 revision storage differs from the actual trusted SQL contract")
			}
			continue
		}
		if candidate.Kind == "constraint" && candidate.Name == "play_sessions.play_sessions_playback_revision_nonnegative" {
			newConstraint++
			var constraint struct {
				Definition, Type string
				Validated        bool
			}
			if json.Unmarshal(candidate.Value, &constraint) != nil || constraint.Type != "c" || !constraint.Validated || constraint.Definition != "CHECK (playback_revision >= 0)" {
				t.Fatal("schema55 omitted the validated nonnegative playback revision constraint")
			}
			continue
		}
		retained = append(retained, candidate)
	}
	if newColumn != 1 || newConstraint != 1 || !equalJSON(before, retained) {
		t.Fatal("schema55 changed an observed object other than its revision column and check")
	}
	plan, err := compiledRecoveryDropPlan(55)
	if err != nil {
		t.Fatal("schema55 omitted the authenticated recovery object plan", err)
	}
	foundDefault, foundCheck := false, false
	for _, value := range plan.defaults {
		foundDefault = foundDefault || value.table == "play_sessions" && value.column == "playback_revision" && !value.generated
	}
	for _, value := range plan.constraints {
		foundCheck = foundCheck || value.table == "play_sessions" && value.name == "play_sessions_playback_revision_nonnegative" && !value.foreign
	}
	if !foundDefault || !foundCheck {
		t.Fatal("schema55 recovery drop plan lost its complete new column default or check")
	}
}
