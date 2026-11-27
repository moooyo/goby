package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type limitedStreamOutput struct {
	writer    io.Writer
	remaining int
	exceeded  bool
	err       error
	cancel    context.CancelFunc
}

func (output *limitedStreamOutput) Write(data []byte) (int, error) {
	size := len(data)
	if size > output.remaining {
		output.exceeded = true
		data = data[:output.remaining]
		output.cancel()
	}
	output.remaining -= len(data)
	n, err := output.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		output.err = err
		output.cancel()
		return n, err
	}
	return size, nil
}

// The stdout writer is bounded and joined before return. Syntax rejection can
// remain a writer-local result while complete stdout and stderr are drained.
// Process retirement uses the same process-group and reaping contract as the
// buffered runner, and never transfers ownership of inherited source files.
func runLimitedFilesStreamOutput(ctx context.Context, timeout time.Duration, maxStdout int, executable string, files []*os.File, writer io.Writer, args ...string) (mediaProcessOutput, error) {
	if err := ctx.Err(); err != nil {
		return mediaProcessOutput{}, err
	}
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	if maxStdout <= 0 || writer == nil {
		return mediaProcessOutput{}, fmt.Errorf("media process stream writer and stdout limit must be valid")
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &limitedStreamOutput{writer: writer, remaining: maxStdout, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	cmd := exec.CommandContext(processContext, executable, args...)
	cmd.ExtraFiles = files
	if len(files) > 0 {
		cmd.Env = mediaProbeEnvironment()
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	process, err := startMediaProcess(processContext, cmd)
	if process != nil {
		if err == nil {
			err = process.Wait()
		}
		// A non-nil Start owner must be joined before inspecting its writers.
		err = errors.Join(err, process.Close())
	}
	if failure := dolbyVisionStreamProcessFailure(ctx, processContext, stdout, stderr, err); failure != nil {
		return mediaProcessOutput{}, fmt.Errorf("%s: %w", filepath.Base(executable), failure)
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
	return mediaProcessOutput{stderr: stderr.buffer.Bytes()}, nil
}

func dolbyVisionStreamProcessFailure(ctx, processContext context.Context, stdout *limitedStreamOutput, stderr *limitedOutput, processErr error) error {
	// An unproved join also means the copier may still own the output state.
	if errors.Is(processErr, ErrProcessRetirementUnknown) {
		return errors.Join(ctx.Err(), processErr)
	}
	switch {
	case stdout.exceeded || stderr.exceeded:
		return ErrOutputLimit
	case ctx.Err() != nil:
		return ctx.Err()
	case stdout.err != nil:
		return stdout.err
	default:
		return processContext.Err()
	}
}
