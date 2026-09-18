package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func playbackAuthorizationSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) identity.ManagedPolicy {
	t.Helper()
	var observedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		t.Fatalf("read the playback authorization clock: %v", err)
	}
	localStart := observedAt.In(time.Local)
	localEnd := observedAt.Add(3 * time.Second).In(time.Local)
	endHour := float64(localEnd.Hour()) + float64(localEnd.Minute())/60 +
		float64(localEnd.Second())/3600 + float64(localEnd.Nanosecond())/3.6e12
	schedules := make([]identity.AccessSchedule, 0, 2)
	if localStart.Year() != localEnd.Year() || localStart.YearDay() != localEnd.YearDay() {
		schedules = append(schedules, identity.AccessSchedule{DayOfWeek: localStart.Weekday().String(), StartHour: 0, EndHour: 24})
	}
	if endHour > 0 {
		schedules = append(schedules, identity.AccessSchedule{DayOfWeek: localEnd.Weekday().String(), StartHour: 0, EndHour: endHour})
	}
	raw, err := json.Marshal(map[string]any{"AccessSchedules": schedules})
	if err != nil {
		t.Fatalf("encode the playback access window: %v", err)
	}
	policy, err := identity.ParseRuntimePolicy(raw)
	if err != nil {
		t.Fatalf("parse the playback access window: %v", err)
	}
	// The policy uses local wall time. A timezone offset transition inside this
	// three-second window cannot always describe one continuous access interval.
	if !policy.AllowsAccessAt(observedAt) || policy.AllowsAccessAt(observedAt.Add(3*time.Second+time.Millisecond)) {
		t.Skip("the local clock changes offset inside the bounded access window")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET policy = policy || $2::jsonb WHERE id = $1", userID, raw); err != nil {
		t.Fatalf("install the bounded playback access window: %v", err)
	}
	return policy
}

func waitPlaybackScheduleEnd(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schedule identity.ManagedPolicy) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var observedAt time.Time
		if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
			t.Fatalf("observe the playback access schedule boundary: %v", err)
		}
		if !schedule.AllowsAccessAt(observedAt) {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("the playback authorization boundary was not reached: %v", ctx.Err())
		}
	}
}

