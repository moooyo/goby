package systemstatus

import (
	"bufio"
	"math"
	"strconv"
	"strings"
)

// Linux proc parsers are platform-independent so fixtures exercise their
// arithmetic on every supported development host.
func parseProcCPU(data string) (cpuCounters, error) {
	line, _, _ := strings.Cut(data, "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, errObservation
	}
	var result cpuCounters
	// Guest and guest_nice already belong to user and nice. Count only the
	// first eight fields, including steal, and classify iowait as idle.
	for index, field := range fields[1:min(len(fields), 9)] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil || math.MaxUint64-result.total < value {
			return cpuCounters{}, errObservation
		}
		result.total += value
		if index == 3 || index == 4 {
			result.idle += value
		}
	}
	return result, nil
}

func parseProcMemory(data string) (Memory, error) {
	fields := map[string]uint64{}
	wanted := map[string]bool{"MemTotal": true, "MemAvailable": true, "Cached": true, "SReclaimable": true, "Shmem": true, "SwapTotal": true, "SwapFree": true}
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		key, raw, ok := strings.Cut(scanner.Text(), ":")
		if !ok || !wanted[key] {
			continue
		}
		values := strings.Fields(raw)
		if _, duplicate := fields[key]; duplicate || len(values) != 2 || values[1] != "kB" {
			return Memory{}, errObservation
		}
		value, err := strconv.ParseUint(values[0], 10, 64)
		if err != nil || value > math.MaxUint64/1024 {
			return Memory{}, errObservation
		}
		fields[key] = value * 1024
	}
	if scanner.Err() != nil || fields["MemTotal"] == 0 {
		return Memory{}, errObservation
	}
	total := fields["MemTotal"]
	result := Memory{TotalBytes: &total}
	if available, ok := fields["MemAvailable"]; ok {
		if available > total {
			return Memory{}, errObservation
		}
		used := total - available
		result.UsedBytes = &used
	}
	cached, hasCached := fields["Cached"]
	reclaimable, hasReclaimable := fields["SReclaimable"]
	shared, hasShared := fields["Shmem"]
	if hasCached && hasReclaimable && hasShared && math.MaxUint64-cached >= reclaimable && cached+reclaimable >= shared {
		cache := cached + reclaimable - shared
		result.CachedBytes = &cache
	}
	swap, hasSwap := fields["SwapTotal"]
	free, hasFree := fields["SwapFree"]
	if hasSwap && hasFree && free <= swap {
		used := swap - free
		result.SwapUsedBytes = &used
	}
	return result, nil
}

func parseProcLoad(data string) loadStatus {
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return loadStatus{}
	}
	var values [3]float64
	for i := range values {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return loadStatus{}
		}
		values[i] = value
	}
	return loadStatus{One: &values[0], Five: &values[1], Fifteen: &values[2]}
}
