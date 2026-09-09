//go:build linux

package transcode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	cacheTestJobA = "0123456789abcdef0123456789abcdef"
	cacheTestJobB = "1123456789abcdef0123456789abcdef"
)

func newTestCache(t *testing.T) *cacheRoot {
	t.Helper()
	cache, err := openCacheRoot(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func writeCacheTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertCacheTestFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file %s: got %q, %v; want %q", filepath.Base(path), data, err, want)
	}
}

func TestCacheClaimsEmptyDirectoryAndHoldsLifetimeLock(t *testing.T) {
	cache := newTestCache(t)
	assertCacheTestFile(t, filepath.Join(cache.RootPath(), cacheMarkerName), cacheMarkerVersion)
	info, err := os.Stat(cache.RootPath())
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache permissions: %v, %v", info, err)
	}
	other, err := openCacheRoot(cache.RootPath())
	if other != nil || !errors.Is(err, ErrCacheLocked) {
		t.Fatalf("second owner: %v, %v", other, err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	other, err = openCacheRoot(cache.RootPath())
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	defer other.Close()
	if err := other.Recover(); err != nil {
		t.Fatal(err)
	}
}

func TestCacheRefusesUnownedNonemptyDirectory(t *testing.T) {
	path := t.TempDir()
	file := filepath.Join(path, "unrelated-data")
	writeCacheTestFile(t, file, "preserve")
	cache, err := openCacheRoot(path)
	if cache != nil || !errors.Is(err, ErrCacheInvalid) {
		t.Fatalf("unexpected ownership claim: %v, %v", cache, err)
	}
	assertCacheTestFile(t, file, "preserve")
	if _, err := os.Lstat(filepath.Join(path, cacheLockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unowned directory was modified: %v", err)
	}
}

func TestCacheRejectsUnsafeRootPaths(t *testing.T) {
	parent := t.TempDir()
	actual := filepath.Join(parent, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"cache", "/", parent + "/./cache", parent + "/cache/../cache", link, filepath.Join(link, "cache")} {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			cache, err := openCacheRoot(path)
			if cache != nil || err == nil {
				t.Fatalf("unsafe root accepted: %q, %v", path, err)
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(actual, "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache was created through a parent symlink: %v", err)
	}
}

func TestCacheRejectsWritableRoot(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	cache, err := openCacheRoot(path)
	if cache != nil || !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("writable cache accepted: %v, %v", cache, err)
	}
}

func TestCacheRejectsInvalidOwnershipFiles(t *testing.T) {
	for _, kind := range []string{"marker-version", "marker-size", "marker-symlink", "marker-hardlink", "lock-symlink", "lock-content"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			path := filepath.Join(parent, "cache")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(path, cacheMarkerName)
			lock := filepath.Join(path, cacheLockName)
			outside := filepath.Join(parent, "outside")
			writeCacheTestFile(t, outside, cacheMarkerVersion)
			writeCacheTestFile(t, marker, cacheMarkerVersion)
			switch kind {
			case "marker-version":
				writeCacheTestFile(t, marker, strings.Repeat("x", len(cacheMarkerVersion)))
			case "marker-size":
				writeCacheTestFile(t, marker, "")
			case "marker-symlink", "marker-hardlink":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				link := os.Symlink
				if kind == "marker-hardlink" {
					link = os.Link
				}
				if err := link(outside, marker); err != nil {
					t.Fatal(err)
				}
			case "lock-symlink":
				if err := os.Symlink(outside, lock); err != nil {
					t.Fatal(err)
				}
			case "lock-content":
				writeCacheTestFile(t, lock, "unrelated lock")
			}
			cache, err := openCacheRoot(path)
			if cache != nil || err == nil {
				t.Fatalf("unsafe ownership file accepted: %v, %v", cache, err)
			}
			assertCacheTestFile(t, outside, cacheMarkerVersion)
		})
	}
}

