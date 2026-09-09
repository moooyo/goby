package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// RevalidateSession refreshes a principal that Resolve previously authenticated
// as an Emby session. Callers must pass that trusted authentication context,
// never identifiers supplied by an unauthenticated client. This operation is
// not an alternative login mechanism and does not require retaining the token.
//
// The session-owner pair is fixed; account state, role, policy, client metadata,
// and timestamps come entirely from the database. Demotion removes elevated
// permissions while retaining an otherwise valid Emby login. Revalidation does
// not record activity or extend expiration; callers may use TouchClientSession
// separately when the authenticated connection demonstrates activity.
func (s *Store) RevalidateSession(ctx context.Context, previouslyAuthenticated Principal) (Principal, error) {
	if previouslyAuthenticated.Kind != "emby" ||
		!validRevalidationID(previouslyAuthenticated.SessionID) ||
		!validRevalidationID(previouslyAuthenticated.User.ID) {
		return Principal{}, ErrUnauthorized
	}
	var principal Principal
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.is_administrator, u.is_disabled,
		u.has_password, u.created_at, u.policy, authentication.id,
		authentication.client_name, authentication.device_id, authentication.device_name,
		authentication.client_version, authentication.kind, authentication.expires_at,
		authentication.last_seen_at
		FROM sessions authentication JOIN users u ON u.id = authentication.user_id
		WHERE authentication.id = $1 AND authentication.user_id = $2
		AND authentication.kind = 'emby' AND authentication.revoked_at IS NULL
		AND authentication.expires_at > clock_timestamp() AND NOT u.is_disabled`,
		previouslyAuthenticated.SessionID, previouslyAuthenticated.User.ID).
		Scan(&principal.User.ID, &principal.User.Name, &principal.User.IsAdministrator,
			&principal.User.IsDisabled, &principal.User.HasPassword, &principal.User.CreatedAt,
			&principal.User.Policy, &principal.SessionID, &principal.Client.Name,
			&principal.Client.DeviceID, &principal.Client.Device, &principal.Client.Version,
			&principal.Kind, &principal.ExpiresAt, &principal.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("revalidate authenticated Emby session: %w", err)
	}
	return principal, nil
}

func validRevalidationID(value string) bool {
	return value != "" && len(value) <= maxClientFieldBytes && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
