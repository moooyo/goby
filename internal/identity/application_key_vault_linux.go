//go:build linux

package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	applicationKeyLockTimeout = 5 * time.Second
	applicationKeyLockPoll    = 10 * time.Millisecond
	applicationKeyOpenFlags   = syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
)

func (v *ApplicationKeyVault) loadMasterKey(ctx context.Context, allowCreate bool) (key [applicationKeyMasterSize]byte, err error) {
	defer func() {
		if err != nil {
			clear(key[:])
		}
	}()
	if err := ctx.Err(); err != nil {
		return key, err
	}
	if v == nil || !filepath.IsAbs(v.path) || filepath.Clean(v.path) != v.path || v.path == string(filepath.Separator) {
		return key, ErrApplicationKeyVaultUnsafe
	}
	directoryFD, err := openApplicationKeyDirectory(ctx, filepath.Dir(v.path))
	if err != nil {
		return key, err
	}
	defer syscall.Close(directoryFD)
	if err := lockApplicationKeyDirectory(ctx, directoryFD); err != nil {
		return key, err
	}
	defer syscall.Flock(directoryFD, syscall.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return key, err
	}
	name := filepath.Base(v.path)
	key, identity, err := readApplicationKeyFile(directoryFD, name)
	if errors.Is(err, ErrApplicationKeyVaultMissing) && allowCreate && !v.hasObservedKey() {
		key, identity, err = createApplicationKeyFile(ctx, directoryFD, name)
	}
	if err != nil {
		return key, err
	}
	if err := ctx.Err(); err != nil {
		return key, err
	}
	if err := v.observeKey(key[:], identity); err != nil {
		return key, err
	}
	return key, nil
}

// Every component is opened relative to the preceding directory descriptor.
// O_NOFOLLOW applies to every ancestor, not just the master file's final name.
func openApplicationKeyDirectory(ctx context.Context, path string) (int, error) {
	flags := applicationKeyOpenFlags | syscall.O_DIRECTORY
	fd, err := syscall.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return -1, ErrApplicationKeyVaultUnavailable
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			syscall.Close(fd)
			return -1, err
		}
		next, openErr := syscall.Openat(fd, component, flags, 0)
		syscall.Close(fd)
		if openErr != nil {
			if errors.Is(openErr, syscall.ELOOP) || errors.Is(openErr, syscall.ENOTDIR) {
				return -1, ErrApplicationKeyVaultUnsafe
			}
			return -1, ErrApplicationKeyVaultUnavailable
		}
		fd = next
	}
	return fd, nil
}

// Directory flock coordinates independent instances and processes without a
// second persistent secret-adjacent lock file. All readers take the same lock,
// so exclusive creation cannot expose a partially written master key to them.
func lockApplicationKeyDirectory(ctx context.Context, fd int) error {
	deadline := time.NewTimer(applicationKeyLockTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(applicationKeyLockPoll)
	defer poll.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return ErrApplicationKeyVaultUnavailable
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrApplicationKeyVaultUnavailable
		case <-poll.C:
		}
	}
}

func readApplicationKeyFile(directoryFD int, name string) (key [applicationKeyMasterSize]byte, identity applicationKeyFileIdentity, err error) {
	defer func() {
		if err != nil {
			clear(key[:])
		}
	}()
	fd, err := syscall.Openat(directoryFD, name, applicationKeyOpenFlags, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return key, identity, ErrApplicationKeyVaultMissing
		}
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return key, identity, ErrApplicationKeyVaultUnsafe
		}
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	identity, err = checkApplicationKeyFile(fd, applicationKeyMasterSize)
	if err != nil {
		return key, identity, err
	}
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return key, identity, ErrApplicationKeyVaultUnsafe
	}
	var trailing [1]byte
	if n, readErr := file.Read(trailing[:]); n != 0 || !errors.Is(readErr, io.EOF) {
		return key, identity, ErrApplicationKeyVaultUnsafe
	}
	after, err := checkApplicationKeyFile(fd, applicationKeyMasterSize)
	if err != nil || after != identity {
		return key, identity, ErrApplicationKeyVaultUnsafe
	}
	if err := checkApplicationKeyName(directoryFD, name, identity); err != nil {
		return key, identity, err
	}
	return key, identity, nil
}

func checkApplicationKeyFile(fd int, size int64) (applicationKeyFileIdentity, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return applicationKeyFileIdentity{}, ErrApplicationKeyVaultUnavailable
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Mode&07777 != 0600 ||
		stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Size != size {
		return applicationKeyFileIdentity{}, ErrApplicationKeyVaultUnsafe
	}
	return applicationKeyFileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func checkApplicationKeyName(directoryFD int, name string, expected applicationKeyFileIdentity) error {
	fd, err := syscall.Openat(directoryFD, name, applicationKeyOpenFlags, 0)
	if err != nil {
		return ErrApplicationKeyVaultUnsafe
	}
	defer syscall.Close(fd)
	identity, err := checkApplicationKeyFile(fd, applicationKeyMasterSize)
	if err != nil || identity != expected {
		return ErrApplicationKeyVaultUnsafe
	}
	return nil
}

func createApplicationKeyFile(ctx context.Context, directoryFD int, name string) (key [applicationKeyMasterSize]byte, identity applicationKeyFileIdentity, err error) {
	defer func() {
		if err != nil {
			clear(key[:])
		}
	}()
	if err := ctx.Err(); err != nil {
		return key, identity, err
	}
	if _, err := rand.Read(key[:]); err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	if err := ctx.Err(); err != nil {
		return key, identity, err
	}
	flags := syscall.O_WRONLY | syscall.O_CREAT | syscall.O_EXCL | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	fd, err := syscall.Openat(directoryFD, name, flags, 0600)
	if errors.Is(err, syscall.EEXIST) {
		clear(key[:])
		return readApplicationKeyFile(directoryFD, name)
	}
	if err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	// Once creation starts, finish this small durable write even if cancellation
	// arrives. Leaving an incomplete file would block all future safe use.
	if err := syscall.Fchmod(fd, 0600); err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	identity, err = checkApplicationKeyFile(fd, 0)
	if err != nil {
		return key, identity, err
	}
	if n, writeErr := file.Write(key[:]); writeErr != nil || n != len(key) {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	after, err := checkApplicationKeyFile(fd, applicationKeyMasterSize)
	if err != nil || after != identity {
		return key, identity, ErrApplicationKeyVaultUnsafe
	}
	if err := file.Sync(); err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	if err := file.Close(); err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	if err := syscall.Fsync(directoryFD); err != nil {
		return key, identity, ErrApplicationKeyVaultUnavailable
	}
	if err := checkApplicationKeyName(directoryFD, name, identity); err != nil {
		return key, identity, err
	}
	return key, identity, nil
}
