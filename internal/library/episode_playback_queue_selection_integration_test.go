package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func assertEpisodeQueueQueries(t *testing.T, trace *itemCountTracer, projections int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	selections, pages := 0, 0
	for _, statement := range trace.statements {
		if strings.Contains(statement, "series_tree AS (") {
			selections++
		}
		if strings.Contains(statement, "SELECT i.id, i.library_id, COALESCE(i.parent_id, '')") {
			pages++
		}
	}
	if selections != 1 || pages != projections {
		t.Fatalf("episode queue used %d eligibility queries and %d item projections, want 1 and %d", selections, pages, projections)
	}
}

func TestEpisodePlaybackQueueSelectsOnceAtEmptyAndCapacityBoundaries(t *testing.T) {
	ctx, pool, fixture, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, fixture, root, "episode-queue-selection-limit")
	series := nextUpSeries(t, ctx, pool, collection, "Bounded", []int{1}, 1)
	first := series.Episodes[1][0]
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1) WHERE id=$2`, media.CurrentProbeVersion, first); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number,parent_index_number,media)
		SELECT source.id || '-' || n,source.library_id,source.parent_id,'Episode ' || n,'Episode ' || n,
			'Episode',false,n,1,source.media FROM items source CROSS JOIN generate_series(2,$2::int) n WHERE source.id=$1`, first, MaxEpisodePlaybackQueueItems+1); err != nil {
		t.Fatal(err)
	}
	store, trace := countItemsTestStore(t, ctx, fixture)
	if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID); !errors.Is(err, ErrEpisodePlaybackQueueLimit) {
		t.Fatalf("oversized queue = %v, want the existing capacity error", err)
	}
	assertEpisodeQueueQueries(t, trace, 0)
	if _, err := pool.Exec(ctx, `UPDATE items SET media=NULL WHERE id=$1`, first+"-"+strconv.Itoa(MaxEpisodePlaybackQueueItems+1)); err != nil {
		t.Fatal(err)
	}
	// Unplayable entries sort before every usable episode. Applying the key
	// limit before eligibility would incorrectly truncate this valid queue.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number,parent_index_number)
		SELECT source.id || '-unavailable-' || n,source.library_id,source.parent_id,'Unavailable','Unavailable',
			'Episode',false,0,1 FROM items source CROSS JOIN generate_series(1,32) n WHERE source.id=$1`, first); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	result, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID)
	if err != nil || result.TotalRecordCount != MaxEpisodePlaybackQueueItems || len(result.Items) != MaxEpisodePlaybackQueueItems {
		t.Fatalf("exact-capacity queue has total=%d items=%d error=%v", result.TotalRecordCount, len(result.Items), err)
	}
	for index, item := range result.Items {
		want := first
		if index != 0 {
			want += "-" + strconv.Itoa(index+1)
		}
		if item.ID != want {
			t.Fatalf("queue position %d = %q, want %q", index, item.ID, want)
		}
	}
	assertEpisodeQueueQueries(t, trace, 1)
	if _, err := pool.Exec(ctx, `UPDATE items SET media=NULL WHERE library_id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	result, err = store.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID)
	if err != nil || result.TotalRecordCount != 0 || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty queue = %+v, %v", result, err)
	}
	assertEpisodeQueueQueries(t, trace, 0)
}

type episodeQueueSelectionTraceKey struct{}

type episodeQueueSelectionTrace struct {
	itemCountTracer
	writer         *pgxpool.Pool
	userID, itemID string
	changed        bool
	err            error
}

func (trace *episodeQueueSelectionTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.itemCountTracer.TraceQueryStart(ctx, connection, data)
	if strings.Contains(data.SQL, "series_tree AS (") {
		return context.WithValue(ctx, episodeQueueSelectionTraceKey{}, true)
	}
	return ctx
}

func (trace *episodeQueueSelectionTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if trace.changed || data.Err != nil || ctx.Value(episodeQueueSelectionTraceKey{}) != true {
		return
	}
	trace.changed = true
	_, trace.err = trace.writer.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, trace.userID)
	if trace.err == nil {
		_, trace.err = trace.writer.Exec(ctx, `DELETE FROM items WHERE id=$1`, trace.itemID)
	}
}

func TestEpisodePlaybackQueueKeepsSelectedProjectionInAuthorizedSnapshot(t *testing.T) {
	ctx, pool, fixture, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, fixture, root, "episode-queue-selection-snapshot")
	series := nextUpSeries(t, ctx, pool, collection, "Snapshot", []int{0, 1}, 2)
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1) WHERE library_id=$2 AND media IS NOT NULL`, media.CurrentProbeVersion, collection.ID); err != nil {
		t.Fatal(err)
	}
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: series.Episodes[0][0], IsFavorite: true, Played: true, PlayCount: 2})
	baseline, err := fixture.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID)
	if err != nil || len(baseline.Items) != 4 {
		t.Fatalf("snapshot baseline has %d items: %v", len(baseline.Items), err)
	}
	trace := &episodeQueueSelectionTrace{writer: pool, userID: userID, itemID: series.Episodes[0][0]}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result, err := (&Store{pool: reader}).EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID)
	if err != nil || !trace.changed || trace.err != nil {
		t.Fatalf("concurrent snapshot change failed: query=%v changed=%t mutation=%v", err, trace.changed, trace.err)
	}
	if !reflect.DeepEqual(result, baseline) {
		t.Fatal("queue projection or attachments escaped the authorization and eligibility snapshot")
	}
	assertEpisodeQueueQueries(t, &trace.itemCountTracer, 1)
	if _, err := fixture.EpisodePlaybackQueue(ctx, Subject{UserID: userID}, series.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("next queue snapshot retained disabled-user authority: %v", err)
	}
}
