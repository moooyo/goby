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
		if table.Name == "libraries" && expected.SchemaVersion >= 36 && expected.SchemaVersion < 49 && actual.SchemaVersion >= 49 {
			// Predict only the specified default on the historical expectation.
			// Compare target rows unchanged, including every original option and
			// the exact new true value; never strip arbitrary target JSON keys.
			before := historicalArchiveRows(t, ctx, source, table.Name, expected.SchemaVersion)
			var migrated string
			if err := source.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(projected ORDER BY projected::text),'[]'::jsonb)::text
				FROM (SELECT jsonb_set(source_row,'{options}',(source_row->'options')||'{"EnableEmbeddedArtwork":true}'::jsonb) projected
				FROM jsonb_array_elements($1::jsonb) retained(source_row)) expected`, before).Scan(&migrated); err != nil {
				t.Fatal("derive the exact historical library-option transition")
			}
			if historicalArchiveRows(t, ctx, target, table.Name, expected.SchemaVersion) != migrated {
				t.Error("migration changed historical library rows beyond the declared embedded-artwork default")
			}
		} else if table.Name == "task_system_events" && expected.SchemaVersion < 60 && actual.SchemaVersion >= 51 {
			// Only the known migration suffix appends neutral event rows. Preserve
			// every original field and timestamp, and reject any other addition.
			added := []string{}
			if expected.SchemaVersion < 51 && actual.SchemaVersion >= 51 {
				added = append(added, "IntroAnalysisRequested")
			}
			if expected.SchemaVersion < 52 && actual.SchemaVersion >= 52 {
				added = append(added, "PreviewGenerationRequested")
			}
			if expected.SchemaVersion < 57 && actual.SchemaVersion >= 57 {
				added = append(added, "BackgroundPreviewGenerationRequested")
			}
			if expected.SchemaVersion < 58 && actual.SchemaVersion >= 58 {
				added = append(added, "AudioWaveformGenerationRequested")
			}
			if expected.SchemaVersion < 59 && actual.SchemaVersion >= 59 {
				added = append(added, "CreditsAnalysisRequested")
			}
			if expected.SchemaVersion < 60 && actual.SchemaVersion >= 60 {
				added = append(added, "SubtitleTimelineGenerationRequested")
			}
			before := historicalArchiveRows(t, ctx, source, table.Name, expected.SchemaVersion)
			after := historicalArchiveRows(t, ctx, target, table.Name, expected.SchemaVersion)
			var retained bool
			if err := source.QueryRow(ctx, `SELECT
				(SELECT COALESCE(jsonb_agg(entry ORDER BY entry::text),'[]'::jsonb)
				 FROM jsonb_array_elements($2::jsonb) restored(entry)
				 WHERE NOT (entry->>'name'=ANY($3::text[])))=$1::jsonb
				AND jsonb_array_length($2::jsonb)=jsonb_array_length($1::jsonb)+cardinality($3::text[])
				AND (SELECT count(*) FROM jsonb_array_elements($2::jsonb) restored(entry)
				 WHERE entry->>'name'=ANY($3::text[])
				 AND entry->'sequence'='0'::jsonb AND entry->>'lifecycle_key'=''
				 AND entry->'occurred_at' IS NOT NULL AND entry->'occurred_at'<>'null'::jsonb)=cardinality($3::text[])`, before, after, added).Scan(&retained); err != nil || !retained {
				t.Errorf("migration changed original event rows or added non-neutral analysis event state: %v", err)
			}
		} else if table.Name != "schema_migrations" && equalJSON(table, currentTable) {
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
