package identity_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func TestApplicationKeyDeviceAuditRecordsTransitionsAndParentRevocationCount(t *testing.T) {
	for _, kind := range []string{"emby", "application_key"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store, native, _ := applicationKeyTestStore(t)
			_, admin := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
			first := issueApplicationKey(t, ctx, store, native, "Shared Audit First")
			second := issueApplicationKey(t, ctx, store, native, "Shared Audit Second")
			historical := issueApplicationKey(t, ctx, store, native, "Shared Audit Historical")
			if _, err := store.RevokeApplicationKey(ctx, native, historical.ID); err != nil {
				t.Fatal(err)
			}
			actor, actorKind, actorID := admin, "user", admin.User.ID
			if kind == "application_key" {
				actor = applicationKeyPrincipal(t, ctx, store, first)
				actorKind, actorID = "application_key", strconv.FormatInt(first.ID, 10)
			}
			id := strconv.FormatInt(first.ReportedDeviceNumericID, 10)
			directKeyAudit := applicationKeyAuditSnapshot(t, ctx, pool)
			if _, err := store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, " "); err != nil {
				t.Fatal(err)
			}
			if entries := readDeviceSessionAudit(t, ctx, pool, "device.updated", id); len(entries) != 0 {
				t.Fatal("an unchanged shared-device option created activity")
			}
			device, err := store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "Shared Audit Name")
			if err != nil {
				t.Fatal(err)
			}
			entries := readDeviceSessionAudit(t, ctx, pool, "device.updated", id)
			if len(entries) != 1 {
				t.Fatalf("shared-device rename activity count = %d, want one", len(entries))
			}
			assertDeviceSessionAudit(t, entries[0], deviceSessionAuditEntry{
				Action: "device.updated", Source: "emby", ActorKind: actorKind, ActorID: actorID,
				ActorCredentialID: actor.SessionID, ResourceKind: "device", ResourceID: id,
				Revision: device.Revision, AffectedCount: 1, ChangedFields: []string{"CustomName"},
			})
			if _, err := store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "Shared Audit Name"); err != nil {
				t.Fatal(err)
			}
			device, err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "")
			if err != nil || device.CustomName != nil {
				t.Fatalf("clear the audited shared-device name: %v", err)
			}
			entries = readDeviceSessionAudit(t, ctx, pool, "device.updated", id)
			if len(entries) != 2 || entries[1].Revision != device.Revision {
				t.Fatal("shared-device rename no-op or clearing produced incorrect activity")
			}
			deletion, err := store.DeleteApplicationKeyDevice(ctx, actor, id)
			if err != nil || deletion.RevokedLoginCount != 2 || len(deletion.RevokedSessionIDs) != 3 {
				t.Fatalf("audited shared-device removal lost its active and historical parent scopes: %v", err)
			}
			removed := readDeviceSessionAudit(t, ctx, pool, "device.removed", id)
			if len(removed) != 1 {
				t.Fatalf("shared-device removal activity count = %d, want one", len(removed))
			}
			assertDeviceSessionAudit(t, removed[0], deviceSessionAuditEntry{
				Action: "device.removed", Source: "emby", ActorKind: actorKind, ActorID: actorID,
				ActorCredentialID: actor.SessionID, ResourceKind: "device", ResourceID: id,
				Revision: device.Revision + 1, AffectedCount: 2,
			})
			for _, key := range []identity.ApplicationKey{first, second, historical} {
				if _, err := store.ResolveEmby(ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("audited removal left a parent credential usable: %v", err)
				}
			}
			retry, err := store.DeleteApplicationKeyDevice(ctx, admin, id)
			if err != nil || retry.RevokedLoginCount != 0 || !retry.DeletedAt.Equal(deletion.DeletedAt) {
				t.Fatalf("audited shared-device removal retry changed its committed fact: %v", err)
			}
			if len(readDeviceSessionAudit(t, ctx, pool, "device.removed", id)) != 1 || applicationKeyAuditSnapshot(t, ctx, pool) != directKeyAudit {
				t.Fatal("owning shared-device removal duplicated activity per key or on retry")
			}
		})
	}
}

