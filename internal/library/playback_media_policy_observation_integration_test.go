//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestPlaybackMediaPrincipalObservationReadsEachCurrentPolicy(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, _ := playbackMediaTestPrincipal(t, fixture, false)
	allowed, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{fixture.library.ID},
		"EnableAllDevices": false, "EnabledDevices": []string{principal.Client.DeviceID},
		"EnableRemoteAccess": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, policy string
		want         error
	}{
		{"scoped", string(allowed), nil},
		{"playback_denied", `{"EnableMediaPlayback":false}`, ErrForbidden},
		{"playback_malformed", `{"EnableMediaPlayback":"invalid"}`, ErrForbidden},
		{"login_malformed", `{"EnableRemoteAccess":null}`, identity.ErrUnauthorized},
		{"locked_out", `{"LockedOutDate":1}`, identity.ErrUnauthorized},
		{"restored", `{}`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", principal.User.ID, test.policy); err != nil {
				t.Fatal(err)
			}
			tx, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := lockPlaybackMediaAuthorityBatch(fixture.ctx, tx, principal, playbackMediaOwner(principal)); err != nil {
				t.Fatal(err)
			}
			fresh, access, observation, err := readPlaybackMediaPrincipal(fixture.ctx, tx, principal)
			if test.want != nil {
				if !errors.Is(err, test.want) || !errors.Is(observation.ValidateAt(time.Now()), identity.ErrUnauthorized) {
					t.Fatalf("rejected playback policy returned authority: %v", err)
				}
				return
			}
			if err != nil || !access.canPlay || fresh.SessionID != principal.SessionID || fresh.PeerIP != principal.PeerIP {
				t.Fatalf("current policy changed the observed principal: %v", err)
			}
			if test.name == "scoped" && (access.all || len(access.folders) != 1 || access.folders[0] != fixture.library.ID || access.policy.EnableRemoteAccess) {
				t.Fatal("library authorization did not reuse the observed scoped policy")
			}
			if test.name == "restored" && !access.all {
				t.Fatal("a later authorization reused an earlier transaction's policy")
			}
			var observedAt time.Time
			if err := tx.QueryRow(fixture.ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
				t.Fatal(err)
			}
			if err := observation.ValidateAt(observedAt); err != nil {
				t.Fatalf("live policy observation failed its final clock check: %v", err)
			}
		})
	}
}

func TestPlaybackMediaPolicyObservationRechecksScheduleAfterPlayWait(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	ctx, cancel := context.WithTimeout(fixture.ctx, 15*time.Second)
	defer cancel()
	barrier, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(barrier)
	var locked string
	if err := barrier.QueryRow(ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE", play.ID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	schedule := playbackAuthorizationSchedule(t, ctx, fixture.pool, principal.User.ID)
	results := make(chan playbackMediaTestResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
		results <- playbackMediaTestResult{file, result, err}
	}()
	defer func() {
		cancel()
		rollback(barrier)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("playback authorization did not release its transaction")
		}
	}()
	waitPlaybackValidationBlock(t, ctx, fixture.pool, barrier.Conn().PgConn().PID())
	waitPlaybackScheduleEnd(t, ctx, fixture.pool, schedule)
	if err := barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.file != nil {
		_ = result.file.Close()
	}
	if result.file != nil || !errors.Is(result.err, identity.ErrUnauthorized) {
		t.Fatalf("parsed policy retained a grant beyond its access schedule: %v", result.err)
	}
}
