//go:build linux

package library

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openScanSpoolFile(root *os.Root, name string, flags int) (*os.File, error) {
	return root.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
}

func scanSpoolDirectoryIdentity(file *os.File) (scanSpoolIdentity, bool, error) {
	handle, mountID, err := unix.NameToHandleAt(int(file.Fd()), "", unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
		return scanSpoolIdentity{}, false, nil
	}
	if err != nil {
		return scanSpoolIdentity{}, false, err
	}
	if mountID <= 0 || int64(mountID) > 1<<31-1 || handle.Type() <= 0 || handle.Size() < 1 || handle.Size() > 128 {
		return scanSpoolIdentity{}, false, errScanReconciliationEvidenceUnavailable
	}
	return scanSpoolIdentity{kind: 1, mountID: int32(mountID), handleType: handle.Type(), bytes: string(handle.Bytes())}, true, nil
}

func removeScanSpoolLeaf(root *os.Root, name string) error {
	return unlinkScanSpool(root, name, 0)
}

func removeScanSpoolDirectory(root *os.Root, name string) error {
	return unlinkScanSpool(root, name, unix.AT_REMOVEDIR)
}

func unlinkScanSpool(root *os.Root, name string, flags int) (result error) {
	if !scanSpoolRawName(name) {
		return errScanReconciliationEvidenceUnavailable
	}
	directory, err := openScanFile(root, ".")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	return unix.Unlinkat(int(directory.Fd()), name, flags)
}
