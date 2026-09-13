//go:build linux

package backuppg

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupformat"
)

func validationDumpFixture(t *testing.T, payload string) (backupformat.SourceFacts, string) {
	t.Helper()
	catalog, migrations, err := loadCatalog(28, "public")
	if err != nil {
		t.Fatal("load the trusted dump validation catalog")
	}
	facts := backupformat.SourceFacts{
		SchemaVersion: 28, SchemaSHA256: catalog.SHA256, MigrationChecksums: migrations,
		ProbeVersion: 6, PostgreSQLVersion: "17.11", PostgreSQLVersionNum: 170011,
		DatabaseSchema: "public", ServerID: "dump-validation-fixture",
	}
	var output strings.Builder
	output.WriteString(decodeTestHeader)
	for _, table := range catalog.Tables {
		columns := make([]string, len(table.Columns))
		for index, column := range table.Columns {
			columns[index] = `"` + column + `"`
		}
		fmt.Fprintf(&output, "--\n-- Data for Name: %s; Type: TABLE DATA; Schema: public; Owner: -\n--\n\nCOPY \"public\".\"%s\" (%s) FROM stdin;\n", table.Name, table.Name, strings.Join(columns, ", "))
		var rowCount int64
		if payload != "" && table.Name == "users" {
			values := make([]string, len(table.Columns))
			for index := range values {
				values[index] = `\N`
			}
			values[0] = payload
			output.WriteString(strings.Join(values, "\t"))
			output.WriteByte('\n')
			rowCount = 1
		}
		output.WriteString("\\.\n\n\n")
		facts.Tables = append(facts.Tables, backupformat.TableFact{Name: table.Name, Rows: rowCount, SHA256: strings.Repeat("0", 64)})
	}
	for _, sequence := range catalog.Sequences {
		fmt.Fprintf(&output, "--\n-- Name: %s; Type: SEQUENCE SET; Schema: public; Owner: -\n--\n\nSELECT pg_catalog.setval('\"public\".\"%s\"', %d, false);\n\n\n", sequence.Name, sequence.Name, sequence.MinValue)
	}
	output.WriteString(decodeTestFooter)
	return facts, output.String()
}

func validationCompressedFixture(t *testing.T, plaintext string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressor := zlib.NewWriter(&buffer)
	if _, err := io.WriteString(compressor, plaintext); err != nil {
		t.Fatal("compress the synthetic decoder fixture")
	}
	if err := compressor.Close(); err != nil {
		t.Fatal("finish the synthetic decoder fixture")
	}
	return buffer.Bytes()
}

func validationToolOptions(t *testing.T, limit int64, finalExit int) Options {
	t.Helper()
	dump := commandFixture(t, "print('pg_dump (PostgreSQL) 17.11')")
	restore := commandFixture(t, fmt.Sprintf("import sys, zlib\nif sys.argv[1:] == ['--version']:\n print('pg_restore (PostgreSQL) 17.11')\nelse:\n sys.stdout.buffer.write(zlib.decompress(sys.stdin.buffer.read()))\n sys.stdout.buffer.flush()\n sys.exit(%d)", finalExit))
	return Options{PGDump: dump, PGRestore: restore, Schema: "public", Timeout: 10 * time.Second, MaxDumpBytes: limit, ProbeVersion: 6}
}

