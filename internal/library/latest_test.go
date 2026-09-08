package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueryLatestRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	negativeParentIndex := -1
	tests := []struct {
		name  string
		query Query
	}{
		{name: "missing user", query: Query{}},
		{name: "negative start", query: Query{UserID: "user", StartIndex: -1}},
		{name: "negative limit", query: Query{UserID: "user", Limit: -1}},
		{name: "excessive limit", query: Query{UserID: "user", Limit: 1001}},
		{name: "unknown item type", query: Query{UserID: "user", IncludeItemTypes: []string{"unknown"}}},
		{name: "negative parent index", query: Query{UserID: "user", ParentIndexNumber: &negativeParentIndex}},
	}
	store := &Store{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, group := range []bool{false, true} {
				if _, err := store.QueryLatest(context.Background(), test.query, group); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("query with grouping %v error = %v, want ErrInvalidInput", group, err)
				}
			}
		})
	}
}

func TestQueryLatestGroupsBeforePagination(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)

	tests := []struct {
		name  string
		query Query
		want  []latestExpectation
	}{
		{
			name:  "all visible media are grouped and ordered by their latest match",
			query: Query{UserID: "restricted"},
			want: []latestExpectation{
				{"album-b", 2}, {"movie-b", 1}, {"series-b", 2},
				{"series-b2", 1}, {"audio-b", 1}, {"orphan-episode-b", 1},
			},
		},
		{
			name:  "ACL and grouping precede offset and limit",
			query: Query{UserID: "restricted", StartIndex: 1, Limit: 1},
			want:  []latestExpectation{{"movie-b", 1}},
		},
		{
			name:  "equal timestamps use the representative ID in ascending order",
			query: Query{UserID: "restricted", StartIndex: 2, Limit: 1},
			want:  []latestExpectation{{"series-b", 2}},
		},
		{
			name:  "offset beyond the final group returns an empty slice",
			query: Query{UserID: "restricted", StartIndex: 6, Limit: 1},
			want:  []latestExpectation{},
		},
		{
			name:  "a user with no visible libraries receives an empty slice",
			query: Query{UserID: "none"},
			want:  []latestExpectation{},
		},
		{
			name:  "default policy includes other libraries without cross-library grouping",
			query: Query{UserID: "default", Limit: 3},
			want:  []latestExpectation{{"movie-a", 1}, {"cross-library-child", 1}, {"album-b", 2}},
		},
		{
			name:  "administrators retain unrestricted library access",
			query: Query{UserID: "admin", Limit: 3},
			want:  []latestExpectation{{"movie-a", 1}, {"cross-library-child", 1}, {"album-b", 2}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := store.QueryLatest(ctx, test.query, true)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, items, test.want)
		})
	}
}

