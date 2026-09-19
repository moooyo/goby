package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrRevisionConflict  = errors.New("user revision conflict")
	ErrLastAdministrator = errors.New("the last enabled administrator cannot be disabled, demoted, or deleted")
)

const (
	// Every managed mutation takes this lock before locking any account. It also
	// serializes the enabled-administrator count with competing role changes.
	managedUsersLockID int64 = 4919415424202458192
	maxManagedFolders        = 256
)

// ManagedUser adds an optimistic revision and an editable policy projection.
// User.Policy remains internal and must not be exposed by native API adapters.
type ManagedUser struct {
	User     User
	Revision int64
	Policy   ManagedPolicy
}

// ManagedUserUpdate is a complete replacement of the supported account fields.
type ManagedUserUpdate struct {
	Revision        int64
	Name            string
	IsAdministrator bool
	IsDisabled      bool
	Policy          ManagedPolicy
}

// ManagedUserMutation reports whether this mutation revoked its own caller.
type ManagedUserMutation struct {
	User                  ManagedUser
	CurrentSessionRevoked bool
	RevokedSessionIDs     []string `json:"-"`
}

// ManagedUserDeletion contains only committed deletion facts. Session IDs are
// internal cleanup handles, never a native response or credential value.
type ManagedUserDeletion struct {
	CurrentSessionRevoked bool
	RevokedSessionIDs     []string
	CollectionsChanged    bool `json:"-"`
}

// ManagedUserValidationError carries safe field messages for native forms.
type ManagedUserValidationError struct {
	Fields map[string]string
}

func (e *ManagedUserValidationError) Error() string {
	return "invalid managed user input"
}

func (e *ManagedUserValidationError) Unwrap() error {
	return ErrInvalidInput
}

// GetManagedUser reads editable configuration without applying effective-role
// overrides. Missing supported flags retain the existing permissive defaults;
// malformed stored values are projected conservatively.
func (s *Store) GetManagedUser(ctx context.Context, id string) (ManagedUser, error) {
	user, err := scanManagedUser(s.pool.QueryRow(ctx, "SELECT "+userColumns+", management_revision FROM users WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUser{}, ErrNotFound
	}
	if err != nil {
		return ManagedUser{}, fmt.Errorf("get managed user: %w", err)
	}
	return user, nil
}

// CreateManagedUser creates an account only while its trusted administrator
// session remains authorized inside the same transaction as the insertion.
func (s *Store) CreateManagedUser(ctx context.Context, actor Principal, name, password string, isAdministrator bool) (User, error) {
	name, normalized, err := normalizeName(name)
	if err != nil {
		return User{}, managedUserFieldError("Name", managedInputMessage(err))
	}
	if err := validatePassword(password, isAdministrator); err != nil {
		return User{}, managedUserFieldError("Password", managedInputMessage(err))
	}
	if !validManagedActor(actor) {
		return User{}, ErrUnauthorized
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return User{}, fmt.Errorf("hash managed user password: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return User{}, err
	}
	tx, _, err := s.beginManagedUserMutation(ctx, actor, actor.User.ID, false)
	if err != nil {
		return User{}, err
	}
	defer rollback(tx)
	user, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, has_password, is_administrator)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+userColumns,
		id, name, normalized, string(hash), password != "", isAdministrator))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_normalized_name_key" {
		return User{}, managedUserFieldError("Name", "username is already in use")
	}
	if err != nil {
		return User{}, fmt.Errorf("create managed user: %w", err)
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return User{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserCreated, Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: user.ID}, Revision: 1, Count: 1,
		ChangedFields: []activity.Field{activity.FieldName, activity.FieldIsAdministrator},
	}); err != nil {
		return User{}, err
	}
	if err := recheckManagedMutation(ctx, tx, actor, false, nil); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit managed user creation: %w", err)
	}
	return user, nil
}

