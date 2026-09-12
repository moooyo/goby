//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
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
			Database: candidateName, Role: candidateName, DatabaseOID: 101, DatabaseOwnerOID: 102,
			RoleOID: 102, SchemaOID: 2200, SchemaOwnerOID: 6171, SchemaOwnerKind: "pg_database_owner",
			Cluster: clusterProof{SystemIdentifier: "123456789", PostmasterPID: 301, PostmasterStartTicks: "401",
				PostmasterStartMicroseconds: 501, BootID: "01234567-89ab-cdef-0123-456789abcdef",
				DataDirectory: dataDirectory, PostgreSQLVersionNum: 170011, Port: 15432},
			DatabaseACL:    aclState{Raw: &databaseACL, Grants: []aclGrant{{Grantor: "@database_role", Grantee: "@database_role", Privilege: "CONNECT"}}},
			SchemaACL:      aclState{Raw: &schemaACL, Grants: []aclGrant{{Grantor: "@object_owner", Grantee: "@public", Privilege: "USAGE"}}},
			RoleProperties: json.RawMessage(postgresRoleProperties),
		},
		Tables: []tableState{{Name: "sessions", OID: 601, OwnerOID: 102,
			Columns: []columnState{{Name: "id", Number: 1, TypeOID: 25, TypeModifier: -1, Type: "text", NotNull: true, CollationOID: 100}},
			Rows:    75, SHA256: strings.Repeat("c", 64)}},
		Relations: []relationState{{Name: "sessions", OID: 601, OwnerOID: 102, Kind: "r",
			ACL:        aclState{Raw: &databaseACL, Grants: []aclGrant{{Grantor: "@database_role", Grantee: "@database_role", Privilege: "SELECT"}}},
			ColumnACLs: []columnACLState{{Name: "id", Number: 1, ACL: aclState{Grants: []aclGrant{}}}}}},
		Checks: []checkState{{Table: "sessions", Name: "sessions_kind_check", OID: 701, Columns: []string{"kind"},
			Definition: "CHECK (kind <> '')", Validated: true}},
		RecoveryBinding: bindingState{DeploymentID: strings.Repeat("d", 32), GenerationID: "", Slot: "primary", SHA256: strings.Repeat("e", 64)},
		Profile:         profileState{Sessions: 75, Devices: 64, ActivityEntries: 167, LibraryRoots: 4, RootMappingValid: true},
	}
	for index, name := range expectedSequences {
		state.Sequences = append(state.Sequences, sequenceState{Name: name, OID: uint32(801 + index), OwnerOID: 102,
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
		{"profile", func(value *capturedState) { value.Profile.Sessions++ }},
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
