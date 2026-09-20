package library

import (
	"context"
	"fmt"
	"github.com/moooyo/goby/internal/notificationjournal"
	"strings"
	"time"
)

// UpdateUserDataFor changes one authorized state row atomically. Folder playback
// values are derived; bulk watched mutations retain their existing dedicated
// SetPlayedFor transaction instead of pretending a parent row updates children.
func (s *Store) UpdateUserDataFor(ctx context.Context, subject Subject, itemID string, patch UserDataPatch) (UserData, error) {
	if strings.TrimSpace(subject.UserID) == "" || patch.Validate() != nil {
		return UserData{}, ErrInvalidInput
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
	if item.isFolder && (patch.PlaybackPositionTicks != nil || patch.PlayCount != nil || patch.Played != nil || patch.LastPlayedDate != nil || patch.ClearLastPlayedDate || patch.HideFromResume != nil && *patch.HideFromResume) {
		return UserData{}, fmt.Errorf("%w: folder playback state is derived; use the dedicated played operation", ErrInvalidInput)
	}
	data, err := lockUserData(ctx, tx, subject.UserID, itemID)
	if err != nil {
		return UserData{}, err
	}
	if patch.PlaybackPositionTicks != nil {
		if item.duration <= 0 && *patch.PlaybackPositionTicks != 0 || item.duration > 0 && *patch.PlaybackPositionTicks > item.duration {
			return UserData{}, ErrInvalidInput
		}
		data.PlaybackPositionTicks = *patch.PlaybackPositionTicks
	}
	if patch.PlayCount != nil {
		data.PlayCount = *patch.PlayCount
	}
	if patch.IsFavorite != nil {
		data.IsFavorite = *patch.IsFavorite
	}
	if patch.Played != nil {
		data.Played = *patch.Played
		if data.Played {
			data.PlaybackPositionTicks = 0
		}
	}
	if patch.ClearLastPlayedDate {
		data.LastPlayedDate = nil
	}
	if patch.LastPlayedDate != nil {
		value := patch.LastPlayedDate.UTC()
		data.LastPlayedDate = &value
	}
	if patch.ClearRating {
		data.Rating = nil
	}
	if patch.Rating != nil {
		value := *patch.Rating
		data.Rating = &value
	}
	if patch.ClearLikes {
		data.Likes = nil
	}
	if patch.Likes != nil {
		value := *patch.Likes
		data.Likes = &value
	}
	if patch.HideFromResume != nil {
		data.HideFromResume = *patch.HideFromResume
	}
	// A generic numeric rating does not preserve an obsolete thumbs-up/down.
	if (patch.Rating != nil || patch.ClearRating) && patch.Likes == nil {
		data.Likes = nil
	}
	if data.Played && data.PlayCount == 0 && patch.PlayCount == nil {
		data.PlayCount = 1
	}
	if data.Played && data.LastPlayedDate == nil && patch.LastPlayedDate == nil && !patch.ClearLastPlayedDate {
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return UserData{}, err
		}
		data.LastPlayedDate = &now
	}
	data, err = scanUserData(tx.QueryRow(ctx, `UPDATE user_item_data SET playback_position_ticks=$3,play_count=$4,is_favorite=$5,played=$6,
		last_played_at=$7,rating=$8,likes=$9,hide_from_resume=$10,updated_at=clock_timestamp()
		WHERE user_id=$1 AND item_id=$2 RETURNING `+userDataColumns, subject.UserID, itemID, data.PlaybackPositionTicks,
		data.PlayCount, data.IsFavorite, data.Played, data.LastPlayedDate, data.Rating, data.Likes, data.HideFromResume))
	if err != nil {
		return UserData{}, fmt.Errorf("persist user data update: %w", err)
	}
	derived := map[string]UserData{itemID: data}
	if err := deriveUserDataFolders(ctx, tx, subject.UserID, derived, access); err != nil {
		return UserData{}, err
	}
	if err := notificationjournal.RecordUserData(ctx, tx, subject.UserID, notificationjournal.Reference{Kind: "Item", ID: itemID}, false); err != nil {
		return UserData{}, err
	}
	if _, err := checkSubjectStateWrite(ctx, tx, subject, false, false); err != nil {
		return UserData{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserData{}, err
	}
	return derived[itemID], nil
}
