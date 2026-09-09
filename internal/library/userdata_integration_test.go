package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func userDataCatalogFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, allowedRoot, name string) (Library, map[string]string) {
	t.Helper()
	root := filepath.Join(allowedRoot, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("create user data fixture root: %v", err)
	}
	library := libraryIntegrationCreate(t, ctx, store, name, "mixed", root)
	ids := make(map[string]string)
	for _, kind := range []string{"Movie", "Series", "Season", "Episode", "Video", "Audio", "MusicAlbum", "MusicArtist", "Folder"} {
		id := library.ID + "-" + kind
		folder := kind == "Series" || kind == "Season" || kind == "MusicAlbum" || kind == "MusicArtist" || kind == "Folder"
		if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder)
			VALUES ($1, $2, $2, $3, lower($3), $3, $4)`, id, library.ID, kind, folder); err != nil {
			t.Fatalf("insert %s user data fixture: %v", kind, err)
		}
		ids[kind] = id
	}
	return library, ids
}

func userDataAssertValue(t *testing.T, actual, expected UserData) {
	t.Helper()
	actualFields, expectedFields := actual, expected
	actualFields.LastPlayedDate, expectedFields.LastPlayedDate = nil, nil
	sameDate := actual.LastPlayedDate == nil && expected.LastPlayedDate == nil
	if actual.LastPlayedDate != nil && expected.LastPlayedDate != nil {
		sameDate = actual.LastPlayedDate.Equal(*expected.LastPlayedDate)
	}
	if !reflect.DeepEqual(actualFields, expectedFields) || !sameDate {
		t.Errorf("user data = %+v, want %+v", actual, expected)
	}
}

func userDataCount(value int) *int {
	return &value
}

func userDataDefaultForKind(itemID, kind string) UserData {
	data := UserData{ItemID: itemID}
	if kind == "Series" || kind == "Season" || kind == "MusicAlbum" || kind == "MusicArtist" {
		data.UnplayedItemCount = userDataCount(0)
	}
	return data
}

func userDataAssertDenied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
		t.Errorf("user data access was not denied: got %v", err)
	}
}

func userDataSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string, data UserData) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, item_id) DO UPDATE SET playback_position_ticks = EXCLUDED.playback_position_ticks,
		play_count = EXCLUDED.play_count, is_favorite = EXCLUDED.is_favorite,
		played = EXCLUDED.played, last_played_at = EXCLUDED.last_played_at`,
		userID, data.ItemID, data.PlaybackPositionTicks, data.PlayCount, data.IsFavorite, data.Played, data.LastPlayedDate); err != nil {
		t.Fatalf("seed existing user item data: %v", err)
	}
}

func TestStoreUserDataDefaultsAndSupportedItemTypes(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "default-user-data")
	requested := []string{library.ID, ids["Folder"], "unknown-user-data-item", ids["Movie"]}
	for kind, id := range ids {
		if kind == "Folder" {
			continue
		}
		data, err := store.GetUserData(ctx, userID, id)
		if err != nil {
			t.Fatalf("read default %s user data: %v", kind, err)
		}
		want := userDataDefaultForKind(id, kind)
		userDataAssertValue(t, data, want)
		item, err := store.GetItem(ctx, userID, id)
		if err != nil || item.UserData == nil {
			t.Fatalf("supported %s item has no default UserData projection: item = %+v, error = %v", kind, item, err)
		}
		userDataAssertValue(t, *item.UserData, want)
		requested = append(requested, id)
	}
	var entityID int64
	if err := pool.QueryRow(ctx, "INSERT INTO catalog_entities (kind, name) VALUES ('Genre', 'User data entity') RETURNING id").Scan(&entityID); err != nil {
		t.Fatalf("insert numeric entity ID fixture: %v", err)
	}
	numericID := strconv.FormatInt(entityID, 10)
	requested = append(requested, numericID)
	unsupportedIDs := []string{library.ID, ids["Folder"], numericID, "unknown-user-data-item"}
	for _, id := range unsupportedIDs {
		if _, err := store.GetUserData(ctx, userID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsupported user data item %q: got %v, want ErrNotFound", id, err)
		}
		if _, err := store.SetFavorite(ctx, userID, id, true); !errors.Is(err, ErrNotFound) {
			t.Errorf("favorite unsupported item %q: got %v, want ErrNotFound", id, err)
		}
		for _, played := range []bool{true, false} {
			if _, err := store.SetPlayed(ctx, userID, id, played, nil); !errors.Is(err, ErrNotFound) {
				t.Errorf("set unsupported item %q played=%v: got %v, want ErrNotFound", id, played, err)
			}
		}
	}
	var unsupportedCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data WHERE user_id = $1 AND item_id = ANY($2::text[])", userID, unsupportedIDs).Scan(&unsupportedCount); err != nil || unsupportedCount != 0 {
		t.Errorf("unsupported writes created %d user data rows, error = %v", unsupportedCount, err)
	}
	batch, err := store.GetUserDataBatch(ctx, userID, requested)
	if err != nil || len(batch) != 8 {
		t.Fatalf("default user data batch = %+v, error = %v; want eight supported items", batch, err)
	}
	for kind, id := range ids {
		if kind == "Folder" {
			continue
		}
		data, exists := batch[id]
		if !exists {
			t.Errorf("batch omitted default user data for supported %s item", kind)
			continue
		}
		userDataAssertValue(t, data, userDataDefaultForKind(id, kind))
	}
	for kind, id := range ids {
		if kind == "Folder" {
			continue
		}
		favorite, err := store.SetFavorite(ctx, userID, id, true)
		if err != nil {
			t.Fatalf("favorite supported %s item: %v", kind, err)
		}
		want := userDataDefaultForKind(id, kind)
		want.IsFavorite = true
		userDataAssertValue(t, favorite, want)
	}
}

