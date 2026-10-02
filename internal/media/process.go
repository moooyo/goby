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
	"sync"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
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
	output, err := runLimitedFilesOutput(ctx, timeout, maxStdout, executable, files, args...)
	return output.stdout, err
}

type mediaProcessOutput struct {
	stdout []byte
	stderr []byte
}

// runLimitedFilesOutput retains bounded diagnostics for callers whose evidence
// requires a clean decoder run even when the executable returns success.
func runLimitedFilesOutput(ctx context.Context, timeout time.Duration, maxStdout int, executable string, files []*os.File, args ...string) (mediaProcessOutput, error) {
	if err := ctx.Err(); err != nil {
		return mediaProcessOutput{}, err
	}
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	if maxStdout <= 0 {
		return mediaProcessOutput{}, fmt.Errorf("media process stdout limit must be positive")
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
	process, err := startMediaProcess(processContext, cmd)
	if process != nil {
		if err == nil {
			err = process.Wait()
		}
		// Start may return an owned child together with a capture failure. Join
		// cleanup before reading its output, and retain any retirement fault.
		err = errors.Join(err, process.Close())
	}
	if stdout.exceeded || stderr.exceeded {
		return mediaProcessOutput{}, errors.Join(fmt.Errorf("%s: %w", filepath.Base(executable), ErrOutputLimit), err)
	}
	if processContext.Err() != nil {
		return mediaProcessOutput{}, errors.Join(fmt.Errorf("%s: %w", filepath.Base(executable), processContext.Err()), err)
	}
	if err != nil {
		detail := stderr.buffer.String()
		if len(detail) > maxErrorDetail {
			detail = detail[:maxErrorDetail] + " [truncated]"
		}
		detail = strings.TrimSpace(detail)
		if detail != "" {
			return mediaProcessOutput{}, fmt.Errorf("execute %s: %w: %s", filepath.Base(executable), err, detail)
		}
		return mediaProcessOutput{}, fmt.Errorf("execute %s: %w", filepath.Base(executable), err)
	}
	return mediaProcessOutput{stdout: stdout.buffer.Bytes(), stderr: stderr.buffer.Bytes()}, nil
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

type mediaProcess struct {
	native           *nativeMediaProcess
	command          *exec.Cmd
	retired          <-chan error
	release          func()
	retainForCleanup bool
	retireOnce       sync.Once
	retireErr        error
	waitOnce         sync.Once
	waitErr          error
	ownerOnce        sync.Once
	ownerErr         error
	conventional     *conventionalMediaProcessOwner
	closeOnce        sync.Once
	closeErr         error
	probeChild       *probeRetirementChild
}

func startMediaProcess(ctx context.Context, command *exec.Cmd) (*mediaProcess, error) {
	return startMediaProcessWithAdmission(ctx, command, mediaProcessAdmission, false)
}

// Cgroup-backed diagnostics retain the lease beyond leader/stdio completion,
// until the session also proves that its command domain has been retired.
func startMediaProcessHeld(ctx context.Context, command *exec.Cmd) (*mediaProcess, error) {
	return startMediaProcessWithAdmission(ctx, command, mediaProcessAdmission, true)
}

func startMediaProcessWithAdmission(ctx context.Context, command *exec.Cmd, governor *mediaProcessGovernor, retainForCleanup bool) (*mediaProcess, error) {
	if scope, required := commanddomain.CommandScopeFromContext(ctx); required {
		return startNativeMediaProcess(ctx, command, governor, scope)
	}
	return startConventionalMediaProcessWithCapability(ctx, command, governor, retainForCleanup, conventionalRetirementSupported())
}

func startConventionalMediaProcessWithCapability(ctx context.Context, command *exec.Cmd, governor *mediaProcessGovernor, retainForCleanup, supported bool) (*mediaProcess, error) {
	if cohort, _ := ctx.Value(probeRetirementContextKey{}).(*probeRetirementCohort); cohort != nil && !supported {
		return nil, commanddomain.ErrUnavailable
	}
	release, err := governor.acquire(ctx)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			release()
		}
	}()
	retire := configureMediaProcess(command)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	process := &mediaProcess{command: command, release: release, retainForCleanup: retainForCleanup}
	if err := process.attachProbeRetirement(ctx); err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		process.probeChild.notStarted()
		return nil, err
	}
	process.probeChild.started()
	// Capture the original direct child before launching any retirement worker.
	// A capture failure retains the actual permit and the registered owner.
	owned = true
	done := make(chan error, 1)
	process.retired = done
	var captureErr error
	if supported {
		captureErr = process.ensureConventionalOwner()
	}
	go func() { done <- retire() }()
	if captureErr != nil {
		process.observeProbeRetirement(false, true)
		return process, captureErr
	}
	return process, nil
}

func (process *mediaProcess) Retire() error {
	if process.native != nil {
		return process.native.wait()
	}
	process.retireOnce.Do(func() { process.retireErr = <-process.retired })
	return process.retireErr
}

