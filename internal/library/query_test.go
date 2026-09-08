package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/metadata"
)

func TestLibraryPolicyDefaultsAndRestrictions(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		all     bool
		folders []string
		denied  bool
	}{
		{name: "missing flag grants all", policy: `{}`, all: true, folders: []string{}},
		{name: "explicit all ignores restricted folders", policy: `{"EnableAllFolders":true,"EnabledFolders":17}`, all: true, folders: []string{}},
		{name: "restricted folders", policy: `{"EnableAllFolders":false,"EnabledFolders":["library-b"]}`, folders: []string{"library-b"}},
		{name: "missing folders grants none", policy: `{"EnableAllFolders":false}`, folders: []string{}},
		{name: "null folders grants none", policy: `{"EnableAllFolders":false,"EnabledFolders":null}`, folders: []string{}},
		{name: "string boolean is rejected", policy: `{"EnableAllFolders":"false"}`, denied: true},
		{name: "null boolean is rejected", policy: `{"EnableAllFolders":null}`, denied: true},
		{name: "invalid folder list is rejected", policy: `{"EnableAllFolders":false,"EnabledFolders":[17]}`, denied: true},
		{name: "null policy is rejected", policy: `null`, denied: true},
		{name: "array policy is rejected", policy: `[]`, denied: true},
		{name: "invalid JSON is rejected", policy: `{`, denied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access, err := parseLibraryPolicy([]byte(test.policy))
			if test.denied {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("parse policy error = %v, want ErrForbidden", err)
				}
				return
			}
			if err != nil || access.all != test.all || !reflect.DeepEqual(access.folders, test.folders) {
				t.Fatalf("parse policy = %+v, %v, want all %v and folders %v", access, err, test.all, test.folders)
			}
		})
	}
}

func TestQueryItemsRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	negativeParentIndex := -1
	excessiveParentIndex := 1 << 31
	tests := []struct {
		name  string
		query Query
	}{
		{name: "missing user", query: Query{}},
		{name: "negative start", query: Query{UserID: "user", StartIndex: -1}},
		{name: "negative limit", query: Query{UserID: "user", Limit: -1}},
		{name: "excessive limit", query: Query{UserID: "user", Limit: 1001}},
		{name: "unknown sort", query: Query{UserID: "user", SortBy: "Random"}},
		{name: "sort injection", query: Query{UserID: "user", SortBy: "Name; DROP TABLE items"}},
		{name: "invalid sort order", query: Query{UserID: "user", SortOrder: "DESC NULLS FIRST"}},
		{name: "unknown item type", query: Query{UserID: "user", IncludeItemTypes: []string{"unknown"}}},
		{name: "unknown media type", query: Query{UserID: "user", MediaTypes: []string{"unknown"}}},
		{name: "empty ID", query: Query{UserID: "user", Ids: []string{""}}},
		{name: "null search", query: Query{UserID: "user", SearchTerm: "invalid\x00query"}},
		{name: "negative parent index", query: Query{UserID: "user", ParentIndexNumber: &negativeParentIndex}},
		{name: "excessive parent index", query: Query{UserID: "user", ParentIndexNumber: &excessiveParentIndex}},
	}
	store := &Store{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.QueryItems(context.Background(), test.query); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("query error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestQueryItemsValidatesEntityFiltersBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	assertInvalid := func(t *testing.T, query Query) {
		t.Helper()
		if _, err := store.QueryItems(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("entity query error = %v, want ErrInvalidInput", err)
		}
	}
	for _, field := range []struct {
		name string
		set  func(*Query, []int64)
	}{
		{name: "GenreIds", set: func(q *Query, values []int64) { q.GenreIds = values }},
		{name: "TagIds", set: func(q *Query, values []int64) { q.TagIds = values }},
		{name: "StudioIds", set: func(q *Query, values []int64) { q.StudioIds = values }},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, id := range []int64{0, -1} {
				query := Query{UserID: "user"}
				field.set(&query, []int64{id})
				assertInvalid(t, query)
			}
			values := make([]int64, 1025)
			for i := range values {
				values[i] = 1
			}
			query := Query{UserID: "user"}
			field.set(&query, values)
			assertInvalid(t, query)
			field.set(&query, values[:1024])
			if _, err := normalizeItemQuery(query); err != nil {
				t.Fatalf("1024 entity IDs error = %v", err)
			}
		})
	}
	for _, field := range []struct {
		name  string
		valid string
		set   func(*Query, []string)
	}{
		{name: "Genres", valid: "Drama", set: func(q *Query, values []string) { q.Genres = values }},
		{name: "Tags", valid: "Shared", set: func(q *Query, values []string) { q.Tags = values }},
		{name: "Studios", valid: "Studio", set: func(q *Query, values []string) { q.Studios = values }},
		{name: "PersonTypes", valid: "Actor", set: func(q *Query, values []string) { q.PersonTypes = values }},
		{name: "PersonIds", valid: "1", set: func(q *Query, values []string) { q.PersonIds = values }},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []string{"", " \t\n", "\u00a0", "bad\x00value", "bad\xffvalue"} {
				query := Query{UserID: "user"}
				field.set(&query, []string{value})
				assertInvalid(t, query)
			}
			values := make([]string, 1025)
			for i := range values {
				values[i] = field.valid
			}
			query := Query{UserID: "user"}
			field.set(&query, values)
			assertInvalid(t, query)
			field.set(&query, values[:1024])
			if _, err := normalizeItemQuery(query); err != nil {
				t.Fatalf("1024 entity values error = %v", err)
			}
		})
	}
	for _, value := range []string{" \t\n", "\u00a0", "bad\x00name", "bad\xffname"} {
		assertInvalid(t, Query{UserID: "user", Person: value})
	}
	for _, value := range []string{"0", "-1", "+1", "01", " 1", "1 ", "1.0", "one", "9223372036854775808", "\uff11"} {
		t.Run("PersonIds/"+value, func(t *testing.T) {
			assertInvalid(t, Query{UserID: "user", PersonIds: []string{value}})
		})
	}
}

