package systemstatus

import (
	"encoding/json"
	"math"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCPUUsageUsesCounterDifferences(t *testing.T) {
	for _, test := range []struct {
		name              string
		previous, current cpuCounters
		want              *float64
	}{
		{"busy interval", cpuCounters{1000, 500}, cpuCounters{1200, 650}, pointer(25.0)},
		{"idle interval", cpuCounters{1000, 500}, cpuCounters{1200, 700}, pointer(0.0)},
		{"fully occupied", cpuCounters{1000, 500}, cpuCounters{1200, 500}, pointer(100.0)},
		{"no interval", cpuCounters{1000, 500}, cpuCounters{1000, 500}, nil},
		{"total reset", cpuCounters{1000, 500}, cpuCounters{900, 600}, nil},
		{"idle reset", cpuCounters{1000, 500}, cpuCounters{1100, 400}, nil},
		{"invalid idle", cpuCounters{1000, 500}, cpuCounters{1100, 700}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := cpuUsage(test.previous, test.current); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("cpuUsage() = %v, want %v", got, test.want)
			}
		})
	}
}

func pointer[T any](value T) *T { return &value }

func testReaders() readers {
	return readers{
		cpu: func() (cpuCounters, error) { return cpuCounters{1000, 500}, nil },
		memory: func() (Memory, error) {
			return Memory{TotalBytes: pointer(uint64(100)), UsedBytes: pointer(uint64(40))}, nil
		},
		load:   func() loadStatus { return loadStatus{One: pointer(1.0)} },
		volume: func(string) (string, uint64, uint64, error) { return "disk", 100, 40, nil },
	}
}

func waitForSample(t *testing.T, c *Collector) {
	t.Helper()
	c.mu.Lock()
	done := c.sampling
	c.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("host sample did not complete")
	}
}

func completedSnapshot(t *testing.T, c *Collector, now time.Time) Snapshot {
	t.Helper()
	c.snapshot(now)
	waitForSample(t, c)
	return c.snapshot(now)
}

func TestCollectorCachesObservationsWithoutSharingMutableData(t *testing.T) {
	started := time.Now()
	reads, calls := testReaders(), 0
	reads.cpu = func() (cpuCounters, error) {
		calls++
		return cpuCounters{1000 + uint64(calls)*100, 500 + uint64(calls)*50}, nil
	}
	c := newCollector([]string{"media"}, reads, started)
	t.Cleanup(c.Close)
	first := completedSnapshot(t, c, started)
	if first.CPU.UsagePercent != nil || first.UptimeSeconds != 0 {
		t.Fatalf("initial sample fabricated current CPU use: %+v", first)
	}
	*first.Memory.TotalBytes = 999
	*first.CPU.Load1 = 999
	first.Storage.Volumes[0].Path = "changed"
	*first.Storage.Volumes[0].UsedBytes = 999
	*first.Storage.TotalBytes = 999
	second := c.snapshot(started.Add(500 * time.Millisecond))
	if calls != 1 || *second.Memory.TotalBytes != 100 || *second.CPU.Load1 != 1 || second.Storage.Volumes[0].Path != "media" ||
		*second.Storage.Volumes[0].UsedBytes != 40 || *second.Storage.TotalBytes != 100 || second.Timestamp != first.Timestamp {
		t.Fatalf("cached state was reread or mutated through a caller: %+v, calls=%d", second, calls)
	}
	third := completedSnapshot(t, c, started.Add(2*time.Second))
	if calls != 2 || third.CPU.UsagePercent == nil || *third.CPU.UsagePercent != 50 || third.UptimeSeconds != 2 {
		t.Fatalf("elapsed sample did not report an interval: %+v, calls=%d", third, calls)
	}
}

func TestCollectorDoesNotBridgeCPUFailures(t *testing.T) {
	started := time.Now()
	reads, calls := testReaders(), 0
	reads.cpu = func() (cpuCounters, error) {
		calls++
		if calls == 2 {
			return cpuCounters{}, errObservation
		}
		return cpuCounters{uint64(calls) * 100, uint64(calls) * 50}, nil
	}
	c := newCollector(nil, reads, started)
	t.Cleanup(c.Close)
	waitForSample(t, c)
	if got := completedSnapshot(t, c, started.Add(time.Second)); got.CPU.UsagePercent != nil {
		t.Fatal("failed CPU read reported utilization")
	}
	if got := completedSnapshot(t, c, started.Add(2*time.Second)); got.CPU.UsagePercent != nil {
		t.Fatal("recovery bridged failed CPU read")
	}
	if got := completedSnapshot(t, c, started.Add(3*time.Second)); got.CPU.UsagePercent == nil || *got.CPU.UsagePercent != 50 {
		t.Fatal("CPU interval did not recover")
	}
}

