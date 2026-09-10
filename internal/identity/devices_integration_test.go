package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func deviceLogin(t *testing.T, ctx context.Context, store *identity.Store, user identity.User, password string, client identity.Client, kind, peerIP string) (identity.Credentials, identity.Principal) {
	t.Helper()
	credentials, err := store.AuthenticateWithPeer(ctx, user.Name, password, client, kind, peerIP)
	if err != nil {
		t.Fatalf("authenticate device fixture: %v", err)
	}
	principal, err := store.Resolve(ctx, credentials.Token, kind)
	if err != nil {
		t.Fatalf("resolve device fixture: %v", err)
	}
	return credentials, principal
}

func readDevice(t *testing.T, ctx context.Context, store *identity.Store, actor identity.Principal, id int64) identity.ManagedDevice {
	t.Helper()
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{Limit: 200})
	if err != nil {
		t.Fatalf("read managed devices: %v", err)
	}
	for _, device := range page.Items {
		if device.ID == id {
			return device
		}
	}
	t.Fatalf("device %d is absent from the native registry", id)
	return identity.ManagedDevice{}
}

func onlyDevice(t *testing.T, ctx context.Context, store *identity.Store, actor identity.Principal) identity.ManagedDevice {
	t.Helper()
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 {
		t.Fatalf("device registry count = %d, items = %d, error = %v; want one", page.TotalRecordCount, len(page.Items), err)
	}
	return page.Items[0]
}

// Differences include only field names and time representations, never device
// names, user identifiers, peer addresses, or credential-bearing snapshots.
func deviceProjectionDifferenceFields(first, second identity.ManagedDevice) []string {
	firstValue, secondValue := reflect.ValueOf(first), reflect.ValueOf(second)
	fields := make([]string, 0)
	for index := range firstValue.NumField() {
		if !reflect.DeepEqual(firstValue.Field(index).Interface(), secondValue.Field(index).Interface()) {
			fields = append(fields, firstValue.Type().Field(index).Name)
		}
	}
	return fields
}

// Snapshots stay in memory and never include their contents in failure output.
func deviceManagementSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'devices', COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM devices d), '[]'::jsonb),
		'sessions', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s), '[]'::jsonb),
		'users', COALESCE((SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u), '[]'::jsonb),
		'keys', COALESCE((SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k), '[]'::jsonb),
		'clients', COALESCE((SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c), '[]'::jsonb))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot device management state: %v", err)
	}
	return snapshot
}

func deviceRetainedHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, revokedIDs []string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'sessions', (SELECT jsonb_agg(CASE WHEN s.id = ANY($1::text[])
			THEN to_jsonb(s) - 'revoked_at' ELSE to_jsonb(s) END ORDER BY id) FROM sessions s),
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c),
		'libraries', (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
		'items', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'user_data', (SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id, item_id) FROM user_item_data u),
		'plays', (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM play_sessions p),
		'encoding', (SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM encoding_jobs e),
		'references', (SELECT jsonb_agg(to_jsonb(r) ORDER BY auth_session_id, client_nonce)
			FROM client_playback_references r))::text`, revokedIDs).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot retained device history: %v", err)
	}
	return snapshot
}

func seedDevicePlaybackHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, principal identity.Principal) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('device-library', 'Device History', 'movies');
		INSERT INTO items (id, library_id, name, sort_name, type)
		VALUES ('device-item', 'device-library', 'Retained Movie', 'retained movie', 'Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, last_played_at)
		VALUES ($1, 'device-item', 12345, 7, true, clock_timestamp())`, principal.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"Playing", "Stopped"} {
		if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
			(id, user_id, auth_session_id, device_id, item_id, media_source_id, state,
			position_ticks, duration_ticks, counted, expires_at, client_correlated)
			VALUES ($1, $2, $3, $4, 'device-item', 'device-source', $5, 12345, 90000000,
			true, clock_timestamp() + interval '1 hour', true)`, "device-play-"+state,
			principal.User.ID, principal.SessionID, principal.Client.DeviceID, state); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO client_playback_references
			(user_id, auth_session_id, device_id, client_nonce, play_session_id)
			VALUES ($1, $2, $3, $4, $5)`, principal.User.ID, principal.SessionID,
			principal.Client.DeviceID, "device-nonce-"+state, "device-play-"+state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO client_playback_references
		(user_id, auth_session_id, device_id, client_nonce)
		VALUES ($1, $2, $3, 'retained-tombstone')`, principal.User.ID, principal.SessionID, principal.Client.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO encoding_jobs
		(id, user_id, auth_session_id, device_id, play_session_id, item_id, media_source_id,
		source_stamp, plan, state, output_bytes, created_at, updated_at, last_access_at)
		VALUES ('ed000000000000000000000000000001', $1, $2, $3, 'device-play-Stopped',
		'device-item', 'device-source', 'retained-source-stamp', '{}'::jsonb, 'completed', 4096,
		clock_timestamp(), clock_timestamp(), clock_timestamp())`, principal.User.ID, principal.SessionID, principal.Client.DeviceID); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDeviceRegistryLifecyclePreservesOtherCredentialsAndHistory(t *testing.T) {
	ctx, pool, store, actor, vaultPath := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "First Device Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Second Device Viewer", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{Name: "First Player", DeviceID: "shared-reported-device", Device: "Reported Television", Version: "1.2.3"}
	first, firstPrincipal := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "192.0.2.10")
	initial := onlyDevice(t, ctx, store, actor)
	if initial.ID < 2 || initial.Revision != 1 || initial.ReportedDeviceID != client.DeviceID || initial.Name != client.Device ||
		initial.ReportedName != client.Device || initial.CustomName != nil || initial.AppName != client.Name || initial.AppVersion != client.Version ||
		initial.LastUserID == nil || *initial.LastUserID != viewer.ID || initial.LastUserName == nil || *initial.LastUserName != viewer.Name ||
		initial.IPAddress != "192.0.2.10" || initial.CreatedAt.IsZero() || initial.LastSeenAt.Before(initial.CreatedAt) || initial.ActiveLoginCount != 1 {
		t.Fatal("ordinary login did not create the expected safe device projection")
	}
	renamed, err := store.UpdateManagedDeviceOptions(ctx, actor, initial.ID, initial.Revision, "  Living Room  ")
	if err != nil || renamed.Name != "Living Room" || renamed.CustomName == nil || *renamed.CustomName != "Living Room" || renamed.Revision != initial.Revision+1 {
		t.Fatalf("rename device: %v", err)
	}
	client.Name, client.Device, client.Version = "Second Player", "Updated Television", "2.0"
	second, _ := deviceLogin(t, ctx, store, other, "other-password", client, "emby", "2001:db8::2")
	third, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "192.0.2.11")
	alreadyRevoked, _ := deviceLogin(t, ctx, store, other, "other-password", client, "emby", "192.0.2.12")
	if err := store.Revoke(ctx, alreadyRevoked.Token); err != nil {
		t.Fatal(err)
	}
	merged := onlyDevice(t, ctx, store, actor)
	if merged.ID != initial.ID || merged.Revision != renamed.Revision || !merged.CreatedAt.Equal(initial.CreatedAt) ||
		merged.Name != renamed.Name || merged.CustomName == nil || *merged.CustomName != "Living Room" || merged.ReportedName != client.Device ||
		merged.AppName != client.Name || merged.AppVersion != client.Version || merged.LastUserID == nil || *merged.LastUserID != other.ID ||
		merged.LastUserName == nil || *merged.LastUserName != other.Name || merged.ActiveLoginCount != 3 {
		t.Fatal("shared reported ID did not retain one generation and its administrator override across users and applications")
	}
	cookie, _ := deviceLogin(t, ctx, store, actor.User, "administrator-password", client, "admin", "192.0.2.13")
	key := issueApplicationKey(t, ctx, store, actor, "Device Isolation Key")
	keyContext := bindApplicationClient(t, ctx, store, key, client)
	if current := onlyDevice(t, ctx, store, actor); !reflect.DeepEqual(current, merged) {
		t.Fatal("native authentication or application-key metadata changed the ordinary device registry")
	}
	siblingClient := identity.Client{Name: "Sibling Player", DeviceID: "different-device", Device: "Bedroom"}
	sibling, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", siblingClient, "emby", "192.0.2.14")
	seedDevicePlaybackHistory(t, ctx, pool, firstPrincipal)
	wantRevoked := []string{first.SessionID, second.SessionID, third.SessionID}
	wantGeneration := append(slices.Clone(wantRevoked), alreadyRevoked.SessionID)
	before := deviceRetainedHistory(t, ctx, pool, wantRevoked)
	store = identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(vaultPath))
	if current := readDevice(t, ctx, store, actor, initial.ID); !reflect.DeepEqual(current, merged) {
		t.Fatal("reopening the store lost device metadata or its administrator override")
	}
	deletion, err := store.DeleteManagedDevice(ctx, actor, initial.ID, merged.Revision)
	if err != nil || deletion.ID != initial.ID || deletion.DeletedAt.IsZero() || deletion.RevokedLoginCount != 3 {
		t.Fatalf("delete shared device generation: %v", err)
	}
	slices.Sort(wantGeneration)
	slices.Sort(deletion.RevokedSessionIDs)
	if !slices.Equal(deletion.RevokedSessionIDs, wantGeneration) {
		t.Error("device deletion did not return the generation's complete ordinary login identities for connection teardown")
	}
	if after := deviceRetainedHistory(t, ctx, pool, wantRevoked); after != before {
		t.Fatal("device deletion changed retained authentication, application credentials, catalog, playback, encoding, user data, or nonce history")
	}
	for _, credentials := range []identity.Credentials{first, second, third, alreadyRevoked} {
		assertManagedTokenRevoked(t, ctx, store, credentials, "emby")
	}
	if _, err := store.Resolve(ctx, cookie.Token, "admin"); err != nil {
		t.Errorf("device deletion revoked a native administrator credential with the same reported ID: %v", err)
	}
	if _, err := store.Resolve(ctx, sibling.Token, "emby"); err != nil {
		t.Errorf("device deletion revoked another device: %v", err)
	}
	if _, err := store.RevalidateSession(ctx, keyContext); err != nil {
		t.Errorf("device deletion revoked a userless application context with the same reported ID: %v", err)
	}
	if err := store.TouchClientSessionFromAddress(ctx, firstPrincipal, "192.0.2.15"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("deleted device credential recreated registration through activity: %v", err)
	}
	remaining := onlyDevice(t, ctx, store, actor)
	if remaining.ReportedDeviceID != siblingClient.DeviceID {
		t.Fatal("deleted generation remained visible or the sibling device disappeared")
	}
	newCredentials, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "192.0.2.16")
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{SearchTerm: client.DeviceID})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 {
		t.Fatalf("fresh login after device removal did not create one visible generation: %v", err)
	}
	fresh := page.Items[0]
	if fresh.ID <= initial.ID || fresh.Revision != 1 || fresh.CustomName != nil || fresh.Name != client.Device || fresh.ActiveLoginCount != 1 {
		t.Fatal("fresh login reused a removed generation, inherited its override, or counted historical credentials")
	}
	afterRegistration := deviceManagementSnapshot(t, ctx, pool)
	repeated, err := store.DeleteManagedDevice(ctx, actor, initial.ID, initial.Revision)
	if err != nil || repeated.ID != initial.ID || !repeated.DeletedAt.Equal(deletion.DeletedAt) || repeated.RevokedLoginCount != 0 {
		t.Fatalf("known removed generation was not idempotent with its original stale revision: %v", err)
	}
	slices.Sort(repeated.RevokedSessionIDs)
	if !slices.Equal(repeated.RevokedSessionIDs, wantGeneration) {
		t.Error("repeated generation removal did not retain the original connection teardown scope")
	}
	if _, err := store.UpdateManagedDeviceOptions(ctx, actor, initial.ID, merged.Revision, "Stale edit"); !errors.Is(err, identity.ErrDeviceNotFound) {
		t.Errorf("removed generation accepted a name edit: %v", err)
	}
	if deviceManagementSnapshot(t, ctx, pool) != afterRegistration {
		t.Fatal("old-generation operations changed the fresh login or registry generation")
	}
	if _, err := store.Resolve(ctx, newCredentials.Token, "emby"); err != nil {
		t.Errorf("repeating removal of the old generation revoked the new login: %v", err)
	}
}

