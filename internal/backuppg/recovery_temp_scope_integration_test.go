//go:build linux

package backuppg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This pre-migration boundary needs the complete disposable source database's
// public schema, rather than recoveryFixture's additional random schema. No
// existing relation is changed; temporary work ends with its physical session
// and each deliberately foreign schema is rolled back with its transaction.
func TestRecoveryBindingIgnoresOtherSessionTemporaryRelations(t *testing.T) {
	rawURL := os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL")
	if rawURL == "" {
		t.Skip("a disposable backup source database is required")
	}
	if os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		t.Fatal("explicit disposable database marker is required")
	}
	configuration, err := pgxpool.ParseConfig(rawURL)
	if err != nil || !strings.HasPrefix(configuration.ConnConfig.Database, "goby_backup_") {
		t.Fatal("the recovery scope fixture requires an owned goby_backup_ database")
	}
	configuration.MaxConns = 3
	configuration.ConnConfig.RuntimeParams["search_path"] = "public"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	pool := openBackupFixturePool(t, ctx, configuration, rawURL)
	temporary, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquire the independent temporary-table session")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Release alone would preserve TEMP objects on an idle pooled session.
		if err := temporary.Conn().Close(cleanup); err != nil {
			t.Errorf("close the temporary-table session: %v", err)
		}
		temporary.Release()
	})
	inspect := func() {
		t.Helper()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if _, err := InspectBindingTransaction(ctx, tx, "public"); err != nil {
			t.Fatalf("inspect the owned public database with independent temporary work: %v", err)
		}
	}
	inspect()
	if _, err := temporary.Exec(ctx, `CREATE TEMP TABLE recovery_scope_temporary (
		id integer PRIMARY KEY, payload text NOT NULL);
		INSERT INTO recovery_scope_temporary
		SELECT 1,string_agg(md5(n::text),'') FROM generate_series(1,4096) n`); err != nil {
		t.Fatalf("create real temporary table, index, and toasted payload: %v", err)
	}
	var tableOID uint32
	var before string
	if err := temporary.QueryRow(ctx, `SELECT 'pg_temp.recovery_scope_temporary'::regclass::oid,
		md5(payload) FROM recovery_scope_temporary WHERE id=1`).Scan(&tableOID, &before); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `WITH owned AS (
		SELECT oid,reltoastrelid FROM pg_catalog.pg_class WHERE oid=$1
	) SELECT c.relkind::text,c.relpersistence::text,pg_catalog.pg_is_other_temp_schema(n.oid)
	FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
	WHERE c.oid IN (SELECT oid FROM owned UNION ALL SELECT reltoastrelid FROM owned)
	OR c.oid IN (SELECT i.indexrelid FROM pg_catalog.pg_index i,owned o
		WHERE i.indrelid=o.oid OR i.indrelid=o.reltoastrelid)`, tableOID)
	if err != nil {
		t.Fatal(err)
	}
	var tables, indexes, toasts int
	for rows.Next() {
		var kind, persistence string
		var otherTemporary bool
		if err := rows.Scan(&kind, &persistence, &otherTemporary); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if persistence != "t" || !otherTemporary {
			rows.Close()
			t.Fatal("fixture relation is not a native other-session temporary object")
		}
		switch kind {
		case "r":
			tables++
		case "i":
			indexes++
		case "t":
			toasts++
		default:
			rows.Close()
			t.Fatalf("unexpected temporary relation kind: %s", kind)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || tables != 1 || indexes != 2 || toasts != 1 {
		t.Fatalf("temporary inventory table/index/toast=%d/%d/%d: %v", tables, indexes, toasts, err)
	}
	inspect()
	for _, kind := range []string{"permanent", "unlogged"} {
		t.Run(kind+" foreign relation remains rejected", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			name := pgx.Identifier{fmt.Sprintf("goby_recovery_scope_%d", temporary.Conn().PgConn().PID())}.Sanitize()
			if _, err := tx.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
				t.Fatal(err)
			}
			persistence, qualifier := "p", ""
			if kind == "unlogged" {
				persistence, qualifier = "u", "UNLOGGED "
			}
			if _, err := tx.Exec(ctx, "CREATE "+qualifier+"TABLE "+name+".foreign_relation (id integer)"); err != nil {
				t.Fatal(err)
			}
			var actualPersistence string
			var otherTemporary bool
			if err := tx.QueryRow(ctx, `SELECT c.relpersistence::text,pg_catalog.pg_is_other_temp_schema(n.oid)
				FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
				WHERE c.oid=($1||'.foreign_relation')::regclass`, name).Scan(&actualPersistence, &otherTemporary); err != nil ||
				actualPersistence != persistence || otherTemporary {
				t.Fatalf("foreign relation fixture has unexpected persistence or namespace: %v", err)
			}
			if _, err := InspectBindingTransaction(ctx, tx, "public"); !errors.Is(err, ErrTarget) {
				t.Fatalf("foreign %s relation was accepted beside temporary work: %v", kind, err)
			}
		})
	}
	inspect()
	var after string
	if err := temporary.QueryRow(ctx, `SELECT md5(payload) FROM recovery_scope_temporary WHERE id=1`).Scan(&after); err != nil || after != before {
		t.Fatalf("scope inspection changed the other session's temporary payload: %v", err)
	}
	t.Logf("recovery_temp_scope accepted_table=%d accepted_indexes=%d accepted_toast=%d foreign_permanent_rejected=true foreign_unlogged_rejected=true", tables, indexes, toasts)
}