func TestCacheOutputReadinessAndTemporaryFileAccounting(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	if err := cache.CreateJob(cacheTestJobA); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate job replaced: %v", err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "#EXTM3U\n")
	writeCacheTestFile(t, filepath.Join(path, "segment-000000.ts.tmp"), "123")
	bytes, ready, err := cache.ScanJob(cacheTestJobA)
	if err != nil || ready || bytes != 11 {
		t.Fatalf("temporary output: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	if _, err := cache.OpenJobFile(cacheTestJobA, "segment-000000.ts.tmp"); !errors.Is(err, ErrCacheInvalid) {
		t.Fatalf("temporary output was exposed: %v", err)
	}
	if err := os.Rename(filepath.Join(path, "segment-000000.ts.tmp"), filepath.Join(path, "segment-000000.ts")); err != nil {
		t.Fatal(err)
	}
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8.tmp"), "next")
	bytes, ready, err = cache.ScanJob(cacheTestJobA)
	if err != nil || !ready || bytes != 15 {
		t.Fatalf("published output: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	file, err := cache.OpenJobFile(cacheTestJobA, "segment-000000.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "23" {
		t.Fatalf("seekable output: %q, %v", body, err)
	}
	free, err := cache.FreeBytes()
	if err != nil || free < 0 {
		t.Fatalf("free space: %d, %v", free, err)
	}
}

func TestCacheRequiresNonemptyPublishedOutputs(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "")
	writeCacheTestFile(t, filepath.Join(path, "segment-0.ts"), "data")
	if _, ready, err := cache.ScanJob(cacheTestJobA); err != nil || ready {
		t.Fatalf("empty playlist accepted: %v, %v", ready, err)
	}
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "#EXTM3U\n")
	writeCacheTestFile(t, filepath.Join(path, "segment-0.ts"), "")
	if _, ready, err := cache.ScanJob(cacheTestJobA); err != nil || ready {
		t.Fatalf("empty segment accepted: %v, %v", ready, err)
	}
}

func TestCacheAccountsPrivateVODFilesWithoutServingThem(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	privateNames := []string{"segment-list.m3u8", "segment-list.m3u8.tmp", "main.m3u8.publish.tmp"}
	const privateBody = "private playlist bytes"
	for _, name := range privateNames {
		writeCacheTestFile(t, filepath.Join(path, name), privateBody)
		if validOutputName(name) {
			t.Fatalf("private output name is public: %s", name)
		}
		file, err := cache.OpenJobFile(cacheTestJobA, name)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, ErrCacheInvalid) {
			t.Fatalf("private file was exposed: %s, %v", name, err)
		}
	}
	writeCacheTestFile(t, filepath.Join(path, "segment-000010.ts"), "segment bytes")
	bytes, ready, err := cache.ScanJob(cacheTestJobA)
	wantBytes := int64(len(privateNames)*len(privateBody) + len("segment bytes"))
	if err != nil || ready || bytes != wantBytes {
		t.Fatalf("private playlist accounting: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "#EXTM3U\n")
	bytes, ready, err = cache.ScanJob(cacheTestJobA)
	if err != nil || !ready || bytes != wantBytes+8 {
		t.Fatalf("published VOD accounting: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	if err := cache.Recover(); err != nil {
		t.Fatalf("private VOD files prevented recovery: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered VOD job remains: %v", err)
	}
}

func TestCacheProgressiveReadinessAndAccounting(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	plan := Plan{OutputMode: "progressive"}
	bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, plan)
	if err != nil || ready || bytes != 0 {
		t.Fatalf("empty progressive directory: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA, "stream.bin")
	writeCacheTestFile(t, path, "")
	bytes, ready, err = cache.ScanPlanJob(cacheTestJobA, plan)
	if err != nil || ready || bytes != 0 {
		t.Fatalf("empty progressive output: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	// Cache readiness measures byte availability; only the runner can confirm
	// that a muxer header is followed by a valid payload for the chosen format.
	const body = "unparsed progressive bytes"
	writeCacheTestFile(t, path, body)
	bytes, ready, err = cache.ScanPlanJob(cacheTestJobA, plan)
	if err != nil || !ready || bytes != int64(len(body)) {
		t.Fatalf("progressive byte accounting: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	file, err := cache.OpenJobFile(cacheTestJobA, "stream.bin")
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, ErrCacheInvalid) {
		t.Fatalf("private progressive file exposed through public output API: %v", err)
	}
	if _, _, err := cache.ScanJob(cacheTestJobA); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("default HLS scan accepted progressive output: %v", err)
	}
	if _, _, err := cache.ScanPlanJob(cacheTestJobA, Plan{}); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("explicit HLS scan accepted progressive output: %v", err)
	}
	if err := cache.RemoveJob(cacheTestJobA); err != nil {
		t.Fatalf("progressive cleanup failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("progressive output remains after cleanup: %v", err)
	}
}

func TestCacheProgressiveFilePreservesGrowingInode(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA, "stream.bin")
	writeCacheTestFile(t, path, "first")
	reader, err := cache.OpenProgressiveFile(cacheTestJobA)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "first" {
		t.Fatalf("initial progressive data: %q, %v", body, err)
	}
	// Keep both descriptors open while replacing the configured pathname.
	// Appends and reads must continue against the originally opened root.
	if err := os.Rename(cache.RootPath(), cache.RootPath()+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCacheTestFile(t, path, "replacement")
	if _, err := io.WriteString(writer, " second"); err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(reader)
	if err != nil || string(body) != " second" {
		t.Fatalf("new bytes through the original descriptor: %q, %v", body, err)
	}
	info, err := reader.Stat()
	if err != nil || info.Size() != int64(len("first second")) {
		t.Fatalf("growing descriptor size: %v, %v", info, err)
	}
	bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{OutputMode: "progressive"})
	if err != nil || !ready || bytes != int64(len("first second")) {
		t.Fatalf("anchored progressive scan: bytes=%d, ready=%v, err=%v", bytes, ready, err)
	}
	secondReader, err := cache.OpenProgressiveFile(cacheTestJobA)
	if err != nil {
		t.Fatal(err)
	}
	defer secondReader.Close()
	body, err = io.ReadAll(secondReader)
	if err != nil || string(body) != "first second" {
		t.Fatalf("reopened progressive descriptor escaped: %q, %v", body, err)
	}
	assertCacheTestFile(t, path, "replacement")
}

func TestCacheScanRejectsOutputModeMixing(t *testing.T) {
	for _, name := range []string{"main.m3u8", "main.m3u8.tmp", "segment-0.ts", "segment-0.ts.tmp", "segment-list.m3u8", "segment-list.m3u8.tmp", "main.m3u8.publish.tmp"} {
		t.Run(name, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cache.RootPath(), cacheTestJobA)
			writeCacheTestFile(t, filepath.Join(path, "stream.bin"), "progressive")
			// Even an empty file from another mode invalidates the directory.
			writeCacheTestFile(t, filepath.Join(path, name), "")
			if _, _, err := cache.ScanPlanJob(cacheTestJobA, Plan{OutputMode: "progressive"}); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("progressive scan accepted HLS content: %v", err)
			}
		})
	}
	t.Run("ready-hls-with-empty-progressive-file", func(t *testing.T) {
		cache := newTestCache(t)
		if err := cache.CreateJob(cacheTestJobA); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cache.RootPath(), cacheTestJobA)
		writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "#EXTM3U\n")
		writeCacheTestFile(t, filepath.Join(path, "segment-0.ts"), "segment")
		if _, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{}); err != nil || !ready {
			t.Fatalf("normal HLS output rejected: %v, %v", ready, err)
		}
		writeCacheTestFile(t, filepath.Join(path, "stream.bin"), "")
		if _, _, err := cache.ScanJob(cacheTestJobA); !errors.Is(err, ErrCacheUnsafe) {
			t.Fatalf("ready HLS job accepted a progressive file: %v", err)
		}
	})
}

