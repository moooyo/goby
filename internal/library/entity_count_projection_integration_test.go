package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type entityCountProjectionCase struct {
	name, kind, family     string
	sharedCount, zuluCount int
}

func entityCountProjectionCases() []entityCountProjectionCase {
	return []entityCountProjectionCase{
		{name: "genres", kind: "Genre", sharedCount: 5, zuluCount: 2},
		{name: "tags", kind: "Tag", sharedCount: 2, zuluCount: 1},
		{name: "studios", kind: "Studio", sharedCount: 2, zuluCount: 1},
		{name: "people", kind: "Person", sharedCount: 2, zuluCount: 1},
		{name: "music artists", family: "artists", sharedCount: 3, zuluCount: 1},
		{name: "all music artists", family: "allartists", sharedCount: 3, zuluCount: 2},
		{name: "album artists", family: "albumartists", sharedCount: 2, zuluCount: 1},
		{name: "music genres", family: "genres", sharedCount: 3, zuluCount: 1},
	}
}

func (test entityCountProjectionCase) list(ctx context.Context, store *Store, query Query) (EntityResult, error) {
	if test.family != "" {
		return store.ListMusicEntities(ctx, test.family, query)
	}
	return store.ListEntities(ctx, test.kind, query)
}

func (test entityCountProjectionCase) count(ctx context.Context, store *Store, query Query) (EntityResult, error) {
	if test.family != "" {
		return store.CountMusicEntities(ctx, test.family, query)
	}
	return store.CountEntities(ctx, test.kind, query)
}

