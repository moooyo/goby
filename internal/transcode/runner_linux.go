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
	"unsafe"
)

const (
	maxProgressLine = 4096
	maxStderrTail   = 64 * 1024
	terminateGrace  = 2 * time.Second
)

// Run borrows a regular source file and opens it in the child through procfs.
// Reopening the inherited descriptor preserves the source inode while giving
// FFmpeg an independent file offset. The caller owns the file and must keep it
// open until Run returns. The output directory must be empty and privately
// owned by the job manager. onProgress runs synchronously on the stdout reader
// and must return promptly; it must not perform blocking network or disk work.
func Run(ctx context.Context, executable, directory string, input *os.File, plan Plan, threads int, onProgress func(Progress)) (RunResult, error) {
	result := RunResult{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	args, err := BuildArgs(plan, threads)
	if err != nil {
		return result, err
	}
	if input == nil {
		return result, ErrInvalidInput
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, ErrInvalidInput
	}
	if !emptyOutputDirectory(directory) {
		return result, ErrInvalidDirectory
	}
	if executable == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return result, ErrStart
	}
	resolved, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(resolved) {
		return result, ErrStart
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	progress := &progressWriter{callback: onProgress, cancel: cancel}
	stderr := &stderrTail{}
	cmd := exec.CommandContext(processCtx, resolved, args...)
	cmd.Dir = directory
	cmd.ExtraFiles = []*os.File{input}
	cmd.Stdout, cmd.Stderr = progress, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = terminateGrace + time.Second
	cmd.Env = processEnvironment()
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
		return result, ErrStart
	}
	// Keep the exited leader waitable: its PID also identifies our process
	// group and must not be recycled until every group signal has been sent.
	// exec's context watcher can signal/close pipes, but only Cmd.Wait reaps.
	waitErr := waitWithoutReaping(cmd.Process.Pid)
	groupMu.Lock()
	if timer != nil {
		timer.Stop()
	}
	// WNOWAIT pins the leader's identity through group retirement. ECHILD
	// indicates that some external reaper violated our exclusive ownership;
	// in that case its old numeric PGID is no longer safe to signal.
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	err = cmd.Wait()
	result.StderrTail = stderr.String()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	progress.finish()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if progress.err != nil {
		return result, ErrProgress
	}
	if waitErr != nil || err != nil {
		return result, ErrProcess
	}
	return result, nil
}

func waitWithoutReaping(pid int) error {
	// Linux siginfo_t is 128 bytes on every supported Linux architecture.
	// We only need the syscall's completion and intentionally do not decode
	// its architecture-dependent child-status layout. uint64 keeps alignment.
	var info [16]uint64
	const pPID = 1
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid), uintptr(unsafe.Pointer(&info[0])),
			syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func emptyOutputDirectory(directory string) bool {
	if !filepath.IsAbs(directory) || strings.ContainsAny(directory, "\x00\r\n") {
		return false
	}
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return false
	}
	dir, err := os.Open(directory)
	if err != nil {
		return false
	}
	defer dir.Close()
	after, err := dir.Stat()
	if err != nil || !os.SameFile(before, after) {
		return false
	}
	_, err = dir.Readdirnames(1)
	return errors.Is(err, io.EOF)
}

func processEnvironment() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		// Only locale, loader, temporary-directory and headless hardware
		// runtime settings reach a parser handling untrusted media. In
		// particular, server credentials and arbitrary report paths do not.
		switch name {
		case "PATH", "LANG", "LANGUAGE", "TZ", "TMPDIR", "TMP", "TEMP", "LD_LIBRARY_PATH",
			"LC_ALL", "LC_ADDRESS", "LC_COLLATE", "LC_CTYPE", "LC_IDENTIFICATION", "LC_MEASUREMENT",
			"LC_MESSAGES", "LC_MONETARY", "LC_NAME", "LC_NUMERIC", "LC_PAPER", "LC_TELEPHONE", "LC_TIME",
			"LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH", "LIBVA_MESSAGING_LEVEL",
			"ONEVPL_SEARCH_PATH", "MFX_HOME", "MFX_LOAD_PLUGINS",
			"CUDA_VISIBLE_DEVICES", "CUDA_DEVICE_ORDER", "CUDA_CACHE_DISABLE", "CUDA_CACHE_MAXSIZE", "CUDA_CACHE_PATH",
			"NVIDIA_VISIBLE_DEVICES", "NVIDIA_DRIVER_CAPABILITIES", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME":
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "AV_LOG_FORCE_NOCOLOR=1")
}

// progressWriter bounds each line, not the lifetime byte count. A long movie
// may emit progress for many hours without accumulating memory or being killed
// merely because its regular progress updates exceeded a total output budget.
type progressWriter struct {
	line     [maxProgressLine]byte
	length   int
	current  Progress
	callback func(Progress)
	cancel   context.CancelFunc
	err      error
}

func (w *progressWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	for i, char := range p {
		if char == '\n' {
			if !w.consumeLine() {
				w.fail()
				return i + 1, w.err
			}
			w.length = 0
			continue
		}
		if w.length == len(w.line) {
			w.fail()
			return i, w.err
		}
		w.line[w.length] = char
		w.length++
	}
	return len(p), nil
}

func (w *progressWriter) fail() {
	w.err = ErrProgress
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *progressWriter) finish() {
	if w.err == nil && w.length > 0 && !w.consumeLine() {
		w.fail()
	}
	w.length = 0
}

func (w *progressWriter) consumeLine() bool {
	line := strings.TrimSuffix(string(w.line[:w.length]), "\r")
	if line == "" {
		return true
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	switch key {
	case "out_time_us":
		if value == "N/A" {
			return true
		}
		microseconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || microseconds > maxDurationTicks/10+10_000_000 {
			return false
		}
		if microseconds < 0 {
			microseconds = 0
		}
		w.current.OutputTicks = microseconds * 10
	case "total_size":
		if value == "N/A" {
			return true
		}
		bytes, err := strconv.ParseInt(value, 10, 64)
		if err != nil || bytes < 0 {
			return false
		}
		w.current.Bytes = bytes
	case "progress":
		if value != "continue" && value != "end" {
			return false
		}
		w.current.Ended = value == "end"
		if w.callback != nil {
			w.callback(w.current)
		}
	}
	return true
}

type stderrTail struct {
	buffer []byte
}

func (w *stderrTail) Write(p []byte) (int, error) {
	n := len(p)
	if n >= maxStderrTail {
		w.buffer = append(w.buffer[:0], p[n-maxStderrTail:]...)
		return n, nil
	}
	if len(w.buffer)+n > maxStderrTail {
		keep := maxStderrTail - n
		copy(w.buffer, w.buffer[len(w.buffer)-keep:])
		w.buffer = w.buffer[:keep]
	}
	w.buffer = append(w.buffer, p...)
	return n, nil
}

func (w *stderrTail) String() string { return string(w.buffer) }
