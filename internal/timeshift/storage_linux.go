package timeshift

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const rootMarker = ".goby-timeshift-root"
const rootMarkerBody = "goby-timeshift-root-v1\n"
const windowMarker = ".goby-timeshift-window"

type storageRoot struct {
	dir  *os.File
	unit int64
}
type storageWindow struct {
	root *storageRoot
	dir  *os.File
	id   string
}

func openStorage(path string) (_ *storageRoot, resultErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, ErrUnsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		next, openErr := openDirectoryAt(dir, part)
		if errors.Is(openErr, os.ErrNotExist) && index == len(parts)-1 {
			if err := unix.Mkdirat(int(dir.Fd()), part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
				_ = dir.Close()
				return nil, err
			}
			next, openErr = openDirectoryAt(dir, part)
		}
		_ = dir.Close()
		if openErr != nil {
			return nil, openErr
		}
		dir = next
	}
	root := &storageRoot{dir: dir}
	defer func() {
		if resultErr != nil {
			_ = root.close()
		}
	}()
	if err := ownedDirectory(dir); err != nil {
		return nil, err
	}
	if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &filesystem); err != nil {
		return nil, err
	}
	root.unit = int64(filesystem.Bsize)
	if root.unit < 512 || root.unit > 1<<20 {
		return nil, ErrUnsafe
	}
	names, err := directoryNames(dir, hardWindows+1)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		if err := writeMarker(dir, rootMarker, rootMarkerBody); err != nil {
			return nil, err
		}
	} else if err := checkMarker(dir, rootMarker, rootMarkerBody); err != nil {
		return nil, err
	}
	if err := root.recover(); err != nil {
		return nil, err
	}
	return root, nil
}

func openDirectoryAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func ownedDirectory(dir *os.File) error {
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	return nil
}

