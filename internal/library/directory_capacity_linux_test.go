//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDirectoryCapacityObservesNativeFilesystem(t *testing.T) {
	path := t.TempDir()
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(directory.Fd()), &filesystem); err != nil {
		t.Fatal(err)
	}
	if filesystem.Bsize <= 0 {
		t.Fatalf("owned directory has an invalid native block size: %d", filesystem.Bsize)
	}
	got, err := observeDirectoryCapacity(context.Background(), directory)
	if err != nil || got == nil {
		t.Fatalf("owned filesystem capacity was unavailable: got=%+v err=%v", got, err)
	}
	blockSize := uint64(filesystem.Bsize)
	wantTotal := filesystem.Blocks * blockSize
	if got.totalBytes != wantTotal || got.usedBytes > wantTotal || got.availableBytes > wantTotal {
		t.Fatalf("capacity differs from the held native filesystem: got=%+v native=%+v", got, filesystem)
	}
	// Other processes can change free space between the two native observations.
	if got.usedBytes%blockSize != 0 || got.availableBytes%blockSize != 0 {
		t.Fatalf("filesystem byte counts lost their native block size: got=%+v blockSize=%d", got, blockSize)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("capacity observation changed the directory: entries=%v err=%v", entries, err)
	}
}

func TestDirectoryCapacityUsesHeldDirectoryWithoutEnumeratingOrWriting(t *testing.T) {
	base := t.TempDir()
	path, retained := filepath.Join(base, "named"), filepath.Join(base, "retained")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	before, err := directory.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original directory name still exists: %v", err)
	}
	got, err := observeDirectoryCapacity(context.Background(), directory)
	if err != nil || got == nil || got.totalBytes == 0 {
		t.Fatalf("renamed held directory lost its filesystem observation: got=%+v err=%v", got, err)
	}
	after, err := directory.Seek(0, io.SeekCurrent)
	if err != nil || after != before {
		t.Fatalf("capacity observation enumerated the held directory: before=%d after=%d err=%v", before, after, err)
	}
	entries, err := os.ReadDir(retained)
	if err != nil || len(entries) != 2 || entries[0].Name() != "first.txt" || entries[1].Name() != "second.txt" {
		t.Fatalf("capacity observation changed directory entries: entries=%v err=%v", entries, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(retained, entry.Name()))
		if err != nil || string(data) != entry.Name() {
			t.Fatalf("capacity observation changed a fixture file: name=%s data=%q err=%v", entry.Name(), data, err)
		}
	}
}

func TestDirectoryCapacityKeepsInvalidDescriptorsAndCancellationDistinct(t *testing.T) {
	path := t.TempDir()
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	closed, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	regularPath := filepath.Join(path, "regular.txt")
	if err := os.WriteFile(regularPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	regular, err := os.Open(regularPath)
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	for _, test := range []struct {
		name string
		file *os.File
	}{
		{name: "nil descriptor"},
		{name: "closed descriptor", file: closed},
		{name: "regular file", file: regular},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := observeDirectoryCapacity(context.Background(), test.file)
			if got != nil || err != nil {
				t.Fatalf("invalid directory descriptor invented capacity: got=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, file := range []*os.File{nil, closed, regular, directory} {
		got, err := observeDirectoryCapacity(ctx, file)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation became an unknown capacity observation: got=%+v err=%v", got, err)
		}
	}
}
