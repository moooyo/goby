package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type collectionPositionEntry struct {
	id       string
	itemID   string
	position int
}

func installCollectionPositionAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// Sequences retain attempted row counts across rollback. Counting every
	// UPDATE, including unchanged values, catches the original write amplification.
	if _, err := pool.Exec(ctx, `CREATE SEQUENCE collection_position_updates;
		CREATE SEQUENCE collection_position_unchanged_updates;
		CREATE FUNCTION count_collection_position_updates() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM nextval('collection_position_updates');
			IF NEW.position = OLD.position THEN
				PERFORM nextval('collection_position_unchanged_updates');
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER count_collection_position_updates AFTER UPDATE OF position ON media_collection_entries
		FOR EACH ROW EXECUTE FUNCTION count_collection_position_updates()`); err != nil {
		t.Fatal(err)
	}
}

func resetCollectionPositionAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE media_collections SET updated_at='2000-01-01T00:00:00Z' WHERE item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER SEQUENCE collection_position_updates RESTART WITH 1;
		ALTER SEQUENCE collection_position_unchanged_updates RESTART WITH 1`); err != nil {
		t.Fatal(err)
	}
}

func assertCollectionPositionWrites(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int64) {
	t.Helper()
	var updates, unchanged int64
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM collection_position_updates),
		(SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM collection_position_unchanged_updates)`).Scan(&updates, &unchanged); err != nil {
		t.Fatal(err)
	}
	if updates != want || unchanged != 0 {
		t.Fatalf("position updates=%d unchanged=%d; want updates=%d unchanged=0", updates, unchanged, want)
	}
}

func readCollectionPositionEntries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) []collectionPositionEntry {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id::text,item_id,position FROM media_collection_entries WHERE collection_id=$1 ORDER BY position,id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var entries []collectionPositionEntry
	for rows.Next() {
		var entry collectionPositionEntry
		if err := rows.Scan(&entry.id, &entry.itemID, &entry.position); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return entries
}

func assertCollectionPositionOrder(t *testing.T, actual, original []collectionPositionEntry, order []int, appended string) {
	t.Helper()
	wantLength := len(order)
	if appended != "" {
		wantLength++
	}
	if len(actual) != wantLength {
		t.Fatalf("entry count=%d, want %d", len(actual), wantLength)
	}
	seen := make(map[string]bool, len(actual))
	for index, entry := range actual {
		if entry.position != index || seen[entry.id] {
			t.Fatalf("noncontiguous position or duplicate entry identity: %+v", actual)
		}
		seen[entry.id] = true
		if index < len(order) {
			prior := original[order[index]]
			if entry.id != prior.id || entry.itemID != prior.itemID {
				t.Fatalf("entry %d=%+v, want original entry %+v", index, entry, prior)
			}
		} else if entry.itemID != appended {
			t.Fatalf("appended media=%q, want %q", entry.itemID, appended)
		}
	}
}

func assertCollectionPositionEffects(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, notifications []CatalogNotification) {
	t.Helper()
	var touched bool
	if err := pool.QueryRow(ctx, `SELECT updated_at>'2000-01-01T00:00:00Z'::timestamptz FROM media_collections WHERE item_id=$1`, id).Scan(&touched); err != nil {
		t.Fatal(err)
	}
	if !touched || len(notifications) != 1 || !notifications[0].Resync || len(notifications[0].Changes) != 0 {
		t.Fatalf("mutation lost touch or private resync: touched=%t notifications=%+v", touched, notifications)
	}
}

func TestCollectionPositionMovesWriteOnlyChangedRows(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	installCollectionPositionAudit(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	for _, test := range []struct {
		name    string
		gapped  bool
		from    int
		to      int
		order   []int
		updates int64
	}{
		{name: "same position", from: 2, to: 2, order: []int{0, 1, 2, 3, 4}},
		{name: "adjacent up", from: 2, to: 1, order: []int{0, 2, 1, 3, 4}, updates: 2},
		{name: "adjacent down", from: 1, to: 2, order: []int{0, 2, 1, 3, 4}, updates: 2},
		{name: "head to tail", from: 0, to: 4, order: []int{1, 2, 3, 4, 0}, updates: 5},
		{name: "tail to head", from: 4, to: 0, order: []int{4, 0, 1, 2, 3}, updates: 5},
		{name: "gapped same position", gapped: true, from: 2, to: 2, order: []int{0, 1, 2, 3, 4}, updates: 4},
		{name: "gapped adjacent", gapped: true, from: 2, to: 1, order: []int{0, 2, 1, 3, 4}, updates: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.SetCatalogChangeListener(nil)
			playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
				Name: test.name, ItemIDs: []string{"collection-track-a", "collection-track-b", "collection-track-a", "collection-track-c", "collection-track-b"},
			})
			if test.gapped {
				if _, err := pool.Exec(ctx, `UPDATE media_collection_entries SET position=position*2 WHERE collection_id=$1`, playlist.ID); err != nil {
					t.Fatal(err)
				}
			}
			original := readCollectionPositionEntries(t, ctx, pool, playlist.ID)
			resetCollectionPositionAudit(t, ctx, pool, playlist.ID)
			var notifications []CatalogNotification
			store.SetCatalogChangeListener(func(value CatalogNotification) { notifications = append(notifications, value) })
			if err := store.MoveCollectionEntry(ctx, owner, playlist.ID, original[test.from].id, test.to); err != nil {
				t.Fatal(err)
			}
			assertCollectionPositionWrites(t, ctx, pool, test.updates)
			assertCollectionPositionOrder(t, readCollectionPositionEntries(t, ctx, pool, playlist.ID), original, test.order, "")
			assertCollectionPositionEffects(t, ctx, pool, playlist.ID, notifications)
		})
	}
}

