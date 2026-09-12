//go:build linux

package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

type rootBindingWriteTestCapture struct {
	snapshot      RootTopologySnapshot
	snapshotHook  func()
	snapshotErr   error
	cloneErr      error
	revalidate    func(context.Context) error
	snapshots     int
	clones        int
	revalidations int
	closes        int
}

func (capture *rootBindingWriteTestCapture) Snapshot() (RootTopologySnapshot, error) {
	capture.snapshots++
	if capture.snapshots == 1 && capture.snapshotHook != nil {
		capture.snapshotHook()
	}
	return cloneRootTopologySnapshot(capture.snapshot), capture.snapshotErr
}

func (capture *rootBindingWriteTestCapture) Revalidate(ctx context.Context) error {
	capture.revalidations++
	if capture.revalidate != nil {
		return capture.revalidate(ctx)
	}
	return ctx.Err()
}

func (capture *rootBindingWriteTestCapture) CloneApprovedAnchor() (*os.Root, error) {
	capture.clones++
	if capture.cloneErr != nil {
		return nil, capture.cloneErr
	}
	return os.OpenRoot(capture.snapshot.Mapping.ApprovedPath)
}

func (capture *rootBindingWriteTestCapture) Close() error {
	capture.closes++
	return nil
}

func rootBindingWriteTestInput(t *testing.T, fixture rootBindingReadFixture, revision string) RootBindingUpdate {
	t.Helper()
	fingerprint, err := fixture.snapshot.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint root binding write fixture: %v", err)
	}
	return RootBindingUpdate{Revision: revision, ObservedFingerprint: fingerprint, AcknowledgeMissingRemoval: true}
}

func rootBindingWriteTestFactory(t *testing.T, fixture rootBindingReadFixture, capture *rootBindingWriteTestCapture) rootBindingCaptureFactory {
	t.Helper()
	return func(_ context.Context, root libraryRoot) (rootBindingWriteCapture, error) {
		if root.id != fixture.root.RootID || root.libraryID != fixture.library.ID ||
			root.path != fixture.root.Path || root.allowedPath != fixture.root.AllowedPath {
			t.Fatalf("capture received a different registered root: %+v", root)
		}
		if !fixture.store.mu.TryLock() {
			t.Fatal("root binding capture ran while Store.mu was held")
		}
		fixture.store.mu.Unlock()
		return capture, nil
	}
}

func rootBindingWriteTestReject(t *testing.T, fixture rootBindingReadFixture, result RootBindingInfo, err, want error, before string) {
	t.Helper()
	if !errors.Is(err, want) || !reflect.DeepEqual(result, RootBindingInfo{}) {
		t.Fatalf("rejected root binding write: result = %+v, error = %v, want = %v", result, err, want)
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("rejected root binding write changed catalog, binding, scan, or activity records")
	}
}

func rootBindingWriteTestUnrelatedCatalog(t *testing.T, fixture rootBindingReadFixture) map[string]json.RawMessage {
	t.Helper()
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(catalogAuditSnapshot(t, fixture.ctx, fixture.pool)), &snapshot); err != nil {
		t.Fatalf("decode catalog snapshot: %v", err)
	}
	var roots []map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["roots"], &roots); err != nil {
		t.Fatalf("decode registered roots from catalog snapshot: %v", err)
	}
	for _, root := range roots {
		var id string
		if err := json.Unmarshal(root["id"], &id); err != nil {
			t.Fatalf("decode registered root identifier: %v", err)
		}
		if id == fixture.root.RootID {
			for _, field := range []string{"binding_revision", "storage_binding", "bound_at", "bound_by"} {
				delete(root, field)
			}
		}
	}
	var events []map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["activity"], &events); err != nil {
		t.Fatalf("decode activity from catalog snapshot: %v", err)
	}
	unrelatedEvents := make([]map[string]json.RawMessage, 0, len(events))
	for _, event := range events {
		var action, resourceID string
		if err := json.Unmarshal(event["action"], &action); err != nil {
			t.Fatalf("decode activity action: %v", err)
		}
		if err := json.Unmarshal(event["resource_id"], &resourceID); err != nil {
			t.Fatalf("decode activity resource: %v", err)
		}
		if action != string(activity.ActionLibraryRootBindingUpdated) || resourceID != fixture.root.RootID {
			unrelatedEvents = append(unrelatedEvents, event)
		}
	}
	snapshot["roots"] = metadataEditTestRaw(t, roots)
	snapshot["activity"] = metadataEditTestRaw(t, unrelatedEvents)
	return snapshot
}

