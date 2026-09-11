package database_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const (
	themeReservedRootA = "theme-reservation-root-a"
	themeReservedRootB = "theme-reservation-root-b"
)

type themeReservedPathFixture struct {
	id, root, path, kind, parent string
	folder, reserved             bool
}

type themeReservedMarker struct {
	root, path string
	directory  bool
}

func themeReservedCreateRoots(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES($1,'device-migration-library','/synthetic/theme-reservations-a','/synthetic','theme-reservations-a'),
		($2,'device-migration-library','/synthetic/theme-reservations-b','/synthetic','theme-reservations-b')`,
		themeReservedRootA, themeReservedRootB); err != nil {
		t.Fatalf("create independent reservation roots: %v", err)
	}
}

func themeReservedInsertItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixtures []themeReservedPathFixture) {
	t.Helper()
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,
			path,relative_path,created_at,updated_at)
			VALUES($1,'device-migration-library',NULLIF($2,''),NULLIF($3,''),$1,lower($1),$4,$5,
			'/synthetic/theme-reservations/' || $6,$6,'2025-09-01T00:00:00Z','2025-09-02T00:00:00Z')`,
			fixture.id, fixture.root, fixture.parent, fixture.kind, fixture.folder, fixture.path); err != nil {
			t.Fatalf("seed retained reservation item %s: %v", fixture.id, err)
		}
	}
}

func themeReservedMarkers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[themeReservedMarker]bool {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT root_id,relative_path,is_directory FROM theme_reserved_paths")
	if err != nil {
		t.Fatal("read exact theme reservation inventory")
	}
	defer rows.Close()
	result := make(map[themeReservedMarker]bool)
	for rows.Next() {
		var marker themeReservedMarker
		if err := rows.Scan(&marker.root, &marker.path, &marker.directory); err != nil {
			t.Fatal("read one exact theme reservation")
		}
		if result[marker] {
			t.Fatal("a theme reservation was duplicated")
		}
		result[marker] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal("finish theme reservation inventory")
	}
	return result
}

