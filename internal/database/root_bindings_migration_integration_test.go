package database_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const rootBindingMigrationRoot = "root-binding-legacy-root"
const rootBindingMigrationMaxJSON = 4 * 1024 * 1024

type rootBindingLegacyActivity struct {
	action, resource, state string
}

func rootBindingLegacyActivities() []rootBindingLegacyActivity {
	return []rootBindingLegacyActivity{
		{"user.created", "user", ""}, {"user.updated", "user", ""}, {"user.password_reset", "user", ""},
		{"session.login", "session", ""}, {"session.revoked", "session", ""},
		{"application_key.created", "application_key", ""}, {"application_key.revealed", "application_key", ""}, {"application_key.revoked", "application_key", ""},
		{"device.updated", "device", ""}, {"device.removed", "device", ""},
		{"library.created", "library", ""}, {"library.removed", "library", ""},
		{"scan.requested", "scan", ""}, {"scan.cancel_requested", "scan", ""}, {"scan.finished", "scan", "completed"},
		{"metadata.updated", "item", ""}, {"settings.updated", "settings", ""},
		{"task.admitted", "task_run", ""}, {"task.cancel_requested", "task_run", ""}, {"task.finished", "task_run", "interrupted"}, {"task.schedule_updated", "task", ""},
		{"backup.requested", "backup", ""}, {"backup.cancel_requested", "backup", ""}, {"backup.finished", "backup", "cancelled"}, {"backup.imported", "backup", ""},
		{"backup.delete_requested", "backup", ""}, {"backup.deleted", "backup", ""}, {"backup.downloaded", "backup", ""},
		{"restore.requested", "restore", ""}, {"restore.planned", "restore", ""}, {"restore.apply_requested", "restore", ""},
		{"restore.applied", "restore", "completed"}, {"restore.rollback_requested", "restore", ""}, {"restore.cancel_requested", "restore", ""}, {"restore.failed", "restore", "failed"},
	}
}

func rootBindingSchema27Fixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	themeOwnersMigrateTo(t, ctx, pool, 27)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash,is_administrator,policy,configuration)
		VALUES('root-binding-legacy-user','Historical root reader','historical root reader','fixture-only-no-authentication',true,
		'{"EnableAllFolders":true}','{"AudioLanguagePreference":"eng"}');
		INSERT INTO libraries(id,name,collection_type) VALUES('root-binding-legacy-library','Historical library','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('root-binding-legacy-root','root-binding-legacy-library','/not-opened/root-a','/not-opened','root-a'),
		('root-binding-other-root','root-binding-legacy-library','/not-opened/root-b','/not-opened','root-b');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('root-binding-legacy-library','root-binding-legacy-library','Historical library','historical library','CollectionFolder',true);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path) VALUES
		('root-binding-legacy-item','root-binding-legacy-library','root-binding-legacy-root','root-binding-legacy-library',
		'Historical movie','historical movie','Movie','/not-opened/root-a/Film.mkv','Film.mkv');
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played) VALUES
		('root-binding-legacy-user','root-binding-legacy-item',1234567,3,true,false)`); err != nil {
		t.Fatalf("seed the published schema27 root binding fixture: %v", err)
	}
	for index, entry := range rootBindingLegacyActivities() {
		if _, err := pool.Exec(ctx, `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,
			actor_credential_id,resource_kind,resource_id,revision,affected_count,state)
			VALUES($1,'Info','native','user','historical-admin','historical-credential',$2,$3,7,9,$4)`,
			entry.action, entry.resource, fmt.Sprintf("historical-activity-%02d", index), entry.state); err != nil {
			t.Fatalf("seed published activity %s: %v", entry.action, err)
		}
	}
}

func migrateRootBindings28(ctx context.Context, pool *pgxpool.Pool, recovery bool) error {
	if !recovery {
		return database.Migrate(ctx, pool)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer themeOwnersRollback(tx)
	if err := database.RecoveryMigrateTo(ctx, tx, 28); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rootBindingOldRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table deviceLegacyTable) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ",") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version <= 27"
	}
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text
		FROM (`+statement+`) r`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot original schema27 columns in %s: %v", table.name, err)
	}
	return snapshot
}

func rootBindingCaptureOldTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []deviceLegacyTable {
	t.Helper()
	tables := captureDeviceLegacyTables(t, ctx, pool)
	if len(tables) != 35 {
		t.Fatalf("schema27 fixture has %d tables, want 35", len(tables))
	}
	for index := range tables {
		tables[index].snapshot = rootBindingOldRows(t, ctx, pool, tables[index])
	}
	return tables
}

func rootBindingAssertOldTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tables []deviceLegacyTable) {
	t.Helper()
	for _, table := range tables {
		if rootBindingOldRows(t, ctx, pool, table) != table.snapshot {
			t.Errorf("root binding migration changed historical rows or columns in %s", table.name)
		}
	}
}

func rootBindingActivityChecks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, preservedOnly bool) string {
	t.Helper()
	filter := ""
	if preservedOnly {
		filter = ` WHERE name <> 'activity_entries_root_binding_fact_check'
			AND NOT (columns = ARRAY['action']::text[] OR columns = ARRAY['resource_kind']::text[]
				OR columns = ARRAY['action','resource_kind']::text[])`
	}
	var snapshot string
	if err := pool.QueryRow(ctx, `WITH checks AS (
		SELECT c.oid::bigint id,c.conname::text name,pg_get_constraintdef(c.oid) definition,
			c.convalidated is_validated,c.condeferrable is_deferrable,c.condeferred is_deferred,c.connoinherit no_inherit,
			ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey)
				ORDER BY a.attname::text COLLATE "C") columns
		FROM pg_constraint c WHERE c.conrelid='activity_entries'::regclass AND c.contype='c'
	) SELECT COALESCE(jsonb_agg(to_jsonb(checks) ORDER BY name),'[]'::jsonb)::text FROM checks`+filter).Scan(&snapshot); err != nil {
		t.Fatalf("read root binding activity CHECK inventory: %v", err)
	}
	return snapshot
}

