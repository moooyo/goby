//go:build linux

package backuppg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
)

func seedThemeSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('theme-library','Theme snapshot','movies'),('theme-foreign-library','Other theme snapshot','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('theme-root','theme-library','/synthetic/theme','/synthetic','theme'),
		('theme-second-root','theme-library','/synthetic/theme-second','/synthetic','theme-second'),
		('theme-foreign-root','theme-foreign-library','/synthetic/theme-foreign','/synthetic','theme-foreign');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('theme-library','theme-library','Theme library root','theme library root','CollectionFolder',true);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path) VALUES
		('theme-owner','theme-library','theme-root','theme-library','Theme owner','theme owner','Movie',false,'Owner/film.mp4'),
		('theme-other-owner','theme-library','theme-root','theme-library','Other owner','other owner','Movie',false,'Other/film.mp4'),
		('theme-fake-root','theme-library','theme-root','theme-library','Nested collection','nested collection','CollectionFolder',true,'Nested');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path,media) VALUES
		('theme-song','theme-library','theme-root','theme-owner','Theme song','theme song','Audio','Owner/theme.mp3',
		'{"ProbeVersion":6,"EmbeddedMusic":{"Version":2,"Title":"Theme song","AlbumArtist":"Retained ensemble"}}'::jsonb),
		('theme-video','theme-library','theme-root','theme-owner','Theme video','theme video','Video','Owner/backdrops/video.mp4','{"ProbeVersion":6}'::jsonb),
		('theme-inactive','theme-library','theme-root','theme-owner','Inactive theme','inactive theme','Audio','Owner/theme-music/inactive.flac','{"ProbeVersion":6}'::jsonb),
		('theme-root-song','theme-library','theme-second-root','theme-library','Root theme','root theme','Audio','theme.mp3','{"ProbeVersion":6}'::jsonb),
		('theme-hidden-legacy','theme-library','theme-root','theme-owner','Legacy reserved file','legacy reserved file','Video','Owner/backdrops/nested/legacy.bin','{"ProbeVersion":6}'::jsonb);
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES
		('theme-root','Owner/theme.mp3',false),('theme-root','Owner/backdrops',true),
		('theme-root','Owner/theme-music',true),('theme-second-root','theme.mp3',false);
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('theme-song','theme-owner','song',true),('theme-video','theme-owner','video',true),
		('theme-inactive','theme-owner','song',false),('theme-root-song','theme-library','song',true);
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,updated_at)
		VALUES('backup-admin','theme-song',9007199254740993,7,true,'2020-01-09T00:00:00Z')`); err != nil {
		t.Fatalf("seed persistent theme classifications and independent owner identities: %v", err)
	}
}

func themeSnapshotState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'owners',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM theme_owner_ids o),
		'paths',(SELECT jsonb_agg(to_jsonb(p) ORDER BY root_id,relative_path) FROM theme_reserved_paths p),
		'resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_theme_resources r),
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'userdata',(SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id) FROM user_item_data d))::text`).Scan(&state); err != nil {
		t.Fatal("capture complete private theme source rows")
	}
	return state
}

