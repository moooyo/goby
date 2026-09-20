//go:build linux

package analysiscache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func testDiskRoot(t *testing.T) *diskRoot {
	t.Helper()
	d, err := openDiskRoot(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Errorf("close cache: %v", err)
		}
	})
	return d
}

func readDiskTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func writeDiskTestFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func TestDiskRootKeepsExclusiveOwnershipUntilClose(t *testing.T) {
	d := testDiskRoot(t)
	owner := d.owner
	if !validDiskOwner(owner) {
		t.Fatalf("invalid generated owner %q", owner)
	}
	markerPath := filepath.Join(d.path, diskMarkerName)
	marker := readDiskTestFile(t, markerPath)
	second, err := openDiskRoot(d.path)
	if second != nil {
		_ = second.close()
		t.Fatal("second open returned a cache while the first owner holds its lock")
	}
	if !errors.Is(err, ErrOwned) {
		t.Fatalf("second open = %v, want ownership conflict", err)
	}
	if err := d.check(); err != nil {
		t.Fatalf("second open disturbed the first owner: %v", err)
	}
	if err := d.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openDiskRoot(d.path)
	if err != nil {
		t.Fatalf("open after close: %v", err)
	}
	defer reopened.close()
	if reopened.owner != owner {
		t.Fatalf("owner changed across reopen: %q != %q", reopened.owner, owner)
	}
	if got := readDiskTestFile(t, markerPath); got != marker {
		t.Fatal("reopen modified the ownership marker")
	}
}

func TestDiskRootRejectsUnmarkedNonemptyDirectoryWithoutMutation(t *testing.T) {
	for _, name := range []string{"unrelated.txt", diskLockName} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir()
			unknownPath := filepath.Join(path, name)
			writeDiskTestFile(t, unknownPath, "operator-owned content", 0o600)
			before, err := os.Stat(unknownPath)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := openDiskRoot(path)
			if opened != nil {
				_ = opened.close()
				t.Fatal("unmarked nonempty directory was admitted")
			}
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("open = %v, want unsafe content", err)
			}
			if got := readDiskTestFile(t, unknownPath); got != "operator-owned content" {
				t.Fatal("opening the cache modified unknown content")
			}
			after, err := os.Stat(unknownPath)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("opening the cache replaced or chmodded unknown content")
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != name {
				t.Fatal("opening an unmarked directory created or removed entries")
			}
		})
	}
}

func TestDiskRootRejectsSharedDirectoryPermissions(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}
	opened, err := openDiskRoot(path)
	if opened != nil {
		_ = opened.close()
		t.Fatal("shared directory was admitted")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("open = %v, want unsafe permissions", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatal("cache admission changed operator directory permissions")
	}
}

func TestOpenRegularRejectsUnsafeEntriesBeforeTruncation(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "shared-mode", "fifo", "traversal"} {
		t.Run(kind, func(t *testing.T) {
			d := testDiskRoot(t)
			external := filepath.Join(filepath.Dir(d.path), "external")
			writeDiskTestFile(t, external, "preserve these bytes", 0o600)
			entry := filepath.Join(d.path, "payload")
			name := "payload"
			switch kind {
			case "symlink":
				if err := os.Symlink(external, entry); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(external, entry); err != nil {
					t.Fatal(err)
				}
			case "shared-mode":
				writeDiskTestFile(t, entry, "preserve these bytes", 0o600)
				if err := os.Chmod(entry, 0o640); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(entry, 0o600); err != nil {
					t.Fatal(err)
				}
			case "traversal":
				name = "../external"
			}
			file, err := openRegular(d.root, name, os.O_WRONLY|os.O_TRUNC, 0)
			if file != nil {
				_ = file.Close()
				t.Fatal("unsafe file was opened")
			}
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("open = %v, want unsafe entry", err)
			}
			if got := readDiskTestFile(t, external); got != "preserve these bytes" {
				t.Fatal("rejected open truncated external content")
			}
			if kind == "shared-mode" && readDiskTestFile(t, entry) != "preserve these bytes" {
				t.Fatal("rejected open truncated the unsafe entry")
			}
		})
	}
}

func TestOpenChildRejectsSymlinkAndSharedPermissions(t *testing.T) {
	d := testDiskRoot(t)
	childPath := filepath.Join(d.path, "private")
	if err := os.Mkdir(childPath, 0o700); err != nil {
		t.Fatal(err)
	}
	child, err := openChild(d.root, "private")
	if err != nil {
		t.Fatal(err)
	}
	_ = child.Close()
	if err := os.Symlink("private", filepath.Join(d.path, "alias")); err != nil {
		t.Fatal(err)
	}
	child, err = openChild(d.root, "alias")
	if child != nil {
		_ = child.Close()
		t.Fatal("symbolic directory alias was admitted")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("symlink open = %v, want unsafe entry", err)
	}
	if err := os.Chmod(childPath, 0o750); err != nil {
		t.Fatal(err)
	}
	child, err = openChild(d.root, "private")
	if child != nil {
		_ = child.Close()
		t.Fatal("shared directory was admitted")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("shared directory open = %v, want unsafe permissions", err)
	}
}