func TestStoreFavoritesAreIndependentFromPlaybackPermissionAndOtherUsers(t *testing.T) {
	ctx, pool, store, allowedRoot, firstUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "favorite-isolation")
	secondUser := "other-favorite-viewer"
	libraryIntegrationUser(t, ctx, pool, secondUser, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, firstUser); err != nil {
		t.Fatalf("disable playback permission for favorite fixture: %v", err)
	}
	id := ids["Movie"]
	marked, err := store.SetFavorite(ctx, firstUser, id, true)
	if err != nil {
		t.Fatalf("favorite item without playback permission: %v", err)
	}
	userDataAssertValue(t, marked, UserData{ItemID: id, IsFavorite: true})
	other, err := store.GetUserData(ctx, secondUser, id)
	if err != nil {
		t.Fatalf("read other user's independent data: %v", err)
	}
	userDataAssertValue(t, other, UserData{ItemID: id})
	if _, err := store.SetFavorite(ctx, secondUser, id, true); err != nil {
		t.Fatalf("favorite item as the second user: %v", err)
	}
	cleared, err := store.SetFavorite(ctx, firstUser, id, false)
	if err != nil {
		t.Fatalf("clear first user's favorite: %v", err)
	}
	userDataAssertValue(t, cleared, UserData{ItemID: id})
	otherBatch, err := store.GetUserDataBatch(ctx, secondUser, []string{id})
	if err != nil || len(otherBatch) != 1 {
		t.Fatalf("read other user's favorite batch: data = %+v, error = %v", otherBatch, err)
	}
	userDataAssertValue(t, otherBatch[id], UserData{ItemID: id, IsFavorite: true})
}

func TestStoreUserDataPolicyChangesApplyToEveryReadAndWrite(t *testing.T) {
	ctx, pool, store, allowedRoot, unrestrictedID := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible, visibleIDs := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "visible-user-data")
	_, hiddenIDs := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "hidden-user-data")
	viewerID := "restricted-user-data-viewer"
	libraryIntegrationUser(t, ctx, pool, viewerID, false, false, []string{visible.ID})
	visibleID, hiddenID := visibleIDs["Movie"], hiddenIDs["Movie"]
	if _, err := store.SetFavorite(ctx, viewerID, visibleID, true); err != nil {
		t.Fatalf("create favorite before policy restriction: %v", err)
	}
	_, err := store.GetUserData(ctx, viewerID, hiddenID)
	userDataAssertDenied(t, err)
	_, err = store.SetFavorite(ctx, viewerID, hiddenID, true)
	userDataAssertDenied(t, err)
	_, err = store.SetPlayed(ctx, viewerID, hiddenID, true, nil)
	userDataAssertDenied(t, err)
	var hiddenStateExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM user_item_data WHERE user_id = $1 AND item_id = $2)", viewerID, hiddenID).Scan(&hiddenStateExists); err != nil || hiddenStateExists {
		t.Errorf("denied favorite write created hidden user data: exists = %v, error = %v", hiddenStateExists, err)
	}
	batch, err := store.GetUserDataBatch(ctx, viewerID, []string{visibleID, hiddenID, visible.ID, visibleIDs["Folder"], "unknown-user-data-item"})
	if err != nil || len(batch) != 1 {
		t.Fatalf("authorized user data batch = %+v, error = %v", batch, err)
	}
	userDataAssertValue(t, batch[visibleID], UserData{ItemID: visibleID, IsFavorite: true})
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnabledFolders}', '[]'::jsonb) WHERE id = $1`, viewerID); err != nil {
		t.Fatalf("remove allowed library from current policy: %v", err)
	}
	_, err = store.GetUserData(ctx, viewerID, visibleID)
	userDataAssertDenied(t, err)
	_, err = store.SetFavorite(ctx, viewerID, visibleID, false)
	userDataAssertDenied(t, err)
	_, err = store.SetPlayed(ctx, viewerID, visibleID, true, nil)
	userDataAssertDenied(t, err)
	if batch, err := store.GetUserDataBatch(ctx, viewerID, []string{visibleID, hiddenID}); err != nil || len(batch) != 0 {
		t.Errorf("restricted batch exposed formerly allowed data: batch = %+v, error = %v", batch, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnabledFolders}', jsonb_build_array($2::text)) WHERE id = $1`, viewerID, visible.ID); err != nil {
		t.Fatalf("restore allowed library for user data fixture: %v", err)
	}
	retained, err := store.GetUserData(ctx, viewerID, visibleID)
	if err != nil {
		t.Fatalf("read data after restoring library access: %v", err)
	}
	userDataAssertValue(t, retained, UserData{ItemID: visibleID, IsFavorite: true})
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", viewerID); err != nil {
		t.Fatalf("disable user data viewer: %v", err)
	}
	if _, err := store.GetUserData(ctx, viewerID, visibleID); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled user data read: got %v, want ErrForbidden", err)
	}
	if _, err := store.SetFavorite(ctx, viewerID, visibleID, false); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled favorite write: got %v, want ErrForbidden", err)
	}
	if _, err := store.SetPlayed(ctx, viewerID, visibleID, true, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled played-state write: got %v, want ErrForbidden", err)
	}
	if _, err := store.GetUserDataBatch(ctx, viewerID, []string{visibleID}); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled user data batch: got %v, want ErrForbidden", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = false WHERE id = $1", viewerID); err != nil {
		t.Fatalf("reenable user data viewer: %v", err)
	}
	retained, err = store.GetUserData(ctx, viewerID, visibleID)
	if err != nil {
		t.Fatalf("read data after reenabling viewer: %v", err)
	}
	userDataAssertValue(t, retained, UserData{ItemID: visibleID, IsFavorite: true})
	unrelated, err := store.GetUserData(ctx, unrestrictedID, visibleID)
	if err != nil {
		t.Fatalf("read unaffected user's data: %v", err)
	}
	userDataAssertValue(t, unrelated, UserData{ItemID: visibleID})
}

