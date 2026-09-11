package library

import (
	"errors"
	"reflect"
	"testing"
)

func tvFilterBool(value bool) *bool {
	return &value
}

func TestQueryTVFiltersUseTypedCatalogFactsBeforeCountingAndPaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET index_number = 1 WHERE id = 'season-b';
		UPDATE items SET parent_index_number = 1 WHERE id IN ('episode-b1', 'episode-b2');
		INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder, index_number, parent_index_number) VALUES
		('season-zero-b', 'library-b', 'series-b', 'Specials', 'E Specials', 'Season', true, 0, 0),
		('special-b1', 'library-b', 'season-zero-b', 'Special One', 'F Special One', 'Episode', false, 1, 0),
		('special-b2', 'library-b', 'season-zero-b', 'Special Two', 'G Special Two', 'Episode', false, 2, 0),
		('season-second-b', 'library-b', 'series-b', 'Season Two', 'H Season Two', 'Season', true, 2, 0),
		('regular-b3', 'library-b', 'season-second-b', 'Regular Three', 'I Regular Three', 'Episode', false, 1, 2),
		('folder-zero-b', 'library-b', 'series-b', 'Ordinary Folder', 'J Ordinary Folder', 'Folder', true, 0, 0),
		('video-zero-b', 'library-b', 'folder-zero-b', 'Ordinary Video', 'K Ordinary Video', 'Video', false, 0, 0),
		('season-zero-a', 'library-a', 'library-a', 'Hidden Specials', '0 Hidden Specials', 'Season', true, 0, 0),
		('special-a', 'library-a', 'season-zero-a', 'Hidden Special', '0 Hidden Special', 'Episode', false, 1, 0)`); err != nil {
		t.Fatalf("seed regular, special, and unrelated zero-index catalog facts: %v", err)
	}

	for _, tc := range []struct {
		name  string
		query Query
		ids   []string
		total int
	}{
		{
			name:  "absent_flags_preserve_the_tree",
			query: Query{ParentID: "series-b", Recursive: true},
			ids:   []string{"episode-b1", "episode-b2", "season-zero-b", "special-b1", "special-b2", "season-second-b", "regular-b3", "folder-zero-b", "video-zero-b", "season-b"},
			total: 10,
		},
		{
			name:  "folders_only",
			query: Query{ParentID: "series-b", Recursive: true, IsFolder: tvFilterBool(true)},
			ids:   []string{"season-zero-b", "season-second-b", "folder-zero-b", "season-b"}, total: 4,
		},
		{
			name:  "files_exclude_season_and_ordinary_folders",
			query: Query{ParentID: "series-b", Recursive: true, IsFolder: tvFilterBool(false)},
			ids:   []string{"episode-b1", "episode-b2", "special-b1", "special-b2", "regular-b3", "video-zero-b"}, total: 6,
		},
		{
			name:  "special_season_does_not_match_other_zero_index_types",
			query: Query{ParentID: "series-b", Recursive: true, IsSpecialSeason: tvFilterBool(true)},
			ids:   []string{"season-zero-b"}, total: 1,
		},
		{
			name:  "excluding_special_seasons_preserves_other_types",
			query: Query{ParentID: "series-b", Recursive: true, IsSpecialSeason: tvFilterBool(false)},
			ids:   []string{"episode-b1", "episode-b2", "special-b1", "special-b2", "season-second-b", "regular-b3", "folder-zero-b", "video-zero-b", "season-b"}, total: 9,
		},
		{
			name:  "special_episode_positive_result_uses_actual_rows",
			query: Query{ParentID: "series-b", Recursive: true, IsSpecialEpisode: tvFilterBool(true)},
			ids:   []string{"special-b1", "special-b2"}, total: 2,
		},
		{
			name:  "excluding_special_episodes_preserves_folders_and_video",
			query: Query{ParentID: "series-b", Recursive: true, IsSpecialEpisode: tvFilterBool(false)},
			ids:   []string{"episode-b1", "episode-b2", "season-zero-b", "season-second-b", "regular-b3", "folder-zero-b", "video-zero-b", "season-b"}, total: 8,
		},
		{
			name:  "regular_file_filter_keeps_unrelated_zero_index_video",
			query: Query{ParentID: "series-b", Recursive: true, IsFolder: tvFilterBool(false), IsSpecialEpisode: tvFilterBool(false)},
			ids:   []string{"episode-b1", "episode-b2", "regular-b3", "video-zero-b"}, total: 4,
		},
		{
			name:  "type_and_boolean_filters_compose",
			query: Query{ParentID: "series-b", Recursive: true, IncludeItemTypes: []string{"Episode"}, IsFolder: tvFilterBool(false), IsSpecialEpisode: tvFilterBool(false)},
			ids:   []string{"episode-b1", "episode-b2", "regular-b3"}, total: 3,
		},
		{
			name:  "contradictory_catalog_predicates_have_a_real_empty_result",
			query: Query{ParentID: "series-b", Recursive: true, IsFolder: tvFilterBool(false), IsSpecialSeason: tvFilterBool(true)},
			ids:   []string{}, total: 0,
		},
		{
			name:  "special_page_is_selected_after_acl_and_filtering",
			query: Query{Recursive: true, IsSpecialEpisode: tvFilterBool(true), StartIndex: 1, Limit: 1},
			ids:   []string{"special-b2"}, total: 2,
		},
		{
			name:  "empty_page_retains_the_filtered_total",
			query: Query{Recursive: true, IsSpecialEpisode: tvFilterBool(true), StartIndex: 2, Limit: 1},
			ids:   []string{}, total: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.query.UserID = "restricted"
			result, err := store.QueryItems(ctx, tc.query)
			if err != nil {
				t.Fatalf("query typed television filters: %v", err)
			}
			if ids := queryItemIDs(result.Items); !reflect.DeepEqual(ids, tc.ids) || result.TotalRecordCount != tc.total {
				t.Fatalf("typed television filters returned %v / total %d, want %v / %d", ids, result.TotalRecordCount, tc.ids, tc.total)
			}
		})
	}
	if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "library-a", Recursive: true,
		IsSpecialEpisode: tvFilterBool(true)}); !errors.Is(err, ErrNotFound) {
		t.Fatal("a special-episode filter bypassed explicit parent authorization")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false}'::jsonb WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, IsSpecialEpisode: tvFilterBool(true)})
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatal("special-episode filtering did not recheck the current library policy")
	}
}
