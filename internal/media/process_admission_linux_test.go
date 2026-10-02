package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"
)

type mediaProcessTestBlockedWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (writer *mediaProcessTestBlockedWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.entered) })
	<-writer.release
	return len(data), nil
}

func mediaProcessTestStart(t *testing.T, governor *mediaProcessGovernor, ctx context.Context, command *exec.Cmd, retained bool) *mediaProcess {
	t.Helper()
	process, err := startMediaProcessWithAdmission(ctx, command, governor, retained)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Close(); process.completeCleanup() })
	return process
}

func TestMediaProcessCapacityRetainsBlockedOutputWriter(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writer := &mediaProcessTestBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	var unblock sync.Once
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "printf output")
	command.Stdout = writer
	process := mediaProcessTestStart(t, governor, ctx, command, false)
	t.Cleanup(func() { unblock.Do(func() { close(writer.release) }) })
	waited := make(chan error, 1)
	go func() { waited <- process.Wait() }()
	select {
	case <-writer.entered:
	case <-ctx.Done():
		t.Fatal("output writer did not reach its blocking barrier")
	}
	if err := process.Retire(); err != nil {
		t.Fatal(err)
	}
	// The leader has exited and its group has retired, but exec still owns the
	// blocked output copier. A later command must remain queued at this point.
	queued := make(chan struct {
		process *mediaProcess
		err     error
	}, 1)
	go func() {
		follower := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
		started, err := startMediaProcessWithAdmission(ctx, follower, governor, false)
		queued <- struct {
			process *mediaProcess
			err     error
		}{started, err}
	}()
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 1)
	select {
	case <-waited:
		t.Fatal("process lease was released before its output writer joined")
	default:
	}
	unblock.Do(func() { close(writer.release) })
	if err := <-waited; err != nil {
		t.Fatal(err)
	}
	select {
	case follower := <-queued:
		if follower.err != nil || follower.process == nil {
			t.Fatalf("follower failed after actual join: %v", follower.err)
		}
		defer follower.process.Close()
		if err := follower.process.Wait(); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("follower remained queued after the output writer joined")
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessCapacityConcurrentCloseWaitRetiresActualGroup(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
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
	process := mediaProcessTestStart(t, governor, ctx, command, false)
	var ids analysisProcessTestPIDs
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if count, err := fmt.Sscanf(line, "ready %d %d\n", &ids.leader, &ids.child); err != nil || count != 2 {
		t.Fatalf("invalid group readiness record %q: %v", line, err)
	}
	var joins sync.WaitGroup
	for range 8 {
		joins.Go(func() { _ = process.Wait() })
		joins.Go(func() { _ = process.Close() })
	}
	joins.Wait()
	analysisProcessTestAssertRetired(t, ids)
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	if process.Wait() != process.waitErr {
		t.Fatal("repeated join did not retain the same result")
	}
}

func TestMediaProcessCapacityStartFailureAndQueuedDeadline(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/goby-nonexistent-media-process")
	if process, err := startMediaProcessWithAdmission(ctx, command, governor, false); process != nil || err == nil {
		t.Fatalf("missing executable returned process=%v error=%v", process != nil, err)
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	owner := mediaProcessAdmissionTestAcquire(t, governor, ctx)
	deadline, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	command = exec.CommandContext(deadline, "/bin/sh", "-c", "exit 0")
	if process, err := startMediaProcessWithAdmission(deadline, command, governor, false); process != nil || !errors.Is(err, context.DeadlineExceeded) || command.Process != nil {
		t.Fatalf("queued timeout started a child: process=%v child=%v error=%v", process != nil, command.Process != nil, err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
	owner()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("invalid startup fixture did not exercise panic rollback")
			}
		}()
		_, _ = startMediaProcessWithAdmission(ctx, nil, governor, false)
	}()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestMediaProcessCapacityRetainsAdditionalCleanup(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	process := mediaProcessTestStart(t, governor, ctx, exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0"), true)
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
	// Diagnostic cgroup failures retain this extra cleanup claim. Only the
	// owner that proves domain retirement may return the remaining capacity.
	process.completeCleanup()
	process.completeCleanup()
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestDiagnosticFailedDomainCleanupRetainsMediaProcessLease(t *testing.T) {
	governor := newMediaProcessAdmission(1, 1, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	process := mediaProcessTestStart(t, governor, ctx, exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0"), true)
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	domain, domainPath := diagnosticTestOwnedGroup(t)
	leaf, leafPath := diagnosticTestOwnedGroup(t)
	domain.domain = true
	diagnosticTestControl(t, domainPath, "memory.events", "max 0\noom 0\noom_kill 0\n")
	diagnosticTestControl(t, domainPath, "pids.events", "max 0\n")
	diagnosticTestControl(t, leafPath, "cgroup.kill", "0")
	diagnosticTestControl(t, leafPath, "cgroup.events", "populated 0\n")
	session := &diagnosticProcessSession{ctx: ctx, cancel: cancel, group: domain, commandGroup: leaf, commandProcess: process}
	// The existing cgroup fixture deliberately cannot remove its directory
	// containing control files. A retirement failure must retain both owners.
	if _, err := session.retireCommand(ctx); err == nil || session.commandProcess != process || session.commandGroup != leaf {
		t.Fatalf("failed cgroup closure lost its process lease: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 1, 0, 0)
	// Model the cgroup owner's successful later retirement, without treating
	// this ordinary directory fixture as a real kernel cgroup acceptance test.
	leaf.removed, domain.removed = true, true
	if err := session.close(); err != nil || session.commandProcess != nil || !session.closed {
		t.Fatalf("retired domain retry did not finish lease cleanup: %v", err)
	}
	mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
}

func TestAnalysisProcessParserPanicJoinsOwnedResources(t *testing.T) {
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
	sink := newAnalysisProcessTestSink("")
	tool := analysisProcessTestGroupTool(t, "printf output")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const failure = "analysis parser panic fixture"
	recovered := make(chan any, 1)
	go func() {
		defer func() { recovered <- recover() }()
		_ = runAnalysisStream(ctx, tool, nil, nil, time.Minute, 1024, sink, func(io.Reader) error {
			if err := sink.waitReady(); err != nil {
				return err
			}
			panic(failure)
		})
	}()
	var ids analysisProcessTestPIDs
	select {
	case ids = <-sink.ready:
	case <-ctx.Done():
		t.Fatal("analysis parser panic fixture did not start its process group")
	}
	select {
	case value := <-recovered:
		if value != failure {
			t.Fatalf("parser panic was changed: %v", value)
		}
	case <-ctx.Done():
		t.Fatal("parser panic did not join analysis output resources")
	}
	select {
	case <-sink.closed:
	default:
		t.Fatal("parser panic left metadata waiters open")
	}
	analysisProcessTestAssertRetired(t, ids)
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
}
