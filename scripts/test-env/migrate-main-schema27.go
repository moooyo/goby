//go:build ignore

// This Linux operator helper only inspects an explicitly pinned database or
// applies the embedded schema 26-to-27 transition in place. The external operator
// owns service isolation, exported snapshots, backups, rehearsal databases,
// deployment receipts, and any later startup. It never restores old data.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverydb"
	"golang.org/x/sys/unix"
)

const (
	reportSchema                = "goby-main-schema27-migration"
	operatorMarker              = "goby-main-schema27-upgrade-v1"
	mainDatabase                = "goby_test"
	mainDatabaseOID      uint32 = 16385
	mainRoleOID          uint32 = 16384
	mainSystemIdentifier        = "7683277964552005578"
	mainDeploymentID            = "f58d5e0c8ff49fca916499e666bffd9f"
	operationRoot               = "/opt/goby-test/backups/main-schema27-v1"
	extraMigrationSHA256        = "b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c"
	maximumReportBytes          = 4 << 20
)

var (
	digestPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idPattern         = regexp.MustCompile(`^[0-9a-f]{32}$`)
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	rehearsalPattern  = regexp.MustCompile(`^goby_upgrade27_m3e_[0-9a-f]{24}$`)
	snapshotPattern   = regexp.MustCompile(`^[0-9A-Fa-f]{1,16}-[0-9A-Fa-f]{1,16}-[0-9]{1,10}$`)
	runPattern        = regexp.MustCompile(`^run-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{24}$`)
	bootPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reportNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}\.json$`)
)

// This is the frozen migration27 backfill projection over the preexisting
// items. Its multiset is captured in the source snapshot and compared with the
// new marker table; no attachment owner or resource is inferred here.
const expectedExtraPathsSQL = `WITH canonical_items AS (
 SELECT item.root_id,item.relative_path,item.is_folder,string_to_array(item.relative_path,'/') AS components
 FROM public.items item JOIN public.library_roots root ON root.id=item.root_id AND root.library_id=item.library_id
 JOIN public.libraries library ON library.id=root.library_id
 WHERE library.collection_type='movies' AND item.relative_path<>''
 AND position(chr(92) in item.relative_path)=0 AND item.relative_path !~ '^[A-Za-z]:'
 AND NOT (string_to_array(item.relative_path,'/') && ARRAY['','.','..'])
), boundaries AS (
 SELECT item.root_id,array_to_string(item.components[1:boundary.position],'/') AS relative_path,boundary.layout
 FROM canonical_items item CROSS JOIN LATERAL (
  SELECT component.position,translate(item.components[component.position],'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz') AS layout
  FROM generate_subscripts(item.components,1) AS component(position)
  WHERE (component.position<array_length(item.components,1) OR item.is_folder)
  AND translate(item.components[component.position],'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz')
   IN ('theme-music','backdrops','featurettes','deleted scenes','trailers')
  ORDER BY component.position LIMIT 1
 ) boundary
)
SELECT DISTINCT root_id,relative_path,true AS is_directory FROM boundaries
WHERE layout IN ('featurettes','deleted scenes','trailers')`

type options struct {
	mode                 string
	expectedUID          int
	expectedDatabase     string
	expectedDatabaseOID  uint64
	expectedRole         string
	expectedRoleOID      uint64
	expectedSystem       string
	expectedPort         int
	expectedDeployment   string
	expectedSchema       int64
	expectedOwnerComment string
	snapshotID           string
	baselinePath         string
	baselineSHA256       string
	output               string
	rehearsal            bool
	clusterProofPath     string
	clusterProofSHA256   string
	runID                string
	sourceManifestSHA256 string
	toolManifestSHA256   string
	candidateSHA256      string
	helperSHA256         string
	proof                clusterProof
}

type clusterProof struct {
	Schema                      string `json:"schema"`
	Version                     int    `json:"version"`
	SystemIdentifier            string `json:"system_identifier"`
	PostmasterPID               int    `json:"postmaster_pid"`
	PostmasterStartTicks        string `json:"postmaster_start_ticks"`
	BootID                      string `json:"boot_id"`
	PostmasterStartMicroseconds int64  `json:"postmaster_start_microseconds"`
	Port                        int    `json:"port"`
	ServerVersion               int    `json:"server_version"`
	DataDirectory               string `json:"data_directory"`
	RunID                       string `json:"run_id"`
	SourceManifestSHA256        string `json:"source_manifest_sha256"`
	ToolManifestSHA256          string `json:"tool_manifest_sha256"`
	CandidateSHA256             string `json:"candidate_sha256"`
	HelperSHA256                string `json:"helper_sha256"`
	Database                    string `json:"database"`
	DatabaseOID                 uint32 `json:"database_oid"`
	Role                        string `json:"role"`
	RoleOID                     uint32 `json:"role_oid"`
}

type databaseIdentity struct {
	Database                    string   `json:"database"`
	DatabaseOID                 uint32   `json:"database_oid"`
	DatabaseOwnerOID            uint32   `json:"database_owner_oid"`
	Role                        string   `json:"role"`
	RoleOID                     uint32   `json:"role_oid"`
	SchemaOID                   uint32   `json:"schema_oid"`
	SchemaOwnerOID              uint32   `json:"schema_owner_oid"`
	SchemaOwnerKind             string   `json:"schema_owner_kind"`
	SystemIdentifier            string   `json:"system_identifier"`
	Port                        int      `json:"port"`
	PostgreSQLVersionNum        int      `json:"postgresql_version_num"`
	PostmasterPID               int      `json:"postmaster_pid"`
	PostmasterStartTicks        string   `json:"postmaster_start_ticks"`
	PostmasterStartMicroseconds int64    `json:"postmaster_start_microseconds"`
	BootID                      string   `json:"boot_id"`
	DatabaseACL                 aclState `json:"database_acl"`
	SchemaACL                   aclState `json:"schema_acl"`
}

type aclGrant struct {
	Grantor   string `json:"grantor"`
	Grantee   string `json:"grantee"`
	Privilege string `json:"privilege"`
	Grantable bool   `json:"grantable"`
}

type aclState struct {
	Raw    *string    `json:"raw"`
	Grants []aclGrant `json:"grants"`
}

type columnACLState struct {
	Name   string   `json:"name"`
	Number int16    `json:"number"`
	ACL    aclState `json:"acl"`
}

type relationState struct {
	Name       string           `json:"name"`
	OID        uint32           `json:"oid"`
	OwnerOID   uint32           `json:"owner_oid"`
	Kind       string           `json:"kind"`
	ACL        aclState         `json:"acl"`
	ColumnACLs []columnACLState `json:"column_acls"`
}

type rowSetState struct {
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

type extraTransitionState struct {
	ReservedPathCount     int64  `json:"reserved_path_count"`
	ExpectedPathsSHA256   string `json:"expected_paths_sha256"`
	ReservedPathsSHA256   string `json:"reserved_paths_sha256"`
	ResourceCount         int64  `json:"resource_count"`
	OldSequencesUnchanged bool   `json:"old_sequences_unchanged"`
}
type columnState struct {
	Name         string `json:"name"`
	Number       int16  `json:"number"`
	TypeOID      uint32 `json:"type_oid"`
	TypeModifier int32  `json:"type_modifier"`
	Type         string `json:"type"`
	NotNull      bool   `json:"not_null"`
	Generated    string `json:"generated"`
	Identity     string `json:"identity"`
	CollationOID uint32 `json:"collation_oid"`
}

type tableState struct {
	Name     string        `json:"name"`
	OID      uint32        `json:"oid"`
	OwnerOID uint32        `json:"owner_oid"`
	Columns  []columnState `json:"columns"`
	Rows     int64         `json:"rows"`
	SHA256   string        `json:"sha256"`
}

type sequenceState struct {
	Name      string `json:"name"`
	OID       uint32 `json:"oid"`
	OwnerOID  uint32 `json:"owner_oid"`
	LastValue string `json:"last_value"`
	LogCount  string `json:"log_cnt"`
	IsCalled  bool   `json:"is_called"`
}

type bindingState struct {
	DeploymentID string `json:"deployment_id"`
	GenerationID string `json:"generation_id"`
	Slot         string `json:"slot"`
	SHA256       string `json:"sha256"`
}

type capturedState struct {
	SchemaVersion          int64            `json:"schema_version"`
	TrustedCatalogVerified bool             `json:"trusted_catalog_verified"`
	MigrationPrefixSHA256  string           `json:"migration_prefix_sha256"`
	RowHashFormat          string           `json:"row_hash_format"`
	Identity               databaseIdentity `json:"identity"`
	Tables                 []tableState     `json:"tables"`
	Sequences              []sequenceState  `json:"sequences"`
	Relations              []relationState  `json:"relations"`
	ExpectedExtraPaths     rowSetState      `json:"expected_extra_paths"`
	Binding                bindingState     `json:"binding"`
}

type targetIdentity struct {
	Database    string `json:"database"`
	DatabaseOID uint32 `json:"database_oid"`
	Role        string `json:"role"`
	RoleOID     uint32 `json:"role_oid"`
}

type report struct {
	Schema                   string                `json:"schema"`
	Version                  int                   `json:"version"`
	Mode                     string                `json:"mode"`
	Status                   string                `json:"status"`
	SourceSchemaVersion      int64                 `json:"source_schema_version"`
	TargetSchemaVersion      int64                 `json:"target_schema_version"`
	Rehearsal                bool                  `json:"rehearsal"`
	StateSHA256              string                `json:"state_sha256"`
	InputBaselineSHA256      string                `json:"input_baseline_sha256,omitempty"`
	BeforeStateSHA256        string                `json:"before_state_sha256,omitempty"`
	PreservedStateSHA256     string                `json:"preserved_state_sha256,omitempty"`
	State                    capturedState         `json:"state"`
	RunID                    string                `json:"run_id"`
	SourceManifestSHA256     string                `json:"source_manifest_sha256"`
	ToolManifestSHA256       string                `json:"tool_manifest_sha256"`
	CandidateSHA256          string                `json:"candidate_sha256"`
	HelperSHA256             string                `json:"helper_sha256"`
	ClusterProofSHA256       string                `json:"cluster_proof_sha256"`
	Target                   targetIdentity        `json:"target"`
	NewExtraDefaultsVerified bool                  `json:"new_extra_defaults_verified"`
	ExtraTransition          *extraTransitionState `json:"extra_transition,omitempty"`
}

type operationFailure struct{ code, outcome string }

func (failure operationFailure) Error() string { return failure.code }
func fail(code string) error                   { return operationFailure{code: code, outcome: "failed"} }

func main() {
	defer func() {
		if recover() != nil {
			emitFailure(operationFailure{code: "internal_failure", outcome: "unknown"})
			os.Exit(1)
		}
	}()
	result, err := run(os.Args[1:])
	if err != nil {
		var failure operationFailure
		if !errors.As(err, &failure) {
			failure = operationFailure{code: "internal_failure", outcome: "unknown"}
		}
		emitFailure(failure)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Schema      string `json:"schema"`
		Version     int    `json:"version"`
		Mode        string `json:"mode"`
		Status      string `json:"status"`
		StateSHA256 string `json:"state_sha256"`
	}{reportSchema, 1, result.Mode, result.Status, result.StateSHA256})
}

func emitFailure(failure operationFailure) {
	_ = json.NewEncoder(os.Stderr).Encode(struct {
		Schema  string `json:"schema"`
		Version int    `json:"version"`
		Status  string `json:"status"`
		Code    string `json:"code"`
	}{reportSchema, 1, failure.outcome, failure.code})
}

func parseOptions(arguments []string) (options, error) {
	var result options
	if len(arguments) == 0 || arguments[0] != "inspect" && arguments[0] != "migrate" {
		return result, fail("invalid_arguments")
	}
	result.mode = arguments[0]
	flags := flag.NewFlagSet("migrate-main-schema27", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&result.expectedUID, "expected-os-uid", 0, "Expected Linux effective and real UID (0).")
	flags.StringVar(&result.expectedDatabase, "expected-database", "", "Pinned database name.")
	flags.Uint64Var(&result.expectedDatabaseOID, "expected-database-oid", 0, "Pinned database OID.")
	flags.StringVar(&result.expectedRole, "expected-role", "", "Pinned ordinary database role.")
	flags.Uint64Var(&result.expectedRoleOID, "expected-role-oid", 0, "Pinned ordinary role OID.")
	flags.StringVar(&result.expectedSystem, "expected-system-identifier", "", "Pinned PostgreSQL cluster identifier.")
	flags.IntVar(&result.expectedPort, "expected-port", 0, "Pinned PostgreSQL port.")
	flags.StringVar(&result.expectedDeployment, "expected-deployment-id", "", "Pinned initial primary deployment identifier.")
	flags.Int64Var(&result.expectedSchema, "expected-schema", 26, "Expected inspection schema, 26 or 27.")
	flags.StringVar(&result.expectedOwnerComment, "expected-owner-comment", "", "Exact rehearsal database and role ownership comment.")
	flags.StringVar(&result.snapshotID, "snapshot-id", "", "Caller-held exported PostgreSQL snapshot.")
	flags.StringVar(&result.baselinePath, "baseline", "", "Private schema26 inspection report from this run.")
	flags.StringVar(&result.baselineSHA256, "baseline-sha256", "", "SHA256 of the exact inspection report bytes.")
	flags.StringVar(&result.output, "output", "", "New private report path.")
	flags.BoolVar(&result.rehearsal, "rehearsal", false, "Restrict execution to an owned random rehearsal database.")
	flags.StringVar(&result.clusterProofPath, "cluster-proof", "", "Root-owned private cluster and process proof.")
	flags.StringVar(&result.clusterProofSHA256, "cluster-proof-sha256", "", "SHA256 of the exact cluster proof bytes.")
	flags.StringVar(&result.runID, "run-id", "", "Exact operator run identifier.")
	flags.StringVar(&result.sourceManifestSHA256, "source-manifest-sha256", "", "Pinned source manifest SHA256.")
	flags.StringVar(&result.toolManifestSHA256, "tool-manifest-sha256", "", "Pinned independent tool manifest SHA256.")
	flags.StringVar(&result.candidateSHA256, "candidate-sha256", "", "Pinned candidate binary SHA256.")
	flags.StringVar(&result.helperSHA256, "expected-helper-sha256", "", "Pinned running helper binary SHA256.")
	if flags.Parse(arguments[1:]) != nil || flags.NArg() != 0 || result.expectedUID != 0 ||
		os.Getuid() != result.expectedUID || os.Geteuid() != result.expectedUID || os.Getgid() != 0 || os.Getegid() != 0 ||
		!namePattern.MatchString(result.expectedDatabase) || !namePattern.MatchString(result.expectedRole) ||
		result.expectedDatabaseOID == 0 || result.expectedDatabaseOID > 4294967295 || result.expectedRoleOID == 0 || result.expectedRoleOID > 4294967295 ||
		result.expectedSystem != mainSystemIdentifier || result.expectedPort != 5432 || !idPattern.MatchString(result.expectedDeployment) ||
		!filepath.IsAbs(result.output) || result.expectedSchema != 26 && result.expectedSchema != 27 ||
		!filepath.IsAbs(result.clusterProofPath) || !digestPattern.MatchString(result.clusterProofSHA256) || !runPattern.MatchString(result.runID) ||
		!digestPattern.MatchString(result.sourceManifestSHA256) || !digestPattern.MatchString(result.toolManifestSHA256) ||
		!digestPattern.MatchString(result.candidateSHA256) || !digestPattern.MatchString(result.helperSHA256) {
		return result, fail("invalid_arguments")
	}
	if result.rehearsal {
		if !rehearsalPattern.MatchString(result.expectedDatabase) || result.expectedRole != result.expectedDatabase ||
			result.expectedDatabaseOID == uint64(mainDatabaseOID) || result.expectedRoleOID == uint64(mainRoleOID) ||
			result.expectedDatabaseOID == 994944 || result.expectedRoleOID == 994943 ||
			result.expectedOwnerComment != operatorMarker+":"+result.runID {
			return result, fail("invalid_rehearsal_scope")
		}
	} else if result.expectedDatabase != mainDatabase || result.expectedRole != mainDatabase ||
		result.expectedDatabaseOID != uint64(mainDatabaseOID) || result.expectedRoleOID != uint64(mainRoleOID) || result.expectedOwnerComment != "" {
		return result, fail("invalid_main_scope")
	}
	runDirectory := filepath.Join(operationRoot, result.runID)
	for _, path := range []string{result.output, result.clusterProofPath, result.baselinePath} {
		if path != "" && (filepath.Clean(path) != path || filepath.Dir(path) != runDirectory ||
			!reportNamePattern.MatchString(filepath.Base(path))) {
			return result, fail("invalid_run_path")
		}
	}
	if result.expectedDeployment != mainDeploymentID {
		return result, fail("invalid_deployment_binding")
	}
	if result.mode == "inspect" {
		if result.baselinePath != "" || result.baselineSHA256 != "" || result.snapshotID != "" && !snapshotPattern.MatchString(result.snapshotID) {
			return result, fail("invalid_inspection_arguments")
		}
		if result.expectedSchema == 26 && !result.rehearsal && result.snapshotID == "" {
			return result, fail("snapshot_required")
		}
	} else if result.expectedSchema != 26 || result.snapshotID != "" || !filepath.IsAbs(result.baselinePath) || !digestPattern.MatchString(result.baselineSHA256) {
		return result, fail("invalid_migration_arguments")
	}
	return result, nil
}

func run(arguments []string) (result report, runErr error) {
	opt, err := parseOptions(arguments)
	if err != nil {
		return result, err
	}
	compiled, err := database.EmbeddedMigrations()
	if err != nil || len(compiled) != 27 || compiled[26].Version != 27 ||
		compiled[26].Name != "0027_movie_extras.sql" || compiled[26].SHA256 != extraMigrationSHA256 {
		return result, fail("embedded_transition_mismatch")
	}
	opt.proof, err = readClusterProof(opt)
	if err != nil {
		return result, err
	}
	output, err := reserveOutput(opt.output, opt.expectedUID)
	if err != nil {
		return result, err
	}
	defer output.close()
	var baseline capturedState
	if opt.mode == "migrate" {
		baseline, err = readBaseline(opt)
		if err != nil {
			return result, err
		}
	}
	url := os.Getenv("GOBY_DATABASE_URL")
	if url == "" {
		return result, fail("database_url_required")
	}
	configuration, err := pgxpool.ParseConfig(url)
	if err != nil {
		return result, fail("database_configuration_invalid")
	}
	configuration.MaxConns, configuration.MinConns = 3, 0
	if configuration.ConnConfig.RuntimeParams == nil {
		configuration.ConnConfig.RuntimeParams = make(map[string]string)
	}
	configuration.ConnConfig.RuntimeParams["application_name"] = "goby-main-schema27-operator"
	configuration.ConnConfig.RuntimeParams["search_path"] = "pg_catalog,public"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return result, fail("database_unavailable")
	}
	defer pool.Close()
	txOptions := pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	if opt.mode == "inspect" {
		txOptions = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := pool.BeginTx(ctx, txOptions)
	if err != nil {
		return result, fail("transaction_unavailable")
	}
	commitAttempted := false
	committed := false
	var lease *database.Lease
	defer func() {
		if !commitAttempted {
			rollbackCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				_ = tx.Conn().Close(rollbackCtx)
				runErr = operationFailure{code: "rollback_outcome_unknown", outcome: "unknown"}
			}
			stop()
		} else if !committed {
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_ = tx.Conn().Close(closeCtx)
			stop()
		}
		// Finish the transaction before releasing startup ownership.
		if lease != nil {
			if err := lease.Close(); err != nil && runErr == nil {
				outcome := "failed"
				if committed {
					outcome = "committed_cleanup_failed"
				}
				runErr = operationFailure{code: "deployment_lease_close_failed", outcome: outcome}
			}
		}
	}()
	if opt.snapshotID != "" {
		// The validated snapshot grammar contains no SQL quoting characters.
		// Import must precede every query in this repeatable-read transaction.
		if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+opt.snapshotID+"'"); err != nil {
			return result, fail("snapshot_import_failed")
		}
	}
	if _, err := readIdentity(ctx, tx, opt); err != nil {
		return result, err
	}
	lease, err = database.AcquireLease(ctx, pool)
	if err != nil {
		return result, fail("deployment_lease_unavailable")
	}
	go func() {
		select {
		case <-lease.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	if !lease.ProtectsTransaction(pool, tx) {
		return result, fail("deployment_lease_mismatch")
	}
	inspection, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public")
	if err != nil {
		return result, fail("trusted_catalog_rejected")
	}
	if opt.mode == "migrate" {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='10s'"); err != nil {
			return result, fail("transaction_configuration_failed")
		}
		if inspection.LockTables(ctx) != nil {
			return result, fail("table_lock_failed")
		}
	}
	before, err := captureState(ctx, tx, opt, opt.expectedSchema)
	if err != nil {
		return result, err
	}
	beforeDigest, err := stateDigest(before)
	if err != nil {
		return result, err
	}
	result = report{Schema: reportSchema, Version: 1, Mode: opt.mode, Status: "inspected", SourceSchemaVersion: opt.expectedSchema,
		TargetSchemaVersion: 27, Rehearsal: opt.rehearsal, StateSHA256: beforeDigest, State: before,
		RunID: opt.runID, SourceManifestSHA256: opt.sourceManifestSHA256, ToolManifestSHA256: opt.toolManifestSHA256, CandidateSHA256: opt.candidateSHA256,
		HelperSHA256: opt.helperSHA256, ClusterProofSHA256: opt.clusterProofSHA256,
		Target: targetIdentity{Database: before.Identity.Database, DatabaseOID: before.Identity.DatabaseOID, Role: before.Identity.Role, RoleOID: before.Identity.RoleOID}}
	if opt.mode == "inspect" {
		if !lease.ProtectsTransaction(pool, tx) || ctx.Err() != nil {
			return result, fail("deployment_lease_lost")
		}
		if tx.Rollback(ctx) != nil {
			return result, fail("inspection_close_failed")
		}
		if output.stage(result) != nil || output.publish() != nil {
			return result, fail("report_publish_failed")
		}
		return result, nil
	}
	if !sameBaseline(baseline, before, opt.rehearsal) {
		return result, fail("baseline_state_mismatch")
	}
	if !lease.ProtectsTransaction(pool, tx) {
		return result, fail("deployment_lease_lost")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path=public,pg_catalog"); err != nil {
		return result, fail("transaction_configuration_failed")
	}
	if database.RecoveryMigrateTo(ctx, tx, 27) != nil {
		return result, fail("migration_failed")
	}
	if _, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public"); err != nil {
		return result, fail("target_catalog_rejected")
	}
	after, err := captureState(ctx, tx, opt, 27)
	if err != nil {
		return result, err
	}
	preservedDigest, err := verifyPreserved(ctx, tx, before, after)
	if err != nil {
		return result, err
	}
	extra, err := verifyNewExtraState(ctx, tx, before, after)
	if err != nil {
		return result, err
	}
	result.NewExtraDefaultsVerified, result.ExtraTransition = true, &extra
	result.Status, result.State = "committed", after
	result.StateSHA256, err = stateDigest(after)
	if err != nil {
		return result, err
	}
	result.InputBaselineSHA256, result.BeforeStateSHA256, result.PreservedStateSHA256 = opt.baselineSHA256, beforeDigest, preservedDigest
	// Prepare and fsync the private report before commit. Only publication of
	// its final name after a successful commit constitutes a success report.
	if output.stage(result) != nil {
		return result, fail("report_prepare_failed")
	}
	if !lease.ProtectsTransaction(pool, tx) || ctx.Err() != nil {
		return result, fail("deployment_lease_lost")
	}
	commitAttempted = true
	if tx.Commit(ctx) != nil {
		return result, operationFailure{code: "commit_outcome_unknown", outcome: "unknown"}
	}
	committed = true
	if output.publish() != nil {
		return result, operationFailure{code: "report_publish_failed", outcome: "committed_report_failed"}
	}
	return result, nil
}

func readClusterProof(opt options) (clusterProof, error) {
	var proof clusterProof
	data, err := readPrivateBytes(opt.clusterProofPath, 0, 65536)
	if err != nil {
		return proof, fail("cluster_proof_unavailable")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != opt.clusterProofSHA256 {
		return proof, fail("cluster_proof_digest_mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proof) != nil || decoder.Decode(new(any)) != io.EOF ||
		proof.Schema != "goby-main-schema27-cluster-proof" || proof.Version != 1 || proof.SystemIdentifier != opt.expectedSystem ||
		proof.Port != opt.expectedPort || proof.ServerVersion != 170011 || proof.DataDirectory != "/var/lib/postgresql/17/main" ||
		proof.PostmasterPID < 2 || proof.PostmasterStartMicroseconds < 1 || !bootPattern.MatchString(proof.BootID) ||
		proof.RunID != opt.runID || proof.SourceManifestSHA256 != opt.sourceManifestSHA256 || proof.ToolManifestSHA256 != opt.toolManifestSHA256 || proof.CandidateSHA256 != opt.candidateSHA256 ||
		proof.HelperSHA256 != opt.helperSHA256 ||
		proof.Database != opt.expectedDatabase || proof.DatabaseOID != uint32(opt.expectedDatabaseOID) ||
		proof.Role != opt.expectedRole || proof.RoleOID != uint32(opt.expectedRoleOID) {
		return proof, fail("cluster_proof_rejected")
	}
	ticks, err := strconv.ParseUint(proof.PostmasterStartTicks, 10, 64)
	if err != nil || ticks == 0 || strconv.FormatUint(ticks, 10) != proof.PostmasterStartTicks {
		return proof, fail("cluster_proof_rejected")
	}
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return proof, fail("helper_identity_unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 128<<20 {
		return proof, fail("helper_identity_unavailable")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, (128<<20)+1))
	if err != nil || count != info.Size() || hex.EncodeToString(hash.Sum(nil)) != opt.helperSHA256 {
		return proof, fail("helper_identity_mismatch")
	}
	if err := verifyProcessProof(proof, 0, proof.PostmasterStartMicroseconds); err != nil {
		return proof, err
	}
	return proof, nil
}

func verifyProcessProof(proof clusterProof, backendPID int, startMicroseconds int64) error {
	if startMicroseconds != proof.PostmasterStartMicroseconds {
		return fail("postmaster_start_mismatch")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != proof.BootID {
		return fail("cluster_boot_mismatch")
	}
	_, ticks, err := processIdentity(proof.PostmasterPID)
	if err != nil || ticks != proof.PostmasterStartTicks {
		return fail("postmaster_process_mismatch")
	}
	if backendPID != 0 {
		parent, _, err := processIdentity(backendPID)
		if err != nil || parent != proof.PostmasterPID {
			return fail("database_backend_mismatch")
		}
	}
	return nil
}

func processIdentity(pid int) (int, string, error) {
	if pid < 2 {
		return 0, "", fail("process_identity_unavailable")
	}
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", fail("process_identity_unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(data) == 0 || len(data) > 8192 {
		return 0, "", fail("process_identity_unavailable")
	}
	text := string(data)
	opening, closing := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if opening < 1 || closing <= opening {
		return 0, "", fail("process_identity_unavailable")
	}
	observed, err := strconv.Atoi(strings.TrimSpace(text[:opening]))
	fields := strings.Fields(text[closing+1:])
	if err != nil || observed != pid || len(fields) < 20 {
		return 0, "", fail("process_identity_unavailable")
	}
	parent, err := strconv.Atoi(fields[1])
	ticks, tickErr := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || parent < 1 || tickErr != nil || ticks == 0 {
		return 0, "", fail("process_identity_unavailable")
	}
	return parent, strconv.FormatUint(ticks, 10), nil
}

func readPrivateBytes(path string, uid int, limit int64) ([]byte, error) {
	directory, err := openPrivateDirectory(filepath.Dir(path), uid)
	if err != nil {
		return nil, fail("private_artifact_unavailable")
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fail("private_artifact_unavailable")
	}
	file := os.NewFile(uintptr(fd), "private-artifact")
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != uint32(uid) || stat.Gid != 0 || stat.Size < 1 || stat.Size > limit || stat.Nlink != 1 {
		return nil, fail("private_artifact_rejected")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) != stat.Size {
		return nil, fail("private_artifact_unavailable")
	}
	return data, nil
}

func openPrivateDirectory(path string, uid int) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fail("private_directory_rejected")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail("private_directory_unavailable")
	}
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
		}
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, fail("private_directory_unavailable")
		}
		fd = next
		current = filepath.Join(current, component)
		var observed unix.Stat_t
		if unix.Fstat(fd, &observed) != nil || observed.Uid != 0 || observed.Gid != 0 || observed.Mode&0022 != 0 ||
			(current == operationRoot && observed.Mode&0777 != 0700) {
			_ = unix.Close(fd)
			return nil, fail("private_directory_rejected")
		}
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&0777 != 0700 || stat.Uid != uint32(uid) || stat.Gid != 0 {
		_ = unix.Close(fd)
		return nil, fail("private_directory_rejected")
	}
	return os.NewFile(uintptr(fd), "private-directory"), nil
}

func readIdentity(ctx context.Context, tx pgx.Tx, opt options) (databaseIdentity, error) {
	var identity databaseIdentity
	var sessionRole, schemaOwner string
	var safe, ownerCommentMatches bool
	var backendPID int
	var postmasterStartMicroseconds int64
	if err := tx.QueryRow(ctx, `SELECT current_database(),d.oid,d.datdba,current_user,r.oid,session_user,n.oid,n.nspowner,
		pg_catalog.pg_get_userbyid(n.nspowner),current_setting('port')::integer,
		current_setting('server_version_num')::integer,
		NOT (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls)
		AND r.rolcanlogin AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid),
		($1='' OR (pg_catalog.shobj_description(d.oid,'pg_database')=$1 AND pg_catalog.shobj_description(r.oid,'pg_authid')=$1)),
		pg_catalog.pg_backend_pid(),(extract(epoch FROM pg_catalog.pg_postmaster_start_time())*1000000)::bigint
		FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles r ON r.rolname=current_user
		CROSS JOIN pg_catalog.pg_namespace n
		WHERE d.datname=current_database() AND n.nspname='public'`, opt.expectedOwnerComment).Scan(&identity.Database, &identity.DatabaseOID, &identity.DatabaseOwnerOID,
		&identity.Role, &identity.RoleOID, &sessionRole, &identity.SchemaOID, &identity.SchemaOwnerOID, &schemaOwner,
		&identity.Port, &identity.PostgreSQLVersionNum, &safe, &ownerCommentMatches, &backendPID, &postmasterStartMicroseconds); err != nil {
		return identity, fail("identity_unavailable")
	}
	if err := verifyProcessProof(opt.proof, backendPID, postmasterStartMicroseconds); err != nil {
		return identity, err
	}
	identity.SystemIdentifier = opt.proof.SystemIdentifier
	identity.PostmasterPID, identity.PostmasterStartTicks = opt.proof.PostmasterPID, opt.proof.PostmasterStartTicks
	identity.PostmasterStartMicroseconds, identity.BootID = opt.proof.PostmasterStartMicroseconds, opt.proof.BootID
	if !safe || identity.Database != opt.expectedDatabase || identity.DatabaseOID != uint32(opt.expectedDatabaseOID) ||
		identity.Role != opt.expectedRole || identity.RoleOID != uint32(opt.expectedRoleOID) || sessionRole != identity.Role ||
		identity.DatabaseOwnerOID != identity.RoleOID || identity.SystemIdentifier != opt.expectedSystem || identity.Port != opt.expectedPort ||
		identity.PostgreSQLVersionNum != opt.proof.ServerVersion {
		return identity, fail("identity_mismatch")
	}
	switch {
	case identity.SchemaOwnerOID == identity.RoleOID:
		identity.SchemaOwnerKind = "database_role"
	case schemaOwner == "pg_database_owner":
		identity.SchemaOwnerKind = "pg_database_owner"
	default:
		return identity, fail("schema_owner_mismatch")
	}
	if opt.rehearsal && !ownerCommentMatches {
		return identity, fail("rehearsal_owner_mismatch")
	}
	var databaseGrants, schemaGrants []byte
	var err error
	if err := tx.QueryRow(ctx, `SELECT d.datacl::text,n.nspacl::text,`+
		aclGrantsSQL("d.datacl", "pg_catalog.acldefault('d',d.datdba)", "d.datdba")+`,`+
		aclGrantsSQL("n.nspacl", "pg_catalog.acldefault('n',n.nspowner)", "n.nspowner")+`
		FROM pg_catalog.pg_database d CROSS JOIN pg_catalog.pg_namespace n
		WHERE d.datname=current_database() AND n.nspname='public'`).Scan(
		&identity.DatabaseACL.Raw, &identity.SchemaACL.Raw, &databaseGrants, &schemaGrants); err != nil {
		return identity, fail("database_acl_capture_failed")
	}
	identity.DatabaseACL.Grants, err = decodeACLGrants(databaseGrants)
	if err != nil {
		return identity, err
	}
	identity.SchemaACL.Grants, err = decodeACLGrants(schemaGrants)
	if err != nil {
		return identity, err
	}
	return identity, nil
}

// Raw ACLs remain part of every in-place comparison. Expanded privileges only
// permit the source-to-rehearsal comparison to map its new owner role without
// hiding a grant, grant option, PUBLIC privilege, or unrelated principal.
// All expression arguments are fixed helper SQL, never command-line values.
func aclGrantsSQL(acl, fallback, owner string) string {
	principal := func(field string) string {
		return `CASE WHEN grant_entry.` + field + `=0 THEN '@public'
		WHEN grant_entry.` + field + `=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user) THEN '@database_role'
		WHEN grant_entry.` + field + `=` + owner + ` THEN '@object_owner'
		ELSE 'role:'||grant_entry.` + field + `::text||':'||pg_catalog.pg_get_userbyid(grant_entry.` + field + `) END`
	}
	return `(SELECT COALESCE(jsonb_agg(jsonb_build_object('grantor',` + principal("grantor") +
		`,'grantee',` + principal("grantee") + `,'privilege',grant_entry.privilege_type,
		'grantable',grant_entry.is_grantable)),'[]'::jsonb)
		FROM pg_catalog.aclexplode(COALESCE(` + acl + `,` + fallback + `)) grant_entry)`
}

func decodeACLGrants(raw []byte) ([]aclGrant, error) {
	var grants []aclGrant
	if len(raw) > 1<<20 || json.Unmarshal(raw, &grants) != nil || grants == nil {
		return nil, fail("acl_capture_failed")
	}
	sort.Slice(grants, func(i, j int) bool {
		left, right := grants[i], grants[j]
		if left.Grantor != right.Grantor {
			return left.Grantor < right.Grantor
		}
		if left.Grantee != right.Grantee {
			return left.Grantee < right.Grantee
		}
		if left.Privilege != right.Privilege {
			return left.Privilege < right.Privilege
		}
		return !left.Grantable && right.Grantable
	})
	return grants, nil
}

func captureRelations(ctx context.Context, tx pgx.Tx, owner uint32) ([]relationState, error) {
	rows, err := tx.Query(ctx, `SELECT c.relname,c.oid,c.relowner,c.relkind::text,c.relacl::text,`+
		aclGrantsSQL("c.relacl", "pg_catalog.acldefault(CASE WHEN c.relkind='S' THEN 'S'::\"char\" ELSE 'r'::\"char\" END,c.relowner)", "c.relowner")+`
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' ORDER BY c.relname COLLATE "C"`)
	if err != nil {
		return nil, fail("relation_capture_failed")
	}
	result := []relationState{}
	indices := make(map[string]int)
	for rows.Next() {
		var relation relationState
		var grants []byte
		if rows.Scan(&relation.Name, &relation.OID, &relation.OwnerOID, &relation.Kind, &relation.ACL.Raw, &grants) != nil ||
			!namePattern.MatchString(relation.Name) || relation.OID == 0 || relation.OwnerOID != owner ||
			(relation.Kind != "r" && relation.Kind != "i" && relation.Kind != "S") {
			rows.Close()
			return nil, fail("relation_capture_failed")
		}
		relation.ACL.Grants, err = decodeACLGrants(grants)
		if err != nil {
			rows.Close()
			return nil, err
		}
		relation.ColumnACLs = []columnACLState{}
		indices[relation.Name] = len(result)
		result = append(result, relation)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, fail("relation_capture_failed")
	}
	// A NULL column ACL has no explicit grants. Preserve NULL for aclexplode:
	// PostgreSQL rejects a fabricated zero-dimensional '{}' ACL array.
	rows, err = tx.Query(ctx, `SELECT c.relname,a.attname,a.attnum,a.attacl::text,`+
		aclGrantsSQL("a.attacl", "NULL::pg_catalog.aclitem[]", "c.relowner")+`
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
		WHERE n.nspname='public' AND a.attnum>0 AND NOT a.attisdropped
		ORDER BY c.relname COLLATE "C",a.attnum`)
	if err != nil {
		return nil, fail("column_acl_capture_failed")
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var column columnACLState
		var grants []byte
		if rows.Scan(&name, &column.Name, &column.Number, &column.ACL.Raw, &grants) != nil ||
			!namePattern.MatchString(column.Name) || column.Number < 1 {
			return nil, fail("column_acl_capture_failed")
		}
		index, exists := indices[name]
		if !exists {
			return nil, fail("column_acl_capture_failed")
		}
		column.ACL.Grants, err = decodeACLGrants(grants)
		if err != nil {
			return nil, err
		}
		result[index].ColumnACLs = append(result[index].ColumnACLs, column)
	}
	if rows.Err() != nil {
		return nil, fail("column_acl_capture_failed")
	}
	return result, nil
}

