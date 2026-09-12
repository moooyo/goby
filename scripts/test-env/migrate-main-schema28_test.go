//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// These are PostgreSQL jsonb transport bytes and the bytes retained when a
// report marshals that RawMessage. The field order and every value are equal.
const postgresRoleProperties = `{"limit": -1, "login": true, "super": false, "bypass": false, "config": null, "inherit": false, "createdb": false, "createrole": false, "replication": false, "valid_until": null}`
const compactRoleProperties = `{"limit":-1,"login":true,"super":false,"bypass":false,"config":null,"inherit":false,"createdb":false,"createrole":false,"replication":false,"valid_until":null}`

func roleStateFixture() capturedState {
	databaseACL := "fixture=CTc/fixture"
	schemaACL := "{pg_database_owner=UC/pg_database_owner,=U/pg_database_owner}"
	state := capturedState{
		SchemaVersion: 27, TrustedCatalogVerified: true, CatalogSHA256: strings.Repeat("a", 64),
		MigrationPrefixSHA256: strings.Repeat("b", 64), RowHashFormat: "goby-canonical-jsonb-sha256-multiset-v1",
		Identity: databaseIdentity{
			Database: mainName, Role: mainName, DatabaseOID: mainDatabaseOID, DatabaseOwnerOID: mainRoleOID,
			RoleOID: mainRoleOID, SchemaOID: mainSchemaOID, SchemaOwnerOID: mainSchemaOwnerOID, SchemaOwnerKind: "pg_database_owner",
			Cluster: clusterProof{SystemIdentifier: mainSystemIdentifier, PostmasterPID: 301, PostmasterStartTicks: "401",
				PostmasterStartMicroseconds: 501, BootID: "01234567-89ab-cdef-0123-456789abcdef",
				DataDirectory: dataDirectory, PostgreSQLVersionNum: 170011, Port: 5432},
			DatabaseACL:    aclState{Raw: &databaseACL, Grants: []aclGrant{{Grantor: "@database_role", Grantee: "@database_role", Privilege: "CONNECT"}}},
			SchemaACL:      aclState{Raw: &schemaACL, Grants: []aclGrant{{Grantor: "@object_owner", Grantee: "@public", Privilege: "USAGE"}}},
			RoleProperties: json.RawMessage(postgresRoleProperties),
		},
		Tables: []tableState{{Name: "sessions", OID: 601, OwnerOID: mainRoleOID,
			Columns: []columnState{{Name: "id", Number: 1, TypeOID: 25, TypeModifier: -1, Type: "text", NotNull: true, CollationOID: 100}},
			Rows:    12, SHA256: strings.Repeat("c", 64)}},
		Relations: []relationState{{Name: "sessions", OID: 601, OwnerOID: mainRoleOID, Kind: "r",
			ACL:        aclState{Raw: &databaseACL, Grants: []aclGrant{{Grantor: "@database_role", Grantee: "@database_role", Privilege: "SELECT"}}},
			ColumnACLs: []columnACLState{{Name: "id", Number: 1, ACL: aclState{Grants: []aclGrant{}}}}}},
		Checks: []checkState{{Table: "sessions", Name: "sessions_kind_check", OID: 701, Columns: []string{"kind"},
			Definition: "CHECK (kind <> '')", Validated: true}},
		RecoveryBinding: bindingState{DeploymentID: mainDeploymentID, GenerationID: "", Slot: "primary", SHA256: strings.Repeat("e", 64)},
		Profile:         profileState{Sessions: 12, Devices: 9, ActivityEntries: 23, LibraryRoots: 2, RootMappingsSHA256: strings.Repeat("f", 64)},
	}
	for index, name := range expectedSequences {
		state.Sequences = append(state.Sequences, sequenceState{Name: name, OID: uint32(801 + index), OwnerOID: mainRoleOID,
			LastValue: "901", LogCount: "31", IsCalled: true})
	}
	return state
}

