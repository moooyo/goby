package identity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/moooyo/goby/internal/activity"
)

const (
	ApplicationKeyKind       = "application_key"
	MaxApplicationKeyClients = 256
)

// ApplicationKey is a safe metadata projection unless Token was explicitly
// requested. JSON serialization never includes the recoverable bearer secret.
type ApplicationKey struct {
	ID                      int64
	CredentialID            string
	AppName                 string
	Token                   string `json:"-"`
	CreatedAt               time.Time
	LastUsedAt              *time.Time
	RevokedAt               *time.Time
	CreatedBy               string
	IPAddress               string
	ReportedDeviceNumericID int64
	Client                  Client
}

type ApplicationKeyFilter struct {
	StartIndex     int
	Limit          int
	SearchTerm     string
	IncludeRevoked bool
	RevealTokens   bool
}

type ApplicationKeyPage struct {
	Items            []ApplicationKey
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}

// ApplicationKeyRevocation is returned only after the revocation commits.
type ApplicationKeyRevocation struct {
	ID                       int64
	CredentialID             string
	RevokedAt                time.Time
	CurrentCredentialRevoked bool
}

type applicationKeyOperation uint8

const (
	applicationKeyMetadataRead applicationKeyOperation = iota
	applicationKeyReveal
	applicationKeyMutation
)

// ResolveEmby accepts either a normal Emby login or a userless application key.
// Resolve itself continues to enforce its original exact login-kind boundary.
func (s *Store) ResolveEmby(ctx context.Context, token string) (Principal, error) {
	return s.resolveEmbyWithPeer(ctx, token, "")
}

func (s *Store) resolveEmbyWithPeer(ctx context.Context, token, peerIP string) (Principal, error) {
	digest, ok := tokenDigest(token)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	var principal Principal
	var createdAt, expiresAt *time.Time
	var observedAt time.Time
	// The hash is globally unique. Materialize its one credential once, then
	// project only the matching kind without treating a failed login as a key.
	err := s.pool.QueryRow(ctx, `WITH credential AS MATERIALIZED (
		SELECT id, user_id, kind, client_name, device_id, device_name, client_version,
			device_registry_id, expires_at, last_seen_at, local_auth
		FROM sessions WHERE token_hash = $1 AND revoked_at IS NULL
		AND kind IN ('emby', 'application_key')
	)
	SELECT u.id, u.name, u.is_administrator, u.is_disabled, u.has_password,
		u.created_at, u.policy, u.configuration, u.local_password_hash IS NOT NULL,
		u.profile_pin_ciphertext IS NOT NULL, s.id, s.client_name, s.device_id,
		COALESCE(d.custom_name, s.device_name), s.client_version, s.kind,
		s.expires_at, s.last_seen_at, 0::bigint, ''::text, clock_timestamp()
	FROM credential s JOIN users u ON u.id = s.user_id
	LEFT JOIN devices d ON d.id = s.device_registry_id AND d.deleted_at IS NULL
	WHERE s.kind = 'emby' AND s.expires_at > clock_timestamp() AND NOT u.is_disabled
	AND (NOT s.local_auth OR $2)
	UNION ALL
	SELECT '', '', false, false, false, NULL::timestamptz, NULL::jsonb, NULL::jsonb,
		false, false, s.id, c.client_name, c.device_id, c.device_name, c.client_version,
		s.kind, NULL::timestamptz, c.last_seen_at, k.id, c.id, clock_timestamp()
	FROM credential s JOIN application_keys k ON k.credential_id = s.id
	JOIN application_key_clients c ON c.credential_id = s.id
		AND c.client_name = s.client_name AND c.device_id = s.device_id
	WHERE s.kind = 'application_key' AND s.user_id IS NULL AND s.expires_at IS NULL`, digest[:], IsLocalPeer(peerIP)).
		Scan(&principal.User.ID, &principal.User.Name, &principal.User.IsAdministrator,
			&principal.User.IsDisabled, &principal.User.HasPassword, &createdAt, &principal.User.Policy,
			&principal.User.Configuration, &principal.User.HasLocalPassword, &principal.User.HasProfilePin,
			&principal.SessionID, &principal.Client.Name, &principal.Client.DeviceID,
			&principal.Client.Device, &principal.Client.Version, &principal.Kind, &expiresAt,
			&principal.LastSeenAt, &principal.ApplicationKeyID, &principal.ClientSessionID, &observedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("resolve Emby credential: %w", err)
	}
	if createdAt != nil {
		principal.User.CreatedAt = *createdAt
	}
	if expiresAt != nil {
		principal.ExpiresAt = *expiresAt
	}
	if principal.Kind == "emby" {
		policy, err := ParseRuntimePolicy(principal.User.Policy)
		if err != nil || !parsedLoginPolicyAllows(policy, principal.User.Policy, principal.Client.DeviceID, observedAt) ||
			(!policy.EnableRemoteAccess && !IsLocalPeer(peerIP)) {
			return Principal{}, ErrUnauthorized
		}
	}
	principal.PeerIP = peerIP
	return principal, nil
}

