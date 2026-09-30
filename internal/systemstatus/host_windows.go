//go:build windows

package systemstatus

import (
	"math"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	statusKernel32             = windows.NewLazySystemDLL("kernel32.dll")
	statusGetSystemTimes       = statusKernel32.NewProc("GetSystemTimes")
	statusGlobalMemoryStatusEx = statusKernel32.NewProc("GlobalMemoryStatusEx")
	statusGetProcessorGroups   = statusKernel32.NewProc("GetActiveProcessorGroupCount")
	statusGetPerformanceInfo   = windows.NewLazySystemDLL("psapi.dll").NewProc("GetPerformanceInfo")
)

func nativeCPU() (cpuCounters, error) {
	// GetSystemTimes only covers the caller's processor group on machines with
	// more than one group. A partial-group reading cannot claim host-wide use.
	groups, _, _ := statusGetProcessorGroups.Call()
	if groups != 1 {
		return cpuCounters{}, errObservation
	}
	var idle, kernel, user windows.Filetime
	result, _, _ := statusGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if result == 0 {
		return cpuCounters{}, errObservation
	}
	ticks := func(value windows.Filetime) uint64 { return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime) }
	kernelTicks, userTicks, idleTicks := ticks(kernel), ticks(user), ticks(idle)
	if math.MaxUint64-kernelTicks < userTicks || idleTicks > kernelTicks {
		return cpuCounters{}, errObservation
	}
	// Kernel time includes idle time; adding idle again would understate use.
	return cpuCounters{total: kernelTicks + userTicks, idle: idleTicks}, nil
}

type windowsMemoryStatus struct {
	Length, MemoryLoad               uint32
	TotalPhysical, AvailablePhysical uint64
	TotalPageFile, AvailablePageFile uint64
	TotalVirtual, AvailableVirtual   uint64
	AvailableExtendedVirtual         uint64
}

type windowsPerformanceInformation struct {
	Size                                               uint32
	CommitTotal, CommitLimit, CommitPeak               uintptr
	PhysicalTotal, PhysicalAvailable, SystemCache      uintptr
	KernelTotal, KernelPaged, KernelNonpaged, PageSize uintptr
	HandleCount, ProcessCount, ThreadCount             uint32
}

func nativeMemory() (Memory, error) {
	status := windowsMemoryStatus{}
	status.Length = uint32(unsafe.Sizeof(status))
	result, _, _ := statusGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 || status.TotalPhysical == 0 || status.AvailablePhysical > status.TotalPhysical {
		return Memory{}, errObservation
	}
	used := status.TotalPhysical - status.AvailablePhysical
	memory := Memory{TotalBytes: &status.TotalPhysical, UsedBytes: &used}
	performance := windowsPerformanceInformation{}
	performance.Size = uint32(unsafe.Sizeof(performance))
	result, _, _ = statusGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&performance)), uintptr(performance.Size))
	if result != 0 && performance.PageSize > 0 && uint64(performance.SystemCache) <= math.MaxUint64/uint64(performance.PageSize) {
		cached := uint64(performance.SystemCache) * uint64(performance.PageSize)
		memory.CachedBytes = &cached
	}
	// Commit charge is not resident swap use. Windows has no matching value in
	// these APIs, so do not present page-file capacity or commit charge as swap.
	return memory, nil
}

func nativeLoad() loadStatus { return loadStatus{} }

func nativeVolume(path string) (string, uint64, uint64, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", 0, 0, errObservation
	}
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", 0, 0, errObservation
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(encoded, &available, &total, &free); err != nil || total == 0 || available > total {
		return "", 0, 0, errObservation
	}
	// Volume GUIDs deduplicate separate mount points on the same volume. UNC
	// shares do not have GUIDs, so retain the normalized share mount path.
	root := make([]uint16, 32768)
	if err := windows.GetVolumePathName(encoded, &root[0], uint32(len(root))); err != nil {
		return "", 0, 0, errObservation
	}
	key := strings.ToLower(windows.UTF16ToString(root))
	name := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(&root[0], &name[0], uint32(len(name))); err == nil {
		key = strings.ToLower(windows.UTF16ToString(name))
	}
	// Both values honor the service account's disk quota. Mixing free bytes
	// for the entire volume with a quota-adjusted total can fabricate usage.
	return key, total, total - available, nil
}
