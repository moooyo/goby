//go:build ignore

// This Linux helper owns no service, role, database creation, or disposal.
// It reads the pinned main, rehearses only in a separately owned target,
// and applies the embedded schema27-to28 migration in one guarded transaction.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/recoverydb"
	"golang.org/x/sys/unix"
)

const (
	reportMarker         = "goby-main-schema28-helper-v1"
	intentMarker         = "goby-main-schema28-helper-intent-v1"
	targetMarker         = "goby-main-schema28-rehearsal-target-v1"
	ownerMarker          = "goby-main-schema28-source55-upgrade-v1"
	workRoot             = "/opt/goby-test/exec-work-m3e"
	productRoot          = workRoot + "/source-attempt-55"
	evidenceBase         = "/opt/goby-test/backups/main-schema28-v1"
	mainName             = "goby_test"
	mainDatabaseOID      = 16385
	mainRoleOID          = 16384
	mainSchemaOID        = 2200
	mainSchemaOwnerOID   = 6171
	mainSystemIdentifier = "7683277964552005578"
	mainDeploymentID     = "f58d5e0c8ff49fca916499e666bffd9f"
	lifecycleDirectory   = "/var/lib/goby-test/recovery-m5j"
	lifecycleLockFile    = lifecycleDirectory + "/.goby-lifecycle.lock"
	lifecycleUID         = 995
	dataDirectory        = "/var/lib/postgresql/17/main"
	sourceManifest       = "7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a"
	catalog27SHA         = "1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d"
	catalog28SHA         = "8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b"
	maximumJSON          = 16 << 20
	maximumDump          = 512 << 20
)

var (
	digestPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	runPattern        = regexp.MustCompile(`^[0-9]{8}_[0-9]{6}_[0-9a-f]{12}$`)
	bootPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reportNamePattern = regexp.MustCompile(`^helper-[a-z0-9-]+\.json$`)
	expectedSequences = []string{"activity_entries_id_seq", "application_keys_id_seq", "catalog_entities_id_seq", "devices_id_seq", "theme_owner_ids_id_seq"}
)

type operationFailure struct{ code, outcome string }

func (failure operationFailure) Error() string { return failure.code }
func fail(code string) error                   { return operationFailure{code: code, outcome: "failed"} }
func require(value bool, code string) error {
	if !value {
		return fail(code)
	}
	return nil
}

