//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCLIPassphraseFilesPreserveExactBytesAndRejectUnsafeInputs(t *testing.T) {
	_ = cliTestConfig(t)
	root := t.TempDir()
	path := filepath.Join(root, "passphrase")
	content := []byte("  exact UTF-8 secret \xc3\xa9\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal("write owned passphrase fixture")
	}
	value, err := readCLIPassphrase(context.Background(), path, false)
	if err != nil || !bytes.Equal(value, content) {
		t.Fatal("passphrase bytes were trimmed or normalized")
	}
	clear(value)
	for _, test := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"short", []byte("elevenbytes"), 0600},
		{"long", []byte(strings.Repeat("x", 1025)), 0600},
		{"utf8", append([]byte(strings.Repeat("x", 12)), 0xff), 0600},
		{"readable", content, 0644},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(root, test.name)
			if os.WriteFile(file, test.data, 0600) != nil || os.Chmod(file, test.mode) != nil {
				t.Fatal("prepare owned rejected passphrase")
			}
			value, err := readCLIPassphrase(context.Background(), file, false)
			if !errors.Is(err, errCLIInput) || value != nil {
				t.Fatal("unsafe passphrase file was accepted")
			}
		})
	}
	linked := filepath.Join(root, "hardlink")
	if err := os.Link(path, linked); err != nil {
		t.Fatal("create owned passphrase hardlink")
	}
	if value, err := readCLIPassphrase(context.Background(), linked, false); !errors.Is(err, errCLIInput) || value != nil {
		t.Fatal("multiply linked passphrase was accepted")
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal("create owned passphrase symlink")
	}
	if value, err := readCLIPassphrase(context.Background(), symlink, false); !errors.Is(err, errCLIInput) || value != nil {
		t.Fatal("symlink passphrase was followed")
	}
	pipe := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(pipe, 0600); err != nil {
		t.Fatal("create owned nonregular passphrase")
	}
	if value, err := readCLIPassphrase(context.Background(), pipe, false); !errors.Is(err, errCLIInput) || value != nil {
		t.Fatal("nonregular passphrase was accepted")
	}
}

func TestCLIPassphraseStdinCancellationClosesOnlyItsOwnedInput(t *testing.T) {
	_ = cliTestConfig(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal("create owned stdin pipe")
	}
	defer reader.Close()
	defer writer.Close()
	previous := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finished := make(chan struct{}), make(chan error, 1)
	go func() { close(started); value, err := readCLIPassphrase(ctx, "", true); clear(value); finished <- err }()
	<-started
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, errCLIInput) {
			t.Fatal("cancelled passphrase stdin was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled passphrase reader retained its blocked stdin")
	}
	if os.Stdin != reader {
		t.Fatal("passphrase cancellation replaced the process input")
	}
}

func TestCLIArchiveInputRejectsSymlinksDirectoriesEmptyAndNonregularFiles(t *testing.T) {
	_ = cliTestConfig(t)
	root := t.TempDir()
	archive := filepath.Join(root, "archive.age")
	if err := os.WriteFile(archive, []byte("encrypted transport fixture"), 0600); err != nil {
		t.Fatal("write owned archive input")
	}
	file, err := openCLIArchive(archive)
	if err != nil {
		t.Fatal("regular archive input was rejected")
	}
	file.Close()
	if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0600); err != nil {
		t.Fatal("write owned empty input")
	}
	if err := os.Symlink(archive, filepath.Join(root, "link")); err != nil {
		t.Fatal("create owned archive symlink")
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal("create owned archive fifo")
	}
	for _, path := range []string{"", root, filepath.Join(root, "empty"), filepath.Join(root, "link"), filepath.Join(root, "fifo")} {
		file, err := openCLIArchive(path)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, errCLIInput) || file != nil {
			t.Fatal("unsafe archive source was opened")
		}
	}
}
