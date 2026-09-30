// Package systemstatus provides read-only host telemetry for the administrator
// dashboard. Missing observations stay null instead of becoming healthy zeros.
package systemstatus

import (
	"errors"
	"math"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Host struct {
	OS           string
	Architecture string
	CPUCount     int
}

type CPU struct {
	UsagePercent *float64
	Load1        *float64
	Load5        *float64
	Load15       *float64
}

type Memory struct {
	TotalBytes    *uint64
	UsedBytes     *uint64
	CachedBytes   *uint64
	SwapUsedBytes *uint64
}

type Volume struct {
	Path       string
	Available  bool
	TotalBytes *uint64
	UsedBytes  *uint64
}

type Storage struct {
	Complete   bool
	TotalBytes *uint64
	UsedBytes  *uint64
	Volumes    []Volume
}

type Snapshot struct {
	Timestamp     time.Time
	UptimeSeconds int64
	Host          Host
	CPU           CPU
	Memory        Memory
	Storage       Storage
}

type cpuCounters struct{ total, idle uint64 }
type loadStatus struct{ One, Five, Fifteen *float64 }

type readers struct {
	cpu    func() (cpuCounters, error)
	memory func() (Memory, error)
	load   func() loadStatus
	volume func(string) (string, uint64, uint64, error)
}

const (
	minimumSampleInterval = time.Second
	maximumSampleAge      = 15 * time.Second
)

// Collector keeps all operating-system I/O outside requests and its mutex.
// At most one sampling goroutine may exist, even when a filesystem operation
// hangs indefinitely. Requests receive a bounded-age observation or nulls.
type Collector struct {
	mu          sync.Mutex
	started     time.Time
	roots       []string
	read        readers
	previousCPU cpuCounters
	cpuTime     time.Time
	cpuValid    bool
	cached      Snapshot
	cachedAt    time.Time
	hasCached   bool
	lastStarted time.Time
	sampling    chan struct{}
	closed      bool
}

func New(roots []string) *Collector {
	return newCollector(roots, readers{nativeCPU, nativeMemory, nativeLoad, nativeVolume}, time.Now())
}

func newCollector(roots []string, read readers, now time.Time) *Collector {
	c := &Collector{started: now, roots: append([]string(nil), roots...), read: read}
	c.snapshot(now)
	return c
}

func (c *Collector) Snapshot() Snapshot { return c.snapshot(time.Now()) }

// Close stops admission and publication without waiting for an uninterruptible
// kernel filesystem call. A previously admitted worker owns no HTTP request.
func (c *Collector) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}

func (c *Collector) snapshot(now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed && c.sampling == nil && (c.lastStarted.IsZero() || now.Sub(c.lastStarted) >= minimumSampleInterval || now.Before(c.lastStarted)) {
		c.lastStarted, c.sampling = now, make(chan struct{})
		go c.collect(now)
	}
	if !c.closed && c.hasCached && now.Sub(c.cachedAt) >= 0 && now.Sub(c.cachedAt) <= maximumSampleAge {
		result := cloneSnapshot(c.cached)
		result.UptimeSeconds = max(0, int64(now.Sub(c.started)/time.Second))
		return result
	}
	result := c.emptySnapshot(now)
	if !c.lastStarted.IsZero() {
		result.Timestamp = c.lastStarted.UTC()
	}
	return result
}

func (c *Collector) emptySnapshot(now time.Time) Snapshot {
	return Snapshot{
		Timestamp: now.UTC(), UptimeSeconds: max(0, int64(now.Sub(c.started)/time.Second)),
		Host:    Host{OS: runtime.GOOS, Architecture: runtime.GOARCH, CPUCount: runtime.NumCPU()},
		Storage: sampleStorage(c.roots, func(string) (string, uint64, uint64, error) { return "", 0, 0, errObservation }),
	}
}