func TestStoreDeviceRegistryActivityUsesStoredClientAndPreservesOptions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	client := identity.Client{Name: "Stored Player", DeviceID: "stored-device", Device: "Stored Name", Version: "1.0"}
	credentials, principal := deviceLogin(t, ctx, store, admin, "administrator-password", client, "emby", "192.0.2.20")
	device := onlyDevice(t, ctx, store, actor)
	renamed, err := store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, "Owner Override")
	if err != nil {
		t.Fatal(err)
	}
	var earlier time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp() - interval '1 minute'").Scan(&earlier); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at = $2 WHERE id = $1", principal.SessionID, earlier); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE devices SET created_at = LEAST(created_at, $2), last_seen_at = $2 WHERE id = $1", device.ID, earlier); err != nil {
		t.Fatal(err)
	}
	claimed := principal
	claimed.Client = identity.Client{Name: "Forged App", DeviceID: "unrelated-device", Device: "Forged Name", Version: "999"}
	if err := store.TouchClientSessionFromAddress(ctx, claimed, "2001:db8::20"); err != nil {
		t.Fatalf("touch an authenticated device with an untrusted metadata snapshot: %v", err)
	}
	current := onlyDevice(t, ctx, store, actor)
	if current.ID != device.ID || current.Revision != renamed.Revision || current.Name != renamed.Name || current.CustomName == nil ||
		*current.CustomName != "Owner Override" || current.ReportedDeviceID != client.DeviceID || current.ReportedName != client.Device ||
		current.AppName != client.Name || current.AppVersion != client.Version || current.IPAddress != "2001:db8::20" || !current.LastSeenAt.After(earlier) {
		t.Fatal("authenticated activity changed registry identity or administrator options, or failed to record the trusted peer")
	}
	resolved, err := store.Resolve(ctx, credentials.Token, "emby")
	expectedClient := client
	expectedClient.Device = "Owner Override"
	if err != nil || !resolved.ExpiresAt.Equal(credentials.ExpiresAt) || resolved.Client != expectedClient {
		t.Fatalf("device activity extended the login lifetime or lost the effective device name: %v", err)
	}
	var storedClient identity.Client
	if err := pool.QueryRow(ctx, "SELECT client_name, device_id, device_name, client_version FROM sessions WHERE id = $1", principal.SessionID).
		Scan(&storedClient.Name, &storedClient.DeviceID, &storedClient.Device, &storedClient.Version); err != nil {
		t.Fatal(err)
	}
	if storedClient != client {
		t.Fatal("device activity wrote an administrator override or untrusted principal metadata into the original login report")
	}
	unchanged, err := store.UpdateManagedDeviceOptions(ctx, actor, current.ID, current.Revision, " Owner Override ")
	if err != nil || !reflect.DeepEqual(unchanged, current) {
		t.Fatalf("equivalent trimmed device name changed the revision or activity: %v; fields=%v; created instant equal=%t locations=%s/%s; seen instant equal=%t locations=%s/%s",
			err, deviceProjectionDifferenceFields(unchanged, current), unchanged.CreatedAt.Equal(current.CreatedAt),
			unchanged.CreatedAt.Location(), current.CreatedAt.Location(), unchanged.LastSeenAt.Equal(current.LastSeenAt),
			unchanged.LastSeenAt.Location(), current.LastSeenAt.Location())
	}
	if unchanged.CreatedAt.Location() != time.UTC || unchanged.LastSeenAt.Location() != time.UTC ||
		current.CreatedAt.Location() != time.UTC || current.LastSeenAt.Location() != time.UTC {
		t.Fatal("native device list and mutation timestamps must use UTC")
	}
	cleared, err := store.UpdateManagedDeviceOptions(ctx, actor, current.ID, current.Revision, "   ")
	if err != nil || cleared.CustomName != nil || cleared.Name != client.Device || cleared.Revision != current.Revision+1 || !cleared.LastSeenAt.Equal(current.LastSeenAt) {
		t.Fatalf("clear custom device name did not restore the reported name without touching activity: %v", err)
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	if _, err := store.UpdateManagedDeviceOptions(ctx, actor, current.ID, current.Revision, "Stale name"); !errors.Is(err, identity.ErrDeviceRevisionConflict) {
		t.Errorf("stale live name edit = %v, want revision conflict", err)
	}
	if _, err := store.DeleteManagedDevice(ctx, actor, current.ID, current.Revision); !errors.Is(err, identity.ErrDeviceRevisionConflict) {
		t.Errorf("stale live removal = %v, want revision conflict", err)
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Fatal("stale device mutation changed registry or authentication state")
	}
}

