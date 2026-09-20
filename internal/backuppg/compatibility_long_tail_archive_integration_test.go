//go:build linux

package backuppg

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestPostgreSQLCompatibilityLongTailRestoresPublishedSchema48WithExactOptionDefaults(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 48)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,revision,options)
		VALUES('long-tail-old-library','Published library','tvshows',9007199254740993,
		'{"EnableLocalMetadata":false,"EnableLocalImages":true}');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder,local_metadata)
		VALUES('long-tail-old-series','long-tail-old-library','The historical title','explicit retained order','Series',true,
		'{"SortName":"explicit retained order","UnknownSourceExtension":{"ExactInteger":9007199254740993}}')`); err != nil {
		t.Fatalf("seed authentic schema48 library and explicit sort provenance: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 48 || len(facts.MigrationChecksums) != 48 || !equalJSON(facts, before) {
		t.Fatal("the fixture did not create an exact schema48 archive")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 48 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the published schema48 archive through the current suffix: %v", err)
	}
	var valid bool
	if err := target.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM libraries WHERE id='long-tail-old-library' AND revision=9007199254740993
			AND options='{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":true}'::jsonb)
		AND EXISTS(SELECT 1 FROM managed_settings WHERE id=1 AND sort_remove_words='{}'::text[])
		AND EXISTS(SELECT 1 FROM item_metadata_state WHERE item_id='long-tail-old-series' AND automatic_sort_name_explicit)
		AND EXISTS(SELECT 1 FROM items WHERE id='long-tail-old-series' AND sort_name='explicit retained order'
			AND local_metadata->'UnknownSourceExtension'='{"ExactInteger":9007199254740993}'::jsonb)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration altered explicit metadata or inferred nondefault sorting/import state: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
	if version, err := database.SchemaVersion(ctx, source); err != nil || version != 48 {
		t.Fatal("offline recovery upgraded its historical source")
	}
}
