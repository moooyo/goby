package activity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/database"
)

// Each fixture owns a random schema and never migrates or removes shared data.
func activityIntegrationPool(t *testing.T, migrate bool) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create activity test administration connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate activity test schema name: %v", err)
	}
	schema := "goby_activity_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create owned activity test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned activity test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse activity test database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated activity test pool")
	}
	t.Cleanup(pool.Close)
	var actualSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&actualSchema); err != nil || actualSchema != schema {
		t.Fatalf("activity test pool is not scoped to its owned schema: %v", err)
	}
	if migrate {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("migrate isolated activity test schema: %v", err)
		}
	}
	return ctx, pool
}

func activityTestEvent(resourceID string) activity.Event {
	return activity.Event{
		Action: activity.ActionUserUpdated, Severity: activity.SeverityInfo, Source: activity.SourceNative,
		Actor:     activity.Actor{Kind: activity.ActorUser, ID: "activity-user", CredentialID: "activity-auth"},
		Resource:  activity.Resource{Kind: activity.ResourceUser, ID: resourceID},
		RequestID: "activity-request", Revision: 2,
	}
}

func seedActivityUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, is_administrator)
		VALUES ('activity-user', 'Original Administrator', 'original administrator', 'synthetic-password-digest', true)`); err != nil {
		t.Fatalf("seed activity account: %v", err)
	}
}

func beginActivityTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin activity test transaction: %v", err)
	}
	t.Cleanup(func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	})
	return tx
}

func insertActivityEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, event activity.Event) (int64, time.Time) {
	t.Helper()
	tx := beginActivityTransaction(t, ctx, pool)
	if err := activity.Record(ctx, tx, event); err != nil {
		t.Fatalf("record activity fixture: %v", err)
	}
	var id int64
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `SELECT id, created_at FROM activity_entries
		WHERE resource_kind = $1 AND resource_id = $2 ORDER BY id DESC LIMIT 1`,
		event.Resource.Kind, event.Resource.ID).Scan(&id, &createdAt); err != nil {
		t.Fatalf("read inserted activity fixture: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit activity fixture: %v", err)
	}
	return id, createdAt
}

type activityOwnedExecutor struct {
	ctx context.Context
	tx  pgx.Tx
}

func (executor activityOwnedExecutor) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	return executor.tx.Exec(executor.ctx, statement, args...)
}

func assertActivityBusinessState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, revision, entries int64) {
	t.Helper()
	var actualName string
	var actualRevision, actualEntries int64
	if err := pool.QueryRow(ctx, `SELECT name, management_revision,
		(SELECT count(*) FROM activity_entries) FROM users WHERE id = 'activity-user'`).
		Scan(&actualName, &actualRevision, &actualEntries); err != nil {
		t.Fatalf("read business and activity state: %v", err)
	}
	if actualName != name || actualRevision != revision || actualEntries != entries {
		t.Errorf("business and activity state = (%q, %d, %d), want (%q, %d, %d)",
			actualName, actualRevision, actualEntries, name, revision, entries)
	}
}

func TestActivityRecordSharesBusinessTransaction(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	seedActivityUser(t, ctx, pool)
	rolledBack := beginActivityTransaction(t, ctx, pool)
	if _, err := rolledBack.Exec(ctx, `UPDATE users SET name = 'Rolled Back Administrator',
		management_revision = management_revision + 1 WHERE id = 'activity-user'`); err != nil {
		t.Fatalf("change business state before rollback: %v", err)
	}
	if err := activity.Record(ctx, rolledBack, activityTestEvent("activity-user")); err != nil {
		t.Fatalf("record activity before rollback: %v", err)
	}
	var pending int64
	if err := rolledBack.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("owner transaction activity count = %d, want 1: %v", pending, err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
	if err := rolledBack.Rollback(ctx); err != nil {
		t.Fatalf("roll back business and activity: %v", err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)

	committed := beginActivityTransaction(t, ctx, pool)
	if _, err := committed.Exec(ctx, `UPDATE users SET name = 'Committed Administrator',
		management_revision = management_revision + 1 WHERE id = 'activity-user'`); err != nil {
		t.Fatalf("change business state before commit: %v", err)
	}
	if err := activity.RecordOwned(activityOwnedExecutor{ctx: ctx, tx: committed}, activityTestEvent("activity-user")); err != nil {
		t.Fatalf("record activity through owner adapter: %v", err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
	if err := committed.Commit(ctx); err != nil {
		t.Fatalf("commit business and activity: %v", err)
	}
	assertActivityBusinessState(t, ctx, pool, "Committed Administrator", 2, 1)
}

func TestActivityRecordDatabaseFailureRollsBackBusinessWrite(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	seedActivityUser(t, ctx, pool)
	if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries
		ADD CONSTRAINT activity_test_reject_insert CHECK (false) NOT VALID`); err != nil {
		t.Fatalf("install owned activity insertion failure: %v", err)
	}
	tx := beginActivityTransaction(t, ctx, pool)
	if _, err := tx.Exec(ctx, `UPDATE users SET name = 'Must Not Commit',
		management_revision = management_revision + 1 WHERE id = 'activity-user'`); err != nil {
		t.Fatalf("change business state before failed audit: %v", err)
	}
	err := activity.Record(ctx, tx, activityTestEvent("activity-user"))
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("audit insertion error = %v, want PostgreSQL check violation", err)
	}
	if err := tx.Commit(ctx); !errors.Is(err, pgx.ErrTxCommitRollback) {
		t.Fatalf("failed audit transaction commit = %v, want ErrTxCommitRollback", err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
}

func TestActivityHistorySurvivesAccountAndCredentialDeletion(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	seedActivityUser(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, kind, expires_at)
		VALUES ('activity-auth', 'activity-user', decode(repeat('a1', 32), 'hex'), 'admin',
		clock_timestamp() + interval '1 day')`); err != nil {
		t.Fatalf("seed historical actor credential: %v", err)
	}
	id, _ := insertActivityEvent(t, ctx, pool, activityTestEvent("activity-user"))
	options := activity.QueryOptions{Limit: 10, ActorID: "activity-user"}
	initial := queryActivityPage(t, ctx, pool, options)
	if len(initial.Items) != 1 || initial.Items[0].ActorName != "Original Administrator" {
		t.Fatal("activity did not enrich its actor with the current account name")
	}
	var storedBefore, storedAfter string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(entry)::text FROM activity_entries entry WHERE id = $1", id).Scan(&storedBefore); err != nil {
		t.Fatalf("snapshot persisted activity before actor rename: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET name = 'Renamed Administrator' WHERE id = 'activity-user'"); err != nil {
		t.Fatalf("rename historical actor account: %v", err)
	}
	renamed := queryActivityPage(t, ctx, pool, options)
	if len(renamed.Items) != 1 || renamed.Items[0].ActorName != "Renamed Administrator" {
		t.Fatal("activity actor enrichment did not follow the current account name")
	}
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(entry)::text FROM activity_entries entry WHERE id = $1", id).Scan(&storedAfter); err != nil {
		t.Fatalf("read persisted activity after actor rename: %v", err)
	}
	if storedAfter != storedBefore {
		t.Error("renaming an actor changed the persisted activity row")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = 'activity-user'"); err != nil {
		t.Fatalf("delete historical actor account: %v", err)
	}
	var credentials int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = 'activity-auth'").Scan(&credentials); err != nil || credentials != 0 {
		t.Fatalf("deleted account credential count = %d, want 0: %v", credentials, err)
	}
	page := queryActivityPage(t, ctx, pool, options)
	if page.TotalRecordCount != 1 || len(page.Items) != 1 {
		t.Fatalf("activity history after account deletion has total %d and %d items, want 1 each", page.TotalRecordCount, len(page.Items))
	}
	entry := page.Items[0]
	if entry.ID != id || entry.Actor.Kind != activity.ActorUser || entry.Actor.ID != "activity-user" ||
		entry.Actor.CredentialID != "activity-auth" || entry.Resource.ID != "activity-user" {
		t.Error("historical actor, credential, or target identifiers changed after account deletion")
	}
	if entry.ActorName != "" {
		t.Error("deleted account still supplied a current actor name")
	}
}

func queryActivityPage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, options activity.QueryOptions) activity.Page {
	t.Helper()
	queries := 0
	page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row {
		queries++
		return pool.QueryRow(ctx, statement, args...)
	}, options)
	if err != nil {
		t.Fatalf("query activity page: %v", err)
	}
	if queries != 1 {
		t.Fatalf("activity page used %d queries, want a single page-and-count snapshot", queries)
	}
	return page
}

type activityAfterScanRow struct {
	row   activity.Row
	after func()
}

func (row activityAfterScanRow) Scan(dest ...any) error {
	if err := row.row.Scan(dest...); err != nil {
		return err
	}
	row.after()
	return nil
}

func TestActivityQuerySnapshotFiltersPaginationAndTimestampPrecision(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	boundary := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	type fixture struct {
		resource string
		at       time.Time
		action   activity.Action
		severity activity.Severity
		actor    string
	}
	fixtures := []fixture{
		{"before-boundary", boundary.Add(-time.Microsecond), activity.ActionUserUpdated, activity.SeverityWarning, "activity-user"},
		{"boundary-first", boundary, activity.ActionUserUpdated, activity.SeverityWarning, "activity-user"},
		{"boundary-second", boundary, activity.ActionUserUpdated, activity.SeverityWarning, "activity-user"},
		{"after-boundary", boundary.Add(time.Microsecond), activity.ActionUserUpdated, activity.SeverityWarning, "activity-user"},
		{"other-action", boundary.Add(time.Second), activity.ActionUserCreated, activity.SeverityWarning, "activity-user"},
		{"other-severity", boundary.Add(time.Second), activity.ActionUserUpdated, activity.SeverityInfo, "activity-user"},
		{"other-actor", boundary.Add(time.Second), activity.ActionUserUpdated, activity.SeverityWarning, "other-user"},
	}
	ids := make(map[string]int64, len(fixtures))
	for _, fixture := range fixtures {
		event := activityTestEvent(fixture.resource)
		event.Action, event.Severity, event.Actor.ID = fixture.action, fixture.severity, fixture.actor
		id, _ := insertActivityEvent(t, ctx, pool, event)
		ids[fixture.resource] = id
		if _, err := pool.Exec(ctx, "UPDATE activity_entries SET created_at = $2 WHERE id = $1", id, fixture.at); err != nil {
			t.Fatalf("set precise activity fixture date: %v", err)
		}
	}
	options := activity.QueryOptions{
		Limit: 1, MinDate: &boundary, Severity: activity.SeverityWarning,
		Action: activity.ActionUserUpdated, ActorID: "activity-user",
	}
	wantIDs := []int64{ids["after-boundary"], ids["boundary-second"], ids["boundary-first"]}
	wantDates := []time.Time{boundary.Add(time.Microsecond), boundary, boundary}
	for index, wantID := range wantIDs {
		options.StartIndex = index
		page := queryActivityPage(t, ctx, pool, options)
		if page.TotalRecordCount != 3 || len(page.Items) != 1 || page.StartIndex != index || page.Limit != 1 {
			t.Fatalf("page %d metadata = total %d, items %d, start %d, limit %d", index,
				page.TotalRecordCount, len(page.Items), page.StartIndex, page.Limit)
		}
		if page.Items[0].ID != wantID || !page.Items[0].Date.Equal(wantDates[index]) {
			t.Errorf("page %d entry = (%d, %s), want (%d, %s)", index, page.Items[0].ID,
				page.Items[0].Date.Format(time.RFC3339Nano), wantID, wantDates[index].Format(time.RFC3339Nano))
		}
		if page.Items[0].Date.Location() != time.UTC {
			t.Errorf("page %d date location = %s, want UTC despite the database session timezone", index, page.Items[0].Date.Location())
		}
	}
	options.StartIndex = 99
	if page := queryActivityPage(t, ctx, pool, options); page.TotalRecordCount != 3 || len(page.Items) != 0 {
		t.Fatalf("out-of-range page = total %d, items %d, want total 3 and no items", page.TotalRecordCount, len(page.Items))
	}
	betweenDatabaseInstants := boundary.Add(time.Nanosecond)
	options.StartIndex, options.MinDate = 0, &betweenDatabaseInstants
	between := queryActivityPage(t, ctx, pool, options)
	if between.TotalRecordCount != 1 || len(between.Items) != 1 || between.Items[0].ID != ids["after-boundary"] {
		t.Fatalf("sub-microsecond lower bound included an earlier database instant: total=%d items=%d",
			between.TotalRecordCount, len(between.Items))
	}
	options.MinDate = &boundary

	// A committed write between the row scan and any possible second query must
	// not make the returned total disagree with the already selected page.
	options.StartIndex = 0
	queries := 0
	inserted := false
	page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row {
		queries++
		return activityAfterScanRow{row: pool.QueryRow(ctx, statement, args...), after: func() {
			if inserted {
				return
			}
			inserted = true
			event := activityTestEvent("inserted-after-snapshot")
			event.Severity = activity.SeverityWarning
			id, _ := insertActivityEvent(t, ctx, pool, event)
			if _, err := pool.Exec(ctx, "UPDATE activity_entries SET created_at = $2 WHERE id = $1", id, boundary.Add(time.Second)); err != nil {
				t.Fatalf("date activity committed after the query snapshot: %v", err)
			}
		}}
	}, options)
	if err != nil {
		t.Fatalf("query activity while another write commits: %v", err)
	}
	if !inserted || queries != 1 || page.TotalRecordCount != 3 || len(page.Items) != 1 || page.Items[0].ID != wantIDs[0] {
		t.Fatalf("activity snapshot changed across a concurrent commit: inserted=%v queries=%d total=%d items=%d",
			inserted, queries, page.TotalRecordCount, len(page.Items))
	}
	if next := queryActivityPage(t, ctx, pool, options); next.TotalRecordCount != 4 {
		t.Errorf("subsequent activity snapshot total = %d, want 4", next.TotalRecordCount)
	}
}

// Apply the published migrations through schema 21 to exercise a real upgrade
// with existing identity rows rather than removing a new activity table.
func activityMigrationVersion21(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	directory := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read activity migration fixture inventory: %v", err)
	}
	names := make(map[int]string)
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || !ok || err != nil || version < 1 || version > 21 {
			continue
		}
		if names[version] != "" {
			t.Fatalf("duplicate historical migration version %d", version)
		}
		names[version] = entry.Name()
	}
	tx := beginActivityTransaction(t, ctx, pool)
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create historical activity migration ledger: %v", err)
	}
	for version := 1; version <= 21; version++ {
		name := names[version]
		if name == "" {
			t.Fatalf("historical migration version %d is missing", version)
		}
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("read historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", version, name); err != nil {
			t.Fatalf("record historical migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit historical activity migration fixture: %v", err)
	}
}

func TestActivityMigrationStartsEmptyAndDoesNotBackfillHistory(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade-with-existing-identities"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool := activityIntegrationPool(t, false)
			if upgrade {
				activityMigrationVersion21(t, ctx, pool)
				seedActivityUser(t, ctx, pool)
				if _, err := pool.Exec(ctx, `INSERT INTO sessions
					(id, user_id, token_hash, kind, created_at, expires_at, revoked_at)
					VALUES ('activity-auth', 'activity-user', decode(repeat('b2', 32), 'hex'), 'admin',
					'2020-01-01T00:00:00Z', '2020-02-01T00:00:00Z', '2020-01-02T00:00:00Z')`); err != nil {
					t.Fatalf("seed pre-activity authentication history: %v", err)
				}
			}
			for range 2 {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatalf("apply idempotent activity migration: %v", err)
				}
			}
			page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Limit: 10})
			if page.TotalRecordCount != 0 || len(page.Items) != 0 {
				t.Errorf("new activity history = total %d, items %d, want empty", page.TotalRecordCount, len(page.Items))
			}
			if upgrade {
				assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
				var sessions int64
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = 'activity-auth' AND revoked_at IS NOT NULL").Scan(&sessions); err != nil || sessions != 1 {
					t.Fatalf("pre-activity session history = %d rows, want 1: %v", sessions, err)
				}
			}
		})
	}
}

func TestActivityPruneUsesDatabaseDatesAndBoundedBatches(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	var before, after time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatalf("read database clock before activity insertion: %v", err)
	}
	templateID, createdAt := insertActivityEvent(t, ctx, pool, activityTestEvent("retained-current"))
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&after); err != nil {
		t.Fatalf("read database clock after activity insertion: %v", err)
	}
	if createdAt.Before(before) || createdAt.After(after) {
		t.Error("activity creation date is outside its database clock interval")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO activity_entries
		(created_at, action, severity, source, actor_kind, actor_id, actor_credential_id,
		resource_kind, resource_id, request_id, revision, affected_count, state, changed_fields)
		SELECT clock_timestamp() - interval '25 hours', action, severity, source, actor_kind, actor_id,
		actor_credential_id, resource_kind, 'expired-' || fixture.number::text, request_id,
		revision, affected_count, state, changed_fields
		FROM activity_entries CROSS JOIN generate_series(1, 1005) AS fixture(number)
		WHERE activity_entries.id = $1`, templateID); err != nil {
		t.Fatalf("seed expired activity batch from database clock: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO activity_entries
		(created_at, action, severity, source, actor_kind, actor_id, actor_credential_id,
		resource_kind, resource_id, request_id, revision, affected_count, state, changed_fields)
		SELECT clock_timestamp() + fixture.offset_hours * interval '1 hour', action, severity,
		source, actor_kind, actor_id, actor_credential_id, resource_kind, fixture.resource_id,
		request_id, revision, affected_count, state, changed_fields
		FROM activity_entries CROSS JOIN (VALUES (-23, 'retained-recent'), (1, 'retained-future'))
		AS fixture(offset_hours, resource_id) WHERE activity_entries.id = $1`, templateID); err != nil {
		t.Fatalf("seed retained activity rows from database clock: %v", err)
	}
	for index, want := range []int64{1000, 5, 0} {
		deleted, err := activity.PruneExpired(ctx, pool, 24*time.Hour)
		if err != nil || deleted != want {
			t.Fatalf("retention batch %d removed %d rows, want %d: %v", index, deleted, want, err)
		}
		var retained int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries WHERE resource_id LIKE 'retained-%'").Scan(&retained); err != nil || retained != 3 {
			t.Fatalf("retention batch %d preserved %d recent rows, want 3: %v", index, retained, err)
		}
	}
	var remaining int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&remaining); err != nil || remaining != 3 {
		t.Errorf("remaining activity rows = %d, want 3: %v", remaining, err)
	}
}
