package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

type collectionProjectionTx struct {
	pgx.Tx
	queries int
}

func (tx *collectionProjectionTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.queries++
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *collectionProjectionTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.queries++
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestCollectionProjectionBatchesMetadataWithoutChangingVisibilityOrEntryOrder(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "projection-reader", false, false, []string{"collection-source-a"})
	libraryIntegrationUser(t, ctx, pool, "projection-administrator", false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=true WHERE id='projection-administrator'`); err != nil {
		t.Fatal(err)
	}
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind,
		CollectionInput{Name: "Ordered playlist", MediaType: "Audio", ItemIDs: []string{"collection-track-a", "collection-track-a", "collection-track-b", "collection-track-c"}})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Shared box", ItemIDs: []string{"collection-track-a", "collection-track-b", "collection-track-c"}})
	empty := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Empty playlist"})
	for _, collection := range []CollectionInfo{playlist, box, empty} {
		shares := []CollectionShare{{UserID: "projection-reader"}, {UserID: "projection-administrator", CanEdit: true}}
		if _, err := store.UpdateCollection(ctx, owner, collection.ID, collection.Kind, CollectionPatch{Shares: &shares}); err != nil {
			t.Fatal(err)
		}
	}
	// A source can acquire an auxiliary role after it was added to a collection.
	// Its surviving entry must no longer contribute to ordinary member counts.
	if _, err := pool.Exec(ctx, `INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES ('collection-track-b','collection-track-a','clip',true)`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		userID        string
		queries       int
		playlistCount int
		boxCount      int
		shares        int
	}{
		{name: "owner", userID: ownerID, queries: 4, playlistCount: 3, boxCount: 2, shares: 2},
		{name: "administrator", userID: "projection-administrator", queries: 4, playlistCount: 3, boxCount: 2, shares: 2},
		{name: "reader", userID: "projection-reader", queries: 3, playlistCount: 2, boxCount: 1, shares: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			subject := Subject{UserID: test.userID}
			baseline := make(map[string]CollectionInfo)
			for _, collection := range []CollectionInfo{playlist, box, empty} {
				info, err := store.GetCollection(ctx, subject, collection.ID, collection.Kind)
				if err != nil {
					t.Fatal(err)
				}
				baseline[collection.ID] = info
			}
			firstState := &UserData{ItemID: playlist.ID, IsFavorite: true}
			secondState := &UserData{ItemID: playlist.ID, Played: true}
			items := []Item{
				{ID: empty.ID, Type: PlaylistKind},
				{ID: playlist.ID, Type: PlaylistKind, PlaylistItemID: "entry-1", UserData: firstState},
				{ID: box.ID, Type: BoxSetKind},
				{ID: playlist.ID, Type: PlaylistKind, PlaylistItemID: "entry-2", UserData: secondState},
				{ID: "collection-track-a", Type: "Audio"},
				{ID: "first-library-root", LibraryID: "collection-source-a", Type: "CollectionFolder"},
				{ID: "second-library-root", LibraryID: "collection-source-a", Type: "CollectionFolder"},
			}
			originalIDs := queryItemIDs(items)
			tx, access, err := store.beginSubjectRead(ctx, subject)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			observed := &collectionProjectionTx{Tx: tx}
			if err := attachCollectionInfo(ctx, observed, access, items); err != nil {
				t.Fatal(err)
			}
			if observed.queries != test.queries {
				t.Fatalf("collection projection used %d queries, want %d for the whole page", observed.queries, test.queries)
			}
			if !reflect.DeepEqual(queryItemIDs(items), originalIDs) || items[1].PlaylistItemID != "entry-1" || items[3].PlaylistItemID != "entry-2" {
				t.Fatal("batch metadata changed page or duplicate entry ordering")
			}
			for _, item := range items[:4] {
				want := baseline[item.ID]
				want.UserData = item.UserData
				if item.Collection == nil || !reflect.DeepEqual(*item.Collection, want) || item.ChildCount == nil || *item.ChildCount != want.ItemCount {
					t.Fatalf("collection metadata differs from its authorized detail: item=%+v want=%+v", item.Collection, want)
				}
				if len(item.Collection.Shares) != test.shares {
					t.Fatalf("sharing roster visibility changed: %+v", item.Collection.Shares)
				}
			}
			if items[0].Collection.ItemCount != 0 || items[1].Collection.ItemCount != test.playlistCount || items[2].Collection.ItemCount != test.boxCount {
				t.Fatal("batch counts lost empty collections, duplicate members, or member visibility")
			}
			if items[1].Collection == items[3].Collection || items[1].Collection.UserData != firstState || items[3].Collection.UserData != secondState {
				t.Fatal("duplicate entries shared their per-entry metadata or user-state projection")
			}
			if items[4].Collection != nil || items[4].ChildCount != nil || items[5].CollectionType != "music" || items[6].CollectionType != "music" {
				t.Fatal("mixed collection page changed ordinary media or collection folder metadata")
			}
		})
	}
}

func TestCollectionProjectionRejectsHiddenCollectionsAndBatchesFolderOnlyPages(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "projection-unshared", false, true, nil)
	private := collectionUserDataCreate(t, ctx, store, Subject{UserID: ownerID}, PlaylistKind, CollectionInput{Name: "Private playlist"})
	tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: "projection-unshared"})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	observed := &collectionProjectionTx{Tx: tx}
	if err := attachCollectionInfo(ctx, observed, access, []Item{{ID: private.ID, Type: PlaylistKind}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private collection projection = %v", err)
	}
	if observed.queries != 1 {
		t.Fatalf("hidden collection loaded counts or sharing data: queries=%d", observed.queries)
	}
	observed.queries = 0
	folders := []Item{
		{ID: "root-a", LibraryID: "collection-source-a", Type: "CollectionFolder"},
		{ID: "root-b", LibraryID: "collection-source-b", Type: "CollectionFolder"},
		{ID: "root-a-repeat", LibraryID: "collection-source-a", Type: "CollectionFolder"},
	}
	if err := attachCollectionInfo(ctx, observed, access, folders); err != nil {
		t.Fatal(err)
	}
	if observed.queries != 1 {
		t.Fatalf("folder page used %d queries, want one", observed.queries)
	}
	for _, folder := range folders {
		if folder.CollectionType != "music" {
			t.Fatalf("folder type = %q", folder.CollectionType)
		}
	}
	observed.queries = 0
	if err := attachCollectionInfo(ctx, observed, access, []Item{{ID: "ordinary", Type: "Movie"}}); err != nil || observed.queries != 0 {
		t.Fatalf("ordinary page performed collection work: queries=%d err=%v", observed.queries, err)
	}
	if err := attachCollectionInfo(ctx, observed, access, []Item{{ID: "missing-root", LibraryID: "missing-library", Type: "CollectionFolder"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing collection folder library = %v", err)
	}
}