type filePin struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type clusterProof struct {
	SystemIdentifier            string `json:"system_identifier"`
	PostmasterPID               int    `json:"postmaster_pid"`
	PostmasterStartTicks        string `json:"postmaster_start_ticks"`
	PostmasterStartMicroseconds int64  `json:"postmaster_start_microseconds"`
	BootID                      string `json:"boot_id"`
	DataDirectory               string `json:"data_directory"`
	PostgreSQLVersionNum        int    `json:"postgresql_version_num"`
	Port                        int    `json:"port"`
}
type targetIdentity struct {
	Database              string `json:"database"`
	Role                  string `json:"role"`
	DatabaseOID           uint32 `json:"database_oid"`
	RoleOID               uint32 `json:"role_oid"`
	SchemaOID             uint32 `json:"schema_oid"`
	SchemaOwnerOID        uint32 `json:"schema_owner_oid"`
	RecoveryDeploymentID  string `json:"recovery_deployment_id,omitempty"`
	RecoveryBindingSHA256 string `json:"recovery_binding_sha256,omitempty"`
	OwnerTag              string `json:"owner_tag,omitempty"`
}
type lifecycleFileIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
}
type lifecycleScope struct {
	Directory         string                `json:"directory"`
	LockFile          string                `json:"lock_file"`
	DirectoryIdentity lifecycleFileIdentity `json:"directory_identity"`
	LockIdentity      lifecycleFileIdentity `json:"lock_identity"`
}
type rehearsalScope struct {
	Database string `json:"database"`
	Role     string `json:"role"`
	OwnerTag string `json:"owner_tag"`
}
type intentDocument struct {
	Marker               string         `json:"marker"`
	Version              int            `json:"version"`
	RunID                string         `json:"run_id"`
	SourceManifestSHA256 string         `json:"source_manifest_sha256"`
	Source55BinarySHA256 string         `json:"source55_binary_sha256"`
	HelperSHA256         string         `json:"helper_sha256"`
	EvidenceRoot         string         `json:"evidence_root"`
	Catalog27            filePin        `json:"catalog27"`
	Catalog28            filePin        `json:"catalog28"`
	Cluster              clusterProof   `json:"cluster"`
	Main                 targetIdentity `json:"main"`
	Rehearsal            rehearsalScope `json:"rehearsal"`
	Lifecycle            lifecycleScope `json:"lifecycle"`
}
type targetReceipt struct {
	Marker         string `json:"marker"`
	Version        int    `json:"version"`
	RunID          string `json:"run_id"`
	IntentSHA256   string `json:"intent_sha256"`
	Database       string `json:"database"`
	Role           string `json:"role"`
	DatabaseOID    uint32 `json:"database_oid"`
	RoleOID        uint32 `json:"role_oid"`
	SchemaOID      uint32 `json:"schema_oid"`
	SchemaOwnerOID uint32 `json:"schema_owner_oid"`
	OwnerTag       string `json:"owner_tag"`
}
type credentials struct {
	MainURL      string `json:"main_url"`
	RehearsalURL string `json:"rehearsal_url"`
}
type options struct {
	mode, intentPath, intentSHA, credentialsPath, output, baselinePath, baselineSHA, targetPath, targetSHA string
	expectedSchema                                                                                         int64
	lifecycleDirectoryFD, lifecycleLockFD                                                                  int
	lifecycleDirectoryFDSet, lifecycleLockFDSet                                                            bool
	intent                                                                                                 intentDocument
	private                                                                                                string
	baseline                                                                                               report
	beforeCredentials                                                                                      []byte
	catalogs                                                                                               map[int64]catalogDocument
}
type catalogDocument struct {
	Version         int64                        `json:"version"`
	PostgreSQLMajor int                          `json:"postgresql_major"`
	Migrations      []backupformat.MigrationFact `json:"migrations"`
	Catalog         backuppg.Catalog             `json:"catalog"`
	Objects         json.RawMessage              `json:"objects"`
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
type checkState struct {
	Table      string   `json:"table"`
	Name       string   `json:"name"`
	OID        uint32   `json:"oid"`
	Columns    []string `json:"columns"`
	Definition string   `json:"definition"`
	Validated  bool     `json:"validated"`
	Deferrable bool     `json:"deferrable"`
	Deferred   bool     `json:"deferred"`
	NoInherit  bool     `json:"no_inherit"`
}
type databaseIdentity struct {
	Database         string          `json:"database"`
	Role             string          `json:"role"`
	DatabaseOID      uint32          `json:"database_oid"`
	DatabaseOwnerOID uint32          `json:"database_owner_oid"`
	RoleOID          uint32          `json:"role_oid"`
	SchemaOID        uint32          `json:"schema_oid"`
	SchemaOwnerOID   uint32          `json:"schema_owner_oid"`
	SchemaOwnerKind  string          `json:"schema_owner_kind"`
	Cluster          clusterProof    `json:"cluster"`
	DatabaseACL      aclState        `json:"database_acl"`
	SchemaACL        aclState        `json:"schema_acl"`
	RoleProperties   json.RawMessage `json:"role_properties"`
}

// Validate the exact pg_roles projection without converting its values back
// through interface{} or dropping any field. Compact only JSON whitespace so
// PostgreSQL jsonb output and the retained report use identical raw bytes.
func normalizeRoleProperties(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if strictJSON(raw, &fields) != nil || len(fields) != 10 {
		return nil, fail("role_properties_invalid")
	}
	for _, name := range []string{"login", "super", "createdb", "createrole", "replication", "bypass", "inherit"} {
		value, exists := fields[name]
		var flag bool
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || strictJSON(value, &flag) != nil {
			return nil, fail("role_properties_invalid")
		}
	}
	var limit int32
	if value, exists := fields["limit"]; !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || strictJSON(value, &limit) != nil {
		return nil, fail("role_properties_invalid")
	}
	configuration, exists := fields["config"]
	if !exists {
		return nil, fail("role_properties_invalid")
	}
	if !bytes.Equal(bytes.TrimSpace(configuration), []byte("null")) {
		var settings []json.RawMessage
		if strictJSON(configuration, &settings) != nil {
			return nil, fail("role_properties_invalid")
		}
		for _, setting := range settings {
			var value string
			if bytes.Equal(bytes.TrimSpace(setting), []byte("null")) || strictJSON(setting, &value) != nil {
				return nil, fail("role_properties_invalid")
			}
		}
	}
	validUntil, exists := fields["valid_until"]
	if !exists {
		return nil, fail("role_properties_invalid")
	}
	if !bytes.Equal(bytes.TrimSpace(validUntil), []byte("null")) {
		var timestamp string
		if strictJSON(validUntil, &timestamp) != nil {
			return nil, fail("role_properties_invalid")
		}
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return nil, fail("role_properties_invalid")
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}

func sameDatabaseIdentity(first, second databaseIdentity) bool {
	var firstErr, secondErr error
	first.RoleProperties, firstErr = normalizeRoleProperties(first.RoleProperties)
	second.RoleProperties, secondErr = normalizeRoleProperties(second.RoleProperties)
	return firstErr == nil && secondErr == nil && reflect.DeepEqual(first, second)
}

func sameCapturedState(first, second capturedState) bool {
	var firstErr, secondErr error
	first.Identity.RoleProperties, firstErr = normalizeRoleProperties(first.Identity.RoleProperties)
	second.Identity.RoleProperties, secondErr = normalizeRoleProperties(second.Identity.RoleProperties)
	return firstErr == nil && secondErr == nil && reflect.DeepEqual(first, second)
}

type bindingState struct {
	DeploymentID string `json:"deployment_id"`
	GenerationID string `json:"generation_id"`
	Slot         string `json:"slot"`
	SHA256       string `json:"sha256"`
}
type profileState struct {
	Sessions           int64  `json:"sessions"`
	Devices            int64  `json:"devices"`
	ActivityEntries    int64  `json:"activity_entries"`
	LibraryRoots       int64  `json:"library_roots"`
	RootMappingsSHA256 string `json:"root_mappings_sha256"`
}
type capturedState struct {
	SchemaVersion          int64            `json:"schema_version"`
	TrustedCatalogVerified bool             `json:"trusted_catalog_verified"`
	CatalogSHA256          string           `json:"catalog_sha256"`
	MigrationPrefixSHA256  string           `json:"migration_prefix_sha256"`
	RowHashFormat          string           `json:"row_hash_format"`
	Identity               databaseIdentity `json:"identity"`
	Tables                 []tableState     `json:"tables"`
	Sequences              []sequenceState  `json:"sequences"`
	Relations              []relationState  `json:"relations"`
	Checks                 []checkState     `json:"checks"`
	RecoveryBinding        bindingState     `json:"recovery_binding"`
	Profile                profileState     `json:"profile"`
}
type dumpDescriptor struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type report struct {
	Marker                    string                    `json:"marker"`
	Version                   int                       `json:"version"`
	Mode                      string                    `json:"mode"`
	RunID                     string                    `json:"run_id"`
	IntentSHA256              string                    `json:"intent_sha256"`
	SourceManifestSHA256      string                    `json:"source_manifest_sha256"`
	Source55BinarySHA256      string                    `json:"source55_binary_sha256"`
	HelperSHA256              string                    `json:"helper_sha256"`
	Status                    string                    `json:"status"`
	SourceSchemaVersion       int64                     `json:"source_schema_version"`
	TargetSchemaVersion       int64                     `json:"target_schema_version"`
	StateSHA256               string                    `json:"state_sha256"`
	State                     capturedState             `json:"state"`
	Facts                     *backupformat.SourceFacts `json:"facts,omitempty"`
	TargetFacts               *backupformat.SourceFacts `json:"target_facts,omitempty"`
	Dump                      *dumpDescriptor           `json:"dump,omitempty"`
	SameExportedSnapshot      bool                      `json:"same_exported_snapshot,omitempty"`
	BaselineSHA256            string                    `json:"baseline_sha256,omitempty"`
	BeforeStateSHA256         string                    `json:"before_state_sha256,omitempty"`
	PreservedStateSHA256      string                    `json:"preserved_state_sha256,omitempty"`
	RootBindingsNoAutoBinding bool                      `json:"root_bindings_no_auto_binding,omitempty"`
	HistoricalAuditDefaults   bool                      `json:"historical_audit_defaults,omitempty"`
	LifecycleFenceVerified    bool                      `json:"lifecycle_fence_verified,omitempty"`
}

func main() {
	result, err := run(os.Args[1:])
	if err != nil {
		failure := operationFailure{code: "helper_failed", outcome: "failed"}
		var known operationFailure
		if errors.As(err, &known) {
			failure = known
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"marker": reportMarker, "status": failure.outcome, "error": failure.code})
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(map[string]any{"marker": reportMarker, "status": result.Status,
		"run_id": result.RunID, "state_sha256": result.StateSHA256}) != nil {
		os.Exit(1)
	}
}

func parseOptions(arguments []string) (options, error) {
	var opt options
	flags := flag.NewFlagSet("migrate-main-schema28", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opt.mode, "mode", "", "One guarded operation")
	flags.StringVar(&opt.intentPath, "intent", "", "Private immutable helper intent")
	flags.StringVar(&opt.intentSHA, "intent-sha256", "", "External intent digest")
	flags.StringVar(&opt.credentialsPath, "credentials", "", "Private credential file")
	flags.StringVar(&opt.output, "output", "", "New private result file")
	flags.Int64Var(&opt.expectedSchema, "expected-schema", 27, "Expected inspection schema")
	flags.StringVar(&opt.baselinePath, "baseline", "", "This run's backup report")
	flags.StringVar(&opt.baselineSHA, "baseline-sha256", "", "Backup report digest")
	flags.StringVar(&opt.targetPath, "target-receipt", "", "New rehearsal ownership receipt")
	flags.StringVar(&opt.targetSHA, "target-receipt-sha256", "", "Rehearsal receipt digest")
	flags.IntVar(&opt.lifecycleDirectoryFD, "lifecycle-directory-fd", -1, "Inherited held lifecycle directory descriptor")
	flags.IntVar(&opt.lifecycleLockFD, "lifecycle-lock-fd", -1, "Inherited held lifecycle named-lock descriptor")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || os.Geteuid() != 0 ||
		(opt.mode != "inspect" && opt.mode != "backup" && opt.mode != "rehearse" && opt.mode != "migrate") ||
		!digestPattern.MatchString(opt.intentSHA) {
		return opt, fail("arguments_invalid")
	}
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "lifecycle-directory-fd":
			opt.lifecycleDirectoryFDSet = true
		case "lifecycle-lock-fd":
			opt.lifecycleLockFDSet = true
		}
	})
	if !validLifecycleArguments(opt) {
		return opt, fail("lifecycle_fd_arguments_invalid")
	}
	if opt.expectedSchema != 27 && (opt.expectedSchema != 28 || opt.mode != "inspect") {
		return opt, fail("schema_argument_invalid")
	}
	privatePath := filepath.Dir(opt.intentPath)
	evidencePath := filepath.Dir(privatePath)
	const evidencePrefix = "run-"
	if filepath.Base(opt.intentPath) != "helper-intent.json" || filepath.Base(privatePath) != "private" ||
		filepath.Dir(evidencePath) != evidenceBase || !strings.HasPrefix(filepath.Base(evidencePath), evidencePrefix) ||
		!runPattern.MatchString(strings.TrimPrefix(filepath.Base(evidencePath), evidencePrefix)) || filepath.Clean(opt.intentPath) != opt.intentPath {
		return opt, fail("intent_path_invalid")
	}
	raw, err := readPrivateBytes(opt.intentPath, 0, maximumJSON)
	if err != nil || sha(raw) != opt.intentSHA || strictJSON(raw, &opt.intent) != nil {
		return opt, fail("intent_invalid")
	}
	i := opt.intent
	expectedRoot := evidenceBase + "/run-" + i.RunID
	if err := validateMainScope(i); err != nil {
		return opt, err
	}
	opt.private = filepath.Join(expectedRoot, "private")
	if checkDirectory(expectedRoot, 0o700) != nil || checkDirectory(opt.private, 0o700) != nil ||
		opt.intentPath != filepath.Join(opt.private, "helper-intent.json") ||
		opt.credentialsPath != filepath.Join(opt.private, "helper-credentials.json") ||
		filepath.Dir(opt.output) != opt.private || !reportNamePattern.MatchString(filepath.Base(opt.output)) ||
		opt.output == opt.intentPath || opt.output == opt.credentialsPath || opt.output == filepath.Join(opt.private, "helper-target.json") {
		return opt, fail("private_scope_invalid")
	}
	if opt.mode == "backup" && opt.output != filepath.Join(opt.private, "helper-backup.json") ||
		opt.mode != "backup" && opt.output == filepath.Join(opt.private, "helper-backup.json") {
		return opt, fail("backup_output_invalid")
	}
	needsBaseline := opt.mode == "rehearse" || opt.mode == "migrate" || opt.baselinePath != ""
	if needsBaseline {
		if opt.mode == "backup" || opt.baselinePath != filepath.Join(opt.private, "helper-backup.json") ||
			!digestPattern.MatchString(opt.baselineSHA) {
			return opt, fail("baseline_arguments_invalid")
		}
	} else if opt.baselineSHA != "" {
		return opt, fail("baseline_arguments_invalid")
	}
	if opt.mode == "rehearse" {
		if opt.targetPath != filepath.Join(opt.private, "helper-target.json") || !digestPattern.MatchString(opt.targetSHA) {
			return opt, fail("target_arguments_invalid")
		}
	} else if opt.targetPath != "" || opt.targetSHA != "" {
		return opt, fail("target_arguments_invalid")
	}
	if err := verifyProduct(&opt); err != nil {
		return opt, err
	}
	if needsBaseline {
		if err := loadBaseline(&opt); err != nil {
			return opt, err
		}
	}
	return opt, nil
}

func validTarget(target targetIdentity) bool {
	return namePattern.MatchString(target.Database) && namePattern.MatchString(target.Role) && target.DatabaseOID > 0 &&
		target.RoleOID > 0 && target.SchemaOID > 0 && target.SchemaOwnerOID > 0
}

func validateMainScope(value intentDocument) error {
	rehearsal := "goby_main_s55_rehearsal_" + strings.ReplaceAll(value.RunID, "_", "")
	if value.Marker != intentMarker || value.Version != 1 || !runPattern.MatchString(value.RunID) ||
		value.EvidenceRoot != evidenceBase+"/run-"+value.RunID || value.SourceManifestSHA256 != sourceManifest ||
		!digestPattern.MatchString(value.Source55BinarySHA256) || !digestPattern.MatchString(value.HelperSHA256) ||
		value.Main.Database != mainName || value.Main.Role != mainName || value.Main.DatabaseOID != mainDatabaseOID ||
		value.Main.RoleOID != mainRoleOID || value.Main.SchemaOID != mainSchemaOID || value.Main.SchemaOwnerOID != mainSchemaOwnerOID ||
		value.Main.RecoveryDeploymentID != mainDeploymentID || !digestPattern.MatchString(value.Main.RecoveryBindingSHA256) ||
		value.Main.OwnerTag != "" || value.Rehearsal.Database != rehearsal || value.Rehearsal.Role != rehearsal ||
		value.Rehearsal.OwnerTag != ownerMarker+":"+value.RunID {
		return fail("intent_scope_invalid")
	}
	cluster := value.Cluster
	if cluster.Port != 5432 || cluster.DataDirectory != dataDirectory || cluster.SystemIdentifier != mainSystemIdentifier ||
		cluster.PostmasterPID <= 1 || cluster.PostmasterStartMicroseconds <= 0 ||
		!regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(cluster.PostmasterStartTicks) ||
		!bootPattern.MatchString(cluster.BootID) || cluster.PostgreSQLVersionNum/10000 != 17 {
		return fail("cluster_proof_invalid")
	}
	if !validLifecycleScope(value.Lifecycle) {
		return fail("lifecycle_scope_invalid")
	}
	return nil
}

