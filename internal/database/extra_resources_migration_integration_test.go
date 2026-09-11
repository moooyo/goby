package database_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const extraLibrary = "extra-migration-movies"
const extraRootA = "extra-migration-root-a"
const extraRootB = "extra-migration-root-b"

func extraVersion26Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	themeOwnersMigrateTo(t, ctx, pool, 26)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('extra-migration-movies','Movie extras','movies'),
		('extra-migration-music','Music controls','music'),
		('extra-migration-mixed','Mixed controls','mixed'),
		('extra-migration-tv','TV controls','tvshows');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('extra-migration-root-a','extra-migration-movies','/synthetic/extras/a','/synthetic','extras/a'),
		('extra-migration-root-b','extra-migration-movies','/synthetic/extras/b','/synthetic','extras/b'),
		('extra-migration-music-root','extra-migration-music','/synthetic/extras/music','/synthetic','extras/music'),
		('extra-migration-mixed-root','extra-migration-mixed','/synthetic/extras/mixed','/synthetic','extras/mixed'),
		('extra-migration-tv-root','extra-migration-tv','/synthetic/extras/tv','/synthetic','extras/tv')`); err != nil {
		t.Fatalf("seed historical extra libraries and roots: %v", err)
	}
}

func extraInsertItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, relative, kind, parent string, folder bool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,
		created_at,updated_at) VALUES($1,$2,$3,NULLIF($4,''),$1,lower($1),$5,$6,'/synthetic/extras/a/'||$7,$7,
		'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`, id, extraLibrary, extraRootA, parent, kind, folder, relative); err != nil {
		t.Fatalf("insert retained extra fixture %s: %v", id, err)
	}
}

func extraSeedThemeTransition(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	extraInsertItem(t, ctx, pool, "extra-owner", "Film/Film.mkv", "Movie", "", false)
	extraInsertItem(t, ctx, pool, "extra-hidden-owner", "Film/featurettes/Old movie.mkv", "Movie", "", false)
	extraInsertItem(t, ctx, pool, "extra-affected-theme", "Film/featurettes/theme.mp3", "Audio", "extra-hidden-owner", false)
	extraInsertItem(t, ctx, pool, "extra-retained-theme", "Film/theme.mp3", "Audio", "extra-owner", false)
	extraInsertItem(t, ctx, pool, "extra-inactive-theme", "Film/featurettes/retired.mp3", "Audio", "extra-hidden-owner", false)
	if _, err := pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES
		('extra-migration-root-a','Film/featurettes/theme.mp3',false),
		('extra-migration-root-a','Film/theme.mp3',false),('extra-migration-root-a','Film/featurettes/retired.mp3',false);
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('extra-affected-theme','extra-hidden-owner','song',true),
		('extra-retained-theme','extra-owner','song',true),
		('extra-inactive-theme','extra-hidden-owner','song',false)`); err != nil {
		t.Fatalf("seed active and inactive schema-26 theme links: %v", err)
	}
}

func extraSnapshot(t *testing.T, ctx context.Context, query themeOwnerQuery, table deviceLegacyTable, expectedTransition bool) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ",") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version<=26"
	}
	projection := "to_jsonb(r)"
	if expectedTransition && table.name == "item_theme_resources" {
		projection = `CASE WHEN r.resource_item_id='extra-affected-theme' THEN jsonb_set(to_jsonb(r),'{active}','false'::jsonb) ELSE to_jsonb(r) END`
	}
	var snapshot string
	if err := query.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(v ORDER BY v::text),'[]'::jsonb)::text FROM
		(SELECT `+projection+` AS v FROM (`+statement+`) r) values_to_compare`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot complete extra migration table %s: %v", table.name, err)
	}
	return snapshot
}

func extraMarkers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[themeReservedMarker]bool {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT root_id,relative_path,is_directory FROM extra_reserved_paths")
	if err != nil {
		t.Fatalf("read published extra reservations: %v", err)
	}
	defer rows.Close()
	result := map[themeReservedMarker]bool{}
	for rows.Next() {
		var marker themeReservedMarker
		if err := rows.Scan(&marker.root, &marker.path, &marker.directory); err != nil {
			t.Fatal("read one extra reservation")
		}
		result[marker] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal("finish extra reservation inventory")
	}
	return result
}

