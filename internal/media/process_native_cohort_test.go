package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

func nativeProbeCohortInput(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "native-cohort-input-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func nativeProbeCohortCounts(receipt *ProbeRetirementReceipt) (pending, claims int) {
	receipt.cohort.mu.Lock()
	defer receipt.cohort.mu.Unlock()
	return len(receipt.cohort.pending), receipt.cohort.claims
}

func TestNativeProbeCohortKnownPrestartFailureHasIndependentReceipt(t *testing.T) {
	for _, test := range []struct {
		name  string
		scope *commanddomain.CommandScope
	}{
		{"missing scope", nil}, {"uninitialized scope", &commanddomain.CommandScope{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := nativeProbeCohortInput(t)
			governor := newMediaProcessAdmission(1, 1, 1)
			var counter atomic.Uint64
			command := exec.Command("/must-never-spawn")
			command.ExtraFiles = []*os.File{input}
			_, receipt, err := probeWithRetirement(context.Background(), input, func(ctx context.Context) (Info, error) {
				process, err := startNativeMediaProcessWithCounter(ctx, command, governor, test.scope, &counter)
				if process != nil {
					return Info{}, errors.New("known rejection returned a native process")
				}
				return Info{}, err
			})
			pending, claims := nativeProbeCohortCounts(receipt)
			if !errors.Is(err, commanddomain.ErrUnavailable) || !receipt.RetirementComplete() || receipt.UnknownObserved() ||
				pending != 0 || claims != 1 || command.Process != nil || command.ProcessState != nil {
				t.Fatalf("known no-child rejection was confused with semantic failure: error=%v pending=%d claims=%d", err, pending, claims)
			}
			if stats := processCapacityStatsFor(governor, &counter); stats.Active != 0 || stats.Queued != 0 || stats.RetirementUnknown != 0 {
				t.Fatalf("known rejection retained synthetic admission: %+v", stats)
			}
		})
	}
}

func TestNativeProbeCohortStdoutPrestartFailureNeverCallsParser(t *testing.T) {
	for _, scope := range []*commanddomain.CommandScope{nil, &commanddomain.CommandScope{}} {
		input := nativeProbeCohortInput(t)
		command := exec.Command("/must-never-spawn")
		command.ExtraFiles = []*os.File{input}
		called := false
		before := GetProcessCapacityStats()
		bound := commanddomain.WithCommandScope(context.Background(), scope)
		_, receipt, err := probeWithRetirement(bound, input, func(ctx context.Context) (Info, error) {
			parseErr, waitErr, startErr := RunProcessStdout(ctx, command, func(io.Reader) error { called = true; return nil })
			return Info{}, errors.Join(parseErr, waitErr, startErr)
		})
		pending, claims := nativeProbeCohortCounts(receipt)
		if !errors.Is(err, commanddomain.ErrUnavailable) || called || command.Process != nil || pending != 0 || claims != 1 ||
			!receipt.RetirementComplete() || receipt.UnknownObserved() {
			t.Fatalf("stdout no-child receipt lost its exact source registration: error=%v pending=%d claims=%d", err, pending, claims)
		}
		if after := GetProcessCapacityStats(); after != before {
			t.Fatalf("stdout known rejection leaked original capacity: before=%+v after=%+v", before, after)
		}
	}
}

func TestNativeProbeCohortTracksOnlyOriginalBorrowedSourcePointer(t *testing.T) {
	input, other := nativeProbeCohortInput(t), nativeProbeCohortInput(t)
	command := exec.Command("/must-never-spawn")
	command.ExtraFiles = []*os.File{other}
	governor := newMediaProcessAdmission(1, 1, 1)
	var counter atomic.Uint64
	_, receipt, err := probeWithRetirement(context.Background(), input, func(ctx context.Context) (Info, error) {
		_, err := startNativeMediaProcessWithCounter(ctx, command, governor, nil, &counter)
		return Info{}, err
	})
	pending, claims := nativeProbeCohortCounts(receipt)
	if !errors.Is(err, commanddomain.ErrUnavailable) || pending != 0 || claims != 0 || !receipt.RetirementComplete() || receipt.UnknownObserved() {
		t.Fatalf("a foreign source acquired cohort ownership: error=%v pending=%d claims=%d", err, pending, claims)
	}
}

