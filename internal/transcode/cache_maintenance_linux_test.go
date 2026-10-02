//go:build linux

package transcode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func cacheMaintenanceUntilKnown(t *testing.T, cache *cacheRoot, id string, plan Plan, budget int) cacheMaintenanceResult {
	t.Helper()
	for attempt := 0; attempt < maxJobFiles; attempt++ {
		result, err := cache.MaintainPlanJob(id, plan, budget)
		if err != nil {
			t.Fatal(err)
		}
		if result.work > budget {
			t.Fatalf("maintenance spent %d units from a %d-unit budget", result.work, budget)
		}
		if !result.pending {
			return result
		}
	}
	t.Fatal("bounded maintenance did not close its accounting")
	return cacheMaintenanceResult{}
}

func cacheMaintenanceInventory(t *testing.T, cache *cacheRoot, id string) *cacheInventory {
	t.Helper()
	cache.jobsMu.Lock()
	s := cache.maintenance[id]
	cache.jobsMu.Unlock()
	if s == nil {
		t.Fatal("job has no maintenance inventory")
	}
	return s
}

func cacheMaintenanceOutput(t *testing.T, count int) *cacheRoot {
	t.Helper()
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(cache.RootPath(), cacheTestJobA)
	for index := range count {
		writeCacheTestFile(t, filepath.Join(directory, fmt.Sprintf("segment-%06d.ts", index)), "payload")
	}
	writeCacheTestFile(t, filepath.Join(directory, "main.m3u8"), "playlist")
	return cache
}

func TestCacheMaintenanceAccountsRenameBatchesWithoutEarlyReadiness(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(directory, "segment-0.ts.tmp"), "payload")
	writeCacheTestFile(t, filepath.Join(directory, "main.m3u8"), "playlist")
	initial := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	if initial.bytes != 15 || initial.ready {
		t.Fatalf("temporary output accounting = %+v", initial)
	}
	if err := os.Rename(filepath.Join(directory, "segment-0.ts.tmp"), filepath.Join(directory, "segment-0.ts")); err != nil {
		t.Fatal(err)
	}
	partial, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 6)
	if err != nil || !partial.pending || partial.work > 6 {
		t.Fatalf("split rename batch = %+v, %v", partial, err)
	}
	final := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	if final.bytes != initial.bytes || !final.ready {
		t.Fatalf("published output accounting = %+v", final)
	}
	if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{}); err != nil || bytes != final.bytes || ready != final.ready {
		t.Fatalf("incremental/final inspection disagree: %d, %t, %v", bytes, ready, err)
	}
}

func TestCacheMaintenanceAccountsProgressiveAppendAndPrivateAssets(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	plan := Plan{OutputMode: "progressive"}
	directory := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(directory, "subtitle.ass"), "private")
	initial := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, plan, 32)
	if initial.bytes != 7 || initial.ready {
		t.Fatalf("private asset established readiness: %+v", initial)
	}
	writeCacheTestFile(t, filepath.Join(directory, "stream.bin"), "payload")
	known := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, plan, 32)
	if known.bytes != 14 || !known.ready {
		t.Fatalf("progressive payload = %+v", known)
	}
	file, err := os.OpenFile(filepath.Join(directory, "stream.bin"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("more")
	err = errors.Join(err, file.Close())
	if err != nil {
		t.Fatal(err)
	}
	appended := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, plan, 32)
	if appended.bytes != 18 || !appended.ready {
		t.Fatalf("progressive append = %+v", appended)
	}
}

func TestCacheMaintenanceFrozenWritesInvalidateEvenWithRestoredMtime(t *testing.T) {
	cache := cacheMaintenanceOutput(t, 1)
	if _, ready, err := cache.FinalizePlanJob(cacheTestJobA, Plan{}); err != nil || !ready {
		t.Fatalf("final inspection = %t, %v", ready, err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA, "segment-000000.ts")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	writeCacheTestFile(t, path, "changed")
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("frozen write was hidden by restored mtime: %v", err)
	}
	if file, err := cache.OpenJobFile(cacheTestJobA, "segment-000000.ts"); file != nil || !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("failed observer exposed frozen output: %v", err)
	}
}

