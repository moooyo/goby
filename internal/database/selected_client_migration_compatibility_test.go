package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Historical fields remain covered by row snapshots and explicit configuration
// expectations. Check schema 42 and 43 additions separately.
func assertSelectedClientMigrationDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM users WHERE local_password_hash IS NOT NULL
			OR profile_pin_ciphertext IS NOT NULL OR local_credentials_revision IS DISTINCT FROM 1
			OR local_password_failures IS DISTINCT FROM 0 OR local_password_blocked_until IS NOT NULL)
		AND NOT EXISTS(SELECT 1 FROM sessions WHERE local_auth IS DISTINCT FROM false)
		AND NOT EXISTS(SELECT 1 FROM item_intro_state)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration inferred local credentials, local authentication, or intro markers: %v", err)
	}
}

func TestMigrateLocalCredentialsClearsInertSchema41ConfigurationWithoutPromotingAuthority(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 41)
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 41 {
		t.Fatalf("local credential baseline version = %d, want 41, error = %v", version, err)
	}
	var laterStateAbsent bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM pg_attribute WHERE NOT attisdropped AND
			(attrelid='users'::regclass AND attname IN ('local_password_hash','profile_pin_ciphertext',
				'local_credentials_revision','local_password_failures','local_password_blocked_until')
			OR attrelid='sessions'::regclass AND attname='local_auth'))
		AND to_regclass('item_intro_state') IS NULL`).Scan(&laterStateAbsent); err != nil || !laterStateAbsent {
		t.Fatalf("schema41 already contains local credentials or intro state: %v", err)
	}
	const originalConfiguration = `{"ProfilePin":"2468","EnableLocalPassword":true,"AudioLanguagePreference":"eng","SubtitleMode":"Always","LegacyExtension":{"ExactInteger":9007199254740993,"Keep":[1,true,"same"]},"LegacyNull":null}`
	const expectedConfiguration = `{"EnableLocalPassword":false,"AudioLanguagePreference":"eng","SubtitleMode":"Always","LegacyExtension":{"ExactInteger":9007199254740993,"Keep":[1,true,"same"]},"LegacyNull":null}`
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id,name,normalized_name,password_hash,has_password,is_administrator,is_disabled,policy,configuration,
		 management_revision,configuration_revision,created_at,updated_at)
		VALUES ('local-legacy-user','Local Legacy User','local legacy user','synthetic-legacy-password-digest',
		 true,true,false,'{"EnableMediaPlayback":true,"LegacyPolicy":[1,true,"same"]}'::jsonb,$1::jsonb,
		 17,19,'2025-01-01T00:00:00Z','2025-01-02T00:00:00Z')`, originalConfiguration); err != nil {
		t.Fatalf("seed inert schema41 local credential settings: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_hash,kind,client_name,device_id,device_name,client_version,client_capabilities,
		 created_at,expires_at,last_seen_at,revoked_at) VALUES
		('local-legacy-admin','local-legacy-user',decode(repeat('a6',32),'hex'),'admin','Dashboard','browser','Browser',
		 '1.0','{}'::jsonb,'2025-01-03T00:00:00Z','2030-01-01T00:00:00Z','2025-02-01T00:00:00Z',NULL),
		('local-legacy-emby','local-legacy-user',decode(repeat('b7',32),'hex'),'emby','Media Client','player','Player',
		 '2.0','{"SupportsMediaControl":true}'::jsonb,'2025-01-04T00:00:00Z','2030-01-01T00:00:00Z',
		 '2025-02-02T00:00:00Z','2025-02-03T00:00:00Z')`); err != nil {
		t.Fatalf("seed historical administrator and media credentials: %v", err)
	}
	// Only configuration changes intentionally. Compare every other original
	// account and session column, including hashes, revisions, and timestamps.
	identitySnapshot := func() string {
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'users',(SELECT jsonb_agg(to_jsonb(u) - 'configuration'
				- ARRAY['local_password_hash','profile_pin_ciphertext','local_credentials_revision',
					'local_password_failures','local_password_blocked_until'] ORDER BY id) FROM users u),
			'sessions',(SELECT jsonb_agg(to_jsonb(s) - 'local_auth' ORDER BY id) FROM sessions s))::text`).Scan(&snapshot); err != nil {
			t.Fatalf("snapshot historical local credential identity: %v", err)
		}
		return snapshot
	}
	before := identitySnapshot()
	history := migrationHistory(t, ctx, pool)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("local credential migration attempt %d: %v", attempt, err)
		}
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != currentMigrationVersion(t) {
			t.Fatalf("local credential target version = %d, want current, error = %v", version, err)
		}
		if identitySnapshot() != before {
			t.Fatal("local credential migration changed historical identity, password hashes, or session tokens")
		}
		var configurationPreserved bool
		if err := pool.QueryRow(ctx, `SELECT configuration=$1::jsonb AND NOT (configuration ? 'ProfilePin')
			AND configuration->'EnableLocalPassword'='false'::jsonb
			FROM users WHERE id='local-legacy-user'`, expectedConfiguration).Scan(&configurationPreserved); err != nil || !configurationPreserved {
			t.Fatalf("local credential migration failed to remove inert authority while retaining unrelated preferences: %v", err)
		}
		assertSelectedClientMigrationDefaults(t, ctx, pool)
		var preservedHistory string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text
			FROM schema_migrations m WHERE version<=41`).Scan(&preservedHistory); err != nil || preservedHistory != history {
			t.Fatalf("local credential migration changed published schema41 history: %v", err)
		}
	}
}
