package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/notificationjournal"
)

func providerCacheBatchFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('provider-cache-library','Provider cache','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES ('provider-cache-root','provider-cache-library','/provider-cache','/provider-cache','');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path)
		VALUES ('provider-cache-parent','provider-cache-library','provider-cache-root','Parent','parent','Folder',true,'Film');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path) VALUES
		('provider-cache-a','provider-cache-library','provider-cache-root','provider-cache-parent','A','a','Movie',false,'Film/A.mp4'),
		('provider-cache-c','provider-cache-library','provider-cache-root','provider-cache-parent','C','c','Folder',true,'Film/C'),
		('provider-cache-fresh','provider-cache-library','provider-cache-root','provider-cache-parent','Fresh','fresh','Movie',false,'Film/Fresh.mp4'),
		('provider-cache-owner','provider-cache-library','provider-cache-root','provider-cache-parent','Owner','owner','Movie',false,'Film/Owner.mp4');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path)
		VALUES ('provider-cache-b','provider-cache-library','provider-cache-root','provider-cache-owner','Auxiliary','auxiliary','Video','Film/featurettes/B.mp4');
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory)
		VALUES ('provider-cache-root','Film/featurettes',true);
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES ('provider-cache-b','provider-cache-owner','clip',true);
		INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash,fetched_at)
		SELECT id,'Primary',0,'tmdb','1','cover',decode('00','hex'),'image/png',1,1,repeat('a',64),clock_timestamp()-interval '10 days'
		FROM items WHERE id IN ('provider-cache-a','provider-cache-b','provider-cache-c');
		INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash,fetched_at)
		VALUES ('provider-cache-a','Backdrop',0,'tmdb','1','backdrop',decode('00','hex'),'image/png',1,1,repeat('a',64),clock_timestamp()-interval '10 days'),
		('provider-cache-fresh','Primary',0,'tmdb','1','fresh',decode('00','hex'),'image/png',1,1,repeat('a',64),clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
}