func TestCollectionPositionAppendAndRemovePreserveNormalization(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	installCollectionPositionAudit(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	for _, test := range []struct {
		name    string
		kind    string
		gapped  bool
		remove  bool
		index   int
		order   []int
		added   int
		updates int64
	}{
		{name: "contiguous append", kind: PlaylistKind, order: []int{0, 1, 2}, added: 1},
		{name: "gapped append", kind: PlaylistKind, gapped: true, order: []int{0, 1, 2}, added: 1, updates: 2},
		{name: "remove tail", kind: PlaylistKind, remove: true, index: 2, order: []int{0, 1}},
		{name: "remove middle", kind: PlaylistKind, remove: true, index: 1, order: []int{0, 2}, updates: 1},
		{name: "gapped remove tail", kind: PlaylistKind, gapped: true, remove: true, index: 2, order: []int{0, 1}, updates: 1},
		{name: "duplicate box member", kind: BoxSetKind, order: []int{0, 1, 2}},
		{name: "gapped duplicate box member", kind: BoxSetKind, gapped: true, order: []int{0, 1, 2}, updates: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.SetCatalogChangeListener(nil)
			collection := collectionUserDataCreate(t, ctx, store, owner, test.kind, CollectionInput{
				Name: test.name, ItemIDs: []string{"collection-track-a", "collection-track-b", "collection-track-c"},
			})
			if test.gapped {
				if _, err := pool.Exec(ctx, `UPDATE media_collection_entries SET position=position*2 WHERE collection_id=$1`, collection.ID); err != nil {
					t.Fatal(err)
				}
			}
			original := readCollectionPositionEntries(t, ctx, pool, collection.ID)
			resetCollectionPositionAudit(t, ctx, pool, collection.ID)
			var notifications []CatalogNotification
			store.SetCatalogChangeListener(func(value CatalogNotification) { notifications = append(notifications, value) })
			appended := ""
			if test.remove {
				if err := store.RemoveCollectionItems(ctx, owner, collection.ID, test.kind, []string{original[test.index].id}); err != nil {
					t.Fatal(err)
				}
			} else {
				if added, err := store.AddCollectionItems(ctx, owner, collection.ID, test.kind, []string{"collection-track-a"}); err != nil || added != test.added {
					t.Fatalf("append count=%d error=%v, want %d", added, err, test.added)
				}
				if test.added != 0 {
					appended = "collection-track-a"
				}
			}
			assertCollectionPositionWrites(t, ctx, pool, test.updates)
			assertCollectionPositionOrder(t, readCollectionPositionEntries(t, ctx, pool, collection.ID), original, test.order, appended)
			assertCollectionPositionEffects(t, ctx, pool, collection.ID, notifications)
		})
	}
}

