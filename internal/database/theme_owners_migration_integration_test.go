package database_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

type themeOwnerQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func themeOwnersMigrateTo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version int64) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin explicit theme owner migration")
	}
	defer themeOwnersRollback(tx)
	if err := database.RecoveryMigrateTo(ctx, tx, version); err != nil {
		t.Fatalf("apply theme owner migration through version %d: %v", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit theme owner migration: %v", err)
	}
}

func themeOwnersRollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func themeOwnersVersion25Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	musicArtistsVersion24Baseline(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 25)
	// Historical item IDs are opaque text, including values that resemble a
	// root or another namespace's numeric ID. None receives an extra format rule.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('1','device-migration-library','Numeric item','numeric item','Movie',false),
		('virtual_root','device-migration-library','Physical root name','physical root name','MusicAlbum',true),
		(' Item / Exact ','device-migration-library','Exact opaque item','exact opaque item','Audio',false);
		UPDATE item_metadata_state SET music_source='{"Version":1,"Name":"Historical track","Album":"Historical album","Artists":["Artist A"],"AlbumArtists":["Ensemble B"]}'::jsonb
		WHERE item_id=' Item / Exact ';
		SELECT sync_catalog_item_entities(' Item / Exact ','{"Artists":["Artist A"],"AlbumArtists":["Ensemble B"]}'::jsonb)`); err != nil {
		t.Fatalf("seed actual schema 25 music and opaque item witnesses: %v", err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('theme_owner_ids') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("the published schema 25 baseline already contains theme owner storage")
	}
}

func themeOwnersTableSnapshot(t *testing.T, ctx context.Context, query themeOwnerQuery, table deviceLegacyTable) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ",") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version <= 25"
	}
	var result string
	if err := query.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM (`+statement+`) r`).Scan(&result); err != nil {
		t.Fatalf("capture exact old columns in %s: %v", table.name, err)
	}
	return result
}

func themeOwnersSequences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relkind='S' AND c.relname<>'theme_owner_ids_id_seq' ORDER BY c.relname`)
	if err != nil {
		t.Fatal("enumerate the original sequences")
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal("read an original sequence name")
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal("finish original sequence inventory")
	}
	result := make(map[string]string, len(names))
	for _, name := range names {
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('oid',$1::regclass::oid::bigint,
			'definition',(SELECT to_jsonb(s) FROM pg_sequence s WHERE s.seqrelid=$1::regclass),
			'value',jsonb_build_object('last_value',v.last_value,'log_cnt',v.log_cnt,'is_called',v.is_called))::text
			FROM `+pgx.Identifier{name}.Sanitize()+` v`, name).Scan(&value); err != nil {
			t.Fatalf("capture original sequence %s: %v", name, err)
		}
		result[name] = value
	}
	return result
}

