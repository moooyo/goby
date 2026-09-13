//go:build linux

package backuppg

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPostgreSQLGeneratedDumpRejectsUnsupportedExpansionBeforePublication(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `UPDATE users SET configuration=jsonb_build_object('Review',repeat('x',$1)) WHERE id='backup-admin'`, int(options.MaxDumpBytes)+(2<<20)); err != nil {
		t.Fatal("seed a valid highly compressible source row")
	}
	snapshot, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		t.Fatalf("open the bounded source snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatalf("fingerprint the source within its per-row limit: %v", err)
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "database.dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create an owned custom archive")
	}
	defer file.Close()
	if err := snapshot.Dump(ctx, file); err != nil {
		t.Fatalf("create the compressed custom archive: %v", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal("close the exported source snapshot")
	}
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() >= options.MaxDumpBytes {
		t.Fatal("the custom archive did not fit the compressed byte budget")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the generated archive")
	}
	if err := ValidateDump(ctx, file, facts, options); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized expansion was not rejected before publication: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the same archive for the restore counterexample")
	}
	if _, err := Restore(ctx, source, target, file, facts, options); !errors.Is(err, ErrLimit) {
		t.Fatalf("preflight and real restoration disagree on the budget: %v", err)
	}
	var objects int
	if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&objects) != nil || objects != 0 {
		t.Fatal("a rejected expanded archive left committed target objects")
	}

	// An explicitly larger supported budget must accept the same bytes, and
	// the corresponding real restore must preserve the source fingerprints.
	options.MaxDumpBytes = 16 << 20
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the archive for an explicit larger-budget preflight")
	}
	if err := ValidateDump(ctx, file, facts, options); err != nil {
		t.Fatalf("supported expansion was rejected by preflight: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the supported archive for restoration")
	}
	result, err := Restore(ctx, source, target, file, facts, options)
	if err != nil || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("supported preflight did not match a real restore: %v", err)
	}
}