func scanApplicationKeyPrincipal(row rowScanner) (Principal, error) {
	principal := Principal{Kind: ApplicationKeyKind}
	err := row.Scan(&principal.ApplicationKeyID, &principal.SessionID, &principal.ClientSessionID, &principal.Client.Name,
		&principal.Client.DeviceID, &principal.Client.Device, &principal.Client.Version, &principal.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("read application credential: %w", err)
	}
	return principal, nil
}

// CheckApplicationKey validates a trusted credential scope in the caller's
// transaction. A write caller requests a shared credential lock; a read-only
// caller may revalidate without retaining a lock across unrelated resources.
func CheckApplicationKey(ctx context.Context, tx pgx.Tx, credentialID string, lock bool) error {
	if !validRevalidationID(credentialID) {
		return ErrUnauthorized
	}
	if lock {
		var locked string
		err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 FOR SHARE`, credentialID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("lock application credential: %w", err)
		}
	}
	var authorized bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions a
		JOIN application_keys k ON k.credential_id = a.id
		WHERE a.id = $1 AND a.kind = 'application_key' AND a.user_id IS NULL
		AND a.expires_at IS NULL AND a.revoked_at IS NULL)`, credentialID).Scan(&authorized)
	if err != nil {
		return fmt.Errorf("authorize application credential: %w", err)
	}
	if !authorized {
		return ErrUnauthorized
	}
	return nil
}

