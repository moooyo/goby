//go:build linux

package backuppg

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

func TestPostgreSQLSchema26ArchiveAuthenticatesBeforeSelectiveExtraMigration(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 26)
	seedThemeSnapshotWitness(t, ctx, source)
	// Schema26 treats this Movie as ordinary. Its valid Theme relationship
	// becomes inactive only when migration27 reserves the outer extra folder.
	if _, err := source.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path) VALUES
		('legacy-extra-owner','theme-library','theme-root','theme-library','Legacy extra owner','legacy extra owner','Movie','Legacy/featurettes/film.mp4'),
		('legacy-extra-theme','theme-library','theme-root','legacy-extra-owner','Legacy theme','legacy theme','Audio','Legacy/featurettes/theme.mp3'),
		('legacy-extra-inactive','theme-library','theme-root','legacy-extra-owner','Legacy inactive theme','legacy inactive theme','Video','Legacy/featurettes/backdrops/old.mp4');
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES
		('theme-root','Legacy/featurettes/theme.mp3',false),('theme-root','Legacy/featurettes/backdrops',true);
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('legacy-extra-theme','legacy-extra-owner','song',true),('legacy-extra-inactive','legacy-extra-owner','video',false);
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,updated_at)
		VALUES('backup-admin','legacy-extra-theme',9007199254740993,13,true,'2020-01-12T00:00:00Z')`); err != nil {
		t.Fatalf("seed actual schema26 relationships affected by a future reservation: %v", err)
	}
	var extraAbsent, historicallyOrdinary bool
	if err := source.QueryRow(ctx, "SELECT to_regclass('item_extra_resources') IS NULL AND to_regclass('extra_reserved_paths') IS NULL").Scan(&extraAbsent); err != nil || !extraAbsent {
		t.Fatal("the historical source already contained schema27 extra storage")
	}
	if err := source.QueryRow(ctx, "SELECT "+database.ThemeOrdinaryItemSQL("i")+" FROM items i WHERE id='legacy-extra-owner'").Scan(&historicallyOrdinary); err != nil || !historicallyOrdinary {
		t.Fatal("the schema26 witness was not an ordinary owner under its published semantics")
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 26 || len(facts.Tables) != 33 || len(facts.MigrationChecksums) != 26 {
		t.Fatal("the historical source does not match the published schema26 prefix")
	}
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	rowsBefore := make(map[string]string, len(facts.Tables))
	for _, table := range facts.Tables {
		rowsBefore[table.Name] = historicalArchiveRows(t, ctx, source, table.Name, 26)
	}
	var expectedLinks string
	if err := source.QueryRow(ctx, `SELECT jsonb_agg(
		CASE WHEN resource_item_id='legacy-extra-theme' THEN jsonb_set(to_jsonb(link),'{active}','false'::jsonb) ELSE to_jsonb(link) END
		ORDER BY (CASE WHEN resource_item_id='legacy-extra-theme' THEN jsonb_set(to_jsonb(link),'{active}','false'::jsonb) ELSE to_jsonb(link) END)::text)::text
		FROM item_theme_resources link`).Scan(&expectedLinks); err != nil || expectedLinks == rowsBefore["item_theme_resources"] {
		t.Fatal("the expected migration did not change exactly the selected active relationship")
	}

	// Construct a deliberately incorrect descriptor containing the *future*
	// link fingerprint, while the archive still contains the original active
	// row. A restore which authenticates only after migration would accept it.
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin private projected-fingerprint fixture")
	}
	if _, err := tx.Exec(ctx, "UPDATE item_theme_resources SET active=false WHERE resource_item_id='legacy-extra-theme'"); err != nil {
		rollback(tx)
		t.Fatal("project the selected future relationship in the fixture transaction")
	}
	catalog, _, err := loadCatalog(26, options.Schema)
	if err != nil {
		rollback(tx)
		t.Fatal("load unchanged schema26 catalog")
	}
	projected, err := fingerprints(ctx, tx, catalog)
	rollback(tx)
	if err != nil {
		t.Fatal("fingerprint the private projected future state")
	}
	wrongFacts := cloneFacts(facts)
	changed := false
	for index := range wrongFacts.Tables {
		if wrongFacts.Tables[index].Name == "item_theme_resources" {
			changed = wrongFacts.Tables[index].SHA256 != projected[index].SHA256
			wrongFacts.Tables[index] = projected[index]
		}
	}
	if !changed {
		t.Fatal("the projected future fingerprint did not differ from the raw archive")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	called := false
	result, err := RestoreOfflineFinalized(ctx, target, archive, wrongFacts, offline,
		func(context.Context, pgx.Tx, RestoreResult) error { called = true; return nil })
	if !errors.Is(err, ErrArchive) || called || result.CurrentVersion != 0 {
		t.Fatalf("restoration authenticated a post-migration hash in place of original schema26 data: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if _, err := archive.Seek(0, 0); err != nil {
		t.Fatal("rewind original schema26 archive after descriptor rejection")
	}
	called = false
	result, err = RestoreOfflineFinalized(ctx, target, archive, facts, offline,
		func(ctx context.Context, tx pgx.Tx, result RestoreResult) error {
			called = true
			if result.SourceVersion != 26 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
				return errors.New("migration finalizer lost original archive facts")
			}
			var active bool
			if err := tx.QueryRow(ctx, "SELECT active FROM item_theme_resources WHERE resource_item_id='legacy-extra-theme'").Scan(&active); err != nil || active {
				return errors.New("migration did not deactivate the newly hidden owner's relationship")
			}
			_, err := tx.Exec(ctx, "UPDATE item_theme_resources SET active=true WHERE resource_item_id='legacy-extra-theme'")
			return err
		})
	if !called || !errors.Is(err, ErrSchema) || result.CurrentVersion != 0 {
		t.Fatalf("a finalizer reactivated a relationship hidden by schema27 reservations: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	assertSourceWitness(t, ctx, source, options, before, sequences)
	if _, err := archive.Seek(0, 0); err != nil {
		t.Fatal("rewind the unchanged historical archive after semantic rollback")
	}
	result, err = RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 26 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore exact schema26 archive with its permitted selective migration: %v", err)
	}
	for table, expected := range rowsBefore {
		if table == "item_theme_resources" {
			expected = expectedLinks
		}
		if historicalArchiveRows(t, ctx, target, table, 26) != expected {
			t.Errorf("migration changed original schema26 rows outside its selected active flag in %s", table)
		}
	}
	for name, expected := range sequences {
		var actual sequenceState
		if err := target.QueryRow(ctx, "SELECT last_value,is_called FROM "+qualified(options.Schema, name)).Scan(&actual.value, &actual.called); err != nil || actual != expected {
			t.Fatalf("migration changed the original sequence %s: %v", name, err)
		}
	}
	var valid bool
	if err := target.QueryRow(ctx, `SELECT
		(SELECT max(version) FROM schema_migrations)=28
		AND (SELECT count(*) FROM pg_tables WHERE schemaname=current_schema())=35
		AND (SELECT count(*) FROM extra_reserved_paths)=1
		AND EXISTS(SELECT 1 FROM extra_reserved_paths WHERE root_id='theme-root' AND relative_path='Legacy/featurettes' AND is_directory)
		AND NOT EXISTS(SELECT 1 FROM item_extra_resources)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("schema26 migration inferred extras or missed its exact reserved boundary: %v", err)
	}
	assertHistoricalArchiveBindingDefaults(t, ctx, target)
	if err := database.Migrate(ctx, target); err != nil {
		t.Fatalf("repeat the completed trusted migration: %v", err)
	}
	if historicalArchiveRows(t, ctx, target, "item_theme_resources", 26) != expectedLinks {
		t.Fatal("migration retry changed already normalized historical classifications")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
