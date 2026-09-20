package library

import (
	"context"
	"fmt"
	"strings"
)

// QuerySuggestions selects local playable catalog entries. Its default ranking
// prefers explicit favorites/likes, then unplayed entries, then recent catalog
// additions. Explicit item ordering replaces that ranking. All predicates,
// counts, ranking and state projection share the same subject read snapshot.
func (s *Store) QuerySuggestions(ctx context.Context, query Query) (ItemResult, error) {
	explicitSort := strings.TrimSpace(query.SortBy) != ""
	query, err := normalizeItemQuery(query)
	if err != nil {
		return ItemResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return ItemResult{}, err
	}
	defer tx.Rollback(ctx)
	parentLibraryID, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return ItemResult{}, err
	}
	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	filter += " AND NOT i.is_folder AND i.media IS NOT NULL AND i.type IN (" + userDataPlayableTypesSQL + ")"
	result := ItemResult{Items: []Item{}}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM items i WHERE "+filter, args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count suggestions: %w", err)
	}
	userParameter := 0
	if query.UserID != "" && (!explicitSort || itemSortUsesUserData(query.SortBy)) {
		args = append(args, query.UserID)
		userParameter = len(args)
	}
	order := "i.created_at DESC, i.id ASC"
	if explicitSort {
		order = itemOrderSQL(query, userParameter)
	} else if userParameter != 0 {
		order = fmt.Sprintf(`EXISTS (SELECT 1 FROM user_item_data suggestion_state
			WHERE suggestion_state.user_id=$%d::text AND suggestion_state.item_id=i.id
			AND (suggestion_state.is_favorite OR suggestion_state.likes IS TRUE)) DESC,
			EXISTS (SELECT 1 FROM user_item_data suggestion_state WHERE suggestion_state.user_id=$%d::text
			AND suggestion_state.item_id=i.id AND suggestion_state.played) ASC,
			i.created_at DESC, i.id ASC`, userParameter, userParameter)
	}
	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, prefix+"SELECT "+access.itemColumnsSQL()+" FROM items i WHERE "+filter+
		" ORDER BY "+access.scopeSQL(order)+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query suggestions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return ItemResult{}, fmt.Errorf("scan suggestion: %w", err)
		}
		item.CanPlay = access.canPlay
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read suggestions: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, query.UserID, result.Items, access); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete suggestions: %w", err)
	}
	return result, nil
}
