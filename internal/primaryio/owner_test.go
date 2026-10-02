package primaryio

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
)

func TestPrimaryIOOwnerCancelDrainsActualPhaseAndRetainedCleanupSeparately(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	lease := primaryIOAcquire(t, owner, primaryIORoute("source", "disk"), Foreground)
	cancelObserved, readRetired, cleanupRetired := make(chan struct{}), make(chan struct{}), make(chan struct{})
	allowReadRetirement, allowCleanup := make(chan struct{}), make(chan struct{})
	var releaseRead, releaseCleanup sync.Once
	t.Cleanup(func() {
		releaseRead.Do(func() { close(allowReadRetirement) })
		releaseCleanup.Do(func() { close(allowCleanup) })
	})
	workerErrors := make(chan error, 1)
	go func() {
		<-owner.Context().Done()
		close(cancelObserved)
		<-allowReadRetirement
		if err := lease.Release(); err != nil {
			workerErrors <- err
			return
		}
		close(readRetired)
		<-allowCleanup
		workerErrors <- owner.Complete()
		close(cleanupRetired)
	}()
	closed := make(chan struct{})
	closeErrors := make(chan error, 1)
	closeContext, cancelClose := context.WithCancel(context.Background())
	t.Cleanup(cancelClose)
	go func() { closeErrors <- owners.Close(closeContext); close(closed) }()
	primaryIOAwaitClosed(t, cancelObserved)
	primaryIOAssertCounts(t, governor, 1, 0, 0)
	if err := owner.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("complete during actual read = %v", err)
	}
	if _, err := owners.Register(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("register after close fence = %v", err)
	}
	primaryIOAssertOpen(t, closed)
	releaseRead.Do(func() { close(allowReadRetirement) })
	primaryIOAwaitClosed(t, readRetired)
	primaryIOAssertCounts(t, governor, 0, 0, 0)
	if stats := owners.Stats(); stats.RegisteredOwners != 1 {
		t.Fatalf("retained owner vanished between read and cleanup: %+v", stats)
	}
	primaryIOAssertOpen(t, closed)
	releaseCleanup.Do(func() { close(allowCleanup) })
	primaryIOAwaitClosed(t, cleanupRetired)
	primaryIOAwaitClosed(t, closed)
	if err := primaryIOAwaitValue(t, workerErrors); err != nil {
		t.Fatal(err)
	}
	if err := primaryIOAwaitValue(t, closeErrors); err != nil {
		t.Fatal(err)
	}
	if stats := owners.Stats(); stats.RegisteredOwners != 0 || !stats.Closed {
		t.Fatalf("drained runtime = %+v", stats)
	}
}

func TestPrimaryIOOwnerCloseDeadlineDoesNotReleaseActiveOrIdleRetention(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	active := primaryIOOwner(t, owners, context.Background())
	idle := primaryIOOwner(t, owners, context.Background())
	lease := primaryIOAcquire(t, active, primaryIORoute("source", "disk"), Background)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owners.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline close = %v", err)
	}
	primaryIOAssertCounts(t, governor, 1, 1, 0)
	if stats := owners.Stats(); stats.RegisteredOwners != 2 {
		t.Fatalf("deadline discarded retained registrations: %+v", stats)
	}
	if active.Context().Err() == nil || idle.Context().Err() == nil {
		t.Fatal("close did not cancel every registered owner")
	}
	primaryIORelease(t, lease)
	primaryIOComplete(t, active)
	if err := owners.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("idle retained descriptor no longer blocks close: %v", err)
	}
	primaryIOComplete(t, idle)
	primaryIOClose(t, owners)
}

func TestPrimaryIOOwnerQueuedAcquisitionBlocksTransferAndCompletion(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	holder := primaryIOOwner(t, owners, context.Background())
	first := primaryIOAcquire(t, holder, primaryIORoute("source", "disk"), Background)
	waiter := primaryIOOwner(t, owners, context.Background())
	result := primaryIOQueuedAcquire(t, waiter, primaryIORoute("source", "disk"), Background)
	primaryIOAssertCounts(t, governor, 1, 1, 1)
	if err := waiter.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("complete queued acquisition = %v", err)
	}
	if _, err := waiter.Transfer(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("transfer queued acquisition = %v", err)
	}
	if _, err := waiter.Acquire(primaryIORoute("other", "other-disk"), Foreground); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("nested acquisition = %v", err)
	}
	if err := waiter.Cancel(); err != nil {
		t.Fatal(err)
	}
	value := primaryIOAwaitAcquire(t, result)
	if !errors.Is(value.err, context.Canceled) || value.lease != nil {
		t.Fatalf("canceled queued result = %+v", value)
	}
	primaryIOAssertCounts(t, governor, 1, 1, 0)
	primaryIOComplete(t, waiter)
	primaryIORelease(t, first)
	primaryIOComplete(t, holder)
}

