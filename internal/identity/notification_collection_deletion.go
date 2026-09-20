package identity

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/notificationjournal"
)

// Retain only typed collection identities and their trusted library scope before
// cascading account deletion. Dispatch still requires current item visibility;
// no global refresh or historical collection name is authorized by this receipt.
func deletedUserCollectionNotificationReferences(ctx context.Context, tx pgx.Tx, userID string) ([]notificationjournal.Reference, error) {
	rows, err := tx.Query(ctx, `SELECT i.id,i.library_id FROM items i
		JOIN media_collections collection ON collection.item_id=i.id
		WHERE collection.owner_id=$1 OR EXISTS (SELECT 1 FROM media_collection_shares share
			WHERE share.collection_id=collection.item_id AND share.user_id=$1)
		ORDER BY i.id LIMIT 4097`, userID)
	if err != nil {
		return nil, fmt.Errorf("read collection notification identities: %w", err)
	}
	defer rows.Close()
	refs := []notificationjournal.Reference{}
	for rows.Next() {
		ref := notificationjournal.Reference{Kind: "Item"}
		if err := rows.Scan(&ref.ID, &ref.LibraryID); err != nil {
			return nil, fmt.Errorf("read collection notification identity: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish collection notification identities: %w", err)
	}
	// A bounded overflow sentinel lets the journal's transaction decide whether
	// any eligible recipient exists. Disabled notifications must not add a new
	// account-deletion limit; active delivery rejects an unprovable source scope.
	return refs, nil
}