// CreateApplicationKey issues an independent, non-expiring server credential.
// The creator is audit metadata, never the credential's authentication owner.
func (s *Store) CreateApplicationKey(ctx context.Context, actor Principal, appName, peerIP string, serverClient Client) (ApplicationKey, error) {
	appName = strings.TrimSpace(appName)
	if !validApplicationKeyText(appName, false) || !validApplicationKeyText(peerIP, true) {
		return ApplicationKey{}, fmt.Errorf("%w: app name and peer address must contain at most 256 UTF-8 bytes without controls", ErrInvalidInput)
	}
	serverClient.Name = appName
	if err := validateClient(serverClient); err != nil {
		return ApplicationKey{}, err
	}
	tx, err := s.beginApplicationKeyOperation(ctx, actor, "", applicationKeyMutation)
	if err != nil {
		return ApplicationKey{}, err
	}
	defer rollback(tx)
	if s.applicationKeyVault == nil {
		return ApplicationKey{}, ErrApplicationKeyVaultUnavailable
	}
	// A loaded or replaced master file must decrypt an existing row before it
	// can issue another key. Revoked history also proves a master key exists.
	allowCreate, err := s.allowVaultMasterCreation(ctx, tx)
	if err != nil {
		return ApplicationKey{}, err
	}
	credentialID, err := randomID()
	if err != nil {
		return ApplicationKey{}, err
	}
	token, digest, err := randomToken()
	if err != nil {
		return ApplicationKey{}, err
	}
	ciphertext, err := s.applicationKeyVault.Seal(ctx, credentialID, token, allowCreate)
	if err != nil {
		return ApplicationKey{}, err
	}
	result := ApplicationKey{CredentialID: credentialID, AppName: appName, Token: token,
		CreatedBy: actor.User.ID, IPAddress: peerIP, Client: serverClient}
	if err := tx.QueryRow(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, kind, client_name, device_id, device_name, client_version, expires_at)
		VALUES ($1, NULL, $2, 'application_key', $3, $4, $5, $6, NULL) RETURNING created_at`,
		credentialID, digest[:], appName, serverClient.DeviceID, serverClient.Device, serverClient.Version).Scan(&result.CreatedAt); err != nil {
		return ApplicationKey{}, fmt.Errorf("create application credential: %w", err)
	}
	result.ReportedDeviceNumericID, err = registerApplicationKeyDevice(ctx, tx, serverClient, peerIP)
	if err != nil {
		return ApplicationKey{}, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO application_keys
		(credential_id, secret_ciphertext, created_by, ip_address, reported_device_numeric_id)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5) RETURNING id`,
		credentialID, ciphertext, actor.User.ID, peerIP, result.ReportedDeviceNumericID).Scan(&result.ID); err != nil {
		return ApplicationKey{}, fmt.Errorf("create application key metadata: %w", err)
	}
	clientSessionID, err := randomID()
	if err != nil {
		return ApplicationKey{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ($1, $2, $3, $4, $5, $6)`, clientSessionID, credentialID,
		serverClient.Name, serverClient.DeviceID, serverClient.Device, serverClient.Version); err != nil {
		return ApplicationKey{}, fmt.Errorf("create default application client: %w", err)
	}
	if err := recordApplicationKeyActivity(ctx, tx, actor, activity.ActionApplicationKeyCreated, result.ID); err != nil {
		return ApplicationKey{}, err
	}
	if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
		return ApplicationKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplicationKey{}, fmt.Errorf("commit application key creation: %w", err)
	}
	return result, nil
}

func validateApplicationKeyFilter(filter ApplicationKeyFilter) (ApplicationKeyFilter, error) {
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.StartIndex < 0 || filter.StartIndex > MaxManagedSessionStartIndex || filter.Limit < 1 || filter.Limit > 200 ||
		!validApplicationKeyText(filter.SearchTerm, true) {
		return ApplicationKeyFilter{}, fmt.Errorf("%w: invalid application key filter", ErrInvalidInput)
	}
	return filter, nil
}

func validApplicationKeyText(value string, empty bool) bool {
	return (empty || value != "") && len(value) <= maxClientFieldBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// ListApplicationKeys returns deterministic creation ordering and one-snapshot
// counts. Revoked history never returns its secret, even with RevealTokens.
func (s *Store) ListApplicationKeys(ctx context.Context, actor Principal, filter ApplicationKeyFilter) (ApplicationKeyPage, error) {
	filter, err := validateApplicationKeyFilter(filter)
	if err != nil {
		return ApplicationKeyPage{}, err
	}
	operation := applicationKeyMetadataRead
	if filter.RevealTokens {
		operation = applicationKeyReveal
	}
	tx, err := s.beginApplicationKeyOperation(ctx, actor, "", operation)
	if err != nil {
		return ApplicationKeyPage{}, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `WITH filtered AS MATERIALIZED (
		SELECT k.id, k.credential_id, a.client_name, a.created_at, k.last_used_at,
			a.revoked_at, k.created_by, k.ip_address, k.reported_device_numeric_id,
			a.device_id, a.device_name, a.client_version,
			CASE WHEN $3 AND a.revoked_at IS NULL THEN k.secret_ciphertext END AS ciphertext
		FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		WHERE a.kind = 'application_key' AND ($1 OR a.revoked_at IS NULL)
		AND ($2 = '' OR strpos(lower(a.client_name), lower($2)) > 0)
	), page AS (
		SELECT * FROM filtered ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5
	)
	SELECT (SELECT count(*) FROM filtered), p.id, p.credential_id, p.client_name,
		p.created_at, p.last_used_at, p.revoked_at, p.created_by, p.ip_address,
		p.reported_device_numeric_id, p.device_id, p.device_name, p.client_version, p.ciphertext
	FROM (SELECT 1) anchor LEFT JOIN page p ON true ORDER BY p.created_at DESC, p.id DESC`,
		filter.IncludeRevoked, filter.SearchTerm, filter.RevealTokens, filter.Limit, filter.StartIndex)
	if err != nil {
		return ApplicationKeyPage{}, fmt.Errorf("list application keys: %w", err)
	}
	result := ApplicationKeyPage{Items: make([]ApplicationKey, 0), StartIndex: filter.StartIndex, Limit: filter.Limit}
	type sealedKey struct {
		credentialID string
		ciphertext   []byte
	}
	sealed := make([]sealedKey, 0)
	for rows.Next() {
		var id, deviceNumericID pgtype.Int8
		var credentialID, appName, createdBy, ipAddress, deviceID, deviceName, version pgtype.Text
		var createdAt, lastUsedAt, revokedAt pgtype.Timestamptz
		var ciphertext []byte
		if err := rows.Scan(&result.TotalRecordCount, &id, &credentialID, &appName, &createdAt,
			&lastUsedAt, &revokedAt, &createdBy, &ipAddress, &deviceNumericID, &deviceID, &deviceName, &version, &ciphertext); err != nil {
			rows.Close()
			return ApplicationKeyPage{}, fmt.Errorf("read application key list: %w", err)
		}
		if !id.Valid {
			continue
		}
		key := ApplicationKey{ID: id.Int64, CredentialID: credentialID.String, AppName: appName.String,
			CreatedAt: createdAt.Time, CreatedBy: createdBy.String, IPAddress: ipAddress.String,
			ReportedDeviceNumericID: deviceNumericID.Int64,
			Client:                  Client{Name: appName.String, DeviceID: deviceID.String, Device: deviceName.String, Version: version.String}}
		if lastUsedAt.Valid {
			value := lastUsedAt.Time
			key.LastUsedAt = &value
		}
		if revokedAt.Valid {
			value := revokedAt.Time
			key.RevokedAt = &value
		}
		result.Items = append(result.Items, key)
		sealed = append(sealed, sealedKey{credentialID: key.CredentialID, ciphertext: ciphertext})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ApplicationKeyPage{}, fmt.Errorf("read application key rows: %w", err)
	}
	for index, secret := range sealed {
		if !filter.RevealTokens || result.Items[index].RevokedAt != nil {
			continue
		}
		if s.applicationKeyVault == nil {
			return ApplicationKeyPage{}, ErrApplicationKeyVaultUnavailable
		}
		token, err := s.applicationKeyVault.Open(ctx, secret.credentialID, secret.ciphertext)
		if err != nil {
			return ApplicationKeyPage{}, err
		}
		result.Items[index].Token = token
		// The secret is prepared for this response; network delivery is not
		// observable inside the transaction and is not an audited fact.
		if err := recordApplicationKeyActivity(ctx, tx, actor, activity.ActionApplicationKeyRevealed, result.Items[index].ID); err != nil {
			return ApplicationKeyPage{}, err
		}
	}
	if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
		return ApplicationKeyPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplicationKeyPage{}, fmt.Errorf("commit application key list: %w", err)
	}
	return result, nil
}

func (s *Store) GetApplicationKey(ctx context.Context, actor Principal, id int64, reveal bool) (ApplicationKey, error) {
	if id < 1 {
		return ApplicationKey{}, fmt.Errorf("%w: application key ID must be positive", ErrInvalidInput)
	}
	operation := applicationKeyMetadataRead
	if reveal {
		operation = applicationKeyReveal
	}
	tx, err := s.beginApplicationKeyOperation(ctx, actor, "", operation)
	if err != nil {
		return ApplicationKey{}, err
	}
	defer rollback(tx)
	var result ApplicationKey
	var ciphertext []byte
	err = tx.QueryRow(ctx, `SELECT k.id, k.credential_id, a.client_name, a.created_at,
		k.last_used_at, a.revoked_at, COALESCE(k.created_by, ''), k.ip_address,
		k.reported_device_numeric_id, a.device_id, a.device_name, a.client_version,
		CASE WHEN $2 AND a.revoked_at IS NULL THEN k.secret_ciphertext END
		FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		WHERE k.id = $1 AND a.kind = 'application_key'`, id, reveal).Scan(&result.ID, &result.CredentialID,
		&result.AppName, &result.CreatedAt, &result.LastUsedAt, &result.RevokedAt, &result.CreatedBy,
		&result.IPAddress, &result.ReportedDeviceNumericID, &result.Client.DeviceID, &result.Client.Device,
		&result.Client.Version, &ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplicationKey{}, ErrNotFound
	}
	if err != nil {
		return ApplicationKey{}, fmt.Errorf("get application key: %w", err)
	}
	result.Client.Name = result.AppName
	if reveal && result.RevokedAt == nil {
		if s.applicationKeyVault == nil {
			return ApplicationKey{}, ErrApplicationKeyVaultUnavailable
		}
		result.Token, err = s.applicationKeyVault.Open(ctx, result.CredentialID, ciphertext)
		if err != nil {
			return ApplicationKey{}, err
		}
		// This records successful preparation, not delivery to the caller.
		if err := recordApplicationKeyActivity(ctx, tx, actor, activity.ActionApplicationKeyRevealed, result.ID); err != nil {
			return ApplicationKey{}, err
		}
	}
	if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
		return ApplicationKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplicationKey{}, fmt.Errorf("commit application key read: %w", err)
	}
	return result, nil
}

func (s *Store) RevokeApplicationKey(ctx context.Context, actor Principal, id int64) (ApplicationKeyRevocation, error) {
	if id < 1 {
		return ApplicationKeyRevocation{}, fmt.Errorf("%w: application key ID must be positive", ErrInvalidInput)
	}
	return s.revokeApplicationKey(ctx, actor, id, nil)
}

// RevokeApplicationKeyToken is deliberately idempotent for malformed and
// unknown tokens. It only compares hashes and never opens the secret vault.
func (s *Store) RevokeApplicationKeyToken(ctx context.Context, actor Principal, token string) (ApplicationKeyRevocation, error) {
	digest, ok := tokenDigest(token)
	if !ok {
		return s.revokeApplicationKey(ctx, actor, 0, []byte{})
	}
	return s.revokeApplicationKey(ctx, actor, 0, digest[:])
}

func (s *Store) revokeApplicationKey(ctx context.Context, actor Principal, id int64, digest []byte) (ApplicationKeyRevocation, error) {
	if !validApplicationKeyActor(actor) {
		return ApplicationKeyRevocation{}, ErrUnauthorized
	}
	// The first read locates a target without taking a credential lock before
	// account locks. Immutable IDs are rechecked under the operation lock.
	var credentialID string
	err := s.pool.QueryRow(ctx, `SELECT k.credential_id FROM application_keys k
		JOIN sessions a ON a.id = k.credential_id
		WHERE ($1::bigint > 0 AND k.id = $1) OR ($1 = 0 AND a.token_hash = $2)`, id, digest).Scan(&credentialID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ApplicationKeyRevocation{}, fmt.Errorf("find application key revocation: %w", err)
	}
	tx, err := s.beginApplicationKeyOperation(ctx, actor, credentialID, applicationKeyMutation)
	if err != nil {
		return ApplicationKeyRevocation{}, err
	}
	defer rollback(tx)
	var result ApplicationKeyRevocation
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT k.id, k.credential_id, a.revoked_at FROM application_keys k
		JOIN sessions a ON a.id = k.credential_id WHERE k.credential_id = $1 AND a.kind = 'application_key'`, credentialID).
		Scan(&result.ID, &result.CredentialID, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if id > 0 {
			return ApplicationKeyRevocation{}, ErrNotFound
		}
		if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
			return ApplicationKeyRevocation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplicationKeyRevocation{}, fmt.Errorf("commit unknown key revocation: %w", err)
		}
		return ApplicationKeyRevocation{}, nil
	}
	if err != nil {
		return ApplicationKeyRevocation{}, fmt.Errorf("read application key revocation: %w", err)
	}
	if revokedAt == nil {
		if err := tx.QueryRow(ctx, `UPDATE sessions SET revoked_at = clock_timestamp()
			WHERE id = $1 AND revoked_at IS NULL RETURNING revoked_at`, result.CredentialID).Scan(&result.RevokedAt); err != nil {
			return ApplicationKeyRevocation{}, fmt.Errorf("revoke application credential: %w", err)
		}
		if err := recordApplicationKeyActivity(ctx, tx, actor, activity.ActionApplicationKeyRevoked, result.ID); err != nil {
			return ApplicationKeyRevocation{}, err
		}
	} else {
		result.RevokedAt = *revokedAt
	}
	result.CurrentCredentialRevoked = result.CredentialID == actor.SessionID && actor.IsApplicationKey()
	var expected *time.Time
	if result.CurrentCredentialRevoked {
		expected = &result.RevokedAt
	}
	if err := authorizeApplicationKeyActor(ctx, tx, actor, expected); err != nil {
		return ApplicationKeyRevocation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplicationKeyRevocation{}, fmt.Errorf("commit application key revocation: %w", err)
	}
	return result, nil
}

