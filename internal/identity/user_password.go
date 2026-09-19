package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

// ChangeUserPassword changes only the authenticated owner's password. The old
// password, current account and current credential must all remain valid, and
// every existing login is revoked atomically with the replacement and audit.
// Administrator resets of other accounts use ResetManagedUserPassword instead.
func (s *Store) ChangeUserPassword(ctx context.Context, actor Principal, userID, currentPassword, newPassword string) (ManagedUserMutation, error) {
	if !validManagedActor(actor) || actor.Kind != "emby" || userID != actor.User.ID {
		return ManagedUserMutation{}, ErrUnauthorized
	}
	if err := validatePassword(newPassword, false); err != nil {
		return ManagedUserMutation{}, managedUserFieldError("NewPw", managedInputMessage(err))
	}
	if err := validatePassword(currentPassword, false); err != nil {
		return ManagedUserMutation{}, ErrInvalidCredentials
	}
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1 AND NOT is_disabled", userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserMutation{}, ErrUnauthorized
	}
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("read password change account: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)) != nil {
		return ManagedUserMutation{}, ErrInvalidCredentials
	}
	replacement, err := bcrypt.GenerateFromPassword([]byte(newPassword), passwordCost)
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("hash replacement password: %w", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("begin password change: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("lock password change: %w", err)
	}
	current, err := scanManagedUser(tx.QueryRow(ctx, "SELECT "+userColumns+", management_revision FROM users WHERE id = $1 AND password_hash = $2 AND NOT is_disabled FOR UPDATE", userID, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserMutation{}, ErrInvalidCredentials
	}
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("lock password change account: %w", err)
	}
	if err := validatePassword(newPassword, current.User.IsAdministrator); err != nil {
		return ManagedUserMutation{}, managedUserFieldError("NewPw", managedInputMessage(err))
	}
	var sessionID string
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 AND user_id = $2 AND kind = 'emby' FOR UPDATE`, actor.SessionID, userID).Scan(&sessionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagedUserMutation{}, ErrUnauthorized
		}
		return ManagedUserMutation{}, fmt.Errorf("lock password change credential: %w", err)
	}
	var authorized bool
	var deviceID string
	var observedAt, expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at > clock_timestamp(),
		device_id, clock_timestamp(), expires_at FROM sessions WHERE id = $1`, sessionID).
		Scan(&authorized, &deviceID, &observedAt, &expiresAt); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("authorize password change: %w", err)
	}
	policy, policyErr := ParseRuntimePolicy(current.User.Policy)
	if !authorized || policyErr != nil || !loginPolicyAllows(current.User.Policy, deviceID, observedAt) ||
		(!policy.EnableRemoteAccess && !IsLocalPeer(actor.PeerIP)) {
		return ManagedUserMutation{}, ErrUnauthorized
	}
	updated, err := scanManagedUser(tx.QueryRow(ctx, `UPDATE users SET password_hash = $2,
		has_password = $3, management_revision = management_revision + 1, updated_at = clock_timestamp(),
		local_credentials_revision=local_credentials_revision+1,
		configuration_revision=configuration_revision+CASE WHEN local_password_hash IS NOT NULL OR profile_pin_ciphertext IS NOT NULL
			OR configuration @> '{"EnableLocalPassword":true}'::jsonb THEN 1 ELSE 0 END,
		configuration=CASE WHEN local_password_hash IS NOT NULL OR profile_pin_ciphertext IS NOT NULL
			OR configuration @> '{"EnableLocalPassword":true}'::jsonb
			THEN (configuration-'ProfilePin') || '{"EnableLocalPassword":false}'::jsonb ELSE configuration END,
		local_password_hash=NULL,profile_pin_ciphertext=NULL,local_password_failures=0,local_password_blocked_until=NULL
		WHERE id = $1 RETURNING `+userColumns+", management_revision", userID, string(replacement), newPassword != ""))
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("change user password: %w", err)
	}
	revoked, revokedSessionIDs, err := revokeManagedUserSessions(ctx, tx, actor, userID, true)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserPasswordReset, Source: activity.SourceEmby, Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: userID}, Revision: updated.Revision, Count: 1,
	}); err != nil {
		return ManagedUserMutation{}, err
	}
	// This exact mutation revoked the locked credential; only time can now
	// invalidate its pre-mutation authority while audit persistence is waiting.
	if err := tx.QueryRow(ctx, "SELECT $1::timestamptz > clock_timestamp(), clock_timestamp()", expiresAt).Scan(&authorized, &observedAt); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("recheck password change expiry: %w", err)
	}
	if !authorized || !loginPolicyAllows(current.User.Policy, deviceID, observedAt) {
		return ManagedUserMutation{}, ErrUnauthorized
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("commit password change: %w", err)
	}
	return ManagedUserMutation{User: updated, CurrentSessionRevoked: revoked, RevokedSessionIDs: revokedSessionIDs}, nil
}

// UserHasUsedDevice reports a historical successful ordinary login for the
// public-user picker. Revocation does not erase the device's prior use.
func (s *Store) UserHasUsedDevice(ctx context.Context, userID, deviceID string) (bool, error) {
	if !validRevalidationID(userID) || !validRevalidationID(deviceID) {
		return false, nil
	}
	var used bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions
		WHERE user_id = $1 AND device_id = $2 AND kind = 'emby')`, userID, deviceID).Scan(&used); err != nil {
		return false, fmt.Errorf("read user device history: %w", err)
	}
	return used, nil
}
