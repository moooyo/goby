//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type productionSealer interface {
	SealProduction(string, Scope) error
}

func managerProductionSealer(t *testing.T, manager *Manager) productionSealer {
	t.Helper()
	sealer, ok := any(manager).(productionSealer)
	if !ok {
		t.Fatal("the concrete manager does not provide retained-output production sealing")
	}
	return sealer
}

func managerSealTestSpec(index int) Spec {
	spec := managerTestSpec(index)
	spec.Plan.SegmentMode, spec.Plan.EndTicks = "vod", 60*ticksPerSecond
	return spec
}

func TestManagerSealProductionRetainsPublishedCacheAndFencesFutureWork(t *testing.T) {
	var launches atomic.Int32
	observed := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRunner()
	run := func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(directory, 188); err != nil {
			return RunResult{}, err
		}
		if launches.Add(1) == 1 {
			<-ctx.Done()
			close(observed)
			<-release
			return RunResult{ProductionSealSafe: true}, ctx.Err()
		}
		return RunResult{}, nil
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	sealer := managerProductionSealer(t, m)
	spec := managerSealTestSpec(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := m.Ensure(ctx, spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	reader, err := m.TryOpen(spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := sealer.SealProduction(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	if err := sealer.SealProduction(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, observed, "production cancellation")
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); err != nil || !snapshot.ProductionSealed {
		t.Fatalf("late attachment could not observe the immediate production fence: %+v/%v", snapshot, err)
	}
	retained, err := m.TryOpen(spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatalf("sealing invalidated a previously published segment before finalization: %v", err)
	}
	defer retained.Close()
	missingCtx, missingCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer missingCancel()
	if handle, err := m.Open(missingCtx, spec.Scope, record.ID, "segment-1.ts"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
		if handle != nil {
			_ = handle.Close()
		}
		t.Fatalf("sealed missing output waited for future publication or became readable: %v", err)
	}
	replacement, err := m.Ensure(ctx, spec, managerTestInput(t))
	if err != nil || replacement.ID == record.ID {
		t.Fatalf("replacement admission inherited a sealed producer: %+v/%v", replacement, err)
	}
	releaseRunner()
	final := managerTestWaitFinished(t, m, record.ID)
	if final.State != "cancelled" || final.ErrorCode != "production_sealed" || final.OutputBytes < 188 {
		t.Fatalf("partial sealed output masqueraded as complete or lost its charge: %+v", final)
	}
	if _, err := m.Snapshot(spec.Scope, record.ID); err != nil {
		t.Fatalf("retained sealed output was not readable after finalization: %v", err)
	}
	if err := m.CancelJob(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	if handle, err := m.TryOpen(spec.Scope, record.ID, "segment-0.ts"); handle != nil || !errors.Is(err, ErrJobCancelled) {
		if handle != nil {
			_ = handle.Close()
		}
		t.Fatalf("full invalidation did not override sealing: %v", err)
	}
	directory := filepath.Join(m.options.Root, record.ID)
	m.maintain()
	if _, err := os.Stat(directory); err != nil {
		t.Fatal("cache was reclaimed while sealed readers still owned it")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	// Final-reader wakeup can race the background maintenance owner. Observe
	// the actual filesystem deletion, rather than one maintenance pass's view
	// of the directory flag while another owner is completing guarded cleanup.
	deletionDeadline := time.NewTimer(3 * time.Second)
	defer deletionDeadline.Stop()
	deletionPoll := time.NewTicker(time.Millisecond)
	defer deletionPoll.Stop()
	for {
		m.maintain()
		_, err := os.Stat(directory)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatalf("observe invalidated sealed cache deletion: %v", err)
		}
		select {
		case <-deletionDeadline.C:
			t.Fatal("invalidated sealed cache remained after reader release")
		case <-deletionPoll.C:
		}
	}
	managerTestWaitFinished(t, m, replacement.ID)
}

func TestManagerSealProductionRejectsUnsafeOutputContracts(t *testing.T) {
	for _, mode := range []string{"unbounded", "generated", "progressive"} {
		t.Run(mode, func(t *testing.T) {
			run := func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				<-ctx.Done()
				return RunResult{}, ctx.Err()
			}
			m := newTestManager(t, managerAccessOptions(t, run))
			sealer := managerProductionSealer(t, m)
			spec := managerTestSpec(1)
			switch mode {
			case "generated":
				spec.Plan.Container, spec.Plan.HLS.SegmentType = "mp4", "fmp4"
			case "progressive":
				spec.Plan.Container, spec.Plan.OutputMode = "mp4", "progressive"
				spec.Plan.SourceFormatStartKnown, spec.Plan.SegmentSeconds = true, 0
			}
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := sealer.SealProduction(record.ID, spec.Scope); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("%s output acquired unsupported partial cache semantics: %v", mode, err)
			}
			if _, err := m.Snapshot(spec.Scope, record.ID); err != nil {
				t.Fatal("rejected seal still invalidated the original producer")
			}
			if err := m.CancelJob(record.ID, spec.Scope); err != nil {
				t.Fatal(err)
			}
			managerTestWaitFinished(t, m, record.ID)
		})
	}
}

func TestManagerSealProductionRequiresIndependentRunnerProof(t *testing.T) {
	run := func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(directory, 188); err != nil {
			return RunResult{}, err
		}
		<-ctx.Done()
		// Cancellation alone cannot prove unchanged input, clean observers or
		// actual process retirement, even though the final directory is safe.
		return RunResult{}, ctx.Err()
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerSealTestSpec(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := m.Ensure(ctx, spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	if err := managerProductionSealer(t, m).SealProduction(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	final := managerTestWaitFinished(t, m, record.ID)
	if final.ErrorCode == "production_sealed" {
		t.Fatal("safe cache facts incorrectly substituted for runner proof")
	}
	if handle, err := m.TryOpen(spec.Scope, record.ID, "segment-0.ts"); handle != nil || !errors.Is(err, ErrJobCancelled) {
		if handle != nil {
			_ = handle.Close()
		}
		t.Fatalf("unproved cancellation retained readable partial output: %v", err)
	}
}

func TestManagerSealProductionColdWindowHasNoFutureReadiness(t *testing.T) {
	run := func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerSealTestSpec(1)
	input := managerTestInput(t)
	record, err := m.Ensure(context.Background(), spec, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SealProduction(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); !errors.Is(err, ErrOutputUnavailable) && !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("cold seal waited for impossible first output: %v", err)
	}
	final := managerTestWaitFinished(t, m, record.ID)
	if final.State == "completed" || final.ErrorCode == "production_sealed" {
		t.Fatal("cold pause granted readable or complete output without a published segment")
	}
	assertManagerInputClosed(t, input)
}

func TestManagerSealProductionStopsLastOwnerWhenCacheLookupIsFenced(t *testing.T) {
	observed := make(chan struct{})
	run := func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(directory, 188); err != nil {
			return RunResult{}, err
		}
		<-ctx.Done()
		close(observed)
		return RunResult{ProductionSealSafe: true}, ctx.Err()
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerSealTestSpec(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := m.Ensure(ctx, spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.cacheFailed = true
	m.mu.Unlock()
	if err := m.SealProduction(record.ID, spec.Scope); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("unsafe cache lookup acquired retained-output semantics: %v", err)
	}
	managerAccessWait(t, observed, "last-owner cancellation despite cache lookup fence")
	final := managerTestWaitFinished(t, m, record.ID)
	if final.ErrorCode != "cache_unavailable" || final.State != "failed" {
		t.Fatalf("cache fence left unowned production or retained unsafe output: %+v", final)
	}
}
