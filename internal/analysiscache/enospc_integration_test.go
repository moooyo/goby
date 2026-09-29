//go:build linux

package analysiscache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The launcher must supply an empty private tmpfs and retain ownership of its
// mount namespace. This test fills only that bounded filesystem, never the host.
func TestCacheRealENOSPCDuringOwnerInitializationRecovers(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_CACHE_ENOSPC") != "1" {
		t.Skip("set GOBY_PHASE3_CACHE_ENOSPC=1 in an isolated bounded tmpfs namespace")
	}
	mount := cacheENOSPCMount(t)
	base, err := os.MkdirTemp(mount, "cache-owner-enospc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("cache_owner_enospc_failed_fixture_preserved=%s", base)
			return
		}
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove only the owned cache fault fixture: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	configuration := Config{Root: filepath.Join(base, "cache"), MaxBytes: 64 << 10,
		MaxEntries: 4, MaxEntryBytes: 16 << 10, MaxFileBytes: 8 << 10, MaxTemporaryFiles: 8}
	store, err := Open(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := store.Close(cleanup); err != nil {
				t.Errorf("close the owned cache fault fixture: %v", err)
			}
		}
	})
	oldBytes := []byte("previously published preview remains intact")
	old := cacheTestPublish(t, store, 1, oldBytes)
	before := store.Stats()
	release := cacheENOSPCFill(t, mount, filepath.Join(base, "filler"))
	builder, beginErr := store.Begin(ctx, cacheTestKey(2), 0)
	if builder != nil {
		_ = builder.Abort(context.Background())
		t.Fatal("a full tmpfs admitted an initialized cache builder")
	}
	var pathError *os.PathError
	if !errors.Is(beginErr, syscall.ENOSPC) || !errors.As(beginErr, &pathError) ||
		pathError.Op != "write" || pathError.Path != ownerName || !errors.Is(pathError.Err, syscall.ENOSPC) {
		t.Fatalf("the failure did not come from an actual owner-marker write: %v", beginErr)
	}
	after := store.Stats()
	t.Logf("cache_owner_enospc owner_write_errno=ENOSPC unsafe=%t building=%d reserved_bytes=%d ready_entries=%d",
		errors.Is(beginErr, ErrUnsafe), after.BuildingEntries, after.ReservedBytes, after.ReadyEntries)
	if errors.Is(beginErr, ErrUnsafe) {
		t.Error("owned initialization ENOSPC was reclassified as unsafe cache identity")
	}
	if after != before {
		t.Errorf("failed initialization retained a builder/reservation or changed published accounting: before=%+v after=%+v", before, after)
	}
	cacheENOSPCAssertEntry(t, ctx, store, old, oldBytes)
	names, err := os.ReadDir(configuration.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name.Name() != diskMarkerName && name.Name() != diskLockName && name.Name() != readyDirectory(old.Key) {
			t.Errorf("failed initialization left an owned temporary directory: %s", name.Name())
		}
	}
	release()
	newBytes := []byte("new preview generated after filesystem capacity returned")
	replacement := cacheTestPublish(t, store, 2, newBytes)
	cacheENOSPCAssertEntry(t, ctx, store, old, oldBytes)
	cacheENOSPCAssertEntry(t, ctx, store, replacement, newBytes)
	if err := store.Close(ctx); err != nil {
		store = nil
		t.Fatalf("close recovered cache: %v", err)
	}
	store, err = Open(ctx, configuration)
	if err != nil {
		t.Fatalf("reopen recovered cache: %v", err)
	}
	cacheENOSPCAssertEntry(t, ctx, store, old, oldBytes)
	cacheENOSPCAssertEntry(t, ctx, store, replacement, newBytes)
	stats := store.Stats()
	if stats.ReadyEntries != 2 || stats.BuildingEntries != 0 || stats.ReservedBytes != 0 ||
		stats.PendingPublications != 0 || stats.Readers != 0 || stats.ReadyBytes != old.Bytes+replacement.Bytes {
		t.Fatalf("reopened cache lost sealed entries or retained failed work: %+v", stats)
	}
	t.Log("cache_owner_enospc recovered=true prior_entry_preserved=true replacement_published=true reopen_preserved=true")
}

