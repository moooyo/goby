package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type nextUpSeriesFixture struct {
	ID, LibraryID string
	Seasons       map[int]string
	Episodes      map[int][]string
}

func nextUpNumber(number int) *int {
	return &number
}

func nextUpLibrary(t *testing.T, ctx context.Context, store *Store, allowedRoot, name string) Library {
	t.Helper()
	root := filepath.Join(allowedRoot, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("create next-up library root: %v", err)
	}
	return libraryIntegrationCreate(t, ctx, store, name, "tvshows", root)
}

func nextUpInsertItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID, id, parentID, name, kind string, folder bool, index, parentIndex *int, withMedia bool) {
	t.Helper()
	var encoded []byte
	if withMedia {
		info := libraryMediaFixture([]byte("video:next-up"))
		info.DurationTicks = 600 * media.TicksPerSecond
		var err error
		encoded, err = json.Marshal(info)
		if err != nil {
			t.Fatalf("encode next-up media fixture: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, type, is_folder, index_number, parent_index_number, media)
		VALUES ($1, $2, $3, $4, lower($4), $5, $6, $7, $8, $9)`,
		id, libraryID, parentID, name, kind, folder, index, parentIndex, encoded); err != nil {
		t.Fatalf("insert next-up %s fixture: %v", kind, err)
	}
}

func nextUpSeries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, library Library, name string, seasons []int, episodeCount int) nextUpSeriesFixture {
	t.Helper()
	series := nextUpSeriesFixture{ID: library.ID + "-" + name, LibraryID: library.ID, Seasons: make(map[int]string), Episodes: make(map[int][]string)}
	nextUpInsertItem(t, ctx, pool, library.ID, series.ID, library.ID, name, "Series", true, nextUpNumber(0), nextUpNumber(0), false)
	for _, number := range seasons {
		seasonID := fmt.Sprintf("%s-season-%d", series.ID, number)
		series.Seasons[number] = seasonID
		nextUpInsertItem(t, ctx, pool, library.ID, seasonID, series.ID, fmt.Sprintf("%s Season %d", name, number), "Season", true, nextUpNumber(number), nextUpNumber(0), false)
		for episode := 1; episode <= episodeCount; episode++ {
			id := fmt.Sprintf("%s-episode-%d", seasonID, episode)
			nextUpInsertItem(t, ctx, pool, library.ID, id, seasonID, fmt.Sprintf("%s S%02dE%02d", name, number, episode), "Episode", false, nextUpNumber(episode), nextUpNumber(number), true)
			series.Episodes[number] = append(series.Episodes[number], id)
		}
	}
	return series
}

func nextUpPlayed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, itemID string, date time.Time) {
	t.Helper()
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: itemID, PlayCount: 1, Played: true, LastPlayedDate: &date})
}

func nextUpQuery(t *testing.T, ctx context.Context, store *Store, query NextUpQuery) ItemResult {
	t.Helper()
	result, err := store.NextUp(ctx, query)
	if err != nil {
		t.Fatalf("query next-up episodes: %v", err)
	}
	return result
}

func nextUpAssertIDs(t *testing.T, result ItemResult, expected ...string) {
	t.Helper()
	if result.TotalRecordCount != len(expected) || len(result.Items) != len(expected) {
		t.Fatalf("next-up count = %+v, want %d candidates", result, len(expected))
	}
	wanted := make(map[string]bool, len(expected))
	for _, id := range expected {
		wanted[id] = true
	}
	for _, item := range result.Items {
		if !wanted[item.ID] || item.Type != "Episode" || item.IsFolder {
			t.Errorf("next-up returned an unexpected or duplicate candidate: %+v", item)
		}
		delete(wanted, item.ID)
	}
	if len(wanted) != 0 {
		t.Errorf("next-up omitted candidate IDs: %+v", wanted)
	}
}

func nextUpAssertFirstID(t *testing.T, result ItemResult, expected string) {
	t.Helper()
	if result.TotalRecordCount < 1 || len(result.Items) != 1 || result.Items[0].ID != expected || result.Items[0].Type != "Episode" || result.Items[0].IsFolder {
		t.Fatalf("first next-up candidate = %+v, want %s", result, expected)
	}
}

func TestStoreNextUpValidatesInputsAndCurrentVisibleScopes(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	visibleLibrary := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-visible")
	hiddenLibrary := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-hidden")
	visible := nextUpSeries(t, ctx, pool, visibleLibrary, "Visible", []int{1}, 2)
	hidden := nextUpSeries(t, ctx, pool, hiddenLibrary, "Hidden", []int{1}, 2)
	viewerID := "next-up-scope-viewer"
	libraryIntegrationUser(t, ctx, pool, viewerID, false, false, []string{visibleLibrary.ID})
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID}))
	date := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	nextUpPlayed(t, ctx, pool, viewerID, visible.Episodes[1][0], date)
	nextUpPlayed(t, ctx, pool, viewerID, hidden.Episodes[1][0], date.Add(time.Hour))
	for _, query := range []NextUpQuery{
		{}, {UserID: "   "}, {UserID: viewerID, StartIndex: -1}, {UserID: viewerID, Limit: -1},
		{UserID: viewerID, Limit: 1001}, {UserID: viewerID + "\x00"},
		{UserID: viewerID, SeriesID: "bad\x00series"}, {UserID: viewerID, ParentID: "bad\x00parent"},
	} {
		if _, err := store.NextUp(ctx, query); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid next-up query %+v: got %v, want ErrInvalidInput", query, err)
		}
	}
	for _, query := range []NextUpQuery{
		{UserID: viewerID, SeriesID: hidden.ID}, {UserID: viewerID, ParentID: hiddenLibrary.ID},
		{UserID: viewerID, SeriesID: visible.Seasons[1]}, {UserID: viewerID, SeriesID: visible.Episodes[1][0]},
		{UserID: viewerID, SeriesID: "unknown-next-up-series"}, {UserID: viewerID, ParentID: "unknown-next-up-parent"},
	} {
		if _, err := store.NextUp(ctx, query); !errors.Is(err, ErrNotFound) {
			t.Errorf("hidden or invalid next-up scope %+v: got %v, want ErrNotFound", query, err)
		}
	}
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID, SeriesID: visible.ID, Limit: 1000}), visible.Episodes[1][1])
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnabledFolders}', '[]'::jsonb) WHERE id = $1`, viewerID); err != nil {
		t.Fatalf("revoke current next-up library scope: %v", err)
	}
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID}))
	if _, err := store.NextUp(ctx, NextUpQuery{UserID: viewerID, SeriesID: visible.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked next-up series remained addressable: got %v, want ErrNotFound", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", viewerID); err != nil {
		t.Fatalf("disable next-up viewer: %v", err)
	}
	if _, err := store.NextUp(ctx, NextUpQuery{UserID: viewerID}); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled next-up viewer: got %v, want ErrForbidden", err)
	}
}