func TestCacheMaintenanceFrozenBlindPathHardlinkFailsAuthoritativeAudit(t *testing.T) {
	cache := cacheMaintenanceOutput(t, 1)
	if _, _, err := cache.FinalizePlanJob(cacheTestJobA, Plan{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA, "segment-000000.ts")
	link := filepath.Join(t.TempDir(), "external-link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
	// Remove optional attribute hints to exercise the stat audit itself. A
	// directory observer does not cover operations through external hard links.
	for s.watch >= 0 {
		observed, err := s.readEvent()
		if err != nil {
			t.Fatal(err)
		}
		if !observed {
			break
		}
	}
	for name := range s.dirty {
		s.forgetDirty(name)
	}
	if _, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("blind-path hard link escaped descriptor inspection: %v", err)
	}
}

func TestCacheMaintenanceRejectsLostObservationGenerations(t *testing.T) {
	for _, mask := range []uint32{unix.IN_Q_OVERFLOW, unix.IN_IGNORED, unix.IN_UNMOUNT, unix.IN_DELETE_SELF, unix.IN_MOVE_SELF} {
		t.Run(fmt.Sprintf("mask-%x", mask), func(t *testing.T) {
			cache := cacheMaintenanceOutput(t, 1)
			cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
			s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
			var descriptors [2]int
			if err := unix.Pipe2(descriptors[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			if err := unix.Close(s.watch); err != nil {
				t.Fatal(err)
			}
			s.watch, s.eventStart, s.eventEnd = descriptors[0], 0, 0
			t.Cleanup(func() { _ = unix.Close(descriptors[1]) })
			var event [unix.SizeofInotifyEvent]byte
			binary.NativeEndian.PutUint32(event[:4], uint32(s.watchID))
			binary.NativeEndian.PutUint32(event[4:8], mask)
			if _, err := unix.Write(descriptors[1], event[:]); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32); !errors.Is(err, ErrCacheUnsafe) {
					t.Fatalf("lost observation became usable after drain: %v", err)
				}
			}
			if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{}); err != nil || bytes != 15 || !ready {
				t.Fatalf("authoritative inspection was skipped after observer loss: %d, %t, %v", bytes, ready, err)
			}
			if _, _, err := cache.FinalizePlanJob(cacheTestJobA, Plan{}); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("a successful final scan cleared lost-generation status: %v", err)
			}
		})
	}
}

func TestCacheMaintenanceBoundsColdCensusAndStableFileChecks(t *testing.T) {
	cache := cacheMaintenanceOutput(t, 600)
	initial, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32)
	if err != nil || !initial.pending || initial.work > 32 {
		t.Fatalf("large cold census did not respect its budget: %+v, %v", initial, err)
	}
	known := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	if known.bytes != 600*7+8 || !known.ready {
		t.Fatalf("bounded census lost files: %+v", known)
	}
	if _, _, err := cache.FinalizePlanJob(cacheTestJobA, Plan{}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		stable, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32)
		if err != nil || stable.pending || stable.bytes != known.bytes || stable.work > 32 {
			t.Fatalf("stable bounded audit = %+v, %v", stable, err)
		}
	}
	s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
	s.auditAt = time.Now().Add(-cacheMaintenanceAuditLimit - time.Second)
	if _, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 32); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("overdue authoritative file audit remained reusable: %v", err)
	}
}

func TestCacheMaintenanceRejectsDirectoryReplacementAndPlanChange(t *testing.T) {
	for _, change := range []string{"directory", "plan"} {
		t.Run(change, func(t *testing.T) {
			cache := cacheMaintenanceOutput(t, 1)
			cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
			plan := Plan{}
			if change == "directory" {
				path := filepath.Join(cache.RootPath(), cacheTestJobA)
				if err := os.Rename(path, filepath.Join(t.TempDir(), "parked")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				plan.OutputMode = "progressive"
			}
			if _, _, err := cache.FinalizePlanJob(cacheTestJobA, plan); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("final inspection accepted %s substitution: %v", change, err)
			}
		})
	}
}