func TestStoreManagedDevicesSearchPaginationAndReadOnlyProjection(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Last User Needle", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	clients := []identity.Client{
		{Name: "App Needle", DeviceID: `literal%_\id`, Device: `Reported%_\Name`, Version: "1.0"},
		{Name: "Decoy", DeviceID: "literalXYZid", Device: "ReportedXYZName", Version: "2.0"},
		{Name: "Third", DeviceID: "third-device", Device: "Third Name", Version: "3.0"},
	}
	credentials := make([]identity.Credentials, len(clients))
	ids := make([]int64, len(clients))
	for index, client := range clients {
		user, password := admin, "administrator-password"
		if index == 0 {
			user, password = viewer, "viewer-password"
		}
		credentials[index], _ = deviceLogin(t, ctx, store, user, password, client, "emby", "192.0.2.30")
		page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{SearchTerm: client.DeviceID})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("read distinct device fixture: %v", err)
		}
		ids[index] = page.Items[0].ID
	}
	first := readDevice(t, ctx, store, actor, ids[0])
	if _, err := store.UpdateManagedDeviceOptions(ctx, actor, first.ID, first.Revision, "Effective Needle"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE devices SET created_at = '2024-01-01T00:00:00Z', last_seen_at = '2025-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	for _, search := range []string{"%", "_", `\`, "APP NEEDLE", "effective needle", "last USER needle", "reported%_"} {
		page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{SearchTerm: search, Limit: 25})
		if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ID != first.ID || page.StartIndex != 0 || page.Limit != 25 {
			t.Errorf("literal case-insensitive search %q did not select exactly the expected device: %v", search, err)
		}
	}
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{StartIndex: 1, Limit: 1})
	if err != nil || page.TotalRecordCount != 3 || len(page.Items) != 1 || page.Items[0].ID != ids[1] || page.StartIndex != 1 || page.Limit != 1 {
		t.Fatalf("device pagination lost its total or numeric-ID tie-break order: %v", err)
	}
	all, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{})
	if err != nil || all.TotalRecordCount != 3 || len(all.Items) != 3 || all.Limit != 50 || all.Items[0].ID != ids[2] || all.Items[2].ID != ids[0] {
		t.Fatalf("default page or equal-activity ordering changed: %v", err)
	}
	for _, filter := range []identity.ManagedDeviceFilter{{StartIndex: 3, Limit: 1}, {StartIndex: 2147483647, Limit: 200}} {
		empty, err := store.ListManagedDevices(ctx, actor, filter)
		if err != nil || empty.TotalRecordCount != 3 || len(empty.Items) != 0 || empty.Items == nil || empty.StartIndex != filter.StartIndex || empty.Limit != filter.Limit {
			t.Errorf("out-of-range page lost its total or returned a null item collection: %v", err)
		}
	}
	missing, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{SearchTerm: "no matching device"})
	if err != nil || missing.TotalRecordCount != 0 || len(missing.Items) != 0 || missing.Items == nil {
		t.Errorf("unmatched search did not return an empty coherent page: %v", err)
	}
	encoded, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range credentials {
		if strings.Contains(string(encoded), credential.Token) || strings.Contains(string(encoded), credential.SessionID) {
			t.Error("device management projection exposed a credential secret or internal login identity")
		}
	}
	for _, forbidden := range []string{"token_hash", "password_hash", "client_capabilities", "secret_ciphertext"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("device projection exposed storage-only field %q", forbidden)
		}
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Fatal("read-only device listing changed presence, credentials, registry, or application-key state")
	}
	if _, err := pool.Exec(ctx, "UPDATE devices SET last_seen_at = '2025-01-02T00:00:00Z' WHERE id = $1", ids[0]); err != nil {
		t.Fatal(err)
	}
	latest, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{Limit: 1})
	if err != nil || len(latest.Items) != 1 || latest.Items[0].ID != ids[0] || latest.TotalRecordCount != 3 {
		t.Fatalf("activity ordering did not precede the numeric-ID tie-break: %v", err)
	}
}