func TestStoreNextUpScopesHistoryAndPaginationToTheCurrentUser(t *testing.T) {
	ctx, pool, store, allowedRoot, otherUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	visibleLibrary := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-history")
	hiddenLibrary := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-private-history")
	viewerID := "next-up-history-viewer"
	libraryIntegrationUser(t, ctx, pool, viewerID, false, false, []string{visibleLibrary.ID})
	date := time.Date(2025, time.February, 3, 4, 5, 6, 0, time.UTC)
	var viewerIDs, otherIDs []string
	for index, name := range []string{"Alpha", "Beta", "Gamma"} {
		series := nextUpSeries(t, ctx, pool, visibleLibrary, name, []int{1}, 3)
		nextUpPlayed(t, ctx, pool, viewerID, series.Episodes[1][0], date.Add(time.Duration(index)*time.Hour))
		nextUpPlayed(t, ctx, pool, otherUser, series.Episodes[1][0], date.Add(10*time.Hour))
		nextUpPlayed(t, ctx, pool, otherUser, series.Episodes[1][1], date.Add(11*time.Hour))
		viewerIDs = append(viewerIDs, series.Episodes[1][1])
		otherIDs = append(otherIDs, series.Episodes[1][2])
	}
	hidden := nextUpSeries(t, ctx, pool, hiddenLibrary, "Hidden", []int{1}, 2)
	nextUpPlayed(t, ctx, pool, viewerID, hidden.Episodes[1][0], date.Add(24*time.Hour))
	complete := nextUpSeries(t, ctx, pool, visibleLibrary, "Complete", []int{1}, 2)
	for _, id := range complete.Episodes[1] {
		nextUpPlayed(t, ctx, pool, viewerID, id, date.Add(48*time.Hour))
	}
	userDataSeed(t, ctx, pool, viewerID, UserData{ItemID: viewerIDs[0], IsFavorite: true})
	result := nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID})
	nextUpAssertIDs(t, result, viewerIDs...)
	for _, item := range result.Items {
		if item.UserData == nil {
			t.Fatalf("next-up candidate %s has no caller user data", item.ID)
		}
		userDataAssertValue(t, *item.UserData, UserData{ItemID: item.ID, IsFavorite: item.ID == viewerIDs[0]})
	}
	seen := make(map[string]bool)
	for offset := range viewerIDs {
		page := nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID, StartIndex: offset, Limit: 1})
		if page.TotalRecordCount != 3 || len(page.Items) != 1 || seen[page.Items[0].ID] {
			t.Fatalf("next-up authorization or counting happened after pagination: %+v", page)
		}
		seen[page.Items[0].ID] = true
	}
	for _, id := range viewerIDs {
		if !seen[id] {
			t.Errorf("paged next-up results omitted visible candidate %s", id)
		}
	}
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: otherUser}), otherIDs...)
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID, SeriesID: complete.ID}))
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, viewerID); err != nil {
		t.Fatalf("disable media playback without revoking catalog access: %v", err)
	}
	browsable := nextUpQuery(t, ctx, store, NextUpQuery{UserID: viewerID})
	nextUpAssertIDs(t, browsable, viewerIDs...)
	for _, item := range browsable.Items {
		if item.CanPlay {
			t.Errorf("next-up candidate ignored current playback policy: %+v", item)
		}
	}
}

