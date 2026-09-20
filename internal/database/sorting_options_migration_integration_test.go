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
			for id, want := range map[string]bool{"sort-generated": false, "sort-nfo": true, "sort-legacy-custom": true} {
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
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if migrationHistory(t, ctx, pool) != complete {
				t.Fatal("repeated migration changed history")
			}
		})
	}
}
