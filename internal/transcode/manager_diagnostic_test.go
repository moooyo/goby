//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestManagerDiagnosticReservationIsExclusiveWithoutRecords(t *testing.T) {
	options := managerTestOptions(t, nil)
	m := newTestManager(t, options)
	const contenders = 32
	start := make(chan struct{})
	type outcome struct {
		release func()
		err     error
	}
	results := make(chan outcome, contenders)
	for range contenders {
		go func() {
			<-start
			release, err := m.ReserveDiagnostic()
			results <- outcome{release: release, err: err}
		}()
	}
	close(start)
	accepted, rejected := 0, 0
	for range contenders {
		select {
		case result := <-results:
			if result.release != nil {
				t.Cleanup(result.release)
			}
			if result.err == nil && result.release != nil {
				accepted++
			} else if errors.Is(result.err, ErrBusy) && result.release == nil {
				rejected++
			} else {
				t.Errorf("unexpected diagnostic admission result: %v", result.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent diagnostic admission did not finish")
		}
	}
	if accepted != 1 || rejected != contenders-1 {
		t.Fatalf("diagnostic admissions accepted=%d rejected=%d, want one owner", accepted, rejected)
	}
	if health := m.Health(); !health.Available || health.Code != "ready" {
		t.Fatalf("temporary diagnostic capacity changed engine health: %+v", health)
	}
	repository := options.Repository.(*managerTestRepository)
	repository.mu.Lock()
	records, updates := len(repository.records), len(repository.updates)
	repository.mu.Unlock()
	if records != 0 || updates != 0 {
		t.Fatal("diagnostic reservation created or updated an encoding record")
	}
}

func TestManagerDiagnosticReservationSharesGlobalExecutionSlots(t *testing.T) {
	started := make(chan int64, 2)
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	options.MaxRuntime, options.StartupTimeout, options.NoProgressTimeout = time.Minute, time.Minute, time.Minute
	m := newTestManager(t, options)
	first := managerTestSpec(1)
	first.Plan.StartTicks = 1
	if _, err := m.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 1 {
		t.Fatalf("unexpected initial conversion: %d", value)
	}
	release, err := m.ReserveDiagnostic()
	if err != nil {
		t.Fatalf("reserve the one remaining execution slot: %v", err)
	}
	t.Cleanup(release)
	second := managerTestSpec(2)
	second.Scope.UserID, second.Scope.AuthSessionID, second.Plan.StartTicks = "other-user", "other-auth", 2
	record, err := m.Ensure(context.Background(), second, managerTestInput(t))
	if err != nil {
		t.Fatalf("queue another subject while a diagnostic owns the slot: %v", err)
	}
	// Scheduling is synchronous up to publishing launch. This checks the fence
	// without relying on a delayed goroutine or a sleep to notice an overrun.
	m.schedule()
	m.mu.Lock()
	launch := m.jobs[record.ID].launch
	m.mu.Unlock()
	select {
	case <-launch:
		t.Fatal("conversion launched into the reserved diagnostic slot")
	default:
	}
	release()
	if value := managerTestWaitStart(t, started); value != 2 {
		t.Fatalf("release did not wake the queued conversion: %d", value)
	}
	if extra, err := m.ReserveDiagnostic(); !errors.Is(err, ErrBusy) || extra != nil {
		if extra != nil {
			extra()
		}
		t.Fatalf("diagnostic bypassed two active global slots: %v", err)
	}
}

func TestManagerDiagnosticReleaseIsIdempotentAcrossLaterReservation(t *testing.T) {
	m := newTestManager(t, managerTestOptions(t, nil))
	first, err := m.ReserveDiagnostic()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first)
	var releases sync.WaitGroup
	for range 32 {
		releases.Add(1)
		go func() {
			defer releases.Done()
			first()
		}()
	}
	releases.Wait()
	second, err := m.ReserveDiagnostic()
	if err != nil {
		t.Fatalf("released capacity could not be acquired again: %v", err)
	}
	t.Cleanup(second)
	first()
	if extra, err := m.ReserveDiagnostic(); !errors.Is(err, ErrBusy) || extra != nil {
		if extra != nil {
			extra()
		}
		t.Fatalf("an old release removed a later reservation: %v", err)
	}
	second()
}

func TestManagerDiagnosticReservationRejectsUnavailableManager(t *testing.T) {
	for _, state := range []string{"closing", "cache_failed"} {
		t.Run(state, func(t *testing.T) {
			m := newTestManager(t, managerTestOptions(t, nil))
			want := ErrOutputUnavailable
			if state == "closing" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := m.Close(ctx); err != nil {
					t.Fatal(err)
				}
				want = ErrManagerClosed
			} else {
				m.mu.Lock()
				m.cacheFailed = true
				m.mu.Unlock()
			}
			if release, err := m.ReserveDiagnostic(); !errors.Is(err, want) || release != nil {
				if release != nil {
					release()
				}
				t.Fatalf("unavailable manager returned %v, want %v", err, want)
			}
		})
	}
}

func TestManagerCloseWaitsForDiagnosticRelease(t *testing.T) {
	options := managerTestOptions(t, nil)
	m := newTestManager(t, options)
	release, err := m.ReserveDiagnostic()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("manager closed before the diagnostic owner released: %v", err)
	}
	if extra, err := m.ReserveDiagnostic(); !errors.Is(err, ErrManagerClosed) || extra != nil {
		if extra != nil {
			extra()
		}
		t.Fatalf("closing manager admitted another diagnostic: %v", err)
	}
	if cache, err := openCacheRoot(options.Root); !errors.Is(err, ErrCacheLocked) {
		if cache != nil {
			_ = cache.Close()
		}
		t.Fatalf("manager released its cache while the diagnostic was outstanding: %v", err)
	}
	release()
	closed, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := m.Close(closed); err != nil {
		t.Fatalf("close did not finish after diagnostic release: %v", err)
	}
	cache, err := openCacheRoot(options.Root)
	if err != nil {
		t.Fatalf("completed close retained its filesystem lock: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
}
