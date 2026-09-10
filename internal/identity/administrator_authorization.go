package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AuthorizationTx is the query-only view of the caller's active transaction.
// Callers must not supply a pool or an independently opened transaction. A
// restricted owner transaction can implement this interface through an adapter
// without exposing its connection, transaction controls, or cancellation policy.
type AuthorizationTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// AdministratorAudience identifies the authenticated API boundary. Its zero
// value is invalid, and scheduled system work has no administrator audience.
type AdministratorAudience uint8

const (
	AdministratorNative AdministratorAudience = iota + 1
	AdministratorEmby
)

// CheckAdministrator checks the current credential and account in the caller's
// transaction. Native operations accept only administrator-cookie credentials;
// Emby operations accept administrator logins or complete application principals.
// Role, expiry, and client display metadata in the principal are not authority.
//
// With lock=true, authority is checked before and after acquiring shared actor
// locks. Call this before any business-record or task advisory lock. Ordinary
// actors lock their account before their credential; application actors lock
// their parent credential, key sidecar, and client context in that order. This
// helper never acquires management, registration, device, or catalog locks.
//
// Mutations must retain these locks, call again with lock=false after waiting
// for business rows, and make another lock=false check their final database
// operation before commit. The fresh checks use clock_timestamp(), so a login
// expiring during a wait or write is rejected. There is no self-revocation or
// system-actor exception, and no activity timestamp or credential is changed.
func CheckAdministrator(ctx context.Context, tx AuthorizationTx, actor Principal, audience AdministratorAudience, lock bool) error {
	if audience != AdministratorNative && audience != AdministratorEmby {
		return ErrUnauthorized
	}
	native := audience == AdministratorNative
	if !validDeviceActor(actor, native) || tx == nil {
		return ErrUnauthorized
	}
	if actor.IsApplicationKey() && (!validRevalidationID(actor.SessionID) || !validRevalidationID(actor.ClientSessionID)) {
		return ErrUnauthorized
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return err
	}
	if !lock {
		return nil
	}
	lockRow := func(statement string, args ...any) error {
		var id string
		if err := tx.QueryRow(ctx, statement, args...).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnauthorized
			}
			return fmt.Errorf("lock administrator authority: %w", err)
		}
		return nil
	}
	if actor.IsApplicationKey() {
		if err := lockRow(`SELECT id FROM sessions WHERE id = $1
			AND kind = 'application_key' AND user_id IS NULL FOR SHARE`, actor.SessionID); err != nil {
			return err
		}
		if err := lockRow(`SELECT credential_id FROM application_keys
			WHERE credential_id = $1 AND id = $2 FOR SHARE`, actor.SessionID, actor.ApplicationKeyID); err != nil {
			return err
		}
		if err := lockRow(`SELECT id FROM application_key_clients
			WHERE id = $1 AND credential_id = $2 FOR SHARE`, actor.ClientSessionID, actor.SessionID); err != nil {
			return err
		}
	} else {
		if err := lockRow("SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID); err != nil {
			return err
		}
		if err := lockRow(`SELECT id FROM sessions WHERE id = $1 AND user_id = $2 AND kind = $3 FOR SHARE`,
			actor.SessionID, actor.User.ID, actor.Kind); err != nil {
			return err
		}
	}
	return authorizeDeviceActor(ctx, tx, actor, native, nil)
}