func TestRootBindingWriteIntegrationRequiresCurrentNativeAdministrator(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	input := rootBindingWriteTestInput(t, fixture, fixture.root.Revision)
	for index, test := range []struct {
		name      string
		statement string
		userRow   bool
		change    func(*identity.Principal)
	}{
		{name: "Emby principal", change: func(actor *identity.Principal) { actor.Kind = "emby" }},
		{name: "application principal", change: func(actor *identity.Principal) {
			actor.Kind, actor.ApplicationKeyID, actor.ClientSessionID = identity.ApplicationKeyKind, 1, "application-client"
		}},
		{name: "different user", change: func(actor *identity.Principal) { actor.User.ID = fixture.actor.User.ID }},
		{name: "missing session", change: func(actor *identity.Principal) { actor.SessionID = "missing-native-session" }},
		{name: "revoked", statement: `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`},
		{name: "expired", statement: `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`},
		{name: "session kind changed", statement: `UPDATE sessions SET kind = 'emby' WHERE id = $1`},
		{name: "demoted", statement: `UPDATE users SET is_administrator = false WHERE id = $1`, userRow: true},
		{name: "disabled", statement: `UPDATE users SET is_disabled = true WHERE id = $1`, userRow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, fmt.Sprintf("root-binding-write-rejected-%d", index))
			if test.statement != "" {
				id := actor.SessionID
				if test.userRow {
					id = actor.User.ID
				}
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, id); err != nil {
					t.Fatalf("change current root binding authority: %v", err)
				}
			}
			if test.change != nil {
				test.change(&actor)
			}
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			captured := false
			result, err := fixture.store.updateRootBinding(fixture.ctx, actor, fixture.library.ID, fixture.root.RootID, input,
				func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
					captured = true
					return &rootBindingWriteTestCapture{snapshot: fixture.snapshot}, nil
				})
			rootBindingWriteTestReject(t, fixture, result, err, ErrForbidden, before)
			if captured {
				t.Fatal("invalid native administrator reached filesystem capture")
			}
		})
	}
}

