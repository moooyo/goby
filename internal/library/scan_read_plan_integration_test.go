//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	scanReadPlanMedia = iota
	scanReadPlanBitmap
)

// Capture the statements executed by the real call sites, without copying
// their SQL or making every ordinary recorder a connection-configuration hook.
type scanReadPlanTrace struct {
	mu         sync.Mutex
	statements [2]string
}

func (trace *scanReadPlanTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	kind := -1
	if strings.HasPrefix(data.SQL, "SELECT i.file_identity, i.file_size, i.modified_at, i.media,") && strings.Contains(data.SQL, "OR EXISTS(SELECT 1 FROM item_bitmap_subtitles") {
		kind = scanReadPlanMedia
	} else if strings.HasPrefix(data.SQL, "SELECT EXISTS(SELECT 1 FROM item_bitmap_subtitles WHERE") {
		kind = scanReadPlanBitmap
	}
	if kind >= 0 {
		trace.mu.Lock()
		trace.statements[kind] = data.SQL
		trace.mu.Unlock()
	}
	return ctx
}

func (*scanReadPlanTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *scanReadPlanTrace) statement(t *testing.T, kind int) string {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.statements[kind] == "" {
		t.Fatal("the actual scan read was not captured")
	}
	return trace.statements[kind]
}

type scanReadPlanConnectTrace struct{ *scanReadPlanTrace }

func (*scanReadPlanConnectTrace) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	data.ConnConfig.StatementCacheCapacity = 0
	return ctx
}

func (*scanReadPlanConnectTrace) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

type scanReadPlanConfiguration struct {
	name                string
	capacity            int
	disableDescriptions bool
	beforeConnect       bool
	connectTracer       bool
}

type scanReadPlanFixture struct {
	base      mediaSourceFixture
	pool      *pgxpool.Pool
	store     *Store
	state     *scanState
	trace     *scanReadPlanTrace
	primary   os.FileInfo
	wantNamed bool
}

func scanReadPlanOpen(t *testing.T, base mediaSourceFixture, test scanReadPlanConfiguration) *scanReadPlanFixture {
	t.Helper()
	trace := &scanReadPlanTrace{}
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
		config.ConnConfig.Tracer = &scanReadPlanConnectTrace{scanReadPlanTrace: trace}
	}
	pool, err := pgxpool.NewWithConfig(base.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	fixture := &scanReadPlanFixture{base: base, pool: pool, trace: trace,
		wantNamed: config.ConnConfig.StatementCacheCapacity > 0 && !test.beforeConnect && !test.connectTracer}
	fixture.openStore(t)
	return fixture
}

func (fixture *scanReadPlanFixture) openStore(t *testing.T) {
	t.Helper()
	store, err := New(fixture.pool, fixture.base.store.prober, []string{fixture.base.allowedRoot})
	if err != nil {
		t.Fatal(err)
	}
	entitiesStoreCleanup(t, store)
	fixture.store = store
	fixture.state = imageScanTestState(t, fixture.base.ctx, fixture.base.pool, store, fixture.base.library, "Nested")
	entries, err := os.ReadDir(filepath.Dir(fixture.base.path))
	if err != nil {
		t.Fatal(err)
	}
	fixture.state.subtitleDirectories = map[string]*subtitleDirectoryIndex{
		"Nested": newSubtitleDirectoryIndex(entries, fixture.base.library.CollectionType, fixture.state.directoryIdentities["Nested"]),
	}
	fixture.primary, err = os.Stat(fixture.base.path)
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *scanReadPlanFixture) media(t *testing.T, itemID, relative string, want bool, wantErr error) {
	t.Helper()
	empty, err := fixture.state.cachedSubtitleScanEmpty(itemID, relative, fixture.primary)
	if empty != want || !errors.Is(err, wantErr) {
		t.Fatalf("cached media-facts read = %t, %v; want %t, %v", empty, err, want, wantErr)
	}
}