func extraValidate(t *testing.T, ctx context.Context, tx pgx.Tx, version int64) {
	t.Helper()
	if err := database.ValidateThemeState(ctx, tx, version); err != nil {
		t.Fatalf("validate theme state at schema %d: %v", version, err)
	}
	if err := database.ValidateExtraState(ctx, tx, version); err != nil {
		t.Fatalf("validate extra state at schema %d: %v", version, err)
	}
}

func extraThemeSequenceSnapshot(t *testing.T, ctx context.Context, query themeOwnerQuery) string {
	t.Helper()
	var snapshot string
	// A sequence does not expose a table's named composite row type. Read its
	// scalar state explicitly, retaining the identity and definition as well.
	if err := query.QueryRow(ctx, `SELECT jsonb_build_object(
		'oid','theme_owner_ids_id_seq'::regclass::oid::bigint,
		'definition',(SELECT to_jsonb(definition) FROM pg_sequence definition
			WHERE definition.seqrelid='theme_owner_ids_id_seq'::regclass),
		'value',jsonb_build_object('last_value',state.last_value,'log_cnt',state.log_cnt,'is_called',state.is_called))::text
		FROM theme_owner_ids_id_seq state`).Scan(&snapshot); err != nil {
		t.Fatalf("capture the existing theme identity sequence: %v", err)
	}
	return snapshot
}

