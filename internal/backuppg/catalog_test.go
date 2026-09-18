package backuppg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
)

func currentRecoveryVersion(t *testing.T) int64 {
	t.Helper()
	migrations, err := database.EmbeddedMigrations()
	if err != nil || len(migrations) == 0 {
		t.Fatalf("read current compiled migration inventory: %v", err)
	}
	return migrations[len(migrations)-1].Version
}

func currentRecoveryCatalog(t *testing.T, schema string) (Catalog, []backupformat.MigrationFact) {
	t.Helper()
	version := currentRecoveryVersion(t)
	catalog, migrations, err := loadCatalog(version, schema)
	if err != nil {
		t.Fatalf("load the current schema %d recovery catalog: %v", version, err)
	}
	return catalog, migrations
}

func assertCurrentRecoveryFacts(t *testing.T, facts backupformat.SourceFacts) Catalog {
	t.Helper()
	catalog, migrations := currentRecoveryCatalog(t, facts.DatabaseSchema)
	if facts.SchemaVersion != currentRecoveryVersion(t) || facts.SchemaSHA256 != catalog.SHA256 ||
		!equalJSON(facts.MigrationChecksums, migrations) || len(facts.Tables) != len(catalog.Tables) {
		t.Fatal("current archive facts do not match the complete compiled recovery catalog")
	}
	for index, table := range catalog.Tables {
		if facts.Tables[index].Name != table.Name {
			t.Fatalf("current archive table inventory omitted or reordered %s", table.Name)
		}
	}
	return catalog
}

func TestPublishedRecoveryCatalogsRetainHistoricalMigrationPrefixes(t *testing.T) {
	digests := make(map[string]int64)
	for _, baseline := range []struct {
		version int64
		tables  int
	}{{23, 29}, {24, 30}, {25, 30}, {26, 33}, {27, 35}, {28, 35}, {29, 35}} {
		t.Run(fmt.Sprintf("schema%d", baseline.version), func(t *testing.T) {
			catalog, migrations, err := loadCatalog(baseline.version, "published_catalog_test")
			if err != nil {
				t.Fatalf("load the authenticated published schema %d catalog: %v", baseline.version, err)
			}
			if len(catalog.Tables) != baseline.tables || len(migrations) != int(baseline.version) || migrations[len(migrations)-1].Version != baseline.version {
				t.Fatalf("schema %d lost its historical table inventory or migration prefix", baseline.version)
			}
			if previous, exists := digests[catalog.SHA256]; exists {
				t.Fatalf("schema %d reused schema %d's catalog digest", baseline.version, previous)
			}
			digests[catalog.SHA256] = baseline.version
		})
	}
}

