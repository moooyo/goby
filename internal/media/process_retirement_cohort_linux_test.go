//go:build linux

package media

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
	"golang.org/x/sys/unix"
)

const retirementCohortSourceBytes = "actual retirement source FD"
const retirementCohortHelperEnvironment = "GOBY_MEDIA_RETIREMENT_COHORT_HELPER"

// The helper reads an inherited real source descriptor before reporting ready.
// Separate control pipes keep leader exit, source readers, and copier gates
// independent; a fake retirement callback never supplies these OS observations.
func TestMediaProcessRetirementCohortHelper(t *testing.T) {
	mode := ""
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
			break
		}
	}
	if mode == "" || os.Getenv(retirementCohortHelperEnvironment) != "1" && mode != "probe-json" {
		return
	}
	source := os.NewFile(3, "retirement-source")
	if source == nil {
		os.Exit(31)
	}
	if mode == "supervisor-orphan" {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		retirementCohortExerciseOrphan(t, source)
		return
	}
	data := make([]byte, len(retirementCohortSourceBytes))
	if n, err := source.ReadAt(data, 0); err != nil || n != len(data) || string(data) != retirementCohortSourceBytes {
		os.Exit(32)
	}
	if mode == "probe-json" {
		info, err := source.Stat()
		if err != nil {
			os.Exit(33)
		}
		output := map[string]any{
			"format":  map[string]any{"format_name": "mov,mp4", "duration": "1.5", "size": strconv.FormatInt(info.Size(), 10), "bit_rate": "128000"},
			"streams": []map[string]any{{"index": 0, "codec_name": "h264", "codec_type": "video", "width": 16, "height": 16}},
		}
		if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
			os.Exit(34)
		}
		os.Exit(0)
	}
	control := os.NewFile(4, "retirement-control")
	if control == nil {
		os.Exit(35)
	}
	child := 0
	if mode == "orphan-leader" {
		childControl := os.NewFile(5, "retirement-child-control")
		command := exec.Command(os.Args[0], "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", "orphan-child")
		command.Env = append(os.Environ(), retirementCohortHelperEnvironment+"=1")
		command.ExtraFiles = []*os.File{source, childControl}
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		// Inherit the actual leader's PGID and session, rather than inventing a
		// synthetic group or a second callback certificate.
		if err := command.Start(); err != nil {
			os.Exit(36)
		}
		child = command.Process.Pid
	}
	fmt.Fprintf(os.Stdout, "cohort-ready %s %d %d %d %x\n", mode, os.Getpid(), syscall.Getpgrp(), child, data)
	if mode == "orphan-child" {
		_ = os.Stdout.Close()
		_ = os.Stderr.Close()
	}
	if mode == "emit-exit" {
		fmt.Fprintln(os.Stdout, "cohort-payload")
		os.Exit(0)
	}
	if mode != "live" && mode != "orphan-leader" && mode != "orphan-child" {
		os.Exit(37)
	}
	var signal [1]byte
	_, _ = control.Read(signal[:])
	os.Exit(0)
}

type retirementCohortReady struct {
	mode              string
	pid, group, child int
	data              string
}

type retirementCohortSink struct {
	mu          sync.Mutex
	line        []byte
	ready       chan retirementCohortReady
	block       bool
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
	unblockOnce sync.Once
}

func retirementCohortNewSink(block bool) *retirementCohortSink {
	return &retirementCohortSink{ready: make(chan retirementCohortReady, 4), block: block, entered: make(chan struct{}), release: make(chan struct{})}
}

func (sink *retirementCohortSink) unblock() {
	sink.unblockOnce.Do(func() { close(sink.release) })
}

func (sink *retirementCohortSink) Write(data []byte) (int, error) {
	blocked := false
	sink.mu.Lock()
	for _, value := range data {
		if len(sink.line) >= 512 {
			sink.mu.Unlock()
			return 0, errors.New("retirement helper record exceeded its bound")
		}
		sink.line = append(sink.line, value)
		if value != '\n' {
			continue
		}
		line := string(sink.line)
		sink.line = nil
		if strings.HasPrefix(line, "cohort-ready ") {
			var ready retirementCohortReady
			var encoded string
			if n, err := fmt.Sscanf(line, "cohort-ready %s %d %d %d %s", &ready.mode, &ready.pid, &ready.group, &ready.child, &encoded); err != nil || n != 5 {
				sink.mu.Unlock()
				return 0, errors.New("invalid retirement helper readiness record")
			}
			decoded, err := hex.DecodeString(encoded)
			if err != nil {
				sink.mu.Unlock()
				return 0, err
			}
			ready.data = string(decoded)
			sink.ready <- ready
		} else if line == "cohort-payload\n" && sink.block {
			blocked = true
		}
	}
	sink.mu.Unlock()
	if blocked {
		sink.once.Do(func() { close(sink.entered) })
		<-sink.release
	}
	return len(data), nil
}

