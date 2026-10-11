package library

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// Separate response-only state reads from the SQL that selects or orders rows.
// A disabled attachment must remove the former without changing the latter.
func userDataOutputQueryTrace(trace *itemCountTracer) (int, []string) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	attachments := 0
	selection := []string{}
	for _, statement := range trace.statements {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "SELECT "+userDataColumns+" FROM user_item_data") ||
			strings.HasPrefix(statement, "WITH RECURSIVE roots AS (") {
			attachments++
		} else if strings.Contains(statement, "user_item_data") {
			selection = append(selection, statement)
		}
	}
	return attachments, selection
}

func assertUserDataOutputProjection(t *testing.T, trace *itemCountTracer, query func(QueryProjection) (ItemResult, error), requiresState bool) {
	t.Helper()
	trace.reset()
	complete, err := query(QueryProjection{})
	if err != nil {
		t.Fatalf("complete projection: %v", err)
	}
	attachments, selection := userDataOutputQueryTrace(trace)
	hasUserData := false
	for index := range complete.Items {
		hasUserData = hasUserData || complete.Items[index].UserData != nil
		complete.Items[index].UserData = nil
	}
	if hasUserData && attachments == 0 {
		t.Fatal("the complete projection did not exercise user-data attachment SQL")
	}
	if requiresState && len(selection) == 0 {
		t.Fatal("the fixture did not exercise user-data selection or ordering")
	}
	trace.reset()
	narrow, err := query(QueryProjection{UserDataDisabled: true})
	if err != nil || !reflect.DeepEqual(complete, narrow) {
		t.Fatalf("disabled user data changed selected items, order, or other fields: complete=%+v narrow=%+v error=%v", complete, narrow, err)
	}
	remaining, narrowSelection := userDataOutputQueryTrace(trace)
	if remaining != 0 {
		t.Fatalf("disabled projection executed %d user-data attachment queries", remaining)
	}
	if !reflect.DeepEqual(selection, narrowSelection) {
		t.Fatal("disabled user-data output changed the SQL used for filtering or ordering")
	}
}

func TestUserDataProjectionPreservesItemFiltersSortingAndDefaults(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, fixture.pool)
	store, trace := countItemsTestStore(t, ctx, fixture)
	yes, no := true, false
	for _, test := range []struct {
		name          string
		query         Query
		requiresState bool
	}{
		{name: "leaves", query: Query{Ids: []string{"episode-b1", "episode-b2", "movie-a", "movie-b", "video-b"}}},
		{name: "folder summaries", query: Query{Ids: []string{"series-b", "season-b", "album-b"}}},
		{name: "played and favorite", query: Query{Recursive: true, IsPlayed: &no, IsFavorite: &yes}, requiresState: true},
		{name: "folder played state", query: Query{Ids: []string{"series-b", "season-b", "album-b"}, IsPlayed: &no}, requiresState: true},
		{name: "play count ordering", query: Query{Recursive: true, IncludeItemTypes: []string{"Episode", "Movie"}, SortBy: "PlayCount,Name", SortOrder: "Descending,Ascending", Limit: 2}, requiresState: true},
		{name: "last played ordering", query: Query{Recursive: true, IncludeItemTypes: []string{"Episode", "Movie"}, SortBy: "DatePlayed", SortOrder: "Descending", StartIndex: 1, Limit: 2}, requiresState: true},
		{name: "resumable selection", query: Query{Recursive: true, Resumable: true, Limit: 3}, requiresState: true},
		{name: "empty page", query: Query{Recursive: true, IsFavorite: &yes, StartIndex: 100}, requiresState: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID = "restricted"
			assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
				query.Projection = projection
				result, err := store.QueryItems(ctx, query)
				if err == nil && len(result.Items) == 0 && test.name != "empty page" {
					t.Fatal("the fixture did not select any items")
				}
				return result, err
			}, test.requiresState)
		})
	}
	for _, method := range []struct {
		name string
		call func(Query) (ItemResult, error)
	}{
		{name: "resume", call: func(query Query) (ItemResult, error) { return store.QueryResume(ctx, query) }},
		{name: "suggestions", call: func(query Query) (ItemResult, error) { return store.QuerySuggestions(ctx, query) }},
	} {
		t.Run(method.name, func(t *testing.T) {
			assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
				return method.call(Query{UserID: "restricted", Recursive: true, Limit: 3, Projection: projection})
			}, true)
		})
	}
}