func captureState(ctx context.Context, tx pgx.Tx, opt options, version int64) (capturedState, error) {
	state := capturedState{SchemaVersion: version, TrustedCatalogVerified: true, RowHashFormat: "goby-canonical-jsonb-sha256-multiset-v1"}
	var actualVersion int64
	if tx.QueryRow(ctx, "SELECT COALESCE(max(version),0) FROM public.schema_migrations").Scan(&actualVersion) != nil || actualVersion != version {
		return state, fail("schema_version_mismatch")
	}
	var err error
	state.Identity, err = readIdentity(ctx, tx, opt)
	if err != nil {
		return state, err
	}
	state.MigrationPrefixSHA256, err = migrationDigest(version)
	if err != nil {
		return state, err
	}
	rows, err := tx.Query(ctx, `SELECT c.relname,c.oid,c.relowner,a.attname,a.attnum,a.atttypid,a.atttypmod,
		pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attgenerated::text,a.attidentity::text,a.attcollation
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
		WHERE n.nspname='public' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped
		ORDER BY c.relname COLLATE "C",a.attnum`)
	if err != nil {
		return state, fail("column_capture_failed")
	}
	for rows.Next() {
		var name string
		var oid, owner uint32
		var column columnState
		if rows.Scan(&name, &oid, &owner, &column.Name, &column.Number, &column.TypeOID, &column.TypeModifier, &column.Type,
			&column.NotNull, &column.Generated, &column.Identity, &column.CollationOID) != nil || !namePattern.MatchString(name) || !namePattern.MatchString(column.Name) {
			rows.Close()
			return state, fail("column_capture_failed")
		}
		if len(state.Tables) == 0 || state.Tables[len(state.Tables)-1].Name != name {
			state.Tables = append(state.Tables, tableState{Name: name, OID: oid, OwnerOID: owner})
		}
		state.Tables[len(state.Tables)-1].Columns = append(state.Tables[len(state.Tables)-1].Columns, column)
	}
	rows.Close()
	if rows.Err() != nil {
		return state, fail("column_capture_failed")
	}
	wantTables := 33
	if version == 27 {
		wantTables = 35
	}
	if len(state.Tables) != wantTables {
		return state, fail("table_inventory_mismatch")
	}
	for index := range state.Tables {
		table := &state.Tables[index]
		if table.OwnerOID != state.Identity.RoleOID {
			return state, fail("table_owner_mismatch")
		}
		table.Rows, table.SHA256, err = rowMultiset(ctx, tx, *table, 0)
		if err != nil {
			return state, err
		}
	}
	state.Sequences, err = captureSequences(ctx, tx, state.Identity.RoleOID)
	if err != nil {
		return state, err
	}
	state.Relations, err = captureRelations(ctx, tx, state.Identity.RoleOID)
	if err != nil {
		return state, err
	}
	state.ExpectedExtraPaths.Rows, state.ExpectedExtraPaths.SHA256, err = projectionMultiset(ctx, tx, expectedExtraPathsSQL)
	if err != nil {
		return state, err
	}
	state.Binding, err = captureBinding(ctx, tx, opt.expectedDeployment)
	return state, err
}