func TestStorePlaybackReportsRejectAuthorizationEndingDuringBusinessLockWait(t *testing.T) {
	for _, boundary := range []string{"credential expiry", "access schedule end"} {
		t.Run(boundary, func(t *testing.T) {
			for _, test := range []struct {
				name, event, lockedTable string
			}{
				{"progress data wait", "Progress", "user_item_data"},
				{"stopped playback wait", "Stopped", "play_sessions"},
				{"repeated start playback wait", "Started", "play_sessions"},
				{"ping playback wait", "Ping", "play_sessions"},
			} {
				t.Run(test.name, func(t *testing.T) {
					ctx, pool, store, owner, ids := playSessionFixture(t, 1)
					owner.PeerIP = "192.168.1.20"
					prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
					if test.event == "Started" || test.event == "Ping" {
						started, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
						if started.StartedAt == nil {
							t.Fatal("the repeated event fixture did not start playback")
						}
						playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress",
							PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
					}
					beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
					operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					defer cancel()
					blocker, err := pool.Begin(operationCtx)
					if err != nil {
						t.Fatalf("begin the playback business-row barrier: %v", err)
					}
					defer rollback(blocker)
					var locked string
					if test.lockedTable == "user_item_data" {
						err = blocker.QueryRow(operationCtx, `SELECT user_id FROM user_item_data
							WHERE user_id = $1 AND item_id = $2 FOR UPDATE`, owner.UserID, ids[0]).Scan(&locked)
					} else {
						err = blocker.QueryRow(operationCtx, "SELECT id FROM play_sessions WHERE id = $1 FOR UPDATE", prepared.ID).Scan(&locked)
					}
					if err != nil {
						t.Fatalf("lock the playback business row: %v", err)
					}
					var schedule *identity.ManagedPolicy
					if boundary == "credential expiry" {
						if _, err := pool.Exec(operationCtx, `UPDATE sessions SET expires_at = clock_timestamp() + interval '3 seconds'
							WHERE id = $1`, owner.SessionID); err != nil {
							t.Fatalf("bound the playback credential lifetime: %v", err)
						}
					} else {
						policy := playbackAuthorizationSchedule(t, operationCtx, pool, owner.UserID)
						schedule = &policy
					}
					finished := make(chan error, 1)
					returned := make(chan struct{})
					go func() {
						defer close(returned)
						_, _, err := store.ReportPlayback(operationCtx, owner, PlaybackReport{
							PlaySessionID: prepared.ID, Event: test.event, IsPaused: true,
							PositionTicks: playSessionPosition(590 * media.TicksPerSecond),
						})
						finished <- err
					}()
					defer func() {
						cancel()
						rollback(blocker)
						select {
						case <-returned:
						case <-time.After(5 * time.Second):
							t.Error("the bounded playback report did not release its transaction")
						}
					}()
					waitCatalogApplicationBlock(t, operationCtx, pool, blocker.Conn().PgConn().PID())
					select {
					case err := <-finished:
						t.Fatalf("the report returned before its business lock was released: %v", err)
					default:
					}
					if schedule == nil {
						waitStateSessionExpiry(t, operationCtx, pool, owner.SessionID)
					} else {
						waitPlaybackScheduleEnd(t, operationCtx, pool, *schedule)
					}
					if err := blocker.Commit(operationCtx); err != nil {
						t.Fatalf("release the playback business-row barrier: %v", err)
					}
					select {
					case err := <-finished:
						if !errors.Is(err, ErrForbidden) {
							t.Fatalf("report after authorization ended = %v, want forbidden", err)
						}
					case <-operationCtx.Done():
						t.Fatal("the playback report did not finish after its business lock was released")
					}
					afterPlay, afterData := playSessionSnapshot(t, ctx, pool, prepared)
					if afterPlay != beforePlay || afterData != beforeData {
						t.Fatal("a report outliving its authorization changed playback or user state")
					}
				})
			}
		})
	}
}

func TestStorePlaybackReportsUseCurrentLoginPolicyAndTrustedPeer(t *testing.T) {
	for _, test := range []struct {
		name, policy, peerIP string
		allowed              bool
	}{
		{"device denied", `{"EnableAllDevices":false,"EnabledDevices":[]}`, "192.168.1.20", false},
		{"remote peer denied", `{"EnableRemoteAccess":false}`, "8.8.8.8", false},
		{"missing peer denied", `{"EnableRemoteAccess":false}`, "", false},
		{"trusted local peer allowed", `{"EnableRemoteAccess":false}`, "192.168.1.20", true},
		{"locked account denied", `{"LockedOutDate":1}`, "192.168.1.20", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool, store, owner, ids := playSessionFixture(t, 1)
			owner.PeerIP = test.peerIP
			prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
			playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
			beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
			if _, err := pool.Exec(ctx, "UPDATE users SET policy = policy || $2::jsonb WHERE id = $1", owner.UserID, test.policy); err != nil {
				t.Fatalf("replace the current playback login policy: %v", err)
			}
			position := int64(240 * media.TicksPerSecond)
			play, data, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: prepared.ID,
				Event: "Progress", PositionTicks: &position})
			if test.allowed {
				if err != nil || play.PositionTicks != position || data.PlaybackPositionTicks != position {
					t.Fatalf("trusted local playback was not preserved: play=%+v data=%+v error=%v", play, data, err)
				}
				afterPlay, afterData := playSessionSnapshot(t, ctx, pool, prepared)
				if afterPlay == beforePlay || afterData == beforeData {
					t.Fatal("the allowed local report did not persist playback and user state")
				}
				return
			}
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("report under a denied current login policy = %v, want forbidden", err)
			}
			afterPlay, afterData := playSessionSnapshot(t, ctx, pool, prepared)
			if afterPlay != beforePlay || afterData != beforeData {
				t.Fatal("a rejected current login policy changed playback or user state")
			}
		})
	}
}
