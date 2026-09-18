package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func stateWriteActorFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) identity.Principal {
	t.Helper()
	actor := metadataEditTestActor(t, ctx, pool, "z-state-actor")
	actor.Kind, actor.PeerIP = "emby", "192.168.1.20"
	if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'emby' WHERE id = $1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	return actor
}

func stateWriteSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(data) ORDER BY user_id, item_id), '[]'::jsonb)::text
		FROM user_item_data data`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func stateWriteOperation(ctx context.Context, store *Store, subject Subject, operation string) error {
	if operation == "favorite" {
		_, err := store.SetFavoriteFor(ctx, subject, "movie-b", true)
		return err
	}
	_, err := store.SetPlayedFor(ctx, subject, "season-b", true, nil)
	return err
}

func TestUserStateReauthorizesActorAfterTargetLockWait(t *testing.T) {
	for _, operation := range []string{"favorite", "played"} {
		for _, change := range []string{"revoke", "demote", "disable"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				ctx, store := libraryQueryTestStore(t)
				seedLibraryQueryFixture(t, ctx, store.pool)
				actor := stateWriteActorFixture(t, ctx, store.pool)
				subject := Subject{UserID: "default", Actor: &actor}
				userDataSeed(t, ctx, store.pool, subject.UserID, UserData{ItemID: "episode-b1", PlayCount: 3, PlaybackPositionTicks: 19})
				before := stateWriteSnapshot(t, ctx, store.pool)
				blocker, err := store.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(blocker)
				if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", subject.UserID); err != nil {
					t.Fatal(err)
				}
				operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				finished := make(chan error, 1)
				go func() { finished <- stateWriteOperation(operationCtx, store, subject, operation) }()
				waitCatalogApplicationBlock(t, operationCtx, store.pool, blocker.Conn().PgConn().PID())
				// The target sorts before the actor, so the actor can change while
				// this write is demonstrably waiting for the target account.
				statement, id := "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID
				if change == "demote" {
					statement, id = "UPDATE users SET is_administrator = false WHERE id = $1", actor.User.ID
				} else if change == "disable" {
					statement, id = "UPDATE users SET is_disabled = true WHERE id = $1", actor.User.ID
				}
				changeCtx, changeCancel := context.WithTimeout(ctx, 2*time.Second)
				_, err = store.pool.Exec(changeCtx, statement, id)
				changeCancel()
				if err != nil {
					t.Fatalf("actor change was blocked behind a target account: %v", err)
				}
				if err := blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					if !errors.Is(err, ErrForbidden) {
						t.Fatalf("state write after %s = %v; want forbidden", change, err)
					}
				case <-operationCtx.Done():
					t.Fatal("state writer did not finish after releasing the target")
				}
				if after := stateWriteSnapshot(t, ctx, store.pool); after != before {
					t.Fatal("rejected actor changed another account's user data")
				}
			})
		}
	}
}

func TestUserStateRechecksExpiryAfterBusinessLockWait(t *testing.T) {
	for _, operation := range []string{"favorite", "played"} {
		t.Run(operation, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			actor := stateWriteActorFixture(t, ctx, store.pool)
			subject := Subject{UserID: "default", Actor: &actor}
			userDataSeed(t, ctx, store.pool, subject.UserID, UserData{ItemID: "episode-b1", PlayCount: 2, PlaybackPositionTicks: 17})
			before := stateWriteSnapshot(t, ctx, store.pool)
			blocker, err := store.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(blocker)
			itemID := "movie-b"
			if operation == "played" {
				itemID = "season-b"
			}
			if _, err := blocker.Exec(ctx, "SELECT id FROM items WHERE id = $1 FOR UPDATE", itemID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, "UPDATE sessions SET expires_at = clock_timestamp() + interval '2 seconds' WHERE id = $1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- stateWriteOperation(operationCtx, store, subject, operation) }()
			waitCatalogApplicationBlock(t, operationCtx, store.pool, blocker.Conn().PgConn().PID())
			waitStateSessionExpiry(t, operationCtx, store.pool, actor.SessionID)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("state write after expiry = %v; want forbidden", err)
				}
			case <-operationCtx.Done():
				t.Fatal("expired state writer did not finish")
			}
			if after := stateWriteSnapshot(t, ctx, store.pool); after != before {
				t.Fatal("expiry during a business lock wait changed user data")
			}
		})
	}
}

func waitStateSessionExpiry(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		if err := pool.QueryRow(ctx, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", sessionID).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("session did not reach its configured expiry")
		}
	}
}

func TestUserStateUsesCurrentActorPolicyAndPersistedDevice(t *testing.T) {
	for _, operation := range []string{"favorite", "played"} {
		for _, restriction := range []string{"device", "schedule", "remote", "lockout"} {
			t.Run(operation+"/"+restriction, func(t *testing.T) {
				ctx, store := libraryQueryTestStore(t)
				seedLibraryQueryFixture(t, ctx, store.pool)
				actor := stateWriteActorFixture(t, ctx, store.pool)
				actor.PeerIP, actor.Client.DeviceID = "8.8.8.8", "claimed-device"
				subject := Subject{UserID: actor.User.ID, Actor: &actor}
				before := stateWriteSnapshot(t, ctx, store.pool)
				policy := map[string]any{}
				switch restriction {
				case "device":
					policy["EnableAllDevices"], policy["EnabledDevices"] = false, []string{actor.Client.DeviceID}
				case "schedule":
					var now time.Time
					if err := store.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
						t.Fatal(err)
					}
					policy["AccessSchedules"] = []identity.AccessSchedule{{DayOfWeek: now.Local().AddDate(0, 0, 1).Weekday().String(), StartHour: 0, EndHour: 24}}
				case "remote":
					policy["EnableRemoteAccess"] = false
				case "lockout":
					policy["LockedOutDate"] = 1
				}
				raw, err := json.Marshal(policy)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.pool.Exec(ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", actor.User.ID, raw); err != nil {
					t.Fatal(err)
				}
				if err := stateWriteOperation(ctx, store, subject, operation); !errors.Is(err, ErrForbidden) {
					t.Fatalf("state write with current %s restriction = %v; want forbidden", restriction, err)
				}
				if after := stateWriteSnapshot(t, ctx, store.pool); after != before {
					t.Fatal("rejected policy changed user data")
				}
			})
		}
	}
}

func TestUserStateActorDoesNotReplaceTargetCatalogAuthority(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	actor := stateWriteActorFixture(t, ctx, store.pool)
	subject := Subject{UserID: "restricted", Actor: &actor}
	if _, err := store.SetFavoriteFor(ctx, subject, "movie-b", true); err != nil {
		t.Fatalf("current administrator could not update a target's visible state: %v", err)
	}
	if _, err := store.SetFavoriteFor(ctx, subject, "movie-a", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("administrator actor replaced the target's catalog scope: %v", err)
	}
	if _, err := store.SetFavoriteFor(ctx, Subject{UserID: "disabled", Actor: &actor}, "movie-b", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary administrator wrote disabled target state: %v", err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetFavoriteFor(ctx, subject, "movie-b", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stale administrator principal granted cross-user authority: %v", err)
	}
	if _, err := store.SetFavoriteFor(ctx, Subject{UserID: "default", Actor: &identity.Principal{}}, "movie-b", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an empty actor downgraded to a trusted internal caller: %v", err)
	}
}