func validLifecycleScope(value lifecycleScope) bool {
	directory, lock := value.DirectoryIdentity, value.LockIdentity
	return value.Directory == lifecycleDirectory && value.LockFile == lifecycleLockFile &&
		directory.Device > 0 && directory.Inode > 0 && lock.Device > 0 && lock.Inode > 0 &&
		directory.UID == lifecycleUID && lock.UID == lifecycleUID && directory.Mode == 0o700 && lock.Mode == 0o600 &&
		(directory.Device != lock.Device || directory.Inode != lock.Inode)
}

func validLifecycleArguments(opt options) bool {
	if opt.mode != "migrate" {
		return !opt.lifecycleDirectoryFDSet && !opt.lifecycleLockFDSet
	}
	return opt.lifecycleDirectoryFDSet && opt.lifecycleLockFDSet && opt.lifecycleDirectoryFD >= 3 &&
		opt.lifecycleLockFD >= 3 && opt.lifecycleDirectoryFD != opt.lifecycleLockFD
}

func lifecycleStatMatches(value unix.Stat_t, pin lifecycleFileIdentity, directory bool) bool {
	if value.Dev != pin.Device || value.Ino != pin.Inode || value.Uid != pin.UID || value.Gid != pin.GID ||
		value.Uid != lifecycleUID || value.Mode&0o7777 != pin.Mode {
		return false
	}
	if directory {
		return value.Mode&unix.S_IFMT == unix.S_IFDIR && pin.Mode == 0o700 && value.Nlink > 0
	}
	return value.Mode&unix.S_IFMT == unix.S_IFREG && pin.Mode == 0o600 && value.Nlink == 1 && value.Size == 0
}

func lifecycleDescriptorMatches(opened, named unix.Stat_t, link, path string, pin lifecycleFileIdentity, directory bool) bool {
	return link == path && lifecycleStatMatches(opened, pin, directory) && lifecycleStatMatches(named, pin, directory)
}

// Linux fdinfo lists FLOCK locks for this open file description, including
// descriptions inherited from the controller. The PID is the original owner,
// not necessarily this process. Reasserting Flock alone would acquire an unheld
// descriptor and therefore cannot prove that the controller held the fence.
func heldLifecycleFlock(raw []byte, device, inode uint64) bool {
	if len(raw) == 0 || len(raw) > 65536 || raw[len(raw)-1] != '\n' || !utf8.Valid(raw) {
		return false
	}
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "lock:" {
			continue
		}
		count++
		if count != 1 || len(fields) != 9 || fields[2] != "FLOCK" || fields[3] != "ADVISORY" ||
			fields[4] != "WRITE" || fields[7] != "0" || fields[8] != "EOF" || !strings.HasSuffix(fields[1], ":") {
			return false
		}
		lockID, err := strconv.ParseUint(strings.TrimSuffix(fields[1], ":"), 10, 64)
		if err != nil || lockID == 0 || !regexp.MustCompile(`^[1-9][0-9]*:$`).MatchString(fields[1]) ||
			!regexp.MustCompile(`^[0-9]+$`).MatchString(fields[5]) {
			return false
		}
		if _, err := strconv.ParseUint(fields[5], 10, 31); err != nil {
			return false
		}
		identity := strings.Split(fields[6], ":")
		if len(identity) != 3 || !regexp.MustCompile(`^[0-9a-fA-F]{2,}$`).MatchString(identity[0]) ||
			!regexp.MustCompile(`^[0-9a-fA-F]{2,}$`).MatchString(identity[1]) || !regexp.MustCompile(`^[0-9]+$`).MatchString(identity[2]) {
			return false
		}
		major, majorErr := strconv.ParseUint(identity[0], 16, 32)
		minor, minorErr := strconv.ParseUint(identity[1], 16, 32)
		actualInode, inodeErr := strconv.ParseUint(identity[2], 10, 64)
		if majorErr != nil || minorErr != nil || inodeErr != nil || major != uint64(unix.Major(device)) ||
			minor != uint64(unix.Minor(device)) || actualInode != inode {
			return false
		}
	}
	return count == 1
}

func checkLifecycleAncestors(path string) error {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		var info unix.Stat_t
		if unix.Lstat(current, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR ||
			info.Mode&0o022 != 0 || (info.Uid != 0 && info.Uid != lifecycleUID) {
			return fail("lifecycle_fence_invalid")
		}
		if current == "/" {
			return nil
		}
	}
}

func observeLifecycleDescriptor(fd int, path string, pin lifecycleFileIdentity, directory bool) error {
	var opened, named unix.Stat_t
	if checkLifecycleAncestors(path) != nil || unix.Fstat(fd, &opened) != nil || unix.Lstat(path, &named) != nil {
		return fail("lifecycle_fence_invalid")
	}
	link, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil || !lifecycleDescriptorMatches(opened, named, link, path, pin, directory) {
		return fail("lifecycle_fence_invalid")
	}
	info, err := os.Open("/proc/self/fdinfo/" + strconv.Itoa(fd))
	if err != nil {
		return fail("lifecycle_fence_invalid")
	}
	raw, readErr := io.ReadAll(io.LimitReader(info, 65537))
	closeErr := info.Close()
	if readErr != nil || closeErr != nil || !heldLifecycleFlock(raw, pin.Device, pin.Inode) {
		return fail("lifecycle_fence_invalid")
	}
	return nil
}

func checkLifecycleFence(opt options) error {
	if opt.mode != "migrate" || !validLifecycleArguments(opt) || !validLifecycleScope(opt.intent.Lifecycle) {
		return fail("lifecycle_fence_invalid")
	}
	scope := opt.intent.Lifecycle
	observe := func() error {
		if err := observeLifecycleDescriptor(opt.lifecycleDirectoryFD, scope.Directory, scope.DirectoryIdentity, true); err != nil {
			return err
		}
		return observeLifecycleDescriptor(opt.lifecycleLockFD, scope.LockFile, scope.LockIdentity, false)
	}
	if err := observe(); err != nil {
		return err
	}
	// These are borrowed descriptors. Never close, unlock, or wrap them in an
	// os.File whose finalizer could close the controller's shared description.
	if unix.Flock(opt.lifecycleDirectoryFD, unix.LOCK_EX|unix.LOCK_NB) != nil ||
		unix.Flock(opt.lifecycleLockFD, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return fail("lifecycle_fence_invalid")
	}
	return observe()
}

func sha(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func stateDigest(state capturedState) (string, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return "", fail("state_encoding_failed")
	}
	return sha(raw), nil
}