// UpdateManagedUser accepts only a Principal previously authenticated by
// Resolve as a native or Emby administrator login. It rechecks that session and current
// database role inside the mutation transaction; caller-supplied role snapshots
// never authorize an update.
func (s *Store) UpdateManagedUser(ctx context.Context, actor Principal, id string, input ManagedUserUpdate) (ManagedUserMutation, error) {
	name, normalized, policy, err := validateManagedUserUpdate(input)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	tx, current, err := s.beginManagedUserMutation(ctx, actor, id, false)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	defer rollback(tx)
	if current.Revision != input.Revision {
		return ManagedUserMutation{}, ErrRevisionConflict
	}
	if input.IsAdministrator && !current.User.HasPassword {
		return ManagedUserMutation{}, managedUserFieldError("IsAdministrator", "administrator password is required before promotion")
	}
	if current.User.IsAdministrator && !current.User.IsDisabled && (!input.IsAdministrator || input.IsDisabled) {
		var administrators int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_administrator AND NOT is_disabled").Scan(&administrators); err != nil {
			return ManagedUserMutation{}, fmt.Errorf("count enabled administrators: %w", err)
		}
		if administrators <= 1 {
			return ManagedUserMutation{}, ErrLastAdministrator
		}
	}
	if err := validateManagedLibraries(ctx, tx, policy.EnabledFolders); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := validateManagedLibraries(ctx, tx, policy.EnableContentDeletionFromFolders); err != nil {
		var validation *ManagedUserValidationError
		if errors.As(err, &validation) {
			return ManagedUserMutation{}, managedUserFieldError("Policy.EnableContentDeletionFromFolders", "every deletion folder must identify an existing library")
		}
		return ManagedUserMutation{}, err
	}
	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("encode managed user policy: %w", err)
	}
	updated, err := scanManagedUser(tx.QueryRow(ctx, `UPDATE users SET name = $2,
		normalized_name = $3, is_administrator = $4, is_disabled = $5,
		policy = (CASE WHEN jsonb_typeof(policy) = 'object' THEN policy ELSE '{}'::jsonb END)
			|| $6::jsonb || jsonb_build_object('IsAdministrator', $4::boolean, 'IsDisabled', $5::boolean),
		management_revision = management_revision + 1, updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+userColumns+", management_revision",
		id, name, normalized, input.IsAdministrator, input.IsDisabled, encodedPolicy))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_normalized_name_key" {
		return ManagedUserMutation{}, managedUserFieldError("Name", "username is already in use")
	}
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("update managed user: %w", err)
	}
	result := ManagedUserMutation{User: updated}
	if input.IsDisabled || (current.User.IsAdministrator && !input.IsAdministrator) {
		result.CurrentSessionRevoked, result.RevokedSessionIDs, err = revokeManagedUserSessions(ctx, tx, actor, id, input.IsDisabled)
		if err != nil {
			return ManagedUserMutation{}, err
		}
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserUpdated, Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: id}, Revision: updated.Revision, Count: 1,
		ChangedFields: managedUserActivityFields(current, updated),
	}); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := recheckManagedMutation(ctx, tx, actor, id == actor.User.ID, current.User.Policy); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("commit managed user update: %w", err)
	}
	return result, nil
}

// ResetManagedUserPassword replaces the password and revokes every old target
// session in the same transaction. Password validation follows CreateUser:
// administrators require a password, while ordinary accounts may use an empty
// one. API adapters may impose a stricter nonempty-password requirement.
func (s *Store) ResetManagedUserPassword(ctx context.Context, actor Principal, id string, revision int64, password string) (ManagedUserMutation, error) {
	if revision < 1 {
		return ManagedUserMutation{}, managedUserFieldError("Revision", "revision must be a positive integer")
	}
	if err := validatePassword(password, false); err != nil {
		return ManagedUserMutation{}, managedUserFieldError("Password", managedInputMessage(err))
	}
	if !validManagedActor(actor) {
		return ManagedUserMutation{}, ErrUnauthorized
	}
	// Expensive hashing never holds account, session, or advisory locks.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("hash managed user password: %w", err)
	}
	tx, current, err := s.beginManagedUserMutation(ctx, actor, id, false)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	defer rollback(tx)
	if current.Revision != revision {
		return ManagedUserMutation{}, ErrRevisionConflict
	}
	if err := validatePassword(password, current.User.IsAdministrator); err != nil {
		return ManagedUserMutation{}, managedUserFieldError("Password", managedInputMessage(err))
	}
	updated, err := scanManagedUser(tx.QueryRow(ctx, `UPDATE users SET password_hash = $2,
		has_password = $3, management_revision = management_revision + 1,
		local_credentials_revision=local_credentials_revision+1,
		configuration_revision=configuration_revision+CASE WHEN local_password_hash IS NOT NULL OR profile_pin_ciphertext IS NOT NULL
			OR configuration @> '{"EnableLocalPassword":true}'::jsonb THEN 1 ELSE 0 END,
		configuration=CASE WHEN local_password_hash IS NOT NULL OR profile_pin_ciphertext IS NOT NULL
			OR configuration @> '{"EnableLocalPassword":true}'::jsonb
			THEN (configuration-'ProfilePin') || '{"EnableLocalPassword":false}'::jsonb ELSE configuration END,
		local_password_hash=NULL,profile_pin_ciphertext=NULL,local_password_failures=0,local_password_blocked_until=NULL,
		policy = CASE WHEN jsonb_typeof(policy) = 'object' THEN policy
			|| jsonb_build_object('IsAdministrator', is_administrator, 'IsDisabled', is_disabled) ELSE policy END,
		updated_at = clock_timestamp() WHERE id = $1 RETURNING `+userColumns+", management_revision",
		id, string(hash), password != ""))
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("reset managed user password: %w", err)
	}
	revoked, revokedSessionIDs, err := revokeManagedUserSessions(ctx, tx, actor, id, true)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserPasswordReset, Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: id}, Revision: updated.Revision, Count: 1,
	}); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := recheckManagedMutation(ctx, tx, actor, id == actor.User.ID, current.User.Policy); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("commit managed user password reset: %w", err)
	}
	return ManagedUserMutation{User: updated, CurrentSessionRevoked: revoked, RevokedSessionIDs: revokedSessionIDs}, nil
}

// DeleteManagedUser removes an account and its FK-owned state atomically with
// an audit event. Account management serialization prevents competing requests
// from deleting or demoting the last enabled administrator.
func (s *Store) DeleteManagedUser(ctx context.Context, actor Principal, id string, revision int64) (ManagedUserDeletion, error) {
	if revision < 1 {
		return ManagedUserDeletion{}, managedUserFieldError("Revision", "revision must be a positive integer")
	}
	tx, current, err := s.beginManagedUserMutation(ctx, actor, id, true)
	if err != nil {
		return ManagedUserDeletion{}, err
	}
	defer rollback(tx)
	if err := CheckAdministrator(ctx, tx, actor, managedAdministratorAudience(actor), false); err != nil {
		return ManagedUserDeletion{}, err
	}
	if current.Revision != revision {
		return ManagedUserDeletion{}, ErrRevisionConflict
	}
	if current.User.IsAdministrator && !current.User.IsDisabled {
		var administrators int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_administrator AND NOT is_disabled").Scan(&administrators); err != nil {
			return ManagedUserDeletion{}, fmt.Errorf("count administrators before user deletion: %w", err)
		}
		if administrators <= 1 {
			return ManagedUserDeletion{}, ErrLastAdministrator
		}
	}
	// This value comes from the credential already locked and reauthorized in
	// this transaction. A Principal expiry snapshot cannot authorize self-deletion.
	var actorExpiresAt time.Time
	var actorDeviceID string
	if err := tx.QueryRow(ctx, "SELECT expires_at, device_id FROM sessions WHERE id = $1 AND user_id = $2", actor.SessionID, actor.User.ID).Scan(&actorExpiresAt, &actorDeviceID); err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("read locked deletion authority expiry: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT id FROM sessions WHERE user_id = $1 ORDER BY id", id)
	if err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("read deleted user session handles: %w", err)
	}
	result := ManagedUserDeletion{CurrentSessionRevoked: id == actor.User.ID, RevokedSessionIDs: []string{}}
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return ManagedUserDeletion{}, fmt.Errorf("read deleted user session handle: %w", err)
		}
		result.RevokedSessionIDs = append(result.RevokedSessionIDs, sessionID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("read deleted user sessions: %w", err)
	}
	// Account locks also fence new collection ownership/share references. Keep
	// this pre-cascade fact in the committed result: ordinary account deletion
	// must not force unrelated catalog subscribers to disconnect and resync.
	if err := tx.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM media_collections WHERE owner_id = $1)
		OR EXISTS (SELECT 1 FROM media_collection_shares WHERE user_id = $1)`, id).Scan(&result.CollectionsChanged); err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("read deleted user collection effects: %w", err)
	}
	deleted, err := tx.Exec(ctx, "DELETE FROM users WHERE id = $1 AND management_revision = $2", id, revision)
	if err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("delete managed user: %w", err)
	}
	if deleted.RowsAffected() != 1 {
		return ManagedUserDeletion{}, ErrRevisionConflict
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserDeletion{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserDeleted, Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: id}, Revision: revision, Count: 1,
	}); err != nil {
		return ManagedUserDeletion{}, err
	}
	if result.CurrentSessionRevoked {
		// The locked account and session were removed by this exact DELETE. Their
		// role/revocation cannot change concurrently, but the database clock can.
		var unexpired bool
		var observedAt time.Time
		if err := tx.QueryRow(ctx, "SELECT $1::timestamptz > clock_timestamp(), clock_timestamp()", actorExpiresAt).Scan(&unexpired, &observedAt); err != nil {
			return ManagedUserDeletion{}, fmt.Errorf("recheck self-deletion authority expiry: %w", err)
		}
		if !unexpired || (actor.Kind == "emby" && !loginPolicyAllows(current.User.Policy, actorDeviceID, observedAt)) {
			return ManagedUserDeletion{}, ErrUnauthorized
		}
	} else if err := CheckAdministrator(ctx, tx, actor, managedAdministratorAudience(actor), false); err != nil {
		return ManagedUserDeletion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDeletion{}, fmt.Errorf("commit managed user deletion: %w", err)
	}
	return result, nil
}

