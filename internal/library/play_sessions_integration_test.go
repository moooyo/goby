package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

const playbackTestDuration int64 = 600 * media.TicksPerSecond

func playSessionOwnerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, deviceID string) PlaybackOwner {
	t.Helper()
	var tokenHash [32]byte
	if _, err := rand.Read(tokenHash[:]); err != nil {
		t.Fatalf("generate playback authentication fixture: %v", err)
	}
	owner := PlaybackOwner{UserID: userID, SessionID: "auth_" + hex.EncodeToString(tokenHash[:12]), DeviceID: deviceID}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, kind, device_id, expires_at)
		VALUES ($1, $2, $3, 'emby', $4, now() + interval '1 day')`,
		owner.SessionID, owner.UserID, tokenHash[:], owner.DeviceID); err != nil {
		t.Fatalf("insert playback authentication fixture: %v", err)
	}
	return owner
}

func playSessionFixture(t *testing.T, count int) (context.Context, *pgxpool.Pool, *Store, PlaybackOwner, []string) {
	t.Helper()
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := libraryIntegrationCreate(t, ctx, store, "Playback fixtures", "movies", allowedRoot)
	ids := make([]string, count)
	for index := range ids {
		id := fmt.Sprintf("%s-playback-%02d", library.ID, index)
		if _, err := pool.Exec(ctx, `INSERT INTO items
			(id, library_id, parent_id, name, sort_name, type, media)
			VALUES ($1, $2, $2, $3, lower($3), 'Movie', jsonb_build_object('DurationTicks', $4::bigint))`,
			id, library.ID, fmt.Sprintf("Playback Movie %02d", index), playbackTestDuration); err != nil {
			t.Fatalf("insert playable catalog fixture: %v", err)
		}
		ids[index] = id
	}
	owner := playSessionOwnerFixture(t, ctx, pool, userID, "playback-test-device")
	return ctx, pool, store, owner, ids
}

func playSessionPosition(value int64) *int64 {
	return &value
}

func playSessionPrepare(t *testing.T, ctx context.Context, store *Store, owner PlaybackOwner, itemID, currentID string) PlaySession {
	t.Helper()
	session, err := store.PreparePlayback(ctx, owner, itemID, media.SourceID(itemID), currentID)
	if err != nil {
		t.Fatalf("prepare playback session: %v", err)
	}
	if !strings.HasPrefix(session.ID, "play_") || session.ID == owner.SessionID ||
		session.UserID != owner.UserID || session.AuthSessionID != owner.SessionID ||
		session.DeviceID != owner.DeviceID || session.ItemID != itemID || session.MediaSourceID != media.SourceID(itemID) {
		t.Fatalf("prepared playback session has incorrect identity: %+v", session)
	}
	return session
}

func playSessionReport(t *testing.T, ctx context.Context, store *Store, owner PlaybackOwner, report PlaybackReport) (PlaySession, UserData) {
	t.Helper()
	session, data, err := store.ReportPlayback(ctx, owner, report)
	if err != nil {
		t.Fatalf("report playback %s: %v", report.Event, err)
	}
	return session, data
}

func playSessionSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, session PlaySession) (string, string) {
	t.Helper()
	var playback, data string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(p)::text FROM play_sessions p WHERE id = $1", session.ID).Scan(&playback); err != nil {
		t.Fatalf("snapshot playback session: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d
		WHERE user_id = $1 AND item_id = $2`, session.UserID, session.ItemID).Scan(&data); err != nil {
		t.Fatalf("snapshot playback user data: %v", err)
	}
	return playback, data
}

type playSessionReportResult struct {
	session PlaySession
	data    UserData
	err     error
}