func (fixture *scanReadPlanFixture) bitmap(t *testing.T, itemID string) {
	t.Helper()
	if err := fixture.state.scanBitmapSubtitlesAttempt(itemID, "Nested/Feature.mkv", fixture.base.item.Media); err != nil {
		t.Fatalf("cached bitmap-presence read: %v", err)
	}
	if fixture.state.warnings != 0 {
		t.Fatalf("cached bitmap-presence read produced %d warnings", fixture.state.warnings)
	}
}

func (fixture *scanReadPlanFixture) read(t *testing.T, kind int) {
	t.Helper()
	if kind == scanReadPlanMedia {
		fixture.media(t, fixture.base.item.ID, "Nested/Feature.mkv", true, nil)
	} else {
		fixture.bitmap(t, fixture.base.item.ID)
	}
}

// With two pool slots, the reserved owner and sole ordinary connection are
// distinct. Never issue diagnostic SQL concurrently on the owner's session.
func (fixture *scanReadPlanFixture) connection(t *testing.T, kind int, use func(*pgx.Conn)) {
	t.Helper()
	if kind == scanReadPlanMedia {
		if err := fixture.store.lockOwnedSession(fixture.base.ctx); err != nil {
			t.Fatal(err)
		}
		defer fixture.store.ownership.mu.Unlock()
		use(fixture.store.ownership.conn.Conn())
		return
	}
	conn, err := fixture.pool.Acquire(fixture.base.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	use(conn.Conn())
}

type scanReadPlanObservation struct {
	pid              uint32
	count, total     int64
	name, parameters string
	generic, custom  int64
}

func (fixture *scanReadPlanFixture) plan(t *testing.T, kind int) scanReadPlanObservation {
	t.Helper()
	statement := fixture.trace.statement(t, kind)
	var plan scanReadPlanObservation
	fixture.connection(t, kind, func(conn *pgx.Conn) {
		// Observer statements must not occupy or evict a named cache entry.
		err := conn.QueryRow(fixture.base.ctx, `SELECT pg_backend_pid(),count(*),
			COALESCE(max(name),''),COALESCE(max(parameter_types::text),''),
			COALESCE(sum(generic_plans),0)::bigint,COALESCE(sum(custom_plans),0)::bigint,
			(SELECT count(*) FROM pg_prepared_statements)
			FROM pg_prepared_statements WHERE statement=$1::text`, pgx.QueryExecModeExec, statement).
			Scan(&plan.pid, &plan.count, &plan.name, &plan.parameters, &plan.generic, &plan.custom, &plan.total)
		if err != nil {
			t.Fatalf("observe the scan read's actual prepared plan: %v", err)
		}
	})
	return plan
}

func (fixture *scanReadPlanFixture) logPlanMemory(t *testing.T, kind int) {
	t.Helper()
	fixture.connection(t, kind, func(conn *pgx.Conn) {
		var allocated int64
		if err := conn.QueryRow(fixture.base.ctx, `SELECT COALESCE(sum(total_bytes),0)::bigint
			FROM pg_backend_memory_contexts WHERE name LIKE 'CachedPlan%'`, pgx.QueryExecModeExec).Scan(&allocated); err != nil {
			t.Log("cached-plan memory observation is unavailable")
			return
		}
		t.Logf("scan-read kind=%d backend=%d cached-plan-context allocated bytes=%d; backend aggregate, not per-query memory", kind, conn.PgConn().PID(), allocated)
	})
}

func TestScanReadPlansRespectCacheConfigurationAndSessions(t *testing.T) {
	// Reuse one schema, indexed source and inactive bitmap history across all
	// configurations. Only each small Store/pool/session population is replaced.
	base := mediaSourceTestCatalog(t, nil)
	sup, idx, sub := writeBitmapCatalogFiles(t, base)
	libraryIntegrationScan(t, base.ctx, base.store, base.library.ID, "Completed")
	for _, path := range []string{sup, idx, sub} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	libraryIntegrationScan(t, base.ctx, base.store, base.library.ID, "Completed")
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
			fixture := scanReadPlanOpen(t, base, test)
			for _, kind := range []int{scanReadPlanMedia, scanReadPlanBitmap} {
				fixture.read(t, kind)
				first := fixture.plan(t, kind)
				fixture.read(t, kind)
				second := fixture.plan(t, kind)
				if fixture.wantNamed {
					parameters := "{text}"
					if kind == scanReadPlanMedia {
						parameters = "{text,text,text,text}"
					}
					if first.count != 1 || second.count != 1 || first.total != 1 || second.total != 1 ||
						first.name == "" || first.name != second.name || first.pid != second.pid || second.parameters != parameters ||
						second.generic+second.custom != first.generic+first.custom+1 {
						t.Fatalf("scan read did not reuse one typed named plan: first=%+v second=%+v", first, second)
					}
				} else if first.count != 0 || second.count != 0 || first.total != 0 || second.total != 0 {
					t.Fatalf("disabled or connection-customized cache retained named plans: first=%+v second=%+v", first, second)
				}
				fixture.logPlanMemory(t, kind)
			}
			if test.name == "one_entry" {
				scanReadPlanEvictionAndReplacement(t, fixture)
			}
			if test.name == "default" && fixture.wantNamed {
				scanReadPlanParameterFreshness(t, fixture)
			}
		})
	}
}