func TestStoreNextUpStopsNestedSeriesAndCrossLibraryTraversalAndHandlesCycles(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-tree")
	foreignLibrary := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-foreign-tree")
	outer := nextUpSeries(t, ctx, pool, library, "Outer", []int{1}, 2)
	inner := nextUpSeries(t, ctx, pool, library, "Inner", []int{1}, 2)
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2 WHERE id = $1", inner.ID, outer.Seasons[1]); err != nil {
		t.Fatalf("nest one series below another: %v", err)
	}
	date := time.Date(2025, time.March, 4, 5, 6, 7, 0, time.UTC)
	nextUpPlayed(t, ctx, pool, userID, inner.Episodes[1][0], date)
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID}), inner.Episodes[1][1])
	main := nextUpSeries(t, ctx, pool, library, "Main", []int{1}, 2)
	foreign := nextUpSeries(t, ctx, pool, foreignLibrary, "Foreign", []int{1}, 2)
	nextUpPlayed(t, ctx, pool, userID, main.Episodes[1][0], date)
	nextUpPlayed(t, ctx, pool, userID, foreign.Episodes[1][0], date)
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2, name = '000 Foreign Candidate', sort_name = '000 foreign candidate' WHERE id = $1", foreign.Episodes[1][1], main.Seasons[1]); err != nil {
		t.Fatalf("create cross-library episode parent corruption: %v", err)
	}
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID}), inner.Episodes[1][1], main.Episodes[1][1])
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2 WHERE id = $1", inner.ID, inner.Seasons[1]); err != nil {
		t.Fatalf("create cyclic next-up parent scope: %v", err)
	}
	cycleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	nextUpAssertIDs(t, nextUpQuery(t, cycleCtx, store, NextUpQuery{UserID: userID, SeriesID: inner.ID, ParentID: inner.ID}), inner.Episodes[1][1])
}