func TestRootBindingWriteIntegrationRejectsInvalidInputAndStaleCAS(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	valid := rootBindingWriteTestInput(t, fixture, fixture.root.Revision)
	for _, test := range []struct {
		name   string
		change func(*RootBindingUpdate)
		want   error
	}{
		{"missing acknowledgement", func(input *RootBindingUpdate) { input.AcknowledgeMissingRemoval = false }, ErrInvalidInput},
		{"empty revision", func(input *RootBindingUpdate) { input.Revision = "" }, ErrInvalidInput},
		{"zero revision", func(input *RootBindingUpdate) { input.Revision = "0" }, ErrInvalidInput},
		{"leading zero", func(input *RootBindingUpdate) { input.Revision = "01" }, ErrInvalidInput},
		{"positive sign", func(input *RootBindingUpdate) { input.Revision = "+1" }, ErrInvalidInput},
		{"negative revision", func(input *RootBindingUpdate) { input.Revision = "-1" }, ErrInvalidInput},
		{"revision whitespace", func(input *RootBindingUpdate) { input.Revision = "1 " }, ErrInvalidInput},
		{"oversized revision", func(input *RootBindingUpdate) { input.Revision = "10000000000000000000" }, ErrInvalidInput},
		{"int64 overflow", func(input *RootBindingUpdate) { input.Revision = "9223372036854775808" }, ErrRootBindingConflict},
		{"stale revision", func(input *RootBindingUpdate) { input.Revision = "2" }, ErrRootBindingConflict},
		{"empty fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = "" }, ErrInvalidInput},
		{"uppercase fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("AB", 32) }, ErrInvalidInput},
		{"short fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("a", 63) }, ErrInvalidInput},
		{"fingerprint mismatch", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("0", 64) }, ErrRootBindingConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.change(&input)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				input, rootBindingWriteTestFactory(t, fixture, capture))
			rootBindingWriteTestReject(t, fixture, result, err, test.want, before)
			if errors.Is(test.want, ErrInvalidInput) && capture.snapshots != 0 {
				t.Fatal("invalid root binding input reached filesystem observation")
			}
			if capture.snapshots > 0 && capture.closes != 1 {
				t.Fatalf("rejected observation retained its capture: closes = %d", capture.closes)
			}
		})
	}
	fixture.bind(t, 9223372036854775807)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
	result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
		rootBindingWriteTestInput(t, fixture, "9223372036854775807"), rootBindingWriteTestFactory(t, fixture, capture))
	rootBindingWriteTestReject(t, fixture, result, err, ErrRootBindingConflict, before)
}

func TestRootBindingWriteIntegrationRechecksAuthorityAfterObservation(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
		userRow   bool
	}{
		{"revoked", `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, false},
		{"expired", `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`, false},
		{"session kind changed", `UPDATE sessions SET kind = 'emby' WHERE id = $1`, false},
		{"demoted", `UPDATE users SET is_administrator = false WHERE id = $1`, true},
		{"disabled", `UPDATE users SET is_disabled = true WHERE id = $1`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 7)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot, snapshotHook: func() {
				id := fixture.actor.SessionID
				if test.userRow {
					id = fixture.actor.User.ID
				}
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, id); err != nil {
					t.Fatalf("change authority during root binding observation: %v", err)
				}
			}}
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				rootBindingWriteTestInput(t, fixture, "7"), rootBindingWriteTestFactory(t, fixture, capture))
			rootBindingWriteTestReject(t, fixture, result, err, ErrForbidden, before)
			if capture.snapshots == 0 || capture.closes != 1 {
				t.Fatalf("authority race did not observe and release its capture: snapshots = %d, closes = %d", capture.snapshots, capture.closes)
			}
		})
	}
}

func TestRootBindingWriteIntegrationRejectsChangedRowsAfterObservation(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
		want      error
	}{
		{"revision", `UPDATE library_roots SET binding_revision = binding_revision + 1 WHERE id = $1`, ErrRootBindingConflict},
		{"mapping", `UPDATE library_roots SET path = path || '/changed', relative_path = 'changed' WHERE id = $1`, ErrRootBindingConflict},
		{"relative mapping", `UPDATE library_roots SET relative_path = 'changed' WHERE id = $1`, ErrRootBindingConflict},
		{"document", `UPDATE library_roots SET storage_binding = jsonb_set(storage_binding, '{registered_root,handle}', '"c3dhcHBlZC1oYW5kbGU="'::jsonb) WHERE id = $1`, ErrRootBindingConflict},
		{"binding time", `UPDATE library_roots SET bound_at = bound_at + interval '1 second' WHERE id = $1`, ErrRootBindingConflict},
		{"binding actor", `UPDATE library_roots SET bound_by = 'historical-other-administrator' WHERE id = $1`, ErrRootBindingConflict},
		{"unbound", `UPDATE library_roots SET storage_binding = NULL, bound_at = NULL, bound_by = NULL WHERE id = $1`, ErrRootBindingConflict},
		{"removed", `DELETE FROM library_roots WHERE id = $1`, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 9)
			var afterObservation string
			capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot, snapshotHook: func() {
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.root.RootID); err != nil {
					t.Fatalf("change registered root during observation: %v", err)
				}
				afterObservation = catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			}}
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				rootBindingWriteTestInput(t, fixture, "9"), rootBindingWriteTestFactory(t, fixture, capture))
			rootBindingWriteTestReject(t, fixture, result, err, test.want, afterObservation)
			if capture.closes != 1 {
				t.Fatalf("changed row retained its capture: closes = %d", capture.closes)
			}
		})
	}
}

func TestRootBindingWriteIntegrationRejectsUnavailableMappingsBeforeObservation(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
	}{
		{"relative mapping", `UPDATE library_roots SET relative_path = 'changed' WHERE id = $1`},
		{"outside mapping", `UPDATE library_roots SET path = '/root-binding-outside-approved', relative_path = '../outside' WHERE id = $1`},
		{"unknown document", `UPDATE library_roots SET storage_binding = '{"unknown":true}'::jsonb WHERE id = $1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 3)
			if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.root.RootID); err != nil {
				t.Fatalf("make stored root unavailable: %v", err)
			}
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			captured := false
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				rootBindingWriteTestInput(t, fixture, "3"), func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
					captured = true
					return &rootBindingWriteTestCapture{snapshot: fixture.snapshot}, nil
				})
			rootBindingWriteTestReject(t, fixture, result, err, ErrUnavailable, before)
			if captured {
				t.Fatal("unavailable persisted root reached filesystem capture")
			}
		})
	}
	fixture := newRootBindingReadFixture(t)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	for _, identifiers := range [][2]string{{"other-library", fixture.root.RootID}, {fixture.library.ID, "missing-root"}} {
		captured := false
		result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, identifiers[0], identifiers[1],
			rootBindingWriteTestInput(t, fixture, fixture.root.Revision), func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
				captured = true
				return &rootBindingWriteTestCapture{snapshot: fixture.snapshot}, nil
			})
		rootBindingWriteTestReject(t, fixture, result, err, ErrNotFound, before)
		if captured {
			t.Fatal("missing or incorrectly scoped root reached filesystem capture")
		}
	}
}

