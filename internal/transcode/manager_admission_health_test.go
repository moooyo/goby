//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerQueueAdmissionPreservesOtherSubjectExecutionCapacity(t *testing.T) {
	for _, owner := range []string{"user", "credential", "application key"} {
		for _, queue := range []int{1, 4, 16} {
			t.Run(fmt.Sprintf("%s/queue-%d", owner, queue), func(t *testing.T) {
				started := make(chan int64, queue+4)
				run := func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
					started <- plan.StartTicks
					<-ctx.Done()
					return RunResult{}, ctx.Err()
				}
				options := managerTestOptions(t, run)
				options.MaxQueueJobs = queue
				m := newTestManager(t, options)
				specFor := func(index int) Spec {
					spec := managerTestSpec(index)
					spec.Plan.StartTicks = int64(index)
					if owner == "user" {
						spec.Scope.AuthSessionID = fmt.Sprintf("auth-%d", index)
					}
					if owner == "application key" {
						spec.Scope.UserID = ""
						spec.Scope.ApplicationKey = true
						spec.Scope.ApplicationClientID = fmt.Sprintf("client-%d", index)
					}
					return spec
				}
				first := specFor(1)
				if _, err := m.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
					t.Fatal(err)
				}
				if got := managerTestWaitStart(t, started); got != 1 {
					t.Fatalf("first runner = %d", got)
				}
				allowance := max(1, queue/2)
				var last Record
				for index := 2; index <= allowance+1; index++ {
					var err error
					last, err = m.Ensure(context.Background(), specFor(index), managerTestInput(t))
					if err != nil {
						t.Fatalf("admit queued job %d: %v", index, err)
					}
				}
				rejected := managerTestInput(t)
				if _, err := m.Ensure(context.Background(), specFor(allowance+2), rejected); !errors.Is(err, ErrBusy) {
					t.Fatalf("subject exhausted its queue but admission returned %v", err)
				}
				assertManagerInputClosed(t, rejected)
				duplicate := managerTestInput(t)
				if reused, err := m.Ensure(context.Background(), last.Spec, duplicate); err != nil || reused.ID != last.ID {
					t.Fatalf("queue pressure prevented existing job reuse: %v", err)
				}
				assertManagerInputClosed(t, duplicate)
				other := managerTestSpec(100)
				other.Scope.UserID, other.Scope.AuthSessionID = "other-user", "other-auth"
				other.Plan.StartTicks = 100
				if _, err := m.Ensure(context.Background(), other, managerTestInput(t)); err != nil {
					t.Fatalf("another subject could not use the idle execution slot: %v", err)
				}
				if got := managerTestWaitStart(t, started); got != 100 {
					t.Fatalf("another subject did not reach its idle execution slot: %d", got)
				}
				if health := m.Health(); !health.Available || health.Code != "ready" {
					t.Fatalf("ordinary queue pressure degraded engine health: %+v", health)
				}
			})
		}
	}
}

func TestManagerAdmissionCountsJobsAwaitingPersistence(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 2)
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	options.MaxQueueJobs = 1
	options.Repository = &managerTestRepository{createGate: gate, createEntered: entered}
	m := newTestManager(t, options)
	defer close(gate)
	results := make(chan error, 2)
	for index := 1; index <= 2; index++ {
		spec, input := managerTestSpec(index), managerTestInput(t)
		go func() {
			_, err := m.Ensure(context.Background(), spec, input)
			results <- err
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("creation did not reach its controlled persistence barrier")
		}
	}
	input := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(3), input); !errors.Is(err, ErrBusy) {
		t.Fatalf("pending creates bypassed subject admission: %v", err)
	}
	assertManagerInputClosed(t, input)
	// Shutdown cancels both pending creations and consumes their input files.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; !errors.Is(err, ErrPersistence) && !errors.Is(err, ErrManagerClosed) {
			t.Fatalf("pending create returned %v during shutdown", err)
		}
	}
	if health := m.Health(); health.Available || health.Code != "manager_closed" {
		t.Fatalf("closed manager advertised availability: %+v", health)
	}
}

func TestManagerSmallRetentionBudgetPreservesOtherSubjectExecutionCapacity(t *testing.T) {
	started := make(chan int64, 2)
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	options.MaxRetainedJobs = options.MaxJobs
	m := newTestManager(t, options)
	first := managerTestSpec(1)
	first.Plan.StartTicks = 1
	if _, err := m.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	input := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(2), input); !errors.Is(err, ErrBusy) {
		t.Fatalf("one subject consumed another subject's retained execution capacity: %v", err)
	}
	assertManagerInputClosed(t, input)
	other := managerTestSpec(3)
	other.Scope.UserID, other.Scope.AuthSessionID = "other-user", "other-auth"
	other.Plan.StartTicks = 3
	if _, err := m.Ensure(context.Background(), other, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	if got := managerTestWaitStart(t, started); got != 3 {
		t.Fatalf("another subject did not start with the smallest retained budget: %d", got)
	}
}

func TestManagerHealthReportsGuardedCacheFailure(t *testing.T) {
	options := managerTestOptions(t, func(_ context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(directory, 188)
	})
	options.IdleTimeout = time.Hour
	m, err := NewManager(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	var unknown, id string
	t.Cleanup(func() {
		m.cancel()
		<-m.loopDone
		if unknown != "" {
			if err := os.Remove(unknown); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove owned unknown fixture: %v", err)
			}
		}
		if id != "" {
			if err := m.cache.RemoveJob(id); err != nil {
				t.Errorf("remove owned cache fixture: %v", err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil && !errors.Is(err, ErrCacheUnsafe) {
			t.Errorf("close manager: %v", err)
		}
	})
	record, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	id = record.ID
	if completed := managerTestWaitFinished(t, m, id); completed.State != "completed" {
		t.Fatalf("controlled runner failed: %+v", completed)
	}
	if health := m.Health(); !health.Available || health.Code != "ready" {
		t.Fatalf("healthy engine status = %+v", health)
	}
	// Stop periodic maintenance so this test directly controls the real guarded
	// cleanup path. Production shutdown also sets closing before cancellation.
	m.cancel()
	<-m.loopDone
	unknown = filepath.Join(options.Root, id, "unknown-health-fixture.txt")
	if err := os.WriteFile(unknown, []byte("preserve unknown content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.CancelJob(id, record.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	m.reclaim(job, false)
	if health := m.Health(); health.Available || health.Code != "cache_unavailable" {
		t.Fatalf("guarded cleanup failure was not exposed: %+v", health)
	}
	if content, err := os.ReadFile(unknown); err != nil || string(content) != "preserve unknown content" {
		t.Fatalf("unsafe cleanup changed unknown content: %q, %v", content, err)
	}
	input := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(2), input); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("unhealthy engine admitted work: %v", err)
	}
	assertManagerInputClosed(t, input)
}