func strictJSON(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > maximumJSON || !utf8.Valid(raw) {
		return fail("document_size_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 40 {
			return fail("document_depth_invalid")
		}
		token, err := decoder.Token()
		if err != nil {
			return fail("document_invalid")
		}
		delim, nested := token.(json.Delim)
		if !nested {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || len(seen) >= 20000 {
					return fail("document_duplicate_or_invalid")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		} else if delim == '[' {
			count := 0
			for decoder.More() {
				count++
				if count > 20000 {
					return fail("document_collection_invalid")
				}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		} else {
			return fail("document_invalid")
		}
		end, err := decoder.Token()
		if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
			return fail("document_invalid")
		}
		return nil
	}
	if walk(0) != nil {
		return fail("document_invalid")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fail("document_trailing_value")
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return fail("document_shape_invalid")
	}
	if exactJSONShape(raw, reflect.TypeOf(target), 0) != nil {
		return fail("document_fields_invalid")
	}
	return nil
}

func exactJSONShape(raw json.RawMessage, kind reflect.Type, depth int) error {
	if depth > 40 {
		return fail("document_depth_invalid")
	}
	if kind == nil {
		return fail("document_type_invalid")
	}
	if kind.Kind() == reflect.Ptr {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return exactJSONShape(raw, kind.Elem(), depth+1)
	}
	if kind == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if kind.Kind() == reflect.Slice || kind.Kind() == reflect.Map || kind.Kind() == reflect.Interface {
			return nil
		}
		return fail("document_null_invalid")
	}
	switch kind.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return fail("document_object_invalid")
		}
		known := map[string]bool{}
		for index := 0; index < kind.NumField(); index++ {
			field := kind.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			known[name] = true
			value, exists := fields[name]
			optional := false
			for _, option := range tag[1:] {
				optional = optional || option == "omitempty"
			}
			if !exists {
				if !optional {
					return fail("document_field_missing")
				}
				continue
			}
			if err := exactJSONShape(value, field.Type, depth+1); err != nil {
				return err
			}
		}
		for name := range fields {
			if !known[name] {
				return fail("document_field_unknown")
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return fail("document_array_invalid")
		}
		for _, value := range values {
			if err := exactJSONShape(value, kind.Elem(), depth+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return fail("document_map_invalid")
		}
		for _, value := range values {
			if err := exactJSONShape(value, kind.Elem(), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkedPath(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fail("path_invalid")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, fail("path_changed")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm()&0o022 != 0 {
			return nil, fail("path_owner_invalid")
		}
		if current == "/" {
			break
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fail("path_unavailable")
	}
	return info, nil
}

func checkDirectory(path string, mode os.FileMode) error {
	info, err := checkedPath(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != mode {
		return fail("private_directory_invalid")
	}
	return nil
}

func openRead(path string, modes []os.FileMode, limit int64) (*os.File, os.FileInfo, error) {
	before, err := checkedPath(path)
	if err != nil {
		return nil, nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	allowed := false
	for _, mode := range modes {
		if before.Mode().Perm() == mode {
			allowed = true
		}
	}
	if !ok || !before.Mode().IsRegular() || stat.Nlink != 1 || stat.Gid != 0 || before.Size() > limit || !allowed {
		return nil, nil, fail("private_file_invalid")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, fail("private_file_unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	actual, err := file.Stat()
	if err != nil || !os.SameFile(before, actual) {
		file.Close()
		return nil, nil, fail("private_file_replaced")
	}
	return file, before, nil
}

func unchangedFile(file *os.File, path string, before os.FileInfo) error {
	after, err := file.Stat()
	current, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(before, after) || !os.SameFile(before, current) ||
		after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || current.Mode() != before.Mode() {
		return fail("private_file_changed")
	}
	oldStat, oldOK := before.Sys().(*syscall.Stat_t)
	newStat, newOK := after.Sys().(*syscall.Stat_t)
	if !oldOK || !newOK || oldStat.Ctim != newStat.Ctim || newStat.Nlink != 1 {
		return fail("private_file_changed")
	}
	return nil
}

func readPrivateBytes(path string, uid int, limit int64) ([]byte, error) {
	if uid != 0 {
		return nil, fail("private_owner_invalid")
	}
	file, before, err := openRead(path, []os.FileMode{0o600}, limit)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) != before.Size() || unchangedFile(file, path, before) != nil {
		return nil, fail("private_read_failed")
	}
	return raw, nil
}

func readProduct(path string, expected string) ([]byte, error) {
	file, before, err := openRead(path, []os.FileMode{0o600, 0o644, 0o500, 0o550, 0o700, 0o755}, 128<<20)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (128<<20)+1))
	if err != nil || int64(len(raw)) != before.Size() || sha(raw) != expected || unchangedFile(file, path, before) != nil {
		return nil, fail("product_bytes_changed")
	}
	return raw, nil
}

func verifyProduct(opt *options) error {
	if _, err := readProduct(productRoot+"/backup-source-inputs.json", sourceManifest); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fail("helper_identity_unavailable")
	}
	if _, err := readProduct(executable, opt.intent.HelperSHA256); err != nil {
		return fail("helper_identity_changed")
	}
	opt.catalogs = map[int64]catalogDocument{}
	for _, item := range []struct {
		version int64
		pin     filePin
		digest  string
	}{{27, opt.intent.Catalog27, catalog27SHA}, {28, opt.intent.Catalog28, catalog28SHA}} {
		path := productRoot + "/internal/backuppg/catalogs/schema-" + strconv.FormatInt(item.version, 10) + "-postgresql-17.json"
		if item.pin.Path != path || item.pin.SHA256 != item.digest {
			return fail("catalog_pin_invalid")
		}
		raw, err := readProduct(path, item.digest)
		if err != nil {
			return err
		}
		var catalog catalogDocument
		if strictJSON(raw, &catalog) != nil || catalog.Version != item.version || catalog.PostgreSQLMajor != 17 ||
			len(catalog.Catalog.Tables) != 35 || len(catalog.Catalog.Sequences) != 5 {
			return fail("catalog_document_invalid")
		}
		opt.catalogs[item.version] = catalog
	}
	compiled, err := database.EmbeddedMigrations()
	if err != nil || len(compiled) != 28 || compiled[27].Version != 28 || compiled[27].Name != "0028_storage_root_bindings.sql" {
		return fail("embedded_migrations_changed")
	}
	for version, catalog := range opt.catalogs {
		if len(catalog.Migrations) != int(version) {
			return fail("catalog_migrations_invalid")
		}
		for index, migration := range catalog.Migrations {
			actual := compiled[index]
			if migration.Version != actual.Version || migration.Name != actual.Name || migration.SHA256 != actual.SHA256 {
				return fail("embedded_migrations_changed")
			}
		}
	}
	return nil
}

func loadBaseline(opt *options) error {
	raw, err := readPrivateBytes(opt.baselinePath, 0, maximumJSON)
	if err != nil || sha(raw) != opt.baselineSHA || strictJSON(raw, &opt.baseline) != nil {
		return fail("backup_report_invalid")
	}
	opt.baseline.State.Identity.RoleProperties, err = normalizeRoleProperties(opt.baseline.State.Identity.RoleProperties)
	if err != nil {
		return err
	}
	b := opt.baseline
	digest, err := stateDigest(b.State)
	if err != nil || b.Marker != reportMarker || b.Version != 1 || b.Mode != "backup" || b.Status != "backed_up" ||
		b.RunID != opt.intent.RunID || b.IntentSHA256 != opt.intentSHA || b.SourceManifestSHA256 != sourceManifest ||
		b.Source55BinarySHA256 != opt.intent.Source55BinarySHA256 || b.HelperSHA256 != opt.intent.HelperSHA256 ||
		b.SourceSchemaVersion != 27 || b.TargetSchemaVersion != 28 || b.State.SchemaVersion != 27 || digest != b.StateSHA256 ||
		!b.SameExportedSnapshot || b.Facts == nil || b.Facts.SchemaVersion != 27 || len(b.Facts.Tables) != 35 ||
		len(b.Facts.MigrationChecksums) != 27 || b.Dump == nil || b.Dump.Path != filepath.Join(opt.private, "main-schema27.dump") ||
		b.Dump.Bytes < 1 || b.Dump.Bytes > maximumDump || !digestPattern.MatchString(b.Dump.SHA256) {
		return fail("backup_report_binding_invalid")
	}
	return nil
}

func readCredentials(opt *options) (credentials, error) {
	raw, err := readPrivateBytes(opt.credentialsPath, 0, 16384)
	if err != nil {
		return credentials{}, err
	}
	var value credentials
	if strictJSON(raw, &value) != nil {
		return value, fail("credentials_invalid")
	}
	opt.beforeCredentials = append([]byte(nil), raw...)
	if validateURL(value.MainURL, mainName) != nil {
		return value, fail("main_credentials_invalid")
	}
	if opt.mode == "rehearse" && validateURL(value.RehearsalURL, opt.intent.Rehearsal.Database) != nil {
		return value, fail("rehearsal_credentials_invalid")
	}
	return value, nil
}

func validateURL(raw, expected string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgresql" && parsed.Scheme != "postgres") || parsed.Host != "127.0.0.1:5432" ||
		parsed.User == nil || parsed.User.Username() != expected || parsed.Path != "/"+expected || parsed.RawQuery != "sslmode=disable" ||
		parsed.Fragment != "" || parsed.Opaque != "" {
		return fail("connection_scope_invalid")
	}
	password, set := parsed.User.Password()
	if !set || len(password) < 24 || len(password) > 512 {
		return fail("connection_secret_invalid")
	}
	return nil
}

type connections struct {
	sync.Mutex
	starts map[uint32]int64
}

func openPool(ctx context.Context, raw string) (*pgxpool.Pool, *connections, error) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") {
			_ = os.Unsetenv(name)
		}
	}
	configuration, err := pgxpool.ParseConfig(raw)
	if err != nil {
		return nil, nil, fail("connection_configuration_invalid")
	}
	configuration.MaxConns, configuration.MinConns = 3, 0
	configuration.ConnConfig.ConnectTimeout = 5 * time.Second
	if configuration.ConnConfig.RuntimeParams == nil {
		configuration.ConnConfig.RuntimeParams = map[string]string{}
	}
	configuration.ConnConfig.RuntimeParams["application_name"] = "goby-main-schema28-helper"
	configuration.ConnConfig.RuntimeParams["search_path"] = "pg_catalog,public"
	configuration.ConnConfig.RuntimeParams["statement_timeout"] = "60000"
	registry := &connections{starts: map[uint32]int64{}}
	configuration.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		var started int64
		if connection.QueryRow(ctx, "SELECT (extract(epoch FROM backend_start)*1000000)::bigint FROM pg_catalog.pg_stat_activity WHERE pid=pg_backend_pid()").Scan(&started) != nil {
			return fail("connection_witness_unavailable")
		}
		registry.Lock()
		registry.starts[connection.PgConn().PID()] = started
		registry.Unlock()
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, nil, fail("database_unavailable")
	}
	return pool, registry, nil
}

func (registry *connections) quiescent(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, "SELECT pid::bigint,(extract(epoch FROM backend_start)*1000000)::bigint FROM pg_catalog.pg_stat_activity WHERE datid=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())")
	if err != nil {
		return fail("connection_inventory_unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var pid uint32
		var started int64
		if rows.Scan(&pid, &started) != nil {
			return fail("connection_inventory_invalid")
		}
		registry.Lock()
		expected, ok := registry.starts[pid]
		registry.Unlock()
		if !ok || expected != started {
			return fail("unowned_database_connection")
		}
	}
	if rows.Err() != nil {
		return fail("connection_inventory_unavailable")
	}
	var count int64
	if tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM pg_prepared_xacts WHERE database=current_database())+(SELECT count(*) FROM pg_replication_slots WHERE database=current_database())").Scan(&count) != nil || count != 0 {
		return fail("prepared_or_replication_work")
	}
	return nil
}

func processIdentity(pid int) (int, string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil || len(raw) > 65536 {
		return 0, "", fail("process_witness_unavailable")
	}
	closing := bytes.LastIndexByte(raw, ')')
	if closing < 0 {
		return 0, "", fail("process_witness_invalid")
	}
	fields := strings.Fields(string(raw[closing+1:]))
	if len(fields) < 20 {
		return 0, "", fail("process_witness_invalid")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil || !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(fields[19]) {
		return 0, "", fail("process_witness_invalid")
	}
	return parent, fields[19], nil
}

func verifyCluster(proof clusterProof, backendPID int, started int64) error {
	parent, _, err := processIdentity(backendPID)
	if err != nil || parent != proof.PostmasterPID || started != proof.PostmasterStartMicroseconds {
		return fail("cluster_process_mismatch")
	}
	_, ticks, err := processIdentity(proof.PostmasterPID)
	boot, bootErr := os.ReadFile("/proc/sys/kernel/random/boot_id")
	command, commandErr := os.ReadFile("/proc/" + strconv.Itoa(proof.PostmasterPID) + "/cmdline")
	if err != nil || bootErr != nil || commandErr != nil || ticks != proof.PostmasterStartTicks || strings.TrimSpace(string(boot)) != proof.BootID || len(command) > 65536 {
		return fail("cluster_process_mismatch")
	}
	parts := strings.Split(strings.TrimSuffix(string(command), "\x00"), "\x00")
	matches := 0
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == "-D" && parts[index+1] == dataDirectory {
			matches++
		}
	}
	if len(parts) == 0 || parts[0] != "/usr/lib/postgresql/17/bin/postgres" || matches != 1 {
		return fail("cluster_directory_mismatch")
	}
	return nil
}

