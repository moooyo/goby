//go:build linux

package backuppg

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestPostgreSQLSelectedTranscodingControlsSchema78ArchivePreservesCompleteRuntimeOverrides(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 78)
	const revision int64 = 9007199254741021
	enableThrottling, extractionThreads := true, 0
	want := managedTranscodingArchiveRuntimeOverrides()
	want.EnableThrottling = &enableThrottling
	want.ExtractionThreadCount = &extractionThreads
	want.CodecProfileLimits = &transcode.CodecProfileLimits{H264: "baseline", HEVC: "main"}
	want.SubtitleOptions = &settings.RuntimeSubtitleOptions{AllowAssToText: false}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal("encode all fifteen managed runtime groups", err)
	}
	if err := settings.ValidateStoredRuntimeOverrides(raw); err != nil {
		t.Fatal("the complete selected controls must satisfy the raw archive contract", err)
	}
	seeded, err := source.Exec(ctx, `UPDATE managed_settings SET runtime_overrides=$1,revision=$2 WHERE id=1`, raw, revision)
	if err != nil || seeded.RowsAffected() != 1 {
		t.Fatal("seed the complete selected controls and exact revision", err)
	}
	var originalStored []byte
	if err := source.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&originalStored); err != nil {
		t.Fatal("capture the canonical selected-control runtime row", err)
	}
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	archive, facts := sourceArchive(t, ctx, source, options)
	catalog, migrations, err := loadCatalog(78, options.Schema)
	if err != nil || facts.SchemaVersion != 78 || len(facts.MigrationChecksums) != 78 ||
		facts.SchemaSHA256 != catalog.SHA256 || !equalJSON(facts.MigrationChecksums, migrations) ||
		!equalJSON(facts, before) {
		t.Fatal("the actual pg_dump archive lost its schema78 selected-control witness", err)
	}
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != 78 || result.CurrentVersion != currentRecoveryVersion(t) ||
		!equalJSON(result.Tables, facts.Tables) {
		t.Fatal("restore the actual schema78 selected-control archive", err)
	}
	for _, database := range []struct {
		name string
		pool *pgxpool.Pool
	}{{"source", source}, {"target", target}} {
		var savedRevision int64
		var saved []byte
		if err := database.pool.QueryRow(ctx, `SELECT revision,runtime_overrides FROM managed_settings WHERE id=1`).Scan(&savedRevision, &saved); err != nil {
			t.Fatalf("read %s selected-control state: %v", database.name, err)
		}
		if err := settings.ValidateStoredRuntimeOverrides(saved); err != nil {
			t.Fatalf("%s selected controls failed raw archive validation: %v", database.name, err)
		}
		var actual settings.RuntimeOverrides
		var groups map[string]json.RawMessage
		if json.Unmarshal(saved, &actual) != nil || json.Unmarshal(saved, &groups) != nil || len(groups) != 15 ||
			savedRevision != revision || !bytes.Equal(saved, originalStored) || !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s changed a runtime group, automatic image threads, explicit subtitle false, profile limit, or exact revision", database.name)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
