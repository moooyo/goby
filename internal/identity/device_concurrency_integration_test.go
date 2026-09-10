package identity_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type concurrentDeviceRemoval struct {
	id         int64
	count      int64
	sessionIDs []string
	err        error
}

type concurrentDeviceLogin struct {
	credentials identity.Credentials
	err         error
}

type concurrentDeviceRename struct {
	device identity.ManagedDevice
	err    error
}

func startDeviceRemoval(ctx context.Context, store *identity.Store, actor identity.Principal, device identity.ManagedDevice) (<-chan concurrentDeviceRemoval, <-chan struct{}) {
	results := make(chan concurrentDeviceRemoval, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err := store.DeleteManagedDevice(ctx, actor, device.ID, device.Revision)
		results <- concurrentDeviceRemoval{
			id: result.ID, count: int64(result.RevokedLoginCount), sessionIDs: result.RevokedSessionIDs, err: err,
		}
	}()
	return results, done
}

func startDeviceLogin(ctx context.Context, store *identity.Store, user identity.User, password string, client identity.Client) (<-chan concurrentDeviceLogin, <-chan struct{}) {
	results := make(chan concurrentDeviceLogin, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		credentials, err := store.AuthenticateWithPeer(ctx, user.Name, password, client, "emby", "192.0.2.101")
		results <- concurrentDeviceLogin{credentials: credentials, err: err}
	}()
	return results, done
}

func startDeviceRename(ctx context.Context, store *identity.Store, actor identity.Principal, device identity.ManagedDevice, name string) (<-chan concurrentDeviceRename, <-chan struct{}) {
	results := make(chan concurrentDeviceRename, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		updated, err := store.UpdateManagedDeviceOptions(ctx, actor, device.ID, device.Revision, name)
		results <- concurrentDeviceRename{device: updated, err: err}
	}()
	return results, done
}

func awaitDeviceOperation[T any](t *testing.T, ctx context.Context, results <-chan T) T {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("concurrent device operation did not complete before its deadline")
		var zero T
		return zero
	}
}

func registeredDeviceForSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, "SELECT device_registry_id FROM sessions WHERE id = $1", sessionID).Scan(&id); err != nil {
		t.Fatalf("read login device generation: %v", err)
	}
	return id
}

func assertConcurrentDeviceRemoval(t *testing.T, result concurrentDeviceRemoval, deviceID int64, sessionIDs ...string) {
	t.Helper()
	if result.err != nil || result.id != deviceID || result.count != int64(len(sessionIDs)) {
		t.Fatalf("concurrent removal returned the wrong generation or revocation count: %v", result.err)
	}
	slices.Sort(sessionIDs)
	slices.Sort(result.sessionIDs)
	if !slices.Equal(result.sessionIDs, sessionIDs) {
		t.Error("concurrent removal revoked a different set of login identities")
	}
}

