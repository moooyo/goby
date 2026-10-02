//go:build linux

package transcode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func waitCacheJobReferences(t *testing.T, cache *cacheRoot, id string, count int) *cacheJobLock {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cache.jobsMu.Lock()
		job := cache.jobs[id]
		ready := job != nil && job.refs >= count
		cache.jobsMu.Unlock()
		if ready {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not acquire %d references", id, count)
	return nil
}

func holdCacheJob(t *testing.T, cache *cacheRoot, id string) func() {
	t.Helper()
	cache.mu.RLock()
	job := cache.lockJob(id)
	var once sync.Once
	release := func() { once.Do(func() { cache.unlockJob(id, job); cache.mu.RUnlock() }) }
	t.Cleanup(release)
	return release
}

func cacheLockingResult(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not complete", operation)
		return nil
	}
}

func TestCacheBlockedJobScanDoesNotBlockOtherJobFiles(t *testing.T) {
	cache := newTestCache(t)
	for _, id := range []string{cacheTestJobA, cacheTestJobB} {
		if err := cache.CreateJob(id); err != nil {
			t.Fatal(err)
		}
		writeCacheTestFile(t, filepath.Join(cache.RootPath(), id, "main.m3u8"), "playlist")
		writeCacheTestFile(t, filepath.Join(cache.RootPath(), id, "segment-0.ts"), "segment")
	}
	release := holdCacheJob(t, cache, cacheTestJobA)
	scan := make(chan error, 1)
	go func() { _, _, err := cache.ScanJob(cacheTestJobA); scan <- err }()
	jobLock := waitCacheJobReferences(t, cache, cacheTestJobA, 2)
	other := make(chan error, 1)
	go func() {
		file, err := cache.OpenJobFile(cacheTestJobB, "segment-0.ts")
		if err == nil {
			err = file.Close()
		}
		if err == nil {
			err = cache.RemoveJob(cacheTestJobB)
		}
		if err == nil {
			_, err = cache.FreeBytes()
		}
		other <- err
	}()
	if err := cacheLockingResult(t, other, "another job's open and cleanup"); err != nil {
		t.Fatal(err)
	}
	same := make(chan error, 1)
	go func() {
		file, err := cache.OpenJobFile(cacheTestJobA, "segment-0.ts")
		if err == nil {
			err = file.Close()
		}
		same <- err
	}()
	if got := waitCacheJobReferences(t, cache, cacheTestJobA, 3); got != jobLock {
		t.Fatal("waiting operations received different locks for the same job")
	}
	select {
	case err := <-scan:
		t.Fatalf("same-job scan escaped its lock: %v", err)
	case err := <-same:
		t.Fatalf("same-job open escaped its lock: %v", err)
	default:
	}
	release()
	if err := cacheLockingResult(t, scan, "same-job scan"); err != nil {
		t.Fatal(err)
	}
	if err := cacheLockingResult(t, same, "same-job open"); err != nil {
		t.Fatal(err)
	}
	cache.jobsMu.Lock()
	remaining := len(cache.jobs)
	cache.jobsMu.Unlock()
	if remaining != 0 {
		t.Fatalf("completed operations retained %d idle job locks", remaining)
	}
}

func TestCacheLifecycleWaitsForJobScan(t *testing.T) {
	for _, operation := range []string{"close", "recover"} {
		t.Run(operation, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			writeCacheTestFile(t, filepath.Join(cache.RootPath(), cacheTestJobA, "segment-0.ts"), "segment")
			release := holdCacheJob(t, cache, cacheTestJobA)
			scan := make(chan error, 1)
			go func() { _, _, err := cache.ScanJob(cacheTestJobA); scan <- err }()
			waitCacheJobReferences(t, cache, cacheTestJobA, 2)
			lifecycle := make(chan error, 1)
			entered := make(chan struct{})
			go func() {
				close(entered)
				if operation == "close" {
					lifecycle <- cache.Close()
				} else {
					lifecycle <- cache.Recover()
				}
			}()
			<-entered
			select {
			case err := <-lifecycle:
				t.Fatalf("%s passed an active job operation: %v", operation, err)
			case <-time.After(20 * time.Millisecond):
			}
			release()
			if err := cacheLockingResult(t, scan, "the scan before lifecycle mutation"); err != nil {
				t.Fatal(err)
			}
			if err := cacheLockingResult(t, lifecycle, operation); err != nil {
				t.Fatal(err)
			}
			if operation == "close" {
				if err := cache.CreateJob(cacheTestJobB); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("job creation after close = %v", err)
				}
			} else {
				if _, err := os.Stat(filepath.Join(cache.RootPath(), cacheTestJobA)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("recovery retained the scanned job: %v", err)
				}
				if err := cache.CreateJob(cacheTestJobA); err != nil {
					t.Fatalf("job ID could not be reused after recovery: %v", err)
				}
			}
		})
	}
}

func TestCacheConcurrentJobCreateRemoveAndOpen(t *testing.T) {
	cache := newTestCache(t)
	const jobs = 8
	results := make(chan error, jobs)
	for index := range jobs {
		go func() {
			id := fmt.Sprintf("%032x", index+1)
			for iteration := 0; iteration < 30; iteration++ {
				if err := cache.CreateJob(id); err != nil {
					results <- err
					return
				}
				if err := os.WriteFile(filepath.Join(cache.RootPath(), id, "segment-0.ts"), []byte("segment"), 0o600); err != nil {
					results <- err
					return
				}
				opened := make(chan error, 1)
				go func() {
					file, err := cache.OpenJobFile(id, "segment-0.ts")
					if err == nil {
						err = file.Close()
					}
					opened <- err
				}()
				_, _, scanErr := cache.ScanJob(id)
				openErr := <-opened
				if err := errors.Join(scanErr, openErr); err != nil {
					results <- err
					return
				}
				if err := cache.RemoveJob(id); err != nil {
					results <- err
					return
				}
			}
			results <- nil
		}()
	}
	for range jobs {
		if err := cacheLockingResult(t, results, "concurrent cache worker"); err != nil {
			t.Fatal(err)
		}
	}
	cache.jobsMu.Lock()
	defer cache.jobsMu.Unlock()
	if len(cache.jobs) != 0 {
		t.Fatalf("job lock registry grew across repeated ID reuse: %d", len(cache.jobs))
	}
}
