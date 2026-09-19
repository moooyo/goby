package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// The original Web client requests its complete episode queue without paging.
// Reject an oversized series explicitly instead of silently dropping its tail.
const MaxEpisodePlaybackQueueItems = 1000

var ErrEpisodePlaybackQueueLimit = errors.New("episode playback queue exceeds its item limit")

// EpisodePlaybackQueue shares NextUp's authorized series traversal and episode
// ordering, but retains played episodes: explicit replay is a client decision.
// The client chooses the current item and whether to advance automatically.
// This read never prepares playback, advances history, or grants future access.
func (s *Store) EpisodePlaybackQueue(ctx context.Context, subject Subject, seriesID string) (ItemResult, error) {
	if !validSubject(subject) || strings.TrimSpace(seriesID) == "" || !utf8.ValidString(seriesID) || strings.ContainsRune(seriesID, '\x00') {
		return ItemResult{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return ItemResult{}, err
	}
	defer rollback(tx)
	var authorized string
	err = tx.QueryRow(ctx, `SELECT i.id FROM items i WHERE i.id=$1 AND i.type='Series' AND i.is_folder
		AND ($2::boolean OR i.library_id=ANY($3::text[])) AND `+access.ordinarySQL("i"), seriesID, access.all, access.folders).Scan(&authorized)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemResult{}, ErrNotFound
	}
	if err != nil {
		return ItemResult{}, fmt.Errorf("authorize episode playback queue: %w", err)
	}
	if !access.canPlay {
		return ItemResult{}, ErrForbidden
	}
	args := []any{subject.UserID, access.all, access.folders, seriesID, "", strconv.Itoa(media.CurrentProbeVersion)}
	prefix := nextUpScopeSQL(access) + `episode_queue AS (
		SELECT episode.id,episode.library_id,episode.episode_order FROM ordered_episodes episode
		JOIN items source ON source.id=episode.id AND source.library_id=episode.library_id
		WHERE source.media->>'ProbeVersion'=$6 AND ` + navigationNumberSQL("source.media", "FileChangeTimeNs") + `>0
		AND ` + navigationNumberSQL("source.media", "DurationTicks") + `>0
	) `
	result := ItemResult{Items: make([]Item, 0)}
	if err := tx.QueryRow(ctx, prefix+`SELECT count(*) FROM episode_queue`, args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count episode playback queue: %w", err)
	}
	if result.TotalRecordCount > MaxEpisodePlaybackQueueItems {
		return ItemResult{}, ErrEpisodePlaybackQueueLimit
	}
	rows, err := tx.Query(ctx, prefix+`SELECT `+access.scopeSQL(nextUpItemColumns)+` FROM episode_queue queue
		JOIN items i ON i.id=queue.id AND i.library_id=queue.library_id ORDER BY queue.episode_order`, args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query episode playback queue: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return ItemResult{}, fmt.Errorf("read episode playback queue: %w", err)
		}
		item.CanPlay = access.canPlay
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read episode playback queue result: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, subject.UserID, result.Items, access); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete episode playback queue: %w", err)
	}
	return result, nil
}
