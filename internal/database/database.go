// Package database configures PostgreSQL connections and applies schema migrations.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 4919415424202458190

const maxMigrationDuration = 30 * time.Minute

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Open creates a bounded pool and verifies that PostgreSQL is reachable.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("database URL is required")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		// Do not include the URL or parser error, which may contain credentials.
		return nil, errors.New("invalid database connection configuration")
	}
	config.MaxConns = 16
	config.MinConns = 1
	config.MaxConnLifetime = time.Hour
	config.MaxConnLifetimeJitter = 5 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = time.Minute
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	config.ConnConfig.RuntimeParams["application_name"] = "goby"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	config.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

type migration struct {
	version int64
	name    string
	sql     string
}

func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	result := make([]migration, 0, len(entries))
	seen := make(map[int64]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if !ok || err != nil || version < 1 || seen[version] {
			return nil, fmt.Errorf("invalid migration filename: %s", entry.Name())
		}
		seen[version] = true
		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		result = append(result, migration{version: version, name: entry.Name(), sql: string(content)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}

// Migrate atomically applies embedded migrations under a PostgreSQL advisory lock.
// Concurrent server startups serialize before inspecting or modifying the schema.
// The caller's deadline is capped at 30 minutes, including lock acquisition.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is required")
	}
	ctx, cancel := context.WithTimeout(ctx, maxMigrationDuration)
	defer cancel()
	available, err := migrations()
	if err != nil {
		return err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer rollback(tx)
	// Data backfills may exceed the ordinary pool statement timeout. SET LOCAL
	// limits this exception to the transaction; the context still bounds every
	// query and lock wait, and COMMIT or ROLLBACK restores connection settings.
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = 0"); err != nil {
		return fmt.Errorf("configure migration transaction timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("lock schema migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version bigint PRIMARY KEY,
		name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration history: %w", err)
		}
		applied[version] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	known := make(map[int64]bool, len(available))
	for _, item := range available {
		known[item.version] = true
		if name, ok := applied[item.version]; ok && name != item.name {
			return fmt.Errorf("migration %d has an unexpected filename", item.version)
		}
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("database migration %d is unsupported by this server", version)
		}
	}
	for _, item := range available {
		if _, ok := applied[item.version]; ok {
			continue
		}
		if _, err := tx.Exec(ctx, item.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", item.version, item.name); err != nil {
			return fmt.Errorf("record migration %s: %w", item.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema migrations: %w", err)
	}
	return nil
}

// SchemaVersion returns the highest applied migration for readiness checks.
func SchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var version int64
	if err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
