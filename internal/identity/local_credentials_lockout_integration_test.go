package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

func assertLocalPasswordBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string, wantFailures int, wantUntil *time.Time) {
	t.Helper()
	var failures int
	var sameUntil bool
	if err := pool.QueryRow(ctx, `SELECT local_password_failures,
		local_password_blocked_until IS NOT DISTINCT FROM $2::timestamptz FROM users WHERE id=$1`,
		userID, wantUntil).Scan(&failures, &sameUntil); err != nil {
		t.Fatal(err)
	}
	if failures != wantFailures || !sameUntil {
		t.Fatalf("local password block changed: failures=%d, want %d, unchanged deadline=%v", failures, wantFailures, sameUntil)
	}
}

func TestStoreLocalPasswordBlockSurvivesNativePinAndPreferenceEdits(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Blocked PIN owner", "normal-password", false)
	if err != nil {
		t.Fatal(err)
	}
	local := "blocked-local-password"
	configured, err := store.UpdateLocalCredentials(ctx, admin, viewer.ID, identity.LocalCredentialsUpdate{
		Revision: 1, EnableLocalPassword: true, LocalPassword: &local,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, owner := managedLogin(t, ctx, store, viewer, "normal-password", "emby")
	client := identity.Client{DeviceID: "blocked-pin-device"}
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, "incorrect-local-password", client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("failed local attempt returned %v", err)
		}
	}
	var blockedUntil time.Time
	var live bool
	if err := pool.QueryRow(ctx, `SELECT local_password_blocked_until,
		local_password_blocked_until>clock_timestamp() FROM users WHERE id=$1`, viewer.ID).Scan(&blockedUntil, &live); err != nil {
		t.Fatal(err)
	}
	if !live {
		t.Fatal("five failed local attempts did not create a live block")
	}
	assertLocalPasswordBlock(t, ctx, pool, viewer.ID, 5, &blockedUntil)
	status := configured.Credentials
	for _, test := range []struct {
		name string
		pin  string
	}{
		{name: "set", pin: "4826"},
		{name: "same PIN", pin: "4826"},
		{name: "clear", pin: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetCount := identityActivityCount(t, ctx, pool, activity.ActionUserPasswordReset, viewer.ID)
			updateCount := identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID)
			preferences, err := store.GetUserPreferences(ctx, owner, viewer.ID)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := store.UpdateLocalCredentials(ctx, admin, viewer.ID, identity.LocalCredentialsUpdate{
				Revision: status.Revision, EnableLocalPassword: true, ProfilePin: &test.pin,
			})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Credentials.Revision != status.Revision+1 || updated.Credentials.HasProfilePin != (test.pin != "") ||
				!updated.Credentials.HasLocalPassword || !updated.Credentials.EnableLocalPassword ||
				updated.CurrentSessionRevoked || len(updated.RevokedSessionIDs) != 0 {
				t.Fatal("native PIN update changed local credential or session semantics")
			}
			status = updated.Credentials
			currentPreferences, err := store.GetUserPreferences(ctx, owner, viewer.ID)
			if err != nil || currentPreferences.Revision != preferences.Revision+1 {
				t.Fatalf("native PIN input did not advance the configuration revision: %v", err)
			}
			if pin, err := store.GetOwnProfilePin(ctx, owner, viewer.ID); err != nil || pin != test.pin {
				t.Fatalf("native PIN update did not persist its profile gate: %v", err)
			}
			if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
				t.Fatal("native PIN update revoked an existing owner session")
			}
			if identityActivityCount(t, ctx, pool, activity.ActionUserPasswordReset, viewer.ID) != resetCount ||
				identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID) != updateCount+1 {
				t.Fatal("native PIN input changed its user-update audit semantics")
			}
			assertLocalPasswordBlock(t, ctx, pool, viewer.ID, 5, &blockedUntil)
			if _, err := store.AuthenticateWithPeer(ctx, viewer.Name, local, client, "emby", "192.168.10.20"); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Fatal("native PIN input allowed a blocked local password to sign in")
			}
			assertLocalPasswordBlock(t, ctx, pool, viewer.ID, 5, &blockedUntil)
		})
	}
	updateCount := identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID)
	unchanged, err := store.UpdateLocalCredentials(ctx, admin, viewer.ID, identity.LocalCredentialsUpdate{
		Revision: status.Revision, EnableLocalPassword: true,
	})
	if err != nil || unchanged.Credentials.Revision != status.Revision ||
		identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID) != updateCount {
		t.Fatalf("unchanged native credentials did not retain no-op behavior: %v", err)
	}
	if _, err := store.UpdateUserPreferences(ctx, owner, viewer.ID, nil, identity.UserConfigurationPatch{
		"SubtitleMode": json.RawMessage(`"Always"`),
	}); err != nil {
		t.Fatal(err)
	}
	assertLocalPasswordBlock(t, ctx, pool, viewer.ID, 5, &blockedUntil)
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatal("unrelated preference edit revoked the owner session")
	}
}