func TestNativeProbeCohortUnownedRawPipeKeepsStickyPendingReceipt(t *testing.T) {
	input := nativeProbeCohortInput(t)
	command := exec.Command("/must-never-spawn")
	command.ExtraFiles = []*os.File{input}
	reader, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if writer, ok := command.Stdout.(*os.File); ok {
		defer writer.Close()
	}
	governor := newMediaProcessAdmission(1, 1, 1)
	var counter atomic.Uint64
	_, receipt, err := probeWithRetirement(context.Background(), input, func(ctx context.Context) (Info, error) {
		_, err := startNativeMediaProcessWithCounter(ctx, command, governor, &commanddomain.CommandScope{}, &counter)
		return Info{}, err
	})
	pending, claims := nativeProbeCohortCounts(receipt)
	if !errors.Is(err, commanddomain.ErrWaitOwnership) || !errors.Is(err, ErrProcessRetirementUnknown) || pending != 1 || claims != 1 ||
		receipt.RetirementComplete() || !receipt.UnknownObserved() || command.Process != nil {
		t.Fatalf("an unowned template pipe fabricated a clean probe receipt: error=%v pending=%d claims=%d", err, pending, claims)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = receipt.Close(ctx); !errors.Is(err, ErrProcessRetirementUnknown) || receipt.RetirementComplete() || !receipt.UnknownObserved() {
		t.Fatalf("cleanup dropped unproved raw ownership: %v", err)
	}
}

type nativeProbeCohortAbortContext struct {
	context.Context
	abort func()
}

func (ctx nativeProbeCohortAbortContext) Err() error { ctx.abort(); return nil }

func TestNativeProbeCohortAbnormalAdmissionCannotInferNotStarted(t *testing.T) {
	for _, test := range []struct {
		name  string
		abort func()
	}{
		{"panic", func() { panic("synthetic admission context fault") }}, {"goexit", runtime.Goexit},
	} {
		for _, stream := range []bool{false, true} {
			name := test.name
			if stream {
				name += " stdout"
			}
			t.Run(name, func(t *testing.T) {
				// Faults occur before a native constructor or fork. This exercises
				// sticky ownership bookkeeping, not a fake successful native owner.
				input := nativeProbeCohortInput(t)
				cohort := &probeRetirementCohort{source: input, pending: make(map[*probeRetirementChild]struct{}), drained: make(chan struct{})}
				receipt := &ProbeRetirementReceipt{cohort: cohort}
				parent := context.WithValue(context.Background(), probeRetirementContextKey{}, cohort)
				ctx := nativeProbeCohortAbortContext{Context: parent, abort: test.abort}
				command := exec.Command("/must-never-spawn")
				command.ExtraFiles = []*os.File{input}
				governor := newMediaProcessAdmission(1, 1, 1)
				var counter atomic.Uint64
				joined := make(chan struct{})
				before := GetProcessCapacityStats()
				go func() {
					defer close(joined)
					defer func() { _ = recover() }()
					if stream {
						_, _, _ = runNativeMediaStdout(ctx, command, func(io.Reader) error { return nil }, &commanddomain.CommandScope{})
					} else {
						_, _ = startNativeMediaProcessWithCounter(ctx, command, governor, &commanddomain.CommandScope{}, &counter)
					}
				}()
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Fatal("abnormal native admission did not actually return its caller")
				}
				pending, claims := nativeProbeCohortCounts(receipt)
				if !receipt.UnknownObserved() || receipt.RetirementComplete() || pending != 1 || claims != 1 || command.Process != nil {
					t.Fatalf("abnormal admission inferred no owned child: pending=%d claims=%d", pending, claims)
				}
				if stats := processCapacityStatsFor(governor, &counter); stats.Active != 0 || stats.Queued != 0 {
					t.Fatalf("pre-admission fault charged an uncreated child: %+v", stats)
				}
				if after := GetProcessCapacityStats(); after != before {
					t.Fatalf("pre-admission stream fault changed global capacity: before=%+v after=%+v", before, after)
				}
			})
		}
	}
}