func TestStoreFavoritePreservesExistingPlaybackState(t *testing.T) {
	ctx, pool, store, allowedRoot, firstUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "favorite-playback-state")
	id := ids["Movie"]
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_build_object('DurationTicks', 1000::bigint) WHERE id = $1", id); err != nil {
		t.Fatalf("seed media duration for favorite state: %v", err)
	}
	lastPlayed := time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC)
	original := UserData{ItemID: id, PlaybackPositionTicks: 321, PlayCount: 4, Played: true, LastPlayedDate: &lastPlayed}
	userDataSeed(t, ctx, pool, firstUser, original)
	secondUser := "second-playback-state-viewer"
	libraryIntegrationUser(t, ctx, pool, secondUser, false, true, nil)
	otherDate := lastPlayed.Add(time.Hour)
	otherOriginal := UserData{ItemID: id, PlaybackPositionTicks: 777, PlayCount: 2, IsFavorite: true, LastPlayedDate: &otherDate}
	userDataSeed(t, ctx, pool, secondUser, otherOriginal)
	for _, favorite := range []bool{true, false} {
		updated, err := store.SetFavorite(ctx, firstUser, id, favorite)
		if err != nil {
			t.Fatalf("update favorite without changing existing playback state: %v", err)
		}
		want := original
		want.IsFavorite = favorite
		userDataAssertValue(t, updated, want)
		persisted, err := store.GetUserData(ctx, firstUser, id)
		if err != nil {
			t.Fatalf("read preserved playback state: %v", err)
		}
		userDataAssertValue(t, persisted, want)
		other, err := store.GetUserData(ctx, secondUser, id)
		if err != nil {
			t.Fatalf("read other user's preserved playback state: %v", err)
		}
		userDataAssertValue(t, other, otherOriginal)
	}
}