func TestPrimaryIOOwnerIdleTransferProtectsReceiverBetweenHTTPChunks(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	sender := primaryIOOwner(t, owners, context.Background())
	first := primaryIOAcquire(t, sender, primaryIORoute("source", "disk"), Foreground)
	primaryIORelease(t, first)
	receiver, err := sender.Transfer()
	if err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func() error{
		"cancel": sender.Cancel, "complete": sender.Complete,
		"transfer": func() error { _, err := sender.Transfer(); return err },
		"acquire":  func() error { _, err := sender.Acquire(primaryIORoute("source", "disk"), Foreground); return err },
	} {
		if err := action(); !errors.Is(err, ErrStaleOwner) {
			t.Fatalf("stale %s = %v", name, err)
		}
	}
	if receiver.Context().Err() != nil {
		t.Fatal("stale sender canceled receiver")
	}
	primaryIOAssertCounts(t, governor, 0, 0, 0)
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("network-write retention was completed by stale sender")
	}
	second := primaryIOAcquire(t, receiver, primaryIORoute("source", "disk"), Foreground)
	primaryIORelease(t, second)
	primaryIOComplete(t, receiver)
	primaryIOComplete(t, receiver)
}

func TestPrimaryIOTransferLeaseMovesActiveAndRetainedRightsAtomically(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	sender := primaryIOOwner(t, owners, context.Background())
	phase := primaryIOAcquire(t, sender, primaryIORoute("source", "disk"), Foreground)
	if _, err := sender.Transfer(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("idle transfer while active = %v", err)
	}
	receiver, receivedPhase, err := sender.TransferLease(phase)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Complete(); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("stale completion = %v", err)
	}
	if err := sender.Cancel(); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("stale cancellation = %v", err)
	}
	if err := phase.Release(); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale phase release = %v", err)
	}
	if _, err := phase.Transfer(); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale phase transfer = %v", err)
	}
	if _, _, err := sender.TransferLease(receivedPhase); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("old owner/new phase = %v", err)
	}
	if _, _, err := receiver.TransferLease(phase); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("new owner/old phase = %v", err)
	}
	primaryIOAssertCounts(t, governor, 1, 0, 0)
	if receiver.Context().Err() != nil || owners.Stats().RegisteredOwners != 1 {
		t.Fatal("stale sender changed receiver retention")
	}
	primaryIORelease(t, receivedPhase)
	primaryIOAssertCounts(t, governor, 0, 0, 0)
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("phase release also released descriptor retention")
	}
	primaryIOComplete(t, receiver)
}

func TestPrimaryIOLeaseOnlyTransferDoesNotCompleteRetainedOwner(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	phase := primaryIOAcquire(t, owner, primaryIORoute("source", "disk"), Foreground)
	received, err := phase.Transfer()
	if err != nil {
		t.Fatal(err)
	}
	if err := phase.Release(); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale phase = %v", err)
	}
	if err := owner.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("retained owner completed borrowed active phase = %v", err)
	}
	primaryIORelease(t, received)
	primaryIORelease(t, received)
	primaryIOAssertCounts(t, governor, 0, 0, 0)
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("phase handle transfer completed owner")
	}
	primaryIOComplete(t, owner)
}

func TestPrimaryIOTransferLeaseRejectsForeignAndRetiredPhasesWithoutMutation(t *testing.T) {
	governor, owners := primaryIOFixture(t, primaryIOLimits())
	first := primaryIOOwner(t, owners, context.Background())
	second := primaryIOOwner(t, owners, context.Background())
	phase := primaryIOAcquire(t, first, primaryIORoute("first", "first-disk"), Foreground)
	if _, _, err := second.TransferLease(phase); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("foreign phase transfer = %v", err)
	}
	primaryIOAssertCounts(t, governor, 1, 0, 0)
	primaryIORelease(t, phase)
	if _, _, err := first.TransferLease(phase); !errors.Is(err, ErrClosed) {
		t.Fatalf("retired phase transfer = %v", err)
	}
	primaryIOComplete(t, first)
	primaryIOComplete(t, second)
}

