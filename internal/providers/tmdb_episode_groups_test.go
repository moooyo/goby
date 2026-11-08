package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"testing"
)

func episodeGroupContractJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode provider fixture: %v", err)
	}
	return string(encoded)
}

func episodeGroupContractClient(t *testing.T, responses map[string]string) (*Client, *[]string) {
	t.Helper()
	client := New(Config{Enabled: true, TMDBToken: "fixture-group-token"})
	var calls []string
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.URL.Path)
		if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "api.themoviedb.org" ||
			request.Header.Get("Authorization") != "Bearer fixture-group-token" || len(request.URL.Query()) != 0 {
			t.Errorf("group lookup changed its fixed-host credential boundary: %s %s", request.Method, request.URL)
		}
		body, exists := responses[request.URL.Path]
		if !exists {
			t.Errorf("unexpected provider route: %s", request.URL.Path)
			return nil, ErrUnavailable
		}
		return metadataContractResponse(body), nil
	})}
	return client, &calls
}

func episodeGroupContractList(groupType, episodes, groups int) map[string]any {
	return map[string]any{"id": 50, "results": []map[string]any{{
		"id": "selected-group", "name": "Selected provider grouping", "type": groupType,
		"episode_count": episodes, "group_count": groups,
	}}}
}

func episodeGroupContractDetail(groupType int) map[string]any {
	return map[string]any{
		"id": "selected-group", "type": groupType, "episode_count": 3, "group_count": 2,
		"groups": []map[string]any{
			{"name": "Second disc", "order": 2, "episodes": []map[string]any{
				{"id": 103, "show_id": 50, "season_number": 1, "episode_number": 3, "order": 1},
				{"id": 102, "show_id": 50, "season_number": 2, "episode_number": 1, "order": 0},
			}},
			{"name": "First disc", "order": 1, "episodes": []map[string]any{
				{"id": 101, "show_id": 50, "season_number": 1, "episode_number": 1, "order": 0},
			}},
		},
	}
}

func TestTMDBEpisodeGroupsRetainMultipleExplicitChoicesOfTheSameType(t *testing.T) {
	listing := map[string]any{"id": 50, "results": []map[string]any{
		{"id": "dvd-b", "name": "Z release", "type": 3, "episode_count": 4, "group_count": 2},
		{"id": "aired", "name": "Aired order", "type": 1, "episode_count": 4, "group_count": 2},
		{"id": "absolute", "name": "Absolute order", "type": 2, "episode_count": 4, "group_count": 1},
		{"id": "dvd-a", "name": "A release", "type": 3, "episode_count": 4, "group_count": 2},
	}}
	client, calls := episodeGroupContractClient(t, map[string]string{
		"/3/tv/50/episode_groups": episodeGroupContractJSON(t, listing),
	})
	groups, err := client.TVEpisodeGroups(context.Background(), "50")
	want := []TVEpisodeGroup{
		{ID: "absolute", Name: "Absolute order", DisplayOrder: "Absolute", EpisodeCount: 4, GroupCount: 1},
		{ID: "dvd-a", Name: "A release", DisplayOrder: "Dvd", EpisodeCount: 4, GroupCount: 2},
		{ID: "dvd-b", Name: "Z release", DisplayOrder: "Dvd", EpisodeCount: 4, GroupCount: 2},
	}
	if err != nil || !reflect.DeepEqual(groups, want) || !reflect.DeepEqual(*calls, []string{"/3/tv/50/episode_groups"}) {
		t.Fatalf("group choices were merged or selected by name: %+v, calls=%v, error=%v", groups, *calls, err)
	}
}

func TestTMDBEpisodeGroupsRejectUnverifiableOrOversizedListings(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"foreign series", func(value map[string]any) { value["id"] = 51 }},
		{"missing listing", func(value map[string]any) { delete(value, "results") }},
		{"null listing", func(value map[string]any) { value["results"] = nil }},
		{"duplicate group", func(value map[string]any) {
			groups := value["results"].([]map[string]any)
			value["results"] = append(groups, groups[0])
		}},
		{"path as identifier", func(value map[string]any) { value["results"].([]map[string]any)[0]["id"] = "../foreign" }},
		{"missing label", func(value map[string]any) { value["results"].([]map[string]any)[0]["name"] = "" }},
		{"episode limit", func(value map[string]any) {
			value["results"].([]map[string]any)[0]["episode_count"] = MaxTVEpisodeGroupEntries + 1
		}},
		{"group limit", func(value map[string]any) {
			value["results"].([]map[string]any)[0]["group_count"] = MaxTVEpisodeGroupEntries + 1
		}},
		{"listing limit", func(value map[string]any) {
			groups := make([]map[string]any, 201)
			for index := range groups {
				groups[index] = map[string]any{"id": "group-" + strconv.Itoa(index), "name": "DVD", "type": 3, "episode_count": 1, "group_count": 1}
			}
			value["results"] = groups
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			listing := episodeGroupContractList(3, 3, 2)
			test.mutate(listing)
			client, calls := episodeGroupContractClient(t, map[string]string{"/3/tv/50/episode_groups": episodeGroupContractJSON(t, listing)})
			if _, err := client.TVEpisodeGroups(context.Background(), "50"); !errors.Is(err, ErrUnavailable) || len(*calls) != 1 {
				t.Fatalf("invalid group listing was accepted: calls=%v error=%v", *calls, err)
			}
		})
	}
	client, _ := episodeGroupContractClient(t, map[string]string{"/3/tv/50/episode_groups": `{"id":50,"results":[]}`})
	groups, err := client.TVEpisodeGroups(context.Background(), "50")
	if err != nil || groups == nil || len(groups) != 0 {
		t.Fatalf("a genuine empty listing was not preserved: %+v, %v", groups, err)
	}
}

