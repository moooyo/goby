//go:build linux

package media

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const conventionalRetirementTimeout = 5 * time.Second
const conventionalProcessCensusLimit = 32768

type conventionalProcessIdentity struct {
	pid, group, session, parent int
	start                       uint64
}

func readConventionalProcessIdentity(pid int) (conventionalProcessIdentity, byte, error) {
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return conventionalProcessIdentity{}, 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return conventionalProcessIdentity{}, 0, err
	}
	if len(data) > 8192 {
		return conventionalProcessIdentity{}, 0, ErrProcessRetirementUnknown
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return conventionalProcessIdentity{}, 0, ErrProcessRetirementUnknown
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 || len(fields[0]) != 1 {
		return conventionalProcessIdentity{}, 0, ErrProcessRetirementUnknown
	}
	identity := conventionalProcessIdentity{pid: pid}
	identity.parent, err = strconv.Atoi(fields[1])
	if err == nil {
		identity.group, err = strconv.Atoi(fields[2])
	}
	if err == nil {
		identity.session, err = strconv.Atoi(fields[3])
	}
	if err == nil {
		identity.start, err = strconv.ParseUint(fields[19], 10, 64)
	}
	if err != nil || identity.start == 0 {
		return conventionalProcessIdentity{}, 0, ErrProcessRetirementUnknown
	}
	return identity, fields[0][0], nil
}

func captureConventionalProcess(command *exec.Cmd) (conventionalProcessIdentity, int, error) {
	descriptor, _, errno := syscall.Syscall(unix.SYS_PIDFD_OPEN, uintptr(command.Process.Pid), 0, 0)
	pin := -1
	if errno == 0 {
		pin = int(descriptor)
	}
	identity, _, err := readConventionalProcessIdentity(command.Process.Pid)
	if err != nil || identity.pid != identity.group || identity.parent != os.Getpid() {
		return identity, pin, errors.Join(ErrProcessRetirementUnknown, err)
	}
	if errno != 0 {
		return identity, -1, errors.Join(ErrProcessRetirementUnknown, errno)
	}
	current, _, err := readConventionalProcessIdentity(identity.pid)
	if err != nil || current != identity {
		return identity, pin, ErrProcessRetirementUnknown
	}
	return identity, pin, nil
}

func retryConventionalProcessCapture(owner *conventionalMediaProcessOwner) error {
	if owner.process.command.ProcessState != nil {
		return ErrProcessRetirementUnknown
	}
	if owner.pin >= 0 {
		file, err := os.Open("/proc/self/fdinfo/" + strconv.Itoa(owner.pin))
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 8193))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 8192 {
			return ErrProcessRetirementUnknown
		}
		matched := false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "Pid:" {
				matched = fields[1] == strconv.Itoa(owner.process.command.Process.Pid)
			}
		}
		if !matched {
			return ErrProcessRetirementUnknown
		}
		identity, _, err := readConventionalProcessIdentity(owner.process.command.Process.Pid)
		if err != nil || identity.pid != identity.group || identity.parent != os.Getpid() {
			return ErrProcessRetirementUnknown
		}
		if owner.identity.start != 0 && owner.identity != identity {
			return ErrProcessRetirementUnknown
		}
		owner.identity = identity
		return nil
	}
	// Reopening a missing pin requires the original already captured epoch.
	// A bare numeric PID from a failed initial metadata capture is insufficient.
	if owner.identity.start == 0 {
		return ErrProcessRetirementUnknown
	}
	identity, pin, err := captureConventionalProcess(owner.process.command)
	if err != nil || identity != owner.identity {
		closeConventionalProcessPin(pin)
		return ErrProcessRetirementUnknown
	}
	owner.pin = pin
	return nil
}

func closeConventionalProcessPin(pin int) {
	if pin >= 0 {
		_ = syscall.Close(pin)
	}
}

