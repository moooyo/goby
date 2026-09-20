package library

import (
	"reflect"
	"testing"
)

func TestQuerySuggestionsRanksActualScopedStateBeforeCountingAndPaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":1800000000,"Container":"mkv"}' WHERE id IN ('episode-b1','episode-b2','movie-b','audio-b','movie-a');
		UPDATE items SET created_at='2026-01-01T00:00:00Z' WHERE id='episode-b1';
		UPDATE items SET created_at='2026-02-01T00:00:00Z' WHERE id='episode-b2';
		UPDATE items SET created_at='2026-03-01T00:00:00Z' WHERE id='audio-b';
		UPDATE items SET created_at='2026-04-01T00:00:00Z' WHERE id='movie-b';
		INSERT INTO user_item_data(user_id,item_id,is_favorite,likes,played) VALUES
		('restricted','episode-b1',false,true,false),('restricted','episode-b2',true,false,true),
		('restricted','audio-b',false,false,false),('restricted','movie-a',true,true,false),
		('default','audio-b',true,true,false)`); err != nil {
		t.Fatal(err)
	}
	query := Query{UserID: "restricted", Recursive: true}
	want := []string{"episode-b1", "episode-b2", "movie-b", "audio-b"}
	result, err := store.QuerySuggestions(ctx, query)
	if err != nil || result.TotalRecordCount != len(want) || !reflect.DeepEqual(queryItemIDs(result.Items), want) {
		t.Fatalf("local suggestion rank = %+v, %v; want %v", result, err, want)
	}
	for offset, id := range want {
		query.StartIndex, query.Limit = offset, 1
		page, err := store.QuerySuggestions(ctx, query)
		if err != nil || page.TotalRecordCount != len(want) || len(page.Items) != 1 || page.Items[0].ID != id {
			t.Fatalf("suggestions paged before authorization or ranking: %+v, %v", page, err)
		}
	}
	query.StartIndex, query.Limit = 0, 100
	query.IsPlayed = discoveryBool(false)
	result, err = store.QuerySuggestions(ctx, query)
	if err != nil || result.TotalRecordCount != 3 || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"episode-b1", "movie-b", "audio-b"}) {
		t.Fatalf("unplayed suggestion predicate disagreed with count or rank: %+v, %v", result, err)
	}
	query.IsPlayed, query.Likes = discoveryBool(true), discoveryBool(false)
	result, err = store.QuerySuggestions(ctx, query)
	if err != nil || result.TotalRecordCount != 1 || result.Items[0].ID != "episode-b2" || result.Items[0].UserData == nil || !result.Items[0].UserData.Played {
		t.Fatal("explicit played/disliked selection did not project the same user's state")
	}
	query.IsPlayed, query.Likes = nil, nil
	query.SortBy, query.SortOrder = "Name", "Descending"
	result, err = store.QuerySuggestions(ctx, query)
	if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), []string{"audio-b", "movie-b", "episode-b2", "episode-b1"}) {
		t.Fatal("explicit item sort did not replace suggestion ranking")
	}
	query.UserID, query.SortBy, query.SortOrder = "default", "", ""
	query.Ids = []string{"episode-b1", "episode-b2", "movie-b", "audio-b"}
	result, err = store.QuerySuggestions(ctx, query)
	if err != nil || result.TotalRecordCount != 4 || result.Items[0].ID != "audio-b" || result.Items[0].UserData == nil || !result.Items[0].UserData.IsFavorite {
		t.Fatal("suggestions inherited another user's positive signals")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	query.UserID = "restricted"
	result, err = store.QuerySuggestions(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatal("suggestions retained revoked library access")
	}
}

func TestQuerySuggestionsExcludesMediaLessAndVirtualCatalogSelections(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, query := range []Query{
		{UserID: "restricted", Recursive: true, Ids: []string{"episode-b2", "movie-b", "audio-b", "series-b"}},
		{UserID: "restricted", Recursive: true, IsMissing: discoveryBool(true), DisplayMissingEpisodes: true},
		{UserID: "restricted", Recursive: true, IsPlaceHolder: discoveryBool(true), DisplayMissingEpisodes: true},
		{UserID: "restricted", Recursive: true, IsVirtualUnaired: discoveryBool(true), DisplayMissingEpisodes: true},
	} {
		result, err := store.QuerySuggestions(ctx, query)
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Fatalf("a nonplayable candidate entered suggestions: %+v, %v", result, err)
		}
	}
}
