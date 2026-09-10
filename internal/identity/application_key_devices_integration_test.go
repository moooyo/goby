package identity_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestApplicationKeyDeviceSharedGenerationDeletionAndOrdinaryIsolation(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	nativeAdmin := admin
	_, admin = managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	first := issueApplicationKey(t, ctx, store, admin, "Shared Alpha")
	second := issueApplicationKey(t, ctx, store, admin, "Shared Sibling")
	if first.ReportedDeviceNumericID != 1 || second.ReportedDeviceNumericID != first.ReportedDeviceNumericID {
		t.Fatal("application keys did not retain the first shared generation")
	}
	if _, err := store.LookupApplicationKeyDevice(ctx, nativeAdmin, "1"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("native administrator crossed the compatibility device boundary: %v", err)
	}
	firstDefault := applicationKeyPrincipal(t, ctx, store, first)
	secondDefault := applicationKeyPrincipal(t, ctx, store, second)
	alpha := bindApplicationClient(t, ctx, store, first, identity.Client{Name: "Shared Alpha Client", DeviceID: "alpha-device", Device: "Alpha Player", Version: "2.0"})
	beta := bindApplicationClient(t, ctx, store, first, identity.Client{Name: "Shared Beta Client", DeviceID: "beta-device", Device: "Beta Player", Version: "3.0"})
	sibling := bindApplicationClient(t, ctx, store, second, identity.Client{Name: "Shared Sibling Client", DeviceID: "sibling-device", Device: "Sibling Player", Version: "4.0"})
	ordinary, err := store.Authenticate(ctx, admin.User.Name, "administrator-password", identity.Client{
		Name: "Ordinary Control", DeviceID: first.Client.DeviceID, Device: "Ordinary Player", Version: "1.0"}, "emby")
	if err != nil {
		t.Fatalf("create ordinary control with colliding reported ID: %v", err)
	}
	device, err := store.LookupApplicationKeyDevice(ctx, alpha, first.Client.DeviceID)
	if err != nil || device.ID != 1 || device.LastUserID != nil || device.LastUserName != nil || device.ActiveLoginCount != 2 {
		t.Fatalf("shared device projection borrowed ordinary identity: %v", err)
	}
	if device.ReportedName != "Sibling Player" || device.AppName != second.AppName || device.AppVersion != "4.0" {
		t.Fatal("shared device did not retain the last application client metadata")
	}
	listed, err := store.ListEmbyDevices(ctx, alpha)
	if err != nil || len(listed) != 1 || listed[0].ID == device.ID || listed[0].ReportedDeviceID != device.ReportedDeviceID {
		t.Fatalf("hidden shared generation entered the ordinary device list: %v", err)
	}
	var keyHistory string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM application_keys k`).Scan(&keyHistory); err != nil {
		t.Fatal(err)
	}
	deletion, err := store.DeleteApplicationKeyDevice(ctx, beta, strconv.FormatInt(device.ID, 10))
	if err != nil || deletion.ID != device.ID || deletion.RevokedLoginCount != 2 || deletion.DeletedAt.IsZero() {
		t.Fatalf("self-key shared generation deletion failed: %v", err)
	}
	wantParents := []string{first.CredentialID, second.CredentialID}
	slices.Sort(wantParents)
	if !reflect.DeepEqual(deletion.RevokedSessionIDs, wantParents) {
		t.Fatal("device deletion returned client IDs instead of both parent credential IDs")
	}
	for _, principal := range []identity.Principal{firstDefault, secondDefault, alpha, beta, sibling} {
		if _, err := store.RevalidateSession(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("deleted device left an application context authenticated: %v", err)
		}
	}
	if _, err := store.Resolve(ctx, ordinary.Token, "emby"); err != nil {
		t.Fatalf("shared device deletion revoked an ordinary reported-ID collision: %v", err)
	}
	var afterHistory string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM application_keys k`).Scan(&afterHistory); err != nil || afterHistory != keyHistory {
		t.Fatalf("device deletion changed application key history or ciphertext: %v", err)
	}
	var retainedContexts, registeredKeys int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM application_key_clients),
		(SELECT count(*) FROM sessions WHERE kind = 'application_key' AND device_registry_id IS NOT NULL)`).Scan(&retainedContexts, &registeredKeys); err != nil || retainedContexts != 5 || registeredKeys != 0 {
		t.Fatalf("application contexts or ordinary registry scope changed: %v", err)
	}
	aliasRetry, err := store.DeleteApplicationKeyDevice(ctx, admin, first.Client.DeviceID)
	if err != nil || aliasRetry.ID != device.ID || aliasRetry.RevokedLoginCount != 0 || !aliasRetry.DeletedAt.Equal(deletion.DeletedAt) {
		t.Fatalf("removed shared alias lost its generation: %v", err)
	}
	if _, err := store.LookupApplicationKeyDevice(ctx, admin, first.Client.DeviceID); !errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
		t.Fatalf("known removed shared alias lost its family on lookup: %v", err)
	}
	if _, err := store.UpdateApplicationKeyDeviceOptions(ctx, admin, first.Client.DeviceID, "Do not rename the ordinary device"); !errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
		t.Fatalf("known removed shared alias lost its family on update: %v", err)
	}
	if _, err := store.Resolve(ctx, ordinary.Token, "emby"); err != nil {
		t.Fatalf("repeated shared alias removal changed the ordinary control: %v", err)
	}
	replacement := issueApplicationKey(t, ctx, store, admin, "Replacement Generation")
	if replacement.ReportedDeviceNumericID <= device.ID || replacement.ReportedDeviceNumericID == listed[0].ID {
		t.Fatal("replacement reused a retired or ordinary numeric device ID")
	}
	retry, err := store.DeleteApplicationKeyDevice(ctx, admin, strconv.FormatInt(device.ID, 10))
	if err != nil || retry.RevokedLoginCount != 0 || !retry.DeletedAt.Equal(deletion.DeletedAt) || !reflect.DeepEqual(retry.RevokedSessionIDs, wantParents) {
		t.Fatalf("old generation deletion was not idempotent: %v", err)
	}
	applicationKeyPrincipal(t, ctx, store, replacement)
	if _, err := store.LookupApplicationKeyDevice(ctx, admin, strconv.FormatInt(device.ID, 10)); !errors.Is(err, identity.ErrDeviceNotFound) {
		t.Fatalf("removed numeric generation resolved to a replacement: %v", err)
	}
	current, err := store.LookupApplicationKeyDevice(ctx, admin, first.Client.DeviceID)
	if err != nil || current.ID != replacement.ReportedDeviceNumericID {
		t.Fatalf("reported ID did not resolve the current shared generation: %v", err)
	}
}

func TestApplicationKeyDeviceOptionsAndIndividualKeyRevocationKeepGeneration(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	_, admin = managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, admin, "Shared Options")
	actor := applicationKeyPrincipal(t, ctx, store, key)
	device, err := store.UpdateApplicationKeyDeviceOptions(ctx, actor, "1", "  Shared Living Room  ")
	if err != nil || device.CustomName == nil || *device.CustomName != "Shared Living Room" || device.Name != *device.CustomName {
		t.Fatalf("application device custom name did not normalize: %v", err)
	}
	beforeRevision := device.Revision
	device, err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, "1", "Shared Living Room")
	if err != nil || device.Revision != beforeRevision {
		t.Fatalf("identical application device options advanced revision: %v", err)
	}
	if _, err := store.UpdateApplicationKeyDeviceOptions(ctx, actor, "1", strings.Repeat("x", 257)); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("application device accepted an oversized name: %v", err)
	}
	device, err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, "1", "  ")
	if err != nil || device.CustomName != nil || device.Name != key.Client.Device {
		t.Fatalf("application device name reset lost its reported name: %v", err)
	}
	viewer, err := store.CreateUser(ctx, "Shared Device Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	for _, call := range []func() error{
		func() error { _, err := store.LookupApplicationKeyDevice(ctx, viewerActor, "1"); return err },
		func() error {
			_, err := store.UpdateApplicationKeyDeviceOptions(ctx, viewerActor, "1", "Denied")
			return err
		},
		func() error { _, err := store.DeleteApplicationKeyDevice(ctx, viewerActor, "1"); return err },
	} {
		if err := call(); !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Fatalf("ordinary viewer managed an application server device: %v", err)
		}
	}
	if _, err := store.RevokeApplicationKey(ctx, admin, key.ID); err != nil {
		t.Fatal(err)
	}
	device, err = store.LookupApplicationKeyDevice(ctx, admin, "1")
	if err != nil || device.ActiveLoginCount != 0 {
		t.Fatalf("revoking the last key removed the shared device: %v", err)
	}
	if _, err := store.DeleteApplicationKeyDevice(ctx, actor, "1"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("stale revoked key retained device-management authority: %v", err)
	}
	var removed bool
	if err := pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM application_key_devices WHERE id = 1").Scan(&removed); err != nil || removed {
		t.Fatalf("failed stale operation changed the shared generation: %v", err)
	}
}

func TestApplicationKeyDeviceDeletionSerializesWithNewKeyRegistration(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	_, admin = managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, admin, "Deleted Before New Key")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	deletions := make(chan error, 1)
	deletionDone := make(chan struct{})
	go func() {
		defer close(deletionDone)
		_, err := store.DeleteApplicationKeyDevice(ctx, admin, "1")
		deletions <- err
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions", deletionDone)
	type issuance struct {
		key identity.ApplicationKey
		err error
	}
	issued := make(chan issuance, 1)
	go func() {
		value, err := store.CreateApplicationKey(ctx, admin, "New Generation", "192.0.2.14", key.Client)
		issued <- issuance{key: value, err: err}
	}()
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-deletions:
		if err != nil {
			t.Fatalf("blocked shared deletion failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("shared device deletion did not finish")
	}
	select {
	case result := <-issued:
		if result.err != nil || result.key.ReportedDeviceNumericID <= 1 {
			t.Fatalf("concurrent new key escaped into the deleted generation: %v", result.err)
		}
		applicationKeyPrincipal(t, ctx, store, result.key)
	case <-ctx.Done():
		t.Fatal("new application key registration did not finish")
	}
}

func TestApplicationKeyDeviceBlockedMutationRevalidatesActor(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	_, admin = managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	issueApplicationKey(t, ctx, store, admin, "Blocked Actor")
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", admin.User.ID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := store.DeleteApplicationKeyDevice(ctx, admin, "1")
		results <- err
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM users", done)
	if _, err := blocker.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Fatalf("blocked deletion trusted stale administrator state: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("blocked actor operation did not finish")
	}
	var intact bool
	if err := pool.QueryRow(ctx, `SELECT d.deleted_at IS NULL AND bool_and(a.revoked_at IS NULL)
		FROM application_key_devices d JOIN application_keys k ON k.reported_device_numeric_id = d.id
		JOIN sessions a ON a.id = k.credential_id WHERE d.id = 1 GROUP BY d.deleted_at`).Scan(&intact); err != nil || !intact {
		t.Fatalf("lost actor authority partially removed the generation: %v", err)
	}
}

func TestApplicationKeyDeviceActorExpiryAfterRevocationRollsBack(t *testing.T) {
	testCtx, pool, store, admin, _ := applicationKeyTestStore(t)
	_, admin = managedLogin(t, testCtx, store, admin.User, "administrator-password", "emby")
	key := issueApplicationKey(t, testCtx, store, admin, "Expiry Rollback")
	ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
	defer cancel()
	blocker, blockerPID := pauseManagedSessionUpdate(t, ctx, pool, key.CredentialID)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, admin.SessionID); err != nil {
		t.Fatal(err)
	}
	before := applicationKeySnapshot(t, ctx, pool)
	var deviceBefore string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(d)::text FROM application_key_devices d WHERE id = 1").Scan(&deviceBefore); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := store.DeleteApplicationKeyDevice(ctx, admin, "1")
		results <- err
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "UPDATE sessions SET revoked_at", done)
	waitManagedSessionDatabaseExpiry(t, ctx, pool, admin.SessionID, done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("expired actor committed shared device revocation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("expired device actor operation did not finish")
	}
	var deviceAfter string
	if err := pool.QueryRow(testCtx, "SELECT to_jsonb(d)::text FROM application_key_devices d WHERE id = 1").Scan(&deviceAfter); err != nil {
		t.Fatal(err)
	}
	if deviceAfter != deviceBefore || applicationKeySnapshot(t, testCtx, pool) != before {
		t.Fatal("actor expiry did not roll back the device generation and parent credentials")
	}
	assertManagedSessionUpdateCount(t, testCtx, pool, 0)
}
