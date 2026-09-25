package library

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type itemCountTracer struct {
	mu         sync.Mutex
	statements []string
}

func (trace *itemCountTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	trace.statements = append(trace.statements, data.SQL)
	trace.mu.Unlock()
	return ctx
}

func (*itemCountTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *itemCountTracer) reset() {
	trace.mu.Lock()
	trace.statements = nil
	trace.mu.Unlock()
}

func (trace *itemCountTracer) assertCountWithoutPage(t *testing.T) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	counts := 0
	for index, statement := range trace.statements {
		if !strings.Contains(statement, "SELECT count(*)") {
			continue
		}
		counts++
		if index != len(trace.statements)-2 || !strings.EqualFold(strings.TrimSpace(trace.statements[index+1]), "commit") {
			t.Fatal("the count was followed by a page or attachment read instead of transaction completion")
		}
	}
	if counts != 1 {
		t.Fatalf("captured %d count statements, want exactly one authorized catalog count", counts)
	}
}

func countItemsTestStore(t *testing.T, ctx context.Context, fixture *Store) (*Store, *itemCountTracer) {
	t.Helper()
	trace := &itemCountTracer{}
	config := fixture.pool.Config()
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return &Store{pool: reader}, trace
}

func TestCountItemsPreservesAuthorizedFiltersWithoutReadingPages(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedMoviePageProjectionFixture(t, ctx, fixture.pool)
	store, trace := countItemsTestStore(t, ctx, fixture)
	favorite := true
	for _, test := range []struct {
		name  string
		query Query
		total int
	}{
		{name: "recursive movies", query: Query{ParentID: "library-b", Recursive: true, IncludeItemTypes: []string{"Movie"}}, total: 225},
		{name: "search", query: Query{Recursive: true, IncludeItemTypes: []string{"Movie"}, SearchTerm: "Page Movie 020"}, total: 10},
		{name: "favorite", query: Query{Recursive: true, IncludeItemTypes: []string{"Movie"}, IsFavorite: &favorite}, total: 2},
		{name: "literal search", query: Query{Recursive: true, IncludeItemTypes: []string{"Episode"}, SearchTerm: "100%_Final"}, total: 1},
		{name: "explicit IDs retain visibility", query: Query{Ids: []string{"movie-a", "page-0201", "page-hidden-movie"}}, total: 1},
		{name: "exhausted offset", query: Query{ParentID: "library-b", Recursive: true, IncludeItemTypes: []string{"Movie"}, StartIndex: 1000}, total: 225},
		{name: "empty population", query: Query{Recursive: true, SearchTerm: "no matching count fixture"}, total: 0},
		{name: "root libraries", query: Query{}, total: 1},
		{name: "nested parent", query: Query{ParentID: "series-b", Recursive: true, IncludeItemTypes: []string{"Episode"}}, total: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID = "restricted"
			// The previous HTTP path requested one row even though it discarded it.
			baselineQuery := query
			baselineQuery.Limit = 1
			baseline, err := fixture.QueryItems(ctx, baselineQuery)
			if err != nil || baseline.TotalRecordCount != test.total {
				t.Fatalf("fixture page count = %d, error = %v; want %d", baseline.TotalRecordCount, err, test.total)
			}
			trace.reset()
			result, err := store.CountQueryItems(ctx, query)
			if err != nil || result.TotalRecordCount != baseline.TotalRecordCount || result.Items == nil || len(result.Items) != 0 {
				t.Fatalf("count-only result = %+v, error = %v; want empty Items and total %d", result, err, test.total)
			}
			trace.assertCountWithoutPage(t)
		})
	}
	query := Query{UserID: "restricted", ParentID: "library-b", Recursive: true, IncludeItemTypes: []string{"Movie"}}
	page, err := store.QueryItems(ctx, query)
	if err != nil || page.TotalRecordCount != 225 || len(page.Items) != 100 {
		t.Fatalf("ordinary library Limit=0 no longer means the default 100-row page: items=%d total=%d error=%v", len(page.Items), page.TotalRecordCount, err)
	}
	for _, parent := range []string{"library-a", "page-hidden-folder", "missing-count-parent"} {
		query.ParentID = parent
		if _, err := store.CountQueryItems(ctx, query); !errors.Is(err, ErrNotFound) {
			t.Fatalf("count accepted hidden or missing parent %q: %v", parent, err)
		}
	}
}

