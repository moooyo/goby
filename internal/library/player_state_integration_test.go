package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestStorePlayerStatePartialUpdatesAndExplicitZeroValues(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if !reflect.DeepEqual(prepared.PlayerState, PlayerState{}) {
		t.Fatal("preparing a playback session invented player hints")
	}
	full := playerStateFullFixture()
	started, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started", PlayerState: &full})
	if !reflect.DeepEqual(started.PlayerState, full) || data.PlayCount != 1 {
		t.Fatalf("start did not preserve player state and count: state=%+v data=%+v", started.PlayerState, data)
	}
	partial := PlayerStateUpdate{VolumeLevel: playerStatePtr(0), IsMuted: playerStatePtr(false)}
	progress, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond), IsPaused: true, PlayerState: &partial})
	want := full
	want.VolumeLevel, want.IsMuted = partial.VolumeLevel, partial.IsMuted
	if !reflect.DeepEqual(progress.PlayerState, want) || progress.State != "Paused" || data.PlayCount != 1 || data.PlaybackPositionTicks != 120*media.TicksPerSecond {
		t.Fatalf("partial hints lost prior fields or changed authoritative behavior: state=%+v data=%+v", progress, data)
	}
	zero := PlayerStateUpdate{CanSeek: playerStatePtr(false), Shuffle: playerStatePtr(false), AudioStreamIndex: playerStatePtr(-1),
		SubtitleStreamIndex: playerStatePtr(-1), SubtitleOffset: playerStatePtr(0), PlaybackRate: playerStatePtr(1.0), RepeatMode: playerStatePtr("RepeatNone")}
	progress, _ = playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", PlayerState: &zero})
	want.CanSeek, want.Shuffle = zero.CanSeek, zero.Shuffle
	want.AudioStreamIndex, want.SubtitleStreamIndex, want.SubtitleOffset = zero.AudioStreamIndex, zero.SubtitleStreamIndex, zero.SubtitleOffset
	want.PlaybackRate, want.RepeatMode = zero.PlaybackRate, zero.RepeatMode
	if !reflect.DeepEqual(progress.PlayerState, want) {
		t.Fatalf("explicit false/zero/-1 values were not preserved: %+v", progress.PlayerState)
	}
	for _, report := range []PlaybackReport{
		{PlaySessionID: prepared.ID, Event: "Progress"},
		{PlaySessionID: prepared.ID, Event: "Progress", PlayerState: &PlayerStateUpdate{}},
		{PlaySessionID: prepared.ID, Event: "Ping", PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(99)}},
	} {
		current, _ := playSessionReport(t, ctx, store, owner, report)
		if !reflect.DeepEqual(current.PlayerState, want) {
			t.Errorf("omitted state or Ping changed stored hints: %+v", current.PlayerState)
		}
	}
	got, err := store.GetPlaybackSession(ctx, owner, prepared.ID)
	if err != nil || !reflect.DeepEqual(got.PlayerState, want) {
		t.Fatalf("GetPlaybackSession lost persisted hints: %+v, %v", got.PlayerState, err)
	}
	listed, err := store.ListPlaybackSessions(ctx, owner.UserID, false)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].PlayerState, want) {
		t.Fatalf("ListPlaybackSessions lost persisted hints: %+v, %v", listed, err)
	}
	var raw string
	if err := pool.QueryRow(ctx, "SELECT player_state::text FROM play_sessions WHERE id = $1", prepared.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"PositionTicks", "IsPaused", "ItemId", "MediaSourceId", "UserId", "Capabilities"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("player hints included authoritative field %s", forbidden)
		}
	}
}

func TestStorePlayerStateInvalidUpdateIsAtomic(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	full := playerStateFullFixture()
	started, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started", PlayerState: &full})
	beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, started)
	for name, invalid := range playerStateInvalidUpdates() {
		t.Run(name, func(t *testing.T) {
			invalid.CanSeek = playerStatePtr(false)
			if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", IsPaused: true,
				PositionTicks: playSessionPosition(590 * media.TicksPerSecond), PlayerState: &invalid}); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("invalid hint error=%v, want ErrInvalidInput", err)
			}
		})
	}
	afterPlay, afterData := playSessionSnapshot(t, ctx, pool, started)
	if beforePlay != afterPlay || beforeData != afterData {
		t.Error("invalid player state partially changed hints, position, count, or timestamps")
	}
}

