package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

// RecoveryMigration identifies compiled migration bytes. Historical migration
// rows record filenames, not checksums; these digests must be retained in the
// backup manifest and independently checked by the restore engine.
type RecoveryMigration struct {
	Version int64
	Name    string
	SHA256  string
}

func EmbeddedMigrations() ([]RecoveryMigration, error) {
	available, err := migrations()
	if err != nil {
		return nil, err
	}
	result := make([]RecoveryMigration, len(available))
	for i, item := range available {
		digest := sha256.Sum256([]byte(item.sql))
		result[i] = RecoveryMigration{Version: item.version, Name: item.name, SHA256: hex.EncodeToString(digest[:])}
	}
	return result, nil
}

// RecoveryMigrateTo applies only embedded SQL, up to an explicitly supported
// version, inside a caller-owned transaction. It never commits or rolls back.
// Callers must establish an independent restore target and a trusted search
// path before calling. Existing history must be an exact, contiguous prefix.
func RecoveryMigrateTo(ctx context.Context, tx pgx.Tx, version int64) error {
	if tx == nil {
		return errors.New("recovery migration transaction is required")
	}
	available, err := migrations()
	if err != nil || len(available) == 0 || version < 1 {
		return errors.New("unsupported recovery migration version")
	}
	found := false
	for _, item := range available {
		if item.version == version {
			found = true
		}
	}
	if !found {
		return errors.New("unsupported recovery migration version")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return errors.New("lock recovery migrations")
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return errors.New("create recovery migration history")
	}
	rows, err := tx.Query(ctx, `SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return errors.New("read recovery migration history")
	}
	applied := 0
	for rows.Next() {
		var v int64
		var name string
		if rows.Scan(&v, &name) != nil || applied >= len(available) || available[applied].version != v || available[applied].name != name || v > version {
			rows.Close()
			return errors.New("invalid recovery migration history")
		}
		applied++
	}
	rows.Close()
	if rows.Err() != nil {
		return errors.New("read recovery migration history")
	}
	for _, item := range available[applied:] {
		if item.version > version {
			break
		}
		if _, err := tx.Exec(ctx, item.sql); err != nil {
			return errors.New("apply trusted recovery migration")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version,name) VALUES($1,$2)`, item.version, item.name); err != nil {
			return errors.New("record trusted recovery migration")
		}
	}
	return nil
}