func TestCacheMaintenanceRetiresObserverAndAllocationCharges(t *testing.T) {
	cache := cacheMaintenanceOutput(t, 4)
	cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
	if err := cache.RemoveJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	cache.jobsMu.Lock()
	remaining, facts, dirty := len(cache.maintenance), cache.maintenanceFacts, cache.maintenanceDirty
	cache.jobsMu.Unlock()
	if remaining != 0 || facts != 0 || dirty != 0 || s.watch != -1 || s.directory != nil || s.membershipActive {
		t.Fatalf("retired observer leaked resources: %d inventories, %d facts, %d dirty", remaining, facts, dirty)
	}
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatalf("retired job ID could not be reused: %v", err)
	}
	if next := cacheMaintenanceInventory(t, cache, cacheTestJobA); next == s || next.frozen {
		t.Fatal("a new job reused the old observation generation")
	}
}

func TestCacheMaintenanceRenameAliasesHaveOneInodeCharge(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(directory, "segment-0.ts.tmp"), string(make([]byte, 200)))
	writeCacheTestFile(t, filepath.Join(directory, "main.m3u8"), "playlist")
	initial := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	if err := os.Rename(filepath.Join(directory, "segment-0.ts.tmp"), filepath.Join(directory, "segment-0.ts")); err != nil {
		t.Fatal(err)
	}
	cache.mu.RLock()
	job := cache.lockJob(cacheTestJobA)
	s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
	// Deliberately resolve TO before FROM, as a bounded dirty-name batch can.
	resolved, err := cache.inspectInventoryName(s, "segment-0.ts")
	charged, aliases := s.bytes, len(s.files)
	cache.unlockJob(cacheTestJobA, job)
	cache.mu.RUnlock()
	if err != nil || !resolved || aliases != 3 || charged != initial.bytes || charged > 210 {
		t.Fatalf("rename manufactured a quota violation: %d bytes, %d aliases, %v", charged, aliases, err)
	}
	closed := cacheMaintenanceUntilKnown(t, cache, cacheTestJobA, Plan{}, 32)
	if closed.bytes != initial.bytes || !closed.ready {
		t.Fatalf("closed rename charge = %+v", closed)
	}
}

