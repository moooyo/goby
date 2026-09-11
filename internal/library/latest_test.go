package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/metadata"
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

func TestQueryLatestReturnsMetadataFromTheSelectedItem(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata = metadata_rows.metadata::jsonb
		FROM (VALUES
			('album-b', '{"Name":"Album metadata","Overview":"Album overview"}'),
			('series-b', '{"Name":"Series metadata","Overview":"Series overview"}'),
			('audio-b1', '{"Name":"First audio metadata","Overview":"First audio overview"}'),
			('audio-b2', '{"Name":"Second audio metadata","Overview":"Second audio overview"}'),
			('episode-b1', '{"Name":"First episode metadata","Overview":"First episode overview"}'),
			('episode-b2', '{"Name":"Second episode metadata","Overview":"Second episode overview"}'),
			('movie-b', '{"Name":"Visible movie metadata","Overview":"Visible movie overview"}'),
			('movie-a', '{"Name":"Hidden movie metadata","Overview":"Hidden movie overview"}'),
			('cross-library-child', '{"Name":"Hidden episode metadata","Overview":"Hidden episode overview"}')
		) AS metadata_rows(id, metadata) WHERE items.id = metadata_rows.id;
		INSERT INTO items
			(id, library_id, parent_id, name, sort_name, type, is_folder, local_metadata) VALUES
			('hidden-series-a', 'library-a', 'library-a', 'Hidden Series', 'Hidden Series', 'Series', true,
			'{"Name":"Hidden series metadata","Overview":"Hidden series overview"}'::jsonb),
			('cross-ancestor-episode-b', 'library-b', 'hidden-series-a', 'Visible Episode', 'Visible Episode', 'Episode', false,
			'{"Name":"Visible episode metadata","Overview":"Visible episode overview"}'::jsonb)`); err != nil {
		t.Fatalf("seed latest metadata fixtures: %v", err)
	}
	wantMetadata := map[string]metadata.Metadata{
		"album-b":                  {Name: "Album metadata", Overview: "Album overview"},
		"series-b":                 {Name: "Series metadata", Overview: "Series overview"},
		"audio-b1":                 {Name: "First audio metadata", Overview: "First audio overview"},
		"audio-b2":                 {Name: "Second audio metadata", Overview: "Second audio overview"},
		"episode-b1":               {Name: "First episode metadata", Overview: "First episode overview"},
		"episode-b2":               {Name: "Second episode metadata", Overview: "Second episode overview"},
		"movie-b":                  {Name: "Visible movie metadata", Overview: "Visible movie overview"},
		"movie-a":                  {Name: "Hidden movie metadata", Overview: "Hidden movie overview"},
		"cross-library-child":      {Name: "Hidden episode metadata", Overview: "Hidden episode overview"},
		"cross-ancestor-episode-b": {Name: "Visible episode metadata", Overview: "Visible episode overview"},
	}
	mediaIDs := []string{"audio-b1", "audio-b2", "episode-b1", "episode-b2", "movie-b", "movie-a", "cross-library-child"}
	tests := []struct {
		name  string
		query Query
		group bool
		want  []latestExpectation
	}{
		{
			name:  "raw results retain each source media metadata",
			query: Query{UserID: "restricted", Ids: mediaIDs},
			want:  []latestExpectation{{"audio-b1", 1}, {"episode-b1", 1}, {"movie-b", 1}, {"episode-b2", 1}, {"audio-b2", 1}},
		},
		{
			name:  "groups use container metadata rather than matching child metadata",
			query: Query{UserID: "restricted", Ids: mediaIDs},
			group: true,
			want:  []latestExpectation{{"album-b", 2}, {"movie-b", 1}, {"series-b", 2}},
		},
		{
			name:  "a representative outside the parent scope retains its own metadata",
			query: Query{UserID: "restricted", ParentID: "season-b"},
			group: true,
			want:  []latestExpectation{{"series-b", 2}},
		},
		{
			name:  "hidden media metadata is absent from raw results",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "cross-library-child"}},
			want:  []latestExpectation{},
		},
		{
			name:  "hidden media metadata is absent from grouped results",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "cross-library-child"}},
			group: true,
			want:  []latestExpectation{},
		},
		{
			name:  "authorized cross-library fallback retains source metadata",
			query: Query{UserID: "default", Ids: []string{"movie-a", "cross-library-child"}},
			group: true,
			want:  []latestExpectation{{"movie-a", 1}, {"cross-library-child", 1}},
		},
		{
			name:  "a visible source cannot expose hidden ancestor metadata",
			query: Query{UserID: "restricted", Ids: []string{"cross-ancestor-episode-b", "hidden-series-a"}},
			group: true,
			want:  []latestExpectation{{"cross-ancestor-episode-b", 1}},
		},
		{
			name:  "access to both libraries does not substitute cross-library ancestor metadata",
			query: Query{UserID: "default", Ids: []string{"cross-ancestor-episode-b", "hidden-series-a"}},
			group: true,
			want:  []latestExpectation{{"cross-ancestor-episode-b", 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := store.QueryLatest(ctx, test.query, test.group)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, items, test.want)
			for _, latest := range items {
				want := wantMetadata[latest.Item.ID]
				if latest.Item.Metadata == nil || !reflect.DeepEqual(*latest.Item.Metadata, want) {
					t.Errorf("latest item %s metadata = %+v, want %+v", latest.Item.ID, latest.Item.Metadata, want)
				}
			}
		})
	}
	t.Run("containers without metadata do not inherit matching child metadata", func(t *testing.T) {
		if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata =
			CASE WHEN id = 'series-b' THEN NULL ELSE 'null'::jsonb END
			WHERE id IN ('series-b', 'album-b')`); err != nil {
			t.Fatal(err)
		}
		items, err := store.QueryLatest(ctx, Query{UserID: "restricted", Ids: mediaIDs}, true)
		if err != nil {
			t.Fatal(err)
		}
		assertLatestItems(t, items, []latestExpectation{{"album-b", 2}, {"movie-b", 1}, {"series-b", 2}})
		for _, latest := range items {
			if latest.Item.IsFolder && latest.Item.Metadata != nil {
				t.Errorf("latest container %s metadata = %+v, want nil", latest.Item.ID, latest.Item.Metadata)
			}
		}
	})
}