func cloneRoleState(t *testing.T, value capturedState) capturedState {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode the in-memory state fixture")
	}
	var result capturedState
	if strictJSON(raw, &result) != nil {
		t.Fatal("decode the in-memory state fixture")
	}
	return result
}

func changedRoleProperty(t *testing.T, name, before, after string) json.RawMessage {
	t.Helper()
	needle := `"` + name + `": ` + before
	if strings.Count(postgresRoleProperties, needle) != 1 {
		t.Fatal("the in-memory property fixture is ambiguous")
	}
	return json.RawMessage(strings.Replace(postgresRoleProperties, needle, `"`+name+`": `+after, 1))
}

func requireInvalidRoleProperties(t *testing.T, raw json.RawMessage) {
	t.Helper()
	value, err := normalizeRoleProperties(raw)
	var failure operationFailure
	if value != nil || !errors.As(err, &failure) || failure.code != "role_properties_invalid" || failure.outcome != "failed" {
		t.Fatal("an invalid role property document did not fail with its fixed safe code")
	}
	first := roleStateFixture()
	first.Identity.RoleProperties = append(json.RawMessage(nil), raw...)
	second := first
	if sameCapturedState(first, second) || sameDatabaseIdentity(first.Identity, second.Identity) {
		t.Fatal("matching malformed role property bytes were accepted by a production comparison")
	}
}

func TestRolePropertiesPostgreSQLWhitespaceReportRoundTrip(t *testing.T) {
	if len(postgresRoleProperties) != 178 || len(compactRoleProperties) != 159 {
		t.Fatal("the PostgreSQL/report transport regression fixture changed")
	}
	live := roleStateFixture()
	raw, err := json.Marshal(report{Marker: reportMarker, Version: 1, Mode: "backup", State: live})
	if err != nil {
		t.Fatal("marshal the real report type")
	}
	var retained report
	if strictJSON(raw, &retained) != nil {
		t.Fatal("decode the real report type")
	}
	if string(retained.State.Identity.RoleProperties) != compactRoleProperties || reflect.DeepEqual(live, retained.State) {
		t.Fatal("the fixture did not reproduce RawMessage whitespace compaction")
	}
	if !sameCapturedState(live, retained.State) || !sameDatabaseIdentity(live.Identity, retained.State.Identity) {
		t.Fatal("the production comparisons rejected the unchanged PostgreSQL/report round trip")
	}
	left, leftErr := normalizeRoleProperties(live.Identity.RoleProperties)
	right, rightErr := normalizeRoleProperties(retained.State.Identity.RoleProperties)
	if leftErr != nil || rightErr != nil || !bytes.Equal(left, right) || string(left) != compactRoleProperties {
		t.Fatal("the exact complete role document was not normalized")
	}
	liveDigest, liveErr := stateDigest(live)
	retainedDigest, retainedErr := stateDigest(retained.State)
	if liveErr != nil || retainedErr != nil || liveDigest != retainedDigest {
		t.Fatal("transport whitespace changed the retained full-state digest")
	}
}

func TestRolePropertiesPrivilegeChangesRemainSignificant(t *testing.T) {
	for _, name := range []string{"login", "super", "createdb", "createrole", "replication", "bypass", "inherit"} {
		t.Run(name, func(t *testing.T) {
			before, after := "false", "true"
			if name == "login" {
				before, after = "true", "false"
			}
			baseline := roleStateFixture()
			changed := cloneRoleState(t, baseline)
			changed.Identity.RoleProperties = changedRoleProperty(t, name, before, after)
			if _, err := normalizeRoleProperties(changed.Identity.RoleProperties); err != nil {
				t.Fatal("a valid boolean change was incorrectly rejected as a format error")
			}
			if sameCapturedState(baseline, changed) || sameDatabaseIdentity(baseline.Identity, changed.Identity) {
				t.Fatal("normalization hid a changed role privilege")
			}
		})
	}
}