func providerCacheBatchEnableJournal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()
	owner := playSessionOwnerFixture(t, ctx, pool, userID, "provider-cache-notifications")
	if _, err := pool.Exec(ctx, `UPDATE notification_transport SET enabled=true,endpoint='https://provider-cache.invalid/events',credential_ciphertext=$1 WHERE id=1`, make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext)
		VALUES($1,$2,$3,$4,'',ARRAY['CatalogInvalidated'],$5)`, strings.Repeat("c", 32), owner.SessionID, owner.UserID, owner.DeviceID, make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
}

func providerCacheBatchChanges() []CatalogChange {
	return []CatalogChange{
		{Kind: CatalogUpdated, ItemID: "provider-cache-a", LibraryID: "provider-cache-library", ParentID: "provider-cache-parent"},
		{Kind: CatalogUpdated, ItemID: "provider-cache-b", LibraryID: "provider-cache-library"},
		{Kind: CatalogUpdated, ItemID: "provider-cache-c", LibraryID: "provider-cache-library", ParentID: "provider-cache-parent", IsFolder: true},
	}
}

func providerCacheBatchReferences(changes []CatalogChange) []notificationjournal.Reference {
	var refs []notificationjournal.Reference
	for _, change := range changes {
		refs = append(refs, notificationjournal.Reference{Kind: "Item", ID: change.ItemID, LibraryID: change.LibraryID, SourceID: change.ItemID})
		if change.ParentID != "" {
			refs = append(refs, notificationjournal.Reference{Kind: "Item", ID: change.ParentID, LibraryID: change.LibraryID, SourceID: change.ItemID})
		}
	}
	return refs
}

func TestProviderCacheBatchRecordsOrderedChangesOnceAndRetainsAuxiliaryMerge(t *testing.T) {
	for _, mode := range []string{"disabled transport", "enabled transport", "auxiliary change after deletion"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, store, userID, trace := catalogBatchTestStore(t)
			providerCacheBatchFixture(t, ctx, pool)
			enabled := mode != "disabled transport"
			if enabled {
				providerCacheBatchEnableJournal(t, ctx, pool, userID)
			}
			want := providerCacheBatchChanges()
			wantJournalCalls := 1
			if mode == "auxiliary change after deletion" {
				// A real database side effect changes a related resource between
				// snapshots, proving the later auxiliary merge still participates.
				if _, err := pool.Exec(ctx, `CREATE FUNCTION provider_cache_change_resource() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN UPDATE items SET name='Pruned auxiliary' WHERE id=OLD.item_id AND id='provider-cache-b'; RETURN OLD; END $$;
					CREATE TRIGGER provider_cache_change_resource AFTER DELETE ON item_provider_images
					FOR EACH ROW EXECUTE FUNCTION provider_cache_change_resource()`); err != nil {
					t.Fatal(err)
				}
				want = append(want, CatalogChange{Kind: CatalogUpdated, ItemID: "provider-cache-owner", LibraryID: "provider-cache-library", ParentID: "provider-cache-parent"})
				wantJournalCalls = 2
			}
			notifications := catalogChangesTestListener(t, store)
			trace.reset()
			removed, err := store.pruneProviderCacheBatch(ctx, time.Now().Add(-24*time.Hour), 1000)
			if err != nil || removed != 4 {
				t.Fatalf("cache batch removed %d entries: %v", removed, err)
			}
			assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), want)
			assertNoCatalogTestNotification(t, notifications)
			if count := catalogBatchStatementCount(trace, "SELECT goby_record_notification_source("); count != wantJournalCalls {
				t.Fatalf("cache batch made %d journal calls, want %d", count, wantJournalCalls)
			}
			var retained, events int
			var sequence int64
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM item_provider_images),
				(SELECT count(*) FROM notification_source_events),sequence FROM notification_journal_state WHERE id=1`).Scan(&retained, &events, &sequence); err != nil || retained != 1 {
				t.Fatalf("cache retention changed: retained=%d events=%d sequence=%d error=%v", retained, events, sequence, err)
			}
			if !enabled {
				if events != 0 || sequence != 0 {
					t.Fatalf("disabled transport retained a source event: events=%d sequence=%d", events, sequence)
				}
				return
			}
			if events != 1 {
				t.Fatalf("one cache transaction changed its mutation identity: events=%d", events)
			}
			var raw []byte
			var eventID string
			var eventSequence int64
			var resync bool
			if err := pool.QueryRow(ctx, `SELECT id,sequence,refs,resync FROM notification_source_events`).Scan(&eventID, &eventSequence, &raw, &resync); err != nil {
				t.Fatal(err)
			}
			var refs []notificationjournal.Reference
			if err := json.Unmarshal(raw, &refs); err != nil {
				t.Fatal(err)
			}
			if len(eventID) != 32 || eventSequence != sequence || eventSequence <= 0 || resync || !reflect.DeepEqual(refs, providerCacheBatchReferences(want)) {
				t.Fatalf("committed journal lost its ordered scopes or final cursor coverage: id=%q sequence=%d state=%d resync=%t refs=%+v", eventID, eventSequence, sequence, resync, refs)
			}
			trace.reset()
			if removed, err := store.pruneProviderCacheBatch(ctx, time.Now().Add(time.Hour), 1000); err != nil || removed != 1 {
				t.Fatalf("second cache batch failed: removed=%d error=%v", removed, err)
			}
			nextCatalogTestNotification(t, notifications)
			var nextSequence int64
			if err := pool.QueryRow(ctx, `SELECT sequence FROM notification_source_events WHERE id<>$1`, eventID).Scan(&nextSequence); err != nil || nextSequence <= eventSequence {
				t.Fatalf("later mutation lost cursor ordering: first=%d next=%d error=%v", eventSequence, nextSequence, err)
			}
		})
	}
}

