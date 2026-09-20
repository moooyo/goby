package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var analysisProcessTestStderrError = errors.New("test metadata parser failure")

type analysisProcessTestPIDs struct {
	leader int
	child  int
}

// The ready record uses stderr, because the runner's zero file-size limit
// intentionally prevents a child from creating a readiness file.
type analysisProcessTestSink struct {
	mu       sync.Mutex
	ready    chan analysisProcessTestPIDs
	startedC chan struct{}
	closed   chan struct{}
	once     sync.Once
	header   []byte
	started  bool
	retired  bool
	metadata int
	mode     string
	err      error
}

func newAnalysisProcessTestSink(mode string) *analysisProcessTestSink {
	return &analysisProcessTestSink{ready: make(chan analysisProcessTestPIDs, 1), startedC: make(chan struct{}), closed: make(chan struct{}), mode: mode}
}

func (sink *analysisProcessTestSink) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.retired {
		return 0, io.ErrClosedPipe
	}
	for index, value := range data {
		if !sink.started {
			if len(sink.header) >= 128 {
				return index, errors.New("test readiness record exceeded its bound")
			}
			sink.header = append(sink.header, value)
			if value != '\n' {
				continue
			}
			var ids analysisProcessTestPIDs
			if n, err := fmt.Sscanf(string(sink.header), "ready %d %d\n", &ids.leader, &ids.child); err != nil || n != 2 || ids.leader <= 1 || ids.child <= 1 || ids.leader == ids.child {
				return index, errors.New("invalid test readiness record")
			}
			sink.started = true
			sink.ready <- ids
			close(sink.startedC)
			continue
		}
		sink.metadata++
		if sink.mode == "error" {
			sink.err = analysisProcessTestStderrError
			return index, sink.err
		}
		if sink.metadata > 32 {
			sink.err = ErrAnalysisBudget
			return index, sink.err
		}
	}
	return len(data), nil
}

func (sink *analysisProcessTestSink) Close(err error) {
	sink.once.Do(func() {
		sink.mu.Lock()
		sink.retired = true
		if sink.err == nil {
			sink.err = err
		}
		sink.mu.Unlock()
		close(sink.closed)
	})
}

func (sink *analysisProcessTestSink) wait() error {
	<-sink.closed
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.err
}

func (sink *analysisProcessTestSink) waitReady() error {
	select {
	case <-sink.startedC:
		return nil
	case <-sink.closed:
		return errors.New("analysis child closed stderr before its readiness record")
	}
}

func analysisProcessTestTool(t *testing.T, body string) string {
	t.Helper()
	tool := filepath.Join(t.TempDir(), "analysis-test-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return tool
}

func analysisProcessTestGroupTool(t *testing.T, action string) string {
	t.Helper()
	return analysisProcessTestTool(t, `sleep 60 &
child=$!
printf 'ready %s %s\n' "$$" "$child" >&2
`+action+`
wait "$child"`)
}

type analysisProcessTestRun struct {
	cancel   context.CancelFunc
	result   chan error
	finished chan struct{}
	sink     *analysisProcessTestSink
}

func startAnalysisProcessTest(t *testing.T, tool string, timeout time.Duration, limit int64, sink *analysisProcessTestSink, parse func(io.Reader) error) analysisProcessTestRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := analysisProcessTestRun{cancel: cancel, result: make(chan error, 1), finished: make(chan struct{}), sink: sink}
	go func() {
		run.result <- runAnalysisStream(ctx, tool, nil, nil, timeout, limit, sink, parse)
		close(run.finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-run.finished:
		case <-time.After(5 * time.Second):
			t.Error("analysis runner did not join its child during cleanup")
		}
	})
	return run
}

