package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// NextUpQuery contains the catalog filters declared by the pinned TvShowsService
// contract. Fields and image/user-data switches belong to the HTTP projection.
type NextUpQuery struct {
	UserID, SeriesID, ParentID string
	ApplicationCredentialID    string
	StartIndex, Limit          int
}

// The general Item representation uses integer zero for absent indexes. Preserve
// SQL NULLs while ordering candidates and normalize only the selected projection.
var nextUpItemColumns = strings.NewReplacer(
	"i.index_number,", "COALESCE(i.index_number, 0),",
	"i.parent_index_number,", "COALESCE(i.parent_index_number, 0),",
).Replace(itemColumns)

// NextUp selects unwatched episodes from authorized series. Permission checks,
// series activity, selection, counts, and the result page share one user
// snapshot; only the requested user's playback history participates.
func (s *Store) NextUp(ctx context.Context, query NextUpQuery) (ItemResult, error) {
	query, err := normalizeNextUpQuery(query)
	if err != nil {
		return ItemResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return ItemResult{}, err
	}
	defer rollback(tx)
	if query.SeriesID != "" {
		var id string
		err := tx.QueryRow(ctx, `SELECT i.id FROM items i WHERE i.id = $1 AND i.type = 'Series' AND i.is_folder
			AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+ordinaryItemSQL("i"), query.SeriesID, access.all, access.folders).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ItemResult{}, ErrNotFound
		}
		if err != nil {
			return ItemResult{}, fmt.Errorf("authorize next-up series: %w", err)
		}
	}
	if _, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access); err != nil {
		return ItemResult{}, err
	}
	args := []any{query.UserID, access.all, access.folders, query.SeriesID, query.ParentID}
	prefix := nextUpScopeSQL() + nextUpCandidateSQL()
	result := ItemResult{Items: make([]Item, 0)}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM next_up WHERE candidate_rank = 1", args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count next-up episodes: %w", err)
	}
	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, prefix+"SELECT "+nextUpItemColumns+` FROM next_up candidate
		JOIN items i ON i.id = candidate.id AND i.library_id = candidate.library_id
		WHERE candidate.candidate_rank = 1
		ORDER BY candidate.last_activity DESC NULLS LAST, lower(candidate.series_sort_name), candidate.series_id, candidate.episode_order, i.id
		LIMIT $6 OFFSET $7`, args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query next-up episodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return ItemResult{}, fmt.Errorf("read next-up episode: %w", err)
		}
		item.CanPlay = access.canPlay
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read next-up result: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, query.UserID, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete next-up query: %w", err)
	}
	return result, nil
}

func normalizeNextUpQuery(query NextUpQuery) (NextUpQuery, error) {
	if strings.TrimSpace(query.UserID) == "" || !validSubject(Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID}) || query.StartIndex < 0 || query.Limit < 0 || query.Limit > 1000 {
		return NextUpQuery{}, ErrInvalidInput
	}
	for _, value := range []string{query.UserID, query.SeriesID, query.ParentID} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return NextUpQuery{}, ErrInvalidInput
		}
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	return query, nil
}

func nextUpScopeSQL() string {
	return `WITH RECURSIVE parent_scope AS (
		SELECT i.id, i.library_id FROM items i WHERE i.id = $5::text
			AND ($2::boolean OR i.library_id = ANY($3::text[])) AND ` + ordinaryItemSQL("i") + `
		UNION
		SELECT child.id, child.library_id FROM items child JOIN parent_scope parent
			ON child.parent_id = parent.id AND child.library_id = parent.library_id
		WHERE ` + ordinaryItemSQL("child") + `
	), series_tree AS (
		SELECT series.id AS series_id, series.id AS item_id, series.library_id, NULL::integer AS season_number
		FROM items series WHERE series.type = 'Series' AND series.is_folder
			AND ($2::boolean OR series.library_id = ANY($3::text[]))
			AND ($4::text = '' OR series.id = $4) AND ` + ordinaryItemSQL("series") + `
		UNION
		SELECT parent.series_id, child.id, child.library_id,
			CASE WHEN child.type = 'Season' THEN child.index_number ELSE parent.season_number END
		FROM series_tree parent JOIN items child
			ON child.parent_id = parent.item_id AND child.library_id = parent.library_id
		WHERE child.type <> 'Series' AND ` + ordinaryItemSQL("child") + `
	), episode_state AS (
		SELECT episode.id, episode.library_id, tree.series_id, series.sort_name AS series_sort_name,
			COALESCE(episode.parent_index_number, tree.season_number) AS season_number,
			episode.index_number AS episode_number, episode.sort_name,
			COALESCE(data.played, false) AS played,
			(NOT COALESCE(data.played, false) AND COALESCE(data.playback_position_ticks, 0) > 0) AS has_partial,
			(COALESCE(data.played, false) OR COALESCE(data.play_count, 0) > 0
				OR COALESCE(data.playback_position_ticks, 0) > 0 OR data.last_played_at IS NOT NULL) AS has_history,
			CASE WHEN COALESCE(data.played, false) OR COALESCE(data.play_count, 0) > 0
				OR COALESCE(data.playback_position_ticks, 0) > 0 OR data.last_played_at IS NOT NULL
				THEN COALESCE(data.last_played_at, data.updated_at) END AS activity_at,
			($5::text = '' OR episode.id IN (SELECT id FROM parent_scope)) AS in_scope
		FROM series_tree tree JOIN items episode ON episode.id = tree.item_id AND episode.library_id = tree.library_id
		JOIN items series ON series.id = tree.series_id AND series.library_id = tree.library_id
		LEFT JOIN user_item_data data ON data.item_id = episode.id AND data.user_id = $1::text
		WHERE episode.type = 'Episode' AND NOT episode.is_folder AND ` + ordinaryItemSQL("episode") + `
	), ordered_episodes AS (
		SELECT episode_state.*, row_number() OVER (PARTITION BY series_id
			ORDER BY season_number ASC NULLS LAST, episode_number ASC NULLS LAST, lower(sort_name), id) AS episode_order
		FROM episode_state
	), series_activity AS (
		SELECT series_id, bool_or(has_history) AS has_history, max(activity_at) AS last_activity,
			max(episode_order) FILTER (WHERE played) AS last_played_order,
			(array_agg(episode_order ORDER BY activity_at DESC NULLS LAST, episode_order)
				FILTER (WHERE has_partial))[1] AS partial_start_order
		FROM ordered_episodes GROUP BY series_id
	), `
}

// Candidate policy is deliberately isolated from ACL and tree traversal.
// Captured series-scoped queries return all unwatched episodes after the watched
// cursor, or start at the partially watched episode when no episode is watched.
// Goby uses the highest watched ordinal, and otherwise the most recently active
// partial episode with episode order as a stable tie-break. Multiple simultaneous
// partial episodes remain a Goby policy, not a verified reference behavior.
// Global selection remains a Goby policy: one unwatched episode after the watched
// cursor per active series, or the first unwatched episode when only incomplete
// playback history exists.
// Positive global results have not been established by reference captures.
func nextUpCandidateSQL() string {
	return `next_up AS (
		SELECT episode.*, activity.last_activity,
			CASE WHEN $4::text <> '' THEN 1::bigint ELSE row_number() OVER (PARTITION BY episode.series_id
				ORDER BY episode.episode_order) END AS candidate_rank
		FROM ordered_episodes episode JOIN series_activity activity ON activity.series_id = episode.series_id
		WHERE NOT episode.played AND episode.in_scope
			AND (($4::text <> '' AND (
					(activity.last_played_order IS NOT NULL AND episode.episode_order > activity.last_played_order)
					OR (activity.last_played_order IS NULL AND episode.episode_order >= activity.partial_start_order)))
				OR ($4::text = '' AND activity.has_history
					AND episode.episode_order > COALESCE(activity.last_played_order, 0)))
	) `
}
