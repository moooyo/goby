package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreClientSessionSummariesProjectOnlyConsumedCapabilities(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	_, ordinary := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{DeviceID: "summary-login"}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Summary application")
	application := applicationKeyPrincipal(t, ctx, store, key)
	features := make([]string, 24)
	for i := range features {
		features[i] = strings.Repeat("x", identity.MaxClientCapabilityText)
	}
	profile, err := json.Marshal(map[string]any{"Name": "Large profile", "DeclaredFeatures": features})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := identity.ClientCapabilities{
		PlayableMediaTypes: []string{"Video", "Audio"}, SupportedCommands: []string{"Play", "Pause"},
		SupportsMediaControl: true, SupportsSync: true, DeviceProfile: profile,
		IconURL: "https://example.test/icon.png", AppID: "example.player",
	}
	for _, target := range []struct {
		name      string
		table     string
		id        string
		principal identity.Principal
	}{
		{"ordinary", "sessions", ordinary.SessionID, ordinary},
		{"application", "application_key_clients", application.ClientSessionID, application},
	} {
		t.Run(target.name, func(t *testing.T) {
			if err := store.UpdateClientCapabilities(ctx, target.principal, target.id, capabilities); err != nil {
				t.Fatal(err)
			}
			filter := identity.ClientSessionFilter{SessionID: target.id}
			full, err := store.ListClientSessions(ctx, target.principal, filter)
			if err != nil || len(full) != 1 {
				t.Fatalf("full capability read returned %d sessions: %v", len(full), err)
			}
			if !reflect.DeepEqual(full[0].Capabilities, capabilities) {
				t.Fatal("full session read lost a supported capability field")
			}
			want := full[0]
			want.Capabilities = identity.ClientCapabilities{
				PlayableMediaTypes: capabilities.PlayableMediaTypes, SupportedCommands: capabilities.SupportedCommands,
				SupportsMediaControl: capabilities.SupportsMediaControl,
			}
			// Both credential kinds may consume either visible session kind.
			for _, reader := range []identity.Principal{ordinary, application} {
				summaries, err := store.ListClientSessionSummaries(ctx, reader, filter)
				if err != nil || len(summaries) != 1 || !reflect.DeepEqual(summaries[0], want) {
					t.Fatalf("summary changed identity metadata or retained unused capability fields: %v", err)
				}
			}
			// A manual edit to an omitted field must still fail the full read,
			// while display and control only validate their consumed fields.
			if _, err := pool.Exec(ctx, "UPDATE "+target.table+" SET client_capabilities = client_capabilities || $2::jsonb WHERE id = $1",
				target.id, `{"DeviceProfile":{"DirectPlayProfiles":[true]},"SupportsSync":"invalid","IconUrl":123,"AppId":false}`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ListClientSessions(ctx, target.principal, filter); !errors.Is(err, identity.ErrInvalidInput) {
				t.Fatalf("full read accepted malformed omitted capability fields: %v", err)
			}
			summaries, err := store.ListClientSessionSummaries(ctx, target.principal, filter)
			if err != nil || len(summaries) != 1 || !reflect.DeepEqual(summaries[0], want) {
				t.Fatalf("unused malformed fields changed the capability summary: %v", err)
			}
		})
	}
}

func clientSessionSummaryByteBoundary(t *testing.T) string {
	t.Helper()
	values := make([]string, 32)
	for i := range values {
		values[i] = strings.Repeat("x", identity.MaxClientCapabilityText)
	}
	capabilities := identity.ClientCapabilities{PlayableMediaTypes: values}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	excess := len(encoded) - identity.MaxClientCapabilitiesBytes
	if excess <= 0 || excess >= len(values[len(values)-1]) {
		t.Fatal("capability byte boundary fixture cannot reach the document limit")
	}
	values[len(values)-1] = values[len(values)-1][:identity.MaxClientCapabilityText-excess]
	encoded, err = json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != identity.MaxClientCapabilitiesBytes {
		t.Fatal("capability byte boundary fixture missed the document limit")
	}
	return string(encoded)
}

func TestStoreClientSessionSummariesValidateProjectedCapabilityBounds(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	_, ordinary := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Bounded summary application")
	application := applicationKeyPrincipal(t, ctx, store, key)
	arrayAtLimit := strings.TrimSuffix(strings.Repeat(`"Video",`, identity.MaxClientCapabilityEntries), ",")
	arrayOverLimit := arrayAtLimit + `,"Video"`
	nodeRow := "[" + strings.TrimSuffix(strings.Repeat("true,", identity.MaxClientCapabilityEntries), ",") + "]"
	tooManyNodes := "[" + strings.TrimSuffix(strings.Repeat(nodeRow+",", 33), ",") + "]"
	largeText := `"` + strings.Repeat("x", identity.MaxClientCapabilityText) + `"`
	tooManyBytes := `{"PlayableMediaTypes":[` + strings.TrimSuffix(strings.Repeat(largeText+",", 33), ",") + `]}`
	for _, target := range []struct {
		name      string
		table     string
		id        string
		principal identity.Principal
	}{
		{"ordinary", "sessions", ordinary.SessionID, ordinary},
		{"application", "application_key_clients", application.ClientSessionID, application},
	} {
		t.Run(target.name, func(t *testing.T) {
			for _, test := range []struct {
				name  string
				input string
				valid bool
			}{
				{"omitted", `{}`, true},
				{"optional_nulls", `{"PlayableMediaTypes":null,"SupportedCommands":null,"SupportsMediaControl":null}`, true},
				{"empty_arrays", `{"PlayableMediaTypes":[],"SupportedCommands":[],"SupportsMediaControl":false}`, true},
				{"array_and_text_boundaries", `{"PlayableMediaTypes":[` + arrayAtLimit + `],"SupportedCommands":[` + largeText + `]}`, true},
				{"exact_byte_boundary_with_omitted_keys", clientSessionSummaryByteBoundary(t), true},
				{"wrong_array_type", `{"PlayableMediaTypes":"Video"}`, false},
				{"wrong_array_entry", `{"SupportedCommands":[true]}`, false},
				{"null_array_entry", `{"SupportedCommands":[null]}`, false},
				{"wrong_boolean", `{"SupportsMediaControl":"true"}`, false},
				{"too_many_entries", `{"SupportedCommands":[` + arrayOverLimit + `]}`, false},
				{"too_much_text", `{"SupportedCommands":["` + strings.Repeat("x", identity.MaxClientCapabilityText+1) + `"]}`, false},
				{"control_text", `{"SupportedCommands":["Play\nPause"]}`, false},
				{"too_many_bytes", tooManyBytes, false},
				{"too_many_nodes", `{"SupportedCommands":` + tooManyNodes + `}`, false},
				{"too_deep", `{"SupportedCommands":` + strings.Repeat("[", 9) + "true" + strings.Repeat("]", 9) + `}`, false},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := pool.Exec(ctx, "UPDATE "+target.table+" SET client_capabilities = $2::jsonb WHERE id = $1", target.id, test.input); err != nil {
						t.Fatal(err)
					}
					filter := identity.ClientSessionFilter{SessionID: target.id}
					summaries, err := store.ListClientSessionSummaries(ctx, target.principal, filter)
					if !test.valid {
						if !errors.Is(err, identity.ErrInvalidInput) || summaries != nil {
							t.Fatalf("summary accepted invalid consumed capabilities: %v", err)
						}
						return
					}
					want, parseErr := identity.ParseClientCapabilities([]byte(test.input))
					if parseErr != nil {
						t.Fatalf("valid fixture failed complete capability validation: %v", parseErr)
					}
					if err != nil || len(summaries) != 1 || !reflect.DeepEqual(summaries[0].Capabilities, want) {
						t.Fatalf("summary changed valid capability boundaries or optional fields: %v", err)
					}
					full, err := store.ListClientSessions(ctx, target.principal, filter)
					if err != nil || len(full) != 1 || !reflect.DeepEqual(full[0], summaries[0]) {
						t.Fatalf("full and summary reads differ for consumed-only capabilities: %v", err)
					}
				})
			}
		})
	}
}

