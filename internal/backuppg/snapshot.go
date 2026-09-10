package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
)

func normalizeOptions(options Options) (Options, error) {
	if options.Schema == "" {
		options.Schema = "public"
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.MaxDumpBytes == 0 {
		options.MaxDumpBytes = 16 << 30
	}
	if !identifierPattern.MatchString(options.Schema) || options.Timeout < time.Second || options.Timeout > 30*time.Minute || options.MaxDumpBytes < 1 || options.MaxDumpBytes > 64<<30 || options.ProbeVersion < 1 {
		return Options{}, ErrConfiguration
	}
	return options, nil
}

// OpenSnapshot validates the live schema against a compiled baseline and
// exports one read-only, repeatable-read snapshot. No staging database is
// needed and no source DDL or owner write transaction is acquired.
func OpenSnapshot(ctx context.Context, pool *pgxpool.Pool, options Options) (*Snapshot, error) {
	if pool == nil {
		return nil, ErrConfiguration
	}
	options, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	if options.SourceURL == "" {
		options.SourceURL = pool.Config().ConnString()
	}
	// Preserve the complete original connection description rather than trying
	// to silently translate unsupported pool, failover, or TLS settings.
	if options.SourceURL != pool.Config().ConnString() {
		return nil, ErrConfiguration
	}
	connection, err := sourceCommandConfig(options)
	if err != nil {
		return nil, err
	}
	poolConfig := pool.Config().ConnConfig
	if err := validateSourceTLS(options.SourceURL, poolConfig); err != nil {
		return nil, err
	}
	if connection["PGHOST"] != poolConfig.Host || connection["PGPORT"] != strconv.Itoa(int(poolConfig.Port)) || connection["PGDATABASE"] != poolConfig.Database || connection["PGUSER"] != poolConfig.User || connection["PGPASSWORD"] != poolConfig.Password {
		return nil, ErrConfiguration
	}
	jobCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	if err := checkToolVersions(jobCtx, options); err != nil {
		cancel()
		return nil, err
	}
	tx, err := pool.BeginTx(jobCtx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		cancel()
		return nil, ErrDatabase
	}
	failed := true
	defer func() {
		if failed {
			rollback(tx)
			cancel()
		}
	}()
	if err := configureTransaction(jobCtx, tx, options.Schema); err != nil {
		return nil, err
	}
	identity, err := readIdentity(jobCtx, tx, options.Schema)
	if err != nil {
		return nil, err
	}
	if identity.User != pool.Config().ConnConfig.User || identity.Database != pool.Config().ConnConfig.Database {
		return nil, ErrConfiguration
	}
	address, ok := tx.Conn().PgConn().Conn().RemoteAddr().(*net.TCPAddr)
	if !ok || address.IP == nil || address.Port != int(poolConfig.Port) {
		return nil, ErrUnsupported
	}
	options.sourceHostAddress = address.IP.String()
	var version int64
	if tx.QueryRow(jobCtx, `SELECT COALESCE(max(version),0) FROM `+qualified(options.Schema, "schema_migrations")).Scan(&version) != nil {
		return nil, ErrSchema
	}
	catalog, migrations, err := loadCatalog(version, options.Schema)
	if err != nil {
		return nil, err
	}
	if err := validateHistory(jobCtx, tx, options.Schema, migrations); err != nil {
		return nil, err
	}
	actual, _, err := inspectCatalog(jobCtx, tx, options.Schema)
	if err != nil || !equalJSON(catalog, actual) {
		return nil, ErrSchema
	}
	if err := validateOwnership(jobCtx, tx, options.Schema); err != nil {
		return nil, err
	}
	var exported string
	if tx.QueryRow(jobCtx, `SELECT pg_catalog.pg_export_snapshot()`).Scan(&exported) != nil {
		return nil, ErrDatabase
	}
	snapshot := &Snapshot{plan: &snapshotSource{options: options, catalog: catalog, version: version, identity: identity}, tx: tx, ctx: jobCtx, cancel: cancel, exported: exported}
	failed = false
	return snapshot, nil
}

// Context supplies the deadline for additional source witnesses made through
// Tx. PostgreSQL sequences are non-MVCC and must not be described as snapshot
// facts; their later pg_dump setval values are validated during restoration.
func (snapshot *Snapshot) Context() context.Context { return snapshot.ctx }
func (snapshot *Snapshot) Tx() pgx.Tx               { return snapshot.tx }

func (snapshot *Snapshot) Close() error {
	if snapshot == nil || snapshot.closed {
		return nil
	}
	snapshot.closed = true
	rollback(snapshot.tx)
	snapshot.cancel()
	return nil
}

func (snapshot *Snapshot) Facts(ctx context.Context) (backupformat.SourceFacts, error) {
	if snapshot == nil || snapshot.closed {
		return backupformat.SourceFacts{}, ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return backupformat.SourceFacts{}, err
	}
	ctx, cancel := linkedContext(ctx, snapshot.ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return backupformat.SourceFacts{}, err
	}
	if snapshot.facts != nil {
		return cloneFacts(*snapshot.facts), nil
	}
	migrations, err := compiledMigrations(snapshot.plan.version)
	if err != nil {
		return backupformat.SourceFacts{}, err
	}
	tables, err := fingerprints(ctx, snapshot.tx, snapshot.plan.catalog)
	if err != nil {
		return backupformat.SourceFacts{}, err
	}
	var serverID string
	if snapshot.tx.QueryRow(ctx, `SELECT value FROM `+qualified(snapshot.plan.options.Schema, "server_settings")+` WHERE key='server_id'`).Scan(&serverID) != nil || serverID == "" {
		return backupformat.SourceFacts{}, ErrSchema
	}
	facts := backupformat.SourceFacts{SchemaVersion: snapshot.plan.version, ProbeVersion: snapshot.plan.options.ProbeVersion,
		PostgreSQLVersion: snapshot.plan.identity.Version, PostgreSQLVersionNum: snapshot.plan.identity.VersionNum,
		DatabaseSchema: snapshot.plan.options.Schema, ServerID: serverID, Tables: tables,
		SchemaSHA256: snapshot.plan.catalog.SHA256, MigrationChecksums: migrations}
	snapshot.facts = &facts
	return cloneFacts(facts), nil
}

// Dump writes a custom archive from exactly the exported snapshot retained by
// this object. The caller must discard its private destination on any error.
func (snapshot *Snapshot) Dump(ctx context.Context, out io.Writer) error {
	if snapshot == nil || snapshot.closed || out == nil {
		return ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := linkedContext(ctx, snapshot.ctx)
	defer cancel()
	if _, err := snapshot.Facts(ctx); err != nil {
		return err
	}
	if err := dumpCommand(ctx, snapshot.plan.options, snapshot.exported, out); err != nil {
		return err
	}
	// Confirm the exporting transaction survived the child operation. This is
	// not a re-read of newer business state and never substitutes a new snapshot.
	var one int
	if snapshot.tx.QueryRow(ctx, `SELECT 1`).Scan(&one) != nil || one != 1 {
		return ErrDatabase
	}
	return nil
}

func fingerprints(ctx context.Context, tx pgx.Tx, catalog Catalog) ([]backupformat.TableFact, error) {
	result := make([]backupformat.TableFact, 0, len(catalog.Tables))
	for _, table := range catalog.Tables {
		order := make([]string, len(table.SortKey))
		for index, column := range table.SortKey {
			order[index] = `t.` + pgx.Identifier{column}.Sanitize() + `::text COLLATE "C" NULLS FIRST`
		}
		rows, err := tx.Query(ctx, `SELECT CASE WHEN pg_catalog.octet_length(pg_catalog.to_jsonb(t)::text)<=33554432 THEN pg_catalog.to_jsonb(t)::text ELSE NULL END FROM `+qualified(catalog.Schema, table.Name)+` t ORDER BY `+strings.Join(order, ","))
		if err != nil {
			return nil, ErrDatabase
		}
		hash := sha256.New()
		var count int64
		var length [8]byte
		for rows.Next() {
			var row *string
			if rows.Scan(&row) != nil {
				rows.Close()
				return nil, ErrDatabase
			}
			if row == nil || len(*row) > 32<<20 {
				rows.Close()
				return nil, ErrLimit
			}
			binary.BigEndian.PutUint64(length[:], uint64(len(*row)))
			_, _ = hash.Write(length[:])
			_, _ = io.WriteString(hash, *row)
			count++
		}
		rows.Close()
		if rows.Err() != nil {
			return nil, ErrDatabase
		}
		result = append(result, backupformat.TableFact{Name: table.Name, Rows: count, SHA256: hex.EncodeToString(hash.Sum(nil))})
	}
	return result, nil
}

func cloneFacts(facts backupformat.SourceFacts) backupformat.SourceFacts {
	facts.Tables = append([]backupformat.TableFact(nil), facts.Tables...)
	facts.MigrationChecksums = append([]backupformat.MigrationFact(nil), facts.MigrationChecksums...)
	return facts
}

func configureTransaction(ctx context.Context, tx pgx.Tx, schema string) error {
	if !identifierPattern.MatchString(schema) {
		return ErrConfiguration
	}
	// A bounded context owns the long transaction. The idle setting extends the
	// pool's 30-second default only locally and still caps abandoned snapshots.
	remaining := 30 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
		if remaining <= 0 {
			return ctx.Err()
		}
	}
	settings := map[string]string{"search_path": `pg_catalog,` + pgx.Identifier{schema}.Sanitize(), "statement_timeout": "0",
		"idle_in_transaction_session_timeout": strconv.FormatInt(remaining.Milliseconds()+1000, 10),
		"TimeZone":                            "UTC", "DateStyle": "ISO, YMD", "IntervalStyle": "postgres", "bytea_output": "hex",
		"extra_float_digits": "3", "client_encoding": "UTF8", "row_security": "off", "standard_conforming_strings": "on"}
	for name, value := range settings {
		if _, err := tx.Exec(ctx, `SELECT pg_catalog.set_config($1,$2,true)`, name, value); err != nil {
			return ErrDatabase
		}
	}
	return nil
}

func linkedContext(ctx, parent context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ctx, cancel)
	return child, func() { stop(); cancel() }
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