func assertThemeRestoreTargetEmpty(t *testing.T, ctx context.Context, target *pgxpool.Pool, schema string) {
	t.Helper()
	var count int
	if err := target.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM pg_class WHERE relnamespace=$1::regnamespace)+
		(SELECT count(*) FROM pg_proc WHERE pronamespace=$1::regnamespace)+
		(SELECT count(*) FROM pg_type WHERE typnamespace=$1::regnamespace)`, schema).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected theme restoration retained target tables, sequences, functions, or types")
	}
}

func TestPostgreSQLThemeRestorePreservesInactiveClassificationAndRetriesSemanticFinalizer(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 26)
	seedThemeSnapshotWitness(t, ctx, source)
	want := themeSnapshotState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 26 || len(facts.Tables) != 33 {
		t.Fatal("the theme preservation archive does not contain the actual schema26")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	called := false
	failed, err := RestoreOfflineFinalized(ctx, target, archive, facts, offline,
		func(ctx context.Context, tx pgx.Tx, result RestoreResult) error {
			called = true
			if result.SourceVersion != 26 || result.CurrentVersion != 27 {
				t.Fatal("the theme finalizer received a different migration transition")
			}
			_, err := tx.Exec(ctx, "DELETE FROM theme_owner_ids WHERE virtual_root")
			return err
		})
	if !called || !errors.Is(err, ErrSchema) || failed.CurrentVersion != 0 {
		t.Fatalf("a finalizer removed the unique root without failing the transaction: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	assertSourceWitness(t, ctx, source, options, before, sequences)
	if _, err := archive.Seek(0, 0); err != nil {
		t.Fatal("rewind unchanged theme archive for retry")
	}
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 26 || result.CurrentVersion != 27 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the same nonempty theme archive after semantic rollback: %v", err)
	}
	if themeSnapshotState(t, ctx, target) != want {
		t.Fatal("restore rotated owner IDs, lost inactive classification, reparented an item, or changed user data")
	}
	for _, test := range []struct {
		id       string
		ordinary bool
		direct   bool
	}{
		{"theme-owner", true, true}, {"theme-song", false, true}, {"theme-video", false, true},
		{"theme-inactive", false, false}, {"theme-root-song", false, true}, {"theme-hidden-legacy", false, false},
	} {
		var ordinary, direct bool
		if err := target.QueryRow(ctx, "SELECT "+database.ThemeOrdinaryItemSQL("i")+","+database.ThemeDirectItemSQL("i")+
			" FROM items i WHERE id=$1", test.id).Scan(&ordinary, &direct); err != nil || ordinary != test.ordinary || direct != test.direct {
			t.Fatalf("restored classification changed ordinary/direct eligibility for %s: %v", test.id, err)
		}
	}
	var maximum, allocated int64
	if err := target.QueryRow(ctx, "SELECT max(id) FROM theme_owner_ids").Scan(&maximum); err != nil {
		t.Fatal("read restored theme owner sequence bound")
	}
	if _, err := target.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type)
		VALUES('theme-after-restore','theme-library','New ordinary item','new ordinary item','Audio')`); err != nil {
		t.Fatal("allocate an owner after restoring the independent identity sequence")
	}
	if err := target.QueryRow(ctx, "SELECT id FROM theme_owner_ids WHERE item_id='theme-after-restore'").Scan(&allocated); err != nil || allocated <= maximum {
		t.Fatal("restored sequence reused an archived theme owner number")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLThemeSnapshotRejectsMissingCoverageAndInvalidInactiveOwners(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"missing_virtual_root", "DELETE FROM theme_owner_ids WHERE virtual_root"},
		{"missing_item_mapping", "DELETE FROM theme_owner_ids WHERE item_id='theme-owner'"},
		{"inactive_parent_mismatch", "UPDATE items SET parent_id='theme-other-owner' WHERE id='theme-inactive'"},
		{"active_owner_moved_root", "UPDATE items SET root_id='theme-second-root' WHERE id='theme-owner'"},
		{"active_owner_became_reserved", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-root','Owner/film.mp4',false)"},
		{"nested_collection_is_not_a_library_root", "UPDATE items SET parent_id='theme-fake-root' WHERE id='theme-root-song'; UPDATE item_theme_resources SET owner_item_id='theme-fake-root' WHERE resource_item_id='theme-root-song'"},
		{"registered_root_belongs_to_another_library", "UPDATE items SET root_id='theme-foreign-root' WHERE id='theme-root-song'; INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-foreign-root','theme.mp3',false)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, source, _, options := recoveryFixtureAtVersion(t, 26)
			seedThemeSnapshotWitness(t, ctx, source)
			if _, err := source.Exec(ctx, test.mutation); err != nil {
				t.Fatalf("construct a referentially valid semantic error: %v", err)
			}
			before := themeSnapshotState(t, ctx, source)
			snapshot, err := OpenSnapshot(ctx, source, options)
			if snapshot != nil {
				_ = snapshot.Close()
			}
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("backup accepted incomplete or shape-invalid theme state: %v", err)
			}
			if themeSnapshotState(t, ctx, source) != before {
				t.Fatal("backup validation repaired or changed invalid source data")
			}
		})
	}
}

