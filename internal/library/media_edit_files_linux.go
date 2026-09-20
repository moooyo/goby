//go:build linux

package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func mediaEditSameFilesystem(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return false
	}
	first, ok := a.Sys().(*syscall.Stat_t)
	second, other := b.Sys().(*syscall.Stat_t)
	return ok && other && first.Dev == second.Dev
}

func mediaEditTrustedToolPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnavailable
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnavailable
		}
		if current == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return ErrUnavailable
			}
		} else if !info.IsDir() {
			return ErrUnavailable
		}
		if current == string(filepath.Separator) {
			return nil
		}
	}
}

func mediaEditExchange(source *os.Root, sourceName string, target *os.Root, targetName string) error {
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
			operationErr = unix.Renameat2(int(fromFD), sourceName, int(toFD), targetName, unix.RENAME_EXCHANGE)
		})
	}); err != nil {
		return err
	}
	return errors.Join(targetErr, operationErr)
}

// Copy filesystem ownership, mode and xattrs before a candidate becomes ready.
// Integrity signatures are source-content signatures and cannot be copied to
// new bytes. Unsupported attribute operations therefore fail closed.
func mediaEditCopyAttributes(source, candidate *os.File) (string, error) {
	before, err := source.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", ErrUnavailable
	}
	attributes, err := mediaEditReadAttributes(source)
	if err != nil {
		return "", err
	}
	for name := range attributes {
		if name == "security.ima" || name == "security.evm" {
			return "", ErrUnavailable
		}
	}
	current, err := candidate.Stat()
	if err != nil {
		return "", err
	}
	other, ok := current.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrUnavailable
	}
	if other.Uid != stat.Uid || other.Gid != stat.Gid {
		if err := candidate.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return "", err
		}
	}
	if err := candidate.Chmod(before.Mode().Perm()); err != nil {
		return "", err
	}
	existing, err := mediaEditReadAttributes(candidate)
	if err != nil {
		return "", err
	}
	for name := range existing {
		if _, present := attributes[name]; !present {
			if err := unix.Fremovexattr(int(candidate.Fd()), name); err != nil {
				return "", err
			}
		}
	}
	for name, value := range attributes {
		if err := unix.Fsetxattr(int(candidate.Fd()), name, value, 0); err != nil {
			return "", err
		}
	}
	expected, err := mediaEditAttributeDigest(source)
	if err != nil {
		return "", err
	}
	actual, err := mediaEditAttributeDigest(candidate)
	if err != nil || actual != expected {
		return "", errors.Join(ErrSourceChanged, err)
	}
	after, err := source.Stat()
	if err != nil || !sameMediaSourceFile(before, after) || mediaEditStatChanged(before, after) {
		return "", ErrSourceChanged
	}
	return expected, nil
}

func mediaEditStatChanged(a, b os.FileInfo) bool {
	first, ok := a.Sys().(*syscall.Stat_t)
	second, other := b.Sys().(*syscall.Stat_t)
	return !ok || !other || first.Size != second.Size || first.Mtim != second.Mtim || first.Ctim != second.Ctim || first.Mode != second.Mode || first.Uid != second.Uid || first.Gid != second.Gid
}

func mediaEditReadAttributes(file *os.File) (map[string][]byte, error) {
	const maxNames, maxValue, maxTotal = 64 << 10, 64 << 10, 512 << 10
	length, err := unix.Flistxattr(int(file.Fd()), nil)
	if errors.Is(err, unix.ENOTSUP) {
		return map[string][]byte{}, nil
	}
	if err != nil || length < 0 || length > maxNames {
		return nil, errors.Join(ErrUnavailable, err)
	}
	names := make([]byte, length)
	read, err := unix.Flistxattr(int(file.Fd()), names)
	if err != nil || read != length {
		return nil, errors.Join(ErrSourceChanged, err)
	}
	result, total := make(map[string][]byte), 0
	for _, name := range strings.Split(string(names), "\x00") {
		if name == "" {
			continue
		}
		if len(name) > 255 || len(result) >= 256 {
			return nil, ErrUnavailable
		}
		size, err := unix.Fgetxattr(int(file.Fd()), name, nil)
		if err != nil || size < 0 || size > maxValue || total+size > maxTotal {
			return nil, errors.Join(ErrUnavailable, err)
		}
		value := make([]byte, size)
		n, err := unix.Fgetxattr(int(file.Fd()), name, value)
		if err != nil || n != size {
			return nil, errors.Join(ErrSourceChanged, err)
		}
		result[name], total = value, total+size
	}
	return result, nil
}

func mediaEditAttributeDigest(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return "", ErrUnavailable
	}
	attributes, err := mediaEditReadAttributes(file)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	// Fixed-width binary framing prevents concatenation ambiguity.
	for _, value := range []uint32{stat.Mode, stat.Uid, stat.Gid} {
		_, _ = hash.Write([]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
	}
	for _, name := range names {
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		length := uint32(len(attributes[name]))
		_, _ = hash.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
		_, _ = hash.Write(attributes[name])
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