func TestRootBindingWriteIntegrationRejectsQueuedAndRunningScans(t *testing.T) {
	for _, status := range []string{"Queued", "Running"} {
		for _, duringObservation := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/during-observation=%t", status, duringObservation), func(t *testing.T) {
				fixture := newRootBindingReadFixture(t)
				fixture.bind(t, 5)
				var before string
				seed := func() {
					catalogAuditSeedJob(t, fixture.ctx, fixture.pool, fixture.library.ID, status, false)
					before = catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
				}
				capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
				if duringObservation {
					capture.snapshotHook = seed
				} else {
					seed()
				}
				result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
					rootBindingWriteTestInput(t, fixture, "5"), rootBindingWriteTestFactory(t, fixture, capture))
				rootBindingWriteTestReject(t, fixture, result, err, ErrBusy, before)
				if duringObservation && (capture.snapshots == 0 || capture.closes != 1) {
					t.Fatalf("scan race did not release the observed capture: snapshots = %d, closes = %d", capture.snapshots, capture.closes)
				}
			})
		}
	}
}

func TestRootBindingWriteIntegrationRollsBackAuditAndFinalAuthorityFailures(t *testing.T) {
	for _, expireActor := range []bool{false, true} {
		t.Run(fmt.Sprintf("expire-actor=%t", expireActor), func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 17)
			catalogAuditInstallTrigger(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, expireActor)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				rootBindingWriteTestInput(t, fixture, "17"), rootBindingWriteTestFactory(t, fixture, capture))
			if expireActor {
				rootBindingWriteTestReject(t, fixture, result, err, ErrForbidden, before)
				var valid bool
				if err := fixture.pool.QueryRow(fixture.ctx, `SELECT expires_at > clock_timestamp() FROM sessions WHERE id = $1`,
					fixture.actor.SessionID).Scan(&valid); err != nil || !valid {
					t.Fatalf("audit-trigger expiry escaped the rolled-back transaction: valid = %v, error = %v", valid, err)
				}
			} else {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || !reflect.DeepEqual(result, RootBindingInfo{}) {
					t.Fatalf("binding write did not reach audit rejection: result = %+v, error = %v", result, err)
				}
				if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
					t.Fatal("audit rejection partially committed a root binding")
				}
			}
			catalogAuditAssertTrigger(t, fixture.ctx, fixture.pool)
			if capture.closes != 1 {
				t.Fatalf("audit failure retained the filesystem capture: closes = %d", capture.closes)
			}
		})
	}
}

