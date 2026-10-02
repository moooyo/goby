//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type generatedAVPreMuxPipe struct {
	read, write *os.File
	parser      *generatedAVPreMuxWriter
	result      GeneratedAVPreMuxDiagnostic
	err         error
}

type generatedAVPreMuxObserver struct {
	ctx       context.Context
	pipes     []*generatedAVPreMuxPipe
	done      chan struct{}
	mu        sync.Mutex
	err       error
	stop      func() bool
	startOnce sync.Once
}

func newGeneratedAVPreMuxObserver(ctx context.Context, count int, cancel context.CancelFunc) (*generatedAVPreMuxObserver, error) {
	if ctx == nil || count < 2 || count > MaxHLSRenditions {
		return nil, ErrInvalidOptions
	}
	observer := &generatedAVPreMuxObserver{ctx: ctx, done: make(chan struct{})}
	fail := func(err error) {
		observer.mu.Lock()
		if observer.err == nil {
			observer.err = err
		}
		observer.mu.Unlock()
		observer.closePipes()
		cancel()
	}
	for variant := 0; variant < count; variant++ {
		for stream := 0; stream < 2; stream++ {
			var parser *generatedAVPreMuxWriter
			parser, _ = newGeneratedAVPreMuxWriter(variant, stream, func() { fail(parser.err) })
			read, write, err := os.Pipe()
			if err != nil {
				observer.close()
				return nil, err
			}
			observer.pipes = append(observer.pipes, &generatedAVPreMuxPipe{read: read, write: write, parser: parser})
		}
	}
	observer.stop = context.AfterFunc(ctx, observer.closePipes)
	return observer, nil
}

func (observer *generatedAVPreMuxObserver) closePipes() {
	for _, pipe := range observer.pipes {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
}
func (observer *generatedAVPreMuxObserver) close() {
	if observer.stop != nil {
		observer.stop()
	}
	observer.closePipes()
}

func (observer *generatedAVPreMuxObserver) start() {
	observer.startOnce.Do(func() {
		var readers sync.WaitGroup
		for _, pipe := range observer.pipes {
			_ = pipe.write.Close()
			readers.Add(1)
			go func(pipe *generatedAVPreMuxPipe) {
				defer readers.Done()
				defer pipe.read.Close()
				completed := false
				defer func() {
					_ = recover()
					if !completed {
						pipe.err = ErrProgress
						_ = pipe.parser.fail(pipe.err)
					}
				}()
				_, pipe.err = io.Copy(pipe.parser, pipe.read)
				if pipe.err == nil {
					pipe.result, pipe.err = pipe.parser.finish()
					if pipe.err == nil {
						pipe.result.ReaderEOF = true
					}
				} else {
					pipe.err = pipe.parser.fail(pipe.err)
				}
				completed = true
			}(pipe)
		}
		go func() {
			readers.Wait()
			observer.mu.Lock()
			if observer.err == nil {
				for _, pipe := range observer.pipes {
					if pipe.err != nil {
						observer.err = pipe.err
						break
					}
				}
			}
			observer.mu.Unlock()
			close(observer.done)
		}()
	})
}

func (observer *generatedAVPreMuxObserver) finish() ([]GeneratedAVPreMuxDiagnostic, error) {
	<-observer.done
	observer.mu.Lock()
	err := observer.err
	observer.mu.Unlock()
	if err = errors.Join(err, observer.ctx.Err()); err != nil {
		return nil, err
	}
	var result []GeneratedAVPreMuxDiagnostic
	for _, pipe := range observer.pipes {
		result = append(result, pipe.result)
	}
	return result, nil
}

// generatedAVObservedArguments changes only private stats paths/formats and
// inserts per-stream input/audio-mux stats options. The ordinary BuildArgs
// media, seek, filter, codec, mux, cut, timestamp and ladder arguments survive.
func generatedAVObservedArguments(plan Plan, args []string) ([]string, []string, error) {
	count := plan.HLS.RenditionCount
	if count < 2 || count > MaxHLSRenditions || plan.HLS.Window.RequireInputEvidence || plan.HLS.Window.NativeClockVersion != 0 {
		return nil, nil, ErrInvalidPlan
	}
	var output, delta []string
	variant, formats := 0, 0
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "-stats_mux_pre_fmt:v:0" {
			if index+1 >= len(args) || args[index+1] != "GOBY {fidx} {n} {tb} {pts}" {
				return nil, nil, ErrInvalidPlan
			}
			output = append(output, argument, generatedAVPreMuxFormat)
			delta = append(delta, "replace video pre-mux stats format: "+generatedAVPreMuxFormat)
			formats++
			index++
			continue
		}
		if variant < count && argument == HLSPlaylistName(variant, count) {
			stats := []string{"-stats_enc_pre:v:0", "pipe:" + strconv.Itoa(4+count+variant), "-stats_enc_pre_fmt:v:0", "GOBY_INPUT {fidx} {sidx} {n} {ni} {tb} {pts} {tbi} {ptsi}",
				"-stats_enc_pre:a:0", "pipe:" + strconv.Itoa(4+2*count+variant), "-stats_enc_pre_fmt:a:0", "GOBY_AUDIO {fidx} {sidx} {n} {sn} {samp} {tb} {pts} {ni} {tbi} {ptsi}",
				"-stats_mux_pre:a:0", "pipe:" + strconv.Itoa(4+3*count+variant), "-stats_mux_pre_fmt:a:0", generatedAVPreMuxFormat}
			output = append(output, stats...)
			delta = append(delta, stats...)
			variant++
		}
		output = append(output, argument)
	}
	if variant != count || formats != count {
		return nil, nil, ErrInvalidPlan
	}
	return output, delta, nil
}

