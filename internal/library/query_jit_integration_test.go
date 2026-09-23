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

func TestLatestQueryJITPreservesScopedResultsAndSessionSetting(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLatestQueryJITFixture(t, ctx, fixture.pool)
	application := seedCatalogApplicationKey(t, ctx, fixture.pool, "latest-jit-key", true)
	for _, incoming := range []string{"on", "off"} {
		for _, credential := range []string{"user", "application"} {
			for _, group := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/group=%t", incoming, credential, group), func(t *testing.T) {
					trace := &catalogQueryJITTracer{projections: true}
					store := catalogQueryJITStore(t, ctx, fixture, incoming, trace)
					pid := assertCatalogQueryJITSession(t, ctx, store.pool, incoming, 0)
					played := false
					query := Query{UserID: "restricted", ParentID: "library-b",
						IncludeItemTypes: []string{"Movie", "Episode"}, IsPlayed: &played, Limit: 2}
					if credential == "application" {
						query.ApplicationCredentialID = application.ApplicationCredentialID
					}
					want := []latestExpectation{{"movie-b", 1}, {"episode-b2", 1}}
					if group {
						want[1] = latestExpectation{"series-b", 1}
					}
					var first []LatestItem
					for call := 0; call < 2; call++ {
						trace.reads = nil
						result, err := store.QueryLatest(ctx, query, group)
						if err != nil {
							t.Fatalf("read the scoped latest page: %v", err)
						}
						assertLatestItems(t, result, want)
						if data := result[0].Item.UserData; data == nil || !data.IsFavorite || data.Played {
							t.Fatalf("latest lost the selected movie's user data: %+v", data)
						}
						if group {
							data := result[1].Item.UserData
							if data == nil || data.UnplayedItemCount == nil || *data.UnplayedItemCount != 1 || data.Played {
								t.Fatalf("latest lost the representative's folder state: %+v", data)
							}
						}
						if call == 0 {
							first = result
						} else if !reflect.DeepEqual(result, first) {
							t.Fatal("the cached latest statement changed its complete item projections")
						}
						reads := []string{"page", "user-data"}
						if group {
							reads = append(reads, "folder-user-data", "collection-user-data")
						}
						reads = append(reads, "subtitles", "owned-subtitles")
						assertCatalogQueryJITReadKinds(t, trace, reads, pid)
						assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
					}
					query.ParentID = "library-a"
					if _, err := store.QueryLatest(ctx, query, group); !errors.Is(err, ErrNotFound) {
						t.Fatalf("an unauthorized latest parent remained readable: %v", err)
					}
					assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
				})
			}
		}
	}
}

func TestLatestQueryJITRestoresSessionAfterProjectionFailure(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLatestQueryJITFixture(t, ctx, fixture.pool)
	for _, incoming := range []string{"on", "off"} {
		for _, group := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/group=%t", incoming, group), func(t *testing.T) {
				trace := &catalogQueryJITTracer{projections: true, failKind: "subtitles"}
				store := catalogQueryJITStore(t, ctx, fixture, incoming, trace)
				pid := assertCatalogQueryJITSession(t, ctx, store.pool, incoming, 0)
				played := false
				query := Query{UserID: "restricted", ParentID: "library-b",
					IncludeItemTypes: []string{"Movie", "Episode"}, IsPlayed: &played, Limit: 2}
				_, err := store.QueryLatest(ctx, query, group)
				var queryError, injectedError *pgconn.PgError
				if !errors.As(err, &queryError) || queryError.Code != "25P02" ||
					!errors.As(trace.injected, &injectedError) || injectedError.Code != "22012" {
					t.Fatalf("the projection did not fail in an aborted database transaction: query=%v, injected=%v", err, trace.injected)
				}
				reads := []string{"page", "user-data"}
				if group {
					reads = append(reads, "folder-user-data", "collection-user-data")
				}
				reads = append(reads, "subtitles")
				assertCatalogQueryJITReadKinds(t, trace, reads, pid)
				assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
				trace.failKind, trace.reads = "", nil
				result, err := store.QueryLatest(ctx, query, group)
				if err != nil {
					t.Fatalf("the same session was not reusable after latest rollback: %v", err)
				}
				want := []latestExpectation{{"movie-b", 1}, {"episode-b2", 1}}
				if group {
					want[1] = latestExpectation{"series-b", 1}
				}
				assertLatestItems(t, result, want)
				assertCatalogQueryJITReadKinds(t, trace, append(reads, "owned-subtitles"), pid)
				assertCatalogQueryJITSession(t, ctx, store.pool, incoming, pid)
			})
		}
	}
}

