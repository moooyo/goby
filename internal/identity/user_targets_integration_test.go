package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreUserTargetsPreserveRawFactsWithoutAccountFilters(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	user, err := store.CreateUser(ctx, "Target account", "target-password", true)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := json.Marshal(map[string]any{
		"GroupedFolders": []string{"library"}, "FuturePreference": strings.Repeat("retained ", 2000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET name='Current target', is_disabled=true,
		is_administrator=false, policy='{"EnableMediaPlayback":"invalid","EnableRemoteAccess":null,"FutureField":123}'::jsonb,
		configuration=$2::jsonb WHERE id=$1`, user.ID, configuration); err != nil {
		t.Fatal(err)
	}
	full, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertTargets := func() {
		t.Helper()
		policy, err := store.GetUserPolicy(ctx, user.ID)
		if err != nil || !bytes.Equal(policy, full.Policy) {
			t.Fatalf("raw target policy changed or rejected disabled account: %v", err)
		}
		name, err := store.GetUserName(ctx, user.ID)
		if err != nil || name != full.Name {
			t.Fatalf("login target name changed or imposed account authority: %v", err)
		}
		settings, err := store.GetUserConfiguration(ctx, user.ID)
		if err != nil || !bytes.Equal(settings, full.Configuration) {
			t.Fatalf("configuration target omitted retained preference fields: %v", err)
		}
		controller, err := store.GetClientSessionController(ctx, user.ID)
		want := identity.ClientSessionController{ID: full.ID, IsAdministrator: full.IsAdministrator,
			IsDisabled: full.IsDisabled, Policy: full.Policy}
		if err != nil || !reflect.DeepEqual(controller, want) {
			t.Fatalf("controller target lost current disabled/role/raw policy facts: %v", err)
		}
		policy[0], settings[0], controller.Policy[0] = '[', '[', '['
	}
	assertTargets()
	assertTargets()
	// An unrelated display timestamp must no longer make a narrow target fail.
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at='infinity'::timestamptz WHERE id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUser(ctx, user.ID); err == nil {
		t.Fatal("complete account read unexpectedly accepted an infinite creation time")
	}
	assertTargets()
}

func TestStoreUserTargetsPreserveMissingAndDatabaseErrors(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	readers := map[string]func(context.Context, string) error{
		"policy":        func(ctx context.Context, id string) error { _, err := store.GetUserPolicy(ctx, id); return err },
		"name":          func(ctx context.Context, id string) error { _, err := store.GetUserName(ctx, id); return err },
		"configuration": func(ctx context.Context, id string) error { _, err := store.GetUserConfiguration(ctx, id); return err },
		"controller": func(ctx context.Context, id string) error {
			_, err := store.GetClientSessionController(ctx, id)
			return err
		},
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"missing-target", "", " padded-target "} {
				if err := read(ctx, id); !errors.Is(err, identity.ErrNotFound) {
					t.Fatalf("missing target changed its error classification: %v", err)
				}
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := read(canceled, "missing-target"); !errors.Is(err, context.Canceled) || errors.Is(err, identity.ErrNotFound) {
				t.Fatalf("database error was mistaken for a missing account: %v", err)
			}
		})
	}
}
