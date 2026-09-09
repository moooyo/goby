package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func managedLogin(t *testing.T, ctx context.Context, store *identity.Store, user identity.User, password, kind string) (identity.Credentials, identity.Principal) {
	t.Helper()
	credentials, err := store.Authenticate(ctx, user.Name, password, identity.Client{}, kind)
	if err != nil {
		t.Fatalf("authenticate managed user as %s: %v", kind, err)
	}
	principal, err := store.Resolve(ctx, credentials.Token, kind)
	if err != nil {
		t.Fatalf("resolve managed user as %s: %v", kind, err)
	}
	return credentials, principal
}

func readManagedUser(t *testing.T, ctx context.Context, store *identity.Store, id string) identity.ManagedUser {
	t.Helper()
	user, err := store.GetManagedUser(ctx, id)
	if err != nil {
		t.Fatalf("read managed user: %v", err)
	}
	return user
}

func managedUpdate(user identity.ManagedUser) identity.ManagedUserUpdate {
	return identity.ManagedUserUpdate{
		Revision: user.Revision, Name: user.User.Name,
		IsAdministrator: user.User.IsAdministrator, IsDisabled: user.User.IsDisabled,
		Policy: user.Policy,
	}
}

// Capture secrets only inside the test process; assertion failures never print them.
func managedSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'user', (SELECT to_jsonb(u) FROM users u WHERE id = $1),
		'sessions', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY id)
			FROM sessions s WHERE user_id = $1), '[]'::jsonb))::text`, id).Scan(&snapshot)
	if err != nil {
		t.Fatalf("snapshot managed user state: %v", err)
	}
	return snapshot
}

func assertManagedUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, before string) {
	t.Helper()
	if after := managedSnapshot(t, ctx, pool, id); after != before {
		t.Error("rejected mutation changed the user or authentication sessions")
	}
}

func assertManagedFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var validation *identity.ManagedUserValidationError
	if !errors.Is(err, identity.ErrInvalidInput) || !errors.As(err, &validation) {
		t.Fatalf("validation error = %v, want managed user validation error", err)
	}
	if validation.Fields[field] == "" {
		t.Errorf("validation fields = %#v, want %s", validation.Fields, field)
	}
}

func assertManagedTokenRevoked(t *testing.T, ctx context.Context, store *identity.Store, credentials identity.Credentials, kind string) {
	t.Helper()
	if _, err := store.Resolve(ctx, credentials.Token, kind); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("old %s session resolved after revocation: %v", kind, err)
	}
}

func TestStoreManagedUserConcurrentLastEnabledAdministrator(t *testing.T) {
	for _, action := range []string{"demote", "disable"} {
		t.Run(action, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			first := bootstrapTestAdmin(t, ctx, store)
			second, err := store.CreateUser(ctx, "Second Administrator", "second-password", true)
			if err != nil {
				t.Fatal(err)
			}
			_, firstActor := managedLogin(t, ctx, store, first, "administrator-password", "admin")
			_, secondActor := managedLogin(t, ctx, store, second, "second-password", "admin")
			actors := []identity.Principal{firstActor, secondActor}
			inputs := []identity.ManagedUserUpdate{
				managedUpdate(readManagedUser(t, ctx, store, first.ID)),
				managedUpdate(readManagedUser(t, ctx, store, second.ID)),
			}
			type outcome struct {
				mutation identity.ManagedUserMutation
				err      error
			}
			start := make(chan struct{})
			results := make(chan outcome, len(actors))
			for i, actor := range actors {
				input := inputs[i]
				if action == "demote" {
					input.IsAdministrator = false
				} else {
					input.IsDisabled = true
				}
				// Each actor changes only itself, so the winner cannot invalidate the
				// loser's authorization before the last-administrator check.
				go func(actor identity.Principal, input identity.ManagedUserUpdate) {
					<-start
					mutation, err := store.UpdateManagedUser(ctx, actor, actor.User.ID, input)
					results <- outcome{mutation: mutation, err: err}
				}(actor, input)
			}
			close(start)
			successes, rejected := 0, 0
			for range actors {
				result := <-results
				switch {
				case result.err == nil:
					successes++
					if !result.mutation.CurrentSessionRevoked || result.mutation.User.Revision != 2 {
						t.Error("successful self-mutation did not revoke its actor or advance its revision")
					}
				case errors.Is(result.err, identity.ErrLastAdministrator):
					rejected++
				default:
					t.Errorf("concurrent self-mutation returned unexpected error: %v", result.err)
				}
			}
			if successes != 1 || rejected != 1 {
				t.Errorf("mutation outcomes = %d successes and %d last-administrator rejections, want one each", successes, rejected)
			}
			var remaining, revisions int64
			if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE is_administrator AND NOT is_disabled),
				sum(management_revision)::bigint FROM users`).Scan(&remaining, &revisions); err != nil {
				t.Fatal(err)
			}
			if remaining != 1 || revisions != 3 {
				t.Errorf("remaining administrators = %d, revision sum = %d, want 1 and 3", remaining, revisions)
			}
		})
	}
}

