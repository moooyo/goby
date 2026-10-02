package library

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ConfirmOwnedPlaybackTerminal is an internal negative retention witness for
// an already stored exact identity, not a reusable AUTH or media grant. It
// performs one ordinary Data statement and never writes/creates/expires a row.
// Callers may only collect actual ownerless stopped lifetimes after true.
func (s *Store) ConfirmOwnedPlaybackTerminal(ctx context.Context, owner PlaybackOwner, canonicalID, itemID, sourceID string) (bool, error) {
	if ctx == nil || !validPlaybackOwner(owner) || !validClientPlaybackReference(canonicalID) || itemID == "" || len(itemID) > 256 || len(sourceID) > 256 {
		return false, ErrInvalidInput
	}
	if sourceID == "" {
		return false, ErrInvalidInput
	}
	if _, err := sourceForItem(itemID, sourceID); err != nil {
		return false, err
	}
	if !s.Available() || s.pool == nil {
		return false, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var state string
	// Terminal states are monotonic under Report/Prepare. A locking SELECT
	// rechecks a concurrently updated tuple; an older active snapshot simply
	// yields no witness until a later cycle. No pre-wait deadline is evidence.
	err := s.pool.QueryRow(ctx, `SELECT state FROM play_sessions
		WHERE id=$1 AND user_id IS NOT DISTINCT FROM NULLIF($2,'') AND auth_session_id=$3 AND device_id=$4
		AND application_client_id IS NOT DISTINCT FROM NULLIF($5,'') AND (user_id IS NULL)=$6
		AND item_id=$7 AND media_source_id=$8 AND NOT is_dynamic AND state IN ('Stopped','Expired') FOR SHARE`,
		canonicalID, owner.UserID, owner.SessionID, owner.DeviceID, owner.ApplicationClientID, owner.ApplicationKey, itemID, sourceID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.Available() {
		return false, ErrUnavailable
	}
	return state == "Stopped" || state == "Expired", nil
}
