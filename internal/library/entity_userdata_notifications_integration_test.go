package library

import (
	"errors"
	"strconv"
	"testing"
)

func TestEntityUserDataNotificationReadsCommittedIndependentStateAndCurrentAccess(t *testing.T) {
	ctx, pool, store, allowedRoot, firstUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection, items := notificationCatalogFixture(t, ctx, pool, store, allowedRoot, "entity-notification")
	secondUser := "entity-notification-peer"
	libraryIntegrationUser(t, ctx, pool, secondUser, false, false, []string{collection.ID})
	var entityID int64
	if err := pool.QueryRow(ctx, `INSERT INTO catalog_entities(kind,name)
		VALUES('Genre','Notification Genre') RETURNING id`).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name)
		VALUES($1,$2,1,'Notification Genre')`, items["UnrelatedMovie"], entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetFavorite(ctx, firstUser, items["UnrelatedMovie"], true); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(entityID, 10)
	query := UserDataNotificationQuery{UserID: firstUser, ItemID: id, Recursive: true, Limit: 1}
	initial := notificationAssertIDs(t, notificationPage(t, ctx, store, query), id)[id]
	if initial.IsFavorite || initial.Played || initial.UnplayedItemCount != nil {
		t.Fatal("an entity inherited associated media state or recursive summaries")
	}
	favorite, rating := true, 8.5
	if _, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: firstUser}, entityID,
		UserDataPatch{IsFavorite: &favorite, Rating: &rating}); err != nil {
		t.Fatal(err)
	}
	updated := notificationAssertIDs(t, notificationPage(t, ctx, store, query), id)[id]
	if !updated.IsFavorite || updated.Rating == nil || *updated.Rating != rating {
		t.Fatal("the entity notification did not read the latest committed state")
	}
	query.UserID = secondUser
	peer := notificationAssertIDs(t, notificationPage(t, ctx, store, query), id)[id]
	if peer.IsFavorite || peer.Rating != nil {
		t.Fatal("entity state crossed the user boundary")
	}
	query.AfterID = id
	page := notificationPage(t, ctx, store, query)
	if len(page.Items) != 0 || page.NextAfterID != "" {
		t.Fatal("the entity notification did not honor its exclusive cursor")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=jsonb_set(policy,'{EnabledFolders}','[]'::jsonb)
		WHERE id=$1`, secondUser); err != nil {
		t.Fatal(err)
	}
	// Even a consumed cursor must not bypass current entity visibility.
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entity notification after library revocation: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM item_entities WHERE entity_id=$1`, entityID); err != nil {
		t.Fatal(err)
	}
	query.UserID, query.AfterID = firstUser, ""
	if _, err := store.UserDataNotificationPage(ctx, query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entity notification disclosed an orphan entity: %v", err)
	}
}