func TestStoreManagedDeviceActiveLoginCountUsesCurrentAuthorization(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(ctx, "Counted Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{DeviceID: "counted-device"}
	first, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "")
	second, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "")
	third, _ := deviceLogin(t, ctx, store, viewer, "viewer-password", client, "emby", "")
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at = clock_timestamp() - interval '1 hour' WHERE user_id = $1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if device := onlyDevice(t, ctx, store, actor); device.ActiveLoginCount != 3 || strings.TrimSpace(device.Name) == "" {
		t.Fatal("device count applied an online presence window or omitted the display-name fallback")
	}
	if err := store.Revoke(ctx, first.Token); err != nil {
		t.Fatal(err)
	}
	if device := onlyDevice(t, ctx, store, actor); device.ActiveLoginCount != 2 {
		t.Error("device count included a revoked login")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '31 days',
		expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, second.SessionID); err != nil {
		t.Fatal(err)
	}
	if device := onlyDevice(t, ctx, store, actor); device.ActiveLoginCount != 1 {
		t.Error("device count included an expired login")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if device := onlyDevice(t, ctx, store, actor); device.ActiveLoginCount != 0 {
		t.Error("disabled account retained active device logins or its persistent device disappeared")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = false WHERE id = $1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if device := onlyDevice(t, ctx, store, actor); device.ActiveLoginCount != 1 {
		t.Error("re-enabled account did not restore its still-valid login to the current count")
	}
	if _, err := store.Resolve(ctx, third.Token, "emby"); err != nil {
		t.Fatalf("counting device credentials mutated their validity: %v", err)
	}
}

func TestStoreManagedDevicesRejectUnauthorizedAndStaleActors(t *testing.T) {
	ctx, pool, store, actor, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Unprivileged Device Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerActor := deviceLogin(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "authorization-device"}, "emby", "")
	_, embyAdmin := managedLogin(t, ctx, store, actor.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, actor, "No Native Device Authority")
	keyActor := applicationKeyPrincipal(t, ctx, store, key)
	device := onlyDevice(t, ctx, store, actor)
	assertDenied := func(t *testing.T, principal identity.Principal) {
		t.Helper()
		before := deviceManagementSnapshot(t, ctx, pool)
		if _, err := store.ListManagedDevices(ctx, principal, identity.ManagedDeviceFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
			t.Errorf("unauthorized native device list = %v, want unauthorized", err)
		}
		if _, err := store.UpdateManagedDeviceOptions(ctx, principal, device.ID, device.Revision, "Denied edit"); !errors.Is(err, identity.ErrUnauthorized) {
			t.Errorf("unauthorized native device edit = %v, want unauthorized", err)
		}
		if _, err := store.DeleteManagedDevice(ctx, principal, device.ID, device.Revision); !errors.Is(err, identity.ErrUnauthorized) {
			t.Errorf("unauthorized native device delete = %v, want unauthorized", err)
		}
		if deviceManagementSnapshot(t, ctx, pool) != before {
			t.Error("rejected device administration changed persisted state")
		}
	}
	for name, principal := range map[string]identity.Principal{
		"anonymous": {}, "viewer": viewerActor, "emby-administrator": embyAdmin, "application-key": keyActor,
	} {
		t.Run(name, func(t *testing.T) { assertDenied(t, principal) })
	}
	forgedViewer := viewerActor
	forgedViewer.Kind, forgedViewer.User.IsAdministrator = "admin", true
	t.Run("claimed-kind-and-role", func(t *testing.T) { assertDenied(t, forgedViewer) })
	forgedOwner := actor
	forgedOwner.User.ID = viewer.ID
	t.Run("claimed-owner", func(t *testing.T) { assertDenied(t, forgedOwner) })
	for _, state := range []string{"demoted", "disabled", "revoked", "expired", "wrong-kind"} {
		t.Run(state, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true, is_disabled = false WHERE id = $1", actor.User.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET kind = 'admin', revoked_at = NULL,
				created_at = clock_timestamp() - interval '2 days', expires_at = clock_timestamp() + interval '1 day'
				WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			statement := map[string]string{
				"demoted":    "UPDATE users SET is_administrator = false WHERE id = $1",
				"disabled":   "UPDATE users SET is_disabled = true WHERE id = $1",
				"revoked":    "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1",
				"expired":    "UPDATE sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1",
				"wrong-kind": "UPDATE sessions SET kind = 'emby' WHERE id = $1",
			}[state]
			id := actor.SessionID
			if state == "demoted" || state == "disabled" {
				id = actor.User.ID
			}
			if _, err := pool.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			assertDenied(t, actor)
		})
	}
}

