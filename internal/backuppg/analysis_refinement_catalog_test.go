package backuppg

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAnalysisRefinementRecoveryCatalogRetainsHistoricalPayloadBounds(t *testing.T) {
	previous, prefix, err := loadCatalog(52, "refinement_catalog")
	if err != nil {
		t.Fatal("load the published schema52 catalog", err)
	}
	current, migrations, err := loadCatalog(53, "refinement_catalog")
	if err != nil {
		t.Fatal("load the observed schema53 catalog", err)
	}
	if len(prefix) != 52 || len(migrations) != 53 || !equalJSON(prefix, migrations[:52]) ||
		migrations[52].Name != "0053_analysis_refinement_cache.sql" || previous.SHA256 == current.SHA256 ||
		!equalJSON(previous.Tables, current.Tables) || !equalJSON(previous.Sequences, current.Sequences) ||
		!equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("refinement cache changed the historical prefix, row shape, or sequence/foreign-key ownership")
	}
	type catalogObject struct {
		Kind  string          `json:"kind"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	var previousObjects []catalogObject
	for _, version := range []int64{50, 51, 52, 53} {
		data, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
		if err != nil {
			t.Fatal(err)
		}
		var baseline catalogBaseline
		if err := json.Unmarshal(data, &baseline); err != nil {
			t.Fatal(err)
		}
		var objects []catalogObject
		if err := json.Unmarshal(baseline.Objects, &objects); err != nil {
			t.Fatal(err)
		}
		found := 0
		for index, object := range objects {
			if object.Kind != "constraint" || object.Name != "analysis_feature_cache.analysis_feature_cache_payload_check" {
				continue
			}
			found++
			var constraint struct {
				Definition string `json:"definition"`
				Validated  bool   `json:"validated"`
			}
			if err := json.Unmarshal(object.Value, &constraint); err != nil {
				t.Fatal(err)
			}
			limit := 262144
			if version == 53 {
				limit = 524288
			}
			want := fmt.Sprintf("CHECK (octet_length(payload) >= 1 AND octet_length(payload) <= %d)", limit)
			if constraint.Definition != want || !constraint.Validated {
				t.Fatalf("schema%d has incorrect feature payload storage bounds", version)
			}
			if version == 53 {
				if len(objects) != len(previousObjects) || previousObjects[index].Name != object.Name {
					t.Fatal("schema53 changed the object inventory")
				}
				objects[index] = previousObjects[index]
			}
		}
		if found != 1 {
			t.Fatalf("schema%d omitted its feature payload constraint", version)
		}
		if version == 52 {
			previousObjects = objects
		} else if version == 53 && !equalJSON(objects, previousObjects) {
			t.Fatal("schema53 changed an object other than the feature payload bound")
		}
		if _, _, err := loadCatalog(version, "refinement_catalog"); err != nil {
			t.Fatalf("schema%d lost its authenticated catalog: %v", version, err)
		}
		if _, err := compiledRecoveryDropPlan(version); err != nil {
			t.Fatalf("schema%d omitted its authenticated recovery object plan: %v", version, err)
		}
	}
}