func TestStoreResumeAndItemUserDataAreScopedToTheCurrentUser(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible, visibleIDs := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "resume-visible")
	hidden, hiddenIDs := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "resume-hidden")
	firstUser, secondUser := "resume-first-viewer", "resume-second-viewer"
	libraryIntegrationUser(t, ctx, pool, firstUser, false, false, []string{visible.ID})
	libraryIntegrationUser(t, ctx, pool, secondUser, false, false, []string{visible.ID})
	const durationTicks = 1000 * media.TicksPerSecond
	if _, err := pool.Exec(ctx, `UPDATE items SET media = jsonb_build_object('DurationTicks', $2::bigint)
		WHERE library_id = ANY($1::text[]) AND type <> 'CollectionFolder'`, []string{visible.ID, hidden.ID}, durationTicks); err != nil {
		t.Fatalf("seed resume fixture durations: %v", err)
	}
	idleID := visible.ID + "-idle-movie"
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, media)
		VALUES ($1, $2, $2, 'Idle Movie', 'idle movie', 'Movie', jsonb_build_object('DurationTicks', $3::bigint))`, idleID, visible.ID, durationTicks); err != nil {
		t.Fatalf("insert movie without resume data: %v", err)
	}
	baseDate := time.Date(2025, time.July, 8, 0, 0, 0, 0, time.UTC)
	dateAt := func(hour int) *time.Time {
		value := baseDate.Add(time.Duration(hour) * time.Hour)
		return &value
	}
	firstMovie := UserData{ItemID: visibleIDs["Movie"], PlaybackPositionTicks: 100 * media.TicksPerSecond, PlayCount: 2, IsFavorite: true, LastPlayedDate: dateAt(10)}
	firstEpisode := UserData{ItemID: visibleIDs["Episode"], PlaybackPositionTicks: 200 * media.TicksPerSecond, PlayCount: 1, LastPlayedDate: dateAt(12)}
	firstVideo := UserData{ItemID: visibleIDs["Video"], PlaybackPositionTicks: 300 * media.TicksPerSecond, PlayCount: 3, Played: true, LastPlayedDate: dateAt(15)}
	for _, data := range []UserData{
		firstMovie, firstEpisode, firstVideo,
		{ItemID: visibleIDs["Audio"], PlaybackPositionTicks: durationTicks, PlayCount: 1, LastPlayedDate: dateAt(14)},
		{ItemID: visibleIDs["Series"], PlaybackPositionTicks: 250 * media.TicksPerSecond, PlayCount: 1, LastPlayedDate: dateAt(16)},
		{ItemID: visibleIDs["MusicAlbum"], PlaybackPositionTicks: 350 * media.TicksPerSecond, PlayCount: 1, LastPlayedDate: dateAt(17)},
		{ItemID: hiddenIDs["Movie"], PlaybackPositionTicks: 400 * media.TicksPerSecond, PlayCount: 1, LastPlayedDate: dateAt(18)},
		{ItemID: visibleIDs["Folder"], PlaybackPositionTicks: 600 * media.TicksPerSecond, PlayCount: 1, LastPlayedDate: dateAt(23)},
	} {
		userDataSeed(t, ctx, pool, firstUser, data)
	}
	secondMovie := UserData{ItemID: visibleIDs["Movie"], PlaybackPositionTicks: 500 * media.TicksPerSecond, PlayCount: 9, LastPlayedDate: dateAt(20)}
	secondEpisode := UserData{ItemID: visibleIDs["Episode"], PlayCount: 1, Played: true, LastPlayedDate: dateAt(21)}
	secondAudio := UserData{ItemID: visibleIDs["Audio"], PlaybackPositionTicks: 150 * media.TicksPerSecond, PlayCount: 2, LastPlayedDate: dateAt(22)}
	for _, data := range []UserData{secondMovie, secondEpisode, secondAudio} {
		userDataSeed(t, ctx, pool, secondUser, data)
	}
	resume, err := store.QueryResume(ctx, Query{UserID: firstUser})
	if err != nil || resume.TotalRecordCount != 2 || len(resume.Items) != 2 || resume.Items[0].ID != firstEpisode.ItemID || resume.Items[1].ID != firstMovie.ItemID {
		t.Fatalf("first user's authorized resume list = %+v, error = %v", resume, err)
	}
	for index, want := range []UserData{firstEpisode, firstMovie} {
		if resume.Items[index].UserData == nil {
			t.Fatalf("resume item %s has no user data", want.ItemID)
		}
		userDataAssertValue(t, *resume.Items[index].UserData, want)
	}
	page, err := store.QueryResume(ctx, Query{UserID: firstUser, StartIndex: 1, Limit: 1})
	if err != nil || page.TotalRecordCount != 2 || len(page.Items) != 1 || page.Items[0].ID != firstMovie.ItemID {
		t.Errorf("resume authorization and user filtering must precede counting and paging: result = %+v, error = %v", page, err)
	}
	otherResume, err := store.QueryResume(ctx, Query{UserID: secondUser})
	if err != nil || otherResume.TotalRecordCount != 2 || len(otherResume.Items) != 2 || otherResume.Items[0].ID != secondAudio.ItemID || otherResume.Items[1].ID != secondMovie.ItemID {
		t.Fatalf("second user's resume list leaked the first user's state: result = %+v, error = %v", otherResume, err)
	}
	for index, want := range []UserData{secondAudio, secondMovie} {
		if otherResume.Items[index].UserData == nil {
			t.Fatalf("second user's resume item %s has no user data", want.ItemID)
		}
		userDataAssertValue(t, *otherResume.Items[index].UserData, want)
	}
	for _, fixture := range []struct {
		userID string
		want   UserData
	}{{firstUser, firstMovie}, {secondUser, secondMovie}} {
		item, err := store.GetItem(ctx, fixture.userID, fixture.want.ItemID)
		if err != nil || item.UserData == nil {
			t.Fatalf("item detail lacks its user's data: item = %+v, error = %v", item, err)
		}
		userDataAssertValue(t, *item.UserData, fixture.want)
	}
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: secondUser, Ids: []string{firstMovie.ItemID, firstEpisode.ItemID, firstVideo.ItemID, visibleIDs["Folder"], visible.ID, idleID}})
	if items.TotalRecordCount != 6 || len(items.Items) != 6 {
		t.Fatalf("item user data projection fixture = %+v", items)
	}
	for _, item := range items.Items {
		switch item.ID {
		case visibleIDs["Folder"], visible.ID:
			if item.UserData != nil {
				t.Errorf("unsupported item type exposed user data: %+v", item)
			}
		default:
			if item.UserData == nil {
				t.Fatalf("supported item %s has no default user data", item.ID)
			}
			want := UserData{ItemID: item.ID}
			if item.ID == secondMovie.ItemID {
				want = secondMovie
			} else if item.ID == secondEpisode.ItemID {
				want = secondEpisode
			}
			userDataAssertValue(t, *item.UserData, want)
		}
	}
	batch, err := store.GetUserDataBatch(ctx, firstUser, []string{firstMovie.ItemID, firstEpisode.ItemID, hiddenIDs["Movie"], visibleIDs["Folder"]})
	if err != nil || len(batch) != 2 {
		t.Fatalf("resume user data batch exposed hidden or unsupported state: batch = %+v, error = %v", batch, err)
	}
	userDataAssertValue(t, batch[firstMovie.ItemID], firstMovie)
	userDataAssertValue(t, batch[firstEpisode.ItemID], firstEpisode)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnabledFolders}', '[]'::jsonb) WHERE id = $1`, firstUser); err != nil {
		t.Fatalf("restrict resume user's current policy: %v", err)
	}
	if restricted, err := store.QueryResume(ctx, Query{UserID: firstUser}); err != nil || restricted.TotalRecordCount != 0 || len(restricted.Items) != 0 {
		t.Errorf("resume retained data after policy restriction: result = %+v, error = %v", restricted, err)
	}
}

