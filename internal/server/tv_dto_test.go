package server

import (
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func assertTVParentFields(t *testing.T, dto map[string]any, expected map[string]string) {
	t.Helper()
	for _, field := range []string{"SeriesId", "SeriesName", "SeasonId", "SeasonName"} {
		value, present := dto[field]
		want, required := expected[field]
		if present != required || required && value != want {
			t.Errorf("TV relationship %s = %#v (present=%v), want %q (present=%v)", field, value, present, want, required)
		}
	}
}

func TestTVParentDTOObservedRelationshipsAreDefaultFields(t *testing.T) {
	api := &Server{serverID: "server-id"}
	for _, kind := range []string{"Episode", "Season"} {
		item := library.Item{ID: "item-id", Type: kind, IsFolder: kind == "Season", ParentID: "parent-id",
			Name: "Catalog Item", IndexNumber: 1, ParentIndexNumber: 2,
			Series: &library.TVParentRef{ID: "series-id", Name: "Catalog Series"}}
		want := map[string]string{"SeriesId": "series-id", "SeriesName": "Catalog Series"}
		if kind == "Episode" {
			item.Season = &library.TVParentRef{ID: "season-id", Name: "Catalog Season"}
			want["SeasonId"], want["SeasonName"] = "season-id", "Catalog Season"
		}
		for _, projection := range []struct {
			name   string
			fields []string
			detail bool
		}{
			{name: "default_list"},
			{name: "unrelated_requested_fields", fields: []string{"UserData", "ParentId"}},
			{name: "explicit_parent_fields", fields: []string{"seriesid", "SeasonName"}},
			{name: "detail", detail: true},
		} {
			t.Run(kind+"/"+projection.name, func(t *testing.T) {
				dto, _ := metadataDTOJSON(t, api.itemDTO(item, projection.fields, projection.detail))
				assertTVParentFields(t, dto, want)
				if dto["Id"] != item.ID || dto["Name"] != item.Name || dto["ParentId"] != item.ParentID {
					t.Fatal("parent projection changed the item's own identity")
				}
			})
		}
	}
}

func TestTVParentDTODoesNotInventUnprovenOrInapplicableRelationships(t *testing.T) {
	series := &library.TVParentRef{ID: "series-id", Name: "Catalog Series"}
	season := &library.TVParentRef{ID: "season-id", Name: "Catalog Season"}
	for _, test := range []struct {
		name string
		item library.Item
		want map[string]string
	}{
		{name: "unresolved_episode", item: library.Item{Type: "Episode", ParentID: "unproven-season", ParentIndexNumber: 7}},
		{name: "unresolved_season", item: library.Item{Type: "Season", IsFolder: true, ParentID: "unproven-series"}},
		{name: "only_direct_season", item: library.Item{Type: "Episode", Season: season}, want: map[string]string{"SeasonId": "season-id", "SeasonName": "Catalog Season"}},
		{name: "direct_series_episode", item: library.Item{Type: "Episode", Series: series}, want: map[string]string{"SeriesId": "series-id", "SeriesName": "Catalog Series"}},
		{name: "series_has_no_parent_projection", item: library.Item{Type: "Series", IsFolder: true, Series: series, Season: season}},
		{name: "movie_has_no_tv_projection", item: library.Item{Type: "Movie", Series: series, Season: season}},
		{name: "audio_has_no_tv_projection", item: library.Item{Type: "Audio", Series: series, Season: season}},
		{name: "episode_folder_is_not_an_episode", item: library.Item{Type: "Episode", IsFolder: true, Series: series, Season: season}},
		{name: "season_leaf_is_not_a_season", item: library.Item{Type: "Season", Series: series}},
		{name: "empty_id_is_not_a_relationship", item: library.Item{Type: "Episode", Series: &library.TVParentRef{Name: "Unproven Series"}, Season: &library.TVParentRef{Name: "Unproven Season"}}},
		{name: "season_never_inherits_another_season", item: library.Item{Type: "Season", IsFolder: true, Season: season}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, detail := range []bool{false, true} {
				dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(test.item, []string{"SeriesId", "SeriesName", "SeasonId", "SeasonName"}, detail))
				assertTVParentFields(t, dto, test.want)
			}
		})
	}
}