func TestPrimaryIOTransferLeaseRacingReleaseHasOneLinearizedOwner(t *testing.T) {
	for round := 0; round < 32; round++ {
		governor, owners := primaryIOFixture(t, primaryIOLimits())
		sender := primaryIOOwner(t, owners, context.Background())
		phase := primaryIOAcquire(t, sender, primaryIORoute("source", "disk"), Foreground)
		start := make(chan struct{})
		type transferResult struct {
			owner *Owner
			phase *PrimaryReadLease
			err   error
		}
		transferred := make(chan transferResult, 1)
		released := make(chan error, 1)
		go func() {
			<-start
			owner, next, err := sender.TransferLease(phase)
			transferred <- transferResult{owner, next, err}
		}()
		go func() { <-start; released <- phase.Release() }()
		close(start)
		value, releaseErr := primaryIOAwaitValue(t, transferred), primaryIOAwaitValue(t, released)
		if value.err == nil {
			if !errors.Is(releaseErr, ErrStaleLease) {
				t.Fatalf("round %d transfer won but release = %v", round, releaseErr)
			}
			primaryIOAssertCounts(t, governor, 1, 0, 0)
			primaryIORelease(t, value.phase)
			primaryIOComplete(t, value.owner)
		} else {
			if !errors.Is(value.err, ErrClosed) || releaseErr != nil {
				t.Fatalf("round %d release won: transfer=%v release=%v", round, value.err, releaseErr)
			}
			primaryIOAssertCounts(t, governor, 0, 0, 0)
			primaryIOComplete(t, sender)
		}
		if owners.Stats().RegisteredOwners != 0 {
			t.Fatalf("round %d retained ownership leaked", round)
		}
	}
}

func TestPrimaryIOOwnerAbnormalWorkerExitDoesNotInventRetirement(t *testing.T) {
	for _, kind := range []string{"panic", "goexit"} {
		t.Run(kind, func(t *testing.T) {
			governor, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			started := make(chan primaryIOAcquireResult, 1)
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				defer func() { _ = recover() }()
				phase, err := owner.Acquire(primaryIORoute("source", "disk"), Background)
				started <- primaryIOAcquireResult{phase, err}
				if err != nil {
					return
				}
				_ = owner.Cancel()
				if kind == "panic" {
					panic("abnormal reader fixture")
				}
				runtime.Goexit()
			}()
			acquired := primaryIOAwaitAcquire(t, started)
			if acquired.err != nil || acquired.lease == nil {
				t.Fatalf("abnormal worker did not acquire its phase: %+v", acquired)
			}
			primaryIOAwaitClosed(t, exited)
			// Worker exit alone supplies no descriptor/child retirement receipt.
			primaryIOAssertCounts(t, governor, 1, 1, 0)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := owners.Close(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("unknown retirement close = %v", err)
			}
			if owners.Stats().RegisteredOwners != 1 {
				t.Fatal("abnormal exit fabricated retained cleanup")
			}
			// The supervising adapter must establish actual cleanup before these.
			primaryIORelease(t, acquired.lease)
			primaryIOComplete(t, owner)
			primaryIOClose(t, owners)
		})
	}
}

func TestPrimaryIOOwnerRegisterRacingCloseKeepsOneClosedFence(t *testing.T) {
	for round := 0; round < 32; round++ {
		_, owners := primaryIOFixture(t, primaryIOLimits())
		start := make(chan struct{})
		type registration struct {
			owner *Owner
			err   error
		}
		registered := make(chan registration, 1)
		closed := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		go func() {
			<-start
			owner, err := owners.Register(context.Background())
			registered <- registration{owner, err}
		}()
		go func() { <-start; closed <- owners.Close(ctx) }()
		close(start)
		value, closeErr := primaryIOAwaitValue(t, registered), primaryIOAwaitValue(t, closed)
		if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
			t.Fatalf("round %d close = %v", round, closeErr)
		}
		if !owners.Stats().Closed {
			t.Fatalf("round %d close did not fence registration", round)
		}
		if value.err == nil {
			if value.owner.Context().Err() == nil || owners.Stats().RegisteredOwners != 1 {
				t.Fatalf("round %d accepted owner escaped close cancellation", round)
			}
			primaryIOComplete(t, value.owner)
		} else if !errors.Is(value.err, ErrClosed) || value.owner != nil {
			t.Fatalf("round %d rejected registration = %+v", round, value)
		}
		if _, err := owners.Register(context.Background()); !errors.Is(err, ErrClosed) {
			t.Fatalf("round %d late registration = %v", round, err)
		}
		primaryIOClose(t, owners)
	}
}

func TestPrimaryIOOwnerRetentionLimitSurvivesCancellationUntilCompletion(t *testing.T) {
	governor, err := NewGovernor(primaryIOLimits())
	if err != nil {
		t.Fatal(err)
	}
	owners, err := NewOwnerRuntime(governor, 2)
	if err != nil {
		t.Fatal(err)
	}
	first := primaryIOOwner(t, owners, context.Background())
	second := primaryIOOwner(t, owners, context.Background())
	if _, err := owners.Register(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("retention cap = %v", err)
	}
	if err := first.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.Register(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("cancellation freed retained capacity = %v", err)
	}
	primaryIOComplete(t, first)
	replacement := primaryIOOwner(t, owners, context.Background())
	if owners.Stats().RegisteredOwners != 2 {
		t.Fatal("completed retained owner did not restore exactly one registration")
	}
	primaryIOComplete(t, second)
	primaryIOComplete(t, replacement)
	primaryIOClose(t, owners)
	governor.Close()
}