func readIdentity(ctx context.Context, tx pgx.Tx, opt options, target targetIdentity) (databaseIdentity, error) {
	var result databaseIdentity
	var sessionRole, schemaOwner string
	var safe, comments bool
	var backend int
	var started int64
	var port, version int
	query := `SELECT current_database(),d.oid,d.datdba,current_user,r.oid,session_user,n.oid,n.nspowner,
 pg_catalog.pg_get_userbyid(n.nspowner),current_setting('port')::integer,current_setting('server_version_num')::integer,
 NOT (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) AND r.rolcanlogin
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid OR m.grantor=r.oid)
 AND NOT EXISTS(SELECT 1 FROM pg_namespace extra WHERE extra.nspname NOT IN ('public','information_schema') AND left(extra.nspname,3)<>'pg_'),
 ($1='' OR (shobj_description(d.oid,'pg_database')=$1 AND shobj_description(r.oid,'pg_authid')=$1)),
 pg_backend_pid(),(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint,
 jsonb_build_object('login',r.rolcanlogin,'super',r.rolsuper,'createdb',r.rolcreatedb,'createrole',r.rolcreaterole,
 'replication',r.rolreplication,'bypass',r.rolbypassrls,'inherit',r.rolinherit,'limit',r.rolconnlimit,
 'config',r.rolconfig,'valid_until',r.rolvaliduntil)
 FROM pg_database d JOIN pg_roles r ON r.rolname=current_user CROSS JOIN pg_namespace n
 WHERE d.datname=current_database() AND n.nspname='public'`
	if tx.QueryRow(ctx, query, target.OwnerTag).Scan(&result.Database, &result.DatabaseOID, &result.DatabaseOwnerOID,
		&result.Role, &result.RoleOID, &sessionRole, &result.SchemaOID, &result.SchemaOwnerOID, &schemaOwner,
		&port, &version, &safe, &comments, &backend, &started, &result.RoleProperties) != nil {
		return result, fail("database_identity_unavailable")
	}
	if !safe || !comments || result.Database != target.Database || result.Role != target.Role || sessionRole != target.Role ||
		result.DatabaseOID != target.DatabaseOID || result.DatabaseOwnerOID != target.RoleOID || result.RoleOID != target.RoleOID ||
		result.SchemaOID != target.SchemaOID || result.SchemaOwnerOID != target.SchemaOwnerOID || port != 5432 ||
		version != opt.intent.Cluster.PostgreSQLVersionNum {
		return result, fail("database_identity_mismatch")
	}
	if err := verifyCluster(opt.intent.Cluster, backend, started); err != nil {
		return result, err
	}
	result.Cluster = opt.intent.Cluster
	switch {
	case result.SchemaOwnerOID == result.RoleOID:
		result.SchemaOwnerKind = "database_role"
	case schemaOwner == "pg_database_owner":
		result.SchemaOwnerKind = "pg_database_owner"
	default:
		return result, fail("schema_owner_mismatch")
	}
	var databaseGrants, schemaGrants []byte
	if tx.QueryRow(ctx, `SELECT d.datacl::text,n.nspacl::text,`+
		aclGrantsSQL("d.datacl", "pg_catalog.acldefault('d',d.datdba)", "d.datdba")+`,`+
		aclGrantsSQL("n.nspacl", "pg_catalog.acldefault('n',n.nspowner)", "n.nspowner")+
		` FROM pg_database d CROSS JOIN pg_namespace n WHERE d.datname=current_database() AND n.nspname='public'`).Scan(
		&result.DatabaseACL.Raw, &result.SchemaACL.Raw, &databaseGrants, &schemaGrants) != nil {
		return result, fail("database_acl_capture_failed")
	}
	var err error
	result.DatabaseACL.Grants, err = decodeACLGrants(databaseGrants)
	if err != nil {
		return result, err
	}
	result.SchemaACL.Grants, err = decodeACLGrants(schemaGrants)
	if err != nil {
		return result, err
	}
	result.RoleProperties, err = normalizeRoleProperties(result.RoleProperties)
	if err != nil {
		return result, err
	}
	return result, nil
}

func boundRecoveryMarker(raw string, expected targetIdentity) (bindingState, error) {
	claim, err := recoverydb.DecodeMarker(raw)
	if err != nil || expected.RecoveryDeploymentID != mainDeploymentID || claim.DeploymentID != expected.RecoveryDeploymentID ||
		string(claim.Slot) != "primary" || sha([]byte(raw)) != expected.RecoveryBindingSHA256 {
		return bindingState{}, fail("recovery_binding_mismatch")
	}
	return bindingState{DeploymentID: claim.DeploymentID, GenerationID: claim.GenerationID, Slot: string(claim.Slot), SHA256: sha([]byte(raw))}, nil
}

func captureState(ctx context.Context, tx pgx.Tx, opt options, target targetIdentity, version int64) (capturedState, error) {
	var result capturedState
	if version != 27 && version != 28 {
		return result, fail("state_version_invalid")
	}
	var actual int64
	if tx.QueryRow(ctx, "SELECT COALESCE(max(version),0) FROM public.schema_migrations").Scan(&actual) != nil || actual != version {
		return result, fail("schema_version_mismatch")
	}
	if _, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public"); err != nil {
		return result, fail("trusted_catalog_rejected")
	}
	var err error
	result.Identity, err = readIdentity(ctx, tx, opt, target)
	if err != nil {
		return result, err
	}
	result.SchemaVersion, result.TrustedCatalogVerified = version, true
	result.CatalogSHA256 = opt.catalogs[version].Catalog.SHA256
	result.RowHashFormat = "goby-canonical-jsonb-sha256-multiset-v1"
	result.MigrationPrefixSHA256, err = migrationDigest(version)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT c.relname,c.oid,c.relowner,a.attname,a.attnum,a.atttypid,a.atttypmod,
 pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attgenerated::text,a.attidentity::text,a.attcollation
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 WHERE n.nspname='public' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped ORDER BY c.relname COLLATE "C",a.attnum`)
	if err != nil {
		return result, fail("column_capture_failed")
	}
	for rows.Next() {
		var name string
		var oid, owner uint32
		var column columnState
		if rows.Scan(&name, &oid, &owner, &column.Name, &column.Number, &column.TypeOID, &column.TypeModifier,
			&column.Type, &column.NotNull, &column.Generated, &column.Identity, &column.CollationOID) != nil ||
			!namePattern.MatchString(name) || !namePattern.MatchString(column.Name) || owner != target.RoleOID {
			rows.Close()
			return result, fail("column_capture_failed")
		}
		if len(result.Tables) == 0 || result.Tables[len(result.Tables)-1].Name != name {
			result.Tables = append(result.Tables, tableState{Name: name, OID: oid, OwnerOID: owner})
		}
		result.Tables[len(result.Tables)-1].Columns = append(result.Tables[len(result.Tables)-1].Columns, column)
	}
	rows.Close()
	if rows.Err() != nil || len(result.Tables) != 35 {
		return result, fail("table_inventory_mismatch")
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		table.Rows, table.SHA256, err = rowMultiset(ctx, tx, *table, 0)
		if err != nil {
			return result, err
		}
	}
	result.Sequences, err = captureSequences(ctx, tx, target.RoleOID)
	if err != nil {
		return result, err
	}
	if len(result.Sequences) != len(expectedSequences) {
		return result, fail("sequence_inventory_mismatch")
	}
	for index, name := range expectedSequences {
		if result.Sequences[index].Name != name {
			return result, fail("sequence_inventory_mismatch")
		}
	}
	result.Relations, err = captureRelations(ctx, tx, target.RoleOID)
	if err != nil {
		return result, err
	}
	var checks []byte
	if tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'oid',c.oid::bigint,
 'columns',ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey) ORDER BY a.attname::text COLLATE "C"),
 'definition',pg_get_constraintdef(c.oid),'validated',c.convalidated,'deferrable',c.condeferrable,'deferred',c.condeferred,'no_inherit',c.connoinherit)
 ORDER BY r.relname COLLATE "C",c.conname COLLATE "C"),'[]') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid
 JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname='public' AND c.contype='c'`).Scan(&checks) != nil ||
		strictJSON(checks, &result.Checks) != nil {
		return result, fail("check_inventory_failed")
	}
	var marker string
	if tx.QueryRow(ctx, "SELECT value FROM public.server_settings WHERE key=$1", recoverydb.MarkerKey).Scan(&marker) != nil {
		return result, fail("recovery_binding_unavailable")
	}
	result.RecoveryBinding, err = boundRecoveryMarker(marker, opt.intent.Main)
	if err != nil {
		return result, err
	}
	if tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.sessions),(SELECT count(*) FROM public.devices),
 (SELECT count(*) FROM public.activity_entries),(SELECT count(*) FROM public.library_roots)`).Scan(
		&result.Profile.Sessions, &result.Profile.Devices, &result.Profile.ActivityEntries, &result.Profile.LibraryRoots) != nil {
		return result, fail("main_profile_capture_failed")
	}
	// Preserve each literal historical mapping without imposing a fixed
	// population, relative path, or allowed-path shape on the main database.
	rootCount, rootDigest, err := projectionMultiset(ctx, tx,
		`SELECT id,library_id,path,allowed_path,relative_path FROM public.library_roots`)
	if err != nil || rootCount != result.Profile.LibraryRoots {
		return result, fail("main_profile_capture_failed")
	}
	result.Profile.RootMappingsSHA256 = rootDigest
	return result, nil
}

func verifyDefaults(ctx context.Context, tx pgx.Tx) error {
	var roots, audits int64
	if tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.library_roots WHERE binding_revision<>1 OR storage_binding IS NOT NULL OR bound_at IS NOT NULL OR bound_by IS NOT NULL),
 (SELECT count(*) FROM public.activity_entries WHERE previous_revision<>0 OR observation_fingerprint<>'')`).Scan(&roots, &audits) != nil || roots != 0 || audits != 0 {
		return fail("historical_binding_or_audit_inferred")
	}
	if database.ValidateRootBindingState(ctx, tx, 28) != nil {
		return fail("root_binding_semantics_invalid")
	}
	return nil
}

