package library

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestLibraryReadsProjectCanPlayWithoutChangingBrowseAccess(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryLatestFixture(t, ctx, store.pool)
	ids := []string{"episode-b1", "audio-b1", "movie-a"}
	for _, test := range []struct {
		name, userID, playbackJSON string
		wantCanPlay                bool
	}{
		{name: "restricted user defaults to playable", userID: "restricted", wantCanPlay: true},
		{name: "administrator defaults to playable", userID: "admin", wantCanPlay: true},
		{name: "disabled playback still allows restricted browsing", userID: "restricted", playbackJSON: `false`},
		{name: "invalid playback flag still allows restricted browsing", userID: "restricted", playbackJSON: `"false"`},
		{name: "administrator respects disabled playback", userID: "admin", playbackJSON: `false`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.playbackJSON != "" {
				if _, err := store.pool.Exec(ctx, `UPDATE users SET policy =
					jsonb_set(policy, '{EnableMediaPlayback}', $2::jsonb) WHERE id = $1`, test.userID, test.playbackJSON); err != nil {
					t.Fatal(err)
				}
			}
			checkCanPlay := func(item Item) {
				t.Helper()
				if item.CanPlay != test.wantCanPlay {
					t.Errorf("item %s CanPlay = %v, want %v", item.ID, item.CanPlay, test.wantCanPlay)
				}
			}
			wantIDs := []string{"episode-b1", "audio-b1"}
			if test.userID == "admin" {
				wantIDs = append([]string{"movie-a"}, wantIDs...)
			}
			result, err := store.QueryItems(ctx, Query{UserID: test.userID, Ids: ids})
			if err != nil {
				t.Fatal(err)
			}
			assertUserDataQueryResult(t, result, wantIDs, len(wantIDs))
			for _, item := range result.Items {
				checkCanPlay(item)
				read, err := store.GetItem(ctx, test.userID, item.ID)
				if err != nil {
					t.Fatal(err)
				}
				checkCanPlay(read)
			}
			if test.userID == "restricted" {
				if _, err := store.GetItem(ctx, test.userID, "movie-a"); !errors.Is(err, ErrNotFound) {
					t.Errorf("read hidden item error = %v, want ErrNotFound", err)
				}
			}
			for _, group := range []bool{false, true} {
				want := []latestExpectation{{"audio-b1", 1}, {"episode-b1", 1}}
				if group {
					want = []latestExpectation{{"album-b", 1}, {"series-b", 1}}
				}
				if test.userID == "admin" {
					want = append([]latestExpectation{{"movie-a", 1}}, want...)
				}
				latest, err := store.QueryLatest(ctx, Query{UserID: test.userID, Ids: ids}, group)
				if err != nil {
					t.Fatal(err)
				}
				assertLatestItems(t, latest, want)
				for _, item := range latest {
					checkCanPlay(item.Item)
				}
			}
		})
	}
}

