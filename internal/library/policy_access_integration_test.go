package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestCatalogPolicyRestrictsCountsDirectReadsAndInheritedResources(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"OfficialRating":"TV-MA","Tags":["Adults"]}' WHERE id='series-b';
		UPDATE items SET local_metadata='{"OfficialRating":"PG","Tags":["Family"]}' WHERE id='movie-b';
		SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id IN ('series-b','movie-b')`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		policy  map[string]any
		visible []string
		denied  string
	}{
		{"excluded ancestor", map[string]any{"ExcludedSubFolders": []string{"series-b"}}, []string{"movie-b", "audio-b"}, "episode-b1"},
		{"parental ceiling inherits series", map[string]any{"MaxParentalRating": 7}, []string{"movie-b", "audio-b"}, "episode-b1"},
		{"blocked ancestor tag", map[string]any{"BlockedTags": []string{"aDuLtS"}}, []string{"movie-b", "audio-b"}, "episode-b1"},
		{"include tags", map[string]any{"IncludeTags": []string{"Family"}}, []string{"movie-b"}, "episode-b1"},
		{"inclusive legacy tags", map[string]any{"BlockedTags": []string{"Family"}, "IsTagBlockingModeInclusive": true}, []string{"movie-b"}, "episode-b1"},
		{"unrated music", map[string]any{"BlockUnratedItems": []string{"Music"}}, []string{"episode-b1", "episode-b2", "movie-b", "season-b", "series-b"}, "audio-b"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.policy["EnableAllFolders"] = false
			test.policy["EnabledFolders"] = []string{"library-b"}
			raw, err := json.Marshal(test.policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, "UPDATE users SET policy=$1 WHERE id='restricted'", raw); err != nil {
				t.Fatal(err)
			}
			result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true})
			if err != nil || result.TotalRecordCount != len(test.visible) || !reflect.DeepEqual(queryItemIDs(result.Items), test.visible) {
				t.Fatalf("authorized result=%+v err=%v, want %v", result, err, test.visible)
			}
			page, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, StartIndex: 999, Limit: 1})
			if err != nil || page.TotalRecordCount != len(test.visible) || len(page.Items) != 0 {
				t.Fatalf("distant page leaked count: %+v %v", page, err)
			}
			if _, err := store.GetItem(ctx, "restricted", test.denied); !errors.Is(err, ErrNotFound) {
				t.Fatalf("direct read error=%v", err)
			}
			if _, err := store.GetItem(ctx, "restricted", test.visible[0]); err != nil {
				t.Fatalf("allowed item disappeared: %v", err)
			}
			batch, err := store.GetItemsByIDFor(ctx, Subject{UserID: "restricted"}, []string{test.denied, test.visible[0]})
			if err != nil || len(batch) != 1 || batch[0].ID != test.visible[0] {
				t.Fatalf("direct batch leaked denied item: %+v %v", batch, err)
			}
			if _, err := store.ListImages(ctx, "restricted", test.denied); !errors.Is(err, ErrNotFound) {
				t.Fatalf("image metadata did not conceal denied item: %v", err)
			}
			if _, err := store.SetFavorite(ctx, "restricted", test.denied, true); !errors.Is(err, ErrNotFound) {
				t.Fatalf("user state write bypassed policy: %v", err)
			}
		})
	}
}

func TestParentalTagsCombineWithoutOverridingExplicitDenials(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"OfficialRating":"R","Tags":["Family","Blocked"]}' WHERE id='movie-b';
		SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id='movie-b';
		UPDATE users SET policy='{"EnableAllFolders":true,"MaxParentalRating":5,"IncludeTags":["Family"],"AllowTagOrRating":true}' WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, "default", "movie-b"); err != nil {
		t.Fatalf("tag grant did not satisfy OR policy: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy||'{"BlockedTags":["Blocked"]}' WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, "default", "movie-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("allow tag bypassed explicit block: %v", err)
	}
}

func TestFolderUserDataCountsExcludeParentalHiddenDescendants(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"OfficialRating":"TV-MA"}' WHERE id='episode-b2';
		UPDATE users SET policy='{"MaxParentalRating":7}' WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	item, err := store.GetItem(ctx, "default", "series-b")
	if err != nil || item.UserData == nil || item.UserData.UnplayedItemCount == nil || *item.UserData.UnplayedItemCount != 1 {
		t.Fatalf("folder summary leaked hidden episode: %+v %v", item.UserData, err)
	}
	if _, err := store.SetPlayed(ctx, "default", "series-b", true, nil); err != nil {
		t.Fatal(err)
	}
	var hiddenWritten bool
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_item_data WHERE user_id='default' AND item_id='episode-b2')`).Scan(&hiddenWritten); err != nil || hiddenWritten {
		t.Fatalf("folder write changed hidden descendant: %t %v", hiddenWritten, err)
	}
}
