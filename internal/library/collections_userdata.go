package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A BoxSet batch and collection membership mutations share the catalog owner.
// Keeping that transaction through user-data commit prevents its selected graph
// from changing between member locks and the returned aggregate. Ordinary media
// and physical folders retain the existing independent state transaction path.
func (s *Store) beginPlayedStateWrite(ctx context.Context, subject Subject, itemID string) (pgx.Tx, libraryAccess, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return s.beginSubjectStateWrite(ctx, subject, false)
	}
	var kind string
	err := s.pool.QueryRow(ctx, `SELECT kind FROM media_collections WHERE item_id=$1`, itemID).Scan(&kind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, libraryAccess{}, err
	}
	if kind != BoxSetKind {
		return s.beginSubjectStateWrite(ctx, subject, false)
	}
	tx, access, err := s.beginCollectionWriteWithScope(ctx, subject, itemID, nil, true)
	if err != nil {
		return nil, libraryAccess{}, err
	}
	if subject.ApplicationCredentialID != "" {
		access = unrestrictedLibraryAccess()
		access.userID = subject.UserID
	}
	return tx, access, nil
}

// The stable root/item/library tuple gives collection graphs set semantics.
// Duplicate entry IDs and overlapping album/series references never multiply
// a media item's personal playback state. UNION also terminates corrupt cycles.
func collectionUserDataDescendantsSQL(rootQuery string, access libraryAccess) string {
	return `WITH RECURSIVE roots AS (` + rootQuery + `), collection_userdata_descendants AS (
		SELECT roots.id AS root_id,roots.id AS item_id,roots.library_id FROM roots
		UNION
		SELECT walk.root_id,child.id,child.library_id
		FROM collection_userdata_descendants walk JOIN items parent ON parent.id=walk.item_id AND parent.library_id=walk.library_id
		JOIN LATERAL (
			SELECT physical.id FROM items physical WHERE physical.parent_id=parent.id AND physical.library_id=parent.library_id
				AND parent.type NOT IN ('Playlist','BoxSet')
			UNION SELECT membership.item_id FROM media_collection_entries membership
				WHERE membership.collection_id=parent.id AND parent.type IN ('Playlist','BoxSet')
		) relationship ON true JOIN items child ON child.id=relationship.id
		WHERE ` + access.ordinarySQL("parent") + ` AND ` + access.ordinarySQL("child") + `
	) `
}

func collectionUserDataCountsSQL(rootQuery string, userParameter int, access libraryAccess) string {
	return collectionUserDataDescendantsSQL(rootQuery, access) + fmt.Sprintf(`SELECT roots.id,
		count(DISTINCT leaf.id) AS total_count,
		count(DISTINCT leaf.id) FILTER (WHERE NOT COALESCE(user_data.played,false)) AS unplayed_count
		FROM roots LEFT JOIN collection_userdata_descendants descendant ON descendant.root_id=roots.id
		LEFT JOIN items leaf ON leaf.id=descendant.item_id AND leaf.library_id=descendant.library_id
			AND leaf.id<>roots.id AND NOT leaf.is_folder AND leaf.type IN (`+userDataPlayableTypesSQL+`) AND `+access.ordinarySQL("leaf")+`
		LEFT JOIN user_item_data user_data ON user_data.item_id=leaf.id AND user_data.user_id=$%d::text
		GROUP BY roots.id`, userParameter)
}

func deriveCollectionUserData(ctx context.Context, tx pgx.Tx, userID string, data map[string]UserData, access libraryAccess) error {
	ids := make([]string, 0, len(data))
	for id := range data {
		ids = append(ids, id)
	}
	rootQuery := `SELECT i.id,i.library_id FROM items i WHERE i.id=ANY($1::text[])
		AND i.is_folder AND i.type IN ('Playlist','BoxSet') AND ` + access.ordinarySQL("i")
	rows, err := tx.Query(ctx, collectionUserDataCountsSQL(rootQuery, 2, access), ids, userID)
	if err != nil {
		return fmt.Errorf("query collection user data: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var total, unplayed int
		if err := rows.Scan(&id, &total, &unplayed); err != nil {
			return fmt.Errorf("scan collection user data: %w", err)
		}
		applyFolderUserData(data, id, total, unplayed)
	}
	return rows.Err()
}

func lockCollectionPlayedTargets(ctx context.Context, tx pgx.Tx, root stateItem, access libraryAccess) ([]string, error) {
	rootQuery := `SELECT i.id,i.library_id FROM items i WHERE i.id=$1 AND i.library_id=$2 AND ` + access.ordinarySQL("i")
	statement := collectionUserDataDescendantsSQL(rootQuery, access) + `SELECT i.id FROM collection_userdata_descendants target
		JOIN items i ON i.id=target.item_id AND i.library_id=target.library_id
		WHERE i.type IN (` + userDataFolderTypesSQL + `) AND ` + access.ordinarySQL("i") + ` ORDER BY i.id FOR SHARE OF i`
	rows, err := tx.Query(ctx, statement, root.id, root.libraryID)
	if err != nil {
		return nil, fmt.Errorf("lock played collection members: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// Metadata classification may have changed while an item lock was pending.
	// Recheck every unique media identity under the already locked authority.
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM items i WHERE i.id=ANY($1::text[]) AND `+access.ordinarySQL("i"), ids).Scan(&visible); err != nil {
		return nil, err
	}
	if visible == 0 || visible != len(ids) {
		return nil, ErrNotFound
	}
	return ids, nil
}
