package identity_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func assertUserQueryReviewPage(t *testing.T, ctx context.Context, store *identity.Store, actor identity.Principal, query identity.UserQuery, wantNames []string, wantTotal int64) identity.UserQueryPage {
	t.Helper()
	page, err := store.QueryUsers(ctx, actor, query)
	if err != nil {
		t.Fatalf("query user directory: %v", err)
	}
	names := make([]string, 0, len(page.Items))
	for _, user := range page.Items {
		names = append(names, user.Name)
	}
	if page.Items == nil || !slices.Equal(names, wantNames) || page.TotalRecordCount != wantTotal {
		t.Fatalf("directory names = %v, total = %d, nil items = %v; want %v, %d and non-nil items",
			names, page.TotalRecordCount, page.Items == nil, wantNames, wantTotal)
	}
	return page
}

func TestStoreUserQueryReviewSQLPagination(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	users := map[string]identity.User{admin.Name: actor.User}
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Zulu"} {
		user, err := store.CreateUser(ctx, name, "directory-password", false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=$2,
			policy=jsonb_build_object('IsHidden', true, 'DirectoryMarker', name),
			configuration=jsonb_build_object('DirectoryMarker', name, 'OpaquePayload', $3::text),
			local_password_hash=password_hash, profile_pin_ciphertext=decode(repeat('01',36),'hex')
			WHERE id=$1`, user.ID, name == "Bravo" || name == "Echo", strings.Repeat(name, 1024)); err != nil {
			t.Fatal(err)
		}
		user, err = store.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		users[name] = user
	}
	disabled, enabled := true, false
	for _, test := range []struct {
		name  string
		query identity.UserQuery
		want  []string
		total int64
	}{
		{"all", identity.UserQuery{Limit: 20}, []string{"Administrator", "Alpha", "Bravo", "Charlie", "Delta", "Echo", "Zulu"}, 7},
		{"first page", identity.UserQuery{Limit: 2}, []string{"Administrator", "Alpha"}, 7},
		{"middle page", identity.UserQuery{StartIndex: 2, Limit: 2}, []string{"Bravo", "Charlie"}, 7},
		{"partial last page", identity.UserQuery{StartIndex: 6, Limit: 2}, []string{"Zulu"}, 7},
		{"end boundary", identity.UserQuery{StartIndex: 7, Limit: 2}, nil, 7},
		{"past end", identity.UserQuery{StartIndex: 2147483647, Limit: 2}, nil, 7},
		{"count only", identity.UserQuery{Limit: 0}, nil, 7},
		{"count only ignores offset", identity.UserQuery{StartIndex: 2147483647, Limit: 0}, nil, 7},
		{"name lower bound", identity.UserQuery{NameStartsWithOrGreater: "bRaVo", Limit: 2}, []string{"Bravo", "Charlie"}, 5},
		{"literal name wildcard", identity.UserQuery{NameStartsWithOrGreater: "Bravo%_", Limit: 20}, []string{"Charlie", "Delta", "Echo", "Zulu"}, 4},
		{"empty filter", identity.UserQuery{NameStartsWithOrGreater: "ZZZZ", Limit: 2}, nil, 0},
		{"empty count", identity.UserQuery{NameStartsWithOrGreater: "ZZZZ", Limit: 0}, nil, 0},
		{"disabled page", identity.UserQuery{IsDisabled: &disabled, StartIndex: 1, Limit: 2}, []string{"Echo"}, 2},
		{"disabled count", identity.UserQuery{IsDisabled: &disabled, Limit: 0}, nil, 2},
		{"enabled page", identity.UserQuery{IsDisabled: &enabled, StartIndex: 1, Limit: 2}, []string{"Alpha", "Charlie"}, 5},
		{"descending", identity.UserQuery{Descending: true, StartIndex: 1, Limit: 3}, []string{"Echo", "Delta", "Charlie"}, 7},
		{"descending lower bound", identity.UserQuery{NameStartsWithOrGreater: "bravo", Descending: true, StartIndex: 2, Limit: 2}, []string{"Delta", "Charlie"}, 5},
		{"combined filter", identity.UserQuery{IsDisabled: &disabled, NameStartsWithOrGreater: "Charlie", Descending: true, Limit: 2}, []string{"Echo"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := assertUserQueryReviewPage(t, ctx, store, actor, test.query, test.want, test.total)
			for _, user := range page.Items {
				if !reflect.DeepEqual(user, users[user.Name]) {
					t.Fatalf("directory page changed persisted account fields for %q", user.Name)
				}
			}
		})
	}
}

func TestStoreUserQueryReviewHiddenMalformedPolicies(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	for _, fixture := range []struct {
		name, policy string
		disabled     bool
	}{
		{"Visibility A Default", `{}`, false},
		{"Visibility B Visible", `{"IsHidden":false}`, false},
		{"Visibility C Hidden", `{"IsHidden":true}`, false},
		{"Visibility D Invalid Flag", `{"IsHidden":"false"}`, false},
		{"Visibility E Invalid Access", `{"IsHidden":false,"EnableRemoteAccess":null}`, true},
		{"Visibility F Scalar", `null`, false},
		{"Visibility G Case Alias", `{"IsHidden":false,"ishidden":false}`, false},
		{"Visibility H Playback Fallback", `{"IsHidden":false,"EnableMediaPlayback":"false"}`, false},
		{"Visibility I Array", `[]`, true},
	} {
		user, err := store.CreateUser(ctx, fixture.name, "directory-password", false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb, is_disabled=$3 WHERE id=$1", user.ID, fixture.policy, fixture.disabled); err != nil {
			t.Fatal(err)
		}
	}
	hidden, visible, disabled := true, false, true
	for _, test := range []struct {
		name  string
		query identity.UserQuery
		want  []string
		total int64
	}{
		{"unfiltered malformed page", identity.UserQuery{StartIndex: 3, Limit: 2}, []string{"Visibility D Invalid Flag", "Visibility E Invalid Access"}, 9},
		{"hidden", identity.UserQuery{IsHidden: &hidden, Limit: 20}, []string{"Visibility C Hidden", "Visibility D Invalid Flag", "Visibility E Invalid Access", "Visibility F Scalar", "Visibility G Case Alias", "Visibility I Array"}, 6},
		{"visible", identity.UserQuery{IsHidden: &visible, Limit: 20}, []string{"Visibility A Default", "Visibility B Visible", "Visibility H Playback Fallback"}, 3},
		{"hidden descending page", identity.UserQuery{IsHidden: &hidden, Descending: true, StartIndex: 1, Limit: 2}, []string{"Visibility G Case Alias", "Visibility F Scalar"}, 6},
		{"hidden disabled", identity.UserQuery{IsHidden: &hidden, IsDisabled: &disabled, Limit: 20}, []string{"Visibility E Invalid Access", "Visibility I Array"}, 2},
		{"hidden count", identity.UserQuery{IsHidden: &hidden, Limit: 0}, nil, 6},
		{"hidden past end", identity.UserQuery{IsHidden: &hidden, StartIndex: 6, Limit: 2}, nil, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.NameStartsWithOrGreater = "Visibility"
			assertUserQueryReviewPage(t, ctx, store, actor, test.query, test.want, test.total)
		})
	}
}

func TestStoreUserQueryReviewTiedOrdering(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	var users []identity.User
	for _, name := range []string{"Tied Alpha", "Tied Bravo", "Tied Charlie"} {
		user, err := store.CreateUser(ctx, name, "directory-password", false)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
	}
	// This isolated schema bypasses account-name uniqueness to exercise the
	// directory's explicit identifier tie-breaker in both ordering directions.
	if _, err := pool.Exec(ctx, `ALTER TABLE users DROP CONSTRAINT users_normalized_name_key;
		UPDATE users SET normalized_name='tied' WHERE NOT is_administrator`); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(users, func(a, b identity.User) int { return strings.Compare(a.ID, b.ID) })
	assertUserQueryReviewPage(t, ctx, store, actor,
		identity.UserQuery{NameStartsWithOrGreater: "Tied", StartIndex: 1, Limit: 2},
		[]string{users[1].Name, users[2].Name}, 3)
	assertUserQueryReviewPage(t, ctx, store, actor,
		identity.UserQuery{NameStartsWithOrGreater: "Tied", Descending: true, StartIndex: 1, Limit: 2},
		[]string{users[1].Name, users[0].Name}, 3)
}

func TestStoreUserQueryReviewRevocationDuringActorWait(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	hidden := true
	for _, test := range []struct {
		name  string
		query identity.UserQuery
	}{
		{"page", identity.UserQuery{Limit: 2}},
		{"count", identity.UserQuery{Limit: 0}},
		{"hidden", identity.UserQuery{IsHidden: &hidden, Limit: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", admin.ID); err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				page identity.UserQueryPage
				err  error
			}
			results, done := make(chan outcome, 1), make(chan struct{})
			go func() {
				defer close(done)
				page, err := store.QueryUsers(ctx, actor, test.query)
				results <- outcome{page: page, err: err}
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM users", done)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-results:
				if !errors.Is(result.err, identity.ErrUnauthorized) || len(result.page.Items) != 0 || result.page.TotalRecordCount != 0 {
					t.Fatalf("revoked directory actor returned %d users, total %d, error %v", len(result.page.Items), result.page.TotalRecordCount, result.err)
				}
			case <-ctx.Done():
				t.Fatal("directory query did not finish after its actor lock was released")
			}
		})
	}
}
