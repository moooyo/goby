package backuppg

import (
	"strings"
	"testing"
)

func TestCatalogEntityReuseRecoveryCatalogChangesOnlySynchronization(t *testing.T) {
	previous, prefix, err := loadCatalog(67, "entity_reuse_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(68, "entity_reuse_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 68 || !equalJSON(prefix, migrations[:67]) || migrations[67].Name != "0068_catalog_entity_reuse.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("entity reuse changed the published migration prefix or durable row shapes")
	}
	before := backendReviewCatalogObjects(t, 67)
	after := backendReviewCatalogObjects(t, 68)
	if len(before) != len(after) {
		t.Fatal("entity reuse changed the schema object inventory")
	}
	changed := 0
	for index, object := range after {
		if equalJSON(object, before[index]) {
			continue
		}
		if object.Kind != "function" || object.Kind != before[index].Kind || object.Name != before[index].Name ||
			!strings.HasPrefix(object.Name, "sync_catalog_item_entities(") {
			t.Fatalf("entity reuse changed an unrelated object: %s %s", object.Kind, object.Name)
		}
		changed++
	}
	if changed != 1 {
		t.Fatal("entity reuse did not publish its observed synchronization function")
	}
	if _, err := compiledRecoveryDropPlan(68); err != nil {
		t.Fatal("entity reuse catalog omitted its authenticated recovery object plan", err)
	}
}
