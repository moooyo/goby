package library

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

type tvParentExpectation struct {
	series *TVParentRef
	season *TVParentRef
}

func assertTVParentReads(t *testing.T, ctx context.Context, store *Store, userID string, expected map[string]tvParentExpectation) {
	t.Helper()
	ids := make([]string, 0, len(expected))
	for id := range expected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	assertItems := func(method string, items []Item) {
		t.Helper()
		if len(items) != len(expected) {
			t.Fatalf("%s returned %d items, want %d", method, len(items), len(expected))
		}
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			want, exists := expected[item.ID]
			if !exists || seen[item.ID] {
				t.Fatalf("%s returned an unexpected or duplicate item %q", method, item.ID)
			}
			seen[item.ID] = true
			if !reflect.DeepEqual(item.Series, want.series) || !reflect.DeepEqual(item.Season, want.season) {
				t.Errorf("%s item %s parents = series %+v, season %+v; want series %+v, season %+v",
					method, item.ID, item.Series, item.Season, want.series, want.season)
			}
		}
	}
	var details []Item
	for _, id := range ids {
		item, err := store.GetItem(ctx, userID, id)
		if err != nil {
			t.Fatalf("GetItem %s: %v", id, err)
		}
		details = append(details, item)
	}
	assertItems("GetItem", details)
	batch, err := store.GetItemsByIDFor(ctx, Subject{UserID: userID}, ids)
	if err != nil {
		t.Fatalf("GetItemsByIDFor: %v", err)
	}
	assertItems("GetItemsByIDFor", batch)
	if got := queryItemIDs(batch); !reflect.DeepEqual(got, ids) {
		t.Errorf("GetItemsByIDFor changed input order: got %v, want %v", got, ids)
	}
	result, err := store.QueryItems(ctx, Query{UserID: userID, Ids: ids, Recursive: true, Limit: 1000})
	if err != nil {
		t.Fatalf("QueryItems: %v", err)
	}
	if result.TotalRecordCount != len(expected) {
		t.Errorf("QueryItems total = %d, want %d", result.TotalRecordCount, len(expected))
	}
	assertItems("QueryItems", result.Items)
}