func playSessionParallelReports(t *testing.T, ctx context.Context, store *Store, owner PlaybackOwner, report PlaybackReport, count int) []playSessionReportResult {
	t.Helper()
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan playSessionReportResult, count)
	for index := 0; index < count; index++ {
		go func() {
			<-start
			session, data, err := store.ReportPlayback(callCtx, owner, report)
			results <- playSessionReportResult{session: session, data: data, err: err}
		}()
	}
	close(start)
	collected := make([]playSessionReportResult, 0, count)
	for len(collected) < count {
		select {
		case result := <-results:
			collected = append(collected, result)
		case <-callCtx.Done():
			t.Fatalf("parallel playback reports did not finish: %v", callCtx.Err())
		}
	}
	return collected
}

func TestStorePlaybackLifecycleReusesPreparationAndKeepsTerminalReportsIdempotent(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	itemID := ids[0]
	prepared := playSessionPrepare(t, ctx, store, owner, itemID, "")
	if prepared.State != "Prepared" || prepared.PositionTicks != 0 || prepared.DurationTicks != playbackTestDuration || prepared.StartedAt != nil {
		t.Fatalf("initial prepared state = %+v", prepared)
	}
	for _, currentID := range []string{"", prepared.ID} {
		if reused := playSessionPrepare(t, ctx, store, owner, itemID, currentID); reused.ID != prepared.ID {
			t.Errorf("preparation did not reuse the current playback ID: %+v", reused)
		}
	}
	if active, err := store.GetPlaybackSession(ctx, owner, prepared.ID); err != nil || active.State != "Prepared" {
		t.Fatalf("prepared media session is unavailable: session = %+v, error = %v", active, err)
	}
	started, initialData := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	if started.State != "Playing" || started.StartedAt == nil || initialData.PlayCount != 1 || initialData.LastPlayedDate == nil {
		t.Fatalf("started playback was not counted once: session = %+v, data = %+v", started, initialData)
	}
	duplicate, duplicateData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Started", PositionTicks: playSessionPosition(30 * media.TicksPerSecond),
	})
	if !reflect.DeepEqual(duplicate, started) || !reflect.DeepEqual(duplicateData, initialData) {
		t.Errorf("duplicate start changed playback state: session = %+v, data = %+v", duplicate, duplicateData)
	}
	progress, data := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	if progress.State != "Playing" || progress.PositionTicks != 120*media.TicksPerSecond ||
		data.PlaybackPositionTicks != progress.PositionTicks || data.PlayCount != 1 || !reflect.DeepEqual(data.LastPlayedDate, initialData.LastPlayedDate) {
		t.Fatalf("progress changed the play count or lost its position: session = %+v, data = %+v", progress, data)
	}
	paused, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", IsPaused: true, PositionTicks: playSessionPosition(180 * media.TicksPerSecond),
	})
	if paused.State != "Paused" {
		t.Errorf("paused progress state = %q, want Paused", paused.State)
	}
	seek, seekData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(60 * media.TicksPerSecond),
	})
	if seek.State != "Playing" || seek.PositionTicks != 60*media.TicksPerSecond || seekData.PlaybackPositionTicks != seek.PositionTicks {
		t.Errorf("backward seek was not applied: session = %+v, data = %+v", seek, seekData)
	}
	clamped, clampedData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(int64(1<<63 - 1)),
	})
	if clamped.PositionTicks != playbackTestDuration || clampedData.PlaybackPositionTicks != playbackTestDuration || clampedData.Played {
		t.Errorf("large progress was not bounded without premature completion: session = %+v, data = %+v", clamped, clampedData)
	}
	beforeSession, beforeData := playSessionSnapshot(t, ctx, pool, clamped)
	if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(-1),
	}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("negative progress: got %v, want ErrInvalidInput", err)
	}
	if afterSession, afterData := playSessionSnapshot(t, ctx, pool, clamped); afterSession != beforeSession || afterData != beforeData {
		t.Error("rejected negative progress changed persisted state")
	}
	stopped, stoppedData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	if stopped.State != "Stopped" || stopped.StoppedAt == nil || stopped.PositionTicks != 120*media.TicksPerSecond ||
		stoppedData.PlaybackPositionTicks != stopped.PositionTicks || stoppedData.Played || stoppedData.PlayCount != 1 {
		t.Fatalf("stop lost resumable playback state: session = %+v, data = %+v", stopped, stoppedData)
	}
	for _, event := range []string{"Started", "Stopped", "Progress"} {
		late, lateData := playSessionReport(t, ctx, store, owner, PlaybackReport{
			PlaySessionID: prepared.ID, Event: event, PositionTicks: playSessionPosition(540 * media.TicksPerSecond),
		})
		if !reflect.DeepEqual(late, stopped) || !reflect.DeepEqual(lateData, stoppedData) {
			t.Errorf("late %s revived or changed terminal playback: session = %+v, data = %+v", event, late, lateData)
		}
	}
	if _, err := store.GetPlaybackSession(ctx, owner, prepared.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("terminal media session lookup: got %v, want ErrNotFound", err)
	}
}

