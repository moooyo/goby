//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

type scanLookupPlanTrace struct {
	*scanReadPlanTrace
	mu      sync.Mutex
	queries []scanClaimLookupQuery
}

func (trace *scanLookupPlanTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.scanReadPlanTrace.TraceQueryStart(ctx, conn, data)
	if strings.HasPrefix(data.SQL, "SELECT "+storedFileColumns) {
		args := append([]any(nil), data.Args...)
		for index, argument := range args {
			if values, ok := argument.([]string); ok && values != nil {
				copied := make([]string, len(values))
				copy(copied, values)
				args[index] = copied
			}
		}
		trace.mu.Lock()
		trace.queries = append(trace.queries, scanClaimLookupQuery{SQL: data.SQL, Args: args})
		trace.mu.Unlock()
	}
	return ctx
}

func (trace *scanLookupPlanTrace) take() []scanClaimLookupQuery {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	queries := trace.queries
	trace.queries = nil
	return queries
}

type scanLookupPlanConnectTrace struct{ *scanLookupPlanTrace }

func (*scanLookupPlanConnectTrace) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	data.ConnConfig.StatementCacheCapacity = 0
	return ctx
}

func (*scanLookupPlanConnectTrace) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

type scanLookupPlanFixture struct {
	*scanReadPlanFixture
	lookup *scanLookupPlanTrace
}

// Only this small configuration adapter is new: the existing real-Store scan
// fixture supplies ownership, root state, bitmap calls and session cleanup.
func scanLookupPlanOpen(t *testing.T, base mediaSourceFixture, test scanReadPlanConfiguration) *scanLookupPlanFixture {
	t.Helper()
	trace := &scanLookupPlanTrace{scanReadPlanTrace: &scanReadPlanTrace{}}
	config := base.pool.Config()
	config.MaxConns, config.MinConns = 2, 0
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	config.ConnConfig.Tracer = trace
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
		config.ConnConfig.Tracer = &scanLookupPlanConnectTrace{scanLookupPlanTrace: trace}
	}
	pool, err := pgxpool.NewWithConfig(base.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	fixture := &scanLookupPlanFixture{lookup: trace, scanReadPlanFixture: &scanReadPlanFixture{
		base: base, pool: pool, trace: trace.scanReadPlanTrace,
		wantNamed: config.ConnConfig.StatementCacheCapacity > 0 && !test.beforeConnect && !test.connectTracer,
	}}
	fixture.openStore(t)
	if err := fixture.store.prepareScanOperationAuthority(base.ctx, fixture.state.task, nil); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *scanLookupPlanFixture) exact(t *testing.T, state *scanState, relative string, info os.FileInfo, role scannedMediaRole, wantID string) (storedFile, scanClaimLookupQuery) {
	t.Helper()
	fixture.lookup.take()
	stored, err := state.findStoredFileForRole(relative, info, role)
	queries := fixture.lookup.take()
	if err != nil || stored.id != wantID || len(queries) != 1 {
		t.Fatalf("exact lookup changed identity or query scope: id=%q want=%q queries=%d error=%v", stored.id, wantID, len(queries), err)
	}
	wantArgs := []any{state.root.id, relative}
	if role == scannedRoleOrdinary && fixture.wantNamed {
		wantArgs = append([]any{pgx.QueryExecModeCacheStatement}, wantArgs...)
	}
	if !reflect.DeepEqual(queries[0].Args, wantArgs) {
		t.Fatalf("exact lookup changed mode or scalar root/path parameters: got=%#v want=%#v", queries[0].Args, wantArgs)
	}
	if role == scannedRoleOrdinary && fmt.Sprintf("%x", sha256.Sum256([]byte(queries[0].SQL))) != "9438c5685901e1a0b10ae7e3127366a7666c40f214e20652745a370ce0a34c1f" {
		t.Fatal("ordinary lookup changed its SQL text")
	}
	return stored, queries[0]
}

