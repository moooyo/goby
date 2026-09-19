//go:build linux

package backuppg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLPhase3Schema35ArchivePreservesHistoricalRows(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 35)
	seedMusicSnapshotWitness(t, ctx, source)
	_, binding := rootBindingArchiveSeed(t, ctx, source)
	seedPhase3HistoricalTaskWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 35 || len(facts.MigrationChecksums) != 35 || len(facts.Tables) != 42 ||
		!equalJSON(facts, before) {
		t.Fatal("the phase 3 transition source is not the complete published schema35 archive")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 35 || result.CurrentVersion != currentRecoveryVersion(t) ||
		result.CurrentVersion <= result.SourceVersion || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the published schema35 source through the complete phase 3 migration prefix: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	rootBindingArchiveAssertBinding(t, ctx, target, binding)
	assertMusicSnapshotIdentity(t, ctx, target)
	assertPhase3HistoricalLibraryDefaults(t, ctx, target)
	assertPhase3HistoricalPreferenceDefaults(t, ctx, target)
	assertPhase3HistoricalArtworkDefaults(t, ctx, target)
	assertPhase3HistoricalSystemEventDefaults(t, ctx, target)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLPhase3CurrentArchivePreservesNativeState(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedMusicSnapshotWitness(t, ctx, source)
	_, binding := rootBindingArchiveSeed(t, ctx, source)
	seedPhase3LibrarySnapshotWitness(t, ctx, source)
	seedPhase3PreferenceSnapshotWitness(t, ctx, source)
	seedPhase3ArtworkSnapshotWitness(t, ctx, source)
	seedPhase3SystemEventSnapshotWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if !equalJSON(before, facts) {
		t.Fatal("phase 3 native state changed after the archive snapshot")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) ||
		!equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore complete phase 3 native state without the source database: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertCurrentRecoveryFacts(t, after)
	if !equalJSON(after, facts) || len(targetSequences) != len(sequences) {
		t.Fatal("phase 3 restoration changed complete source fingerprints or sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("phase 3 restoration changed sequence %s", name)
		}
	}
	rootBindingArchiveAssertBinding(t, ctx, target, binding)
	assertMusicSnapshotIdentity(t, ctx, target)
	assertPhase3LibrarySnapshotWitness(t, ctx, target)
	assertPhase3PreferenceSnapshotWitness(t, ctx, target)
	assertPhase3ArtworkSnapshotWitness(t, ctx, target)
	assertPhase3SystemEventSnapshotWitness(t, ctx, target)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func assertPhase3HistoricalLibraryDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM libraries
		WHERE revision IS DISTINCT FROM 1
		OR options IS DISTINCT FROM '{"EnableLocalMetadata":true,"EnableLocalImages":true}'::jsonb)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical restoration changed neutral library edit defaults: %v", err)
	}
}

func seedPhase3LibrarySnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE libraries SET revision=9007199254740993,
		options='{"EnableLocalMetadata":false,"EnableLocalImages":true}'::jsonb
		WHERE id='theme-library';
		UPDATE libraries SET revision=9007199254740995,
		options='{"EnableLocalMetadata":true,"EnableLocalImages":false}'::jsonb
		WHERE id='music-snapshot-library'`); err != nil {
		t.Fatalf("seed independent library edit revisions and local-resource options: %v", err)
	}
	assertPhase3LibrarySnapshotWitness(t, ctx, pool)
}

func assertPhase3LibrarySnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM libraries WHERE id='theme-library' AND revision=9007199254740993
			AND options='{"EnableLocalMetadata":false,"EnableLocalImages":true}'::jsonb)
		AND EXISTS(SELECT 1 FROM libraries WHERE id='music-snapshot-library' AND revision=9007199254740995
			AND options='{"EnableLocalMetadata":true,"EnableLocalImages":false}'::jsonb)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("restoration changed exact library edit state or replayed root revision triggers during COPY: %v", err)
	}
	// Exercise the restored trigger in a rollback-only transaction. The raw
	// archive comparison above must continue to retain the original revision.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin the rollback-only library revision trigger witness")
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id='theme-root'`); err != nil {
		t.Fatalf("update the restored root binding through its library revision trigger: %v", err)
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM libraries WHERE id='theme-library'`).Scan(&revision); err != nil || revision != 9007199254740994 {
		t.Fatalf("the restored root trigger did not advance exactly one library revision: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("discard the library revision trigger witness")
	}
}

func seedPhase3HistoricalTaskWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name,revision)
		VALUES(repeat('6',32),'phase3.historical','Historical startup task',7);
		INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind)
		VALUES(repeat('7',32),repeat('6',32),7,0,'startup');
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,trigger_id,trigger_revision,
			scheduled_for,started_at,finished_at)
		VALUES(repeat('8',32),repeat('6',32),'completed','startup','phase3.historical','Historical startup task',
			repeat('7',32),7,'2020-01-05T00:00:00Z','2020-01-05T00:00:00Z','2020-01-05T00:01:00Z')`); err != nil {
		t.Fatalf("seed original schema35 task history before phase 3 migration: %v", err)
	}
}

