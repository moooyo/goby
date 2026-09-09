package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxClientPlaybackReferenceBytes     = 256
	maxClientPlaybackReferencesPerAuth  = 65_536
	maxClientPlaybackReferencesPerUser  = 262_144
	clientPlaybackReferenceCleanupBatch = 256
)

// PrepareCorrelatedPlayback binds a client-generated reference to a distinct
// internal play within its authenticated credential/client/device scope. Reusing a
// reference may refresh only the same live item/source. Terminal or deleted
// targets cannot be rebound. Reserved play_ identifiers only resolve existing
// internal sessions; they never become new client references.
func (s *Store) PrepareCorrelatedPlayback(ctx context.Context, owner PlaybackOwner, itemID, mediaSourceID, reference string) (PlaySession, error) {
	return s.preparePlayback(ctx, owner, itemID, mediaSourceID, reference, true)
}

// ResolvePlaybackReference returns an owned canonical ID without creating or
// refreshing a play. Existing terminal plays remain resolvable for idempotent
// cleanup. Missing targets and tombstones return ErrNotFound. Current account,
// authentication, playback policy, and catalog access are checked every time.
func (s *Store) ResolvePlaybackReference(ctx context.Context, owner PlaybackOwner, reference string) (string, error) {
	if !validClientPlaybackReference(reference) {
		return "", ErrInvalidInput
	}
	tx, access, err := s.beginPlaybackWrite(ctx, owner)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	session, err := readOwnedPlaySession(ctx, tx, owner, reference, false)
	if err != nil {
		return "", err
	}
	if _, err := lockStateItem(ctx, tx, access, session.ItemID, true); err != nil {
		return "", err
	}
	// Resolve the canonical row again after the catalog lock. Deletion may make
	// it unavailable, but an immutable alias can never redirect this operation.
	session, err = readOwnedCanonicalPlaySession(ctx, tx, owner, session.ID, false)
	if err != nil {
		return "", err
	}
	if _, err := sourceForItem(session.ItemID, session.MediaSourceID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("complete playback reference resolution: %w", err)
	}
	return session.ID, nil
}

func validClientPlaybackReference(reference string) bool {
	return strings.TrimSpace(reference) != "" && len(reference) <= maxClientPlaybackReferenceBytes &&
		utf8.ValidString(reference) && strings.IndexFunc(reference, unicode.IsControl) < 0
}

// Internal IDs retain priority, including historical rows without the current
// purpose prefix. Nonce lookup never widens ownership or creates missing rows.
func readOwnedPlaySession(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, reference string, lock bool) (PlaySession, error) {
	if !validClientPlaybackReference(reference) {
		return PlaySession{}, ErrInvalidInput
	}
	session, err := readOwnedCanonicalPlaySession(ctx, tx, owner, reference, lock)
	if !errors.Is(err, ErrNotFound) || strings.HasPrefix(reference, "play_") {
		return session, err
	}
	id, exists, err := lookupClientPlaybackReference(ctx, tx, owner, reference)
	if err != nil {
		return PlaySession{}, err
	}
	if !exists || id == "" {
		return PlaySession{}, ErrNotFound
	}
	return readOwnedCanonicalPlaySession(ctx, tx, owner, id, lock)
}