func TestStorePlaybackStopAppliesDurationAndCompletionBoundaries(t *testing.T) {
	tests := []struct {
		name                       string
		duration, position, stored int64
		played                     bool
	}{
		{name: "below minimum position", duration: playbackTestDuration, position: 12*media.TicksPerSecond - 1},
		{name: "minimum position", duration: playbackTestDuration, position: 12 * media.TicksPerSecond, stored: 12 * media.TicksPerSecond},
		{name: "below completion", duration: playbackTestDuration, position: 540*media.TicksPerSecond - 1, stored: 540*media.TicksPerSecond - 1},
		{name: "completion boundary", duration: playbackTestDuration, position: 540 * media.TicksPerSecond, played: true},
		{name: "past duration", duration: playbackTestDuration, position: playbackTestDuration + 1, played: true},
		{name: "short media", duration: 119 * media.TicksPerSecond, position: 60 * media.TicksPerSecond},
		{name: "minimum duration", duration: 120 * media.TicksPerSecond, position: 60 * media.TicksPerSecond, stored: 60 * media.TicksPerSecond},
	}
	ctx, pool, store, owner, ids := playSessionFixture(t, len(tests))
	for index, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			itemID := ids[index]
			if _, err := pool.Exec(ctx, "UPDATE items SET media = jsonb_build_object('DurationTicks', $2::bigint) WHERE id = $1", itemID, fixture.duration); err != nil {
				t.Fatalf("set stop policy fixture duration: %v", err)
			}
			if _, err := store.SetFavorite(ctx, owner.UserID, itemID, true); err != nil {
				t.Fatalf("favorite stop policy fixture: %v", err)
			}
			prepared := playSessionPrepare(t, ctx, store, owner, itemID, "")
			playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
			stopped, data := playSessionReport(t, ctx, store, owner, PlaybackReport{
				PlaySessionID: prepared.ID, Event: "Stopped", PositionTicks: playSessionPosition(fixture.position),
			})
			rawPosition := fixture.position
			if rawPosition > fixture.duration {
				rawPosition = fixture.duration
			}
			if stopped.State != "Stopped" || stopped.PositionTicks != rawPosition || stopped.DurationTicks != fixture.duration ||
				data.PlaybackPositionTicks != fixture.stored || data.Played != fixture.played || data.PlayCount != 1 || !data.IsFavorite {
				t.Errorf("stop policy result: session = %+v, data = %+v, want position %d and played %v", stopped, data, fixture.stored, fixture.played)
			}
			persisted, err := store.GetUserData(ctx, owner.UserID, itemID)
			if err != nil || !reflect.DeepEqual(persisted, data) {
				t.Errorf("stop policy was not persisted: data = %+v, error = %v", persisted, err)
			}
		})
	}
}

