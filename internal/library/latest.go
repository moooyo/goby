package library

import (
	"context"
	"fmt"
	"strings"
)

// LatestItem contains a media item or its container and the matching media count.
type LatestItem struct {
	Item       Item
	ChildCount int
}

// QueryLatest filters accessible media before grouping and paging by newest media.
// Parent scopes always include descendants. A visible same-library container may
// be returned outside that scope to represent matching episodes or audio tracks.
func (s *Store) QueryLatest(ctx context.Context, query Query, group bool) ([]LatestItem, error) {
	query.Recursive = true
	query, err := normalizeItemQuery(query)
	if err != nil {
		return nil, err
	}
	tx, access, err := s.beginUserRead(ctx, query.UserID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	parentLibraryID, err := readQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return nil, err
	}
	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	filter += " AND NOT i.is_folder AND i.type <> 'CollectionFolder'"
	args = append(args, query.Limit, query.StartIndex)
	pagination := fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	statement := prefix + "SELECT " + itemColumns + ", 1 FROM items i WHERE " + filter +
		" ORDER BY i.created_at DESC, i.id ASC" + pagination
	if group {
		statement = latestGroupedSQL(prefix, filter) + pagination
	}
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest library items: %w", err)
	}
	defer rows.Close()
	items := make([]LatestItem, 0)
	for rows.Next() {
		var latest LatestItem
		latest.Item, err = scanItem(rows, &latest.ChildCount)
		if err != nil {
			return nil, fmt.Errorf("scan latest library item: %w", err)
		}
		items = append(items, latest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read latest library items: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete latest library query: %w", err)
	}
	return items, nil
}

func latestGroupedSQL(prefix, filter string) string {
	if prefix == "" {
		prefix = "WITH RECURSIVE "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	// The ancestor walk never leaves a source's library, stops at the first
	// matching container, and rejects repeated IDs in a corrupt parent cycle.
	return prefix + `source_items AS (
		SELECT i.id, i.library_id, i.parent_id, i.type, i.created_at
		FROM items i WHERE ` + filter + `
	), ancestors AS (
		SELECT source.id AS source_id, source.library_id, parent.id, parent.parent_id, parent.type,
			CASE WHEN source.type = 'Episode' THEN 'Series' ELSE 'MusicAlbum' END AS target_type,
			ARRAY[source.id, parent.id] AS visited
		FROM source_items source
		JOIN items parent ON parent.id = source.parent_id AND parent.library_id = source.library_id
		WHERE source.type IN ('Episode', 'Audio') AND parent.id <> source.id
		UNION ALL
		SELECT ancestor.source_id, ancestor.library_id, parent.id, parent.parent_id, parent.type,
			ancestor.target_type, ancestor.visited || parent.id
		FROM ancestors ancestor
		JOIN items parent ON parent.id = ancestor.parent_id AND parent.library_id = ancestor.library_id
		WHERE ancestor.type <> ancestor.target_type AND NOT parent.id = ANY(ancestor.visited)
	), latest_groups AS (
		SELECT COALESCE(ancestor.id, source.id) AS group_id, source.library_id,
			count(*) AS child_count, max(source.created_at) AS newest_created_at
		FROM source_items source
		LEFT JOIN ancestors ancestor ON ancestor.source_id = source.id AND ancestor.type = ancestor.target_type
		GROUP BY COALESCE(ancestor.id, source.id), source.library_id
	)
	SELECT ` + itemColumns + `, latest.child_count
	FROM latest_groups latest
	JOIN items i ON i.id = latest.group_id AND i.library_id = latest.library_id
	WHERE ($1::boolean OR i.library_id = ANY($2::text[]))
	ORDER BY latest.newest_created_at DESC, i.id ASC`
}
