package backuppg

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
)

type databaseIdentity struct {
	Database   string
	User       string
	Version    string
	VersionNum int
}

func readIdentity(ctx context.Context, tx pgx.Tx, schema string) (databaseIdentity, error) {
	var result databaseIdentity
	var unsafe bool
	var encoding string
	if tx.QueryRow(ctx, `SELECT current_database(),current_user,current_setting('server_version'),current_setting('server_version_num')::integer,
		r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls,current_setting('server_encoding')
		FROM pg_catalog.pg_roles r WHERE r.rolname=current_user`).Scan(&result.Database, &result.User, &result.Version, &result.VersionNum, &unsafe, &encoding) != nil {
		return result, ErrDatabase
	}
	if result.VersionNum/10000 != 17 || encoding != "UTF8" {
		return result, ErrUnsupported
	}
	if unsafe {
		return result, ErrTarget
	}
	var schemaExists bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)`, schema).Scan(&schemaExists) != nil || !schemaExists {
		return result, ErrTarget
	}
	return result, nil
}

func validateOwnership(ctx context.Context, tx pgx.Tx, schema string) error {
	var safe bool
	// PUBLIC access to function execution is PostgreSQL's standard default.
	// Relation writes, schema creation, and inherited administrative roles are
	// not acceptable. Owner-equivalent pg_database_owner is allowed for public.
	err := tx.QueryRow(ctx, `SELECT
		pg_catalog.pg_has_role(n.nspowner,'USAGE') AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a
		 WHERE a.privilege_type='CREATE' AND a.grantee<>n.nspowner AND a.grantee<> (SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c WHERE c.relnamespace=n.oid AND NOT pg_catalog.pg_has_role(c.relowner,'USAGE')) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p WHERE p.pronamespace=n.oid AND NOT pg_catalog.pg_has_role(p.proowner,'USAGE')) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(c.relacl,pg_catalog.acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) a
		 WHERE c.relnamespace=n.oid AND a.grantee<>c.relowner AND a.grantee<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_attribute column_entry ON column_entry.attrelid=c.oid CROSS JOIN LATERAL pg_catalog.aclexplode(column_entry.attacl) a
		 WHERE c.relnamespace=n.oid AND a.grantee<>c.relowner AND a.grantee<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
		 WHERE p.pronamespace=n.oid AND a.grantee<>0 AND a.grantee<>p.proowner AND a.grantee<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)) AND
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles r WHERE (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) AND pg_catalog.pg_has_role(r.oid,'MEMBER'))
		FROM pg_catalog.pg_namespace n WHERE n.nspname=$1`, schema).Scan(&safe)
	if err != nil || !safe {
		return ErrTarget
	}
	return nil
}

func validateEmptyTarget(ctx context.Context, tx pgx.Tx, schema string) error {
	if err := validateOwnership(ctx, tx, schema); err != nil {
		return err
	}
	var empty bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(
		SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
		UNION ALL SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
		UNION ALL SELECT 1 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
		UNION ALL SELECT 1 FROM pg_catalog.pg_extension WHERE extname<>'plpgsql'
		)`).Scan(&empty)
	if err != nil || !empty {
		return ErrTarget
	}
	return nil
}

// Restore builds only trusted embedded DDL in a distinct, empty database. The
// external pg_restore process receives no database connection or credentials:
// it decodes the custom archive to text; Decode recognizes a closed COPY and
// sequence grammar. Archive SQL is never submitted to PostgreSQL.
//
// All table and schema work, including upgrades, is one target transaction.
// Failure rolls it back. The coordinator must exclusively reserve the target
// database before calling and perform credential/runtime normalization after
// successful restoration, before activation. The source database is read-only.
func Restore(ctx context.Context, source, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options) (RestoreResult, error) {
	return restoreWithSource(ctx, source, target, archive, facts, options, nil)
}

// RestoreFinalized adds an obligatory trusted pre-commit finalizer to Restore.
// The source pool is still read-only. Finalizer failure cannot leave a committed
// raw target without the application's normalization and local ownership marker.
func RestoreFinalized(ctx context.Context, source, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options, finalizer Finalizer) (RestoreResult, error) {
	if finalizer == nil {
		return RestoreResult{}, ErrConfiguration
	}
	return restoreWithSource(ctx, source, target, archive, facts, options, finalizer)
}

func restoreWithSource(ctx context.Context, source, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options, finalizer Finalizer) (RestoreResult, error) {
	if source == nil || target == nil || source == target || archive == nil {
		return RestoreResult{}, ErrConfiguration
	}
	return restoreTarget(ctx, target, archive, facts, options, finalizer, func(ctx context.Context, schema string) (databaseIdentity, error) {
		sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return databaseIdentity{}, ErrDatabase
		}
		defer rollback(sourceTx)
		return readIdentity(ctx, sourceTx, schema)
	})
}