// Wait retains the exited leader as a waitable child until process-group
// retirement, then joins exec's pipe-copy writers before returning capacity.
// Repeated or concurrent Wait and Close calls share the same join and result.
func (process *mediaProcess) Wait() error {
	if process.native != nil {
		return process.native.wait()
	}
	if !conventionalRetirementSupported() {
		process.waitOnce.Do(func() {
			if !process.retainForCleanup {
				defer process.release()
			}
			process.waitErr = errors.Join(process.Retire(), process.command.Wait())
		})
		return process.waitErr
	}
	process.waitOnce.Do(func() {
		completed := false
		defer func() {
			if !completed {
				process.waitErr = errors.Join(process.waitErr, ErrProcessRetirementUnknown)
				if process.conventional != nil {
					process.conventional.markUnknown()
				}
			}
		}()
		if err := process.ensureConventionalOwner(); err != nil {
			process.waitErr = errors.Join(ErrProcessRetirementUnknown, err)
			process.observeProbeRetirement(false, true)
			completed = true
			return
		}
		retireErr := process.Retire()
		if retireErr != nil {
			process.conventional.markUnknown()
			process.waitErr = errors.Join(ErrProcessRetirementUnknown, retireErr)
			completed = true
			return
		}
		if err := process.conventional.fence(); err != nil {
			process.conventional.markUnknown()
			process.waitErr = errors.Join(ErrProcessRetirementUnknown, err)
			completed = true
			return
		}
		process.waitErr = process.command.Wait()
		if process.command.ProcessState == nil {
			process.conventional.markUnknown()
			process.waitErr = errors.Join(process.waitErr, ErrProcessRetirementUnknown)
		} else {
			process.conventional.joinedKnown()
		}
		completed = true
	})
	return process.waitErr
}

// ExitCode observes only the actual joined child. The native owner's original
// immutable exec.Cmd template never contains its private ProcessState.
func (process *mediaProcess) ExitCode() (int, bool) {
	if process == nil {
		return -1, false
	}
	if process.native != nil {
		if process.native.process == nil {
			return -1, false
		}
		return process.native.process.ExitCode()
	}
	if process.command == nil || process.command.ProcessState == nil {
		return -1, false
	}
	return process.command.ProcessState.ExitCode(), true
}

func (process *mediaProcess) Close() error {
	if process == nil {
		return nil
	}
	if process.native != nil {
		return process.native.close()
	}
	if !conventionalRetirementSupported() {
		if process.command.Cancel != nil {
			_ = process.command.Cancel()
		} else if process.command.Process != nil {
			_ = process.command.Process.Kill()
		}
		return process.Wait()
	}
	process.closeOnce.Do(func() {
		if err := process.ensureConventionalOwner(); err != nil {
			process.closeErr = errors.Join(ErrProcessRetirementUnknown, err)
			process.observeProbeRetirement(false, true)
			if process.conventional == nil {
				return
			}
			for process.conventional.retryCapture() != nil {
				time.Sleep(time.Second)
			}
			// The original callback may still own a cancellation signal fence.
			// Join it before the independent cleanup proof permits actual Wait.
			process.closeErr = errors.Join(process.closeErr, process.Retire())
		}
		cancelErr := process.conventional.cancel()
		waitErr := process.Wait()
		if process.command.ProcessState == nil {
			// A failed callback did not authorize Wait. Explicit cleanup uses a
			// fresh actual kernel fence while the original leader is still pinned.
			for {
				if err := process.conventional.fence(); err == nil {
					break
				} else {
					process.conventional.markUnknown()
					process.closeErr = errors.Join(cancelErr, waitErr, err, ErrProcessRetirementUnknown)
				}
				// Actual cleanup retains bounded owners and the original strong
				// identity while a kernel reader is still live. Receipt.Close's
				// caller deadline does not terminate this owner or free capacity.
				time.Sleep(time.Second)
				cancelErr = errors.Join(cancelErr, process.conventional.cancel())
			}
			joinErr := process.command.Wait()
			if process.command.ProcessState == nil {
				process.conventional.markUnknown()
				process.closeErr = errors.Join(cancelErr, waitErr, joinErr, ErrProcessRetirementUnknown)
				return
			}
			process.conventional.joinedKnown()
			process.closeErr = errors.Join(process.closeErr, cancelErr, waitErr, joinErr)
		} else {
			process.closeErr = errors.Join(cancelErr, waitErr)
		}
		if !process.retainForCleanup {
			process.conventional.returnKnownCapacity()
		}
	})
	return process.closeErr
}

// completeCleanup is called only by the retained cgroup owner after its extra
// domain cleanup succeeds. Wait also prevents a cleanup call from releasing a
// lease while command output writers or the leader are still owned.
func (process *mediaProcess) completeCleanup() {
	if process != nil {
		if process.native != nil {
			_ = process.native.wait()
			return
		}
		if !conventionalRetirementSupported() {
			_ = process.Wait()
			process.release()
			return
		}
		_ = process.Wait()
		if process.conventional != nil {
			process.conventional.returnKnownCapacity()
		}
	}
}
