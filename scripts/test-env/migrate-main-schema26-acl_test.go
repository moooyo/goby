//go:build ignore

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Argument parsing does not open files or connect to PostgreSQL. Each repair
// flag selects exactly one fixed root; it cannot widen a previous run's scope.
func TestMainSchema26RepairRootsAreExclusive(t *testing.T) {
	runID := "run-20260911T000000Z-" + strings.Repeat("a", 24)
	digest := strings.Repeat("a", 64)
	for _, fixture := range []struct {
		name      string
		root      string
		proofRoot string
		baseline  bool
		rehearsal bool
		reject    bool
	}{
		{name: "normal", root: operationRoot},
		{name: "baseline_repair", root: baselineRepairRoot, baseline: true},
		{name: "rehearsal_repair", root: rehearsalRepairRoot, rehearsal: true},
		{name: "new_root_without_flag", root: rehearsalRepairRoot, reject: true},
		{name: "new_flag_old_root", root: operationRoot, rehearsal: true, reject: true},
		{name: "baseline_flag_new_root", root: rehearsalRepairRoot, baseline: true, reject: true},
		{name: "new_flag_baseline_root", root: baselineRepairRoot, rehearsal: true, reject: true},
		{name: "both_repair_flags", root: rehearsalRepairRoot, baseline: true, rehearsal: true, reject: true},
		{name: "mixed_proof_root", root: rehearsalRepairRoot, proofRoot: baselineRepairRoot, rehearsal: true, reject: true},
		{name: "unowned_root", root: "/opt/goby-test/backups/unowned-v1", rehearsal: true, reject: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			proofRoot := fixture.proofRoot
			if proofRoot == "" {
				proofRoot = fixture.root
			}
			args := []string{"inspect", "--expected-os-uid", "0", "--expected-database", "goby_test",
				"--expected-database-oid", "16385", "--expected-role", "goby_test", "--expected-role-oid", "16384",
				"--expected-system-identifier", mainSystemIdentifier, "--expected-port", "5432",
				"--expected-deployment-id", mainDeploymentID, "--expected-schema", "25", "--snapshot-id", "1-1-1",
				"--output", fixture.root + "/" + runID + "/baseline.json",
				"--cluster-proof", proofRoot + "/" + runID + "/baseline-cluster.json", "--cluster-proof-sha256", digest,
				"--run-id", runID, "--source-manifest-sha256", digest, "--tool-manifest-sha256", digest,
				"--candidate-sha256", digest, "--expected-helper-sha256", digest}
			if fixture.baseline {
				args = append(args, "--repair-baseline")
			}
			if fixture.rehearsal {
				args = append(args, "--repair-rehearsal")
			}
			value, err := parseOptions(args)
			if (err != nil) != fixture.reject {
				t.Fatalf("repair root acceptance differs: rejected=%t, expected=%t", err != nil, fixture.reject)
			}
			if err == nil && (value.repairBaseline != fixture.baseline || value.repairRehearsal != fixture.rehearsal) {
				t.Fatal("argument parsing changed the selected repair mode")
			}
		})
	}
}

// Run this explicit-file regression only on the authorized PostgreSQL17 test
// environment. It reads constant ACL values and actual public catalog metadata
// inside one READ ONLY transaction; it never invokes the migration helper run,
// acquires a deployment lease, or creates database/filesystem objects.
func TestMainSchema26ColumnACLPostgreSQL17(t *testing.T) {
	configuration, err := pgx.ParseConfig(os.Getenv("GOBY_TEST_DATABASE_URL"))
	if err != nil || configuration.Host != "127.0.0.1" ||
		(configuration.Port != 5432 && configuration.Port != 15432) ||
		configuration.Database != "goby_test" || configuration.User != "goby_test" {
		t.Fatal("an explicit fixed test-environment ordinary database URL is required")
	}
	if configuration.RuntimeParams == nil {
		configuration.RuntimeParams = make(map[string]string)
	}
	configuration.RuntimeParams["application_name"] = "goby-main-schema26-acl-regression"
	configuration.RuntimeParams["default_transaction_read_only"] = "on"
	configuration.RuntimeParams["search_path"] = "pg_catalog"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, configuration)
	if err != nil {
		t.Fatal("open the authorized read-only PostgreSQL connection")
	}
	defer connection.Close(context.Background())
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin the read-only ACL observation")
	}
	defer tx.Rollback(context.Background())
	var version int
	var role uint32
	var safe, readonly bool
	if tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer,r.oid,
		current_user=session_user AND NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls),
		current_setting('transaction_read_only')='on' FROM pg_catalog.pg_roles r WHERE r.rolname=current_user`).Scan(
		&version, &role, &safe, &readonly) != nil || version/10000 != 17 || !safe || !readonly {
		t.Fatal("the ACL regression requires PostgreSQL17, an ordinary actor, and READ ONLY")
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT `+aclGrantsSQL("NULL::pg_catalog.aclitem[]", "NULL::pg_catalog.aclitem[]", "$1::oid"), role).Scan(&raw); err != nil {
		t.Fatal("a NULL column ACL must expand without an array-dimension error")
	}
	grants, err := decodeACLGrants(raw)
	if err != nil || string(raw) != "[]" || len(grants) != 0 {
		t.Fatal("a NULL column ACL must produce an exact empty JSON grants array")
	}
	relations, err := captureRelations(ctx, tx, role)
	if err != nil || len(relations) == 0 {
		t.Fatal("the actual public relation and column ACL capture must complete")
	}
	nullColumns := 0
	for _, relation := range relations {
		for _, column := range relation.ColumnACLs {
			if column.ACL.Raw == nil {
				nullColumns++
				if column.ACL.Grants == nil || len(column.ACL.Grants) != 0 {
					t.Fatal("an actual NULL column ACL acquired synthetic grants")
				}
			}
		}
	}
	if nullColumns == 0 {
		t.Fatal("the actual public catalog did not exercise any NULL column ACL")
	}
	// Keep the original failing expression as a real PostgreSQL negative
	// control. This final statement aborts only our read-only transaction.
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.aclexplode('{}'::pg_catalog.aclitem[])`).Scan(&count)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Message != "ACL arrays must be one-dimensional" {
		t.Fatal("the original empty-array expression did not reproduce its PostgreSQL17 rejection")
	}
}