func openOwnedFile(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
			err = ErrUnsafe
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func directoryNames(dir *os.File, limit int) ([]string, error) {
	reader, err := openDirectoryAt(dir, ".")
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var names []string
	for {
		batch, err := reader.Readdirnames(128)
		names = append(names, batch...)
		if len(names) > limit {
			return nil, ErrUnsafe
		}
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func writeMarker(dir *os.File, name, body string) error {
	file, err := openOwnedFile(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	_, err = io.WriteString(file, body)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = dir.Sync()
	}
	return err
}

func checkMarker(dir *os.File, name, body string) error {
	file, err := openOwnedFile(dir, name, unix.O_RDONLY)
	if err != nil {
		return ErrUnsafe
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(len(body))+1))
	if err != nil || string(data) != body {
		return ErrUnsafe
	}
	return nil
}

func validStorageID(id, prefix string) bool {
	return len(id) == len(prefix)+32 && strings.HasPrefix(id, prefix) && strings.Trim(strings.TrimPrefix(id, prefix), "0123456789abcdef") == ""
}

func windowMarkerBody(id string) string { return "goby-timeshift-window-v1:" + id + "\n" }

// Recovery inspects every entry before deleting any media. An unfamiliar name,
// link, owner, marker or file type stops recovery without claiming ownership.
func (root *storageRoot) recover() error {
	names, err := directoryNames(root.dir, hardWindows+1)
	if err != nil {
		return err
	}
	type inspected struct {
		window *storageWindow
		files  []string
	}
	var windows []inspected
	defer func() {
		for _, item := range windows {
			_ = item.window.dir.Close()
		}
	}()
	total := 0
	for _, id := range names {
		if id == rootMarker {
			continue
		}
		if !validStorageID(id, "w_") {
			return ErrUnsafe
		}
		dir, err := openDirectoryAt(root.dir, id)
		if err != nil {
			return ErrUnsafe
		}
		window := &storageWindow{root: root, dir: dir, id: id}
		windows = append(windows, inspected{window: window})
		if err := ownedDirectory(dir); err != nil {
			return err
		}
		if err := checkMarker(dir, windowMarker, windowMarkerBody(id)); err != nil {
			return err
		}
		files, err := directoryNames(dir, hardArtifacts+1)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, name := range files {
			if name == windowMarker {
				continue
			}
			base, extension := strings.CutSuffix(name, ".media")
			if !extension {
				base, extension = strings.CutSuffix(name, ".part")
			}
			if !extension || !validStorageID(base, "a_") {
				return ErrUnsafe
			}
			if seen[base] {
				return ErrUnsafe
			}
			seen[base] = true
			file, err := openOwnedFile(dir, name, unix.O_RDONLY)
			if err != nil {
				return ErrUnsafe
			}
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr != nil || info.Size() > 256<<20 || info.Size() < 0 || strings.HasSuffix(name, ".media") && info.Size() == 0 {
				return ErrUnsafe
			}
			windows[len(windows)-1].files = append(windows[len(windows)-1].files, name)
			total++
			if total > hardArtifacts {
				return ErrUnsafe
			}
		}
	}
	for _, item := range windows {
		for _, name := range item.files {
			if err := item.window.removeName(name); err != nil {
				return err
			}
		}
		if err := item.window.destroy(); err != nil {
			return err
		}
	}
	return nil
}

func (root *storageRoot) create(id string) (*storageWindow, error) {
	if !validStorageID(id, "w_") {
		return nil, ErrInvalid
	}
	if err := unix.Mkdirat(int(root.dir.Fd()), id, 0700); err != nil {
		return nil, err
	}
	dir, err := openDirectoryAt(root.dir, id)
	if err != nil {
		return nil, err
	}
	window := &storageWindow{root: root, dir: dir, id: id}
	if err := writeMarker(dir, windowMarker, windowMarkerBody(id)); err != nil {
		_ = unix.Unlinkat(int(dir.Fd()), windowMarker, 0)
		_ = dir.Close()
		_ = unix.Unlinkat(int(root.dir.Fd()), id, unix.AT_REMOVEDIR)
		return nil, err
	}
	if err := root.dir.Sync(); err != nil {
		_ = window.destroy()
		_ = dir.Close()
		return nil, err
	}
	return window, nil
}

func (root *storageRoot) charge(size int64) int64 {
	return ((size + root.unit - 1) / root.unit) * root.unit
}

func sameFileVersion(before, after os.FileInfo) bool {
	first, firstOK := before.Sys().(*syscall.Stat_t)
	second, secondOK := after.Sys().(*syscall.Stat_t)
	return firstOK && secondOK && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime() == after.ModTime() && first.Ctim == second.Ctim
}

func (window *storageWindow) copy(ctx context.Context, id string, source *os.File, before os.FileInfo) (charge int64, resultErr error) {
	if !validStorageID(id, "a_") {
		return 0, ErrInvalid
	}
	name := id + ".part"
	file, err := openOwnedFile(window.dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	buffer := make([]byte, 64*1024)
	var offset int64
	for offset < before.Size() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		length := min(int64(len(buffer)), before.Size()-offset)
		n, err := source.ReadAt(buffer[:int(length)], offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if n != int(length) {
			return 0, ErrInvalid
		}
		if written, err := file.Write(buffer[:n]); err != nil || written != n {
			return 0, errors.Join(err, io.ErrShortWrite)
		}
		offset += int64(n)
	}
	after, err := source.Stat()
	if err != nil || !sameFileVersion(before, after) {
		return 0, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != before.Size() {
		return 0, ErrStorage
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, ErrStorage
	}
	charge = max(window.root.charge(info.Size()), int64(stat.Blocks)*512)
	if err := unix.Renameat(int(window.dir.Fd()), name, int(window.dir.Fd()), id+".media"); err != nil {
		return 0, err
	}
	return charge, nil
}

func (window *storageWindow) open(id string) (*os.File, error) {
	if !validStorageID(id, "a_") {
		return nil, ErrNotFound
	}
	return openOwnedFile(window.dir, id+".media", unix.O_RDONLY)
}

func (window *storageWindow) removeName(name string) error {
	file, err := openOwnedFile(window.dir, name, unix.O_RDONLY)
	if err != nil {
		return err
	}
	_ = file.Close()
	return unix.Unlinkat(int(window.dir.Fd()), name, 0)
}

func (window *storageWindow) remove(id string) error {
	if !validStorageID(id, "a_") {
		return ErrInvalid
	}
	var result error
	for _, extension := range []string{".media", ".part"} {
		err := window.removeName(id + extension)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (window *storageWindow) destroy() error {
	names, err := directoryNames(window.dir, 1)
	if err != nil || len(names) != 1 || names[0] != windowMarker {
		return ErrUnsafe
	}
	if err := checkMarker(window.dir, windowMarker, windowMarkerBody(window.id)); err != nil {
		return err
	}
	if err := window.removeName(windowMarker); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(window.root.dir.Fd()), window.id, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	return nil
}

func (root *storageRoot) close() error {
	if root == nil || root.dir == nil {
		return nil
	}
	return root.dir.Close()
}

func (window *storageWindow) close() error { return window.dir.Close() }

func (root *storageRoot) String() string {
	return fmt.Sprintf("timeshift storage allocation unit %d", root.unit)
}
