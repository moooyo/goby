package backuppg

import (
	"bytes"
	"context"
	"io"

	"github.com/moooyo/goby/internal/backupformat"
)

// ValidateDump checks a generated custom archive through the same bounded
// pg_restore decoder and COPY grammar used by Restore. It consumes archive from
// its current position without closing it. Callers must rewind before later
// packaging and must not publish a generated backup if this check fails.
//
// This preflight verifies encoded and expanded byte limits, row limits, complete
// table/sequence grammar, and snapshot row counts without opening a database.
// It is not a replacement for restoration's fingerprints, constraints, sequence
// consumer checks, trusted migrations, or application finalizer.
func ValidateDump(ctx context.Context, archive io.Reader, facts backupformat.SourceFacts, options Options) error {
	if archive == nil {
		return ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	options, err := normalizeOptions(options)
	if err != nil {
		return err
	}
	catalog, _, err := archiveCatalog(facts, options)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if err := checkToolVersions(ctx, options); err != nil {
		return err
	}
	expected := make(map[string]int64, len(facts.Tables))
	for _, table := range facts.Tables {
		expected[table.Name] = table.Rows
	}
	sink := &dumpValidationSink{expected: expected}
	return decodeCommand(ctx, options, archive, func(decoded io.Reader) error {
		return Decode(ctx, decoded, catalog, sink, DecodeOptions{MaxBytes: options.MaxDumpBytes})
	})
}

// Both preflight and restoration accept the same compiled catalog and source
// compatibility facts. No archive-provided name or SQL becomes a destination.
func archiveCatalog(facts backupformat.SourceFacts, options Options) (Catalog, []backupformat.MigrationFact, error) {
	if facts.DatabaseSchema != options.Schema || facts.PostgreSQLVersionNum/10000 != 17 || facts.ProbeVersion < 1 || facts.ProbeVersion > options.ProbeVersion {
		return Catalog{}, nil, ErrArchive
	}
	catalog, migrations, err := loadCatalog(facts.SchemaVersion, options.Schema)
	if err != nil {
		return Catalog{}, nil, err
	}
	if facts.SchemaSHA256 != catalog.SHA256 || !equalJSON(facts.MigrationChecksums, migrations) || len(facts.Tables) != len(catalog.Tables) {
		return Catalog{}, nil, ErrArchive
	}
	for index, table := range catalog.Tables {
		if facts.Tables[index].Name != table.Name || facts.Tables[index].Rows < 0 {
			return Catalog{}, nil, ErrArchive
		}
	}
	return catalog, migrations, nil
}

type dumpValidationSink struct {
	expected map[string]int64
}

func (sink *dumpValidationSink) Copy(ctx context.Context, table TableSpec, data io.Reader) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var counter dumpRowCounter
	if _, err := io.CopyBuffer(&counter, data, make([]byte, 64<<10)); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	expected, exists := sink.expected[table.Name]
	if !exists || counter.rows != expected {
		return 0, ErrArchive
	}
	return counter.rows, nil
}

func (*dumpValidationSink) SetSequence(ctx context.Context, _ SequenceSpec, _ int64, _ bool) error {
	// Decode already validates the trusted sequence identity and value range.
	// Consumer maxima require the restored database and remain Restore's job.
	return ctx.Err()
}

type dumpRowCounter struct {
	rows int64
}

func (counter *dumpRowCounter) Write(data []byte) (int, error) {
	// The shared decoder rejects raw embedded newlines and returns only bytes
	// from canonical COPY records, so each LF represents one validated row.
	counter.rows += int64(bytes.Count(data, []byte{'\n'}))
	return len(data), nil
}
