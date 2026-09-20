package library

import (
	"reflect"
	"testing"
)

func discoveryBool(value bool) *bool { return &value }

func TestDiscoveryMissingPreferenceAndExplicitSelectors(t *testing.T) {
	for _, test := range []struct {
		name  string
		query Query
		want  bool
	}{
		{"absent", Query{}, false},
		{"preference", Query{DisplayMissingEpisodes: true}, true},
		{"missing_false_wins", Query{DisplayMissingEpisodes: true, IsMissing: discoveryBool(false)}, false},
		{"placeholder_false_wins", Query{DisplayMissingEpisodes: true, IsPlaceHolder: discoveryBool(false)}, false},
		{"explicit_missing", Query{IsMissing: discoveryBool(true)}, true},
		{"explicit_virtual_unaired", Query{IsVirtualUnaired: discoveryBool(true)}, true},
		{"explicit_unaired", Query{IsUnaired: discoveryBool(true)}, true},
		{"only_virtual_false", Query{IsVirtualUnaired: discoveryBool(false)}, false},
		{"display_non_future_virtual", Query{DisplayMissingEpisodes: true, IsVirtualUnaired: discoveryBool(false)}, true},
		{"conflicting_predicates_remain_empty", Query{IsMissing: discoveryBool(false), IsPlaceHolder: discoveryBool(true)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := includesExpectedEpisodes(test.query); got != test.want {
				t.Fatalf("expected population admission = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDiscoveryLikesDislikesAndFavoritesUseExplicitScopedState(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,is_favorite,likes) VALUES
		('restricted','episode-b1',false,true),('restricted','episode-b2',true,false),
		('restricted','audio-b',true,NULL),('restricted','movie-a',true,true),
		('default','episode-b1',false,false),('default','episode-b2',false,true)`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		query Query
		want  []string
	}{
		{"liked", Query{Likes: discoveryBool(true)}, []string{"episode-b1"}},
		{"disliked", Query{Likes: discoveryBool(false)}, []string{"episode-b2"}},
		{"favorite_or_liked", Query{IsFavoriteOrLikes: discoveryBool(true)}, []string{"episode-b1", "episode-b2", "audio-b"}},
		{"disliked_favorite", Query{Likes: discoveryBool(false), IsFavorite: discoveryBool(true)}, []string{"episode-b2"}},
		{"liked_nonfavorite", Query{Likes: discoveryBool(true), IsFavorite: discoveryBool(false)}, []string{"episode-b1"}},
		{"liked_favorite_empty", Query{Likes: discoveryBool(true), IsFavorite: discoveryBool(true)}, []string{}},
		{"other_user", Query{UserID: "default", Likes: discoveryBool(true)}, []string{"episode-b2"}},
		{"folder_intersection", Query{Likes: discoveryBool(true), IsFolder: discoveryBool(true)}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			if query.UserID == "" {
				query.UserID = "restricted"
			}
			query.Recursive = true
			result, err := store.QueryItems(ctx, query)
			if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
				t.Fatalf("scoped preference filter = %+v, %v; want %v", result, err, test.want)
			}
			for offset := 0; offset <= len(test.want); offset++ {
				query.StartIndex, query.Limit = offset, 1
				page, err := store.QueryItems(ctx, query)
				if err != nil || page.TotalRecordCount != len(test.want) {
					t.Fatalf("state filter count changed when paging: %+v, %v", page, err)
				}
				if offset < len(test.want) && (len(page.Items) != 1 || page.Items[0].ID != test.want[offset]) || offset == len(test.want) && len(page.Items) != 0 {
					t.Fatal("preference or ACL predicates were applied after pagination")
				}
			}
		})
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Likes: discoveryBool(true)})
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatal("a liked item bypassed current library revocation")
	}
}

func TestDiscoveryPhysicalUnairedRequiresExplicitFutureEpisodeDate(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"PremiereDate":"2999-01-01T00:00:00Z"}' WHERE id IN ('episode-b1','movie-b');
		UPDATE items SET local_metadata='{"PremiereDate":"2001-01-01T00:00:00Z"}' WHERE id='episode-b2'`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query Query
		want  []string
	}{
		{Query{IsUnaired: discoveryBool(true), IsMissing: discoveryBool(false)}, []string{"episode-b1"}},
		{Query{IsUnaired: discoveryBool(false), IncludeItemTypes: []string{"Episode"}}, []string{"episode-b2"}},
		{Query{IsMissing: discoveryBool(true)}, []string{}},
		{Query{IsPlaceHolder: discoveryBool(true)}, []string{}},
		{Query{IsVirtualUnaired: discoveryBool(true)}, []string{}},
	} {
		test.query.UserID, test.query.Recursive = "restricted", true
		result, err := store.QueryItems(ctx, test.query)
		if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
			t.Fatalf("physical future-date classification = %+v, %v; want %v", result, err, test.want)
		}
	}
}
