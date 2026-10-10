//go:build linux

package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const captureRecoveryHelperEnvironment = "GOBY_MEDIA_CAPTURE_RECOVERY_HELPER"

// Descriptor limits and subreaper state belong only to this helper process.
// The tested child still uses the production capture, retry, fence and Wait.
func TestMediaProcessInitialCaptureRecovery(t *testing.T) {
	for _, scenario := range []string{"double-live-cancel", "double-exited", "double-copier", "double-descendant", "pin-only", "metadata-only"} {
		t.Run(scenario, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.v", "-test.run=^TestMediaProcessInitialCaptureRecoveryHelper$", "--", scenario)
			command.Env = append(os.Environ(), captureRecoveryHelperEnvironment+"=1")
			command.WaitDelay = 2 * time.Second
			output, err := command.CombinedOutput()
			var exited *exec.ExitError
			if errors.As(err, &exited) && exited.ExitCode() == 77 {
				t.Skip("the original Go process handle is unavailable on this Linux profile")
			}
			if err != nil {
				t.Fatalf("capture recovery helper failed: %v\n%s", err, output)
			}
		})
	}
}

func TestMediaProcessInitialCaptureRecoveryHelper(t *testing.T) {
	if os.Getenv(captureRecoveryHelperEnvironment) != "1" {
		return
	}
	scenario := ""
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			scenario = os.Args[index+1]
			break
		}
	}
	if scenario == "" {
		t.Fatal("capture recovery scenario is missing")
	}
	if strings.HasPrefix(scenario, "double-") {
		// Check the precise recovery prerequisite before creating any children.
		self, err := os.FindProcess(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		err = self.WithHandle(func(uintptr) {})
		_ = self.Release()
		if errors.Is(err, os.ErrNoHandle) {
			os.Exit(77)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "double-descendant" {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	governor := newMediaProcessAdmission(1, 1, 8)
	source := retirementCohortSource(t)
	descendantPID := 0
	// Register before the leader fixture so cleanup first joins the leader,
	// then reaps any descendant adopted by this helper's subreaper.
	t.Cleanup(func() {
		if descendantPID > 0 {
			captureRecoveryReapDescendant(t, descendantPID, true)
		}
	})
	mode := "live"
	if scenario == "double-copier" || scenario == "double-exited" {
		mode = "emit-exit"
	} else if scenario == "double-descendant" {
		mode = "orphan-leader"
	}
	fixture, retire, releases := captureRecoveryStart(t, ctx, source, governor, mode)
	ready := captureRecoveryReady(t, fixture, source, scenario == "double-descendant")
	descendantPID = ready.child
	if scenario == "double-copier" || scenario == "double-exited" {
		retirementCohortAwait(t, fixture.sink.entered)
		if err := waitMediaProcessWithoutReaping(ready.pid); err != nil {
			t.Fatal(err)
		}
	}
	var captureErr error
	if strings.HasPrefix(scenario, "double-") {
		captureErr = captureRecoveryExhaustDescriptors(t, fixture.process)
	} else {
		captureErr = fixture.process.ensureConventionalOwnerWithCapture(func(command *exec.Cmd) (conventionalProcessIdentity, int, error) {
			if scenario == "pin-only" {
				return captureConventionalProcessWith(command, func(int) (int, error) { return -1, syscall.EMFILE }, readConventionalProcessIdentity)
			}
			return captureConventionalProcessWith(command, openConventionalProcessPin, func(int) (conventionalProcessIdentity, byte, error) {
				return conventionalProcessIdentity{}, 0, syscall.EMFILE
			})
		})
		owner := fixture.process.conventional
		if scenario == "pin-only" && (owner.pin != -1 || owner.identity.start == 0) {
			t.Fatal("pin-only failure did not retain its observed identity")
		}
		if scenario == "metadata-only" && (owner.pin < 0 || owner.identity.start != 0) {
			t.Fatal("metadata-only failure did not retain its original kernel pin")
		}
	}
	if !errors.Is(captureErr, syscall.EMFILE) || !errors.Is(captureErr, ErrProcessRetirementUnknown) {
		t.Fatalf("initial capture did not expose its descriptor failure: %v", captureErr)
	}
	if err := fixture.process.ensureConventionalOwner(); err != captureErr {
		t.Fatal("re-entering owner capture replaced the original failure")
	}
	if scenario == "double-exited" {
		fixture.sink.unblock()
	}
	go func() { _ = retire() }()
	if scenario == "double-live-cancel" || scenario == "double-descendant" || scenario == "pin-only" || scenario == "metadata-only" {
		cancel()
	}
	done := make(chan error, 1)
	go func() { done <- fixture.process.Close() }()
	if scenario == "double-copier" {
		captureRecoveryWaitForSignalFence(t, fixture.process.conventional)
		if releases.Load() != 0 {
			t.Fatal("capture recovery released capacity before the stdout copier joined")
		}
		select {
		case err := <-done:
			t.Fatalf("Close escaped the blocked stdout copier: %v", err)
		default:
		}
		fixture.sink.unblock()
	}
	closeErr := retirementCohortAwait(t, done)
	if !errors.Is(closeErr, captureErr) || !errors.Is(closeErr, ErrProcessRetirementUnknown) {
		t.Fatalf("successful retirement hid the initial capture failure: %v", closeErr)
	}
	retirementCohortAssertReaped(t, fixture.command)
	owner := fixture.process.conventional
	if !owner.joined || owner.registered || owner.pin != -1 || releases.Load() != 1 {
		t.Fatalf("capture recovery did not retire exactly one kernel owner and permit: joined=%v registered=%v pin=%d releases=%d", owner.joined, owner.registered, owner.pin, releases.Load())
	}
	if fixture.process.Close() != closeErr || releases.Load() != 1 {
		t.Fatal("repeated Close changed the result or returned capacity twice")
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	if scenario == "double-descendant" {
		captureRecoveryReapDescendant(t, ready.child, false)
	}
}

func captureRecoveryStart(t *testing.T, ctx context.Context, source *os.File, governor *mediaProcessGovernor, mode string) (*retirementCohortFixture, func() error, *atomic.Int32) {
	t.Helper()
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childRead, childWrite, err := os.Pipe()
	if err != nil {
		_ = controlRead.Close()
		_ = controlWrite.Close()
		t.Fatal(err)
	}
	var controlsOnce sync.Once
	controls := func() {
		controlsOnce.Do(func() {
			_ = controlRead.Close()
			_ = controlWrite.Close()
			_ = childRead.Close()
			_ = childWrite.Close()
		})
	}
	t.Cleanup(controls)
	release, err := governor.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	releases := &atomic.Int32{}
	executable, err := os.Executable()
	if err != nil {
		release()
		t.Fatal(err)
	}
	sink := retirementCohortNewSink(mode == "emit-exit")
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", mode)
	command.Env = append(os.Environ(), retirementCohortHelperEnvironment+"=1")
	command.ExtraFiles = []*os.File{source, controlRead, childRead}
	command.Stdout = sink
	retire := configureMediaProcess(command)
	if err := command.Start(); err != nil {
		release()
		t.Fatal(err)
	}
	retired := make(chan error, 1)
	process := &mediaProcess{command: command, retired: retired, release: func() { releases.Add(1); release() }}
	fixture := &retirementCohortFixture{process: process, command: command, sink: sink, control: controlWrite, childControl: childWrite, releaseControls: controls}
	var retireOnce sync.Once
	var retireErr error
	retireOwned := func() error {
		retireOnce.Do(func() { retireErr = retire(); retired <- retireErr })
		return retireErr
	}
	t.Cleanup(func() {
		sink.unblock()
		controls()
		_ = command.Process.Kill()
		joined := make(chan struct{})
		go func() { _ = retireOwned(); _ = process.Close(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("the capture-recovery fixture did not join its original child")
		}
	})
	return fixture, retireOwned, releases
}

func captureRecoveryReady(t *testing.T, fixture *retirementCohortFixture, source *os.File, descendant bool) retirementCohortReady {
	t.Helper()
	want := 1
	if descendant {
		want = 2
	}
	var leader retirementCohortReady
	for range want {
		ready := retirementCohortAwait(t, fixture.sink.ready)
		if ready.data != retirementCohortSourceBytes || ready.group != fixture.command.Process.Pid {
			t.Fatal("the helper did not read the original source in its owned process group")
		}
		if ready.mode != "emit-exit" {
			retirementCohortAssertSourceFD(t, source, ready.pid)
		}
		if ready.pid == fixture.command.Process.Pid {
			leader = ready
		}
	}
	if leader.pid == 0 || descendant && leader.child <= 0 {
		t.Fatal("the actual direct child or descendant identity is missing")
	}
	return leader
}

func captureRecoveryReapDescendant(t *testing.T, child int, allowReaped bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(child, &status, syscall.WNOHANG, nil)
		if pid == child && err == nil || allowReaped && errors.Is(err, syscall.ECHILD) {
			return
		}
		if err != nil {
			t.Errorf("the source-reading descendant could not be reaped: pid=%d err=%v", pid, err)
			return
		}
		select {
		case <-deadline.C:
			t.Error("the source-reading descendant remained live after group retirement")
			return
		case <-tick.C:
		}
	}
}

func captureRecoveryExhaustDescriptors(t *testing.T, process *mediaProcess) error {
	t.Helper()
	var original unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	maximum := 0
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err == nil && fd > maximum {
			maximum = fd
		}
	}
	limit := uint64(maximum + 32)
	if limit > 4096 || original.Cur < limit {
		t.Fatal("the isolated descriptor-pressure fixture has insufficient bounded headroom")
	}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: limit, Max: original.Max}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
			t.Error(err)
		}
	}()
	held := make([]int, 0, int(limit))
	defer func() {
		for _, fd := range held {
			_ = unix.Close(fd)
		}
	}()
	var exhaustion error
	for {
		fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			exhaustion = err
			break
		}
		held = append(held, fd)
	}
	captureErr := process.ensureConventionalOwner()
	owner := process.conventional
	initialPin, initialIdentity := owner.pin, owner.identity
	retryErr := owner.retryCapture()
	noCapacityPin := owner.pin
	// One free FD permits the original-handle duplicate, but not fdinfo. The
	// retained duplicate must survive the failed metadata read without growing.
	last := len(held) - 1
	_ = unix.Close(held[last])
	held = held[:last]
	metadataErr := owner.retryCapture()
	retainedPin := owner.pin
	secondErr := owner.retryCapture()
	stablePin := owner.pin
	for _, fd := range held {
		_ = unix.Close(fd)
	}
	held = nil
	if !errors.Is(exhaustion, syscall.EMFILE) || !errors.Is(captureErr, syscall.EMFILE) || initialPin != -1 || initialIdentity != (conventionalProcessIdentity{}) {
		t.Fatalf("actual descriptor exhaustion did not fail both initial captures: exhaustion=%v capture=%v pin=%d identity=%+v", exhaustion, captureErr, initialPin, initialIdentity)
	}
	if !errors.Is(retryErr, syscall.EMFILE) || noCapacityPin != -1 {
		t.Fatalf("a failed original-handle duplicate changed ownership: err=%v pin=%d", retryErr, noCapacityPin)
	}
	if !errors.Is(metadataErr, syscall.EMFILE) || !errors.Is(secondErr, syscall.EMFILE) || retainedPin < 0 || stablePin != retainedPin {
		t.Fatalf("failed metadata retries did not retain exactly one original pin: first=%v second=%v pins=%d/%d", metadataErr, secondErr, retainedPin, stablePin)
	}
	return captureErr
}

func captureRecoveryWaitForSignalFence(t *testing.T, owner *conventionalMediaProcessOwner) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		owner.mu.Lock()
		retired := owner.signalsRetired
		owner.mu.Unlock()
		if retired {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("the actual group signal fence did not complete")
		case <-tick.C:
		}
	}
}

func TestMediaProcessCaptureWithoutOriginalHandleNeverReopensPID(t *testing.T) {
	owner := &conventionalMediaProcessOwner{pin: -1, process: &mediaProcess{command: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}}}
	err := owner.retryCapture()
	if !errors.Is(err, os.ErrNoHandle) || !errors.Is(err, ErrProcessRetirementUnknown) || owner.pin != -1 || owner.identity != (conventionalProcessIdentity{}) {
		t.Fatalf("a bare numeric PID was accepted as original capture authority: err=%v pin=%d identity=%+v", err, owner.pin, owner.identity)
	}
}