func recordApplicationKeyActivity(ctx context.Context, tx pgx.Tx, principal Principal, action activity.Action, id int64) error {
	actor, err := identityActivityActor(principal)
	if err != nil {
		return err
	}
	return activity.Record(ctx, tx, activity.Event{
		Action: action, Source: identityActivitySource(principal.Kind), Actor: actor,
		Resource: activity.Resource{Kind: activity.ResourceApplicationKey, ID: strconv.FormatInt(id, 10)},
		Count:    1,
	})
}

func validApplicationKeyActor(actor Principal) bool {
	return validRevalidationID(actor.SessionID) && ((actor.IsApplicationKey() && validRevalidationID(actor.ClientSessionID)) ||
		((actor.Kind == "admin" || actor.Kind == "emby") && actor.ApplicationKeyID == 0 && actor.ClientSessionID == "" && validRevalidationID(actor.User.ID)))
}

func (s *Store) beginApplicationKeyOperation(ctx context.Context, actor Principal, targetCredentialID string, operation applicationKeyOperation) (pgx.Tx, error) {
	if !validApplicationKeyActor(actor) {
		return nil, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin application key operation: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			rollback(tx)
		}
	}()
	if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
		return nil, err
	}
	// Metadata reads only retain shared actor locks. Reveal and mutation keep
	// the management lock before credentials, preserving crossed-target order
	// and the authority boundary for secret preparation and audit writes.
	if operation != applicationKeyMetadataRead {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
			return nil, fmt.Errorf("lock application key management: %w", err)
		}
		if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
			return nil, err
		}
	}
	if actor.User.ID != "" {
		var accountID string
		err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID).Scan(&accountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		if err != nil {
			return nil, fmt.Errorf("lock application key actor account: %w", err)
		}
	}
	locking := " FOR SHARE"
	if operation == applicationKeyMutation {
		locking = " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, `SELECT id FROM sessions WHERE id = ANY($1::text[]) ORDER BY id`+locking,
		[]string{actor.SessionID, targetCredentialID})
	if err != nil {
		return nil, fmt.Errorf("lock application key credentials: %w", err)
	}
	for rows.Next() {
		var locked string
		if err := rows.Scan(&locked); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read application credential lock: %w", err)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read application credential locks: %w", err)
	}
	if actor.IsApplicationKey() {
		var clientSessionID string
		err := tx.QueryRow(ctx, `SELECT id FROM application_key_clients
			WHERE id = $1 AND credential_id = $2 FOR SHARE`, actor.ClientSessionID, actor.SessionID).Scan(&clientSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		if err != nil {
			return nil, fmt.Errorf("lock application key actor client: %w", err)
		}
	}
	if err := authorizeApplicationKeyActor(ctx, tx, actor, nil); err != nil {
		return nil, err
	}
	ok = true
	return tx, nil
}

