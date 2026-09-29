//go:build linux

package backuppg

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestPostgreSQLLibraryIntroSchema50ArchiveMigratesLegacyGlobalOptOut(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 50)
	seedAnalysisArchiveState(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,revision,options)
		VALUES('legacy-intro-library','Legacy TV','tvshows',9007199254740993,
		'{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":true}');
		UPDATE analysis_settings SET auto_publish_intros=false,revision=17 WHERE id=1`); err != nil {
		t.Fatalf("seed the actual schema50 library and global opt-out: %v", err)
	}
	retained := map[string]string{}
	for _, table := range []string{"libraries", "analysis_run_profiles", "analysis_work", "analysis_work_sources", "analysis_intro_decisions", "analysis_preview_state"} {
		retained[table] = historicalArchiveRows(t, ctx, source, table, 50)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 50 || len(facts.MigrationChecksums) != 50 || !equalJSON(facts, before) {
		t.Fatal("fixture did not produce an authentic schema50 archive")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 50 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the schema50 archive before applying the current migration suffix: %v", err)
	}
	for table, expected := range retained {
		if actual := historicalArchiveRows(t, ctx, target, table, 50); actual != expected {
			t.Fatalf("library policy migration rewrote retained %s rows", table)
		}
	}
	var valid bool
	if err := target.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM analysis_settings WHERE id=1 AND auto_publish_intros AND revision=18)
		AND NOT EXISTS(SELECT 1 FROM analysis_feature_cache)
		AND NOT EXISTS(SELECT 1 FROM analysis_previews)
		AND NOT EXISTS(SELECT 1 FROM analysis_detections WHERE auto_published)
		AND EXISTS(SELECT 1 FROM task_system_events WHERE name='IntroAnalysisRequested'
			AND sequence=0 AND occurred_at IS NOT NULL AND lifecycle_key='')
		AND NOT EXISTS(SELECT 1 FROM libraries WHERE options ? 'EnableIntroDetection')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("old opt-out did not become canonical policy with fresh execution authority: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, _ := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertCurrentRecoveryFacts(t, after)
	assertSourceWitness(t, ctx, source, options, before, sequences)
	if version, err := database.SchemaVersion(ctx, source); err != nil || version != 50 {
		t.Fatal("offline restoration modified its published schema50 source")
	}
}

func TestPostgreSQLLibraryIntroCurrentArchivePreservesOptionsAndPendingRequest(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,revision,options) VALUES
		('intro-enabled','Enabled TV','tvshows',9007199254740993,
		'{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":true}'),
		('intro-disabled','Disabled TV','tvshows',7,
		'{"EnableLocalMetadata":true,"EnableLocalImages":false,"EnableEmbeddedArtwork":false,"EnableIntroDetection":false}'),
		('intro-movies','Movie library','movies',9,
		'{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false}');
		UPDATE task_system_events SET sequence=9007199254740993,occurred_at='2026-09-30T00:00:00.123456Z'
		WHERE name='IntroAnalysisRequested'`); err != nil {
		t.Fatalf("seed current library policies and one retained intro request: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore current library options and pending automatic work: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, before) || len(targetSequences) != len(sequences) {
		t.Fatal("current restoration changed options, pending request identity, or another durable row")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("current restoration changed sequence %s", name)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
