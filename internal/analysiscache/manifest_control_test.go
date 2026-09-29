//go:build linux

package analysiscache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedControlCleanupRemovesOnlyItsCreatedFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	created, err := openRegular(root, ownerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Close()
	if _, err := created.Write([]byte("incomplete owner")); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(directory, "operator-note")
	writeDiskTestFile(t, unrelated, "preserve unrelated content", 0o600)
	if err := removeFailedControl(root, ownerName, created); err != nil {
		t.Fatalf("remove the writer's own incomplete control: %v", err)
	}
	if _, err := root.Lstat(ownerName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete control remained after verified cleanup: %v", err)
	}
	if got := readDiskTestFile(t, unrelated); got != "preserve unrelated content" {
		t.Fatal("control cleanup changed unrelated content")
	}
}

func TestFailedControlCleanupRetainsReplacedOrLinkedNames(t *testing.T) {
	for _, scenario := range []string{"replacement", "symlink", "hardlink"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			created, err := openRegular(root, ownerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer created.Close()
			if _, err := created.Write([]byte("incomplete owner")); err != nil {
				t.Fatal(err)
			}
			path, retained := filepath.Join(directory, ownerName), filepath.Join(directory, "retained-original")
			if scenario == "hardlink" {
				if err := os.Link(path, retained); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(path, retained); err != nil {
					t.Fatal(err)
				}
				if scenario == "replacement" {
					writeDiskTestFile(t, path, "replacement must survive", 0o600)
				} else if err := os.Symlink("retained-original", path); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := removeFailedControl(root, ownerName, created); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("changed control identity was accepted for cleanup: %v", err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("failed cleanup changed the current control name: %v", err)
			}
			if got := readDiskTestFile(t, retained); got != "incomplete owner" {
				t.Fatal("failed cleanup changed the retained original inode")
			}
			if scenario == "replacement" && readDiskTestFile(t, path) != "replacement must survive" {
				t.Fatal("failed cleanup changed replacement content")
			}
		})
	}
}

func TestWriteControlRetainsAnAlreadyExistingFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := filepath.Join(directory, ownerName)
	writeDiskTestFile(t, path, "existing control", 0o600)
	if err := writeControl(root, ownerName, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("control creation did not reject an existing file: %v", err)
	}
	if got := readDiskTestFile(t, path); got != "existing control" {
		t.Fatal("failed exclusive creation removed or overwrote an existing control")
	}
}
