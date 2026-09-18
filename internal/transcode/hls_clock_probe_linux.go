//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxHLSMuxClockInputBytes  = 64 << 20
	maxHLSMuxClockOutputBytes = 1 << 20
	maxHLSMuxClockLineBytes   = 4096
	hlsMuxClockTimeout        = 10 * time.Second
)

// DuplicateInput returns an owned close-on-exec descriptor for a borrowed
// regular input. Duplication is atomic with descriptor access and CLOEXEC. The
// copy shares the original file offset, so borrowers should use ReadAt.
func DuplicateInput(input *os.File) (*os.File, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}
	raw, err := input.SyscallConn()
	if err != nil {
		return nil, errors.Join(ErrInvalidInput, err)
	}
	duplicateFD := -1
	var duplicateErr error
	controlErr := raw.Control(func(fd uintptr) {
		var stat syscall.Stat_t
		if err := syscall.Fstat(int(fd), &stat); err != nil {
			duplicateErr = err
			return
		}
		if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
			duplicateErr = ErrInvalidInput
			return
		}
		result, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_DUPFD_CLOEXEC, 0)
		if errno != 0 {
			duplicateErr = errno
			return
		}
		duplicateFD = int(result)
	})
	if controlErr != nil || duplicateErr != nil || duplicateFD < 0 {
		if duplicateFD >= 0 {
			_ = syscall.Close(duplicateFD)
		}
		return nil, errors.Join(ErrInvalidInput, controlErr, duplicateErr)
	}
	return os.NewFile(uintptr(duplicateFD), input.Name()), nil
}

// MeasureHLSMuxClock reads the first selected packet's actual muxed PTS. It
// borrows authorized regular files without closing them or changing offsets.
// Fragmented MP4 is supplied as initialization bytes followed by media bytes;
// a transport stream needs only segment. Negative packet timestamps are valid.
func MeasureHLSMuxClock(ctx context.Context, ffprobe string, initialization, segment *os.File, video bool) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if segment == nil {
		return 0, ErrInvalidInput
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
	var readers []io.Reader
	var total int64
	for _, input := range inputs {
		file, err := DuplicateInput(input)
		if err != nil {
			return 0, err
		}
		owned = append(owned, file)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return 0, ErrInvalidInput
		}
		if info.Size() > maxHLSMuxClockInputBytes-total {
			return 0, ErrTimelineLimit
		}
		total += info.Size()
		before = append(before, info)
		readers = append(readers, io.NewSectionReader(file, 0, info.Size()))
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return 0, ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return 0, ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, hlsMuxClockTimeout)
	defer cancel()
	budget := &hlsMuxClockBudget{cancel: cancel}
	output := &hlsMuxClockWriter{budget: budget, inspect: true, video: video}
	diagnostics := &hlsMuxClockWriter{budget: budget}
	stream := "a:0"
	if video {
		stream = "v:0"
	}
	cmd := exec.CommandContext(processCtx, executable,
		"-v", "error", "-threads", "1", "-protocol_whitelist", "pipe", "-format_whitelist", inputFormats,
		"-select_streams", stream, "-read_intervals", "%+#8192",
		"-show_entries", "packet=pts_time,flags", "-of", "compact=p=1:nk=0", "-i", "pipe:0")
	cmd.Dir = "/"
	cmd.Stdin = io.MultiReader(readers...)
	cmd.Stdout, cmd.Stderr = output, diagnostics
	cmd.Env = processEnvironment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	var groupMu sync.Mutex
	retired := false
	cmd.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	if err := cmd.Start(); err != nil {
		return 0, ErrStart
	}
	// WNOWAIT keeps the leader PID pinned while the entire group is retired,
	// including descendants that inherited one of the probe's pipe endpoints.
	waitErr := waitWithoutReaping(cmd.Process.Pid)
	groupMu.Lock()
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	runErr := cmd.Wait()
	output.finish()
	diagnostics.finish()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := budget.failure(); err != nil {
		return 0, err
	}
	if err := processCtx.Err(); err != nil {
		return 0, err
	}
	if waitErr != nil || runErr != nil || !output.found {
		return 0, ErrTimelineProbe
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return 0, ErrInvalidInput
		}
	}
	return output.firstPTS, nil
}