func (fixture *scanLookupPlanFixture) lookupPlan(t *testing.T, statement string) scanReadPlanObservation {
	t.Helper()
	var plan scanReadPlanObservation
	fixture.connection(t, scanReadPlanBitmap, func(conn *pgx.Conn) {
		// Observe only this target; lookup and bitmap can both be resident.
		if err := conn.QueryRow(fixture.base.ctx, `SELECT pg_backend_pid(),count(*),COALESCE(max(name),''),
			COALESCE(max(parameter_types::text),''),COALESCE(sum(generic_plans),0)::bigint,COALESCE(sum(custom_plans),0)::bigint
			FROM pg_prepared_statements WHERE statement=$1::text`, pgx.QueryExecModeExec, statement).
			Scan(&plan.pid, &plan.count, &plan.name, &plan.parameters, &plan.generic, &plan.custom); err != nil {
			t.Fatal(err)
		}
	})
	return plan
}

func TestScanLookupPlanConfigurationAndFreshness(t *testing.T) {
	base := mediaSourceTestCatalog(t, nil)
	quoted := libraryIntegrationFile(t, base.allowedRoot, "movies/Nested/Quoted ' Film.mkv", "video:quoted-lookup")
	otherPath := libraryIntegrationFile(t, base.allowedRoot, "other/Nested/Feature.mkv", "video:other-root-lookup")
	otherLibrary := libraryIntegrationCreate(t, base.ctx, base.store, "Other lookup root", "movies", filepath.Join(base.allowedRoot, "other"))
	libraryIntegrationScan(t, base.ctx, base.store, base.library.ID, "Completed")
	libraryIntegrationScan(t, base.ctx, base.store, otherLibrary.ID, "Completed")
	var quotedID, otherID string
	if err := base.pool.QueryRow(base.ctx, "SELECT id FROM items WHERE library_id=$1 AND path=$2", pgx.QueryExecModeExec, base.library.ID, quoted).Scan(&quotedID); err != nil {
		t.Fatal(err)
	}
	if err := base.pool.QueryRow(base.ctx, "SELECT id FROM items WHERE library_id=$1 AND path=$2", pgx.QueryExecModeExec, otherLibrary.ID, otherPath).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if err := base.store.Close(base.ctx); err != nil {
		t.Fatal(err)
	}
	for _, test := range []scanReadPlanConfiguration{
		{name: "disabled", capacity: 0},
		{name: "one_entry", capacity: 1},
		{name: "default", capacity: -1},
		{name: "disabled_statements_and_descriptions", capacity: 0, disableDescriptions: true},
		{name: "before_connect_disables_actual_cache", capacity: 1, beforeConnect: true},
		{name: "connect_tracer_disables_actual_cache", capacity: 1, connectTracer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := scanLookupPlanOpen(t, base, test)
			before, query := fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, base.item.ID)
			first := fixture.lookupPlan(t, query.SQL)
			again, _ := fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, base.item.ID)
			second := fixture.lookupPlan(t, query.SQL)
			if !reflect.DeepEqual(before, again) {
				t.Fatal("prepared lookup changed unchanged decoded rows")
			}
			if fixture.wantNamed {
				if first.count != 1 || second.count != 1 || first.name != second.name || first.pid != second.pid || second.parameters != "{text,text}" || second.generic+second.custom != first.generic+first.custom+1 {
					t.Fatalf("actual lookup did not reuse its typed named plan: first=%+v second=%+v", first, second)
				}
			} else if first.count != 0 || second.count != 0 {
				t.Fatalf("disabled or customized connections retained a lookup plan: first=%+v second=%+v", first, second)
			}
			fixture.exact(t, fixture.state, "Nested/Quoted ' Film.mkv", scanClaimLookupInfo(t, quoted), scannedRoleOrdinary, quotedID)
			other := scanClaimLookupState(t, base.ctx, fixture.store, otherLibrary, filepath.Join(base.allowedRoot, "other"))
			fixture.exact(t, other, "Nested/Feature.mkv", scanClaimLookupInfo(t, otherPath), scannedRoleOrdinary, otherID)
			if test.name == "default" {
				scanLookupPlanFreshRows(t, fixture, before)
			}
			if test.name == "one_entry" {
				fixture.bitmap(t, base.item.ID)
				if evicted := fixture.lookupPlan(t, query.SQL); evicted.count != 0 {
					t.Fatalf("bitmap read did not evict the one-entry lookup plan: %+v", evicted)
				}
				fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, base.item.ID)
				rebuilt := fixture.lookupPlan(t, query.SQL)
				if bitmap := fixture.plan(t, scanReadPlanBitmap); bitmap.count != 0 || rebuilt.count != 1 || rebuilt.generic+rebuilt.custom != 1 {
					t.Fatalf("lookup did not rebuild while evicting bitmap: lookup=%+v bitmap=%+v", rebuilt, bitmap)
				}
				fixture.pool.Reset()
				fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, base.item.ID)
				replaced := fixture.lookupPlan(t, query.SQL)
				if replaced.pid == rebuilt.pid || replaced.count != 1 || replaced.generic+replaced.custom != 1 {
					t.Fatalf("replacement pool connection lost lookup preparation: before=%+v after=%+v", rebuilt, replaced)
				}
			}
		})
	}
}

