package library

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type playbackValidationResult struct {
	session PlaySession
	err     error
}

func waitPlaybackValidationBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blocker uint32) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock')`, blocker).Scan(&blocked); err != nil {
			t.Fatalf("observe playback validation lock wait: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatal("playback operation did not wait for its row barrier")
		}
	}
}

func awaitPlaybackValidation(t *testing.T, ctx context.Context, result <-chan playbackValidationResult) playbackValidationResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-ctx.Done():
		t.Fatalf("playback validation did not finish: %v", ctx.Err())
		return playbackValidationResult{}
	}
}

func TestStorePlaybackValidationSharesLocksForCanonicalReferencesAndLegacyIDs(t *testing.T) {
	ctx, pool, store, normal, ids := playSessionFixture(t, 6)
	for ownerIndex, owner := range []PlaybackOwner{normal, applicationPlaybackOwnerFixture(t, ctx, pool, "validation-shared")} {
		for selectorIndex, selector := range []string{"canonical", "client reference", "legacy canonical"} {
			t.Run(fmt.Sprintf("owner-%d/%s", ownerIndex, selector), func(t *testing.T) {
				itemID := ids[ownerIndex*3+selectorIndex]
				var prepared PlaySession
				var reference string
				if selector == "client reference" {
					reference = "shared-validation-client-reference"
					prepared = playReferencePrepare(t, ctx, store, owner, itemID, reference)
				} else {
					prepared = playSessionPrepare(t, ctx, store, owner, itemID, "")
					reference = prepared.ID
					if selector == "legacy canonical" {
						reference = fmt.Sprintf("legacy-validation-%d", ownerIndex)
						if _, err := pool.Exec(ctx, "UPDATE play_sessions SET id=$2 WHERE id=$1", prepared.ID, reference); err != nil {
							t.Fatalf("install legacy playback identifier: %v", err)
						}
						prepared.ID = reference
					}
				}
				var before string
				if err := pool.QueryRow(ctx, "SELECT to_jsonb(play)::text || play.xmin::text FROM play_sessions play WHERE id=$1", prepared.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				barrier, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(barrier)
				var locked string
				if err := barrier.QueryRow(ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR SHARE", prepared.ID).Scan(&locked); err != nil {
					t.Fatalf("hold compatible playback validation lock: %v", err)
				}
				callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				const readers = 8
				start := make(chan struct{})
				results := make(chan playbackValidationResult, readers)
				for index := 0; index < readers; index++ {
					go func() {
						<-start
						session, err := store.GetPlaybackSession(callCtx, owner, reference)
						results <- playbackValidationResult{session: session, err: err}
					}()
				}
				close(start)
				for index := 0; index < readers; index++ {
					result := awaitPlaybackValidation(t, callCtx, results)
					if result.err != nil || result.session.ID != prepared.ID || result.session.State != "Prepared" {
						t.Fatalf("validation serialized behind a compatible reader or changed its identity: %+v, %v", result.session, result.err)
					}
				}
				if err := barrier.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				var after string
				if err := pool.QueryRow(ctx, "SELECT to_jsonb(play)::text || play.xmin::text FROM play_sessions play WHERE id=$1", prepared.ID).Scan(&after); err != nil || after != before {
					t.Fatalf("validation wrote playback state or its row version: %v", err)
				}
			})
		}
	}
}

func TestStorePlaybackValidationRejectsStopCommittedDuringRowWait(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	const reference = "validation-stop-reference"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	barrier, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(barrier)
	if _, err := barrier.Exec(ctx, `UPDATE play_sessions SET state='Stopped',
		stopped_at=clock_timestamp(), expires_at=clock_timestamp() WHERE id=$1`, prepared.ID); err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results := make(chan playbackValidationResult, 1)
	go func() {
		session, err := store.GetPlaybackSession(callCtx, owner, reference)
		results <- playbackValidationResult{session: session, err: err}
	}()
	waitPlaybackValidationBlock(t, callCtx, pool, barrier.Conn().PgConn().PID())
	if err := barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if result := awaitPlaybackValidation(t, callCtx, results); !errors.Is(result.err, ErrNotFound) {
		t.Fatalf("validation authorized a stop committed while it waited: %+v, %v", result.session, result.err)
	}
}

func TestStorePlaybackValidationRechecksDeadlinesAfterRowWait(t *testing.T) {
	for _, deadline := range []string{"playback", "authentication"} {
		t.Run(deadline, func(t *testing.T) {
			ctx, pool, store, owner, ids := playSessionFixture(t, 1)
			prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
			statement, id, want := "UPDATE play_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1", prepared.ID, ErrNotFound
			waitStatement := "SELECT pg_sleep(GREATEST(0, extract(epoch FROM expires_at-clock_timestamp()))+0.05) FROM play_sessions WHERE id=$1"
			if deadline == "authentication" {
				statement, id, want = "UPDATE sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1", owner.SessionID, ErrForbidden
				waitStatement = "SELECT pg_sleep(GREATEST(0, extract(epoch FROM expires_at-clock_timestamp()))+0.05) FROM sessions WHERE id=$1"
			}
			if _, err := pool.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			barrier, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			var locked string
			if err := barrier.QueryRow(ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE", prepared.ID).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackValidationResult, 1)
			go func() {
				session, err := store.GetPlaybackSession(callCtx, owner, prepared.ID)
				results <- playbackValidationResult{session: session, err: err}
			}()
			waitPlaybackValidationBlock(t, callCtx, pool, barrier.Conn().PgConn().PID())
			// Advance database time beyond the fixture deadline without changing
			// its tuple. Locking SELECT expressions must be refreshed explicitly.
			if _, err := barrier.Exec(ctx, waitStatement, id); err != nil {
				t.Fatal(err)
			}
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if result := awaitPlaybackValidation(t, callCtx, results); !errors.Is(result.err, want) {
				t.Fatalf("validation accepted an elapsed %s deadline: %+v, %v", deadline, result.session, result.err)
			}
			var state string
			if err := pool.QueryRow(ctx, "SELECT state FROM play_sessions WHERE id=$1", prepared.ID).Scan(&state); err != nil || state != "Prepared" {
				t.Fatalf("read validation upgraded its lock to expire playback: state=%s error=%v", state, err)
			}
		})
	}
}

func TestStorePlaybackValidationRechecksAuthorizationAfterLockWait(t *testing.T) {
	for _, change := range []string{"disabled account", "playback permission", "library permission", "revoked login", "revoked key", "deleted key client"} {
		t.Run(change, func(t *testing.T) {
			ctx, pool, store, owner, ids := playSessionFixture(t, 1)
			if change == "revoked key" || change == "deleted key client" {
				owner = applicationPlaybackOwnerFixture(t, ctx, pool, "validation-authorization")
			}
			prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
			barrier, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			statement, id, want := "UPDATE users SET is_disabled=true WHERE id=$1", owner.UserID, ErrForbidden
			switch change {
			case "playback permission":
				statement = `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`
			case "library permission":
				statement = `UPDATE users SET policy=policy || '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`
				want = ErrNotFound
			case "revoked login", "revoked key":
				statement, id = "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", owner.SessionID
			case "deleted key client":
				statement, id = "DELETE FROM application_key_clients WHERE id=$1", owner.ApplicationClientID
			}
			if _, err := barrier.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackValidationResult, 1)
			go func() {
				session, err := store.GetPlaybackSession(callCtx, owner, prepared.ID)
				results <- playbackValidationResult{session: session, err: err}
			}()
			waitPlaybackValidationBlock(t, callCtx, pool, barrier.Conn().PgConn().PID())
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if result := awaitPlaybackValidation(t, callCtx, results); !errors.Is(result.err, want) {
				t.Fatalf("validation retained authorization after %s: %+v, %v", change, result.session, result.err)
			}
		})
	}
}

func TestStorePlaybackStopWaitsForSharedValidationTransaction(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	validation, access, err := store.beginPlaybackWrite(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(validation)
	if _, err := lockStateItem(ctx, validation, access, prepared.ItemID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnedCanonicalPlaySessionWithLock(ctx, validation, owner, prepared.ID, " FOR SHARE"); err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results := make(chan playbackValidationResult, 1)
	go func() {
		session, _, err := store.ReportPlayback(callCtx, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped"})
		results <- playbackValidationResult{session: session, err: err}
	}()
	waitPlaybackValidationBlock(t, callCtx, pool, validation.Conn().PgConn().PID())
	if err := commitPlaybackWrite(ctx, validation, owner); err != nil {
		t.Fatal(err)
	}
	if result := awaitPlaybackValidation(t, callCtx, results); result.err != nil || result.session.State != "Stopped" {
		t.Fatalf("stop failed after validation released its shared lock: %+v, %v", result.session, result.err)
	}
	if _, err := store.GetPlaybackSession(ctx, owner, prepared.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed stop remains available to validation: %v", err)
	}
}