func TestExtraMigrationPreservesSchema26AndOnlyRetiresAffectedThemes(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			extraVersion26Baseline(t, ctx, pool)
			extraSeedThemeTransition(t, ctx, pool)
			if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
				VALUES('extra-preserved-user','Extra preserved viewer','extra preserved viewer','synthetic-unused-password-hash');
				INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played)
				VALUES('extra-preserved-user','extra-affected-theme',123456789,3,true,false),
				('extra-preserved-user','extra-retained-theme',0,4,false,true);
				UPDATE item_metadata_state SET overrides='{"Name":"Retained user title"}',
				locked_values='{"Overview":"Retained locked description"}',revision=9,
				last_edited_by='extra-preserved-user',last_edited_at='2026-02-01T00:00:00Z'
				WHERE item_id='extra-affected-theme';
				SELECT sync_catalog_item_entities('extra-affected-theme','{"Genres":["Historical genre"]}'::jsonb)`); err != nil {
				t.Fatalf("seed retained user state, native metadata and entity identity: %v", err)
			}
			for index, fixture := range []struct {
				path   string
				folder bool
			}{
				{"OnlyFolder/FEATURETTES", true},
				{"Nested/deleted scenes/nested/notes.txt", false},
				{"Literal%_/Trailers/trailer.mp4", false},
				{"Outer/featurettes/backdrops/clip.mp4", false},
				{"OuterTheme/theme-music/featurettes/clip.mp4", false},
				{"OuterBackdrops/backdrops/trailers/clip.mp4", false},
				{"featurettes", false},
				{"Unicode/featurette\u017f/clip.mp4", false},
				{"Lookalike/featurettes-more/clip.mp4", false},
				{"FileParent/theme.mp3/trailers/clip.mp4", false},
				{"Unproved/extras/clip.mp4", false},
				{"Invalid//featurettes/clip.mp4", false},
				{"Invalid/./trailers/clip.mp4", false},
				{"Invalid/../deleted scenes/clip.mp4", false},
				{"C:/featurettes/clip.mp4", false},
				{"Invalid\\featurettes/clip.mp4", false},
			} {
				extraInsertItem(t, ctx, pool, fmt.Sprintf("extra-path-%d", index), fixture.path, "Video", "", fixture.folder)
			}
			for _, kind := range []string{"music", "mixed", "tv"} {
				if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,name,sort_name,type,relative_path)
					VALUES($1,$2,$3,$1,$1,'Movie','Control/featurettes/clip.mp4')`, "extra-control-"+kind,
					"extra-migration-"+kind, "extra-migration-"+kind+"-root"); err != nil {
					t.Fatalf("seed collection-type control: %v", err)
				}
			}
			extraInsertItem(t, ctx, pool, "extra-unrooted", "Unrooted/featurettes/clip.mp4", "Video", "", false)
			extraInsertItem(t, ctx, pool, "extra-mismatched-root", "Mismatched/featurettes/clip.mp4", "Video", "", false)
			if _, err := pool.Exec(ctx, `UPDATE items SET root_id=NULL WHERE id='extra-unrooted';
				UPDATE items SET library_id='extra-migration-music' WHERE id='extra-mismatched-root'`); err != nil {
				t.Fatal("seed unrooted and mismatched-root reservation controls")
			}
			legacy := captureDeviceLegacyTables(t, ctx, pool)
			if len(legacy) != 33 {
				t.Fatalf("historical extra baseline has %d tables, want 33", len(legacy))
			}
			original, expected := map[string]string{}, map[string]string{}
			names := make([]string, len(legacy))
			for index, table := range legacy {
				names[index] = table.name
				original[table.name] = extraSnapshot(t, ctx, pool, table, false)
				expected[table.name] = extraSnapshot(t, ctx, pool, table, true)
			}
			identities, sequences := themeReservedTableIdentities(t, ctx, pool, names), themeOwnersSequences(t, ctx, pool)
			themeSequence := extraThemeSequenceSnapshot(t, ctx, pool)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin extra migration rollback witness")
			}
			defer themeOwnersRollback(tx)
			// These calls execute while schema27 tables do not exist.
			extraValidate(t, ctx, tx, 26)
			if err := database.RecoveryMigrateTo(ctx, tx, 27); err != nil {
				t.Fatalf("migrate schema26 inside rollback witness: %v", err)
			}
			extraValidate(t, ctx, tx, 27)
			for _, table := range legacy {
				if extraSnapshot(t, ctx, tx, table, false) != expected[table.name] {
					t.Errorf("uncommitted migration changed unexpected historical state in %s", table.name)
				}
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal("roll back extra reservations and selected theme deactivation")
			}
			var absent bool
			if err := pool.QueryRow(ctx, "SELECT to_regclass('extra_reserved_paths') IS NULL AND to_regclass('item_extra_resources') IS NULL").Scan(&absent); err != nil || !absent {
				t.Fatal("extra migration rollback retained new storage")
			}
			for _, table := range legacy {
				if extraSnapshot(t, ctx, pool, table, false) != original[table.name] {
					t.Errorf("rolled-back extra migration changed historical state in %s", table.name)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatalf("complete normal extra migration: %v", err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 27)
				}
				for _, table := range legacy {
					if extraSnapshot(t, ctx, pool, table, false) != expected[table.name] {
						t.Errorf("extra migration changed an unexpected historical row or value in %s", table.name)
					}
				}
				if extraThemeSequenceSnapshot(t, ctx, pool) != themeSequence {
					t.Fatal("extra migration advanced or replaced the theme identity sequence")
				}
				if themeReservedTableIdentities(t, ctx, pool, names) != identities || !reflect.DeepEqual(themeOwnersSequences(t, ctx, pool), sequences) {
					t.Fatal("extra migration changed a historical table identity or sequence")
				}
			}
			want := map[themeReservedMarker]bool{
				{extraRootA, "Film/featurettes", true}:              true,
				{extraRootA, "OnlyFolder/FEATURETTES", true}:        true,
				{extraRootA, "Nested/deleted scenes", true}:         true,
				{extraRootA, "Literal%_/Trailers", true}:            true,
				{extraRootA, "Outer/featurettes", true}:             true,
				{extraRootA, "FileParent/theme.mp3/trailers", true}: true,
			}
			if !reflect.DeepEqual(extraMarkers(t, ctx, pool), want) {
				t.Fatal("extra migration omitted a proved outer boundary or reserved an unsupported layout")
			}
			for _, test := range []struct {
				id               string
				ordinary, direct bool
			}{
				{"extra-owner", true, true},
				{"extra-hidden-owner", false, false},
				{"extra-affected-theme", false, false},
				{"extra-retained-theme", false, true},
				{"extra-inactive-theme", false, false},
			} {
				var ordinary, direct bool
				statement := "SELECT " + database.CatalogOrdinaryItemSQL("item") + "," + database.CatalogDirectItemSQL("item") +
					" FROM items item WHERE item.id=$1"
				if err := pool.QueryRow(ctx, statement, test.id).Scan(&ordinary, &direct); err != nil || ordinary != test.ordinary || direct != test.direct {
					t.Errorf("transition visibility for %s: ordinary=%v direct=%v error=%v", test.id, ordinary, direct, err)
				}
			}
			var historicalOrdinary bool
			if err := pool.QueryRow(ctx, "SELECT "+database.ThemeOrdinaryItemSQL("item")+" FROM items item WHERE id='extra-hidden-owner'").Scan(&historicalOrdinary); err != nil || !historicalOrdinary {
				t.Fatal("the historical schema26 predicate acquired current-schema reservations")
			}
			var tables, resources, version int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()),
				(SELECT count(*) FROM item_extra_resources),(SELECT max(version) FROM schema_migrations)`).Scan(&tables, &resources, &version); err != nil || tables != 35 || resources != 0 || version != 27 {
				t.Fatalf("extra migration inventory: tables=%d resources=%d version=%d error=%v", tables, resources, version, err)
			}
		})
	}
}

func extraSeedCurrentResources(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	extraVersion26Baseline(t, ctx, pool)
	extraInsertItem(t, ctx, pool, "extra-owner", "Film/Film.mkv", "Movie", "", false)
	extraInsertItem(t, ctx, pool, "extra-clip", "Film/featurettes/Clip.mp4", "Video", "extra-owner", false)
	extraInsertItem(t, ctx, pool, "extra-deleted", "Film/deleted scenes/Deleted.mp4", "Video", "extra-owner", false)
	extraInsertItem(t, ctx, pool, "extra-trailer", "Film/trailers/Trailer.mp4", "Video", "extra-owner", false)
	extraInsertItem(t, ctx, pool, "extra-retired", "Film/featurettes/Retired.mp4", "Video", "extra-owner", false)
	themeOwnersMigrateTo(t, ctx, pool, 27)
	if _, err := pool.Exec(ctx, `INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('extra-clip','extra-owner','clip',true),('extra-deleted','extra-owner','deleted_scene',true),
		('extra-trailer','extra-owner','trailer',true),('extra-retired','extra-owner','clip',false)`); err != nil {
		t.Fatalf("seed active and inactive current extras: %v", err)
	}
}

func TestExtraVisibilityAndSemanticStateRejectCrossRowCorruption(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	extraSeedCurrentResources(t, ctx, pool)
	statement := "SELECT " + database.CatalogOrdinaryItemSQL("item") + "," + database.CatalogDirectItemSQL("item") +
		"," + database.ExtraResourceItemSQL("item", false) + " FROM items item WHERE item.id=$1"
	for _, test := range []struct {
		id                         string
		ordinary, direct, resource bool
	}{
		{"extra-owner", true, true, false},
		{"extra-clip", false, true, true},
		{"extra-deleted", false, true, true},
		{"extra-trailer", false, true, true},
		{"extra-retired", false, false, true},
	} {
		var ordinary, direct, resource bool
		if err := pool.QueryRow(ctx, statement, test.id).Scan(&ordinary, &direct, &resource); err != nil ||
			ordinary != test.ordinary || direct != test.direct || resource != test.resource {
			t.Errorf("extra visibility for %s: ordinary=%v direct=%v resource=%v error=%v", test.id, ordinary, direct, resource, err)
		}
	}
	for _, test := range []struct{ name, mutation string }{
		{"missing parent", "UPDATE items SET parent_id=NULL WHERE id='extra-clip'"},
		{"wrong type", "UPDATE items SET type='Movie' WHERE id='extra-clip'"},
		{"folder resource", "UPDATE items SET is_folder=true WHERE id='extra-clip'"},
		{"nonmovie owner", "UPDATE items SET type='Episode' WHERE id='extra-owner'"},
		{"folder owner", "UPDATE items SET is_folder=true WHERE id='extra-owner'"},
		{"moved active owner", "UPDATE items SET root_id='extra-migration-root-b' WHERE id='extra-owner'"},
		{"different owner library", "UPDATE items SET library_id='extra-migration-music' WHERE id='extra-owner'"},
		{"different resource root library", "UPDATE items SET root_id='extra-migration-music-root' WHERE id='extra-clip'"},
		{"missing reservation", "DELETE FROM extra_reserved_paths WHERE relative_path='Film/featurettes'"},
		{"reserved owner", "INSERT INTO extra_reserved_paths VALUES('extra-migration-root-a','Film/Film.mkv',false)"},
		{"noncanonical resource", "UPDATE items SET relative_path='Film/featurettes/../Clip.mp4' WHERE id='extra-clip'"},
		{"inactive missing parent", "UPDATE items SET parent_id=NULL WHERE id='extra-retired'"},
		{"inactive wrong type", "UPDATE items SET type='Audio' WHERE id='extra-retired'"},
		{"inactive moved library", "UPDATE items SET library_id='extra-migration-music' WHERE id='extra-retired'"},
		{"active cross role", `INSERT INTO theme_reserved_paths VALUES('extra-migration-root-a','Film/featurettes/Clip.mp4',false);
			INSERT INTO item_theme_resources VALUES('extra-clip','extra-owner','video',true)`},
		{"inactive cross role", `INSERT INTO theme_reserved_paths VALUES('extra-migration-root-a','Film/featurettes/Retired.mp4',false);
			INSERT INTO item_theme_resources VALUES('extra-retired','extra-owner','video',false)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated semantic corruption")
			}
			defer themeOwnersRollback(tx)
			extraValidate(t, ctx, tx, 27)
			if _, err := tx.Exec(ctx, test.mutation); err != nil {
				t.Fatalf("construct referentially valid semantic corruption: %v", err)
			}
			if err := database.ValidateExtraState(ctx, tx, 27); !errors.Is(err, database.ErrExtraState) {
				t.Fatalf("extra corruption validation error=%v, want ErrExtraState", err)
			}
			if strings.Contains(test.name, "cross role") {
				if err := database.ValidateThemeState(ctx, tx, 27); !errors.Is(err, database.ErrThemeState) {
					t.Fatalf("theme cross-role validation error=%v, want ErrThemeState", err)
				}
				var leaked bool
				if err := tx.QueryRow(ctx, "SELECT bool_or("+database.CatalogDirectItemSQL("item")+") FROM items item WHERE item.id IN ('extra-clip','extra-retired') AND EXISTS(SELECT 1 FROM item_theme_resources t WHERE t.resource_item_id=item.id)").Scan(&leaked); err != nil || leaked {
					t.Fatal("cross-role resource remained directly visible")
				}
			}
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin inactive owner history witness")
	}
	defer themeOwnersRollback(tx)
	if _, err := tx.Exec(ctx, `UPDATE item_extra_resources SET active=false;
		UPDATE items SET root_id='extra-migration-root-b',type='Folder',is_folder=true WHERE id='extra-owner';
		INSERT INTO extra_reserved_paths VALUES('extra-migration-root-b','Film',true)`); err != nil {
		t.Fatalf("retain history after the owner moves and becomes reserved: %v", err)
	}
	extraValidate(t, ctx, tx, 27)
}

