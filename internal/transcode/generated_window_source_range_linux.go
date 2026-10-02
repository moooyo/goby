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

const generatedSourceRangeTimeout = 30 * time.Second

// MeasureGeneratedSourceRange proves decoded source-frame coverage for one
// bounded requested interval. It independently verifies the format origin and
// returns source-global rational clocks. Seeking and a successful interval
// probe do not prove EOF, even when the requested end equals metadata duration.
// The borrowed descriptor's identity and ctime remain fenced across the entire
// process lifetime; opening it through procfs preserves its shared file offset.
func MeasureGeneratedSourceRange(ctx context.Context, ffprobe string, source *os.File, plan Plan, formatOriginTicks int64) (GeneratedSourceRange, error) {
	var empty GeneratedSourceRange
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := generatedSourceRangePlan(plan, formatOriginTicks); err != nil {
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
	// Both origins are bounded by maxDurationTicks, so their sum is safely
	// representable. Add the format origin exactly once when asking libavformat
	// to seek, then subtract its independently observed value from frame PTS.
	interval := signedTickSeconds(formatOriginTicks+plan.StartTicks) + "%" + signedTickSeconds(formatOriginTicks+plan.HLS.Window.EndTicks)
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
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		// WNOWAIT pins the leader's numeric PID until the group signal has
		// completed. ECHILD is not a safe descendant-retirement proof.
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
	result, err := parseGeneratedSourceRange(output.buffer.Bytes(), plan, formatOriginTicks)
	if err != nil {
		return empty, err
	}
	if !transcodeSourceUnchanged(file, before) {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