func TestCacheProgressiveFileRejectsNonregularContent(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "job-symlink"} {
		t.Run(kind, func(t *testing.T) {
			cache := newTestCache(t)
			outside := t.TempDir()
			foreign := filepath.Join(outside, "stream.bin")
			writeCacheTestFile(t, foreign, "preserve")
			jobPath := filepath.Join(cache.RootPath(), cacheTestJobA)
			if kind == "job-symlink" {
				if err := os.Symlink(outside, jobPath); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := cache.CreateJob(cacheTestJobA); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(jobPath, "stream.bin")
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(foreign, path)
				case "hardlink":
					err = os.Link(foreign, path)
				case "directory":
					err = os.Mkdir(path, 0o700)
				case "fifo":
					err = syscall.Mkfifo(path, 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			file, err := cache.OpenProgressiveFile(cacheTestJobA)
			if file != nil {
				_ = file.Close()
			}
			if !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("unsafe progressive file opened: %v", err)
			}
			if _, _, err := cache.ScanPlanJob(cacheTestJobA, Plan{OutputMode: "progressive"}); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("unsafe progressive content scanned: %v", err)
			}
			if err := cache.Recover(); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("unsafe progressive content removed: %v", err)
			}
			assertCacheTestFile(t, foreign, "preserve")
		})
	}
}

