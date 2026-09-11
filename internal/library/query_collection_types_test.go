package library

import (
	"errors"
	"reflect"
	"testing"
)

func TestQueryCollectionTypesNormalizeKnownKindsAndRejectUnknown(t *testing.T) {
	query, err := normalizeItemQuery(Query{UserID: "viewer", IncludeItemTypes: []string{"pLaYlIsT", " BoxSet ", "playlist", "Movie"}})
	if err != nil || !reflect.DeepEqual(query.IncludeItemTypes, []string{"Playlist", "BoxSet", "Movie"}) {
		t.Fatal("collection type filters did not preserve their distinct canonical catalog kinds")
	}
	for _, kinds := range [][]string{{"Unknown"}, {"Playlist", "Unknown"}, {"BoxSet", "Movie; DROP TABLE items"}} {
		if _, err := normalizeItemQuery(Query{UserID: "viewer", IncludeItemTypes: kinds}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("recognizing observed collection kinds accepted an unknown item kind")
		}
	}
}

func TestQueryCollectionTypesUseRealCatalogFilteringAndPermissions(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, kind := range []string{"Playlist", "BoxSet"} {
		result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{kind}})
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Fatal("an absent collection kind did not produce its real empty catalog result")
		}
	}
	// These rows represent catalog facts, not a new playlist or collection
	// creation API. Their nonempty results prevent a hard-coded empty response.
	if _, err := store.pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('playlist-a', 'library-a', 'library-a', '0 Hidden Playlist', '0 Hidden Playlist', 'Playlist', true),
		('boxset-a', 'library-a', 'library-a', '0 Hidden Collection', '0 Hidden Collection', 'BoxSet', true),
		('playlist-b', 'library-b', 'library-b', 'E Visible Playlist', 'E Visible Playlist', 'Playlist', true),
		('folder-b', 'library-b', 'library-b', 'Nested Folder', 'Nested Folder', 'Folder', true),
		('playlist-nested-b', 'library-b', 'folder-b', 'F Nested Playlist', 'F Nested Playlist', 'Playlist', true),
		('boxset-b', 'library-b', 'library-b', 'G Visible Collection', 'G Visible Collection', 'BoxSet', true)`); err != nil {
		t.Fatalf("seed representative catalog collection kinds: %v", err)
	}
	for _, test := range []struct {
		name  string
		query Query
		ids   []string
		total int
	}{
		{"visible_playlists", Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Playlist"}}, []string{"playlist-b", "playlist-nested-b"}, 2},
		{"visible_boxsets", Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"BoxSet"}}, []string{"boxset-b"}, 1},
		{"mixed_movie_and_playlists", Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Movie", "Playlist"}}, []string{"movie-b", "playlist-b", "playlist-nested-b"}, 3},
		{"page_after_acl_and_type_filter", Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Movie", "Playlist"}, StartIndex: 1, Limit: 1}, []string{"playlist-b"}, 3},
		{"direct_parent", Query{UserID: "restricted", ParentID: "library-b", IncludeItemTypes: []string{"Playlist"}}, []string{"playlist-b"}, 1},
		{"recursive_parent", Query{UserID: "restricted", ParentID: "folder-b", Recursive: true, IncludeItemTypes: []string{"Playlist"}}, []string{"playlist-nested-b"}, 1},
		{"no_visible_libraries", Query{UserID: "none", Recursive: true, IncludeItemTypes: []string{"Playlist", "BoxSet"}}, []string{}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatalf("query catalog collection kinds: %v", err)
			}
			ids := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				ids = append(ids, item.ID)
			}
			if result.TotalRecordCount != test.total || !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("collection query returned IDs %v / total %d, want %v / %d", ids, result.TotalRecordCount, test.ids, test.total)
			}
		})
	}
	if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "library-a", Recursive: true,
		IncludeItemTypes: []string{"Playlist"}}); !errors.Is(err, ErrNotFound) {
		t.Fatal("a collection type filter bypassed explicit parent authorization")
	}
}