func TestNormalizeItemQueryPreservesEntityNamesForDatabaseNormalization(t *testing.T) {
	query := Query{
		UserID: "user", Genres: []string{" \u00a0DRAMA\u00a0 "}, Tags: []string{" MiXeD Tag "},
		Studios: []string{" \u00a0Studio\u00a0 "}, Person: " \u00a0Person\u00a0 ",
		GenreIds: []int64{1<<63 - 1}, TagIds: []int64{1<<63 - 1}, StudioIds: []int64{1<<63 - 1},
		PersonIds: []string{"9223372036854775807"},
	}
	normalized, err := normalizeItemQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized.Genres, query.Genres) || !reflect.DeepEqual(normalized.Tags, query.Tags) ||
		!reflect.DeepEqual(normalized.Studios, query.Studios) || normalized.Person != query.Person {
		t.Fatalf("entity names were rewritten before SQL normalization: %+v", normalized)
	}
	if !reflect.DeepEqual(normalized.PersonIds, query.PersonIds) {
		t.Fatalf("maximum person ID = %v, want %v", normalized.PersonIds, query.PersonIds)
	}
}

func TestQueryItemsFiltersParentIndexAndOrdersAcrossSeasons(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_index_number = 1, index_number = 9 WHERE id = 'episode-b1';
		UPDATE items SET parent_index_number = 2, index_number = 1 WHERE id = 'episode-b2'`); err != nil {
		t.Fatal(err)
	}
	query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Episode"}, SortBy: "IndexNumber"}
	for _, test := range []struct {
		order string
		ids   []string
	}{
		{order: "Ascending", ids: []string{"episode-b1", "episode-b2"}},
		{order: "Descending", ids: []string{"episode-b2", "episode-b1"}},
	} {
		query.SortOrder = test.order
		result, err := store.QueryItems(ctx, query)
		if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), test.ids) {
			t.Fatalf("season order %s = %+v, %v, want %v", test.order, result, err, test.ids)
		}
	}
	for _, test := range []struct {
		index int
		ids   []string
	}{
		{index: 0, ids: []string{}},
		{index: 1, ids: []string{"episode-b1"}},
		{index: 2, ids: []string{"episode-b2"}},
		{index: 1<<31 - 1, ids: []string{}},
	} {
		query.ParentIndexNumber = &test.index
		result, err := store.QueryItems(ctx, query)
		if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), test.ids) || result.TotalRecordCount != len(test.ids) {
			t.Fatalf("parent index %d = %+v, %v, want %v", test.index, result, err, test.ids)
		}
	}
}

func TestQueryItemsAppliesLibraryPolicyBeforePagination(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)

	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Limit: 1, StartIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRecordCount != 6 || len(result.Items) != 1 || result.Items[0].ID != "episode-b2" {
		t.Fatalf("restricted page = %+v, want second visible item and visible total 6", result)
	}
	result, err = store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Limit: 2, StartIndex: 99})
	if err != nil || result.TotalRecordCount != 6 || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty page = %+v, %v, want empty items with visible total 6", result, err)
	}
	for _, test := range []struct {
		user string
		ids  []string
	}{
		{user: "restricted", ids: []string{"library-b"}},
		{user: "default", ids: []string{"library-a", "library-b", "library-c"}},
		{user: "admin", ids: []string{"library-a", "library-b", "library-c"}},
		{user: "none", ids: []string{}},
	} {
		t.Run(test.user, func(t *testing.T) {
			result, err := store.QueryItems(ctx, Query{UserID: test.user})
			if err != nil {
				t.Fatal(err)
			}
			if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, test.ids) || result.TotalRecordCount != len(test.ids) {
				t.Fatalf("root items = %v, total = %d, want %v", got, result.TotalRecordCount, test.ids)
			}
			libraries, err := store.ListUserLibraries(ctx, test.user)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(libraries))
			for _, library := range libraries {
				ids = append(ids, library.ID)
				if len(library.Paths) != 1 || library.Paths[0] == "" {
					t.Errorf("library %s paths = %v, want configured root", library.ID, library.Paths)
				}
			}
			if !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("user libraries = %v, want %v", ids, test.ids)
			}
		})
	}
}

func TestQueryItemsFiltersAndSortsVisibleItems(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	tests := []struct {
		name  string
		query Query
		ids   []string
	}{
		{
			name:  "IDs override root scope without bypassing ACL",
			query: Query{UserID: "restricted", Ids: []string{"movie-a", "episode-b1", "library-b"}},
			ids:   []string{"episode-b1", "library-b"},
		},
		{
			name:  "item types ignore case",
			query: Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"ePiSoDe"}},
			ids:   []string{"episode-b1", "episode-b2"},
		},
		{
			name:  "literal wildcard search",
			query: Query{UserID: "restricted", Recursive: true, SearchTerm: "%_"},
			ids:   []string{"episode-b2"},
		},
		{
			name:  "case insensitive substring search",
			query: Query{UserID: "restricted", Recursive: true, SearchTerm: "bEgInN"},
			ids:   []string{"episode-b1"},
		},
		{
			name:  "audio media filter",
			query: Query{UserID: "restricted", Recursive: true, MediaTypes: []string{"aUdIo"}},
			ids:   []string{"audio-b"},
		},
		{
			name:  "video filter excludes folders",
			query: Query{UserID: "restricted", Recursive: true, MediaTypes: []string{"Video"}},
			ids:   []string{"episode-b1", "episode-b2", "movie-b"},
		},
		{
			name:  "descending episode index",
			query: Query{UserID: "restricted", ParentID: "season-b", SortBy: "indexnumber", SortOrder: "Descending"},
			ids:   []string{"episode-b2", "episode-b1"},
		},
		{
			name:  "name descending",
			query: Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Episode"}, SortBy: "Name", SortOrder: "desc"},
			ids:   []string{"episode-b2", "episode-b1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, test.ids) || result.TotalRecordCount != len(test.ids) {
				t.Fatalf("query items = %v, total = %d, want %v", got, result.TotalRecordCount, test.ids)
			}
		})
	}
}

func TestQueryItemsRecursionStaysWithinAuthorizedParentLibrary(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, userID := range []string{"restricted", "default", "admin"} {
		t.Run(userID, func(t *testing.T) {
			result, err := store.QueryItems(ctx, Query{UserID: userID, ParentID: "series-b", Recursive: true})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"episode-b1", "episode-b2", "season-b"}
			if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, want) || result.TotalRecordCount != 3 {
				t.Fatalf("recursive items = %v, total = %d, want %v", got, result.TotalRecordCount, want)
			}
			result, err = store.QueryItems(ctx, Query{UserID: userID, ParentID: "season-b"})
			if err != nil || result.TotalRecordCount != 2 {
				t.Fatalf("direct children = %+v, %v, want two same-library children", result, err)
			}
		})
	}
	for _, parentID := range []string{"library-a", "movie-a", "missing"} {
		if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: parentID, Recursive: true}); !errors.Is(err, ErrNotFound) {
			t.Errorf("query inaccessible parent %s error = %v, want ErrNotFound", parentID, err)
		}
	}
	// A corrupt cycle must terminate and must not include the requested parent.
	if _, err := store.pool.Exec(ctx, "UPDATE items SET parent_id = 'episode-b1' WHERE id = 'series-b'"); err != nil {
		t.Fatal(err)
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "series-b", Recursive: true})
	if err != nil || result.TotalRecordCount != 3 {
		t.Fatalf("cyclic tree query = %+v, %v, want three descendants", result, err)
	}
}

func TestItemReadsEnforceActiveUserPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	item, err := store.GetItem(ctx, "restricted", "episode-b1")
	if err != nil || item.ID != "episode-b1" || item.Media == nil || item.Media.DurationTicks != 15000000 {
		t.Fatalf("visible media item = %+v, %v, want stored media metadata", item, err)
	}
	for _, id := range []string{"movie-a", "cross-library-child", "missing"} {
		if _, err := store.GetItem(ctx, "restricted", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("read inaccessible item %s error = %v, want ErrNotFound", id, err)
		}
	}
	for _, userID := range []string{"disabled", "unknown", "malformed"} {
		t.Run(userID, func(t *testing.T) {
			if _, err := store.QueryItems(ctx, Query{UserID: userID, Recursive: true}); !errors.Is(err, ErrForbidden) {
				t.Errorf("query error = %v, want ErrForbidden", err)
			}
			if _, err := store.GetItem(ctx, userID, "episode-b1"); !errors.Is(err, ErrForbidden) {
				t.Errorf("item read error = %v, want ErrForbidden", err)
			}
			if _, err := store.ListUserLibraries(ctx, userID); !errors.Is(err, ErrForbidden) {
				t.Errorf("library list error = %v, want ErrForbidden", err)
			}
		})
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false}'::jsonb WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, "restricted", "episode-b1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read item after policy revocation error = %v, want ErrNotFound", err)
	}
}

func TestItemQueriesReturnLocalMetadataWithinLibraryPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata = '{
		"Kind":"episodedetails","Name":"Local Episode","SortName":"Episode Local",
		"OriginalTitle":"Original Episode","Overview":"A local episode overview.",
		"OfficialRating":"TV-PG","ProductionYear":2024,"PremiereDate":"2024-01-02T03:04:05Z",
		"IndexNumber":0,"ParentIndexNumber":0,"CommunityRating":8.5,
		"ProviderIDs":{"Imdb":"tt1234567","Tvdb":"7654321"},
		"Genres":["Drama","Mystery"],"Tags":["Local"],"Studios":["Example Studio"],
		"People":[{"Name":"Example Actor","Role":"Lead","Type":"Actor","SortOrder":0}]
	}'::jsonb, local_metadata_hash = repeat('a', 64), local_metadata_path = '/private/episode.nfo'
		WHERE id = 'episode-b1';
		UPDATE items SET local_metadata = '{"Kind":"movie","Name":"Private Movie","Overview":"Hidden metadata"}'::jsonb,
			local_metadata_hash = repeat('b', 64), local_metadata_path = '/private/movie.nfo' WHERE id = 'movie-a'`); err != nil {
		t.Fatal(err)
	}
	year, zero, rating := 2024, 0, 8.5
	premiere := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	want := &metadata.Metadata{
		Kind: "episodedetails", Name: "Local Episode", SortName: "Episode Local",
		OriginalTitle: "Original Episode", Overview: "A local episode overview.",
		OfficialRating: "TV-PG", ProductionYear: &year, PremiereDate: &premiere,
		IndexNumber: &zero, ParentIndexNumber: &zero, CommunityRating: &rating,
		ProviderIDs: map[string]string{"Imdb": "tt1234567", "Tvdb": "7654321"},
		Genres:      []string{"Drama", "Mystery"}, Tags: []string{"Local"}, Studios: []string{"Example Studio"},
		People: []metadata.Person{{Name: "Example Actor", Role: "Lead", Type: "Actor", SortOrder: &zero}},
	}
	query := Query{UserID: "restricted", Ids: []string{"movie-a", "episode-b1"}}
	result, err := store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != "episode-b1" {
		t.Fatalf("metadata query = %+v, %v, want one visible episode", result, err)
	}
	if !reflect.DeepEqual(result.Items[0].Metadata, want) {
		t.Errorf("query metadata = %+v, want %+v", result.Items[0].Metadata, want)
	}
	item, err := store.GetItem(ctx, "restricted", "episode-b1")
	if err != nil || !reflect.DeepEqual(item.Metadata, want) {
		t.Fatalf("item metadata = %+v, %v, want %+v", item.Metadata, err, want)
	}
	if item.Media == nil || item.Media.DurationTicks != 15000000 {
		t.Errorf("local metadata changed the media projection: %+v", item.Media)
	}
	if _, err := store.GetItem(ctx, "restricted", "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden metadata item error = %v, want ErrNotFound", err)
	}
	private, err := store.GetItem(ctx, "admin", "movie-a")
	if err != nil || private.Metadata == nil || private.Metadata.Name != "Private Movie" {
		t.Errorf("administrator metadata = %+v, %v, want private movie metadata", private.Metadata, err)
	}
	// Hidden rows must be excluded before their metadata is decoded.
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata = '{"Name":17}'::jsonb WHERE id = 'movie-a'`); err != nil {
		t.Fatal(err)
	}
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 || !reflect.DeepEqual(result.Items[0].Metadata, want) {
		t.Fatalf("hidden invalid metadata affected visible results: %+v, %v", result, err)
	}
	if _, err := store.GetItem(ctx, "restricted", "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden invalid metadata error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetItem(ctx, "admin", "movie-a"); err == nil {
		t.Error("invalid visible metadata must not be returned as a successful item")
	}
}

func TestItemQueriesPreserveAbsentAndNullLocalMetadata(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata = 'null'::jsonb WHERE id = 'movie-b'`); err != nil {
		t.Fatal(err)
	}
	ids := []string{"episode-b2", "movie-b"}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids})
	if err != nil || result.TotalRecordCount != 2 || len(result.Items) != 2 {
		t.Fatalf("absent metadata query = %+v, %v, want two items", result, err)
	}
	for _, item := range result.Items {
		if item.Metadata != nil {
			t.Errorf("item %s metadata = %+v, want nil", item.ID, item.Metadata)
		}
	}
	for _, id := range ids {
		item, err := store.GetItem(ctx, "restricted", id)
		if err != nil || item.Metadata != nil {
			t.Errorf("read item %s metadata = %+v, %v, want nil metadata", id, item.Metadata, err)
		}
	}
}

