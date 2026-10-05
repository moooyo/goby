//go:build linux

package library

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func lockBackgroundClipDirectory(root *os.Root) (func() error, error) {
	file, err := root.OpenFile(".generation.lock", os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		file.Close()
		return nil, ErrBackgroundClipConflict
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() error { return errors.Join(unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close()) }, nil
}

func backgroundClipRenameNoReplace(root *os.Root, oldName, newName string) error {
	if strings.ContainsAny(oldName+newName, "/\\\x00") || oldName == "." || newName == "." || oldName == ".." || newName == ".." {
		return ErrInvalidInput
	}
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return unix.Renameat2(int(file.Fd()), oldName, int(file.Fd()), newName, unix.RENAME_NOREPLACE)
}

func syncBackgroundClipDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