func cancelConventionalProcess(identity conventionalProcessIdentity, pin int) error {
	if pin < 0 || identity.pid <= 0 {
		return ErrProcessRetirementUnknown
	}
	current, _, err := readConventionalProcessIdentity(identity.pid)
	if err != nil || current != identity {
		return ErrProcessRetirementUnknown
	}
	// The direct leader is still unreaped and both its kernel pin and exact
	// identity remain owned. This numeric group cannot be reused before Wait.
	if err := syscall.Kill(-identity.group, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return errors.Join(ErrProcessRetirementUnknown, err)
	}
	return nil
}

func conventionalLeaderWaitable(identity conventionalProcessIdentity) (bool, error) {
	var information [16]uint64
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(identity.pid), uintptr(unsafe.Pointer(&information[0])),
			syscall.WEXITED|syscall.WNOWAIT|syscall.WNOHANG, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return false, errors.Join(ErrProcessRetirementUnknown, errno)
		}
		return *(*int32)(unsafe.Pointer(&information[2])) == int32(identity.pid), nil
	}
}

func conventionalLiveGroupMembers(identity conventionalProcessIdentity) (int, error) {
	directory, err := os.Open("/proc")
	if err != nil {
		return 0, err
	}
	defer directory.Close()
	count, entries := 0, 0
	for {
		names, readErr := directory.Readdirnames(256)
		for _, name := range names {
			entries++
			if entries > conventionalProcessCensusLimit {
				return 0, ErrProcessRetirementUnknown
			}
			pid, err := strconv.Atoi(name)
			if err != nil || pid <= 0 || pid == identity.pid {
				continue
			}
			member, state, err := readConventionalProcessIdentity(pid)
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				return 0, err
			}
			if member.group == identity.group && member.session == identity.session {
				if state != 'Z' && state != 'X' {
					count++
					continue
				}
				// A terminal group leader alone does not prove that every thread
				// released its shared descriptor table. Inspect the actual task
				// group before accepting a zombie as a closed source reader.
				live, err := conventionalTerminalGroupThreadsLive(pid)
				if err != nil {
					return 0, err
				}
				if live {
					count++
				}
			}
		}
		if readErr == io.EOF {
			return count, nil
		}
		if readErr != nil {
			return 0, readErr
		}
	}
}

func conventionalTerminalGroupThreadsLive(pid int) (bool, error) {
	directory, err := os.Open("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		if _, _, currentErr := readConventionalProcessIdentity(pid); errors.Is(currentErr, os.ErrNotExist) || errors.Is(currentErr, syscall.ESRCH) {
			return false, nil
		}
		return false, errors.Join(ErrProcessRetirementUnknown, err)
	}
	defer directory.Close()
	entries := 0
	for {
		names, readErr := directory.Readdirnames(128)
		for _, name := range names {
			entries++
			if entries > 1024 {
				return false, ErrProcessRetirementUnknown
			}
			tid, err := strconv.Atoi(name)
			if err != nil || tid <= 0 {
				return false, ErrProcessRetirementUnknown
			}
			_, state, err := readConventionalProcessIdentity(tid)
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				return false, err
			}
			if state != 'Z' && state != 'X' {
				return true, nil
			}
		}
		if readErr == io.EOF {
			return false, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

func fenceConventionalProcess(identity conventionalProcessIdentity, pin int) error {
	if pin < 0 || identity.pid <= 0 {
		return ErrProcessRetirementUnknown
	}
	deadline := time.Now().Add(conventionalRetirementTimeout)
	for {
		current, _, err := readConventionalProcessIdentity(identity.pid)
		if err != nil || current != identity {
			return ErrProcessRetirementUnknown
		}
		waitable, err := conventionalLeaderWaitable(identity)
		if err != nil {
			return err
		}
		if waitable {
			if err := cancelConventionalProcess(identity, pin); err != nil {
				return err
			}
			members, err := conventionalLiveGroupMembers(identity)
			if err != nil {
				return errors.Join(ErrProcessRetirementUnknown, err)
			}
			if members == 0 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: actual process group remains live", ErrProcessRetirementUnknown)
		}
		time.Sleep(time.Millisecond)
	}
}