func TestStoreSetPlayedPreservesFavoriteAndExistingCount(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "manual-played-state")
	id := ids["Movie"]
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_build_object('DurationTicks', $2::bigint) WHERE id = $1", id, 600*media.TicksPerSecond); err != nil {
		t.Fatalf("seed duration for manual played state: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, userID); err != nil {
		t.Fatalf("disable playback while retaining manual state editing: %v", err)
	}
	previousDate := time.Date(2025, time.March, 4, 5, 6, 7, 0, time.UTC)
	original := UserData{ItemID: id, PlaybackPositionTicks: 120 * media.TicksPerSecond, PlayCount: 7, IsFavorite: true, LastPlayedDate: &previousDate}
	userDataSeed(t, ctx, pool, userID, original)
	otherUser := "other-manual-played-viewer"
	libraryIntegrationUser(t, ctx, pool, otherUser, false, true, nil)
	otherOriginal := UserData{ItemID: id, PlaybackPositionTicks: 80 * media.TicksPerSecond, PlayCount: 3}
	userDataSeed(t, ctx, pool, otherUser, otherOriginal)
	expected := original
	expected.PlaybackPositionTicks, expected.Played = 0, true
	for attempt := 0; attempt < 2; attempt++ {
		marked, err := store.SetPlayed(ctx, userID, id, true, nil)
		if err != nil {
			t.Fatalf("mark existing history played on attempt %d: %v", attempt+1, err)
		}
		userDataAssertValue(t, marked, expected)
	}
	explicitDate := previousDate.Add(48 * time.Hour)
	marked, err := store.SetPlayed(ctx, userID, id, true, &explicitDate)
	if err != nil {
		t.Fatalf("set explicit played date: %v", err)
	}
	expected.LastPlayedDate = &explicitDate
	userDataAssertValue(t, marked, expected)
	if repeated, err := store.SetPlayed(ctx, userID, id, true, nil); err != nil {
		t.Fatalf("repeat played state after an explicit date: %v", err)
	} else {
		userDataAssertValue(t, repeated, expected)
	}
	for _, date := range []*time.Time{nil, &explicitDate} {
		watchedWithProgress := expected
		watchedWithProgress.PlaybackPositionTicks = 240 * media.TicksPerSecond
		userDataSeed(t, ctx, pool, userID, watchedWithProgress)
		cleared, err := store.SetPlayed(ctx, userID, id, false, date)
		if err != nil {
			t.Fatalf("clear played history: %v", err)
		}
		userDataAssertValue(t, cleared, UserData{ItemID: id, IsFavorite: true})
	}
	persisted, err := store.GetUserData(ctx, userID, id)
	if err != nil {
		t.Fatalf("read cleared played state: %v", err)
	}
	userDataAssertValue(t, persisted, UserData{ItemID: id, IsFavorite: true})
	other, err := store.GetUserData(ctx, otherUser, id)
	if err != nil {
		t.Fatalf("read unaffected user's played state: %v", err)
	}
	userDataAssertValue(t, other, otherOriginal)
}

func TestStoreSetPlayedInitializesOnePlayAndUsesExistingOrCurrentDate(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "initial-played-state")
	var before, after time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatalf("read database time before setting played state: %v", err)
	}
	marked, err := store.SetPlayed(ctx, userID, ids["Movie"], true, nil)
	if err != nil {
		t.Fatalf("mark a previously unseen movie played: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&after); err != nil {
		t.Fatalf("read database time after setting played state: %v", err)
	}
	if marked.LastPlayedDate == nil || marked.LastPlayedDate.Before(before) || marked.LastPlayedDate.After(after) {
		t.Fatalf("initial played date was not the database's current time: data = %+v, before = %v, after = %v", marked, before, after)
	}
	userDataAssertValue(t, marked, UserData{ItemID: ids["Movie"], PlayCount: 1, Played: true, LastPlayedDate: marked.LastPlayedDate})
	repeated, err := store.SetPlayed(ctx, userID, ids["Movie"], true, nil)
	if err != nil {
		t.Fatalf("repeat initial played update: %v", err)
	}
	userDataAssertValue(t, repeated, marked)
	previousDate := time.Date(2024, time.December, 3, 2, 1, 0, 0, time.UTC)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: ids["Episode"], PlaybackPositionTicks: 50, IsFavorite: true, LastPlayedDate: &previousDate})
	episode, err := store.SetPlayed(ctx, userID, ids["Episode"], true, nil)
	if err != nil {
		t.Fatalf("mark an episode with an existing date played: %v", err)
	}
	userDataAssertValue(t, episode, UserData{ItemID: ids["Episode"], PlayCount: 1, IsFavorite: true, Played: true, LastPlayedDate: &previousDate})
	cleared, err := store.SetPlayed(ctx, userID, ids["Episode"], false, nil)
	if err != nil {
		t.Fatalf("clear the episode's history: %v", err)
	}
	userDataAssertValue(t, cleared, UserData{ItemID: ids["Episode"], IsFavorite: true})
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: ids["Series"], PlaybackPositionTicks: 50, PlayCount: 3, IsFavorite: true, Played: true, LastPlayedDate: &previousDate})
	for _, played := range []bool{true, false} {
		empty, err := store.SetPlayed(ctx, userID, ids["Series"], played, nil)
		if err != nil {
			t.Fatalf("set empty series played state: %v", err)
		}
		want := userDataDefaultForKind(ids["Series"], "Series")
		want.IsFavorite = true
		userDataAssertValue(t, empty, want)
	}
}