func seedEntityCountProjectionFixture(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('projection-album','library-b','library-b','Projection Album','projection album','MusicAlbum',true),
		('projection-own','library-b','projection-album','Own Track','own track','Audio',false),
		('projection-fallback','library-b','projection-album','Fallback Track','fallback track','Audio',false),
		('projection-hidden','library-a','library-a','Hidden Track','hidden track','Audio',false)`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, metadata string }{
		{"movie-b", `{"Genres":["Projection Shared","Projection Zulu"],"Tags":["Projection Shared","Projection Zulu"],"Studios":["Projection Shared","Projection Zulu"],"People":[{"Name":"Projection Shared","Type":"Actor","Role":"Lead"},{"Name":"Projection Shared","Type":"Director"},{"Name":"Projection Zulu","Type":"Actor"}]}`},
		{"episode-b1", `{"Genres":["Projection Shared"],"Tags":["Projection Shared"],"Studios":["Projection Shared"],"People":[{"Name":"Projection Shared","Type":"Actor"}]}`},
		{"movie-a", `{"Genres":["Projection Shared","Projection Hidden"],"Tags":["Projection Shared","Projection Hidden"],"Studios":["Projection Shared","Projection Hidden"],"People":[{"Name":"Projection Shared","Type":"Actor"},{"Name":"Projection Hidden","Type":"Actor"}]}`},
		{"projection-album", `{"Genres":["Projection Shared","Projection Zulu"],"Artists":["Projection Shared","Projection Zulu"],"AlbumArtists":["Projection Shared"]}`},
		{"projection-own", `{"Genres":["Projection Shared"],"Artists":["Projection Shared"],"AlbumArtists":["Projection Zulu"]}`},
		{"projection-fallback", `{"Genres":["Projection Shared"],"Artists":["Projection Shared"]}`},
		{"projection-hidden", `{"Genres":["Projection Shared","Projection Hidden"],"Artists":["Projection Shared","Projection Hidden"],"AlbumArtists":["Projection Shared","Projection Hidden"]}`},
	} {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,$2::jsonb)`, item.id, item.metadata); err != nil {
			t.Fatalf("index projection fixture %s: %v", item.id, err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO catalog_entities(kind,name)
		SELECT kind,'Projection Orphan' FROM unnest(ARRAY['Genre','Tag','Studio','Person','MusicArtist']) AS kinds(kind);
		INSERT INTO entity_user_data(user_id,entity_id,is_favorite)
		SELECT 'restricted',id,true FROM catalog_entities WHERE name='Projection Shared';
		INSERT INTO user_item_data(user_id,item_id,is_favorite)
		VALUES ('restricted','movie-b',true),('restricted','projection-album',true)`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"Genre", "Tag", "Studio", "Person", "MusicArtist"} {
		id := libraryQueryEntityID(t, ctx, store.pool, kind, "Projection Shared")
		searchHintTestArtwork(t, ctx, store, SearchHintReference{Kind: "Entity", ID: strconv.FormatInt(id, 10)}, strings.Repeat("a", 64))
	}
}

func entityCountProjectionSQLCount(trace *itemCountTracer, fragment string) int {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	count := 0
	for _, statement := range trace.statements {
		if strings.Contains(strings.ToLower(statement), strings.ToLower(fragment)) {
			count++
		}
	}
	return count
}

func TestEntityBrowseProjectionsPreserveSourceCountsAndIndependentAttachments(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedEntityCountProjectionFixture(t, ctx, fixture)
	actor := metadataEditTestActor(t, ctx, fixture.pool, "entity-projection-administrator")
	store, trace := countItemsTestStore(t, ctx, fixture)
	for _, endpoint := range entityCountProjectionCases() {
		t.Run(endpoint.name, func(t *testing.T) {
			query := Query{UserID: "restricted", SortBy: "Name", SortOrder: "Descending"}
			complete, err := endpoint.list(ctx, store, query)
			if err != nil || complete.TotalRecordCount != 2 || len(complete.Items) != 2 {
				t.Fatalf("complete entity fixture: %+v, %v", complete, err)
			}
			shared := entitiesNamed(t, complete, "Projection Shared")
			if shared.Count != endpoint.sharedCount || entitiesNamed(t, complete, "Projection Zulu").Count != endpoint.zuluCount ||
				shared.UserData == nil || !shared.UserData.IsFavorite || len(shared.Images) != 1 || complete.Items[0].Name != "Projection Zulu" {
				t.Fatalf("complete domain projection lost distinct sources, order, artwork or user state: %+v", complete)
			}
			for _, projection := range []QueryProjection{
				{EntitySourceCountsDisabled: true},
				{UserDataDisabled: true},
				{EntitySourceCountsDisabled: true, UserDataDisabled: true, ImagesDisabled: true},
			} {
				query.Projection = projection
				trace.reset()
				narrow, err := endpoint.list(ctx, store, query)
				if err != nil {
					t.Fatal(err)
				}
				want := complete
				want.Items = append([]Entity(nil), complete.Items...)
				for index := range want.Items {
					if projection.EntitySourceCountsDisabled {
						want.Items[index].Count = 0
					}
					if projection.UserDataDisabled {
						want.Items[index].UserData = nil
					}
					if projection.ImagesDisabled {
						want.Items[index].Images = []Image{}
					}
				}
				if !reflect.DeepEqual(narrow, want) {
					t.Fatalf("projection %+v changed retained entity fields: got=%+v want=%+v", projection, narrow, want)
				}
				if projection.EntitySourceCountsDisabled && entityCountProjectionSQLCount(trace, "count(DISTINCT i.id)") != 0 {
					t.Fatal("disabled source counts still requested distinct source aggregation")
				}
				if projection.UserDataDisabled && entityCountProjectionSQLCount(trace, "entity_user_data") != 0 {
					t.Fatal("disabled user data still loaded entity preferences")
				}
				if projection.ImagesDisabled {
					assertNoArtworkProjectionQueries(t, trace)
				}
			}
			favorite := true
			query.IsFavorite = &favorite
			query.Projection = QueryProjection{EntitySourceCountsDisabled: true, UserDataDisabled: true}
			trace.reset()
			favorites, err := endpoint.list(ctx, store, query)
			if err != nil || favorites.TotalRecordCount != 1 || len(favorites.Items) != 1 || favorites.Items[0].ID != shared.ID ||
				favorites.Items[0].UserData != nil || len(favorites.Items[0].Images) != 1 || favorites.Items[0].Count != 0 {
				t.Fatalf("narrow favorite projection lost entity state filtering or artwork: %+v, %v", favorites, err)
			}
			if entityCountProjectionSQLCount(trace, "entity_user_data") == 0 ||
				entityCountProjectionSQLCount(trace, "FROM entity_user_data WHERE user_id=") != 0 {
				t.Fatal("favorite filtering must retain its predicate without loading user-data attachments")
			}
			nativeQuery := Query{ParentID: "library-b"}
			var native EntityResult
			if endpoint.family == "" {
				native, err = store.QueryArtworkEntities(ctx, actor, endpoint.kind, nativeQuery)
			} else {
				native, err = store.QueryMusicMetadataEntities(ctx, actor, endpoint.family, nativeQuery)
			}
			if err != nil || native.TotalRecordCount != 2 || entitiesNamed(t, native, "Projection Shared").Count != endpoint.sharedCount ||
				entitiesNamed(t, native, "Projection Zulu").Count != endpoint.zuluCount {
				t.Fatalf("native management lost complete source counts: %+v, %v", native, err)
			}
			projection := QueryProjection{EntitySourceCountsDisabled: true, UserDataDisabled: true}
			subject := Subject{UserID: "restricted"}
			trace.reset()
			var detail Entity
			if endpoint.family == "" {
				detail, err = store.GetEntityFor(ctx, subject, endpoint.kind, shared.Name, projection)
			} else {
				detail, err = store.GetMusicEntityFor(ctx, subject, endpoint.family, shared.Name, projection)
			}
			shared.Count, shared.UserData = 0, nil
			if err != nil || !reflect.DeepEqual(detail, shared) || entityCountProjectionSQLCount(trace, "count(DISTINCT i.id)") != 0 ||
				entityCountProjectionSQLCount(trace, "entity_user_data") != 0 {
				t.Fatalf("detail did not honor the independent projection: %+v, %v", detail, err)
			}
			if endpoint.kind == "Genre" {
				byID, err := store.GetEntityByIDFor(ctx, subject, shared.ID, projection)
				if err != nil || !reflect.DeepEqual(byID, shared) {
					t.Fatalf("numeric entity detail changed identity or retained fields: %+v, %v", byID, err)
				}
			}
		})
	}
}

func TestEntityCountsPreserveFiltersWithoutReadingPagesOrAttachments(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedEntityCountProjectionFixture(t, ctx, fixture)
	key := seedCatalogApplicationKey(t, ctx, fixture.pool, "entity-count-key", true)
	store, trace := countItemsTestStore(t, ctx, fixture)
	favorite := true
	for _, endpoint := range entityCountProjectionCases() {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, test := range []struct {
				name  string
				query Query
				total int
			}{
				{name: "zero limit", query: Query{UserID: "restricted"}, total: 2},
				{name: "out of range", query: Query{UserID: "restricted", StartIndex: 999, Limit: 1}, total: 2},
				{name: "parent favorite", query: Query{UserID: "restricted", ParentID: "library-b", IsFavorite: &favorite,
					Projection: QueryProjection{UserDataDisabled: true, ImagesDisabled: true}}, total: 1},
				{name: "empty search", query: Query{UserID: "restricted", SearchTerm: "no matching projection entity"}},
				{name: "empty permission scope", query: Query{UserID: "none"}},
				{name: "userless key", query: Query{ApplicationCredentialID: key.ApplicationCredentialID}, total: 3},
				{name: "targeted key", query: Query{UserID: "restricted", ApplicationCredentialID: key.ApplicationCredentialID}, total: 2},
			} {
				t.Run(test.name, func(t *testing.T) {
					pageQuery := test.query
					pageQuery.Limit = 1
					page, err := endpoint.list(ctx, fixture, pageQuery)
					if err != nil || page.TotalRecordCount != test.total || test.query.StartIndex != 0 && len(page.Items) != 0 {
						t.Fatalf("authorized page baseline: %+v, %v; want total %d", page, err, test.total)
					}
					trace.reset()
					result, err := endpoint.count(ctx, store, test.query)
					if err != nil || result.TotalRecordCount != test.total || result.Items == nil || len(result.Items) != 0 {
						t.Fatalf("explicit count result: %+v, %v; want empty Items and total %d", result, err, test.total)
					}
					trace.assertCountWithoutPage(t)
					assertNoArtworkProjectionQueries(t, trace)
				})
			}
			if endpoint.family != "" {
				for _, itemID := range []string{"projection-own", "projection-fallback"} {
					query := Query{UserID: "restricted", ParentID: "projection-album", Ids: []string{itemID}}
					want := 1
					if endpoint.family == "allartists" && itemID == "projection-own" {
						want = 2
					}
					trace.reset()
					result, err := endpoint.count(ctx, store, query)
					if err != nil || result.TotalRecordCount != want || result.Items == nil || len(result.Items) != 0 {
						t.Fatalf("music role or physical-parent count changed for %s: %+v, %v", itemID, result, err)
					}
					trace.assertCountWithoutPage(t)
				}
			}
		})
	}
}

func TestEntityCountsRetainValidationAndSubjectAuthority(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedEntityCountProjectionFixture(t, ctx, fixture)
	key := seedCatalogApplicationKey(t, ctx, fixture.pool, "entity-count-authority-key", true)
	for _, endpoint := range []entityCountProjectionCase{{name: "entities", kind: "Genre"}, {name: "music", family: "albumartists"}} {
		t.Run(endpoint.name, func(t *testing.T) {
			favorite := true
			for _, test := range []struct {
				name  string
				query Query
				want  error
			}{
				{name: "negative limit", query: Query{UserID: "restricted", Limit: -1}, want: ErrInvalidInput},
				{name: "invalid sort", query: Query{UserID: "restricted", SortBy: "DateCreated"}, want: ErrInvalidInput},
				{name: "invalid music identifier", query: Query{UserID: "restricted", AlbumArtistIds: []int64{-1}}, want: ErrInvalidInput},
				{name: "hidden parent", query: Query{UserID: "restricted", ParentID: "library-a"}, want: ErrNotFound},
				{name: "missing parent", query: Query{UserID: "restricted", ParentID: "missing-projection-parent"}, want: ErrNotFound},
				{name: "disabled account", query: Query{UserID: "disabled"}, want: ErrForbidden},
				{name: "missing key target", query: Query{UserID: "missing-projection-target", ApplicationCredentialID: key.ApplicationCredentialID}, want: ErrNotFound},
				{name: "userless favorite", query: Query{ApplicationCredentialID: key.ApplicationCredentialID, IsFavorite: &favorite}, want: ErrInvalidInput},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := endpoint.count(ctx, fixture, test.query); !errors.Is(err, test.want) {
						t.Fatalf("count query bypassed validation or authority: got %v, want %v", err, test.want)
					}
				})
			}
		})
	}
	if _, err := fixture.CountEntities(ctx, "Unknown", Query{UserID: "restricted"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid entity kind was accepted: %v", err)
	}
	if _, err := fixture.CountMusicEntities(ctx, "Unknown", Query{UserID: "restricted"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid music family was accepted: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	query := Query{UserID: "restricted", ApplicationCredentialID: key.ApplicationCredentialID}
	if _, err := fixture.CountEntities(ctx, "Genre", query); !errors.Is(err, ErrForbidden) {
		t.Fatalf("entity count reused a revoked key: %v", err)
	}
	if _, err := fixture.CountMusicEntities(ctx, "albumartists", query); !errors.Is(err, ErrForbidden) {
		t.Fatalf("music count reused a revoked key: %v", err)
	}
}

func TestEntityListsKeepTheDefaultHundredRowPage(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	names := make([]string, 101)
	for index := range names {
		names[index] = fmt.Sprintf("Default page %03d", index)
	}
	if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities('audio-b',
		jsonb_build_object('Genres',to_jsonb($1::text[]),'Artists',to_jsonb($1::text[])))`, names); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []entityCountProjectionCase{{name: "entities", kind: "Genre"}, {name: "music", family: "allartists"}} {
		t.Run(endpoint.name, func(t *testing.T) {
			query := Query{UserID: "restricted", Projection: QueryProjection{ImagesDisabled: true, UserDataDisabled: true}}
			page, err := endpoint.list(ctx, store, query)
			if err != nil || page.TotalRecordCount != 101 || len(page.Items) != 100 || page.Items[0].Name != names[0] || page.Items[99].Name != names[99] {
				t.Fatalf("ordinary zero limit no longer defaults to 100 ordered entities: %+v, %v", page, err)
			}
			query.StartIndex = 100
			last, err := endpoint.list(ctx, store, query)
			if err != nil || last.TotalRecordCount != 101 || len(last.Items) != 1 || last.Items[0].Name != names[100] {
				t.Fatalf("default page offset changed the authorized total or last entity: %+v, %v", last, err)
			}
		})
	}
}