func TestRolePropertiesIntegerAndNullableValuesArePreserved(t *testing.T) {
	for _, fixture := range []struct{ name, before, after string }{
		{"limit", "-1", "-2147483648"}, {"limit", "-1", "0"}, {"limit", "-1", "12"}, {"limit", "-1", "2147483647"},
		{"config", "null", "[]"}, {"config", "null", `["work_mem=4MB","setting=two words"]`},
		{"valid_until", "null", `"infinity"`}, {"valid_until", "null", `"2026-09-12T00:00:00+00:00"`},
	} {
		t.Run(fixture.name+"_"+fixture.after, func(t *testing.T) {
			baseline := roleStateFixture()
			changed := cloneRoleState(t, baseline)
			changed.Identity.RoleProperties = changedRoleProperty(t, fixture.name, fixture.before, fixture.after)
			normalized, err := normalizeRoleProperties(changed.Identity.RoleProperties)
			if err != nil || !bytes.Contains(normalized, []byte(`"`+fixture.name+`":`+fixture.after)) {
				t.Fatal("normalization changed a valid integer or nullable value")
			}
			if sameCapturedState(baseline, changed) || sameDatabaseIdentity(baseline.Identity, changed.Identity) {
				t.Fatal("normalization hid a changed role property value")
			}
		})
	}
}

func TestRolePropertiesRejectMissingUnknownDuplicateAndWrongShapes(t *testing.T) {
	for _, raw := range []string{"null", "[]", "{}", `"role"`, `{"login":true}`,
		strings.TrimSuffix(postgresRoleProperties, "}") + `, "super": false}`,
		strings.Replace(postgresRoleProperties, `"super": false`, `"Super": false`, 1)} {
		requireInvalidRoleProperties(t, json.RawMessage(raw))
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(postgresRoleProperties), &fields) != nil {
		t.Fatal("decode the in-memory role fields")
	}
	for name := range fields {
		t.Run("missing_"+name, func(t *testing.T) {
			copyFields := map[string]json.RawMessage{}
			for key, value := range fields {
				if key != name {
					copyFields[key] = value
				}
			}
			raw, err := json.Marshal(copyFields)
			if err != nil {
				t.Fatal("encode the missing-field fixture")
			}
			requireInvalidRoleProperties(t, raw)
		})
	}
	for _, extra := range []string{"unknown", "Super"} {
		t.Run(extra, func(t *testing.T) {
			copyFields := map[string]json.RawMessage{}
			for key, value := range fields {
				copyFields[key] = value
			}
			copyFields[extra] = json.RawMessage(`"untrusted fixture text"`)
			raw, err := json.Marshal(copyFields)
			if err != nil {
				t.Fatal("encode the unknown-field fixture")
			}
			requireInvalidRoleProperties(t, raw)
		})
	}
}

func TestRolePropertiesRejectValueTypeChanges(t *testing.T) {
	for _, name := range []string{"login", "super", "createdb", "createrole", "replication", "bypass", "inherit"} {
		before := "false"
		if name == "login" {
			before = "true"
		}
		for _, after := range []string{"null", "0", `"false"`, "[]", "{}"} {
			t.Run(name+"_"+after, func(t *testing.T) { requireInvalidRoleProperties(t, changedRoleProperty(t, name, before, after)) })
		}
	}
	for _, fixture := range []struct {
		name, before string
		invalid      []string
	}{
		{"limit", "-1", []string{"null", "true", `"12"`, "12.0", "1e1", "2147483648", "-2147483649", "[]"}},
		{"config", "null", []string{"true", "12", `"setting=value"`, "{}", "[null]", "[12]", "[true]", `["valid",null]`}},
		{"valid_until", "null", []string{"true", "12", "[]", "{}"}},
	} {
		for _, after := range fixture.invalid {
			t.Run(fixture.name+"_"+after, func(t *testing.T) {
				requireInvalidRoleProperties(t, changedRoleProperty(t, fixture.name, fixture.before, after))
			})
		}
	}
}