// Both entry points share every target, schema, data, and transaction check.
// Source identity resolution is the sole difference: Restore reads it from a
// live read-only connection; RestoreOffline parses trusted deployment policy.
func restoreTarget(ctx context.Context, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options, finalizer Finalizer, identifySource func(context.Context, string) (databaseIdentity, error)) (RestoreResult, error) {
	var result RestoreResult
	options, err := normalizeOptions(options)
	if err != nil {
		return result, err
	}
	if facts.DatabaseSchema != options.Schema || facts.PostgreSQLVersionNum/10000 != 17 || facts.ProbeVersion < 1 || facts.ProbeVersion > options.ProbeVersion {
		return result, ErrArchive
	}
	catalog, migrations, err := loadCatalog(facts.SchemaVersion, options.Schema)
	if err != nil {
		return result, err
	}
	if facts.SchemaSHA256 != catalog.SHA256 || !equalJSON(facts.MigrationChecksums, migrations) || len(facts.Tables) != len(catalog.Tables) {
		return result, ErrArchive
	}
	for i, table := range catalog.Tables {
		if facts.Tables[i].Name != table.Name || facts.Tables[i].Rows < 0 {
			return result, ErrArchive
		}
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if err := checkToolVersions(ctx, options); err != nil {
		return result, err
	}
	sourceIdentity, identityErr := identifySource(ctx, options.Schema)
	if identityErr != nil {
		return result, identityErr
	}
	targetOptions := pgx.TxOptions{}
	if finalizer != nil {
		targetOptions.IsoLevel = pgx.ReadCommitted
	}
	tx, err := target.BeginTx(ctx, targetOptions)
	if err != nil {
		return result, ErrDatabase
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, options.Schema); err != nil {
		return result, err
	}
	targetIdentity, err := readIdentity(ctx, tx, options.Schema)
	if err != nil {
		return result, err
	}
	// Reject equal database or role names even across different hosts: this
	// conservative check prevents aliases from disguising the live database.
	if sourceIdentity.Database == targetIdentity.Database || sourceIdentity.User == targetIdentity.User {
		return result, ErrTarget
	}
	var inherited bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname=$1 AND pg_catalog.pg_has_role(oid,'MEMBER'))`, sourceIdentity.User).Scan(&inherited) != nil || inherited {
		return result, ErrTarget
	}
	if err := validateEmptyTarget(ctx, tx, options.Schema); err != nil {
		return result, err
	}
	if err := migrateTarget(ctx, tx, options.Schema, facts.SchemaVersion); err != nil {
		return result, err
	}
	if err := validateOwnership(ctx, tx, options.Schema); err != nil {
		return result, err
	}
	actual, _, err := inspectCatalog(ctx, tx, options.Schema)
	if err != nil || !equalJSON(actual, catalog) {
		return result, ErrSchema
	}
	// These definitions came from freshly applied embedded migrations, never
	// from the archive. Temporarily remove foreign keys to handle cycles and
	// disable user triggers so restored facts are not regenerated as new rows.
	for _, constraint := range actual.Constraints {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+qualified(options.Schema, constraint.Table)+` DROP CONSTRAINT `+pgx.Identifier{constraint.Name}.Sanitize()); err != nil {
			return result, ErrDatabase
		}
	}
	allTables := make([]string, len(catalog.Tables))
	for i, table := range catalog.Tables {
		allTables[i] = qualified(options.Schema, table.Name)
		if _, err := tx.Exec(ctx, `ALTER TABLE `+allTables[i]+` DISABLE TRIGGER USER`); err != nil {
			return result, ErrDatabase
		}
	}
	if _, err := tx.Exec(ctx, `TRUNCATE TABLE `+strings.Join(allTables, ",")); err != nil {
		return result, ErrDatabase
	}
	sink := &transactionSink{tx: tx, schema: options.Schema, sequenceValues: make(map[string]sequenceState)}
	err = decodeCommand(ctx, options, archive, func(decoded io.Reader) error {
		return Decode(ctx, decoded, catalog, sink, DecodeOptions{MaxBytes: options.MaxDumpBytes})
	})
	if err != nil {
		return result, err
	}
	for _, table := range catalog.Tables {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+qualified(options.Schema, table.Name)+` ENABLE TRIGGER USER`); err != nil {
			return result, ErrDatabase
		}
	}
	for _, constraint := range actual.Constraints {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+qualified(options.Schema, constraint.Table)+` ADD CONSTRAINT `+pgx.Identifier{constraint.Name}.Sanitize()+` `+constraint.Definition); err != nil {
			return result, ErrArchive
		}
	}
	if err := validateHistory(ctx, tx, options.Schema, migrations); err != nil {
		return result, ErrArchive
	}
	if err := validateThemeState(ctx, tx, facts.SchemaVersion); err != nil {
		if errors.Is(err, ErrSchema) {
			return result, ErrArchive
		}
		return result, err
	}
	tables, err := fingerprints(ctx, tx, catalog)
	if err != nil {
		return result, err
	}
	if !equalJSON(tables, facts.Tables) {
		return result, ErrArchive
	}
	var serverID string
	if tx.QueryRow(ctx, `SELECT value FROM `+qualified(options.Schema, "server_settings")+` WHERE key='server_id'`).Scan(&serverID) != nil || serverID != facts.ServerID {
		return result, ErrArchive
	}
	if err := sink.validateSequences(ctx, catalog.Sequences); err != nil {
		return result, err
	}
	compiled, err := database.EmbeddedMigrations()
	if err != nil || len(compiled) == 0 {
		return result, ErrSchema
	}
	current := compiled[len(compiled)-1].Version
	if current > facts.SchemaVersion {
		if err := migrateTarget(ctx, tx, options.Schema, current); err != nil {
			return result, err
		}
		latest, _, err := loadCatalog(current, options.Schema)
		if err != nil {
			return result, err
		}
		actual, _, err := inspectCatalog(ctx, tx, options.Schema)
		if err != nil || !equalJSON(latest, actual) {
			return result, ErrSchema
		}
	}
	if err := validateOwnership(ctx, tx, options.Schema); err != nil {
		return result, err
	}
	if err := validateThemeState(ctx, tx, current); err != nil {
		return result, err
	}
	completed := RestoreResult{SourceVersion: facts.SchemaVersion, CurrentVersion: current, Tables: append([]backupformat.TableFact(nil), tables...)}
	if finalizer != nil {
		if err := finalizer(ctx, tx, completed); err != nil {
			return result, err
		}
		if err := validateThemeState(ctx, tx, current); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, ErrDatabase
	}
	return RestoreResult{SourceVersion: facts.SchemaVersion, CurrentVersion: current, Tables: tables}, nil
}

func migrateTarget(ctx context.Context, tx pgx.Tx, schema string, version int64) error {
	if _, err := tx.Exec(ctx, `SELECT pg_catalog.set_config('search_path',$1,true)`, pgx.Identifier{schema}.Sanitize()+`,pg_catalog`); err != nil {
		return ErrDatabase
	}
	if err := database.RecoveryMigrateTo(ctx, tx, version); err != nil {
		return ErrSchema
	}
	if _, err := tx.Exec(ctx, `SELECT pg_catalog.set_config('search_path',$1,true)`, `pg_catalog,`+pgx.Identifier{schema}.Sanitize()); err != nil {
		return ErrDatabase
	}
	return nil
}

type sequenceState struct {
	value  int64
	called bool
}
type transactionSink struct {
	tx             pgx.Tx
	schema         string
	sequenceValues map[string]sequenceState
}

func (sink *transactionSink) Copy(ctx context.Context, table TableSpec, data io.Reader) (int64, error) {
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		columns[i] = pgx.Identifier{column}.Sanitize()
	}
	tag, err := sink.tx.Conn().PgConn().CopyFrom(ctx, data, `COPY `+qualified(sink.schema, table.Name)+` (`+strings.Join(columns, ", ")+`) FROM STDIN`)
	if err != nil {
		return 0, ErrArchive
	}
	return tag.RowsAffected(), nil
}
func (sink *transactionSink) SetSequence(ctx context.Context, sequence SequenceSpec, value int64, called bool) error {
	// Save until all data and hashes have passed; setval itself is non-MVCC.
	// Sequences were newly created in this target transaction, so even a later
	// rollback removes them rather than altering a previously existing sequence.
	if value < sequence.MinValue || value > sequence.MaxValue {
		return ErrArchive
	}
	sink.sequenceValues[sequence.Name] = sequenceState{value, called}
	return nil
}
func (sink *transactionSink) validateSequences(ctx context.Context, sequences []SequenceSpec) error {
	for _, sequence := range sequences {
		state, ok := sink.sequenceValues[sequence.Name]
		if !ok {
			return ErrArchive
		}
		next := state.value
		if state.called {
			if sequence.Increment <= 0 || state.value > math.MaxInt64-sequence.Increment {
				return ErrArchive
			}
			next += sequence.Increment
		}
		if next > sequence.MaxValue || len(sequence.Consumers) == 0 {
			return ErrArchive
		}
		for _, consumer := range sequence.Consumers {
			var maximum *int64
			if sink.tx.QueryRow(ctx, `SELECT max(`+pgx.Identifier{consumer.Column}.Sanitize()+`) FROM `+qualified(sink.schema, consumer.Table)).Scan(&maximum) != nil {
				return ErrDatabase
			}
			if maximum != nil && next <= *maximum {
				return ErrArchive
			}
		}
		if _, err := sink.tx.Exec(ctx, `SELECT pg_catalog.setval($1::regclass,$2,$3)`, qualified(sink.schema, sequence.Name), state.value, state.called); err != nil {
			return ErrArchive
		}
	}
	return nil
}