func TestStoreManagedDeviceValidationAndHiddenIDs(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	deviceLogin(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: "validation-device", Device: "Default Name"}, "emby", "")
	device := onlyDevice(t, ctx, store, actor)
	assertField := func(err error, field string) {
		t.Helper()
		var validation *identity.DeviceValidationError
		if !errors.Is(err, identity.ErrInvalidInput) || !errors.As(err, &validation) || validation.Fields[field] == "" {
			t.Errorf("invalid %s did not return a field-specific device validation error: %v", field, err)
		}
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	for _, name := range []string{strings.Repeat("a", 257), strings.Repeat("é", 129), "line\nbreak", "nul\x00value", "delete\x7fvalue", string([]byte{0xff})} {
		_, err := store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, name)
		assertField(err, "CustomName")
	}
	for _, test := range []struct {
		filter identity.ManagedDeviceFilter
		field  string
	}{
		{identity.ManagedDeviceFilter{StartIndex: -1}, "StartIndex"},
		{identity.ManagedDeviceFilter{StartIndex: 2147483648}, "StartIndex"},
		{identity.ManagedDeviceFilter{Limit: -1}, "Limit"},
		{identity.ManagedDeviceFilter{Limit: 201}, "Limit"},
		{identity.ManagedDeviceFilter{SearchTerm: strings.Repeat("é", 129)}, "SearchTerm"},
		{identity.ManagedDeviceFilter{SearchTerm: "line\nbreak"}, "SearchTerm"},
		{identity.ManagedDeviceFilter{SearchTerm: string([]byte{0xff})}, "SearchTerm"},
	} {
		_, err := store.ListManagedDevices(ctx, actor, test.filter)
		assertField(err, test.field)
	}
	for _, id := range []int64{1, device.ID + 10000} {
		if _, err := store.UpdateManagedDeviceOptions(ctx, actor, id, device.Revision, "Missing"); !errors.Is(err, identity.ErrDeviceNotFound) {
			t.Errorf("hidden or missing device %d name update = %v, want not found", id, err)
		}
		if _, err := store.DeleteManagedDevice(ctx, actor, id, device.Revision); !errors.Is(err, identity.ErrDeviceNotFound) {
			t.Errorf("hidden or missing device %d delete = %v, want not found", id, err)
		}
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Fatal("invalid input or unknown device operations changed persisted state")
	}
	boundary := strings.Repeat("é", 128)
	updated, err := store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, boundary)
	if err != nil || updated.Name != boundary || updated.CustomName == nil || *updated.CustomName != boundary {
		t.Fatalf("valid 256-byte UTF-8 custom name was rejected or truncated: %v", err)
	}
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{SearchTerm: boundary})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ID != device.ID {
		t.Fatalf("valid 256-byte UTF-8 search was rejected or truncated: %v", err)
	}
}

func TestStoreDeviceRegistryIgnoresFailedAndUnidentifiedLogins(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	client := identity.Client{Name: "Rejected Player", DeviceID: "never-registered", Device: "Never Registered"}
	before := deviceManagementSnapshot(t, ctx, pool)
	for _, name := range []string{admin.Name, "Missing User"} {
		if _, err := store.AuthenticateWithPeer(ctx, name, "incorrect-password", client, "emby", "192.0.2.40"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Errorf("failed device login = %v, want invalid credentials", err)
		}
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Fatal("failed authentication created or touched a device or credential")
	}
	deviceLogin(t, ctx, store, admin, "administrator-password", client, "admin", "192.0.2.41")
	_, unidentified := deviceLogin(t, ctx, store, admin, "administrator-password", identity.Client{Name: "No Reported ID", Device: "Anonymous Device"}, "emby", "192.0.2.42")
	if err := store.TouchClientSessionFromAddress(ctx, unidentified, "192.0.2.43"); err != nil {
		t.Fatalf("unidentified ordinary login lost ordinary session activity: %v", err)
	}
	page, err := store.ListManagedDevices(ctx, actor, identity.ManagedDeviceFilter{})
	if err != nil || page.TotalRecordCount != 0 || len(page.Items) != 0 || page.Items == nil {
		t.Fatalf("native or unidentified login created a fake physical-device registration: %v", err)
	}
	var attached int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE device_registry_id IS NOT NULL").Scan(&attached); err != nil {
		t.Fatal(err)
	}
	if attached != 0 {
		t.Error("native or unidentified login acquired an ordinary device generation")
	}
}