func TestRootBindingWriteIntegrationRejectsIncompleteOrChangedCaptures(t *testing.T) {
	privateFailure := errors.New("private storage observation failed")
	for _, test := range []struct {
		name          string
		factoryErr    error
		change        func(*rootBindingWriteTestCapture)
		want          error
		revalidations int
	}{
		{name: "capture failure", factoryErr: privateFailure, want: ErrUnavailable},
		{name: "snapshot failure", change: func(capture *rootBindingWriteTestCapture) {
			capture.snapshotErr = privateFailure
		}, want: ErrUnavailable},
		{name: "invalid identity", change: func(capture *rootBindingWriteTestCapture) {
			capture.snapshot.RegisteredRoot.Handle = nil
		}, want: ErrUnavailable},
		{name: "different mapping", change: func(capture *rootBindingWriteTestCapture) {
			capture.snapshot.Mapping.RegisteredPath += "/different-root"
		}, want: ErrRootBindingConflict},
		{name: "anchor clone failure", change: func(capture *rootBindingWriteTestCapture) {
			capture.cloneErr = privateFailure
		}, want: ErrUnavailable},
		{name: "first revalidation failure", change: func(capture *rootBindingWriteTestCapture) {
			capture.revalidate = func(context.Context) error { return privateFailure }
		}, want: ErrUnavailable, revalidations: 1},
		{name: "final revalidation failure", change: func(capture *rootBindingWriteTestCapture) {
			capture.revalidate = func(context.Context) error {
				if capture.revalidations == 2 {
					return privateFailure
				}
				return nil
			}
		}, want: ErrUnavailable, revalidations: 2},
		{name: "changed final observation", change: func(capture *rootBindingWriteTestCapture) {
			capture.revalidate = func(context.Context) error {
				if capture.revalidations == 2 {
					return ErrRootTopologyChanged
				}
				return nil
			}
		}, want: ErrRootBindingConflict, revalidations: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingReadFixture(t)
			fixture.bind(t, 13)
			input := rootBindingWriteTestInput(t, fixture, "13")
			capture := &rootBindingWriteTestCapture{snapshot: cloneRootTopologySnapshot(fixture.snapshot)}
			if test.change != nil {
				test.change(capture)
			}
			factory := rootBindingWriteTestFactory(t, fixture, capture)
			if test.factoryErr != nil {
				factory = func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
					return nil, test.factoryErr
				}
			}
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID, input, factory)
			rootBindingWriteTestReject(t, fixture, result, err, test.want, before)
			if test.factoryErr == nil && capture.closes != 1 {
				t.Fatalf("rejected observation retained its capture: closes = %d", capture.closes)
			}
			if capture.revalidations != test.revalidations {
				t.Fatalf("observation failed at the wrong transaction boundary: revalidations = %d, want %d", capture.revalidations, test.revalidations)
			}
		})
	}
}