func TestStoreManagedUserRevisionsRejectLostUpdates(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	// Authorization must use the current account, not the caller's role snapshot.
	actor.User.IsAdministrator = false
	viewer, err := store.CreateManagedUser(ctx, actor, "Managed Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create with a valid session and stale role snapshot: %v", err)
	}
	managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	current := readManagedUser(t, ctx, store, viewer.ID)
	if current.Revision != 1 {
		t.Fatalf("new user revision = %d, want 1", current.Revision)
	}
	input := managedUpdate(current)
	input.Name = "Renamed Viewer"
	updated, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input)
	if err != nil || updated.User.Revision != current.Revision+1 || updated.User.User.Name != input.Name || updated.CurrentSessionRevoked {
		t.Fatalf("update did not return the new name and revision: error = %v", err)
	}
	before := managedSnapshot(t, ctx, pool, viewer.ID)
	input.Name = "Stale Replacement"
	if _, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Errorf("stale update error = %v, want ErrRevisionConflict", err)
	}
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	if _, err := store.ResetManagedUserPassword(ctx, actor, viewer.ID, current.Revision, "stale-password"); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Errorf("stale password reset error = %v, want ErrRevisionConflict", err)
	}
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	reset, err := store.ResetManagedUserPassword(ctx, actor, viewer.ID, updated.User.Revision, "replacement-password")
	if err != nil || reset.User.Revision != updated.User.Revision+1 {
		t.Errorf("password reset revision did not advance: error = %v", err)
	}
}