func TestRolePropertiesNormalizationDoesNotMutateOrAliasInput(t *testing.T) {
	raw := json.RawMessage(postgresRoleProperties)
	before := append([]byte(nil), raw...)
	normalized, err := normalizeRoleProperties(raw)
	if err != nil || !bytes.Equal(raw, before) {
		t.Fatal("normalization modified its input")
	}
	normalized[0] = '['
	if !bytes.Equal(raw, before) {
		t.Fatal("normalized bytes alias the input")
	}
}

func TestRoleNormalizationDoesNotHideOtherCapturedStateChanges(t *testing.T) {
	changes := []struct {
		name   string
		mutate func(*capturedState)
	}{
		{"database_oid", func(value *capturedState) { value.Identity.DatabaseOID++ }},
		{"database_owner_oid", func(value *capturedState) { value.Identity.DatabaseOwnerOID++ }},
		{"role_oid", func(value *capturedState) { value.Identity.RoleOID++ }},
		{"schema_oid", func(value *capturedState) { value.Identity.SchemaOID++ }},
		{"schema_owner_oid", func(value *capturedState) { value.Identity.SchemaOwnerOID++ }},
		{"cluster", func(value *capturedState) { value.Identity.Cluster.PostmasterStartTicks = "changed" }},
		{"database_raw_acl", func(value *capturedState) { *value.Identity.DatabaseACL.Raw = "changed" }},
		{"schema_acl", func(value *capturedState) { value.Identity.SchemaACL.Grants[0].Grantable = true }},
		{"table_oid", func(value *capturedState) { value.Tables[0].OID++ }},
		{"table_owner", func(value *capturedState) { value.Tables[0].OwnerOID++ }},
		{"table_columns", func(value *capturedState) { value.Tables[0].Columns[0].NotNull = false }},
		{"table_rows", func(value *capturedState) { value.Tables[0].Rows++ }},
		{"table_digest", func(value *capturedState) { value.Tables[0].SHA256 = strings.Repeat("f", 64) }},
		{"sequence_oid", func(value *capturedState) { value.Sequences[0].OID++ }},
		{"sequence_owner", func(value *capturedState) { value.Sequences[0].OwnerOID++ }},
		{"sequence_value", func(value *capturedState) { value.Sequences[0].LastValue = "902" }},
		{"sequence_log_count", func(value *capturedState) { value.Sequences[0].LogCount = "32" }},
		{"sequence_called", func(value *capturedState) { value.Sequences[0].IsCalled = false }},
		{"relation_owner", func(value *capturedState) { value.Relations[0].OwnerOID++ }},
		{"relation_acl", func(value *capturedState) { value.Relations[0].ACL.Grants[0].Grantable = true }},
		{"column_acl", func(value *capturedState) {
			value.Relations[0].ColumnACLs[0].ACL.Grants = []aclGrant{{Privilege: "SELECT"}}
		}},
		{"check_oid", func(value *capturedState) { value.Checks[0].OID++ }},
		{"check_definition", func(value *capturedState) { value.Checks[0].Definition = "changed" }},
		{"recovery_binding", func(value *capturedState) { value.RecoveryBinding.SHA256 = strings.Repeat("f", 64) }},
		{"recovery_generation", func(value *capturedState) { value.RecoveryBinding.GenerationID = strings.Repeat("1", 32) }},
		{"recovery_slot", func(value *capturedState) { value.RecoveryBinding.Slot = "recovery" }},
		{"profile", func(value *capturedState) { value.Profile.Sessions++ }},
		{"root_mapping", func(value *capturedState) { value.Profile.RootMappingsSHA256 = strings.Repeat("0", 64) }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			baseline := roleStateFixture()
			changed := cloneRoleState(t, baseline)
			if !sameCapturedState(baseline, changed) {
				t.Fatal("the unchanged state fixture was not accepted")
			}
			change.mutate(&changed)
			if sameCapturedState(baseline, changed) {
				t.Fatal("role normalization hid another captured state change")
			}
		})
	}
}