func TestRootBindingWriteIntegrationRollsBackDeferredAuditCommitFailure(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 19)
	fixture.store.mu.Lock()
	anchorsBefore := len(fixture.store.rootBindingAnchors)
	anchorBefore, foundAnchorBefore := fixture.store.rootBindingAnchors[fixture.root.RootID]
	fixture.store.mu.Unlock()
	if !foundAnchorBefore || anchorBefore.approved == nil {
		t.Fatal("registered root did not retain its initial anchor")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE SEQUENCE root_binding_commit_failure_hits;
		CREATE FUNCTION reject_root_binding_commit_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.action = 'library.root_binding.updated' THEN
				PERFORM nextval('root_binding_commit_failure_hits');
				RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'Injected root binding commit failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE CONSTRAINT TRIGGER reject_root_binding_commit_for_test AFTER INSERT ON activity_entries
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_root_binding_commit_for_test()`); err != nil {
		t.Fatalf("install deferred root binding audit failure: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := fixture.pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS reject_root_binding_commit_for_test ON activity_entries;
			DROP FUNCTION IF EXISTS reject_root_binding_commit_for_test(); DROP SEQUENCE IF EXISTS root_binding_commit_failure_hits`); err != nil {
			t.Errorf("remove deferred root binding audit failure: %v", err)
		}
	})
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
	result, err := fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
		rootBindingWriteTestInput(t, fixture, "19"), rootBindingWriteTestFactory(t, fixture, capture))
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || !reflect.DeepEqual(result, RootBindingInfo{}) {
		t.Fatalf("binding write did not reach deferred audit rejection: result = %+v, error = %v", result, err)
	}
	var triggered bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT is_called FROM root_binding_commit_failure_hits`).Scan(&triggered); err != nil || !triggered {
		t.Fatalf("binding commit did not reach its deferred audit trigger: triggered = %v, error = %v", triggered, err)
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("deferred audit failure partially committed root binding state")
	}
	fixture.store.mu.Lock()
	anchorsAfter := len(fixture.store.rootBindingAnchors)
	anchorAfter, foundAnchorAfter := fixture.store.rootBindingAnchors[fixture.root.RootID]
	fixture.store.mu.Unlock()
	if anchorsAfter != anchorsBefore || !foundAnchorAfter || anchorAfter.approved != anchorBefore.approved || anchorAfter.root != anchorBefore.root {
		t.Fatal("deferred audit failure changed the retained anchor or its registered mapping")
	}
	if _, err := anchorBefore.approved.Stat("."); err != nil {
		t.Fatalf("deferred audit failure closed the original registered anchor: %v", err)
	}
	if capture.revalidations != 2 || capture.closes != 1 {
		t.Fatalf("deferred commit failure did not revalidate and release its capture: revalidations = %d, closes = %d", capture.revalidations, capture.closes)
	}
}

func TestRootBindingWriteIntegrationCommitsAfterCallerCancellationInsideTransaction(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 23)
	callerCtx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
	capture.revalidate = func(ctx context.Context) error {
		if capture.revalidations == 2 {
			cancel()
			if ctx.Err() != nil {
				t.Fatalf("transaction revalidation inherited caller cancellation: %v", ctx.Err())
			}
		}
		return ctx.Err()
	}
	input := rootBindingWriteTestInput(t, fixture, "23")
	result, err := fixture.store.updateRootBinding(callerCtx, fixture.actor, fixture.library.ID, fixture.root.RootID,
		input, rootBindingWriteTestFactory(t, fixture, capture))
	if err != nil || !errors.Is(callerCtx.Err(), context.Canceled) || result.Revision != "24" ||
		result.Status != RootBindingVerified || result.ApprovedFingerprint != input.ObservedFingerprint ||
		result.ObservedFingerprint != input.ObservedFingerprint || result.BoundAt == nil || result.BoundBy != fixture.actor.User.ID {
		t.Fatalf("caller cancellation changed a committed binding result: result = %+v, error = %v, caller error = %v", result, err, callerCtx.Err())
	}
	if capture.revalidations != 2 || capture.closes != 1 {
		t.Fatalf("cancellation did not happen at final held revalidation: revalidations = %d, closes = %d", capture.revalidations, capture.closes)
	}
	var revision int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT binding_revision FROM library_roots WHERE id = $1`,
		fixture.root.RootID).Scan(&revision); err != nil || revision != 24 {
		t.Fatalf("successful cancellation response did not correspond to a commit: revision = %d, error = %v", revision, err)
	}
	fact := catalogAuditOnlyFact(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, fixture.root.RootID)
	if fact.revision != revision {
		t.Fatalf("caller cancellation separated binding and audit commit: fact = %+v", fact)
	}
}

