//go:build linux

package backuppg

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

func configureBackupProcess(command *exec.Cmd) (func() error, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	var mu sync.Mutex
	retired := false
	command.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if retired || command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return func() error {
		// Keep the leader waitable until all group signals have been sent. A
		// concurrent Wait must not reap and recycle its process-group ID.
		err := waitBackupProcessWithoutReaping(command.Process.Pid)
		mu.Lock()
		if !errors.Is(err, syscall.ECHILD) {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		retired = true
		mu.Unlock()
		return err
	}, nil
}

func waitBackupProcessWithoutReaping(pid int) error {
	var info [16]uint64
	const processID = 1
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, processID, uintptr(pid), uintptr(unsafe.Pointer(&info[0])),
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