func mainIntentFixture() intentDocument {
	run := "20260912_123456_abcdef123456"
	rehearsal := "goby_main_s55_rehearsal_" + strings.ReplaceAll(run, "_", "")
	return intentDocument{
		Marker: intentMarker, Version: 1, RunID: run, EvidenceRoot: evidenceBase + "/run-" + run,
		SourceManifestSHA256: sourceManifest, Source55BinarySHA256: strings.Repeat("a", 64), HelperSHA256: strings.Repeat("b", 64),
		Cluster: roleStateFixture().Identity.Cluster,
		Main: targetIdentity{Database: mainName, Role: mainName, DatabaseOID: mainDatabaseOID, RoleOID: mainRoleOID,
			SchemaOID: mainSchemaOID, SchemaOwnerOID: mainSchemaOwnerOID, RecoveryDeploymentID: mainDeploymentID,
			RecoveryBindingSHA256: strings.Repeat("c", 64)},
		Rehearsal: rehearsalScope{Database: rehearsal, Role: rehearsal, OwnerTag: ownerMarker + ":" + run},
		Lifecycle: lifecycleScope{Directory: lifecycleDirectory, LockFile: lifecycleLockFile,
			DirectoryIdentity: lifecycleFileIdentity{Device: unix.Mkdev(8, 1), Inode: 2001, UID: lifecycleUID, GID: 986, Mode: 0o700},
			LockIdentity:      lifecycleFileIdentity{Device: unix.Mkdev(8, 1), Inode: 2002, UID: lifecycleUID, GID: 986, Mode: 0o600}},
	}
}

func TestMainIntentUsesOnlyPinnedPrimaryAndFreshIndependentRehearsal(t *testing.T) {
	if validateMainScope(mainIntentFixture()) != nil {
		t.Fatal("the pinned main scope was rejected")
	}
	for _, fixture := range []struct {
		name   string
		change func(*intentDocument)
	}{
		{"candidate_database", func(v *intentDocument) { v.Main.Database = "goby_client_m3e" }},
		{"inactive_role", func(v *intentDocument) { v.Main.Role = "goby_recovery_m5j" }},
		{"database_oid", func(v *intentDocument) { v.Main.DatabaseOID++ }},
		{"role_oid", func(v *intentDocument) { v.Main.RoleOID++ }},
		{"schema_oid", func(v *intentDocument) { v.Main.SchemaOID++ }},
		{"schema_owner_oid", func(v *intentDocument) { v.Main.SchemaOwnerOID++ }},
		{"deployment", func(v *intentDocument) { v.Main.RecoveryDeploymentID = strings.Repeat("d", 32) }},
		{"binding_missing", func(v *intentDocument) { v.Main.RecoveryBindingSHA256 = "" }},
		{"workspace_port", func(v *intentDocument) { v.Cluster.Port = 15432 }},
		{"workspace_data", func(v *intentDocument) { v.Cluster.DataDirectory = "/var/lib/postgresql/goby-workspace-v1/data" }},
		{"cluster_system", func(v *intentDocument) { v.Cluster.SystemIdentifier = "12345" }},
		{"cluster_pid", func(v *intentDocument) { v.Cluster.PostmasterPID = 0 }},
		{"cluster_ticks", func(v *intentDocument) { v.Cluster.PostmasterStartTicks = "0" }},
		{"cluster_start", func(v *intentDocument) { v.Cluster.PostmasterStartMicroseconds = 0 }},
		{"cluster_boot", func(v *intentDocument) { v.Cluster.BootID = "invalid" }},
		{"postgres_major", func(v *intentDocument) { v.Cluster.PostgreSQLVersionNum = 180001 }},
		{"foreign_evidence", func(v *intentDocument) { v.EvidenceRoot = workRoot + "/run-" + v.RunID }},
		{"rehearsal_main", func(v *intentDocument) { v.Rehearsal.Database = mainName }},
		{"rehearsal_inactive", func(v *intentDocument) { v.Rehearsal.Role = "goby_recovery_m5j" }},
		{"rehearsal_tag", func(v *intentDocument) { v.Rehearsal.OwnerTag = intentMarker + ":" + v.RunID }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value := mainIntentFixture()
			fixture.change(&value)
			if validateMainScope(value) == nil {
				t.Fatal("a changed primary scope passed the production admission boundary")
			}
		})
	}
}

