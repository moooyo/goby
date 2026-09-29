package backuppg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLibraryPreviewRecoveryCatalogRetainsSchema51AndAddsSchema52(t *testing.T) {
	previous, prefix, err := loadCatalog(51, "preview_catalog")
	if err != nil {
		t.Fatal("load the published schema51 catalog", err)
	}
	current, migrations, err := loadCatalog(52, "preview_catalog")
	if err != nil {
		t.Fatal("load the observed schema52 catalog", err)
	}
	if len(prefix) != 51 || len(migrations) != 52 || !equalJSON(prefix, migrations[:51]) ||
		migrations[51].Name != "0052_library_preview_automation.sql" || previous.SHA256 == current.SHA256 ||
		!equalJSON(previous.Tables, current.Tables) || !equalJSON(previous.Sequences, current.Sequences) ||
		!equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("preview policy changed the historical prefix, durable row shape, or sequence/foreign-key ownership")
	}
	for _, version := range []string{"51", "52"} {
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
				field = "EnablePreviewGeneration"
			case "task_system_events.task_system_events_name_check", "task_triggers.task_triggers_system_event_check":
				field = "PreviewGenerationRequested"
			}
			if object.Kind == "constraint" && field != "" {
				found++
				if strings.Contains(string(object.Value), field) != (version == "52") {
					t.Fatalf("schema%s has incorrect preview policy metadata for %s", version, object.Name)
				}
			}
		}
		if found != 3 {
			t.Fatalf("schema%s omitted required preview policy constraints", version)
		}
	}
	if _, err := compiledRecoveryDropPlan(52); err != nil {
		t.Fatal("schema52 omitted its authenticated recovery object plan", err)
	}
}