func TestDiskRootDetectsRootPathReplacement(t *testing.T) {
	for _, replacement := range []string{"directory", "symlink-to-original"} {
		t.Run(replacement, func(t *testing.T) {
			d := testDiskRoot(t)
			detached := d.path + "-detached"
			if err := os.Rename(d.path, detached); err != nil {
				t.Fatal(err)
			}
			if replacement == "directory" {
				if err := os.Mkdir(d.path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(detached, d.path); err != nil {
				t.Fatal(err)
			}
			if err := d.check(); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("check after root replacement = %v, want unsafe root", err)
			}
			if replacement == "directory" {
				entries, err := os.ReadDir(d.path)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatal("check initialized the replacement directory")
				}
			}
		})
	}
}

func TestDiskRootDetectsChangedOwnershipFiles(t *testing.T) {
	for _, change := range []string{"marker-inode", "marker-bytes", "marker-mode", "marker-hardlink", "marker-symlink", "lock-inode"} {
		t.Run(change, func(t *testing.T) {
			d := testDiskRoot(t)
			markerPath := filepath.Join(d.path, diskMarkerName)
			marker := readDiskTestFile(t, markerPath)
			switch change {
			case "marker-inode", "marker-symlink":
				oldPath := filepath.Join(d.path, "old-marker")
				if err := os.Rename(markerPath, oldPath); err != nil {
					t.Fatal(err)
				}
				if change == "marker-inode" {
					writeDiskTestFile(t, markerPath, marker, 0o600)
				} else if err := os.Symlink(oldPath, markerPath); err != nil {
					t.Fatal(err)
				}
			case "marker-bytes":
				writeDiskTestFile(t, markerPath, marker+"\n", 0o600)
			case "marker-mode":
				if err := os.Chmod(markerPath, 0o640); err != nil {
					t.Fatal(err)
				}
			case "marker-hardlink":
				if err := os.Link(markerPath, filepath.Join(d.path, "marker-link")); err != nil {
					t.Fatal(err)
				}
			case "lock-inode":
				lockPath := filepath.Join(d.path, diskLockName)
				if err := os.Rename(lockPath, filepath.Join(d.path, "old-lock")); err != nil {
					t.Fatal(err)
				}
				writeDiskTestFile(t, lockPath, "", 0o600)
			}
			if err := d.check(); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("check after %s = %v, want unsafe ownership", change, err)
			}
		})
	}
}

func TestRenameNoReplacePreservesExistingDestination(t *testing.T) {
	d := testDiskRoot(t)
	source := filepath.Join(d.path, "source")
	destination := filepath.Join(d.path, "destination")
	writeDiskTestFile(t, source, "new bytes", 0o600)
	writeDiskTestFile(t, destination, "published bytes", 0o600)
	if err := renameNoReplace(d.root, "source", "destination"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("rename to existing destination = %v, want already exists", err)
	}
	if got := readDiskTestFile(t, source); got != "new bytes" {
		t.Fatal("failed rename changed the source")
	}
	if got := readDiskTestFile(t, destination); got != "published bytes" {
		t.Fatal("failed rename overwrote the destination")
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(d.root, "source", "destination"); err != nil {
		t.Fatalf("rename to absent destination: %v", err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source after successful rename = %v, want absent", err)
	}
	if got := readDiskTestFile(t, destination); got != "new bytes" {
		t.Fatal("successful rename did not publish the source")
	}
}

func TestDiskRootOperationsFailAfterClose(t *testing.T) {
	d := testDiskRoot(t)
	writeDiskTestFile(t, filepath.Join(d.path, "payload"), "retained bytes", 0o600)
	if err := os.Mkdir(filepath.Join(d.path, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, lock := d.root, d.lock
	if err := d.close(); err != nil {
		t.Fatal(err)
	}
	if err := d.close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	if err := d.check(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("check after close = %v, want closed", err)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("lock after close = %v, want closed", err)
	}
	file, err := openRegular(root, "payload", os.O_RDONLY, 0)
	if file != nil {
		_ = file.Close()
		t.Fatal("file was opened through a closed root")
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("file open after close = %v, want closed", err)
	}
	child, err := openChild(root, "child")
	if child != nil {
		_ = child.Close()
		t.Fatal("directory was opened through a closed root")
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("child open after close = %v, want closed", err)
	}
	if err := renameNoReplace(root, "payload", "moved"); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("rename after close = %v, want closed", err)
	}
	if err := syncDirectory(root); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("sync after close = %v, want closed", err)
	}
	if got := readDiskTestFile(t, filepath.Join(d.path, "payload")); got != "retained bytes" {
		t.Fatal("operation through a closed root changed its files")
	}
}