func assertPhase3HistoricalSystemEventDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM task_system_events)=3
		AND (SELECT count(*) FROM task_system_events WHERE name IN
			('ServerStarted','LibraryChanged','ConfigurationChanged') AND sequence=0
			AND lifecycle_key='' AND occurred_at IS NOT NULL)=3
		AND NOT EXISTS(SELECT 1 FROM task_system_event_receipts)
		AND EXISTS(SELECT 1 FROM task_triggers WHERE id=repeat('7',32)
			AND kind='startup' AND system_event IS NULL AND last_event_sequence=0)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical restoration inferred system events or changed the neutral legacy trigger defaults: %v", err)
	}
}

func seedPhase3SystemEventSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE task_system_events SET sequence=9007199254741001,
		occurred_at='2020-01-05T00:02:00.123456Z' WHERE name='LibraryChanged';
		UPDATE task_system_events SET sequence=9007199254740993,
		occurred_at='2020-01-05T00:00:00.654321Z',lifecycle_key='retained-startup-generation'
		WHERE name='ServerStarted';
		INSERT INTO task_definitions(id,key,name,revision)
		VALUES(repeat('6',32),'phase3.events','Retained event task',7);
		INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,system_event,last_event_sequence)
		VALUES(repeat('7',32),repeat('6',32),7,0,'system_event','LibraryChanged',9007199254741001);
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,trigger_id,trigger_revision,
			scheduled_for,started_at,finished_at)
		VALUES(repeat('8',32),repeat('6',32),'completed','system_event','phase3.events','Retained event task',
			repeat('7',32),7,'2020-01-05T00:01:00Z','2020-01-05T00:01:00Z','2020-01-05T00:02:00Z');
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name)
		VALUES(repeat('9',32),repeat('6',32),'pending','manual','phase3.events','Retained event task');
		INSERT INTO task_system_event_receipts(trigger_id,task_id,schedule_revision,system_event,
			first_sequence,last_sequence,occurred_at,run_id,disposition)
		VALUES(repeat('7',32),repeat('6',32),7,'LibraryChanged',9007199254740993,9007199254740997,
			'2020-01-05T00:01:00.123456Z',repeat('8',32),'admitted'),
			(repeat('7',32),repeat('6',32),7,'LibraryChanged',9007199254740998,9007199254741001,
			'2020-01-05T00:02:00.123456Z',repeat('9',32),'overlap')`); err != nil {
		t.Fatalf("seed durable phase 3 event counters, coalesced ranges, and distinct task dispositions: %v", err)
	}
	assertPhase3SystemEventSnapshotWitness(t, ctx, pool)
}

func assertPhase3SystemEventSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM task_system_events WHERE name='ServerStarted' AND sequence=9007199254740993
			AND lifecycle_key='retained-startup-generation' AND occurred_at='2020-01-05T00:00:00.654321Z')
		AND EXISTS(SELECT 1 FROM task_system_events WHERE name='LibraryChanged' AND sequence=9007199254741001
			AND lifecycle_key='' AND occurred_at='2020-01-05T00:02:00.123456Z')
		AND EXISTS(SELECT 1 FROM task_triggers WHERE id=repeat('7',32) AND kind='system_event'
			AND system_event='LibraryChanged' AND last_event_sequence=9007199254741001)
		AND (SELECT count(*) FROM task_system_event_receipts WHERE trigger_id=repeat('7',32))=2
		AND EXISTS(SELECT 1 FROM task_system_event_receipts WHERE trigger_id=repeat('7',32)
			AND first_sequence=9007199254740993 AND last_sequence=9007199254740997
			AND run_id=repeat('8',32) AND disposition='admitted')
		AND EXISTS(SELECT 1 FROM task_system_event_receipts WHERE trigger_id=repeat('7',32)
			AND first_sequence=9007199254740998 AND last_sequence=9007199254741001
			AND run_id=repeat('9',32) AND disposition='overlap')
		AND EXISTS(SELECT 1 FROM task_runs WHERE id=repeat('8',32) AND source='system_event' AND state='completed')
		AND EXISTS(SELECT 1 FROM task_runs WHERE id=repeat('9',32) AND source='manual' AND state='pending')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("restoration changed exact system-event state or normalized raw task history: %v", err)
	}
}
