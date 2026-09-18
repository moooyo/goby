package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func collectionUserDataCreate(t *testing.T, ctx context.Context, store *Store, owner Subject, kind string, input CollectionInput) CollectionInfo {
	t.Helper()
	collection, err := store.CreateCollection(ctx, owner, kind, input)
	if err != nil {
		t.Fatalf("create %s user data fixture: %v", kind, err)
	}
	return collection
}

func collectionUserDataShare(t *testing.T, ctx context.Context, store *Store, owner Subject, collection CollectionInfo, userIDs ...string) {
	t.Helper()
	shares := make([]CollectionShare, 0, len(userIDs))
	for _, userID := range userIDs {
		shares = append(shares, CollectionShare{UserID: userID})
	}
	if _, err := store.UpdateCollection(ctx, owner, collection.ID, collection.Kind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatalf("update %s user data fixture shares: %v", collection.Kind, err)
	}
}

func collectionUserDataAssert(t *testing.T, ctx context.Context, store *Store, subject Subject, itemID string, favorite, played bool, unplayed int) {
	t.Helper()
	want := UserData{ItemID: itemID, IsFavorite: favorite, Played: played, UnplayedItemCount: userDataCount(unplayed)}
	data, err := store.GetUserDataFor(ctx, subject, itemID)
	if err != nil {
		t.Fatalf("read collection user data: %v", err)
	}
	userDataAssertValue(t, data, want)
	item, err := store.GetItemFor(ctx, subject, itemID)
	if err != nil || item.UserData == nil {
		t.Fatalf("collection item lost user data: %+v, %v", item, err)
	}
	userDataAssertValue(t, *item.UserData, want)
	batch, err := store.GetUserDataBatchFor(ctx, subject, []string{itemID, itemID, "missing-collection-state"})
	if err != nil || len(batch) != 1 {
		t.Fatalf("collection user data batch = %+v, %v", batch, err)
	}
	userDataAssertValue(t, batch[itemID], want)
	page, err := store.QueryItems(ctx, Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID,
		Recursive: true, Ids: []string{itemID}, Limit: 100})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].UserData == nil {
		t.Fatalf("collection query lost user data: %+v, %v", page, err)
	}
	userDataAssertValue(t, *page.Items[0].UserData, want)
}

