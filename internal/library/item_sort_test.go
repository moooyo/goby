package library

import (
	"errors"
	"reflect"
	"testing"
)

func TestMusicItemSortNormalizesObservedOrderedFields(t *testing.T) {
	query, err := normalizeItemSort(Query{UserID: "viewer", SortBy: "ProductionYear,PremiereDate,SortName", SortOrder: "Descending,Descending,Ascending"})
	if err != nil || query.SortBy != "ProductionYear,PremiereDate,SortName" || query.SortOrder != "DESC,DESC,ASC" {
		t.Fatalf("observed ordered sort was not preserved: %+v, %v", query, err)
	}
	for _, value := range []Query{
		{SortBy: "Unknown"}, {SortBy: "SortName,SortName"}, {SortBy: "Name,"},
		{SortBy: "Name,DateCreated,SortName", SortOrder: "Ascending,Descending"},
		{SortBy: "Name", SortOrder: "Ascending,Descending"}, {SortBy: "Name", SortOrder: "sideways"},
		{SortBy: "DatePlayed"}, {SortBy: "PlayCount"},
	} {
		if _, err := normalizeItemSort(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("an invalid or unscoped personal sort was accepted")
		}
	}
}

func TestMusicItemSortUsesEffectiveMetadataAndCurrentUserHistory(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,local_metadata) VALUES
		('sort-album-a','library-b','library-b','A Album','a album','MusicAlbum',true,'{"ProductionYear":2025,"PremiereDate":"2025-01-01T00:00:00Z"}'),
		('sort-album-b','library-b','library-b','B Album','b album','MusicAlbum',true,'{"ProductionYear":2025,"PremiereDate":"2025-02-01T00:00:00Z"}'),
		('sort-album-c','library-b','library-b','C Album','c album','MusicAlbum',true,'{"ProductionYear":2026,"PremiereDate":"2024-01-01T00:00:00Z"}'),
		('sort-album-empty','library-b','library-b','Empty Album','empty album','MusicAlbum',true,NULL),
		('sort-album-hidden','library-a','library-a','Hidden Album','hidden album','MusicAlbum',true,'{"ProductionYear":9999}'),
		('sort-track-a','library-b','sort-album-a','A Track','a track','Audio',false,NULL),
		('sort-track-b','library-b','sort-album-a','B Track','b track','Audio',false,NULL),
		('sort-track-empty','library-b','sort-album-a','Empty Track','empty track','Audio',false,NULL);
		INSERT INTO user_item_data(user_id,item_id,play_count,last_played_at) VALUES
		('restricted','sort-track-a',5,'2026-01-01T00:00:00Z'),('restricted','sort-track-b',2,'2024-01-01T00:00:00Z'),
		('default','sort-track-a',1,'2024-01-01T00:00:00Z'),('default','sort-track-b',10,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal("seed real metadata and personal sort facts")
	}
	query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"MusicAlbum"},
		SortBy: "ProductionYear,PremiereDate,SortName", SortOrder: "Descending,Descending,Ascending"}
	result, err := store.QueryItems(ctx, query)
	want := []string{"sort-album-c", "sort-album-b", "sort-album-a", "sort-album-empty"}
	if err != nil || result.TotalRecordCount != len(want) || !reflect.DeepEqual(queryItemIDs(result.Items), want) {
		t.Fatalf("multi-field metadata sort ignored actual dates, years, or ACL: %+v, %v", result, err)
	}
	for offset, id := range want {
		query.StartIndex, query.Limit = offset, 1
		page, err := store.QueryItems(ctx, query)
		if err != nil || page.TotalRecordCount != len(want) || len(page.Items) != 1 || page.Items[0].ID != id {
			t.Fatalf("metadata sorting was not applied before paging: %+v, %v", page, err)
		}
	}
	for _, sortBy := range []string{"DatePlayed", "PlayCount"} {
		for _, user := range []struct{ id, first, second string }{
			{"restricted", "sort-track-a", "sort-track-b"}, {"default", "sort-track-b", "sort-track-a"},
		} {
			result, err := store.QueryItems(ctx, Query{UserID: user.id, ParentID: "sort-album-a", IncludeItemTypes: []string{"Audio"}, SortBy: sortBy, SortOrder: "Descending"})
			if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), []string{user.first, user.second, "sort-track-empty"}) {
				t.Fatalf("personal music sort used another user's state or lost empty history: %+v, %v", result, err)
			}
		}
	}
}
