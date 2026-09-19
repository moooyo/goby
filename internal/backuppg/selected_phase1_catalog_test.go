package backuppg

import "testing"

func TestSelectedPhase1RecoveryCatalogPreservesCredentialAndIntroState(t *testing.T) {
	catalog, _ := currentRecoveryCatalog(t, "selected_phase1_catalog")
	tables := make(map[string]TableSpec, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tables[table.Name] = table
	}
	for table, names := range map[string][]string{
		"users":    {"local_password_hash", "profile_pin_ciphertext", "local_credentials_revision", "local_password_failures", "local_password_blocked_until"},
		"sessions": {"local_auth"},
	} {
		columns := make(map[string]bool)
		for _, column := range tables[table].Columns {
			columns[column] = true
		}
		for _, name := range names {
			if !columns[name] {
				t.Errorf("recovery omits durable account state %s.%s", table, name)
			}
		}
	}
	want := TableSpec{Name: "item_intro_state",
		Columns:    []string{"item_id", "revision", "source_revision", "start_ticks", "end_ticks", "provenance", "last_edited_by", "last_edited_at"},
		PrimaryKey: []string{"item_id"}, SortKey: []string{"item_id"}}
	if !equalJSON(tables[want.Name], want) {
		t.Fatal("recovery omits intro source binding, stable revision, timing or provenance")
	}
	historical, migrations, err := loadCatalog(41, "selected_phase1_history")
	if err != nil || len(migrations) != 41 || len(historical.Tables) == 0 {
		t.Fatalf("the published schema41 recovery contract must remain loadable: %v", err)
	}
}