func TestCollectorConcurrentPollsCoalesce(t *testing.T) {
	started := time.Now()
	reads, calls := testReaders(), 0
	reads.volume = func(string) (string, uint64, uint64, error) { calls++; return "disk", 100, 40, nil }
	c := newCollector([]string{"media"}, reads, started)
	t.Cleanup(c.Close)
	waitForSample(t, c)
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() { defer group.Done(); c.snapshot(started.Add(time.Second)) }()
	}
	group.Wait()
	waitForSample(t, c)
	if calls != 2 {
		t.Fatalf("concurrent requests collected %d host snapshots", calls)
	}
}

func TestCollectorBlockedVolumeReturnsPromptlyWithoutMultiplyingWorkers(t *testing.T) {
	started := time.Now()
	reads := testReaders()
	var calls atomic.Int32
	entered, release := make(chan struct{}, 100), make(chan struct{})
	reads.volume = func(string) (string, uint64, uint64, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return "disk", 100, 40, nil
	}
	c := newCollector([]string{"network-mount"}, reads, started)
	t.Cleanup(c.Close)
	defer close(release)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("blocked probe did not start")
	}
	results := make(chan Snapshot, 64)
	for i := range 64 {
		go func() { results <- c.snapshot(started.Add(time.Duration(i+1) * time.Second)) }()
	}
	deadline := time.After(time.Second)
	for range 64 {
		select {
		case got := <-results:
			if got.Memory.TotalBytes != nil || got.Storage.Complete || got.Storage.TotalBytes != nil ||
				len(got.Storage.Volumes) != 1 || got.Storage.Volumes[0].Available || got.Storage.Volumes[0].Path != "network-mount" {
				t.Fatal("blocked initial sample fabricated observations")
			}
		case <-deadline:
			t.Fatal("a blocked filesystem operation held an administrator request")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("blocked filesystem accumulated %d workers", calls.Load())
	}
}

func TestCollectorExpiresPriorSampleAndRecoversAfterBlockedRefresh(t *testing.T) {
	started := time.Now()
	reads := testReaders()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	reads.volume = func(string) (string, uint64, uint64, error) {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return "disk", 100, 40, nil
	}
	c := newCollector([]string{"network-mount"}, reads, started)
	t.Cleanup(c.Close)
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	waitForSample(t, c)
	prior := c.snapshot(started.Add(5 * time.Second))
	if prior.Storage.TotalBytes == nil || *prior.Storage.TotalBytes != 100 || prior.Timestamp != started.UTC() {
		t.Fatal("bounded-age observation disappeared during refresh")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not enter the blocked filesystem probe")
	}
	expiredAt := started.Add(maximumSampleAge + time.Second)
	expired := c.snapshot(expiredAt)
	if expired.Memory.TotalBytes != nil || expired.CPU.Load1 != nil || expired.Storage.Complete || expired.Storage.TotalBytes != nil || calls.Load() != 2 {
		t.Fatal("expired sample was reported as current or a second blocked worker was started")
	}
	released.Do(func() { close(release) })
	waitForSample(t, c)
	// The formerly blocked worker retains its original sample time. A fresh
	// completed observation is needed after it has exceeded the age limit.
	recovered := completedSnapshot(t, c, expiredAt.Add(5*time.Second))
	if recovered.Memory.TotalBytes == nil || recovered.Storage.TotalBytes == nil || *recovered.Storage.TotalBytes != 100 ||
		!recovered.Storage.Complete || !recovered.Storage.Volumes[0].Available || calls.Load() != 3 {
		t.Fatal("recovered probe did not publish real observations")
	}
}

func TestCollectorFiveSecondPollingKeepsCompletedObservations(t *testing.T) {
	started := time.Now()
	c := newCollector([]string{"media"}, testReaders(), started)
	t.Cleanup(c.Close)
	waitForSample(t, c)
	for step := 1; step <= 5; step++ {
		now := started.Add(time.Duration(step) * 5 * time.Second)
		got := c.snapshot(now)
		if got.Memory.TotalBytes == nil || got.Storage.TotalBytes == nil || !got.Storage.Complete ||
			got.Timestamp != now.Add(-5*time.Second).UTC() {
			t.Fatalf("five-second polling lost the preceding completed sample at step %d: %+v", step, got)
		}
		waitForSample(t, c)
	}
}

func TestCollectorCloseDoesNotWaitForBlockedProbeOrAdmitAnother(t *testing.T) {
	started := time.Now()
	reads := testReaders()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	reads.volume = func(string) (string, uint64, uint64, error) {
		calls.Add(1)
		close(entered)
		<-release
		return "disk", 100, 40, nil
	}
	c := newCollector([]string{"network-mount"}, reads, started)
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("collector shutdown waited for filesystem I/O")
	}
	released.Do(func() { close(release) })
	waitForSample(t, c)
	got := c.snapshot(started.Add(time.Minute))
	if got.Storage.TotalBytes != nil || got.Storage.Complete || calls.Load() != 1 {
		t.Fatal("closed collector published a late result or admitted another worker")
	}
}

