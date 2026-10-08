package identity_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestApplicationKeyDeviceLookupSkipsManagementAndSiblingLocks(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, native, "Lookup actor")
	sibling := issueApplicationKey(t, ctx, store, native, "Lookup sibling")
	application := applicationKeyPrincipal(t, ctx, store, key)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	blocker, _ := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(4919415424202458192)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"SELECT id FROM sessions WHERE id = $1 FOR UPDATE",
		"SELECT credential_id FROM application_keys WHERE credential_id = $1 FOR UPDATE",
	} {
		if _, err := blocker.Exec(ctx, statement, sibling.CredentialID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM application_key_devices WHERE id = $1 FOR SHARE", key.ReportedDeviceNumericID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []identity.Principal{emby, application} {
		t.Run(actor.Kind, func(t *testing.T) {
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			device, err := store.LookupApplicationKeyDevice(readCtx, actor, key.Client.DeviceID)
			if err != nil || device.ID != key.ReportedDeviceNumericID {
				t.Fatalf("device lookup waited for unrelated management or credentials: %v", err)
			}
			if _, err := store.LookupApplicationKeyDevice(readCtx, actor, "ordinary-only-device"); !errors.Is(err, identity.ErrDeviceNotFound) || errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
				t.Fatalf("unknown shared device did not preserve ordinary fallback: %v", err)
			}
		})
	}
}

func TestApplicationKeyDeviceOptionsKeepActorSharedAndSkipSiblingLocks(t *testing.T) {
	for _, kind := range []string{"emby", identity.ApplicationKeyKind} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store, native, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, ctx, store, native, "Options actor")
			sibling := issueApplicationKey(t, ctx, store, native, "Options sibling")
			actor := applicationKeyPrincipal(t, ctx, store, key)
			if kind == "emby" {
				_, actor = managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
			}
			blocker, _ := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if actor.IsApplicationKey() {
				for _, statement := range []string{
					"SELECT credential_id FROM application_keys WHERE credential_id = $1 FOR SHARE",
					"SELECT id FROM application_key_clients WHERE credential_id = $1 FOR SHARE",
				} {
					if _, err := blocker.Exec(ctx, statement, actor.SessionID); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, statement := range []string{
				"SELECT id FROM sessions WHERE id = $1 FOR UPDATE",
				"SELECT credential_id FROM application_keys WHERE credential_id = $1 FOR UPDATE",
			} {
				if _, err := blocker.Exec(ctx, statement, sibling.CredentialID); err != nil {
					t.Fatal(err)
				}
			}
			updateCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			device, err := store.UpdateApplicationKeyDeviceOptions(updateCtx, actor, key.Client.DeviceID, "Shared options")
			if err != nil || device.CustomName == nil || *device.CustomName != "Shared options" || device.ActiveLoginCount != 2 {
				t.Fatalf("options required exclusive actor or unrelated credential locks: %v", err)
			}
		})
	}
}

func TestApplicationKeyDeviceReadAndOptionsReauthorizeAfterActorWait(t *testing.T) {
	for _, operation := range []string{"lookup", "options"} {
		for _, kind := range []string{"emby", identity.ApplicationKeyKind} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				testCtx, pool, store, native, _ := applicationKeyTestStore(t)
				key := issueApplicationKey(t, testCtx, store, native, "Waiting device actor")
				actor := applicationKeyPrincipal(t, testCtx, store, key)
				if kind == "emby" {
					_, actor = managedLogin(t, testCtx, store, native.User, "administrator-password", "emby")
				}
				ctx, cancel := context.WithTimeout(testCtx, 10*time.Second)
				defer cancel()
				blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
				statement, id := "SELECT id FROM users WHERE id = $1 FOR UPDATE", actor.User.ID
				if actor.IsApplicationKey() {
					statement, id = "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", actor.SessionID
				}
				if _, err := blocker.Exec(ctx, statement, id); err != nil {
					t.Fatal(err)
				}
				finished := make(chan error, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					var err error
					if operation == "lookup" {
						_, err = store.LookupApplicationKeyDevice(ctx, actor, key.Client.DeviceID)
					} else {
						_, err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, key.Client.DeviceID, "Revoked options")
					}
					finished <- err
				}()
				waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM", done)
				if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
					t.Fatal(err)
				}
				if err := blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					if !errors.Is(err, identity.ErrUnauthorized) {
						t.Fatalf("device operation trusted an actor revoked during its wait: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("device operation did not finish after releasing actor locks")
				}
				var customName *string
				if err := pool.QueryRow(testCtx, "SELECT custom_name FROM application_key_devices WHERE id = $1", key.ReportedDeviceNumericID).Scan(&customName); err != nil || customName != nil {
					t.Fatalf("revoked actor changed the device options: %v", err)
				}
			})
		}
	}
}

func TestApplicationKeyDeviceLookupObservesTombstoneAfterDeviceWait(t *testing.T) {
	for _, numeric := range []bool{false, true} {
		t.Run(strconv.FormatBool(numeric), func(t *testing.T) {
			testCtx, pool, store, native, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, testCtx, store, native, "Tombstone lookup")
			_, actor := managedLogin(t, testCtx, store, native.User, "administrator-password", "emby")
			ctx, cancel := context.WithTimeout(testCtx, 10*time.Second)
			defer cancel()
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT id FROM application_key_devices WHERE id = $1 FOR UPDATE", key.ReportedDeviceNumericID); err != nil {
				t.Fatal(err)
			}
			reference := key.Client.DeviceID
			if numeric {
				reference = strconv.FormatInt(key.ReportedDeviceNumericID, 10)
			}
			finished := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := store.LookupApplicationKeyDevice(ctx, actor, reference)
				finished <- err
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT deleted_at FROM application_key_devices", done)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", key.CredentialID); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, "UPDATE application_key_devices SET deleted_at = clock_timestamp() WHERE id = $1", key.ReportedDeviceNumericID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
					t.Fatalf("device lookup lost its generation tombstone after waiting: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("device lookup did not finish after releasing the generation")
			}
		})
	}
}
