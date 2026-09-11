// Package identity stores users and revocable authentication sessions in PostgreSQL.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrAlreadyInitialized = errors.New("server is already initialized")
	ErrInvalidInput       = errors.New("invalid input")
	ErrNotFound           = errors.New("user not found")
)

const (
	bootstrapLockID     int64 = 4919415424202458191
	adminLifetime             = 24 * time.Hour
	embyLifetime              = 30 * 24 * time.Hour
	passwordCost              = bcrypt.DefaultCost
	maxClientFieldBytes       = 256
	userColumns               = "id, name, is_administrator, is_disabled, has_password, created_at, policy, configuration"
	// The fixed cost matches stored hashes so unknown users still perform bcrypt.
	fakePasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
)

// User carries account metadata and internal-only persisted policy and
// configuration snapshots. Password hashes never leave Store, and the snapshots
// are excluded from JSON encoding.
type User struct {
	ID              string
	Name            string
	IsAdministrator bool
	IsDisabled      bool
	HasPassword     bool
	CreatedAt       time.Time
	Policy          json.RawMessage `json:"-"`
	Configuration   json.RawMessage `json:"-"`
}

// Client describes the application and device that requested a session.
type Client struct {
	Name     string
	DeviceID string
	Device   string
	Version  string
}

// Credentials contains a newly issued token, which cannot be recovered later.
type Credentials struct {
	User      User
	Client    Client
	Token     string
	SessionID string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Principal describes an authenticated credential and, for a login, its account.
// SessionID is the actual shared credential ID, including for application keys;
// it does not imply that the credential is a user login.
type Principal struct {
	User      User
	SessionID string
	// ClientSessionID identifies a userless application's client context. It is
	// empty for logins, whose existing SessionID is also their client session ID.
	ClientSessionID  string
	ApplicationKeyID int64
	Client           Client
	Kind             string
	ExpiresAt        time.Time
	LastSeenAt       time.Time
}

// Store provides database-backed identity operations.
type Store struct {
	pool                *pgxpool.Pool
	applicationKeyVault *ApplicationKeyVault
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// NewWithApplicationKeyVault enables recoverable application-key management.
// Ordinary authentication does not depend on the vault being available.
func NewWithApplicationKeyVault(pool *pgxpool.Pool, vault *ApplicationKeyVault) *Store {
	return &Store{pool: pool, applicationKeyVault: vault}
}

// IsApplicationKey distinguishes userless application credentials from logins.
func (p Principal) IsApplicationKey() bool {
	return p.Kind == ApplicationKeyKind && p.ApplicationKeyID > 0 && p.SessionID != "" && p.ClientSessionID != "" &&
		p.User.ID == "" && p.User.Name == "" && !p.User.IsAdministrator && !p.User.IsDisabled &&
		!p.User.HasPassword && p.User.CreatedAt.IsZero() && len(p.User.Policy) == 0 && len(p.User.Configuration) == 0
}

// CanManageServer describes the authenticated snapshot. Mutations must still
// revalidate this authority in their own database transaction.
func (p Principal) CanManageServer() bool {
	return p.IsApplicationKey() || ((p.Kind == "admin" || p.Kind == "emby") &&
		p.ApplicationKeyID == 0 && p.ClientSessionID == "" && p.SessionID != "" && p.User.ID != "" && p.User.IsAdministrator && !p.User.IsDisabled)
}

// Initialized remains true after setup even if account data is later removed.
func (s *Store) Initialized(ctx context.Context) (bool, error) {
	var initialized bool
	err := s.pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM server_settings WHERE key = 'setup_completed' AND value = 'true')
		OR EXISTS (SELECT 1 FROM users)`).Scan(&initialized)
	if err != nil {
		return false, fmt.Errorf("read initialization state: %w", err)
	}
	return initialized, nil
}

// Bootstrap atomically creates the first administrator and seals initial setup.
func (s *Store) Bootstrap(ctx context.Context, name, password string) (User, error) {
	name, normalized, err := normalizeName(name)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password, true); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return User{}, fmt.Errorf("hash administrator password: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return User{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, fmt.Errorf("begin initialization: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLockID); err != nil {
		return User{}, fmt.Errorf("lock initialization: %w", err)
	}
	var initialized bool
	if err := tx.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM server_settings WHERE key = 'setup_completed' AND value = 'true')
		OR EXISTS (SELECT 1 FROM users)`).Scan(&initialized); err != nil {
		return User{}, fmt.Errorf("read initialization state: %w", err)
	}
	if initialized {
		return User{}, ErrAlreadyInitialized
	}
	user, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, has_password, is_administrator)
		VALUES ($1, $2, $3, $4, true, true)
		RETURNING `+userColumns, id, name, normalized, string(hash)))
	if err != nil {
		return User{}, fmt.Errorf("create initial administrator: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO server_settings (key, value)
		VALUES ('setup_completed', 'true')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`); err != nil {
		return User{}, fmt.Errorf("complete initialization: %w", err)
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserCreated, Source: activity.SourceNative,
		Actor:    activity.Actor{Kind: activity.ActorSystem},
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: user.ID},
		Revision: 1, Count: 1,
		ChangedFields: []activity.Field{activity.FieldName, activity.FieldIsAdministrator},
	}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit initialization: %w", err)
	}
	return user, nil
}

