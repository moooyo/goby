//go:build linux

package backuppg

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func assertHistoricalArchiveExtraDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM extra_reserved_paths)
		AND NOT EXISTS(SELECT 1 FROM item_extra_resources)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical archive inferred unattested extra reservations or resources: %v", err)
	}
}

func seedExtraSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedThemeSnapshotWitness(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path,media) VALUES
		('extra-clip','theme-library','theme-root','theme-owner','Bonus clip','bonus clip','Video','Owner/featurettes/clip.mp4','{"ProbeVersion":6,"RunTimeTicks":9007199254740993}'::jsonb),
		('extra-deleted','theme-library','theme-root','theme-owner','Deleted scene','deleted scene','Video','Owner/deleted scenes/scene.mp4','{"ProbeVersion":6}'::jsonb),
		('extra-trailer','theme-library','theme-root','theme-owner','Local trailer','local trailer','Video','Owner/trailers/trailer.mp4','{"ProbeVersion":6}'::jsonb),
		('extra-inactive','theme-library','theme-root','theme-owner','Retained bonus','retained bonus','Video','Owner/featurettes/inactive.mp4','{"ProbeVersion":6}'::jsonb),
		('extra-hidden','theme-library','theme-root','theme-owner','Nested legacy','nested legacy','Video','Owner/featurettes/nested/legacy.mp4','{"ProbeVersion":6}'::jsonb);
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES
		('theme-root','Owner/featurettes',true),('theme-root','Owner/deleted scenes',true),('theme-root','Owner/trailers',true);
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('extra-clip','theme-owner','clip',true),('extra-deleted','theme-owner','deleted_scene',true),
		('extra-trailer','theme-owner','trailer',true),('extra-inactive','theme-owner','clip',false);
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,updated_at) VALUES
		('backup-admin','extra-clip',9007199254740993,9,true,'2020-01-10T00:00:00Z'),
		('backup-admin','extra-inactive',9223372036854775806,11,true,'2020-01-11T00:00:00Z')`); err != nil {
		t.Fatalf("seed real active and inactive extra classifications alongside theme state: %v", err)
	}
}

func extraSnapshotState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'owners',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM theme_owner_ids o),
		'theme_paths',(SELECT jsonb_agg(to_jsonb(p) ORDER BY root_id,relative_path) FROM theme_reserved_paths p),
		'theme_resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_theme_resources r),
		'extra_paths',(SELECT jsonb_agg(to_jsonb(p) ORDER BY root_id,relative_path) FROM extra_reserved_paths p),
		'extra_resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_extra_resources r),
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'userdata',(SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id) FROM user_item_data d))::text`).Scan(&state); err != nil {
		t.Fatal("capture exact auxiliary identities, classifications, paths and user history")
	}
	return state
}

