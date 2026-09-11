package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

type UserData struct {
	ItemID                string
	PlaybackPositionTicks int64
	PlayCount             int
	IsFavorite, Played    bool
	LastPlayedDate        *time.Time
	UnplayedItemCount     *int
}

// playbackAllowed is shared by source opening and event handling. Playback
// policy applies to administrators too; malformed policy fails closed.
func playbackAllowed(policy []byte) bool {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(policy, &values); err != nil || values == nil {
		return false
	}
	raw, exists := values["EnableMediaPlayback"]
	if !exists {
		return true
	}
	var enabled *bool
	return json.Unmarshal(raw, &enabled) == nil && enabled != nil && *enabled
}

func supportsUserData(itemType string) bool {
	switch itemType {
	case "Movie", "Series", "Season", "Episode", "Video", "Audio", "MusicAlbum", "MusicArtist":
		return true
	default:
		return false
	}
}

type stateItem struct {
	id, itemType, libraryID string
	isFolder                bool
	duration                int64
}

// Writes lock the account before reading its policy, so a concurrent disable or
// library-policy change cannot commit before a stale authorization writes data.
// These short transactions use the pool independently from the scan owner.
func (s *Store) beginStateWrite(ctx context.Context, userID string, requirePlayback bool) (pgx.Tx, libraryAccess, error) {
	if s == nil || s.pool == nil || strings.TrimSpace(userID) == "" || strings.ContainsRune(userID, '\x00') {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, libraryAccess{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, libraryAccess{}, fmt.Errorf("begin user state update: %w", err)
	}
	var administrator, disabled bool
	var policy []byte
	err = tx.QueryRow(ctx, `SELECT is_administrator, is_disabled, policy FROM users
		WHERE id = $1 FOR SHARE`, userID).Scan(&administrator, &disabled, &policy)
	if err != nil {
		rollback(tx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, libraryAccess{}, ErrForbidden
		}
		return nil, libraryAccess{}, fmt.Errorf("authorize user state update: %w", err)
	}
	if disabled || (requirePlayback && !playbackAllowed(policy)) {
		rollback(tx)
		return nil, libraryAccess{}, ErrForbidden
	}
	if administrator {
		return tx, libraryAccess{all: true, folders: []string{}}, nil
	}
	access, err := parseLibraryPolicy(policy)
	if err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	return tx, access, nil
}

func lockStateItem(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID string, playable bool) (stateItem, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') {
		return stateItem{}, ErrInvalidInput
	}
	var item stateItem
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT i.id, i.type, i.library_id, i.is_folder, i.media FROM items i
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+directItemSQL("i")+` FOR SHARE OF i`,
		itemID, access.all, access.folders).Scan(&item.id, &item.itemType, &item.libraryID, &item.isFolder, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return stateItem{}, ErrNotFound
	}
	if err != nil {
		return stateItem{}, fmt.Errorf("authorize user state item: %w", err)
	}
	// A scanner locks affected items before changing theme classification. If
	// this statement waited for that lock, its original READ COMMITTED snapshot
	// may predate the marker or relationship change; reread after acquiring SHARE.
	var direct bool
	if err := tx.QueryRow(ctx, "SELECT "+directItemSQL("i")+" FROM items i WHERE i.id=$1", itemID).Scan(&direct); err != nil {
		return stateItem{}, fmt.Errorf("recheck user state item visibility: %w", err)
	}
	if !direct {
		return stateItem{}, ErrNotFound
	}
	if !supportsUserData(item.itemType) {
		return stateItem{}, ErrNotFound
	}
	if len(raw) > 0 && string(raw) != "null" {
		var info media.Info
		if err := json.Unmarshal(raw, &info); err != nil {
			return stateItem{}, fmt.Errorf("read user state media duration: %w", err)
		}
		item.duration = info.DurationTicks
	}
	if item.duration < 0 || (playable && (item.isFolder || len(raw) == 0 || string(raw) == "null")) {
		return stateItem{}, ErrNotFound
	}
	return item, nil
}

const userDataColumns = `item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at`

func scanUserData(row rowScanner) (UserData, error) {
	var data UserData
	err := row.Scan(&data.ItemID, &data.PlaybackPositionTicks, &data.PlayCount, &data.IsFavorite, &data.Played, &data.LastPlayedDate)
	if data.LastPlayedDate != nil {
		utc := data.LastPlayedDate.UTC()
		data.LastPlayedDate = &utc
	}
	return data, err
}

