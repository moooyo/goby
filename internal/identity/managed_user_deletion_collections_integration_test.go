package identity_test

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreDeleteManagedUserReportsOnlyItsCommittedCollectionEffects(t *testing.T) {
	for _, impact := range []string{"none", "owned", "shared"} {
		t.Run(impact, func(t *testing.T) {
			ctx, pool, store, actor, _ := applicationKeyTestStore(t)
			target, err := store.CreateManagedUser(ctx, actor, "Collection Deletion Target", "target-password", false)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := store.CreateManagedUser(ctx, actor, "Collection Deletion Peer", "peer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('deletion-catalog','Deletion Catalog','mixed');
				INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
				('surviving-list','deletion-catalog','Surviving List','Surviving List','Playlist',true),
				('retained-source','deletion-catalog','Retained Source','Retained Source','Movie',false)`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO media_collections(item_id,owner_id,kind) VALUES ('surviving-list',$1,'Playlist')`, peer.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES ('surviving-list','retained-source',0)`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,is_favorite) VALUES ($1,'retained-source',true)`, target.ID); err != nil {
				t.Fatal(err)
			}
			switch impact {
			case "owned":
				if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
					VALUES ('owned-list','deletion-catalog','Owned List','Owned List','Playlist',true)`); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO media_collections(item_id,owner_id,kind) VALUES ('owned-list',$1,'Playlist')`, target.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES ('owned-list','retained-source',0);
					UPDATE items SET parent_id='owned-list' WHERE id='surviving-list'`); err != nil {
					t.Fatal(err)
				}
			case "shared":
				if _, err := pool.Exec(ctx, `INSERT INTO media_collection_shares(collection_id,user_id,can_edit) VALUES ('surviving-list',$1,false)`, target.ID); err != nil {
					t.Fatal(err)
				}
			}
			current := readManagedUser(t, ctx, store, target.ID)
			if result, err := store.DeleteManagedUser(ctx, actor, target.ID, current.Revision+1); !errors.Is(err, identity.ErrRevisionConflict) || result.CollectionsChanged {
				t.Fatalf("rejected deletion reported a committed catalog change: %+v, %v", result, err)
			}
			result, err := store.DeleteManagedUser(ctx, actor, target.ID, current.Revision)
			if err != nil || result.CollectionsChanged != (impact != "none") {
				t.Fatalf("wrong committed collection effect for %s: changed=%t error=%v", impact, result.CollectionsChanged, err)
			}
			var retained, owned, shared, userData bool
			if err := pool.QueryRow(ctx, `SELECT
				EXISTS(SELECT 1 FROM items WHERE id='surviving-list' AND parent_id IS NULL)
				AND EXISTS(SELECT 1 FROM media_collections WHERE item_id='surviving-list' AND owner_id=$2)
				AND EXISTS(SELECT 1 FROM media_collection_entries WHERE collection_id='surviving-list' AND item_id='retained-source')
				AND EXISTS(SELECT 1 FROM items WHERE id='retained-source'),
				EXISTS(SELECT 1 FROM items WHERE id='owned-list'),
				EXISTS(SELECT 1 FROM media_collection_shares WHERE user_id=$1),
				EXISTS(SELECT 1 FROM user_item_data WHERE user_id=$1)`, target.ID, peer.ID).Scan(&retained, &owned, &shared, &userData); err != nil || !retained || owned || shared || userData {
				t.Fatalf("deletion did not retain unrelated catalog data or complete its own cascades: retained=%t owned=%t shared=%t state=%t error=%v", retained, owned, shared, userData, err)
			}
		})
	}
}