func scanReadPlanEvictionAndReplacement(t *testing.T, fixture *scanReadPlanFixture) {
	t.Helper()
	for _, kind := range []int{scanReadPlanMedia, scanReadPlanBitmap} {
		fixture.connection(t, kind, func(conn *pgx.Conn) {
			var value int32
			if err := conn.QueryRow(fixture.base.ctx, "SELECT $1::integer /* scan read plan eviction */", pgx.QueryExecModeCacheStatement, int32(7)).Scan(&value); err != nil || value != 7 {
				t.Fatalf("evict the one-entry statement cache: value=%d error=%v", value, err)
			}
		})
		evicted := fixture.plan(t, kind)
		if evicted.count != 0 || evicted.total != 1 {
			t.Fatalf("one-entry cache did not release the old named plan: %+v", evicted)
		}
		fixture.read(t, kind)
		rebuilt := fixture.plan(t, kind)
		if rebuilt.count != 1 || rebuilt.total != 1 || rebuilt.generic+rebuilt.custom != 1 {
			t.Fatalf("evicted scan read did not rebuild its bounded plan: %+v", rebuilt)
		}
	}
	// A shape-preserving private-schema change invalidates the server plans.
	// The next actual read must replan without changing nullable/JSON decoding.
	for _, table := range []string{"items", "item_bitmap_subtitles"} {
		if _, err := fixture.base.pool.Exec(fixture.base.ctx, "ALTER TABLE "+pgx.Identifier{table}.Sanitize()+" ADD COLUMN scan_plan_fixture_note integer", pgx.QueryExecModeExec); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []int{scanReadPlanMedia, scanReadPlanBitmap} {
		before := fixture.plan(t, kind)
		fixture.read(t, kind)
		after := fixture.plan(t, kind)
		if after.name != before.name || after.generic+after.custom != before.generic+before.custom+1 {
			t.Fatalf("schema invalidation lost the scan read's prepared plan: before=%+v after=%+v", before, after)
		}
	}
	owner, pooled := fixture.plan(t, scanReadPlanMedia), fixture.plan(t, scanReadPlanBitmap)
	fixture.pool.Reset()
	fixture.read(t, scanReadPlanBitmap)
	replacement := fixture.plan(t, scanReadPlanBitmap)
	if replacement.pid == pooled.pid || replacement.count != 1 || replacement.generic+replacement.custom != 1 {
		t.Fatalf("replacement pool connection inherited or omitted the previous session's plan: before=%+v after=%+v", pooled, replacement)
	}
	fixture.read(t, scanReadPlanMedia)
	if unchanged := fixture.plan(t, scanReadPlanMedia); unchanged.pid != owner.pid {
		t.Fatal("resetting idle pool connections replaced the live catalog owner")
	}
	// Retire the real owner before replacement; no test teaches an old Store
	// to reacquire ownership or continue after its reserved connection is lost.
	imageScanTestRetireTask(t, fixture.base.pool, fixture.store, fixture.state.task)
	if err := fixture.store.Close(fixture.base.ctx); err != nil {
		t.Fatal(err)
	}
	fixture.pool.Reset()
	fixture.openStore(t)
	fixture.read(t, scanReadPlanMedia)
	newOwner := fixture.plan(t, scanReadPlanMedia)
	if newOwner.pid == owner.pid || newOwner.count != 1 || newOwner.generic+newOwner.custom != 1 {
		t.Fatalf("new Store inherited or omitted its owner's plan: before=%+v after=%+v", owner, newOwner)
	}
}

func scanReadPlanParameterFreshness(t *testing.T, fixture *scanReadPlanFixture) {
	t.Helper()
	ctx, itemID := fixture.base.ctx, fixture.base.item.ID
	for _, policy := range []string{"force_custom_plan", "force_generic_plan"} {
		for _, kind := range []int{scanReadPlanMedia, scanReadPlanBitmap} {
			fixture.connection(t, kind, func(conn *pgx.Conn) {
				var current string
				if err := conn.QueryRow(ctx, "SELECT set_config('plan_cache_mode',$1,false)", pgx.QueryExecModeExec, policy).Scan(&current); err != nil || current != policy {
					t.Fatalf("select the private session's plan policy: %v", err)
				}
			})
			before := fixture.plan(t, kind)
			fixture.read(t, kind)
			if kind == scanReadPlanMedia {
				fixture.media(t, "missing-plan-item", "Nested/Feature.mkv", false, ErrNotFound)
				fixture.media(t, itemID, "Nested/quoted'path.mkv", false, ErrNotFound)
			} else {
				fixture.bitmap(t, "missing-plan-item")
				fixture.bitmap(t, itemID)
			}
			after := fixture.plan(t, kind)
			generic, custom := after.generic-before.generic, after.custom-before.custom
			if after.name != before.name || policy == "force_custom_plan" && (custom != 3 || generic != 0) ||
				policy == "force_generic_plan" && (generic != 3 || custom != 0) {
				t.Fatalf("cached parameters did not exercise the requested plan policy: policy=%s before=%+v after=%+v", policy, before, after)
			}
		}
	}
	// The named plan must fetch current nullable facts and active-state changes,
	// including a cached false EXISTS becoming true before actual retirement.
	t.Cleanup(func() {
		if _, err := fixture.base.pool.Exec(ctx, "UPDATE items SET modified_at=$2 WHERE id=$1", itemID, catalogModifiedTime(fixture.primary)); err != nil {
			t.Errorf("restore the indexed modification time: %v", err)
		}
		if _, err := fixture.base.pool.Exec(ctx, "UPDATE item_bitmap_subtitles SET active=false WHERE item_id=$1", itemID); err != nil {
			t.Errorf("restore inactive bitmap history: %v", err)
		}
	})
	if _, err := fixture.base.pool.Exec(ctx, "UPDATE items SET modified_at=NULL WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	fixture.media(t, itemID, "Nested/Feature.mkv", false, nil)
	if _, err := fixture.base.pool.Exec(ctx, "UPDATE items SET modified_at=$2 WHERE id=$1", itemID, catalogModifiedTime(fixture.primary)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.base.pool.Exec(ctx, "UPDATE item_bitmap_subtitles SET active=true WHERE item_id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	fixture.media(t, itemID, "Nested/Feature.mkv", false, nil)
	fixture.bitmap(t, "missing-plan-item")
	fixture.bitmap(t, itemID)
	fixture.media(t, itemID, "Nested/Feature.mkv", true, nil)
	var active int
	if err := fixture.base.pool.QueryRow(ctx, "SELECT count(*) FROM item_bitmap_subtitles WHERE item_id=$1 AND active", itemID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("cached bitmap presence hid newly active rows from retirement: active=%d error=%v", active, err)
	}
}
