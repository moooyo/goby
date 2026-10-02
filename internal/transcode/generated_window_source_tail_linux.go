//go:build linux

package transcode

import (
	"context"
	"errors"
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

// MeasureGeneratedMP4SourceTail joins a finite MP4 sample-table certificate
// with the complete observed decoded tail. Every selected frame, including
// postroll, participates in the endpoint guard. A successful command does not
// establish CLI or demux EOF; the independent certificate bounds the declared
// sample set, while this probe must actually cover its last sample interval and
// reject decoded frames that extend beyond that declared endpoint.
//
// The raw probe setup intentionally remains local to this new helper so the
// existing source-range contract stays unchanged. A later shared probe pipeline
// can consolidate these settings while preserving the different endpoint guard.
func MeasureGeneratedMP4SourceTail(ctx context.Context, ffprobe string, source *os.File, plan Plan, certificate GeneratedSourceEndpointCertificate) (GeneratedSourceRange, error) {
	var empty GeneratedSourceRange
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	originTicks, err := generatedMP4SourceTailPlan(plan, certificate)
	if err != nil {
		return empty, err
	}
	file, err := DuplicateInput(source)
	if err != nil {
		return empty, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return empty, ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil || identity != certificate.SourceIdentity {
		return empty, ErrInvalidInput
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return empty, ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return empty, ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, generatedSourceRangeTimeout)
	defer cancel()
	budget := &generatedSourceRangeBudget{cancel: cancel}
	output := &generatedSourceRangeOutput{budget: budget}
	diagnostics := &generatedSourceRangeDiagnostics{budget: budget}
	// No end position or frame-count option can turn this observed tail into
	// a successful truncated prefix. All selected decoder output is collected
	// under the ordinary byte, frame and time budgets and inspected afterward.
	interval := signedTickSeconds(originTicks+plan.StartTicks) + "%"
	command := exec.CommandContext(processCtx, executable,
		"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
		"-select_streams", strconv.Itoa(plan.VideoStreamIndex), "-read_intervals", interval,
		"-show_frames", "-show_streams", "-show_format", "-show_entries",
		"frame=media_type,stream_index,pts,duration:frame_side_data=:stream=index,codec_type,time_base:stream_tags=:stream_disposition=:stream_side_data=:format=start_time:format_tags=",
		"-of", "json", "-i", "/proc/self/fd/3")
	command.Dir, command.Stdout, command.Stderr = "/", output, diagnostics
	command.ExtraFiles = []*os.File{file}
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
	// Preserve the caller's foreground/background admission class. The
	// retirement callback and Wait join precede release of the process lease.
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		if err := waitWithoutReaping(command.Process.Pid); err != nil {
			groupMu.Lock()
			retired = true
			groupMu.Unlock()
			return err
		}
		groupMu.Lock()
		defer groupMu.Unlock()
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		retired = true
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if err := ctx.Err(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if err := budget.failure(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if err := processCtx.Err(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if runErr != nil {
		if command.Process == nil {
			return empty, errors.Join(ErrStart, runErr)
		}
		return empty, errors.Join(ErrTimelineProbe, runErr)
	}
	coverage, err := parseGeneratedMP4SourceTail(output.buffer.Bytes(), plan, certificate)
	if err != nil {
		return empty, err
	}
	if !transcodeSourceUnchanged(file, before) || ValidateGeneratedMP4SourceEndpointIdentity(file, certificate) != nil {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return coverage, nil
}
