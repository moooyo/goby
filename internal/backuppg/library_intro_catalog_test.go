package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLibraryIntroRecoveryCatalogRetainsSchema50AndAddsSchema51(t *testing.T) {
	previous, priorMigrations, err := loadCatalog(50, "intro_catalog")
	if err != nil {
		t.Fatalf("load the unchanged published schema50 catalog: %v", err)
	}
	current, migrations, err := loadCatalog(51, "intro_catalog")
	if err != nil {
		t.Fatalf("load the observed schema51 catalog: %v", err)
	}
	if len(priorMigrations) != 50 || len(migrations) != 51 || !equalJSON(priorMigrations, migrations[:50]) ||
		migrations[50].Name != "0051_library_intro_automation.sql" {
		t.Fatal("the new catalog changed its published migration prefix")
	}
	if previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("the library policy migration changed durable copy fields, sequence ownership, or foreign keys")
	}
	for _, version := range []string{"50", "51"} {
		data, err := catalogFiles.ReadFile("catalogs/schema-" + version + "-postgresql-17.json")
		if err != nil {
			t.Fatal(err)
		}
		var baseline catalogBaseline
		if err := json.Unmarshal(data, &baseline); err != nil {
			t.Fatal(err)
		}
		var objects []struct {
			Kind  string          `json:"kind"`
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(baseline.Objects, &objects); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, object := range objects {
			field := ""
			switch object.Name {
			case "libraries.libraries_options_check":
				field = "EnableIntroDetection"
			case "task_system_events.task_system_events_name_check", "task_triggers.task_triggers_system_event_check":
				field = "IntroAnalysisRequested"
			}
			if object.Kind != "constraint" || field == "" {
				continue
			}
			found++
			if strings.Contains(string(object.Value), field) != (version == "51") {
				t.Fatalf("schema%s has incorrect policy metadata for %s", version, object.Name)
			}
		}
		if found != 3 {
			t.Fatalf("schema%s omitted a required policy or event constraint", version)
		}
	}
	if _, err := compiledRecoveryDropPlan(51); err != nil {
		t.Fatalf("schema51 cannot construct its authenticated recovery object plan: %v", err)
	}
}
