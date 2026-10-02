package transcode

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResourceLifecycleRetirementBarrierAndStagesAreOnceOnly(t *testing.T) {
	var barriers atomic.Int32
	var events [resourceLifecycleStages]atomic.Int32
	lifecycle := &resourceLifecycle{
		retire: func(context.Context) error { barriers.Add(1); return nil },
		onStage: func(event resourceLifecycleEvent) {
			events[event.Stage].Add(1)
			if event.At.IsZero() {
				t.Error("lifecycle event lacks a timestamp")
			}
		},
	}
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := lifecycle.retireProcesses(context.Background()); err != nil {
				t.Error(err)
			}
			if err := lifecycle.runnerReturned(context.Background()); err != nil {
				t.Error(err)
			}
			lifecycle.record(resourceWorkspaceSealed, false)
			lifecycle.record(resourceTerminalPersisted, false)
		}()
	}
	workers.Wait()
	if barriers.Load() != 1 {
		t.Fatalf("retirement barrier ran %d times", barriers.Load())
	}
	for stage := range resourceLifecycleStages {
		if events[stage].Load() != 1 {
			t.Fatalf("stage %d emitted %d events", stage, events[stage].Load())
		}
	}
	snapshot := lifecycle.snapshot()
	if !snapshot.RetirementVerified || snapshot.RetirementError != nil || snapshot.ProcessRetired.IsZero() ||
		snapshot.WritersDrained.IsZero() || snapshot.WorkspaceSealed.IsZero() || snapshot.TerminalPersisted.IsZero() {
		t.Fatalf("lifecycle boundaries were lost: %+v", snapshot)
	}
	if snapshot.WritersDrained.Before(snapshot.ProcessRetired) {
		t.Fatal("output drain preceded the retirement barrier")
	}
}

func TestResourceLifecycleFailedBarrierDoesNotReportRetirement(t *testing.T) {
	failure := errors.New("command domain remained populated")
	var barriers, retirements atomic.Int32
	lifecycle := &resourceLifecycle{
		retire: func(context.Context) error { barriers.Add(1); return failure },
		onStage: func(event resourceLifecycleEvent) {
			if event.Stage == resourceProcessRetired {
				retirements.Add(1)
			}
		},
	}
	if err := lifecycle.retireProcesses(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("retirement error was lost: %v", err)
	}
	if err := lifecycle.runnerReturned(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("final output boundary hid the retirement failure: %v", err)
	}
	snapshot := lifecycle.snapshot()
	if barriers.Load() != 1 || retirements.Load() != 0 || !snapshot.ProcessRetired.IsZero() ||
		snapshot.RetirementVerified || !errors.Is(snapshot.RetirementError, failure) || snapshot.WritersDrained.IsZero() {
		t.Fatalf("failed domain became eligible for early capacity release: %+v", snapshot)
	}
}

func TestResourceLifecycleDefaultRetirementIsUnverifiedReturnObservation(t *testing.T) {
	lifecycle := &resourceLifecycle{}
	if err := lifecycle.retireProcesses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := lifecycle.snapshot(); !snapshot.ProcessRetired.IsZero() || !snapshot.WritersDrained.IsZero() {
		t.Fatalf("unverified process group authorized an early boundary: %+v", snapshot)
	}
	if err := lifecycle.runnerReturned(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := lifecycle.snapshot()
	if snapshot.ProcessRetired.IsZero() || snapshot.WritersDrained.IsZero() || snapshot.RetirementVerified {
		t.Fatalf("default Run return boundary changed its meaning: %+v", snapshot)
	}
}

func TestResourceLifecycleContextKeepsBorrowedRecord(t *testing.T) {
	lifecycle := &resourceLifecycle{}
	ctx := withResourceLifecycle(context.Background(), lifecycle)
	if resourceLifecycleFromContext(ctx) != lifecycle || resourceLifecycleFromContext(context.Background()) != nil {
		t.Fatal("runner did not receive the exact lifecycle owner")
	}
}