func TestPostgreSQLExtraArchivePreservesAllRolesAndRetriesSemanticFinalizer(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedExtraSnapshotWitness(t, ctx, source)
	want := extraSnapshotState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 27 || len(facts.Tables) != 35 || len(facts.MigrationChecksums) != 27 {
		t.Fatal("the extra archive did not contain the complete actual schema27")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	for _, mutation := range []string{
		"DELETE FROM extra_reserved_paths WHERE relative_path='Owner/featurettes'",
		"UPDATE items SET parent_id='theme-other-owner' WHERE id='extra-inactive'",
	} {
		called := false
		failed, err := RestoreOfflineFinalized(ctx, target, archive, facts, offline,
			func(ctx context.Context, tx pgx.Tx, result RestoreResult) error {
				called = true
				if result.SourceVersion != 27 || result.CurrentVersion != 27 || !equalJSON(result.Tables, facts.Tables) {
					return errors.New("extra finalizer received changed source facts")
				}
				_, err := tx.Exec(ctx, mutation)
				return err
			})
		if !called || !errors.Is(err, ErrSchema) || failed.CurrentVersion != 0 {
			t.Fatalf("a finalizer committed invalid extra state: %v", err)
		}
		assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
		assertSourceWitness(t, ctx, source, options, before, sequences)
		if _, err := archive.Seek(0, 0); err != nil {
			t.Fatal("rewind the same extra archive after finalizer rollback")
		}
	}
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 27 || result.CurrentVersion != 27 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the unchanged nonempty extra archive: %v", err)
	}
	if extraSnapshotState(t, ctx, target) != want {
		t.Fatal("restoration changed resource roles, stable IDs, owner IDs, media, reservations or exact user history")
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	targetFacts, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(targetFacts, before) || len(targetSequences) != len(sequences) {
		t.Fatal("the schema27 round trip changed complete source table fingerprints or sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("the schema27 round trip changed original sequence %s", name)
		}
	}
	for _, test := range []struct {
		id       string
		ordinary bool
		direct   bool
	}{
		{"theme-owner", true, true}, {"theme-song", false, true}, {"theme-inactive", false, false},
		{"extra-clip", false, true}, {"extra-deleted", false, true}, {"extra-trailer", false, true},
		{"extra-inactive", false, false}, {"extra-hidden", false, false},
	} {
		var ordinary, direct bool
		if err := target.QueryRow(ctx, "SELECT "+database.CatalogOrdinaryItemSQL("i")+","+database.CatalogDirectItemSQL("i")+
			" FROM items i WHERE id=$1", test.id).Scan(&ordinary, &direct); err != nil || ordinary != test.ordinary || direct != test.direct {
			t.Fatalf("restored resource eligibility changed for %s: %v", test.id, err)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLExtraSnapshotAndRestoreRejectSemanticCorruption(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"missing_reservation", "DELETE FROM extra_reserved_paths WHERE relative_path='Owner/featurettes'"},
		{"inactive_wrong_parent", "UPDATE items SET parent_id='theme-other-owner' WHERE id='extra-inactive'"},
		{"inactive_not_video", "UPDATE items SET type='Audio' WHERE id='extra-inactive'"},
		{"active_owner_moved_root", "UPDATE items SET root_id='theme-second-root' WHERE id='theme-owner'; UPDATE item_theme_resources SET active=false WHERE owner_item_id='theme-owner'"},
		{"active_owner_not_movie", "UPDATE items SET type='Series' WHERE id='theme-owner'"},
		{"active_owner_reserved", "INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-root','Owner/film.mp4',false); UPDATE item_theme_resources SET active=false WHERE owner_item_id='theme-owner'"},
		{"inactive_resource_foreign_root", "UPDATE items SET root_id='theme-foreign-root' WHERE id='extra-inactive'; INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-foreign-root','Owner/featurettes',true)"},
		{"inactive_cross_role", `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-root','Owner/featurettes',true);
			INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES('extra-inactive','theme-owner','video',false)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, source, target, options := recoveryFixture(t)
			seedExtraSnapshotWitness(t, ctx, source)
			valid, err := OpenSnapshot(ctx, source, options)
			if err != nil {
				t.Fatalf("read exact trusted catalog before semantic corruption: %v", err)
			}
			plan := valid.plan
			_ = valid.Close()
			if _, err := source.Exec(ctx, test.mutation); err != nil {
				t.Fatalf("construct referentially valid extra corruption: %v", err)
			}
			before := extraSnapshotState(t, ctx, source)
			snapshot, err := OpenSnapshot(ctx, source, options)
			if snapshot != nil {
				_ = snapshot.Close()
			}
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("snapshot accepted invalid extra semantics: %v", err)
			}
			tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal("begin private recovery-facts rejection fixture")
			}
			// Exercise the semantic recheck used after a locked recovery
			// inspection. Invalid state must fail before any facts are returned.
			inspection := &RecoveryInspection{tx: tx, catalog: plan.catalog, identity: plan.identity, version: 27, locked: true}
			_, recoveryErr := inspection.Facts(ctx, options.ProbeVersion)
			rollback(tx)
			if !errors.Is(recoveryErr, ErrSchema) {
				t.Fatalf("recovery facts accepted invalid extra semantics: %v", recoveryErr)
			}
			// The archive has real pg_dump bytes and matching table hashes.
			// Restore must independently reject the semantic defect.
			archive, facts := uncheckedThemeArchive(t, ctx, source, plan)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if !errors.Is(err, ErrArchive) || result.CurrentVersion != 0 {
				t.Fatalf("restoration trusted row hashes without valid extra state: %v", err)
			}
			assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
			if extraSnapshotState(t, ctx, source) != before {
				t.Fatal("snapshot or rejected restoration repaired invalid source rows")
			}
		})
	}
}

func TestPostgreSQLExtraInactiveHistorySurvivesOwnerEligibilityChanges(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"owner_moved_root", "UPDATE items SET root_id='theme-second-root' WHERE id='theme-owner'"},
		{"owner_changed_type", "UPDATE items SET type='Series' WHERE id='theme-owner'"},
		{"owner_became_reserved", "INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-root','Owner/film.mp4',false)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, source, target, options := recoveryFixture(t)
			seedExtraSnapshotWitness(t, ctx, source)
			if _, err := source.Exec(ctx, test.mutation+`; UPDATE item_theme_resources SET active=false WHERE owner_item_id='theme-owner';
				UPDATE item_extra_resources SET active=false WHERE owner_item_id='theme-owner'`); err != nil {
				t.Fatalf("deactivate historical resources with an owner eligibility change: %v", err)
			}
			before := extraSnapshotState(t, ctx, source)
			archive, facts := sourceArchive(t, ctx, source, options)
			if _, err := RestoreOffline(ctx, target, archive, facts, options); err != nil {
				t.Fatalf("restore valid inactive extra history: %v", err)
			}
			if extraSnapshotState(t, ctx, target) != before {
				t.Fatal("restored inactive history changed stable IDs, parents, roles or exact user data")
			}
			var exposed int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM items i WHERE id LIKE 'extra-%' AND ("+
				database.CatalogOrdinaryItemSQL("i")+" OR "+database.CatalogDirectItemSQL("i")+")").Scan(&exposed); err != nil || exposed != 0 {
				t.Fatalf("inactive or reserved extra history became visible after restore: %v", err)
			}
		})
	}
}