func TestItemQueriesProjectEntityDisplayNamesAndCreditOrder(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	entityID := func(kind, name string) int64 {
		return libraryQueryEntityID(t, ctx, store.pool, kind, name)
	}
	sharedPersonID := strconv.FormatInt(entityID("Person", "Shared Person"), 10)
	otherPersonID := strconv.FormatInt(entityID("Person", "Other Person"), 10)
	eight, zero, one := 8, 0, 1
	want := ItemEntities{
		Genres: []EntityRef{
			{ID: entityID("Genre", "Zeta"), Name: "Zeta"},
			{ID: entityID("Genre", "Drama"), Name: "dRAMA"},
			{ID: entityID("Genre", "Alpha"), Name: "Alpha"},
		},
		Tags: []EntityRef{
			{ID: entityID("Tag", "Alpha"), Name: "Alpha"},
			{ID: entityID("Tag", "Shared"), Name: "sHARED"},
			{ID: entityID("Tag", "z-last"), Name: "z-last"},
		},
		Studios: []EntityRef{
			{ID: entityID("Studio", "Z Studio"), Name: "Z Studio"},
			{ID: entityID("Studio", "Studio One"), Name: "sTUDIO One"},
		},
		People: []PersonRef{
			{ID: otherPersonID, Name: "Other Person", Role: "Director", Type: "Director", SortOrder: &zero},
			{ID: sharedPersonID, Name: "SHARED PERSON", Role: "Narrator", Type: "GuestStar", SortOrder: &one},
			{ID: sharedPersonID, Name: "sHARED Person", Role: "Lead", Type: "Actor", SortOrder: &eight},
			{ID: sharedPersonID, Name: "sHARED Person", Role: "Cameo", Type: "Actor"},
		},
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{"movie-a", "episode-b1"}})
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 {
		t.Fatalf("entity projection query = %+v, %v, want one visible item", result, err)
	}
	if !reflect.DeepEqual(result.Items[0].Entities, want) {
		t.Errorf("query entities = %+v, want %+v", result.Items[0].Entities, want)
	}
	item, err := store.GetItem(ctx, "restricted", "episode-b1")
	if err != nil || !reflect.DeepEqual(item.Entities, want) {
		t.Fatalf("item entities = %+v, %v, want %+v", item.Entities, err, want)
	}
	for _, id := range []string{"movie-a", "cross-library-child"} {
		if _, err := store.GetItem(ctx, "restricted", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("hidden entity item %s error = %v, want ErrNotFound", id, err)
		}
	}
	item, err = store.GetItem(ctx, "restricted", "audio-b")
	if err != nil || len(item.Entities.Genres)+len(item.Entities.Tags)+len(item.Entities.Studios)+len(item.Entities.People) != 0 {
		t.Fatalf("unassociated item entities = %+v, %v, want empty collections", item.Entities, err)
	}
}