func TestStorePlayerStateKeepsTerminalAndRepeatedStartImmutable(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	initial := PlayerStateUpdate{VolumeLevel: playerStatePtr(25), PlayMethod: playerStatePtr("DirectPlay")}
	started, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started", PlayerState: &initial})
	repeated, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started", PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(99)}})
	if !reflect.DeepEqual(repeated, started) {
		t.Error("duplicate Started changed player state or playback counters")
	}
	finalUpdate := PlayerStateUpdate{VolumeLevel: playerStatePtr(30), RepeatMode: playerStatePtr("RepeatOne")}
	stopped, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond), PlayerState: &finalUpdate})
	want := initial
	want.VolumeLevel, want.RepeatMode = finalUpdate.VolumeLevel, finalUpdate.RepeatMode
	if stopped.State != "Stopped" || !reflect.DeepEqual(stopped.PlayerState, want) || data.PlayCount != 1 {
		t.Fatalf("first Stop did not retain its final hints: %+v, %+v", stopped, data)
	}
	beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, stopped)
	for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
		current, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: stopped.ID, Event: event,
			PositionTicks: playSessionPosition(590 * media.TicksPerSecond), PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(90)}})
		if current.State != "Stopped" || !reflect.DeepEqual(current.PlayerState, want) {
			t.Errorf("terminal %s changed player state: %+v", event, current)
		}
	}
	afterPlay, afterData := playSessionSnapshot(t, ctx, pool, stopped)
	if beforePlay != afterPlay || beforeData != afterData {
		t.Error("terminal player reports changed persistent rows")
	}
	if sessions, err := store.ListPlaybackSessions(ctx, owner.UserID, false); err != nil || len(sessions) != 0 {
		t.Errorf("terminal player state remained in active projection: %+v, %v", sessions, err)
	}
	next := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: next.ID, Event: "Started", PlayerState: &initial})
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", next.ID); err != nil {
		t.Fatal(err)
	}
	expired, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: next.ID, Event: "Progress", PlayerState: &finalUpdate})
	if expired.State != "Expired" || !reflect.DeepEqual(expired.PlayerState, initial) {
		t.Errorf("expiring a session accepted newer client hints: %+v", expired)
	}
	beforePlay, beforeData = playSessionSnapshot(t, ctx, pool, expired)
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: expired.ID, Event: "Stopped", PlayerState: &finalUpdate})
	afterPlay, afterData = playSessionSnapshot(t, ctx, pool, expired)
	if beforePlay != afterPlay || beforeData != afterData {
		t.Error("expired player state was changed by a late report")
	}
}

func TestStorePlayerStateConcurrentPartialReportsDoNotLoseFields(t *testing.T) {
	ctx, _, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	want := playerStateFullFixture()
	updates := []PlayerStateUpdate{
		{CanSeek: want.CanSeek}, {IsMuted: want.IsMuted}, {VolumeLevel: want.VolumeLevel},
		{AudioStreamIndex: want.AudioStreamIndex}, {SubtitleStreamIndex: want.SubtitleStreamIndex},
		{PlayMethod: want.PlayMethod}, {RepeatMode: want.RepeatMode}, {PlaybackRate: want.PlaybackRate},
		{Shuffle: want.Shuffle}, {SubtitleOffset: want.SubtitleOffset},
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, len(updates))
	for _, update := range updates {
		go func(update PlayerStateUpdate) {
			<-start
			_, _, err := store.ReportPlayback(callCtx, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", PlayerState: &update})
			results <- err
		}(update)
	}
	close(start)
	for range updates {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("concurrent partial report failed: %v", err)
			}
		case <-callCtx.Done():
			t.Fatalf("concurrent partial reports did not finish: %v", callCtx.Err())
		}
	}
	got, err := store.GetPlaybackSession(ctx, owner, prepared.ID)
	if err != nil || !reflect.DeepEqual(got.PlayerState, want) {
		t.Fatalf("concurrent patches lost disjoint fields: %+v, %v", got.PlayerState, err)
	}
	data, err := store.GetUserData(ctx, owner.UserID, ids[0])
	if err != nil || data.PlayCount != 1 || data.PlaybackPositionTicks != 0 {
		t.Errorf("hint-only reports changed count or position: %+v, %v", data, err)
	}
}

