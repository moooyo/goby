package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func backupPublicationMigrate(ctx context.Context, pool *pgxpool.Pool, recovery bool) error {
	if !recovery {
		return database.Migrate(ctx, pool)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer themeOwnersRollback(tx)
	if err := database.RecoveryMigrateTo(ctx, tx, 71); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func backupPublicationOldActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var rows string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(entry) ORDER BY id),'[]'::jsonb)::text
		FROM activity_entries entry`).Scan(&rows); err != nil {
		t.Fatal("capture historical backup publication activity")
	}
	return rows
}

func backupPublicationNamedChecks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, names []string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `WITH checks AS (
		SELECT c.oid::bigint id,c.conname::text name,pg_get_constraintdef(c.oid) definition,
			c.convalidated is_validated,c.condeferrable is_deferrable,c.condeferred is_deferred,c.connoinherit no_inherit,
			ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey)
				ORDER BY a.attname::text COLLATE "C") columns
		FROM pg_constraint c WHERE c.conrelid='activity_entries'::regclass AND c.contype='c'
			AND c.conname::text=ANY($1::text[])
	) SELECT COALESCE(jsonb_agg(to_jsonb(checks) ORDER BY name),'[]'::jsonb)::text FROM checks`, names).Scan(&snapshot); err != nil {
		t.Fatal("capture unrelated activity CHECK identities and definitions")
	}
	return snapshot
}

func backupPublicationExpectedTerminal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, oldDefinition string) string {
	t.Helper()
	if !strings.HasPrefix(oldDefinition, "CHECK ") {
		t.Fatal("historical terminal constraint is not a CHECK")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin PostgreSQL terminal predicate normalization")
	}
	defer themeOwnersRollback(tx)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE activity_entries ADD CONSTRAINT publication_expected_terminal
		CHECK ((%s) OR (action='backup.published' AND state='completed'))`, strings.TrimPrefix(oldDefinition, "CHECK "))); err != nil {
		t.Fatal("normalize the complete historical terminal predicate and typed publication case")
	}
	var definition string
	if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid='activity_entries'::regclass AND conname='publication_expected_terminal'`).Scan(&definition); err != nil {
		t.Fatal("read the normalized expected terminal predicate")
	}
	return definition
}

func TestBackupPublicationMigrationPreservesHistoricalTerminalNames(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, renamed := range []bool{false, true} {
			t.Run(fmt.Sprintf("recovery_%t_renamed_%t", recovery, renamed), func(t *testing.T) {
				ctx, pool := migrationTestPool(t)
				rootBindingSchema27Fixture(t, ctx, pool)
				terminalName := "activity_entries_terminal_state_check"
				if renamed {
					terminalName = "historical publication terminal"
					if _, err := pool.Exec(ctx, "ALTER TABLE activity_entries RENAME CONSTRAINT activity_entries_terminal_state_check TO "+pgx.Identifier{terminalName}.Sanitize()); err != nil {
						t.Fatal("rename the published schema27 terminal CHECK")
					}
				}
				themeOwnersMigrateTo(t, ctx, pool, 70)
				var originalDefinition string
				if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
					WHERE conrelid='activity_entries'::regclass AND conname=$1`, terminalName).Scan(&originalDefinition); err != nil {
					t.Fatal("schema28 did not preserve the historical terminal CHECK name")
				}
				expectedDefinition := backupPublicationExpectedTerminal(t, ctx, pool, originalDefinition)
				history, rows := migrationHistory(t, ctx, pool), backupPublicationOldActivity(t, ctx, pool)
				var unrelatedNames []string
				if err := pool.QueryRow(ctx, `SELECT ARRAY(SELECT conname::text FROM pg_constraint
					WHERE conrelid='activity_entries'::regclass AND contype='c' AND conname::text<>ALL($1::text[])
					ORDER BY conname)`, []string{terminalName, "activity_entries_action_check", "activity_entries_action_resource_check", "activity_entries_root_binding_fact_check"}).Scan(&unrelatedNames); err != nil {
					t.Fatal("select the unrelated historical activity CHECKs")
				}
				unrelated := backupPublicationNamedChecks(t, ctx, pool, unrelatedNames)
				for range 2 {
					if err := backupPublicationMigrate(ctx, pool, recovery); err != nil {
						t.Fatalf("upgrade the preserved terminal CHECK to publication semantics: %v", err)
					}
				}
				var retainedHistory string
				if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(entry) ORDER BY version)::text
					FROM schema_migrations entry WHERE version<=70`).Scan(&retainedHistory); err != nil || retainedHistory != history {
					t.Fatalf("publication migration changed the original history prefix: %v", err)
				}
				if backupPublicationOldActivity(t, ctx, pool) != rows || backupPublicationNamedChecks(t, ctx, pool, unrelatedNames) != unrelated {
					t.Fatal("publication migration changed historical activity or an unrelated CHECK")
				}
				wantVersion := currentMigrationVersion(t)
				if recovery {
					wantVersion = 71
				}
				if version, err := database.SchemaVersion(ctx, pool); err != nil || version != wantVersion {
					t.Fatalf("publication migration ended at version %d, want %d: %v", version, wantVersion, err)
				}
				var names []string
				if err := pool.QueryRow(ctx, `SELECT array_agg(c.conname::text ORDER BY c.conname) FROM pg_constraint c
					WHERE c.conrelid='activity_entries'::regclass AND c.contype='c'
					AND ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey)
						ORDER BY a.attname::text COLLATE "C")=ARRAY['action','state']::text[]`).Scan(&names); err != nil || len(names) != 1 || names[0] != terminalName {
					t.Fatalf("publication migration lost the exact historical terminal CHECK identity: %v", err)
				}
				var actualDefinition string
				var validated bool
				if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid),convalidated FROM pg_constraint
					WHERE conrelid='activity_entries'::regclass AND conname=$1`, terminalName).Scan(&actualDefinition, &validated); err != nil || !validated || actualDefinition != expectedDefinition {
					t.Fatalf("publication migration did not retain the complete old predicate plus its typed terminal case: %v", err)
				}
				for _, test := range []struct {
					name, state, source, resource, fingerprint, code string
				}{
					{name: "published", state: "completed", source: "system", resource: "backup", fingerprint: strings.Repeat("a", 64)},
					{name: "nonterminal", state: "", source: "system", resource: "backup", fingerprint: strings.Repeat("a", 64), code: "23514"},
					{name: "failed", state: "failed", source: "system", resource: "backup", fingerprint: strings.Repeat("a", 64), code: "23514"},
					{name: "wrong_source", state: "completed", source: "native", resource: "backup", fingerprint: strings.Repeat("a", 64), code: "23514"},
					{name: "wrong_resource", state: "completed", source: "system", resource: "item", fingerprint: strings.Repeat("a", 64), code: "23514"},
					{name: "missing_fingerprint", state: "completed", source: "system", resource: "backup", code: "23514"},
				} {
					t.Run(test.name, func(t *testing.T) {
						tx, err := pool.Begin(ctx)
						if err != nil {
							t.Fatal("begin a publication activity constraint witness")
						}
						defer themeOwnersRollback(tx)
						_, err = tx.Exec(ctx, `INSERT INTO activity_entries(action,severity,source,actor_kind,resource_kind,resource_id,state,observation_fingerprint)
							VALUES('backup.published','Info',$1,'system',$2,'publication-migration-witness',$3,$4)`, test.source, test.resource, test.state, test.fingerprint)
						if test.code != "" {
							rootBindingRequireSQLState(t, err, test.code)
						} else if err != nil {
							t.Fatalf("valid published backup was rejected: %v", err)
						}
					})
				}
			})
		}
	}
}