func TestCollectionUserDataDefaultsAndFavoritesRemainPersonal(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-state-reader", false, true, nil)
	owner, reader := Subject{UserID: ownerID}, Subject{UserID: "collection-state-reader"}
	var collections []CollectionInfo
	for _, kind := range []string{PlaylistKind, BoxSetKind} {
		collection := collectionUserDataCreate(t, ctx, store, owner, kind,
			CollectionInput{Name: "Personal " + kind, ItemIDs: []string{"collection-track-a", "collection-track-c"}})
		collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
		collections = append(collections, collection)
		collectionUserDataAssert(t, ctx, store, owner, collection.ID, false, false, 2)
		collectionUserDataAssert(t, ctx, store, reader, collection.ID, false, false, 2)
		empty := collectionUserDataCreate(t, ctx, store, owner, kind, CollectionInput{Name: "Empty " + kind})
		collectionUserDataAssert(t, ctx, store, owner, empty.ID, false, false, 0)
	}
	var persisted int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data`).Scan(&persisted); err != nil || persisted != 0 {
		t.Fatalf("default collection projections persisted state: rows=%d, %v", persisted, err)
	}
	for _, collection := range collections {
		data, err := store.SetFavoriteFor(ctx, reader, collection.ID, true)
		if err != nil {
			t.Fatalf("favorite a read-only shared collection: %v", err)
		}
		userDataAssertValue(t, data, UserData{ItemID: collection.ID, IsFavorite: true, UnplayedItemCount: userDataCount(2)})
		collectionUserDataAssert(t, ctx, store, reader, collection.ID, true, false, 2)
		collectionUserDataAssert(t, ctx, store, owner, collection.ID, false, false, 2)
	}
	favorite := true
	page, err := store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true,
		IncludeItemTypes: []string{PlaylistKind, BoxSetKind}, IsFavorite: &favorite, Limit: 100})
	if err != nil || page.TotalRecordCount != 2 || len(page.Items) != 2 {
		t.Fatalf("personal collection favorites query = %+v, %v", page, err)
	}
	media, err := store.GetUserDataBatchFor(ctx, reader, []string{"collection-track-a", "collection-track-b", "collection-track-c"})
	if err != nil || len(media) != 3 {
		t.Fatalf("read members after collection favorite: %+v, %v", media, err)
	}
	for id, data := range media {
		userDataAssertValue(t, data, UserData{ItemID: id})
	}
	for _, id := range []string{"collection-track-a", "collection-track-c"} {
		if _, err := store.SetPlayedFor(ctx, reader, id, true, nil); err != nil {
			t.Fatalf("complete collection member: %v", err)
		}
	}
	played := true
	page, err = store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true,
		IncludeItemTypes: []string{PlaylistKind, BoxSetKind}, IsPlayed: &played, Limit: 100})
	if err != nil || page.TotalRecordCount != 2 || len(page.Items) != 2 {
		t.Fatalf("derived played collection query = %+v, %v", page, err)
	}
	for _, collection := range collections {
		collectionUserDataAssert(t, ctx, store, reader, collection.ID, true, true, 0)
		collectionUserDataAssert(t, ctx, store, owner, collection.ID, false, false, 2)
	}
}

func TestCollectionUserDataUsesDistinctVisibleMembershipAndExactEntryIDs(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-count-reader", false, false, []string{"collection-source-a"})
	owner, reader := Subject{UserID: ownerID}, Subject{UserID: "collection-count-reader"}
	if _, err := pool.Exec(ctx, `SELECT setval(pg_get_serial_sequence('media_collection_entries','id'),9007199254740992,true)`); err != nil {
		t.Fatalf("advance collection identity beyond JavaScript safe integers: %v", err)
	}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind,
		CollectionInput{Name: "Repeated tracks", ItemIDs: []string{"collection-album", "collection-track-a", "collection-track-c"}})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Overlapping roots", ItemIDs: []string{"collection-album", "collection-track-a"}})
	child := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Overlapping child", ParentID: box.ID, ItemIDs: []string{"collection-track-b", "collection-track-c"}})
	for _, collection := range []CollectionInfo{playlist, box, child} {
		collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
	}
	entries, err := store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(entries.Items) != 4 || entries.TotalRecordCount != 4 {
		t.Fatalf("expanded duplicate playlist entries = %+v, %v", entries, err)
	}
	seenEntries := make(map[string]bool)
	for index, entry := range entries.Items {
		id, err := strconv.ParseInt(entry.PlaylistItemID, 10, 64)
		if err != nil || id != 9007199254740993+int64(index) || seenEntries[entry.PlaylistItemID] {
			t.Fatalf("playlist entry identity lost integer precision or uniqueness: %q, %v", entry.PlaylistItemID, err)
		}
		seenEntries[entry.PlaylistItemID] = true
	}
	if entries.Items[0].ID != "collection-track-a" || entries.Items[2].ID != "collection-track-a" {
		t.Fatalf("playlist duplicate fixture order = %+v", entries.Items)
	}
	for _, collection := range []CollectionInfo{playlist, box} {
		collectionUserDataAssert(t, ctx, store, owner, collection.ID, false, false, 3)
		collectionUserDataAssert(t, ctx, store, reader, collection.ID, false, false, 2)
	}
	if err := store.RemoveCollectionItems(ctx, owner, playlist.ID, PlaylistKind, []string{entries.Items[0].PlaylistItemID}); err != nil {
		t.Fatalf("remove one exact large duplicate entry ID: %v", err)
	}
	remaining, err := store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 100)
	if err != nil || len(remaining.Items) != 3 || remaining.Items[1].PlaylistItemID != entries.Items[2].PlaylistItemID {
		t.Fatalf("large entry removal changed the surviving duplicate identity: %+v, %v", remaining, err)
	}
	collectionUserDataAssert(t, ctx, store, owner, playlist.ID, false, false, 3)
	for _, subject := range []Subject{owner, reader} {
		if _, err := store.SetPlayedFor(ctx, subject, "collection-track-a", true, nil); err != nil {
			t.Fatalf("mark repeated media identity played: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
		VALUES('collection-track-d','collection-source-a','collection-album','Track D','d','Audio')`); err != nil {
		t.Fatalf("add a new physical album descendant: %v", err)
	}
	collectionUserDataAssert(t, ctx, store, owner, box.ID, false, false, 3)
	collectionUserDataAssert(t, ctx, store, reader, box.ID, false, false, 2)
	collectionUserDataAssert(t, ctx, store, owner, playlist.ID, false, false, 2)
	collectionUserDataAssert(t, ctx, store, reader, playlist.ID, false, false, 1)
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id='collection-track-b'`); err != nil {
		t.Fatalf("remove a referenced media item: %v", err)
	}
	collectionUserDataAssert(t, ctx, store, owner, box.ID, false, false, 2)
	collectionUserDataAssert(t, ctx, store, reader, box.ID, false, false, 1)
	collectionUserDataAssert(t, ctx, store, owner, playlist.ID, false, false, 1)
	collectionUserDataAssert(t, ctx, store, reader, playlist.ID, false, true, 0)
}

func TestBoxSetPlayedChangesVisibleMembersForReadOnlySharers(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	for _, statement := range []string{
		`INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES ('collection-series','collection-source-a','Series','series','Series',true)`,
		`INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES ('collection-season','collection-source-a','collection-series','Season','season','Season',true)`,
		`INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES ('collection-episode-a','collection-source-a','collection-season','Episode A','episode a','Episode'),('collection-episode-b','collection-source-a','collection-season','Episode B','episode b','Episode'),('collection-unrelated','collection-source-a',NULL,'Unrelated','unrelated','Audio')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("insert recursive collection media: %v", err)
		}
	}
	libraryIntegrationUser(t, ctx, pool, "collection-played-reader", false, false, []string{"collection-source-a"})
	owner, reader := Subject{UserID: ownerID}, Subject{UserID: "collection-played-reader"}
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Visible recursive members", ItemIDs: []string{"collection-album", "collection-series", "collection-track-a"}})
	child := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Cross-library child", ParentID: box.ID, ItemIDs: []string{"collection-track-b", "collection-track-c"}})
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind,
		CollectionInput{Name: "No bulk playlist state", ItemIDs: []string{"collection-track-a", "collection-track-b"}})
	for _, collection := range []CollectionInfo{box, child, playlist} {
		collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
	}
	if _, err := store.SetFavoriteFor(ctx, reader, box.ID, true); err != nil {
		t.Fatalf("favorite the shared box set: %v", err)
	}
	previousDate := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	userDataSeed(t, ctx, pool, reader.UserID, UserData{ItemID: "collection-track-a", IsFavorite: true,
		PlaybackPositionTicks: 12345, PlayCount: 7, LastPlayedDate: &previousDate})
	datePlayed := time.Date(2025, 4, 5, 6, 7, 8, 0, time.FixedZone("fixture", 2*60*60))
	data, err := store.SetPlayedFor(ctx, reader, box.ID, true, &datePlayed)
	if err != nil {
		t.Fatalf("read-only sharer marks their own box set played: %v", err)
	}
	userDataAssertValue(t, data, UserData{ItemID: box.ID, IsFavorite: true, Played: true, UnplayedItemCount: userDataCount(0)})
	visibleLeaves := []string{"collection-track-a", "collection-track-b", "collection-episode-a", "collection-episode-b"}
	for _, id := range visibleLeaves {
		data, err := store.GetUserDataFor(ctx, reader, id)
		if err != nil {
			t.Fatalf("read visible bulk-played member %s: %v", id, err)
		}
		want := UserData{ItemID: id, Played: true, PlayCount: 1, LastPlayedDate: &datePlayed}
		if id == "collection-track-a" {
			want.IsFavorite, want.PlayCount = true, 7
		}
		userDataAssertValue(t, data, want)
	}
	for _, id := range []string{"collection-album", "collection-series", "collection-season", child.ID} {
		data, err := store.GetUserDataFor(ctx, reader, id)
		if err != nil || !data.Played || data.UnplayedItemCount == nil || *data.UnplayedItemCount != 0 {
			t.Fatalf("recursive member summary %s = %+v, %v", id, data, err)
		}
	}
	var foreignRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id<>$1
		OR item_id IN ('collection-track-c','collection-unrelated')`, reader.UserID).Scan(&foreignRows); err != nil || foreignRows != 0 {
		t.Fatalf("bulk collection state escaped member visibility or user ownership: rows=%d, %v", foreignRows, err)
	}
	collectionUserDataAssert(t, ctx, store, owner, box.ID, false, false, 5)
	before, err := store.GetUserDataBatchFor(ctx, reader, append(append([]string{}, visibleLeaves...), playlist.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, played := range []bool{true, false} {
		if _, err := store.SetPlayedFor(ctx, reader, playlist.ID, played, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("playlist bulk played=%v = %v; want ErrInvalidInput", played, err)
		}
	}
	after, err := store.GetUserDataBatchFor(ctx, reader, append(append([]string{}, visibleLeaves...), playlist.ID))
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected playlist bulk write changed user data: before=%+v after=%+v, %v", before, after, err)
	}
	data, err = store.SetPlayedFor(ctx, reader, box.ID, false, nil)
	if err != nil {
		t.Fatalf("clear the shared box set's played state: %v", err)
	}
	userDataAssertValue(t, data, UserData{ItemID: box.ID, IsFavorite: true, UnplayedItemCount: userDataCount(4)})
	for _, id := range visibleLeaves {
		data, err := store.GetUserDataFor(ctx, reader, id)
		if err != nil {
			t.Fatal(err)
		}
		userDataAssertValue(t, data, UserData{ItemID: id, IsFavorite: id == "collection-track-a"})
	}
}

func TestCollectionUserDataRechecksSharingAndRestrictedFeatures(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-revoked-reader", false, false, []string{"collection-source-a"})
	owner, reader := Subject{UserID: ownerID}, Subject{UserID: "collection-revoked-reader"}
	for _, test := range []struct{ kind, feature string }{
		{PlaylistKind, identity.FeaturePlaylists}, {BoxSetKind, identity.FeatureCollections},
	} {
		t.Run(test.kind, func(t *testing.T) {
			collection := collectionUserDataCreate(t, ctx, store, owner, test.kind,
				CollectionInput{Name: "Revocable " + test.kind, ItemIDs: []string{"collection-track-a", "collection-track-c"}})
			collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
			if _, err := store.SetFavoriteFor(ctx, reader, collection.ID, true); err != nil {
				t.Fatalf("seed collection state before revocation: %v", err)
			}
			for _, revoke := range []string{"feature", "share"} {
				t.Run(revoke, func(t *testing.T) {
					if revoke == "feature" {
						if _, err := pool.Exec(ctx, `UPDATE users SET policy=jsonb_set(policy,'{RestrictedFeatures}',to_jsonb($2::text[])) WHERE id=$1`, reader.UserID, []string{test.feature}); err != nil {
							t.Fatal(err)
						}
					} else {
						collectionUserDataShare(t, ctx, store, owner, collection)
					}
					if _, err := store.GetItemFor(ctx, reader, collection.ID); !errors.Is(err, ErrNotFound) {
						t.Fatalf("revoked collection item = %v", err)
					}
					if _, err := store.GetUserDataFor(ctx, reader, collection.ID); !errors.Is(err, ErrNotFound) {
						t.Fatalf("revoked collection user data = %v", err)
					}
					batch, err := store.GetUserDataBatchFor(ctx, reader, []string{collection.ID, "collection-track-c"})
					if err != nil || len(batch) != 0 {
						t.Fatalf("revoked collection batch leaked state: %+v, %v", batch, err)
					}
					page, err := store.QueryItems(ctx, Query{UserID: reader.UserID, Recursive: true, Ids: []string{collection.ID}, Limit: 100})
					if err != nil || page.TotalRecordCount != 0 || len(page.Items) != 0 {
						t.Fatalf("revoked collection leaked query count or state: %+v, %v", page, err)
					}
					if _, err := store.SetFavoriteFor(ctx, reader, collection.ID, false); !errors.Is(err, ErrNotFound) {
						t.Fatalf("revoked collection favorite = %v", err)
					}
					if _, err := store.SetPlayedFor(ctx, reader, collection.ID, true, nil); !errors.Is(err, ErrNotFound) {
						t.Fatalf("revoked collection played = %v", err)
					}
					if _, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{UserID: reader.UserID, ItemID: collection.ID}); !errors.Is(err, ErrNotFound) {
						t.Fatalf("revoked collection notification target = %v", err)
					}
					pageData := notificationPage(t, ctx, store, UserDataNotificationQuery{UserID: reader.UserID, ItemID: "collection-track-a", Limit: 100})
					for _, data := range pageData.Items {
						if data.ItemID == collection.ID || data.ItemID == "collection-track-c" {
							t.Fatalf("media notification leaked revoked container or hidden member: %+v", data)
						}
					}
					if revoke == "feature" {
						if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy-'RestrictedFeatures' WHERE id=$1`, reader.UserID); err != nil {
							t.Fatal(err)
						}
						collectionUserDataAssert(t, ctx, store, reader, collection.ID, true, false, 1)
					}
				})
			}
		})
	}
}