func TestMainConnectionURLCannotSelectWorkspaceOrInactiveDatabase(t *testing.T) {
	base := "postgresql://goby_test:" + strings.Repeat("p", 24) + "@127.0.0.1:5432/goby_test?sslmode=disable"
	if validateURL(base, mainName) != nil {
		t.Fatal("the pinned ordinary primary connection was rejected")
	}
	for _, raw := range []string{
		strings.Replace(base, ":5432/", ":15432/", 1), strings.ReplaceAll(base, "goby_test", "goby_recovery_m5j"),
		strings.ReplaceAll(base, "goby_test", "goby_client_m3e"), strings.Replace(base, "127.0.0.1", "localhost", 1),
		base + "&options=-csearch_path%3Dother", base + "#fragment",
	} {
		if validateURL(raw, mainName) == nil {
			t.Fatal("a connection outside the exact primary scope was accepted")
		}
	}
}

func TestMainBindingPreservesSelectedGenerationAndExactRawValue(t *testing.T) {
	for _, generation := range []string{"", strings.Repeat("1", 32)} {
		raw := fmt.Sprintf(`{"version":1,"deploymentId":%q,"generationId":%q,"slot":"primary"}`, mainDeploymentID, generation)
		expected := mainIntentFixture().Main
		expected.RecoveryBindingSHA256 = sha([]byte(raw))
		actual, err := boundRecoveryMarker(raw, expected)
		if err != nil || actual.DeploymentID != mainDeploymentID || actual.GenerationID != generation ||
			actual.Slot != "primary" || actual.SHA256 != expected.RecoveryBindingSHA256 {
			t.Fatal("a pinned default or selected primary generation was not retained exactly")
		}
		if _, err := boundRecoveryMarker(" "+raw, expected); err == nil {
			t.Fatal("equivalent marker JSON hid changed raw database bytes")
		}
		foreign := strings.Replace(raw, `"primary"`, `"recovery"`, 1)
		expected.RecoveryBindingSHA256 = sha([]byte(foreign))
		if _, err := boundRecoveryMarker(foreign, expected); err == nil {
			t.Fatal("a self-consistent inactive-slot marker was accepted")
		}
	}
}

func TestMainPopulationUsesACompleteDynamicBaseline(t *testing.T) {
	for _, profile := range []profileState{
		{Sessions: 0, Devices: 0, ActivityEntries: 0, LibraryRoots: 0, RootMappingsSHA256: strings.Repeat("0", 64)},
		{Sessions: 128, Devices: 71, ActivityEntries: 402, LibraryRoots: 7, RootMappingsSHA256: strings.Repeat("1", 64)},
	} {
		live := roleStateFixture()
		live.Profile = profile
		retained := cloneRoleState(t, live)
		if !sameCapturedState(live, retained) {
			t.Fatal("a dynamic main population was rejected after the captured-state round trip")
		}
		retained.Profile.RootMappingsSHA256 = strings.Repeat("2", 64)
		if sameCapturedState(live, retained) {
			t.Fatal("a changed historical root mapping was hidden by the dynamic profile")
		}
	}
}

