package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func linuxAuxiliaryPathHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM items r),
		'owners',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM theme_owner_ids r),
		'themes',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_theme_resources r),
		'extras',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_extra_resources r),
		'theme_paths',(SELECT jsonb_agg(to_jsonb(r) ORDER BY root_id,relative_path) FROM theme_reserved_paths r),
		'extra_paths',(SELECT jsonb_agg(to_jsonb(r) ORDER BY root_id,relative_path) FROM extra_reserved_paths r),
		'migrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY version) FROM schema_migrations r WHERE version<=65))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func rejectLinuxAuxiliaryReservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, relative string) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO `+table+`(root_id,relative_path,is_directory) VALUES($1,$2,true)`, extraRootA, relative)
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) || postgres.Code != "23514" {
		t.Fatalf("reservation %s accepted invalid path %q or returned an unrelated error: %v", table, relative, err)
	}
}

func TestLinuxAuxiliaryPathsMigrationPreservesSchema65(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			extraVersion26Baseline(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 65)
			extraInsertItem(t, ctx, pool, "colon-owner", "C:Movie/Main.mp4", "Movie", "", false)
			extraInsertItem(t, ctx, pool, "colon-theme", "C:Movie/theme.mp3", "Audio", "colon-owner", false)
			extraInsertItem(t, ctx, pool, "colon-extra", "C:Movie/featurettes/clip.mp4", "Video", "colon-owner", false)
			for _, table := range []string{"theme_reserved_paths", "extra_reserved_paths"} {
				rejectLinuxAuxiliaryReservation(t, ctx, pool, table, "C:Movie/featurettes")
			}
			before := linuxAuxiliaryPathHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 66)
			}
			if after := linuxAuxiliaryPathHistory(t, ctx, pool); after != before {
				t.Fatal("Linux path migration rewrote retained rows, owner identities, or published history")
			}
			for _, table := range []string{"theme_reserved_paths", "extra_reserved_paths"} {
				for _, relative := range []string{"", "/C:Movie/theme.mp3", "C:Movie//theme.mp3", "C:Movie/./theme.mp3", "C:Movie/../theme.mp3", `C:Movie\theme.mp3`} {
					rejectLinuxAuxiliaryReservation(t, ctx, pool, table, relative)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO `+table+`(root_id,relative_path,is_directory) VALUES($1,'C:/literal',true)`, extraRootA); err != nil {
					t.Fatalf("literal Linux colon directory was rejected: %v", err)
				}
			}
			if _, err := pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('extra-migration-root-a','C:Movie/theme.mp3',false);
				INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('extra-migration-root-a','C:Movie/featurettes',true);
				INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES('colon-theme','colon-owner','song',true);
				INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES('colon-extra','colon-owner','clip',true)`); err != nil {
				t.Fatal("publish literal colon auxiliary state", err)
			}
			var direct bool
			if err := pool.QueryRow(ctx, `SELECT `+database.CatalogDirectItemSQL("i")+` FROM items i WHERE id='colon-extra'`).Scan(&direct); err != nil || !direct {
				t.Fatalf("current direct reads rejected a valid colon extra: %v", err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err := database.ValidateThemeState(ctx, tx, 66); err != nil {
				t.Fatal("validate current colon theme state", err)
			}
			if err := database.ValidateExtraState(ctx, tx, 66); err != nil {
				t.Fatal("validate current colon extra state", err)
			}
			if err := database.ValidateExtraState(ctx, tx, 65); !errors.Is(err, database.ErrExtraState) {
				t.Fatalf("historical schema semantics were relaxed: %v", err)
			}
		})
	}
}

func TestLinuxAuxiliaryPathsRecoveryMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	extraVersion26Baseline(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 65)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 66); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 65 {
		t.Fatalf("rolled-back path migration version = %d, want 65: %v", version, err)
	}
	for _, table := range []string{"theme_reserved_paths", "extra_reserved_paths"} {
		rejectLinuxAuxiliaryReservation(t, ctx, pool, table, "C:Movie/featurettes")
	}
}
