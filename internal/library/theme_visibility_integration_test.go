package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func seedThemeVisibilityFixture(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path) VALUES
		('theme-root-b2', 'library-b', '/media/b2', '/media', 'b2');
		UPDATE items SET root_id = 'root-b', path = '/media/b/Owner/film.mp4',
			relative_path = 'Owner/film.mp4' WHERE id = 'movie-b';
		UPDATE items SET root_id = 'root-a', path = '/media/a/Owner/film.mp4',
			relative_path = 'Owner/film.mp4' WHERE id = 'movie-a';
		INSERT INTO items (id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, relative_path) VALUES
		('theme-control-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Control', 'E Ordinary Control', 'Audio', false,
		 '/media/b/Owner/theme-music-extra/control.mp3', 'Owner/theme-music-extra/control.mp3'),
		('theme-prefix-control-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Prefix Control', 'F Ordinary Prefix', 'Audio', false,
		 '/media/b/Owner/theme.mp3.extra', 'Owner/theme.mp3.extra'),
		('theme-song-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Song', '0 Theme Song', 'Audio', false,
		 '/media/b/Owner/theme.mp3', 'Owner/theme.mp3'),
		('theme-video-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Video', '0 Theme Video', 'Video', false,
		 '/media/b/Owner/backdrops/clip.mp4', 'Owner/backdrops/clip.mp4'),
		('theme-library-song-b', 'library-b', 'root-b', 'library-b', 'Theme Visibility Library Song', '0 Theme Library Song', 'Audio', false,
		 '/media/b/theme-music/opening.flac', 'theme-music/opening.flac'),
		('theme-inactive-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Inactive', '0 Theme Inactive', 'Audio', false,
		 '/media/b/Owner/theme-music/inactive.mp3', 'Owner/theme-music/inactive.mp3'),
		('theme-associated-unreserved-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Unreserved Association', '0 Theme Association', 'Audio', false,
		 '/media/b/Owner/associated.mp3', 'Owner/associated.mp3'),
		('theme-inactive-unreserved-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Inactive Association', '0 Theme Inactive Association', 'Audio', false,
		 '/media/b/Owner/retained.mp3', 'Owner/retained.mp3'),
		('theme-legacy-file-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Legacy File', '0 Theme Legacy File', 'Audio', false,
		 '/media/b/Owner/legacy-theme.mp3', 'Owner/legacy-theme.mp3'),
		('theme-legacy-directory-b', 'library-b', 'root-b', 'movie-b', 'Theme Visibility Legacy Directory', '0 Theme Legacy Directory', 'Folder', true,
		 '/media/b/Owner/theme-music', 'Owner/theme-music'),
		('theme-legacy-nested-b', 'library-b', 'root-b', 'theme-legacy-directory-b', 'Theme Visibility Nested Directory', '0 Theme Nested Directory', 'Folder', true,
		 '/media/b/Owner/theme-music/nested', 'Owner/theme-music/nested'),
		('theme-legacy-descendant-b', 'library-b', 'root-b', 'theme-legacy-nested-b', 'Theme Visibility Legacy Descendant', '0 Theme Descendant', 'Audio', false,
		 '/media/b/Owner/theme-music/nested/legacy.mp3', 'Owner/theme-music/nested/legacy.mp3'),
		('theme-song-a', 'library-a', 'root-a', 'movie-a', 'Theme Visibility Inaccessible Song', '0 Theme Inaccessible Song', 'Audio', false,
		 '/media/a/Owner/theme.mp3', 'Owner/theme.mp3');
		INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory) VALUES
		('root-b', 'Owner/theme.mp3', false),
		('root-b', 'Owner/backdrops', true),
		('root-b', 'Owner/theme-music', true),
		('root-b', 'Owner/legacy-theme.mp3', false),
		('root-b', 'theme-music', true),
		('root-a', 'Owner/theme.mp3', false);
		INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active) VALUES
		('theme-song-b', 'movie-b', 'song', true),
		('theme-video-b', 'movie-b', 'video', true),
		('theme-library-song-b', 'library-b', 'song', true),
		('theme-inactive-b', 'movie-b', 'song', false),
		('theme-associated-unreserved-b', 'movie-b', 'song', true),
		('theme-inactive-unreserved-b', 'movie-b', 'song', false),
		('theme-song-a', 'movie-a', 'song', true)`); err != nil {
		t.Fatalf("seed permanent theme visibility facts: %v", err)
	}
}

func themeVisibilityHiddenIDs() []string {
	return []string{"theme-song-b", "theme-video-b", "theme-library-song-b", "theme-inactive-b",
		"theme-associated-unreserved-b", "theme-inactive-unreserved-b", "theme-legacy-file-b",
		"theme-legacy-directory-b", "theme-legacy-nested-b", "theme-legacy-descendant-b"}
}

func TestThemeVisibilityOrdinaryQueriesHideReservedAndAssociatedItemsBeforePaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	visible := []string{"episode-b1", "episode-b2", "movie-b", "audio-b", "theme-control-b", "theme-prefix-control-b", "season-b", "series-b"}
	for _, test := range []struct {
		name  string
		query Query
		ids   []string
		total int
	}{
		{"complete_visible_catalog", Query{Recursive: true}, visible, len(visible)},
		{"page_after_exclusion", Query{Recursive: true, StartIndex: 1, Limit: 2}, []string{"episode-b2", "movie-b"}, len(visible)},
		{"page_at_visible_end", Query{Recursive: true, StartIndex: len(visible), Limit: 1}, []string{}, len(visible)},
		{"search_does_not_reveal_theme_names", Query{Recursive: true, SearchTerm: "Theme Visibility"}, []string{"theme-control-b", "theme-prefix-control-b"}, 2},
		{"hidden_ids_are_not_an_escape_hatch", Query{Ids: themeVisibilityHiddenIDs()}, []string{}, 0},
		{"mixed_ids_keep_only_ordinary_items", Query{Ids: []string{"theme-song-b", "movie-b", "theme-legacy-file-b", "audio-b"}}, []string{"movie-b", "audio-b"}, 2},
		{"ordinary_parent_direct_children", Query{ParentID: "movie-b"}, []string{"theme-control-b", "theme-prefix-control-b"}, 2},
		{"ordinary_parent_recursive_children", Query{ParentID: "movie-b", Recursive: true}, []string{"theme-control-b", "theme-prefix-control-b"}, 2},
		{"ordinary_parent_search_and_page", Query{ParentID: "movie-b", Recursive: true, SearchTerm: "Theme Visibility", StartIndex: 1, Limit: 1}, []string{"theme-prefix-control-b"}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.UserID = "restricted"
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatalf("query ordinary catalog with retained theme records: %v", err)
			}
			if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, test.ids) || result.TotalRecordCount != test.total {
				t.Fatalf("ordinary query = %v / total %d, want %v / %d", got, result.TotalRecordCount, test.ids, test.total)
			}
		})
	}
	for _, userID := range []string{"admin", "default", "restricted"} {
		result, err := store.QueryItems(ctx, Query{UserID: userID, Ids: append(themeVisibilityHiddenIDs(), "theme-song-a")})
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Fatalf("ordinary ID query for %s exposed theme resources: %v, %v", userID, queryItemIDs(result.Items), err)
		}
	}
	for _, parentID := range themeVisibilityHiddenIDs() {
		for _, recursive := range []bool{false, true} {
			if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: parentID, Recursive: recursive}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("hidden parent %s (recursive=%v) returned %v, want ErrNotFound", parentID, recursive, err)
			}
		}
	}
	var retained int
	if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM item_theme_resources WHERE NOT active").Scan(&retained); err != nil || retained != 2 {
		t.Fatalf("ordinary reads changed permanent inactive resource roles: count=%d, error=%v", retained, err)
	}
}

func TestThemeVisibilityExplicitReadsReturnOnlyActiveOwnedResources(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	subject := Subject{UserID: "restricted"}
	for _, id := range []string{"movie-b", "theme-control-b", "theme-song-b", "theme-video-b", "theme-library-song-b"} {
		item, err := store.GetItemFor(ctx, subject, id)
		if err != nil || item.ID != id {
			t.Fatalf("explicit authorized item %s = %s, %v", id, item.ID, err)
		}
	}
	for _, id := range []string{"theme-inactive-b", "theme-associated-unreserved-b", "theme-inactive-unreserved-b",
		"theme-legacy-file-b", "theme-legacy-directory-b", "theme-legacy-nested-b", "theme-legacy-descendant-b", "theme-song-a", "missing"} {
		if _, err := store.GetItemFor(ctx, subject, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid or inaccessible explicit theme %s returned %v, want ErrNotFound", id, err)
		}
	}
	requested := []string{"theme-video-b", "theme-inactive-b", "movie-b", "theme-song-a", "theme-song-b", "missing",
		"theme-associated-unreserved-b", "theme-library-song-b", "theme-legacy-descendant-b"}
	items, err := store.GetItemsByIDFor(ctx, subject, requested)
	want := []string{"theme-video-b", "movie-b", "theme-song-b", "theme-library-song-b"}
	if err != nil || !reflect.DeepEqual(queryItemIDs(items), want) {
		t.Fatalf("explicit batch = %v, %v, want input-ordered authorized subset %v", queryItemIDs(items), err, want)
	}
	if item, err := store.GetItemFor(ctx, Subject{UserID: "default"}, "theme-song-a"); err != nil || item.ID != "theme-song-a" {
		t.Fatalf("an independently authorized library theme was not addressable: %s, %v", item.ID, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false}'::jsonb WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItemFor(ctx, subject, "theme-song-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("explicit theme read ignored the current library restriction: %v", err)
	}
	items, err = store.GetItemsByIDFor(ctx, subject, requested)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("explicit batch ignored the current library restriction: %v, %v", queryItemIDs(items), err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = 'restricted'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItemFor(ctx, subject, "theme-song-b"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled account could resolve an explicit theme: %v", err)
	}
	if _, err := store.GetItemsByIDFor(ctx, subject, requested); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled account could resolve an explicit batch: %v", err)
	}
}

func TestThemeVisibilityExplicitReadsRejectBrokenResourceOwnership(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	// Keep the foreign root empty so this case reaches the authorization
	// predicate instead of colliding with the other library's valid song path.
	if _, err := store.pool.Exec(ctx, `INSERT INTO library_roots (id,library_id,path,allowed_path,relative_path)
		VALUES ('theme-root-a2','library-a','/media/a2','/media','a2');
		INSERT INTO theme_reserved_paths (root_id,relative_path,is_directory)
		VALUES ('theme-root-a2','Owner/theme.mp3',false)`); err != nil {
		t.Fatalf("seed a noncolliding reserved path in the foreign library: %v", err)
	}
	subject := Subject{UserID: "default"}
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, `
			UPDATE items SET library_id = 'library-b', root_id = 'root-b', parent_id = 'movie-b',
				type = 'Audio', is_folder = false WHERE id = 'theme-song-b';
			UPDATE items SET library_id = 'library-b', root_id = 'root-b', parent_id = 'library-b',
				type = 'Movie', is_folder = false WHERE id = 'movie-b';
			DELETE FROM item_theme_resources WHERE resource_item_id = 'movie-b';
			DELETE FROM theme_reserved_paths WHERE root_id = 'root-b' AND relative_path = 'Owner/film.mp4';
			INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
				VALUES ('root-b', 'Owner/theme.mp3', false) ON CONFLICT (root_id, relative_path) DO NOTHING;
			INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
				VALUES ('theme-song-b', 'movie-b', 'song', true)
				ON CONFLICT (resource_item_id) DO UPDATE SET owner_item_id = EXCLUDED.owner_item_id, kind = EXCLUDED.kind, active = EXCLUDED.active`); err != nil {
			t.Fatalf("restore the owned theme fixture between independent corruptions: %v", err)
		}
	}
	for _, test := range []struct {
		name string
		sql  string
	}{
		{"inactive_role", `UPDATE item_theme_resources SET active = false WHERE resource_item_id = 'theme-song-b'`},
		{"missing_role", `DELETE FROM item_theme_resources WHERE resource_item_id = 'theme-song-b'`},
		{"unreserved_resource", `DELETE FROM theme_reserved_paths WHERE root_id = 'root-b' AND relative_path = 'Owner/theme.mp3'`},
		{"resource_is_folder", `UPDATE items SET is_folder = true WHERE id = 'theme-song-b'`},
		{"wrong_resource_type", `UPDATE items SET type = 'Video' WHERE id = 'theme-song-b'`},
		{"wrong_role_kind", `UPDATE item_theme_resources SET kind = 'video' WHERE resource_item_id = 'theme-song-b'`},
		{"wrong_parent", `UPDATE items SET parent_id = 'series-b' WHERE id = 'theme-song-b'`},
		{"missing_resource_root", `UPDATE items SET root_id = NULL WHERE id = 'theme-song-b'`},
		{"resource_root_in_another_library", `UPDATE items SET root_id = 'theme-root-a2' WHERE id = 'theme-song-b'`},
		{"resource_library_differs_from_root_and_owner", `UPDATE items SET library_id = 'library-a' WHERE id = 'theme-song-b'`},
		{"owner_has_another_root", `UPDATE items SET root_id = 'theme-root-b2' WHERE id = 'movie-b'`},
		{"ordinary_owner_has_no_root", `UPDATE items SET root_id = NULL WHERE id = 'movie-b'`},
		{"forged_collection_folder_owner", `UPDATE items SET type = 'CollectionFolder', is_folder = true, root_id = NULL WHERE id = 'movie-b'`},
		{"owner_in_another_library", `UPDATE items SET parent_id = 'movie-a' WHERE id = 'theme-song-b';
			UPDATE item_theme_resources SET owner_item_id = 'movie-a' WHERE resource_item_id = 'theme-song-b'`},
		{"owner_is_reserved", `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory) VALUES ('root-b', 'Owner/film.mp4', false)`},
		{"owner_has_a_permanent_inactive_role", `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
			VALUES ('movie-b', 'library-b', 'video', false)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(func() { reset(t) })
			if _, err := store.pool.Exec(ctx, test.sql); err != nil {
				t.Fatalf("seed broken theme ownership: %v", err)
			}
			if _, err := store.GetItemFor(ctx, subject, "theme-song-b"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("broken explicit theme returned %v, want ErrNotFound", err)
			}
			items, err := store.GetItemsByIDFor(ctx, subject, []string{"theme-song-b", "audio-b"})
			if err != nil || !reflect.DeepEqual(queryItemIDs(items), []string{"audio-b"}) {
				t.Fatalf("broken theme escaped explicit batch filtering: %v, %v", queryItemIDs(items), err)
			}
			result, err := store.QueryItems(ctx, Query{UserID: "default", Ids: []string{"theme-song-b"}})
			if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
				t.Fatalf("broken theme reappeared in ordinary browsing: %v, %v", queryItemIDs(result.Items), err)
			}
		})
	}
	item, err := store.GetItemFor(ctx, subject, "theme-song-b")
	if err != nil || item.ID != "theme-song-b" {
		t.Fatalf("restored valid theme did not become explicitly addressable again: %s, %v", item.ID, err)
	}
}

