//go:build linux

package analysiscache

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	diskMarkerName    = ".goby-analysis-cache"
	diskMarkerVersion = "goby-analysis-cache-v1"
	diskLockName      = ".writer-lock"
	diskMarkerLimit   = 256
)

type diskRoot struct {
	root           *os.Root
	path           string
	info           os.FileInfo
	owner          string
	lock           *os.File
	markerInfo     os.FileInfo
	markerIdentity string
}

type diskMarker struct {
	Marker string `json:"marker"`
	Owner  string `json:"owner"`
}

// openDiskRoot admits only an empty directory or an existing owned cache. The
// lock remains held until close, including while the marker is initialized.
func openDiskRoot(path string) (_ *diskRoot, err error) {
	root, err := openDiskPath(path, true)
	if err != nil {
		return nil, err
	}
	d := &diskRoot{root: root, path: path}
	defer func() {
		if err != nil {
			_ = d.close()
		}
	}()
	d.info, err = root.Stat(".")
	if err != nil {
		return nil, err
	}
	if err = checkOwnedDirectory(d.info); err != nil {
		return nil, err
	}
	mayInitialize := false
	if _, markerErr := root.Lstat(diskMarkerName); errors.Is(markerErr, os.ErrNotExist) {
		names, readErr := diskDirectoryNames(root, 1)
		if readErr != nil {
			return nil, readErr
		}
		if len(names) != 0 {
			return nil, fmt.Errorf("%w: nonempty cache directory has no ownership marker", ErrUnsafe)
		}
		mayInitialize = true
	} else if markerErr != nil {
		return nil, markerErr
	}
	d.lock, err = openRegular(root, diskLockName, os.O_RDWR|os.O_CREATE, 0o600)
	if errors.Is(err, os.ErrExist) {
		// Another opener may have created the lock after the initial lstat.
		// Reopening still verifies its type, ownership, permissions, and inode.
		d.lock, err = openRegular(root, diskLockName, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(d.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, fmt.Errorf("%w: analysis cache is already locked", ErrOwned)
		}
		return nil, fmt.Errorf("lock analysis cache: %w", err)
	}
	if err = d.checkPath(); err != nil {
		return nil, err
	}
	if err = d.checkLock(); err != nil {
		return nil, err
	}
	if _, markerErr := root.Lstat(diskMarkerName); errors.Is(markerErr, os.ErrNotExist) {
		if !mayInitialize {
			return nil, fmt.Errorf("%w: ownership marker disappeared", ErrUnsafe)
		}
		names, readErr := diskDirectoryNames(root, 2)
		if readErr != nil {
			return nil, readErr
		}
		if len(names) != 1 || names[0] != diskLockName {
			return nil, fmt.Errorf("%w: directory changed before initialization", ErrUnsafe)
		}
		if err = d.createMarker(); err != nil {
			return nil, err
		}
	} else if markerErr != nil {
		return nil, markerErr
	}
	if err = d.readMarker(true); err != nil {
		return nil, err
	}
	if err = d.check(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *diskRoot) check() error {
	if d == nil || d.root == nil || d.lock == nil {
		return os.ErrClosed
	}
	if err := d.checkPath(); err != nil {
		return err
	}
	if err := d.checkLock(); err != nil {
		return err
	}
	if err := d.readMarker(false); err != nil {
		return err
	}
	return d.checkPath()
}

func (d *diskRoot) close() error {
	if d == nil {
		return nil
	}
	var err error
	if d.root != nil {
		err = d.root.Close()
		d.root = nil
	}
	if d.lock != nil {
		err = errors.Join(err, d.lock.Close())
		d.lock = nil
	}
	return err
}

func (d *diskRoot) checkPath() error {
	info, err := d.root.Stat(".")
	if err != nil {
		return err
	}
	if err := checkOwnedDirectory(info); err != nil {
		return err
	}
	if !os.SameFile(d.info, info) {
		return fmt.Errorf("%w: opened cache directory changed", ErrUnsafe)
	}
	named, err := openDiskPath(d.path, false)
	if err != nil {
		return fmt.Errorf("%w: cache path cannot be reopened: %w", ErrUnsafe, err)
	}
	defer named.Close()
	current, err := named.Stat(".")
	if err != nil {
		return err
	}
	if err := checkOwnedDirectory(current); err != nil {
		return err
	}
	if !os.SameFile(d.info, current) {
		return fmt.Errorf("%w: cache path now names another directory", ErrUnsafe)
	}
	return nil
}

func (d *diskRoot) checkLock() error {
	held, err := d.lock.Stat()
	if err != nil {
		return err
	}
	if err := checkOwnedRegular(held); err != nil {
		return err
	}
	current, err := d.root.Lstat(diskLockName)
	if err != nil {
		return fmt.Errorf("%w: cache lock is unavailable: %w", ErrUnsafe, err)
	}
	if err := checkOwnedRegular(current); err != nil {
		return err
	}
	if !os.SameFile(held, current) || held.Size() != 0 || current.Size() != 0 {
		return fmt.Errorf("%w: cache lock changed", ErrUnsafe)
	}
	return nil
}

func (d *diskRoot) createMarker() error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	body, err := json.Marshal(diskMarker{Marker: diskMarkerVersion, Owner: hex.EncodeToString(nonce[:])})
	if err != nil {
		return err
	}
	file, err := openRegular(d.root, diskMarkerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(body)
	if writeErr == nil && n != len(body) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return syncDirectory(d.root)
}

func (d *diskRoot) readMarker(capture bool) error {
	file, err := openRegular(d.root, diskMarkerName, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("%w: ownership marker is unavailable: %w", ErrUnsafe, err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return err
	}
	identity, err := fileIdentity(before)
	if err != nil {
		return err
	}
	if before.Size() <= 0 || before.Size() > diskMarkerLimit {
		return fmt.Errorf("%w: ownership marker has an unexpected size", ErrUnsafe)
	}
	if !capture && (!os.SameFile(d.markerInfo, before) || identity != d.markerIdentity) {
		return fmt.Errorf("%w: ownership marker changed", ErrUnsafe)
	}
	body, err := io.ReadAll(io.LimitReader(file, diskMarkerLimit+1))
	if err != nil {
		return err
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := d.root.Lstat(diskMarkerName)
	if err != nil {
		return fmt.Errorf("%w: ownership marker disappeared: %w", ErrUnsafe, err)
	}
	for _, info := range []os.FileInfo{after, named} {
		if err := checkOwnedRegular(info); err != nil {
			return err
		}
		current, err := fileIdentity(info)
		if err != nil {
			return err
		}
		if !os.SameFile(before, info) || current != identity {
			return fmt.Errorf("%w: ownership marker changed while reading", ErrUnsafe)
		}
	}
	var marker diskMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		return fmt.Errorf("%w: invalid ownership marker", ErrUnsafe)
	}
	canonical, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if marker.Marker != diskMarkerVersion || !validDiskOwner(marker.Owner) || !bytes.Equal(body, canonical) {
		return fmt.Errorf("%w: unrecognized ownership marker", ErrUnsafe)
	}
	if capture {
		d.owner, d.markerInfo, d.markerIdentity = marker.Owner, before, identity
	} else if marker.Owner != d.owner {
		return fmt.Errorf("%w: cache owner changed", ErrUnsafe)
	}
	return nil
}

// openDiskPath walks each component from the filesystem root. Only the final
// missing component may be created; existing ancestors are never chmodded.
func openDiskPath(path string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > 4096 || strings.ContainsAny(path, "\\\x00\r\n") {
		return nil, fmt.Errorf("%w: cache path must be a canonical absolute directory", ErrUnsafe)
	}
	current, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, name := range parts {
		next, openErr := openDiskDirectory(current, name, false)
		if errors.Is(openErr, os.ErrNotExist) && create && index == len(parts)-1 {
			mkdirErr := current.Mkdir(name, 0o700)
			if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				_ = current.Close()
				return nil, mkdirErr
			}
			if mkdirErr == nil {
				if err := syncDiskDirectory(current, false); err != nil {
					_ = current.Close()
					return nil, err
				}
			}
			next, openErr = openDiskDirectory(current, name, false)
		}
		_ = current.Close()
		if openErr != nil {
			return nil, openErr
		}
		current = next
	}
	return current, nil
}

func openChild(root *os.Root, name string) (*os.Root, error) {
	return openDiskDirectory(root, name, true)
}

func openDiskDirectory(root *os.Root, name string, owned bool) (*os.Root, error) {
	if root == nil || !validDiskName(name) {
		return nil, fmt.Errorf("%w: invalid directory name", ErrUnsafe)
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, diskOpenError(err)
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: directory is not a real directory", ErrUnsafe)
	}
	if owned {
		if err := checkOwnedDirectory(before); err != nil {
			return nil, err
		}
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, diskOpenError(err)
	}
	after, statErr := child.Stat(".")
	named, nameErr := root.Lstat(name)
	if statErr != nil || nameErr != nil {
		_ = child.Close()
		return nil, errors.Join(statErr, nameErr)
	}
	if !after.IsDir() || !named.IsDir() || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) || !os.SameFile(after, named) {
		_ = child.Close()
		return nil, fmt.Errorf("%w: directory changed while opening", ErrUnsafe)
	}
	if owned {
		for _, info := range []os.FileInfo{after, named} {
			if err := checkOwnedDirectory(info); err != nil {
				_ = child.Close()
				return nil, err
			}
		}
	}
	return child, nil
}

func openRegular(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	if root == nil || !validDiskName(name) || flags&os.O_CREATE != 0 && mode != 0o600 {
		return nil, fmt.Errorf("%w: invalid owned file open", ErrUnsafe)
	}
	before, err := root.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, diskOpenError(err)
	}
	if err == nil {
		if err := checkOwnedRegular(before); err != nil {
			return nil, err
		}
	} else if flags&os.O_CREATE == 0 {
		return nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	directoryInfo, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	if err := checkOwnedDirectory(directoryInfo); err != nil {
		return nil, err
	}
	// Defer truncation until the opened inode has passed all identity checks.
	// An absent name must be created exclusively, even without caller O_EXCL.
	openFlags := flags &^ os.O_TRUNC
	if before == nil {
		openFlags |= os.O_EXCL
	} else if flags&os.O_EXCL == 0 {
		openFlags &^= os.O_CREATE
	}
	fd, err := unix.Openat(int(directory.Fd()), name, openFlags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return nil, diskOpenError(err)
	}
	file := os.NewFile(uintptr(fd), name)
	opened, statErr := file.Stat()
	named, nameErr := root.Lstat(name)
	if statErr != nil || nameErr != nil {
		_ = file.Close()
		return nil, errors.Join(statErr, nameErr)
	}
	for _, info := range []os.FileInfo{opened, named} {
		if err := checkOwnedRegular(info); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	if !os.SameFile(opened, named) || before != nil && !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("%w: owned file changed while opening", ErrUnsafe)
	}
	if flags&os.O_TRUNC != 0 {
		if err := file.Truncate(0); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return file, nil
}

// fileIdentity includes nanosecond timestamps as separate seconds and
// nanoseconds fields, avoiding the overflow of a single UnixNano integer.
func fileIdentity(info os.FileInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("%w: missing file identity", ErrUnsafe)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Size() < 0 {
		return "", fmt.Errorf("%w: unavailable file identity", ErrUnsafe)
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", uint64(stat.Dev), stat.Ino,
		stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec, info.Size()), nil
}

func renameNoReplace(root *os.Root, oldName, newName string) error {
	if root == nil || !validDiskName(oldName) || !validDiskName(newName) {
		return fmt.Errorf("%w: invalid rename name", ErrUnsafe)
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if err := checkOwnedDirectory(info); err != nil {
		return err
	}
	return diskOpenError(unix.Renameat2(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_NOREPLACE))
}

func syncDirectory(root *os.Root) error {
	return syncDiskDirectory(root, true)
}

func syncDiskDirectory(root *os.Root, owned bool) error {
	if root == nil {
		return os.ErrClosed
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: directory descriptor is not a directory", ErrUnsafe)
	}
	if owned {
		if err := checkOwnedDirectory(info); err != nil {
			return err
		}
	}
	return directory.Sync()
}

func diskDirectoryNames(root *os.Root, limit int) ([]string, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	names, err := directory.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > limit {
		return nil, fmt.Errorf("%w: unexpected cache contents before initialization", ErrUnsafe)
	}
	return names, nil
}

func checkOwnedDirectory(info os.FileInfo) error {
	if info == nil {
		return fmt.Errorf("%w: missing directory identity", ErrUnsafe)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Mode&0o7777 != 0o700 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink == 0 {
		return fmt.Errorf("%w: cache directory must be owned by the process user with mode 0700", ErrUnsafe)
	}
	return nil
}

func checkOwnedRegular(info os.FileInfo) error {
	if info == nil {
		return fmt.Errorf("%w: missing file identity", ErrUnsafe)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Mode&0o7777 != 0o600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Size() < 0 {
		return fmt.Errorf("%w: cache file must be an owned regular file with mode 0600 and one link", ErrUnsafe)
	}
	return nil
}

func validDiskName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00\r\n")
}

func validDiskOwner(owner string) bool {
	if len(owner) != 32 {
		return false
	}
	for _, char := range owner {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func diskOpenError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errors.Join(ErrUnsafe, err)
	}
	return err
}
