package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func userActivityTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *identity.Store, identity.Principal) {
	t.Helper()
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	return ctx, pool, store, actor
}

func userActivityItem(t *testing.T, page identity.UserActivityPage, id string) identity.UserActivity {
	t.Helper()
	if page.Items == nil || page.ObservedAt.IsZero() {
		t.Fatal("activity page omitted its non-null list or observation time")
	}
	for _, item := range page.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatal("activity page omitted the requested account")
	return identity.UserActivity{}
}

func TestStoreUserActivityRetainsActualLoginAfterSessionRemoval(t *testing.T) {
	ctx, pool, store, actor := userActivityTestStore(t)
	viewer, err := store.CreateUser(ctx, "Activity viewer", "activity-password", false)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.ListUserActivity(ctx, actor, []string{viewer.ID, actor.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	item := userActivityItem(t, page, viewer.ID)
	if item.LastLoginDate != nil || item.LastActivityDate != nil || item.LastPlayedDate != nil || item.PlayedItemCount != 0 {
		t.Fatal("an account without a login was assigned synthetic activity")
	}
	if _, err := store.Authenticate(ctx, viewer.Name, "incorrect-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("invalid password did not fail authentication")
	}
	credentials, principal := managedLogin(t, ctx, store, viewer, "activity-password", "emby")
	page, err = store.ListUserActivity(ctx, actor, []string{viewer.ID})
	if err != nil {
		t.Fatal(err)
	}
	item = userActivityItem(t, page, viewer.ID)
	if item.LastLoginDate == nil || !item.LastLoginDate.Equal(credentials.CreatedAt) || item.LastActivityDate == nil || !item.LastActivityDate.Equal(credentials.CreatedAt) {
		t.Fatal("login summary does not match the issued credential timestamp")
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchClientSession(ctx, principal); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListUserActivity(ctx, actor, []string{viewer.ID})
	if err != nil {
		t.Fatal(err)
	}
	active := userActivityItem(t, page, viewer.ID)
	if active.LastActivityDate == nil || !active.LastActivityDate.After(*item.LastActivityDate) || !active.LastLoginDate.Equal(*item.LastLoginDate) {
		t.Fatal("authenticated activity changed the issuance time or failed to advance activity")
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListUserActivity(ctx, actor, []string{viewer.ID})
	if err != nil {
		t.Fatal(err)
	}
	retained := userActivityItem(t, page, viewer.ID)
	if retained.LastLoginDate == nil || retained.LastActivityDate == nil ||
		!retained.LastLoginDate.Equal(*active.LastLoginDate) || !retained.LastActivityDate.Equal(*active.LastActivityDate) {
		t.Fatal("revocation or session cleanup erased recorded user activity")
	}
}

func TestStoreUserActivityLoginSummaryRollsBackWithRejectedAudit(t *testing.T) {
	ctx, pool, store, actor := userActivityTestStore(t)
	viewer, err := store.CreateUser(ctx, "Atomic activity viewer", "activity-password", false)
	if err != nil {
		t.Fatal(err)
	}
	rejectIdentityActivity(t, ctx, pool)
	if _, err := store.Authenticate(ctx, viewer.Name, "activity-password", identity.Client{}, "emby"); err == nil {
		t.Fatal("login committed despite a rejected audit record")
	}
	acceptIdentityActivity(t, ctx, pool)
	page, err := store.ListUserActivity(ctx, actor, []string{viewer.ID})
	if err != nil {
		t.Fatal(err)
	}
	item := userActivityItem(t, page, viewer.ID)
	if item.LastLoginDate != nil || item.LastActivityDate != nil {
		t.Fatal("failed login committed an activity summary")
	}
}

func TestStoreAdministratorActivityConcurrentSamplesDoNotExtendLogin(t *testing.T) {
	ctx, pool, store, actor := userActivityTestStore(t)
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	var beforeExpiry time.Time
	var beforeRevision int64
	if err := pool.QueryRow(ctx, `SELECT a.expires_at,u.management_revision FROM sessions a
		JOIN users u ON u.id=a.user_id WHERE a.id=$1`, actor.SessionID).Scan(&beforeExpiry, &beforeRevision); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for index := 0; index < cap(results); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- store.TouchAdministratorActivity(bounded, actor)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent native activity failed: %v", err)
		}
	}
	var expiry, seen, firstSeen time.Time
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT a.expires_at,a.last_seen_at,u.management_revision FROM sessions a
		JOIN users u ON u.id=a.user_id WHERE a.id=$1`, actor.SessionID).Scan(&expiry, &seen, &revision); err != nil {
		t.Fatal(err)
	}
	if !expiry.Equal(beforeExpiry) || revision != beforeRevision {
		t.Fatal("activity sampling changed the login lifetime or user revision")
	}
	firstSeen = seen
	if err := store.TouchAdministratorActivity(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT last_seen_at FROM sessions WHERE id=$1", actor.SessionID).Scan(&seen); err != nil || !seen.Equal(firstSeen) {
		t.Fatal("activity sampling wrote another timestamp inside its interval")
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchAdministratorActivity(ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked native login was allowed to record activity")
	}
	if _, err := store.ListUserActivity(ctx, actor, []string{actor.User.ID}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked native login was allowed to read account activity")
	}
}

func TestStoreUserActivityWatchedCountsRemainItemState(t *testing.T) {
	ctx, pool, store, actor := userActivityTestStore(t)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('activity-library','Activity library','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('activity-movie','activity-library','Movie','movie','Movie',false),
		('activity-episode','activity-library','Episode','episode','Episode',false),
		('activity-audio','activity-library','Song','song','Audio',false),
		('activity-unfinished','activity-library','Unfinished','unfinished','Movie',false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,played,play_count,last_played_at) VALUES
		($1,'activity-movie',true,20,'2026-01-01T10:00:00Z'),
		($1,'activity-episode',true,3,'2026-01-02T10:00:00Z'),
		($1,'activity-audio',true,50,'2026-01-04T10:00:00Z'),
		($1,'activity-unfinished',false,2,'2026-01-03T10:00:00Z')`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListUserActivity(ctx, actor, []string{actor.User.ID, "missing-account"})
	if err != nil {
		t.Fatal(err)
	}
	item := userActivityItem(t, page, actor.User.ID)
	if len(page.Items) != 1 || item.PlayedItemCount != 2 || item.LastPlayedDate == nil ||
		!item.LastPlayedDate.Equal(time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC)) {
		t.Fatal("watched count became play-count sum or a missing user became a record")
	}
	_, emby := managedLogin(t, ctx, store, actor.User, "administrator-password", "emby")
	if _, err := store.ListUserActivity(ctx, emby, []string{actor.User.ID}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("Emby administrator was allowed through the native activity boundary")
	}
}
