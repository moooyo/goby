package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type UserDataNotificationQuery struct {
	UserID, ItemID, AfterID string
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
	tx, access, err := s.beginUserRead(ctx, query.UserID)
	if err != nil {
		return UserDataNotificationResult{}, err
	}
	defer rollback(tx)
	var libraryID string
	var folder bool
	err = tx.QueryRow(ctx, `SELECT library_id, is_folder FROM items WHERE id = $1
		AND ($2::boolean OR library_id = ANY($3::text[]))`, query.ItemID, access.all, access.folders).Scan(&libraryID, &folder)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserDataNotificationResult{}, ErrNotFound
	}
	if err != nil {
		return UserDataNotificationResult{}, fmt.Errorf("authorize user data notification target: %w", err)
	}
	rows, err := tx.Query(ctx, userDataNotificationItemsSQL(), query.ItemID, libraryID, query.Recursive && folder, query.AfterID, query.Limit+1)
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
	if err := attachUserData(ctx, tx, query.UserID, items); err != nil {
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
	if strings.TrimSpace(query.UserID) == "" || strings.TrimSpace(query.ItemID) == "" || query.Limit < 0 || query.Limit > 256 {
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

// Both traversals retain the authorized target's library and use UNION to stop
// cycles. Filtering the combined set avoids duplicates where the trees overlap.
// Only identifiers and item types are read before loading the current user data.
func userDataNotificationItemsSQL() string {
	return `WITH RECURSIVE ancestors AS (
		SELECT id, parent_id, library_id FROM items WHERE id = $1::text AND library_id = $2::text
		UNION
		SELECT parent.id, parent.parent_id, parent.library_id FROM ancestors child JOIN items parent
			ON parent.id = child.parent_id AND parent.library_id = child.library_id
	), descendants AS (
		SELECT id, library_id FROM items WHERE id = $1::text AND library_id = $2::text AND $3::boolean
		UNION
		SELECT child.id, child.library_id FROM descendants parent JOIN items child
			ON child.parent_id = parent.id AND child.library_id = parent.library_id
	), related AS (
		SELECT id, library_id FROM ancestors
		UNION
		SELECT id, library_id FROM descendants
	)
	SELECT item.id, item.type FROM related JOIN items item
		ON item.id = related.id AND item.library_id = related.library_id
	WHERE item.type IN (` + userDataFolderTypesSQL + `)
		AND item.id COLLATE "C" > $4::text COLLATE "C"
	ORDER BY item.id COLLATE "C" LIMIT $5`
}