func TestCollectionPositionMovesPreserveHiddenOrderAndErrorPrecedence(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	installCollectionPositionAudit(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "position-editor", false, false, []string{"collection-source-a"})
	owner, editor := Subject{UserID: ownerID}, Subject{UserID: "position-editor"}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
		Name: "Hidden positions", ItemIDs: []string{"collection-track-c", "collection-track-a", "collection-track-c", "collection-track-b", "collection-track-a"},
	})
	shares := []CollectionShare{{UserID: editor.UserID, CanEdit: true}}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_collection_entries SET position=position*3 WHERE collection_id=$1`, playlist.ID); err != nil {
		t.Fatal(err)
	}
	original := readCollectionPositionEntries(t, ctx, pool, playlist.ID)
	for _, test := range []struct {
		name  string
		entry string
		index int
		want  error
	}{
		{name: "hidden before invalid index", entry: original[0].id, index: 99, want: ErrNotFound},
		{name: "missing before invalid index", entry: "9223372036854775807", index: 99, want: ErrNotFound},
		{name: "visible invalid index", entry: original[1].id, index: 99, want: ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetCollectionPositionAudit(t, ctx, pool, playlist.ID)
			var notifications []CatalogNotification
			store.SetCatalogChangeListener(func(value CatalogNotification) { notifications = append(notifications, value) })
			if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, test.entry, test.index); !errors.Is(err, test.want) {
				t.Fatalf("move error=%v, want %v", err, test.want)
			}
			// Normalization precedes target validation, so its four attempted
			// writes remain observable while every catalog change rolls back.
			assertCollectionPositionWrites(t, ctx, pool, 4)
			if actual := readCollectionPositionEntries(t, ctx, pool, playlist.ID); !reflect.DeepEqual(actual, original) || len(notifications) != 0 {
				t.Fatalf("rejected move changed entries or notified: entries=%+v notifications=%+v", actual, notifications)
			}
		})
	}
	resetCollectionPositionAudit(t, ctx, pool, playlist.ID)
	var notifications []CatalogNotification
	store.SetCatalogChangeListener(func(value CatalogNotification) { notifications = append(notifications, value) })
	if err := store.MoveCollectionEntry(ctx, editor, playlist.ID, original[1].id, 1); err != nil {
		t.Fatal(err)
	}
	assertCollectionPositionWrites(t, ctx, pool, 7)
	assertCollectionPositionOrder(t, readCollectionPositionEntries(t, ctx, pool, playlist.ID), original, []int{0, 2, 3, 1, 4}, "")
	assertCollectionPositionEffects(t, ctx, pool, playlist.ID, notifications)
}

func TestCollectionPositionNoopRetainsFinalActorAuthorization(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	installCollectionPositionAudit(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Noop authority", ItemIDs: []string{"collection-track-a"}})
	original := readCollectionPositionEntries(t, ctx, pool, playlist.ID)
	resetCollectionPositionAudit(t, ctx, pool, playlist.ID)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES('position-credential',$1,decode(repeat('af',32),'hex'),'emby',clock_timestamp()+interval '1 hour');`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION revoke_collection_position_actor() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='position-credential'; RETURN NEW; END $$;
		CREATE TRIGGER revoke_collection_position_actor AFTER UPDATE ON media_collections
		FOR EACH ROW EXECUTE FUNCTION revoke_collection_position_actor()`); err != nil {
		t.Fatal(err)
	}
	actor := identity.Principal{User: identity.User{ID: ownerID}, SessionID: "position-credential", Kind: "emby", PeerIP: "127.0.0.1"}
	var notifications []CatalogNotification
	store.SetCatalogChangeListener(func(value CatalogNotification) { notifications = append(notifications, value) })
	if err := store.MoveCollectionEntry(WithCollectionActor(ctx, actor), owner, playlist.ID, original[0].id, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("same-position move bypassed final authority: %v", err)
	}
	assertCollectionPositionWrites(t, ctx, pool, 0)
	var unchanged, active bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT updated_at='2000-01-01T00:00:00Z'::timestamptz FROM media_collections WHERE item_id=$1),
		(SELECT revoked_at IS NULL FROM sessions WHERE id='position-credential')`, playlist.ID).Scan(&unchanged, &active); err != nil {
		t.Fatal(err)
	}
	if !unchanged || !active || len(notifications) != 0 {
		t.Fatalf("authority failure did not roll back touch and revocation: unchanged=%t active=%t notifications=%+v", unchanged, active, notifications)
	}
}

func TestCollectionPositionFullPlaylistUsesTwoAdjacentUpdates(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	installCollectionPositionAudit(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Full position population"})
	if _, err := pool.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position)
		SELECT $1,'collection-track-a',n FROM generate_series(0,9999)n`, playlist.ID); err != nil {
		t.Fatal(err)
	}
	var entry, neighbor string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT id::text FROM media_collection_entries WHERE collection_id=$1 AND position=5000),
		(SELECT id::text FROM media_collection_entries WHERE collection_id=$1 AND position=5001)`, playlist.ID).Scan(&entry, &neighbor); err != nil {
		t.Fatal(err)
	}
	resetCollectionPositionAudit(t, ctx, pool, playlist.ID)
	if err := store.MoveCollectionEntry(ctx, owner, playlist.ID, entry, 5001); err != nil {
		t.Fatal(err)
	}
	assertCollectionPositionWrites(t, ctx, pool, 2)
	var count, entryPosition, neighborPosition int
	if err := pool.QueryRow(ctx, `SELECT count(*),
		max(position) FILTER (WHERE id::text=$2),max(position) FILTER (WHERE id::text=$3)
		FROM media_collection_entries WHERE collection_id=$1`, playlist.ID, entry, neighbor).Scan(&count, &entryPosition, &neighborPosition); err != nil {
		t.Fatal(err)
	}
	if count != collectionEntryLimit || entryPosition != 5001 || neighborPosition != 5000 {
		t.Fatalf("full playlist move changed population/order: count=%d entry=%d neighbor=%d", count, entryPosition, neighborPosition)
	}
	t.Logf("collection_position_observation entries=%d adjacent_position_updates=2 unchanged_position_updates=0", count)
}
