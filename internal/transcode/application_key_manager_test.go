//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func applicationManagerSpec(index int, credential string) Spec {
	spec := managerTestSpec(index)
	spec.Scope.UserID = ""
	spec.Scope.ApplicationKey = true
	spec.Scope.AuthSessionID = credential
	spec.Scope.DeviceID = "device-" + credential
	spec.Scope.ApplicationClientID = "application-client-" + spec.Scope.PlaySessionID
	spec.Plan.StartTicks = int64(index)
	return spec
}

func TestApplicationKeyManagerUsesCredentialQuotaAndExactCacheOwnership(t *testing.T) {
	started := make(chan int64, 8)
	run := func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	options := managerTestOptions(t, run)
	options.MaxJobs, options.MaxUserJobs, options.MaxSessionJobs = 4, 1, 2
	manager := newTestManager(t, options)
	first := applicationManagerSpec(1, "key-first")
	firstInput := managerTestInput(t)
	firstRecord, err := manager.Ensure(context.Background(), first, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 1 {
		t.Fatalf("first runner = %d", value)
	}
	duplicateInput := managerTestInput(t)
	if duplicate, err := manager.Ensure(context.Background(), first, duplicateInput); err != nil || duplicate.ID != firstRecord.ID {
		t.Fatalf("same key scope failed to reuse cached work: %+v, %v", duplicate, err)
	}
	assertManagerInputClosed(t, duplicateInput)
	second := applicationManagerSpec(2, first.Scope.AuthSessionID)
	secondRecord, err := manager.Ensure(context.Background(), second, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 2 {
		t.Fatalf("key inherited the unrelated user quota: runner=%d", value)
	}
	queued := applicationManagerSpec(3, first.Scope.AuthSessionID)
	queuedRecord, err := manager.Ensure(context.Background(), queued, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	other := applicationManagerSpec(4, "key-second")
	otherRecord, err := manager.Ensure(context.Background(), other, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 4 {
		t.Fatalf("credential quota leaked across keys or admitted excess work: runner=%d", value)
	}
	select {
	case value := <-started:
		t.Fatalf("key exceeded its credential quota: runner=%d", value)
	case <-time.After(30 * time.Millisecond):
	}
	foreign := first.Scope
	foreign.ApplicationClientID = second.Scope.ApplicationClientID
	if _, err := manager.Snapshot(foreign, firstRecord.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("sibling client acquired cached status: %v", err)
	}
	foreign = first.Scope
	foreign.AuthSessionID = other.Scope.AuthSessionID
	if _, err := manager.Snapshot(foreign, firstRecord.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("another key acquired cached status: %v", err)
	}
	foreign = first.Scope
	foreign.ApplicationKey = false
	foreign.UserID = "viewer"
	if _, err := manager.Snapshot(foreign, firstRecord.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("normal user acquired userless cached status: %v", err)
	}
	if err := manager.Cancel(context.Background(), first.Scope); err != nil {
		t.Fatal(err)
	}
	assertManagerInputClosed(t, firstInput)
	if value := managerTestWaitStart(t, started); value != 3 {
		t.Fatalf("released credential capacity started runner=%d", value)
	}
	manager.CancelSession(first.Scope.AuthSessionID)
	for _, record := range []Record{secondRecord, queuedRecord} {
		if _, err := manager.Snapshot(record.Spec.Scope, record.ID); !errors.Is(err, ErrJobCancelled) {
			t.Fatalf("parent revocation left a client encoding usable: %v", err)
		}
	}
	if _, err := manager.Snapshot(other.Scope, otherRecord.ID); err != nil {
		t.Fatalf("revoking one key cancelled another key: %v", err)
	}
}

func TestApplicationKeyManagerRejectsAmbiguousIdentityAndClosesSource(t *testing.T) {
	run := func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	manager := newTestManager(t, managerTestOptions(t, run))
	for _, edit := range []func(*Scope){
		func(scope *Scope) { scope.UserID = "invented-user" },
		func(scope *Scope) { scope.ApplicationKey = false },
		func(scope *Scope) { scope.ApplicationClientID = "" },
		func(scope *Scope) { scope.AuthSessionID = "" },
	} {
		spec := applicationManagerSpec(1, "key")
		edit(&spec.Scope)
		input := managerTestInput(t)
		if _, err := manager.Ensure(context.Background(), spec, input); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("ambiguous application scope was admitted: %+v, %v", spec.Scope, err)
		}
		assertManagerInputClosed(t, input)
	}
}

func TestApplicationKeyManagerOutputRequiresTheExactClientContext(t *testing.T) {
	run := func(_ context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(directory, 188)
	}
	manager := newTestManager(t, managerTestOptions(t, run))
	spec := applicationManagerSpec(1, "shared-key")
	record, err := manager.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, manager, record.ID)
	foreign := spec.Scope
	foreign.ApplicationClientID = "other-real-client-context"
	if _, err := manager.Open(context.Background(), foreign, record.ID, "segment-0.ts"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("same key sibling client acquired output: %v", err)
	}
	handle, err := manager.Open(context.Background(), spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatalf("own client output unavailable: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}
