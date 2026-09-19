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

var liveCaptionExtractionSlots = make(chan struct{}, 4)

type liveCaptionOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (output *liveCaptionOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := output.limit - output.Len()
	if len(data) > remaining {
		data = data[:remaining]
		output.exceeded = true
		output.cancel()
	}
	_, _ = output.Buffer.Write(data)
	return n, nil
}

// ExtractLiveCaption fully drains one completed companion into bounded WebVTT
// before its caller may seal EndTicks. It borrows the file, reopens that exact
// descriptor with an independent offset, and preserves the original copied
// timestamps. An empty track returns a real WEBVTT header, not inferred silence.
func ExtractLiveCaption(ctx context.Context, executable string, segment LiveCaptionSegment) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validLiveCaptionSegment(segment) || strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return nil, ErrLiveCaption
	}
	processCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case liveCaptionExtractionSlots <- struct{}{}:
		defer func() { <-liveCaptionExtractionSlots }()
	case <-processCtx.Done():
		return nil, processCtx.Err()
	}
	before, err := segment.File.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxLiveSegmentBytes {
		return nil, ErrLiveCaption
	}
	resolved, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(resolved) {
		return nil, ErrStart
	}
	formats := "matroska,webm"
	if segment.Container == "mp4" {
		formats = "mov,mp4,m4a,3gp,3g2,mj2"
	}
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-copyts", "-threads", "1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", formats, "-discard:v", "all", "-discard:a", "all",
		"-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(segment.SubtitleStreamIndex), "-vn", "-an", "-dn",
		"-c:s", "webvtt", "-avoid_negative_ts", "disabled", "-f", "webvtt", "pipe:1"}
	stdout := &liveCaptionOutput{limit: media.MaxSubtitleExtractionBytes, cancel: cancel}
	stderr := &liveCaptionOutput{limit: maxStderrTail, cancel: cancel}
	cmd := exec.CommandContext(processCtx, resolved, args...)
	cmd.ExtraFiles = []*os.File{segment.File}
	cmd.Env = processEnvironment()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = terminateGrace + time.Second
	var groupMu sync.Mutex
	var timer *time.Timer
	retired := false
	cmd.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		timer = time.AfterFunc(terminateGrace, func() {
			groupMu.Lock()
			defer groupMu.Unlock()
			if !retired {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		})
		return err
	}
	if err := cmd.Start(); err != nil {
		if processCtx.Err() != nil {
			return nil, processCtx.Err()
		}
		return nil, ErrStart
	}
	// Keep the leader waitable until all process-group signals have completed,
	// matching the parent conversion runner's protection against PID reuse.
	waitErr := waitWithoutReaping(cmd.Process.Pid)
	groupMu.Lock()
	if timer != nil {
		timer.Stop()
	}
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	err = cmd.Wait()
	if processCtx.Err() != nil {
		return nil, processCtx.Err()
	}
	if waitErr != nil || err != nil || stdout.exceeded || stderr.exceeded || stderr.Len() != 0 ||
		stdout.Len() == 0 || !bytes.HasPrefix(stdout.Bytes(), []byte("WEBVTT")) || !transcodeSourceUnchanged(segment.File, before) {
		return nil, ErrLiveCaption
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}
