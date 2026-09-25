package library

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueryItemsMoviePageProjectsOnlySelectedRows(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedMoviePageProjectionFixture(t, ctx, fixture.pool)
	trace := &movieProjectionTracer{}
	config := fixture.pool.Config()
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	store := &Store{pool: reader}
	favorite := true
	for _, test := range []struct {
		name     string
		query    Query
		total    int
		ids      []string
		global   bool
		fallback bool
	}{
		{name: "shallow", query: Query{Limit: 20}, total: 225, ids: moviePageIDs(1, 20)},
		{name: "deep", query: Query{StartIndex: 200, Limit: 20}, total: 225, ids: moviePageIDs(201, 220)},
		{name: "tie boundary", query: Query{StartIndex: 201, Limit: 3}, total: 225, ids: moviePageIDs(202, 204)},
		{name: "exhausted", query: Query{StartIndex: 300, Limit: 20}, total: 225, ids: []string{}},
		{name: "filtered", query: Query{SearchTerm: "Page Movie 020", StartIndex: 1, Limit: 3}, total: 10, ids: moviePageIDs(201, 203)},
		{name: "favorite", query: Query{IsFavorite: &favorite, StartIndex: 1, Limit: 1}, total: 2, ids: []string{"page-0203"}},
		{name: "global visibility", query: Query{StartIndex: 200, Limit: 20}, total: 225, ids: moviePageIDs(201, 220), global: true},
		{name: "descending fallback", query: Query{SortOrder: "DESC", Limit: 3}, total: 225, ids: []string{"movie-b", "page-0224", "page-0223"}, fallback: true},
		{name: "explicit ID fallback", query: Query{Ids: []string{"page-0201", "page-0203"}, Limit: 20}, total: 2, ids: []string{"page-0201", "page-0203"}, fallback: true},
		{name: "mixed fallback", query: Query{IncludeItemTypes: []string{"Movie", "Episode"}, Limit: 3}, total: 227,
			ids: []string{"episode-b1", "page-0001", "page-0002"}, fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID, query.ParentID, query.Recursive, query.SortBy = "restricted", "library-b", true, "SortName"
			if len(query.IncludeItemTypes) == 0 {
				query.IncludeItemTypes = []string{"Movie"}
			}
			if test.global {
				query.ParentID = ""
			}
			*trace = movieProjectionTracer{}
			result, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalRecordCount != test.total || !reflect.DeepEqual(queryItemIDs(result.Items), test.ids) {
				t.Fatalf("authorized page changed: total=%d ids=%v, want total=%d ids=%v", result.TotalRecordCount, queryItemIDs(result.Items), test.total, test.ids)
			}
			if trace.captures != 1 || strings.Contains(trace.statement, "selected_page AS MATERIALIZED") == test.fallback {
				t.Fatalf("unexpected page selection: captures=%d fallback=%t", trace.captures, test.fallback)
			}
			tx, access, err := fixture.beginSubjectRead(ctx, Subject{UserID: query.UserID})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
				t.Fatal(err)
			}
			baselineSQL, args, total := originalMoviePageQuery(t, ctx, tx, access, query)
			if total != result.TotalRecordCount || !reflect.DeepEqual(args, trace.arguments) {
				t.Fatal("the page rewrite changed the original count or parameter vector")
			}
			baseline := readMovieProjectionBaseline(t, ctx, tx, access, query, baselineSQL, args)
			if !reflect.DeepEqual(result.Items, baseline) {
				t.Fatal("the page boundary changed complete item fields from the original wide query")
			}
			for _, item := range result.Items {
				if item.ID == "page-0201" && (item.Metadata == nil || item.Metadata.Overview != "Effective page-0201" ||
					len(item.Entities.Genres) != 2 || len(item.Entities.People) != 1 || item.Media == nil ||
					item.Media.DurationTicks != 600000000 || item.Intro == nil || item.Intro.StartTicks != 10000000 ||
					item.Intro.Provenance != "Manual" || item.UserData == nil || !item.UserData.IsFavorite ||
					len(item.Subtitles) != 1 || item.ChildCount != nil || item.Album != nil || item.Series != nil || item.Season != nil) {
					t.Fatal("the selected Movie lost metadata, entities, media, intro, user data, subtitles, or typed NULL columns")
				}
			}
			if test.name == "shallow" || test.name == "deep" {
				assertMoviePageProjectionLoops(t, ctx, tx, trace.statement, args, 20)
			}
		})
	}

	t.Run("repeatable snapshot", func(t *testing.T) {
		query := Query{UserID: "restricted", ParentID: "library-b", Recursive: true, IncludeItemTypes: []string{"Movie"},
			SortBy: "SortName", StartIndex: 200, Limit: 20}
		*trace = movieProjectionTracer{}
		if _, err := store.QueryItems(ctx, query); err != nil {
			t.Fatal(err)
		}
		statement, arguments := trace.statement, append([]any(nil), trace.arguments...)
		tx, access, err := fixture.beginSubjectRead(ctx, Subject{UserID: query.UserID})
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
			t.Fatal(err)
		}
		before := readMovieProjectionBaseline(t, ctx, tx, access, query, statement, arguments)
		if _, err := fixture.pool.Exec(ctx, `UPDATE item_metadata_state
			SET effective=jsonb_set(effective,'{Overview}','"Changed after snapshot"'::jsonb)
			WHERE item_id='page-0201'`); err != nil {
			t.Fatal(err)
		}
		after := readMovieProjectionBaseline(t, ctx, tx, access, query, statement, arguments)
		baselineSQL, args, _ := originalMoviePageQuery(t, ctx, tx, access, query)
		baseline := readMovieProjectionBaseline(t, ctx, tx, access, query, baselineSQL, args)
		if len(before) != 20 || before[0].Metadata == nil || before[0].Metadata.Overview != "Effective page-0201" ||
			!reflect.DeepEqual(before, after) || !reflect.DeepEqual(before, baseline) {
			t.Fatal("the selector and wide projection did not retain the original repeatable-read snapshot")
		}
		rollback(tx)
		fresh, err := store.QueryItems(ctx, query)
		if err != nil || len(fresh.Items) != 20 || fresh.Items[0].Metadata == nil || fresh.Items[0].Metadata.Overview != "Changed after snapshot" {
			t.Fatalf("a fresh query did not observe the metadata change: %v", err)
		}
	})
}

func seedMoviePageProjectionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedLibraryEntityQueryFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id,library_id,parent_id,root_id,relative_path,path,name,sort_name,type,is_folder,file_identity,file_size,modified_at,media)
		SELECT 'page-'||lpad(n::text,4,'0'),'library-b','library-b','root-b',
			'Page'||n||'.mkv','/media/b/Page'||n||'.mkv','Page Movie '||lpad(n::text,4,'0'),
			'A Page '||lpad(((n-1)/2)::text,4,'0'),'Movie',false,'page-source-'||n,512,'2026-01-01T00:00:00Z',
			'{"DurationTicks":600000000,"Container":"mkv"}'::jsonb FROM generate_series(1,224) n;
		INSERT INTO items (id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('page-hidden-folder','library-b','library-b','Hidden Folder','0 Hidden Folder','Folder',true),
		('page-hidden-movie','library-b','page-hidden-folder','Hidden Movie','0 Hidden Movie','Movie',false);
		UPDATE users SET policy=policy||'{"ExcludedSubFolders":["page-hidden-folder"]}'::jsonb WHERE id='restricted';
		WITH changed AS (UPDATE items SET local_metadata=(SELECT local_metadata FROM items WHERE id='movie-b')
			WHERE id='page-0201' RETURNING id,local_metadata)
		SELECT sync_catalog_item_entities(id,local_metadata) FROM changed;
		UPDATE item_metadata_state SET effective=jsonb_build_object('Overview','Effective '||item_id,'ProductionYear',2020)
			WHERE item_id LIKE 'page-%';
		INSERT INTO user_item_data(user_id,item_id,is_favorite) VALUES
		('restricted','page-0201',true),('restricted','page-0203',true);
		INSERT INTO item_subtitles
		(item_id,root_id,stream_index,relative_path,file_identity,source_hash,file_size,modified_at,
		 change_time_ns,codec,language,title,is_default,mime_type)
		VALUES ('page-0201','root-b',42,'Page201.en.srt','page-subtitle',repeat('c',64),32,
		 '2026-01-01T00:00:00Z',1,'srt','eng','Page subtitle',true,'application/x-subrip')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_intro_state(item_id,source_revision,start_ticks,end_ticks,provenance)
		SELECT i.id,`+introSourceRevisionSQL+`,10000000,30000000,'Manual' FROM items i WHERE i.id='page-0201'`); err != nil {
		t.Fatal(err)
	}
}

func moviePageIDs(first, last int) []string {
	ids := make([]string, 0, last-first+1)
	for index := first; index <= last; index++ {
		ids = append(ids, fmt.Sprintf("page-%04d", index))
	}
	return ids
}

// Reconstruct the previous page statement, independently of itemPageQuerySQL.
// The original filter and count builder remain the comparison authority.
func originalMoviePageQuery(t *testing.T, ctx context.Context, tx pgx.Tx, access libraryAccess, query Query) (string, []any, int) {
	t.Helper()
	normalized, err := normalizeItemQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	parentLibrary, err := readOrdinaryQueryParent(ctx, tx, normalized.ParentID, access)
	if err != nil {
		t.Fatal(err)
	}
	prefix, filter, args := itemQuerySQLWithExtraIDs(normalized, access, parentLibrary, len(normalized.Ids) != 0)
	var total int
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM items i WHERE "+filter, args...).Scan(&total); err != nil {
		t.Fatal(err)
	}
	args = append(args, normalized.Limit, normalized.StartIndex)
	statement := prefix + "SELECT " + access.scopeSQL(itemQueryColumns(normalized)) + " FROM items i WHERE " + filter +
		" ORDER BY " + access.scopeSQL(itemOrderSQL(normalized, 0)) + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return statement, args, total
}

func assertMoviePageProjectionLoops(t *testing.T, ctx context.Context, tx pgx.Tx, statement string, args []any, wanted int) {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, TIMING OFF, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type node struct {
		Relation string  `json:"Relation Name"`
		Alias    string  `json:"Alias"`
		Loops    float64 `json:"Actual Loops"`
		Plans    []node  `json:"Plans"`
	}
	var documents []struct{ Plan node }
	if err := json.Unmarshal(raw, &documents); err != nil || len(documents) != 1 {
		t.Fatalf("decode actual page plan: %v", err)
	}
	var matches int
	var loops float64
	var visit func(node)
	visit = func(current node) {
		if current.Relation == "item_metadata_state" && current.Alias == "ms" {
			matches++
			loops += current.Loops
		}
		for _, child := range current.Plans {
			visit(child)
		}
	}
	visit(documents[0].Plan)
	if matches != 1 || loops != float64(wanted) {
		t.Fatalf("metadata projection nodes=%d loops=%g, want one node with %d returned-row loops", matches, loops, wanted)
	}
}

func TestItemPageQueryFallsBackOutsideMovieSortNameProof(t *testing.T) {
	base := Query{IncludeItemTypes: []string{"Movie"}, SortBy: "SortName", SortOrder: "ASC"}
	for _, test := range []struct {
		name       string
		change     func(*Query)
		population string
		order      string
	}{
		{name: "unknown types", change: func(query *Query) { query.IncludeItemTypes = nil }},
		{name: "mixed types", change: func(query *Query) { query.IncludeItemTypes = []string{"Movie", "Audio"} }},
		{name: "expected population", change: func(query *Query) { query.expectedEpisodePopulation = true }},
		{name: "resume", change: func(query *Query) { query.Resumable = true }},
		{name: "explicit IDs", change: func(query *Query) { query.Ids = []string{"movie"} }},
		{name: "descending", change: func(query *Query) { query.SortOrder = "DESC" }},
		{name: "metadata sort", change: func(query *Query) { query.SortBy = "ProductionYear" }},
		{name: "compound sort", change: func(query *Query) { query.SortBy = "SortName,DateCreated" }},
		{name: "other population", population: "discovery_items"},
		{name: "different effective order", order: "i.id DESC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query, population, order := base, test.population, test.order
			if test.change != nil {
				test.change(&query)
			}
			if population == "" {
				population = "items"
			}
			if order == "" {
				order = "lower(i.sort_name) ASC NULLS LAST, i.id ASC"
			}
			prefix, columns, filter, pagination := "WITH RECURSIVE retained AS (SELECT 1) ", "i.id", "unchanged_filter", " LIMIT $6 OFFSET $7"
			want := prefix + "SELECT " + columns + " FROM " + population + " i WHERE " + filter + " ORDER BY " + order + pagination
			if got := itemPageQuerySQL(query, prefix, population, columns, filter, order, pagination); got != want {
				t.Fatal("an unsupported query changed its original complete SQL")
			}
		})
	}
}
