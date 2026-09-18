//go:build linux

package library

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func fileDeletionSupported() bool { return true }

func fileDeletionPrivateDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && fileIdentity(info) != ""
}

func fileDeletionDirectoryFD(root *os.Root) (*os.File, syscall.RawConn, error) {
	file, err := openScanFile(root, ".")
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		_ = file.Close()
		return nil, nil, fileDeletionError("captured directory descriptor is invalid", err)
	}
	connection, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, connection, nil
}

func fileDeletionRenameNoReplace(source *os.Root, sourceName string, target *os.Root, targetName string) error {
	from, fromConnection, err := fileDeletionDirectoryFD(source)
	if err != nil {
		return err
	}
	defer from.Close()
	to, toConnection, err := fileDeletionDirectoryFD(target)
	if err != nil {
		return err
	}
	defer to.Close()
	var operationErr, targetErr error
	if err := fromConnection.Control(func(fromFD uintptr) {
		targetErr = toConnection.Control(func(toFD uintptr) {
			operationErr = unix.Renameat2(int(fromFD), sourceName, int(toFD), targetName, unix.RENAME_NOREPLACE)
		})
	}); err != nil {
		return err
	}
	if targetErr != nil {
		return targetErr
	}
	return operationErr
}

func fileDeletionUnlink(root *os.Root, name string) error {
	file, connection, err := fileDeletionDirectoryFD(root)
	if err != nil {
		return err
	}
	defer file.Close()
	var operationErr error
	if err := connection.Control(func(descriptor uintptr) {
		operationErr = unix.Unlinkat(int(descriptor), name, 0)
	}); err != nil {
		return err
	}
	return operationErr
}
