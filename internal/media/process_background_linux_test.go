package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestBackgroundProcessRejectsInvalidOwnership(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	for _, fixture := range []struct {
		ctx     context.Context
		command *exec.Cmd
		retire  func() error
	}{
		{nil, &exec.Cmd{}, func() error { return nil }},
		{context.Background(), nil, func() error { return nil }},
		{context.Background(), &exec.Cmd{}, nil},
		{context.Background(), &exec.Cmd{Process: &os.Process{Pid: 1}}, func() error { return nil }},
	} {
		if err := runBackgroundProcess(fixture.ctx, fixture.command, fixture.retire, governor, &unknown); !errors.Is(err, errBackgroundProcessInput) {
			t.Fatalf("invalid owner returned %v", err)
		}
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{}) {
		t.Fatalf("invalid owner consumed capacity: %+v", stats)
	}
}

func TestBackgroundProcessPreservesCommandAndJoinsOutput(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "cat")
	var output bytes.Buffer
	command.Stdin, command.Stdout = strings.NewReader("literal process input"), &output
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir, command.WaitDelay = "/", time.Second
	retire := configureMediaProcess(command)
	attributes := command.SysProcAttr
	if err := runBackgroundProcess(ctx, command, retire, governor, &unknown); err != nil {
		t.Fatal(err)
	}
	if output.String() != "literal process input" || command.ProcessState == nil || !command.ProcessState.Success() || command.SysProcAttr != attributes || command.Dir != "/" || command.WaitDelay != time.Second {
		t.Fatal("background runner changed command ownership or returned before copier completion")
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{}) {
		t.Fatalf("successful command retained capacity: %+v", stats)
	}
}

func TestBackgroundProcessStartFailureAndQueuedCancellation(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/goby-nonexistent-background-process")
	retire := configureMediaProcess(command)
	if err := runBackgroundProcess(ctx, command, retire, governor, &unknown); err == nil || command.Process != nil {
		t.Fatalf("failed executable started a child: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	owner := mediaProcessAdmissionTestAcquire(t, governor, WithBackgroundProcess(ctx))
	deadline, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	command = exec.CommandContext(deadline, "/bin/sh", "-c", "exit 0")
	retire = configureMediaProcess(command)
	if err := runBackgroundProcess(deadline, command, retire, governor, &unknown); !errors.Is(err, context.DeadlineExceeded) || command.Process != nil {
		t.Fatalf("queued deadline started a child: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 1, 0)
	owner()
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{}) {
		t.Fatalf("failed startup or queued deadline retained capacity: %+v", stats)
	}
}

func TestRetirementProcessForegroundUsesReserveWhileBackgroundIsFull(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := mediaProcessAdmissionTestAcquire(t, governor, WithBackgroundProcess(ctx))
	second := mediaProcessAdmissionTestAcquire(t, governor, WithBackgroundProcess(ctx))
	mediaProcessAdmissionTestWait(t, governor, 2, 2, 0)
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	retire := configureMediaProcess(command)
	if err := runProcessWithRetirement(ctx, command, retire, governor, &unknown); err != nil || command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("necessary foreground proof waited behind a full background budget: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 2, 2, 0)
	deadline, stop := context.WithTimeout(WithBackgroundProcess(ctx), 20*time.Millisecond)
	defer stop()
	command = exec.CommandContext(deadline, "/bin/sh", "-c", "exit 0")
	retire = configureMediaProcess(command)
	if err := runProcessWithRetirement(deadline, command, retire, governor, &unknown); !errors.Is(err, context.DeadlineExceeded) || command.Process != nil {
		t.Fatalf("background context discarded its shared budget classification: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 2, 2, 0)
	first()
	second()
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{}) {
		t.Fatalf("foreground reserve or queued background retained capacity: %+v", stats)
	}
}

func TestBackgroundProcessRetirementFailureRetainsObservableCharge(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	retire := configureMediaProcess(command)
	failure := errors.New("strict process retirement fixture failure")
	err := runBackgroundProcess(ctx, command, func() error { return errors.Join(retire(), failure) }, governor, &unknown)
	if !errors.Is(err, ErrProcessRetirementUnknown) || !errors.Is(err, failure) || command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("retirement failure was hidden by successful leader exit: %v", err)
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
		t.Fatalf("unknown retirement silently returned capacity: %+v", stats)
	}
}

func TestBackgroundProcessFaultObservableBeforeBlockedCopierJoin(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writer := &mediaProcessTestBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	var unblock sync.Once
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "printf output")
	command.Stdout, command.WaitDelay = writer, time.Second
	retire := configureMediaProcess(command)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- runBackgroundProcess(ctx, command, func() error { return errors.Join(retire(), errors.New("retirement fault fixture")) }, governor, &unknown)
		close(finished)
	}()
	t.Cleanup(func() {
		unblock.Do(func() { close(writer.release) })
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("background owner did not join after its writer was unblocked")
		}
	})
	select {
	case <-writer.entered:
	case <-ctx.Done():
		t.Fatal("output copier did not reach the blocking fixture")
	}
	deadline := time.Now().Add(5 * time.Second)
	for unknown.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("unknown retirement was hidden while the output copier remained owned")
		}
		time.Sleep(time.Millisecond)
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
		t.Fatalf("blocked copier did not retain the unknown charge: %+v", stats)
	}
	select {
	case <-done:
		t.Fatal("background runner returned before its blocked copier joined")
	default:
	}
	unblock.Do(func() { close(writer.release) })
	if err := <-done; !errors.Is(err, ErrProcessRetirementUnknown) {
		t.Fatalf("joined copier hid the retirement fault: %v", err)
	}
}

