package backuppg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
)

//go:embed catalogs/*.json
var catalogFiles embed.FS

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type catalogBaseline struct {
	Version         int64                        `json:"version"`
	PostgreSQLMajor int                          `json:"postgresql_major"`
	Migrations      []backupformat.MigrationFact `json:"migrations"`
	Catalog         Catalog                      `json:"catalog"`
	Objects         json.RawMessage              `json:"objects"`
}

// ExportCatalog reads a fresh database created from embedded migrations. The
// release operator, rather than an online request, is responsible for proving
// its origin and committing the resulting baseline. No source DDL is issued.
func ExportCatalog(ctx context.Context, pool *pgxpool.Pool, schema string, version int64) ([]byte, error) {
	if pool == nil || !identifierPattern.MatchString(schema) {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, ErrDatabase
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, schema); err != nil {
		return nil, err
	}
	var pgVersion int
	if tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer`).Scan(&pgVersion) != nil || pgVersion/10000 != 17 {
		return nil, ErrUnsupported
	}
	migrations, err := compiledMigrations(version)
	if err != nil {
		return nil, err
	}
	if err := validateHistory(ctx, tx, schema, migrations); err != nil {
		return nil, err
	}
	catalog, objects, err := inspectCatalog(ctx, tx, schema)
	if err != nil {
		return nil, err
	}
	// Schema names are runtime configuration. Deparsed definitions use a trusted
	// search path so only visible object names enter the portable baseline.
	catalog.Schema = ""
	return json.MarshalIndent(catalogBaseline{Version: version, PostgreSQLMajor: 17, Migrations: migrations, Catalog: catalog, Objects: objects}, "", "  ")
}

func loadCatalog(version int64, schema string) (Catalog, []backupformat.MigrationFact, error) {
	if version < 1 || version > 9999 || !identifierPattern.MatchString(schema) {
		return Catalog{}, nil, ErrConfiguration
	}
	data, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
	if err != nil {
		return Catalog{}, nil, ErrUnsupported
	}
	var baseline catalogBaseline
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&baseline) != nil || decoder.Decode(new(any)) != io.EOF || baseline.Version != version || baseline.PostgreSQLMajor != 17 {
		return Catalog{}, nil, ErrSchema
	}
	expected, err := compiledMigrations(version)
	if err != nil || !equalJSON(expected, baseline.Migrations) {
		return Catalog{}, nil, ErrSchema
	}
	// JSON string escapes and indentation are transport details. Normalize
	// decoded values with exact JSON numbers before hashing either representation.
	compact, err := normalizeCatalogObjects(baseline.Objects)
	if err != nil {
		return Catalog{}, nil, ErrSchema
	}
	digest := sha256.Sum256(compact)
	if hex.EncodeToString(digest[:]) != baseline.Catalog.SHA256 || len(baseline.Catalog.Tables) == 0 {
		return Catalog{}, nil, ErrSchema
	}
	baseline.Catalog.Schema = schema
	return baseline.Catalog, expected, nil
}

func compiledMigrations(version int64) ([]backupformat.MigrationFact, error) {
	items, err := database.EmbeddedMigrations()
	if err != nil {
		return nil, ErrSchema
	}
	result := make([]backupformat.MigrationFact, 0, len(items))
	for _, item := range items {
		if item.Version > version {
			break
		}
		if item.Version != int64(len(result)+1) {
			return nil, ErrSchema
		}
		result = append(result, backupformat.MigrationFact{Version: item.Version, Name: item.Name, SHA256: item.SHA256})
	}
	if len(result) == 0 || result[len(result)-1].Version != version {
		return nil, ErrUnsupported
	}
	return result, nil
}

func equalJSON(a, b any) bool {
	aa, e := json.Marshal(a)
	bb, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(aa, bb)
}

func normalizeCatalogObjects(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrSchema
	}
	var result bytes.Buffer
	encoder := json.NewEncoder(&result)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return nil, ErrSchema
	}
	return bytes.TrimSuffix(result.Bytes(), []byte{'\n'}), nil
}

func validateHistory(ctx context.Context, tx pgx.Tx, schema string, expected []backupformat.MigrationFact) error {
	rows, err := tx.Query(ctx, `SELECT version,name FROM `+qualified(schema, "schema_migrations")+` ORDER BY version`)
	if err != nil {
		return ErrSchema
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var version int64
		var name string
		if rows.Scan(&version, &name) != nil || index >= len(expected) || expected[index].Version != version || expected[index].Name != name {
			return ErrSchema
		}
		index++
	}
	if rows.Err() != nil || index != len(expected) {
		return ErrSchema
	}
	return nil
}

func qualified(schema, name string) string { return pgx.Identifier{schema, name}.Sanitize() }

func inspectCatalog(ctx context.Context, tx pgx.Tx, schema string) (Catalog, json.RawMessage, error) {
	var result Catalog
	result.Schema = schema
	var raw string
	if err := tx.QueryRow(ctx, catalogObjectsSQL, schema).Scan(&raw); err != nil {
		return result, nil, fmt.Errorf("catalog object query: %w", ErrSchema)
	}
	// Preserve exact numbers and decoded function bodies while normalizing
	// object-key order, insignificant whitespace, and JSON escape spelling.
	compact, err := normalizeCatalogObjects([]byte(raw))
	if err != nil {
		return result, nil, ErrSchema
	}
	digest := sha256.Sum256(compact)
	result.SHA256 = hex.EncodeToString(digest[:])
	var unsupported bool
	if tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=$1 AND (c.relkind NOT IN ('r','i','S') OR c.relpersistence<>'p' OR c.relispartition OR c.relrowsecurity OR c.relforcerowsecurity OR c.reloftype<>0)
		UNION ALL SELECT 1 FROM pg_catalog.pg_inherits i JOIN pg_catalog.pg_class c ON c.oid=i.inhrelid OR c.oid=i.inhparent JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
		WHERE n.nspname=$1 AND t.typtype NOT IN ('c','b')
		UNION ALL SELECT 1 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
		WHERE n.nspname=$1 AND t.typtype='b' AND t.typelem=0
		UNION ALL SELECT 1 FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid=p.polrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_rewrite r JOIN pg_catalog.pg_class c ON c.oid=r.ev_class JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_operator o JOIN pg_catalog.pg_namespace n ON n.oid=o.oprnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_opclass o JOIN pg_catalog.pg_namespace n ON n.oid=o.opcnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_opfamily o JOIN pg_catalog.pg_namespace n ON n.oid=o.opfnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_collation c JOIN pg_catalog.pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_conversion c JOIN pg_catalog.pg_namespace n ON n.oid=c.connamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_ts_config c JOIN pg_catalog.pg_namespace n ON n.oid=c.cfgnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_ts_dict c JOIN pg_catalog.pg_namespace n ON n.oid=c.dictnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_ts_parser c JOIN pg_catalog.pg_namespace n ON n.oid=c.prsnamespace WHERE n.nspname=$1
		UNION ALL SELECT 1 FROM pg_catalog.pg_ts_template c JOIN pg_catalog.pg_namespace n ON n.oid=c.tmplnamespace WHERE n.nspname=$1
		)`, schema).Scan(&unsupported) != nil || unsupported {
		return result, nil, fmt.Errorf("catalog unsupported object check: %w", ErrSchema)
	}
	rows, err := tx.Query(ctx, `SELECT c.relname,
		ARRAY(SELECT a.attname::text FROM pg_catalog.pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attgenerated='' ORDER BY a.attnum),
		ARRAY(SELECT a.attname::text FROM pg_catalog.pg_index i CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum,ord)
		JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum WHERE i.indrelid=c.oid AND i.indisprimary ORDER BY k.ord),
		ARRAY(SELECT a.attname::text FROM (
		 SELECT i.indkey,i.indnkeyatts FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
		 WHERE i.indrelid=c.oid AND (i.indisprimary OR (i.indisunique AND i.indnullsnotdistinct))
		 AND i.indpred IS NULL AND i.indexprs IS NULL AND i.indisvalid AND i.indisready AND i.indislive
		 ORDER BY i.indisprimary DESC,idx.relname COLLATE "C" LIMIT 1
		) chosen CROSS JOIN LATERAL unnest(chosen.indkey) WITH ORDINALITY k(attnum,ord)
		JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum WHERE k.ord<=chosen.indnkeyatts ORDER BY k.ord)
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind='r' ORDER BY c.relname COLLATE "C"`, schema)
	if err != nil {
		return result, nil, fmt.Errorf("catalog table query: %w", ErrSchema)
	}
	for rows.Next() {
		var table TableSpec
		if rows.Scan(&table.Name, &table.Columns, &table.PrimaryKey, &table.SortKey) != nil {
			rows.Close()
			return result, nil, fmt.Errorf("catalog table decode: %w", ErrSchema)
		}
		if !identifierPattern.MatchString(table.Name) || len(table.Columns) == 0 || len(table.SortKey) == 0 {
			rows.Close()
			return result, nil, fmt.Errorf("catalog table descriptor: %w", ErrSchema)
		}
		for _, col := range append(append(append([]string{}, table.Columns...), table.PrimaryKey...), table.SortKey...) {
			if !identifierPattern.MatchString(col) {
				rows.Close()
				return result, nil, ErrSchema
			}
		}
		result.Tables = append(result.Tables, table)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, nil, ErrSchema
	}
	rows, err = tx.Query(ctx, `SELECT s.relname,t.relname,a.attname,q.seqmin,q.seqmax,q.seqincrement
		FROM pg_catalog.pg_class s JOIN pg_catalog.pg_namespace n ON n.oid=s.relnamespace
		JOIN pg_catalog.pg_sequence q ON q.seqrelid=s.oid
		JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=s.oid AND d.refclassid='pg_catalog.pg_class'::regclass AND d.deptype IN ('a','i')
		JOIN pg_catalog.pg_class t ON t.oid=d.refobjid JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=d.refobjsubid
		WHERE n.nspname=$1 AND s.relkind='S' ORDER BY s.relname COLLATE "C"`, schema)
	if err != nil {
		return result, nil, fmt.Errorf("catalog sequence query: %w", ErrSchema)
	}
	for rows.Next() {
		var sequence SequenceSpec
		if rows.Scan(&sequence.Name, &sequence.Table, &sequence.Column, &sequence.MinValue, &sequence.MaxValue, &sequence.Increment) != nil || !identifierPattern.MatchString(sequence.Name) || sequence.Increment <= 0 {
			rows.Close()
			return result, nil, fmt.Errorf("catalog sequence metadata: %w", ErrSchema)
		}
		result.Sequences = append(result.Sequences, sequence)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, nil, ErrSchema
	}
	for index := range result.Sequences {
		sequence := &result.Sequences[index]
		consumerRows, err := tx.Query(ctx, `SELECT table_name,column_name FROM (
			SELECT $3::text AS table_name,$4::text AS column_name
			UNION SELECT c.relname::text,a.attname::text FROM pg_catalog.pg_depend dep
			JOIN pg_catalog.pg_attrdef def ON dep.classid='pg_catalog.pg_attrdef'::regclass AND def.oid=dep.objid
			JOIN pg_catalog.pg_class c ON c.oid=def.adrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
			JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum=def.adnum
			WHERE dep.refclassid='pg_catalog.pg_class'::regclass AND dep.refobjid=$2::regclass AND n.nspname=$1
		) consumers ORDER BY table_name COLLATE "C",column_name COLLATE "C"`, schema, qualified(schema, sequence.Name), sequence.Table, sequence.Column)
		if err != nil {
			return result, nil, fmt.Errorf("catalog sequence consumer query: %w", ErrSchema)
		}
		for consumerRows.Next() {
			var consumer SequenceColumn
			if consumerRows.Scan(&consumer.Table, &consumer.Column) != nil || !identifierPattern.MatchString(consumer.Table) || !identifierPattern.MatchString(consumer.Column) {
				consumerRows.Close()
				return result, nil, ErrSchema
			}
			sequence.Consumers = append(sequence.Consumers, consumer)
		}
		consumerRows.Close()
		if consumerRows.Err() != nil {
			return result, nil, ErrSchema
		}
	}
	rows, err = tx.Query(ctx, `SELECT c.relname,k.conname,pg_catalog.pg_get_constraintdef(k.oid,true)
		FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=$1 AND k.contype='f' ORDER BY c.relname COLLATE "C",k.conname COLLATE "C"`, schema)
	if err != nil {
		return result, nil, fmt.Errorf("catalog foreign key query: %w", ErrSchema)
	}
	for rows.Next() {
		var constraint ConstraintSpec
		if rows.Scan(&constraint.Table, &constraint.Name, &constraint.Definition) != nil || !identifierPattern.MatchString(constraint.Name) {
			rows.Close()
			return result, nil, ErrSchema
		}
		result.Constraints = append(result.Constraints, constraint)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, nil, ErrSchema
	}
	return result, compact, nil
}

