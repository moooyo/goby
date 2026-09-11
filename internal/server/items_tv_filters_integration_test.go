package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type scannedTVFilterSeries struct {
	id       string
	seasons  map[int]string
	episodes map[[2]int]string
}

func readScannedTVFilterSeries(t *testing.T, f *serverFixture, headers http.Header, libraryID, name string, seasonNumbers []int, episodeNumbers [][2]int) scannedTVFilterSeries {
	t.Helper()
	query := url.Values{"ParentId": {libraryID}, "IncludeItemTypes": {"Series"}, "SearchTerm": {name}}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+query.Encode(), nil, headers))
	if total != 1 || len(items) != 1 || items[0]["Type"] != "Series" || items[0]["Name"] != name {
		t.Fatalf("scanner did not produce the expected series %q: %#v, total = %d", name, items, total)
	}
	series := scannedTVFilterSeries{
		id: stringValue(t, items[0], "Id"), seasons: make(map[int]string), episodes: make(map[[2]int]string),
	}
	items, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+series.id+"&Recursive=true", nil, headers))
	if total != len(seasonNumbers)+len(episodeNumbers) || len(items) != total {
		t.Fatalf("scanned series descendants = %d/%d, want %d", len(items), total, len(seasonNumbers)+len(episodeNumbers))
	}
	for _, item := range items {
		index, ok := item["IndexNumber"].(float64)
		if !ok {
			t.Fatalf("scanned season or episode has no numeric index: %#v", item)
		}
		id := stringValue(t, item, "Id")
		switch item["Type"] {
		case "Season":
			if item["IsFolder"] != true || series.seasons[int(index)] != "" {
				t.Fatalf("scanned season is not a unique folder: %#v", item)
			}
			series.seasons[int(index)] = id
		case "Episode":
			parentIndex, ok := item["ParentIndexNumber"].(float64)
			if !ok || item["IsFolder"] != false {
				t.Fatalf("scanned episode has no season index or is a folder: %#v", item)
			}
			key := [2]int{int(parentIndex), int(index)}
			if series.episodes[key] != "" {
				t.Fatalf("scanner produced duplicate season/episode numbers: %v", key)
			}
			series.episodes[key] = id
		default:
			t.Fatalf("scanner produced an unexpected series descendant: %#v", item)
		}
	}
	for _, number := range seasonNumbers {
		if series.seasons[number] == "" {
			t.Fatalf("scanner did not produce season %d for %q", number, name)
		}
	}
	for _, number := range episodeNumbers {
		if series.episodes[number] == "" {
			t.Fatalf("scanner did not produce episode %v for %q", number, name)
		}
	}
	return series
}

func assertTVFilterItems(t *testing.T, response *httptest.ResponseRecorder, expected []string, expectedTotal int, ordered bool) {
	t.Helper()
	items, total := responseItems(t, response)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, stringValue(t, item, "Id"))
	}
	want := append([]string{}, expected...)
	if !ordered {
		sort.Strings(ids)
		sort.Strings(want)
	}
	if !reflect.DeepEqual(ids, want) || total != expectedTotal {
		t.Fatalf("filtered item IDs/total = %v/%d, want %v/%d", ids, total, want, expectedTotal)
	}
}

