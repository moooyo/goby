//go:build linux

package library

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func scanEvidencePlatformSupported() bool { return true }

func scanEvidencePrivateInfo(info os.FileInfo, directory bool) error {
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok || value.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 ||
		directory && (!info.IsDir() || info.Mode().Perm() != 0700) ||
		!directory && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600 || value.Nlink != 1) {
		return fmt.Errorf("%w: scan evidence inventory is not privately owned", ErrUnavailable)
	}
	return nil
}

func scanEvidenceFileIdentity(info os.FileInfo) (uint64, uint64, error) {
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, ErrUnavailable
	}
	return uint64(value.Dev), value.Ino, nil
}

func scanEvidenceOpenFile(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	before, statErr := root.Lstat(name)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if statErr == nil {
		if err := scanEvidencePrivateInfo(before, false); err != nil {
			return nil, err
		}
	} else {
		flags |= os.O_EXCL
	}
	file, err := root.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, mode)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err == nil {
		err = scanEvidencePrivateInfo(opened, false)
	}
	named, namedErr := root.Lstat(name)
	if err == nil && (namedErr != nil || !os.SameFile(opened, named) || before != nil && !os.SameFile(before, opened)) {
		err = errors.Join(ErrUnavailable, namedErr)
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func scanEvidenceLockFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("%w: scan evidence directory is already owned: %w", ErrBusy, err)
	}
	return nil
}

func scanEvidenceFilesystemIdentity(root *os.Root) (scanEvidenceFilesystem, error) {
	file, err := root.Open(".")
	if err != nil {
		return scanEvidenceFilesystem{}, err
	}
	var value unix.Statfs_t
	err = unix.Fstatfs(int(file.Fd()), &value)
	return scanEvidenceFilesystem{Type: int64(value.Type), ID: value.Fsid.Val}, errors.Join(err, file.Close())
}

func scanEvidencePersistentHandle(root *os.Root) (int32, []byte, error) {
	file, err := root.Open(".")
	if err != nil {
		return 0, nil, err
	}
	before, statErr := file.Stat()
	if statErr != nil {
		return 0, nil, errors.Join(statErr, file.Close())
	}
	identity, supported, identityErr := scanSpoolDirectoryIdentity(file)
	after, afterErr := file.Stat()
	if afterErr == nil && !os.SameFile(before, after) {
		afterErr = ErrUnavailable
	}
	if err = errors.Join(identityErr, afterErr, file.Close()); err != nil {
		return 0, nil, err
	}
	if !supported {
		return 0, nil, nil
	}
	return identity.handleType, []byte(identity.bytes), nil
}
