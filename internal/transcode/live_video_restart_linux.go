//go:build linux

package transcode

import (
	"bytes"
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

// ValidateLiveVideoRestart proves the first copied video access unit of one
// closed segment. It borrows regular descriptors without changing offsets.
// fMP4 initialization and media are concatenated privately; MPEG-TS must carry
// its own parameter sets before the first IDR. No source scan or URL is opened.
// Callers bound concurrent probes with their existing publication semaphore.
func ValidateLiveVideoRestart(ctx context.Context, ffprobe string, initialization, segment *os.File, codec string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !VideoEncodingSupported(codec) || codec == "av1" && initialization == nil {
		return ErrInvalidPlan
	}
	if segment == nil {
		return ErrInvalidInput
	}
	inputs := []*os.File{segment}
	if initialization != nil {
		inputs = []*os.File{initialization, segment}
	}
	var owned []*os.File
	var before []os.FileInfo
	defer func() {
		for _, file := range owned {
			_ = file.Close()
		}
	}()
	var total int64
	for _, input := range inputs {
		file, err := DuplicateInput(input)
		if err != nil {
			return err
		}
		owned = append(owned, file)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return ErrInvalidInput
		}
		if info.Size() > maxHLSMuxClockInputBytes-total {
			return ErrTimelineLimit
		}
		total += info.Size()
		before = append(before, info)
	}
	inputReader := func() io.Reader {
		readers := make([]io.Reader, len(owned))
		for index, file := range owned {
			readers[index] = io.NewSectionReader(file, 0, before[index].Size())
		}
		return io.MultiReader(readers...)
	}
	if initialization != nil {
		prefix, err := io.ReadAll(io.LimitReader(inputReader(), MaxProgressivePrefixBytes))
		if err != nil {
			return ErrInvalidInput
		}
		ready := false
		for _, audio := range []bool{false, true} {
			plan := Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "copy", VideoCopyCodec: codec, VideoStreamIndex: 0, AudioStreamIndex: -1}
			if audio {
				plan.AudioStreamIndex, plan.AudioCodec = 1, "copy"
			}
			matched, err := ProgressiveVideoReady(plan, prefix)
			ready = ready || matched && err == nil
		}
		if !ready {
			return ErrTimelineProbe
		}
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, hlsMuxClockTimeout)
	defer cancel()
	budget := &liveRestartProbeBudget{cancel: cancel}
	output := &liveRestartProbeOutput{budget: budget, limit: maxLiveRestartProbeBytes}
	diagnostics := &liveRestartProbeOutput{budget: budget, limit: 64 << 10}
	format := "mpegts"
	if initialization != nil {
		format = "mov"
	}
	command := exec.CommandContext(processCtx, executable, "-v", "error", "-threads", "1", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", format, "-select_streams", "v:0", "-read_intervals", "%+#1",
		"-show_packets", "-show_frames", "-show_streams", "-show_data", "-show_entries",
		"packet=type,stream_index,size,flags,data:packet_side_data=side_data_type:frame=type,media_type,stream_index,key_frame,pict_type,width,height:stream=index,codec_name,codec_type,extradata,extradata_size",
		"-of", "json", "-i", "pipe:0")
	command.Dir, command.Stdin, command.Stdout, command.Stderr = "/", inputReader(), output, diagnostics
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
	if err := command.Start(); err != nil {
		return ErrStart
	}
	waitErr := waitWithoutReaping(command.Process.Pid)
	groupMu.Lock()
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	runErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if budget.failed {
		return ErrTimelineLimit
	}
	if processCtx.Err() != nil {
		return processCtx.Err()
	}
	if waitErr != nil || runErr != nil || diagnostics.buffer.Len() != 0 {
		return ErrTimelineProbe
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return ErrInvalidInput
		}
	}
	return parseLiveVideoRestartProbe(output.buffer.Bytes(), codec, initialization != nil)
}

type liveRestartProbeBudget struct {
	mu     sync.Mutex
	bytes  int
	failed bool
	cancel context.CancelFunc
}

type liveRestartProbeOutput struct {
	budget *liveRestartProbeBudget
	limit  int
	buffer bytes.Buffer
}

func (output *liveRestartProbeOutput) Write(data []byte) (int, error) {
	output.budget.mu.Lock()
	if output.budget.failed || len(data) > maxLiveRestartProbeBytes-output.budget.bytes || len(data) > output.limit-output.buffer.Len() {
		output.budget.failed = true
		output.budget.mu.Unlock()
		output.budget.cancel()
		return 0, ErrTimelineLimit
	}
	output.budget.bytes += len(data)
	output.budget.mu.Unlock()
	return output.buffer.Write(data)
}