func TestExtraMigrationRejectsOldThemeCorruptionBeforeRepair(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			extraVersion26Baseline(t, ctx, pool)
			extraSeedThemeTransition(t, ctx, pool)
			// Deactivation would conceal this old active-owner mismatch. The
			// schema26 check must reject it before creating the new reservations.
			if _, err := pool.Exec(ctx, "UPDATE items SET root_id=$1 WHERE id='extra-hidden-owner'", extraRootB); err != nil {
				t.Fatal("create the old active-owner root corruption")
			}
			before := captureDeviceLegacyTables(t, ctx, pool)
			var err error
			if runner == "normal" {
				err = database.Migrate(ctx, pool)
			} else {
				var tx pgx.Tx
				tx, err = pool.Begin(ctx)
				if err != nil {
					t.Fatal("begin rejected recovery migration")
				}
				err = database.RecoveryMigrateTo(ctx, tx, 27)
				themeOwnersRollback(tx)
			}
			if !errors.Is(err, database.ErrThemeState) {
				t.Fatalf("corrupt source migration error=%v, want ErrThemeState", err)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var absent bool
			if err := pool.QueryRow(ctx, `SELECT to_regclass('extra_reserved_paths') IS NULL AND to_regclass('item_extra_resources') IS NULL
				AND (SELECT max(version) FROM schema_migrations)=26`).Scan(&absent); err != nil || !absent {
				t.Fatal("rejected source migration retained new schema or history")
			}
		})
	}
}

