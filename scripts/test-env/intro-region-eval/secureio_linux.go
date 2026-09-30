//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Each directory is opened relative to an already owned descriptor. A path
// component cannot be swapped to a symlink between its inspection and use.
func openDirectory(path string) (*os.File, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, openErr := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if openErr != nil {
			return nil, &os.PathError{Op: "open directory without symlinks", Path: path, Err: openErr}
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openRegular(path string) (*os.File, error) {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := syscall.Openat(int(parent.Fd()), filepath.Base(path), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open regular input", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = errors.New("input is not a regular file")
		}
		return nil, err
	}
	return file, nil
}

type reportWriter struct {
	parent    *os.File
	root      *os.Root
	file      *os.File
	temporary string
	target    string
}

func newReportWriter(path string) (*reportWriter, error) {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	// The final directory must be private to its owner. This protects the
	// staging name from replacement before the atomic hard-link publication.
	info, err := parent.Stat()
	if err != nil {
		parent.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0022 != 0 {
		parent.Close()
		return nil, errors.New("report directory must be owned by the current user and not writable by group or others")
	}
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", parent.Fd()))
	if err != nil {
		parent.Close()
		return nil, err
	}
	w := &reportWriter{parent: parent, root: root, target: filepath.Base(path)}
	if _, err := root.Lstat(w.target); err == nil {
		w.Close()
		return nil, errors.New("report path already exists")
	} else if !os.IsNotExist(err) {
		w.Close()
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		w.Close()
		return nil, err
	}
	temporary := ".intro-region-eval-" + hex.EncodeToString(random[:]) + ".tmp"
	w.file, err = root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		w.Close()
		return nil, err
	}
	w.temporary = temporary
	return w, nil
}

func (w *reportWriter) Publish(data []byte) error {
	if _, err := w.file.Write(data); err != nil {
		return err
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	// Link atomically creates a complete report and fails if the name exists.
	// There is deliberately no Rename or overwrite fallback.
	if err := w.root.Link(w.temporary, w.target); err != nil {
		return err
	}
	if err := w.parent.Sync(); err != nil {
		return err
	}
	return nil
}

func (w *reportWriter) Close() {
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	if w.root != nil {
		if w.temporary != "" {
			w.root.Remove(w.temporary)
		}
		w.root.Close()
		w.root = nil
	}
	if w.parent != nil {
		w.parent.Close()
		w.parent = nil
	}
}