func TestLifecycleDescriptorsAreExplicitAndMigrateOnly(t *testing.T) {
	valid := options{mode: "migrate", lifecycleDirectoryFD: 7, lifecycleLockFD: 8,
		lifecycleDirectoryFDSet: true, lifecycleLockFDSet: true}
	if !validLifecycleArguments(valid) {
		t.Fatal("two explicit distinct inherited descriptors were rejected")
	}
	for _, mode := range []string{"inspect", "backup", "rehearse"} {
		value := options{mode: mode, lifecycleDirectoryFD: -1, lifecycleLockFD: -1}
		if !validLifecycleArguments(value) {
			t.Fatal("a read or isolated rehearsal mode required a live lifecycle fence")
		}
		value.lifecycleDirectoryFDSet = true
		if validLifecycleArguments(value) {
			t.Fatal("an explicit lifecycle descriptor flag was ignored outside migrate")
		}
		value.lifecycleDirectoryFDSet, value.lifecycleLockFDSet = false, true
		if validLifecycleArguments(value) {
			t.Fatal("an explicit negative lock descriptor flag was ignored outside migrate")
		}
	}
	for _, mutate := range []func(*options){
		func(v *options) { v.lifecycleDirectoryFDSet = false }, func(v *options) { v.lifecycleLockFDSet = false },
		func(v *options) { v.lifecycleDirectoryFD = -1 }, func(v *options) { v.lifecycleLockFD = 2 },
		func(v *options) { v.lifecycleLockFD = v.lifecycleDirectoryFD },
	} {
		value := valid
		mutate(&value)
		if validLifecycleArguments(value) {
			t.Fatal("missing, standard, or shared descriptors passed the migrate boundary")
		}
	}
}

func TestLifecycleScopeRequiresPinnedExistingServiceObjects(t *testing.T) {
	for _, mutate := range []func(*lifecycleScope){
		func(v *lifecycleScope) { v.Directory += "/alias" }, func(v *lifecycleScope) { v.LockFile += ".other" },
		func(v *lifecycleScope) { v.DirectoryIdentity.UID = 0 }, func(v *lifecycleScope) { v.LockIdentity.UID = 0 },
		func(v *lifecycleScope) { v.DirectoryIdentity.Mode = 0o755 }, func(v *lifecycleScope) { v.LockIdentity.Mode = 0o644 },
		func(v *lifecycleScope) { v.DirectoryIdentity.Inode = 0 }, func(v *lifecycleScope) { v.LockIdentity.Device = 0 },
		func(v *lifecycleScope) { v.LockIdentity.Inode = v.DirectoryIdentity.Inode },
	} {
		value := mainIntentFixture().Lifecycle
		mutate(&value)
		if validLifecycleScope(value) {
			t.Fatal("a changed lifecycle scope passed admission")
		}
	}
	value := mainIntentFixture().Lifecycle
	value.DirectoryIdentity.GID, value.LockIdentity.GID = 995, 995
	if !validLifecycleScope(value) {
		t.Fatal("a fresh pinned service group was replaced with a fixture-specific group")
	}
}

