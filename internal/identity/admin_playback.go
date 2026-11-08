package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RevalidateManagedPlaybackController refreshes a principal originally resolved
// from an administrator cookie. The owner and credential kind remain fixed.
// This does not authenticate a token, grant an Emby identity, or record activity.
func (s *Store) RevalidateManagedPlaybackController(ctx context.Context, previous Principal) (Principal, error) {
	if !validDeviceActor(previous, true) {
		return Principal{}, ErrUnauthorized
	}
	var current Principal
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.is_administrator, u.is_disabled,
		u.policy, u.configuration, u.has_password, u.created_at, a.id, a.kind,
		a.client_name, a.device_id, a.device_name, a.client_version,
		a.expires_at, a.last_seen_at
		FROM sessions a JOIN users u ON u.id = a.user_id
		WHERE a.id = $1 AND a.user_id = $2 AND a.kind = 'admin'
		AND a.revoked_at IS NULL AND a.expires_at > clock_timestamp()
		AND u.is_administrator AND NOT u.is_disabled`, previous.SessionID, previous.User.ID).
		Scan(&current.User.ID, &current.User.Name, &current.User.IsAdministrator, &current.User.IsDisabled,
			&current.User.Policy, &current.User.Configuration, &current.User.HasPassword, &current.User.CreatedAt,
			&current.SessionID, &current.Kind, &current.Client.Name, &current.Client.DeviceID,
			&current.Client.Device, &current.Client.Version, &current.ExpiresAt, &current.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("revalidate administrator playback controller: %w", err)
	}
	current.PeerIP = previous.PeerIP
	return current, nil
}