func TestCacheRecoveryAccountsMixedKnownOutputsAndPreservesUnknownContent(t *testing.T) {
	cache := newTestCache(t)
	for _, id := range []string{cacheTestJobA, cacheTestJobB} {
		if err := cache.CreateJob(id); err != nil {
			t.Fatal(err)
		}
	}
	pathA := filepath.Join(cache.RootPath(), cacheTestJobA)
	pathB := filepath.Join(cache.RootPath(), cacheTestJobB)
	writeCacheTestFile(t, filepath.Join(pathA, "stream.bin"), "first")
	names := []string{"stream.bin", "main.m3u8", "main.m3u8.tmp", "segment-0.ts", "segment-1.ts.tmp", "segment-list.m3u8", "segment-list.m3u8.tmp", "main.m3u8.publish.tmp"}
	for _, name := range names {
		writeCacheTestFile(t, filepath.Join(pathB, name), "owned")
	}
	dir, err := cacheOpenDirectoryAt(cache.dir, cacheTestJobB)
	if err != nil {
		t.Fatal(err)
	}
	files, bytes, _, err := cacheInspectJob(dir, false)
	_ = dir.Close()
	if err != nil || len(files) != len(names) || bytes != int64(len(names)*len("owned")) {
		t.Fatalf("mixed recovery accounting: files=%d, bytes=%d, err=%v", len(files), bytes, err)
	}
	unknown := filepath.Join(pathB, "stream.bin.tmp")
	writeCacheTestFile(t, unknown, "foreign")
	if err := cache.Recover(); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("unexpected progressive suffix accepted during recovery: %v", err)
	}
	assertCacheTestFile(t, filepath.Join(pathA, "stream.bin"), "first")
	for _, name := range names {
		assertCacheTestFile(t, filepath.Join(pathB, name), "owned")
	}
	assertCacheTestFile(t, unknown, "foreign")
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	if err := cache.Recover(); err != nil {
		t.Fatalf("mixed owned outputs could not be recovered: %v", err)
	}
	for _, path := range []string{pathA, pathB} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned output remains after recovery: %v", err)
		}
	}
}

func TestCachePlanScanValidatesModeAndJobID(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"hls", "Progressive", "PROGRESSIVE", "progressive ", "unknown"} {
		if _, _, err := cache.ScanPlanJob(cacheTestJobA, Plan{OutputMode: mode}); !errors.Is(err, ErrCacheInvalid) {
			t.Errorf("invalid mode %q accepted: %v", mode, err)
		}
	}
	for _, id := range []string{"", "../outside", strings.Repeat("A", 32)} {
		if _, _, err := cache.ScanPlanJob(id, Plan{OutputMode: "progressive"}); !errors.Is(err, ErrCacheInvalid) {
			t.Errorf("invalid scan job %q accepted: %v", id, err)
		}
		file, err := cache.OpenProgressiveFile(id)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, ErrCacheInvalid) {
			t.Errorf("invalid progressive job %q opened: %v", id, err)
		}
	}
}

func TestCacheRejectsUnrecognizedAndLinkedJobContent(t *testing.T) {
	for _, kind := range []string{"unknown", "nested", "symlink", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cache.RootPath(), cacheTestJobA)
			preserved := filepath.Join(path, "main.m3u8")
			writeCacheTestFile(t, preserved, "keep")
			outside := filepath.Join(t.TempDir(), "outside")
			writeCacheTestFile(t, outside, "outside")
			target := filepath.Join(path, "segment-0.ts")
			var err error
			switch kind {
			case "unknown":
				writeCacheTestFile(t, filepath.Join(path, "other.bin"), "foreign")
			case "nested":
				err = os.Mkdir(target, 0o700)
			case "symlink":
				err = os.Symlink(outside, target)
			case "hardlink":
				err = os.Link(outside, target)
			case "fifo":
				err = syscall.Mkfifo(target, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := cache.ScanJob(cacheTestJobA); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("unsafe scan accepted: %v", err)
			}
			if err := cache.RemoveJob(cacheTestJobA); !errors.Is(err, ErrCacheUnsafe) {
				t.Fatalf("unsafe cleanup accepted: %v", err)
			}
			assertCacheTestFile(t, preserved, "keep")
			assertCacheTestFile(t, outside, "outside")
		})
	}
}