func TestProviderCacheBatchRollsBackDeletionOnNotificationCapacity(t *testing.T) {
	ctx, pool, store, userID, trace := catalogBatchTestStore(t)
	providerCacheBatchFixture(t, ctx, pool)
	providerCacheBatchEnableJournal(t, ctx, pool, userID)
	if _, err := pool.Exec(ctx, `UPDATE notification_journal_state SET sequence=512 WHERE id=1;
		INSERT INTO notification_source_events(id,sequence,kind,refs)
		SELECT md5('provider-cache-capacity-'||n::text),n,'CatalogInvalidated',
		jsonb_build_array(jsonb_build_object('Kind','Item','Id','provider-cache-a','LibraryId','provider-cache-library'))
		FROM generate_series(1,512)n`); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	trace.reset()
	removed, err := store.pruneProviderCacheBatch(ctx, time.Now().Add(-24*time.Hour), 1000)
	if !errors.Is(err, notificationjournal.ErrCapacity) || removed != 0 {
		t.Fatalf("capacity failure did not reject the cache transaction: removed=%d error=%v", removed, err)
	}
	assertNoCatalogTestNotification(t, notifications)
	var retained, events int
	var sequence int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM item_provider_images),
		(SELECT count(*) FROM notification_source_events),sequence FROM notification_journal_state WHERE id=1`).Scan(&retained, &events, &sequence); err != nil || retained != 5 || events != 512 || sequence != 512 {
		t.Fatalf("capacity rollback changed cache or journal: retained=%d events=%d sequence=%d error=%v", retained, events, sequence, err)
	}
	if count := catalogBatchStatementCount(trace, "SELECT goby_record_notification_source("); count != 1 {
		t.Fatalf("rejected cache batch made %d journal calls, want one", count)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_registrations SET source_cursor=512`); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.pruneProviderCacheBatch(ctx, time.Now().Add(-24*time.Hour), 1000); err != nil || removed != 4 {
		t.Fatalf("retry after capacity recovery failed: removed=%d error=%v", removed, err)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), providerCacheBatchChanges())
	assertNoCatalogTestNotification(t, notifications)
}

func TestProviderCacheBatchKeepsBoundedResyncScopeForThousandItems(t *testing.T) {
	ctx, pool, store, userID, trace := catalogBatchTestStore(t)
	providerCacheBatchEnableJournal(t, ctx, pool, userID)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('provider-resync','Provider resync','movies');
		INSERT INTO items(id,library_id,name,sort_name,type)
		SELECT repeat('r',200)||lpad(n::text,4,'0'),'provider-resync',n::text,n::text,'Movie' FROM generate_series(1,1000)n;
		INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash,fetched_at)
		SELECT id,'Primary',0,'tmdb','1','cover',decode('00','hex'),'image/png',1,1,repeat('a',64),clock_timestamp()-interval '10 days'
		FROM items WHERE library_id='provider-resync'`); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	trace.reset()
	removed, err := store.pruneProviderCacheBatch(ctx, time.Now().Add(-24*time.Hour), 1000)
	if err != nil || removed != 1000 {
		t.Fatalf("bounded cache batch failed: removed=%d error=%v", removed, err)
	}
	batch := nextCatalogTestNotification(t, notifications)
	if !batch.Resync || len(batch.Changes) != 0 {
		t.Fatalf("oversized change facts published a partial batch: %+v", batch)
	}
	if count := catalogBatchStatementCount(trace, "SELECT goby_record_notification_source("); count != 1 {
		t.Fatalf("resync cache batch made %d journal calls, want one", count)
	}
	var raw []byte
	var resync bool
	if err := pool.QueryRow(ctx, `SELECT refs,resync FROM notification_source_events`).Scan(&raw, &resync); err != nil {
		t.Fatal(err)
	}
	var refs []notificationjournal.Reference
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatal(err)
	}
	if !resync || len(refs) != 1000 {
		t.Fatalf("resync discarded bounded notification authority: resync=%t refs=%d", resync, len(refs))
	}
	for index, ref := range refs {
		if ref.Kind != "Item" || ref.LibraryID != "provider-resync" || ref.SourceID != ref.ID || index > 0 && refs[index-1].ID >= ref.ID {
			t.Fatalf("resync lost ordered source scope: %+v", ref)
		}
	}
}
