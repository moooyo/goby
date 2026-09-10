package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// This is the deployment protocol key, observed from pg_locks below. These
// tests deliberately remain serial because the lease spans the whole database,
// including the separate schemas owned by migrationTestPool.
const deploymentLeaseTestID int64 = 4919415424202458201

type deploymentLeaseAttempt struct {
	lease *database.Lease
	err   error
	pool  int
}

type deploymentLeaseOwner struct {
	pid         int32
	classID     int64
	objectID    int64
	subID       int32
	application string
}

func deploymentLeasePool(t *testing.T, ctx context.Context, source *pgxpool.Pool) (*pgxpool.Pool, string) {
	t.Helper()
	config := source.Config().Copy()
	// The helper's random schema gives each owned session a unique, bounded
	// marker. Termination tests require this marker and the current SQL role.
	application := config.ConnConfig.RuntimeParams["search_path"] + "_lease"
	if application == "_lease" || len(application) > 63 {
		t.Fatal("lease integration fixture lacks a bounded isolated session marker")
	}
	config.ConnConfig.RuntimeParams["application_name"] = application
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create independent deployment lease pool")
	}
	t.Cleanup(pool.Close)
	return pool, application
}

func deploymentLeaseLocks(t *testing.T, ctx context.Context, observer *pgxpool.Pool, applications []string) []deploymentLeaseOwner {
	t.Helper()
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := observer.Query(queryCtx, `SELECT locks.pid, locks.classid::bigint,
		locks.objid::bigint, locks.objsubid, activity.application_name
		FROM pg_catalog.pg_locks locks
		JOIN pg_catalog.pg_stat_activity activity ON activity.pid=locks.pid
		WHERE locks.locktype='advisory' AND locks.mode='ExclusiveLock' AND locks.granted
		AND locks.database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
		AND activity.usename=current_user AND activity.application_name=ANY($1::text[])
		ORDER BY locks.pid, locks.classid, locks.objid, locks.objsubid`, applications)
	if err != nil {
		t.Fatalf("observe owned deployment lease sessions: %v", err)
	}
	defer rows.Close()
	var owners []deploymentLeaseOwner
	for rows.Next() {
		var owner deploymentLeaseOwner
		if err := rows.Scan(&owner.pid, &owner.classID, &owner.objectID, &owner.subID, &owner.application); err != nil {
			t.Fatalf("read owned deployment lease identity: %v", err)
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish reading owned deployment leases: %v", err)
	}
	return owners
}

func requireDeploymentLeaseOwner(t *testing.T, ctx context.Context, observer *pgxpool.Pool, applications []string) deploymentLeaseOwner {
	t.Helper()
	owners := deploymentLeaseLocks(t, ctx, observer, applications)
	if len(owners) != 1 {
		t.Fatalf("owned deployment lease sessions = %d, want exactly one", len(owners))
	}
	owner := owners[0]
	actualID := int64(uint64(owner.classID)<<32 | uint64(owner.objectID))
	if owner.pid <= 0 || owner.subID != 1 || actualID != deploymentLeaseTestID {
		t.Fatalf("deployment lease uses an unexpected advisory lock identity: %+v", owner)
	}
	// The two-integer device registration protocol uses objsubid=2 and cannot
	// collide with this bigint key. These are the existing fixed bigint keys.
	existingIDs := map[string]int64{
		"migration":     migrationTimeoutLockID,
		"bootstrap":     4919415424202458191,
		"managed_users": 4919415424202458192,
	}
	for namespace, id := range existingIDs {
		if actualID == id {
			t.Fatalf("deployment lease collides with the %s advisory namespace", namespace)
		}
	}
	return owner
}

func cleanupDeploymentLease(t *testing.T, lease *database.Lease) {
	t.Helper()
	t.Cleanup(func() { closeDeploymentLeaseForCleanup(t, lease) })
}

func closeDeploymentLeaseForCleanup(t *testing.T, lease *database.Lease) {
	t.Helper()
	finished := make(chan error, 1)
	go func() { finished <- lease.Close() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-finished:
		if err != nil && !errors.Is(err, database.ErrLeaseUnavailable) {
			t.Errorf("close deployment lease during cleanup: %v", err)
		}
	case <-timer.C:
		t.Error("deployment lease cleanup exceeded its shutdown bound")
	}
}

func waitDeploymentLeaseDone(t *testing.T, lease *database.Lease) {
	t.Helper()
	// The monitor allows one second between probes and bounds probe/connection
	// cleanup separately. This also tolerates scheduling on the shared test host.
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-lease.Done():
	case <-timer.C:
		t.Fatal("deployment lease did not report termination within its monitoring bound")
	}
}

