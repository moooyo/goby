package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func notificationCatalogFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, allowedRoot, name string) (Library, map[string]string) {
	t.Helper()
	root := filepath.Join(allowedRoot, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("create notification fixture root: %v", err)
	}
	library := libraryIntegrationCreate(t, ctx, store, name, "mixed", root)
	ids := map[string]string{"Library": library.ID}
	for _, item := range []struct {
		key, parent, kind string
		folder            bool
	}{
		{"Series", "Library", "Series", true},
		{"Season1", "Series", "Season", true},
		{"Season2", "Series", "Season", true},
		{"Episode1", "Season1", "Episode", false},
		{"Episode2", "Season1", "Episode", false},
		{"Episode3", "Season2", "Episode", false},
		{"UnrelatedMovie", "Library", "Movie", false},
		{"UnrelatedFolder", "Library", "Folder", true},
		{"UnrelatedAudio", "UnrelatedFolder", "Audio", false},
	} {
		ids[item.key] = library.ID + "-" + item.key
		if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type, is_folder)
			VALUES ($1, $2, $3, $4, $4, $5, $6)`, ids[item.key], library.ID, ids[item.parent], item.key, item.kind, item.folder); err != nil {
			t.Fatalf("insert notification fixture %s: %v", item.key, err)
		}
	}
	return library, ids
}

func notificationPage(t *testing.T, ctx context.Context, store *Store, query UserDataNotificationQuery) UserDataNotificationResult {
	t.Helper()
	result, err := store.UserDataNotificationPage(ctx, query)
	if err != nil {
		t.Fatalf("read notification page: %v", err)
	}
	return result
}

func notificationAssertIDs(t *testing.T, page UserDataNotificationResult, expected ...string) map[string]UserData {
	t.Helper()
	sort.Strings(expected)
	actual := make([]string, 0, len(page.Items))
	byID := make(map[string]UserData, len(page.Items))
	for _, data := range page.Items {
		actual = append(actual, data.ItemID)
		byID[data.ItemID] = data
	}
	if len(actual) != len(expected) || !sort.StringsAreSorted(actual) {
		t.Fatalf("notification IDs = %v, want sorted %v", actual, expected)
	}
	for index, id := range expected {
		if actual[index] != id {
			t.Fatalf("notification IDs = %v, want %v", actual, expected)
		}
	}
	return byID
}

func TestStoreUserDataNotificationsReadLatestTargetAndAncestorSummaries(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-ancestors")
	if _, err := store.SetPlayed(ctx, userID, ids["Episode1"], true, nil); err != nil {
		t.Fatalf("mark the first episode watched: %v", err)
	}
	query := UserDataNotificationQuery{UserID: userID, ItemID: ids["Episode1"]}
	page := notificationPage(t, ctx, store, query)
	values := notificationAssertIDs(t, page, ids["Episode1"], ids["Season1"], ids["Series"])
	if !values[ids["Episode1"]].Played || values[ids["Episode1"]].PlayCount != 1 {
		t.Errorf("notification omitted committed episode state: %+v", values[ids["Episode1"]])
	}
	userDataAssertValue(t, values[ids["Season1"]], UserData{ItemID: ids["Season1"], UnplayedItemCount: userDataCount(1)})
	userDataAssertValue(t, values[ids["Series"]], UserData{ItemID: ids["Series"], UnplayedItemCount: userDataCount(2)})
	if _, err := store.SetPlayed(ctx, userID, ids["Episode2"], true, nil); err != nil {
		t.Fatalf("complete the first season: %v", err)
	}
	if _, err := store.SetFavorite(ctx, userID, ids["Episode1"], true); err != nil {
		t.Fatalf("update the target after its earlier notification marker: %v", err)
	}
	values = notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Episode1"], ids["Season1"], ids["Series"])
	if !values[ids["Episode1"]].IsFavorite {
		t.Error("notification reused state from an earlier commit")
	}
	userDataAssertValue(t, values[ids["Season1"]], UserData{ItemID: ids["Season1"], Played: true, UnplayedItemCount: userDataCount(0)})
	userDataAssertValue(t, values[ids["Series"]], UserData{ItemID: ids["Series"], UnplayedItemCount: userDataCount(1)})
	// Invalid technical metadata must not affect a state-only notification read.
	if _, err := pool.Exec(ctx, `UPDATE items SET media = '{"DurationTicks":"not technical data"}'::jsonb,
		path = '/unavailable/notification-source' WHERE id = $1`, ids["Episode1"]); err != nil {
		t.Fatalf("replace unrelated source metadata: %v", err)
	}
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Episode1"], ids["Season1"], ids["Series"])
}

func TestStoreUserDataNotificationsRecursivelyCoverSeasonsWithoutUnrelatedItems(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library, ids := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-recursive")
	if _, err := store.SetPlayed(ctx, userID, ids["Series"], true, nil); err != nil {
		t.Fatalf("mark the series watched: %v", err)
	}
	query := UserDataNotificationQuery{UserID: userID, ItemID: ids["Series"], Recursive: true}
	values := notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"], ids["Season2"], ids["Episode1"], ids["Episode2"], ids["Episode3"])
	for _, value := range values {
		if !value.Played {
			t.Errorf("recursive notification omitted committed watched state: %+v", value)
		}
	}
	query.ItemID = ids["Season1"]
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"], ids["Episode1"], ids["Episode2"])
	query.Recursive = false
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"])
	query.ItemID, query.Recursive = ids["Episode1"], true
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"], ids["Episode1"])
	// Unsupported folders remain useful roots while their own state is omitted.
	query.ItemID = ids["UnrelatedFolder"]
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["UnrelatedAudio"])
	query.ItemID = library.ID
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"], ids["Season2"], ids["Episode1"], ids["Episode2"], ids["Episode3"], ids["UnrelatedMovie"], ids["UnrelatedAudio"])
	query.Recursive = false
	page := notificationPage(t, ctx, store, query)
	notificationAssertIDs(t, page)
	if page.Items == nil || page.NextAfterID != "" {
		t.Errorf("empty notification page must be a completed empty list: %+v", page)
	}
}

func TestStoreUserDataNotificationsUseBoundedExclusiveKeysetPages(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library, ids := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-pages")
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, parent_id, name, sort_name, type)
		SELECT $1::text || '-Page-' || lpad(number::text, 3, '0'), $1, $2, 'Page item', 'Page item', 'Episode'
		FROM generate_series(1, 270) AS number`, library.ID, ids["Series"]); err != nil {
		t.Fatalf("insert paginated notification items: %v", err)
	}
	expected := []string{ids["Series"], ids["Season1"], ids["Season2"], ids["Episode1"], ids["Episode2"], ids["Episode3"]}
	for number := 1; number <= 270; number++ {
		expected = append(expected, fmt.Sprintf("%s-Page-%03d", library.ID, number))
	}
	sort.Strings(expected)
	query := UserDataNotificationQuery{UserID: userID, ItemID: ids["Series"], Recursive: true}
	first := notificationPage(t, ctx, store, query)
	if len(first.Items) != 64 || first.NextAfterID != first.Items[63].ItemID {
		t.Fatalf("default notification page did not stop at 64 items: %+v", first)
	}
	query.Limit = 256
	maximum := notificationPage(t, ctx, store, query)
	if len(maximum.Items) != 256 || maximum.NextAfterID != maximum.Items[255].ItemID {
		t.Fatalf("maximum notification page was not bounded: %+v", maximum)
	}
	query.Limit = 37
	var actual []string
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > 10 {
			t.Fatal("notification cursor did not terminate")
		}
		page := notificationPage(t, ctx, store, query)
		for _, data := range page.Items {
			actual = append(actual, data.ItemID)
		}
		if page.NextAfterID == "" {
			break
		}
		if len(page.Items) != 37 || page.NextAfterID <= query.AfterID || page.NextAfterID != page.Items[len(page.Items)-1].ItemID {
			t.Fatalf("notification cursor did not advance exclusively: %+v", page)
		}
		query.AfterID = page.NextAfterID
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("notification pages duplicated, omitted, or included unrelated items: got %v, want %v", actual, expected)
	}
	query.AfterID = expected[len(expected)-1]
	last := notificationPage(t, ctx, store, query)
	if len(last.Items) != 0 || last.NextAfterID != "" {
		t.Errorf("exclusive final notification cursor returned extra items: %+v", last)
	}
}