func runGeneratedAVStatsDiagnostic(ctx context.Context, executable, directory string, source *os.File, plan Plan, threads int, maxMediaBytes int64) (GeneratedAVDiagnosticRun, []string, error) {
	result := GeneratedAVDiagnosticRun{Plan: plan}
	if ctx == nil || source == nil || !emptyOutputDirectory(directory) {
		return result, nil, ErrInvalidOptions
	}
	threads, err := ExecutionThreads(plan, threads)
	if err != nil {
		return result, nil, err
	}
	baselineArgs, err := BuildArgs(plan, threads)
	if err != nil {
		return result, nil, err
	}
	args, delta, err := generatedAVObservedArguments(plan, baselineArgs)
	if err != nil {
		return result, nil, err
	}
	result.Arguments = args
	if executable == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return result, delta, ErrStart
	}
	resolved, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(resolved) {
		return result, delta, ErrStart
	}
	before, err := source.Stat()
	if err != nil {
		return result, delta, ErrInvalidInput
	}
	owned, err := DuplicateInput(source)
	if err != nil {
		return result, delta, err
	}
	defer owned.Close()
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if maxMediaBytes < 1 || maxMediaBytes > generatedAVTransportBytes {
		return result, delta, ErrTimelineLimit
	}
	monitor, err := newGeneratedAVDiagnosticOutputMonitor(processCtx, directory, maxMediaBytes, cancel)
	if err != nil {
		return result, delta, err
	}
	defer monitor.finish()
	count := plan.HLS.RenditionCount
	mux, err := newGeneratedAVPreMuxObserver(processCtx, count, cancel)
	if err != nil {
		return result, delta, err
	}
	defer mux.close()
	video, err := newGeneratedInputObserver(processCtx, plan, nil, cancel)
	if err != nil {
		return result, delta, err
	}
	defer video.close()
	options := make([]RawGeneratedAudioInputOptions, count)
	span := plan.HLS.Window.EndTicks - plan.StartTicks
	maxSamples := span*48000/ticksPerSecond + 4096
	for variant := range options {
		options[variant] = RawGeneratedAudioInputOptions{Rendition: variant, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: 4096, MaxSamples: maxSamples}
	}
	audio, err := newGeneratedAudioInputObserver(processCtx, options, nil, cancel)
	if err != nil {
		return result, delta, err
	}
	defer audio.close()
	command := exec.CommandContext(processCtx, resolved, args...)
	command.Env, command.Dir = processEnvironment(), directory
	command.ExtraFiles = []*os.File{owned}
	for _, pipe := range mux.pipes {
		if pipe.parser.result.StreamIndex == 0 {
			command.ExtraFiles = append(command.ExtraFiles, pipe.write)
		}
	}
	for _, pipe := range video.pipes {
		command.ExtraFiles = append(command.ExtraFiles, pipe.write)
	}
	for _, pipe := range audio.pipes {
		command.ExtraFiles = append(command.ExtraFiles, pipe.write)
	}
	for _, pipe := range mux.pipes {
		if pipe.parser.result.StreamIndex == 1 {
			command.ExtraFiles = append(command.ExtraFiles, pipe.write)
		}
	}
	progress := &progressWriter{cancel: cancel}
	stderr := &stderrTail{cancel: cancel}
	command.Stdout, command.Stderr = progress, stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	var muxErr, videoErr, audioErr error
	started := false
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		started = true
		mux.start()
		video.start()
		audio.start()
		waitErr := waitWithoutReaping(command.Process.Pid)
		killErr := func() error {
			groupMu.Lock()
			defer groupMu.Unlock()
			retired = true
			if waitErr != nil {
				return nil
			}
			err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			if errors.Is(err, syscall.ESRCH) {
				return nil
			}
			return err
		}()
		monitor.finish()
		if waitErr != nil || killErr != nil {
			cancel()
			mux.closePipes()
			video.closePipes()
			audio.closePipes()
		}
		// Private observer goroutines are outside os/exec's copier set. Join
		// them inside retirement so the process lease cannot precede them.
		result.PreMux, muxErr = mux.finish()
		videoErr = video.finish()
		audioErr = audio.finish()
		result.ObserversJoined = true
		// Parser/reader failure rejects diagnostic evidence after the join.
		// It must not falsely declare successful OS retirement unknown and
		// permanently retain the shared process charge.
		return errors.Join(waitErr, killErr)
	})
	if !started {
		return result, delta, errors.Join(runErr, ErrStart)
	}
	progress.finish()
	result.VideoInput, result.AudioInput = video.snapshot(), audio.snapshot()
	if err := errors.Join(runErr, muxErr, videoErr, audioErr, monitor.failure(), progress.err, processCtx.Err(), ctx.Err()); err != nil {
		return result, delta, err
	}
	if stderr.failed {
		return result, delta, ErrProcess
	}
	if !transcodeSourceUnchanged(source, before) {
		return result, delta, ErrInvalidInput
	}
	result.SourceIdentityUnchanged = true
	for _, trace := range result.PreMux {
		if trace.StreamIndex == 0 && len(trace.Packets) > 0 {
			packet := trace.Packets[0]
			result.MuxClocks[trace.Rendition] = HLSMuxClock{Rendition: trace.Rendition, PTS: packet.PTS, TimeBaseNumerator: packet.TimeBase.Num, TimeBaseDenominator: packet.TimeBase.Den}
		}
	}
	return result, delta, nil
}
