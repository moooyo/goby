//go:build ignore

// This release helper is executed only in a newly created, disposable database.
// The surrounding operator proves ownership and retains the source manifest.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
)

func main() {
	if len(os.Args) != 2 || os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		fail("invalid catalog generation invocation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"))
	if err != nil {
		fail("open disposable catalog database")
	}
	defer pool.Close()
	var tables int
	if pool.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'`).Scan(&tables) != nil || tables != 0 {
		fail("catalog database is not empty")
	}
	if err := database.Migrate(ctx, pool); err != nil {
		fail("apply trusted catalog migrations")
	}
	migrations, err := database.EmbeddedMigrations()
	if err != nil || len(migrations) == 0 {
		fail("read trusted migration metadata")
	}
	version := migrations[len(migrations)-1].Version
	data, err := backuppg.ExportCatalog(ctx, pool, "public", version)
	if err != nil {
		var diagnostic string
		if pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('name',c.relname,'columns',(SELECT count(*) FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attgenerated=''),'primary_keys',(SELECT count(*) FROM pg_index i WHERE i.indrelid=c.oid AND i.indisprimary)) ORDER BY c.relname)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'`).Scan(&diagnostic) == nil {
			fmt.Println(diagnostic)
		}
		fail("export trusted catalog: " + err.Error())
	}
	file, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail("create catalog artifact")
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		fail("write catalog artifact")
	}
	if file.Sync() != nil || file.Close() != nil {
		fail("persist catalog artifact")
	}
	fmt.Printf("Generated trusted schema %d catalog.\n", version)
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