func lockUserData(ctx context.Context, tx pgx.Tx, userID, itemID string) (UserData, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO user_item_data (user_id, item_id) VALUES ($1, $2)
		ON CONFLICT (user_id, item_id) DO NOTHING`, userID, itemID); err != nil {
		return UserData{}, fmt.Errorf("initialize user item data: %w", err)
	}
	data, err := scanUserData(tx.QueryRow(ctx, "SELECT "+userDataColumns+" FROM user_item_data WHERE user_id = $1 AND item_id = $2 FOR UPDATE", userID, itemID))
	if err != nil {
		return UserData{}, fmt.Errorf("lock user item data: %w", err)
	}
	return data, nil
}

func (s *Store) GetUserData(ctx context.Context, userID, itemID string) (UserData, error) {
	return s.GetUserDataFor(ctx, Subject{UserID: userID}, itemID)
}

// GetUserDataFor always requires an explicit target account.
func (s *Store) GetUserDataFor(ctx context.Context, subject Subject, itemID string) (UserData, error) {
	items, err := s.GetUserDataBatchFor(ctx, subject, []string{itemID})
	if err != nil {
		return UserData{}, err
	}
	data, exists := items[itemID]
	if !exists {
		return UserData{}, ErrNotFound
	}
	return data, nil
}

// GetUserDataBatch returns authorized supported items, including their defaults.
// Missing, unsupported, and invisible identifiers are absent from the map.
func (s *Store) GetUserDataBatch(ctx context.Context, userID string, itemIDs []string) (map[string]UserData, error) {
	return s.GetUserDataBatchFor(ctx, Subject{UserID: userID}, itemIDs)
}

// GetUserDataBatchFor projects an existing target within its catalog visibility.
func (s *Store) GetUserDataBatchFor(ctx context.Context, subject Subject, itemIDs []string) (map[string]UserData, error) {
	if strings.TrimSpace(subject.UserID) == "" || len(itemIDs) > 1000 {
		return nil, ErrInvalidInput
	}
	userID := subject.UserID
	for _, id := range itemIDs {
		if strings.TrimSpace(id) == "" || strings.ContainsRune(id, '\x00') {
			return nil, ErrInvalidInput
		}
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `SELECT i.id, COALESCE(data.playback_position_ticks, 0),
		COALESCE(data.play_count, 0), COALESCE(data.is_favorite, false), COALESCE(data.played, false), data.last_played_at
		FROM items i LEFT JOIN user_item_data data ON data.item_id = i.id AND data.user_id = $1
		WHERE i.id = ANY($2::text[]) AND ($3::boolean OR i.library_id = ANY($4::text[]))
		AND i.type IN ('Movie','Series','Season','Episode','Video','Audio','MusicAlbum','MusicArtist') AND `+directItemSQL("i"),
		userID, itemIDs, access.all, access.folders)
	if err != nil {
		return nil, fmt.Errorf("query user item data: %w", err)
	}
	defer rows.Close()
	result := make(map[string]UserData)
	for rows.Next() {
		data, err := scanUserData(rows)
		if err != nil {
			return nil, fmt.Errorf("read user item data: %w", err)
		}
		result[data.ItemID] = data
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user item data list: %w", err)
	}
	rows.Close()
	if err := deriveUserDataFolders(ctx, tx, userID, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete user item data read: %w", err)
	}
	return result, nil
}

func (s *Store) SetFavorite(ctx context.Context, userID, itemID string, favorite bool) (UserData, error) {
	return s.SetFavoriteFor(ctx, Subject{UserID: userID}, itemID, favorite)
}

// SetFavoriteFor updates only the explicitly selected account's item state.
func (s *Store) SetFavoriteFor(ctx context.Context, subject Subject, itemID string, favorite bool) (UserData, error) {
	if strings.TrimSpace(subject.UserID) == "" {
		return UserData{}, ErrInvalidInput
	}
	userID := subject.UserID
	tx, access, err := s.beginSubjectStateWrite(ctx, subject, false)
	if err != nil {
		return UserData{}, err
	}
	defer rollback(tx)
	if _, err := lockStateItem(ctx, tx, access, itemID, false); err != nil {
		return UserData{}, err
	}
	if _, err := lockUserData(ctx, tx, userID, itemID); err != nil {
		return UserData{}, err
	}
	data, err := scanUserData(tx.QueryRow(ctx, `UPDATE user_item_data SET is_favorite = $3, updated_at = now()
		WHERE user_id = $1 AND item_id = $2 RETURNING `+userDataColumns, userID, itemID, favorite))
	if err != nil {
		return UserData{}, fmt.Errorf("set favorite state: %w", err)
	}
	derived := map[string]UserData{itemID: data}
	if err := deriveUserDataFolders(ctx, tx, userID, derived); err != nil {
		return UserData{}, err
	}
	data = derived[itemID]
	if err := tx.Commit(ctx); err != nil {
		return UserData{}, fmt.Errorf("commit favorite state: %w", err)
	}
	return data, nil
}

// SetPlayed updates a supported folder and its ordinary same-library descendants
// in one transaction, preserving attachment history. An explicitly selected
// active theme remains writable. Folder summaries use only ordinary leaves.
func (s *Store) SetPlayed(ctx context.Context, userID, itemID string, played bool, datePlayed *time.Time) (UserData, error) {
	return s.SetPlayedFor(ctx, Subject{UserID: userID}, itemID, played, datePlayed)
}

// SetPlayedFor retains same-library descendant updates for the explicit target.
func (s *Store) SetPlayedFor(ctx context.Context, subject Subject, itemID string, played bool, datePlayed *time.Time) (UserData, error) {
	if strings.TrimSpace(subject.UserID) == "" {
		return UserData{}, ErrInvalidInput
	}
	userID := subject.UserID
	if datePlayed != nil {
		utc := datePlayed.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return UserData{}, ErrInvalidInput
		}
		datePlayed = &utc
	}
	tx, access, err := s.beginSubjectStateWrite(ctx, subject, false)
	if err != nil {
		return UserData{}, err
	}
	defer rollback(tx)
	item, err := lockStateItem(ctx, tx, access, itemID, false)
	if err != nil {
		return UserData{}, err
	}
	itemIDs, err := lockPlayedTargets(ctx, tx, item)
	if err != nil {
		return UserData{}, err
	}
	if err := lockUserDataTargets(ctx, tx, userID, itemIDs); err != nil {
		return UserData{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE user_item_data data SET playback_position_ticks = 0,
		played = $3, play_count = CASE WHEN i.is_folder THEN 0 WHEN $3 THEN GREATEST(data.play_count, 1) ELSE 0 END,
		last_played_at = CASE WHEN i.is_folder OR NOT $3 THEN NULL ELSE COALESCE($4::timestamptz, data.last_played_at, now()) END,
		updated_at = clock_timestamp() FROM items i WHERE data.user_id = $1 AND data.item_id = ANY($2::text[]) AND i.id = data.item_id`,
		userID, itemIDs, played, datePlayed)
	if err != nil {
		return UserData{}, fmt.Errorf("set played state: %w", err)
	}
	data, err := scanUserData(tx.QueryRow(ctx, "SELECT "+userDataColumns+" FROM user_item_data WHERE user_id = $1 AND item_id = $2", userID, itemID))
	if err != nil {
		return UserData{}, err
	}
	derived := map[string]UserData{itemID: data}
	if err := deriveUserDataFolders(ctx, tx, userID, derived); err != nil {
		return UserData{}, err
	}
	data = derived[itemID]
	if err := tx.Commit(ctx); err != nil {
		return UserData{}, fmt.Errorf("commit played state: %w", err)
	}
	return data, nil
}

