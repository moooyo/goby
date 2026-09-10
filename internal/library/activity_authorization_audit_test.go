package library

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestCatalogAuditRevocationDuringActorLockWaitRejectsEveryMutation(t *testing.T) {
	for _, operation := range []string{"create", "delete", "scan", "cancel", "metadata"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
			var library Library
			var jobID string
			var detail ItemMetadataDetail
			if operation == "metadata" {
				detail = catalogAuditMetadataFixture(t, ctx, pool, store, root, actor)
			} else if operation != "create" {
				library = libraryIntegrationCreate(t, ctx, store, "Waiting administrator", "movies", root)
			}
			if operation == "cancel" {
				jobID = catalogAuditSeedJob(t, ctx, pool, library.ID, "Queued", false)
			}
			before := catalogAuditSnapshot(t, ctx, pool)
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin administrator revocation gate: %v", err)
			}
			defer rollback(blocker)
			var blockerPID int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatalf("identify administrator revocation gate: %v", err)
			}
			if _, err := blocker.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", actor.User.ID); err != nil {
				t.Fatalf("hold administrator account before a catalog write: %v", err)
			}
			ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
			finished := make(chan error, 1)
			go func() {
				var result error
				switch operation {
				case "create":
					_, result = store.CreateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative,
						"Revoked registration", "movies", []string{root})
				case "delete":
					result = store.DeleteLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID)
				case "scan":
					_, result = store.StartScanAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, ScanOptions{})
				case "cancel":
					result = store.CancelJobAsAdministrator(ctx, actor, identity.AdministratorNative, jobID)
				case "metadata":
					_, result = store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{
						Revision: detail.Revision, Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Revoked edit"`)}, LockedFields: []string{}})
				}
				finished <- result
			}()
			ownedTransactionsWaitForBlock(t, ctx, pool, ownerPID, blockerPID, finished)
			if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
				t.Fatalf("revoke administrator while catalog admission waits: %v", err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatalf("release committed administrator revocation: %v", err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("catalog write retained revoked authority after a lock wait: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("catalog write did not finish after administrator revocation")
			}
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Error("revoked catalog write changed business state or committed activity")
			}
		})
	}
}