func TestStoreEmbyDevicesRequireCurrentCompatibilityAuthority(t *testing.T) {
	ctx, pool, store, nativeAdmin, _ := applicationKeyTestStore(t)
	viewer, err := store.CreateUser(ctx, "Compatibility Device Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, viewerActor := deviceLogin(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "compatibility-device", Device: "Reported Compat Device"}, "emby", "192.0.2.50")
	_, embyAdmin := managedLogin(t, ctx, store, nativeAdmin.User, "administrator-password", "emby")
	key := issueApplicationKey(t, ctx, store, nativeAdmin, "Compatibility Device Manager")
	keyActor := applicationKeyPrincipal(t, ctx, store, key)
	device := onlyDevice(t, ctx, store, nativeAdmin)
	for _, principal := range []identity.Principal{embyAdmin, keyActor} {
		devices, err := store.ListEmbyDevices(ctx, principal)
		if err != nil {
			t.Fatalf("authorized compatibility device list: %v", err)
		}
		found := false
		for _, listed := range devices {
			if listed.ID == device.ID {
				found = true
				if listed.ReportedDeviceID != device.ReportedDeviceID || listed.ActiveLoginCount != 1 {
					t.Error("compatibility list lost the ordinary registry identity")
				}
			}
		}
		if !found {
			t.Error("server-management compatibility credential could not list an ordinary device")
		}
		for _, lookup := range []string{device.ReportedDeviceID, strconv.FormatInt(device.ID, 10)} {
			found, err := store.LookupEmbyDevice(ctx, principal, lookup)
			if err != nil || found.ID != device.ID {
				t.Errorf("compatibility device lookup lost its raw or numeric identity: %v", err)
			}
		}
	}
	for name, principal := range map[string]identity.Principal{"native-cookie": nativeAdmin, "ordinary-viewer": viewerActor, "anonymous": {}} {
		t.Run(name, func(t *testing.T) {
			want := identity.ErrUnauthorized
			if name == "ordinary-viewer" {
				want = identity.ErrClientSessionForbidden
			}
			before := deviceManagementSnapshot(t, ctx, pool)
			if _, err := store.ListEmbyDevices(ctx, principal); !errors.Is(err, want) {
				t.Errorf("invalid compatibility actor listed devices: %v", err)
			}
			if _, err := store.LookupEmbyDevice(ctx, principal, device.ReportedDeviceID); !errors.Is(err, want) {
				t.Errorf("invalid compatibility actor read a device: %v", err)
			}
			if _, err := store.UpdateEmbyDeviceOptions(ctx, principal, device.ReportedDeviceID, "Denied"); !errors.Is(err, want) {
				t.Errorf("invalid compatibility actor changed a device name: %v", err)
			}
			if _, err := store.DeleteEmbyDevice(ctx, principal, device.ReportedDeviceID); !errors.Is(err, want) {
				t.Errorf("invalid compatibility actor removed a device: %v", err)
			}
			if deviceManagementSnapshot(t, ctx, pool) != before {
				t.Error("rejected compatibility device administration changed persisted state")
			}
		})
	}
	renamed, err := store.UpdateEmbyDeviceOptions(ctx, embyAdmin, device.ReportedDeviceID, "Emby Administrator Name")
	if err != nil || renamed.Name != "Emby Administrator Name" || renamed.Revision != device.Revision+1 {
		t.Fatalf("Emby administrator could not edit the shared device override: %v", err)
	}
	renamed, err = store.UpdateEmbyDeviceOptions(ctx, keyActor, strconv.FormatInt(device.ID, 10), "Application Key Name")
	if err != nil || renamed.Name != "Application Key Name" || renamed.Revision != device.Revision+2 {
		t.Fatalf("application credential could not edit the shared device override: %v", err)
	}
	if current := readDevice(t, ctx, store, nativeAdmin, device.ID); !reflect.DeepEqual(current, renamed) {
		t.Errorf("native and compatibility device options diverged; fields=%v; created instant equal=%t locations=%s/%s; seen instant equal=%t locations=%s/%s",
			deviceProjectionDifferenceFields(current, renamed), current.CreatedAt.Equal(renamed.CreatedAt),
			current.CreatedAt.Location(), renamed.CreatedAt.Location(), current.LastSeenAt.Equal(renamed.LastSeenAt),
			current.LastSeenAt.Location(), renamed.LastSeenAt.Location())
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", nativeAdmin.User.ID); err != nil {
		t.Fatal(err)
	}
	before := deviceManagementSnapshot(t, ctx, pool)
	if _, err := store.ListEmbyDevices(ctx, embyAdmin); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Errorf("demoted administrator snapshot retained compatibility device visibility: %v", err)
	}
	if _, err := store.LookupEmbyDevice(ctx, embyAdmin, device.ReportedDeviceID); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Errorf("demoted administrator snapshot retained compatibility device read authority: %v", err)
	}
	if _, err := store.UpdateEmbyDeviceOptions(ctx, embyAdmin, device.ReportedDeviceID, "Demoted edit"); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Errorf("demoted administrator snapshot retained compatibility device edit authority: %v", err)
	}
	if _, err := store.DeleteEmbyDevice(ctx, embyAdmin, device.ReportedDeviceID); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Errorf("demoted administrator snapshot retained compatibility device deletion: %v", err)
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Error("demoted administrator changed device state through stale compatibility authority")
	}
	deleted, err := store.DeleteEmbyDevice(ctx, keyActor, strconv.FormatInt(device.ID, 10))
	if err != nil || deleted.ID != device.ID || deleted.RevokedLoginCount != 1 || !slices.Equal(deleted.RevokedSessionIDs, []string{credentials.SessionID}) {
		t.Fatalf("independent application authority could not remove an ordinary device after creator demotion: %v", err)
	}
	assertManagedTokenRevoked(t, ctx, store, credentials, "emby")
	if _, err := store.RevalidateSession(ctx, keyActor); err != nil {
		t.Errorf("ordinary device removal invalidated the application manager: %v", err)
	}
	if _, err := store.LookupEmbyDevice(ctx, keyActor, device.ReportedDeviceID); !errors.Is(err, identity.ErrDeviceNotFound) {
		t.Errorf("removed raw device lookup = %v, want not found", err)
	}
	if _, err := store.RevokeApplicationKey(ctx, keyActor, key.ID); err != nil {
		t.Fatal(err)
	}
	before = deviceManagementSnapshot(t, ctx, pool)
	if _, err := store.ListEmbyDevices(ctx, keyActor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("revoked application snapshot retained device visibility: %v", err)
	}
	if _, err := store.DeleteEmbyDevice(ctx, keyActor, strconv.FormatInt(device.ID, 10)); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("revoked application snapshot reused idempotent device deletion: %v", err)
	}
	if deviceManagementSnapshot(t, ctx, pool) != before {
		t.Error("revoked application manager changed device state")
	}
}