func (run analysisProcessTestRun) ready(t *testing.T) analysisProcessTestPIDs {
	t.Helper()
	select {
	case ids := <-run.sink.ready:
		return ids
	case <-run.finished:
		select {
		case ids := <-run.sink.ready:
			return ids
		default:
			t.Fatalf("analysis child exited before readiness: %v", <-run.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("analysis test child did not report its actual process IDs")
	}
	return analysisProcessTestPIDs{}
}

func (run analysisProcessTestRun) complete(t *testing.T) error {
	t.Helper()
	select {
	case err := <-run.result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("analysis failure did not unblock parsing and retire its process group")
	}
	return nil
}

func analysisProcessTestAssertRetired(t *testing.T, ids analysisProcessTestPIDs) {
	t.Helper()
	// ECHILD proves the runner performed Wait, instead of merely returning
	// after context cancellation while its direct child remains waitable.
	var status syscall.WaitStatus
	if pid, err := syscall.Wait4(ids.leader, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("analysis group leader was not already reaped: pid=%d status=%v err=%v", pid, status, err)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(ids.leader))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("analysis group leader still exists after return: %v", err)
	}
	// A killed orphan may briefly remain a zombie until the host's init reaps
	// it. The runner is its grandparent and cannot wait for that orphan; require
	// actual death, never just a successful signal or canceled context.
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(ids.child), "stat"))
		if analysisProcessTestDisappeared(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		end := bytes.LastIndexByte(data, ')')
		if end >= 0 && len(data) > end+2 && data[end+2] == 'Z' {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("analysis descendant remains executable after runner returned: %s", data)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A task can disappear after procfs lookup or open but before its stat read.
// Linux may report ESRCH for that race rather than the pathname's ENOENT.
// Both prove absence; permission, I/O and cancellation errors prove nothing.
func analysisProcessTestDisappeared(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func TestAnalysisProcessDisappearanceClassificationPreservesOtherFailures(t *testing.T) {
	for _, fixture := range []struct {
		err  error
		gone bool
	}{
		{&os.PathError{Op: "read", Path: "/proc/123/stat", Err: syscall.ESRCH}, true},
		{&os.PathError{Op: "open", Path: "/proc/123/stat", Err: syscall.ENOENT}, true},
		{&os.PathError{Op: "read", Path: "/proc/123/stat", Err: syscall.EACCES}, false},
		{&os.PathError{Op: "read", Path: "/proc/123/stat", Err: syscall.EIO}, false},
		{context.Canceled, false},
		{nil, false},
	} {
		if got := analysisProcessTestDisappeared(fixture.err); got != fixture.gone {
			t.Fatalf("process disappearance %v = %v, want %v", fixture.err, got, fixture.gone)
		}
	}
}

func TestAnalysisProcessStdoutBudgetRetiresActualGroup(t *testing.T) {
	sink := newAnalysisProcessTestSink("")
	tool := analysisProcessTestGroupTool(t, "dd if=/dev/zero bs=4096 count=1024 2>/dev/null")
	run := startAnalysisProcessTest(t, tool, 30*time.Second, 64, sink, func(reader io.Reader) error {
		if err := sink.waitReady(); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, reader)
		return err
	})
	ids := run.ready(t)
	if err := run.complete(t); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("stdout budget returned %v", err)
	}
	analysisProcessTestAssertRetired(t, ids)
}

func TestAnalysisProcessParserFailureRetiresActualGroup(t *testing.T) {
	sink := newAnalysisProcessTestSink("")
	tool := analysisProcessTestGroupTool(t, "printf 'malformed-json'")
	parseError := errors.New("test invalid analysis output")
	run := startAnalysisProcessTest(t, tool, 30*time.Second, 1024, sink, func(reader io.Reader) error {
		if err := sink.waitReady(); err != nil {
			return err
		}
		var prefix [1]byte
		if _, err := io.ReadFull(reader, prefix[:]); err != nil {
			return err
		}
		return parseError
	})
	ids := run.ready(t)
	if err := run.complete(t); !errors.Is(err, parseError) {
		t.Fatalf("parser failure returned %v", err)
	}
	analysisProcessTestAssertRetired(t, ids)
}

func TestAnalysisProcessEarlyParserSuccessRejectsUnconsumedOutput(t *testing.T) {
	sink := newAnalysisProcessTestSink("")
	tool := analysisProcessTestGroupTool(t, "printf 'prefix'; dd if=/dev/zero bs=4096 count=1024 2>/dev/null")
	run := startAnalysisProcessTest(t, tool, 30*time.Second, 1024, sink, func(reader io.Reader) error {
		if err := sink.waitReady(); err != nil {
			return err
		}
		var prefix [6]byte
		_, err := io.ReadFull(reader, prefix[:])
		return err
	})
	ids := run.ready(t)
	if err := run.complete(t); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parser success adopted trailing output or waited for timeout: %v", err)
	}
	analysisProcessTestAssertRetired(t, ids)
}