func TestStoreNextUpParentScopeKeepsSeriesHistoryAcrossSeasonGaps(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-season-scope")
	series := nextUpSeries(t, ctx, pool, library, "Season Gap", []int{1, 3}, 1)
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][0], time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC))
	query := NextUpQuery{UserID: userID, ParentID: series.Seasons[3]}
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, query), series.Episodes[3][0])
	query.SeriesID = series.ID
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, query), series.Episodes[3][0])
	if _, err := pool.Exec(ctx, "UPDATE items SET media = NULL WHERE id = $1", series.Episodes[3][0]); err != nil {
		t.Fatalf("create a metadata-only next-up candidate: %v", err)
	}
	metadataOnly := nextUpQuery(t, ctx, store, query)
	nextUpAssertIDs(t, metadataOnly, series.Episodes[3][0])
	if metadataOnly.Items[0].Media != nil {
		t.Error("metadata-only episode unexpectedly acquired media information")
	}
}

func TestStoreNextUpExcludesMoviesAndFolderEpisodesFromSeriesHistory(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-episode-types")
	regular := nextUpSeries(t, ctx, pool, library, "Regular", []int{1}, 2)
	movies := nextUpSeries(t, ctx, pool, library, "Movie Only", []int{1}, 0)
	folderHistory := nextUpSeries(t, ctx, pool, library, "Folder Episode History", []int{1}, 0)
	date := time.Date(2025, time.April, 6, 7, 8, 9, 0, time.UTC)
	nextUpPlayed(t, ctx, pool, userID, regular.Episodes[1][0], date)
	for number := 1; number <= 2; number++ {
		id := fmt.Sprintf("%s-movie-%d", movies.ID, number)
		nextUpInsertItem(t, ctx, pool, library.ID, id, movies.Seasons[1], fmt.Sprintf("Movie %d", number), "Movie", false, nextUpNumber(number), nextUpNumber(1), true)
		if number == 1 {
			nextUpPlayed(t, ctx, pool, userID, id, date.Add(time.Hour))
		}
	}
	folderEpisode := folderHistory.ID + "-folder-episode"
	realEpisode := folderHistory.ID + "-real-episode"
	nextUpInsertItem(t, ctx, pool, library.ID, folderEpisode, folderHistory.Seasons[1], "Invalid Folder Episode", "Episode", true, nextUpNumber(1), nextUpNumber(1), true)
	nextUpInsertItem(t, ctx, pool, library.ID, realEpisode, folderHistory.Seasons[1], "Real Unstarted Episode", "Episode", false, nextUpNumber(2), nextUpNumber(1), true)
	nextUpPlayed(t, ctx, pool, userID, folderEpisode, date.Add(2*time.Hour))
	nextUpAssertIDs(t, nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID}), regular.Episodes[1][1])
}

func TestStoreNextUpDefaultsToOneHundredAndCountsBeforePaging(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-many-series")
	date := time.Date(2025, time.May, 6, 7, 8, 9, 0, time.UTC)
	expected := make(map[string]bool)
	for index := 0; index < 105; index++ {
		series := nextUpSeries(t, ctx, pool, library, fmt.Sprintf("Series %03d", index), []int{1}, 2)
		nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][0], date)
		expected[series.Episodes[1][1]] = true
	}
	first := nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID})
	if first.TotalRecordCount != 105 || len(first.Items) != 100 {
		t.Fatalf("default next-up limit or total count = %+v", first)
	}
	all := nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID, Limit: 1000})
	if all.TotalRecordCount != 105 || len(all.Items) != 105 {
		t.Fatalf("maximum accepted next-up limit did not return all candidates: %+v", all)
	}
	for _, item := range all.Items {
		if !expected[item.ID] {
			t.Errorf("large next-up result duplicated or invented a candidate: %s", item.ID)
		}
		delete(expected, item.ID)
	}
	if len(expected) != 0 {
		t.Errorf("large next-up result omitted candidates: %+v", expected)
	}
	last := nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID, StartIndex: 100, Limit: 1000})
	if last.TotalRecordCount != 105 || len(last.Items) != 5 {
		t.Fatalf("next-up count was truncated to the requested page: %+v", last)
	}
	for index, item := range last.Items {
		if item.ID != all.Items[index+100].ID {
			t.Errorf("next-up pagination changed candidate order at offset %d", index+100)
		}
	}
	beyond := nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID, StartIndex: 200, Limit: 1})
	if beyond.TotalRecordCount != 105 || len(beyond.Items) != 0 {
		t.Errorf("out-of-range next-up page lost the total count: %+v", beyond)
	}
}

