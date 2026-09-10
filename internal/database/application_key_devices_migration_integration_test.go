package database_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

//go:embed migrations/0016_application_keys.sql
var applicationKeyDeviceBaselineFiles embed.FS

func applicationKeyDeviceLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'credentials', (SELECT jsonb_agg(to_jsonb(a) - 'device_registry_id' ORDER BY id) FROM sessions a),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot legacy application device data: %v", err)
	}
	return snapshot
}

// Read the published schema directly before migration. The current Store is
// allowed to depend on newly migrated tables, including ordinary devices.
func applicationKeyDeviceLegacyProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *identity.ApplicationKeyVault) identity.ApplicationKeyPage {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT k.id, k.credential_id, a.client_name, a.created_at,
		k.last_used_at, a.revoked_at, COALESCE(k.created_by, ''), k.ip_address,
		k.reported_device_numeric_id, a.device_id, a.device_name, a.client_version, k.secret_ciphertext
		FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		WHERE a.kind = 'application_key' ORDER BY a.created_at DESC, k.id DESC`)
	if err != nil {
		t.Fatalf("read published application key projection: %v", err)
	}
	defer rows.Close()
	page := identity.ApplicationKeyPage{Items: make([]identity.ApplicationKey, 0), Limit: 50}
	for rows.Next() {
		var key identity.ApplicationKey
		var ciphertext []byte
		if err := rows.Scan(&key.ID, &key.CredentialID, &key.AppName, &key.CreatedAt, &key.LastUsedAt,
			&key.RevokedAt, &key.CreatedBy, &key.IPAddress, &key.ReportedDeviceNumericID,
			&key.Client.DeviceID, &key.Client.Device, &key.Client.Version, &ciphertext); err != nil {
			t.Fatalf("read published application key fields: %v", err)
		}
		key.Client.Name = key.AppName
		if key.RevokedAt == nil {
			key.Token, err = vault.Open(ctx, key.CredentialID, ciphertext)
			if err != nil {
				t.Fatalf("open published application key ciphertext: %v", err)
			}
		}
		page.Items = append(page.Items, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read published application key projection rows: %v", err)
	}
	page.TotalRecordCount = int64(len(page.Items))
	return page
}

func TestMigrateApplicationKeyDevicesPreservesSchema16CredentialsAndProjection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("application key ciphertext preservation requires the Linux vault")
	}
	ctx, pool := migrationTestPool(t)
	applicationKeyVersion15Baseline(t, ctx, pool)
	baseline, err := applicationKeyDeviceBaselineFiles.ReadFile("migrations/0016_application_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(baseline)); err != nil {
		t.Fatalf("apply published schema 16: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES (16, '0016_application_keys.sql');
		INSERT INTO users (id, name, normalized_name, password_hash, is_administrator)
		VALUES ('device-legacy-admin', 'Legacy Administrator', 'legacy administrator', 'synthetic-digest', true)`); err != nil {
		t.Fatal(err)
	}
	vault := identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key"))
	adminToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	liveToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	revokedToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x43}, 32))
	adminDigest, liveDigest, revokedDigest := sha256.Sum256([]byte(adminToken)), sha256.Sum256([]byte(liveToken)), sha256.Sum256([]byte(revokedToken))
	liveCiphertext, err := vault.Seal(ctx, "device-legacy-live", liveToken, true)
	if err != nil {
		t.Fatal(err)
	}
	revokedCiphertext, err := vault.Seal(ctx, "device-legacy-revoked", revokedToken, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, kind, client_name, device_id, device_name,
		client_version, created_at, last_seen_at, expires_at, revoked_at) VALUES
		('device-legacy-actor', 'device-legacy-admin', $1, 'admin', 'Dashboard', 'browser', 'Browser', '1.0',
		 '2025-01-01T00:00:00Z', '2025-01-02T00:00:00Z', clock_timestamp() + interval '1 hour', NULL),
		('device-legacy-live', NULL, $2, 'application_key', 'Legacy Live App', 'legacy-server', 'Last Server Name', '2.0',
		 '2025-02-01T00:00:00Z', '2025-02-05T00:00:00Z', NULL, NULL),
		('device-legacy-revoked', NULL, $3, 'application_key', 'Legacy Old App', 'legacy-server', 'Old Server Name', '1.5',
		 '2025-01-15T00:00:00Z', '2025-01-18T00:00:00Z', NULL, '2025-01-19T00:00:00Z')`, adminDigest[:], liveDigest[:], revokedDigest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_keys (credential_id, secret_ciphertext, created_by, last_used_at, ip_address)
		VALUES ('device-legacy-live', $1, 'device-legacy-admin', '2025-02-05T00:00:00Z', '192.0.2.41'),
		('device-legacy-revoked', $2, 'device-legacy-admin', '2025-01-18T00:00:00Z', '192.0.2.42')`, liveCiphertext, revokedCiphertext); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients (id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ('device-legacy-live-client', 'device-legacy-live', 'Legacy Live App', 'legacy-server', 'Last Server Name', '2.0'),
		('device-legacy-revoked-client', 'device-legacy-revoked', 'Legacy Old App', 'legacy-server', 'Old Server Name', '1.5')`); err != nil {
		t.Fatal(err)
	}
	beforePage := applicationKeyDeviceLegacyProjection(t, ctx, pool, vault)
	before := applicationKeyDeviceLegacySnapshot(t, ctx, pool)
	history := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate shared application devices: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 18 {
		t.Fatalf("application device schema = %d, want 18, error=%v", version, err)
	}
	if after := applicationKeyDeviceLegacySnapshot(t, ctx, pool); after != before {
		t.Fatal("schema 18 changed old key, token digest, ciphertext, client, or account fields")
	}
	var oldHistory string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version <= 16").Scan(&oldHistory); err != nil || oldHistory != history {
		t.Fatalf("shared device migration rewrote published migration history: %v", err)
	}
	store := identity.NewWithApplicationKeyVault(pool, vault)
	actor, err := store.Resolve(ctx, adminToken, "admin")
	if err != nil {
		t.Fatalf("resolve the migrated administrator credential: %v", err)
	}
	afterPage, err := store.ListApplicationKeys(ctx, actor, identity.ApplicationKeyFilter{IncludeRevoked: true, RevealTokens: true})
	if err != nil || !reflect.DeepEqual(afterPage, beforePage) {
		t.Fatalf("shared device migration changed legacy key projections: %v", err)
	}
	principal, err := store.ResolveEmby(ctx, liveToken)
	if err != nil || principal.SessionID != "device-legacy-live" || !principal.IsApplicationKey() {
		t.Fatalf("migration changed live application credential resolution: %v", err)
	}
	device, err := store.LookupApplicationKeyDevice(ctx, principal, "1")
	if err != nil || device.ReportedDeviceID != "legacy-server" || device.Name != "Last Server Name" || device.ActiveLoginCount != 1 {
		t.Fatalf("legacy shared device backfill lost metadata: %v", err)
	}
	var ordinaryID int64
	if err := pool.QueryRow(ctx, "INSERT INTO devices (reported_device_id) VALUES ('new-ordinary-device') RETURNING id").Scan(&ordinaryID); err != nil || ordinaryID != 2 {
		t.Fatalf("shared generation consumed the ordinary sequence's reserved start: id=%d error=%v", ordinaryID, err)
	}
	_, err = pool.Exec(ctx, "UPDATE application_keys SET reported_device_numeric_id = 9999")
	requireApplicationKeyConstraint(t, err, "23503")
	_, err = pool.Exec(ctx, "DELETE FROM application_key_devices WHERE id = 1")
	requireApplicationKeyConstraint(t, err, "23503")
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat shared device migration: %v", err)
	}
	if after := applicationKeyDeviceLegacySnapshot(t, ctx, pool); after != before {
		t.Fatal("repeat shared device migration changed retained credentials")
	}
}
