package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogQueryJITPreservesScopedResultsAndSessionSetting(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	application := seedCatalogApplicationKey(t, ctx, fixture.pool, "catalog-jit-key", true)
	for _, role := range []string{"theme", "extra"} {
		id := "catalog-jit-reserved-" + role
		themeTestItem(t, ctx, fixture, id, "Video", "library-b", "library-b", role+"/reserved.mp4")
		if _, err := fixture.pool.Exec(ctx, "INSERT INTO "+role+`_reserved_paths(root_id, relative_path, is_directory)
			SELECT root_id, relative_path, false FROM items WHERE id=$1`, id); err != nil {
			t.Fatalf("reserve the %s resource path: %v", role, err)
		}
	}
	for _, incoming := range []string{"on", "off"} {
		for _, credential := range []string{"user", "application"} {
			t.Run(incoming+"/"+credential, func(t *testing.T) {
				trace := &catalogQueryJITTracer{}
				store := catalogQueryJITStore(t, ctx, fixture, incoming, trace)
				pid := assertCatalogQueryJITSession(t, ctx, store.pool, incoming, 0)
				query := Query{UserID: "restricted", ParentID: "library-b", Recursive: true,
					IncludeItemTypes: []string{"Movie", "Episode", "Video"}, SortBy: "SortName",
					StartIndex: 1, Limit: 2}
				if credential == "application" {
					query.ApplicationCredentialID = application.ApplicationCredentialID
				}
				var first ItemResult
				for call := 0; call < 2; call++ {
					result, err := store.QueryItems(ctx, query)
					if err != nil {
						t.Fatalf("read the scoped catalog page: %v", err)
					}
					if result.TotalRecordCount != 3 || !slices.Equal(queryItemIDs(result.Items), []string{"episode-b2", "movie-b"}) {
						t.Fatalf("catalog filtering, total or page order changed: %+v", result)
					}
					if call == 0 {
						first = result
					} else if !reflect.DeepEqual(result, first) {
						t.Fatal("the cached catalog statement changed its complete item projections")
					}
					assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
				}
				query.SearchTerm, query.StartIndex, query.Limit = "100%_Final", 0, 20
				result, err := store.QueryItems(ctx, query)
				if err != nil || result.TotalRecordCount != 1 || !slices.Equal(queryItemIDs(result.Items), []string{"episode-b2"}) {
					t.Fatalf("literal search filtering changed: result=%+v, error=%v", result, err)
				}
				assertCatalogQueryJITReads(t, trace, 3, pid)
				assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
				query.ParentID = "library-a"
				if _, err := store.QueryItems(ctx, query); !errors.Is(err, ErrNotFound) {
					t.Fatalf("an unauthorized parent remained readable: %v", err)
				}
				assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
			})
		}
	}
}

func TestCatalogQueryJITRestoresSessionAfterQueryFailure(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	for _, incoming := range []string{"on", "off"} {
		t.Run(incoming, func(t *testing.T) {
			trace := &catalogQueryJITTracer{failPage: true}
			store := catalogQueryJITStore(t, ctx, fixture, incoming, trace)
			pid := assertCatalogQueryJITSession(t, ctx, store.pool, incoming, 0)
			query := Query{UserID: "restricted", ParentID: "library-b", Recursive: true,
				IncludeItemTypes: []string{"Episode"}, Limit: 20}
			_, err := store.QueryItems(ctx, query)
			var queryError, injectedError *pgconn.PgError
			if !errors.As(err, &queryError) || queryError.Code != "25P02" ||
				!errors.As(trace.injected, &injectedError) || injectedError.Code != "22012" {
				t.Fatalf("the page did not fail in an aborted database transaction: query=%v, injected=%v", err, trace.injected)
			}
			assertCatalogQueryJITReads(t, trace, 1, pid)
			assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
			trace.failPage = false
			result, err := store.QueryItems(ctx, query)
			if err != nil || result.TotalRecordCount != 2 || !slices.Equal(queryItemIDs(result.Items), []string{"episode-b1", "episode-b2"}) {
				t.Fatalf("the same session was not reusable after rollback: result=%+v, error=%v", result, err)
			}
			assertCatalogQueryJITReads(t, trace, 2, pid)
			assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
		})
	}
}