func TestStoreManagedUserPolicyPreservesUnknownFieldsAndCanonicalLibraries(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Policy Managed Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('library-a', 'Library A', 'movies'), ('library-b', 'Library B', 'music')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = $2::jsonb WHERE id = $1`, viewer.ID,
		`{"CustomExtension":{"Marker":"preserved-private-marker","Values":[1,true]},"IsAdministrator":true,"IsDisabled":true}`); err != nil {
		t.Fatal(err)
	}
	input := managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
	input.Policy.EnableAllFolders = false
	input.Policy.EnabledFolders = []string{"library-b", "library-a", "library-b"}
	input.Policy.EnableMediaPlayback = false
	updated, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.User.Policy.EnabledFolders, []string{"library-a", "library-b"}) || updated.User.Policy.EnableAllFolders || updated.User.Policy.EnableMediaPlayback {
		t.Errorf("updated policy did not preserve supported configuration: %#v", updated.User.Policy)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(updated.User.User.Policy, &raw); err != nil {
		t.Fatal(err)
	}
	var extension struct {
		Marker string
		Values []any
	}
	if err := json.Unmarshal(raw["CustomExtension"], &extension); err != nil {
		t.Fatal(err)
	}
	if extension.Marker != "preserved-private-marker" || !reflect.DeepEqual(extension.Values, []any{float64(1), true}) {
		t.Error("updating supported policy fields discarded an unknown extension")
	}
	if string(raw["IsAdministrator"]) != "false" || string(raw["IsDisabled"]) != "false" {
		t.Error("persisted role mirrors disagree with authoritative account columns")
	}
	encoded, err := json.Marshal(updated.User)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "preserved-private-marker") || strings.Contains(string(encoded), "CustomExtension") {
		t.Error("managed user JSON exposed unknown private policy fields")
	}
	before := managedSnapshot(t, ctx, pool, viewer.ID)
	input = managedUpdate(updated.User)
	input.Policy.EnabledFolders = []string{"library-a", "missing-library"}
	_, err = store.UpdateManagedUser(ctx, actor, viewer.ID, input)
	assertManagedFieldError(t, err, "Policy.EnabledFolders")
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
}

func TestStoreManagedUserInvalidInputHasNoSideEffects(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Invalid Managed Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	current := readManagedUser(t, ctx, store, viewer.ID)
	before := managedSnapshot(t, ctx, pool, viewer.ID)
	for _, test := range []struct {
		name, field string
		change      func(*identity.ManagedUserUpdate)
	}{
		{"empty-name", "Name", func(input *identity.ManagedUserUpdate) { input.Name = "   " }},
		{"duplicate-name", "Name", func(input *identity.ManagedUserUpdate) { input.Name = " ADMINISTRATOR " }},
		{"invalid-revision", "Revision", func(input *identity.ManagedUserUpdate) { input.Revision = 0 }},
		{"malformed-library", "Policy.EnabledFolders", func(input *identity.ManagedUserUpdate) { input.Policy.EnabledFolders = []string{" library "} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := managedUpdate(current)
			test.change(&input)
			_, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input)
			assertManagedFieldError(t, err, test.field)
			assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
		})
	}
	_, err = store.ResetManagedUserPassword(ctx, actor, viewer.ID, current.Revision, strings.Repeat("a", 73))
	assertManagedFieldError(t, err, "Password")
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	_, err = store.ResetManagedUserPassword(ctx, actor, viewer.ID, 0, "valid-password")
	assertManagedFieldError(t, err, "Revision")
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	guest, err := store.CreateUser(ctx, "Passwordless Managed Guest", "", false)
	if err != nil {
		t.Fatal(err)
	}
	guestBefore := managedSnapshot(t, ctx, pool, guest.ID)
	input := managedUpdate(readManagedUser(t, ctx, store, guest.ID))
	input.IsAdministrator = true
	_, err = store.UpdateManagedUser(ctx, actor, guest.ID, input)
	assertManagedFieldError(t, err, "IsAdministrator")
	assertManagedUnchanged(t, ctx, pool, guest.ID, guestBefore)
}

func TestStoreManagedUserMutationsRejectStaleActors(t *testing.T) {
	for _, state := range []string{"revoked", "demoted", "disabled", "expired", "wrong-owner", "wrong-kind", "stored-emby-kind"} {
		t.Run(state, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			root := bootstrapTestAdmin(t, ctx, store)
			admin, err := store.CreateUser(ctx, "Stale Administrator", "actor-password", true)
			if err != nil {
				t.Fatal(err)
			}
			viewer, err := store.CreateUser(ctx, "Stale Actor Target", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			credentials, actor := managedLogin(t, ctx, store, admin, "actor-password", "admin")
			viewerCredentials, _ := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
			switch state {
			case "revoked":
				err = store.Revoke(ctx, credentials.Token)
			case "demoted":
				_, err = pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.ID)
			case "disabled":
				_, err = pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", admin.ID)
			case "expired":
				_, err = pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '2 days',
					expires_at = clock_timestamp() - interval '1 day' WHERE id = $1`, actor.SessionID)
			case "wrong-owner":
				actor.User.ID = root.ID
			case "wrong-kind":
				actor.Kind = "emby"
			case "stored-emby-kind":
				_, actor = managedLogin(t, ctx, store, admin, "actor-password", "emby")
				actor.Kind = "admin"
			}
			if err != nil {
				t.Fatalf("invalidate actor: %v", err)
			}
			current := readManagedUser(t, ctx, store, viewer.ID)
			before := managedSnapshot(t, ctx, pool, viewer.ID)
			input := managedUpdate(current)
			input.Name = "Forbidden Rename"
			if _, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("update with %s actor = %v, want ErrUnauthorized", state, err)
			}
			if _, err := store.ResetManagedUserPassword(ctx, actor, viewer.ID, current.Revision, "forbidden-password"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("reset with %s actor = %v, want ErrUnauthorized", state, err)
			}
			if _, err := store.CreateManagedUser(ctx, actor, "Forbidden Creation", "forbidden-password", true); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("create with %s actor = %v, want ErrUnauthorized", state, err)
			}
			assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
			users, err := store.ListUsers(ctx)
			if err != nil || len(users) != 3 {
				t.Errorf("rejected creation changed user count: count = %d, error = %v", len(users), err)
			}
			if _, err := store.Resolve(ctx, viewerCredentials.Token, "emby"); err != nil {
				t.Errorf("rejected mutation revoked the target session: %v", err)
			}
		})
	}
}