func TestQueryLatestFiltersSourceMediaBeforeGrouping(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)
	firstSeason, secondSeason, zero := 1, 2, 0

	tests := []struct {
		name  string
		query Query
		want  []latestExpectation
	}{
		{
			name:  "IDs filter matching media and exclude hidden items and folders",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "series-b", "episode-b2", "movie-b"}},
			want:  []latestExpectation{{"movie-b", 1}, {"series-b", 1}},
		},
		{
			name:  "filtered group timestamps exclude newer unmatched media",
			query: Query{UserID: "restricted", Ids: []string{"audio-b2", "episode-b3"}},
			want:  []latestExpectation{{"series-b2", 1}, {"album-b", 1}},
		},
		{
			name:  "literal wildcard search matches an episode rather than its series",
			query: Query{UserID: "restricted", SearchTerm: "%_"},
			want:  []latestExpectation{{"series-b", 1}},
		},
		{
			name:  "episode type filter returns series representatives",
			query: Query{UserID: "restricted", IncludeItemTypes: []string{"ePiSoDe"}},
			want:  []latestExpectation{{"series-b", 2}, {"series-b2", 1}, {"orphan-episode-b", 1}},
		},
		{
			name:  "audio filter groups indirect album descendants and retains orphan audio",
			query: Query{UserID: "restricted", MediaTypes: []string{"aUdIo"}},
			want:  []latestExpectation{{"album-b", 2}, {"audio-b", 1}},
		},
		{
			name:  "video filter excludes audio and folders",
			query: Query{UserID: "restricted", MediaTypes: []string{"Video"}},
			want:  []latestExpectation{{"movie-b", 1}, {"series-b", 2}, {"series-b2", 1}, {"orphan-episode-b", 1}},
		},
		{
			name:  "folder-only filters have no source media",
			query: Query{UserID: "restricted", IncludeItemTypes: []string{"Series", "MusicAlbum"}},
			want:  []latestExpectation{},
		},
		{
			name:  "first parent index counts only matching source media",
			query: Query{UserID: "restricted", ParentIndexNumber: &firstSeason},
			want:  []latestExpectation{{"album-b", 1}, {"series-b", 1}, {"series-b2", 1}},
		},
		{
			name:  "second parent index excludes newer media in each group",
			query: Query{UserID: "restricted", ParentIndexNumber: &secondSeason},
			want:  []latestExpectation{{"series-b", 1}, {"album-b", 1}},
		},
		{
			name:  "zero parent index is an explicit filter",
			query: Query{UserID: "restricted", ParentIndexNumber: &zero},
			want:  []latestExpectation{{"movie-b", 1}, {"audio-b", 1}, {"orphan-episode-b", 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := store.QueryLatest(ctx, test.query, true)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, items, test.want)
		})
	}
}

func TestQueryLatestRawReturnsIndividualMedia(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)

	items, err := store.QueryLatest(ctx, Query{UserID: "restricted"}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{
		{"audio-b1", 1}, {"episode-b1", 1}, {"movie-b", 1}, {"episode-b2", 1},
		{"episode-b3", 1}, {"audio-b2", 1}, {"audio-b", 1}, {"orphan-episode-b", 1},
	})
	for _, latest := range items {
		if latest.Item.IsFolder {
			t.Errorf("raw latest item %s is a folder", latest.Item.ID)
		}
		if latest.Item.ID == "episode-b1" && (latest.Item.Media == nil || latest.Item.Media.DurationTicks != 15000000) {
			t.Errorf("raw latest episode media = %+v, want stored media metadata", latest.Item.Media)
		}
	}
	items, err = store.QueryLatest(ctx, Query{UserID: "restricted", StartIndex: 1, Limit: 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{{"episode-b1", 1}, {"movie-b", 1}})

	secondSeason := 2
	items, err = store.QueryLatest(ctx, Query{UserID: "restricted", ParentIndexNumber: &secondSeason}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{{"episode-b2", 1}, {"audio-b2", 1}})
}

