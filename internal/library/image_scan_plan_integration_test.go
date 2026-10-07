//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type imageScanPlanTrace struct {
	*imageScanNoopProofTrace
	mu        sync.Mutex
	statement string
	arguments []any
}

func (trace *imageScanPlanTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.imageScanNoopProofTrace.TraceQueryStart(ctx, conn, data)
	if conn == trace.authority.owner.Load() && strings.HasPrefix(data.SQL, "/* image_catalog_unchanged */") {
		trace.mu.Lock()
		trace.statement, trace.arguments = data.SQL, append([]any(nil), data.Args...)
		trace.mu.Unlock()
	}
	return ctx
}

func (trace *imageScanPlanTrace) query(t *testing.T, wantNamed bool) (string, []any) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	args := append([]any(nil), trace.arguments...)
	if trace.statement == "" || len(args) == 0 {
		t.Fatal("the actual image comparison query was not captured")
	}
	mode, overridden := args[0].(pgx.QueryExecMode)
	if overridden != wantNamed || overridden && mode != pgx.QueryExecModeCacheStatement {
		t.Fatalf("image comparison changed its configured execution mode: overridden=%t mode=%v", overridden, mode)
	}
	if overridden {
		args = args[1:]
	}
	if len(args) != 17 {
		t.Fatalf("image comparison changed its data parameter count: %d", len(args))
	}
	return trace.statement, args
}

type imageScanPlanConnectTrace struct{ *imageScanPlanTrace }

func (*imageScanPlanConnectTrace) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	data.ConnConfig.StatementCacheCapacity = 0
	return ctx
}

func (*imageScanPlanConnectTrace) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

func imageScanPlanPrepare(t *testing.T, test scanReadPlanConfiguration) (imageScanReadonlyFixture, *imageScanPlanTrace) {
	t.Helper()
	var trace *imageScanPlanTrace
	fixture := imageScanReadonlyPrepareWithConfig(t, true, func(config *pgxpool.Config) {
		trace = &imageScanPlanTrace{imageScanNoopProofTrace: config.ConnConfig.Tracer.(*imageScanNoopProofTrace)}
		config.ConnConfig.Tracer = trace
		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
		if test.capacity >= 0 {
			config.ConnConfig.StatementCacheCapacity = test.capacity
		}
		if test.disableDescriptions {
			config.ConnConfig.DescriptionCacheCapacity = 0
		}
		if config.ConnConfig.DescriptionCacheCapacity == 0 {
			config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
		}
		if test.beforeConnect {
			config.BeforeConnect = func(_ context.Context, actual *pgx.ConnConfig) error {
				actual.StatementCacheCapacity = 0
				return nil
			}
		}
		if test.connectTracer {
			config.ConnConfig.Tracer = &imageScanPlanConnectTrace{imageScanPlanTrace: trace}
		}
	})
	return fixture, trace
}

func imageScanPlanScan(t *testing.T, fixture imageScanReadonlyFixture) {
	t.Helper()
	fixture.trace.authority.reset()
	fixture.trace.arm(fixture.itemID, nil)
	if err := fixture.state.scanImages(fixture.itemID, "Movie", "Film.mp4", false); err != nil || fixture.state.warnings != 0 {
		t.Fatalf("scan the unchanged image: warnings=%d error=%v", fixture.state.warnings, err)
	}
	imageScanReadonlyAssertNoPublication(t, fixture)
}

