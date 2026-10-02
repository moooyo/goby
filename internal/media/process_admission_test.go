package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mediaProcessAdmissionTestResult struct {
	release func()
	err     error
}

func mediaProcessAdmissionTestWait(t *testing.T, governor *mediaProcessGovernor, active, background, queued int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		governor.mu.Lock()
		actual, actualBackground, actualQueued := governor.active, governor.background, len(governor.waiters)
		governor.mu.Unlock()
		if actual == active && actualBackground == background && actualQueued == queued {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("capacity active/background/queued = %d/%d/%d, want %d/%d/%d", actual, actualBackground, actualQueued, active, background, queued)
		}
		time.Sleep(time.Millisecond)
	}
}

func mediaProcessAdmissionTestAcquire(t *testing.T, governor *mediaProcessGovernor, ctx context.Context) func() {
	t.Helper()
	release, err := governor.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return release
}

func mediaProcessAdmissionTestQueue(governor *mediaProcessGovernor, ctx context.Context) <-chan mediaProcessAdmissionTestResult {
	result := make(chan mediaProcessAdmissionTestResult, 1)
	go func() {
		release, err := governor.acquire(ctx)
		result <- mediaProcessAdmissionTestResult{release: release, err: err}
	}()
	return result
}

func mediaProcessAdmissionTestReceive(t *testing.T, result <-chan mediaProcessAdmissionTestResult) mediaProcessAdmissionTestResult {
	t.Helper()
	select {
	case acquired := <-result:
		if acquired.release != nil {
			t.Cleanup(acquired.release)
		}
		return acquired
	case <-time.After(5 * time.Second):
		t.Fatal("media process admission did not complete")
	}
	return mediaProcessAdmissionTestResult{}
}

func TestMediaProcessAdmissionReservesForeground(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	background := WithBackgroundProcess(context.Background())
	first := mediaProcessAdmissionTestAcquire(t, governor, background)
	second := mediaProcessAdmissionTestAcquire(t, governor, background)
	ctx, cancel := context.WithCancel(background)
	defer cancel()
	queued := mediaProcessAdmissionTestQueue(governor, ctx)
	mediaProcessAdmissionTestWait(t, governor, 2, 2, 1)
	foreground := mediaProcessAdmissionTestAcquire(t, governor, context.Background())
	otherForeground := mediaProcessAdmissionTestAcquire(t, governor, context.Background())
	mediaProcessAdmissionTestWait(t, governor, 4, 2, 1)
	foreground()
	mediaProcessAdmissionTestWait(t, governor, 3, 2, 1)
	first()
	acquired := mediaProcessAdmissionTestReceive(t, queued)
	if acquired.err != nil || acquired.release == nil {
		t.Fatalf("background waiter was not admitted after background retirement: %v", acquired.err)
	}
	mediaProcessAdmissionTestWait(t, governor, 3, 2, 0)
	acquired.release()
	second()
	otherForeground()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessAdmissionOldestEligibleFIFO(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	owner := mediaProcessAdmissionTestAcquire(t, governor, context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var results [3]<-chan mediaProcessAdmissionTestResult
	for index := range results {
		results[index] = mediaProcessAdmissionTestQueue(governor, ctx)
		mediaProcessAdmissionTestWait(t, governor, 1, 0, index+1)
	}
	owner()
	for index, result := range results {
		acquired := mediaProcessAdmissionTestReceive(t, result)
		if acquired.err != nil || acquired.release == nil {
			t.Fatalf("waiter %d failed: %v", index, acquired.err)
		}
		mediaProcessAdmissionTestWait(t, governor, 1, 0, len(results)-index-1)
		acquired.release()
		acquired.release()
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessAdmissionBoundsAndCancelsQueue(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 2)
	owner := mediaProcessAdmissionTestAcquire(t, governor, context.Background())
	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	first := mediaProcessAdmissionTestQueue(governor, firstContext)
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	second := mediaProcessAdmissionTestQueue(governor, secondContext)
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 2)
	if release, err := governor.acquire(context.Background()); release != nil || !errors.Is(err, ErrProcessCapacity) {
		t.Fatalf("full queue returned release=%v error=%v", release != nil, err)
	}
	cancelFirst()
	if acquired := mediaProcessAdmissionTestReceive(t, first); acquired.release != nil || !errors.Is(acquired.err, context.Canceled) {
		t.Fatalf("canceled waiter retained capacity: %v", acquired.err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
	deadlineContext, cancelDeadline := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelDeadline()
	if release, err := governor.acquire(deadlineContext); release != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued deadline returned release=%v error=%v", release != nil, err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
	cancelSecond()
	if acquired := mediaProcessAdmissionTestReceive(t, second); acquired.release != nil || !errors.Is(acquired.err, context.Canceled) {
		t.Fatalf("second canceled waiter retained capacity: %v", acquired.err)
	}
	owner()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessAdmissionGrantCancellationRace(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	for range 200 {
		owner, err := governor.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		result := mediaProcessAdmissionTestQueue(governor, ctx)
		mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
		retired := make(chan struct{})
		go func() { owner(); close(retired) }()
		cancel()
		acquired := mediaProcessAdmissionTestReceive(t, result)
		if acquired.release != nil {
			acquired.release()
		} else if !errors.Is(acquired.err, context.Canceled) {
			t.Fatalf("grant/cancel race returned %v", acquired.err)
		}
		<-retired
		mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	}
}

func TestBackgroundProcessMarkerInheritsCancellation(t *testing.T) {
	if WithBackgroundProcess(nil) != nil {
		t.Fatal("nil context was given a new lifetime")
	}
	base, cancel := context.WithCancel(context.Background())
	marked := WithBackgroundProcess(base)
	derived, stop := context.WithTimeout(marked, time.Hour)
	defer stop()
	if !backgroundMediaProcess(derived) || WithBackgroundProcess(marked) != marked || backgroundMediaProcess(base) {
		t.Fatal("background marker changed classification or failed to inherit")
	}
	cancel()
	if !errors.Is(derived.Err(), context.Canceled) {
		t.Fatal("background marker discarded cancellation")
	}
}