type retirementCohortFixture struct {
	process               *mediaProcess
	command               *exec.Cmd
	sink                  *retirementCohortSink
	control, childControl *os.File
	releaseControls       func()
}

func retirementCohortSource(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cohort-source.mp4")
	if err := os.WriteFile(path, []byte(retirementCohortSourceBytes), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func retirementCohortStart(t *testing.T, ctx context.Context, source *os.File, governor *mediaProcessGovernor, mode string, callbackErr error, block bool, attach bool) *retirementCohortFixture {
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
	releaseControls := func() {
		controlsOnce.Do(func() {
			_ = controlWrite.Close()
			_ = childWrite.Close()
			_ = controlRead.Close()
			_ = childRead.Close()
		})
	}
	release, err := governor.acquire(ctx)
	if err != nil {
		releaseControls()
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		release()
		releaseControls()
		t.Fatal(err)
	}
	sink := retirementCohortNewSink(block)
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", mode)
	command.Env = append(os.Environ(), retirementCohortHelperEnvironment+"=1")
	command.ExtraFiles = []*os.File{source, controlRead, childRead}
	command.Stdout, command.Stderr = sink, io.Discard
	_ = configureMediaProcess(command)
	if err := command.Start(); err != nil {
		release()
		releaseControls()
		t.Fatal(err)
	}
	retired := make(chan error, 1)
	retired <- callbackErr
	process := &mediaProcess{command: command, retired: retired, release: release}
	fixture := &retirementCohortFixture{process: process, command: command, sink: sink, control: controlWrite, childControl: childWrite, releaseControls: releaseControls}
	t.Cleanup(func() {
		sink.unblock()
		releaseControls()
		joined := make(chan struct{})
		go func() { _ = process.Close(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(8 * time.Second):
			t.Error("retirement fixture did not actually join during cleanup")
		}
	})
	if err := process.ensureConventionalOwner(); err != nil {
		t.Fatal(err)
	}
	if attach {
		if err := process.attachProbeRetirement(ctx); err != nil {
			t.Fatal(err)
		}
		process.probeChild.started()
	}
	return fixture
}

func retirementCohortAwait[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(8 * time.Second):
		t.Fatal("actual retirement fixture missed its completion barrier")
		var zero T
		return zero
	}
}

func retirementCohortReadyFor(t *testing.T, fixture *retirementCohortFixture, mode string) retirementCohortReady {
	t.Helper()
	for {
		ready := retirementCohortAwait(t, fixture.sink.ready)
		if ready.data != retirementCohortSourceBytes || ready.pid <= 1 || ready.group != fixture.command.Process.Pid {
			t.Fatal("retirement helper did not read its actual source FD in the owned group")
		}
		if ready.mode == mode {
			return ready
		}
	}
}

func retirementCohortAssertSourceFD(t *testing.T, source *os.File, pid int) {
	t.Helper()
	expected, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "fd", "3"))
	if err != nil || !os.SameFile(expected, actual) {
		t.Fatalf("actual child did not retain the borrowed source inode: %v", err)
	}
}

func retirementCohortAssertHeld(t *testing.T, fixture *retirementCohortFixture) {
	t.Helper()
	owner := fixture.process.conventional
	if owner == nil {
		t.Fatal("actual conventional owner was not registered")
	}
	conventionalMediaOwners.mu.Lock()
	owner.mu.Lock()
	held := owner.registered && owner.pin >= 0 && !owner.joined && conventionalMediaOwners.entries[owner.slot] == owner
	owner.mu.Unlock()
	conventionalMediaOwners.mu.Unlock()
	if !held || fixture.command.ProcessState != nil {
		t.Fatal("unproved retirement lost its exact registry, pidfd, or unreaped leader")
	}
}

func retirementCohortAssertReaped(t *testing.T, command *exec.Cmd) {
	t.Helper()
	var status syscall.WaitStatus
	if pid, err := syscall.Wait4(command.Process.Pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("production cleanup did not perform the actual leader Wait: pid=%d err=%v", pid, err)
	}
	if command.ProcessState == nil {
		t.Fatal("actual cleanup did not publish joined leader state")
	}
}