func TestStorePlaybackReportsEnforceOwnerSourceAndCurrentAuthorization(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 2)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	beforeSession, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
	otherUser := "other-playback-user"
	libraryIntegrationUser(t, ctx, pool, otherUser, false, true, nil)
	otherOwner := playSessionOwnerFixture(t, ctx, pool, otherUser, owner.DeviceID)
	otherAuth := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	otherDevice := owner
	otherDevice.DeviceID = "unrecognized-playback-device"
	for _, fixture := range []struct {
		name  string
		owner PlaybackOwner
		want  error
	}{
		{"different user", otherOwner, ErrNotFound},
		{"different authentication session", otherAuth, ErrNotFound},
		{"different device", otherDevice, ErrForbidden},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, _, err := store.ReportPlayback(ctx, fixture.owner, PlaybackReport{
				PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(60 * media.TicksPerSecond),
			}); !errors.Is(err, fixture.want) {
				t.Errorf("foreign owner report: got %v, want %v", err, fixture.want)
			}
			if _, err := store.GetPlaybackSession(ctx, fixture.owner, prepared.ID); !errors.Is(err, fixture.want) {
				t.Errorf("foreign owner media lookup: got %v, want %v", err, fixture.want)
			}
			if _, err := store.PreparePlayback(ctx, fixture.owner, ids[0], media.SourceID(ids[0]), prepared.ID); !errors.Is(err, fixture.want) {
				t.Errorf("foreign owner preparation reuse: got %v, want %v", err, fixture.want)
			}
		})
	}
	for _, report := range []PlaybackReport{
		{PlaySessionID: prepared.ID, ItemID: ids[1], Event: "Progress"},
		{PlaySessionID: prepared.ID, MediaSourceID: media.SourceID(ids[1]), Event: "Progress"},
	} {
		if _, _, err := store.ReportPlayback(ctx, owner, report); !errors.Is(err, ErrNotFound) {
			t.Errorf("mismatched item or source report: got %v, want ErrNotFound", err)
		}
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[1]), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("mismatched source preparation: got %v, want ErrNotFound", err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[1], media.SourceID(ids[1]), prepared.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("mismatched current-session item: got %v, want ErrNotFound", err)
	}
	assertUnchanged := func(checkT *testing.T) {
		checkT.Helper()
		if afterSession, afterData := playSessionSnapshot(checkT, ctx, pool, prepared); afterSession != beforeSession || afterData != beforeData {
			checkT.Error("rejected playback request changed persisted session or user data")
		}
		var sessions, dataRows int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM play_sessions), (SELECT count(*) FROM user_item_data)").Scan(&sessions, &dataRows); err != nil || sessions != 1 || dataRows != 1 {
			checkT.Errorf("rejected request created state: sessions = %d, user data = %d, error = %v", sessions, dataRows, err)
		}
	}
	assertUnchanged(t)
	for _, fixture := range []struct {
		name, block, restore, id string
	}{
		{"revoked authentication", "UPDATE sessions SET revoked_at = now() WHERE id = $1", "UPDATE sessions SET revoked_at = NULL WHERE id = $1", owner.SessionID},
		{"disabled user", "UPDATE users SET is_disabled = true WHERE id = $1", "UPDATE users SET is_disabled = false WHERE id = $1", owner.UserID},
		{"playback permission removed", `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, "UPDATE users SET policy = policy - 'EnableMediaPlayback' WHERE id = $1", owner.UserID},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, fixture.block, fixture.id); err != nil {
				t.Fatalf("change current playback authorization: %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if _, err := pool.Exec(cleanupCtx, fixture.restore, fixture.id); err != nil {
					t.Errorf("restore playback authorization fixture: %v", err)
				}
			})
			if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
				PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(60 * media.TicksPerSecond),
			}); !errors.Is(err, ErrForbidden) {
				t.Errorf("report after authorization removal: got %v, want ErrForbidden", err)
			}
			if _, err := store.GetPlaybackSession(ctx, owner, prepared.ID); !errors.Is(err, ErrForbidden) {
				t.Errorf("media lookup after authorization removal: got %v, want ErrForbidden", err)
			}
			if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), prepared.ID); !errors.Is(err, ErrForbidden) {
				t.Errorf("preparation after authorization removal: got %v, want ErrForbidden", err)
			}
			assertUnchanged(t)
		})
	}
	if active, err := store.GetPlaybackSession(ctx, owner, prepared.ID); err != nil || active.State != "Playing" {
		t.Errorf("restored authorization could not access the original active session: session = %+v, error = %v", active, err)
	}
}

func TestStoreLegacyPlaybackMapsByOwnerAndItemWithoutAcceptingUnknownExplicitIDs(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 3)
	for _, event := range []string{"Progress", "Stopped"} {
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{ItemID: ids[2], Event: event}); !errors.Is(err, ErrNotFound) {
			t.Errorf("legacy %s without a started session: got %v, want ErrNotFound", event, err)
		}
	}
	first, firstData := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	if !strings.HasPrefix(first.ID, "play_") || first.ID == owner.SessionID || first.State != "Playing" ||
		first.ItemID != ids[0] || first.MediaSourceID != media.SourceID(ids[0]) || firstData.PlayCount != 1 {
		t.Fatalf("legacy start did not create a separate playback identity: session = %+v, data = %+v", first, firstData)
	}
	second, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[1], Event: "Started"})
	if second.ID == first.ID || second.ItemID != ids[1] {
		t.Fatalf("legacy starts for different items shared a session: first = %+v, second = %+v", first, second)
	}
	duplicate, duplicateData := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	if duplicate.ID != first.ID || duplicateData.PlayCount != 1 {
		t.Errorf("duplicate legacy start created or recounted a session: session = %+v, data = %+v", duplicate, duplicateData)
	}
	progress, progressData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		ItemID: ids[0], Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	if progress.ID != first.ID || progress.PositionTicks != 120*media.TicksPerSecond || progressData.PlayCount != 1 {
		t.Errorf("legacy progress did not resolve its source item: session = %+v, data = %+v", progress, progressData)
	}
	stopped, stoppedData := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Stopped"})
	if stopped.ID != first.ID || stopped.State != "Stopped" || stoppedData.PlaybackPositionTicks != 120*media.TicksPerSecond {
		t.Errorf("legacy stop did not resolve its source item: session = %+v, data = %+v", stopped, stoppedData)
	}
	late, lateData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		ItemID: ids[0], Event: "Progress", PositionTicks: playSessionPosition(300 * media.TicksPerSecond),
	})
	if !reflect.DeepEqual(late, stopped) || !reflect.DeepEqual(lateData, stoppedData) {
		t.Errorf("late legacy progress revived a stopped session: session = %+v, data = %+v", late, lateData)
	}
	if current, err := store.GetPlaybackSession(ctx, owner, second.ID); err != nil || current.State != "Playing" || current.PositionTicks != 0 {
		t.Errorf("legacy reports affected the other item's session: session = %+v, error = %v", current, err)
	}
	otherAuth := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	if _, _, err := store.ReportPlayback(ctx, otherAuth, PlaybackReport{ItemID: ids[1], Event: "Progress"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("legacy report borrowed another authentication session: got %v, want ErrNotFound", err)
	}
	for _, unknownID := range []string{"play_unknown-explicit-session", owner.SessionID} {
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
			PlaySessionID: unknownID, ItemID: ids[2], MediaSourceID: media.SourceID(ids[2]), Event: "Started",
		}); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown explicit playback ID created a session: got %v, want ErrNotFound", err)
		}
	}
	var sessions, dataRows int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM play_sessions), (SELECT count(*) FROM user_item_data)").Scan(&sessions, &dataRows); err != nil || sessions != 2 || dataRows != 2 {
		t.Errorf("legacy misses or explicit unknown IDs created state: sessions = %d, data rows = %d, error = %v", sessions, dataRows, err)
	}
}

func TestStoreLegacyReportsPreferActiveSessionOverNewerTerminalHistory(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	old, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	old, _ = playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: old.ID, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	active, data := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	if active.ID == old.ID || active.State != "Playing" || data.PlayCount != 2 {
		t.Fatalf("legacy restart did not create a fresh active session: session = %+v, data = %+v", active, data)
	}
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET created_at = $2 WHERE id = $1", old.ID, active.CreatedAt.Add(time.Hour)); err != nil {
		t.Fatalf("make terminal history newer than the active session: %v", err)
	}
	oldSnapshot, _ := playSessionSnapshot(t, ctx, pool, old)
	for _, event := range []string{"Progress", "Stopped"} {
		reported, reportedData := playSessionReport(t, ctx, store, owner, PlaybackReport{
			ItemID: ids[0], Event: event, PositionTicks: playSessionPosition(180 * media.TicksPerSecond),
		})
		wantState := "Playing"
		if event == "Stopped" {
			wantState = "Stopped"
		}
		if reported.ID != active.ID || reported.State != wantState || reported.PositionTicks != 180*media.TicksPerSecond ||
			reportedData.PlayCount != 2 || reportedData.PlaybackPositionTicks != reported.PositionTicks {
			t.Errorf("legacy %s selected newer terminal history instead of the active session: session = %+v, data = %+v", event, reported, reportedData)
		}
		if retained, _ := playSessionSnapshot(t, ctx, pool, old); retained != oldSnapshot {
			t.Errorf("legacy %s modified the older terminal session", event)
		}
	}
}

func TestStorePlaybackAdminAuthenticationIsRejectedAfterDemotion(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", owner.UserID); err != nil {
		t.Fatalf("promote administrator playback fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'admin' WHERE id = $1", owner.SessionID); err != nil {
		t.Fatalf("set administrator authentication session kind: %v", err)
	}
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	beforeSession, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", owner.UserID); err != nil {
		t.Fatalf("demote administrator playback fixture: %v", err)
	}
	if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
		PlaySessionID: prepared.ID, Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("demoted administrator reported through an admin session: got %v, want ErrForbidden", err)
	}
	if _, err := store.GetPlaybackSession(ctx, owner, prepared.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("demoted administrator opened media through an admin session: got %v, want ErrForbidden", err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), prepared.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("demoted administrator reused playback through an admin session: got %v, want ErrForbidden", err)
	}
	if afterSession, afterData := playSessionSnapshot(t, ctx, pool, prepared); afterSession != beforeSession || afterData != beforeData {
		t.Error("rejected demoted administrator request changed playback state")
	}
}

func TestStoreConcurrentPlaybackStartAndStopCountOnlyOnce(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	for _, phase := range []struct {
		event string
		state string
	}{
		{"Started", "Playing"},
		{"Stopped", "Stopped"},
	} {
		report := PlaybackReport{PlaySessionID: prepared.ID, Event: phase.event}
		if phase.event == "Stopped" {
			report.PositionTicks = playSessionPosition(120 * media.TicksPerSecond)
		}
		results := playSessionParallelReports(t, ctx, store, owner, report, 8)
		for index, result := range results {
			if result.err != nil || result.session.ID != prepared.ID || result.session.State != phase.state ||
				result.data.PlayCount != 1 || result.data.LastPlayedDate == nil {
				t.Errorf("parallel %s result %d was not idempotent: session = %+v, data = %+v, error = %v",
					phase.event, index, result.session, result.data, result.err)
			}
		}
	}
	persisted, err := store.GetUserData(ctx, owner.UserID, ids[0])
	if err != nil || persisted.PlayCount != 1 || persisted.PlaybackPositionTicks != 120*media.TicksPerSecond || persisted.Played {
		t.Errorf("concurrent reports changed persisted play count: data = %+v, error = %v", persisted, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM play_sessions WHERE auth_session_id = $1", owner.SessionID).Scan(&count); err != nil || count != 1 {
		t.Errorf("concurrent reports created %d playback sessions, error = %v", count, err)
	}
}

func TestStorePlaybackCapacityIsAtomicAndExpiredPreparationCanBeReplaced(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 33)
	prepared := make([]PlaySession, 0, 32)
	for _, itemID := range ids[:31] {
		prepared = append(prepared, playSessionPrepare(t, ctx, store, owner, itemID, ""))
	}
	type prepareResult struct {
		itemID  string
		session PlaySession
		err     error
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan prepareResult, 2)
	for _, itemID := range ids[31:] {
		go func(itemID string) {
			<-start
			session, err := store.PreparePlayback(callCtx, owner, itemID, media.SourceID(itemID), "")
			results <- prepareResult{itemID: itemID, session: session, err: err}
		}(itemID)
	}
	close(start)
	collected := make([]prepareResult, 0, 2)
	for len(collected) < 2 {
		select {
		case result := <-results:
			collected = append(collected, result)
		case <-callCtx.Done():
			t.Fatalf("capacity contenders did not finish: %v", callCtx.Err())
		}
	}
	succeeded, busy := 0, 0
	blockedItem := ""
	for _, result := range collected {
		if result.err == nil {
			succeeded++
			prepared = append(prepared, result.session)
		} else if errors.Is(result.err, ErrBusy) {
			busy++
			blockedItem = result.itemID
		} else {
			t.Errorf("capacity contender returned an unexpected error: %v", result.err)
		}
	}
	if succeeded != 1 || busy != 1 || len(prepared) != 32 {
		t.Fatalf("active capacity was not enforced atomically: successes = %d, busy = %d", succeeded, busy)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE auth_session_id = $1
		AND state IN ('Prepared','Playing','Paused') AND expires_at > now()`, owner.SessionID).Scan(&active); err != nil || active != 32 {
		t.Fatalf("active sessions at capacity = %d, error = %v", active, err)
	}
	if _, err := store.PreparePlayback(ctx, owner, blockedItem, media.SourceID(blockedItem), ""); !errors.Is(err, ErrBusy) {
		t.Errorf("preparation beyond authentication capacity: got %v, want ErrBusy", err)
	}
	if reused := playSessionPrepare(t, ctx, store, owner, ids[0], prepared[0].ID); reused.ID != prepared[0].ID {
		t.Errorf("full capacity prevented reusing an existing session: %+v", reused)
	}
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at = now() - interval '1 second' WHERE id = $1", prepared[0].ID); err != nil {
		t.Fatalf("expire owned playback preparation: %v", err)
	}
	if _, err := store.GetPlaybackSession(ctx, owner, prepared[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired media session lookup: got %v, want ErrNotFound", err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), prepared[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("explicit expired preparation was revived: got %v, want ErrNotFound", err)
	}
	replacement := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if replacement.ID == prepared[0].ID || replacement.State != "Prepared" {
		t.Errorf("expired preparation was not replaced with a fresh ID: %+v", replacement)
	}
	var oldState string
	var stoppedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT state, stopped_at FROM play_sessions WHERE id = $1", prepared[0].ID).Scan(&oldState, &stoppedAt); err != nil || oldState != "Expired" || stoppedAt == nil {
		t.Errorf("expired preparation was not made terminal: state = %q, stopped = %v, error = %v", oldState, stoppedAt, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE auth_session_id = $1
		AND state IN ('Prepared','Playing','Paused') AND expires_at > now()`, owner.SessionID).Scan(&active); err != nil || active != 32 {
		t.Errorf("replacement changed the active capacity bound: count = %d, error = %v", active, err)
	}
}

func TestStorePlaybackTerminalRetentionAndRequiredIndexes(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, device_id, item_id, media_source_id, state,
		 position_ticks, duration_ticks, created_at, updated_at, expires_at, stopped_at)
		SELECT 'play_terminal_' || entry::text, $1, $2, $3, $4, $5, 'Stopped',
			0, $6, now() - interval '1 day' + entry * interval '1 second',
			now() - interval '1 hour', now() - interval '1 hour', now() - interval '1 hour'
		FROM generate_series(1, 300) AS entry`,
		owner.UserID, owner.SessionID, owner.DeviceID, ids[0], media.SourceID(ids[0]), playbackTestDuration); err != nil {
		t.Fatalf("seed recent terminal playback history: %v", err)
	}
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	var terminalCount int
	var oldestExists, newestExists bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM play_sessions WHERE auth_session_id = $1 AND state IN ('Stopped','Expired')),
		EXISTS (SELECT 1 FROM play_sessions WHERE id = 'play_terminal_1'),
		EXISTS (SELECT 1 FROM play_sessions WHERE id = 'play_terminal_300')`,
		owner.SessionID).Scan(&terminalCount, &oldestExists, &newestExists); err != nil {
		t.Fatalf("read pruned playback history: %v", err)
	}
	if terminalCount > 256 || oldestExists || !newestExists {
		t.Errorf("terminal retention did not retain bounded recent history: count = %d, oldest = %v, newest = %v", terminalCount, oldestExists, newestExists)
	}
	if active, err := store.GetPlaybackSession(ctx, owner, prepared.ID); err != nil || active.State != "Prepared" {
		t.Errorf("history pruning removed the live preparation: session = %+v, error = %v", active, err)
	}
	for _, index := range []struct {
		name, columns string
		unique        bool
	}{
		{"play_sessions_current_source_idx", "(user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id)", true},
		{"play_sessions_owner_recent_idx", "(auth_session_id, created_at DESC, id)", false},
		{"play_sessions_expiry_idx", "(expires_at, id)", false},
		{"play_sessions_user_expiry_idx", "(user_id, expires_at, id)", false},
	} {
		var definition string
		if err := pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes
			WHERE schemaname = current_schema() AND tablename = 'play_sessions' AND indexname = $1`,
			index.name).Scan(&definition); err != nil {
			t.Errorf("required playback index %s is missing: %v", index.name, err)
			continue
		}
		if strings.Contains(definition, "CREATE UNIQUE INDEX") != index.unique || !strings.Contains(definition, index.columns) {
			t.Errorf("playback index %s has unexpected keys or uniqueness: %s", index.name, definition)
		}
		if index.unique && (!strings.Contains(definition, "WHERE") || !strings.Contains(definition, "'Prepared'") ||
			!strings.Contains(definition, "'Playing'") || !strings.Contains(definition, "'Paused'")) {
			t.Errorf("active playback uniqueness is missing its state predicate: %s", definition)
		}
	}
}