func TestStoreSetPlayedValidatesUTCDateAndReturnsSerializableUserData(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, "played-date-boundary")
	id := ids["Movie"]
	previousDate := time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC)
	original := UserData{ItemID: id, PlaybackPositionTicks: 120 * media.TicksPerSecond, PlayCount: 5, IsFavorite: true, LastPlayedDate: &previousDate}
	userDataSeed(t, ctx, pool, userID, original)
	var before string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(data)::text FROM user_item_data data WHERE user_id = $1 AND item_id = $2", userID, id).Scan(&before); err != nil {
		t.Fatalf("snapshot state before invalid UTC date: %v", err)
	}
	invalidDate, err := time.Parse(time.RFC3339, "9999-12-31T23:59:59-01:00")
	if err != nil {
		t.Fatalf("parse UTC overflow fixture: %v", err)
	}
	if _, err := store.SetPlayed(ctx, userID, id, true, &invalidDate); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("played date outside the supported UTC year range: got %v, want ErrInvalidInput", err)
	}
	var after string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(data)::text FROM user_item_data data WHERE user_id = $1 AND item_id = $2", userID, id).Scan(&after); err != nil || after != before {
		t.Errorf("invalid UTC date changed stored user data: before = %s, after = %s, error = %v", before, after, err)
	}
	validDate, err := time.Parse(time.RFC3339, "9999-12-31T23:59:59Z")
	if err != nil {
		t.Fatalf("parse valid maximum-year fixture: %v", err)
	}
	marked, err := store.SetPlayed(ctx, userID, id, true, &validDate)
	if err != nil {
		t.Fatalf("set valid maximum-year played date: %v", err)
	}
	expected := UserData{ItemID: id, PlayCount: 5, IsFavorite: true, Played: true, LastPlayedDate: &validDate}
	userDataAssertValue(t, marked, expected)
	persisted, err := store.GetUserData(ctx, userID, id)
	if err != nil {
		t.Fatalf("read maximum-year played date: %v", err)
	}
	userDataAssertValue(t, persisted, expected)
	if persisted.LastPlayedDate == nil || persisted.LastPlayedDate.Location() != time.UTC {
		t.Errorf("played date was not normalized to UTC: %+v", persisted.LastPlayedDate)
	}
	if _, err := json.Marshal(persisted); err != nil {
		t.Errorf("valid maximum-year user data could not be serialized: %v", err)
	}
}

type userDataSeriesTree struct {
	Series, FirstSeason, SecondSeason string
	Episodes                          []string
	ForeignEpisode                    string
}

func (tree userDataSeriesTree) IDs() []string {
	return append([]string{tree.Series, tree.FirstSeason, tree.SecondSeason}, tree.Episodes...)
}

func userDataSeriesTreeFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, allowedRoot, name string) userDataSeriesTree {
	t.Helper()
	library, ids := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, name)
	_, foreignIDs := userDataCatalogFixture(t, ctx, pool, store, allowedRoot, name+"-foreign")
	tree := userDataSeriesTree{
		Series: ids["Series"], FirstSeason: ids["Season"], SecondSeason: library.ID + "-season-two",
		Episodes:       []string{ids["Episode"], library.ID + "-episode-two", library.ID + "-episode-three"},
		ForeignEpisode: foreignIDs["Episode"],
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2, index_number = 1 WHERE id = $1", tree.FirstSeason, tree.Series); err != nil {
		t.Fatalf("attach first season to series: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2, index_number = 1, parent_index_number = 1 WHERE id = $1", tree.Episodes[0], tree.FirstSeason); err != nil {
		t.Fatalf("attach first episode to season: %v", err)
	}
	for _, item := range []struct {
		id, parent, name, kind string
		folder                 bool
		index, parentIndex     int
	}{
		{tree.SecondSeason, tree.Series, "Season Two", "Season", true, 2, 0},
		{tree.Episodes[1], tree.FirstSeason, "Episode Two", "Episode", false, 2, 1},
		{tree.Episodes[2], tree.SecondSeason, "Episode Three", "Episode", false, 1, 2},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO items
			(id, library_id, parent_id, name, sort_name, type, is_folder, index_number, parent_index_number)
			VALUES ($1, $2, $3, $4, lower($4), $5, $6, $7, $8)`,
			item.id, library.ID, item.parent, item.name, item.kind, item.folder, item.index, item.parentIndex); err != nil {
			t.Fatalf("insert watched-state hierarchy item: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2 WHERE id = $1", tree.ForeignEpisode, tree.FirstSeason); err != nil {
		t.Fatalf("create cross-library parent corruption fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_build_object('DurationTicks', $2::bigint) WHERE id = ANY($1::text[])", append(append([]string(nil), tree.Episodes...), tree.ForeignEpisode), 600*media.TicksPerSecond); err != nil {
		t.Fatalf("set watched-state episode durations: %v", err)
	}
	return tree
}

func userDataRowsSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemIDs []string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(data) ORDER BY user_id, item_id), '[]'::jsonb)::text
		FROM user_item_data data WHERE item_id = ANY($1::text[])`, itemIDs).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot complete subtree user data: %v", err)
	}
	return snapshot
}