func TestAttachUserDataDerivesOnlyReturnedFolders(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, fixture.pool)
	for _, test := range []struct {
		name      string
		ids       []string
		folders   []string
		withScope bool
	}{
		{name: "leaves", ids: []string{"episode-b1", "video-b"}},
		{name: "mixed", ids: []string{"episode-b1", "video-b", "series-b", "album-b"},
			folders: []string{"album-b", "series-b"}, withScope: true},
		{name: "folders", ids: []string{"series-b", "season-b", "album-b", "artist-b"},
			folders: []string{"album-b", "artist-b", "season-b", "series-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := &catalogQueryJITTracer{projections: true}
			store := catalogQueryJITStore(t, ctx, fixture, "off", trace)
			pid := assertCatalogQueryJITSession(t, ctx, store.pool, "off", 0)
			tx, access, err := store.beginUserRead(ctx, "restricted")
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			ids := append(slices.Clone(test.ids), "library-b", "album-disc-b")
			items := readUserDataProjectionItems(t, ctx, tx, ids)
			// Repeated physical items still get independent personal state. A
			// virtual entry sharing a physical ID must not consume that ID.
			items = append(items, slices.Clone(items)...)
			items = append([]Item{{ID: "episode-b1", Type: "Episode", ExpectedEpisode: &ExpectedEpisodeInfo{},
				UserData: &UserData{ItemID: "stale"}}}, items...)
			trace.reads = nil
			var scopes []libraryAccess
			if test.withScope {
				scopes = append(scopes, access)
			}
			if err := attachUserData(ctx, tx, "restricted", items, scopes...); err != nil {
				t.Fatalf("attach the snapshot's user data: %v", err)
			}
			reads := []string{"user-data"}
			if len(test.folders) != 0 {
				reads = append(reads, "folder-user-data", "collection-user-data")
			}
			assertCatalogQueryJITReadKinds(t, trace, reads, pid)
			for _, read := range trace.reads {
				if read.kind == "folder-user-data" || read.kind == "collection-user-data" {
					if !slices.Equal(read.ids, test.folders) {
						t.Fatalf("derived query received IDs %v, want only returned folders %v", read.ids, test.folders)
					}
				}
			}
			want := map[string]UserData{
				"episode-b1": userDataQueryEpisodeState("restricted"),
				"video-b":    {ItemID: "video-b"},
				"series-b":   userDataQueryFolderState("series-b", 1, false, false),
				"season-b":   userDataQueryFolderState("season-b", 1, false, false),
				"album-b":    userDataQueryFolderState("album-b", 2, false, false),
				"artist-b":   userDataQueryFolderState("artist-b", 0, false, false),
			}
			seen := make(map[string]*UserData)
			for _, item := range items {
				if item.ExpectedEpisode != nil || item.ID == "library-b" || item.ID == "album-disc-b" {
					if item.UserData != nil {
						t.Fatalf("unsupported or virtual item retained user data: %+v", item)
					}
					continue
				}
				assertLibraryUserData(t, item, want[item.ID])
				if previous := seen[item.ID]; previous != nil {
					if previous == item.UserData || previous.LastPlayedDate != nil && previous.LastPlayedDate == item.UserData.LastPlayedDate ||
						previous.UnplayedItemCount != nil && previous.UnplayedItemCount == item.UserData.UnplayedItemCount {
						t.Fatalf("repeated item %s shares mutable user data", item.ID)
					}
				}
				seen[item.ID] = item.UserData
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAttachUserDataFolderSubsetKeepsReadSnapshot(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, fixture.pool)
	trace := &catalogQueryJITTracer{projections: true}
	store := catalogQueryJITStore(t, ctx, fixture, "off", trace)
	pid := assertCatalogQueryJITSession(t, ctx, store.pool, "off", 0)
	tx, access, err := store.beginUserRead(ctx, "restricted")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	items := readUserDataProjectionItems(t, ctx, tx, []string{"episode-b1", "series-b"})
	if _, err := fixture.pool.Exec(ctx, `UPDATE user_item_data SET play_count=99
		WHERE user_id='restricted' AND item_id='episode-b1';
		UPDATE user_item_data SET played=false WHERE user_id='restricted' AND item_id='episode-b2';
		UPDATE user_item_data SET is_favorite=true WHERE user_id='restricted' AND item_id='series-b'`); err != nil {
		t.Fatalf("commit user data after the item snapshot: %v", err)
	}
	trace.reads = nil
	if err := attachUserData(ctx, tx, "restricted", items, access); err != nil {
		t.Fatal(err)
	}
	assertCatalogQueryJITReadKinds(t, trace, []string{"user-data", "folder-user-data", "collection-user-data"}, pid)
	assertLibraryUserData(t, items[0], userDataQueryEpisodeState("restricted"))
	assertLibraryUserData(t, items[1], userDataQueryFolderState("series-b", 1, false, false))
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.GetItem(ctx, "restricted", "series-b")
	if err != nil {
		t.Fatal(err)
	}
	assertLibraryUserData(t, updated, userDataQueryFolderState("series-b", 2, false, true))
	leaf, err := fixture.GetItem(ctx, "restricted", "episode-b1")
	if err != nil {
		t.Fatal(err)
	}
	want := userDataQueryEpisodeState("restricted")
	want.PlayCount = 99
	assertLibraryUserData(t, leaf, want)
}

func readUserDataProjectionItems(t *testing.T, ctx context.Context, tx pgx.Tx, ids []string) []Item {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT id,type,is_folder FROM items WHERE id=ANY($1::text[]) ORDER BY id`, ids)
	if err != nil {
		t.Fatalf("read actual item kinds in the projection snapshot: %v", err)
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Type, &item.IsFolder); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(items) != len(ids) {
		t.Fatalf("read %d physical projection items, want %d", len(items), len(ids))
	}
	return items
}

func seedLatestQueryJITFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedLibraryLatestFixture(t, ctx, pool)
	// A media-bearing movie reaches both subtitle projections in grouped and
	// raw pages. A played episode exercises source filtering and folder state.
	if _, err := pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":15000000,"Container":"mkv"}'::jsonb WHERE id='movie-b';
		INSERT INTO user_item_data (user_id,item_id,is_favorite,played) VALUES
			('restricted','movie-b',true,false), ('restricted','episode-b1',false,true)`); err != nil {
		t.Fatalf("seed latest JIT projections: %v", err)
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
	ids                            []string
}

type catalogQueryJITTracer struct {
	reads       []catalogQueryJITRead
	err         error
	projections bool
	failPage    bool
	failKind    string
	injected    error
}

type catalogQueryJITObservationKey struct{}

func (trace *catalogQueryJITTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(catalogQueryJITObservationKey{}) != nil {
		return ctx
	}
	kind := ""
	if strings.Contains(data.SQL, "SELECT count(*) FROM items i WHERE ") {
		kind = "count"
	} else if (strings.Contains(data.SQL, " FROM items i WHERE ") || strings.Contains(data.SQL, "FROM latest_groups latest")) &&
		strings.Contains(data.SQL, " LIMIT $") && strings.Contains(data.SQL, " OFFSET $") {
		kind = "page"
	}
	if kind == "" && trace.projections {
		switch {
		case strings.HasPrefix(data.SQL, "SELECT "+userDataColumns+" FROM user_item_data"):
			kind = "user-data"
		case strings.HasPrefix(data.SQL, "WITH RECURSIVE roots AS (") && strings.Contains(data.SQL, "folder_descendants AS ("):
			kind = "folder-user-data"
		case strings.HasPrefix(data.SQL, "WITH RECURSIVE roots AS (") && strings.Contains(data.SQL, "collection_userdata_descendants AS ("):
			kind = "collection-user-data"
		case strings.Contains(data.SQL, " FROM item_subtitles s"):
			kind = "subtitles"
		case strings.Contains(data.SQL, " FROM item_owned_subtitles s"):
			kind = "owned-subtitles"
		}
	}
	if kind == "" {
		return ctx
	}
	observation := catalogQueryJITRead{kind: kind}
	if kind == "folder-user-data" || kind == "collection-user-data" {
		if len(data.Args) == 0 {
			trace.err = errors.Join(trace.err, errors.New("derived user data query has no root IDs"))
		} else if ids, ok := data.Args[0].([]string); ok {
			observation.ids = slices.Clone(ids)
			slices.Sort(observation.ids)
		} else {
			trace.err = errors.Join(trace.err, errors.New("derived user data query has unexpected root IDs"))
		}
	}
	observationCtx := context.WithValue(ctx, catalogQueryJITObservationKey{}, true)
	if err := conn.QueryRow(observationCtx, `SELECT current_setting('jit'), current_setting('transaction_read_only'),
		current_setting('transaction_isolation'), pg_backend_pid()`).
		Scan(&observation.jit, &observation.readOnly, &observation.isolation, &observation.pid); err != nil {
		trace.err = errors.Join(trace.err, fmt.Errorf("observe the catalog transaction: %w", err))
	}
	trace.reads = append(trace.reads, observation)
	if kind == "page" && trace.failPage || kind == trace.failKind {
		// A real SQL error tests rollback after earlier reads succeeded, without
		// cancelling or replacing the pooled connection.
		_, trace.injected = conn.Exec(observationCtx, `SELECT 1 / 0`)
	}
	return ctx
}

func (*catalogQueryJITTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func assertCatalogQueryJITReads(t *testing.T, trace *catalogQueryJITTracer, pairs int, pid int32) {
	t.Helper()
	want := make([]string, pairs*2)
	for index := range want {
		want[index] = []string{"count", "page"}[index%2]
	}
	assertCatalogQueryJITReadKinds(t, trace, want, pid)
}

func assertCatalogQueryJITReadKinds(t *testing.T, trace *catalogQueryJITTracer, want []string, pid int32) {
	t.Helper()
	if trace.err != nil || len(trace.reads) != len(want) {
		t.Fatalf("catalog query observations are incomplete: reads=%+v, error=%v", trace.reads, trace.err)
	}
	for index, read := range trace.reads {
		if read.kind != want[index] || read.jit != "off" || read.readOnly != "on" || read.isolation != "repeatable read" || read.pid != pid {
			t.Fatalf("catalog read lost its transaction-local execution policy: %+v", read)
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