func TestLibraryUserDataDefaultsAreIndependentForSupportedItems(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	ids := []string{"movie-b", "series-b", "season-b", "episode-b1", "video-b", "audio-b1", "album-b", "artist-b", "library-b", "album-disc-b"}
	result, err := store.QueryItems(ctx, Query{UserID: "admin", Ids: ids})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRecordCount != len(ids) || len(result.Items) != len(ids) {
		t.Fatalf("default user data query = %+v, want %d items", result, len(ids))
	}
	seen := make(map[*UserData]string)
	folderCounts := map[string]int{"series-b": 2, "season-b": 2, "album-b": 2, "artist-b": 0}
	for _, item := range result.Items {
		want := UserData{ItemID: item.ID}
		if count, exists := folderCounts[item.ID]; exists {
			want = userDataQueryFolderState(item.ID, count, false, false)
		}
		if item.Type == "CollectionFolder" || item.Type == "Folder" {
			if item.UserData != nil {
				t.Errorf("folder %s user data = %+v, want nil", item.ID, item.UserData)
			}
		} else {
			assertLibraryUserData(t, item, want)
			if previous, exists := seen[item.UserData]; exists {
				t.Errorf("items %s and %s share a default user data object", previous, item.ID)
			}
			seen[item.UserData] = item.ID
		}
		read, err := store.GetItem(ctx, "admin", item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.UserData == nil {
			if read.UserData != nil {
				t.Errorf("read folder %s user data = %+v, want nil", read.ID, read.UserData)
			}
		} else {
			assertLibraryUserData(t, read, want)
			if read.UserData == item.UserData {
				t.Errorf("separate reads of %s share a user data object", item.ID)
			}
		}
	}
}

func TestAttachUserDataKeepsRepeatedItemsAndDatesIndependent(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	tx, _, err := store.beginUserRead(ctx, "restricted")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	items := []Item{
		{ID: "episode-b1", Type: "Episode"}, {ID: "episode-b1", Type: "Episode"},
		{ID: "video-b", Type: "Video"}, {ID: "video-b", Type: "Video"},
		{ID: "series-b", Type: "Series", IsFolder: true}, {ID: "series-b", Type: "Series", IsFolder: true},
	}
	if err := attachUserData(ctx, tx, "restricted", items); err != nil {
		t.Fatal(err)
	}
	want := userDataQueryEpisodeState("restricted")
	assertLibraryUserData(t, items[0], want)
	assertLibraryUserData(t, items[1], want)
	assertLibraryUserData(t, items[2], UserData{ItemID: "video-b"})
	assertLibraryUserData(t, items[3], UserData{ItemID: "video-b"})
	assertLibraryUserData(t, items[4], userDataQueryFolderState("series-b", 1, false, false))
	assertLibraryUserData(t, items[5], userDataQueryFolderState("series-b", 1, false, false))
	if items[0].UserData == items[1].UserData || items[2].UserData == items[3].UserData || items[4].UserData == items[5].UserData {
		t.Fatal("repeated items share a user data object")
	}
	if items[0].UserData.LastPlayedDate == items[1].UserData.LastPlayedDate {
		t.Fatal("repeated items share a last played date")
	}
	if items[4].UserData.UnplayedItemCount == items[5].UserData.UnplayedItemCount {
		t.Fatal("repeated folders share an unplayed item count")
	}
	items[0].UserData.PlayCount++
	*items[0].UserData.LastPlayedDate = items[0].UserData.LastPlayedDate.Add(time.Hour)
	items[2].UserData.PlayCount++
	*items[4].UserData.UnplayedItemCount++
	assertLibraryUserData(t, items[1], want)
	assertLibraryUserData(t, items[3], UserData{ItemID: "video-b"})
	assertLibraryUserData(t, items[5], userDataQueryFolderState("series-b", 1, false, false))
}

func TestFolderUserDataDerivesDistinctSameLibraryDescendants(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE user_item_data SET is_favorite = true
		WHERE user_id = 'restricted' AND item_id = 'series-b';
		UPDATE user_item_data SET played = true
		WHERE user_id = 'restricted' AND item_id = 'artist-b'`); err != nil {
		t.Fatal(err)
	}
	checkFolders := func(userID string, unplayed int, played bool) {
		t.Helper()
		want := map[string]UserData{
			"series-b": userDataQueryFolderState("series-b", unplayed, played, userID == "restricted"),
			"season-b": userDataQueryFolderState("season-b", unplayed, played, false),
			"album-b":  userDataQueryFolderState("album-b", 2, false, false),
			"artist-b": userDataQueryFolderState("artist-b", 0, false, false),
		}
		result, err := store.QueryItems(ctx, Query{UserID: userID, Ids: []string{"series-b", "season-b", "album-b", "artist-b"}})
		if err != nil {
			t.Fatal(err)
		}
		if result.TotalRecordCount != len(want) || len(result.Items) != len(want) {
			t.Fatalf("folder query = %+v, want four independently derived roots", result)
		}
		for _, item := range result.Items {
			assertLibraryUserData(t, item, want[item.ID])
		}
		for _, id := range []string{"series-b", "season-b"} {
			item, err := store.GetItem(ctx, userID, id)
			if err != nil {
				t.Fatal(err)
			}
			assertLibraryUserData(t, item, want[id])
		}
		for _, isPlayed := range []bool{true, false} {
			filtered, err := store.QueryItems(ctx, Query{
				UserID: userID, Ids: []string{"series-b", "season-b", "artist-b"}, IsPlayed: &isPlayed, SortBy: "Name",
			})
			if err != nil {
				t.Fatal(err)
			}
			wantIDs := []string{}
			if !isPlayed {
				wantIDs = append(wantIDs, "artist-b")
			}
			if isPlayed == played {
				wantIDs = append(wantIDs, "season-b", "series-b")
			}
			assertUserDataQueryResult(t, filtered, wantIDs, len(wantIDs))
		}
		isFavorite := true
		favorites, err := store.QueryItems(ctx, Query{
			UserID: userID, Ids: []string{"series-b", "season-b", "album-b", "artist-b"}, IsFavorite: &isFavorite,
		})
		if err != nil {
			t.Fatal(err)
		}
		wantFavorites := []string{}
		if userID == "restricted" {
			wantFavorites = []string{"series-b"}
		}
		assertUserDataQueryResult(t, favorites, wantFavorites, len(wantFavorites))
	}
	checkFolders("restricted", 1, false)
	checkFolders("default", 1, false)
	// The Series and Season remain separate roots even when a corrupt cycle
	// reaches either root again. The cross-library child is never counted.
	if _, err := store.pool.Exec(ctx, "UPDATE items SET parent_id = 'episode-b1' WHERE id = 'series-b'"); err != nil {
		t.Fatal(err)
	}
	checkFolders("restricted", 1, false)
	if _, err := store.pool.Exec(ctx, `UPDATE user_item_data SET played = true
		WHERE user_id = 'restricted' AND item_id = 'episode-b1'`); err != nil {
		t.Fatal(err)
	}
	checkFolders("restricted", 0, true)
	checkFolders("default", 1, false)
}

func TestLibraryQueriesAttachOnlyTheCurrentUsersData(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	for _, userID := range []string{"restricted", "default"} {
		t.Run(userID, func(t *testing.T) {
			want := userDataQueryEpisodeState(userID)
			item, err := store.GetItem(ctx, userID, "episode-b1")
			if err != nil {
				t.Fatal(err)
			}
			assertLibraryUserData(t, item, want)
			result, err := store.QueryItems(ctx, Query{UserID: userID, Ids: []string{"episode-b1", "season-b"}})
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalRecordCount != 2 || len(result.Items) != 2 {
				t.Fatalf("user data query = %+v, want two visible items", result)
			}
			for _, item := range result.Items {
				if item.ID == "episode-b1" {
					assertLibraryUserData(t, item, want)
				} else {
					assertLibraryUserData(t, item, userDataQueryFolderState("season-b", 1, false, false))
				}
			}
			latest, err := store.QueryLatest(ctx, Query{UserID: userID, Ids: []string{"episode-b1"}}, false)
			if err != nil {
				t.Fatal(err)
			}
			assertLatestItems(t, latest, []latestExpectation{{"episode-b1", 1}})
			assertLibraryUserData(t, latest[0].Item, want)
		})
	}
	if _, err := store.GetItem(ctx, "restricted", "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read hidden item with stored user data error = %v, want ErrNotFound", err)
	}
	for _, id := range []string{"library-b", "album-disc-b"} {
		item, err := store.GetItem(ctx, "restricted", id)
		if err != nil {
			t.Fatal(err)
		}
		if item.UserData != nil {
			t.Errorf("folder %s exposes stored user data: %+v", id, item.UserData)
		}
	}
}

func TestUserDataFlagsFilterBeforeCountingAndPaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	yes, no := true, false
	ids := []string{"episode-b1", "episode-b2", "movie-b", "video-b", "movie-a", "cross-library-child"}
	tests := []struct {
		name  string
		query Query
		want  []string
		total int
	}{
		{name: "unplayed includes missing rows", query: Query{UserID: "restricted", IsPlayed: &no}, want: []string{"episode-b1", "movie-b", "video-b"}, total: 3},
		{name: "not favorite includes missing rows", query: Query{UserID: "restricted", IsFavorite: &no}, want: []string{"episode-b2", "movie-b", "video-b"}, total: 3},
		{name: "false flags combine", query: Query{UserID: "restricted", IsPlayed: &no, IsFavorite: &no}, want: []string{"movie-b", "video-b"}, total: 2},
		{name: "played uses only current user state", query: Query{UserID: "restricted", IsPlayed: &yes}, want: []string{"episode-b2"}, total: 1},
		{name: "favorite does not expose hidden stored state", query: Query{UserID: "restricted", IsFavorite: &yes}, want: []string{"episode-b1"}, total: 1},
		{name: "unplayed count precedes paging", query: Query{UserID: "restricted", IsPlayed: &no, StartIndex: 1, Limit: 1}, want: []string{"movie-b"}, total: 3},
		{name: "favorite count precedes paging", query: Query{UserID: "restricted", IsFavorite: &no, StartIndex: 1, Limit: 1}, want: []string{"movie-b"}, total: 3},
		{name: "another user has different played items", query: Query{UserID: "default", IsPlayed: &yes}, want: []string{"episode-b1"}, total: 1},
		{name: "another user has different favorite items", query: Query{UserID: "default", IsFavorite: &yes}, want: []string{"video-b"}, total: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.query.Ids = ids
			test.query.SortBy = "Name"
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			assertUserDataQueryResult(t, result, test.want, test.total)
		})
	}
}

func TestQueryResumeUsesCurrentUserStateAndStableLastPlayedOrder(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	yes := true
	all := []string{"audio-b1", "episode-b1", "movie-tie-b", "movie-b", "movie-null-date-b"}
	tests := []struct {
		name  string
		query Query
		want  []string
		total int
	}{
		{name: "default includes recursive media and sorts null dates last", query: Query{UserID: "restricted"}, want: all, total: 5},
		{name: "resume overrides caller sort", query: Query{UserID: "restricted", SortBy: "Name", SortOrder: "Descending"}, want: all, total: 5},
		{name: "filter and stable ordering precede paging", query: Query{UserID: "restricted", StartIndex: 1, Limit: 2}, want: []string{"episode-b1", "movie-tie-b"}, total: 5},
		{name: "empty page preserves eligible count", query: Query{UserID: "restricted", StartIndex: 5, Limit: 2}, want: []string{}, total: 5},
		{name: "favorites combine with current resume state", query: Query{UserID: "restricted", IsFavorite: &yes}, want: []string{"audio-b1", "episode-b1"}, total: 2},
		{name: "favorite video filter combines before counting", query: Query{UserID: "restricted", IsFavorite: &yes, MediaTypes: []string{"Video"}}, want: []string{"episode-b1"}, total: 1},
		{name: "parent scope recurses within the authorized library", query: Query{UserID: "restricted", ParentID: "series-b", IncludeItemTypes: []string{"Episode"}}, want: []string{"episode-b1"}, total: 1},
		{name: "played items cannot also be resumable", query: Query{UserID: "restricted", IsPlayed: &yes}, want: []string{}, total: 0},
		{name: "another user sees only their own resumable progress", query: Query{UserID: "default"}, want: []string{"video-b"}, total: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.QueryResume(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			assertUserDataQueryResult(t, result, test.want, test.total)
			for _, item := range result.Items {
				if item.UserData == nil || item.UserData.ItemID != item.ID || item.UserData.PlaybackPositionTicks <= 0 {
					t.Errorf("resume item %s user data = %+v, want current progress", item.ID, item.UserData)
				}
			}
		})
	}
	for _, userID := range []string{"disabled", "unknown"} {
		if _, err := store.QueryResume(ctx, Query{UserID: userID}); !errors.Is(err, ErrForbidden) {
			t.Errorf("resume user %s error = %v, want ErrForbidden", userID, err)
		}
	}
	if _, err := store.QueryResume(ctx, Query{UserID: "restricted", ParentID: "library-a"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("resume hidden parent error = %v, want ErrNotFound", err)
	}
}

func TestResumableQueriesUseCurrentDurationWithoutClampingStoredProgress(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	for _, id := range []string{"episode-b2", "audio-b2", "audio-b", "episode-b3", "orphan-episode-b", "movie-null-duration-b", "movie-zero-position-b", "video-b", "series-b", "album-b", "artist-b", "album-disc-b"} {
		t.Run(id, func(t *testing.T) {
			result, err := store.QueryResume(ctx, Query{UserID: "restricted", Ids: []string{id}})
			if err != nil {
				t.Fatal(err)
			}
			assertUserDataQueryResult(t, result, []string{}, 0)
		})
	}
	for _, test := range []struct {
		duration int64
		eligible bool
	}{{1000, true}, {300, false}, {200, false}, {1000, true}} {
		if _, err := store.pool.Exec(ctx, `UPDATE items SET media = jsonb_build_object('DurationTicks', $1::bigint)
			WHERE id = 'movie-b'`, test.duration*media.TicksPerSecond); err != nil {
			t.Fatal(err)
		}
		result, err := store.QueryResume(ctx, Query{UserID: "restricted", Ids: []string{"movie-b"}})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		if test.eligible {
			want = []string{"movie-b"}
		}
		assertUserDataQueryResult(t, result, want, len(want))
		item, err := store.GetItem(ctx, "restricted", "movie-b")
		if err != nil {
			t.Fatal(err)
		}
		if item.UserData == nil || item.UserData.PlaybackPositionTicks != 300*media.TicksPerSecond {
			t.Errorf("duration %d seconds user data = %+v, want stored position 300 seconds", test.duration, item.UserData)
		}
	}
	for _, test := range []struct {
		name               string
		duration, position int64
		eligible           bool
	}{
		{name: "duration below 120 seconds", duration: 119, position: 3},
		{name: "duration at 120 seconds", duration: 120, position: 3, eligible: true},
		{name: "position below two percent", duration: 1000, position: 19},
		{name: "position at two percent", duration: 1000, position: 20, eligible: true},
		{name: "position below ninety percent", duration: 1000, position: 899, eligible: true},
		{name: "position at ninety percent", duration: 1000, position: 900},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, `UPDATE items SET media = jsonb_build_object('DurationTicks', $1::bigint)
				WHERE id = 'movie-b'`, test.duration*media.TicksPerSecond); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, `UPDATE user_item_data SET playback_position_ticks = $1
				WHERE user_id = 'restricted' AND item_id = 'movie-b'`, test.position*media.TicksPerSecond); err != nil {
				t.Fatal(err)
			}
			result, err := store.QueryResume(ctx, Query{UserID: "restricted", Ids: []string{"movie-b"}})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{}
			if test.eligible {
				want = []string{"movie-b"}
			}
			assertUserDataQueryResult(t, result, want, len(want))
		})
	}
	t.Run("large tick values preserve overflow-safe percentage filtering", func(t *testing.T) {
		const duration int64 = 1<<63 - 1
		const position int64 = duration / 2
		if _, err := store.pool.Exec(ctx, `UPDATE items SET media = jsonb_build_object('DurationTicks', $1::bigint)
			WHERE id = 'movie-b'`, duration); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE user_item_data SET playback_position_ticks = $1
			WHERE user_id = 'restricted' AND item_id = 'movie-b'`, position); err != nil {
			t.Fatal(err)
		}
		result, err := store.QueryResume(ctx, Query{UserID: "restricted", Ids: []string{"movie-b"}})
		if err != nil {
			t.Fatal(err)
		}
		assertUserDataQueryResult(t, result, []string{"movie-b"}, 1)
		if result.Items[0].UserData == nil || result.Items[0].UserData.PlaybackPositionTicks != position {
			t.Errorf("large position user data = %+v, want stored position %d", result.Items[0].UserData, position)
		}
	})
}