func TestAnalysisProcessStderrFailureUnblocksMetadataParser(t *testing.T) {
	for _, mode := range []string{"budget", "error"} {
		t.Run(mode, func(t *testing.T) {
			sink := newAnalysisProcessTestSink(mode)
			tool := analysisProcessTestGroupTool(t, "printf '0123456789012345678901234567890123456789' >&2")
			run := startAnalysisProcessTest(t, tool, 30*time.Second, 1024, sink, func(io.Reader) error {
				return sink.wait()
			})
			ids := run.ready(t)
			want := error(ErrAnalysisBudget)
			if mode == "error" {
				want = analysisProcessTestStderrError
			}
			if err := run.complete(t); !errors.Is(err, want) {
				t.Fatalf("metadata failure did not reach the blocked parser: %v", err)
			}
			analysisProcessTestAssertRetired(t, ids)
		})
	}
}

func TestAnalysisProcessCancellationAndTimeoutReapActualGroup(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			sink := newAnalysisProcessTestSink("")
			tool := analysisProcessTestGroupTool(t, ":")
			timeout := 30 * time.Second
			if mode == "timeout" {
				timeout = time.Second
			}
			run := startAnalysisProcessTest(t, tool, timeout, 1024, sink, func(io.Reader) error {
				return sink.wait()
			})
			ids := run.ready(t)
			if pgid, err := syscall.Getpgid(ids.child); err != nil || pgid != ids.leader {
				t.Fatalf("fixture descendant was not in the analysis process group: pgid=%d err=%v", pgid, err)
			}
			want := error(context.DeadlineExceeded)
			if mode == "cancel" {
				want = context.Canceled
				run.cancel()
			}
			if err := run.complete(t); !errors.Is(err, want) {
				t.Fatalf("analysis %s returned %v", mode, err)
			}
			analysisProcessTestAssertRetired(t, ids)
		})
	}
}

func TestAnalysisProcessBorrowedDescriptorAndExactOutputBoundary(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "authorized-analysis-source")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	const payload = "authorized descriptor bytes"
	if _, err := input.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	// FFmpeg and the probe access the inherited descriptor through this path,
	// obtaining an independent open-file position rather than consuming fd 3.
	tool := analysisProcessTestTool(t, "cat /proc/self/fd/3")
	var output bytes.Buffer
	if err := runAnalysisStream(context.Background(), tool, input, nil, 5*time.Second, int64(len(payload)), &analysisDiscardStderr{}, func(reader io.Reader) error {
		_, err := io.Copy(&output, reader)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if output.String() != payload {
		t.Fatalf("borrowed descriptor read differs: %q", output.String())
	}
	if offset, err := input.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("analysis moved or closed its borrowed descriptor: %d, %v", offset, err)
	}
	var first [1]byte
	if _, err := input.ReadAt(first[:], 0); err != nil || first[0] != payload[0] {
		t.Fatalf("borrowed source did not remain readable: %q, %v", first, err)
	}
}

func TestAnalysisProcessForwardsBoundedPCMStdin(t *testing.T) {
	const payload = "\x00\x80\xff\x7f\x00\x00\xff\xff"
	tool := analysisProcessTestTool(t, "cat")
	var output bytes.Buffer
	if err := runAnalysisProcess(context.Background(), tool, nil, strings.NewReader(payload), nil, 5*time.Second, int64(len(payload)), &analysisDiscardStderr{}, func(reader io.Reader) error {
		_, err := io.Copy(&output, reader)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if output.String() != payload {
		t.Fatalf("PCM stdin bytes changed: %q", output.Bytes())
	}
}