func (s *Store) beginManagedUserMutation(ctx context.Context, actor Principal, id string, lockTargetSessions bool) (pgx.Tx, ManagedUser, error) {
	if !validManagedActor(actor) {
		return nil, ManagedUser{}, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, ManagedUser{}, fmt.Errorf("begin managed user mutation: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			rollback(tx)
		}
	}()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return nil, ManagedUser{}, fmt.Errorf("lock managed user mutations: %w", err)
	}
	// FOR UPDATE conflicts with Authenticate's FOR SHARE while preserving a
	// deterministic account order for actor/target pairs in either direction.
	rows, err := tx.Query(ctx, "SELECT "+userColumns+", management_revision FROM users WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE", []string{actor.User.ID, id})
	if err != nil {
		return nil, ManagedUser{}, fmt.Errorf("lock managed user accounts: %w", err)
	}
	users := make(map[string]ManagedUser, 2)
	for rows.Next() {
		user, err := scanManagedUser(rows)
		if err != nil {
			rows.Close()
			return nil, ManagedUser{}, fmt.Errorf("read locked managed user: %w", err)
		}
		users[user.User.ID] = user
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, ManagedUser{}, fmt.Errorf("read managed user accounts: %w", err)
	}
	account, found := users[actor.User.ID]
	if !found || account.User.IsDisabled || !account.User.IsAdministrator {
		return nil, ManagedUser{}, ErrUnauthorized
	}
	var sessionID string
	if lockTargetSessions {
		// Deletion locks all affected credentials in one deterministic order.
		// Account locks prevent new target logins while this set is consumed.
		sessions, err := tx.Query(ctx, `SELECT id FROM sessions
			WHERE (id = $1 AND user_id = $2) OR user_id = $3 ORDER BY id FOR UPDATE`, actor.SessionID, actor.User.ID, id)
		if err != nil {
			return nil, ManagedUser{}, fmt.Errorf("lock deleted user sessions: %w", err)
		}
		for sessions.Next() {
			var lockedID string
			if err := sessions.Scan(&lockedID); err != nil {
				sessions.Close()
				return nil, ManagedUser{}, fmt.Errorf("read locked deletion session: %w", err)
			}
			if lockedID == actor.SessionID {
				sessionID = lockedID
			}
		}
		sessions.Close()
		if err := sessions.Err(); err != nil {
			return nil, ManagedUser{}, fmt.Errorf("read locked deletion sessions: %w", err)
		}
		if sessionID == "" {
			return nil, ManagedUser{}, ErrUnauthorized
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 AND user_id = $2 FOR UPDATE`, actor.SessionID, actor.User.ID).Scan(&sessionID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ManagedUser{}, ErrUnauthorized
	}
	if err != nil {
		return nil, ManagedUser{}, fmt.Errorf("lock managed user actor session: %w", err)
	}
	// Check the clock after acquiring all authentication locks: a queued session
	// may expire or be revoked while waiting for its account/session row.
	var authorized bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions
		WHERE id = $1 AND user_id = $2 AND kind = $3 AND revoked_at IS NULL
		AND expires_at > clock_timestamp())`, sessionID, actor.User.ID, actor.Kind).Scan(&authorized); err != nil {
		return nil, ManagedUser{}, fmt.Errorf("revalidate managed user actor: %w", err)
	}
	if !authorized {
		return nil, ManagedUser{}, ErrUnauthorized
	}
	if actor.Kind == "emby" {
		var deviceID string
		var observedAt time.Time
		if err := tx.QueryRow(ctx, "SELECT device_id, clock_timestamp() FROM sessions WHERE id = $1", sessionID).Scan(&deviceID, &observedAt); err != nil {
			return nil, ManagedUser{}, fmt.Errorf("read managed user actor policy context: %w", err)
		}
		policy, err := ParseRuntimePolicy(account.User.Policy)
		if err != nil || !loginPolicyAllows(account.User.Policy, deviceID, observedAt) ||
			(!policy.EnableRemoteAccess && !IsLocalPeer(actor.PeerIP)) {
			return nil, ManagedUser{}, ErrUnauthorized
		}
	}
	target, found := users[id]
	if !found {
		return nil, ManagedUser{}, ErrNotFound
	}
	ok = true
	return tx, target, nil
}