func TestStoreNextUpOrdersSpecialsAndNullableNumbersBeforeProjection(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	// Only this owned schema permits legacy unknown numbers for this fixture.
	if _, err := pool.Exec(ctx, `ALTER TABLE items ALTER COLUMN index_number DROP NOT NULL,
		ALTER COLUMN parent_index_number DROP NOT NULL`); err != nil {
		t.Fatalf("allow nullable index fixtures in the owned schema: %v", err)
	}
	library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-unknown-numbers")
	series := nextUpSeries(t, ctx, pool, library, "Numbered", []int{0, 1}, 3)
	unknownSeason := series.ID + "-unknown-season"
	unknownSeasonEpisode := unknownSeason + "-episode"
	nextUpInsertItem(t, ctx, pool, library.ID, unknownSeason, series.ID, "Unknown Season", "Season", true, nil, nextUpNumber(0), false)
	nextUpInsertItem(t, ctx, pool, library.ID, unknownSeasonEpisode, unknownSeason, "Unknown Season Episode", "Episode", false, nextUpNumber(1), nil, true)
	if _, err := pool.Exec(ctx, "UPDATE items SET index_number = NULL WHERE id = $1", series.Episodes[0][1]); err != nil {
		t.Fatalf("clear legacy episode number: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_index_number = NULL WHERE id = $1", series.Episodes[1][0]); err != nil {
		t.Fatalf("clear episode parent number while retaining a known season: %v", err)
	}
	date := time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC)
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[0][0], date)
	query := NextUpQuery{UserID: userID, SeriesID: series.ID, Limit: 1}
	nextUpAssertFirstID(t, nextUpQuery(t, ctx, store, query), series.Episodes[0][2])
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[0][2], date.Add(time.Hour))
	unknownEpisode := nextUpQuery(t, ctx, store, query)
	nextUpAssertFirstID(t, unknownEpisode, series.Episodes[0][1])
	if unknownEpisode.Items[0].IndexNumber != 0 || unknownEpisode.Items[0].ParentIndexNumber != 0 {
		t.Errorf("nullable episode number did not normalize only after selection: %+v", unknownEpisode.Items[0])
	}
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[0][1], date.Add(2*time.Hour))
	knownSeason := nextUpQuery(t, ctx, store, query)
	nextUpAssertFirstID(t, knownSeason, series.Episodes[1][0])
	if knownSeason.Items[0].ParentIndexNumber != 0 {
		t.Errorf("nullable stored parent index was not projected as zero: %+v", knownSeason.Items[0])
	}
	for index, id := range series.Episodes[1] {
		nextUpPlayed(t, ctx, pool, userID, id, date.Add(time.Duration(index+3)*time.Hour))
	}
	last := nextUpQuery(t, ctx, store, query)
	nextUpAssertFirstID(t, last, unknownSeasonEpisode)
	if last.Items[0].ParentIndexNumber != 0 {
		t.Errorf("unknown season number was not projected as zero: %+v", last.Items[0])
	}
}