func TestHTTPTVFiltersScannedCatalog(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	for _, relative := range []string{
		"visible/Special Show/Season 00/Special.Show.S00E01.mp4",
		"visible/Special Show/Season 01/Special.Show.S01E01.mp4",
		"visible/Special Show/Season 01/Special.Show.S01E02.mp4",
		"visible/Special Show/Season 02/Special.Show.S02E01.mp4",
		"visible/Regular Show/Season 01/Regular.Show.S01E01.mp4",
		"visible/Regular Show/Season 01/Regular.Show.S01E02.mp4",
		"visible/Regular Show/Season 02/Regular.Show.S02E01.mp4",
		"hidden/Private Show/Season 00/Private.Show.S00E01.mp4",
		"hidden/Private Show/Season 01/Private.Show.S01E01.mp4",
	} {
		writeAPIMediaFile(t, root, relative)
	}
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	visibleID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "visible"), "tvshows")
	hiddenID := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "hidden"), "tvshows")
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, adminLogin, "AccessToken")}}
	special := readScannedTVFilterSeries(t, f, adminHeaders, visibleID, "Special Show", []int{0, 1, 2}, [][2]int{{0, 1}, {1, 1}, {1, 2}, {2, 1}})
	regular := readScannedTVFilterSeries(t, f, adminHeaders, visibleID, "Regular Show", []int{1, 2}, [][2]int{{1, 1}, {1, 2}, {2, 1}})
	private := readScannedTVFilterSeries(t, f, adminHeaders, hiddenID, "Private Show", []int{0, 1}, [][2]int{{0, 1}, {1, 1}})
	viewer, err := f.users.CreateUser(f.ctx, "TV Filter Viewer", "tv-filter-password", false)
	if err != nil {
		t.Fatalf("create television filter viewer: %v", err)
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{visibleID}})
	if err != nil {
		t.Fatalf("encode television filter viewer policy: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", policy, viewer.ID); err != nil {
		t.Fatalf("restrict television filter viewer: %v", err)
	}
	login := f.embyLogin(t, viewer.Name, "tv-filter-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	itemsPath := "/emby/Items?ParentId=" + special.id + "&Recursive=true"
	seasonsPath := "/emby/Shows/" + special.id + "/Seasons?"
	episodesPath := "/emby/Shows/" + special.id + "/Episodes?"
	season0, season1, season2 := special.seasons[0], special.seasons[1], special.seasons[2]
	episode0 := special.episodes[[2]int{0, 1}]
	episode11, episode12, episode21 := special.episodes[[2]int{1, 1}], special.episodes[[2]int{1, 2}], special.episodes[[2]int{2, 1}]
	regularEpisodes := []string{episode11, episode12, episode21}
	allEpisodes := []string{episode0, episode11, episode12, episode21}

	t.Run("mixed_item_predicates", func(t *testing.T) {
		for _, test := range []struct {
			name, query string
			ids         []string
		}{
			{"folders", "IsFolder=true", []string{season0, season1, season2}},
			{"files", "IsFolder=false", allEpisodes},
			{"special_season", "IsSpecialSeason=true", []string{season0}},
			{"exclude_special_season_preserves_episodes", "IsSpecialSeason=false", []string{season1, season2, episode0, episode11, episode12, episode21}},
			{"special_episode", "IsSpecialEpisode=true", []string{episode0}},
			{"exclude_special_episode_preserves_seasons", "IsSpecialEpisode=false", []string{season0, season1, season2, episode11, episode12, episode21}},
			{"combined_regular_episodes", "IsFolder=false&IsSpecialSeason=false&IsSpecialEpisode=false", regularEpisodes},
			{"combined_regular_seasons", "IsFolder=true&IsSpecialSeason=false&IsSpecialEpisode=false", []string{season1, season2}},
			{"conflicting_classifications", "IsFolder=true&IsSpecialEpisode=true", nil},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertTVFilterItems(t, f.request(t, http.MethodGet, itemsPath+"&"+test.query, nil, headers), test.ids, len(test.ids), false)
			})
		}
		// A default zero index on a Series must not make it a special season or episode.
		assertTVFilterItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+visibleID+"&IsSpecialSeason=false&IsSpecialEpisode=false", nil, headers), []string{special.id, regular.id}, 2, false)
		assertTVFilterItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+special.id+"&Recursive=false&IsFolder=false", nil, headers), nil, 0, false)
	})

	t.Run("filtering_precedes_count_and_pagination", func(t *testing.T) {
		for _, test := range []struct {
			name, query string
			ids         []string
		}{
			{"folders", "IsFolder=true", []string{season0, season1, season2}},
			{"regular_seasons", "IncludeItemTypes=Season&IsSpecialSeason=false", []string{season1, season2}},
			{"regular_episodes", "IsFolder=false&IsSpecialEpisode=false", regularEpisodes},
		} {
			t.Run(test.name, func(t *testing.T) {
				path := itemsPath + "&SortBy=IndexNumber&" + test.query
				assertTVFilterItems(t, f.request(t, http.MethodGet, path, nil, headers), test.ids, len(test.ids), true)
				assertTVFilterItems(t, f.request(t, http.MethodGet, path+"&StartIndex=1&Limit=1", nil, headers), test.ids[1:2], len(test.ids), true)
				assertTVFilterItems(t, f.request(t, http.MethodGet, path+"&Limit=0", nil, headers), nil, len(test.ids), true)
			})
		}
	})

	t.Run("shows_aliases", func(t *testing.T) {
		for _, test := range []struct {
			name, path string
			ids        []string
		}{
			{"special_seasons", seasonsPath + "IsSpecialSeason=true", []string{season0}},
			{"regular_seasons", seasonsPath + "IsSpecialSeason=false", []string{season1, season2}},
			{"root_regular_seasons", "/Shows/" + special.id + "/Seasons?IsSpecialSeason=false", []string{season1, season2}},
			{"seasons_folder_conflict", seasonsPath + "IsFolder=false", nil},
			{"special_episodes", episodesPath + "IsFolder=false&IsSpecialEpisode=true", []string{episode0}},
			{"mixed_case_special_episodes", "/EMBY/shows/" + special.id + "/episodes?IsSpecialEpisode=true", []string{episode0}},
			{"regular_episodes", episodesPath + "IsSpecialEpisode=false", regularEpisodes},
			{"season_number_intersection", episodesPath + "Season=1&IsSpecialEpisode=true", nil},
			{"season_id_intersection", episodesPath + "SeasonId=" + season0 + "&IsSpecialEpisode=false", nil},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertTVFilterItems(t, f.request(t, http.MethodGet, test.path, nil, headers), test.ids, len(test.ids), true)
			})
		}
		assertTVFilterItems(t, f.request(t, http.MethodGet, seasonsPath+"IsSpecialSeason=false&StartIndex=1&Limit=1", nil, headers), []string{season2}, 2, true)
		assertTVFilterItems(t, f.request(t, http.MethodGet, episodesPath+"IsSpecialEpisode=false&Limit=0", nil, headers), nil, 3, true)
	})

	t.Run("observed_client_without_specials", func(t *testing.T) {
		path := "/emby/Users/" + viewer.ID + "/Items?ParentId=" + regular.id + "&Recursive=true&IsFolder=false"
		want := []string{regular.episodes[[2]int{1, 1}], regular.episodes[[2]int{1, 2}], regular.episodes[[2]int{2, 1}]}
		// IsStandaloneSpecial remains unmodeled. This fixture establishes only
		// folder exclusion and an empty Specials section for a regular series.
		assertTVFilterItems(t, f.request(t, http.MethodGet, path+"&IsStandaloneSpecial=false", nil, headers), want, 3, false)
		assertTVFilterItems(t, f.request(t, http.MethodGet, path+"&IsSpecialEpisode=true", nil, headers), nil, 0, false)
		assertTVFilterItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+regular.id+"/Seasons?IsSpecialSeason=false", nil, headers), []string{regular.seasons[1], regular.seasons[2]}, 2, true)
	})

	t.Run("invalid_values_and_authorized_scope", func(t *testing.T) {
		for _, name := range []string{"IsFolder", "IsSpecialSeason", "IsSpecialEpisode"} {
			expectAPIError(t, f.request(t, http.MethodGet, itemsPath+"&"+name+"=invalid", nil, headers), http.StatusBadRequest, "invalid_input", true)
		}
		expectAPIError(t, f.request(t, http.MethodGet, seasonsPath+"IsSpecialSeason=invalid", nil, headers), http.StatusBadRequest, "invalid_input", true)
		expectAPIError(t, f.request(t, http.MethodGet, episodesPath+"IsSpecialEpisode=invalid", nil, headers), http.StatusBadRequest, "invalid_input", true)
		for _, path := range []string{
			itemsPath + "&IsFolder=false&IsSpecialEpisode=true",
			seasonsPath + "IsSpecialSeason=false",
			episodesPath + "IsSpecialEpisode=true",
		} {
			expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized, "authentication_required", true)
		}
		for _, path := range []string{
			"/emby/Items?ParentId=" + hiddenID + "&Recursive=true&IsFolder=false&IsSpecialEpisode=true&Limit=0",
			"/emby/Shows/" + private.id + "/Seasons?IsSpecialSeason=true",
			"/emby/Shows/" + private.id + "/Episodes?IsSpecialEpisode=true",
		} {
			expectAPIError(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusNotFound, "not_found", true)
		}
		assertTVFilterItems(t, f.request(t, http.MethodGet, "/emby/Items?Recursive=true&IsFolder=false&IsSpecialEpisode=true", nil, headers), []string{episode0}, 1, true)
		assertTVFilterItems(t, f.request(t, http.MethodGet, "/emby/Items?Recursive=true&IsSpecialSeason=true&Limit=0", nil, headers), nil, 1, true)
	})
}