func TestMediaProcessRetirementCallbackFaultKeepsLeaderAndPermit(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		callback   error
		waitable   bool
	}{
		{"callback error with live leader", "live", errors.New("controlled retirement callback error"), false},
		{"nil callback with live leader", "live", nil, false},
		{"callback error with waitable leader", "live", errors.New("controlled exited-leader callback error"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			governor := newMediaProcessAdmission(1, 1, 8)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			source := retirementCohortSource(t)
			fixture := retirementCohortStart(t, ctx, source, governor, test.mode, test.callback, false, false)
			ready := retirementCohortReadyFor(t, fixture, test.mode)
			retirementCohortAssertSourceFD(t, source, ready.pid)
			if test.waitable {
				if _, err := fixture.control.Write([]byte("E")); err != nil {
					t.Fatal(err)
				}
				if err := waitMediaProcessWithoutReaping(ready.pid); err != nil {
					t.Fatal(err)
				}
			} else if waitable, err := conventionalLeaderWaitable(fixture.process.conventional.identity); err != nil || waitable {
				t.Fatalf("live source reader was not held behind its kernel control gate: waitable=%v err=%v", waitable, err)
			}
			waited := make(chan error, 1)
			go func() { waited <- fixture.process.Wait() }()
			waitErr := retirementCohortAwait(t, waited)
			if !errors.Is(waitErr, ErrProcessRetirementUnknown) || test.callback != nil && !errors.Is(waitErr, test.callback) {
				t.Fatalf("callback or live-group uncertainty was reported as retirement: %v", waitErr)
			}
			retirementCohortAssertHeld(t, fixture)
			mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
			queued := mediaProcessAdmissionTestQueue(governor, ctx)
			mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
			closeErr := fixture.process.Close()
			if !errors.Is(closeErr, ErrProcessRetirementUnknown) || test.callback != nil && !errors.Is(closeErr, test.callback) {
				t.Fatalf("actual cleanup erased the first retirement fault: %v", closeErr)
			}
			retirementCohortAssertReaped(t, fixture.command)
			acquired := mediaProcessAdmissionTestReceive(t, queued)
			if acquired.err != nil || acquired.release == nil {
				t.Fatalf("actual cleanup did not return held media capacity: %v", acquired.err)
			}
			acquired.release()
			mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
			if fixture.process.Wait() != waitErr || fixture.process.Close() != closeErr {
				t.Fatal("repeated Wait or Close changed its shared sticky result")
			}
		})
	}
}

func TestMediaProcessRetirementFencesExitedLeaderSourceFDChild(t *testing.T) {
	// The supervisor alone becomes a subreaper. Main-suite process ownership is
	// untouched, and only the adopted orphan PID is explicitly reaped there.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	source := retirementCohortSource(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMediaProcessRetirementCohortHelper$", "--", "supervisor-orphan")
	command.Env = append(os.Environ(), retirementCohortHelperEnvironment+"=1")
	command.ExtraFiles = []*os.File{source}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		t.Fatalf("actual orphan-FD supervisor failed: %v; %s", err, output.Bytes())
	}
}

