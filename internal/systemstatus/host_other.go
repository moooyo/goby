//go:build !linux && !windows

package systemstatus

func nativeCPU() (cpuCounters, error)                     { return cpuCounters{}, errObservation }
func nativeMemory() (Memory, error)                       { return Memory{}, errObservation }
func nativeLoad() loadStatus                              { return loadStatus{} }
func nativeVolume(string) (string, uint64, uint64, error) { return "", 0, 0, errObservation }