func TestStoreEmbyDeviceLookupKeepsNumericAndOpaqueIdentitiesDistinct(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, nativeAdmin := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "emby")
	deviceLogin(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: "OpaqueDevice", Device: "First Device"}, "emby", "")
	first := onlyDevice(t, ctx, store, nativeAdmin)
	numericID := strconv.FormatInt(first.ID, 10)
	for _, reportedID := range []string{numericID, "0" + numericID, "9007199254740991"} {
		deviceLogin(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: reportedID, Device: "Reported " + reportedID}, "emby", "")
	}
	page, err := store.ListManagedDevices(ctx, nativeAdmin, identity.ManagedDeviceFilter{})
	if err != nil || page.TotalRecordCount != 4 || len(page.Items) != 4 {
		t.Fatalf("numeric-looking reported identifiers did not retain separate registry generations: %v", err)
	}
	byReportedID := make(map[string]identity.ManagedDevice)
	for _, device := range page.Items {
		byReportedID[device.ReportedDeviceID] = device
	}
	if byReportedID[numericID].ID == first.ID {
		t.Fatal("reported identifier was conflated with an internal numeric registry ID during login")
	}
	for _, lookup := range []string{first.ReportedDeviceID, numericID} {
		found, err := store.LookupEmbyDevice(ctx, actor, lookup)
		if err != nil || found.ID != first.ID {
			t.Errorf("canonical numeric lookup did not select the registry ID before reported aliases: %v", err)
		}
	}
	leadingZero, err := store.LookupEmbyDevice(ctx, actor, "0"+numericID)
	if err != nil || leadingZero.ID != byReportedID["0"+numericID].ID {
		t.Fatalf("noncanonical numeric-looking reported ID was normalized into a different device: %v", err)
	}
	for _, lookup := range []string{"opaquedevice", "9007199254740991", "missing-device"} {
		if _, err := store.LookupEmbyDevice(ctx, actor, lookup); !errors.Is(err, identity.ErrDeviceNotFound) {
			t.Errorf("missing numeric identity or case-distinct opaque lookup %q = %v, want not found", lookup, err)
		}
	}
	if _, err := store.LookupEmbyDevice(ctx, actor, ""); !errors.Is(err, identity.ErrInvalidInput) {
		t.Errorf("empty compatibility lookup = %v, want invalid input", err)
	}
	renamed, err := store.UpdateEmbyDeviceOptions(ctx, actor, numericID, "First Numeric Target")
	if err != nil || renamed.ID != first.ID {
		t.Fatalf("numeric device edit selected the reported-ID collision: %v", err)
	}
	if other := readDevice(t, ctx, store, nativeAdmin, byReportedID[numericID].ID); other.CustomName != nil {
		t.Error("numeric device edit changed another generation's opaque reported-ID alias")
	}
}