func TestThemeVisibilityReservedPathsRespectRootAndComponentBoundaries(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, relative_path) VALUES
		('theme-other-root-control', 'library-b', 'theme-root-b2', 'library-b', 'Same Path Other Root', 'A Root Control', 'Audio', false,
		 '/media/b2/Owner/theme-music/track.mp3', 'Owner/theme-music/track.mp3'),
		('theme-literal-hidden', 'library-b', 'root-b', 'movie-b', 'Literal Reserved Child', 'B Literal Hidden', 'Audio', false,
		 '/media/b/Owner/100%_themes/song.mp3', 'Owner/100%_themes/song.mp3'),
		('theme-literal-control', 'library-b', 'root-b', 'movie-b', 'Literal Prefix Control', 'C Literal Control', 'Audio', false,
		 '/media/b/Owner/100xythemes/song.mp3', 'Owner/100xythemes/song.mp3');
		INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
		VALUES ('root-b', 'Owner/100%_themes', true)`); err != nil {
		t.Fatalf("seed reserved path boundary controls: %v", err)
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{
		"theme-other-root-control", "theme-literal-hidden", "theme-literal-control", "theme-control-b", "theme-prefix-control-b"}})
	want := []string{"theme-other-root-control", "theme-literal-control", "theme-control-b", "theme-prefix-control-b"}
	if err != nil || result.TotalRecordCount != len(want) || !reflect.DeepEqual(queryItemIDs(result.Items), want) {
		t.Fatalf("reserved paths crossed root or literal component boundaries: %v / total %d, %v", queryItemIDs(result.Items), result.TotalRecordCount, err)
	}
	for _, id := range want {
		if item, err := store.GetItemFor(ctx, Subject{UserID: "restricted"}, id); err != nil || item.ID != id {
			t.Fatalf("ordinary boundary control %s became inaccessible: %s, %v", id, item.ID, err)
		}
	}
	if _, err := store.GetItemFor(ctx, Subject{UserID: "restricted"}, "theme-literal-hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reserved legacy descendant without a role remained directly accessible: %v", err)
	}
}

func TestThemeVisibilityExplicitBatchValidatesEveryIDAndCurrentSubject(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	subject := Subject{UserID: "restricted"}
	for _, input := range [][]string{nil, {}} {
		items, err := store.GetItemsByIDFor(ctx, subject, input)
		if err != nil || items == nil || len(items) != 0 {
			t.Fatalf("empty explicit batch = %v, %v, want nonnil empty items", items, err)
		}
	}
	for _, input := range [][]string{{""}, {" "}, {"theme-song-b", "bad\x00id"},
		{string([]byte{0xff})}, {strings.Repeat("a", 257)}, {"theme-song-b", "theme-song-b"}} {
		if _, err := store.GetItemsByIDFor(ctx, subject, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("malformed explicit batch returned %v, want ErrInvalidInput", err)
		}
	}
	ids := make([]string, 1000)
	for index := range ids {
		ids[index] = fmt.Sprintf("missing-theme-batch-%04d", index)
	}
	ids[0], ids[999] = "theme-video-b", "movie-b"
	items, err := store.GetItemsByIDFor(ctx, subject, ids)
	if err != nil || !reflect.DeepEqual(queryItemIDs(items), []string{"theme-video-b", "movie-b"}) {
		t.Fatalf("maximum explicit batch lost its input order or visible subset: %v, %v", queryItemIDs(items), err)
	}
	if _, err := store.GetItemsByIDFor(ctx, subject, append(ids, "overflow-theme-batch")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-limit explicit batch was truncated or accepted: %v", err)
	}
	for _, userID := range []string{"disabled", "unknown", "malformed"} {
		if _, err := store.GetItemsByIDFor(ctx, Subject{UserID: userID}, nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("empty explicit batch bypassed current subject %s: %v", userID, err)
		}
	}
}
