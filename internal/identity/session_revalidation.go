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
// as an Emby login or application key. Callers must pass that trusted context,
// never identifiers supplied by an unauthenticated client. This operation is
// not an alternative login mechanism and does not require retaining the token.
//
// The session-owner pair is fixed; account state, role, policy, configuration,
// client metadata, and timestamps come entirely from the database. Demotion
// removes elevated permissions while retaining an otherwise valid Emby login.
// Revalidation does not record activity or extend expiration; callers may use
// TouchClientSession separately when the authenticated connection demonstrates activity.
func (s *Store) RevalidateSession(ctx context.Context, previouslyAuthenticated Principal) (Principal, error) {
	if previouslyAuthenticated.IsApplicationKey() {
		if !validRevalidationID(previouslyAuthenticated.SessionID) || !validRevalidationID(previouslyAuthenticated.ClientSessionID) {
			return Principal{}, ErrUnauthorized
		}
		return scanApplicationKeyPrincipal(s.pool.QueryRow(ctx, `SELECT k.id, a.id, c.id,
			c.client_name, c.device_id, c.device_name, c.client_version, c.last_seen_at
			FROM sessions a JOIN application_keys k ON k.credential_id = a.id
			JOIN application_key_clients c ON c.credential_id = a.id AND c.id = $3
			WHERE a.id = $1 AND k.id = $2 AND a.kind = 'application_key'
			AND a.user_id IS NULL AND a.expires_at IS NULL AND a.revoked_at IS NULL`,
			previouslyAuthenticated.SessionID, previouslyAuthenticated.ApplicationKeyID, previouslyAuthenticated.ClientSessionID))
	}
	if previouslyAuthenticated.Kind != "emby" || previouslyAuthenticated.ApplicationKeyID != 0 || previouslyAuthenticated.ClientSessionID != "" ||
		!validRevalidationID(previouslyAuthenticated.SessionID) ||
		!validRevalidationID(previouslyAuthenticated.User.ID) {
		return Principal{}, ErrUnauthorized
	}
	var principal Principal
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.is_administrator, u.is_disabled,
		u.has_password, u.created_at, u.policy, u.configuration, authentication.id,
		authentication.client_name, authentication.device_id, COALESCE(d.custom_name, authentication.device_name),
		authentication.client_version, authentication.kind, authentication.expires_at,
		authentication.last_seen_at
		FROM sessions authentication JOIN users u ON u.id = authentication.user_id
		LEFT JOIN devices d ON d.id = authentication.device_registry_id AND d.deleted_at IS NULL
		WHERE authentication.id = $1 AND authentication.user_id = $2
		AND authentication.kind = 'emby' AND authentication.revoked_at IS NULL
		AND authentication.expires_at > clock_timestamp() AND NOT u.is_disabled`,
		previouslyAuthenticated.SessionID, previouslyAuthenticated.User.ID).
		Scan(&principal.User.ID, &principal.User.Name, &principal.User.IsAdministrator,
			&principal.User.IsDisabled, &principal.User.HasPassword, &principal.User.CreatedAt,
			&principal.User.Policy, &principal.User.Configuration, &principal.SessionID, &principal.Client.Name,
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