func TestRootBindingWriteIntegrationRechecksRevocationAfterAdministratorLockWait(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 29)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	blocker, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatalf("begin binding administrator lock gate: %v", err)
	}
	defer rollback(blocker)
	var blockerPID int32
	if err := blocker.QueryRow(fixture.ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("identify binding administrator lock gate: %v", err)
	}
	if _, err := blocker.Exec(fixture.ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, fixture.actor.User.ID); err != nil {
		t.Fatalf("hold binding administrator account: %v", err)
	}
	ownerPID := int32(fixture.store.ownership.conn.Conn().PgConn().PID())
	capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
	input := rootBindingWriteTestInput(t, fixture, "29")
	finished := make(chan error, 1)
	var result RootBindingInfo
	go func() {
		var updateErr error
		result, updateErr = fixture.store.updateRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
			input, func(context.Context, libraryRoot) (rootBindingWriteCapture, error) { return capture, nil })
		finished <- updateErr
	}()
	ownedTransactionsWaitForBlock(t, fixture.ctx, fixture.pool, ownerPID, blockerPID, finished)
	if _, err := blocker.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, fixture.actor.SessionID); err != nil {
		t.Fatalf("revoke binding administrator during admission lock wait: %v", err)
	}
	if err := blocker.Commit(fixture.ctx); err != nil {
		t.Fatalf("commit binding administrator revocation: %v", err)
	}
	select {
	case err := <-finished:
		rootBindingWriteTestReject(t, fixture, result, err, ErrForbidden, before)
	case <-fixture.ctx.Done():
		t.Fatal("binding write did not finish after the administrator gate released")
	}
	if capture.snapshots == 0 || capture.closes != 1 {
		t.Fatalf("authorization lock rejection retained the held observation: snapshots = %d, closes = %d", capture.snapshots, capture.closes)
	}
}