func themeOwnersAssertComplete(t *testing.T, ctx context.Context, query themeOwnerQuery) {
	t.Helper()
	var roots, missing, orphaned, items, mappings, distinctIDs int
	if err := query.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL),
		(SELECT count(*) FROM items i LEFT JOIN theme_owner_ids o ON o.item_id=i.id WHERE o.id IS NULL),
		(SELECT count(*) FROM theme_owner_ids o LEFT JOIN items i ON i.id=o.item_id WHERE NOT o.virtual_root AND i.id IS NULL),
		(SELECT count(*) FROM items), (SELECT count(*) FROM theme_owner_ids),
		(SELECT count(DISTINCT id) FROM theme_owner_ids WHERE id>0)`).Scan(&roots, &missing, &orphaned, &items, &mappings, &distinctIDs); err != nil ||
		roots != 1 || missing != 0 || orphaned != 0 || mappings != items+1 || distinctIDs != mappings {
		t.Fatalf("theme owner coverage: roots=%d missing=%d orphaned=%d items=%d mappings=%d distinct=%d error=%v",
			roots, missing, orphaned, items, mappings, distinctIDs, err)
	}
}

func themeOwnersSnapshot(t *testing.T, ctx context.Context, query themeOwnerQuery) string {
	t.Helper()
	var snapshot string
	if err := query.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM theme_owner_ids o").Scan(&snapshot); err != nil {
		t.Fatal("snapshot persistent theme owner IDs")
	}
	return snapshot
}

func TestThemeOwnersMigrationPreservesSchema25RowsSequencesAndRollback(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if len(legacy) != 30 {
		t.Fatalf("theme owner baseline has %d tables, want the published 30", len(legacy))
	}
	for index := range legacy {
		legacy[index].snapshot = themeOwnersTableSnapshot(t, ctx, pool, legacy[index])
	}
	sequences := themeOwnersSequences(t, ctx, pool)
	if _, ok := sequences["catalog_entities_id_seq"]; !ok {
		t.Fatal("the preserved catalog entity sequence is absent from the witness")
	}
	var oldRelations string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(c.oid::bigint,c.relname,c.relkind,c.relowner::bigint)
		ORDER BY c.relname)::text FROM pg_class c WHERE c.relnamespace=current_schema()::regnamespace`).Scan(&oldRelations); err != nil {
		t.Fatal("snapshot all preexisting relation identities")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin rollback witness")
	}
	defer themeOwnersRollback(tx)
	if err := database.RecoveryMigrateTo(ctx, tx, 26); err != nil {
		t.Fatalf("migrate inside caller-owned rollback transaction: %v", err)
	}
	themeOwnersAssertComplete(t, ctx, tx)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("roll back new theme owner DDL and backfill")
	}
	var absent bool
	var rolledBackRelations string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('theme_owner_ids') IS NULL AND to_regclass('theme_owner_ids_id_seq') IS NULL
		AND to_regprocedure('assign_theme_owner_id()') IS NULL
		AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='items'::regclass AND tgname='items_assign_theme_owner_id')
		AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=26),
		(SELECT jsonb_agg(jsonb_build_array(c.oid::bigint,c.relname,c.relkind,c.relowner::bigint) ORDER BY c.relname)::text
		FROM pg_class c WHERE c.relnamespace=current_schema()::regnamespace)`).Scan(&absent, &rolledBackRelations); err != nil || !absent || rolledBackRelations != oldRelations {
		t.Fatal("rollback left new DDL/history or changed an original relation identity")
	}
	for attempt := 0; attempt < 3; attempt++ {
		// First inspect the rolled-back state, then commit and repeat migration.
		if attempt > 0 {
			themeOwnersMigrateTo(t, ctx, pool, 26)
		}
		for _, table := range legacy {
			if themeOwnersTableSnapshot(t, ctx, pool, table) != table.snapshot {
				t.Errorf("theme owner migration changed historical rows in %s", table.name)
			}
		}
		if !reflect.DeepEqual(themeOwnersSequences(t, ctx, pool), sequences) {
			t.Fatal("theme owner rollback, backfill, or repeat advanced an original sequence")
		}
	}
	themeOwnersAssertComplete(t, ctx, pool)
	owners, history := themeOwnersSnapshot(t, ctx, pool), migrationHistory(t, ctx, pool)
	var total, version int
	var sequenceIndependent bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()),
		(SELECT max(version) FROM schema_migrations),
		pg_get_serial_sequence('theme_owner_ids','id')::regclass <> pg_get_serial_sequence('catalog_entities','id')::regclass`).Scan(&total, &version, &sequenceIndependent); err != nil || total != 33 || version != 26 || !sequenceIndependent {
		t.Fatal("theme owner migration lacks its independent identity sequence or exact schema26 table")
	}
	themeOwnersMigrateTo(t, ctx, pool, 26)
	if themeOwnersSnapshot(t, ctx, pool) != owners || migrationHistory(t, ctx, pool) != history {
		t.Fatal("repeated schema26 migration rotated theme owner identities or history")
	}
}

const themeOwnerInsertItem = `INSERT INTO items(id,library_id,name,sort_name,type)
	VALUES($1,'device-migration-library','Theme fixture','theme fixture','Audio')`

