package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var ErrOutputLimit = errors.New("media process output exceeded the configured limit")

const (
	defaultProcessTimeout = 30 * time.Second
	maxProcessStderr      = 64 * 1024
	maxErrorDetail        = 4 * 1024
)

// limitedOutput discards excess bytes and cancels the process as soon as its
// budget is exhausted. Each instance belongs to one command output stream.
type limitedOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining < len(p) {
		w.exceeded = true
		p = p[:remaining]
		w.cancel()
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

func runLimited(ctx context.Context, timeout time.Duration, maxStdout int, executable string, args ...string) ([]byte, error) {
	return runLimitedFiles(ctx, timeout, maxStdout, executable, nil, args...)
}

func runLimitedFiles(ctx context.Context, timeout time.Duration, maxStdout int, executable string, files []*os.File, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	if maxStdout <= 0 {
		return nil, fmt.Errorf("media process stdout limit must be positive")
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &limitedOutput{limit: maxStdout, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	cmd := exec.CommandContext(processContext, executable, args...)
	cmd.ExtraFiles = files
	if len(files) > 0 {
		cmd.Env = mediaProbeEnvironment()
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	retired, err := startMediaProcess(cmd)
	if err == nil {
		err = errors.Join(<-retired, cmd.Wait())
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("%s: %w", filepath.Base(executable), ErrOutputLimit)
	}
	if processContext.Err() != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(executable), processContext.Err())
	}
	if err != nil {
		detail := stderr.buffer.String()
		if len(detail) > maxErrorDetail {
			detail = detail[:maxErrorDetail] + " [truncated]"
		}
		detail = strings.TrimSpace(detail)
		if detail != "" {
			return nil, fmt.Errorf("execute %s: %w: %s", filepath.Base(executable), err, detail)
		}
		return nil, fmt.Errorf("execute %s: %w", filepath.Base(executable), err)
	}
	return stdout.buffer.Bytes(), nil
}

// Descriptor probes need no inherited reporting, proxy, or user configuration
// variables. The explicit loader path supports the pinned shared toolchain.
func mediaProbeEnvironment() []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C", "LC_ALL=C", "AV_LOG_FORCE_NOCOLOR=1"}
	if loader := os.Getenv("LD_LIBRARY_PATH"); loader != "" {
		env = append(env, "LD_LIBRARY_PATH="+loader)
	}
	return env
}

func startMediaProcess(command *exec.Cmd) (<-chan error, error) {
	retire := configureMediaProcess(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- retire() }()
	return done, nil
}
