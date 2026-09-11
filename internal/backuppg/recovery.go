package backuppg

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backupformat"
)

// RecoveryIdentity describes observed database ownership, not credentials.
type RecoveryIdentity struct {
	Database    string
	Role        string
	Schema      string
	SchemaOwner string
}

// RecoveryInspection is created only after the actual database has passed the
// compiled catalog, history, role, ownership, and whole-database scope checks.
// Its transaction remains owned by the caller. The caller must additionally
// hold its deployment lease and authorize every mutation using local control.
type RecoveryInspection struct {
	tx             pgx.Tx
	catalog        Catalog
	identity       databaseIdentity
	schemaOwner    string
	publicUsage    bool
	defaultComment bool
	version        int64
	locked         bool
	dropPlan       recoveryDropPlan
}

// InspectRecoveryTransaction validates a dedicated database without requiring
// server_id to have been initialized. It never starts or ends a transaction.
// Unlike ordinary backup snapshots, slot reuse accepts no outside user schema,
// extension, large object, publication, or other external database surface.
func InspectRecoveryTransaction(ctx context.Context, tx pgx.Tx, schema string) (*RecoveryInspection, error) {
	if tx == nil || !identifierPattern.MatchString(schema) {
		return nil, ErrConfiguration
	}
	if err := configureTransaction(ctx, tx, schema); err != nil {
		return nil, err
	}
	identity, err := readIdentity(ctx, tx, schema)
	if err != nil {
		return nil, err
	}
	if err := validateOwnership(ctx, tx, schema); err != nil {
		return nil, err
	}
	if err := validateRecoveryScope(ctx, tx, schema); err != nil {
		return nil, err
	}
	var history bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='schema_migrations' AND c.relkind='r')`, schema).Scan(&history) != nil {
		return nil, ErrDatabase
	}
	if !history {
		return nil, ErrSchema
	}
	var version int64
	if tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM `+qualified(schema, "schema_migrations")).Scan(&version) != nil {
		return nil, ErrSchema
	}
	catalog, migrations, err := loadCatalog(version, schema)
	if err != nil {
		return nil, err
	}
	if err := validateHistory(ctx, tx, schema, migrations); err != nil {
		return nil, err
	}
	actual, _, err := inspectCatalog(ctx, tx, schema)
	if err != nil || !equalJSON(actual, catalog) {
		return nil, ErrSchema
	}
	if err := validateThemeState(ctx, tx, version); err != nil {
		return nil, err
	}
	var owner string
	var publicUsage, defaultComment bool
	if tx.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(n.nspowner),
	 EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a WHERE a.grantee=0 AND a.privilege_type='USAGE'),
	 EXISTS(SELECT 1 FROM pg_catalog.pg_description d WHERE d.classoid='pg_catalog.pg_namespace'::regclass AND d.objoid=n.oid AND d.description='standard public schema')
	 FROM pg_catalog.pg_namespace n WHERE n.nspname=$1`, schema).Scan(&owner, &publicUsage, &defaultComment) != nil {
		return nil, ErrDatabase
	}
	plan, err := compiledRecoveryDropPlan(version)
	if err != nil {
		return nil, err
	}
	return &RecoveryInspection{tx: tx, catalog: catalog, identity: identity, schemaOwner: owner, publicUsage: publicUsage, defaultComment: defaultComment, version: version, dropPlan: plan}, nil
}

func (inspection *RecoveryInspection) Identity() RecoveryIdentity {
	return RecoveryIdentity{Database: inspection.identity.Database, Role: inspection.identity.User, Schema: inspection.catalog.Schema, SchemaOwner: inspection.schemaOwner}
}

// LockTables acquires a deterministic exclusive table set before refreshing
// its inspection. READ COMMITTED is mandatory: a snapshot taken before waiting
// for these locks must not conceal a writer that commits while the caller waits.
func (inspection *RecoveryInspection) LockTables(ctx context.Context) error {
	if inspection == nil {
		return ErrConfiguration
	}
	var isolation string
	if inspection.tx.QueryRow(ctx, `SELECT current_setting('transaction_isolation')`).Scan(&isolation) != nil {
		return ErrDatabase
	}
	if isolation != "read committed" {
		return ErrConfiguration
	}
	tables := make([]string, len(inspection.catalog.Tables))
	for index, table := range inspection.catalog.Tables {
		tables[index] = qualified(inspection.catalog.Schema, table.Name)
	}
	if _, err := inspection.tx.Exec(ctx, `LOCK TABLE `+strings.Join(tables, ",")+` IN ACCESS EXCLUSIVE MODE`); err != nil {
		return ErrDatabase
	}
	fresh, err := InspectRecoveryTransaction(ctx, inspection.tx, inspection.catalog.Schema)
	if err != nil {
		return err
	}
	if fresh.identity != inspection.identity || fresh.schemaOwner != inspection.schemaOwner || fresh.publicUsage != inspection.publicUsage || fresh.defaultComment != inspection.defaultComment || !equalJSON(fresh.catalog, inspection.catalog) {
		return ErrSchema
	}
	inspection.locked = true
	return nil
}

// Facts uses the same exact row serializer and trusted unique ordering as
// Snapshot.Facts. Exclusive locks make its READ COMMITTED statements stable.
func (inspection *RecoveryInspection) Facts(ctx context.Context, probeVersion int64) (backupformat.SourceFacts, error) {
	if inspection == nil || !inspection.locked || probeVersion < 1 {
		return backupformat.SourceFacts{}, ErrConfiguration
	}
	if err := validateThemeState(ctx, inspection.tx, inspection.version); err != nil {
		return backupformat.SourceFacts{}, err
	}
	migrations, err := compiledMigrations(inspection.version)
	if err != nil {
		return backupformat.SourceFacts{}, err
	}
	tables, err := fingerprints(ctx, inspection.tx, inspection.catalog)
	if err != nil {
		return backupformat.SourceFacts{}, err
	}
	var serverID string
	if inspection.tx.QueryRow(ctx, `SELECT value FROM `+qualified(inspection.catalog.Schema, "server_settings")+` WHERE key='server_id'`).Scan(&serverID) != nil || serverID == "" {
		return backupformat.SourceFacts{}, ErrSchema
	}
	return backupformat.SourceFacts{SchemaVersion: inspection.version, ProbeVersion: probeVersion, PostgreSQLVersion: inspection.identity.Version,
		PostgreSQLVersionNum: inspection.identity.VersionNum, DatabaseSchema: inspection.catalog.Schema, ServerID: serverID,
		SchemaSHA256: inspection.catalog.SHA256, MigrationChecksums: migrations, Tables: tables}, nil
}

// EmptyTrustedSchema is destructive and is intended only for recoverydb after
// a locked marker and exact retained-fingerprint comparison. expected must come
// from protected local control, never from an archive or HTTP request. Every
// DROP uses RESTRICT. Any external dependency or newly encountered object makes
// the caller's transaction fail; the caller must roll back the entire operation.
func (inspection *RecoveryInspection) EmptyTrustedSchema(ctx context.Context, expected backupformat.SourceFacts) error {
	if inspection == nil || !inspection.locked {
		return ErrConfiguration
	}
	actual, err := inspection.Facts(ctx, expected.ProbeVersion)
	if err != nil {
		return err
	}
	if !sameRecoveryFacts(actual, expected) {
		return ErrArchive
	}
	if err := validateRecoveryScope(ctx, inspection.tx, inspection.catalog.Schema); err != nil {
		return err
	}
	if err := inspection.dropTrustedObjects(ctx); err != nil {
		return err
	}
	schema := pgx.Identifier{inspection.catalog.Schema}.Sanitize()
	if _, err := inspection.tx.Exec(ctx, `DROP SCHEMA `+schema+` RESTRICT`); err != nil {
		return ErrTarget
	}
	if _, err := inspection.tx.Exec(ctx, `CREATE SCHEMA `+schema+` AUTHORIZATION `+pgx.Identifier{inspection.schemaOwner}.Sanitize()); err != nil {
		return ErrDatabase
	}
	// Preserve the normal public schema's public USAGE. All other grants were
	// rejected by the recovery scope check before any object was removed.
	if inspection.publicUsage {
		if _, err := inspection.tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+schema+` TO PUBLIC`); err != nil {
			return ErrDatabase
		}
	}
	if inspection.defaultComment {
		if _, err := inspection.tx.Exec(ctx, `COMMENT ON SCHEMA `+schema+` IS 'standard public schema'`); err != nil {
			return ErrDatabase
		}
	}
	_, err = InspectEmptyRecoveryTransaction(ctx, inspection.tx, inspection.catalog.Schema)
	return err
}

func sameRecoveryFacts(actual, expected backupformat.SourceFacts) bool {
	return actual.SchemaVersion == expected.SchemaVersion && actual.ProbeVersion == expected.ProbeVersion && actual.DatabaseSchema == expected.DatabaseSchema &&
		actual.ServerID == expected.ServerID && actual.SchemaSHA256 == expected.SchemaSHA256 && actual.PostgreSQLVersionNum/10000 == expected.PostgreSQLVersionNum/10000 &&
		equalJSON(actual.MigrationChecksums, expected.MigrationChecksums) && equalJSON(actual.Tables, expected.Tables)
}

// InspectEmptyRecoveryTransaction accepts a preconfigured empty schema without
// creating a claim, database, role, or application table.
func InspectEmptyRecoveryTransaction(ctx context.Context, tx pgx.Tx, schema string) (RecoveryIdentity, error) {
	if tx == nil || !identifierPattern.MatchString(schema) {
		return RecoveryIdentity{}, ErrConfiguration
	}
	if err := configureTransaction(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	identity, err := readIdentity(ctx, tx, schema)
	if err != nil {
		return RecoveryIdentity{}, err
	}
	if err := validateRecoveryScope(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	if err := validateEmptyTarget(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	var owner string
	if tx.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(nspowner) FROM pg_catalog.pg_namespace WHERE nspname=$1`, schema).Scan(&owner) != nil {
		return RecoveryIdentity{}, ErrDatabase
	}
	return RecoveryIdentity{Database: identity.Database, Role: identity.User, Schema: schema, SchemaOwner: owner}, nil
}