func TestStorePlayerStateDatabaseRejectsUnsupportedOrInvalidValues(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	for _, encoded := range []string{
		`[]`, `null`, `{"CanSeek":null}`, `{"CanSeek":"true"}`, `{"VolumeLevel":101}`, `{"VolumeLevel":1.5}`,
		`{"AudioStreamIndex":-2}`, `{"SubtitleStreamIndex":2147483648}`, `{"SubtitleOffset":-2147483649}`,
		`{"PlayMethod":"unknown"}`, `{"RepeatMode":"RepeatOther"}`, `{"PlaybackRate":0}`, `{"PlaybackRate":10.1}`,
		`{"PlaybackRate":"NaN"}`, `{"Shuffle":1}`, `{"PositionTicks":1}`, `{"IsPaused":true}`, `{"ItemId":"other"}`,
	} {
		if _, err := pool.Exec(ctx, "UPDATE play_sessions SET player_state = $2::jsonb WHERE id = $1", prepared.ID, encoded); err == nil {
			t.Errorf("database accepted invalid or authoritative player-state data %s", encoded)
		}
	}
	got, err := store.GetPlaybackSession(ctx, owner, prepared.ID)
	if err != nil || !reflect.DeepEqual(got.PlayerState, PlayerState{}) {
		t.Fatalf("failed database updates changed stored projection: %+v, %v", got.PlayerState, err)
	}
}

func TestStoreNowPlayingSelectsLatestPlayingOrPausedBeforePrepared(t *testing.T) {
	ctx, _, store, owner, ids := playSessionFixture(t, 3)
	first := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: first.ID, Event: "Started"})
	second := playSessionPrepare(t, ctx, store, owner, ids[1], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: second.ID, Event: "Started", IsPaused: true,
		PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(40)}})
	prepared := playSessionPrepare(t, ctx, store, owner, ids[2], "")
	all, err := store.ListPlaybackSessions(ctx, owner.UserID, false)
	if err != nil || len(all) != 3 || all[0].ID != prepared.ID {
		t.Fatalf("general active listing no longer preserves prepared sessions: %+v, %v", all, err)
	}
	current, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, false, []string{owner.SessionID})
	if err != nil || len(current) != 1 || current[0].ID != second.ID || current[0].State != "Paused" ||
		current[0].PlayerState.VolumeLevel == nil || *current[0].PlayerState.VolumeLevel != 40 {
		t.Fatalf("new preparation hid the latest paused item: %+v, %v", current, err)
	}
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: first.ID, Event: "Progress",
		PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(60)}})
	current, err = store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, false, []string{owner.SessionID})
	if err != nil || len(current) != 1 || current[0].ID != first.ID || current[0].State != "Playing" ||
		current[0].PlayerState.VolumeLevel == nil || *current[0].PlayerState.VolumeLevel != 60 {
		t.Fatalf("new progress did not replace the older now-playing projection: %+v, %v", current, err)
	}
}

