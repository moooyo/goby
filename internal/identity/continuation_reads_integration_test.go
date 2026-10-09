package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type continuationIdentityReadTrace struct {
	selects      atomic.Int64
	begins       atomic.Int64
	commits      atomic.Int64
	afterProfile func(context.Context)
}

type continuationProfileReadKey struct{}

func (trace *continuationIdentityReadTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	sql := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(sql, "select ") {
		trace.selects.Add(1)
	}
	if sql == "begin" {
		trace.begins.Add(1)
	}
	if sql == "commit" {
		trace.commits.Add(1)
	}
	if trace.afterProfile != nil && strings.Contains(sql, "select profile_pin_ciphertext,configuration from users") {
		return context.WithValue(ctx, continuationProfileReadKey{}, true)
	}
	return ctx
}

func (trace *continuationIdentityReadTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil && ctx.Value(continuationProfileReadKey{}) == true && trace.afterProfile != nil {
		trace.afterProfile(ctx)
	}
}

func continuationIdentityReader(t *testing.T, ctx context.Context, pool *pgxpool.Pool, masterPath string) (*identity.Store, *continuationIdentityReadTrace) {
	t.Helper()
	trace := new(continuationIdentityReadTrace)
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	store := identity.New(reader)
	if masterPath != "" {
		store = identity.NewWithApplicationKeyVault(reader, identity.NewApplicationKeyVault(masterPath))
	}
	return store, trace
}

