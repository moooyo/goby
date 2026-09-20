package backuppg

import "testing"

func TestSelectedPhase3RecoveryCatalogPreservesRosterAuthorityAndHistory(t *testing.T) {
	catalog, _ := currentRecoveryCatalog(t, "selected_phase3_catalog")
	tables := make(map[string]TableSpec, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tables[table.Name] = table
	}
	for _, expected := range []TableSpec{
		{Name: "series_episode_rosters", Columns: []string{"series_id", "revision", "state", "source_key", "source_label", "source_revision", "parser_version", "payload_sha256", "last_edited_by", "created_at", "updated_at"}, PrimaryKey: []string{"series_id"}},
		{Name: "episode_roster_imports", Columns: []string{"series_id", "revision", "action", "source_key", "source_label", "source_revision", "parser_version", "payload", "payload_sha256", "actor_id", "created_at"}, PrimaryKey: []string{"series_id", "revision"}},
		{Name: "expected_episodes", Columns: []string{"id", "series_id", "source_key", "entry_key", "season_number", "episode_number", "name", "premiere_date", "active", "import_revision", "created_at", "updated_at", "retired_at"}, PrimaryKey: []string{"id"}},
	} {
		expected.SortKey = expected.PrimaryKey
		if !equalJSON(tables[expected.Name], expected) {
			t.Errorf("recovery omits stable roster identity or complete provenance in %s", expected.Name)
		}
	}
	historical, migrations, err := loadCatalog(45, "selected_phase3_history")
	if err != nil || len(migrations) != 45 || len(historical.Tables) != 53 {
		t.Fatalf("the published schema45 catalog must remain unchanged and loadable: %v", err)
	}
}