func requireCorrelatedPlaybackAuthentication(ctx context.Context, tx pgx.Tx, owner PlaybackOwner) error {
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT (kind = 'emby' AND NOT $4::boolean AND device_id = $3)
		OR (kind = 'application_key' AND $4::boolean AND EXISTS (
			SELECT 1 FROM application_key_clients client WHERE client.id = $5 AND client.credential_id = sessions.id AND client.device_id = $3))
		FROM sessions WHERE id = $1 AND user_id IS NOT DISTINCT FROM NULLIF($2, '')`,
		owner.SessionID, owner.UserID, owner.DeviceID, owner.ApplicationKey, owner.ApplicationClientID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("authorize client playback reference: %w", err)
	}
	return nil
}

// An existing NULL target is a tombstone, not permission to create a new play.
// The surrounding transaction already holds current user/authentication locks.
func lookupClientPlaybackReference(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, reference string) (string, bool, error) {
	var id *string
	err := tx.QueryRow(ctx, `SELECT reference.play_session_id FROM client_playback_references reference
		JOIN sessions authentication ON authentication.id = reference.auth_session_id
			AND authentication.user_id IS NOT DISTINCT FROM reference.user_id
		WHERE reference.user_id IS NOT DISTINCT FROM NULLIF($1, '') AND reference.auth_session_id = $2 AND reference.device_id = $3
		AND reference.application_client_id IS NOT DISTINCT FROM NULLIF($6, '')
		AND reference.client_nonce = $4 AND ((authentication.kind = 'emby' AND NOT $5::boolean AND authentication.device_id = reference.device_id)
			OR (authentication.kind = 'application_key' AND $5::boolean AND EXISTS (
				SELECT 1 FROM application_key_clients client WHERE client.id = reference.application_client_id
				AND client.credential_id = authentication.id AND client.device_id = reference.device_id)))`,
		owner.UserID, owner.SessionID, owner.DeviceID, reference, owner.ApplicationKey, owner.ApplicationClientID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read client playback reference: %w", err)
	}
	if id == nil {
		return "", true, nil
	}
	return *id, true, nil
}

// Call only while holding lockPlaybackCapacity, followed by the usual item data
// lock. Admission serializes per user or per userless application credential.
func createCorrelatedPlayback(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, item stateItem, sourceID string, data UserData, reference string) (PlaySession, error) {
	var authCount, userCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE auth_session_id = $2),
		count(*) FILTER (WHERE user_id = NULLIF($1, '')) FROM client_playback_references
		WHERE user_id = NULLIF($1, '') OR auth_session_id = $2`, owner.UserID, owner.SessionID).Scan(&authCount, &userCount); err != nil {
		return PlaySession{}, fmt.Errorf("check client playback reference capacity: %w", err)
	}
	if authCount >= maxClientPlaybackReferencesPerAuth || userCount >= maxClientPlaybackReferencesPerUser {
		return PlaySession{}, ErrBusy
	}
	session, err := createPlaybackSession(ctx, tx, owner, item, sourceID, data, true)
	if err != nil {
		return PlaySession{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO client_playback_references
		(user_id, auth_session_id, device_id, client_nonce, play_session_id, application_client_id)
		VALUES (NULLIF($1, ''), $2, $3, $4, $5, NULLIF($6, ''))`, owner.UserID, owner.SessionID, owner.DeviceID, reference, session.ID, owner.ApplicationClientID); err != nil {
		return PlaySession{}, fmt.Errorf("bind client playback reference: %w", err)
	}
	return session, nil
}

func pruneInactiveClientPlaybackReferences(ctx context.Context, tx pgx.Tx, owner PlaybackOwner) error {
	// Disabling an account, stopping a play, or expiring a play does not permit
	// nonce reuse. Only revoked/expired authentication makes its bindings inert.
	_, err := tx.Exec(ctx, `WITH removable AS (
		SELECT reference.user_id, reference.auth_session_id, reference.device_id, reference.client_nonce, reference.application_client_id
		FROM client_playback_references reference JOIN sessions authentication ON authentication.id = reference.auth_session_id
		WHERE reference.user_id IS NOT DISTINCT FROM NULLIF($1, '')
		AND (NOT $3::boolean OR (reference.auth_session_id = $4 AND reference.application_client_id = $5))
		AND (authentication.revoked_at IS NOT NULL OR authentication.expires_at <= clock_timestamp()
			OR (authentication.kind = 'application_key' AND NOT EXISTS (
				SELECT 1 FROM application_keys application WHERE application.credential_id = authentication.id)))
		ORDER BY reference.auth_session_id, reference.device_id, reference.client_nonce
		LIMIT $2 FOR UPDATE OF reference SKIP LOCKED
	) DELETE FROM client_playback_references reference USING removable
	WHERE reference.user_id IS NOT DISTINCT FROM removable.user_id AND reference.auth_session_id = removable.auth_session_id
	AND reference.application_client_id IS NOT DISTINCT FROM removable.application_client_id
	AND reference.device_id = removable.device_id AND reference.client_nonce = removable.client_nonce`,
		owner.UserID, clientPlaybackReferenceCleanupBatch, owner.ApplicationKey, owner.SessionID, owner.ApplicationClientID)
	if err != nil {
		return fmt.Errorf("prune inactive client playback references: %w", err)
	}
	return nil
}