func retirementCohortExerciseOrphan(t *testing.T, source *os.File) {
	governor := newMediaProcessAdmission(1, 1, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := retirementCohortStart(t, ctx, source, governor, "orphan-leader", nil, false, false)
	var leader, child retirementCohortReady
	for leader.pid == 0 || child.pid == 0 {
		ready := retirementCohortAwait(t, fixture.sink.ready)
		if ready.data != retirementCohortSourceBytes || ready.group != fixture.command.Process.Pid {
			t.Fatal("orphan readiness lacked a real source read and inherited process group")
		}
		if ready.mode == "orphan-leader" {
			leader = ready
		} else if ready.mode == "orphan-child" {
			child = ready
		}
	}
	if leader.child != child.pid {
		t.Fatal("actual leader did not report its source-FD child")
	}
	retirementCohortAssertSourceFD(t, source, child.pid)
	if _, err := fixture.control.Write([]byte("E")); err != nil {
		t.Fatal(err)
	}
	if err := waitMediaProcessWithoutReaping(leader.pid); err != nil {
		t.Fatal(err)
	}
	identity, state, err := readConventionalProcessIdentity(child.pid)
	if err != nil || identity.parent != os.Getpid() || state == 'Z' || state == 'X' {
		t.Fatalf("source reader did not become a live, adopted orphan: identity=%+v state=%c err=%v", identity, state, err)
	}
	retirementCohortAssertSourceFD(t, source, child.pid)
	if err := fixture.process.Wait(); err != nil {
		t.Fatalf("independent actual group fence did not retire the inherited source reader: %v", err)
	}
	retirementCohortAssertReaped(t, fixture.command)
	deadline := time.NewTimer(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(child.pid, &status, syscall.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid == child.pid {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("kernel group fence did not terminate the adopted source reader")
		}
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(child.pid))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adopted source reader remained after its actual Wait: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessRetirementBlockedCopierSharesConcurrentJoin(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	source := retirementCohortSource(t)
	fixture := retirementCohortStart(t, ctx, source, governor, "emit-exit", nil, true, false)
	retirementCohortReadyFor(t, fixture, "emit-exit")
	retirementCohortAwait(t, fixture.sink.entered)
	results := make(chan error, 17)
	go func() { results <- fixture.process.Wait() }()
	// ECHILD fixes the real exec.Wait/copier window without using a sleep as a
	// certificate. Wait cannot finish while the actual writer is gated.
	deadline := time.NewTimer(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		_, _, err := readConventionalProcessIdentity(fixture.command.Process.Pid)
		if analysisProcessTestDisappeared(err) {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("leader was not actually reaped before the gated copier join")
		}
	}
	queued := mediaProcessAdmissionTestQueue(governor, ctx)
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
	for range 8 {
		go func() { results <- fixture.process.Wait() }()
		go func() { results <- fixture.process.Close() }()
	}
	select {
	case err := <-results:
		t.Fatalf("Wait/Close returned media capacity before the actual copier joined: %v", err)
	default:
	}
	fixture.sink.unblock()
	for range 17 {
		if err := retirementCohortAwait(t, results); err != nil {
			t.Fatalf("shared actual join introduced a false retirement fault: %v", err)
		}
	}
	acquired := mediaProcessAdmissionTestReceive(t, queued)
	if acquired.err != nil || acquired.release == nil {
		t.Fatal("copier completion did not admit the queued successor")
	}
	acquired.release()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	retirementCohortAssertReaped(t, fixture.command)
	if fixture.process.Wait() != nil || fixture.process.Close() != nil {
		t.Fatal("repeated normal joins changed their shared successful result")
	}
}

func TestProbeRetirementCohortTwoActualChildrenKeepsStickyUnknown(t *testing.T) {
	governor := newMediaProcessAdmission(2, 1, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source := retirementCohortSource(t)
	failure := errors.New("controlled second probe-child retirement fault")
	var second *retirementCohortFixture
	var firstWaitErr error
	info, receipt, err := probeWithRetirement(ctx, source, func(work context.Context) (Info, error) {
		first := retirementCohortStart(t, work, source, governor, "live", nil, false, true)
		second = retirementCohortStart(t, work, source, governor, "live", failure, false, true)
		firstReady := retirementCohortReadyFor(t, first, "live")
		secondReady := retirementCohortReadyFor(t, second, "live")
		retirementCohortAssertSourceFD(t, source, firstReady.pid)
		retirementCohortAssertSourceFD(t, source, secondReady.pid)
		if _, err := first.control.Write([]byte("E")); err != nil {
			t.Fatal(err)
		}
		if err := waitMediaProcessWithoutReaping(firstReady.pid); err != nil {
			t.Fatal(err)
		}
		if err := first.process.Wait(); err != nil {
			t.Fatal(err)
		}
		firstWaitErr = second.process.Wait()
		if !errors.Is(firstWaitErr, failure) || !errors.Is(firstWaitErr, ErrProcessRetirementUnknown) {
			t.Fatalf("actual second child did not retain its callback fault: %v", firstWaitErr)
		}
		// Semantic success cannot erase an actual child's unproved retirement.
		return Info{Container: "claimed-probe-success", DurationTicks: 123}, nil
	})
	if info.Container != "claimed-probe-success" || info.DurationTicks != 123 || !errors.Is(err, ErrProcessRetirementUnknown) || receipt == nil || receipt.RetirementComplete() || !receipt.UnknownObserved() {
		t.Fatalf("whole-probe receipt accepted outer Info,nil over an unknown actual child: info=%+v err=%v", info, err)
	}
	retirementCohortAssertHeld(t, second)
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
	closeCtx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	if err := receipt.Close(closeCtx); !errors.Is(err, ErrProcessRetirementUnknown) || !errors.Is(err, failure) {
		t.Fatalf("actual cohort cleanup erased the semantic fault: %v", err)
	}
	if !receipt.RetirementComplete() || !receipt.UnknownObserved() || second.process.Wait() != firstWaitErr {
		t.Fatal("actual later cleanup failed to retire resources or cleared sticky uncertainty")
	}
	retirementCohortAssertReaped(t, second.command)
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestProbeRetirementCohortCloseDeadlineKeepsBlockedCopier(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source := retirementCohortSource(t)
	failure := errors.New("controlled gated probe-child retirement fault")
	var fixture *retirementCohortFixture
	var waitErr error
	_, receipt, err := probeWithRetirement(ctx, source, func(work context.Context) (Info, error) {
		fixture = retirementCohortStart(t, work, source, governor, "emit-exit", failure, true, true)
		retirementCohortReadyFor(t, fixture, "emit-exit")
		retirementCohortAwait(t, fixture.sink.entered)
		waitErr = fixture.process.Wait()
		return Info{Container: "outer-success"}, nil
	})
	if !errors.Is(err, ErrProcessRetirementUnknown) || !errors.Is(waitErr, failure) || receipt == nil {
		t.Fatal("gated actual child did not produce an incomplete faulted cohort")
	}
	expired, stopExpired := context.WithDeadline(context.Background(), time.Now())
	defer stopExpired()
	if err := receipt.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline did not end only the cleanup wait: %v", err)
	}
	if receipt.RetirementComplete() || !receipt.UnknownObserved() {
		t.Fatal("deadline manufactured a retirement receipt while the actual writer remained owned")
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
	fixture.sink.unblock()
	joined, stopJoined := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopJoined()
	if err := receipt.Close(joined); !errors.Is(err, ErrProcessRetirementUnknown) || !errors.Is(err, failure) {
		t.Fatalf("joined actual copier discarded the original cohort fault: %v", err)
	}
	if !receipt.RetirementComplete() || !receipt.UnknownObserved() || fixture.process.Wait() != waitErr {
		t.Fatal("cleanup retry changed the original Wait result or failed to join actual resources")
	}
	retirementCohortAssertReaped(t, fixture.command)
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestProbeRetirementCohortProbeFileOwnedCallsActualFDProbe(t *testing.T) {
	source := retirementCohortSource(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// ProbeFile's fixed environment intentionally omits custom test variables.
	// A literal wrapper selects the real FD-reading helper through arguments.
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	tool := analysisProcessTestTool(t, "exec "+quoted+" -test.run='^TestMediaProcessRetirementCohortHelper$' -- probe-json \"$@\"")
	if _, err := source.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, receipt, err := (Prober{FFprobePath: tool}).ProbeFileOwned(ctx, source)
	if err != nil || receipt == nil || !receipt.RetirementComplete() || receipt.UnknownObserved() || info.Size != int64(len(retirementCohortSourceBytes)) || info.Container != "mov,mp4" || len(info.Streams) != 1 || info.Streams[0].Codec != "h264" {
		t.Fatalf("ProbeFileOwned did not use the actual FD probe and opaque retirement cohort: info=%+v err=%v", info, err)
	}
	if position, err := source.Seek(0, io.SeekCurrent); err != nil || position != 5 {
		t.Fatalf("whole-probe wrapper changed the borrowed source offset: offset=%d err=%v", position, err)
	}
	if err := receipt.Close(ctx); err != nil || !receipt.RetirementComplete() || receipt.UnknownObserved() {
		t.Fatalf("already joined whole-probe cohort failed idempotent cleanup: %v", err)
	}
	if _, err := source.Stat(); err != nil {
		t.Fatalf("probe cohort took ownership of its caller's borrowed source FD: %v", err)
	}
}

func TestProbeRetirementCohortUnsupportedCapabilityRejectsBeforeStart(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 8)
	source := retirementCohortSource(t)
	marker := filepath.Join(t.TempDir(), "must-not-spawn")
	var command *exec.Cmd
	_, receipt, err := probeWithRetirement(context.Background(), source, func(work context.Context) (Info, error) {
		command = exec.CommandContext(work, "/bin/sh", "-c", "printf spawned > \"$1\"", "capability-fixture", marker)
		command.ExtraFiles = []*os.File{source}
		process, err := startConventionalMediaProcessWithCapability(work, command, governor, false, false)
		if process != nil || command.Process != nil || !errors.Is(err, commanddomain.ErrUnavailable) {
			t.Fatalf("unsupported owned cohort crossed the pre-Start boundary: process=%v child=%v err=%v", process != nil, command.Process != nil, err)
		}
		return Info{}, err
	})
	if !errors.Is(err, commanddomain.ErrUnavailable) || command == nil || command.Process != nil {
		t.Fatalf("unsupported owned probe did not preserve its closed capability boundary: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported owned probe created its real spawn marker: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	// No child was started. Vacuous completion here is a no-spawn fact, never
	// an operating-system retirement certificate for a hypothetical child.
	if receipt == nil || !receipt.RetirementComplete() || receipt.UnknownObserved() {
		t.Fatal("pre-Start rejection retained a fabricated or incomplete child claim")
	}
}
