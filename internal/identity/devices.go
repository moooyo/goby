package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	ManagedDeviceDefaultLimit             = 50
	MaxManagedDevices                     = 200
	MaxManagedDeviceStartIndex            = 2147483647
	MaxDeviceFieldBytes                   = 256
	deviceRegistrationLockNamespace int32 = 1735352915
)

var (
	ErrDeviceNotFound         = errors.New("device not found")
	ErrDeviceRevisionConflict = errors.New("device revision conflict")
)

// ManagedDevice is a safe ordinary-device projection. ID identifies a registry
// generation; reported client IDs and login credentials have separate identities.
type ManagedDevice struct {
	ID               int64
	Revision         int64
	ReportedDeviceID string
	Name             string
	ReportedName     string
	CustomName       *string
	AppName          string
	AppVersion       string
	LastUserID       *string
	LastUserName     *string
	CreatedAt        time.Time
	LastSeenAt       time.Time
	IPAddress        string
	ActiveLoginCount int64
}

type ManagedDeviceFilter struct {
	SearchTerm string
	StartIndex int
	Limit      int
}

type ManagedDevicesPage struct {
	Items            []ManagedDevice
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}

// DeviceDeletion is returned only after commit. RevokedSessionIDs includes all
// ordinary credentials of this generation, including on an idempotent retry,
// so a caller can compensate for interrupted process-local retirement.
type DeviceDeletion struct {
	ID                int64
	DeletedAt         time.Time
	RevokedLoginCount int64
	RevokedSessionIDs []string
}

type DeviceValidationError struct{ Fields map[string]string }

func (e *DeviceValidationError) Error() string { return "invalid device input" }
func (e *DeviceValidationError) Unwrap() error { return ErrInvalidInput }

