package library

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type movieProjectionTracer struct {
	statement string
	arguments []any
	captures  int
}

func (trace *movieProjectionTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "SELECT i.id, i.library_id, COALESCE(i.parent_id, '')") &&
		strings.Contains(data.SQL, " ORDER BY ") && strings.Contains(data.SQL, " LIMIT ") {
		trace.statement, trace.arguments = data.SQL, append([]any(nil), data.Args...)
		trace.captures++
	}
	return ctx
}

func (*movieProjectionTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestQueryItemsMovieProjectionPreservesAuthorizedFieldsAndPlan(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, fixture.pool)
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO items
		(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('movie-b2','library-b','library-b','A Movie','A Movie','Movie',false),
		('album-b','library-b','library-b','E Album','E Album','MusicAlbum',true);
		UPDATE items SET parent_id='album-b' WHERE id='audio-b';
		UPDATE items SET root_id='root-b',path='/media/b/Movie.mkv',relative_path='Movie.mkv',
			file_identity='projection-source',file_size=512,modified_at='2026-01-01T00:00:00Z',
			media='{"DurationTicks":600000000,"Container":"mkv"}'::jsonb WHERE id='movie-b';
		UPDATE item_metadata_state SET effective='{"Overview":"Effective movie overview","ProductionYear":2020}'::jsonb
			WHERE item_id='movie-b';
		INSERT INTO user_item_data(user_id,item_id,is_favorite) VALUES ('restricted','movie-b',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO item_intro_state
		(item_id,source_revision,start_ticks,end_ticks,provenance)
		SELECT i.id,`+introSourceRevisionSQL+`,10000000,30000000,'Manual' FROM items i WHERE i.id='movie-b'`); err != nil {
		t.Fatal(err)
	}
	trace := &movieProjectionTracer{}
	config := fixture.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	store := &Store{pool: reader}
	favorite := true
	for _, test := range []struct {
		name  string
		query Query
		total int
		ids   []string
		typed bool
	}{
		{name: "page", query: Query{IncludeItemTypes: []string{"mOvIe"}, StartIndex: 1, Limit: 1}, total: 2, ids: []string{"movie-b"}, typed: true},
		{name: "search", query: Query{IncludeItemTypes: []string{"Movie"}, SearchTerm: "C Movie", Limit: 20}, total: 1, ids: []string{"movie-b"}, typed: true},
		{name: "favorite", query: Query{IncludeItemTypes: []string{"Movie"}, IsFavorite: &favorite, Limit: 20}, total: 1, ids: []string{"movie-b"}, typed: true},
		{name: "explicit ID", query: Query{IncludeItemTypes: []string{"Movie"}, Ids: []string{"movie-b"}, Limit: 20}, total: 1, ids: []string{"movie-b"}, typed: true},
		{name: "empty page", query: Query{IncludeItemTypes: []string{"Movie"}, StartIndex: 99, Limit: 1}, total: 2, ids: []string{}, typed: true},
		{name: "global scope", query: Query{IncludeItemTypes: []string{"Movie"}, Limit: 20}, total: 2, ids: []string{"movie-b2", "movie-b"}, typed: true},
		{name: "mixed types", query: Query{IncludeItemTypes: []string{"Movie", "Episode", "Audio"}, Limit: 20}, total: 5,
			ids: []string{"episode-b1", "movie-b2", "episode-b2", "movie-b", "audio-b"}},
		{name: "unbounded types", query: Query{Limit: 20}, total: 8,
			ids: []string{"episode-b1", "movie-b2", "episode-b2", "movie-b", "audio-b", "album-b", "season-b", "series-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			*trace = movieProjectionTracer{}
			query := test.query
			query.UserID, query.ParentID, query.Recursive, query.SortBy = "restricted", "library-b", true, "SortName"
			if test.name == "global scope" {
				query.ParentID = ""
			}
			result, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalRecordCount != test.total || !reflect.DeepEqual(queryItemIDs(result.Items), test.ids) {
				t.Fatalf("authorized total or page changed: total=%d ids=%v", result.TotalRecordCount, queryItemIDs(result.Items))
			}
			if trace.captures != 1 {
				t.Fatalf("captured %d catalog page statements, want one", trace.captures)
			}
			for _, fragment := range []string{"album_ancestors", "tv_parent", "tv_series", "i.type = 'MusicAlbum' AND i.is_folder"} {
				if strings.Contains(trace.statement, fragment) == test.typed {
					t.Fatalf("unexpected conditional projection %q for typed=%t", fragment, test.typed)
				}
			}
			normalized, err := normalizeItemQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			tx, access, err := fixture.beginSubjectRead(ctx, Subject{UserID: query.UserID})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
				t.Fatal(err)
			}
			projection := access.scopeSQL(itemQueryColumns(normalized))
			if strings.Count(trace.statement, projection) != 1 {
				t.Fatal("the actual page did not contain exactly one authorized projection")
			}
			baselineSQL := strings.Replace(trace.statement, projection, access.itemColumnsSQL(), 1)
			baseline := readMovieProjectionBaseline(t, ctx, tx, access, query, baselineSQL, trace.arguments)
			if !reflect.DeepEqual(result.Items, baseline) {
				t.Fatal("the specialized query changed complete item fields from the original projection")
			}
			for _, item := range result.Items {
				if item.ID == "movie-b" && (item.Metadata == nil || item.Metadata.Overview != "Effective movie overview" ||
					len(item.Entities.Genres) != 2 || len(item.Entities.People) != 1 || item.Media == nil ||
					item.Intro == nil || item.Intro.Provenance != "Manual" || item.Intro.StartTicks != 10000000 ||
					item.UserData == nil || !item.UserData.IsFavorite || item.ChildCount != nil ||
					item.Album != nil || item.Series != nil || item.Season != nil) {
					t.Fatal("the Movie fixture lost metadata, entities, media, intro, user state, or NULL projections")
				}
				if item.ID == "audio-b" && (item.Album == nil || item.Album.ID != "album-b") {
					t.Fatal("the mixed population lost the audio album projection")
				}
				if item.ID == "episode-b1" && (item.Season == nil || item.Series == nil) {
					t.Fatal("the mixed population lost the television parent projection")
				}
			}
			if test.name == "page" {
				for _, plan := range []struct {
					statement string
					typed     bool
				}{{trace.statement, true}, {baselineSQL, false}} {
					var encoded []byte
					if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON, COSTS OFF) "+plan.statement, trace.arguments...).Scan(&encoded); err != nil {
						t.Fatal(err)
					}
					if !json.Valid(encoded) || !strings.Contains(string(encoded), "descendants") {
						t.Fatal("the page plan lost the recursive parent population")
					}
					for _, name := range []string{"album_ancestors", "tv_parent", "tv_series"} {
						if plan.typed && strings.Contains(string(encoded), name) {
							t.Fatalf("unexpected unreachable plan %q for typed=%t", name, plan.typed)
						}
					}
					t.Logf("catalog page plan: specialized=%t bytes=%d", plan.typed, len(encoded))
				}
			}
		})
	}
}

func readMovieProjectionBaseline(t *testing.T, ctx context.Context, tx pgx.Tx, access libraryAccess, query Query, statement string, args []any) []Item {
	t.Helper()
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		item.CanPlay = access.canPlay
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := attachUserData(ctx, tx, query.UserID, items, access); err != nil {
		t.Fatal(err)
	}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		t.Fatal(err)
	}
	if len(query.Ids) != 0 {
		if err := attachExtraItemAttributes(ctx, tx, items, access); err != nil {
			t.Fatal(err)
		}
	}
	if err := attachCollectionInfo(ctx, tx, access, items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestItemQueryColumnsFallsBackWithoutExclusiveMovieProof(t *testing.T) {
	for _, query := range []Query{
		{},
		{IncludeItemTypes: []string{"Unknown"}},
		{IncludeItemTypes: []string{"movie"}},
		{IncludeItemTypes: []string{"Episode"}},
		{IncludeItemTypes: []string{"Movie", "Audio"}},
		{IncludeItemTypes: []string{"Movie"}, expectedEpisodePopulation: true},
	} {
		if itemQueryColumns(query) != itemColumns {
			t.Fatalf("an unproven item population used specialized columns: %+v", query)
		}
	}
}
