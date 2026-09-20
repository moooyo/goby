package library

import (
	"github.com/moooyo/goby/internal/notificationjournal"
	"strconv"
	"testing"
)

func TestNotificationProjectionRequiresVisibleSourceBeforeParentInvalidation(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "notification-hidden-source", "Movie", "library-b", "library-b", map[string]any{"Tags": []string{"Blocked"}})
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"BlockedTags":["Blocked"]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	refs := []notificationjournal.Reference{{Kind: "Item", ID: "notification-hidden-source", LibraryID: "library-b", SourceID: "notification-hidden-source"}, {Kind: "Item", ID: "library-b", LibraryID: "library-b", SourceID: "notification-hidden-source"}}
	actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 0 {
		t.Fatalf("a visible parent leaked a hidden-only source change: %+v %v", actual, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy-'BlockedTags' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	actual, err = store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 2 {
		t.Fatalf("current source permission was not observed: %+v %v", actual, err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM items WHERE id='notification-hidden-source'`); err != nil {
		t.Fatal(err)
	}
	actual, err = store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 0 {
		t.Fatal("deleted source authority was inferred from historical parent visibility")
	}
}
func TestNotificationProjectionNeverConfusesNumericItemAndEntityIDs(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "notification-visible-audio", "Audio", "library-b", "library-b", map[string]any{"Artists": []string{"Notification artist"}})
	var id int64
	if store.pool.QueryRow(ctx, `SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Notification artist'`).Scan(&id) != nil {
		t.Fatal("read entity")
	}
	text := strconv.FormatInt(id, 10)
	similarInsert(t, ctx, store, text, "Audio", "library-a", "library-a", nil)
	actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, []notificationjournal.Reference{{Kind: "Item", ID: text}, {Kind: "Entity", ID: text}})
	if err != nil || len(actual) != 1 || actual[0].Kind != "Entity" {
		t.Fatalf("typed reference changed its authority namespace: %+v %v", actual, err)
	}
}