func TestStoreSetPlayedRecursesWithinLibraryWhileFavoriteStaysOnOneItem(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	tree := userDataSeriesTreeFixture(t, ctx, pool, store, allowedRoot, "recursive-watched")
	previousDate := time.Date(2025, time.February, 3, 4, 5, 6, 0, time.UTC)
	initial := map[string]UserData{
		tree.Series:       {ItemID: tree.Series, PlaybackPositionTicks: 90, PlayCount: 11, Played: true, LastPlayedDate: &previousDate},
		tree.FirstSeason:  {ItemID: tree.FirstSeason, PlayCount: 9, Played: true, LastPlayedDate: &previousDate},
		tree.SecondSeason: {ItemID: tree.SecondSeason, IsFavorite: true},
		tree.Episodes[0]:  {ItemID: tree.Episodes[0], PlaybackPositionTicks: 30 * media.TicksPerSecond},
		tree.Episodes[1]:  {ItemID: tree.Episodes[1], PlayCount: 4, IsFavorite: true, Played: true, LastPlayedDate: &previousDate},
		tree.Episodes[2]:  {ItemID: tree.Episodes[2], PlaybackPositionTicks: 80 * media.TicksPerSecond, PlayCount: 7, LastPlayedDate: &previousDate},
	}
	for _, data := range initial {
		userDataSeed(t, ctx, pool, userID, data)
	}
	foreign := UserData{ItemID: tree.ForeignEpisode, PlaybackPositionTicks: 70 * media.TicksPerSecond, PlayCount: 5, IsFavorite: true, LastPlayedDate: &previousDate}
	userDataSeed(t, ctx, pool, userID, foreign)
	otherUser := "other-recursive-viewer"
	libraryIntegrationUser(t, ctx, pool, otherUser, false, true, nil)
	other := UserData{ItemID: tree.Episodes[0], PlaybackPositionTicks: 40 * media.TicksPerSecond, PlayCount: 2, IsFavorite: true}
	userDataSeed(t, ctx, pool, otherUser, other)
	before, err := store.GetUserData(ctx, userID, tree.Series)
	if err != nil {
		t.Fatalf("read derived series state before marking watched: %v", err)
	}
	userDataAssertValue(t, before, UserData{ItemID: tree.Series, UnplayedItemCount: userDataCount(2)})
	descendants := append([]string{tree.FirstSeason, tree.SecondSeason}, tree.Episodes...)
	beforeFavorite := userDataRowsSnapshot(t, ctx, pool, descendants)
	favorite, err := store.SetFavorite(ctx, userID, tree.Series, true)
	if err != nil {
		t.Fatalf("favorite series without changing descendants: %v", err)
	}
	userDataAssertValue(t, favorite, UserData{ItemID: tree.Series, IsFavorite: true, UnplayedItemCount: userDataCount(2)})
	if after := userDataRowsSnapshot(t, ctx, pool, descendants); after != beforeFavorite {
		t.Errorf("series favorite changed descendant state: before = %s, after = %s", beforeFavorite, after)
	}
	playedDate := previousDate.Add(24 * time.Hour)
	marked, err := store.SetPlayed(ctx, userID, tree.Series, true, &playedDate)
	if err != nil {
		t.Fatalf("recursively mark series played: %v", err)
	}
	userDataAssertValue(t, marked, UserData{ItemID: tree.Series, IsFavorite: true, Played: true, UnplayedItemCount: userDataCount(0)})
	batch, err := store.GetUserDataBatch(ctx, userID, tree.IDs())
	if err != nil || len(batch) != 6 {
		t.Fatalf("read recursively marked hierarchy: batch = %+v, error = %v", batch, err)
	}
	for _, folder := range []string{tree.Series, tree.FirstSeason, tree.SecondSeason} {
		userDataAssertValue(t, batch[folder], UserData{ItemID: folder, IsFavorite: folder != tree.FirstSeason, Played: true, UnplayedItemCount: userDataCount(0)})
	}
	for index, id := range tree.Episodes {
		count := []int{1, 4, 7}[index]
		userDataAssertValue(t, batch[id], UserData{ItemID: id, PlayCount: count, IsFavorite: initial[id].IsFavorite, Played: true, LastPlayedDate: &playedDate})
		withProgress := batch[id]
		withProgress.PlaybackPositionTicks = 150 * media.TicksPerSecond
		userDataSeed(t, ctx, pool, userID, withProgress)
	}
	cleared, err := store.SetPlayed(ctx, userID, tree.Series, false, nil)
	if err != nil {
		t.Fatalf("recursively clear series history: %v", err)
	}
	userDataAssertValue(t, cleared, UserData{ItemID: tree.Series, IsFavorite: true, UnplayedItemCount: userDataCount(3)})
	batch, err = store.GetUserDataBatch(ctx, userID, tree.IDs())
	if err != nil || len(batch) != 6 {
		t.Fatalf("read recursively cleared hierarchy: batch = %+v, error = %v", batch, err)
	}
	for _, folder := range []struct {
		id       string
		unplayed int
		favorite bool
	}{{tree.Series, 3, true}, {tree.FirstSeason, 2, false}, {tree.SecondSeason, 1, true}} {
		userDataAssertValue(t, batch[folder.id], UserData{ItemID: folder.id, IsFavorite: folder.favorite, UnplayedItemCount: userDataCount(folder.unplayed)})
	}
	for _, id := range tree.Episodes {
		userDataAssertValue(t, batch[id], UserData{ItemID: id, IsFavorite: initial[id].IsFavorite})
	}
	if retained, err := store.GetUserData(ctx, userID, tree.ForeignEpisode); err != nil {
		t.Fatalf("read unaffected cross-library episode: %v", err)
	} else {
		userDataAssertValue(t, retained, foreign)
	}
	if retained, err := store.GetUserData(ctx, otherUser, other.ItemID); err != nil {
		t.Fatalf("read unaffected other-user descendant: %v", err)
	} else {
		userDataAssertValue(t, retained, other)
	}
}

