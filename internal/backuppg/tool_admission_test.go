//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDumpRequiresOnlyItsSelectedDecoder(t *testing.T) {
	facts, plaintext := validationDumpFixture(t, "decoder-only")
	archive := validationCompressedFixture(t, plaintext)
	options := validationToolOptions(t, 1<<20, 0)
	marker := filepath.Join(t.TempDir(), "unused-dump-launched")
	unused := commandFixture(t, "open("+pythonString(marker)+", 'w').close()\nprint('pg_dump (PostgreSQL) 16.11')")
	for _, dump := range []string{filepath.Join(t.TempDir(), "missing-pg-dump"), unused} {
		options.PGDump = dump
		if err := ValidateDump(context.Background(), bytes.NewReader(archive), facts, options); err != nil {
			t.Fatalf("decoder validation required an absent or incompatible dump tool: %v", err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dump validation executed the unused pg_dump tool")
	}
	options.PGRestore = commandFixture(t, "print('pg_restore (PostgreSQL) 16.11')")
	if err := ValidateDump(context.Background(), bytes.NewReader(archive), facts, options); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("dump validation skipped the required decoder version: %v", err)
	}
}

func TestSelectedPostgreSQLToolVersionDoesNotRequireItsPeer(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "unused-tool")
	dump := commandFixture(t, "print('pg_dump (PostgreSQL) 17.11')")
	restore := commandFixture(t, "print('pg_restore (PostgreSQL) 17.11')")
	if err := checkDumpVersion(context.Background(), Options{PGDump: dump, PGRestore: missing}); err != nil {
		t.Fatalf("dump admission required the unused decoder: %v", err)
	}
	if err := checkRestoreVersion(context.Background(), Options{PGDump: missing, PGRestore: restore}); err != nil {
		t.Fatalf("decoder admission required the unused dump tool: %v", err)
	}
}
