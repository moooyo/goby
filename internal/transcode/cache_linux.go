//go:build linux

package transcode

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	ErrCacheInvalid = errors.New("invalid transcode cache")
	ErrCacheLocked  = errors.New("transcode cache is already locked")
	ErrCacheUnsafe  = errors.New("unsafe transcode cache content")
)

const (
	cacheMarkerName    = ".goby-transcode-cache"
	cacheMarkerVersion = "goby-transcode-cache-v1\n"
	cacheLockName      = ".goby-transcode-lock"
	maxCacheEntries    = 4096
	maxJobFiles        = 65536
	cacheReadBatch     = 256
)

// cacheRoot owns an exclusive process lock until Close. Every filesystem
// operation is relative to an opened directory descriptor, and each supplied
// component is opened without following symbolic links.
type cacheRoot struct {
	mu     sync.Mutex
	path   string
	dir    *os.File
	lock   *os.File
	closed bool
}

type cacheFileSnapshot struct {
	name string
	info os.FileInfo
}

type cacheJobSnapshot struct {
	name string
	info os.FileInfo
}

func openCacheRoot(path string) (_ *cacheRoot, err error) {
	dir, err := openCacheDirectory(path, true)
	if err != nil {
		return nil, err
	}
	c := &cacheRoot{path: path, dir: dir}
	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()
	info, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%w: cache directory must be owned by the process user and not writable by other users", ErrCacheUnsafe)
	}
	names, err := cacheDirectoryNames(dir, maxCacheEntries)
	if err != nil {
		return nil, err
	}
	hasMarker := false
	for _, name := range names {
		hasMarker = hasMarker || name == cacheMarkerName
	}
	if !hasMarker && len(names) != 0 {
		return nil, fmt.Errorf("%w: nonempty directory has no ownership marker", ErrCacheInvalid)
	}
	c.lock, err = cacheOpenRegular(dir, cacheLockName, syscall.O_RDWR|syscall.O_CREAT, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(c.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrCacheLocked
		}
		return nil, fmt.Errorf("lock transcode cache: %w", err)
	}
	// Recheck after taking the lock. Another owner may have initialized this
	// formerly empty directory while this opener was acquiring its lock.
	if err = c.ensureMarker(true); err != nil {
		return nil, err
	}
	if _, err = c.inspectRoot(); err != nil {
		return nil, err
	}
	return c, nil
}

// openCacheDirectory walks an absolute, canonical path from the filesystem
// root. Only the final missing component may be created.
func openCacheDirectory(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, ErrCacheInvalid
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range parts {
		next, openErr := cacheOpenDirectoryAt(dir, name)
		if errors.Is(openErr, os.ErrNotExist) && create && i == len(parts)-1 {
			if mkdirErr := syscall.Mkdirat(int(dir.Fd()), name, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
				_ = dir.Close()
				return nil, mkdirErr
			}
			next, openErr = cacheOpenDirectoryAt(dir, name)
		}
		_ = dir.Close()
		if openErr != nil {
			return nil, openErr
		}
		dir = next
	}
	return dir, nil
}

func (c *cacheRoot) RootPath() string { return c.path }

// JobPath addresses the opened cache root through the owning process. FFmpeg
// must finish using this path before Close releases the root descriptor. Using
// the parent's PID avoids depending on descriptor numbers inside the child.
func (c *cacheRoot) JobPath(id string) (string, error) {
	if !validJobID(id) {
		return "", ErrCacheInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", os.ErrClosed
	}
	return fmt.Sprintf("/proc/%d/fd/%d/%s", os.Getpid(), c.dir.Fd(), id), nil
}

func (c *cacheRoot) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var err error
	if c.lock != nil {
		err = c.lock.Close()
	}
	if c.dir != nil {
		err = errors.Join(err, c.dir.Close())
	}
	return err
}

func (c *cacheRoot) ensureMarker(create bool) error {
	marker, err := cacheOpenRegular(c.dir, cacheMarkerName, syscall.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return fmt.Errorf("%w: ownership marker is missing", ErrCacheUnsafe)
		}
		names, listErr := cacheDirectoryNames(c.dir, maxCacheEntries)
		if listErr != nil {
			return listErr
		}
		for _, name := range names {
			if name != cacheLockName {
				return fmt.Errorf("%w: directory changed before initialization", ErrCacheUnsafe)
			}
		}
		marker, err = cacheOpenRegular(c.dir, cacheMarkerName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(marker, cacheMarkerVersion)
		if writeErr == nil {
			writeErr = marker.Sync()
		}
		closeErr := marker.Close()
		if writeErr != nil || closeErr != nil {
			return errors.Join(writeErr, closeErr)
		}
		return c.dir.Sync()
	}
	if err != nil {
		return err
	}
	defer marker.Close()
	info, err := marker.Stat()
	if err != nil {
		return err
	}
	if info.Size() != int64(len(cacheMarkerVersion)) {
		return fmt.Errorf("%w: ownership marker has an unexpected size", ErrCacheInvalid)
	}
	body, err := io.ReadAll(io.LimitReader(marker, int64(len(cacheMarkerVersion))+1))
	if err != nil {
		return err
	}
	if string(body) != cacheMarkerVersion {
		return fmt.Errorf("%w: unknown ownership marker version", ErrCacheInvalid)
	}
	return nil
}

