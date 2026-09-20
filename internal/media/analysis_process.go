package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Close releases any metadata waiters on EOF or failure. It must be idempotent.
// Write must not block on unbounded queues; it returns an error at its budget.
type analysisStderrSink interface {
	io.Writer
	Close(error)
}

type analysisProcessStderr struct {
	sink      analysisStderrSink
	cancel    context.CancelFunc
	remaining int64
}

func (writer *analysisProcessStderr) Write(data []byte) (int, error) {
	if int64(len(data)) > writer.remaining {
		writer.cancel()
		return 0, ErrAnalysisBudget
	}
	writer.remaining -= int64(len(data))
	n, err := writer.sink.Write(data)
	if err != nil || n != len(data) {
		writer.cancel()
		if err == nil {
			err = io.ErrShortWrite
		}
		return n, err
	}
	return n, nil
}

func runAnalysisStream(ctx context.Context, executable string, input *os.File, args []string, timeout time.Duration, stdoutLimit int64,
	stderr analysisStderrSink, parse func(io.Reader) error, executables ...*os.File) error {
	return runAnalysisProcess(ctx, executable, input, nil, args, timeout, stdoutLimit, stderr, parse, executables...)
}

// Stdout is parsed while stderr is drained independently. Every cancellation or
// parser failure closes metadata waiters, kills the process group and joins the
// actual child before returning. Context cancellation is never a reaping claim.
func runAnalysisProcess(ctx context.Context, executable string, input *os.File, stdin io.Reader, args []string, timeout time.Duration, stdoutLimit int64,
	stderr analysisStderrSink, parse func(io.Reader) error, executables ...*os.File) error {
	if ctx == nil || runtime.GOOS != "linux" || executable == "" || stderr == nil || parse == nil || stdoutLimit < 1 || stdoutLimit > 8<<30 || timeout <= 0 || timeout > 2*time.Hour {
		return ErrAnalysisUnavailable
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := processContext.Err(); err != nil {
		stderr.Close(err)
		return err
	}
	limiter, err := mediaEditResourceLimiter()
	if err != nil {
		stderr.Close(err)
		return fmt.Errorf("%w: process limiter", ErrAnalysisUnavailable)
	}
	arguments := append([]string{"--as=2147483648:2147483648", "--nofile=64:64", "--fsize=0:0", "--", executable}, args...)
	command := exec.CommandContext(processContext, limiter, arguments...)
	if input != nil {
		command.ExtraFiles = []*os.File{input}
	}
	if len(executables) > 1 || len(executables) == 1 && executables[0] == nil {
		stderr.Close(ErrAnalysisUnavailable)
		return ErrAnalysisUnavailable
	}
	command.ExtraFiles = append(command.ExtraFiles, executables...)
	command.Env = mediaProbeEnvironment()
	command.Stdin = stdin
	command.WaitDelay = time.Second
	stdout, err := command.StdoutPipe()
	if err != nil {
		stderr.Close(err)
		return err
	}
	defer stdout.Close()
	errorPipe, err := command.StderrPipe()
	if err != nil {
		stderr.Close(err)
		return err
	}
	defer errorPipe.Close()
	retired, err := startMediaProcess(command)
	if err != nil {
		stderr.Close(err)
		return fmt.Errorf("%w: start analysis process: %w", ErrAnalysisUnavailable, err)
	}
	stderrDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&analysisProcessStderr{sink: stderr, cancel: cancel, remaining: 256 << 20}, errorPipe)
		if err != nil {
			cancel()
		}
		stderr.Close(err)
		stderrDone <- err
	}()
	bounded := &io.LimitedReader{R: stdout, N: stdoutLimit + 1}
	parseErr := parse(bounded)
	if parseErr == nil {
		var trailing [1]byte
		if n, err := bounded.Read(trailing[:]); n != 0 || err != io.EOF {
			parseErr = fmt.Errorf("%w: analysis parser did not consume its complete stream", ErrAnalysisUnproven)
		}
	}
	if bounded.N <= 0 {
		parseErr = ErrAnalysisBudget
	}
	if parseErr != nil {
		cancel()
		stderr.Close(parseErr)
	}
	// Wait for retirement before Wait, retaining the process-group leader as a
	// waitable child until all descendant signals have been delivered.
	retireErr := <-retired
	stderrErr := <-stderrDone
	waitErr := command.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(parseErr, ErrAnalysisBudget) || errors.Is(stderrErr, ErrAnalysisBudget) {
		return ErrAnalysisBudget
	}
	if parseErr != nil {
		return parseErr
	}
	if stderrErr != nil {
		return stderrErr
	}
	if err := processContext.Err(); err != nil {
		return err
	}
	if err := errors.Join(retireErr, waitErr); err != nil {
		return fmt.Errorf("analysis process did not exit cleanly: %w", err)
	}
	return nil
}

type analysisDiscardStderr struct {
	mu     sync.Mutex
	closed bool
	err    error
	bytes  int
}

func (sink *analysisDiscardStderr) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.closed {
		return 0, sink.err
	}
	sink.bytes += len(data)
	if sink.bytes > 64<<10 {
		return 0, ErrAnalysisBudget
	}
	// The fingerprint helper emits no diagnostics on success.
	if len(data) != 0 {
		sink.err = fmt.Errorf("%w: fingerprint helper emitted diagnostics", ErrAnalysisUnproven)
	}
	return len(data), nil
}

func (sink *analysisDiscardStderr) Close(err error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.closed = true
	if sink.err == nil {
		sink.err = err
	}
}

func (sink *analysisDiscardStderr) failure() error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.err
}
