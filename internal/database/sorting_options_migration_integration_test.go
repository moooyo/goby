package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestSortingOptionsMigrationPreservesSchema48FactsAndExplicitKeys(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 48)
			if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,local_metadata) VALUES
			('sort-generated','wave-library','Ä Σ Title','ä σ title','Movie',NULL),
			('sort-nfo','wave-library','The NFO','the explicit nfo','Movie','{"SortName":"The explicit nfo"}'::jsonb),
			('sort-legacy-custom','wave-library','The legacy name','archival custom order','Movie',NULL);
			UPDATE libraries SET options='{"EnableLocalMetadata":false,"EnableLocalImages":true}' WHERE id='wave-library'`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path) VALUES
			('sort-extra-active','wave-library','wave-root','wave-movie','The Trailer','The Trailer','Video','/not-opened/wave/trailers/The Trailer.mp4','trailers/The Trailer.mp4'),
			('sort-extra-inactive','wave-library','wave-root','wave-movie','the trailer','the trailer','Video','/not-opened/wave/trailers/the trailer.mp4','trailers/the trailer.mp4'),
			('sort-theme-active','wave-library','wave-root','wave-movie','The Theme Song','The Theme Song','Audio','/not-opened/wave/theme-music/song.mp3','theme-music/song.mp3'),
			('sort-theme-inactive','wave-library','wave-root','wave-movie','The Backdrop','The Backdrop','Video','/not-opened/wave/backdrops/The Backdrop.mp4','backdrops/The Backdrop.mp4'),
			('sort-extra-custom','wave-library','wave-root','wave-movie','The Custom','historical custom order','Video','/not-opened/wave/trailers/The Custom.mp4','trailers/The Custom.mp4');
			INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('wave-root','trailers',true);
			INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('wave-root','theme-music',true),('wave-root','backdrops',true);
			INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES('sort-extra-active','wave-movie','trailer',true),('sort-extra-inactive','wave-movie','trailer',false),('sort-extra-custom','wave-movie','trailer',false);
			INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES('sort-theme-active','wave-movie','song',true),('sort-theme-inactive','wave-movie','video',false)`); err != nil {
				t.Fatal(err)
			}
			var before, after string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(ms) ORDER BY item_id)::text FROM item_metadata_state ms`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			history := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 49)
			}
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(ms)-'automatic_sort_name_explicit' ORDER BY item_id)::text FROM item_metadata_state ms`).Scan(&after); err != nil || after != before {
				t.Fatalf("migration changed retained metadata/source/control state: %v", err)
			}
			var retained string
			if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text FROM schema_migrations m WHERE version<=48`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("migration rewrote published schema48 history: %v", err)
			}
			for id, want := range map[string]bool{"sort-generated": false, "sort-nfo": true, "sort-legacy-custom": true,
				"sort-extra-active": false, "sort-extra-inactive": false, "sort-theme-active": false, "sort-theme-inactive": false, "sort-extra-custom": true} {
				var explicit bool
				if err := pool.QueryRow(ctx, `SELECT automatic_sort_name_explicit FROM item_metadata_state WHERE item_id=$1`, id).Scan(&explicit); err != nil || explicit != want {
					t.Fatalf("explicit provenance for%s=%v want%v: %v", id, explicit, want, err)
				}
			}
			var valid bool
			if err := pool.QueryRow(ctx, `SELECT (SELECT options='{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":true}'::jsonb FROM libraries WHERE id='wave-library')
			AND (SELECT sort_remove_words='{}'::text[] FROM managed_settings WHERE id=1)
			AND (SELECT sort_name='ä σ title' FROM items WHERE id='sort-generated')`).Scan(&valid); err != nil || !valid {
				t.Fatalf("new defaults changed old sorting or importer switches: %v", err)
			}
			for _, statement := range []string{
				`UPDATE libraries SET options='{"EnableLocalMetadata":true,"EnableLocalImages":true}' WHERE id='wave-library'`,
				`UPDATE managed_settings SET sort_remove_words=ARRAY['Ä','ä'] WHERE id=1`,
				`UPDATE managed_settings SET sort_remove_words='[0:1]={a,b}'::text[] WHERE id=1`,
			} {
				if _, err := pool.Exec(ctx, statement); err == nil {
					t.Fatal("schema49 accepted invalid persisted option state")
				}
			}
			complete := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 49)
			}
			if migrationHistory(t, ctx, pool) != complete {
				t.Fatal("repeated migration changed history")
			}
			for _, removed := range []bool{true, false} {
				words := []string{}
				if removed {
					words = []string{"The"}
				}
				if _, err := pool.Exec(ctx, `UPDATE managed_settings SET sort_remove_words=$1`, words); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `SELECT goby_rebuild_generated_sort_names()`); err != nil {
					t.Fatal(err)
				}
				for id, original := range map[string]string{"sort-extra-active": "The Trailer", "sort-extra-inactive": "the trailer", "sort-theme-active": "The Theme Song", "sort-theme-inactive": "The Backdrop", "sort-extra-custom": "historical custom order"} {
					want := original
					if removed && id != "sort-extra-custom" {
						want = original[4:]
					}
					var name, sortName string
					wantName := original
					if id == "sort-extra-custom" {
						wantName = "The Custom"
					}
					if err := pool.QueryRow(ctx, `SELECT name,sort_name FROM items WHERE id=$1`, id).Scan(&name, &sortName); err != nil || sortName != want || name != wantName {
						t.Fatalf("retained auxiliary sorting mode changed: item%s key%q want%q error%v", id, sortName, want, err)
					}
				}
			}
		})
	}
}
