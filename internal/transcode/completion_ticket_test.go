package transcode

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCompletionTicketsKeepFinalizationBacklogInsideLaunchBound(t *testing.T) {
	pool, err := newCompletionTicketPool(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	tickets := make([]*completionTicket, 3)
	for index := range tickets {
		tickets[index], err = pool.reserve()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !tickets[0].queue() || !tickets[0].work() || !tickets[1].queue() {
		t.Fatal("the finalization transfer lost its original launch reservation")
	}
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("retired processes created an unbounded finalization conveyor: %v", err)
	}
	if snapshot := pool.snapshot(); snapshot.Reserved != 1 || snapshot.Queued != 1 || snapshot.Working != 1 || snapshot.Available != 0 {
		t.Fatalf("the complete finalization lifetime is not charged: %+v", snapshot)
	}
	if tickets[0].queue() || tickets[0].work() || tickets[1].queue() {
		t.Fatal("a duplicate phase notification enqueued or launched another finalizer")
	}
	if !tickets[0].release() || tickets[0].release() || tickets[0].queue() || tickets[0].work() {
		t.Fatal("a stale completion ticket changed capacity after terminal handling")
	}
	replacement, err := pool.reserve()
	if err != nil || replacement == tickets[0] {
		t.Fatalf("returned completion capacity was not independently reserved: %v", err)
	}
	if snapshot := pool.snapshot(); snapshot.Reserved != 2 || snapshot.Queued != 1 || snapshot.Working != 0 || snapshot.Available != 0 {
		t.Fatalf("terminal handling did not return exactly one allowance: %+v", snapshot)
	}
	for _, ticket := range tickets[1:] {
		ticket.release()
	}
	replacement.release()
	if snapshot := pool.snapshot(); snapshot.Available != snapshot.Capacity || snapshot.Reserved+snapshot.Queued+snapshot.Working != 0 {
		t.Fatalf("completion ownership leaked after all terminal handling: %+v", snapshot)
	}
}

func TestCompletionTicketConcurrentNotificationsReleaseExactlyOnce(t *testing.T) {
	var notifications atomic.Int32
	pool, err := newCompletionTicketPool(1, func() { notifications.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	var queued, working, released atomic.Int32
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if ticket.queue() {
				queued.Add(1)
			}
		}()
	}
	workers.Wait()
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if ticket.work() {
				working.Add(1)
			}
		}()
	}
	workers.Wait()
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if ticket.release() {
				released.Add(1)
			}
		}()
	}
	workers.Wait()
	if queued.Load() != 1 || working.Load() != 1 || released.Load() != 1 || notifications.Load() != 1 {
		t.Fatalf("concurrent callbacks changed completion capacity multiple times: queued=%d working=%d released=%d notified=%d",
			queued.Load(), working.Load(), released.Load(), notifications.Load())
	}
	if snapshot := pool.snapshot(); snapshot.Available != 1 || snapshot.Reserved+snapshot.Queued+snapshot.Working != 0 {
		t.Fatalf("concurrent terminal handling damaged the completion bound: %+v", snapshot)
	}
}

func TestCompletionTicketReturnWakesSchedulerOutsidePoolLock(t *testing.T) {
	var pool *completionTicketPool
	var observed completionTicketSnapshot
	pool, err := newCompletionTicketPool(1, func() { observed = pool.snapshot() })
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	ticket.release()
	if observed.Available != 1 {
		t.Fatalf("scheduler woke before completion capacity was visible: %+v", observed)
	}
}

func TestCompletionTicketShutdownKeepsExistingOwnershipAndRejectsNewLaunches(t *testing.T) {
	var notifications atomic.Int32
	pool, err := newCompletionTicketPool(2, func() { notifications.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	first, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	pool.close()
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("shutdown admitted a new completion lifetime: %v", err)
	}
	if !first.queue() || !first.work() || !first.release() || !second.release() || notifications.Load() != 0 {
		t.Fatal("shutdown discarded existing cleanup ownership or woke admission")
	}
	if snapshot := pool.snapshot(); !snapshot.Closed || snapshot.Available != 2 || snapshot.Reserved+snapshot.Queued+snapshot.Working != 0 {
		t.Fatalf("shutdown did not retain and then drain completion ownership: %+v", snapshot)
	}
}

func TestCompletionTicketPoolRejectsUnboundedOrEmptyCapacity(t *testing.T) {
	for _, capacity := range []int{-1, 0, maxCompletionTickets + 1} {
		if pool, err := newCompletionTicketPool(capacity, nil); pool != nil || !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid completion capacity %d was accepted: %v", capacity, err)
		}
	}
}