func rowMultiset(ctx context.Context, tx pgx.Tx, table tableState, historyPrefix int64) (int64, string, error) {
	columns := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		columns[index] = pgx.Identifier{column.Name}.Sanitize()
	}
	if len(columns) == 0 || !namePattern.MatchString(table.Name) {
		return 0, "", fail("row_projection_invalid")
	}
	projection := "SELECT " + strings.Join(columns, ",") + " FROM " + pgx.Identifier{"public", table.Name}.Sanitize()
	if table.Name == "schema_migrations" && historyPrefix > 0 {
		projection += " WHERE version<=" + strconv.FormatInt(historyPrefix, 10)
	}
	return projectionMultiset(ctx, tx, projection)
}

func projectionMultiset(ctx context.Context, tx pgx.Tx, projection string) (int64, string, error) {
	// Only fixed-size row digests and their multiplicities leave PostgreSQL.
	// Include generated columns and frame duplicate counts in the final hash.
	rows, err := tx.Query(ctx, `SELECT row_digest,count(*) FROM (SELECT
		CASE WHEN pg_catalog.octet_length(pg_catalog.to_jsonb(original)::text)<=33554432
		THEN pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(pg_catalog.to_jsonb(original)::text,'UTF8')),'hex')
		ELSE NULL END AS row_digest FROM (`+projection+`) original) hashed
		GROUP BY row_digest ORDER BY row_digest COLLATE "C"`)
	if err != nil {
		return 0, "", fail("row_capture_failed")
	}
	defer rows.Close()
	hash := sha256.New()
	_, _ = io.WriteString(hash, "goby-row-multiset-v1\x00")
	var total int64
	var countBytes [8]byte
	for rows.Next() {
		var digest *string
		var count int64
		if rows.Scan(&digest, &count) != nil || digest == nil || !digestPattern.MatchString(*digest) || count < 1 || total > 9223372036854775807-count {
			return 0, "", fail("row_capture_failed")
		}
		decoded, err := hex.DecodeString(*digest)
		if err != nil {
			return 0, "", fail("row_capture_failed")
		}
		_, _ = hash.Write(decoded)
		binary.BigEndian.PutUint64(countBytes[:], uint64(count))
		_, _ = hash.Write(countBytes[:])
		total += count
	}
	if rows.Err() != nil {
		return 0, "", fail("row_capture_failed")
	}
	return total, hex.EncodeToString(hash.Sum(nil)), nil
}