func themeReservedTableIdentities(t *testing.T, ctx context.Context, pool *pgxpool.Pool, names []string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(c.relname,c.oid::bigint,c.relowner::bigint)
		ORDER BY c.relname)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relkind='r' AND c.relname=ANY($1::text[])`, names).Scan(&snapshot); err != nil {
		t.Fatal("capture historical table identities before reservation migration")
	}
	return snapshot
}

func themeReservedAssertVisibility(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixtures []themeReservedPathFixture) {
	t.Helper()
	statement := "SELECT " + database.ThemeReservedItemSQL("item") + "," + database.ThemeOrdinaryItemSQL("item") + "," +
		database.ThemeDirectItemSQL("item") + " FROM items item WHERE item.id=$1"
	for _, fixture := range fixtures {
		var reserved, ordinary, direct bool
		if err := pool.QueryRow(ctx, statement, fixture.id).Scan(&reserved, &ordinary, &direct); err != nil {
			t.Fatalf("read reservation visibility for %s: %v", fixture.id, err)
		}
		if reserved != fixture.reserved || ordinary == fixture.reserved || direct == fixture.reserved {
			t.Errorf("reservation visibility for %s: reserved=%v ordinary=%v direct=%v, want reserved=%v with no resource association",
				fixture.id, reserved, ordinary, direct, fixture.reserved)
		}
	}
}

func TestThemeReservedPathsMigrationPreservesSchema25AndReservesOnlyCanonicalLayouts(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	themeReservedCreateRoots(t, ctx, pool)
	const owner = "theme-reservation-old-owner"
	fixtures := []themeReservedPathFixture{
		{owner, themeReservedRootA, "Catalog", "MusicAlbum", "", true, false},
		{"reserved-folder", themeReservedRootA, "Albums/Theme-Music", "MusicAlbum", owner, true, true},
		{"reserved-folder-song", themeReservedRootA, "Albums/Theme-Music/song.flac", "Audio", "reserved-folder", false, true},
		{"reserved-folder-deep", themeReservedRootA, "Albums/Theme-Music/deep/subtitle.srt", "Video", "reserved-folder", false, true},
		{"reserved-derived-backdrop", themeReservedRootA, "Movies/BACKDROPS/trailer.mp4", "Video", owner, false, true},
		{"reserved-nested-directories", themeReservedRootA, "Nested/theme-music/backdrops/clip.mkv", "Video", owner, false, true},
		{"reserved-literal-metacharacters", themeReservedRootA, "Literal%_Box/ThEmE-MuSiC/song.mp3", "Audio", owner, false, true},
		{"reserved-root-theme-file", themeReservedRootA, "THEME.FLAC", "Audio", owner, false, true},
		{"ordinary-second-root", themeReservedRootB, "ordinary.mp3", "Audio", owner, false, false},
		{"ordinary-theme-file-directory", themeReservedRootA, "FolderNamed/theme.mp3", "MusicAlbum", owner, true, false},
		{"ordinary-theme-file-directory-child", themeReservedRootA, "FolderNamed/theme.mp3/track.mp3", "Audio", "ordinary-theme-file-directory", false, false},
		{"ordinary-directory-name-file", themeReservedRootA, "theme-music", "Audio", owner, false, false},
		{"ordinary-backdrops-name-file", themeReservedRootA, "backdrops", "Video", owner, false, false},
		{"ordinary-unicode-long-s", themeReservedRootA, "Unicode/theme-mu\u017fic/song.mp3", "Audio", owner, false, false},
		{"ordinary-unicode-kelvin", themeReservedRootA, "Unicode/bac\u212adrops/clip.mp4", "Video", owner, false, false},
		{"ordinary-video-theme", themeReservedRootA, "theme.mp4", "Video", owner, false, false},
		{"ordinary-other-video-theme", themeReservedRootA, "THEME.MKV", "Video", owner, false, false},
		{"ordinary-theme-prefix", themeReservedRootA, "not-theme.mp3", "Audio", owner, false, false},
		{"ordinary-theme-suffix", themeReservedRootA, "theme.mp3.extra", "Audio", owner, false, false},
		{"ordinary-theme-space", themeReservedRootA, "theme.mp3 ", "Audio", owner, false, false},
		{"ordinary-no-root", "", "Unrooted/theme-music/theme.mp3", "Audio", owner, false, false},
	}
	wantMarkers := map[themeReservedMarker]bool{
		{themeReservedRootA, "Albums/Theme-Music", true}:           true,
		{themeReservedRootA, "Movies/BACKDROPS", true}:             true,
		{themeReservedRootA, "Nested/theme-music", true}:           true,
		{themeReservedRootA, "Nested/theme-music/backdrops", true}: true,
		{themeReservedRootA, "Literal%_Box/ThEmE-MuSiC", true}:     true,
		{themeReservedRootA, "THEME.FLAC", false}:                  true,
	}
	// These are the scanner's thirteen accepted audio extensions. Standalone
	// theme video filenames above must not be included in this file convention.
	for _, extension := range []string{"mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma", "aiff", "aif", "alac", "ape", "mka"} {
		path := "Extensions/TheMe." + strings.ToUpper(extension)
		fixtures = append(fixtures, themeReservedPathFixture{"reserved-audio-" + extension, themeReservedRootA, path, "Audio", owner, false, true})
		wantMarkers[themeReservedMarker{themeReservedRootA, path, false}] = true
	}
	for index, path := range []string{"", "/Invalid/theme-music/song.mp3", "C:/Invalid/backdrops/clip.mp4", "C:theme.mp3",
		"Invalid\\theme-music/song.mp3", "InvalidDot/./theme-music/song.mp3", "InvalidParent/../backdrops/clip.mp4",
		"InvalidEmpty//theme-music/song.mp3", "InvalidTrailing/theme-music/", "./theme.mp3", "../theme.mp3", "//theme.mp3"} {
		fixtures = append(fixtures, themeReservedPathFixture{fmt.Sprintf("ordinary-invalid-path-%d", index), themeReservedRootA, path, "Audio", owner, false, false})
	}
	themeReservedInsertItems(t, ctx, pool, fixtures)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if len(legacy) != 30 {
		t.Fatalf("reservation baseline contains %d tables, want the published schema 25 inventory of 30", len(legacy))
	}
	legacyNames := make([]string, len(legacy))
	for index := range legacy {
		legacyNames[index] = legacy[index].name
		legacy[index].snapshot = themeOwnersTableSnapshot(t, ctx, pool, legacy[index])
	}
	oldIdentities := themeReservedTableIdentities(t, ctx, pool, legacyNames)
	oldSequences := themeOwnersSequences(t, ctx, pool)
	var ownerSnapshot, firstHistory string
	for attempt := 0; attempt < 2; attempt++ {
		themeOwnersMigrateTo(t, ctx, pool, 26)
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 26 {
			t.Fatalf("reservation migration version=%d, want 26: %v", version, err)
		}
		if actual := themeReservedMarkers(t, ctx, pool); !reflect.DeepEqual(actual, wantMarkers) {
			t.Fatal("migration reserved an unsupported layout or omitted an exact canonical directory/file marker")
		}
		for _, table := range legacy {
			if themeOwnersTableSnapshot(t, ctx, pool, table) != table.snapshot {
				t.Errorf("reservation migration changed an original row, item ID, parent, type, or metadata field in %s", table.name)
			}
		}
		if themeReservedTableIdentities(t, ctx, pool, legacyNames) != oldIdentities || !reflect.DeepEqual(themeOwnersSequences(t, ctx, pool), oldSequences) {
			t.Fatal("reservation migration replaced an old table or changed an original sequence")
		}
		var resources int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_theme_resources").Scan(&resources); err != nil || resources != 0 {
			t.Fatalf("migration inferred attachments without an accepted scan: resources=%d error=%v", resources, err)
		}
		themeOwnersAssertComplete(t, ctx, pool)
		themeReservedAssertVisibility(t, ctx, pool, fixtures)
		owners, history := themeOwnersSnapshot(t, ctx, pool), migrationHistory(t, ctx, pool)
		if attempt == 0 {
			ownerSnapshot, firstHistory = owners, history
		} else if owners != ownerSnapshot || history != firstHistory {
			t.Fatal("repeated reservation migration changed owner identities or applied history")
		}
	}
	// Add these after backfill so the predicate must rely on existing markers.
	// Similar text and another root must not invent additional reservations.
	late := []themeReservedPathFixture{
		{"late-exact-directory", themeReservedRootA, "Literal%_Box/ThEmE-MuSiC", "MusicAlbum", owner, true, true},
		{"late-exact-descendant", themeReservedRootA, "Literal%_Box/ThEmE-MuSiC/deep/new.mp3", "Audio", owner, false, true},
		{"late-wildcard-lookalike", themeReservedRootA, "LiteralABCBox/ThEmE-MuSiC/deep/new.mp3", "Audio", owner, false, false},
		{"late-prefix-lookalike", themeReservedRootA, "Literal%_Box/ThEmE-MuSiC-extra/new.mp3", "Audio", owner, false, false},
		{"late-case-lookalike", themeReservedRootA, "Literal%_Box/theme-music/new.mp3", "Audio", owner, false, false},
		{"late-other-root-directory", themeReservedRootB, "Literal%_Box/ThEmE-MuSiC/song.mp3", "Audio", owner, false, false},
		{"late-file-descendant", themeReservedRootA, "Extensions/TheMe.MP3/child.mp3", "Audio", owner, false, false},
		{"late-file-case-lookalike", themeReservedRootA, "Extensions/theme.mp3", "Audio", owner, false, false},
		{"late-other-root-file", themeReservedRootB, "Extensions/TheMe.MP3", "Audio", owner, false, false},
	}
	themeReservedInsertItems(t, ctx, pool, late)
	themeReservedAssertVisibility(t, ctx, pool, late)
	if !reflect.DeepEqual(themeReservedMarkers(t, ctx, pool), wantMarkers) || !reflect.DeepEqual(themeOwnersSequences(t, ctx, pool), oldSequences) {
		t.Fatal("ordinary inserts or visibility reads changed reservations or another namespace's sequence")
	}
}

func TestThemeReservedPathConstraintsAndRootDeletionStayScoped(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	themeReservedCreateRoots(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 26)
	const path = "Literal%_/backdrops"
	if _, err := pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		VALUES($1,$3,true),($2,$3,true)`, themeReservedRootA, themeReservedRootB, path); err != nil {
		t.Fatalf("seed the same exact marker in two independent roots: %v", err)
	}
	before := themeReservedMarkers(t, ctx, pool)
	for _, test := range []struct {
		name, path string
	}{
		{"empty", ""}, {"absolute", "/theme.mp3"}, {"drive", "C:/theme.mp3"}, {"drive relative", "c:theme.mp3"},
		{"backslash", "folder\\theme.mp3"}, {"dot component", "folder/./theme.mp3"},
		{"parent component", "folder/../theme.mp3"}, {"empty component", "folder//theme.mp3"}, {"trailing separator", "folder/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES($1,$2,false)", themeReservedRootA, test.path)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != "23514" {
				t.Fatalf("noncanonical reservation error=%v, want SQLSTATE 23514", err)
			}
		})
	}
	for _, test := range []struct {
		name, statement, code string
	}{
		{"unknown root", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('missing-theme-root','theme.mp3',false)", "23503"},
		{"duplicate path with another kind", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-reservation-root-a','Literal%_/backdrops',false)", "23505"},
		{"null root", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES(NULL,'theme.mp3',false)", "23502"},
		{"null path", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-reservation-root-a',NULL,false)", "23502"},
		{"null kind", "INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('theme-reservation-root-a','theme.mp3',NULL)", "23502"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, test.statement)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != test.code {
				t.Fatalf("reservation storage constraint error=%v, want SQLSTATE %s", err, test.code)
			}
		})
	}
	if !reflect.DeepEqual(themeReservedMarkers(t, ctx, pool), before) {
		t.Fatal("rejected marker writes changed an existing root reservation")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM library_roots WHERE id=$1", themeReservedRootA); err != nil {
		t.Fatalf("delete one exclusively owned marker root: %v", err)
	}
	want := map[themeReservedMarker]bool{{themeReservedRootB, path, true}: true}
	if !reflect.DeepEqual(themeReservedMarkers(t, ctx, pool), want) {
		t.Fatal("root deletion retained orphan markers or removed another root's identical path")
	}
	themeOwnersAssertComplete(t, ctx, pool)
}