func TestResumableItemAndLatestQueriesRetainTheirSortContracts(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	for _, test := range []struct {
		name  string
		query Query
		want  []string
	}{
		{name: "default uses last played order", query: Query{UserID: "restricted", Recursive: true, Resumable: true},
			want: []string{"audio-b1", "episode-b1", "movie-tie-b", "movie-b", "movie-null-date-b"}},
		{name: "explicit name order is retained", query: Query{UserID: "restricted", Recursive: true, Resumable: true, SortBy: "Name"},
			want: []string{"movie-null-date-b", "episode-b1", "movie-b", "audio-b1", "movie-tie-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.QueryItems(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			assertUserDataQueryResult(t, result, test.want, 5)
		})
	}
	latest, err := store.QueryLatest(ctx, Query{UserID: "restricted", Resumable: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, latest, []latestExpectation{
		{"audio-b1", 1}, {"episode-b1", 1}, {"movie-b", 1}, {"movie-tie-b", 1}, {"movie-null-date-b", 1},
	})
	latest, err = store.QueryLatest(ctx, Query{UserID: "restricted", Resumable: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, latest, []latestExpectation{
		{"album-b", 1}, {"movie-b", 1}, {"series-b", 1}, {"movie-tie-b", 1}, {"movie-null-date-b", 1},
	})
}

func TestLatestGroupsAttachRepresentativeUserDataAfterSourceFiltering(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	yes := true
	query := Query{UserID: "restricted", Ids: []string{"episode-b1", "episode-b2", "audio-b1", "audio-b2", "movie-a"}, IsFavorite: &yes}
	latest, err := store.QueryLatest(ctx, query, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, latest, []latestExpectation{{"album-b", 1}, {"series-b", 1}})
	for _, item := range latest {
		unplayed := 1
		if item.Item.ID == "album-b" {
			unplayed = 2
		}
		assertLibraryUserData(t, item.Item, userDataQueryFolderState(item.Item.ID, unplayed, false, false))
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM user_item_data
		WHERE user_id = 'restricted' AND item_id IN ('series-b', 'album-b')`); err != nil {
		t.Fatal(err)
	}
	latest, err = store.QueryLatest(ctx, query, true)
	if err != nil {
		t.Fatal(err)
	}
	assertLatestItems(t, latest, []latestExpectation{{"album-b", 1}, {"series-b", 1}})
	if latest[0].Item.UserData == latest[1].Item.UserData {
		t.Fatal("latest representatives share a default user data object")
	}
	for _, item := range latest {
		unplayed := 1
		if item.Item.ID == "album-b" {
			unplayed = 2
		}
		assertLibraryUserData(t, item.Item, userDataQueryFolderState(item.Item.ID, unplayed, false, false))
	}
}

func assertLibraryUserData(t *testing.T, item Item, want UserData) {
	t.Helper()
	if item.UserData == nil {
		t.Fatalf("item %s user data is nil, want %+v", item.ID, want)
	}
	if want.UnplayedItemCount != nil && item.UserData.UnplayedItemCount == nil {
		t.Fatalf("folder %s unplayed item count is nil, want %d", item.ID, *want.UnplayedItemCount)
	}
	got := *item.UserData
	if (got.LastPlayedDate == nil) != (want.LastPlayedDate == nil) ||
		got.LastPlayedDate != nil && !got.LastPlayedDate.Equal(*want.LastPlayedDate) {
		t.Errorf("item %s last played date = %v, want %v", item.ID, got.LastPlayedDate, want.LastPlayedDate)
	}
	got.LastPlayedDate, want.LastPlayedDate = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("item %s user data = %+v, want %+v", item.ID, got, want)
	}
}

func assertUserDataQueryResult(t *testing.T, result ItemResult, want []string, total int) {
	t.Helper()
	if result.Items == nil || !reflect.DeepEqual(queryItemIDs(result.Items), want) || result.TotalRecordCount != total {
		t.Fatalf("query IDs = %v, total = %d, want %v and total %d", queryItemIDs(result.Items), result.TotalRecordCount, want, total)
	}
}

func userDataQueryDate(day int) *time.Time {
	date := time.Date(2025, time.January, day, 10, 0, 0, 0, time.UTC)
	return &date
}

func userDataQueryFolderState(itemID string, unplayed int, played, favorite bool) UserData {
	return UserData{ItemID: itemID, UnplayedItemCount: &unplayed, Played: played, IsFavorite: favorite}
}

func userDataQueryEpisodeState(userID string) UserData {
	if userID == "restricted" {
		return UserData{ItemID: "episode-b1", PlaybackPositionTicks: 100 * media.TicksPerSecond, PlayCount: 2, IsFavorite: true, LastPlayedDate: userDataQueryDate(2)}
	}
	date := time.Date(2025, time.February, 1, 10, 0, 0, 0, time.UTC)
	return UserData{ItemID: "episode-b1", PlaybackPositionTicks: 700 * media.TicksPerSecond, PlayCount: 9, Played: true, LastPlayedDate: &date}
}

func seedLibraryUserDataQueryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedLibraryLatestFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, type, is_folder, created_at) VALUES
		('video-b', 'library-b', 'library-b', 'Z Video', 'Z Video', 'Video', false, '2024-12-28T10:00:00Z'),
		('artist-b', 'library-b', 'library-b', 'Visible Artist', 'Visible Artist', 'MusicArtist', true, '2024-12-28T10:00:00Z'),
		('movie-tie-b', 'library-b', 'library-b', 'Z Tie Movie', 'Z Tie Movie', 'Movie', false, '2024-12-30T10:00:00Z'),
		('movie-null-date-b', 'library-b', 'library-b', '0 Null Date', '0 Null Date', 'Movie', false, '2024-12-29T10:00:00Z'),
		('movie-null-duration-b', 'library-b', 'library-b', 'Null Duration', 'Null Duration', 'Movie', false, '2024-12-28T10:00:00Z'),
		('movie-zero-position-b', 'library-b', 'library-b', 'Zero Position', 'Zero Position', 'Movie', false, '2024-12-28T10:00:00Z');
		UPDATE items SET media = '{"DurationTicks":1000}'::jsonb;
		UPDATE items SET media = '{"DurationTicks":2000}'::jsonb WHERE id = 'audio-b1';
		UPDATE items SET media = '{"DurationTicks":0}'::jsonb WHERE id = 'audio-b';
		UPDATE items SET media = NULL WHERE id = 'episode-b3';
		UPDATE items SET media = '{}'::jsonb WHERE id = 'orphan-episode-b';
		UPDATE items SET media = '{"DurationTicks":null}'::jsonb WHERE id = 'movie-null-duration-b';
		INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at) VALUES
		('restricted', 'episode-b1', 100, 2, true, false, '2025-01-02T10:00:00Z'),
		('restricted', 'episode-b2', 200, 3, false, true, '2025-01-03T10:00:00Z'),
		('restricted', 'movie-b', 300, 4, false, false, '2025-01-01T10:00:00Z'),
		('restricted', 'audio-b1', 400, 5, true, false, '2025-01-04T10:00:00Z'),
		('restricted', 'audio-b2', 1000, 1, false, false, '2025-01-07T10:00:00Z'),
		('restricted', 'audio-b', 100, 1, false, false, '2025-01-08T10:00:00Z'),
		('restricted', 'episode-b3', 100, 1, false, false, '2025-01-09T10:00:00Z'),
		('restricted', 'orphan-episode-b', 100, 1, false, false, '2025-01-10T10:00:00Z'),
		('restricted', 'movie-null-duration-b', 100, 1, false, false, '2025-01-10T10:00:00Z'),
		('restricted', 'movie-zero-position-b', 0, 0, false, false, '2025-01-10T10:00:00Z'),
		('restricted', 'movie-tie-b', 250, 1, false, false, '2025-01-02T10:00:00Z'),
		('restricted', 'movie-null-date-b', 250, 1, false, false, NULL),
		('restricted', 'movie-a', 500, 7, true, false, '2025-01-12T10:00:00Z'),
		('restricted', 'cross-library-child', 500, 7, false, false, '2025-01-11T10:00:00Z'),
		('restricted', 'series-b', 9000, 11, false, true, '2025-01-15T10:00:00Z'),
		('restricted', 'album-b', 8000, 12, false, true, '2025-01-16T10:00:00Z'),
		('restricted', 'library-b', 100, 1, true, true, '2025-01-17T10:00:00Z'),
		('restricted', 'album-disc-b', 100, 1, true, false, '2025-01-17T10:00:00Z'),
		('restricted', 'artist-b', 100, 1, false, false, '2025-01-17T10:00:00Z'),
		('default', 'episode-b1', 700, 9, false, true, '2025-02-01T10:00:00Z'),
		('default', 'video-b', 600, 8, true, false, '2025-02-02T10:00:00Z');
		UPDATE items SET media = jsonb_set(media, '{DurationTicks}',
			to_jsonb((media ->> 'DurationTicks')::bigint * 10000000::bigint))
		WHERE jsonb_typeof(media -> 'DurationTicks') = 'number';
		UPDATE user_item_data SET playback_position_ticks = playback_position_ticks * 10000000::bigint`); err != nil {
		t.Fatalf("seed user data query fixtures: %v", err)
	}
}