func imageScanPlanObserve(t *testing.T, fixture imageScanReadonlyFixture, statement string) scanReadPlanObservation {
	t.Helper()
	if err := fixture.store.lockOwnedSession(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	defer fixture.store.ownership.mu.Unlock()
	var plan scanReadPlanObservation
	// Observe only the real target without adding a cached diagnostic statement.
	err := fixture.store.ownership.conn.QueryRow(fixture.ctx, `SELECT pg_backend_pid(),count(*),
		COALESCE(max(name),''),COALESCE(max(array_to_string(parameter_types,',')),''),
		COALESCE(sum(generic_plans),0)::bigint,COALESCE(sum(custom_plans),0)::bigint,
		(SELECT count(*) FROM pg_prepared_statements)
		FROM pg_prepared_statements WHERE statement=$1::text`, pgx.QueryExecModeExec, statement).
		Scan(&plan.pid, &plan.count, &plan.name, &plan.parameters, &plan.generic, &plan.custom, &plan.total)
	if err != nil {
		t.Fatalf("observe the actual image comparison plan: %v", err)
	}
	return plan
}

func TestScanImagePlanConfigurationAndReuse(t *testing.T) {
	for _, test := range []scanReadPlanConfiguration{
		{name: "disabled", capacity: 0},
		{name: "one_entry", capacity: 1},
		{name: "default", capacity: -1},
		{name: "disabled_statements_and_descriptions", capacity: 0, disableDescriptions: true},
		{name: "before_connect_disables_actual_cache", capacity: 1, beforeConnect: true},
		{name: "connect_tracer_disables_actual_cache", capacity: 1, connectTracer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, trace := imageScanPlanPrepare(t, test)
			wantNamed := fixture.store.ownership.conn.Conn().Config().StatementCacheCapacity > 0
			versions := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)
			imageScanPlanScan(t, fixture)
			statement, args := trace.query(t, wantNamed)
			first := imageScanPlanObserve(t, fixture, statement)
			imageScanPlanScan(t, fixture)
			again, repeatedArgs := trace.query(t, wantNamed)
			second := imageScanPlanObserve(t, fixture, statement)
			if statement != again || !reflect.DeepEqual(args, repeatedArgs) ||
				!reflect.DeepEqual(versions, scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)) {
				t.Fatal("plan reuse changed the image query, parameters or stored tuples")
			}
			if wantNamed {
				const parameterTypes = "text,text,text,text[],text[],integer[],text[],text[],text[],bigint[],timestamp with time zone[],integer[],integer[],text[],text,text,text"
				if first.count != 1 || second.count != 1 || first.name == "" || first.name != second.name || first.pid != second.pid ||
					second.parameters != parameterTypes || second.generic+second.custom != first.generic+first.custom+1 {
					t.Fatalf("image comparison did not reuse its typed owner plan: first=%+v second=%+v", first, second)
				}
			} else if first.count != 0 || second.count != 0 {
				t.Fatalf("image comparison retained a named plan with the actual cache disabled: first=%+v second=%+v", first, second)
			}
			if test.name == "one_entry" {
				primary, err := os.Stat(filepath.Join(fixture.state.root.path, "Film.mp4"))
				if err != nil {
					t.Fatal(err)
				}
				if empty, err := fixture.state.cachedSubtitleScanEmpty(fixture.itemID, "Film.mp4", primary); err != nil || !empty {
					t.Fatalf("read media facts through the shared owner cache: empty=%t error=%v", empty, err)
				}
				if evicted := imageScanPlanObserve(t, fixture, statement); evicted.count != 0 || evicted.total != 1 {
					t.Fatalf("media facts did not evict the one-entry image plan: %+v", evicted)
				}
				imageScanPlanScan(t, fixture)
				if rebuilt := imageScanPlanObserve(t, fixture, statement); rebuilt.count != 1 || rebuilt.total != 1 || rebuilt.generic+rebuilt.custom != 1 {
					t.Fatalf("image comparison did not rebuild its bounded owner plan: %+v", rebuilt)
				}
			}
		})
	}
}

