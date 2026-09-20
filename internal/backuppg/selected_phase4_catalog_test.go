package backuppg

import "testing"

func TestSelectedPhase4RecoveryCatalogContainsRuntimeAndNotificationEvidence(t *testing.T) {
	catalog, _ := currentRecoveryCatalog(t, "selected_phase4_catalog")
	tables := make(map[string]TableSpec, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tables[table.Name] = table
	}
	for _, expected := range []TableSpec{
		{Name: "notification_transport", Columns: []string{"id", "revision", "enabled", "endpoint", "allowed_networks", "credential_ciphertext", "credential_generation"}, PrimaryKey: []string{"id"}},
		{Name: "notification_journal_state", Columns: []string{"id", "sequence"}, PrimaryKey: []string{"id"}},
		{Name: "notification_registrations", Columns: []string{"id", "session_id", "user_id", "device_id", "peer_ip", "revision", "enabled", "event_ids", "token_ciphertext", "token_generation", "source_cursor", "last_outcome", "created_at", "updated_at"}, PrimaryKey: []string{"id"}},
		{Name: "notification_source_events", Columns: []string{"id", "sequence", "kind", "user_id", "refs", "recursive", "resync", "created_at"}, PrimaryKey: []string{"id"}},
		{Name: "notification_deliveries", Columns: []string{"id", "registration_id", "registration_revision", "transport_revision", "source_sequence", "kind", "refs", "recursive", "state", "attempts", "due_at", "lease_id", "lease_until", "outcome", "created_at", "updated_at"}, PrimaryKey: []string{"id"}},
	} {
		expected.SortKey = expected.PrimaryKey
		if !equalJSON(tables[expected.Name], expected) {
			t.Errorf("phase4 catalog lost complete durable identity/evidence for %s", expected.Name)
		}
	}
	found := false
	for _, column := range tables["managed_settings"].Columns {
		found = found || column == "runtime_overrides"
	}
	if !found {
		t.Fatal("the exact recovery catalog omitted managed runtime overrides")
	}
	historical, migrations, err := loadCatalog(46, "selected_phase4_history")
	if err != nil || len(historical.Tables) != 56 || len(migrations) != 46 {
		t.Fatalf("published schema46 recovery inventory changed: %v", err)
	}
}
