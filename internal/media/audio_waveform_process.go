package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Full-title PCM can exceed the legacy intro runner's eight-GiB stdout budget.
// This runner streams it with a computed finite byte bound, while retaining the
// same process governor, source-read phase, resource limiter, retirement, and
// child/pipe join contract. Diagnostic bytes are counted, never accumulated.
func runAudioWaveformProcess(ctx context.Context, source *os.File, tool *analysisTool, args []string, timeout time.Duration, stdoutLimit int64, stderr analysisStderrSink, parse func(io.Reader) error) error {
	if ctx == nil || source == nil || tool == nil || tool.file == nil || stderr == nil || parse == nil || timeout <= 0 || timeout > 2*time.Hour || stdoutLimit < 1 || stdoutLimit > 4<<40 {
		return ErrAnalysisUnavailable
	}
	err := RunSourceReadPhase(ctx, func(work context.Context) error {
		return runAudioWaveformProcessJoined(work, source, tool, args, timeout, stdoutLimit, stderr, parse)
	})
	if err != nil {
		stderr.Close(err)
	}
	return err
}

func runAudioWaveformProcessJoined(ctx context.Context, source *os.File, tool *analysisTool, args []string, timeout time.Duration, stdoutLimit int64, stderr analysisStderrSink, parse func(io.Reader) error) (resultErr error) {
	processContext, cancel := context.WithTimeout(WithBackgroundProcess(ctx), timeout)
	defer cancel()
	if err := processContext.Err(); err != nil {
		stderr.Close(err)
		return err
	}
	limiter, err := mediaEditResourceLimiter()
	if err != nil {
		stderr.Close(err)
		return fmt.Errorf("%w: waveform process limiter", ErrAnalysisUnavailable)
	}
	arguments := append([]string{"--as=2147483648:2147483648", "--nofile=64:64", "--fsize=0:0", "--", "/proc/self/fd/4"}, args...)
	command := exec.CommandContext(processContext, limiter, arguments...)
	command.ExtraFiles = []*os.File{source, tool.file}
	command.Env = mediaProbeEnvironment()
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
	process, err := startMediaProcess(processContext, command)
	if err != nil {
		cancel()
		stderr.Close(err)
		if process != nil {
			err = errors.Join(err, process.Close())
		}
		return fmt.Errorf("%w: start waveform decoder: %w", ErrAnalysisUnavailable, err)
	}
	stderrDone := make(chan error, 1)
	stderrJoined := false
	defer func() {
		cancel()
		stderr.Close(processContext.Err())
		if command.Cancel != nil {
			_ = command.Cancel()
		}
		if !stderrJoined {
			<-stderrDone
		}
		resultErr = errors.Join(resultErr, process.Close())
	}()
	go func() {
		_, err := io.Copy(&analysisProcessStderr{sink: stderr, cancel: cancel, remaining: 4 << 30}, errorPipe)
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
			parseErr = fmt.Errorf("%w: unmatched waveform PCM bytes", ErrAnalysisUnproven)
		}
	}
	if bounded.N <= 0 {
		parseErr = errors.Join(ErrAnalysisBudget, parseErr)
	}
	if parseErr != nil {
		cancel()
		stderr.Close(parseErr)
	}
	retireErr := process.Retire()
	stderrErr := <-stderrDone
	stderrJoined = true
	waitErr := process.Wait()
	joinedErr := errors.Join(retireErr, waitErr)
	if err := ctx.Err(); err != nil {
		return errors.Join(err, parseErr, stderrErr, joinedErr)
	}
	if errors.Is(parseErr, ErrAnalysisBudget) || errors.Is(stderrErr, ErrAnalysisBudget) {
		return errors.Join(ErrAnalysisBudget, parseErr, stderrErr, joinedErr)
	}
	if parseErr != nil {
		return errors.Join(parseErr, stderrErr, joinedErr)
	}
	if stderrErr != nil {
		return errors.Join(stderrErr, joinedErr)
	}
	if err := processContext.Err(); err != nil {
		return errors.Join(err, joinedErr)
	}
	if joinedErr != nil {
		return fmt.Errorf("waveform decoder did not exit cleanly: %w", joinedErr)
	}
	return nil
}