type deviceRecord struct {
	ID               int64     `json:"id"`
	Revision         int64     `json:"revision"`
	ReportedDeviceID string    `json:"reported_device_id"`
	ReportedName     string    `json:"reported_name"`
	CustomName       *string   `json:"custom_name"`
	AppName          string    `json:"app_name"`
	AppVersion       string    `json:"app_version"`
	LastUserID       *string   `json:"last_user_id"`
	LastUserName     *string   `json:"last_user_name"`
	CreatedAt        time.Time `json:"created_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	IPAddress        string    `json:"ip_address"`
	ActiveLoginCount int64     `json:"active_login_count"`
}

func (row deviceRecord) device() ManagedDevice {
	value := ManagedDevice{ID: row.ID, Revision: row.Revision, ReportedDeviceID: row.ReportedDeviceID,
		ReportedName: row.ReportedName, CustomName: row.CustomName, AppName: row.AppName, AppVersion: row.AppVersion,
		LastUserID: row.LastUserID, LastUserName: row.LastUserName, CreatedAt: row.CreatedAt.UTC(), LastSeenAt: row.LastSeenAt.UTC(),
		IPAddress: row.IPAddress, ActiveLoginCount: row.ActiveLoginCount}
	switch {
	case value.CustomName != nil:
		value.Name = *value.CustomName
	case strings.TrimSpace(value.ReportedName) != "":
		value.Name = value.ReportedName
	case strings.TrimSpace(value.AppName) != "":
		value.Name = value.AppName
	default:
		value.Name = value.ReportedDeviceID
	}
	return value
}

func deviceText(value string) bool {
	return utf8.ValidString(value) && len(value) <= MaxDeviceFieldBytes && strings.IndexFunc(value, unicode.IsControl) < 0
}

func deviceInput(field, message string) error {
	return &DeviceValidationError{Fields: map[string]string{field: message}}
}

func validateDeviceFilter(filter ManagedDeviceFilter) (ManagedDeviceFilter, error) {
	if !deviceText(filter.SearchTerm) {
		return ManagedDeviceFilter{}, deviceInput("SearchTerm", "search must contain at most 256 UTF-8 bytes without controls")
	}
	if filter.StartIndex < 0 || filter.StartIndex > MaxManagedDeviceStartIndex {
		return ManagedDeviceFilter{}, deviceInput("StartIndex", "start index must be between 0 and 2147483647")
	}
	if filter.Limit == 0 {
		filter.Limit = ManagedDeviceDefaultLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxManagedDevices {
		return ManagedDeviceFilter{}, deviceInput("Limit", "limit must be between 1 and 200")
	}
	return filter, nil
}

func normalizeDeviceName(value string) (*string, error) {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return nil, deviceInput("CustomName", "custom name must contain at most 256 UTF-8 bytes without controls")
	}
	value = strings.TrimSpace(value)
	if len(value) > MaxDeviceFieldBytes {
		return nil, deviceInput("CustomName", "custom name must contain at most 256 UTF-8 bytes without controls")
	}
	if value == "" {
		return nil, nil
	}
	return &value, nil
}

func validateDevicePeer(value string) error {
	if !deviceText(value) {
		return deviceInput("IpAddress", "peer address must contain at most 256 UTF-8 bytes without controls")
	}
	return nil
}

type deviceReference struct {
	id       int64
	reported string
}

func nativeDeviceReference(id int64) (deviceReference, error) {
	if id <= 0 {
		return deviceReference{}, deviceInput("Id", "device ID must be a positive decimal integer")
	}
	return deviceReference{id: id}, nil
}

func compatibilityDeviceReference(value string) (deviceReference, error) {
	if value == "" || !deviceText(value) || strings.TrimSpace(value) != value {
		return deviceReference{}, deviceInput("Id", "device identifier must contain 1 to 256 UTF-8 bytes without surrounding whitespace or controls")
	}
	// Canonical numeric IDs address their generation, even if another client
	// reports that same string. Missing numeric IDs never fall back to aliases.
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 && strconv.FormatInt(id, 10) == value {
		return deviceReference{id: id}, nil
	}
	return deviceReference{reported: value}, nil
}

func validDeviceActor(actor Principal, native bool) bool {
	if actor.IsApplicationKey() {
		return !native
	}
	return ((native && actor.Kind == "admin") || (!native && actor.Kind == "emby")) &&
		actor.ApplicationKeyID == 0 && actor.ClientSessionID == "" && validRevalidationID(actor.SessionID) && validRevalidationID(actor.User.ID)
}

func authorizeDeviceActor(ctx context.Context, tx pgx.Tx, actor Principal, native bool, selfRevocation *time.Time) error {
	if !validDeviceActor(actor, native) {
		return ErrUnauthorized
	}
	if actor.IsApplicationKey() {
		return authorizeApplicationKeyActor(ctx, tx, actor, selfRevocation)
	}
	var administrator bool
	err := tx.QueryRow(ctx, `SELECT u.is_administrator FROM sessions a JOIN users u ON u.id = a.user_id
		WHERE a.id = $1 AND a.user_id = $2 AND a.kind = $3 AND NOT u.is_disabled
		AND a.expires_at > clock_timestamp()
		AND (a.revoked_at IS NULL OR ($4::timestamptz IS NOT NULL AND a.revoked_at = $4))`,
		actor.SessionID, actor.User.ID, actor.Kind, selfRevocation).Scan(&administrator)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("authorize device manager: %w", err)
	}
	if !administrator {
		if native || actor.Kind == "admin" {
			return ErrUnauthorized
		}
		return ErrClientSessionForbidden
	}
	return nil
}

func lockDeviceActor(ctx context.Context, tx pgx.Tx, actor Principal, native bool) error {
	if !validDeviceActor(actor, native) {
		return ErrUnauthorized
	}
	var id string
	if actor.User.ID != "" {
		if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnauthorized
			}
			return fmt.Errorf("lock device manager account: %w", err)
		}
	}
	if err := tx.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", actor.SessionID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("lock device manager credential: %w", err)
	}
	if actor.IsApplicationKey() {
		if err := tx.QueryRow(ctx, `SELECT id FROM application_key_clients WHERE id = $1 AND credential_id = $2 FOR SHARE`,
			actor.ClientSessionID, actor.SessionID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnauthorized
			}
			return fmt.Errorf("lock device manager application context: %w", err)
		}
	}
	return authorizeDeviceActor(ctx, tx, actor, native, nil)
}

func (s *Store) beginDeviceRead(ctx context.Context, actor Principal, native bool) (pgx.Tx, error) {
	if !validDeviceActor(actor, native) {
		return nil, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin device read: %w", err)
	}
	if err := lockDeviceActor(ctx, tx, actor, native); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

const deviceColumns = `d.id, d.revision, d.reported_device_id, d.reported_name, d.custom_name,
	d.app_name, d.app_version, d.last_user_id, last_user.name AS last_user_name,
	d.created_at, d.last_seen_at, d.ip_address,
	(SELECT count(*) FROM sessions authentication JOIN users account ON account.id = authentication.user_id
	 WHERE authentication.device_registry_id = d.id AND authentication.kind = 'emby'
	 AND authentication.revoked_at IS NULL AND authentication.expires_at > observation.observed_at
	 AND NOT account.is_disabled) AS active_login_count`

func readManagedDevice(ctx context.Context, tx pgx.Tx, id int64) (ManagedDevice, error) {
	var row deviceRecord
	err := tx.QueryRow(ctx, `WITH observation AS MATERIALIZED (SELECT clock_timestamp() AS observed_at)
		SELECT `+deviceColumns+` FROM devices d LEFT JOIN users last_user ON last_user.id = d.last_user_id
		CROSS JOIN observation WHERE d.id = $1 AND d.id > 1 AND d.deleted_at IS NULL`, id).
		Scan(&row.ID, &row.Revision, &row.ReportedDeviceID, &row.ReportedName, &row.CustomName, &row.AppName,
			&row.AppVersion, &row.LastUserID, &row.LastUserName, &row.CreatedAt, &row.LastSeenAt, &row.IPAddress, &row.ActiveLoginCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedDevice{}, ErrDeviceNotFound
	}
	if err != nil {
		return ManagedDevice{}, fmt.Errorf("read ordinary device: %w", err)
	}
	return row.device(), nil
}

func (s *Store) ListManagedDevices(ctx context.Context, actor Principal, filter ManagedDeviceFilter) (ManagedDevicesPage, error) {
	return s.listManagedDevices(ctx, actor, filter, true)
}

func (s *Store) listManagedDevices(ctx context.Context, actor Principal, filter ManagedDeviceFilter, native bool) (ManagedDevicesPage, error) {
	filter, err := validateDeviceFilter(filter)
	if err != nil {
		return ManagedDevicesPage{}, err
	}
	tx, err := s.beginDeviceRead(ctx, actor, native)
	if err != nil {
		return ManagedDevicesPage{}, err
	}
	defer rollback(tx)
	result := ManagedDevicesPage{Items: make([]ManagedDevice, 0), StartIndex: filter.StartIndex, Limit: filter.Limit}
	var encoded []byte
	err = tx.QueryRow(ctx, `WITH observation AS MATERIALIZED (SELECT clock_timestamp() AS observed_at),
		filtered AS MATERIALIZED (
		 SELECT `+deviceColumns+` FROM devices d LEFT JOIN users last_user ON last_user.id = d.last_user_id
		 CROSS JOIN observation WHERE d.id > 1 AND d.deleted_at IS NULL AND ($1 = ''
		 OR strpos(lower(COALESCE(d.custom_name, d.reported_name)), lower($1)) > 0
		 OR strpos(lower(d.reported_name), lower($1)) > 0 OR strpos(lower(d.reported_device_id), lower($1)) > 0
		 OR strpos(lower(d.app_name), lower($1)) > 0 OR strpos(lower(COALESCE(last_user.name, '')), lower($1)) > 0)
		), page AS (SELECT * FROM filtered ORDER BY last_seen_at DESC, id DESC LIMIT $2 OFFSET $3)
		SELECT (SELECT count(*) FROM filtered), COALESCE(jsonb_agg(to_jsonb(page) ORDER BY last_seen_at DESC, id DESC), '[]'::jsonb)
		FROM page`, filter.SearchTerm, filter.Limit, filter.StartIndex).Scan(&result.TotalRecordCount, &encoded)
	if err != nil {
		return ManagedDevicesPage{}, fmt.Errorf("list ordinary devices: %w", err)
	}
	var rows []deviceRecord
	if err := json.Unmarshal(encoded, &rows); err != nil {
		return ManagedDevicesPage{}, fmt.Errorf("decode ordinary device page: %w", err)
	}
	for _, row := range rows {
		result.Items = append(result.Items, row.device())
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return ManagedDevicesPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedDevicesPage{}, fmt.Errorf("commit device read: %w", err)
	}
	return result, nil
}

// ListEmbyDevices returns all ordinary current generations from one statement.
// The shared application-key server identity has its own separate lifecycle.
func (s *Store) ListEmbyDevices(ctx context.Context, actor Principal) ([]ManagedDevice, error) {
	tx, err := s.beginDeviceRead(ctx, actor, false)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `WITH observation AS MATERIALIZED (SELECT clock_timestamp() AS observed_at)
		SELECT `+deviceColumns+` FROM devices d LEFT JOIN users last_user ON last_user.id = d.last_user_id
		CROSS JOIN observation WHERE d.id > 1 AND d.deleted_at IS NULL ORDER BY d.last_seen_at DESC, d.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list compatibility devices: %w", err)
	}
	result := make([]ManagedDevice, 0)
	for rows.Next() {
		var row deviceRecord
		if err := rows.Scan(&row.ID, &row.Revision, &row.ReportedDeviceID, &row.ReportedName, &row.CustomName, &row.AppName,
			&row.AppVersion, &row.LastUserID, &row.LastUserName, &row.CreatedAt, &row.LastSeenAt, &row.IPAddress, &row.ActiveLoginCount); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read compatibility device: %w", err)
		}
		result = append(result, row.device())
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read compatibility device list: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit compatibility device list: %w", err)
	}
	return result, nil
}

func findDeviceGeneration(ctx context.Context, tx pgx.Tx, reference deviceReference) (int64, string, error) {
	var id int64
	var reported string
	err := tx.QueryRow(ctx, `SELECT id, reported_device_id FROM devices WHERE id > 1
		AND (($1::bigint > 0 AND id = $1) OR ($1::bigint = 0 AND reported_device_id = $2 AND deleted_at IS NULL))`,
		reference.id, reference.reported).Scan(&id, &reported)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrDeviceNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("find device generation: %w", err)
	}
	return id, reported, nil
}

func (s *Store) LookupEmbyDevice(ctx context.Context, actor Principal, lookup string) (ManagedDevice, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return ManagedDevice{}, err
	}
	tx, err := s.beginDeviceRead(ctx, actor, false)
	if err != nil {
		return ManagedDevice{}, err
	}
	defer rollback(tx)
	id, _, err := findDeviceGeneration(ctx, tx, reference)
	if err != nil {
		return ManagedDevice{}, err
	}
	result, err := readManagedDevice(ctx, tx, id)
	if err != nil {
		return ManagedDevice{}, err
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return ManagedDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedDevice{}, fmt.Errorf("commit device lookup: %w", err)
	}
	return result, nil
}
