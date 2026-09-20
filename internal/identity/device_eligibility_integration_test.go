package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func assertDeviceEligibilityProjections(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *identity.Store,
	native, compatibility identity.Principal, initial identity.ManagedDevice, want int64) {
	t.Helper()
	before := deviceManagementSnapshot(t, ctx, pool)
	check := func(device identity.ManagedDevice, err error) {
		t.Helper()
		if err != nil || device.ID != initial.ID || device.Revision != initial.Revision || device.ActiveLoginCount != want {
			t.Fatalf("device eligibility projection differs: want %d, error %v", want, err)
		}
	}
	page, err := store.ListManagedDevices(ctx, native, identity.ManagedDeviceFilter{SearchTerm: initial.ReportedDeviceID})
	if err != nil || len(page.Items) != 1 || page.TotalRecordCount != 1 {
		t.Fatalf("native device eligibility page failed: %v", err)
	}
	check(page.Items[0], nil)
	listed, err := store.ListEmbyDevices(ctx, compatibility)
	if err != nil || len(listed) != 1 {
		t.Fatalf("compatibility device eligibility list failed: %v", err)
	}
	check(listed[0], nil)
	check(store.LookupEmbyDevice(ctx, compatibility, strconv.FormatInt(initial.ID, 10)))
	check(store.LookupEmbyDevice(ctx, compatibility, initial.ReportedDeviceID))
	// A no-op options update shares the detail projection and must not rewrite
	// credentials, policies, names, timestamps, or the native device revision.
	check(store.UpdateManagedDeviceOptions(ctx, native, initial.ID, initial.Revision, ""))
	if after := deviceManagementSnapshot(t, ctx, pool); after != before {
		t.Fatal("eligibility reads or no-op options changed stored state")
	}
}