func TestScanImagePlanFreshRowsAndParameters(t *testing.T) {
	fixture, trace := imageScanPlanPrepare(t, scanReadPlanConfiguration{name: "enabled", capacity: 8})
	imageScanPlanScan(t, fixture)
	statement, args := trace.query(t, true)
	replacement := imageScanReplacement{
		types: args[4].([]string), indexes: args[5].([]int), paths: args[6].([]string),
		identities: args[7].([]string), hashes: args[8].([]string), sizes: args[9].([]int64),
		modified: args[10].([]time.Time), widths: args[11].([]int), heights: args[12].([]int), mimes: args[13].([]string),
	}
	replaceTypes := args[3].([]string)
	expected := fixture.state.task.authority.Load().roots[fixture.state.root.id]
	read := func(itemID string, types []string, candidate imageScanReplacement, want bool, wantErr error) {
		t.Helper()
		unchanged, err := fixture.state.imageCatalogReplacementUnchanged(itemID, types, candidate, expected)
		if unchanged != want || !errors.Is(err, wantErr) {
			t.Fatalf("prepared image comparison = %t, %v; want %t, %v", unchanged, err, want, wantErr)
		}
	}
	for _, policy := range []string{"force_custom_plan", "force_generic_plan"} {
		if err := fixture.store.lockOwnedSession(fixture.ctx); err != nil {
			t.Fatal(err)
		}
		var current string
		err := fixture.store.ownership.conn.QueryRow(fixture.ctx, "SELECT set_config('plan_cache_mode',$1,false)", pgx.QueryExecModeExec, policy).Scan(&current)
		fixture.store.ownership.mu.Unlock()
		if err != nil || current != policy {
			t.Fatalf("select the owner's plan policy: %v", err)
		}
		before := imageScanPlanObserve(t, fixture, statement)
		read(fixture.itemID, replaceTypes, replacement, true, nil)
		quoted := replacement
		quoted.paths = append([]string(nil), replacement.paths...)
		quoted.paths[0] = "Quoted ' poster.png"
		read(fixture.itemID, replaceTypes, quoted, false, nil)
		read(fixture.itemID, replaceTypes, imageScanReplacement{}, false, nil)
		read(fixture.itemID, []string{"Backdrop"}, imageScanReplacement{}, true, nil)
		read("missing-image-plan-item", replaceTypes, replacement, false, ErrNotFound)
		after := imageScanPlanObserve(t, fixture, statement)
		generic, custom := after.generic-before.generic, after.custom-before.custom
		if after.name != before.name || policy == "force_custom_plan" && (custom != 5 || generic != 0) ||
			policy == "force_generic_plan" && (generic != 5 || custom != 0) {
			t.Fatalf("image parameters did not use the selected prepared plan: policy=%s before=%+v after=%+v", policy, before, after)
		}
	}
	// Commit a private source-field change on another session. The same prepared
	// statement must observe it immediately and must not repair it while reading.
	beforeFreshRows := imageScanPlanObserve(t, fixture, statement)
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE item_images SET file_identity='changed-image-plan-identity' WHERE item_id=$1", fixture.itemID); err != nil {
		t.Fatal(err)
	}
	changed := imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary")
	versions := scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)
	read(fixture.itemID, replaceTypes, replacement, false, nil)
	if changed != imageScanNoopRow(t, fixture.ctx, fixture.pool, fixture.itemID, "Primary") ||
		!reflect.DeepEqual(versions, scanImageDiffVersions(t, fixture.ctx, fixture.pool, fixture.itemID)) {
		t.Fatal("the prepared image observation rewrote its current source facts")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE item_images SET file_identity=$2 WHERE item_id=$1", fixture.itemID, replacement.identities[0]); err != nil {
		t.Fatal(err)
	}
	read(fixture.itemID, replaceTypes, replacement, true, nil)
	if after := imageScanPlanObserve(t, fixture, statement); after.name != beforeFreshRows.name || after.generic != beforeFreshRows.generic+2 {
		t.Fatalf("fresh image rows did not use the same prepared plan: before=%+v after=%+v", beforeFreshRows, after)
	}
	assertNoCatalogTestNotification(t, fixture.notifications)
}
