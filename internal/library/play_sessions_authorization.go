package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// Accounts are already locked by beginSubjectStateWrite before this credential
// check. The persisted session kind preserves native administrator playback
// while the database device must still match the authenticated playback owner.
func checkPlaybackStateWrite(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, lock bool) error {
	if !validPlaybackOwner(owner) {
		return ErrInvalidInput
	}
	subject := Subject{UserID: owner.UserID}
	if owner.ApplicationKey {
		subject.ApplicationCredentialID = owner.SessionID
		statement := `SELECT authentication.id FROM sessions authentication
			JOIN application_keys application ON application.credential_id = authentication.id
			JOIN application_key_clients client ON client.credential_id = authentication.id
			WHERE authentication.id = $1 AND authentication.user_id IS NULL AND client.id = $2 AND client.device_id = $3
			AND authentication.kind = 'application_key' AND authentication.revoked_at IS NULL`
		if lock {
			statement += " FOR SHARE OF authentication, application, client"
		}
		var sessionID string
		err := tx.QueryRow(ctx, statement, owner.SessionID, owner.ApplicationClientID, owner.DeviceID).Scan(&sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		} else if err != nil {
			return fmt.Errorf("authorize application playback client: %w", err)
		}
	} else {
		statement := `SELECT kind FROM sessions WHERE id = $1 AND user_id = $2 AND device_id = $3 AND kind IN ('emby', 'admin')`
		if lock {
			statement += " FOR SHARE"
		}
		var kind string
		err := tx.QueryRow(ctx, statement, owner.SessionID, owner.UserID, owner.DeviceID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		} else if err != nil {
			return fmt.Errorf("authorize playback authentication session: %w", err)
		}
		subject.Actor = &identity.Principal{User: identity.User{ID: owner.UserID}, SessionID: owner.SessionID,
			Kind: kind, PeerIP: owner.PeerIP}
	}
	_, err := checkSubjectStateWrite(ctx, tx, subject, true, false)
	return err
}

// Every successful playback transaction, including idempotent reports and
// reference resolution, finishes with credentials and time policy revalidation.
func commitPlaybackWrite(ctx context.Context, tx pgx.Tx, owner PlaybackOwner) error {
	if err := checkPlaybackStateWrite(ctx, tx, owner, false); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