func cacheENOSPCAssertEntry(t *testing.T, ctx context.Context, store *Store, entry Entry, expected []byte) {
	t.Helper()
	lease, err := store.Acquire(ctx, entry.Key, entry.Seal, "240.bif")
	if err != nil {
		t.Fatalf("read preserved sealed cache entry: %v", err)
	}
	data, readErr := io.ReadAll(lease.File)
	closeErr := lease.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(data, expected) ||
		lease.Seal != entry.Seal || len(entry.Artifacts) != 1 || lease.Artifact != entry.Artifacts[0] {
		t.Fatalf("sealed entry changed across owner initialization failure: read=%v close=%v", readErr, closeErr)
	}
}

func cacheENOSPCMount(t *testing.T) string {
	t.Helper()
	mount := os.Getenv("GOBY_PHASE3_ENOSPC_MOUNT")
	canonical, err := filepath.EvalSymlinks(mount)
	if err != nil || !filepath.IsAbs(mount) || canonical != mount || filepath.Clean(mount) != mount ||
		mount == "/" || strings.ContainsAny(mount, " \t\r\n\\") {
		t.Fatal("an exact canonical owned tmpfs mountpoint is required")
	}
	host := os.Getenv("GOBY_PHASE3_ENOSPC_HOST_MOUNT_NAMESPACE")
	pidOne, err := os.Readlink("/proc/1/ns/mnt")
	self, selfErr := os.Readlink("/proc/self/ns/mnt")
	if err != nil || selfErr != nil || host == "" || host != pidOne || self == host {
		t.Fatal("the cache ENOSPC test requires an independent mount namespace")
	}
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(info), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 6 {
			t.Fatal("malformed mount namespace inventory")
		}
		for _, optional := range fields[6:] {
			if strings.HasPrefix(optional, "shared:") || strings.HasPrefix(optional, "master:") || strings.HasPrefix(optional, "propagate_from:") {
				t.Fatal("the cache fault namespace has propagating mounts")
			}
		}
		filesystem := strings.Fields(parts[1])
		if fields[4] == mount && len(filesystem) != 0 {
			found = filesystem[0] == "tmpfs" && fields[3] == "/"
		}
	}
	names, readErr := os.ReadDir(mount)
	var fs unix.Statfs_t
	if err := unix.Statfs(mount, &fs); err != nil || !found || fs.Type != unix.TMPFS_MAGIC || fs.Bsize <= 0 ||
		fs.Blocks < uint64((4<<20)/fs.Bsize) || fs.Blocks > uint64((32<<20)/fs.Bsize) || readErr != nil || len(names) != 0 {
		t.Fatal("the independent cache fault tmpfs must be empty and between 4 and 32 MiB")
	}
	t.Logf("cache_owner_enospc_mount verified_private_tmpfs=true bytes=%d", fs.Blocks*uint64(fs.Bsize))
	return mount
}

func cacheENOSPCFill(t *testing.T, mount, path string) func() {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	remove := func() {
		t.Helper()
		if !removed {
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove only the owned cache fault filler: %v", err)
			}
			removed = true
		}
	}
	t.Cleanup(func() { _ = file.Close(); remove() })
	buffer := make([]byte, 256<<10)
	var written int64
	for {
		n, writeErr := file.Write(buffer)
		written += int64(n)
		if errors.Is(writeErr, syscall.ENOSPC) {
			break
		}
		if writeErr != nil || n == 0 || written > 32<<20 {
			t.Fatalf("bounded cache pressure did not produce real ENOSPC: written=%d error=%v", written, writeErr)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(mount, &fs); err != nil || fs.Bavail != 0 || written == 0 {
		t.Fatal("the cache filler did not exhaust actual tmpfs capacity")
	}
	t.Logf("cache_owner_enospc_pressure filler_errno=ENOSPC filler_bytes=%d remaining_bytes=0", written)
	return remove
}
