package library

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestScanStagingKnownExpiryPreservesRollbackAndDiscardRules(t *testing.T) {
	for _, test := range []struct {
		name          string
		ddl           string
		cleanup       bool
		budget        bool
		auditFailure  bool
		failedCleanup bool
	}{
		{name: "ordinary"},
		{name: "create", ddl: "create"},
		{name: "drop", ddl: "drop"},
		{name: "cleanup", cleanup: true},
		{name: "physical_budget", budget: true},
		{name: "physical_audit_failure", auditFailure: true},
		{name: "unknown_ddl_rollback", ddl: "create", failedCleanup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Exercise the real 20-second protected deadline without adding a
			// production timeout hook. Each case owns an independent schema.
			t.Parallel()
			ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
			ownerConn := store.ownership.conn
			ownerPID := int32(ownerConn.Conn().PgConn().PID())
			var stage *scanReconciliationStaging
			if test.ddl != "create" {
				stage = scanStagingTestBegin(t, ctx, store, "completion-scan", "completion-library")
			}
			if test.auditFailure {
				if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
					_, err := tx.Exec(`DROP TABLE ` + scanReconciliationSeenTable)
					return err
				}); err != nil {
					t.Fatalf("prepare the physical audit failure: %v", err)
				}
			}
			expired, published := false, false
			err := store.withScanStagingTx(ctx, test.cleanup, func(transaction *scanStagingTx) (func(), error) {
				if _, err := transaction.tx.Exec(transaction.ctx,
					`INSERT INTO server_settings(key,value) VALUES('scan-staging-expired-completion','must roll back')`); err != nil {
					return nil, err
				}
				switch test.ddl {
				case "create":
					if err := transaction.create(); err != nil {
						return nil, err
					}
				case "drop":
					if err := transaction.drop(); err != nil {
						return nil, err
					}
				default:
					if _, err := transaction.tx.Exec(transaction.ctx, `UPDATE `+scanReconciliationPassTable+`
						SET sealed=true WHERE generation=$1 AND scan_id=$2 AND library_id=$3`, stage.generation, stage.scanID, stage.libraryID); err != nil {
						return nil, err
					}
				}
				if test.budget {
					// Only the deferred physical audit observes this lower ceiling.
					// No operation has set discard or checked physicalBytes yet.
					stage.limits.physicalBytes = 1
				}
				select {
				case <-transaction.ctx.Done():
					expired = true
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if test.failedCleanup {
					ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
				}
				return func() {
					published = true
					if stage != nil {
						stage.sealed.Store(true)
					}
				}, nil
			})
			if !expired || !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || published || errors.Is(err, pgx.ErrTxCommitRollback) {
				t.Fatalf("known staging expiration lost its boundary or published state: expired=%t published=%t caller=%v error=%v",
					expired, published, ctx.Err(), err)
			}
			if stage != nil && stage.sealed.Load() {
				t.Fatal("an expired transaction published staging seal bookkeeping")
			}
			ownedTransactionsExpectAbsent(t, ctx, pool, "scan-staging-expired-completion")
			if errors.Is(err, errScanReconciliationStagingBudget) != test.budget {
				t.Fatalf("known expiration skipped or invented the physical capacity audit: %v", err)
			}
			if test.auditFailure {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "42P01" {
					t.Fatalf("known expiration hid the post-rollback relation audit failure: %v", err)
				}
			}
			wantDiscard := test.cleanup || test.budget || test.auditFailure || test.failedCleanup
			if errors.Is(err, ErrUnavailable) != wantDiscard {
				t.Fatalf("staging changed its discard classification: discard=%t error=%v", wantDiscard, err)
			}
			if wantDiscard {
				if store.Available() || !store.ownership.lost.Load() || store.ownership.conn != nil || store.ctx.Err() == nil {
					t.Fatal("failed cleanup or physical auditing left a reusable staging session")
				}
				return
			}
			if store.ownership.conn != ownerConn || store.ctx.Err() != nil || !store.ownership.scanStagingDirty {
				t.Fatal("confirmed staging rollback replaced or canceled the reserved owner")
			}
			if test.ddl == "create" {
				if store.ownership.scanStagingReady || len(store.ownership.scanStagingPasses) != 0 {
					t.Fatal("rolled-back DDL published staging readiness or a pass")
				}
				scanStagingTestRelationsAbsent(t, ctx, store)
			} else {
				if !store.ownership.scanStagingReady || store.ownership.scanStagingPasses[stage.scanID] != stage {
					t.Fatal("confirmed rollback discarded existing staging pass bookkeeping")
				}
				var sealed, seenExists bool
				if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
					return tx.QueryRow(`SELECT sealed,to_regclass($4) IS NOT NULL FROM `+scanReconciliationPassTable+`
						WHERE generation=$1 AND scan_id=$2 AND library_id=$3`, stage.generation, stage.scanID, stage.libraryID,
						scanReconciliationSeenTable).Scan(&sealed, &seenExists)
				}); err != nil || sealed || !seenExists {
					t.Fatalf("confirmed rollback did not preserve the unsealed control and relations: sealed=%t seen=%t error=%v", sealed, seenExists, err)
				}
				if err := stage.Seal(ctx); err != nil {
					t.Fatalf("reuse staging after confirmed rollback: %v", err)
				}
			}
			ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
		})
	}
}

func TestScanStagingCleanupKeepsItsProtectedCallerCancellationContract(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	published := false
	err := store.withScanStagingTx(caller, true, func(transaction *scanStagingTx) (func(), error) {
		if _, err := transaction.tx.Exec(transaction.ctx,
			`INSERT INTO server_settings(key,value) VALUES('scan-staging-caller-cancellation','committed')`); err != nil {
			return nil, err
		}
		cancelCaller()
		if transaction.ctx.Err() != nil {
			return nil, errors.New("caller cancellation escaped into the staging write context")
		}
		return func() { published = true }, nil
	})
	if err != nil || !published || !errors.Is(caller.Err(), context.Canceled) {
		t.Fatalf("ordinary caller cancellation changed protected cleanup commit: published=%t caller=%v error=%v", published, caller.Err(), err)
	}
	ownedTransactionsExpectSetting(t, ctx, pool, "scan-staging-caller-cancellation", "committed")
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestScanStagingAttemptedCommitFailureStillDiscardsItsSession(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	published := false
	err := store.withScanStagingTx(ctx, false, func(transaction *scanStagingTx) (func(), error) {
		if err := transaction.create(); err != nil {
			return nil, err
		}
		if _, err := transaction.tx.Exec(transaction.ctx,
			`INSERT INTO server_settings(key,value) VALUES('scan-staging-failed-commit','must roll back')`); err != nil {
			return nil, err
		}
		ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
		if transaction.ctx.Err() != nil {
			return nil, errors.New("the attempted staging commit fixture exhausted its write context")
		}
		return func() { published = true }, nil
	})
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, pgx.ErrTxCommitRollback) ||
		published || ctx.Err() != nil || store.Available() || store.ownership.conn != nil {
		t.Fatalf("an attempted staging commit failure escaped disposal: published=%t caller=%v error=%v", published, ctx.Err(), err)
	}
	ownedTransactionsExpectAbsent(t, ctx, pool, "scan-staging-failed-commit")
}