func TestCacheMaintenanceLifecycleReleasesEveryObserver(t *testing.T) {
	for _, operation := range []string{"close", "recover"} {
		t.Run(operation, func(t *testing.T) {
			cache := newTestCache(t)
			var observers []*cacheInventory
			for index := range 8 {
				id := fmt.Sprintf("%032x", index+1)
				if err := cache.CreateJob(id); err != nil {
					t.Fatal(err)
				}
				writeCacheTestFile(t, filepath.Join(cache.RootPath(), id, "segment-0.ts"), "payload")
				cacheMaintenanceUntilKnown(t, cache, id, Plan{}, 32)
				observers = append(observers, cacheMaintenanceInventory(t, cache, id))
			}
			var err error
			if operation == "close" {
				err = cache.Close()
			} else {
				err = cache.Recover()
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, observer := range observers {
				if observer.watch != -1 || observer.directory != nil || observer.membershipActive {
					t.Fatalf("%s retained an observer descriptor", operation)
				}
			}
			if len(cache.maintenance) != 0 || cache.maintenanceFacts != 0 || cache.maintenanceDirty != 0 {
				t.Fatalf("%s retained observer allocation charges", operation)
			}
		})
	}
}

func TestCacheMaintenanceRetainedCapacity(t *testing.T) {
	const fixture = "GOBY_CACHE_MAINTENANCE_LOW_FD_FIXTURE"
	if os.Getenv(fixture) != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestCacheMaintenanceRetainedCapacity$", "-test.count=1")
		command.Env = append(os.Environ(), fixture+"=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated low-descriptor capacity fixture: %v\n%s", err, output)
		}
		return
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	limit.Cur = min(limit.Cur, 64)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	options := managerTestOptions(t, nil)
	options.MaxRetainedJobs = 256
	m := newJobLockingManagerWithOptions(t, options)
	for index := range 192 {
		job := addJobLockingOutput(t, m, index+1, false)
		if _, _, err := m.cache.FinalizePlanJob(job.record.ID, job.record.Spec.Plan); err != nil {
			t.Fatalf("retained job %d: %v", index, err)
		}
		observer := cacheMaintenanceInventory(t, m.cache, job.record.ID)
		if observer.watch != -1 || observer.directory != nil {
			t.Fatalf("completed job %d retained a notification instance or directory descriptor", index)
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		m.maintainJobs(true)
	}
	for _, job := range m.jobs {
		handle, err := m.TryOpen(job.record.Spec.Scope, job.record.ID, "segment-0.ts")
		if err != nil {
			t.Fatalf("retained job lookup under a %d-FD limit: %v", limit.Cur, err)
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCacheMaintenanceUnavailableObserverUsesAuthoritativeFallback(t *testing.T) {
	cache := cacheMaintenanceOutput(t, 2)
	s := cacheMaintenanceInventory(t, cache, cacheTestJobA)
	if s.watch >= 0 {
		if err := unix.Close(s.watch); err != nil {
			t.Fatal(err)
		}
	}
	s.watch, s.fallback = -1, true
	s.auditAt, s.membershipAt = time.Now().Add(-2*cacheMaintenanceAuditLimit), time.Now().Add(-2*cacheMaintenanceAuditLimit)
	result, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 6)
	if err != nil || result.pending || result.bytes != 22 || !result.ready {
		t.Fatalf("notification fallback skipped full accounting: %+v, %v", result, err)
	}
	unknown := filepath.Join(cache.RootPath(), cacheTestJobA, "unknown.txt")
	writeCacheTestFile(t, unknown, "private unexpected bytes")
	if _, err := cache.MaintainPlanJob(cacheTestJobA, Plan{}, 6); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("notification fallback ignored unknown output: %v", err)
	}
}

func TestCacheMaintenanceCreationRollbackPreservesUnknownContent(t *testing.T) {
	for _, content := range []string{"empty", "unknown"} {
		t.Run(content, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			observer := cacheMaintenanceInventory(t, cache, cacheTestJobA)
			path := filepath.Join(cache.RootPath(), cacheTestJobA, "unknown.txt")
			if content == "unknown" {
				writeCacheTestFile(t, path, "preserve unexpected bytes")
			}
			cause := errors.New("injected cold initialization failure")
			cache.mu.RLock()
			job := cache.lockJob(cacheTestJobA)
			err := cache.rollbackJobCreation(cacheTestJobA, cause)
			cache.unlockJob(cacheTestJobA, job)
			cache.mu.RUnlock()
			if !errors.Is(err, cause) {
				t.Fatalf("rollback lost its initialization cause: %v", err)
			}
			if content == "unknown" {
				if !errors.Is(err, errCacheCreationUnaccounted) || !errors.Is(err, ErrCacheUnsafe) {
					t.Fatalf("failed guarded rollback lost its accounting marker: %v", err)
				}
				assertCacheTestFile(t, path, "preserve unexpected bytes")
			} else {
				if errors.Is(err, errCacheCreationUnaccounted) || observer.watch != -1 || observer.directory != nil {
					t.Fatalf("successful rollback retained ownership or observer FDs: %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(cache.RootPath(), cacheTestJobA)); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("successful rollback retained its directory: %v", statErr)
				}
			}
			if err := cache.Close(); err != nil {
				t.Fatal(err)
			}
			if observer.watch != -1 || observer.directory != nil {
				t.Fatal("close retained a failed creation observer")
			}
		})
	}
}