func scanLookupPlanFreshRows(t *testing.T, fixture *scanLookupPlanFixture, before storedFile) {
	t.Helper()
	ctx, pool, itemID := fixture.base.ctx, fixture.base.pool, fixture.base.item.ID
	encoded, err := json.Marshal(before.media)
	if err != nil {
		t.Fatal(err)
	}
	var local any
	if len(before.local.raw) != 0 {
		local = string(before.local.raw)
	}
	var automatic []byte
	var explicit bool
	if err := pool.QueryRow(ctx, "SELECT automatic,automatic_sort_name_explicit FROM item_metadata_state WHERE item_id=$1", pgx.QueryExecModeExec, itemID).Scan(&automatic, &explicit); err != nil {
		t.Fatal(err)
	}
	const imageRelative = "Nested/Feature-poster.png"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, `UPDATE items SET name=$2,overview=$3,modified_at=$4,media=$5,local_metadata=$8,
			local_metadata_hash=$6,local_metadata_path=$7 WHERE id=$1`, pgx.QueryExecModeExec, itemID, before.name, before.overview, before.modified, string(encoded), before.local.hash, before.local.path, local); err != nil {
			t.Errorf("restore lookup row: %v", err)
		}
		if _, err := pool.Exec(cleanup, "UPDATE item_metadata_state SET automatic=$2,automatic_sort_name_explicit=$3 WHERE item_id=$1", pgx.QueryExecModeExec, itemID, string(automatic), explicit); err != nil {
			t.Errorf("restore automatic lookup metadata: %v", err)
		}
		if _, err := pool.Exec(cleanup, "DELETE FROM item_images WHERE item_id=$1", pgx.QueryExecModeExec, itemID); err != nil {
			t.Errorf("restore lookup image absence: %v", err)
		}
		if err := os.Remove(filepath.Join(fixture.state.root.path, imageRelative)); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove the lookup image fixture: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `UPDATE items SET name='Changed lookup',overview='Current overview',modified_at=NULL,media=NULL,
		local_metadata='{"Name":"Changed local","IndexNumber":0}',local_metadata_hash=repeat('a',64),local_metadata_path='Nested/Feature.nfo' WHERE id=$1`, pgx.QueryExecModeExec, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state SET automatic=automatic || '{"Name":"Changed automatic","SortName":"Explicit current sort"}',
		automatic_sort_name_explicit=true WHERE item_id=$1`, pgx.QueryExecModeExec, itemID); err != nil {
		t.Fatal(err)
	}
	imageStoreTestInsert(t, ctx, pool, fixture.state.root, itemID, "Primary", 0, imageRelative, imageStoreTestPNG(t))
	current, _ := fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, itemID)
	if current.name != "Changed lookup" || current.overview != "Current overview" || current.modified != nil || current.media != nil || !current.hasLocalImages ||
		current.local.value == nil || current.local.value.Name != "Changed local" || current.local.value.IndexNumber == nil || *current.local.value.IndexNumber != 0 ||
		current.local.hash != strings.Repeat("a", 64) || current.local.path != "Nested/Feature.nfo" || current.automatic == nil || current.automatic.Name != "Changed automatic" ||
		current.scanSortName == nil || *current.scanSortName != "Explicit current sort" {
		t.Fatal("prepared lookup retained stale metadata, nullable facts or image presence")
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET local_metadata=NULL WHERE id=$1", pgx.QueryExecModeExec, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM item_images WHERE item_id=$1", pgx.QueryExecModeExec, itemID); err != nil {
		t.Fatal(err)
	}
	current, _ = fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, itemID)
	if current.local.value != nil || current.hasLocalImages {
		t.Fatal("prepared lookup retained cleared local metadata or deleted image presence")
	}
}

func TestScanLookupPlanPreservesRoleAndRenameCalls(t *testing.T) {
	base := mediaSourceTestCatalog(t, nil)
	if err := base.store.Close(base.ctx); err != nil {
		t.Fatal(err)
	}
	fixture := scanLookupPlanOpen(t, base, scanReadPlanConfiguration{name: "enabled", capacity: 8})
	_, ordinary := fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, scannedRoleOrdinary, base.item.ID)
	if plan := fixture.lookupPlan(t, ordinary.SQL); plan.count != 1 {
		t.Fatal("role fixture did not enable actual ordinary lookup plan reuse")
	}
	for _, role := range []scannedMediaRole{scannedRoleTheme, scannedRoleExtra} {
		_, query := fixture.exact(t, fixture.state, "Nested/Feature.mkv", fixture.primary, role, base.item.ID)
		if plan := fixture.lookupPlan(t, query.SQL); plan.count != 0 {
			t.Fatalf("non-ordinary role opted into a named plan: role=%s plan=%+v", role, plan)
		}
	}
	const relative = "Nested/Renamed.mkv"
	renamed := filepath.Join(fixture.state.root.path, filepath.FromSlash(relative))
	if err := os.Rename(base.path, renamed); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(renamed, base.path) })
	info := scanClaimLookupInfo(t, renamed)
	fixture.state.themes = &themeScan{claimed: map[string]string{"unrelated-claim": "ordinary:another-root:Other.mkv"}}
	fixture.lookup.take()
	stored, err := fixture.state.findStoredFileForRole(relative, info, scannedRoleOrdinary)
	queries := fixture.lookup.take()
	if err != nil || stored.id != base.item.ID || len(queries) != 2 {
		t.Fatalf("enabled ordinary lookup lost its inode fallback: id=%q queries=%d error=%v", stored.id, len(queries), err)
	}
	if !reflect.DeepEqual(queries[0].Args, []any{pgx.QueryExecModeCacheStatement, fixture.state.root.id, relative}) ||
		!reflect.DeepEqual(queries[1].Args, []any{base.library.ID, fileIdentity(info), info.Size(), catalogModifiedTime(info), []string{"unrelated-claim"}}) ||
		!strings.Contains(queries[1].SQL, "NOT (id=ANY($5::text[]))") || !strings.HasSuffix(queries[1].SQL, "ORDER BY created_at, id LIMIT 8") {
		t.Fatal("inode fallback changed its unmodified five arguments, claim exclusion or candidate limit")
	}
	if plan := fixture.lookupPlan(t, queries[1].SQL); plan.count != 0 {
		t.Fatal("inode fallback unexpectedly opted into a named plan")
	}
	fixture.state.themes.claimed[base.item.ID] = "ordinary:another-root:Claimed.mkv"
	if stored, err := fixture.state.findStoredFileForRole(relative, info, scannedRoleOrdinary); err != nil || stored.id != "" {
		t.Fatalf("enabled lookup reused an identity claimed by another visit: id=%q error=%v", stored.id, err)
	}
}