// Every OID-bearing reference is rendered as a stable visible name. Object
// ownership and privileges are checked separately, not masked into this hash.
const catalogObjectsSQL = `WITH namespace AS (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname=$1),
relations AS (SELECT c.* FROM pg_catalog.pg_class c WHERE c.relnamespace=(SELECT oid FROM namespace)),
objects AS (
SELECT 'relation' AS kind,c.relname::text AS name,jsonb_build_object('kind',c.relkind,'persistence',c.relpersistence,
 'partition',c.relispartition,'replica_identity',c.relreplident,'row_security',c.relrowsecurity,'force_row_security',c.relforcerowsecurity,'options',c.reloptions) AS value FROM relations c
UNION ALL SELECT 'column',c.relname||'.'||lpad(a.attnum::text,5,'0'),jsonb_build_object('name',a.attname,'type',pg_catalog.format_type(a.atttypid,a.atttypmod),
 'not_null',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,'dropped',a.attisdropped,
 'storage',a.attstorage,'compression',a.attcompression,'collation',CASE WHEN a.attcollation=0 THEN NULL ELSE a.attcollation::regcollation::text END,
 'default',pg_catalog.pg_get_expr(d.adbin,d.adrelid,true)) FROM relations c JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE a.attnum>0
UNION ALL SELECT 'constraint',c.relname||'.'||k.conname,jsonb_build_object('type',k.contype,'definition',pg_catalog.pg_get_constraintdef(k.oid,true),
 'validated',k.convalidated,'deferrable',k.condeferrable,'deferred',k.condeferred,'no_inherit',k.connoinherit) FROM relations c JOIN pg_catalog.pg_constraint k ON k.conrelid=c.oid
UNION ALL SELECT 'index',c.relname,jsonb_build_object('table',t.relname,'method',am.amname,'unique',i.indisunique,'primary',i.indisprimary,
 'exclusion',i.indisexclusion,'immediate',i.indimmediate,'valid',i.indisvalid,'ready',i.indisready,'live',i.indislive,
 'nulls_not_distinct',i.indnullsnotdistinct,'key_count',i.indnkeyatts,'options',i.indoption::text,
 'operator_classes',ARRAY(SELECT n.nspname||'.'||o.opcname FROM unnest(i.indclass) WITH ORDINALITY k(oid,position) JOIN pg_catalog.pg_opclass o ON o.oid=k.oid JOIN pg_catalog.pg_namespace n ON n.oid=o.opcnamespace ORDER BY k.position),
 'collations',ARRAY(SELECT CASE WHEN k.oid=0 THEN NULL ELSE k.oid::regcollation::text END FROM unnest(i.indcollation) WITH ORDINALITY k(oid,position) ORDER BY k.position),
 'columns',ARRAY(SELECT pg_catalog.pg_get_indexdef(c.oid,k,true) FROM generate_series(1,i.indnatts) k),
 'predicate',pg_catalog.pg_get_expr(i.indpred,i.indrelid,true)) FROM relations c JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid JOIN relations t ON t.oid=i.indrelid JOIN pg_catalog.pg_am am ON am.oid=c.relam
UNION ALL SELECT 'sequence',c.relname,jsonb_build_object('type',pg_catalog.format_type(s.seqtypid,NULL),'start',s.seqstart,'increment',s.seqincrement,
 'max',s.seqmax,'min',s.seqmin,'cache',s.seqcache,'cycle',s.seqcycle,'owned_by',t.relname||'.'||a.attname)
 FROM relations c JOIN pg_catalog.pg_sequence s ON s.seqrelid=c.oid LEFT JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=c.oid AND d.refclassid='pg_catalog.pg_class'::regclass AND d.deptype IN ('a','i')
 LEFT JOIN relations t ON t.oid=d.refobjid LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=d.refobjsubid
UNION ALL SELECT 'function',p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('kind',p.prokind,'language',l.lanname,
 'return_type',pg_catalog.pg_get_function_result(p.oid),'arguments',pg_catalog.pg_get_function_arguments(p.oid),'source',p.prosrc,'binary',p.probin,
 'security_definer',p.prosecdef,'leakproof',p.proleakproof,'strict',p.proisstrict,'returns_set',p.proretset,'volatility',p.provolatile,'parallel',p.proparallel,'config',p.proconfig,
 'cost',p.procost,'rows',p.prorows,'support',CASE WHEN p.prosupport=0 THEN NULL ELSE p.prosupport::regproc::text END)
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_language l ON l.oid=p.prolang WHERE p.pronamespace=(SELECT oid FROM namespace)
UNION ALL SELECT 'trigger',c.relname||'.'||t.tgname,jsonb_build_object('function',t.tgfoid::regprocedure::text,'enabled',t.tgenabled,'type',t.tgtype,
 'arguments',encode(t.tgargs,'hex'),'columns',t.tgattr::text,'when',pg_catalog.pg_get_expr(t.tgqual,t.tgrelid,true),'old_table',t.tgoldtable,'new_table',t.tgnewtable)
 FROM relations c JOIN pg_catalog.pg_trigger t ON t.tgrelid=c.oid WHERE NOT t.tgisinternal
) SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'name',name,'value',value) ORDER BY kind COLLATE "C",name COLLATE "C"),'[]'::jsonb)::text FROM objects`