// Authenticate verifies credentials and issues a session scoped to kind.
func (s *Store) Authenticate(ctx context.Context, name, password string, client Client, kind string) (Credentials, error) {
	return s.AuthenticateWithPeer(ctx, name, password, client, kind, "")
}

// AuthenticateWithPeer records the trusted network peer for an ordinary Emby
// device. Native dashboard logins never register devices, regardless of their
// reported identifier. The original Authenticate API retains its behavior.
func (s *Store) AuthenticateWithPeer(ctx context.Context, name, password string, client Client, kind, peerIP string) (Credentials, error) {
	lifetime, err := sessionLifetime(kind)
	if err != nil {
		return Credentials{}, err
	}
	if err := validateClient(client); err != nil {
		return Credentials{}, err
	}
	if err := validateDevicePeer(peerIP); err != nil {
		return Credentials{}, err
	}
	if len(password) > 72 || !utf8.ValidString(password) {
		return Credentials{}, fmt.Errorf("%w: password must contain at most 72 UTF-8 bytes", ErrInvalidInput)
	}
	_, normalized, nameErr := normalizeName(name)
	if nameErr != nil {
		_ = bcrypt.CompareHashAndPassword([]byte(fakePasswordHash), []byte(password))
		return Credentials{}, ErrInvalidCredentials
	}
	var user User
	var hash string
	user, err = scanUser(s.pool.QueryRow(ctx, "SELECT "+userColumns+", password_hash FROM users WHERE normalized_name = $1", normalized), &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword([]byte(fakePasswordHash), []byte(password))
		return Credentials{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("read authentication account: %w", err)
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if passwordErr != nil || user.IsDisabled || (kind == "admin" && !user.IsAdministrator) {
		return Credentials{}, ErrInvalidCredentials
	}
	token, digest, err := randomToken()
	if err != nil {
		return Credentials{}, err
	}
	sessionID, err := randomID()
	if err != nil {
		return Credentials{}, err
	}
	return s.issueLogin(ctx, loginIssue{userID: user.ID, passwordHash: hash, token: token, digest: digest[:],
		sessionID: sessionID, client: client, kind: kind, lifetime: lifetime, peerIP: peerIP})
}

// Resolve accepts only the requested session kind and checks current user state.
func (s *Store) Resolve(ctx context.Context, token, kind string) (Principal, error) {
	if _, err := sessionLifetime(kind); err != nil {
		return Principal{}, ErrUnauthorized
	}
	digest, ok := tokenDigest(token)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	var principal Principal
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.is_administrator, u.is_disabled,
		u.has_password, u.created_at, u.policy, u.configuration, s.id, s.client_name, s.device_id, COALESCE(d.custom_name, s.device_name),
		s.client_version, s.kind, s.expires_at, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		LEFT JOIN devices d ON d.id = s.device_registry_id AND d.deleted_at IS NULL
		WHERE s.token_hash = $1 AND s.kind = $2 AND s.revoked_at IS NULL
		AND s.expires_at > now() AND NOT u.is_disabled
		AND ($2 <> 'admin' OR u.is_administrator)`, digest[:], kind).
		Scan(&principal.User.ID, &principal.User.Name, &principal.User.IsAdministrator,
			&principal.User.IsDisabled, &principal.User.HasPassword, &principal.User.CreatedAt, &principal.User.Policy,
			&principal.User.Configuration,
			&principal.SessionID, &principal.Client.Name, &principal.Client.DeviceID,
			&principal.Client.Device, &principal.Client.Version, &principal.Kind, &principal.ExpiresAt, &principal.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("resolve authentication session: %w", err)
	}
	return principal, nil
}

// Revoke invalidates a token. Unknown and previously revoked tokens are
// intentionally idempotent. The existing token-only API also permits retiring
// an expired credential; it does not authorize any other administrative action.
func (s *Store) Revoke(ctx context.Context, token string) error {
	digest, ok := tokenDigest(token)
	if !ok {
		return nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin authentication revocation: %w", err)
	}
	defer rollback(tx)
	var sessionID, kind string
	var userID *string
	err = tx.QueryRow(ctx, `UPDATE sessions SET revoked_at = clock_timestamp()
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id, user_id, kind`, digest[:]).
		Scan(&sessionID, &userID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke authentication session: %w", err)
	}
	event := activity.Event{Action: activity.ActionSessionRevoked, Source: identityActivitySource(kind),
		Resource: activity.Resource{Kind: activity.ResourceSession, ID: sessionID}, Count: 1}
	if kind == ApplicationKeyKind && userID == nil {
		var keyID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM application_keys WHERE credential_id = $1", sessionID).Scan(&keyID); err != nil {
			return fmt.Errorf("identify revoked application credential: %w", err)
		}
		event.Actor = activity.Actor{Kind: activity.ActorApplicationKey, ID: strconv.FormatInt(keyID, 10), CredentialID: sessionID}
		event.Action = activity.ActionApplicationKeyRevoked
		event.Resource = activity.Resource{Kind: activity.ResourceApplicationKey, ID: event.Actor.ID}
	} else if (kind == "admin" || kind == "emby") && userID != nil {
		event.Actor = activity.Actor{Kind: activity.ActorUser, ID: *userID, CredentialID: sessionID}
	} else {
		return ErrUnauthorized
	}
	if err := activity.Record(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit authentication revocation: %w", err)
	}
	return nil
}

// ServerID returns a persistent public server identifier shared by all instances.
func (s *Store) ServerID(ctx context.Context) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	var stored string
	err = s.pool.QueryRow(ctx, `INSERT INTO server_settings (key, value)
		VALUES ('server_id', $1)
		ON CONFLICT (key) DO UPDATE SET value = server_settings.value
		RETURNING value`, id).Scan(&stored)
	if err != nil {
		return "", fmt.Errorf("read server identifier: %w", err)
	}
	return stored, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+userColumns+" FROM users ORDER BY normalized_name, id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("read listed user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user list: %w", err)
	}
	return users, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

// CreateUser is an unaudited fixture helper retained for integration tests.
// Production account creation must use Bootstrap or CreateManagedUser, which
// establish the actor and record the committed change in the same transaction.
func (s *Store) CreateUser(ctx context.Context, name, password string, isAdmin bool) (User, error) {
	name, normalized, err := normalizeName(name)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password, isAdmin); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return User{}, fmt.Errorf("hash user password: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return User{}, err
	}
	user, err := scanUser(s.pool.QueryRow(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, has_password, is_administrator)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+userColumns, id, name, normalized, string(hash), password != "", isAdmin))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_normalized_name_key" {
		return User{}, fmt.Errorf("%w: username is already in use", ErrInvalidInput)
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner, extra ...any) (User, error) {
	var user User
	columns := []any{&user.ID, &user.Name, &user.IsAdministrator, &user.IsDisabled, &user.HasPassword, &user.CreatedAt, &user.Policy, &user.Configuration}
	err := row.Scan(append(columns, extra...)...)
	return user, err
}

func normalizeName(name string) (string, string, error) {
	if !utf8.ValidString(name) {
		return "", "", fmt.Errorf("%w: username must be valid UTF-8", ErrInvalidInput)
	}
	name = strings.TrimSpace(name)
	if count := utf8.RuneCountInString(name); count < 1 || count > 128 {
		return "", "", fmt.Errorf("%w: username must contain between 1 and 128 characters", ErrInvalidInput)
	}
	var normalized strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", "", fmt.Errorf("%w: username must not contain control characters", ErrInvalidInput)
		}
		// Use the lowest rune in each simple-fold cycle so equivalent forms such
		// as Greek sigma and final sigma have the same database uniqueness key.
		canonical := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			if folded < canonical {
				canonical = folded
			}
		}
		normalized.WriteRune(unicode.ToLower(canonical))
	}
	return name, normalized.String(), nil
}

func validatePassword(password string, requirePassword bool) error {
	if !utf8.ValidString(password) || len(password) > 72 {
		return fmt.Errorf("%w: password must contain at most 72 UTF-8 bytes", ErrInvalidInput)
	}
	if requirePassword && password == "" {
		return fmt.Errorf("%w: administrator password is required", ErrInvalidInput)
	}
	return nil
}

func validateClient(client Client) error {
	for _, value := range []string{client.Name, client.DeviceID, client.Device, client.Version} {
		if !utf8.ValidString(value) || len(value) > maxClientFieldBytes || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: client fields must contain at most 256 UTF-8 bytes and no null bytes", ErrInvalidInput)
		}
	}
	return nil
}

func sessionLifetime(kind string) (time.Duration, error) {
	switch kind {
	case "admin":
		return adminLifetime, nil
	case "emby":
		return embyLifetime, nil
	default:
		return 0, fmt.Errorf("%w: unsupported session kind", ErrInvalidInput)
	}
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func randomToken() (string, [32]byte, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate authentication token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func tokenDigest(token string) ([32]byte, bool) {
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return [32]byte{}, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(token)), true
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
