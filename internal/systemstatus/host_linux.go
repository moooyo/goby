//go:build linux

package systemstatus

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func nativeCPU() (cpuCounters, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, errObservation
	}
	return parseProcCPU(string(data))
}

func nativeMemory() (Memory, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}, errObservation
	}
	return parseProcMemory(string(data))
}

func nativeLoad() loadStatus {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return loadStatus{}
	}
	return parseProcLoad(string(data))
}

func nativeVolume(path string) (string, uint64, uint64, error) {
	var file unix.Stat_t
	if err := unix.Stat(path, &file); err != nil || file.Mode&unix.S_IFMT != unix.S_IFDIR {
		return "", 0, 0, errObservation
	}
	var status unix.Statfs_t
	if err := unix.Statfs(path, &status); err != nil || status.Bsize <= 0 {
		return "", 0, 0, errObservation
	}
	total, used, err := volumeBytes(status.Blocks, status.Bfree, uint64(status.Bsize))
	if err != nil {
		return "", 0, 0, err
	}
	// st_dev identifies the backing filesystem even through bind mounts. A
	// configured directory does not imply that it consumes the entire volume.
	return strconv.FormatUint(uint64(file.Dev), 10), total, used, nil
}
