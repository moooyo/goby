//go:build linux

package transcode

import (
	"bytes"
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

const generatedAVSourceTimeout = 30 * time.Second

// MeasureGeneratedMP4AVSourceEndpoint returns only a metadata candidate. It
// borrows a regular source descriptor without changing its offset, fences its
// identity/ctime across metadata and fresh demux observation, and retains F
// independently of metadata-derived P. The trusted caller's foreground or
// background context controls global media process admission; request data
// must not choose that classification. Decoded coverage and actual effective
// AAC samples require additional evidence before any publication decision.
func MeasureGeneratedMP4AVSourceEndpoint(ctx context.Context, ffprobe string, source *os.File, videoIndex, audioIndex int) (GeneratedAVSourceCertificate, error) {
	var result GeneratedAVSourceCertificate
	if ctx == nil {
		return result, ErrInvalidOptions
	}
	err := media.RunSourceReadPhase(ctx, func(work context.Context) error {
		var err error
		result, err = measureGeneratedMP4AVSourceEndpoint(work, ffprobe, source, videoIndex, audioIndex)
		return err
	})
	return result, err
}

func measureGeneratedMP4AVSourceEndpoint(ctx context.Context, ffprobe string, source *os.File, videoIndex, audioIndex int) (_ GeneratedAVSourceCertificate, resultErr error) {
	var empty GeneratedAVSourceCertificate
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	file, err := DuplicateInput(source)
	if err != nil {
		return empty, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeSourceReadInput(file)) }()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return empty, ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil {
		return empty, ErrInvalidInput
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return empty, ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return empty, ErrStart
	}
	proofCtx, cancel := context.WithTimeout(ctx, generatedAVSourceTimeout)
	defer cancel()
	candidate, parseErr := parseGeneratedMP4AVSourceMetadata(proofCtx, file, before.Size(), videoIndex, audioIndex)
	if !transcodeSourceUnchanged(file, before) {
		return empty, ErrInvalidInput
	}
	if parseErr != nil {
		return empty, parseErr
	}
	budget := &generatedAVSourceProbeBudget{cancel: cancel}
	output := &generatedAVSourceProbeOutput{budget: budget}
	command := exec.CommandContext(proofCtx, executable,
		"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
		"-show_streams", "-show_format", "-show_entries",
		"stream=index,id,codec_type,codec_name,time_base,start_pts,sample_rate,channels:stream_tags=:stream_disposition=:stream_side_data=:format=start_time:format_tags=",
		"-of", "json", "-i", "/proc/self/fd/3")
	command.Dir = "/"
	command.Stdout = output
	command.Stderr = &generatedAVSourceProbeDiagnostics{budget: budget}
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
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(proofCtx, command, func() error {
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if !transcodeSourceUnchanged(file, before) {
		return empty, errors.Join(ErrInvalidInput, runErr)
	}
	if err := ctx.Err(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if err := budget.failure(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if err := proofCtx.Err(); err != nil {
		return empty, errors.Join(err, runErr)
	}
	if runErr != nil {
		if command.Process == nil {
			return empty, errors.Join(ErrStart, runErr)
		}
		return empty, errors.Join(ErrTimelineProbe, runErr)
	}
	result, err := bindGeneratedAVDemuxOrigin(candidate, output.buffer.Bytes())
	if err != nil {
		return empty, err
	}
	if !transcodeSourceUnchanged(file, before) {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := proofCtx.Err(); err != nil {
		return empty, err
	}
	result.SourceIdentity = identity
	return result, nil
}

// ValidateGeneratedMP4AVSourceEndpointIdentity does not refresh or upgrade a
// candidate. Keep the same descriptor identity across all later evidence and
// recheck it immediately before a future publication decision.
func ValidateGeneratedMP4AVSourceEndpointIdentity(source *os.File, certificate GeneratedAVSourceCertificate) error {
	if source == nil || certificate.SourceIdentity == "" {
		return ErrInvalidInput
	}
	info, err := source.Stat()
	if err != nil {
		return ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(info)
	if err != nil || identity != certificate.SourceIdentity {
		return ErrInvalidInput
	}
	return nil
}

type generatedAVSourceProbeBudget struct {
	mu     sync.Mutex
	bytes  int
	err    error
	cancel context.CancelFunc
}

func (budget *generatedAVSourceProbeBudget) add(length int) error {
	budget.mu.Lock()
	if budget.err == nil && length > generatedAVSourceProjectionBytes-budget.bytes {
		budget.err = ErrTimelineLimit
	}
	if budget.err == nil {
		budget.bytes += length
	}
	err := budget.err
	budget.mu.Unlock()
	if err != nil {
		budget.cancel()
	}
	return err
}

func (budget *generatedAVSourceProbeBudget) fail(err error) error {
	budget.mu.Lock()
	if budget.err == nil {
		budget.err = err
	}
	err = budget.err
	budget.mu.Unlock()
	budget.cancel()
	return err
}

func (budget *generatedAVSourceProbeBudget) failure() error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.err
}

type generatedAVSourceProbeOutput struct {
	budget *generatedAVSourceProbeBudget
	buffer bytes.Buffer
}

func (output *generatedAVSourceProbeOutput) Write(data []byte) (int, error) {
	if err := output.budget.add(len(data)); err != nil {
		return 0, err
	}
	return output.buffer.Write(data)
}

type generatedAVSourceProbeDiagnostics struct{ budget *generatedAVSourceProbeBudget }

func (diagnostics *generatedAVSourceProbeDiagnostics) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	return 0, diagnostics.budget.fail(ErrTimelineProbe)
}
