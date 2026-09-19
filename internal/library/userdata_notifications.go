package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type UserDataNotificationQuery struct {
	UserID, ItemID, AfterID string
	ApplicationCredentialID string
	Recursive               bool
	Limit                   int
}

type UserDataNotificationResult struct {
	Items       []UserData
	NextAfterID string
}

// UserDataNotificationPage reads current state after the caller's write commits.
// Each page independently authorizes its target and uses one repeatable-read
// snapshot for related item IDs and user data. The cursor is exclusive and uses
// bytewise item-ID order; an empty NextAfterID means that this page has no more
// results in its snapshot. Later pages may observe newer committed state.
func (s *Store) UserDataNotificationPage(ctx context.Context, query UserDataNotificationQuery) (UserDataNotificationResult, error) {
	query, err := normalizeUserDataNotificationQuery(query)
	if err != nil {
		return UserDataNotificationResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return UserDataNotificationResult{}, err
	}
	defer rollback(tx)
	// Numeric entity IDs occupy their own namespace. Their state belongs to the
	// entity itself, never to its associated media or those items' ancestors.
	if entityID, parseErr := strconv.ParseInt(query.ItemID, 10, 64); parseErr == nil &&
		entityID > 0 && strconv.FormatInt(entityID, 10) == query.ItemID {
		if err := visibleEntityForState(ctx, tx, access, entityID, false); err != nil {
			return UserDataNotificationResult{}, err
		}
		result := UserDataNotificationResult{Items: []UserData{}}
		if query.AfterID == "" || query.AfterID < query.ItemID {
			data, err := scanEntityUserData(tx.QueryRow(ctx, "SELECT "+entityUserDataColumns+
				" FROM entity_user_data WHERE user_id=$1 AND entity_id=$2", query.UserID, entityID))
			if errors.Is(err, pgx.ErrNoRows) {
				data, err = UserData{ItemID: query.ItemID}, nil
			}
			if err != nil {
				return UserDataNotificationResult{}, fmt.Errorf("read entity notification state: %w", err)
			}
			result.Items = append(result.Items, data)
		}
		if err := tx.Commit(ctx); err != nil {
			return UserDataNotificationResult{}, fmt.Errorf("complete entity notification read: %w", err)
		}
		return result, nil
	}
	var libraryID string
	var folder bool
	err = tx.QueryRow(ctx, `SELECT i.library_id, i.is_folder FROM items i WHERE i.id = $1
		AND `+access.directSQL("i"), query.ItemID).Scan(&libraryID, &folder)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserDataNotificationResult{}, ErrNotFound
	}
	if err != nil {
		return UserDataNotificationResult{}, fmt.Errorf("authorize user data notification target: %w", err)
	}
	rows, err := tx.Query(ctx, userDataNotificationItemsSQL(access), query.ItemID, libraryID, query.Recursive && folder, query.AfterID, query.Limit+1)
	if err != nil {
		return UserDataNotificationResult{}, fmt.Errorf("query user data notification items: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0, query.Limit+1)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Type); err != nil {
			return UserDataNotificationResult{}, fmt.Errorf("scan user data notification item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return UserDataNotificationResult{}, fmt.Errorf("read user data notification items: %w", err)
	}
	rows.Close()
	result := UserDataNotificationResult{Items: make([]UserData, 0, query.Limit)}
	if len(items) > query.Limit {
		items = items[:query.Limit]
		result.NextAfterID = items[len(items)-1].ID
	}
	if err := attachUserData(ctx, tx, query.UserID, items, access); err != nil {
		return UserDataNotificationResult{}, err
	}
	for _, item := range items {
		if item.UserData != nil {
			result.Items = append(result.Items, *item.UserData)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return UserDataNotificationResult{}, fmt.Errorf("complete user data notification read: %w", err)
	}
	return result, nil
}

func normalizeUserDataNotificationQuery(query UserDataNotificationQuery) (UserDataNotificationQuery, error) {
	if strings.TrimSpace(query.UserID) == "" || !validSubject(Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID}) || strings.TrimSpace(query.ItemID) == "" || query.Limit < 0 || query.Limit > 256 {
		return UserDataNotificationQuery{}, ErrInvalidInput
	}
	for _, value := range []string{query.UserID, query.ItemID, query.AfterID} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return UserDataNotificationQuery{}, ErrInvalidInput
		}
	}
	if query.Limit == 0 {
		query.Limit = 64
	}
	return query, nil
}

// Physical edges remain within a library; collection membership may cross it.
// Reverse membership includes every visible container whose derived state can
// change. UNION bounds cycles and repeated playlist occurrences before paging.
func userDataNotificationItemsSQL(scopes ...libraryAccess) string {
	access := unrestrictedLibraryAccess()
	if len(scopes) != 0 {
		access = scopes[0]
	}
	return `WITH RECURSIVE descendants AS (
		SELECT i.id,i.parent_id,i.library_id FROM items i
		WHERE i.id=$1::text AND i.library_id=$2::text AND $3::boolean AND ` + access.ordinarySQL("i") + `
		UNION SELECT child.id,child.parent_id,child.library_id
		FROM descendants walk JOIN items parent ON parent.id=walk.id AND parent.library_id=walk.library_id
		JOIN LATERAL (
			SELECT physical.id FROM items physical WHERE physical.parent_id=parent.id AND physical.library_id=parent.library_id AND parent.type NOT IN ('Playlist','BoxSet')
			UNION SELECT membership.item_id FROM media_collection_entries membership WHERE membership.collection_id=parent.id
				AND parent.type IN ('Playlist','BoxSet') AND EXISTS(SELECT 1 FROM items seed WHERE seed.id=$1::text AND seed.type IN ('Playlist','BoxSet'))
		) relationship ON true JOIN items child ON child.id=relationship.id
		WHERE ` + access.ordinarySQL("parent") + ` AND ` + access.ordinarySQL("child") + `
	), ancestors AS (
		SELECT i.id, i.parent_id, i.library_id FROM items i WHERE i.id = $1::text AND i.library_id = $2::text AND ` + access.directSQL("i") + `
		UNION
		SELECT id,parent_id,library_id FROM descendants
		UNION
		SELECT parent.id, parent.parent_id, parent.library_id FROM ancestors walk
		JOIN LATERAL (
			SELECT physical.id FROM items physical WHERE physical.id=walk.parent_id AND physical.library_id=walk.library_id
			UNION SELECT membership.collection_id FROM media_collection_entries membership
				JOIN items membership_item ON membership_item.id=membership.item_id
				WHERE membership.item_id=walk.id AND ` + access.ordinarySQL("membership_item") + `
		) relationship ON true JOIN items parent ON parent.id=relationship.id
		WHERE ` + access.ordinarySQL("parent") + `
	), related AS (
		SELECT id, library_id FROM ancestors
		UNION
		SELECT id, library_id FROM descendants
	)
	SELECT item.id, item.type FROM related JOIN items item
		ON item.id = related.id AND item.library_id = related.library_id
	WHERE item.type IN (` + userDataFolderTypesSQL + `)
		AND (` + access.ordinarySQL("item") + ` OR (item.id = $1::text AND ` + access.directSQL("item") + `))
		AND item.id COLLATE "C" > $4::text COLLATE "C"
	ORDER BY item.id COLLATE "C" LIMIT $5`
}