func TestStorePlayerStateAuthFilterPrecedesGlobalSessionLimit(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 32)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", owner.UserID); err != nil {
		t.Fatal(err)
	}
	target := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	target, _ = playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: target.ID, Event: "Started",
		PlayerState: &PlayerStateUpdate{VolumeLevel: playerStatePtr(65)}})
	if _, err := pool.Exec(ctx, `UPDATE play_sessions SET created_at = clock_timestamp() - interval '1 minute',
		updated_at = clock_timestamp() - interval '1 minute', expires_at = clock_timestamp() + interval '29 minutes' WHERE id = $1`, target.ID); err != nil {
		t.Fatal(err)
	}
	newerPrepared := playSessionPrepare(t, ctx, store, owner, ids[1], "")
	authIDs := []string{owner.SessionID}
	// Seed 288 newer sessions across three users and nine auth sessions, keeping
	// each user below 128 and each auth session at its normal 32-session cap.
	for userIndex := 0; userIndex < 3; userIndex++ {
		userID := fmt.Sprintf("player-state-list-user-%d", userIndex)
		libraryIntegrationUser(t, ctx, pool, userID, false, true, nil)
		for device := 0; device < 3; device++ {
			other := playSessionOwnerFixture(t, ctx, pool, userID, fmt.Sprintf("list-device-%d", device))
			authIDs = append(authIDs, other.SessionID)
			if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
				(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
				SELECT 'play_list_' || $2 || '_' || i.id, $1, $2, $3, i.id, 'mediasource_' || i.id,
				'Playing', $4, clock_timestamp() + interval '30 minutes' FROM items i WHERE i.id = ANY($5::text[])`,
				other.UserID, other.SessionID, other.DeviceID, playbackTestDuration, ids); err != nil {
				t.Fatalf("seed recent active-session view: %v", err)
			}
		}
	}
	all, err := store.ListPlaybackSessions(ctx, owner.UserID, true)
	if err != nil || len(all) != 256 {
		t.Fatalf("unfiltered active-session view lost its normal limit: count=%d error=%v", len(all), err)
	}
	for _, session := range all {
		if session.ID == target.ID {
			t.Fatal("fixture target was not outside the global top 256")
		}
	}
	filtered, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, true, []string{owner.SessionID, owner.SessionID})
	if err != nil || len(filtered) != 1 || filtered[0].ID != target.ID || !reflect.DeepEqual(filtered[0].PlayerState, target.PlayerState) {
		t.Fatalf("authentication filter was applied after the global limit: %+v, %v", filtered, err)
	}
	if filtered[0].ID == newerPrepared.ID {
		t.Error("newer preparation hid the authenticated client's active media")
	}
	grouped, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, true, authIDs)
	if err != nil || len(grouped) != len(authIDs) {
		t.Fatalf("many plays from one authentication session crowded out another: count=%d want=%d error=%v", len(grouped), len(authIDs), err)
	}
	seen := make(map[string]bool, len(grouped))
	for _, session := range grouped {
		if seen[session.AuthSessionID] || session.State == "Prepared" {
			t.Errorf("now-playing projection returned duplicate clients or prepared media: %+v", session)
		}
		seen[session.AuthSessionID] = true
	}
	if !seen[owner.SessionID] {
		t.Error("older target client was lost behind newer multi-play clients")
	}
	for _, selected := range [][]string{nil, {}, {"missing-auth-session"}} {
		filtered, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, true, selected)
		if err != nil || len(filtered) != 0 {
			t.Errorf("empty or unknown authentication filter widened its scope: %+v, %v", filtered, err)
		}
	}
	if filtered, err := store.ListNowPlayingSessionsForAuth(ctx, "player-state-list-user-0", false, []string{owner.SessionID}); err != nil || len(filtered) != 0 {
		t.Errorf("authentication filtering bypassed user scope: %+v, %v", filtered, err)
	}
	if _, err := store.ListNowPlayingSessionsForAuth(ctx, "player-state-list-user-0", true, []string{owner.SessionID}); !errors.Is(err, ErrForbidden) {
		t.Errorf("authentication filtering bypassed administrator permission: %v", err)
	}
	for _, invalid := range [][]string{make([]string, 257), {""}, {" "}, {"bad\x00id"}, {"bad\xffid"}, {strings.Repeat("a", 257)}} {
		if _, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, true, invalid); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid authentication filter returned %v", err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", owner.SessionID); err != nil {
		t.Fatal(err)
	}
	if filtered, err := store.ListNowPlayingSessionsForAuth(ctx, owner.UserID, true, []string{owner.SessionID}); err != nil || len(filtered) != 0 {
		t.Errorf("authentication filter bypassed current revocation: %+v, %v", filtered, err)
	}
}