func TestDeviceEligibilityCurrentPolicyCountsCredentialsAcrossUsersAndReadPaths(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, native := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	_, compatibility := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	first, err := store.CreateUser(ctx, "Eligibility First", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateUser(ctx, "Eligibility Second", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{DeviceID: "eligibility-shared", Device: "Shared Player"}
	firstLogin, _ := deviceLogin(t, ctx, store, first, "viewer-password", client, "emby", "192.0.2.5")
	secondLogin, _ := deviceLogin(t, ctx, store, first, "viewer-password", client, "emby", "127.0.0.1")
	otherLogin, _ := deviceLogin(t, ctx, store, second, "viewer-password", client, "emby", "")
	deviceLogin(t, ctx, store, admin, "administrator-password", client, "admin", "")
	initial := onlyDevice(t, ctx, store, native)
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 3)
	closedStart := float64((time.Now().Local().Hour() + 12) % 24)
	closedSchedule, err := json.Marshal(map[string]any{"AccessSchedules": []identity.AccessSchedule{
		{DayOfWeek: "Everyday", StartHour: closedStart, EndHour: closedStart + 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		policy  string
		want    int64
		allowed bool
	}{
		{"device_denied", `{"EnableAllDevices":false,"EnabledDevices":[]}`, 1, false},
		{"device_allowed", `{"EnableAllDevices":false,"EnabledDevices":["eligibility-shared"]}`, 3, true},
		{"closed_schedule", string(closedSchedule), 1, false},
		{"open_schedule", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":0,"EndHour":24}]}`, 3, true},
		{"locked_out", `{"LockedOutDate":123}`, 1, false},
		{"malformed_lockout", `{"LockedOutDate":"123"}`, 1, false},
		{"malformed_login_policy", `{"EnableAllDevices":"true"}`, 1, false},
		{"oversized_policy", `{"Unknown":"` + strings.Repeat("x", identity.MaxManagedPolicyBytes) + `"}`, 1, false},
		{"policy_restored", `{}`, 3, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", first.ID, test.policy); err != nil {
				t.Fatal(err)
			}
			assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, test.want)
			_, err := store.Resolve(ctx, firstLogin.Token, "emby")
			if (err == nil) != test.allowed {
				t.Fatalf("current credential resolution disagrees with stored-policy eligibility: %v", err)
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET device_id='credential-specific-device' WHERE id=$1", firstLogin.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":["credential-specific-device"]}' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 2)
	if _, err := store.Resolve(ctx, firstLogin.Token, "emby"); err != nil {
		t.Fatalf("actual session device was not accepted: %v", err)
	}
	if _, err := store.Resolve(ctx, secondLogin.Token, "emby"); err == nil {
		t.Fatal("registry device identity replaced the actual session device")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy='{"EnableRemoteAccess":false}' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE devices SET ip_address='192.0.2.200' WHERE id=$1", initial.ID); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 3)
	if _, err := store.ResolveWithPeer(ctx, firstLogin.Token, "emby", "192.0.2.5"); err == nil {
		t.Fatal("remote policy was bypassed by the eligibility projection")
	}
	if _, err := store.ResolveWithPeer(ctx, firstLogin.Token, "emby", "127.0.0.1"); err != nil {
		t.Fatalf("stored-policy eligibility guessed network policy from registry history: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 1)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=false, policy='{}' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, firstLogin.Token); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 2)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '2 days',
		expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, secondLogin.SessionID); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 1)
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", second.ID); err != nil {
		t.Fatal(err)
	}
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, compatibility, initial, 0)
	if _, err := store.Resolve(ctx, otherLogin.Token, "emby"); err == nil {
		t.Fatal("deleted account retained an eligible credential")
	}
}

func TestDeviceEligibilityLimitsArePageScopedAndDoNotBlockGenerationDeletion(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, native := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	_, compatibility := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	viewer, err := store.CreateUser(ctx, "Eligibility Budget", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{DeviceID: "eligibility-over-budget"}
	login, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "")
	overloaded := onlyDevice(t, ctx, store, native)
	deviceLogin(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "eligibility-healthy"}, "emby", "")
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_hash,kind,device_id,device_registry_id,expires_at)
		SELECT 'eligibility-budget-'||n,$1,decode(md5('eligibility-budget-'||n)||md5('eligibility-tail-'||n),'hex'),
		'emby',$2,$3,clock_timestamp()+interval '1 day' FROM generate_series(1,100000) n`,
		viewer.ID, client.DeviceID, overloaded.ID); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListManagedDevices(ctx, native, identity.ManagedDeviceFilter{SearchTerm: "eligibility-over-budget"})
	if !errors.Is(err, identity.ErrDeviceEligibilityLimit) || page.Items != nil || page.TotalRecordCount != 0 {
		t.Fatalf("credential budget returned a partial device page: %v", err)
	}
	if _, err := store.LookupEmbyDevice(ctx, compatibility, client.DeviceID); !errors.Is(err, identity.ErrDeviceEligibilityLimit) {
		t.Fatalf("detail bypassed the credential processing budget: %v", err)
	}
	if _, err := store.ListEmbyDevices(ctx, compatibility); !errors.Is(err, identity.ErrDeviceEligibilityLimit) {
		t.Fatalf("compatibility list bypassed the credential processing budget: %v", err)
	}
	page, err = store.ListManagedDevices(ctx, native, identity.ManagedDeviceFilter{SearchTerm: "eligibility-healthy"})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ActiveLoginCount != 1 {
		t.Fatalf("another device's credentials exhausted a page-scoped budget: %v", err)
	}
	deleted, err := store.DeleteManagedDevice(ctx, native, overloaded.ID, overloaded.Revision)
	if err != nil || deleted.RevokedLoginCount != 100001 {
		t.Fatalf("eligibility projection blocked generation cleanup: %v", err)
	}
	if _, err := store.Resolve(ctx, login.Token, "emby"); err == nil {
		t.Fatal("deleted generation retained its original credential")
	}
	deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "")
	replacement, err := store.LookupEmbyDevice(ctx, compatibility, client.DeviceID)
	if err != nil || replacement.ID == overloaded.ID || replacement.ActiveLoginCount != 1 {
		t.Fatalf("new generation included historical credentials: %v", err)
	}
}

func TestDeviceEligibilityDoesNotCountSharedApplicationKeys(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	_, compatibility := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, compatibility, "Eligibility Shared Key")
	principal := applicationKeyPrincipal(t, ctx, store, key)
	deviceLogin(t, ctx, store, native.User, "administrator-password", key.Client, "emby", "")
	deviceLogin(t, ctx, store, native.User, "administrator-password", key.Client, "admin", "")
	initial := onlyDevice(t, ctx, store, native)
	assertDeviceEligibilityProjections(t, ctx, pool, store, native, principal, initial, 1)
}

func TestDeviceEligibilityCompatibilityDeviceBudgetNeverTruncates(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, native := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	_, compatibility := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	if _, err := pool.Exec(ctx, `INSERT INTO devices(reported_device_id)
		SELECT 'eligibility-device-'||n FROM generate_series(1,10001) n`); err != nil {
		t.Fatal(err)
	}
	if items, err := store.ListEmbyDevices(ctx, compatibility); !errors.Is(err, identity.ErrDeviceEligibilityLimit) || items != nil {
		t.Fatalf("compatibility device budget returned an incomplete list: %v", err)
	}
	page, err := store.ListManagedDevices(ctx, native, identity.ManagedDeviceFilter{Limit: 1, StartIndex: 10000})
	if err != nil || page.TotalRecordCount != 10001 || len(page.Items) != 1 || page.Items[0].ActiveLoginCount != 0 {
		t.Fatalf("compatibility budget incorrectly limited native paging: %v", err)
	}
	page, err = store.ListManagedDevices(ctx, native, identity.ManagedDeviceFilter{Limit: 1, StartIndex: 10001})
	if err != nil || page.TotalRecordCount != 10001 || len(page.Items) != 0 {
		t.Fatalf("empty native page lost its exact total: %v", err)
	}
}