func authorizeApplicationKeyActor(ctx context.Context, tx AuthorizationTx, actor Principal, expectedSelfRevocation *time.Time) error {
	var authorized bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM sessions a LEFT JOIN users u ON u.id = a.user_id
		LEFT JOIN application_keys k ON k.credential_id = a.id
		WHERE a.id = $1 AND a.kind = $2
		AND (a.revoked_at IS NULL OR ($5::timestamptz IS NOT NULL AND a.revoked_at = $5))
		AND ((a.kind IN ('admin', 'emby') AND a.user_id = $3 AND u.is_administrator
			AND NOT u.is_disabled AND a.expires_at > clock_timestamp() AND $4::bigint = 0 AND $6 = '')
			OR (a.kind = 'application_key' AND a.user_id IS NULL AND $3 = ''
				AND a.expires_at IS NULL AND k.id = $4 AND EXISTS (SELECT 1 FROM application_key_clients c
					WHERE c.credential_id = a.id AND c.id = $6))))`, actor.SessionID, actor.Kind,
		actor.User.ID, actor.ApplicationKeyID, expectedSelfRevocation, actor.ClientSessionID).Scan(&authorized)
	if err != nil {
		return fmt.Errorf("authorize application key manager: %w", err)
	}
	if !authorized {
		return ErrUnauthorized
	}
	return nil
}
