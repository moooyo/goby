package identity_test

import (
	"slices"
	"testing"
)

func TestStoreDeleteManagedUserReturnsOnlyLockedTargetSessions(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		self    bool
		history bool
	}{
		{"empty target", false, false},
		{"other target history", false, true},
		{"self history", true, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
			extraActorSession, _ := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
			other, err := store.CreateUser(ctx, "Deletion Session Target", "target-password", true)
			if err != nil {
				t.Fatal(err)
			}
			target, password := other, "target-password"
			want := make([]string, 0)
			if fixture.self {
				target, password = admin, "administrator-password"
				want = append(want, actor.SessionID, extraActorSession.SessionID)
			}
			if fixture.history {
				active, _ := managedLogin(t, ctx, store, target, password, "admin")
				expired, _ := managedLogin(t, ctx, store, target, password, "emby")
				revoked, _ := managedLogin(t, ctx, store, target, password, "emby")
				if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',
					expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.SessionID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", revoked.SessionID); err != nil {
					t.Fatal(err)
				}
				want = append(want, active.SessionID, expired.SessionID, revoked.SessionID)
			}
			slices.Sort(want)
			result, err := store.DeleteManagedUser(ctx, actor, target.ID, 1)
			if err != nil || result.CurrentSessionRevoked != fixture.self || result.RevokedSessionIDs == nil ||
				!slices.Equal(result.RevokedSessionIDs, want) {
				t.Fatalf("deletion did not retain exactly its sorted target-session handles: %v", err)
			}
			var remaining int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1", target.ID).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("target sessions survived committed deletion: %v", err)
			}
			if !fixture.self {
				if _, err := store.Resolve(ctx, extraActorSession.Token, "emby"); err != nil {
					t.Fatalf("deleting another account retired the actor's unrelated session: %v", err)
				}
			}
		})
	}
}

func TestStoreDeleteManagedUserDoesNotExposeSessionHandlesAfterRollback(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	target, err := store.CreateUser(ctx, "Rollback Session Target", "target-password", false)
	if err != nil {
		t.Fatal(err)
	}
	managedLogin(t, ctx, store, target, "target-password", "emby")
	rejectIdentityActivity(t, ctx, pool)
	before := managedDeletionSnapshot(t, ctx, pool)
	result, err := store.DeleteManagedUser(ctx, actor, target.ID, 1)
	if err == nil || result.CurrentSessionRevoked || result.RevokedSessionIDs != nil || result.CollectionsChanged {
		t.Fatal("failed deletion exposed session cleanup handles before a successful commit")
	}
	if managedDeletionSnapshot(t, ctx, pool) != before {
		t.Fatal("audit rejection retained account, session or audit changes")
	}
}
