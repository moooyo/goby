//go:build linux

package recoverycontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type trackedFile struct {
	Present  bool
	Identity identity
	Digest   string
}

func statIdentity(stat unix.Stat_t) identity {
	return identity{Device: uint64(stat.Dev), Inode: stat.Ino}
}
func ownedRegular(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&07777 == 0600 && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}
func ownedDirectory(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&07777 == 0700 && stat.Nlink != 0 && stat.Uid == uint32(os.Geteuid())
}
func validDirectory(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && len(path) <= 4096 && !strings.ContainsAny(path, "\x00\\")
}
func temporaryName(name string) bool {
	return strings.HasPrefix(name, ".next-") && validHex(strings.TrimPrefix(name, ".next-"), 32)
}

func openRoot(ctx context.Context, path string, create bool) (*os.File, identity, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, identity{}, ErrUnavailable
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			unix.Close(fd)
			return nil, identity{}, err
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create && index == len(parts)-1 {
			if mkdirErr := unix.Mkdirat(fd, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return nil, identity{}, ErrUnavailable
			}
			if err := unix.Fsync(fd); err != nil {
				unix.Close(fd)
				return nil, identity{}, ErrUnavailable
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return nil, identity{}, ErrUnavailable
		}
		fd = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || !ownedDirectory(stat) {
		unix.Close(fd)
		return nil, identity{}, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "recovery control directory"), statIdentity(stat), nil
}

func openRegular(directory *os.File, name string, flags int) (*os.File, unix.Stat_t, error) {
	var stat unix.Stat_t
	fd, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, stat, err
	}
	if err := unix.Fstat(fd, &stat); err != nil || !ownedRegular(stat) {
		unix.Close(fd)
		return nil, stat, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), name), stat, nil
}

func namedIdentity(directory *os.File, name string, expected identity) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil || !ownedRegular(stat) || statIdentity(stat) != expected {
		return ErrUnavailable
	}
	return nil
}

func readFile(ctx context.Context, directory *os.File, name string, limit int64) ([]byte, trackedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, trackedFile{}, err
	}
	file, stat, err := openRegular(directory, name, unix.O_RDONLY)
	if errors.Is(err, unix.ENOENT) {
		return nil, trackedFile{}, nil
	}
	if err != nil {
		return nil, trackedFile{}, ErrUnavailable
	}
	defer file.Close()
	if stat.Size < 0 || stat.Size > limit {
		return nil, trackedFile{}, ErrUnavailable
	}
	data := make([]byte, stat.Size)
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, trackedFile{}, err
		}
		end := min(offset+32*1024, len(data))
		n, err := io.ReadFull(file, data[offset:end])
		offset += n
		if err != nil {
			return nil, trackedFile{}, ErrUnavailable
		}
	}
	var extra [1]byte
	if n, err := file.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, trackedFile{}, ErrUnavailable
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || !ownedRegular(after) || statIdentity(after) != statIdentity(stat) || after.Size != stat.Size || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim {
		return nil, trackedFile{}, ErrUnavailable
	}
	if err := namedIdentity(directory, name, statIdentity(stat)); err != nil {
		return nil, trackedFile{}, err
	}
	return data, trackedFile{Present: true, Identity: statIdentity(stat), Digest: hash(data)}, nil
}

func directoryNames(directory *os.File) ([]string, error) {
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "recovery control directory listing")
	defer file.Close()
	names, err := file.Readdirnames(maxDirectoryEntries + 1)
	if err != nil && err != io.EOF || len(names) > maxDirectoryEntries {
		return nil, ErrRecoveryRequired
	}
	return names, nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (s *Store) write(ctx context.Context, file *os.File, data []byte) error {
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Write(data[offset:min(offset+32*1024, len(data))])
		offset += n
		if err != nil || n == 0 {
			return ErrUnavailable
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.syncFile(file); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) checkFile(ctx context.Context, name string, expected trackedFile) error {
	_, actual, err := readFile(ctx, s.directory, name, maxRecordBytes)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) createTemporary(ctx context.Context, data []byte) (string, trackedFile, error) {
	if err := s.checkRoot(ctx); err != nil {
		return "", trackedFile{}, err
	}
	id, err := newID()
	if err != nil {
		return "", trackedFile{}, err
	}
	name := ".next-" + id
	file, stat, err := openRegular(s.directory, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return "", trackedFile{}, ErrUnavailable
	}
	defer file.Close()
	tracked := trackedFile{Present: true, Identity: statIdentity(stat), Digest: hash(data)}
	if err := s.write(ctx, file, data); err != nil {
		s.removeOwnedTemporary(name, tracked)
		return "", trackedFile{}, err
	}
	if err := namedIdentity(s.directory, name, tracked.Identity); err != nil {
		return "", trackedFile{}, err
	}
	return name, tracked, nil
}

// removeOwnedTemporary never scans for names to delete. Its identity must have
// been created by this exact call; process-restart debris remains untouched.
func (s *Store) removeOwnedTemporary(name string, expected trackedFile) {
	if !temporaryName(name) || !expected.Present || s.directory == nil {
		return
	}
	if namedIdentity(s.directory, name, expected.Identity) == nil {
		_ = unix.Unlinkat(int(s.directory.Fd()), name, 0)
	}
}

func (s *Store) publish(ctx context.Context, temporary string, candidate trackedFile, name string, expected trackedFile) (trackedFile, error) {
	if err := s.checkRoot(ctx); err != nil {
		return trackedFile{}, err
	}
	if err := s.checkFile(ctx, name, expected); err != nil {
		return trackedFile{}, err
	}
	if err := s.checkFile(ctx, temporary, candidate); err != nil {
		return trackedFile{}, err
	}
	if err := ctx.Err(); err != nil {
		return trackedFile{}, err
	}
	flags := uint(0)
	if !expected.Present {
		flags = unix.RENAME_NOREPLACE
	}
	if err := s.rename(int(s.directory.Fd()), temporary, int(s.directory.Fd()), name, flags); err != nil {
		return trackedFile{}, ErrUnavailable
	}
	// After rename the new record is already visible. A late cancellation
	// cannot authorize undoing it, and a failed sync is an uncertain outcome.
	if err := s.syncDirectory(s.directory); err != nil {
		s.degraded = true
		return candidate, ErrRecoveryRequired
	}
	if err := namedIdentity(s.directory, name, candidate.Identity); err != nil {
		s.degraded = true
		return candidate, ErrRecoveryRequired
	}
	return candidate, nil
}

func (s *Store) replace(ctx context.Context, name string, data []byte, expected trackedFile) (trackedFile, error) {
	temporary, candidate, err := s.createTemporary(ctx, data)
	if err != nil {
		return trackedFile{}, err
	}
	defer s.removeOwnedTemporary(temporary, candidate)
	return s.publish(ctx, temporary, candidate, name, expected)
}