func TestApplicationKeyDeviceAuditFailureRollsBackGenerationAndCredentials(t *testing.T) {
	for _, operation := range []string{"update", "remove"} {
		for _, kind := range []string{"emby", "application_key"} {
			for _, failure := range []string{"missing_table", "reject_trigger"} {
				t.Run(operation+"/"+kind+"/"+failure, func(t *testing.T) {
					ctx, pool, store, native, _ := applicationKeyTestStore(t)
					_, actor := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
					first := issueApplicationKey(t, ctx, store, native, "Shared Rollback First")
					second := issueApplicationKey(t, ctx, store, native, "Shared Rollback Second")
					if kind == "application_key" {
						actor = applicationKeyPrincipal(t, ctx, store, first)
					}
					id := strconv.FormatInt(first.ReportedDeviceNumericID, 10)
					before := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
					table, code := installDeviceSessionAuditFailure(t, ctx, pool, failure)
					auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, table)
					var result deviceSessionAuditMutationResult
					if operation == "update" {
						result.device, result.err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "Must Roll Back")
					} else {
						result.deletion, result.err = store.DeleteApplicationKeyDevice(ctx, actor, id)
					}
					var databaseError *pgconn.PgError
					if !errors.As(result.err, &databaseError) || databaseError.Code != code || result.hasBusinessResult() {
						t.Fatalf("shared-device mutation did not propagate audit failure with an empty result: %v", result.err)
					}
					if applicationKeyAuditBusinessSnapshot(t, ctx, pool) != before || deviceSessionAuditSnapshot(t, ctx, pool, table) != auditBefore {
						t.Fatal("failed shared-device audit committed device, key, client, credential or audit changes")
					}
					applicationKeyPrincipal(t, ctx, store, first)
					applicationKeyPrincipal(t, ctx, store, second)
				})
			}
		}
	}
}

func TestApplicationKeyDeviceAuditNoOpsDoNotRequireAnActivityInsert(t *testing.T) {
	for _, operation := range []string{"update", "remove_retry"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, store, native, _ := applicationKeyTestStore(t)
			_, actor := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
			key := issueApplicationKey(t, ctx, store, native, "Shared No-op")
			id := strconv.FormatInt(key.ReportedDeviceNumericID, 10)
			if operation == "remove_retry" {
				if _, err := store.DeleteApplicationKeyDevice(ctx, actor, id); err != nil {
					t.Fatal(err)
				}
			}
			before := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
			installDeviceSessionAuditFailure(t, ctx, pool, "reject_trigger")
			auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, "activity_entries")
			var err error
			if operation == "update" {
				_, err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "")
			} else {
				_, err = store.DeleteApplicationKeyDevice(ctx, actor, id)
			}
			if err != nil || applicationKeyAuditBusinessSnapshot(t, ctx, pool) != before || deviceSessionAuditSnapshot(t, ctx, pool, "activity_entries") != auditBefore {
				t.Fatalf("shared-device no-op attempted audit insertion or changed state: %v", err)
			}
		})
	}
}

func TestApplicationKeyDeviceAuditPrecedesFinalFreshAuthorization(t *testing.T) {
	for _, operation := range []string{"update", "remove"} {
		t.Run(operation, func(t *testing.T) {
			testCtx, pool, store, native, _ := applicationKeyTestStore(t)
			_, actor := managedLogin(t, testCtx, store, native.User, "administrator-password", "emby")
			key := issueApplicationKey(t, testCtx, store, native, "Shared Audit Expiry")
			id := strconv.FormatInt(key.ReportedDeviceNumericID, 10)
			action := "device.updated"
			if operation == "remove" {
				action = "device.removed"
			}
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseDeviceSessionAuditInsert(t, ctx, pool, action, id)
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
				t.Fatal(err)
			}
			before := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
			auditBefore := deviceSessionAuditSnapshot(t, ctx, pool, "activity_entries")
			results := make(chan deviceSessionAuditMutationResult, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				var result deviceSessionAuditMutationResult
				if operation == "update" {
					result.device, result.err = store.UpdateApplicationKeyDeviceOptions(ctx, actor, id, "Expired Audit Name")
				} else {
					result.deletion, result.err = store.DeleteApplicationKeyDevice(ctx, actor, id)
				}
				results <- result
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, actor.SessionID, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-results:
				if !errors.Is(result.err, identity.ErrUnauthorized) || result.hasBusinessResult() {
					t.Fatalf("shared-device audit outlived actor expiry: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("shared-device audit expiry mutation did not finish")
			}
			if applicationKeyAuditBusinessSnapshot(t, testCtx, pool) != before || deviceSessionAuditSnapshot(t, testCtx, pool, "activity_entries") != auditBefore {
				t.Fatal("expired shared-device actor committed business or activity changes")
			}
			var count int
			if err := pool.QueryRow(testCtx, "SELECT insert_count FROM device_session_audit_gate").Scan(&count); err != nil || count != 0 {
				t.Fatalf("expired shared-device actor committed the audit insertion side effect: %v", err)
			}
		})
	}
}