func TestQueryItemsCombinesEntityFiltersWithinVisibleItems(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	dramaID := libraryQueryEntityID(t, ctx, store.pool, "Genre", "Drama")
	comedyID := libraryQueryEntityID(t, ctx, store.pool, "Genre", "Comedy")
	sharedTagID := libraryQueryEntityID(t, ctx, store.pool, "Tag", "Shared")
	studioOneID := libraryQueryEntityID(t, ctx, store.pool, "Studio", "Studio One")
	sharedPersonID := strconv.FormatInt(libraryQueryEntityID(t, ctx, store.pool, "Person", "Shared Person"), 10)
	tests := []struct {
		name  string
		query Query
		ids   []string
	}{
		{name: "genre IDs browse beyond roots", query: Query{GenreIds: []int64{dramaID}}, ids: []string{"episode-b1"}},
		{name: "genre names use SQL normalization", query: Query{Genres: []string{" dRaMa "}}, ids: []string{"episode-b1"}},
		{name: "genre names are alternatives", query: Query{Genres: []string{"Drama", "Comedy"}}, ids: []string{"episode-b1", "episode-b2"}},
		{name: "genre IDs are alternatives", query: Query{GenreIds: []int64{dramaID, comedyID}}, ids: []string{"episode-b1", "episode-b2"}},
		{name: "genre ID and name constraints intersect", query: Query{GenreIds: []int64{dramaID}, Genres: []string{"Comedy"}}, ids: []string{}},
		{name: "genre constraints can match different associations", query: Query{GenreIds: []int64{dramaID}, Genres: []string{"Alpha"}}, ids: []string{"episode-b1"}},
		{name: "tag ID and name constraints intersect", query: Query{TagIds: []int64{sharedTagID}, Tags: []string{" movieonly "}}, ids: []string{}},
		{name: "tag constraints can match different associations", query: Query{TagIds: []int64{sharedTagID}, Tags: []string{"Alpha"}}, ids: []string{"episode-b1"}},
		{name: "studio ID and name constraints intersect", query: Query{StudioIds: []int64{studioOneID}, Studios: []string{"studio two"}}, ids: []string{}},
		{name: "studio constraints can match different associations", query: Query{StudioIds: []int64{studioOneID}, Studios: []string{"z studio"}}, ids: []string{"episode-b1"}},
		{name: "person IDs do not duplicate items", query: Query{PersonIds: []string{sharedPersonID}}, ids: []string{"episode-b1", "episode-b2"}},
		{name: "person ID and name must identify the same person", query: Query{Person: " Other Person ", PersonIds: []string{sharedPersonID}}, ids: []string{}},
		{
			name: "different entity dimensions intersect",
			query: Query{Genres: []string{"Drama", "Comedy"}, TagIds: []int64{sharedTagID},
				StudioIds: []int64{studioOneID}, PersonIds: []string{sharedPersonID}},
			ids: []string{"episode-b1"},
		},
		{name: "different dimensions cannot match different items", query: Query{Genres: []string{"Comedy"}, Studios: []string{"Studio One"}}, ids: []string{}},
		{name: "person name and type share an association", query: Query{Person: "shared person", PersonTypes: []string{"Director"}}, ids: []string{"episode-b2"}},
		{name: "person ID and type share an association", query: Query{PersonIds: []string{sharedPersonID}, PersonTypes: []string{"Director"}}, ids: []string{"episode-b2"}},
		{name: "person name ID and type share an association", query: Query{Person: "Shared Person", PersonIds: []string{sharedPersonID}, PersonTypes: []string{"Director"}}, ids: []string{"episode-b2"}},
		{name: "person type cannot bridge different name and ID matches", query: Query{Person: "Other Person", PersonIds: []string{sharedPersonID}, PersonTypes: []string{"Director"}}, ids: []string{}},
		{name: "person types alone browse beyond roots", query: Query{PersonTypes: []string{"Actor"}}, ids: []string{"episode-b1", "movie-b"}},
		{name: "person type alternatives retain all matching credits", query: Query{Person: "Shared Person", PersonTypes: []string{"Actor", "Director"}}, ids: []string{"episode-b1", "episode-b2"}},
		{name: "later credit is preserved", query: Query{PersonIds: []string{sharedPersonID}, PersonTypes: []string{"GuestStar"}}, ids: []string{"episode-b1"}},
		{name: "SQL trim preserves nonbreaking spaces", query: Query{Genres: []string{" \u00a0Spaced Genre\u00a0 "}}, ids: []string{"movie-b"}},
		{name: "nonbreaking spaces remain part of the entity name", query: Query{Genres: []string{"Spaced Genre"}}, ids: []string{}},
		{name: "genre ID cannot match a tag", query: Query{GenreIds: []int64{sharedTagID}}, ids: []string{}},
		{name: "tag ID cannot match a genre", query: Query{TagIds: []int64{dramaID}}, ids: []string{}},
		{name: "studio ID cannot match a genre", query: Query{StudioIds: []int64{dramaID}}, ids: []string{}},
		{name: "person ID cannot match a studio", query: Query{PersonIds: []string{strconv.FormatInt(studioOneID, 10)}}, ids: []string{}},
		{name: "explicit IDs still constrain entity filters", query: Query{Ids: []string{"movie-a", "episode-b2"}, Tags: []string{"Shared"}}, ids: []string{"episode-b2"}},
		{name: "explicit parent still constrains entity filters", query: Query{ParentID: "library-b", Genres: []string{"Drama"}}, ids: []string{}},
		{name: "recursive parent stays in its library", query: Query{ParentID: "series-b", Recursive: true, Genres: []string{"Drama"}}, ids: []string{"episode-b1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.query.UserID = "restricted"
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			if got := queryItemIDs(result.Items); !reflect.DeepEqual(got, test.ids) || result.TotalRecordCount != len(test.ids) {
				t.Fatalf("entity query items = %v, total = %d, want %v", got, result.TotalRecordCount, test.ids)
			}
		})
	}
}

