package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// checkSubjectStateWrite keeps state ownership separate from the credential
// that requested the write. Accounts are locked together in ID order before
// credentials. The final check runs after business locks and aggregate queries
// so database time, expiry, and access schedules cannot age during a lock wait.
func checkSubjectStateWrite(ctx context.Context, tx pgx.Tx, subject Subject, requirePlayback, lock bool) (libraryAccess, error) {
	if ctx == nil || tx == nil || !validSubject(subject) {
		return libraryAccess{}, ErrInvalidInput
	}
	if subject.ApplicationCredentialID != "" {
		if subject.Actor != nil {
			return libraryAccess{}, ErrForbidden
		}
		if subject.UserID != "" {
			statement := "SELECT id FROM users WHERE id = $1"
			if lock {
				statement += " FOR SHARE"
			}
			var targetID string
			if err := tx.QueryRow(ctx, statement, subject.UserID).Scan(&targetID); errors.Is(err, pgx.ErrNoRows) {
				return libraryAccess{}, ErrNotFound
			} else if err != nil {
				return libraryAccess{}, fmt.Errorf("authorize application state target: %w", err)
			}
		}
		if err := checkSubjectApplicationKey(ctx, tx, subject.ApplicationCredentialID, lock); err != nil {
			return libraryAccess{}, err
		}
		access := unrestrictedLibraryAccess()
		access.userID = subject.UserID
		return access, nil
	}
	actor := subject.Actor
	if actor != nil && ((actor.Kind != "emby" && actor.Kind != "admin") || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
		!validCatalogLibraryIdentifier(actor.User.ID) || !validCatalogLibraryIdentifier(actor.SessionID)) {
		return libraryAccess{}, ErrForbidden
	}
	accounts := []string{subject.UserID}
	if actor != nil {
		accounts = append(accounts, actor.User.ID)
	}
	if lock {
		rows, err := tx.Query(ctx, "SELECT id FROM users WHERE id = ANY($1::text[]) ORDER BY id FOR SHARE", accounts)
		if err != nil {
			return libraryAccess{}, fmt.Errorf("lock user state accounts: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return libraryAccess{}, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return libraryAccess{}, err
		}
	}
	// Use a fresh statement after all account locks. READ COMMITTED may have
	// waited for a role or policy change while acquiring an earlier row lock.
	target, err := readStateAccount(ctx, tx, subject.UserID)
	if err != nil {
		return libraryAccess{}, err
	}
	access, err := parseLibraryPolicy(target.policy)
	if err != nil {
		return libraryAccess{}, err
	}
	if requirePlayback && !access.canPlay {
		return libraryAccess{}, ErrForbidden
	}
	access.userID, access.administrator = subject.UserID, target.administrator
	if target.administrator {
		access.all = true
	}
	if actor == nil {
		return access, nil
	}
	account := target
	if actor.User.ID != subject.UserID {
		account, err = readStateAccount(ctx, tx, actor.User.ID)
		if err != nil {
			return libraryAccess{}, err
		}
	}
	if (actor.User.ID != subject.UserID || actor.Kind == "admin") && !account.administrator {
		return libraryAccess{}, ErrForbidden
	}
	var actorPolicy identity.ManagedPolicy
	if actor.Kind == "emby" {
		actorPolicy, err = identity.ParseRuntimePolicy(account.policy)
		if err != nil {
			return libraryAccess{}, ErrForbidden
		}
		var loginState struct{ LockedOutDate *int64 }
		if json.Unmarshal(account.policy, &loginState) != nil || loginState.LockedOutDate != nil && *loginState.LockedOutDate != 0 {
			return libraryAccess{}, ErrForbidden
		}
	}
	if lock {
		var sessionID string
		err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 AND user_id = $2 AND kind = $3 FOR SHARE`,
			actor.SessionID, actor.User.ID, actor.Kind).Scan(&sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return libraryAccess{}, ErrForbidden
		} else if err != nil {
			return libraryAccess{}, fmt.Errorf("lock user state actor session: %w", err)
		}
	}
	var live bool
	var deviceID string
	var observedAt time.Time
	err = tx.QueryRow(ctx, `SELECT COALESCE(revoked_at IS NULL AND expires_at > clock_timestamp(), false),
		device_id, clock_timestamp() FROM sessions WHERE id = $1 AND user_id = $2 AND kind = $3`,
		actor.SessionID, actor.User.ID, actor.Kind).Scan(&live, &deviceID, &observedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !live {
		return libraryAccess{}, ErrForbidden
	} else if err != nil {
		return libraryAccess{}, fmt.Errorf("revalidate user state actor session: %w", err)
	}
	if actor.Kind == "emby" && (!actorPolicy.AllowsDevice(deviceID) || !actorPolicy.AllowsAccessAt(observedAt) ||
		!actorPolicy.EnableRemoteAccess && !identity.IsLocalPeer(actor.PeerIP)) {
		return libraryAccess{}, ErrForbidden
	}
	return access, nil
}

type stateAccount struct {
	administrator bool
	policy        []byte
}

func readStateAccount(ctx context.Context, tx pgx.Tx, userID string) (stateAccount, error) {
	var account stateAccount
	var disabled bool
	err := tx.QueryRow(ctx, `SELECT is_administrator, is_disabled,
		CASE WHEN octet_length(policy::text) <= 131072 THEN policy END FROM users WHERE id = $1`, userID).
		Scan(&account.administrator, &disabled, &account.policy)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && disabled {
		return stateAccount{}, ErrForbidden
	} else if err != nil {
		return stateAccount{}, fmt.Errorf("authorize user state account: %w", err)
	}
	return account, nil
}