func TestCacheRecoveryValidatesAllJobsBeforeDeletion(t *testing.T) {
	cache := newTestCache(t)
	for _, id := range []string{cacheTestJobA, cacheTestJobB} {
		if err := cache.CreateJob(id); err != nil {
			t.Fatal(err)
		}
		writeCacheTestFile(t, filepath.Join(cache.RootPath(), id, "main.m3u8"), "keep")
	}
	foreign := filepath.Join(cache.RootPath(), cacheTestJobB, "unrecognized.txt")
	writeCacheTestFile(t, foreign, "foreign")
	if err := cache.Recover(); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("unexpected recovery result: %v", err)
	}
	for _, id := range []string{cacheTestJobA, cacheTestJobB} {
		assertCacheTestFile(t, filepath.Join(cache.RootPath(), id, "main.m3u8"), "keep")
	}
	assertCacheTestFile(t, foreign, "foreign")
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := cache.Recover(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{cacheTestJobA, cacheTestJobB} {
		if _, err := os.Lstat(filepath.Join(cache.RootPath(), id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned job was not removed: %v", err)
		}
	}
	assertCacheTestFile(t, filepath.Join(cache.RootPath(), cacheMarkerName), cacheMarkerVersion)
}

func TestCacheRecoveryPreservesUnknownRootContent(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	playlist := filepath.Join(cache.RootPath(), cacheTestJobA, "main.m3u8")
	writeCacheTestFile(t, playlist, "keep")
	foreign := filepath.Join(cache.RootPath(), "other-data")
	writeCacheTestFile(t, foreign, "preserve")
	if err := cache.Recover(); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("unexpected recovery: %v", err)
	}
	assertCacheTestFile(t, playlist, "keep")
	assertCacheTestFile(t, foreign, "preserve")
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := openCacheRoot(cache.RootPath())
	if other != nil || !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("mixed cache reopened: %v, %v", other, err)
	}
}

func TestCacheRecoveryRejectsMissingMarkerWithoutRecreatingIt(t *testing.T) {
	cache := newTestCache(t)
	marker := filepath.Join(cache.RootPath(), cacheMarkerName)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := cache.Recover(); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("missing marker accepted: %v", err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership marker was recreated: %v", err)
	}
}

func TestCacheRejectsSymlinkJobAndStaysWithinOpenedRoot(t *testing.T) {
	cache := newTestCache(t)
	outside := t.TempDir()
	writeCacheTestFile(t, filepath.Join(outside, "main.m3u8"), "outside")
	if err := os.Symlink(outside, filepath.Join(cache.RootPath(), cacheTestJobA)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.OpenJobFile(cacheTestJobA, "main.m3u8"); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("symlink job opened: %v", err)
	}
	if err := cache.RemoveJob(cacheTestJobA); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("symlink job removed: %v", err)
	}
	assertCacheTestFile(t, filepath.Join(outside, "main.m3u8"), "outside")
	if err := os.Remove(filepath.Join(cache.RootPath(), cacheTestJobA)); err != nil {
		t.Fatal(err)
	}
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	writeCacheTestFile(t, filepath.Join(cache.RootPath(), cacheTestJobA, "main.m3u8"), "owned")
	moved := cache.RootPath() + "-moved"
	if err := os.Rename(cache.RootPath(), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, cache.RootPath()); err != nil {
		t.Fatal(err)
	}
	file, err := cache.OpenJobFile(cacheTestJobA, "main.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(body) != "owned" {
		t.Fatalf("opened root escaped: %q, %v", body, err)
	}
	if err := cache.RemoveJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	assertCacheTestFile(t, filepath.Join(outside, "main.m3u8"), "outside")
}