func TestCurrentRecoveryCatalogIncludesFeatureWaveMetadata(t *testing.T) {
	catalog, migrations := currentRecoveryCatalog(t, "current_catalog_test")
	if migrations[len(migrations)-1].Version != currentRecoveryVersion(t) {
		t.Fatal("current recovery catalog does not contain the complete migration prefix")
	}
	tables := make(map[string]TableSpec, len(catalog.Tables))
	for _, table := range catalog.Tables {
		if _, exists := tables[table.Name]; exists {
			t.Fatalf("current recovery catalog repeats table %s", table.Name)
		}
		tables[table.Name] = table
	}
	for _, expected := range []TableSpec{
		{Name: "media_collections", Columns: []string{"item_id", "owner_id", "kind", "media_type", "is_public", "is_locked", "created_at", "updated_at"}, PrimaryKey: []string{"item_id"}},
		{Name: "media_collection_entries", Columns: []string{"id", "collection_id", "item_id", "position"}, PrimaryKey: []string{"id"}},
		{Name: "media_collection_shares", Columns: []string{"collection_id", "user_id", "can_edit"}, PrimaryKey: []string{"collection_id", "user_id"}},
		{Name: "item_provider_sources", Columns: []string{"item_id", "provider", "provider_id", "source_url", "language", "fields", "fetched_at"}, PrimaryKey: []string{"item_id", "provider"}},
		{Name: "item_provider_images", Columns: []string{"item_id", "image_type", "image_index", "provider", "provider_id", "image_id", "content", "mime_type", "width", "height", "source_hash", "fetched_at"}, PrimaryKey: []string{"item_id", "image_type", "image_index"}},
		{Name: "item_subtitle_provider_sources", Columns: []string{"item_id", "stream_index", "provider", "provider_id", "downloaded_at"}, PrimaryKey: []string{"item_id", "stream_index"}},
		{Name: "media_deletion_operations", Columns: []string{"id", "item_id", "library_id", "root_id", "actor_id", "credential_id", "origin_host", "kind", "subtitle_index", "state", "source_snapshot", "created_at", "updated_at"}, PrimaryKey: []string{"id"}},
	} {
		expected.SortKey = expected.PrimaryKey
		if actual, exists := tables[expected.Name]; !exists || !equalJSON(actual, expected) {
			t.Errorf("current recovery catalog lost complete copy columns or stable identity for %s", expected.Name)
		}
	}
	for table, appended := range map[string][]string{
		"item_metadata_state": {"online_source", "online_type", "online_base"},
		"task_run_children":   {"executor_token"},
		"managed_settings":    {"management"},
		"play_sessions":       {"is_dynamic"},
	} {
		columns := tables[table].Columns
		if len(columns) < len(appended) || !equalJSON(columns[len(columns)-len(appended):], appended) {
			t.Errorf("current recovery catalog omitted appended columns for %s", table)
		}
	}
	expectedSequence := SequenceSpec{
		Name: "media_collection_entries_id_seq", Table: "media_collection_entries", Column: "id",
		MinValue: 1, MaxValue: math.MaxInt64, Increment: 1,
		Consumers: []SequenceColumn{{Table: "media_collection_entries", Column: "id"}},
	}
	foundSequence := false
	for _, sequence := range catalog.Sequences {
		if sequence.Name == expectedSequence.Name {
			foundSequence = true
			if !equalJSON(sequence, expectedSequence) {
				t.Error("collection entry sequence lost its stable ownership or consumer metadata")
			}
		}
		for _, consumer := range append([]SequenceColumn{{Table: sequence.Table, Column: sequence.Column}}, sequence.Consumers...) {
			table, exists := tables[consumer.Table]
			foundColumn := false
			for _, column := range table.Columns {
				foundColumn = foundColumn || column == consumer.Column
			}
			if !exists || !foundColumn {
				t.Errorf("sequence %s references an absent table or column", sequence.Name)
			}
		}
	}
	if !foundSequence {
		t.Error("current recovery catalog omitted the collection entry identity sequence")
	}
	constraints := make(map[string]map[string]bool)
	for _, constraint := range catalog.Constraints {
		if _, exists := tables[constraint.Table]; !exists {
			t.Errorf("foreign key %s belongs to an absent table", constraint.Name)
		}
		if constraints[constraint.Table] == nil {
			constraints[constraint.Table] = make(map[string]bool)
		}
		constraints[constraint.Table][constraint.Definition] = true
	}
	for table, definitions := range map[string][]string{
		"media_collections": {
			"FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE",
			"FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		"media_collection_entries": {
			"FOREIGN KEY (collection_id) REFERENCES media_collections(item_id) ON DELETE CASCADE",
			"FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE",
		},
		"media_collection_shares": {
			"FOREIGN KEY (collection_id) REFERENCES media_collections(item_id) ON DELETE CASCADE",
			"FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		"item_provider_sources":          {"FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE"},
		"item_provider_images":           {"FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE"},
		"item_subtitle_provider_sources": {"FOREIGN KEY (item_id, stream_index) REFERENCES item_subtitles(item_id, stream_index) ON DELETE CASCADE"},
		"media_deletion_operations": {
			"FOREIGN KEY (library_id) REFERENCES libraries(id) ON DELETE RESTRICT",
			"FOREIGN KEY (root_id) REFERENCES library_roots(id) ON DELETE RESTRICT",
		},
	} {
		if len(constraints[table]) != len(definitions) {
			t.Errorf("current recovery catalog changed the foreign key inventory for %s", table)
		}
		for _, definition := range definitions {
			if !constraints[table][definition] {
				t.Errorf("current recovery catalog omitted a required reference for %s: %s", table, definition)
			}
		}
	}
}

func TestCatalogCanonicalJSONPreservesEscapesAndInt64(t *testing.T) {
	original := []byte(`[{"value":{"source":"CHECK (id > 0 AND id < 9223372036854775807)","maximum":9223372036854775807},"name":"sequence"}]`)
	canonical, err := normalizeCatalogObjects(original)
	if err != nil {
		t.Fatal("normalize PostgreSQL object representation")
	}
	transport, err := json.MarshalIndent(json.RawMessage(original), "", "  ")
	if err != nil {
		t.Fatal("serialize baseline transport")
	}
	if !bytes.Contains(transport, []byte(`\u003c`)) {
		t.Fatal("fixture did not exercise HTML escaping")
	}
	recovered, err := normalizeCatalogObjects(transport)
	if err != nil || !bytes.Equal(canonical, recovered) {
		t.Fatal("JSON transport escaping changed the trusted schema digest input")
	}
	if !bytes.Contains(canonical, []byte("9223372036854775807")) || bytes.Contains(canonical, []byte("9223372036854775808")) {
		t.Fatal("schema normalization lost bigint precision")
	}
	if _, err := normalizeCatalogObjects(append(original, []byte(` {"extra":true}`)...)); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}