func TestStoreUserDataNotificationsRecheckPermissionsAndIsolateUsers(t *testing.T) {
	ctx, pool, store, allowedRoot, otherUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	library, ids := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-visible")
	_, hidden := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-hidden")
	userID := "notification-restricted-viewer"
	libraryIntegrationUser(t, ctx, pool, userID, false, false, []string{library.ID})
	if _, err := store.SetPlayed(ctx, userID, ids["Episode1"], true, nil); err != nil {
		t.Fatalf("seed the restricted user's watched state: %v", err)
	}
	query := UserDataNotificationQuery{UserID: otherUser, ItemID: ids["Episode1"]}
	values := notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Episode1"], ids["Season1"], ids["Series"])
	userDataAssertValue(t, values[ids["Episode1"]], UserData{ItemID: ids["Episode1"]})
	userDataAssertValue(t, values[ids["Season1"]], UserData{ItemID: ids["Season1"], UnplayedItemCount: userDataCount(2)})
	query.UserID, query.ItemID = userID, hidden["Episode1"]
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Errorf("notification revealed a hidden target: %v", err)
	}
	query.ItemID = ids["Episode1"]
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, userID); err != nil {
		t.Fatalf("disable playback independently of browsing: %v", err)
	}
	values = notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Episode1"], ids["Season1"], ids["Series"])
	if !values[ids["Episode1"]].Played {
		t.Error("the visible user's state was replaced by another user's defaults")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnabledFolders}', '[]'::jsonb) WHERE id = $1`, userID); err != nil {
		t.Fatalf("revoke notification access: %v", err)
	}
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Errorf("notification reused stale library access: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled = true WHERE id = $1`, userID); err != nil {
		t.Fatalf("disable notification user: %v", err)
	}
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrForbidden) {
		t.Errorf("notification accepted a disabled user: %v", err)
	}
	query.UserID = "unknown-notification-user"
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrForbidden) {
		t.Errorf("notification accepted an unknown user: %v", err)
	}
}

