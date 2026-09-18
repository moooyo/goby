package library

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestDynamicPlaybackUsesIndependentClockWithoutChangingLocalUserData(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	itemID := ids[0]
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id,item_id,playback_position_ticks,play_count,is_favorite,played)
		VALUES ($1,$2,$3,7,true,false)`, owner.UserID, itemID, 45*media.TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, owner.UserID, itemID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareDynamicPlayback(ctx, owner, itemID, media.SourceID(itemID), "")
	if err != nil || !prepared.IsDynamic || prepared.DurationTicks != 0 || prepared.PositionTicks != 0 {
		t.Fatalf("dynamic preparation: %+v, %v", prepared, err)
	}
	started, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	if !started.IsDynamic || started.StartedAt == nil {
		t.Fatal("dynamic session did not enter its own playback lifecycle")
	}
	if _, err := store.PreparePlayback(ctx, owner, itemID, media.SourceID(itemID), prepared.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("active dynamic session changed to local-file semantics: %v", err)
	}
	position := 2 * playbackTestDuration
	for _, event := range []string{"Progress", "Ping", "Stopped"} {
		play, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event, PositionTicks: &position})
		if !play.IsDynamic || play.DurationTicks != 0 || play.PositionTicks != position {
			t.Fatalf("%s clamped a stream to local-file duration: %+v", event, play)
		}
		var after string
		if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, owner.UserID, itemID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s modified local-file resume, watched state, or history", event)
		}
	}
	var dynamic bool
	if err := pool.QueryRow(ctx, `SELECT is_dynamic FROM play_sessions WHERE id=$1`, prepared.ID).Scan(&dynamic); err != nil || !dynamic {
		t.Fatalf("dynamic source mode was not durable: %v", err)
	}
}

func TestDynamicPlaybackStillRequiresCurrentPolicyAndOwner(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared, err := store.PrepareDynamicPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), "")
	if err != nil {
		t.Fatal(err)
	}
	foreign := playSessionOwnerFixture(t, ctx, pool, owner.UserID, "other-device")
	if _, _, err := store.ReportPlayback(ctx, foreign, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"}); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign stream report admitted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, owner.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress"}); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked stream report admitted: %v", err)
	}
}

func TestDynamicPlaybackDoesNotCreateLocalUserDataOrChangePreparedSourceMode(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 2)
	local := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if _, err := store.PrepareDynamicPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), local.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("prepared local session silently became dynamic: %v", err)
	}
	dynamic, err := store.PrepareDynamicPlayback(ctx, owner, ids[1], media.SourceID(ids[1]), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[1], media.SourceID(ids[1]), dynamic.ID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("prepared dynamic session silently became local: %v", err)
	}
	for _, report := range []PlaybackReport{
		{PlaySessionID: dynamic.ID, Event: "Started"},
		{ItemID: ids[1], MediaSourceID: media.SourceID(ids[1]), Event: "Progress", PositionTicks: playSessionPosition(playbackTestDuration * 3)},
		{PlaySessionID: dynamic.ID, Event: "Stopped"},
	} {
		play, _ := playSessionReport(t, ctx, store, owner, report)
		if !play.IsDynamic {
			t.Fatal("dynamic mode was lost")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id=$1 AND item_id=$2`, owner.UserID, ids[1]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stream reports created local-file user data: count=%d, error=%v", count, err)
	}
}