func addedColumns(table string) []string {
	switch table {
	case "library_roots":
		return []string{"binding_revision", "storage_binding", "bound_at", "bound_by"}
	case "activity_entries":
		return []string{"previous_revision", "observation_fingerprint"}
	}
	return nil
}

func replaceableActivityCheck(check checkState) bool {
	if check.Table != "activity_entries" {
		return false
	}
	return reflect.DeepEqual(check.Columns, []string{"action"}) || reflect.DeepEqual(check.Columns, []string{"resource_kind"}) || reflect.DeepEqual(check.Columns, []string{"action", "resource_kind"})
}

func sameACL(first, second aclState, rehearsal bool) bool {
	if rehearsal {
		first.Raw, second.Raw = nil, nil
	}
	return reflect.DeepEqual(first, second)
}

func sameSequences(first, second []sequenceState, rehearsal bool) bool {
	if !rehearsal {
		return reflect.DeepEqual(first, second)
	}
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index].Name != second[index].Name || first[index].LastValue != second[index].LastValue || first[index].IsCalled != second[index].IsCalled {
			return false
		}
	}
	return true
}

func rehearsalDatabaseACL(acl aclState) bool {
	if len(acl.Grants) != 3 {
		return false
	}
	expected := map[string]bool{"CREATE": true, "CONNECT": true, "TEMPORARY": true}
	for _, grant := range acl.Grants {
		if grant.Grantor != "@database_role" || grant.Grantee != "@database_role" || grant.Grantable || !expected[grant.Privilege] {
			return false
		}
		delete(expected, grant.Privilege)
	}
	return len(expected) == 0
}

func verifyPreserved(ctx context.Context, tx pgx.Tx, before, after capturedState, rehearsal bool) (string, error) {
	if before.SchemaVersion != 27 || after.SchemaVersion != 28 || len(before.Tables) != 35 || len(after.Tables) != 35 ||
		before.RecoveryBinding != after.RecoveryBinding || before.Profile != after.Profile || !sameSequences(before.Sequences, after.Sequences, rehearsal) {
		return "", fail("historical_state_changed")
	}
	if !rehearsal && !sameDatabaseIdentity(before.Identity, after.Identity) {
		return "", fail("historical_database_identity_changed")
	}
	// The fresh rehearsal deliberately removes PUBLIC database grants. Preserve
	// source privileges in place, while requiring the narrower target policy.
	if rehearsal && (before.Identity.Cluster != after.Identity.Cluster || before.Identity.SchemaOwnerKind != after.Identity.SchemaOwnerKind ||
		!rehearsalDatabaseACL(after.Identity.DatabaseACL) || !sameACL(before.Identity.SchemaACL, after.Identity.SchemaACL, true)) {
		return "", fail("rehearsal_privileges_changed")
	}
	for index, old := range before.Tables {
		current := after.Tables[index]
		extra := addedColumns(old.Name)
		if old.Name != current.Name || len(current.Columns) != len(old.Columns)+len(extra) ||
			!reflect.DeepEqual(old.Columns, current.Columns[:len(old.Columns)]) ||
			(!rehearsal && (old.OID != current.OID || old.OwnerOID != current.OwnerOID)) {
			return "", fail("historical_columns_changed")
		}
		for offset, name := range extra {
			if current.Columns[len(old.Columns)+offset].Name != name {
				return "", fail("new_column_mismatch")
			}
		}
		count, digest, err := rowMultiset(ctx, tx, old, 27)
		if err != nil {
			return "", err
		}
		if count != old.Rows || digest != old.SHA256 {
			return "", fail("historical_rows_changed")
		}
	}
	if len(before.Relations) != len(after.Relations) {
		return "", fail("historical_relation_inventory_changed")
	}
	for index, old := range before.Relations {
		current := after.Relations[index]
		if old.Name != current.Name || old.Kind != current.Kind || !sameACL(old.ACL, current.ACL, rehearsal) ||
			(!rehearsal && (old.OID != current.OID || old.OwnerOID != current.OwnerOID)) {
			return "", fail("historical_relation_changed")
		}
		extra := addedColumns(old.Name)
		if len(current.ColumnACLs) != len(old.ColumnACLs)+len(extra) {
			return "", fail("column_acl_inventory_changed")
		}
		for offset, previous := range old.ColumnACLs {
			actual := current.ColumnACLs[offset]
			if previous.Name != actual.Name || previous.Number != actual.Number || !sameACL(previous.ACL, actual.ACL, rehearsal) {
				return "", fail("historical_column_acl_changed")
			}
		}
		for offset, name := range extra {
			actual := current.ColumnACLs[len(old.ColumnACLs)+offset]
			if actual.Name != name || actual.ACL.Raw != nil || len(actual.ACL.Grants) != 0 {
				return "", fail("new_column_acl_changed")
			}
		}
	}
	currentChecks := map[string]checkState{}
	for _, item := range after.Checks {
		currentChecks[item.Table+"/"+item.Name] = item
	}
	for _, old := range before.Checks {
		if replaceableActivityCheck(old) {
			continue
		}
		actual, exists := currentChecks[old.Table+"/"+old.Name]
		if rehearsal {
			actual.OID, old.OID = 0, 0
		}
		if !exists || !reflect.DeepEqual(old, actual) {
			return "", fail("unrelated_check_changed")
		}
	}
	sequences, err := captureSequences(ctx, tx, after.Identity.RoleOID)
	if err != nil || !reflect.DeepEqual(sequences, after.Sequences) || !sameSequences(before.Sequences, sequences, rehearsal) {
		return "", fail("historical_sequences_changed")
	}
	if verifyDefaults(ctx, tx) != nil {
		return "", fail("historical_defaults_changed")
	}
	return stateDigest(before)
}

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

type privateOutput struct {
	directory     *os.File
	file          *os.File
	name, pending string
}

func reserveOutput(path string) (*privateOutput, error) {
	parent := filepath.Dir(path)
	if checkDirectory(parent, 0o700) != nil {
		return nil, fail("output_directory_invalid")
	}
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fail("output_directory_unavailable")
	}
	directory := os.NewFile(uintptr(fd), parent)
	name := filepath.Base(path)
	var exists unix.Stat_t
	if err := unix.Fstatat(fd, name, &exists, unix.AT_SYMLINK_NOFOLLOW); err != unix.ENOENT {
		directory.Close()
		return nil, fail("output_already_exists")
	}
	pending := name + ".pending"
	fileFD, err := unix.Openat(fd, pending, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		directory.Close()
		return nil, fail("output_reservation_failed")
	}
	return &privateOutput{directory: directory, file: os.NewFile(uintptr(fileFD), filepath.Join(parent, pending)), name: name, pending: pending}, nil
}

func (output *privateOutput) stage(value report) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > maximumJSON {
		return fail("report_encoding_failed")
	}
	raw = append(raw, '\n')
	if output.file.Truncate(0) != nil {
		return fail("report_prepare_failed")
	}
	if _, err := output.file.Seek(0, io.SeekStart); err != nil {
		return fail("report_prepare_failed")
	}
	if _, err := output.file.Write(raw); err != nil || output.file.Sync() != nil {
		return fail("report_prepare_failed")
	}
	return nil
}

func (output *privateOutput) publish() error {
	parent, parentErr := output.directory.Stat()
	currentParent, currentErr := os.Lstat(output.directory.Name())
	owned, ownedErr := output.file.Stat()
	var pending unix.Stat_t
	pendingErr := unix.Fstatat(int(output.directory.Fd()), output.pending, &pending, unix.AT_SYMLINK_NOFOLLOW)
	if parentErr != nil || currentErr != nil || ownedErr != nil || pendingErr != nil || !os.SameFile(parent, currentParent) {
		return fail("report_publication_identity_changed")
	}
	stat, ok := owned.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev != uint64(pending.Dev) || stat.Ino != pending.Ino || pending.Mode&unix.S_IFMT != unix.S_IFREG ||
		pending.Mode&0o7777 != 0o600 || pending.Uid != 0 || pending.Gid != 0 || pending.Nlink != 1 {
		return fail("report_publication_identity_changed")
	}
	if err := unix.Renameat2(int(output.directory.Fd()), output.pending, int(output.directory.Fd()), output.name, unix.RENAME_NOREPLACE); err != nil {
		return fail("report_publication_failed")
	}
	if output.directory.Sync() != nil {
		return fail("report_directory_sync_failed")
	}
	return nil
}

func (output *privateOutput) close() { _ = output.file.Close(); _ = output.directory.Close() }

func newReport(opt options, state capturedState, status string) (report, error) {
	digest, err := stateDigest(state)
	if err != nil {
		return report{}, err
	}
	source := int64(27)
	if opt.mode == "inspect" {
		source = state.SchemaVersion
	}
	return report{Marker: reportMarker, Version: 1, Mode: opt.mode, RunID: opt.intent.RunID, IntentSHA256: opt.intentSHA,
		SourceManifestSHA256: sourceManifest, Source55BinarySHA256: opt.intent.Source55BinarySHA256, HelperSHA256: opt.intent.HelperSHA256,
		Status: status, SourceSchemaVersion: source, TargetSchemaVersion: 28, StateSHA256: digest, State: state}, nil
}

