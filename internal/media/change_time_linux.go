package media

import (
	"os"
	"syscall"
)

// FileChangeTime returns the inode change time in Unix nanoseconds.
// Unavailable, invalid, or unrepresentable timestamps return zero.
func FileChangeTime(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0
	}
	return changeTimeNanoseconds(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec))
}

func changeTimeNanoseconds(seconds, nanoseconds int64) int64 {
	const (
		nanosecondsPerSecond int64 = 1_000_000_000
		maxInt64             int64 = 1<<63 - 1
		minInt64             int64 = -1 << 63
		maxSeconds                 = maxInt64 / nanosecondsPerSecond
		minSeconds                 = minInt64/nanosecondsPerSecond - 1
		minNanoseconds             = nanosecondsPerSecond + minInt64%nanosecondsPerSecond
	)
	if nanoseconds < 0 || nanoseconds >= nanosecondsPerSecond || seconds < minSeconds || seconds > maxSeconds {
		return 0
	}
	if seconds == maxSeconds && nanoseconds > maxInt64%nanosecondsPerSecond {
		return 0
	}
	if seconds == minSeconds {
		if nanoseconds < minNanoseconds {
			return 0
		}
		// The seconds product underflows here even when the final value fits.
		return minInt64 + (nanoseconds - minNanoseconds)
	}
	return seconds*nanosecondsPerSecond + nanoseconds
}