func TestStorePlaybackListingRechecksAdministratorAndSessionOwnerLibraryAccess(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 3)
	live := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	terminal := playSessionPrepare(t, ctx, store, owner, ids[1], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: terminal.ID, Event: "Started"})
	playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: terminal.ID, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	expired := playSessionPrepare(t, ctx, store, owner, ids[2], "")
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", expired.ID); err != nil {
		t.Fatalf("expire prepared playback listing fixture: %v", err)
	}
	if _, err := store.ListPlaybackSessions(ctx, owner.UserID, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-administrator requested administrator playback scope: got %v, want ErrForbidden", err)
	}
	listed, err := store.ListPlaybackSessions(ctx, owner.UserID, false)
	if err != nil || len(listed) != 1 || listed[0].ID != live.ID || listed[0].State != "Prepared" {
		t.Fatalf("owner listing included terminal or expired sessions: sessions = %+v, error = %v", listed, err)
	}
	administratorID := "playback-list-administrator"
	libraryIntegrationUser(t, ctx, pool, administratorID, false, true, nil)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", administratorID); err != nil {
		t.Fatalf("promote playback listing administrator fixture: %v", err)
	}
	listed, err = store.ListPlaybackSessions(ctx, administratorID, true)
	if err != nil || len(listed) != 1 || listed[0].ID != live.ID || listed[0].UserID != owner.UserID {
		t.Fatalf("administrator listing did not expose the authorized live session: sessions = %+v, error = %v", listed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = policy ||
		'{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = $1`, owner.UserID); err != nil {
		t.Fatalf("remove playback owner's current library access: %v", err)
	}
	listed, err = store.ListPlaybackSessions(ctx, administratorID, true)
	if err != nil || len(listed) != 0 {
		t.Errorf("administrator listing retained playback after the owner's library access was revoked: sessions = %+v, error = %v", listed, err)
	}
	listed, err = store.ListPlaybackSessions(ctx, owner.UserID, false)
	if err != nil || len(listed) != 0 {
		t.Errorf("owner listing retained playback after library access was revoked: sessions = %+v, error = %v", listed, err)
	}
}
