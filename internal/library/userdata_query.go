package library

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

const minimumResumeDurationTicks = 120 * media.TicksPerSecond

const userDataFolderTypesSQL = `'Movie', 'Series', 'Season', 'Episode', 'Video', 'Audio', 'MusicAlbum', 'MusicArtist'`

// QueryResume returns playable resume positions ordered by the user's last play.
func (s *Store) QueryResume(ctx context.Context, query Query) (ItemResult, error) {
	query.Recursive = true
	query.Resumable = true
	// Resume ordering is fixed; ordinary item queries retain their explicit sort.
	query.SortBy = "SortName"
	query.SortOrder = "Ascending"
	return s.queryItems(ctx, query, true)
}

// attachUserData loads one user's data for items already filtered by the caller's
// ACL query. Stored state and folder summaries use batched reads in that transaction.
func attachUserData(ctx context.Context, tx pgx.Tx, userID string, items []Item) error {
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for index := range items {
		items[index].UserData = nil
		if !supportsUserData(items[index].Type) || seen[items[index].ID] {
			continue
		}
		seen[items[index].ID] = true
		ids = append(ids, items[index].ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT "+userDataColumns+` FROM user_item_data
		WHERE user_id = $1 AND item_id = ANY($2::text[])`, userID, ids)
	if err != nil {
		return fmt.Errorf("query item user data: %w", err)
	}
	defer rows.Close()
	stored := make(map[string]UserData, len(ids))
	for _, id := range ids {
		stored[id] = UserData{ItemID: id}
	}
	for rows.Next() {
		data, err := scanUserData(rows)
		if err != nil {
			return fmt.Errorf("scan item user data: %w", err)
		}
		stored[data.ItemID] = data
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read item user data: %w", err)
	}
	rows.Close()
	if err := deriveUserDataFolders(ctx, tx, userID, stored); err != nil {
		return err
	}
	for index := range items {
		if !supportsUserData(items[index].Type) {
			continue
		}
		data := stored[items[index].ID]
		data.ItemID = items[index].ID
		if data.LastPlayedDate != nil {
			date := *data.LastPlayedDate
			data.LastPlayedDate = &date
		}
		if data.UnplayedItemCount != nil {
			count := *data.UnplayedItemCount
			data.UnplayedItemCount = &count
		}
		items[index].UserData = &data
	}
	return nil
}

// deriveUserDataFolders replaces stored playback fields with the current state
// of each authorized folder's playable descendants, retaining its own favorite.
func deriveUserDataFolders(ctx context.Context, tx pgx.Tx, userID string, data map[string]UserData) error {
	if len(data) == 0 {
		return nil
	}
	ids := make([]string, 0, len(data))
	for id := range data {
		ids = append(ids, id)
	}
	rootQuery := `SELECT id, library_id FROM items WHERE id = ANY($1::text[])
		AND is_folder AND type IN (` + userDataFolderTypesSQL + `)`
	rows, err := tx.Query(ctx, folderUserDataCountsSQL(rootQuery, 2), ids, userID)
	if err != nil {
		return fmt.Errorf("query folder user data: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var total, unplayed int
		if err := rows.Scan(&id, &total, &unplayed); err != nil {
			return fmt.Errorf("scan folder user data: %w", err)
		}
		value := data[id]
		value.ItemID = id
		value.PlaybackPositionTicks = 0
		value.PlayCount = 0
		value.LastPlayedDate = nil
		value.Played = total > 0 && unplayed == 0
		value.UnplayedItemCount = &unplayed
		data[id] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read folder user data: %w", err)
	}
	rows.Close()
	return nil
}

// A UNION of stable root/item/library identifiers terminates cycles and keeps
// overlapping requested roots independent without counting a descendant twice.
func folderUserDataCountsSQL(rootQuery string, userParameter int) string {
	return fmt.Sprintf(`WITH RECURSIVE roots AS (%s), folder_descendants AS (
		SELECT roots.id AS root_id, roots.id AS item_id, roots.library_id FROM roots
		UNION
		SELECT parent.root_id, child.id, child.library_id
		FROM folder_descendants parent JOIN items child
			ON child.parent_id = parent.item_id AND child.library_id = parent.library_id
	)
	SELECT roots.id, count(DISTINCT leaf.id) AS total_count,
		count(DISTINCT leaf.id) FILTER (WHERE NOT COALESCE(user_data.played, false)) AS unplayed_count
	FROM roots LEFT JOIN folder_descendants descendant ON descendant.root_id = roots.id
	LEFT JOIN items leaf ON leaf.id = descendant.item_id AND leaf.library_id = descendant.library_id
		AND leaf.id <> roots.id AND NOT leaf.is_folder AND leaf.type IN ('Movie', 'Episode', 'Video', 'Audio')
	LEFT JOIN user_item_data user_data ON user_data.item_id = leaf.id AND user_data.user_id = $%d::text
	GROUP BY roots.id`, rootQuery, userParameter)
}

func itemPlayedSQL(userParameter int) string {
	folderCounts := folderUserDataCountsSQL("SELECT i.id, i.library_id", userParameter)
	return fmt.Sprintf(`CASE WHEN i.is_folder AND i.type IN (%s) THEN
		(SELECT total_count > 0 AND unplayed_count = 0 FROM (%s) folder_counts)
		ELSE EXISTS (SELECT 1 FROM user_item_data user_data
			WHERE user_data.user_id = $%d::text AND user_data.item_id = i.id AND user_data.played)
		END`, userDataFolderTypesSQL, folderCounts, userParameter)
}

func addUserDataConditions(query Query, conditions []string, args []any) ([]string, []any) {
	if query.IsPlayed == nil && query.IsFavorite == nil && !query.Resumable {
		return conditions, args
	}
	args = append(args, query.UserID)
	userParameter := len(args)
	for _, flag := range []struct {
		column string
		value  *bool
	}{
		{column: "played", value: query.IsPlayed},
		{column: "is_favorite", value: query.IsFavorite},
	} {
		if flag.value == nil {
			continue
		}
		args = append(args, *flag.value)
		if flag.column == "played" {
			conditions = append(conditions, fmt.Sprintf("(%s) = $%d::boolean", itemPlayedSQL(userParameter), len(args)))
			continue
		}
		conditions = append(conditions, fmt.Sprintf(`(EXISTS (SELECT 1 FROM user_item_data user_data
			WHERE user_data.user_id = $%d::text AND user_data.item_id = i.id AND user_data.%s)) = $%d::boolean`,
			userParameter, flag.column, len(args)))
	}
	if query.Resumable {
		// The catalog's current duration is authoritative after file replacement
		// or reprobe. Initial resume policy accepts 2% through less than 90% of
		// media lasting at least two minutes. Numeric arithmetic avoids overflow.
		conditions = append(conditions, "NOT i.is_folder", "i.media IS NOT NULL", fmt.Sprintf(`EXISTS (
			SELECT 1 FROM user_item_data user_data
			CROSS JOIN LATERAL (SELECT CASE
				WHEN jsonb_typeof(i.media -> 'DurationTicks') = 'number'
					AND (i.media ->> 'DurationTicks') ~ '^[0-9]{1,19}$'
				THEN CASE WHEN (i.media ->> 'DurationTicks')::numeric <= 9223372036854775807
					THEN (i.media ->> 'DurationTicks')::numeric ELSE 0 END
				ELSE 0 END AS duration_ticks) runtime
			WHERE user_data.user_id = $%d::text AND user_data.item_id = i.id
				AND user_data.playback_position_ticks > 0 AND NOT user_data.played
				AND runtime.duration_ticks >= %d
				AND user_data.playback_position_ticks < runtime.duration_ticks
				AND user_data.playback_position_ticks::numeric * 100 >= runtime.duration_ticks * 2
				AND user_data.playback_position_ticks::numeric * 100 < runtime.duration_ticks * 90)`,
			userParameter, minimumResumeDurationTicks))
	}
	return conditions, args
}