func TestStoreExplicitLocalCredentialChangesResetBlockRevokeAndAudit(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	administrator := bootstrapTestAdmin(t, ctx, store)
	_, admin := managedLogin(t, ctx, store, administrator, "administrator-password", "admin")
	for _, test := range []struct {
		name           string
		initialEnabled bool
		enabled        bool
		password       string
		replace        bool
		wantPassword   bool
	}{
		{name: "replace password", initialEnabled: true, enabled: true, password: "replacement-local-password", replace: true, wantPassword: true},
		{name: "same password input", initialEnabled: true, enabled: true, password: "original-local-password", replace: true, wantPassword: true},
		{name: "clear password", initialEnabled: true, replace: true},
		{name: "disable", initialEnabled: true, wantPassword: true},
		{name: "enable", enabled: true, wantPassword: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			viewer, err := store.CreateUser(ctx, "Reset block "+test.name, "normal-password", false)
			if err != nil {
				t.Fatal(err)
			}
			local := "original-local-password"
			configured, err := store.UpdateLocalCredentials(ctx, admin, viewer.ID, identity.LocalCredentialsUpdate{
				Revision: 1, EnableLocalPassword: test.initialEnabled, LocalPassword: &local,
			})
			if err != nil {
				t.Fatal(err)
			}
			credentials, _ := managedLogin(t, ctx, store, viewer, "normal-password", "emby")
			if _, err := pool.Exec(ctx, `UPDATE users SET local_password_failures=5,
				local_password_blocked_until=clock_timestamp()+interval '5 minutes' WHERE id=$1`, viewer.ID); err != nil {
				t.Fatal(err)
			}
			resetCount := identityActivityCount(t, ctx, pool, activity.ActionUserPasswordReset, viewer.ID)
			updateCount := identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID)
			input := identity.LocalCredentialsUpdate{Revision: configured.Credentials.Revision, EnableLocalPassword: test.enabled}
			if test.replace {
				input.LocalPassword = &test.password
			}
			updated, err := store.UpdateLocalCredentials(ctx, admin, viewer.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Credentials.Revision != configured.Credentials.Revision+1 ||
				updated.Credentials.HasLocalPassword != test.wantPassword || updated.Credentials.EnableLocalPassword != test.enabled ||
				updated.CurrentSessionRevoked || !slices.Contains(updated.RevokedSessionIDs, credentials.SessionID) {
				t.Fatal("explicit local credential change lost its credential or revocation semantics")
			}
			assertLocalPasswordBlock(t, ctx, pool, viewer.ID, 0, nil)
			if _, err := store.Resolve(ctx, credentials.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("explicit local credential change retained the previous login")
			}
			if identityActivityCount(t, ctx, pool, activity.ActionUserPasswordReset, viewer.ID) != resetCount+1 ||
				identityActivityCount(t, ctx, pool, activity.ActionUserUpdated, viewer.ID) != updateCount {
				t.Fatal("explicit local credential change did not preserve password-reset auditing")
			}
			var action string
			var revision int64
			if err := pool.QueryRow(ctx, `SELECT action,revision FROM activity_entries
				WHERE resource_id=$1 ORDER BY id DESC LIMIT 1`, viewer.ID).Scan(&action, &revision); err != nil {
				t.Fatal(err)
			}
			if action != string(activity.ActionUserPasswordReset) || revision != updated.Credentials.Revision {
				t.Fatal("password-reset audit lost the resulting credential revision")
			}
		})
	}
}
