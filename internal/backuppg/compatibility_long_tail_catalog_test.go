package backuppg

import (
	"slices"
	"testing"
)

func TestCompatibilityLongTailCatalogRetainsSortPolicyAndProvenance(t *testing.T) {
	catalog, _ := currentRecoveryCatalog(t, "long_tail_catalog")
	for table, column := range map[string]string{"managed_settings": "sort_remove_words", "item_metadata_state": "automatic_sort_name_explicit"} {
		found := false
		for _, item := range catalog.Tables {
			if item.Name == table {
				found = slices.Contains(item.Columns, column)
			}
		}
		if !found {
			t.Fatalf("current archive omitted %s.%s", table, column)
		}
	}
	historical, migrations, err := loadCatalog(48, "long_tail_history")
	if err != nil || len(historical.Tables) != 61 || len(migrations) != 48 {
		t.Fatalf("published schema48 recovery inventory changed: %v", err)
	}
}
