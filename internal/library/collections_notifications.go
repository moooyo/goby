package library

import (
	"github.com/jackc/pgx/v5"
)

// recordCollectionSourceRemovals runs before source rows disappear. Cascading
// membership deletion invalidates collection counts and order, but a generic
// resync keeps private collection identities out of library-wide notifications.
func recordCollectionSourceRemovals(tx pgx.Tx, changes []CatalogChange) error {
	owned, ok := tx.(*ownedTx)
	if !ok || owned == nil || owned.finished {
		return ErrInvalidInput
	}
	if owned.catalogChanges.resync {
		return nil
	}
	var itemIDs, libraryIDs []string
	for _, change := range changes {
		if change.Kind != CatalogRemoved {
			continue
		}
		itemIDs = append(itemIDs, change.ItemID)
		if change.IsCollectionFolder {
			libraryIDs = append(libraryIDs, change.LibraryID)
		}
	}
	if len(itemIDs) == 0 {
		return nil
	}
	if len(itemIDs) > maxCatalogChanges {
		owned.catalogChanges.requireResync()
		return nil
	}
	var affectedIDs []string
	err := tx.QueryRow(owned.ctx, `WITH RECURSIVE affected_ancestors AS (
		SELECT i.id,i.parent_id,i.library_id FROM items i WHERE i.id=ANY($1::text[])
		UNION SELECT parent.id,parent.parent_id,parent.library_id FROM items parent
		JOIN affected_ancestors child ON parent.id=child.parent_id AND parent.library_id=child.library_id
	) SELECT ARRAY(SELECT DISTINCT entry.collection_id FROM media_collection_entries entry JOIN items member ON member.id=entry.item_id
		WHERE member.id IN (SELECT id FROM affected_ancestors) OR member.library_id=ANY($2::text[]) LIMIT 4097)`, itemIDs, libraryIDs).Scan(&affectedIDs)
	if err != nil {
		return err
	}
	if len(affectedIDs) > 0 {
		for _, id := range affectedIDs {
			owned.rememberNotificationScope(collectionLibraryID, id)
		}
		owned.catalogChanges.requireResync()
	}
	return nil
}