func checkInputsUnchanged(opt options) error {
	raw, err := readPrivateBytes(opt.intentPath, 0, maximumJSON)
	if err != nil || sha(raw) != opt.intentSHA {
		return fail("intent_changed")
	}
	raw, err = readPrivateBytes(opt.credentialsPath, 0, 16384)
	if err != nil || !bytes.Equal(raw, opt.beforeCredentials) {
		return fail("credentials_changed")
	}
	if opt.baselinePath != "" {
		raw, err = readPrivateBytes(opt.baselinePath, 0, maximumJSON)
		if err != nil || sha(raw) != opt.baselineSHA {
			return fail("baseline_changed")
		}
	}
	return nil
}

func backupOptions(source string) backuppg.Options {
	return backuppg.Options{SourceURL: source, PGDump: "/usr/lib/postgresql/17/bin/pg_dump", PGRestore: "/usr/lib/postgresql/17/bin/pg_restore",
		Schema: "public", Timeout: 4 * time.Minute, MaxDumpBytes: maximumDump, ProbeVersion: media.CurrentProbeVersion}
}

func run(arguments []string) (result report, runErr error) {
	opt, err := parseOptions(arguments)
	if err != nil {
		return result, err
	}
	if opt.mode == "migrate" {
		if err := checkLifecycleFence(opt); err != nil {
			return result, err
		}
	}
	unix.Umask(0o077)
	secret, err := readCredentials(&opt)
	if err != nil {
		return result, err
	}
	defer clear(opt.beforeCredentials)
	output, err := reserveOutput(opt.output)
	if err != nil {
		return result, err
	}
	defer output.close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if opt.mode == "rehearse" {
		return rehearse(ctx, opt, secret, output)
	}
	pool, registry, err := openPool(ctx, secret.MainURL)
	if err != nil {
		return result, err
	}
	defer pool.Close()
	switch opt.mode {
	case "inspect":
		return inspect(ctx, opt, pool, output)
	case "backup":
		return backup(ctx, opt, pool, output)
	case "migrate":
		return migrate(ctx, cancel, opt, pool, registry, output)
	}
	return result, fail("mode_invalid")
}

func closeTransaction(tx pgx.Tx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		_ = tx.Conn().Close(ctx)
		return operationFailure{code: "rollback_outcome_unknown", outcome: "unknown"}
	}
	return nil
}

func inspect(ctx context.Context, opt options, pool *pgxpool.Pool, output *privateOutput) (result report, err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, fail("inspection_transaction_unavailable")
	}
	defer func() {
		if closeErr := closeTransaction(tx); closeErr != nil {
			err = closeErr
		}
	}()
	if _, err := readIdentity(ctx, tx, opt, opt.intent.Main); err != nil {
		return result, err
	}
	state, err := captureState(ctx, tx, opt, opt.intent.Main, opt.expectedSchema)
	if err != nil {
		return result, err
	}
	result, err = newReport(opt, state, "inspected")
	if err != nil {
		return result, err
	}
	if opt.baselinePath != "" {
		result.BaselineSHA256 = opt.baselineSHA
		result.BeforeStateSHA256 = opt.baseline.StateSHA256
		if opt.expectedSchema == 27 {
			if !sameCapturedState(state, opt.baseline.State) {
				return result, fail("backup_baseline_drift")
			}
			result.PreservedStateSHA256 = result.StateSHA256
		} else {
			result.PreservedStateSHA256, err = verifyPreserved(ctx, tx, opt.baseline.State, state, false)
			if err != nil {
				return result, err
			}
			result.RootBindingsNoAutoBinding, result.HistoricalAuditDefaults = true, true
		}
	}
	if opt.expectedSchema == 28 && verifyDefaults(ctx, tx) != nil {
		return result, fail("historical_defaults_changed")
	}
	if checkInputsUnchanged(opt) != nil {
		return result, fail("inspection_inputs_changed")
	}
	if err := closeTransaction(tx); err != nil {
		return result, err
	}
	pool.Close()
	if output.stage(result) != nil || output.publish() != nil {
		return result, fail("inspection_report_failed")
	}
	return result, nil
}

func snapshotDumpFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fail("snapshot_dump_cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return fail("snapshot_dump_deadline")
	case errors.Is(err, backuppg.ErrConfiguration):
		return fail("snapshot_dump_configuration")
	case errors.Is(err, backuppg.ErrDatabase):
		return fail("snapshot_dump_database")
	case errors.Is(err, backuppg.ErrArchive):
		return fail("snapshot_dump_archive")
	case errors.Is(err, backuppg.ErrLimit):
		return fail("snapshot_dump_limit")
	case errors.Is(err, backuppg.ErrSchema):
		return fail("snapshot_dump_schema")
	case errors.Is(err, backuppg.ErrTarget):
		return fail("snapshot_dump_target")
	case errors.Is(err, backuppg.ErrCommand):
		return fail("snapshot_dump_command")
	case errors.Is(err, backuppg.ErrUnsupported):
		return fail("snapshot_dump_unsupported")
	default:
		return fail("snapshot_dump_failed")
	}
}

func sameDumpFile(actual, expected os.FileInfo, content bool) bool {
	if actual == nil || expected == nil || actual.Mode() != 0o600 || expected.Mode() != 0o600 ||
		!os.SameFile(actual, expected) {
		return false
	}
	actualStat, actualOK := actual.Sys().(*syscall.Stat_t)
	expectedStat, expectedOK := expected.Sys().(*syscall.Stat_t)
	if !actualOK || !expectedOK || actualStat.Uid != 0 || actualStat.Gid != 0 || actualStat.Nlink != 1 ||
		expectedStat.Uid != 0 || expectedStat.Gid != 0 || expectedStat.Nlink != 1 {
		return false
	}
	return !content || actual.Size() == expected.Size() && actual.ModTime().Equal(expected.ModTime()) && actualStat.Ctim == expectedStat.Ctim
}

func checkDumpUnchanged(file *os.File, path string, expected os.FileInfo) error {
	opened, openedErr := file.Stat()
	current, currentErr := os.Lstat(path)
	if openedErr != nil || currentErr != nil || !sameDumpFile(opened, expected, true) || !sameDumpFile(current, expected, true) {
		return fail("dump_bytes_changed")
	}
	return nil
}

func backup(ctx context.Context, opt options, pool *pgxpool.Pool, output *privateOutput) (result report, err error) {
	snapshot, err := backuppg.OpenSnapshot(ctx, pool, backupOptions(pool.Config().ConnString()))
	if err != nil {
		return result, fail("source_snapshot_rejected")
	}
	defer snapshot.Close()
	state, err := captureState(snapshot.Context(), snapshot.Tx(), opt, opt.intent.Main, 27)
	if err != nil {
		return result, err
	}
	facts, err := snapshot.Facts(ctx)
	if err != nil || facts.SchemaVersion != 27 || len(facts.Tables) != 35 || len(facts.MigrationChecksums) != 27 ||
		facts.SchemaSHA256 != opt.catalogs[27].Catalog.SHA256 {
		return result, fail("source_facts_rejected")
	}
	for index, table := range facts.Tables {
		if table.Name != state.Tables[index].Name || table.Rows != state.Tables[index].Rows {
			return result, fail("snapshot_witness_mismatch")
		}
	}
	fd, err := unix.Openat(int(output.directory.Fd()), "main-schema27.dump", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return result, fail("dump_destination_exists_or_unavailable")
	}
	dumpPath := filepath.Join(opt.private, "main-schema27.dump")
	file := os.NewFile(uintptr(fd), dumpPath)
	defer file.Close()
	created, err := file.Stat()
	if err != nil || created.Size() != 0 || checkDumpUnchanged(file, dumpPath, created) != nil {
		return result, fail("dump_bytes_changed")
	}
	// The product accepts this retained regular file directly and enforces the
	// configured dump byte limit while its owned pg_dump process writes it.
	if err := snapshot.Dump(ctx, file); err != nil {
		return result, snapshotDumpFailure(err)
	}
	sequences, err := captureSequences(snapshot.Context(), snapshot.Tx(), opt.intent.Main.RoleOID)
	if err != nil || !reflect.DeepEqual(sequences, state.Sequences) {
		return result, fail("source_sequences_changed_during_dump")
	}
	if file.Sync() != nil {
		return result, fail("dump_completion_failed")
	}
	completed, err := file.Stat()
	if err != nil || completed.Size() < 1 || completed.Size() > maximumDump {
		return result, fail("dump_size_mismatch")
	}
	if !sameDumpFile(completed, created, false) || checkDumpUnchanged(file, dumpPath, completed) != nil {
		return result, fail("dump_bytes_changed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return result, fail("dump_rewind_failed")
	}
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(file, maximumDump+1))
	if err != nil {
		return result, fail("dump_bytes_changed")
	}
	if size != completed.Size() || size < 1 || size > maximumDump {
		return result, fail("dump_size_mismatch")
	}
	if checkDumpUnchanged(file, dumpPath, completed) != nil {
		return result, fail("dump_bytes_changed")
	}
	if err := snapshot.Close(); err != nil {
		return result, fail("snapshot_close_failed")
	}
	pool.Close()
	if checkInputsUnchanged(opt) != nil {
		return result, fail("backup_inputs_changed")
	}
	result, err = newReport(opt, state, "backed_up")
	if err != nil {
		return result, err
	}
	result.Facts, result.SameExportedSnapshot = &facts, true
	result.Dump = &dumpDescriptor{Path: dumpPath, Bytes: size, SHA256: hex.EncodeToString(digest.Sum(nil))}
	if output.stage(result) != nil {
		return result, fail("backup_report_failed")
	}
	if checkDumpUnchanged(file, dumpPath, completed) != nil {
		return result, fail("dump_bytes_changed")
	}
	if file.Close() != nil || output.directory.Sync() != nil {
		return result, fail("dump_close_failed")
	}
	current, err := os.Lstat(dumpPath)
	if err != nil || !sameDumpFile(current, completed, true) {
		return result, fail("dump_bytes_changed")
	}
	if output.publish() != nil {
		return result, fail("backup_report_failed")
	}
	return result, nil
}

