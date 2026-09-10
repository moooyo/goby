// Package backuppg exports and restores PostgreSQL data without executing SQL
// supplied by a backup archive. Only embedded migrations and synthesized,
// allowlisted COPY statements can modify the separately configured target.
package backuppg

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backupformat"
)

var (
	ErrConfiguration = errors.New("invalid PostgreSQL backup configuration")
	ErrDatabase      = errors.New("PostgreSQL backup database operation failed")
	ErrArchive       = errors.New("invalid PostgreSQL data archive")
	ErrLimit         = errors.New("PostgreSQL backup limit exceeded")
	ErrSchema        = errors.New("PostgreSQL backup schema does not match trusted migrations")
	ErrTarget        = errors.New("PostgreSQL restore target is not an independent empty database")
	ErrCommand       = errors.New("PostgreSQL backup command failed")
	ErrUnsupported   = errors.New("unsupported PostgreSQL backup environment")
)

// Catalog is derived from verified fresh databases built with embedded
// migrations. It must never be populated from untrusted archive SQL.
type Catalog struct {
	Schema      string
	Tables      []TableSpec
	Sequences   []SequenceSpec
	SHA256      string
	Constraints []ConstraintSpec
}

type TableSpec struct {
	Name       string
	Columns    []string
	PrimaryKey []string
	SortKey    []string
}

type SequenceSpec struct {
	Name      string
	Table     string
	Column    string
	MinValue  int64
	MaxValue  int64
	Increment int64
	Consumers []SequenceColumn
}

type SequenceColumn struct {
	Table  string
	Column string
}

type ConstraintSpec struct{ Table, Name, Definition string }

// CopySink accepts only decoded data. Implementations must consume all data
// synchronously before returning and must never interpret the bytes as SQL.
type CopySink interface {
	Copy(context.Context, TableSpec, io.Reader) (int64, error)
	SetSequence(context.Context, SequenceSpec, int64, bool) error
}

// Options contains administrator-provided executable paths and connection
// configuration. URLs never appear in errors or command lines. Unsupported
// libpq URI options are rejected rather than silently weakening TLS settings.
// SourceURL is trusted deployment configuration, never archive or HTTP input.
type Options struct {
	SourceURL         string
	PGDump            string
	PGRestore         string
	Schema            string
	Timeout           time.Duration
	MaxDumpBytes      int64
	ProbeVersion      int64
	sourceHostAddress string
}

type snapshotSource struct {
	options  Options
	catalog  Catalog
	version  int64
	identity databaseIdentity
}

// Snapshot owns one bounded, repeatable-read transaction. Tx is available for
// additional source witnesses that must share the dump snapshot. Callers must
// serialize its use and must not commit, roll back, or execute writes in Tx.
type Snapshot struct {
	plan     *snapshotSource
	tx       pgx.Tx
	ctx      context.Context
	cancel   context.CancelFunc
	exported string
	facts    *backupformat.SourceFacts
	closed   bool
}

type RestoreResult struct {
	SourceVersion  int64
	CurrentVersion int64
	Tables         []backupformat.TableFact
}

// Finalizer is trusted application code executed after raw restoration has
// been verified and before its transaction commits. It must not commit, roll
// back, retain the transaction, or be derived from archive or HTTP content.
// Any returned error or cancelled operation rolls back the entire new target.
type Finalizer func(context.Context, pgx.Tx, RestoreResult) error