func TestStoreUserDataNotificationsStopCrossLibraryEdgesAndCyclesAndRejectDeletedTargets(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	_, ids := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-tree")
	_, foreign := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "notification-foreign-tree")
	if _, err := pool.Exec(ctx, `UPDATE items SET parent_id = CASE id WHEN $1 THEN $2 ELSE $3 END WHERE id IN ($1, $4)`,
		ids["Series"], foreign["Season1"], ids["Season1"], foreign["Episode1"]); err != nil {
		t.Fatalf("insert cross-library ancestor and descendant edges: %v", err)
	}
	query := UserDataNotificationQuery{UserID: userID, ItemID: ids["Series"], Recursive: true}
	expected := []string{ids["Series"], ids["Season1"], ids["Season2"], ids["Episode1"], ids["Episode2"], ids["Episode3"]}
	values := notificationAssertIDs(t, notificationPage(t, ctx, store, query), expected...)
	userDataAssertValue(t, values[ids["Series"]], UserData{ItemID: ids["Series"], UnplayedItemCount: userDataCount(3)})
	userDataAssertValue(t, values[ids["Season1"]], UserData{ItemID: ids["Season1"], UnplayedItemCount: userDataCount(2)})
	query.ItemID, query.Recursive = ids["Episode1"], false
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), ids["Series"], ids["Season1"], ids["Episode1"])
	if _, err := pool.Exec(ctx, `UPDATE items SET parent_id = $2 WHERE id = $1`, ids["Series"], ids["Season2"]); err != nil {
		t.Fatalf("insert a cyclic ancestor relationship: %v", err)
	}
	cycleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	notificationAssertIDs(t, notificationPage(t, cycleCtx, store, query), ids["Series"], ids["Season1"], ids["Season2"], ids["Episode1"])
	query.ItemID, query.Recursive = ids["Series"], true
	notificationAssertIDs(t, notificationPage(t, cycleCtx, store, query), expected...)
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id = $1`, ids["Episode1"]); err != nil {
		t.Fatalf("delete a queued notification target: %v", err)
	}
	query.ItemID = ids["Episode1"]
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Errorf("notification returned state for a deleted target: %v", err)
	}
}

func TestNormalizeUserDataNotificationQueryRejectsInvalidBoundsAndIdentifiers(t *testing.T) {
	for _, query := range []UserDataNotificationQuery{
		{}, {UserID: "user"}, {UserID: " ", ItemID: "item"}, {UserID: "user", ItemID: " "},
		{UserID: "user", ItemID: "item", Limit: -1}, {UserID: "user", ItemID: "item", Limit: 257},
		{UserID: "user\x00", ItemID: "item"}, {UserID: "user", ItemID: "item\x00"},
		{UserID: "user", ItemID: "item", AfterID: "cursor\x00"},
		{UserID: "user", ItemID: "item", AfterID: string([]byte{0xff})},
	} {
		if _, err := normalizeUserDataNotificationQuery(query); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid notification query %+v was accepted: %v", query, err)
		}
	}
	query, err := normalizeUserDataNotificationQuery(UserDataNotificationQuery{UserID: "user", ItemID: "item"})
	if err != nil || query.Limit != 64 {
		t.Errorf("default notification page size = %d, error = %v", query.Limit, err)
	}
}
