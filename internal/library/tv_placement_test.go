package library

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/metadata"
)

func TestStandaloneSpecialUsesPlacementInsteadOfSeasonAlone(t *testing.T) {
	zero, one, two := 0, 1, 2
	for _, tc := range []struct {
		name string
		item Item
		want bool
	}{
		{"unplaced_special", Item{Type: "Episode", ParentIndexNumber: 0}, true},
		{"zero_season_placement", Item{Type: "Episode", Metadata: &metadata.Metadata{AirsBeforeSeasonNumber: &zero}}, true},
		{"episode_without_season", Item{Type: "Episode", Metadata: &metadata.Metadata{AirsBeforeEpisodeNumber: &two}}, true},
		{"before_season", Item{Type: "Episode", Metadata: &metadata.Metadata{AirsBeforeSeasonNumber: &one}}, false},
		{"after_season", Item{Type: "Episode", Metadata: &metadata.Metadata{AirsAfterSeasonNumber: &two}}, false},
		{"regular_episode", Item{Type: "Episode", ParentIndexNumber: 1}, false},
		{"season_folder", Item{Type: "Season"}, false},
		{"unrelated_video", Item{Type: "Video"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.IsStandaloneSpecial(); got != tc.want {
				t.Fatalf("standalone = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTelevisionMetadataEditValidationAndControls(t *testing.T) {
	for _, field := range []string{"AirsBeforeSeasonNumber", "AirsAfterSeasonNumber", "AirsBeforeEpisodeNumber"} {
		if !slices.Contains(editableMetadataFields("Episode"), field) || slices.Contains(editableMetadataFields("Movie"), field) {
			t.Fatalf("placement field %s has the wrong edit scope", field)
		}
		for _, raw := range []string{"null", "0", "2", "2147483647"} {
			if _, err := normalizeMetadataValue(field, json.RawMessage(raw)); err != nil {
				t.Fatalf("rejected valid %s=%s: %v", field, raw, err)
			}
		}
		for _, raw := range []string{"-1", "1.5", "2147483648", `"2"`} {
			if _, err := normalizeMetadataValue(field, json.RawMessage(raw)); err == nil {
				t.Fatalf("accepted invalid %s=%s", field, raw)
			}
		}
	}
	if !slices.Contains(editableMetadataFields("Series"), "Status") || slices.Contains(editableMetadataFields("Episode"), "Status") {
		t.Fatal("series status has the wrong edit scope")
	}
	if raw, err := normalizeMetadataValue("Status", json.RawMessage(`"ended"`)); err != nil || string(raw) != `"Ended"` {
		t.Fatalf("series status was not canonicalized: %s, %v", raw, err)
	}
	for _, raw := range []string{`"Unknown"`, `"Unreleased"`, "true", "1"} {
		if _, err := normalizeMetadataValue("Status", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted unsupported status %s", raw)
		}
	}
	automatic := []byte(`{"AirsBeforeSeasonNumber":2,"EndDate":"2025-04-05T00:00:00Z","Status":"Continuing"}`)
	values, _, err := composeMetadataValues(automatic,
		map[string]json.RawMessage{"AirsBeforeSeasonNumber": json.RawMessage("null"), "Status": json.RawMessage(`"Ended"`)},
		map[string]json.RawMessage{"EndDate": json.RawMessage(`"2025-06-07T00:00:00Z"`)})
	if err != nil || values.AirsBeforeSeasonNumber != nil || values.Status == nil || *values.Status != "Ended" || values.EndDate == nil || values.EndDate.Format("2006-01-02") != "2025-06-07" {
		t.Fatalf("manual and locked TV facts did not override the source: %#v, %v", values, err)
	}
}

func TestQueryStandaloneSpecialPlacementCountsAndMetadataEdits(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_index_number=1 WHERE id IN ('episode-b1','episode-b2');
		INSERT INTO items (id,library_id,parent_id,name,sort_name,type,is_folder,index_number,parent_index_number,local_metadata) VALUES
		('standalone-b','library-b','season-b','Standalone','a','Episode',false,1,0,'{}'),
		('before-b','library-b','season-b','Before','b','Episode',false,2,0,'{"AirsBeforeSeasonNumber":1,"AirsBeforeEpisodeNumber":2}'),
		('after-b','library-b','season-b','After','c','Episode',false,3,0,'{"AirsAfterSeasonNumber":1}'),
		('unknown-b','library-b','season-b','Unknown','d','Episode',false,4,0,'{"AirsBeforeEpisodeNumber":2}'),
		('video-b','library-b','season-b','Video','e','Video',false,0,0,'{}'),
		('standalone-a','library-a','library-a','Hidden','0','Episode',false,1,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query Query
		ids   []string
		total int
	}{
		{Query{IsStandaloneSpecial: tvFilterBool(true)}, []string{"standalone-b", "unknown-b"}, 2},
		{Query{IsStandaloneSpecial: tvFilterBool(true), StartIndex: 1, Limit: 1}, []string{"unknown-b"}, 2},
		{Query{IsStandaloneSpecial: tvFilterBool(true), StartIndex: 2, Limit: 1}, []string{}, 2},
		{Query{IsStandaloneSpecial: tvFilterBool(false), IsSpecialEpisode: tvFilterBool(true)}, []string{"before-b", "after-b"}, 2},
		{Query{IsStandaloneSpecial: tvFilterBool(false), Ids: []string{"video-b", "before-b", "standalone-b"}}, []string{"before-b", "video-b"}, 2},
	} {
		tc.query.UserID, tc.query.Recursive = "restricted", true
		result, err := store.QueryItems(ctx, tc.query)
		if err != nil || !reflect.DeepEqual(queryItemIDs(result.Items), tc.ids) || result.TotalRecordCount != tc.total {
			t.Fatalf("standalone query = %#v, %v; want %v / %d", result, err, tc.ids, tc.total)
		}
		for _, item := range result.Items {
			if item.IsStandaloneSpecial() != *tc.query.IsStandaloneSpecial {
				t.Fatalf("query predicate and item projection disagree for %s", item.ID)
			}
		}
	}
	actor := metadataEditTestActor(t, ctx, store.pool, "tv-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, "before-b")
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{"AirsBeforeSeasonNumber": json.RawMessage("null")}, nil)
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Ids: []string{"before-b"}, IsStandaloneSpecial: tvFilterBool(true)})
	if err != nil || result.TotalRecordCount != 1 {
		t.Fatalf("manual null did not clear effective placement before querying: %#v, %v", result, err)
	}
	metadataEditTestUpdate(t, ctx, store, actor, detail, nil, nil)
	result, err = store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Ids: []string{"before-b"}, IsStandaloneSpecial: tvFilterBool(false)})
	if err != nil || result.TotalRecordCount != 1 {
		t.Fatalf("removing the override did not restore NFO placement: %#v, %v", result, err)
	}
}
