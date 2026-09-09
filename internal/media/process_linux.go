package media

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

func configureMediaProcess(command *exec.Cmd) func() error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
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
		// Keep the exited leader waitable until every signal has been sent, so
		// its numeric process-group identifier cannot be recycled underneath us.
		err := waitMediaProcessWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		if !errors.Is(err, syscall.ECHILD) {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		retired = true
		groupMu.Unlock()
		return err
	}
}

func waitMediaProcessWithoutReaping(pid int) error {
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