func TestBackgroundProcessRetirementPanicJoinsGroupWithoutRetryingReapedPID(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tool := analysisProcessTestTool(t, `sleep 60 &
child=$!
printf 'ready %s %s\n' "$$" "$child"
wait "$child"`)
	command := exec.CommandContext(ctx, tool)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = configureMediaProcess(command)
	cancelGroup := command.Cancel
	var cancelCalls atomic.Int64
	command.Cancel = func() error {
		cancelCalls.Add(1)
		return cancelGroup()
	}
	var ids analysisProcessTestPIDs
	const privateDetail = "private parser retirement panic contents"
	err = runBackgroundProcess(ctx, command, func() error {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			return err
		}
		if count, err := fmt.Sscanf(line, "ready %d %d\n", &ids.leader, &ids.child); err != nil || count != 2 {
			return errors.New("invalid process readiness fixture")
		}
		panic(privateDetail)
	}, governor, &unknown)
	if !errors.Is(err, ErrProcessRetirementUnknown) || strings.Contains(err.Error(), privateDetail) || cancelCalls.Load() != 1 {
		t.Fatalf("retirement panic leaked details or retried Cancel after reaping: calls=%d err=%v", cancelCalls.Load(), err)
	}
	analysisProcessTestAssertRetired(t, ids)
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) || cancelCalls.Load() != 1 {
		t.Fatalf("post-reap cancellation reached the original group signal: calls=%d err=%v", cancelCalls.Load(), err)
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
		t.Fatalf("panicking retirement silently returned capacity: %+v", stats)
	}
}

func TestBackgroundProcessJoinsContextCancellationBeforeReaping(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cancelEntered, cancelReleased := make(chan struct{}), make(chan struct{})
	retireReleased := make(chan struct{})
	var cancelOnce, releaseCancel, releaseRetire sync.Once
	var cancelCalls atomic.Int64
	command.Cancel = func() error {
		cancelCalls.Add(1)
		cancelOnce.Do(func() { close(cancelEntered) })
		<-cancelReleased
		return os.ErrProcessDone
	}
	waitable := make(chan int, 1)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- runBackgroundProcess(ctx, command, func() error {
			if err := waitMediaProcessWithoutReaping(command.Process.Pid); err != nil {
				return err
			}
			waitable <- command.Process.Pid
			<-retireReleased
			return nil
		}, governor, &unknown)
		close(finished)
	}()
	t.Cleanup(func() {
		releaseCancel.Do(func() { close(cancelReleased) })
		releaseRetire.Do(func() { close(retireReleased) })
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("background owner did not join its cancellation watcher")
		}
	})
	var pid int
	select {
	case pid = <-waitable:
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not reach its pinned exited state")
	}
	cancel()
	select {
	case <-cancelEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("context watcher did not reach the original cancellation barrier")
	}
	releaseRetire.Do(func() { close(retireReleased) })
	// Retirement has returned, but the original cancellation signal still
	// owns its barrier. The leader must remain unreaped until that call joins.
	if err := waitMediaProcessWithoutReaping(pid); err != nil {
		t.Fatalf("leader reaped before in-flight context cancellation joined: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 1, 0)
	releaseCancel.Do(func() { close(cancelReleased) })
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("joined context cancellation returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background process did not finish after its signal owner joined")
	}
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) || cancelCalls.Load() != 1 {
		t.Fatalf("closed cancellation fence forwarded a post-reap signal: calls=%d err=%v", cancelCalls.Load(), err)
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{}) {
		t.Fatalf("joined cancellation retained capacity or recorded a false fault: %+v", stats)
	}
}