func TestStoreRecursivePlayedStateAndFolderCountsTerminateOnParentCycles(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	tree := userDataSeriesTreeFixture(t, ctx, pool, store, allowedRoot, "cyclic-watched")
	if _, err := pool.Exec(ctx, "UPDATE items SET parent_id = $2 WHERE id = $1", tree.Series, tree.FirstSeason); err != nil {
		t.Fatalf("create same-library parent cycle: %v", err)
	}
	cycleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	batch, err := store.GetUserDataBatch(cycleCtx, userID, []string{tree.Series, tree.FirstSeason, tree.SecondSeason})
	if err != nil || len(batch) != 3 {
		t.Fatalf("derive overlapping cyclic folder scopes: batch = %+v, error = %v", batch, err)
	}
	for _, folder := range []struct {
		id    string
		count int
	}{{tree.Series, 3}, {tree.FirstSeason, 3}, {tree.SecondSeason, 1}} {
		userDataAssertValue(t, batch[folder.id], UserData{ItemID: folder.id, UnplayedItemCount: userDataCount(folder.count)})
	}
	date := time.Date(2025, time.August, 9, 10, 11, 12, 0, time.UTC)
	marked, err := store.SetPlayed(cycleCtx, userID, tree.Series, true, &date)
	if err != nil {
		t.Fatalf("recursive mark did not terminate on a cycle: %v", err)
	}
	userDataAssertValue(t, marked, UserData{ItemID: tree.Series, Played: true, UnplayedItemCount: userDataCount(0)})
	for _, id := range tree.Episodes {
		data, err := store.GetUserData(cycleCtx, userID, id)
		if err != nil {
			t.Fatalf("read episode after cyclic recursive mark: %v", err)
		}
		userDataAssertValue(t, data, UserData{ItemID: id, PlayCount: 1, Played: true, LastPlayedDate: &date})
	}
	cleared, err := store.SetPlayed(cycleCtx, userID, tree.Series, false, nil)
	if err != nil {
		t.Fatalf("recursive clear did not terminate on a cycle: %v", err)
	}
	userDataAssertValue(t, cleared, UserData{ItemID: tree.Series, UnplayedItemCount: userDataCount(3)})
}

func TestStoreRecursivePlayedChildFailureRollsBackTheEntireSubtree(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	tree := userDataSeriesTreeFixture(t, ctx, pool, store, allowedRoot, "atomic-watched")
	date := time.Date(2025, time.September, 10, 11, 12, 13, 0, time.UTC)
	for _, id := range []string{tree.Series, tree.FirstSeason, tree.SecondSeason, tree.Episodes[0], tree.Episodes[2]} {
		userDataSeed(t, ctx, pool, userID, UserData{ItemID: id, PlaybackPositionTicks: 120 * media.TicksPerSecond, PlayCount: 4, IsFavorite: id == tree.Series, LastPlayedDate: &date})
	}
	before := userDataRowsSnapshot(t, ctx, pool, tree.IDs())
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin child update failure fixture: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `CREATE SEQUENCE watched_child_failure_hits;
		CREATE FUNCTION reject_watched_child_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.item_id LIKE '%-episode-three' THEN
				PERFORM nextval('watched_child_failure_hits');
				RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'Injected watched child update failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_watched_child_for_test BEFORE UPDATE ON user_item_data
		FOR EACH ROW EXECUTE FUNCTION reject_watched_child_for_test()`); err != nil {
		t.Fatalf("install child update failure fixture: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit child update failure fixture: %v", err)
	}
	removeFailure := func(cleanupCtx context.Context) error {
		cleanupTx, err := pool.Begin(cleanupCtx)
		if err != nil {
			return err
		}
		defer rollback(cleanupTx)
		if _, err := cleanupTx.Exec(cleanupCtx, `SET LOCAL lock_timeout = '2s';
			DROP TRIGGER IF EXISTS reject_watched_child_for_test ON user_item_data;
			DROP FUNCTION IF EXISTS reject_watched_child_for_test();
			DROP SEQUENCE IF EXISTS watched_child_failure_hits`); err != nil {
			return err
		}
		return cleanupTx.Commit(cleanupCtx)
	}
	failureActive := true
	t.Cleanup(func() {
		if failureActive {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := removeFailure(cleanupCtx); err != nil {
				t.Errorf("remove owned child failure fixture: %v", err)
			}
		}
	})
	_, err = store.SetPlayed(ctx, userID, tree.Series, true, &date)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("recursive update did not return the injected child failure: %v", err)
	}
	var triggered bool
	if err := pool.QueryRow(ctx, "SELECT is_called FROM watched_child_failure_hits").Scan(&triggered); err != nil || !triggered {
		t.Fatalf("recursive update did not reach the child trigger: triggered = %v, error = %v", triggered, err)
	}
	if after := userDataRowsSnapshot(t, ctx, pool, tree.IDs()); after != before {
		t.Errorf("child failure partially committed root or descendant state: before = %s, after = %s", before, after)
	}
	var initialized bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM user_item_data WHERE user_id = $1 AND item_id = $2)", userID, tree.Episodes[1]).Scan(&initialized); err != nil || initialized {
		t.Errorf("child failure retained a newly initialized sibling row: exists = %v, error = %v", initialized, err)
	}
	if err := removeFailure(ctx); err != nil {
		t.Fatalf("remove child failure before retry: %v", err)
	}
	failureActive = false
	marked, err := store.SetPlayed(ctx, userID, tree.Series, true, &date)
	if err != nil {
		t.Fatalf("retry recursive update after rollback: %v", err)
	}
	userDataAssertValue(t, marked, UserData{ItemID: tree.Series, IsFavorite: true, Played: true, UnplayedItemCount: userDataCount(0)})
	for _, id := range tree.Episodes {
		data, err := store.GetUserData(ctx, userID, id)
		if err != nil {
			t.Fatalf("read child after successful retry: %v", err)
		}
		count := 4
		if id == tree.Episodes[1] {
			count = 1
		}
		userDataAssertValue(t, data, UserData{ItemID: id, PlayCount: count, Played: true, LastPlayedDate: &date})
	}
}
