//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/storagebinding"
)

const rootBindingArchiveRevision int64 = 9007199254740993
const rootBindingArchiveActor = "historical-binding-admin"

func rootBindingArchiveDocument(t *testing.T) (storagebinding.Snapshot, []byte) {
	t.Helper()
	snapshot := storagebinding.Snapshot{
		Version: storagebinding.TopologyVersion,
		Mapping: storagebinding.Mapping{ApprovedPath: "/synthetic", RegisteredPath: "/synthetic/theme"},
		Anchor: storagebinding.Identity{
			Version: storagebinding.IdentityVersion, Profile: storagebinding.IdentityProfile,
			FilesystemUUID: "00112233445566778899aabbccddeeff", HandleType: 1,
			Handle: []byte{0, 1, 127, 255, 0},
		},
		RegisteredRoot: storagebinding.Identity{
			Version: storagebinding.IdentityVersion, Profile: storagebinding.IdentityProfile,
			FilesystemUUID: "00112233445566778899aabbccddeeff", HandleType: 2,
			Handle: []byte{255, 0, 128, 2, 0, 3},
		},
		Boundaries: []storagebinding.Boundary{{
			RelativePath: "nested/volume",
			Identity: storagebinding.Identity{
				Version: storagebinding.IdentityVersion, Profile: storagebinding.IdentityProfile,
				FilesystemUUID: "ffeeddccbbaa99887766554433221100", HandleType: -7,
				Handle: []byte{0, 255, 4, 128, 5, 0, 6},
			},
		}},
	}
	document, err := storagebinding.EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("encode the complete root binding archive document: %v", err)
	}
	return snapshot, document
}

func rootBindingArchiveSeed(t *testing.T, ctx context.Context, source *pgxpool.Pool) (storagebinding.Snapshot, []byte) {
	t.Helper()
	// All media paths and identities are SQL fixture values. No media directory
	// is created, opened, captured, or implicitly approved by these archive tests.
	seedExtraSnapshotWitness(t, ctx, source)
	snapshot, document := rootBindingArchiveDocument(t)
	if _, err := source.Exec(ctx, `UPDATE library_roots SET binding_revision=$1,
		storage_binding=$2::jsonb,bound_at=$3,bound_by=$4 WHERE id='theme-root'`,
		rootBindingArchiveRevision, string(document),
		time.Date(2026, 9, 12, 1, 2, 3, 456789000, time.UTC), rootBindingArchiveActor); err != nil {
		t.Fatalf("seed all persisted root binding columns: %v", err)
	}
	rootBindingArchiveAssertBinding(t, ctx, source, document)
	return snapshot, document
}

func rootBindingArchiveAssertBinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected []byte) {
	t.Helper()
	var revision int64
	var document, actor string
	var boundAt time.Time
	var actorExists bool
	if err := pool.QueryRow(ctx, `SELECT binding_revision,storage_binding::text,bound_at,bound_by,
		EXISTS(SELECT 1 FROM users u WHERE u.id=r.bound_by)
		FROM library_roots r WHERE id='theme-root'`).Scan(&revision, &document, &boundAt, &actor, &actorExists); err != nil {
		t.Fatalf("read all restored root binding columns: %v", err)
	}
	if revision != rootBindingArchiveRevision || actor != rootBindingArchiveActor || actorExists ||
		!boundAt.Equal(time.Date(2026, 9, 12, 1, 2, 3, 456789000, time.UTC)) {
		t.Fatal("root binding restoration changed the exact revision, time, or historical actor")
	}
	decoded, err := storagebinding.DecodeSnapshot([]byte(document))
	if err != nil {
		t.Fatalf("decode the restored complete root binding document: %v", err)
	}
	canonical, err := storagebinding.EncodeSnapshot(decoded)
	if err != nil || !bytes.Equal(canonical, expected) {
		t.Fatal("root binding restoration changed the mapping, identities, or complete opaque handles")
	}
}

func rootBindingArchiveAssertRoundTrip(t *testing.T, ctx context.Context, target *pgxpool.Pool,
	options Options, expected backupformat.SourceFacts, sequences map[string]sequenceState) {
	t.Helper()
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	actual, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(actual, expected) || len(targetSequences) != len(sequences) {
		t.Fatal("root binding restoration changed full table facts or the sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("root binding restoration changed sequence %s", name)
		}
	}
}

