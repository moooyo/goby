package identity

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

// LocalCredentials contains presence flags only. Profile PINs are encrypted
// client profile gates, not account passwords or server login aliases.
type LocalCredentials struct {
	UserID              string `json:"UserId"`
	Revision            int64  `json:"Revision,string"`
	HasLocalPassword    bool
	HasProfilePin       bool
	EnableLocalPassword bool
}

// LocalCredentialsUpdate retains absent, clear, and replace semantics. Native
// callers must supply the exact revision returned by GetLocalCredentials.
type LocalCredentialsUpdate struct {
	Revision            int64
	EnableLocalPassword bool
	LocalPassword       *string
	ProfilePin          *string
}

type LocalCredentialsMutation struct {
	Credentials           LocalCredentials
	CurrentSessionRevoked bool
	RevokedSessionIDs     []string `json:"-"`
}

func validateLocalSecret(value string, pin bool) error {
	if value == "" {
		return nil
	}
	if pin {
		if len(value) != 4 {
			return managedUserFieldError("ProfilePin", "Use exactly 4 ASCII digits, or an empty string to clear the PIN.")
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return managedUserFieldError("ProfilePin", "Use exactly 4 ASCII digits, or an empty string to clear the PIN.")
			}
		}
		return nil
	}
	if len(value) > 72 || !utf8.ValidString(value) {
		return managedUserFieldError("LocalPassword", "Use at most 72 UTF-8 bytes, or an empty string to clear the local password.")
	}
	return nil
}

func hashLocalPassword(value *string) (*string, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	if err := validateLocalSecret(*value, false); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*value), passwordCost)
	if err != nil {
		return nil, fmt.Errorf("hash local credential: %w", err)
	}
	encoded := string(hash)
	return &encoded, nil
}

func readLocalCredentials(ctx context.Context, tx pgx.Tx, userID string) (LocalCredentials, error) {
	result := LocalCredentials{UserID: userID}
	err := tx.QueryRow(ctx, `SELECT local_credentials_revision, local_password_hash IS NOT NULL,
		profile_pin_ciphertext IS NOT NULL, configuration @> '{"EnableLocalPassword":true}'::jsonb
		FROM users WHERE id=$1`, userID).Scan(&result.Revision, &result.HasLocalPassword,
		&result.HasProfilePin, &result.EnableLocalPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return LocalCredentials{}, ErrNotFound
	}
	if err != nil {
		return LocalCredentials{}, fmt.Errorf("read local credential status: %w", err)
	}
	return result, nil
}

func (s *Store) GetLocalCredentials(ctx context.Context, actor Principal, userID string) (LocalCredentials, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LocalCredentials{}, err
	}
	defer rollback(tx)
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, false); err != nil {
		return LocalCredentials{}, err
	}
	result, err := readLocalCredentials(ctx, tx, userID)
	if err != nil {
		return LocalCredentials{}, err
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return LocalCredentials{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LocalCredentials{}, err
	}
	return result, nil
}