func TestTMDBEpisodeGroupVerifiesMembershipBeforeFetchingAndFlattensProviderRanks(t *testing.T) {
	for _, test := range []struct {
		groupType int
		order     string
	}{{3, "Dvd"}, {2, "Absolute"}} {
		t.Run(test.order, func(t *testing.T) {
			client, calls := episodeGroupContractClient(t, map[string]string{
				"/3/tv/50/episode_groups":            episodeGroupContractJSON(t, episodeGroupContractList(test.groupType, 3, 2)),
				"/3/tv/episode_group/selected-group": episodeGroupContractJSON(t, episodeGroupContractDetail(test.groupType)),
			})
			detail, err := client.TVEpisodeGroup(context.Background(), "50", "selected-group")
			want := []TVEpisodeGroupEntry{
				{ProviderEpisodeID: "101", SeasonNumber: 1, EpisodeNumber: 1, GroupOrder: 1, EpisodeOrder: 0, Ordinal: 0, GroupName: "First disc"},
				{ProviderEpisodeID: "102", SeasonNumber: 2, EpisodeNumber: 1, GroupOrder: 2, EpisodeOrder: 0, Ordinal: 1, GroupName: "Second disc"},
				{ProviderEpisodeID: "103", SeasonNumber: 1, EpisodeNumber: 3, GroupOrder: 2, EpisodeOrder: 1, Ordinal: 2, GroupName: "Second disc"},
			}
			if err != nil || detail.DisplayOrder != test.order || detail.SeriesID != "50" || !reflect.DeepEqual(detail.Entries, want) {
				t.Fatalf("provider ranks replaced canonical identity or lost cross-season order: %+v, %v", detail, err)
			}
			if !reflect.DeepEqual(*calls, []string{"/3/tv/50/episode_groups", "/3/tv/episode_group/selected-group"}) {
				t.Fatalf("group membership was not resolved first: %v", *calls)
			}
		})
	}
	client, calls := episodeGroupContractClient(t, map[string]string{"/3/tv/50/episode_groups": episodeGroupContractJSON(t, episodeGroupContractList(3, 3, 2))})
	if _, err := client.TVEpisodeGroup(context.Background(), "50", "foreign-group"); !errors.Is(err, ErrNotFound) ||
		!reflect.DeepEqual(*calls, []string{"/3/tv/50/episode_groups"}) {
		t.Fatalf("an unlisted group reached the detail endpoint: calls=%v error=%v", *calls, err)
	}
}

func TestTMDBEpisodeGroupRejectsAmbiguousIncompleteOrForeignDetails(t *testing.T) {
	groups := func(value map[string]any) []map[string]any { return value["groups"].([]map[string]any) }
	episodes := func(value map[string]any, group int) []map[string]any {
		return groups(value)[group]["episodes"].([]map[string]any)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"different group", func(value map[string]any) { value["id"] = "foreign-group" }},
		{"different order type", func(value map[string]any) { value["type"] = 2 }},
		{"different episode count", func(value map[string]any) { value["episode_count"] = 4 }},
		{"different group count", func(value map[string]any) { value["group_count"] = 3 }},
		{"foreign episode", func(value map[string]any) { episodes(value, 0)[0]["show_id"] = 51 }},
		{"duplicate canonical pair", func(value map[string]any) {
			episodes(value, 0)[0]["season_number"] = 1
			episodes(value, 0)[0]["episode_number"] = 1
		}},
		{"duplicate provider identity", func(value map[string]any) { episodes(value, 0)[0]["id"] = 101 }},
		{"duplicate group rank", func(value map[string]any) { groups(value)[0]["order"] = 1 }},
		{"duplicate episode rank", func(value map[string]any) { episodes(value, 0)[0]["order"] = 0 }},
		{"missing group rank", func(value map[string]any) { delete(groups(value)[0], "order") }},
		{"missing episode rank", func(value map[string]any) { delete(episodes(value, 0)[0], "order") }},
		{"missing canonical season", func(value map[string]any) { delete(episodes(value, 0)[0], "season_number") }},
		{"missing canonical episode", func(value map[string]any) { delete(episodes(value, 0)[0], "episode_number") }},
		{"negative canonical season", func(value map[string]any) { episodes(value, 0)[0]["season_number"] = -1 }},
		{"excessive canonical episode", func(value map[string]any) { episodes(value, 0)[0]["episode_number"] = 10000 }},
		{"empty provider identity", func(value map[string]any) { episodes(value, 0)[0]["id"] = 0 }},
		{"negative group rank", func(value map[string]any) { groups(value)[0]["order"] = -1 }},
		{"excessive episode rank", func(value map[string]any) { episodes(value, 0)[0]["order"] = MaxTVEpisodeGroupEntries + 1 }},
		{"omitted episodes", func(value map[string]any) { delete(groups(value)[0], "episodes") }},
		{"missing declared episode", func(value map[string]any) { groups(value)[0]["episodes"] = episodes(value, 0)[:1] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := episodeGroupContractDetail(3)
			test.mutate(payload)
			client, calls := episodeGroupContractClient(t, map[string]string{
				"/3/tv/50/episode_groups":            episodeGroupContractJSON(t, episodeGroupContractList(3, 3, 2)),
				"/3/tv/episode_group/selected-group": episodeGroupContractJSON(t, payload),
			})
			if _, err := client.TVEpisodeGroup(context.Background(), "50", "selected-group"); !errors.Is(err, ErrUnavailable) || len(*calls) != 2 {
				t.Fatalf("invalid group detail was accepted: calls=%v error=%v", *calls, err)
			}
		})
	}
}