func TestTVParentQueriesUseCurrentCatalogNamesAcrossReadEntrypoints(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	seedLibraryQueryFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('tv-second-root', 'library-b', '/media/second', '/media', 'second');
		UPDATE items SET root_id = 'root-b', relative_path = 'Virtual Series' WHERE id = 'series-b';
		UPDATE items SET root_id = 'tv-second-root', relative_path = 'Virtual Season' WHERE id = 'season-b';
		UPDATE items SET root_id = 'root-b', relative_path = 'Missing Episode.mkv',
			path = '/unavailable/tv-parent/Missing Episode.mkv' WHERE id = 'episode-b1';
		INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('tv-direct-episode', 'library-b', 'series-b', 'Direct Episode', 'Direct Episode', 'Episode', false),
		('tv-other-series', 'library-b', 'library-b', 'Other Series', 'Other Series', 'Series', true),
		('tv-other-season', 'library-b', 'tv-other-series', 'Other Season', 'Other Season', 'Season', true),
		('tv-other-episode', 'library-b', 'tv-other-season', 'Other Episode', 'Other Episode', 'Episode', false),
		('tv-middle-folder', 'library-b', 'series-b', 'Middle Folder', 'Middle Folder', 'Folder', true),
		('tv-folder-episode', 'library-b', 'tv-middle-folder', 'Nested Episode', 'Nested Episode', 'Episode', false)`); err != nil {
		t.Fatalf("seed virtual and independent TV parent chains: %v", err)
	}
	series := &TVParentRef{ID: "series-b", Name: "Z Series"}
	season := &TVParentRef{ID: "season-b", Name: "Y Season"}
	expected := map[string]tvParentExpectation{
		"episode-b1":        {series: series, season: season},
		"episode-b2":        {series: series, season: season},
		"season-b":          {series: series},
		"series-b":          {},
		"tv-direct-episode": {series: series},
		"tv-other-episode": {
			series: &TVParentRef{ID: "tv-other-series", Name: "Other Series"},
			season: &TVParentRef{ID: "tv-other-season", Name: "Other Season"},
		},
		"tv-folder-episode": {},
		"tv-middle-folder":  {},
		"movie-b":           {},
	}
	assertTVParentReads(t, ctx, store, "restricted", expected)
	for _, id := range []string{series.ID, season.ID} {
		item, err := store.GetItem(ctx, "restricted", id)
		if err != nil || item.Path != "" {
			t.Fatalf("virtual parent %s lost its empty catalog path: path = %q, error = %v", id, item.Path, err)
		}
	}
	actor := metadataEditTestActor(t, ctx, pool, "tv-parent-metadata-editor")
	for _, parent := range []*TVParentRef{series, season} {
		before := metadataEditTestDetail(t, ctx, store, actor, parent.ID)
		name := "Edited " + parent.Name
		updated := metadataEditTestUpdate(t, ctx, store, actor, before,
			map[string]json.RawMessage{"Name": metadataEditTestRaw(t, name)}, nil)
		if updated.Effective.Name != name {
			t.Fatalf("metadata edit returned name %q, want %q", updated.Effective.Name, name)
		}
		var catalogName string
		if err := pool.QueryRow(ctx, "SELECT name FROM items WHERE id = $1", parent.ID).Scan(&catalogName); err != nil || catalogName != name {
			t.Fatalf("metadata edit did not persist parent %s catalog name: name = %q, error = %v", parent.ID, catalogName, err)
		}
		parent.Name = name
		assertTVParentReads(t, ctx, store, "restricted", expected)
	}
}

func TestTVParentQueriesRejectInvalidEdgesAndKeepProvenSeason(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	series := &TVParentRef{ID: "series-b", Name: "Z Series"}
	season := &TVParentRef{ID: "season-b", Name: "Y Season"}
	for _, test := range []struct {
		name          string
		statement     string
		episodeParent tvParentExpectation
		seasonParent  tvParentExpectation
	}{
		{
			name:      "cross_library_season",
			statement: `UPDATE items SET library_id = 'library-a' WHERE id = 'season-b'`,
		},
		{
			name:          "cross_library_series",
			statement:     `UPDATE items SET library_id = 'library-a' WHERE id = 'series-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
		{
			name: "cross_library_direct_series",
			statement: `UPDATE items SET library_id = 'library-a' WHERE id = 'series-b';
				UPDATE items SET parent_id = 'series-b' WHERE id = 'episode-b1'`,
		},
		{
			name:      "wrong_season_type",
			statement: `UPDATE items SET type = 'Folder' WHERE id = 'season-b'`,
		},
		{
			name:      "season_is_not_folder",
			statement: `UPDATE items SET is_folder = false WHERE id = 'season-b'`,
		},
		{
			name:          "wrong_series_type",
			statement:     `UPDATE items SET type = 'Folder' WHERE id = 'series-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
		{
			name:          "series_is_not_folder",
			statement:     `UPDATE items SET is_folder = false WHERE id = 'series-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
		{
			name:         "episode_is_folder",
			statement:    `UPDATE items SET is_folder = true WHERE id = 'episode-b1'`,
			seasonParent: tvParentExpectation{series: series},
		},
		{
			name:         "non_tv_child",
			statement:    `UPDATE items SET type = 'Movie' WHERE id = 'episode-b1'`,
			seasonParent: tvParentExpectation{series: series},
		},
		{
			name:         "episode_has_no_parent",
			statement:    `UPDATE items SET parent_id = NULL WHERE id = 'episode-b1'`,
			seasonParent: tvParentExpectation{series: series},
		},
		{
			name:          "season_has_no_parent",
			statement:     `UPDATE items SET parent_id = NULL WHERE id = 'season-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
		{
			name:         "episode_parent_is_itself",
			statement:    `UPDATE items SET parent_id = id WHERE id = 'episode-b1'`,
			seasonParent: tvParentExpectation{series: series},
		},
		{
			name:          "season_parent_is_itself",
			statement:     `UPDATE items SET parent_id = id WHERE id = 'season-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
		{
			name:          "episode_season_cycle",
			statement:     `UPDATE items SET parent_id = 'episode-b1' WHERE id = 'season-b'`,
			episodeParent: tvParentExpectation{season: season},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, `UPDATE items SET library_id = 'library-b',
				parent_id = 'library-b', type = 'Series', is_folder = true WHERE id = 'series-b';
				UPDATE items SET library_id = 'library-b', parent_id = 'series-b',
				type = 'Season', is_folder = true WHERE id = 'season-b';
				UPDATE items SET parent_id = 'season-b', type = 'Episode', is_folder = false WHERE id = 'episode-b1'`); err != nil {
				t.Fatalf("reset TV parent edge fixture: %v", err)
			}
			if _, err := store.pool.Exec(ctx, test.statement); err != nil {
				t.Fatalf("seed invalid TV parent edge: %v", err)
			}
			// Both callers can access every library, so authorization cannot hide
			// an incorrect cross-library parent projection from this assertion.
			for _, userID := range []string{"admin", "default"} {
				assertTVParentReads(t, ctx, store, userID, map[string]tvParentExpectation{
					"episode-b1": test.episodeParent,
					"season-b":   test.seasonParent,
				})
			}
		})
	}
}