// Pause a real login after registry binding and before credential insertion.
// Observing its advisory lock wait establishes that later operations overlap
// this transaction without relying on password hashing or scheduling delays.
func pauseDeviceCredentialInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, reportedID string) (pgx.Tx, int32) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE device_login_gate (
		reported_id text PRIMARY KEY,
		lock_class integer NOT NULL,
		lock_key integer NOT NULL
	);
	CREATE FUNCTION pause_device_credential_insert() RETURNS trigger LANGUAGE plpgsql AS $function$
	DECLARE
		gate device_login_gate%ROWTYPE;
	BEGIN
		SELECT * INTO gate FROM device_login_gate WHERE reported_id = NEW.device_id;
		IF FOUND AND NEW.kind = 'emby' THEN
			PERFORM pg_advisory_xact_lock(gate.lock_class, gate.lock_key);
		END IF;
		RETURN NEW;
	END;
	$function$;
	CREATE TRIGGER device_credential_insert_barrier BEFORE INSERT ON sessions
		FOR EACH ROW EXECUTE FUNCTION pause_device_credential_insert()`); err != nil {
		t.Fatalf("install device credential insertion barrier: %v", err)
	}
	blocker, pid := managedSessionBlocker(t, ctx, pool)
	const lockClass int32 = 1196446301
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, $2::integer)", lockClass, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO device_login_gate (reported_id, lock_class, lock_key) VALUES ($1, $2, $3)", reportedID, lockClass, pid); err != nil {
		t.Fatal(err)
	}
	return blocker, pid
}

func TestStoreDeviceRemovalSerializesWithSharedReportedIDLogin(t *testing.T) {
	for _, order := range []string{"login-before-removal", "removal-before-login"} {
		t.Run(order, func(t *testing.T) {
			testCtx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, testCtx, store)
			_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
			owner, err := store.CreateUser(testCtx, "Existing Device Owner", "owner-password", false)
			if err != nil {
				t.Fatal(err)
			}
			// A different account prevents existing device-owner locks from
			// accidentally protecting a credential that removal has not seen.
			newOwner, err := store.CreateUser(testCtx, "Concurrent Device Owner", "new-owner-password", false)
			if err != nil {
				t.Fatal(err)
			}
			client := identity.Client{Name: "Concurrent Player", DeviceID: "shared-concurrent-device", Device: "Shared Television", Version: "1.0"}
			originalLogin, _ := deviceLogin(t, testCtx, store, owner, "owner-password", client, "emby", "192.0.2.100")
			original := onlyDevice(t, testCtx, store, actor)
			ctx, cancel := context.WithTimeout(testCtx, 30*time.Second)
			defer cancel()
			var blocker pgx.Tx
			var blockerPID int32
			var logins <-chan concurrentDeviceLogin
			var removals <-chan concurrentDeviceRemoval
			if order == "login-before-removal" {
				blocker, blockerPID = pauseDeviceCredentialInsert(t, ctx, pool, client.DeviceID)
				var loginDone <-chan struct{}
				logins, loginDone = startDeviceLogin(ctx, store, newOwner, "new-owner-password", client)
				loginPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "", loginDone)
				var removalDone <-chan struct{}
				removals, removalDone = startDeviceRemoval(ctx, store, actor, original)
				waitManagedBlockedQuery(t, ctx, pool, loginPID, "", removalDone)
			} else {
				blocker, blockerPID = pauseManagedSessionUpdate(t, ctx, pool, originalLogin.SessionID)
				var removalDone <-chan struct{}
				removals, removalDone = startDeviceRemoval(ctx, store, actor, original)
				removalPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "", removalDone)
				var loginDone <-chan struct{}
				logins, loginDone = startDeviceLogin(ctx, store, newOwner, "new-owner-password", client)
				waitManagedBlockedQuery(t, ctx, pool, removalPID, "", loginDone)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			login := awaitDeviceOperation(t, ctx, logins)
			removal := awaitDeviceOperation(t, ctx, removals)
			if login.err != nil {
				t.Fatalf("serialized login did not issue a credential: %v", login.err)
			}
			queuedGeneration := registeredDeviceForSession(t, testCtx, pool, login.credentials.SessionID)
			if order == "login-before-removal" {
				// The deletion uses the revision read before login activity. That
				// activity must neither make it stale nor escape its revocation set.
				assertConcurrentDeviceRemoval(t, removal, original.ID, originalLogin.SessionID, login.credentials.SessionID)
				if queuedGeneration != original.ID {
					t.Error("login that preceded removal did not use the existing generation")
				}
				assertManagedTokenRevoked(t, testCtx, store, login.credentials, "emby")
			} else {
				assertConcurrentDeviceRemoval(t, removal, original.ID, originalLogin.SessionID)
				if queuedGeneration <= original.ID {
					t.Error("login that followed removal reused the retired generation")
				}
				if _, err := store.Resolve(testCtx, login.credentials.Token, "emby"); err != nil {
					t.Errorf("login registered after removal did not retain access: %v", err)
				}
			}
			assertManagedTokenRevoked(t, testCtx, store, originalLogin, "emby")
			var escaped int64
			if err := pool.QueryRow(testCtx, `SELECT count(*) FROM sessions
				WHERE device_registry_id = $1 AND revoked_at IS NULL`, original.ID).Scan(&escaped); err != nil {
				t.Fatal(err)
			}
			if escaped != 0 {
				t.Error("removed device generation retained an unrevoked credential")
			}
			later, _ := deviceLogin(t, testCtx, store, newOwner, "new-owner-password", client, "emby", "192.0.2.102")
			laterGeneration := registeredDeviceForSession(t, testCtx, pool, later.SessionID)
			if laterGeneration <= original.ID || (order == "removal-before-login" && laterGeneration != queuedGeneration) {
				t.Error("later login did not join the fresh device generation")
			}
			current := onlyDevice(t, testCtx, store, actor)
			wantActive := int64(1)
			if order == "removal-before-login" {
				wantActive = 2
			}
			if current.ID != laterGeneration || current.Revision != 1 || int64(current.ActiveLoginCount) != wantActive {
				t.Error("fresh generation reused the removed revision or counted historical logins")
			}
		})
	}
}

func TestStoreDifferentDevicesConcurrentRemovalPreservesUnrelatedState(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	firstAdmin := bootstrapTestAdmin(t, testCtx, store)
	secondAdmin, err := store.CreateUser(testCtx, "Second Device Administrator", "second-password", true)
	if err != nil {
		t.Fatal(err)
	}
	firstCookie, firstActor := managedLogin(t, testCtx, store, firstAdmin, "administrator-password", "admin")
	secondCookie, secondActor := managedLogin(t, testCtx, store, secondAdmin, "second-password", "admin")
	clients := []identity.Client{
		{Name: "First Shared Player", DeviceID: "first-shared-device", Device: "First Screen"},
		{Name: "Second Shared Player", DeviceID: "second-shared-device", Device: "Second Screen"},
	}
	var devices [2]identity.ManagedDevice
	var logins [2][]identity.Credentials
	var expectedRevoked []string
	for index, client := range clients {
		first, _ := deviceLogin(t, testCtx, store, firstAdmin, "administrator-password", client, "emby", "192.0.2.110")
		second, _ := deviceLogin(t, testCtx, store, secondAdmin, "second-password", client, "emby", "192.0.2.111")
		logins[index] = []identity.Credentials{first, second}
		devices[index] = readDevice(t, testCtx, store, firstActor, registeredDeviceForSession(t, testCtx, pool, first.SessionID))
		expectedRevoked = append(expectedRevoked, first.SessionID, second.SessionID)
	}
	keptLogin, _ := deviceLogin(t, testCtx, store, firstAdmin, "administrator-password",
		identity.Client{Name: "Unrelated Player", DeviceID: "unrelated-device", Device: "Retained Screen"}, "emby", "192.0.2.112")
	kept := readDevice(t, testCtx, store, firstActor, registeredDeviceForSession(t, testCtx, pool, keptLogin.SessionID))
	before := deviceRetainedHistory(t, testCtx, pool, expectedRevoked)
	ctx, cancel := context.WithTimeout(testCtx, 30*time.Second)
	defer cancel()
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", firstActor.SessionID); err != nil {
		t.Fatal(err)
	}
	firstResults, firstDone := startDeviceRemoval(ctx, store, firstActor, devices[0])
	firstPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "", firstDone)
	secondResults, secondDone := startDeviceRemoval(ctx, store, secondActor, devices[1])
	// Both devices contain sessions from both administrators. The second
	// removal must queue without retaining locks that the first still needs.
	waitManagedBlockedQuery(t, ctx, pool, firstPID, "", secondDone)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	firstResult := awaitDeviceOperation(t, ctx, firstResults)
	secondResult := awaitDeviceOperation(t, ctx, secondResults)
	assertConcurrentDeviceRemoval(t, firstResult, devices[0].ID, logins[0][0].SessionID, logins[0][1].SessionID)
	assertConcurrentDeviceRemoval(t, secondResult, devices[1].ID, logins[1][0].SessionID, logins[1][1].SessionID)
	if after := deviceRetainedHistory(t, testCtx, pool, expectedRevoked); after != before {
		t.Error("concurrent device removals changed unrelated credentials, accounts, or retained history")
	}
	if current := onlyDevice(t, testCtx, store, firstActor); !reflect.DeepEqual(current, kept) {
		t.Error("concurrent device removals changed or removed the unrelated device")
	}
	for _, credentials := range append(logins[0], logins[1]...) {
		assertManagedTokenRevoked(t, testCtx, store, credentials, "emby")
	}
	for _, retained := range []struct {
		credentials identity.Credentials
		kind        string
	}{{firstCookie, "admin"}, {secondCookie, "admin"}, {keptLogin, "emby"}} {
		if _, err := store.Resolve(testCtx, retained.credentials.Token, retained.kind); err != nil {
			t.Errorf("concurrent device removals revoked an unrelated %s login: %v", retained.kind, err)
		}
	}
}

func TestStoreConcurrentDeviceNamesCommitOneRevisionAcrossActivity(t *testing.T) {
	testCtx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, testCtx, store)
	_, actor := managedLogin(t, testCtx, store, admin, "administrator-password", "admin")
	viewer, err := store.CreateUser(testCtx, "Concurrent Rename Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	client := identity.Client{Name: "Rename Player", DeviceID: "rename-race-device", Device: "Reported Screen"}
	deviceLogin(t, testCtx, store, viewer, "viewer-password", client, "emby", "192.0.2.120")
	device := onlyDevice(t, testCtx, store, actor)
	ctx, cancel := context.WithTimeout(testCtx, 30*time.Second)
	defer cancel()
	client.Name, client.Device, client.Version = "Updated Rename Player", "Updated Reported Screen", "2.0"
	blocker, blockerPID := pauseDeviceCredentialInsert(t, ctx, pool, client.DeviceID)
	loginResults, loginDone := startDeviceLogin(ctx, store, viewer, "viewer-password", client)
	loginPID := waitManagedBlockedQuery(t, ctx, pool, blockerPID, "", loginDone)
	// Both edits carry the revision from before concurrent login activity.
	// The first waits for that activity; the second waits for the first edit.
	firstResults, firstDone := startDeviceRename(ctx, store, actor, device, "First Override")
	firstPID := waitManagedBlockedQuery(t, ctx, pool, loginPID, "", firstDone)
	secondResults, secondDone := startDeviceRename(ctx, store, actor, device, "Second Override")
	waitManagedBlockedQuery(t, ctx, pool, firstPID, "", secondDone)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	login := awaitDeviceOperation(t, ctx, loginResults)
	first := awaitDeviceOperation(t, ctx, firstResults)
	second := awaitDeviceOperation(t, ctx, secondResults)
	if login.err != nil {
		t.Fatalf("concurrent login activity failed: %v", login.err)
	}
	if first.err != nil || first.device.Revision != device.Revision+1 || first.device.Name != "First Override" {
		t.Fatalf("first concurrent name edit did not commit one revision: %v", first.err)
	}
	if !errors.Is(second.err, identity.ErrDeviceRevisionConflict) {
		t.Errorf("second concurrent name edit did not reject its stale revision: %v", second.err)
	}
	current := onlyDevice(t, testCtx, store, actor)
	if !reflect.DeepEqual(current, first.device) {
		t.Error("rejected concurrent name edit changed the committed device")
	}
	if current.ID != device.ID || current.Revision != first.device.Revision || current.Name != "First Override" ||
		current.CustomName == nil || *current.CustomName != "First Override" || current.ReportedName != client.Device ||
		current.AppName != client.Name || current.AppVersion != client.Version || current.IPAddress != "192.0.2.101" || current.ActiveLoginCount != 2 {
		t.Error("activity changed the winning administrator revision or lost its device override")
	}
	if _, err := store.Resolve(testCtx, login.credentials.Token, "emby"); err != nil {
		t.Errorf("concurrent administrator name edits revoked login activity: %v", err)
	}
}
