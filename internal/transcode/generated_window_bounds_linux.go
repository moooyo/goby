//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
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

const generatedBoundsProbeTimeout = 30 * time.Second

// MeasureGeneratedSegmentBounds probes all packets of one bounded closed
// generated segment. It borrows regular descriptors without changing offsets.
// A fragmented MP4 is supplied as initialization followed by media. No packet
// limit or interval cuts off the probe. Success requires complete JSON, input
// copier EOF, successful group retirement and unchanged input identities.
// The evidence does not replace a physical-container or source-coverage proof.
func MeasureGeneratedSegmentBounds(ctx context.Context, ffprobe string, initialization, segment *os.File, video bool) (GeneratedSegmentBounds, error) {
	var result GeneratedSegmentBounds
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if segment == nil {
		return result, ErrInvalidInput
	}
	inputs := []*os.File{segment}
	if initialization != nil {
		inputs = []*os.File{initialization, segment}
	}
	var owned []*os.File
	defer func() {
		for _, file := range owned {
			_ = file.Close()
		}
	}()
	var before []os.FileInfo
	input := &generatedBoundsInput{ctx: ctx}
	var total int64
	for _, borrowed := range inputs {
		file, err := DuplicateInput(borrowed)
		if err != nil {
			return result, err
		}
		owned = append(owned, file)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return result, ErrInvalidInput
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return result, ErrInvalidInput
		}
		if info.Size() > maxGeneratedBoundsInputBytes-total {
			return result, ErrTimelineLimit
		}
		total += info.Size()
		before = append(before, info)
		input.parts = append(input.parts, generatedBoundsInputPart{reader: io.NewSectionReader(file, 0, info.Size()), sum: sha256.New()})
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return result, ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return result, ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, generatedBoundsProbeTimeout)
	defer cancel()
	input.ctx = processCtx
	budget := &generatedBoundsBudget{cancel: cancel}
	output := &generatedBoundsOutput{budget: budget}
	diagnostics := &generatedBoundsDiagnostics{budget: budget}
	command := exec.CommandContext(processCtx, executable,
		"-v", "error", "-threads", "1", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats,
		"-show_packets", "-show_streams", "-show_entries",
		"packet=stream_index,pts,dts,duration,flags:packet_side_data=side_data_type,skip_samples,discard_padding:stream=index,codec_name,codec_type,time_base,sample_rate:stream_tags=:stream_disposition=:stream_side_data=",
		"-of", "json", "-i", "pipe:0")
	command.Dir, command.Stdin, command.Stdout, command.Stderr = "/", input, output, diagnostics
	command.Env = processEnvironment()
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
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	// Pin the leader PID with WNOWAIT until all inherited pipe holders have
	// been killed, then reap and join every os/exec copier before inspecting.
	var waitErr, retirementErr error
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		waitErr = waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		if !errors.Is(waitErr, syscall.ECHILD) {
			if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				retirementErr = err
			}
		}
		retired = true
		return errors.Join(waitErr, retirementErr)
	})
	if errors.Is(runErr, media.ErrProcessRetirementUnknown) {
		return result, errors.Join(ErrTimelineProbe, runErr)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := budget.failure(); err != nil {
		return result, err
	}
	if err := processCtx.Err(); err != nil {
		return result, err
	}
	if runErr != nil {
		if command.Process == nil {
			return result, errors.Join(ErrStart, runErr)
		}
		return result, errors.Join(ErrTimelineProbe, runErr)
	}
	if waitErr != nil || retirementErr != nil || !input.eof || input.bytes != total {
		return result, ErrTimelineProbe
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return result, ErrInvalidInput
		}
	}
	result, err = output.finish(video)
	if err != nil {
		return GeneratedSegmentBounds{}, err
	}
	segmentIndex := len(input.parts) - 1
	copy(result.SegmentSHA256[:], input.parts[segmentIndex].sum.Sum(nil))
	if initialization != nil {
		copy(result.InitializationSHA256[:], input.parts[0].sum.Sum(nil))
	}
	return result, nil
}

type generatedBoundsInputPart struct {
	reader *io.SectionReader
	sum    hash.Hash
}

// ReadAt-backed sections preserve the borrowed descriptors' shared offsets.
// The explicit EOF flag catches a child that successfully exits early; merely
// matching bytes is insufficient when the last copier read did not reach EOF.
type generatedBoundsInput struct {
	ctx   context.Context
	parts []generatedBoundsInputPart
	part  int
	bytes int64
	eof   bool
}

func (r *generatedBoundsInput) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	for r.part < len(r.parts) {
		part := &r.parts[r.part]
		n, err := part.reader.Read(data)
		if n != 0 {
			_, _ = part.sum.Write(data[:n])
			r.bytes += int64(n)
		}
		if err == io.EOF {
			r.part++
			if n != 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
	r.eof = true
	return 0, io.EOF
}

// With -v error every diagnostic is a failed evidence pass. Retain no output
// bytes and immediately cancel the process, avoiding sensitive diagnostics.
type generatedBoundsDiagnostics struct{ budget *generatedBoundsBudget }

func (w *generatedBoundsDiagnostics) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if err := w.budget.add(len(data)); err != nil {
		return 0, err
	}
	return 0, w.budget.fail(ErrTimelineProbe)
}
