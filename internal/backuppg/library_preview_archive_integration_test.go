//go:build linux

package backuppg

import "testing"

func TestPostgreSQLLibraryPreviewSchema51ArchivePreservesExistingAnalysisAndOptions(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 51)
	seedAnalysisArchiveState(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,revision,options)
		VALUES('legacy-preview-library','Legacy TV','tvshows',9007199254740993,
		'{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":true}');
		UPDATE task_system_events SET sequence=17,occurred_at='2026-09-30T00:00:00Z' WHERE name='IntroAnalysisRequested'`); err != nil {
		t.Fatal("seed the authentic schema51 policy and retained intro request", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 51 || len(facts.MigrationChecksums) != 51 {
		t.Fatal("preview migration fixture is not an authentic schema51 archive")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 51 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("restore schema51 with its original row fingerprints before the preview migration", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	var unchanged bool
	if err := target.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM libraries WHERE options ? 'EnablePreviewGeneration')
		AND EXISTS(SELECT 1 FROM task_system_events WHERE name='PreviewGenerationRequested'
			AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL)
		AND (SELECT count(*) FROM analysis_feature_cache)=1`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("preview migration inferred an opt-in, changed analysis bytes, or fabricated pending work", err)
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLLibraryPreviewCurrentArchivePreservesIndependentAutomation(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,revision,options) VALUES
		('preview-movies','Preview movies','movies',17,
		'{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false,"EnablePreviewGeneration":true}'),
		('preview-tv','Preview TV','tvshows',9007199254740993,
		'{"EnableLocalMetadata":true,"EnableLocalImages":false,"EnableEmbeddedArtwork":true,"EnableIntroDetection":true,"EnablePreviewGeneration":true}'),
		('preview-mixed','Preview mixed','mixed',7,
		'{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":false,"EnableIntroDetection":false,"EnablePreviewGeneration":true}'),
		('preview-disabled','Disabled previews','tvshows',9,
		'{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":true,"EnablePreviewGeneration":false}');
		UPDATE task_system_events SET sequence=9007199254740993,occurred_at='2026-09-30T00:00:00.123456Z'
		WHERE name='PreviewGenerationRequested';
		UPDATE task_system_events SET sequence=17,occurred_at='2026-09-30T00:00:01.123456Z'
		WHERE name='IntroAnalysisRequested'`); err != nil {
		t.Fatal("seed independent current automation switches and exact pending requests", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("restore independent preview policy and exact pending work", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, before) || len(targetSequences) != len(sequences) {
		t.Fatal("current restore changed automation, retained analysis, or durable identity")
	}
	for name, expected := range sequences {
		if value, present := targetSequences[name]; !present || value != expected {
			t.Fatalf("current restore changed sequence %s", name)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