func (c *Collector) collect(now time.Time) {
	result := c.emptySnapshot(now)
	// Counter state belongs exclusively to the one admitted worker. The sample
	// timestamp is its start, so slow I/O cannot make old observations look new.
	current, cpuErr := c.read.cpu()
	if cpuErr != nil || current.idle > current.total {
		c.cpuValid = false
	} else {
		if c.cpuValid && now.Sub(c.cpuTime) >= minimumSampleInterval {
			result.CPU.UsagePercent = cpuUsage(c.previousCPU, current)
		}
		c.previousCPU, c.cpuTime, c.cpuValid = current, now, true
	}
	load := c.read.load()
	result.CPU.Load1, result.CPU.Load5, result.CPU.Load15 = load.One, load.Five, load.Fifteen
	if memory, err := c.read.memory(); err == nil {
		result.Memory = memory
	}
	result.Storage = sampleStorage(c.roots, c.read.volume)
	c.mu.Lock()
	if !c.closed {
		c.cached, c.cachedAt, c.hasCached = result, now, true
	}
	close(c.sampling)
	c.sampling = nil
	c.mu.Unlock()
}

func cpuUsage(previous, current cpuCounters) *float64 {
	if current.total <= previous.total || current.idle < previous.idle {
		return nil
	}
	total, idle := current.total-previous.total, current.idle-previous.idle
	if idle > total {
		return nil
	}
	value := 100 * float64(total-idle) / float64(total)
	return &value
}

func sampleStorage(roots []string, read func(string) (string, uint64, uint64, error)) Storage {
	result := Storage{Complete: true, Volumes: make([]Volume, 0, len(roots))}
	seenPaths, seenVolumes := map[string]bool{}, map[string]bool{}
	var total, used uint64
	for _, root := range roots {
		path := filepath.Clean(root)
		if seenPaths[path] {
			continue
		}
		seenPaths[path] = true
		volume := Volume{Path: path}
		key, capacity, occupied, err := read(path)
		if err != nil || key == "" || capacity == 0 || occupied > capacity {
			result.Complete = false
		} else {
			volume.Available, volume.TotalBytes, volume.UsedBytes = true, &capacity, &occupied
			if !seenVolumes[key] {
				seenVolumes[key] = true
				if math.MaxUint64-total < capacity || math.MaxUint64-used < occupied {
					result.Complete = false
				} else {
					total, used = total+capacity, used+occupied
				}
			}
		}
		result.Volumes = append(result.Volumes, volume)
	}
	// No configured media directory is a real empty set, not the system disk.
	if result.Complete {
		result.TotalBytes, result.UsedBytes = &total, &used
	}
	return result
}

func cloneSnapshot(value Snapshot) Snapshot {
	value.CPU.UsagePercent = clonePointer(value.CPU.UsagePercent)
	value.CPU.Load1, value.CPU.Load5, value.CPU.Load15 = clonePointer(value.CPU.Load1), clonePointer(value.CPU.Load5), clonePointer(value.CPU.Load15)
	value.Memory.TotalBytes, value.Memory.UsedBytes = clonePointer(value.Memory.TotalBytes), clonePointer(value.Memory.UsedBytes)
	value.Memory.CachedBytes, value.Memory.SwapUsedBytes = clonePointer(value.Memory.CachedBytes), clonePointer(value.Memory.SwapUsedBytes)
	value.Storage.TotalBytes, value.Storage.UsedBytes = clonePointer(value.Storage.TotalBytes), clonePointer(value.Storage.UsedBytes)
	value.Storage.Volumes = append([]Volume{}, value.Storage.Volumes...)
	for i := range value.Storage.Volumes {
		volume := &value.Storage.Volumes[i]
		volume.TotalBytes, volume.UsedBytes = clonePointer(volume.TotalBytes), clonePointer(volume.UsedBytes)
	}
	return value
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var errObservation = errors.New("host observation unavailable")

func volumeBytes(blocks, free, blockSize uint64) (uint64, uint64, error) {
	if blocks == 0 || blockSize == 0 || free > blocks || blocks > math.MaxUint64/blockSize {
		return 0, 0, errObservation
	}
	return blocks * blockSize, (blocks - free) * blockSize, nil
}