func TestUsersWithDeviceHistoryBatchesOnlyOrdinaryHistoricalLogins(t *testing.T) {
	ctx, pool, _ := identityTestStore(t)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
		SELECT 'history-user-'||n,'History User '||n,'history user '||n,'fixture' FROM generate_series(1,513) n;
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,created_at,expires_at,revoked_at)
		SELECT 'history-session-'||id,id,decode(md5(id)||md5(id),'hex'),'emby','history-device',
		clock_timestamp()-interval '2 days',clock_timestamp()-interval '1 day',clock_timestamp()
		FROM users;
		INSERT INTO users(id,name,normalized_name,password_hash) VALUES
		('history-admin','History Admin','history admin','fixture'),
		('history-other-device','Other Device','other device','fixture');
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at) VALUES
		('history-admin-session','history-admin',decode(repeat('ab',32),'hex'),'admin','history-device',clock_timestamp()+interval '1 hour'),
		('history-other-session','history-other-device',decode(repeat('cd',32),'hex'),'emby','other-device',clock_timestamp()+interval '1 hour');
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		VALUES('history-application',NULL,decode(repeat('ef',32),'hex'),'application_key','history-device',NULL)`); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 517)
	for index := 1; index <= 513; index++ {
		ids = append(ids, fmt.Sprintf("history-user-%d", index))
	}
	ids = append(ids, "history-user-1", "", " invalid-user ", "history-admin", "history-other-device")
	store, trace := continuationIdentityReader(t, ctx, pool, "")
	used, err := store.UsersWithDeviceHistory(ctx, ids, "history-device")
	if err != nil || len(used) != 513 || used["history-admin"] || used["history-other-device"] {
		t.Fatalf("historical ordinary-device projection = %d users, error %v", len(used), err)
	}
	if trace.selects.Load() != 2 || trace.begins.Load() != 0 {
		t.Fatalf("history used %d SELECTs and %d transactions, want two bounded statements", trace.selects.Load(), trace.begins.Load())
	}
	for _, deviceID := range []string{"", " invalid-device ", "invalid\x00device", strings.Repeat("x", 257)} {
		trace.selects.Store(0)
		used, err := store.UsersWithDeviceHistory(ctx, ids, deviceID)
		if err != nil || len(used) != 0 || trace.selects.Load() != 0 {
			t.Fatal("invalid device history performed a query or produced a historical grant")
		}
	}
	trace.selects.Store(0)
	if used, err := store.UsersWithDeviceHistory(ctx, []string{"", " invalid-user "}, "history-device"); err != nil || len(used) != 0 || trace.selects.Load() != 0 {
		t.Fatal("an empty valid-user set queried historical sessions")
	}
}

func TestOwnPreferenceConfigurationCombinesReadAndPreservesPrivateAuthority(t *testing.T) {
	ctx, pool, base := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, base)
	_, native := managedLogin(t, ctx, base, admin, "administrator-password", "admin")
	viewer, err := base.CreateUser(ctx, "Combined configuration owner", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, owner := clientSessionPrincipal(t, ctx, base, viewer, "viewer-password", identity.Client{DeviceID: "combined-owner"}, "emby")
	masterPath := filepath.Join(t.TempDir(), "profile-master.key")
	store := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	if _, err := store.UpdateUserPreferences(ctx, owner, viewer.ID, nil, identity.UserConfigurationPatch{
		"ProfilePin": json.RawMessage(`"4826"`), "SubtitleMode": json.RawMessage(`"Always"`),
	}); err != nil {
		t.Fatal(err)
	}
	reader, trace := continuationIdentityReader(t, ctx, pool, masterPath)
	raw, pin, err := reader.GetOwnPreferenceConfiguration(ctx, owner, viewer.ID)
	if err != nil || pin != "4826" || identity.ProjectUserConfiguration(raw).SubtitleMode != "Always" {
		t.Fatalf("combined owner configuration lost its configuration/PIN pair: %v", err)
	}
	if trace.selects.Load() != 6 || trace.begins.Load() != 1 || trace.commits.Load() != 1 {
		t.Fatalf("combined read used SELECT/BEGIN/COMMIT = %d/%d/%d, want 6/1/1", trace.selects.Load(), trace.begins.Load(), trace.commits.Load())
	}
	for _, actor := range []identity.Principal{native, {Kind: identity.ApplicationKeyKind, SessionID: "application", ApplicationKeyID: 1, ClientSessionID: "client"}} {
		if raw, pin, err := reader.GetOwnPreferenceConfiguration(ctx, actor, viewer.ID); !errors.Is(err, identity.ErrUnauthorized) || raw != nil || pin != "" {
			t.Fatal("a non-owner credential received the private configuration projection")
		}
	}
	if _, _, err := reader.GetOwnPreferenceConfiguration(ctx, owner, admin.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("an owner credential selected another account's private configuration")
	}
	for _, policy := range []string{`{"EnableUserPreferenceAccess":false}`, `{"RestrictedFeatures":["goby_preferences"]}`} {
		if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
			t.Fatal(err)
		}
		if raw, pin, err := reader.GetOwnPreferenceConfiguration(ctx, owner, viewer.ID); !errors.Is(err, identity.ErrClientSessionForbidden) || raw != nil || pin != "" {
			t.Fatal("the Configuration endpoint ignored its preference gate")
		}
		if _, pin, err := reader.GetOwnProfileConfiguration(ctx, owner, viewer.ID); err != nil || pin != "4826" {
			t.Fatal("endpoint-specific preference gates changed the login/Users/Me PIN contract")
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET policy='{}',profile_pin_ciphertext=decode(repeat('00',36),'hex') WHERE id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if raw, pin, err := reader.GetOwnPreferenceConfiguration(ctx, owner, viewer.ID); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || raw != nil || pin != "" {
		t.Fatal("a vault failure returned a partial private configuration")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET profile_pin_ciphertext=NULL WHERE id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, pin, err := reader.GetOwnPreferenceConfiguration(ctx, owner, viewer.ID); err != nil || pin != "" {
		t.Fatalf("an owner without a PIN could not read configuration: %v", err)
	}
}

func TestOwnPreferenceConfigurationRechecksAuthorityAfterLockWait(t *testing.T) {
	for _, scenario := range []string{"policy", "revocation", "expiration"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			bootstrapTestAdmin(t, ctx, store)
			user, err := store.CreateUser(ctx, "Waiting combined owner", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := clientSessionPrincipal(t, ctx, store, user, "viewer-password", identity.Client{DeviceID: "waiting-combined"}, "emby")
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			var pid int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", user.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var raw json.RawMessage
			var pin string
			var readErr error
			go func() {
				defer close(done)
				raw, pin, readErr = store.GetOwnPreferenceConfiguration(ctx, actor, user.ID)
			}()
			waitManagedBlockedQuery(t, ctx, pool, pid, "SELECT id FROM users", done)
			want := identity.ErrUnauthorized
			switch scenario {
			case "policy":
				_, err = blocker.Exec(ctx, `UPDATE users SET policy='{"EnableUserPreferenceAccess":false}' WHERE id=$1`, user.ID)
				want = identity.ErrClientSessionForbidden
			case "revocation":
				_, err = blocker.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID)
			case "expiration":
				_, err = blocker.Exec(ctx, "UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1", actor.SessionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("combined read did not finish after its authority changed")
			}
			if !errors.Is(readErr, want) || raw != nil || pin != "" {
				t.Fatalf("combined read after %s returned %v and private data", scenario, readErr)
			}
		})
	}
}

func TestOwnPreferenceConfigurationKeepsMixedUpdateTogether(t *testing.T) {
	ctx, pool, base := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, base)
	user, err := base.CreateUser(ctx, "Atomic combined owner", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, owner := clientSessionPrincipal(t, ctx, base, user, "viewer-password", identity.Client{DeviceID: "atomic-combined"}, "emby")
	store := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "profile-master.key")))
	if _, err := store.UpdateUserPreferences(ctx, owner, user.ID, nil, identity.UserConfigurationPatch{
		"ProfilePin": json.RawMessage(`"1357"`), "SubtitleMode": json.RawMessage(`"Always"`),
	}); err != nil {
		t.Fatal(err)
	}
	var replacement []byte
	if err := pool.QueryRow(ctx, "SELECT profile_pin_ciphertext FROM users WHERE id=$1", user.ID).Scan(&replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUserPreferences(ctx, owner, user.ID, nil, identity.UserConfigurationPatch{
		"ProfilePin": json.RawMessage(`"4826"`), "SubtitleMode": json.RawMessage(`"None"`),
	}); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var pid int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, `UPDATE users SET profile_pin_ciphertext=$2,
		configuration='{"SubtitleMode":"Always"}' WHERE id=$1`, user.ID, replacement); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var raw json.RawMessage
	var pin string
	var readErr error
	go func() {
		defer close(done)
		raw, pin, readErr = store.GetOwnPreferenceConfiguration(ctx, owner, user.ID)
	}()
	waitManagedBlockedQuery(t, ctx, pool, pid, "SELECT id FROM users", done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("combined read did not finish after the mixed update")
	}
	if readErr != nil || pin != "1357" || identity.ProjectUserConfiguration(raw).SubtitleMode != "Always" {
		t.Fatalf("combined read returned a torn configuration/PIN pair: %v", readErr)
	}
}

func TestOwnPreferenceConfigurationChecksExpiryAfterPrivateRead(t *testing.T) {
	ctx, pool, base := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, base)
	user, err := base.CreateUser(ctx, "Expiring combined owner", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, owner := clientSessionPrincipal(t, ctx, base, user, "viewer-password", identity.Client{DeviceID: "expiring-combined"}, "emby")
	reader, trace := continuationIdentityReader(t, ctx, pool, "")
	if _, _, err := reader.GetOwnPreferenceConfiguration(ctx, owner, user.ID); err != nil {
		t.Fatal(err)
	}
	var expires time.Time
	if err := pool.QueryRow(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '5 seconds'
		WHERE id=$1 RETURNING expires_at`, owner.SessionID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	observed := false
	trace.afterProfile = func(readCtx context.Context) {
		observed = true
		// Wait on the database clock while authority rows remain locked, then
		// require the final statement to reject rather than return private data.
		if _, err := pool.Exec(readCtx, `SELECT pg_sleep(GREATEST(0,
			EXTRACT(EPOCH FROM $1::timestamptz-clock_timestamp()))+0.01)`, expires); err != nil {
			t.Fatal(err)
		}
	}
	raw, pin, err := reader.GetOwnPreferenceConfiguration(ctx, owner, user.ID)
	if !observed || !errors.Is(err, identity.ErrUnauthorized) || raw != nil || pin != "" {
		t.Fatalf("private read did not reject expiry after reading its payload: observed=%v error=%v", observed, err)
	}
}