func TestStoreManagedUserSelfDemotionRetainsOrdinaryEmbySessions(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	if _, err := store.CreateUser(ctx, "Remaining Administrator", "remaining-password", true); err != nil {
		t.Fatal(err)
	}
	credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	otherAdmin, _ := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	emby, previous := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	input := managedUpdate(readManagedUser(t, ctx, store, admin.ID))
	input.IsAdministrator = false
	result, err := store.UpdateManagedUser(ctx, actor, admin.ID, input)
	if err != nil || !result.CurrentSessionRevoked || result.User.User.IsAdministrator {
		t.Fatalf("self-demotion did not revoke current admin session: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, credentials, "admin")
	assertManagedTokenRevoked(t, ctx, store, otherAdmin, "admin")
	resolved, err := store.Resolve(ctx, emby.Token, "emby")
	if err != nil || resolved.User.IsAdministrator {
		t.Errorf("retained Emby token did not lose administrator role: %v", err)
	}
	refreshed, err := store.RevalidateSession(ctx, previous)
	if err != nil || refreshed.User.IsAdministrator {
		t.Errorf("existing Emby principal did not lose administrator role: %v", err)
	}
	if _, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "admin"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("demoted account authenticated as admin: %v", err)
	}
}

func TestStoreManagedUserReenableDoesNotRestoreOldSessions(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	target, err := store.CreateUser(ctx, "Disabled Administrator", "target-password", true)
	if err != nil {
		t.Fatal(err)
	}
	targetAdmin, _ := managedLogin(t, ctx, store, target, "target-password", "admin")
	targetEmby, _ := managedLogin(t, ctx, store, target, "target-password", "emby")
	input := managedUpdate(readManagedUser(t, ctx, store, target.ID))
	input.IsDisabled = true
	disabled, err := store.UpdateManagedUser(ctx, actor, target.ID, input)
	if err != nil || disabled.CurrentSessionRevoked || !disabled.User.User.IsDisabled {
		t.Fatalf("disable target failed: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, targetAdmin, "admin")
	assertManagedTokenRevoked(t, ctx, store, targetEmby, "emby")
	if _, err := store.Authenticate(ctx, target.Name, "target-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("disabled account authenticated: %v", err)
	}
	input = managedUpdate(disabled.User)
	input.IsDisabled = false
	enabled, err := store.UpdateManagedUser(ctx, actor, target.ID, input)
	if err != nil || enabled.User.User.IsDisabled || enabled.User.Revision != disabled.User.Revision+1 {
		t.Fatalf("reenable target failed: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, targetAdmin, "admin")
	assertManagedTokenRevoked(t, ctx, store, targetEmby, "emby")
	managedLogin(t, ctx, store, target, "target-password", "admin")
	managedLogin(t, ctx, store, target, "target-password", "emby")
}

func TestStoreManagedUserPasswordResetRevokesAllTargetSessions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	actorCredentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	target, err := store.CreateUser(ctx, "Reset Administrator", "target-password", true)
	if err != nil {
		t.Fatal(err)
	}
	old := make(map[string][]identity.Credentials)
	for _, kind := range []string{"admin", "emby"} {
		for range 2 {
			credentials, _ := managedLogin(t, ctx, store, target, "target-password", kind)
			old[kind] = append(old[kind], credentials)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy =
		'{"ResetMarker":"preserved","IsAdministrator":false,"IsDisabled":true}'::jsonb
		WHERE id = $1`, target.ID); err != nil {
		t.Fatal(err)
	}
	current := readManagedUser(t, ctx, store, target.ID)
	reset, err := store.ResetManagedUserPassword(ctx, actor, target.ID, current.Revision, "new-target-password")
	if err != nil || reset.CurrentSessionRevoked || !reset.User.User.HasPassword || reset.User.Revision != current.Revision+1 {
		t.Fatalf("target password reset failed: %v", err)
	}
	assertStoredUserPolicy(t, reset.User.User, map[string]any{
		"ResetMarker": "preserved", "IsAdministrator": true, "IsDisabled": false,
	})
	for kind, credentials := range old {
		for _, credential := range credentials {
			assertManagedTokenRevoked(t, ctx, store, credential, kind)
		}
	}
	if _, err := store.Authenticate(ctx, target.Name, "target-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("old target password authenticated after reset: %v", err)
	}
	managedLogin(t, ctx, store, target, "new-target-password", "admin")
	managedLogin(t, ctx, store, target, "new-target-password", "emby")
	if _, err := store.Resolve(ctx, actorCredentials.Token, "admin"); err != nil {
		t.Errorf("resetting another user revoked the actor: %v", err)
	}
	otherAdmin, _ := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	actorEmby, _ := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	self := readManagedUser(t, ctx, store, admin.ID)
	before := managedSnapshot(t, ctx, pool, admin.ID)
	_, err = store.ResetManagedUserPassword(ctx, actor, admin.ID, self.Revision, "")
	assertManagedFieldError(t, err, "Password")
	assertManagedUnchanged(t, ctx, pool, admin.ID, before)
	selfReset, err := store.ResetManagedUserPassword(ctx, actor, admin.ID, self.Revision, "new-administrator-password")
	if err != nil || !selfReset.CurrentSessionRevoked || selfReset.User.Revision != self.Revision+1 {
		t.Fatalf("self password reset did not revoke current session: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, actorCredentials, "admin")
	assertManagedTokenRevoked(t, ctx, store, otherAdmin, "admin")
	assertManagedTokenRevoked(t, ctx, store, actorEmby, "emby")
	if _, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "admin"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("old administrator password authenticated after self-reset: %v", err)
	}
	managedLogin(t, ctx, store, admin, "new-administrator-password", "admin")
}

func TestStoreManagedUserPasswordResetAllowsPasswordlessMember(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Passwordless Reset Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	current := readManagedUser(t, ctx, store, viewer.ID)
	reset, err := store.ResetManagedUserPassword(ctx, actor, viewer.ID, current.Revision, "")
	if err != nil || reset.User.User.HasPassword || reset.CurrentSessionRevoked {
		t.Fatalf("empty member password reset failed: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, old, "emby")
	if _, err := store.Authenticate(ctx, viewer.Name, "viewer-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("old member password authenticated after reset: %v", err)
	}
	managedLogin(t, ctx, store, viewer, "", "emby")
}

// Observe the actual blocking relationship rather than guessing when bcrypt or
// a transaction has reached its next statement. Every wait has a deadline.
func waitManagedBlockedQuery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blockerPID int32, queryFragment string, done <-chan struct{}) int32 {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waitingPID int32
		err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT pid FROM pg_stat_activity
			WHERE datname = current_database() AND $1::integer = ANY(pg_blocking_pids(pid))
			AND position($2::text in query) > 0 ORDER BY pid LIMIT 1), 0)`, blockerPID, queryFragment).Scan(&waitingPID)
		if err != nil {
			t.Fatalf("observe managed user lock wait: %v", err)
		}
		if waitingPID != 0 {
			return waitingPID
		}
		select {
		case <-done:
			t.Fatal("operation completed before reaching the expected database lock")
		case <-ctx.Done():
			t.Fatal("timed out waiting for the managed user database lock")
		case <-ticker.C:
		}
	}
}

func TestStoreAuthenticateReturnsTheLockedAccountSnapshot(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	bootstrapTestAdmin(t, testCtx, store)
	target, err := store.CreateUser(testCtx, "Original Login Account", "target-password", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = blocker.Rollback(cleanupCtx)
	}()
	var blockerPID int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, `UPDATE users SET name = 'Updated Login Account',
		normalized_name = 'updated login account', is_administrator = false,
		policy = '{"EnableMediaPlayback":false,"SnapshotMarker":"current-account"}'::jsonb
		WHERE id = $1`, target.ID); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		credentials identity.Credentials
		err         error
	}
	results := make(chan outcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		credentials, err := store.Authenticate(ctx, target.Name, "target-password", identity.Client{}, "emby")
		results <- outcome{credentials: credentials, err: err}
	}()
	// The initial lookup sees the old committed name and administrator role;
	// issuance waits until the changed account becomes visible under its lock.
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "WITH account AS MATERIALIZED", done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var result outcome
	select {
	case result = <-results:
	case <-ctx.Done():
		t.Fatal("authentication did not finish after releasing the account lock")
	}
	if result.err != nil {
		t.Fatalf("authenticate after concurrent account update: %v", result.err)
	}
	if result.credentials.User.Name != "Updated Login Account" || result.credentials.User.IsAdministrator || result.credentials.User.ID != target.ID {
		t.Error("authentication returned the account snapshot from before password verification")
	}
	assertStoredUserPolicy(t, result.credentials.User, map[string]any{
		"EnableMediaPlayback": false, "SnapshotMarker": "current-account",
	})
	resolved, err := store.Resolve(ctx, result.credentials.Token, "emby")
	if err != nil || !reflect.DeepEqual(resolved.User, result.credentials.User) {
		t.Errorf("issued credential account differs from the current resolved account: %v", err)
	}
}

func TestStoreManagedUserRejectsActorInvalidatedWhileWaiting(t *testing.T) {
	for _, state := range []string{"revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			viewer, err := store.CreateUser(testCtx, "Queued Actor Target", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			input := managedUpdate(readManagedUser(t, testCtx, store, viewer.ID))
			input.Name = "Forbidden Queued Rename"
			before := managedSnapshot(t, testCtx, pool, viewer.ID)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				_ = blocker.Rollback(cleanupCtx)
			}()
			var blockerPID int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := store.UpdateManagedUser(ctx, actor, viewer.ID, input)
				results <- err
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions", done)
			statement := "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1"
			if state == "expired" {
				// The expiry is later than the mutation transaction's frozen now(),
				// but already elapsed when its blocked session lock is released.
				statement = "UPDATE sessions SET expires_at = clock_timestamp() WHERE id = $1"
			}
			if _, err := blocker.Exec(ctx, statement, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-results:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Errorf("queued mutation accepted %s actor: %v", state, err)
				}
			case <-ctx.Done():
				t.Fatal("queued mutation did not finish after its actor became invalid")
			}
			assertManagedUnchanged(t, testCtx, pool, viewer.ID, before)
		})
	}
}

