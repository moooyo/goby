//go:build linux

package backuppg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type restoreOwnershipTraceKey struct{}

type restoreValidationTrace struct {
	ownershipChecks int
	rawRestore      bool
	rawChecks       int
	finalChecks     int
	cancel          context.CancelFunc
}

func (trace *restoreValidationTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	if strings.HasPrefix(statement, "TRUNCATE TABLE ") {
		trace.rawRestore = true
	} else if strings.HasPrefix(statement, "SELECT CASE WHEN pg_catalog.octet_length(serialized.row_text)") {
		trace.rawRestore = false
	}
	if strings.HasPrefix(statement, "SELECT (SELECT count(*) FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL) = 1") {
		if trace.rawRestore {
			trace.rawChecks++
		}
		if trace.ownershipChecks == 3 {
			trace.finalChecks++
		}
	}
	if strings.HasPrefix(statement, "SELECT pg_catalog.pg_has_role(n.nspowner,'USAGE') AND") {
		return context.WithValue(ctx, restoreOwnershipTraceKey{}, true)
	}
	return ctx
}

func (trace *restoreValidationTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if marked, _ := ctx.Value(restoreOwnershipTraceKey{}).(bool); marked && data.Err == nil {
		trace.ownershipChecks++
		if trace.ownershipChecks == 3 && trace.cancel != nil {
			trace.cancel()
		}
	}
}

func tracedRestoreTarget(t *testing.T, ctx context.Context, target *pgxpool.Pool, trace *restoreValidationTrace) *pgxpool.Pool {
	t.Helper()
	configuration := target.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRestoreValidatesRawMigratedAndFinalizedResourcesAtTheirBoundaries(t *testing.T) {
	for _, version := range []int64{0, 28} {
		for _, finalized := range []bool{false, true} {
			t.Run(fmt.Sprintf("schema_%d/finalizer_%t", version, finalized), func(t *testing.T) {
				ctx, source, target, options := recoveryFixtureAtVersion(t, version)
				archive, facts := sourceArchive(t, ctx, source, options)
				trace := &restoreValidationTrace{}
				observedTarget := tracedRestoreTarget(t, ctx, target, trace)
				var finalizer Finalizer
				called := false
				if finalized {
					finalizer = func(ctx context.Context, tx pgx.Tx, _ RestoreResult) error {
						called = true
						_, err := tx.Exec(ctx, "UPDATE users SET name='Finalized user',normalized_name='finalized user' WHERE id='backup-admin'")
						return err
					}
				}
				result, err := restoreWithSource(ctx, source, observedTarget, archive, facts, options, finalizer)
				if err != nil || called != finalized || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) {
					t.Fatalf("resource validation changed restoration or finalization: %v", err)
				}
				wantFinal := 0
				if facts.SchemaVersion < result.CurrentVersion {
					wantFinal++
				}
				if finalized {
					wantFinal++
				}
				if trace.rawChecks != 1 || trace.finalChecks != wantFinal || trace.ownershipChecks != 3 {
					t.Fatalf("resource validation passes: raw=%d final=%d ownership=%d, want 1/%d/3", trace.rawChecks, trace.finalChecks, trace.ownershipChecks, wantFinal)
				}
			})
		}
	}
}

func TestRestoreCancellationAfterOwnershipStopsBeforeFinalizer(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	archive, facts := sourceArchive(t, ctx, source, options)
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	trace := &restoreValidationTrace{cancel: cancel}
	observedTarget := tracedRestoreTarget(t, ctx, target, trace)
	called := false
	_, err := RestoreFinalized(work, source, observedTarget, archive, facts, options, func(context.Context, pgx.Tx, RestoreResult) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called || trace.ownershipChecks != 3 || trace.rawChecks != 1 {
		t.Fatalf("cancelled restored transaction entered finalization: error=%v called=%t ownership=%d raw=%d", err, called, trace.ownershipChecks, trace.rawChecks)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
}
