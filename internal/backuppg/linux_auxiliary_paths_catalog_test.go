package backuppg

import (
	"strings"
	"testing"
)

func TestLinuxAuxiliaryPathsRecoveryCatalogPreservesSchema65(t *testing.T) {
	previous, prefix, err := loadCatalog(65, "linux_auxiliary_catalog")
	if err != nil {
		t.Fatal(err)
	}
	current, migrations, err := loadCatalog(66, "linux_auxiliary_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 66 || !equalJSON(prefix, migrations[:65]) || migrations[65].Name != "0066_linux_auxiliary_paths.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) || !equalJSON(previous.Sequences, current.Sequences) {
		t.Fatal("Linux auxiliary paths changed the published prefix, table rows, or sequence identities")
	}
	before, after := backendReviewCatalogObjects(t, 65), backendReviewCatalogObjects(t, 66)
	if len(before) != len(after) {
		t.Fatal("Linux auxiliary paths changed the catalog object inventory")
	}
	changed := make(map[string]bool)
	for index, original := range before {
		updated := after[index]
		if equalJSON(original, updated) {
			continue
		}
		if original.Kind != "constraint" || updated.Kind != original.Kind || updated.Name != original.Name ||
			(original.Name != "theme_reserved_paths.theme_reserved_paths_canonical_check" && original.Name != "extra_reserved_paths.extra_reserved_paths_canonical_check") ||
			!strings.Contains(string(original.Value), "^[A-Za-z]:") || strings.Contains(string(updated.Value), "^[A-Za-z]:") {
			t.Fatalf("Linux auxiliary paths changed an unrelated catalog object: %s %s", original.Kind, original.Name)
		}
		changed[original.Name] = true
	}
	if len(changed) != 2 {
		t.Fatal("Linux auxiliary paths did not replace exactly the two reservation constraints")
	}
}