func validManagedActor(actor Principal) bool {
	return (actor.Kind == "admin" || actor.Kind == "emby") && actor.ApplicationKeyID == 0 && actor.ClientSessionID == "" &&
		validRevalidationID(actor.User.ID) && validRevalidationID(actor.SessionID)
}

func managedAdministratorAudience(actor Principal) AdministratorAudience {
	if actor.Kind == "emby" {
		return AdministratorEmby
	}
	return AdministratorNative
}

func recheckManagedMutation(ctx context.Context, tx pgx.Tx, actor Principal, selfMutation bool, policyBefore json.RawMessage) error {
	if !selfMutation {
		return CheckAdministrator(ctx, tx, actor, managedAdministratorAudience(actor), false)
	}
	// This transaction already locked and authorized the actor, and it alone
	// can have changed its own role, policy or revocation while those locks are
	// held. Expiration still advances and is checked after audit persistence.
	var unexpired bool
	var deviceID string
	var observedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT expires_at > clock_timestamp(), device_id, clock_timestamp() FROM sessions
		WHERE id = $1 AND user_id = $2 AND kind = $3`, actor.SessionID, actor.User.ID, actor.Kind).Scan(&unexpired, &deviceID, &observedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("recheck managed user actor expiry: %w", err)
	}
	if !unexpired || (actor.Kind == "emby" && !loginPolicyAllows(policyBefore, deviceID, observedAt)) {
		return ErrUnauthorized
	}
	return nil
}

func revokeManagedUserSessions(ctx context.Context, tx pgx.Tx, actor Principal, id string, allKinds bool) (bool, []string, error) {
	rows, err := tx.Query(ctx, `UPDATE sessions SET revoked_at = clock_timestamp()
		WHERE user_id = $1 AND revoked_at IS NULL AND ($2 OR kind = 'admin')
		RETURNING id`, id, allKinds)
	if err != nil {
		return false, nil, fmt.Errorf("revoke managed user sessions: %w", err)
	}
	defer rows.Close()
	currentRevoked := false
	revokedSessionIDs := make([]string, 0)
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return false, nil, fmt.Errorf("read revoked managed user session: %w", err)
		}
		currentRevoked = currentRevoked || (id == actor.User.ID && sessionID == actor.SessionID)
		revokedSessionIDs = append(revokedSessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		return false, nil, fmt.Errorf("read managed user session revocations: %w", err)
	}
	slices.Sort(revokedSessionIDs)
	return currentRevoked, revokedSessionIDs, nil
}

func validateManagedLibraries(ctx context.Context, tx pgx.Tx, folders []string) error {
	if len(folders) == 0 {
		return nil
	}
	// Keep selected libraries alive until the user policy commits.
	rows, err := tx.Query(ctx, "SELECT id FROM libraries WHERE id = ANY($1::text[]) ORDER BY id FOR KEY SHARE", folders)
	if err != nil {
		return fmt.Errorf("read managed user libraries: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("read managed user library: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read managed user library list: %w", err)
	}
	if count != len(folders) {
		return managedUserFieldError("Policy.EnabledFolders", "every enabled folder must identify an existing library")
	}
	return nil
}

func validateManagedUserUpdate(input ManagedUserUpdate) (string, string, ManagedPolicy, error) {
	fields := make(map[string]string)
	if input.Revision < 1 {
		fields["Revision"] = "revision must be a positive integer"
	}
	name, normalized, err := normalizeName(input.Name)
	if err != nil {
		fields["Name"] = managedInputMessage(err)
	}
	policy := canonicalManagedPolicy(input.Policy, fields)
	if len(fields) != 0 {
		return "", "", ManagedPolicy{}, &ManagedUserValidationError{Fields: fields}
	}
	return name, normalized, policy, nil
}

func canonicalManagedFolders(input []string) ([]string, error) {
	if len(input) > maxManagedFolders {
		return nil, fmt.Errorf("%w: enabled folders must contain at most 256 library identifiers", ErrInvalidInput)
	}
	folders := make([]string, 0, len(input))
	for _, id := range input {
		if !validRevalidationID(id) {
			return nil, fmt.Errorf("%w: library identifiers must contain 1 to 256 UTF-8 bytes without surrounding whitespace or control characters", ErrInvalidInput)
		}
		folders = append(folders, id)
	}
	slices.Sort(folders)
	return slices.Compact(folders), nil
}

func scanManagedUser(row rowScanner) (ManagedUser, error) {
	var revision int64
	user, err := scanUser(row, &revision)
	if err != nil {
		return ManagedUser{}, err
	}
	return ManagedUser{User: user, Revision: revision, Policy: projectManagedPolicy(user.Policy)}, nil
}

func managedUserFieldError(field, message string) error {
	return &ManagedUserValidationError{Fields: map[string]string{field: message}}
}

func managedInputMessage(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")
}