func loadTarget(opt options) (targetIdentity, error) {
	raw, err := readPrivateBytes(opt.targetPath, 0, maximumJSON)
	if err != nil || sha(raw) != opt.targetSHA {
		return targetIdentity{}, fail("rehearsal_receipt_changed")
	}
	var value targetReceipt
	if strictJSON(raw, &value) != nil || value.Marker != targetMarker || value.Version != 1 || value.RunID != opt.intent.RunID ||
		value.IntentSHA256 != opt.intentSHA || value.Database != opt.intent.Rehearsal.Database || value.Role != opt.intent.Rehearsal.Role ||
		value.OwnerTag != opt.intent.Rehearsal.OwnerTag {
		return targetIdentity{}, fail("rehearsal_receipt_invalid")
	}
	result := targetIdentity{Database: value.Database, Role: value.Role, DatabaseOID: value.DatabaseOID, RoleOID: value.RoleOID,
		SchemaOID: value.SchemaOID, SchemaOwnerOID: value.SchemaOwnerOID, RecoveryDeploymentID: opt.intent.Main.RecoveryDeploymentID,
		RecoveryBindingSHA256: opt.intent.Main.RecoveryBindingSHA256, OwnerTag: value.OwnerTag}
	if !validTarget(result) || result.DatabaseOID == opt.intent.Main.DatabaseOID || result.RoleOID == opt.intent.Main.RoleOID {
		return result, fail("rehearsal_identity_not_independent")
	}
	return result, nil
}

func openDump(opt options) (*os.File, os.FileInfo, error) {
	if opt.baseline.Dump == nil {
		return nil, nil, fail("dump_descriptor_missing")
	}
	value := opt.baseline.Dump
	file, before, err := openRead(value.Path, []os.FileMode{0o600}, maximumDump)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(file, maximumDump+1))
	if err != nil || size != value.Bytes || hex.EncodeToString(digest.Sum(nil)) != value.SHA256 || unchangedFile(file, value.Path, before) != nil {
		file.Close()
		return nil, nil, fail("dump_bytes_changed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, fail("dump_rewind_failed")
	}
	return file, before, nil
}

func rehearse(ctx context.Context, opt options, secret credentials, output *privateOutput) (result report, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	target, err := loadTarget(opt)
	if err != nil {
		return result, err
	}
	dump, dumpInfo, err := openDump(opt)
	if err != nil {
		return result, err
	}
	defer dump.Close()
	pool, registry, err := openPool(ctx, secret.RehearsalURL)
	if err != nil {
		return result, err
	}
	defer pool.Close()
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		return result, fail("rehearsal_lease_unavailable")
	}
	go func() {
		select {
		case <-lease.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	defer func() {
		if closeErr := lease.Close(); closeErr != nil {
			err = operationFailure{code: "rehearsal_lease_close_failed", outcome: "retained"}
		}
	}()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, fail("rehearsal_inspection_unavailable")
	}
	if _, err = readIdentity(ctx, tx, opt, target); err == nil {
		_, err = backuppg.InspectEmptyRecoveryTransaction(ctx, tx, "public")
	}
	if err == nil {
		err = registry.quiescent(ctx, tx)
	}
	closeErr := closeTransaction(tx)
	if err != nil || closeErr != nil {
		return result, fail("rehearsal_not_owned_and_empty")
	}
	if checkInputsUnchanged(opt) != nil {
		return result, fail("rehearsal_inputs_changed")
	}
	if _, err := loadTarget(opt); err != nil {
		return result, err
	}
	if !lease.Protects(pool) {
		return result, fail("rehearsal_lease_lost")
	}
	restored, err := backuppg.RestoreOffline(ctx, pool, dump, *opt.baseline.Facts, backupOptions(secret.MainURL))
	if err != nil {
		return result, operationFailure{code: "rehearsal_restore_failed", outcome: "retained"}
	}
	if restored.SourceVersion != 27 || restored.CurrentVersion != 28 || !reflect.DeepEqual(restored.Tables, opt.baseline.Facts.Tables) {
		return result, fail("rehearsal_restore_result_invalid")
	}
	if unchangedFile(dump, opt.baseline.Dump.Path, dumpInfo) != nil {
		return result, fail("rehearsal_dump_changed")
	}
	snapshot, err := backuppg.OpenSnapshot(ctx, pool, backupOptions(pool.Config().ConnString()))
	if err != nil {
		return result, fail("rehearsal_result_catalog_rejected")
	}
	defer snapshot.Close()
	state, err := captureState(snapshot.Context(), snapshot.Tx(), opt, target, 28)
	if err != nil {
		return result, err
	}
	preserved, err := verifyPreserved(snapshot.Context(), snapshot.Tx(), opt.baseline.State, state, true)
	if err != nil {
		return result, err
	}
	targetFacts, err := snapshot.Facts(ctx)
	if err != nil || targetFacts.SchemaVersion != 28 || len(targetFacts.Tables) != 35 || len(targetFacts.MigrationChecksums) != 28 {
		return result, fail("rehearsal_target_facts_invalid")
	}
	if registry.quiescent(ctx, snapshot.Tx()) != nil || !lease.Protects(pool) {
		return result, fail("rehearsal_observers_changed")
	}
	if snapshot.Close() != nil || lease.Close() != nil {
		return result, fail("rehearsal_cleanup_failed")
	}
	pool.Close()
	if checkInputsUnchanged(opt) != nil {
		return result, fail("rehearsal_inputs_changed")
	}
	result, err = newReport(opt, state, "rehearsed")
	if err != nil {
		return result, err
	}
	result.Facts, result.TargetFacts, result.Dump = opt.baseline.Facts, &targetFacts, opt.baseline.Dump
	result.BaselineSHA256, result.BeforeStateSHA256, result.PreservedStateSHA256 = opt.baselineSHA, opt.baseline.StateSHA256, preserved
	result.RootBindingsNoAutoBinding, result.HistoricalAuditDefaults = true, true
	if output.stage(result) != nil || output.publish() != nil {
		return result, fail("rehearsal_report_failed")
	}
	return result, nil
}

func migrate(ctx context.Context, cancel context.CancelFunc, opt options, pool *pgxpool.Pool, registry *connections, output *privateOutput) (result report, err error) {
	if err := checkLifecycleFence(opt); err != nil {
		return result, err
	}
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		return result, fail("main_lease_busy_or_unavailable")
	}
	go func() {
		select {
		case <-lease.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	committed, commitAttempted := false, false
	defer func() {
		if closeErr := lease.Close(); closeErr != nil && err == nil {
			outcome := "failed"
			if committed {
				outcome = "committed_cleanup_failed"
			}
			err = operationFailure{code: "main_lease_close_failed", outcome: outcome}
		}
	}()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, fail("migration_transaction_unavailable")
	}
	defer func() {
		if !commitAttempted {
			if closeErr := closeTransaction(tx); closeErr != nil {
				err = closeErr
			}
		}
		if commitAttempted && !committed {
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_ = tx.Conn().Close(closeCtx)
			stop()
		}
	}()
	if _, err := readIdentity(ctx, tx, opt, opt.intent.Main); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='60s'"); err != nil {
		return result, fail("migration_limits_failed")
	}
	inspection, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public")
	if err != nil || inspection.LockTables(ctx) != nil {
		return result, fail("migration_catalog_or_lock_rejected")
	}
	if !lease.ProtectsTransaction(pool, tx) || registry.quiescent(ctx, tx) != nil {
		return result, fail("migration_ownership_not_exclusive")
	}
	if err := checkLifecycleFence(opt); err != nil {
		return result, err
	}
	before, err := captureState(ctx, tx, opt, opt.intent.Main, 27)
	if err != nil {
		return result, err
	}
	if !sameCapturedState(before, opt.baseline.State) {
		return result, fail("backup_baseline_drift")
	}
	if checkInputsUnchanged(opt) != nil {
		return result, fail("migration_inputs_changed")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path=public,pg_catalog"); err != nil {
		return result, fail("migration_search_path_failed")
	}
	if err := checkLifecycleFence(opt); err != nil {
		return result, err
	}
	if database.RecoveryMigrateTo(ctx, tx, 28) != nil {
		return result, fail("migration_failed")
	}
	after, err := captureState(ctx, tx, opt, opt.intent.Main, 28)
	if err != nil {
		return result, err
	}
	preserved, err := verifyPreserved(ctx, tx, before, after, false)
	if err != nil {
		return result, err
	}
	current, err := readIdentity(ctx, tx, opt, opt.intent.Main)
	if err != nil || !sameDatabaseIdentity(current, after.Identity) || registry.quiescent(ctx, tx) != nil || !lease.ProtectsTransaction(pool, tx) {
		return result, fail("migration_final_ownership_changed")
	}
	result, err = newReport(opt, after, "committed")
	if err != nil {
		return result, err
	}
	result.BaselineSHA256, result.BeforeStateSHA256, result.PreservedStateSHA256 = opt.baselineSHA, opt.baseline.StateSHA256, preserved
	result.RootBindingsNoAutoBinding, result.HistoricalAuditDefaults = true, true
	result.LifecycleFenceVerified = true
	if checkInputsUnchanged(opt) != nil || output.stage(result) != nil {
		return result, fail("migration_report_prepare_failed")
	}
	if err := checkLifecycleFence(opt); err != nil {
		return result, err
	}
	commitAttempted = true
	if tx.Commit(ctx) != nil {
		return result, operationFailure{code: "migration_commit_outcome_unknown", outcome: "unknown"}
	}
	committed = true
	if lease.Close() != nil {
		return result, operationFailure{code: "main_lease_close_failed", outcome: "committed_cleanup_failed"}
	}
	pool.Close()
	if checkLifecycleFence(opt) != nil {
		return result, operationFailure{code: "lifecycle_fence_invalid", outcome: "committed_cleanup_failed"}
	}
	if output.publish() != nil {
		return result, operationFailure{code: "migration_report_publication_failed", outcome: "committed_report_failed"}
	}
	return result, nil
}