func TestBackgroundProcessAbnormalCallbackExitPublishesAndJoins(t *testing.T) {
	for _, mode := range []string{"retirement nil panic", "retirement Goexit", "cleanup Cancel Goexit", "watcher Cancel Goexit"} {
		t.Run(mode, func(t *testing.T) {
			governor := newMediaProcessAdmission(4, 2, 128)
			var unknown atomic.Uint64
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tool := analysisProcessTestTool(t, `sleep 60 &
child=$!
printf 'ready %s %s\n' "$$" "$child"
wait "$child"`)
			command := exec.CommandContext(ctx, tool)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			retire := configureMediaProcess(command)
			cancelGroup := command.Cancel
			var cancelCalls atomic.Int64
			command.Cancel = func() error {
				cancelCalls.Add(1)
				err := cancelGroup()
				if mode == "cleanup Cancel Goexit" || mode == "watcher Cancel Goexit" {
					runtime.Goexit()
				}
				return err
			}
			ready := make(chan analysisProcessTestPIDs, 1)
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				done <- runBackgroundProcess(ctx, command, func() error {
					line, err := bufio.NewReader(stdout).ReadString('\n')
					if err != nil {
						return err
					}
					var ids analysisProcessTestPIDs
					if count, err := fmt.Sscanf(line, "ready %d %d\n", &ids.leader, &ids.child); err != nil || count != 2 {
						return errors.New("invalid abnormal-exit readiness fixture")
					}
					ready <- ids
					switch mode {
					case "retirement nil panic":
						panic(nil)
					case "retirement Goexit":
						runtime.Goexit()
					case "cleanup Cancel Goexit":
						return errors.New("force abnormal pre-Wait cancellation fixture")
					}
					// The watcher case returns a successful retirement while its
					// original cancellation callback exits abnormally. The fence
					// must join that callback and retain the aggregate fault.
					return retire()
				}, governor, &unknown)
				close(finished)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("abnormal callback prevented owned process completion")
				}
			})
			var ids analysisProcessTestPIDs
			select {
			case ids = <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("abnormal-exit fixture did not start its actual process group")
			}
			if mode == "watcher Cancel Goexit" {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrProcessRetirementUnknown) {
					t.Fatalf("abnormal callback exit published a successful result: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("abnormal callback exited without publishing retirement or watcher completion")
			}
			analysisProcessTestAssertRetired(t, ids)
			if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
				t.Fatalf("abnormal callbacks lost or duplicated their retained charge: %+v", stats)
			}
			if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) || cancelCalls.Load() != 1 {
				t.Fatalf("abnormal callback left a post-reap signal path: calls=%d err=%v", cancelCalls.Load(), err)
			}
		})
	}
}

func TestBackgroundProcessCancelGoexitBeforeSignalingStopsOwnedLeader(t *testing.T) {
	governor := newMediaProcessAdmission(4, 2, 128)
	var unknown atomic.Uint64
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Exec replaces the shell with sleep; no orphan descendants are created by
	// this fixture when the abnormal Cancel fails to send its intended signal.
	tool := analysisProcessTestTool(t, `printf 'ready %s\n' "$$"
exec sleep 60`)
	command := exec.CommandContext(ctx, tool)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	retire := configureMediaProcess(command)
	var cancelCalls atomic.Int64
	command.Cancel = func() error {
		cancelCalls.Add(1)
		runtime.Goexit()
		return nil
	}
	ready := make(chan int, 1)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- runBackgroundProcess(ctx, command, func() error {
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil {
				return err
			}
			var pid int
			if count, err := fmt.Sscanf(line, "ready %d\n", &pid); err != nil || count != 1 {
				return errors.New("invalid direct-leader readiness fixture")
			}
			ready <- pid
			return retire()
		}, governor, &unknown)
		close(finished)
	}()
	leaderReady := false
	t.Cleanup(func() {
		cancel()
		if leaderReady {
			// Use the owned process handle, never an unpinned numeric PGID, to
			// clean up a regression where the cancellation fallback is absent.
			_ = command.Process.Kill()
		}
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("abnormal cancellation retained its direct leader join")
		}
	})
	var pid int
	select {
	case pid = <-ready:
		leaderReady = true
	case <-time.After(5 * time.Second):
		t.Fatal("abnormal cancellation fixture did not start its direct leader")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrProcessRetirementUnknown) {
			t.Fatalf("unsignaled cancellation exit published a successful result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel Goexit left the owned direct leader alive indefinitely")
	}
	var status syscall.WaitStatus
	if actual, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("abnormal cancellation did not reap its direct leader: pid=%d err=%v", actual, err)
	}
	if stats := processCapacityStatsFor(governor, &unknown); stats != (ProcessCapacitySnapshot{Active: 1, Background: 1, RetirementUnknown: 1}) {
		t.Fatalf("direct-leader cleanup hid the unknown group charge: %+v", stats)
	}
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) || cancelCalls.Load() != 1 {
		t.Fatalf("fallback cancellation left a post-reap signal path: calls=%d err=%v", cancelCalls.Load(), err)
	}
}
