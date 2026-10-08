//go:build linux

package library

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type playbackSourcePlanTrace struct {
	mu                              sync.Mutex
	statement                       string
	lockArgs, sourceArgs            []any
	lockReads, sourceReads, batches int
}

func (trace *playbackSourcePlanTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if data.SQL == "SELECT id FROM items WHERE id=$1 FOR SHARE" {
		trace.lockReads++
		trace.lockArgs = append([]any(nil), data.Args...)
	} else if strings.HasPrefix(data.SQL, "SELECT i.id, i.library_id, i.type, i.path, i.media,") {
		trace.sourceReads++
		trace.statement, trace.sourceArgs = data.SQL, append([]any(nil), data.Args...)
	}
	return ctx
}

func (*playbackSourcePlanTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *playbackSourcePlanTrace) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	queries := data.Batch.QueuedQueries
	if len(queries) == 2 && queries[0].SQL == "SELECT id FROM items WHERE id=$1 FOR SHARE" {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		trace.batches++
		trace.lockArgs = append([]any(nil), queries[0].Arguments...)
		trace.statement, trace.sourceArgs = queries[1].SQL, append([]any(nil), queries[1].Arguments...)
	}
	return ctx
}

func (*playbackSourcePlanTrace) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {
}
func (*playbackSourcePlanTrace) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

type playbackSourcePlanConnectTrace struct{ *playbackSourcePlanTrace }

func (*playbackSourcePlanConnectTrace) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	data.ConnConfig.StatementCacheCapacity = 0
	return ctx
}

func (*playbackSourcePlanConnectTrace) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

type playbackSourcePlanObservation struct {
	pid                       uint32
	count, total              int64
	name, parameters          string
	genericPlans, customPlans int64
}

func playbackSourcePlanObserve(t *testing.T, ctx context.Context, conn *pgx.Conn, statement string) playbackSourcePlanObservation {
	t.Helper()
	var plan playbackSourcePlanObservation
	// This observer cannot occupy or evict an entry in the cache under test.
	err := conn.QueryRow(ctx, `SELECT pg_backend_pid(),count(*),COALESCE(max(name),''),
		COALESCE(max(parameter_types::text),''),COALESCE(sum(generic_plans),0)::bigint,
		COALESCE(sum(custom_plans),0)::bigint,(SELECT count(*) FROM pg_prepared_statements)
		FROM pg_prepared_statements WHERE statement=$1::text`, pgx.QueryExecModeExec, statement).
		Scan(&plan.pid, &plan.count, &plan.name, &plan.parameters, &plan.genericPlans, &plan.customPlans, &plan.total)
	if err != nil {
		t.Fatalf("observe the actual playback source plan: %v", err)
	}
	return plan
}

func playbackSourcePlanRead(ctx context.Context, conn *pgx.Conn, access libraryAccess, itemID, sourceID string) (indexedMediaSource, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer rollback(tx)
	return readIndexedPlaybackMediaBatch(ctx, tx, access, itemID, sourceID, false)
}