func TestCacheScansConcurrentAtomicPublication(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "#EXTM3U\n")
	writeCacheTestFile(t, filepath.Join(path, "segment-0.ts"), "segment")
	done := make(chan error, 1)
	go func() {
		for i := range 100 {
			segment := filepath.Join(path, fmt.Sprintf("segment-%d.ts", i+1))
			if err := os.WriteFile(segment+".tmp", []byte("segment"), 0o600); err != nil {
				done <- err
				return
			}
			if err := os.Rename(segment+".tmp", segment); err != nil {
				done <- err
				return
			}
			playlist := filepath.Join(path, "main.m3u8")
			if err := os.WriteFile(playlist+".tmp", []byte("#EXTM3U\n"), 0o600); err != nil {
				done <- err
				return
			}
			if err := os.Rename(playlist+".tmp", playlist); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var scanErr error
	for range 100 {
		if _, ready, err := cache.ScanJob(cacheTestJobA); err != nil || !ready {
			scanErr = fmt.Errorf("ready=%v, err=%v", ready, err)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if err := cache.RemoveJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
}

func TestCacheJobPathRemainsAnchoredAfterRootReplacement(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	writeCacheTestFile(t, filepath.Join(cache.RootPath(), cacheTestJobA, "main.m3u8"), "owned")
	if err := os.Rename(cache.RootPath(), cache.RootPath()+"-old"); err != nil {
		t.Fatal(err)
	}
	replacementJob := filepath.Join(cache.RootPath(), cacheTestJobA)
	if err := os.MkdirAll(replacementJob, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCacheTestFile(t, filepath.Join(replacementJob, "replacement.txt"), "preserve")
	path, err := cache.JobPath(cacheTestJobA)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("/proc/%d/fd/%d/%s", os.Getpid(), cache.dir.Fd(), cacheTestJobA)
	if path != want {
		t.Fatalf("job path: got %q, want %q", path, want)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "main.m3u8" {
		t.Fatalf("stable job path listed replacement content: %v, %v", entries, err)
	}
	assertCacheTestFile(t, filepath.Join(path, "main.m3u8"), "owned")
	assertCacheTestFile(t, filepath.Join(replacementJob, "replacement.txt"), "preserve")
	if _, err := cache.JobPath("../outside"); !errors.Is(err, ErrCacheInvalid) {
		t.Fatalf("invalid job path accepted: %v", err)
	}
}

func TestCacheDirectoryListingIsBounded(t *testing.T) {
	cache := newTestCache(t)
	writeCacheTestFile(t, filepath.Join(cache.RootPath(), "extra"), "")
	if _, err := cacheDirectoryNames(cache.dir, 2); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("entry limit not enforced: %v", err)
	}
}

func TestCacheValidatesJobAndOutputNames(t *testing.T) {
	for _, id := range []string{"", "../outside", strings.Repeat("A", 32), strings.Repeat("0", 31), strings.Repeat("0", 33), strings.Repeat("0", 31) + "/"} {
		if validJobID(id) {
			t.Errorf("invalid job accepted: %q", id)
		}
	}
	if !validJobID(cacheTestJobA) {
		t.Fatal("valid job rejected")
	}
	for _, name := range []string{"main.m3u8", "segment-0.ts", "segment-000001.ts"} {
		if !validOutputName(name) || !validCacheFileName(name+".tmp") {
			t.Errorf("valid output rejected: %q", name)
		}
	}
	for _, name := range []string{"", "../main.m3u8", "x/main.m3u8", "/main.m3u8", "Main.m3u8", "main.m3u8.tmp", "segment-.ts", "segment--1.ts", "segment-1a.ts", "segment-١.ts", "segment-0.ts/extra", "segment-0.ts\x00", "stream.bin", "stream.bin.tmp", strings.Repeat("0", 256)} {
		if validOutputName(name) {
			t.Errorf("invalid output accepted: %q", name)
		}
	}
	if validCacheFileName("main.m3u8.tmp.tmp") {
		t.Fatal("multiple temporary suffixes accepted")
	}
	if !validCacheFileName("stream.bin") {
		t.Fatal("progressive output is not recoverable")
	}
	for _, name := range []string{"segment-list.m3u8.tmp.tmp", "main.m3u8.publish", "main.m3u8.publish.tmp.tmp", "segment-list-1.m3u8", "Segment-list.m3u8", "stream.bin.tmp", "stream.bin.publish.tmp", ".stream.bin", "Stream.bin", "stream.BIN", "stream.bin/extra", "stream.bin\x00"} {
		if validCacheFileName(name) {
			t.Errorf("unexpected private filename accepted: %q", name)
		}
	}
}

func TestCacheMethodsRejectClosedRoot(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	checks := []error{cache.CreateJob(cacheTestJobA), cache.RemoveJob(cacheTestJobA), cache.Recover()}
	_, openErr := cache.OpenJobFile(cacheTestJobA, "main.m3u8")
	_, _, scanErr := cache.ScanJob(cacheTestJobA)
	_, freeErr := cache.FreeBytes()
	_, pathErr := cache.JobPath(cacheTestJobA)
	_, progressiveErr := cache.OpenProgressiveFile(cacheTestJobA)
	_, _, planScanErr := cache.ScanPlanJob(cacheTestJobA, Plan{OutputMode: "progressive"})
	checks = append(checks, openErr, scanErr, freeErr, pathErr, progressiveErr, planScanErr)
	for _, err := range checks {
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("closed cache operation: %v", err)
		}
	}
}