func TestStorageDeduplicatesVolumesAndPreservesUnavailableRoots(t *testing.T) {
	read := func(path string) (string, uint64, uint64, error) {
		switch path {
		case "a", "b":
			return "volume-one", 100, 30, nil
		case "c":
			return "volume-two", 200, 80, nil
		default:
			return "", 0, 0, errObservation
		}
	}
	got := sampleStorage([]string{"a", "a", "b", "c"}, read)
	if !got.Complete || len(got.Volumes) != 3 || *got.TotalBytes != 300 || *got.UsedBytes != 110 {
		t.Fatalf("backing volumes counted more than once: %+v", got)
	}
	partial := sampleStorage([]string{"a", "lost"}, read)
	if partial.Complete || partial.TotalBytes != nil || partial.UsedBytes != nil || !partial.Volumes[0].Available || partial.Volumes[1].Available || partial.Volumes[1].UsedBytes != nil {
		t.Fatalf("partial storage observation presented as complete: %+v", partial)
	}
	empty := sampleStorage(nil, read)
	if !empty.Complete || *empty.TotalBytes != 0 || *empty.UsedBytes != 0 || empty.Volumes == nil {
		t.Fatalf("empty configuration invented a filesystem: %+v", empty)
	}
}

func TestStorageRejectsOverflowAndImpossibleCapacity(t *testing.T) {
	for _, values := range [][2]uint64{{0, 0}, {100, 101}} {
		got := sampleStorage([]string{"a"}, func(string) (string, uint64, uint64, error) { return "disk", values[0], values[1], nil })
		if got.Complete || got.TotalBytes != nil || got.Volumes[0].Available {
			t.Fatalf("invalid capacity accepted: %+v", got)
		}
	}
	got := sampleStorage([]string{"a", "b"}, func(path string) (string, uint64, uint64, error) { return path, math.MaxUint64, 0, nil })
	if got.Complete || got.TotalBytes != nil {
		t.Fatal("storage aggregate overflowed")
	}
	for _, values := range [][3]uint64{{0, 0, 1}, {1, 0, 0}, {1, 2, 1}, {math.MaxUint64, 0, 2}} {
		if _, _, err := volumeBytes(values[0], values[1], values[2]); err == nil {
			t.Fatalf("invalid filesystem counters accepted: %v", values)
		}
	}
}

func TestUnavailableObservationsSerializeAsNull(t *testing.T) {
	reads := testReaders()
	reads.cpu = func() (cpuCounters, error) { return cpuCounters{}, errObservation }
	reads.memory = func() (Memory, error) { return Memory{}, errObservation }
	reads.load = func() loadStatus { return loadStatus{} }
	reads.volume = func(string) (string, uint64, uint64, error) { return "", 0, 0, errObservation }
	c := newCollector([]string{"missing"}, reads, time.Now())
	t.Cleanup(c.Close)
	waitForSample(t, c)
	encoded, err := json.Marshal(c.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"UsagePercent", "Load1", "Load5", "Load15", "TotalBytes", "UsedBytes", "CachedBytes", "SwapUsedBytes"} {
		if !strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Fatalf("%s did not preserve unknown state: %s", field, encoded)
		}
	}
}

func TestNativeSnapshotReportsMemoryAndMediaVolume(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native telemetry is implemented for Windows and Linux")
	}
	c := New([]string{t.TempDir()})
	t.Cleanup(c.Close)
	waitForSample(t, c)
	got := c.Snapshot()
	if got.Host.CPUCount < 1 || got.Host.OS != runtime.GOOS || got.Memory.TotalBytes == nil || got.Memory.UsedBytes == nil ||
		*got.Memory.TotalBytes == 0 || *got.Memory.UsedBytes > *got.Memory.TotalBytes {
		t.Fatalf("native memory observation was unavailable or inconsistent: %+v", got)
	}
	if !got.Storage.Complete || got.Storage.TotalBytes == nil || *got.Storage.TotalBytes == 0 || len(got.Storage.Volumes) != 1 ||
		!got.Storage.Volumes[0].Available || *got.Storage.UsedBytes > *got.Storage.TotalBytes {
		t.Fatalf("native temporary directory volume was unavailable or inconsistent: %+v", got.Storage)
	}
	if runtime.GOOS == "windows" && (got.CPU.Load1 != nil || got.Memory.SwapUsedBytes != nil) {
		t.Fatal("Windows fabricated Linux-only observations")
	}
}