func closeDeploymentLeaseConcurrently(t *testing.T, lease *database.Lease, allowUnavailable bool) {
	t.Helper()
	const callers = 4
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- lease.Close()
		}()
	}
	close(start)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var first error
	for index := range callers {
		select {
		case err := <-results:
			if err != nil && (!allowUnavailable || !errors.Is(err, database.ErrLeaseUnavailable)) {
				t.Errorf("close deployment lease: %v", err)
			}
			if index == 0 {
				first = err
			} else if !errors.Is(err, first) || !errors.Is(first, err) {
				t.Error("concurrent Close calls returned different lease outcomes")
			}
		case <-timer.C:
			t.Fatal("concurrent deployment lease shutdown exceeded its bound")
		}
	}
	waitDeploymentLeaseDone(t, lease)
	go func() { results <- lease.Close() }()
	select {
	case err := <-results:
		if !errors.Is(err, first) || !errors.Is(first, err) {
			t.Error("repeated Close changed the completed lease outcome")
		}
	case <-timer.C:
		t.Fatal("repeated deployment lease shutdown exceeded its bound")
	}
}

func TestDeploymentLeaseConcurrentAcquisitionHasOneDatabaseWideWinner(t *testing.T) {
	ctx, sourceA := migrationTestPool(t)
	_, sourceB := migrationTestPool(t)
	poolA, applicationA := deploymentLeasePool(t, ctx, sourceA)
	poolB, applicationB := deploymentLeasePool(t, ctx, sourceB)
	pools := []*pgxpool.Pool{poolA, poolB}
	applications := []string{applicationA, applicationB}
	const attempts = 6
	start := make(chan struct{})
	results := make(chan deploymentLeaseAttempt, attempts)
	attemptCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	received := 0
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for received < attempts {
			select {
			case attempt := <-results:
				received++
				if attempt.lease != nil {
					closeDeploymentLeaseForCleanup(t, attempt.lease)
				}
			case <-timer.C:
				t.Error("deployment lease contender did not finish after cancellation")
				return
			}
		}
	})
	for index := range attempts {
		go func() {
			<-start
			lease, err := database.AcquireLease(attemptCtx, pools[index%len(pools)])
			results <- deploymentLeaseAttempt{lease: lease, err: err, pool: index % len(pools)}
		}()
	}
	close(start)
	var winners []deploymentLeaseAttempt
	busy := 0
	for received < attempts {
		select {
		case attempt := <-results:
			received++
			if attempt.lease != nil {
				cleanupDeploymentLease(t, attempt.lease)
			}
			switch {
			case attempt.err == nil && attempt.lease != nil:
				winners = append(winners, attempt)
			case errors.Is(attempt.err, database.ErrLeaseBusy) && attempt.lease == nil:
				busy++
			default:
				t.Errorf("concurrent lease acquisition returned an unexpected outcome: %v", attempt.err)
			}
		case <-attemptCtx.Done():
			t.Fatal("concurrent deployment lease acquisition exceeded its bound")
		}
	}
	if len(winners) != 1 || busy != attempts-1 {
		t.Fatalf("deployment contenders produced %d winners and %d busy responses", len(winners), busy)
	}
	winner := winners[0]
	select {
	case <-winner.lease.Done():
		t.Fatal("the winning deployment lease was lost during contention")
	default:
	}
	requireDeploymentLeaseOwner(t, ctx, sourceA, applications)
	closeDeploymentLeaseConcurrently(t, winner.lease, false)
	if owners := deploymentLeaseLocks(t, ctx, sourceA, applications); len(owners) != 0 {
		t.Fatal("closing the deployment lease left a session-level advisory lock")
	}
	// Use the other pool and its different schema to prove that Close releases
	// database ownership rather than merely making its original pool reusable.
	successor, err := database.AcquireLease(ctx, pools[1-winner.pool])
	if err != nil || successor == nil {
		t.Fatalf("another pool could not acquire the released deployment lease: %v", err)
	}
	cleanupDeploymentLease(t, successor)
	requireDeploymentLeaseOwner(t, ctx, sourceA, applications)
	closeDeploymentLeaseConcurrently(t, successor, false)
}