func TestUserDataProjectionKeepsDirectItemAndApplicationAuthority(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, fixture.pool)
	application := seedCatalogApplicationKey(t, ctx, fixture.pool, "userdata-output-key", true)
	store, trace := countItemsTestStore(t, ctx, fixture)
	for _, id := range []string{"movie-b", "series-b"} {
		t.Run(id, func(t *testing.T) {
			assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
				var item Item
				var err error
				subject := Subject{UserID: "restricted"}
				if projection.UserDataDisabled {
					item, err = store.GetItemProjectionFor(ctx, subject, id, projection)
				} else {
					item, err = store.GetItemFor(ctx, subject, id)
				}
				return ItemResult{Items: []Item{item}, TotalRecordCount: 1}, err
			}, false)
		})
	}
	for _, userID := range []string{"", "restricted"} {
		t.Run("application user "+userID, func(t *testing.T) {
			assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
				return store.QueryItems(ctx, Query{UserID: userID, ApplicationCredentialID: application.ApplicationCredentialID,
					Ids: []string{"movie-a", "movie-b"}, Projection: projection})
			}, false)
		})
	}
	projection := QueryProjection{UserDataDisabled: true}
	if _, err := store.GetItemProjectionFor(ctx, Subject{UserID: "restricted"}, "movie-a", projection); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled projection changed hidden-item authority: %v", err)
	}
	if _, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "library-a", Projection: projection}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled projection changed parent authorization: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, application.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryItems(ctx, Query{ApplicationCredentialID: application.ApplicationCredentialID, Projection: projection}); err == nil {
		t.Fatal("disabled projection accepted a revoked application credential")
	}
}

func TestUserDataProjectionKeepsLatestGroupingAndSourceFilters(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, fixture.pool)
	store, trace := countItemsTestStore(t, ctx, fixture)
	yes := true
	query := Query{UserID: "restricted", IsFavorite: &yes,
		Ids: []string{"episode-b1", "episode-b2", "audio-b1", "audio-b2", "movie-a"}}
	for _, group := range []bool{false, true} {
		trace.reset()
		complete, err := store.QueryLatest(ctx, query, group)
		if err != nil || len(complete) != 2 {
			t.Fatalf("latest fixture: %+v, %v", complete, err)
		}
		attachments, selection := userDataOutputQueryTrace(trace)
		if attachments == 0 || len(selection) == 0 {
			t.Fatal("latest fixture did not exercise both attachment and source-state filtering")
		}
		for index := range complete {
			if complete[index].Item.UserData == nil {
				t.Fatal("complete latest result lost its user state")
			}
			complete[index].Item.UserData = nil
		}
		query.Projection.UserDataDisabled = true
		trace.reset()
		narrow, err := store.QueryLatest(ctx, query, group)
		if err != nil || !reflect.DeepEqual(complete, narrow) {
			t.Fatalf("disabled latest user data changed grouping or other output: %+v, %v", narrow, err)
		}
		remaining, narrowSelection := userDataOutputQueryTrace(trace)
		if remaining != 0 || !reflect.DeepEqual(selection, narrowSelection) {
			t.Fatal("latest projection retained attachment SQL or changed source filtering")
		}
		query.Projection.UserDataDisabled = false
	}
}

func TestUserDataProjectionKeepsNextUpAndEpisodeQueueSelection(t *testing.T) {
	ctx, pool, fixture, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, fixture, root, "userdata-output-next-up")
	series := nextUpSeries(t, ctx, pool, collection, "Projection", []int{1}, 3)
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1)
		WHERE id=ANY($2::text[])`, media.CurrentProbeVersion, series.Episodes[1]); err != nil {
		t.Fatal(err)
	}
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][0], time.Now())
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: series.Episodes[1][1], IsFavorite: true})
	store, trace := countItemsTestStore(t, ctx, fixture)
	assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
		result, err := store.NextUp(ctx, NextUpQuery{UserID: userID, SeriesID: series.ID, Limit: 1, Projection: projection})
		if err == nil && (len(result.Items) != 1 || result.Items[0].ID != series.Episodes[1][1]) {
			t.Fatalf("next-up projection changed the selected unwatched episode: %+v", result)
		}
		return result, err
	}, true)
	assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
		result, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID, projection)
		if err == nil && (len(result.Items) != 3 || result.Items[0].ID != series.Episodes[1][0]) {
			t.Fatalf("episode queue projection lost the replayable episode: %+v", result)
		}
		return result, err
	}, true)
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NextUp(ctx, NextUpQuery{UserID: userID, Projection: QueryProjection{UserDataDisabled: true}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled output admitted a disabled account: %v", err)
	}
}

func TestUserDataProjectionKeepsSimilarAndInstantMixCandidates(t *testing.T) {
	ctx, fixture, _, _ := musicDiscoveryFixture(t)
	store, trace := countItemsTestStore(t, ctx, fixture)
	userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: "mix-sibling", IsFavorite: true, PlayCount: 2})
	yes := true
	assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
		result, err := store.QueryInstantMix(ctx, MusicMixSeed{Kind: "Song", ID: "mix-seed"}, InstantMixQuery{Query: Query{
			UserID: "restricted", Limit: 10, IsFavorite: &yes, Projection: projection,
		}})
		if err == nil && (len(result.Items) != 1 || result.Items[0].ID != "mix-sibling") {
			t.Fatalf("instant mix projection changed favorite candidates: %+v", result)
		}
		return result, err
	}, true)
	assertUserDataOutputProjection(t, trace, func(projection QueryProjection) (ItemResult, error) {
		result, err := store.QuerySimilar(ctx, "mix-seed", SimilarQuery{Query: Query{
			UserID: "restricted", Limit: 10, SortBy: "PlayCount", SortOrder: "Descending", Projection: projection,
		}, ExplicitSort: true})
		if err == nil && (len(result.Items) == 0 || result.Items[0].ID != "mix-sibling") {
			t.Fatalf("similar projection lost state-based candidate ordering: %+v", result)
		}
		return result, err
	}, true)
}