// inspectRoot recognizes names only; it does not remove unknown root content.
func (c *cacheRoot) inspectRoot() ([]string, error) {
	names, err := cacheDirectoryNames(c.dir, maxCacheEntries)
	if err != nil {
		return nil, err
	}
	jobs := make([]string, 0, len(names))
	markerFound, lockFound := false, false
	for _, name := range names {
		switch name {
		case cacheMarkerName:
			markerFound = true
		case cacheLockName:
			lockFound = true
			lock, openErr := cacheOpenRegular(c.dir, name, syscall.O_RDONLY, 0)
			if openErr != nil {
				return nil, openErr
			}
			current, currentErr := lock.Stat()
			_ = lock.Close()
			held, heldErr := c.lock.Stat()
			if currentErr != nil || heldErr != nil || !os.SameFile(current, held) || current.Size() != 0 {
				return nil, fmt.Errorf("%w: lock file changed", ErrCacheUnsafe)
			}
		default:
			if !validJobID(name) {
				return nil, fmt.Errorf("%w: unrecognized root entry", ErrCacheUnsafe)
			}
			dir, openErr := cacheOpenDirectoryAt(c.dir, name)
			if openErr != nil {
				return nil, openErr
			}
			_ = dir.Close()
			jobs = append(jobs, name)
		}
	}
	if !markerFound || !lockFound {
		return nil, fmt.Errorf("%w: cache ownership files are missing", ErrCacheUnsafe)
	}
	return jobs, nil
}

// Recover validates every job before removing the first one. Unknown names,
// symbolic links, nested directories, and special files stop recovery without
// allowing recursive deletion of content that this cache does not recognize.
func (c *cacheRoot) Recover() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	if err := c.ensureMarker(false); err != nil {
		return err
	}
	jobs, err := c.inspectRoot()
	if err != nil {
		return err
	}
	snapshots := make([]cacheJobSnapshot, 0, len(jobs))
	for _, id := range jobs {
		dir, err := cacheOpenDirectoryAt(c.dir, id)
		if err != nil {
			return err
		}
		info, statErr := dir.Stat()
		_, _, _, inspectErr := cacheInspectJob(dir, false)
		_ = dir.Close()
		if statErr != nil || inspectErr != nil {
			return errors.Join(statErr, inspectErr)
		}
		snapshots = append(snapshots, cacheJobSnapshot{name: id, info: info})
	}
	for _, snapshot := range snapshots {
		if err := c.removeJob(snapshot.name, snapshot.info); err != nil {
			return err
		}
	}
	return nil
}

func (c *cacheRoot) CreateJob(id string) error {
	if !validJobID(id) {
		return ErrCacheInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	return syscall.Mkdirat(int(c.dir.Fd()), id, 0o700)
}

func (c *cacheRoot) OpenJobFile(id, name string) (*os.File, error) {
	if !validJobID(id) || !validOutputName(name) {
		return nil, ErrCacheInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, os.ErrClosed
	}
	dir, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return cacheOpenRegular(dir, name, syscall.O_RDONLY, 0)
}

func (c *cacheRoot) ScanJob(id string) (bytes int64, ready bool, err error) {
	if !validJobID(id) {
		return 0, false, ErrCacheInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, false, os.ErrClosed
	}
	dir, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return 0, false, err
	}
	defer dir.Close()
	_, bytes, ready, err = cacheInspectJob(dir, true)
	return bytes, ready, err
}

