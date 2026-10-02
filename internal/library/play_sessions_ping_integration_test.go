package library

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestStorePlaybackPingReadsUserDataWithoutWaitingForUpdateLock(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 3)
	for index, selector := range []string{"canonical ID", "client reference", "item and source"} {
		t.Run(selector, func(t *testing.T) {
			itemID := ids[index]
			var prepared PlaySession
			report := PlaybackReport{Event: "Ping"}
			if selector == "client reference" {
				const reference = "ping-lock-client-reference"
				prepared = playReferencePrepare(t, ctx, store, owner, itemID, reference)
				report.PlaySessionID = reference
			} else {
				prepared = playSessionPrepare(t, ctx, store, owner, itemID, "")
				if selector == "canonical ID" {
					report.PlaySessionID = prepared.ID
				} else {
					report.ItemID, report.MediaSourceID = itemID, media.SourceID(itemID)
				}
			}
			paused, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{
				PlaySessionID: prepared.ID, Event: "Progress", IsPaused: true,
				PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
			})
			if _, err := pool.Exec(ctx, `UPDATE user_item_data SET is_favorite = true,
				rating = 8.5, likes = true, hide_from_resume = true WHERE user_id = $1 AND item_id = $2`, owner.UserID, itemID); err != nil {
				t.Fatalf("seed the committed Ping user data snapshot: %v", err)
			}
			want, err := scanUserData(pool.QueryRow(ctx, "SELECT "+userDataColumns+" FROM user_item_data WHERE user_id = $1 AND item_id = $2", owner.UserID, itemID))
			if err != nil {
				t.Fatalf("read the committed Ping user data snapshot: %v", err)
			}
			beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin the Ping user data barrier: %v", err)
			}
			defer rollback(blocker)
			var locked string
			if err := blocker.QueryRow(ctx, `SELECT item_id FROM user_item_data
				WHERE user_id = $1 AND item_id = $2 FOR UPDATE`, owner.UserID, itemID).Scan(&locked); err != nil {
				t.Fatalf("lock the Ping user data row: %v", err)
			}
			if _, err := blocker.Exec(ctx, `UPDATE user_item_data SET playback_position_ticks = $3,
				play_count = 99, is_favorite = false WHERE user_id = $1 AND item_id = $2`,
				owner.UserID, itemID, 300*media.TicksPerSecond); err != nil {
				t.Fatalf("write an uncommitted Ping user data snapshot: %v", err)
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			ping, data, err := store.ReportPlayback(pingCtx, owner, report)
			if err != nil {
				t.Fatalf("Ping waited for the held user data lock or failed: %v", err)
			}
			if ping.ID != paused.ID || ping.State != "Paused" || ping.PositionTicks != paused.PositionTicks ||
				ping.counted != paused.counted || !ping.ExpiresAt.After(paused.ExpiresAt) {
				t.Errorf("Ping failed to refresh the existing paused playback lease: %+v", ping)
			}
			userDataAssertValue(t, data, want)
			afterPlay, afterData := playSessionSnapshot(t, ctx, pool, prepared)
			if afterPlay == beforePlay || afterData != beforeData {
				t.Error("Ping did not refresh playback or changed committed user data")
			}
		})
	}
}

func TestStorePlaybackPingDoesNotInitializeMissingUserData(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 8)
	index := 0
	for _, selector := range []string{"canonical ID", "item and source"} {
		for _, state := range []string{"Prepared", "Stopped", "Expired", "expired deadline"} {
			t.Run(selector+"/"+state, func(t *testing.T) {
				itemID := ids[index]
				index++
				prepared := playSessionPrepare(t, ctx, store, owner, itemID, "")
				wantState := state
				if state == "Stopped" {
					prepared, _ = playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped"})
				} else if state == "Expired" {
					if _, err := pool.Exec(ctx, `UPDATE play_sessions SET state = 'Expired',
						stopped_at = clock_timestamp(), expires_at = clock_timestamp() WHERE id = $1`, prepared.ID); err != nil {
						t.Fatalf("expire the terminal Ping fixture: %v", err)
					}
				} else if state == "expired deadline" {
					wantState = "Expired"
					if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", prepared.ID); err != nil {
						t.Fatalf("end the Ping fixture lease: %v", err)
					}
				}
				if _, err := pool.Exec(ctx, "DELETE FROM user_item_data WHERE user_id = $1 AND item_id = $2", owner.UserID, itemID); err != nil {
					t.Fatalf("remove the Ping fixture user data: %v", err)
				}
				var before string
				if err := pool.QueryRow(ctx, "SELECT to_jsonb(p)::text FROM play_sessions p WHERE id = $1", prepared.ID).Scan(&before); err != nil {
					t.Fatalf("snapshot the Ping fixture playback: %v", err)
				}
				report := PlaybackReport{Event: "Ping"}
				if selector == "canonical ID" {
					report.PlaySessionID = prepared.ID
				} else {
					report.ItemID, report.MediaSourceID = itemID, media.SourceID(itemID)
				}
				ping, data := playSessionReport(t, ctx, store, owner, report)
				if ping.ID != prepared.ID || ping.State != wantState {
					t.Errorf("Ping changed playback identity or returned the wrong state: %+v", ping)
				}
				userDataAssertValue(t, data, UserData{ItemID: itemID})
				var rows int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data WHERE user_id = $1 AND item_id = $2", owner.UserID, itemID).Scan(&rows); err != nil || rows != 0 {
					t.Errorf("Ping initialized missing user data: rows = %d, error = %v", rows, err)
				}
				if state == "Stopped" || state == "Expired" {
					var after string
					if err := pool.QueryRow(ctx, "SELECT to_jsonb(p)::text FROM play_sessions p WHERE id = $1", prepared.ID).Scan(&after); err != nil {
						t.Fatalf("snapshot the terminal Ping result: %v", err)
					}
					if after != before {
						t.Error("Ping changed terminal playback state")
					}
				}
			})
		}
	}
}

func TestStorePlaybackPingUnknownSessionsDoNotCreateState(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	for _, playID := range []string{"play_unknown-ping", "unknown-ping-reference", ""} {
		t.Run(playID, func(t *testing.T) {
			if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
				PlaySessionID: playID, ItemID: ids[0], MediaSourceID: media.SourceID(ids[0]), Event: "Ping",
			}); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown Ping = %v, want ErrNotFound", err)
			}
			var plays, dataRows int
			if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM play_sessions), (SELECT count(*) FROM user_item_data)").Scan(&plays, &dataRows); err != nil || plays != 0 || dataRows != 0 {
				t.Errorf("unknown Ping created state: plays = %d, user data = %d, error = %v", plays, dataRows, err)
			}
		})
	}
}

func TestApplicationKeyPlaybackPingReturnsNoUserData(t *testing.T) {
	ctx, pool, store, _, ids := playSessionFixture(t, 1)
	owner := applicationPlaybackOwnerFixture(t, ctx, pool, "ping")
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	for _, event := range []string{"Ping", "Stopped", "Ping"} {
		play, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event})
		if play.ID != prepared.ID || !reflect.DeepEqual(data, UserData{}) {
			t.Errorf("application key %s acquired user data: %+v, %+v", event, play, data)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("application key Ping created user data: rows = %d, error = %v", rows, err)
	}
}