func TestNativeProbeCohortUnknownNativeRegistryOwnerRetainsOriginalCharge(t *testing.T) {
	// The test owns only synthetic admission and registry state. It neither
	// creates a native Domain nor reports a successful operating-system join.
	input := nativeProbeCohortInput(t)
	governor := newMediaProcessAdmission(1, 1, 1)
	release, err := governor.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var counter atomic.Uint64
	var owner *nativeMediaProcess
	t.Cleanup(func() {
		if owner != nil {
			nativeMediaOwners.mu.Lock()
			if nativeMediaOwners.entries[owner.slot] == owner {
				nativeMediaOwners.entries[owner.slot] = nil
			}
			nativeMediaOwners.mu.Unlock()
		}
		release()
	})
	_, receipt, err := probeWithRetirement(context.Background(), input, func(ctx context.Context) (Info, error) {
		command := exec.Command("/must-never-spawn")
		command.ExtraFiles = []*os.File{input}
		child, err := beginProbeRetirementChild(ctx, command)
		if err != nil {
			return Info{}, err
		}
		owner, err = newNativeMediaOwner(&commanddomain.CommandScope{}, governor, release, &counter)
		if err != nil {
			return Info{}, err
		}
		owner.probeChild = child
		owner.markUnknown()
		owner.returnKnownCapacity()
		return Info{}, nil // Semantic success must not hide the ownership fault.
	})
	pending, claims := nativeProbeCohortCounts(receipt)
	if !errors.Is(err, ErrProcessRetirementUnknown) || !receipt.UnknownObserved() || receipt.RetirementComplete() || pending != 1 || claims != 1 {
		t.Fatalf("semantic success released an uncertain native probe child: error=%v pending=%d claims=%d", err, pending, claims)
	}
	if stats := processCapacityStatsFor(governor, &counter); stats.Active != 1 || stats.RetirementUnknown != 1 {
		t.Fatalf("unknown native owner returned its original charge: %+v", stats)
	}
	nativeMediaOwners.mu.Lock()
	held := nativeMediaOwners.entries[owner.slot] == owner
	nativeMediaOwners.mu.Unlock()
	if !held {
		t.Fatal("the exact unknown native registry reference was discarded")
	}
}

func TestNativeProbeCohortCleanupCannotInferNoChildBeforeStartReturns(t *testing.T) {
	// Only the fixed Start-publication gate and retained bookkeeping are tested.
	// No native constructor, process or passing kernel retirement is fabricated.
	input := nativeProbeCohortInput(t)
	cohort := &probeRetirementCohort{source: input, pending: make(map[*probeRetirementChild]struct{}), drained: make(chan struct{})}
	receipt := &ProbeRetirementReceipt{cohort: cohort}
	command := exec.Command("/must-never-spawn")
	command.ExtraFiles = []*os.File{input}
	ctx := context.WithValue(context.Background(), probeRetirementContextKey{}, cohort)
	child, err := beginProbeRetirementChild(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	governor := newMediaProcessAdmission(1, 1, 1)
	release, err := governor.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var counter atomic.Uint64
	owner, err := newNativeMediaOwner(&commanddomain.CommandScope{}, governor, release, &counter)
	if err != nil {
		release()
		t.Fatal(err)
	}
	owner.probeChild = child
	entered := make(chan struct{})
	child.bindClose(func() error { close(entered); return owner.close() })
	t.Cleanup(func() {
		owner.finishStart()
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = receipt.Close(cleanup)
		select {
		case <-cohort.drained:
		case <-cleanup.Done():
			t.Error("test cleanup did not actually join its receipt cleanup owner")
			return
		}
		nativeMediaOwners.mu.Lock()
		if nativeMediaOwners.entries[owner.slot] == owner {
			nativeMediaOwners.entries[owner.slot] = nil
		}
		nativeMediaOwners.mu.Unlock()
		release()
	})
	observation, cancel := context.WithCancel(context.Background())
	observed, callerJoined := make(chan error, 1), make(chan struct{})
	go func() { defer close(callerJoined); observed <- receipt.Close(observation) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("receipt cleanup never entered its actual bound owner")
	}
	cancel()
	select {
	case err = <-observed:
	case <-time.After(time.Second):
		t.Fatal("cancelled receipt observer did not return")
	}
	select {
	case <-callerJoined:
	case <-time.After(time.Second):
		t.Fatal("cancelled receipt observer did not actually join")
	}
	if !errors.Is(err, context.Canceled) || receipt.UnknownObserved() {
		t.Fatalf("in-flight Start was guessed instead of observed: %v", err)
	}
	pending, claims := nativeProbeCohortCounts(receipt)
	if pending != 1 || claims != 1 {
		t.Fatal("the observation deadline dropped an in-flight child")
	}
	// Publish an unknown exit, not a nil-handle rollback. Actual native code
	// makes this publication from its normal/abnormal Start-exit defers.
	owner.markUnknown()
	owner.finishStart()
	owner.finishStart()
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = receipt.Close(join); !errors.Is(err, ErrProcessRetirementUnknown) || !receipt.UnknownObserved() || receipt.RetirementComplete() {
		t.Fatalf("the actual cleanup observer converted unknown Start into no-child retirement: %v", err)
	}
	if stats := processCapacityStatsFor(governor, &counter); stats.Active != 1 || stats.RetirementUnknown != 1 {
		t.Fatalf("unknown Start returned original admission: %+v", stats)
	}
}