func TestThemeOwnersEnforceKindsAndFollowItemTransactions(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 26)
	oldSequences := themeOwnersSequences(t, ctx, pool)
	for _, test := range []struct{ name, statement, code string }{
		{"missing kind", "INSERT INTO theme_owner_ids DEFAULT VALUES", "23514"},
		{"null root", "INSERT INTO theme_owner_ids(item_id,virtual_root) VALUES('1',NULL)", "23502"},
		{"root with item", "INSERT INTO theme_owner_ids(item_id,virtual_root) VALUES('1',true)", "23514"},
		{"duplicate root", "INSERT INTO theme_owner_ids(virtual_root) VALUES(true)", "23505"},
		{"duplicate canonical item", "INSERT INTO theme_owner_ids(item_id) VALUES('1')", "23505"},
		{"unknown item", "INSERT INTO theme_owner_ids(item_id) VALUES('missing-theme-item')", "23503"},
		{"zero owner", "UPDATE theme_owner_ids SET id=0 WHERE virtual_root", "428C9"},
		{"negative restored owner", "INSERT INTO theme_owner_ids(id,virtual_root) OVERRIDING SYSTEM VALUE VALUES(-1,true)", "23514"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, test.statement)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != test.code {
				t.Fatalf("theme owner constraint error=%v, want SQLSTATE %s", err, test.code)
			}
		})
	}
	owners := themeOwnersSnapshot(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin new item rollback witness")
	}
	defer themeOwnersRollback(tx)
	if _, err := tx.Exec(ctx, themeOwnerInsertItem, "rolled-back-theme-item"); err != nil {
		t.Fatal("insert the item and its theme owner in one transaction")
	}
	themeOwnersAssertComplete(t, ctx, tx)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("roll back the item and its assigned theme owner")
	}
	if themeOwnersSnapshot(t, ctx, pool) != owners {
		t.Fatal("item rollback retained an orphaned mapping or changed prior owners")
	}
	if _, err := pool.Exec(ctx, themeOwnerInsertItem, "theme-delete-recreate"); err != nil {
		t.Fatal("insert cascade witness")
	}
	var firstID, secondID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM theme_owner_ids WHERE item_id='theme-delete-recreate'").Scan(&firstID); err != nil {
		t.Fatal("new item has no same-transaction mapping")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM items WHERE id='theme-delete-recreate'"); err != nil {
		t.Fatal("delete one theme owner item")
	}
	if themeOwnersSnapshot(t, ctx, pool) != owners {
		t.Fatal("item deletion retained its mapping or changed another item/root owner")
	}
	if _, err := pool.Exec(ctx, themeOwnerInsertItem, "theme-delete-recreate"); err != nil {
		t.Fatal("recreate a previously deleted canonical item")
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM theme_owner_ids WHERE item_id='theme-delete-recreate'").Scan(&secondID); err != nil || secondID <= firstID {
		t.Fatal("a recreated item reused a retired theme owner identity")
	}
	if !reflect.DeepEqual(themeOwnersSequences(t, ctx, pool), oldSequences) {
		t.Fatal("ordinary theme allocation consumed a different namespace's sequence")
	}
	themeOwnersAssertComplete(t, ctx, pool)
	// Referential constraints do not prove total coverage after a restore that
	// disables triggers. Record that semantic gap without introducing read repair.
	if _, err := pool.Exec(ctx, "DELETE FROM theme_owner_ids WHERE virtual_root OR item_id='1'"); err != nil {
		t.Fatal("construct the explicit restore semantic-validation witness")
	}
	var roots, missing int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM theme_owner_ids WHERE virtual_root),
		(SELECT count(*) FROM items i LEFT JOIN theme_owner_ids o ON o.item_id=i.id WHERE o.id IS NULL)`).Scan(&roots, &missing); err != nil || roots != 0 || missing != 1 {
		t.Fatal("missing root/item coverage was not separately observable for restore validation")
	}
}

func themeOwnersWaitBlocked(t *testing.T, ctx context.Context, query themeOwnerQuery, blocked, blocker uint32) {
	t.Helper()
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var found bool
		if err := query.QueryRow(wait, "SELECT $2::integer=ANY(pg_blocking_pids($1::integer))", blocked, blocker).Scan(&found); err != nil {
			t.Fatalf("observe the exact theme owner lock dependency: %v", err)
		}
		if found {
			return
		}
		select {
		case <-wait.Done():
			t.Fatal("the expected theme owner lock dependency was not observed")
		case <-ticker.C:
		}
	}
}

func TestThemeOwnersMigrationCoversWritersOnBothSidesOfBackfill(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin pre-migration writer")
	}
	defer themeOwnersRollback(writer)
	if _, err := writer.Exec(ctx, themeOwnerInsertItem, "theme-before-backfill"); err != nil {
		t.Fatal("insert item before theme mapping exists")
	}
	migrating, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin concurrent migration")
	}
	defer themeOwnersRollback(migrating)
	migrationDone := make(chan error, 1)
	migrationPending := true
	migrationPID, writerPID := migrating.Conn().PgConn().PID(), writer.Conn().PgConn().PID()
	go func() { migrationDone <- database.RecoveryMigrateTo(work, migrating, 26) }()
	defer func() {
		cancel()
		if migrationPending {
			<-migrationDone
		}
	}()
	themeOwnersWaitBlocked(t, ctx, pool, migrationPID, writerPID)
	if err := writer.Commit(ctx); err != nil {
		t.Fatal("commit the pre-migration item")
	}
	err = <-migrationDone
	migrationPending = false
	if err != nil {
		t.Fatalf("complete theme backfill after the old writer commits: %v", err)
	}
	after, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquire writer behind the uncommitted migration")
	}
	defer after.Release()
	afterDone := make(chan error, 1)
	afterPending := true
	afterPID := after.Conn().PgConn().PID()
	go func() { _, err := after.Exec(work, themeOwnerInsertItem, "theme-after-backfill"); afterDone <- err }()
	defer func() {
		cancel()
		if afterPending {
			<-afterDone
		}
	}()
	themeOwnersWaitBlocked(t, ctx, pool, afterPID, migrationPID)
	if err := migrating.Commit(ctx); err != nil {
		t.Fatal("publish mapping storage, trigger, and backfill atomically")
	}
	err = <-afterDone
	afterPending = false
	if err != nil {
		t.Fatalf("the waiting new item did not acquire its trigger mapping: %v", err)
	}
	themeOwnersAssertComplete(t, ctx, pool)
}

func TestThemeOwnersConcurrentItemsAndDuplicatesKeepOneMapping(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersVersion25Baseline(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 26)
	const count = 6
	start, results := make(chan struct{}), make(chan error, count)
	for index := 0; index < count; index++ {
		go func(index int) {
			<-start
			_, err := pool.Exec(ctx, themeOwnerInsertItem, fmt.Sprintf("theme-concurrent-%d", index))
			results <- err
		}(index)
	}
	close(start)
	var firstError error
	for index := 0; index < count; index++ {
		if err := <-results; err != nil && firstError == nil {
			firstError = err
		}
	}
	if firstError != nil {
		t.Fatalf("concurrent independent item lost its owner: %v", firstError)
	}
	start, results = make(chan struct{}), make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			_, err := pool.Exec(ctx, themeOwnerInsertItem, "theme-concurrent-duplicate")
			results <- err
		}()
	}
	close(start)
	passed, duplicates := 0, 0
	for index := 0; index < 2; index++ {
		err := <-results
		var constraint *pgconn.PgError
		switch {
		case err == nil:
			passed++
		case errors.As(err, &constraint) && constraint.Code == "23505":
			duplicates++
		default:
			firstError = err
		}
	}
	if firstError != nil {
		t.Fatalf("unexpected concurrent canonical-item result: %v", firstError)
	}
	if passed != 1 || duplicates != 1 {
		t.Fatal("duplicate item insertion did not retain exactly one committed owner")
	}
	themeOwnersAssertComplete(t, ctx, pool)
}