// hlsMuxClockBudget bounds combined stdout and stderr, whose copy goroutines
// run independently. Only the first failure is retained, without diagnostics.
type hlsMuxClockBudget struct {
	mu     sync.Mutex
	bytes  int
	err    error
	cancel context.CancelFunc
}

func (b *hlsMuxClockBudget) add(length int) error {
	b.mu.Lock()
	if b.err != nil {
		err := b.err
		b.mu.Unlock()
		return err
	}
	if length > maxHLSMuxClockOutputBytes-b.bytes {
		b.err = ErrTimelineLimit
		b.mu.Unlock()
		b.cancel()
		return ErrTimelineLimit
	}
	b.bytes += length
	b.mu.Unlock()
	return nil
}

func (b *hlsMuxClockBudget) fail(err error) error {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	err = b.err
	b.mu.Unlock()
	b.cancel()
	return err
}

func (b *hlsMuxClockBudget) failure() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// hlsMuxClockWriter retains one bounded line and the first packet timestamp.
// Later packet data is discarded, but every byte and line remains bounded.
type hlsMuxClockWriter struct {
	budget         *hlsMuxClockBudget
	inspect, video bool
	line           [maxHLSMuxClockLineBytes]byte
	length         int
	firstPTS       int64
	found          bool
	err            error
}

func (w *hlsMuxClockWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if err := w.budget.add(len(data)); err != nil {
		w.err = err
		return 0, err
	}
	for index, char := range data {
		if char == '\n' {
			if err := w.consumeLine(); err != nil {
				w.err = w.budget.fail(err)
				return index + 1, w.err
			}
			w.length = 0
			continue
		}
		if w.length == len(w.line) {
			w.err = w.budget.fail(ErrTimelineLimit)
			return index, w.err
		}
		w.line[w.length] = char
		w.length++
	}
	return len(data), nil
}

func (w *hlsMuxClockWriter) finish() {
	if w.err == nil && w.length > 0 {
		if err := w.consumeLine(); err != nil {
			w.err = w.budget.fail(err)
		}
	}
	w.length = 0
}

func (w *hlsMuxClockWriter) consumeLine() error {
	if !w.inspect || w.found {
		return nil
	}
	line := strings.TrimSuffix(string(w.line[:w.length]), "\r")
	if line == "" {
		return nil
	}
	kind, fields, ok := strings.Cut(line, "|")
	if !ok || kind != "packet" {
		return ErrTimelineProbe
	}
	var ptsValue, flags string
	var hasPTS, hasFlags bool
	for _, field := range strings.Split(fields, "|") {
		key, value, hasValue := strings.Cut(field, "=")
		switch key {
		case "pts_time":
			if hasPTS || !hasValue {
				return ErrTimelineProbe
			}
			ptsValue, hasPTS = value, true
		case "flags":
			if hasFlags || !hasValue {
				return ErrTimelineProbe
			}
			flags, hasFlags = value, true
		}
	}
	pts, valid := timelineTimestamp(ptsValue)
	if !hasPTS || !valid || len(flags) > 8 || (w.video && (!hasFlags || !strings.ContainsRune(flags, 'K'))) {
		return ErrTimelineProbe
	}
	for _, flag := range flags {
		// Audio priming packets can carry the demuxer's discard flag. Their
		// timestamp still identifies the first packet passed to the muxer.
		if flag != 'K' && flag != '_' && !(flag == 'D' && !w.video) {
			return ErrTimelineProbe
		}
	}
	w.firstPTS, w.found = pts, true
	return nil
}