func lockPlayedTargets(ctx context.Context, tx pgx.Tx, root stateItem) ([]string, error) {
	if !root.isFolder {
		return []string{root.id}, nil
	}
	rows, err := tx.Query(ctx, `WITH RECURSIVE targets AS (
		SELECT i.id, i.library_id FROM items i WHERE i.id = $1 AND i.library_id = $2 AND `+ordinaryItemSQL("i")+`
		UNION
		SELECT child.id, child.library_id FROM items child JOIN targets parent
			ON child.parent_id = parent.id AND child.library_id = parent.library_id
		WHERE `+ordinaryItemSQL("child")+`
	) SELECT i.id FROM items i JOIN targets target ON target.id = i.id AND target.library_id = i.library_id
	WHERE i.type IN ('Movie','Series','Season','Episode','Video','Audio','MusicAlbum','MusicArtist') AND `+ordinaryItemSQL("i")+`
	ORDER BY i.id FOR SHARE OF i`, root.id, root.libraryID)
	if err != nil {
		return nil, fmt.Errorf("lock played folder descendants: %w", err)
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
	// The classification writer takes UPDATE locks on these same item rows.
	// Recheck in a fresh statement before any user-data row is created or changed;
	// a marker committed while the locking query waited must reject the batch.
	var ordinaryCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM items i
		WHERE i.id=ANY($1::text[]) AND i.library_id=$2 AND `+ordinaryItemSQL("i"), ids, root.libraryID).Scan(&ordinaryCount); err != nil {
		return nil, fmt.Errorf("recheck played folder item visibility: %w", err)
	}
	if ordinaryCount != len(ids) || ordinaryCount == 0 {
		return nil, ErrNotFound
	}
	return ids, nil
}

func lockUserDataTargets(ctx context.Context, tx pgx.Tx, userID string, ids []string) error {
	// All bulk writers acquire user-data rows in the same order. Single-item
	// playback writers never lock a second user-data row or a parent state row.
	if _, err := tx.Exec(ctx, `INSERT INTO user_item_data (user_id, item_id)
		SELECT $1, id FROM unnest($2::text[]) AS target(id) ORDER BY id
		ON CONFLICT (user_id, item_id) DO NOTHING`, userID, ids); err != nil {
		return fmt.Errorf("initialize played folder data: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT item_id FROM user_item_data
		WHERE user_id = $1 AND item_id = ANY($2::text[]) ORDER BY item_id FOR UPDATE`, userID, ids)
	if err != nil {
		return fmt.Errorf("lock played folder user data: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}