func TestStoreManagedUserMutationRejectsLoginWithPreviouslyReadCredentials(t *testing.T) {
	for _, action := range []string{"password-reset", "disable"} {
		t.Run(action, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			target, err := store.CreateUser(testCtx, "Concurrent Login Target", "target-password", false)
			if err != nil {
				t.Fatal(err)
			}
			old, _ := managedLogin(t, testCtx, store, target, "target-password", "emby")
			current := readManagedUser(t, testCtx, store, target.ID)
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				_ = blocker.Rollback(cleanupCtx)
			}()
			var blockerPID int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			type mutationOutcome struct {
				mutation identity.ManagedUserMutation
				err      error
			}
			mutations := make(chan mutationOutcome, 1)
			mutationDone := make(chan struct{})
			go func() {
				defer close(mutationDone)
				var result identity.ManagedUserMutation
				var err error
				if action == "password-reset" {
					result, err = store.ResetManagedUserPassword(ctx, actor, target.ID, current.Revision, "new-target-password")
				} else {
					input := managedUpdate(current)
					input.IsDisabled = true
					result, err = store.UpdateManagedUser(ctx, actor, target.ID, input)
				}
				mutations <- mutationOutcome{mutation: result, err: err}
			}()
			// The mutation holds its account locks before waiting for the actor
			// session. Authentication can still read the old committed password.
			mutationPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions", mutationDone)
			logins := make(chan error, 1)
			loginDone := make(chan struct{})
			go func() {
				defer close(loginDone)
				_, err := store.Authenticate(ctx, target.Name, "target-password", identity.Client{}, "emby")
				logins <- err
			}()
			waitManagedBlockedQuery(t, ctx, pool, mutationPID, "WITH account AS MATERIALIZED", loginDone)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-mutations:
				if result.err != nil || result.mutation.CurrentSessionRevoked || result.mutation.User.Revision != current.Revision+1 {
					t.Fatalf("concurrent managed mutation failed: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("managed mutation did not finish after releasing the actor session")
			}
			select {
			case err := <-logins:
				if !errors.Is(err, identity.ErrInvalidCredentials) {
					t.Errorf("login issued access from previously read credentials: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("authentication did not finish after the managed mutation")
			}
			assertManagedTokenRevoked(t, ctx, store, old, "emby")
			var active int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", target.ID).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if active != 0 {
				t.Errorf("concurrent mutation left %d unrevoked target sessions", active)
			}
			if action == "password-reset" {
				managedLogin(t, ctx, store, target, "new-target-password", "emby")
			} else if !readManagedUser(t, ctx, store, target.ID).User.IsDisabled {
				t.Error("concurrent disable did not persist the disabled account state")
			}
		})
	}
}