func TestStorageRootBindingMigrationPreservesSchema27RowsAndRenamedChecks(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(fmt.Sprintf("recovery_%t", recovery), func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			rootBindingSchema27Fixture(t, ctx, pool)
			tables := rootBindingCaptureOldTables(t, ctx, pool)
			if _, err := pool.Exec(ctx, `DO $$ DECLARE entry record; counter integer := 0; BEGIN
				FOR entry IN SELECT conname FROM pg_constraint WHERE conrelid='activity_entries'::regclass AND contype='c' ORDER BY conname
				LOOP counter := counter + 1;
					EXECUTE format('ALTER TABLE activity_entries RENAME CONSTRAINT %I TO %I',entry.conname,'root_binding_old_check_'||counter::text);
				END LOOP;
			END $$`); err != nil {
				t.Fatalf("rename the original activity CHECKs: %v", err)
			}
			preserved := rootBindingActivityChecks(t, ctx, pool, true)
			for range 2 {
				if err := migrateRootBindings28(ctx, pool, recovery); err != nil {
					t.Fatalf("migrate published roots to schema28: %v", err)
				}
			}
			rootBindingAssertOldTables(t, ctx, pool, tables)
			if rootBindingActivityChecks(t, ctx, pool, true) != preserved {
				t.Fatal("migration replaced an unrelated activity CHECK or the original terminal CHECK")
			}
			var version int64
			var count, boundRoots, changedLegacyActivity int
			if err := pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),
				(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()),
				(SELECT count(*) FROM library_roots WHERE binding_revision<>1 OR storage_binding IS NOT NULL OR bound_at IS NOT NULL OR bound_by IS NOT NULL),
				(SELECT count(*) FROM activity_entries WHERE previous_revision<>0 OR observation_fingerprint<>'')`).Scan(
				&version, &count, &boundRoots, &changedLegacyActivity); err != nil || version != 28 || count != 35 || boundRoots != 0 || changedLegacyActivity != 0 {
				t.Fatalf("schema28 inferred binding or audit facts: version=%d tables=%d roots=%d activity=%d error=%v", version, count, boundRoots, changedLegacyActivity, err)
			}
		})
	}
}

func TestStorageRootBindingMigrationRejectsAmbiguousOrMissingChecksAtomically(t *testing.T) {
	for _, scenario := range []string{"ambiguous resource check", "missing action resource check"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			rootBindingSchema27Fixture(t, ctx, pool)
			if scenario == "ambiguous resource check" {
				if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries ADD CONSTRAINT root_binding_ambiguous_resource CHECK(length(resource_kind)>0)`); err != nil {
					t.Fatal(err)
				}
			} else if _, err := pool.Exec(ctx, "ALTER TABLE activity_entries DROP CONSTRAINT activity_entries_action_resource_check"); err != nil {
				t.Fatal(err)
			}
			tables := rootBindingCaptureOldTables(t, ctx, pool)
			checks := rootBindingActivityChecks(t, ctx, pool, false)
			if err := database.Migrate(ctx, pool); err == nil {
				t.Fatal("schema28 accepted an ambiguous or missing historical CHECK")
			}
			rootBindingAssertOldTables(t, ctx, pool, tables)
			if rootBindingActivityChecks(t, ctx, pool, false) != checks {
				t.Fatal("failed discovery did not restore previously dropped CHECKs")
			}
			var version int64
			var columns int
			if err := pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),
				(SELECT count(*) FROM pg_attribute WHERE NOT attisdropped AND
					(attrelid='library_roots'::regclass AND attname IN ('binding_revision','storage_binding','bound_at','bound_by')
					OR attrelid='activity_entries'::regclass AND attname IN ('previous_revision','observation_fingerprint')))`).
				Scan(&version, &columns); err != nil || version != 27 || columns != 0 {
				t.Fatalf("failed migration retained partial columns or history: version=%d columns=%d error=%v", version, columns, err)
			}
		})
	}
}

func rootBindingRequireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		t.Fatalf("root binding constraint returned %v, want SQLSTATE %s", err, code)
	}
}

func TestStorageRootBindingMigrationEnforcesBindingCoherenceAndBounds(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	rootBindingSchema27Fixture(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var overhead int
	if err := pool.QueryRow(ctx, `SELECT octet_length(jsonb_build_object('padding','')::text)`).Scan(&overhead); err != nil {
		t.Fatal(err)
	}
	at := "2026-09-12T00:00:00Z"
	maximum := `{"padding":"` + strings.Repeat("x", rootBindingMigrationMaxJSON-overhead) + `"}`
	oversized := `{"padding":"` + strings.Repeat("x", rootBindingMigrationMaxJSON-overhead+1) + `"}`
	for _, test := range []struct {
		name                         string
		revision, binding, at, actor any
		code                         string
		exactSize                    bool
	}{
		{"unbound", int64(1), nil, nil, nil, "", false},
		{"historical actor without foreign key", int64(2), `{}`, at, "deleted-admin._:27", "", false},
		{"maximum revision", int64(9223372036854775807), `{}`, at, "admin", "", false},
		{"maximum actor", int64(2), `{}`, at, strings.Repeat("a", 256), "", false},
		{"exact JSON bytes", int64(2), maximum, at, "admin", "", true},
		{"zero revision", int64(0), nil, nil, nil, "23514", false},
		{"negative revision", int64(-1), nil, nil, nil, "23514", false},
		{"null revision", nil, nil, nil, nil, "23502", false},
		{"unbound timestamp", int64(1), nil, at, nil, "23514", false},
		{"unbound actor", int64(1), nil, nil, "admin", "23514", false},
		{"bound missing timestamp", int64(2), `{}`, nil, "admin", "23514", false},
		{"bound missing actor", int64(2), `{}`, at, nil, "23514", false},
		{"JSON null", int64(2), `null`, at, "admin", "23514", false},
		{"JSON array", int64(2), `[]`, at, "admin", "23514", false},
		{"JSON scalar", int64(2), `"binding"`, at, "admin", "23514", false},
		{"oversized JSON", int64(2), oversized, at, "admin", "23514", false},
		{"infinite timestamp", int64(2), `{}`, "infinity", "admin", "23514", false},
		{"negative infinite timestamp", int64(2), `{}`, "-infinity", "admin", "23514", false},
		{"empty actor", int64(2), `{}`, at, "", "23514", false},
		{"punctuation first", int64(2), `{}`, at, ".admin", "23514", false},
		{"actor path", int64(2), `{}`, at, "admin/user", "23514", false},
		{"actor control", int64(2), `{}`, at, "admin\nuser", "23514", false},
		{"non-ASCII actor", int64(2), `{}`, at, "Admin\u00e9", "23514", false},
		{"oversized actor", int64(2), `{}`, at, strings.Repeat("a", 257), "23514", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer themeOwnersRollback(tx)
			_, err = tx.Exec(ctx, `UPDATE library_roots SET binding_revision=$1,storage_binding=$2::text::jsonb,
				bound_at=$3::text::timestamptz,bound_by=$4::text WHERE id=$5`, test.revision, test.binding, test.at, test.actor, rootBindingMigrationRoot)
			if test.code != "" {
				rootBindingRequireSQLState(t, err, test.code)
				return
			}
			if err != nil {
				t.Fatalf("valid structural binding was rejected: %v", err)
			}
			if test.exactSize {
				var size int
				if err := tx.QueryRow(ctx, "SELECT octet_length(storage_binding::text) FROM library_roots WHERE id=$1", rootBindingMigrationRoot).Scan(&size); err != nil || size != rootBindingMigrationMaxJSON {
					t.Fatalf("the accepted JSON did not reach the exact byte boundary: %d, %v", size, err)
				}
			}
		})
	}
}

func TestStorageRootBindingMigrationRestrictsNewAuditFactsAndPreservesOldActions(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	rootBindingSchema27Fixture(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	type auditFact struct {
		action, source, actorKind, actorID, resource, state string
		revision, previous, fingerprint                     any
		count                                               int64
		fields                                              []string
	}
	valid := auditFact{action: "library.root_binding.updated", source: "native", actorKind: "user", actorID: "historical-admin",
		resource: "library_root", revision: int64(2), previous: int64(1), fingerprint: strings.Repeat("a", 64), fields: []string{}}
	insert := func(tx pgx.Tx, value auditFact, resourceID string) error {
		_, err := tx.Exec(ctx, `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id,
			revision,previous_revision,observation_fingerprint,affected_count,state,changed_fields)
			VALUES($1,'Info',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, value.action, value.source, value.actorKind,
			value.actorID, value.resource, resourceID, value.revision, value.previous, value.fingerprint, value.count, value.state, value.fields)
		return err
	}
	for index, legacy := range rootBindingLegacyActivities() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		value := valid
		value.action, value.resource, value.state = legacy.action, legacy.resource, legacy.state
		value.revision, value.previous, value.fingerprint = int64(7), int64(0), ""
		err = insert(tx, value, fmt.Sprintf("accepted-old-action-%02d", index))
		themeOwnersRollback(tx)
		if err != nil {
			t.Fatalf("schema28 removed an old permitted action/resource pair %s: %v", legacy.action, err)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*auditFact)
		code   string
	}{
		{"valid update", func(*auditFact) {}, ""},
		{"maximum successor", func(f *auditFact) { f.previous = int64(9223372036854775806); f.revision = int64(9223372036854775807) }, ""},
		{"zero previous", func(f *auditFact) { f.previous = int64(0); f.revision = int64(1) }, "23514"},
		{"negative previous", func(f *auditFact) { f.previous = int64(-9223372036854775808); f.revision = int64(0) }, "23514"},
		{"maximum previous without overflow", func(f *auditFact) { f.previous = int64(9223372036854775807); f.revision = int64(9223372036854775807) }, "23514"},
		{"unchanged revision", func(f *auditFact) { f.revision = int64(1) }, "23514"},
		{"skipped revision", func(f *auditFact) { f.revision = int64(3) }, "23514"},
		{"null previous", func(f *auditFact) { f.previous = nil }, "23502"},
		{"null fingerprint", func(f *auditFact) { f.fingerprint = nil }, "23502"},
		{"short fingerprint", func(f *auditFact) { f.fingerprint = strings.Repeat("a", 63) }, "23514"},
		{"long fingerprint", func(f *auditFact) { f.fingerprint = strings.Repeat("a", 65) }, "23514"},
		{"uppercase fingerprint", func(f *auditFact) { f.fingerprint = strings.Repeat("A", 64) }, "23514"},
		{"nonhex fingerprint", func(f *auditFact) { f.fingerprint = strings.Repeat("g", 64) }, "23514"},
		{"Emby source", func(f *auditFact) { f.source = "emby" }, "23514"},
		{"system source", func(f *auditFact) { f.source = "system" }, "23514"},
		{"application actor", func(f *auditFact) { f.actorKind = "application_key" }, "23514"},
		{"system actor", func(f *auditFact) { f.actorKind = "system"; f.actorID = "" }, "23514"},
		{"wrong resource", func(f *auditFact) { f.resource = "library" }, "23514"},
		{"nonzero count", func(f *auditFact) { f.count = 1 }, "23514"},
		{"terminal state", func(f *auditFact) { f.state = "completed" }, "23514"},
		{"changed fields", func(f *auditFact) { f.fields = []string{"Name"} }, "23514"},
		{"old action previous", func(f *auditFact) { f.action = "library.created"; f.resource = "library"; f.fingerprint = "" }, "23514"},
		{"old action fingerprint", func(f *auditFact) { f.action = "library.created"; f.resource = "library"; f.previous = int64(0) }, "23514"},
		{"old action root kind", func(f *auditFact) { f.action = "library.created"; f.previous = int64(0); f.fingerprint = "" }, "23514"},
		{"unknown action", func(f *auditFact) { f.action = "library.root_binding.unknown" }, "23514"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.change(&value)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer themeOwnersRollback(tx)
			err = insert(tx, value, "historical-root-id")
			if test.code != "" {
				rootBindingRequireSQLState(t, err, test.code)
			} else if err != nil {
				t.Fatalf("valid root binding audit fact was rejected: %v", err)
			}
		})
	}
}