func TestQueryLatestProjectsSourceAndRepresentativeEntities(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	entityIDs := seedLibraryLatestEntityFixture(t, ctx, store.pool)
	entityRef := func(kind, normalizedName, displayName string) EntityRef {
		return EntityRef{ID: latestFixtureEntityID(t, entityIDs, kind, normalizedName), Name: displayName}
	}
	personRef := func(normalizedName, displayName, role, creditType string, sortOrder *int) PersonRef {
		return PersonRef{
			ID:   strconv.FormatInt(latestFixtureEntityID(t, entityIDs, "Person", normalizedName), 10),
			Name: displayName, Role: role, Type: creditType, SortOrder: sortOrder,
		}
	}
	zero, one, two, three := 0, 1, 2, 3
	wantEntities := map[string]ItemEntities{
		"episode-b1": {
			Artists: []EntityRef{}, AlbumArtists: []EntityRef{},
			Genres:  []EntityRef{entityRef("Genre", "mystery", "Mystery"), entityRef("Genre", "drama", "dRaMa")},
			Tags:    []EntityRef{entityRef("Tag", "alpha", "alpha"), entityRef("Tag", "zulu", "Zulu")},
			Studios: []EntityRef{entityRef("Studio", "z studio", "Z Studio"), entityRef("Studio", "a studio", "A Studio")},
			People: []PersonRef{
				personRef("alice", "ALICE", "Lead", "Actor", &zero),
				personRef("bob", "Bob", "Narrator", "Actor", &two),
				personRef("alice", "ALICE", "Director", "Director", nil),
			},
		},
		"audio-b1": {
			Artists: []EntityRef{}, AlbumArtists: []EntityRef{},
			Genres:  []EntityRef{entityRef("Genre", "music", "Music")},
			Tags:    []EntityRef{entityRef("Tag", "live", "Live")},
			Studios: []EntityRef{entityRef("Studio", "audio studio", "Audio Studio")},
			People:  []PersonRef{personRef("performer", "Performer", "Vocals", "Artist", &three)},
		},
		"series-b": {
			Artists: []EntityRef{}, AlbumArtists: []EntityRef{},
			Genres:  []EntityRef{entityRef("Genre", "series only", "Series Only")},
			Tags:    []EntityRef{entityRef("Tag", "series tag", "Series Tag")},
			Studios: []EntityRef{entityRef("Studio", "series studio", "Series Studio")},
			People:  []PersonRef{personRef("series creator", "Series Creator", "Showrunner", "Writer", &one)},
		},
		"album-b": {
			Artists: []EntityRef{}, AlbumArtists: []EntityRef{},
			Genres:  []EntityRef{entityRef("Genre", "album only", "Album Only")},
			Tags:    []EntityRef{entityRef("Tag", "album tag", "Album Tag")},
			Studios: []EntityRef{entityRef("Studio", "album studio", "Album Studio")},
			People:  []PersonRef{personRef("album creator", "Album Creator", "Composer", "Composer", nil)},
		},
		"movie-a": {
			Artists: []EntityRef{}, AlbumArtists: []EntityRef{},
			Genres:  []EntityRef{entityRef("Genre", "drama", "Drama")},
			Tags:    []EntityRef{entityRef("Tag", "hidden tag", "Hidden Tag")},
			Studios: []EntityRef{entityRef("Studio", "hidden studio", "Hidden Studio")},
			People: []PersonRef{
				personRef("alice", "Alice", "Hidden Lead", "Actor", &zero),
				personRef("hidden person", "Hidden Person", "Secret", "Actor", nil),
			},
		},
	}
	mediaIDs := []string{"episode-b1", "audio-b1", "movie-a", "cross-library-child"}
	tests := []struct {
		name  string
		query Query
		group bool
		want  []latestExpectation
	}{
		{
			name:  "raw media retain their own association values and order",
			query: Query{UserID: "restricted", Ids: mediaIDs},
			want:  []latestExpectation{{"audio-b1", 1}, {"episode-b1", 1}},
		},
		{
			name:  "groups use representative entities without merging child entities",
			query: Query{UserID: "restricted", Ids: mediaIDs},
			group: true,
			want:  []latestExpectation{{"album-b", 1}, {"series-b", 1}},
		},
		{
			name:  "hidden entities do not expose raw media",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "cross-library-child"}},
			want:  []latestExpectation{},
		},
		{
			name:  "hidden entities do not expose grouped media",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "cross-library-child"}},
			group: true,
			want:  []latestExpectation{},
		},
		{
			name:  "authorized users can read the hidden fixture associations",
			query: Query{UserID: "default", Ids: []string{"movie-a"}},
			want:  []latestExpectation{{"movie-a", 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := store.QueryLatest(ctx, test.query, test.group)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, items, test.want)
			for _, latest := range items {
				want := wantEntities[latest.Item.ID]
				if !reflect.DeepEqual(latest.Item.Entities, want) {
					t.Errorf("latest item %s entities = %+v, want %+v", latest.Item.ID, latest.Item.Entities, want)
				}
			}
		})
	}
	t.Run("representatives without associations do not inherit child entities", func(t *testing.T) {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities(id, NULL::jsonb)
			FROM items WHERE id IN ('series-b', 'album-b')`); err != nil {
			t.Fatal(err)
		}
		items, err := store.QueryLatest(ctx, Query{UserID: "restricted", Ids: mediaIDs}, true)
		if err != nil {
			t.Fatal(err)
		}
		assertLatestItems(t, items, []latestExpectation{{"album-b", 1}, {"series-b", 1}})
		for _, latest := range items {
			entities := latest.Item.Entities
			if len(entities.Genres)+len(entities.Tags)+len(entities.Studios)+len(entities.People) != 0 {
				t.Errorf("latest representative %s entities = %+v, want no associations", latest.Item.ID, entities)
			}
		}
	})
}

func TestQueryLatestFiltersEntitiesBeforeGroupingCountingAndPaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	entityIDs := seedLibraryLatestEntityFixture(t, ctx, store.pool)
	dramaID := latestFixtureEntityID(t, entityIDs, "Genre", "drama")
	mysteryID := latestFixtureEntityID(t, entityIDs, "Genre", "mystery")
	aliceID := strconv.FormatInt(latestFixtureEntityID(t, entityIDs, "Person", "alice"), 10)

	tests := []struct {
		name  string
		query Query
		group bool
		want  []latestExpectation
	}{
		{
			name: "genre and person IDs filter source media before grouping and counting",
			query: Query{UserID: "restricted", GenreIds: []int64{dramaID}, PersonIds: []string{aliceID},
				PersonTypes: []string{"Actor"}},
			group: true,
			want:  []latestExpectation{{"movie-b", 1}, {"series-b", 1}, {"series-b2", 1}},
		},
		{
			name: "ACL and entity filters precede the grouped page",
			query: Query{UserID: "restricted", GenreIds: []int64{dramaID}, PersonIds: []string{aliceID},
				PersonTypes: []string{"Actor"}, StartIndex: 1, Limit: 1},
			group: true,
			want:  []latestExpectation{{"series-b", 1}},
		},
		{
			name: "person type must match the selected person on the same credit",
			query: Query{UserID: "restricted", Genres: []string{"drama"}, Person: "aLiCe",
				PersonTypes: []string{"Actor"}},
			want: []latestExpectation{{"episode-b1", 1}, {"movie-b", 1}, {"episode-b3", 1}},
		},
		{
			name: "multiple genre and person matches retain one raw media item",
			query: Query{UserID: "restricted", Ids: []string{"episode-b1"},
				GenreIds: []int64{dramaID, mysteryID}, PersonIds: []string{aliceID}},
			want: []latestExpectation{{"episode-b1", 1}},
		},
		{
			name: "multiple genre and person matches count the source only once",
			query: Query{UserID: "restricted", Ids: []string{"episode-b1"},
				GenreIds: []int64{dramaID, mysteryID}, PersonIds: []string{aliceID}},
			group: true,
			want:  []latestExpectation{{"series-b", 1}},
		},
		{
			name: "album grouping uses the matching audio credit and genre",
			query: Query{UserID: "restricted", Genres: []string{"Music"}, Person: "Performer",
				PersonTypes: []string{"Artist"}},
			group: true,
			want:  []latestExpectation{{"album-b", 1}},
		},
		{
			name:  "a hidden-only person filter cannot bypass library access",
			query: Query{UserID: "restricted", Genres: []string{"Drama"}, Person: "Hidden Person"},
			group: true,
			want:  []latestExpectation{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := store.QueryLatest(ctx, test.query, test.group)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, items, test.want)
		})
	}
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

func seedLibraryLatestEntityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	seedLibraryLatestFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO catalog_entities (kind, name) VALUES
		('Genre', 'Drama'), ('Person', 'Alice'), ('Tag', 'Alpha');
		UPDATE items SET local_metadata = metadata_rows.metadata::jsonb
		FROM (VALUES
			('episode-b1', '{"Genres":["Mystery","dRaMa"],"Tags":["Zulu","alpha"],"Studios":["Z Studio","A Studio"],
				"People":[{"Name":"Bob","Role":"Narrator","Type":"Actor","SortOrder":2},
				{"Name":"ALICE","Role":"Lead","Type":"Actor","SortOrder":0},{"Name":"ALICE","Role":"Director","Type":"Director"}]}'),
			('episode-b2', '{"Genres":["Drama"],"People":[{"Name":"Alice","Type":"Director"},{"Name":"Bob","Type":"Actor"}]}'),
			('episode-b3', '{"Genres":["Drama"],"People":[{"Name":"Alice","Type":"Actor"}]}'),
			('audio-b1', '{"Genres":["Music"],"Tags":["Live"],"Studios":["Audio Studio"],
				"People":[{"Name":"Performer","Role":"Vocals","Type":"Artist","SortOrder":3}]}'),
			('audio-b2', '{"Genres":["Music"],"People":[{"Name":"Performer","Type":"Composer"}]}'),
			('movie-b', '{"Genres":["Drama"],"People":[{"Name":"Alice","Type":"Actor"}]}'),
			('series-b', '{"Genres":["Series Only"],"Tags":["Series Tag"],"Studios":["Series Studio"],
				"People":[{"Name":"Series Creator","Role":"Showrunner","Type":"Writer","SortOrder":1}]}'),
			('album-b', '{"Genres":["Album Only"],"Tags":["Album Tag"],"Studios":["Album Studio"],
				"People":[{"Name":"Album Creator","Role":"Composer","Type":"Composer"}]}'),
			('movie-a', '{"Genres":["Drama"],"Tags":["Hidden Tag"],"Studios":["Hidden Studio"],
				"People":[{"Name":"Alice","Role":"Hidden Lead","Type":"Actor","SortOrder":0},{"Name":"Hidden Person","Role":"Secret","Type":"Actor"}]}'),
			('cross-library-child', '{"Genres":["Drama"],"People":[{"Name":"Alice","Type":"Actor"}]}')
		) AS metadata_rows(id, metadata) WHERE items.id = metadata_rows.id;
		SELECT sync_catalog_item_entities(id, local_metadata) FROM items
		WHERE local_metadata IS NOT NULL ORDER BY id`); err != nil {
		t.Fatalf("seed latest entity fixtures: %v", err)
	}
	rows, err := pool.Query(ctx, "SELECT kind || ':' || normalized_name, id FROM catalog_entities")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := make(map[string]int64)
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			t.Fatal(err)
		}
		ids[key] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func latestFixtureEntityID(t *testing.T, ids map[string]int64, kind, normalizedName string) int64 {
	t.Helper()
	id, ok := ids[kind+":"+normalizedName]
	if !ok || id <= 0 {
		t.Fatalf("missing %s entity ID for %s", kind, normalizedName)
	}
	return id
}
