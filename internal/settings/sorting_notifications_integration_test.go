package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
)

func TestSortingPolicyNotificationScopesAreIndependentOfChangedKeysAndCapacityIsAtomic(t *testing.T) {
	ctx, pool, owner, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	// Only the SQL source journal is exercised. No transport worker or secret
	// decryptor is started; bounded ciphertext placeholders never leave this DB.
	if _, err := pool.Exec(ctx, `UPDATE notification_transport SET enabled=true,endpoint='https://receiver.invalid/events',credential_ciphertext=decode(repeat('00',48),'hex');
		INSERT INTO users(id,name,normalized_name,password_hash,policy) VALUES('sorting-reader','Sorting reader','sorting reader','','{"EnableAllFolders":true,"ExcludedSubFolders":["sorting-hidden-title"]}'),('sorting-other','Sorting other','sorting other','','{"EnableAllFolders":true}');
		INSERT INTO libraries(id,name,collection_type) VALUES('sorting-visible','Visible','movies'),('sorting-private','Collections','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('sorting-visible','sorting-visible','Visible','visible','CollectionFolder',true),
		('sorting-hidden-title','sorting-visible','The Match','the match','Movie',false),
		('sorting-own-list','sorting-private','Own List','own list','Playlist',true),('sorting-other-list','sorting-private','Other List','other list','Playlist',true);
		INSERT INTO media_collections(item_id,owner_id,kind,media_type,is_public,is_locked) VALUES('sorting-own-list','sorting-reader','Playlist','Audio',false,false),('sorting-other-list','sorting-other','Playlist','Audio',false,false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext) VALUES($1,$2,$3,'','',ARRAY['CatalogInvalidated'],decode(repeat('00',48),'hex'))`, settingsTestID(t), actor.Principal.SessionID, actor.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	var firstRefs string
	for _, word := range []string{"The", "Unmatched"} {
		before := store.Snapshot()
		sorting := Sorting{SortRemoveWords: []string{word}}
		if _, err := store.Update(ctx, native, UpdateRequest{Revision: before.Revision, Overrides: before.Overrides, Sorting: &sorting}); err != nil {
			t.Fatal(err)
		}
		var raw string
		var resync bool
		if err := pool.QueryRow(ctx, `SELECT refs::text,resync FROM notification_source_events ORDER BY sequence DESC LIMIT 1`).Scan(&raw, &resync); err != nil || !resync {
			t.Fatalf("missing single policy invalidation: %v", err)
		}
		if firstRefs != "" && raw != firstRefs {
			t.Fatal("notification scope disclosed which private/hidden keys changed")
		}
		firstRefs = raw
	}
	var refs []notificationjournal.Reference
	if err := json.Unmarshal([]byte(firstRefs), &refs); err != nil {
		t.Fatal(err)
	}
	visible, err := owner.FilterNotificationReferences(ctx, library.Subject{UserID: "sorting-reader"}, refs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.GetItem(ctx, "sorting-reader", "sorting-hidden-title"); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("hidden source fixture was visible: %v", err)
	}
	ids := map[string]bool{}
	for _, ref := range visible {
		ids[ref.ID] = true
	}
	if len(visible) != 2 || !ids["sorting-visible"] || !ids["sorting-own-list"] || ids["sorting-other-list"] || ids["sorting-hidden-title"] {
		t.Fatalf("policy invalidation lost current collection authority: %+v", visible)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM notification_source_events;
		UPDATE notification_journal_state SET sequence=512;
		INSERT INTO notification_source_events(id,sequence,kind,refs,resync) SELECT md5('sorting-capacity-'||n::text),n,'CatalogInvalidated','[{"Kind":"Library","Id":"sorting-visible","LibraryId":"sorting-visible"}]'::jsonb,true FROM generate_series(1,512)n`); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	sorting := Sorting{SortRemoveWords: []string{"The"}}
	request := UpdateRequest{Revision: before.Revision, Overrides: before.Overrides, Sorting: &sorting}
	if _, err := store.Update(ctx, native, request); !errors.Is(err, notificationjournal.ErrCapacity) {
		t.Fatalf("full durable source journal did not reject atomically: %v", err)
	}
	var key string
	var events int
	if err := pool.QueryRow(ctx, `SELECT sort_name,(SELECT count(*) FROM notification_source_events) FROM items WHERE id='sorting-hidden-title'`).Scan(&key, &events); err != nil || key != "the match" || events != 512 || !reflect.DeepEqual(store.Snapshot(), before) {
		t.Fatalf("capacity failure partially committed settings, key or journal: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_registrations SET source_cursor=512`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, native, request); err != nil {
		t.Fatalf("same request/revision could not retry after queue recovery: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT sort_name,(SELECT count(*) FROM notification_source_events) FROM items WHERE id='sorting-hidden-title'`).Scan(&key, &events); err != nil || key != "match" || events != 1 {
		t.Fatalf("retry did not commit one invalidation and its key: %v", err)
	}
}