func (c *cacheRoot) RemoveJob(id string) error {
	if !validJobID(id) {
		return ErrCacheInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	return c.removeJob(id, nil)
}

func (c *cacheRoot) removeJob(id string, expected os.FileInfo) error {
	dir, err := cacheOpenDirectoryAt(c.dir, id)
	if errors.Is(err, os.ErrNotExist) && expected == nil {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	current, err := dir.Stat()
	if err != nil {
		return err
	}
	if expected != nil && !os.SameFile(current, expected) {
		return fmt.Errorf("%w: job directory changed during recovery", ErrCacheUnsafe)
	}
	files, _, _, err := cacheInspectJob(dir, false)
	if err != nil {
		return err
	}
	for _, snapshot := range files {
		file, err := cacheOpenRegular(dir, snapshot.name, syscall.O_RDONLY, 0)
		if err != nil {
			return err
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil {
			return statErr
		}
		if !os.SameFile(info, snapshot.info) {
			return fmt.Errorf("%w: job file changed during cleanup", ErrCacheUnsafe)
		}
		if err := syscall.Unlinkat(int(dir.Fd()), snapshot.name); err != nil {
			return err
		}
	}
	// unlinkat with AT_REMOVEDIR removes only an empty directory entry; it
	// never follows a replacement link or recursively visits other content.
	return cacheRemoveDirectoryAt(c.dir, id)
}

func (c *cacheRoot) FreeBytes() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, os.ErrClosed
	}
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(c.dir.Fd()), &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, ErrCacheUnsafe
	}
	blockSize := uint64(stat.Bsize)
	if stat.Bavail > uint64(math.MaxInt64)/blockSize {
		return math.MaxInt64, nil
	}
	return int64(stat.Bavail * blockSize), nil
}

func cacheInspectJob(dir *os.File, tolerateRename bool) ([]cacheFileSnapshot, int64, bool, error) {
	names, err := cacheDirectoryNames(dir, maxJobFiles)
	if err != nil {
		return nil, 0, false, err
	}
	files := make([]cacheFileSnapshot, 0, len(names))
	type inode struct{ device, number uint64 }
	seen := make(map[inode]bool, len(names))
	var bytes int64
	playlist, segment := false, false
	for _, name := range names {
		if !validCacheFileName(name) {
			return nil, 0, false, fmt.Errorf("%w: unrecognized job file", ErrCacheUnsafe)
		}
		file, err := cacheOpenRegular(dir, name, syscall.O_RDONLY, 0)
		if tolerateRename && errors.Is(err, os.ErrNotExist) {
			// FFmpeg publishes its temporary outputs with an atomic rename.
			// A later scan sees the final name if this directory read did not.
			continue
		}
		if err != nil {
			return nil, 0, false, err
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil {
			return nil, 0, false, statErr
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Size() < 0 {
			return nil, 0, false, ErrCacheUnsafe
		}
		key := inode{device: uint64(stat.Dev), number: stat.Ino}
		if !seen[key] {
			if info.Size() > math.MaxInt64-bytes {
				return nil, 0, false, fmt.Errorf("%w: aggregate job size overflow", ErrCacheUnsafe)
			}
			bytes += info.Size()
			seen[key] = true
		}
		playlist = playlist || name == "main.m3u8" && info.Size() > 0
		segment = segment || strings.HasPrefix(name, "segment-") && strings.HasSuffix(name, ".ts") && info.Size() > 0
		files = append(files, cacheFileSnapshot{name: name, info: info})
	}
	return files, bytes, playlist && segment, nil
}

func cacheDirectoryNames(dir *os.File, limit int) ([]string, error) {
	// Reopen instead of dup: dup would share the directory offset with callers.
	reader, err := cacheOpenDirectoryAt(dir, ".")
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var names []string
	for {
		batch, readErr := reader.Readdirnames(cacheReadBatch)
		if len(batch) > limit-len(names) {
			return nil, fmt.Errorf("%w: directory entry limit exceeded", ErrCacheUnsafe)
		}
		names = append(names, batch...)
		if errors.Is(readErr, io.EOF) {
			return names, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func cacheOpenDirectoryAt(parent *os.File, name string) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, cacheOpenError(err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func cacheOpenRegular(parent *os.File, name string, flags int, mode uint32) (*os.File, error) {
	// O_NONBLOCK prevents an unexpected FIFO from blocking before its type can
	// be checked. Regular files remain seekable and support normal reads.
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, mode)
	if err != nil {
		return nil, cacheOpenError(err)
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink > 1 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: output must be a regular file without hard links", ErrCacheUnsafe)
	}
	// A published playlist may be replaced immediately after openat. A zero
	// link count is safe for that already-open regular inode; rejecting it
	// would turn normal atomic playlist publication into a spurious failure.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: output is not seekable", ErrCacheUnsafe)
	}
	return file, nil
}

func cacheOpenError(err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return errors.Join(ErrCacheUnsafe, err)
	}
	return err
}

func cacheRemoveDirectoryAt(parent *os.File, name string) error {
	const atRemovedir = 0x200
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_UNLINKAT, parent.Fd(), uintptr(unsafe.Pointer(pointer)), atRemovedir, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func validJobID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validOutputName(name string) bool {
	if name == "main.m3u8" {
		return true
	}
	if len(name) > 255 || !strings.HasPrefix(name, "segment-") || !strings.HasSuffix(name, ".ts") {
		return false
	}
	digits := name[len("segment-") : len(name)-len(".ts")]
	if digits == "" {
		return false
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validCacheFileName(name string) bool {
	return len(name) <= 255 && validOutputName(strings.TrimSuffix(name, ".tmp"))
}