func TestRootBindingWriteIntegrationPersistsExactDocumentAndAuditProjection(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	const initialRevision int64 = 9007199254740992
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision = $1,
		storage_binding = NULL, bound_at = NULL, bound_by = NULL WHERE id = $2`,
		initialRevision, fixture.root.RootID); err != nil {
		t.Fatalf("seed an exact unbound revision: %v", err)
	}
	before := rootBindingWriteTestUnrelatedCatalog(t, fixture)
	actor := fixture.actor
	actor.User.IsAdministrator, actor.User.IsDisabled, actor.ExpiresAt = false, true, time.Unix(1, 0)
	for step := int64(0); step < 2; step++ {
		previousRevision := initialRevision + step
		if step == 1 {
			fixture.snapshot.RegisteredRoot.Handle = []byte("private-rebound-directory-handle")
			fixture.snapshot.Boundaries = []RootTopologyBoundary{{RelativePath: "nested", Identity: fixture.snapshot.RegisteredRoot}}
		}
		input := rootBindingWriteTestInput(t, fixture, strconv.FormatInt(previousRevision, 10))
		capture := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
		result, err := fixture.store.updateRootBinding(fixture.ctx, actor, fixture.library.ID, fixture.root.RootID,
			input, rootBindingWriteTestFactory(t, fixture, capture))
		if err != nil {
			t.Fatalf("commit root binding transition %d: %v", step, err)
		}
		wantRoot := fixture.root
		wantRoot.Revision = strconv.FormatInt(previousRevision+1, 10)
		wantTopology, wantFingerprint, err := rootBindingTopologyInfo(fixture.snapshot)
		if err != nil {
			t.Fatalf("project expected binding observation: %v", err)
		}
		if result.RegisteredRootInfo != wantRoot || result.Status != RootBindingVerified ||
			result.ApprovedFingerprint != wantFingerprint || result.ObservedFingerprint != wantFingerprint ||
			!reflect.DeepEqual(result.Approved, wantTopology) || !reflect.DeepEqual(result.Observed, wantTopology) ||
			result.BoundAt == nil || result.BoundBy != fixture.actor.User.ID {
			t.Fatalf("committed binding projection lost exact revision or observed identities: %+v", result)
		}
		if capture.snapshots == 0 || capture.clones != 1 || capture.revalidations != 2 || capture.closes != 1 {
			t.Fatalf("commit did not observe, retain, revalidate, and release the held capture: snapshots = %d, clones = %d, revalidations = %d, closes = %d",
				capture.snapshots, capture.clones, capture.revalidations, capture.closes)
		}
		var persistedRevision int64
		var document []byte
		var boundAt time.Time
		var boundBy string
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT binding_revision, storage_binding::text, bound_at, bound_by
			FROM library_roots WHERE library_id = $1 AND id = $2`, fixture.library.ID, fixture.root.RootID).
			Scan(&persistedRevision, &document, &boundAt, &boundBy); err != nil {
			t.Fatalf("read committed root binding document: %v", err)
		}
		persisted, err := storagebinding.DecodeSnapshot(document)
		if err != nil || !reflect.DeepEqual(persisted, fixture.snapshot) || persistedRevision != previousRevision+1 ||
			!boundAt.Equal(*result.BoundAt) || boundBy != fixture.actor.User.ID {
			t.Fatalf("binding document and projection disagree: revision = %d, snapshot = %+v, error = %v", persistedRevision, persisted, err)
		}
		facts := catalogAuditFacts(t, fixture.ctx, fixture.pool, activity.ActionLibraryRootBindingUpdated, fixture.root.RootID)
		if len(facts) != int(step+1) {
			t.Fatalf("binding transition recorded an unexpected number of activity facts: %d", len(facts))
		}
		fact := facts[len(facts)-1]
		catalogAuditAssertActor(t, fact, fixture.actor, activity.SourceNative, activity.ResourceLibraryRoot)
		if fact.revision != previousRevision+1 || fact.state != "" || len(fact.fields) != 0 {
			t.Fatalf("binding activity changed unrelated audit fields: %+v", fact)
		}
		var audit struct {
			PreviousRevision       int64  `json:"previous_revision"`
			Revision               int64  `json:"revision"`
			ObservationFingerprint string `json:"observation_fingerprint"`
			AffectedCount          int64  `json:"affected_count"`
		}
		if err := json.Unmarshal([]byte(fact.raw), &audit); err != nil || audit.PreviousRevision != previousRevision ||
			audit.Revision != previousRevision+1 || audit.ObservationFingerprint != wantFingerprint || audit.AffectedCount != 0 {
			t.Fatalf("binding activity lost exact transition facts: audit = %+v, error = %v", audit, err)
		}
		for _, privateValue := range []string{fixture.root.Path, string(fixture.snapshot.RegisteredRoot.Handle),
			base64.StdEncoding.EncodeToString(fixture.snapshot.RegisteredRoot.Handle), "storage_binding", "handle_type", "filesystem_uuid"} {
			if strings.Contains(fact.raw, privateValue) {
				t.Fatalf("binding activity retained private storage data: %q", privateValue)
			}
		}
		if after := rootBindingWriteTestUnrelatedCatalog(t, fixture); !reflect.DeepEqual(after, before) {
			t.Fatal("root binding approval changed libraries, media, metadata, entities, or scans")
		}
	}
}