func catalogQueryJITStore(t *testing.T, ctx context.Context, fixture *Store, incoming string, trace *catalogQueryJITTracer) *Store {
	t.Helper()
	config := fixture.pool.Config()
	config.MaxConns, config.MinConns = 1, 0
	config.ConnConfig.RuntimeParams["jit"] = incoming
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create the single-session catalog pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	return &Store{pool: pool}
}

type catalogQueryJITRead struct {
	kind, jit, readOnly, isolation string
	pid                            int32
}

type catalogQueryJITTracer struct {
	reads    []catalogQueryJITRead
	err      error
	failPage bool
	injected error
}

type catalogQueryJITObservationKey struct{}

func (trace *catalogQueryJITTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(catalogQueryJITObservationKey{}) != nil {
		return ctx
	}
	kind := ""
	if strings.Contains(data.SQL, "SELECT count(*) FROM items i WHERE ") {
		kind = "count"
	} else if strings.Contains(data.SQL, " FROM items i WHERE ") && strings.Contains(data.SQL, " LIMIT $") && strings.Contains(data.SQL, " OFFSET $") {
		kind = "page"
	}
	if kind == "" {
		return ctx
	}
	observation := catalogQueryJITRead{kind: kind}
	observationCtx := context.WithValue(ctx, catalogQueryJITObservationKey{}, true)
	if err := conn.QueryRow(observationCtx, `SELECT current_setting('jit'), current_setting('transaction_read_only'),
		current_setting('transaction_isolation'), pg_backend_pid()`).
		Scan(&observation.jit, &observation.readOnly, &observation.isolation, &observation.pid); err != nil {
		trace.err = errors.Join(trace.err, fmt.Errorf("observe the catalog transaction: %w", err))
	}
	trace.reads = append(trace.reads, observation)
	if kind == "page" && trace.failPage {
		// A real SQL error tests rollback of an aborted transaction after the
		// count succeeded, without cancelling or replacing its pooled connection.
		_, trace.injected = conn.Exec(observationCtx, `SELECT 1 / 0`)
	}
	return ctx
}

func (*catalogQueryJITTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func assertCatalogQueryJITReads(t *testing.T, trace *catalogQueryJITTracer, pairs int, pid int32) {
	t.Helper()
	if trace.err != nil || len(trace.reads) != pairs*2 {
		t.Fatalf("catalog query observations are incomplete: reads=%+v, error=%v", trace.reads, trace.err)
	}
	for index, read := range trace.reads {
		wantKind := []string{"count", "page"}[index%2]
		if read.kind != wantKind || read.jit != "off" || read.readOnly != "on" || read.isolation != "repeatable read" || read.pid != pid {
			t.Fatalf("catalog count/page lost its transaction-local execution policy: %+v", read)
		}
	}
}

func assertCatalogQueryJITSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want string, previousPID int32) int32 {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("reacquire the catalog session: %v", err)
	}
	defer conn.Release()
	var setting string
	var pid int32
	if err := conn.QueryRow(ctx, `SELECT current_setting('jit'), pg_backend_pid()`).Scan(&setting, &pid); err != nil {
		t.Fatalf("read the returned session setting: %v", err)
	}
	if setting != want || previousPID != 0 && pid != previousPID || conn.Conn().PgConn().TxStatus() != 'I' {
		t.Fatalf("catalog query did not restore the same idle session: jit=%q, pid=%d, previous=%d, status=%c", setting, pid, previousPID, conn.Conn().PgConn().TxStatus())
	}
	return pid
}
