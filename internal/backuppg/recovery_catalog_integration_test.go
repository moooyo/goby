//go:build linux

package backuppg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
)

func assertHistoricalRecoveryFacts(t *testing.T, ctx context.Context, source, target *pgxpool.Pool,
	expected, actual backupformat.SourceFacts, originalSequences, targetSequences map[string]sequenceState) {
	t.Helper()
	current := assertCurrentRecoveryFacts(t, actual)
	historical, _, err := loadCatalog(expected.SchemaVersion, expected.DatabaseSchema)
	if err != nil {
		t.Fatalf("load original archive catalog for migrated target comparison: %v", err)
	}
	if len(expected.Tables) != len(historical.Tables) {
		t.Fatal("historical archive facts do not match their catalog table inventory")
	}
	if actual.SchemaVersion <= expected.SchemaVersion || actual.SchemaSHA256 == expected.SchemaSHA256 ||
		actual.DatabaseSchema != expected.DatabaseSchema || actual.ServerID != expected.ServerID ||
		actual.ProbeVersion != expected.ProbeVersion {
		t.Fatal("historical restoration lost its source identity or current target schema")
	}
	currentTables := make(map[string]TableSpec, len(current.Tables))
	for _, table := range current.Tables {
		currentTables[table.Name] = table
	}
	targetFacts := make(map[string]backupformat.TableFact, len(actual.Tables))
	for _, table := range actual.Tables {
		targetFacts[table.Name] = table
	}
	for index, table := range historical.Tables {
		fact := expected.Tables[index]
		if fact.Name != table.Name {
			t.Fatal("historical archive facts do not match their catalog table order")
		}
		currentTable, exists := currentTables[table.Name]
		targetFact, hasFact := targetFacts[table.Name]
		if !exists || !hasFact {
			t.Fatalf("current target lost historical table %s", table.Name)
		}
		if table.Name != "schema_migrations" && equalJSON(table, currentTable) {
			if !equalJSON(fact, targetFact) {
				t.Errorf("migration changed original table fingerprint for %s", table.Name)
			}
		} else if historicalArchiveRows(t, ctx, source, table.Name, expected.SchemaVersion) !=
			historicalArchiveRows(t, ctx, target, table.Name, expected.SchemaVersion) {
			t.Errorf("migration changed an original field or row in %s", table.Name)
		}
	}
	if len(targetSequences) != len(current.Sequences) {
		t.Fatal("migrated target sequence inventory does not match the current catalog")
	}
	for _, sequence := range current.Sequences {
		if _, exists := targetSequences[sequence.Name]; !exists {
			t.Errorf("migrated target omitted current sequence %s", sequence.Name)
		}
	}
	for name, expected := range originalSequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Errorf("migration changed original sequence state for %s", name)
		}
	}
}