func TestBackupPublicationMigrationRejectsMissingOrAmbiguousTerminalAtomically(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, ambiguous := range []bool{false, true} {
			t.Run(fmt.Sprintf("recovery_%t_ambiguous_%t", recovery, ambiguous), func(t *testing.T) {
				ctx, pool := migrationTestPool(t)
				rootBindingSchema27Fixture(t, ctx, pool)
				themeOwnersMigrateTo(t, ctx, pool, 70)
				statement := "ALTER TABLE activity_entries DROP CONSTRAINT activity_entries_terminal_state_check"
				if ambiguous {
					statement = "ALTER TABLE activity_entries ADD CONSTRAINT publication_ambiguous_terminal CHECK(action<>'' OR state='')"
				}
				if _, err := pool.Exec(ctx, statement); err != nil {
					t.Fatal("prepare the missing or ambiguous historical terminal CHECK")
				}
				history, rows := migrationHistory(t, ctx, pool), backupPublicationOldActivity(t, ctx, pool)
				checks := rootBindingActivityChecks(t, ctx, pool, false)
				err := backupPublicationMigrate(ctx, pool, recovery)
				if err == nil || !recovery && !strings.Contains(err.Error(), "Backup publication activity constraint discovery found an unexpected catalog") {
					t.Fatalf("publication migration did not reject its unexpected terminal catalog: %v", err)
				}
				if migrationHistory(t, ctx, pool) != history || backupPublicationOldActivity(t, ctx, pool) != rows || rootBindingActivityChecks(t, ctx, pool, false) != checks {
					t.Fatal("rejected publication migration retained a partial CHECK, row or history change")
				}
				var absent bool
				if err := pool.QueryRow(ctx, `SELECT to_regclass('backup_policy') IS NULL
					AND to_regclass('backup_policy_runs') IS NULL AND to_regclass('backup_publication_receipts') IS NULL`).Scan(&absent); err != nil || !absent {
					t.Fatalf("rejected publication migration retained a new table: %v", err)
				}
				if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 70 {
					t.Fatalf("rejected publication migration changed version70: version=%d error=%v", version, err)
				}
			})
		}
	}
}
