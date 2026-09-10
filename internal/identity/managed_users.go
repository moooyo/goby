package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrRevisionConflict  = errors.New("user revision conflict")
	ErrLastAdministrator = errors.New("the last enabled administrator cannot be disabled or demoted")
)

const (
	// Every managed mutation takes this lock before locking any account. It also
	// serializes the enabled-administrator count with competing role changes.
	managedUsersLockID int64 = 4919415424202458192
	maxManagedFolders        = 256
)

// ManagedPolicy contains supported configuration facts, independent of the
// account's current administrator role, disabled state, and server limits.
type ManagedPolicy struct {
	EnableAllFolders               bool
	EnabledFolders                 []string
	EnableMediaPlayback            bool
	EnablePlaybackRemuxing         bool
	EnableAudioPlaybackTranscoding bool
	EnableVideoPlaybackTranscoding bool
}

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
	tx, _, err := s.beginManagedUserMutation(ctx, actor, actor.User.ID)
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
		Action: activity.ActionUserCreated, Source: activity.SourceNative, Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: user.ID}, Revision: 1, Count: 1,
		ChangedFields: []activity.Field{activity.FieldName, activity.FieldIsAdministrator},
	}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit managed user creation: %w", err)
	}
	return user, nil
}

// UpdateManagedUser accepts only a Principal previously authenticated by
// Resolve as an admin session. It rechecks that trusted session and current
// database role inside the mutation transaction; caller-supplied role snapshots
// never authorize an update.
func (s *Store) UpdateManagedUser(ctx context.Context, actor Principal, id string, input ManagedUserUpdate) (ManagedUserMutation, error) {
	name, normalized, policy, err := validateManagedUserUpdate(input)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	tx, current, err := s.beginManagedUserMutation(ctx, actor, id)
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
		result.CurrentSessionRevoked, err = revokeManagedUserSessions(ctx, tx, actor, id, input.IsDisabled)
		if err != nil {
			return ManagedUserMutation{}, err
		}
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserUpdated, Source: activity.SourceNative, Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: id}, Revision: updated.Revision, Count: 1,
		ChangedFields: managedUserActivityFields(current, updated),
	}); err != nil {
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
	tx, current, err := s.beginManagedUserMutation(ctx, actor, id)
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
		policy = CASE WHEN jsonb_typeof(policy) = 'object' THEN policy
			|| jsonb_build_object('IsAdministrator', is_administrator, 'IsDisabled', is_disabled) ELSE policy END,
		updated_at = clock_timestamp() WHERE id = $1 RETURNING `+userColumns+", management_revision",
		id, string(hash), password != ""))
	if err != nil {
		return ManagedUserMutation{}, fmt.Errorf("reset managed user password: %w", err)
	}
	revoked, err := revokeManagedUserSessions(ctx, tx, actor, id, true)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return ManagedUserMutation{}, err
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserPasswordReset, Source: activity.SourceNative, Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: id}, Revision: updated.Revision, Count: 1,
	}); err != nil {
		return ManagedUserMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserMutation{}, fmt.Errorf("commit managed user password reset: %w", err)
	}
	return ManagedUserMutation{User: updated, CurrentSessionRevoked: revoked}, nil
}

func (s *Store) beginManagedUserMutation(ctx context.Context, actor Principal, id string) (pgx.Tx, ManagedUser, error) {
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
	err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 AND user_id = $2 FOR UPDATE`, actor.SessionID, actor.User.ID).Scan(&sessionID)
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
		WHERE id = $1 AND user_id = $2 AND kind = 'admin' AND revoked_at IS NULL
		AND expires_at > clock_timestamp())`, sessionID, actor.User.ID).Scan(&authorized); err != nil {
		return nil, ManagedUser{}, fmt.Errorf("revalidate managed user actor: %w", err)
	}
	if !authorized {
		return nil, ManagedUser{}, ErrUnauthorized
	}
	target, found := users[id]
	if !found {
		return nil, ManagedUser{}, ErrNotFound
	}
	ok = true
	return tx, target, nil
}

func validManagedActor(actor Principal) bool {
	return actor.Kind == "admin" && validRevalidationID(actor.User.ID) && validRevalidationID(actor.SessionID)
}

func revokeManagedUserSessions(ctx context.Context, tx pgx.Tx, actor Principal, id string, allKinds bool) (bool, error) {
	rows, err := tx.Query(ctx, `UPDATE sessions SET revoked_at = clock_timestamp()
		WHERE user_id = $1 AND revoked_at IS NULL AND ($2 OR kind = 'admin')
		RETURNING id`, id, allKinds)
	if err != nil {
		return false, fmt.Errorf("revoke managed user sessions: %w", err)
	}
	defer rows.Close()
	currentRevoked := false
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return false, fmt.Errorf("read revoked managed user session: %w", err)
		}
		currentRevoked = currentRevoked || (id == actor.User.ID && sessionID == actor.SessionID)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read managed user session revocations: %w", err)
	}
	return currentRevoked, nil
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
	policy := input.Policy
	policy.EnabledFolders, err = canonicalManagedFolders(policy.EnabledFolders)
	if err != nil {
		fields["Policy.EnabledFolders"] = managedInputMessage(err)
	}
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

func projectManagedPolicy(raw json.RawMessage) ManagedPolicy {
	policy := ManagedPolicy{EnabledFolders: []string{}}
	var values map[string]json.RawMessage
	if !utf8.Valid(raw) || json.Unmarshal(raw, &values) != nil || values == nil {
		return policy
	}
	readFlag := func(name string) (bool, bool) {
		value, found := values[name]
		if !found {
			return true, true
		}
		var enabled *bool
		if json.Unmarshal(value, &enabled) != nil || enabled == nil {
			return false, false
		}
		return *enabled, true
	}
	var validFoldersFlag bool
	policy.EnableAllFolders, validFoldersFlag = readFlag("EnableAllFolders")
	policy.EnableMediaPlayback, _ = readFlag("EnableMediaPlayback")
	policy.EnablePlaybackRemuxing, _ = readFlag("EnablePlaybackRemuxing")
	policy.EnableAudioPlaybackTranscoding, _ = readFlag("EnableAudioPlaybackTranscoding")
	policy.EnableVideoPlaybackTranscoding, _ = readFlag("EnableVideoPlaybackTranscoding")
	if value, found := values["EnabledFolders"]; validFoldersFlag && found {
		var folders []string
		if json.Unmarshal(value, &folders) == nil {
			if canonical, err := canonicalManagedFolders(folders); err == nil {
				policy.EnabledFolders = canonical
			}
		}
	}
	return policy
}

func managedUserFieldError(field, message string) error {
	return &ManagedUserValidationError{Fields: map[string]string{field: message}}
}

func managedInputMessage(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")
}