func TestStoreClientSessionSummariesRetainVisibilityAndPresence(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Summary Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerPrincipal := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "summary-viewer"}, "emby")
	_, adminPrincipal := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{DeviceID: "summary-admin"}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Visible summary application")
	application := applicationKeyPrincipal(t, ctx, store, key)
	for _, test := range []struct {
		name      string
		remote    bool
		shared    bool
		principal identity.Principal
		expected  []string
	}{
		{"own", false, false, viewerPrincipal, []string{viewerPrincipal.SessionID}},
		{"other_users", true, false, viewerPrincipal, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID}},
		{"shared_devices", false, true, viewerPrincipal, []string{viewerPrincipal.SessionID, application.ClientSessionID}},
		{"both", true, true, viewerPrincipal, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, application.ClientSessionID}},
		{"administrator", false, false, adminPrincipal, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, application.ClientSessionID}},
		{"application_key", false, false, application, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, application.ClientSessionID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := fmt.Sprintf(`{"EnableRemoteControlOfOtherUsers":%t,"EnableSharedDeviceControl":%t}`, test.remote, test.shared)
			if _, err := pool.Exec(ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", viewer.ID, policy); err != nil {
				t.Fatal(err)
			}
			summaries, err := store.ListClientSessionSummaries(ctx, test.principal, identity.ClientSessionFilter{})
			if err != nil {
				t.Fatal(err)
			}
			assertClientSessionIDs(t, summaries, test.expected)
		})
	}
	for _, mutation := range []struct{ query, id string }{
		{"UPDATE sessions SET last_seen_at = clock_timestamp() - interval '6 minutes' WHERE id = $1", viewerPrincipal.SessionID},
		{"UPDATE application_key_clients SET last_seen_at = clock_timestamp() - interval '6 minutes' WHERE id = $1", application.ClientSessionID},
	} {
		if _, err := pool.Exec(ctx, mutation.query, mutation.id); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err := store.ListClientSessionSummaries(ctx, application, identity.ClientSessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSessionIDs(t, summaries, []string{adminPrincipal.SessionID})
	zero := 0
	summaries, err = store.ListClientSessionSummaries(ctx, application, identity.ClientSessionFilter{ActiveWithinSeconds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSessionIDs(t, summaries, []string{viewerPrincipal.SessionID, adminPrincipal.SessionID, application.ClientSessionID})
	summaries, err = store.ListClientSessionSummaries(ctx, application, identity.ClientSessionFilter{ActiveWithinSeconds: &zero, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSessionIDs(t, summaries, []string{adminPrincipal.SessionID})
	for _, limit := range []int{-1, identity.MaxClientSessions + 1} {
		if _, err := store.ListClientSessionSummaries(ctx, application, identity.ClientSessionFilter{Limit: limit}); !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatalf("summary accepted an invalid session limit: %v", err)
		}
	}
	summaries, err = store.ListClientSessionSummaries(ctx, application, identity.ClientSessionFilter{DeviceID: "summary-viewer", ActiveWithinSeconds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSessionIDs(t, summaries, []string{viewerPrincipal.SessionID})
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, viewerPrincipal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", application.SessionID); err != nil {
		t.Fatal(err)
	}
	summaries, err = store.ListClientSessionSummaries(ctx, adminPrincipal, identity.ClientSessionFilter{ActiveWithinSeconds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSessionIDs(t, summaries, []string{adminPrincipal.SessionID})
	for _, principal := range []identity.Principal{viewerPrincipal, application, administrator} {
		if _, err := store.ListClientSessionSummaries(ctx, principal, identity.ClientSessionFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("summary accepted an expired, revoked, or cookie principal: %v", err)
		}
	}
}

func listClientSessionSummariesAsync(ctx context.Context, store *identity.Store, principal identity.Principal) (<-chan clientSessionListOutcome, <-chan struct{}) {
	results := make(chan clientSessionListOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sessions, err := store.ListClientSessionSummaries(ctx, principal, identity.ClientSessionFilter{})
		results <- clientSessionListOutcome{sessions: sessions, err: err}
	}()
	return results, done
}

func TestStoreClientSessionSummariesRejectRevocationAfterCredentialWait(t *testing.T) {
	ctx, pool, store, administrator, _ := applicationKeyTestStore(t)
	_, ordinary := clientSessionPrincipal(t, ctx, store, administrator.User, "administrator-password", identity.Client{}, "emby")
	key := issueApplicationKey(t, ctx, store, administrator, "Waiting summary application")
	application := applicationKeyPrincipal(t, ctx, store, key)
	for _, principal := range []identity.Principal{ordinary, application} {
		t.Run(principal.Kind, func(t *testing.T) {
			blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", principal.SessionID); err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results, done := listClientSessionSummariesAsync(operationCtx, store, principal)
			waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "SELECT id FROM sessions", done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := awaitClientSessionList(t, operationCtx, results)
			if !errors.Is(result.err, identity.ErrUnauthorized) || result.sessions != nil {
				t.Fatalf("summary retained authority revoked during a credential wait: %v", result.err)
			}
		})
	}
}

func TestStoreClientSessionSummariesRejectExpiryDuringProjectionWait(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, principal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, principal.SessionID); err != nil {
		t.Fatal(err)
	}
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	// This gate pauses the projection after the initial authority check.
	if _, err := blocker.Exec(ctx, "LOCK TABLE devices IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, done := listClientSessionSummariesAsync(operationCtx, store, principal)
	waitManagedBlockedQuery(t, operationCtx, pool, blockerPID, "WITH clients AS", done)
	waitManagedSessionDatabaseExpiry(t, operationCtx, pool, principal.SessionID, done)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := awaitClientSessionList(t, operationCtx, results)
	if !errors.Is(result.err, identity.ErrUnauthorized) || result.sessions != nil {
		t.Fatalf("summary retained expired authority after its projection wait: %v", result.err)
	}
}