func TestQueryLatestParentScopeUsesSameLibraryDescendants(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)

	for _, userID := range []string{"restricted", "default", "admin"} {
		t.Run(userID, func(t *testing.T) {
			for _, parentID := range []string{"series-b", "season-b"} {
				items, err := store.QueryLatest(ctx, Query{UserID: userID, ParentID: parentID}, true)
				if err != nil {
					t.Fatal(err)
				}
				assertLatestItems(t, items, []latestExpectation{{"series-b", 2}})
				items, err = store.QueryLatest(ctx, Query{UserID: userID, ParentID: parentID}, false)
				if err != nil {
					t.Fatal(err)
				}
				assertLatestItems(t, items, []latestExpectation{{"episode-b1", 1}, {"episode-b2", 1}})
			}
		})
	}
	items, err := store.QueryLatest(ctx, Query{
		UserID: "default", ParentID: "season-b", Ids: []string{"episode-b1", "cross-library-child", "movie-b"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{{"series-b", 1}})
	items, err = store.QueryLatest(ctx, Query{UserID: "restricted", ParentID: "episode-b1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{})
	for _, parentID := range []string{"library-a", "movie-a", "missing"} {
		if _, err := store.QueryLatest(ctx, Query{UserID: "restricted", ParentID: parentID}, true); !errors.Is(err, ErrNotFound) {
			t.Errorf("query inaccessible parent %s error = %v, want ErrNotFound", parentID, err)
		}
	}
}

func TestQueryLatestEnforcesActiveUserPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)

	for _, userID := range []string{"disabled", "unknown", "malformed"} {
		for _, group := range []bool{false, true} {
			if _, err := store.QueryLatest(ctx, Query{UserID: userID}, group); !errors.Is(err, ErrForbidden) {
				t.Errorf("query user %s with grouping %v error = %v, want ErrForbidden", userID, group, err)
			}
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false}'::jsonb WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	items, err := store.QueryLatest(ctx, Query{UserID: "restricted"}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{})
}

func TestQueryLatestFallsBackWhenAncestorsContainACycle(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('cycle-a', 'library-b', NULL, 'Cycle A', 'Cycle A', 'Folder', true),
		('cycle-b', 'library-b', 'cycle-a', 'Cycle B', 'Cycle B', 'Folder', true);
		UPDATE items SET parent_id = 'cycle-b' WHERE id = 'cycle-a';
		UPDATE items SET parent_id = 'cycle-a' WHERE id IN ('audio-b', 'orphan-episode-b')`); err != nil {
		t.Fatal(err)
	}
	items, err := store.QueryLatest(ctx, Query{
		UserID: "restricted", Ids: []string{"audio-b", "orphan-episode-b"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, items, []latestExpectation{{"audio-b", 1}, {"orphan-episode-b", 1}})
}

type latestExpectation struct {
	id         string
	childCount int
}

func assertLatestItems(t *testing.T, items []LatestItem, want []latestExpectation) {
	t.Helper()
	if items == nil {
		t.Fatal("latest items must be a non-nil slice")
	}
	got := make([]latestExpectation, 0, len(items))
	for _, latest := range items {
		got = append(got, latestExpectation{latest.Item.ID, latest.ChildCount})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("latest items = %+v, want %+v", got, want)
	}
}

func seedLibraryLatestFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedLibraryQueryFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('album-b', 'library-b', 'library-b', 'Visible Album', 'Visible Album', 'MusicAlbum', true),
		('album-disc-b', 'library-b', 'album-b', 'Disc One', 'Disc One', 'Folder', true),
		('audio-b1', 'library-b', 'album-disc-b', 'First Album Song', 'First Album Song', 'Audio', false),
		('audio-b2', 'library-b', 'album-disc-b', 'Second Album Song', 'Second Album Song', 'Audio', false),
		('series-b2', 'library-b', 'library-b', 'Other Series', 'Other Series', 'Series', true),
		('season-b2', 'library-b', 'series-b2', 'Other Season', 'Other Season', 'Season', true),
		('episode-b3', 'library-b', 'season-b2', 'Other Episode', 'Other Episode', 'Episode', false),
		('orphan-episode-b', 'library-b', NULL, 'Orphan Episode', 'Orphan Episode', 'Episode', false);
		UPDATE items SET created_at = '2025-01-20T10:00:00Z'::timestamptz;
		UPDATE items SET created_at = dates.created_at::timestamptz,
			parent_index_number = dates.parent_index_number
		FROM (VALUES
			('movie-a', '2025-01-10T10:00:00Z', 0),
			('cross-library-child', '2025-01-09T10:00:00Z', 1),
			('audio-b1', '2025-01-06T10:00:00Z', 1),
			('episode-b1', '2025-01-05T10:00:00Z', 1),
			('movie-b', '2025-01-05T10:00:00Z', 0),
			('episode-b2', '2025-01-04T10:00:00Z', 2),
			('episode-b3', '2025-01-03T10:00:00Z', 1),
			('audio-b2', '2025-01-02T10:00:00Z', 2),
			('audio-b', '2025-01-01T10:00:00Z', 0),
			('orphan-episode-b', '2024-12-31T10:00:00Z', 0)
		) AS dates(id, created_at, parent_index_number)
		WHERE items.id = dates.id`); err != nil {
		t.Fatalf("seed latest query fixtures: %v", err)
	}
}
