package systemstatus

import (
	"testing"
)

func TestProcCPUExcludesDoubleCountedGuestTime(t *testing.T) {
	got, err := parseProcCPU("cpu  100 20 30 400 50 6 7 8 90 10\ncpu0 1 2 3 4\n")
	if err != nil || got != (cpuCounters{total: 621, idle: 450}) {
		t.Fatalf("incorrect aggregate counters: %+v, %v", got, err)
	}
	for _, bad := range []string{"", "cpu0 1 2 3 4", "cpu 1 2 3", "cpu 1 2 -3 4", "cpu 18446744073709551615 1 0 0"} {
		if _, err := parseProcCPU(bad); err == nil {
			t.Fatalf("invalid CPU sample accepted: %q", bad)
		}
	}
}

func TestProcMemoryAccountsForReclaimableAndSharedPages(t *testing.T) {
	got, err := parseProcMemory("MemTotal: 1000 kB\nMemAvailable: 600 kB\nCached: 300 kB\nSReclaimable: 50 kB\nShmem: 20 kB\nSwapTotal: 200 kB\nSwapFree: 100 kB\n")
	if err != nil || *got.TotalBytes != 1024000 || *got.UsedBytes != 409600 || *got.CachedBytes != 337920 || *got.SwapUsedBytes != 102400 {
		t.Fatalf("incorrect proc memory calculation: %+v, %v", got, err)
	}
	partial, err := parseProcMemory("MemTotal: 1000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	if err != nil || partial.TotalBytes == nil || partial.UsedBytes != nil || partial.CachedBytes != nil || partial.SwapUsedBytes == nil || *partial.SwapUsedBytes != 0 {
		t.Fatalf("missing fields became zero-valued observations: %+v, %v", partial, err)
	}
	for _, bad := range []string{"", "MemTotal: 0 kB", "MemTotal: 100 bytes", "MemTotal: 100 kB\nMemAvailable: 200 kB", "MemTotal: 100 kB\nMemTotal: 100 kB", "MemTotal: 18446744073709551615 kB"} {
		if _, err := parseProcMemory(bad); err == nil {
			t.Fatalf("invalid memory sample accepted: %q", bad)
		}
	}
}

func TestProcLoadRejectsNonFiniteValues(t *testing.T) {
	got := parseProcLoad("1.42 0.98 0.87 1/200 123")
	if got.One == nil || *got.One != 1.42 || *got.Five != 0.98 || *got.Fifteen != 0.87 {
		t.Fatalf("incorrect load averages: %+v", got)
	}
	for _, bad := range []string{"", "1 2", "NaN 1 2", "1 +Inf 2", "1 2 -1", "1e309 1 2"} {
		if got := parseProcLoad(bad); got.One != nil || got.Five != nil || got.Fifteen != nil {
			t.Fatalf("invalid load accepted: %q", bad)
		}
	}
}