// UpdateLocalCredentials shares account-before-credential lock ordering with
// account management. Local-password changes revoke all previous target
// sessions atomically; profile PIN changes preserve the client login.
func (s *Store) UpdateLocalCredentials(ctx context.Context, actor Principal, userID string, input LocalCredentialsUpdate) (LocalCredentialsMutation, error) {
	if input.Revision < 1 {
		return LocalCredentialsMutation{}, managedUserFieldError("Revision", "Supply a positive credential revision.")
	}
	if !validManagedActor(actor) || !validRevalidationID(userID) {
		return LocalCredentialsMutation{}, ErrUnauthorized
	}
	passwordHash, err := hashLocalPassword(input.LocalPassword)
	if err != nil {
		return LocalCredentialsMutation{}, err
	}
	if input.ProfilePin != nil {
		if err := validateLocalSecret(*input.ProfilePin, true); err != nil {
			return LocalCredentialsMutation{}, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LocalCredentialsMutation{}, err
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return LocalCredentialsMutation{}, err
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, true); err != nil {
		return LocalCredentialsMutation{}, err
	}
	current, err := readLocalCredentials(ctx, tx, userID)
	if err != nil {
		return LocalCredentialsMutation{}, err
	}
	if current.Revision != input.Revision {
		return LocalCredentialsMutation{}, ErrRevisionConflict
	}
	var pinCiphertext []byte
	if input.ProfilePin != nil {
		pinCiphertext, err = s.prepareProfilePin(ctx, tx, userID, *input.ProfilePin)
		if err != nil {
			return LocalCredentialsMutation{}, err
		}
	}
	hasPassword := current.HasLocalPassword
	if input.LocalPassword != nil {
		hasPassword = passwordHash != nil
	}
	if input.EnableLocalPassword && !hasPassword {
		return LocalCredentialsMutation{}, managedUserFieldError("EnableLocalPassword", "Configure a local password before enabling local password sign-in.")
	}
	if input.LocalPassword == nil && input.ProfilePin == nil && current.EnableLocalPassword == input.EnableLocalPassword {
		if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
			return LocalCredentialsMutation{}, err
		}
		return LocalCredentialsMutation{Credentials: current}, nil
	}
	var policyBefore []byte
	if err := tx.QueryRow(ctx, "SELECT policy FROM users WHERE id=$1", actor.User.ID).Scan(&policyBefore); err != nil {
		return LocalCredentialsMutation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET
		local_password_hash=CASE WHEN $2 THEN $3 ELSE local_password_hash END,
		profile_pin_ciphertext=CASE WHEN $4 THEN $5 ELSE profile_pin_ciphertext END,
		local_credentials_revision=local_credentials_revision+1,
		local_password_failures=0,local_password_blocked_until=NULL,
		configuration=(configuration-'ProfilePin') || jsonb_build_object('EnableLocalPassword',$6::boolean),
		configuration_revision=configuration_revision+CASE WHEN $6 <> $7 OR $4 THEN 1 ELSE 0 END,
		updated_at=clock_timestamp() WHERE id=$1`, userID, input.LocalPassword != nil, passwordHash,
		input.ProfilePin != nil, pinCiphertext, input.EnableLocalPassword, current.EnableLocalPassword); err != nil {
		return LocalCredentialsMutation{}, fmt.Errorf("update local credentials: %w", err)
	}
	updated, err := readLocalCredentials(ctx, tx, userID)
	if err != nil {
		return LocalCredentialsMutation{}, err
	}
	localChanged := input.LocalPassword != nil || current.EnableLocalPassword != input.EnableLocalPassword
	var revoked bool
	var sessionIDs []string
	if localChanged {
		revoked, sessionIDs, err = revokeManagedUserSessions(ctx, tx, actor, userID, true)
		if err != nil {
			return LocalCredentialsMutation{}, err
		}
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return LocalCredentialsMutation{}, err
	}
	action := activity.ActionUserUpdated
	if localChanged {
		action = activity.ActionUserPasswordReset
	}
	if err := activity.Record(ctx, tx, activity.Event{Action: action,
		Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: userID}, Revision: updated.Revision, Count: 1}); err != nil {
		return LocalCredentialsMutation{}, err
	}
	if localChanged {
		if err := recheckManagedMutation(ctx, tx, actor, userID == actor.User.ID, policyBefore); err != nil {
			return LocalCredentialsMutation{}, err
		}
	} else if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return LocalCredentialsMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LocalCredentialsMutation{}, fmt.Errorf("commit local credentials: %w", err)
	}
	return LocalCredentialsMutation{Credentials: updated, CurrentSessionRevoked: revoked, RevokedSessionIDs: sessionIDs}, nil
}

// authenticateLocalPassword is reachable only after primary password rejection.
// A local password cannot authenticate the native administrator dashboard.
func (s *Store) authenticateLocalPassword(ctx context.Context, userID, password, peerIP string) (string, error) {
	if password == "" || !IsLocalPeer(peerIP) {
		return "", ErrInvalidCredentials
	}
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT local_password_hash FROM users WHERE id=$1 AND NOT is_disabled
		AND local_password_hash IS NOT NULL AND configuration @> '{"EnableLocalPassword":true}'::jsonb
		AND (local_password_blocked_until IS NULL OR local_password_blocked_until<=clock_timestamp())`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("read local authentication credential: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		if err := s.recordLocalPasswordFailure(ctx, userID, hash); err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	return hash, nil
}

func (s *Store) recordLocalPasswordFailure(ctx context.Context, userID, hash string) error {
	// The account update serializes concurrent failures. Expired windows start
	// again at one; attempts during a live block neither extend nor reset it.
	_, err := s.pool.Exec(ctx, `UPDATE users SET
		local_password_failures=CASE WHEN local_password_blocked_until<=clock_timestamp() THEN 1 ELSE LEAST(local_password_failures+1,5) END,
		local_password_blocked_until=CASE WHEN local_password_blocked_until<=clock_timestamp() THEN NULL
			WHEN local_password_failures>=4 THEN clock_timestamp()+interval '5 minutes' ELSE NULL END
		WHERE id=$1 AND local_password_hash=$2 AND NOT is_disabled
		AND configuration @> '{"EnableLocalPassword":true}'::jsonb
		AND (local_password_blocked_until IS NULL OR local_password_blocked_until<=clock_timestamp())`, userID, hash)
	if err != nil {
		return fmt.Errorf("record local authentication attempt: %w", err)
	}
	return nil
}
