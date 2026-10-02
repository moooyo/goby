//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
)

// The release operator explicitly supplies a new private artifact path. The
// catalog comes from PostgreSQL 17's actual freshly migrated owned database,
// not an edited historical catalog or a skipped recovery authority check.
func TestGenerationStartupExportSchema55TrustedCatalog(t *testing.T) {
	output := os.Getenv("GOBY_GENERATION_STARTUP_CATALOG_OUTPUT")
	if output == "" {
		t.Skip("GOBY_GENERATION_STARTUP_CATALOG_OUTPUT explicitly selects a new private schema55 artifact")
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || filepath.Base(output) != "schema-55-postgresql-17.json" {
		t.Fatal("catalog export requires a canonical absolute new schema55 artifact path")
	}
	f := newGenerationStartupFixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	if err := f.verifyDatabase(ctx, f.primary); err != nil {
		t.Fatal("refuse to export from an unverified catalog generation database")
	}
	pool, err := database.Open(ctx, f.primary.uri)
	if err != nil {
		t.Fatal("open the exact new catalog generation database")
	}
	t.Cleanup(pool.Close)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin the catalog generation emptiness proof")
	}
	_, inspectionErr := backuppg.InspectEmptyRecoveryTransaction(ctx, tx, "public")
	rollbackErr := tx.Rollback(ctx)
	if inspectionErr != nil || rollbackErr != nil {
		t.Fatalf("catalog generation database is not an independently owned empty public schema: failure={%s}", generationStartupFailure(inspectionErr))
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("apply only the complete trusted catalog migration prefix: failure={%s}", generationStartupFailure(err))
	}
	migrations, err := database.EmbeddedMigrations()
	if err != nil || len(migrations) != 55 || migrations[54].Version != 55 || migrations[54].Name != "0055_playback_demand_revision.sql" {
		t.Fatal("the schema55 export does not match its exact compiled migration source")
	}
	data, err := backuppg.ExportCatalog(ctx, pool, "public", 55)
	if err != nil {
		t.Fatalf("export the actual PostgreSQL17 schema55 catalog: failure={%s}", generationStartupFailure(err))
	}
	data = append(data, '\n')
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create the explicitly selected new private catalog artifact")
	}
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("persist the actual catalog artifact")
	}
	digest := sha256.Sum256(data)
	t.Logf("schema55_catalog_export postgres_major=17 schema_version=55 migration_count=%d migration55_sha256=%s catalog_artifact_sha256=%s artifact_bytes=%d database_oid_verified=true role_owner_verified=true empty_before_migration=true",
		len(migrations), migrations[54].SHA256, hex.EncodeToString(digest[:]), len(data))
}