func TestTVParentQueriesHideAuxiliaryAndReservedParents(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET root_id = 'root-b',
		relative_path = id || '/catalog', path = '/media/b/' || id || '/catalog'
		WHERE id IN ('series-b', 'season-b')`); err != nil {
		t.Fatalf("seed separately reserved TV parent paths: %v", err)
	}
	for _, parentID := range []string{"series-b", "season-b"} {
		for _, test := range []struct {
			name      string
			statement string
		}{
			{"active_theme_role", `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
				VALUES ($1, 'movie-b', 'video', true)`},
			{"inactive_theme_role", `INSERT INTO item_theme_resources (resource_item_id, owner_item_id, kind, active)
				VALUES ($1, 'movie-b', 'video', false)`},
			{"active_extra_role", `INSERT INTO item_extra_resources (resource_item_id, owner_item_id, kind, active)
				VALUES ($1, 'movie-b', 'clip', true)`},
			{"inactive_extra_role", `INSERT INTO item_extra_resources (resource_item_id, owner_item_id, kind, active)
				VALUES ($1, 'movie-b', 'clip', false)`},
			{"exact_theme_reservation", `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
				SELECT root_id, relative_path, false FROM items WHERE id = $1`},
			{"theme_reserved_directory", `INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
				SELECT root_id, split_part(relative_path, '/', 1), true FROM items WHERE id = $1`},
			{"exact_extra_reservation", `INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
				SELECT root_id, relative_path, false FROM items WHERE id = $1`},
			{"extra_reserved_directory", `INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
				SELECT root_id, split_part(relative_path, '/', 1), true FROM items WHERE id = $1`},
		} {
			t.Run(parentID+"/"+test.name, func(t *testing.T) {
				if _, err := store.pool.Exec(ctx, `DELETE FROM item_theme_resources WHERE resource_item_id IN ('series-b', 'season-b');
					DELETE FROM item_extra_resources WHERE resource_item_id IN ('series-b', 'season-b');
					DELETE FROM theme_reserved_paths WHERE root_id = 'root-b';
					DELETE FROM extra_reserved_paths WHERE root_id = 'root-b'`); err != nil {
					t.Fatalf("reset hidden TV parent state: %v", err)
				}
				// The database permits these damaged role/type combinations.
				// Ordinary reads must reject permanent roles even in this state.
				if _, err := store.pool.Exec(ctx, test.statement, parentID); err != nil {
					t.Fatalf("seed hidden TV parent: %v", err)
				}
				var want tvParentExpectation
				if parentID == "series-b" {
					want.season = &TVParentRef{ID: "season-b", Name: "Y Season"}
				}
				for _, userID := range []string{"restricted", "admin"} {
					assertTVParentReads(t, ctx, store, userID, map[string]tvParentExpectation{
						"episode-b1": want,
						"episode-b2": want,
					})
				}
			})
		}
	}
}