func captureSequences(ctx context.Context, tx pgx.Tx, owner uint32) ([]sequenceState, error) {
	rows, err := tx.Query(ctx, `SELECT c.relname,c.oid,c.relowner FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='S' ORDER BY c.relname COLLATE "C"`)
	if err != nil {
		return nil, fail("sequence_capture_failed")
	}
	result := []sequenceState{}
	for rows.Next() {
		var sequence sequenceState
		if rows.Scan(&sequence.Name, &sequence.OID, &sequence.OwnerOID) != nil || !namePattern.MatchString(sequence.Name) || sequence.OwnerOID != owner {
			rows.Close()
			return nil, fail("sequence_capture_failed")
		}
		result = append(result, sequence)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, fail("sequence_capture_failed")
	}
	for index := range result {
		if tx.QueryRow(ctx, "SELECT last_value::text,log_cnt::text,is_called FROM "+pgx.Identifier{"public", result[index].Name}.Sanitize()).Scan(&result[index].LastValue, &result[index].LogCount, &result[index].IsCalled) != nil {
			return nil, fail("sequence_capture_failed")
		}
	}
	return result, nil
}

func captureBinding(ctx context.Context, tx pgx.Tx, deployment string) (bindingState, error) {
	result := bindingState{DeploymentID: deployment, GenerationID: "", Slot: "primary"}
	expected, err := recoverydb.EncodeMarker(recoverydb.Marker{Version: 1, DeploymentID: deployment, GenerationID: "", Slot: lifecycle.DatabasePrimary})
	if err != nil {
		return result, fail("binding_expectation_invalid")
	}
	var count int
	if tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(value,'UTF8')),'hex')),'')
		FROM public.server_settings WHERE key=$1 AND value=$2`, recoverydb.MarkerKey, expected).Scan(&count, &result.SHA256) != nil || count != 1 || !digestPattern.MatchString(result.SHA256) {
		return result, fail("binding_mismatch")
	}
	return result, nil
}

func migrationDigest(version int64) (string, error) {
	available, err := database.EmbeddedMigrations()
	if err != nil {
		return "", fail("embedded_migrations_unavailable")
	}
	prefix := make([]database.RecoveryMigration, 0, version)
	for _, migration := range available {
		if migration.Version <= version {
			prefix = append(prefix, migration)
		}
	}
	if int64(len(prefix)) != version || prefix[len(prefix)-1].Version != version {
		return "", fail("embedded_migrations_unavailable")
	}
	encoded, err := json.Marshal(prefix)
	if err != nil {
		return "", fail("state_encoding_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sameBaseline(expected, actual capturedState, rehearsal bool) bool {
	if rehearsal {
		expected, actual = logicalState(expected), logicalState(actual)
	}
	return reflect.DeepEqual(expected, actual)
}

func logicalState(state capturedState) capturedState {
	state.Identity.Database, state.Identity.Role = "", ""
	state.Identity.DatabaseOID, state.Identity.DatabaseOwnerOID, state.Identity.RoleOID, state.Identity.SchemaOID, state.Identity.SchemaOwnerOID = 0, 0, 0, 0, 0
	state.Identity.DatabaseACL.Raw, state.Identity.SchemaACL.Raw = nil, nil
	state.Tables = append([]tableState(nil), state.Tables...)
	for index := range state.Tables {
		state.Tables[index].OID, state.Tables[index].OwnerOID = 0, 0
	}
	state.Sequences = append([]sequenceState(nil), state.Sequences...)
	for index := range state.Sequences {
		state.Sequences[index].OID, state.Sequences[index].OwnerOID = 0, 0
		// pg_dump/setval preserve the logical value and called flag, not the
		// original cluster's internal WAL preallocation counter. In-place
		// comparisons still require this physical counter to remain identical.
		state.Sequences[index].LogCount = ""
	}
	state.Relations = append([]relationState(nil), state.Relations...)
	for index := range state.Relations {
		relation := &state.Relations[index]
		relation.OID, relation.OwnerOID, relation.ACL.Raw = 0, 0, nil
		relation.ColumnACLs = append([]columnACLState(nil), relation.ColumnACLs...)
		for column := range relation.ColumnACLs {
			relation.ColumnACLs[column].ACL.Raw = nil
		}
	}
	return state
}

func verifyPreserved(ctx context.Context, tx pgx.Tx, before, after capturedState) (string, error) {
	if before.SchemaVersion != 26 || after.SchemaVersion != 27 || !reflect.DeepEqual(before.Identity, after.Identity) ||
		before.Binding != after.Binding || len(before.Tables) != 33 || len(after.Tables) != 35 ||
		before.ExpectedExtraPaths != after.ExpectedExtraPaths || !oldSequencesPreserved(before.Sequences, after.Sequences) {
		return "", fail("historical_state_changed")
	}
	addedRelations := map[string]string{
		"extra_reserved_paths": "r", "extra_reserved_paths_pkey": "i",
		"item_extra_resources": "r", "item_extra_resources_pkey": "i", "item_extra_resources_owner_idx": "i",
	}
	oldRelations := make(map[string]relationState, len(before.Relations))
	for _, relation := range before.Relations {
		oldRelations[relation.Name] = relation
	}
	for _, relation := range after.Relations {
		if original, exists := oldRelations[relation.Name]; exists {
			if !reflect.DeepEqual(original, relation) {
				return "", fail("historical_relation_identity_or_acl_changed")
			}
			delete(oldRelations, relation.Name)
			continue
		}
		kind, exists := addedRelations[relation.Name]
		if !exists || relation.Kind != kind || relation.OID == 0 || relation.OwnerOID != after.Identity.RoleOID || relation.ACL.Raw != nil {
			return "", fail("new_relation_mismatch")
		}
		for _, column := range relation.ColumnACLs {
			if column.ACL.Raw != nil {
				return "", fail("new_relation_acl_mismatch")
			}
		}
		delete(addedRelations, relation.Name)
	}
	if len(oldRelations) != 0 || len(addedRelations) != 0 {
		return "", fail("relation_inventory_mismatch")
	}
	actualTables := make(map[string]tableState, len(after.Tables))
	for _, table := range after.Tables {
		actualTables[table.Name] = table
	}
	for _, original := range before.Tables {
		current, exists := actualTables[original.Name]
		if !exists || current.OID != original.OID || current.OwnerOID != original.OwnerOID ||
			!reflect.DeepEqual(current.Columns, original.Columns) {
			return "", fail("historical_columns_changed")
		}
		count, digest, err := rowMultiset(ctx, tx, current, 26)
		if err != nil {
			return "", err
		}
		if count != original.Rows || digest != original.SHA256 {
			return "", fail("historical_rows_changed")
		}
		delete(actualTables, original.Name)
	}
	if len(actualTables) != 2 || actualTables["extra_reserved_paths"].Name != "extra_reserved_paths" ||
		actualTables["item_extra_resources"].Name != "item_extra_resources" {
		return "", fail("new_table_mismatch")
	}
	// Sequences are not MVCC snapshot data. Observe them again after every old
	// row comparison; the external operator must prevent direct nextval users.
	sequences, err := captureSequences(ctx, tx, before.Identity.RoleOID)
	if err != nil || !reflect.DeepEqual(sequences, after.Sequences) || !oldSequencesPreserved(before.Sequences, sequences) {
		return "", fail("historical_sequences_changed")
	}
	return stateDigest(before)
}

func oldSequencesPreserved(before, after []sequenceState) bool {
	return reflect.DeepEqual(before, after)
}

func verifyNewExtraState(ctx context.Context, tx pgx.Tx, before, after capturedState) (extraTransitionState, error) {
	var result extraTransitionState
	if err := database.ValidateExtraState(ctx, tx, 27); err != nil {
		return result, fail("new_extra_semantics_mismatch")
	}
	for _, table := range after.Tables {
		switch table.Name {
		case "extra_reserved_paths":
			result.ReservedPathCount, result.ReservedPathsSHA256 = table.Rows, table.SHA256
		case "item_extra_resources":
			result.ResourceCount = table.Rows
		}
	}
	result.ExpectedPathsSHA256 = before.ExpectedExtraPaths.SHA256
	result.OldSequencesUnchanged = oldSequencesPreserved(before.Sequences, after.Sequences)
	if result.ResourceCount != 0 || result.ReservedPathCount != before.ExpectedExtraPaths.Rows ||
		result.ReservedPathsSHA256 != result.ExpectedPathsSHA256 || !result.OldSequencesUnchanged {
		return result, fail("new_extra_reservations_mismatch")
	}
	return result, nil
}
func stateDigest(state capturedState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fail("state_encoding_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func readBaseline(opt options) (capturedState, error) {
	var empty capturedState
	data, err := readPrivateBytes(opt.baselinePath, opt.expectedUID, maximumReportBytes)
	if err != nil {
		return empty, fail("baseline_file_rejected")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != opt.baselineSHA256 {
		return empty, fail("baseline_digest_mismatch")
	}
	var value report
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Schema != reportSchema || value.Version != 1 ||
		value.Mode != "inspect" || value.Status != "inspected" || value.Rehearsal ||
		value.SourceSchemaVersion != 26 || value.TargetSchemaVersion != 27 ||
		value.State.SchemaVersion != 26 || !value.State.TrustedCatalogVerified || value.State.Identity.Database != mainDatabase ||
		value.State.Identity.DatabaseOID != mainDatabaseOID || value.State.Identity.Role != mainDatabase || value.State.Identity.RoleOID != mainRoleOID ||
		value.State.Identity.SystemIdentifier != mainSystemIdentifier || value.State.Identity.Port != 5432 {
		return empty, fail("baseline_report_rejected")
	}
	if value.RunID != opt.runID || value.SourceManifestSHA256 != opt.sourceManifestSHA256 || value.ToolManifestSHA256 != opt.toolManifestSHA256 || value.CandidateSHA256 != opt.candidateSHA256 ||
		value.HelperSHA256 != opt.helperSHA256 || !digestPattern.MatchString(value.ClusterProofSHA256) {
		return empty, fail("baseline_run_mismatch")
	}
	if value.Target != (targetIdentity{Database: mainDatabase, DatabaseOID: mainDatabaseOID, Role: mainDatabase, RoleOID: mainRoleOID}) {
		return empty, fail("baseline_target_mismatch")
	}
	actualDigest, err := stateDigest(value.State)
	if err != nil || actualDigest != value.StateSHA256 {
		return empty, fail("baseline_state_digest_mismatch")
	}
	return value.State, nil
}

type privateOutput struct {
	directory *os.File
	file      *os.File
	temporary string
	name      string
	published bool
}

func reserveOutput(path string, uid int) (*privateOutput, error) {
	directory, name := filepath.Dir(path), filepath.Base(path)
	if name == "." || name == ".." || name == "" {
		return nil, fail("output_path_rejected")
	}
	dir, err := openPrivateDirectory(directory, uid)
	if err != nil {
		return nil, fail("output_directory_unavailable")
	}
	dirFD := int(dir.Fd())
	output := &privateOutput{directory: dir, name: name}
	var stat unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		output.close()
		return nil, fail("output_already_exists")
	}
	reservation, err := unix.Openat(dirFD, "."+name+".reserved", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		output.close()
		return nil, fail("output_reservation_rejected")
	}
	reserved := os.NewFile(uintptr(reservation), "private-report-reservation")
	reservationBytes := []byte(reportSchema + "\n" + name + "\n")
	written, writeErr := reserved.Write(reservationBytes)
	if writeErr != nil || written != len(reservationBytes) || reserved.Sync() != nil || reserved.Close() != nil || dir.Sync() != nil {
		_ = reserved.Close()
		output.close()
		return nil, fail("output_reservation_failed")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		output.close()
		return nil, fail("output_prepare_failed")
	}
	temporary := ".main-schema27-report-" + hex.EncodeToString(nonce[:]) + ".pending"
	fd, err := unix.Openat(dirFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		output.close()
		return nil, fail("output_prepare_failed")
	}
	output.temporary = temporary
	output.file = os.NewFile(uintptr(fd), "private-report")
	if unix.Fstat(fd, &stat) != nil || stat.Mode&0777 != 0600 || stat.Uid != uint32(uid) || stat.Gid != 0 || stat.Nlink != 1 {
		output.close()
		return nil, fail("output_file_rejected")
	}
	return output, nil
}

func (output *privateOutput) stage(value report) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(encoded)+1 > maximumReportBytes {
		return fail("report_encoding_failed")
	}
	encoded = append(encoded, '\n')
	if _, err := output.file.Write(encoded); err != nil || output.file.Sync() != nil {
		return fail("report_write_failed")
	}
	return nil
}

func (output *privateOutput) publish() error {
	dirFD := int(output.directory.Fd())
	if unix.Renameat2(dirFD, output.temporary, dirFD, output.name, unix.RENAME_NOREPLACE) != nil {
		return fail("report_publish_failed")
	}
	output.published = true
	if output.directory.Sync() != nil {
		return fail("report_publish_failed")
	}
	return nil
}

func (output *privateOutput) close() {
	if output == nil {
		return
	}
	if output.file != nil {
		_ = output.file.Close()
	}
	if output.directory != nil {
		// Failed or uncertain work retains both its fixed reservation and any
		// staged report. A later call cannot reuse this output as an automatic retry.
		_ = output.directory.Close()
	}
}