func TestCountItemsRechecksSubjectAuthority(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	application := seedCatalogApplicationKey(t, ctx, fixture.pool, "count-application", true)
	store, trace := countItemsTestStore(t, ctx, fixture)
	query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Movie"}}
	result, err := store.CountQueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 1 {
		t.Fatalf("initial user count = %+v, %v", result, err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	result, err = store.CountQueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("a fresh count retained revoked library access: %+v, %v", result, err)
	}
	trace.assertCountWithoutPage(t)
	query.ApplicationCredentialID = application.ApplicationCredentialID
	trace.reset()
	result, err = store.CountQueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Fatalf("the application credential bypassed its explicitly selected user's catalog restrictions: %+v, %v", result, err)
	}
	trace.assertCountWithoutPage(t)
	query.UserID = ""
	trace.reset()
	result, err = store.CountQueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 2 || len(result.Items) != 0 {
		t.Fatalf("the userless application credential lost its catalog authority: %+v, %v", result, err)
	}
	trace.assertCountWithoutPage(t)
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, application.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CountQueryItems(ctx, query); err == nil {
		t.Fatal("a revoked application credential could still count catalog items")
	}
	query.ApplicationCredentialID, query.UserID = "", "disabled"
	if _, err := store.CountQueryItems(ctx, query); err == nil {
		t.Fatal("a disabled user could count catalog items")
	}
}

func TestCountItemsPreservesCollectionMembershipAndDuplicates(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('count-playlist','library-b','library-b','Count Playlist','Count Playlist','Playlist',true),
		('count-boxset','library-b','library-b','Count BoxSet','Count BoxSet','BoxSet',true),
		('count-private','library-b','library-b','Private Playlist','Private Playlist','Playlist',true);
		INSERT INTO media_collections(item_id,owner_id,kind) VALUES
		('count-playlist','restricted','Playlist'),('count-boxset','restricted','BoxSet'),('count-private','default','Playlist');
		INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES
		('count-playlist','movie-b',0),('count-playlist','movie-b',1),('count-playlist','movie-a',2),
		('count-boxset','series-b',0),('count-private','movie-b',0)`); err != nil {
		t.Fatal(err)
	}
	store, trace := countItemsTestStore(t, ctx, fixture)
	for _, query := range []Query{
		{UserID: "restricted", ParentID: "count-playlist", StartIndex: 1},
		{UserID: "restricted", ParentID: "count-playlist", StartIndex: 100},
		{UserID: "restricted", ParentID: "count-boxset", Recursive: true, IncludeItemTypes: []string{"Episode"}},
	} {
		baselineQuery := query
		baselineQuery.Limit = 1
		baseline, err := fixture.QueryItems(ctx, baselineQuery)
		if err != nil || baseline.TotalRecordCount != 2 {
			t.Fatalf("collection baseline = %+v, %v", baseline, err)
		}
		trace.reset()
		result, err := store.CountQueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != baseline.TotalRecordCount || result.Items == nil || len(result.Items) != 0 {
			t.Fatalf("collection count changed duplicate entries or authorized recursive membership: %+v, %v", result, err)
		}
		trace.assertCountWithoutPage(t)
	}
	if _, err := store.CountQueryItems(ctx, Query{UserID: "restricted", ParentID: "count-private"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private collection count became visible: %v", err)
	}
}

func TestCountItemsRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	for _, query := range []Query{
		{}, {UserID: "user", StartIndex: -1}, {UserID: "user", Limit: -1},
		{UserID: "user", Limit: 1001}, {UserID: "user", SortBy: "Random"},
		{UserID: "user", IncludeItemTypes: []string{"unknown"}},
	} {
		if _, err := store.CountQueryItems(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid count input returned %v, want ErrInvalidInput", err)
		}
	}
}