func TestDeploymentLeaseReportsOwnedBackendLossAndAllowsSuccessor(t *testing.T) {
	ctx, observer := migrationTestPool(t)
	pool, application := deploymentLeasePool(t, ctx, observer)
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil || lease == nil {
		t.Fatalf("acquire deployment lease before backend termination: %v", err)
	}
	cleanupDeploymentLease(t, lease)
	owner := requireDeploymentLeaseOwner(t, ctx, observer, []string{application})
	terminateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var terminated bool
	// Match the observed PID, lock key, database, unique owned application name,
	// and current SQL role again at termination. No unrelated backend is eligible.
	err = observer.QueryRow(terminateCtx, `SELECT pg_terminate_backend(activity.pid)
		FROM pg_catalog.pg_locks locks
		JOIN pg_catalog.pg_stat_activity activity ON activity.pid=locks.pid
		WHERE locks.pid=$1 AND locks.classid::bigint=$2 AND locks.objid::bigint=$3
		AND locks.objsubid=$4 AND locks.locktype='advisory' AND locks.mode='ExclusiveLock' AND locks.granted
		AND locks.database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
		AND activity.application_name=$5 AND activity.usename=current_user
		AND activity.pid<>pg_backend_pid()`, owner.pid, owner.classID, owner.objectID, owner.subID, owner.application).Scan(&terminated)
	if err != nil || !terminated {
		t.Fatalf("terminate only the owned deployment lease backend: terminated=%v error=%v", terminated, err)
	}
	waitDeploymentLeaseDone(t, lease)
	if owners := deploymentLeaseLocks(t, ctx, observer, []string{application}); len(owners) != 0 {
		t.Fatal("a lost deployment lease retained or silently reacquired its advisory lock")
	}
	// Reacquire before calling Close on the failed owner. Ownership loss itself
	// must release the session, and old-owner shutdown must preserve its successor.
	successor, err := database.AcquireLease(ctx, pool)
	if err != nil || successor == nil {
		t.Fatalf("acquire deployment lease after its owning connection was lost: %v", err)
	}
	cleanupDeploymentLease(t, successor)
	closeDeploymentLeaseConcurrently(t, lease, true)
	select {
	case <-successor.Done():
		t.Fatal("closing the failed owner invalidated its successor")
	default:
	}
	requireDeploymentLeaseOwner(t, ctx, observer, []string{application})
	closeDeploymentLeaseConcurrently(t, successor, false)
}

func TestDeploymentLeaseNilBoundaries(t *testing.T) {
	lease, err := database.AcquireLease(context.Background(), nil)
	if lease != nil || !errors.Is(err, database.ErrLeaseUnavailable) {
		t.Fatalf("nil pool lease acquisition returned an unexpected outcome: %v", err)
	}
	var absent *database.Lease
	if err := absent.Close(); err != nil {
		t.Fatalf("close nil deployment lease: %v", err)
	}
}

func TestDeploymentLeaseCancelledAcquisitionLeavesNoOwnership(t *testing.T) {
	ctx, observer := migrationTestPool(t)
	pool, application := deploymentLeasePool(t, ctx, observer)
	for _, test := range []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
	}{
		{"cancelled", func() (context.Context, context.CancelFunc) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			return cancelled, cancel
		}},
		{"expired", func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(ctx, time.Now().Add(-time.Second))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			attemptCtx, cancel := test.context()
			defer cancel()
			result := make(chan deploymentLeaseAttempt, 1)
			go func() {
				lease, err := database.AcquireLease(attemptCtx, pool)
				result <- deploymentLeaseAttempt{lease: lease, err: err}
			}()
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case attempt := <-result:
				if attempt.lease != nil {
					cleanupDeploymentLease(t, attempt.lease)
				}
				if attempt.lease != nil || attempt.err == nil || errors.Is(attempt.err, database.ErrLeaseBusy) {
					t.Fatalf("cancelled lease acquisition returned ownership or contention: %v", attempt.err)
				}
			case <-timer.C:
				t.Fatal("cancelled deployment lease acquisition did not return promptly")
			}
			if owners := deploymentLeaseLocks(t, ctx, observer, []string{application}); len(owners) != 0 {
				t.Fatal("cancelled acquisition left a deployment advisory lock")
			}
		})
	}
	successor, err := database.AcquireLease(ctx, pool)
	if err != nil || successor == nil {
		t.Fatalf("cancelled acquisition prevented a later deployment lease: %v", err)
	}
	cleanupDeploymentLease(t, successor)
	closeDeploymentLeaseConcurrently(t, successor, false)
}
