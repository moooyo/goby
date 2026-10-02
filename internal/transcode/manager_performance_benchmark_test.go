//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// BenchmarkManagerParallelHLSRead can be copied unchanged into the baseline.
// Kernel open notifications count sentinel inspections independently of
// implementation hooks, including bounded audits. Readers never open it.
func BenchmarkManagerParallelHLSRead(b *testing.B) {
	benchmarkManagerParallelHLSRead(b, time.Second)
}

// BenchmarkManagerParallelHLSRead250ms retains the production scan interval
// so directory inspection competes with artifact opens during the measurement.
func BenchmarkManagerParallelHLSRead250ms(b *testing.B) {
	benchmarkManagerParallelHLSRead(b, 250*time.Millisecond)
}

func benchmarkManagerParallelHLSRead(b *testing.B, pollInterval time.Duration) {
	for _, segments := range []int{128, 1024} {
		b.Run(fmt.Sprintf("segments-%d", segments), func(b *testing.B) {
			options := Options{Root: filepath.Join(b.TempDir(), "cache"), FFmpegPath: "/not-executed/ffmpeg",
				Repository: &managerTestRepository{}, MaxReaders: 512, MaxJobReaders: 256,
				IdleTimeout: time.Hour, MinFreeBytes: 1, pollInterval: pollInterval}
			m, err := NewManager(context.Background(), options)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := m.Close(ctx); err != nil {
					b.Error(err)
				}
			})
			const jobs = 8
			var records [jobs]Record
			var directories []string
			now := time.Now().UTC()
			for index := range jobs {
				id := fmt.Sprintf("%032x", index+1)
				if err := m.cache.CreateJob(id); err != nil {
					b.Fatal(err)
				}
				directory := filepath.Join(options.Root, id)
				directories = append(directories, directory)
				for segment := range segments {
					if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("segment-%06d.ts", segment)), make([]byte, 188), 0o600); err != nil {
						b.Fatal(err)
					}
				}
				playlist := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:3,\nsegment-000000.ts\n#EXT-X-ENDLIST\n")
				if err := os.WriteFile(filepath.Join(directory, "main.m3u8"), playlist, 0o600); err != nil {
					b.Fatal(err)
				}
				var size int64
				var ready bool
				spec := managerTestSpec(index + 1)
				if finalizer, ok := any(m.cache).(interface {
					FinalizePlanJob(string, Plan) (int64, bool, error)
				}); ok {
					size, ready, err = finalizer.FinalizePlanJob(id, spec.Plan)
				} else {
					size, ready, err = m.cache.ScanPlanJob(id, spec.Plan)
				}
				if err != nil || !ready {
					b.Fatalf("completed cache boundary: %d, %t, %v", size, ready, err)
				}
				record := Record{ID: id, Spec: spec, State: "completed",
					CreatedAt: now, UpdatedAt: now, LastAccessAt: now, OutputBytes: size}
				jobContext, cancelJob := context.WithCancel(m.ctx)
				j := &managedJob{record: record, ctx: jobContext, cancel: cancelJob, directory: true, ready: true, finished: true,
					created: make(chan struct{}), launch: make(chan struct{}), done: make(chan struct{}), changed: make(chan struct{})}
				close(j.created)
				close(j.done)
				m.mu.Lock()
				m.jobs[id], m.bySpec[record.Spec] = j, j
				m.bytes += record.OutputBytes
				m.mu.Unlock()
				records[index] = record
			}
			stopCounting, scanCount := benchmarkCacheScanCounter(b, directories, fmt.Sprintf("segment-%06d.ts", segments-1))
			var sequence atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					record := records[sequence.Add(1)%jobs]
					handle, err := m.TryOpen(record.Spec.Scope, record.ID, "segment-000000.ts")
					if err != nil {
						b.Error(err)
						return
					}
					if err := handle.Close(); err != nil {
						b.Error(err)
						return
					}
				}
			})
			b.StopTimer()
			m.cancel()
			<-m.loopDone
			stopCounting()
			b.ReportMetric(float64(scanCount.Load())/float64(b.N), "sentinel-opens/op")
		})
	}
}

func benchmarkCacheScanCounter(b *testing.B, directories []string, sentinel string) (func(), *atomic.Uint64) {
	b.Helper()
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		b.Fatal(err)
	}
	for _, directory := range directories {
		if _, err := unix.InotifyAddWatch(fd, directory, unix.IN_OPEN); err != nil {
			_ = unix.Close(fd)
			b.Fatal(err)
		}
	}
	count := &atomic.Uint64{}
	stop, done := make(chan struct{}), make(chan error, 1)
	go func() {
		var buffer [64 * 1024]byte
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		for {
			if _, err := unix.Poll(poll, 10); err != nil && !errors.Is(err, unix.EINTR) {
				done <- err
				return
			}
			for {
				n, err := unix.Read(fd, buffer[:])
				if errors.Is(err, unix.EAGAIN) {
					break
				}
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil {
					done <- err
					return
				}
				for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
					event := (*unix.InotifyEvent)(unsafe.Pointer(&buffer[offset]))
					end := offset + unix.SizeofInotifyEvent + int(event.Len)
					if end > n || event.Mask&unix.IN_Q_OVERFLOW != 0 {
						done <- errors.New("cache scan notification overflow")
						return
					}
					name := strings.TrimRight(string(buffer[offset+unix.SizeofInotifyEvent:end]), "\x00")
					if name == sentinel {
						count.Add(1)
					}
					offset = end
				}
			}
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
		}
	}()
	return func() {
		close(stop)
		err := <-done
		_ = unix.Close(fd)
		if err != nil {
			b.Error(err)
		}
	}, count
}