func TestExtraStorageConstraintsAndHistoricalValidationBoundary(t *testing.T) {
	if err := database.ValidateExtraState(context.Background(), nil, 26); err != nil {
		t.Fatal("historical extra validation requires tables or a transaction")
	}
	if err := database.ValidateExtraState(context.Background(), nil, 27); err == nil {
		t.Fatal("current extra validation accepted a missing transaction")
	}
	ctx, pool := migrationTestPool(t)
	extraSeedCurrentResources(t, ctx, pool)
	for _, test := range []struct{ name, statement, code string }{
		{"unknown resource kind", "UPDATE item_extra_resources SET kind='interview' WHERE resource_item_id='extra-clip'", "23514"},
		{"self owner", "UPDATE item_extra_resources SET owner_item_id=resource_item_id WHERE resource_item_id='extra-clip'", "23514"},
		{"missing owner", "UPDATE item_extra_resources SET owner_item_id='absent-extra-owner' WHERE resource_item_id='extra-clip'", "23503"},
		{"unknown marker root", "INSERT INTO extra_reserved_paths VALUES('absent-extra-root','Film/featurettes',true)", "23503"},
		{"noncanonical marker", "INSERT INTO extra_reserved_paths VALUES('extra-migration-root-a','Film/../featurettes',true)", "23514"},
		{"marker kind collision", "INSERT INTO extra_reserved_paths VALUES('extra-migration-root-a','Film/featurettes',false)", "23505"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, test.statement)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != test.code {
				t.Fatalf("extra storage constraint error=%v, want SQLSTATE %s", err, test.code)
			}
		})
	}
}

