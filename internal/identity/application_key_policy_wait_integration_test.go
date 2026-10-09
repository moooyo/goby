package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

type applicationKeyPolicyOutcome struct {
	returned bool
	err      error
}

type applicationKeyPolicyOperation struct {
	name   string
	action activity.Action
	run    func(context.Context, *identity.Store, identity.Principal, identity.ApplicationKey) applicationKeyPolicyOutcome
}

func applicationKeyPolicyOperations() []applicationKeyPolicyOperation {
	return []applicationKeyPolicyOperation{
		{"create", activity.ActionApplicationKeyCreated, func(ctx context.Context, store *identity.Store, actor identity.Principal, _ identity.ApplicationKey) applicationKeyPolicyOutcome {
			key, err := store.CreateApplicationKey(ctx, actor, "Policy wait creation", "192.0.2.88", identity.Client{DeviceID: "persistent-server-id", Device: "Uncommitted device"})
			return applicationKeyPolicyOutcome{key != (identity.ApplicationKey{}), err}
		}},
		{"list-reveal", activity.ActionApplicationKeyRevealed, func(ctx context.Context, store *identity.Store, actor identity.Principal, _ identity.ApplicationKey) applicationKeyPolicyOutcome {
			page, err := store.ListApplicationKeys(ctx, actor, identity.ApplicationKeyFilter{RevealTokens: true})
			return applicationKeyPolicyOutcome{page.Items != nil || page.TotalRecordCount != 0, err}
		}},
		{"get-reveal", activity.ActionApplicationKeyRevealed, func(ctx context.Context, store *identity.Store, actor identity.Principal, key identity.ApplicationKey) applicationKeyPolicyOutcome {
			value, err := store.GetApplicationKey(ctx, actor, key.ID, true)
			return applicationKeyPolicyOutcome{value != (identity.ApplicationKey{}), err}
		}},
		{"revoke", activity.ActionApplicationKeyRevoked, func(ctx context.Context, store *identity.Store, actor identity.Principal, key identity.ApplicationKey) applicationKeyPolicyOutcome {
			value, err := store.RevokeApplicationKey(ctx, actor, key.ID)
			return applicationKeyPolicyOutcome{value != (identity.ApplicationKeyRevocation{}), err}
		}},
	}
}

func runApplicationKeyPolicyOperation(ctx context.Context, store *identity.Store, actor identity.Principal, key identity.ApplicationKey, operation applicationKeyPolicyOperation) (<-chan applicationKeyPolicyOutcome, <-chan struct{}) {
	results, done := make(chan applicationKeyPolicyOutcome, 1), make(chan struct{})
	go func() {
		defer close(done)
		results <- operation.run(ctx, store, actor, key)
	}()
	return results, done
}

func awaitDeniedApplicationKeyPolicyOperation(t *testing.T, ctx context.Context, results <-chan applicationKeyPolicyOutcome) {
	t.Helper()
	select {
	case result := <-results:
		if result.err != identity.ErrUnauthorized || result.returned {
			t.Fatalf("restricted application key manager returned data or the wrong error: %v", result.err)
		}
	case <-ctx.Done():
		t.Fatal("application key policy operation did not finish")
	}
}

// Policy is changed deliberately by the blocker. All key-management effects
// and audit rows must otherwise remain byte-for-byte identical on rejection.
func applicationKeyPolicyEffects(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'credentials', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM sessions a),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c),
		'devices', (SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM application_key_devices d),
		'audit', (SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM activity_entries e))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot application key policy effects: %v", err)
	}
	return snapshot
}

func TestStoreApplicationKeyManagementRechecksEmbyPolicyAfterWait(t *testing.T) {
	operations := applicationKeyPolicyOperations()
	metadata := applicationKeyPolicyOperation{name: "metadata-account-wait", run: func(ctx context.Context, store *identity.Store, actor identity.Principal, _ identity.ApplicationKey) applicationKeyPolicyOutcome {
		page, err := store.ListApplicationKeys(ctx, actor, identity.ApplicationKeyFilter{})
		return applicationKeyPolicyOutcome{page.Items != nil || page.TotalRecordCount != 0, err}
	}}
	for _, test := range []struct {
		operation   applicationKeyPolicyOperation
		policy      string
		accountWait bool
	}{
		{operations[0], `{"EnableAllDevices":false,"EnabledDevices":[]}`, false},
		{operations[1], `{"EnableRemoteAccess":false}`, false},
		{operations[2], `{"LockedOutDate":1}`, false},
		{operations[3], `{"AccessSchedules":null}`, false},
		{metadata, `{"EnableAllDevices":false,"EnabledDevices":[]}`, true},
	} {
		t.Run(test.operation.name, func(t *testing.T) {
			testCtx, pool, store, native, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, testCtx, store, native, "Policy wait target")
			_, actor := managedLogin(t, testCtx, store, native.User, "administrator-password", "emby")
			actor.PeerIP = "192.0.2.88"
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			fragment := "pg_advisory_xact_lock"
			if test.accountWait {
				if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", actor.User.ID); err != nil {
					t.Fatal(err)
				}
				fragment = "SELECT id FROM users"
			} else if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
				t.Fatal(err)
			}
			before := applicationKeyPolicyEffects(t, ctx, pool)
			results, done := runApplicationKeyPolicyOperation(ctx, store, actor, key, test.operation)
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, fragment, done)
			if _, err := blocker.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", actor.User.ID, test.policy); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			awaitDeniedApplicationKeyPolicyOperation(t, ctx, results)
			if applicationKeyPolicyEffects(t, testCtx, pool) != before {
				t.Fatal("policy rejection committed key-management effects or audit records")
			}
		})
	}
}

func installApplicationKeyClosingSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) time.Time {
	t.Helper()
	var observedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		t.Fatal(err)
	}
	local := observedAt.Local()
	boundary := local.Add(3 * time.Second)
	endHour := float64(boundary.Hour()) + float64(boundary.Minute())/60 + float64(boundary.Second())/3600 + float64(boundary.Nanosecond())/3.6e12
	if endHour == 0 {
		boundary = boundary.Add(time.Millisecond)
		endHour = float64(time.Millisecond) / float64(time.Hour)
	}
	schedules := []identity.AccessSchedule{{DayOfWeek: boundary.Weekday().String(), StartHour: 0, EndHour: endHour}}
	if local.YearDay() != boundary.YearDay() || local.Year() != boundary.Year() {
		// Keep the initial authorization open when this bounded wait crosses midnight.
		schedules = append(schedules, identity.AccessSchedule{DayOfWeek: local.Weekday().String(), StartHour: 0, EndHour: 24})
	}
	policy, err := json.Marshal(map[string]any{"AccessSchedules": schedules})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", userID, policy); err != nil {
		t.Fatal(err)
	}
	return boundary
}

func waitApplicationKeyScheduleBoundary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, boundary time.Time, done <-chan struct{}) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var closed bool
		if err := pool.QueryRow(ctx, "SELECT clock_timestamp() >= $1::timestamptz", boundary.Add(time.Millisecond)).Scan(&closed); err != nil {
			t.Fatal(err)
		}
		if closed {
			return
		}
		select {
		case <-done:
			t.Fatal("key operation finished before the schedule boundary while its audit was blocked")
		case <-ctx.Done():
			t.Fatal("timed out waiting for the application key schedule boundary")
		case <-ticker.C:
		}
	}
}

func TestStoreApplicationKeyManagementScheduleClosesDuringAudit(t *testing.T) {
	for _, operation := range applicationKeyPolicyOperations() {
		t.Run(operation.name, func(t *testing.T) {
			testCtx, pool, store, native, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, testCtx, store, native, "Schedule wait target")
			_, actor := managedLogin(t, testCtx, store, native.User, "administrator-password", "emby")
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseApplicationKeyActivityInsert(t, ctx, pool, operation.action)
			boundary := installApplicationKeyClosingSchedule(t, ctx, pool, actor.User.ID)
			before := applicationKeyPolicyEffects(t, ctx, pool)
			results, done := runApplicationKeyPolicyOperation(ctx, store, actor, key, operation)
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitApplicationKeyScheduleBoundary(t, ctx, pool, boundary, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			awaitDeniedApplicationKeyPolicyOperation(t, ctx, results)
			if applicationKeyPolicyEffects(t, testCtx, pool) != before {
				t.Fatal("closed access schedule committed key-management effects or audit records")
			}
			var insertCount int64
			if err := pool.QueryRow(testCtx, "SELECT insert_count FROM application_key_activity_gate").Scan(&insertCount); err != nil || insertCount != 0 {
				t.Fatalf("schedule rejection retained an uncommitted audit side effect: %v", err)
			}
		})
	}
}

func TestStoreApplicationKeyManagementPolicyKeepsCredentialBoundaries(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, native, "Independent policy authority")
	keyActor := applicationKeyPrincipal(t, ctx, store, key)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	emby.PeerIP = "192.0.2.88"
	if value, err := store.GetApplicationKey(ctx, emby, key.ID, true); err != nil || value.Token != key.Token {
		t.Fatalf("eligible Emby administrator lost key management: %v", err)
	}
	viewer, err := store.CreateUser(ctx, "Application Policy Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	for _, operation := range applicationKeyPolicyOperations() {
		result := operation.run(ctx, store, viewerActor, key)
		if result.err != identity.ErrUnauthorized || result.returned {
			t.Fatalf("ordinary viewer operation %s changed denial behavior: %v", operation.name, result.err)
		}
	}
	for _, policy := range []string{
		`{"EnableAllDevices":false,"EnableRemoteAccess":false,"LockedOutDate":1}`,
		`{"AccessSchedules":null}`,
	} {
		if _, err := pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", native.User.ID, policy); err != nil {
			t.Fatal(err)
		}
		if value, err := store.GetApplicationKey(ctx, emby, key.ID, true); err != identity.ErrUnauthorized || value != (identity.ApplicationKey{}) {
			t.Fatalf("restricted Emby administrator revealed a key or changed error mapping: %v", err)
		}
		for _, actor := range []identity.Principal{native, keyActor} {
			if value, err := store.GetApplicationKey(ctx, actor, key.ID, true); err != nil || value.Token != key.Token {
				t.Fatalf("Emby policy restricted independent native/application authority: %v", err)
			}
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator=false,is_disabled=true WHERE id=$1", native.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetApplicationKey(ctx, native, key.ID, true); err != identity.ErrUnauthorized {
		t.Fatalf("disabled native administrator retained key management: %v", err)
	}
	if value, err := store.GetApplicationKey(ctx, keyActor, key.ID, true); err != nil || value.Token != key.Token {
		t.Fatalf("creator disablement restricted application authority: %v", err)
	}
	value, err := store.RevokeApplicationKey(ctx, keyActor, key.ID)
	if err != nil || !value.CurrentCredentialRevoked || value.RevokedAt.IsZero() {
		t.Fatalf("application self-revocation lost its exact exception: %v", err)
	}
	if _, err := store.GetApplicationKey(ctx, keyActor, key.ID, true); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("self-revoked key retained reveal authority: %v", err)
	}
}