func TestPostgreSQLThemeInactiveHistorySurvivesOwnerRootAndRoleChanges(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"owner_moved_root", "UPDATE items SET root_id='theme-second-root' WHERE id='theme-owner'"},
		{"owner_became_theme_resource", `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-root','Owner/film.mp4',false);
			UPDATE items SET type='Video' WHERE id='theme-owner';
			INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES('theme-owner','theme-library','video',true)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, source, target, options := recoveryFixtureAtVersion(t, 26)
			seedThemeSnapshotWitness(t, ctx, source)
			var userDataBefore string
			if err := source.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id)::text FROM user_item_data d").Scan(&userDataBefore); err != nil {
				t.Fatal("capture user history before changing theme owner eligibility")
			}
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin atomic owner change and child deactivation")
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, test.mutation+`; UPDATE item_theme_resources SET active=false WHERE owner_item_id='theme-owner'`); err != nil {
				t.Fatal("retain inactive child classifications when the owner changes eligibility")
			}
			if err := database.ValidateThemeState(ctx, tx, 26); err != nil {
				t.Fatalf("valid inactive history cannot follow an owner eligibility change: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal("commit the owner change and deactivation together")
			}
			before := themeSnapshotState(t, ctx, source)
			archive, facts := sourceArchive(t, ctx, source, options)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if err != nil || result.CurrentVersion != 27 || themeSnapshotState(t, ctx, target) != before {
				t.Fatalf("inactive history could not be preserved after its owner moved or became reserved: %v", err)
			}
			var active, ordinary, direct int
			if err := target.QueryRow(ctx, `SELECT count(*) FILTER(WHERE link.active),
				count(*) FILTER(WHERE `+database.ThemeOrdinaryItemSQL("i")+`),
				count(*) FILTER(WHERE `+database.ThemeDirectItemSQL("i")+`)
				FROM item_theme_resources link JOIN items i ON i.id=link.resource_item_id WHERE link.owner_item_id='theme-owner'`).Scan(&active, &ordinary, &direct); err != nil || active != 0 || ordinary != 0 || direct != 0 {
				t.Fatal("restored inactive children returned to ordinary or direct visibility")
			}
			var userDataAfter string
			if err := target.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id)::text FROM user_item_data d").Scan(&userDataAfter); err != nil || userDataAfter != userDataBefore {
				t.Fatal("owner eligibility changes or restore rewrote prior user history")
			}
		})
	}
}

// Construct a real pg_dump archive whose catalog and fingerprints are correct
// but whose application state is deliberately invalid. This bypass is local
// to the fixture: normal OpenSnapshot must reject it. Restore must enforce its
// own semantic boundary rather than trusting a source-side check or row hash.
func uncheckedThemeArchive(t *testing.T, ctx context.Context, source *pgxpool.Pool, plan *snapshotSource) (*os.File, backupformat.SourceFacts) {
	t.Helper()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin intentionally invalid theme archive fixture")
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, plan.options.Schema); err != nil {
		t.Fatal("configure the private exported archive fixture")
	}
	actual, _, err := inspectCatalog(ctx, tx, plan.options.Schema)
	if err != nil || !equalJSON(actual, plan.catalog) {
		t.Fatal("the invalid-data fixture changed its trusted catalog")
	}
	var exported string
	if tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&exported) != nil {
		t.Fatal("export the real invalid-data fixture snapshot")
	}
	snapshot := &Snapshot{plan: plan, tx: tx, ctx: ctx, cancel: func() {}, exported: exported}
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatal("fingerprint the intentionally invalid theme rows")
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "invalid-theme.dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create the private invalid-data archive")
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := snapshot.Dump(ctx, file); err != nil {
		t.Fatalf("dump the actual invalid theme fixture: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal("rewind actual invalid theme archive")
	}
	return file, facts
}

func TestPostgreSQLThemeRestoreRejectsSemanticallyInvalidRealArchives(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"missing_root", "DELETE FROM theme_owner_ids WHERE virtual_root"},
		{"inactive_wrong_parent", "UPDATE items SET parent_id='theme-other-owner' WHERE id='theme-inactive'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, source, target, options := recoveryFixtureAtVersion(t, 26)
			seedThemeSnapshotWitness(t, ctx, source)
			valid, err := OpenSnapshot(ctx, source, options)
			if err != nil {
				t.Fatalf("read exact source identity and compiled catalog before corruption: %v", err)
			}
			plan := valid.plan
			_ = valid.Close()
			if _, err := source.Exec(ctx, test.mutation); err != nil {
				t.Fatal("introduce the intended application-state error")
			}
			before := themeSnapshotState(t, ctx, source)
			archive, facts := uncheckedThemeArchive(t, ctx, source, plan)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if !errors.Is(err, ErrArchive) || result.CurrentVersion != 0 {
				t.Fatalf("restoration trusted row hashes without valid theme state: %v", err)
			}
			assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
			if themeSnapshotState(t, ctx, source) != before {
				t.Fatal("rejected restoration modified its invalid source")
			}
		})
	}
}