func TestPlaybackSourcePlanRespectsActualConnectionConfiguration(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	access := unrestrictedLibraryAccess()
	baseline, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := readIndexedPlaybackMedia(fixture.ctx, baseline, access, fixture.item.ID, media.SourceID(fixture.item.ID), false)
	closeErr := baseline.Rollback(fixture.ctx)
	if err != nil || closeErr != nil {
		t.Fatalf("read the existing source projection oracle: read=%v rollback=%v", err, closeErr)
	}
	expectedSQL := indexedPlaybackMediaSQL(access)
	for _, test := range []struct {
		name                         string
		mode                         pgx.QueryExecMode
		capacity                     int
		beforeConnect, connectTracer bool
	}{
		{name: "disabled", mode: pgx.QueryExecModeCacheDescribe, capacity: 0},
		{name: "one_entry", mode: pgx.QueryExecModeCacheDescribe, capacity: 1},
		{name: "default_capacity", mode: pgx.QueryExecModeCacheDescribe, capacity: 512},
		{name: "before_connect_disables_cache", mode: pgx.QueryExecModeCacheDescribe, capacity: 1, beforeConnect: true},
		{name: "connect_tracer_disables_cache", mode: pgx.QueryExecModeCacheDescribe, capacity: 1, connectTracer: true},
		{name: "describe_exec", mode: pgx.QueryExecModeDescribeExec, capacity: 1},
		{name: "exec", mode: pgx.QueryExecModeExec, capacity: 1},
		{name: "simple_protocol", mode: pgx.QueryExecModeSimpleProtocol, capacity: 1},
		{name: "existing_named_batch", mode: pgx.QueryExecModeCacheStatement, capacity: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := &playbackSourcePlanTrace{}
			configuration := fixture.pool.Config().Copy()
			configuration.MaxConns, configuration.MinConns = 1, 0
			configuration.ConnConfig.DefaultQueryExecMode = test.mode
			configuration.ConnConfig.StatementCacheCapacity = test.capacity
			configuration.ConnConfig.Tracer = trace
			if test.beforeConnect {
				configuration.BeforeConnect = func(_ context.Context, actual *pgx.ConnConfig) error {
					actual.StatementCacheCapacity = 0
					return nil
				}
			}
			if test.connectTracer {
				configuration.ConnConfig.Tracer = &playbackSourcePlanConnectTrace{trace}
			}
			pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, pool)
			connection, err := pool.Acquire(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Release()
			actual := connection.Conn().Config()
			wantSequential := test.mode == pgx.QueryExecModeCacheDescribe && test.capacity > 0 && !test.beforeConnect && !test.connectTracer
			var plans [2]playbackSourcePlanObservation
			for repeat := range 2 {
				snapshot, err := playbackSourcePlanRead(fixture.ctx, connection.Conn(), access, fixture.item.ID, media.SourceID(fixture.item.ID))
				if err != nil {
					t.Fatalf("read the configured playback source: %v", err)
				}
				trace.mu.Lock()
				statement := trace.statement
				trace.mu.Unlock()
				if !reflect.DeepEqual(snapshot, expected) || statement == "" || statement != expectedSQL {
					t.Fatal("query-mode selection changed the source SQL or complete decoded snapshot")
				}
				plans[repeat] = playbackSourcePlanObserve(t, fixture.ctx, connection.Conn(), statement)
			}
			trace.mu.Lock()
			lockReads, sourceReads, batches := trace.lockReads, trace.sourceReads, trace.batches
			lockArgs, sourceArgs := trace.lockArgs, trace.sourceArgs
			trace.mu.Unlock()
			wantSourceArgs := []any{fixture.item.ID, access.all, access.folders}
			if wantSequential {
				wantSourceArgs = append([]any{pgx.QueryExecModeCacheStatement}, wantSourceArgs...)
			}
			if !reflect.DeepEqual(lockArgs, []any{fixture.item.ID}) || !reflect.DeepEqual(sourceArgs, wantSourceArgs) ||
				wantSequential && (lockReads != 2 || sourceReads != 2 || batches != 0) ||
				!wantSequential && (lockReads != 0 || sourceReads != 0 || batches != 2) {
				t.Fatal("source plan selection changed parameters or bypassed the required fallback")
			}
			first, second := plans[0], plans[1]
			if wantSequential || test.mode == pgx.QueryExecModeCacheStatement {
				wantTotal := int64(1)
				if !wantSequential {
					wantTotal = 2
				}
				if first.count != 1 || second.count != 1 || first.total != wantTotal || second.total != wantTotal ||
					first.name == "" || first.name != second.name || first.pid != second.pid || second.parameters != "{text,boolean,text[]}" ||
					second.genericPlans+second.customPlans != first.genericPlans+first.customPlans+1 {
					t.Fatalf("source plan was not bounded and reused on its real session: first=%+v second=%+v", first, second)
				}
			} else if first.count != 0 || second.count != 0 || first.total != 0 || second.total != 0 {
				t.Fatal("a disabled or custom-mode connection retained an unexpected named plan")
			}
			after := connection.Conn().Config()
			if after.DefaultQueryExecMode != actual.DefaultQueryExecMode || after.StatementCacheCapacity != actual.StatementCacheCapacity || after.DescriptionCacheCapacity != actual.DescriptionCacheCapacity {
				t.Fatal("source reads changed the physical connection defaults")
			}
		})
	}
}

func TestPlaybackSourcePlanReadsCurrentParametersAndMedia(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	otherPath := libraryIntegrationFile(t, fixture.allowedRoot, "movies/Nested/Other.mkv", "video:other-source-plan")
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	items := libraryIntegrationQuery(t, fixture.ctx, fixture.store, Query{UserID: fixture.userID, ParentID: fixture.library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	other := libraryIntegrationItemByPath(t, items.Items, otherPath)
	configuration := fixture.pool.Config().ConnConfig.Copy()
	configuration.DefaultQueryExecMode, configuration.StatementCacheCapacity = pgx.QueryExecModeCacheDescribe, 1
	conn, err := pgx.ConnectConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(fixture.ctx)
	access := unrestrictedLibraryAccess()
	for _, planPolicy := range []string{"force_custom_plan", "force_generic_plan"} {
		if _, err := conn.Exec(fixture.ctx, "SELECT set_config('plan_cache_mode',$1,false)", pgx.QueryExecModeExec, planPolicy); err != nil {
			t.Fatal(err)
		}
		for _, item := range []Item{fixture.item, other} {
			snapshot, err := playbackSourcePlanRead(fixture.ctx, conn, access, item.ID, media.SourceID(item.ID))
			if err != nil || snapshot.mediaFile.Item.ID != item.ID || snapshot.mediaFile.SourceID != media.SourceID(item.ID) ||
				snapshot.mediaFile.Item.Path != item.Path || snapshot.relativePath != filepath.ToSlash(filepath.Join("Nested", filepath.Base(item.Path))) {
				t.Fatalf("reused source plan returned another parameter's result: %v", err)
			}
		}
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE items SET media=jsonb_set(media,'{DurationTicks}','7654321'::jsonb) WHERE id=$1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := playbackSourcePlanRead(fixture.ctx, conn, access, fixture.item.ID, media.SourceID(fixture.item.ID))
	if err != nil || snapshot.mediaFile.Item.Media == nil || snapshot.mediaFile.Item.Media.DurationTicks != 7654321 {
		t.Fatalf("cached source plan reused an old media result: %v", err)
	}
	for _, input := range []struct{ itemID, sourceID string }{
		{"missing-source-plan-item", ""}, {fixture.item.ID, "source_wrong"},
	} {
		result, err := playbackSourcePlanRead(fixture.ctx, conn, access, input.itemID, input.sourceID)
		if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(result, indexedMediaSource{}) {
			t.Fatalf("source plan bypassed missing-item or source-ID validation: %v", err)
		}
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{Streams}','"invalid"'::jsonb) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	result, err := playbackSourcePlanRead(fixture.ctx, conn, access, fixture.item.ID, "")
	if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(result, indexedMediaSource{}) || !strings.Contains(err.Error(), "decode playback media snapshot") {
		t.Fatalf("source plan bypassed complete media decoding: %v", err)
	}
}
