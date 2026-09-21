//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Production has no quarantine-release path. Tests explicitly simulate process
// exit after their assertions, closing only their own retained session and root.
func releaseRetainedScanEvidenceTestOwner(t *testing.T, store *Store) {
	t.Helper()
	if store.done != nil {
		joined, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = store.Close(joined)
		cancel()
		select {
		case <-store.done:
		default:
			t.Error("test owner still has unfinished close work; its fences remain retained")
			return
		}
	}
	retainedScanEvidenceOwners.Lock()
	retained, exists := retainedScanEvidenceOwners.owners[store.ownership]
	delete(retainedScanEvidenceOwners.owners, store.ownership)
	retainedScanEvidenceOwners.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if exists && retained.connection != nil {
		if err := retained.connection.Close(ctx); err != nil {
			t.Error("close the test's retained PostgreSQL session")
		}
	} else if store.ownership != nil {
		if err := store.ownership.release(); err != nil {
			t.Error("release the test's non-detached catalog owner")
		}
	}
	if store.scanEvidence != nil {
		if err := store.scanEvidence.disk.close(); err != nil {
			t.Error(err)
		}
	}
}

func requireScanEvidencePoolClosed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	done := make(chan struct{})
	go func() { pool.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pool close waited for retained scan evidence ownership")
	}
}

func TestScanEvidenceFailedCleanupDetachesPoolButRetainsCatalogAndFilesystemFences(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, original, mediaRoot, _ := libraryIntegrationStore(t, prober)
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	options := scanEvidenceTestOptions(t)
	store, err := New(pool, prober, []string{mediaRoot}, WithScanEvidence(options))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseRetainedScanEvidenceTestOwner(t, store) })
	pass, err := store.newScanReconciliationEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.scanEvidence.disk.root.Rename(pass.spool.name, "retained-failed-pass"); err != nil {
		t.Fatal(err)
	}
	if err := pass.Close(); err == nil {
		t.Fatal("fixture did not create a real cleanup failure")
	}
	if done, receipt := pass.CleanupStatus(); !done || receipt.Err == nil {
		t.Fatal("test must join the failed retirement before closing its owner")
	}
	peer, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal("connect independent catalog fence observer")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = peer.Close(cleanup)
	})
	transaction, err := store.beginOwnedTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(transaction)
	closed := make(chan error, 1)
	go func() { closed <- store.Close(ctx) }()
	select {
	case err := <-closed:
		t.Fatalf("close bypassed an in-flight owned transaction: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if _, err := transaction.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatal("detachment interrupted the protected transaction")
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cleanup failure was not preserved: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("store close did not finish after its transaction and retirement joined")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed owner still occupies a pool checkout")
	}
	if _, err := store.beginOwnedTx(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("detached generation admitted another owned transaction")
	}
	retainedScanEvidenceOwners.Lock()
	retained := retainedScanEvidenceOwners.owners[store.ownership]
	retainedScanEvidenceOwners.Unlock()
	if retained.connection == nil || retained.manager != store.scanEvidence || retained.connection.IsClosed() || !store.ownership.lost.Load() {
		t.Fatal("failed generation did not retain the raw session and filesystem manager")
	}
	requireScanEvidencePoolClosed(t, pool)
	var acquired bool
	if err := peer.QueryRow(ctx, "SELECT pg_try_advisory_lock($1::bigint)", store.ownership.key).Scan(&acquired); err != nil || acquired {
		if acquired {
			_, _ = peer.Exec(ctx, "SELECT pg_advisory_unlock($1::bigint)", store.ownership.key)
		}
		t.Fatal("pool closure released the failed generation's catalog advisory fence")
	}
	if successor, err := newScanEvidenceManager(ctx, store.scanEvidence.disk.document.Scope, options, []string{mediaRoot}); err == nil {
		_ = successor.close()
		t.Fatal("detachment released failed filesystem ownership")
	}
	// A later database failure cannot unlock the independent filesystem owner.
	if err := retained.connection.Close(ctx); err != nil {
		t.Fatal("simulate loss of the retained database session")
	}
	if successor, err := newScanEvidenceManager(ctx, store.scanEvidence.disk.document.Scope, options, []string{mediaRoot}); err == nil {
		_ = successor.close()
		t.Fatal("database session loss released failed filesystem ownership")
	}
	if status := store.ScanEvidenceStatus(); status.ActivePasses != 1 || status.CleanupFailures != 1 || status.ReservedBytes == 0 {
		t.Fatalf("detachment erased the failed reservation: %+v", status)
	}
}

func TestScanEvidenceNewRecoveryErrorClosesCleanInventoryAndPool(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, original, mediaRoot, _ := libraryIntegrationStore(t, prober)
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// This fixture owns a random schema. Renaming only its scan table forces
	// New's real recovery error after catalog ownership and spool initialization.
	if _, err := pool.Exec(ctx, "ALTER TABLE scan_jobs RENAME TO scan_jobs_recovery_fixture"); err != nil {
		t.Fatal(err)
	}
	options := scanEvidenceTestOptions(t)
	store, err := New(pool, prober, []string{mediaRoot}, WithScanEvidence(options))
	if store != nil || err == nil || !strings.Contains(err.Error(), "recover interrupted scans") {
		t.Fatal("fixture did not reach New's actual recovery failure branch")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed New retained an unnecessary pool checkout")
	}
	if _, err := os.Stat(filepath.Join(options.Directory, scanEvidenceManifest)); err != nil {
		t.Fatal("recovery failure occurred before evidence ownership was initialized")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE scan_jobs_recovery_fixture RENAME TO scan_jobs"); err != nil {
		t.Fatal(err)
	}
	// A normally cleaned failed constructor may reopen; it has no quarantine.
	successor, err := New(pool, prober, []string{mediaRoot}, WithScanEvidence(options))
	if err != nil {
		t.Fatal(err)
	}
	if err := successor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	requireScanEvidencePoolClosed(t, pool)
}

func TestScanEvidenceStartupCleanupFailureUsesTheSameDetachedFence(t *testing.T) {
	ctx, pool, original, mediaRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := acquireScanOwnership(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &Store{pool: pool, ownership: owner, ctx: lifetime, cancel: cancel}
	options := scanEvidenceTestOptions(t)
	scope, err := store.scanEvidenceScope(ctx, options.ServerID)
	if err != nil {
		_ = owner.release()
		t.Fatal(err)
	}
	store.scanEvidence, err = newScanEvidenceManager(lifetime, scope, options, []string{mediaRoot})
	if err != nil {
		_ = owner.release()
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseRetainedScanEvidenceTestOwner(t, store) })
	// Model the constructor's state before any scan worker is started. Its
	// failure cleanup calls this exact shared production retirement function.
	store.scanEvidence.mu.Lock()
	store.scanEvidence.err = errors.New("owned startup cleanup fixture failure")
	store.scanEvidence.mu.Unlock()
	cancel()
	if err := store.retireScanEvidenceOwnership(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("startup cleanup error was lost")
	}
	retainedScanEvidenceOwners.Lock()
	retained, exists := retainedScanEvidenceOwners.owners[owner]
	retainedScanEvidenceOwners.Unlock()
	if !exists || retained.connection == nil || retained.manager != store.scanEvidence || pool.Stat().AcquiredConns() != 0 {
		t.Fatal("a failed constructor without a returned Store lost its strong cleanup owner")
	}
	requireScanEvidencePoolClosed(t, pool)
}