func TestValidateDumpUsesRestorationExpandedByteBoundary(t *testing.T) {
	facts, plaintext := validationDumpFixture(t, strings.Repeat("x", 128<<10))
	compressed := validationCompressedFixture(t, plaintext)
	if len(compressed) >= len(plaintext)/2 {
		t.Fatal("the fixture did not cross the compressed/expanded size boundary")
	}
	for _, test := range []struct {
		name  string
		limit int64
		want  error
	}{
		{name: "exact_expanded_size", limit: int64(len(plaintext))},
		{name: "one_byte_below_expanded_size", limit: int64(len(plaintext) - 1), want: ErrLimit},
		{name: "compressed_size_only", limit: int64(len(compressed)), want: ErrLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := validationToolOptions(t, test.limit, 0)
			err := ValidateDump(t.Context(), bytes.NewReader(compressed), facts, options)
			if !errors.Is(err, test.want) {
				t.Fatalf("generated dump preflight = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateDumpRejectsIncompleteGrammarCountsAndChildFailure(t *testing.T) {
	facts, plaintext := validationDumpFixture(t, "synthetic-row")
	for _, test := range []struct {
		name      string
		plaintext string
		wrongRows bool
		exit      int
		want      error
	}{
		{name: "unknown_sql", plaintext: strings.Replace(plaintext, "SET statement_timeout = 0;", "DROP TABLE users;", 1), want: ErrArchive},
		{name: "missing_footer", plaintext: strings.TrimSuffix(plaintext, decodeTestFooter), want: ErrArchive},
		{name: "wrong_snapshot_row_count", plaintext: plaintext, wrongRows: true, want: ErrArchive},
		{name: "child_fails_after_valid_eof", plaintext: plaintext, exit: 7, want: ErrCommand},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := cloneFacts(facts)
			if test.wrongRows {
				for index := range copy.Tables {
					if copy.Tables[index].Name == "users" {
						copy.Tables[index].Rows++
					}
				}
			}
			options := validationToolOptions(t, int64(len(plaintext)+1024), test.exit)
			err := ValidateDump(t.Context(), bytes.NewReader(validationCompressedFixture(t, test.plaintext)), copy, options)
			if !errors.Is(err, test.want) {
				t.Fatalf("invalid generated dump preflight = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateDumpUsesRestorationRowLimit(t *testing.T) {
	facts, plaintext := validationDumpFixture(t, strings.Repeat("x", int(defaultDecodeMaxRowBytes)))
	options := validationToolOptions(t, int64(len(plaintext)+1024), 0)
	if err := ValidateDump(t.Context(), bytes.NewReader(validationCompressedFixture(t, plaintext)), facts, options); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized COPY row passed generated dump preflight: %v", err)
	}
}

func TestValidateDumpPreservesOwnedFileAndRejectsPrecancelledWork(t *testing.T) {
	facts, plaintext := validationDumpFixture(t, "synthetic-row")
	compressed := validationCompressedFixture(t, plaintext)
	options := validationToolOptions(t, int64(len(plaintext)), 0)
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "private-dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create the owned validation input")
	}
	defer file.Close()
	prefix := []byte("outside-the-current-input-position")
	if _, err := file.Write(append(prefix, compressed...)); err != nil {
		t.Fatal("write the bounded validation input")
	}
	if _, err := file.Seek(int64(len(prefix)), io.SeekStart); err != nil {
		t.Fatal("position the validation input")
	}
	if err := ValidateDump(t.Context(), file, facts, options); err != nil {
		t.Fatalf("validate from the caller's current file position: %v", err)
	}
	if _, err := file.Seek(int64(len(prefix)), io.SeekStart); err != nil {
		t.Fatal("validation closed the caller-owned file")
	}
	actual, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(actual, compressed) {
		t.Fatal("validation changed the private archive bytes")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ValidateDump(ctx, &commandNeverReader{}, facts, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled validation = %v", err)
	}
}

func TestValidateDumpRejectsUnsupportedFactsBeforeCommands(t *testing.T) {
	facts, _ := validationDumpFixture(t, "")
	options := Options{PGDump: "/must-not-run-pg-dump", PGRestore: "/must-not-run-pg-restore", Schema: "public", Timeout: time.Second, MaxDumpBytes: 1 << 20, ProbeVersion: 6}
	for name, corrupt := range map[string]func(*backupformat.SourceFacts){
		"schema": func(value *backupformat.SourceFacts) { value.DatabaseSchema = "foreign" },
		"migration": func(value *backupformat.SourceFacts) {
			value.MigrationChecksums[0].SHA256 = strings.Repeat("0", 64)
		},
		"table_inventory": func(value *backupformat.SourceFacts) { value.Tables = value.Tables[1:] },
		"row_count":       func(value *backupformat.SourceFacts) { value.Tables[0].Rows = -1 },
		"future_probe":    func(value *backupformat.SourceFacts) { value.ProbeVersion++ },
	} {
		t.Run(name, func(t *testing.T) {
			copy := cloneFacts(facts)
			corrupt(&copy)
			if err := ValidateDump(t.Context(), &commandNeverReader{}, copy, options); !errors.Is(err, ErrArchive) {
				t.Fatalf("invalid source facts reached decoder admission: %v", err)
			}
		})
	}
}