// InspectBindingTransaction is the pre-migration ownership boundary. It does
// not require a current Goby schema and performs no DDL. Callers can inspect an
// existing binding before deciding whether any migration is authorized.
func InspectBindingTransaction(ctx context.Context, tx pgx.Tx, schema string) (RecoveryIdentity, error) {
	if tx == nil || !identifierPattern.MatchString(schema) {
		return RecoveryIdentity{}, ErrConfiguration
	}
	if err := configureTransaction(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	identity, err := readIdentity(ctx, tx, schema)
	if err != nil {
		return RecoveryIdentity{}, err
	}
	if err := validateOwnership(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	if err := validateRecoveryScope(ctx, tx, schema); err != nil {
		return RecoveryIdentity{}, err
	}
	var owner string
	if tx.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(nspowner) FROM pg_catalog.pg_namespace WHERE nspname=$1`, schema).Scan(&owner) != nil {
		return RecoveryIdentity{}, ErrDatabase
	}
	return RecoveryIdentity{Database: identity.Database, Role: identity.User, Schema: schema, SchemaOwner: owner}, nil
}

func validateRecoveryScope(ctx context.Context, tx pgx.Tx, schema string) error {
	var foreign bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
	 SELECT 1 FROM pg_catalog.pg_database WHERE datname=current_database() AND NOT pg_catalog.pg_has_role(datdba,'USAGE')
	 UNION ALL
	 SELECT 1 FROM pg_catalog.pg_namespace n WHERE n.nspname<>$1 AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
	 UNION ALL SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname<>$1 AND n.nspname NOT IN ('pg_catalog','pg_toast','information_schema')
	 UNION ALL SELECT 1 FROM pg_catalog.pg_extension WHERE extname<>'plpgsql'
	 UNION ALL SELECT 1 FROM pg_catalog.pg_event_trigger
	 UNION ALL SELECT 1 FROM pg_catalog.pg_foreign_server
	 UNION ALL SELECT 1 FROM pg_catalog.pg_foreign_data_wrapper
	 UNION ALL SELECT 1 FROM pg_catalog.pg_publication
	 UNION ALL SELECT 1 FROM pg_catalog.pg_subscription
	 UNION ALL SELECT 1 FROM pg_catalog.pg_largeobject_metadata
	 UNION ALL SELECT 1 FROM pg_catalog.pg_default_acl
	 UNION ALL SELECT 1 FROM pg_catalog.pg_seclabel
	 UNION ALL SELECT 1 FROM pg_catalog.pg_description d WHERE
	 (d.classoid='pg_catalog.pg_class'::regclass AND d.objoid IN (SELECT c.oid FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1))
	 OR (d.classoid='pg_catalog.pg_proc'::regclass AND d.objoid IN (SELECT p.oid FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1))
	 OR (d.classoid='pg_catalog.pg_type'::regclass AND d.objoid IN (SELECT t.oid FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname=$1))
	 OR (d.classoid='pg_catalog.pg_constraint'::regclass AND d.objoid IN (SELECT c.oid FROM pg_catalog.pg_constraint c JOIN pg_catalog.pg_namespace n ON n.oid=c.connamespace WHERE n.nspname=$1))
	 OR (d.classoid='pg_catalog.pg_trigger'::regclass AND d.objoid IN (SELECT t.oid FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1))
	 OR (d.classoid='pg_catalog.pg_namespace'::regclass AND d.objoid=(SELECT oid FROM pg_catalog.pg_namespace WHERE nspname=$1) AND NOT ($1='public' AND d.description='standard public schema'))
	 UNION ALL SELECT 1 FROM pg_catalog.pg_namespace n CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a
	 WHERE n.nspname=$1 AND a.grantee<>n.nspowner AND a.grantee<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)
	 AND NOT (a.grantee=0 AND a.privilege_type='USAGE' AND NOT a.is_grantable)
	)`, schema).Scan(&foreign)
	if err != nil || foreign {
		return ErrTarget
	}
	return nil
}

func (inspection *RecoveryInspection) dropTrustedObjects(ctx context.Context) error {
	tx, schema, plan := inspection.tx, inspection.catalog.Schema, inspection.dropPlan
	for _, trigger := range plan.triggers {
		if _, err := tx.Exec(ctx, `DROP TRIGGER `+pgx.Identifier{trigger[1]}.Sanitize()+` ON `+qualified(schema, trigger[0])+` RESTRICT`); err != nil {
			return ErrTarget
		}
	}
	for _, constraint := range plan.constraints {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+qualified(schema, constraint.table)+` DROP CONSTRAINT `+pgx.Identifier{constraint.name}.Sanitize()+` RESTRICT`); err != nil {
			return ErrTarget
		}
	}
	for _, name := range plan.indexes {
		if _, err := tx.Exec(ctx, `DROP INDEX `+qualified(schema, name)+` RESTRICT`); err != nil {
			return ErrTarget
		}
	}
	for _, item := range plan.defaults {
		operation := " DROP DEFAULT"
		if item.generated {
			operation = " DROP EXPRESSION"
		}
		if _, err := tx.Exec(ctx, `ALTER TABLE `+qualified(schema, item.table)+` ALTER COLUMN `+pgx.Identifier{item.column}.Sanitize()+operation); err != nil {
			return ErrTarget
		}
	}
	if len(plan.functions) > 0 {
		selectors := make([]string, len(plan.functions))
		for index, function := range plan.functions {
			selectors[index] = qualified(schema, function.name) + `(` + function.arguments + `)`
		}
		if _, err := tx.Exec(ctx, `DROP FUNCTION `+strings.Join(selectors, ",")+` RESTRICT`); err != nil {
			return ErrTarget
		}
	}
	tables := make([]string, len(inspection.catalog.Tables))
	for index, table := range inspection.catalog.Tables {
		tables[index] = qualified(schema, table.Name)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE `+strings.Join(tables, ",")+` RESTRICT`); err != nil {
		return ErrTarget
	}
	return nil
}
