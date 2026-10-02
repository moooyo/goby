//go:build linux

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This gate sits inside exec's actual stdout copier after the frozen helper's
// bounded readiness record has proved its source read. It does not substitute
// callback return, leader exit, or a timer for actual copier completion.
type backgroundCohortWriter struct {
	sink        *retirementCohortSink
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	unblockOnce sync.Once
	gate        bool
}

func (writer *backgroundCohortWriter) Write(data []byte) (int, error) {
	count, err := writer.sink.Write(data)
	if err == nil && writer.gate && bytes.Contains(data, []byte("\n")) {
		writer.enteredOnce.Do(func() { close(writer.entered) })
		<-writer.release
	}
	return count, err
}

func (writer *backgroundCohortWriter) unblock() {
	writer.unblockOnce.Do(func() { close(writer.release) })
}

type backgroundCohortResult struct {
	info      Info
	receipt   *ProbeRetirementReceipt
	probeErr  error
	runnerErr error
}

type backgroundCohortFixture struct {
	command      *exec.Cmd
	writer       *backgroundCohortWriter
	owner        chan *conventionalMediaProcessOwner
	faultRelease chan struct{}
	faultOnce    sync.Once
	finished     chan struct{}
	result       chan backgroundCohortResult
	controls     func()
	cancelCalls  atomic.Int64
}

func backgroundCohortOwnerFor(command *exec.Cmd) *conventionalMediaProcessOwner {
	conventionalMediaOwners.mu.Lock()
	defer conventionalMediaOwners.mu.Unlock()
	for _, owner := range conventionalMediaOwners.entries {
		if owner != nil && owner.process.command == command {
			return owner
		}
	}
	return nil
}

func backgroundCohortStart(t *testing.T, ctx context.Context, source *os.File, governor *mediaProcessGovernor, unknown *atomic.Uint64, mode string, gate bool, fault func() error) *backgroundCohortFixture {
	t.Helper()
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var controlsOnce sync.Once
	controls := func() {
		controlsOnce.Do(func() {
			_ = controlWrite.Close()
			_ = controlRead.Close()
		})
	}
	executable, err := os.Executable()
	if err != nil {
		controls()
		t.Fatal(err)
	}
	writer := &backgroundCohortWriter{
		sink: retirementCohortNewSink(false), entered: make(chan struct{}), release: make(chan struct{}), gate: gate,
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", mode)
	command.Env = append(os.Environ(), retirementCohortHelperEnvironment+"=1")
	command.ExtraFiles = []*os.File{source, controlRead}
	command.Stdout, command.Stderr = writer, io.Discard
	_ = configureMediaProcess(command)
	fixture := &backgroundCohortFixture{
		command: command, writer: writer, owner: make(chan *conventionalMediaProcessOwner, 1),
		faultRelease: make(chan struct{}), finished: make(chan struct{}), result: make(chan backgroundCohortResult, 1), controls: controls,
	}
	originalCancel := command.Cancel
	command.Cancel = func() error {
		fixture.cancelCalls.Add(1)
		return originalCancel()
	}
	t.Cleanup(func() {
		fixture.faultOnce.Do(func() { close(fixture.faultRelease) })
		writer.unblock()
		controls()
		select {
		case <-fixture.finished:
		case <-time.After(8 * time.Second):
			t.Error("background cohort did not actually join during fixture cleanup")
		}
	})
	go func() {
		var runnerErr error
		info, receipt, probeErr := probeWithRetirement(ctx, source, func(work context.Context) (Info, error) {
			runnerErr = runBackgroundProcess(work, command, func() error {
				// The production gateway must have captured its exact owner before
				// this callback worker starts, including abnormal callback exits.
				fixture.owner <- backgroundCohortOwnerFor(command)
				<-fixture.faultRelease
				return fault()
			}, governor, unknown)
			// Deliberately discard the semantic error. Actual gateway events must
			// still make the opaque outer receipt and returned error truthful.
			return Info{Container: "claimed-background-probe-success"}, nil
		})
		fixture.result <- backgroundCohortResult{info: info, receipt: receipt, probeErr: probeErr, runnerErr: runnerErr}
		close(fixture.finished)
	}()
	return fixture
}

func backgroundCohortReady(t *testing.T, fixture *backgroundCohortFixture, source *os.File, mode string, requireSourceFD bool) *conventionalMediaProcessOwner {
	t.Helper()
	owner := retirementCohortAwait(t, fixture.owner)
	if owner == nil {
		t.Fatal("background callback ran before its actual kernel owner was captured")
	}
	ready := retirementCohortAwait(t, fixture.writer.sink.ready)
	if ready.mode != mode || ready.data != retirementCohortSourceBytes || ready.pid != fixture.command.Process.Pid || ready.group != ready.pid {
		t.Fatal("background helper did not read its actual source FD in the owned group")
	}
	if requireSourceFD {
		retirementCohortAssertSourceFD(t, source, ready.pid)
	}
	conventionalMediaOwners.mu.Lock()
	owner.mu.Lock()
	held := owner.registered && owner.pin >= 0 && !owner.joined && conventionalMediaOwners.entries[owner.slot] == owner &&
		owner.identity.pid == ready.pid && owner.identity.group == ready.group
	owner.mu.Unlock()
	conventionalMediaOwners.mu.Unlock()
	if !held || owner.process.probeChild == nil {
		t.Fatal("background source reader lost its exact kernel or automatic cohort owner")
	}
	cohort := owner.process.probeChild.cohort
	cohort.mu.Lock()
	borrowed := cohort.source == source && cohort.claims == 1 && len(cohort.pending) == 1 && owner.process.probeChild.hasStarted
	cohort.mu.Unlock()
	if !borrowed {
		t.Fatal("background gateway did not attach the actual borrowed source before Start")
	}
	return owner
}

func backgroundCohortAwaitState(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("background cohort did not reach %s", description)
		}
	}
}