func TestLifecycleDescriptorChecksPathTypeOwnerLinksAndPinnedMetadata(t *testing.T) {
	for _, directory := range []bool{true, false} {
		scope := mainIntentFixture().Lifecycle
		path, pin := scope.LockFile, scope.LockIdentity
		kind := uint32(unix.S_IFREG)
		if directory {
			path, pin, kind = scope.Directory, scope.DirectoryIdentity, unix.S_IFDIR
		}
		valid := unix.Stat_t{Dev: pin.Device, Ino: pin.Inode, Uid: pin.UID, Gid: pin.GID, Mode: kind | pin.Mode, Nlink: 1}
		if !lifecycleDescriptorMatches(valid, valid, path, path, pin, directory) {
			t.Fatal("the exact inherited descriptor and current path were rejected")
		}
		for _, change := range []func(*unix.Stat_t){
			func(v *unix.Stat_t) { v.Dev++ }, func(v *unix.Stat_t) { v.Ino++ }, func(v *unix.Stat_t) { v.Uid = 0 },
			func(v *unix.Stat_t) { v.Gid++ }, func(v *unix.Stat_t) { v.Mode |= 0o4000 },
			func(v *unix.Stat_t) { v.Mode = unix.S_IFLNK | pin.Mode }, func(v *unix.Stat_t) { v.Nlink = 0 },
		} {
			changed := valid
			change(&changed)
			if lifecycleDescriptorMatches(changed, valid, path, path, pin, directory) ||
				lifecycleDescriptorMatches(valid, changed, path, path, pin, directory) {
				t.Fatal("opened or current lifecycle metadata drift was accepted")
			}
		}
		for _, link := range []string{path + " (deleted)", path + "/alias", ""} {
			if lifecycleDescriptorMatches(valid, valid, link, path, pin, directory) {
				t.Fatal("a stale or aliased descriptor target was accepted")
			}
		}
		if !directory {
			for _, change := range []func(*unix.Stat_t){func(v *unix.Stat_t) { v.Nlink = 2 }, func(v *unix.Stat_t) { v.Size = 1 }} {
				changed := valid
				change(&changed)
				if lifecycleDescriptorMatches(changed, valid, path, path, pin, false) {
					t.Fatal("a hard-linked or nonempty lifecycle lock file was accepted")
				}
			}
		}
	}
}

func TestLifecycleFDInfoRequiresAnAlreadyHeldWholeFileExclusiveFlock(t *testing.T) {
	device, inode := unix.Mkdev(259, 4097), uint64(123456789)
	valid := fmt.Sprintf("pos:\t0\nflags:\t0100000\nmnt_id:\t28\nino:\t%d\nlock:\t1: FLOCK ADVISORY WRITE 301 %02x:%02x:%d 0 EOF\n",
		inode, unix.Major(device), unix.Minor(device), inode)
	for _, raw := range []string{valid, strings.Replace(valid, "WRITE 301", "WRITE 0", 1),
		strings.ReplaceAll(valid, " FLOCK ADVISORY WRITE ", "\tFLOCK  ADVISORY\tWRITE\t")} {
		if !heldLifecycleFlock([]byte(raw), device, inode) {
			t.Fatal("the actual Linux inherited exclusive FLOCK record was rejected")
		}
	}
	for _, raw := range []string{
		"pos:\t0\n", strings.TrimSuffix(valid, "\n"), strings.Repeat("x", 65537) + "\n", valid + "lock: 2: FLOCK ADVISORY WRITE 301 103:1001:123456789 0 EOF\n",
		strings.Replace(valid, "FLOCK", "POSIX", 1), strings.Replace(valid, "FLOCK", "OFDLCK", 1),
		strings.Replace(valid, "ADVISORY", "MANDATORY", 1), strings.Replace(valid, "WRITE", "READ", 1),
		strings.Replace(valid, "1: FLOCK", "1: -> FLOCK", 1), strings.Replace(valid, "1: FLOCK", "+1: FLOCK", 1),
		strings.Replace(valid, "WRITE 301", "WRITE -1", 1), strings.Replace(valid, "WRITE 301", "WRITE 2147483648", 1),
		strings.Replace(valid, "103:1001:123456789", "103:1001:123456788", 1),
		strings.Replace(valid, "103:1001:123456789", "103:1002:123456789", 1),
		strings.Replace(valid, "103:1001:123456789", "104:1001:123456789", 1),
		strings.Replace(valid, "103:1001:123456789", "+103:1001:123456789", 1),
		strings.Replace(valid, "103:1001:123456789", "103:1001:18446744073709551616", 1),
		strings.Replace(valid, " 0 EOF", " 1 EOF", 1), strings.Replace(valid, " 0 EOF", " 0 999", 1),
		strings.Replace(valid, " 0 EOF", " 0 EOF extra", 1),
	} {
		if heldLifecycleFlock([]byte(raw), device, inode) {
			t.Fatal("a missing, shared, foreign, malformed, or partial lifecycle lock was accepted")
		}
	}
}