func TestQueryItemsAppliesEntityACLBeforeCountsAndPagination(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	sharedPersonID := strconv.FormatInt(libraryQueryEntityID(t, ctx, store.pool, "Person", "Shared Person"), 10)
	query := Query{UserID: "restricted", Tags: []string{"Shared"}, PersonIds: []string{sharedPersonID}, StartIndex: 1, Limit: 1}
	result, err := store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 2 || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"episode-b2"}) {
		t.Fatalf("entity page = %+v, %v, want second visible item and total 2", result, err)
	}
	query.StartIndex = 99
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 2 || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty entity page = %+v, %v, want empty items and visible total 2", result, err)
	}
	query.StartIndex, query.Limit = 0, 10
	query.UserID = "admin"
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 4 || len(result.Items) != 4 {
		t.Fatalf("administrator entity query = %+v, %v, want all four associated items", result, err)
	}
	query.UserID = "none"
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("denied entity query = %+v, %v, want no items", result, err)
	}
	query.UserID = "restricted"
	query.Tags, query.PersonIds = nil, nil
	query.Genres = []string{"Private Genre"}
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("hidden-only entity query = %+v, %v, want no items", result, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false}'::jsonb WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	query.Genres = []string{"Drama"}
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("revoked entity query = %+v, %v, want no items", result, err)
	}
	if _, err := store.GetItem(ctx, "restricted", "episode-b1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked entity item error = %v, want ErrNotFound", err)
	}
}

func seedLibraryEntityQueryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedLibraryQueryFixture(t, ctx, pool)
	for _, fixture := range []struct {
		itemID   string
		metadata string
	}{
		{itemID: "movie-a", metadata: `{
			"Genres":["Drama","Private Genre"],"Tags":["Shared","Private Tag"],"Studios":["Studio One"],
			"People":[{"Name":"Shared Person","Role":"Hidden","Type":"Actor"}]}`},
		{itemID: "episode-b1", metadata: `{
			"Genres":["Zeta","dRAMA","Alpha"],"Tags":["z-last","sHARED","Alpha"],"Studios":["Z Studio","sTUDIO One"],
			"People":[
				{"Name":"sHARED Person","Role":"Lead","Type":"Actor","SortOrder":8},
				{"Name":"Other Person","Role":"Director","Type":"Director","SortOrder":0},
				{"Name":"SHARED PERSON","Role":"Narrator","Type":"GuestStar","SortOrder":1},
				{"Name":"sHARED Person","Role":"Cameo","Type":"Actor"}]}`},
		{itemID: "episode-b2", metadata: `{
			"Genres":["Comedy"],"Tags":["Shared","EpisodeOnly"],"Studios":["Studio Two"],
			"People":[{"Name":"Shared Person","Type":"Director"}]}`},
		{itemID: "movie-b", metadata: `{
			"Genres":["Mystery","\u00a0Spaced Genre\u00a0"],"Tags":["MovieOnly"],"Studios":["Studio One"],
			"People":[{"Name":"Other Person","Type":"Actor"}]}`},
		{itemID: "cross-library-child", metadata: `{
			"Genres":["Drama"],"Tags":["Shared"],"Studios":["Studio One"],
			"People":[{"Name":"Shared Person","Type":"Actor"}]}`},
	} {
		if _, err := pool.Exec(ctx, `WITH changed AS (
			UPDATE items SET local_metadata = $2::jsonb WHERE id = $1 RETURNING id, local_metadata
		) SELECT sync_catalog_item_entities(id, local_metadata) FROM changed`, fixture.itemID, fixture.metadata); err != nil {
			t.Fatalf("seed item %s entities: %v", fixture.itemID, err)
		}
	}
}