func TestStoreNextUpExplicitSeriesExpandsUnwatchedEpisodesAfterTheWatchedCursor(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		playedIndex int
	}{
		{name: "UnstartedSeries", playedIndex: -1},
		{name: "OnlyMiddleEpisodePlayed", playedIndex: 1},
		{name: "OnlyFirstEpisodePlayed", playedIndex: 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-explicit-cursor")
			series := nextUpSeries(t, ctx, pool, library, "Cursor Series", []int{1}, 2)
			secondSeason := series.ID + "-season-2"
			lastEpisode := secondSeason + "-episode-1"
			nextUpInsertItem(t, ctx, pool, library.ID, secondSeason, series.ID, "Cursor Series Season 2", "Season", true, nextUpNumber(2), nextUpNumber(0), false)
			nextUpInsertItem(t, ctx, pool, library.ID, lastEpisode, secondSeason, "Cursor Series S02E01", "Episode", false, nextUpNumber(1), nextUpNumber(2), true)
			var expected []string
			if scenario.playedIndex >= 0 {
				nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][scenario.playedIndex], time.Date(2025, time.July, 8, 9, 10, 11, 0, time.UTC))
				if scenario.playedIndex == 0 {
					expected = append(expected, series.Episodes[1][1])
				}
				expected = append(expected, lastEpisode)
			}
			for _, parentID := range []string{"", library.ID} {
				query := NextUpQuery{UserID: userID, SeriesID: series.ID, ParentID: parentID}
				result := nextUpQuery(t, ctx, store, query)
				nextUpAssertIDs(t, result, expected...)
				for index, id := range expected {
					if result.Items[index].ID != id {
						t.Errorf("explicit series expansion was not in episode order: items = %+v", result.Items)
					}
				}
				for offset := 0; offset <= len(expected); offset++ {
					query.StartIndex, query.Limit = offset, 1
					page := nextUpQuery(t, ctx, store, query)
					if page.TotalRecordCount != len(expected) {
						t.Fatalf("explicit series count was reduced to the expanded page: %+v", page)
					}
					if offset == len(expected) {
						if len(page.Items) != 0 {
							t.Errorf("explicit series returned episodes beyond the final page: %+v", page)
						}
					} else if len(page.Items) != 1 || page.Items[0].ID != expected[offset] {
						t.Errorf("explicit series expansion paged the wrong episode: page = %+v, want %s", page, expected[offset])
					}
				}
			}
		})
	}
}

func TestStoreNextUpGlobalGobyPolicySeparatesWatchedPartialAndFavoriteState(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		played      bool
		position    int64
		favorite    bool
		wantedIndex int
	}{
		{name: "MiddleWatchedSkipsEarlierGap", played: true, wantedIndex: 2},
		{name: "PartialWithoutWatchedStartsAtFirstEpisode", position: 120 * media.TicksPerSecond, wantedIndex: 0},
		{name: "FavoriteOnlyHasNoWatchingHistory", favorite: true, wantedIndex: -1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			library := nextUpLibrary(t, ctx, store, allowedRoot, "next-up-global-policy")
			series := nextUpSeries(t, ctx, pool, library, "Global Series", []int{1}, 2)
			secondSeason := series.ID + "-season-2"
			lastEpisode := secondSeason + "-episode-1"
			nextUpInsertItem(t, ctx, pool, library.ID, secondSeason, series.ID, "Global Series Season 2", "Season", true, nextUpNumber(2), nextUpNumber(0), false)
			nextUpInsertItem(t, ctx, pool, library.ID, lastEpisode, secondSeason, "Global Series S02E01", "Episode", false, nextUpNumber(1), nextUpNumber(2), true)
			data := UserData{ItemID: series.Episodes[1][1], Played: scenario.played, PlaybackPositionTicks: scenario.position, IsFavorite: scenario.favorite}
			if scenario.played {
				date := time.Date(2025, time.August, 9, 10, 11, 12, 0, time.UTC)
				data.PlayCount, data.LastPlayedDate = 1, &date
			}
			userDataSeed(t, ctx, pool, userID, data)
			result := nextUpQuery(t, ctx, store, NextUpQuery{UserID: userID})
			if scenario.wantedIndex < 0 {
				nextUpAssertIDs(t, result)
			} else {
				episodes := []string{series.Episodes[1][0], series.Episodes[1][1], lastEpisode}
				nextUpAssertIDs(t, result, episodes[scenario.wantedIndex])
			}
		})
	}
}