func TestCollectionApplicationStateWritesKeepIndependentAuthorityAndScopedReads(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-key-target", false, false, []string{"collection-source-a"})
	owner := Subject{UserID: ownerID}
	key := seedCatalogApplicationKey(t, ctx, pool, "collection-userdata-key", true)
	key.UserID = "collection-key-target"
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Independent server state", ItemIDs: []string{"collection-album", "collection-track-c"}})
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind,
		CollectionInput{Name: "Independent server favorite", ItemIDs: []string{"collection-track-a", "collection-track-c"}})
	for _, collection := range []CollectionInfo{box, playlist} {
		collectionUserDataShare(t, ctx, store, owner, collection, key.UserID)
		favorite, err := store.SetFavoriteFor(ctx, key, collection.ID, true)
		if err != nil || !favorite.IsFavorite || favorite.UnplayedItemCount == nil {
			t.Fatalf("application collection favorite = %+v, %v", favorite, err)
		}
		wantCount := 2
		if collection.Kind == BoxSetKind {
			wantCount = 3
		}
		if *favorite.UnplayedItemCount != wantCount {
			t.Fatalf("application mutation response inherited target catalog scope: %+v", favorite)
		}
	}
	collectionUserDataAssert(t, ctx, store, key, box.ID, true, false, 2)
	collectionUserDataAssert(t, ctx, store, key, playlist.ID, true, false, 1)
	played, err := store.SetPlayedFor(ctx, key, box.ID, true, nil)
	if err != nil {
		t.Fatalf("application bulk state ignored independent authority: %v", err)
	}
	userDataAssertValue(t, played, UserData{ItemID: box.ID, IsFavorite: true, Played: true, UnplayedItemCount: userDataCount(0)})
	var memberRows, wrongOwners int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id=$1 AND played
		AND item_id IN ('collection-track-a','collection-track-b','collection-track-c')),
		count(*) FILTER (WHERE user_id<>$1) FROM user_item_data`, key.UserID).Scan(&memberRows, &wrongOwners); err != nil || memberRows != 3 || wrongOwners != 0 {
		t.Fatalf("application box set write lost hidden members or target ownership: members=%d other=%d, %v", memberRows, wrongOwners, err)
	}
	if _, err := store.GetUserDataFor(ctx, key, "collection-track-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("independent write widened later target-scoped media reads: %v", err)
	}
	collectionUserDataAssert(t, ctx, store, key, box.ID, true, true, 0)
	collectionUserDataAssert(t, ctx, store, key, playlist.ID, true, true, 0)
	if _, err := store.SetPlayedFor(ctx, key, playlist.ID, false, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("application credential bypassed the playlist bulk-state contract: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, key.UserID); err != nil {
		t.Fatal(err)
	}
	collectionUserDataAssert(t, ctx, store, key, box.ID, true, false, 0)
	played, err = store.SetPlayedFor(ctx, key, box.ID, false, nil)
	if err != nil {
		t.Fatalf("application mutation after target folder revocation: %v", err)
	}
	userDataAssertValue(t, played, UserData{ItemID: box.ID, IsFavorite: true, UnplayedItemCount: userDataCount(3)})
	collectionUserDataAssert(t, ctx, store, key, box.ID, true, false, 0)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetFavoriteFor(ctx, key, box.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application credential retained collection favorite authority: %v", err)
	}
	if _, err := store.SetPlayedFor(ctx, key, box.ID, true, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application credential retained collection played authority: %v", err)
	}
}

func TestCollectionUserDataNotificationsFollowVisibleMembershipAndRecheckPages(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES
		('collection-child-only','collection-source-a',NULL,'Child only','child only','Audio'),
		('collection-cross-parent','collection-source-b','collection-album','Foreign physical child','foreign child','Audio')`); err != nil {
		t.Fatalf("insert notification membership boundary fixtures: %v", err)
	}
	libraryIntegrationUser(t, ctx, pool, "collection-notification-reader", false, false, []string{"collection-source-a"})
	owner, reader := Subject{UserID: ownerID}, Subject{UserID: "collection-notification-reader"}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind,
		CollectionInput{Name: "Notification duplicate playlist", ItemIDs: []string{"collection-track-a", "collection-track-a", "collection-track-c"}})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Notification root", ItemIDs: []string{"collection-album", "collection-track-c"}})
	child := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Notification nested box", ParentID: box.ID, ItemIDs: []string{"collection-track-a", "collection-child-only"}})
	private := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Unshared notification reference", ItemIDs: []string{"collection-track-a"}})
	for _, collection := range []CollectionInfo{playlist, box, child} {
		collectionUserDataShare(t, ctx, store, owner, collection, reader.UserID)
	}
	if _, err := store.SetPlayedFor(ctx, reader, "collection-track-a", true, nil); err != nil {
		t.Fatalf("commit a shared collection member state: %v", err)
	}
	query := UserDataNotificationQuery{UserID: reader.UserID, ItemID: "collection-track-a", Limit: 100}
	values := notificationAssertIDs(t, notificationPage(t, ctx, store, query),
		"collection-track-a", "collection-album", playlist.ID, box.ID, child.ID)
	userDataAssertValue(t, values["collection-album"], UserData{ItemID: "collection-album", UnplayedItemCount: userDataCount(1)})
	userDataAssertValue(t, values[playlist.ID], UserData{ItemID: playlist.ID, Played: true, UnplayedItemCount: userDataCount(0)})
	userDataAssertValue(t, values[box.ID], UserData{ItemID: box.ID, UnplayedItemCount: userDataCount(2)})
	userDataAssertValue(t, values[child.ID], UserData{ItemID: child.ID, UnplayedItemCount: userDataCount(1)})
	if _, err := store.SetPlayedFor(ctx, reader, box.ID, true, nil); err != nil {
		t.Fatalf("commit visible recursive collection state: %v", err)
	}
	query.ItemID, query.Recursive, query.Limit = box.ID, true, 2
	var pages UserDataNotificationResult
	for number := 0; ; number++ {
		if number > 8 {
			t.Fatal("collection notification cursor did not terminate")
		}
		page := notificationPage(t, ctx, store, query)
		pages.Items = append(pages.Items, page.Items...)
		if page.NextAfterID == "" {
			break
		}
		if len(page.Items) != 2 || page.NextAfterID <= query.AfterID || page.NextAfterID != page.Items[len(page.Items)-1].ItemID {
			t.Fatalf("collection notification cursor did not advance exclusively: %+v", page)
		}
		query.AfterID = page.NextAfterID
	}
	values = notificationAssertIDs(t, pages, "collection-track-a", "collection-track-b", "collection-child-only",
		"collection-album", playlist.ID, box.ID, child.ID)
	for _, data := range values {
		if !data.Played {
			t.Fatalf("recursive notification omitted a committed visible member or summary: %+v", data)
		}
	}
	var hiddenWrites int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id=$1
		AND item_id IN ('collection-track-c','collection-cross-parent')`, reader.UserID).Scan(&hiddenWrites); err != nil || hiddenWrites != 0 {
		t.Fatalf("recursive played write crossed hidden membership or a foreign physical edge: rows=%d, %v", hiddenWrites, err)
	}
	collectionUserDataShare(t, ctx, store, owner, child)
	collectionUserDataShare(t, ctx, store, owner, playlist)
	query.AfterID, query.Limit = "", 100
	values = notificationAssertIDs(t, notificationPage(t, ctx, store, query),
		"collection-track-a", "collection-track-b", "collection-album", box.ID)
	userDataAssertValue(t, values[box.ID], UserData{ItemID: box.ID, Played: true, UnplayedItemCount: userDataCount(0)})
	query.ItemID, query.Recursive = "collection-track-a", false
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), "collection-track-a", "collection-album", box.ID)
	query.ItemID, query.Recursive, query.Limit = box.ID, true, 1
	first := notificationPage(t, ctx, store, query)
	if len(first.Items) != 1 || first.NextAfterID == "" {
		t.Fatalf("revocation fixture did not produce a continuation cursor: %+v", first)
	}
	collectionUserDataShare(t, ctx, store, owner, box)
	query.AfterID = first.NextAfterID
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("collection notification continuation retained revoked sharing: %v", err)
	}
	query.ItemID, query.Recursive, query.AfterID, query.Limit = "collection-track-a", false, "", 100
	notificationAssertIDs(t, notificationPage(t, ctx, store, query), "collection-track-a", "collection-album")
	ownerPage := notificationPage(t, ctx, store, UserDataNotificationQuery{UserID: ownerID, ItemID: box.ID, Recursive: true, Limit: 100})
	notificationAssertIDs(t, ownerPage, box.ID, child.ID, playlist.ID, private.ID,
		"collection-album", "collection-track-a", "collection-track-b", "collection-track-c", "collection-child-only")
}

func TestBoxSetPlayedSerializesMembershipEditsThroughCatalogOwnership(t *testing.T) {
	for _, mutation := range []string{"remove", "add"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
			collectionTestMedia(t, ctx, pool)
			owner := Subject{UserID: ownerID}
			box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
				CollectionInput{Name: "Serialized membership " + mutation, ItemIDs: []string{"collection-track-a"}})
			gate, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin source item gate: %v", err)
			}
			defer rollback(gate)
			if _, err := gate.Exec(ctx, `SELECT id FROM items WHERE id='collection-track-a' FOR UPDATE`); err != nil {
				t.Fatalf("hold source item before the collection batch: %v", err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			datePlayed := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
			type playedResult struct {
				data UserData
				err  error
			}
			playedDone := make(chan playedResult, 1)
			go func() {
				data, err := store.SetPlayedFor(operationCtx, owner, box.ID, true, &datePlayed)
				playedDone <- playedResult{data: data, err: err}
			}()
			// The exact owner session must reach the source row lock. A pool-only
			// state transaction cannot satisfy this barrier.
			taskScanWaitOwnerBlocked(t, operationCtx, pool, store, gate.Conn().PgConn().PID())
			var batchQuery bool
			if err := pool.QueryRow(operationCtx, `SELECT query LIKE 'WITH RECURSIVE roots AS (%'
				FROM pg_stat_activity WHERE pid=$1`,
				store.ownership.conn.Conn().PgConn().PID()).Scan(&batchQuery); err != nil || !batchQuery {
				t.Fatalf("owner did not block in the collection member locking query: matched=%t, %v", batchQuery, err)
			}
			membershipDone := make(chan error, 1)
			go func() {
				if mutation == "remove" {
					membershipDone <- store.RemoveCollectionItems(operationCtx, owner, box.ID, BoxSetKind, []string{"collection-track-a"})
					return
				}
				_, err := store.AddCollectionItems(operationCtx, owner, box.ID, BoxSetKind, []string{"collection-track-c"})
				membershipDone <- err
			}()
			// The batch already released Store.mu and the scan workers are idle.
			// Only the membership API can hold it while awaiting catalog ownership.
			admissionCtx, stopAdmission := context.WithTimeout(operationCtx, 5*time.Second)
			ticker := time.NewTicker(5 * time.Millisecond)
			admitted := false
			for !admitted {
				if !store.mu.TryLock() {
					admitted = true
					break
				}
				store.mu.Unlock()
				select {
				case err := <-membershipDone:
					ticker.Stop()
					stopAdmission()
					t.Fatalf("membership edit completed before the batch released ownership: %v", err)
				case <-ticker.C:
				case <-admissionCtx.Done():
					ticker.Stop()
					stopAdmission()
					t.Fatal("membership edit did not reach catalog admission behind the blocked batch")
				}
			}
			ticker.Stop()
			stopAdmission()
			select {
			case err := <-membershipDone:
				t.Fatalf("membership edit escaped catalog ownership while the source was locked: %v", err)
			default:
			}
			var memberIDs []string
			if err := pool.QueryRow(operationCtx, `SELECT array_agg(item_id ORDER BY position)
				FROM media_collection_entries WHERE collection_id=$1`, box.ID).Scan(&memberIDs); err != nil || !reflect.DeepEqual(memberIDs, []string{"collection-track-a"}) {
				t.Fatalf("pending membership edit changed the accepted batch population: %v, %v", memberIDs, err)
			}
			var stateRows int
			if err := pool.QueryRow(operationCtx, `SELECT count(*) FROM user_item_data WHERE user_id=$1`, ownerID).Scan(&stateRows); err != nil || stateRows != 0 {
				t.Fatalf("blocked batch published partial user state: rows=%d, %v", stateRows, err)
			}
			if err := gate.Commit(operationCtx); err != nil {
				t.Fatalf("release the collection member locking query: %v", err)
			}
			select {
			case result := <-playedDone:
				if result.err != nil {
					t.Fatalf("complete the accepted box set batch: %v", result.err)
				}
				userDataAssertValue(t, result.data, UserData{ItemID: box.ID, Played: true, UnplayedItemCount: userDataCount(0)})
			case <-operationCtx.Done():
				t.Fatal("box set batch did not finish after releasing its source gate")
			}
			select {
			case err := <-membershipDone:
				if err != nil {
					t.Fatalf("complete membership edit after the accepted batch: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("membership edit did not finish after the box set batch")
			}
			data, err := store.GetUserDataFor(ctx, owner, "collection-track-a")
			if err != nil {
				t.Fatalf("read the originally accepted source state: %v", err)
			}
			userDataAssertValue(t, data, UserData{ItemID: "collection-track-a", Played: true, PlayCount: 1, LastPlayedDate: &datePlayed})
			info, err := store.GetCollection(ctx, owner, box.ID, BoxSetKind)
			if err != nil {
				t.Fatalf("read the subsequently edited collection: %v", err)
			}
			if mutation == "remove" {
				if info.ItemCount != 0 {
					t.Fatalf("serialized removal left %d members", info.ItemCount)
				}
				collectionUserDataAssert(t, ctx, store, owner, box.ID, false, false, 0)
				return
			}
			if info.ItemCount != 2 {
				t.Fatalf("serialized addition left %d members", info.ItemCount)
			}
			collectionUserDataAssert(t, ctx, store, owner, box.ID, false, false, 1)
			data, err = store.GetUserDataFor(ctx, owner, "collection-track-c")
			if err != nil {
				t.Fatalf("read the member added after the accepted batch: %v", err)
			}
			userDataAssertValue(t, data, UserData{ItemID: "collection-track-c"})
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id=$1 AND item_id='collection-track-c'`, ownerID).Scan(&stateRows); err != nil || stateRows != 0 {
				t.Fatalf("earlier batch persisted state for a later member: rows=%d, %v", stateRows, err)
			}
		})
	}
}