func libraryQueryEntityID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, kind, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM catalog_entities
		WHERE kind = $1 AND normalized_name = lower(btrim($2::text))`, kind, name).Scan(&id); err != nil {
		t.Fatalf("read %s entity %q: %v", kind, name, err)
	}
	return id
}

func queryItemIDs(items []Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// libraryQueryTestStore owns a fresh schema and never changes existing schemas.
func libraryQueryTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(admin.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate query test schema name: %v", err)
	}
	schema := "goby_library_query_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create isolated query test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned query test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse query test database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated query test pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate query test database: %v", err)
	}
	return ctx, &Store{pool: pool}
}

func seedLibraryQueryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, is_administrator, is_disabled, policy) VALUES
		('admin', 'Admin', 'admin', 'fixture-only', true, false, '{"EnableAllFolders":false}'),
		('default', 'Default', 'default', 'fixture-only', false, false, '{}'),
		('restricted', 'Restricted', 'restricted', 'fixture-only', false, false, '{"EnableAllFolders":false,"EnabledFolders":["library-b"]}'),
		('none', 'None', 'none', 'fixture-only', false, false, '{"EnableAllFolders":false}'),
		('disabled', 'Disabled', 'disabled', 'fixture-only', true, true, '{}'),
		('malformed', 'Malformed', 'malformed', 'fixture-only', false, false, '{"EnableAllFolders":"false"}');
		INSERT INTO libraries (id, name, collection_type) VALUES
		('library-a', 'A Hidden', 'movies'), ('library-b', 'B Visible', 'mixed'), ('library-c', 'C Hidden', 'mixed');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path) VALUES
		('root-a', 'library-a', '/media/a', '/media', 'a'),
		('root-b', 'library-b', '/media/b', '/media', 'b'),
		('root-c', 'library-c', '/media/c', '/media', 'c');
		INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder) VALUES
		('library-a', 'library-a', NULL, 'A Hidden', 'A Hidden', 'CollectionFolder', true),
		('library-b', 'library-b', NULL, 'B Visible', 'B Visible', 'CollectionFolder', true),
		('library-c', 'library-c', NULL, 'C Hidden', 'C Hidden', 'CollectionFolder', true),
		('movie-a', 'library-a', 'library-a', '0 Hidden Movie', '0 Hidden Movie', 'Movie', false),
		('series-b', 'library-b', 'library-b', 'Z Series', 'Z Series', 'Series', true),
		('season-b', 'library-b', 'series-b', 'Y Season', 'Y Season', 'Season', true),
		('episode-b1', 'library-b', 'season-b', 'A Beginning', 'A Beginning', 'Episode', false),
		('episode-b2', 'library-b', 'season-b', 'B 100%_Final', 'B 100%_Final', 'Episode', false),
		('movie-b', 'library-b', 'library-b', 'C Movie', 'C Movie', 'Movie', false),
		('audio-b', 'library-b', 'library-b', 'D Song', 'D Song', 'Audio', false),
		('cross-library-child', 'library-c', 'season-b', '0 Cross Library', '0 Cross Library', 'Episode', false);
		UPDATE items SET index_number = 1, media = '{"DurationTicks":15000000,"Container":"mkv"}'::jsonb WHERE id = 'episode-b1';
		UPDATE items SET index_number = 2 WHERE id = 'episode-b2'`); err != nil {
		t.Fatalf("seed query fixtures: %v", err)
	}
}