func TestExtraReservationsUseExactRootAndPathBoundaries(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	extraVersion26Baseline(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 27)
	if _, err := pool.Exec(ctx, `INSERT INTO extra_reserved_paths VALUES
		('extra-migration-root-a','Literal%_Box/FEATURETTES',true),
		('extra-migration-root-a','File/trailers/Reserved.mp4',false)`); err != nil {
		t.Fatal("seed exact extra reservations")
	}
	for index, test := range []struct {
		path, root string
		reserved   bool
	}{
		{"Literal%_Box/FEATURETTES", extraRootA, true},
		{"Literal%_Box/FEATURETTES/nested/video.mp4", extraRootA, true},
		{"LiteralABCBox/FEATURETTES/video.mp4", extraRootA, false},
		{"Literal%_Box/featurettes/video.mp4", extraRootA, false},
		{"Literal%_Box/FEATURETTES-more/video.mp4", extraRootA, false},
		{"Literal%_Box/FEATURETTES/video.mp4", extraRootB, false},
		{"File/trailers/Reserved.mp4", extraRootA, true},
		{"File/trailers/reserved.mp4", extraRootA, false},
		{"File/trailers/Reserved.mp4/child.mp4", extraRootA, false},
	} {
		id := fmt.Sprintf("extra-exact-path-%d", index)
		extraInsertItem(t, ctx, pool, id, test.path, "Video", "", false)
		if test.root != extraRootA {
			if _, err := pool.Exec(ctx, "UPDATE items SET root_id=$1 WHERE id=$2", test.root, id); err != nil {
				t.Fatal("move the exact-path control to another registered root")
			}
		}
		var reserved, ordinary, direct bool
		if err := pool.QueryRow(ctx, "SELECT "+database.ExtraReservedItemSQL("item")+","+database.CatalogOrdinaryItemSQL("item")+
			","+database.CatalogDirectItemSQL("item")+" FROM items item WHERE item.id=$1", id).Scan(&reserved, &ordinary, &direct); err != nil ||
			reserved != test.reserved || ordinary == test.reserved || direct == test.reserved {
			t.Errorf("exact extra reservation for %s: reserved=%v ordinary=%v direct=%v error=%v", id, reserved, ordinary, direct, err)
		}
	}
}

func TestExtraMigrationValidatesAfterWaitingForSchema26Writer(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	extraVersion26Baseline(t, ctx, pool)
	extraSeedThemeTransition(t, ctx, pool)
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin source-schema writer")
	}
	defer themeOwnersRollback(writer)
	if _, err := writer.Exec(ctx, "UPDATE items SET root_id=$1 WHERE id='extra-hidden-owner'", extraRootB); err != nil {
		t.Fatal("construct a pending invalid active-owner update")
	}
	migrating, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin migration waiting for the source writer")
	}
	defer themeOwnersRollback(migrating)
	done := make(chan error, 1)
	pending := true
	go func() { done <- database.RecoveryMigrateTo(work, migrating, 27) }()
	defer func() {
		cancel()
		if pending {
			<-done
		}
	}()
	themeOwnersWaitBlocked(t, ctx, pool, migrating.Conn().PgConn().PID(), writer.Conn().PgConn().PID())
	if err := writer.Commit(ctx); err != nil {
		t.Fatal("publish the source update while migration waits for the write boundary")
	}
	err = <-done
	pending = false
	if !errors.Is(err, database.ErrThemeState) {
		t.Fatalf("migration did not validate the committed source update: %v", err)
	}
	if err := migrating.Rollback(ctx); err != nil {
		t.Fatal("roll back the rejected migration")
	}
	var absent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('extra_reserved_paths') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("rejected concurrent migration published reservations")
	}
}
