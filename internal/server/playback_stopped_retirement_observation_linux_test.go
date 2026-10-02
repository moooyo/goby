//go:build linux

package server

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Fixed numeric/enumerated facts preserve the exact failed observation. No
// error text, command, path, token or inferred process/clock fact is retained.
type stoppedRetirementObservationFacts struct {
	Stage                    string
	PIDFDObserved            bool
	PIDFDReady, PIDFDErrno   int
	PIDFDRevents             int16
	StatObserved             bool
	StatErrno                int
	StatClass                string
	ExpectedStart, SeenStart uint64
	StartMismatch            bool
	GroupObserved            bool
	GroupErrno               int
}

func stoppedRetirementObservationErrno(err error) int {
	var number syscall.Errno
	if errors.As(err, &number) {
		return int(number)
	}
	return 0
}

// This remains an unknown observation. Only a fresh complete observation of
// the same pidfd and captured start identity can later establish retirement.
func (facts stoppedRetirementObservationFacts) retryExitedStatESRCH(exited bool) bool {
	return exited && facts.Stage == "stat_unknown" && facts.PIDFDObserved &&
		facts.PIDFDReady > 0 && facts.PIDFDErrno == 0 &&
		facts.PIDFDRevents&(unix.POLLIN|unix.POLLHUP) != 0 &&
		facts.PIDFDRevents&(unix.POLLERR|unix.POLLNVAL) == 0 &&
		facts.StatObserved && facts.StatErrno == int(syscall.ESRCH) && facts.StatClass == "esrch" &&
		facts.ExpectedStart != 0 && facts.SeenStart == 0 && !facts.StartMismatch && !facts.GroupObserved
}

// This observation-only copy retains the frozen helper's strict acceptance:
// same pidfd exit plus actual stat ENOENT, then a read-only absent group probe.
// ESRCH at stat is recorded as unknown, pending actual diagnosis, not accepted.
func stoppedEncoderRetirementObservation(process playbackStopAliasEncoder) (exited, reaped, groupClosed, known bool, facts stoppedRetirementObservationFacts) {
	facts.Stage, facts.ExpectedStart = "pidfd", process.startTick
	for attempt := 0; attempt < 4; attempt++ {
		fds := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 0)
		facts.PIDFDObserved, facts.PIDFDReady, facts.PIDFDErrno, facts.PIDFDRevents = true, ready, stoppedRetirementObservationErrno(err), fds[0].Revents
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return false, false, false, false, facts
		}
		exited = ready > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0
		facts.Stage = "stat"
		_, start, statErr := hlsColdPID(process.pid)
		facts.StatObserved, facts.SeenStart, facts.StatErrno = true, start, stoppedRetirementObservationErrno(statErr)
		facts.StartMismatch = statErr == nil && start != process.startTick
		switch {
		case statErr == nil:
			facts.StatClass = "present"
		case errors.Is(statErr, os.ErrNotExist):
			facts.StatClass = "enoent"
		case errors.Is(statErr, syscall.ESRCH):
			facts.StatClass = "esrch"
		default:
			facts.StatClass = "other"
		}
		if facts.StartMismatch {
			facts.Stage = "start_mismatch"
			return exited, false, false, false, facts
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			facts.Stage = "stat_unknown"
			return exited, false, false, false, facts
		}
		reaped = exited && errors.Is(statErr, os.ErrNotExist)
		if reaped {
			facts.Stage = "group"
			groupErr := syscall.Kill(-process.pid, 0)
			facts.GroupObserved, facts.GroupErrno = true, stoppedRetirementObservationErrno(groupErr)
			groupClosed = errors.Is(groupErr, syscall.ESRCH)
		}
		facts.Stage = "known"
		return exited, reaped, groupClosed, true, facts
	}
	facts.Stage = "pidfd_interrupted_bound"
	return false, false, false, false, facts
}