func backgroundCohortAssertDetached(t *testing.T, owner *conventionalMediaProcessOwner) {
	t.Helper()
	conventionalMediaOwners.mu.Lock()
	owner.mu.Lock()
	detached := owner.joined && owner.signalsRetired && !owner.registered && owner.pin == -1
	for _, entry := range conventionalMediaOwners.entries {
		detached = detached && entry != owner
	}
	owner.mu.Unlock()
	conventionalMediaOwners.mu.Unlock()
	if !detached {
		t.Fatal("actual background group and copier join retained its kernel registry or pidfd")
	}
}

func backgroundCohortAssertResult(t *testing.T, fixture *backgroundCohortFixture, owner *conventionalMediaProcessOwner, result backgroundCohortResult, governor *mediaProcessGovernor, unknown *atomic.Uint64, failure error) {
	t.Helper()
	if !errors.Is(result.runnerErr, ErrProcessRetirementUnknown) || failure != nil && !errors.Is(result.runnerErr, failure) {
		t.Fatalf("actual background cleanup erased its original retirement fault: %v", result.runnerErr)
	}
	if result.info.Container != "claimed-background-probe-success" || !errors.Is(result.probeErr, ErrProcessRetirementUnknown) ||
		result.receipt == nil || !result.receipt.RetirementComplete() || !result.receipt.UnknownObserved() {
		t.Fatalf("semantic success hid the actual background cohort fault: info=%+v error=%v", result.info, result.probeErr)
	}
	retirementCohortAssertReaped(t, fixture.command)
	backgroundCohortAssertDetached(t, owner)
	want := ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}
	if stats := processCapacityStatsFor(governor, unknown); stats != want {
		t.Fatalf("actual kernel join released the separate outer unknown charge: %+v", stats)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := result.receipt.Close(ctx); !errors.Is(err, ErrProcessRetirementUnknown) || !result.receipt.RetirementComplete() || !result.receipt.UnknownObserved() {
		t.Fatalf("joined receipt cleanup cleared its sticky background fault: %v", err)
	}
	if stats := processCapacityStatsFor(governor, unknown); stats != want {
		t.Fatalf("repeated receipt cleanup released the retained outer charge: %+v", stats)
	}
	if calls := fixture.cancelCalls.Load(); calls != 1 {
		t.Fatalf("background callback fault invoked the original cancellation %d times", calls)
	}
	if err := fixture.command.Cancel(); !errors.Is(err, os.ErrProcessDone) || fixture.cancelCalls.Load() != 1 {
		t.Fatalf("joined background command forwarded a post-reap group signal: %v", err)
	}
}

