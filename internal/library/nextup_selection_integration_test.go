package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestNextUpCountOnlyAndEmptyPagesSkipItemProjection(t *testing.T) {
	ctx, pool, fixture, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, fixture, root, "next-up-count-only")
	series := nextUpSeries(t, ctx, pool, collection, "Count", []int{1, 2}, 2)
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][0], time.Now())
	// A count and an empty page must not decode candidate media. This malformed
	// field makes an accidental item projection fail without changing membership.
	if _, err := pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":"invalid"}'::jsonb
		WHERE id=ANY($1::text[])`, append(series.Episodes[1][1:], series.Episodes[2]...)); err != nil {
		t.Fatal(err)
	}
	store, trace := countItemsTestStore(t, ctx, fixture)
	for _, test := range []struct {
		name  string
		query NextUpQuery
		total int
	}{
		{name: "global count", query: NextUpQuery{CountOnly: true}, total: 1},
		{name: "series count", query: NextUpQuery{SeriesID: series.ID, CountOnly: true}, total: 3},
		{name: "count ignores offset", query: NextUpQuery{SeriesID: series.ID, StartIndex: 100, CountOnly: true}, total: 3},
		{name: "parent retains history", query: NextUpQuery{SeriesID: series.ID, ParentID: series.Seasons[2], CountOnly: true}, total: 2},
		{name: "exhausted page", query: NextUpQuery{SeriesID: series.ID, StartIndex: 100, Limit: 1}, total: 3},
		{name: "empty candidates", query: NextUpQuery{SeriesID: series.ID, ParentID: series.Episodes[1][0], Limit: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID = userID
			trace.reset()
			result, err := store.NextUp(ctx, query)
			if err != nil || result.TotalRecordCount != test.total || result.Items == nil || len(result.Items) != 0 {
				t.Fatalf("next-up count or empty page = %+v, %v; want total %d and empty Items", result, err, test.total)
			}
			trace.assertCountWithoutPage(t)
		})
	}
	if _, err := store.NextUp(ctx, NextUpQuery{UserID: userID, SeriesID: series.ID, CountOnly: true, ParentID: "missing-next-up-parent"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("count-only query accepted a missing parent: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NextUp(ctx, NextUpQuery{UserID: userID, CountOnly: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("count-only query accepted a disabled user: %v", err)
	}
}

func TestNextUpSelectsCandidatePageOnceAndPreservesProjection(t *testing.T) {
	ctx, pool, fixture, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, fixture, root, "next-up-single-selection")
	series := nextUpSeries(t, ctx, pool, collection, "Selection", []int{0, 1}, 3)
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[0][0], time.Now())
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: series.Episodes[1][1], IsFavorite: true})
	store, trace := countItemsTestStore(t, ctx, fixture)
	query := NextUpQuery{UserID: userID, SeriesID: series.ID, ParentID: series.Seasons[1], StartIndex: 1, Limit: 2}
	result, err := store.NextUp(ctx, query)
	if err != nil || result.TotalRecordCount != 3 || len(result.Items) != 2 {
		t.Fatalf("next-up candidate page = %+v, %v", result, err)
	}
	trace.mu.Lock()
	statements := append([]string(nil), trace.statements...)
	trace.mu.Unlock()
	var statement string
	selections := 0
	for _, candidate := range statements {
		if strings.Contains(candidate, "series_tree AS (") {
			selections++
			statement = candidate
		}
	}
	if selections != 1 {
		t.Fatalf("next-up traversed candidate history %d times, want once", selections)
	}
	tx, access, err := fixture.beginSubjectRead(ctx, Subject{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	args := []any{query.UserID, access.all, access.folders, query.SeriesID, query.ParentID, query.Limit, query.StartIndex}
	// Reconstruct the previous wide page query as the equivalence baseline.
	rows, err := tx.Query(ctx, nextUpScopeSQL(access)+nextUpCandidateSQL()+`SELECT `+access.scopeSQL(nextUpItemColumns)+`
		FROM next_up candidate JOIN items i ON i.id=candidate.id AND i.library_id=candidate.library_id
		WHERE candidate.candidate_rank=1
		ORDER BY candidate.last_activity DESC NULLS LAST, lower(candidate.series_sort_name), candidate.series_id, candidate.episode_order, i.id
		LIMIT $6 OFFSET $7`, args...)
	if err != nil {
		t.Fatal(err)
	}
	baseline := make([]Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		item.CanPlay = access.canPlay
		baseline = append(baseline, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := attachUserData(ctx, tx, userID, baseline); err != nil {
		t.Fatal(err)
	}
	if err := attachSubtitles(ctx, tx, baseline); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Items, baseline) {
		t.Fatal("candidate key selection changed the complete item projection or page order")
	}
	assertNextUpCandidateEvaluation(t, ctx, tx, statement, args)
}

func assertNextUpCandidateEvaluation(t *testing.T, ctx context.Context, tx pgx.Tx, statement string, args []any) {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, TIMING OFF, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Subplan string     `json:"Subplan Name"`
		Loops   float64    `json:"Actual Loops"`
		Plans   []planNode `json:"Plans"`
	}
	var documents []struct{ Plan planNode }
	if err := json.Unmarshal(raw, &documents); err != nil || len(documents) != 1 {
		t.Fatalf("decode next-up selection plan: %v", err)
	}
	definitions := 0
	var visit func(planNode)
	visit = func(node planNode) {
		if node.Subplan == "CTE selected_next_up" {
			definitions++
			if node.Loops != 1 {
				t.Errorf("candidate materialization ran %g times, want once", node.Loops)
			}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(documents[0].Plan)
	if definitions != 1 {
		t.Fatalf("candidate plan contains %d materializations, want one", definitions)
	}
}

func TestNextUpBrowseProjectionLeavesDomainMediaComplete(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, store, root, "next-up-browse-media")
	series := nextUpSeries(t, ctx, pool, collection, "Projection", []int{1}, 2)
	nextUpPlayed(t, ctx, pool, userID, series.Episodes[1][0], time.Now())
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media||'{"VideoSeekIndexes":[{"version":1,"entries":[{"pts":0,"dts":0}]}]}'::jsonb
		WHERE id=$1`, series.Episodes[1][1]); err != nil {
		t.Fatal(err)
	}
	query := NextUpQuery{UserID: userID, SeriesID: series.ID}
	complete := nextUpQuery(t, ctx, store, query)
	if len(complete.Items) != 1 || complete.Items[0].Media == nil || len(complete.Items[0].Media.VideoSeekIndexes) != 1 {
		t.Fatal("default NextUp query lost private domain media facts")
	}
	query.Projection.Browse = true
	browse := nextUpQuery(t, ctx, store, query)
	complete.Items[0].Media.VideoSeekIndexes = nil
	if !reflect.DeepEqual(complete, browse) {
		t.Fatal("NextUp browse projection changed public facts or retained private seek indexes")
	}
}
