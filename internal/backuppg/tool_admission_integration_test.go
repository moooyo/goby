//go:build linux

package backuppg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotSQLFactsDoNotLaunchPostgreSQLTools(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	marker := filepath.Join(t.TempDir(), "tool-launched")
	tool := commandFixture(t, "open("+pythonString(marker)+", 'w').close()\nprint('pg_dump (PostgreSQL) 16.11')")
	options.PGDump, options.PGRestore = tool, tool
	snapshot, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		t.Fatalf("SQL-only snapshot required an unused command: %v", err)
	}
	defer snapshot.Close()
	// The generic backup fixture also has an empty public schema. Recovery
	// deliberately rejects that outside namespace without consulting tools.
	if _, err := InspectRecoveryTransaction(snapshot.Context(), snapshot.Tx(), options.Schema); !errors.Is(err, ErrTarget) {
		t.Fatalf("SQL recovery inspection changed its dedicated-database boundary: %v", err)
	}
	if _, err := snapshot.Facts(ctx); err != nil {
		t.Fatalf("snapshot facts required an unused command: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("SQL-only recovery facts launched a PostgreSQL subprocess")
	}
	var output bytes.Buffer
	if err := snapshot.Dump(ctx, &output); !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("actual dump skipped its required version check: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("actual dump did not reach its selected executable")
	}
}