func TestBackgroundProbeCohortFaultsJoinActualSourceAndCopier(t *testing.T) {
	failure := errors.New("controlled background cohort retirement failure")
	for _, test := range []struct {
		name    string
		fault   func() error
		failure error
	}{
		{"callback error", func() error { return failure }, failure},
		{"callback nil panic", func() error { panic(nil) }, nil},
		{"callback Goexit", func() error { runtime.Goexit(); return nil }, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			source := retirementCohortSource(t)
			governor := newMediaProcessAdmission(1, 1, 8)
			var unknown atomic.Uint64
			fixture := backgroundCohortStart(t, ctx, source, governor, &unknown, "live", true, test.fault)
			owner := backgroundCohortReady(t, fixture, source, "live", true)
			retirementCohortAwait(t, fixture.writer.entered)
			mediaProcessAdmissionTestWait(t, governor, 1, 1, 0)
			fixture.faultOnce.Do(func() { close(fixture.faultRelease) })
			backgroundCohortAwaitState(t, "observable outer retirement fault", func() bool {
				return unknown.Load() == 1
			})
			// Actual procfs disappearance fixes the exec.Wait/copier window.
			// Neither the callback error nor a timeout authorizes this assertion.
			backgroundCohortAwaitState(t, "reaped leader behind its blocked copier", func() bool {
				_, _, err := readConventionalProcessIdentity(owner.identity.pid)
				return analysisProcessTestDisappeared(err)
			})
			conventionalMediaOwners.mu.Lock()
			owner.mu.Lock()
			held := owner.registered && owner.pin >= 0 && owner.signalsRetired && !owner.joined && conventionalMediaOwners.entries[owner.slot] == owner
			owner.mu.Unlock()
			conventionalMediaOwners.mu.Unlock()
			if !held {
				t.Fatal("blocked actual copier lost its pinned background kernel owner")
			}
			cohort := owner.process.probeChild.cohort
			cohort.mu.Lock()
			pending := cohort.unknown && !cohort.returned && len(cohort.pending) == 1
			cohort.mu.Unlock()
			if !pending || processCapacityStatsFor(governor, &unknown) != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
				t.Fatal("blocked actual copier retired its cohort or returned the outer unknown charge")
			}
			select {
			case result := <-fixture.result:
				t.Fatalf("background runner returned before actual copier completion: %v", result.runnerErr)
			default:
			}
			fixture.writer.unblock()
			result := retirementCohortAwait(t, fixture.result)
			backgroundCohortAssertResult(t, fixture, owner, result, governor, &unknown, test.failure)
		})
	}
}

func TestBackgroundProbeCohortSequentialUnknownChargesDoNotFillKernelRegistry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source := retirementCohortSource(t)
	before := GetProcessCapacityStats()
	failure := errors.New("sequential private background cohort fault")
	governors := make([]*mediaProcessGovernor, 0, mediaProcessOwnerLimit+2)
	counters := make([]*atomic.Uint64, 0, mediaProcessOwnerLimit+2)
	for index := 0; index < mediaProcessOwnerLimit+2; index++ {
		// Each independent governor intentionally retains its unknown charge.
		// A shared governor or a manual reset would obscure a global registry leak.
		governor := newMediaProcessAdmission(1, 1, 8)
		unknown := &atomic.Uint64{}
		governors = append(governors, governor)
		counters = append(counters, unknown)
		fixture := backgroundCohortStart(t, ctx, source, governor, unknown, "emit-exit", false, func() error { return failure })
		owner := backgroundCohortReady(t, fixture, source, "emit-exit", false)
		fixture.faultOnce.Do(func() { close(fixture.faultRelease) })
		result := retirementCohortAwait(t, fixture.result)
		backgroundCohortAssertResult(t, fixture, owner, result, governor, unknown, failure)
	}
	output, err := runLimited(ctx, 5*time.Second, 4096, "/bin/sh", "-c", "printf background-registry-restored")
	if err != nil || string(output) != "background-registry-restored" {
		t.Fatalf("independent private faults polluted the actual foreground process registry: output=%q error=%v", output, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err = runLimitedFiles(ctx, 5*time.Second, 4096, executable, []*os.File{source}, "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", "probe-json")
	var decoded struct {
		Format struct {
			Name string `json:"format_name"`
			Size string `json:"size"`
		} `json:"format"`
	}
	if err != nil || json.Unmarshal(output, &decoded) != nil || decoded.Format.Name != "mov,mp4" || decoded.Format.Size != strconv.Itoa(len(retirementCohortSourceBytes)) {
		t.Fatalf("actual source-bearing limited process failed after private background faults: error=%v output=%q", err, output)
	}
	if after := GetProcessCapacityStats(); after != before {
		t.Fatalf("private callback faults changed global process admission: before=%+v after=%+v", before, after)
	}
	for index, governor := range governors {
		if stats := processCapacityStatsFor(governor, counters[index]); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
			t.Fatalf("successful independent processes cleared private unknown charge %d: %+v", index, stats)
		}
	}
}
