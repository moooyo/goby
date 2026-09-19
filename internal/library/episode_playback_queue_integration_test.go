package library

import (
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestEpisodePlaybackQueueRetainsFullOrderedReplayableSeriesAndCurrentAuthority(t *testing.T) {
	ctx, pool, store, root, otherUser := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible := nextUpLibrary(t, ctx, store, root, "episode-queue-visible")
	private := nextUpLibrary(t, ctx, store, root, "episode-queue-private")
	series := nextUpSeries(t, ctx, pool, visible, "Full", []int{1, 2}, 60)
	hidden := nextUpSeries(t, ctx, pool, private, "Hidden", []int{1}, 1)
	nested := nextUpSeries(t, ctx, pool, visible, "Nested", []int{1}, 1)
	if _, err := pool.Exec(ctx, `UPDATE items SET parent_id=$2 WHERE id=$1`, nested.ID, series.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1) WHERE media IS NOT NULL`, media.CurrentProbeVersion); err != nil {
		t.Fatal(err)
	}
	viewer := "episode-queue-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, false, []string{visible.ID})
	nextUpPlayed(t, ctx, pool, viewer, series.Episodes[1][0], time.Now())
	nextUpPlayed(t, ctx, pool, otherUser, series.Episodes[1][1], time.Now())
	result, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID)
	if err != nil || len(result.Items) != 120 || result.TotalRecordCount != 120 {
		t.Fatalf("complete episode queue was truncated: count=%d, items=%d, error=%v", result.TotalRecordCount, len(result.Items), err)
	}
	want := append(append([]string{}, series.Episodes[1]...), series.Episodes[2]...)
	for index, item := range result.Items {
		if item.ID != want[index] || !item.CanPlay {
			t.Fatal("queue ordering, nested-series isolation, or playback projection changed")
		}
	}
	if result.Items[0].UserData == nil || !result.Items[0].UserData.Played || result.Items[1].UserData == nil || result.Items[1].UserData.Played {
		t.Fatal("queue excluded explicit replay or inherited another user's history")
	}
	application := seedCatalogApplicationKey(t, ctx, pool, "episode-queue-key", true)
	applicationQueue, err := store.EpisodePlaybackQueue(ctx, application, series.ID)
	if err != nil || len(applicationQueue.Items) != 120 || applicationQueue.Items[0].UserData != nil {
		t.Fatal("userless application queue was denied or acquired personal playback state")
	}
	if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, hidden.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("queue exposed an unauthorized series")
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=NULL WHERE id=$1`, series.Episodes[2][59]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || '{"ProbeVersion":0}'::jsonb WHERE id=$1`, series.Episodes[2][58]); err != nil {
		t.Fatal(err)
	}
	result, err = store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID)
	if err != nil || result.TotalRecordCount != 118 || result.Items[len(result.Items)-1].ID != series.Episodes[2][57] {
		t.Fatal("queue retained missing or obsolete playback source facts")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("queue reused revoked playback authority")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":true,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("queue reused a revoked library grant")
	}
}

func TestEpisodePlaybackQueueRejectsOversizedSeriesWithoutTruncating(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := nextUpLibrary(t, ctx, store, root, "episode-queue-limit")
	series := nextUpSeries(t, ctx, pool, collection, "Bounded", []int{1}, 1)
	if _, err := pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('ProbeVersion',$1::int,'FileChangeTimeNs',1) WHERE id=$2`, media.CurrentProbeVersion, series.Episodes[1][0]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number,parent_index_number,media)
		SELECT source.id || '-' || n,source.library_id,source.parent_id,'Episode ' || n,'Episode ' || n,
			'Episode',false,n,1,source.media FROM items source CROSS JOIN generate_series(2,$2::int) n WHERE source.id=$1`, series.Episodes[1][0], MaxEpisodePlaybackQueueItems+1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EpisodePlaybackQueue(ctx, Subject{UserID: viewer}, series.ID); !errors.Is(err, ErrEpisodePlaybackQueueLimit) {
		t.Fatalf("oversized queue was silently truncated or accepted: %v", err)
	}
}