func TestPostgreSQLRootBindingArchiveRoundTripsCompleteOfflineState(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	_, document := rootBindingArchiveSeed(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 28 || len(facts.Tables) != 35 || len(facts.MigrationChecksums) != 28 || !equalJSON(facts, before) {
		t.Fatal("the root binding archive did not contain complete schema28 source facts")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 28 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the complete root binding archive with an unavailable source: %v", err)
	}
	rootBindingArchiveAssertBinding(t, ctx, target, document)
	rootBindingArchiveAssertRoundTrip(t, ctx, target, options, before, sequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLRootBindingArchiveRejectsInvalidFinalizersAndRetries(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	snapshot, document := rootBindingArchiveSeed(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 28 || len(facts.Tables) != 35 || len(facts.MigrationChecksums) != 28 || !equalJSON(facts, before) {
		t.Fatal("the semantic finalizer fixture did not contain the actual schema28 archive")
	}
	mismatched := snapshot.Clone()
	mismatched.Mapping.RegisteredPath = "/synthetic/different-root"
	mismatchedDocument, err := storagebinding.EncodeSnapshot(mismatched)
	if err != nil {
		t.Fatalf("encode a structurally valid but mismatched stored mapping: %v", err)
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	for _, test := range []struct {
		name, document string
	}{
		{"empty_object", "{}"},
		{"unknown_field", string(document[:len(document)-1]) + `,"unknown_field":true}`},
		{"mismatched_mapping", string(mismatchedDocument)},
	} {
		if !t.Run(test.name, func(t *testing.T) {
			called, applied := false, false
			failed, err := RestoreOfflineFinalized(ctx, target, archive, facts, offline,
				func(ctx context.Context, tx pgx.Tx, result RestoreResult) error {
					called = true
					if result.SourceVersion != 28 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
						return errors.New("root binding finalizer received changed source facts")
					}
					tag, err := tx.Exec(ctx, `UPDATE library_roots SET storage_binding=$1::jsonb WHERE id='theme-root'`, test.document)
					applied = err == nil && tag.RowsAffected() == 1
					return err
				})
			if !called || !applied || !errors.Is(err, ErrSchema) || failed.CurrentVersion != 0 {
				t.Fatalf("a SQL-valid but semantically invalid binding was not rejected as ErrSchema: %v", err)
			}
			assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
			assertSourceWitness(t, ctx, source, options, before, sequences)
			if _, err := archive.Seek(0, 0); err != nil {
				t.Fatal("rewind the same root binding archive after semantic rollback")
			}
		}) {
			return
		}
	}
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 28 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("retry the unchanged root binding archive after all semantic rollbacks: %v", err)
	}
	rootBindingArchiveAssertBinding(t, ctx, target, document)
	rootBindingArchiveAssertRoundTrip(t, ctx, target, options, before, sequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLRootBindingArchiveMigratesSchema27WithoutInferringApproval(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 27)
	seedExtraSnapshotWitness(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('historical-anchor-root','theme-library','/synthetic','/synthetic','');
		INSERT INTO activity_entries(created_at,action,severity,source,actor_kind,actor_id,actor_credential_id,
			resource_kind,resource_id,request_id,revision,affected_count,state,changed_fields) VALUES
		('2020-01-13T01:02:03.456789Z','metadata.updated','Info','emby','application_key','historical-key','historical-credential',
			'item','theme-owner','historical-request',9007199254740993,11,'',ARRAY['Name','Overview','ProviderIds']),
		('2020-01-14T01:02:03.654321Z','backup.finished','Warn','system','system','','',
			'backup','historical-backup','',7,9,'cancelled',ARRAY[]::text[])`); err != nil {
		t.Fatalf("seed historical root paths and complete legacy audit fields: %v", err)
	}
	var columns int
	if err := source.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='library_roots'`).Scan(&columns); err != nil || columns != 5 {
		t.Fatal("the historical source did not retain the original five-column library_roots table")
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 27 || len(facts.Tables) != 35 || len(facts.MigrationChecksums) != 27 || !equalJSON(facts, before) {
		t.Fatal("the historical root archive did not authenticate the actual schema27 source")
	}
	rowsBefore := make(map[string]string, len(facts.Tables))
	for _, table := range facts.Tables {
		rowsBefore[table.Name] = historicalArchiveRows(t, ctx, source, table.Name, 27)
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 27 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore schema27 without replacing its authenticated source table facts: %v", err)
	}
	for table, expected := range rowsBefore {
		if historicalArchiveRows(t, ctx, target, table, 27) != expected {
			t.Errorf("root binding migration changed an original schema27 field in table %s", table)
		}
	}
	assertHistoricalArchiveBindingDefaults(t, ctx, target)
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	actual, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if actual.SchemaVersion != 28 || len(actual.Tables) != 35 || len(actual.MigrationChecksums) != 28 ||
		actual.SchemaSHA256 == facts.SchemaSHA256 || equalJSON(actual.Tables, facts.Tables) {
		t.Fatal("the migrated target was not independently fingerprinted as schema28")
	}
	if len(targetSequences) != len(sequences) {
		t.Fatal("root binding migration changed the historical sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("root binding migration changed historical sequence %s", name)
		}
	}
	if facts.SchemaVersion != 27 || !equalJSON(facts, before) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("root binding migration rewrote the original schema27 archive descriptor")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