func TestTMDBEpisodeGroupRejectsEntryOverflowWithoutTruncation(t *testing.T) {
	episodes := make([]map[string]any, MaxTVEpisodeGroupEntries+1)
	for index := range episodes {
		episodes[index] = map[string]any{"id": index + 1, "show_id": 50, "season_number": 1, "episode_number": index + 1, "order": index}
	}
	payload := map[string]any{"id": "selected-group", "type": 3, "episode_count": MaxTVEpisodeGroupEntries, "group_count": 1,
		"groups": []map[string]any{{"name": "Oversized group", "order": 1, "episodes": episodes}}}
	client, calls := episodeGroupContractClient(t, map[string]string{
		"/3/tv/50/episode_groups":            episodeGroupContractJSON(t, episodeGroupContractList(3, MaxTVEpisodeGroupEntries, 1)),
		"/3/tv/episode_group/selected-group": episodeGroupContractJSON(t, payload),
	})
	if _, err := client.TVEpisodeGroup(context.Background(), "50", "selected-group"); !errors.Is(err, ErrUnavailable) || len(*calls) != 2 {
		t.Fatalf("an oversized provider group was accepted or truncated: calls=%v error=%v", *calls, err)
	}
	payload["groups"].([]map[string]any)[0]["episodes"] = episodes[:MaxTVEpisodeGroupEntries]
	client, _ = episodeGroupContractClient(t, map[string]string{
		"/3/tv/50/episode_groups":            episodeGroupContractJSON(t, episodeGroupContractList(3, MaxTVEpisodeGroupEntries, 1)),
		"/3/tv/episode_group/selected-group": episodeGroupContractJSON(t, payload),
	})
	detail, err := client.TVEpisodeGroup(context.Background(), "50", "selected-group")
	if err != nil || len(detail.Entries) != MaxTVEpisodeGroupEntries || detail.Entries[MaxTVEpisodeGroupEntries-1].Ordinal != MaxTVEpisodeGroupEntries-1 {
		t.Fatalf("a complete group at the admitted boundary was rejected or truncated: count=%d error=%v", len(detail.Entries), err)
	}
}

func TestTMDBEpisodeGroupRejectsInvalidInputBeforeNetworkAccess(t *testing.T) {
	client, calls := episodeGroupContractClient(t, nil)
	for _, series := range []string{"0", "-1", "050", "50/episodes", "50?api_key=secret", "9223372036854775808"} {
		if _, err := client.TVEpisodeGroups(context.Background(), series); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid series ID %q was accepted: %v", series, err)
		}
	}
	for _, group := range []string{"", "../group", "group?token=secret", "group/elsewhere"} {
		if _, err := client.TVEpisodeGroup(context.Background(), "50", group); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid group ID %q was accepted: %v", group, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid input reached the transport: %v", *calls)
	}
	for _, test := range []struct {
		config Config
		want   error
	}{{Config{TMDBToken: "fixture"}, ErrDisabled}, {Config{Enabled: true}, ErrNotConfigured}} {
		client := New(test.config)
		client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
			t.Errorf("disabled or unconfigured provider reached the network: %s", request.URL)
			return nil, ErrUnavailable
		})}
		if _, err := client.TVEpisodeGroups(context.Background(), "50"); !errors.Is(err, test.want) {
			t.Fatalf("provider gate = %v, want %v", err, test.want)
		}
	}
}
